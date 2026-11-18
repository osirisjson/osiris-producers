// diagnostics.go - Findings, occurrence fingerprints (section 7.2),
// deterministic ordering and the run summary (section 6) of the test
// data policy scanner.
//
// A finding never carries a credential value or a long fixture
// fragment: values are shown truncated and credential findings omit
// the value entirely. Only the fingerprint, a truncated SHA-256 hash,
// is derived from the raw value.
//
// OSIRIS JSON Producers Test Data Policy:
// [OSIRIS-PRODUCER-TEST-DATA-POLICY]: https://github.com/osirisjson/osiris-producers/blob/main/docs/policies/OSIRIS-PRODUCER-TEST-DATA-POLICY.md

package testdatapolicy

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// maxDisplay bounds how many bytes of a value a message shows.
const maxDisplay = 64

// A Finding is one policy violation, one excepted occurrence or one
// allowlist problem.
type Finding struct {
	Path    string // repository-relative path
	Line    int    // 1-based line, 0 when not applicable
	Col     int    // 1-based byte column, 0 when not applicable
	Rule    string // rule identifier, e.g. OSIRIS-PR-TDP-001
	Message string // safe explanation, never a credential value

	// Fingerprint identifies the occurrence for an allowlist entry.
	// It is empty when the finding cannot be excepted.
	Fingerprint string

	canonical string // normalized value, hashed only
	lineText  string // trimmed line, hashed only
}

// String renders the finding as
// path:line:col: rule: message; use remedy [fingerprint].
func (f Finding) String() string {
	var b strings.Builder
	b.WriteString(f.Path)
	if f.Line > 0 {
		fmt.Fprintf(&b, ":%d:%d", f.Line, f.Col)
	}
	fmt.Fprintf(&b, ": %s: %s", f.Rule, f.Message)
	if r := remedies[f.Rule]; r != "" {
		b.WriteString("; use ")
		b.WriteString(r)
	}
	if f.Fingerprint != "" {
		fmt.Fprintf(&b, " [%s]", f.Fingerprint)
	}
	return b.String()
}

// fingerprint hashes the occurrence identity (policy section 7.2,
// fingerprint contract revision 1). The line number is not
// part of it, so moving a line keeps an allowlist entry valid while
// editing the line invalidates it.
func fingerprint(rule, path, canonical, lineText string, ordinal int) string {
	h := sha256.New()
	for _, s := range []string{rule, path, canonical, lineText, strconv.Itoa(ordinal)} {
		h.Write([]byte(s))
		h.Write([]byte{0})
	}
	sum := h.Sum(nil)
	return "sha256:" + hex.EncodeToString(sum[:16])
}

// assignFingerprints numbers identical occurrences in file order and
// sets each finding's fingerprint. Findings with an empty canonical
// value are left without a fingerprint.
func assignFingerprints(fs []Finding) {
	seen := map[string]int{}
	for i := range fs {
		f := &fs[i]
		if f.canonical == "" {
			continue
		}
		key := strings.Join([]string{f.Rule, f.Path, f.canonical, f.lineText}, "\x00")
		f.Fingerprint = fingerprint(f.Rule, f.Path, f.canonical, f.lineText, seen[key])
		seen[key]++
	}
}

// sortFindings orders findings by path, line, column and rule.
func sortFindings(fs []Finding) {
	slices.SortStableFunc(fs, func(a, b Finding) int {
		return cmp.Or(
			cmp.Compare(a.Path, b.Path),
			cmp.Compare(a.Line, b.Line),
			cmp.Compare(a.Col, b.Col),
			cmp.Compare(a.Rule, b.Rule),
		)
	})
}

// dedupeFindings drops repeated reports of the same value under the
// same rule on the same line, keeping the first.
func dedupeFindings(fs []Finding) []Finding {
	seen := map[string]bool{}
	out := fs[:0]
	for _, f := range fs {
		key := fmt.Sprintf("%s\x00%d\x00%s\x00%s", f.Path, f.Line, f.Rule, f.canonical)
		if f.Line > 0 && seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, f)
	}
	return out
}

// display quotes a value for a message, truncated to maxDisplay bytes.
func display(v string) string {
	if len(v) > maxDisplay {
		v = v[:maxDisplay] + "..."
	}
	return strconv.Quote(v)
}

// AllowlistState records whether the allowlist took part in a run.
type AllowlistState int

const (
	// AllowlistAbsent means no allowlist file exists.
	AllowlistAbsent AllowlistState = iota
	// AllowlistLoaded means the allowlist was in the inventory and
	// its entries were applied.
	AllowlistLoaded
	// AllowlistInvalid means the allowlist was in the inventory but
	// could not be decoded; no entry was applied.
	AllowlistInvalid
	// AllowlistUnused means the allowlist exists in the working tree
	// but not in the inventory, typically because it is not tracked
	// by Git yet. No entry was applied.
	AllowlistUnused
)

// A Report is the result of one policy run.
type Report struct {
	// Inventory describes what was inventoried, for example tracked
	// files only. Empty when the source does not say.
	Inventory string
	// Allowlist and AllowlistEntries describe the allowlist used.
	Allowlist        AllowlistState
	AllowlistEntries int

	Files      int // tracked index entries
	TextFiles  int // entries scanned as text
	OtherFiles int // binaries, symlinks and submodules accounted for
	Candidates int // literals and context values inspected

	Violations        []Finding // unsuppressed findings
	Excepted          []Finding // findings matched by an allowlist entry
	ExceptionProblems []Finding // invalid, stale or expired entries
	Stale             int       // entries that match no finding
}

// Failed reports whether the run must fail.
func (r *Report) Failed() bool {
	return len(r.Violations) > 0 || len(r.ExceptionProblems) > 0
}

// Summary renders the deterministic run summary. When the allowlist
// exists but was not used, it ends with a note explaining why
// reviewed occurrences are reported as violations.
func (r *Report) Summary() string {
	result := "PASS"
	if r.Failed() {
		result = "FAIL"
	}
	var b strings.Builder
	b.WriteString("Test Data Policy\n")
	if r.Inventory != "" {
		fmt.Fprintf(&b, "Inventory: %s\n", r.Inventory)
	}
	fmt.Fprintf(&b, "Allowlist: %s\n", r.allowlistStatus())
	fmt.Fprintf(&b, `Files inventoried: %d
Text files scanned: %d
Binary/unsupported files accounted for: %d
Candidates inspected: %d
Violations: %d
Approved exceptions: %d
Stale exceptions: %d
Result: %s
`, r.Files, r.TextFiles, r.OtherFiles, r.Candidates, len(r.Violations), len(r.Excepted), r.Stale, result)
	if r.Allowlist == AllowlistUnused {
		fmt.Fprintf(&b, `
Note: %s is not tracked by Git, so its exceptions were not applied and
the occurrences it covers are reported above as violations.

Stage it with:
  git add %s
or use it locally before staging with TDP_INCLUDE_UNTRACKED=1.
`, AllowlistFile, AllowlistFile)
	}
	return b.String()
}

// allowlistStatus renders the Allowlist summary line.
func (r *Report) allowlistStatus() string {
	switch r.Allowlist {
	case AllowlistLoaded:
		return fmt.Sprintf("%s, %d entries applied", AllowlistFile, r.AllowlistEntries)
	case AllowlistInvalid:
		return AllowlistFile + ", invalid, no entries applied"
	case AllowlistUnused:
		return AllowlistFile + ", NOT USED (present but not tracked by Git)"
	}
	return "none"
}
