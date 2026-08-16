// transform.go - Pure NX-OS to OSIRIS mapping functions.
// Converts typed NX-API response DTOs (see dto.go) into OSIRIS types.
// All functions are stateless.
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

const extensionNamespace = "osiris.cisco"
const providerName = "cisco"

// TransformDevice converts "show version" output into a single
// network.switch resource.
func TransformDevice(hostname string, version versionResponse) (sdk.Resource, string) {
	model := string(version.ChassisID)
	serial := string(version.ProcBoardID)
	swVersion := string(version.SysVerStr)

	role := classifyRole(hostname, model)
	resType := "network.switch"
	if role == "spine" {
		resType = "osiris.cisco.switch.spine"
	} else if role == "leaf" {
		resType = "osiris.cisco.switch.leaf"
	}

	canonicalKey := hostname
	id := resourceID(resType, canonicalKey)

	prov := sdk.Provider{
		Name:     providerName,
		NativeID: hostname,
		Type:     model,
		Version:  swVersion,
	}

	r, err := sdk.NewResource(id, resType, prov)
	if err != nil {
		return sdk.Resource{}, ""
	}
	r.Name = hostname
	r.Status = "active"

	props := map[string]any{
		"serial":     serial,
		"model":      model,
		"chassis_id": string(version.ChassisID),
	}

	if v := string(version.HostName); v != "" {
		props["hostname"] = v
	}
	if v := int64(version.Memory); v > 0 {
		props["memory"] = v
	}
	if v := int64(version.MemType); v > 0 {
		props["memory"] = v
	}

	// Cisco extensions on device.
	ext := make(map[string]any)
	if v := string(version.BiosVerStr); v != "" {
		ext["bios_version"] = v
	}
	if v := string(version.RRReason); v != "" {
		ext["last_reset_reason"] = v
	}
	if v := string(version.KernUptmDays); v != "" {
		days := v
		hrs := string(version.KernUptmHrs)
		mins := string(version.KernUptmMins)
		secs := string(version.KernUptmSecs)
		ext["kernel_uptime"] = fmt.Sprintf("%sd %sh %sm %ss", days, hrs, mins, secs)
	}
	if v := string(version.RRSysVer); v != "" {
		ext["uptime"] = v
	}

	if len(ext) > 0 {
		r.Extensions = map[string]any{extensionNamespace: ext}
	}

	r.Properties = props
	return r, id
}

// TransformInterfaces converts "show interface brief" output into
// interface resources.
// Returns resources and a map of interface name -> resource ID.
func TransformInterfaces(hostname string, ifBrief interfaceBriefResponse) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	nameToID := make(map[string]string)

	for _, row := range ifBrief.TableInterface.RowInterface {
		ifName := string(row.Interface)
		if ifName == "" {
			continue
		}

		resType := classifyInterfaceType(ifName)
		canonicalKey := fmt.Sprintf("%s|%s", hostname, ifName)
		id := resourceID(resType, canonicalKey)
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
		r.Status = mapInterfaceStatus(string(row.State))

		props := map[string]any{}
		if v := string(row.Speed); v != "" {
			props["speed"] = v
		}
		if v := string(row.Type); v != "" {
			props["mode"] = v
		}
		if v := string(row.PortMode); v != "" {
			props["port_mode"] = v
		}
		if v := string(row.State); v != "" {
			props["admin_status"] = v
		}
		if v := string(row.Status); v != "" {
			props["oper_status"] = v
		}
		if v := string(row.VLAN); v != "" {
			props["vlan"] = v
		}
		if len(props) > 0 {
			r.Properties = props
		}

		resources = append(resources, r)
	}

	return resources, nameToID
}

