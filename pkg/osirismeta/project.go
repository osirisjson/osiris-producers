// project.go - Purpose-driven document projection.
//
// Project mutates an sdk.Document so its content matches the declared
// --purpose. Audit returns the document unchanged (full detail).
// Documentation strips the rich `properties` and `extensions` maps from
// every resource, connection and group, leaving identity, provider
// traceability, relationships, names and tags intact.
//
// Per OSIRIS JSON spec chapter 13.1.3 the documentation projection targets the
// typical viewer (diagrams, inventory dashboards, high-level docs) and
// omits "detailed IP addresses, serial numbers and configuration
// specifics". The audit projection keeps those for compliance use cases.
//
// Per-type enrichment of the documentation projection (e.g. keeping
// subnet CIDRs or VM sizes even in minimal mode) is the responsibility
// of each producer's transform layer, not this package.

package osirismeta

import "go.osirisjson.org/producers/pkg/sdk"

// Project applies the projection for the given purpose to doc in place.
// Also writes the canonical purpose string to doc.Metadata.Scope.Purpose
// so consumers can tell at a glance which grade of detail they're reading.
//
// Project is safe to call with a nil doc (it returns without error).
// An empty Purpose is treated as Default().
func Project(doc *sdk.Document, p Purpose) {
	if doc == nil {
		return
	}
	if p == "" {
		p = Default()
	}

	ensureScopePurpose(doc, p)

	if p == PurposeAudit {
		return
	}

	// Documentation projection: drop rich payload, keep the graph.
	for i := range doc.Topology.Resources {
		doc.Topology.Resources[i].Properties = nil
		doc.Topology.Resources[i].Extensions = nil
	}
	for i := range doc.Topology.Connections {
		doc.Topology.Connections[i].Properties = nil
		doc.Topology.Connections[i].Extensions = nil
	}
	for i := range doc.Topology.Groups {
		doc.Topology.Groups[i].Properties = nil
		doc.Topology.Groups[i].Extensions = nil
	}
}

// ensureScopePurpose writes p into doc.Metadata.Scope.Purpose, creating the
// Scope if it was nil. It never overwrites a non-empty scope.Purpose so that
// producers which already set a value keep theirs.
func ensureScopePurpose(doc *sdk.Document, p Purpose) {
	if doc.Metadata.Scope == nil {
		doc.Metadata.Scope = &sdk.Scope{}
	}
	if doc.Metadata.Scope.Purpose == "" {
		doc.Metadata.Scope.Purpose = p.String()
	}
}
