// transform_routing.go - Configured routing and BGP control-plane
// relationships for the OSIRIS JSON Microsoft Azure producer.
//
// Two kinds of configuration name a private IP address instead of an
// ARM resource:
//
//   - a user-defined route with next hop type VirtualAppliance names
//     the next-hop IP (routeTables.routes[].nextHopIpAddress);
//   - a Route Server BGP peer names the peer IP
//     (virtualHubs/bgpConnections[].peerIp).
//
// Such an IP is resolved to an exported resource only when exactly one
// NIC IP configuration (or, for next hops, one internal load balancer
// frontend) carries it inside the VNets the configuration can reach:
// the VNet of the referencing resource and the VNets an exported
// peering links it with.
// Everything else stays unresolved and is reported, never guessed.
//
// The two relationships keep different meanings:
//
//   - route table -> next hop (osiris.azure.route.nexthop, forward) is
//     configured forwarding intent, not observed traffic;
//   - NIC <-> Route Server (network.bgp, bidirectional) is a BGP
//     control-plane session; a Route Server never forwards traffic.
//
// No Route Server <-> gateway route exchange is emitted: it exists only
// when allowBranchToBranchTraffic is true, which is exported
// as a property.

package azure

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"go.osirisjson.org/producers/pkg/sdk"
)

// connRouteNextHop links a route table to the resource its
// VirtualAppliance routes send traffic to.
const connRouteNextHop = "osiris.azure.route.nexthop"

// ipHolder is an exported resource carrying a private IP in a VNet.
type ipHolder struct {
	resourceArm string // NIC or load balancer ARM ID
	vnetKey     string // canonical VNet ARM ID
}

// privateIPIndex maps a private IP to the resources carrying it.
type privateIPIndex map[string][]ipHolder

// vnetOf returns the canonical VNet ARM ID of a subnet ARM ID.
func vnetOf(subnetArm string) string {
	return armKey(armParent(subnetArm, "subnets"))
}

// buildPrivateIPIndex indexes NIC IP configurations and, when withLB is
// true, internal load balancer frontend IP configurations by private IP.
func buildPrivateIPIndex(nics []NetworkInterface, lbs []LoadBalancer, withLB bool) privateIPIndex {
	idx := privateIPIndex{}
	add := func(ip, resArm, subnetArm string) {
		if ip == "" || subnetArm == "" {
			return
		}
		v := vnetOf(subnetArm)
		for _, h := range idx[ip] {
			if armKey(h.resourceArm) == armKey(resArm) && h.vnetKey == v {
				return
			}
		}
		idx[ip] = append(idx[ip], ipHolder{resourceArm: resArm, vnetKey: v})
	}
	for _, n := range nics {
		for _, ipc := range n.IPConfigurations {
			add(ipc.PrivateIPAddress, n.ID, ipc.SubnetID())
		}
	}
	if withLB {
		for _, lb := range lbs {
			for _, fe := range lb.FrontendIPConfigurations {
				if fe.Subnet != nil {
					add(fe.PrivateIPAddress, lb.ID, fe.Subnet.ID)
				}
			}
		}
	}
	return idx
}

// vnetScope returns the canonical VNet IDs reachable from vnets through
// exported peerings that are not disconnected, in either direction.
func vnetScope(vnets map[string]bool, peerings []VNetPeering) map[string]bool {
	scope := map[string]bool{}
	for v := range vnets {
		scope[v] = true
	}
	for _, p := range peerings {
		if strings.EqualFold(p.PeeringState, "Disconnected") {
			continue
		}
		local, remote := armKey(p.VNetID()), armKey(p.RemoteVNetID())
		if local == "" || remote == "" {
			continue
		}
		if vnets[local] {
			scope[remote] = true
		}
		if vnets[remote] {
			scope[local] = true
		}
	}
	return scope
}

// resolveIP returns the single resource carrying ip within scope,
// and how the lookup ended: "resolved", "ambiguous" (several holders)
// or "unresolved" (none).
func (idx privateIPIndex) resolveIP(ip string, scope map[string]bool) (string, string) {
	var found []string
	for _, h := range idx[ip] {
		if scope[h.vnetKey] {
			found = append(found, h.resourceArm)
		}
	}
	switch len(found) {
	case 0:
		return "", "unresolved"
	case 1:
		return found[0], "resolved"
	default:
		return "", "ambiguous"
	}
}

