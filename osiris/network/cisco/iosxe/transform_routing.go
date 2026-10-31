// transform_routing.go - Layer-3 topology for the Cisco IOS-XE OSIRIS
// JSON Producer: connected subnets, configured static routes and
// OSPF/BGP neighbor sessions.
//
//   - Connected subnets: every network prefix configured on an emitted
//     interface becomes one network.subnet resource per (VRF, prefix),
//     ID cisco.iosxe::<serial>/subnet/<vrf>/<cidr> (vrf "default" for
//     the global table), linked to each interface addressed in it by a
//     bidirectional network.l3 connection and added to its VRF group.
//     Host prefixes (/32, /128) and IPv6 link-local prefixes describe
//     an interface, not a segment and are skipped.
//   - Static routes: the configured "ip route" / "ipv6 route" entries,
//     global and per VRF, as extensions.osiris.cisco.static_routes on
//     the device. This is configuration; the routing table (RIB) is
//     volatile operational state and is never collected.
//   - OSPF and BGP: each neighbor session becomes a network.ospf
//     (interface -> neighbor) or network.bgp (device -> neighbor)
//     connection to an unresolved stub resource, carrying only the
//     session facts the device reports: state, VRF, area or address
//     families. No route or prefix counters are emitted.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package iosxe

import (
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"

	"go.osirisjson.org/producers/pkg/sdk"
)

// defaultVRF names the global routing table, as IOS-XE itself does in
// its BGP operational model.
const defaultVRF = "default"

// connectedPrefix returns the network prefix of an interface address in
// CIDR form, or false for host prefixes and IPv6 link-local addresses.
func connectedPrefix(cidr string) (string, bool) {
	ip, n, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", false
	}
	ones, bits := n.Mask.Size()
	if ones == bits || ip.IsLinkLocalUnicast() {
		return "", false
	}
	return n.String(), true
}

// TransformSubnets builds the connected network.subnet resources of the
// device with key deviceKey from the interface address properties set
// by TransformInterfaces, the network.l3 connections from each
// interface to its subnets and adds each subnet to its VRF group.
// ifVRF maps interface names to VRFs (see interfaceVRFs).
func TransformSubnets(deviceKey string, set interfaceSet, ifVRF map[string]string, groups []sdk.Group, vrfNameToGroupID map[string]string) ([]sdk.Resource, []sdk.Connection) {
	gidx := groupIndex(groups)
	var resources []sdk.Resource
	var conns []sdk.Connection
	subnetIDs := map[string]string{}

	for _, r := range set.Resources {
		vrf := ifVRF[r.Name]
		if vrf == "" {
			vrf = defaultVRF
		}
		var cidrs []string
		for _, k := range []string{"ipv4_addresses", "ipv6_addresses"} {
			if v, ok := r.Properties[k].([]string); ok {
				cidrs = append(cidrs, v...)
			}
		}
		linked := map[string]bool{}
		for _, c := range cidrs {
			prefix, ok := connectedPrefix(c)
			if !ok {
				continue
			}
			key := vrf + "/" + prefix
			subnetID, ok := subnetIDs[key]
			if !ok {
				native := deviceKey + "/subnet/" + key
				subnetID = resourceID(providerName, native)
				s, err := sdk.NewResource(subnetID, "network.subnet", sdk.Provider{Name: providerName, NativeID: native})
				if err != nil {
					continue
				}
				s.Name = prefix
				s.Status = "active"
				props := map[string]any{"cidr": prefix}
				if vrf != defaultVRF {
					props["vrf"] = vrf
				}
				s.Properties = props
				resources = append(resources, s)
				subnetIDs[key] = subnetID
				if gi, ok := gidx[vrfNameToGroupID[vrf]]; ok && vrf != defaultVRF {
					groups[gi].AddMembers(subnetID)
				}
			}
			if linked[subnetID] {
				continue
			}
			linked[subnetID] = true
			ck := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
				Type:      "network.l3",
				Direction: "bidirectional",
				Source:    r.ID,
				Target:    subnetID,
			})
			conn, err := sdk.NewConnection(sdk.BuildConnectionID(ck, 16), "network.l3", r.ID, subnetID)
			if err != nil {
				continue
			}
			conn.Name = fmt.Sprintf("%s in %s", r.Name, prefix)
			conn.Direction = "bidirectional"
			conn.Status = r.Status
			if conn.Status == "" {
				conn.Status = "unknown"
			}
			conns = append(conns, conn)
		}
	}
	return resources, conns
}

