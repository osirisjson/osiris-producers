// transform_l3out.go - APIC L3Out external connectivity. Maps external
// EPGs (l3extInstP) and their policy prefixes (l3extSubnet) into
// OSIRIS JSON resources contained by their L3Out and the L3Out node
// and path attachments (l3extRsNodeL3OutAtt, l3extRsPathL3OutAtt) into
// forward edges from the L3Out to the fabric node and to the logical
// path resource (transform_paths.go). The bridge-domain -> L3Out
// relation (fvRsBDToOut) and the external EPG provides/consumes
// relations are wired with the other policy relations in
// transform_policy.go.
//
// The model is configured intent only. A policy prefix is the
// classification and route-control intent configured on an external
// EPG, never a statement that the route is installed or advertised; an
// attachment is the configured node or routed interface, never a
// statement that a routing session is established. The logical node
// and interface profiles are not resources: their names are carried on
// the attachment edges. l3outScope names what is not modeled.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package apic

import (
	"net"
	"strings"

	"go.osirisjson.org/producers/pkg/sdk"
)

// L3Out resource and connection types (namespaced OSIRIS JSON
// extension types).
const (
	l3outEPGType    = "osiris.cisco.l3out.epg"
	l3outPrefixType = "osiris.cisco.l3out.prefix"
	connL3OutNode   = "osiris.cisco.l3out.node"
	connL3OutPath   = "osiris.cisco.l3out.path"
)

// l3outScope is the fixed description of what the L3Out model covers,
// attached to the controller resources when L3Out classes were
// collected.
var l3outScope = map[string]any{
	"semantics": "configured-intent",
	"not_modeled": []string{
		"bgp-ospf-eigrp-peers", "installed-routes", "route-control-profiles",
		"vpc-svi-side-addresses",
	},
}

// l3outClasses are the classes that make up the L3Out slice.
var l3outClasses = []string{"l3extInstP", "l3extSubnet", "l3extRsNodeL3OutAtt", "l3extRsPathL3OutAtt", "fvRsBDToOut"}

// TransformExternalEPGs converts l3extInstP attributes into external
// EPG resources, each contained by its L3Out. An external EPG whose
// L3Out was not emitted is counted as unresolved. Returns the
// resources, the external EPG DN -> resource ID map and the contains
// edges.
func TransformExternalEPGs(instPs []map[string]any, l3outDNToID map[string]string, tally policyTally) ([]sdk.Resource, map[string]string, []sdk.Connection) {
	var resources []sdk.Resource
	var conns []sdk.Connection
	dnToID := map[string]string{}
	for _, e := range instPs {
		dn := str(e, "dn")
		i := strings.LastIndex(dn, "/instP-")
		if i < 0 {
			tally.unresolved["l3extInstP"]++
			continue
		}
		l3outID, ok := l3outDNToID[dn[:i]]
		if !ok {
			tally.unresolved["l3extInstP"]++
			continue
		}
		r, ok := policyResource(e, l3outEPGType, "l3extInstP")
		if !ok {
			continue
		}
		props := map[string]any{}
		setProp(props, e, "prefGrMemb", "preferred_group")
		setProp(props, e, "prio", "qos_class")
		if len(props) > 0 {
			r.Properties = props
		}
		dnToID[dn] = r.ID
		resources = append(resources, r)
		if c, ok := containsConn(l3outID, r.ID); ok {
			conns = append(conns, c)
		}
	}
	return resources, dnToID, conns
}

// TransformL3OutPrefixes converts the l3extSubnet objects configured
// under an external EPG into policy prefix resources contained by that
// external EPG. The prefix is kept as configured (an IPv4 or IPv6
// network, never expanded); scope and aggregate keep the ACI flags. A
// prefix under another parent (a fabric external connection profile)
// is counted as out of scope; one whose external EPG was not emitted,
// as unresolved.
func TransformL3OutPrefixes(subnets []map[string]any, extEPGs map[string]string, tally policyTally) ([]sdk.Resource, []sdk.Connection) {
	var resources []sdk.Resource
	var conns []sdk.Connection
	for _, s := range subnets {
		dn := str(s, "dn")
		i := strings.LastIndex(dn, "/extsubnet-[")
		if i < 0 {
			tally.unresolved["l3extSubnet"]++
			continue
		}
		if kind := dnKind(dn[:i]); kind != "instP" {
			tally.skip("l3extSubnet", kind)
			continue
		}
		epgID, ok := extEPGs[dn[:i]]
		if !ok {
			tally.unresolved["l3extSubnet"]++
			continue
		}
		r, err := sdk.NewResource(resourceID(dn), l3outPrefixType, sdk.Provider{Name: providerName, NativeID: dn, Type: "l3extSubnet"})
		if err != nil {
			continue
		}
		ip := str(s, "ip")
		r.Name = ip
		if n := str(s, "name"); n != "" {
			r.Name = n
		}
		if d := str(s, "descr"); d != "" {
			r.Description = d
		}
		props := map[string]any{}
		if _, ipnet, err := net.ParseCIDR(ip); err == nil {
			props["cidr"] = ipnet.String()
		} else if ip != "" {
			props["prefix"] = ip
		}
		setProp(props, s, "scope", "scope")
		setProp(props, s, "aggregate", "aggregate")
		r.Properties = props
		resources = append(resources, r)
		if c, ok := containsConn(epgID, r.ID); ok {
			conns = append(conns, c)
		}
	}
	return resources, conns
}

