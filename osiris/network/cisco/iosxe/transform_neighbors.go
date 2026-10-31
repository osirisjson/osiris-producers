// transform_neighbors.go - Link-layer neighbors for the Cisco IOS-XE
// OSIRIS JSON Producer: Cisco-IOS-XE-lldp-oper and
// Cisco-IOS-XE-cdp-oper merged into one set of physical.ethernet
// connections to unresolved remote-port stub resources.
//
// LLDP and CDP often report the same physical link. Observations are
// deduplicated by (local port, remote system, remote port), with the
// remote system compared by host name (case-insensitive, domain
// stripped) and the remote port compared in its expanded IOS form, so
// "LAB-SW01.example.com Gi1/0/24" (CDP) and "LAB-SW01
// GigabitEthernet1/0/24" (LLDP) are one link. Each connection and stub
// records every protocol that saw it in properties.discovered_via; the
// stub ID and name use the host name and expanded port, while
// remote_system and remote_port keep the names as reported (the longest
// reported system name, usually the FQDN).
// An observation missing its local port, remote system, or remote port
// is dropped and so is one whose local port is not an emitted
// interface: only resolvable relationships are reported.
//
// A device that hears itself, a cable looped between two of its own
// ports, is linked port to port instead of to a stub: the remote host
// name equals the device's own, the remote port is one of its emitted
// interfaces and a reported management address, if any, is one of its
// own addresses. Both ends of such a loop report it; the link is
// emitted once.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package iosxe

import (
	"fmt"
	"net"
	"strings"

	"go.osirisjson.org/producers/pkg/sdk"
)

// ifAbbreviations maps IOS interface-name abbreviations to the full
// type names the YANG operational models use. Longer prefixes come
// first so "Twe" is not read as "Tw".
var ifAbbreviations = []struct{ short, long string }{
	{"Twe", "TwentyFiveGigE"},
	{"Tw", "TwoGigabitEthernet"},
	{"Fi", "FiveGigabitEthernet"},
	{"Gi", "GigabitEthernet"},
	{"Te", "TenGigabitEthernet"},
	{"Fo", "FortyGigabitEthernet"},
	{"Hu", "HundredGigE"},
	{"Fa", "FastEthernet"},
	{"Po", "Port-channel"},
	{"Vl", "Vlan"},
	{"Lo", "Loopback"},
	{"Tu", "Tunnel"},
	{"Ap", "AppGigabitEthernet"},
}

// expandIfName returns name with an IOS abbreviation ("Gi1/0/1",
// "Gig 1/0/1") expanded to its full form ("GigabitEthernet1/0/1").
// Names that are already full, or not abbreviations, are returned
// unchanged apart from surrounding space.
func expandIfName(name string) string {
	name = strings.TrimSpace(name)
	letters := 0
	for letters < len(name) && (name[letters] >= 'A' && name[letters] <= 'Z' || name[letters] >= 'a' && name[letters] <= 'z' || name[letters] == '-') {
		letters++
	}
	if letters == 0 || letters == len(name) {
		return name
	}
	prefix := name[:letters]
	rest := strings.TrimSpace(name[letters:])
	if rest == "" || rest[0] < '0' || rest[0] > '9' {
		return name
	}
	for _, a := range ifAbbreviations {
		if strings.EqualFold(prefix, a.long) {
			return a.long + rest
		}
	}
	for _, a := range ifAbbreviations {
		if len(prefix) >= len(a.short) && len(prefix) < len(a.long) &&
			strings.EqualFold(prefix, a.long[:len(prefix)]) && strings.EqualFold(prefix[:len(a.short)], a.short) {
			return a.long + rest
		}
	}
	return name
}

// lookupInterface resolves an interface name reported by a protocol to
// its resource ID, trying the name as reported and then its expanded
// form.
func lookupInterface(ifNameToID map[string]string, name string) (string, string, bool) {
	name = strings.TrimSpace(name)
	if id, ok := ifNameToID[name]; ok {
		return name, id, true
	}
	full := expandIfName(name)
	if id, ok := ifNameToID[full]; ok {
		return full, id, true
	}
	return "", "", false
}

