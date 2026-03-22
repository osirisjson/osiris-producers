// apic_test.go - Integration tests for the Cisco ACI/APIC producer.
// Verifies end-to-end Collect behavior using a canned fixture server,
// including detail levels, fault wiring, ACI extensions and deterministic output.
//
// For an introduction to OSIRIS JSON Producer for Cisco see:
// "[OSIRIS-JSON-CISCO]."
//
// [OSIRIS-JSON-CISCO]: https://osirisjson.org/en/docs/producers/cisco

package apic

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.osirisjson.org/producers/osiris/network/cisco/run"
	"go.osirisjson.org/producers/pkg/sdk"
	"go.osirisjson.org/producers/pkg/testharness"
)

// fixtureServer creates an httptest.Server that serves canned APIC responses.
func fixtureServer(t *testing.T) *httptest.Server {
	t.Helper()

	fixtures := map[string]any{
		"fabricNode": []any{
			apicObj("fabricNode", map[string]any{"dn": "topology/pod-1/node-1", "name": "APIC1", "role": "controller", "serial": "TST00001", "model": "APIC-SERVER-L3", "version": "5.2(8h)", "address": "10.1.0.1", "id": "1", "fabricSt": "unknown"}),
			apicObj("fabricNode", map[string]any{"dn": "topology/pod-1/node-101", "name": "SPINE1", "role": "spine", "serial": "TST00101", "model": "N9K-C9508", "version": "n9000-15.2(8h)", "address": "10.1.1.101", "id": "101", "fabricSt": "active"}),
			apicObj("fabricNode", map[string]any{"dn": "topology/pod-1/node-111", "name": "LEAF1", "role": "leaf", "serial": "TST00111", "model": "N9K-C93180YC-FX", "version": "n9000-15.2(8h)", "address": "10.1.1.111", "id": "111", "fabricSt": "active"}),
		},
		"topSystem": []any{
			apicObj("topSystem", map[string]any{"dn": "topology/pod-1/node-1/sys", "name": "APIC1", "oobMgmtAddr": "10.0.0.1", "inbMgmtAddr": "10.2.0.1", "systemUpTime": "100:00:00:00.000", "state": "in-service", "fabricDomain": "TEST_DC", "role": "controller", "serial": "TST00001", "fabricMAC": "AA:BB:CC:00:00:01", "controlPlaneMTU": "9000", "lastRebootTime": "2024-04-13T16:47:52.025+00:00", "fabricId": "1"}),
			apicObj("topSystem", map[string]any{"dn": "topology/pod-1/node-101/sys", "name": "SPINE1", "oobMgmtAddr": "10.0.0.101", "state": "in-service", "role": "spine", "fabricMAC": "AA:BB:CC:00:01:01", "controlPlaneMTU": "9000", "fabricId": "1"}),
		},
		"firmwareRunning": []any{
			apicObj("firmwareRunning", map[string]any{"dn": "topology/pod-1/node-101/sys/fwstatuscont/running", "version": "n9000-15.2(8h)", "peVer": "5.2(8h)"}),
			apicObj("firmwareRunning", map[string]any{"dn": "topology/pod-1/node-111/sys/fwstatuscont/running", "version": "n9000-15.2(8h)", "peVer": "5.2(8h)"}),
		},
		"fvTenant": []any{
			apicObj("fvTenant", map[string]any{"dn": "uni/tn-common", "name": "common", "descr": ""}),
			apicObj("fvTenant", map[string]any{"dn": "uni/tn-tn_Example", "name": "tn_Example", "descr": "Example tenant"}),
		},
		"fvCtx": []any{
			apicObj("fvCtx", map[string]any{"dn": "uni/tn-tn_Example/ctx-vrf_Prod_1", "name": "vrf_Prod_1", "descr": "Production VRF", "pcEnfPref": "enforced"}),
		},
		"fvBD": []any{
			apicObj("fvBD", map[string]any{"dn": "uni/tn-tn_Example/BD-bd_App", "name": "bd_App", "descr": "Application bridge domain", "unicastRoute": "yes", "unkMacUcastAct": "proxy", "arpFlood": "yes", "mac": "00:AA:BB:CC:DD:EE"}),
		},
		"fvSubnet": []any{
			apicObj("fvSubnet", map[string]any{"dn": "uni/tn-tn_Example/BD-bd_App/subnet-[10.0.0.1/24]", "ip": "10.0.0.1/24", "scope": "public", "preferred": "no"}),
		},
		"fvAEPg": []any{
			apicObj("fvAEPg", map[string]any{"dn": "uni/tn-tn_Example/ap-app1/epg-epg_WEB", "name": "epg_WEB", "descr": "Web EPG"}),
		},
		"l3extOut": []any{
			apicObj("l3extOut", map[string]any{"dn": "uni/tn-tn_Example/out-__ui_svi_dummy_id_0", "name": "__ui_svi_dummy_id_0", "descr": "dummy"}),
			apicObj("l3extOut", map[string]any{"dn": "uni/tn-tn_Example/out-l3out_Prod_1", "name": "l3out_Prod_1", "descr": "Production L3Out"}),
		},
		"fvCEp": []any{
			apicObj("fvCEp", map[string]any{"dn": "uni/tn-tn_Example/ap-app1/epg-epg_WEB/cep-AA:BB:CC:DD:EE:FF", "mac": "AA:BB:CC:DD:EE:FF", "encap": "vlan-100", "fabricPathDn": "topology/pod-1/paths-111/pathep-[eth1/1]"}),
		},
		"faultInst": []any{
			apicObj("faultInst", map[string]any{"dn": "topology/pod-1/node-101/sys/ch/supslot-1/sup/sensor-1/fault-F1527", "code": "F1527", "severity": "warning", "cause": "equipment-full", "descr": "Storage unit full", "created": "2026-02-25T07:32:52.237+00:00", "lastTransition": "2026-02-25T07:49:53.368+00:00", "lc": "raised", "domain": "infra", "subject": "equipment-full"}),
			apicObj("faultInst", map[string]any{"dn": "topology/pod-1/node-111/sys/phys/fault-F0532", "code": "F0532", "severity": "minor", "cause": "interface-physical-down", "descr": "Physical interface down", "created": "2026-02-25T08:00:00.000+00:00", "lastTransition": "2026-02-25T08:30:00.000+00:00", "lc": "soaking", "domain": "access", "subject": "eth-port"}),
			apicObj("faultInst", map[string]any{"dn": "uni/tn-tn_Example/fault-F2222", "code": "F2222", "severity": "critical", "cause": "config-failure", "descr": "Configuration deployment failed", "created": "2026-02-25T10:00:00.000+00:00", "lastTransition": "2026-02-25T10:15:00.000+00:00", "lc": "raised", "domain": "tenant", "subject": "config"}),
			apicObj("faultInst", map[string]any{"dn": "topology/pod-1/node-101/sys/old/fault-F9999", "code": "F9999", "severity": "cleared", "cause": "resolved", "descr": "Old cleared fault", "created": "2026-01-01T00:00:00.000+00:00", "lastTransition": "2026-02-01T00:00:00.000+00:00", "lc": "cleared", "domain": "infra", "subject": "test"}),
		},
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/api/aaaLogin.json" {
			json.NewEncoder(w).Encode(map[string]any{
				"imdata": []any{
					map[string]any{"aaaLogin": map[string]any{"attributes": map[string]any{"token": "test-token"}}},
				},
			})
			return
		}

		if strings.HasPrefix(r.URL.Path, "/api/class/") {
			className := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/class/"), ".json")
			data, ok := fixtures[className]
			if !ok {
				data = []any{}
			}
			json.NewEncoder(w).Encode(map[string]any{"imdata": data})
			return
		}

		w.WriteHeader(404)
	}))
}

