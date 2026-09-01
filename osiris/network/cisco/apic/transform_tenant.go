// transform_tenant.go - APIC tenant object model. Maps fvTenant, fvCtx,
// fvBD, fvSubnet, fvAEPg and l3extOut into OSIRIS groups and resources,
// and wires the ACI containment hierarchy (Rs* relationship classes and
// parent-DN membership) into group members and children.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package apic

import (
	"net"
	"strings"

	"go.osirisjson.org/producers/pkg/sdk"
)

// TransformTenants converts fvTenant attributes into OSIRIS groups.
// Returns the groups and a map of tenant DN -> group ID for use by child transforms.
func TransformTenants(tenants []map[string]any) ([]sdk.Group, map[string]string) {
	var groups []sdk.Group
	dnToID := make(map[string]string, len(tenants))

	for _, t := range tenants {
		dn := str(t, "dn")
		name := str(t, "name")

		gid := sdk.GroupID(sdk.GroupIDInput{
			Type:          "logical.tenant",
			BoundaryToken: dn,
		})
		dnToID[dn] = gid

		g, err := sdk.NewGroup(gid, "logical.tenant")
		if err != nil {
			continue
		}
		g.Name = name
		if d := str(t, "descr"); d != "" {
			g.Description = d
		}
		groups = append(groups, g)
	}
	return groups, dnToID
}

// TransformVRFs converts fvCtx (VRF) attributes into OSIRIS groups.
// Returns the groups and a map of VRF DN -> group ID.
func TransformVRFs(vrfs []map[string]any) ([]sdk.Group, map[string]string) {
	var groups []sdk.Group
	dnToID := make(map[string]string, len(vrfs))

	for _, v := range vrfs {
		dn := str(v, "dn")
		name := str(v, "name")

		gid := sdk.GroupID(sdk.GroupIDInput{
			Type:          "logical.vrf",
			BoundaryToken: dn,
		})
		dnToID[dn] = gid

		g, err := sdk.NewGroup(gid, "logical.vrf")
		if err != nil {
			continue
		}
		g.Name = name
		if d := str(v, "descr"); d != "" {
			g.Description = d
		}

		props := map[string]any{}
		if pref := str(v, "pcEnfPref"); pref != "" {
			props["enforcement"] = pref
		}
		if len(props) > 0 {
			g.Properties = props
		}

		groups = append(groups, g)
	}
	return groups, dnToID
}

// TransformBridgeDomains converts fvBD attributes into OSIRIS resources.
// Returns resources and a map of BD DN -> resource ID.
func TransformBridgeDomains(bds []map[string]any) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	dnToID := make(map[string]string, len(bds))

	for _, bd := range bds {
		dn := str(bd, "dn")
		name := str(bd, "name")

		id := resourceID(dn)
		dnToID[dn] = id

		prov := sdk.Provider{
			Name:     providerName,
			NativeID: dn,
		}

		r, err := sdk.NewResource(id, "osiris.cisco.domain.bridge", prov)
		if err != nil {
			continue
		}
		r.Name = name
		if d := str(bd, "descr"); d != "" {
			r.Description = d
		}
		r.Status = "active"

		props := map[string]any{}
		if v := str(bd, "unicastRoute"); v != "" {
			props["unicast_routing"] = v
		}
		if v := str(bd, "unkMacUcastAct"); v != "" {
			props["l2_unknown_unicast"] = v
		}
		if v := str(bd, "arpFlood"); v != "" {
			props["arp_flood"] = v
		}
		if v := str(bd, "mac"); v != "" {
			props["mac"] = v
		}
		if len(props) > 0 {
			r.Properties = props
		}

		resources = append(resources, r)
	}
	return resources, dnToID
}

// TransformSubnets converts fvSubnet attributes into OSIRIS resources.
// fvSubnet.ip is an "<gateway>/<prefix>" pair: it is normalized into
// properties.cidr (the network prefix) and properties.gateway_ip (the
// host address). The ACI routing scope (fvSubnet.scope) expresses
// advertisement behavior, not Internet reachability, so it is kept in
// the vendor extension and never mapped to a public/private property.
func TransformSubnets(subnets []map[string]any) []sdk.Resource {
	var resources []sdk.Resource
	for _, s := range subnets {
		dn := str(s, "dn")
		ip := str(s, "ip")
		cidr, gateway := subnetCIDR(ip)

		id := resourceID(dn)
		prov := sdk.Provider{
			Name:     providerName,
			NativeID: dn,
		}

		r, err := sdk.NewResource(id, "network.subnet", prov)
		if err != nil {
			continue
		}
		r.Name = cidr
		r.Status = "active"

		props := map[string]any{}
		if cidr != "" {
			props["cidr"] = cidr
		}
		if gateway != "" {
			props["gateway_ip"] = gateway
		}
		r.Properties = props

		ext := map[string]any{}
		if v := str(s, "scope"); v != "" {
			ext["aci_scope"] = v
		}
		if v := str(s, "preferred"); v != "" {
			ext["preferred"] = v
		}
		if len(ext) > 0 {
			r.Extensions = map[string]any{extensionNamespace: ext}
		}

		resources = append(resources, r)
	}
	return resources
}

