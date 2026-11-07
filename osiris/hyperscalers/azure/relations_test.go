// relations_test.go - Tests for ARM ID normalization (armindex.go) and
// the final connection resolution pass (relations.go) of the
// OSIRIS JSON Microsoft Azure producer.

package azure

import (
	"testing"

	"go.osirisjson.org/producers/pkg/sdk"
)

func TestArmLookup(t *testing.T) {
	const vm = "/subscriptions/00000000-0000-0000-0000-000000000001/resourceGroups/RG-LAB/providers/Microsoft.Compute/virtualMachines/LAB-VM01"
	m := map[string]string{armKey(vm): "azure::" + vm}

	tests := []struct {
		name string
		ref  string
		ok   bool
	}{
		{"exact", vm, true},
		{"lower-cased resource group segment", "/subscriptions/00000000-0000-0000-0000-000000000001/resourcegroups/rg-lab/providers/Microsoft.Compute/virtualMachines/LAB-VM01", true},
		{"upper-cased whole id", "/SUBSCRIPTIONS/00000000-0000-0000-0000-000000000001/RESOURCEGROUPS/RG-LAB/PROVIDERS/MICROSOFT.COMPUTE/VIRTUALMACHINES/LAB-VM01", true},
		{"trailing slash", vm + "/", true},
		{"empty", "", false},
		{"different resource", "/subscriptions/00000000-0000-0000-0000-000000000001/resourceGroups/RG-LAB/providers/Microsoft.Compute/virtualMachines/LAB-VM02", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := armLookup(m, tt.ref)
			if ok != tt.ok {
				t.Fatalf("armLookup ok = %v, want %v", ok, tt.ok)
			}
			if ok && got != "azure::"+vm {
				t.Errorf("armLookup = %q, want original-case resource ID", got)
			}
		})
	}
}

// TestArmLookupRawKeys covers maps built with raw (non-canonical) keys,
// which must still resolve on an exact match.
func TestArmLookupRawKeys(t *testing.T) {
	m := map[string]string{"/subscriptions/S/resourceGroups/RG/providers/X/y/Z": "id"}
	if _, ok := armLookup(m, "/subscriptions/S/resourceGroups/RG/providers/X/y/Z"); !ok {
		t.Fatal("exact raw key not resolved")
	}
}

func TestFinalizeConnections(t *testing.T) {
	mk := func(id, typ, src, dst string) sdk.Connection {
		return sdk.Connection{ID: id, Type: typ, Source: src, Target: dst}
	}
	resources := map[string]struct{}{"a": {}, "b": {}, "c": {}}
	in := []sdk.Connection{
		mk("c1", "contains", "a", "b"),
		mk("c1", "contains", "a", "b"), // same relationship learned twice
		mk("c2", "network", "b", "missing"),
		mk("c3", "network", "a", "a"),
		mk("c4", "dependency.storage", "a", "c"),
	}
	out, st := finalizeConnections(in, resources)

	if len(out) != 2 || out[0].ID != "c1" || out[1].ID != "c4" {
		t.Fatalf("unexpected output: %+v", out)
	}
	want := relationStats{Candidates: 5, Emitted: 2, Duplicates: 1, Dangling: 1, SelfRef: 1}
	if st.Candidates != want.Candidates || st.Emitted != want.Emitted ||
		st.Duplicates != want.Duplicates || st.Dangling != want.Dangling || st.SelfRef != want.SelfRef {
		t.Errorf("stats = %+v, want %+v", st, want)
	}
	if st.ByType["contains"] != 1 || st.ByType["dependency.storage"] != 1 || st.ByType["network"] != 0 {
		t.Errorf("ByType = %v", st.ByType)
	}
}
