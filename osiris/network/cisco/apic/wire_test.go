// wire_test.go - Unit tests for the APIC topology wiring: node/port and
// bridge-domain/subnet containment, port-channel membership, the merged
// fabricLink/LLDP/CDP adjacency graph with external-neighbour resources
// and the audit-only endpoint-to-port and EPG-to-path attachments.
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

func nodeRes(t *testing.T, dn string) sdk.Resource {
	t.Helper()
	r, err := sdk.NewResource(resourceID(dn), "network.switch", sdk.Provider{Name: providerName, NativeID: dn})
	if err != nil {
		t.Fatalf("nodeRes(%q): %v", dn, err)
	}
	return r
}

func connByType(conns []sdk.Connection, typ string) []sdk.Connection {
	var out []sdk.Connection
	for _, c := range conns {
		if c.Type == typ {
			out = append(out, c)
		}
	}
	return out
}

func TestWireNodePorts(t *testing.T) {
	nodeIDs := map[string]bool{resourceID("topology/pod-1/node-111"): true}
	portDNToID := map[string]string{
		"topology/pod-1/node-111/sys/phys-[eth1/1]": resourceID("topology/pod-1/node-111/sys/phys-[eth1/1]"),
		"topology/pod-1/node-999/sys/phys-[eth1/1]": resourceID("topology/pod-1/node-999/sys/phys-[eth1/1]"),
	}
	conns := WireNodePorts(portDNToID, nodeIDs)
	if len(conns) != 1 {
		t.Fatalf("expected 1 contains edge (node-999 not emitted), got %d", len(conns))
	}
	c := conns[0]
	if c.Type != "contains" || c.Direction != "forward" ||
		c.Source != resourceID("topology/pod-1/node-111") ||
		c.Target != resourceID("topology/pod-1/node-111/sys/phys-[eth1/1]") {
		t.Errorf("unexpected connection: %+v", c)
	}
}

func TestWirePortChannelMembers(t *testing.T) {
	pc := "topology/pod-1/node-111/sys/aggr-[po1]"
	port := func(p string) string { return "topology/pod-1/node-111/sys/phys-[" + p + "]" }
	ethpm := []map[string]any{
		{"dn": port("eth1/5") + "/phys", "bundleIndex": "po1"}, // also in pcRsMbrIfs: one edge
		{"dn": port("eth1/6") + "/phys", "bundleIndex": "unspecified"},
		{"dn": port("eth1/9") + "/phys", "bundleIndex": "po9"}, // port-channel not emitted: silent
	}
	mbrIfs := []map[string]any{
		{"dn": pc + "/rsmbrIfs-[eth1/5]", "tDn": "sys/phys-[eth1/5]", "tSKey": "eth1/5"},
		{"dn": pc + "/rsmbrIfs-[eth1/7]", "tDn": port("eth1/7")},        // absolute target
		{"dn": pc + "/rsmbrIfs-[eth1/8]", "tSKey": "eth1/8"},            // no tDn
		{"dn": pc + "/rsmbrIfs-[eth1/30]", "tDn": "sys/phys-[eth1/30]"}, // port not emitted
		{"dn": "uni/x/rsmbrIfs-[eth1/1]", "tDn": "sys/phys-[eth1/1]"},   // not under a node
	}
	pcDNToID := map[string]string{pc: resourceID(pc)}
	portDNToID := map[string]string{}
	for _, p := range []string{"eth1/5", "eth1/7", "eth1/8"} {
		portDNToID[port(p)] = resourceID(port(p))
	}
	conns, unresolved := WirePortChannelMembers(ethpm, mbrIfs, pcDNToID, portDNToID)
	if len(conns) != 3 || unresolved != 1 {
		t.Fatalf("got %d edges, %d unresolved; want 3 and 1", len(conns), unresolved)
	}
	for i, p := range []string{"eth1/5", "eth1/7", "eth1/8"} {
		if conns[i].Type != "contains" || conns[i].Source != resourceID(pc) || conns[i].Target != resourceID(port(p)) {
			t.Errorf("edge %d = %+v", i, conns[i])
		}
	}

	// bundleIndex alone (pcRsMbrIfs not collected) still wires the member.
	conns, _ = WirePortChannelMembers(ethpm[:1], nil, pcDNToID, portDNToID)
	if len(conns) != 1 || conns[0].Target != resourceID(port("eth1/5")) {
		t.Errorf("bundleIndex fallback = %+v", conns)
	}
}

