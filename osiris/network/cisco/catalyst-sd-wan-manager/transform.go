// transform.go - Pure SD-WAN Manager->OSIRIS JSON mapping functions.
// Converts Device records (from GET /dataservice/device) into OSIRIS
// SDK resource types. All functions are stateless: no I/O, no HTTP,
// just data transformation.
//
// OSIRIS JSON Producer for Cisco Catalyst SD-WAN Manager introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package catalystsdwanmanager

import (
	"maps"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.osirisjson.org/producers/pkg/sdk"
)

// providerName identifies this producer's product line within the
// Cisco vendor namespace (dotted form).
const providerName = "cisco.catalystsdwanmanager"

// extensionKey namespaces every SD-WAN Manager-specific field placed
// under a resource or connection's extensions block (chapter 8 of
// OSIRIS JSON specification: vendor-specific data MUST use the
// osiris.<namespace> prefix convention).
const extensionKey = "osiris.cisco.catalystsdwanmanager"

// Device personality values as the Manager API reports them. They
// keep the component names from before the Catalyst rebranding and
// must match the API exactly, so they are not renamed. The same API
// values pass through unchanged into provider.type and
// properties.model (from device-model).
const (
	personalityManager    = "vmanage" // Catalyst SD-WAN Manager
	personalityController = "vsmart"  // Catalyst SD-WAN Controller
	personalityValidator  = "vbond"   // Catalyst SD-WAN Validator
	personalityVEdge      = "vedge"   // WAN edge (vEdge platform)
	personalityCEdge      = "cedge"   // WAN edge (IOS-XE platform)
)

// personalityToType maps SD-WAN Manager device personalities to
// OSIRIS JSON resource types.
//
// OSIRIS JSON v1.0 Appendix C.2 has still no core controller type, so
// the three control-plane components use the namespaced
// "osiris.cisco.controller"; WAN edges use the core network.router.
// Unknown personalities are skipped until mapped.
var personalityToType = map[string]string{
	personalityManager:    "osiris.cisco.controller",
	personalityController: "osiris.cisco.controller",
	personalityValidator:  "osiris.cisco.controller",
	personalityVEdge:      "network.router",
	personalityCEdge:      "network.router",
}

// TransformDevices converts SD-WAN Manager Device records into OSIRIS
// resources. purpose is "documentation" (default) or "audit" per
// pkg/osirismeta audit adds fields that are stable but higher detail
// (serial_number, certificate validity); purpose.go then applies the
// documentation allowlist. Geo-coordinates are not set here: SD-WAN
// Manager reports the same latitude/longitude for every device at a
// site, so they belong on the site group (see TransformSiteGroup).
// health/state_description/connected_manager/last_update/up_since are
// emitted here for both purposes; purpose.go keeps only health (and
// site_id) in documentation.
//
// Devices whose personality is not in personalityToType are skipped,
// following apic's nodeRoleToType precedent for unrecognized roles.
//
// id/provider.native_id use deviceNativeKey the hardware serial for
// network.router devices, unconditionally (identity fields are never
// purpose-gated), matching OSIRIS-JSON-v1.0 section 4.1.5 own
// on-premise-network-device example.
//
// provider.site (the raw site-id) and provider.system (the Manager the
// device was collected from) are set by the document stage (see
// setProvenance in document.go), which knows the run's Manager.
//
// Returns the resources plus a system-ip -> resource ID index, used by
// the caller to wire interfaces (transform_interfaces.go) and
// same-document connections (transform_connections.go) back to their
// owning device.
func TransformDevices(devices []Device, purpose string) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	systemIPToID := make(map[string]string, len(devices))

	for _, d := range devices {
		resType, ok := personalityToType[d.Personality]
		if !ok {
			continue
		}

		key := deviceNativeKey(d, resType)
		if key == "" {
			continue // no native identity: never emit an empty ID (see unidentifiedDevices).
		}
		id := resourceID(key)

		prov := sdk.Provider{
			Name:     providerName,
			NativeID: key,
			Type:     d.DeviceModel,
		}

		r, err := sdk.NewResource(id, resType, prov)
		if err != nil {
			continue
		}
		r.Name = d.HostName
		r.Status = mapDeviceStatus(d.Status, d.Reachability)
		if d.Reachability != "" {
			r.State = d.Reachability
		}

		// manufacturer/model/version match OSIRIS JSON v1.0
		// specification 7.5.2 network.router "Common properties" table;
		// manufacturer is a constant since SD-WAN Manager only manages
		// Cisco/Viptela hardware. The system IP is the device's SD-WAN
		// overlay identifier, not a proven management address, so it is
		// kept as extension system_ip rather than management_ip.
		props := map[string]any{"manufacturer": "Cisco"}
		if d.DeviceModel != "" {
			props["model"] = d.DeviceModel
		}
		if d.Version != "" {
			props["version"] = d.Version
		}
		if purpose == "audit" {
			if d.BoardSerial != "" {
				props["serial_number"] = d.BoardSerial
			}
		}
		r.Properties = props

		ext := map[string]any{}
		if d.SiteID != "" {
			ext["site_id"] = d.SiteID
		}
		if ip := sdk.NormalizeIP(d.SystemIP); ip != "" {
			ext["system_ip"] = ip
		}
		if len(d.DeviceGroups) > 0 {
			ext["device_groups"] = sortedCopy(d.DeviceGroups)
		}
		if resType == "network.router" && d.BoardSerial != "" && d.UUID != "" {
			ext["chassis_number"] = d.UUID
		}
		if d.HealthState != "" {
			ext["health"] = d.HealthState
		}
		if d.StateDescription != "" {
			ext["state_description"] = d.StateDescription
		}
		if len(d.ConnectedManagers) > 0 {
			ext["connected_manager"] = sortedCopy(d.ConnectedManagers)
		}
		if lastUpdate := epochMillisToRFC3339(int64(d.LastUpdated)); lastUpdate != "" {
			ext["last_update"] = lastUpdate
		}
		if upSince := epochMillisToRFC3339(int64(d.UptimeDate)); upSince != "" {
			ext["up_since"] = upSince
		}
		if purpose == "audit" {
			if d.CertificateValidity != "" {
				ext["certificate_validity"] = d.CertificateValidity
			}
		}
		if len(ext) > 0 {
			r.Extensions = map[string]any{extensionKey: ext}
		}

		resources = append(resources, r)
		if d.SystemIP != "" {
			systemIPToID[d.SystemIP] = id
		}
	}

	return resources, systemIPToID
}

