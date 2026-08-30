// transform_test.go - Unit tests for the shared helpers in transform.go.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package nxos

import (
	"testing"
)

func TestTrimmed(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"plain", "plain"},
		{"  padded  ", "padded"},
		{`"quoted"`, "quoted"},
		{`"  quoted padded  "`, "quoted padded"},
		{`  "Chassis"  `, "Chassis"},
		{`"48x10/25G/32G + 6x40/100G Ethernet/FC Module"`, "48x10/25G/32G + 6x40/100G Ethernet/FC Module"},
		{`"`, `"`}, // a lone quote is not a matched pair
		{`""`, ""}, // an empty matched pair
		{`no"middle"quote`, `no"middle"quote`},
	}
	for _, c := range cases {
		if got := trimmed(flexString(c.in)); got != c.want {
			t.Errorf("trimmed(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestResourceID_Deterministic(t *testing.T) {
	id1 := resourceID("network.switch", "LAB-SW01")
	id2 := resourceID("network.switch", "LAB-SW01")
	if id1 != id2 {
		t.Errorf("resourceID not deterministic: %s != %s", id1, id2)
	}

	id3 := resourceID("network.switch", "LAB-SW02")
	if id1 == id3 {
		t.Error("different inputs should produce different IDs")
	}
}
