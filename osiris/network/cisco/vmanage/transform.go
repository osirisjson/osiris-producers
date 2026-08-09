// transform.go - Pure vManage->OSIRIS JSON mapping functions.
// Converts Device records (from GET /dataservice/device) into OSIRIS
// SDK resource types. All functions are stateless: no I/O, no HTTP,
// just data transformation.
//
// OSIRIS JSON Producer for Cisco SD-WAN Manager (vManage) introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package vmanage

import (
	"maps"
	"strconv"
	"strings"
	"time"

	"go.osirisjson.org/producers/pkg/sdk"
)

// providerName identifies this producer's product line within the
// Cisco vendor namespace (dotted form).
const providerName = "cisco.vmanage"

// extensionKey namespaces every vManage-specific field placed under a
// resource or connection's extensions block (chapter 8 of OSIRIS-JSON
// specification: vendor-specific data MUST use the
// osiris.<namespace> prefix convention).
const extensionKey = "osiris.cisco.vmanage"

// personalityToType maps vManage device "personality" values to OSIRIS
// resource types.
//
// osiris/network/cisco/apic/transform.go already extends the taxonomy
// with "osiris.cisco.controller" for controller-role fabric nodes,
// since OSIRIS-JSON-v1.0 Appendix C.2 has no core network.controller
// type reused here (same osiris.cisco namespace) for the vManage
// control-plane personalities. WAN edges reuse the core network.router.
//
// This is a first pass for producer release 0.1.0 to be extended as new
// personality values are identified.
var personalityToType = map[string]string{
	"vmanage": "osiris.cisco.controller",
	"vsmart":  "osiris.cisco.controller",
	"vbond":   "osiris.cisco.controller",
	"vedge":   "network.router",
	"cedge":   "network.router",
}

