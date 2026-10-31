// transform.go - Pure YANG->OSIRIS mapping functions for Cisco IOS-XE.
// Converts decoded YANG reply DTOs (see dto.go) into SDK types.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package iosxe

import "go.osirisjson.org/producers/pkg/sdk"

const extensionNamespace = "osiris.cisco"

// Helper functions
// groupIndex builds a map of group ID -> index in slice for
// efficient mutation.
func groupIndex(groups []sdk.Group) map[string]int {
	idx := make(map[string]int, len(groups))
	for i, g := range groups {
		idx[g.ID] = i
	}
	return idx
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
