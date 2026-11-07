// relations.go - Final connection resolution pass for the OSIRIS JSON
// Microsoft Azure producer.
//
// Relationship builders (the Transform*Connections functions) each turn
// one kind of ARM reference into OSIRIS JSON connections.
// Several references are visible from both sides (a VM lists its disks,
// a disk names its VM in managedBy) and a reference may point at a
// resource outside the discovery scope. finalizeConnections runs once
// over every candidate connection before the document is built and
// enforces, in one place:
//
//   - referential integrity: source and target must be resources in the
//     same document `topology.resources` array (OSIRIS JSON spec
//     chapter 5 section 5.1.5) dangling edges are drop never emitted;
//   - no self-references (source == target, same section);
//   - deduplication on connection ID, which is derived from the
//     canonical tuple (type, direction, source, target), so the same
//     relationship learned from two ARM fields yields exactly one
//     connection.
//
// The pass is diagnostic-only on stderr: counts are logged via the
// [slog.Logger] and never written into the OSIRIS JSON document.

package azure

import (
	"log/slog"
	"sort"

	"go.osirisjson.org/producers/pkg/sdk"
)

// Connection types used by relationship builders. Standard types follow
// OSIRIS JSON spec chapter 5 section 5.2.3; Azure-specific associations
// use the namespaced pattern of OSIRIS JSON spec section 5.2.4.
const (
	connContains          = "contains"
	connNetwork           = "network"
	connDependencyStorage = "dependency.storage"
	// connNSGAssociation links a subnet or NIC to the network security
	// group applied to it. It is a policy association, not network
	// reachability, so the standard "network" type does not apply.
	connNSGAssociation = "osiris.azure.nsg.association"
	// connRouteTableAssociation links a subnet to the route table
	// applied to it, and connNATGatewayAssociation a subnet to the NAT
	// gateway its outbound traffic uses. Both are configuration
	// associations of the subnet, like the NSG association.
	connRouteTableAssociation = "osiris.azure.routetable.association"
	connNATGatewayAssociation = "osiris.azure.natgateway.association"
	connDependency            = "dependency"
	connDataflow              = "dataflow"
	// connASGAssociation links a NIC to an application security group
	// it is a member of, and connIdentityAssignment a workload to a
	// user-assigned managed identity it runs as. Both are configuration
	// associations, not network reachability.
	connASGAssociation     = "osiris.azure.asg.association"
	connIdentityAssignment = "osiris.azure.identity.assignment"
	// connERConnectionEndpoint links a Microsoft.Network/connections
	// resource of type ExpressRoute to the gateway and the circuit it
	// configures: a configuration relationship, next to the gateway <->
	// circuit `network` edge that represents the data path.
	connERConnectionEndpoint = "osiris.azure.expressroute.connection.endpoint"
)

// newBidirectionalEdge builds a bidirectional connection between
// a and b with a deterministic ID derived from (type, direction, a, b).
func newBidirectionalEdge(connType, a, b, name string) (sdk.Connection, bool) {
	key := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
		Type:      connType,
		Direction: "bidirectional",
		Source:    a,
		Target:    b,
	})
	conn, err := sdk.NewConnection(sdk.BuildConnectionID(key, 16), connType, a, b)
	if err != nil {
		return sdk.Connection{}, false
	}
	conn.Name = name
	_ = conn.SetDirection("bidirectional")
	return conn, true
}

// newEdge builds a forward connection from source to target with a
// deterministic ID derived from (type, direction, source, target).
// ok is false when the SDK rejects the input.
func newEdge(connType, source, target, name string) (sdk.Connection, bool) {
	key := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
		Type:      connType,
		Direction: "forward",
		Source:    source,
		Target:    target,
	})
	conn, err := sdk.NewConnection(sdk.BuildConnectionID(key, 16), connType, source, target)
	if err != nil {
		return sdk.Connection{}, false
	}
	conn.Name = name
	_ = conn.SetDirection("forward")
	return conn, true
}

// relationStats counts the outcome of [finalizeConnections].
type relationStats struct {
	Candidates int            // connections offered by relationship builders
	Emitted    int            // connections kept
	Duplicates int            // dropped because an identical ID was already kept
	Dangling   int            // dropped because an endpoint is not a resource
	SelfRef    int            // dropped because source == target
	ByType     map[string]int // emitted connections per connection type
}

// finalizeConnections returns the subset of conns that is safe to emit,
// in input order, together with the counts of what was dropped and why.
// resourceIDs is the set of OSIRIS JSON resource IDs in the document.
func finalizeConnections(conns []sdk.Connection, resourceIDs map[string]struct{}) ([]sdk.Connection, relationStats) {
	st := relationStats{Candidates: len(conns), ByType: map[string]int{}}
	seen := make(map[string]struct{}, len(conns))
	out := make([]sdk.Connection, 0, len(conns))
	for _, c := range conns {
		if c.Source == c.Target {
			st.SelfRef++
			continue
		}
		_, okS := resourceIDs[c.Source]
		_, okT := resourceIDs[c.Target]
		if !okS || !okT {
			st.Dangling++
			continue
		}
		if _, dup := seen[c.ID]; dup {
			st.Duplicates++
			continue
		}
		seen[c.ID] = struct{}{}
		st.ByType[c.Type]++
		out = append(out, c)
	}
	st.Emitted = len(out)
	return out, st
}

// log writes the relationship resolution summary to the logger.
// Per-type counts are emitted in sorted order so the log line
// is deterministic.
func (s relationStats) log(l *slog.Logger) {
	if l == nil {
		return
	}
	types := make([]string, 0, len(s.ByType))
	for t := range s.ByType {
		types = append(types, t)
	}
	sort.Strings(types)
	attrs := []any{
		"candidates", s.Candidates,
		"emitted", s.Emitted,
		"duplicates", s.Duplicates,
		"dangling", s.Dangling,
		"self_references", s.SelfRef,
	}
	for _, t := range types {
		attrs = append(attrs, "type."+t, s.ByType[t])
	}
	l.Info("relationship resolution", attrs...)
}
