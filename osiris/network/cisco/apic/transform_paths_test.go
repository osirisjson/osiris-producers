// transform_paths_test.go - Tests for the APIC logical path, vPC domain
// and pod mapping: path DN parsing, the path emission rule, path member
// resolution (port, port-channel, vPC legs, FEX, node fallback), EPG
// and endpoint path edges, vPC domain membership and pod groups.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package apic

import (
	"reflect"
	"testing"

	"go.osirisjson.org/producers/pkg/sdk"
)

const (
	testLeaf1 = "topology/pod-1/node-111"
	testLeaf2 = "topology/pod-1/node-112"
	testVPC   = "topology/pod-1/protpaths-111-112/pathep-[LAB-VPC-1]"
	testPC    = "topology/pod-1/paths-111/pathep-[LAB-PC-1]"
	testPort  = "topology/pod-1/paths-111/pathep-[eth1/1]"
	testFEX   = "topology/pod-1/paths-111/extpaths-150/pathep-[eth1/7]"
)

func TestParsePathDN(t *testing.T) {
	tests := []struct {
		in    string
		ok    bool
		nodes []string
		fexes []string
		name  string
	}{
		{testPort, true, []string{"111"}, nil, "eth1/1"},
		{testVPC, true, []string{"111", "112"}, nil, "LAB-VPC-1"},
		{testFEX, true, []string{"111"}, []string{"150"}, "eth1/7"},
		{"topology/pod-2/protpaths-111-112/extprotpaths-150-151/pathep-[LAB-VPC-2]", true, []string{"111", "112"}, []string{"150", "151"}, "LAB-VPC-2"},
		{"topology/pod-1/protpaths-111-112/extpaths-150/pathep-[x]", false, nil, nil, ""}, // leg count mismatch
		{"topology/pod-1/paths-1x1/pathep-[eth1/1]", false, nil, nil, ""},
		{"topology/pod-1/paths-111/pathep-[]", false, nil, nil, ""},
		{"topology/pod-1/node-111/sys/phys-[eth1/1]", false, nil, nil, ""},
		{"uni/tn-a/pathep-[x]", false, nil, nil, ""},
	}
	for _, tt := range tests {
		ref, ok := parsePathDN(tt.in)
		if ok != tt.ok {
			t.Errorf("parsePathDN(%q) ok = %v, want %v", tt.in, ok, tt.ok)
			continue
		}
		if !ok {
			continue
		}
		if !reflect.DeepEqual(ref.nodes, tt.nodes) || !reflect.DeepEqual(ref.fexes, tt.fexes) || ref.name != tt.name {
			t.Errorf("parsePathDN(%q) = %+v", tt.in, ref)
		}
	}
}

func TestTransformPaths_EmissionRule(t *testing.T) {
	eps := []map[string]any{
		{"dn": testPort, "pathT": "leaf", "lagT": "not-aggregated"},                         // referenced
		{"dn": "topology/pod-1/paths-111/pathep-[eth1/2]", "lagT": "not-aggregated"},        // not referenced: skipped
		{"dn": testPC, "pathT": "leaf", "lagT": "link", "descr": "LAB bond"},                // aggregated
		{"dn": testVPC, "pathT": "leaf", "lagT": "node"},                                    // aggregated
		{"dn": testFEX, "pathT": "fex", "lagT": "not-aggregated"},                           // referenced
		{"dn": "topology/pod-1/node-111/sys/phys-[eth1/3]", "lagT": "link"},                 // not a path DN
		{"dn": "topology/pod-1/paths-111/pathep-[eth1/4]", "lagT": "unspecified", "x": "y"}, // sentinel, not referenced
	}
	refs := map[string]bool{testPort: true, testFEX: true}
	rs, paths := TransformPaths(eps, refs)
	if len(rs) != 4 || len(paths) != 4 {
		t.Fatalf("got %d resources / %d paths, want 4", len(rs), len(paths))
	}
	byID := map[string]sdk.Resource{}
	for _, r := range rs {
		byID[r.ID] = r
	}
	pc := byID[resourceID(testPC)]
	if pc.Type != pathType || pc.Name != "LAB-PC-1" || pc.Description != "LAB bond" || pc.Properties["aggregation"] != "link" {
		t.Errorf("port-channel path = %+v", pc)
	}
	vpc := byID[resourceID(testVPC)]
	if !reflect.DeepEqual(vpc.Properties["node_ids"], []string{"111", "112"}) || vpc.Properties["pod"] != "1" {
		t.Errorf("vPC path properties = %v", vpc.Properties)
	}
	if fex := byID[resourceID(testFEX)]; !reflect.DeepEqual(fex.Properties["fex_ids"], []string{"150"}) {
		t.Errorf("FEX path properties = %v", fex.Properties)
	}
}

