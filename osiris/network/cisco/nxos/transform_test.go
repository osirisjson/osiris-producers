// transform_test.go - Unit tests for NX-OS to OSIRIS transform
// functions.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package nxos

import (
	"testing"

	"go.osirisjson.org/producers/pkg/sdk"
)

func TestTransformDevice(t *testing.T) {
	version := versionResponse{
		ChassisID:    "Nexus9000 C9508",
		ProcBoardID:  "TST0000NX01",
		SysVerStr:    "10.3(4a)",
		HostName:     "LAB-SPINE01",
		BiosVerStr:   "08.42",
		RRReason:     "Reset Requested by CLI command reload",
		KernUptmDays: "10",
		KernUptmHrs:  "5",
		KernUptmMins: "30",
		KernUptmSecs: "15",
		Memory:       65536000,
	}

	r, id := TransformDevice("LAB-SPINE01", version)
	if id == "" {
		t.Fatal("expected non-empty resource ID")
	}
	if r.Name != "LAB-SPINE01" {
		t.Errorf("name: %s", r.Name)
	}
	if r.Type != "osiris.cisco.switch.spine" {
		t.Errorf("type: %s", r.Type)
	}
	if r.Status != "active" {
		t.Errorf("status: %s", r.Status)
	}
	if r.Provider.Name != "cisco" {
		t.Errorf("provider: %s", r.Provider.Name)
	}
	if r.Properties["serial"] != "TST0000NX01" {
		t.Errorf("serial: %v", r.Properties["serial"])
	}
	if r.Properties["model"] != "Nexus9000 C9508" {
		t.Errorf("model: %v", r.Properties["model"])
	}

	// Check extensions.
	cisco := r.Extensions[extensionNamespace].(map[string]any)
	if cisco["bios_version"] != "08.42" {
		t.Errorf("bios_version: %v", cisco["bios_version"])
	}
	if cisco["last_reset_reason"] != "Reset Requested by CLI command reload" {
		t.Errorf("last_reset_reason: %v", cisco["last_reset_reason"])
	}
	if cisco["kernel_uptime"] != "10d 5h 30m 15s" {
		t.Errorf("kernel_uptime: %v", cisco["kernel_uptime"])
	}
}

func TestTransformDevice_Leaf(t *testing.T) {
	version := versionResponse{
		ChassisID:   "Nexus9000 C93108TC-FX",
		ProcBoardID: "TST0000NX02",
		SysVerStr:   "10.3(4a)",
		HostName:    "LAB-LEAF01",
	}

	r, _ := TransformDevice("LAB-LEAF01", version)
	if r.Type != "osiris.cisco.switch.leaf" {
		t.Errorf("expected leaf type, got: %s", r.Type)
	}
}

func TestTransformInterfaces(t *testing.T) {
	ifBrief := interfaceBriefResponse{
		TableInterface: interfaceTable{
			RowInterface: rowList[interfaceBriefRow]{
				{Interface: "Ethernet1/1", State: "up", Speed: "10G", Type: "eth", VLAN: "100"},
				{Interface: "Ethernet1/2", State: "down", Speed: "10G", Type: "eth", VLAN: "200"},
				{Interface: "port-channel10", State: "up", Speed: "20G"},
				{Interface: "loopback0", State: "up"},
				{Interface: "mgmt0", State: "up"},
			},
		},
	}

	resources, nameToID := TransformInterfaces("LAB-SW01", ifBrief)

	if len(resources) != 5 {
		t.Fatalf("expected 5 resources, got %d", len(resources))
	}
	if len(nameToID) != 5 {
		t.Fatalf("expected 5 name mappings, got %d", len(nameToID))
	}

	// Check Ethernet type.
	if resources[0].Type != "network.interface" {
		t.Errorf("Ethernet should be network.interface, got %s", resources[0].Type)
	}
	if resources[0].Status != "active" {
		t.Errorf("up interface should be active, got %s", resources[0].Status)
	}
	if resources[1].Status != "inactive" {
		t.Errorf("down interface should be inactive, got %s", resources[1].Status)
	}

	// Check port-channel type.
	if resources[2].Type != "osiris.cisco.interface.lag" {
		t.Errorf("port-channel should be network.interface.lag, got %s", resources[2].Type)
	}
}

