// hyperscalers_test.go - Shared helpers and cross-provider tests for
// the hyperscaler identity rules (section 5): service naming
// restrictions, identity field names and file scope, provider metadata
// and relationship preservation after synthetic-ID replacement. The
// provider tests are in hyperscalers_azure_test.go and
// hyperscalers_aws_test.go.
//
// The package scans its own sources, so every non-approved sample is
// assembled at runtime by the helpers below, which the provider tests
// share.
//
// OSIRIS JSON Producers Test Data Policy:
// [OSIRIS-PRODUCER-TEST-DATA-POLICY]: https://github.com/osirisjson/osiris-producers/blob/main/docs/policies/OSIRIS-PRODUCER-TEST-DATA-POLICY.md

package testdatapolicy

import (
	"fmt"
	"strings"
	"testing"
)

// Approved synthetic identities used across the tests.
const (
	tenantA = "00000000-0000-1000-8000-000000000001"
	subA    = "00000000-0000-1000-8000-000000000011"
	subB    = "00000000-0000-2000-8000-000000000012"
	subC    = "00000000-0000-3000-8000-000000000013"
	objA    = "00000000-0000-4000-8000-000000000021"
	acctA   = "123456789012"
	acctB   = "111122223333"
	acctC   = "444455556666"
)

// arm assembles an ARM path from its segments.
func arm(sub string, segs ...string) string {
	return "/subscriptions/" + sub + "/" + strings.Join(segs, "/")
}

// arn assembles an ARN.
func arn(service, region, account, resource string) string {
	return strings.Join([]string{"arn", "aws", service, region, account, resource}, ":")
}

// awsID assembles an AWS resource ID from its family and suffix.
func awsID(family, suffix string) string { return strings.Join([]string{family, suffix}, "-") }

// guidOf assembles a GUID from its five groups.
func guidOf(a, b, c, d, e string) string { return strings.Join([]string{a, b, c, d, e}, "-") }

// hyperscalerRules scans one line of a file and returns the hyperscaler
// identity rule IDs found, in order.
func hyperscalerRules(t *testing.T, path, line string) []string {
	t.Helper()
	fs, _ := scanText(path, []byte(line))
	var out []string
	for _, f := range fs {
		switch f.Rule {
		case RuleAzureGUID, RuleAzureARM, RuleAWSAccount, RuleAWSResource, RuleHyperscalerName:
			out = append(out, f.Rule)
		}
	}
	return out
}

func TestServiceNaming(t *testing.T) {
	tests := []struct {
		service, name string
		ok            bool
	}{
		{"storageaccounts", "labstorage01", true},
		{"storageaccounts", "LabStorage01", false},
		{"storageaccounts", "lab-storage-01", false},
		{"storageaccounts", "lab" + strings.Repeat("s", 22), false},
		{"vaults", "LAB-KV-01", true},
		{"vaults", "LAB-KV-01-", false},
		{"s3", "lab-test-bucket-01", true},
		{"s3", "lab", true},
		{"s3", "la", false},
		{"s3", "lab-Bucket", false},
		{"iam", "LAB-ROLE-01", true},
		{"iam", "LAB" + strings.Repeat("r", 62), false},
		{"lambda", "LAB-FN-01", true},
		{"lambda", "LAB.FN", false},
		{"", "TEST-anything", true},
		{"", "prod" + "-vm", false},
	}
	for _, tt := range tests {
		if _, ok := checkHyperscalerName(tt.service, tt.name); ok != tt.ok {
			t.Errorf("checkHyperscalerName(%q, %q) = %v, want %v", tt.service, tt.name, ok, tt.ok)
		}
	}
	for _, rg := range []string{"RG-LAB", "RG-LAB-HUB-NET", "RG-TEST-01", "rg_fake_x", "LAB-RG", "stub-rg"} {
		if msg, ok := checkResourceGroup(rg); !ok {
			t.Errorf("checkResourceGroup(%q): %s", rg, msg)
		}
	}
	for _, rg := range []string{"RG-" + "HUB", "rg" + "1", "RG-LAB.", "RG-LAB/x", "RG-LAB" + strings.Repeat("x", 90)} {
		if _, ok := checkResourceGroup(rg); ok {
			t.Errorf("checkResourceGroup(%q) accepted", rg)
		}
	}
}

