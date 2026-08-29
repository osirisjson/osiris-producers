// transform_interfaces_test.go - Unit tests for transform_interfaces.go.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package nxos

import (
	"testing"

	"go.osirisjson.org/producers/pkg/sdk"
)

func TestTransformInterfaces(t *testing.T) {
	ifBrief := interfaceBriefResponse{
		TableInterface: interfaceTable{
			RowInterface: rowList[interfaceBriefRow]{
				{Interface: "Ethernet1/1", State: "up", Speed: "100000", PortMode: "access", VLAN: "100"},
				{Interface: "Ethernet1/2", State: "down", Speed: "10G", VLAN: "200"}, // symbolic speed
				{Interface: "port-channel10", State: "up", Speed: "20000"},
				{Interface: "loopback0", State: "up"},
				{Interface: "mgmt0", State: "up"},
			},
		},
	}

	resources, nameToID := TransformInterfaces("TST0000NX01", ifBrief)

	if len(resources) != 5 {
		t.Fatalf("expected 5 resources, got %d", len(resources))
	}
	if len(nameToID) != 5 {
		t.Fatalf("expected 5 name mappings, got %d", len(nameToID))
	}

	// Ethernet1/1: physical port with every sourced property.
	eth1 := resources[0]
	if eth1.Type != "network.switch.port" {
		t.Errorf("Ethernet should be network.switch.port, got %s", eth1.Type)
	}
	if eth1.ID != "cisco.nxos::TST0000NX01/Ethernet1/1" {
		t.Errorf("id should be <provider>::<device-serial>/<interface-name>, got %q", eth1.ID)
	}
	if nameToID["Ethernet1/1"] != eth1.ID {
		t.Errorf("nameToID should agree with the resource's own id: %q vs %q", nameToID["Ethernet1/1"], eth1.ID)
	}
	if eth1.Status != "active" {
		t.Errorf("up interface should be active, got %s", eth1.Status)
	}
	if eth1.Properties["interface_name"] != "Ethernet1/1" {
		t.Errorf("interface_name: %v", eth1.Properties["interface_name"])
	}
	if eth1.Properties["speed_mbps"] != 100000 {
		t.Errorf("speed_mbps: %v", eth1.Properties["speed_mbps"])
	}
	if eth1.Properties["port_mode"] != "access" {
		t.Errorf("port_mode: %v", eth1.Properties["port_mode"])
	}
	if eth1.Properties["vlan"] != 100 {
		t.Errorf("vlan should decode as an int, got %v (%T)", eth1.Properties["vlan"], eth1.Properties["vlan"])
	}

	// Ethernet1/2: down, and a symbolic (non-numeric) speed must be
	// omitted rather than guessed at a number.
	eth2 := resources[1]
	if eth2.Status != "inactive" {
		t.Errorf("down interface should be inactive, got %s", eth2.Status)
	}
	if _, ok := eth2.Properties["speed_mbps"]; ok {
		t.Errorf("symbolic speed %q should not decode to speed_mbps", "10G")
	}

	// port-channel10: LAG bucket, no interface_name (not in
	// network.interface's own property table).
	pc := resources[2]
	if pc.Type != "network.interface.lag" {
		t.Errorf("port-channel should be network.interface.lag, got %s", pc.Type)
	}
	if _, ok := pc.Properties["interface_name"]; ok {
		t.Error("interface_name is a network.switch.port-only property")
	}
	if pc.Properties["speed_mbps"] != 20000 {
		t.Errorf("speed_mbps: %v", pc.Properties["speed_mbps"])
	}

	// loopback0: logical bucket.
	if resources[3].Type != "network.interface" {
		t.Errorf("loopback should be network.interface, got %s", resources[3].Type)
	}

	// mgmt0: physical port despite being out-of-band - it has a real
	// transceiver slot on the chassis.
	if resources[4].Type != "network.switch.port" {
		t.Errorf("mgmt0 should be network.switch.port, got %s", resources[4].Type)
	}
}

// TestTransformInterfaces_SVIUsesAdminStateFallback covers real-device
// row shape: VLAN SVIs report only svi_admin_state, never state. Before
// this fallback, every SVI resource fell through to Status "unknown"
// with no admin_status property at all even one carrying a live OSPF
// adjacency in the capture this fix was grounded against.
func TestTransformInterfaces_SVIUsesAdminStateFallback(t *testing.T) {
	ifBrief := interfaceBriefResponse{
		TableInterface: interfaceTable{
			RowInterface: rowList[interfaceBriefRow]{
				{Interface: "Vlan900", SVIAdminState: "up"},
				{Interface: "Vlan901", SVIAdminState: "down"},
			},
		},
	}

	resources, _ := TransformInterfaces("TST0000NX01", ifBrief)

	if resources[0].Status != "active" {
		t.Errorf("Vlan900 status = %s, want active", resources[0].Status)
	}
	if resources[0].Properties["admin_status"] != "up" {
		t.Errorf("Vlan900 admin_status = %v, want up", resources[0].Properties["admin_status"])
	}
	if resources[1].Status != "inactive" {
		t.Errorf("Vlan901 status = %s, want inactive", resources[1].Status)
	}
}

