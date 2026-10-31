// iosxe_test.go - Integration tests for the Cisco IOS-XE producer.
// Verifies end-to-end Collect behavior using a mock NETCONF transport,
// including detail levels, CDP connections, VRF membership and
// deterministic output.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package iosxe

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.osirisjson.org/producers/osiris/network/cisco/run"
	"go.osirisjson.org/producers/pkg/sdk"
	"go.osirisjson.org/producers/pkg/testharness"
)

// fixtureTransport implements Transport by answering each request with
// the canned rpc-reply of the discovery operation whose filter it
// carries.
type fixtureTransport struct {
	fixtures map[string]string // operation -> rpc-reply.
	closed   bool
	// breakIDsFor, when set to an operation, makes its reply carry a
	// wrong message-id.
	breakIDsFor string
}

func (ft *fixtureTransport) Send(rpc []byte) ([]byte, error) {
	if ft.closed {
		return nil, fmt.Errorf("transport closed")
	}
	req := string(rpc)
	if strings.Contains(req, "<close-session/>") {
		return echoMessageID(rpc, []byte(`<rpc-reply xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><ok/></rpc-reply>`)), nil
	}
	for _, q := range discoveryQueries {
		if !strings.Contains(req, q.filter) {
			continue
		}
		reply, ok := ft.fixtures[q.op]
		if !ok {
			break
		}
		if ft.breakIDsFor == q.op {
			return []byte(strings.Replace(reply, "<rpc-reply ", `<rpc-reply message-id="0" `, 1)), nil
		}
		return echoMessageID(rpc, []byte(reply)), nil
	}
	// Return empty <data/> for unmatched queries (graceful).
	return echoMessageID(rpc, []byte(`<rpc-reply xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><data/></rpc-reply>`)), nil
}

func (ft *fixtureTransport) Close() error {
	ft.closed = true
	return nil
}

// fixtureDir is the scenario the sandbox device of the Collect tests
// serves: one synthetic <data> payload per discovery operation.
const fixtureDir = goldenRoot + "/isr4451"

// newFixtureTransport returns a sandbox device serving the fixtureDir
// payloads. An operation without a fixture file answers with an empty
// <data/>.
func newFixtureTransport(t *testing.T) *fixtureTransport {
	t.Helper()
	ft := &fixtureTransport{fixtures: map[string]string{}}
	for _, q := range discoveryQueries {
		path := filepath.Join(fixtureDir, "netconf", fixtureFile(q.op))
		if _, err := os.Stat(path); err == nil {
			ft.fixtures[q.op] = wrapRPCReply(string(testharness.LoadFixture(t, path)))
		}
	}
	return ft
}

func wrapRPCReply(data string) string {
	return fmt.Sprintf(`<rpc-reply xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><data>%s</data></rpc-reply>`, data)
}

