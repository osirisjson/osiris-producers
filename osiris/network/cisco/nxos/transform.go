// transform.go - Pure NX-OS->OSIRIS mapping functions.
// Converts NX-API CLI response bodies into OSIRIS types.
// All functions are stateless: no I/O, no HTTP, just data transformation.
//
// For an introduction to OSIRIS JSON Producer for Cisco see:
// "[OSIRIS-JSON-CISCO]."
//
// [OSIRIS-JSON-CISCO]: https://osirisjson.org/en/docs/producers/cisco

package nxos

import (
	"fmt"
	"strconv"
	"strings"

	"go.osirisjson.org/producers/pkg/sdk"
)

const extensionNamespace = "osiris.cisco"
const providerName = "cisco"

// TransformDevice converts "show version" output into a single network.switch resource.
func TransformDevice(hostname string, version map[string]any) (sdk.Resource, string) {
	model := str(version, "chassis_id")
	serial := str(version, "proc_board_id")
	swVersion := str(version, "sys_ver_str")

	role := classifyRole(hostname, model)
	resType := "network.switch"
	if role == "spine" {
		resType = "network.switch.spine"
	} else if role == "leaf" {
		resType = "network.switch.leaf"
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
		"chassis_id": str(version, "chassis_id"),
	}

	if v := str(version, "host_name"); v != "" {
		props["hostname"] = v
	}
	if v := num(version, "memory"); v > 0 {
		props["memory"] = v
	}
	if v := num(version, "mem_type"); v > 0 {
		props["memory"] = v
	}

	// Cisco extensions on device.
	ext := make(map[string]any)
	if v := str(version, "bios_ver_str"); v != "" {
		ext["bios_version"] = v
	}
	if v := str(version, "rr_reason"); v != "" {
		ext["last_reset_reason"] = v
	}
	if v := str(version, "kern_uptm_days"); v != "" {
		days := str(version, "kern_uptm_days")
		hrs := str(version, "kern_uptm_hrs")
		mins := str(version, "kern_uptm_mins")
		secs := str(version, "kern_uptm_secs")
		ext["kernel_uptime"] = fmt.Sprintf("%sd %sh %sm %ss", days, hrs, mins, secs)
	}
	if v := str(version, "rr_sys_ver"); v != "" {
		ext["uptime"] = v
	}

	if len(ext) > 0 {
		r.Extensions = map[string]any{extensionNamespace: ext}
	}

	r.Properties = props
	return r, id
}

// TransformInterfaces converts "show interface brief" output into interface resources.
// Returns resources and a map of interface name -> resource ID.
func TransformInterfaces(hostname string, ifBrief map[string]any) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	nameToID := make(map[string]string)

	// Ethernet interfaces from TABLE_interface.
	ethRows := parseTableRows(ifBrief, "TABLE_interface", "ROW_interface")
	for _, row := range ethRows {
		ifName := str(row, "interface")
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
		r.Status = mapInterfaceStatus(str(row, "state"))

		props := map[string]any{}
		if v := str(row, "speed"); v != "" {
			props["speed"] = v
		}
		if v := str(row, "type"); v != "" {
			props["mode"] = v
		}
		if v := str(row, "portmode"); v != "" {
			props["port_mode"] = v
		}
		if v := str(row, "state"); v != "" {
			props["admin_status"] = v
		}
		if v := str(row, "status"); v != "" {
			props["oper_status"] = v
		}
		if v := str(row, "vlan"); v != "" {
			props["vlan"] = v
		}
		if len(props) > 0 {
			r.Properties = props
		}

		resources = append(resources, r)
	}

	return resources, nameToID
}

