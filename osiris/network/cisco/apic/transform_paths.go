// transform_paths.go - APIC logical paths, vPC domains and pods. Maps
// fabricPathEp into osiris.cisco.path resources with explicit member
// edges to the ports, port-channels or nodes that realize them,
// fabricExplicitGEp (with its fabricNodePEp members and, at audit
// purpose, the per-leaf vpcDom state) into osiris.cisco.vpc.domain
// resources and fabricPod into physical.pod groups. It also holds the
// path resolver every path consumer shares (static EPG attachments,
// endpoints, L3Out paths and the documentation-purpose port bound).
//
// A path is identified by its exact fabricPathEp DN. A vPC path
// (protpaths) has two leg nodes and no single owner: each leg is a
// separate member edge, to the leg's port-channel when vpcIf and
// vpcRsVpcConf name it, to the leg node otherwise. No path resource is
// emitted for a DN that fabricPathEp did not report and no port or
// cable is inferred for a leg the source data does not name.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package apic

import (
	"sort"
	"strings"

	"go.osirisjson.org/producers/pkg/sdk"
)

// Path, vPC and pod types (namespaced OSIRIS JSON extension types and
// the standard physical.pod group type).
const (
	pathType       = "osiris.cisco.path"
	vpcDomainType  = "osiris.cisco.vpc.domain"
	podGroupType   = "physical.pod"
	connPathMember = "osiris.cisco.path.member"
	connVPCMember  = "osiris.cisco.vpc.member"
	connEPGPath    = "osiris.cisco.epg.path"
	connLearnedOn  = "osiris.cisco.endpoint.learned"
)

// pathRef is a parsed fabricPathEp DN.
//
//	topology/pod-1/paths-101/pathep-[eth1/1]
//	topology/pod-1/paths-101/extpaths-150/pathep-[eth1/1]
//	topology/pod-1/protpaths-101-102/pathep-[LAB-VPC-1]
//	topology/pod-1/protpaths-101-102/extprotpaths-150-151/pathep-[LAB-VPC-1]
type pathRef struct {
	dn    string
	pod   string   // pod number
	nodes []string // one leaf, or the two vPC leg leaves
	fexes []string // FEX ids, in leg order, when the path is on a FEX
	name  string   // the pathep-[...] name
}

// protected reports whether the path is a vPC (two leg nodes).
func (p pathRef) protected() bool { return len(p.nodes) == 2 }

// parsePathDN parses a fabricPathEp DN. ok is false for any other DN.
func parsePathDN(dn string) (pathRef, bool) {
	i := strings.Index(dn, "/pathep-[")
	if i < 0 || !strings.HasSuffix(dn, "]") {
		return pathRef{}, false
	}
	ref := pathRef{dn: dn, name: dn[i+len("/pathep-[") : len(dn)-1]}
	segs := strings.Split(dn[:i], "/")
	if ref.name == "" || len(segs) < 3 || len(segs) > 4 || segs[0] != "topology" {
		return pathRef{}, false
	}
	pod, ok := strings.CutPrefix(segs[1], "pod-")
	if !ok || !digits(pod) {
		return pathRef{}, false
	}
	ref.pod = pod
	if ref.nodes = numberList(segs[2], "paths-", "protpaths-"); ref.nodes == nil {
		return pathRef{}, false
	}
	if len(segs) == 4 {
		ref.fexes = numberList(segs[3], "extpaths-", "extprotpaths-")
		if len(ref.fexes) != len(ref.nodes) {
			return pathRef{}, false
		}
	}
	return ref, true
}

// numberList parses "<single>N" into one number or "<pair>A-B" into
// two, or returns nil.
func numberList(seg, single, pair string) []string {
	if rest, ok := strings.CutPrefix(seg, pair); ok {
		a, b, found := strings.Cut(rest, "-")
		if found && digits(a) && digits(b) {
			return []string{a, b}
		}
		return nil
	}
	if rest, ok := strings.CutPrefix(seg, single); ok && digits(rest) {
		return []string{rest}
	}
	return nil
}

// digits reports whether s is a non-empty run of ASCII digits.
func digits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// pathResolver maps a path DN to the interface that realizes it on one
// leaf. A port-channel path is named after its interface policy group,
// which is the pcAggrIf name on each member leaf; the pcAggrIf id
// ("po1") is accepted as well.
type pathResolver struct {
	numToDN  map[string]string            // node number -> node DN
	pcByName map[string]map[string]string // node DN -> pcAggrIf name -> DN
	pcByID   map[string]map[string]string // node DN -> pcAggrIf id -> DN
}

