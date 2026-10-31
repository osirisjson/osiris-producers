// transform_segmentation_test.go - Unit tests for VLAN, encapsulation
// and VRF transforms of the Cisco IOS-XE OSIRIS JSON Producer.
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

// segmentationCases are the synthetic device replies the segmentation
// unit tests decode: one minimal NETCONF <data> payload per case,
// keyed by the behavior it exercises.
var segmentationCases = map[string]string{
	"encapsulation": `<native xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-native"><interface>
  <GigabitEthernet><name>2.100</name><encapsulation><dot1Q><vlan-id>100</vlan-id></dot1Q></encapsulation></GigabitEthernet>
  <GigabitEthernet><name>2</name><description>LAB uplink</description></GigabitEthernet>
  <Vlan><name>10</name></Vlan>
  <GigabitEthernet><name>9.200</name><encapsulation><dot1Q><vlan-id>200</vlan-id></dot1Q></encapsulation></GigabitEthernet>
</interface></native>`,
	"interface-vrfs-native": `<native xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-native"><interface>
  <GigabitEthernet><name>0/0</name><vrf><forwarding>Mgmt-vrf</forwarding></vrf></GigabitEthernet>
  <GigabitEthernet><name>1</name><vrf><forwarding>LAB-CONFIGURED</forwarding></vrf></GigabitEthernet>
  <GigabitEthernet><name>2</name></GigabitEthernet>
</interface></native>`,
	"interface-vrfs-oper": `<interfaces xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-interfaces-oper">
  <interface><name>GigabitEthernet1</name><vrf>LAB-OPERATIONAL</vrf></interface>
  <interface><name>GigabitEthernet2</name><vrf></vrf></interface>
</interfaces>`,
	"switchport-interfaces": `<native xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-native"><interface>
  <GigabitEthernet><name>1</name><switchport-config><switchport><mode><access/></mode><access><vlan><vlan>10</vlan></vlan></access></switchport></switchport-config></GigabitEthernet>
  <GigabitEthernet><name>2</name><switchport-config><switchport><mode><trunk/></mode><trunk><allowed><vlan-v2><vlan-choices><vlans>10,20,30</vlans></vlan-choices></vlan-v2></allowed><native><vlan><vlan-id>99</vlan-id></vlan></native></trunk></switchport></switchport-config></GigabitEthernet>
  <TenGigabitEthernet><name>0/1/0</name><switchport-config><switchport><mode><trunk/></mode></switchport></switchport-config></TenGigabitEthernet>
  <GigabitEthernet><name>9</name><switchport-config><switchport><mode><access/></mode><access><vlan><vlan>10</vlan></vlan></access></switchport></switchport-config></GigabitEthernet>
</interface></native>`,
	"switchport-vlans":             `<vlans xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-vlan-oper"><vlan><id>1</id></vlan><vlan><id>10</id></vlan><vlan><id>20</id></vlan></vlans>`,
	"trunk-allowed-legacy-list":    `<native xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-native"><interface><GigabitEthernet><name>1</name><switchport-config><switchport><mode><trunk/></mode><trunk><allowed><vlan><vlans>100-101</vlans></vlan></allowed></trunk></switchport></switchport-config></GigabitEthernet></interface></native>`,
	"trunk-allowed-legacy-none":    `<native xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-native"><interface><GigabitEthernet><name>1</name><switchport-config><switchport><mode><trunk/></mode><trunk><allowed><vlan><none/></vlan></allowed></trunk></switchport></switchport-config></GigabitEthernet></interface></native>`,
	"trunk-allowed-malformed-list": `<native xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-native"><interface><GigabitEthernet><name>1</name><switchport-config><switchport><mode><trunk/></mode><trunk><allowed><vlan-v2><vlan-choices><vlans>10,abc</vlans></vlan-choices></vlan-v2></allowed></trunk></switchport></switchport-config></GigabitEthernet></interface></native>`,
	"trunk-allowed-no-list":        `<native xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-native"><interface><GigabitEthernet><name>1</name><switchport-config><switchport><mode><trunk/></mode><trunk><allowed></allowed></trunk></switchport></switchport-config></GigabitEthernet></interface></native>`,
	"trunk-allowed-out-of-range":   `<native xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-native"><interface><GigabitEthernet><name>1</name><switchport-config><switchport><mode><trunk/></mode><trunk><allowed><vlan-v2><vlan-choices><vlans>4095</vlans></vlan-choices></vlan-v2></allowed></trunk></switchport></switchport-config></GigabitEthernet></interface></native>`,
	"trunk-allowed-v2-all":         `<native xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-native"><interface><GigabitEthernet><name>1</name><switchport-config><switchport><mode><trunk/></mode><trunk><allowed><vlan-v2><vlan-choices><all>true</all></vlan-choices></vlan-v2></allowed></trunk></switchport></switchport-config></GigabitEthernet></interface></native>`,
	"trunk-allowed-v2-except":      `<native xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-native"><interface><GigabitEthernet><name>1</name><switchport-config><switchport><mode><trunk/></mode><trunk><allowed><vlan-v2><except>2-4094</except></vlan-v2></allowed></trunk></switchport></switchport-config></GigabitEthernet></interface></native>`,
	"trunk-allowed-v2-list-add":    `<native xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-native"><interface><GigabitEthernet><name>1</name><switchport-config><switchport><mode><trunk/></mode><trunk><allowed><vlan-v2><vlan-choices><vlans>10</vlans></vlan-choices><add-vlans><add><vlans>30-31</vlans></add><add><vlans>40</vlans></add></add-vlans></vlan-v2></allowed></trunk></switchport></switchport-config></GigabitEthernet></interface></native>`,
	"trunk-allowed-v2-list":        `<native xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-native"><interface><GigabitEthernet><name>1</name><switchport-config><switchport><mode><trunk/></mode><trunk><allowed><vlan-v2><vlan-choices><vlans>10,20-22</vlans></vlan-choices></vlan-v2></allowed></trunk></switchport></switchport-config></GigabitEthernet></interface></native>`,
	"trunk-allowed-v2-none":        `<native xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-native"><interface><GigabitEthernet><name>1</name><switchport-config><switchport><mode><trunk/></mode><trunk><allowed><vlan-v2><vlan-choices><none/></vlan-choices></vlan-v2></allowed></trunk></switchport></switchport-config></GigabitEthernet></interface></native>`,
	"trunk-allowed-v2-remove":      `<native xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-native"><interface><GigabitEthernet><name>1</name><switchport-config><switchport><mode><trunk/></mode><trunk><allowed><vlan-v2><remove>2-4093</remove></vlan-v2></allowed></trunk></switchport></switchport-config></GigabitEthernet></interface></native>`,
	"vlans": `<vlans xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-vlan-oper">
  <vlan><id>1</id><name>default</name><status>active</status></vlan>
  <vlan><id>10</id><name>LAB-USERS</name><status>active</status>
    <ports><interface>GigabitEthernet1</interface><subinterface>0</subinterface></ports>
    <vlan-interfaces><interface>GigabitEthernet1</interface></vlan-interfaces>
    <vlan-interfaces><interface>Gi2</interface></vlan-interfaces>
    <ports><interface>GigabitEthernet9</interface></ports>
  </vlan>
  <vlan><id>20</id><status>suspend</status></vlan>
  <vlan><id>5000</id><name>LAB-INVALID</name></vlan>
  <vlan><id>10</id><name>LAB-DUPLICATE</name></vlan>
</vlans>`,
	"vrf-definitions": `<native xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-native">
  <vrf>
    <definition><name>CORP</name><rd>192.0.2.1:100</rd><description>Corporate VRF</description>
      <address-family><ipv4><route-target><export>192.0.2.1:100</export></route-target></ipv4></address-family>
    </definition>
    <definition><name>MGMT</name></definition>
  </vrf>
</native>`,
}

