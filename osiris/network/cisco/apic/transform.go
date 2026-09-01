// transform.go - Shared helpers for the APIC->OSIRIS mapping. The
// per-domain transforms live in transform_fabric.go,
// transform_tenant.go, transform_endpoints.go, transform_topology.go,
// transform_faults.go and wire.go; this file holds only the constants
// and stateless DN/identity/extension helpers they share.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package apic

import (
	"regexp"
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

// nodeNumRe matches the numeric node id in a topology DN
// ("topology/pod-1/node-101" -> "101").
var nodeNumRe = regexp.MustCompile(`/node-(\d+)`)

// nodeNumFromDN returns the bare fabric node number embedded in a
// topology DN, or "" when the DN carries none.
func nodeNumFromDN(dn string) string {
	m := nodeNumRe.FindStringSubmatch(dn)
	if m == nil {
		return ""
	}
	return m[1]
}

// physIfDN builds the l1PhysIf DN for a port on a node
// ("topology/pod-1/node-101" + "eth1/10" ->
// "topology/pod-1/node-101/sys/phys-[eth1/10]").
func physIfDN(nodeDN, portID string) string {
	return nodeDN + "/sys/phys-[" + portID + "]"
}

// aggrIfDN builds the pcAggrIf DN for a port-channel on a node
// ("topology/pod-1/node-111" + "po1" ->
// "topology/pod-1/node-111/sys/aggr-[po1]").
func aggrIfDN(nodeDN, poID string) string {
	return nodeDN + "/sys/aggr-[" + poID + "]"
}

// extractIfToken pulls the interface id out of an "if-[<id>]" segment
// of an LLDP/CDP/LACP child DN
// (".../sys/lldp/inst/if-[eth1/10]/adj-1" -> "eth1/10"). The interface
// id never contains a bracket, so the first "]" after "if-[" closes it.
func extractIfToken(dn string) string {
	i := strings.Index(dn, "if-[")
	if i < 0 {
		return ""
	}
	rest := dn[i+len("if-["):]
	j := strings.IndexByte(rest, ']')
	if j < 0 {
		return ""
	}
	return rest[:j]
}

// normalizeIfID lowercases the vendor "Eth1/53"/"Ethernet1/43" port
// spellings LLDP and CDP report for a peer into the "eth1/53" form APIC
// uses in its own DNs. A value that is not an Ethernet port id (a
// management name, a description) is returned unchanged.
func normalizeIfID(s string) string {
	switch {
	case strings.HasPrefix(s, "Ethernet"):
		return "eth" + s[len("Ethernet"):]
	case strings.HasPrefix(s, "Eth"):
		return "eth" + s[len("Eth"):]
	default:
		return s
	}
}

// pathTargetDN resolves an APIC path target (an fvRsPathAtt.tDn or an
// fvCEp.fabricPathDn) to the DN of the resource it points at.
// It returns the l1PhysIf DN for a single-port path, the pcAggrIf DN
// for a port-channel path, and ("", "vpc") for a vPC (protpaths) path,
// which spans two nodes and has no single owning resource. numToDN maps
// a bare node number to that node's DN.
//
//	topology/pod-1/paths-121/pathep-[eth1/10]      -> <node-121 DN>/sys/phys-[eth1/10], "port"
//	topology/pod-1/paths-121/pathep-[po5]          -> <node-121 DN>/sys/aggr-[po5],     "portchannel"
//	topology/pod-1/protpaths-121-122/pathep-[vpc1] -> "", "vpc"
func pathTargetDN(pathDN string, numToDN map[string]string) (dn, kind string) {
	tok := extractBracketToken(pathDN, "pathep-[")
	if tok == "" {
		return "", ""
	}
	if strings.Contains(pathDN, "/protpaths-") {
		return "", "vpc"
	}
	i := strings.Index(pathDN, "/paths-")
	if i < 0 {
		return "", ""
	}
	rest := pathDN[i+len("/paths-"):]
	num := rest
	if j := strings.IndexByte(rest, '/'); j >= 0 {
		num = rest[:j]
	}
	nodeDN, ok := numToDN[num]
	if !ok {
		return "", ""
	}
	if strings.HasPrefix(tok, "po") {
		return aggrIfDN(nodeDN, tok), "portchannel"
	}
	return physIfDN(nodeDN, tok), "port"
}

// extractBracketToken returns the text between "<prefix>" and the next
// "]". Path targets never nest a bracket inside pathep-[...], so a
// plain scan is safe here (unlike a full fvRsPathAtt DN).
func extractBracketToken(s, prefix string) string {
	i := strings.Index(s, prefix)
	if i < 0 {
		return ""
	}
	rest := s[i+len(prefix):]
	j := strings.IndexByte(rest, ']')
	if j < 0 {
		return ""
	}
	return rest[:j]
}