func TestWireBDSubnets(t *testing.T) {
	subnets := []map[string]any{
		{"dn": "uni/tn-tn_Example/BD-bd_App/subnet-[203.0.113.1/24]"},
		{"dn": "uni/tn-tn_Example/ap-app1/epg-epg_WEB/subnet-[198.51.100.1/24]"}, // under EPG, skipped
	}
	bdDNToID := map[string]string{
		"uni/tn-tn_Example/BD-bd_App": resourceID("uni/tn-tn_Example/BD-bd_App"),
	}
	conns := WireBDSubnets(subnets, bdDNToID)
	if len(conns) != 1 {
		t.Fatalf("expected 1 BD->subnet edge, got %d", len(conns))
	}
	if conns[0].Source != bdDNToID["uni/tn-tn_Example/BD-bd_App"] ||
		conns[0].Target != resourceID("uni/tn-tn_Example/BD-bd_App/subnet-[203.0.113.1/24]") {
		t.Errorf("unexpected BD->subnet edge: %+v", conns[0])
	}
}

func TestWireFabricAdjacencies_MergesSources(t *testing.T) {
	nodes := []map[string]any{
		{"dn": "topology/pod-1/node-101", "name": "LAB-SPINE1"},
		{"dn": "topology/pod-1/node-111", "name": "LAB-LEAF1"},
	}
	nodeResources := []sdk.Resource{
		nodeRes(t, "topology/pod-1/node-101"),
		nodeRes(t, "topology/pod-1/node-111"),
	}
	fabricLinks := []map[string]any{
		{"n1": "111", "s1": "1", "p1": "53", "n2": "101", "s2": "1", "p2": "1", "linkState": "ok"},
	}
	// Same cable, observed by LLDP from node-111's side (peer carries a
	// topology sysDesc) and by CDP from node-101's side (peer sysName
	// matches the fabric node).
	lldp := []map[string]any{
		{"dn": "topology/pod-1/node-111/sys/lldp/inst/if-[eth1/53]/adj-1", "sysDesc": "topology/pod-1/node-101", "portIdV": "Eth1/1"},
	}
	cdp := []map[string]any{
		{"dn": "topology/pod-1/node-101/sys/cdp/inst/if-[eth1/1]/adj-1", "sysName": "LAB-LEAF1", "portId": "Ethernet1/53"},
	}
	ext, conns, _ := WireFabricAdjacencies(fabricLinks, lldp, cdp, nodes, nodeResources)
	if len(ext) != 0 {
		t.Fatalf("expected no external resources, got %d", len(ext))
	}
	phys := connByType(conns, "physical.ethernet")
	if len(phys) != 1 {
		t.Fatalf("expected 1 merged physical link, got %d: %+v", len(phys), conns)
	}
	c := phys[0]
	if c.Properties["discovered_by"] != "cdp,fabricLink,lldp" {
		t.Errorf("discovered_by = %v, want cdp,fabricLink,lldp", c.Properties["discovered_by"])
	}
	if c.Properties["link_state"] != "ok" {
		t.Errorf("link_state = %v", c.Properties["link_state"])
	}
	// Endpoints ordered by resource ID (node-101 < node-111).
	if c.Source != resourceID("topology/pod-1/node-101") || c.Target != resourceID("topology/pod-1/node-111") {
		t.Errorf("endpoints: %s -> %s", c.Source, c.Target)
	}
	if c.Properties["source_port"] != "eth1/1" || c.Properties["target_port"] != "eth1/53" {
		t.Errorf("ports: %v / %v", c.Properties["source_port"], c.Properties["target_port"])
	}
}

