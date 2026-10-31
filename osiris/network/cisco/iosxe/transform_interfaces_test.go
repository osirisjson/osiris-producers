// transform_interfaces_test.go - Unit tests for interface resources,
// address normalization, device containment and VRF binding
// (transform_interfaces.go).
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package iosxe

import (
	"fmt"
	"testing"

	"go.osirisjson.org/producers/pkg/sdk"
)

// interfacesCases are the synthetic device replies the interfaces unit
// tests decode: one minimal NETCONF <data> payload per case, keyed by
// the behavior it exercises.
var interfacesCases = map[string]string{
	"boot-list":    `<native xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-native"><boot><system><bootfile><filename-list><filename>bootflash:lab-a.bin</filename></filename-list></bootfile></system></boot></native>`,
	"boot-none":    `<native xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-native"/>`,
	"boot-ordered": `<native xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-native"><boot><system><bootfile><filename-list-ordered-by-user><filename>bootflash:lab-b.bin</filename></filename-list-ordered-by-user><filename-list-ordered-by-user><filename>bootflash:lab-a.bin</filename></filename-list-ordered-by-user><filename-list><filename>bootflash:lab-a.bin</filename></filename-list></bootfile></system></boot></native>`,
	"config": `<interfaces xmlns="urn:ietf:params:xml:ns:yang:ietf-interfaces">
  <interface><name>GigabitEthernet1</name><description>LAB uplink</description><type xmlns:ianaift="urn:ietf:params:xml:ns:yang:iana-if-type">ianaift:ethernetCsmacd</type><enabled>true</enabled>
    <ipv4 xmlns="urn:ietf:params:xml:ns:yang:ietf-ip">
      <address><ip>192.0.2.1</ip><netmask>255.255.255.0</netmask></address>
      <address><ip>192.0.2.65</ip><netmask>255.255.255.192</netmask></address>
      <address><ip>192.0.2.1</ip><netmask>255.255.255.0</netmask></address>
      <address><ip>0.0.0.0</ip><netmask>0.0.0.0</netmask></address>
      <address><ip>192.0.2.9</ip><netmask>255.0.255.0</netmask></address>
    </ipv4>
    <ipv6 xmlns="urn:ietf:params:xml:ns:yang:ietf-ip">
      <address><ip>2001:DB8:0:0::1</ip><prefix-length>64</prefix-length></address>
      <address><ip>2001:db8::1</ip><prefix-length>64</prefix-length></address>
      <address><ip>::</ip><prefix-length>0</prefix-length></address>
      <address><ip>2001:db8::2</ip><prefix-length>129</prefix-length></address>
    </ipv6>
  </interface>
  <interface><name>GigabitEthernet1.100</name><type xmlns:ianaift="urn:ietf:params:xml:ns:yang:iana-if-type">ianaift:l2vlan</type><enabled>true</enabled></interface>
  <interface><name>GigabitEthernet2</name><type xmlns:ianaift="urn:ietf:params:xml:ns:yang:iana-if-type">ianaift:ethernetCsmacd</type><enabled>false</enabled></interface>
  <interface><name>Loopback0</name><type xmlns:ianaift="urn:ietf:params:xml:ns:yang:iana-if-type">ianaift:softwareLoopback</type><enabled>true</enabled>
    <ipv4 xmlns="urn:ietf:params:xml:ns:yang:ietf-ip"><address><ip>203.0.113.1</ip><netmask>255.255.255.255</netmask></address></ipv4>
  </interface>
  <interface><name>Port-channel1</name><type xmlns:ianaift="urn:ietf:params:xml:ns:yang:iana-if-type">ianaift:ieee8023adLag</type><enabled>true</enabled></interface>
  <interface><name>Vlan10</name><type xmlns:ianaift="urn:ietf:params:xml:ns:yang:iana-if-type">ianaift:l3ipvlan</type><enabled>true</enabled></interface>
</interfaces>`,
	"oper": `<interfaces xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-interfaces-oper">
  <interface><name>GigabitEthernet1</name><interface-type>iana-iftype-ethernet-csmacd</interface-type><admin-status>if-state-up</admin-status><oper-status>if-oper-state-ready</oper-status><phys-address>00:00:5E:00:53:01</phys-address><speed>1000000000</speed><statistics><in-octets>1</in-octets></statistics><vrf>LAB-A</vrf><ipv4>192.0.2.1</ipv4><ipv4-subnet-mask>255.255.255.0</ipv4-subnet-mask><description>LAB uplink</description><mtu>1500</mtu><ether-state><negotiated-duplex-mode>full-duplex</negotiated-duplex-mode><negotiated-port-speed>speed-1gb</negotiated-port-speed></ether-state></interface>
  <interface><name>GigabitEthernet2</name><interface-type>iana-iftype-ethernet-csmacd</interface-type><admin-status>if-state-down</admin-status><oper-status>if-oper-state-no-pass</oper-status><phys-address>00:00:5e:00:53:02</phys-address><speed>1000000000</speed><vrf></vrf><mtu>1500</mtu><ether-state><negotiated-duplex-mode>unknown-duplex</negotiated-duplex-mode></ether-state></interface>
  <interface><name>Loopback0</name><interface-type>iana-iftype-sw-loopback</interface-type><admin-status>if-state-up</admin-status><oper-status>if-oper-state-ready</oper-status><phys-address>00:00:00:00:00:00</phys-address><speed>8000000000</speed><vrf></vrf><mtu>1514</mtu></interface>
  <interface><name>Tunnel0</name><interface-type>iana-iftype-tunnel</interface-type><admin-status>if-state-up</admin-status><oper-status>if-oper-state-lower-layer-down</oper-status><speed>100000</speed><vrf>LAB-UNDEFINED</vrf><ipv4>198.51.100.1</ipv4><ipv4-subnet-mask>255.255.255.252</ipv4-subnet-mask></interface>
  <interface><name>Async1</name><interface-type>iana-iftype-other</interface-type><admin-status>if-state-up</admin-status><oper-status>if-oper-state-lower-layer-down</oper-status></interface>
</interfaces>`,
	"vrf-definitions": `<native xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-native"><vrf><definition><name>LAB-A</name></definition><definition><name>Mgmt-vrf</name></definition></vrf></native>`,
}

