// transform_groups.go - Group transforms (Resource Group, Subscription,
// Region groups and wiring helpers).
//
// For an introduction to OSIRIS JSON Producer for Microsoft Azure see:
// [OSIRIS-JSON-AZURE]: https://docs.osirisjson.org/osiris-producers/hyperscalers/microsoft-azure/
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package azure

import (
	"strings"

	"go.osirisjson.org/producers/pkg/sdk"
)

// TransformResourceGroupResources creates OSIRIS JSON resources of type
// container.resourcegroup for each Azure resource group.
// Per OSIRIS JSON specification Appendix C.5, resource groups are
// modeled as resources to enable full provenance tracking.
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
// TransformSubscriptionGroup creates an OSIRIS JSON group for
// the subscription.
func TransformSubscriptionGroup(sub SubscriptionInfo) sdk.Group {
	gid := sdk.GroupID(sdk.GroupIDInput{
		Type:          "logical.subscription",
		BoundaryToken: sub.SubscriptionID,
	})

	g, _ := sdk.NewGroup(gid, "logical.subscription")
	g.Name = sub.DisplayName
	g.Tags = sub.Tags
	props := map[string]any{"subscription_id": canonicalSubscriptionID(sub.SubscriptionID)}
	if sub.TenantID != "" {
		props["tenant_id"] = sub.TenantID
	}
	if sub.TenantDisplayName != "" {
		props["tenant_name"] = sub.TenantDisplayName
	}
	g.Properties = props
	return g
}

// TransformResourceGroupGroups creates OSIRIS JSON groups for each
// Azure resource group.
// Returns the groups and a map of resource group name
// (lowered) -> group ID for membership wiring.
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
		if ps := rg.Properties.ProvisioningState; ps != "" {
			ext["provisioning_state"] = ps
		}
		if rg.ManagedBy != "" {
			ext["managed_by"] = rg.ManagedBy
		}
		g.Extensions = map[string]any{extensionNamespace: ext}
		groups = append(groups, g)
	}
	return groups, nameToID
}

