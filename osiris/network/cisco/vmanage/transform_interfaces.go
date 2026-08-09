// transform_interfaces.go - Interface resource and router-to-interface
// "contains" connection transforms.
//
// OSIRIS JSON Producer for Cisco SD-WAN Manager (vManage) introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package vmanage

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"go.osirisjson.org/producers/pkg/sdk"
)

// TransformInterfaces converts one device's interfaces into OSIRIS
// resources plus "contains" connections wiring each one back to
// deviceResourceID, the router/controller resource it belongs to.
//
// deviceResourceType is the owning device's own OSIRIS JSON type
// (personalityToType's value "network.router" or
// "osiris.cisco.controller"): interfaces belonging to a
// "network.router" become OSIRIS-JSON-v1.0 section 7.5.2's
// "network.router.port" (a router port is specifically a router-owned
// interface per that section's definition).
//
// ifaces is the device's GET /dataservice/device/interface rows
// (selectIPv4Interfaces collapses the ipv4/ipv6 address-family row
// pairs the endpoint returns per interface down to one row each).
// wanIfaces is the device's GET
// /dataservice/device/control/waninterface rows, joined back to ifaces
// by interface name, used to enrich WAN-transport interfaces with a
// public/NAT address and SD-WAN color.
//
// Returns the resources, the "contains" connections, and a
// "{system-ip}:{color}" -> interface resource ID index consumed by
// TransformTunnels (transform_connections.go) to resolve SD-WAN tunnel
// endpoints.
func TransformInterfaces(deviceResourceID, deviceResourceType, deviceNativeKey, systemIP string, ifaces []Interface, wanIfaces []WANInterface) ([]sdk.Resource, []sdk.Connection, map[string]string) {
	var resources []sdk.Resource
	var connections []sdk.Connection
	tunnelIndex := make(map[string]string)

	resType := "network.interface"
	if deviceResourceType == "network.router" {
		resType = "network.router.port"
	}

	wanByIfname := make(map[string]WANInterface, len(wanIfaces))
	for _, w := range wanIfaces {
		wanByIfname[w.Interface] = w
	}

	for _, iface := range selectIPv4Interfaces(ifaces) {
		if iface.IfName == "" {
			continue
		}
		nativeID := resourceKey(deviceNativeKey, iface.IfName)
		id := resourceID(nativeID)

		prov := sdk.Provider{
			Name:     providerName,
			NativeID: nativeID,
			// The interface's own native identifier (e.g.
			// "GigabitEthernet0/0/0") Cisco's interface-class-plus-
			// slot/subslot/port naming already doubles as its native
			// type within the device, distinct from the generic
			// "Router Interface" category string
			// OSIRIS-JSON-v1.0 section 7.5.2's own example uses.
			Type: iface.IfName,
		}
		r, err := sdk.NewResource(id, resType, prov)
		if err != nil {
			continue
		}
		r.Name = iface.IfName
		r.Description = iface.Description
		r.Status = mapUpDownStatus(iface.IfOperStatus)
		if iface.IfOperStatus != "" {
			r.State = strings.ToLower(iface.IfOperStatus)
		}

		wan, isWAN := wanByIfname[iface.IfName]

		// Candidate addresses are classified by what they actually are
		// (RFC 1918/4193, via net.IP.IsPrivate) rather than trusted by
		// which vManage field they came from GET
		// /dataservice/device/control/waninterface's "private-ip"/
		// "public-ip" names mean "before NAT" vs "after NAT", not
		// "RFC 1918 private" vs "public": a Direct Internet Access
		// circuit with no NAT reports the same real, public
		// ISP-assigned address in both fields, which would otherwise
		// get mislabeled as private_ip.
		var candidates []string
		if base := stripCIDRHost(iface.IPAddress); base != "" {
			candidates = append(candidates, base)
		}
		if isWAN {
			if wp := sdk.NormalizeIP(wan.PrivateIP); wp != "" {
				candidates = append(candidates, wp)
			}
			if wq := sdk.NormalizeIP(wan.PublicIP); wq != "" {
				candidates = append(candidates, wq)
			}
		}

		ipAddrs := map[string]any{}
		var privateIPs []string
		var publicIP string
		seen := make(map[string]bool, len(candidates))
		for _, ip := range candidates {
			if seen[ip] {
				continue
			}
			seen[ip] = true
			if parsed := net.ParseIP(ip); parsed != nil && parsed.IsPrivate() {
				privateIPs = append(privateIPs, ip)
			} else {
				publicIP = ip
			}
		}
		if len(privateIPs) > 0 {
			ipAddrs["private_ip"] = privateIPs
		}
		if publicIP != "" {
			ipAddrs["public_ip"] = publicIP
		}

		// interface_name/admin_status/oper_status/speed_mbps/duplex/mtu
		// match OSIRIS-JSON-v1.0 section 7.5.2 network.router.port
		// "Common properties" table (description is set separately, on
		// r.Description, per the OSIRIS-JSON-v1.0 section 4.1.3.
		// See the field's own doc comment on Interface in client.go).
		// vlan_id is still not included: no vManage endpoint reports a
		// VLAN tag for a service-side sub-interface emitting a
		// placeholder would misrepresent absent data as a real value.
		props := map[string]any{"interface_name": iface.IfName}
		if len(ipAddrs) > 0 {
			props["ip_addresses"] = ipAddrs
		}
		if mac := sdk.NormalizeMAC(iface.HWAddr); mac != "" {
			props["mac_address"] = mac
		}
		if iface.IfAdminStatus != "" {
			props["admin_status"] = strings.ToLower(iface.IfAdminStatus)
		}
		if iface.IfOperStatus != "" {
			props["oper_status"] = strings.ToLower(iface.IfOperStatus)
		}
		if speed, ok := parseIntField(string(iface.SpeedMbps)); ok {
			props["speed_mbps"] = speed
		}
		if iface.Duplex != "" {
			props["duplex"] = iface.Duplex
		}
		if mtu, ok := parseIntField(string(iface.Mtu)); ok {
			props["mtu"] = mtu
		}
		if iface.EncapType != "" && iface.EncapType != "null" {
			props["encapsulation"] = iface.EncapType
		}
		if it := interfaceType(iface.PortType); it != "" {
			props["interface_type"] = it
		}
		if iface.VPNID != "" {
			// vManage's raw response only ever reports "vpn-id" never
			// any form of "vrf" so this is surfaced under vManage's own
			// native field name: OSIRIS-JSON-v1.0 network.router.port
			// Common Properties table does not document a vrf_id
			// property either.
			props["vpn_id"] = iface.VPNID
		}
		r.Properties = props

		if isWAN && wan.Color != "" {
			ext := map[string]any{"color": wan.Color}
			if wan.NatType != "" {
				ext["nat_type"] = wan.NatType
			}
			r.Extensions = map[string]any{extensionKey: ext}
		}

		resources = append(resources, r)

		canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
			Type:      "contains",
			Direction: "forward",
			Source:    deviceResourceID,
			Target:    id,
		})
		connID := sdk.BuildConnectionID(canonicalKey, 16)
		conn, err := sdk.NewConnection(connID, "contains", deviceResourceID, id)
		if err == nil {
			conn.Name = fmt.Sprintf("%s -> %s", deviceNativeKey, iface.IfName)
			_ = conn.SetDirection("forward")
			connections = append(connections, conn)
		}

		if isWAN && wan.Color != "" && systemIP != "" {
			tunnelIndex[systemIP+":"+wan.Color] = id
		}
	}

	return resources, connections, tunnelIndex
}

