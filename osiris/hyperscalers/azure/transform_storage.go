// transform_storage.go - Storage resource and connection transforms (Storage Account).
//
// For an introduction to OSIRIS JSON Producer for Microsoft Azure see:
// [OSIRIS-JSON-AZURE]: https://osirisjson.org/en/docs/producers/hyperscalers/microsoft-azure
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/docs/spec/v10/00-preface

package azure

import (
	"go.osirisjson.org/producers/pkg/sdk"
)

// TransformStorageAccounts converts Azure storage accounts into OSIRIS JSON
// resources of type osiris.azure.storage. Returns resources and the ARM ID ->
// resource ID map for wiring private endpoint connections.
func TransformStorageAccounts(accts []StorageAccount, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(accts))

	for _, a := range accts {
		id := resourceID("osiris.azure.storage", a.ID)
		idMap[a.ID] = id

		prov := azureProvider(a.ID, "Microsoft.Storage/storageAccounts", a.Location, sub)

		r, err := sdk.NewResource(id, "osiris.azure.storage", prov)
		if err != nil {
			continue
		}
		r.Name = a.Name
		r.Status = mapProvisioningState(a.ProvisioningState)
		r.State = a.ProvisioningState
		r.Tags = a.Tags

		props := map[string]any{
			"resource_group": a.ResourceGroup,
		}
		if a.Kind != "" {
			props["kind"] = a.Kind
		}
		if a.SKU.Name != "" {
			props["sku"] = a.SKU.Name
		}
		if a.SKU.Tier != "" {
			props["sku_tier"] = a.SKU.Tier
		}
		if a.AccessTier != "" {
			props["access_tier"] = a.AccessTier
		}
		props["https_only"] = a.EnableHTTPSTrafficOnly
		if a.MinimumTLSVersion != "" {
			props["min_tls_version"] = a.MinimumTLSVersion
		}
		if a.PublicNetworkAccess != "" {
			props["public_network_access"] = a.PublicNetworkAccess
		}
		if a.IsHnsEnabled {
			props["hierarchical_namespace"] = true
		}
		if a.AllowBlobPublicAccess != nil {
			props["allow_blob_public_access"] = *a.AllowBlobPublicAccess
		}
		if a.AllowSharedKeyAccess != nil {
			props["allow_shared_key_access"] = *a.AllowSharedKeyAccess
		}
		if a.AllowCrossTenantReplication != nil {
			props["allow_cross_tenant_replication"] = *a.AllowCrossTenantReplication
		}
		r.Properties = props

		// Extensions: Azure-specific fields (endpoints, network ACLs, encryption, PE IDs).
		ext := map[string]any{}
		if ep := a.PrimaryEndpoints; ep != nil {
			endpoints := map[string]any{}
			if ep.Blob != "" {
				endpoints["blob"] = ep.Blob
			}
			if ep.Queue != "" {
				endpoints["queue"] = ep.Queue
			}
			if ep.Table != "" {
				endpoints["table"] = ep.Table
			}
			if ep.File != "" {
				endpoints["file"] = ep.File
			}
			if ep.Web != "" {
				endpoints["web"] = ep.Web
			}
			if ep.Dfs != "" {
				endpoints["dfs"] = ep.Dfs
			}
			if len(endpoints) > 0 {
				ext["endpoints"] = endpoints
			}
		}
		if n := a.NetworkRuleSet; n != nil {
			acls := map[string]any{}
			if n.DefaultAction != "" {
				acls["default_action"] = n.DefaultAction
			}
			if n.Bypass != "" {
				acls["bypass"] = n.Bypass
			}
			if len(n.IPRules) > 0 {
				ips := make([]string, 0, len(n.IPRules))
				for _, rule := range n.IPRules {
					if rule.Value != "" {
						ips = append(ips, rule.Value)
					}
				}
				if len(ips) > 0 {
					acls["ip_rules"] = ips
				}
			}
			if len(n.VirtualNetworkRules) > 0 {
				subnets := make([]string, 0, len(n.VirtualNetworkRules))
				for _, rule := range n.VirtualNetworkRules {
					if rule.ID != "" {
						subnets = append(subnets, rule.ID)
					}
				}
				if len(subnets) > 0 {
					acls["vnet_subnet_ids"] = subnets
				}
			}
			if len(acls) > 0 {
				ext["network_acls"] = acls
			}
		}
		if e := a.Encryption; e != nil {
			enc := map[string]any{}
			if e.KeySource != "" {
				enc["key_source"] = e.KeySource
			}
			if kv := e.KeyVaultProperties; kv != nil && kv.KeyVaultURI != "" {
				enc["keyvault_uri"] = kv.KeyVaultURI
				if kv.KeyName != "" {
					enc["keyvault_key_name"] = kv.KeyName
				}
			}
			if len(enc) > 0 {
				ext["encryption"] = enc
			}
		}
		if peIDs := collectPEIDs(a.PrivateEndpointConnections); len(peIDs) > 0 {
			ext["private_endpoint_ids"] = peIDs
		}
		if a.MinimumTLSVersion != "" {
			ext["min_tls_version"] = a.MinimumTLSVersion
		}
		if a.NetworkRuleSet != nil && a.NetworkRuleSet.DefaultAction != "" {
			ext["default_action"] = a.NetworkRuleSet.DefaultAction
		}
		if len(ext) > 0 {
			r.Extensions = map[string]any{extensionNamespace: ext}
		}

		attachArmBody(&r, &a)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformPEToStorageConnections creates network connections from each
// private endpoint to the storage account it fronts. The binding lives on the storage account's privateEndpointConnections array.
func TransformPEToStorageConnections(accts []StorageAccount, storageIDMap, peIDMap map[string]string) []sdk.Connection {
	return transformPEBoundConnections(
		collectPEBindings(func(yield func(targetArmID string, conns []azPrivateEndpointConnRef, name string)) {
			for _, a := range accts {
				yield(a.ID, a.PrivateEndpointConnections, a.Name)
			}
		}),
		storageIDMap, peIDMap, "dependency.storage",
	)
}
