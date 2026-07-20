// transform_networks.go - Access point, radio, BSSID, WLAN
// and swarm transforms. These resource/group types have no standard
// OSIRIS JSON equivalent (OSIRIS JSON core schema's resource type
// taxonomy defines no wireless family in v1.0), so they are emitted
// under the osiris.hpe.arubacentral.* vendor-namespace per the
// taxonomy's custom-type fallback rule, matching the pattern already
// used for VSX peering in transform_switches.go.
//
// OSIRIS JSON Producer for HPE Aruba Networking Central introduction:
// [OSIRIS-JSON-HPE-ARUBA-NETWORKING]: https://docs.osirisjson.org/osiris-producers/network/hpe-aruba-networking
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package arubacentral

import (
	"fmt"

	"go.osirisjson.org/producers/pkg/sdk"
)

// TransformAccessPoints converts Aruba Central access points into
// OSIRIS osiris.hpe.arubacentral.accesspoint resources. Returns the
// resources and a serial -> resourceID map used to wire sub-resources
// (ports, tunnels, radios, WLANs).
func TransformAccessPoints(aps []AccessPoint) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(aps))

	for _, ap := range aps {
		if ap.SerialNumber == "" {
			continue
		}
		id := resourceID(ap.SerialNumber)
		idMap[ap.SerialNumber] = id

		prov := provider(ap.SerialNumber, ap.Model, ap.FirmwareVersion, ap.SiteName)
		r, err := sdk.NewResource(id, "osiris.hpe.arubacentral.accesspoint", prov)
		if err != nil {
			continue
		}
		r.Name = ap.DeviceName
		r.Status = mapDeviceStatus(ap.Status)
		r.State = ap.Status

		props := map[string]any{}
		setIfNotEmpty(props, "site_id", ap.SiteID)
		setIfNotEmpty(props, "ipv4", sdk.NormalizeIP(ap.IPv4))
		setIfNotEmpty(props, "ipv6", sdk.NormalizeIP(ap.IPv6))
		setIfNotEmpty(props, "public_ipv4", sdk.NormalizeIP(ap.PublicIPv4))
		setIfNotEmpty(props, "mac_address", sdk.NormalizeMAC(ap.MACAddress))
		setIfNotEmpty(props, "part_number", ap.PartNumber)
		setIfNotEmpty(props, "deployment", ap.Deployment)
		setIfNotEmpty(props, "device_function", ap.DeviceFunction)
		setIfNotEmpty(props, "role", ap.Role)
		setIfNotEmpty(props, "mesh_role", ap.MeshRole)
		setIfNotEmpty(props, "cluster_name", ap.ClusterName)
		setIfNotEmpty(props, "device_group_name", ap.DeviceGroupName)
		setIfPositive(props, "client_count", ap.ClientCount)
		setIfPositive(props, "wlan_count", ap.WLANCount)
		setIfPositive(props, "cpu_utilization_pct", ap.CPUUtilization)
		setIfPositive(props, "memory_utilization_pct", ap.MemoryUtilization)
		if ap.PowerConsumption > 0 {
			props["power_consumption_watts"] = ap.PowerConsumption
		}
		if ap.UptimeInMillis > 0 {
			props["uptime_seconds"] = ap.UptimeInMillis / 1000
		}
		r.Properties = props

		resources = append(resources, r)
	}

	return resources, idMap
}

