// client_test.go - Unit tests for the APIC REST API client.
// Covers login authentication, class-based queries, pagination and
// error handling using httptest servers with canned responses.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package apic

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.osirisjson.org/producers/osiris/network/cisco/run"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// newTestClient creates a Client pointing at the given httptest.Server,
// with retry backoff collapsed to near-zero so retry paths test fast.
func newTestClient(ts *httptest.Server) *Client {
	return &Client{
		baseURL:    ts.URL,
		httpClient: ts.Client(),
		logger:     testLogger(),
		ctx:        context.Background(),
		retryBase:  time.Millisecond,
	}
}

func TestLogin_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/aaaLogin.json" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"imdata": []any{
				map[string]any{
					"aaaLogin": map[string]any{
						"attributes": map[string]any{
							"token": "test-token-abc123",
						},
					},
				},
			},
		})
	}))
	defer ts.Close()

	c := newTestClient(ts)
	err := c.Login("admin", "secret")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	if c.token != "test-token-abc123" {
		t.Errorf("expected token %q, got %q", "test-token-abc123", c.token)
	}
}

func TestLogin_AuthFailure(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"imdata":[],"totalCount":"0"}`))
	}))
	defer ts.Close()

	c := newTestClient(ts)
	err := c.Login("admin", "wrong")
	if err == nil {
		t.Fatal("expected error for auth failure")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error should mention 403: %v", err)
	}
}

func TestLogin_ConnectionError(t *testing.T) {
	c := &Client{
		baseURL:    "https://192.0.2.1:8443",
		httpClient: &http.Client{},
		logger:     testLogger(),
		ctx:        context.Background(),
		retryBase:  time.Millisecond,
		maxRetries: 1,
	}
	err := c.Login("admin", "secret")
	if err == nil {
		t.Fatal("expected connection error")
	}
}

// TestLogin_EncodesCredentialsAsJSON proves: a password with
// characters that would break naive string interpolation (a double
// quote and a backslash) still produces valid JSON that decodes back to
// the exact credential the caller passed.
func TestLogin_EncodesCredentialsAsJSON(t *testing.T) {
	const trickyUser = `ad"min\`
	const trickyPass = `p"a\ss"w\ord`

	var gotName, gotPwd string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req aaaLoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("server could not decode login body as JSON: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		gotName = req.AaaUser.Attributes.Name
		gotPwd = req.AaaUser.Attributes.Pwd
		json.NewEncoder(w).Encode(map[string]any{
			"imdata": []any{
				map[string]any{"aaaLogin": map[string]any{"attributes": map[string]any{"token": "tok"}}},
			},
		})
	}))
	defer ts.Close()

	c := newTestClient(ts)
	if err := c.Login(trickyUser, trickyPass); err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	if gotName != trickyUser {
		t.Errorf("name round-trip: got %q want %q", gotName, trickyUser)
	}
	if gotPwd != trickyPass {
		t.Errorf("pwd round-trip: got %q want %q", gotPwd, trickyPass)
	}
}

// TestLogin_RetriesTransientThenSucceeds proves: a 503 is
// retried and a subsequent 200 completes the login.
func TestLogin_RetriesTransientThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"imdata":[]}`))
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"imdata": []any{
				map[string]any{"aaaLogin": map[string]any{"attributes": map[string]any{"token": "tok"}}},
			},
		})
	}))
	defer ts.Close()

	c := newTestClient(ts)
	if err := c.Login("admin", "secret"); err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("expected 2 attempts (1 retry), got %d", got)
	}
}

// TestLogin_DoesNotRetryAuthFailure proves: a 403 is a
// classified, non-retryable error - exactly one request is made.
func TestLogin_DoesNotRetryAuthFailure(t *testing.T) {
	var calls atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"imdata":[],"totalCount":"0"}`))
	}))
	defer ts.Close()

	c := newTestClient(ts)
	if err := c.Login("admin", "wrong"); err == nil {
		t.Fatal("expected auth error")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("auth failure must not be retried: got %d attempts", got)
	}
}

// TestQueryClass_RetriesOn429 proves: HTTP 429 is transient.
func TestQueryClass_RetriesOn429(t *testing.T) {
	var calls atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"imdata": []any{}})
	}))
	defer ts.Close()

	c := newTestClient(ts)
	if _, err := c.QueryClass("fabricNode"); err != nil {
		t.Fatalf("QueryClass failed: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("expected 1 retry after 429, got %d attempts", got)
	}
}

