// rawbody_test.go - Tests for lossless ARM body capture, redaction and
// attachment (rawbody.go) in the OSIRIS JSON Microsoft Azure producer.

package azure

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"go.osirisjson.org/producers/pkg/sdk"
)

const (
	rbVNet   = "/subscriptions/00000000-0000-0000-0000-000000000001/resourceGroups/RG-LAB/providers/Microsoft.Network/virtualNetworks/LAB-VNET01"
	rbSubnet = rbVNet + "/subnets/LAB-SNET01"
	rbNSG    = "/subscriptions/00000000-0000-0000-0000-000000000001/resourceGroups/RG-LAB/providers/Microsoft.Network/networkSecurityGroups/LAB-NSG01"
)

func decodeBody(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("invalid JSON body: %v", err)
	}
	return m
}

func TestRawStoreCapture(t *testing.T) {
	// A VNet list item embeds its subnet; the subnet embeds a
	// bare NSG reference.
	vnetList := `[{"id":"` + rbVNet + `","name":"LAB-VNET01","properties":{"unmodelled":"x","subnets":[` +
		`{"id":"` + rbSubnet + `","name":"LAB-SNET01","properties":{"networkSecurityGroup":{"id":"` + rbNSG + `"}}}]}}]`

	s := newRawStore()
	s.capture([]byte(vnetList))

	vnet, ok := s.get(rbVNet)
	if !ok || !strings.Contains(string(vnet), `"unmodelled":"x"`) {
		t.Fatalf("VNet list item not captured verbatim: %s", vnet)
	}
	sub, ok := s.get(strings.ToUpper(rbSubnet))
	if !ok || decodeBody(t, sub)["name"] != "LAB-SNET01" {
		t.Fatalf("nested subnet not captured (case-insensitive lookup): %s", sub)
	}
	if _, ok := s.get(rbNSG); ok {
		t.Error("a bare {\"id\": ...} reference must not be captured as a resource body")
	}

	// A later top-level item replaces the nested copy; a nested copy
	// never replaces a top-level item.
	s.capture([]byte(`[{"id":"` + rbSubnet + `","name":"LAB-SNET01","properties":{"fromList":true}}]`))
	s.capture([]byte(vnetList))
	sub, _ = s.get(rbSubnet)
	if !strings.Contains(string(sub), `"fromList":true`) {
		t.Errorf("top-level subnet body did not win over nested copy: %s", sub)
	}

	// Single-object output (e.g. resource show).
	s.capture([]byte(`{"id":"` + rbNSG + `","name":"LAB-NSG01","properties":{}}`))
	if _, ok := s.get(rbNSG); !ok {
		t.Error("single-object output not captured")
	}
}

func TestRawStoreNilAndConcurrent(t *testing.T) {
	var nilStore *rawStore
	nilStore.capture([]byte(`[{"id":"/subscriptions/x/a","name":"a"}]`))
	if _, ok := nilStore.get("/subscriptions/x/a"); ok {
		t.Fatal("nil store must capture nothing")
	}

	s := newRawStore()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := rbVNet + "-" + string(rune('a'+i))
			s.capture([]byte(`[{"id":"` + id + `","name":"n"}]`))
		}(i)
	}
	wg.Wait()
	if len(s.m) != 16 {
		t.Errorf("expected 16 captured bodies, got %d", len(s.m))
	}
}

