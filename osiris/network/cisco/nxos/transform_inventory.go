// transform_inventory.go - Stable hardware inventory for the device's
// osiris.cisco extension: "show inventory" (FRU list), "show module"
// (module operational state), "show interface transceiver" (per-port
// optics), and "show environment" (PSU/fan/temperature posture).
// Volatile telemetry is excluded throughout, even at audit purpose.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package nxos

// TransformInventory converts "show inventory" output into an
// inventory array for the device's cisco extension. Serial numbers are
// audit-only, matching every other hardware-serial field this producer
// emits (the same asset-identifying-data tier as a device's own
// chassis serial) identity/classification fields (name, description,
// product/vendor ID) are not sensitive and stay at documentation
// purpose.
func TransformInventory(inventory inventoryResponse, audit bool) []map[string]any {
	var items []map[string]any
	for _, row := range inventory.TableInv.RowInv {
		item := map[string]any{}
		if v := trimmed(row.Name); v != "" {
			item["name"] = v
		}
		if v := trimmed(row.Desc); v != "" {
			item["description"] = v
		}
		if v := trimmed(row.ProductID); v != "" {
			item["product_id"] = v
		}
		if v := trimmed(row.VendorID); v != "" {
			item["vendor_id"] = v
		}
		if audit {
			if v := trimmed(row.SerialNum); v != "" && v != "N/A" {
				item["serial"] = v
			}
		}
		if len(item) > 0 {
			items = append(items, item)
		}
	}
	return items
}

// TransformModules converts "show module" output into a module status
// array for the device's cisco extension: operational status, POST
// diagnostic result, and hardware/software version per module.
// Distinct from TransformInventory's static FRU list ("show
// inventory") this is the module's own operational state, not its
// identity. The three source tables this command returns each key the
// same module by a differently-named field (modinf/mod/modwwn -
// NX-API's own inconsistency, not this producer's), merged here by
// that shared module number so each module appears once with every
// field it reported.
func TransformModules(mod moduleResponse) []map[string]any {
	merged := make(map[string]map[string]any)
	var order []string
	ensure := func(num string) map[string]any {
		if num == "" {
			return nil
		}
		m, ok := merged[num]
		if !ok {
			m = map[string]any{"module": num}
			merged[num] = m
			order = append(order, num)
		}
		return m
	}

	for _, row := range mod.TableModInfo.RowModInfo {
		m := ensure(trimmed(row.ModInf))
		if m == nil {
			continue
		}
		if v := trimmed(row.Model); v != "" {
			m["model"] = v
		}
		if v := trimmed(row.ModType); v != "" {
			m["type"] = v
		}
		if v := trimmed(row.Ports); v != "" {
			m["ports"] = v
		}
		if v := trimmed(row.Status); v != "" {
			m["status"] = v
		}
	}
	for _, row := range mod.TableModDiagInfo.RowModDiagInfo {
		m := ensure(trimmed(row.Mod))
		if m == nil {
			continue
		}
		if v := trimmed(row.DiagStatus); v != "" {
			m["diag_status"] = v
		}
	}
	for _, row := range mod.TableModWwnInfo.RowModWwnInfo {
		m := ensure(trimmed(row.ModWwn))
		if m == nil {
			continue
		}
		if v := trimmed(row.HW); v != "" {
			m["hw_version"] = v
		}
		if v := trimmed(row.SW); v != "" {
			m["sw_version"] = v
		}
		if v := trimmed(row.SlotType); v != "" && v != "NA" {
			m["slot_type"] = v
		}
	}

	var modules []map[string]any
	for _, num := range order {
		modules = append(modules, merged[num])
	}
	return modules
}