// TransformAPPorts converts an AP's wired ports into "network.interface"
// resources plus "contains" connections from the AP.
func TransformAPPorts(apID, serial string, ports []APPort) ([]sdk.Resource, []sdk.Connection) {
	var resources []sdk.Resource
	var connections []sdk.Connection

	for _, p := range ports {
		name := p.Name
		if name == "" {
			continue
		}
		id := resourceID(fmt.Sprintf("%s/port/%s", serial, name))

		prov := sdk.Provider{Name: providerName, NativeID: serial, Source: providerSource}
		r, err := sdk.NewResource(id, "network.interface", prov)
		if err != nil {
			continue
		}
		r.Name = name
		r.Status = mapDeviceStatus(p.Status)

		props := map[string]any{}
		setIfNotEmpty(props, "connector", p.Connector)
		setIfNotEmpty(props, "duplex", p.Duplex)
		setIfNotEmpty(props, "speed", p.Speed)
		setIfNotEmpty(props, "mac_address", sdk.NormalizeMAC(p.MACAddress))
		setIfNotEmpty(props, "native_vlan", p.NativeVlan)
		setIfNotEmpty(props, "allowed_vlan", p.AllowedVlan)
		setIfNotEmpty(props, "access_vlan", p.AccessVlan)
		setIfNotEmpty(props, "vlan_mode", p.VlanMode)
		r.Properties = props

		resources = append(resources, r)

		connections = append(connections, containsConnection(apID, id, fmt.Sprintf("%s -> %s", serial, name)))
	}

	return resources, connections
}

// TransformAPTunnels converts an AP's tunnels into
// osiris.hpe.arubacentral.tunnel connections to a stub destination
// resource (the tunnel's remote endpoint, typically a mobility
// conductor or gateway not otherwise collected).
func TransformAPTunnels(apID, serial string, tunnels []APTunnel) ([]sdk.Connection, []sdk.Resource) {
	var connections []sdk.Connection
	var stubs []sdk.Resource

	for _, t := range tunnels {
		if t.TunnelName == "" && t.TunnelID == "" {
			continue
		}
		destKey := t.DestinationName
		if destKey == "" {
			destKey = t.DestinationIPAddress
		}
		if destKey == "" {
			continue
		}
		destID := resourceID(fmt.Sprintf("tunnel-endpoint/%s", destKey))

		prov := sdk.Provider{Name: providerName, NativeID: destKey, Source: providerSource}
		stub, err := sdk.NewResource(destID, "osiris.hpe.arubacentral.tunnelendpoint", prov)
		if err == nil {
			stub.Name = destKey
			stub.Status = "unknown"
			if ip := sdk.NormalizeIP(t.DestinationIPAddress); ip != "" {
				stub.Properties = map[string]any{"ipv4": ip}
			}
			stubs = append(stubs, stub)
		}

		canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
			Type:      "osiris.hpe.arubacentral.tunnel",
			Direction: "forward",
			Source:    apID,
			Target:    destID,
			Qualifiers: map[string]string{
				"tunnel": t.TunnelName,
			},
		})
		connID := sdk.BuildConnectionID(canonicalKey, 16)
		conn, err := sdk.NewConnection(connID, "osiris.hpe.arubacentral.tunnel", apID, destID)
		if err != nil {
			continue
		}
		conn.Name = t.TunnelName
		conn.Status = mapDeviceStatus(t.Status)
		_ = conn.SetDirection("forward")

		props := map[string]any{}
		setIfNotEmpty(props, "crypto_type", t.CryptoType)
		setIfNotEmpty(props, "encapsulation_type", t.EncapsulationType)
		if t.Active {
			props["active"] = true
		}
		conn.Properties = props

		connections = append(connections, conn)
	}

	return connections, stubs
}