func newTestProducer(t *testing.T, detailLevel string) (*Producer, *sdk.Context) {
	t.Helper()
	purpose := "documentation"
	if detailLevel == "detailed" {
		purpose = "audit"
	}
	ctx := testharness.NewTestContext(t, testharness.WithConfig(&sdk.ProducerConfig{
		Purpose:         purpose,
		SafeFailureMode: sdk.FailClosed,
	}))
	return &Producer{
		target: run.TargetConfig{Host: "192.0.2.1", Hostname: "LAB-RTR01", Username: "admin", Password: "test"},
		cfg:    &Config{Purpose: purpose},
		client: &Client{
			transport: newFixtureTransport(t),
			logger:    ctx.Logger,
			addr:      "192.0.2.1:830",
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

	// Resources: 1 device + 5 interfaces + 1 CDP stub + 1 connected
	// subnet (the Loopback0 /32 is a host prefix, not a subnet) = 8.
	if len(doc.Topology.Resources) != 8 {
		t.Errorf("expected 8 resources, got %d", len(doc.Topology.Resources))
		for _, r := range doc.Topology.Resources {
			t.Logf("  resource: %s (%s) name=%s", r.ID, r.Type, r.Name)
		}
	}

	typeCounts := countTypes(doc.Topology.Resources)
	assertCount(t, typeCounts, "network.router", 1)
	assertCount(t, typeCounts, "network.router.port", 4) // Gi0/0/0, Te0/1/0, Lo0, Gi0/0/0.100
	assertCount(t, typeCounts, "network.interface.lag", 1)
	assertCount(t, typeCounts, "network.interface", 1) // CDP stub
	assertCount(t, typeCounts, "network.subnet", 1)

	// Connections: 5 device containment links + 1 CDP link + 1 subnet
	// attachment.
	connTypes := map[string]int{}
	for _, c := range doc.Topology.Connections {
		connTypes[c.Type]++
	}
	if connTypes["contains.physical"] != 2 || connTypes["contains.logical"] != 3 || connTypes["physical.ethernet"] != 1 || connTypes["network.l3"] != 1 {
		t.Errorf("connection types = %v, want 2 contains.physical, 3 contains.logical, 1 physical.ethernet, 1 network.l3", connTypes)
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

	// Minimal's 8 resources plus one BGP and one OSPF neighbor stub.
	if len(doc.Topology.Resources) != 10 {
		t.Errorf("expected 10 resources, got %d", len(doc.Topology.Resources))
	}

	// Verify the device has its OSPF processes and no volatile telemetry.
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

	for _, k := range []string{"cpu_utilization_1min", "cpu_utilization_5min", "memory_used", "memory_free"} {
		if _, ok := cisco[k]; ok {
			t.Errorf("volatile %s must not be emitted", k)
		}
	}
	if _, ok := cisco["bgp_neighbors"]; ok {
		t.Error("BGP neighbors are connections, not a device extension")
	}
	ospf, ok := cisco["ospf_processes"].([]map[string]any)
	if !ok || len(ospf) != 1 || ospf[0]["router_id"] != "198.51.100.1" || ospf[0]["process_id"] != 100 {
		t.Errorf("ospf_processes = %v", cisco["ospf_processes"])
	}

	var bgp, ospfConn *sdk.Connection
	for i, c := range doc.Topology.Connections {
		switch c.Type {
		case "network.bgp":
			bgp = &doc.Topology.Connections[i]
		case "network.ospf":
			ospfConn = &doc.Topology.Connections[i]
		}
	}
	if bgp == nil || bgp.Source != device.ID || bgp.Status != "active" ||
		fmt.Sprint(bgp.Properties["address_families"]) != "[ipv4-unicast ipv6-unicast]" || bgp.Properties["state"] != "established" {
		t.Errorf("network.bgp connection = %+v", bgp)
	}
	if _, ok := bgp.Properties["prefixes_received"]; ok {
		t.Error("prefix counters must not be emitted")
	}
	if ospfConn == nil || ospfConn.Source != "cisco.iosxe::TST0000XE01/GigabitEthernet0/0/0" ||
		ospfConn.Properties["state"] != "full" || ospfConn.Properties["area"] != uint64(0) {
		t.Errorf("network.ospf connection = %+v", ospfConn)
	}

	// Interface counters are high-churn telemetry and never emitted.
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
	for _, k := range []string{"rx_bytes", "tx_bytes", "rx_errors", "tx_errors"} {
		if _, ok := gig.Properties[k]; ok {
			t.Errorf("GigabitEthernet0/0/0 %s must not be emitted", k)
		}
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
	if !ok || len(inv) != 3 {
		t.Fatalf("expected 3 inventory items, got %v", cisco["inventory"])
	}
	if inv[0]["name"] != "Chassis" {
		t.Errorf("inventory[0].name: %v", inv[0]["name"])
	}
	if _, ok := inv[0]["serial"]; ok {
		t.Errorf("inventory serials are audit-only: %v", inv[0])
	}

	// Boot images come from the configured boot system list only.
	if fmt.Sprint(cisco["boot_images"]) != "[bootflash:lab-isr4400-universalk9.bin]" {
		t.Errorf("boot_images: %v", cisco["boot_images"])
	}
	if _, ok := cisco["boot_image"]; ok {
		t.Error("the former hard-coded boot_image must not be emitted")
	}
}

func TestCollect_CDPConnections(t *testing.T) {
	producer, ctx := newTestProducer(t, "minimal")
	doc, err := producer.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	var cdpConns []sdk.Connection
	for _, c := range doc.Topology.Connections {
		if c.Type == "physical.ethernet" {
			cdpConns = append(cdpConns, c)
		}
	}
	if len(cdpConns) != 1 {
		t.Fatalf("expected 1 CDP connection, got %d", len(cdpConns))
	}

	conn := cdpConns[0]
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
	if stub.Properties["remote_system"] != "LAB-SW01" {
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
	// CORP test VRF should have GigabitEthernet0/0/0, Loopback0 and the
	// connected subnet of GigabitEthernet0/0/0 as members.
	if len(corpVRF.Members) != 3 {
		t.Errorf("CORP VRF: expected 3 members, got %d: %v", len(corpVRF.Members), corpVRF.Members)
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

// deviceCoverage returns the device resource's coverage extension keyed
// by operation and the document's scope description.
func deviceCoverage(t *testing.T, doc *sdk.Document) (map[string]map[string]any, string) {
	t.Helper()
	for _, r := range doc.Topology.Resources {
		if r.Type != "network.router" {
			continue
		}
		cisco, _ := r.Extensions[extensionNamespace].(map[string]any)
		entries, ok := cisco["coverage"].([]map[string]any)
		if !ok {
			t.Fatal("device resource has no coverage extension")
		}
		byOp := make(map[string]map[string]any, len(entries))
		for _, e := range entries {
			byOp[e["operation"].(string)] = e
		}
		if doc.Metadata.Scope == nil {
			t.Fatal("document has no scope")
		}
		return byOp, doc.Metadata.Scope.Description
	}
	t.Fatal("missing device resource")
	return nil, ""
}

func setFixture(p *Producer, key, reply string) {
	p.client.transport.(*fixtureTransport).fixtures[key] = reply
}

const unsupportedReply = `<rpc-reply xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><rpc-error><error-type>application</error-type><error-tag>unknown-namespace</error-tag><error-severity>error</error-severity><error-message>unknown namespace</error-message></rpc-error></rpc-reply>`

func TestCollect_CoverageDocumentation(t *testing.T) {
	producer, ctx := newTestProducer(t, "minimal")
	doc, err := producer.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}
	cov, desc := deviceCoverage(t, doc)

	for op, want := range map[string]string{
		"native-system":      statusSucceeded,
		"interfaces":         statusSucceeded,
		"interfaces-oper":    statusSucceeded,
		"boot-config":        statusSucceeded,
		"hardware-inventory": statusSucceeded,
		"cdp-neighbors":      statusSucceeded,
		"vrf-definitions":    statusSucceeded,
		"lldp-neighbors":     statusEmpty,
		"vlans":              statusEmpty,
		"native-interfaces":  statusEmpty,
		"static-routes":      statusEmpty,
		"transceivers":       statusEmpty,
		"bgp":                statusSkipped,
		"ospf":               statusSkipped,
		"aaa":                statusSkipped,
		"acls":               statusSkipped,
		"certificates":       statusSkipped,
	} {
		if got := cov[op]["status"]; got != want {
			t.Errorf("%s: status %v, want %s", op, got, want)
		}
	}
	if cov["interfaces"]["count"] != 5 {
		t.Errorf("interfaces count = %v, want 5", cov["interfaces"]["count"])
	}
	if !strings.Contains(desc, "7/25 discovery operations returned data") || !strings.Contains(desc, "bgp (skipped: audit purpose only)") {
		t.Errorf("scope description = %q", desc)
	}
	if doc.Metadata.Scope.Purpose != "documentation" {
		t.Errorf("scope purpose = %q", doc.Metadata.Scope.Purpose)
	}
}

func TestCollect_CoverageAudit(t *testing.T) {
	producer, ctx := newTestProducer(t, "detailed")
	doc, err := producer.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}
	cov, desc := deviceCoverage(t, doc)
	for _, op := range []string{"bgp", "ospf"} {
		if cov[op]["status"] != statusSucceeded {
			t.Errorf("%s: status %v, want succeeded", op, cov[op]["status"])
		}
	}
	for _, op := range []string{"cpu", "memory"} {
		if _, ok := cov[op]; ok {
			t.Errorf("volatile operation %s must not be attempted", op)
		}
	}
	if strings.Contains(desc, "Not represented") {
		t.Errorf("a complete audit run should report no gaps: %q", desc)
	}
}

func TestCollect_OptionalFailuresYieldPartialDocument(t *testing.T) {
	producer, ctx := newTestProducer(t, "minimal")
	setFixture(producer, "hardware-inventory", unsupportedReply)
	// A CDP reply whose payload is not the CDP model.
	setFixture(producer, "cdp-neighbors", wrapRPCReply(`<interfaces xmlns="urn:ietf:params:xml:ns:yang:ietf-interfaces"/>`))
	setFixture(producer, "vrf-definitions", wrapRPCReply(``))

	doc, err := producer.Collect(ctx)
	if err != nil {
		t.Fatalf("optional failures must not abort Collect: %v", err)
	}
	cov, desc := deviceCoverage(t, doc)

	if cov["hardware-inventory"]["status"] != statusUnsupported {
		t.Errorf("hardware-inventory = %v, want unsupported", cov["hardware-inventory"])
	}
	if cov["cdp-neighbors"]["status"] != statusFailed || cov["cdp-neighbors"]["category"] != "decode-error" {
		t.Errorf("cdp-neighbors = %v, want failed/decode-error", cov["cdp-neighbors"])
	}
	if cov["vrf-definitions"]["status"] != statusEmpty {
		t.Errorf("vrf-definitions = %v, want empty", cov["vrf-definitions"])
	}
	for _, want := range []string{"hardware-inventory (unsupported)", "cdp-neighbors (failed: decode-error)", "No instances:", "vrf-definitions"} {
		if !strings.Contains(desc, want) {
			t.Errorf("scope description %q missing %q", desc, want)
		}
	}

	// The data those operations would have supplied is absent, not
	// synthetic: no CDP link, no VRF groups. Containment is unaffected.
	for _, c := range doc.Topology.Connections {
		if c.Type == "physical.ethernet" {
			t.Errorf("unexpected CDP connection %s", c.ID)
		}
	}
	if len(doc.Topology.Groups) != 0 {
		t.Errorf("groups=%d, want 0", len(doc.Topology.Groups))
	}
}

func TestCollect_EssentialFailureAborts(t *testing.T) {
	cases := map[string]string{
		"interfaces unsupported":   unsupportedReply,
		"interfaces wrong model":   wrapRPCReply(`<native xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-native"/>`),
		"interfaces malformed xml": wrapRPCReply(`<interfaces xmlns="urn:ietf:params:xml:ns:yang:ietf-interfaces"><interface>`),
	}
	for name, reply := range cases {
		t.Run(name, func(t *testing.T) {
			producer, ctx := newTestProducer(t, "minimal")
			setFixture(producer, "interfaces", reply)
			doc, err := producer.Collect(ctx)
			if err == nil {
				t.Fatalf("expected Collect to abort, got a document with %d resources", len(doc.Topology.Resources))
			}
			if !strings.Contains(err.Error(), "interfaces query failed") {
				t.Errorf("error should name the failed operation: %v", err)
			}
		})
	}
}

func TestCollect_SessionFailureRecordedForLaterOperations(t *testing.T) {
	producer, ctx := newTestProducer(t, "minimal")
	// A stale message-id on the CDP reply makes the session unusable;
	// the later optional operation must record that.
	tr := producer.client.transport.(*fixtureTransport)
	tr.breakIDsFor = "cdp-neighbors"

	doc, err := producer.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}
	cov, _ := deviceCoverage(t, doc)
	if cov["cdp-neighbors"]["category"] != "message-id-mismatch" {
		t.Errorf("cdp-neighbors = %v, want failed/message-id-mismatch", cov["cdp-neighbors"])
	}
	if cov["vrf-definitions"]["category"] != "session-unusable" {
		t.Errorf("vrf-definitions = %v, want failed/session-unusable", cov["vrf-definitions"])
	}
}

func TestCollect_HardwareIdentity(t *testing.T) {
	// A failed hardware query aborts: falling back would silently
	// change every emitted ID.
	producer, ctx := newTestProducer(t, "minimal")
	producer.client.transport.(*fixtureTransport).breakIDsFor = "hardware-inventory"
	if _, err := producer.Collect(ctx); err == nil || !strings.Contains(err.Error(), "hardware-inventory query failed") {
		t.Fatalf("failed hardware query: err = %v, want abort", err)
	}

	// An unsupported hardware model falls back to the target address.
	producer, ctx = newTestProducer(t, "minimal")
	setFixture(producer, "hardware-inventory", unsupportedReply)
	doc, err := producer.Collect(ctx)
	if err != nil {
		t.Fatalf("unsupported hardware model must not abort: %v", err)
	}
	for _, r := range doc.Topology.Resources {
		if r.Type == "network.router" && r.ID != "cisco.iosxe::192.0.2.1" {
			t.Errorf("fallback device id = %q", r.ID)
		}
	}
}

func TestCollect_Identity(t *testing.T) {
	producer, ctx := newTestProducer(t, "minimal")
	doc, err := producer.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}
	var device sdk.Resource
	for _, r := range doc.Topology.Resources {
		if r.Type == "network.router" {
			device = r
		}
	}
	if device.ID != "cisco.iosxe::TST0000XE01" || device.Name != "LAB-RTR01" {
		t.Fatalf("device id/name = %q/%q", device.ID, device.Name)
	}
	if doc.Metadata.Scope.Name != "LAB-RTR01" || doc.Metadata.Scope.Providers[0] != "cisco.iosxe" {
		t.Errorf("scope = %+v", doc.Metadata.Scope)
	}
	for _, r := range doc.Topology.Resources {
		switch {
		case r.Type == "network.router":
		case r.Provider.Name == unknownProviderName:
			if !strings.HasPrefix(r.ID, device.ID+"/neighbor/") {
				t.Errorf("stub %q is not anchored under the device", r.ID)
			}
		case r.Type == "network.subnet":
			if r.ID != "cisco.iosxe::"+r.Provider.NativeID || !strings.HasPrefix(r.Provider.NativeID, "TST0000XE01/subnet/") {
				t.Errorf("subnet %q: id %q native_id %q", r.Name, r.ID, r.Provider.NativeID)
			}
		default:
			if r.ID != "cisco.iosxe::TST0000XE01/"+r.Name || r.Provider.NativeID != "TST0000XE01/"+r.Name {
				t.Errorf("interface %q: id %q native_id %q", r.Name, r.ID, r.Provider.NativeID)
			}
		}
	}

	// Collecting the same device under a different target alias and
	// address yields the same IDs.
	other, ctx2 := newTestProducer(t, "minimal")
	other.target.Host, other.target.Hostname = "lab-rtr01.example.com", "LAB-ALIAS"
	doc2, err := other.Collect(ctx2)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}
	ids := func(d *sdk.Document) []string {
		var out []string
		for _, r := range d.Topology.Resources {
			out = append(out, r.ID)
		}
		for _, g := range d.Topology.Groups {
			out = append(out, g.ID)
		}
		for _, c := range d.Topology.Connections {
			out = append(out, c.ID)
		}
		return out
	}
	if a, b := strings.Join(ids(doc), ","), strings.Join(ids(doc2), ","); a != b {
		t.Errorf("IDs changed with the target alias:\n%s\n%s", a, b)
	}
}
