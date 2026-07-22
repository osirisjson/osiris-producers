// transform_topology.go - Gateway transforms and device-to-device
// neighbor (LLDP/CDP-like adjacency) connections, the network
// topology backbone.
//
// OSIRIS JSON Producer for HPE Aruba Networking Central introduction:
// [OSIRIS-JSON-HPE-ARUBA-NETWORKING]: https://docs.osirisjson.org/osiris-producers/network/hpe-aruba-networking-central
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package arubacentral

import (
	"fmt"

	"go.osirisjson.org/producers/pkg/sdk"
)

// TransformGateways converts Aruba Central gateways into OSIRIS
// network.gateway resources. Returns the resources and a
// serial -> resourceID map used to wire sub-resources
// (ports, VLANs, uplinks).
func TransformGateways(gateways []Gateway) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(gateways))

	for _, gw := range gateways {
		if gw.SerialNumber == "" {
			continue
		}
		id := resourceID(gw.SerialNumber)
		idMap[gw.SerialNumber] = id

		prov := provider(gw.SerialNumber, gw.Model, gw.FirmwareVersion, gw.SiteName)
		r, err := sdk.NewResource(id, "network.gateway", prov)
		if err != nil {
			continue
		}
		r.Name = gw.DeviceName
		r.Status = mapDeviceStatus(gw.Status)
		r.State = gw.Status

		props := map[string]any{}
		setIfNotEmpty(props, "site_id", gw.SiteID)
		setIfNotEmpty(props, "ip_address", sdk.NormalizeIP(gw.IPAddress))
		setIfNotEmpty(props, "mac_address", sdk.NormalizeMAC(gw.MACAddress))
		setIfNotEmpty(props, "mac_range", gw.MACRange)
		setIfNotEmpty(props, "device_function", gw.DeviceFunction)
		setIfNotEmpty(props, "role", gw.Role)
		setIfNotEmpty(props, "mode", gw.Mode)
		setIfNotEmpty(props, "cluster_name", gw.ClusterName)
		setIfNotEmpty(props, "reboot_reason", gw.RebootReason)
		setIfPositive(props, "cpu_utilization_pct", gw.CPUUtilization)
		setIfPositive(props, "memory_utilization_pct", gw.MemoryUtilization)
		if gw.UptimeInMillis > 0 {
			props["uptime_seconds"] = gw.UptimeInMillis / 1000
		}
		r.Properties = props

		resources = append(resources, r)
	}

	return resources, idMap
}

// TransformGatewayPorts converts a gateway's physical ports into OSIRIS
// network.interface resources plus "contains"
// connections from the gateway.
func TransformGatewayPorts(gwID, serial string, ports []GatewayPort) ([]sdk.Resource, []sdk.Connection) {
	var resources []sdk.Resource
	var connections []sdk.Connection

	for _, p := range ports {
		name := p.Name
		if name == "" {
			name = p.PortNumber
		}
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
		r.Status = mapHealthStatus(p.Health)

		props := map[string]any{}
		setIfNotEmpty(props, "admin_state", p.AdminState)
		setIfNotEmpty(props, "oper_state", p.OperState)
		setIfNotEmpty(props, "connector_type", p.ConnectorType)
		setIfNotEmpty(props, "port_type", p.PortType)
		setIfNotEmpty(props, "speed", p.Speed)
		setIfNotEmpty(props, "duplex", p.Duplex)
		setIfNotEmpty(props, "mac_address", sdk.NormalizeMAC(p.MACAddress))
		setIfNotEmpty(props, "vlan", p.VLAN)
		r.Properties = props

		resources = append(resources, r)
		connections = append(connections, containsConnection(gwID, id, fmt.Sprintf("%s -> %s", serial, name)))
	}

	return resources, connections
}

// TransformGatewayVLANs converts a gateway's VLANs into OSIRIS
// network.vlan groups. Gateway VLAN membership is not resolved to
// interface IDs at this API surface (the vlan response only lists a
// raw "interfaces" string), so these groups carry no members yet; the
// VLAN ID/name/subnet is still valuable topology metadata.
func TransformGatewayVLANs(serial string, vlans []GatewayVLAN) []sdk.Group {
	var groups []sdk.Group
	for _, v := range vlans {
		if v.VLANID == 0 {
			continue
		}
		boundaryToken := fmt.Sprintf("%s|vlan-%d", serial, v.VLANID)
		gid := sdk.GroupID(sdk.GroupIDInput{Type: "network.vlan", BoundaryToken: boundaryToken})

		g, err := sdk.NewGroup(gid, "network.vlan")
		if err != nil {
			continue
		}
		g.Name = v.Name
		if g.Name == "" {
			g.Name = fmt.Sprintf("VLAN %d", v.VLANID)
		}

		props := map[string]any{"vlan_id": v.VLANID}
		setIfNotEmpty(props, "vlan_type", v.VLANType)
		setIfNotEmpty(props, "ipv4", sdk.NormalizeIP(v.IPv4))
		setIfNotEmpty(props, "ipv4_mask", v.IPv4MaskAddr)
		setIfNotEmpty(props, "status", mapDeviceStatus(v.Status))
		g.Properties = props

		groups = append(groups, g)
	}
	return groups
}