func TestTransformLLDPNeighbors(t *testing.T) {
	lldp := lldpNeighborsResponse{
		TableNborDetail: lldpTable{
			RowNborDetail: rowList[lldpNeighborRow]{
				{LocalPortID: "Ethernet1/1", SysName: "REMOTE-SW01", PortID: "Ethernet1/49", MgmtAddr: "192.0.2.10"},
				{LocalPortID: "Ethernet1/2", SysName: "REMOTE-SW02", PortID: "Ethernet1/50"},
			},
		},
	}

	ifNameToID := map[string]string{
		"Ethernet1/1": "res-network.interface-eth1-1-abc123",
		"Ethernet1/2": "res-network.interface-eth1-2-def456",
	}

	connections, stubs := TransformLLDPNeighbors("LAB-SW01", lldp, ifNameToID)

	if len(connections) != 2 {
		t.Fatalf("expected 2 connections, got %d", len(connections))
	}
	if len(stubs) != 2 {
		t.Fatalf("expected 2 stubs, got %d", len(stubs))
	}

	// Verify stub properties.
	if stubs[0].Status != "unknown" {
		t.Errorf("stub status should be unknown, got %s", stubs[0].Status)
	}
	if stubs[0].Properties["remote_system"] != "REMOTE-SW01" {
		t.Errorf("remote_system: %v", stubs[0].Properties["remote_system"])
	}
	if stubs[0].Properties["remote_mgmt_addr"] != "192.0.2.10" {
		t.Errorf("remote_mgmt_addr: %v", stubs[0].Properties["remote_mgmt_addr"])
	}

	// Verify connection.
	if connections[0].Type != "physical.ethernet" {
		t.Errorf("connection type: %s", connections[0].Type)
	}
	if connections[0].Status != "active" {
		t.Errorf("connection status: %s", connections[0].Status)
	}
}

func TestTransformLLDPNeighbors_MissingLocalInterface(t *testing.T) {
	lldp := lldpNeighborsResponse{
		TableNborDetail: lldpTable{
			RowNborDetail: rowList[lldpNeighborRow]{
				{LocalPortID: "Ethernet1/99", SysName: "REMOTE-SW01", PortID: "Ethernet1/1"},
			},
		},
	}

	ifNameToID := map[string]string{} // empty - no matching local interface

	connections, stubs := TransformLLDPNeighbors("LAB-SW01", lldp, ifNameToID)
	if len(connections) != 0 {
		t.Errorf("expected 0 connections for missing local interface, got %d", len(connections))
	}
	if len(stubs) != 0 {
		t.Errorf("expected 0 stubs for missing local interface, got %d", len(stubs))
	}
}

func TestTransformVLANs(t *testing.T) {
	vlanBrief := vlanBriefResponse{
		TableVlanBrief: vlanBriefTable{
			RowVlanBrief: rowList[vlanBriefRow]{
				{VLANID: "100", VLANName: "PROD", VLANState: "active", ShutState: "noshutdown"},
				{VLANID: "200", VLANName: "MGMT", VLANState: "active", ShutState: "noshutdown"},
			},
		},
	}

	groups, idMap := TransformVLANs("LAB-SW01", vlanBrief)

	if len(groups) != 2 {
		t.Fatalf("expected 2 VLAN groups, got %d", len(groups))
	}
	if len(idMap) != 2 {
		t.Fatalf("expected 2 VLAN ID mappings, got %d", len(idMap))
	}

	if groups[0].Type != "network.vlan" {
		t.Errorf("type: %s", groups[0].Type)
	}
	if groups[0].Name != "VLAN 100" {
		t.Errorf("name: %s", groups[0].Name)
	}
	if groups[0].Description != "PROD" {
		t.Errorf("description: %s", groups[0].Description)
	}
}