// routeResolutionStats counts VirtualAppliance routes by outcome
// (logged to stderr only).
type routeResolutionStats struct {
	Resolved, Unresolved, Ambiguous, NoScope int
}

func (s routeResolutionStats) log(l *slog.Logger) {
	if l == nil || s.Resolved+s.Unresolved+s.Ambiguous+s.NoScope == 0 {
		return
	}
	l.Info("route next hop resolution", "resolved", s.Resolved, "unresolved", s.Unresolved,
		"ambiguous", s.Ambiguous, "route_table_without_subnet", s.NoScope)
}

// TransformRouteNextHopConnections emits one osiris.azure.route.nexthop
// connection from a route table to each exported NIC or internal load
// balancer that its VirtualAppliance routes name, resolved per the file
// comment. The route table's VNets are those of the subnets it is
// associated with (routeTable.subnets and subnet.routeTable).
// properties: next_hop_ips, route_count, address_prefix_count,
// route_names.
func TransformRouteNextHopConnections(tables []RouteTable, subnets []Subnet, peerings []VNetPeering, idx privateIPIndex, rtIDMap, targetIDMap map[string]string) ([]sdk.Connection, routeResolutionStats) {
	rtVNets := map[string]map[string]bool{}
	addVNet := func(rtArm, subnetArm string) {
		k := armKey(rtArm)
		if rtVNets[k] == nil {
			rtVNets[k] = map[string]bool{}
		}
		if v := vnetOf(subnetArm); v != "" {
			rtVNets[k][v] = true
		}
	}
	for _, rt := range tables {
		for _, s := range rt.Subnets {
			addVNet(rt.ID, s.ID)
		}
	}
	for _, s := range subnets {
		if rt := s.RouteTableId(); rt != "" {
			addVNet(rt, s.ID)
		}
	}

	type agg struct {
		ips, names, prefixes map[string]bool
		count                int
	}
	var st routeResolutionStats
	var connections []sdk.Connection
	for _, rt := range tables {
		sourceID, ok := armLookup(rtIDMap, rt.ID)
		if !ok {
			continue
		}
		scope := vnetScope(rtVNets[armKey(rt.ID)], peerings)
		byTarget := map[string]*agg{}
		var order []string
		for _, r := range rt.Routes {
			if !strings.EqualFold(r.NextHopType, "VirtualAppliance") || r.NextHopIPAddress == "" {
				continue
			}
			if len(rtVNets[armKey(rt.ID)]) == 0 {
				st.NoScope++
				continue
			}
			arm, outcome := idx.resolveIP(r.NextHopIPAddress, scope)
			targetID, ok := "", false
			if outcome == "resolved" {
				targetID, ok = armLookup(targetIDMap, arm)
			}
			switch {
			case ok:
				st.Resolved++
			case outcome == "ambiguous":
				st.Ambiguous++
				continue
			default:
				st.Unresolved++
				continue
			}
			a := byTarget[targetID]
			if a == nil {
				a = &agg{ips: map[string]bool{}, names: map[string]bool{}, prefixes: map[string]bool{}}
				byTarget[targetID] = a
				order = append(order, targetID)
			}
			a.count++
			a.ips[r.NextHopIPAddress] = true
			a.names[r.Name] = true
			a.prefixes[r.AddressPrefix] = true
		}
		for _, targetID := range order {
			a := byTarget[targetID]
			conn, ok := newEdge(connRouteNextHop, sourceID, targetID,
				fmt.Sprintf("%s -> %s", rt.Name, extractLastSegment(targetID)))
			if !ok {
				continue
			}
			conn.Properties = map[string]any{
				"next_hop_ips":         sortedKeys(a.ips),
				"route_count":          a.count,
				"address_prefix_count": len(a.prefixes),
				"route_names":          sortedKeys(a.names),
			}
			connections = append(connections, conn)
		}
	}
	return connections, st
}

// bgpPeerResolution records how each Route Server BGP peer was resolved,
// keyed by canonical Route Server ARM ID and peer name.
type bgpPeerResolution map[string]map[string]peerOutcome

type peerOutcome struct {
	outcome    string // resolved, unresolved, ambiguous, no_placement
	resourceID string // OSIRIS JSON resource ID of the NIC when resolved
}

