// transform_sites.go - Site and device-group transforms.
//
// OSIRIS JSON Producer for HPE Aruba Networking Central introduction:
// [OSIRIS-JSON-HPE-ARUBA-NETWORKING]: https://docs.osirisjson.org/osiris-producers/network/hpe-aruba-networking
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package arubacentral

import (
	"fmt"

	"go.osirisjson.org/producers/pkg/sdk"
)

// TransformSites converts Aruba Central sites into OSIRIS
// "network.site" resources. Returns the resources and a
// siteName -> resourceID map used to wire devices back to their site.
func TransformSites(sites []Site) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	nameToID := make(map[string]string, len(sites))

	for _, s := range sites {
		nativeID := s.ScopeID
		if nativeID == "" {
			nativeID = s.ID
		}
		if nativeID == "" {
			continue
		}
		id := resourceID(fmt.Sprintf("site/%s", nativeID))

		prov := sdk.Provider{
			Name:     providerName,
			NativeID: nativeID,
			Source:   providerSource,
			Site:     s.ScopeName,
		}

		r, err := sdk.NewResource(id, "network.site", prov)
		if err != nil {
			continue
		}
		r.Name = s.ScopeName
		r.Status = "active"

		props := map[string]any{}
		setIfNotEmpty(props, "address", s.Address)
		setIfNotEmpty(props, "city", s.City)
		setIfNotEmpty(props, "state", s.State)
		setIfNotEmpty(props, "country", s.Country)
		setIfNotEmpty(props, "zipcode", s.Zipcode)
		if s.Latitude != 0 {
			props["latitude"] = s.Latitude
		}
		if s.Longitude != 0 {
			props["longitude"] = s.Longitude
		}
		setIfPositive(props, "device_count", s.DeviceCount)
		setIfNotEmpty(props, "timezone_id", s.Timezone.TimezoneID)
		setIfNotEmpty(props, "timezone_name", s.Timezone.TimezoneName)
		r.Properties = props

		resources = append(resources, r)
		if s.ScopeName != "" {
			nameToID[s.ScopeName] = id
		}
	}

	return resources, nameToID
}

// EnrichSiteHealth folds a site's health overview onto its network.site
// resource: overall health as a drift-detection signal (documentation),
// plus device/client health breakdowns (audit only), mirroring the
// config-health enrichment pattern used for devices.
func EnrichSiteHealth(r *sdk.Resource, health SiteHealth, purpose string) {
	if r.Properties == nil {
		r.Properties = map[string]any{}
	}
	setIfNotEmpty(r.Properties, "health_status", health.SiteHealth)
	if mapHealthStatus(health.SiteHealth) == "degraded" && r.Status == "active" {
		r.Status = "degraded"
	}
	if purpose == "audit" {
		setIfNotEmpty(r.Properties, "device_health", health.DeviceHealth)
		setIfNotEmpty(r.Properties, "client_health", health.ClientHealth)
	}
}

// EnrichSiteDeviceHealth folds a site's per-device-category health
// breakdown onto its network.site resource. Audit-only: like
// device_health/client_health on EnrichSiteHealth, is more granular
// detail than the documentation purpose's minimal health signal needs.
func EnrichSiteDeviceHealth(r *sdk.Resource, health SiteDeviceHealth, purpose string) {
	if purpose != "audit" {
		return
	}
	if r.Properties == nil {
		r.Properties = map[string]any{}
	}
	setIfNotEmpty(r.Properties, "ap_health", health.APHealth)
	setIfNotEmpty(r.Properties, "switch_health", health.SwitchHealth)
	setIfNotEmpty(r.Properties, "gateway_health", health.GatewayHealth)
	setIfNotEmpty(r.Properties, "bridge_health", health.BridgeHealth)
}

// TransformDeviceGroups convert Aruba Central device groups into OSIRIS
// "logical.devicegroup" groups. Returns the groups and a group name ->
// group ID map used by device transforms to add themselves as members.
func TransformDeviceGroups(groups []DeviceGroup) ([]sdk.Group, map[string]string) {
	var result []sdk.Group
	nameToID := make(map[string]string, len(groups))

	for _, g := range groups {
		boundaryToken := g.ID
		if boundaryToken == "" {
			boundaryToken = g.ScopeID
		}
		if boundaryToken == "" {
			continue
		}
		gid := sdk.GroupID(sdk.GroupIDInput{
			Type:          "logical.devicegroup",
			BoundaryToken: boundaryToken,
		})

		group, err := sdk.NewGroup(gid, "logical.devicegroup")
		if err != nil {
			continue
		}
		group.Name = g.ID
		group.Description = g.Description

		props := map[string]any{}
		setIfPositive(props, "device_count", g.DeviceCount)
		setIfNotEmpty(props, "device_type", g.Type)
		if g.IsIap8x {
			props["is_iap8x"] = true
		}
		group.Properties = props

		result = append(result, group)
		if g.ID != "" {
			nameToID[g.ID] = gid
		}
	}

	return result, nameToID
}

// groupIndex builds a name/id -> slice-index map so callers can mutate
// group members/children in place after the initial transform
// (groups are stored by value in a slice; mutation must go through
// the index, not a copy).
func groupIndex(groups []sdk.Group) map[string]int {
	idx := make(map[string]int, len(groups))
	for i, g := range groups {
		idx[g.ID] = i
	}
	return idx
}

// WireDeviceToGroup adds a device resource as a member of its named
// device group, when both the group and the device group name are known.
func WireDeviceToGroup(groups []sdk.Group, idx map[string]int, nameToGroupID map[string]string, groupName string, deviceResourceID string) {
	if groupName == "" || deviceResourceID == "" {
		return
	}
	gid, ok := nameToGroupID[groupName]
	if !ok {
		return
	}
	i, ok := idx[gid]
	if !ok {
		return
	}
	groups[i].AddMembers(deviceResourceID)
}
