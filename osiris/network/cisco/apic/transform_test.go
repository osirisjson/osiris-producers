// transform_test.go - Tests for the shared APIC->OSIRIS helpers
// (DN parsing, resource identity).
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package apic

import "testing"

func TestDnPrefix(t *testing.T) {
	tests := []struct {
		dn   string
		want string
	}{
		{"topology/pod-1/node-1/sys", "topology/pod-1/node-1"},
		{"topology/pod-2/node-201/sys/fwstatuscont/running", "topology/pod-2/node-201"},
		{"topology/pod-1/node-101", "topology/pod-1/node-101"},
	}

	for _, tt := range tests {
		got := dnPrefix(tt.dn)
		if got != tt.want {
			t.Errorf("dnPrefix(%q) = %q, want %q", tt.dn, got, tt.want)
		}
	}
}

func TestResourceID_FromDN(t *testing.T) {
	id := resourceID("topology/pod-1/node-1")
	if id != "cisco.apic::topology/pod-1/node-1" {
		t.Errorf("resourceID = %q, want cisco.apic::topology/pod-1/node-1", id)
	}
	if resourceID("topology/pod-1/node-1") != id {
		t.Error("resourceID is not deterministic for the same DN")
	}
	if resourceID("topology/pod-1/node-2") == id {
		t.Error("different DNs must produce different IDs")
	}
}
