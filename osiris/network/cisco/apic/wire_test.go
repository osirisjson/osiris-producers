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
	ethpm := []map[string]any{
		{"dn": "topology/pod-1/node-111/sys/phys-[eth1/5]/phys", "bundleIndex": "po1"},
		{"dn": "topology/pod-1/node-111/sys/phys-[eth1/6]/phys", "bundleIndex": "unspecified"},
	}
	pcDNToID := map[string]string{
		"topology/pod-1/node-111/sys/aggr-[po1]": resourceID("topology/pod-1/node-111/sys/aggr-[po1]"),
	}
	portDNToID := map[string]string{
		"topology/pod-1/node-111/sys/phys-[eth1/5]": resourceID("topology/pod-1/node-111/sys/phys-[eth1/5]"),
	}
	conns := WirePortChannelMembers(ethpm, pcDNToID, portDNToID)
	if len(conns) != 1 {
		t.Fatalf("expected 1 PC member edge, got %d", len(conns))
	}
	if conns[0].Source != pcDNToID["topology/pod-1/node-111/sys/aggr-[po1]"] ||
		conns[0].Target != portDNToID["topology/pod-1/node-111/sys/phys-[eth1/5]"] {
		t.Errorf("unexpected PC member edge: %+v", conns[0])
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
	ext, conns := WireFabricAdjacencies(fabricLinks, lldp, cdp, nodes, nodeResources)
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
	ext, conns := WireFabricAdjacencies(nil, lldp, cdp, nodes, nodeResources)
	if len(ext) != 1 {
		t.Fatalf("expected 1 external neighbour resource, got %d", len(ext))
	}
	e := ext[0]
	if e.Type != "network.switch" || e.ID != providerName+"::external/lab-ext-sw1" {
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
	if cisco, _ := e.Extensions[extensionNamespace].(map[string]any); cisco["external"] != true {
		t.Errorf("external extension = %v", e.Extensions)
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
	conns := WireEndpointPorts(endpoints, portDNToID, nil, numToDN)
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
	WireEPGPathAttachments(pathAtts, epgDNToID, epgGroups, portDNToID, nil, numToDN)
	if len(epgGroups[0].Members) != 1 ||
		epgGroups[0].Members[0] != resourceID("topology/pod-1/node-111/sys/phys-[eth1/1]") {
		t.Errorf("epg_WEB members = %v", epgGroups[0].Members)
	}
}
