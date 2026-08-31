// discover.go - APIC discovery plan and per-domain failure policy. Each
// class query has a declared criticality: an essential or structural
// failure aborts the whole document; an optional failure degrades to
// partial output. The coverage record that reports which operations
// actually returned data lives in coverage.go.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package apic

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
