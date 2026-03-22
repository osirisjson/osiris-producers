// transform_test.go - Unit tests for IOS-XE->OSIRIS transform functions.
// All test data is invented - no real device data.
//
// For an introduction to OSIRIS JSON Producer for Cisco see:
// "[OSIRIS-JSON-CISCO]."
//
// [OSIRIS-JSON-CISCO]: https://osirisjson.org/en/docs/producers/cisco

package iosxe

import (
	"testing"

	"go.osirisjson.org/producers/pkg/sdk"
)

var testNativeXML = []byte(`<native xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-native">
  <version>16.9</version>
  <hostname>LAB-RTR01</hostname>
</native>`)

var testHardwareXML = []byte(`<device-hardware-data xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-device-hardware-oper">
  <device-hardware>
    <device-inventory>
      <hw-type>hw-type-chassis</hw-type>
      <hw-description>ISR4451-X/K9 Chassis</hw-description>
      <part-number>ISR4451-X/K9</part-number>
      <serial-number>TST0000XE01</serial-number>
      <dev-name>Chassis</dev-name>
    </device-inventory>
    <device-inventory>
      <hw-type>hw-type-module</hw-type>
      <hw-description>ISR4451-X 4-Port GE NIM</hw-description>
      <part-number>NIM-4GE</part-number>
      <serial-number>TST0000NIM1</serial-number>
      <dev-name>NIM subslot 0/0</dev-name>
    </device-inventory>
    <device-inventory>
      <hw-type>hw-type-transceiver</hw-type>
      <hw-description>SFP-10G-SR Transceiver</hw-description>
      <part-number>SFP-10G-SR</part-number>
      <serial-number>TST0000SFP1</serial-number>
      <dev-name>TenGigabitEthernet0/1/0 SFP</dev-name>
    </device-inventory>
  </device-hardware>
</device-hardware-data>`)

var testInterfacesXML = []byte(`<interfaces xmlns="urn:ietf:params:xml:ns:yang:ietf-interfaces">
  <interface>
    <name>GigabitEthernet0/0/0</name>
    <type>ianaift:ethernetCsmacd</type>
    <enabled>true</enabled>
    <description>WAN uplink</description>
    <admin-status>up</admin-status>
    <oper-status>up</oper-status>
    <speed>1000000000</speed>
    <mtu>1500</mtu>
    <phys-address>aa:bb:cc:dd:00:01</phys-address>
    <ipv4>
      <address>
        <ip>10.99.0.1</ip>
        <netmask>255.255.255.0</netmask>
      </address>
    </ipv4>
  </interface>
  <interface>
    <name>TenGigabitEthernet0/1/0</name>
    <type>ianaift:ethernetCsmacd</type>
    <enabled>true</enabled>
    <admin-status>up</admin-status>
    <oper-status>up</oper-status>
    <speed>10000000000</speed>
    <mtu>9216</mtu>
    <phys-address>aa:bb:cc:dd:00:02</phys-address>
  </interface>
  <interface>
    <name>Loopback0</name>
    <type>ianaift:softwareLoopback</type>
    <enabled>true</enabled>
    <admin-status>up</admin-status>
    <oper-status>up</oper-status>
    <ipv4>
      <address>
        <ip>10.99.255.1</ip>
        <netmask>255.255.255.255</netmask>
      </address>
    </ipv4>
  </interface>
  <interface>
    <name>Tunnel100</name>
    <type>ianaift:tunnel</type>
    <enabled>true</enabled>
    <admin-status>up</admin-status>
    <oper-status>up</oper-status>
  </interface>
  <interface>
    <name>Port-channel1</name>
    <type>ianaift:ieee8023adLag</type>
    <enabled>true</enabled>
    <admin-status>up</admin-status>
    <oper-status>up</oper-status>
  </interface>
  <interface>
    <name>GigabitEthernet0/0/1</name>
    <type>ianaift:ethernetCsmacd</type>
    <enabled>false</enabled>
    <admin-status>down</admin-status>
    <oper-status>down</oper-status>
  </interface>
</interfaces>`)