// TransformLLDPNeighbors converts "show lldp neighbors detail"
// output into network.link connections and stub network.interface
// resources for remote endpoints.
func TransformLLDPNeighbors(hostname string, lldp lldpNeighborsResponse, ifNameToID map[string]string) ([]sdk.Connection, []sdk.Resource) {
	var connections []sdk.Connection
	var stubs []sdk.Resource

	for _, row := range lldp.TableNborDetail.RowNborDetail {
		localPort := string(row.LocalPortID)
		remoteSystem := string(row.SysName)
		remotePort := string(row.PortID)

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
		if v := string(row.MgmtAddr); v != "" {
			props["remote_mgmt_addr"] = v
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

// TransformVLANs converts "show vlan brief" output into VLAN groups.
// Returns groups and a map of VLAN ID string -> group ID.
func TransformVLANs(hostname string, vlanBrief vlanBriefResponse) ([]sdk.Group, map[string]string) {
	var groups []sdk.Group
	vlanIDToGroupID := make(map[string]string)

	for _, row := range vlanBrief.TableVlanBrief.RowVlanBrief {
		vlanIDStr := string(row.VLANID)
		vlanName := string(row.VLANName)

		if vlanIDStr == "" {
			continue
		}

		boundaryToken := fmt.Sprintf("%s|vlan-%s", hostname, vlanIDStr)
		gid := sdk.GroupID(sdk.GroupIDInput{
			Type:          "network.vlan",
			BoundaryToken: boundaryToken,
		})
		vlanIDToGroupID[vlanIDStr] = gid

		g, err := sdk.NewGroup(gid, "network.vlan")
		if err != nil {
			continue
		}
		g.Name = fmt.Sprintf("VLAN %s", vlanIDStr)
		if vlanName != "" {
			g.Description = vlanName
		}

		props := map[string]any{
			"vlan_id": vlanIDStr,
		}
		if v := string(row.VLANState); v != "" {
			props["state"] = v
		}
		if v := string(row.ShutState); v != "" {
			props["admin_state"] = v
		}
		g.Properties = props

		groups = append(groups, g)
	}

	return groups, vlanIDToGroupID
}

// TransformVRFs converts "show vrf all detail" output into VRF groups.
// Returns groups and a map of VRF name -> group ID.
func TransformVRFs(hostname string, vrfDetail vrfDetailResponse) ([]sdk.Group, map[string]string) {
	var groups []sdk.Group
	vrfNameToGroupID := make(map[string]string)

	for _, row := range vrfDetail.TableVRF.RowVRF {
		vrfName := string(row.VRFName)
		if vrfName == "" {
			continue
		}

		boundaryToken := fmt.Sprintf("%s|vrf-%s", hostname, vrfName)
		gid := sdk.GroupID(sdk.GroupIDInput{
			Type:          "logical.vrf",
			BoundaryToken: boundaryToken,
		})
		vrfNameToGroupID[vrfName] = gid

		g, err := sdk.NewGroup(gid, "logical.vrf")
		if err != nil {
			continue
		}
		g.Name = vrfName

		props := map[string]any{}
		if v := string(row.VRFID); v != "" {
			props["vrf_id"] = v
		}
		if v := string(row.VRFState); v != "" {
			props["state"] = v
		}
		if v := string(row.RD); v != "" {
			props["route_distinguisher"] = v
		}
		if len(props) > 0 {
			g.Properties = props
		}

		groups = append(groups, g)
	}

	return groups, vrfNameToGroupID
}

// TransformVPC converts "show vpc brief" output into a vPC group.
// Returns nil group and empty string if vPC is
// not configured (graceful).
func TransformVPC(hostname string, vpcBrief vpcBriefResponse) (*sdk.Group, string) {
	domainID := string(vpcBrief.DomainID)
	if domainID == "" || domainID == "not configured" {
		return nil, ""
	}

	boundaryToken := fmt.Sprintf("%s|vpc-%s", hostname, domainID)
	gid := sdk.GroupID(sdk.GroupIDInput{
		Type:          "network.vpc",
		BoundaryToken: boundaryToken,
	})

	g, err := sdk.NewGroup(gid, "network.vpc")
	if err != nil {
		return nil, ""
	}
	g.Name = fmt.Sprintf("vPC Domain %s", domainID)

	props := map[string]any{
		"domain_id": domainID,
	}
	if v := string(vpcBrief.Role); v != "" {
		props["role"] = v
	}
	if v := string(vpcBrief.PeerStatus); v != "" {
		props["peer_status"] = v
	}
	if v := string(vpcBrief.PeerKeepaliveStatus); v != "" {
		props["peer_keepalive_status"] = v
	}
	g.Properties = props

	return &g, gid
}

// TransformInventory converts "show inventory" output into an
// inventory array for the device's cisco extension.
func TransformInventory(inventory inventoryResponse) []map[string]any {
	var items []map[string]any
	for _, row := range inventory.TableInv.RowInv {
		item := map[string]any{}
		if v := string(row.Name); v != "" {
			item["name"] = v
		}
		if v := string(row.Desc); v != "" {
			item["description"] = v
		}
		if v := string(row.ProductID); v != "" {
			item["product_id"] = v
		}
		if v := string(row.VendorID); v != "" {
			item["vendor_id"] = v
		}
		if v := string(row.SerialNum); v != "" {
			item["serial"] = v
		}
		if len(item) > 0 {
			items = append(items, item)
		}
	}
	return items
}

// TransformSystemResources converts "show system resources" output into
// cisco extension fields for CPU, memory, and load.
func TransformSystemResources(sysRes systemResourcesResponse) map[string]any {
	ext := make(map[string]any)

	if v := string(sysRes.CPUStateIdle); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			ext["cpu_idle"] = f
		}
	}
	if v := string(sysRes.MemoryUsageUsed); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			ext["memory_used"] = n
		}
	}
	if v := string(sysRes.MemoryUsageFree); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			ext["memory_free"] = n
		}
	}
	if v := string(sysRes.LoadAvg1Min); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			ext["load_avg_1min"] = f
		}
	}

	return ext
}

