// client_test.go - Unit tests for the NX-API CLI client.
// Covers login authentication, show commands, multi-command batching
// and error handling using httptest servers with canned responses.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

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

func TestLogin_CachesVersionData(t *testing.T) {
	// the "show version" body Login fetches to validate
	// credentials must be retained (via VersionData) so nxos.go's
	// Collect can reuse it instead of fetching "show version" again.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	if got := c.VersionData(); got != nil {
		t.Fatalf("VersionData before Login: expected nil, got %v", got)
	}
	if err := c.Login("admin", "secret"); err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	vd := c.VersionData()
	if vd == nil {
		t.Fatal("VersionData after Login: expected non-nil")
	}
	if vd.ChassisID != "Nexus9000 C9508" {
		t.Errorf("VersionData chassis_id: %v", vd.ChassisID)
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
	var vd versionResponse
	if err := json.Unmarshal(result, &vd); err != nil {
		t.Fatalf("failed to decode Show result: %v", err)
	}
	if vd.SysVerStr != "10.3(4a)" {
		t.Errorf("unexpected version: %v", vd.SysVerStr)
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
	var vd versionResponse
	if err := json.Unmarshal(results[0].Body, &vd); err != nil {
		t.Fatalf("failed to decode results[0].Body: %v", err)
	}
	if vd.SysVerStr != "10.3(4a)" {
		t.Errorf("unexpected version: %v", vd.SysVerStr)
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

func TestShowMulti_PerCommandFailureIsolated(t *testing.T) {
	// one command failing within a multi-command batch must
	// not erase, or be reported as, a failure of the whole batch its
	// siblings' results (success or failure) must be unaffected.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"ins_api": map[string]any{
				"outputs": map[string]any{
					"output": []map[string]any{
						{"code": "200", "msg": "Success", "body": json.RawMessage(`{"a":1}`)},
						{"code": "400", "msg": "Feature not configured", "body": json.RawMessage(`""`)},
						{"code": "200", "msg": "Success", "body": json.RawMessage(`{"c":3}`)},
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	c := newTestClient(ts)
	results, err := c.ShowMulti([]string{"show a", "show lldp neighbors detail", "show c"})
	if err != nil {
		t.Fatalf("ShowMulti should not fail when only one command in the batch failed: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if results[0].Err != nil || string(results[0].Body) != `{"a":1}` {
		t.Errorf("results[0] = %+v, want successful body {a:1}", results[0])
	}
	if results[1].Err == nil {
		t.Error("results[1] should carry the command-level failure, not be silently empty")
	}
	if !strings.Contains(results[1].Err.Error(), "400") {
		t.Errorf("results[1].Err = %v, want mention of code 400", results[1].Err)
	}
	if results[2].Err != nil || string(results[2].Body) != `{"c":3}` {
		t.Errorf("results[2] = %+v, want successful body {c:3} - unaffected by results[1]'s failure", results[2])
	}
}

func TestShowMulti_UndecodableBodyIsPerCommandError(t *testing.T) {
	// A non-empty body that fails to decode as an object must surface
	// as that command's Err, never silently become an empty map
	// indistinguishable from a genuinely empty response.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"ins_api": map[string]any{
				"outputs": map[string]any{
					"output": map[string]any{
						"code": "200",
						"msg":  "Success",
						"body": json.RawMessage(`"not-an-object"`),
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	c := newTestClient(ts)
	results, err := c.ShowMulti([]string{"show weird"})
	if err != nil {
		t.Fatalf("unexpected transport-level error: %v", err)
	}
	if results[0].Err == nil {
		t.Error("expected a per-command decode error, not silent success")
	}
}

func TestShowMulti_EmptyStringBodyIsNotAnError(t *testing.T) {
	// A bare "" body means "no output" and must decode to an empty,
	// error-free map not be confused with the undecodable-body case
	// above.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"ins_api": map[string]any{
				"outputs": map[string]any{
					"output": map[string]any{
						"code": "200",
						"msg":  "Success",
						"body": json.RawMessage(`""`),
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	c := newTestClient(ts)
	results, err := c.ShowMulti([]string{"show nothing"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results[0].Err != nil {
		t.Errorf("empty string body should not be an error: %v", results[0].Err)
	}
	if string(results[0].Body) != "{}" {
		t.Errorf("expected an empty JSON object body, got %q", results[0].Body)
	}
}

func TestShowMulti_MismatchedResultCountIsTransportError(t *testing.T) {
	// The envelope claiming fewer/more results than commands sent is a
	// transport-level anomaly (a malformed or unexpected envelope), not
	// something attributable to any one command.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(nxapiMultiResp(map[string]any{"a": 1}))
	}))
	defer ts.Close()

	c := newTestClient(ts)
	_, err := c.ShowMulti([]string{"show a", "show b"})
	if err == nil {
		t.Fatal("expected a transport-level error for a mismatched result count")
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
	target := run.TargetConfig{Host: "192.0.2.1", Port: 8443}
	c := NewClient(target, true, testLogger())
	if c.baseURL != "https://192.0.2.1:8443" {
		t.Errorf("unexpected baseURL: %s", c.baseURL)
	}
}

func TestNewClient_DefaultPort(t *testing.T) {
	target := run.TargetConfig{Host: "192.0.2.1"}
	c := NewClient(target, false, testLogger())
	if c.baseURL != "https://192.0.2.1:443" {
		t.Errorf("unexpected baseURL: %s", c.baseURL)
	}
}

func TestNewClient_SetsRequestTimeout(t *testing.T) {
	target := run.TargetConfig{Host: "192.0.2.1"}
	c := NewClient(target, false, testLogger())
	if c.httpClient.Timeout != requestTimeout {
		t.Errorf("httpClient.Timeout = %v, want %v", c.httpClient.Timeout, requestTimeout)
	}
}

func TestNewClient_WarnsOnInsecure(t *testing.T) {
	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	NewClient(run.TargetConfig{Host: "192.0.2.1"}, true, logger)
	if !strings.Contains(buf.String(), "TLS certificate verification disabled") {
		t.Errorf("expected insecure warning log, got: %s", buf.String())
	}
}

func TestNewClient_NoWarningWhenSecure(t *testing.T) {
	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	NewClient(run.TargetConfig{Host: "192.0.2.1"}, false, logger)
	if strings.Contains(buf.String(), "TLS certificate verification disabled") {
		t.Errorf("unexpected insecure warning when insecure=false: %s", buf.String())
	}
}

func TestShowMulti_RetriesOn429ThenSucceeds(t *testing.T) {
	var attempts int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 2 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(nxapiResp(map[string]any{"sys_ver_str": "10.3(4a)"}))
	}))
	defer ts.Close()

	c := newTestClient(ts)
	results, err := c.ShowMulti([]string{"show version"})
	if err != nil {
		t.Fatalf("ShowMulti failed after retry: %v", err)
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts (1 retry), got %d", attempts)
	}
	var vd versionResponse
	if err := json.Unmarshal(results[0].Body, &vd); err != nil {
		t.Fatalf("failed to decode results[0].Body: %v", err)
	}
	if vd.SysVerStr != "10.3(4a)" {
		t.Errorf("unexpected version: %v", vd.SysVerStr)
	}
}

func TestShowMulti_RetriesOn503ThenSucceeds(t *testing.T) {
	var attempts int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(nxapiResp(map[string]any{"sys_ver_str": "10.3(4a)"}))
	}))
	defer ts.Close()

	c := newTestClient(ts)
	if _, err := c.ShowMulti([]string{"show version"}); err != nil {
		t.Fatalf("ShowMulti failed after retry: %v", err)
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts (1 retry), got %d", attempts)
	}
}

func TestShowMulti_NoRetryOn401(t *testing.T) {
	var attempts int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer ts.Close()

	c := newTestClient(ts)
	_, err := c.ShowMulti([]string{"show version"})
	if err == nil {
		t.Fatal("expected error for 401")
	}
	if attempts != 1 {
		t.Errorf("expected exactly 1 attempt (no retry on auth failure), got %d", attempts)
	}
}

func TestShowMulti_NoRetryOn403(t *testing.T) {
	var attempts int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusForbidden)
	}))
	defer ts.Close()

	c := newTestClient(ts)
	_, err := c.ShowMulti([]string{"show version"})
	if err == nil {
		t.Fatal("expected error for 403")
	}
	if attempts != 1 {
		t.Errorf("expected exactly 1 attempt (no retry on auth failure), got %d", attempts)
	}
}