func newPathResolver(nodes, pcs []map[string]any) *pathResolver {
	r := &pathResolver{
		numToDN:  nodeNumIndex(nodes),
		pcByName: map[string]map[string]string{},
		pcByID:   map[string]map[string]string{},
	}
	add := func(m map[string]map[string]string, node, key, dn string) {
		if key == "" {
			return
		}
		if m[node] == nil {
			m[node] = map[string]string{}
		}
		m[node][key] = dn
	}
	for _, p := range pcs {
		dn := str(p, "dn")
		node := dnPrefix(dn)
		if node == dn {
			continue
		}
		add(r.pcByName, node, str(p, "name"), dn)
		add(r.pcByID, node, str(p, "id"), dn)
	}
	return r
}

// aggregate returns the pcAggrIf DN named name on nodeDN and how it was
// matched ("name" or "id"), or "" when there is none.
func (r *pathResolver) aggregate(nodeDN, name string) (dn, how string) {
	if dn := r.pcByName[nodeDN][name]; dn != "" {
		return dn, "name"
	}
	if dn := r.pcByID[nodeDN][name]; dn != "" {
		return dn, "id"
	}
	return "", ""
}

// singleTarget resolves a single-leaf, non-FEX path to the l1PhysIf DN
// ("port") or pcAggrIf DN ("portchannel") it names. A vPC path returns
// ("", "vpc") and a FEX path ("", "fex"): neither has one owning
// interface on a modeled leaf. ("", "") means unresolved.
func (r *pathResolver) singleTarget(pathDN string) (dn, kind string) {
	ref, ok := parsePathDN(pathDN)
	if !ok {
		return "", ""
	}
	if ref.protected() {
		return "", "vpc"
	}
	if ref.fexes != nil {
		return "", "fex"
	}
	nodeDN, ok := r.numToDN[ref.nodes[0]]
	if !ok {
		return "", ""
	}
	if pc, _ := r.aggregate(nodeDN, ref.name); pc != "" {
		return pc, "portchannel"
	}
	if strings.HasPrefix(ref.name, "eth") {
		return physIfDN(nodeDN, ref.name), "port"
	}
	return "", ""
}

// referencedPaths returns the set of path DNs named by the attachment
// classes: fvRsPathAtt.tDn, fvCEp.fabricPathDn and
// l3extRsPathL3OutAtt.tDn.
func referencedPaths(raw map[string][]map[string]any) map[string]bool {
	refs := map[string]bool{}
	for _, kv := range [][2]string{{"fvRsPathAtt", "tDn"}, {"fvCEp", "fabricPathDn"}, {"l3extRsPathL3OutAtt", "tDn"}} {
		for _, o := range raw[kv[0]] {
			if v := str(o, kv[1]); v != "" {
				refs[v] = true
			}
		}
	}
	return refs
}

// TransformPaths converts fabricPathEp attributes into path resources.
// A path is emitted when it is aggregated (a port-channel or vPC,
// lagT other than not-aggregated) or when refs names it; a plain
// single-port path nothing attaches to would only duplicate its port.
// Returns the resources and the path DN -> parsed path map.
func TransformPaths(pathEps []map[string]any, refs map[string]bool) ([]sdk.Resource, map[string]pathRef) {
	var resources []sdk.Resource
	paths := map[string]pathRef{}
	for _, p := range pathEps {
		dn := str(p, "dn")
		ref, ok := parsePathDN(dn)
		if !ok {
			continue
		}
		lagT := cleanVal(str(p, "lagT"))
		if !refs[dn] && (lagT == "" || lagT == "not-aggregated") {
			continue
		}
		r, err := sdk.NewResource(resourceID(dn), pathType, sdk.Provider{Name: providerName, NativeID: dn, Type: "fabricPathEp"})
		if err != nil {
			continue
		}
		r.Name = ref.name
		if d := str(p, "descr"); d != "" {
			r.Description = d
		}
		props := map[string]any{
			"pod":      ref.pod,
			"node_ids": append([]string(nil), ref.nodes...),
		}
		setProp(props, p, "pathT", "path_type")
		setProp(props, p, "lagT", "aggregation")
		if ref.fexes != nil {
			props["fex_ids"] = append([]string(nil), ref.fexes...)
		}
		r.Properties = props
		paths[dn] = ref
		resources = append(resources, r)
	}
	return resources, paths
}