// staticPrefixV4 returns "a.b.c.d/len" for a static route destination,
// accepting the default route and rejecting non-contiguous masks.
func staticPrefixV4(prefix, mask string) (string, bool) {
	ip := net.ParseIP(strings.TrimSpace(prefix)).To4()
	m := net.ParseIP(strings.TrimSpace(mask)).To4()
	if ip == nil || m == nil {
		return "", false
	}
	ones, bits := net.IPMask(m).Size()
	if bits == 0 {
		return "", false
	}
	return fmt.Sprintf("%s/%d", ip.Mask(net.IPMask(m)), ones), true
}

// staticPrefixV6 canonicalizes an IPv6 static route destination.
func staticPrefixV6(prefix string) (string, bool) {
	ip, n, err := net.ParseCIDR(strings.TrimSpace(prefix))
	if err != nil || ip.To4() != nil {
		return "", false
	}
	return n.String(), true
}

// routeEntries expands one destination's forwarding list into route
// entries. fwd is a next-hop address, an interface (including Null0),
// or an interface qualified by interface-next-hop addresses.
func routeEntries(prefix, vrf string, fwds []routeFwd) []map[string]any {
	var out []map[string]any
	base := func(metric, name string) map[string]any {
		e := map[string]any{"prefix": prefix}
		if vrf != "" {
			e["vrf"] = vrf
		}
		if n, err := strconv.Atoi(strings.TrimSpace(metric)); err == nil {
			e["metric"] = n
		}
		if v := strings.TrimSpace(name); v != "" {
			e["name"] = v
		}
		return e
	}
	for _, f := range fwds {
		fwd := strings.TrimSpace(f.Fwd)
		if fwd == "" {
			continue
		}
		if ip := net.ParseIP(fwd); ip != nil {
			e := base(f.Metric, f.Name)
			e["next_hop"] = ip.String()
			out = append(out, e)
			continue
		}
		emitted := false
		for _, nh := range f.NextHops {
			addr := strings.TrimSpace(nh.IPv4)
			if addr == "" {
				addr = strings.TrimSpace(nh.IPv6)
			}
			ip := net.ParseIP(addr)
			if ip == nil {
				continue
			}
			e := base(nh.Metric, nh.Name)
			e["interface"] = fwd
			e["next_hop"] = ip.String()
			out = append(out, e)
			emitted = true
		}
		if !emitted {
			e := base(f.Metric, f.Name)
			e["interface"] = fwd
			out = append(out, e)
		}
	}
	return out
}

// TransformStaticRoutes converts the configured static routes into the
// static_routes extension array, in configuration order: IPv4 global,
// IPv4 per VRF, IPv6 global, IPv6 per VRF. Entries with an invalid
// destination are skipped.
func TransformStaticRoutes(r nativeRoutes) []map[string]any {
	var out []map[string]any
	v4 := func(vrf string, routes []ipv4Route) {
		for _, rt := range routes {
			p, ok := staticPrefixV4(rt.Prefix, rt.Mask)
			if !ok {
				continue
			}
			out = append(out, routeEntries(p, vrf, append(append([]routeFwd{}, rt.Fwd...), rt.FwdTrck...))...)
		}
	}
	v6 := func(vrf string, routes []ipv6Route) {
		for _, rt := range routes {
			p, ok := staticPrefixV6(rt.Prefix)
			if !ok {
				continue
			}
			out = append(out, routeEntries(p, vrf, rt.Fwd)...)
		}
	}
	v4("", r.IPv4.Routes)
	for _, v := range r.IPv4.VRFs {
		v4(strings.TrimSpace(v.Name), v.Routes)
	}
	v6("", r.IPv6.Routes)
	for _, v := range r.IPv6.VRFs {
		v6(strings.TrimSpace(v.Name), v.Routes)
	}
	return out
}

