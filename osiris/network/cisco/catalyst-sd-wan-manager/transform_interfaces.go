// transform_interfaces.go - Interface resource and router-to-interface
// "contains" connection transforms.
//
// OSIRIS JSON Producer for Cisco Catalyst SD-WAN Manager introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package catalystsdwanmanager

import (
	"fmt"
	"net"
	"sort"
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
// (mergeInterfaceRows reconciles the per-address-family rows of each
// interface into one, and keeps equal interface names in different
// VPNs apart, see interfaceKey).
// wanIfaces is the device's GET
// /dataservice/device/control/waninterface rows, joined back to ifaces
// by interface name, used to enrich WAN-transport interfaces with a
// public/NAT address and SD-WAN color.
//
// Returns the resources, the "contains" connections, and a
// "{system-ip}:{color}" -> interface resource ID index consumed by
// TransformTunnels (transform_connections.go) to resolve SD-WAN tunnel
// endpoints. A color carried by more than one interface of the device
// maps to "" (ambiguous): a tunnel on it is left unresolved rather than
// attached to an arbitrary interface.
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

	for _, m := range mergeInterfaceRows(ifaces) {
		iface := m.Interface
		nativeID := resourceKey(deviceNativeKey, m.key)
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

		// Transport (WAN) data joins only the interface that keeps the
		// plain interface name: the lowest VPN, which is VPN 0 for a
		// transport interface.
		wan, isWAN := wanByIfname[iface.IfName]
		isWAN = isWAN && m.key == iface.IfName

		// Candidate addresses are classified by what they actually are
		// (RFC 1918/4193, via net.IP.IsPrivate) rather than trusted by
		// which SD-WAN Manager field they came from GET
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
			if isPublicIP(ip) {
				publicIP = ip
			} else {
				privateIPs = append(privateIPs, ip)
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
		// r.Description, per the OSIRIS-JSON-v1.0 section 4.1.3. See
		// the field's own doc comment on Interface in client.go).
		// vlan_id is still not included: no SD-WAN Manager endpoint
		// reports a VLAN tag for a service-side sub-interface emitting
		// a placeholder would misrepresent absent data as a real value.
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
			// SD-WAN Manager's raw response only ever reports "vpn-id"
			// never any form of "vrf" so this is surfaced under SD-WAN
			// Manager's own native field name: OSIRIS-JSON-v1.0
			// network.router.port Common Properties table does not
			// document a vrf_id property either.
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

		if conn, ok := containsConnection(deviceResourceID, deviceNativeKey, id, iface.IfName); ok {
			connections = append(connections, conn)
		}

		if isWAN && wan.Color != "" && systemIP != "" {
			addTunnelEndpoint(tunnelIndex, systemIP+":"+wan.Color, id)
		}
	}

	return resources, connections, tunnelIndex
}

// mergedInterface is one reconciled interface and the key its
// resource ID derives from (see interfaceKey).
type mergedInterface struct {
	Interface
	key string
}

// mergeInterfaceRows reconciles GET /dataservice/device/interface rows,
// which arrive one per (interface, address family), into one entry per
// interface. Rows belong to the same interface when they share the
// interface name and the VPN: an equal name in another VPN is another
// interface, never merged. Within one interface the IPv4 row supplies
// the fields and any field it leaves empty (description, MAC address,
// statuses, speed, MTU) is filled from the other rows, so a description
// carried only by the IPv6 row is kept. Rows are ordered by address
// family and address before reconciling, so the result does not depend
// on API row order. Rows without an interface name are skipped.
func mergeInterfaceRows(rows []Interface) []mergedInterface {
	type groupKey struct{ vpn, name string }
	groups := map[groupKey][]Interface{}
	vpnsByName := map[string][]string{}
	for _, r := range rows {
		if r.IfName == "" {
			continue
		}
		k := groupKey{r.VPNID, r.IfName}
		if _, ok := groups[k]; !ok {
			vpnsByName[r.IfName] = append(vpnsByName[r.IfName], r.VPNID)
		}
		groups[k] = append(groups[k], r)
	}

	keys := make([]groupKey, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].name != keys[j].name {
			return keys[i].name < keys[j].name
		}
		return vpnLess(keys[i].vpn, keys[j].vpn)
	})

	out := make([]mergedInterface, 0, len(keys))
	for _, k := range keys {
		rs := groups[k]
		sort.SliceStable(rs, func(i, j int) bool {
			ai, aj := rs[i].AFType == "ipv4", rs[j].AFType == "ipv4"
			if ai != aj {
				return ai
			}
			if rs[i].AFType != rs[j].AFType {
				return rs[i].AFType < rs[j].AFType
			}
			return rs[i].IPAddress < rs[j].IPAddress
		})
		m := rs[0]
		for _, r := range rs[1:] {
			fillEmpty(&m.Description, r.Description)
			fillEmpty(&m.HWAddr, r.HWAddr)
			fillEmpty(&m.IfAdminStatus, r.IfAdminStatus)
			fillEmpty(&m.IfOperStatus, r.IfOperStatus)
			fillEmpty(&m.PortType, r.PortType)
			fillEmpty(&m.Duplex, r.Duplex)
			fillEmpty(&m.EncapType, r.EncapType)
			if m.SpeedMbps == "" {
				m.SpeedMbps = r.SpeedMbps
			}
			if m.Mtu == "" {
				m.Mtu = r.Mtu
			}
		}
		out = append(out, mergedInterface{Interface: m, key: interfaceKey(k.name, k.vpn, vpnsByName[k.name])})
	}
	return out
}