func TestTransformVRFs(t *testing.T) {
	vrfDetail := vrfDetailResponse{
		TableVRF: vrfTable{
			RowVRF: rowList[vrfDetailRow]{
				{VRFName: "PROD", VRFID: "3", VRFState: "Up", RD: "192.0.2.1:3"},
				{VRFName: "MGMT", VRFID: "4", VRFState: "Up"},
			},
		},
	}

	groups, nameMap := TransformVRFs("LAB-SW01", vrfDetail)

	if len(groups) != 2 {
		t.Fatalf("expected 2 VRF groups, got %d", len(groups))
	}
	if len(nameMap) != 2 {
		t.Fatalf("expected 2 VRF name mappings, got %d", len(nameMap))
	}

	if groups[0].Type != "logical.vrf" {
		t.Errorf("type: %s", groups[0].Type)
	}
	if groups[0].Name != "PROD" {
		t.Errorf("name: %s", groups[0].Name)
	}
	if groups[0].Properties["route_distinguisher"] != "192.0.2.1:3" {
		t.Errorf("rd: %v", groups[0].Properties["route_distinguisher"])
	}
}

func TestTransformVPC(t *testing.T) {
	vpcBrief := vpcBriefResponse{
		DomainID:            "10",
		Role:                "primary",
		PeerStatus:          "peer-ok",
		PeerKeepaliveStatus: "peer-alive",
	}

	g, gid := TransformVPC("LAB-SW01", vpcBrief)
	if g == nil {
		t.Fatal("expected vPC group")
	}
	if gid == "" {
		t.Fatal("expected vPC group ID")
	}
	if g.Type != "network.vpc" {
		t.Errorf("type: %s", g.Type)
	}
	if g.Properties["domain_id"] != "10" {
		t.Errorf("domain_id: %v", g.Properties["domain_id"])
	}
	if g.Properties["role"] != "primary" {
		t.Errorf("role: %v", g.Properties["role"])
	}
}

func TestTransformVPC_NotConfigured(t *testing.T) {
	g, _ := TransformVPC("LAB-SW01", vpcBriefResponse{})
	if g != nil {
		t.Error("expected nil group when vPC not configured")
	}
}

func TestTransformInventory(t *testing.T) {
	inv := inventoryResponse{
		TableInv: inventoryTable{
			RowInv: rowList[inventoryRow]{
				{Name: "Chassis", Desc: "Nexus9000 C9508 Chassis", ProductID: "N9K-C9508", VendorID: "V01", SerialNum: "TST0000NX01"},
				{Name: "Slot 1", Desc: "Supervisor Module", ProductID: "N9K-SUP-B+", SerialNum: "TST0000SUP1"},
			},
		},
	}

	items := TransformInventory(inv)
	if len(items) != 2 {
		t.Fatalf("expected 2 inventory items, got %d", len(items))
	}
	if items[0]["name"] != "Chassis" {
		t.Errorf("name: %v", items[0]["name"])
	}
	if items[0]["serial"] != "TST0000NX01" {
		t.Errorf("serial: %v", items[0]["serial"])
	}
}

func TestTransformSystemResources(t *testing.T) {
	sysRes := systemResourcesResponse{
		CPUStateIdle:    "95.50",
		MemoryUsageUsed: "8000000",
		MemoryUsageFree: "4000000",
		LoadAvg1Min:     "0.25",
	}

	ext := TransformSystemResources(sysRes)
	if ext["cpu_idle"] != 95.50 {
		t.Errorf("cpu_idle: %v", ext["cpu_idle"])
	}
	if ext["memory_used"] != int64(8000000) {
		t.Errorf("memory_used: %v", ext["memory_used"])
	}
	if ext["memory_free"] != int64(4000000) {
		t.Errorf("memory_free: %v", ext["memory_free"])
	}
	if ext["load_avg_1min"] != 0.25 {
		t.Errorf("load_avg_1min: %v", ext["load_avg_1min"])
	}
}

