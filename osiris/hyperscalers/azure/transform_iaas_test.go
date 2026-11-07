// transform_iaas_test.go - Positive and negative relationship tests for
// the core IaaS topology of the OSIRIS JSON Microsoft Azure producer:
// VM -> NIC, VM -> Disk, NIC -> NSG and NIC -> Public IP.
//
// Every positive case names the ARM field used as evidence.
// Every negative case removes that field while keeping resource names
// correlated, and asserts that no edge is inferred from names alone.

package azure

import "testing"

const (
	tSub    = "/subscriptions/00000000-0000-0000-0000-000000000001"
	tVM1    = tSub + "/resourceGroups/RG-LAB/providers/Microsoft.Compute/virtualMachines/LAB-VM01"
	tVM2    = tSub + "/resourceGroups/RG-LAB/providers/Microsoft.Compute/virtualMachines/LAB-VM02"
	tNIC1   = tSub + "/resourceGroups/RG-LAB/providers/Microsoft.Network/networkInterfaces/LAB-VM01-nic"
	tNIC2   = tSub + "/resourceGroups/RG-LAB/providers/Microsoft.Network/networkInterfaces/LAB-VM02-nic"
	tOSDisk = tSub + "/resourceGroups/RG-LAB/providers/Microsoft.Compute/disks/LAB-VM01-os"
	tData   = tSub + "/resourceGroups/RG-LAB/providers/Microsoft.Compute/disks/LAB-VM01-data"
	tNSG    = tSub + "/resourceGroups/RG-LAB/providers/Microsoft.Network/networkSecurityGroups/LAB-NSG01"
	tPIP    = tSub + "/resourceGroups/RG-LAB/providers/Microsoft.Network/publicIPAddresses/LAB-PIP01"
)

// lowerRG rewrites the resource group segment the way some ARM APIs
// return it ("resourcegroups/rg-lab"), to exercise case-insensitive
// resolution.
func lowerRG(id string) string {
	const from, to = "/resourceGroups/RG-LAB/", "/resourcegroups/rg-lab/"
	for i := 0; i+len(from) <= len(id); i++ {
		if id[i:i+len(from)] == from {
			return id[:i] + to + id[i+len(from):]
		}
	}
	return id
}

func idMapOf(ids ...string) map[string]string {
	m := make(map[string]string, len(ids))
	for _, id := range ids {
		m[armKey(id)] = resourceID("", id)
	}
	return m
}

func TestTransformVMToNICConnections(t *testing.T) {
	vmIDMap := idMapOf(tVM1, tVM2)
	nicIDMap := idMapOf(tNIC1, tNIC2)

	t.Run("evidence from VM networkProfile and NIC virtualMachine is one edge", func(t *testing.T) {
		vms := []VirtualMachine{{ID: tVM1, NetworkProfile: azVMNetworkProfile{NetworkInterfaces: []azVMNICRef{{ID: tNIC1}}}}}
		nics := []NetworkInterface{{ID: tNIC1, VirtualMachine: &azNICVMRef{ID: lowerRG(tVM1)}}}
		conns := TransformVMToNICConnections(vms, nics, vmIDMap, nicIDMap)
		if len(conns) != 1 {
			t.Fatalf("expected 1 connection, got %d", len(conns))
		}
		c := conns[0]
		if c.Type != "contains" || c.Direction != "forward" {
			t.Errorf("expected contains/forward, got %s/%s", c.Type, c.Direction)
		}
		if c.Source != "azure::"+tVM1 || c.Target != "azure::"+tNIC1 {
			t.Errorf("expected VM -> NIC, got %q -> %q", c.Source, c.Target)
		}
	})

	t.Run("NIC virtualMachine alone is sufficient evidence", func(t *testing.T) {
		nics := []NetworkInterface{{ID: tNIC2, VirtualMachine: &azNICVMRef{ID: tVM2}}}
		conns := TransformVMToNICConnections(nil, nics, vmIDMap, nicIDMap)
		if len(conns) != 1 || conns[0].Source != "azure::"+tVM2 {
			t.Fatalf("expected VM02 -> NIC02, got %+v", conns)
		}
	})

	t.Run("correlated names without ARM reference produce no edge", func(t *testing.T) {
		vms := []VirtualMachine{{ID: tVM1, Name: "LAB-VM01"}}
		nics := []NetworkInterface{{ID: tNIC1, Name: "LAB-VM01-nic"}}
		if conns := TransformVMToNICConnections(vms, nics, vmIDMap, nicIDMap); len(conns) != 0 {
			t.Fatalf("expected no inferred edge, got %d", len(conns))
		}
	})

	t.Run("reference to a NIC outside the document produces no edge", func(t *testing.T) {
		vms := []VirtualMachine{{ID: tVM1, NetworkProfile: azVMNetworkProfile{NetworkInterfaces: []azVMNICRef{{ID: tSub + "/resourceGroups/RG-OTHER/providers/Microsoft.Network/networkInterfaces/x"}}}}}
		if conns := TransformVMToNICConnections(vms, nil, vmIDMap, nicIDMap); len(conns) != 0 {
			t.Fatalf("expected no dangling edge, got %d", len(conns))
		}
	})
}

