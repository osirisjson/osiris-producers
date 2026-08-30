// transform_segmentation.go - Layer-2/3 segmentation: VLANs and VRFs.
// "show vlan brief" -> network.vlan resources (7.5.1) plus network.l2
// port-membership connections; "show vrf all detail" -> logical.vrf
// groups and their interface membership.
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

// TransformVLANs converts "show vlan brief" output into network.vlan
// resources (OSIRIS-JSON-v1.0 7.5.1). Returns the resources and a map
// of VLAN ID string -> resource ID; the Wire* helpers below turn each
// port's membership into a network.l2 connection to the matching
// resource (OSIRIS-JSON-v1.0 5.2.3), so "VLAN N has these ports"
// is a real, traversable set of edges a consumer can collapse rather
// than a denormalised port list carried as a property.
//
// deviceKey is the owning switch's own canonical identity
// (see deviceNativeKey) a VLAN's resource ID is <serial>/vlan/<id>,
// stable across a target alias change, matching the
// interface-ID convention.
func TransformVLANs(deviceKey string, vlanBrief vlanBriefResponse) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	vlanIDToResourceID := make(map[string]string)

	for _, row := range vlanBrief.TableVlanBrief.RowVlanBrief {
		vlanIDStr := strings.TrimSpace(string(row.VLANID))
		if vlanIDStr == "" {
			continue
		}
		vlanName := trimmed(row.VLANName)

		canonicalKey := fmt.Sprintf("%s/vlan/%s", deviceKey, vlanIDStr)
		id := resourceID(providerName, canonicalKey)
		vlanIDToResourceID[vlanIDStr] = id

		prov := sdk.Provider{Name: providerName, NativeID: canonicalKey}
		r, err := sdk.NewResource(id, "network.vlan", prov)
		if err != nil {
			continue
		}
		if vlanName != "" {
			r.Name = vlanName
		} else {
			r.Name = "VLAN " + vlanIDStr
		}
		r.Status = mapVLANStatus(string(row.VLANState))

		props := map[string]any{}
		if n, err := strconv.Atoi(vlanIDStr); err == nil {
			props["vlan_id"] = n
		}
		if vlanName != "" {
			props["vlan_name"] = vlanName
		}
		if v := trimmed(row.ShutState); v != "" {
			props["admin_state"] = v
		}
		if len(props) > 0 {
			r.Properties = props
		}

		resources = append(resources, r)
	}

	return resources, vlanIDToResourceID
}

// mapVLANStatus maps an NX-OS "show vlan brief" state
// (active / suspend / act/lshut / ...) onto an OSIRIS resource status.
func mapVLANStatus(state string) string {
	s := strings.ToLower(strings.TrimSpace(state))
	switch {
	case s == "":
		return "unknown"
	case strings.HasPrefix(s, "act"):
		return "active"
	case strings.HasPrefix(s, "sus"):
		return "inactive"
	default:
		return "unknown"
	}
}

// vlanMembershipConn builds one network.l2 connection linking a switch
// port to the network.vlan resource for the L2 broadcast domain it
// participates in. Deduplicated via seen (keyed by the deterministic
// connection ID) so a port reported both by "show vlan brief" port
// list and by "show interface switchport"'s trunk_vlans yields a single
// connection, not a duplicate-ID document-build failure. Direction is
// bidirectional: L2 co-membership carries no data-flow direction.
func vlanMembershipConn(portID, vlanResID, portName, vlanID string, seen map[string]bool) (sdk.Connection, bool) {
	input := sdk.ConnectionIDInput{
		Type:      "network.l2",
		Direction: "bidirectional",
		Source:    portID,
		Target:    vlanResID,
	}
	connID := sdk.BuildConnectionID(sdk.ConnectionCanonicalKey(input), 16)
	if seen[connID] {
		return sdk.Connection{}, false
	}
	seen[connID] = true

	conn, err := sdk.NewConnection(connID, "network.l2", portID, vlanResID)
	if err != nil {
		return sdk.Connection{}, false
	}
	conn.Name = fmt.Sprintf("%s in VLAN %s", portName, vlanID)
	conn.Direction = "bidirectional"
	conn.Status = "active"
	return conn, true
}

