// transform_clients.go - Unified wired/wireless client transforms.
//
// OSIRIS JSON Producer for HPE Aruba Networking Central introduction:
// [OSIRIS-JSON-HPE-ARUBA-NETWORKING]: https://docs.osirisjson.org/osiris-producers/network/hpe-aruba-networking
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package arubacentral

import (
	"fmt"

	"go.osirisjson.org/producers/pkg/sdk"
)

// TransformClients converts unified (wired + wireless) clients into
// "osiris.hpe.arubacentral.client" resources, plus "network"
// connections to the device they are connected through (resolved via
// deviceIDMap, keyed by serial) when known.
func TransformClients(clients []ClientDevice, deviceIDMap map[string]string, purpose string) ([]sdk.Resource, []sdk.Connection) {
	var resources []sdk.Resource
	var connections []sdk.Connection

	for _, cl := range clients {
		mac := sdk.NormalizeMAC(cl.MACAddress)
		if mac == "" {
			continue
		}
		id := resourceID(fmt.Sprintf("client/%s", mac))

		prov := sdk.Provider{Name: providerName, NativeID: mac, Source: providerSource, Site: cl.SiteName}
		r, err := sdk.NewResource(id, "osiris.hpe.arubacentral.client", prov)
		if err != nil {
			continue
		}
		r.Name = firstNonEmpty(cl.ClientName, cl.HostName, mac)
		r.Status = mapDeviceStatus(cl.Status)

		props := map[string]any{"mac_address": mac}
		setIfNotEmpty(props, "client_category", cl.ClientCategory)
		setIfNotEmpty(props, "client_connection_type", cl.ClientConnectionType)
		setIfNotEmpty(props, "site_id", cl.SiteID)
		setIfNotEmpty(props, "vlan_id", cl.VLANID)
		setIfNotEmpty(props, "vlan_name", cl.VLANName)
		setIfNotEmpty(props, "wlan_name", cl.WLANName)
		setIfNotEmpty(props, "wireless_band", cl.WirelessBand)
		setIfNotEmpty(props, "ipv4", sdk.NormalizeIP(cl.IPv4))
		setIfNotEmpty(props, "ipv6", sdk.NormalizeIP(cl.IPv6))
		if purpose == "audit" {
			setIfNotEmpty(props, "host_name", cl.HostName)
			setIfNotEmpty(props, "user_name", cl.UserName)
			setIfNotEmpty(props, "client_function", cl.ClientFunction)
			setIfNotEmpty(props, "client_manufacturer", cl.ClientManufacturer)
			setIfNotEmpty(props, "client_operating_system", cl.ClientOperatingSystem)
			setIfNotEmpty(props, "security_type", cl.AuthenticationType)
			setIfNotEmpty(props, "connected_at", cl.ConnectedAt)
		}
		r.Properties = props

		resources = append(resources, r)

		if deviceID, ok := deviceIDMap[cl.ConnectedDeviceSerial]; ok {
			canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
				Type:       "network",
				Direction:  "forward",
				Source:     id,
				Target:     deviceID,
				Qualifiers: map[string]string{"port": cl.Port},
			})
			connID := sdk.BuildConnectionID(canonicalKey, 16)
			conn, err := sdk.NewConnection(connID, "network", id, deviceID)
			if err == nil {
				conn.Name = fmt.Sprintf("%s connected via %s", r.Name, cl.Port)
				conn.Status = mapDeviceStatus(cl.Status)
				_ = conn.SetDirection("forward")
				if cl.Port != "" {
					conn.Properties = map[string]any{"port": cl.Port}
				}
				connections = append(connections, conn)
			}
		}
	}

	return resources, connections
}

// firstNonEmpty returns the first non-empty string argument, or "" if
// all are empty.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
