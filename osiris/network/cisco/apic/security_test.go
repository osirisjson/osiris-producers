// security_test.go - Credential-safety canary tests for the Cisco
// ACI/APIC producer. Proves that a login secret never
// reaches the emitted OSIRIS JSON document or the diagnostic log
// stream, in either --purpose mode, that a secret-shaped value in APIC
// data aborts a fail-closed build without being echoed and that
// untrusted APIC response content never reaches a diagnostic.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package apic

import (
	"bytes"
	"context"
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

func TestCollect_NoCredentialLeak(t *testing.T) {
	const (
		canaryUser = "CANARY-USER-1a2b3c"
		canaryPass = "CANARY-PW-9f3a2b4c5d"
	)

	for _, purpose := range []string{"documentation", "audit"} {
		t.Run(purpose, func(t *testing.T) {
			ts := fixtureServer(t)
			defer ts.Close()

			var logBuf bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

			producer := &Producer{
				target: run.TargetConfig{Host: "test", Username: canaryUser, Password: canaryPass},
				cfg:    &Config{Purpose: purpose},
				ctx:    context.Background(),
				client: &Client{
					baseURL:    ts.URL,
					httpClient: ts.Client(),
					token:      "test",
					username:   canaryUser,
					logger:     logger,
					ctx:        context.Background(),
				},
			}
			ctx := testharness.NewTestContext(t, testharness.WithConfig(&sdk.ProducerConfig{
				Purpose:         purpose,
				SafeFailureMode: sdk.FailClosed,
			}))
			ctx.Logger = logger

			doc, err := producer.Collect(ctx)
			if err != nil {
				t.Fatalf("Collect failed: %v", err)
			}

			data, err := sdk.MarshalDocument(doc)
			if err != nil {
				t.Fatalf("MarshalDocument failed: %v", err)
			}

			if bytes.Contains(data, []byte(canaryPass)) {
				t.Error("password canary leaked into the emitted document")
			}
			if bytes.Contains(data, []byte(canaryUser)) {
				t.Error("username canary leaked into the emitted document")
			}
			if strings.Contains(logBuf.String(), canaryPass) {
				t.Error("password canary leaked into the log stream")
			}
		})
	}
}

// Synthetic secret canaries.
// Each matches an SDK value pattern and is synthetic data.
const (
	canaryBearer = "Bearer CANARYfaultTOKEN0001"
	canaryAWSKey = "AKIACANARY000000TEST"
	canaryURI    = "https://admin:secret@example.com/x"
	canaryBody   = "CANARY-BODY-7e1d0c"
)

// collectWith runs Collect against srv with the given purpose and safe
// failure mode, capturing every log record. It returns the serialized
// document (nil on error), the error and the log text.
func collectWith(t *testing.T, srv string, client *http.Client, purpose, mode string) ([]byte, error, string) {
	t.Helper()
	return collect(t, srv, client, &sdk.ProducerConfig{Purpose: purpose, SafeFailureMode: mode})
}

// collectWithRaw is collectWith in fail-closed mode with
// --include-raw-body set as given.
func collectWithRaw(t *testing.T, srv string, client *http.Client, purpose string, raw bool) ([]byte, error, string) {
	t.Helper()
	return collect(t, srv, client, &sdk.ProducerConfig{Purpose: purpose, SafeFailureMode: sdk.FailClosed, IncludeRawBody: raw})
}

// collect runs Collect against srv with cfg, capturing every log record.
func collect(t *testing.T, srv string, client *http.Client, cfg *sdk.ProducerConfig) ([]byte, error, string) {
	t.Helper()
	purpose := cfg.Purpose
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	producer := &Producer{
		target: run.TargetConfig{Host: "test"},
		cfg:    &Config{Purpose: purpose},
		ctx:    context.Background(),
		client: &Client{baseURL: srv, httpClient: client, token: "test", username: "admin", logger: logger, ctx: context.Background()},
	}
	ctx := testharness.NewTestContext(t, testharness.WithConfig(cfg))
	ctx.Logger = logger
	doc, err := producer.Collect(ctx)
	if err != nil {
		return nil, err, logBuf.String()
	}
	data, mErr := sdk.MarshalDocument(doc)
	if mErr != nil {
		t.Fatalf("MarshalDocument: %v", mErr)
	}
	return data, nil, logBuf.String()
}

// TestCollect_SecretInAPICDataFailsClosed proves: a secret-shaped value
// in any APIC string that reaches the document (a fault description
// held in a typed extension slice, a tenant group description, a node
// resource name, a bridge-domain description) aborts a fail-closed
// build and the value never appears in the returned error or the log
// stream. Each case first runs with scanning off to prove the value is
// actually emitted for that purpose.
func TestCollect_SecretInAPICDataFailsClosed(t *testing.T) {
	cases := []struct {
		name     string
		canary   string
		override map[string]any
	}{
		{"fault description", canaryBearer, map[string]any{"faultInst": []any{
			apicObj("faultInst", map[string]any{"dn": "topology/pod-1/node-101/sys/fault-F1527", "code": "F1527", "severity": "major", "cause": "test", "descr": "login failed with " + canaryBearer, "lc": "raised", "domain": "infra", "subject": "test"}),
		}}},
		{"tenant description", canaryAWSKey, map[string]any{"fvTenant": []any{
			apicObj("fvTenant", map[string]any{"dn": "uni/tn-tn_Example", "name": "tn_Example", "descr": "key " + canaryAWSKey}),
		}}},
		{"bridge-domain description", canaryURI, map[string]any{"fvBD": []any{
			apicObj("fvBD", map[string]any{"dn": "uni/tn-tn_Example/BD-bd_App", "name": "bd_App", "descr": canaryURI}),
		}}},
		{"contract description", canaryBearer, map[string]any{"vzBrCP": []any{
			apicObj("vzBrCP", map[string]any{"dn": "uni/tn-tn_Example/brc-web", "name": "web", "descr": "token " + canaryBearer}),
		}}},
		{"application profile description", canaryAWSKey, map[string]any{"fvAp": []any{
			apicObj("fvAp", map[string]any{"dn": "uni/tn-tn_Example/ap-app1", "name": "app1", "descr": canaryAWSKey}),
		}}},
		{"filter entry name", canaryURI, map[string]any{
			"vzFilter": []any{apicObj("vzFilter", map[string]any{"dn": "uni/tn-tn_Example/flt-f", "name": "f"})},
			"vzEntry":  []any{apicObj("vzEntry", map[string]any{"dn": "uni/tn-tn_Example/flt-f/e-x", "name": canaryURI, "prot": "tcp"})},
		}},
		{"logical path description", canaryBearer, map[string]any{"fabricPathEp": []any{
			apicObj("fabricPathEp", map[string]any{"dn": "topology/pod-1/paths-111/pathep-[LAB-PC-1]", "name": "LAB-PC-1", "lagT": "link", "descr": "token " + canaryBearer}),
		}}},
		{"vPC domain name", canaryAWSKey, map[string]any{"fabricExplicitGEp": []any{
			apicObj("fabricExplicitGEp", map[string]any{"dn": "uni/fabric/protpol/expgep-x", "name": canaryAWSKey, "id": "10"}),
		}}},
		{"external EPG description", canaryURI, map[string]any{"l3extInstP": []any{
			apicObj("l3extInstP", map[string]any{"dn": "uni/tn-tn_Example/out-l3out_Prod_1/instP-ext", "name": "ext", "descr": canaryURI}),
		}}},
		{"policy prefix description", canaryBearer, map[string]any{
			"l3extInstP":  []any{apicObj("l3extInstP", map[string]any{"dn": "uni/tn-tn_Example/out-l3out_Prod_1/instP-ext", "name": "ext"})},
			"l3extSubnet": []any{apicObj("l3extSubnet", map[string]any{"dn": "uni/tn-tn_Example/out-l3out_Prod_1/instP-ext/extsubnet-[203.0.113.0/24]", "ip": "203.0.113.0/24", "descr": canaryBearer})},
		}},
		{"external neighbour system name", canaryAWSKey, map[string]any{"lldpAdjEp": []any{
			apicObj("lldpAdjEp", map[string]any{"dn": "topology/pod-1/node-111/sys/lldp/inst/if-[eth1/48]/adj-1", "sysName": canaryAWSKey, "chassisIdT": "mac", "chassisIdV": "00:00:5E:00:53:A1"}),
		}}},
	}
	for _, tc := range cases {
		for _, purpose := range []string{"documentation", "audit"} {
			t.Run(tc.name+"/"+purpose, func(t *testing.T) {
				ts := fixtureServerWith(t, nil, tc.override)
				defer ts.Close()

				emitted, err, _ := collectWith(t, ts.URL, ts.Client(), purpose, sdk.Off)
				if err != nil {
					t.Fatalf("Collect with scanning off: %v", err)
				}
				if !bytes.Contains(emitted, []byte(tc.canary)) {
					t.Skipf("%s is not emitted under --purpose %s", tc.name, purpose)
				}

				data, err, logs := collectWith(t, ts.URL, ts.Client(), purpose, sdk.FailClosed)
				if err == nil {
					t.Fatalf("fail-closed Collect succeeded; document leaked=%v", bytes.Contains(data, []byte(tc.canary)))
				}
				if !strings.Contains(err.Error(), "secret scanning") {
					t.Errorf("error is not a secret-scan abort: %v", err)
				}
				if strings.Contains(err.Error(), tc.canary) {
					t.Error("canary leaked into the returned error")
				}
				if strings.Contains(logs, tc.canary) {
					t.Error("canary leaked into the log stream")
				}
			})
		}
	}
}

// TestClient_ErrorBodiesNeverInDiagnostics proves: untrusted APIC
// response content (an HTTP error body, an APIC error-envelope text, a
// malformed body) never reaches a returned error or a log record, for
// login, class queries and logout. The error still carries its stable
// category (HTTP status or APIC error code).
func TestClient_ErrorBodiesNeverInDiagnostics(t *testing.T) {
	errEnvelope := `{"imdata":[{"error":{"attributes":{"code":"403","text":"` + canaryBody + ` ` + canaryBearer + `"}}}]}`
	cases := []struct {
		name   string
		status int
		body   string
		call   func(c *Client) error
		want   string
	}{
		{"login 401", 401, canaryBody, func(c *Client) error { return c.Login("admin", "secret") }, "HTTP 401"},
		{"login 200 error envelope", 200, errEnvelope, func(c *Client) error { return c.Login("admin", "secret") }, "APIC error 403"},
		{"login 200 malformed", 200, "{" + canaryBody, func(c *Client) error { return c.Login("admin", "secret") }, "decode"},
		{"query 403", 403, canaryBody, func(c *Client) error { _, err := c.QueryClass("fvBD"); return err }, "HTTP 403"},
		{"query 429", 429, canaryBody, func(c *Client) error { _, err := c.QueryClass("fvBD"); return err }, "HTTP 429"},
		{"query 500", 500, canaryBody, func(c *Client) error { _, err := c.QueryClass("fvBD"); return err }, "HTTP 500"},
		{"query 200 error envelope", 200, errEnvelope, func(c *Client) error { _, err := c.QueryClass("fvBD"); return err }, "APIC error 403"},
		{"query 200 malformed", 200, `{"imdata":[` + canaryBody, func(c *Client) error { _, err := c.QueryClass("fvBD"); return err }, "decode"},
		{"logout 500", 500, canaryBody, func(c *Client) error { c.token, c.username = "test", "admin"; return c.Logout() }, "HTTP 500"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer ts.Close()

			var logBuf bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
			c := &Client{baseURL: ts.URL, httpClient: ts.Client(), logger: logger, ctx: context.Background(), maxRetries: 1, retryBase: time.Millisecond}

			err := tc.call(c)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not carry category %q", err, tc.want)
			}
			for _, canary := range []string{canaryBody, canaryBearer} {
				if strings.Contains(err.Error(), canary) {
					t.Errorf("canary %q leaked into the returned error: %v", canary, err)
				}
				if strings.Contains(logBuf.String(), canary) {
					t.Errorf("canary %q leaked into the log stream", canary)
				}
			}
		})
	}
}