// WireResourcesToResourceGroups assigns resources as members of their
// resource group.
// rgGroups are the resource groups of subscriptionID. A resource whose
// ARM ID belongs to another subscription (e.g. a stub for a remote
// peered VNet) is never a member, even when its resource group has the
// same name as a local one: resource group names are unique only
// within a subscription.
func WireResourcesToResourceGroups(resources []sdk.Resource, subscriptionID string, rgNameToGroupID map[string]string, rgGroups []sdk.Group) {
	idx := groupIndex(rgGroups)
	for _, r := range resources {
		if owner := extractSubscriptionID(r.Provider.NativeID); owner != "" && !strings.EqualFold(owner, subscriptionID) {
			continue
		}
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

// WireResourceGroupsToSubscription adds resource group group IDs as
// children of the subscription group.
func WireResourceGroupsToSubscription(subGroup *sdk.Group, rgGroups []sdk.Group) {
	for _, rg := range rgGroups {
		subGroup.AddChildren(rg.ID)
	}
}

// TransformRegionGroups builds one container.region group per distinct
// provider.region value found on the resources, membering every
// resource in that region. Region "global" and empty-region resources
// are skipped they are not geographically scoped. The boundary token is
// "<subscription-id>/<region>" so groups do not collide
// across subscriptions.
//
// container.region is not one of the standard group type families of
// OSIRIS JSON spec chapter 6 section 6.2.3; it is a custom group type,
// which consumers must accept per section 6.2.5.
// Region is a geographical dimension orthogonal to resource group
// membership: a resource is a member of both its resource group and
// its region group, and resource groups are never nested under
// region groups.
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

// enrichStubSubscriptionContext fills the provider context of stub
// resources (resources referenced from this subscription but owned
// by another subscription or not discovered here, e.g. a remote peered
// VNet) from the subscription entries of the management group entity
// list, which is authoritative for every subscription visible in
// the tenant.
//
// For each stub, provider.account is the owning subscription read
// from the stub's own ARM ID (set when empty). When that subscription
// is in the entity list, provider.tenant is set from it. Display names
// are not put on resources; they belong to the document's subscription
// catalog.
// Stubs are never made members of any group: the producer did not
// discover their resource group or management group placement, and
// must not imply it.
// Stubs with no matching entity get no tenant.
func enrichStubSubscriptionContext(stubs []sdk.Resource, entities []MGEntity) {
	subs := make(map[string]*MGEntity)
	for i := range entities {
		e := &entities[i]
		if strings.EqualFold(e.Type, "/subscriptions") && e.Name != "" {
			subs[strings.ToLower(e.Name)] = e
		}
	}
	for i := range stubs {
		p := &stubs[i].Provider
		if p.Account == "" {
			p.Account = extractSubscriptionID(p.NativeID)
		}
		p.Account = canonicalSubscriptionID(p.Account)
		if p.Account == "" {
			continue
		}
		if e, ok := subs[strings.ToLower(p.Account)]; ok && p.Tenant == "" && e.TenantID != "" {
			p.Tenant = e.TenantID
		}
	}
}

// buildMGAncestors extracts the management group ancestry chain for
// subscriptionID from the flat entity list returned by az account
// management-group entities list.
// Returns the ancestor MGEntities ordered root-to-leaf and the display
// path string (e.g. "Tenant Root Group > IT Hub > Production").
// Returns nil, "" when the subscription cannot be found or has
// no parent MGs.
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

	// A management group the caller cannot read is absent from the
	// entity list but still named in the subscription's own parent
	// chains. It is rebuilt from those chains rather than skipped:
	// skipping would attach its child to the grandparent and assert a
	// hierarchy Azure did not return.
	ancestors := make([]MGEntity, 0, len(subEntity.ParentNameChain))
	for i, name := range subEntity.ParentNameChain {
		if e, ok := byName[name]; ok {
			ancestors = append(ancestors, *e)
			continue
		}
		display := name
		if i < len(subEntity.ParentDisplayNameChain) && subEntity.ParentDisplayNameChain[i] != "" {
			display = subEntity.ParentDisplayNameChain[i]
		}
		ancestors = append(ancestors, MGEntity{
			ID:          mgARMIDPrefix + name,
			Name:        name,
			DisplayName: display,
			Type:        "Microsoft.Management/managementGroups",
			TenantID:    subEntity.TenantID,
		})
	}
	return ancestors, subEntity.ParentDisplayNameChain
}

// mgARMIDPrefix is the ARM resource ID prefix of a management group;
// the group name follows it.
const mgARMIDPrefix = "/providers/Microsoft.Management/managementGroups/"

// TransformManagementGroupGroups creates OSIRIS JSON logical.managementgroup
// groups for each management group in the ancestry chain. Groups are
// ordered root-to-leaf.
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

// setTenantName adds properties.tenant_name to every group whose
// properties.tenant_id is tenantID, when the tenant display name is
// known. Groups of another tenant, or an unknown name, are
// left unchanged.
func setTenantName(groups []sdk.Group, tenantID, tenantName string) {
	if tenantID == "" || tenantName == "" {
		return
	}
	for i := range groups {
		if id, _ := groups[i].Properties["tenant_id"].(string); strings.EqualFold(id, tenantID) {
			groups[i].Properties["tenant_name"] = tenantName
		}
	}
}

// WireMGHierarchy wires management group groups into a root-to-leaf
// parent-child chain and connects the subscription group as a child
// of the leaf (direct parent) MG.
// mgGroups must be ordered root-to-leaf. subGroup is the subscription
// group; the leaf MG gains subGroup.ID as a child so the full
// hierarchy is: Root MG -> ... -> Leaf MG -> Sub.
func WireMGHierarchy(mgGroups []sdk.Group, subGroup *sdk.Group) {
	if len(mgGroups) == 0 {
		return
	}
	for i := 0; i < len(mgGroups)-1; i++ {
		mgGroups[i].AddChildren(mgGroups[i+1].ID)
	}
	mgGroups[len(mgGroups)-1].AddChildren(subGroup.ID)
}

// groupIndex builds a map of group ID -> index in slice for
// efficient mutation.
func groupIndex(groups []sdk.Group) map[string]int {
	idx := make(map[string]int, len(groups))
	for i, g := range groups {
		idx[g.ID] = i
	}
	return idx
}

// raToMap converts a RoleAssignment to the canonical map shape used
// in group extensions.
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

// appendGroupRA appends a role-assignment map to the group's
// osiris.azure extension.
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
