// iosxe_test.go - Integration tests for the Cisco IOS-XE producer.
// Verifies end-to-end Collect behavior using a mock NETCONF transport,
// including detail levels, CDP connections, VRF membership and deterministic output.
// All test data is invented - no real device data.
//
// For an introduction to OSIRIS JSON Producer for Cisco see:
// "[OSIRIS-JSON-CISCO]."
//
// [OSIRIS-JSON-CISCO]: https://osirisjson.org/en/docs/producers/network/cisco

package iosxe

import (
	"fmt"
	"strings"
	"testing"

	"go.osirisjson.org/producers/osiris/network/cisco/run"
	"go.osirisjson.org/producers/pkg/sdk"
	"go.osirisjson.org/producers/pkg/testharness"
)

// fixtureTransport implements Transport by mapping NETCONF filter substrings to canned XML replies.
type fixtureTransport struct {
	fixtures map[string]string
	closed   bool
}

func (ft *fixtureTransport) Send(rpc []byte) ([]byte, error) {
	if ft.closed {
		return nil, fmt.Errorf("transport closed")
	}
	req := string(rpc)
	for key, reply := range ft.fixtures {
		if strings.Contains(req, key) {
			return []byte(reply), nil
		}
	}
	// Return empty <data/> for unmatched queries (graceful).
	return []byte(`<rpc-reply xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><data/></rpc-reply>`), nil
}

func (ft *fixtureTransport) Close() error {
	ft.closed = true
	return nil
}

func newFixtureTransport() *fixtureTransport {
	return &fixtureTransport{
		fixtures: map[string]string{
			// Native config (version, hostname).
			"<version/><hostname/>": wrapRPCReply(`
    <native xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-native">
      <version>16.9</version>
      <hostname>LAB-RTR01</hostname>
    </native>`),

			// Interfaces.
			"ietf-interfaces": wrapRPCReply(`
    <interfaces xmlns="urn:ietf:params:xml:ns:yang:ietf-interfaces">
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
        <statistics>
          <in-octets>5000000</in-octets>
          <out-octets>3000000</out-octets>
          <in-errors>0</in-errors>
          <out-errors>0</out-errors>
        </statistics>
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
        <statistics>
          <in-octets>10000000</in-octets>
          <out-octets>8000000</out-octets>
          <in-errors>5</in-errors>
          <out-errors>0</out-errors>
        </statistics>
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
        <name>Port-channel1</name>
        <type>ianaift:ieee8023adLag</type>
        <enabled>true</enabled>
        <admin-status>up</admin-status>
        <oper-status>up</oper-status>
      </interface>
      <interface>
        <name>GigabitEthernet0/0/0.100</name>
        <type>ianaift:l3ipvlan</type>
        <enabled>true</enabled>
        <admin-status>up</admin-status>
        <oper-status>up</oper-status>
      </interface>
    </interfaces>`),

			// Hardware inventory.
			"device-hardware-oper": wrapRPCReply(`
    <device-hardware-data xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-device-hardware-oper">
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
      </device-hardware>
    </device-hardware-data>`),

			// CDP neighbors.
			"cdp-oper": wrapRPCReply(`
    <cdp-neighbor-details xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-cdp-oper">
      <cdp-neighbor-detail>
        <device-name>REMOTE-SW01</device-name>
        <local-intf-name>GigabitEthernet0/0/0</local-intf-name>
        <port-id>GigabitEthernet1/0/1</port-id>
        <platform-name>cisco WS-C3850-48T</platform-name>
        <mgmt-address>10.99.0.10</mgmt-address>
      </cdp-neighbor-detail>
    </cdp-neighbor-details>`),

			// VRF definitions.
			"<vrf/>": wrapRPCReply(`
    <native xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-native">
      <vrf>
        <definition>
          <name>CORP</name>
          <rd>10.99.0.1:100</rd>
          <description>Corporate VRF</description>
          <interface>GigabitEthernet0/0/0</interface>
          <interface>Loopback0</interface>
        </definition>
        <definition>
          <name>MGMT</name>
          <rd>10.99.0.1:200</rd>
        </definition>
      </vrf>
    </native>`),

			// BGP state (detailed only).
			"bgp-oper": wrapRPCReply(`
    <bgp-state-data xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-bgp-oper">
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
      </neighbors>
    </bgp-state-data>`),

			// OSPF state (detailed only).
			"ospf-oper": wrapRPCReply(`
    <ospf-oper-data xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-ospf-oper">
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
    </ospf-oper-data>`),

			// CPU utilization (detailed only).
			"process-cpu-oper": wrapRPCReply(`
    <cpu-usage xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-process-cpu-oper">
      <cpu-utilization>
        <one-minute>12</one-minute>
        <five-minutes>8</five-minutes>
      </cpu-utilization>
    </cpu-usage>`),

			// Memory statistics (detailed only).
			"memory-oper": wrapRPCReply(`
    <memory-statistics xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-memory-oper">
      <memory-statistic>
        <name>Processor</name>
        <total-memory>4000000</total-memory>
        <used-memory>2500000</used-memory>
        <free-memory>1500000</free-memory>
      </memory-statistic>
    </memory-statistics>`),

			// Close session.
			"close-session": `<rpc-reply xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><ok/></rpc-reply>`,
		},
	}
}

