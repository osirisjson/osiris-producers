// transform_policy.go - APIC configured policy intent. Maps application
// profiles (fvAp), contracts (vzBrCP), subjects (vzSubj), filters
// (vzFilter, with vzEntry entries at audit purpose) and the relation
// classes fvRsProv, fvRsCons, vzRsSubjFiltAtt, fvRsBd, fvRsCtx,
// l3extRsEctx and fvRsBDToOut into OSIRIS JSON resources, groups and
// connections. An external EPG (l3extInstP, transform_l3out.go)
// provides and consumes contracts like an application EPG.
//
// The model is configured intent only. An EPG providing and another
// consuming the same contract are linked through the contract resource,
// never to each other: nothing here asserts that traffic is permitted
// or flows. vzAny, taboo contracts, endpoint security groups, contract
// inheritance, service graphs and direction-specific subject terms
// (vzInTerm/vzOutTerm), management EPGs and out-of-band contracts are
// not modeled; policyScope names them in the document. A relation or
// subject whose parent is one of those objects is counted as
// out_of_scope; one whose modeled source or target is missing is
// counted as unresolved.
//
// EPG and VRF stay OSIRIS JSON groups (membership and presentation, IDs
// unchanged). Because a group cannot be a connection endpoint, each EPG
// and VRF also gets a resource anchor (osiris.cisco.policy.epg /
// osiris.cisco.policy.vrf) identified by its native DN. The group's
// osiris.cisco extension names its anchor ("resource"); the anchor is
// not added as a group member, so existing group membership is
// unchanged.
// Every relation edge is forward and qualified by the relation object's
// own DN, so two relations between the same objects never collapse.
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

// Policy resource types (namespaced OSIRIS JSON extension types).
const (
	policyEPGType      = "osiris.cisco.policy.epg"
	policyVRFType      = "osiris.cisco.policy.vrf"
	policyContractType = "osiris.cisco.policy.contract"
	policySubjectType  = "osiris.cisco.policy.subject"
	policyFilterType   = "osiris.cisco.policy.filter"
)

// Policy connection types (namespaced OSIRIS JSON extension types).
const (
	connProvides      = "osiris.cisco.contract.provides"
	connConsumes      = "osiris.cisco.contract.consumes"
	connSubjectFilter = "osiris.cisco.subject.filter"
	connEPGToBD       = "osiris.cisco.epg.bd"
	connBDToVRF       = "osiris.cisco.bd.vrf"
	connL3OutToVRF    = "osiris.cisco.l3out.vrf"
	connBDToL3Out     = "osiris.cisco.bd.l3out"
)

// commonTenantDN is the tenant whose contracts and filters every tenant
// can reference by name when no local object has that name.
const commonTenantDN = "uni/tn-common"

// maxFilterEntries bounds the entries carried on one filter resource.
// A filter with more is emitted with the first maxFilterEntries entries
// in DN order and entries_truncated set.
const maxFilterEntries = 256

// policyScope is the fixed description of what the policy model covers,
// attached to the controller resources when policy classes were
// collected, so a consumer never reads absent mechanisms as "no policy".
var policyScope = map[string]any{
	"semantics": "configured-intent",
	"not_modeled": []string{
		"contract-inheritance", "direction-specific-subject-terms", "endpoint-security-groups",
		"management-epgs", "out-of-band-contracts", "service-graphs",
		"taboo-contracts", "vzany",
	},
}

// parentKinds maps the APIC DN naming prefix of a parent object the
// policy model does not cover to its policyScope "not_modeled" name.
var parentKinds = map[string]string{
	"esg":    "endpoint-security-groups",
	"inb":    "management-epgs",
	"oob":    "management-epgs",
	"oobbrc": "out-of-band-contracts",
}

// dnKind returns the naming prefix of the last DN segment
// ("uni/tn-A/ap-p/epg-e" -> "epg"), or "" when it is not a short
// alphanumeric token.
func dnKind(dn string) string {
	seg := dn[strings.LastIndex(dn, "/")+1:]
	i := strings.IndexByte(seg, '-')
	if i <= 0 || i > 24 {
		return ""
	}
	for _, r := range seg[:i] {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return ""
		}
	}
	return seg[:i]
}

// scopeName returns the policyScope name for an unmodeled parent kind,
// or "other" for a kind without one.
func scopeName(kind string) string {
	if n, ok := parentKinds[kind]; ok {
		return n
	}
	return "other"
}

