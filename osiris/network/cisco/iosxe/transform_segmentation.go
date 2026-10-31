// transform_segmentation.go - Layer-2 and layer-3 segmentation for the
// Cisco IOS-XE OSIRIS JSON Producer: VLANs and VRFs.
//
// The representation is the one the Cisco NX-OS producer uses, so a
// consumer reads both the same way:
//
//   - a VLAN is a network.vlan resource owned by the device, ID
//     cisco.iosxe::<serial>/vlan/<id>, sourced from
//     Cisco-IOS-XE-vlan-oper; each interface the VLAN database assigns
//     to it is linked by a bidirectional network.l2 connection
//     (port <-> VLAN);
//   - a VRF is a network.vrf group (OSIRIS JSON lists VRFs among the
//     network-boundary groups) scoped to the device, sourced from
//     the native vrf definitions; its members are the interfaces bound
//     to it, by operational state or else by "vrf forwarding" (and the
//     connected subnets of those interfaces, see transform_routing.go);
//   - a switchport's configured access VLAN and every VLAN its trunk
//     allows (the allowed list, or all VLANs when none is configured,
//     which is the IOS-XE default), are network.l2 memberships too;
//     the port carries port_mode ("access" or "trunk") and, on a trunk,
//     native_vlan;
//   - an 802.1Q subinterface carries encapsulation "dot1q" and its
//     vlan_id as properties, from the native interface configuration,
//     and an SVI (interface VlanN) carries vlan_id. These are tags on
//     the interface, not memberships, so no connection is drawn for
//     them.
//
// A membership whose interface or VLAN is not an emitted resource is
// dropped, never guessed.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package iosxe

import (
	"fmt"
	"strconv"
	"strings"

	"go.osirisjson.org/producers/pkg/sdk"
)

// vlanStatus maps the vlan-oper status enum to a resource status.
func vlanStatus(s string) string {
	switch strings.TrimSpace(s) {
	case "active":
		return "active"
	case "suspend":
		return "inactive"
	}
	return "unknown"
}

// vlanMembership builds network.l2 port <-> VLAN connections,
// deduplicated by connection ID across every membership source.
type vlanMembership struct {
	seen  map[string]bool
	conns []sdk.Connection
}

func (m *vlanMembership) add(portID, portName, vlanResID string, vlanID int) {
	key := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
		Type:      "network.l2",
		Direction: "bidirectional",
		Source:    portID,
		Target:    vlanResID,
	})
	connID := sdk.BuildConnectionID(key, 16)
	if m.seen[connID] {
		return
	}
	m.seen[connID] = true
	c, err := sdk.NewConnection(connID, "network.l2", portID, vlanResID)
	if err != nil {
		return
	}
	c.Name = fmt.Sprintf("%s in VLAN %d", portName, vlanID)
	c.Direction = "bidirectional"
	c.Status = "active"
	m.conns = append(m.conns, c)
}

