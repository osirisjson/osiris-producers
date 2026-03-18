// transform_test.go - Unit tests for APIC-to-OSIRIS data transformation.
// Covers node, tenant, VRF, bridge domain, subnet, EPG, endpoint, L3Out and
// fault mapping, plus wiring functions that connect resources to groups.
//
// For an introduction to OSIRIS JSON Producer for Cisco see:
// "[OSIRIS-JSON-CISCO]."
//
// [OSIRIS-JSON-CISCO]: https://osirisjson.org/en/docs/producers/cisco

package apic

import (
	"testing"

	"go.osirisjson.org/producers/pkg/sdk"
)

func TestTransformNodes(t *testing.T) {
	nodes := []map[string]any{
		{"dn": "topology/pod-1/node-1", "name": "APIC1", "role": "controller", "serial": "TST00001", "model": "APIC-SERVER-L3", "version": "5.2(8h)", "address": "10.1.0.1", "id": "1", "fabricSt": "unknown"},
		{"dn": "topology/pod-1/node-101", "name": "SPINE1", "role": "spine", "serial": "TST00101", "model": "N9K-C9508", "version": "n9000-15.2(8h)", "address": "10.1.1.101", "id": "101", "fabricSt": "active"},
		{"dn": "topology/pod-1/node-111", "name": "LEAF1", "role": "leaf", "serial": "TST00111", "model": "N9K-C93180YC-FX", "version": "n9000-15.2(8h)", "address": "10.1.1.111", "id": "111", "fabricSt": "active"},
	}

	systems := []map[string]any{
		{"dn": "topology/pod-1/node-1/sys", "oobMgmtAddr": "10.0.0.1", "inbMgmtAddr": "10.2.0.1", "systemUpTime": "100:00:00:00.000", "state": "in-service", "fabricDomain": "TEST_DC"},
		{"dn": "topology/pod-1/node-101/sys", "oobMgmtAddr": "10.0.0.101", "state": "in-service"},
	}

	firmware := []map[string]any{
		{"dn": "topology/pod-1/node-101/sys/fwstatuscont/running", "version": "n9000-15.2(8h)", "peVer": "5.2(8h)"},
	}

	resources := TransformNodes(nodes, systems, firmware)

	if len(resources) != 3 {
		t.Fatalf("expected 3 resources, got %d", len(resources))
	}

	byType := make(map[string]sdk.Resource)
	for _, r := range resources {
		byType[r.Type] = r
	}

	ctrl, ok := byType["network.controller"]
	if !ok {
		t.Fatal("missing controller resource")
	}
	if ctrl.Name != "APIC1" {
		t.Errorf("controller name: expected APIC1, got %s", ctrl.Name)
	}
	if ctrl.Provider.NativeID != "topology/pod-1/node-1" {
		t.Errorf("controller NativeID: %s", ctrl.Provider.NativeID)
	}
	if ctrl.Status != "unknown" {
		t.Errorf("controller status: expected unknown, got %s", ctrl.Status)
	}
	if ctrl.Properties["oob_mgmt_addr"] != "10.0.0.1" {
		t.Errorf("controller oob_mgmt_addr: %v", ctrl.Properties["oob_mgmt_addr"])
	}
	if ctrl.Properties["fabric_domain"] != "TEST_DC" {
		t.Errorf("controller fabric_domain: %v", ctrl.Properties["fabric_domain"])
	}

	spine, ok := byType["network.switch.spine"]
	if !ok {
		t.Fatal("missing spine resource")
	}
	if spine.Status != "active" {
		t.Errorf("spine status: expected active, got %s", spine.Status)
	}
	if spine.Provider.Version != "5.2(8h)" {
		t.Errorf("spine firmware version: %s", spine.Provider.Version)
	}

	if _, ok := byType["network.switch.leaf"]; !ok {
		t.Fatal("missing leaf resource")
	}
}

func TestTransformTenants(t *testing.T) {
	tenants := []map[string]any{
		{"dn": "uni/tn-common", "name": "common", "descr": ""},
		{"dn": "uni/tn-infra", "name": "infra", "descr": ""},
		{"dn": "uni/tn-tn_Example", "name": "tn_Example", "descr": "Example tenant"},
	}

	groups, dnToID := TransformTenants(tenants)

	if len(groups) != 3 {
		t.Fatalf("expected 3 groups, got %d", len(groups))
	}
	if len(dnToID) != 3 {
		t.Fatalf("expected 3 DN mappings, got %d", len(dnToID))
	}

	for _, g := range groups {
		if g.Type != "logical.tenant" {
			t.Errorf("expected type logical.tenant, got %s", g.Type)
		}
	}

	for _, g := range groups {
		if g.Name == "tn_Example" && g.Description != "Example tenant" {
			t.Errorf("Example description: %s", g.Description)
		}
	}
	if _, ok := dnToID["uni/tn-common"]; !ok {
		t.Error("missing DN mapping for uni/tn-common")
	}
}

