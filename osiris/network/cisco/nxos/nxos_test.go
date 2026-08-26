// nxos_test.go - Integration tests for the Cisco NX-OS producer.
// Verifies end-to-end Collect behavior using a canned fixture server,
// including detail levels, LLDP connections, VLAN/VRF membership
// and deterministic output.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package nxos

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.osirisjson.org/producers/osiris/network/cisco/run"
	"go.osirisjson.org/producers/pkg/sdk"
	"go.osirisjson.org/producers/pkg/testharness"
)

// fixtureBodies returns the canned NX-API command bodies shared by
// fixtureServer and fixtureServerWithFailingCommand, keyed by the
// exact CLI command string.
func fixtureBodies() map[string]map[string]any {
	return map[string]map[string]any{
		"show version": {
			"chassis_id":     "Nexus9000 C9508",
			"proc_board_id":  "TST0000NX01",
			"sys_ver_str":    "10.3(4a)",
			"host_name":      "LAB-SW01",
			"bios_ver_str":   "08.42",
			"rr_reason":      "Reset by CLI",
			"kern_uptm_days": "10",
			"kern_uptm_hrs":  "5",
			"kern_uptm_mins": "30",
			"kern_uptm_secs": "15",
			"memory":         float64(65536000),
		},
		"show inventory": {
			"TABLE_inv": map[string]any{
				"ROW_inv": []any{
					map[string]any{"name": "Chassis", "desc": "Nexus9000 C9508 Chassis", "productid": "N9K-C9508", "vendorid": "V01", "serialnum": "TST0000NX01"},
					map[string]any{"name": "Slot 1", "desc": "Supervisor", "productid": "N9K-SUP-B+", "serialnum": "TST0000SUP1"},
				},
			},
		},
		"show interface brief": {
			"TABLE_interface": map[string]any{
				"ROW_interface": []any{
					map[string]any{"interface": "Ethernet1/1", "state": "up", "speed": "10G", "type": "eth", "vlan": "100"},
					map[string]any{"interface": "Ethernet1/2", "state": "up", "speed": "10G", "type": "eth", "vlan": "200"},
					map[string]any{"interface": "port-channel10", "state": "up", "speed": "20G"},
					map[string]any{"interface": "loopback0", "state": "up"},
				},
			},
		},
		"show vlan brief": {
			"TABLE_vlanbriefxbrief": map[string]any{
				"ROW_vlanbriefxbrief": []any{
					map[string]any{"vlanshowbr-vlanid": "100", "vlanshowbr-vlanname": "PROD", "vlanshowbr-vlanstate": "active", "vlanshowplist-ifidx": "Ethernet1/1"},
					map[string]any{"vlanshowbr-vlanid": "200", "vlanshowbr-vlanname": "MGMT", "vlanshowbr-vlanstate": "active", "vlanshowplist-ifidx": "Ethernet1/2"},
				},
			},
		},
		"show vrf all detail": {
			"TABLE_vrf": map[string]any{

				"ROW_vrf": []any{
					map[string]any{
						"vrf_name": "PROD", "vrf_id": "3", "vrf_state": "Up",
						"TABLE_if": map[string]any{
							"ROW_if": []any{
								map[string]any{"if_name": "Ethernet1/1"},
								map[string]any{"if_name": "loopback0"},
							},
						},
					},
					map[string]any{
						"vrf_name": "MGMT", "vrf_id": "4", "vrf_state": "Up",
						"TABLE_if": map[string]any{
							"ROW_if": map[string]any{"if_name": "Ethernet1/2"},
						},
					},
				},
			},
		},
		"show vrf interface": {
			"TABLE_if": map[string]any{
				"ROW_if": []any{
					map[string]any{"if_name": "Ethernet1/1", "vrf_name": "PROD"},
					map[string]any{"if_name": "loopback0", "vrf_name": "PROD"},
					map[string]any{"if_name": "Ethernet1/2", "vrf_name": "MGMT"},
				},
			},
		},
		"show lldp neighbors detail": {
			"TABLE_nbor_detail": map[string]any{
				"ROW_nbor_detail": map[string]any{
					"l_port_id": "Ethernet1/1",
					"sys_name":  "REMOTE-SW01",
					"port_id":   "Ethernet1/49",
					"mgmt_addr": "192.0.2.10",
				},
			},
		},
		"show vpc brief": {
			"vpc-domain-id":             "10",
			"vpc-role":                  "primary",
			"vpc-peer-status":           "peer-ok",
			"vpc-peer-keepalive-status": "peer-alive",
			"TABLE_vpc": map[string]any{
				"ROW_vpc": map[string]any{
					"vpc-ifindex": "port-channel10",
				},
			},
		},
		"show port-channel summary": {
			"TABLE_channel": map[string]any{
				"ROW_channel": map[string]any{
					"group":        "10",
					"port-channel": "Po10",
					"TABLE_member": map[string]any{
						"ROW_member": []any{
							map[string]any{"port": "Eth1/1", "port-status": "P"},
							map[string]any{"port": "Eth1/2", "port-status": "P"},
						},
					},
				},
			},
		},
		"show interface": {
			"TABLE_interface": map[string]any{
				"ROW_interface": []any{
					map[string]any{"interface": "Ethernet1/1", "eth_mtu": float64(9216), "eth_bw": float64(10000000), "eth_duplex": "full", "eth_hw_addr": "aabb.ccdd.0001", "desc": "Uplink", "eth_outbytes": float64(1000000), "eth_inbytes": float64(2000000)},
					map[string]any{"interface": "Ethernet1/2", "eth_mtu": float64(1500), "eth_bw": float64(10000000), "eth_duplex": "full", "eth_hw_addr": "aabb.ccdd.0002"},
				},
			},
		},
		"show system resources": {
			"cpu_state_idle":    "95.50",
			"memory_usage_used": "8000000",
			"memory_usage_free": "4000000",
			"load_avg_1min":     "0.25",
		},
		"show environment": {
			"TABLE_psinfo": map[string]any{
				"ROW_psinfo": map[string]any{
					"psnum": "1", "psmodel": "NXA-PAC-1100W", "ps_status": "ok", "actual_out": "350 W",
				},
			},
			"TABLE_tempinfo": map[string]any{
				"ROW_tempinfo": map[string]any{
					"tempmod": "1", "sensor": "CPU", "curtemp": "42", "alarmstatus": "Ok",
				},
			},
		},
	}
}