// TransformVLANs converts the VLAN database into network.vlan resources
// owned by the device with key deviceKey and network.l2 connections
// from every interface the database assigns to a VLAN (both the ports
// and vlan-interfaces lists) and from every switchport whose
// configuration places it in a VLAN (access VLAN, trunk allowed list).
// Memberships of VLANs absent from the database are dropped.
func TransformVLANs(deviceKey string, vlans vlansOper, native nativeInterfaces, ifNameToID map[string]string) ([]sdk.Resource, []sdk.Connection) {
	var resources []sdk.Resource
	vlanResID := map[int]string{}
	var vlanOrder []int
	m := &vlanMembership{seen: map[string]bool{}}

	for _, v := range vlans.VLANs {
		id, err := strconv.Atoi(strings.TrimSpace(v.ID))
		if err != nil || id < 1 || id > 4094 || vlanResID[id] != "" {
			continue
		}

		nativeID := fmt.Sprintf("%s/vlan/%d", deviceKey, id)
		resID := resourceID(providerName, nativeID)
		r, err := sdk.NewResource(resID, "network.vlan", sdk.Provider{Name: providerName, NativeID: nativeID})
		if err != nil {
			continue
		}
		vlanResID[id] = resID
		vlanOrder = append(vlanOrder, id)
		name := strings.TrimSpace(v.Name)
		r.Name = name
		if r.Name == "" {
			r.Name = fmt.Sprintf("VLAN %d", id)
		}
		r.Status = vlanStatus(v.Status)
		props := map[string]any{"vlan_id": id}
		if name != "" {
			props["vlan_name"] = name
		}
		r.Properties = props
		resources = append(resources, r)

		for _, mem := range append(append([]vlanMember{}, v.Ports...), v.Intfs...) {
			if ifName, portID, ok := lookupInterface(ifNameToID, mem.name()); ok {
				m.add(portID, ifName, resID, id)
			}
		}
	}

	for _, n := range native.Interface.Entries {
		ifName := n.fullName()
		portID, ok := ifNameToID[ifName]
		if !ok {
			continue
		}
		sp := n.Switchport
		switch {
		case sp.Mode.Trunk != nil:
			allowed := trunkAllowedVLANs(sp)
			for _, id := range vlanOrder {
				if allowed == nil || allowed[id] {
					m.add(portID, ifName, vlanResID[id], id)
				}
			}
		case sp.Mode.Access != nil || strings.TrimSpace(sp.AccessVLAN) != "":
			if id, err := strconv.Atoi(strings.TrimSpace(sp.AccessVLAN)); err == nil && vlanResID[id] != "" {
				m.add(portID, ifName, vlanResID[id], id)
			}
		}
	}
	return resources, m.conns
}

// parseVLANList expands an IOS VLAN list ("10,20-22") into a set. ok is
// false when any token is malformed or out of range.
func parseVLANList(s string) (map[int]bool, bool) {
	out := map[int]bool{}
	for _, tok := range strings.Split(strings.TrimSpace(s), ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		lo, hi, isRange := strings.Cut(tok, "-")
		a, err := strconv.Atoi(strings.TrimSpace(lo))
		if err != nil {
			return nil, false
		}
		b := a
		if isRange {
			if b, err = strconv.Atoi(strings.TrimSpace(hi)); err != nil {
				return nil, false
			}
		}
		if a < 1 || b > 4094 || b < a {
			return nil, false
		}
		for v := a; v <= b; v++ {
			out[v] = true
		}
	}
	return out, true
}

// trunkAllowedVLANs returns the VLANs a trunk carries, or nil for all
// VLANs. With no allowed list configured a trunk carries every VLAN.
// "none" yields an empty set; an explicit list is extended by add and
// reduced by remove; except means every VLAN but the listed ones. A
// malformed list yields an empty set, so nothing is guessed.
func trunkAllowedVLANs(sp switchport) map[int]bool {
	v2, old := sp.Allowed.V2, sp.Allowed.Legacy
	list, except, remove := v2.VLANs, v2.Except, v2.Remove
	adds := append([]string{}, v2.Add...)
	none := v2.None != nil
	if list == "" && except == "" && remove == "" && len(adds) == 0 && !none {
		list, except, remove, none = old.VLANs, old.Except, old.Remove, old.None != nil
		if old.Add != "" {
			adds = append(adds, old.Add)
		}
	}

	var allowed map[int]bool // nil = all
	switch {
	case none:
		allowed = map[int]bool{}
	case strings.TrimSpace(list) != "":
		set, ok := parseVLANList(list)
		if !ok {
			return map[int]bool{}
		}
		allowed = set
	case strings.TrimSpace(except) != "":
		set, ok := parseVLANList(except)
		if !ok {
			return map[int]bool{}
		}
		allowed = map[int]bool{}
		for v := 1; v <= 4094; v++ {
			if !set[v] {
				allowed[v] = true
			}
		}
	}
	for _, a := range adds {
		set, ok := parseVLANList(a)
		if !ok {
			return map[int]bool{}
		}
		if allowed == nil {
			continue
		}
		for v := range set {
			allowed[v] = true
		}
	}
	if strings.TrimSpace(remove) != "" {
		set, ok := parseVLANList(remove)
		if !ok {
			return map[int]bool{}
		}
		if allowed == nil {
			allowed = map[int]bool{}
			for v := 1; v <= 4094; v++ {
				allowed[v] = true
			}
		}
		for v := range set {
			delete(allowed, v)
		}
	}
	return allowed
}