func apicObj(className string, attrs map[string]any) map[string]any {
	return map[string]any{className: map[string]any{"attributes": attrs}}
}

func newTestProducer(t *testing.T, ts *httptest.Server, detailLevel string) (*Producer, *sdk.Context) {
	t.Helper()
	ctx := testharness.NewTestContext(t, testharness.WithConfig(&sdk.ProducerConfig{
		DetailLevel:     detailLevel,
		SafeFailureMode: sdk.FailClosed,
	}))
	return &Producer{
		target: run.TargetConfig{Host: "test", Username: "admin", Password: "test"},
		cfg:    &run.RunConfig{DetailLevel: detailLevel},
		client: &Client{
			baseURL:    ts.URL,
			httpClient: ts.Client(),
			token:      "test-token",
			logger:     ctx.Logger,
		},
	}, ctx
}

func TestCollect_Minimal(t *testing.T) {
	ts := fixtureServer(t)
	defer ts.Close()

	producer, ctx := newTestProducer(t, ts, "minimal")
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

	// Minimal: 3 nodes + 1 BD + 1 subnet + 1 L3Out = 6 resources.
	if len(doc.Topology.Resources) != 6 {
		t.Errorf("expected 6 resources, got %d", len(doc.Topology.Resources))
		for _, r := range doc.Topology.Resources {
			t.Logf("  resource: %s (%s)", r.ID, r.Type)
		}
	}

	typeCounts := countTypes(doc.Topology.Resources)
	assertCount(t, typeCounts, "network.controller", 1)
	assertCount(t, typeCounts, "network.switch.spine", 1)
	assertCount(t, typeCounts, "network.switch.leaf", 1)
	assertCount(t, typeCounts, "network.domain.bridge", 1)
	assertCount(t, typeCounts, "network.subnet", 1)
	assertCount(t, typeCounts, "network.l3out", 1)
	assertCount(t, typeCounts, "network.endpoint", 0) // minimal = no endpoints

	// No connections (L3Outs are resources now, not connections).
	if len(doc.Topology.Connections) != 0 {
		t.Errorf("expected 0 connections, got %d", len(doc.Topology.Connections))
	}

	// Groups: 2 tenants + 1 VRF + 1 EPG = 4.
	if len(doc.Topology.Groups) != 4 {
		t.Errorf("expected 4 groups, got %d", len(doc.Topology.Groups))
		for _, g := range doc.Topology.Groups {
			t.Logf("  group: %s (%s) members=%d children=%d", g.ID, g.Type, len(g.Members), len(g.Children))
		}
	}

	// Verify tenant tn_Example has wired relationships.
	exampleTenant := findGroup(doc.Topology.Groups, "tn_Example")
	if exampleTenant == nil {
		t.Fatal("missing tn_Example tenant group")
	}
	// Members: 1 BD + 1 subnet + 1 L3Out = 3 resource members.
	if len(exampleTenant.Members) != 3 {
		t.Errorf("tn_Example: expected 3 members (BD+subnet+L3Out), got %d: %v", len(exampleTenant.Members), exampleTenant.Members)
	}
	// Children: 1 VRF + 1 EPG = 2 child groups.
	if len(exampleTenant.Children) != 2 {
		t.Errorf("tn_Example: expected 2 children (VRF+EPG), got %d: %v", len(exampleTenant.Children), exampleTenant.Children)
	}

	// Verify marshaling works.
	if _, err := sdk.MarshalDocument(doc); err != nil {
		t.Fatalf("MarshalDocument failed: %v", err)
	}
}

