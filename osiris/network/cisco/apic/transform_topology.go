// transform_topology.go - APIC physical-fabric resource mapping. Turns
// l1PhysIf (enriched by ethpmPhysIf) into core network.switch.port
// resources and pcAggrIf into aggregate network.switch.port resources.
// The node->port containment, fabric links, LLDP/CDP adjacencies and
// port-channel membership that connect these resources are wired in
// wire.go.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package apic

import (
	"go.osirisjson.org/producers/pkg/sdk"
)

// portType is the OSIRIS resource type for every switch port, physical
// or aggregate (OSIRIS-JSON-v1.0 section 6, network.switch.port).
const portType = "network.switch.port"

// TransformSwitchPorts converts l1PhysIf attributes (admin/config state)
// enriched by the matching ethpmPhysIf object (operational state) into
// network.switch.port resources. When keep is non-nil only ports whose
// DN is a key of keep are emitted the bounded documentation-mode set
// of topology-participating ports; a nil keep emits every port
// (--purpose audit). Returns the resources and a map of l1PhysIf DN ->
// resource ID for the containment and attachment wiring.
func TransformSwitchPorts(l1, ethpm []map[string]any, keep map[string]bool) ([]sdk.Resource, map[string]string) {
	ethpmByParent := make(map[string]map[string]any, len(ethpm))
	for _, e := range ethpm {
		if parent := extractParentDN(str(e, "dn"), "/phys"); parent != "" {
			ethpmByParent[parent] = e
		}
	}

	var resources []sdk.Resource
	dnToID := make(map[string]string)
	for _, p := range l1 {
		dn := str(p, "dn")
		if dn == "" {
			continue
		}
		if keep != nil && !keep[dn] {
			continue
		}

		id := resourceID(dn)
		dnToID[dn] = id

		prov := sdk.Provider{Name: providerName, NativeID: dn, Type: "l1PhysIf"}
		r, err := sdk.NewResource(id, portType, prov)
		if err != nil {
			continue
		}
		name := str(p, "id")
		r.Name = name

		op := ethpmByParent[dn]
		r.Status = mapPortStatus(str(p, "adminSt"), str(op, "operSt"))

		props := map[string]any{
			"interface_name": name,
			"admin_status":   str(p, "adminSt"),
		}
		if v := nodeNumFromDN(dn); v != "" {
			props["node_id"] = v
		}
		if v := str(p, "layer"); v != "" {
			props["layer"] = v
		}
		if v := str(p, "mode"); v != "" {
			props["port_mode"] = v
		}
		if v := str(p, "mtu"); v != "" {
			props["mtu"] = v
		}
		if v := str(p, "usage"); v != "" {
			props["usage"] = v
		}
		if op != nil {
			if v := str(op, "operSt"); v != "" {
				props["oper_status"] = v
			}
			if v := str(op, "operSpeed"); v != "" {
				props["speed"] = v
			}
			if v := str(op, "operDuplex"); v != "" {
				props["duplex"] = v
			}
		}
		r.Properties = props

		ext := map[string]any{}
		if v := str(p, "portT"); v != "" {
			ext["port_type"] = v
		}
		if op != nil {
			if v := str(op, "operStQual"); v != "" && v != "none" {
				ext["oper_status_qualifier"] = v
			}
		}
		if len(ext) > 0 {
			r.Extensions = map[string]any{extensionNamespace: ext}
		}

		resources = append(resources, r)
	}
	return resources, dnToID
}

// TransformPortChannels converts pcAggrIf attributes into aggregate
// network.switch.port resources (properties.aggregate = true). The
// member ports are not listed on pcAggrIf itself; they are wired from
// ethpmPhysIf.bundleIndex by WirePortChannelMembers. Every port-channel
// is emitted in both purposes the set is small (one per configured
// bundle per leaf) and is inherently topology. Returns the resources
// and a map of pcAggrIf DN -> resource ID.
func TransformPortChannels(pc []map[string]any) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	dnToID := make(map[string]string)
	for _, p := range pc {
		dn := str(p, "dn")
		if dn == "" {
			continue
		}

		id := resourceID(dn)
		dnToID[dn] = id

		prov := sdk.Provider{Name: providerName, NativeID: dn, Type: "pcAggrIf"}
		r, err := sdk.NewResource(id, portType, prov)
		if err != nil {
			continue
		}
		name := str(p, "id")
		r.Name = name
		if d := str(p, "descr"); d != "" {
			r.Description = d
		}
		r.Status = mapPortStatus(str(p, "adminSt"), aggrOperSt(p))

		props := map[string]any{
			"interface_name": name,
			"aggregate":      true,
			"admin_status":   str(p, "adminSt"),
		}
		if v := nodeNumFromDN(dn); v != "" {
			props["node_id"] = v
		}
		if v := str(p, "pcMode"); v != "" {
			props["channel_mode"] = v
		}
		if v := str(p, "minLinks"); v != "" {
			props["min_links"] = v
		}
		if v := str(p, "activePorts"); v != "" {
			props["active_ports"] = v
		}
		if v := str(p, "mode"); v != "" {
			props["port_mode"] = v
		}
		if v := str(p, "mtu"); v != "" {
			props["mtu"] = v
		}
		if v := str(p, "usage"); v != "" {
			props["usage"] = v
		}
		r.Properties = props

		if v := str(p, "pcId"); v != "" {
			r.Extensions = map[string]any{extensionNamespace: map[string]any{"pc_id": v}}
		}

		resources = append(resources, r)
	}
	return resources, dnToID
}

