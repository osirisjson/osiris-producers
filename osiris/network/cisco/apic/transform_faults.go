// transform_faults.go - APIC fault mapping. Groups non-cleared
// faultInst objects by DN prefix and attaches them to the matching node
// resource or tenant group under the osiris.cisco extension.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package apic

import (
	"strings"

	"go.osirisjson.org/producers/pkg/sdk"
)

// Fault extensions.
// Fault represents a curated APIC fault for extensions.
type Fault struct {
	Code           string `json:"code"`
	Severity       string `json:"severity"`
	Cause          string `json:"cause"`
	Description    string `json:"description"`
	Created        string `json:"created"`
	LastTransition string `json:"last_transition"`
	Lifecycle      string `json:"lifecycle"`
	Domain         string `json:"domain"`
	Subject        string `json:"subject"`
}

// TransformFaults groups non-cleared faults by DN prefix.
// Node faults are keyed by "topology/pod-N/node-N" (3 segments).
// Tenant faults are keyed by "uni/tn-NAME" (2 segments).
// Other faults are skipped (no resource/group to attach to).
func TransformFaults(faults []map[string]any) map[string][]Fault {
	result := make(map[string][]Fault)
	for _, f := range faults {
		if str(f, "severity") == "cleared" {
			continue
		}

		fault := Fault{
			Code:           str(f, "code"),
			Severity:       str(f, "severity"),
			Cause:          str(f, "cause"),
			Description:    str(f, "descr"),
			Created:        str(f, "created"),
			LastTransition: str(f, "lastTransition"),
			Lifecycle:      str(f, "lc"),
			Domain:         str(f, "domain"),
			Subject:        str(f, "subject"),
		}

		dn := str(f, "dn")
		prefix := faultDNPrefix(dn)
		if prefix == "" {
			continue
		}
		result[prefix] = append(result[prefix], fault)
	}
	return result
}

// WireFaultsToNodes attaches faults to node resources
// via their DN prefix.
// Mutates resources in-place, merging into
// extensions["osiris.cisco"]["faults"].
func WireFaultsToNodes(resources []sdk.Resource, faultsByDN map[string][]Fault) {
	for i := range resources {
		dn := resources[i].Provider.NativeID
		prefix := dnPrefix(dn)
		faults, ok := faultsByDN[prefix]
		if !ok || len(faults) == 0 {
			continue
		}
		ensureCiscoExtension(&resources[i].Extensions)
		resources[i].Extensions[extensionNamespace].(map[string]any)["faults"] = faults
	}
}

// WireFaultsToTenants attaches faults to tenant groups via their DN.
// Mutates groups in-place, setting extensions["osiris.cisco"]["faults"].
func WireFaultsToTenants(groups []sdk.Group, tenantDNToID map[string]string, faultsByDN map[string][]Fault) {
	// Build reverse map: group ID -> index.
	idx := groupIndex(groups)

	for dn, gid := range tenantDNToID {
		faults, ok := faultsByDN[dn]
		if !ok || len(faults) == 0 {
			continue
		}
		i, ok := idx[gid]
		if !ok {
			continue
		}
		ensureCiscoExtension(&groups[i].Extensions)
		groups[i].Extensions[extensionNamespace].(map[string]any)["faults"] = faults
	}
}

// faultDNPrefix extracts the relevant parent prefix from a fault DN.
// Node faults: "topology/pod-N/node-N" from longer topology DNs.
// Tenant faults: "uni/tn-NAME" from uni/tn-* DNs.
// Returns empty string for unrecognized patterns.
func faultDNPrefix(dn string) string {
	if strings.HasPrefix(dn, "topology/") {
		// Extract topology/pod-N/node-N (first 3 segments).
		parts := strings.SplitN(dn, "/", 4)
		if len(parts) >= 3 && strings.HasPrefix(parts[1], "pod-") && strings.HasPrefix(parts[2], "node-") {
			return strings.Join(parts[:3], "/")
		}
		return ""
	}
	if strings.HasPrefix(dn, "uni/tn-") {
		// Extract uni/tn-NAME (first 2 segments).
		return extractTenantDN(dn)
	}
	return ""
}
