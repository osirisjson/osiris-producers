// listprops_test.go - Tests for filling resource properties that
// az list commands omit, for the OSIRIS JSON Microsoft Azure producer:
// the Resource Graph backfill (fillProps, rawStore.setProperties),
// the Key Vault show merge (mergeKVProperties, rawStore.replace) and
// the redaction of the credential-bearing properties those bodies add.

package azure

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFillProps(t *testing.T) {
	ai := tRG + "/microsoft.insights/components/LAB-AI01"
	items := []AutoscaleSetting{
		{ID: tRG + "/Microsoft.Insights/autoscalesettings/LAB-AS01"},
		{ID: tRG + "/Microsoft.Insights/autoscalesettings/LAB-AS02", Properties: &AutoscaleProps{TargetResourceURI: "kept"}},
		{ID: tRG + "/Microsoft.Insights/autoscalesettings/LAB-AS03"},
	}
	graph := map[string]json.RawMessage{
		// Case differs from the list ID, as Resource Graph
		// may return it.
		armKey(strings.ToLower(items[0].ID)): json.RawMessage(`{"enabled":true,"targetResourceUri":"` + tVMSS + `","profiles":"wrong-type"}`),
		armKey(items[1].ID):                  json.RawMessage(`{"enabled":true,"targetResourceUri":"replaced"}`),
		armKey(ai):                           json.RawMessage(`{}`),
	}
	n := fillProps(items, func(v *AutoscaleSetting) (string, **AutoscaleProps) { return v.ID, &v.Properties }, graph)
	if n != 1 {
		t.Fatalf("filled %d, want 1", n)
	}
	if p := items[0].Properties; p == nil || !p.Enabled || p.TargetResourceURI != tVMSS {
		t.Errorf("properties not filled despite a mismatched field: %+v", p)
	}
	if items[1].Properties.TargetResourceURI != "kept" {
		t.Error("existing properties must not be replaced")
	}
	if items[2].Properties != nil {
		t.Error("no graph entry must leave properties nil")
	}
}

func TestRawStoreSetPropertiesAndReplace(t *testing.T) {
	id := tRG + "/Microsoft.Web/connections/LAB-CONN01"
	s := newRawStore()
	s.capture([]byte(`[{"id":"` + id + `","name":"LAB-CONN01","properties":null}]`))
	s.setProperties(lowerRG(id), json.RawMessage(`{"displayName":"LAB"}`))
	body, _ := s.get(id)
	if !strings.Contains(string(body), `"displayName":"LAB"`) || !strings.Contains(string(body), `"name":"LAB-CONN01"`) {
		t.Errorf("null properties not filled, other members must be kept: %s", body)
	}
	// Existing properties are never overwritten.
	s.setProperties(id, json.RawMessage(`{"displayName":"OTHER"}`))
	if body, _ = s.get(id); strings.Contains(string(body), "OTHER") {
		t.Errorf("present properties must not be replaced: %s", body)
	}
	// A show object replaces the list object.
	s.replace([]byte(`{"id":"` + id + `","name":"LAB-CONN01","properties":{"displayName":"SHOW"}}`))
	if body, _ = s.get(id); !strings.Contains(string(body), "SHOW") {
		t.Errorf("replace must store the fuller object: %s", body)
	}
	var nilStore *rawStore
	nilStore.setProperties(id, nil)
	nilStore.replace([]byte(`{}`))
}

func TestMergeKVProperties(t *testing.T) {
	show := &KeyVaultProperties{VaultURI: "https://LAB-KV01.example.com/", NetworkACLs: &azKeyVaultNetworkACLs{}}
	prev := &KeyVaultProperties{MinimumTLSVersion: "1.2", PublicNetworkAccess: "Disabled", NetworkACLs: &azKeyVaultNetworkACLs{DefaultAction: "Deny"}}
	got := mergeKVProperties(show, prev)
	if got.VaultURI != show.VaultURI || got.MinimumTLSVersion != "1.2" || got.PublicNetworkAccess != "Disabled" || got.NetworkACLs.DefaultAction != "Deny" {
		t.Errorf("merged = %+v", got)
	}
	if show.NetworkACLs.DefaultAction != "" {
		t.Error("merge must not modify the show properties in place")
	}
	if got := mergeKVProperties(show, nil); got.VaultURI != show.VaultURI {
		t.Errorf("merge without previous properties = %+v", got)
	}
}

func TestRedactListedCredentialProperties(t *testing.T) {
	in := `{"properties":{
		"InstrumentationKey":"00000000-0000-0000-0000-0000000000cc",
		"ApplicationId":"LAB-AI01",
		"parameterValues":{"username":"admin","server":"LAB-SQL01.example.com","nested":{"password":"changeme"}},
		"displayName":"LAB-CONN01",
		"accessPolicies":[{"objectId":"00000000-0000-0000-0000-0000000000dd","permissions":{"secrets":["get","list"],"keys":["get"]}}]}}`
	out, _, ok := redactRawBody(json.RawMessage(in))
	if !ok {
		t.Fatal("redaction failed")
	}
	p := decodeBody(t, out)["properties"].(map[string]any)
	if p["InstrumentationKey"] != redactedValue {
		t.Errorf("InstrumentationKey = %v", p["InstrumentationKey"])
	}
	pv := p["parameterValues"].(map[string]any)
	if pv["username"] != redactedValue || pv["server"] != redactedValue || pv["nested"].(map[string]any)["password"] != redactedValue {
		t.Errorf("parameterValues must be redacted entirely: %v", pv)
	}
	perms := p["accessPolicies"].([]any)[0].(map[string]any)["permissions"].(map[string]any)
	if perms["secrets"].([]any)[0] != "get" {
		t.Errorf("Key Vault permission names must be kept: %v", perms)
	}
	if p["displayName"] != "LAB-CONN01" || p["ApplicationId"] != "LAB-AI01" {
		t.Errorf("unrelated fields must be kept: %v", p)
	}
}