// GroupDevicesBySiteID partitions devices by their site-id, for the
// per-site document split in catalystsdwanmanager.go. Devices with no
// site-id are grouped under the "" key (see unsitedSegment in
// config.go).
func GroupDevicesBySiteID(devices []Device) map[string][]Device {
	groups := make(map[string][]Device)
	for _, d := range devices {
		groups[d.SiteID] = append(groups[d.SiteID], d)
	}
	return groups
}

// TransformSiteGroup builds the "logical.site" group of one site's
// document. Its members are the site's own resources, devices and
// their interfaces, listed explicitly (OSIRIS JSON section 6.3); a
// neighbor device of another site that only appears to keep a
// cross-site relationship valid is never a member.
//
// logical.site, not physical.room: an SD-WAN site is the Manager's
// logical grouping of devices by site-id, and the API carries no
// evidence of a room, building or rack. logical.site is the type the
// specification's own section 6.3.2 example uses. The unsited bucket
// (devices with no site-id) is a logical.site named unsitedSegment and
// marked unsited, an operational grouping rather than a place.
//
// The group's ID is derived from the raw site-id (unsitedSegment for
// the unsited bucket), never from the display name, so a renamed site
// keeps its group ID. The raw site-id is also kept as extension
// site_id. properties.geo_location takes the first parsable device
// coordinate pair; it is audit-only (see purpose.go). ok is false when
// there is nothing to group.
func TransformSiteGroup(siteID, siteName string, devices []Device, memberIDs []string) (group sdk.Group, ok bool) {
	if len(memberIDs) == 0 {
		return sdk.Group{}, false
	}

	boundaryToken := siteID
	if boundaryToken == "" {
		boundaryToken = unsitedSegment
	}
	gid := sdk.GroupID(sdk.GroupIDInput{Type: "logical.site", BoundaryToken: boundaryToken})

	g, err := sdk.NewGroup(gid, "logical.site")
	if err != nil {
		return sdk.Group{}, false
	}
	g.Name = siteName
	if g.Name == "" {
		g.Name = unsitedSegment
	}
	g.AddMembers(memberIDs...)
	if siteID != "" {
		g.Extensions = map[string]any{extensionKey: map[string]any{"site_id": siteID}}
	} else {
		g.Extensions = map[string]any{extensionKey: map[string]any{"unsited": true}}
	}

	for _, d := range devices {
		lat, latOK := toFloat(d.Latitude)
		lon, lonOK := toFloat(d.Longitude)
		if latOK && lonOK && lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180 {
			g.Properties = map[string]any{
				"geo_location": map[string]any{
					"latitude":  lat,
					"longitude": lon,
				},
			}
			break
		}
	}

	return g, true
}

