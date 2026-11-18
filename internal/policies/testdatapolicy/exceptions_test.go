// exceptions_test.go - Tests for the allowlist contract: one valid,
// narrowly scoped exception and rejection of malformed, stale,
// expired, duplicate, unsorted, broad, unexplained and mismatched
// entries. An invalid entry must never suppress its finding.

package testdatapolicy

import (
	"encoding/json"
	"strings"
	"testing"
)

// fixture returns a source with one violating line in a.md and the
// fingerprint of that violation.
func fixture(t *testing.T) (*memSource, string) {
	t.Helper()
	m := newMem("a.md", "link "+v6("fe80", "", "1")+"\n", "dir/b.md", "ok\n")
	rep := scanMem(t, m)
	if len(rep.Violations) != 1 {
		t.Fatalf("fixture violations %v, want 1", rep.Violations)
	}
	return m, rep.Violations[0].Fingerprint
}

func validEntry(fp string) Exception {
	return Exception{
		Path:        "a.md",
		Rule:        RuleIPv6,
		Fingerprint: fp,
		Reason:      "Verifies that link-local prefixes are skipped",
		Owner:       "network-producers",
		Issue:       "TDP-1",
		ReviewBy:    "2027-05-15",
	}
}

// withAllowlist adds an allowlist file built from entries to m.
func withAllowlist(t *testing.T, m *memSource, entries ...Exception) *memSource {
	t.Helper()
	data, err := json.Marshal(allowlistDoc{Exceptions: entries})
	if err != nil {
		t.Fatal(err)
	}
	return withAllowlistBytes(m, data)
}

func withAllowlistBytes(m *memSource, data []byte) *memSource {
	c := &memSource{files: map[string][]byte{}}
	for k, v := range m.files {
		c.files[k] = v
	}
	c.entries = append(append([]IndexEntry{}, m.entries...), IndexEntry{Mode: modeRegular, Path: AllowlistFile})
	c.files[AllowlistFile] = data
	return c
}

func TestValidException(t *testing.T) {
	m, fp := fixture(t)
	rep := scanMem(t, withAllowlist(t, m, validEntry(fp)))
	if len(rep.Violations) != 0 || len(rep.ExceptionProblems) != 0 {
		t.Fatalf("violations %v problems %v, want none", rep.Violations, rep.ExceptionProblems)
	}
	if len(rep.Excepted) != 1 || rep.Failed() {
		t.Errorf("excepted %v failed %v", rep.Excepted, rep.Failed())
	}
	if !strings.Contains(rep.Summary(), "Approved exceptions: 1") {
		t.Errorf("summary:\n%s", rep.Summary())
	}
}

func TestEmptyAllowlist(t *testing.T) {
	rep := scanMem(t, withAllowlistBytes(newMem("a.md", "ok\n"), []byte(`{"exceptions": []}`)))
	if rep.Failed() {
		t.Errorf("empty allowlist failed: %v", rep.ExceptionProblems)
	}
}

