// discover.go - APIC discovery plan and per-domain failure policy.
// Each class query has a declared criticality: an essential or
// structural failure aborts the whole document; an optional failure
// degrades to partial output. The coverage record that reports which
// operations actually returned data lives in coverage.go.
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

// discoveryPlan returns the ordered class queries for a run. The
// physical-topology classes (l1PhysIf..fvRsPathAtt) are collected in
// both purposes: the physical fabric graph is emitted regardless of
// --purpose, bounded in documentation mode to ports that participate in
// topology. Endpoint inventory (fvCEp) and its per-endpoint IP
// addresses (fvIp), filter entries (vzEntry) and the per-leaf vPC
// domain state (vpcDom) are collected only for --purpose audit.
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

		// Configured policy intent (application profiles, contracts,
		// subjects, filters and their relations). All optional: a
		// failure leaves the policy slice incomplete, recorded in the
		// coverage record and never implies "no policy".
		{"fvAp", critOptional},
		{"vzBrCP", critOptional},
		{"vzSubj", critOptional},
		{"vzFilter", critOptional},
		{"fvRsProv", critOptional},
		{"fvRsCons", critOptional},
		{"vzRsSubjFiltAtt", critOptional},

		// Physical + logical fabric topology. All optional: a
		// failure leaves the document without that slice of the graph
		// and adds a coverage entry, it never aborts the producer run.
		{"l1PhysIf", critOptional},    // physical switch ports (config)
		{"ethpmPhysIf", critOptional}, // port operational state
		{"fabricLink", critOptional},  // spine/leaf physical links
		{"lldpAdjEp", critOptional},   // LLDP neighbours
		{"cdpAdjEp", critOptional},    // CDP neighbours (merge/fallback)
		{"pcAggrIf", critOptional},    // port-channel aggregate ifaces
		{"pcRsMbrIfs", critOptional},  // port-channel -> member port
		{"fvRsPathAtt", critOptional}, // EPG -> path attachments

		// Logical paths, vPC and pods. All optional: without them the
		// path resources, member edges and pod groups are missing,
		// recorded in the coverage record; the physical graph stays.
		{"fabricPod", critOptional},         // pods
		{"fabricPathEp", critOptional},      // logical paths (port, PC, vPC, FEX)
		{"vpcIf", critOptional},             // vPC leg -> vPC path
		{"vpcRsVpcConf", critOptional},      // vPC leg -> port-channel
		{"fabricExplicitGEp", critOptional}, // vPC explicit protection groups
		{"fabricNodePEp", critOptional},     // vPC protection group members

		// L3Out external connectivity (configured intent). All
		// optional: a failure leaves the L3Out slice incomplete and the
		// L3Out inventory itself (l3extOut, structural) unchanged.
		{"l3extInstP", critOptional},          // external EPGs
		{"l3extSubnet", critOptional},         // external EPG policy prefixes
		{"l3extRsNodeL3OutAtt", critOptional}, // L3Out -> border leaf
		{"l3extRsPathL3OutAtt", critOptional}, // L3Out -> routed path
		{"fvRsBDToOut", critOptional},         // bridge domain -> L3Out
	}
	if audit {
		plan = append(plan,
			discoveryClass{"vpcDom", critOptional},  // per-leaf vPC domain state
			discoveryClass{"vzEntry", critOptional}, // filter entries (ports, protocols)
			discoveryClass{"fvCEp", critOptional},
			discoveryClass{"fvIp", critOptional},
		)
	}
	return plan
}
