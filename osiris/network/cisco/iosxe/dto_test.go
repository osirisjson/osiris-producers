// dto_test.go - Unit tests for the typed YANG reply DTOs (dto.go):
// strict root/namespace checking, empty versus malformed payloads,
// trailing content and that decode errors never echo payload values.
// Synthetic payloads only; no device data.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package iosxe

import (
	"errors"
	"strings"
	"testing"
)

const testNativePayload = `<native xmlns="` + nsNative + `"><version>17.15</version><hostname>LAB-RTR01</hostname></native>`

func TestDecodeModel_Valid(t *testing.T) {
	cases := map[string]string{
		"default namespace": testNativePayload,
		"prefixed root":     `<x:native xmlns:x="` + nsNative + `"><x:version>17.15</x:version><x:hostname>LAB-RTR01</x:hostname></x:native>`,
		"surrounding space": "\n  " + testNativePayload + "\n  ",
		"trailing comment":  testNativePayload + `<!-- end -->`,
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			v, err := decodeModel[nativeSystem]([]byte(payload))
			if err != nil {
				t.Fatalf("decodeModel: %v", err)
			}
			if v.Hostname != "LAB-RTR01" || v.Version != "17.15" || v.rows() != 1 {
				t.Errorf("decoded %+v", v)
			}
		})
	}
}

func TestDecodeModel_Empty(t *testing.T) {
	for _, payload := range []string{"", "   ", "\n\t\n", "<!-- header only -->\n", "<?pi x?><!-- a --><!-- b -->"} {
		v, err := decodeModel[ietfInterfaces]([]byte(payload))
		if !errors.Is(err, errEmptyModel) {
			t.Fatalf("%q: err = %v, want errEmptyModel", payload, err)
		}
		if v.rows() != 0 {
			t.Errorf("%q: empty payload should decode to zero rows", payload)
		}
	}
}

func TestDecodeModel_Rejected(t *testing.T) {
	cases := map[string]string{
		"wrong root element":       `<interfaces xmlns="` + nsIETFInterface + `"/>`,
		"right name, wrong module": `<native xmlns="` + nsIETFInterface + `"><hostname>LAB-RTR01</hostname></native>`,
		"no namespace":             `<native><hostname>LAB-RTR01</hostname></native>`,
		"malformed":                `<native xmlns="` + nsNative + `"><hostname>LAB-RTR01</native>`,
		"truncated":                `<native xmlns="` + nsNative + `"><hostname>LAB-RTR01`,
		"second root element":      testNativePayload + testNativePayload,
		"trailing text":            testNativePayload + `junk`,
		"text only":                `junk`,
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			v, err := decodeModel[nativeSystem]([]byte(payload))
			if !errors.Is(err, errDecode) {
				t.Fatalf("err = %v, want errDecode", err)
			}
			if errors.Is(err, errEmptyModel) {
				t.Error("a rejected payload must not look empty")
			}
			if v.rows() != 0 {
				t.Errorf("a rejected payload must decode to the zero value, got %+v", v)
			}
		})
	}
}

func TestDecodeModel_ErrorsDoNotEchoValues(t *testing.T) {
	for _, payload := range []string{
		`<native xmlns="` + nsNative + `"><enable><secret>changeme</secret>`,
		`<native xmlns="` + nsNative + `"><enable><secret>changeme</secret></native>`,
		testNativePayload + `changeme`,
		`<native xmlns="` + nsNative + `"><hostname>changeme&bogus;</hostname></native>`,
	} {
		_, err := decodeModel[nativeSystem]([]byte(payload))
		if err == nil {
			t.Fatalf("%q: expected an error", payload)
		}
		if strings.Contains(err.Error(), "changeme") {
			t.Errorf("decode error leaks a payload value: %v", err)
		}
	}
}

func TestModelRows(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		rows    func([]byte) (int, error)
		want    int
	}{
		{"interfaces", `<interfaces xmlns="` + nsIETFInterface + `"><interface><name>a</name></interface><interface><name>b</name></interface></interfaces>`, rowsOf[ietfInterfaces], 2},
		{"native without values", `<native xmlns="` + nsNative + `"/>`, rowsOf[nativeSystem], 0},
		{"vrf definitions", `<native xmlns="` + nsNative + `"><vrf><definition><name>LAB-A</name></definition></vrf></native>`, rowsOf[nativeVRFList], 1},
		{"hardware", `<device-hardware-data xmlns="` + nsHardwareOper + `"><device-hardware><device-inventory><hw-type>hw-type-chassis</hw-type></device-inventory></device-hardware></device-hardware-data>`, rowsOf[hwInventory], 1},
		{"cdp with no neighbors", `<cdp-neighbor-details xmlns="` + nsCDPOper + `"/>`, rowsOf[cdpNeighborDetails], 0},
		{"bgp skeleton without neighbors", `<bgp-state-data xmlns="` + nsBGPOper + `"><bgp-route-vrfs><bgp-route-vrf><vrf>default</vrf></bgp-route-vrf></bgp-route-vrfs></bgp-state-data>`, rowsOf[bgpStateData], 0},
		{"ospf without instances", `<ospf-oper-data xmlns="` + nsOSPFOper + `"><ospf-state><op-mode>ospf-ships-in-the-night</op-mode></ospf-state></ospf-oper-data>`, rowsOf[ospfOperData], 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.rows([]byte(tc.payload))
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got != tc.want {
				t.Errorf("rows = %d, want %d", got, tc.want)
			}
		})
	}
}

func rowsOf[T model](b []byte) (int, error) {
	v, err := decodeModel[T](b)
	return v.rows(), err
}

// FuzzDecodeModel checks that arbitrary payloads never panic and that a
// failed decode always yields the zero value.
func FuzzDecodeModel(f *testing.F) {
	for _, seed := range []string{
		testNativePayload,
		`<interfaces xmlns="` + nsIETFInterface + `"><interface><name>a</name></interface></interfaces>`,
		``,
		`<native>`,
		testNativePayload + `x`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, in []byte) {
		if v, err := decodeModel[ietfInterfaces](in); err != nil && v.rows() != 0 {
			t.Fatalf("failed decode returned %d rows", v.rows())
		}
		if v, err := decodeModel[nativeSystem](in); err != nil && v.rows() != 0 {
			t.Fatalf("failed decode returned %d rows", v.rows())
		}
	})
}
