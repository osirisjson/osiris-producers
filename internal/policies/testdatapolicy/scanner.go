// scanner.go - Orchestration of one test data policy run, in order:
// Git inventory, text/binary classification, address rules (IPv4,
// IPv6, MAC; section 3), context rules (hostnames, devices,
// sites, ASNs, serials, credentials; section 4), hyperscaler identity
// rules (section 5), occurrence fingerprints, allowlist matching and a
// deterministic report.
//
// OSIRIS JSON Producers Test Data Policy:
// [OSIRIS-PRODUCER-TEST-DATA-POLICY]: https://github.com/osirisjson/osiris-producers/blob/main/docs/policies/OSIRIS-PRODUCER-TEST-DATA-POLICY.md

package testdatapolicy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Options configures a policy run.
type Options struct {
	// Today is the date review_by values are compared against.
	Today time.Time
}

// Scan runs the policy over every entry of src. It returns an error
// only when the inventory or a tracked file cannot be read; policy
// violations are reported in the [Report].
func Scan(ctx context.Context, src Source, opt Options) (*Report, error) {
	entries, err := src.Entries(ctx)
	if err != nil {
		return nil, fmt.Errorf("inventory: %w", err)
	}
	rep := &Report{Files: len(entries)}
	tracked := make(map[string]bool, len(entries))
	var findings []Finding
	var allowlist []byte
	haveAllowlist := false

	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		tracked[e.Path] = true
		switch e.Mode {
		case modeSymlink:
			rep.OtherFiles++
			findings = append(findings, Finding{Path: e.Path, Rule: RuleInventory, Message: "symlinks are not permitted in the repository"})
			continue
		case modeSubmodule:
			rep.OtherFiles++
			findings = append(findings, Finding{Path: e.Path, Rule: RuleInventory, Message: "submodules are not permitted in the repository"})
			continue
		case modeRegular, modeExecutable:
		default:
			rep.OtherFiles++
			findings = append(findings, Finding{Path: e.Path, Rule: RuleInventory, Message: "unsupported Git object mode " + e.Mode})
			continue
		}
		data, err := src.ReadFile(e.Path)
		if err != nil {
			return nil, fmt.Errorf("inventory: %w", err)
		}
		if e.Path == AllowlistFile {
			allowlist, haveAllowlist = data, true
		}
		if !isText(data) {
			rep.OtherFiles++
			sum := sha256.Sum256(data)
			findings = append(findings, Finding{
				Path:      e.Path,
				Rule:      RuleInventory,
				Message:   "binary file must be reviewed and listed in the allowlist",
				canonical: hex.EncodeToString(sum[:]),
			})
			continue
		}
		rep.TextFiles++
		fs, n := scanText(e.Path, data)
		rep.Candidates += n
		findings = append(findings, fs...)
	}

	assignFingerprints(findings)
	sortFindings(findings)

	if d, ok := src.(interface{ InventoryMode() string }); ok {
		rep.Inventory = d.InventoryMode()
	}
	var entriesList []Exception
	switch {
	case haveAllowlist:
		entriesList, err = parseAllowlist(allowlist)
		if err != nil {
			rep.Allowlist = AllowlistInvalid
			rep.ExceptionProblems = append(rep.ExceptionProblems, Finding{
				Path:    AllowlistFile,
				Rule:    RuleException,
				Message: err.Error(),
			})
		} else {
			rep.Allowlist = AllowlistLoaded
			rep.AllowlistEntries = len(entriesList)
		}
	default:
		// An allowlist outside the inventory is never applied; it is
		// only reported, so the summary can explain the violations.
		if _, err := src.ReadFile(AllowlistFile); err == nil {
			rep.Allowlist = AllowlistUnused
		}
	}
	var problems []Finding
	rep.Violations, rep.Excepted, problems, rep.Stale = applyExceptions(entriesList, findings, tracked, opt.Today)
	rep.ExceptionProblems = append(rep.ExceptionProblems, problems...)
	return rep, nil
}

// isText reports whether data is valid UTF-8 without NUL bytes.
func isText(data []byte) bool {
	return utf8.Valid(data) && bytes.IndexByte(data, 0) < 0
}

// scanText evaluates every line of a text file and returns the
// findings, deduplicated, and the number of candidates inspected.
func scanText(path string, data []byte) ([]Finding, int) {
	testFile := strings.HasSuffix(path, "_test.go")
	var out []Finding
	n := 0
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSuffix(line, "\r")
		trimmed := strings.TrimSpace(line)
		for _, c := range scanAddresses(line, testFile) {
			n++
			if c.res.allowed {
				continue
			}
			out = append(out, Finding{
				Path:      path,
				Line:      i + 1,
				Col:       c.col,
				Rule:      c.rule,
				Message:   addressMessage(c),
				canonical: c.res.canonical,
				lineText:  trimmed,
			})
		}
		cfs, cn := scanContext(path, line)
		n += cn
		for _, c := range cfs {
			out = append(out, Finding{
				Path:      path,
				Line:      i + 1,
				Col:       c.col,
				Rule:      c.rule,
				Message:   c.message,
				canonical: c.canonical,
				lineText:  trimmed,
			})
		}
	}
	sortFindings(out)
	return dedupeFindings(out), n
}

func addressMessage(c addrCandidate) string {
	kind := map[string]string{RuleIPv4: "IPv4", RuleIPv6: "IPv6", RuleMAC: "MAC"}[c.rule]
	return fmt.Sprintf("%s %s is %s", kind, display(c.raw), article(c.res.class))
}

func article(class string) string {
	switch class[0] {
	case 'a', 'e', 'i', 'o', 'u', 'I':
		return "an " + class
	}
	return "a " + class
}
