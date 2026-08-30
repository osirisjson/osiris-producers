// transform_inventory_test.go - Unit tests for transform_inventory.go.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package nxos

import (
	"testing"
)

func TestTransformInventory(t *testing.T) {
	inv := inventoryResponse{
		TableInv: inventoryTable{
			RowInv: rowList[inventoryRow]{
				// NX-API wraps these free-text fields in literal quote
				// characters; trimmed() must strip them.
				{Name: `"Chassis"`, Desc: `"Nexus9000 C9508 Chassis"`, ProductID: "N9K-C9508", VendorID: "V01", SerialNum: "TST0000NX01"},
				{Name: "Slot 1", Desc: "Supervisor Module", ProductID: "N9K-SUP-B+", SerialNum: "TST0000SUP1"},
			},
		},
	}

	// documentation purpose: serial is withheld.
	items := TransformInventory(inv, false)
	if len(items) != 2 {
		t.Fatalf("expected 2 inventory items, got %d", len(items))
	}
	if items[0]["name"] != "Chassis" {
		t.Errorf("name should have its NX-API quote wrapper stripped: %q", items[0]["name"])
	}
	if items[0]["description"] != "Nexus9000 C9508 Chassis" {
		t.Errorf("description should have its NX-API quote wrapper stripped: %q", items[0]["description"])
	}
	if _, ok := items[0]["serial"]; ok {
		t.Errorf("serial should be absent at documentation purpose, got %v", items[0]["serial"])
	}

	// audit purpose: serial is present.
	auditItems := TransformInventory(inv, true)
	if auditItems[0]["serial"] != "TST0000NX01" {
		t.Errorf("serial: %v", auditItems[0]["serial"])
	}
}

func TestTransformEnvironment(t *testing.T) {
	env := environmentResponse{
		PowerSup: environmentPowerSup{
			TablePSInfo: psuTable{
				RowPSInfo: rowList[psuRow]{
					{PSNum: "1", PSModel: "NXA-PAC-1100W", PSStatus: "ok", TotCapa: "1100 W"},
				},
			},
		},
		FanDetails: environmentFanDetails{
			TableFanInfo: fanTable{
				RowFanInfo: rowList[fanRow]{
					{FanName: "Fan1(sys_fan1)", FanModel: "NXA-FAN-30CFM-F", FanStatus: "Ok", FanDir: "back-to-front"},
					{FanName: "Fan_in_PS1", FanModel: "--", FanStatus: "Ok", FanDir: "back-to-front"},
				},
			},
		},
		TableTempInfo: tempTable{
			RowTempInfo: rowList[tempRow]{
				{TempMod: "1", Sensor: "CPU", AlarmStatus: "Ok", MajThres: "90", MinThres: "80"},
			},
		},
	}

	ext := TransformEnvironment(env)

	psus, ok := ext["power_supplies"].([]map[string]any)
	if !ok || len(psus) != 1 {
		t.Fatalf("expected 1 PSU, got %v", ext["power_supplies"])
	}
	if psus[0]["model"] != "NXA-PAC-1100W" {
		t.Errorf("psu model: %v", psus[0]["model"])
	}
	if psus[0]["capacity"] != "1100 W" {
		t.Errorf("psu capacity: %v", psus[0]["capacity"])
	}
	if _, ok := psus[0]["actual_output"]; ok {
		t.Errorf("psu actual_output should be excluded as volatile telemetry, got %v", psus[0]["actual_output"])
	}

	fans, ok := ext["fans"].([]map[string]any)
	if !ok || len(fans) != 2 {
		t.Fatalf("expected 2 fans, got %v", ext["fans"])
	}
	if fans[0]["name"] != "Fan1(sys_fan1)" || fans[0]["model"] != "NXA-FAN-30CFM-F" || fans[0]["status"] != "Ok" || fans[0]["direction"] != "back-to-front" {
		t.Errorf("fan[0]: %v", fans[0])
	}
	if _, ok := fans[1]["model"]; ok {
		t.Errorf("fan model placeholder \"--\" should be omitted, got %v", fans[1]["model"])
	}

	temps, ok := ext["temperature"].([]map[string]any)
	if !ok || len(temps) != 1 {
		t.Fatalf("expected 1 temp sensor, got %v", ext["temperature"])
	}
	if temps[0]["major_threshold_c"] != "90" || temps[0]["minor_threshold_c"] != "80" {
		t.Errorf("temp thresholds: %v", temps[0])
	}
	if _, ok := temps[0]["current"]; ok {
		t.Errorf("temp current reading should be excluded as volatile telemetry, got %v", temps[0]["current"])
	}
}

