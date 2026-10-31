// coverage_test.go - Unit tests for the discovery-coverage record
// (coverage.go): outcome classification, sanitized failure categories,
// the extension slice and the metadata.scope.description summary.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package iosxe

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

// timeoutErr is a net.Error that reports a timeout.
type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func TestCoverageRecord(t *testing.T) {
	cases := []struct {
		name         string
		count        int
		err          error
		wantStatus   string
		wantCount    int
		wantCategory string
	}{
		{"rows returned", 3, nil, statusSucceeded, 3, ""},
		{"valid reply, no rows", 0, nil, statusEmpty, 0, ""},
		{"empty data", 0, errEmptyModel, statusEmpty, 0, ""},
		{"model not implemented", 0, fmt.Errorf("wrap: %w", &rpcError{Tag: "unknown-namespace"}), statusUnsupported, 0, ""},
		{"operation not supported", 0, &rpcError{Tag: "operation-not-supported"}, statusUnsupported, 0, ""},
		{"access denied", 0, &rpcError{Tag: "access-denied"}, statusFailed, 0, "access-denied"},
		{"other rpc-error", 0, &rpcError{Tag: "invalid-value"}, statusFailed, 0, "rpc-error"},
		{"decode failure", 5, fmt.Errorf("%w: bad", errDecode), statusFailed, 0, "decode-error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var r coverageRecorder
			o := r.record("op", tc.count, tc.err)
			if o.Status != tc.wantStatus || o.Count != tc.wantCount || o.Category != tc.wantCategory {
				t.Errorf("got %+v, want status=%s count=%d category=%q", o, tc.wantStatus, tc.wantCount, tc.wantCategory)
			}
			if len(r.ops) != 1 {
				t.Errorf("recorded %d outcomes, want 1", len(r.ops))
			}
		})
	}
}

func TestFailureCategory(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{nil, ""},
		{fmt.Errorf("%w: earlier", errSessionUnusable), "session-unusable"},
		{fmt.Errorf("%w: stream ended", os.ErrDeadlineExceeded), "timeout"},
		{fmt.Errorf("read: %w", timeoutErr{}), "timeout"},
		{fmt.Errorf("x: %w", errReplyTooLarge), "reply-too-large"},
		{fmt.Errorf("x: %w", errMalformedFrame), "malformed-frame"},
		{fmt.Errorf("x: %w", errMessageIDMismatch), "message-id-mismatch"},
		{fmt.Errorf("x: %w", errInvalidReply), "invalid-reply"},
		{fmt.Errorf("x: %w", errDecode), "decode-error"},
		{errors.New("dial tcp 192.0.2.1:830: connection refused"), "transport-error"},
	}
	for _, tc := range cases {
		if got := failureCategory(tc.err); got != tc.want {
			t.Errorf("failureCategory(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

func TestFailureCategoryNeverCarriesErrorText(t *testing.T) {
	err := errors.New("password changeme rejected by 192.0.2.1")
	var r coverageRecorder
	r.record("op", 0, err)
	for _, m := range r.asSlice() {
		for k, v := range m {
			if s, ok := v.(string); ok && (strings.Contains(s, "changeme") || strings.Contains(s, "192.0.2.1")) {
				t.Errorf("coverage field %s leaks error text: %q", k, s)
			}
		}
	}
	if s := r.summary("LAB-RTR01"); strings.Contains(s, "changeme") || strings.Contains(s, "192.0.2.1") {
		t.Errorf("summary leaks error text: %q", s)
	}
}

func TestCoverageAsSliceAndSummary(t *testing.T) {
	var r coverageRecorder
	r.record("native-system", 1, nil)
	r.record("interfaces", 4, nil)
	r.record("hardware-inventory", 0, &rpcError{Tag: "unknown-namespace"})
	r.record("cdp-neighbors", 0, errEmptyModel)
	r.record("vrf-definitions", 0, fmt.Errorf("%w: x", errDecode))
	r.skipped("bgp", "audit purpose only")

	got := r.asSlice()
	want := []map[string]any{
		{"operation": "native-system", "status": "succeeded", "count": 1},
		{"operation": "interfaces", "status": "succeeded", "count": 4},
		{"operation": "hardware-inventory", "status": "unsupported", "count": 0},
		{"operation": "cdp-neighbors", "status": "empty", "count": 0},
		{"operation": "vrf-definitions", "status": "failed", "count": 0, "category": "decode-error"},
		{"operation": "bgp", "status": "skipped", "count": 0, "category": "audit purpose only"},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("asSlice:\n got %v\nwant %v", got, want)
	}

	wantSummary := "Cisco IOS-XE device LAB-RTR01: 2/6 discovery operations returned data." +
		" No instances: cdp-neighbors." +
		" Not represented: hardware-inventory (unsupported), vrf-definitions (failed: decode-error), bgp (skipped: audit purpose only)."
	if s := r.summary("LAB-RTR01"); s != wantSummary {
		t.Errorf("summary:\n got %q\nwant %q", s, wantSummary)
	}

	var all coverageRecorder
	all.record("native-system", 1, nil)
	if s := all.summary(""); s != "Cisco IOS-XE device: 1/1 discovery operations returned data." {
		t.Errorf("complete summary = %q", s)
	}
}