func TestTransformVRFs(t *testing.T) {
	vrfs := []map[string]any{
		{"dn": "uni/tn-tn_Example/ctx-vrf_Prod_1", "name": "vrf_Prod_1", "descr": "Production VRF 1", "pcEnfPref": "enforced"},
		{"dn": "uni/tn-tn_Example/ctx-vrf_Mgmt_1", "name": "vrf_Mgmt_1", "descr": "Management VRF 1", "pcEnfPref": "enforced"},
	}

	groups, dnToID := TransformVRFs(vrfs)

	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	if len(dnToID) != 2 {
		t.Fatalf("expected 2 DN mappings, got %d", len(dnToID))
	}
	for _, g := range groups {
		if g.Type != "logical.vrf" {
			t.Errorf("expected type logical.vrf, got %s", g.Type)
		}
	}
}

func TestTransformBridgeDomains(t *testing.T) {
	bds := []map[string]any{
		{"dn": "uni/tn-tn_TestCorp/BD-bd_App_Private", "name": "bd_App_Private", "descr": "App bridge domain", "unicastRoute": "yes", "unkMacUcastAct": "proxy", "arpFlood": "yes", "mac": "00:AA:BB:CC:DD:EE"},
	}

	resources, dnToID := TransformBridgeDomains(bds)

	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	if len(dnToID) != 1 {
		t.Fatalf("expected 1 DN mapping, got %d", len(dnToID))
	}

	r := resources[0]
	if r.Type != "network.domain.bridge" {
		t.Errorf("type: %s", r.Type)
	}
	if r.Name != "bd_App_Private" {
		t.Errorf("name: %s", r.Name)
	}
	if r.Properties["unicast_routing"] != "yes" {
		t.Errorf("unicast_routing: %v", r.Properties["unicast_routing"])
	}
}

func TestTransformSubnets(t *testing.T) {
	subnets := []map[string]any{
		{"dn": "uni/tn-tn_TestCorp/BD-bd_App01/subnet-[10.100.0.1/23]", "ip": "10.100.0.1/23", "scope": "public", "preferred": "no"},
	}

	resources := TransformSubnets(subnets)

	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	if resources[0].Name != "10.100.0.1/23" {
		t.Errorf("name: %s", resources[0].Name)
	}
	if resources[0].Properties["ip"] != "10.100.0.1/23" {
		t.Errorf("ip: %v", resources[0].Properties["ip"])
	}
}

func TestTransformEPGs(t *testing.T) {
	epgs := []map[string]any{
		{"dn": "uni/tn-tn_TestCorp/ap-appl_prof_Prod_1/epg-epg_WebTier", "name": "epg_WebTier", "descr": "Web tier EPG"},
	}

	groups, dnToID := TransformEPGs(epgs)

	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if len(dnToID) != 1 {
		t.Fatalf("expected 1 DN mapping, got %d", len(dnToID))
	}
	if groups[0].Type != "logical.epg" {
		t.Errorf("type: %s", groups[0].Type)
	}
}

func TestTransformEndpoints(t *testing.T) {
	endpoints := []map[string]any{
		{"dn": "uni/tn-tn_Lab/ap-appl/epg-epg1/cep-00:11:22:33:44:55", "mac": "00:11:22:33:44:55", "encap": "vlan-914", "fabricPathDn": "topology/pod-2/paths-219/pathep-[eth1/46]"},
	}

	resources := TransformEndpoints(endpoints)

	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	if resources[0].Properties["mac"] != "00:11:22:33:44:55" {
		t.Errorf("normalized mac: %v", resources[0].Properties["mac"])
	}
}

