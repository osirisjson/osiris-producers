// transform_faults_test.go - Tests for the APIC fault transform
// and fault wiring.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package apic

import (
	"testing"

	"go.osirisjson.org/producers/pkg/sdk"
)

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
		{ID: "res-node-101", Type: "network.switch", Provider: sdk.Provider{NativeID: "topology/pod-1/node-101"}},
		{ID: "res-node-102", Type: "network.switch", Provider: sdk.Provider{NativeID: "topology/pod-1/node-102"}},
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
			Type:     "network.switch",
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