// aggrOperSt derives a coarse operational state for a port-channel from
// its active-member count: "up" when at least one member is active,
// "down" otherwise. pcAggrIf carries no operSt field of its own.
func aggrOperSt(p map[string]any) string {
	switch str(p, "activePorts") {
	case "", "0":
		return "down"
	default:
		return "up"
	}
}

// mapPortStatus maps an APIC admin state plus an optional operational
// state to an OSIRIS status. Admin-down is always inactive. With
// admin-up, the operational state decides when it is known; an unknown
// operational state falls back to the admin intent.
func mapPortStatus(adminSt, operSt string) string {
	if adminSt == "down" || adminSt == "disabled" {
		return "inactive"
	}
	switch operSt {
	case "up":
		return "active"
	case "down", "link-down", "channel-admin-down":
		return "inactive"
	case "":
		if adminSt == "up" {
			return "active"
		}
		return "unknown"
	default:
		return "unknown"
	}
}

// topologyPortDNs returns the set of l1PhysIf/pcAggrIf DNs that take
// part in fabric topology and are therefore emitted as resources in
// documentation mode: any port carrying a fabric link, an LLDP or CDP
// neighbour, an EPG path attachment, or a port-channel membership.
// numToDN maps a bare node number to that node's DN.
func topologyPortDNs(fabricLinks, lldp, cdp, pathAtts, ethpm []map[string]any, numToDN map[string]string) map[string]bool {
	keep := make(map[string]bool)

	for _, l := range fabricLinks {
		for _, side := range [][3]string{
			{str(l, "n1"), str(l, "s1"), str(l, "p1")},
			{str(l, "n2"), str(l, "s2"), str(l, "p2")},
		} {
			if dn, ok := linkPortDN(side[0], side[1], side[2], numToDN); ok {
				keep[dn] = true
			}
		}
	}

	for _, a := range lldp {
		if dn := adjLocalPortDN(str(a, "dn")); dn != "" {
			keep[dn] = true
		}
	}
	for _, a := range cdp {
		if dn := adjLocalPortDN(str(a, "dn")); dn != "" {
			keep[dn] = true
		}
	}

	for _, ra := range pathAtts {
		if dn, kind := pathTargetDN(str(ra, "tDn"), numToDN); dn != "" && kind != "vpc" {
			keep[dn] = true
		}
	}

	for _, e := range ethpm {
		bi := str(e, "bundleIndex")
		if bi == "" || bi == "unspecified" {
			continue
		}
		if member := extractParentDN(str(e, "dn"), "/phys"); member != "" {
			keep[member] = true
		}
	}

	return keep
}

// adjLocalPortDN turns an LLDP or CDP adjacency child DN into the
// l1PhysIf DN of the local port the neighbour was seen on.
//
//	topology/pod-1/node-101/sys/lldp/inst/if-[eth1/10]/adj-1
//	-> topology/pod-1/node-101/sys/phys-[eth1/10]
func adjLocalPortDN(adjDN string) string {
	nodeDN := dnPrefix(adjDN)
	port := extractIfToken(adjDN)
	if nodeDN == "" || nodeDN == adjDN || port == "" {
		return ""
	}
	return physIfDN(nodeDN, port)
}

// linkPortDN builds the l1PhysIf DN for one endpoint of a fabricLink,
// given its node number, slot and port. It reports ok=false when the
// node number is not a known fabric node.
func linkPortDN(node, slot, port string, numToDN map[string]string) (string, bool) {
	nodeDN, ok := numToDN[node]
	if !ok || slot == "" || port == "" {
		return "", false
	}
	return physIfDN(nodeDN, "eth"+slot+"/"+port), true
}

// nodeNumIndex maps a bare fabric node number ("101") to that node's DN
// for path and link resolution.
func nodeNumIndex(nodes []map[string]any) map[string]string {
	m := make(map[string]string, len(nodes))
	for _, n := range nodes {
		dn := str(n, "dn")
		if num := nodeNumFromDN(dn); num != "" {
			m[num] = dn
		}
	}
	return m
}

// resourceIDSet collects the IDs of a resource slice for reference
// guarding before a connection or group member is emitted.
func resourceIDSet(rs []sdk.Resource) map[string]bool {
	m := make(map[string]bool, len(rs))
	for _, r := range rs {
		m[r.ID] = true
	}
	return m
}