func TestTransformVLANs(t *testing.T) {
	x := unitXML(t, segmentationCases, "vlans")
	res, conns := TransformVLANs("TST0000XE01", mustDecode[vlansOper](t, x), nativeInterfaces{}, testIfNameToID)

	if len(res) != 3 {
		t.Fatalf("expected 3 VLANs (invalid and duplicate IDs dropped), got %d", len(res))
	}
	byID := map[string]sdk.Resource{}
	for _, r := range res {
		byID[r.ID] = r
	}
	v10, ok := byID["cisco.iosxe::TST0000XE01/vlan/10"]
	if !ok || v10.Type != "network.vlan" || v10.Name != "LAB-USERS" || v10.Status != "active" ||
		v10.Properties["vlan_id"] != 10 || v10.Properties["vlan_name"] != "LAB-USERS" {
		t.Errorf("VLAN 10 = %+v", v10)
	}
	if v20 := byID["cisco.iosxe::TST0000XE01/vlan/20"]; v20.Name != "VLAN 20" || v20.Status != "inactive" {
		t.Errorf("VLAN 20 = %+v", v20)
	}

	// GigabitEthernet1 is listed twice (deduplicated), Gi2 resolves by
	// expansion, GigabitEthernet9 is not an emitted interface.
	if len(conns) != 2 {
		t.Fatalf("expected 2 membership connections, got %d: %+v", len(conns), conns)
	}
	for _, c := range conns {
		if c.Type != "network.l2" || c.Direction != "bidirectional" || c.Target != v10.ID {
			t.Errorf("membership = %+v", c)
		}
	}
}