// TransformEnvironment converts "show environment" output into
// cisco extension fields for power supplies and temperature.
func TransformEnvironment(env environmentResponse) map[string]any {
	ext := make(map[string]any)

	// Power supplies.
	if len(env.TablePSInfo.RowPSInfo) > 0 {
		var psus []map[string]any
		for _, row := range env.TablePSInfo.RowPSInfo {
			psu := map[string]any{}
			if v := string(row.PSNum); v != "" {
				psu["id"] = v
			}
			if v := string(row.PSModel); v != "" {
				psu["model"] = v
			}
			if v := string(row.PSStatus); v != "" {
				psu["status"] = v
			}
			if v := string(row.ActualOut); v != "" {
				psu["actual_output"] = v
			}
			if len(psu) > 0 {
				psus = append(psus, psu)
			}
		}
		if len(psus) > 0 {
			ext["power_supplies"] = psus
		}
	}

	// Temperature sensors.
	if len(env.TableTempInfo.RowTempInfo) > 0 {
		var temps []map[string]any
		for _, row := range env.TableTempInfo.RowTempInfo {
			temp := map[string]any{}
			if v := string(row.TempMod); v != "" {
				temp["module"] = v
			}
			if v := string(row.Sensor); v != "" {
				temp["sensor"] = v
			}
			if v := string(row.CurTemp); v != "" {
				temp["current"] = v
			}
			if v := string(row.AlarmStatus); v != "" {
				temp["alarm_status"] = v
			}
			if len(temp) > 0 {
				temps = append(temps, temp)
			}
		}
		if len(temps) > 0 {
			ext["temperature"] = temps
		}
	}

	return ext
}

// Wiring functions - add interface resource IDs to group members.

// WireInterfacesToVLANs adds interface resource IDs as members of
// their VLAN groups.
// Uses the VLAN assignment from "show vlan brief" port list.
func WireInterfacesToVLANs(vlanBrief vlanBriefResponse, ifBrief interfaceBriefResponse, ifNameToID map[string]string, vlanGroups []sdk.Group, vlanIDToGroupID map[string]string) int {
	idx := groupIndex(vlanGroups)
	matched := 0

	// Strategy 1: VLAN brief port list (vlanshowplist-ifidx).
	for _, row := range vlanBrief.TableVlanBrief.RowVlanBrief {
		vlanIDStr := string(row.VLANID)
		gid, ok := vlanIDToGroupID[vlanIDStr]
		if !ok {
			continue
		}
		gi, ok := idx[gid]
		if !ok {
			continue
		}

		// vlanshowplist-ifidx contains comma-separated interface names.
		portList := string(row.PortList)
		if portList == "" {
			continue
		}
		ports := strings.Split(portList, ",")
		for _, port := range ports {
			port = strings.TrimSpace(port)
			ifName := normalizeIfName(port)
			if resID, ok := ifNameToID[ifName]; ok {
				vlanGroups[gi].AddMembers(resID)
				matched++
			}
		}
	}

	// fallback: scan interface brief for per-interface VLAN assignment.
	// NX-OS "show interface brief" includes a "vlan"
	// field per interface.
	if matched == 0 {
		for _, row := range ifBrief.TableInterface.RowInterface {
			vlanStr := string(row.VLAN)
			if vlanStr == "" || vlanStr == "--" {
				continue
			}
			gid, ok := vlanIDToGroupID[vlanStr]
			if !ok {
				continue
			}
			gi, ok := idx[gid]
			if !ok {
				continue
			}
			ifName := string(row.Interface)
			if resID, ok := ifNameToID[ifName]; ok {
				vlanGroups[gi].AddMembers(resID)
				matched++
			}
		}
	}

	return matched
}

