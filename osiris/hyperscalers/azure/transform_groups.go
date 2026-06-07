// transform_groups.go - Group transforms (Resource Group, Subscription, Region groups and wiring helpers).
//
// For an introduction to OSIRIS JSON Producer for Microsoft Azure see:
// [OSIRIS-JSON-AZURE]: https://osirisjson.org/en/docs/producers/hyperscalers/microsoft-azure
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/docs/spec/v10/00-preface

package azure

import (
	"strings"

	"go.osirisjson.org/producers/pkg/sdk"
)

// TransformResourceGroupResources creates OSIRIS JSON resources of type container.resourcegroup
// for each Azure resource group. Per OSIRIS JSON specification Appendix C.5, resource groups are modeled
// as resources to enable full provenance tracking.
func TransformResourceGroupResources(rgs []ResourceGroup, sub SubscriptionInfo) []sdk.Resource {
	var resources []sdk.Resource
	for _, rg := range rgs {
		id := resourceID("container.resourcegroup", rg.ID)
		prov := azureProvider(rg.ID, "Microsoft.Resources/resourceGroups", rg.Location, sub)

		r, err := sdk.NewResource(id, "container.resourcegroup", prov)
		if err != nil {
			continue
		}
		r.Name = rg.Name
		r.Description = deriveDescription(rg.Name, "", rg.Tags)
		r.Tags = rg.Tags
		ps := rg.Properties.ProvisioningState
		if ps == "" {
			ps = "Succeeded"
		}
		r.Status = mapProvisioningState(ps)
		r.State = ps
		r.Properties = map[string]any{
			"location": rg.Location,
		}
		attachArmBody(&r, &rg)
		resources = append(resources, r)
	}
	return resources
}

// OSIRIS JSON Group transforms

// TransformSubscriptionGroup creates an OSIRIS JSON group for the subscription.
func TransformSubscriptionGroup(sub SubscriptionInfo) sdk.Group {
	gid := sdk.GroupID(sdk.GroupIDInput{
		Type:          "logical.subscription",
		BoundaryToken: sub.SubscriptionID,
	})

	g, _ := sdk.NewGroup(gid, "logical.subscription")
	g.Name = sub.DisplayName
	g.Tags = sub.Tags
	return g
}

// TransformResourceGroupGroups creates OSIRIS JSON groups for each Azure resource group.
// Returns the groups and a map of resource group name (lowered) -> group ID for membership wiring.
func TransformResourceGroupGroups(rgs []ResourceGroup, sub SubscriptionInfo) ([]sdk.Group, map[string]string) {
	var groups []sdk.Group
	nameToID := make(map[string]string, len(rgs))

	for _, rg := range rgs {
		gid := sdk.GroupID(sdk.GroupIDInput{
			Type:          "logical.resourcegroup",
			BoundaryToken: rg.ID,
		})
		nameToID[strings.ToLower(rg.Name)] = gid

		g, err := sdk.NewGroup(gid, "logical.resourcegroup")
		if err != nil {
			continue
		}
		g.Name = rg.Name
		g.Description = deriveDescription(rg.Name, "", rg.Tags)
		g.Tags = rg.Tags
		g.Properties = map[string]any{
			"location": rg.Location,
		}
		ext := map[string]any{}
		ps := rg.Properties.ProvisioningState
		if ps == "" {
			ps = "Succeeded"
		}
		ext["provisioning_state"] = ps
		if rg.ManagedBy != "" {
			ext["managed_by"] = rg.ManagedBy
		}
		g.Extensions = map[string]any{extensionNamespace: ext}
		groups = append(groups, g)
	}
	return groups, nameToID
}

// WireResourcesToResourceGroups assigns resources as members of their resource group.
func WireResourcesToResourceGroups(resources []sdk.Resource, rgNameToGroupID map[string]string, rgGroups []sdk.Group) {
	idx := groupIndex(rgGroups)
	for _, r := range resources {
		rgName := ""
		if r.Properties != nil {
			if rg, ok := r.Properties["resource_group"].(string); ok {
				rgName = strings.ToLower(rg)
			}
		}
		if rgName == "" {
			rgName = strings.ToLower(extractResourceGroup(r.Provider.NativeID))
		}
		if rgName == "" {
			continue
		}
		groupID, ok := rgNameToGroupID[rgName]
		if !ok {
			continue
		}
		if i, ok := idx[groupID]; ok {
			rgGroups[i].AddMembers(r.ID)
		}
	}
}

// WireResourceGroupsToSubscription adds resource group group IDs as children of the subscription group.
func WireResourceGroupsToSubscription(subGroup *sdk.Group, rgGroups []sdk.Group) {
	for _, rg := range rgGroups {
		subGroup.AddChildren(rg.ID)
	}
}