func TestTransformDevice(t *testing.T) {
	r, id := TransformDevice("LAB-RTR01", testNativeXML, testHardwareXML)
	if id == "" {
		t.Fatal("expected non-empty resource ID")
	}
	if r.Name != "LAB-RTR01" {
		t.Errorf("name: %s", r.Name)
	}
	if r.Type != "network.router" {
		t.Errorf("type: %s", r.Type)
	}
	if r.Status != "active" {
		t.Errorf("status: %s", r.Status)
	}
	if r.Provider.Name != "cisco" {
		t.Errorf("provider: %s", r.Provider.Name)
	}
	if r.Properties["serial"] != "TST0000XE01" {
		t.Errorf("serial: %v", r.Properties["serial"])
	}
	if r.Properties["model"] != "ISR4451-X/K9" {
		t.Errorf("model: %v", r.Properties["model"])
	}
	if r.Properties["version"] != "16.9" {
		t.Errorf("version: %v", r.Properties["version"])
	}
	if r.Properties["hostname"] != "LAB-RTR01" {
		t.Errorf("hostname: %v", r.Properties["hostname"])
	}
}

func TestTransformDevice_ASR(t *testing.T) {
	nativeXML := []byte(`<native xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-native">
  <version>17.3</version>
  <hostname>LAB-ASR01</hostname>
</native>`)

	hwXML := []byte(`<device-hardware-data xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-device-hardware-oper">
  <device-hardware>
    <device-inventory>
      <hw-type>hw-type-chassis</hw-type>
      <hw-description>ASR1002-HX Chassis</hw-description>
      <part-number>ASR1002-HX</part-number>
      <serial-number>TST0000ASR1</serial-number>
      <dev-name>Chassis</dev-name>
    </device-inventory>
  </device-hardware>
</device-hardware-data>`)

	r, _ := TransformDevice("LAB-ASR01", nativeXML, hwXML)
	if r.Type != "network.router" {
		t.Errorf("type: %s", r.Type)
	}
	if r.Properties["model"] != "ASR1002-HX" {
		t.Errorf("model: %v", r.Properties["model"])
	}
	if r.Properties["serial"] != "TST0000ASR1" {
		t.Errorf("serial: %v", r.Properties["serial"])
	}
}

func TestTransformInterfaces(t *testing.T) {
	resources, nameToID := TransformInterfaces("LAB-RTR01", testInterfacesXML)

	if len(resources) != 6 {
		t.Fatalf("expected 6 resources, got %d", len(resources))
	}
	if len(nameToID) != 6 {
		t.Fatalf("expected 6 name mappings, got %d", len(nameToID))
	}

	// Check GigabitEthernet type and properties.
	gig := resources[0]
	if gig.Type != "network.interface" {
		t.Errorf("GigE should be network.interface, got %s", gig.Type)
	}
	if gig.Status != "active" {
		t.Errorf("up interface should be active, got %s", gig.Status)
	}
	if gig.Properties["ip_address"] != "10.99.0.1" {
		t.Errorf("ip_address: %v", gig.Properties["ip_address"])
	}
	if gig.Properties["description"] != "WAN uplink" {
		t.Errorf("description: %v", gig.Properties["description"])
	}
	if gig.Properties["mtu"] != int64(1500) {
		t.Errorf("mtu: %v", gig.Properties["mtu"])
	}

	// Check Port-channel type.
	var pc *sdk.Resource
	for i, r := range resources {
		if r.Name == "Port-channel1" {
			pc = &resources[i]
			break
		}
	}
	if pc == nil {
		t.Fatal("missing Port-channel1")
	}
	if pc.Type != "network.interface.lag" {
		t.Errorf("Port-channel should be network.interface.lag, got %s", pc.Type)
	}

	// Check down interface.
	var downIf *sdk.Resource
	for i, r := range resources {
		if r.Name == "GigabitEthernet0/0/1" {
			downIf = &resources[i]
			break
		}
	}
	if downIf == nil {
		t.Fatal("missing GigabitEthernet0/0/1")
	}
	if downIf.Status != "inactive" {
		t.Errorf("down interface should be inactive, got %s", downIf.Status)
	}
}

func TestTransformInterfaces_Subinterface(t *testing.T) {
	subIfXML := []byte(`<interfaces xmlns="urn:ietf:params:xml:ns:yang:ietf-interfaces">
  <interface>
    <name>GigabitEthernet0/0/0.100</name>
    <type>ianaift:l3ipvlan</type>
    <enabled>true</enabled>
    <admin-status>up</admin-status>
    <oper-status>up</oper-status>
  </interface>
</interfaces>`)

	resources, _ := TransformInterfaces("LAB-RTR01", subIfXML)
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	if resources[0].Properties["parent_interface"] != "GigabitEthernet0/0/0" {
		t.Errorf("parent_interface: %v", resources[0].Properties["parent_interface"])
	}
	if resources[0].Type != "network.interface" {
		t.Errorf("subinterface type: %s", resources[0].Type)
	}
}

