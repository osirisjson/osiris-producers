// transform.go - Pure XML->OSIRIS mapping functions for Cisco IOS-XE.
// Converts NETCONF XML responses into SDK types. All functions are stateless:
// no I/O, no SSH, just data transformation via encoding/xml.
//
// For an introduction to OSIRIS JSON Producer for Cisco see:
// "[OSIRIS-JSON-CISCO]."
//
// [OSIRIS-JSON-CISCO]: https://osirisjson.org/en/docs/producers/network/cisco

package iosxe

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"

	"go.osirisjson.org/producers/pkg/sdk"
)

const extensionNamespace = "osiris.cisco"
const providerName = "cisco"

// XML structs.

// ietfInterfaces maps ietf-interfaces:interfaces.
type ietfInterfaces struct {
	XMLName    xml.Name    `xml:"interfaces"`
	Interfaces []ietfIface `xml:"interface"`
}

type ietfIface struct {
	Name        string        `xml:"name"`
	Type        string        `xml:"type"`
	Enabled     string        `xml:"enabled"`
	Description string        `xml:"description"`
	Statistics  *ifStatistics `xml:"statistics"`
	IPv4        *ifIPv4       `xml:"ipv4"`
	Speed       string        `xml:"speed"`
	MTU         string        `xml:"mtu"`
	AdminStatus string        `xml:"admin-status"`
	OperStatus  string        `xml:"oper-status"`
	PhysAddress string        `xml:"phys-address"`
}

type ifStatistics struct {
	InOctets  string `xml:"in-octets"`
	OutOctets string `xml:"out-octets"`
	InErrors  string `xml:"in-errors"`
	OutErrors string `xml:"out-errors"`
}

type ifIPv4 struct {
	Address []ifIPv4Addr `xml:"address"`
}

type ifIPv4Addr struct {
	IP     string `xml:"ip"`
	Prefix string `xml:"netmask"`
}

// hwInventory maps Cisco-IOS-XE-device-hardware-oper data.
type hwInventory struct {
	XMLName    xml.Name      `xml:"device-hardware-data"`
	Components []hwComponent `xml:"device-hardware>device-inventory"`
}

type hwComponent struct {
	Name         string `xml:"hw-type"`
	Description  string `xml:"hw-description"`
	PartNumber   string `xml:"part-number"`
	SerialNumber string `xml:"serial-number"`
	DevName      string `xml:"dev-name"`
}

// nativeConfig maps Cisco-IOS-XE-native:native for version/hostname.
type nativeConfig struct {
	XMLName  xml.Name `xml:"native"`
	Version  string   `xml:"version"`
	Hostname string   `xml:"hostname"`
}

// nativeVRFs maps Cisco-IOS-XE-native:native/vrf/definition.
type nativeVRFList struct {
	XMLName xml.Name `xml:"native"`
	VRF     struct {
		Definitions []vrfDef `xml:"definition"`
	} `xml:"vrf"`
}

type vrfDef struct {
	Name string `xml:"name"`
	RD   string `xml:"rd"`
	Desc string `xml:"description"`
	AF   *vrfAF `xml:"address-family"`
}

type vrfAF struct {
	IPv4RT string `xml:"ipv4>route-target>export"`
	IPv6RT string `xml:"ipv6>route-target>export"`
}

// cdpNeighborDetails maps Cisco-IOS-XE-cdp-oper data.
type cdpNeighborDetails struct {
	XMLName   xml.Name      `xml:"cdp-neighbor-details"`
	Neighbors []cdpNeighbor `xml:"cdp-neighbor-detail"`
}

type cdpNeighbor struct {
	DeviceName  string `xml:"device-name"`
	LocalIntf   string `xml:"local-intf-name"`
	PortID      string `xml:"port-id"`
	Platform    string `xml:"platform-name"`
	MgmtAddress string `xml:"mgmt-address"`
}

// bgpStateData maps Cisco-IOS-XE-bgp-oper data.
type bgpStateData struct {
	XMLName   xml.Name `xml:"bgp-state-data"`
	Neighbors struct {
		Entries []bgpNeighbor `xml:"neighbor"`
	} `xml:"neighbors"`
}

type bgpNeighbor struct {
	ID           string `xml:"afi-safi>afi-safi-name"`
	NeighborID   string `xml:"neighbor-id"`
	VRFName      string `xml:"vrf-name"`
	AS           string `xml:"as"`
	State        string `xml:"connection>state"`
	PrefixesRecv string `xml:"prefix-activity>received>current-prefixes"`
}

