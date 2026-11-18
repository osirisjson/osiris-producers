// hyperscalers_azure_test.go - Positive, negative and boundary tests
// for the Microsoft Azure identity rules (sections 5.1 and 5.2):
// approved and rejected GUIDs, the nil UUID exception path, several
// subscriptions under one tenant, and ARM paths with reserved,
// composite and nested names.
//
// The package scans its own sources, so every non-approved sample is
// assembled at runtime by the helpers in hyperscalers_test.go.
//
// OSIRIS JSON Producers Test Data Policy:
// [OSIRIS-PRODUCER-TEST-DATA-POLICY]: https://github.com/osirisjson/osiris-producers/blob/main/docs/policies/OSIRIS-PRODUCER-TEST-DATA-POLICY.md

package testdatapolicy

import (
	"fmt"
	"strings"
	"testing"
)

func TestCheckGUID(t *testing.T) {
	for _, g := range []string{tenantA, subA, subB, subC, objA,
		"00000000-0000-5000-8000-000000000022", "00000000-0000-1000-8000-00000000FFFF"} {
		if msg, ok := checkGUID(g); !ok {
			t.Errorf("checkGUID(%s): %s", g, msg)
		}
	}
	bad := []string{
		guidOf("00000000", "0000", "0000", "0000", "000000000011"), // retired generic form
		guidOf("00000000", "0000", "6000", "8000", "000000000011"), // version 6
		guidOf("00000000", "0000", "0000", "8000", "000000000011"), // version 0
		guidOf("00000000", "0000", "4000", "9000", "000000000011"), // variant 9
		guidOf("00000000", "0000", "4000", "8000", "000000010011"), // suffix wider than 4
		guidOf("11111111", "1111", "1111", "1111", "111111111111"), // repeated digits
		guidOf("a1b2c3d4", "e5f6", "4890", "8bcd", "ef1234567890"), // hand-typed
	}
	for _, g := range bad {
		if _, ok := checkGUID(g); ok {
			t.Errorf("checkGUID(%s) accepted", g)
		}
	}
	if msg, ok := checkGUID(nilUUID); ok || !strings.Contains(msg, "nil UUID") {
		t.Errorf("nil UUID: ok=%v msg=%q", ok, msg)
	}
}

func TestNilUUIDNeedsExactException(t *testing.T) {
	line := fmt.Sprintf("{%q: %q}\n", "tenant"+"Id", nilUUID)
	m := newMem("x_test.go", line)
	rep := scanMem(t, m)
	if len(rep.Violations) != 1 || rep.Violations[0].Rule != RuleAzureGUID {
		t.Fatalf("nil UUID without exception: %v", rep.Violations)
	}
	e := Exception{
		Path: "x_test.go", Rule: RuleAzureGUID, Fingerprint: rep.Violations[0].Fingerprint,
		Reason: "Verifies the nil tenant identity is treated as unset", Owner: "maintainers",
		Issue: "TDP-3", ReviewBy: "2027-11-15",
	}
	if rep := scanMem(t, withAllowlist(t, m, e)); rep.Failed() || len(rep.Excepted) != 1 {
		t.Errorf("nil UUID with exact exception: violations %v problems %v", rep.Violations, rep.ExceptionProblems)
	}
	// A second occurrence needs its own entry.
	m2 := newMem("x_test.go", line+line)
	if rep := scanMem(t, withAllowlist(t, m2, e)); len(rep.Violations) != 1 {
		t.Errorf("second nil UUID occurrence suppressed by one entry: %v", rep.Violations)
	}
	e.Fingerprint = "sha256:" + strings.Repeat("0", 32)
	if rep := scanMem(t, withAllowlist(t, m, e)); len(rep.Violations) != 1 || rep.Stale != 1 {
		t.Errorf("mismatched exception: violations %v stale %d", rep.Violations, rep.Stale)
	}
}

func TestMultipleSubscriptionsOneTenant(t *testing.T) {
	lines := []string{
		fmt.Sprintf(`{"tenantId": %q, "subscriptionId": %q}`, tenantA, subA),
		fmt.Sprintf(`{"tenantId": %q, "subscriptionId": %q}`, tenantA, subB),
		fmt.Sprintf(`{"id": %q}`, arm(subC, "resourceGroups", "RG-LAB-HUB-NET", "providers", "Microsoft.Network", "virtualNetworks", "LAB-VNET-HUB")),
	}
	for _, l := range lines {
		if got := hyperscalerRules(t, "testdata/x.json", l); len(got) != 0 {
			t.Errorf("%s: %v", l, got)
		}
	}
	bad := fmt.Sprintf(`{"tenantId": %q}`, guidOf("22222222", "2222", "2222", "2222", "222222222222"))
	if got := hyperscalerRules(t, "testdata/x.json", bad); len(got) != 1 || got[0] != RuleAzureGUID {
		t.Errorf("unapproved tenant: %v", got)
	}
}