func TestRedactRawBody(t *testing.T) {
	in := `{
		"name": "LAB-APP01",
		"properties": {
			"adminPassword": "changeme",
			"enableRbacAuthorization": true,
			"addonProfiles": {"azureKeyvaultSecretsProvider": {"config": {"enableSecretRotation": "false"}}},
			"secretFlagPassword": "true-secret",
			"siteConfig": {"connectionStrings": [{"name": "db", "connectionString": "Server=LAB-SQL01;Password=secret", "type": "SQLAzure"}]},
			"accessKeys": {"primaryKey": "secret", "keyCount": 2},
			"endpoint": "https://admin:password@example.com/",
			"logicAppReceivers": [{"name": "LAB-LA01", "callbackUrl": "https://example.com/workflows/x/triggers/manual/paths/invoke?api-version=2016-10-01&sp=%2Fx&sv=1.0&sig=changeme"}],
			"azureFunctionReceivers": [{"name": "LAB-FN01", "httpTriggerUrl": "https://example.com/api/x?code=changeme"}],
			"secrets": [{"name": "LAB-SECRET01", "value": "changeme"}],
			"definition": {
				"parameters": {"apiPassphrase": {"type": "securestring", "defaultValue": "changeme"}, "region": {"type": "string", "defaultValue": "westeurope"}},
				"actions": {
					"Get_secret": {"type": "Http", "inputs": {"method": "GET", "uri": "https://LAB-KV01.example.com/secrets/x", "authentication": {"type": "ManagedServiceIdentity", "audience": "https://example.com"}}},
					"HTTP_Get_Token": {"type": "Http", "inputs": {"method": "POST", "body": "grant_type=client_credentials&client_secret=changeme", "headers": {"Content-Type": "application/x-www-form-urlencoded", "Authorization": "Basic changeme"}}},
					"Call_API": {"type": "Http", "inputs": {"method": "POST", "body": "client_id=x&client_secret=changeme"}, "runAfter": {"Get_secret": ["Succeeded"], "HTTP_Get_Token": ["Succeeded", "Failed"]}}
				}
			},
			"largeCounter": 12345678901234567890,
			"description": "plain text"
		}
	}`
	out, n, ok := redactRawBody(json.RawMessage(in))
	if !ok {
		t.Fatal("redaction failed on valid JSON")
	}
	if strings.Contains(string(out), "changeme") || strings.Contains(string(out), "password@") {
		t.Fatalf("a secret survived redaction:\n%s", out)
	}
	p := decodeBody(t, out)["properties"].(map[string]any)
	get := func(m any, path ...any) any {
		for _, k := range path {
			switch key := k.(type) {
			case string:
				m = m.(map[string]any)[key]
			case int:
				m = m.([]any)[key]
			}
		}
		return m
	}

	// Kept: configuration and structure, including below sensitive keys.
	keep := []struct {
		path []any
		want any
	}{
		{[]any{"enableRbacAuthorization"}, true},
		{[]any{"addonProfiles", "azureKeyvaultSecretsProvider", "config", "enableSecretRotation"}, "false"},
		{[]any{"siteConfig", "connectionStrings", 0, "name"}, "db"},
		{[]any{"siteConfig", "connectionStrings", 0, "type"}, "SQLAzure"},
		{[]any{"accessKeys", "keyCount"}, float64(2)},
		{[]any{"logicAppReceivers", 0, "name"}, "LAB-LA01"},
		{[]any{"secrets", 0, "name"}, "LAB-SECRET01"},
		{[]any{"definition", "parameters", "region", "defaultValue"}, "westeurope"},
		{[]any{"definition", "actions", "Get_secret", "type"}, "Http"},
		{[]any{"definition", "actions", "Get_secret", "inputs", "method"}, "GET"},
		{[]any{"definition", "actions", "Get_secret", "inputs", "uri"}, "https://LAB-KV01.example.com/secrets/x"},
		{[]any{"definition", "actions", "Get_secret", "inputs", "authentication", "type"}, "ManagedServiceIdentity"},
		{[]any{"definition", "actions", "Get_secret", "inputs", "authentication", "audience"}, "https://example.com"},
		{[]any{"definition", "actions", "HTTP_Get_Token", "inputs", "method"}, "POST"},
		{[]any{"definition", "actions", "HTTP_Get_Token", "inputs", "headers", "Content-Type"}, "application/x-www-form-urlencoded"},
		{[]any{"description"}, "plain text"},
		{[]any{"definition", "actions", "Call_API", "runAfter", "Get_secret", 0}, "Succeeded"},
		{[]any{"definition", "actions", "Call_API", "runAfter", "HTTP_Get_Token", 1}, "Failed"},
	}
	for _, k := range keep {
		if got := get(p, k.path...); got != k.want {
			t.Errorf("%v = %v, want %v (must not be redacted)", k.path, got, k.want)
		}
	}

	// Redacted.
	redact := [][]any{
		{"adminPassword"},
		{"secretFlagPassword"},
		{"siteConfig", "connectionStrings", 0, "connectionString"},
		{"accessKeys", "primaryKey"},
		{"endpoint"},
		{"logicAppReceivers", 0, "callbackUrl"},
		{"azureFunctionReceivers", 0, "httpTriggerUrl"},
		{"secrets", 0, "value"},
		{"definition", "parameters", "apiPassphrase", "defaultValue"},
		{"definition", "actions", "HTTP_Get_Token", "inputs", "body"},
		{"definition", "actions", "HTTP_Get_Token", "inputs", "headers", "Authorization"},
		{"definition", "actions", "Call_API", "inputs", "body"},
	}
	for _, path := range redact {
		if got := get(p, path...); got != redactedValue {
			t.Errorf("%v = %v, want %s", path, got, redactedValue)
		}
	}
	if n != len(redact) {
		t.Errorf("redacted %d values, want %d", n, len(redact))
	}
	if !strings.Contains(string(out), "12345678901234567890") {
		t.Error("large integer lost precision")
	}
	if _, _, ok := redactRawBody(json.RawMessage(`{not json`)); ok {
		t.Error("invalid JSON must report !ok")
	}
}

