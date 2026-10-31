// transform_routing_test.go - Unit tests for connected subnets,
// static routes and OSPF/BGP sessions of the
// Cisco IOS-XE OSIRIS JSON Producer.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package iosxe

import (
	"fmt"
	"strings"
	"testing"

	"go.osirisjson.org/producers/pkg/sdk"
)

// routingCases are the synthetic device replies the routing unit tests
// decode: one minimal NETCONF <data> payload per case, keyed by the
// behavior it exercises.
var routingCases = map[string]string{
	"bgp": `<bgp-state-data xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-bgp-oper"><neighbors>
  <neighbor><afi-safi>ipv6-unicast</afi-safi><vrf-name>default</vrf-name><neighbor-id>198.51.100.2</neighbor-id><session-state>fsm-established</session-state><as>64513</as></neighbor>
  <neighbor><afi-safi>ipv4-unicast</afi-safi><vrf-name>default</vrf-name><neighbor-id>198.51.100.2</neighbor-id><description>LAB core</description><session-state>fsm-established</session-state><transport><local-host>198.51.100.1</local-host></transport><prefix-activity><received><current-prefixes>10</current-prefixes></received></prefix-activity><as>64513</as></neighbor>
  <neighbor><afi-safi>ipv4-unicast</afi-safi><vrf-name>LAB-A</vrf-name><neighbor-id>198.51.100.2</neighbor-id><session-state>fsm-idle</session-state><transport><local-host>0.0.0.0</local-host></transport><as>64514</as></neighbor>
  <neighbor><afi-safi>ipv4-unicast</afi-safi><vrf-name>default</vrf-name><neighbor-id>not-an-address</neighbor-id></neighbor>
</neighbors></bgp-state-data>`,
	"ospf": `<ospf-oper-data xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-ospf-oper"><ospf-state><op-mode>ospf-ships-in-the-night</op-mode>
  <ospf-instance><af>address-family-ipv4</af><router-id>3405803777</router-id><process-id>1</process-id>
    <ospf-area><area-id>0</area-id>
      <ospf-interface><name>GigabitEthernet1</name>
        <ospf-neighbor><neighbor-id>198.51.100.2</neighbor-id><address>198.51.100.2</address><state>ospf-nbr-full</state></ospf-neighbor>
        <ospf-neighbor><neighbor-id>198.51.100.3</neighbor-id><address>198.51.100.3</address><state>ospf-nbr-init</state></ospf-neighbor>
      </ospf-interface>
    </ospf-area>
    <ospf-area><area-id>1</area-id>
      <ospf-interface><name>GigabitEthernet2</name>
        <ospf-neighbor><neighbor-id>198.51.100.2</neighbor-id><address>192.0.2.2</address><state>ospf-nbr-two-way</state></ospf-neighbor>
      </ospf-interface>
      <ospf-interface><name>GigabitEthernet9</name>
        <ospf-neighbor><neighbor-id>198.51.100.4</neighbor-id><state>ospf-nbr-full</state></ospf-neighbor>
      </ospf-interface>
    </ospf-area>
  </ospf-instance>
</ospf-state></ospf-oper-data>`,
	"static-routes": `<native xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-native">
  <ip><route>
    <ip-route-interface-forwarding-list><prefix>0.0.0.0</prefix><mask>0.0.0.0</mask><fwd-list><fwd>198.51.100.254</fwd></fwd-list><fwd-list><fwd>198.51.100.253</fwd><metric>10</metric></fwd-list></ip-route-interface-forwarding-list>
    <ip-route-interface-forwarding-list><prefix>203.0.113.130</prefix><mask>255.255.255.128</mask><fwd-list><fwd>Null0</fwd><name>LAB-DISCARD</name></fwd-list></ip-route-interface-forwarding-list>
    <ip-route-interface-forwarding-list><prefix>203.0.113.0</prefix><mask>255.0.255.0</mask><fwd-list><fwd>Null0</fwd></fwd-list></ip-route-interface-forwarding-list>
    <ip-route-interface-forwarding-list><prefix>192.0.2.0</prefix><mask>255.255.255.0</mask><fwd-list-with-track><fwd>198.51.100.252</fwd></fwd-list-with-track></ip-route-interface-forwarding-list>
    <vrf><name>LAB-A</name><ip-route-interface-forwarding-list><prefix>203.0.113.64</prefix><mask>255.255.255.192</mask><fwd-list><fwd>GigabitEthernet2.100</fwd><interface-next-hop><ip-address>198.51.100.130</ip-address><metric>5</metric></interface-next-hop></fwd-list></ip-route-interface-forwarding-list></vrf>
  </route></ip>
  <ipv6><route>
    <ipv6-route-list><prefix>::/0</prefix><ipv6-fwd-list><fwd>2001:DB8:100::FE</fwd></ipv6-fwd-list></ipv6-route-list>
    <vrf><name>LAB-A</name><ipv6-route-list><prefix>2001:db8:200::/48</prefix><ipv6-fwd-list><fwd>Null0</fwd></ipv6-fwd-list></ipv6-route-list></vrf>
  </route></ipv6>
</native>`,
	"subnets-interfaces-oper": `<interfaces xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-interfaces-oper"><interface><name>GigabitEthernet3</name><vrf>LAB-A</vrf></interface></interfaces>`,
	"subnets-vrf-definitions": `<native xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-native"><vrf><definition><name>LAB-A</name></definition></vrf></native>`,
}