func TestTransformEnvironment(t *testing.T) {
	env := environmentResponse{
		TablePSInfo: psuTable{
			RowPSInfo: rowList[psuRow]{
				{PSNum: "1", PSModel: "NXA-PAC-1100W", PSStatus: "ok", ActualOut: "350 W"},
			},
		},
		TableTempInfo: tempTable{
			RowTempInfo: rowList[tempRow]{
				{TempMod: "1", Sensor: "CPU", CurTemp: "42", AlarmStatus: "Ok"},
			},
		},
	}

	ext := TransformEnvironment(env)

	psus, ok := ext["power_supplies"].([]map[string]any)
	if !ok || len(psus) != 1 {
		t.Fatalf("expected 1 PSU, got %v", ext["power_supplies"])
	}
	if psus[0]["model"] != "NXA-PAC-1100W" {
		t.Errorf("psu model: %v", psus[0]["model"])
	}

	temps, ok := ext["temperature"].([]map[string]any)
	if !ok || len(temps) != 1 {
		t.Fatalf("expected 1 temp sensor, got %v", ext["temperature"])
	}
	if temps[0]["current"] != "42" {
		t.Errorf("temp current: %v", temps[0]["current"])
	}
}

func TestWireInterfacesToVLANs(t *testing.T) {
	vlanBrief := vlanBriefResponse{
		TableVlanBrief: vlanBriefTable{
			RowVlanBrief: rowList[vlanBriefRow]{
				{VLANID: "100", PortList: "Ethernet1/1,Ethernet1/2"},
			},
		},
	}

	ifNameToID := map[string]string{
		"Ethernet1/1": "res-if-1",
		"Ethernet1/2": "res-if-2",
	}

	groups := []sdk.Group{{ID: "grp-vlan-100", Type: "network.vlan"}}
	vlanIDToGroupID := map[string]string{"100": "grp-vlan-100"}

	WireInterfacesToVLANs(vlanBrief, interfaceBriefResponse{}, ifNameToID, groups, vlanIDToGroupID)

	if len(groups[0].Members) != 2 {
		t.Errorf("expected 2 VLAN members, got %d: %v", len(groups[0].Members), groups[0].Members)
	}
}

func TestWireInterfacesToVLANs_FallbackToInterfaceBrief(t *testing.T) {
	// When the VLAN's own port list yields no matches, fall back to
	// scanning "show interface brief" per-interface vlan field.
	vlanBrief := vlanBriefResponse{
		TableVlanBrief: vlanBriefTable{
			RowVlanBrief: rowList[vlanBriefRow]{
				{VLANID: "100"}, // no PortList
			},
		},
	}
	ifBrief := interfaceBriefResponse{
		TableInterface: interfaceTable{
			RowInterface: rowList[interfaceBriefRow]{
				{Interface: "Ethernet1/1", VLAN: "100"},
				{Interface: "Ethernet1/2", VLAN: "--"},
			},
		},
	}

	ifNameToID := map[string]string{"Ethernet1/1": "res-if-1", "Ethernet1/2": "res-if-2"}
	groups := []sdk.Group{{ID: "grp-vlan-100", Type: "network.vlan"}}
	vlanIDToGroupID := map[string]string{"100": "grp-vlan-100"}

	matched := WireInterfacesToVLANs(vlanBrief, ifBrief, ifNameToID, groups, vlanIDToGroupID)
	if matched != 1 {
		t.Fatalf("expected 1 fallback match, got %d", matched)
	}
	if len(groups[0].Members) != 1 {
		t.Errorf("expected 1 VLAN member, got %d: %v", len(groups[0].Members), groups[0].Members)
	}
}

