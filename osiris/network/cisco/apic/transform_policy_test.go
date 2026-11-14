// transform_policy_test.go - Unit tests for the Cisco ACI/APIC
// configured policy intent transform: contract and filter name
// resolution (tDn, local tenant, tenant common), relation edge
// identity, the filter-entry allowlist and bound, subject ownership,
// application-profile nesting and the EPG/VRF resource anchors.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package apic

import (
	"fmt"
	"testing"

	"go.osirisjson.org/producers/pkg/sdk"
)

// TestPolicyIndex_Resolve proves the contract/filter lookup order: a
// tDn is authoritative (a tDn naming an absent object never falls back
// to the name), then the relation's own tenant, then tenant common.
func TestPolicyIndex_Resolve(t *testing.T) {
	idx := newPolicyIndex()
	idx.add("uni/tn-A/brc-web", "web", "id-A-web")
	idx.add("uni/tn-B/brc-web", "web", "id-B-web")
	idx.add("uni/tn-common/brc-web", "web", "id-common-web")
	idx.add("uni/tn-common/brc-dns", "dns", "id-common-dns")

	rel := "uni/tn-A/ap-p/epg-e/rscons-x"
	cases := []struct {
		name, relDN, tDn, cname string
		wantID, wantHow         string
		wantOK                  bool
	}{
		{"tDn wins over name", rel, "uni/tn-B/brc-web", "web", "id-B-web", "tdn", true},
		{"absent tDn is not rescued by name", rel, "uni/tn-A/brc-gone", "web", "", "tdn", false},
		{"local tenant before common", rel, "", "web", "id-A-web", "name-local", true},
		{"common fallback", rel, "", "dns", "id-common-dns", "name-common", true},
		{"other tenant never matches", "uni/tn-C/ap-p/epg-e/rscons-x", "", "web", "id-common-web", "name-common", true},
		{"unknown name", rel, "", "nope", "", "", false},
		{"no name, no tDn", rel, "", "", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id, how, ok := idx.resolve(tc.relDN, tc.tDn, tc.cname)
			if id != tc.wantID || how != tc.wantHow || ok != tc.wantOK {
				t.Errorf("resolve = (%q, %q, %v), want (%q, %q, %v)", id, how, ok, tc.wantID, tc.wantHow, tc.wantOK)
			}
		})
	}
}

// TestWirePolicyRelations proves: relations become forward edges from
// the EPG anchor to the contract, two relation objects between the same
// pair stay two edges with distinct IDs, a relation under an object the
// model does not cover (an external or management EPG) is counted out
// of scope by kind and a relation whose target is absent is counted
// unresolved; neither emits an edge.
func TestWirePolicyRelations(t *testing.T) {
	contracts := newPolicyIndex()
	contracts.add("uni/tn-A/brc-web", "web", resourceID("uni/tn-A/brc-web"))
	epg := "uni/tn-A/ap-p/epg-e"
	refs := policyRefs{
		epgAnchors: map[string]string{epg: resourceID(epg)},
		contracts:  contracts,
		filters:    newPolicyIndex(),
		subjects:   map[string]string{},
	}
	raw := map[string][]map[string]any{
		"fvRsProv": {
			{"dn": epg + "/rsprov-web", "tDn": "uni/tn-A/brc-web", "tnVzBrCPName": "web", "state": "formed", "matchT": "AtleastOne"},
			{"dn": "uni/tn-A/out-o/instP-x/rsprov-web", "tDn": "uni/tn-A/brc-web", "tnVzBrCPName": "web"},
		},
		"fvRsCons": {
			{"dn": epg + "/rscons-web", "tDn": "uni/tn-A/brc-web", "state": "formed"},
			{"dn": epg + "/rscons-gone", "tDn": "uni/tn-A/brc-gone", "state": "missing-target"},
			{"dn": "uni/tn-mgmt/mgmtp-default/inb-i/rscons-web", "tnVzBrCPName": "web"},
		},
		"vzRsSubjFiltAtt": {
			{"dn": "uni/tn-mgmt/oobbrc-o/subj-s/rssubjFiltAtt-f", "tnVzFilterName": "f"},
		},
	}
	tally := newPolicyTally()
	conns := WirePolicyRelations(raw, refs, tally)
	if len(conns) != 2 {
		t.Fatalf("expected 2 edges, got %d: %+v", len(conns), conns)
	}
	if conns[0].ID == conns[1].ID {
		t.Error("provides and consumes edges between the same pair share an ID")
	}
	for _, c := range conns {
		if c.Direction != "forward" || c.Source != resourceID(epg) || c.Target != resourceID("uni/tn-A/brc-web") {
			t.Errorf("edge %s: %s %s -> %s", c.Type, c.Direction, c.Source, c.Target)
		}
	}
	if p := conns[0].Properties; p["state"] != "formed" || p["match_type"] != "AtleastOne" || p["resolved_by"] != nil {
		t.Errorf("provides properties = %v", p)
	}
	if fmt.Sprint(tally.unresolved) != "map[fvRsCons:1]" {
		t.Errorf("unresolved = %v, want fvRsCons:1", tally.unresolved)
	}
	want := "map[fvRsCons:map[management-epgs:1] fvRsProv:map[external-epgs:1] vzRsSubjFiltAtt:map[out-of-band-contracts:1]]"
	if got := fmt.Sprint(tally.outOfScope); got != want {
		t.Errorf("outOfScope = %s, want %s", got, want)
	}

	// The same relation class twice between one pair (two relation
	// objects) yields two distinct edges.
	a, _ := relationConn(connConsumes, "s", "t", "uni/tn-A/x/rscons-1", nil)
	b, _ := relationConn(connConsumes, "s", "t", "uni/tn-A/x/rscons-2", nil)
	if a.ID == b.ID {
		t.Error("relation DN does not qualify the connection identity")
	}
	if _, ok := relationConn(connConsumes, "s", "s", "x", nil); ok {
		t.Error("self-referencing relation must not produce an edge")
	}
}

