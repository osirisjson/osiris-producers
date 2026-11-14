// coverage.go - Discovery-coverage record surfaced in the emitted
// document. Accumulates one outcome per discovery operation (succeeded
// failed / skipped, object count, sanitized failure category), renders
// the extension slice and the metadata.scope.description summary and
// attaches the record to the controller resource(s).
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package apic

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"go.osirisjson.org/producers/pkg/sdk"
)

// opOutcome is one discovery operation's result in the coverage record.
// Count keeps its meaning: objects of the class carried into the
// transform. The other counters are emitted only when non-zero (or, for
// ReportedTotal, only when it disagrees with Received), so a consumer
// can tell query success apart from what actually reached the graph.
type opOutcome struct {
	Operation string `json:"operation"`
	Status    string `json:"status"` // "succeeded" | "failed" | "skipped"
	Count     int    `json:"count"`
	Category  string `json:"category,omitempty"` // sanitized; set only when Status == "failed"

	Received      int // imdata elements received across all pages
	Rejected      int // elements without class/attributes/dn, dropped
	Duplicates    int // repeated dn across pages, dropped
	ReportedTotal int // APIC totalCount, -1 when unknown
	Unresolved    int // observations the graph could not resolve (set after wiring)
	OutOfScope    int // objects under a parent the model does not cover (set after wiring)
}

// coverageRecorder accumulates per-operation outcomes for the run.
type coverageRecorder struct {
	ops []opOutcome
}

func (r *coverageRecorder) succeeded(op string, n int) {
	r.ops = append(r.ops, opOutcome{Operation: op, Status: "succeeded", Count: n, Received: n, ReportedTotal: -1})
}

// succeededQuery records a succeeded class query with its counters.
// kept is the object count after deduplication.
func (r *coverageRecorder) succeededQuery(op string, q classQuery, kept int) {
	r.ops = append(r.ops, opOutcome{
		Operation:     op,
		Status:        "succeeded",
		Count:         kept,
		Received:      q.received,
		Rejected:      q.rejected,
		Duplicates:    len(q.objs) - kept,
		ReportedTotal: q.reportedTotal,
	})
}

// unresolved adds n unresolved observations to op's succeeded outcome.
func (r *coverageRecorder) unresolved(op string, n int) {
	if o := r.succeededOp(op); o != nil && n > 0 {
		o.Unresolved += n
	}
}

// outOfScope adds n out-of-scope objects to op's succeeded outcome.
func (r *coverageRecorder) outOfScope(op string, n int) {
	if o := r.succeededOp(op); o != nil && n > 0 {
		o.OutOfScope += n
	}
}

// succeededOp returns op's succeeded outcome, or nil.
func (r *coverageRecorder) succeededOp(op string) *opOutcome {
	for i := range r.ops {
		if r.ops[i].Operation == op && r.ops[i].Status == "succeeded" {
			return &r.ops[i]
		}
	}
	return nil
}

func (r *coverageRecorder) failed(op, category string) {
	r.ops = append(r.ops, opOutcome{Operation: op, Status: "failed", Category: category})
}

func (r *coverageRecorder) skipped(op string) {
	r.ops = append(r.ops, opOutcome{Operation: op, Status: "skipped"})
}

// tally counts outcomes by status for progress logging.
func (r *coverageRecorder) tally() (ok, failed, skipped int) {
	for _, o := range r.ops {
		switch o.Status {
		case "succeeded":
			ok++
		case "failed":
			failed++
		case "skipped":
			skipped++
		}
	}
	return ok, failed, skipped
}

// asSlice renders the record as the []map[string]any shape carried in
// the resource extension.
func (r *coverageRecorder) asSlice() []map[string]any {
	out := make([]map[string]any, 0, len(r.ops))
	for _, o := range r.ops {
		m := map[string]any{"operation": o.Operation, "status": o.Status, "count": o.Count}
		if o.Category != "" {
			m["category"] = o.Category
		}
		if o.Rejected > 0 {
			m["rejected"] = o.Rejected
		}
		if o.Duplicates > 0 {
			m["duplicates"] = o.Duplicates
		}
		if o.Unresolved > 0 {
			m["unresolved"] = o.Unresolved
		}
		if o.OutOfScope > 0 {
			m["out_of_scope"] = o.OutOfScope
		}
		if o.Status == "succeeded" && o.ReportedTotal >= 0 && o.ReportedTotal != o.Received {
			m["received"] = o.Received
			m["reported_total"] = o.ReportedTotal
		}
		out = append(out, m)
	}
	return out
}