func TestShowMulti_GivesUpAfterMaxRetries(t *testing.T) {
	var attempts int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer ts.Close()

	c := newTestClient(ts)
	_, err := c.ShowMulti([]string{"show version"})
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if attempts != max429Retries+1 {
		t.Errorf("expected %d attempts, got %d", max429Retries+1, attempts)
	}
}

func TestShowMulti_BoundedResponseBody(t *testing.T) {
	oversized := strings.Repeat("a", int(maxResponseBodyBytes)+1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(oversized))
	}))
	defer ts.Close()

	c := newTestClient(ts)
	_, err := c.ShowMulti([]string{"show version"})
	if err == nil {
		t.Fatal("expected error for oversized response body")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("error = %q, want mention of exceeding the size limit", err.Error())
	}
}

func TestReadLimitedBody_ExactLimitAllowed(t *testing.T) {
	data := strings.Repeat("a", 100)
	got, err := readLimitedBody(strings.NewReader(data), 100)
	if err != nil {
		t.Fatalf("unexpected error at exact limit: %v", err)
	}
	if string(got) != data {
		t.Errorf("got %d bytes, want %d", len(got), len(data))
	}
}

func TestReadLimitedBody_OverLimitRejected(t *testing.T) {
	data := strings.Repeat("a", 101)
	_, err := readLimitedBody(strings.NewReader(data), 100)
	if err == nil {
		t.Fatal("expected error for body over limit")
	}
}