// ospfOperData maps Cisco-IOS-XE-ospf-oper data.
type ospfOperData struct {
	XMLName   xml.Name      `xml:"ospf-oper-data"`
	Instances []ospfProcess `xml:"ospf-state>ospf-instance"`
}

type ospfProcess struct {
	ProcessID string         `xml:"process-id"`
	RouterID  string         `xml:"router-id"`
	Neighbors []ospfNeighbor `xml:"ospf-neighbor"`
}

type ospfNeighbor struct {
	NeighborID string `xml:"neighbor-id"`
	Address    string `xml:"address"`
	State      string `xml:"state"`
	DR         string `xml:"dr"`
}

// cpuUsage maps Cisco-IOS-XE-process-cpu-oper data.
type cpuUsage struct {
	XMLName      xml.Name `xml:"cpu-usage"`
	CPUUtilStats struct {
		OneMinute string `xml:"one-minute"`
		FiveMin   string `xml:"five-minutes"`
	} `xml:"cpu-utilization"`
}

// memoryStats maps Cisco-IOS-XE-memory-oper data.
type memoryStats struct {
	XMLName xml.Name `xml:"memory-statistics"`
	Stats   []struct {
		Name  string `xml:"name"`
		Total string `xml:"total-memory"`
		Used  string `xml:"used-memory"`
		Free  string `xml:"free-memory"`
	} `xml:"memory-statistic"`
}

// Resource Transforms.

// TransformDevice converts native config XML + hardware XML into a network.router resource.
func TransformDevice(hostname string, nativeXML []byte, hwXML []byte) (sdk.Resource, string) {
	var native nativeConfig
	xml.Unmarshal(nativeXML, &native)

	version := native.Version
	if version == "" {
		version = "unknown"
	}
	deviceHostname := native.Hostname
	if deviceHostname == "" {
		deviceHostname = hostname
	}

	// Extract serial from hardware inventory (first chassis component).
	serial, model := extractChassisInfo(hwXML)

	resType := "network.router"
	canonicalKey := hostname
	id := resourceID(resType, canonicalKey)

	prov := sdk.Provider{
		Name:     providerName,
		NativeID: hostname,
		Type:     model,
		Version:  version,
	}

	r, err := sdk.NewResource(id, resType, prov)
	if err != nil {
		return sdk.Resource{}, ""
	}
	r.Name = hostname
	r.Status = "active"

	props := map[string]any{
		"hostname": deviceHostname,
		"version":  version,
	}
	if serial != "" {
		props["serial"] = serial
	}
	if model != "" {
		props["model"] = model
	}

	ext := make(map[string]any)
	if version != "" {
		ext["boot_image"] = "bootflash:packages.conf"
	}

	if len(ext) > 0 {
		r.Extensions = map[string]any{extensionNamespace: ext}
	}
	r.Properties = props
	return r, id
}

// extractChassisInfo pulls serial and model from hardware inventory XML.
func extractChassisInfo(hwXML []byte) (serial, model string) {
	if len(hwXML) == 0 {
		return "", ""
	}
	var hw hwInventory
	if err := xml.Unmarshal(hwXML, &hw); err != nil {
		// Try wrapping in root element.
		wrapped := append([]byte("<device-hardware-data>"), hwXML...)
		wrapped = append(wrapped, []byte("</device-hardware-data>")...)
		xml.Unmarshal(wrapped, &hw)
	}
	for _, c := range hw.Components {
		if strings.Contains(strings.ToLower(c.HwType()), "chassis") ||
			strings.Contains(strings.ToLower(c.DevName), "chassis") ||
			c.SerialNumber != "" && serial == "" {
			if c.SerialNumber != "" {
				serial = c.SerialNumber
			}
			if c.PartNumber != "" {
				model = c.PartNumber
			}
			if serial != "" && model != "" {
				return
			}
		}
	}
	return serial, model
}

// HwType returns the hw-type or dev-name for classification.
func (c hwComponent) HwType() string {
	if c.Name != "" {
		return c.Name
	}
	return c.DevName
}