func TestCollect_Detailed(t *testing.T) {
	ts := fixtureServer(t)
	defer ts.Close()

	producer, ctx := newTestProducer(t, ts, "detailed")
	doc, err := producer.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	// Detailed: 3 nodes + 1 BD + 1 subnet + 1 L3Out + 1 endpoint = 7.
	if len(doc.Topology.Resources) != 7 {
		t.Errorf("expected 7 resources, got %d", len(doc.Topology.Resources))
		for _, r := range doc.Topology.Resources {
			t.Logf("  resource: %s (%s)", r.ID, r.Type)
		}
	}

	typeCounts := countTypes(doc.Topology.Resources)
	assertCount(t, typeCounts, "network.endpoint", 1)

	// EPG should have the endpoint as a member.
	epgWEB := findGroup(doc.Topology.Groups, "epg_WEB")
	if epgWEB == nil {
		t.Fatal("missing epg_WEB group")
	}
	if len(epgWEB.Members) != 1 {
		t.Errorf("epg_WEB: expected 1 endpoint member, got %d", len(epgWEB.Members))
	}
}

func TestCollect_Deterministic(t *testing.T) {
	ts := fixtureServer(t)
	defer ts.Close()

	producer, ctx := newTestProducer(t, ts, "minimal")
	testharness.AssertDeterministic(t, producer, ctx)
}

func TestNewFactory(t *testing.T) {
	factory := NewFactory()
	p := factory(run.TargetConfig{Host: "10.0.0.1"}, &run.RunConfig{})
	if _, ok := p.(*Producer); !ok {
		t.Error("factory should return *Producer")
	}
}

