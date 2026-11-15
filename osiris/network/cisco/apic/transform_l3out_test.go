// transform_l3out_test.go - Tests for the APIC L3Out mapping: external
// EPGs and policy prefixes with their containment, the profile-name
// split of L3Out relation DNs, node and path attachments at both
// purposes and the bridge-domain -> L3Out relation resolution.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package apic

import (
	"testing"
)

const (
	testL3Out  = "uni/tn-tn_Example/out-l3out_Core"
	testExtEPG = testL3Out + "/instP-ext_Any"
)

func TestTransformExternalEPGsAndPrefixes(t *testing.T) {
	l3outs := map[string]string{testL3Out: resourceID(testL3Out)}
	tally := newPolicyTally()
	epgs, epgDNToID, epgConns := TransformExternalEPGs([]map[string]any{
		{"dn": testExtEPG, "name": "ext_Any", "prefGrMemb": "exclude", "prio": "unspecified"},
		{"dn": "uni/tn-tn_Example/out-gone/instP-x", "name": "x"}, // L3Out not emitted
	}, l3outs, tally)
	if len(epgs) != 1 || epgs[0].Type != l3outEPGType || epgs[0].Name != "ext_Any" || epgDNToID[testExtEPG] != epgs[0].ID {
		t.Fatalf("external EPGs = %+v", epgs)
	}
	if epgs[0].Properties["preferred_group"] != "exclude" || epgs[0].Properties["qos_class"] != nil {
		t.Errorf("external EPG properties = %v", epgs[0].Properties)
	}
	if len(epgConns) != 1 || epgConns[0].Type != "contains" || epgConns[0].Source != resourceID(testL3Out) || epgConns[0].Target != epgs[0].ID {
		t.Errorf("L3Out contains external EPG = %+v", epgConns)
	}

	prefixes, prefixConns := TransformL3OutPrefixes([]map[string]any{
		{"dn": testExtEPG + "/extsubnet-[0.0.0.0/0]", "ip": "0.0.0.0/0", "scope": "import-security", "aggregate": ""},
		{"dn": testExtEPG + "/extsubnet-[2001:db8:10::5/48]", "ip": "2001:db8:10::5/48", "scope": "import-security,shared-rtctrl", "aggregate": "shared-rtctrl"},
		{"dn": "uni/tn-infra/fabricExtConnP-1/fabricExtRoutingP-r/extsubnet-[198.51.100.0/24]", "ip": "198.51.100.0/24"}, // not under an external EPG
		{"dn": testL3Out + "/instP-gone/extsubnet-[203.0.113.0/24]", "ip": "203.0.113.0/24"},                             // external EPG not emitted
	}, epgDNToID, tally)
	if len(prefixes) != 2 || len(prefixConns) != 2 {
		t.Fatalf("prefixes = %+v", prefixes)
	}
	if p := prefixes[0].Properties; p["cidr"] != "0.0.0.0/0" || p["scope"] != "import-security" || p["aggregate"] != nil {
		t.Errorf("default prefix properties = %v", p)
	}
	if p := prefixes[1].Properties; p["cidr"] != "2001:db8:10::/48" || p["aggregate"] != "shared-rtctrl" {
		t.Errorf("IPv6 prefix properties = %v", p)
	}
	if prefixConns[0].Source != epgs[0].ID {
		t.Errorf("external EPG contains prefix = %+v", prefixConns[0])
	}
	if tally.unresolved["l3extInstP"] != 1 || tally.unresolved["l3extSubnet"] != 1 || tally.outOfScope["l3extSubnet"]["other"] != 1 {
		t.Errorf("tally = %+v", tally)
	}
}

func TestL3OutProfiles(t *testing.T) {
	tests := []struct {
		dn, marker, l3out, node, ifp string
		ok                           bool
	}{
		{testL3Out + "/lnodep-np1/rsnodeL3OutAtt-[topology/pod-1/node-111]", "/rsnodeL3OutAtt-", testL3Out, "np1", "", true},
		{testL3Out + "/lnodep-np1/lifp-ip1/rspathL3OutAtt-[topology/pod-1/paths-111/pathep-[eth1/10]]", "/rspathL3OutAtt-", testL3Out, "np1", "ip1", true},
		{testL3Out + "/rsnodeL3OutAtt-[x]", "/rsnodeL3OutAtt-", "", "", "", false},
		{testL3Out + "/lnodep-np1/x", "/rsnodeL3OutAtt-", "", "", "", false},
	}
	for _, tt := range tests {
		l3out, node, ifp, ok := l3outProfiles(tt.dn, tt.marker)
		if ok != tt.ok || l3out != tt.l3out || node != tt.node || ifp != tt.ifp {
			t.Errorf("l3outProfiles(%q) = %q %q %q %v", tt.dn, l3out, node, ifp, ok)
		}
	}
}

