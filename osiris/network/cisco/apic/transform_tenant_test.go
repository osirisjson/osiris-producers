// transform_tenant_test.go - Tests for the APIC tenant object-model
// transform and wiring.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package apic

import (
	"testing"

	"go.osirisjson.org/producers/pkg/sdk"
)

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
		{"dn": "uni/tn-tn_TestCorp/BD-bd_App_Private", "name": "bd_App_Private", "descr": "App bridge domain", "unicastRoute": "yes", "unkMacUcastAct": "proxy", "arpFlood": "yes", "mac": "00:00:5E:00:53:DD"},
	}

	resources, dnToID := TransformBridgeDomains(bds)

	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	if len(dnToID) != 1 {
		t.Fatalf("expected 1 DN mapping, got %d", len(dnToID))
	}

	r := resources[0]
	if r.Type != "osiris.cisco.domain.bridge" {
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
		{"dn": "uni/tn-tn_TestCorp/BD-bd_App01/subnet-[203.0.113.1/24]", "ip": "203.0.113.1/24", "scope": "public,shared", "preferred": "no"},
	}

	resources := TransformSubnets(subnets)

	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	r := resources[0]
	if r.ID != "cisco.apic::uni/tn-tn_TestCorp/BD-bd_App01/subnet-[203.0.113.1/24]" {
		t.Errorf("id: %s", r.ID)
	}
	if r.Name != "203.0.113.0/24" {
		t.Errorf("name: %s", r.Name)
	}
	if r.Properties["cidr"] != "203.0.113.0/24" {
		t.Errorf("cidr: %v", r.Properties["cidr"])
	}
	if r.Properties["gateway_ip"] != "203.0.113.1" {
		t.Errorf("gateway_ip: %v", r.Properties["gateway_ip"])
	}
	if _, ok := r.Properties["ip"]; ok {
		t.Errorf("raw ip property should be dropped: %v", r.Properties["ip"])
	}
	if _, ok := r.Properties["scope"]; ok {
		t.Error("ACI scope must not sit in core properties")
	}
	cisco, ok := r.Extensions[extensionNamespace].(map[string]any)
	if !ok {
		t.Fatal("subnet should carry an osiris.cisco extension")
	}
	if cisco["aci_scope"] != "public,shared" {
		t.Errorf("aci_scope: %v", cisco["aci_scope"])
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
	if groups[0].Type != "osiris.cisco.epg" {
		t.Errorf("type: %s", groups[0].Type)
	}
}

func TestTransformL3Outs_SkipsDummies(t *testing.T) {
	l3outs := []map[string]any{
		{"dn": "uni/tn-tn_Example/out-__ui_svi_dummy_id_0", "name": "__ui_svi_dummy_id_0", "descr": "dummy"},
		{"dn": "uni/tn-tn_Example/out-l3out_Prod_1", "name": "l3out_Prod_1", "descr": "Production L3Out"},
	}

	resources, _ := TransformL3Outs(l3outs)

	if len(resources) != 1 {
		t.Fatalf("expected 1 resource (dummy skipped), got %d", len(resources))
	}
	if resources[0].Name != "l3out_Prod_1" {
		t.Errorf("name: %s", resources[0].Name)
	}
	if resources[0].Type != "osiris.cisco.l3out" {
		t.Errorf("type: %s", resources[0].Type)
	}
}

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

func TestExtractTenantDN(t *testing.T) {
	tests := []struct {
		dn   string
		want string
	}{
		{"uni/tn-tn_Example/BD-bd1", "uni/tn-tn_Example"},
		{"uni/tn-common/ctx-vrf1", "uni/tn-common"},
		{"uni/tn-tn_TestCorp/BD-bd_App01/subnet-[203.0.113.1/24]", "uni/tn-tn_TestCorp"},
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
		{"uni/tn-tn_Example/ap-app1/epg-epg_WEB/cep-00:00:5E:00:53:AA", "uni/tn-tn_Example/ap-app1/epg-epg_WEB"},
		{"uni/tn-tn_Example/ap-app1/epg-epg_DB/cep-00:00:5E:00:53:BB", "uni/tn-tn_Example/ap-app1/epg-epg_DB"},
		{"uni/tn-tn_Example/ap-app1/epg-epg_WEB", ""}, // no cep segment
	}

	for _, tt := range tests {
		got := extractEPGDN(tt.dn)
		if got != tt.want {
			t.Errorf("extractEPGDN(%q) = %q, want %q", tt.dn, got, tt.want)
		}
	}
}
