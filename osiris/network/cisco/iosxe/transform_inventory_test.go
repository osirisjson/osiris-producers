// transform_inventory_test.go - Tests for the hardware inventory and
// transceiver transforms of the Cisco IOS-XE OSIRIS JSON Producer.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package iosxe

import (
	"testing"

	"go.osirisjson.org/producers/pkg/sdk"
)

// inventoryCases are the synthetic device replies the inventory unit
// tests decode: one minimal NETCONF <data> payload per case, keyed
// by the behavior it exercises.
var inventoryCases = map[string]string{
	"hardware-fields": `<device-hardware-data xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-device-hardware-oper"><device-hardware>
  <device-inventory><hw-type>hw-type-pem</hw-type><hw-dev-index>2</hw-dev-index><version>V01</version><part-number>PWR-LAB</part-number><serial-number>TST0000PSU2</serial-number><dev-name>Power Supply B</dev-name><field-replaceable>true</field-replaceable><hw-class>hw-class-physical</hw-class></device-inventory>
  <device-inventory><hw-type>hw-type-cpu</hw-type><hw-dev-index>0</hw-dev-index><field-replaceable>false</field-replaceable><hw-class>hw-class-virtual</hw-class></device-inventory>
</device-hardware></device-hardware-data>`,
	"transceivers": `<transceiver-oper-data xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-transceiver-oper">
  <transceiver><name>Te1/1/1</name><present>true</present><vendor>LAB-OPTICS</vendor><vendor-part>SFP-LAB</vendor-part><serial-no>TST0000SFP1</serial-no></transceiver>
  <transceiver><name>TenGigabitEthernet1/1/2</name><present>false</present><vendor>LAB-OPTICS</vendor></transceiver>
  <transceiver><name>TenGigabitEthernet9/9/9</name><present>true</present><vendor>LAB-OPTICS</vendor><serial-no>TST0000SFP9</serial-no></transceiver>
</transceiver-oper-data>`,
}

func TestInventoryClass(t *testing.T) {
	for in, want := range map[string]string{
		"hw-type-chassis":     "chassis",
		"hw-type-pim":         "interface-module",
		"hw-type-pem":         "power-entry-module",
		"hw-type-fantray":     "fan-tray",
		"hw-type-transceiver": "transceiver",
		"hw-type-ssd":         "ssd",
		" hw-type-dram ":      "dram",
	} {
		if got := inventoryClass(in); got != want {
			t.Errorf("inventoryClass(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTransformInventory_Fields(t *testing.T) {
	hw := mustDecode[hwInventory](t, unitXML(t, inventoryCases, "hardware-fields"))
	got := mustJSON(t, TransformInventory(hw, true))
	want := `[{"class":"power-entry-module","field_replaceable":true,"hw_version":"V01","index":2,"name":"Power Supply B","part_number":"PWR-LAB","serial":"TST0000PSU2"},` +
		`{"class":"cpu","field_replaceable":false,"index":0,"virtual":true}]`
	if got != want {
		t.Errorf("inventory =\n%s\nwant\n%s", got, want)
	}
}

func TestEnrichTransceivers(t *testing.T) {
	x := mustDecode[transceiverOper](t, unitXML(t, inventoryCases, "transceivers"))
	resources := []sdk.Resource{{ID: "cisco.iosxe::TST/TenGigabitEthernet1/1/1"}, {ID: "cisco.iosxe::TST/TenGigabitEthernet1/1/2"}}
	ifNameToID := map[string]string{
		"TenGigabitEthernet1/1/1": resources[0].ID,
		"TenGigabitEthernet1/1/2": resources[1].ID,
	}

	unmatched := EnrichTransceivers(x, resources, ifNameToID, false)
	if s := mustJSON(t, resources[0].Properties["transceiver"]); s != `{"part_number":"SFP-LAB","vendor":"LAB-OPTICS"}` {
		t.Errorf("abbreviated name must resolve to the port; transceiver = %s", s)
	}
	if resources[1].Properties != nil {
		t.Errorf("a transceiver that is not present must not be attached: %v", resources[1].Properties)
	}
	if s := mustJSON(t, unmatched); s != `[{"name":"TenGigabitEthernet9/9/9","vendor":"LAB-OPTICS"}]` {
		t.Errorf("unmatched = %s", s)
	}

	resources[0].Properties = nil
	EnrichTransceivers(x, resources, ifNameToID, true)
	if resources[0].Properties["transceiver"].(map[string]any)["serial_number"] != "TST0000SFP1" {
		t.Errorf("audit purpose must add serial_number: %v", resources[0].Properties)
	}
}

func TestTransceiverPort(t *testing.T) {
	ifNameToID := map[string]string{
		"GigabitEthernet0/0/0":     "gi000",
		"GigabitEthernet0/0/0.100": "gi000.100",
		"GigabitEthernet10/0/0":    "gi1000",
		"TenGigabitEthernet1/1/1":  "te111",
		"GigabitEthernet1/1/1":     "gi111",
		"Loopback0":                "lo0",
	}
	for _, tc := range []struct {
		name, want string
		ok         bool
	}{
		{"GigabitEthernet0/0/0", "gi000", true},
		{"Gi0/0/0", "gi000", true},
		{"Transceiver0/0/0", "gi000", true}, // not GigabitEthernet10/0/0, not the subinterface.
		{"Transceiver1/1/1", "", false},     // two ports at 1/1/1: not guessed.
		{"Transceiver9/9/9", "", false},     // no port at that position.
		{"Transceiver", "", false},
		{"TransceiverX", "", false},
	} {
		got, ok := transceiverPort(ifNameToID, tc.name)
		if got != tc.want || ok != tc.ok {
			t.Errorf("transceiverPort(%q) = %q, %v; want %q, %v", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}
