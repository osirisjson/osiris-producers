// transform_identity.go - Identity resource transforms (Managed Identity).
//
// For an introduction to OSIRIS JSON Producer for Microsoft Azure see:
// [OSIRIS-JSON-AZURE]: https://osirisjson.org/en/docs/producers/hyperscalers/microsoft-azure
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/docs/spec/v10/00-preface

package azure

import (
	"go.osirisjson.org/producers/pkg/sdk"
)

// TransformManagedIdentities converts Azure User-Assigned Managed Identities
// into OSIRIS JSON resources of type osiris.azure.managedidentity. Returns
// resources and ARM ID -> resource ID map so webapps/VMs can reference them.
func TransformManagedIdentities(ids []ManagedIdentity, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(ids))

	for _, mi := range ids {
		id := resourceID("osiris.azure.managedidentity", mi.ID)
		idMap[mi.ID] = id

		prov := azureProvider(mi.ID, "Microsoft.ManagedIdentity/userAssignedIdentities", mi.Location, sub)

		r, err := sdk.NewResource(id, "osiris.azure.managedidentity", prov)
		if err != nil {
			continue
		}
		r.Name = mi.Name
		miPS := mi.ProvisioningState
		if miPS == "" {
			miPS = "Succeeded"
		}
		r.Status = mapProvisioningState(miPS)
		r.State = miPS
		r.Tags = mi.Tags
		props := map[string]any{
			"resource_group": mi.ResourceGroup,
		}
		if mi.PrincipalID != "" {
			props["principal_id"] = mi.PrincipalID
		}
		if mi.ClientID != "" {
			props["client_id"] = mi.ClientID
		}
		if mi.TenantID != "" {
			props["tenant_id"] = mi.TenantID
		}
		r.Properties = props

		attachArmBody(&r, &mi)
		resources = append(resources, r)
	}
	return resources, idMap
}
