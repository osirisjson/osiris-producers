// transform_recovery.go - Recovery resource and connection transforms (Recovery Services Vault, Backup Vault).
//
// For an introduction to OSIRIS JSON Producer for Microsoft Azure see:
// [OSIRIS-JSON-AZURE]: https://osirisjson.org/en/docs/producers/hyperscalers/microsoft-azure
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/docs/spec/v10/00-preface

package azure

import (
	"fmt"

	"go.osirisjson.org/producers/pkg/sdk"
)

// TransformRecoveryServicesVaults converts Microsoft.RecoveryServices/vaults
// into OSIRIS JSON resources of type osiris.azure.recoveryservicesvault.
// Returns resources and ARM ID -> resource ID map for wiring private endpoint
// connections and protected-item edges.
func TransformRecoveryServicesVaults(vaults []RecoveryServicesVault, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(vaults))

	for _, v := range vaults {
		id := resourceID("osiris.azure.recoveryservicesvault", v.ID)
		idMap[v.ID] = id

		prov := azureProvider(v.ID, "Microsoft.RecoveryServices/vaults", v.Location, sub)

		r, err := sdk.NewResource(id, "osiris.azure.recoveryservicesvault", prov)
		if err != nil {
			continue
		}
		r.Name = v.Name
		r.Tags = v.Tags

		props := map[string]any{
			"resource_group": v.ResourceGroup,
		}
		if v.SKU != nil && v.SKU.Name != "" {
			props["sku"] = v.SKU.Name
		}
		if v.SKU != nil && v.SKU.Tier != "" {
			props["sku_tier"] = v.SKU.Tier
		}

		var peIDs []string
		if p := v.Properties; p != nil {
			r.Status = mapProvisioningState(p.ProvisioningState)
			r.State = p.ProvisioningState
			if p.PublicNetworkAccess != "" {
				props["public_network_access"] = p.PublicNetworkAccess
			}
			if p.RedundancySettings != nil {
				if p.RedundancySettings.StandardTierStorageRedundancy != "" {
					props["storage_redundancy"] = p.RedundancySettings.StandardTierStorageRedundancy
				}
				if p.RedundancySettings.CrossRegionRestore != "" {
					props["cross_region_restore"] = p.RedundancySettings.CrossRegionRestore
				}
			}
			peIDs = collectPEIDs(p.PrivateEndpointConnections)
		} else {
			r.Status = "active"
			r.State = "Succeeded"
		}
		props["protected_item_count"] = len(v.ProtectedItems)
		r.Properties = props

		if len(peIDs) > 0 {
			r.Extensions = map[string]any{extensionNamespace: map[string]any{"private_endpoint_ids": peIDs}}
		}

		attachArmBody(&r, &v)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformBackupVaults converts Microsoft.DataProtection/backupVaults into
// OSIRIS JSON resources of type osiris.azure.backupvault. Returns resources
// and ARM ID -> resource ID map for wiring backup-instance edges.
func TransformBackupVaults(vaults []BackupVault, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(vaults))

	for _, v := range vaults {
		id := resourceID("osiris.azure.backupvault", v.ID)
		idMap[v.ID] = id

		prov := azureProvider(v.ID, "Microsoft.DataProtection/backupVaults", v.Location, sub)

		r, err := sdk.NewResource(id, "osiris.azure.backupvault", prov)
		if err != nil {
			continue
		}
		r.Name = v.Name
		r.Tags = v.Tags

		props := map[string]any{
			"resource_group": v.ResourceGroup,
		}

		if p := v.Properties; p != nil {
			r.Status = mapProvisioningState(p.ProvisioningState)
			r.State = p.ProvisioningState
			if len(p.StorageSettings) > 0 {
				storages := make([]map[string]any, 0, len(p.StorageSettings))
				for _, s := range p.StorageSettings {
					entry := map[string]any{}
					if s.DatastoreType != "" {
						entry["datastore_type"] = s.DatastoreType
					}
					if s.Type != "" {
						entry["redundancy"] = s.Type
					}
					storages = append(storages, entry)
				}
				props["storage_settings"] = storages
			}
			if p.SecuritySettings != nil {
				if p.SecuritySettings.ImmutabilitySettings != nil && p.SecuritySettings.ImmutabilitySettings.State != "" {
					props["immutability"] = p.SecuritySettings.ImmutabilitySettings.State
				}
				if p.SecuritySettings.SoftDeleteSettings != nil && p.SecuritySettings.SoftDeleteSettings.State != "" {
					props["soft_delete"] = p.SecuritySettings.SoftDeleteSettings.State
					if p.SecuritySettings.SoftDeleteSettings.RetentionDurationInDays > 0 {
						props["soft_delete_retention_days"] = p.SecuritySettings.SoftDeleteSettings.RetentionDurationInDays
					}
				}
			}
		} else {
			r.Status = "active"
			r.State = "Succeeded"
		}
		props["backup_instance_count"] = len(v.ProtectedInstances)
		r.Properties = props

		attachArmBody(&r, &v)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformBackupProtectedItemConnections wires each backed-up resource to
// its Recovery Services Vault via a "network" edge (backup data flow).
// Protected items whose SourceResourceID is not among collected resources
// are emitted as stub edges only when the source resource is known; unknown
// sources are skipped silently (the backed-up resource may be in another
// subscription or outside collection scope).
//
// resourceIDMap carries the merged ARM ID -> OSIRIS JSON resource ID for all
// possible protected resource types (VM, SQL server, file share in storage, managed disk, etc.).
func TransformBackupProtectedItemConnections(vaults []RecoveryServicesVault, rsvIDMap, resourceIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	seen := map[string]bool{}
	for _, v := range vaults {
		vaultID, ok := rsvIDMap[v.ID]
		if !ok {
			continue
		}
		for _, item := range v.ProtectedItems {
			srcArm := item.SourceResourceID()
			if srcArm == "" {
				continue
			}
			srcID, ok := resourceIDMap[srcArm]
			if !ok {
				continue
			}
			pairKey := srcID + "|" + vaultID
			if seen[pairKey] {
				continue
			}
			seen[pairKey] = true

			canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
				Type:      "network",
				Direction: "forward",
				Source:    srcID,
				Target:    vaultID,
			})
			connID := sdk.BuildConnectionID(canonicalKey, 16)

			conn, err := sdk.NewConnection(connID, "network", srcID, vaultID)
			if err != nil {
				continue
			}
			conn.Name = fmt.Sprintf("%s -> %s", extractLastSegment(srcArm), v.Name)
			_ = conn.SetDirection("forward")
			connections = append(connections, conn)
		}
	}
	return connections
}

