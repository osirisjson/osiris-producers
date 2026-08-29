// transform_device_test.go - Unit tests for transform_device.go.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package nxos

import (
	"testing"
)

func TestTransformDevice(t *testing.T) {
	version := versionResponse{
		ChassisID:    "Nexus9000 C9508",
		ProcBoardID:  "TST0000NX01",
		NXOSVerStr:   "10.3(4a)",
		HostName:     "LAB-SPINE01",
		BiosVerStr:   "08.42",
		RRReason:     "Reset Requested by CLI command reload",
		RRSysVer:     "9.3(11)",
		KernUptmDays: "10",
		KernUptmHrs:  "5",
		KernUptmMins: "30",
		KernUptmSecs: "15",
		Memory:       65536000,
		MemUnit:      "kB",
	}

	r, id := TransformDevice("198.51.100.10", "target-alias", version)
	if id == "" {
		t.Fatal("expected non-empty resource ID")
	}
	if id != "cisco.nxos::TST0000NX01" {
		t.Errorf("id should be the spec's namespaced-native-id form (OSIRIS-JSON-v1.0 2.1.2), got %q", id)
	}
	if r.ID != id {
		t.Errorf("r.ID should match the returned id: %s vs %s", r.ID, id)
	}
	if r.Name != "LAB-SPINE01" {
		t.Errorf("name should prefer the device's own reported hostname: %s", r.Name)
	}
	if r.Type != "network.switch" {
		t.Errorf("type: %s", r.Type)
	}
	if r.Status != "active" {
		t.Errorf("status: %s", r.Status)
	}
	if r.Provider.Name != "cisco.nxos" {
		t.Errorf("provider: %s", r.Provider.Name)
	}
	if r.Provider.NativeID != "TST0000NX01" {
		t.Errorf("native_id should be the chassis serial: %s", r.Provider.NativeID)
	}
	if r.Properties["manufacturer"] != "Cisco" {
		t.Errorf("manufacturer: %v", r.Properties["manufacturer"])
	}
	if r.Properties["model"] != "Nexus9000 C9508" {
		t.Errorf("model: %v", r.Properties["model"])
	}
	if r.Properties["version"] != "10.3(4a)" {
		t.Errorf("version: %v", r.Properties["version"])
	}
	if r.Properties["serial_number"] != "TST0000NX01" {
		t.Errorf("serial_number: %v", r.Properties["serial_number"])
	}
	if r.Properties["management_ip"] != "198.51.100.10" {
		t.Errorf("management_ip: %v", r.Properties["management_ip"])
	}
	if r.Properties["memory_mb"] != int64(64000) {
		t.Errorf("memory_mb: %v", r.Properties["memory_mb"])
	}
	if _, ok := r.Properties["memory"]; ok {
		t.Error("old 'memory' property key should not survive the memory_mb rename")
	}
	if _, ok := r.Properties["memory_type"]; ok {
		t.Error("memory_type is redundant once the unit is baked into memory_mb")
	}
	if _, ok := r.Properties["serial"]; ok {
		t.Error("old 'serial' property key should not survive the serial_number rename")
	}
	if _, ok := r.Properties["chassis_id"]; ok {
		t.Error("chassis_id duplicated model under a second key and should be dropped")
	}
	if _, ok := r.Properties["hostname"]; ok {
		t.Error("hostname property is redundant once r.Name sources from version.HostName")
	}
	if _, ok := r.Properties["layer3_capable"]; ok {
		t.Error("layer3_capable has no data source in this producer and must not be guessed")
	}

	// Check extensions.
	cisco := r.Extensions[extensionNamespace].(map[string]any)
	if cisco["bios_version"] != "08.42" {
		t.Errorf("bios_version: %v", cisco["bios_version"])
	}
	if cisco["last_reset_reason"] != "Reset Requested by CLI command reload" {
		t.Errorf("last_reset_reason: %v", cisco["last_reset_reason"])
	}
	if cisco["kernel_uptime"] != "10d 5h 30m 15s" {
		t.Errorf("kernel_uptime: %v", cisco["kernel_uptime"])
	}
	if cisco["last_reset_version"] != "9.3(11)" {
		t.Errorf("last_reset_version: %v", cisco["last_reset_version"])
	}
	if _, ok := cisco["uptime"]; ok {
		t.Error("rr_sys_ver must not be emitted as 'uptime' - it is the pre-reset NX-OS version, not an uptime")
	}
	for _, k := range []string{"cpu_idle", "load_avg_1min", "memory_used", "memory_free"} {
		if _, ok := cisco[k]; ok {
			t.Errorf("volatile telemetry key %q must not appear on the device extension", k)
		}
	}
}

func TestTransformDevice_MemoryUnitConversion(t *testing.T) {
	cases := []struct {
		name    string
		memory  flexInt64
		unit    flexString
		wantKey bool
		want    int64
	}{
		{"kB is converted down to MB", 65536000, "kB", true, 64000},
		{"mb is passed through as-is", 65536, "MB", true, 65536},
		{"unrecognized unit is omitted, not guessed", 65536000, "GB", false, 0},
		{"missing unit is omitted, not guessed", 65536000, "", false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := TransformDevice("198.51.100.10", "target", versionResponse{
				ProcBoardID: "TST0000NX01",
				Memory:      tc.memory,
				MemUnit:     tc.unit,
			})
			got, ok := r.Properties["memory_mb"]
			if ok != tc.wantKey {
				t.Fatalf("memory_mb presence = %v, want %v (value: %v)", ok, tc.wantKey, got)
			}
			if tc.wantKey && got != tc.want {
				t.Errorf("memory_mb = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTransformDevice_NameFallsBackToTargetHostname(t *testing.T) {
	version := versionResponse{ProcBoardID: "TST0000NX03"} // no HostName reported
	r, _ := TransformDevice("198.51.100.11", "LAB-SW03", version)
	if r.Name != "LAB-SW03" {
		t.Errorf("name should fall back to the target hostname, got %q", r.Name)
	}
}

func TestDeviceNativeKey(t *testing.T) {
	withSerial := versionResponse{ProcBoardID: "TST0000NX01"}
	if got := deviceNativeKey("198.51.100.10", withSerial); got != "TST0000NX01" {
		t.Errorf("expected chassis serial, got %q", got)
	}

	noSerial := versionResponse{}
	if got := deviceNativeKey("198.51.100.10", noSerial); got != "198.51.100.10" {
		t.Errorf("expected targetHost fallback, got %q", got)
	}
}

func TestTransformDevice_IdentityStableAcrossTargetAliasChange(t *testing.T) {
	// A device that reports a serial keeps the same resource ID no
	// matter what target host/hostname was used to reach it.
	version := versionResponse{ProcBoardID: "TST0000NX01", HostName: "LAB-SW01"}

	_, id1 := TransformDevice("198.51.100.10", "old-alias", version)
	_, id2 := TransformDevice("198.51.100.99", "new-alias", version)
	if id1 != id2 {
		t.Errorf("resource ID changed across target alias change: %s != %s", id1, id2)
	}
}
