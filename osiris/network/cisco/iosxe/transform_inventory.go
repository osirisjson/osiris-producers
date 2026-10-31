// transform_inventory.go - Stable hardware inventory for the Cisco
// IOS-XE OSIRIS JSON Producer.
//
// The device's field-replaceable and fixed components (chassis, CPU,
// memory, storage, interface modules, transceivers, fan trays and power
// entry modules) come from Cisco-IOS-XE-device-hardware-oper, the same
// reply that supplies the device identity and are emitted as a
// structured inventory list in the device's osiris.cisco extension.
// Pluggable transceivers are additionally described per port from
// Cisco-IOS-XE-transceiver-oper, as the port's properties.transceiver
// object (vendor, part_number, serial_number, per OSIRIS JSON
// transceiver objects). Component and transceiver serial numbers are
// emitted at audit purpose only. Readings (temperature, power, optical
// levels) are volatile telemetry and are never decoded.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package iosxe

import (
	"strconv"
	"strings"

	"go.osirisjson.org/producers/pkg/sdk"
)

// hwClassNames maps a device-inventory hw-type, without its "hw-type-"
// prefix, to the inventory class emitted for it, where the YANG
// identifier is an abbreviation.
// Every other hw-type is emitted with the prefix removed.
var hwClassNames = map[string]string{
	"pim":     "interface-module",
	"pem":     "power-entry-module",
	"fantray": "fan-tray",
}

// inventoryClass returns the inventory class of a device-inventory row.
func inventoryClass(hwType string) string {
	t := strings.TrimPrefix(strings.TrimSpace(hwType), "hw-type-")
	if v, ok := hwClassNames[t]; ok {
		return v
	}
	return t
}

// TransformInventory converts the device-inventory rows into the
// inventory list of the device's osiris.cisco extension, in reply order.
// Each item carries its class, its index within that class, the device
// name and description, part number, hardware version and whether it
// is field-replaceable or virtual. Serial numbers are added at audit
// purpose only.
func TransformInventory(hw hwInventory, audit bool) []map[string]any {
	var items []map[string]any
	for _, c := range hw.Components {
		item := map[string]any{}
		if v := inventoryClass(c.Name); v != "" {
			item["class"] = v
		}
		if n, err := strconv.Atoi(strings.TrimSpace(c.HwDevIndex)); err == nil {
			item["index"] = n
		}
		if v := strings.TrimSpace(c.DevName); v != "" {
			item["name"] = v
		}
		if v := strings.TrimSpace(c.Description); v != "" {
			item["description"] = v
		}
		if v := strings.TrimSpace(c.PartNumber); v != "" {
			item["part_number"] = v
		}
		if v := strings.TrimSpace(c.Version); v != "" {
			item["hw_version"] = v
		}
		if b, err := strconv.ParseBool(strings.TrimSpace(c.FieldReplaceable)); err == nil {
			item["field_replaceable"] = b
		}
		if strings.TrimSpace(c.HwClass) == "hw-class-virtual" {
			item["virtual"] = true
		}
		if audit {
			if v := strings.TrimSpace(c.SerialNumber); v != "" {
				item["serial"] = v
			}
		}
		if len(item) > 0 {
			items = append(items, item)
		}
	}
	return items
}

// transceiverPrefix is the name prefix transceiver-oper gives a
// pluggable module: "Transceiver<slot>/<subslot>/<port>".
const transceiverPrefix = "Transceiver"

// transceiverPort resolves a transceiver name to the port resource it
// is plugged into. A name that is itself an interface name, full or
// abbreviated, resolves directly. A "Transceiver<position>" name
// resolves to the one port whose name is an interface type followed by
// exactly that position (Transceiver0/0/0 -> GigabitEthernet0/0/0);
// when no port, or more than one, has that position, the transceiver
// is left unresolved rather than guessed.
func transceiverPort(ifNameToID map[string]string, name string) (string, bool) {
	if _, id, ok := lookupInterface(ifNameToID, name); ok {
		return id, true
	}
	pos := strings.TrimPrefix(strings.TrimSpace(name), transceiverPrefix)
	if pos == strings.TrimSpace(name) || pos == "" || pos[0] < '0' || pos[0] > '9' {
		return "", false
	}
	var found string
	n := 0
	for ifName, id := range ifNameToID {
		prefix, ok := strings.CutSuffix(ifName, pos)
		if !ok || prefix == "" {
			continue
		}
		if c := prefix[len(prefix)-1]; !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z') {
			continue
		}
		found, n = id, n+1
	}
	if n != 1 {
		return "", false
	}
	return found, true
}

// EnrichTransceivers sets properties.transceiver on every port that has
// a present transceiver whose name resolves to the port (see
// transceiverPort). It returns the
// present transceivers that match no emitted port, for the device's
// osiris.cisco extension, so none is silently dropped. serial_number is
// added at audit purpose only.
func EnrichTransceivers(t transceiverOper, resources []sdk.Resource, ifNameToID map[string]string, audit bool) []map[string]any {
	idx := make(map[string]int, len(resources))
	for i, r := range resources {
		idx[r.ID] = i
	}
	var unmatched []map[string]any
	for _, x := range t.Transceivers {
		if p, err := strconv.ParseBool(strings.TrimSpace(x.Present)); err != nil || !p {
			continue
		}
		obj := map[string]any{}
		for k, v := range map[string]string{
			"vendor":       x.Vendor,
			"part_number":  x.VendorPart,
			"revision":     x.VendorRev,
			"identifier":   x.Identifier,
			"connector":    x.Connector,
			"ethernet_pmd": x.EthernetPMD,
		} {
			if v = strings.TrimSpace(v); v != "" {
				obj[k] = v
			}
		}
		if audit {
			if v := strings.TrimSpace(x.SerialNo); v != "" {
				obj["serial_number"] = v
			}
		}
		if len(obj) == 0 {
			continue
		}
		if id, ok := transceiverPort(ifNameToID, x.Name); ok {
			if i, ok := idx[id]; ok {
				if resources[i].Properties == nil {
					resources[i].Properties = map[string]any{}
				}
				resources[i].Properties["transceiver"] = obj
				continue
			}
		}
		if n := strings.TrimSpace(x.Name); n != "" {
			obj["name"] = n
		}
		unmatched = append(unmatched, obj)
	}
	return unmatched
}
