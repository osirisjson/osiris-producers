// transform_topology_test.go - Unit tests for the APIC physical-fabric
// resource transforms: switch-port and port-channel resources, the
// admin/oper status mapping, the bounded documentation-mode port set,
// and the path-target DN resolver.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package apic

import "testing"

func TestMapPortStatus(t *testing.T) {
	tests := []struct{ admin, oper, want string }{
		{"up", "up", "active"},
		{"up", "down", "inactive"},
		{"up", "", "active"},
		{"up", "link-down", "inactive"},
		{"down", "up", "inactive"},
		{"disabled", "", "inactive"},
		{"up", "something", "unknown"},
		{"", "", "unknown"},
	}
	for _, tt := range tests {
		if got := mapPortStatus(tt.admin, tt.oper); got != tt.want {
			t.Errorf("mapPortStatus(%q,%q) = %q, want %q", tt.admin, tt.oper, got, tt.want)
		}
	}
}

func TestTransformSwitchPorts_JoinAndFilter(t *testing.T) {
	l1 := []map[string]any{
		{"dn": "topology/pod-1/node-111/sys/phys-[eth1/1]", "id": "eth1/1", "adminSt": "up", "layer": "Layer2", "mode": "trunk", "mtu": "9000", "usage": "epg", "portT": "leaf"},
		{"dn": "topology/pod-1/node-111/sys/phys-[eth1/9]", "id": "eth1/9", "adminSt": "down", "layer": "Layer2", "usage": "discovery", "portT": "leaf"},
	}
	ethpm := []map[string]any{
		{"dn": "topology/pod-1/node-111/sys/phys-[eth1/1]/phys", "operSt": "up", "operSpeed": "25G", "operDuplex": "full", "operStQual": "none"},
	}

	// keep set contains only eth1/1.
	keep := map[string]bool{"topology/pod-1/node-111/sys/phys-[eth1/1]": true}
	res, dnToID := TransformSwitchPorts(l1, ethpm, keep)
	if len(res) != 1 {
		t.Fatalf("expected 1 port (filtered), got %d", len(res))
	}
	p := res[0]
	if p.Type != "network.switch.port" {
		t.Errorf("type = %q", p.Type)
	}
	if p.ID != resourceID("topology/pod-1/node-111/sys/phys-[eth1/1]") {
		t.Errorf("id = %q", p.ID)
	}
	if dnToID["topology/pod-1/node-111/sys/phys-[eth1/1]"] != p.ID {
		t.Errorf("dnToID mismatch: %v", dnToID)
	}
	if p.Status != "active" {
		t.Errorf("status = %q, want active", p.Status)
	}
	if p.Properties["interface_name"] != "eth1/1" || p.Properties["node_id"] != "111" ||
		p.Properties["oper_status"] != "up" || p.Properties["speed"] != "25G" ||
		p.Properties["layer"] != "Layer2" || p.Properties["port_mode"] != "trunk" {
		t.Errorf("properties = %#v", p.Properties)
	}
	cisco, _ := p.Extensions[extensionNamespace].(map[string]any)
	if cisco["port_type"] != "leaf" {
		t.Errorf("extension port_type = %v", cisco)
	}

	// nil keep emits every port.
	all, _ := TransformSwitchPorts(l1, ethpm, nil)
	if len(all) != 2 {
		t.Fatalf("nil keep: expected 2 ports, got %d", len(all))
	}
	for _, r := range all {
		if r.Name == "eth1/9" && r.Status != "inactive" {
			t.Errorf("admin-down port status = %q, want inactive", r.Status)
		}
	}
}

