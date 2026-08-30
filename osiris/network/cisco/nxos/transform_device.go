// transform_device.go - "show version" -> the root network.switch
// resource. See transform.go for the shared helpers this file uses
// (resourceID, and the extensionNamespace/providerName constants).
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package nxos

import (
	"fmt"
	"strings"

	"go.osirisjson.org/producers/pkg/sdk"
)

// deviceNativeKey returns the stable canonical identity for an NX-OS
// device: its chassis serial number when the device reported one,
// falling back to targetHost only when the serial is unavailable (a
// pre-authenticated Client injected in tests, or a real device that
// omitted proc_board_id). Every resource this device owns (the switch
// itself and its interfaces) derives its ID from this key so a target
// alias/hostname change in inventory never mints a new resource ID for
// the same physical hardware.
func deviceNativeKey(targetHost string, version versionResponse) string {
	if serial := string(version.ProcBoardID); serial != "" {
		return serial
	}
	return targetHost
}

// TransformDevice converts "show version" output into a single
// network.switch resource. targetHost is the address used to reach the
// device (becomes properties.management_ip and the identity fallback
// above); targetHostname is the operator-supplied target name, used
// only when the device itself reported no hostname.
func TransformDevice(targetHost, targetHostname string, version versionResponse) (sdk.Resource, string) {
	model := string(version.ChassisID)
	swVersion := string(version.NXOSVerStr)
	if swVersion == "" {
		swVersion = string(version.KickstartVerStr)
	}
	serial := string(version.ProcBoardID)

	name := string(version.HostName)
	if name == "" {
		name = targetHostname
	}

	const resType = "network.switch"
	canonicalKey := deviceNativeKey(targetHost, version)
	id := resourceID(providerName, canonicalKey)

	prov := sdk.Provider{
		Name:     providerName,
		NativeID: canonicalKey,
		Type:     model,
		Version:  swVersion,
	}

	r, err := sdk.NewResource(id, resType, prov)
	if err != nil {
		return sdk.Resource{}, ""
	}
	r.Name = name
	r.Status = "active"

	// manufacturer/model/version/serial_number/management_ip match
	// OSIRIS-JSON-v1.0 section 7.5.2's network.switch "Common
	// properties" table. manufacturer is a constant since this producer
	// only ever talks to Cisco NX-OS hardware. layer3_capable (also
	// listed in that table) is deliberately not emitted no command
	// this producer issues reports it, and inferring it from the model
	// number would be a guess presented as sourced data. port_count is
	// filled in by the caller once interface data is available (this
	// function only sees "show version").
	props := map[string]any{
		"manufacturer": "Cisco",
		"model":        model,
	}
	if swVersion != "" {
		props["version"] = swVersion
	}
	if serial != "" {
		props["serial_number"] = serial
	}
	if targetHost != "" {
		props["management_ip"] = targetHost
	}
	// memory_mb (unit in the key name, per OSIRIS-JSON-v1.0 section 4's
	// property-naming convention). Only converted when MemUnit is a
	// recognized, confirmed unit: "kb" is divided down to MB; "mb"
	// (kept only in case another NX-OS platform reports it) is
	// passed through as-is. An empty or unrecognized unit means the
	// conversion factor is not actually known, so memory_mb is omitted
	// entirely rather than emitting a value under a "_mb" key that
	// might not really be megabytes.
	if raw := int64(version.Memory); raw > 0 {
		switch strings.ToLower(strings.TrimSpace(string(version.MemUnit))) {
		case "kb":
			props["memory_mb"] = raw / 1024
		case "mb":
			props["memory_mb"] = raw
		}
	}

	// Cisco extensions on device.
	ext := make(map[string]any)
	if v := string(version.BiosVerStr); v != "" {
		ext["bios_version"] = v
	}
	if v := string(version.RRReason); v != "" {
		ext["last_reset_reason"] = v
	}
	if v := string(version.KernUptmDays); v != "" {
		days := v
		hrs := string(version.KernUptmHrs)
		mins := string(version.KernUptmMins)
		secs := string(version.KernUptmSecs)
		ext["kernel_uptime"] = fmt.Sprintf("%sd %sh %sm %ss", days, hrs, mins, secs)
	}
	// rr_sys_ver is the NX-OS version that was running at the last
	// reset/reload (part of "show version"'s rr_* reset-reason block),
	// not an uptime it pairs with last_reset_reason above (e.g.
	// "Reset due to upgrade" from "9.3(11)"). System uptime is
	// kernel_uptime.
	if v := string(version.RRSysVer); v != "" {
		ext["last_reset_version"] = v
	}

	if len(ext) > 0 {
		r.Extensions = map[string]any{extensionNamespace: ext}
	}

	r.Properties = props
	return r, id
}