func TestHyperscalerKeysAndScope(t *testing.T) {
	bucket := fmt.Sprintf(`{"BucketName": %q}`, "prod"+"-logs")
	if got := hyperscalerRules(t, "osiris/hyperscalers/aws/x_test.go", bucket); len(got) != 1 {
		t.Errorf("AWS bucket field: %v", got)
	}
	// Name fields are checked in AWS and Azure producer files only.
	if got := hyperscalerRules(t, "osiris/network/x/x_test.go", fmt.Sprintf(`{"ClusterName": %q}`, "mesh"+"-1")); len(got) != 0 {
		t.Errorf("cluster name outside hyperscaler files reported: %v", got)
	}
	// Resource group fields apply everywhere.
	if got := hyperscalerRules(t, "testdata/x.json", fmt.Sprintf(`{"resourceGroup": %q}`, "rg"+"1")); len(got) != 1 {
		t.Errorf("resource group field: %v", got)
	}
	// GUIDs outside Azure files and fields are not hyperscaler identities.
	other := guidOf("11111111", "1111", "1111", "1111", "111111111111")
	if got := hyperscalerRules(t, "osiris/network/cisco/catalyst-sd-wan-manager/x_test.go", fmt.Sprintf(`{"uuid": %q}`, other)); len(got) != 0 {
		t.Errorf("unrelated UUID reported: %v", got)
	}
	if got := hyperscalerRules(t, "osiris/hyperscalers/azure/x_test.go", fmt.Sprintf(`{"uuid": %q}`, other)); len(got) != 1 {
		t.Errorf("GUID in Azure file not reported: %v", got)
	}
	// Azure CLI identity arguments apply in every file.
	if got := hyperscalerRules(t, "osiris/hyperscalers/azure/help.go", "  tool -S "+other); len(got) != 1 {
		t.Errorf("CLI subscription argument: %v", got)
	}
	if got := hyperscalerRules(t, "README.md", "  tool --tenant "+tenantA); len(got) != 0 {
		t.Errorf("approved CLI tenant reported: %v", got)
	}
}

func TestUnknownHyperscalerIDsInProviderMetadata(t *testing.T) {
	unknownSub := guidOf("00000000", "0000", "0000", "0000", "000000000077")
	unknownAcct := "9999" + "99999999"
	golden := strings.Join([]string{
		`"provider": {`,
		fmt.Sprintf(`"name": "azure", "native_id": %q,`, arm(unknownSub, "resourceGroups", "RG-LAB")),
		fmt.Sprintf(`"subscription": %q, "tenant": %q,`, unknownSub, tenantA),
		fmt.Sprintf(`"account": %q`, unknownAcct),
		`}`,
	}, "\n")
	rep := scanMem(t, newMem("testdata/osiris/x/golden_audit.json", golden))
	var got []string
	for _, f := range rep.Violations {
		got = append(got, fmt.Sprintf("%d:%s", f.Line, f.Rule))
	}
	want := []string{"2:" + RuleAzureGUID, "3:" + RuleAzureGUID, "4:" + RuleAWSAccount}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("provider metadata findings %v, want %v", got, want)
	}
}

func TestRelationshipPreservationAfterReplacement(t *testing.T) {
	// A resource and the relation that references it must carry the same
	// synthetic identity. A partial replacement leaves the old identity
	// in one place and the scanner points at exactly that reference.
	oldSub := guidOf("00000000", "0000", "0000", "0000", "000000000011")
	vm := func(sub string) string {
		return arm(sub, "resourceGroups", "RG-LAB", "providers", "Microsoft.Compute", "virtualMachines", "LAB-VM-01")
	}
	complete := fmt.Sprintf("{%q: %q}\n{%q: %q}\n", "id", vm(subA), "target", vm(subA))
	if rep := scanMem(t, newMem("testdata/x.json", complete)); rep.Failed() {
		t.Errorf("consistent replacement reported: %v", rep.Violations)
	}
	partial := fmt.Sprintf("{%q: %q}\n{%q: %q}\n", "id", vm(subA), "target", vm(oldSub))
	rep := scanMem(t, newMem("testdata/x.json", partial))
	if len(rep.Violations) != 1 || rep.Violations[0].Line != 2 || rep.Violations[0].Rule != RuleAzureGUID {
		t.Errorf("partial replacement: %v", rep.Violations)
	}
}