// TransformLLDPNeighbors converts "show lldp neighbors detail" output into
// network.link connections and stub network.interface resources for remote endpoints.
func TransformLLDPNeighbors(hostname string, lldp map[string]any, ifNameToID map[string]string) ([]sdk.Connection, []sdk.Resource) {
	var connections []sdk.Connection
	var stubs []sdk.Resource

	rows := parseTableRows(lldp, "TABLE_nbor_detail", "ROW_nbor_detail")
	for _, row := range rows {
		localPort := str(row, "l_port_id")
		remoteSystem := str(row, "sys_name")
		remotePort := str(row, "port_id")

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
		if v := str(row, "mgmt_addr"); v != "" {
			props["remote_mgmt_addr"] = v
		}
		stub.Properties = props
		stubs = append(stubs, stub)

		// Create connection.
		connInput := sdk.ConnectionIDInput{
			Type:      "network.link",
			Direction: "bidirectional",
			Source:    localID,
			Target:    remoteID,
		}
		connKey := sdk.ConnectionCanonicalKey(connInput)
		connID := sdk.BuildConnectionID(connKey, 16)

		conn, err := sdk.NewConnection(connID, "network.link", localID, remoteID)
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
func TransformVLANs(hostname string, vlanBrief map[string]any) ([]sdk.Group, map[string]string) {
	var groups []sdk.Group
	vlanIDToGroupID := make(map[string]string)

	rows := parseTableRows(vlanBrief, "TABLE_vlanbriefxbrief", "ROW_vlanbriefxbrief")
	for _, row := range rows {
		vlanIDStr := str(row, "vlanshowbr-vlanid")
		vlanName := str(row, "vlanshowbr-vlanname")

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
		if v := str(row, "vlanshowbr-vlanstate"); v != "" {
			props["state"] = v
		}
		if v := str(row, "vlanshowbr-shutstate"); v != "" {
			props["admin_state"] = v
		}
		g.Properties = props

		groups = append(groups, g)
	}

	return groups, vlanIDToGroupID
}

// TransformVRFs converts "show vrf all detail" output into VRF groups.
// Returns groups and a map of VRF name -> group ID.
func TransformVRFs(hostname string, vrfDetail map[string]any) ([]sdk.Group, map[string]string) {
	var groups []sdk.Group
	vrfNameToGroupID := make(map[string]string)

	rows := parseTableRows(vrfDetail, "TABLE_vrf", "ROW_vrf")
	for _, row := range rows {
		vrfName := str(row, "vrf_name")
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
		if v := str(row, "vrf_id"); v != "" {
			props["vrf_id"] = v
		}
		if v := str(row, "vrf_state"); v != "" {
			props["state"] = v
		}
		if v := str(row, "rd"); v != "" {
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
// Returns nil group and empty string if vPC is not configured (graceful).
func TransformVPC(hostname string, vpcBrief map[string]any) (*sdk.Group, string) {
	domainID := str(vpcBrief, "vpc-domain-id")
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
	if v := str(vpcBrief, "vpc-role"); v != "" {
		props["role"] = v
	}
	if v := str(vpcBrief, "vpc-peer-status"); v != "" {
		props["peer_status"] = v
	}
	if v := str(vpcBrief, "vpc-peer-keepalive-status"); v != "" {
		props["peer_keepalive_status"] = v
	}
	g.Properties = props

	return &g, gid
}

// TransformInventory converts "show inventory" output into an inventory array
// for the device's cisco extension.
func TransformInventory(inventory map[string]any) []map[string]any {
	rows := parseTableRows(inventory, "TABLE_inv", "ROW_inv")
	var items []map[string]any
	for _, row := range rows {
		item := map[string]any{}
		if v := str(row, "name"); v != "" {
			item["name"] = v
		}
		if v := str(row, "desc"); v != "" {
			item["description"] = v
		}
		if v := str(row, "productid"); v != "" {
			item["product_id"] = v
		}
		if v := str(row, "vendorid"); v != "" {
			item["vendor_id"] = v
		}
		if v := str(row, "serialnum"); v != "" {
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
func TransformSystemResources(sysRes map[string]any) map[string]any {
	ext := make(map[string]any)

	if v := str(sysRes, "cpu_state_idle"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			ext["cpu_idle"] = f
		}
	}
	if v := str(sysRes, "memory_usage_used"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			ext["memory_used"] = n
		}
	}
	if v := str(sysRes, "memory_usage_free"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			ext["memory_free"] = n
		}
	}
	if v := str(sysRes, "load_avg_1min"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			ext["load_avg_1min"] = f
		}
	}

	return ext
}

// TransformEnvironment converts "show environment" output into
// cisco extension fields for power supplies and temperature.
func TransformEnvironment(env map[string]any) map[string]any {
	ext := make(map[string]any)

	// Power supplies.
	psuRows := parseTableRows(env, "TABLE_psinfo", "ROW_psinfo")
	if len(psuRows) > 0 {
		var psus []map[string]any
		for _, row := range psuRows {
			psu := map[string]any{}
			if v := str(row, "psnum"); v != "" {
				psu["id"] = v
			}
			if v := str(row, "psmodel"); v != "" {
				psu["model"] = v
			}
			if v := str(row, "ps_status"); v != "" {
				psu["status"] = v
			}
			if v := str(row, "actual_out"); v != "" {
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
	tempRows := parseTableRows(env, "TABLE_tempinfo", "ROW_tempinfo")
	if len(tempRows) > 0 {
		var temps []map[string]any
		for _, row := range tempRows {
			temp := map[string]any{}
			if v := str(row, "tempmod"); v != "" {
				temp["module"] = v
			}
			if v := str(row, "sensor"); v != "" {
				temp["sensor"] = v
			}
			if v := str(row, "curtemp"); v != "" {
				temp["current"] = v
			}
			if v := str(row, "alarmstatus"); v != "" {
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

// WireInterfacesToVLANs adds interface resource IDs as members of their VLAN groups.
// Uses the VLAN assignment from "show vlan brief" port list.
func WireInterfacesToVLANs(vlanBrief map[string]any, ifNameToID map[string]string, vlanGroups []sdk.Group, vlanIDToGroupID map[string]string) {
	idx := groupIndex(vlanGroups)

	rows := parseTableRows(vlanBrief, "TABLE_vlanbriefxbrief", "ROW_vlanbriefxbrief")
	for _, row := range rows {
		vlanIDStr := str(row, "vlanshowbr-vlanid")
		gid, ok := vlanIDToGroupID[vlanIDStr]
		if !ok {
			continue
		}
		gi, ok := idx[gid]
		if !ok {
			continue
		}

		// vlanshowplist-ifidx contains comma-separated interface names.
		portList := str(row, "vlanshowplist-ifidx")
		if portList == "" {
			continue
		}
		ports := strings.Split(portList, ",")
		for _, port := range ports {
			port = strings.TrimSpace(port)
			ifName := normalizeIfName(port)
			if resID, ok := ifNameToID[ifName]; ok {
				vlanGroups[gi].AddMembers(resID)
			}
		}
	}
}

// WireInterfacesToVRFs adds interface resource IDs as members of their VRF groups.
// Uses the interface list from "show vrf all detail".
func WireInterfacesToVRFs(vrfDetail map[string]any, ifNameToID map[string]string, vrfGroups []sdk.Group, vrfNameToGroupID map[string]string) {
	idx := groupIndex(vrfGroups)

	rows := parseTableRows(vrfDetail, "TABLE_vrf", "ROW_vrf")
	for _, row := range rows {
		vrfName := str(row, "vrf_name")
		gid, ok := vrfNameToGroupID[vrfName]
		if !ok {
			continue
		}
		gi, ok := idx[gid]
		if !ok {
			continue
		}

		// TABLE_if contains the interfaces in this VRF.
		ifRows := parseTableRows(row, "TABLE_if", "ROW_if")
		for _, ifRow := range ifRows {
			ifName := str(ifRow, "if_name")
			ifName = normalizeIfName(ifName)
			if resID, ok := ifNameToID[ifName]; ok {
				vrfGroups[gi].AddMembers(resID)
			}
		}
	}
}

// WirePortChannelsToVPC adds port-channel resource IDs as members of the vpc group.
func WirePortChannelsToVPC(vpcBrief map[string]any, ifNameToID map[string]string, vpcGroup *sdk.Group) {
	if vpcGroup == nil {
		return
	}

	rows := parseTableRows(vpcBrief, "TABLE_vpc", "ROW_vpc")
	for _, row := range rows {
		pcName := str(row, "vpc-ifindex")
		if pcName == "" {
			continue
		}
		pcName = normalizeIfName(pcName)
		if resID, ok := ifNameToID[pcName]; ok {
			vpcGroup.AddMembers(resID)
		}
	}
}

// EnrichInterfaceDetails mutates interface resources in-place with detailed
// information from "show interface" (full output).
func EnrichInterfaceDetails(hostname string, ifDetail map[string]any, resources []sdk.Resource, ifNameToID map[string]string) {
	// Build reverse map: resource ID -> index in resources.
	resIdx := make(map[string]int, len(resources))
	for i, r := range resources {
		resIdx[r.ID] = i
	}

	rows := parseTableRows(ifDetail, "TABLE_interface", "ROW_interface")
	for _, row := range rows {
		ifName := str(row, "interface")
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

		if v := num(row, "eth_mtu"); v > 0 {
			props["mtu"] = v
		}
		if v := num(row, "eth_bw"); v > 0 {
			props["bandwidth"] = v
		}
		if v := str(row, "eth_duplex"); v != "" {
			props["duplex"] = v
		}
		if v := str(row, "eth_hw_addr"); v != "" {
			props["mac_address"] = sdk.NormalizeMAC(v)
		}
		if v := str(row, "desc"); v != "" {
			props["description"] = v
		}

		// Counters.
		if v := num(row, "eth_outbytes"); v > 0 {
			props["tx_bytes"] = v
		}
		if v := num(row, "eth_inbytes"); v > 0 {
			props["rx_bytes"] = v
		}
		if v := num(row, "eth_outpkts"); v > 0 {
			props["tx_packets"] = v
		}
		if v := num(row, "eth_inpkts"); v > 0 {
			props["rx_packets"] = v
		}
	}
}

// Helper functions.

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

// parseTableRows handles NX-API polymorphism for TABLE/ROW structures.
// When a single row exists, NX-API returns it as an object rather than an array.
func parseTableRows(body map[string]any, tableKey, rowKey string) []map[string]any {
	table, ok := body[tableKey]
	if !ok {
		return nil
	}

	tableMap, ok := table.(map[string]any)
	if !ok {
		return nil
	}

	rowData, ok := tableMap[rowKey]
	if !ok {
		return nil
	}

	// NX-API polymorphism: single row = object, multiple = array.
	switch v := rowData.(type) {
	case map[string]any:
		return []map[string]any{v}
	case []any:
		var rows []map[string]any
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				rows = append(rows, m)
			}
		}
		return rows
	}

	return nil
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

// classifyInterfaceType determines the OSIRIS type for an interface by name.
func classifyInterfaceType(ifName string) string {
	lower := strings.ToLower(ifName)
	if strings.HasPrefix(lower, "port-channel") || strings.HasPrefix(lower, "po") {
		return "network.interface.lag"
	}
	return "network.interface"
}

// mapInterfaceStatus converts NX-OS interface state to OSIRIS status values.
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

// str safely extracts a string value from an attribute map.
func str(attrs map[string]any, key string) string {
	if attrs == nil {
		return ""
	}
	if v, ok := attrs[key]; ok {
		switch s := v.(type) {
		case string:
			return s
		case float64:
			if s == float64(int64(s)) {
				return strconv.FormatInt(int64(s), 10)
			}
			return strconv.FormatFloat(s, 'f', -1, 64)
		}
	}
	return ""
}

// num safely extracts a numeric value from an attribute map.
func num(attrs map[string]any, key string) int64 {
	if attrs == nil {
		return 0
	}
	v, ok := attrs[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	case string:
		if i, err := strconv.ParseInt(n, 10, 64); err == nil {
			return i
		}
	}
	return 0
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