func TestEnrichEncapsulation(t *testing.T) {
	x := unitXML(t, segmentationCases, "encapsulation")
	resources := []sdk.Resource{
		{ID: testDeviceID + "/GigabitEthernet2.100"},
		{ID: testDeviceID + "/GigabitEthernet2", Properties: map[string]any{}},
		{ID: testDeviceID + "/Vlan10"},
	}
	ifNameToID := map[string]string{
		"GigabitEthernet2.100": resources[0].ID,
		"GigabitEthernet2":     resources[1].ID,
		"Vlan10":               resources[2].ID,
	}
	EnrichEncapsulation(mustDecode[nativeInterfaces](t, x), resources, ifNameToID)

	if p := resources[0].Properties; p["encapsulation"] != "dot1q" || p["vlan_id"] != 100 {
		t.Errorf("subinterface = %v", p)
	}
	if p := resources[1].Properties; len(p) != 0 {
		t.Errorf("untagged parent must stay untouched, got %v", p)
	}
	if p := resources[2].Properties; p["vlan_id"] != 10 || p["encapsulation"] != nil {
		t.Errorf("SVI = %v", p)
	}
}

func TestTransformVRFs(t *testing.T) {
	vrfXML := unitXML(t, segmentationCases, "vrf-definitions")

	groups, nameMap := TransformVRFs("TST0000XE01", mustDecode[nativeVRFList](t, vrfXML))
	if len(groups) != 2 || len(nameMap) != 2 {
		t.Fatalf("expected 2 VRF groups, got %d", len(groups))
	}
	if groups[0].Type != "network.vrf" || groups[0].Name != "CORP" {
		t.Errorf("group[0] = %+v", groups[0])
	}
	if groups[0].Properties["route_distinguisher"] != "192.0.2.1:100" || groups[0].Properties["route_target_export"] != "192.0.2.1:100" {
		t.Errorf("CORP properties = %v", groups[0].Properties)
	}
	if groups[1].Properties != nil {
		t.Errorf("MGMT carries no properties, got %v", groups[1].Properties)
	}
}