func TestTransformL3Outs_SkipsDummies(t *testing.T) {
	l3outs := []map[string]any{
		{"dn": "uni/tn-tn_Example/out-__ui_svi_dummy_id_0", "name": "__ui_svi_dummy_id_0", "descr": "dummy"},
		{"dn": "uni/tn-tn_Example/out-l3out_Prod_1", "name": "l3out_Prod_1", "descr": "Production L3Out"},
	}

	resources := TransformL3Outs(l3outs)

	if len(resources) != 1 {
		t.Fatalf("expected 1 resource (dummy skipped), got %d", len(resources))
	}
	if resources[0].Name != "l3out_Prod_1" {
		t.Errorf("name: %s", resources[0].Name)
	}
	if resources[0].Type != "network.l3out" {
		t.Errorf("type: %s", resources[0].Type)
	}
}

// ACI extension tests ---

func TestTransformNodes_ACIExtensions(t *testing.T) {
	nodes := []map[string]any{
		{"dn": "topology/pod-1/node-101", "name": "SPINE1", "role": "spine", "serial": "TST00101", "model": "N9K-C9508", "version": "n9000-15.2(8h)", "address": "10.1.1.101", "id": "101", "fabricSt": "active"},
	}
	systems := []map[string]any{
		{"dn": "topology/pod-1/node-101/sys", "oobMgmtAddr": "10.0.0.101", "state": "in-service", "fabricMAC": "AA:BB:CC:DD:EE:01", "controlPlaneMTU": "9000", "lastRebootTime": "2024-04-13T16:47:52.025+00:00", "fabricId": "1"},
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
	if cisco["fabric_mac"] != "AA:BB:CC:DD:EE:01" {
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
		{"dn": "topology/pod-1/node-111", "name": "LEAF1", "role": "leaf", "serial": "TST00111", "model": "N9K-C93180YC-FX", "version": "n9000-15.2(8h)", "address": "10.1.1.111", "id": "111", "fabricSt": "active"},
	}

	resources := TransformNodes(nodes, nil, nil)
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	if resources[0].Extensions != nil {
		t.Error("expected no extensions without topSystem data")
	}
}

// Fault tests ---

func TestTransformFaults_GroupsByDN(t *testing.T) {
	faults := []map[string]any{
		{"dn": "topology/pod-1/node-101/sys/ch/supslot-1/sup/sensor-1/fault-F1527", "code": "F1527", "severity": "warning", "cause": "equipment-full", "descr": "Storage unit full", "created": "2026-02-25T07:32:52.237+00:00", "lastTransition": "2026-02-25T07:49:53.368+00:00", "lc": "raised", "domain": "infra", "subject": "equipment-full"},
		{"dn": "topology/pod-1/node-101/sys/something/fault-F9999", "code": "F9999", "severity": "major", "cause": "other", "descr": "Another fault", "created": "2026-02-25T08:00:00.000+00:00", "lastTransition": "2026-02-25T08:00:00.000+00:00", "lc": "raised", "domain": "infra", "subject": "other"},
		{"dn": "topology/pod-1/node-102/sys/fault-F0001", "code": "F0001", "severity": "minor", "cause": "test", "descr": "Different node", "created": "2026-02-25T09:00:00.000+00:00", "lastTransition": "2026-02-25T09:00:00.000+00:00", "lc": "soaking", "domain": "access", "subject": "test"},
		{"dn": "uni/tn-tn_Example/fault-F2222", "code": "F2222", "severity": "critical", "cause": "config-error", "descr": "Config issue", "created": "2026-02-25T10:00:00.000+00:00", "lastTransition": "2026-02-25T10:00:00.000+00:00", "lc": "raised", "domain": "tenant", "subject": "config"},
	}

	result := TransformFaults(faults)

	// Two node-101 faults grouped together.
	if len(result["topology/pod-1/node-101"]) != 2 {
		t.Errorf("expected 2 faults for node-101, got %d", len(result["topology/pod-1/node-101"]))
	}
	// One node-102 fault.
	if len(result["topology/pod-1/node-102"]) != 1 {
		t.Errorf("expected 1 fault for node-102, got %d", len(result["topology/pod-1/node-102"]))
	}
	// One tenant fault.
	if len(result["uni/tn-tn_Example"]) != 1 {
		t.Errorf("expected 1 fault for tn_Example, got %d", len(result["uni/tn-tn_Example"]))
	}

	// Verify field mapping.
	f := result["topology/pod-1/node-101"][0]
	if f.Code != "F1527" {
		t.Errorf("code: %s", f.Code)
	}
	if f.Severity != "warning" {
		t.Errorf("severity: %s", f.Severity)
	}
	if f.Description != "Storage unit full" {
		t.Errorf("description: %s", f.Description)
	}
	if f.Lifecycle != "raised" {
		t.Errorf("lifecycle: %s", f.Lifecycle)
	}
}

func TestTransformFaults_FilterCleared(t *testing.T) {
	faults := []map[string]any{
		{"dn": "topology/pod-1/node-101/sys/fault-F1111", "code": "F1111", "severity": "cleared", "cause": "resolved", "descr": "Old fault", "created": "2026-01-01T00:00:00.000+00:00", "lastTransition": "2026-02-01T00:00:00.000+00:00", "lc": "cleared", "domain": "infra", "subject": "test"},
		{"dn": "topology/pod-1/node-101/sys/fault-F2222", "code": "F2222", "severity": "warning", "cause": "active", "descr": "Active fault", "created": "2026-02-25T07:00:00.000+00:00", "lastTransition": "2026-02-25T07:00:00.000+00:00", "lc": "raised", "domain": "infra", "subject": "test"},
	}

	result := TransformFaults(faults)

	if len(result["topology/pod-1/node-101"]) != 1 {
		t.Fatalf("expected 1 fault (cleared filtered), got %d", len(result["topology/pod-1/node-101"]))
	}
	if result["topology/pod-1/node-101"][0].Code != "F2222" {
		t.Errorf("expected F2222, got %s", result["topology/pod-1/node-101"][0].Code)
	}
}

func TestTransformFaults_SkipsUnknownDN(t *testing.T) {
	faults := []map[string]any{
		{"dn": "polUni/infra/fault-F0000", "code": "F0000", "severity": "minor", "cause": "test", "descr": "Infra fault", "created": "2026-01-01T00:00:00.000+00:00", "lastTransition": "2026-01-01T00:00:00.000+00:00", "lc": "raised", "domain": "framework", "subject": "test"},
	}

	result := TransformFaults(faults)
	if len(result) != 0 {
		t.Errorf("expected no faults (unmatched DN), got %d groups", len(result))
	}
}

func TestWireFaultsToNodes(t *testing.T) {
	resources := []sdk.Resource{
		{ID: "res-node-101", Type: "network.switch.spine", Provider: sdk.Provider{NativeID: "topology/pod-1/node-101"}},
		{ID: "res-node-102", Type: "network.switch.leaf", Provider: sdk.Provider{NativeID: "topology/pod-1/node-102"}},
	}
	faultsByDN := map[string][]Fault{
		"topology/pod-1/node-101": {{Code: "F1527", Severity: "warning"}},
	}

	WireFaultsToNodes(resources, faultsByDN)

	// Node 101 should have faults.
	if resources[0].Extensions == nil {
		t.Fatal("expected extensions on node-101")
	}
	cisco := resources[0].Extensions["osiris.cisco"].(map[string]any)
	faults, ok := cisco["faults"].([]Fault)
	if !ok || len(faults) != 1 {
		t.Fatalf("expected 1 fault on node-101, got %v", cisco["faults"])
	}
	if faults[0].Code != "F1527" {
		t.Errorf("fault code: %s", faults[0].Code)
	}

	// Node 102 should have no extensions.
	if resources[1].Extensions != nil {
		t.Error("expected no extensions on node-102")
	}
}

func TestWireFaultsToNodes_MergesWithExistingExtensions(t *testing.T) {
	resources := []sdk.Resource{
		{
			ID:       "res-node-101",
			Type:     "network.switch.spine",
			Provider: sdk.Provider{NativeID: "topology/pod-1/node-101"},
			Extensions: map[string]any{
				"osiris.cisco": map[string]any{"fabric_id": 1},
			},
		},
	}
	faultsByDN := map[string][]Fault{
		"topology/pod-1/node-101": {{Code: "F1527", Severity: "warning"}},
	}

	WireFaultsToNodes(resources, faultsByDN)

	cisco := resources[0].Extensions["osiris.cisco"].(map[string]any)
	if cisco["fabric_id"] != 1 {
		t.Errorf("existing extension lost: fabric_id=%v", cisco["fabric_id"])
	}
	faults, ok := cisco["faults"].([]Fault)
	if !ok || len(faults) != 1 {
		t.Fatal("expected 1 fault merged into existing extensions")
	}
}

func TestWireFaultsToTenants(t *testing.T) {
	groups := []sdk.Group{
		{ID: "group-tenant-example", Type: "logical.tenant", Name: "tn_Example"},
		{ID: "group-tenant-common", Type: "logical.tenant", Name: "common"},
	}
	tenantDNToID := map[string]string{
		"uni/tn-tn_Example": "group-tenant-example",
		"uni/tn-common":     "group-tenant-common",
	}
	faultsByDN := map[string][]Fault{
		"uni/tn-tn_Example": {{Code: "F2222", Severity: "critical"}},
	}

	WireFaultsToTenants(groups, tenantDNToID, faultsByDN)

	// Example tenant should have faults.
	if groups[0].Extensions == nil {
		t.Fatal("expected extensions on tn_Example")
	}
	cisco := groups[0].Extensions["osiris.cisco"].(map[string]any)
	faults, ok := cisco["faults"].([]Fault)
	if !ok || len(faults) != 1 {
		t.Fatalf("expected 1 fault on tn_Example, got %v", cisco["faults"])
	}

	// Common tenant should have no extensions.
	if groups[1].Extensions != nil {
		t.Error("expected no extensions on common tenant")
	}
}

func TestFaultDNPrefix(t *testing.T) {
	tests := []struct {
		dn   string
		want string
	}{
		{"topology/pod-1/node-101/sys/ch/fault-F1527", "topology/pod-1/node-101"},
		{"topology/pod-2/node-201/sys/something", "topology/pod-2/node-201"},
		{"uni/tn-tn_Example/fault-F2222", "uni/tn-tn_Example"},
		{"uni/tn-common/ap-app1/fault-F3333", "uni/tn-common"},
		{"polUni/infra/fault-F0000", ""},
		{"topology/pod-1/fault-no-node", ""},
	}

	for _, tt := range tests {
		got := faultDNPrefix(tt.dn)
		if got != tt.want {
			t.Errorf("faultDNPrefix(%q) = %q, want %q", tt.dn, got, tt.want)
		}
	}
}

// Wiring tests.

func TestWireBDsToTenants(t *testing.T) {
	bdAttrs := []map[string]any{
		{"dn": "uni/tn-tn_Example/BD-bd1"},
		{"dn": "uni/tn-tn_Example/BD-bd2"},
	}
	bdDNToID := map[string]string{
		"uni/tn-tn_Example/BD-bd1": "res-bd1",
		"uni/tn-tn_Example/BD-bd2": "res-bd2",
	}
	tenantDNToID := map[string]string{
		"uni/tn-tn_Example": "group-tenant-example",
	}
	tenantGroups := []sdk.Group{
		{ID: "group-tenant-example", Type: "logical.tenant", Name: "tn_Example"},
	}

	WireBDsToTenants(bdAttrs, bdDNToID, tenantDNToID, tenantGroups)

	if len(tenantGroups[0].Members) != 2 {
		t.Fatalf("expected 2 BD members, got %d", len(tenantGroups[0].Members))
	}
}

func TestWireVRFsToTenants(t *testing.T) {
	vrfAttrs := []map[string]any{
		{"dn": "uni/tn-tn_Example/ctx-vrf1"},
	}
	vrfDNToID := map[string]string{
		"uni/tn-tn_Example/ctx-vrf1": "group-vrf1",
	}
	tenantDNToID := map[string]string{
		"uni/tn-tn_Example": "group-tenant-example",
	}
	tenantGroups := []sdk.Group{
		{ID: "group-tenant-example", Type: "logical.tenant", Name: "tn_Example"},
	}

	WireVRFsToTenants(vrfAttrs, vrfDNToID, tenantDNToID, tenantGroups)

	if len(tenantGroups[0].Children) != 1 {
		t.Fatalf("expected 1 VRF child, got %d", len(tenantGroups[0].Children))
	}
	if tenantGroups[0].Children[0] != "group-vrf1" {
		t.Errorf("child: %s", tenantGroups[0].Children[0])
	}
}

func TestWireEPGsToTenants(t *testing.T) {
	epgAttrs := []map[string]any{
		{"dn": "uni/tn-tn_Example/ap-app1/epg-epg_WEB"},
	}
	epgDNToID := map[string]string{
		"uni/tn-tn_Example/ap-app1/epg-epg_WEB": "group-epg-web",
	}
	tenantDNToID := map[string]string{
		"uni/tn-tn_Example": "group-tenant-example",
	}
	tenantGroups := []sdk.Group{
		{ID: "group-tenant-example", Type: "logical.tenant", Name: "tn_Example"},
	}

	WireEPGsToTenants(epgAttrs, epgDNToID, tenantDNToID, tenantGroups)

	if len(tenantGroups[0].Children) != 1 {
		t.Fatalf("expected 1 EPG child, got %d", len(tenantGroups[0].Children))
	}
}

func TestWireEndpointsToEPGs(t *testing.T) {
	epAttrs := []map[string]any{
		{"dn": "uni/tn-tn_Example/ap-app1/epg-epg_WEB/cep-AA:BB:CC:DD:EE:FF"},
		{"dn": "uni/tn-tn_Example/ap-app1/epg-epg_WEB/cep-11:22:33:44:55:66"},
	}
	epgDNToID := map[string]string{
		"uni/tn-tn_Example/ap-app1/epg-epg_WEB": "group-epg-web",
	}
	epgGroups := []sdk.Group{
		{ID: "group-epg-web", Type: "logical.epg", Name: "epg_WEB"},
	}

	WireEndpointsToEPGs(epAttrs, epgDNToID, epgGroups)

	if len(epgGroups[0].Members) != 2 {
		t.Fatalf("expected 2 endpoint members, got %d", len(epgGroups[0].Members))
	}
}

func TestWireL3OutsToTenants(t *testing.T) {
	l3outAttrs := []map[string]any{
		{"dn": "uni/tn-tn_Example/out-__ui_svi_dummy_id_0", "name": "__ui_svi_dummy_id_0"},
		{"dn": "uni/tn-tn_Example/out-l3out_Prod", "name": "l3out_Prod"},
	}
	tenantDNToID := map[string]string{
		"uni/tn-tn_Example": "group-tenant-example",
	}
	tenantGroups := []sdk.Group{
		{ID: "group-tenant-example", Type: "logical.tenant", Name: "tn_Example"},
	}

	WireL3OutsToTenants(l3outAttrs, tenantDNToID, tenantGroups)

	// Only 1 member (dummy skipped).
	if len(tenantGroups[0].Members) != 1 {
		t.Fatalf("expected 1 L3Out member (dummy skipped), got %d", len(tenantGroups[0].Members))
	}
}

// Helper tests.

func TestExtractTenantDN(t *testing.T) {
	tests := []struct {
		dn   string
		want string
	}{
		{"uni/tn-tn_Example/BD-bd1", "uni/tn-tn_Example"},
		{"uni/tn-common/ctx-vrf1", "uni/tn-common"},
		{"uni/tn-tn_TestCorp/BD-bd_App01/subnet-[10.0.0.1/24]", "uni/tn-tn_TestCorp"},
		{"topology/pod-1/node-1", ""},
	}

	for _, tt := range tests {
		got := extractTenantDN(tt.dn)
		if got != tt.want {
			t.Errorf("extractTenantDN(%q) = %q, want %q", tt.dn, got, tt.want)
		}
	}
}

func TestExtractEPGDN(t *testing.T) {
	tests := []struct {
		dn   string
		want string
	}{
		{"uni/tn-tn_Example/ap-app1/epg-epg_WEB/cep-AA:BB:CC:DD:EE:FF", "uni/tn-tn_Example/ap-app1/epg-epg_WEB"},
		{"uni/tn-tn_Example/ap-app1/epg-epg_DB/cep-11:22:33:44:55:66", "uni/tn-tn_Example/ap-app1/epg-epg_DB"},
		{"uni/tn-tn_Example/ap-app1/epg-epg_WEB", ""}, // no cep segment
	}

	for _, tt := range tests {
		got := extractEPGDN(tt.dn)
		if got != tt.want {
			t.Errorf("extractEPGDN(%q) = %q, want %q", tt.dn, got, tt.want)
		}
	}
}

func TestDnPrefix(t *testing.T) {
	tests := []struct {
		dn   string
		want string
	}{
		{"topology/pod-1/node-1/sys", "topology/pod-1/node-1"},
		{"topology/pod-2/node-201/sys/fwstatuscont/running", "topology/pod-2/node-201"},
		{"topology/pod-1/node-101", "topology/pod-1/node-101"},
	}

	for _, tt := range tests {
		got := dnPrefix(tt.dn)
		if got != tt.want {
			t.Errorf("dnPrefix(%q) = %q, want %q", tt.dn, got, tt.want)
		}
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

func TestResourceID_Deterministic(t *testing.T) {
	id1 := resourceID("network.controller", "topology/pod-1/node-1")
	id2 := resourceID("network.controller", "topology/pod-1/node-1")
	if id1 != id2 {
		t.Errorf("resourceID not deterministic: %q != %q", id1, id2)
	}

	id3 := resourceID("network.controller", "topology/pod-1/node-2")
	if id1 == id3 {
		t.Errorf("different DNs should produce different IDs")
	}
}
