// wire.go - APIC topology wiring. Builds the OSIRIS connections and the
// group membership that join the resources produced by the transform_*
// files: node "contains" port, the merged fabricLink/LLDP/CDP physical
// adjacency graph (with minimal external-neighbour resources identified
// by the LLDP chassis id or CDP device id), the port-channel
// membership, the bridge-domain "contains" subnet edges and the
// audit-only endpoint-to-port and EPG-to-interface group membership.
// Logical path edges are in transform_paths.go.
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

// containsConn builds a forward "contains" connection from a parent
// resource to a child resource, with a deterministic ID. The second
// return is false when the SDK rejects the endpoints.
func containsConn(parentID, childID string) (sdk.Connection, bool) {
	if parentID == "" || childID == "" || parentID == childID {
		return sdk.Connection{}, false
	}
	key := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
		Type:      "contains",
		Direction: "forward",
		Source:    parentID,
		Target:    childID,
	})
	c, err := sdk.NewConnection(sdk.BuildConnectionID(key, 16), "contains", parentID, childID)
	if err != nil {
		return sdk.Connection{}, false
	}
	_ = c.SetDirection("forward")
	return c, true
}

// WireNodePorts emits a "contains" connection from each fabric node to
// every switch port (physical or aggregate) it owns. nodeIDs guards the
// parent reference so a port on a node that was not emitted is skipped
// rather than producing a dangling connection.
func WireNodePorts(portDNToID map[string]string, nodeIDs map[string]bool) []sdk.Connection {
	var conns []sdk.Connection
	for portDN, portID := range portDNToID {
		nodeID := resourceID(dnPrefix(portDN))
		if !nodeIDs[nodeID] {
			continue
		}
		if c, ok := containsConn(nodeID, portID); ok {
			conns = append(conns, c)
		}
	}
	return conns
}

// pcMember is one port-channel membership: the pcAggrIf DN and the
// l1PhysIf DN of the member port and the class that reported it.
type pcMember struct {
	pcDN, portDN, source string
}

// portChannelMembers returns the port-channel memberships reported by
// pcRsMbrIfs (the pcAggrIf -> member relation, primary) and by
// ethpmPhysIf.bundleIndex (the member's operational bundle, fallback),
// deduplicated by port-channel and port in a stable order.
// A pcRsMbrIfs target is a relative "sys/phys-[...]" DN, an absolute DN
// or, without tDn, the tSKey port id.
func portChannelMembers(ethpm, mbrIfs []map[string]any) []pcMember {
	seen := map[[2]string]bool{}
	var out []pcMember
	add := func(pc, port, source string) {
		k := [2]string{pc, port}
		if pc == "" || port == "" || seen[k] {
			return
		}
		seen[k] = true
		out = append(out, pcMember{pc, port, source})
	}
	for _, m := range mbrIfs {
		dn := str(m, "dn")
		i := strings.Index(dn, "/rsmbrIfs-")
		if i < 0 {
			continue
		}
		pc := dn[:i]
		node := dnPrefix(pc)
		if node == pc {
			continue
		}
		port := ""
		switch t := str(m, "tDn"); {
		case strings.HasPrefix(t, "topology/"):
			port = t
		case strings.HasPrefix(t, "sys/phys-["):
			port = node + "/" + t
		case str(m, "tSKey") != "":
			port = physIfDN(node, str(m, "tSKey"))
		}
		add(pc, port, "pcRsMbrIfs")
	}
	for _, e := range ethpm {
		bi := str(e, "bundleIndex")
		if bi == "" || bi == "unspecified" {
			continue
		}
		if port := extractParentDN(str(e, "dn"), "/phys"); port != "" {
			add(aggrIfDN(dnPrefix(port), bi), port, "ethpmPhysIf")
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].pcDN != out[j].pcDN {
			return out[i].pcDN < out[j].pcDN
		}
		return out[i].portDN < out[j].portDN
	})
	return out
}

// WirePortChannelMembers emits a "contains" connection from each
// port-channel resource to its member physical ports (see
// portChannelMembers). It returns the number of pcRsMbrIfs relations
// whose port-channel or member port was not emitted; a bundleIndex
// observation without an emitted pair is skipped silently, as an
// unbundled port is not an error.
func WirePortChannelMembers(ethpm, mbrIfs []map[string]any, pcDNToID, portDNToID map[string]string) ([]sdk.Connection, int) {
	var conns []sdk.Connection
	unresolved := 0
	for _, m := range portChannelMembers(ethpm, mbrIfs) {
		pcID, okPC := pcDNToID[m.pcDN]
		memberID, okPort := portDNToID[m.portDN]
		if !okPC || !okPort {
			if m.source == "pcRsMbrIfs" {
				unresolved++
			}
			continue
		}
		if c, ok := containsConn(pcID, memberID); ok {
			conns = append(conns, c)
		}
	}
	return conns, unresolved
}