func TestApplyRawBodies(t *testing.T) {
	mk := func(arm string) sdk.Resource {
		r := sdk.Resource{ID: "azure::" + arm, Provider: sdk.Provider{NativeID: arm, Type: "Microsoft.Network/virtualNetworks"}}
		attachArmBody(&r, map[string]string{"decoded": "only"})
		return r
	}
	captured := mk(rbVNet)
	missing := mk(rbVNet + "-missing")
	// A URL in one field and an e-mail address in the next one
	// (adjacent in sorted key order) are each clean; in compact JSON
	// they match the credential URI pattern together. The spaced
	// encoding must keep this body, also once stored as a JSON string
	// in the document.
	crossField := mk(rbSubnet)
	// An unusable capture exercises the rejection path.
	broken := mk(rbNSG)

	s := newRawStore()
	s.capture([]byte(`[{"id":"` + rbVNet + `","name":"LAB-VNET01","properties":{"adminPassword":"changeme","unmodelled":1}}]`))
	s.capture([]byte(`[{"id":"` + rbSubnet + `","portalUrl":"https://LAB-APIM01.example.com","publisherEmail":"admin@example.com"}]`))
	s.m[armKey(rbNSG)] = rawEntry{body: json.RawMessage(`{"id":"` + rbNSG + `",`)}

	res := []sdk.Resource{captured, missing, crossField, broken}
	st := applyRawBodies(res, s)

	ext := func(i int) map[string]any { return res[i].Extensions["osiris.azure.arm"].(map[string]any) }
	if ext(0)["source"] != "arm" || !strings.Contains(ext(0)["body"].(string), `"unmodelled": 1`) {
		t.Errorf("captured body not attached: %v", ext(0))
	}
	if strings.Contains(ext(0)["body"].(string), "changeme") {
		t.Error("attached body contains an unredacted secret")
	}
	if ext(1)["source"] != "struct" || !strings.Contains(ext(1)["body"].(string), "decoded") {
		t.Errorf("missing capture must keep the struct body: %v", ext(1))
	}
	stored, _ := json.Marshal(ext(2)["body"])
	if ext(2)["source"] != "arm" || sdk.ScanValue(ext(2)["body"].(string)) != "" || sdk.ScanValue(string(stored)) != "" {
		t.Errorf("URL and e-mail in separate fields must be kept and pass the scanner: %v", ext(2))
	}
	if ext(3)["source"] != "struct" || ext(3)["raw_rejected"] != "invalid_json" {
		t.Errorf("unusable capture must fall back with a reason: %v", ext(3))
	}
	if st.ARM != 2 || st.Struct != 1 || st.Rejected != 1 || st.Redacted != 1 {
		t.Errorf("stats = %+v", st)
	}
	if st.StructByType["Microsoft.Network/virtualNetworks"] != 1 {
		t.Errorf("StructByType = %v", st.StructByType)
	}
}