// TransformWLANs converts the account-wide WLAN (SSID) list into
// osiris.hpe.arubacentral.wlan resources. Returns the resources and a
// WLAN name -> resourceID map used to wire AP broadcast
// and BSSID connections.
func TransformWLANs(wlans []APWLAN) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	nameToID := make(map[string]string, len(wlans))

	seen := map[string]bool{}
	for _, w := range wlans {
		if w.WLANName == "" || seen[w.WLANName] {
			continue
		}
		seen[w.WLANName] = true

		id := resourceID(fmt.Sprintf("wlan/%s", w.WLANName))
		nameToID[w.WLANName] = id

		prov := sdk.Provider{Name: providerName, Source: providerSource}
		r, err := sdk.NewResource(id, "osiris.hpe.arubacentral.wlan", prov)
		if err != nil {
			continue
		}
		r.Name = w.WLANName
		r.Status = mapDeviceStatus(w.Status)

		props := map[string]any{}
		setIfNotEmpty(props, "security", w.Security)
		setIfNotEmpty(props, "security_level", w.SecurityLevel)
		setIfNotEmpty(props, "band", w.Band)
		setIfNotEmpty(props, "vlan", w.VLAN)
		r.Properties = props

		resources = append(resources, r)
	}

	return resources, nameToID
}

// TransformAPWLANConnections wires an AP to the WLANs it broadcasts
// (per-AP, per-band WLAN instances) via "network" connections.
func TransformAPWLANConnections(apID string, apWLANs []APWLAN, wlanIDToID map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, w := range apWLANs {
		targetID, ok := wlanIDToID[w.WLANName]
		if !ok {
			continue
		}
		canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
			Type:       "network",
			Direction:  "forward",
			Source:     apID,
			Target:     targetID,
			Qualifiers: map[string]string{"band": w.Band},
		})
		connID := sdk.BuildConnectionID(canonicalKey, 16)
		conn, err := sdk.NewConnection(connID, "network", apID, targetID)
		if err != nil {
			continue
		}
		conn.Name = fmt.Sprintf("broadcasts %s", w.WLANName)
		conn.Status = mapDeviceStatus(w.Status)
		_ = conn.SetDirection("forward")
		conn.Properties = map[string]any{"band": w.Band, "vlan": w.VLAN}
		connections = append(connections, conn)
	}
	return connections
}

// TransformRadios converts the account-wide radio list into
// osiris.hpe.arubacentral.radio resources plus contains connections
// from their parent access point.
// Returns the resources, connections and a
// serial|radioNumber -> resourceID map used to wire BSSIDs.
func TransformRadios(radios []Radio, apIDMap map[string]string) ([]sdk.Resource, []sdk.Connection, map[string]string) {
	var resources []sdk.Resource
	var connections []sdk.Connection
	radioIDMap := make(map[string]string, len(radios))

	for _, radio := range radios {
		if radio.SerialNumber == "" {
			continue
		}
		key := fmt.Sprintf("%s|%d", radio.SerialNumber, radio.RadioNumber)
		id := resourceID(fmt.Sprintf("%s/radio/%d", radio.SerialNumber, radio.RadioNumber))
		radioIDMap[key] = id

		prov := sdk.Provider{Name: providerName, NativeID: radio.SerialNumber, Source: providerSource}
		r, err := sdk.NewResource(id, "osiris.hpe.arubacentral.radio", prov)
		if err != nil {
			continue
		}
		r.Name = fmt.Sprintf("%s radio %d", radio.DeviceName, radio.RadioNumber)
		r.Status = mapDeviceStatus(radio.Status)

		props := map[string]any{}
		setIfNotEmpty(props, "band", radio.Band)
		setIfNotEmpty(props, "radio_type", radio.RadioType)
		setIfNotEmpty(props, "antenna", radio.Antenna)
		setIfNotEmpty(props, "spatial_stream", radio.SpatialStream)
		setIfNotEmpty(props, "mac_address", sdk.NormalizeMAC(radio.MACAddress))
		setIfPositive(props, "client_count", radio.ClientCount)
		r.Properties = props

		resources = append(resources, r)

		if apID, ok := apIDMap[radio.SerialNumber]; ok {
			connections = append(connections, containsConnection(apID, id, r.Name))
		}
	}

	return resources, connections, radioIDMap
}

