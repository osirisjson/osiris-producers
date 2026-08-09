// transform_interfaces_test.go - Unit tests for interface resource and
// "contains" connection mapping.
//
// OSIRIS JSON Producer for Cisco SD-WAN Manager (vManage) introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package vmanage

import "testing"

func TestTransformInterfaces_WANInterfaceGetsBothAddresses(t *testing.T) {
	ifaces := []Interface{
		{IfName: "GigabitEthernet1", AFType: "ipv4", IPAddress: "192.0.2.0/24", HWAddr: "00:00:5E:00:53:00", IfAdminStatus: "Up", IfOperStatus: "Up", PortType: "transport", Duplex: "full", Mtu: "1500", SpeedMbps: "1000", EncapType: "dot1q"},
	}
	wanIfaces := []WANInterface{
		{Interface: "GigabitEthernet1", Color: "lte", PrivateIP: "192.0.2.10", PublicIP: "203.0.113.10", NatType: "E"},
	}

	resources, connections, tunnelIndex := TransformInterfaces("cisco.vmanage::TST0000001", "network.router", "TST0000001", "192.0.2.10", ifaces, wanIfaces)

	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	r := resources[0]
	wantID := "cisco.vmanage::TST0000001-GigabitEthernet1"
	if r.ID != wantID {
		t.Errorf("resource ID = %q, want %q", r.ID, wantID)
	}
	if r.Type != "network.router.port" {
		t.Errorf("type = %q, want network.router.port", r.Type)
	}
	if r.Provider.Type != "GigabitEthernet1" {
		t.Errorf("provider.type = %q, want %q", r.Provider.Type, "GigabitEthernet1")
	}
	ipAddrs := r.Properties["ip_addresses"].(map[string]any)
	if ipAddrs["public_ip"] != "203.0.113.10" {
		t.Errorf("public_ip = %v, want 203.0.113.10", ipAddrs["public_ip"])
	}
	privateIP := ipAddrs["private_ip"].([]string)
	if len(privateIP) != 1 || privateIP[0] != "192.0.2.10" {
		t.Errorf("private_ip = %v, want [192.0.2.10]", privateIP)
	}
	if r.Properties["interface_name"] != "GigabitEthernet1" {
		t.Errorf("interface_name = %v", r.Properties["interface_name"])
	}
	if r.Properties["mac_address"] != "00:00:5E:00:53:00" {
		t.Errorf("mac_address = %v", r.Properties["mac_address"])
	}
	if r.Properties["admin_status"] != "up" {
		t.Errorf("admin_status = %v, want up", r.Properties["admin_status"])
	}
	if r.Properties["oper_status"] != "up" {
		t.Errorf("oper_status = %v, want up", r.Properties["oper_status"])
	}
	if r.Properties["speed_mbps"] != 1000 {
		t.Errorf("speed_mbps = %v (%T), want 1000 (int)", r.Properties["speed_mbps"], r.Properties["speed_mbps"])
	}
	if r.Properties["duplex"] != "full" {
		t.Errorf("duplex = %v, want full", r.Properties["duplex"])
	}
	if r.Properties["mtu"] != 1500 {
		t.Errorf("mtu = %v (%T), want 1500 (int)", r.Properties["mtu"], r.Properties["mtu"])
	}
	if r.Properties["encapsulation"] != "dot1q" {
		t.Errorf("encapsulation = %v, want dot1q", r.Properties["encapsulation"])
	}
	if r.Properties["interface_type"] != "primary" {
		t.Errorf("interface_type = %v, want primary", r.Properties["interface_type"])
	}
	if r.State != "up" {
		t.Errorf("state = %q, want up", r.State)
	}
	if r.Status != "active" {
		t.Errorf("status = %q, want active", r.Status)
	}
	ext := r.Extensions[extensionKey].(map[string]any)
	if ext["color"] != "lte" {
		t.Errorf("extensions color = %v, want lte", ext["color"])
	}

	if len(connections) != 1 {
		t.Fatalf("expected 1 contains connection, got %d", len(connections))
	}
	c := connections[0]
	if c.Type != "contains" || c.Source != "cisco.vmanage::TST0000001" || c.Target != wantID {
		t.Errorf("unexpected connection: %+v", c)
	}
	if c.Direction != "forward" {
		t.Errorf("direction = %q, want forward", c.Direction)
	}

	if got := tunnelIndex["198.51.100.10:lte"]; got != wantID {
		t.Errorf("tunnelIndex[198.51.100.10:lte] = %q, want %q", got, wantID)
	}
}

