// transform_routing_test.go - Unit tests for transform_routing.go.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package nxos

import (
	"testing"
)

func TestTransformOSPFNeighbors(t *testing.T) {
	ospf := ospfNeighborsResponse{
		TableCtx: ospfCtxTable{
			RowCtx: rowList[ospfCtxRow]{
				{
					VRFName: "default",
					TableNbr: ospfNeighborTable{
						RowNbr: rowList[ospfNeighborRow]{
							{RouterID: "203.0.113.2", Priority: "1", State: "FULL", DRState: "DR", UpTime: "P1Y7M14DT6H23M57S", Address: "203.0.113.2", IfName: "Vlan100"},
							{RouterID: "203.0.113.3", State: "FULL", DRState: "-", Address: "203.0.113.3", IfName: "Vlan999"}, // unresolvable
						},
					},
				},
			},
		},
	}
	ifNameToID := map[string]string{"Vlan100": "res-vlan100"}

	connections, stubs := TransformOSPFNeighbors("cisco.nxos::TST0000NX01", ospf, ifNameToID)
	if len(connections) != 1 || len(stubs) != 1 {
		t.Fatalf("expected 1 resolvable connection/stub, got %d/%d", len(connections), len(stubs))
	}
	if connections[0].Type != "network.ospf" {
		t.Errorf("connection type = %s, want network.ospf", connections[0].Type)
	}
	if connections[0].Source != "res-vlan100" {
		t.Errorf("connection source = %s, want res-vlan100", connections[0].Source)
	}
	if connections[0].Properties["state"] != "FULL" {
		t.Errorf("state: %v", connections[0].Properties["state"])
	}
	if connections[0].Properties["dr_state"] != "DR" {
		t.Errorf("dr_state: %v", connections[0].Properties["dr_state"])
	}
	if connections[0].Properties["priority"] != "1" {
		t.Errorf("priority: %v", connections[0].Properties["priority"])
	}
	if connections[0].Properties["uptime"] != "P1Y7M14DT6H23M57S" {
		t.Errorf("uptime: %v", connections[0].Properties["uptime"])
	}
	if connections[0].Properties["vrf"] != "default" {
		t.Errorf("vrf: %v", connections[0].Properties["vrf"])
	}
	if stubs[0].Provider.Name != unknownProviderName {
		t.Errorf("stub provider = %s, want %s", stubs[0].Provider.Name, unknownProviderName)
	}
}

func TestTransformOSPFNeighbors_DRStateDashIsOmitted(t *testing.T) {
	// "-" is drstate's own placeholder for "no DR/BDR role" (e.g. a
	// point-to-point link) must not be emitted as a literal property
	// value. A real device also reported it with a leading space
	// ("FULL/ -" in the CLI's own text rendering), so the padded form
	// must be caught too, not just the bare dash.
	ospf := ospfNeighborsResponse{
		TableCtx: ospfCtxTable{
			RowCtx: rowList[ospfCtxRow]{
				{
					VRFName: "default",
					TableNbr: ospfNeighborTable{
						RowNbr: rowList[ospfNeighborRow]{
							{RouterID: "203.0.113.2", State: "FULL", DRState: "-", Address: "203.0.113.2", IfName: "Vlan100"},
							{RouterID: "203.0.113.4", State: "FULL", DRState: " -", Address: "203.0.113.4", IfName: "Vlan100"},
						},
					},
				},
			},
		},
	}
	ifNameToID := map[string]string{"Vlan100": "res-vlan100"}

	connections, _ := TransformOSPFNeighbors("cisco.nxos::TST0000NX01", ospf, ifNameToID)
	if len(connections) != 2 {
		t.Fatalf("expected 2 connections, got %d", len(connections))
	}
	for _, c := range connections {
		if _, ok := c.Properties["dr_state"]; ok {
			t.Errorf("dr_state should be omitted for the '-' placeholder (bare or padded), got %v", c.Properties["dr_state"])
		}
	}
}

