// raw_test.go - Tests for --include-raw-body redaction, raw row
// accounting and the producer-side log-and-redact pass of the Cisco
// Catalyst SD-WAN Manager OSIRIS JSON Producer.
//
// Secret-shaped inputs use the approved synthetic placeholders only.
//
// OSIRIS JSON Producer for Cisco Catalyst SD-WAN Manager introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification
package catalystsdwanmanager

import (
	"encoding/json"
	"strings"
	"testing"

	"go.osirisjson.org/producers/pkg/sdk"
	"go.osirisjson.org/producers/pkg/testharness"
)

const (
	canaryBearer = "Bearer abcdefghijklmnop"
	canaryURI    = "https://admin:changeme@sdwan-manager.example.com/"
)

// noSecrets fails when any canary or the plain placeholder password
// survives in data.
func noSecrets(t *testing.T, data []byte) {
	t.Helper()
	for _, c := range []string{canaryBearer, canaryURI, "changeme"} {
		if strings.Contains(string(data), c) {
			t.Fatalf("secret %q survived redaction in %s", c, data)
		}
	}
}

func TestRedactValue(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"sensitive key", `{"password":"changeme","ifname":"ge0/0"}`, `{"ifname":"ge0/0","password":"[REDACTED]"}`},
		{"hyphenated sensitive key", `{"auth-key":"changeme"}`, `{"auth-key":"[REDACTED]"}`},
		{"sd-wan credential key", `{"pre-shared-key":"changeme","community":"changeme"}`, `{"community":"[REDACTED]","pre-shared-key":"[REDACTED]"}`},
		{"nested object", `{"a":{"b":{"secret":"changeme"}}}`, `{"a":{"b":{"secret":"[REDACTED]"}}}`},
		{"object inside array", `{"rows":[{"token":"changeme"},{"x":1}]}`, `{"rows":[{"token":"[REDACTED]"},{"x":1}]}`},
		{"value pattern in string", `{"desc":"` + canaryBearer + `"}`, `{"desc":"[REDACTED]"}`},
		{"value pattern in array", `["` + canaryURI + `","ok"]`, `["[REDACTED]","ok"]`},
		{"sensitive key with object value", `{"credential":{"user":"admin","pass":"changeme"}}`, `{"credential":"[REDACTED]"}`},
		{"empty and null under sensitive key kept", `{"password":"","secret":null}`, `{"password":"","secret":null}`},
		{"numbers kept exact", `{"uptime":1634626320000,"mtu":1500}`, `{"mtu":1500,"uptime":1634626320000}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, err := decodeRaw(json.RawMessage(tt.in))
			if err != nil {
				t.Fatalf("decodeRaw: %v", err)
			}
			var n int
			got, err := json.Marshal(redactValue(v, &n))
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("redactValue = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestRawCollector_AttachesRedactedRowsAndCountsOmissions(t *testing.T) {
	rc := newRawCollector(true)
	r := sdk.Resource{ID: "cisco.catalystsdwanmanager::TEST-1"}
	other := sdk.Resource{ID: "cisco.catalystsdwanmanager::TEST-2"}

	rc.attachOne(&r, "raw", json.RawMessage(`{"host-name":"LAB-RTR01","password":"changeme"}`))
	rc.attachList(&r, "interfaces_raw", []json.RawMessage{
		json.RawMessage(`{"ifname":"ge0/0","description":"` + canaryBearer + `"}`),
		json.RawMessage(`{not json`),
		json.RawMessage(`{"ifname":"` + strings.Repeat("x", maxRawRowBytes) + `"}`),
	})
	rc.attachList(&other, "omp_peers_raw", nil)
	resources := []sdk.Resource{r, other}
	rc.finish(resources)

	data, err := json.Marshal(resources)
	if err != nil {
		t.Fatal(err)
	}
	noSecrets(t, data)

	ext := resources[0].Extensions[extensionKey].(map[string]any)
	rows, _ := ext["interfaces_raw"].([]any)
	if len(rows) != 1 {
		t.Fatalf("interfaces_raw rows = %d, want 1 (one unparseable and one oversized withheld)", len(rows))
	}
	omitted, _ := ext["raw_omitted"].([]any)
	got, _ := json.Marshal(omitted)
	want := `[{"count":1,"endpoint":"interfaces_raw","reason":"row-too-large"},{"count":1,"endpoint":"interfaces_raw","reason":"unparseable"}]`
	if string(got) != want {
		t.Errorf("raw_omitted = %s, want %s", got, want)
	}
	if resources[1].Extensions != nil {
		t.Errorf("resource without raw rows got extensions %v", resources[1].Extensions)
	}
}

func TestRawCollector_TotalLimit(t *testing.T) {
	rc := newRawCollector(true)
	rc.total = maxRawTotalBytes - 10
	r := sdk.Resource{ID: "cisco.catalystsdwanmanager::TEST-1"}
	rc.attachOne(&r, "raw", json.RawMessage(`{"host-name":"LAB-RTR01-LONGER-THAN-TEN"}`))
	rs := []sdk.Resource{r}
	rc.finish(rs)
	ext := rs[0].Extensions[extensionKey].(map[string]any)
	if _, ok := ext["raw"]; ok {
		t.Error("row over the total limit was attached")
	}
	got, _ := json.Marshal(ext["raw_omitted"])
	if !strings.Contains(string(got), rawOmitTotalLimit) {
		t.Errorf("raw_omitted = %s, want a %s entry", got, rawOmitTotalLimit)
	}
}

func TestRawCollector_NilIsNoop(t *testing.T) {
	var rc *rawCollector = newRawCollector(false)
	r := sdk.Resource{ID: "cisco.catalystsdwanmanager::TEST-1"}
	rc.attachOne(&r, "raw", json.RawMessage(`{"password":"changeme"}`))
	rc.attachList(&r, "interfaces_raw", []json.RawMessage{json.RawMessage(`{}`)})
	rc.finish([]sdk.Resource{r})
	if r.Extensions != nil {
		t.Errorf("disabled collector attached %v", r.Extensions)
	}
}

// TestRedactElements_EveryModeKeepsTheNoSecretInvariant builds the same
// secret-bearing document under each --safe-failure-mode: fail-closed
// must abort, log-and-redact and off must emit no detected value and
// log-and-redact must claim redaction only when it really redacted.
func TestRedactElements_EveryModeKeepsTheNoSecretInvariant(t *testing.T) {
	build := func(mode string) ([]byte, error) {
		resources := []sdk.Resource{{
			ID:          "cisco.catalystsdwanmanager::TEST-1",
			Type:        "network.router",
			Name:        "LAB-RTR01",
			Description: canaryURI,
			Provider:    sdk.Provider{Name: providerName, NativeID: "TEST-1"},
			Properties:  map[string]any{"notes": []any{canaryBearer}},
			Extensions:  map[string]any{extensionKey: map[string]any{"interfaces_raw": []any{map[string]any{"auth-key": "changeme"}}}},
		}}
		groups := []sdk.Group{{ID: "group-test", Type: "physical.room", Name: "BRANCH-00", Description: canaryBearer, Members: []string{resources[0].ID}}}
		if mode != sdk.FailClosed {
			redactElements(resources, nil, groups)
		}
		ctx := testharness.NewTestContext(t)
		ctx.Config.SafeFailureMode = mode
		b := sdk.NewDocumentBuilder(ctx).WithGenerator(generatorName, generatorVersion, generatorURL)
		for _, r := range resources {
			b.AddResource(r)
		}
		for _, g := range groups {
			b.AddGroup(g)
		}
		doc, err := b.Build()
		if err == nil {
			err = verifyRedacted(doc, mode)
		}
		if err != nil {
			return nil, err
		}
		return sdk.MarshalDocument(doc)
	}

	if _, err := build(sdk.FailClosed); err == nil {
		t.Error("fail-closed: expected Build to abort on the secret-bearing document")
	}
	for _, mode := range []string{sdk.LogAndRedact, sdk.Off} {
		data, err := build(mode)
		if err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		noSecrets(t, data)
		if f := sdk.ScanDocument(mustDoc(t, data)); len(f) != 0 {
			t.Errorf("%s: document still has findings %v", mode, f)
		}
	}
}

// TestVerifyRedacted_RejectsLeftoverFinding proves log-and-redact never
// writes a document with a detected value under a redacted claim, even
// in a field the producer does not redact.
func TestVerifyRedacted_RejectsLeftoverFinding(t *testing.T) {
	ctx := testharness.NewTestContext(t)
	ctx.Config.SafeFailureMode = sdk.LogAndRedact
	b := sdk.NewDocumentBuilder(ctx).WithGenerator(generatorName, generatorVersion, generatorURL)
	b.AddResource(sdk.Resource{
		ID:       "cisco.catalystsdwanmanager::TEST-1",
		Type:     "network.router",
		Provider: sdk.Provider{Name: providerName, NativeID: canaryBearer},
	})
	doc, err := b.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	err = verifyRedacted(doc, sdk.LogAndRedact)
	if err == nil {
		t.Fatal("expected verifyRedacted to reject a leftover finding")
	}
	if strings.Contains(err.Error(), canaryBearer) {
		t.Errorf("error echoes the secret value: %v", err)
	}
	if verifyRedacted(doc, sdk.Off) != nil || verifyRedacted(doc, sdk.FailClosed) != nil {
		t.Error("verifyRedacted must only gate log-and-redact")
	}
}

func mustDoc(t *testing.T, data []byte) *sdk.Document {
	t.Helper()
	var doc sdk.Document
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	return &doc
}
