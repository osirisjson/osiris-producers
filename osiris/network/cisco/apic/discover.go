// discover.go - APIC discovery plan, per-domain failure policy, and the
// discovery-coverage record surfaced in the emitted document.
//
// Each APIC class query has a declared criticality. An essential or
// structural failure aborts the whole document (a fabric snapshot with
// no nodes, or an ACI tenant model with no tenants, would misrepresent
// the fabric). An optional failure degrades to partial output and is
// recorded so a consumer can tell "genuinely absent" from "not
// collected this run". The record mirrors the NX-OS producer's
// per-command coverage shape: one entry per operation with operation,
// status, object count, and, on failure, a sanitized category.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package apic

import (
	"errors"
	"fmt"
	"strings"

	"go.osirisjson.org/producers/pkg/sdk"
)

// criticality declares what a failed discovery operation
// does to the run.
type criticality int

const (
	// critEssential is fabric identity and node inventory. A failure
	// aborts: the document cannot describe a fabric it could not read.
	critEssential criticality = iota

	// critStructural is the tenant/logical object model (tenants, VRFs,
	// bridge domains, subnets, EPGs, L3Outs). A failure aborts: a
	// document claiming to model an ACI fabric
	// with an empty tenant set because one class returned HTTP 500
	// would be misleading.
	critStructural

	// critOptional is enrichment and relationship wiring (firmware,
	// faults, the fvRs* relationship classes, endpoint inventory). A
	// failure leaves the document partial and adds a coverage entry.
	critOptional
)

// aborts reports whether a failure of this criticality stops the run.
func (c criticality) aborts() bool { return c != critOptional }

func (c criticality) String() string {
	switch c {
	case critEssential:
		return "essential"
	case critStructural:
		return "structural"
	default:
		return "optional"
	}
}

// discoveryClass is one APIC class query in the run plan.
type discoveryClass struct {
	name string
	crit criticality
}

// discoveryPlan returns the ordered class queries for a run. Endpoint
// inventory (fvCEp) and its per-endpoint IP addresses (fvIp) are
// collected only for --purpose audit.
func discoveryPlan(audit bool) []discoveryClass {
	plan := []discoveryClass{
		{"fabricNode", critEssential},
		{"topSystem", critEssential},
		{"firmwareRunning", critOptional},
		{"fvTenant", critStructural},
		{"fvCtx", critStructural},
		{"fvBD", critStructural},
		{"fvSubnet", critStructural},
		{"fvAEPg", critStructural},
		{"l3extOut", critStructural},
		{"fvRsCtx", critOptional},
		{"fvRsBd", critOptional},
		{"l3extRsEctx", critOptional},
		{"faultInst", critOptional},
	}
	if audit {
		plan = append(plan,
			discoveryClass{"fvCEp", critOptional},
			discoveryClass{"fvIp", critOptional},
		)
	}
	return plan
}

// opOutcome is one discovery operation's result in the coverage record.
type opOutcome struct {
	Operation string `json:"operation"`
	Status    string `json:"status"` // "succeeded" | "failed" | "skipped"
	Count     int    `json:"count"`
	Category  string `json:"category,omitempty"` // sanitized; set only when Status == "failed"
}

// coverageRecorder accumulates per-operation outcomes for the run.
type coverageRecorder struct {
	ops []opOutcome
}

func (r *coverageRecorder) succeeded(op string, n int) {
	r.ops = append(r.ops, opOutcome{Operation: op, Status: "succeeded", Count: n})
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
// the resource extension (matching the NX-OS producer's coverage
// surface).
func (r *coverageRecorder) asSlice() []map[string]any {
	out := make([]map[string]any, 0, len(r.ops))
	for _, o := range r.ops {
		m := map[string]any{"operation": o.Operation, "status": o.Status, "count": o.Count}
		if o.Category != "" {
			m["category"] = o.Category
		}
		out = append(out, m)
	}
	return out
}

// summary is the one-line prose form for metadata.scope.description. It
// names the fabric domain (when known), the succeeded/total ratio, and
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
		if resources[i].Type != nodeRoleToType["controller"] {
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