func TestRedactSecureObjectParameter(t *testing.T) {
	in := `{"parameters": {"settings": {"type": "SecureObject", "defaultValue": {"endpoint": "LAB-EP01", "nested": ["a", {"k": "b"}]}}}}`
	out, n, ok := redactRawBody(json.RawMessage(in))
	if !ok || n != 3 {
		t.Fatalf("expected 3 redacted values in a secureobject default, got %d (ok=%v): %s", n, ok, out)
	}
	for _, v := range []string{"LAB-EP01", `"a"`, `"b"`} {
		if strings.Contains(string(out), v) {
			t.Errorf("secureobject value %s survived: %s", v, out)
		}
	}
}

// A multi-line string whose lines are clean on their own but whose
// JSON-encoded form (line breaks as `\n`) matches a secret pattern must
// be redacted, so the attached body passes the document scanner.
func TestRedactMultilineEncodedMatch(t *testing.T) {
	policy := "backend=https://LAB-API01.example.com\nnotify:ops@example.com"
	if sdk.ScanValue(policy) != "" {
		t.Fatal("test precondition: decoded string must be clean")
	}
	in, _ := json.Marshal(map[string]any{"id": rbVNet, "properties": map[string]any{"policy": policy, "kept": "x"}})
	out, n, ok := redactRawBody(in)
	if !ok || n != 1 {
		t.Fatalf("expected the policy string redacted, got n=%d: %s", n, out)
	}
	if sdk.ScanValue(string(out)) != "" {
		t.Errorf("redacted body still matches the document scanner: %s", out)
	}
	if decodeBody(t, out)["properties"].(map[string]any)["kept"] != "x" {
		t.Error("unrelated field lost")
	}
}

func TestRedactDocumentValues(t *testing.T) {
	const conn = "InstrumentationKey=00000000-0000-0000-0000-0000000000ee;IngestionEndpoint=https://LAB-AI01.example.com/"
	shared := map[string]string{"hidden-link": conn, "env": "LAB"}
	doc := &sdk.Document{}
	doc.Topology.Resources = []sdk.Resource{
		{
			ID:         "azure::a",
			Tags:       shared,
			Properties: map[string]any{"env": []any{map[string]any{"name": "APPLICATIONINSIGHTS_CONNECTION_STRING", "value": conn}}, "callback": "https://example.com/x?sig=changeme"},
			Extensions: map[string]any{"osiris.azure.arm": map[string]any{"body": `{"kept":"` + conn + `"}`, "source": "arm"}},
		},
		{ID: "azure::b", Tags: shared},
	}
	doc.Topology.Connections = []sdk.Connection{{ID: "c", Tags: map[string]string{"hidden-link": conn}}}
	doc.Topology.Groups = []sdk.Group{{ID: "g", Tags: map[string]string{"owner": "LAB-TEAM"}}}

	if n := redactDocumentValues(doc); n != 5 {
		t.Errorf("redacted %d values, want 5 (2 resource tags, 1 connection tag, 2 property strings)", n)
	}
	r := doc.Topology.Resources[0]
	if r.Tags["hidden-link"] != redactedValue || r.Tags["env"] != "LAB" {
		t.Errorf("resource tags = %v", r.Tags)
	}
	if v := r.Properties["env"].([]any)[0].(map[string]any); v["value"] != redactedValue || v["name"] != "APPLICATIONINSIGHTS_CONNECTION_STRING" {
		t.Errorf("property env = %v", v)
	}
	if r.Properties["callback"] != redactedValue {
		t.Errorf("SAS URL in properties not redacted: %v", r.Properties["callback"])
	}
	if doc.Topology.Connections[0].Tags["hidden-link"] != redactedValue {
		t.Error("connection tag not redacted")
	}
	if doc.Topology.Groups[0].Tags["owner"] != "LAB-TEAM" {
		t.Error("clean group tag must be kept")
	}
	// The raw body string is left to the per-value redaction at attach time.
	if body := r.Extensions["osiris.azure.arm"].(map[string]any)["body"].(string); !strings.Contains(body, "InstrumentationKey") {
		t.Error("redactDocumentValues must not rewrite the raw body string")
	}
	// The shared input map itself is not modified.
	if shared["hidden-link"] != conn {
		t.Error("shared tag map must not be modified in place")
	}
	// The pattern also redacts the raw body at attach time.
	out, _, _ := redactRawBody(json.RawMessage(`{"tags":{"hidden-link":"` + conn + `"}}`))
	if strings.Contains(string(out), "InstrumentationKey") {
		t.Errorf("raw body tag not redacted: %s", out)
	}
}

