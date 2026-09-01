// wire.go - APIC topology wiring. Builds the OSIRIS connections and the
// group membership that join the resources produced by the transform_*
// files: node "contains" port, the merged fabricLink/LLDP/CDP physical
// adjacency graph (with minimal external-neighbour resources), the
// port-channel membership, the bridge-domain "contains" subnet edges,
// and the audit-only endpoint-to-port and EPG-to-path attachments.
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

// WirePortChannelMembers emits a "contains" connection from each
// port-channel resource to its member physical ports, sourced from
// ethpmPhysIf.bundleIndex (the only place APIC reports the membership).
func WirePortChannelMembers(ethpm []map[string]any, pcDNToID, portDNToID map[string]string) []sdk.Connection {
	var conns []sdk.Connection
	for _, e := range ethpm {
		bi := str(e, "bundleIndex")
		if bi == "" || bi == "unspecified" {
			continue
		}
		memberDN := extractParentDN(str(e, "dn"), "/phys")
		if memberDN == "" {
			continue
		}
		memberID, ok := portDNToID[memberDN]
		if !ok {
			continue
		}
		pcID, ok := pcDNToID[aggrIfDN(dnPrefix(memberDN), bi)]
		if !ok {
			continue
		}
		if c, ok := containsConn(pcID, memberID); ok {
			conns = append(conns, c)
		}
	}
	return conns
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
// resource. vPC-attached endpoints span two nodes with no single owning
// resource and are skipped. Audit purpose only (endpoint resources
// exist only then).
func WireEndpointPorts(endpoints []map[string]any, portDNToID, pcDNToID, numToDN map[string]string) []sdk.Connection {
	var conns []sdk.Connection
	for _, ep := range endpoints {
		targetDN, kind := pathTargetDN(str(ep, "fabricPathDn"), numToDN)
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
// each fvRsPathAtt as a member of its EPG group, wiring the
// EPG-to-interface attachment the fabric actually programs. vPC path
// targets are skipped (no single owning resource). Audit purpose only.
func WireEPGPathAttachments(pathAtts []map[string]any, epgDNToID map[string]string, epgGroups []sdk.Group, portDNToID, pcDNToID, numToDN map[string]string) {
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
		targetDN, kind := pathTargetDN(str(ra, "tDn"), numToDN)
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
// one physical.ethernet connection per distinct link, and collects a
// minimal resource for every neighbour that is not an APIC fabric node.
type adjacencyGraph struct {
	numToDN   map[string]string
	nodeIDs   map[string]bool
	nameToID  map[string]string
	links     map[string]*adjacency
	externals map[string]*sdk.Resource
}

// WireFabricAdjacencies merges the fabricLink, lldpAdjEp and cdpAdjEp
// classes into one set of physical.ethernet connections between fabric
// nodes, keeping the observing sources in connection
// properties.discovered_by. A neighbour that is not a fabric node gets
// deterministic minimal network.switch resource
// (id "cisco.apic::external/<name>") so the connection resolves;
// manufacturer/model are set only when CDP explicitly reports them.
func WireFabricAdjacencies(fabricLinks, lldp, cdp, nodes []map[string]any, nodeResources []sdk.Resource) ([]sdk.Resource, []sdk.Connection) {
	g := &adjacencyGraph{
		numToDN:   nodeNumIndex(nodes),
		nodeIDs:   resourceIDSet(nodeResources),
		nameToID:  make(map[string]string, len(nodes)),
		links:     make(map[string]*adjacency),
		externals: make(map[string]*sdk.Resource),
	}
	for _, n := range nodes {
		if nm := str(n, "name"); nm != "" {
			g.nameToID[nm] = resourceID(str(n, "dn"))
		}
	}

	g.addFabricLinks(fabricLinks)
	g.addLLDP(lldp)
	g.addCDP(cdp)
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
	return extResources, conns
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

		remoteID, remotePort := g.remoteFromLLDP(a)
		g.add(localID, localPort, remoteID, remotePort, "lldp", "")
	}
}

// remoteFromLLDP resolves the far side of an LLDP adjacency: an APIC
// fabric node when sysDesc carries its topology DN, otherwise a minimal
// external resource keyed by the neighbour's short system name (or,
// with none, its chassis id).
func (g *adjacencyGraph) remoteFromLLDP(a map[string]any) (id, port string) {
	remotePort := lldpPeerPort(a)
	if rd := str(a, "sysDesc"); strings.HasPrefix(rd, "topology/") && strings.Contains(rd, "/node-") {
		if rid := resourceID(dnPrefix(rd)); g.nodeIDs[rid] {
			return rid, remotePort
		}
	}
	name := str(a, "sysName")
	key := baseSysName(name)
	if key == "" {
		key = cleanVal(str(a, "chassisIdV"))
	}
	if key == "" {
		return "", ""
	}
	props := map[string]any{}
	if v := cleanVal(str(a, "chassisIdV")); v != "" {
		props["chassis_id"] = v
	}
	if v := cleanVal(str(a, "mgmtIp")); v != "" {
		props["management_ip"] = v
	}
	return g.upsertExternal(key, name, "lldp", props), remotePort
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

		remoteID, remotePort := g.remoteFromCDP(a)
		g.add(localID, localPort, remoteID, remotePort, "cdp", "")
	}
}

// remoteFromCDP resolves the far side of a CDP adjacency: a fabric node
// when the reported system name matches one, otherwise a minimal
// external resource. CDP is Cisco-proprietary and its ver string names
// the OS, so manufacturer/model are set for an external CDP neighbour.
func (g *adjacencyGraph) remoteFromCDP(a map[string]any) (id, port string) {
	name := str(a, "sysName")
	remotePort := normalizeIfID(cleanVal(str(a, "portId")))

	if rid, ok := g.nameToID[name]; ok {
		return rid, remotePort
	}
	if rid, ok := g.nameToID[baseSysName(name)]; ok {
		return rid, remotePort
	}

	key := baseSysName(name)
	if key == "" {
		key = str(a, "devId")
	}
	if key == "" {
		return "", ""
	}
	props := map[string]any{}
	if v := str(a, "platId"); v != "" {
		props["model"] = v
	}
	if strings.Contains(str(a, "ver"), "Cisco") {
		props["manufacturer"] = "Cisco"
	}
	if s := parenToken(str(a, "devId")); s != "" {
		props["serial"] = s
	}
	return g.upsertExternal(key, name, "cdp", props), remotePort
}

// upsertExternal creates, or merges an observation into, the external
// neighbour resource for key. discovered_by accumulates the protocols
// that saw it; a later observation fills a property the first left
// blank but never overwrites one.
func (g *adjacencyGraph) upsertExternal(key, name, proto string, props map[string]any) string {
	id := providerName + "::external/" + sanitizeExtKey(key)
	r := g.externals[id]
	if r == nil {
		prov := sdk.Provider{Name: providerName, NativeID: "external/" + sanitizeExtKey(key), Type: "neighbour"}
		nr, err := sdk.NewResource(id, "network.switch", prov)
		if err != nil {
			return ""
		}
		nr.Name = name
		nr.Status = "unknown"
		nr.Properties = map[string]any{"discovered_by": proto}
		nr.Extensions = map[string]any{extensionNamespace: map[string]any{"external": true}}
		g.externals[id] = &nr
		r = &nr
	} else {
		r.Properties["discovered_by"] = mergeCSV(str(r.Properties, "discovered_by"), proto)
		if r.Name == "" {
			r.Name = name
		}
	}
	for k, v := range props {
		if _, present := r.Properties[k]; !present {
			r.Properties[k] = v
		}
	}
	return id
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
