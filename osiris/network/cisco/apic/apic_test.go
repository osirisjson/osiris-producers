// apic_test.go - Integration tests for the Cisco ACI/APIC producer.
// Verifies end-to-end Collect behavior using a canned fixture server,
// including detail levels, fault wiring, ACI extensions
// and deterministic output.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package apic

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.osirisjson.org/producers/osiris/network/cisco/run"
	"go.osirisjson.org/producers/pkg/sdk"
	"go.osirisjson.org/producers/pkg/testharness"
)

// fixtureServer creates an httptest.Server that serves
// canned APIC responses.
func fixtureServer(t *testing.T) *httptest.Server {
	return fixtureServerWithFailures(t, nil)
}

// fixtureServerWithFailures is fixtureServer plus a map of
// class name -> HTTP status the class query should fail with, so a
// test can exercise the discovery failure policy.
func fixtureServerWithFailures(t *testing.T, failCodes map[string]int) *httptest.Server {
	t.Helper()

	fixtures := map[string]any{
		"fabricNode": []any{
			apicObj("fabricNode", map[string]any{"dn": "topology/pod-1/node-1", "name": "LAB-APIC1", "role": "controller", "serial": "TST00001", "model": "APIC-SERVER-L3", "version": "5.2(8h)", "address": "192.0.2.1", "id": "1", "fabricSt": "unknown"}),
			apicObj("fabricNode", map[string]any{"dn": "topology/pod-1/node-101", "name": "LAB-SPINE1", "role": "spine", "serial": "TST00101", "model": "N9K-C9508", "version": "n9000-15.2(8h)", "address": "192.0.2.101", "id": "101", "fabricSt": "active"}),
			apicObj("fabricNode", map[string]any{"dn": "topology/pod-1/node-111", "name": "LAB-LEAF1", "role": "leaf", "serial": "TST00111", "model": "N9K-C93180YC-FX", "version": "n9000-15.2(8h)", "address": "192.0.2.111", "id": "111", "fabricSt": "active"}),
		},
		"topSystem": []any{
			apicObj("topSystem", map[string]any{"dn": "topology/pod-1/node-1/sys", "name": "LAB-APIC1", "oobMgmtAddr": "198.51.100.1", "inbMgmtAddr": "198.51.100.201", "systemUpTime": "100:00:00:00.000", "state": "in-service", "fabricDomain": "MXP", "role": "controller", "serial": "TST00001", "fabricMAC": "AA:BB:CC:DD:00:01", "controlPlaneMTU": "9000", "lastRebootTime": "2024-04-13T16:47:52.025+00:00", "fabricId": "1"}),
			apicObj("topSystem", map[string]any{"dn": "topology/pod-1/node-101/sys", "name": "LAB-SPINE1", "oobMgmtAddr": "198.51.100.101", "state": "in-service", "role": "spine", "fabricMAC": "AA:BB:CC:DD:00:02", "controlPlaneMTU": "9000", "fabricId": "1"}),
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
			apicObj("fvBD", map[string]any{"dn": "uni/tn-tn_Example/BD-bd_App", "name": "bd_App", "descr": "Application bridge domain", "unicastRoute": "yes", "unkMacUcastAct": "proxy", "arpFlood": "yes", "mac": "00:00:5E:00:53:DD"}),
		},
		"fvSubnet": []any{
			apicObj("fvSubnet", map[string]any{"dn": "uni/tn-tn_Example/BD-bd_App/subnet-[203.0.113.1/24]", "ip": "203.0.113.1/24", "scope": "public", "preferred": "no"}),
		},
		"fvAEPg": []any{
			apicObj("fvAEPg", map[string]any{"dn": "uni/tn-tn_Example/ap-app1/epg-epg_WEB", "name": "epg_WEB", "descr": "Web EPG"}),
		},
		"l3extOut": []any{
			apicObj("l3extOut", map[string]any{"dn": "uni/tn-tn_Example/out-__ui_svi_dummy_id_0", "name": "__ui_svi_dummy_id_0", "descr": "dummy"}),
			apicObj("l3extOut", map[string]any{"dn": "uni/tn-tn_Example/out-l3out_Prod_1", "name": "l3out_Prod_1", "descr": "Production L3Out"}),
		},
		"fvCEp": []any{
			apicObj("fvCEp", map[string]any{"dn": "uni/tn-tn_Example/ap-app1/epg-epg_WEB/cep-00:00:5E:00:53:AA", "mac": "00:00:5E:00:53:AA", "encap": "vlan-100", "fabricPathDn": "topology/pod-1/paths-111/pathep-[eth1/1]"}),
		},
		"fvIp": []any{
			apicObj("fvIp", map[string]any{"dn": "uni/tn-tn_Example/ap-app1/epg-epg_WEB/cep-00:00:5E:00:53:AA/ip-[203.0.113.30]", "addr": "203.0.113.30"}),
		},
		"fvRsCtx": []any{
			apicObj("fvRsCtx", map[string]any{"dn": "uni/tn-tn_Example/BD-bd_App/rsctx", "tDn": "uni/tn-tn_Example/ctx-vrf_Prod_1", "state": "formed"}),
		},
		"fvRsBd": []any{
			apicObj("fvRsBd", map[string]any{"dn": "uni/tn-tn_Example/ap-app1/epg-epg_WEB/rsbd", "tDn": "uni/tn-tn_Example/BD-bd_App", "state": "formed"}),
		},
		"l3extRsEctx": []any{
			apicObj("l3extRsEctx", map[string]any{"dn": "uni/tn-tn_Example/out-l3out_Prod_1/rsectx", "tDn": "uni/tn-tn_Example/ctx-vrf_Prod_1", "state": "formed"}),
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
			if code, fail := failCodes[className]; fail {
				w.WriteHeader(code)
				json.NewEncoder(w).Encode(map[string]any{"imdata": []any{}})
				return
			}
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

func TestDedupeByDN(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	in := []map[string]any{
		{"dn": "uni/tn-A/ap-P/epg-E/cep-00:00:5E:00:53:01"},
		{"dn": "uni/tn-A/ap-P/epg-E/cep-00:00:5E:00:53:02"},
		{"dn": "uni/tn-A/ap-P/epg-E/cep-00:00:5E:00:53:01"}, // page-boundary repeat
		{"name": "no-dn-kept"},
		{"name": "no-dn-kept-2"},
	}
	out := dedupeByDN(in, "fvCEp", logger)
	if len(out) != 4 {
		t.Fatalf("got %d objects, want 4 (one duplicate dn dropped, both dn-less objects kept)", len(out))
	}
	counts := map[string]int{}
	for _, o := range out {
		if dn, _ := o["dn"].(string); dn != "" {
			counts[dn]++
		}
	}
	for dn, n := range counts {
		if n != 1 {
			t.Errorf("dn %q appears %d times after dedupe, want 1", dn, n)
		}
	}
}

func newTestProducer(t *testing.T, ts *httptest.Server, detailLevel string) (*Producer, *sdk.Context) {
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
		target: run.TargetConfig{Host: "test", Username: "admin", Password: "test"},
		cfg:    &Config{Purpose: purpose},
		client: &Client{
			baseURL:    ts.URL,
			httpClient: ts.Client(),
			token:      "test-token",
			username:   "admin",
			logger:     ctx.Logger,
			retryBase:  time.Millisecond, // keep failure-policy tests fast
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
	assertCount(t, typeCounts, "compute.server", 1)
	assertCount(t, typeCounts, "network.switch", 2) // 1 spine + 1 leaf
	assertCount(t, typeCounts, "osiris.cisco.domain.bridge", 1)
	assertCount(t, typeCounts, "network.subnet", 1)
	assertCount(t, typeCounts, "osiris.cisco.l3out", 1)
	assertCount(t, typeCounts, "network.interface", 0) // minimal = no endpoints

	// No connections (ACI relationships are modeled as group membership).
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
	assertCount(t, typeCounts, "network.interface", 1)

	// The endpoint carries its fvIp address in ip_addresses and its ACI
	// encapsulation in the vendor extension.
	var ep *sdk.Resource
	for i := range doc.Topology.Resources {
		if doc.Topology.Resources[i].Type == "network.interface" {
			ep = &doc.Topology.Resources[i]
			break
		}
	}
	if ep == nil {
		t.Fatal("missing network.interface endpoint resource")
	}
	if addrs, ok := ep.Properties["ip_addresses"].([]string); !ok || len(addrs) != 1 || addrs[0] != "203.0.113.30" {
		t.Errorf("endpoint ip_addresses: %v", ep.Properties["ip_addresses"])
	}
	if cisco, ok := ep.Extensions[extensionNamespace].(map[string]any); !ok || cisco["encap"] != "vlan-100" {
		t.Errorf("endpoint encap extension: %v", ep.Extensions)
	}

	// EPG should have the endpoint + BD as members.
	epgWEB := findGroup(doc.Topology.Groups, "epg_WEB")
	if epgWEB == nil {
		t.Fatal("missing epg_WEB group")
	}
	if len(epgWEB.Members) != 2 {
		t.Errorf("epg_WEB: expected 2 members (endpoint+BD), got %d: %v", len(epgWEB.Members), epgWEB.Members)
	}
}

func TestCollect_Deterministic(t *testing.T) {
	ts := fixtureServer(t)
	defer ts.Close()

	producer, ctx := newTestProducer(t, ts, "minimal")
	testharness.AssertDeterministic(t, producer, ctx)
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
		if r.Type == "network.switch" && r.Properties["role"] == "spine" {
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

	var leaf *sdk.Resource
	for i, r := range doc.Topology.Resources {
		if r.Type == "network.switch" && r.Properties["role"] == "leaf" {
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

func TestCollect_RelationshipWiring(t *testing.T) {
	ts := fixtureServer(t)
	defer ts.Close()

	producer, ctx := newTestProducer(t, ts, "minimal")
	doc, err := producer.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	// VRF should have BD + L3Out as members (via fvRsCtx and l3extRsEctx).
	vrfProd := findGroup(doc.Topology.Groups, "vrf_Prod_1")
	if vrfProd == nil {
		t.Fatal("missing vrf_Prod_1 group")
	}
	if len(vrfProd.Members) != 2 {
		t.Errorf("vrf_Prod_1: expected 2 members (BD+L3Out), got %d: %v", len(vrfProd.Members), vrfProd.Members)
	}

	// EPG should have BD as a member (via fvRsBd).
	epgWEB := findGroup(doc.Topology.Groups, "epg_WEB")
	if epgWEB == nil {
		t.Fatal("missing epg_WEB group")
	}
	if len(epgWEB.Members) != 1 {
		t.Errorf("epg_WEB: expected 1 BD member, got %d: %v", len(epgWEB.Members), epgWEB.Members)
	}

	// Verify BD resource ID is a member of both VRF and EPG.
	bdID := ""
	for _, r := range doc.Topology.Resources {
		if r.Type == "osiris.cisco.domain.bridge" {
			bdID = r.ID
			break
		}
	}
	if bdID == "" {
		t.Fatal("missing BD resource")
	}

	if !containsID(vrfProd.Members, bdID) {
		t.Errorf("BD %q not found in VRF members", bdID)
	}
	if !containsID(epgWEB.Members, bdID) {
		t.Errorf("BD %q not found in EPG members", bdID)
	}
}

func containsID(ids []string, target string) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}

func TestCollect_ACINodeExtensions(t *testing.T) {
	ts := fixtureServer(t)
	defer ts.Close()

	producer, ctx := newTestProducer(t, ts, "minimal")
	doc, err := producer.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	// LAB-APIC1 (controller) should have ACI extensions from topSystem.
	var ctrl *sdk.Resource
	for i, r := range doc.Topology.Resources {
		if r.Type == "compute.server" {
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
	if cisco["fabric_mac"] != "AA:BB:CC:DD:00:01" {
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

// Test helpers.

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

func coverageOf(t *testing.T, r *sdk.Resource) []map[string]any {
	t.Helper()
	if r.Extensions == nil {
		t.Fatal("resource has no extensions")
	}
	cisco, ok := r.Extensions["osiris.cisco"].(map[string]any)
	if !ok {
		t.Fatal("resource has no osiris.cisco extension")
	}
	cov, ok := cisco["coverage"].([]map[string]any)
	if !ok {
		t.Fatalf("resource has no coverage slice: %T", cisco["coverage"])
	}
	return cov
}

func covEntry(cov []map[string]any, op string) map[string]any {
	for _, e := range cov {
		if e["operation"] == op {
			return e
		}
	}
	return nil
}

func TestCollect_CoverageOnControllers(t *testing.T) {
	ts := fixtureServer(t)
	defer ts.Close()

	producer, ctx := newTestProducer(t, ts, "minimal")
	doc, err := producer.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	var controllers int
	for i := range doc.Topology.Resources {
		r := &doc.Topology.Resources[i]
		if r.Type != "compute.server" {
			continue
		}
		controllers++
		cov := coverageOf(t, r)

		fn := covEntry(cov, "fabricNode")
		if fn == nil || fn["status"] != "succeeded" || fn["count"] != 3 {
			t.Errorf("fabricNode coverage entry wrong: %v", fn)
		}
		// documentation purpose: fvCEp is skipped, not queried.
		cep := covEntry(cov, "fvCEp")
		if cep == nil || cep["status"] != "skipped" {
			t.Errorf("fvCEp should be recorded as skipped: %v", cep)
		}
		for _, e := range cov {
			if e["status"] == "failed" {
				t.Errorf("clean run should have no failed operations: %v", e)
			}
		}
	}
	if controllers == 0 {
		t.Fatal("no controller resource carried a coverage record")
	}

	if !strings.Contains(doc.Metadata.Scope.Description, "MXP") {
		t.Errorf("scope.description should name the fabric domain: %q", doc.Metadata.Scope.Description)
	}
}

func TestCollect_OptionalDiscoveryFailureDegrades(t *testing.T) {
	ts := fixtureServerWithFailures(t, map[string]int{"faultInst": http.StatusInternalServerError})
	defer ts.Close()

	producer, ctx := newTestProducer(t, ts, "minimal")
	doc, err := producer.Collect(ctx)
	if err != nil {
		t.Fatalf("optional-domain failure must not abort the run: %v", err)
	}

	// Core resources are still there.
	if len(doc.Topology.Resources) != 6 {
		t.Errorf("expected 6 resources despite faultInst failure, got %d", len(doc.Topology.Resources))
	}

	var sawFailed bool
	for i := range doc.Topology.Resources {
		if doc.Topology.Resources[i].Type != "compute.server" {
			continue
		}
		e := covEntry(coverageOf(t, &doc.Topology.Resources[i]), "faultInst")
		if e == nil || e["status"] != "failed" {
			t.Fatalf("faultInst should be recorded as failed: %v", e)
		}
		if e["category"] != "http-5xx" {
			t.Errorf("faultInst failure category = %v, want http-5xx", e["category"])
		}
		sawFailed = true
	}
	if !sawFailed {
		t.Fatal("no controller carried the degraded coverage record")
	}
	if !strings.Contains(doc.Metadata.Scope.Description, "faultInst (http-5xx)") {
		t.Errorf("scope.description should list the gap: %q", doc.Metadata.Scope.Description)
	}
}

func TestCollect_StructuralDiscoveryFailureAborts(t *testing.T) {
	ts := fixtureServerWithFailures(t, map[string]int{"fvBD": http.StatusInternalServerError})
	defer ts.Close()

	producer, ctx := newTestProducer(t, ts, "minimal")
	doc, err := producer.Collect(ctx)
	if err == nil {
		t.Fatal("a structural-domain failure must abort the document")
	}
	if doc != nil {
		t.Error("no document should be returned on a structural failure")
	}
	if !strings.Contains(err.Error(), "fvBD") || !strings.Contains(err.Error(), "structural") {
		t.Errorf("error should name the class and its criticality: %v", err)
	}
}

func TestCollect_EssentialDiscoveryFailureAborts(t *testing.T) {
	ts := fixtureServerWithFailures(t, map[string]int{"fabricNode": http.StatusInternalServerError})
	defer ts.Close()

	producer, ctx := newTestProducer(t, ts, "minimal")
	if _, err := producer.Collect(ctx); err == nil {
		t.Fatal("fabricNode failure must abort the document")
	}
}
