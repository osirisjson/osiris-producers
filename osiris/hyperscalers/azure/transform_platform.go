// transform_platform.go - Platform networking and SQL relationship
// builders for the OSIRIS JSON Microsoft Azure producer.
//
// Each builder turns one explicit ARM reference into OSIRIS JSON
// connections between resources that are emitted in the same document:
//
//   - private endpoint -> Private Link target (network), from the
//     endpoint's own privateLinkServiceConnections and
//     manualPrivateLinkServiceConnections, with the approval state;
//   - load balancer -> backend NIC or VMSS (network), for backend pools
//     that a load-balancing rule sends traffic to;
//   - VMSS -> NSG (osiris.azure.nsg.association), from the scale set
//     NIC configurations;
//   - subnet -> NAT gateway (osiris.azure.natgateway.association), from
//     either side of the attachment;
//   - SQL database -> elastic pool (dependency), from elasticPoolId.
//
// ARM subresources (backend pools, IP configurations, rules) are
// evidence only; they never become OSIRIS JSON resources.
// Nothing is correlated by name. Dangling and duplicate edges are
// removed later by finalizeConnections.

package azure

import (
	"fmt"
	"sort"
	"strings"

	"go.osirisjson.org/producers/pkg/sdk"
)

// privateLinkStatus maps a Private Link approval state to an
// OSIRIS JSON status (spec chapter 4 section 4.5): only an approved
// binding carries traffic.
func privateLinkStatus(state string) string {
	switch strings.ToLower(state) {
	case "approved":
		return "active"
	case "pending", "rejected", "disconnected":
		return "inactive"
	case "":
		return ""
	default:
		return "unknown"
	}
}

// TransformPrivateEndpointToTargetConnections emits one network
// connection from each private endpoint to every Private Link target
// it names, whether the binding was auto-approved
// (privateLinkServiceConnections) or needs manual approval
// (manualPrivateLinkServiceConnections). targetIDMap maps the ARM IDs
// of all emitted resources to OSIRIS JSON resource IDs.
//
// status reflects the approval state (Approved -> active; Pending,
// Rejected, Disconnected -> inactive) and state keeps the ARM value.
// properties.group_ids lists the Private Link sub-resources
// (blob, vault, sqlServer, ...) and properties.manual_approval marks
// manual bindings. Several bindings of one endpoint to the same
// target merge into one edge.
func TransformPrivateEndpointToTargetConnections(pes []PrivateEndpoint, peIDMap, targetIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, pe := range pes {
		sourceID, ok := armLookup(peIDMap, pe.ID)
		if !ok {
			continue
		}
		type binding struct {
			conn   sdk.Connection
			groups map[string]bool
		}
		var order []string
		byTarget := map[string]*binding{}
		add := func(plsc azPrivateLinkServiceConnection, manual bool) {
			targetID, ok := armLookup(targetIDMap, plsc.PrivateLinkServiceID)
			if !ok {
				return
			}
			b := byTarget[targetID]
			if b == nil {
				conn, ok := newEdge(connNetwork, sourceID, targetID,
					fmt.Sprintf("%s -> %s", pe.Name, extractLastSegment(plsc.PrivateLinkServiceID)))
				if !ok {
					return
				}
				conn.Properties = map[string]any{"manual_approval": manual}
				if plsc.ConnectionState != nil && plsc.ConnectionState.Status != "" {
					conn.State = plsc.ConnectionState.Status
					_ = conn.SetStatus(privateLinkStatus(plsc.ConnectionState.Status))
				}
				b = &binding{conn: conn, groups: map[string]bool{}}
				byTarget[targetID] = b
				order = append(order, targetID)
			}
			for _, g := range plsc.GroupIDs {
				if g != "" {
					b.groups[g] = true
				}
			}
		}
		for _, plsc := range pe.PrivateLinkServiceConnections {
			add(plsc, false)
		}
		for _, plsc := range pe.ManualPrivateLinkServiceConnections {
			add(plsc, true)
		}
		for _, t := range order {
			b := byTarget[t]
			if len(b.groups) > 0 {
				b.conn.Properties["group_ids"] = sortedKeys(b.groups)
			}
			connections = append(connections, b.conn)
		}
	}
	return connections
}