// WireInterfacesToVLANs builds network.l2 port-membership connections
// (switch port <-> network.vlan resource) from "show vlan brief" own
// per-VLAN port list (vlanshowplist-ifidx), falling back to
// "show interface brief" per-interface access-VLAN field only when the
// port list yielded nothing. seen deduplicates against
// WireTrunkPortsToVLANs, which shares it. Returns the connections built.
func WireInterfacesToVLANs(vlanBrief vlanBriefResponse, ifBrief interfaceBriefResponse, ifNameToID map[string]string, vlanIDToResID map[string]string, seen map[string]bool) []sdk.Connection {
	var conns []sdk.Connection

	// VLAN brief port list (vlanshowplist-ifidx,
	// comma-separated interface names).
	for _, row := range vlanBrief.TableVlanBrief.RowVlanBrief {
		vlanID := strings.TrimSpace(string(row.VLANID))
		vlanResID, ok := vlanIDToResID[vlanID]
		if !ok {
			continue
		}
		portList := string(row.PortList)
		if portList == "" {
			continue
		}
		for _, port := range strings.Split(portList, ",") {
			ifName := normalizeIfName(strings.TrimSpace(port))
			portID, ok := ifNameToID[ifName]
			if !ok {
				continue
			}
			if c, ok := vlanMembershipConn(portID, vlanResID, ifName, vlanID, seen); ok {
				conns = append(conns, c)
			}
		}
	}

	// Fallback: per-interface access-VLAN field from
	// "show interface brief" (only when the port list above
	// produced nothing).
	if len(conns) == 0 {
		for _, row := range ifBrief.TableInterface.RowInterface {
			vlanID := strings.TrimSpace(string(row.VLAN))
			if vlanID == "" || vlanID == "--" {
				continue
			}
			vlanResID, ok := vlanIDToResID[vlanID]
			if !ok {
				continue
			}
			ifName := normalizeIfName(string(row.Interface))
			portID, ok := ifNameToID[ifName]
			if !ok {
				continue
			}
			if c, ok := vlanMembershipConn(portID, vlanResID, ifName, vlanID, seen); ok {
				conns = append(conns, c)
			}
		}
	}

	return conns
}

// WireTrunkPortsToVLANs builds a network.l2 connection from each trunk
// port to every network.vlan resource in its
// "show interface switchport" trunk_vlans list (range-expanded via
// expandVLANRanges). A trunk_vlans entry with no matching resource
// (a VLAN allowed on the trunk but not itself present in
// "show vlan brief") is skipped, matching WireInterfacesToVLANs/VRFs
// own resolvable-relationships-only policy. seen is shared with
// WireInterfacesToVLANs for deduplication.
// Returns the connections built.
func WireTrunkPortsToVLANs(switchport switchportResponse, ifNameToID map[string]string, vlanIDToResID map[string]string, seen map[string]bool) []sdk.Connection {
	var conns []sdk.Connection

	for _, row := range switchport.TableInterface.RowInterface {
		ifName := normalizeIfName(string(row.Interface))
		portID, ok := ifNameToID[ifName]
		if !ok {
			continue
		}
		for _, vlanID := range expandVLANRanges(string(row.TrunkVLANs)) {
			vlanResID, ok := vlanIDToResID[vlanID]
			if !ok {
				continue
			}
			if c, ok := vlanMembershipConn(portID, vlanResID, ifName, vlanID, seen); ok {
				conns = append(conns, c)
			}
		}
	}

	return conns
}

// expandVLANRanges parses NX-OS own comma-separated, range-compressed
// VLAN list format (e.g. "85,900,906-909" -> ["85", "900", "906",
// "907", "908", "909"]) into individual VLAN ID strings, matching
// vlanIDToResID own string-keyed lookup. A malformed token (not a
// plain integer or a valid "low-high" range) is skipped rather than
// aborting the whole list.
func expandVLANRanges(s string) []string {
	var ids []string
	for _, token := range strings.Split(s, ",") {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		low, high, isRange := strings.Cut(token, "-")
		if !isRange {
			if _, err := strconv.Atoi(token); err != nil {
				continue
			}
			ids = append(ids, token)
			continue
		}
		lowN, err := strconv.Atoi(low)
		if err != nil {
			continue
		}
		highN, err := strconv.Atoi(high)
		if err != nil || highN < lowN {
			continue
		}
		for n := lowN; n <= highN; n++ {
			ids = append(ids, strconv.Itoa(n))
		}
	}
	return ids
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