// WireBDSubnets emits a "contains" connection from each bridge-domain
// resource to the subnet resources configured directly under it.
// Subnets under an EPG or an L3Out are left to their existing
// tenant-group membership.
func WireBDSubnets(subnets []map[string]any, bdDNToID map[string]string) []sdk.Connection {
	var conns []sdk.Connection
	for _, s := range subnets {
		dn := str(s, "dn")
		i := strings.Index(dn, "/subnet-")
		if i < 0 {
			continue
		}
		parentDN := dn[:i]
		if !strings.Contains(parentDN, "/BD-") {
			continue
		}
		bdID, ok := bdDNToID[parentDN]
		if !ok {
			continue
		}
		if c, ok := containsConn(bdID, resourceID(dn)); ok {
			conns = append(conns, c)
		}
	}
	return conns
}

// WireEndpointPorts emits a "contains" connection from the switch port
// (or port-channel) an endpoint was learned on to that endpoint
// resource. vPC and FEX-attached endpoints have no single owning
// interface on a modeled leaf and are skipped here; their path is
// wired by WireEndpointPaths.
// Audit purpose only (endpoint resources exist only then).
func WireEndpointPorts(endpoints []map[string]any, portDNToID, pcDNToID map[string]string, paths *pathResolver) []sdk.Connection {
	var conns []sdk.Connection
	for _, ep := range endpoints {
		targetDN, kind := paths.singleTarget(str(ep, "fabricPathDn"))
		if targetDN == "" {
			continue
		}
		var targetID string
		switch kind {
		case "port":
			targetID = portDNToID[targetDN]
		case "portchannel":
			targetID = pcDNToID[targetDN]
		}
		if targetID == "" {
			continue
		}
		if c, ok := containsConn(targetID, resourceID(str(ep, "dn"))); ok {
			conns = append(conns, c)
		}
	}
	return conns
}

// WireEPGPathAttachments adds the switch port or port-channel named by
// each fvRsPathAtt as a member of its EPG group (presentation). vPC and
// FEX path targets have no single owning interface and are skipped,
// the attachment edge itself is WireEPGPaths. Audit purpose only.
func WireEPGPathAttachments(pathAtts []map[string]any, epgDNToID map[string]string, epgGroups []sdk.Group, portDNToID, pcDNToID map[string]string, paths *pathResolver) {
	idx := groupIndex(epgGroups)
	for _, ra := range pathAtts {
		dn := str(ra, "dn")
		i := strings.Index(dn, "/rspathAtt-")
		if i < 0 {
			continue
		}
		epgID, ok := epgDNToID[dn[:i]]
		if !ok {
			continue
		}
		targetDN, kind := paths.singleTarget(str(ra, "tDn"))
		if targetDN == "" {
			continue
		}
		var memberID string
		switch kind {
		case "port":
			memberID = portDNToID[targetDN]
		case "portchannel":
			memberID = pcDNToID[targetDN]
		}
		if memberID == "" {
			continue
		}
		if gi, ok := idx[epgID]; ok {
			epgGroups[gi].AddMembers(memberID)
		}
	}
}

// adjacency is one merged physical link between two endpoints, ordered
// so (aID, aPort) <= (bID, bPort) regardless of which side observed it.
type adjacency struct {
	aID, bID     string
	aPort, bPort string
	sources      map[string]struct{}
	linkState    string
}

// adjacencyGraph accumulates fabricLink, LLDP and CDP observations into
// one physical.ethernet connection per distinct link and collects a
// minimal resource for every neighbour that is not an APIC fabric node.
type adjacencyGraph struct {
	numToDN     map[string]string
	nodeIDs     map[string]bool
	nameToID    map[string]string // emitted fabric node name -> resource ID
	hiddenNames map[string]bool   // hiddenNames are fabric node names with no emitted resource.
	links       map[string]*adjacency
	externals   map[string]*sdk.Resource
	unresolved  int // unresolved counts CDP observations skipped because they name a fabric node without an emitted resource.
	obs         []neighbourObs
}

