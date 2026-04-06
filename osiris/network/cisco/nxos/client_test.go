// client_test.go - Unit tests for the NX-API CLI client.
// Covers login authentication, show commands, multi-command batching and error handling
// using httptest servers with canned responses.
//
// For an introduction to OSIRIS JSON Producer for Cisco see:
// "[OSIRIS-JSON-CISCO]."
//
// [OSIRIS-JSON-CISCO]: https://osirisjson.org/en/docs/producers/network/cisco

package nxos

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"go.osirisjson.org/producers/osiris/network/cisco/run"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func newTestClient(ts *httptest.Server) *Client {
	return &Client{
		baseURL:    ts.URL,
		httpClient: ts.Client(),
		username:   "admin",
		password:   "test",
		logger:     testLogger(),
	}
}

// nxapiResp builds a canned NX-API response for a single command.
func nxapiResp(body map[string]any) map[string]any {
	bodyBytes, _ := json.Marshal(body)
	return map[string]any{
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
}

// nxapiMultiResp builds a canned NX-API response for multiple commands.
func nxapiMultiResp(bodies ...map[string]any) map[string]any {
	var outputs []map[string]any
	for _, body := range bodies {
		bodyBytes, _ := json.Marshal(body)
		outputs = append(outputs, map[string]any{
			"code": "200",
			"msg":  "Success",
			"body": json.RawMessage(bodyBytes),
		})
	}
	return map[string]any{
		"ins_api": map[string]any{
			"outputs": map[string]any{
				"output": outputs,
			},
		},
	}
}

func TestLogin_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ins" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		user, pass, ok := r.BasicAuth()
		if !ok || user != "admin" || pass != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(nxapiResp(map[string]any{
			"sys_ver_str": "10.3(4a)",
			"chassis_id":  "Nexus9000 C9508",
		}))
	}))
	defer ts.Close()

	c := &Client{
		baseURL:    ts.URL,
		httpClient: ts.Client(),
		logger:     testLogger(),
	}
	err := c.Login("admin", "secret")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
}

func TestLogin_AuthFailure(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer ts.Close()

	c := &Client{
		baseURL:    ts.URL,
		httpClient: ts.Client(),
		logger:     testLogger(),
	}
	err := c.Login("admin", "wrong")
	if err == nil {
		t.Fatal("expected error for auth failure")
	}
	if !strings.Contains(err.Error(), "authentication failed") {
		t.Errorf("error should mention authentication: %v", err)
	}
}

func TestLogin_ConnectionError(t *testing.T) {
	c := &Client{
		baseURL:    "https://127.0.0.1:1",
		httpClient: &http.Client{},
		logger:     testLogger(),
	}
	err := c.Login("admin", "secret")
	if err == nil {
		t.Fatal("expected connection error")
	}
}

func TestShow_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(nxapiResp(map[string]any{
			"sys_ver_str": "10.3(4a)",
		}))
	}))
	defer ts.Close()

	c := newTestClient(ts)
	result, err := c.Show("show version")
	if err != nil {
		t.Fatalf("Show failed: %v", err)
	}
	if result["sys_ver_str"] != "10.3(4a)" {
		t.Errorf("unexpected version: %v", result["sys_ver_str"])
	}
}

func TestShow_CommandError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"ins_api": map[string]any{
				"outputs": map[string]any{
					"output": map[string]any{
						"code": "400",
						"msg":  "Invalid command",
						"body": json.RawMessage(`""`),
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	c := newTestClient(ts)
	_, err := c.Show("show invalid")
	if err == nil {
		t.Fatal("expected error for invalid command")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error should mention code 400: %v", err)
	}
}

func TestShow_HTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer ts.Close()

	c := newTestClient(ts)
	_, err := c.Show("show version")
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention 500: %v", err)
	}
}

func TestShowMulti_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(nxapiMultiResp(
			map[string]any{"sys_ver_str": "10.3(4a)"},
			map[string]any{"TABLE_inv": map[string]any{}},
		))
	}))
	defer ts.Close()

	c := newTestClient(ts)
	results, err := c.ShowMulti([]string{"show version", "show inventory"})
	if err != nil {
		t.Fatalf("ShowMulti failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0]["sys_ver_str"] != "10.3(4a)" {
		t.Errorf("unexpected version: %v", results[0]["sys_ver_str"])
	}
}

func TestShowMulti_SingleCommand(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(nxapiResp(map[string]any{"sys_ver_str": "10.3(4a)"}))
	}))
	defer ts.Close()

	c := newTestClient(ts)
	results, err := c.ShowMulti([]string{"show version"})
	if err != nil {
		t.Fatalf("ShowMulti with single command failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

func TestShowMulti_MaxCommands(t *testing.T) {
	c := &Client{logger: testLogger()}
	cmds := make([]string, maxMultiCommands+1)
	for i := range cmds {
		cmds[i] = "show version"
	}
	_, err := c.ShowMulti(cmds)
	if err == nil {
		t.Fatal("expected error for too many commands")
	}
	if !strings.Contains(err.Error(), "too many") {
		t.Errorf("error should mention too many: %v", err)
	}
}

func TestNewClient(t *testing.T) {
	target := run.TargetConfig{Host: "10.99.0.1", Port: 8443}
	c := NewClient(target, true, testLogger())
	if c.baseURL != "https://10.99.0.1:8443" {
		t.Errorf("unexpected baseURL: %s", c.baseURL)
	}
}

func TestNewClient_DefaultPort(t *testing.T) {
	target := run.TargetConfig{Host: "10.99.0.1"}
	c := NewClient(target, false, testLogger())
	if c.baseURL != "https://10.99.0.1:443" {
		t.Errorf("unexpected baseURL: %s", c.baseURL)
	}
}