// sortedKeys returns the keys of m in ascending order.
func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// armParent returns the part of a child ARM ID before /<segment>/
// (matched case-insensitively), or "" when the segment is absent.
// For ".../loadBalancers/lb1/backendAddressPools/pool1" and segment
// "backendAddressPools" it returns ".../loadBalancers/lb1".
func armParent(childID, segment string) string {
	i := strings.Index(strings.ToLower(childID), "/"+strings.ToLower(segment)+"/")
	if i <= 0 {
		return ""
	}
	return childID[:i]
}

// lbPool identifies a rule-backed backend pool by its load balancer.
type lbPool struct {
	lbArmID string
	name    string
}

// TransformLBBackendConnections emits a network connection from a load
// balancer to each NIC and VMSS that is a member of one of its backend
// pools, for pools that at least one load-balancing rule sends traffic
// to. Pools without a rule (outbound-only or unused) carry no inbound
// traffic and yield no edge.
//
// Membership is read from every side that states it: the pool's
// backendIPConfigurations and loadBalancerBackendAddresses (NIC IP
// configurations), the NIC IP configurations'
// loadBalancerBackendAddressPools and the VMSS model's IP
// configurations. properties.backend_pools lists the pool names that
// link the pair.
func TransformLBBackendConnections(lbs []LoadBalancer, nics []NetworkInterface, vmsses []VMSS, lbIDMap, nicIDMap, vmssIDMap map[string]string) []sdk.Connection {
	// Rule-backed pools, keyed by canonical pool ARM ID.
	pools := map[string]lbPool{}
	for _, lb := range lbs {
		ruled := map[string]bool{}
		for _, rule := range lb.LoadBalancingRules {
			for _, id := range rule.BackendPoolIDs() {
				ruled[armKey(id)] = true
			}
		}
		for _, p := range lb.BackendAddressPools {
			if p.ID != "" && (ruled[armKey(p.ID)] || len(armRefIDs(p.LoadBalancingRules)) > 0) {
				pools[armKey(p.ID)] = lbPool{lbArmID: lb.ID, name: p.Name}
			}
		}
	}

	type pair struct{ lb, member string }
	var order []pair
	poolNames := map[pair]map[string]bool{}
	link := func(poolArmID, memberID string) {
		pool, ok := pools[armKey(poolArmID)]
		if !ok {
			return
		}
		lbID, ok := armLookup(lbIDMap, pool.lbArmID)
		if !ok {
			return
		}
		k := pair{lbID, memberID}
		if poolNames[k] == nil {
			poolNames[k] = map[string]bool{}
			order = append(order, k)
		}
		poolNames[k][pool.name] = true
	}

	// Pool side: members are NIC IP configurations.
	for _, lb := range lbs {
		for _, p := range lb.BackendAddressPools {
			var ipConfigIDs []string
			for _, ref := range p.BackendIPConfigurations {
				ipConfigIDs = append(ipConfigIDs, ref.ID)
			}
			for _, a := range p.LoadBalancerBackendAddresses {
				if a.NetworkInterfaceIPConfiguration != nil {
					ipConfigIDs = append(ipConfigIDs, a.NetworkInterfaceIPConfiguration.ID)
				}
			}
			for _, ipc := range ipConfigIDs {
				if nicID, ok := armLookup(nicIDMap, armParent(ipc, "ipConfigurations")); ok {
					link(p.ID, nicID)
				}
			}
		}
	}
	// NIC side.
	for _, nic := range nics {
		nicID, ok := armLookup(nicIDMap, nic.ID)
		if !ok {
			continue
		}
		for _, ipc := range nic.IPConfigurations {
			for _, poolID := range armRefIDs(ipc.LoadBalancerBackendAddressPools) {
				link(poolID, nicID)
			}
		}
	}
	// VMSS model side.
	for i := range vmsses {
		vmssID, ok := armLookup(vmssIDMap, vmsses[i].ID)
		if !ok {
			continue
		}
		for _, poolID := range vmsses[i].VMSSBackendPoolIDs() {
			link(poolID, vmssID)
		}
	}

	var connections []sdk.Connection
	for _, k := range order {
		conn, ok := newEdge(connNetwork, k.lb, k.member,
			fmt.Sprintf("%s -> %s", extractLastSegment(k.lb), extractLastSegment(k.member)))
		if !ok {
			continue
		}
		conn.Properties = map[string]any{"backend_pools": sortedKeys(poolNames[k])}
		connections = append(connections, conn)
	}
	return connections
}