// pathMemberRefs carries the emitted interfaces a path member edge may
// reference.
type pathMemberRefs struct {
	resolver   *pathResolver
	portDNToID map[string]string            // l1PhysIf DN -> resource ID
	pcIDs      map[string]string            // pcAggrIf DN -> resource ID
	nodeIDs    map[string]bool              // emitted fabric node IDs
	vpcLegs    map[string]map[string]vpcLeg // path DN -> node DN -> leg
	audit      bool
}

// vpcLeg is one leaf's vpcIf for a vPC path: the pcAggrIf it binds
// (vpcRsVpcConf) and its operational state.
type vpcLeg struct {
	aggrDN       string
	localOperSt  string
	remoteOperSt string
}

// indexVPCLegs joins vpcIf (fabricPathDn names the vPC path) with its
// vpcRsVpcConf child (tDn names the leg port-channel, relative to the
// leaf or absolute). Returns path DN -> leaf DN -> leg.
func indexVPCLegs(vpcIfs, confs []map[string]any) map[string]map[string]vpcLeg {
	aggrByIf := map[string]string{}
	for _, c := range confs {
		dn := str(c, "dn")
		ifDN, ok := strings.CutSuffix(dn, "/rsvpcConf")
		if !ok {
			continue
		}
		t := str(c, "tDn")
		switch {
		case t == "":
			continue
		case strings.HasPrefix(t, "topology/"):
			aggrByIf[ifDN] = t
		default:
			aggrByIf[ifDN] = dnPrefix(ifDN) + "/" + t
		}
	}
	legs := map[string]map[string]vpcLeg{}
	for _, v := range vpcIfs {
		dn := str(v, "dn")
		pathDN := str(v, "fabricPathDn")
		node := dnPrefix(dn)
		if pathDN == "" || node == dn {
			continue
		}
		if legs[pathDN] == nil {
			legs[pathDN] = map[string]vpcLeg{}
		}
		legs[pathDN][node] = vpcLeg{
			aggrDN:       aggrByIf[dn],
			localOperSt:  cleanVal(str(v, "localOperSt")),
			remoteOperSt: cleanVal(str(v, "remoteOperSt")),
		}
	}
	return legs
}

// memberConn builds a forward path -> member edge.
func memberConn(typ, source, target string, props map[string]any) (sdk.Connection, bool) {
	if source == "" || target == "" || source == target {
		return sdk.Connection{}, false
	}
	key := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{Type: typ, Direction: "forward", Source: source, Target: target})
	c, err := sdk.NewConnection(sdk.BuildConnectionID(key, 16), typ, source, target)
	if err != nil {
		return sdk.Connection{}, false
	}
	_ = c.SetDirection("forward")
	if len(props) > 0 {
		c.Properties = props
	}
	return c, true
}

// WirePathMembers emits one osiris.cisco.path.member edge per leg of
// every emitted path. A leg resolves, in order, to the port-channel its
// vpcIf binds (resolved_by "vpcIf"), to the port-channel or port the
// path name identifies on that leaf, or to the leaf node itself
// (member_level "node"). A FEX leg is always the leaf node, with the
// FEX id on the edge. A leg whose leaf is not an emitted node is
// counted as unresolved.
func WirePathMembers(paths map[string]pathRef, refs pathMemberRefs, tally policyTally) []sdk.Connection {
	var conns []sdk.Connection
	for _, dn := range sortedKeys(paths) {
		ref := paths[dn]
		for i, num := range ref.nodes {
			nodeDN, ok := refs.resolver.numToDN[num]
			if !ok || !refs.nodeIDs[resourceID(nodeDN)] {
				tally.unresolved["fabricPathEp"]++
				continue
			}
			target, props := refs.legTarget(ref, i, nodeDN)
			if c, ok := memberConn(connPathMember, resourceID(dn), target, props); ok {
				conns = append(conns, c)
			}
		}
	}
	return conns
}