func wrapRPCReply(data string) string {
	return fmt.Sprintf(`<rpc-reply xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><data>%s</data></rpc-reply>`, data)
}

func newTestProducer(t *testing.T, detailLevel string) (*Producer, *sdk.Context) {
	t.Helper()
	ctx := testharness.NewTestContext(t, testharness.WithConfig(&sdk.ProducerConfig{
		DetailLevel:     detailLevel,
		SafeFailureMode: sdk.FailClosed,
	}))
	return &Producer{
		target: run.TargetConfig{Host: "10.99.0.1", Hostname: "LAB-RTR01", Username: "admin", Password: "test"},
		cfg:    &run.RunConfig{DetailLevel: detailLevel},
		client: &Client{
			transport: newFixtureTransport(),
			logger:    ctx.Logger,
			addr:      "10.99.0.1:830",
		},
	}, ctx
}

func TestCollect_Minimal(t *testing.T) {
	producer, ctx := newTestProducer(t, "minimal")
	doc, err := producer.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	if doc.Schema != sdk.SchemaURI {
		t.Errorf("schema: %s", doc.Schema)
	}
	if doc.Metadata.Generator.Name != generatorName {
		t.Errorf("generator: %s", doc.Metadata.Generator.Name)
	}

	// Resources: 1 device + 5 interfaces + 1 CDP stub = 7.
	if len(doc.Topology.Resources) != 7 {
		t.Errorf("expected 7 resources, got %d", len(doc.Topology.Resources))
		for _, r := range doc.Topology.Resources {
			t.Logf("  resource: %s (%s) name=%s", r.ID, r.Type, r.Name)
		}
	}

	typeCounts := countTypes(doc.Topology.Resources)
	assertCount(t, typeCounts, "network.router", 1)
	assertCount(t, typeCounts, "network.interface", 5) // 4 local + 1 CDP stub
	assertCount(t, typeCounts, "osiris.cisco.interface.lag", 1)

	// Connections: 1 CDP link.
	if len(doc.Topology.Connections) != 1 {
		t.Errorf("expected 1 connection, got %d", len(doc.Topology.Connections))
	}

	// Groups: 2 VRFs.
	if len(doc.Topology.Groups) != 2 {
		t.Errorf("expected 2 groups, got %d", len(doc.Topology.Groups))
		for _, g := range doc.Topology.Groups {
			t.Logf("  group: %s (%s) name=%s members=%d", g.ID, g.Type, g.Name, len(g.Members))
		}
	}

	// Verify marshaling works.
	if _, err := sdk.MarshalDocument(doc); err != nil {
		t.Fatalf("MarshalDocument failed: %v", err)
	}
}