// interfaceKey is the per-device interface identity. An interface name
// used in a single VPN keeps the plain name, so its resource ID is
// unchanged from earlier releases. When one device uses the same name
// in several VPNs, the lowest VPN keeps the plain name and every other
// VPN appends "@vpn<id>", so the interfaces stay distinct and the
// transport interface (VPN 0) keeps a stable ID that a neighbor
// reference in another site's document can reproduce.
func interfaceKey(name, vpn string, vpns []string) string {
	if len(vpns) < 2 {
		return name
	}
	lowest := vpns[0]
	for _, v := range vpns[1:] {
		if vpnLess(v, lowest) {
			lowest = v
		}
	}
	if vpn == lowest {
		return name
	}
	return name + "@vpn" + vpn
}

// vpnLess orders VPN IDs numerically, falling back to string order for
// non-numeric values.
func vpnLess(a, b string) bool {
	ai, aerr := strconv.Atoi(a)
	bi, berr := strconv.Atoi(b)
	if aerr == nil && berr == nil {
		return ai < bi
	}
	if (aerr == nil) != (berr == nil) {
		return aerr == nil
	}
	return a < b
}

// fillEmpty sets *dst to v when *dst is empty.
func fillEmpty(dst *string, v string) {
	if *dst == "" {
		*dst = v
	}
}

// addTunnelEndpoint records key -> id, marking key ambiguous ("") when
// a different interface already claims it.
func addTunnelEndpoint(index map[string]string, key, id string) {
	if prev, ok := index[key]; ok && prev != id {
		index[key] = ""
		return
	}
	index[key] = id
}

// containsConnection builds the "contains" connection from a device to
// one of its interfaces. The interface's own site document and any
// other site document that carries it as a neighbor build it the same
// way, so the connection ID is the same in every document.
func containsConnection(deviceResourceID, deviceNativeKey, ifaceID, ifname string) (sdk.Connection, bool) {
	canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
		Type:      "contains",
		Direction: "forward",
		Source:    deviceResourceID,
		Target:    ifaceID,
	})
	conn, err := sdk.NewConnection(sdk.BuildConnectionID(canonicalKey, 16), "contains", deviceResourceID, ifaceID)
	if err != nil {
		return sdk.Connection{}, false
	}
	conn.Name = fmt.Sprintf("%s -> %s", deviceNativeKey, ifname)
	_ = conn.SetDirection("forward")
	return conn, true
}

// sharedAddressSpace is RFC 6598 carrier-grade NAT space, which
// net.IP.IsPrivate does not cover.
var sharedAddressSpace = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// isPublicIP reports whether ip is a globally routable unicast address.
// Private (RFC 1918 / RFC 4193), shared (RFC 6598), loopback,
// link-local, multicast and unspecified addresses are not public.
func isPublicIP(ip string) bool {
	p := net.ParseIP(ip)
	if p == nil || !p.IsGlobalUnicast() || p.IsPrivate() {
		return false
	}
	return !sharedAddressSpace.Contains(p)
}

// parseIntField parses an SD-WAN Manager numeric-string field (e.g.
// mtu, speed_mbps, both returned as strings like "1500") into an int,
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

// interfaceType maps SD-WAN Manager's port-type to the interface_type
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

// mapUpDownStatus converts an SD-WAN Manager interface admin/oper
// state string into an OSIRIS-JSON-v1.0 section 4.5.2 status enum
// value. Two distinct vocabularies exist depending on platform:
//   - classic vEdge (Viptela OS): simple "Up"/"Down".
//   - cEdge (IOS-XE, e.g. C8000v/C8200L/ISR/ASR): the IOS-XE
//     ietf-interfaces oper-status identity values
//     ("if-oper-state-ready", "if-oper-state-no-pass", etc.) passed
//     through verbatim - not documented anywhere in the SD-WAN Manager
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