// TransformBackupInstanceConnections wires each backup instance in a Backup
// Vault to its source resource via a "network" edge. Same semantics as
// TransformBackupProtectedItemConnections but for the DataProtection service.
func TransformBackupInstanceConnections(vaults []BackupVault, bvIDMap, resourceIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	seen := map[string]bool{}
	for _, v := range vaults {
		vaultID, ok := bvIDMap[v.ID]
		if !ok {
			continue
		}
		for _, inst := range v.ProtectedInstances {
			srcArm := inst.SourceResourceID()
			if srcArm == "" {
				continue
			}
			srcID, ok := resourceIDMap[srcArm]
			if !ok {
				continue
			}
			pairKey := srcID + "|" + vaultID
			if seen[pairKey] {
				continue
			}
			seen[pairKey] = true

			canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
				Type:      "network",
				Direction: "forward",
				Source:    srcID,
				Target:    vaultID,
			})
			connID := sdk.BuildConnectionID(canonicalKey, 16)

			conn, err := sdk.NewConnection(connID, "network", srcID, vaultID)
			if err != nil {
				continue
			}
			conn.Name = fmt.Sprintf("%s -> %s", extractLastSegment(srcArm), v.Name)
			_ = conn.SetDirection("forward")
			connections = append(connections, conn)
		}
	}
	return connections
}

// TransformPEToRecoveryServicesVaultConnections wires Private Endpoints to
// Recovery Services Vaults via the vault's privateEndpointConnections.
func TransformPEToRecoveryServicesVaultConnections(vaults []RecoveryServicesVault, rsvIDMap, peIDMap map[string]string) []sdk.Connection {
	bindings := make([]peBinding, 0, len(vaults))
	for _, v := range vaults {
		if v.Properties == nil {
			continue
		}
		bindings = append(bindings, peBinding{
			TargetArmID: v.ID,
			Name:        v.Name,
			Conns:       v.Properties.PrivateEndpointConnections,
		})
	}
	return transformPEBoundConnections(bindings, rsvIDMap, peIDMap, "dependency")
}
