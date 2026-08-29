// transform_interfaces.go - Interface resources and their enrichment:
// "show interface brief" -> network.switch.port/network.interface[.lag]
// resources, "show interface"/"show ip interface brief vrf all"
// "show interface switchport" enrichment, and the switch "contains"
// its own ports.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package nxos

import (
	"fmt"
	"strconv"
	"strings"

	"go.osirisjson.org/producers/pkg/sdk"
)

// TransformInterfaces converts "show interface brief" output into
// interface resources. deviceKey is the owning switch's own canonical
// identity (see deviceNativeKey) every interface's ID is derived from
// it instead of the raw hostname, so a target alias change does not
// mint new port IDs for the same physical hardware.
// Returns resources and a map of interface name -> resource ID.
func TransformInterfaces(deviceKey string, ifBrief interfaceBriefResponse) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	nameToID := make(map[string]string)

	for _, row := range ifBrief.TableInterface.RowInterface {
		ifName := string(row.Interface)
		if ifName == "" {
			continue
		}

		resType := classifyInterfaceType(ifName)
		canonicalKey := fmt.Sprintf("%s/%s", deviceKey, ifName)
		id := resourceID(providerName, canonicalKey)
		nameToID[ifName] = id

		prov := sdk.Provider{
			Name:     providerName,
			NativeID: canonicalKey,
		}

		r, err := sdk.NewResource(id, resType, prov)
		if err != nil {
			continue
		}
		r.Name = ifName
		// VLAN SVI rows report only svi_admin_state, never state
		// confirmed against a real device, where every SVI (including
		// ones carrying live OSPF adjacencies) previously fell through
		// to an "unknown" status because only State was read.
		state := string(row.State)
		if state == "" {
			state = string(row.SVIAdminState)
		}
		r.Status = mapInterfaceStatus(state)

		props := map[string]any{}
		// interface_name is a network.switch.port-specific property
		// (OSIRIS-JSON-v1.0 7.5.2); the logical bucket's own type,
		// network.interface (7.5.3), does not list it.
		if resType == "network.switch.port" {
			props["interface_name"] = ifName
		}
		// speed_mbps: the brief-mode speed field is Mbps-valued when
		// numeric (e.g. "100000" for 100G). A symbolic value such as
		// "auto" is left unset rather than guessed at a number.
		if v := string(row.Speed); v != "" {
			if mbps, err := strconv.Atoi(v); err == nil {
				props["speed_mbps"] = mbps
			}
		}
		if v := string(row.PortMode); v != "" {
			props["port_mode"] = v
		}
		// admin_status: brief mode reports one up/down field per
		// interface class "state" for Ethernet-family ports,
		// "svi_admin_state" for VLAN SVIs (Cisco's own name for that
		// field already says "admin state", so no semantic stretch).
		// There is no separate operational-status field at this
		// verbosity; a prior "oper_status" property sourced from a
		// "status" key that does not exist in any real capture was
		// removed rather than left permanently empty.
		if state != "" {
			props["admin_status"] = state
		}
		// vlan is a JSON integer per the spec's property table; "--"
		// and other non-numeric brief-mode values are omitted rather
		// than coerced.
		if v := string(row.VLAN); v != "" {
			if vlan, err := strconv.Atoi(v); err == nil {
				props["vlan"] = vlan
			}
		}
		if len(props) > 0 {
			r.Properties = props
		}

		resources = append(resources, r)
	}

	return resources, nameToID
}

// classifyInterfaceType determines the OSIRIS type for an interface by
// name:
//
//   - network.switch.port (OSIRIS-JSON-v1.0 7.5.2) for physical ports
//     with a real transceiver slot on the chassis data Ethernet
//     interfaces and the mgmt0 out-of-band port;
//   - network.interface.lag (7.5.3 with the 4.2.3 extended-hierarchy
//     ".<variant>" suffix) for a port-channel bundle a distinct
//     resource that aggregates member ports, worth surfacing as a
//     type so a consumer can select LAGs without string-matching the
//     name. Shared across every Cisco producer in this repository;
//   - network.interface (7.5.3) for everything else logical with no
//     physical slot of its own (loopback, Vlan SVI, tunnel).
func classifyInterfaceType(ifName string) string {
	lower := strings.ToLower(ifName)
	switch {
	case strings.HasPrefix(lower, "ethernet"), strings.HasPrefix(lower, "mgmt"):
		return "network.switch.port"
	case strings.HasPrefix(lower, "port-channel"):
		return "network.interface.lag"
	default:
		return "network.interface"
	}
}