// TestTransformEnvironment_PaddedFieldsAreTrimmed guards against a
// real production discrepancy: the same "tot_capa" value ("500 W")
// came back column-padded ("  500 W") on one live query and unpadded
// on another raw capture NX-API's own JSON serializer, not this
// producer, appears to sometimes carry over CLI display padding.
// Every NXOS-P2-06 field is trimmed defensively against the same
// quirk.
func TestTransformEnvironment_PaddedFieldsAreTrimmed(t *testing.T) {
	env := environmentResponse{
		PowerSup: environmentPowerSup{
			TablePSInfo: psuTable{
				RowPSInfo: rowList[psuRow]{
					{PSNum: "1", PSModel: "NXA-PAC-500W-PE", PSStatus: "Ok", TotCapa: "  500 W"},
				},
			},
		},
		FanDetails: environmentFanDetails{
			TableFanInfo: fanTable{
				RowFanInfo: rowList[fanRow]{
					{FanName: " Fan1(sys_fan1) ", FanModel: "NXA-FAN-30CFM-F", FanStatus: "Ok", FanDir: "back-to-front"},
				},
			},
		},
		TableTempInfo: tempTable{
			RowTempInfo: rowList[tempRow]{
				{TempMod: "1", Sensor: "CPU", AlarmStatus: "Ok", MajThres: " 90", MinThres: "80 "},
			},
		},
	}

	ext := TransformEnvironment(env)

	psus := ext["power_supplies"].([]map[string]any)
	if psus[0]["capacity"] != "500 W" {
		t.Errorf("psu capacity should be trimmed, got %q", psus[0]["capacity"])
	}
	fans := ext["fans"].([]map[string]any)
	if fans[0]["name"] != "Fan1(sys_fan1)" {
		t.Errorf("fan name should be trimmed, got %q", fans[0]["name"])
	}
	temps := ext["temperature"].([]map[string]any)
	if temps[0]["major_threshold_c"] != "90" || temps[0]["minor_threshold_c"] != "80" {
		t.Errorf("temp thresholds should be trimmed, got %v", temps[0])
	}
}

func TestTransformModules(t *testing.T) {
	mod := moduleResponse{
		TableModInfo: moduleInfoTable{
			RowModInfo: rowList[moduleInfoRow]{
				{ModInf: "1", Model: "N9K-C93180YC-FX", ModType: "48x10/25G/32G + 6x40/100G Ethernet/FC Module", Ports: "54", Status: "active *"},
			},
		},
		TableModDiagInfo: moduleDiagTable{
			RowModDiagInfo: rowList[moduleDiagRow]{
				{Mod: "1", DiagStatus: "Pass"},
			},
		},
		TableModWwnInfo: moduleWwnTable{
			RowModWwnInfo: rowList[moduleWwnRow]{
				{ModWwn: "1", HW: "1.2", SW: "10.3(6)", SlotType: "NA"},
			},
		},
	}

	modules := TransformModules(mod)
	if len(modules) != 1 {
		t.Fatalf("expected 1 module, got %d", len(modules))
	}
	m := modules[0]
	if m["module"] != "1" || m["model"] != "N9K-C93180YC-FX" || m["ports"] != "54" || m["status"] != "active *" {
		t.Errorf("module info fields: %v", m)
	}
	if m["diag_status"] != "Pass" {
		t.Errorf("diag_status: %v", m["diag_status"])
	}
	if m["hw_version"] != "1.2" || m["sw_version"] != "10.3(6)" {
		t.Errorf("hw/sw version: %v", m)
	}
	if _, ok := m["slot_type"]; ok {
		t.Errorf("slot_type placeholder \"NA\" should be omitted, got %v", m["slot_type"])
	}
}

func TestTransformModules_MismatchedModuleNumbersMergeSeparately(t *testing.T) {
	mod := moduleResponse{
		TableModInfo: moduleInfoTable{
			RowModInfo: rowList[moduleInfoRow]{
				{ModInf: "1", Model: "N9K-C9508"},
				{ModInf: "2", Model: "N9K-SUP-B+"},
			},
		},
		TableModDiagInfo: moduleDiagTable{
			RowModDiagInfo: rowList[moduleDiagRow]{
				{Mod: "1", DiagStatus: "Pass"},
			},
		},
	}

	modules := TransformModules(mod)
	if len(modules) != 2 {
		t.Fatalf("expected 2 modules, got %d: %v", len(modules), modules)
	}
	if modules[0]["module"] != "1" || modules[0]["diag_status"] != "Pass" {
		t.Errorf("module 1: %v", modules[0])
	}
	if modules[1]["module"] != "2" {
		t.Errorf("module 2: %v", modules[1])
	}
	if _, ok := modules[1]["diag_status"]; ok {
		t.Errorf("module 2 should have no diag_status, got %v", modules[1]["diag_status"])
	}
}

func TestTransformTransceivers(t *testing.T) {
	tr := transceiverResponse{
		TableInterface: transceiverTable{
			RowInterface: rowList[transceiverRow]{
				{Interface: "Ethernet1/1", SFP: "present", Name: "CISCO-ACCELINK", CiscoProductID: "SFP-10G-SR", PartNum: "RTXM228-551-C98", SerialNum: "TST0000SFP01", Type: "10Gbase-SR"},
				{Interface: "Ethernet1/2", SFP: "not present"},
			},
		},
	}

	// documentation purpose: serial_number withheld.
	out := TransformTransceivers(tr, false)
	if len(out) != 1 {
		t.Fatalf("expected 1 transceiver entry (empty port omitted), got %d: %v", len(out), out)
	}
	x, ok := out["Ethernet1/1"]
	if !ok {
		t.Fatalf("expected Ethernet1/1 entry, got %v", out)
	}
	if x["vendor"] != "CISCO-ACCELINK" || x["model"] != "SFP-10G-SR" || x["part_number"] != "RTXM228-551-C98" {
		t.Errorf("transceiver fields: %v", x)
	}
	if _, ok := x["serial_number"]; ok {
		t.Errorf("serial_number should be absent at documentation purpose, got %v", x["serial_number"])
	}
	if _, ok := x["form_factor"]; ok {
		t.Errorf("form_factor should never be populated (no grounded source field), got %v", x["form_factor"])
	}

	// audit purpose: serial_number present.
	auditOut := TransformTransceivers(tr, true)
	if auditOut["Ethernet1/1"]["serial_number"] != "TST0000SFP01" {
		t.Errorf("serial_number: %v", auditOut["Ethernet1/1"]["serial_number"])
	}
}