// TransformRouteServerPeerConnections emits a network.bgp bidirectional
// connection between a Route Server and the NIC carrying each
// configured BGP peer IP, when exactly one exported NIC IP
// configuration carries it in the Route Server's VNet (from its IP
// configurations) or a VNet peered with it. Every peer's outcome is
// returned, so unresolved peers stay visible on the Route Server.
func TransformRouteServerPeerConnections(servers []RouteServer, peerings []VNetPeering, nicIdx privateIPIndex, rsIDMap, nicIDMap map[string]string) ([]sdk.Connection, bgpPeerResolution) {
	res := bgpPeerResolution{}
	var connections []sdk.Connection
	for _, rs := range servers {
		rsID, ok := armLookup(rsIDMap, rs.ID)
		if !ok {
			continue
		}
		vnets := map[string]bool{}
		for _, ipc := range rs.IPConfigurations {
			if ipc.Subnet != nil {
				if v := vnetOf(ipc.Subnet.ID); v != "" {
					vnets[v] = true
				}
			}
		}
		scope := vnetScope(vnets, peerings)
		out := map[string]peerOutcome{}
		for _, p := range rs.BGPConnections {
			if len(vnets) == 0 {
				out[p.Name] = peerOutcome{outcome: "no_placement"}
				continue
			}
			arm, outcome := nicIdx.resolveIP(p.PeerIp, scope)
			if outcome != "resolved" {
				out[p.Name] = peerOutcome{outcome: outcome}
				continue
			}
			nicID, ok := armLookup(nicIDMap, arm)
			if !ok {
				out[p.Name] = peerOutcome{outcome: "unresolved"}
				continue
			}
			out[p.Name] = peerOutcome{outcome: "resolved", resourceID: nicID}
			conn, ok := newBidirectionalEdge("network.bgp", nicID, rsID,
				fmt.Sprintf("%s <-> %s", extractLastSegment(arm), rs.Name))
			if !ok {
				continue
			}
			props := map[string]any{"peer_name": p.Name, "peer_ip": p.PeerIp}
			if p.PeerAsn > 0 {
				props["peer_asn"] = p.PeerAsn
			}
			if rs.VirtualRouterAsn > 0 {
				props["route_server_asn"] = rs.VirtualRouterAsn
			}
			conn.Properties = props
			if p.ConnectionState != "" {
				conn.State = p.ConnectionState
			}
			connections = append(connections, conn)
		}
		res[armKey(rs.ID)] = out
	}
	return connections, res
}

// annotateRouteServerPeers writes each BGP peer of the Route Server
// resources to extensions["osiris.azure"].bgp_connections with its
// resolution, so unresolved peers are preserved.
func annotateRouteServerPeers(resources []sdk.Resource, servers []RouteServer, res bgpPeerResolution) {
	byArm := map[string]RouteServer{}
	for _, rs := range servers {
		byArm[armKey(rs.ID)] = rs
	}
	for i := range resources {
		rs, ok := byArm[armKey(resources[i].Provider.NativeID)]
		if !ok || len(rs.BGPConnections) == 0 {
			continue
		}
		peers := make([]map[string]any, 0, len(rs.BGPConnections))
		for _, p := range rs.BGPConnections {
			peer := map[string]any{"name": p.Name, "peer_ip": p.PeerIp}
			if p.PeerAsn > 0 {
				peer["peer_asn"] = p.PeerAsn
			}
			if p.ConnectionState != "" {
				peer["connection_state"] = p.ConnectionState
			}
			if o, ok := res[armKey(rs.ID)][p.Name]; ok {
				peer["resolution"] = o.outcome
				if o.resourceID != "" {
					peer["resource_id"] = o.resourceID
				}
			}
			peers = append(peers, peer)
		}
		sort.Slice(peers, func(a, b int) bool { return peers[a]["name"].(string) < peers[b]["name"].(string) })
		if resources[i].Extensions == nil {
			resources[i].Extensions = map[string]any{}
		}
		ext, _ := resources[i].Extensions[extensionNamespace].(map[string]any)
		if ext == nil {
			ext = map[string]any{}
			resources[i].Extensions[extensionNamespace] = ext
		}
		ext["bgp_connections"] = peers
		if resources[i].Properties != nil {
			resources[i].Properties["bgp_peer_count"] = len(peers)
		}
	}
}
