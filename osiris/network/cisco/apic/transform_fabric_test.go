// transform_fabric_test.go - Tests for the APIC fabric-node transform.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package apic

import (
	"testing"

	"go.osirisjson.org/producers/pkg/sdk"
)

func TestTransformNodes(t *testing.T) {
	nodes := []map[string]any{
		{"dn": "topology/pod-1/node-1", "name": "LAB-APIC1", "role": "controller", "serial": "TST00001", "model": "APIC-SERVER-L3", "version": "5.2(8h)", "address": "192.0.2.1", "id": "1", "fabricSt": "unknown"},
		{"dn": "topology/pod-1/node-101", "name": "LAB-SPINE1", "role": "spine", "serial": "TST00101", "model": "N9K-C9508", "version": "n9000-15.2(8h)", "address": "192.0.2.101", "id": "101", "fabricSt": "active"},
		{"dn": "topology/pod-1/node-111", "name": "LAB-LEAF1", "role": "leaf", "serial": "TST00111", "model": "N9K-C93180YC-FX", "version": "n9000-15.2(8h)", "address": "192.0.2.111", "id": "111", "fabricSt": "active"},
	}

	systems := []map[string]any{
		{"dn": "topology/pod-1/node-1/sys", "oobMgmtAddr": "198.51.100.1", "inbMgmtAddr": "198.51.100.201", "systemUpTime": "100:00:00:00.000", "state": "in-service", "fabricDomain": "MXP"},
		{"dn": "topology/pod-1/node-101/sys", "oobMgmtAddr": "198.51.100.101", "state": "in-service"},
	}

	firmware := []map[string]any{
		{"dn": "topology/pod-1/node-101/sys/fwstatuscont/running", "version": "n9000-15.2(8h)", "peVer": "5.2(8h)"},
	}

	resources := TransformNodes(nodes, systems, firmware)

	if len(resources) != 3 {
		t.Fatalf("expected 3 resources, got %d", len(resources))
	}

	// Leaf and spine share the core network.switch type; index nodes by
	// role instead so both stay reachable.
	byRole := make(map[string]sdk.Resource)
	for _, r := range resources {
		if role, _ := r.Properties["role"].(string); role != "" {
			byRole[role] = r
		}
	}

	ctrl, ok := byRole["controller"]
	if !ok {
		t.Fatal("missing controller resource")
	}
	if ctrl.Type != "compute.server" {
		t.Errorf("controller type: %s", ctrl.Type)
	}
	if ctrl.Name != "LAB-APIC1" {
		t.Errorf("controller name: expected LAB-APIC1, got %s", ctrl.Name)
	}
	if ctrl.ID != "cisco.apic::topology/pod-1/node-1" {
		t.Errorf("controller ID: %s", ctrl.ID)
	}
	if ctrl.Provider.NativeID != "topology/pod-1/node-1" {
		t.Errorf("controller NativeID: %s", ctrl.Provider.NativeID)
	}
	if ctrl.Provider.Type != "fabricNode" {
		t.Errorf("controller provider.type: %s", ctrl.Provider.Type)
	}
	if ctrl.Provider.Site != "" {
		t.Errorf("provider.site must not carry fabric state: %q", ctrl.Provider.Site)
	}
	if ctrl.Status != "active" {
		t.Errorf("controller status: expected active, got %s", ctrl.Status)
	}
	if ctrl.Properties["manufacturer"] != "Cisco" {
		t.Errorf("controller manufacturer: %v", ctrl.Properties["manufacturer"])
	}
	if ctrl.Properties["oob_mgmt_addr"] != "198.51.100.1" {
		t.Errorf("controller oob_mgmt_addr: %v", ctrl.Properties["oob_mgmt_addr"])
	}
	if ctrl.Properties["fabric_domain"] != "MXP" {
		t.Errorf("controller fabric_domain: %v", ctrl.Properties["fabric_domain"])
	}

	spine, ok := byRole["spine"]
	if !ok {
		t.Fatal("missing spine resource")
	}
	if spine.Type != "network.switch" {
		t.Errorf("spine type: expected network.switch, got %s", spine.Type)
	}
	if spine.Status != "active" {
		t.Errorf("spine status: expected active, got %s", spine.Status)
	}
	if spine.Provider.Version != "5.2(8h)" {
		t.Errorf("spine firmware version: %s", spine.Provider.Version)
	}

	leaf, ok := byRole["leaf"]
	if !ok {
		t.Fatal("missing leaf resource")
	}
	if leaf.Type != "network.switch" {
		t.Errorf("leaf type: expected network.switch, got %s", leaf.Type)
	}
}

