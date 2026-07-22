// transform.go - Shared utilities for the
// HPE Aruba Networking Central OSIRIS JSON producer.
// All functions are stateless: no I/O, just data transformation.
//
// Domain-specific transforms are organized in dedicated files:
//   transform_sites.go    - Site, DeviceGroup
//   transform_switches.go - Switch, SwitchInterface, SwitchVLAN,
// 							 SwitchLAG, SwitchStack, SwitchVSX
//   transform_networks.go - AccessPoint, Radio, BSSID, WLAN, Swarm
//   transform_topology.go - Gateway, GatewayUplink/VLAN/Port,
// 							 LLDP/CDP-like neighbor connections
//   transform_clients.go  - unified wired/wireless Client
//
// OSIRIS JSON Producer for HPE Aruba Networking Central introduction:
// [OSIRIS-JSON-HPE-ARUBA-NETWORKING]: https://docs.osirisjson.org/osiris-producers/network/hpe-aruba-networking-central
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package arubacentral

import (
	"strings"
	"time"

	"go.osirisjson.org/producers/pkg/sdk"
)

const (
	// providerName is the OSIRIS provider.name for every resource this
	// producer emits, dot-namespaced under "hpe." so Aruba Central,
	// Apstra or Juniper Mist - unrelated product lines that
	// happen to share an "hpe" CLI vendor namespace stay
	// distinguishable as separate providers if you run more than one
	// HPE producer in your environment.
	providerName = "hpe.arubacentral"

	// providerSource is the OSIRIS provider.source for every resource
	// this producer emits: a short identifier of the data source
	// path/method (OSIRIS JSON spec section 4.3.4), not the vendor
	// itself (that's providerName) which collection API surface
	// produced the data. "aruba-new-central-api" names the New Central
	// API specifically (https://developer.arubanetworks.com/new-central-config/reference),
	// as distinct from the older Aruba Central Classic API the two are
	// architecturally different surfaces this producer not talk to.
	providerSource = "aruba-new-central-api"

	// extensionNamespace is the osiris.* extension key used for
	// lossless, audit-only passthrough of raw API fields not otherwise
	// modelled. Dot-namespaced under "hpe." to match providerName.
	extensionNamespace = "osiris.hpe.arubacentral"
)

// resourceID builds a deterministic OSIRIS resource ID from a stable
// Aruba Central native identifier (a device serial number, or a
// composite key for sub-resources that are not globally unique on their
// own, e.g. "SERIAL/vlan/10"). Prefixed to match providerName so a
// resource's ID and its provider.name agree on which provider came from.
func resourceID(nativeID string) string {
	return providerName + "::" + nativeID
}

// provider builds a Provider for an Aruba Central resource.
func provider(nativeID, model, firmwareVersion, siteName string) sdk.Provider {
	return sdk.Provider{
		Name:     providerName,
		NativeID: nativeID,
		Type:     model,
		Version:  firmwareVersion,
		Site:     siteName,
		Source:   providerSource,
	}
}

// mapDeviceStatus translate the Aruba Central device "status" field
// ("Up", "Down", "Offline", etc., observed case-insensitively) to an
// OSIRIS JSON status enum value.
func mapDeviceStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "up", "online", "connected", "active":
		return "active"
	case "down", "offline", "disconnected":
		return "inactive"
	case "degraded", "fair", "poor":
		return "degraded"
	case "":
		return "unknown"
	default:
		return "unknown"
	}
}

// mapHealthStatus translate a hardware-category health string
// ("Good"/"Fair"/"Poor") to an OSIRIS JSON status enum value.
func mapHealthStatus(health string) string {
	switch strings.ToLower(strings.TrimSpace(health)) {
	case "good", "up":
		return "active"
	case "fair":
		return "degraded"
	case "poor", "down", "critical":
		return "degraded"
	case "":
		return "unknown"
	default:
		return "unknown"
	}
}

// mapConfigStatus translate a config-health "configStatus" value
// ("Up-to-date", "Out-of-sync", etc.) to an OSIRIS status enum value,
// used as a compliance/drift signal per the producer's drift-detection.
func mapConfigStatus(status string) string {
	lower := strings.ToLower(strings.TrimSpace(status))
	switch {
	case lower == "":
		return ""
	case strings.Contains(lower, "up-to-date") || strings.Contains(lower, "success"):
		return "active"
	case strings.Contains(lower, "fail") || strings.Contains(lower, "error"):
		return "degraded"
	case strings.Contains(lower, "sync") && strings.Contains(lower, "out"):
		return "degraded"
	default:
		return "unknown"
	}
}

// epochMillisToRFC3339 translate a Unix epoch-milliseconds timestamp to
// RFC3339 UTC. Returns "" for a zero/negative value.
func epochMillisToRFC3339(ms int64) string {
	if ms <= 0 {
		return ""
	}
	return sdk.NormalizeRFC3339UTC(time.UnixMilli(ms))
}

// setIfNotEmpty assigns props[key] = value when value is non-empty.
func setIfNotEmpty(props map[string]any, key, value string) {
	if value != "" {
		props[key] = value
	}
}

// setIfPositive assigns props[key] = value when value > 0.
func setIfPositive[T int | int64 | float64](props map[string]any, key string, value T) {
	if value > 0 {
		props[key] = value
	}
}

// indexResources builds a resourceID -> slice-index map so callers can
// mutate a resource in place (e.g. enrichment passes) after the initial
// transform, without rebuilding the slice.
func indexResources(resources []sdk.Resource) map[string]int {
	idx := make(map[string]int, len(resources))
	for i, r := range resources {
		idx[r.ID] = i
	}
	return idx
}

// dedupeConnections drops connections whose ID has already been seen.
// Connections derived from device-to-device adjacency
// (e.g. neighbor links) are naturally reported from both endpoints and
// produce the same deterministic bidirectional ID twice; the document
// builder rejects duplicate IDs outright, so
// callers must dedupe before Build().
func dedupeConnections(conns []sdk.Connection) []sdk.Connection {
	seen := make(map[string]bool, len(conns))
	out := make([]sdk.Connection, 0, len(conns))
	for _, c := range conns {
		if seen[c.ID] {
			continue
		}
		seen[c.ID] = true
		out = append(out, c)
	}
	return out
}

// dedupeResources drops resources whose ID has already been seen
// (e.g. the same neighbor stub reported by multiple local devices).
func dedupeResources(resources []sdk.Resource) []sdk.Resource {
	seen := make(map[string]bool, len(resources))
	out := make([]sdk.Resource, 0, len(resources))
	for _, r := range resources {
		if seen[r.ID] {
			continue
		}
		seen[r.ID] = true
		out = append(out, r)
	}
	return out
}
