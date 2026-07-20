// transform_switches.go - Switch, interface, VLAN, LAG, stack
// and VSX transforms.
//
// OSIRIS JSON Producer for HPE Aruba Networking Central introduction:
// [OSIRIS-JSON-HPE-ARUBA-NETWORKING]: https://docs.osirisjson.org/osiris-producers/network/hpe-aruba-networking
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package arubacentral

import (
	"fmt"

	"go.osirisjson.org/producers/pkg/sdk"
)

// TransformSwitches converts Aruba Central switches into OSIRIS
// network.switch resources. Returns the resources and a
// serial -> resourceID map used to wire sub-resources (interfaces,
// VLANs, stacks, VSX peers) back to their switch.
func TransformSwitches(switches []Switch) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(switches))

	for _, sw := range switches {
		if sw.SerialNumber == "" {
			continue
		}
		id := resourceID(sw.SerialNumber)
		idMap[sw.SerialNumber] = id

		prov := provider(sw.SerialNumber, sw.Model, sw.FirmwareVersion, sw.SiteName)
		r, err := sdk.NewResource(id, "network.switch", prov)
		if err != nil {
			continue
		}
		r.Name = sw.DeviceName
		r.Status = mapDeviceStatus(sw.Status)
		r.State = sw.Status

		props := map[string]any{}
		setIfNotEmpty(props, "site_id", sw.SiteID)
		setIfNotEmpty(props, "ipv4", sdk.NormalizeIP(sw.IPv4))
		setIfNotEmpty(props, "ipv6", sdk.NormalizeIP(sw.IPv6))
		setIfNotEmpty(props, "mac_address", sdk.NormalizeMAC(sw.MACAddress))
		setIfNotEmpty(props, "public_ip", sw.PublicIP)
		setIfNotEmpty(props, "switch_role", sw.SwitchRole)
		setIfNotEmpty(props, "switch_type", sw.SwitchType)
		setIfNotEmpty(props, "deployment", sw.Deployment)
		setIfNotEmpty(props, "stack_id", sw.StackID)
		if sw.UptimeInMillis > 0 {
			props["uptime_seconds"] = sw.UptimeInMillis / 1000
		}
		if ts := epochMillisToRFC3339(sw.LastSeenAt); ts != "" {
			props["last_seen_at"] = ts
		}
		if len(sw.SwitchTrends) > 0 {
			t := sw.SwitchTrends[0]
			props["cpu_utilization_pct"] = t.CPUUtilization
			props["memory_utilization_pct"] = t.MemoryUtilization
			if t.PowerConsumption > 0 {
				props["power_consumption_watts"] = t.PowerConsumption
			}
			if t.SystemTemperature > 0 {
				props["system_temperature"] = t.SystemTemperature
			}
		}
		r.Properties = props

		resources = append(resources, r)
	}

	return resources, idMap
}

// TransformSwitchInterfaces converts a switch's interfaces into OSIRIS
// network.interface resources plus "contains" connections from
// the switch. Returns the resources, connections and an interface
// name -> resourceID map used to wire VLANs and LAGs.
//
// Each interface is wired to its own switch resource (resolved from
// iface.SerialNumber via switchIDMap, falling back to defaultSerial
// when that field is empty), not necessarily to the switch the query
// was made against: for a switch stack,
// /switches/{conductorSerial}/interfaces returns every physical
// member's interfaces in one response.
func TransformSwitchInterfaces(switchIDMap map[string]string, defaultSerial string, ifaces []SwitchInterface) ([]sdk.Resource, []sdk.Connection, map[string]string) {
	var resources []sdk.Resource
	var connections []sdk.Connection
	nameToID := make(map[string]string, len(ifaces))

	for _, iface := range ifaces {
		if iface.Name == "" {
			continue
		}
		serial := iface.SerialNumber
		if serial == "" {
			serial = defaultSerial
		}
		switchID, ok := switchIDMap[serial]
		if !ok {
			continue
		}

		id := resourceID(fmt.Sprintf("%s/interface/%s", serial, iface.Name))
		nameToID[iface.Name] = id

		prov := sdk.Provider{Name: providerName, NativeID: serial, Source: providerSource}
		r, err := sdk.NewResource(id, "network.interface", prov)
		if err != nil {
			continue
		}
		r.Name = iface.Name
		r.Description = iface.Description
		r.Status = mapDeviceStatus(iface.Status)

		props := map[string]any{}
		setIfNotEmpty(props, "admin_status", iface.AdminStatus)
		setIfNotEmpty(props, "oper_status", iface.OperStatus)
		setIfNotEmpty(props, "connector", iface.Connector)
		setIfNotEmpty(props, "duplex", iface.Duplex)
		setIfPositive(props, "speed_mbps", iface.Speed)
		setIfPositive(props, "mtu", iface.MTU)
		setIfNotEmpty(props, "ipv4", sdk.NormalizeIP(iface.IPv4))
		setIfPositive(props, "native_vlan", iface.NativeVlan)
		if len(iface.AllowedVlans) > 0 {
			props["allowed_vlans"] = iface.AllowedVlans
		}
		setIfNotEmpty(props, "lag", iface.Lag)
		setIfNotEmpty(props, "module", iface.Module)
		setIfNotEmpty(props, "poe_status", iface.PoEStatus)
		r.Properties = props

		resources = append(resources, r)

		canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
			Type:      "contains",
			Direction: "forward",
			Source:    switchID,
			Target:    id,
		})
		connID := sdk.BuildConnectionID(canonicalKey, 16)
		conn, err := sdk.NewConnection(connID, "contains", switchID, id)
		if err != nil {
			continue
		}
		conn.Name = fmt.Sprintf("%s -> %s", serial, iface.Name)
		_ = conn.SetDirection("forward")
		connections = append(connections, conn)
	}

	return resources, connections, nameToID
}

