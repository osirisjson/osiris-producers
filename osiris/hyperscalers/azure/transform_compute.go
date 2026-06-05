// transform_compute.go - Compute resource and connection transforms (VM, Disk, Snapshot).
//
// For an introduction to OSIRIS JSON Producer for Microsoft Azure see:
// [OSIRIS-JSON-AZURE]: https://osirisjson.org/en/docs/producers/hyperscalers/microsoft-azure
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/docs/spec/v10/00-preface

package azure

import (
	"fmt"
	"strings"

	"go.osirisjson.org/producers/pkg/sdk"
)

// TransformDisks converts Azure managed disks into OSIRIS JSON resources of
// type osiris.azure.disk. Returns resources and ARM ID -> resource ID map so
// snapshots can reference the source disk.
func TransformDisks(disks []Disk, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(disks))

	for _, d := range disks {
		id := resourceID("osiris.azure.disk", d.ID)
		idMap[d.ID] = id

		prov := azureProvider(d.ID, "Microsoft.Compute/disks", d.Location, sub)
		if len(d.Zones) > 0 {
			prov.Zone = strings.Join(d.Zones, ",")
		}

		r, err := sdk.NewResource(id, "osiris.azure.disk", prov)
		if err != nil {
			continue
		}
		r.Name = d.Name
		r.Status = mapDiskState(d.DiskState, d.ProvisioningState)
		r.State = d.DiskState
		r.Tags = d.Tags

		props := map[string]any{
			"resource_group": d.ResourceGroup,
		}
		if d.SKU.Name != "" {
			props["sku"] = d.SKU.Name
		}
		if d.SKU.Tier != "" {
			props["sku_tier"] = d.SKU.Tier
		}
		if d.DiskSizeGB > 0 {
			props["size_gb"] = d.DiskSizeGB
		}
		if d.DiskIOPSReadWrite > 0 {
			props["iops"] = d.DiskIOPSReadWrite
		}
		if d.DiskMBPSReadWrite > 0 {
			props["mbps"] = d.DiskMBPSReadWrite
		}
		if d.DiskState != "" {
			props["disk_state"] = d.DiskState
		}
		if d.OSType != "" {
			props["os_type"] = d.OSType
		}
		if d.ManagedBy != "" {
			props["managed_by"] = d.ManagedBy
		}
		if len(d.Zones) > 0 {
			props["zones"] = d.Zones
		}
		r.Properties = props

		attachArmBody(&r, &d)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformSnapshots converts Azure disk snapshots into OSIRIS JSON resources
// of type osiris.azure.snapshot.
func TransformSnapshots(snaps []Snapshot, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(snaps))

	for _, s := range snaps {
		id := resourceID("osiris.azure.snapshot", s.ID)
		idMap[s.ID] = id

		prov := azureProvider(s.ID, "Microsoft.Compute/snapshots", s.Location, sub)

		r, err := sdk.NewResource(id, "osiris.azure.snapshot", prov)
		if err != nil {
			continue
		}
		r.Name = s.Name
		r.Status = mapProvisioningState(s.ProvisioningState)
		r.State = s.ProvisioningState
		r.Tags = s.Tags

		props := map[string]any{
			"resource_group": s.ResourceGroup,
		}
		if s.SKU.Name != "" {
			props["sku"] = s.SKU.Name
		}
		if s.DiskSizeGB > 0 {
			props["size_gb"] = s.DiskSizeGB
		}
		if s.Incremental {
			props["incremental"] = true
		}
		if s.OSType != "" {
			props["os_type"] = s.OSType
		}
		if cd := s.CreationData; cd != nil {
			if cd.CreateOption != "" {
				props["create_option"] = cd.CreateOption
			}
			if cd.SourceResourceID != "" {
				props["source_resource_id"] = cd.SourceResourceID
			}
		}
		r.Properties = props

		attachArmBody(&r, &s)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformVMs converts Azure VirtualMachines into OSIRIS JSON resources.
func TransformVMs(vms []VirtualMachine, sub SubscriptionInfo) []sdk.Resource {
	var resources []sdk.Resource
	for _, vm := range vms {
		id := resourceID("compute.vm", vm.ID)
		prov := azureProvider(vm.ID, "Microsoft.Compute/virtualMachines", vm.Location, sub)
		if len(vm.Zones) > 0 {
			prov.Zone = strings.Join(vm.Zones, ",")
		}

		r, err := sdk.NewResource(id, "compute.vm", prov)
		if err != nil {
			continue
		}
		r.Name = vm.Name
		r.Status = mapVMPowerState(vm.PowerState)
		r.State = vm.ProvisioningState
		r.Tags = vm.Tags

		props := map[string]any{
			"resource_group": vm.ResourceGroup,
		}
		// vmSize: prefer top-level (az vm list -d flattens it), fall back to nested hardwareProfile.
		vmSize := vm.VMSize
		if vmSize == "" {
			vmSize = vm.HardwareProfile.VMSize
		}
		if vmSize != "" {
			props["vm_size"] = vmSize
		}
		if vm.ProvisioningState != "" {
			props["provisioning_state"] = strings.ToLower(vm.ProvisioningState)
		}
		if vm.VMId != "" {
			props["vm_id"] = vm.VMId
		}
		if vm.LicenseType != "" {
			props["license_type"] = vm.LicenseType
		}
		if osType := vm.StorageProfile.OSDisk.OSType; osType != "" {
			props["os_type"] = strings.ToLower(osType)
		}
		if ref := vm.StorageProfile.ImageReference; ref.Publisher != "" {
			props["image_publisher"] = ref.Publisher
			props["image_offer"] = ref.Offer
			props["image_sku"] = ref.Sku
			props["image_version"] = ref.Version
		}
		if vm.OsProfile.ComputerName != "" {
			props["computer_name"] = vm.OsProfile.ComputerName
		}
		if nicCount := len(vm.NetworkProfile.NetworkInterfaces); nicCount > 0 {
			props["nic_count"] = nicCount
		}
		r.Properties = props
		if len(vm.VMExtensions) > 0 {
			exts := make([]map[string]any, 0, len(vm.VMExtensions))
			for _, e := range vm.VMExtensions {
				em := map[string]any{
					"name":      e.Name,
					"publisher": e.Publisher,
				}
				if e.ExtType != "" {
					em["type"] = e.ExtType
				}
				if e.TypeHandlerVersion != "" {
					em["version"] = e.TypeHandlerVersion
				}
				if e.ProvisioningState != "" {
					em["status"] = strings.ToLower(e.ProvisioningState)
				}
				em["auto_upgrade_minor"] = e.AutoUpgradeMinorVersion
				if e.EnableAutomaticUpgrade != nil {
					em["auto_upgrade"] = *e.EnableAutomaticUpgrade
				}
				exts = append(exts, em)
			}
			r.Extensions = map[string]any{extensionNamespace: map[string]any{"vm_extensions": exts}}
		}
		attachArmBody(&r, &vm)
		resources = append(resources, r)
	}
	return resources
}

// TransformSnapshotToDiskConnections wires each snapshot back to the disk
// (or other snapshot) it was taken from via creationData.sourceResourceId.
// Uses the "contains" type with direction=reverse so the topology reads
// "snapshot of disk X" rather than "disk X contains snapshot".
func TransformSnapshotToDiskConnections(snaps []Snapshot, snapshotIDMap, diskIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, s := range snaps {
		if s.CreationData == nil || s.CreationData.SourceResourceID == "" {
			continue
		}
		sourceID, ok := snapshotIDMap[s.ID]
		if !ok {
			continue
		}
		// Source can be a disk OR another snapshot (chained snapshots).
		targetID, ok := diskIDMap[s.CreationData.SourceResourceID]
		if !ok {
			targetID, ok = snapshotIDMap[s.CreationData.SourceResourceID]
			if !ok {
				continue
			}
		}

		canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
			Type:      "contains",
			Direction: "reverse",
			Source:    sourceID,
			Target:    targetID,
		})
		connID := sdk.BuildConnectionID(canonicalKey, 16)

		conn, err := sdk.NewConnection(connID, "contains", sourceID, targetID)
		if err != nil {
			continue
		}
		conn.Name = fmt.Sprintf("%s -> %s", s.Name, extractLastSegment(s.CreationData.SourceResourceID))
		_ = conn.SetDirection("reverse")
		connections = append(connections, conn)
	}
	return connections
}