func TestWireInterfacesToVRFs(t *testing.T) {
	vrfDetail := vrfDetailResponse{
		TableVRF: vrfTable{
			RowVRF: rowList[vrfDetailRow]{
				{
					VRFName: "PROD",
					TableIf: vrfIfTable{
						RowIf: rowList[vrfInterfaceIfRow]{
							{IfName: "Ethernet1/1"},
							{IfName: "loopback0"},
						},
					},
				},
			},
		},
	}

	ifNameToID := map[string]string{
		"Ethernet1/1": "res-if-1",
		"loopback0":   "res-if-lo0",
	}

	groups := []sdk.Group{{ID: "grp-vrf-prod", Type: "logical.vrf"}}
	vrfNameToGroupID := map[string]string{"PROD": "grp-vrf-prod"}

	WireInterfacesToVRFs(vrfDetail, vrfInterfaceResponse{}, ifNameToID, groups, vrfNameToGroupID)

	if len(groups[0].Members) != 2 {
		t.Errorf("expected 2 VRF members, got %d: %v", len(groups[0].Members), groups[0].Members)
	}
}

func TestWireInterfacesToVRFs_TableIntfShapeFallback(t *testing.T) {
	// Some NX-OS versions nest member interfaces under TABLE_intf/
	// ROW_intf (intf_name) instead of TABLE_if/ROW_if (if_name).
	vrfDetail := vrfDetailResponse{
		TableVRF: vrfTable{
			RowVRF: rowList[vrfDetailRow]{
				{
					VRFName: "PROD",
					TableIntf: vrfIntfTable{
						RowIntf: rowList[vrfInterfaceIntfRow]{
							{IntfName: "Ethernet1/1"},
						},
					},
				},
			},
		},
	}
	ifNameToID := map[string]string{"Ethernet1/1": "res-if-1"}
	groups := []sdk.Group{{ID: "grp-vrf-prod", Type: "logical.vrf"}}
	vrfNameToGroupID := map[string]string{"PROD": "grp-vrf-prod"}

	matched := WireInterfacesToVRFs(vrfDetail, vrfInterfaceResponse{}, ifNameToID, groups, vrfNameToGroupID)
	if matched != 1 {
		t.Fatalf("expected 1 match via TABLE_intf fallback, got %d", matched)
	}
}

func TestWireInterfacesToVRFs_FallbackToVRFInterface(t *testing.T) {
	// When "show vrf all detail" yields 0 matches, fall back to the
	// separate flat "show vrf interface" mapping.
	vrfDetail := vrfDetailResponse{
		TableVRF: vrfTable{
			RowVRF: rowList[vrfDetailRow]{{VRFName: "PROD"}}, // no nested interfaces
		},
	}
	vrfInterface := vrfInterfaceResponse{
		TableIf: vrfInterfaceTable{
			RowIf: rowList[vrfInterfaceFlatRow]{
				{IfName: "Ethernet1/1", VRFName: "PROD"},
			},
		},
	}
	ifNameToID := map[string]string{"Ethernet1/1": "res-if-1"}
	groups := []sdk.Group{{ID: "grp-vrf-prod", Type: "logical.vrf"}}
	vrfNameToGroupID := map[string]string{"PROD": "grp-vrf-prod"}

	matched := WireInterfacesToVRFs(vrfDetail, vrfInterface, ifNameToID, groups, vrfNameToGroupID)
	if matched != 1 {
		t.Fatalf("expected 1 fallback match, got %d", matched)
	}
}

func TestWirePortChannelsToVPC(t *testing.T) {
	vpcBrief := vpcBriefResponse{
		TableVPC: vpcTable{
			RowVPC: rowList[vpcMemberRow]{
				{IfIndex: "port-channel10"},
				{IfIndex: "port-channel20"},
			},
		},
	}

	ifNameToID := map[string]string{
		"port-channel10": "res-pc-10",
		"port-channel20": "res-pc-20",
	}

	g := &sdk.Group{ID: "grp-vpc-10", Type: "network.vpc"}

	WirePortChannelsToVPC(vpcBrief, ifNameToID, g)

	if len(g.Members) != 2 {
		t.Errorf("expected 2 vPC members, got %d: %v", len(g.Members), g.Members)
	}
}