func TestTransformOSPFNeighbors_SameRouterOnMultipleInterfacesDeduplicatesStub(t *testing.T) {
	// same OSPF router can be a neighbor across several local
	// interfaces/VLANs at once (a common, normal OSPF topology)
	// the stub resource's ID is derived from the router ID alone, so
	// creating one per row instead of one per unique router ID produced
	// two resources sharing one ID, which failed document assembly
	// outright ("duplicate id ... found in both resource and resource").
	ospf := ospfNeighborsResponse{
		TableCtx: ospfCtxTable{
			RowCtx: rowList[ospfCtxRow]{
				{
					VRFName: "default",
					TableNbr: ospfNeighborTable{
						RowNbr: rowList[ospfNeighborRow]{
							{RouterID: "203.0.113.2", State: "FULL", DRState: "DR", Address: "203.0.113.10", IfName: "Vlan100"},
							{RouterID: "203.0.113.2", State: "FULL", DRState: "DR", Address: "203.0.113.11", IfName: "Vlan200"},
							{RouterID: "203.0.113.2", State: "FULL", DRState: "DR", Address: "203.0.113.12", IfName: "Vlan300"},
						},
					},
				},
			},
		},
	}
	ifNameToID := map[string]string{"Vlan100": "res-vlan100", "Vlan200": "res-vlan200", "Vlan300": "res-vlan300"}

	connections, stubs := TransformOSPFNeighbors("cisco.nxos::TST0000NX01", ospf, ifNameToID)
	if len(connections) != 3 {
		t.Fatalf("expected 3 connections (one per adjacency), got %d", len(connections))
	}
	if len(stubs) != 1 {
		t.Fatalf("expected 1 deduplicated stub resource for the shared router ID, got %d", len(stubs))
	}
	seen := make(map[string]bool)
	for _, c := range connections {
		seen[c.Target] = true
		if c.Target != stubs[0].ID {
			t.Errorf("connection target = %s, want the single stub's ID %s", c.Target, stubs[0].ID)
		}
	}
	if len(seen) != 1 {
		t.Errorf("all 3 connections should target the same deduplicated stub, saw %d distinct targets", len(seen))
	}
}

func TestTransformBGPNeighbors(t *testing.T) {
	bgp := bgpSummaryResponse{
		TableVRF: bgpVRFTable{
			RowVRF: rowList[bgpVRFRow]{
				{
					VRFNameOut: "default",
					TableAf: bgpAfTable{
						RowAf: rowList[bgpAfRow]{
							{
								TableSaf: bgpSafTable{
									RowSaf: rowList[bgpSafRow]{
										{
											TableNeighbor: bgpNeighborTable{
												RowNeighbor: rowList[bgpNeighborRow]{
													{NeighborID: "192.0.2.1", RemoteAS: "65000", State: "Established", PfxReceived: "10"},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	connections, stubs := TransformBGPNeighbors("res-switch-01", bgp)
	if len(connections) != 1 || len(stubs) != 1 {
		t.Fatalf("expected 1 connection/stub, got %d/%d", len(connections), len(stubs))
	}
	if connections[0].Type != "network.bgp" {
		t.Errorf("connection type = %s, want network.bgp", connections[0].Type)
	}
	if connections[0].Source != "res-switch-01" {
		t.Errorf("connection source = %s, want res-switch-01", connections[0].Source)
	}
	if connections[0].Properties["state"] != "Established" {
		t.Errorf("state: %v", connections[0].Properties["state"])
	}
	if connections[0].Properties["prefixes_received"] != "10" {
		t.Errorf("prefixes_received: %v", connections[0].Properties["prefixes_received"])
	}
	if connections[0].Properties["vrf"] != "default" {
		t.Errorf("vrf: %v", connections[0].Properties["vrf"])
	}
	if stubs[0].Properties["remote_asn"] != "65000" {
		t.Errorf("remote_asn: %v", stubs[0].Properties["remote_asn"])
	}
}

func TestTransformBGPNeighbors_NoPeers(t *testing.T) {
	connections, stubs := TransformBGPNeighbors("res-switch-01", bgpSummaryResponse{})
	if len(connections) != 0 || len(stubs) != 0 {
		t.Errorf("expected no connections/stubs for an empty response, got %d/%d", len(connections), len(stubs))
	}
}

func TestTransformBGPNeighbors_SamePeerAcrossAddressFamiliesDeduplicates(t *testing.T) {
	// Same bug class as the OSPF fix: "show bgp all summary" enumerates
	// neighbors per address-family table, so a dual-stack/multi-AF peer
	// configuration can appear more than once for the same neighbor IP.
	// Both the stub resource ID and the connection ID (device -> peer,
	// unqualified by address family) are derived from the neighbor IP
	// alone a second row must not produce a second resource/connection
	// that collides with the first.
	bgp := bgpSummaryResponse{
		TableVRF: bgpVRFTable{
			RowVRF: rowList[bgpVRFRow]{
				{
					VRFNameOut: "default",
					TableAf: bgpAfTable{
						RowAf: rowList[bgpAfRow]{
							{
								TableSaf: bgpSafTable{
									RowSaf: rowList[bgpSafRow]{
										{TableNeighbor: bgpNeighborTable{RowNeighbor: rowList[bgpNeighborRow]{
											{NeighborID: "192.0.2.1", RemoteAS: "65000", State: "Established"},
										}}},
									},
								},
							},
							{
								TableSaf: bgpSafTable{
									RowSaf: rowList[bgpSafRow]{
										{TableNeighbor: bgpNeighborTable{RowNeighbor: rowList[bgpNeighborRow]{
											{NeighborID: "192.0.2.1", RemoteAS: "65000", State: "Established"},
										}}},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	connections, stubs := TransformBGPNeighbors("res-switch-01", bgp)
	if len(connections) != 1 {
		t.Fatalf("expected 1 deduplicated connection, got %d", len(connections))
	}
	if len(stubs) != 1 {
		t.Fatalf("expected 1 deduplicated stub, got %d", len(stubs))
	}
}