func TestTrunkAllowedVLANs(t *testing.T) {
	sw := func(fixture string) switchport {
		return mustDecode[nativeInterfaces](t, unitXML(t, segmentationCases, "trunk-allowed-"+fixture)).Interface.Entries[0].Switchport
	}
	keys := func(m map[int]bool) string {
		if m == nil {
			return "all"
		}
		var out []int
		for v := 1; v <= 4094; v++ {
			if m[v] {
				out = append(out, v)
			}
		}
		if len(out) > 8 {
			return fmt.Sprintf("%d VLANs, first %v, last %d", len(out), out[:3], out[len(out)-1])
		}
		return fmt.Sprint(out)
	}
	for _, tt := range []struct{ name, fixture, want string }{
		{"no list: all", "no-list", "all"},
		{"v2 all leaf", "v2-all", "all"},
		{"v2 list", "v2-list", "[10 20 21 22]"},
		{"v2 list plus add", "v2-list-add", "[10 30 31 40]"},
		{"v2 none", "v2-none", "[]"},
		{"v2 except", "v2-except", "[1]"},
		{"v2 remove from all", "v2-remove", "[1 4094]"},
		{"legacy list", "legacy-list", "[100 101]"},
		{"legacy none", "legacy-none", "[]"},
		{"malformed list carries nothing", "malformed-list", "[]"},
		{"out of range carries nothing", "out-of-range", "[]"},
	} {
		if got := keys(trunkAllowedVLANs(sw(tt.fixture))); got != tt.want {
			t.Errorf("%s: got %s, want %s", tt.name, got, tt.want)
		}
	}
}

func TestTransformVLANs_SwitchportMembership(t *testing.T) {
	vlans := mustDecode[vlansOper](t, unitXML(t, segmentationCases, "switchport-vlans"))
	native := mustDecode[nativeInterfaces](t, unitXML(t, segmentationCases, "switchport-interfaces"))
	_, conns := TransformVLANs("TST0000XE01", vlans, native, testIfNameToID)

	got := map[string]bool{}
	for _, c := range conns {
		got[c.Name] = true
	}
	want := []string{
		"GigabitEthernet1 in VLAN 10",
		"GigabitEthernet2 in VLAN 10", "GigabitEthernet2 in VLAN 20", // VLAN 30 is not in the database
		"TenGigabitEthernet0/1/0 in VLAN 1", "TenGigabitEthernet0/1/0 in VLAN 10", "TenGigabitEthernet0/1/0 in VLAN 20", // no list: all
	}
	if len(conns) != len(want) {
		t.Errorf("got %d memberships %v, want %d", len(conns), got, len(want))
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("missing membership %q", w)
		}
	}

	resources := []sdk.Resource{{ID: testIfNameToID["GigabitEthernet1"]}, {ID: testIfNameToID["GigabitEthernet2"]}}
	EnrichEncapsulation(native, resources, testIfNameToID)
	if resources[0].Properties["port_mode"] != "access" || resources[0].Properties["native_vlan"] != nil {
		t.Errorf("access port = %v", resources[0].Properties)
	}
	if resources[1].Properties["port_mode"] != "trunk" || resources[1].Properties["native_vlan"] != 99 {
		t.Errorf("trunk port = %v", resources[1].Properties)
	}
}

func TestInterfaceVRFs(t *testing.T) {
	native := mustDecode[nativeInterfaces](t, unitXML(t, segmentationCases, "interface-vrfs-native"))
	oper := mustDecode[interfacesOper](t, unitXML(t, segmentationCases, "interface-vrfs-oper"))

	got := interfaceVRFs(oper, native)
	want := map[string]string{"GigabitEthernet0/0": "Mgmt-vrf", "GigabitEthernet1": "LAB-OPERATIONAL"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("operational binding must win, config fills the rest: got %v, want %v", got, want)
	}

	// interfaces-oper failed: the configuration alone still binds.
	got = interfaceVRFs(interfacesOper{}, native)
	if got["GigabitEthernet0/0"] != "Mgmt-vrf" || got["GigabitEthernet1"] != "LAB-CONFIGURED" || len(got) != 2 {
		t.Errorf("config-only = %v", got)
	}
}