// systemHost returns a remote system name without its DNS domain,
// unless the name is an IP address.
func systemHost(name string) string {
	n := strings.TrimSpace(name)
	if net.ParseIP(n) != nil {
		return n
	}
	if host, _, ok := strings.Cut(n, "."); ok && host != "" {
		return host
	}
	return n
}

// neighborObservation is one deduplicated local-port to remote-port
// link, possibly seen by more than one protocol.
type neighborObservation struct {
	localPort, localID       string
	remoteSystem, remotePort string
	platform, mgmtAddr       string
	sources                  []string
}

// lldpObservations returns the LLDP neighbors as (local port, remote
// system, remote port, management address) tuples. The per-interface
// detail list is preferred; the summary list is used only when the
// detail list reports no neighbor. A neighbor without a system name is
// identified by its chassis ID.
func lldpObservations(l lldpEntries) [][4]string {
	var out [][4]string
	for _, intf := range l.IntfDets {
		for _, n := range intf.Neighbors {
			system := strings.TrimSpace(n.SystemName)
			if system == "" {
				system = strings.TrimSpace(n.ChassisID)
			}
			mgmt := ""
			for _, a := range n.MgmtAddrs {
				if v := strings.TrimSpace(a.Addr); v != "" {
					mgmt = v
					break
				}
			}
			out = append(out, [4]string{intf.IfName, system, n.PortID, mgmt})
		}
	}
	if len(out) > 0 {
		return out
	}
	for _, e := range l.Entries {
		out = append(out, [4]string{e.LocalInterface, e.DeviceID, e.ConnectingInterface, ""})
	}
	return out
}

// selfIdentity is what identifies the collected device in a neighbor
// observation: its own hostname and every address it owns.
type selfIdentity struct {
	hostname string
	addrs    map[string]bool
}

// newSelfIdentity builds the device's selfIdentity from its reported
// hostname, the target address and the addresses of its interfaces.
func newSelfIdentity(hostname, targetHost string, set interfaceSet) selfIdentity {
	id := selfIdentity{hostname: strings.TrimSpace(hostname), addrs: map[string]bool{}}
	if ip := net.ParseIP(strings.TrimSpace(targetHost)); ip != nil {
		id.addrs[ip.String()] = true
	}
	for _, r := range set.Resources {
		for _, k := range []string{"ipv4_addresses", "ipv6_addresses"} {
			cidrs, _ := r.Properties[k].([]string)
			for _, c := range cidrs {
				if ip, _, err := net.ParseCIDR(c); err == nil {
					id.addrs[ip.String()] = true
				}
			}
		}
	}
	return id
}

// isSelf reports whether a remote system and management address name
// the collected device.
func (s selfIdentity) isSelf(remoteSystem, mgmtAddr string) bool {
	if s.hostname == "" || !strings.EqualFold(systemHost(remoteSystem), systemHost(s.hostname)) {
		return false
	}
	if mgmtAddr == "" {
		return true
	}
	ip := net.ParseIP(mgmtAddr)
	return ip != nil && s.addrs[ip.String()]
}