// policyTally counts, per APIC class, the objects that produced no
// output: unresolved (a modeled source or target is not in the
// document) and out of scope (the parent is an object the model does
// not cover), the latter also by scope name for diagnostics.
type policyTally struct {
	unresolved map[string]int
	outOfScope map[string]map[string]int
}

func newPolicyTally() policyTally {
	return policyTally{unresolved: map[string]int{}, outOfScope: map[string]map[string]int{}}
}

func (t policyTally) skip(class, kind string) {
	if t.outOfScope[class] == nil {
		t.outOfScope[class] = map[string]int{}
	}
	t.outOfScope[class][scopeName(kind)]++
}

// policyIndex resolves policy objects by DN or by tenant-scoped name.
type policyIndex struct {
	dnToID map[string]string            // object DN -> resource ID
	byName map[string]map[string]string // tenant DN -> object name -> object DN
}

func newPolicyIndex() policyIndex {
	return policyIndex{dnToID: map[string]string{}, byName: map[string]map[string]string{}}
}

func (x policyIndex) add(dn, name, id string) {
	x.dnToID[dn] = id
	t := extractTenantDN(dn)
	if x.byName[t] == nil {
		x.byName[t] = map[string]string{}
	}
	x.byName[t][name] = dn
}

// resolve returns the resource ID a relation targets and how it was
// resolved. A non-empty tDn is authoritative: it resolves only to that
// exact object. Without a tDn, name resolves in the relation's own
// tenant first, then in tenant common. ok is false when the target is
// not an emitted object.
func (x policyIndex) resolve(relDN, tDn, name string) (id, how string, ok bool) {
	if tDn != "" {
		id, ok = x.dnToID[tDn]
		return id, "tdn", ok
	}
	if name == "" {
		return "", "", false
	}
	if dn, found := x.byName[extractTenantDN(relDN)][name]; found {
		return x.dnToID[dn], "name-local", true
	}
	if dn, found := x.byName[commonTenantDN][name]; found {
		return x.dnToID[dn], "name-common", true
	}
	return "", "", false
}

// policyResource builds a policy resource identified by its native DN.
func policyResource(obj map[string]any, typ, class string) (sdk.Resource, bool) {
	dn := str(obj, "dn")
	r, err := sdk.NewResource(resourceID(dn), typ, sdk.Provider{Name: providerName, NativeID: dn, Type: class})
	if err != nil {
		return sdk.Resource{}, false
	}
	r.Name = str(obj, "name")
	if d := str(obj, "descr"); d != "" {
		r.Description = d
	}
	return r, true
}

// setProp sets props[key] to the APIC value of attr unless it is an
// APIC "no value" sentinel.
func setProp(props map[string]any, obj map[string]any, attr, key string) {
	if v := cleanVal(str(obj, attr)); v != "" {
		props[key] = v
	}
}

// relationConn builds a forward connection of type typ from source to
// target, qualified by the relation object's DN.
func relationConn(typ, source, target, relDN string, props map[string]any) (sdk.Connection, bool) {
	if source == "" || target == "" || source == target {
		return sdk.Connection{}, false
	}
	key := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
		Type:       typ,
		Direction:  "forward",
		Source:     source,
		Target:     target,
		Qualifiers: map[string]string{"relation": relDN},
	})
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

// TransformAppProfiles converts fvAp attributes into
// logical.application groups. Returns the groups and a map of
// application-profile DN -> group ID.
func TransformAppProfiles(apps []map[string]any) ([]sdk.Group, map[string]string) {
	var groups []sdk.Group
	dnToID := make(map[string]string, len(apps))
	for _, a := range apps {
		dn := str(a, "dn")
		gid := sdk.GroupID(sdk.GroupIDInput{Type: "logical.application", BoundaryToken: dn})
		g, err := sdk.NewGroup(gid, "logical.application")
		if err != nil {
			continue
		}
		dnToID[dn] = gid
		g.Name = str(a, "name")
		if d := str(a, "descr"); d != "" {
			g.Description = d
		}
		g.Extensions = map[string]any{extensionNamespace: map[string]any{"dn": dn}}
		groups = append(groups, g)
	}
	return groups, dnToID
}