// testInterfaceSet transforms the interfaces/config fixture, with the
// interfaces/oper fixture as operational state when withOper is set.
func testInterfaceSet(t *testing.T, deviceType string, withOper bool) interfaceSet {
	t.Helper()
	cfg := mustDecode[ietfInterfaces](t, unitXML(t, interfacesCases, "config"))
	var o interfacesOper
	if withOper {
		o = mustDecode[interfacesOper](t, unitXML(t, interfacesCases, "oper"))
	}
	return TransformInterfaces("TST0000RTR01", deviceType, cfg, o)
}

func byName(set interfaceSet) map[string]sdk.Resource {
	m := map[string]sdk.Resource{}
	for _, r := range set.Resources {
		m[r.Name] = r
	}
	return m
}

func TestTransformInterfaces_Router(t *testing.T) {
	set := testInterfaceSet(t, "network.router", true)
	got := byName(set)

	wantTypes := map[string]string{
		"GigabitEthernet1":     "network.router.port",
		"GigabitEthernet1.100": "network.router.port",
		"GigabitEthernet2":     "network.router.port",
		"Loopback0":            "network.router.port",
		"Port-channel1":        "network.interface.lag",
		"Vlan10":               "network.router.port",
		"Tunnel0":              "network.router.port", // operational state only
	}
	if len(got) != len(wantTypes) {
		t.Errorf("emitted %d interfaces, want %d (Async lines skipped)", len(got), len(wantTypes))
	}
	for name, typ := range wantTypes {
		if got[name].Type != typ {
			t.Errorf("%s: type %q, want %q", name, got[name].Type, typ)
		}
		if got[name].ID != "cisco.iosxe::TST0000RTR01/"+name || got[name].Properties["interface_name"] != name {
			t.Errorf("%s: id %q interface_name %v", name, got[name].ID, got[name].Properties["interface_name"])
		}
	}
	if _, ok := got["Async1"]; ok {
		t.Error("async terminal lines must not be emitted")
	}

	g1 := got["GigabitEthernet1"]
	want := map[string]any{
		"description":    "LAB uplink",
		"admin_status":   "up",
		"oper_status":    "up",
		"speed_mbps":     int64(1000),
		"duplex":         "full",
		"mtu":            int64(1500),
		"mac_address":    "00:00:5e:00:53:01",
		"ip_address":     "192.0.2.1",
		"subnet_mask":    "255.255.255.0",
		"ipv4_addresses": []string{"192.0.2.1/24", "192.0.2.65/26"},
		"ipv6_addresses": []string{"2001:db8::1/64"},
	}
	for k, v := range want {
		if fmt.Sprint(g1.Properties[k]) != fmt.Sprint(v) {
			t.Errorf("GigabitEthernet1.%s = %v, want %v", k, g1.Properties[k], v)
		}
	}
	if g1.Status != "active" {
		t.Errorf("GigabitEthernet1 status %q, want active", g1.Status)
	}

	g2 := got["GigabitEthernet2"]
	if g2.Status != "inactive" || g2.Properties["admin_status"] != "down" || g2.Properties["oper_status"] != "down" {
		t.Errorf("GigabitEthernet2: status %q props %v", g2.Status, g2.Properties)
	}
	if _, ok := g2.Properties["duplex"]; ok {
		t.Error("unknown-duplex must be omitted")
	}
	if _, ok := got["Loopback0"].Properties["mac_address"]; ok {
		t.Error("an all-zero MAC must be omitted")
	}
	if got["Loopback0"].Properties["speed_mbps"] != int64(8000) {
		t.Errorf("Loopback0 speed_mbps = %v", got["Loopback0"].Properties["speed_mbps"])
	}

	tun := got["Tunnel0"]
	if tun.Status != "inactive" || tun.Properties["oper_status"] != "lower-layer-down" {
		t.Errorf("Tunnel0: status %q oper %v", tun.Status, tun.Properties["oper_status"])
	}
	if _, ok := tun.Properties["speed_mbps"]; ok {
		t.Error("sub-Mbps speeds must be omitted rather than rounded to 0")
	}
	if tun.Properties["ip_address"] != "198.51.100.1" || fmt.Sprint(tun.Properties["ipv4_addresses"]) != "[198.51.100.1/30]" {
		t.Errorf("operational address must be used when none is configured: %v", tun.Properties)
	}

	if got["GigabitEthernet1.100"].Properties["parent_interface"] != "GigabitEthernet1" {
		t.Error("subinterface parent missing")
	}

	for name, physical := range map[string]bool{
		"GigabitEthernet1": true, "GigabitEthernet2": true,
		"GigabitEthernet1.100": false, "Loopback0": false, "Port-channel1": false, "Vlan10": false, "Tunnel0": false,
	} {
		if set.Physical[got[name].ID] != physical {
			t.Errorf("%s: physical = %v, want %v", name, !physical, physical)
		}
	}
}