// selectIPv4Interfaces collapses GET /dataservice/device/interface
// one-row-per-address-family response down to a single row per
// interface name, preferring the af-type=ipv4 row (this producer does
// not model IPv6 addressing yet) and otherwise keeping the first row
// seen. Order of first appearance is preserved.
func selectIPv4Interfaces(ifaces []Interface) []Interface {
	byName := make(map[string]Interface, len(ifaces))
	order := make([]string, 0, len(ifaces))
	for _, iface := range ifaces {
		existing, seen := byName[iface.IfName]
		if !seen {
			byName[iface.IfName] = iface
			order = append(order, iface.IfName)
			continue
		}
		if existing.AFType != "ipv4" && iface.AFType == "ipv4" {
			byName[iface.IfName] = iface
		}
	}
	result := make([]Interface, 0, len(order))
	for _, name := range order {
		result = append(result, byName[name])
	}
	return result
}

// parseIntField parses a vManage numeric-string field (e.g. mtu,
// speed_mbps, both returned as strings like "1500") into an int,
// matching OSIRIS-JSON-v1.0 section 7.5.2 integer type for these
// properties. Returns false for empty or non-numeric input.
func parseIntField(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return n, true
}

// interfaceType maps vManage's port-type to the interface_type
// convention shown in OSIRIS-JSON-v1.0 7.5.3 ("primary"/"secondary"):
// "transport" (WAN uplink) is a device's primary interface, "service"
// (LAN-side) is secondary. Unrecognized values return "".
func interfaceType(portType string) string {
	switch portType {
	case "transport":
		return "primary"
	case "service":
		return "secondary"
	default:
		return ""
	}
}

// mapUpDownStatus converts a vManage interface admin/oper state string
// into an OSIRIS-JSON-v1.0 section 4.5.2 status enum value.
// Two distinct vocabularies exist depending on platform:
//   - classic vEdge (Viptela OS): simple "Up"/"Down".
//   - cEdge (IOS-XE, e.g. C8000v/C8200L/ISR/ASR): the IOS-XE
//     ietf-interfaces oper-status identity values
//     ("if-oper-state-ready", "if-oper-state-no-pass", etc.) passed
//     through verbatim - not documented anywhere in the vManage
//     OpenAPI spec.
func mapUpDownStatus(state string) string {
	switch strings.ToLower(state) {
	case "up", "if-oper-state-ready":
		return "active"
	case "down", "if-oper-state-down", "if-oper-state-lower-layer-down", "if-oper-state-not-present":
		return "inactive"
	case "if-oper-state-no-pass":
		// Administratively/physically up but not passing traffic (e.g.
		// a sub-interface whose encapsulation/protocol isn't fully
		// negotiated) - a real middle state, not plain up or down.
		return "degraded"
	default:
		return "unknown"
	}
}
