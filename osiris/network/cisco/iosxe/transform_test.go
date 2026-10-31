// transform_test.go - Unit tests for IOS-XE->OSIRIS JSON transform
// functions. Device replies are synthetic fixtures under
// testdata/osiris/network/cisco/iosxe/ see its README.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package iosxe

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"go.osirisjson.org/producers/pkg/testharness"
)

// unitXML returns the synthetic device reply of one unit test case from
// cases, the inline case table of the test file that owns it.
// A missing case fails the test.
func unitXML(t *testing.T, cases map[string]string, name string) []byte {
	t.Helper()
	x, ok := cases[name]
	if !ok {
		t.Fatalf("no unit test case %q", name)
	}
	return []byte(x)
}

// fixtureFile names the scenario fixture file of a discovery operation:
// the operation name in snake_case, with an .xml extension.
func fixtureFile(op string) string {
	return strings.ReplaceAll(op, "-", "_") + ".xml"
}

// unitFixture loads a synthetic device reply from the isr4451 testdata
// scenario, by discovery operation.
func unitFixture(t *testing.T, op string) []byte {
	t.Helper()
	return testharness.LoadFixture(t, filepath.Join(fixtureDir, "netconf", fixtureFile(op)))
}

func TestTransformDevice(t *testing.T) {
	r, id, key := TransformDevice("192.0.2.1", "LAB-TARGET", mustDecode[nativeSystem](t, unitFixture(t, "native-system")), mustDecode[hwInventory](t, unitFixture(t, "hardware-inventory")))
	if id != "cisco.iosxe::TST0000XE01" || key != "TST0000XE01" {
		t.Errorf("id/key = %q/%q, want cisco.iosxe::TST0000XE01/TST0000XE01", id, key)
	}
	if r.ID != id || r.Provider.NativeID != key || r.Provider.Name != "cisco.iosxe" {
		t.Errorf("resource id %q, provider %+v", r.ID, r.Provider)
	}
	if r.Name != "LAB-RTR01" {
		t.Errorf("name should be the device-reported hostname, got %s", r.Name)
	}
	if r.Type != "network.router" {
		t.Errorf("type: %s", r.Type)
	}
	if r.Status != "active" {
		t.Errorf("status: %s", r.Status)
	}
	want := map[string]any{
		"manufacturer":  "Cisco",
		"model":         "ISR4451-X/K9",
		"version":       "17.12.4",
		"serial_number": "TST0000XE01",
		"management_ip": "192.0.2.1",
	}
	for k, v := range want {
		if r.Properties[k] != v {
			t.Errorf("properties.%s = %v, want %v", k, r.Properties[k], v)
		}
	}
	for _, gone := range []string{"hostname", "serial"} {
		if _, ok := r.Properties[gone]; ok {
			t.Errorf("properties.%s should no longer be emitted", gone)
		}
	}
	if r.Provider.Type != "ISR4451-X/K9" || r.Provider.Version != "17.12.4" {
		t.Errorf("provider type/version = %q/%q", r.Provider.Type, r.Provider.Version)
	}
}

func TestTransformDevice_IdentityIndependentOfTarget(t *testing.T) {
	native := mustDecode[nativeSystem](t, unitFixture(t, "native-system"))
	hw := mustDecode[hwInventory](t, unitFixture(t, "hardware-inventory"))
	a, idA, _ := TransformDevice("192.0.2.1", "LAB-TARGET-A", native, hw)
	b, idB, _ := TransformDevice("lab-rtr01.example.com", "LAB-TARGET-B", native, hw)
	if idA != idB || a.Name != b.Name {
		t.Errorf("a target alias/address change must not change identity: %q/%q vs %q/%q", idA, a.Name, idB, b.Name)
	}
	if a.Properties["management_ip"] == b.Properties["management_ip"] {
		t.Error("management_ip should still reflect the target used")
	}
}

func TestTransformDevice_Fallbacks(t *testing.T) {
	// No hardware inventory: the key falls back to the target address.
	r, id, key := TransformDevice("192.0.2.1", "LAB-TARGET", nativeSystem{}, hwInventory{})
	if key != "192.0.2.1" || id != "cisco.iosxe::192.0.2.1" {
		t.Errorf("fallback id/key = %q/%q", id, key)
	}
	if r.Name != "LAB-TARGET" {
		t.Errorf("without a device hostname the target label names the device, got %q", r.Name)
	}
	if _, ok := r.Properties["version"]; ok {
		t.Error("no version source: version must be omitted, not a placeholder")
	}
	if _, ok := r.Properties["model"]; ok {
		t.Error("no chassis row: model must be omitted")
	}

	// Neither hostname nor label: the target address names the device.
	r, _, _ = TransformDevice("192.0.2.1", "", nativeSystem{}, hwInventory{})
	if r.Name != "192.0.2.1" {
		t.Errorf("name = %q, want the target address", r.Name)
	}

	// Without a banner, version falls back to the native config line.
	r, _, _ = TransformDevice("192.0.2.1", "", nativeSystem{Version: "17.3"}, hwInventory{})
	if r.Properties["version"] != "17.3" {
		t.Errorf("version = %v, want 17.3", r.Properties["version"])
	}
}