func pathFixture() (pathMemberRefs, map[string]pathRef) {
	nodes := []map[string]any{{"dn": testLeaf1}, {"dn": testLeaf2}}
	pcs := []map[string]any{
		{"dn": testLeaf1 + "/sys/aggr-[po1]", "id": "po1", "name": "LAB-PC-1"},
		{"dn": testLeaf1 + "/sys/aggr-[po2]", "id": "po2", "name": "LAB-VPC-1"},
		{"dn": testLeaf2 + "/sys/aggr-[po7]", "id": "po7", "name": "LAB-VPC-1"},
	}
	vpcIfs := []map[string]any{
		{"dn": testLeaf1 + "/sys/vpc/inst/dom-10/if-1", "fabricPathDn": testVPC, "localOperSt": "up", "remoteOperSt": "down"},
	}
	confs := []map[string]any{
		{"dn": testLeaf1 + "/sys/vpc/inst/dom-10/if-1/rsvpcConf", "tDn": "sys/aggr-[po2]"},
	}
	pcIDs := map[string]string{}
	for _, p := range pcs {
		pcIDs[str(p, "dn")] = resourceID(str(p, "dn"))
	}
	portDN := physIfDN(testLeaf1, "eth1/1")
	refs := pathMemberRefs{
		resolver:   newPathResolver(nodes, pcs),
		portDNToID: map[string]string{portDN: resourceID(portDN)},
		pcIDs:      pcIDs,
		nodeIDs:    map[string]bool{resourceID(testLeaf1): true, resourceID(testLeaf2): true},
		vpcLegs:    indexVPCLegs(vpcIfs, confs),
		audit:      true,
	}
	paths := map[string]pathRef{}
	for _, dn := range []string{testPort, testPC, testVPC, testFEX, "topology/pod-1/paths-999/pathep-[eth1/1]"} {
		ref, _ := parsePathDN(dn)
		paths[dn] = ref
	}
	return refs, paths
}

func TestWirePathMembers(t *testing.T) {
	refs, paths := pathFixture()
	tally := newPolicyTally()
	conns := WirePathMembers(paths, refs, tally)

	type edge struct{ src, dst, level, how string }
	got := map[edge]bool{}
	for _, c := range conns {
		if c.Type != connPathMember || c.Direction != "forward" {
			t.Errorf("unexpected edge %+v", c)
		}
		got[edge{c.Source, c.Target, str(c.Properties, "member_level"), str(c.Properties, "resolved_by")}] = true
	}
	want := []edge{
		{resourceID(testPort), resourceID(physIfDN(testLeaf1, "eth1/1")), "port", ""},
		{resourceID(testPC), resourceID(testLeaf1 + "/sys/aggr-[po1]"), "aggregate", "pcAggrIf-name"},
		{resourceID(testVPC), resourceID(testLeaf1 + "/sys/aggr-[po2]"), "aggregate", "vpcIf"},
		{resourceID(testVPC), resourceID(testLeaf2 + "/sys/aggr-[po7]"), "aggregate", "pcAggrIf-name"},
		{resourceID(testFEX), resourceID(testLeaf1), "node", ""},
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("missing member edge %+v", w)
		}
	}
	if len(conns) != len(want) {
		t.Errorf("got %d member edges, want %d: %v", len(conns), len(want), got)
	}
	if tally.unresolved["fabricPathEp"] != 1 {
		t.Errorf("unresolved = %v, want fabricPathEp:1 (unknown node 999)", tally.unresolved)
	}
	for _, c := range conns {
		if c.Target == resourceID(testLeaf1+"/sys/aggr-[po2]") {
			if c.Properties["local_oper_status"] != "up" || c.Properties["remote_oper_status"] != "down" {
				t.Errorf("vPC leg state not carried at audit: %v", c.Properties)
			}
		}
		if c.Source == resourceID(testFEX) && c.Properties["fex_id"] != "150" {
			t.Errorf("FEX member edge = %v", c.Properties)
		}
	}
}

