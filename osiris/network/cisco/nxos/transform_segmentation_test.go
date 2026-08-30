// transform_segmentation_test.go - Unit tests for
// transform_segmentation.go.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package nxos

import (
	"testing"

	"go.osirisjson.org/producers/pkg/sdk"
)

func TestTransformVLANs(t *testing.T) {
	vlanBrief := vlanBriefResponse{
		TableVlanBrief: vlanBriefTable{
			RowVlanBrief: rowList[vlanBriefRow]{
				{VLANID: "100", VLANName: `"PROD"`, VLANState: "active", ShutState: "noshutdown"},
				{VLANID: "200", VLANState: "suspend"},
			},
		},
	}

	resources, idMap := TransformVLANs("TST0000NX01", vlanBrief)

	if len(resources) != 2 {
		t.Fatalf("expected 2 VLAN resources, got %d", len(resources))
	}
	if len(idMap) != 2 {
		t.Fatalf("expected 2 VLAN ID mappings, got %d", len(idMap))
	}

	v100 := resources[0]
	if v100.Type != "network.vlan" {
		t.Errorf("type: %s", v100.Type)
	}
	if v100.ID != "cisco.nxos::TST0000NX01/vlan/100" {
		t.Errorf("id should be <provider>::<serial>/vlan/<id>, got %q", v100.ID)
	}
	if idMap["100"] != v100.ID {
		t.Errorf("idMap should agree with the resource id: %q vs %q", idMap["100"], v100.ID)
	}
	if v100.Name != "PROD" {
		t.Errorf("name should be the (quote-stripped) VLAN name: %q", v100.Name)
	}
	if v100.Status != "active" {
		t.Errorf("status: %s", v100.Status)
	}
	if v100.Properties["vlan_id"] != 100 {
		t.Errorf("vlan_id should be an int, got %v (%T)", v100.Properties["vlan_id"], v100.Properties["vlan_id"])
	}
	if v100.Properties["vlan_name"] != "PROD" {
		t.Errorf("vlan_name: %v", v100.Properties["vlan_name"])
	}
	if v100.Properties["admin_state"] != "noshutdown" {
		t.Errorf("admin_state: %v", v100.Properties["admin_state"])
	}

	v200 := resources[1]
	if v200.Name != "VLAN 200" {
		t.Errorf("unnamed VLAN should fall back to 'VLAN <id>', got %q", v200.Name)
	}
	if v200.Status != "inactive" {
		t.Errorf("suspended VLAN status = %s, want inactive", v200.Status)
	}
	if _, ok := v200.Properties["vlan_name"]; ok {
		t.Error("vlan_name should be absent when the device reports none")
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
	vlanIDToResID := map[string]string{"100": "res-vlan-100"}

	conns := WireInterfacesToVLANs(vlanBrief, interfaceBriefResponse{}, ifNameToID, vlanIDToResID, map[string]bool{})

	if len(conns) != 2 {
		t.Fatalf("expected 2 network.l2 connections, got %d", len(conns))
	}
	for _, c := range conns {
		if c.Type != "network.l2" {
			t.Errorf("connection type = %s, want network.l2", c.Type)
		}
		if c.Target != "res-vlan-100" {
			t.Errorf("connection target = %s, want res-vlan-100", c.Target)
		}
		if c.Direction != "bidirectional" {
			t.Errorf("connection direction = %s, want bidirectional", c.Direction)
		}
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
	vlanIDToResID := map[string]string{"100": "res-vlan-100"}

	conns := WireInterfacesToVLANs(vlanBrief, ifBrief, ifNameToID, vlanIDToResID, map[string]bool{})
	if len(conns) != 1 {
		t.Fatalf("expected 1 fallback connection, got %d", len(conns))
	}
	if conns[0].Source != "res-if-1" || conns[0].Target != "res-vlan-100" {
		t.Errorf("connection %s -> %s, want res-if-1 -> res-vlan-100", conns[0].Source, conns[0].Target)
	}
}

func TestWireInterfacesToVLANs_DedupesAgainstTrunkWiring(t *testing.T) {
	// A port reported both by "show vlan brief" and by
	// "show interface switchport" must yield exactly one connection
	// (shared seen map).
	vlanBrief := vlanBriefResponse{
		TableVlanBrief: vlanBriefTable{
			RowVlanBrief: rowList[vlanBriefRow]{{VLANID: "100", PortList: "Ethernet1/1"}},
		},
	}
	switchport := switchportResponse{
		TableInterface: switchportTable{
			RowInterface: rowList[switchportRow]{{Interface: "Ethernet1/1", TrunkVLANs: "100"}},
		},
	}
	ifNameToID := map[string]string{"Ethernet1/1": "res-if-1"}
	vlanIDToResID := map[string]string{"100": "res-vlan-100"}

	seen := map[string]bool{}
	conns := WireInterfacesToVLANs(vlanBrief, interfaceBriefResponse{}, ifNameToID, vlanIDToResID, seen)
	conns = append(conns, WireTrunkPortsToVLANs(switchport, ifNameToID, vlanIDToResID, seen)...)
	if len(conns) != 1 {
		t.Errorf("expected 1 deduplicated connection, got %d", len(conns))
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

func TestExpandVLANRanges(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"85", []string{"85"}},
		{"85,900", []string{"85", "900"}},
		{"906-909", []string{"906", "907", "908", "909"}},
		{"85,900,906-909,921", []string{"85", "900", "906", "907", "908", "909", "921"}},
		{"", nil},
		{" 85 , 900 ", []string{"85", "900"}},
		{"900-899", nil},           // inverted range, malformed skipped
		{"abc,86", []string{"86"}}, // one malformed token doesn't drop the rest
	}
	for _, tc := range cases {
		got := expandVLANRanges(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("expandVLANRanges(%q) = %v, want %v", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("expandVLANRanges(%q) = %v, want %v", tc.in, got, tc.want)
				break
			}
		}
	}
}

func TestWireTrunkPortsToVLANs(t *testing.T) {
	switchport := switchportResponse{
		TableInterface: switchportTable{
			RowInterface: rowList[switchportRow]{
				{Interface: "Ethernet1/1", TrunkVLANs: "100,200-201"},
				{Interface: "Ethernet1/2", TrunkVLANs: "100"}, // no group for 200/201
			},
		},
	}
	ifNameToID := map[string]string{"Ethernet1/1": "res-eth1-1", "Ethernet1/2": "res-eth1-2"}
	vlanIDToResID := map[string]string{"100": "res-vlan-100", "200": "res-vlan-200"}

	conns := WireTrunkPortsToVLANs(switchport, ifNameToID, vlanIDToResID, map[string]bool{})
	// Ethernet1/1: 100 (match), 200 (match), 201 (no resource) = 2.
	// Ethernet1/2: 100 (match) = 1.
	if len(conns) != 3 {
		t.Errorf("connections = %d, want 3", len(conns))
	}
	var toV100, toV200 int
	for _, c := range conns {
		if c.Type != "network.l2" {
			t.Errorf("connection type = %s, want network.l2", c.Type)
		}
		switch c.Target {
		case "res-vlan-100":
			toV100++
		case "res-vlan-200":
			toV200++
		}
	}
	if toV100 != 2 {
		t.Errorf("connections to VLAN 100 = %d, want 2", toV100)
	}
	if toV200 != 1 {
		t.Errorf("connections to VLAN 200 = %d, want 1", toV200)
	}
}

func TestWireTrunkPortsToVLANs_UnresolvableInterfaceSkipped(t *testing.T) {
	switchport := switchportResponse{
		TableInterface: switchportTable{
			RowInterface: rowList[switchportRow]{
				{Interface: "Ethernet1/99", TrunkVLANs: "100"},
			},
		},
	}
	vlanIDToResID := map[string]string{"100": "res-vlan-100"}

	conns := WireTrunkPortsToVLANs(switchport, map[string]string{}, vlanIDToResID, map[string]bool{})
	if len(conns) != 0 {
		t.Errorf("expected no connections, got %d", len(conns))
	}
}