// WireAppProfiles nests each application group under its tenant group
// and each EPG group under its application group. It returns the DNs of
// the EPGs placed under an application, so the caller nests only the
// remaining EPGs (application profiles not collected) directly under
// their tenant.
func WireAppProfiles(apps []map[string]any, appDNToID map[string]string, appGroups []sdk.Group,
	epgAttrs []map[string]any, epgDNToID, tenantDNToID map[string]string, tenantGroups []sdk.Group) map[string]bool {
	tIdx := groupIndex(tenantGroups)
	for _, a := range apps {
		dn := str(a, "dn")
		appID, ok := appDNToID[dn]
		if !ok {
			continue
		}
		if i, ok := tIdx[tenantDNToID[extractTenantDN(dn)]]; ok {
			tenantGroups[i].AddChildren(appID)
		}
	}

	placed := map[string]bool{}
	aIdx := groupIndex(appGroups)
	for _, e := range epgAttrs {
		dn := str(e, "dn")
		epgID, ok := epgDNToID[dn]
		if !ok {
			continue
		}
		i := strings.LastIndex(dn, "/epg-")
		if i < 0 {
			continue
		}
		if gi, ok := aIdx[appDNToID[dn[:i]]]; ok {
			appGroups[gi].AddChildren(epgID)
			placed[dn] = true
		}
	}
	return placed
}

// TransformPolicyAnchors creates the resource anchor for every EPG and
// VRF group and records it in the group's osiris.cisco extension.
// Returns the anchors and the EPG and VRF DN -> anchor ID maps.
func TransformPolicyAnchors(epgAttrs []map[string]any, epgDNToID map[string]string, epgGroups []sdk.Group,
	vrfAttrs []map[string]any, vrfDNToID map[string]string, vrfGroups []sdk.Group) ([]sdk.Resource, map[string]string, map[string]string) {
	var resources []sdk.Resource
	anchor := func(objs []map[string]any, groupIDs map[string]string, groups []sdk.Group, typ, class string) map[string]string {
		idx := groupIndex(groups)
		out := map[string]string{}
		for _, o := range objs {
			dn := str(o, "dn")
			gi, ok := idx[groupIDs[dn]]
			if !ok {
				continue
			}
			r, ok := policyResource(o, typ, class)
			if !ok {
				continue
			}
			out[dn] = r.ID
			resources = append(resources, r)
			ensureCiscoExtension(&groups[gi].Extensions)
			groups[gi].Extensions[extensionNamespace].(map[string]any)["resource"] = r.ID
		}
		return out
	}
	epgAnchors := anchor(epgAttrs, epgDNToID, epgGroups, policyEPGType, "fvAEPg")
	vrfAnchors := anchor(vrfAttrs, vrfDNToID, vrfGroups, policyVRFType, "fvCtx")
	return resources, epgAnchors, vrfAnchors
}

// TransformContracts converts vzBrCP attributes into contract resources.
func TransformContracts(contracts []map[string]any) ([]sdk.Resource, policyIndex) {
	idx := newPolicyIndex()
	var resources []sdk.Resource
	for _, c := range contracts {
		r, ok := policyResource(c, policyContractType, "vzBrCP")
		if !ok {
			continue
		}
		props := map[string]any{}
		setProp(props, c, "scope", "scope")
		setProp(props, c, "prio", "priority")
		if len(props) > 0 {
			r.Properties = props
		}
		idx.add(str(c, "dn"), r.Name, r.ID)
		resources = append(resources, r)
	}
	return resources, idx
}

// TransformSubjects converts vzSubj attributes into subject resources,
// each contained by its parent contract. A subject of an object other
// than a contract (an out-of-band contract) is counted in tally as out
// of scope; one whose contract was not emitted, as unresolved.
func TransformSubjects(subjects []map[string]any, contracts policyIndex, tally policyTally) ([]sdk.Resource, map[string]string, []sdk.Connection) {
	var resources []sdk.Resource
	var conns []sdk.Connection
	dnToID := map[string]string{}
	for _, s := range subjects {
		dn := str(s, "dn")
		i := strings.LastIndex(dn, "/subj-")
		if i < 0 {
			tally.unresolved["vzSubj"]++
			continue
		}
		if kind := dnKind(dn[:i]); kind != "brc" {
			tally.skip("vzSubj", kind)
			continue
		}
		contractID, ok := contracts.dnToID[dn[:i]]
		if !ok {
			tally.unresolved["vzSubj"]++
			continue
		}
		r, ok := policyResource(s, policySubjectType, "vzSubj")
		if !ok {
			continue
		}
		props := map[string]any{}
		setProp(props, s, "revFltPorts", "reverse_filter_ports")
		setProp(props, s, "prio", "priority")
		if len(props) > 0 {
			r.Properties = props
		}
		dnToID[dn] = r.ID
		resources = append(resources, r)
		if c, ok := containsConn(contractID, r.ID); ok {
			conns = append(conns, c)
		}
	}
	return resources, dnToID, conns
}