// TestTransformFilters_EntryAllowlistAndBound proves: only allowlisted
// vzEntry attributes reach the document (an unexpected attribute, even
// one shaped like a secret, is dropped), APIC sentinels are omitted,
// entries are ordered by DN and more than maxFilterEntries entries are
// truncated with entries_truncated set.
func TestTransformFilters_EntryAllowlistAndBound(t *testing.T) {
	filters := []map[string]any{
		{"dn": "uni/tn-A/flt-f", "name": "f"},
		{"dn": "uni/tn-A/flt-big", "name": "big"},
		{"dn": "uni/tn-A/flt-none", "name": "none"},
	}
	entries := []map[string]any{
		{"dn": "uni/tn-A/flt-f/e-b", "name": "b", "prot": "udp", "dFromPort": "unspecified", "secretAttr": "CANARY-ENTRY"},
		{"dn": "uni/tn-A/flt-f/e-a", "name": "a", "prot": "tcp", "dFromPort": "443"},
	}
	for i := range maxFilterEntries + 5 {
		entries = append(entries, map[string]any{"dn": fmt.Sprintf("uni/tn-A/flt-big/e-%04d", i), "name": fmt.Sprintf("%04d", i)})
	}
	res, idx := TransformFilters(filters, entries)
	byName := map[string]sdk.Resource{}
	for _, r := range res {
		byName[r.Name] = r
	}

	f := byName["f"].Properties["entries"].([]map[string]any)
	if len(f) != 2 || f[0]["name"] != "a" || f[1]["name"] != "b" {
		t.Fatalf("entries not ordered by DN: %v", f)
	}
	if f[0]["destination_from_port"] != "443" || f[1]["destination_from_port"] != nil {
		t.Errorf("port mapping / sentinel handling: %v", f)
	}
	if fmt.Sprint(f[1]) != fmt.Sprint(map[string]any{"ip_protocol": "udp", "name": "b"}) {
		t.Errorf("non-allowlisted attribute reached the entry: %v", f[1])
	}

	big := byName["big"].Properties
	if len(big["entries"].([]map[string]any)) != maxFilterEntries || big["entries_truncated"] != true {
		t.Errorf("big filter: %d entries, truncated=%v", len(big["entries"].([]map[string]any)), big["entries_truncated"])
	}
	if byName["none"].Properties != nil {
		t.Errorf("filter without entries carries properties: %v", byName["none"].Properties)
	}
	if _, _, ok := idx.resolve("uni/tn-A/brc-c/subj-s/rssubjFiltAtt-f", "", "f"); !ok {
		t.Error("filter not indexed by name")
	}
}

// TestTransformSubjects_RequiresContract proves: a subject is emitted
// and contained by its contract only when the contract was emitted;
// a subject of a missing contract or with an unparsable DN is counted
// unresolved and a subject of an out-of-band contract out of scope.
func TestTransformSubjects_RequiresContract(t *testing.T) {
	contracts := newPolicyIndex()
	contracts.add("uni/tn-A/brc-c", "c", resourceID("uni/tn-A/brc-c"))
	tally := newPolicyTally()
	res, dnToID, conns := TransformSubjects([]map[string]any{
		{"dn": "uni/tn-A/brc-c/subj-s", "name": "s", "revFltPorts": "yes", "prio": "unspecified"},
		{"dn": "uni/tn-A/brc-gone/subj-s", "name": "s"},
		{"dn": "uni/tn-A/odd", "name": "odd"},
		{"dn": "uni/tn-mgmt/oobbrc-o/subj-s", "name": "s"},
	}, contracts, tally)
	if len(res) != 1 || len(conns) != 1 || tally.unresolved["vzSubj"] != 2 || dnToID["uni/tn-A/brc-c/subj-s"] == "" {
		t.Fatalf("res=%d conns=%d unresolved=%v", len(res), len(conns), tally.unresolved)
	}
	if tally.outOfScope["vzSubj"]["out-of-band-contracts"] != 1 {
		t.Errorf("outOfScope = %v", tally.outOfScope)
	}
	if res[0].Properties["reverse_filter_ports"] != "yes" || res[0].Properties["priority"] != nil {
		t.Errorf("subject properties = %v", res[0].Properties)
	}
	if c := conns[0]; c.Type != "contains" || c.Source != resourceID("uni/tn-A/brc-c") {
		t.Errorf("subject containment = %+v", c)
	}
}