func TestTransformInterfaces_Switch(t *testing.T) {
	got := byName(testInterfaceSet(t, "network.switch", true))
	for name, typ := range map[string]string{
		"GigabitEthernet1":     "network.switch.port",
		"GigabitEthernet2":     "network.switch.port",
		"GigabitEthernet1.100": "network.interface",
		"Loopback0":            "network.interface",
		"Vlan10":               "network.interface",
		"Port-channel1":        "network.interface.lag",
	} {
		if got[name].Type != typ {
			t.Errorf("%s: type %q, want %q", name, got[name].Type, typ)
		}
	}
}

func TestTransformInterfaces_ConfigOnly(t *testing.T) {
	got := byName(testInterfaceSet(t, "network.router", false))
	if len(got) != 6 {
		t.Errorf("emitted %d interfaces from configuration alone, want 6", len(got))
	}
	g1, g2 := got["GigabitEthernet1"], got["GigabitEthernet2"]
	if g1.Properties["admin_status"] != "up" || g1.Status != "unknown" {
		t.Errorf("without operational state: admin %v status %q", g1.Properties["admin_status"], g1.Status)
	}
	if g2.Properties["admin_status"] != "down" || g2.Status != "inactive" {
		t.Errorf("a disabled interface is inactive: admin %v status %q", g2.Properties["admin_status"], g2.Status)
	}
	if _, ok := g1.Properties["oper_status"]; ok {
		t.Error("oper_status must not be invented without operational state")
	}
}

