// raw_test.go - Tests for the Cisco ACI/APIC --include-raw-body
// fallback: attribute redaction, owner resolution by longest DN prefix,
// unowned objects, the per-object and total size caps and an
// end-to-end secret canary through Collect.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package apic

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"go.osirisjson.org/producers/pkg/sdk"
)

// TestRedactRawAttrs proves: a sensitive attribute name or a
// secret-shaped value is replaced with the redaction placeholder; every
// other attribute is kept verbatim.
func TestRedactRawAttrs(t *testing.T) {
	in := map[string]any{"dn": "uni/tn-A", "name": "A", "pwd": "x", "authKey": "y", "descr": canaryBearer, "n": 3}
	out := redactRawAttrs(in)
	for k, want := range map[string]any{"dn": "uni/tn-A", "name": "A", "pwd": sdk.RedactedValue, "authKey": sdk.RedactedValue, "descr": sdk.RedactedValue, "n": 3} {
		if out[k] != want {
			t.Errorf("%s = %v, want %v", k, out[k], want)
		}
	}
	if in["descr"] != canaryBearer {
		t.Error("input map was modified")
	}
	got := redactRawAttrs(map[string]any{"password": "secret", "secretArn": "s", "Key": "k", "community": "c", "keyRing": "r", "keepAlive": "10"})
	for _, k := range []string{"password", "secretArn", "Key", "community", "keyRing"} {
		if got[k] != sdk.RedactedValue {
			t.Errorf("%s not redacted: %v", k, got[k])
		}
	}
	if got["keepAlive"] != "10" {
		t.Errorf("non-credential attribute redacted: %v", got["keepAlive"])
	}
}

// TestAttachRawBodies_Owners proves: an object attaches to the resource
// or group with the longest matching DN prefix (bracketed DN segments
// included), a resource wins over a group with the same DN, objects
// are listed per class in DN order and an object without an owner goes
// to the controller's raw_unowned.
func TestAttachRawBodies_Owners(t *testing.T) {
	node := sdk.Resource{ID: resourceID("topology/pod-1/node-1"), Type: "compute.server", Provider: sdk.Provider{Name: providerName, NativeID: "topology/pod-1/node-1"}, Properties: map[string]any{"role": "controller"}}
	port := sdk.Resource{ID: resourceID("topology/pod-1/node-1/sys/phys-[eth1/1]"), Type: portType, Provider: sdk.Provider{Name: providerName, NativeID: "topology/pod-1/node-1/sys/phys-[eth1/1]"}}
	epg := sdk.Resource{ID: resourceID("uni/tn-A/ap-p/epg-e"), Type: policyEPGType, Provider: sdk.Provider{Name: providerName, NativeID: "uni/tn-A/ap-p/epg-e"}}
	resources := []sdk.Resource{node, port, epg}
	groups := []sdk.Group{{ID: "g-epg", Type: "osiris.cisco.epg"}, {ID: "g-tenant", Type: "logical.tenant"}}
	groupDNs := map[string]string{"g-epg": "uni/tn-A/ap-p/epg-e", "g-tenant": "uni/tn-A"}

	raw := map[string][]map[string]any{
		"topSystem":   {{"dn": "topology/pod-1/node-1/sys", "name": "LAB-APIC1"}},
		"ethpmPhysIf": {{"dn": "topology/pod-1/node-1/sys/phys-[eth1/1]/phys", "operSt": "up"}},
		"fvRsPathAtt": {{"dn": "uni/tn-A/ap-p/epg-e/rspathAtt-[topology/pod-1/paths-1/pathep-[eth1/1]]"}},
		"vzBrCP":      {{"dn": "uni/tn-A/brc-z"}, {"dn": "uni/tn-A/brc-a"}},
		"fabricLink":  {{"dn": "topology/pod-1/lnkcnt-1/lnk-1-1-1-to-2-1-1"}},
	}
	attachRawBodies(raw, resources, groups, groupDNs)

	rawOf := func(ext map[string]any) map[string]any {
		c, _ := ext[extensionNamespace].(map[string]any)
		r, _ := c["raw"].(map[string]any)
		return r
	}
	if r := rawOf(resources[0].Extensions); r["topSystem"] == nil || r["ethpmPhysIf"] != nil {
		t.Errorf("node raw = %v", r)
	}
	if r := rawOf(resources[1].Extensions); r["ethpmPhysIf"] == nil {
		t.Errorf("port raw = %v", r)
	}
	if r := rawOf(resources[2].Extensions); r["fvRsPathAtt"] == nil {
		t.Errorf("EPG anchor raw = %v", r)
	}
	if groups[0].Extensions != nil {
		t.Errorf("EPG group took raw although a resource has the same DN: %v", groups[0].Extensions)
	}
	contracts := rawOf(groups[1].Extensions)["vzBrCP"].([]map[string]any)
	if len(contracts) != 2 || contracts[0]["dn"] != "uni/tn-A/brc-a" {
		t.Errorf("tenant vzBrCP raw not in DN order: %v", contracts)
	}
	unowned, _ := resources[0].Extensions[extensionNamespace].(map[string]any)["raw_unowned"].(map[string]any)
	if unowned["fabricLink"] == nil {
		t.Errorf("fabricLink not in raw_unowned: %v", unowned)
	}
}