// TransformTransceivers converts "show interface transceiver" output
// into a map of normalized interface name -> transceiver info
// (OSIRIS-JSON-v1.0 5.4.2's transceiver object shape), one entry per
// port with an SFP/QSFP module physically present. Ports reporting
// "sfp: not present" are omitted entirely, not represented with an
// empty object. serial_number is audit-only, matching every other
// hardware-serial field this producer emits.
//
// form_factor (spec: "sfp"/"sfp+"/"sfp28"/"qsfp28"/"qsfp-dd") is
// deliberately not populated: NX-API reports no discrete field for it
// here, and deriving it from cisco_product_id's naming convention
// (e.g. "SFP-10G-SR" implies sfp+) would be an unverified guess rather
// than real device data the same category of mistake already made
// once for a different producer's vrf_id naming and not repeated here.
func TransformTransceivers(t transceiverResponse, audit bool) map[string]map[string]any {
	out := make(map[string]map[string]any)
	for _, row := range t.TableInterface.RowInterface {
		if trimmed(row.SFP) != "present" {
			continue
		}
		ifName := normalizeIfName(trimmed(row.Interface))
		if ifName == "" {
			continue
		}
		xcvr := map[string]any{}
		if v := trimmed(row.Name); v != "" {
			xcvr["vendor"] = v
		}
		if v := trimmed(row.CiscoProductID); v != "" {
			xcvr["model"] = v
		}
		if v := trimmed(row.PartNum); v != "" {
			xcvr["part_number"] = v
		}
		if audit {
			if v := trimmed(row.SerialNum); v != "" {
				xcvr["serial_number"] = v
			}
		}
		if len(xcvr) > 0 {
			out[ifName] = xcvr
		}
	}
	return out
}

// TransformEnvironment converts "show environment" output into cisco
// extension fields for power supplies, fans, and temperature. Only
// stable posture is captured throughout: live readings (PSU
// actual_input/actual_out, sensor curtemp) are volatile telemetry,
// excluded per OSIRIS-JSON-v1.0 13.1.3 even though this whole block is
// already audit-gated by the caller "audit" widens stable-posture
// depth, it does not admit time-series data.
func TransformEnvironment(env environmentResponse) map[string]any {
	ext := make(map[string]any)

	// Power supplies.
	if len(env.PowerSup.TablePSInfo.RowPSInfo) > 0 {
		var psus []map[string]any
		for _, row := range env.PowerSup.TablePSInfo.RowPSInfo {
			psu := map[string]any{}
			if v := string(row.PSNum); v != "" {
				psu["id"] = v
			}
			if v := string(row.PSModel); v != "" {
				psu["model"] = v
			}
			if v := string(row.PSStatus); v != "" {
				psu["status"] = v
			}
			if v := trimmed(row.TotCapa); v != "" {
				psu["capacity"] = v
			}
			if len(psu) > 0 {
				psus = append(psus, psu)
			}
		}
		if len(psus) > 0 {
			ext["power_supplies"] = psus
		}
	}

	// Fans.
	if len(env.FanDetails.TableFanInfo.RowFanInfo) > 0 {
		var fans []map[string]any
		for _, row := range env.FanDetails.TableFanInfo.RowFanInfo {
			fan := map[string]any{}
			if v := trimmed(row.FanName); v != "" {
				fan["name"] = v
			}
			if v := trimmed(row.FanModel); v != "" && v != "--" {
				fan["model"] = v
			}
			if v := trimmed(row.FanStatus); v != "" {
				fan["status"] = v
			}
			if v := trimmed(row.FanDir); v != "" {
				fan["direction"] = v
			}
			if len(fan) > 0 {
				fans = append(fans, fan)
			}
		}
		if len(fans) > 0 {
			ext["fans"] = fans
		}
	}

	// Temperature sensors.
	if len(env.TableTempInfo.RowTempInfo) > 0 {
		var temps []map[string]any
		for _, row := range env.TableTempInfo.RowTempInfo {
			temp := map[string]any{}
			if v := string(row.TempMod); v != "" {
				temp["module"] = v
			}
			if v := string(row.Sensor); v != "" {
				temp["sensor"] = v
			}
			if v := string(row.AlarmStatus); v != "" {
				temp["alarm_status"] = v
			}
			if v := trimmed(row.MajThres); v != "" {
				temp["major_threshold_c"] = v
			}
			if v := trimmed(row.MinThres); v != "" {
				temp["minor_threshold_c"] = v
			}
			if len(temp) > 0 {
				temps = append(temps, temp)
			}
		}
		if len(temps) > 0 {
			ext["temperature"] = temps
		}
	}

	return ext
}
