// transform_security_test.go - Unit tests for transform_security.go.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package nxos

import (
	"encoding/json"
	"strings"
	"testing"

	"go.osirisjson.org/producers/pkg/sdk"
)

func TestTransformAAA(t *testing.T) {
	accounting := aaaAccountingResponse{
		TableAcctMethods: aaaAccountingTable{
			RowAcctMethods: rowList[aaaAccountingRow]{
				{Service: "default", Methods: "group tacacs+"},
			},
		},
	}
	authentication := aaaAuthenticationResponse{
		TableAuthenMethods: aaaAuthenticationTable{
			RowAuthenMethods: rowList[aaaAuthenticationRow]{
				{Service: "default", Method: "group tacacs+"},
				{Service: "console", Method: "local"},
			},
		},
	}
	groups := aaaGroupsResponse{
		TableGroups: aaaGroupTable{
			RowGroups: rowList[aaaGroupRow]{
				{Group: "radius"},
				{Group: "tacacs+"},
			},
		},
	}
	radius := radiusServerResponse{
		GlobalDeadtime:   "0",
		GlobalSecureMode: "none",
		GlobalSourceIntf: "any available",
		GlobalTimeout:    "5",
		ServerCount:      "0",
	}
	tacacs := tacacsServerResponse{
		GlobalDeadtime:   "0",
		GlobalSourceIntf: "mgmt0",
		GlobalTimeout:    "5",
		ServerCount:      "2",
		TableServer: tacacsServerTable{
			RowServer: rowList[tacacsServerRow]{
				{ServerIP: "198.51.100.10", Port: "49", Timeout: "30"},
				{ServerIP: "198.51.100.11", Port: "49", Timeout: "30"},
			},
		},
	}

	r, ok := TransformAAA("TST0000NX01", accounting, authentication, groups, radius, tacacs)
	if !ok {
		t.Fatal("expected ok=true, got false")
	}
	if r.Type != "osiris.cisco.aaa" {
		t.Errorf("type: %s", r.Type)
	}
	if r.ID != "cisco.nxos::TST0000NX01/aaa" {
		t.Errorf("id should be <provider>::<device-serial>/aaa, got %q", r.ID)
	}
	if r.Name != "AAA" {
		t.Errorf("name: %s", r.Name)
	}
	if r.Status != "active" {
		t.Errorf("status: %s", r.Status)
	}

	authMethods, ok := r.Properties["login_methods"].([]map[string]any)
	if !ok || len(authMethods) != 2 {
		t.Fatalf("login_methods: %v", r.Properties["login_methods"])
	}
	if authMethods[0]["service"] != "default" || authMethods[0]["method"] != "group tacacs+" {
		t.Errorf("login_methods[0]: %v", authMethods[0])
	}

	acctMethods, ok := r.Properties["accounting_methods"].([]map[string]any)
	if !ok || len(acctMethods) != 1 {
		t.Fatalf("accounting_methods: %v", r.Properties["accounting_methods"])
	}
	if acctMethods[0]["methods"] != "group tacacs+" {
		t.Errorf("accounting_methods[0]: %v", acctMethods[0])
	}

	serverGroups, ok := r.Properties["server_groups"].([]string)
	if !ok || len(serverGroups) != 2 {
		t.Fatalf("server_groups: %v", r.Properties["server_groups"])
	}

	radiusPosture, ok := r.Properties["radius"].(map[string]any)
	if !ok || radiusPosture["secure_mode"] != "none" || radiusPosture["timeout"] != "5" {
		t.Errorf("radius posture: %v", r.Properties["radius"])
	}
	if _, ok := radiusPosture["server_count"]; !ok || radiusPosture["server_count"] != 0 {
		t.Errorf("radius server_count should decode as int 0, got %v (%T)", radiusPosture["server_count"], radiusPosture["server_count"])
	}

	tacacsPosture, ok := r.Properties["tacacs"].(map[string]any)
	if !ok || tacacsPosture["source_interface"] != "mgmt0" {
		t.Errorf("tacacs posture: %v", r.Properties["tacacs"])
	}
	if tacacsPosture["server_count"] != 2 {
		t.Errorf("tacacs server_count should decode as int 2, got %v (%T)", tacacsPosture["server_count"], tacacsPosture["server_count"])
	}

	servers, ok := r.Properties["tacacs_servers"].([]map[string]any)
	if !ok || len(servers) != 2 {
		t.Fatalf("tacacs_servers: %v", r.Properties["tacacs_servers"])
	}
	if servers[0]["server_ip"] != "198.51.100.10" || servers[0]["port"] != 49 || servers[0]["timeout"] != 30 {
		t.Errorf("tacacs_servers[0]: %v", servers[0])
	}
}

func TestTransformAAA_NoDataReturnsNotOK(t *testing.T) {
	_, ok := TransformAAA("TST0000NX01", aaaAccountingResponse{}, aaaAuthenticationResponse{}, aaaGroupsResponse{}, radiusServerResponse{}, tacacsServerResponse{})
	if ok {
		t.Error("expected ok=false when every source command produced nothing")
	}
}

// TestTransformAAA_SecretFieldsNeverDecodeOrEmit is the canary test
// acceptance signal requires: a real "show tacacs-server"
// response shape carrying a live-looking secretKey/global_testPassword
// value must never surface anywhere in the resulting resource, because
// tacacsServerRow/tacacsServerResponse never give those fields a struct
// field to decode into in the first place (see their doc comments in
// dto.go) the strongest guarantee available, stronger than
// decode-then-filter.
func TestTransformAAA_SecretFieldsNeverDecodeOrEmit(t *testing.T) {
	const canary = "CANARY-SECRET-VALUE-DO-NOT-LEAK"
	raw := `{
		"global_deadtime": "0",
		"global_source_intf": "mgmt0",
		"global_testPassword": "` + canary + `",
		"global_testUsername": "test",
		"global_timeout": "5",
		"server_count": "1",
		"TABLE_server": {
			"ROW_server": {
				"port": "49",
				"secretKey": "` + canary + `",
				"server_ip": "198.51.100.10",
				"timeout": "30"
			}
		}
	}`

	var tacacs tacacsServerResponse
	if err := json.Unmarshal([]byte(raw), &tacacs); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	r, ok := TransformAAA("TST0000NX01", aaaAccountingResponse{}, aaaAuthenticationResponse{}, aaaGroupsResponse{}, radiusServerResponse{}, tacacs)
	if !ok {
		t.Fatal("expected ok=true (tacacs global posture + server list present)")
	}

	marshaled, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if strings.Contains(string(marshaled), canary) {
		t.Fatalf("canary secret leaked into emitted resource: %s", marshaled)
	}
}

func TestTransformAAAContainment(t *testing.T) {
	aaa := sdk.Resource{ID: "res-aaa-1", Name: "AAA"}
	conn := TransformAAAContainment("res-switch-01", "LAB-SW01", aaa)
	if conn.Type != "contains" {
		t.Errorf("type: %s", conn.Type)
	}
	if conn.Source != "res-switch-01" || conn.Target != "res-aaa-1" {
		t.Errorf("source/target: %s -> %s", conn.Source, conn.Target)
	}
	if conn.Direction != "forward" {
		t.Errorf("direction: %s", conn.Direction)
	}
	if conn.Status != "active" {
		t.Errorf("status: %s", conn.Status)
	}
}