func TestTransformInterfaces_DirectInternetAccessNotMislabeledPrivate(t *testing.T) {
	// A Direct Internet Access (DIA) circuit with no NAT reports the
	// SAME public ISP-assigned address in both
	// GET /dataservice/device/control/waninterface's "private-ip" and
	// "public-ip" fields those field names mean "before NAT"/"after
	// NAT", not "RFC 1918 private"/"public". Trusting the field name
	// instead of the actual address would mislabel a public address as
	// private_ip.
	ifaces := []Interface{
		{IfName: "GigabitEthernet0", AFType: "ipv4", IPAddress: "203.0.113.50/30", PortType: "transport"},
	}
	wanIfaces := []WANInterface{
		{Interface: "GigabitEthernet0", Color: "silver", PrivateIP: "203.0.113.50", PublicIP: "203.0.113.50", NatType: "N"},
	}

	resources, _, _ := TransformInterfaces("cisco.vmanage::TST0000001", "network.router", "TST0000001", "203.0.113.50", ifaces, wanIfaces)

	ipAddrs := resources[0].Properties["ip_addresses"].(map[string]any)
	if ipAddrs["public_ip"] != "203.0.113.50" {
		t.Errorf("public_ip = %v, want 203.0.113.50", ipAddrs["public_ip"])
	}
	if _, ok := ipAddrs["private_ip"]; ok {
		t.Errorf("private_ip = %v, want absent - 203.0.113.50 is a real public address, not RFC 1918 private", ipAddrs["private_ip"])
	}
}

func TestTransformInterfaces_ControllerOwnerKeepsGenericType(t *testing.T) {
	ifaces := []Interface{
		{IfName: "eth0", AFType: "ipv4", IPAddress: "192.0.2.0/24"},
	}

	resources, _, _ := TransformInterfaces("cisco.vmanage::TST0000099", "osiris.cisco.controller", "TST0000099", "", ifaces, nil)

	if resources[0].Type != "network.interface" {
		t.Errorf("type = %q, want network.interface for a non-router owner", resources[0].Type)
	}
}

func TestTransformInterfaces_VPNID(t *testing.T) {
	ifaces := []Interface{
		{IfName: "GigabitEthernet2", AFType: "ipv4", IPAddress: "192.0.2.1/24", PortType: "service", VPNID: "10"},
		{IfName: "GigabitEthernet3", AFType: "ipv4", IPAddress: "192.0.2.2/24", PortType: "service"},
	}

	resources, _, _ := TransformInterfaces("cisco.vmanage::TST0000001", "network.router", "TST0000001", "", ifaces, nil)

	if resources[0].Properties["vpn_id"] != "10" {
		t.Errorf("properties.vpn_id = %v, want %q", resources[0].Properties["vpn_id"], "10")
	}
	if _, ok := resources[0].Properties["vrf_id"]; ok {
		t.Error("vrf_id should not be emitted - vManage's raw data only ever reports vpn-id, never vrf")
	}
	if _, ok := resources[1].Properties["vpn_id"]; ok {
		t.Error("empty vpn-id should not produce a vpn_id property")
	}
}

func TestTransformInterfaces_Description(t *testing.T) {
	ifaces := []Interface{
		{IfName: "GigabitEthernet2", AFType: "ipv4", IPAddress: "192.0.2.1/24", PortType: "transport", Description: "TEST WAN UPLINK"},
		{IfName: "GigabitEthernet3", AFType: "ipv4", IPAddress: "192.0.2.2/24", PortType: "service"},
	}

	resources, _, _ := TransformInterfaces("cisco.vmanage::TST0000001", "network.router", "TST0000001", "", ifaces, nil)

	if resources[0].Description != "TEST WAN UPLINK" {
		t.Errorf("Description = %q, want %q", resources[0].Description, "TEST WAN UPLINK")
	}
	if resources[1].Description != "" {
		t.Errorf("Description = %q, want empty for an interface with no description reported", resources[1].Description)
	}
}

