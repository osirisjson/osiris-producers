// transform.go - Shared constants and stateless helpers used across
// the transform_<area>.go files (device, interfaces, segmentation,
// port-channel/vPC, neighbors, routing, security, inventory). Each
// area file converts a group of typed NX-API response DTOs (see
// dto.go) into OSIRIS types; the identity/ID scheme, the "unknown"
// provider name for unresolved stubs, and the small normalization
// helpers all live here.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package nxos

import (
	"strings"

	"go.osirisjson.org/producers/pkg/sdk"
)

const extensionNamespace = "osiris.cisco"

// providerName is dotted (vendor.product-line)
// see the resourceID doc comment below for why resource IDs need this
// to agree with provider.name.
const providerName = "cisco.nxos"

// unknownProviderName is used for external/unresolved neighbor stub
// resources (LLDP/CDP/vPC keepalive peers this producer never
// connects to directly). Using providerName ("cisco.nxos") there would
// assert the remote device is this exact platform, which neither LLDP
// (vendor-neutral) nor CDP (Cisco-proprietary, but third-party devices
// can and do speak it) actually establishes.
const unknownProviderName = "unknown"

// trimmed normalizes a flexString field from NX-API, whose JSON
// serializer sometimes carries over the CLI's own display form:
//
//   - column padding: "show environment" power-supply "tot_capa" comes
//     back "  500 W" on some queries and "500 W" on others for the
//     identical value;
//   - literal quotes: "show inventory" name/description come back
//     wrapped in double-quote characters ("\"Chassis\"") that are part
//     of the string value, not JSON syntax.
//
// trimmed strips surrounding whitespace, then one matched leading/
// trailing double-quote pair, then any padding that pair enclosed.
func trimmed(s flexString) string {
	v := strings.TrimSpace(string(s))
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		v = strings.TrimSpace(v[1 : len(v)-1])
	}
	return v
}

// resourceID builds an OSIRIS-JSON-v1.0 section 2.1.2 "namespaced
// native ID" (<provider>::<native-id>, strategy 2 - the spec's own
// example is "cisco::FOC1234ABCD") directly from a stable native
// identifier. prov must be either providerName or
// unknownProviderName so a resource's id and its own provider.name
// always agree on which namespace it came from previously 0.1.0 this
// producer generated an opaque res-<type>-<hint>-<hash> ID via
// sdk.Hash16/DeriveHint that ignored the device's own real native
// identifier entirely, using native provider IDs maintains
// traceability to source systems. nativeID for a device sub-resource is
// a "/"-separated composite (e.g. "FDO12345LZ5/Ethernet1/1").
func resourceID(prov, nativeID string) string {
	return prov + "::" + nativeID
}

// unresolvedStubID anchors an external/unresolved stub resource's id
// (an LLDP/CDP neighbor, an OSPF/BGP neighbor, a vPC keepalive peer
// anything this producer saw evidence of but never queried directly)
// under the local device's own already-namespaced id, instead of
// minting a separate "unknown::<composite>" identity for a remote
// resource this producer cannot actually verify or resolve. deviceID
// is the owning switch's own full id (already "<provider>::<serial>");
// category names the kind of observation ("neighbor",
// "vpc-peer-keepalive", "ospf-neighbor", "bgp-neighbor"); parts are the
// observed identifying values in order (e.g. remote system, remote
// port). The returned stub resource's provider.name still stays
// unknownProviderName this only changes how the id itself is built,
// not what this producer claims to know about the remote device's own
// vendor (OSIRIS-JSON-v1.0 2.1.2's own id field is document-scoped and
// opaque, so a stub's id not sharing its literal provider.name prefix
// is not a spec violation "unknown" here would still be inventing a
// pseudo-identity for something this producer never actually resolved).
func unresolvedStubID(deviceID, category string, parts ...string) string {
	return deviceID + "/" + category + "/" + strings.Join(parts, "/")
}

// groupIndex builds a map of group ID -> index in slice for
// efficient mutation.
func groupIndex(groups []sdk.Group) map[string]int {
	idx := make(map[string]int, len(groups))
	for i, g := range groups {
		idx[g.ID] = i
	}
	return idx
}

// mapInterfaceStatus converts NX-OS interface state to
// OSIRIS status values.
func mapInterfaceStatus(state string) string {
	switch strings.ToLower(state) {
	case "up":
		return "active"
	case "down":
		return "inactive"
	default:
		return "unknown"
	}
}

// normalizeIfName normalizes interface name abbreviations to full form.
func normalizeIfName(name string) string {
	name = strings.TrimSpace(name)
	// Common NX-OS abbreviations.
	if strings.HasPrefix(name, "Eth") && !strings.HasPrefix(name, "Ethernet") {
		return "Ethernet" + strings.TrimPrefix(name, "Eth")
	}
	if strings.HasPrefix(name, "Po") && !strings.HasPrefix(name, "port-channel") {
		return "port-channel" + strings.TrimPrefix(name, "Po")
	}
	return name
}

// ensureCiscoExtension ensures the extensions map and
// osiris.cisco sub-map exist.
func ensureCiscoExtension(ext *map[string]any) {
	if *ext == nil {
		*ext = make(map[string]any)
	}
	if _, ok := (*ext)[extensionNamespace]; !ok {
		(*ext)[extensionNamespace] = make(map[string]any)
	}
}