// WireFabricAdjacencies merges the fabricLink, lldpAdjEp and cdpAdjEp
// classes into one set of physical.ethernet connections between fabric
// nodes, keeping the observing sources in connection
// properties.discovered_by. A neighbour that is not a fabric node gets
// a deterministic minimal network.switch resource identified by its
// typed source identifier (see resolveExternals) so the connection
// resolves; manufacturer/model are set only when CDP explicitly
// reports them.
//
// A CDP neighbour whose system name matches a fabric node that is not
// in nodeResources (an unsupported role) is never wired: the edge would
// reference a resource the document does not contain and the node is
// not an external device either. Such observations are skipped and
// returned as the unresolved count.
func WireFabricAdjacencies(fabricLinks, lldp, cdp, nodes []map[string]any, nodeResources []sdk.Resource) ([]sdk.Resource, []sdk.Connection, int) {
	g := &adjacencyGraph{
		numToDN:     nodeNumIndex(nodes),
		nodeIDs:     resourceIDSet(nodeResources),
		nameToID:    make(map[string]string, len(nodes)),
		hiddenNames: make(map[string]bool),
		links:       make(map[string]*adjacency),
		externals:   make(map[string]*sdk.Resource),
	}
	for _, n := range nodes {
		nm := str(n, "name")
		if nm == "" {
			continue
		}
		if id := resourceID(str(n, "dn")); g.nodeIDs[id] {
			g.nameToID[nm] = id
		} else {
			g.hiddenNames[nm] = true
		}
	}

	g.addFabricLinks(fabricLinks)
	g.addLLDP(lldp)
	g.addCDP(cdp)
	g.resolveExternals()
	g.reconcile()

	extResources := make([]sdk.Resource, 0, len(g.externals))
	for _, r := range g.externals {
		extResources = append(extResources, *r)
	}

	conns := make([]sdk.Connection, 0, len(g.links))
	for _, e := range g.links {
		srcs := make([]string, 0, len(e.sources))
		for s := range e.sources {
			srcs = append(srcs, s)
		}
		sort.Strings(srcs)

		key := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
			Type:       "physical.ethernet",
			Direction:  "bidirectional",
			Source:     e.aID,
			Target:     e.bID,
			Qualifiers: map[string]string{"a_port": e.aPort, "b_port": e.bPort},
		})
		c, err := sdk.NewConnection(sdk.BuildConnectionID(key, 16), "physical.ethernet", e.aID, e.bID)
		if err != nil {
			continue
		}
		props := map[string]any{"discovered_by": strings.Join(srcs, ",")}
		if e.aPort != "" {
			props["source_port"] = e.aPort
		}
		if e.bPort != "" {
			props["target_port"] = e.bPort
		}
		if e.linkState != "" {
			props["link_state"] = e.linkState
		}
		c.Properties = props
		conns = append(conns, c)
	}
	return extResources, conns, g.unresolved
}

// add records one observation of a link between two endpoints.
func (g *adjacencyGraph) add(aID, aPort, bID, bPort, source, linkState string) {
	if aID == "" || bID == "" || aID == bID {
		return
	}
	if aID > bID || (aID == bID && aPort > bPort) {
		aID, bID = bID, aID
		aPort, bPort = bPort, aPort
	}
	k := aID + "\x00" + aPort + "\x00" + bID + "\x00" + bPort
	e := g.links[k]
	if e == nil {
		e = &adjacency{aID: aID, bID: bID, aPort: aPort, bPort: bPort, sources: map[string]struct{}{}}
		g.links[k] = e
	}
	e.sources[source] = struct{}{}
	if linkState != "" && e.linkState == "" {
		e.linkState = linkState
	}
}

// reconcile folds every partially-resolved adjacency (one endpoint port
// unknown, e.g. an APIC controller seen over its management LLDP) into
// the single fully-resolved adjacency for the same node pair and known
// port, when exactly one exists. This makes the merge independent of
// order fabricLink, LLDP and CDP were processed in. Parallel links
// between the same node pair stay separate because they differ on the
// known port.
func (g *adjacencyGraph) reconcile() {
	for k, e := range g.links {
		if e.aPort != "" && e.bPort != "" {
			continue
		}
		host := g.resolvedSibling(e)
		if host == nil {
			continue
		}
		for s := range e.sources {
			host.sources[s] = struct{}{}
		}
		if host.linkState == "" {
			host.linkState = e.linkState
		}
		delete(g.links, k)
	}
}

