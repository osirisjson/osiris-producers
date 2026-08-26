// transform_security.go - Security resource and connection transforms
// (Key Vault, Container Registry).
//
// For an introduction to OSIRIS JSON Producer for Microsoft Azure see:
// [OSIRIS-JSON-AZURE]: https://docs.osirisjson.org/osiris-producers/hyperscalers/microsoft-azure/
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package azure

import (
	"strings"

	"go.osirisjson.org/producers/pkg/sdk"
)

// TransformKeyVaults converts Azure Key Vaults into OSIRIS JSON resources of
// type osiris.azure.keyvault. Returns resources and ARM ID -> resource ID map.
func TransformKeyVaults(vaults []KeyVault, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(vaults))

	for _, v := range vaults {
		id := resourceID("osiris.azure.keyvault", v.ID)
		idMap[v.ID] = id

		prov := azureProvider(v.ID, "Microsoft.KeyVault/vaults", v.Location, sub)

		r, err := sdk.NewResource(id, "osiris.azure.keyvault", prov)
		if err != nil {
			continue
		}
		r.Name = v.Name
		r.Status = "active"
		r.State = "Succeeded"
		r.Tags = v.Tags

		props := map[string]any{
			"resource_group": v.ResourceGroup,
		}
		ext := map[string]any{}

		p := v.Properties
		if p != nil {
			r.Status = mapProvisioningState(p.ProvisioningState)
			r.State = p.ProvisioningState
			if p.SKU.Name != "" {
				props["sku"] = p.SKU.Name
			}
			if p.SKU.Family != "" {
				props["sku_family"] = p.SKU.Family
			}
			if p.VaultURI != "" {
				props["vault_uri"] = p.VaultURI
			}
			if p.EnableRbacAuthorization {
				props["rbac_enabled"] = true
			}
			if p.EnableSoftDelete != nil {
				props["soft_delete_enabled"] = *p.EnableSoftDelete
			}
			if p.SoftDeleteRetentionInDays > 0 {
				props["soft_delete_retention_days"] = p.SoftDeleteRetentionInDays
			}
			if p.EnablePurgeProtection != nil {
				props["purge_protection_enabled"] = *p.EnablePurgeProtection
			}
			if p.EnabledForDeployment {
				props["enabled_for_deployment"] = true
			}
			if p.EnabledForDiskEncryption {
				props["enabled_for_disk_encryption"] = true
			}
			if p.EnabledForTemplateDeployment {
				props["enabled_for_template_deployment"] = true
			}
			if p.PublicNetworkAccess != "" {
				props["public_network_access"] = p.PublicNetworkAccess
			}
			if p.MinimumTLSVersion != "" {
				props["min_tls_version"] = p.MinimumTLSVersion
				ext["min_tls_version"] = p.MinimumTLSVersion
			}
			if n := p.NetworkACLs; n != nil {
				// Build the ACL map once and write it to both props and ext.
				acls := map[string]any{}
				if n.DefaultAction != "" {
					acls["default_action"] = n.DefaultAction
					ext["default_action"] = n.DefaultAction
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
					props["network_acls"] = acls
					ext["network_acls"] = acls
				}
			} else if _, hasDA := ext["default_action"]; !hasDA {
				// No explicit ACL configured: derive default_action from publicNetworkAccess
				// so all vaults carry a consistent network policy signal.
				if p.PublicNetworkAccess != "" {
					ext["default_action"] = publicNetworkAccessToDefaultAction(p.PublicNetworkAccess)
				}
			}
			if p.TenantID != "" {
				ext["tenant_id"] = p.TenantID
			}
			if peIDs := collectPEIDs(p.PrivateEndpointConnections); len(peIDs) > 0 {
				ext["private_endpoint_ids"] = peIDs
			}
		}
		r.Properties = props
		if len(ext) > 0 {
			r.Extensions = map[string]any{extensionNamespace: ext}
		}

		attachArmBody(&r, &v)
		resources = append(resources, r)
	}
	return resources, idMap
}