// TestWireAppProfiles proves: application groups nest under their
// tenant and EPGs under their application; an EPG whose application
// profile was not collected is reported unplaced so it stays under its
// tenant.
func TestWireAppProfiles(t *testing.T) {
	tenants, tenantIDs := TransformTenants([]map[string]any{{"dn": "uni/tn-A", "name": "A"}})
	apps, appIDs := TransformAppProfiles([]map[string]any{{"dn": "uni/tn-A/ap-p", "name": "p", "descr": "app"}})
	epgAttrs := []map[string]any{{"dn": "uni/tn-A/ap-p/epg-e1", "name": "e1"}, {"dn": "uni/tn-A/ap-q/epg-e2", "name": "e2"}}
	_, epgIDs := TransformEPGs(epgAttrs)

	placed := WireAppProfiles([]map[string]any{{"dn": "uni/tn-A/ap-p"}}, appIDs, apps, epgAttrs, epgIDs, tenantIDs, tenants)
	if !placed["uni/tn-A/ap-p/epg-e1"] || placed["uni/tn-A/ap-q/epg-e2"] {
		t.Errorf("placed = %v", placed)
	}
	if len(tenants[0].Children) != 1 || tenants[0].Children[0] != appIDs["uni/tn-A/ap-p"] {
		t.Errorf("tenant children = %v", tenants[0].Children)
	}
	if len(apps[0].Children) != 1 || apps[0].Children[0] != epgIDs["uni/tn-A/ap-p/epg-e1"] {
		t.Errorf("app children = %v", apps[0].Children)
	}
	if apps[0].Type != "logical.application" || apps[0].Description != "app" {
		t.Errorf("app group = %+v", apps[0])
	}
}

// TestTransformPolicyAnchors proves: each EPG and VRF group gets a
// resource anchor with the native DN identity and a distinct ID, the
// group names it in its extension and group membership is unchanged.
func TestTransformPolicyAnchors(t *testing.T) {
	epgAttrs := []map[string]any{{"dn": "uni/tn-A/ap-p/epg-e", "name": "e"}}
	vrfAttrs := []map[string]any{{"dn": "uni/tn-A/ctx-v", "name": "v"}}
	epgs, epgIDs := TransformEPGs(epgAttrs)
	vrfs, vrfIDs := TransformVRFs(vrfAttrs)

	res, epgAnchors, vrfAnchors := TransformPolicyAnchors(epgAttrs, epgIDs, epgs, vrfAttrs, vrfIDs, vrfs)
	if len(res) != 2 {
		t.Fatalf("expected 2 anchors, got %d", len(res))
	}
	for _, tc := range []struct {
		g      sdk.Group
		anchor string
		typ    string
	}{{epgs[0], epgAnchors["uni/tn-A/ap-p/epg-e"], policyEPGType}, {vrfs[0], vrfAnchors["uni/tn-A/ctx-v"], policyVRFType}} {
		if tc.anchor == "" || tc.anchor == tc.g.ID {
			t.Errorf("%s: anchor %q must exist and differ from group ID", tc.typ, tc.anchor)
		}
		if got := tc.g.Extensions[extensionNamespace].(map[string]any)["resource"]; got != tc.anchor {
			t.Errorf("%s: group extension resource = %v", tc.typ, got)
		}
		if len(tc.g.Members) != 0 {
			t.Errorf("%s: anchor added to group members: %v", tc.typ, tc.g.Members)
		}
	}
	for _, r := range res {
		if r.Provider.NativeID == "" || r.ID != resourceID(r.Provider.NativeID) {
			t.Errorf("anchor identity: %+v", r)
		}
	}
}

// TestDNKind proves: the parent kind is the naming prefix of the last
// DN segment and an unexpected segment yields "" (reported as "other"),
// never DN text.
func TestDNKind(t *testing.T) {
	for dn, want := range map[string]string{
		"uni/tn-A/ap-p/epg-e":                    "epg",
		"uni/tn-A/out-o/instP-x":                 "instP",
		"uni/tn-mgmt/oobbrc-o":                   "oobbrc",
		"uni/tn-A/BD-b":                          "BD",
		"uni/tn-A/x y-z":                         "",
		"noprefix":                               "",
		"uni/tn-A/averyveryverylongprefixname-x": "",
	} {
		if got := dnKind(dn); got != want {
			t.Errorf("dnKind(%q) = %q, want %q", dn, got, want)
		}
	}
	if scopeName("") != "other" || scopeName("instP") != "external-epgs" {
		t.Error("scopeName mapping")
	}
}