// l3outProfiles splits an L3Out child relation DN into its L3Out DN and
// the names of the logical node and (optional) interface profiles it
// sits under.
//
//	uni/tn-A/out-o/lnodep-n/rsnodeL3OutAtt-[...]       -> uni/tn-A/out-o, n, ""
//	uni/tn-A/out-o/lnodep-n/lifp-i/rspathL3OutAtt-[...] -> uni/tn-A/out-o, n, i
func l3outProfiles(relDN, marker string) (l3outDN, nodeProfile, ifProfile string, ok bool) {
	i := strings.LastIndex(relDN, marker)
	if i < 0 {
		return "", "", "", false
	}
	parent := relDN[:i]
	if j := strings.LastIndex(parent, "/lifp-"); j >= 0 {
		ifProfile = parent[j+len("/lifp-"):]
		parent = parent[:j]
	}
	j := strings.LastIndex(parent, "/lnodep-")
	if j < 0 {
		return "", "", "", false
	}
	return parent[:j], parent[j+len("/lnodep-"):], ifProfile, true
}

// l3outAttachRefs carries what the L3Out attachment edges may
// reference.
type l3outAttachRefs struct {
	l3outDNToID map[string]string
	nodeIDs     map[string]bool
	paths       map[string]pathRef
	audit       bool
}

// WireL3OutAttachments emits a forward osiris.cisco.l3out.node edge for
// every l3extRsNodeL3OutAtt (L3Out -> fabric node) and a forward
// osiris.cisco.l3out.path edge for every l3extRsPathL3OutAtt (L3Out ->
// logical path), each qualified by the relation DN and carrying the
// node and interface profile names. Router ids and interface addresses
// are audit purpose only. A relation whose L3Out, node or path was not
// emitted is counted as unresolved.
func WireL3OutAttachments(nodeAtts, pathAtts []map[string]any, refs l3outAttachRefs, tally policyTally) []sdk.Connection {
	var conns []sdk.Connection
	for _, a := range nodeAtts {
		dn := str(a, "dn")
		l3outDN, nodeProfile, _, ok := l3outProfiles(dn, "/rsnodeL3OutAtt-")
		src := refs.l3outDNToID[l3outDN]
		tgt := resourceID(str(a, "tDn"))
		if !ok || src == "" || !refs.nodeIDs[tgt] {
			tally.unresolved["l3extRsNodeL3OutAtt"]++
			continue
		}
		props := map[string]any{"node_profile": nodeProfile}
		if refs.audit {
			setProp(props, a, "rtrId", "router_id")
			setProp(props, a, "rtrIdLoopBack", "router_id_loopback")
		}
		if c, ok := relationConn(connL3OutNode, src, tgt, dn, props); ok {
			conns = append(conns, c)
		}
	}
	for _, a := range pathAtts {
		dn := str(a, "dn")
		l3outDN, nodeProfile, ifProfile, ok := l3outProfiles(dn, "/rspathL3OutAtt-")
		src := refs.l3outDNToID[l3outDN]
		tDn := str(a, "tDn")
		if _, emitted := refs.paths[tDn]; !ok || src == "" || !emitted {
			tally.unresolved["l3extRsPathL3OutAtt"]++
			continue
		}
		props := map[string]any{"node_profile": nodeProfile}
		if ifProfile != "" {
			props["interface_profile"] = ifProfile
		}
		for _, kv := range [][2]string{{"ifInstT", "interface_type"}, {"encap", "encap"}, {"encapScope", "encap_scope"}, {"mode", "mode"}, {"mtu", "mtu"}} {
			setProp(props, a, kv[0], kv[1])
		}
		if refs.audit {
			setProp(props, a, "addr", "address")
		}
		if c, ok := relationConn(connL3OutPath, src, resourceID(tDn), dn, props); ok {
			conns = append(conns, c)
		}
	}
	return conns
}

// l3outIndex indexes L3Outs by DN and tenant-scoped name for
// fvRsBDToOut resolution.
func l3outIndex(l3outs []map[string]any, l3outDNToID map[string]string) policyIndex {
	idx := newPolicyIndex()
	for _, o := range l3outs {
		dn := str(o, "dn")
		if id, ok := l3outDNToID[dn]; ok {
			idx.add(dn, str(o, "name"), id)
		}
	}
	return idx
}