// resolvedSibling returns the unique fully-resolved adjacency that
// matches p on both node IDs and on every port p does know, or nil when
// there is no match or more than one.
func (g *adjacencyGraph) resolvedSibling(p *adjacency) *adjacency {
	var found *adjacency
	n := 0
	for _, e := range g.links {
		if e == p || e.aID != p.aID || e.bID != p.bID || e.aPort == "" || e.bPort == "" {
			continue
		}
		if p.aPort != "" && e.aPort != p.aPort {
			continue
		}
		if p.bPort != "" && e.bPort != p.bPort {
			continue
		}
		found = e
		n++
	}
	if n == 1 {
		return found
	}
	return nil
}

func (g *adjacencyGraph) addFabricLinks(links []map[string]any) {
	for _, l := range links {
		aDN, aOK := g.numToDN[str(l, "n1")]
		bDN, bOK := g.numToDN[str(l, "n2")]
		if !aOK || !bOK {
			continue
		}
		aID, bID := resourceID(aDN), resourceID(bDN)
		if !g.nodeIDs[aID] || !g.nodeIDs[bID] {
			continue
		}
		g.add(aID, "eth"+str(l, "s1")+"/"+str(l, "p1"),
			bID, "eth"+str(l, "s2")+"/"+str(l, "p2"),
			"fabricLink", str(l, "linkState"))
	}
}

func (g *adjacencyGraph) addLLDP(adjs []map[string]any) {
	for _, a := range adjs {
		dn := str(a, "dn")
		localDN := dnPrefix(dn)
		if localDN == "" || localDN == dn {
			continue
		}
		localID := resourceID(localDN)
		if !g.nodeIDs[localID] {
			continue
		}
		localPort := extractIfToken(dn)
		remotePort := lldpPeerPort(a)

		// A fabric node announces its own topology DN in sysDesc.
		if rd := str(a, "sysDesc"); strings.HasPrefix(rd, "topology/") && strings.Contains(rd, "/node-") {
			if rid := resourceID(dnPrefix(rd)); g.nodeIDs[rid] {
				g.add(localID, localPort, rid, remotePort, "lldp", "")
				continue
			}
		}

		name := str(a, "sysName")
		o := neighbourObs{
			dn: dn, proto: "lldp", localID: localID, localPort: localPort,
			remotePort: remotePort, name: name, legacyKey: baseSysName(name),
			props: map[string]any{},
		}
		if o.legacyKey == "" {
			o.legacyKey = cleanVal(str(a, "chassisIdV"))
		}
		chassisT, chassisV := cleanVal(str(a, "chassisIdT")), cleanVal(str(a, "chassisIdV"))
		switch {
		case chassisV != "":
			if chassisT == "" {
				chassisT = "unknown"
			}
			o.key = "lldp/" + chassisT + "/" + chassisV
			o.basis = "lldp-chassis-id"
			o.props["chassis_id"] = chassisV
			o.props["chassis_id_type"] = chassisT
		case baseSysName(name) != "":
			o.key = "lldp-name/" + baseSysName(name)
			o.basis = "lldp-system-name"
		default:
			continue
		}
		if v := cleanVal(str(a, "mgmtIp")); v != "" {
			o.props["management_ip"] = v
		}
		g.obs = append(g.obs, o)
	}
}

// lldpPeerPort returns the neighbour's port id, or "" when LLDP reported
// it as a MAC address (portIdT == "mac") rather than an interface name.
func lldpPeerPort(a map[string]any) string {
	if str(a, "portIdT") == "mac" {
		return ""
	}
	return normalizeIfID(cleanVal(str(a, "portIdV")))
}

// cleanVal maps the APIC "no value" sentinels to the empty string so
// they never reach an emitted property.
func cleanVal(s string) string {
	switch s {
	case "", "unspecified", "not-applicable", "0.0.0.0", "::", "0.0.0.0/0":
		return ""
	default:
		return s
	}
}