func TestWirePathMembers_NodeFallbackAndDocumentation(t *testing.T) {
	refs, _ := pathFixture()
	refs.pcIDs = map[string]string{} // no port-channel emitted
	refs.audit = false
	ref, _ := parsePathDN(testVPC)
	conns := WirePathMembers(map[string]pathRef{testVPC: ref}, refs, newPolicyTally())
	if len(conns) != 2 {
		t.Fatalf("got %d edges, want one per leg", len(conns))
	}
	for _, c := range conns {
		if c.Properties["member_level"] != "node" || (c.Target != resourceID(testLeaf1) && c.Target != resourceID(testLeaf2)) {
			t.Errorf("leg without a port-channel must fall back to its leaf: %+v", c)
		}
		if _, ok := c.Properties["local_oper_status"]; ok {
			t.Errorf("vPC leg state must be audit only: %v", c.Properties)
		}
	}
}

func TestIndexVPCLegs_AbsoluteTarget(t *testing.T) {
	legs := indexVPCLegs(
		[]map[string]any{{"dn": testLeaf2 + "/sys/vpc/inst/dom-10/if-1", "fabricPathDn": testVPC, "localOperSt": "unspecified"}},
		[]map[string]any{{"dn": testLeaf2 + "/sys/vpc/inst/dom-10/if-1/rsvpcConf", "tDn": testLeaf2 + "/sys/aggr-[po7]"}},
	)
	leg := legs[testVPC][testLeaf2]
	if leg.aggrDN != testLeaf2+"/sys/aggr-[po7]" || leg.localOperSt != "" {
		t.Errorf("leg = %+v", leg)
	}
}

func TestWireEPGPaths(t *testing.T) {
	epgDN := "uni/tn-tn_Example/ap-app1/epg-epg_Web"
	anchors := map[string]string{epgDN: resourceID(epgDN)}
	_, paths := pathFixture()
	pathAtts := []map[string]any{
		{"dn": epgDN + "/rspathAtt-[" + testVPC + "]", "tDn": testVPC, "encap": "vlan-100", "mode": "regular", "instrImedcy": "immediate", "state": "formed", "primaryEncap": "unknown"},
		{"dn": epgDN + "/rspathAtt-[topology/pod-1/paths-111/pathep-[eth1/30]]", "tDn": "topology/pod-1/paths-111/pathep-[eth1/30]"}, // path not emitted
		{"dn": "uni/tn-tn_Example/ap-app1/esg-esg1/rspathAtt-[" + testVPC + "]", "tDn": testVPC},                                     // not an EPG
		{"dn": "uni/tn-tn_Example/ap-app1/epg-missing/rspathAtt-[" + testVPC + "]", "tDn": testVPC},                                  // EPG not emitted
	}
	tally := newPolicyTally()
	conns := WireEPGPaths(pathAtts, anchors, paths, tally)
	if len(conns) != 1 {
		t.Fatalf("got %d edges, want 1", len(conns))
	}
	c := conns[0]
	if c.Type != connEPGPath || c.Source != resourceID(epgDN) || c.Target != resourceID(testVPC) ||
		c.Properties["encap"] != "vlan-100" || c.Properties["instr_immediacy"] != "immediate" || c.Properties["primary_encap"] != "unknown" {
		t.Errorf("edge = %+v", c)
	}
	if tally.unresolved["fvRsPathAtt"] != 2 || tally.outOfScope["fvRsPathAtt"]["endpoint-security-groups"] != 1 {
		t.Errorf("tally = %+v", tally)
	}
}

