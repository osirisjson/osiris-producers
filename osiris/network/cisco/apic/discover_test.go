// discover_test.go - Tests for the APIC discovery plan, per-domain
// failure policy, failure-category sanitization, and the coverage
// record.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package apic

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// TestDiscoveryPlan_FailurePolicy pins each class's abort-vs-degrade
// policy so a criticality change is a deliberate, reviewed edit.
func TestDiscoveryPlan_FailurePolicy(t *testing.T) {
	want := map[string]bool{ // class -> aborts the run on failure
		"fabricNode":      true,
		"topSystem":       true,
		"fvTenant":        true,
		"fvCtx":           true,
		"fvBD":            true,
		"fvSubnet":        true,
		"fvAEPg":          true,
		"l3extOut":        true,
		"firmwareRunning": false,
		"fvRsCtx":         false,
		"fvRsBd":          false,
		"l3extRsEctx":     false,
		"faultInst":       false,
		"fvCEp":           false,
		"fvIp":            false,
	}

	got := map[string]bool{}
	for _, dc := range discoveryPlan(true) { // audit: includes fvCEp
		got[dc.name] = dc.crit.aborts()
	}

	if len(got) != len(want) {
		t.Fatalf("plan has %d classes, expected %d: %v", len(got), len(want), got)
	}
	for name, wantAbort := range want {
		if got[name] != wantAbort {
			t.Errorf("%s: aborts=%v, want %v", name, got[name], wantAbort)
		}
	}
}

func TestDiscoveryPlan_EndpointClassesAreAuditOnly(t *testing.T) {
	auditOnly := map[string]bool{"fvCEp": true, "fvIp": true}

	for _, dc := range discoveryPlan(false) {
		if auditOnly[dc.name] {
			t.Errorf("%s must not be in the documentation-purpose plan", dc.name)
		}
	}
	seen := map[string]bool{}
	for _, dc := range discoveryPlan(true) {
		if auditOnly[dc.name] {
			seen[dc.name] = true
		}
	}
	for name := range auditOnly {
		if !seen[name] {
			t.Errorf("%s must be in the audit-purpose plan", name)
		}
	}
}

func TestSanitizeFailureCategory(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, ""},
		{"api client error", &apicAPIError{code: "400", text: "bad"}, "api-client-error"},
		{"api server error", &apicAPIError{code: "599", text: "x"}, "api-server-error"},
		{"api error no code", &apicAPIError{code: "", text: "x"}, "api-error"},
		{"http 5xx", fmt.Errorf("APIC query fvBD: gave up after 4 attempts: HTTP 503: x"), "http-5xx"},
		{"http 401", fmt.Errorf("HTTP 401: forbidden"), "auth"},
		{"http 404", fmt.Errorf("HTTP 404: not found"), "not-found"},
		{"http 429", fmt.Errorf("HTTP 429: slow down"), "rate-limited"},
		{"context deadline", fmt.Errorf("request cancelled: %w", context.DeadlineExceeded), "timeout-or-cancelled"},
		{"decode", fmt.Errorf("decode envelope: unexpected end of JSON input"), "decode-error"},
		{"body limit", fmt.Errorf("response body exceeded 256-byte limit"), "response-too-large"},
		{"unknown", errors.New("something else entirely"), "transport-error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeFailureCategory(tt.err); got != tt.want {
				t.Errorf("sanitizeFailureCategory(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}

func TestCoverageRecorder(t *testing.T) {
	r := &coverageRecorder{}
	r.succeeded("fabricNode", 41)
	r.succeeded("fvBD", 288)
	r.failed("faultInst", "http-5xx")
	r.skipped("fvCEp")

	slice := r.asSlice()
	if len(slice) != 4 {
		t.Fatalf("asSlice len = %d, want 4", len(slice))
	}
	if slice[0]["operation"] != "fabricNode" || slice[0]["status"] != "succeeded" || slice[0]["count"] != 41 {
		t.Errorf("entry 0 = %v", slice[0])
	}
	if _, hasCat := slice[0]["category"]; hasCat {
		t.Error("succeeded entry must not carry a category")
	}
	if slice[2]["category"] != "http-5xx" {
		t.Errorf("failed entry category = %v", slice[2]["category"])
	}

	summary := r.summary("MXP")
	if !strings.Contains(summary, "MXP") {
		t.Errorf("summary missing fabric domain: %q", summary)
	}
	if !strings.Contains(summary, "2/4") {
		t.Errorf("summary missing succeeded ratio: %q", summary)
	}
	if !strings.Contains(summary, "faultInst (http-5xx)") || !strings.Contains(summary, "fvCEp (skipped)") {
		t.Errorf("summary missing gaps: %q", summary)
	}
}

func TestCoverageRecorder_SummaryNoGaps(t *testing.T) {
	r := &coverageRecorder{}
	r.succeeded("fabricNode", 3)
	r.succeeded("topSystem", 3)
	s := r.summary("")
	if strings.Contains(s, "Not represented") {
		t.Errorf("clean run should not list gaps: %q", s)
	}
	if !strings.Contains(s, "2/2") {
		t.Errorf("summary: %q", s)
	}
}