func (g *adjacencyGraph) addCDP(adjs []map[string]any) {
	for _, a := range adjs {
		dn := str(a, "dn")
		localDN := dnPrefix(dn)
		if localDN == "" || localDN == dn {
			continue
		}
		localID := resourceID(localDN)
		if !g.nodeIDs[localID] {
			continue
		}
		localPort := extractIfToken(dn)
		name := str(a, "sysName")
		remotePort := normalizeIfID(cleanVal(str(a, "portId")))

		// CDP carries no topology DN: a fabric node is matched by its
		// reported system name.
		if rid, ok := g.nameToID[name]; ok {
			g.add(localID, localPort, rid, remotePort, "cdp", "")
			continue
		}
		if rid, ok := g.nameToID[baseSysName(name)]; ok {
			g.add(localID, localPort, rid, remotePort, "cdp", "")
			continue
		}
		if g.hiddenNames[name] || g.hiddenNames[baseSysName(name)] {
			g.unresolved++
			continue
		}

		devID := str(a, "devId")
		o := neighbourObs{
			dn: dn, proto: "cdp", localID: localID, localPort: localPort,
			remotePort: remotePort, name: name, legacyKey: baseSysName(name),
			props: map[string]any{},
		}
		if o.legacyKey == "" {
			o.legacyKey = devID
		}
		switch {
		case devID != "":
			o.key = "cdp/" + devID
			o.basis = "cdp-device-id"
			o.props["cdp_device_id"] = devID
		case baseSysName(name) != "":
			o.key = "cdp-name/" + baseSysName(name)
			o.basis = "cdp-system-name"
		default:
			continue
		}
		// CDP is Cisco-proprietary and its ver string names the OS, so
		// manufacturer/model are set for an external CDP neighbour.
		if v := str(a, "platId"); v != "" {
			o.props["model"] = v
		}
		if strings.Contains(str(a, "ver"), "Cisco") {
			o.props["manufacturer"] = "Cisco"
		}
		if s := parenToken(devID); s != "" {
			o.props["serial"] = s
		}
		g.obs = append(g.obs, o)
	}
}

// neighbourObs is one LLDP or CDP observation of a neighbour that is
// not an emitted fabric node.
type neighbourObs struct {
	dn                 string // adjacency DN
	proto              string // "lldp" or "cdp"
	localID, localPort string
	remotePort         string
	name               string // reported system name
	key                string // typed identity key ("lldp/mac/<mac>", "cdp/<device id>", ...)
	basis              string // identity_basis property value
	legacyKey          string // the 0.3.0 identity input (short system name)
	props              map[string]any
}

// resolveExternals assigns every external observation its neighbour
// resource and records the link. Identity is the typed source
// identifier: the LLDP chassis id (type and value), otherwise the CDP
// device id, otherwise the reported system name, never a shortened
// name. A CDP observation folds into an LLDP neighbour only when both
// were seen on the same local port and report the same system name,
// and its device id correlates with exactly one LLDP chassis across the
// fabric. Observations on one local port that report different system
// names stay separate neighbours, each flagged identity_conflict.
// The result does not depend on input order.
func (g *adjacencyGraph) resolveExternals() {
	sort.SliceStable(g.obs, func(i, j int) bool {
		if g.obs[i].proto != g.obs[j].proto {
			return g.obs[i].proto > g.obs[j].proto // lldp before cdp
		}
		return g.obs[i].dn < g.obs[j].dn
	})

	byPort := map[string][]int{}
	for i, o := range g.obs {
		lp := o.localID + "\x00" + o.localPort
		byPort[lp] = append(byPort[lp], i)
	}
	cdpToLLDP := map[string]map[string]bool{}
	conflicts := map[string]map[string]bool{}
	flag := func(a, b string) {
		for _, p := range [][2]string{{a, b}, {b, a}} {
			if conflicts[p[0]] == nil {
				conflicts[p[0]] = map[string]bool{}
			}
			conflicts[p[0]][p[1]] = true
		}
	}
	for _, lp := range sortedKeys(byPort) {
		var lldp, cdp []neighbourObs
		for _, i := range byPort[lp] {
			if g.obs[i].proto == "lldp" {
				lldp = append(lldp, g.obs[i])
			} else {
				cdp = append(cdp, g.obs[i])
			}
		}
		for _, l := range lldp {
			for _, c := range cdp {
				if strings.EqualFold(baseSysName(l.name), baseSysName(c.name)) && baseSysName(l.name) != "" {
					if cdpToLLDP[c.key] == nil {
						cdpToLLDP[c.key] = map[string]bool{}
					}
					cdpToLLDP[c.key][l.key] = true
				} else if l.key != c.key {
					flag(l.key, c.key)
				}
			}
		}
	}

	final := func(key string) string {
		if m := cdpToLLDP[key]; len(m) == 1 {
			for k := range m {
				return k
			}
		}
		return key
	}
	for key, m := range cdpToLLDP {
		if len(m) > 1 {
			for l := range m {
				flag(key, l)
			}
		}
	}

	for _, o := range g.obs {
		key := final(o.key)
		id := g.upsertExternal(key, o)
		g.add(o.localID, o.localPort, id, o.remotePort, o.proto, "")
	}
	for key, others := range conflicts {
		r := g.externals[externalID(final(key))]
		if r == nil {
			continue
		}
		ext := r.Extensions[extensionNamespace].(map[string]any)
		ids := []string{}
		if prev, ok := ext["identity_conflict"].([]string); ok {
			ids = prev
		}
		for k := range others {
			if oid := externalID(final(k)); oid != r.ID {
				ids = append(ids, oid)
			}
		}
		if len(ids) > 0 {
			ext["identity_conflict"] = sortedUnique(ids)
		}
	}
}