// TransformNeighbors merges LLDP and CDP neighbors into
// physical.ethernet connections (retyped physical.fiber by
// ApplyLinkMedium when transceiver evidence shows an optical medium)
// from each local port to a stub network.interface resource for the
// remote port, or to the device's own port for a self-loop (see self).
// Stubs are anchored under deviceID with provider.name unknown, because
// neither protocol establishes what the remote device is. deviceName
// labels the connections.
func TransformNeighbors(deviceID, deviceName string, self selfIdentity, lldp lldpEntries, cdp cdpNeighborDetails, ifNameToID map[string]string) ([]sdk.Connection, []sdk.Resource) {
	seen := map[string]*neighborObservation{}
	var ordered []*neighborObservation

	add := func(local, system, port, platform, mgmt, source string) {
		system, port = strings.TrimSpace(system), strings.TrimSpace(port)
		if system == "" || port == "" {
			return
		}
		localName, localID, ok := lookupInterface(ifNameToID, local)
		if !ok {
			return
		}
		key := localName + "|" + strings.ToLower(systemHost(system)) + "|" + strings.ToLower(expandIfName(port))
		if obs, ok := seen[key]; ok {
			obs.sources = append(obs.sources, source)
			if len(system) > len(obs.remoteSystem) {
				obs.remoteSystem = system
			}
			if obs.mgmtAddr == "" {
				obs.mgmtAddr = strings.TrimSpace(mgmt)
			}
			if obs.platform == "" {
				obs.platform = strings.TrimSpace(platform)
			}
			return
		}
		obs := &neighborObservation{
			localPort: localName, localID: localID,
			remoteSystem: system, remotePort: port,
			platform: strings.TrimSpace(platform), mgmtAddr: strings.TrimSpace(mgmt),
			sources: []string{source},
		}
		seen[key] = obs
		ordered = append(ordered, obs)
	}

	for _, o := range lldpObservations(lldp) {
		add(o[0], o[1], o[2], "", o[3], "lldp")
	}
	for _, n := range cdp.Neighbors {
		mgmt := n.MgmtAddress
		if strings.TrimSpace(mgmt) == "" {
			mgmt = n.IPAddress
		}
		add(n.LocalIntf, n.DeviceName, n.PortID, n.Platform, mgmt, "cdp")
	}

	var connections []sdk.Connection
	var stubs []sdk.Resource
	stubSeen := map[string]bool{}
	connSeen := map[string]bool{}
	for _, obs := range ordered {
		if c, ok := selfLoop(obs, self, deviceName, ifNameToID); ok {
			if !connSeen[c.ID] {
				connSeen[c.ID] = true
				connections = append(connections, c)
			}
			continue
		}

		// The stub identity uses the host name and the expanded port
		// name, so it does not change with the protocol that reported
		// the link.
		host, port := systemHost(obs.remoteSystem), expandIfName(obs.remotePort)
		remoteID := unresolvedStubID(deviceID, "neighbor", host, port)
		if !stubSeen[remoteID] {
			stub, err := sdk.NewResource(remoteID, "network.interface", sdk.Provider{
				Name:     unknownProviderName,
				NativeID: host + "|" + port,
				Source:   strings.Join(obs.sources, "+"),
			})
			if err != nil {
				continue
			}
			stub.Name = host + ":" + port
			stub.Status = "unknown"
			props := map[string]any{
				"remote_system":  obs.remoteSystem,
				"remote_port":    obs.remotePort,
				"discovered_via": obs.sources,
			}
			if obs.platform != "" {
				props["remote_platform"] = obs.platform
			}
			if obs.mgmtAddr != "" {
				props["remote_mgmt_addr"] = obs.mgmtAddr
			}
			stub.Properties = props
			stubs = append(stubs, stub)
			stubSeen[remoteID] = true
		}

		key := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
			Type:      "physical.ethernet",
			Direction: "bidirectional",
			Source:    obs.localID,
			Target:    remoteID,
		})
		conn, err := sdk.NewConnection(sdk.BuildConnectionID(key, 16), "physical.ethernet", obs.localID, remoteID)
		if err != nil {
			continue
		}
		conn.Name = fmt.Sprintf("%s:%s <-> %s:%s", deviceName, obs.localPort, host, port)
		conn.Status = "active"
		conn.Properties = map[string]any{"discovered_via": obs.sources}
		connections = append(connections, conn)
	}
	return connections, stubs
}