// TransformInterfaces converts ietf-interfaces XML into interface resources.
// Returns resources and a map of interface name -> resource ID.
func TransformInterfaces(hostname string, ifXML []byte) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	nameToID := make(map[string]string)

	if len(ifXML) == 0 {
		return resources, nameToID
	}

	var ifaces ietfInterfaces
	if err := xml.Unmarshal(ifXML, &ifaces); err != nil {
		// Try wrapping.
		wrapped := append([]byte(`<interfaces xmlns="urn:ietf:params:xml:ns:yang:ietf-interfaces">`), ifXML...)
		wrapped = append(wrapped, []byte("</interfaces>")...)
		xml.Unmarshal(wrapped, &ifaces)
	}

	for _, iface := range ifaces.Interfaces {
		if iface.Name == "" {
			continue
		}

		resType := classifyInterfaceType(iface.Name)
		canonicalKey := fmt.Sprintf("%s|%s", hostname, iface.Name)
		id := resourceID(resType, canonicalKey)
		nameToID[iface.Name] = id

		prov := sdk.Provider{
			Name:     providerName,
			NativeID: canonicalKey,
		}

		r, err := sdk.NewResource(id, resType, prov)
		if err != nil {
			continue
		}
		r.Name = iface.Name
		r.Status = mapInterfaceStatus(iface.OperStatus)

		props := map[string]any{}
		if iface.AdminStatus != "" {
			props["admin_status"] = iface.AdminStatus
		}
		if iface.OperStatus != "" {
			props["oper_status"] = iface.OperStatus
		}
		if iface.Description != "" {
			props["description"] = iface.Description
		}
		if iface.Speed != "" {
			props["speed"] = iface.Speed
		}
		if iface.MTU != "" {
			if n, err := strconv.ParseInt(iface.MTU, 10, 64); err == nil {
				props["mtu"] = n
			}
		}
		if iface.PhysAddress != "" {
			props["mac_address"] = sdk.NormalizeMAC(iface.PhysAddress)
		}
		if iface.IPv4 != nil && len(iface.IPv4.Address) > 0 {
			props["ip_address"] = iface.IPv4.Address[0].IP
			if iface.IPv4.Address[0].Prefix != "" {
				props["netmask"] = iface.IPv4.Address[0].Prefix
			}
		}
		if iface.Enabled != "" {
			props["enabled"] = iface.Enabled == "true"
		}

		// Subinterface parent linkage.
		if parent := parentInterface(iface.Name); parent != "" {
			props["parent_interface"] = parent
		}

		if len(props) > 0 {
			r.Properties = props
		}
		resources = append(resources, r)
	}

	return resources, nameToID
}

// TransformInventory converts hardware inventory XML into an inventory array
// for the device's cisco extension.
func TransformInventory(hwXML []byte) []map[string]any {
	if len(hwXML) == 0 {
		return nil
	}

	var hw hwInventory
	if err := xml.Unmarshal(hwXML, &hw); err != nil {
		wrapped := append([]byte("<device-hardware-data>"), hwXML...)
		wrapped = append(wrapped, []byte("</device-hardware-data>")...)
		xml.Unmarshal(wrapped, &hw)
	}

	var items []map[string]any
	for _, c := range hw.Components {
		item := map[string]any{}
		name := c.DevName
		if name == "" {
			name = c.Name
		}
		if name != "" {
			item["name"] = name
		}
		if c.Description != "" {
			item["description"] = c.Description
		}
		if c.PartNumber != "" {
			item["part_number"] = c.PartNumber
		}
		if c.SerialNumber != "" {
			item["serial"] = c.SerialNumber
		}
		if len(item) > 0 {
			items = append(items, item)
		}
	}
	return items
}

// Connection Transforms.