// TransformSwitchVLANs converts a switch's VLANs into OSIRIS
// network.vlan groups, with member interfaces resolved via ifNameToID.
func TransformSwitchVLANs(serial string, vlans []SwitchVLAN, ifNameToID map[string]string) []sdk.Group {
	var groups []sdk.Group
	for _, v := range vlans {
		if v.ID == "" {
			continue
		}
		boundaryToken := fmt.Sprintf("%s|vlan-%s", serial, v.ID)
		gid := sdk.GroupID(sdk.GroupIDInput{Type: "network.vlan", BoundaryToken: boundaryToken})

		g, err := sdk.NewGroup(gid, "network.vlan")
		if err != nil {
			continue
		}
		g.Name = fmt.Sprintf("VLAN %s", v.ID)
		if v.Name != "" {
			g.Description = v.Name
		}

		props := map[string]any{"vlan_id": v.ID}
		setIfNotEmpty(props, "ipv4", sdk.NormalizeIP(v.IPv4))
		setIfNotEmpty(props, "voice", v.Voice)
		setIfNotEmpty(props, "status", mapDeviceStatus(v.Status))
		g.Properties = props

		var members []string
		for _, portName := range v.Interfaces {
			if id, ok := ifNameToID[portName]; ok {
				members = append(members, id)
			}
		}
		for _, portName := range v.TaggedPorts {
			if id, ok := ifNameToID[portName]; ok {
				members = append(members, id)
			}
		}
		for _, portName := range v.UntaggedPorts {
			if id, ok := ifNameToID[portName]; ok {
				members = append(members, id)
			}
		}
		if len(members) > 0 {
			g.AddMembers(members...)
		}

		groups = append(groups, g)
	}
	return groups
}

// TransformSwitchLAGs converts a switch's link-aggregation groups into
// OSIRIS network.lag groups, with member interfaces
// resolved via ifNameToID.
func TransformSwitchLAGs(serial string, lags []SwitchLAG, ifNameToID map[string]string) []sdk.Group {
	var groups []sdk.Group
	for _, l := range lags {
		if l.ID == "" {
			continue
		}
		boundaryToken := fmt.Sprintf("%s|lag-%s", serial, l.ID)
		gid := sdk.GroupID(sdk.GroupIDInput{Type: "network.lag", BoundaryToken: boundaryToken})

		g, err := sdk.NewGroup(gid, "network.lag")
		if err != nil {
			continue
		}
		g.Name = l.Name
		if g.Name == "" {
			g.Name = fmt.Sprintf("LAG %s", l.ID)
		}
		g.Properties = map[string]any{"port_count": l.Count}

		var members []string
		for _, portName := range l.Ports {
			if id, ok := ifNameToID[portName]; ok {
				members = append(members, id)
			}
		}
		if len(members) > 0 {
			g.AddMembers(members...)
		}

		groups = append(groups, g)
	}
	return groups
}

// EnrichSwitchHardwareForStack applies each per-member hardware
// category returned by a single /hardware-categories query to the
// correct switch resource, matched by hw.SerialNumber via switchIDMap.
//
// For a switch stack, /switches/{conductorSerial}/hardware-categories
// returns one category set per physical stack member (each carrying its
// own serialNumber), not just the conductor's own hardware, querying a
// non-conductor member's own serial result in 404s.
// For a standalone switch, this is just the one query result
// for its own serial.
func EnrichSwitchHardwareForStack(resources []sdk.Resource, resIdx map[string]int, switchIDMap map[string]string, categories []SwitchHardware) {
	for _, hw := range categories {
		switchID, ok := switchIDMap[hw.SerialNumber]
		if !ok {
			continue
		}
		idx, ok := resIdx[switchID]
		if !ok {
			continue
		}
		EnrichSwitchHardware(&resources[idx], hw)
	}
}

