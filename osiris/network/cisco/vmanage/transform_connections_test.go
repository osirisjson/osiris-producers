// transform_connections_test.go - Unit tests for tunnel and OMP peering
// connection mapping.
//
// OSIRIS JSON Producer for Cisco SD-WAN Manager (vManage) introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package vmanage

import "testing"

func TestParseTunnelEndpoints(t *testing.T) {
	cases := []struct {
		name         string
		wantA, wantB string
		wantOK       bool
	}{
		{"198.51.100.1:lte-198.51.100.2:lte", "198.51.100.1:lte", "198.51.100.2:lte", true},
		{"203.0.113.1:biz-internet-203.0.113.2:public-internet", "203.0.113.1:biz-internet", "203.0.113.2:public-internet", true},
		{"not-a-valid-tunnel-name", "", "", false},
	}
	for _, c := range cases {
		gotA, gotB, ok := parseTunnelEndpoints(c.name)
		if ok != c.wantOK {
			t.Errorf("parseTunnelEndpoints(%q) ok = %v, want %v", c.name, ok, c.wantOK)
			continue
		}
		if !ok {
			continue
		}
		if gotA != c.wantA || gotB != c.wantB {
			t.Errorf("parseTunnelEndpoints(%q) = (%q, %q), want (%q, %q)", c.name, gotA, gotB, c.wantA, c.wantB)
		}
	}
}

func TestTransformTunnels_ResolvesBothEndpoints(t *testing.T) {
	devices := []SiteTopologyDevice{
		{
			DeviceID: "TST0000001",
			Circuits: []SiteCircuit{
				{
					Color:    "lte",
					SystemIP: "192.0.2.10",
					Tunnels: []SiteTunnel{
						{Name: "198.51.100.1:lte-198.51.100.2:lte", State: "Up", VqoeScore: 9},
					},
				},
			},
		},
	}
	ifaceIndex := map[string]string{
		"198.51.100.1:lte": "cisco.vmanage::TST0000001-GigabitEthernet1",
		"198.51.100.2:lte": "cisco.vmanage::TST0000002-GigabitEthernet1",
	}

	connections := TransformTunnels(devices, ifaceIndex)
	if len(connections) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(connections))
	}
	c := connections[0]
	if c.Type != "network.vpn" {
		t.Errorf("type = %q, want network.vpn", c.Type)
	}
	if c.Status != "active" || c.State != "up" {
		t.Errorf("status/state = %q/%q, want active/up", c.Status, c.State)
	}
	ext := c.Extensions[extensionKey].(map[string]any)
	if ext["color"] != "lte" || ext["vqoe_score"] != float64(9) {
		t.Errorf("unexpected extensions: %+v", ext)
	}
}

func TestTransformTunnels_SkipsUnresolvedPeer(t *testing.T) {
	devices := []SiteTopologyDevice{
		{
			Circuits: []SiteCircuit{
				{
					Color:    "lte",
					SystemIP: "192.0.2.10",
					// 198.51.100.99 belongs to different site document
					// and is not in ifaceIndex read OSIRIS-JSON-v1.0 section 2.2.3.
					Tunnels: []SiteTunnel{{Name: "198.51.100.1:lte-198.51.100.99:lte", State: "Up"}},
				},
			},
		},
	}
	ifaceIndex := map[string]string{
		"198.51.100.1:lte": "cisco.vmanage::TST0000001-GigabitEthernet1",
	}

	connections := TransformTunnels(devices, ifaceIndex)
	if len(connections) != 0 {
		t.Fatalf("expected 0 connections for an unresolvable peer, got %d", len(connections))
	}
}

func TestTransformTunnels_DedupesSymmetricEntries(t *testing.T) {
	// The same tunnel appears once under each device's own circuit
	// list, in opposite name order.
	devices := []SiteTopologyDevice{
		{Circuits: []SiteCircuit{{Color: "lte", SystemIP: "192.0.2.10", Tunnels: []SiteTunnel{{Name: "198.51.100.1:lte-198.51.100.2:lte", State: "Up"}}}}},
		{Circuits: []SiteCircuit{{Color: "lte", SystemIP: "192.0.2.11", Tunnels: []SiteTunnel{{Name: "198.51.100.2:lte-198.51.100.1:lte", State: "Up"}}}}},
	}
	ifaceIndex := map[string]string{
		"198.51.100.1:lte": "cisco.vmanage::TST0000001-GigabitEthernet1",
		"198.51.100.2:lte": "cisco.vmanage::TST0000002-GigabitEthernet1",
	}

	connections := TransformTunnels(devices, ifaceIndex)
	if len(connections) != 1 {
		t.Fatalf("expected symmetric tunnel entries to dedupe to 1 connection, got %d", len(connections))
	}
}

func TestTransformOMPLinks_ResolvesBothEndpoints(t *testing.T) {
	links := []OMPLink{
		{State: "up", ASystemIP: "192.0.2.10", BSystemIP: "192.0.2.11"},
	}
	systemIPToDeviceID := map[string]string{
		"192.0.2.10": "cisco.vmanage::TST0000001",
		"192.0.2.11": "cisco.vmanage::TST0000099",
	}

	connections := TransformOMPLinks(links, systemIPToDeviceID)
	if len(connections) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(connections))
	}
	if connections[0].Type != "network" {
		t.Errorf("type = %q, want network", connections[0].Type)
	}
}

func TestTransformOMPPeers_SkipsUnresolvedAndSelf(t *testing.T) {
	peers := []OMPPeer{
		{Peer: "192.0.2.11", State: "up"},    // resolvable
		{Peer: "198.51.100.99", State: "up"}, // unresolvable (different site)
		{Peer: "192.0.2.10", State: "up"},    // self-loop
	}
	systemIPToDeviceID := map[string]string{
		"192.0.2.10": "cisco.vmanage::TST0000001",
		"192.0.2.11": "cisco.vmanage::TST0000099",
	}

	connections := TransformOMPPeers("cisco.vmanage::TST0000001", peers, systemIPToDeviceID)
	if len(connections) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(connections))
	}
	if connections[0].Direction != "forward" {
		t.Errorf("direction = %q, want forward", connections[0].Direction)
	}
}

func TestTransformOMPLinksAndPeers_DedupeAcrossSources(t *testing.T) {
	links := []OMPLink{
		{State: "up", ASystemIP: "192.0.2.10", BSystemIP: "192.0.2.11"},
	}
	peers := []OMPPeer{
		{Peer: "10.0.1.1", State: "up"},
	}
	systemIPToDeviceID := map[string]string{
		"192.0.2.10": "cisco.vmanage::TST0000001",
		"192.0.2.11": "cisco.vmanage::TST0000099",
	}

	linkConns := TransformOMPLinks(links, systemIPToDeviceID)
	peerConns := TransformOMPPeers("cisco.vmanage::TST0000001", peers, systemIPToDeviceID)

	merged := dedupeConnections(append(linkConns, peerConns...))
	if len(merged) != 1 {
		t.Fatalf("expected omp/links and omp/peers describing the same pair to dedupe to 1 connection, got %d", len(merged))
	}
}