func TestClassifyInterfaceType(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Ethernet1/1", "network.switch.port"},
		{"Ethernet1/1/1", "network.switch.port"},
		{"mgmt0", "network.switch.port"},
		{"port-channel10", "network.interface.lag"},
		{"Port-channel1", "network.interface.lag"},
		{"loopback0", "network.interface"},
		{"Vlan100", "network.interface"},
		{"Tunnel0", "network.interface"},
	}
	for _, tc := range cases {
		if got := classifyInterfaceType(tc.in); got != tc.want {
			t.Errorf("classifyInterfaceType(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestEnrichInterfaceIPs(t *testing.T) {
	ipBrief := ipInterfaceBriefResponse{
		TableIntf: ipInterfaceTable{
			RowIntf: rowList[ipInterfaceRow]{
				{IntfName: "Vlan100", Prefix: "203.0.113.1", VRFName: "default"},
				{IntfName: "Vlan200", Prefix: ""}, // no address assigned
			},
		},
	}
	ifNameToID := map[string]string{"Vlan100": "res-vlan100", "Vlan200": "res-vlan200"}
	resources := []sdk.Resource{
		{ID: "res-vlan100", Type: "network.interface", Name: "Vlan100"},
		{ID: "res-vlan200", Type: "network.interface", Name: "Vlan200"},
	}

	EnrichInterfaceIPs(ipBrief, resources, ifNameToID)

	if resources[0].Properties["ip_address"] != "203.0.113.1" {
		t.Errorf("ip_address: %v", resources[0].Properties["ip_address"])
	}
	if resources[1].Properties != nil {
		if _, ok := resources[1].Properties["ip_address"]; ok {
			t.Error("Vlan200 has no reported prefix and should not get ip_address")
		}
	}
}

func TestEnrichSwitchportDetails(t *testing.T) {
	switchport := switchportResponse{
		TableInterface: switchportTable{
			RowInterface: rowList[switchportRow]{
				{Interface: "Ethernet1/1", NativeVLAN: "100"},
				{Interface: "Ethernet1/2", NativeVLAN: ""}, // not reported
			},
		},
	}
	ifNameToID := map[string]string{"Ethernet1/1": "res-eth1-1", "Ethernet1/2": "res-eth1-2"}
	resources := []sdk.Resource{
		{ID: "res-eth1-1", Type: "network.switch.port", Name: "Ethernet1/1"},
		{ID: "res-eth1-2", Type: "network.switch.port", Name: "Ethernet1/2"},
	}

	EnrichSwitchportDetails(switchport, resources, ifNameToID)

	if resources[0].Properties["native_vlan"] != 100 {
		t.Errorf("native_vlan: %v (%T)", resources[0].Properties["native_vlan"], resources[0].Properties["native_vlan"])
	}
	if resources[1].Properties != nil {
		if _, ok := resources[1].Properties["native_vlan"]; ok {
			t.Error("Ethernet1/2 has no reported native VLAN and should not get the property")
		}
	}
}

func TestTransformDeviceContainment(t *testing.T) {
	ifResources := []sdk.Resource{
		{ID: "res-eth-1-1", Type: "network.switch.port", Name: "Ethernet1/1"},
		{ID: "res-eth-1-2", Type: "network.switch.port", Name: "Ethernet1/2"},
		{ID: "res-pc-10", Type: "network.interface", Name: "port-channel10"},
	}

	connections := TransformDeviceContainment("res-switch-01", "LAB-SW01", ifResources)
	if len(connections) != 3 {
		t.Fatalf("expected 3 containment connections, got %d", len(connections))
	}

	var physical, logical int
	for _, c := range connections {
		if c.Source != "res-switch-01" {
			t.Errorf("connection source = %s, want res-switch-01", c.Source)
		}
		if c.Direction != "forward" {
			t.Errorf("connection direction = %s, want forward", c.Direction)
		}
		if c.Status != "active" {
			t.Errorf("connection status = %s, want active", c.Status)
		}
		switch c.Type {
		case "contains.physical":
			physical++
		case "contains.logical":
			logical++
		default:
			t.Errorf("unexpected connection type: %s", c.Type)
		}
	}
	if physical != 2 {
		t.Errorf("expected 2 contains.physical connections, got %d", physical)
	}
	if logical != 1 {
		t.Errorf("expected 1 contains.logical connection, got %d", logical)
	}
}

func TestTransformDeviceContainment_SkipsUnrelatedType(t *testing.T) {
	// A resource type this function doesn't own (defensive in
	// practice ifResources only ever holds TransformInterfaces output)
	// must not produce a connection.
	ifResources := []sdk.Resource{
		{ID: "res-remote-1", Type: "osiris.cisco.something", Name: "remote"},
	}
	connections := TransformDeviceContainment("res-switch-01", "LAB-SW01", ifResources)
	if len(connections) != 0 {
		t.Errorf("expected 0 connections, got %d", len(connections))
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