// TestQueryClass_BodyLimit proves: a response larger than the
// configured cap is rejected rather than buffered without bound.
func TestQueryClass_BodyLimit(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"imdata":[`))
		blob := strings.Repeat("x", 4096)
		for i := 0; i < 64; i++ {
			io.WriteString(w, `"`+blob+`",`)
		}
		w.Write([]byte(`""]}`))
	}))
	defer ts.Close()

	c := newTestClient(ts)
	c.maxBodyBytes = 8 << 10 // 8 KiB
	if _, err := c.QueryClass("fabricNode"); err == nil {
		t.Fatal("expected body-limit error")
	} else if !strings.Contains(err.Error(), "limit") {
		t.Errorf("error should mention the limit: %v", err)
	}
}

// TestDoRequest_RootContextCancelled proves: a cancelled root
// context aborts promptly without exhausting retries.
func TestDoRequest_RootContextCancelled(t *testing.T) {
	var calls atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		json.NewEncoder(w).Encode(map[string]any{"imdata": []any{}})
	}))
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := newTestClient(ts)
	c.ctx = ctx
	done := make(chan struct{})
	go func() {
		_, err := c.QueryClass("fabricNode")
		if err == nil {
			t.Error("expected cancellation error")
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("QueryClass did not return promptly after context cancel")
	}
}

// TestPerRequestTimeout proves: a server that stalls past the
// per-request timeout fails within budget instead of hanging.
func TestPerRequestTimeout(t *testing.T) {
	release := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer ts.Close()
	defer close(release)

	c := newTestClient(ts)
	c.reqTimeout = 100 * time.Millisecond
	c.maxRetries = 1

	start := time.Now()
	_, err := c.QueryClass("fabricNode")
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("timeout not enforced: took %s", elapsed)
	}
}

// TestLogout_PostsAaaLogout proves: Logout calls aaaLogout
// naming the session user and clears the stored token.
func TestLogout_PostsAaaLogout(t *testing.T) {
	var path, name string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		var req aaaLoginRequest
		json.NewDecoder(r.Body).Decode(&req)
		name = req.AaaUser.Attributes.Name
		json.NewEncoder(w).Encode(map[string]any{"imdata": []any{}})
	}))
	defer ts.Close()

	c := newTestClient(ts)
	c.token = "live-token"
	c.username = "admin"
	if err := c.Logout(); err != nil {
		t.Fatalf("Logout failed: %v", err)
	}
	if path != "/api/aaaLogout.json" {
		t.Errorf("logout path: %q", path)
	}
	if name != "admin" {
		t.Errorf("logout user: %q", name)
	}
	if c.token != "" {
		t.Errorf("token not cleared after logout: %q", c.token)
	}
}

// TestLogout_NoSessionIsNoop proves: Logout with no token
// makes no HTTP call and returns nil.
func TestLogout_NoSessionIsNoop(t *testing.T) {
	var calls atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
	}))
	defer ts.Close()

	c := newTestClient(ts)
	if err := c.Logout(); err != nil {
		t.Fatalf("Logout should be a no-op, got: %v", err)
	}
	if calls.Load() != 0 {
		t.Errorf("Logout made %d HTTP calls with no session", calls.Load())
	}
}

func TestQueryClass_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/class/fabricNode.json") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"imdata": []any{
				map[string]any{
					"fabricNode": map[string]any{
						"attributes": map[string]any{
							"dn":   "topology/pod-1/node-1",
							"name": "APIC1",
							"role": "controller",
						},
					},
				},
				map[string]any{
					"fabricNode": map[string]any{
						"attributes": map[string]any{
							"dn":   "topology/pod-1/node-101",
							"name": "SPINE1",
							"role": "spine",
						},
					},
				},
			},
		})
	}))
	defer ts.Close()

	c := newTestClient(ts)
	results, err := c.QueryClass("fabricNode")
	if err != nil {
		t.Fatalf("QueryClass failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0]["name"] != "APIC1" {
		t.Errorf("expected name APIC1, got %v", results[0]["name"])
	}
}

func TestQueryClass_Pagination(t *testing.T) {
	callCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		w.Header().Set("Content-Type", "application/json")

		// Return pageSize items on first page, fewer on second.
		items := make([]any, 0)
		if page == "0" {
			for i := 0; i < pageSize; i++ {
				items = append(items, map[string]any{
					"fabricNode": map[string]any{
						"attributes": map[string]any{
							"dn": "topology/pod-1/node-" + strings.Repeat("0", i),
						},
					},
				})
			}
		} else {
			items = append(items, map[string]any{
				"fabricNode": map[string]any{
					"attributes": map[string]any{
						"dn": "topology/pod-1/node-last",
					},
				},
			})
		}
		callCount++
		json.NewEncoder(w).Encode(map[string]any{"imdata": items})
	}))
	defer ts.Close()

	c := newTestClient(ts)
	results, err := c.QueryClass("fabricNode")
	if err != nil {
		t.Fatalf("QueryClass failed: %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 API calls for pagination, got %d", callCount)
	}
	if len(results) != pageSize+1 {
		t.Errorf("expected %d results, got %d", pageSize+1, len(results))
	}
}

func TestQueryClass_HTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer ts.Close()

	c := newTestClient(ts)
	_, err := c.QueryClass("fabricNode")
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention 500: %v", err)
	}
}

func TestNewClient(t *testing.T) {
	target := run.TargetConfig{Host: "192.0.2.1", Port: 8443}
	c := NewClient(context.Background(), target, true, testLogger())
	if c.baseURL != "https://192.0.2.1:8443" {
		t.Errorf("unexpected baseURL: %s", c.baseURL)
	}
}

func TestNewClient_DefaultPort(t *testing.T) {
	target := run.TargetConfig{Host: "192.0.2.1"}
	c := NewClient(context.Background(), target, false, testLogger())
	if c.baseURL != "https://192.0.2.1:443" {
		t.Errorf("unexpected baseURL: %s", c.baseURL)
	}
}