// TransformVMSSToNSGConnections associates a VMSS with each network
// security group its NIC configurations apply to scale set instances.
func TransformVMSSToNSGConnections(vmsses []VMSS, vmssIDMap, nsgIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for i := range vmsses {
		sourceID, ok := armLookup(vmssIDMap, vmsses[i].ID)
		if !ok {
			continue
		}
		for _, nsgArmID := range vmsses[i].VMSSNSGIDs() {
			targetID, ok := armLookup(nsgIDMap, nsgArmID)
			if !ok {
				continue
			}
			if conn, ok := newEdge(connNSGAssociation, sourceID, targetID,
				fmt.Sprintf("%s -> %s", vmsses[i].Name, extractLastSegment(nsgArmID))); ok {
				connections = append(connections, conn)
			}
		}
	}
	return connections
}

// TransformSubnetToNATGatewayConnections associates a subnet with the
// NAT gateway its outbound traffic uses. The attachment is read from
// the subnet (natGateway) and from the gateway (subnets); both sides
// yield the same connection ID and are collapsed by finalizeConnections.
func TransformSubnetToNATGatewayConnections(subnets []Subnet, gws []NATGateway, subnetIDMap map[string]string) []sdk.Connection {
	natIDMap := make(map[string]string, len(gws))
	for _, gw := range gws {
		natIDMap[armKey(gw.ID)] = resourceID("network.gateway", gw.ID)
	}
	var connections []sdk.Connection
	emit := func(subnetArmID, natArmID string) {
		sourceID, ok := armLookup(subnetIDMap, subnetArmID)
		if !ok {
			return
		}
		targetID, ok := armLookup(natIDMap, natArmID)
		if !ok {
			return
		}
		if conn, ok := newEdge(connNATGatewayAssociation, sourceID, targetID,
			fmt.Sprintf("%s -> %s", extractLastSegment(subnetArmID), extractLastSegment(natArmID))); ok {
			connections = append(connections, conn)
		}
	}
	for _, s := range subnets {
		if s.NatGateway != nil && s.NatGateway.ID != "" {
			emit(s.ID, s.NatGateway.ID)
		}
	}
	for _, gw := range gws {
		for _, subnetArmID := range gw.SubnetIDs() {
			emit(subnetArmID, gw.ID)
		}
	}
	return connections
}

// TransformSQLDatabaseToElasticPoolConnections emits a dependency from
// each SQL database to the elastic pool that provides its compute
// and storage (elasticPoolId).
func TransformSQLDatabaseToElasticPoolConnections(servers []SQLServer, dbIDMap, poolIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, s := range servers {
		for _, db := range s.Databases {
			if db.Properties == nil || db.Properties.ElasticPoolID == "" {
				continue
			}
			sourceID, ok := armLookup(dbIDMap, db.ID)
			if !ok {
				continue
			}
			targetID, ok := armLookup(poolIDMap, db.Properties.ElasticPoolID)
			if !ok {
				continue
			}
			if conn, ok := newEdge(connDependency, sourceID, targetID,
				fmt.Sprintf("%s -> %s", db.Name, extractLastSegment(db.Properties.ElasticPoolID))); ok {
				connections = append(connections, conn)
			}
		}
	}
	return connections
}