// TransformFilters converts vzFilter attributes into filter resources.
// With entries (vzEntry, audit purpose only) each filter carries its
// entries as an ordered, allowlisted properties.entries list.
func TransformFilters(filters, entries []map[string]any) ([]sdk.Resource, policyIndex) {
	byFilter := map[string][]map[string]any{}
	for _, e := range entries {
		dn := str(e, "dn")
		if i := strings.LastIndex(dn, "/e-"); i >= 0 {
			byFilter[dn[:i]] = append(byFilter[dn[:i]], e)
		}
	}

	idx := newPolicyIndex()
	var resources []sdk.Resource
	for _, f := range filters {
		r, ok := policyResource(f, policyFilterType, "vzFilter")
		if !ok {
			continue
		}
		dn := str(f, "dn")
		if es := byFilter[dn]; len(es) > 0 {
			sort.Slice(es, func(i, j int) bool { return str(es[i], "dn") < str(es[j], "dn") })
			props := map[string]any{}
			if len(es) > maxFilterEntries {
				es = es[:maxFilterEntries]
				props["entries_truncated"] = true
			}
			list := make([]map[string]any, 0, len(es))
			for _, e := range es {
				list = append(list, filterEntry(e))
			}
			props["entries"] = list
			r.Properties = props
		}
		idx.add(dn, r.Name, r.ID)
		resources = append(resources, r)
	}
	return resources, idx
}

// filterEntry renders one vzEntry with an explicit attribute allowlist.
func filterEntry(e map[string]any) map[string]any {
	m := map[string]any{}
	for _, kv := range [][2]string{
		{"name", "name"},
		{"etherT", "ether_type"},
		{"prot", "ip_protocol"},
		{"sFromPort", "source_from_port"},
		{"sToPort", "source_to_port"},
		{"dFromPort", "destination_from_port"},
		{"dToPort", "destination_to_port"},
		{"stateful", "stateful"},
		{"tcpRules", "tcp_flags"},
		{"applyToFrag", "apply_to_fragments"},
		{"icmpv4T", "icmpv4_type"},
		{"icmpv6T", "icmpv6_type"},
		{"arpOpc", "arp_opcode"},
	} {
		setProp(m, e, kv[0], kv[1])
	}
	return m
}

// policyRefs carries the emitted objects relation edges may reference.
type policyRefs struct {
	epgAnchors  map[string]string // EPG DN -> anchor ID
	extEPGs     map[string]string // external EPG (l3extInstP) DN -> resource ID
	vrfAnchors  map[string]string // VRF DN -> anchor ID
	bdDNToID    map[string]string
	l3outDNToID map[string]string
	l3outs      policyIndex // L3Out DN / tenant-scoped name -> resource ID
	contracts   policyIndex
	subjects    map[string]string // subject DN -> resource ID
	filters     policyIndex
}

// relationSpec describes one APIC relation class wired into edges.
type relationSpec struct {
	class    string
	connType string
	marker   string // DN segment that starts the relation under its parent
	// kindOf extracts the kind of the relation's parent object (dnKind
	// of the parent DN); sources maps each modeled parent kind to the
	// lookup of the edge's source resource.
	kindOf  func(parentDN string) string
	sources map[string]func(parentDN string) string
	target  func(rel map[string]any) (id, how string, ok bool)
	props   [][2]string // attribute allowlist for connection properties
}