// EnrichEncapsulation sets, from the native interface configuration,
// encapsulation "dot1q" and vlan_id on every 802.1Q subinterface,
// vlan_id on every SVI and port_mode ("access" or "trunk") plus, on a
// trunk with a configured native VLAN, native_vlan on every switchport.
// Interfaces that are not emitted resources are skipped.
func EnrichEncapsulation(native nativeInterfaces, resources []sdk.Resource, ifNameToID map[string]string) {
	idx := make(map[string]int, len(resources))
	for i, r := range resources {
		idx[r.ID] = i
	}
	for _, n := range native.Interface.Entries {
		resID, ok := ifNameToID[n.fullName()]
		if !ok {
			continue
		}
		ri, ok := idx[resID]
		if !ok {
			continue
		}
		props := resources[ri].Properties
		if props == nil {
			props = map[string]any{}
			resources[ri].Properties = props
		}
		switch {
		case n.Switchport.Mode.Trunk != nil:
			props["port_mode"] = "trunk"
			if vid, err := strconv.Atoi(strings.TrimSpace(n.Switchport.NativeVLAN)); err == nil && vid >= 1 && vid <= 4094 {
				props["native_vlan"] = vid
			}
		case n.Switchport.Mode.Access != nil:
			props["port_mode"] = "access"
		}
		if vid, err := strconv.Atoi(strings.TrimSpace(n.Dot1Q)); err == nil && vid >= 1 && vid <= 4094 {
			props["encapsulation"] = "dot1q"
			props["vlan_id"] = vid
			continue
		}
		if n.XMLName.Local == "Vlan" {
			if vid, err := strconv.Atoi(strings.TrimSpace(n.Name)); err == nil && vid >= 1 && vid <= 4094 {
				props["vlan_id"] = vid
			}
		}
	}
}

// vrfGroupType is the group type of a VRF: a network boundary, in the
// same network.* family as the specification's network.vpc example.
const vrfGroupType = "network.vrf"

// TransformVRFs converts native VRF definitions into network.vrf groups
// scoped to the device with key deviceKey. Returns groups and a map of
// VRF name -> group ID.
func TransformVRFs(deviceKey string, native nativeVRFList) ([]sdk.Group, map[string]string) {
	var groups []sdk.Group
	vrfNameToGroupID := make(map[string]string)

	for _, vrf := range native.VRF.Definitions {
		if vrf.Name == "" {
			continue
		}

		boundaryToken := fmt.Sprintf("%s|vrf-%s", deviceKey, vrf.Name)
		gid := sdk.GroupID(sdk.GroupIDInput{
			Type:          vrfGroupType,
			BoundaryToken: boundaryToken,
		})
		vrfNameToGroupID[vrf.Name] = gid

		g, err := sdk.NewGroup(gid, vrfGroupType)
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

// interfaceVRFs maps each interface name to its VRF. The operational
// binding (interfaces-oper) wins; the configured "vrf forwarding" from
// the native interface configuration is used for interfaces the
// operational model does not report, including when that model failed.
// Interfaces in the global table are absent.
func interfaceVRFs(oper interfacesOper, native nativeInterfaces) map[string]string {
	out := map[string]string{}
	for _, n := range native.Interface.Entries {
		if vrf, name := strings.TrimSpace(n.VRF), n.fullName(); vrf != "" && name != "" {
			out[name] = vrf
		}
	}
	for _, o := range oper.Interfaces {
		name := strings.TrimSpace(o.Name)
		if name == "" {
			continue
		}
		if vrf := strings.TrimSpace(o.VRF); vrf != "" {
			out[name] = vrf
		}
	}
	return out
}

// WireInterfacesToVRFs adds each interface as a member of the VRF group
// ifVRF (see interfaceVRFs) binds it to.
func WireInterfacesToVRFs(ifVRF map[string]string, ifNameToID map[string]string, groups []sdk.Group, nameToGroupID map[string]string) {
	idx := groupIndex(groups)
	for name, vrf := range ifVRF {
		resID, ok := ifNameToID[name]
		if !ok {
			continue
		}
		if gi, ok := idx[nameToGroupID[vrf]]; ok {
			groups[gi].AddMembers(resID)
		}
	}
}
