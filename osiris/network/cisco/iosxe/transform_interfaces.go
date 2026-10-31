// transform_interfaces.go - Interface resources, their addresses and
// device containment for the Cisco IOS-XE OSIRIS JSON Producer.
//
// An interface is emitted when it appears in either the configuration
// model (ietf-interfaces) or the operational model
// (Cisco-IOS-XE-interfaces-oper). Configuration supplies the name,
// description, enablement and every configured IPv4/IPv6 address;
// operational state supplies oper and admin status, speed, MTU, MAC
// address, negotiated duplex and the VRF binding. Async terminal lines
// are not network ports and are skipped.
//
// The resource type follows the device role: every interface of a
// router is a network.router.port; on a switch, physical Ethernet ports
// are network.switch.port and logical interfaces (SVIs, loopbacks,
// tunnels, subinterfaces) are network.interface. Port-channels are
// network.interface.lag on either.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package iosxe

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"go.osirisjson.org/producers/pkg/sdk"
)

// interfaceSet is the result of TransformInterfaces.
type interfaceSet struct {
	Resources []sdk.Resource
	// NameToID maps an interface name to its resource ID.
	NameToID map[string]string
	// Physical holds the resource IDs of physical Ethernet ports.
	Physical map[string]bool
}

// isAsyncLine reports whether name is an async terminal line.
func isAsyncLine(name string) bool {
	return strings.HasPrefix(name, "Async")
}

// isPhysical reports whether an interface is a physical Ethernet port:
// an ethernetCsmacd interface type, in either the iana-if-type identity
// form ("ianaift:ethernetCsmacd") or the Cisco operational enum form
// ("iana-iftype-ethernet-csmacd"), that is not a subinterface.
func isPhysical(name, ifType string) bool {
	if parentInterface(name) != "" {
		return false
	}
	t := strings.ToLower(ifType)
	return strings.HasSuffix(t, "ethernetcsmacd") || t == "iana-iftype-ethernet-csmacd"
}

// interfaceResourceType maps an interface to its resource type for a
// device of deviceType (network.router or network.switch).
func interfaceResourceType(deviceType, name string, physical bool) string {
	switch {
	case strings.HasPrefix(strings.ToLower(name), "port-channel"):
		return "network.interface.lag"
	case deviceType == "network.router":
		return "network.router.port"
	case physical:
		return "network.switch.port"
	default:
		return "network.interface"
	}
}

// adminStatus normalizes the operational admin-status enum, falling
// back to the configured enabled leaf.
func adminStatus(oper, enabled string) string {
	switch oper {
	case "if-state-up":
		return "up"
	case "if-state-down":
		return "down"
	case "if-state-test":
		return "testing"
	}
	switch strings.TrimSpace(enabled) {
	case "true":
		return "up"
	case "false":
		return "down"
	}
	return ""
}

// operStatus maps the Cisco oper-status enum to the RFC 2863
// ifOperStatus vocabulary. "no-pass" is the IOS-XE form of down.
func operStatus(oper string) string {
	switch oper {
	case "if-oper-state-ready":
		return "up"
	case "if-oper-state-no-pass":
		return "down"
	case "if-oper-state-lower-layer-down":
		return "lower-layer-down"
	case "if-oper-state-test":
		return "testing"
	case "if-oper-state-dormant":
		return "dormant"
	case "if-oper-state-not-present":
		return "not-present"
	}
	return ""
}

// interfaceStatus derives the OSIRIS JSON resource status.
func interfaceStatus(oper, admin string) string {
	switch oper {
	case "up":
		return "active"
	case "down", "lower-layer-down", "not-present":
		return "inactive"
	}
	if admin == "down" {
		return "inactive"
	}
	return "unknown"
}

// duplex maps the negotiated duplex enum; unknown-duplex is omitted.
func duplex(v string) string {
	switch v {
	case "full-duplex":
		return "full"
	case "half-duplex":
		return "half"
	case "auto-duplex":
		return "auto"
	}
	return ""
}

