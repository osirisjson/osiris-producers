// transform_assignments.go - Cross-type assignment relationships for
// the OSIRIS JSON Microsoft Azure producer.
//
// These relationships are stated on the assigning side for many
// resource types at once, so they are resolved against every emitted
// resource (docIDMap: canonical ARM ID -> OSIRIS JSON resource ID)
// instead of a per-type ID map:
//
//   - workload -> user-assigned managed identity
//     (osiris.azure.identity.assignment), from the resource's ARM
//     identity block (identity.userAssignedIdentities) and, for AKS,
//     the cluster's identityProfile (e.g. the kubelet identity);
//   - monitored resource -> data collection rule (dataflow), from data
//     collection rule associations, whose ARM parent is the monitored
//     resource.
//
// Only references to resources in the document become connections;
// nothing is correlated by name.

package azure

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"go.osirisjson.org/producers/pkg/sdk"
)

// TransformIdentityAssignmentConnections emits one connection from each
// resource to every user-assigned managed identity it runs as.
// properties.assigned_as lists how the identity is assigned: "resource"
// for the ARM identity block, or the AKS identityProfile key (for
// example "kubeletidentity").
func TransformIdentityAssignmentConnections(resources []sdk.Resource, envelopes map[string]*GraphEnvelope, clusters []AKSCluster, docIDMap map[string]string) []sdk.Connection {
	type pair struct{ src, dst string }
	var order []pair
	roles := map[pair]map[string]bool{}
	names := map[pair]string{}
	link := func(srcArm, identityArm, role string) {
		src, ok := armLookup(docIDMap, srcArm)
		if !ok {
			return
		}
		dst, ok := armLookup(docIDMap, identityArm)
		if !ok || dst == src {
			return
		}
		k := pair{src, dst}
		if roles[k] == nil {
			roles[k] = map[string]bool{}
			order = append(order, k)
			names[k] = fmt.Sprintf("%s -> %s", extractLastSegment(srcArm), extractLastSegment(identityArm))
		}
		roles[k][role] = true
	}

	ids := make([]string, 0, len(resources))
	for _, r := range resources {
		ids = append(ids, r.Provider.NativeID)
	}
	sort.Strings(ids)
	for _, armID := range ids {
		env := envelopes[strings.ToLower(armID)]
		if env == nil || env.Identity == nil {
			continue
		}
		uami := make([]string, 0, len(env.Identity.UserAssignedIdentities))
		for k := range env.Identity.UserAssignedIdentities {
			uami = append(uami, k)
		}
		sort.Strings(uami)
		for _, identityArm := range uami {
			link(armID, identityArm, "resource")
		}
	}
	for _, c := range clusters {
		if c.Properties == nil {
			continue
		}
		keys := make([]string, 0, len(c.Properties.IdentityProfile))
		for k := range c.Properties.IdentityProfile {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if ref := c.Properties.IdentityProfile[k]; ref.ResourceID != "" {
				link(c.ID, ref.ResourceID, k)
			}
		}
	}

	var connections []sdk.Connection
	for _, k := range order {
		conn, ok := newEdge(connIdentityAssignment, k.src, k.dst, names[k])
		if !ok {
			continue
		}
		conn.Properties = map[string]any{"assigned_as": sortedKeys(roles[k])}
		connections = append(connections, conn)
	}
	return connections
}

// dcrAssocStats counts associations that yield no connection because
// one end is not a resource in the document (logged to stderr only).
type dcrAssocStats struct {
	Associations   int
	ResourceAbsent int // monitored resource not in the document
	RuleAbsent     int // data collection rule not in the document (e.g. another subscription)
}

// log writes the counts to l.
func (s dcrAssocStats) log(l *slog.Logger) {
	if l == nil || s.Associations == 0 {
		return
	}
	l.Info("data collection rule associations", "associations", s.Associations,
		"resource_not_in_document", s.ResourceAbsent, "rule_not_in_document", s.RuleAbsent)
}

// TransformDCRAssociationConnections emits a dataflow from each
// monitored resource to the data collection rule its telemetry is
// collected through. properties.association names the association.
func TransformDCRAssociationConnections(assocs []DCRAssociation, docIDMap map[string]string) ([]sdk.Connection, dcrAssocStats) {
	var connections []sdk.Connection
	st := dcrAssocStats{Associations: len(assocs)}
	for _, a := range assocs {
		src, ok := armLookup(docIDMap, a.ResourceID())
		if !ok {
			st.ResourceAbsent++
			continue
		}
		dst, ok := armLookup(docIDMap, a.DataCollectionRuleID)
		if !ok {
			st.RuleAbsent++
			continue
		}
		conn, ok := newEdge(connDataflow, src, dst,
			fmt.Sprintf("%s -> %s", extractLastSegment(a.ResourceID()), extractLastSegment(a.DataCollectionRuleID)))
		if !ok {
			continue
		}
		if a.Name != "" {
			conn.Properties = map[string]any{"association": a.Name}
		}
		connections = append(connections, conn)
	}
	return connections, st
}