// TransformDevices converts vManage Device records into OSIRIS
// resources. purpose is "documentation" (default) or "audit" per
// pkg/osirismeta audit adds fields that are stable but higher detail
// (serial_number, certificate validity). Geo-coordinates are not set
// here vManage reports the same latitude/longitude for every device
// at a site, so that data belongs on the site's physical.room group
// (properties.geo_location, OSIRIS-JSON-v1.0 section 6.5.1.1) since the
// device is installed in a room, rather han duplicated per device;
// see TransformSiteGroup.
// health/state_description/connected_manager/last_update/up_since are
// basic device state, not higher-detail data, so they are emitted
// regardless of purpose same tier as site_id/device_groups and the
// top-level state/status fields.
//
// Devices whose personality is not in personalityToType are skipped,
// following apic's nodeRoleToType precedent for unrecognized roles.
//
// id/provider.native_id use deviceNativeKey the hardware serial for
// network.router devices, unconditionally (identity fields are never
// purpose-gated), matching OSIRIS-JSON-v1.0 section 4.1.5 own
// on-premise-network-device example.
//
// provider.site (the resolved site display name) is deliberately not
// set here, OSIRIS-JSON-v1.0 section 7.5.2 own network.router example
// carries site placement under a top-level "location" object, which
// this producer doesn't emit yet (sdk.Resource has no Location field;
// adding one is a shared, cross-producer pkg/sdk change, deferred to a
// future roadmap). The site name is still available at the document
// level via metadata.scope.sites; the raw numeric site-id lives in
// extensions.osiris.cisco.vmanage.site_id.
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

		// manufacturer/model/version/management_ip match
		// OSIRIS-JSON-v1.0 specification 7.5.2 network.router "Common
		// properties" table, manufacturer is a constant since vManage
		// only manages Cisco/Viptela hardware; management_ip is
		// d.SystemIP, vManage's own management/control-plane address
		// for the device. No separate ip_addresses object it would
		// just duplicate management_ip with the same single value.
		props := map[string]any{"manufacturer": "Cisco"}
		if d.DeviceModel != "" {
			props["model"] = d.DeviceModel
		}
		if d.Version != "" {
			props["version"] = d.Version
		}
		if ip := sdk.NormalizeIP(d.SystemIP); ip != "" {
			props["management_ip"] = ip
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
		if len(d.DeviceGroups) > 0 {
			ext["device_groups"] = d.DeviceGroups
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
		if len(d.ConnectedVManages) > 0 {
			ext["connected_manager"] = d.ConnectedVManages
		}
		if lastUpdate := epochMillisToRFC3339(d.LastUpdated); lastUpdate != "" {
			ext["last_update"] = lastUpdate
		}
		if upSince := epochMillisToRFC3339(d.UptimeDate); upSince != "" {
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
// per-site document split in vmanage.go. Devices with no site-id are
// grouped under the "" key (see unsitedSegment in config.go).
func GroupDevicesBySiteID(devices []Device) map[string][]Device {
	groups := make(map[string][]Device)
	for _, d := range devices {
		groups[d.SiteID] = append(groups[d.SiteID], d)
	}
	return groups
}

// TransformSiteGroup builds the "physical.room" group for one site's
// document, with membership set to every device resource (router or
// controller) TransformDevices produced for that site. Interfaces are
// not added directly their "contains" connection to the owning
// device already implies site membership transitively.
//
// physical.room (OSIRIS-JSON-v1.0 section 7.6.5), not logical.site
// (section 6.2.3) or a coarser physical.* type: a WAN edge site is a
// real single physical location with its own coordinates the
// "location-based and facility-based" family 6.2.3 describes physical.*
// for, not the organizational/conceptual grouping logical.* is meant
// for (6.3.2's own logical.site example is "AWS us-east-1", an
// hyperscaler region spanning many facilities, not one place with one
// lat/long). Within physical.*, vManage WAN edges are network equipment
// installed in a rack that is not necessarily inside a datacenter or
// even a dedicated building often a network closet or equipment room in
// a small office or factory. 7.6.5's own physical.room definition names
// exactly that ("network closets", "equipment rooms"), a closer fit
// than physical.building/datacenter (which describe whole structures/
// campuses) or physical.rack which will fit perfectly but unfortunately
// vManage still reports no rack-level detail at all.
//
// This stays a lightweight group (membership + geo_location only), not
// the fuller physical.room resource shape 7.6.5's own example shows
// (location, room_number, room_sqft, room_type, cooling_capacity_tons)
// vManage's device data has none of that, and inventing placeholder
// values would misrepresent absent data as real.
//
// siteID is the raw numeric site-id ("" for the unclaimed fallback
// group, using unsitedSegment as the boundary token instead). siteName
// is the resolved display name, used as the group's Name when
// available. ok is false when there are no device resources to group
// (nothing to add to the document).
//
// devices is the site's device list, used only to source
// properties.geo_location: vManage reports the same latitude/longitude
// for every device at a site (confirmed against a real site's
// devices), so the first resolvable pair is used rather than repeating
// identical coordinates per device. geo_location is not purpose-gated
// a site's coordinates are not sensitive the way a serial number or
// certificate validity is, so it is emitted regardless of --purpose.
func TransformSiteGroup(siteID, siteName string, devices []Device, deviceResourceIDs []string) (group sdk.Group, ok bool) {
	if len(deviceResourceIDs) == 0 {
		return sdk.Group{}, false
	}

	boundaryToken := siteID
	if boundaryToken == "" {
		boundaryToken = unsitedSegment
	}
	gid := sdk.GroupID(sdk.GroupIDInput{Type: "physical.room", BoundaryToken: boundaryToken})

	g, err := sdk.NewGroup(gid, "physical.room")
	if err != nil {
		return sdk.Group{}, false
	}
	g.Name = siteName
	if g.Name == "" {
		g.Name = unsitedSegment
	}
	g.AddMembers(deviceResourceIDs...)

	for _, d := range devices {
		lat, latOK := toFloat(d.Latitude)
		lon, lonOK := toFloat(d.Longitude)
		if latOK && lonOK {
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

// mapDeviceStatus converts vManage status/reachability into an OSIRIS
// status enum value following OSIRIS JSON section 6.1.3 status field
// (active, inactive, degraded, retired, unknown).
// vManage's own status enum is {error, warning, normal, new}; this is
// a first pass, pending validation against a wider range of devices.
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

// epochMillisToRFC3339 converts a vManage epoch-milliseconds timestamp
// (e.g. "lastupdated", "uptime-date") to an RFC3339 UTC string, or ""
// for an unset (zero/negative) value.
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
// hardware serial, unlike "uuid", which vManage does not populate as
// an actual UUID for vEdge/cEdge platforms but as a
// "<model>-<chassis-serial>" string instead (e.g.
// "C8200L-1N-4T-S123456789L"). That string is kept under
// extensions.chassis_number instead (see TransformDevices) rather than
// used as the resource identity. Falls back to uuid, then deviceId,
// when no serial is available - the fallback path controller
// personalities (vmanage/vsmart/vbond) always take, since the serial
// substitution only applies to resType == "network.router".
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
// extensions[osiris.cisco.vmanage] map, preserving whatever else is
// already there (e.g. TransformDevices' own site_id/health fields)
// instead of replacing it used by the --include-raw-body attachment
// in vmanage.go (see wantRawBody) so raw-body keys never clobber
// fields a transform function already set, regardless of call order.
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
// vManage devices and interfaces always carry a stable native
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
// vManage's /dataservice/device/interface response encodes ip-address
// as host/prefix; returns "" for empty, "-" or "0.0.0.0" (both observed
// in responses as vManage's own placeholders for "no address
// configured" on an interface not a real address, and not private
// just because it fails an RFC 1918 check) or unparseable input.
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