// TransformEPGs converts fvAEPg attributes into OSIRIS groups.
// Returns groups and a map of EPG DN -> group ID.
func TransformEPGs(epgs []map[string]any) ([]sdk.Group, map[string]string) {
	var groups []sdk.Group
	dnToID := make(map[string]string, len(epgs))

	for _, e := range epgs {
		dn := str(e, "dn")
		name := str(e, "name")

		gid := sdk.GroupID(sdk.GroupIDInput{
			Type:          "osiris.cisco.epg",
			BoundaryToken: dn,
		})
		dnToID[dn] = gid

		g, err := sdk.NewGroup(gid, "osiris.cisco.epg")
		if err != nil {
			continue
		}
		g.Name = name
		if d := str(e, "descr"); d != "" {
			g.Description = d
		}
		groups = append(groups, g)
	}
	return groups, dnToID
}

// TransformL3Outs converts l3extOut attributes into OSIRIS resources.
// Dummy L3Outs (name starting with __ui_svi_dummy_id_) are skipped.
// L3Outs represent external routing boundaries modeled as resources
// within their parent tenant scope, not as connections.
// Returns resources and a map of L3Out DN -> resource ID.
func TransformL3Outs(l3outs []map[string]any) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	dnToID := make(map[string]string, len(l3outs))

	for _, l := range l3outs {
		name := str(l, "name")
		if strings.HasPrefix(name, "__ui_svi_dummy_id_") {
			continue
		}
		dn := str(l, "dn")

		id := resourceID(dn)
		dnToID[dn] = id

		prov := sdk.Provider{
			Name:     providerName,
			NativeID: dn,
		}

		r, err := sdk.NewResource(id, "osiris.cisco.l3out", prov)
		if err != nil {
			continue
		}
		r.Name = name
		if d := str(l, "descr"); d != "" {
			r.Description = d
		}
		r.Status = "active"
		resources = append(resources, r)
	}
	return resources, dnToID
}

// WireBDsToVRFs adds BD resource IDs as members of their associated
// VRF groups.
// Uses fvRsCtx relationship class (DN: .../BD-Y/rsctx, tDn: .../ctx-Z).
func WireBDsToVRFs(bdToCtxAttrs []map[string]any, bdDNToID map[string]string, vrfDNToID map[string]string, vrfGroups []sdk.Group) {
	idx := groupIndex(vrfGroups)
	for _, rel := range bdToCtxAttrs {
		dn := str(rel, "dn")
		tDn := str(rel, "tDn")
		if dn == "" || tDn == "" {
			continue
		}

		bdDN := extractParentDN(dn, "/rsctx")
		if bdDN == "" {
			continue
		}

		bdID, ok := bdDNToID[bdDN]
		if !ok {
			continue
		}
		vrfID, ok := vrfDNToID[tDn]
		if !ok {
			continue
		}

		if i, ok := idx[vrfID]; ok {
			vrfGroups[i].AddMembers(bdID)
		}
	}
}

// WireL3OutsToVRFs adds L3Out resource IDs as members of their
// associated VRF groups.
// Uses l3extRsEctx relationship class
// (DN: .../out-Y/rsectx, tDn: .../ctx-Z).
func WireL3OutsToVRFs(l3outToCtxAttrs []map[string]any, l3outDNToID map[string]string, vrfDNToID map[string]string, vrfGroups []sdk.Group) {
	idx := groupIndex(vrfGroups)
	for _, rel := range l3outToCtxAttrs {
		dn := str(rel, "dn")
		tDn := str(rel, "tDn")
		if dn == "" || tDn == "" {
			continue
		}

		l3outDN := extractParentDN(dn, "/rsectx")
		if l3outDN == "" {
			continue
		}

		l3outID, ok := l3outDNToID[l3outDN]
		if !ok {
			continue
		}
		vrfID, ok := vrfDNToID[tDn]
		if !ok {
			continue
		}

		if i, ok := idx[vrfID]; ok {
			vrfGroups[i].AddMembers(l3outID)
		}
	}
}

