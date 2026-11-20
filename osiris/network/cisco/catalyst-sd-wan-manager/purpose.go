// purpose.go - --purpose documentation field contract for the Cisco
// Catalyst SD-WAN Manager OSIRIS JSON Producer.
//
// OSIRIS JSON specification chapter 13.1.3 targets the documentation
// purpose at diagrams, inventories and high-level documentation and
// leaves out detailed IP addresses, serial numbers and configuration
// specifics. This producer keeps an explicit allowlist instead of the
// shared osirismeta.Project projection (which drops every property and
// extension), so a documentation document still labels interfaces,
// VPNs, transport colors and device models:
//
//	device (network.router, osiris.cisco.controller)
//	  properties: model, manufacturer, version
//	  extension:  site_id, health, site_role
//	interface (network.interface, network.router.port)
//	  properties: interface_name, interface_type, admin_status,
//	              oper_status, speed_mbps, vpn_id
//	  extension:  color, site_role
//	connection
//	  extension:  color, colors, protocol
//	group: extension site_id, unsited; no properties
//
// Everything else (addresses, MAC addresses, management IPs, site
// coordinates, system IPs, device groups, connected managers,
// timestamps, state text, duplex, MTU, encapsulation, NAT type, serial
// and chassis numbers, certificate state, quality scores, OMP sources
// and observed states, raw rows) is audit only. Identity, names,
// descriptions, provider fields, connections and group membership are
// kept in both purposes. --purpose audit is unchanged, secrets are
// redacted in both (see raw.go). Documentation is minimized, not
// anonymized.
//
// OSIRIS JSON Producer for Cisco Catalyst SD-WAN Manager introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification
package catalystsdwanmanager

import "go.osirisjson.org/producers/pkg/sdk"

// fieldAllowlist is the documentation field set for one element kind.
type fieldAllowlist struct {
	properties map[string]bool
	extension  map[string]bool
}

func keySet(keys ...string) map[string]bool {
	m := make(map[string]bool, len(keys))
	for _, k := range keys {
		m[k] = true
	}
	return m
}

var (
	documentationDevice = fieldAllowlist{
		properties: keySet("model", "manufacturer", "version"),
		extension:  keySet("site_id", "health", "site_role"),
	}
	documentationInterface = fieldAllowlist{
		properties: keySet("interface_name", "interface_type", "admin_status", "oper_status", "speed_mbps", "vpn_id"),
		extension:  keySet("color", "site_role"),
	}
	documentationConnection = fieldAllowlist{
		extension: keySet("color", "colors", "protocol"),
	}
	documentationGroup = fieldAllowlist{
		extension: keySet("site_id", "unsited"),
	}
	// documentationOther applies to any element kind not listed above
	// (for example a resource type added later): nothing beyond the
	// core fields until it is reviewed into an allowlist.
	documentationOther = fieldAllowlist{}
)

// resourceAllowlist returns the documentation allowlist for a resource
// type.
func resourceAllowlist(resType string) fieldAllowlist {
	switch resType {
	case "network.router", "osiris.cisco.controller":
		return documentationDevice
	case "network.interface", "network.router.port":
		return documentationInterface
	}
	return documentationOther
}

// filterProperties keeps only the allowed keys; it returns nil when
// none remain so the member is omitted from the document.
func filterProperties(m map[string]any, allowed map[string]bool) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		if allowed[k] {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// filterExtensions applies allowed to the producer's own extension
// namespace and drops any other namespace.
func filterExtensions(m map[string]any, allowed map[string]bool) map[string]any {
	ext, _ := m[extensionKey].(map[string]any)
	kept := filterProperties(ext, allowed)
	if kept == nil {
		return nil
	}
	return map[string]any{extensionKey: kept}
}

// applyPurpose enforces the documentation field contract in place.
// It is a no-op for audit.
func applyPurpose(purpose string, resources []sdk.Resource, connections []sdk.Connection, groups []sdk.Group) {
	if purpose == "audit" {
		return
	}
	for i := range resources {
		al := resourceAllowlist(resources[i].Type)
		resources[i].Properties = filterProperties(resources[i].Properties, al.properties)
		resources[i].Extensions = filterExtensions(resources[i].Extensions, al.extension)
	}
	for i := range connections {
		connections[i].Properties = filterProperties(connections[i].Properties, documentationConnection.properties)
		connections[i].Extensions = filterExtensions(connections[i].Extensions, documentationConnection.extension)
	}
	for i := range groups {
		groups[i].Properties = nil
		groups[i].Extensions = filterExtensions(groups[i].Extensions, documentationGroup.extension)
	}
}