func TestTransformInterfaces_NonWANInterfaceHasNoPublicIP(t *testing.T) {
	ifaces := []Interface{
		{IfName: "GigabitEthernet2", AFType: "ipv4", IPAddress: "192.0.2.1/24", PortType: "service", IfOperStatus: "Up"},
	}

	resources, _, tunnelIndex := TransformInterfaces("cisco.vmanage::TST0000001", "network.router", "TST0000001", "192.0.2.10", ifaces, nil)

	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	ipAddrs := resources[0].Properties["ip_addresses"].(map[string]any)
	if _, ok := ipAddrs["public_ip"]; ok {
		t.Error("non-WAN interface should not have a public_ip")
	}
	if resources[0].Properties["interface_type"] != "secondary" {
		t.Errorf("interface_type = %v, want secondary", resources[0].Properties["interface_type"])
	}
	if len(tunnelIndex) != 0 {
		t.Errorf("non-WAN interface should not be indexed for tunnel resolution, got %v", tunnelIndex)
	}
}

func TestTransformInterfaces_NoAddressPlaceholderOmitted(t *testing.T) {
	ifaces := []Interface{
		{IfName: "GigabitEthernet3", AFType: "ipv4", IPAddress: "0.0.0.0", PortType: "service"},
	}

	resources, _, _ := TransformInterfaces("cisco.vmanage::TST0000001", "network.router", "TST0000001", "", ifaces, nil)

	if _, ok := resources[0].Properties["ip_addresses"]; ok {
		t.Errorf("ip_addresses = %v, want absent - 0.0.0.0 is vManage's placeholder for no address configured", resources[0].Properties["ip_addresses"])
	}
}

func TestParseIntField(t *testing.T) {
	cases := []struct {
		in     string
		want   int
		wantOK bool
	}{
		{"1500", 1500, true},
		{"", 0, false},
		{"not-a-number", 0, false},
	}
	for _, c := range cases {
		got, ok := parseIntField(c.in)
		if ok != c.wantOK || (ok && got != c.want) {
			t.Errorf("parseIntField(%q) = (%d, %v), want (%d, %v)", c.in, got, ok, c.want, c.wantOK)
		}
	}
}

func TestSelectIPv4Interfaces_PrefersIPv4Row(t *testing.T) {
	ifaces := []Interface{
		{IfName: "GigabitEthernet1", AFType: "ipv6", IPAddress: "2001:DB8::/32"},
		{IfName: "GigabitEthernet1", AFType: "ipv4", IPAddress: "192.0.2.0/24"},
	}
	selected := selectIPv4Interfaces(ifaces)
	if len(selected) != 1 {
		t.Fatalf("expected 1 deduplicated interface, got %d", len(selected))
	}
	if selected[0].AFType != "ipv4" {
		t.Errorf("expected the ipv4 row to be selected, got af-type=%q", selected[0].AFType)
	}
}

func TestInterfaceType(t *testing.T) {
	cases := []struct{ portType, want string }{
		{"transport", "primary"},
		{"service", "secondary"},
		{"", ""},
		{"unknown", ""},
	}
	for _, c := range cases {
		if got := interfaceType(c.portType); got != c.want {
			t.Errorf("interfaceType(%q) = %q, want %q", c.portType, got, c.want)
		}
	}
}

func TestMapUpDownStatus(t *testing.T) {
	cases := []struct{ state, want string }{
		{"Up", "active"},
		{"up", "active"},
		{"Down", "inactive"},
		{"", "unknown"},
		// cEdge (IOS-XE) ietf-interfaces oper-status vocabulary not
		// documented anywhere in the vManage OpenAPI spec.
		{"if-oper-state-ready", "active"},
		{"if-oper-state-no-pass", "degraded"},
		{"if-oper-state-down", "inactive"},
		{"if-oper-state-lower-layer-down", "inactive"},
		{"if-oper-state-not-present", "inactive"},
		{"if-oper-state-testing", "unknown"},
	}
	for _, c := range cases {
		if got := mapUpDownStatus(c.state); got != c.want {
			t.Errorf("mapUpDownStatus(%q) = %q, want %q", c.state, got, c.want)
		}
	}
}
