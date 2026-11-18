// exceptions.go - Reader and evaluator for the committed test data
// policy allowlist (section 7).
//
// Each allowlist entry names one occurrence by exact path, rule and
// fingerprint and carries a technical reason, an owner, a tracking
// reference and a review date. The evaluator rejects malformed,
// generic, broad, duplicate, unsorted, expired and stale entries; an
// invalid entry never suppresses a finding.
//
// OSIRIS JSON Producers Test Data Policy:
// [OSIRIS-PRODUCER-TEST-DATA-POLICY]: https://github.com/osirisjson/osiris-producers/blob/main/docs/policies/OSIRIS-PRODUCER-TEST-DATA-POLICY.md

package testdatapolicy

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
)

// AllowlistFile is the repository-relative path of the allowlist.
const AllowlistFile = "testdata-policy-allowlist.json"

// An Exception is one reviewed allowlist entry.
type Exception struct {
	Path        string `json:"path"`
	Rule        string `json:"rule"`
	Fingerprint string `json:"fingerprint"`
	Reason      string `json:"reason"`
	Owner       string `json:"owner"`
	Issue       string `json:"issue"`
	ReviewBy    string `json:"review_by"`
}

type allowlistDoc struct {
	Exceptions []Exception `json:"exceptions"`
}

var reFingerprint = regexp.MustCompile(`^sha256:[0-9a-f]{32}$`)

// genericReasons lists justifications that explain nothing.
var genericReasons = map[string]bool{
	"needed": true, "required": true, "legacy": true, "todo": true,
	"temporary": true, "temp": true, "ignore": true, "n/a": true,
	"false positive": true, "fix later": true, "test": true,
	"test data": true, "exception": true,
}

// minReasonWords is the minimum number of words of a reason.
const minReasonWords = 4

// parseAllowlist strictly decodes the allowlist document: unknown
// fields and trailing data are rejected.
func parseAllowlist(data []byte) ([]Exception, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var doc allowlistDoc
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("decode: trailing data after the allowlist object")
	}
	if doc.Exceptions == nil {
		return nil, errors.New(`decode: missing "exceptions" array`)
	}
	return doc.Exceptions, nil
}

// applyExceptions splits findings into violations and excepted
// occurrences and returns the allowlist problems. tracked holds every
// tracked path; today is the date used for review_by checks.
func applyExceptions(entries []Exception, findings []Finding, tracked map[string]bool, today time.Time) (violations, excepted, problems []Finding, stale int) {
	problem := func(i int, e Exception, msg string) {
		problems = append(problems, Finding{
			Path:    AllowlistFile,
			Rule:    RuleException,
			Message: fmt.Sprintf("entry %d (%s %s): %s", i+1, e.Path, e.Rule, msg),
		})
	}
	todayDate := today.UTC().Format(time.DateOnly)

	valid := map[string]int{} // path|rule|fingerprint -> entry index
	for i, e := range entries {
		if i > 0 && compareExceptions(entries[i-1], e) > 0 {
			problem(i, e, "entries are not sorted by path, rule and fingerprint")
		}
		if msg := validateException(e, tracked, todayDate); msg != "" {
			problem(i, e, msg)
			if msg == "path is not a tracked file" {
				stale++
			}
			continue
		}
		key := e.Path + "\x00" + e.Rule + "\x00" + e.Fingerprint
		if j, dup := valid[key]; dup {
			problem(i, e, fmt.Sprintf("duplicates entry %d", j+1))
			continue
		}
		valid[key] = i
	}

	used := map[string]bool{}
	for _, f := range findings {
		key := f.Path + "\x00" + f.Rule + "\x00" + f.Fingerprint
		if _, ok := valid[key]; ok && f.Fingerprint != "" {
			excepted = append(excepted, f)
			used[key] = true
			continue
		}
		violations = append(violations, f)
	}
	for key, i := range valid {
		if !used[key] {
			problem(i, entries[i], "stale: matches no current finding")
			stale++
		}
	}
	sortFindings(problems)
	return violations, excepted, problems, stale
}

func compareExceptions(a, b Exception) int {
	return cmp.Or(
		cmp.Compare(a.Path, b.Path),
		cmp.Compare(a.Rule, b.Rule),
		cmp.Compare(a.Fingerprint, b.Fingerprint),
	)
}

// validateException returns a problem description, or "" when the
// entry is well formed and current.
func validateException(e Exception, tracked map[string]bool, today string) string {
	switch {
	case e.Path == "":
		return "missing path"
	case strings.ContainsAny(e.Path, "*?[") || strings.HasSuffix(e.Path, "/"):
		return "path must name one file, not a glob or directory"
	case !exceptableRules[e.Rule]:
		return "unknown or non-exceptable rule"
	case e.Fingerprint == "":
		return "missing fingerprint: whole-file exceptions are not allowed"
	case !reFingerprint.MatchString(e.Fingerprint):
		return "fingerprint must be sha256: followed by 32 lowercase hex digits"
	case isGenericReason(e.Reason):
		return "reason is empty or generic"
	case strings.TrimSpace(e.Owner) == "":
		return "missing owner"
	case strings.TrimSpace(e.Issue) == "":
		return "missing issue"
	}
	if _, err := time.Parse(time.DateOnly, e.ReviewBy); err != nil {
		return "review_by must be a YYYY-MM-DD date"
	}
	if e.ReviewBy < today {
		return "review_by " + e.ReviewBy + " has passed"
	}
	if !tracked[e.Path] {
		for p := range tracked {
			if strings.HasPrefix(p, e.Path+"/") {
				return "path must name one file, not a glob or directory"
			}
		}
		return "path is not a tracked file"
	}
	return ""
}

func isGenericReason(r string) bool {
	r = strings.ToLower(strings.TrimSpace(r))
	return genericReasons[r] || len(strings.Fields(r)) < minReasonWords
}