// TransformDeviceContainment builds "contains" connections from the
// switch resource to every interface resource TransformInterfaces
// produced for it only ifResources (this device's own interfaces),
// never LLDP stub resources, which represent a remote neighbor's port,
// not this switch's own. Two subtypes distinguish chassis-physical
// containment from logical/administrative containment, following the
// dot-notation extension pattern OSIRIS-JSON-v1.0 section 5.2.3 uses
// for its other standard connection types (contains itself has no
// spec-enumerated subtype list):
//
//   - contains.physical: network.switch.port resources (a real
//     transceiver slot on the chassis).
//   - contains.logical: network.interface resources (port-channel/LAG,
//     loopback, Vlan SVI - no physical slot of their own).
func TransformDeviceContainment(deviceID, deviceName string, ifResources []sdk.Resource) []sdk.Connection {
	var connections []sdk.Connection

	for _, r := range ifResources {
		var connType string
		switch r.Type {
		case "network.switch.port":
			connType = "contains.physical"
		case "network.interface", "network.interface.lag":
			connType = "contains.logical"
		default:
			continue
		}

		canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
			Type:      connType,
			Direction: "forward",
			Source:    deviceID,
			Target:    r.ID,
		})
		connID := sdk.BuildConnectionID(canonicalKey, 16)
		conn, err := sdk.NewConnection(connID, connType, deviceID, r.ID)
		if err != nil {
			continue
		}
		conn.Name = fmt.Sprintf("%s contains %s", deviceName, r.Name)
		conn.Direction = "forward"
		conn.Status = "active"

		connections = append(connections, conn)
	}

	return connections
}

// EnrichInterfaceDetails mutates interface resources in-place with
// detailed information from "show interface" (full output).
func EnrichInterfaceDetails(hostname string, ifDetail interfaceDetailResponse, resources []sdk.Resource, ifNameToID map[string]string) {
	// Build reverse map: resource ID -> index in resources.
	resIdx := make(map[string]int, len(resources))
	for i, r := range resources {
		resIdx[r.ID] = i
	}

	for _, row := range ifDetail.TableInterface.RowInterface {
		ifName := string(row.Interface)
		if ifName == "" {
			continue
		}
		resID, ok := ifNameToID[ifName]
		if !ok {
			continue
		}
		ri, ok := resIdx[resID]
		if !ok {
			continue
		}

		if resources[ri].Properties == nil {
			resources[ri].Properties = make(map[string]any)
		}
		props := resources[ri].Properties

		if v := int64(row.MTU); v > 0 {
			props["mtu"] = v
		}
		if v := int64(row.Bandwidth); v > 0 {
			props["bandwidth"] = v
		}
		if v := string(row.Duplex); v != "" {
			props["duplex"] = v
		}
		if v := string(row.HWAddr); v != "" {
			props["mac_address"] = sdk.NormalizeMAC(v)
		}
		if v := string(row.Desc); v != "" {
			props["description"] = v
		}

		// Counters.
		if v := int64(row.OutBytes); v > 0 {
			props["tx_bytes"] = v
		}
		if v := int64(row.InBytes); v > 0 {
			props["rx_bytes"] = v
		}
		if v := int64(row.OutPkts); v > 0 {
			props["tx_packets"] = v
		}
		if v := int64(row.InPkts); v > 0 {
			props["rx_packets"] = v
		}
	}
}

// EnrichInterfaceIPs adds ip_address to any interface resource "show
// ip interface brief vrf all" reports an address for a bare IP, not
// a CIDR: the brief-mode command reports no subnet mask, so no
// prefix/subnet is emitted here rather than guessed at a length. Full
// prefix mapping needs "show ip interface" (not brief), not
// implemented yet.
func EnrichInterfaceIPs(ipBrief ipInterfaceBriefResponse, resources []sdk.Resource, ifNameToID map[string]string) {
	resIdx := make(map[string]int, len(resources))
	for i, r := range resources {
		resIdx[r.ID] = i
	}

	for _, row := range ipBrief.TableIntf.RowIntf {
		ifName := normalizeIfName(string(row.IntfName))
		addr := string(row.Prefix)
		if ifName == "" || addr == "" {
			continue
		}
		resID, ok := ifNameToID[ifName]
		if !ok {
			continue
		}
		ri, ok := resIdx[resID]
		if !ok {
			continue
		}
		if resources[ri].Properties == nil {
			resources[ri].Properties = make(map[string]any)
		}
		resources[ri].Properties["ip_address"] = addr
	}
}

// EnrichSwitchportDetails mutates network.switch.port resources
// in-place with native_vlan (trunk mode's untagged VLAN) from "show
// interface switchport" the trunk/access VLAN context "portmode"
// alone (already surfaced by TransformInterfaces) cannot provide.
// Spanning-tree state and PoE are not implemented this pass each
// needs its own additional command ("show spanning-tree interface",
// "show power inline") with no capture or established convention to
// ground field names against at all.
func EnrichSwitchportDetails(switchport switchportResponse, resources []sdk.Resource, ifNameToID map[string]string) {
	resIdx := make(map[string]int, len(resources))
	for i, r := range resources {
		resIdx[r.ID] = i
	}

	for _, row := range switchport.TableInterface.RowInterface {
		ifName := normalizeIfName(string(row.Interface))
		nativeVLAN := string(row.NativeVLAN)
		if ifName == "" || nativeVLAN == "" {
			continue
		}
		resID, ok := ifNameToID[ifName]
		if !ok {
			continue
		}
		ri, ok := resIdx[resID]
		if !ok {
			continue
		}
		vlan, err := strconv.Atoi(nativeVLAN)
		if err != nil {
			continue
		}
		if resources[ri].Properties == nil {
			resources[ri].Properties = make(map[string]any)
		}
		resources[ri].Properties["native_vlan"] = vlan
	}
}