// selfLoop returns the port-to-port connection for an observation of
// the device itself, with its endpoints in a fixed order so the reports
// from both ends yield the same connection.
func selfLoop(obs *neighborObservation, self selfIdentity, deviceName string, ifNameToID map[string]string) (sdk.Connection, bool) {
	if !self.isSelf(obs.remoteSystem, obs.mgmtAddr) {
		return sdk.Connection{}, false
	}
	remoteName, remoteID, ok := lookupInterface(ifNameToID, obs.remotePort)
	if !ok || remoteID == obs.localID {
		return sdk.Connection{}, false
	}
	a, b, aName, bName := obs.localID, remoteID, obs.localPort, remoteName
	if b < a {
		a, b, aName, bName = b, a, bName, aName
	}
	key := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
		Type: "physical.ethernet", Direction: "bidirectional", Source: a, Target: b,
	})
	c, err := sdk.NewConnection(sdk.BuildConnectionID(key, 16), "physical.ethernet", a, b)
	if err != nil {
		return sdk.Connection{}, false
	}
	c.Name = fmt.Sprintf("%s:%s <-> %s:%s", deviceName, aName, deviceName, bName)
	c.Status = "active"
	c.Properties = map[string]any{"discovered_via": obs.sources}
	return c, true
}

// opticalPMDPrefixes are the first letters of the IEEE 802.3 physical
// medium dependent suffix (after "BASE-") of optical PMDs: SR, SW, SX,
// LR, LRM, LW, LX, LH, ER, EW, EX, ZR, ZX, BX, FX, DR, FR. Copper PMDs
// (T, CR, CX, KR, KX) start with other letters.
var opticalPMDPrefixes = "SLEZBFD"

// opticalPMD reports whether an ethernet-pmd value, as transceiver-oper
// reports it (e.g. "10GBASE-SR", "ETH_10GBASE_LR"), names an optical
// medium. An unrecognized value is not optical.
func opticalPMD(pmd string) bool {
	p := strings.ToUpper(strings.TrimSpace(pmd))
	i := strings.Index(p, "BASE")
	if i < 0 || i+5 > len(p) || (p[i+4] != '-' && p[i+4] != '_') {
		return false
	}
	return strings.IndexByte(opticalPMDPrefixes, p[i+5]) >= 0
}

// portPMD returns the ethernet_pmd of the transceiver on a port
// resource, or "".
func portPMD(r sdk.Resource) string {
	x, _ := r.Properties["transceiver"].(map[string]any)
	pmd, _ := x["ethernet_pmd"].(string)
	return pmd
}

// ApplyLinkMedium retypes a neighbor link from physical.ethernet to
// physical.fiber when an endpoint port carries a transceiver whose
// reported ethernet_pmd is optical and records that PMD on the
// connection as the evidence. LLDP and CDP establish adjacency, not the
// medium, so a link without such transceiver evidence keeps
// physical.ethernet.
func ApplyLinkMedium(conns []sdk.Connection, ports []sdk.Resource) {
	pmds := make(map[string]string, len(ports))
	for _, r := range ports {
		if pmd := portPMD(r); opticalPMD(pmd) {
			pmds[r.ID] = pmd
		}
	}
	for i, c := range conns {
		if c.Type != "physical.ethernet" {
			continue
		}
		pmd := pmds[c.Source]
		if pmd == "" {
			pmd = pmds[c.Target]
		}
		if pmd == "" {
			continue
		}
		key := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
			Type: "physical.fiber", Direction: "bidirectional", Source: c.Source, Target: c.Target,
		})
		fiber, err := sdk.NewConnection(sdk.BuildConnectionID(key, 16), "physical.fiber", c.Source, c.Target)
		if err != nil {
			continue
		}
		fiber.Name, fiber.Status, fiber.Direction = c.Name, c.Status, c.Direction
		props := map[string]any{}
		for k, v := range c.Properties {
			props[k] = v
		}
		props["ethernet_pmd"] = pmd
		fiber.Properties = props
		conns[i] = fiber
	}
}