// WireEPGsToBDs adds BD resource IDs as members of their associated
// EPG groups.
// Uses fvRsBd relationship class (DN: .../epg-Z/rsbd, tDn: .../BD-W).
func WireEPGsToBDs(epgToBdAttrs []map[string]any, epgDNToID map[string]string, bdDNToID map[string]string, epgGroups []sdk.Group) {
	idx := groupIndex(epgGroups)
	for _, rel := range epgToBdAttrs {
		dn := str(rel, "dn")
		tDn := str(rel, "tDn")
		if dn == "" || tDn == "" {
			continue
		}

		epgDN := extractParentDN(dn, "/rsbd")
		if epgDN == "" {
			continue
		}

		epgID, ok := epgDNToID[epgDN]
		if !ok {
			continue
		}
		bdID, ok := bdDNToID[tDn]
		if !ok {
			continue
		}

		if i, ok := idx[epgID]; ok {
			epgGroups[i].AddMembers(bdID)
		}
	}
}

// WireBDsToTenants adds BD resource IDs as members of their
// parent tenant groups.
func WireBDsToTenants(bdAttrs []map[string]any, bdDNToID, tenantDNToID map[string]string, tenantGroups []sdk.Group) {
	idx := groupIndex(tenantGroups)
	for _, bd := range bdAttrs {
		dn := str(bd, "dn")
		bdID, ok := bdDNToID[dn]
		if !ok {
			continue
		}
		tenantDN := extractTenantDN(dn)
		parentID, ok := tenantDNToID[tenantDN]
		if !ok {
			continue
		}
		if i, ok := idx[parentID]; ok {
			tenantGroups[i].AddMembers(bdID)
		}
	}
}

// WireSubnetsToTenants adds subnet resource IDs as members of their
// parent tenant groups.
func WireSubnetsToTenants(subnetAttrs []map[string]any, tenantDNToID map[string]string, tenantGroups []sdk.Group) {
	idx := groupIndex(tenantGroups)
	for _, s := range subnetAttrs {
		dn := str(s, "dn")
		subnetID := resourceID(dn)
		tenantDN := extractTenantDN(dn)
		parentID, ok := tenantDNToID[tenantDN]
		if !ok {
			continue
		}
		if i, ok := idx[parentID]; ok {
			tenantGroups[i].AddMembers(subnetID)
		}
	}
}

// WireVRFsToTenants adds VRF group IDs as children of their
// parent tenant groups.
func WireVRFsToTenants(vrfAttrs []map[string]any, vrfDNToID, tenantDNToID map[string]string, tenantGroups []sdk.Group) {
	idx := groupIndex(tenantGroups)
	for _, v := range vrfAttrs {
		dn := str(v, "dn")
		vrfID, ok := vrfDNToID[dn]
		if !ok {
			continue
		}
		tenantDN := extractTenantDN(dn)
		parentID, ok := tenantDNToID[tenantDN]
		if !ok {
			continue
		}
		if i, ok := idx[parentID]; ok {
			tenantGroups[i].AddChildren(vrfID)
		}
	}
}

// WireEPGsToTenants adds EPG group IDs as children of their
// parent tenant groups.
func WireEPGsToTenants(epgAttrs []map[string]any, epgDNToID, tenantDNToID map[string]string, tenantGroups []sdk.Group) {
	idx := groupIndex(tenantGroups)
	for _, e := range epgAttrs {
		dn := str(e, "dn")
		epgID, ok := epgDNToID[dn]
		if !ok {
			continue
		}
		tenantDN := extractTenantDN(dn)
		parentID, ok := tenantDNToID[tenantDN]
		if !ok {
			continue
		}
		if i, ok := idx[parentID]; ok {
			tenantGroups[i].AddChildren(epgID)
		}
	}
}

// WireL3OutsToTenants adds L3Out resource IDs as members of their
// parent tenant groups.
func WireL3OutsToTenants(l3outAttrs []map[string]any, tenantDNToID map[string]string, tenantGroups []sdk.Group) {
	idx := groupIndex(tenantGroups)
	for _, l := range l3outAttrs {
		name := str(l, "name")
		if strings.HasPrefix(name, "__ui_svi_dummy_id_") {
			continue
		}
		dn := str(l, "dn")
		l3outID := resourceID(dn)
		tenantDN := extractTenantDN(dn)
		parentID, ok := tenantDNToID[tenantDN]
		if !ok {
			continue
		}
		if i, ok := idx[parentID]; ok {
			tenantGroups[i].AddMembers(l3outID)
		}
	}
}

// subnetCIDR normalizes an fvSubnet "ip" value (an "<address>/<prefix>"
// pair where the address is the gateway, not the network address) into
// the network prefix and the gateway host address. On a parse failure
// it returns the raw value as the CIDR and an empty gateway.
func subnetCIDR(ip string) (cidr, gateway string) {
	gwIP, ipNet, err := net.ParseCIDR(ip)
	if err != nil {
		return ip, ""
	}
	return ipNet.String(), gwIP.String()
}