// TransformCDPNeighbors converts CDP neighbor XML into network.link connections
// and stub network.interface resources for remote endpoints.
func TransformCDPNeighbors(hostname string, cdpXML []byte, ifNameToID map[string]string) ([]sdk.Connection, []sdk.Resource) {
	var connections []sdk.Connection
	var stubs []sdk.Resource

	if len(cdpXML) == 0 {
		return connections, stubs
	}

	var cdp cdpNeighborDetails
	if err := xml.Unmarshal(cdpXML, &cdp); err != nil {
		wrapped := append([]byte(`<cdp-neighbor-details>`), cdpXML...)
		wrapped = append(wrapped, []byte("</cdp-neighbor-details>")...)
		xml.Unmarshal(wrapped, &cdp)
	}

	for _, n := range cdp.Neighbors {
		localPort := n.LocalIntf
		remoteSystem := n.DeviceName
		remotePort := n.PortID

		if localPort == "" || remoteSystem == "" || remotePort == "" {
			continue
		}

		// Local interface must exist.
		localID, ok := ifNameToID[localPort]
		if !ok {
			continue
		}

		// Create stub resource for remote interface.
		remoteCanonical := fmt.Sprintf("%s|%s", remoteSystem, remotePort)
		remoteID := resourceID("network.interface", remoteCanonical)

		remoteProv := sdk.Provider{
			Name:     providerName,
			NativeID: remoteCanonical,
		}
		stub, err := sdk.NewResource(remoteID, "network.interface", remoteProv)
		if err != nil {
			continue
		}
		stub.Name = fmt.Sprintf("%s:%s", remoteSystem, remotePort)
		stub.Status = "unknown"

		props := map[string]any{
			"remote_system": remoteSystem,
			"remote_port":   remotePort,
		}
		if n.Platform != "" {
			props["remote_platform"] = n.Platform
		}
		if n.MgmtAddress != "" {
			props["remote_mgmt_addr"] = n.MgmtAddress
		}
		stub.Properties = props
		stubs = append(stubs, stub)

		// Create connection.
		connInput := sdk.ConnectionIDInput{
			Type:      "physical.ethernet",
			Direction: "bidirectional",
			Source:    localID,
			Target:    remoteID,
		}
		connKey := sdk.ConnectionCanonicalKey(connInput)
		connID := sdk.BuildConnectionID(connKey, 16)

		conn, err := sdk.NewConnection(connID, "physical.ethernet", localID, remoteID)
		if err != nil {
			continue
		}
		conn.Name = fmt.Sprintf("%s:%s <-> %s:%s", hostname, localPort, remoteSystem, remotePort)
		conn.Status = "active"

		connections = append(connections, conn)
	}

	return connections, stubs
}

// Group Transforms.

// TransformVRFs converts native VRF config XML into logical.vrf groups.
// Returns groups and a map of VRF name -> group ID.
func TransformVRFs(hostname string, vrfXML []byte) ([]sdk.Group, map[string]string) {
	var groups []sdk.Group
	vrfNameToGroupID := make(map[string]string)

	if len(vrfXML) == 0 {
		return groups, vrfNameToGroupID
	}

	var native nativeVRFList
	if err := xml.Unmarshal(vrfXML, &native); err != nil {
		wrapped := append([]byte(`<native xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-native">`), vrfXML...)
		wrapped = append(wrapped, []byte("</native>")...)
		xml.Unmarshal(wrapped, &native)
	}

	for _, vrf := range native.VRF.Definitions {
		if vrf.Name == "" {
			continue
		}

		boundaryToken := fmt.Sprintf("%s|vrf-%s", hostname, vrf.Name)
		gid := sdk.GroupID(sdk.GroupIDInput{
			Type:          "logical.vrf",
			BoundaryToken: boundaryToken,
		})
		vrfNameToGroupID[vrf.Name] = gid

		g, err := sdk.NewGroup(gid, "logical.vrf")
		if err != nil {
			continue
		}
		g.Name = vrf.Name

		props := map[string]any{}
		if vrf.RD != "" {
			props["route_distinguisher"] = vrf.RD
		}
		if vrf.Desc != "" {
			props["description"] = vrf.Desc
		}
		if vrf.AF != nil && vrf.AF.IPv4RT != "" {
			props["route_target_export"] = vrf.AF.IPv4RT
		}
		if len(props) > 0 {
			g.Properties = props
		}

		groups = append(groups, g)
	}

	return groups, vrfNameToGroupID
}

// Wiring Functions.