// fixtureServer creates an httptest.Server that serves fixtureBodies'
// canned NX-API responses. Routes commands by parsing the "input"
// field from the POST body.
func fixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	return newFixtureServer(t, "", nil)
}

// fixtureServerWithFailingCommand behaves like fixtureServer but makes
// failCmd fail (CLI code 400) instead of returning its normal fixture,
// while every other command in the same batch still succeeds: one
// command failing inside a multi-command batch must not erase its
// siblings' data from the emitted document.
func fixtureServerWithFailingCommand(t *testing.T, failCmd string) *httptest.Server {
	t.Helper()
	return newFixtureServer(t, failCmd, nil)
}

// fixtureServerWithCommandCounter behaves like fixtureServer but also
// counts how many times each command string was requested, across every
// Show/ShowMulti call made against it regardless of whether the calls
// came from Client.Login or a later batch. "show version" must be
// requested exactly once per run, whether that one request happens
// during Login or (when Login was never called,
// e.g. an injected pre-authenticated Client) during batch 1.
func fixtureServerWithCommandCounter(t *testing.T) (*httptest.Server, map[string]int) {
	t.Helper()
	counts := make(map[string]int)
	return newFixtureServer(t, "", counts), counts
}

// newFixtureServer backs fixtureServer, fixtureServerWithFailingCommand
// and fixtureServerWithCommandCounter. When failCmd is non-empty, that
// one command returns CLI code 400 instead of its fixtureBodies() entry;
// every other command is unaffected. When counts is non-nil, every
// command seen (successful or not) increments its own entry.
func newFixtureServer(t *testing.T, failCmd string, counts map[string]int) *httptest.Server {
	t.Helper()
	fixtures := fixtureBodies()

	outputFor := func(cmd string) map[string]any {
		if counts != nil {
			counts[cmd]++
		}
		if failCmd != "" && cmd == failCmd {
			return map[string]any{"code": "400", "msg": "Command failed", "body": json.RawMessage(`""`)}
		}
		fixture := fixtures[cmd]
		if fixture == nil {
			fixture = map[string]any{}
		}
		bodyBytes, _ := json.Marshal(fixture)
		return map[string]any{"code": "200", "msg": "Success", "body": json.RawMessage(bodyBytes)}
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path != "/ins" {
			w.WriteHeader(404)
			return
		}

		body, _ := io.ReadAll(r.Body)
		var req struct {
			InsAPI struct {
				Input string `json:"input"`
			} `json:"ins_api"`
		}
		json.Unmarshal(body, &req)

		commands := splitCommands(req.InsAPI.Input)

		if len(commands) == 1 {
			resp := map[string]any{
				"ins_api": map[string]any{
					"outputs": map[string]any{
						"output": outputFor(commands[0]),
					},
				},
			}
			json.NewEncoder(w).Encode(resp)
			return
		}

		var outputs []map[string]any
		for _, cmd := range commands {
			outputs = append(outputs, outputFor(cmd))
		}
		resp := map[string]any{
			"ins_api": map[string]any{
				"outputs": map[string]any{
					"output": outputs,
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
}

// splitCommands parses semicolon-separated NX-API command input.
func splitCommands(input string) []string {
	var cmds []string
	for _, part := range splitSemicolon(input) {
		cmd := trimSpace(part)
		if cmd != "" {
			cmds = append(cmds, cmd)
		}
	}
	if len(cmds) == 0 {
		return []string{input}
	}
	return cmds
}

func splitSemicolon(s string) []string {
	var result []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ';' {
			result = append(result, s[start:i])
			start = i + 1
		}
	}
	result = append(result, s[start:])
	return result
}

func trimSpace(s string) string {
	i, j := 0, len(s)
	for i < j && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	for j > i && (s[j-1] == ' ' || s[j-1] == '\t') {
		j--
	}
	return s[i:j]
}

func newTestProducer(t *testing.T, ts *httptest.Server, detailLevel string) (*Producer, *sdk.Context) {
	t.Helper()
	ctx := testharness.NewTestContext(t, testharness.WithConfig(&sdk.ProducerConfig{
		DetailLevel:     detailLevel,
		SafeFailureMode: sdk.FailClosed,
	}))
	return &Producer{
		target: run.TargetConfig{Host: "192.0.2.1", Hostname: "LAB-SW01", Username: "admin", Password: "test"},
		cfg:    &run.RunConfig{DetailLevel: detailLevel},
		client: &Client{
			baseURL:    ts.URL,
			httpClient: ts.Client(),
			username:   "admin",
			password:   "test",
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

	// Resources: 1 device + 4 interfaces + 1 LLDP stub = 6.
	if len(doc.Topology.Resources) != 6 {
		t.Errorf("expected 6 resources, got %d", len(doc.Topology.Resources))
		for _, r := range doc.Topology.Resources {
			t.Logf("  resource: %s (%s) name=%s", r.ID, r.Type, r.Name)
		}
	}

	typeCounts := countTypes(doc.Topology.Resources)
	assertCount(t, typeCounts, "osiris.cisco.switch.spine", 1)
	assertCount(t, typeCounts, "network.interface", 4) // 3 local + 1 LLDP stub
	assertCount(t, typeCounts, "osiris.cisco.interface.lag", 1)

	// Connections: 1 LLDP link + 2 port-channel "contains"
	// (Eth1/1, Eth1/2 -> Po10).
	if len(doc.Topology.Connections) != 3 {
		t.Errorf("expected 3 connections, got %d", len(doc.Topology.Connections))
	}

	// Groups: 2 VLANs + 2 VRFs + 1 vPC = 5.
	if len(doc.Topology.Groups) != 5 {
		t.Errorf("expected 5 groups, got %d", len(doc.Topology.Groups))
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
	ts := fixtureServer(t)
	defer ts.Close()

	producer, ctx := newTestProducer(t, ts, "detailed")
	doc, err := producer.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	// Same resource count as minimal (detail enriches, doesn't add).
	if len(doc.Topology.Resources) != 6 {
		t.Errorf("expected 6 resources, got %d", len(doc.Topology.Resources))
	}

	// Verify interface enrichment: Ethernet1/1 should have mtu from detailed query.
	var eth1 *sdk.Resource
	for i, r := range doc.Topology.Resources {
		if r.Name == "Ethernet1/1" {
			eth1 = &doc.Topology.Resources[i]
			break
		}
	}
	if eth1 == nil {
		t.Fatal("missing Ethernet1/1 resource")
	}
	if eth1.Properties["mtu"] != int64(9216) {
		t.Errorf("Ethernet1/1 mtu: %v", eth1.Properties["mtu"])
	}
	if eth1.Properties["tx_bytes"] != int64(1000000) {
		t.Errorf("Ethernet1/1 tx_bytes: %v", eth1.Properties["tx_bytes"])
	}

	// Verify device has system resources extension.
	var device *sdk.Resource
	for i, r := range doc.Topology.Resources {
		if r.Type == "osiris.cisco.switch.spine" {
			device = &doc.Topology.Resources[i]
			break
		}
	}
	if device == nil {
		t.Fatal("missing device resource")
	}
	cisco := device.Extensions[extensionNamespace].(map[string]any)
	if cisco["cpu_idle"] != 95.50 {
		t.Errorf("cpu_idle: %v", cisco["cpu_idle"])
	}
	if cisco["load_avg_1min"] != 0.25 {
		t.Errorf("load_avg_1min: %v", cisco["load_avg_1min"])
	}

	// Verify environment extensions.
	psus, ok := cisco["power_supplies"].([]map[string]any)
	if !ok || len(psus) != 1 {
		t.Errorf("expected 1 PSU in extensions, got %v", cisco["power_supplies"])
	}
}

func TestCollect_Deterministic(t *testing.T) {
	ts := fixtureServer(t)
	defer ts.Close()

	producer, ctx := newTestProducer(t, ts, "minimal")
	testharness.AssertDeterministic(t, producer, ctx)
}

func TestCollect_DeviceExtensions(t *testing.T) {
	ts := fixtureServer(t)
	defer ts.Close()

	producer, ctx := newTestProducer(t, ts, "minimal")
	doc, err := producer.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	var device *sdk.Resource
	for i, r := range doc.Topology.Resources {
		if r.Type == "osiris.cisco.switch.spine" {
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

	// Verify device-level extensions.
	if cisco["bios_version"] != "08.42" {
		t.Errorf("bios_version: %v", cisco["bios_version"])
	}
	if cisco["last_reset_reason"] != "Reset by CLI" {
		t.Errorf("last_reset_reason: %v", cisco["last_reset_reason"])
	}
}

func TestCollect_LLDPConnections(t *testing.T) {
	ts := fixtureServer(t)
	defer ts.Close()

	producer, ctx := newTestProducer(t, ts, "minimal")
	doc, err := producer.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	var conn *sdk.Connection
	for i, c := range doc.Topology.Connections {
		if c.Type == "physical.ethernet" {
			conn = &doc.Topology.Connections[i]
			break
		}
	}
	if conn == nil {
		t.Fatalf("missing physical.ethernet (LLDP) connection among %d connections", len(doc.Topology.Connections))
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
		t.Fatal("missing LLDP stub resource")
	}
	if stub.Properties["remote_system"] != "REMOTE-SW01" {
		t.Errorf("stub remote_system: %v", stub.Properties["remote_system"])
	}
}

func TestCollect_VLANMembership(t *testing.T) {
	ts := fixtureServer(t)
	defer ts.Close()

	producer, ctx := newTestProducer(t, ts, "minimal")
	doc, err := producer.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	vlan100 := findGroup(doc.Topology.Groups, "VLAN 100")
	if vlan100 == nil {
		t.Fatal("missing VLAN 100 group")
	}
	if len(vlan100.Members) != 1 {
		t.Errorf("VLAN 100: expected 1 member (Ethernet1/1), got %d: %v", len(vlan100.Members), vlan100.Members)
	}

	vlan200 := findGroup(doc.Topology.Groups, "VLAN 200")
	if vlan200 == nil {
		t.Fatal("missing VLAN 200 group")
	}
	if len(vlan200.Members) != 1 {
		t.Errorf("VLAN 200: expected 1 member (Ethernet1/2), got %d: %v", len(vlan200.Members), vlan200.Members)
	}
}

func TestCollect_VRFMembership(t *testing.T) {
	ts := fixtureServer(t)
	defer ts.Close()

	producer, ctx := newTestProducer(t, ts, "minimal")
	doc, err := producer.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	prodVRF := findGroup(doc.Topology.Groups, "PROD")
	if prodVRF == nil {
		t.Fatal("missing PROD VRF group")
	}
	// PROD VRF should have Ethernet1/1 and loopback0 as members.
	if len(prodVRF.Members) != 2 {
		t.Errorf("PROD VRF: expected 2 members, got %d: %v", len(prodVRF.Members), prodVRF.Members)
	}

	mgmtVRF := findGroup(doc.Topology.Groups, "MGMT")
	if mgmtVRF == nil {
		t.Fatal("missing MGMT VRF group")
	}
	if len(mgmtVRF.Members) != 1 {
		t.Errorf("MGMT VRF: expected 1 member, got %d: %v", len(mgmtVRF.Members), mgmtVRF.Members)
	}
}

func TestCollect_LLDPFailureDoesNotEraseVPCOrPortChannel(t *testing.T) {
	// "show lldp neighbors detail" failing (a switch with LLDP disabled,
	// for example) must not also erase vPC or port-channel data fetched
	// successfully in the same batch. This was the exact all-or-nothing
	// bug: a single command's CLI-level failure used to fail the whole
	// ShowMulti call, and the caller's fallback zeroed every command in
	// that batch, not just the failed one.
	ts := fixtureServerWithFailingCommand(t, "show lldp neighbors detail")
	defer ts.Close()

	producer, ctx := newTestProducer(t, ts, "minimal")
	doc, err := producer.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect should not fail when only LLDP is unavailable: %v", err)
	}

	// No LLDP connection or stub LLDP genuinely failed. The 2
	// port-channel "contains" connections (Eth1/1, Eth1/2 -> Po10) must
	// still be present they come from an unrelated ShowMulti batch
	// entirely, but also prove that a failure inside batch 2 (LLDP)
	// does not erase port-channel data fetched in the very same batch.
	for _, c := range doc.Topology.Connections {
		if c.Type == "physical.ethernet" {
			t.Errorf("unexpected physical.ethernet (LLDP) connection: LLDP failed, should have produced none")
		}
	}
	if len(doc.Topology.Connections) != 2 {
		t.Errorf("expected 2 connections (port-channel contains only), got %d", len(doc.Topology.Connections))
	}

	// vPC group must still be present it succeeded in the same batch
	// as the failed LLDP command.
	vpcFound := false
	for _, g := range doc.Topology.Groups {
		if g.Type == "network.vpc" {
			vpcFound = true
			break
		}
	}
	if !vpcFound {
		t.Error("vPC group missing - LLDP's failure incorrectly erased sibling batch data")
	}

	// VLAN/VRF groups (from the unrelated batch1 call) must also be
	// completely unaffected.
	if len(doc.Topology.Groups) != 5 {
		t.Errorf("expected 5 groups (2 VLAN + 2 VRF + 1 vPC), got %d", len(doc.Topology.Groups))
	}
}

func TestCollect_PortChannelMembership(t *testing.T) {
	// "show port-channel summary" was fetched but never
	// consumed this verifies its response now produces "contains"
	// connections from the Po10 LAG resource to its physical members
	// and a member_count property on the LAG resource itself.
	ts := fixtureServer(t)
	defer ts.Close()

	producer, ctx := newTestProducer(t, ts, "minimal")
	doc, err := producer.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	resourceByID := make(map[string]sdk.Resource)
	var lag *sdk.Resource
	for i, r := range doc.Topology.Resources {
		resourceByID[r.ID] = r
		if r.Name == "port-channel10" {
			lag = &doc.Topology.Resources[i]
		}
	}
	if lag == nil {
		t.Fatal("missing port-channel10 resource")
	}
	if lag.Properties["member_count"] != 2 {
		t.Errorf("port-channel10 member_count: %v", lag.Properties["member_count"])
	}

	var containsConns []sdk.Connection
	for _, c := range doc.Topology.Connections {
		if c.Type == "contains" {
			containsConns = append(containsConns, c)
		}
	}
	if len(containsConns) != 2 {
		t.Fatalf("expected 2 contains connections, got %d", len(containsConns))
	}

	wantTargets := map[string]bool{"Ethernet1/1": false, "Ethernet1/2": false}
	for _, c := range containsConns {
		if c.Source != lag.ID {
			t.Errorf("contains connection source = %s, want %s (port-channel10)", c.Source, lag.ID)
		}
		if c.Direction != "forward" {
			t.Errorf("contains connection direction = %s, want forward", c.Direction)
		}
		target, ok := resourceByID[c.Target]
		if !ok {
			t.Fatalf("contains connection target %s not found in resources", c.Target)
		}
		if _, tracked := wantTargets[target.Name]; !tracked {
			t.Errorf("unexpected contains connection target: %s", target.Name)
			continue
		}
		wantTargets[target.Name] = true
		if c.Properties["port_status"] != "P" {
			t.Errorf("contains connection to %s: port_status = %v, want P", target.Name, c.Properties["port_status"])
		}
	}
	for name, seen := range wantTargets {
		if !seen {
			t.Errorf("missing contains connection to %s", name)
		}
	}
}

func TestCollect_ReusesLoginVersionData(t *testing.T) {
	// Login already fetches "show version" once to validate
	// credentials; Collect must reuse that body rather than fetching it
	// again in batch 1. Login and Collect share the same *Client here
	// (unlike newTestProducer's other callers, which inject a client
	// that never went through Login at all), so this is the one test
	// that actually exercises the login-then-collect path this fix
	// targets.
	ts, counts := fixtureServerWithCommandCounter(t)
	defer ts.Close()

	ctx := testharness.NewTestContext(t, testharness.WithConfig(&sdk.ProducerConfig{
		DetailLevel:     "minimal",
		SafeFailureMode: sdk.FailClosed,
	}))
	client := &Client{
		baseURL:    ts.URL,
		httpClient: ts.Client(),
		logger:     ctx.Logger,
	}
	if err := client.Login("admin", "test"); err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	producer := &Producer{
		target: run.TargetConfig{Host: "192.0.2.1", Hostname: "LAB-SW01", Username: "admin", Password: "test"},
		cfg:    &run.RunConfig{DetailLevel: "minimal"},
		client: client,
	}
	doc, err := producer.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	if counts["show version"] != 1 {
		t.Errorf(`"show version" requested %d times across Login+Collect, want exactly 1`, counts["show version"])
	}

	// The device resource must still be built correctly from the
	// Login-cached version data - not silently empty.
	var device *sdk.Resource
	for i, r := range doc.Topology.Resources {
		if r.Type == "osiris.cisco.switch.spine" {
			device = &doc.Topology.Resources[i]
			break
		}
	}
	if device == nil {
		t.Fatal("missing device resource")
	}
	if device.Properties["model"] != "Nexus9000 C9508" {
		t.Errorf("device model: %v", device.Properties["model"])
	}
}

func TestCollect_CoverageReportsSucceededAndFailed(t *testing.T) {
	// attempted/succeeded/failed/unavailable per command must
	// be visible in the emitted document itself,
	// not only in stderr logs.
	ts := fixtureServerWithFailingCommand(t, "show lldp neighbors detail")
	defer ts.Close()

	producer, ctx := newTestProducer(t, ts, "minimal")
	doc, err := producer.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	var device *sdk.Resource
	for i, r := range doc.Topology.Resources {
		if r.Type == "osiris.cisco.switch.spine" {
			device = &doc.Topology.Resources[i]
			break
		}
	}
	if device == nil {
		t.Fatal("missing device resource")
	}
	cisco, ok := device.Extensions[extensionNamespace].(map[string]any)
	if !ok {
		t.Fatal("device should have osiris.cisco extension")
	}
	coverage, ok := cisco["coverage"].([]map[string]any)
	if !ok {
		t.Fatalf("expected coverage extension, got %v", cisco["coverage"])
	}

	byCommand := make(map[string]map[string]any, len(coverage))
	for _, entry := range coverage {
		byCommand[entry["command"].(string)] = entry
	}

	failed, ok := byCommand["show lldp neighbors detail"]
	if !ok {
		t.Fatal("missing coverage entry for show lldp neighbors detail")
	}
	if failed["status"] != "failed" {
		t.Errorf("lldp status = %v, want failed", failed["status"])
	}
	if failed["error"] == nil || failed["error"] == "" {
		t.Error("failed coverage entry should carry its error message")
	}

	succeeded, ok := byCommand["show vpc brief"]
	if !ok {
		t.Fatal("missing coverage entry for show vpc brief")
	}
	if succeeded["status"] != "succeeded" {
		t.Errorf("vpc brief status = %v, want succeeded", succeeded["status"])
	}
	if _, hasErr := succeeded["error"]; hasErr {
		t.Error("succeeded coverage entry should not carry an error field")
	}

	// Every batch 1 command must also be represented (minimal mode still
	// issues "show version" in batch 1 here, since the fixture-injected
	// client bypasses Login).
	for _, cmd := range []string{"show version", "show inventory", "show interface brief", "show vlan brief", "show vrf all detail", "show vrf interface", "show port-channel summary"} {
		if _, ok := byCommand[cmd]; !ok {
			t.Errorf("missing coverage entry for %q", cmd)
		}
	}
}

func TestCollect_CoverageMarksWholeBatchUnavailableOnTransportFailure(t *testing.T) {
	// A batch-level transport failure (a malformed envelope, or a
	// result count that does not match the number of commands sent see
	// ShowMulti's contract) must still produce one "unavailable"
	// coverage entry per command in that batch, not silently omit them.
	// Batch 1 succeeds normally here; only batch 2 (identified by
	// containing the LLDP command) is made to return a mismatched
	// output count.
	fixtures := fixtureBodies()
	outputFor := func(cmd string) map[string]any {
		fixture := fixtures[cmd]
		if fixture == nil {
			fixture = map[string]any{}
		}
		bodyBytes, _ := json.Marshal(fixture)
		return map[string]any{"code": "200", "msg": "Success", "body": json.RawMessage(bodyBytes)}
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body, _ := io.ReadAll(r.Body)
		var req struct {
			InsAPI struct {
				Input string `json:"input"`
			} `json:"ins_api"`
		}
		json.Unmarshal(body, &req)
		commands := splitCommands(req.InsAPI.Input)

		for _, c := range commands {
			if c == "show lldp neighbors detail" {
				// Batch 2: declare 0 outputs for 3 commands sent
				// a transport-level mismatch, not a per-command failure.
				resp := map[string]any{
					"ins_api": map[string]any{
						"outputs": map[string]any{"output": []map[string]any{}},
					},
				}
				json.NewEncoder(w).Encode(resp)
				return
			}
		}

		if len(commands) == 1 {
			resp := map[string]any{"ins_api": map[string]any{"outputs": map[string]any{"output": outputFor(commands[0])}}}
			json.NewEncoder(w).Encode(resp)
			return
		}
		var outputs []map[string]any
		for _, cmd := range commands {
			outputs = append(outputs, outputFor(cmd))
		}
		json.NewEncoder(w).Encode(map[string]any{"ins_api": map[string]any{"outputs": map[string]any{"output": outputs}}})
	}))
	defer ts.Close()

	producer, ctx := newTestProducer(t, ts, "minimal")
	doc, err := producer.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect should tolerate a batch 2 transport failure: %v", err)
	}

	var device *sdk.Resource
	for i, r := range doc.Topology.Resources {
		if r.Type == "osiris.cisco.switch.spine" {
			device = &doc.Topology.Resources[i]
			break
		}
	}
	if device == nil {
		t.Fatal("missing device resource")
	}
	cisco, ok := device.Extensions[extensionNamespace].(map[string]any)
	if !ok {
		t.Fatal("device should have osiris.cisco extension")
	}
	coverage, ok := cisco["coverage"].([]map[string]any)
	if !ok {
		t.Fatalf("expected coverage extension, got %v", cisco["coverage"])
	}
	byCommand := make(map[string]map[string]any, len(coverage))
	for _, entry := range coverage {
		byCommand[entry["command"].(string)] = entry
	}
	for _, cmd := range []string{"show lldp neighbors detail", "show vpc brief", "show port-channel summary"} {
		entry, ok := byCommand[cmd]
		if !ok {
			t.Fatalf("missing coverage entry for %q", cmd)
		}
		if entry["status"] != "unavailable" {
			t.Errorf("%s status = %v, want unavailable", cmd, entry["status"])
		}
	}
}

func TestNewFactory(t *testing.T) {
	factory := NewFactory()
	p := factory(run.TargetConfig{Host: "192.0.2.1"}, &run.RunConfig{})
	if _, ok := p.(*Producer); !ok {
		t.Error("factory should return *Producer")
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