func TestInvalidExceptions(t *testing.T) {
	m, fp := fixture(t)
	otherFP := "sha256:" + strings.Repeat("0", 32)
	mod := func(f func(*Exception)) Exception {
		e := validEntry(fp)
		f(&e)
		return e
	}
	tests := []struct {
		name     string
		entries  []Exception
		problem  string
		stale    int
		suppress bool // whether the finding is still excepted
	}{
		{"missing reason", []Exception{mod(func(e *Exception) { e.Reason = "" })}, "reason is empty or generic", 0, false},
		{"generic reason", []Exception{mod(func(e *Exception) { e.Reason = "legacy" })}, "reason is empty or generic", 0, false},
		{"short reason", []Exception{mod(func(e *Exception) { e.Reason = "needed for test" })}, "reason is empty or generic", 0, false},
		{"missing owner", []Exception{mod(func(e *Exception) { e.Owner = " " })}, "missing owner", 0, false},
		{"missing issue", []Exception{mod(func(e *Exception) { e.Issue = "" })}, "missing issue", 0, false},
		{"expired", []Exception{mod(func(e *Exception) { e.ReviewBy = "2026-11-14" })}, "has passed", 0, false},
		{"bad date", []Exception{mod(func(e *Exception) { e.ReviewBy = "15/11/2027" })}, "YYYY-MM-DD", 0, false},
		{"unknown rule", []Exception{mod(func(e *Exception) { e.Rule = "OSIRIS-PR-TDP-999" })}, "unknown or non-exceptable rule", 0, false},
		{"exception rule", []Exception{mod(func(e *Exception) { e.Rule = RuleException })}, "unknown or non-exceptable rule", 0, false},
		{"whole file", []Exception{mod(func(e *Exception) { e.Fingerprint = "" })}, "whole-file", 0, false},
		{"bad fingerprint", []Exception{mod(func(e *Exception) { e.Fingerprint = "md5:abc" })}, "fingerprint must be", 0, false},
		{"glob", []Exception{mod(func(e *Exception) { e.Path = "*.md" })}, "not a glob or directory", 0, false},
		{"directory", []Exception{mod(func(e *Exception) { e.Path = "dir" })}, "not a glob or directory", 0, false},
		{"trailing slash", []Exception{mod(func(e *Exception) { e.Path = "dir/" })}, "not a glob or directory", 0, false},
		{"missing path", []Exception{mod(func(e *Exception) { e.Path = "gone.md" })}, "not a tracked file", 1, false},
		{"empty path", []Exception{mod(func(e *Exception) { e.Path = "" })}, "missing path", 0, false},
		{"path mismatch", []Exception{mod(func(e *Exception) { e.Path = "dir/b.md" })}, "stale", 1, false},
		{"rule mismatch", []Exception{mod(func(e *Exception) { e.Rule = RuleIPv4 })}, "stale", 1, false},
		{"fingerprint mismatch", []Exception{mod(func(e *Exception) { e.Fingerprint = otherFP })}, "stale", 1, false},
		{"duplicate", []Exception{validEntry(fp), validEntry(fp)}, "duplicates entry 1", 0, true},
		{"unsorted", []Exception{validEntry(fp), mod(func(e *Exception) { e.Path = "A.md"; e.Fingerprint = otherFP })}, "not sorted", 0, true},
	}
	for _, tt := range tests {
		rep := scanMem(t, withAllowlist(t, m, tt.entries...))
		if !rep.Failed() {
			t.Errorf("%s: report did not fail", tt.name)
		}
		found := false
		for _, p := range rep.ExceptionProblems {
			if p.Rule != RuleException || p.Path != AllowlistFile {
				t.Errorf("%s: malformed problem %v", tt.name, p)
			}
			if strings.Contains(p.Message, tt.problem) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: problems %v, want one containing %q", tt.name, rep.ExceptionProblems, tt.problem)
		}
		if rep.Stale < tt.stale {
			t.Errorf("%s: stale = %d, want >= %d", tt.name, rep.Stale, tt.stale)
		}
		if suppressed := len(rep.Excepted) == 1; suppressed != tt.suppress {
			t.Errorf("%s: finding suppressed = %v, want %v", tt.name, suppressed, tt.suppress)
		}
	}
}

func TestMalformedAllowlist(t *testing.T) {
	m, fp := fixture(t)
	good, _ := json.Marshal(allowlistDoc{Exceptions: []Exception{validEntry(fp)}})
	for name, data := range map[string]string{
		"not json":      "{",
		"unknown field": strings.Replace(string(good), `"path"`, `"ignore":true,"path"`, 1),
		"trailing data": string(good) + "{}",
		"no array":      `{}`,
		"wrong shape":   `[]`,
	} {
		rep := scanMem(t, withAllowlistBytes(m, []byte(data)))
		if len(rep.ExceptionProblems) == 0 || len(rep.Violations) != 1 {
			t.Errorf("%s: problems %v violations %v, want a problem and the unsuppressed finding", name, rep.ExceptionProblems, rep.Violations)
		}
	}
}

func TestBinaryException(t *testing.T) {
	m := newMem("logo.gif", "GIF89a\x00")
	rep := scanMem(t, m)
	if len(rep.Violations) != 1 {
		t.Fatalf("violations %v", rep.Violations)
	}
	e := Exception{
		Path: "logo.gif", Rule: RuleInventory, Fingerprint: rep.Violations[0].Fingerprint,
		Reason: "Project logo reviewed for embedded identifiers", Owner: "maintainers",
		Issue: "TDP-2", ReviewBy: "2027-11-15",
	}
	if rep := scanMem(t, withAllowlist(t, m, e)); rep.Failed() {
		t.Errorf("reviewed binary still fails: %v %v", rep.Violations, rep.ExceptionProblems)
	}
	// Changing the binary invalidates its review.
	m.files["logo.gif"] = []byte("GIF89a\x00\x01")
	if rep := scanMem(t, withAllowlist(t, m, e)); !rep.Failed() || rep.Stale != 1 {
		t.Errorf("changed binary passed: stale=%d", rep.Stale)
	}
}