func TestTransformInventory(t *testing.T) {
	items := TransformInventory(testHardwareXML)
	if len(items) != 3 {
		t.Fatalf("expected 3 inventory items, got %d", len(items))
	}
	if items[0]["name"] != "Chassis" {
		t.Errorf("name: %v", items[0]["name"])
	}
	if items[0]["serial"] != "TST0000XE01" {
		t.Errorf("serial: %v", items[0]["serial"])
	}
	if items[0]["part_number"] != "ISR4451-X/K9" {
		t.Errorf("part_number: %v", items[0]["part_number"])
	}
	if items[1]["name"] != "NIM subslot 0/0" {
		t.Errorf("name[1]: %v", items[1]["name"])
	}
	if items[2]["name"] != "TenGigabitEthernet0/1/0 SFP" {
		t.Errorf("name[2]: %v", items[2]["name"])
	}
}

func TestTransformVRFs(t *testing.T) {
	vrfXML := []byte(`<native xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-native">
  <vrf>
    <definition>
      <name>CORP</name>
      <rd>10.99.0.1:100</rd>
      <description>Corporate VRF</description>
    </definition>
    <definition>
      <name>MGMT</name>
      <rd>10.99.0.1:200</rd>
    </definition>
  </vrf>
</native>`)

	groups, nameMap := TransformVRFs("LAB-RTR01", vrfXML)

	if len(groups) != 2 {
		t.Fatalf("expected 2 VRF groups, got %d", len(groups))
	}
	if len(nameMap) != 2 {
		t.Fatalf("expected 2 VRF name mappings, got %d", len(nameMap))
	}

	if groups[0].Type != "logical.vrf" {
		t.Errorf("type: %s", groups[0].Type)
	}
	if groups[0].Name != "CORP" {
		t.Errorf("name: %s", groups[0].Name)
	}
	if groups[0].Properties["route_distinguisher"] != "10.99.0.1:100" {
		t.Errorf("rd: %v", groups[0].Properties["route_distinguisher"])
	}
	if groups[0].Properties["description"] != "Corporate VRF" {
		t.Errorf("description: %v", groups[0].Properties["description"])
	}
}

func TestTransformCDPNeighbors(t *testing.T) {
	cdpXML := []byte(`<cdp-neighbor-details xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-cdp-oper">
  <cdp-neighbor-detail>
    <device-name>REMOTE-SW01</device-name>
    <local-intf-name>GigabitEthernet0/0/0</local-intf-name>
    <port-id>GigabitEthernet1/0/1</port-id>
    <platform-name>cisco WS-C3850-48T</platform-name>
    <mgmt-address>10.99.0.10</mgmt-address>
  </cdp-neighbor-detail>
  <cdp-neighbor-detail>
    <device-name>REMOTE-SW02</device-name>
    <local-intf-name>TenGigabitEthernet0/1/0</local-intf-name>
    <port-id>TenGigabitEthernet1/0/1</port-id>
    <platform-name>cisco C9300-48T</platform-name>
  </cdp-neighbor-detail>
</cdp-neighbor-details>`)

	ifNameToID := map[string]string{
		"GigabitEthernet0/0/0":    "res-network.interface-gig000-abc123",
		"TenGigabitEthernet0/1/0": "res-network.interface-te010-def456",
	}

	connections, stubs := TransformCDPNeighbors("LAB-RTR01", cdpXML, ifNameToID)

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
	if stubs[0].Properties["remote_mgmt_addr"] != "10.99.0.10" {
		t.Errorf("remote_mgmt_addr: %v", stubs[0].Properties["remote_mgmt_addr"])
	}
	if stubs[0].Properties["remote_platform"] != "cisco WS-C3850-48T" {
		t.Errorf("remote_platform: %v", stubs[0].Properties["remote_platform"])
	}

	// Verify connection.
	if connections[0].Type != "network.link" {
		t.Errorf("connection type: %s", connections[0].Type)
	}
	if connections[0].Status != "active" {
		t.Errorf("connection status: %s", connections[0].Status)
	}
}