func TestCollect_FaultExtensions(t *testing.T) {
	ts := fixtureServer(t)
	defer ts.Close()

	producer, ctx := newTestProducer(t, ts, "minimal")
	doc, err := producer.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	// Find the spine resource (node-101) - should have 1 fault (cleared filtered).
	var spine *sdk.Resource
	for i, r := range doc.Topology.Resources {
		if r.Type == "network.switch.spine" {
			spine = &doc.Topology.Resources[i]
			break
		}
	}
	if spine == nil {
		t.Fatal("missing spine resource")
	}

	if spine.Extensions == nil {
		t.Fatal("spine should have extensions")
	}
	cisco, ok := spine.Extensions["osiris.cisco"].(map[string]any)
	if !ok {
		t.Fatal("spine should have osiris.cisco extension")
	}

	// Check ACI metadata.
	if cisco["control_plane_mtu"] != 9000 {
		t.Errorf("control_plane_mtu: %v", cisco["control_plane_mtu"])
	}

	// Check faults - 1 active fault (F1527), cleared F9999 is filtered.
	faults, ok := cisco["faults"].([]Fault)
	if !ok || len(faults) != 1 {
		t.Fatalf("expected 1 fault on spine, got %v", cisco["faults"])
	}
	if faults[0].Code != "F1527" {
		t.Errorf("fault code: %s", faults[0].Code)
	}

	// Find tn_Example tenant - should have 1 fault.
	example := findGroup(doc.Topology.Groups, "tn_Example")
	if example == nil {
		t.Fatal("missing tn_Example group")
	}
	if example.Extensions == nil {
		t.Fatal("tn_Example should have extensions")
	}
	ciscoTenant, ok := example.Extensions["osiris.cisco"].(map[string]any)
	if !ok {
		t.Fatal("tn_Example should have osiris.cisco extension")
	}
	tenantFaults, ok := ciscoTenant["faults"].([]Fault)
	if !ok || len(tenantFaults) != 1 {
		t.Fatalf("expected 1 fault on tn_Example, got %v", ciscoTenant["faults"])
	}
	if tenantFaults[0].Code != "F2222" {
		t.Errorf("tenant fault code: %s", tenantFaults[0].Code)
	}

	// LEAF1 (node-111) should have 1 fault (F0532).
	var leaf *sdk.Resource
	for i, r := range doc.Topology.Resources {
		if r.Type == "network.switch.leaf" {
			leaf = &doc.Topology.Resources[i]
			break
		}
	}
	if leaf == nil {
		t.Fatal("missing leaf resource")
	}
	if leaf.Extensions == nil {
		t.Fatal("leaf should have extensions")
	}
	ciscoLeaf := leaf.Extensions["osiris.cisco"].(map[string]any)
	leafFaults, ok := ciscoLeaf["faults"].([]Fault)
	if !ok || len(leafFaults) != 1 {
		t.Fatalf("expected 1 fault on leaf, got %v", ciscoLeaf["faults"])
	}
	if leafFaults[0].Code != "F0532" {
		t.Errorf("leaf fault code: %s", leafFaults[0].Code)
	}

	// Common tenant should have no extensions.
	common := findGroup(doc.Topology.Groups, "common")
	if common != nil && common.Extensions != nil {
		t.Error("common tenant should have no extensions")
	}
}

func TestCollect_ACINodeExtensions(t *testing.T) {
	ts := fixtureServer(t)
	defer ts.Close()

	producer, ctx := newTestProducer(t, ts, "minimal")
	doc, err := producer.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	// APIC1 (controller) should have ACI extensions from topSystem.
	var ctrl *sdk.Resource
	for i, r := range doc.Topology.Resources {
		if r.Type == "network.controller" {
			ctrl = &doc.Topology.Resources[i]
			break
		}
	}
	if ctrl == nil {
		t.Fatal("missing controller resource")
	}
	if ctrl.Extensions == nil {
		t.Fatal("controller should have extensions")
	}
	cisco, ok := ctrl.Extensions["osiris.cisco"].(map[string]any)
	if !ok {
		t.Fatal("controller should have osiris.cisco extension")
	}
	if cisco["fabric_mac"] != "AA:BB:CC:00:00:01" {
		t.Errorf("fabric_mac: %v", cisco["fabric_mac"])
	}
	if cisco["control_plane_mtu"] != 9000 {
		t.Errorf("control_plane_mtu: %v", cisco["control_plane_mtu"])
	}
	if cisco["last_reboot_time"] != "2024-04-13T16:47:52.025+00:00" {
		t.Errorf("last_reboot_time: %v", cisco["last_reboot_time"])
	}
	if cisco["fabric_id"] != 1 {
		t.Errorf("fabric_id: %v", cisco["fabric_id"])
	}
}

// Test helpers ---

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
