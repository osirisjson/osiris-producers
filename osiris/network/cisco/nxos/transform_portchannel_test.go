// transform_portchannel_test.go - Unit tests for
// transform_portchannel.go.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package nxos

import (
	"testing"

	"go.osirisjson.org/producers/pkg/sdk"
)

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
	if g.Type != "osiris.cisco.vpc" {
		t.Errorf("type: %s (Cisco vPC != network.vpc)", g.Type)
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
					Protocol:    "LACP",
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
		{ID: "res-pc-10", Type: "network.interface", Name: "port-channel10"},
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
	if resources[0].Properties["protocol"] != "LACP" {
		t.Errorf("port-channel protocol = %v, want LACP", resources[0].Properties["protocol"])
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
		{ID: "res-pc-10", Type: "network.interface", Name: "port-channel10"},
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

func TestTransformVPCKeepalive_Configured(t *testing.T) {
	keepalive := vpcPeerKeepaliveResponse{
		Status:      "peer is alive",
		Destination: "198.51.100.5",
		VRF:         "management",
	}

	conn, stub := TransformVPCKeepalive("res-switch-01", "LAB-SW01", keepalive)
	if conn == nil || stub == nil {
		t.Fatal("expected a connection and stub for a configured keepalive destination")
	}
	if conn.Type != "network" {
		t.Errorf("connection type = %s, want network", conn.Type)
	}
	if conn.Source != "res-switch-01" {
		t.Errorf("connection source = %s, want res-switch-01", conn.Source)
	}
	cisco, ok := conn.Extensions[extensionNamespace].(map[string]any)
	if !ok {
		t.Fatal("expected osiris.cisco extension on the connection")
	}
	if cisco["role"] != "vpc_keepalive" {
		t.Errorf("role: %v", cisco["role"])
	}
	if cisco["keepalive_status"] != "peer is alive" {
		t.Errorf("keepalive_status: %v", cisco["keepalive_status"])
	}
	if _, ok := conn.Properties["role"]; ok {
		t.Error("role should no longer live in bare properties")
	}
	if stub.Provider.Name != unknownProviderName {
		t.Errorf("stub provider = %s, want %s", stub.Provider.Name, unknownProviderName)
	}
	if stub.Properties["remote_mgmt_addr"] != "198.51.100.5" {
		t.Errorf("remote_mgmt_addr: %v", stub.Properties["remote_mgmt_addr"])
	}
	if stub.Properties["vrf"] != "management" {
		t.Errorf("vrf: %v", stub.Properties["vrf"])
	}
}

func TestTransformVPCKeepalive_NotConfigured(t *testing.T) {
	cases := []vpcPeerKeepaliveResponse{
		{},
		{Destination: "N/A"},
	}
	for _, kc := range cases {
		conn, stub := TransformVPCKeepalive("res-switch-01", "LAB-SW01", kc)
		if conn != nil || stub != nil {
			t.Errorf("expected nil connection/stub for destination %q", kc.Destination)
		}
	}
}