func TestTransformSubnets(t *testing.T) {
	set := interfaceSet{Resources: []sdk.Resource{
		{ID: testDeviceID + "/GigabitEthernet1", Name: "GigabitEthernet1", Status: "active", Properties: map[string]any{
			"ipv4_addresses": []string{"198.51.100.1/24", "198.51.100.200/24", "192.0.2.1/25"},
			"ipv6_addresses": []string{"2001:db8:100::1/64", "fe80::1/64"},
		}},
		{ID: testDeviceID + "/GigabitEthernet2", Name: "GigabitEthernet2", Status: "inactive", Properties: map[string]any{
			"ipv4_addresses": []string{"198.51.100.2/24"},
		}},
		{ID: testDeviceID + "/Loopback0", Name: "Loopback0", Status: "active", Properties: map[string]any{
			"ipv4_addresses": []string{"203.0.113.1/32"},
			"ipv6_addresses": []string{"2001:db8::1/128"},
		}},
		{ID: testDeviceID + "/GigabitEthernet3", Name: "GigabitEthernet3", Status: "active", Properties: map[string]any{
			"ipv4_addresses": []string{"198.51.100.3/24"},
		}},
	}}
	oper := mustDecode[interfacesOper](t, unitXML(t, routingCases, "subnets-interfaces-oper"))
	groups, nameToGID := TransformVRFs("TST0000XE01", mustDecode[nativeVRFList](t, unitXML(t, routingCases, "subnets-vrf-definitions")))

	res, conns := TransformSubnets("TST0000XE01", set, interfaceVRFs(oper, nativeInterfaces{}), groups, nameToGID)

	var ids []string
	for _, r := range res {
		ids = append(ids, r.ID)
		if r.Type != "network.subnet" || r.Properties["cidr"] != r.Name {
			t.Errorf("subnet = %+v", r)
		}
	}
	want := []string{
		"cisco.iosxe::TST0000XE01/subnet/default/198.51.100.0/24",
		"cisco.iosxe::TST0000XE01/subnet/default/192.0.2.0/25",
		"cisco.iosxe::TST0000XE01/subnet/default/2001:db8:100::/64",
		"cisco.iosxe::TST0000XE01/subnet/LAB-A/198.51.100.0/24",
	}
	if fmt.Sprint(ids) != fmt.Sprint(want) {
		t.Errorf("subnets = %v\nwant      %v", ids, want)
	}
	if res[3].Properties["vrf"] != "LAB-A" || res[0].Properties["vrf"] != nil {
		t.Errorf("vrf property: %v / %v", res[3].Properties, res[0].Properties)
	}

	// Gi1: three subnets (two addresses in one prefix count once), Gi2
	// and Gi3: one each. Host prefixes and link-local are skipped.
	if len(conns) != 5 {
		t.Fatalf("expected 5 network.l3 connections, got %d", len(conns))
	}
	for _, c := range conns {
		if c.Type != "network.l3" || c.Direction != "bidirectional" {
			t.Errorf("connection = %+v", c)
		}
		if c.Source == testDeviceID+"/GigabitEthernet2" && c.Status != "inactive" {
			t.Errorf("connection status follows the interface, got %q", c.Status)
		}
	}
	if fmt.Sprint(groups[0].Members) != "[cisco.iosxe::TST0000XE01/subnet/LAB-A/198.51.100.0/24]" {
		t.Errorf("VRF members = %v", groups[0].Members)
	}
}

