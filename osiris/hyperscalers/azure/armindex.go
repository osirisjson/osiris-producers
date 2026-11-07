// armindex.go - ARM resource ID normalization for relationship
// resolution.
//
// Azure returns the same ARM resource ID with inconsistent casing
// depending on which API produced it: a VM lists itself as
// "/subscriptions/<id>/resourceGroups/RG-A/providers/Microsoft.Compute/..."
// while a managed disk's managedBy field may reference it as
// ".../resourcegroups/rg-a/providers/Microsoft.Compute/...".
// ARM resource IDs are case-insensitive by contract, so a relationship
// builder that matches them byte-for-byte silently drops valid edges.
//
// Every ARM ID -> OSIRIS JSON resource ID map in this package is keyed
// by [armKey] and read through [armLookup]. The original casing is
// never rewritten anywhere it is emitted: provider.native_id and the
// resource ID itself keep the value Azure returned for the resource, so
// normalization cannot cause resource ID churn between releases.

package azure

import "strings"

// armKey returns the canonical lookup key for an ARM resource ID:
// lower-cased with any trailing slash removed. It is used only as a map
// key and MUST NOT be emitted into an OSIRIS JSON document.
func armKey(armID string) string {
	return strings.ToLower(strings.TrimRight(armID, "/"))
}

// armLookup resolves an ARM resource ID reference against an ARM ID map
// ignoring case. An exact match is tried first so maps populated with
// raw keys keep working; otherwise the canonical [armKey] form is used.
func armLookup(m map[string]string, armID string) (string, bool) {
	if armID == "" {
		return "", false
	}
	if v, ok := m[armID]; ok {
		return v, true
	}
	v, ok := m[armKey(armID)]
	return v, ok
}