// publicNetworkAccessToDefaultAction maps Azure's publicNetworkAccess flag to a
// canonical network_acls default_action value, consistent with storage accounts.
func publicNetworkAccessToDefaultAction(pna string) string {
	if strings.EqualFold(pna, "Disabled") {
		return "Deny"
	}
	return "Allow"
}

// TransformContainerRegistries converts Azure Container Registries into
// OSIRIS JSON resources of type osiris.azure.containerregistry.
func TransformContainerRegistries(regs []ContainerRegistry, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(regs))

	for _, reg := range regs {
		id := resourceID("osiris.azure.containerregistry", reg.ID)
		idMap[reg.ID] = id

		prov := azureProvider(reg.ID, "Microsoft.ContainerRegistry/registries", reg.Location, sub)

		r, err := sdk.NewResource(id, "osiris.azure.containerregistry", prov)
		if err != nil {
			continue
		}
		r.Name = reg.Name
		r.Status = mapProvisioningState(reg.ProvisioningState)
		r.State = reg.ProvisioningState
		r.Tags = reg.Tags

		props := map[string]any{
			"resource_group": reg.ResourceGroup,
		}
		if reg.SKU.Name != "" {
			props["sku"] = reg.SKU.Name
		}
		if reg.SKU.Tier != "" {
			props["sku_tier"] = reg.SKU.Tier
		}
		if reg.LoginServer != "" {
			props["login_server"] = reg.LoginServer
		}
		if reg.AdminUserEnabled {
			props["admin_user_enabled"] = true
		}
		if reg.AnonymousPullEnabled {
			props["anonymous_pull_enabled"] = true
		}
		if reg.DataEndpointEnabled {
			props["data_endpoint_enabled"] = true
		}
		if reg.PublicNetworkAccess != "" {
			props["public_network_access"] = reg.PublicNetworkAccess
		}
		if reg.ZoneRedundancy != "" && strings.EqualFold(reg.ZoneRedundancy, "Enabled") {
			props["zone_redundant"] = true
		}
		r.Properties = props

		ext := map[string]any{}
		if peIDs := collectPEIDs(reg.PrivateEndpointConnections); len(peIDs) > 0 {
			ext["private_endpoint_ids"] = peIDs
		}
		if len(reg.Replications) > 0 {
			repls := make([]map[string]any, 0, len(reg.Replications))
			for _, repl := range reg.Replications {
				rm := map[string]any{
					"name":     repl.Name,
					"location": repl.Location,
					"status":   mapProvisioningState(repl.ProvisioningState),
				}
				if repl.ZoneRedundancy != "" && strings.EqualFold(repl.ZoneRedundancy, "Enabled") {
					rm["zone_redundant"] = true
				}
				if repl.RegionEndpoint {
					rm["region_endpoint"] = true
				}
				repls = append(repls, rm)
			}
			ext["replications"] = repls
		}
		if len(ext) > 0 {
			r.Extensions = map[string]any{extensionNamespace: ext}
		}

		attachArmBody(&r, &reg)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformPEToKeyVaultConnections mirrors TransformPEToStorageConnections
// for Key Vaults. The PE list lives under properties.privateEndpointConnections.
func TransformPEToKeyVaultConnections(vaults []KeyVault, kvIDMap, peIDMap map[string]string) []sdk.Connection {
	return transformPEBoundConnections(
		collectPEBindings(func(yield func(targetArmID string, conns []azPrivateEndpointConnRef, name string)) {
			for _, v := range vaults {
				var conns []azPrivateEndpointConnRef
				if v.Properties != nil {
					conns = v.Properties.PrivateEndpointConnections
				}
				yield(v.ID, conns, v.Name)
			}
		}),
		kvIDMap, peIDMap, "dependency",
	)
}

// TransformPEToContainerRegistryConnections mirrors the pattern for ACR.
func TransformPEToContainerRegistryConnections(regs []ContainerRegistry, acrIDMap, peIDMap map[string]string) []sdk.Connection {
	return transformPEBoundConnections(
		collectPEBindings(func(yield func(targetArmID string, conns []azPrivateEndpointConnRef, name string)) {
			for _, r := range regs {
				yield(r.ID, r.PrivateEndpointConnections, r.Name)
			}
		}),
		acrIDMap, peIDMap, "dependency",
	)
}