func TestARMPaths(t *testing.T) {
	rg := "RG-LAB-HUB-NET"
	vnet := []string{"resourceGroups", rg, "providers", "Microsoft.Network", "virtualNetworks", "LAB-VNET-HUB"}
	tests := []struct {
		name string
		path string
		want []string
	}{
		{"valid vnet", arm(subA, vnet...), nil},
		{"reserved subnet", arm(subA, append(vnet, "subnets", "GatewaySubnet")...), nil},
		{"reserved subnet any case", arm(subA, append(vnet, "subnets", "azurebastionsubnet")...), nil},
		{"reserved name outside its type", arm(subA, "resourceGroups", rg, "providers", "Microsoft.Compute", "virtualMachines", "Gateway"+"Subnet"), []string{RuleHyperscalerName}},
		{"default child", arm(subA, "resourceGroups", rg, "providers", "Microsoft.Storage", "storageAccounts", "labstorage01", "blobServices", "default"), nil},
		{"default top level", arm(subA, "resourceGroups", rg, "providers", "Microsoft.Compute", "virtualMachines", "def"+"ault"), []string{RuleHyperscalerName}},
		{"ipconfig1 in context", arm(subA, append(vnet, "ipConfigurations", "ipconfig1")...), nil},
		{"ipconfig1 out of context", arm(subA, "resourceGroups", rg, "providers", "Microsoft.Compute", "disks", "ip"+"config1"), []string{RuleHyperscalerName}},
		{"managed API", arm(subA, "providers", "Microsoft.Web", "locations", "westeurope", "managedApis", "azureblob"), nil},
		{"nested extension provider", arm(subA, "resourceGroups", rg, "providers", "Microsoft.Network", "networkSecurityGroups", "LAB-NSG", "providers", "microsoft.insights", "diagnosticSettings", "LAB-TO-LA"), nil},
		{"backup composite ok", arm(subA, "resourceGroups", rg, "providers", "Microsoft.RecoveryServices", "vaults", "LAB-RSV01", "backupFabrics", "default", "protectionContainers", "iaasvmcontainerv2;rg-lab-hub-nva;lab-nva01"), nil},
		{"backup composite bad vm", arm(subA, "resourceGroups", rg, "providers", "Microsoft.RecoveryServices", "vaults", "LAB-RSV01", "backupFabrics", "default", "protectionContainers", "iaasvmcontainerv2;rg-lab-hub-nva;"+"nva01"), []string{RuleHyperscalerName}},
		{"generic resource group", arm(subA, "resourceGroups", "rg"+"1"), []string{RuleHyperscalerName}},
		{"resource group without LAB", arm(subA, "resourceGroups", "RG-"+"HUB-NET"), []string{RuleHyperscalerName}},
		{"generic resource name", arm(subA, "resourceGroups", rg, "providers", "Microsoft.Network", "virtualNetworks", "vnet"+"1"), []string{RuleHyperscalerName}},
		{"guid name segment", arm(subA, "resourceGroups", rg, "providers", "Microsoft.Authorization", "roleAssignments", objA), nil},
		{"non-guid subscription", arm("sub"+"-123", "resourceGroups", rg), []string{RuleAzureARM}},
		{"unapproved subscription", arm(guidOf("00000000", "0000", "0000", "0000", "000000000011"), "resourceGroups", rg), []string{RuleAzureGUID}},
		{"template", "/subscriptions/%s/resourceGroups/{rg}/providers/Microsoft.Network/virtualNetworks/%s", nil},
		{"uppercase prefix", strings.ToUpper(arm(subA, "resourceGroups", rg)), nil},
	}
	for _, tt := range tests {
		got := hyperscalerRules(t, "testdata/x.json", fmt.Sprintf(`{"id": %q}`, tt.path))
		if strings.Join(got, ",") != strings.Join(tt.want, ",") {
			t.Errorf("%s: rules %v, want %v (%s)", tt.name, got, tt.want, tt.path)
		}
	}
}