// legTarget picks the member resource for leg i of ref on nodeDN.
func (refs pathMemberRefs) legTarget(ref pathRef, i int, nodeDN string) (string, map[string]any) {
	props := map[string]any{"leg_node_id": ref.nodes[i]}
	if ref.fexes != nil {
		props["member_level"] = "node"
		props["fex_id"] = ref.fexes[i]
		return resourceID(nodeDN), props
	}
	if leg, ok := refs.vpcLegs[ref.dn][nodeDN]; ok {
		if refs.audit {
			if leg.localOperSt != "" {
				props["local_oper_status"] = leg.localOperSt
			}
			if leg.remoteOperSt != "" {
				props["remote_oper_status"] = leg.remoteOperSt
			}
		}
		if id := refs.pcIDs[leg.aggrDN]; id != "" {
			props["member_level"] = "aggregate"
			props["resolved_by"] = "vpcIf"
			return id, props
		}
	}
	if pc, how := refs.resolver.aggregate(nodeDN, ref.name); pc != "" {
		if id := refs.pcIDs[pc]; id != "" {
			props["member_level"] = "aggregate"
			props["resolved_by"] = "pcAggrIf-" + how
			return id, props
		}
	}
	if !ref.protected() && strings.HasPrefix(ref.name, "eth") {
		if id := refs.portDNToID[physIfDN(nodeDN, ref.name)]; id != "" {
			props["member_level"] = "port"
			return id, props
		}
	}
	props["member_level"] = "node"
	return resourceID(nodeDN), props
}

// WireEPGPaths turns every fvRsPathAtt into a forward osiris.cisco.epg.path
// edge from the EPG anchor to the path resource, qualified by the
// relation DN. A relation whose EPG anchor or path was not emitted is
// counted as unresolved; one under another parent kind, as out of
// scope.
func WireEPGPaths(pathAtts []map[string]any, epgAnchors map[string]string, paths map[string]pathRef, tally policyTally) []sdk.Connection {
	var conns []sdk.Connection
	for _, ra := range pathAtts {
		dn := str(ra, "dn")
		i := strings.LastIndex(dn, "/rspathAtt-")
		if i < 0 {
			tally.unresolved["fvRsPathAtt"]++
			continue
		}
		if kind := dnKind(dn[:i]); kind != "epg" {
			tally.skip("fvRsPathAtt", kind)
			continue
		}
		src := epgAnchors[dn[:i]]
		tDn := str(ra, "tDn")
		if _, ok := paths[tDn]; src == "" || !ok {
			tally.unresolved["fvRsPathAtt"]++
			continue
		}
		props := map[string]any{}
		for _, kv := range [][2]string{{"encap", "encap"}, {"primaryEncap", "primary_encap"}, {"mode", "mode"}, {"instrImedcy", "instr_immediacy"}, {"state", "state"}} {
			setProp(props, ra, kv[0], kv[1])
		}
		if c, ok := relationConn(connEPGPath, src, resourceID(tDn), dn, props); ok {
			conns = append(conns, c)
		}
	}
	return conns
}

// WireEndpointPaths emits a forward osiris.cisco.endpoint.learned edge
// from each endpoint to the path it was learned on (fvCEp.fabricPathDn),
// an observation, not ownership. An endpoint without a path is not
// counted; one whose path was not emitted is counted as unresolved.
// Audit purpose only (endpoint resources exist only then).
func WireEndpointPaths(endpoints []map[string]any, paths map[string]pathRef, tally policyTally) []sdk.Connection {
	var conns []sdk.Connection
	for _, ep := range endpoints {
		p := str(ep, "fabricPathDn")
		if p == "" {
			continue
		}
		if _, ok := paths[p]; !ok {
			tally.unresolved["fvCEp"]++
			continue
		}
		props := map[string]any{"semantics": "observed"}
		setProp(props, ep, "encap", "encap")
		if c, ok := memberConn(connLearnedOn, resourceID(str(ep, "dn")), resourceID(p), props); ok {
			conns = append(conns, c)
		}
	}
	return conns
}