func TestCollect_Detailed(t *testing.T) {
	producer, ctx := newTestProducer(t, "detailed")
	doc, err := producer.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	// Same resource count as minimal (detail enriches, doesn't add).
	if len(doc.Topology.Resources) != 7 {
		t.Errorf("expected 7 resources, got %d", len(doc.Topology.Resources))
	}

	// Verify device has BGP, OSPF, CPU extensions.
	var device *sdk.Resource
	for i, r := range doc.Topology.Resources {
		if r.Type == "network.router" {
			device = &doc.Topology.Resources[i]
			break
		}
	}
	if device == nil {
		t.Fatal("missing device resource")
	}
	cisco := device.Extensions[extensionNamespace].(map[string]any)

	if cisco["cpu_utilization_1min"] != float64(12) {
		t.Errorf("cpu_utilization_1min: %v", cisco["cpu_utilization_1min"])
	}
	if cisco["memory_used"] != int64(2500000) {
		t.Errorf("memory_used: %v", cisco["memory_used"])
	}

	bgp, ok := cisco["bgp_neighbors"].([]map[string]any)
	if !ok || len(bgp) != 1 {
		t.Errorf("expected 1 BGP neighbor, got %v", cisco["bgp_neighbors"])
	}
	if bgp[0]["remote_as"] != "65001" {
		t.Errorf("bgp remote_as: %v", bgp[0]["remote_as"])
	}

	ospf, ok := cisco["ospf_processes"].([]map[string]any)
	if !ok || len(ospf) != 1 {
		t.Errorf("expected 1 OSPF process, got %v", cisco["ospf_processes"])
	}

	// Verify interface enrichment: GigabitEthernet0/0/0 should have counters.
	var gig *sdk.Resource
	for i, r := range doc.Topology.Resources {
		if r.Name == "GigabitEthernet0/0/0" {
			gig = &doc.Topology.Resources[i]
			break
		}
	}
	if gig == nil {
		t.Fatal("missing GigabitEthernet0/0/0 resource")
	}
	if gig.Properties["rx_bytes"] != int64(5000000) {
		t.Errorf("GigabitEthernet0/0/0 rx_bytes: %v", gig.Properties["rx_bytes"])
	}
}

func TestCollect_Deterministic(t *testing.T) {
	producer, ctx := newTestProducer(t, "minimal")
	testharness.AssertDeterministic(t, producer, ctx)
}

func TestCollect_DeviceExtensions(t *testing.T) {
	producer, ctx := newTestProducer(t, "minimal")
	doc, err := producer.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	var device *sdk.Resource
	for i, r := range doc.Topology.Resources {
		if r.Type == "network.router" {
			device = &doc.Topology.Resources[i]
			break
		}
	}
	if device == nil {
		t.Fatal("missing device resource")
	}

	if device.Extensions == nil {
		t.Fatal("device should have extensions")
	}
	cisco, ok := device.Extensions[extensionNamespace].(map[string]any)
	if !ok {
		t.Fatal("device should have osiris.cisco extension")
	}

	// Verify inventory.
	inv, ok := cisco["inventory"].([]map[string]any)
	if !ok || len(inv) != 2 {
		t.Fatalf("expected 2 inventory items, got %v", cisco["inventory"])
	}
	if inv[0]["name"] != "Chassis" {
		t.Errorf("inventory[0].name: %v", inv[0]["name"])
	}
	if inv[0]["serial"] != "TST0000XE01" {
		t.Errorf("inventory[0].serial: %v", inv[0]["serial"])
	}

	// Verify boot_image extension.
	if cisco["boot_image"] != "bootflash:packages.conf" {
		t.Errorf("boot_image: %v", cisco["boot_image"])
	}
}

