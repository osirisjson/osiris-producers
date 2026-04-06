// transform.go - Pure APIC->OSIRIS mapping functions.
// Converts APIC attribute maps (from class queries) into SDK types.
// All functions are stateless: no I/O, no HTTP, just data transformation.
//
// For an introduction to OSIRIS JSON Producer for Cisco see:
// "[OSIRIS-JSON-CISCO]."
//
// [OSIRIS-JSON-CISCO]: https://osirisjson.org/en/docs/producers/network/cisco

package apic

import (
	"fmt"
	"strconv"
	"strings"

	"go.osirisjson.org/producers/pkg/sdk"
)

const extensionNamespace = "osiris.cisco"

const providerName = "cisco"

// nodeRoleToType maps APIC fabricNode role values to OSIRIS resource types.
var nodeRoleToType = map[string]string{
	"controller": "osiris.cisco.controller",
	"spine":      "osiris.cisco.switch.spine",
	"leaf":       "osiris.cisco.switch.leaf",
}

// TransformNodes converts fabricNode attributes (merged with topSystem and firmware)
// into OSIRIS resources. The systems and firmware slices are matched by DN prefix
// (topology/pod-N/node-N).
func TransformNodes(nodes, systems, firmware []map[string]any) []sdk.Resource {
	sysMap := indexByDNPrefix(systems)
	fwMap := indexByDNPrefix(firmware)

	var resources []sdk.Resource
	for _, n := range nodes {
		dn := str(n, "dn")
		role := str(n, "role")
		resType, ok := nodeRoleToType[role]
		if !ok {
			continue
		}

		name := str(n, "name")
		id := resourceID(resType, dn)

		prov := sdk.Provider{
			Name:     providerName,
			NativeID: dn,
			Type:     str(n, "model"),
			Version:  str(n, "version"),
			Site:     str(n, "fabricSt"),
		}

		r, err := sdk.NewResource(id, resType, prov)
		if err != nil {
			continue
		}
		r.Name = name

		// Map fabricSt to OSIRIS status, falling back to topSystem state
		// for controllers where fabricSt is often empty/unknown.
		r.Status = mapNodeStatus(str(n, "fabricSt"))
		if r.Status == "unknown" {
			if sys, ok := sysMap[dnPrefix(dn)]; ok {
				if st := str(sys, "state"); st == "in-service" {
					r.Status = "active"
				}
			}
		}

		props := map[string]any{
			"serial":  str(n, "serial"),
			"model":   str(n, "model"),
			"address": str(n, "address"),
			"node_id": str(n, "id"),
			"pod":     extractPod(dn),
		}

		// Merge topSystem attributes.
		if sys, ok := sysMap[dnPrefix(dn)]; ok {
			if v := str(sys, "oobMgmtAddr"); v != "" {
				props["oob_mgmt_addr"] = v
			}
			if v := str(sys, "inbMgmtAddr"); v != "" {
				props["inb_mgmt_addr"] = v
			}
			if v := str(sys, "systemUpTime"); v != "" {
				props["uptime"] = v
			}
			if v := str(sys, "state"); v != "" {
				props["system_state"] = v
			}
			if v := str(sys, "fabricDomain"); v != "" {
				props["fabric_domain"] = v
			}
		}

		// Merge firmware version.
		if fw, ok := fwMap[dnPrefix(dn)]; ok {
			if v := str(fw, "version"); v != "" {
				props["firmware_version"] = v
			}
			if v := str(fw, "peVer"); v != "" {
				prov.Version = v
				r.Provider = prov
			}
		}

		// ACI-specific extensions (osiris.cisco).
		if sys, ok := sysMap[dnPrefix(dn)]; ok {
			ext := make(map[string]any)
			if v := str(sys, "fabricMAC"); v != "" {
				ext["fabric_mac"] = v
			}
			if v := str(sys, "controlPlaneMTU"); v != "" {
				if mtu, err := strconv.Atoi(v); err == nil {
					ext["control_plane_mtu"] = mtu
				}
			}
			if v := str(sys, "lastRebootTime"); v != "" {
				ext["last_reboot_time"] = v
			}
			if v := str(sys, "fabricId"); v != "" {
				if fid, err := strconv.Atoi(v); err == nil {
					ext["fabric_id"] = fid
				}
			}
			if len(ext) > 0 {
				r.Extensions = map[string]any{extensionNamespace: ext}
			}
		}

		r.Properties = props
		resources = append(resources, r)
	}
	return resources
}

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

		id := resourceID("osiris.cisco.domain.bridge", dn)
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
func TransformSubnets(subnets []map[string]any) []sdk.Resource {
	var resources []sdk.Resource
	for _, s := range subnets {
		dn := str(s, "dn")
		ip := str(s, "ip")

		id := resourceID("network.subnet", dn)
		prov := sdk.Provider{
			Name:     providerName,
			NativeID: dn,
		}

		r, err := sdk.NewResource(id, "network.subnet", prov)
		if err != nil {
			continue
		}
		r.Name = ip
		r.Status = "active"

		props := map[string]any{
			"ip":    ip,
			"scope": str(s, "scope"),
		}
		if v := str(s, "preferred"); v != "" {
			props["preferred"] = v
		}
		r.Properties = props
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

// TransformEndpoints converts fvCEp attributes into OSIRIS resources (detailed mode only).
func TransformEndpoints(endpoints []map[string]any) []sdk.Resource {
	var resources []sdk.Resource
	for _, ep := range endpoints {
		dn := str(ep, "dn")
		mac := str(ep, "mac")

		id := resourceID("osiris.cisco.endpoint", dn)
		prov := sdk.Provider{
			Name:     providerName,
			NativeID: dn,
		}

		r, err := sdk.NewResource(id, "osiris.cisco.endpoint", prov)
		if err != nil {
			continue
		}
		r.Name = mac
		r.Status = "active"

		props := map[string]any{
			"mac":   sdk.NormalizeMAC(mac),
			"encap": str(ep, "encap"),
		}
		if v := str(ep, "fabricPathDn"); v != "" {
			props["fabric_path"] = v
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources
}

// TransformL3Outs converts l3extOut attributes into OSIRIS resources.
// Dummy L3Outs (name starting with __ui_svi_dummy_id_) are skipped.
// L3Outs represent external routing boundaries - modeled as resources within
// their parent tenant scope, not as connections.
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

		id := resourceID("osiris.cisco.l3out", dn)
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

// Relationship wiring from ACI relationship classes (Rs*).
// In ACI, VRFs and EPGs are modeled as OSIRIS groups. Since OSIRIS connections
// require resource endpoints, BD->VRF and L3Out->VRF relationships are modeled
// as group membership (resources become members of their VRF group).

// WireBDsToVRFs adds BD resource IDs as members of their associated VRF groups.
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

// WireL3OutsToVRFs adds L3Out resource IDs as members of their associated VRF groups.
// Uses l3extRsEctx relationship class (DN: .../out-Y/rsectx, tDn: .../ctx-Z).
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

// WireEPGsToBDs adds BD resource IDs as members of their associated EPG groups.
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

// extractParentDN strips a known suffix from a DN to get the parent object DN.
func extractParentDN(dn, suffix string) string {
	if !strings.HasSuffix(dn, suffix) {
		return ""
	}
	return dn[:len(dn)-len(suffix)]
}

// extractLastSegment returns the last path segment of a DN.
func extractLastSegment(dn string) string {
	idx := strings.LastIndex(dn, "/")
	if idx < 0 {
		return dn
	}
	return dn[idx+1:]
}

// Group membership wiring.
// These functions wire the APIC containment hierarchy into OSIRIS group
// members and children. This is what makes the document answer "how it relates".

// WireBDsToTenants adds BD resource IDs as members of their parent tenant groups.
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

// WireSubnetsToTenants adds subnet resource IDs as members of their parent tenant groups.
func WireSubnetsToTenants(subnetAttrs []map[string]any, tenantDNToID map[string]string, tenantGroups []sdk.Group) {
	idx := groupIndex(tenantGroups)
	for _, s := range subnetAttrs {
		dn := str(s, "dn")
		subnetID := resourceID("network.subnet", dn)
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

// WireVRFsToTenants adds VRF group IDs as children of their parent tenant groups.
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

// WireEPGsToTenants adds EPG group IDs as children of their parent tenant groups.
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

// WireL3OutsToTenants adds L3Out resource IDs as members of their parent tenant groups.
func WireL3OutsToTenants(l3outAttrs []map[string]any, tenantDNToID map[string]string, tenantGroups []sdk.Group) {
	idx := groupIndex(tenantGroups)
	for _, l := range l3outAttrs {
		name := str(l, "name")
		if strings.HasPrefix(name, "__ui_svi_dummy_id_") {
			continue
		}
		dn := str(l, "dn")
		l3outID := resourceID("osiris.cisco.l3out", dn)
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

// WireEndpointsToEPGs adds endpoint resource IDs as members of their parent EPG groups.
// Endpoint DN format: uni/tn-NAME/ap-NAME/epg-NAME/cep-MAC
// EPG DN format: uni/tn-NAME/ap-NAME/epg-NAME
func WireEndpointsToEPGs(endpointAttrs []map[string]any, epgDNToID map[string]string, epgGroups []sdk.Group) {
	idx := groupIndex(epgGroups)
	for _, ep := range endpointAttrs {
		dn := str(ep, "dn")
		epID := resourceID("osiris.cisco.endpoint", dn)
		epgDN := extractEPGDN(dn)
		parentID, ok := epgDNToID[epgDN]
		if !ok {
			continue
		}
		if i, ok := idx[parentID]; ok {
			epgGroups[i].AddMembers(epID)
		}
	}
}

// Fault extensions.
// Fault represents a curated APIC fault for extensions.
type Fault struct {
	Code           string `json:"code"`
	Severity       string `json:"severity"`
	Cause          string `json:"cause"`
	Description    string `json:"description"`
	Created        string `json:"created"`
	LastTransition string `json:"last_transition"`
	Lifecycle      string `json:"lifecycle"`
	Domain         string `json:"domain"`
	Subject        string `json:"subject"`
}

// TransformFaults groups non-cleared faults by DN prefix.
// Node faults are keyed by "topology/pod-N/node-N" (3 segments).
// Tenant faults are keyed by "uni/tn-NAME" (2 segments).
// Other faults are skipped (no resource/group to attach to).
func TransformFaults(faults []map[string]any) map[string][]Fault {
	result := make(map[string][]Fault)
	for _, f := range faults {
		// Filter out cleared faults - snapshot documents "what's wrong now".
		if str(f, "severity") == "cleared" {
			continue
		}

		fault := Fault{
			Code:           str(f, "code"),
			Severity:       str(f, "severity"),
			Cause:          str(f, "cause"),
			Description:    str(f, "descr"),
			Created:        str(f, "created"),
			LastTransition: str(f, "lastTransition"),
			Lifecycle:      str(f, "lc"),
			Domain:         str(f, "domain"),
			Subject:        str(f, "subject"),
		}

		dn := str(f, "dn")
		prefix := faultDNPrefix(dn)
		if prefix == "" {
			continue
		}
		result[prefix] = append(result[prefix], fault)
	}
	return result
}

// WireFaultsToNodes attaches faults to node resources via their DN prefix.
// Mutates resources in-place, merging into extensions["osiris.cisco"]["faults"].
func WireFaultsToNodes(resources []sdk.Resource, faultsByDN map[string][]Fault) {
	for i := range resources {
		dn := resources[i].Provider.NativeID
		prefix := dnPrefix(dn)
		faults, ok := faultsByDN[prefix]
		if !ok || len(faults) == 0 {
			continue
		}
		ensureCiscoExtension(&resources[i].Extensions)
		resources[i].Extensions[extensionNamespace].(map[string]any)["faults"] = faults
	}
}

// WireFaultsToTenants attaches faults to tenant groups via their DN.
// Mutates groups in-place, setting extensions["osiris.cisco"]["faults"].
func WireFaultsToTenants(groups []sdk.Group, tenantDNToID map[string]string, faultsByDN map[string][]Fault) {
	// Build reverse map: group ID -> index.
	idx := groupIndex(groups)

	for dn, gid := range tenantDNToID {
		faults, ok := faultsByDN[dn]
		if !ok || len(faults) == 0 {
			continue
		}
		i, ok := idx[gid]
		if !ok {
			continue
		}
		ensureCiscoExtension(&groups[i].Extensions)
		groups[i].Extensions[extensionNamespace].(map[string]any)["faults"] = faults
	}
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

// faultDNPrefix extracts the relevant parent prefix from a fault DN.
// Node faults: "topology/pod-N/node-N" from longer topology DNs.
// Tenant faults: "uni/tn-NAME" from uni/tn-* DNs.
// Returns empty string for unrecognized patterns.
func faultDNPrefix(dn string) string {
	if strings.HasPrefix(dn, "topology/") {
		// Extract topology/pod-N/node-N (first 3 segments).
		parts := strings.SplitN(dn, "/", 4)
		if len(parts) >= 3 && strings.HasPrefix(parts[1], "pod-") && strings.HasPrefix(parts[2], "node-") {
			return strings.Join(parts[:3], "/")
		}
		return ""
	}
	if strings.HasPrefix(dn, "uni/tn-") {
		// Extract uni/tn-NAME (first 2 segments).
		return extractTenantDN(dn)
	}
	return ""
}

// Helpers.
// str safely extracts a string value from an attribute map.
func str(attrs map[string]any, key string) string {
	if v, ok := attrs[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// resourceID generates a deterministic resource ID from type and APIC DN.
func resourceID(resType, dn string) string {
	canonicalKey := fmt.Sprintf("v1|%s|%s", resType, dn)
	hash := sdk.Hash16(canonicalKey)
	hint := sdk.DeriveHint(dn, hash)
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

// indexByDNPrefix indexes a slice of attribute maps by the DN prefix
// (topology/pod-N/node-N), stripping any trailing path components.
func indexByDNPrefix(items []map[string]any) map[string]map[string]any {
	m := make(map[string]map[string]any, len(items))
	for _, item := range items {
		dn := str(item, "dn")
		prefix := dnPrefix(dn)
		if prefix != "" {
			m[prefix] = item
		}
	}
	return m
}

// dnPrefix extracts "topology/pod-N/node-N" from a longer DN.
func dnPrefix(dn string) string {
	// DN format: topology/pod-N/node-N[/...]
	parts := strings.SplitN(dn, "/", 4)
	if len(parts) >= 3 && strings.HasPrefix(parts[0], "topology") {
		return strings.Join(parts[:3], "/")
	}
	return dn
}

// extractPod extracts the pod number from a DN like "topology/pod-1/node-101".
func extractPod(dn string) string {
	parts := strings.Split(dn, "/")
	for _, p := range parts {
		if strings.HasPrefix(p, "pod-") {
			return strings.TrimPrefix(p, "pod-")
		}
	}
	return ""
}

// extractTenantDN extracts the tenant DN (uni/tn-NAME) from a child DN.
func extractTenantDN(dn string) string {
	// DN format: uni/tn-NAME/...
	idx := strings.Index(dn, "/tn-")
	if idx < 0 {
		return ""
	}
	rest := dn[idx+1:] // "tn-NAME/..."
	if slashIdx := strings.Index(rest, "/"); slashIdx >= 0 {
		return dn[:idx+1+slashIdx]
	}
	return dn
}

// extractEPGDN extracts the EPG DN from an endpoint DN.
// Endpoint DN: uni/tn-NAME/ap-NAME/epg-NAME/cep-MAC
// EPG DN: uni/tn-NAME/ap-NAME/epg-NAME
func extractEPGDN(dn string) string {
	idx := strings.LastIndex(dn, "/cep-")
	if idx < 0 {
		return ""
	}
	return dn[:idx]
}

// mapNodeStatus converts APIC fabricSt to OSIRIS status values.
func mapNodeStatus(fabricSt string) string {
	switch fabricSt {
	case "active":
		return "active"
	case "inactive":
		return "inactive"
	case "disabled":
		return "inactive"
	case "unknown":
		return "unknown"
	default:
		return "unknown"
	}
}