// TestAttachRawBodies_Caps proves: an object over maxRawObjectBytes is
// withheld, objects past maxRawTotalBytes are withheld and every
// withheld object is counted in raw_omitted by class and reason.
func TestAttachRawBodies_Caps(t *testing.T) {
	node := sdk.Resource{ID: resourceID("topology/pod-1/node-1"), Type: "compute.server", Provider: sdk.Provider{Name: providerName, NativeID: "topology/pod-1/node-1"}, Properties: map[string]any{"role": "controller"}}
	resources := []sdk.Resource{node}
	big := strings.Repeat("x", maxRawObjectBytes)
	chunk := strings.Repeat("y", maxRawObjectBytes/2)
	var faults []map[string]any
	n := maxRawTotalBytes/(maxRawObjectBytes/2) + 10
	for i := range n {
		faults = append(faults, map[string]any{"dn": fmt.Sprintf("topology/pod-1/node-1/sys/fault-%06d", i), "descr": chunk})
	}
	raw := map[string][]map[string]any{
		"eqptSensor": {{"dn": "topology/pod-1/node-1/sys/sensor-1", "descr": big}},
		"faultInst":  faults,
	}
	attachRawBodies(raw, resources, nil, nil)

	cisco := resources[0].Extensions[extensionNamespace].(map[string]any)
	omitted := fmt.Sprint(cisco["raw_omitted"])
	if !strings.Contains(omitted, "class:eqptSensor count:1 reason:object-too-large") ||
		!strings.Contains(omitted, "class:faultInst") || !strings.Contains(omitted, "reason:total-cap") {
		t.Errorf("raw_omitted = %s", omitted)
	}
	kept := len(cisco["raw"].(map[string]any)["faultInst"].([]map[string]any))
	if kept == 0 || kept >= n {
		t.Errorf("faultInst kept %d of %d", kept, n)
	}
}

// TestCollect_IncludeRawBody proves end to end: with --purpose audit
// and --include-raw-body every collected object reaches the document
// under its owner, unmodeled attributes included, a secret-shaped
// attribute value or a sensitive attribute name is redacted (so
// fail-closed does not abort and the canary never appears) and without
// the flag no raw data is attached.
func TestCollect_IncludeRawBody(t *testing.T) {
	ts := fixtureServerWith(t, nil, map[string]any{"fvBD": []any{
		apicObj("fvBD", map[string]any{"dn": "uni/tn-tn_Example/BD-bd_App", "name": "bd_App", "unmodeledAttr": "visible", "ipLearning": canaryBearer, "authKey": "k"}),
	}})
	defer ts.Close()

	for _, raw := range []bool{false, true} {
		t.Run(fmt.Sprint("raw=", raw), func(t *testing.T) {
			data, err, logs := collectWithRaw(t, ts.URL, ts.Client(), "audit", raw)
			if err != nil {
				t.Fatalf("Collect: %v", err)
			}
			if bytes.Contains(data, []byte("CANARYfaultTOKEN0001")) || strings.Contains(logs, "CANARYfaultTOKEN0001") {
				t.Fatal("canary leaked")
			}
			has := bytes.Contains(data, []byte(`"unmodeledAttr": "visible"`))
			if has != raw {
				t.Errorf("unmodeled attribute present=%v, want %v", has, raw)
			}
			if raw && !bytes.Contains(data, []byte(`"authKey": "[REDACTED]"`)) {
				t.Error("sensitive attribute name not redacted")
			}
		})
	}
}