func TestTransformPortChannels(t *testing.T) {
	pcSummary := portChannelSummaryResponse{
		TableChannel: portChannelTable{
			RowChannel: rowList[portChannelRow]{
				{
					Group:       "10",
					PortChannel: "Po10",
					TableMember: portChannelMemberTable{
						RowMember: rowList[portChannelMemberRow]{
							{Port: "Eth1/1", PortStatus: "P"},
							{Port: "Eth1/2", PortStatus: "P"},
						},
					},
				},
			},
		},
	}

	ifNameToID := map[string]string{
		"port-channel10": "res-pc-10",
		"Ethernet1/1":    "res-eth-1-1",
		"Ethernet1/2":    "res-eth-1-2",
	}

	resources := []sdk.Resource{
		{ID: "res-pc-10", Type: "osiris.cisco.interface.lag", Name: "port-channel10"},
		{ID: "res-eth-1-1", Type: "network.interface", Name: "Ethernet1/1"},
		{ID: "res-eth-1-2", Type: "network.interface", Name: "Ethernet1/2"},
	}

	connections := TransformPortChannels(pcSummary, resources, ifNameToID)

	if len(connections) != 2 {
		t.Fatalf("expected 2 contains connections, got %d", len(connections))
	}
	for _, c := range connections {
		if c.Type != "contains" {
			t.Errorf("connection type = %s, want contains", c.Type)
		}
		if c.Source != "res-pc-10" {
			t.Errorf("connection source = %s, want res-pc-10", c.Source)
		}
		if c.Direction != "forward" {
			t.Errorf("connection direction = %s, want forward", c.Direction)
		}
		if c.Properties["port_status"] != "P" {
			t.Errorf("connection port_status = %v, want P", c.Properties["port_status"])
		}
	}

	if resources[0].Properties["member_count"] != 2 {
		t.Errorf("port-channel member_count = %v, want 2", resources[0].Properties["member_count"])
	}
}

func TestTransformPortChannels_UnresolvedMemberSkipped(t *testing.T) {
	// A member port that never appeared in "show interface brief" (and
	// therefore has no resource ID) must be skipped, not crash or
	// produce a dangling connection member_count still only reflects
	// rows the device actually reported, resolvable or not.
	pcSummary := portChannelSummaryResponse{
		TableChannel: portChannelTable{
			RowChannel: rowList[portChannelRow]{
				{
					PortChannel: "Po10",
					TableMember: portChannelMemberTable{
						RowMember: rowList[portChannelMemberRow]{
							{Port: "Eth1/1", PortStatus: "P"},
							{Port: "Eth1/99", PortStatus: "P"},
						},
					},
				},
			},
		},
	}

	ifNameToID := map[string]string{
		"port-channel10": "res-pc-10",
		"Ethernet1/1":    "res-eth-1-1",
	}
	resources := []sdk.Resource{
		{ID: "res-pc-10", Type: "osiris.cisco.interface.lag", Name: "port-channel10"},
		{ID: "res-eth-1-1", Type: "network.interface", Name: "Ethernet1/1"},
	}

	connections := TransformPortChannels(pcSummary, resources, ifNameToID)
	if len(connections) != 1 {
		t.Fatalf("expected 1 resolvable connection, got %d", len(connections))
	}
	if resources[0].Properties["member_count"] != 2 {
		t.Errorf("member_count should count every reported member row, got %v", resources[0].Properties["member_count"])
	}
}

func TestTransformPortChannels_UnknownPortChannelSkipped(t *testing.T) {
	// A port-channel with no matching interface resource (should not
	// happen in practice "show interface brief" already produced
	// every LAG resource but must not panic if it does).
	pcSummary := portChannelSummaryResponse{
		TableChannel: portChannelTable{
			RowChannel: rowList[portChannelRow]{
				{
					PortChannel: "Po99",
					TableMember: portChannelMemberTable{
						RowMember: rowList[portChannelMemberRow]{
							{Port: "Eth1/1", PortStatus: "P"},
						},
					},
				},
			},
		},
	}
	ifNameToID := map[string]string{"Ethernet1/1": "res-eth-1-1"}
	resources := []sdk.Resource{{ID: "res-eth-1-1", Type: "network.interface", Name: "Ethernet1/1"}}

	connections := TransformPortChannels(pcSummary, resources, ifNameToID)
	if len(connections) != 0 {
		t.Errorf("expected 0 connections for an unresolvable port-channel, got %d", len(connections))
	}
}

