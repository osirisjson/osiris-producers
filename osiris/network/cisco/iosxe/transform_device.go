// transform_device.go - Device identity, classification and the root
// device resource for the Cisco IOS-XE OSIRIS JSON Producer.
//
// Every resource and group this producer emits derives its ID from one
// device key: the chassis serial number reported by
// Cisco-IOS-XE-device-hardware-oper. IDs follow the namespaced
// native-ID form <provider>::<native-id> (cisco.iosxe::<serial> for
// the device, cisco.iosxe::<serial>/<interface> for its interfaces), so
// renaming the collection target, or reaching the device through a
// different address, never changes an emitted ID. The target address is
// kept only as properties.management_ip and the device name is the
// hostname the device itself reports.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package iosxe

import (
	"regexp"
	"strconv"
	"strings"

	"go.osirisjson.org/producers/pkg/sdk"
)

// providerName is the dotted vendor.product-line provider name used for
// provider.name and as the ID namespace of every resource this device
// owns.
const providerName = "cisco.iosxe"

// unknownProviderName is the provider.name of stub resources for remote
// devices seen only through a neighbor protocol. CDP does not establish
// that the remote device runs IOS-XE, or that it is Cisco hardware.
const unknownProviderName = "unknown"

// chassisHWType is the device-inventory hw-type of a chassis row.
const chassisHWType = "hw-type-chassis"

// resourceID builds a namespaced native ID, <provider>::<native-id>.
func resourceID(prov, nativeID string) string {
	return prov + "::" + nativeID
}

// unresolvedStubID anchors a stub resource for something observed but
// never queried directly (a CDP neighbor) under the owning device's ID,
// so the producer does not mint an identity for a device it cannot
// resolve. category names the observation kind; parts are the observed
// identifying values in order.
func unresolvedStubID(deviceID, category string, parts ...string) string {
	return deviceID + "/" + category + "/" + strings.Join(parts, "/")
}

// chassisComponent returns the chassis row of the hardware inventory.
// When several chassis rows are present, as on a switch stack, the row
// with the lowest hw-dev-index wins, so the choice does not depend on
// reply order.
func chassisComponent(hw hwInventory) (hwComponent, bool) {
	var best hwComponent
	bestIdx, found := 0, false
	for _, c := range hw.Components {
		if strings.TrimSpace(c.Name) != chassisHWType {
			continue
		}
		idx, err := strconv.Atoi(strings.TrimSpace(c.HwDevIndex))
		if err != nil {
			idx = int(^uint(0) >> 1)
		}
		if !found || idx < bestIdx {
			best, bestIdx, found = c, idx, true
		}
	}
	return best, found
}

// deviceNativeKey returns the device key every emitted ID derives from:
// the chassis serial number, or targetHost when the device reported no
// chassis serial.
func deviceNativeKey(targetHost string, hw hwInventory) string {
	if c, ok := chassisComponent(hw); ok {
		if serial := strings.TrimSpace(c.SerialNumber); serial != "" {
			return serial
		}
	}
	return targetHost
}

// softwareVersionRe extracts the release from the IOS banner, e.g.
// "..., Version 17.15.1a, RELEASE SOFTWARE (fc3)".
var softwareVersionRe = regexp.MustCompile(`\bVersion ([0-9][^,\s]*)`)

// releaseVersion returns the running release reported in
// device-system-data, falling back to the native config "version" line,
// which carries only the major.minor configuration syntax version.
func releaseVersion(native nativeSystem, hw hwInventory) string {
	if m := softwareVersionRe.FindStringSubmatch(hw.SystemData.SoftwareVersion); m != nil {
		return m[1]
	}
	return strings.TrimSpace(native.Version)
}

// classifyDevice maps the device to network.switch or network.router
// from the image family in its own software banner ("Cisco IOS Software
// [<train>], <family> Software (<image>), Version ..."). Catalyst
// switches name a "... Switch Software" family; every other IOS-XE
// family (ISR, ASR, Catalyst 8000, virtual) is a router. Advertised YANG
// modules cannot distinguish the two, because routers advertise the
// switching models as well.
func classifyDevice(hw hwInventory) string {
	if strings.Contains(hw.SystemData.SoftwareVersion, "Switch Software") {
		return "network.switch"
	}
	return "network.router"
}

// TransformDevice builds the root device resource. targetHost is the
// address used to reach the device (properties.management_ip and the
// identity fallback); targetHostname is the operator-supplied target
// label, used as the name only when the device reports no hostname. It
// returns the resource, its ID and the device key.
func TransformDevice(targetHost, targetHostname string, native nativeSystem, hw hwInventory) (sdk.Resource, string, string) {
	key := deviceNativeKey(targetHost, hw)
	id := resourceID(providerName, key)
	resType := classifyDevice(hw)
	version := releaseVersion(native, hw)

	chassis, _ := chassisComponent(hw)
	model := strings.TrimSpace(chassis.PartNumber)
	serial := strings.TrimSpace(chassis.SerialNumber)

	r, err := sdk.NewResource(id, resType, sdk.Provider{
		Name:     providerName,
		NativeID: key,
		Type:     model,
		Version:  version,
	})
	if err != nil {
		return sdk.Resource{}, "", ""
	}

	r.Name = strings.TrimSpace(native.Hostname)
	if r.Name == "" {
		r.Name = targetHostname
	}
	if r.Name == "" {
		r.Name = targetHost
	}
	r.Status = "active"

	props := map[string]any{"manufacturer": "Cisco"}
	if model != "" {
		props["model"] = model
	}
	if version != "" {
		props["version"] = version
	}
	if serial != "" {
		props["serial_number"] = serial
	}
	if targetHost != "" {
		props["management_ip"] = targetHost
	}
	r.Properties = props

	return r, id, key
}