// TransformBSSIDs converts the account-wide BSSID list into
// osiris.hpe.arubacentral.bssid resources, plus contains connections
// from the parent radio and network connections to the WLAN
// each BSSID broadcasts.
func TransformBSSIDs(bssids []BSSID, radioIDMap map[string]string, wlanIDToID map[string]string) ([]sdk.Resource, []sdk.Connection) {
	var resources []sdk.Resource
	var connections []sdk.Connection

	for _, b := range bssids {
		if b.BSSID == "" {
			continue
		}
		id := resourceID(fmt.Sprintf("bssid/%s", b.BSSID))

		prov := sdk.Provider{Name: providerName, NativeID: b.SerialNumber, Source: providerSource}
		r, err := sdk.NewResource(id, "osiris.hpe.arubacentral.bssid", prov)
		if err != nil {
			continue
		}
		r.Name = b.BSSID
		r.Status = "active"

		props := map[string]any{}
		setIfNotEmpty(props, "mac_address", sdk.NormalizeMAC(b.MACAddress))
		setIfNotEmpty(props, "wlan_name", b.WLANName)
		setIfPositive(props, "client_count", b.ClientCount)
		setIfPositive(props, "radio_number", b.RadioNumber)
		r.Properties = props

		resources = append(resources, r)

		radioKey := fmt.Sprintf("%s|%d", b.SerialNumber, b.RadioNumber)
		if radioID, ok := radioIDMap[radioKey]; ok {
			connections = append(connections, containsConnection(radioID, id, b.BSSID))
		}
		if wlanID, ok := wlanIDToID[b.WLANName]; ok {
			canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
				Type:      "network",
				Direction: "forward",
				Source:    id,
				Target:    wlanID,
			})
			connID := sdk.BuildConnectionID(canonicalKey, 16)
			conn, err := sdk.NewConnection(connID, "network", id, wlanID)
			if err == nil {
				conn.Name = fmt.Sprintf("broadcasts %s", b.WLANName)
				_ = conn.SetDirection("forward")
				connections = append(connections, conn)
			}
		}
	}

	return resources, connections
}

// TransformSwarms converts IAP mesh swarms (clusters) into
// osiris.hpe.arubacentral.swarm groups whose members are the
// participating access points, with the conductor
// AP recorded in properties.
func TransformSwarms(swarms []Swarm, apIDMap map[string]string) []sdk.Group {
	var groups []sdk.Group
	for _, s := range swarms {
		boundaryToken := s.ClusterID
		if boundaryToken == "" {
			boundaryToken = s.ID
		}
		if boundaryToken == "" {
			continue
		}
		gid := sdk.GroupID(sdk.GroupIDInput{Type: "osiris.hpe.arubacentral.swarm", BoundaryToken: boundaryToken})
		g, err := sdk.NewGroup(gid, "osiris.hpe.arubacentral.swarm")
		if err != nil {
			continue
		}
		g.Name = s.ClusterName
		if g.Name == "" {
			g.Name = boundaryToken
		}

		props := map[string]any{}
		setIfNotEmpty(props, "conductor_device_name", s.ConductorDeviceName)
		setIfNotEmpty(props, "conductor_serial", s.ConductorSerialNumber)
		setIfNotEmpty(props, "firmware_version", s.FirmwareVersion)
		g.Properties = props

		if id, ok := apIDMap[s.ConductorSerialNumber]; ok {
			g.AddMembers(id)
		}

		groups = append(groups, g)
	}
	return groups
}

// containsConnection builds a deterministic "contains" connection from
// source to target with the given human-readable name.
func containsConnection(sourceID, targetID, name string) sdk.Connection {
	canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
		Type:      "contains",
		Direction: "forward",
		Source:    sourceID,
		Target:    targetID,
	})
	connID := sdk.BuildConnectionID(canonicalKey, 16)
	conn, err := sdk.NewConnection(connID, "contains", sourceID, targetID)
	if err != nil {
		return sdk.Connection{}
	}
	conn.Name = name
	_ = conn.SetDirection("forward")
	return conn
}