// TransformGatewayUplinks converts a gateway's WAN uplinks into
// osiris.hpe.arubacentral.uplink connections to a stub WAN destination
// resource.
func TransformGatewayUplinks(gwID, serial string, uplinks []GatewayUplink) ([]sdk.Connection, []sdk.Resource) {
	var connections []sdk.Connection
	var stubs []sdk.Resource

	for _, u := range uplinks {
		if u.Name == "" {
			continue
		}
		destID := resourceID(fmt.Sprintf("%s/uplink/%s", serial, u.Name))

		prov := sdk.Provider{Name: providerName, NativeID: serial, Source: providerSource}
		stub, err := sdk.NewResource(destID, "osiris.hpe.arubacentral.uplinkendpoint", prov)
		if err == nil {
			stub.Name = u.Name
			stub.Status = "unknown"
			stubs = append(stubs, stub)
		}

		canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
			Type:      "osiris.hpe.arubacentral.uplink",
			Direction: "forward",
			Source:    gwID,
			Target:    destID,
		})
		connID := sdk.BuildConnectionID(canonicalKey, 16)
		conn, err := sdk.NewConnection(connID, "osiris.hpe.arubacentral.uplink", gwID, destID)
		if err != nil {
			continue
		}
		conn.Name = u.Name
		conn.Status = mapDeviceStatus(u.Status)
		_ = conn.SetDirection("forward")
		connections = append(connections, conn)
	}

	return connections, stubs
}

// neighborTypeSkip lists /neighbours "type" values that duplicate a
// relationship this producer already models more precisely elsewhere,
// and so are excluded from TransformNeighbors: "Client" (already a
// osiris.hpe.arubacentral.client resource + connection via
// TransformClients, keyed by MAC) and "Stack" (already a
// osiris.hpe.arubacentral.stack group via TransformSwitchStack,
// keyed by stack ID rather than a device serial which would otherwise
// produce a bogus stub resource here). Every other type (Switch,
// Access Point, Gateway, Unmanaged, and any not yet seen) is a genuine
// device adjacency and kept.
var neighborTypeSkip = map[string]bool{
	"Client": true,
	"Stack":  true,
}

// TransformNeighbors converts a device's LLDP/CDP-like neighbor list
// into "network" connections to the adjacent device. When the neighbor
// is a device collected in this run (resolved via allIDMap, keyed by
// serial), the connection targets it directly; otherwise a stub
// resource is created for the remote endpoint with
// osiris.hpe.arubacentral.device since its concrete category
// (switch/AP/gateway) is not yet known at this API surface and no
// standard OSIRIS JSON v1.0 specification type exists for a generic,
// not-yet-classified network device. This includes "Unmanaged"
// neighbors a real device Central saw via LLDP/CDP but does not manage.
func TransformNeighbors(deviceID, serial string, neighbors []Neighbor, allIDMap map[string]string) ([]sdk.Connection, []sdk.Resource) {
	var connections []sdk.Connection
	var stubs []sdk.Resource

	for _, n := range neighbors {
		if n.RemoteSerial == "" || n.LocalPort == "" || neighborTypeSkip[n.Type] {
			continue
		}

		var stub *sdk.Resource
		remoteID, ok := allIDMap[n.RemoteSerial]
		if !ok {
			remoteID = resourceID(n.RemoteSerial)
			prov := sdk.Provider{Name: providerName, NativeID: n.RemoteSerial, Source: providerSource}
			r, err := sdk.NewResource(remoteID, "osiris.hpe.arubacentral.device", prov)
			if err != nil {
				continue
			}
			r.Name = n.Name
			r.Status = "unknown"
			stub = &r
		}

		// Deliberately no per-port qualifiers in the canonical key:
		// the same physical link is reported once from each endpoint
		// with its local and remote port swapped, and the key must
		// resolve to the same ID regardless of which side reported it.
		// Port detail is still recorded in conn.Properties below.
		canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
			Type:      "network",
			Direction: "bidirectional",
			Source:    deviceID,
			Target:    remoteID,
		})
		connID := sdk.BuildConnectionID(canonicalKey, 16)
		conn, err := sdk.NewConnection(connID, "network", deviceID, remoteID)
		if err != nil {
			continue
		}
		conn.Name = fmt.Sprintf("%s:%s <-> %s:%s", serial, n.LocalPort, n.RemoteSerial, n.ToPort)
		conn.Status = mapHealthStatus(n.Health)
		conn.Properties = map[string]any{
			"local_port":  n.LocalPort,
			"remote_port": n.ToPort,
		}

		connections = append(connections, conn)
		if stub != nil {
			stubs = append(stubs, *stub)
		}
	}

	return connections, stubs
}
