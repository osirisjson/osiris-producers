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
	"strings"
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
			"nxos_ver_str":   "10.3(4a)",
			"host_name":      "LAB-SW01",
			"bios_ver_str":   "08.42",
			"rr_reason":      "Reset by CLI",
			"kern_uptm_days": "10",
			"kern_uptm_hrs":  "5",
			"kern_uptm_mins": "30",
			"kern_uptm_secs": "15",
			"memory":         float64(65536000),
			"mem_type":       "kB",
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
		"show module": {
			"TABLE_modinfo": map[string]any{
				"ROW_modinfo": map[string]any{
					"modinf": "1", "model": "N9K-C93180YC-FX", "modtype": "48x10/25G/32G + 6x40/100G Ethernet/FC Module", "ports": "54", "status": "active *",
				},
			},
			"TABLE_moddiaginfo": map[string]any{
				"ROW_moddiaginfo": map[string]any{
					"mod": "1", "diagstatus": "Pass",
				},
			},
			"TABLE_modwwninfo": map[string]any{
				"ROW_modwwninfo": map[string]any{
					"modwwn": "1", "hw": "1.2", "sw": "10.3(6)", "slottype": "NA",
				},
			},
		},
		"show interface transceiver": {
			"TABLE_interface": map[string]any{
				"ROW_interface": []any{
					map[string]any{
						"interface": "Ethernet1/1", "sfp": "present", "name": "CISCO-ACCELINK",
						"cisco_product_id": "SFP-10G-SR", "partnum": "RTXM228-551-C98", "serialnum": "TST0000SFP01", "type": "10Gbase-SR",
					},
					map[string]any{"interface": "Ethernet1/2", "sfp": "not present"},
				},
			},
		},
		"show environment": {
			"powersup": map[string]any{
				"TABLE_psinfo": map[string]any{
					"ROW_psinfo": map[string]any{
						"psnum": "1", "psmodel": "NXA-PAC-1100W", "ps_status": "ok", "actual_out": "350 W", "tot_capa": "1100 W",
					},
				},
			},
			"fandetails": map[string]any{
				"TABLE_faninfo": map[string]any{
					"ROW_faninfo": map[string]any{
						"fanname": "Fan1(sys_fan1)", "fanmodel": "NXA-FAN-30CFM-F", "fanstatus": "Ok", "fandir": "back-to-front",
					},
				},
			},
			"TABLE_tempinfo": map[string]any{
				"ROW_tempinfo": map[string]any{
					"tempmod": "1", "sensor": "CPU", "curtemp": "42", "alarmstatus": "Ok", "majthres": "90", "minthres": "80",
				},
			},
		},
		"show aaa accounting": {
			"TABLE_acctMethods": map[string]any{
				"ROW_acctMethods": map[string]any{
					"service": "default", "methods": "group tacacs+",
				},
			},
		},
		"show aaa authentication": {
			"TABLE_AuthenMethods": map[string]any{
				"ROW_AuthenMethods": []any{
					map[string]any{"service": "default", "method": "group tacacs+"},
					map[string]any{"service": "console", "method": "local"},
				},
			},
		},
		"show aaa groups": {
			"TABLE_groups": map[string]any{
				"ROW_groups": []any{
					map[string]any{"group": "radius"},
					map[string]any{"group": "tacacs+"},
				},
			},
		},
		"show radius-server": {
			"global_deadtime":     "0",
			"global_secure_mode":  "none",
			"global_source_intf":  "any available",
			"global_timeout":      "5",
			"retransmissionCount": "1",
			"server_count":        "0",
		},
		"show tacacs-server": {
			"global_deadtime":     "0",
			"global_source_intf":  "mgmt0",
			"global_testPassword": "SECRET-NEVER-EMIT",
			"global_testUsername": "test",
			"global_timeout":      "5",
			"server_count":        "1",
			"TABLE_server": map[string]any{
				"ROW_server": map[string]any{
					"port": "49", "secretKey": "SECRET-NEVER-EMIT", "server_ip": "198.51.100.10", "timeout": "30",
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

func newTestProducer(t *testing.T, ts *httptest.Server, purpose string) (*Producer, *sdk.Context) {
	t.Helper()
	ctx := testharness.NewTestContext(t, testharness.WithConfig(&sdk.ProducerConfig{
		Purpose:         purpose,
		SafeFailureMode: sdk.FailClosed,
	}))
	return &Producer{
		target: run.TargetConfig{Host: "192.0.2.1", Hostname: "LAB-SW01", Username: "admin", Password: "test"},
		cfg:    &Config{},
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

	producer, ctx := newTestProducer(t, ts, "")
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
	if doc.Metadata.Generator.URL != generatorURL {
		t.Errorf("generator url = %q, want %q", doc.Metadata.Generator.URL, generatorURL)
	}
	if doc.Metadata.Scope.Name != "LAB-SW01" {
		t.Errorf("scope name = %q, want %q (device hostname)", doc.Metadata.Scope.Name, "LAB-SW01")
	}

	// Resources: 1 device + 4 interfaces + 2 network.vlan + 1 LLDP stub = 8.
	if len(doc.Topology.Resources) != 8 {
		t.Errorf("expected 8 resources, got %d", len(doc.Topology.Resources))
		for _, r := range doc.Topology.Resources {
			t.Logf("  resource: %s (%s) name=%s", r.ID, r.Type, r.Name)
		}
	}

	typeCounts := countTypes(doc.Topology.Resources)
	assertCount(t, typeCounts, "network.switch", 1)
	assertCount(t, typeCounts, "network.switch.port", 2)   // Ethernet1/1, Ethernet1/2
	assertCount(t, typeCounts, "network.interface", 2)     // loopback0 + LLDP stub
	assertCount(t, typeCounts, "network.interface.lag", 1) // port-channel10
	assertCount(t, typeCounts, "network.vlan", 2)          // VLAN 100, 200

	// Connections: 1 LLDP link + 2 port-channel "contains" (Eth1/1,
	// Eth1/2 -> Po10) + 2 switch contains.physical (the 2 Ethernet
	// ports) + 2 switch contains.logical (port-channel10, loopback0)
	// + 2 network.l2 VLAN membership (from "show vlan brief" port list).
	if len(doc.Topology.Connections) != 9 {
		t.Errorf("expected 9 connections, got %d", len(doc.Topology.Connections))
		for _, c := range doc.Topology.Connections {
			t.Logf("  connection: %s %s -> %s", c.Type, c.Source, c.Target)
		}
	}

	// Groups: 2 VRFs + 1 vPC = 3 (VLANs are resources now, not groups).
	if len(doc.Topology.Groups) != 3 {
		t.Errorf("expected 3 groups, got %d", len(doc.Topology.Groups))
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

	producer, ctx := newTestProducer(t, ts, "audit")
	doc, err := producer.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	if doc.Metadata.Scope.Purpose != "audit" {
		t.Errorf("scope purpose = %q, want %q", doc.Metadata.Scope.Purpose, "audit")
	}

	// One more resource than minimal (8): the osiris.cisco.aaa posture
	// resource (detail enriches every other resource, but AAA has no
	// equivalent to enrich in minimal mode it's whole-resource-new).
	if len(doc.Topology.Resources) != 9 {
		t.Errorf("expected 9 resources, got %d", len(doc.Topology.Resources))
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

	// Locate the device resource for the extension assertions below.
	var device *sdk.Resource
	for i, r := range doc.Topology.Resources {
		if r.Type == "network.switch" {
			device = &doc.Topology.Resources[i]
			break
		}
	}
	if device == nil {
		t.Fatal("missing device resource")
	}
	cisco := device.Extensions[extensionNamespace].(map[string]any)

	// "show system resources" is not collected: its cpu_idle/load_avg/
	// memory_used/free fields are volatile telemetry excluded by
	// OSIRIS-JSON-v1.0 13.1.3 even at audit purpose.
	for _, k := range []string{"cpu_idle", "load_avg_1min", "memory_used", "memory_free"} {
		if _, ok := cisco[k]; ok {
			t.Errorf("volatile telemetry key %q must not be emitted", k)
		}
	}

	// Verify environment extensions.
	psus, ok := cisco["power_supplies"].([]map[string]any)
	if !ok || len(psus) != 1 {
		t.Errorf("expected 1 PSU in extensions, got %v", cisco["power_supplies"])
	}
	if psus[0]["capacity"] != "1100 W" {
		t.Errorf("psu capacity: %v", psus[0]["capacity"])
	}
	if _, ok := psus[0]["actual_output"]; ok {
		t.Errorf("psu actual_output is volatile telemetry, must not appear: %v", psus[0]["actual_output"])
	}
	fans, ok := cisco["fans"].([]map[string]any)
	if !ok || len(fans) != 1 || fans[0]["status"] != "Ok" {
		t.Errorf("expected 1 fan in extensions, got %v", cisco["fans"])
	}
	temps, ok := cisco["temperature"].([]map[string]any)
	if !ok || len(temps) != 1 || temps[0]["major_threshold_c"] != "90" {
		t.Errorf("expected 1 temp sensor with thresholds, got %v", cisco["temperature"])
	}

	// Inventory/transceiver serial numbers are audit-only:
	// present here at audit purpose.
	inv, ok := cisco["inventory"].([]map[string]any)
	if !ok || inv[0]["serial"] != "TST0000NX01" {
		t.Errorf("expected chassis serial at audit purpose, got %v", cisco["inventory"])
	}
	var neighborConn *sdk.Connection
	for i, c := range doc.Topology.Connections {
		if c.Type == "physical.ethernet" {
			neighborConn = &doc.Topology.Connections[i]
			break
		}
	}
	if neighborConn == nil {
		t.Fatal("missing physical.ethernet connection")
	}
	xcvr, ok := neighborConn.Properties["source_transceiver"].(map[string]any)
	if !ok || xcvr["serial_number"] != "TST0000SFP01" {
		t.Errorf("expected transceiver serial_number at audit purpose, got %v", neighborConn.Properties["source_transceiver"])
	}

	// Verify the AAA posture resource and its containment connection.
	var aaa *sdk.Resource
	for i, r := range doc.Topology.Resources {
		if r.Type == "osiris.cisco.aaa" {
			aaa = &doc.Topology.Resources[i]
			break
		}
	}
	if aaa == nil {
		t.Fatal("missing osiris.cisco.aaa resource")
	}
	if servers, ok := aaa.Properties["tacacs_servers"].([]map[string]any); !ok || len(servers) != 1 {
		t.Errorf("tacacs_servers: %v", aaa.Properties["tacacs_servers"])
	}
	var containment *sdk.Connection
	for i, c := range doc.Topology.Connections {
		if c.Type == "contains" && c.Target == aaa.ID {
			containment = &doc.Topology.Connections[i]
			break
		}
	}
	if containment == nil {
		t.Error("missing switch -> AAA contains connection")
	}

	// Canary: the whole document, marshaled, must never carry the
	// TACACS secretKey/global_testPassword value fixtureBodies.
	docBytes, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal document failed: %v", err)
	}
	if strings.Contains(string(docBytes), "SECRET-NEVER-EMIT") {
		t.Fatal("TACACS secret leaked into the emitted document")
	}
}

func TestCollect_Deterministic(t *testing.T) {
	ts := fixtureServer(t)
	defer ts.Close()

	producer, ctx := newTestProducer(t, ts, "")
	testharness.AssertDeterministic(t, producer, ctx)
}

func TestCollect_DeviceExtensions(t *testing.T) {
	ts := fixtureServer(t)
	defer ts.Close()

	producer, ctx := newTestProducer(t, ts, "")
	doc, err := producer.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	var device *sdk.Resource
	for i, r := range doc.Topology.Resources {
		if r.Type == "network.switch" {
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

	// Verify inventory. Serial numbers are audit-only:
	// absent here at documentation purpose.
	inv, ok := cisco["inventory"].([]map[string]any)
	if !ok || len(inv) != 2 {
		t.Fatalf("expected 2 inventory items, got %v", cisco["inventory"])
	}
	if inv[0]["name"] != "Chassis" {
		t.Errorf("inventory[0].name: %v", inv[0]["name"])
	}
	if _, ok := inv[0]["serial"]; ok {
		t.Errorf("inventory serial must not appear at documentation purpose: %v", inv[0]["serial"])
	}

	// Verify module status (not audit-gated operational status/version,
	// not a security posture item).
	modules, ok := cisco["modules"].([]map[string]any)
	if !ok || len(modules) != 1 {
		t.Fatalf("expected 1 module, got %v", cisco["modules"])
	}
	if modules[0]["status"] != "active *" || modules[0]["diag_status"] != "Pass" || modules[0]["sw_version"] != "10.3(6)" {
		t.Errorf("module fields: %v", modules[0])
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

	producer, ctx := newTestProducer(t, ts, "")
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

	// Verify source_transceiver: Ethernet1/1 has a real SFP
	// present in the fixture, and this is its LLDP-discovered neighbor
	// connection. serial_number must not appear at documentation purpose.
	xcvr, ok := conn.Properties["source_transceiver"].(map[string]any)
	if !ok {
		t.Fatalf("missing source_transceiver on Ethernet1/1's connection: %v", conn.Properties)
	}
	if xcvr["model"] != "SFP-10G-SR" || xcvr["vendor"] != "CISCO-ACCELINK" {
		t.Errorf("source_transceiver fields: %v", xcvr)
	}
	if _, ok := xcvr["serial_number"]; ok {
		t.Errorf("source_transceiver.serial_number must not appear at documentation purpose: %v", xcvr["serial_number"])
	}
	if _, ok := conn.Properties["target_transceiver"]; ok {
		t.Errorf("target_transceiver must never be populated: %v", conn.Properties["target_transceiver"])
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

	producer, ctx := newTestProducer(t, ts, "")
	doc, err := producer.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	// VLANs are network.vlan resources (7.5.1); membership is a set of
	// network.l2 connections from the port to the VLAN resource.
	resByID := map[string]*sdk.Resource{}
	for i := range doc.Topology.Resources {
		r := &doc.Topology.Resources[i]
		resByID[r.ID] = r
	}

	// Expect Ethernet1/1 -> VLAN 100 and Ethernet1/2 -> VLAN 200
	// (matched by the target resource's vlan_id property).
	want := map[string]int{"Ethernet1/1": 100, "Ethernet1/2": 200}
	got := map[string]int{}
	for _, c := range doc.Topology.Connections {
		if c.Type != "network.l2" {
			continue
		}
		src, dst := resByID[c.Source], resByID[c.Target]
		if src == nil || dst == nil || dst.Type != "network.vlan" {
			t.Errorf("network.l2 connection has a bad endpoint: %s -> %s", c.Source, c.Target)
			continue
		}
		if id, ok := dst.Properties["vlan_id"].(int); ok {
			got[src.Name] = id
		}
	}
	for port, vlan := range want {
		if got[port] != vlan {
			t.Errorf("expected network.l2 %s -> VLAN %d, got %d", port, vlan, got[port])
		}
	}

	// The VLAN 100 resource itself is present and correctly shaped.
	var v100 *sdk.Resource
	for i := range doc.Topology.Resources {
		if doc.Topology.Resources[i].Type == "network.vlan" && doc.Topology.Resources[i].Properties["vlan_id"] == 100 {
			v100 = &doc.Topology.Resources[i]
		}
	}
	if v100 == nil {
		t.Fatal("missing VLAN 100 network.vlan resource")
	}
}

func TestCollect_VRFMembership(t *testing.T) {
	ts := fixtureServer(t)
	defer ts.Close()

	producer, ctx := newTestProducer(t, ts, "")
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

	producer, ctx := newTestProducer(t, ts, "")
	doc, err := producer.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect should not fail when only LLDP is unavailable: %v", err)
	}

	// No LLDP connection or stub LLDP genuinely failed. The 2
	// port-channel "contains" connections (Eth1/1, Eth1/2 -> Po10) and
	// the 4 switch containment connections (2 contains.physical + 2
	// contains.logical) must still be present they come from unrelated
	// ShowMulti batches / interface data entirely, but also prove that
	// a failure inside batch 2 (LLDP) does not erase port-channel data
	// fetched in the very same batch.
	for _, c := range doc.Topology.Connections {
		if c.Type == "physical.ethernet" {
			t.Errorf("unexpected physical.ethernet (LLDP) connection: LLDP failed, should have produced none")
		}
	}
	// 2 port-channel contains + 4 switch containment + 2 network.l2
	// VLAN membership = 8.
	if len(doc.Topology.Connections) != 8 {
		t.Errorf("expected 8 connections (port-channel contains + switch containment + VLAN l2), got %d", len(doc.Topology.Connections))
	}

	// vPC group must still be present it succeeded in the same batch
	// as the failed LLDP command.
	vpcFound := false
	for _, g := range doc.Topology.Groups {
		if g.Type == "osiris.cisco.vpc" {
			vpcFound = true
			break
		}
	}
	if !vpcFound {
		t.Error("vPC group missing - LLDP's failure incorrectly erased sibling batch data")
	}

	// VRF groups + vPC (from the unrelated batch1 call) must also be
	// completely unaffected. VLANs are resources now, not groups.
	if len(doc.Topology.Groups) != 3 {
		t.Errorf("expected 3 groups (2 VRF + 1 vPC), got %d", len(doc.Topology.Groups))
	}
	// The 2 network.vlan resources also survive LLDP's failure.
	vlanCount := 0
	for _, r := range doc.Topology.Resources {
		if r.Type == "network.vlan" {
			vlanCount++
		}
	}
	if vlanCount != 2 {
		t.Errorf("expected 2 network.vlan resources, got %d", vlanCount)
	}
}

func TestCollect_PortChannelMembership(t *testing.T) {
	// "show port-channel summary" was fetched but never
	// consumed this verifies its response now produces "contains"
	// connections from the Po10 LAG resource to its physical members
	// and a member_count property on the LAG resource itself.
	ts := fixtureServer(t)
	defer ts.Close()

	producer, ctx := newTestProducer(t, ts, "")
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

func TestCollect_PortChannelIdentityStableAcrossTargetAliasChange(t *testing.T) {
	// A port-channel/LAG resource goes through the same
	// TransformInterfaces code path as any other interface, so its
	// identity is stable across a target alias change for the same
	// reason a physical port's is: the canonical key is the device's
	// own reported chassis serial, not the target host/hostname used
	// to reach it.
	runWithTarget := func(host, hostname string) *sdk.Document {
		ts := fixtureServer(t)
		defer ts.Close()
		ctx := testharness.NewTestContext(t, testharness.WithConfig(&sdk.ProducerConfig{
			SafeFailureMode: sdk.FailClosed,
		}))
		producer := &Producer{
			target: run.TargetConfig{Host: host, Hostname: hostname, Username: "admin", Password: "test"},
			cfg:    &Config{},
			client: &Client{baseURL: ts.URL, httpClient: ts.Client(), username: "admin", password: "test", logger: ctx.Logger},
		}
		doc, err := producer.Collect(ctx)
		if err != nil {
			t.Fatalf("Collect failed: %v", err)
		}
		return doc
	}

	idFor := func(doc *sdk.Document, name string) string {
		for _, r := range doc.Topology.Resources {
			if r.Name == name {
				return r.ID
			}
		}
		t.Fatalf("missing resource named %q", name)
		return ""
	}

	docA := runWithTarget("192.0.2.1", "old-alias")
	docB := runWithTarget("192.0.2.99", "new-alias")

	if idFor(docA, "port-channel10") != idFor(docB, "port-channel10") {
		t.Error("port-channel10 resource ID changed across a target alias change")
	}
	if idFor(docA, "LAB-SW01") != idFor(docB, "LAB-SW01") {
		t.Error("switch resource ID changed across a target alias change")
	}
}

func TestCollect_P1NeighborAndSessionFeatures(t *testing.T) {
	// End-to-end coverage for merged LLDP/CDP neighbor discovery, vPC
	// keepalive, interface IP, OSPF/BGP neighbors, and switchport
	// native VLAN, through a real Collect() call on top of the base
	// fixtureBodies() set: CDP-only neighbor (mgmt0, no LLDP),
	// interface IP (bare, no prefix), vPC keepalive, OSPF neighbor,
	// BGP neighbor, and switchport native VLAN.
	fixtures := fixtureBodies()
	fixtures["show cdp neighbors detail"] = map[string]any{
		// Ethernet1/2 (already a known interface from the base fixture,
		// but with no LLDP neighbor of its own) a CDP-only
		// observation on an existing port, not requiring a new
		// interface resource this test doesn't otherwise set up.
		"TABLE_cdp_neighbor_detail_info": map[string]any{
			"ROW_cdp_neighbor_detail_info": map[string]any{
				"intf_id": "Ethernet1/2", "sysname": "REMOTE-MGMT01", "port_id": "Ethernet1/18", "v4mgmtaddr": "198.51.100.9",
			},
		},
	}
	fixtures["show vpc peer-keepalive"] = map[string]any{
		"vpc-peer-keepalive-status": "peer is alive",
		"vpc-dest":                  "198.51.100.5",
		"vpc-keepalive-vrf":         "management",
	}
	fixtures["show ip interface brief vrf all"] = map[string]any{
		"TABLE_intf": map[string]any{
			"ROW_intf": map[string]any{
				"intf-name": "Ethernet1/1", "prefix": "203.0.113.1", "vrf-name-out": "default",
			},
		},
	}
	fixtures["show ip ospf neighbor vrf all"] = map[string]any{
		"TABLE_ctx": map[string]any{
			"ROW_ctx": map[string]any{
				"cname": "default",
				"TABLE_nbr": map[string]any{
					"ROW_nbr": map[string]any{
						"rid": "203.0.113.2", "state": "FULL", "drstate": "DR", "addr": "203.0.113.2", "intf": "Ethernet1/1",
					},
				},
			},
		},
	}
	fixtures["show bgp all summary"] = map[string]any{
		"TABLE_vrf": map[string]any{
			"ROW_vrf": map[string]any{
				"vrf-name-out": "default",
				"TABLE_af": map[string]any{
					"ROW_af": map[string]any{
						"TABLE_saf": map[string]any{
							"ROW_saf": map[string]any{
								"TABLE_neighbor": map[string]any{
									"ROW_neighbor": map[string]any{
										"neighborid": "203.0.113.3", "neighboras": "65000", "state": "Established", "prefixreceived": "5",
									},
								},
							},
						},
					},
				},
			},
		},
	}
	fixtures["show interface switchport"] = map[string]any{
		"TABLE_interface": map[string]any{
			"ROW_interface": map[string]any{
				"interface": "Ethernet1/1", "native_vlan": "100", "trunk_vlans": "100,200-201",
			},
		},
	}

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
		if len(commands) == 1 {
			json.NewEncoder(w).Encode(map[string]any{"ins_api": map[string]any{"outputs": map[string]any{"output": outputFor(commands[0])}}})
			return
		}
		var outputs []map[string]any
		for _, cmd := range commands {
			outputs = append(outputs, outputFor(cmd))
		}
		json.NewEncoder(w).Encode(map[string]any{"ins_api": map[string]any{"outputs": map[string]any{"output": outputs}}})
	}))
	defer ts.Close()

	producer, ctx := newTestProducer(t, ts, "audit") // audit: exercises OSPF/BGP (NXOS-P1-10)
	doc, err := producer.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	// CDP-only neighbor on mgmt0.
	var cdpStub *sdk.Resource
	for i, r := range doc.Topology.Resources {
		if r.Name == "REMOTE-MGMT01:Ethernet1/18" {
			cdpStub = &doc.Topology.Resources[i]
		}
	}
	if cdpStub == nil {
		t.Error("missing CDP-discovered neighbor stub")
	} else if cdpStub.Provider.Name != unknownProviderName {
		t.Errorf("CDP stub provider = %s, want %s", cdpStub.Provider.Name, unknownProviderName)
	}

	// Interface IP + switchport native VLAN, both on Ethernet1/1.
	var eth1 *sdk.Resource
	for i, r := range doc.Topology.Resources {
		if r.Name == "Ethernet1/1" {
			eth1 = &doc.Topology.Resources[i]
		}
	}
	if eth1 == nil {
		t.Fatal("missing Ethernet1/1 resource")
	}
	if eth1.Properties["ip_address"] != "203.0.113.1" {
		t.Errorf("ip_address: %v", eth1.Properties["ip_address"])
	}
	if eth1.Properties["native_vlan"] != 100 {
		t.Errorf("native_vlan: %v", eth1.Properties["native_vlan"])
	}

	// Trunk VLAN membership: Ethernet1/1's trunk_vlans ("100,200-201")
	// should produce a network.l2 connection from it to the VLAN 100
	// and VLAN 200 resources (both exist in the base fixture); VLAN 201
	// has no resource and is silently skipped.
	vlanResVID := map[string]int{}
	for _, r := range doc.Topology.Resources {
		if r.Type == "network.vlan" {
			if id, ok := r.Properties["vlan_id"].(int); ok {
				vlanResVID[r.ID] = id
			}
		}
	}
	l2Targets := map[int]bool{}
	for _, c := range doc.Topology.Connections {
		if c.Type == "network.l2" && c.Source == eth1.ID {
			l2Targets[vlanResVID[c.Target]] = true
		}
	}
	if !l2Targets[100] {
		t.Error("Ethernet1/1 should have a network.l2 connection to VLAN 100 via trunk_vlans")
	}
	if !l2Targets[200] {
		t.Error("Ethernet1/1 should have a network.l2 connection to VLAN 200 via trunk_vlans")
	}

	// vPC keepalive, OSPF neighbor, BGP neighbor connections.
	var haveKeepalive, haveOSPF, haveBGP bool
	for _, c := range doc.Topology.Connections {
		switch {
		case c.Type == "network" && func() bool {
			cisco, _ := c.Extensions[extensionNamespace].(map[string]any)
			return cisco["role"] == "vpc_keepalive"
		}():
			haveKeepalive = true
		case c.Type == "network.ospf":
			haveOSPF = true
		case c.Type == "network.bgp":
			haveBGP = true
		}
	}
	if !haveKeepalive {
		t.Error("missing vPC keepalive connection")
	}
	if !haveOSPF {
		t.Error("missing OSPF neighbor connection")
	}
	if !haveBGP {
		t.Error("missing BGP neighbor connection")
	}
}

// TestCollect_DocumentationPurposeOmitsAuditTierData confirms
// "documentation" purpose (the default) never issues, let alone emits,
// any of the audit-tier commands/resources this producer gates behind
// --purpose audit: OSPF/BGP neighbor connections, the osiris.cisco.aaa
// resource, and the system-resources/environment device extensions.
func TestCollect_DocumentationPurposeOmitsAuditTierData(t *testing.T) {
	ts := fixtureServer(t)
	defer ts.Close()

	producer, ctx := newTestProducer(t, ts, "")
	doc, err := producer.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	for _, r := range doc.Topology.Resources {
		if r.Type == "osiris.cisco.aaa" {
			t.Error("osiris.cisco.aaa resource must not appear at documentation purpose")
		}
	}
	for _, c := range doc.Topology.Connections {
		if c.Type == "network.ospf" || c.Type == "network.bgp" {
			t.Errorf("audit-tier connection %s must not appear at documentation purpose", c.Type)
		}
	}
	for _, r := range doc.Topology.Resources {
		if r.Type != "network.switch" {
			continue
		}
		cisco, _ := r.Extensions[extensionNamespace].(map[string]any)
		if _, ok := cisco["power_supplies"]; ok {
			t.Error("environment extension must not appear at documentation purpose")
		}
	}
	for _, entry := range coverageEntries(t, doc) {
		cmd, _ := entry["command"].(string)
		if cmd == "show ip ospf neighbor vrf all" || cmd == "show bgp all summary" || cmd == "show aaa accounting" {
			t.Errorf("audit-tier command %q must not even be issued at documentation purpose", cmd)
		}
	}
}

// TestCollect_IncludeRawBody confirms --include-raw-body only takes
// effect at audit purpose (matching sdk.ProducerConfig.IncludeRawBody's
// documented contract) and, when it does, attaches real command bodies
// under extensions.osiris.cisco.raw_commands.
func TestCollect_IncludeRawBody(t *testing.T) {
	ts := fixtureServer(t)
	defer ts.Close()

	cases := []struct {
		name           string
		purpose        string
		includeRawBody bool
		wantRaw        bool
	}{
		{"audit + include-raw-body", "audit", true, true},
		{"audit without include-raw-body", "audit", false, false},
		{"documentation + include-raw-body ignored", "", true, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := testharness.NewTestContext(t, testharness.WithConfig(&sdk.ProducerConfig{
				Purpose:         tc.purpose,
				IncludeRawBody:  tc.includeRawBody,
				SafeFailureMode: sdk.FailClosed,
			}))
			producer := &Producer{
				target: run.TargetConfig{Host: "192.0.2.1", Hostname: "LAB-SW01", Username: "admin", Password: "test"},
				cfg:    &Config{},
				client: &Client{baseURL: ts.URL, httpClient: ts.Client(), username: "admin", password: "test", logger: ctx.Logger},
			}
			doc, err := producer.Collect(ctx)
			if err != nil {
				t.Fatalf("Collect failed: %v", err)
			}

			var device *sdk.Resource
			for i, r := range doc.Topology.Resources {
				if r.Type == "network.switch" {
					device = &doc.Topology.Resources[i]
					break
				}
			}
			if device == nil {
				t.Fatal("missing device resource")
			}
			cisco, _ := device.Extensions[extensionNamespace].(map[string]any)
			raw, ok := cisco["raw_commands"].(map[string]json.RawMessage)

			if tc.wantRaw {
				if !ok || len(raw) == 0 {
					t.Fatalf("expected raw_commands to be populated, got %v", cisco["raw_commands"])
				}
				if _, ok := raw["show version"]; !ok {
					t.Error(`raw_commands missing "show version"`)
				}
				// Sensitive keys in a raw body (the fixture plants
				// TACACS secretKey / global_testPassword = "SECRET-NEVER-EMIT")
				// must be redacted, never attached verbatim.
				tb, ok := raw["show tacacs-server"]
				if !ok {
					t.Fatal(`raw_commands missing "show tacacs-server"`)
				}
				if strings.Contains(string(tb), "SECRET-NEVER-EMIT") {
					t.Errorf("raw show tacacs-server body leaked a secret value: %s", tb)
				}
				if !strings.Contains(string(tb), redactRawBodyMarker) {
					t.Errorf("raw show tacacs-server body should carry the redaction marker: %s", tb)
				}
				// Document-wide canary.
				full, mErr := sdk.MarshalDocument(doc)
				if mErr != nil {
					t.Fatalf("MarshalDocument: %v", mErr)
				}
				if strings.Contains(string(full), "SECRET-NEVER-EMIT") {
					t.Error("marshaled document leaked a secret from a raw command body")
				}
			} else if ok && len(raw) > 0 {
				t.Errorf("raw_commands should be absent/empty, got %v", raw)
			}
		})
	}
}

func TestRedactRawBody(t *testing.T) {
	in := `{
	  "global_testPassword": "test",
	  "global_testUsername": "svc-tacacs",
	  "TABLE_server": {"ROW_server": [
	    {"server_ip": "198.51.100.10", "secretKey": "topsecret", "port": "49"}
	  ]},
	  "note": "-----BEGIN OPENSSH PRIVATE KEY-----AAAA",
	  "plain": "nothing here"
	}`
	out, ok := redactRawBody(json.RawMessage(in))
	if !ok {
		t.Fatal("redactRawBody returned ok=false for valid JSON")
	}
	s := string(out)
	for _, leaked := range []string{"test", "topsecret", "BEGIN OPENSSH PRIVATE KEY"} {
		if strings.Contains(s, leaked) {
			t.Errorf("redacted body still contains %q: %s", leaked, s)
		}
	}
	// Non-sensitive fields survive.
	for _, kept := range []string{"svc-tacacs", "198.51.100.10", "nothing here", `"port":"49"`} {
		if !strings.Contains(strings.ReplaceAll(s, " ", ""), strings.ReplaceAll(kept, " ", "")) {
			t.Errorf("redacted body dropped a non-sensitive value %q: %s", kept, s)
		}
	}
	if _, ok := redactRawBody(json.RawMessage(`not json`)); ok {
		t.Error("redactRawBody should return ok=false for unparseable input")
	}
}

// coverageEntries returns the device resource's own coverage array
// (see recordCoverage in nxos.go), or nil if absent.
func coverageEntries(t *testing.T, doc *sdk.Document) []map[string]any {
	t.Helper()
	for _, r := range doc.Topology.Resources {
		if r.Type != "network.switch" {
			continue
		}
		cisco, _ := r.Extensions[extensionNamespace].(map[string]any)
		cov, _ := cisco["coverage"].([]map[string]any)
		return cov
	}
	return nil
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
		cfg:    &Config{},
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
		if r.Type == "network.switch" {
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

	producer, ctx := newTestProducer(t, ts, "")
	doc, err := producer.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	var device *sdk.Resource
	for i, r := range doc.Topology.Resources {
		if r.Type == "network.switch" {
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

	producer, ctx := newTestProducer(t, ts, "")
	doc, err := producer.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect should tolerate a batch 2 transport failure: %v", err)
	}

	var device *sdk.Resource
	for i, r := range doc.Topology.Resources {
		if r.Type == "network.switch" {
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

func contains(members []string, id string) bool {
	for _, m := range members {
		if m == id {
			return true
		}
	}
	return false
}
