// client_test.go - Unit tests for the APIC REST API client.
// Covers login authentication, class-based queries, pagination and error handling
// using httptest servers with canned responses.
//
// For an introduction to OSIRIS JSON Producer for Cisco see:
// "[OSIRIS-JSON-CISCO]."
//
// [OSIRIS-JSON-CISCO]: https://osirisjson.org/en/docs/producers/cisco

package apic

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

// newTestClient creates a Client pointing at the given httptest.Server.
func newTestClient(ts *httptest.Server) *Client {
	return &Client{
		baseURL:    ts.URL,
		httpClient: ts.Client(),
		logger:     testLogger(),
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
		baseURL:    "https://127.0.0.1:1",
		httpClient: &http.Client{},
		logger:     testLogger(),
	}
	err := c.Login("admin", "secret")
	if err == nil {
		t.Fatal("expected connection error")
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
	target := run.TargetConfig{Host: "10.0.0.1", Port: 8443}
	c := NewClient(target, true, testLogger())
	if c.baseURL != "https://10.0.0.1:8443" {
		t.Errorf("unexpected baseURL: %s", c.baseURL)
	}
}

func TestNewClient_DefaultPort(t *testing.T) {
	target := run.TargetConfig{Host: "10.0.0.1"}
	c := NewClient(target, false, testLogger())
	if c.baseURL != "https://10.0.0.1:443" {
		t.Errorf("unexpected baseURL: %s", c.baseURL)
	}
}