func TestTransformVMToDiskConnections(t *testing.T) {
	vmIDMap := idMapOf(tVM1)
	diskIDMap := idMapOf(tOSDisk, tData)

	t.Run("storage profile and managedBy collapse to one edge per disk", func(t *testing.T) {
		vms := []VirtualMachine{{
			ID: tVM1,
			StorageProfile: azVMStorageProfile{
				OSDisk:    azVMOSDisk{ManagedDisk: &azVMManagedDiskRef{ID: tOSDisk}},
				DataDisks: []azVMDataDisk{{Lun: 2, ManagedDisk: &azVMManagedDiskRef{ID: tData}}},
			},
		}}
		disks := []Disk{
			{ID: tOSDisk, ManagedBy: lowerRG(tVM1)},
			{ID: tData, ManagedBy: lowerRG(tVM1)},
		}
		conns := TransformVMToDiskConnections(vms, disks, vmIDMap, diskIDMap)
		if len(conns) != 2 {
			t.Fatalf("expected 2 connections, got %d", len(conns))
		}
		for _, c := range conns {
			if c.Type != "dependency.storage" || c.Direction != "forward" || c.Source != "azure::"+tVM1 {
				t.Errorf("expected VM -> disk dependency.storage/forward, got %s/%s from %q", c.Type, c.Direction, c.Source)
			}
		}
		if conns[0].Properties["disk_role"] != "os" {
			t.Errorf("expected os disk role, got %v", conns[0].Properties)
		}
		if conns[1].Properties["disk_role"] != "data" || conns[1].Properties["lun"] != 2 {
			t.Errorf("expected data disk role with lun 2, got %v", conns[1].Properties)
		}
	})

	// Regression: managedBy frequently differs from the VM ARM ID only
	// in casing; byte-for-byte matching dropped every such attachment.
	t.Run("managedBy alone resolves despite casing", func(t *testing.T) {
		disks := []Disk{{ID: tOSDisk, ManagedBy: lowerRG(tVM1)}}
		conns := TransformVMToDiskConnections(nil, disks, vmIDMap, diskIDMap)
		if len(conns) != 1 || conns[0].Source != "azure::"+tVM1 || conns[0].Target != "azure::"+tOSDisk {
			t.Fatalf("expected VM01 -> OS disk, got %+v", conns)
		}
	})

	t.Run("unattached disk with correlated name produces no edge", func(t *testing.T) {
		disks := []Disk{{ID: tOSDisk, Name: "LAB-VM01-os"}}
		vms := []VirtualMachine{{ID: tVM1, Name: "LAB-VM01"}}
		if conns := TransformVMToDiskConnections(vms, disks, vmIDMap, diskIDMap); len(conns) != 0 {
			t.Fatalf("expected no inferred edge, got %d", len(conns))
		}
	})
}

func TestTransformNICToNSGConnections(t *testing.T) {
	nicIDMap := idMapOf(tNIC1, tNIC2)
	nsgIDMap := idMapOf(tNSG)
	nics := []NetworkInterface{
		{ID: tNIC1, Name: "LAB-VM01-nic", NetworkSecurityGroup: &azNSGRef{ID: lowerRG(tNSG)}},
		{ID: tNIC2, Name: "LAB-VM02-nic"}, // no NIC-level NSG
	}
	conns := TransformNICToNSGConnections(nics, nicIDMap, nsgIDMap)
	if len(conns) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(conns))
	}
	c := conns[0]
	if c.Type != "osiris.azure.nsg.association" || c.Direction != "forward" {
		t.Errorf("expected osiris.azure.nsg.association/forward, got %s/%s", c.Type, c.Direction)
	}
	if c.Source != "azure::"+tNIC1 || c.Target != "azure::"+tNSG {
		t.Errorf("expected NIC01 -> NSG, got %q -> %q", c.Source, c.Target)
	}
}

func TestTransformNICToPublicIPConnections(t *testing.T) {
	nicIDMap := idMapOf(tNIC1, tNIC2)
	pipIDMap := idMapOf(tPIP)
	nics := []NetworkInterface{
		{ID: tNIC1, IPConfigurations: []IPConfiguration{
			{Name: "ipconfig1", PublicIPAddress: &azPublicIPRef{ID: lowerRG(tPIP)}},
			{Name: "ipconfig2"},
		}},
		{ID: tNIC2, Name: "LAB-PIP01-nic", IPConfigurations: []IPConfiguration{{Name: "ipconfig1"}}},
	}
	conns := TransformNICToPublicIPConnections(nics, nicIDMap, pipIDMap)
	if len(conns) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(conns))
	}
	c := conns[0]
	if c.Type != "network" || c.Source != "azure::"+tNIC1 || c.Target != "azure::"+tPIP {
		t.Errorf("expected NIC01 -> PIP network, got %s %q -> %q", c.Type, c.Source, c.Target)
	}
	if c.Properties["ip_configuration"] != "ipconfig1" {
		t.Errorf("expected ip_configuration ipconfig1, got %v", c.Properties)
	}
}