func TestWireL3OutAttachments(t *testing.T) {
	pathDN := "topology/pod-1/paths-111/pathep-[eth1/10]"
	ref, _ := parsePathDN(pathDN)
	nodeAtts := []map[string]any{
		{"dn": testL3Out + "/lnodep-np1/rsnodeL3OutAtt-[" + testLeaf1 + "]", "tDn": testLeaf1, "rtrId": "192.0.2.111", "rtrIdLoopBack": "yes"},
		{"dn": testL3Out + "/lnodep-np1/rsnodeL3OutAtt-[topology/pod-1/node-999]", "tDn": "topology/pod-1/node-999"}, // node not emitted
	}
	pathAtts := []map[string]any{
		{"dn": testL3Out + "/lnodep-np1/lifp-ip1/rspathL3OutAtt-[" + pathDN + "]", "tDn": pathDN, "ifInstT": "sub-interface", "encap": "vlan-300", "addr": "198.51.100.1/30", "mtu": "inherit", "encapScope": "local", "mode": "regular"},
		{"dn": testL3Out + "/lnodep-np1/lifp-ip1/rspathL3OutAtt-[topology/pod-1/paths-111/pathep-[eth1/11]]", "tDn": "topology/pod-1/paths-111/pathep-[eth1/11]"}, // path not emitted
	}
	for _, audit := range []bool{false, true} {
		refs := l3outAttachRefs{
			l3outDNToID: map[string]string{testL3Out: resourceID(testL3Out)},
			nodeIDs:     map[string]bool{resourceID(testLeaf1): true},
			paths:       map[string]pathRef{pathDN: ref},
			audit:       audit,
		}
		tally := newPolicyTally()
		conns := WireL3OutAttachments(nodeAtts, pathAtts, refs, tally)
		if len(conns) != 2 {
			t.Fatalf("audit=%v: got %d edges", audit, len(conns))
		}
		node, path := conns[0], conns[1]
		if node.Type != connL3OutNode || node.Source != resourceID(testL3Out) || node.Target != resourceID(testLeaf1) || node.Properties["node_profile"] != "np1" {
			t.Errorf("audit=%v node edge = %+v", audit, node)
		}
		if path.Type != connL3OutPath || path.Target != resourceID(pathDN) || path.Properties["interface_profile"] != "ip1" ||
			path.Properties["interface_type"] != "sub-interface" || path.Properties["encap"] != "vlan-300" {
			t.Errorf("audit=%v path edge = %+v", audit, path)
		}
		if _, ok := node.Properties["router_id"]; ok != audit {
			t.Errorf("audit=%v router_id present=%v", audit, ok)
		}
		if _, ok := path.Properties["address"]; ok != audit {
			t.Errorf("audit=%v address present=%v", audit, ok)
		}
		if tally.unresolved["l3extRsNodeL3OutAtt"] != 1 || tally.unresolved["l3extRsPathL3OutAtt"] != 1 {
			t.Errorf("audit=%v tally = %v", audit, tally.unresolved)
		}
	}
}

func TestWirePolicyRelations_BDToL3Out(t *testing.T) {
	bd := "uni/tn-tn_Example/BD-bd_App"
	common := "uni/tn-common/out-l3out_Shared"
	l3outDNToID := map[string]string{testL3Out: resourceID(testL3Out), common: resourceID(common)}
	refs := policyRefs{
		bdDNToID:  map[string]string{bd: resourceID(bd)},
		l3outs:    l3outIndex([]map[string]any{{"dn": testL3Out, "name": "l3out_Core"}, {"dn": common, "name": "l3out_Shared"}}, l3outDNToID),
		contracts: newPolicyIndex(),
		filters:   newPolicyIndex(),
	}
	raw := map[string][]map[string]any{"fvRsBDToOut": {
		{"dn": bd + "/rsBDToOut-l3out_Core", "tnL3extOutName": "l3out_Core", "state": "formed"},
		{"dn": bd + "/rsBDToOut-l3out_Shared", "tnL3extOutName": "l3out_Shared"},
		{"dn": bd + "/rsBDToOut-l3out_Gone", "tnL3extOutName": "l3out_Gone"},
	}}
	tally := newPolicyTally()
	conns := WirePolicyRelations(raw, refs, tally)
	if len(conns) != 2 || tally.unresolved["fvRsBDToOut"] != 1 {
		t.Fatalf("edges = %+v, tally = %v", conns, tally.unresolved)
	}
	if c := conns[0]; c.Type != connBDToL3Out || c.Source != resourceID(bd) || c.Target != resourceID(testL3Out) || c.Properties["resolved_by"] != "name-local" {
		t.Errorf("local edge = %+v", c)
	}
	if c := conns[1]; c.Target != resourceID(common) || c.Properties["resolved_by"] != "name-common" {
		t.Errorf("common edge = %+v", c)
	}
}