// EnrichSwitchHardware folds one hardware health category into the
// switch resource's properties and downgrades its status to "degraded"
// when any component (CPU, memory, temperature, fans, power supplies)
// is unhealthy.
func EnrichSwitchHardware(r *sdk.Resource, hw SwitchHardware) {
	if r.Properties == nil {
		r.Properties = map[string]any{}
	}

	unhealthy := false
	check := func(health string) string {
		mapped := mapHealthStatus(health)
		if mapped == "degraded" {
			unhealthy = true
		}
		return health
	}

	hwProps := map[string]any{}
	setIfNotEmpty(hwProps, "cpu_health", check(hw.CPU.Health))
	setIfNotEmpty(hwProps, "memory_health", check(hw.Memory.Health))
	setIfNotEmpty(hwProps, "temperature_health", check(hw.Temperature.Health))
	setIfNotEmpty(hwProps, "fans_health", check(hw.Fans.Health))
	setIfPositive(hwProps, "fans_up_count", hw.Fans.UpCount)
	setIfPositive(hwProps, "fans_total_count", hw.Fans.TotalCount)
	setIfNotEmpty(hwProps, "power_supplies_health", check(hw.PowerSupplies.Health))
	setIfPositive(hwProps, "power_supplies_up_count", hw.PowerSupplies.UpCount)
	setIfPositive(hwProps, "power_supplies_total_count", hw.PowerSupplies.TotalCount)

	if len(hwProps) > 0 {
		r.Properties["hardware"] = hwProps
	}
	if unhealthy && r.Status == "active" {
		r.Status = "degraded"
	}
}

// TransformSwitchStack converts a stack membership response into an
// OSIRIS network.stack group whose members are the participating
// switches (resolved via switchIDMap).
// Returns nil when the stack has no resolvable members.
func TransformSwitchStack(serial string, stack *StackMembers, switchIDMap map[string]string) *sdk.Group {
	if stack == nil || len(stack.Members) == 0 {
		return nil
	}

	gid := sdk.GroupID(sdk.GroupIDInput{Type: "network.stack", BoundaryToken: serial})
	g, err := sdk.NewGroup(gid, "network.stack")
	if err != nil {
		return nil
	}
	g.Name = fmt.Sprintf("Stack %s", serial)
	g.Properties = map[string]any{
		"stack_type": stack.StackType,
		"topology":   stack.Topology,
	}

	var members []string
	for _, m := range stack.Members {
		if id, ok := switchIDMap[m.SerialNumber]; ok {
			members = append(members, id)
		}
	}
	if len(members) == 0 {
		return nil
	}
	g.AddMembers(members...)
	return &g
}

// vsxConnectionType is the connection type for VSX peering. VSX is
// proprietary Aruba AOS-CX multi-chassis link-aggregation technology
// so it is namespaced per spec chapter 5.2.4, matching
// extensionNamespace's "hpe." grouping.
const vsxConnectionType = "osiris.hpe.arubacentral.vsx"

// TransformSwitchVSX converts a VSX peering response into an OSIRIS
// vsxConnectionType connection between the two switches. If the peer
// switch was not collected in this run, a stub resource is created for
// it (mirrors the LLDP neighbor stub pattern used elsewhere in this
// producer), so the connection still has a valid target.
func TransformSwitchVSX(switchID string, vsx *SwitchVSX,
	switchIDMap map[string]string) (*sdk.Connection, *sdk.Resource) {
	if vsx == nil || vsx.VSXPeerSerial == "" {
		return nil, nil
	}

	var stub *sdk.Resource
	peerID, ok := switchIDMap[vsx.VSXPeerSerial]
	if !ok {
		peerID = resourceID(vsx.VSXPeerSerial)
		prov := sdk.Provider{Name: providerName, NativeID: vsx.VSXPeerSerial, Source: providerSource}
		r, err := sdk.NewResource(peerID, "network.switch", prov)
		if err != nil {
			return nil, nil
		}
		r.Name = vsx.VSXPeerName
		r.Status = "unknown"
		stub = &r
	}

	canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
		Type:      vsxConnectionType,
		Direction: "bidirectional",
		Source:    switchID,
		Target:    peerID,
	})
	connID := sdk.BuildConnectionID(canonicalKey, 16)
	conn, err := sdk.NewConnection(connID, vsxConnectionType, switchID, peerID)
	if err != nil {
		return nil, stub
	}
	conn.Name = fmt.Sprintf("VSX %s <-> %s", vsx.Role, vsx.PeerRole)
	conn.Status = mapHealthStatus(vsx.VSXHealth.Health)

	props := map[string]any{}
	setIfNotEmpty(props, "role", vsx.Role)
	setIfNotEmpty(props, "peer_role", vsx.PeerRole)
	setIfNotEmpty(props, "keepalive_status", vsx.KeepaliveStatus)
	setIfNotEmpty(props, "keepalive_health", vsx.KeepaliveHealth)
	setIfNotEmpty(props, "isl_port", vsx.ISLPort)
	setIfNotEmpty(props, "peer_isl_port", vsx.PeerISLPort)
	conn.Properties = props

	return &conn, stub
}
