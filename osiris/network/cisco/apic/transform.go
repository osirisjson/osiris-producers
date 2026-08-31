// transform.go - Shared helpers for the APIC->OSIRIS mapping. The
// per-domain transforms live in transform_fabric.go,
// transform_tenant.go, transform_endpoints.go and transform_faults.go;
// this file holds only the constants and stateless
// DN/identity/extension helpers they share.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package apic

import (
	"strings"

	"go.osirisjson.org/producers/pkg/sdk"
)

const extensionNamespace = "osiris.cisco"

// providerName is the dotted vendor.product identity carried in every
// resource's provider.name and in metadata.scope.providers.
// The vendor extension namespace stays "osiris.cisco".
const providerName = "cisco.apic"

// extractParentDN strips a known suffix from a DN to get the
// parent object DN.
func extractParentDN(dn, suffix string) string {
	if !strings.HasSuffix(dn, suffix) {
		return ""
	}
	return dn[:len(dn)-len(suffix)]
}

// extractLastSegment returns the last path segment of a DN.
func extractLastSegment(dn string) string {
	idx := strings.LastIndex(dn, "/")
	if idx < 0 {
		return dn
	}
	return dn[idx+1:]
}

// ensureCiscoExtension ensures the extensions map and osiris.cisco
// sub-map exist.
func ensureCiscoExtension(ext *map[string]any) {
	if *ext == nil {
		*ext = make(map[string]any)
	}
	if _, ok := (*ext)[extensionNamespace]; !ok {
		(*ext)[extensionNamespace] = make(map[string]any)
	}
}

// Helpers.
// str safely extracts a string value from an attribute map.
func str(attrs map[string]any, key string) string {
	if v, ok := attrs[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// resourceID builds a resource ID from the object's APIC distinguished
// name, following OSIRIS-JSON-v1.0 section 2.1.2's preferred
// "<provider>::<native-id>" construction.
// An APIC DN is fabric-wide unique and is already carried verbatim in
// provider.native_id, so no hashing is needed.
func resourceID(dn string) string {
	return providerName + "::" + dn
}

// groupIndex builds a map of group ID -> index in slice
// for efficient mutation.
func groupIndex(groups []sdk.Group) map[string]int {
	idx := make(map[string]int, len(groups))
	for i, g := range groups {
		idx[g.ID] = i
	}
	return idx
}

// indexByDNPrefix indexes a slice of attribute maps by the DN prefix
// (topology/pod-N/node-N), stripping any trailing path components.
func indexByDNPrefix(items []map[string]any) map[string]map[string]any {
	m := make(map[string]map[string]any, len(items))
	for _, item := range items {
		dn := str(item, "dn")
		prefix := dnPrefix(dn)
		if prefix != "" {
			m[prefix] = item
		}
	}
	return m
}

// dnPrefix extracts "topology/pod-N/node-N" from a longer DN.
func dnPrefix(dn string) string {
	// DN format: topology/pod-N/node-N[/...]
	parts := strings.SplitN(dn, "/", 4)
	if len(parts) >= 3 && strings.HasPrefix(parts[0], "topology") {
		return strings.Join(parts[:3], "/")
	}
	return dn
}

// extractTenantDN extracts the tenant DN (uni/tn-NAME) from a child DN.
func extractTenantDN(dn string) string {
	// DN format: uni/tn-NAME/...
	idx := strings.Index(dn, "/tn-")
	if idx < 0 {
		return ""
	}
	rest := dn[idx+1:] // "tn-NAME/..."
	if slashIdx := strings.Index(rest, "/"); slashIdx >= 0 {
		return dn[:idx+1+slashIdx]
	}
	return dn
}

// extractEPGDN extracts the EPG DN from an endpoint DN.
// Endpoint DN: uni/tn-NAME/ap-NAME/epg-NAME/cep-MAC
// EPG DN: uni/tn-NAME/ap-NAME/epg-NAME
func extractEPGDN(dn string) string {
	idx := strings.LastIndex(dn, "/cep-")
	if idx < 0 {
		return ""
	}
	return dn[:idx]
}