func TestTransformStaticRoutes(t *testing.T) {
	x := unitXML(t, routingCases, "static-routes")
	got := TransformStaticRoutes(mustDecode[nativeRoutes](t, x))
	want := []string{
		"map[next_hop:198.51.100.254 prefix:0.0.0.0/0]",
		"map[metric:10 next_hop:198.51.100.253 prefix:0.0.0.0/0]",
		"map[interface:Null0 name:LAB-DISCARD prefix:203.0.113.128/25]",
		"map[next_hop:198.51.100.252 prefix:192.0.2.0/24]",
		"map[interface:GigabitEthernet2.100 metric:5 next_hop:198.51.100.130 prefix:203.0.113.64/26 vrf:LAB-A]",
		"map[next_hop:2001:db8:100::fe prefix:::/0]",
		"map[interface:Null0 prefix:2001:db8:200::/48 vrf:LAB-A]",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d routes, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if fmt.Sprint(got[i]) != want[i] {
			t.Errorf("route %d = %v, want %s", i, got[i], want[i])
		}
	}
}

func TestTransformOSPF(t *testing.T) {
	x := unitXML(t, routingCases, "ospf")
	ospf := mustDecode[ospfOperData](t, x)

	procs := TransformOSPFProcesses(ospf)
	if len(procs) != 1 || procs[0]["router_id"] != "203.0.113.1" || procs[0]["process_id"] != 1 {
		t.Errorf("processes = %v", procs)
	}

	conns, stubs := TransformOSPFNeighbors(testDeviceID, ospf, testIfNameToID)
	// 198.51.100.2 is adjacent on two interfaces (two connections, one
	// stub); 198.51.100.4 is on an interface that is not emitted.
	if len(conns) != 3 || len(stubs) != 2 {
		t.Fatalf("got %d connections, %d stubs", len(conns), len(stubs))
	}
	if stubs[0].ID != testDeviceID+"/ospf-neighbor/198.51.100.2" || stubs[0].Properties["remote_address"] != "198.51.100.2" {
		t.Errorf("stub = %+v", stubs[0])
	}
	status := map[string]string{}
	for _, c := range conns {
		if c.Type != "network.ospf" {
			t.Errorf("type = %s", c.Type)
		}
		status[c.Source+"|"+c.Target] = c.Status + "/" + fmt.Sprint(c.Properties["state"]) + "/" + fmt.Sprint(c.Properties["area"])
	}
	for k, want := range map[string]string{
		testDeviceID + "/GigabitEthernet1|" + testDeviceID + "/ospf-neighbor/198.51.100.2": "active/full/0",
		testDeviceID + "/GigabitEthernet1|" + testDeviceID + "/ospf-neighbor/198.51.100.3": "inactive/init/0",
		testDeviceID + "/GigabitEthernet2|" + testDeviceID + "/ospf-neighbor/198.51.100.2": "active/two-way/1",
	} {
		if status[k] != want {
			t.Errorf("%s = %q, want %q", k, status[k], want)
		}
	}
}

func TestTransformBGPNeighbors(t *testing.T) {
	x := unitXML(t, routingCases, "bgp")
	conns, stubs := TransformBGPNeighbors(testDeviceID, mustDecode[bgpStateData](t, x))

	// The same address in two VRFs is two sessions; the two address
	// families of the default-VRF session merge into one.
	if len(conns) != 2 || len(stubs) != 2 {
		t.Fatalf("got %d connections, %d stubs", len(conns), len(stubs))
	}
	c := conns[0]
	if c.Type != "network.bgp" || c.Source != testDeviceID || c.Target != testDeviceID+"/bgp-neighbor/default/198.51.100.2" || c.Status != "active" {
		t.Errorf("default session = %+v", c)
	}
	if fmt.Sprint(c.Properties["address_families"]) != "[ipv4-unicast ipv6-unicast]" || c.Properties["state"] != "established" ||
		c.Properties["description"] != "LAB core" || c.Properties["local_address"] != "198.51.100.1" || c.Properties["vrf"] != "default" {
		t.Errorf("default session properties = %v", c.Properties)
	}
	for k := range c.Properties {
		if strings.Contains(k, "prefix") {
			t.Errorf("prefix counters must not be emitted: %s", k)
		}
	}
	if stubs[0].Properties["remote_asn"] != uint64(64513) {
		t.Errorf("remote_asn = %#v", stubs[0].Properties["remote_asn"])
	}
	if c2 := conns[1]; c2.Status != "inactive" || c2.Properties["state"] != "idle" || c2.Properties["local_address"] != nil {
		t.Errorf("LAB-A session = %+v", c2)
	}
}

func TestRouterIDString(t *testing.T) {
	for in, want := range map[string]string{"3405803777": "203.0.113.1", "203.0.113.1": "203.0.113.1", "": "", "x": ""} {
		if got := routerIDString(in); got != want {
			t.Errorf("routerIDString(%q) = %q, want %q", in, got, want)
		}
	}
}