// TransformRegionGroups builds one container.region group per distinct
// provider.region value found on the resources, membering every resource in
// that region. Region "global" and empty-region resources are skipped -
// they are not geographically scoped. The boundary token is
// "<subscription-id>/<region>" so groups do not collide across subscriptions.
//
// OSIRIS JSON spec chapter 6 section 6.5 defines container.region as a standard group type for
// geographical or regional metadata distribution visualisation.
func TransformRegionGroups(resources []sdk.Resource, sub SubscriptionInfo) []sdk.Group {
	regions := map[string][]string{}
	for _, r := range resources {
		reg := r.Provider.Region
		if reg == "" || reg == "global" {
			continue
		}
		regions[reg] = append(regions[reg], r.ID)
	}
	groups := make([]sdk.Group, 0, len(regions))
	for reg, members := range regions {
		gid := sdk.GroupID(sdk.GroupIDInput{
			Type:          "container.region",
			BoundaryToken: sub.SubscriptionID + "/" + reg,
		})
		g, err := sdk.NewGroup(gid, "container.region")
		if err != nil {
			continue
		}
		g.Name = reg
		g.Properties = map[string]any{"region": reg}
		g.AddMembers(members...)
		groups = append(groups, g)
	}
	return groups
}

// buildMGAncestors extracts the management group ancestry chain for subscriptionID
// from the flat entity list returned by az account management-group entities list.
// Returns the ancestor MGEntities ordered root-to-leaf and the display path string
// (e.g. "Tenant Root Group > IT Hub > Production").
// Returns nil, "" when the subscription cannot be found or has no parent MGs.
func buildMGAncestors(entities []MGEntity, subscriptionID string) ([]MGEntity, []string) {
	var subEntity *MGEntity
	for i := range entities {
		e := &entities[i]
		if strings.EqualFold(e.Type, "/subscriptions") && strings.EqualFold(e.Name, subscriptionID) {
			subEntity = e
			break
		}
	}
	if subEntity == nil || len(subEntity.ParentNameChain) == 0 {
		return nil, nil
	}

	byName := make(map[string]*MGEntity, len(entities))
	for i := range entities {
		e := &entities[i]
		if strings.EqualFold(e.Type, "Microsoft.Management/managementGroups") {
			byName[e.Name] = e
		}
	}

	ancestors := make([]MGEntity, 0, len(subEntity.ParentNameChain))
	for _, name := range subEntity.ParentNameChain {
		if e, ok := byName[name]; ok {
			ancestors = append(ancestors, *e)
		}
	}
	return ancestors, subEntity.ParentDisplayNameChain
}

// TransformManagementGroupGroups creates OSIRIS JSON logical.managementgroup groups for
// each management group in the ancestry chain. Groups are ordered root-to-leaf.
// Returns groups and a name -> groupID map for hierarchy wiring.
func TransformManagementGroupGroups(ancestors []MGEntity) ([]sdk.Group, map[string]string) {
	groups := make([]sdk.Group, 0, len(ancestors))
	nameToID := make(map[string]string, len(ancestors))

	for _, mg := range ancestors {
		gid := sdk.GroupID(sdk.GroupIDInput{
			Type:          "logical.managementgroup",
			BoundaryToken: mg.ID,
		})
		nameToID[mg.Name] = gid

		g, err := sdk.NewGroup(gid, "logical.managementgroup")
		if err != nil {
			continue
		}
		g.Name = mg.DisplayName
		g.Properties = map[string]any{
			"management_group_id": mg.Name,
			"tenant_id":           mg.TenantID,
		}
		groups = append(groups, g)
	}
	return groups, nameToID
}

// WireMGHierarchy wires management group groups into a root-to-leaf parent-child
// chain and connects the subscription group as a child of the leaf (direct parent) MG.
// mgGroups must be ordered root-to-leaf. subGroup is the subscription group; the leaf MG
// gains subGroup.ID as a child so the full hierarchy is: Root MG -> ... -> Leaf MG -> Sub.
func WireMGHierarchy(mgGroups []sdk.Group, subGroup *sdk.Group) {
	if len(mgGroups) == 0 {
		return
	}
	for i := 0; i < len(mgGroups)-1; i++ {
		mgGroups[i].AddChildren(mgGroups[i+1].ID)
	}
	mgGroups[len(mgGroups)-1].AddChildren(subGroup.ID)
}

// groupIndex builds a map of group ID -> index in slice for efficient mutation.
func groupIndex(groups []sdk.Group) map[string]int {
	idx := make(map[string]int, len(groups))
	for i, g := range groups {
		idx[g.ID] = i
	}
	return idx
}

// raToMap converts a RoleAssignment to the canonical map shape used in group extensions.
func raToMap(ra RoleAssignment) map[string]any {
	m := map[string]any{
		"role":         ra.RoleName,
		"principal_id": ra.PrincipalID,
	}
	if ra.PrincipalType != "" {
		m["principal_type"] = ra.PrincipalType
	}
	if ra.PrincipalName != "" {
		m["principal_name"] = ra.PrincipalName
	}
	return m
}

// appendGroupRA appends a role-assignment map to the group's osiris.azure extension.
func appendGroupRA(g *sdk.Group, ra map[string]any) {
	if g.Extensions == nil {
		g.Extensions = make(map[string]any)
	}
	ext, _ := g.Extensions[extensionNamespace].(map[string]any)
	if ext == nil {
		ext = make(map[string]any)
	}
	var ras []map[string]any
	if existing, ok := ext["role_assignments"].([]map[string]any); ok {
		ras = existing
	}
	ext["role_assignments"] = append(ras, ra)
	g.Extensions[extensionNamespace] = ext
}