func TestTransformCDPNeighbors_MissingLocal(t *testing.T) {
	cdpXML := []byte(`<cdp-neighbor-details xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-cdp-oper">
  <cdp-neighbor-detail>
    <device-name>REMOTE-SW01</device-name>
    <local-intf-name>GigabitEthernet0/0/99</local-intf-name>
    <port-id>GigabitEthernet1/0/1</port-id>
  </cdp-neighbor-detail>
</cdp-neighbor-details>`)

	ifNameToID := map[string]string{} // empty - no matching local interface

	connections, stubs := TransformCDPNeighbors("LAB-RTR01", cdpXML, ifNameToID)
	if len(connections) != 0 {
		t.Errorf("expected 0 connections for missing local interface, got %d", len(connections))
	}
	if len(stubs) != 0 {
		t.Errorf("expected 0 stubs for missing local interface, got %d", len(stubs))
	}
}

func TestWireInterfacesToVRFs(t *testing.T) {
	vrfXML := []byte(`<native xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-native">
  <vrf>
    <definition>
      <name>CORP</name>
      <interface>GigabitEthernet0/0/0</interface>
      <interface>Loopback0</interface>
    </definition>
  </vrf>
</native>`)

	ifNameToID := map[string]string{
		"GigabitEthernet0/0/0": "res-if-gig",
		"Loopback0":            "res-if-lo0",
	}

	groups := []sdk.Group{{ID: "grp-vrf-corp", Type: "logical.vrf"}}
	nameToGroupID := map[string]string{"CORP": "grp-vrf-corp"}

	WireInterfacesToVRFs(vrfXML, ifNameToID, groups, nameToGroupID)

	if len(groups[0].Members) != 2 {
		t.Errorf("expected 2 VRF members, got %d: %v", len(groups[0].Members), groups[0].Members)
	}
}

func TestTransformCPUMemory(t *testing.T) {
	cpuXML := []byte(`<cpu-usage xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-process-cpu-oper">
  <cpu-utilization>
    <one-minute>12</one-minute>
    <five-minutes>8</five-minutes>
  </cpu-utilization>
</cpu-usage>`)

	memXML := []byte(`<memory-statistics xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-memory-oper">
  <memory-statistic>
    <name>Processor</name>
    <total-memory>4000000</total-memory>
    <used-memory>2500000</used-memory>
    <free-memory>1500000</free-memory>
  </memory-statistic>
</memory-statistics>`)

	ext := TransformCPUMemory(cpuXML, memXML)
	if ext["cpu_utilization_1min"] != float64(12) {
		t.Errorf("cpu_utilization_1min: %v", ext["cpu_utilization_1min"])
	}
	if ext["cpu_utilization_5min"] != float64(8) {
		t.Errorf("cpu_utilization_5min: %v", ext["cpu_utilization_5min"])
	}
	if ext["memory_used"] != int64(2500000) {
		t.Errorf("memory_used: %v", ext["memory_used"])
	}
	if ext["memory_free"] != int64(1500000) {
		t.Errorf("memory_free: %v", ext["memory_free"])
	}
}

func TestTransformBGPNeighbors(t *testing.T) {
	bgpXML := []byte(`<bgp-state-data xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-bgp-oper">
  <neighbors>
    <neighbor>
      <neighbor-id>10.99.1.1</neighbor-id>
      <vrf-name>default</vrf-name>
      <as>65001</as>
      <connection>
        <state>established</state>
      </connection>
      <prefix-activity>
        <received>
          <current-prefixes>150</current-prefixes>
        </received>
      </prefix-activity>
    </neighbor>
    <neighbor>
      <neighbor-id>10.99.2.1</neighbor-id>
      <vrf-name>CORP</vrf-name>
      <as>65002</as>
      <connection>
        <state>idle</state>
      </connection>
    </neighbor>
  </neighbors>
</bgp-state-data>`)

	neighbors := TransformBGPNeighbors(bgpXML)
	if len(neighbors) != 2 {
		t.Fatalf("expected 2 BGP neighbors, got %d", len(neighbors))
	}
	if neighbors[0]["neighbor_id"] != "10.99.1.1" {
		t.Errorf("neighbor_id: %v", neighbors[0]["neighbor_id"])
	}
	if neighbors[0]["remote_as"] != "65001" {
		t.Errorf("remote_as: %v", neighbors[0]["remote_as"])
	}
	if neighbors[0]["state"] != "established" {
		t.Errorf("state: %v", neighbors[0]["state"])
	}
	if neighbors[0]["prefixes_received"] != "150" {
		t.Errorf("prefixes_received: %v", neighbors[0]["prefixes_received"])
	}
	if neighbors[1]["vrf"] != "CORP" {
		t.Errorf("vrf: %v", neighbors[1]["vrf"])
	}
}