func TestWireEndpointPaths(t *testing.T) {
	_, paths := pathFixture()
	cep := "uni/tn-tn_Example/ap-app1/epg-epg_Web/cep-00:00:5E:00:53:11"
	eps := []map[string]any{
		{"dn": cep, "fabricPathDn": testVPC, "encap": "vlan-100"},
		{"dn": cep + "x", "fabricPathDn": "topology/pod-1/paths-111/pathep-[eth1/30]"}, // path not emitted
		{"dn": cep + "y"}, // no path
	}
	tally := newPolicyTally()
	conns := WireEndpointPaths(eps, paths, tally)
	if len(conns) != 1 || conns[0].Type != connLearnedOn || conns[0].Source != resourceID(cep) ||
		conns[0].Target != resourceID(testVPC) || conns[0].Properties["semantics"] != "observed" {
		t.Fatalf("edges = %+v", conns)
	}
	if tally.unresolved["fvCEp"] != 1 {
		t.Errorf("tally = %+v", tally.unresolved)
	}
}

func TestTransformVPCDomains(t *testing.T) {
	grpDN := "uni/fabric/protpol/expgep-LAB-VPC-111-112"
	groups := []map[string]any{{"dn": grpDN, "name": "LAB-VPC-111-112", "id": "10", "configSt": "deployed", "virtualIp": "192.0.2.250/32"}}
	members := []map[string]any{
		{"dn": grpDN + "/nodepep-111", "id": "111"},
		{"dn": grpDN + "/nodepep-112", "id": "112"},
		{"dn": grpDN + "/nodepep-999", "id": "999"}, // not an emitted node
	}
	doms := []map[string]any{{"dn": testLeaf1 + "/sys/vpc/inst/dom-10", "operRole": "primary", "peerSt": "up", "peerStQual": "success"}}
	numToDN := map[string]string{"111": testLeaf1, "112": testLeaf2}
	nodeIDs := map[string]bool{resourceID(testLeaf1): true, resourceID(testLeaf2): true}

	for _, audit := range []bool{false, true} {
		tally := newPolicyTally()
		rs, conns, pairs := TransformVPCDomains(groups, members, doms, numToDN, nodeIDs, audit, tally)
		if len(rs) != 1 || rs[0].Type != vpcDomainType || rs[0].Properties["domain_id"] != "10" {
			t.Fatalf("audit=%v resources = %+v", audit, rs)
		}
		if _, ok := rs[0].Properties["virtual_ip"]; ok != audit {
			t.Errorf("audit=%v virtual_ip present=%v", audit, ok)
		}
		if len(conns) != 2 || tally.unresolved["fabricNodePEp"] != 1 {
			t.Errorf("audit=%v conns=%d tally=%v", audit, len(conns), tally.unresolved)
		}
		for _, c := range conns {
			_, hasRole := c.Properties["role"]
			if c.Target == resourceID(testLeaf1) && hasRole != audit {
				t.Errorf("audit=%v leaf1 edge = %v", audit, c.Properties)
			}
		}
		if pairs["111-112"] != "10" {
			t.Errorf("pairs = %v", pairs)
		}

		pathRs, paths := TransformPaths([]map[string]any{{"dn": testVPC, "lagT": "node"}}, nil)
		LabelVPCPaths(pathRs, paths, pairs)
		if pathRs[0].Properties["vpc_domain_id"] != "10" {
			t.Errorf("vPC path not labeled: %v", pathRs[0].Properties)
		}
	}
}

func TestTransformPods(t *testing.T) {
	members := []sdk.Resource{
		{ID: resourceID(testLeaf1), Provider: sdk.Provider{NativeID: testLeaf1}},
		{ID: resourceID(testVPC), Provider: sdk.Provider{NativeID: testVPC}},
		{ID: resourceID("topology/pod-2/node-211"), Provider: sdk.Provider{NativeID: "topology/pod-2/node-211"}},
		{ID: resourceID("uni/fabric/protpol/expgep-x"), Provider: sdk.Provider{NativeID: "uni/fabric/protpol/expgep-x"}},
	}
	groups, dnToID := TransformPods([]map[string]any{{"dn": "topology/pod-1", "id": "1", "podType": "physical"}}, members)
	if len(groups) != 1 || groups[0].Type != podGroupType || groups[0].Name != "pod-1" || dnToID["topology/pod-1"] != groups[0].ID {
		t.Fatalf("groups = %+v", groups)
	}
	if !reflect.DeepEqual(groups[0].Members, []string{resourceID(testLeaf1), resourceID(testVPC)}) {
		t.Errorf("members = %v", groups[0].Members)
	}
	if groups[0].Properties["pod_type"] != "physical" {
		t.Errorf("properties = %v", groups[0].Properties)
	}
}