// WireInterfacesToVRFs adds interface resource IDs as members of
// their VRF groups.
// Uses the interface list from "show vrf all detail".
//
// NX-OS JSON output varies across platforms and versions:
//   - TABLE_if / ROW_if with if_name (common)
//   - TABLE_intf / ROW_intf with intf_name (some versions)
//
// If the VRF detail data yields 0 matches, falls back to the separate
// vrfInterface data from "show vrf interface" (TABLE_if / ROW_if with
// if_name and vrf_name at the top level).
func WireInterfacesToVRFs(vrfDetail vrfDetailResponse, vrfInterface vrfInterfaceResponse, ifNameToID map[string]string, vrfGroups []sdk.Group, vrfNameToGroupID map[string]string) int {
	idx := groupIndex(vrfGroups)
	matched := 0

	for _, row := range vrfDetail.TableVRF.RowVRF {
		vrfName := string(row.VRFName)
		gid, ok := vrfNameToGroupID[vrfName]
		if !ok {
			continue
		}
		gi, ok := idx[gid]
		if !ok {
			continue
		}

		// interfaceNames tries TABLE_if / ROW_if first, then falls back
		// to TABLE_intf / ROW_intf see vrfDetailRow.interfaceNames.
		for _, ifName := range row.interfaceNames() {
			ifName = normalizeIfName(ifName)
			if resID, ok := ifNameToID[ifName]; ok {
				vrfGroups[gi].AddMembers(resID)
				matched++
			}
		}
	}

	// fallback: "show vrf interface" returns a flat list of
	// VRF-to-interface mappings.
	if matched == 0 {
		for _, ifRow := range vrfInterface.TableIf.RowIf {
			vrfName := string(ifRow.VRFName)
			gid, ok := vrfNameToGroupID[vrfName]
			if !ok {
				continue
			}
			gi, ok := idx[gid]
			if !ok {
				continue
			}
			ifName := normalizeIfName(string(ifRow.IfName))
			if resID, ok := ifNameToID[ifName]; ok {
				vrfGroups[gi].AddMembers(resID)
				matched++
			}
		}
	}

	return matched
}

// WirePortChannelsToVPC adds port-channel resource IDs as
// members of the vpc group.
func WirePortChannelsToVPC(vpcBrief vpcBriefResponse, ifNameToID map[string]string, vpcGroup *sdk.Group) {
	if vpcGroup == nil {
		return
	}

	for _, row := range vpcBrief.TableVPC.RowVPC {
		pcName := string(row.IfIndex)
		if pcName == "" {
			continue
		}
		pcName = normalizeIfName(pcName)
		if resID, ok := ifNameToID[pcName]; ok {
			vpcGroup.AddMembers(resID)
		}
	}
}