// routerIDString renders an OSPF router ID, reported as a uint32, in
// dotted-quad form. A value that is already dotted is returned as is.
func routerIDString(v string) string {
	v = strings.TrimSpace(v)
	if net.ParseIP(v) != nil {
		return v
	}
	n, err := strconv.ParseUint(v, 10, 32)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d.%d.%d.%d", n>>24, n>>16&0xff, n>>8&0xff, n&0xff)
}

// ospfState strips the "ospf-nbr-" prefix from the neighbor FSM enum.
func ospfState(s string) string {
	return strings.TrimPrefix(strings.TrimSpace(s), "ospf-nbr-")
}

// TransformOSPFProcesses summarizes each OSPF instance as its process
// ID and router ID, for the device's cisco extension. Neighbors are
// emitted as connections by TransformOSPFNeighbors.
func TransformOSPFProcesses(ospf ospfOperData) []map[string]any {
	var out []map[string]any
	for _, inst := range ospf.Instances {
		p := map[string]any{}
		if n, err := strconv.Atoi(strings.TrimSpace(inst.ProcessID)); err == nil {
			p["process_id"] = n
		}
		if id := routerIDString(inst.RouterID); id != "" {
			p["router_id"] = id
		}
		if af := strings.TrimSpace(inst.AF); af != "" {
			p["address_family"] = af
		}
		if len(p) > 0 {
			out = append(out, p)
		}
	}
	return out
}

// TransformOSPFNeighbors converts OSPF adjacencies into network.ospf
// connections from the local interface to a stub resource per neighbor
// router ID. A neighbor seen on an interface that is not an emitted
// resource is skipped. One stub is emitted per router ID, however many
// interfaces it is adjacent on.
func TransformOSPFNeighbors(deviceID string, ospf ospfOperData, ifNameToID map[string]string) ([]sdk.Connection, []sdk.Resource) {
	var conns []sdk.Connection
	var stubs []sdk.Resource
	stubIDs := map[string]string{}
	seenConn := map[string]bool{}

	for _, inst := range ospf.Instances {
		for _, area := range inst.Areas {
			for _, intf := range area.Interfaces {
				ifName, localID, ok := lookupInterface(ifNameToID, intf.Name)
				if !ok {
					continue
				}
				for _, n := range intf.Neighbors {
					rid := strings.TrimSpace(n.NeighborID)
					if net.ParseIP(rid) == nil {
						continue
					}
					remoteID, ok := stubIDs[rid]
					if !ok {
						remoteID = unresolvedStubID(deviceID, "ospf-neighbor", rid)
						stub, err := sdk.NewResource(remoteID, "network.interface", sdk.Provider{
							Name: unknownProviderName, NativeID: "ospf|" + rid, Source: "ospf",
						})
						if err != nil {
							continue
						}
						stub.Name = "ospf-neighbor:" + rid
						stub.Status = "unknown"
						props := map[string]any{"ospf_router_id": rid}
						if a := net.ParseIP(strings.TrimSpace(n.Address)); a != nil {
							props["remote_address"] = a.String()
						}
						stub.Properties = props
						stubs = append(stubs, stub)
						stubIDs[rid] = remoteID
					}

					ck := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
						Type: "network.ospf", Direction: "bidirectional", Source: localID, Target: remoteID,
					})
					connID := sdk.BuildConnectionID(ck, 16)
					if seenConn[connID] {
						continue
					}
					seenConn[connID] = true
					conn, err := sdk.NewConnection(connID, "network.ospf", localID, remoteID)
					if err != nil {
						continue
					}
					conn.Name = fmt.Sprintf("OSPF %s <-> %s", ifName, rid)
					conn.Direction = "bidirectional"
					state := ospfState(n.State)
					conn.Status = "inactive"
					if state == "full" || state == "two-way" {
						conn.Status = "active"
					}
					props := map[string]any{}
					if state != "" {
						props["state"] = state
					}
					if a, err := strconv.ParseUint(strings.TrimSpace(area.AreaID), 10, 32); err == nil {
						props["area"] = a
					}
					if p, err := strconv.Atoi(strings.TrimSpace(inst.ProcessID)); err == nil {
						props["process_id"] = p
					}
					if len(props) > 0 {
						conn.Properties = props
					}
					conns = append(conns, conn)
				}
			}
		}
	}
	return conns, stubs
}