func TestResourceID_Deterministic(t *testing.T) {
	id1 := resourceID("network.switch", "LAB-SW01")
	id2 := resourceID("network.switch", "LAB-SW01")
	if id1 != id2 {
		t.Errorf("resourceID not deterministic: %s != %s", id1, id2)
	}

	id3 := resourceID("network.switch", "LAB-SW02")
	if id1 == id3 {
		t.Error("different inputs should produce different IDs")
	}
}

func TestMapInterfaceStatus(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"up", "active"},
		{"down", "inactive"},
		{"Up", "active"},
		{"Down", "inactive"},
		{"unknown", "unknown"},
		{"", "unknown"},
	}
	for _, tc := range cases {
		got := mapInterfaceStatus(tc.in)
		if got != tc.want {
			t.Errorf("mapInterfaceStatus(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeIfName(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Eth1/1", "Ethernet1/1"},
		{"Ethernet1/1", "Ethernet1/1"},
		{"Po10", "port-channel10"},
		{"port-channel10", "port-channel10"},
		{"loopback0", "loopback0"},
		{" Eth1/2 ", "Ethernet1/2"},
	}
	for _, tc := range cases {
		got := normalizeIfName(tc.in)
		if got != tc.want {
			t.Errorf("normalizeIfName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestClassifyRole(t *testing.T) {
	cases := []struct {
		hostname, model, want string
	}{
		{"LAB-SPINE01", "N9K-C9508", "spine"},
		{"LAB-LEAF01", "N9K-C93108TC-FX", "leaf"},
		{"SWITCH01", "N9K-C9508", "spine"},
		{"SWITCH02", "N9K-C93108TC-FX", "leaf"},
		{"SWITCH03", "UNKNOWN-MODEL", ""},
	}
	for _, tc := range cases {
		got := classifyRole(tc.hostname, tc.model)
		if got != tc.want {
			t.Errorf("classifyRole(%q, %q) = %q, want %q", tc.hostname, tc.model, got, tc.want)
		}
	}
}

func TestEnrichInterfaceDetails(t *testing.T) {
	ifNameToID := map[string]string{
		"Ethernet1/1": "res-if-1",
	}

	resources := []sdk.Resource{
		{ID: "res-if-1", Type: "network.interface", Properties: map[string]any{"speed": "10G"}},
	}

	ifDetail := interfaceDetailResponse{
		TableInterface: interfaceDetailTable{
			RowInterface: rowList[interfaceDetailRow]{
				{
					Interface: "Ethernet1/1",
					MTU:       9216,
					Bandwidth: 10000000,
					Duplex:    "full",
					HWAddr:    "aabb.ccdd.eeff",
					Desc:      "Uplink to spine",
					OutBytes:  1000000,
					InBytes:   2000000,
				},
			},
		},
	}

	EnrichInterfaceDetails("LAB-SW01", ifDetail, resources, ifNameToID)

	props := resources[0].Properties
	if props["mtu"] != int64(9216) {
		t.Errorf("mtu: %v", props["mtu"])
	}
	if props["bandwidth"] != int64(10000000) {
		t.Errorf("bandwidth: %v", props["bandwidth"])
	}
	if props["duplex"] != "full" {
		t.Errorf("duplex: %v", props["duplex"])
	}
	if props["description"] != "Uplink to spine" {
		t.Errorf("description: %v", props["description"])
	}
	if props["tx_bytes"] != int64(1000000) {
		t.Errorf("tx_bytes: %v", props["tx_bytes"])
	}
	if props["rx_bytes"] != int64(2000000) {
		t.Errorf("rx_bytes: %v", props["rx_bytes"])
	}
}