// An ExpressRoute circuit service key is a provisioning credential: it
// is redacted in the raw body and never decoded into the struct (so a
// struct fallback body cannot carry it either).
func TestExpressRouteServiceKeyNeverEmitted(t *testing.T) {
	const key = "00000000-0000-0000-0000-0000000000ff"
	body := `{"id":"` + tRG + `/Microsoft.Network/expressRouteCircuits/LAB-ER01","name":"LAB-ER01","serviceKey":"` + key + `","serviceProviderProperties":{"bandwidthInMbps":200}}`
	out, _, ok := redactRawBody(json.RawMessage(body))
	if !ok || strings.Contains(string(out), key) {
		t.Errorf("service key survived raw body redaction: %s", out)
	}
	var c ExpressRouteCircuit
	if err := json.Unmarshal([]byte(body), &c); err != nil {
		t.Fatal(err)
	}
	res := TransformExpressRouteCircuits([]ExpressRouteCircuit{c}, SubscriptionInfo{})
	b, _ := json.Marshal(res)
	if strings.Contains(string(b), key) {
		t.Errorf("service key emitted by the transform: %s", b)
	}
}

// TestStructBodiesAreRedacted: a resource without a usable capture
// keeps its struct-derived body only after the same redaction and
// checks as a captured body (a credential URI is redacted in place); a
// struct body that cannot be verified is dropped, recording only
// the rule name.
func TestStructBodiesAreRedacted(t *testing.T) {
	mk := func(id, body string) sdk.Resource {
		return sdk.Resource{ID: "azure::" + id, Provider: sdk.Provider{NativeID: id, Type: "Microsoft.Lab/things"},
			Extensions: map[string]any{"osiris.azure.arm": map[string]any{"body": body}}}
	}
	credURI := mk(rbVNet, `{"repoUrl":"https://admin:changeme@LAB-GIT01.example.com/repo.git","branch":"main"}`)
	broken := mk(rbVNet+"-broken", `{"repoUrl":`)
	secretKey := mk(rbSubnet, `{"name":"LAB-APP01","adminPassword":"changeme","sku":"standard"}`)
	rejected := mk(rbNSG, `{"name":"LAB-NSG01","sharedKey":"changeme"}`)

	s := newRawStore()
	// A capture that is not valid JSON forces the struct fallback.
	s.m[armKey(rbNSG)] = rawEntry{body: json.RawMessage(`{"id":`)}

	res := []sdk.Resource{credURI, secretKey, rejected, broken}
	st := applyRawBodies(res, s)
	ext := func(i int) map[string]any { return res[i].Extensions["osiris.azure.arm"].(map[string]any) }

	if b, _ := ext(0)["body"].(string); strings.Contains(b, "changeme") || !strings.Contains(b, `"branch": "main"`) || ext(0)["source"] != "struct" {
		t.Errorf("credential URI in a struct body must be redacted in place: %v", ext(0))
	}
	if _, has := ext(3)["body"]; has || ext(3)["struct_rejected"] != "invalid_json" {
		t.Errorf("an unverifiable struct body must be dropped: %v", ext(3))
	}
	if b, _ := ext(1)["body"].(string); strings.Contains(b, "changeme") || !strings.Contains(b, `"sku": "standard"`) {
		t.Errorf("struct body must be redacted, configuration kept: %s", b)
	}
	if b, _ := ext(2)["body"].(string); strings.Contains(b, "changeme") || ext(2)["raw_rejected"] != "invalid_json" {
		t.Errorf("rejected capture must fall back to a redacted struct body: %v", ext(2))
	}
	for i := range res {
		b, _ := ext(i)["body"].(string)
		if sdk.ScanValue(b) != "" || containsAzureSecret(b) {
			t.Errorf("resource %d emits a body matching a secret pattern", i)
		}
	}
	if st.StructRejected != 1 || st.Struct != 3 || st.Rejected != 1 {
		t.Errorf("stats = %+v", st)
	}
}