func TestChassisComponent_Stack(t *testing.T) {
	hw := mustDecode[hwInventory](t, []byte(`<device-hardware-data xmlns="`+nsHardwareOper+`"><device-hardware>
  <device-inventory><hw-type>hw-type-transceiver</hw-type><hw-dev-index>0</hw-dev-index><serial-number>TST0000SFP1</serial-number></device-inventory>
  <device-inventory><hw-type>hw-type-chassis</hw-type><hw-dev-index>2</hw-dev-index><part-number>C9300X-24HX</part-number><serial-number>TST-CHASSIS-02</serial-number></device-inventory>
  <device-inventory><hw-type>hw-type-chassis</hw-type><hw-dev-index>1</hw-dev-index><part-number>C9300X-24HX</part-number><serial-number>TST-CHASSIS-01</serial-number></device-inventory>
</device-hardware></device-hardware-data>`))
	c, ok := chassisComponent(hw)
	if !ok || c.SerialNumber != "TST-CHASSIS-01" {
		t.Errorf("stack: chassis = %+v, want the lowest hw-dev-index member", c)
	}
	if key := deviceNativeKey("192.0.2.1", hw); key != "TST-CHASSIS-01" {
		t.Errorf("device key = %q", key)
	}
}

func TestClassifyDevice(t *testing.T) {
	cases := []struct {
		banner string
		want   string
	}{
		{"Cisco IOS Software [IOSXE], Virtual XE Software (X86_64_LINUX_IOSD-UNIVERSALK9-M), Version 17.15.1a, RELEASE SOFTWARE (fc3)", "network.router"},
		{"Cisco IOS Software [Dublin], ISR Software (X86_64_LINUX_IOSD-UNIVERSALK9-M), Version 17.12.4, RELEASE SOFTWARE (fc1)", "network.router"},
		{"Cisco IOS Software [Cupertino], ASR1000 Software (X86_64_LINUX_IOSD-UNIVERSALK9-M), Version 17.9.4a, RELEASE SOFTWARE (fc3)", "network.router"},
		{"Cisco IOS Software [IOSXE], Catalyst L3 Switch Software (CAT9K_IOSXE), Version 17.12.3, RELEASE SOFTWARE (fc7)", "network.switch"},
		{"", "network.router"},
	}
	for _, tc := range cases {
		var hw hwInventory
		hw.SystemData.SoftwareVersion = tc.banner
		if got := classifyDevice(hw); got != tc.want {
			t.Errorf("classifyDevice(%q) = %s, want %s", tc.banner, got, tc.want)
		}
		if m := softwareVersionRe.FindStringSubmatch(tc.banner); tc.banner != "" && m == nil {
			t.Errorf("no release parsed from %q", tc.banner)
		}
	}
}

func TestTransformInventory(t *testing.T) {
	hw := mustDecode[hwInventory](t, unitFixture(t, "hardware-inventory"))
	for _, item := range TransformInventory(hw, false) {
		if _, ok := item["serial"]; ok {
			t.Errorf("serial must be audit-only: %v", item)
		}
	}
	items := TransformInventory(hw, true)
	if len(items) != 3 {
		t.Fatalf("expected 3 inventory items, got %d", len(items))
	}
	if items[0]["name"] != "Chassis" {
		t.Errorf("name: %v", items[0]["name"])
	}
	if items[0]["serial"] != "TST0000XE01" {
		t.Errorf("serial: %v", items[0]["serial"])
	}
	if items[0]["part_number"] != "ISR4451-X/K9" {
		t.Errorf("part_number: %v", items[0]["part_number"])
	}
	if items[1]["name"] != "NIM subslot 0/0" {
		t.Errorf("name[1]: %v", items[1]["name"])
	}
	if items[2]["name"] != "TenGigabitEthernet0/1/0 SFP" {
		t.Errorf("name[2]: %v", items[2]["name"])
	}
}

func TestResourceIDs(t *testing.T) {
	if got := resourceID(providerName, "TST0000XE01/GigabitEthernet1"); got != "cisco.iosxe::TST0000XE01/GigabitEthernet1" {
		t.Errorf("resourceID = %q", got)
	}
	if got := unresolvedStubID("cisco.iosxe::TST0000XE01", "neighbor", "LAB-SW01", "Gi1/0/1"); got != "cisco.iosxe::TST0000XE01/neighbor/LAB-SW01/Gi1/0/1" {
		t.Errorf("unresolvedStubID = %q", got)
	}
}

func TestParentInterface(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"GigabitEthernet0/0/0.100", "GigabitEthernet0/0/0"},
		{"TenGigabitEthernet0/1/0.101", "TenGigabitEthernet0/1/0"},
		{"GigabitEthernet0/0/0", ""},
		{"Loopback0", ""},
		{"Tunnel100", ""},
		{"Port-channel1", ""},
	}
	for _, tc := range cases {
		got := parentInterface(tc.in)
		if got != tc.want {
			t.Errorf("parentInterface(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// mustDecode decodes a test fixture payload, failing the test on any
// error other than an empty payload.
func mustDecode[T model](t *testing.T, data []byte) T {
	t.Helper()
	v, err := decodeModel[T](data)
	if err != nil && !errors.Is(err, errEmptyModel) {
		t.Fatalf("decodeModel[%T]: %v", v, err)
	}
	return v
}