func TestCollect_CDPConnections(t *testing.T) {
	producer, ctx := newTestProducer(t, "minimal")
	doc, err := producer.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	if len(doc.Topology.Connections) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(doc.Topology.Connections))
	}

	conn := doc.Topology.Connections[0]
	if conn.Type != "physical.ethernet" {
		t.Errorf("connection type: %s", conn.Type)
	}
	if conn.Status != "active" {
		t.Errorf("connection status: %s", conn.Status)
	}

	// Verify source and target reference existing resources.
	resourceIDs := make(map[string]bool)
	for _, r := range doc.Topology.Resources {
		resourceIDs[r.ID] = true
	}
	if !resourceIDs[conn.Source] {
		t.Errorf("connection source %q not found in resources", conn.Source)
	}
	if !resourceIDs[conn.Target] {
		t.Errorf("connection target %q not found in resources", conn.Target)
	}

	// Verify stub resource exists.
	var stub *sdk.Resource
	for i, r := range doc.Topology.Resources {
		if r.Status == "unknown" && r.Type == "network.interface" {
			stub = &doc.Topology.Resources[i]
			break
		}
	}
	if stub == nil {
		t.Fatal("missing CDP stub resource")
	}
	if stub.Properties["remote_system"] != "REMOTE-SW01" {
		t.Errorf("stub remote_system: %v", stub.Properties["remote_system"])
	}
}

func TestCollect_VRFMembership(t *testing.T) {
	producer, ctx := newTestProducer(t, "minimal")
	doc, err := producer.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	corpVRF := findGroup(doc.Topology.Groups, "CORP")
	if corpVRF == nil {
		t.Fatal("missing CORP VRF group")
	}
	// CORP VRF should have GigabitEthernet0/0/0 and Loopback0 as members.
	if len(corpVRF.Members) != 2 {
		t.Errorf("CORP VRF: expected 2 members, got %d: %v", len(corpVRF.Members), corpVRF.Members)
	}

	mgmtVRF := findGroup(doc.Topology.Groups, "MGMT")
	if mgmtVRF == nil {
		t.Fatal("missing MGMT VRF group")
	}
	// MGMT VRF has no interface references in our fixture.
	if len(mgmtVRF.Members) != 0 {
		t.Errorf("MGMT VRF: expected 0 members, got %d: %v", len(mgmtVRF.Members), mgmtVRF.Members)
	}
}

func TestCollect_Subinterfaces(t *testing.T) {
	producer, ctx := newTestProducer(t, "minimal")
	doc, err := producer.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	var subIf *sdk.Resource
	for i, r := range doc.Topology.Resources {
		if r.Name == "GigabitEthernet0/0/0.100" {
			subIf = &doc.Topology.Resources[i]
			break
		}
	}
	if subIf == nil {
		t.Fatal("missing subinterface GigabitEthernet0/0/0.100")
	}
	if subIf.Properties["parent_interface"] != "GigabitEthernet0/0/0" {
		t.Errorf("parent_interface: %v", subIf.Properties["parent_interface"])
	}
}

func TestNewFactory(t *testing.T) {
	factory := NewFactory()
	p := factory(run.TargetConfig{Host: "10.99.0.1"}, &run.RunConfig{})
	if _, ok := p.(*Producer); !ok {
		t.Error("factory should return *Producer")
	}
}

// Test helpers

func countTypes(resources []sdk.Resource) map[string]int {
	m := make(map[string]int)
	for _, r := range resources {
		m[r.Type]++
	}
	return m
}

func assertCount(t *testing.T, counts map[string]int, typ string, want int) {
	t.Helper()
	if counts[typ] != want {
		t.Errorf("expected %d %s, got %d", want, typ, counts[typ])
	}
}

func findGroup(groups []sdk.Group, name string) *sdk.Group {
	for i, g := range groups {
		if g.Name == name {
			return &groups[i]
		}
	}
	return nil
}