func TestTransformPortChannels(t *testing.T) {
	pc := []map[string]any{
		{"dn": "topology/pod-1/node-111/sys/aggr-[po1]", "id": "po1", "adminSt": "up", "pcMode": "active", "minLinks": "1", "activePorts": "2", "mode": "trunk", "mtu": "9000", "usage": "epg", "pcId": "1", "descr": "to LAB-EXT-SW1"},
		{"dn": "topology/pod-1/node-111/sys/aggr-[po2]", "id": "po2", "adminSt": "up", "activePorts": "0"},
	}
	res, dnToID := TransformPortChannels(pc)
	if len(res) != 2 {
		t.Fatalf("expected 2 port-channels, got %d", len(res))
	}
	byName := map[string]int{}
	for i, r := range res {
		byName[r.Name] = i
	}
	po1 := res[byName["po1"]]
	if po1.Type != "network.switch.port" || po1.Properties["aggregate"] != true {
		t.Errorf("po1 type/aggregate = %q / %v", po1.Type, po1.Properties["aggregate"])
	}
	if po1.Status != "active" {
		t.Errorf("po1 status = %q, want active (activePorts=2)", po1.Status)
	}
	if po1.Properties["channel_mode"] != "active" || po1.Properties["min_links"] != "1" {
		t.Errorf("po1 properties = %#v", po1.Properties)
	}
	if po1.Description != "to LAB-EXT-SW1" {
		t.Errorf("po1 description = %q", po1.Description)
	}
	if res[byName["po2"]].Status != "inactive" {
		t.Errorf("po2 status = %q, want inactive (activePorts=0)", res[byName["po2"]].Status)
	}
	if dnToID["topology/pod-1/node-111/sys/aggr-[po1]"] != po1.ID {
		t.Errorf("dnToID: %v", dnToID)
	}
}

func TestTopologyPortDNs(t *testing.T) {
	numToDN := map[string]string{
		"101": "topology/pod-1/node-101",
		"111": "topology/pod-1/node-111",
	}
	fabricLinks := []map[string]any{
		{"n1": "111", "s1": "1", "p1": "53", "n2": "101", "s2": "1", "p2": "1"},
	}
	lldp := []map[string]any{
		{"dn": "topology/pod-1/node-111/sys/lldp/inst/if-[eth1/2]/adj-1"},
	}
	cdp := []map[string]any{
		{"dn": "topology/pod-1/node-111/sys/cdp/inst/if-[eth1/3]/adj-1"},
	}
	pathAtts := []map[string]any{
		{"tDn": "topology/pod-1/paths-111/pathep-[eth1/4]"},
		{"tDn": "topology/pod-1/paths-111/pathep-[po1]"},
		{"tDn": "topology/pod-1/protpaths-111-112/pathep-[vpc_x]"}, // vPC: not a single port, skipped
	}
	ethpm := []map[string]any{
		{"dn": "topology/pod-1/node-111/sys/phys-[eth1/5]/phys", "bundleIndex": "po1"},
		{"dn": "topology/pod-1/node-111/sys/phys-[eth1/6]/phys", "bundleIndex": "unspecified"},
	}

	keep := topologyPortDNs(fabricLinks, lldp, cdp, pathAtts, ethpm, numToDN)
	want := []string{
		"topology/pod-1/node-111/sys/phys-[eth1/53]",
		"topology/pod-1/node-101/sys/phys-[eth1/1]",
		"topology/pod-1/node-111/sys/phys-[eth1/2]",
		"topology/pod-1/node-111/sys/phys-[eth1/3]",
		"topology/pod-1/node-111/sys/phys-[eth1/4]",
		"topology/pod-1/node-111/sys/aggr-[po1]",
		"topology/pod-1/node-111/sys/phys-[eth1/5]",
	}
	for _, w := range want {
		if !keep[w] {
			t.Errorf("expected %q in keep set", w)
		}
	}
	if keep["topology/pod-1/node-111/sys/phys-[eth1/6]"] {
		t.Error("unspecified bundleIndex must not add a port")
	}
	if len(keep) != len(want) {
		t.Errorf("keep set size = %d, want %d: %v", len(keep), len(want), keep)
	}
}

func TestPathTargetDN(t *testing.T) {
	numToDN := map[string]string{"121": "topology/pod-1/node-121"}
	tests := []struct {
		in       string
		wantDN   string
		wantKind string
	}{
		{"topology/pod-1/paths-121/pathep-[eth1/10]", "topology/pod-1/node-121/sys/phys-[eth1/10]", "port"},
		{"topology/pod-1/paths-121/pathep-[po5]", "topology/pod-1/node-121/sys/aggr-[po5]", "portchannel"},
		{"topology/pod-1/protpaths-121-122/pathep-[vpc1]", "", "vpc"},
		{"topology/pod-1/paths-999/pathep-[eth1/1]", "", ""}, // unknown node
		{"garbage", "", ""},
	}
	for _, tt := range tests {
		dn, kind := pathTargetDN(tt.in, numToDN)
		if dn != tt.wantDN || kind != tt.wantKind {
			t.Errorf("pathTargetDN(%q) = (%q,%q), want (%q,%q)", tt.in, dn, kind, tt.wantDN, tt.wantKind)
		}
	}
}