// sortedCopy returns a sorted copy of an unordered API string list, so
// output does not depend on API order.
func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

// mapDeviceStatus converts SD-WAN Manager status/reachability into an
// OSIRIS status enum value following OSIRIS JSON section 6.1.3 status
// field (active, inactive, degraded, retired, unknown). SD-WAN Manager
// own status enum is {error, warning, normal, new}; this is a
// first pass, pending validation against a wider range of devices.
func mapDeviceStatus(status, reachability string) string {
	if reachability == "unreachable" {
		return "inactive"
	}
	switch status {
	case "normal":
		return "active"
	case "error", "warning":
		return "degraded"
	case "new":
		return "unknown"
	default:
		return "unknown"
	}
}

// epochMillisToRFC3339 converts an SD-WAN Manager epoch-milliseconds
// timestamp (e.g. "lastupdated", "uptime-date") to an RFC3339 UTC
// string, or "" for an unset (zero/negative) value.
func epochMillisToRFC3339(ms int64) string {
	if ms <= 0 {
		return ""
	}
	return sdk.NormalizeRFC3339UTC(time.UnixMilli(ms))
}

// toFloat converts a decoded JSON value (string or float64) to a
// float64, handling the spec's own documented example mixing a string
// latitude with a numeric longitude for the same device object.
func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case string:
		f, err := strconv.ParseFloat(n, 64)
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}

// deviceNativeKey picks the native-ID key for a device resource:
// board-serial for network.router devices, when available a real
// hardware serial, unlike "uuid", which SD-WAN Manager does not
// populate as an actual UUID for vEdge/cEdge platforms but as a
// "<model>-<chassis-serial>" string instead (e.g.
// "C8200L-1N-4T-TST0000001"). That string is kept under
// extensions.chassis_number instead (see TransformDevices) rather than
// used as the resource identity. Falls back to uuid, then deviceId,
// when no serial is available the fallback path the Manager,
// Controller and Validator personalities always take, since the
// serial substitution only applies to resType == "network.router".
func deviceNativeKey(d Device, resType string) string {
	if resType == "network.router" && d.BoardSerial != "" {
		return d.BoardSerial
	}
	if d.UUID != "" {
		return d.UUID
	}
	return d.DeviceID
}

// setExtension merges a single key into a resource's
// extensions[osiris.cisco.catalystsdwanmanager] map, preserving
// whatever else is already there (e.g. TransformDevices' own
// site_id/health fields) instead of replacing it used by the
// --include-raw-body attachment in catalystsdwanmanager.go (see
// wantRawBody) so raw-body keys never clobber fields a transform
// function already set, regardless of call order.
func setExtension(r *sdk.Resource, key string, value any) {
	if r.Extensions == nil {
		r.Extensions = map[string]any{}
	}
	ext, _ := r.Extensions[extensionKey].(map[string]any)
	merged := make(map[string]any, len(ext)+1)
	maps.Copy(merged, ext)
	merged[key] = value
	r.Extensions[extensionKey] = merged
}

// resourceID builds a namespaced native-ID resource identifier
// OSIRIS-JSON-v1.0 section 2.1.2, strategy 2: "Namespaced native
// ID <provider>::<id>"), preferred here over a deterministic hash since
// SD-WAN Manager devices and interfaces always carry a stable native
// identifier (device serial/uuid/deviceId, see deviceNativeKey, or a
// device-key + interface-name composite for sub-resources see
// resourceKey).
func resourceID(nativeID string) string {
	return providerName + "::" + nativeID
}

// resourceKey builds the composite native ID for a device sub-resource.
func resourceKey(deviceKey, name string) string {
	return deviceKey + "-" + name
}

// stripCIDRHost returns the host portion of a CIDR-form address
// ("192.0.2.20/24" -> "192.0.2.20"), normalized via sdk.NormalizeIP.
// SD-WAN Manager's /dataservice/device/interface response encodes
// ip-address as host/prefix; returns "" for empty, "-" or "0.0.0.0"
// (both observed in responses as SD-WAN Manager's own placeholders for
// "no address configured" on an interface not a real address, and not
// private just because it fails an RFC 1918 check) or unparseable
// input.
func stripCIDRHost(s string) string {
	if s == "" || s == "-" || s == "0.0.0.0" {
		return ""
	}
	if idx := strings.IndexByte(s, '/'); idx >= 0 {
		s = s[:idx]
	}
	if s == "0.0.0.0" {
		return ""
	}
	return sdk.NormalizeIP(s)
}