// bgpState strips the "fsm-" prefix from the BGP FSM enum.
func bgpState(s string) string {
	return strings.TrimPrefix(strings.TrimSpace(s), "fsm-")
}

// TransformBGPNeighbors converts BGP neighbors into network.bgp
// connections from the device to a stub resource per (VRF, neighbor
// address). The operational model lists a neighbor once per address
// family; those rows are merged into one connection whose
// address_families property lists them all.
func TransformBGPNeighbors(deviceID string, bgp bgpStateData) ([]sdk.Connection, []sdk.Resource) {
	type session struct {
		vrf, addr, asn, desc, state, local string
		afs                                []string
	}
	var order []string
	sessions := map[string]*session{}
	for _, n := range bgp.Neighbors.Entries {
		ip := net.ParseIP(strings.TrimSpace(n.NeighborID))
		if ip == nil {
			continue
		}
		vrf := strings.TrimSpace(n.VRFName)
		if vrf == "" {
			vrf = defaultVRF
		}
		key := vrf + "|" + ip.String()
		s, ok := sessions[key]
		if !ok {
			s = &session{vrf: vrf, addr: ip.String()}
			sessions[key] = s
			order = append(order, key)
		}
		if af := strings.TrimSpace(n.AFISAFI); af != "" {
			s.afs = append(s.afs, af)
		}
		if s.asn == "" {
			s.asn = strings.TrimSpace(n.AS)
		}
		if s.desc == "" {
			s.desc = strings.TrimSpace(n.Description)
		}
		if s.state == "" {
			s.state = bgpState(n.SessionState)
		}
		if s.local == "" {
			if l := net.ParseIP(strings.TrimSpace(n.LocalHost)); l != nil && !l.IsUnspecified() {
				s.local = l.String()
			}
		}
	}

	var conns []sdk.Connection
	var stubs []sdk.Resource
	for _, key := range order {
		s := sessions[key]
		remoteID := unresolvedStubID(deviceID, "bgp-neighbor", s.vrf, s.addr)
		stub, err := sdk.NewResource(remoteID, "network.interface", sdk.Provider{
			Name: unknownProviderName, NativeID: "bgp|" + s.vrf + "|" + s.addr, Source: "bgp",
		})
		if err != nil {
			continue
		}
		stub.Name = "bgp-neighbor:" + s.addr
		stub.Status = "unknown"
		props := map[string]any{"remote_address": s.addr}
		if n, err := strconv.ParseUint(s.asn, 10, 32); err == nil {
			props["remote_asn"] = n
		}
		stub.Properties = props
		stubs = append(stubs, stub)

		ck := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
			Type: "network.bgp", Direction: "bidirectional", Source: deviceID, Target: remoteID,
		})
		conn, err := sdk.NewConnection(sdk.BuildConnectionID(ck, 16), "network.bgp", deviceID, remoteID)
		if err != nil {
			continue
		}
		conn.Name = "BGP -> " + s.addr
		conn.Direction = "bidirectional"
		conn.Status = "inactive"
		if s.state == "established" {
			conn.Status = "active"
		}
		cp := map[string]any{"vrf": s.vrf}
		if s.state != "" {
			cp["state"] = s.state
		}
		if len(s.afs) > 0 {
			sort.Strings(s.afs)
			cp["address_families"] = s.afs
		}
		if s.desc != "" {
			cp["description"] = s.desc
		}
		if s.local != "" {
			cp["local_address"] = s.local
		}
		conn.Properties = cp
		conns = append(conns, conn)
	}
	return conns, stubs
}