// TransformDiskToVMConnections wires each managed disk to the VM that has
// attached it (via managedBy on the disk). Emits as contains/reverse so the
// topology reads "disk attached to VM".
func TransformDiskToVMConnections(disks []Disk, diskIDMap, vmIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, d := range disks {
		if d.ManagedBy == "" {
			continue
		}
		sourceID, ok := diskIDMap[d.ID]
		if !ok {
			continue
		}
		targetID, ok := vmIDMap[d.ManagedBy]
		if !ok {
			continue
		}

		canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
			Type:      "contains",
			Direction: "reverse",
			Source:    sourceID,
			Target:    targetID,
		})
		connID := sdk.BuildConnectionID(canonicalKey, 16)

		conn, err := sdk.NewConnection(connID, "contains", sourceID, targetID)
		if err != nil {
			continue
		}
		conn.Name = fmt.Sprintf("%s -> %s", d.Name, extractLastSegment(d.ManagedBy))
		_ = conn.SetDirection("reverse")
		connections = append(connections, conn)
	}
	return connections
}

// OSIRIS JSON Helpers
// mapVMPowerState converts Azure VM power state to OSIRIS JSON status.
func mapVMPowerState(state string) string {
	lower := strings.ToLower(state)
	switch {
	case strings.Contains(lower, "running"):
		return "active"
	case strings.Contains(lower, "deallocat"):
		return "inactive"
	case strings.Contains(lower, "stopped"):
		return "inactive"
	default:
		return "unknown"
	}
}

// mapDiskState converts an Azure managed-disk diskState to an OSIRIS JSON status.
// ActiveSAS / Attached / ReadyToUpload all mean the disk is serving a client.
func mapDiskState(diskState, provisioningState string) string {
	switch strings.ToLower(diskState) {
	case "attached", "activesas", "activeupload", "readytoupload":
		return "active"
	case "unattached", "reserved":
		return "inactive"
	}
	return mapProvisioningState(provisioningState)
}

// BuildVMIDMap builds a map of VM ARM ID -> OSIRIS JSON resource ID, used for
// wiring disks -> VM via the disk's managedBy field.
func BuildVMIDMap(vms []VirtualMachine) map[string]string {
	m := make(map[string]string, len(vms))
	for _, vm := range vms {
		m[vm.ID] = resourceID("compute.vm", vm.ID)
	}
	return m
}