// WirePolicyRelations turns the relation classes into forward edges.
// raw holds the collected class objects. A relation under a parent the
// model does not cover (a management EPG, an endpoint security group,
// an out-of-band contract subject) is counted in tally as out of
// scope; one whose modeled source or target is not an emitted object,
// as unresolved. Neither produces an edge.
func WirePolicyRelations(raw map[string][]map[string]any, refs policyRefs, tally policyTally) []sdk.Connection {
	byDN := func(m map[string]string) func(map[string]any) (string, string, bool) {
		return func(rel map[string]any) (string, string, bool) {
			id, ok := m[str(rel, "tDn")]
			return id, "tdn", ok
		}
	}
	contractTarget := func(rel map[string]any) (string, string, bool) {
		return refs.contracts.resolve(str(rel, "dn"), str(rel, "tDn"), str(rel, "tnVzBrCPName"))
	}
	stateProps := [][2]string{{"state", "state"}}
	// subjectOwnerKind is the kind of a subject's parent (a contract
	// "brc" or an out-of-band contract "oobbrc").
	subjectOwnerKind := func(subjDN string) string {
		if i := strings.LastIndex(subjDN, "/subj-"); i >= 0 {
			return dnKind(subjDN[:i])
		}
		return ""
	}
	lookup := func(m map[string]string) func(string) string { return func(p string) string { return m[p] } }
	epgOrExternal := map[string]func(string) string{"epg": lookup(refs.epgAnchors), "instP": lookup(refs.extEPGs)}
	specs := []relationSpec{
		{"fvRsProv", connProvides, "/rsprov-", dnKind, epgOrExternal, contractTarget,
			[][2]string{{"state", "state"}, {"matchT", "match_type"}, {"prio", "priority"}}},
		{"fvRsCons", connConsumes, "/rscons-", dnKind, epgOrExternal, contractTarget,
			[][2]string{{"state", "state"}, {"prio", "priority"}}},
		{"vzRsSubjFiltAtt", connSubjectFilter, "/rssubjFiltAtt-", subjectOwnerKind, map[string]func(string) string{"brc": lookup(refs.subjects)},
			func(rel map[string]any) (string, string, bool) {
				return refs.filters.resolve(str(rel, "dn"), str(rel, "tDn"), str(rel, "tnVzFilterName"))
			},
			[][2]string{{"state", "state"}, {"action", "action"}, {"directives", "directives"}}},
		{"fvRsBd", connEPGToBD, "/rsbd", dnKind, map[string]func(string) string{"epg": lookup(refs.epgAnchors)}, byDN(refs.bdDNToID), stateProps},
		{"fvRsCtx", connBDToVRF, "/rsctx", dnKind, map[string]func(string) string{"BD": lookup(refs.bdDNToID)}, byDN(refs.vrfAnchors), stateProps},
		{"l3extRsEctx", connL3OutToVRF, "/rsectx", dnKind, map[string]func(string) string{"out": lookup(refs.l3outDNToID)}, byDN(refs.vrfAnchors), stateProps},
		{"fvRsBDToOut", connBDToL3Out, "/rsBDToOut-", dnKind, map[string]func(string) string{"BD": lookup(refs.bdDNToID)},
			func(rel map[string]any) (string, string, bool) {
				return refs.l3outs.resolve(str(rel, "dn"), str(rel, "tDn"), str(rel, "tnL3extOutName"))
			}, stateProps},
	}

	var conns []sdk.Connection
	for _, sp := range specs {
		for _, rel := range raw[sp.class] {
			dn := str(rel, "dn")
			i := strings.LastIndex(dn, sp.marker)
			if i < 0 {
				tally.unresolved[sp.class]++
				continue
			}
			source, modeled := sp.sources[sp.kindOf(dn[:i])]
			if !modeled {
				tally.skip(sp.class, sp.kindOf(dn[:i]))
				continue
			}
			src := source(dn[:i])
			tgt, how, ok := sp.target(rel)
			if src == "" || !ok {
				tally.unresolved[sp.class]++
				continue
			}
			props := map[string]any{}
			for _, kv := range sp.props {
				setProp(props, rel, kv[0], kv[1])
			}
			if how != "tdn" {
				props["resolved_by"] = how
			}
			if c, ok := relationConn(sp.connType, src, tgt, dn, props); ok {
				conns = append(conns, c)
			}
		}
	}
	return conns
}

// WirePolicyToTenants adds contract, subject, filter, external EPG and
// policy prefix resources as members of their tenant group.
func WirePolicyToTenants(resources []sdk.Resource, tenantDNToID map[string]string, tenantGroups []sdk.Group) {
	idx := groupIndex(tenantGroups)
	for _, r := range resources {
		switch r.Type {
		case policyContractType, policySubjectType, policyFilterType, l3outEPGType, l3outPrefixType:
		default:
			continue
		}
		if i, ok := idx[tenantDNToID[extractTenantDN(r.Provider.NativeID)]]; ok {
			tenantGroups[i].AddMembers(r.ID)
		}
	}
}