// speedMbps converts a bits-per-second decimal string to whole Mbps.
func speedMbps(bps string) (int64, bool) {
	n, err := strconv.ParseUint(strings.TrimSpace(bps), 10, 64)
	if err != nil || n < 1_000_000 {
		return 0, false
	}
	return int64(n / 1_000_000), true
}

// ipv4CIDR validates an address and dotted netmask and returns
// "a.b.c.d/len". The unspecified address and non-contiguous masks are
// rejected.
func ipv4CIDR(addr, mask string) (string, bool) {
	ip := net.ParseIP(strings.TrimSpace(addr)).To4()
	m := net.ParseIP(strings.TrimSpace(mask)).To4()
	if ip == nil || m == nil || ip.IsUnspecified() {
		return "", false
	}
	ones, bits := net.IPMask(m).Size()
	if bits == 0 {
		return "", false
	}
	return fmt.Sprintf("%s/%d", ip, ones), true
}

// ipv6CIDR validates an address and prefix length and returns the
// canonical "addr/len" form. The unspecified address is rejected.
func ipv6CIDR(addr, prefixLen string) (string, bool) {
	ip := net.ParseIP(strings.TrimSpace(addr))
	if ip == nil || ip.To4() != nil || ip.IsUnspecified() {
		return "", false
	}
	n, err := strconv.Atoi(strings.TrimSpace(prefixLen))
	if err != nil || n < 0 || n > 128 {
		return "", false
	}
	return fmt.Sprintf("%s/%d", ip, n), true
}

// addressProperties sets ip_address/subnet_mask (the first valid IPv4
// address and its mask) and the ipv4_addresses/ipv6_addresses lists
// (every valid configured address in CIDR form, in configured order,
// without duplicates). When the configuration carries no IPv4 address,
// the operational primary address is used, which covers addresses
// learned at run time.
func addressProperties(props map[string]any, cfg ietfIface, oper operIface, hasOper bool) {
	var v4, v6 []string
	seen := map[string]bool{}
	firstAddr, firstMask := "", ""

	addV4 := func(addr, mask string) {
		c, ok := ipv4CIDR(addr, mask)
		if !ok || seen[c] {
			return
		}
		seen[c] = true
		v4 = append(v4, c)
		if firstAddr == "" {
			firstAddr, firstMask = strings.TrimSpace(addr), strings.TrimSpace(mask)
		}
	}
	if cfg.IPv4 != nil {
		for _, a := range cfg.IPv4.Address {
			addV4(a.IP, a.Prefix)
		}
	}
	if len(v4) == 0 && hasOper {
		addV4(oper.IPv4, oper.IPv4Mask)
	}
	if cfg.IPv6 != nil {
		for _, a := range cfg.IPv6.Address {
			if c, ok := ipv6CIDR(a.IP, a.PrefixLength); ok && !seen[c] {
				seen[c] = true
				v6 = append(v6, c)
			}
		}
	}

	if firstAddr != "" {
		props["ip_address"] = firstAddr
		props["subnet_mask"] = firstMask
	}
	if len(v4) > 0 {
		props["ipv4_addresses"] = v4
	}
	if len(v6) > 0 {
		props["ipv6_addresses"] = v6
	}
}