func TestWireFabricAdjacencies_ExternalNeighbour(t *testing.T) {
	nodes := []map[string]any{{"dn": "topology/pod-1/node-111", "name": "LAB-LEAF1"}}
	nodeResources := []sdk.Resource{nodeRes(t, "topology/pod-1/node-111")}

	// LLDP and CDP both see the same off-fabric neighbour on eth1/1.
	lldp := []map[string]any{
		{"dn": "topology/pod-1/node-111/sys/lldp/inst/if-[eth1/1]/adj-1", "sysName": "LAB-EXT-SW1.example.com", "chassisIdT": "mac", "chassisIdV": "00:00:5E:00:53:01", "portIdV": "Eth1/48", "sysDesc": "external device", "mgmtIp": "198.51.100.9"},
	}
	cdp := []map[string]any{
		{"dn": "topology/pod-1/node-111/sys/cdp/inst/if-[eth1/1]/adj-1", "sysName": "LAB-EXT-SW1", "portId": "Ethernet1/48", "platId": "N9K-C93180YC-FX", "ver": "Cisco Nexus Operating System (NX-OS)", "devId": "LAB-EXT-SW1(TST0000009)"},
	}
	ext, conns, _ := WireFabricAdjacencies(nil, lldp, cdp, nodes, nodeResources)
	if len(ext) != 1 {
		t.Fatalf("expected 1 external neighbour resource, got %d", len(ext))
	}
	e := ext[0]
	if e.Type != "network.switch" || e.ID != providerName+"::external/lldp/mac/00:00:5e:00:53:01" {
		t.Errorf("external resource type/id = %q / %q", e.Type, e.ID)
	}
	if e.Status != "unknown" {
		t.Errorf("external status = %q", e.Status)
	}
	if e.Properties["discovered_by"] != "cdp,lldp" {
		t.Errorf("external discovered_by = %v", e.Properties["discovered_by"])
	}
	if e.Properties["manufacturer"] != "Cisco" || e.Properties["model"] != "N9K-C93180YC-FX" {
		t.Errorf("external manufacturer/model = %v / %v", e.Properties["manufacturer"], e.Properties["model"])
	}
	if e.Properties["serial"] != "TST0000009" {
		t.Errorf("external serial = %v", e.Properties["serial"])
	}
	if e.Properties["chassis_id"] != "00:00:5E:00:53:01" || e.Properties["management_ip"] != "198.51.100.9" {
		t.Errorf("external chassis/mgmt = %v / %v", e.Properties["chassis_id"], e.Properties["management_ip"])
	}
	if cisco, _ := e.Extensions[extensionNamespace].(map[string]any); cisco["external"] != true ||
		!reflect.DeepEqual(cisco["legacy_ids"], []string{providerName + "::external/lab-ext-sw1"}) {
		t.Errorf("external extension = %v", e.Extensions)
	}
	if e.Properties["identity_basis"] != "lldp-chassis-id" || e.Properties["cdp_device_id"] != "LAB-EXT-SW1(TST0000009)" {
		t.Errorf("external identity = %v", e.Properties)
	}

	phys := connByType(conns, "physical.ethernet")
	if len(phys) != 1 {
		t.Fatalf("expected 1 merged external link, got %d", len(phys))
	}
	if phys[0].Properties["discovered_by"] != "cdp,lldp" {
		t.Errorf("link discovered_by = %v", phys[0].Properties["discovered_by"])
	}
	if phys[0].Source != e.ID && phys[0].Target != e.ID {
		t.Errorf("external link does not reference the neighbour resource: %+v", phys[0])
	}
}

func TestWireEndpointPorts(t *testing.T) {
	numToDN := map[string]string{"111": "topology/pod-1/node-111"}
	endpoints := []map[string]any{
		{"dn": "uni/tn-tn_Example/ap-app1/epg-epg_WEB/cep-00:00:5E:00:53:AA", "fabricPathDn": "topology/pod-1/paths-111/pathep-[eth1/1]"},
		{"dn": "uni/tn-tn_Example/ap-app1/epg-epg_WEB/cep-00:00:5E:00:53:BB", "fabricPathDn": "topology/pod-1/protpaths-111-112/pathep-[vpc_x]"}, // vPC: skipped
	}
	portDNToID := map[string]string{
		"topology/pod-1/node-111/sys/phys-[eth1/1]": resourceID("topology/pod-1/node-111/sys/phys-[eth1/1]"),
	}
	conns := WireEndpointPorts(endpoints, portDNToID, nil, newPathResolver(nodesFromNumbers(numToDN), nil))
	if len(conns) != 1 {
		t.Fatalf("expected 1 endpoint->port edge, got %d", len(conns))
	}
	c := conns[0]
	if c.Type != "contains" ||
		c.Source != resourceID("topology/pod-1/node-111/sys/phys-[eth1/1]") ||
		c.Target != resourceID("uni/tn-tn_Example/ap-app1/epg-epg_WEB/cep-00:00:5E:00:53:AA") {
		t.Errorf("unexpected endpoint edge: %+v", c)
	}
}