// externalID is the resource ID of the external neighbour with the
// typed identity key ("<source>/<type>/<value>" or "<source>/<value>").
func externalID(key string) string {
	return providerName + "::" + externalNativeID(key)
}

// externalNativeID slugs each segment of a typed identity key: the
// source and type segments keep their "/" separators, a value never
// introduces a new one.
func externalNativeID(key string) string {
	n := 2
	if strings.HasPrefix(key, "lldp/") {
		n = 3
	}
	segs := strings.SplitN(key, "/", n)
	for i := range segs {
		segs[i] = sanitizeExtKey(segs[i])
	}
	return "external/" + strings.Join(segs, "/")
}

// upsertExternal creates, or merges an observation into, the external
// neighbour resource for key. discovered_by accumulates the protocols
// that saw it; a later observation fills a property the first left
// blank but never overwrites one.
func (g *adjacencyGraph) upsertExternal(key string, o neighbourObs) string {
	id := externalID(key)
	r := g.externals[id]
	if r == nil {
		prov := sdk.Provider{Name: providerName, NativeID: externalNativeID(key), Type: "neighbour"}
		nr, err := sdk.NewResource(id, "network.switch", prov)
		if err != nil {
			return ""
		}
		nr.Name = o.name
		nr.Status = "unknown"
		nr.Properties = map[string]any{"discovered_by": o.proto, "identity_basis": o.basis}
		nr.Extensions = map[string]any{extensionNamespace: map[string]any{"external": true}}
		g.externals[id] = &nr
		r = &nr
	} else {
		r.Properties["discovered_by"] = mergeCSV(str(r.Properties, "discovered_by"), o.proto)
		if r.Name == "" {
			r.Name = o.name
		}
	}
	for k, v := range o.props {
		if _, present := r.Properties[k]; !present {
			r.Properties[k] = v
		}
	}
	if o.legacyKey != "" {
		ext := r.Extensions[extensionNamespace].(map[string]any)
		legacy, _ := ext["legacy_ids"].([]string)
		ext["legacy_ids"] = sortedUnique(append(legacy, providerName+"::external/"+sanitizeExtKey(o.legacyKey)))
	}
	return id
}

// sortedUnique returns the sorted, deduplicated values of s.
func sortedUnique(s []string) []string {
	set := map[string]struct{}{}
	for _, v := range s {
		set[v] = struct{}{}
	}
	return sortedKeys(set)
}

// baseSysName trims a DNS suffix from a reported system name so LLDP's
// FQDN and CDP's short name for the same neighbour merge.
func baseSysName(s string) string {
	if i := strings.IndexByte(s, '.'); i > 0 {
		return s[:i]
	}
	return s
}

// parenToken returns the text inside the first "(...)" of s, used to
// pull a serial out of a CDP devId like "LAB-SW04(TST0000001)".
func parenToken(s string) string {
	i := strings.IndexByte(s, '(')
	if i < 0 {
		return ""
	}
	rest := s[i+1:]
	j := strings.IndexByte(rest, ')')
	if j < 0 {
		return ""
	}
	return rest[:j]
}

// sanitizeExtKey reduces a neighbour name to a stable id-safe slug.
func sanitizeExtKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '.', r == ':':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// mergeCSV adds v to a sorted, deduplicated comma-separated list.
func mergeCSV(list, v string) string {
	set := map[string]struct{}{}
	if list != "" {
		for _, p := range strings.Split(list, ",") {
			set[p] = struct{}{}
		}
	}
	set[v] = struct{}{}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}