func TestTransformOSPF(t *testing.T) {
	ospfXML := []byte(`<ospf-oper-data xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-ospf-oper">
  <ospf-state>
    <ospf-instance>
      <process-id>100</process-id>
      <router-id>10.99.255.1</router-id>
      <ospf-neighbor>
        <neighbor-id>10.99.255.2</neighbor-id>
        <address>10.99.0.2</address>
        <state>full</state>
      </ospf-neighbor>
    </ospf-instance>
  </ospf-state>
</ospf-oper-data>`)

	processes := TransformOSPF(ospfXML)
	if len(processes) != 1 {
		t.Fatalf("expected 1 OSPF process, got %d", len(processes))
	}
	if processes[0]["process_id"] != "100" {
		t.Errorf("process_id: %v", processes[0]["process_id"])
	}
	if processes[0]["router_id"] != "10.99.255.1" {
		t.Errorf("router_id: %v", processes[0]["router_id"])
	}

	nbrs, ok := processes[0]["neighbors"].([]map[string]any)
	if !ok || len(nbrs) != 1 {
		t.Fatalf("expected 1 OSPF neighbor, got %v", processes[0]["neighbors"])
	}
	if nbrs[0]["neighbor_id"] != "10.99.255.2" {
		t.Errorf("neighbor_id: %v", nbrs[0]["neighbor_id"])
	}
	if nbrs[0]["state"] != "full" {
		t.Errorf("state: %v", nbrs[0]["state"])
	}
}

func TestResourceID_Deterministic(t *testing.T) {
	id1 := resourceID("network.router", "LAB-RTR01")
	id2 := resourceID("network.router", "LAB-RTR01")
	if id1 != id2 {
		t.Errorf("resourceID not deterministic: %s != %s", id1, id2)
	}

	id3 := resourceID("network.router", "LAB-RTR02")
	if id1 == id3 {
		t.Error("different inputs should produce different IDs")
	}
}

func TestClassifyInterfaceType(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"GigabitEthernet0/0/0", "network.interface"},
		{"TenGigabitEthernet0/1/0", "network.interface"},
		{"Loopback0", "network.interface"},
		{"Tunnel100", "network.interface"},
		{"Port-channel1", "network.interface.lag"},
		{"GigabitEthernet0/0/0.100", "network.interface"},
	}
	for _, tc := range cases {
		got := classifyInterfaceType(tc.in)
		if got != tc.want {
			t.Errorf("classifyInterfaceType(%q) = %q, want %q", tc.in, got, tc.want)
		}
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

func TestParentInterface(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"GigabitEthernet0/0/0.100", "GigabitEthernet0/0/0"},
		{"TenGigabitEthernet0/1/0.101", "TenGigabitEthernet0/1/0"},
		{"GigabitEthernet0/0/0", ""},
		{"Loopback0", ""},
		{"Tunnel100", ""},
		{"Port-channel1", ""},
	}
	for _, tc := range cases {
		got := parentInterface(tc.in)
		if got != tc.want {
			t.Errorf("parentInterface(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestEnrichInterfaceCounters(t *testing.T) {
	ifNameToID := map[string]string{
		"GigabitEthernet0/0/0": "res-if-gig",
	}

	resources := []sdk.Resource{
		{ID: "res-if-gig", Type: "network.interface", Properties: map[string]any{"speed": "1000000000"}},
	}

	ifXML := []byte(`<interfaces xmlns="urn:ietf:params:xml:ns:yang:ietf-interfaces">
  <interface>
    <name>GigabitEthernet0/0/0</name>
    <statistics>
      <in-octets>5000000</in-octets>
      <out-octets>3000000</out-octets>
      <in-errors>10</in-errors>
      <out-errors>0</out-errors>
    </statistics>
  </interface>
</interfaces>`)

	EnrichInterfaceCounters(ifXML, resources, ifNameToID)

	props := resources[0].Properties
	if props["rx_bytes"] != int64(5000000) {
		t.Errorf("rx_bytes: %v", props["rx_bytes"])
	}
	if props["tx_bytes"] != int64(3000000) {
		t.Errorf("tx_bytes: %v", props["tx_bytes"])
	}
	if props["rx_errors"] != int64(10) {
		t.Errorf("rx_errors: %v", props["rx_errors"])
	}
	// out-errors is 0, should not be set.
	if _, ok := props["tx_errors"]; ok {
		t.Errorf("tx_errors should not be set for 0 value, got %v", props["tx_errors"])
	}
}