// TransformPortChannels converts "show port-channel summary" output
// into "contains" connections from each port-channel (LAG) resource
// to its bundled physical member interfaces, and enriches the
// port-channel resource's own properties with its member count.
// The port-channel resource itself is expected to already exist
// (from TransformInterfaces, via ifNameToID) this only wires
// membership, it does not create the LAG resource.
// A port-channel with no resolvable member interfaces (a rare
// data row, or the LAG resource itself missing) yields no connections
// for that row and leaves member_count unset.
//
// Only "group" (bundle number), "port-channel" (LAG interface name) and
// the member "port" list are read NX-API's own per-row protocol field
// name (LACP vs. PAgP vs. static) is not confirmed against a real
// response yet, so it is deliberately not extracted here rather
// than guessed.
func TransformPortChannels(pcSummary portChannelSummaryResponse, resources []sdk.Resource, ifNameToID map[string]string) []sdk.Connection {
	resIdx := make(map[string]int, len(resources))
	for i, r := range resources {
		resIdx[r.ID] = i
	}

	var connections []sdk.Connection

	for _, row := range pcSummary.TableChannel.RowChannel {
		pcName := normalizeIfName(string(row.PortChannel))
		if pcName == "" {
			continue
		}
		pcID, ok := ifNameToID[pcName]
		if !ok {
			continue
		}

		memberCount := 0
		for _, memberRow := range row.TableMember.RowMember {
			portName := normalizeIfName(string(memberRow.Port))
			if portName == "" {
				continue
			}
			memberCount++

			portID, ok := ifNameToID[portName]
			if !ok {
				continue
			}

			canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
				Type:      "contains",
				Direction: "forward",
				Source:    pcID,
				Target:    portID,
			})
			connID := sdk.BuildConnectionID(canonicalKey, 16)
			conn, err := sdk.NewConnection(connID, "contains", pcID, portID)
			if err != nil {
				continue
			}
			conn.Name = fmt.Sprintf("%s contains %s", pcName, portName)
			conn.Direction = "forward"
			conn.Status = "active"
			if v := string(memberRow.PortStatus); v != "" {
				conn.Properties = map[string]any{"port_status": v}
			}

			connections = append(connections, conn)
		}

		if memberCount > 0 {
			if ri, ok := resIdx[pcID]; ok {
				if resources[ri].Properties == nil {
					resources[ri].Properties = make(map[string]any)
				}
				resources[ri].Properties["member_count"] = memberCount
			}
		}
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

// Helper functions.

// resourceID generates a deterministic resource ID from type
// and canonical suffix.
func resourceID(resType, canonicalSuffix string) string {
	canonicalKey := fmt.Sprintf("v1|%s|%s", resType, canonicalSuffix)
	hash := sdk.Hash16(canonicalKey)
	hint := sdk.DeriveHint(canonicalSuffix, hash)
	return fmt.Sprintf("res-%s-%s-%s", resType, hint, hash)
}

// groupIndex builds a map of group ID -> index in slice for
// efficient mutation.
func groupIndex(groups []sdk.Group) map[string]int {
	idx := make(map[string]int, len(groups))
	for i, g := range groups {
		idx[g.ID] = i
	}
	return idx
}

// classifyRole heuristically determines if a device is leaf or spine.
func classifyRole(hostname, model string) string {
	h := strings.ToLower(hostname)
	m := strings.ToLower(model)

	if strings.Contains(h, "spine") || strings.Contains(h, "spn") {
		return "spine"
	}
	if strings.Contains(h, "leaf") || strings.Contains(h, "lf") {
		return "leaf"
	}

	// Model-based: C93xx are typically leaf, C95xx are spine.
	if strings.Contains(m, "c93") || strings.Contains(m, "93") {
		return "leaf"
	}
	if strings.Contains(m, "c95") || strings.Contains(m, "95") {
		return "spine"
	}

	return ""
}

// classifyInterfaceType determines the OSIRIS type for an
// interface by name.
func classifyInterfaceType(ifName string) string {
	lower := strings.ToLower(ifName)
	if strings.HasPrefix(lower, "port-channel") || strings.HasPrefix(lower, "po") {
		return "osiris.cisco.interface.lag"
	}
	return "network.interface"
}

// mapInterfaceStatus converts NX-OS interface state to
// OSIRIS status values.
func mapInterfaceStatus(state string) string {
	switch strings.ToLower(state) {
	case "up":
		return "active"
	case "down":
		return "inactive"
	default:
		return "unknown"
	}
}

// normalizeIfName normalizes interface name abbreviations to full form.
func normalizeIfName(name string) string {
	name = strings.TrimSpace(name)
	// Common NX-OS abbreviations.
	if strings.HasPrefix(name, "Eth") && !strings.HasPrefix(name, "Ethernet") {
		return "Ethernet" + strings.TrimPrefix(name, "Eth")
	}
	if strings.HasPrefix(name, "Po") && !strings.HasPrefix(name, "port-channel") {
		return "port-channel" + strings.TrimPrefix(name, "Po")
	}
	return name
}

// ensureCiscoExtension ensures the extensions map and
// osiris.cisco sub-map exist.
func ensureCiscoExtension(ext *map[string]any) {
	if *ext == nil {
		*ext = make(map[string]any)
	}
	if _, ok := (*ext)[extensionNamespace]; !ok {
		(*ext)[extensionNamespace] = make(map[string]any)
	}
}
