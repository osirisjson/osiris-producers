// client_security_test.go - Security tests for the Cisco Catalyst
// SD-WAN Manager REST API client: no response body or request
// parameter in diagnostics, same-origin-only redirects, and an
// end-to-end secret canary through --include-raw-body.
//
// Secret-shaped inputs use the approved synthetic placeholders only.
//
// OSIRIS JSON Producer for Cisco Catalyst SD-WAN Manager introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification
package catalystsdwanmanager

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"go.osirisjson.org/producers/pkg/sdk"
)

// secretBody is an error body carrying every canary.
const secretBody = `{"error":{"message":"password=changeme","details":"` + canaryBearer + `","url":"` + canaryURI + `"}}`

// newSecureTestClient builds a client through NewClient (so the real
// redirect policy and timeout apply) against a TLS test server.
func newSecureTestClient(t *testing.T, ts *httptest.Server) *Client {
	t.Helper()
	host, port := splitHostPort(t, ts.Listener.Addr().String())
	return NewClient(&Config{Host: host, Port: port, InsecureTLS: true, SiteNameRateLimit: 1000}, testLogger(t))
}

func TestClientErrors_NeverEchoResponseBodies(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		call    func(*Client) error
	}{
		{"login HTTP error", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, secretBody)
		}, func(c *Client) error { return c.Login("admin", "changeme") }},
		{"token HTTP error", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/j_security_check" {
				return
			}
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, secretBody)
		}, func(c *Client) error { return c.Login("admin", "changeme") }},
		{"GET HTTP error", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, secretBody)
		}, func(c *Client) error { _, err := c.GetDevices(); return err }},
		{"GET 429 exhausted", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, secretBody)
		}, func(c *Client) error { _, err := c.GetDevices(); return err }},
		{"GET with query", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
			fmt.Fprint(w, secretBody)
		}, func(c *Client) error { _, err := c.GetDeviceInterfaces("192.0.2.1"); return err }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewTLSServer(tt.handler)
			defer ts.Close()
			var logs bytes.Buffer
			c := newSecureTestClient(t, ts)
			c.logger = slog.New(slog.NewTextHandler(&logs, nil))
			err := tt.call(c)
			if err == nil {
				t.Fatal("expected an error")
			}
			for _, s := range []string{"changeme", canaryBearer, canaryURI, "192.0.2.1", `"error"`} {
				if strings.Contains(err.Error(), s) || strings.Contains(logs.String(), s) {
					t.Errorf("diagnostic leaks %q: err=%v logs=%s", s, err, logs.String())
				}
			}
		})
	}
}

func TestClient_RefusesCrossOriginRedirect(t *testing.T) {
	var leaked atomic.Bool
	other := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if bytes.Contains(body, []byte("changeme")) || r.Header.Get("X-XSRF-TOKEN") != "" {
			leaked.Store(true)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer other.Close()

	for _, code := range []int{http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		t.Run(fmt.Sprintf("login %d", code), func(t *testing.T) {
			leaked.Store(false)
			ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, other.URL+r.URL.Path, code)
			}))
			defer ts.Close()
			err := newSecureTestClient(t, ts).Login("admin", "changeme")
			if err == nil {
				t.Fatal("expected login to fail on a cross-origin redirect")
			}
			if !errors.Is(err, errCrossOriginRedirect) {
				t.Errorf("err = %v, want errCrossOriginRedirect", err)
			}
			if strings.Contains(err.Error(), other.URL) {
				t.Errorf("error echoes the redirect target: %v", err)
			}
			if leaked.Load() {
				t.Error("credentials reached the other origin")
			}
		})
	}

	t.Run("GET carries no XSRF token off origin", func(t *testing.T) {
		leaked.Store(false)
		ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, other.URL+r.URL.Path, http.StatusTemporaryRedirect)
		}))
		defer ts.Close()
		c := newSecureTestClient(t, ts)
		c.xsrfToken = "test"
		if _, err := c.GetDevices(); !errors.Is(err, errCrossOriginRedirect) {
			t.Errorf("err = %v, want errCrossOriginRedirect", err)
		}
		if leaked.Load() {
			t.Error("XSRF token reached the other origin")
		}
	})
}

func TestClient_FollowsSameOriginRedirect(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/dataservice/device":
			http.Redirect(w, r, "/dataservice/device/v2", http.StatusFound)
		case "/dataservice/device/v2":
			fmt.Fprint(w, `{"data":[{"deviceId":"192.0.2.1","host-name":"LAB-RTR01"}]}`)
		}
	}))
	defer ts.Close()
	devices, err := newSecureTestClient(t, ts).GetDevices()
	if err != nil {
		t.Fatalf("GetDevices: %v", err)
	}
	if len(devices) != 1 {
		t.Errorf("devices = %d, want 1", len(devices))
	}
}

func TestClient_StopsRedirectLoop(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.Path, http.StatusFound)
	}))
	defer ts.Close()
	if _, err := newSecureTestClient(t, ts).GetDevices(); err == nil || !strings.Contains(err.Error(), "redirects") {
		t.Errorf("err = %v, want a redirect-limit error", err)
	}
}

func TestOriginKey(t *testing.T) {
	a, b := mustURL(t, "https://LAB-MANAGER01.example.com/x"), mustURL(t, "https://lab-manager01.example.com:443/y")
	if originKey(a) != originKey(b) {
		t.Errorf("originKey(%v) != originKey(%v)", a, b)
	}
	for _, u := range []string{"http://lab-manager01.example.com/", "https://lab-manager01.example.com:8443/", "https://lab-manager02.example.com/"} {
		if originKey(mustURL(t, u)) == originKey(a) {
			t.Errorf("originKey(%s) must differ from %s", u, a)
		}
	}
}

// secretRawHandler serves the end-to-end fixture with a secret-bearing
// interface row and device field.
func secretRawHandler(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/dataservice/device/interface":
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[{"ifname":"ge0/0","vpn-id":"0","auth-key":"changeme","nested":[{"note":"`+canaryBearer+`"}]}]}`)
	default:
		fixtureHandlerWithRawBody(w, r)
	}
}

// TestRunExport_RawBodyNeverEmitsSecrets runs the full export with
// --include-raw-body under every --safe-failure-mode and checks no
// canary reaches any written document or file mode beyond 0600.
func TestRunExport_RawBodyNeverEmitsSecrets(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(secretRawHandler))
	defer ts.Close()
	host, port := splitHostPort(t, ts.Listener.Addr().String())
	for _, mode := range []string{sdk.FailClosed, sdk.LogAndRedact, sdk.Off} {
		t.Run(mode, func(t *testing.T) {
			out := t.TempDir()
			cfg := &Config{
				Host: host, Port: port, Username: "user", Password: "changeme", InsecureTLS: true,
				SafeFailureMode: mode, Purpose: "audit", IncludeRawBody: true,
				AllSites: true, SiteNameRateLimit: 1000, OutputDir: out,
			}
			if err := runExport(cfg); err != nil {
				t.Fatalf("runExport: %v", err)
			}
			n := 0
			err := filepath.WalkDir(out, func(path string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return err
				}
				n++
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				noSecrets(t, data)
				fi, _ := d.Info()
				if fi.Mode().Perm() != outputFileMode {
					t.Errorf("%s mode = %v, want %v", path, fi.Mode().Perm(), outputFileMode)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if n == 0 {
				t.Fatal("no document written")
			}
		})
	}
}

func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