// WireInterfacesToVRFs adds interface resource IDs as members of their VRF groups.
// This is based on interface names containing the VRF name in IOS-XE configuration.
// In practice, we wire based on VRF definition interface lists from the config.
func WireInterfacesToVRFs(vrfXML []byte, ifNameToID map[string]string, groups []sdk.Group, nameToGroupID map[string]string) {
	// IOS-XE VRF definitions don't carry interface lists directly in the native model
	// the same way NX-OS does. We parse the VRF XML and match interfaces by name
	// convention or by a secondary query. For now, we support explicit interface
	// references if present in the VRF definition.
	if len(vrfXML) == 0 {
		return
	}

	idx := groupIndex(groups)

	// Parse a lightweight struct that includes interface references.
	type vrfWithIfaces struct {
		Name       string   `xml:"name"`
		Interfaces []string `xml:"interface"`
	}
	type vrfContainer struct {
		VRF struct {
			Defs []vrfWithIfaces `xml:"definition"`
		} `xml:"vrf"`
	}

	var native vrfContainer
	if err := xml.Unmarshal(vrfXML, &native); err != nil {
		wrapped := append([]byte(`<native xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-native">`), vrfXML...)
		wrapped = append(wrapped, []byte("</native>")...)
		xml.Unmarshal(wrapped, &native)
	}

	for _, vrf := range native.VRF.Defs {
		gid, ok := nameToGroupID[vrf.Name]
		if !ok {
			continue
		}
		gi, ok := idx[gid]
		if !ok {
			continue
		}

		for _, ifName := range vrf.Interfaces {
			ifName = strings.TrimSpace(ifName)
			if resID, ok := ifNameToID[ifName]; ok {
				groups[gi].AddMembers(resID)
			}
		}
	}
}

// Detail-only Enrichment.

// EnrichInterfaceCounters mutates interface resources in-place with counter statistics
// from ietf-interfaces statistics elements.
func EnrichInterfaceCounters(ifXML []byte, resources []sdk.Resource, ifNameToID map[string]string) {
	if len(ifXML) == 0 {
		return
	}

	var ifaces ietfInterfaces
	if err := xml.Unmarshal(ifXML, &ifaces); err != nil {
		wrapped := append([]byte(`<interfaces xmlns="urn:ietf:params:xml:ns:yang:ietf-interfaces">`), ifXML...)
		wrapped = append(wrapped, []byte("</interfaces>")...)
		xml.Unmarshal(wrapped, &ifaces)
	}

	// Build reverse map: resource ID -> index in resources.
	resIdx := make(map[string]int, len(resources))
	for i, r := range resources {
		resIdx[r.ID] = i
	}

	for _, iface := range ifaces.Interfaces {
		if iface.Name == "" || iface.Statistics == nil {
			continue
		}
		resID, ok := ifNameToID[iface.Name]
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

		if v := iface.Statistics.InOctets; v != "" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				props["rx_bytes"] = n
			}
		}
		if v := iface.Statistics.OutOctets; v != "" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				props["tx_bytes"] = n
			}
		}
		if v := iface.Statistics.InErrors; v != "" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
				props["rx_errors"] = n
			}
		}
		if v := iface.Statistics.OutErrors; v != "" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
				props["tx_errors"] = n
			}
		}
	}
}

// TransformCPUMemory extracts CPU utilization and memory stats into cisco extension fields.
func TransformCPUMemory(cpuXML, memXML []byte) map[string]any {
	ext := make(map[string]any)

	if len(cpuXML) > 0 {
		var cpu cpuUsage
		if err := xml.Unmarshal(cpuXML, &cpu); err == nil {
			if v := cpu.CPUUtilStats.OneMinute; v != "" {
				if f, err := strconv.ParseFloat(v, 64); err == nil {
					ext["cpu_utilization_1min"] = f
				}
			}
			if v := cpu.CPUUtilStats.FiveMin; v != "" {
				if f, err := strconv.ParseFloat(v, 64); err == nil {
					ext["cpu_utilization_5min"] = f
				}
			}
		}
	}

	if len(memXML) > 0 {
		var mem memoryStats
		if err := xml.Unmarshal(memXML, &mem); err == nil {
			// Use the "Processor" pool if available.
			for _, s := range mem.Stats {
				if strings.EqualFold(s.Name, "Processor") || len(mem.Stats) == 1 {
					if v, err := strconv.ParseInt(s.Used, 10, 64); err == nil {
						ext["memory_used"] = v
					}
					if v, err := strconv.ParseInt(s.Free, 10, 64); err == nil {
						ext["memory_free"] = v
					}
					break
				}
			}
		}
	}

	return ext
}