// summary is the one-line prose form for metadata.scope.description. It
// names the fabric domain (when known), the succeeded/total ratio and
// every operation not represented in the document.
func (r *coverageRecorder) summary(fabricDomain string) string {
	var ok int
	var gaps []string
	for _, o := range r.ops {
		switch o.Status {
		case "succeeded":
			ok++
		case "failed":
			gaps = append(gaps, o.Operation+" ("+o.Category+")")
		case "skipped":
			gaps = append(gaps, o.Operation+" (skipped)")
		}
	}

	b := "Cisco ACI fabric"
	if fabricDomain != "" {
		b += " " + fabricDomain
	}
	b += fmt.Sprintf(": %d/%d discovery operations returned data.", ok, len(r.ops))
	if len(gaps) > 0 {
		b += " Not represented: " + strings.Join(gaps, ", ") + "."
	}
	return b
}

// anySucceeded reports whether any of ops succeeded.
func (r *coverageRecorder) anySucceeded(ops ...string) bool {
	for _, o := range r.ops {
		if o.Status != "succeeded" {
			continue
		}
		for _, op := range ops {
			if o.Operation == op {
				return true
			}
		}
	}
	return false
}

// sortedKeys returns the keys of m in sorted order.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// attachControllerExtension sets key in the osiris.cisco extension of
// every controller resource, falling back to the first resource as
// attachCoverage does.
func attachControllerExtension(resources []sdk.Resource, key string, value any) {
	if len(resources) == 0 {
		return
	}
	attached := false
	for i := range resources {
		if role, _ := resources[i].Properties["role"].(string); role != "controller" {
			continue
		}
		ensureCiscoExtension(&resources[i].Extensions)
		resources[i].Extensions[extensionNamespace].(map[string]any)[key] = value
		attached = true
	}
	if !attached {
		ensureCiscoExtension(&resources[0].Extensions)
		resources[0].Extensions[extensionNamespace].(map[string]any)[key] = value
	}
}

// attachCoverage records the coverage slice on every controller
// resource's osiris.cisco extension. If the fabric reported no
// controller (not expected for a real APIC, since fabricNode/topSystem
// are essential and would have aborted), it falls back to the first
// node so the record is never dropped.
func attachCoverage(resources []sdk.Resource, coverage []map[string]any) {
	if len(coverage) == 0 || len(resources) == 0 {
		return
	}
	attached := false
	for i := range resources {
		if role, _ := resources[i].Properties["role"].(string); role != "controller" {
			continue
		}
		ensureCiscoExtension(&resources[i].Extensions)
		resources[i].Extensions[extensionNamespace].(map[string]any)["coverage"] = coverage
		attached = true
	}
	if !attached {
		ensureCiscoExtension(&resources[0].Extensions)
		resources[0].Extensions[extensionNamespace].(map[string]any)["coverage"] = coverage
	}
}

// firstFabricDomain returns the first non-empty topSystem fabricDomain,
// the ACI fabric's own name, for the scope description.
func firstFabricDomain(systems []map[string]any) string {
	for _, s := range systems {
		if v := str(s, "fabricDomain"); v != "" {
			return v
		}
	}
	return ""
}

// sanitizeFailureCategory reduces a discovery error to a stable,
// non-sensitive label for the coverage record. It never returns raw
// error text (which for APIC can include a response-body fragment).
func sanitizeFailureCategory(err error) string {
	if err == nil {
		return ""
	}

	if errors.Is(err, errRejectedObjects) {
		return "rejected-objects"
	}
	if errors.Is(err, errInvalidEnvelope) {
		return "invalid-envelope"
	}
	var apiErr *apicAPIError
	if errors.As(err, &apiErr) {
		switch {
		case strings.HasPrefix(apiErr.code, "4"):
			return "api-client-error"
		case strings.HasPrefix(apiErr.code, "5"):
			return "api-server-error"
		default:
			return "api-error"
		}
	}

	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "cancelled"), strings.Contains(msg, "context canceled"),
		strings.Contains(msg, "deadline exceeded"), strings.Contains(msg, "timeout"):
		return "timeout-or-cancelled"
	case strings.Contains(msg, "http 401"), strings.Contains(msg, "http 403"):
		return "auth"
	case strings.Contains(msg, "http 404"):
		return "not-found"
	case strings.Contains(msg, "http 429"):
		return "rate-limited"
	case strings.Contains(msg, "http 5"):
		return "http-5xx"
	case strings.Contains(msg, "parse"), strings.Contains(msg, "unmarshal"), strings.Contains(msg, "decode"):
		return "decode-error"
	case strings.Contains(msg, "body exceeded"):
		return "response-too-large"
	default:
		return "transport-error"
	}
}