func TestWireEPGPathAttachments(t *testing.T) {
	numToDN := map[string]string{"111": "topology/pod-1/node-111"}
	epgGroups, epgDNToID := TransformEPGs([]map[string]any{
		{"dn": "uni/tn-tn_Example/ap-app1/epg-epg_WEB", "name": "epg_WEB"},
	})
	pathAtts := []map[string]any{
		{"dn": "uni/tn-tn_Example/ap-app1/epg-epg_WEB/rspathAtt-[topology/pod-1/paths-111/pathep-[eth1/1]]", "tDn": "topology/pod-1/paths-111/pathep-[eth1/1]", "encap": "vlan-100"},
	}
	portDNToID := map[string]string{
		"topology/pod-1/node-111/sys/phys-[eth1/1]": resourceID("topology/pod-1/node-111/sys/phys-[eth1/1]"),
	}
	WireEPGPathAttachments(pathAtts, epgDNToID, epgGroups, portDNToID, nil, newPathResolver(nodesFromNumbers(numToDN), nil))
	if len(epgGroups[0].Members) != 1 ||
		epgGroups[0].Members[0] != resourceID("topology/pod-1/node-111/sys/phys-[eth1/1]") {
		t.Errorf("epg_WEB members = %v", epgGroups[0].Members)
	}
}

// nodesFromNumbers turns a node number -> DN map into fabricNode
// attribute maps for newPathResolver.
func nodesFromNumbers(numToDN map[string]string) []map[string]any {
	var nodes []map[string]any
	for _, dn := range numToDN {
		nodes = append(nodes, map[string]any{"dn": dn})
	}
	return nodes
}

// externalIdentityFixture returns a leaf and the LLDP/CDP observations
// of the external-identity cases:
//   - eth1/1: LLDP chassis A "LAB-EXT-SW1.example.com" and CDP
//     "LAB-EXT-SW1" agree (one neighbour);
//   - eth1/2: CDP only, same device id as eth1/1 (correlates to A);
//   - eth1/3: LLDP chassis B "LAB-EXT-SW1.example.net" (same short
//     name as A, a different device);
//   - eth1/4: LLDP chassis C "LAB-EXT-SW2" and CDP "LAB-EXT-SW3"
//     disagree (two neighbours, both flagged).
func externalIdentityFixture() (lldp, cdp []map[string]any) {
	adj := func(proto, port string) string {
		return "topology/pod-1/node-111/sys/" + proto + "/inst/if-[" + port + "]/adj-1"
	}
	lldp = []map[string]any{
		{"dn": adj("lldp", "eth1/1"), "sysName": "LAB-EXT-SW1.example.com", "chassisIdT": "mac", "chassisIdV": "00:00:5E:00:53:A1", "portIdV": "Eth1/48"},
		{"dn": adj("lldp", "eth1/3"), "sysName": "LAB-EXT-SW1.example.net", "chassisIdT": "mac", "chassisIdV": "00:00:5E:00:53:B1", "portIdV": "Eth1/48"},
		{"dn": adj("lldp", "eth1/4"), "sysName": "LAB-EXT-SW2", "chassisIdT": "mac", "chassisIdV": "00:00:5E:00:53:C1", "portIdV": "Eth1/1"},
	}
	cdp = []map[string]any{
		{"dn": adj("cdp", "eth1/1"), "sysName": "LAB-EXT-SW1", "devId": "LAB-EXT-SW1(TST0000901)", "portId": "Ethernet1/48"},
		{"dn": adj("cdp", "eth1/2"), "sysName": "LAB-EXT-SW1", "devId": "LAB-EXT-SW1(TST0000901)", "portId": "Ethernet1/49"},
		{"dn": adj("cdp", "eth1/4"), "sysName": "LAB-EXT-SW3", "devId": "LAB-EXT-SW3(TST0000903)", "portId": "Ethernet1/1"},
	}
	return lldp, cdp
}