// TransformVPCDomains converts fabricExplicitGEp (vPC explicit
// protection groups) into osiris.cisco.vpc.domain resources and emits a
// forward osiris.cisco.vpc.member edge to each leaf its fabricNodePEp
// children name. At audit purpose the edge carries that leaf's vpcDom
// role and peer state. Returns the resources, the edges and the
// sorted leaf-pair key -> domain id map used to label vPC paths.
func TransformVPCDomains(groups, nodePEps, vpcDoms []map[string]any, numToDN map[string]string, nodeIDs map[string]bool, audit bool, tally policyTally) ([]sdk.Resource, []sdk.Connection, map[string]string) {
	domState := map[string]map[string]any{} // vpcDom DN -> object
	for _, d := range vpcDoms {
		domState[str(d, "dn")] = d
	}
	members := map[string][]map[string]any{} // group DN -> fabricNodePEp
	for _, n := range nodePEps {
		dn := str(n, "dn")
		if i := strings.LastIndex(dn, "/nodepep-"); i >= 0 {
			members[dn[:i]] = append(members[dn[:i]], n)
		}
	}

	var resources []sdk.Resource
	var conns []sdk.Connection
	pairs := map[string]string{}
	for _, g := range groups {
		dn := str(g, "dn")
		if !strings.HasPrefix(dn, "uni/fabric/protpol/expgep-") {
			continue
		}
		r, err := sdk.NewResource(resourceID(dn), vpcDomainType, sdk.Provider{Name: providerName, NativeID: dn, Type: "fabricExplicitGEp"})
		if err != nil {
			continue
		}
		r.Name = str(g, "name")
		domainID := str(g, "id")
		props := map[string]any{}
		if domainID != "" {
			props["domain_id"] = domainID
		}
		setProp(props, g, "configSt", "config_state")
		if audit {
			setProp(props, g, "virtualIp", "virtual_ip")
		}
		r.Properties = props
		resources = append(resources, r)

		var leaves []string
		for _, n := range members[dn] {
			num := str(n, "id")
			nodeDN, ok := numToDN[num]
			if !ok || !nodeIDs[resourceID(nodeDN)] {
				tally.unresolved["fabricNodePEp"]++
				continue
			}
			leaves = append(leaves, num)
			eprops := map[string]any{"node_id": num}
			if audit {
				if st := domState[nodeDN+"/sys/vpc/inst/dom-"+domainID]; st != nil {
					setProp(eprops, st, "operRole", "role")
					setProp(eprops, st, "peerSt", "peer_status")
					setProp(eprops, st, "peerStQual", "peer_status_qualifier")
				}
			}
			if c, ok := memberConn(connVPCMember, r.ID, resourceID(nodeDN), eprops); ok {
				conns = append(conns, c)
			}
		}
		if len(leaves) == 2 && domainID != "" {
			pairs[pairKey(leaves[0], leaves[1])] = domainID
		}
	}
	return resources, conns, pairs
}

// pairKey is the order-independent key of a leaf pair.
func pairKey(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return a + "-" + b
}

// LabelVPCPaths records, on every emitted vPC path, the id of the vPC
// domain whose two members are exactly the path's leg leaves.
func LabelVPCPaths(resources []sdk.Resource, paths map[string]pathRef, pairs map[string]string) {
	for i := range resources {
		ref, ok := paths[resources[i].Provider.NativeID]
		if !ok || !ref.protected() || resources[i].Type != pathType {
			continue
		}
		if id := pairs[pairKey(ref.nodes[0], ref.nodes[1])]; id != "" {
			resources[i].Properties["vpc_domain_id"] = id
		}
	}
}

// TransformPods converts fabricPod attributes into physical.pod groups
// whose members are the emitted fabric nodes and paths of that pod.
// Returns the groups and the pod DN -> group ID map.
func TransformPods(pods []map[string]any, members []sdk.Resource) ([]sdk.Group, map[string]string) {
	byPod := map[string][]string{}
	for _, r := range members {
		dn := r.Provider.NativeID
		if !strings.HasPrefix(dn, "topology/pod-") {
			continue
		}
		pod := strings.SplitN(dn, "/", 3)
		byPod[pod[0]+"/"+pod[1]] = append(byPod[pod[0]+"/"+pod[1]], r.ID)
	}
	var groups []sdk.Group
	dnToID := map[string]string{}
	for _, p := range pods {
		dn := str(p, "dn")
		if !strings.HasPrefix(dn, "topology/pod-") {
			continue
		}
		gid := sdk.GroupID(sdk.GroupIDInput{Type: podGroupType, BoundaryToken: dn})
		g, err := sdk.NewGroup(gid, podGroupType)
		if err != nil {
			continue
		}
		g.Name = extractLastSegment(dn)
		props := map[string]any{}
		setProp(props, p, "id", "pod_id")
		setProp(props, p, "podType", "pod_type")
		if len(props) > 0 {
			g.Properties = props
		}
		g.Extensions = map[string]any{extensionNamespace: map[string]any{"dn": dn}}
		ids := byPod[dn]
		sort.Strings(ids)
		g.AddMembers(ids...)
		dnToID[dn] = gid
		groups = append(groups, g)
	}
	return groups, dnToID
}
