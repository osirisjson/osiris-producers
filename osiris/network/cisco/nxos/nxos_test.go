// nxos_test.go - Integration tests for the Cisco NX-OS producer.
// Verifies end-to-end Collect behavior using a canned fixture server,
// including detail levels, LLDP connections, VLAN/VRF membership and deterministic output.
//
// For an introduction to OSIRIS JSON Producer for Cisco see:
// "[OSIRIS-JSON-CISCO]."
//
// [OSIRIS-JSON-CISCO]: https://osirisjson.org/en/docs/producers/cisco

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

// fixtureServer creates an httptest.Server that serves canned NX-API responses.
// Routes commands by parsing the "input" field from the POST body.
func fixtureServer(t *testing.T) *httptest.Server {
	t.Helper()

	fixtures := map[string]map[string]any{
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
		"show lldp neighbors detail": {
			"TABLE_nbor_detail": map[string]any{
				"ROW_nbor_detail": map[string]any{
					"l_port_id": "Ethernet1/1",
					"sys_name":  "REMOTE-SW01",
					"port_id":   "Ethernet1/49",
					"mgmt_addr": "10.99.0.10",
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
		"show port-channel summary": {},
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

		// Parse semicolon-separated commands.
		commands := splitCommands(req.InsAPI.Input)

		if len(commands) == 1 {
			// Single command: return single output object.
			fixture := fixtures[commands[0]]
			if fixture == nil {
				fixture = map[string]any{}
			}
			bodyBytes, _ := json.Marshal(fixture)
			resp := map[string]any{
				"ins_api": map[string]any{
					"outputs": map[string]any{
						"output": map[string]any{
							"code": "200",
							"msg":  "Success",
							"body": json.RawMessage(bodyBytes),
						},
					},
				},
			}
			json.NewEncoder(w).Encode(resp)
			return
		}

		// Multiple commands: return array of outputs.
		var outputs []map[string]any
		for _, cmd := range commands {
			fixture := fixtures[cmd]
			if fixture == nil {
				fixture = map[string]any{}
			}
			bodyBytes, _ := json.Marshal(fixture)
			outputs = append(outputs, map[string]any{
				"code": "200",
				"msg":  "Success",
				"body": json.RawMessage(bodyBytes),
			})
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
		target: run.TargetConfig{Host: "10.99.0.1", Hostname: "LAB-SW01", Username: "admin", Password: "test"},
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
	assertCount(t, typeCounts, "network.switch.spine", 1)
	assertCount(t, typeCounts, "network.interface", 4) // 3 local + 1 LLDP stub
	assertCount(t, typeCounts, "network.interface.lag", 1)

	// Connections: 1 LLDP link.
	if len(doc.Topology.Connections) != 1 {
		t.Errorf("expected 1 connection, got %d", len(doc.Topology.Connections))
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
		if r.Type == "network.switch.spine" {
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
		if r.Type == "network.switch.spine" {
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

	if len(doc.Topology.Connections) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(doc.Topology.Connections))
	}

	conn := doc.Topology.Connections[0]
	if conn.Type != "network.link" {
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

func TestNewFactory(t *testing.T) {
	factory := NewFactory()
	p := factory(run.TargetConfig{Host: "10.99.0.1"}, &run.RunConfig{})
	if _, ok := p.(*Producer); !ok {
		t.Error("factory should return *Producer")
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