// TransformInterfaces builds the interface resources of the device with
// key deviceKey and type deviceType from configuration and operational
// state.
func TransformInterfaces(deviceKey, deviceType string, cfg ietfInterfaces, oper interfacesOper) interfaceSet {
	set := interfaceSet{NameToID: map[string]string{}, Physical: map[string]bool{}}

	operByName := make(map[string]operIface, len(oper.Interfaces))
	for _, o := range oper.Interfaces {
		if n := strings.TrimSpace(o.Name); n != "" {
			operByName[n] = o
		}
	}
	cfgByName := make(map[string]ietfIface, len(cfg.Interfaces))
	var names []string
	for _, c := range cfg.Interfaces {
		n := strings.TrimSpace(c.Name)
		if n == "" || isAsyncLine(n) {
			continue
		}
		if _, dup := cfgByName[n]; !dup {
			names = append(names, n)
		}
		cfgByName[n] = c
	}
	for _, o := range oper.Interfaces {
		n := strings.TrimSpace(o.Name)
		if _, inCfg := cfgByName[n]; n == "" || inCfg || isAsyncLine(n) {
			continue
		}
		names = append(names, n)
		cfgByName[n] = ietfIface{Name: n}
	}

	for _, name := range names {
		c := cfgByName[name]
		o, hasOper := operByName[name]

		ifType := c.Type
		if hasOper && o.InterfaceType != "" {
			ifType = o.InterfaceType
		}
		physical := isPhysical(name, ifType)
		resType := interfaceResourceType(deviceType, name, physical)

		native := deviceKey + "/" + name
		id := resourceID(providerName, native)
		r, err := sdk.NewResource(id, resType, sdk.Provider{Name: providerName, NativeID: native})
		if err != nil {
			continue
		}
		r.Name = name

		props := map[string]any{"interface_name": name}
		if d := strings.TrimSpace(c.Description); d != "" {
			props["description"] = d
		} else if d := strings.TrimSpace(o.Description); d != "" {
			props["description"] = d
		}
		admin := adminStatus(o.AdminStatus, c.Enabled)
		if admin != "" {
			props["admin_status"] = admin
		}
		op := operStatus(o.OperStatus)
		if op != "" {
			props["oper_status"] = op
		}
		r.Status = interfaceStatus(op, admin)
		if mbps, ok := speedMbps(o.Speed); ok {
			props["speed_mbps"] = mbps
		}
		if d := duplex(o.EtherState.NegotiatedDuplex); d != "" {
			props["duplex"] = d
		}
		if n, err := strconv.ParseInt(strings.TrimSpace(o.MTU), 10, 64); err == nil && n > 0 {
			props["mtu"] = n
		}
		if m := sdk.NormalizeMAC(o.PhysAddress); m != "" && m != "00:00:00:00:00:00" {
			props["mac_address"] = m
		}
		addressProperties(props, c, o, hasOper)
		if parent := parentInterface(name); parent != "" {
			props["parent_interface"] = parent
		}
		r.Properties = props

		set.Resources = append(set.Resources, r)
		set.NameToID[name] = id
		if physical {
			set.Physical[id] = true
		}
	}
	return set
}

// TransformDeviceContainment links the device to every interface it
// owns: contains.physical for physical Ethernet ports and
// contains.logical for every other interface.
func TransformDeviceContainment(deviceID, deviceName string, set interfaceSet) []sdk.Connection {
	var out []sdk.Connection
	for _, r := range set.Resources {
		connType := "contains.logical"
		if set.Physical[r.ID] {
			connType = "contains.physical"
		}
		key := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
			Type:      connType,
			Direction: "forward",
			Source:    deviceID,
			Target:    r.ID,
		})
		conn, err := sdk.NewConnection(sdk.BuildConnectionID(key, 16), connType, deviceID, r.ID)
		if err != nil {
			continue
		}
		conn.Name = fmt.Sprintf("%s contains %s", deviceName, r.Name)
		conn.Direction = "forward"
		conn.Status = "active"
		out = append(out, conn)
	}
	return out
}

// parentInterface extracts the parent interface name from a
// subinterface name, e.g. "GigabitEthernet0/0/0.100" -> "GigabitEthernet0/0/0".
// It returns "" when name is not a subinterface.
func parentInterface(name string) string {
	dot := strings.LastIndex(name, ".")
	if dot == -1 {
		return ""
	}
	if _, err := strconv.Atoi(name[dot+1:]); err != nil {
		return ""
	}
	return name[:dot]
}