// TransformBGPNeighbors extracts BGP neighbor data into a cisco extension array.
func TransformBGPNeighbors(bgpXML []byte) []map[string]any {
	if len(bgpXML) == 0 {
		return nil
	}

	var bgp bgpStateData
	if err := xml.Unmarshal(bgpXML, &bgp); err != nil {
		wrapped := append([]byte(`<bgp-state-data>`), bgpXML...)
		wrapped = append(wrapped, []byte("</bgp-state-data>")...)
		xml.Unmarshal(wrapped, &bgp)
	}

	var neighbors []map[string]any
	for _, n := range bgp.Neighbors.Entries {
		entry := map[string]any{}
		if n.NeighborID != "" {
			entry["neighbor_id"] = n.NeighborID
		}
		if n.AS != "" {
			entry["remote_as"] = n.AS
		}
		if n.VRFName != "" {
			entry["vrf"] = n.VRFName
		}
		if n.State != "" {
			entry["state"] = n.State
		}
		if n.PrefixesRecv != "" {
			entry["prefixes_received"] = n.PrefixesRecv
		}
		if len(entry) > 0 {
			neighbors = append(neighbors, entry)
		}
	}
	return neighbors
}

// TransformOSPF extracts OSPF process data into a cisco extension array.
func TransformOSPF(ospfXML []byte) []map[string]any {
	if len(ospfXML) == 0 {
		return nil
	}

	var ospf ospfOperData
	if err := xml.Unmarshal(ospfXML, &ospf); err != nil {
		wrapped := append([]byte(`<ospf-oper-data>`), ospfXML...)
		wrapped = append(wrapped, []byte("</ospf-oper-data>")...)
		xml.Unmarshal(wrapped, &ospf)
	}

	var processes []map[string]any
	for _, p := range ospf.Instances {
		proc := map[string]any{}
		if p.ProcessID != "" {
			proc["process_id"] = p.ProcessID
		}
		if p.RouterID != "" {
			proc["router_id"] = p.RouterID
		}
		if len(p.Neighbors) > 0 {
			var nbrs []map[string]any
			for _, n := range p.Neighbors {
				nbr := map[string]any{}
				if n.NeighborID != "" {
					nbr["neighbor_id"] = n.NeighborID
				}
				if n.Address != "" {
					nbr["address"] = n.Address
				}
				if n.State != "" {
					nbr["state"] = n.State
				}
				if len(nbr) > 0 {
					nbrs = append(nbrs, nbr)
				}
			}
			if len(nbrs) > 0 {
				proc["neighbors"] = nbrs
			}
		}
		if len(proc) > 0 {
			processes = append(processes, proc)
		}
	}
	return processes
}

// Helper functions

// resourceID generates a deterministic resource ID from type and canonical suffix.
func resourceID(resType, canonicalSuffix string) string {
	canonicalKey := fmt.Sprintf("v1|%s|%s", resType, canonicalSuffix)
	hash := sdk.Hash16(canonicalKey)
	hint := sdk.DeriveHint(canonicalSuffix, hash)
	return fmt.Sprintf("res-%s-%s-%s", resType, hint, hash)
}

// groupIndex builds a map of group ID -> index in slice for efficient mutation.
func groupIndex(groups []sdk.Group) map[string]int {
	idx := make(map[string]int, len(groups))
	for i, g := range groups {
		idx[g.ID] = i
	}
	return idx
}

// classifyInterfaceType determines the OSIRIS type for an interface by name.
func classifyInterfaceType(ifName string) string {
	lower := strings.ToLower(ifName)
	if strings.HasPrefix(lower, "port-channel") {
		return "osiris.cisco.interface.lag"
	}
	return "network.interface"
}

// mapInterfaceStatus converts IOS-XE interface oper-status to OSIRIS status values.
func mapInterfaceStatus(status string) string {
	switch strings.ToLower(status) {
	case "up":
		return "active"
	case "down":
		return "inactive"
	default:
		return "unknown"
	}
}

// parentInterface extracts the parent interface name from a subinterface name.
// E.g., "GigabitEthernet0/0/0.100" -> "GigabitEthernet0/0/0".
// Returns empty string if not a subinterface.
func parentInterface(name string) string {
	dotIdx := strings.LastIndex(name, ".")
	if dotIdx == -1 {
		return ""
	}
	// Verify the part after the dot is numeric (subinterface ID).
	sub := name[dotIdx+1:]
	if _, err := strconv.Atoi(sub); err != nil {
		return ""
	}
	return name[:dotIdx]
}

// ensureCiscoExtension ensures the extensions map and osiris.cisco sub-map exist.
func ensureCiscoExtension(ext *map[string]any) {
	if *ext == nil {
		*ext = make(map[string]any)
	}
	if _, ok := (*ext)[extensionNamespace]; !ok {
		(*ext)[extensionNamespace] = make(map[string]any)
	}
}