func TestControllerType(t *testing.T) {
	cases := []struct {
		name string
		sys  map[string]any
		want string
	}{
		{"physical appliance", map[string]any{"virtualMode": "no"}, "compute.server"},
		{"virtual APIC", map[string]any{"virtualMode": "yes"}, "compute.vm"},
		{"no topSystem match", nil, "compute.server"},
		{"virtualMode absent", map[string]any{"state": "in-service"}, "compute.server"},
	}
	for _, c := range cases {
		if got := controllerType(c.sys); got != c.want {
			t.Errorf("%s: controllerType = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestTransformNodes_VirtualController(t *testing.T) {
	nodes := []map[string]any{
		{"dn": "topology/pod-1/node-1", "name": "LAB-VAPIC1", "role": "controller", "id": "1", "fabricSt": "active"},
	}
	systems := []map[string]any{
		{"dn": "topology/pod-1/node-1/sys", "state": "in-service", "virtualMode": "yes"},
	}
	resources := TransformNodes(nodes, systems, nil)
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	if resources[0].Type != "compute.vm" {
		t.Errorf("virtual APIC type: expected compute.vm, got %s", resources[0].Type)
	}
	if role, _ := resources[0].Properties["role"].(string); role != "controller" {
		t.Errorf("virtual APIC properties.role: expected controller, got %q", role)
	}
}

func TestTransformNodes_ACIExtensions(t *testing.T) {
	nodes := []map[string]any{
		{"dn": "topology/pod-1/node-101", "name": "LAB-SPINE1", "role": "spine", "serial": "TST00101", "model": "N9K-C9508", "version": "n9000-15.2(8h)", "address": "192.0.2.101", "id": "101", "fabricSt": "active"},
	}
	systems := []map[string]any{
		{"dn": "topology/pod-1/node-101/sys", "oobMgmtAddr": "198.51.100.101", "state": "in-service", "fabricMAC": "AA:BB:CC:DD:00:01", "controlPlaneMTU": "9000", "lastRebootTime": "2024-04-13T16:47:52.025+00:00", "fabricId": "1"},
	}
	firmware := []map[string]any{}

	resources := TransformNodes(nodes, systems, firmware)
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}

	r := resources[0]
	if r.Extensions == nil {
		t.Fatal("expected extensions on node with topSystem data")
	}
	cisco, ok := r.Extensions["osiris.cisco"].(map[string]any)
	if !ok {
		t.Fatal("expected osiris.cisco extension map")
	}
	if cisco["fabric_mac"] != "AA:BB:CC:DD:00:01" {
		t.Errorf("fabric_mac: %v", cisco["fabric_mac"])
	}
	if cisco["control_plane_mtu"] != 9000 {
		t.Errorf("control_plane_mtu: %v", cisco["control_plane_mtu"])
	}
	if cisco["last_reboot_time"] != "2024-04-13T16:47:52.025+00:00" {
		t.Errorf("last_reboot_time: %v", cisco["last_reboot_time"])
	}
	if cisco["fabric_id"] != 1 {
		t.Errorf("fabric_id: %v", cisco["fabric_id"])
	}
}

func TestTransformNodes_NoExtensionsWithoutTopSystem(t *testing.T) {
	nodes := []map[string]any{
		{"dn": "topology/pod-1/node-111", "name": "LAB-LEAF1", "role": "leaf", "serial": "TST00111", "model": "N9K-C93180YC-FX", "version": "n9000-15.2(8h)", "address": "192.0.2.111", "id": "111", "fabricSt": "active"},
	}

	resources := TransformNodes(nodes, nil, nil)
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	if resources[0].Extensions != nil {
		t.Error("expected no extensions without topSystem data")
	}
}

func TestMapNodeStatus(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"active", "active"},
		{"inactive", "inactive"},
		{"disabled", "inactive"},
		{"unknown", "unknown"},
		{"", "unknown"},
	}
	for _, tt := range tests {
		got := mapNodeStatus(tt.in)
		if got != tt.want {
			t.Errorf("mapNodeStatus(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