func TestWireFabricAdjacencies_ExternalIdentity(t *testing.T) {
	nodes := []map[string]any{{"dn": "topology/pod-1/node-111", "name": "LAB-LEAF1"}}
	nodeResources := []sdk.Resource{nodeRes(t, "topology/pod-1/node-111")}
	lldp, cdp := externalIdentityFixture()
	ext, conns, _ := WireFabricAdjacencies(nil, lldp, cdp, nodes, nodeResources)

	byID := map[string]sdk.Resource{}
	for _, r := range ext {
		byID[r.ID] = r
	}
	a := providerName + "::external/lldp/mac/00:00:5e:00:53:a1"
	b := providerName + "::external/lldp/mac/00:00:5e:00:53:b1"
	c := providerName + "::external/lldp/mac/00:00:5e:00:53:c1"
	d := providerName + "::external/cdp/lab-ext-sw3_tst0000903_"
	if len(byID) != 4 || byID[a].ID == "" || byID[b].ID == "" || byID[c].ID == "" || byID[d].ID == "" {
		t.Fatalf("external IDs = %v", sortedKeys(byID))
	}
	if byID[a].Properties["discovered_by"] != "cdp,lldp" || byID[b].Properties["discovered_by"] != "lldp" {
		t.Errorf("discovered_by a=%v b=%v", byID[a].Properties["discovered_by"], byID[b].Properties["discovered_by"])
	}
	conflict := func(id string) any {
		return byID[id].Extensions[extensionNamespace].(map[string]any)["identity_conflict"]
	}
	if !reflect.DeepEqual(conflict(c), []string{d}) || !reflect.DeepEqual(conflict(d), []string{c}) {
		t.Errorf("conflict c=%v d=%v", conflict(c), conflict(d))
	}
	if conflict(a) != nil || conflict(b) != nil {
		t.Errorf("unexpected conflict a=%v b=%v", conflict(a), conflict(b))
	}
	links := 0
	for _, cn := range connByType(conns, "physical.ethernet") {
		if cn.Source == a || cn.Target == a {
			links++
		}
	}
	if links != 2 {
		t.Errorf("neighbour A links = %d, want 2 (eth1/1 and the CDP-only eth1/2)", links)
	}

	// Input order never changes the result.
	rev := func(in []map[string]any) []map[string]any {
		out := append([]map[string]any(nil), in...)
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
		return out
	}
	ext2, conns2, _ := WireFabricAdjacencies(nil, rev(lldp), rev(cdp), nodes, nodeResources)
	if len(ext2) != len(ext) || len(conns2) != len(conns) {
		t.Fatalf("reversed input: %d/%d resources, %d/%d connections", len(ext2), len(ext), len(conns2), len(conns))
	}
	for _, r := range ext2 {
		if !reflect.DeepEqual(r, byID[r.ID]) {
			t.Errorf("reversed input changed %s:\n%+v\n%+v", r.ID, r, byID[r.ID])
		}
	}
}

func TestWireFabricAdjacencies_AmbiguousCDPDevice(t *testing.T) {
	nodes := []map[string]any{{"dn": "topology/pod-1/node-111", "name": "LAB-LEAF1"}}
	nodeResources := []sdk.Resource{nodeRes(t, "topology/pod-1/node-111")}
	adj := func(proto, port string) string {
		return "topology/pod-1/node-111/sys/" + proto + "/inst/if-[" + port + "]/adj-1"
	}
	// One CDP device id agrees by name with two different LLDP chassis:
	// it cannot be folded into either.
	lldp := []map[string]any{
		{"dn": adj("lldp", "eth1/1"), "sysName": "LAB-EXT-SW1", "chassisIdT": "mac", "chassisIdV": "00:00:5E:00:53:A1"},
		{"dn": adj("lldp", "eth1/2"), "sysName": "LAB-EXT-SW1", "chassisIdT": "mac", "chassisIdV": "00:00:5E:00:53:A2"},
	}
	cdp := []map[string]any{
		{"dn": adj("cdp", "eth1/1"), "sysName": "LAB-EXT-SW1", "devId": "LAB-EXT-SW1"},
		{"dn": adj("cdp", "eth1/2"), "sysName": "LAB-EXT-SW1", "devId": "LAB-EXT-SW1"},
	}
	ext, _, _ := WireFabricAdjacencies(nil, lldp, cdp, nodes, nodeResources)
	if len(ext) != 3 {
		t.Fatalf("got %d external neighbours, want 3 (two chassis and the ambiguous CDP device)", len(ext))
	}
	for _, r := range ext {
		cf := r.Extensions[extensionNamespace].(map[string]any)["identity_conflict"]
		if r.Properties["identity_basis"] == "cdp-device-id" && len(cf.([]string)) != 2 {
			t.Errorf("CDP device conflict = %v", cf)
		}
	}
}