func TestInterfaceNormalizers(t *testing.T) {
	for in, want := range map[string]string{
		"if-oper-state-ready": "up", "if-oper-state-no-pass": "down", "if-oper-state-lower-layer-down": "lower-layer-down",
		"if-oper-state-test": "testing", "if-oper-state-dormant": "dormant", "if-oper-state-not-present": "not-present",
		"if-oper-state-invalid": "", "": "",
	} {
		if got := operStatus(in); got != want {
			t.Errorf("operStatus(%q) = %q, want %q", in, got, want)
		}
	}
	for _, tc := range []struct{ in, mask, want string }{
		{"192.0.2.1", "255.255.255.0", "192.0.2.1/24"},
		{" 192.0.2.1 ", "255.255.255.255", "192.0.2.1/32"},
		{"0.0.0.0", "0.0.0.0", ""},
		{"192.0.2.1", "255.0.255.0", ""},
		{"2001:db8::1", "255.255.255.0", ""},
		{"not-an-ip", "255.255.255.0", ""},
	} {
		got, ok := ipv4CIDR(tc.in, tc.mask)
		if got != tc.want || ok != (tc.want != "") {
			t.Errorf("ipv4CIDR(%q, %q) = %q, %v", tc.in, tc.mask, got, ok)
		}
	}
	for _, tc := range []struct{ in, plen, want string }{
		{"2001:DB8::0:1", "64", "2001:db8::1/64"},
		{"::", "0", ""},
		{"2001:db8::1", "129", ""},
		{"192.0.2.1", "24", ""},
	} {
		got, ok := ipv6CIDR(tc.in, tc.plen)
		if got != tc.want || ok != (tc.want != "") {
			t.Errorf("ipv6CIDR(%q, %q) = %q, %v", tc.in, tc.plen, got, ok)
		}
	}
	for in, want := range map[string]int64{"1000000000": 1000, "10000000000": 10000, "999999": 0, "": 0, "fast": 0} {
		got, ok := speedMbps(in)
		if got != want || ok != (want != 0) {
			t.Errorf("speedMbps(%q) = %d, %v", in, got, ok)
		}
	}
}

func TestTransformDeviceContainment(t *testing.T) {
	set := testInterfaceSet(t, "network.router", true)
	const deviceID = "cisco.iosxe::TST0000RTR01"
	conns := TransformDeviceContainment(deviceID, "LAB-RTR01", set)
	if len(conns) != len(set.Resources) {
		t.Fatalf("%d containment connections for %d interfaces", len(conns), len(set.Resources))
	}
	targets := map[string]string{}
	for _, c := range conns {
		if c.Source != deviceID || c.Direction != "forward" {
			t.Errorf("connection %s: source %q direction %q", c.ID, c.Source, c.Direction)
		}
		targets[c.Target] = c.Type
	}
	for _, r := range set.Resources {
		want := "contains.logical"
		if set.Physical[r.ID] {
			want = "contains.physical"
		}
		if targets[r.ID] != want {
			t.Errorf("%s contained by %q, want %q", r.Name, targets[r.ID], want)
		}
	}
}

func TestWireInterfacesToVRFs(t *testing.T) {
	set := testInterfaceSet(t, "network.router", true)
	vrfs := mustDecode[nativeVRFList](t, unitXML(t, interfacesCases, "vrf-definitions"))
	groups, nameToID := TransformVRFs("TST0000RTR01", vrfs)
	WireInterfacesToVRFs(interfaceVRFs(mustDecode[interfacesOper](t, unitXML(t, interfacesCases, "oper")), nativeInterfaces{}), set.NameToID, groups, nameToID)

	members := map[string][]string{}
	for _, g := range groups {
		members[g.Name] = g.Members
	}
	if fmt.Sprint(members["LAB-A"]) != "[cisco.iosxe::TST0000RTR01/GigabitEthernet1]" {
		t.Errorf("LAB-A members = %v", members["LAB-A"])
	}
	if len(members["Mgmt-vrf"]) != 0 {
		t.Errorf("Mgmt-vrf has no bound interface, got %v", members["Mgmt-vrf"])
	}
}

func TestNativeBootImages(t *testing.T) {
	for _, tc := range []struct {
		fixture string
		want    string
	}{
		{"boot-ordered", "[bootflash:lab-b.bin bootflash:lab-a.bin]"},
		{"boot-list", "[bootflash:lab-a.bin]"},
		{"boot-none", "[]"},
	} {
		b := mustDecode[nativeBoot](t, unitXML(t, interfacesCases, tc.fixture))
		if got := fmt.Sprint(b.images()); got != tc.want {
			t.Errorf("images() = %s, want %s", got, tc.want)
		}
	}
}
