// transform_fabric.go - APIC fabric-node mapping. Merges fabricNode
// with topSystem and firmwareRunning into OSIRIS network.switch
// (leaf/spine) and compute.server/compute.vm (APIC controller)
// resources, with the node-status and pod-number helpers used only here.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package apic

import (
	"strconv"
	"strings"

	"go.osirisjson.org/producers/pkg/sdk"
)

// nodeRoleToType maps APIC fabricNode role values to OSIRIS resource
// types. Leaf and spine are the core network.switch type with the role
// kept in properties.role. The controller has no fixed entry: a
// physical APIC appliance is a bare-metal server and a virtual APIC
// (vAPIC) is a VM, so its type is resolved per node by controllerType.
var nodeRoleToType = map[string]string{
	"spine": "network.switch",
	"leaf":  "network.switch",
}

// controllerType returns the OSIRIS type for an APIC controller node.
// A physical APIC appliance maps to compute.server; a virtual APIC
// (vAPIC) maps to compute.vm, told apart by the topSystem.virtualMode
// flag ("yes" for a VM). With no topSystem match it defaults to
// compute.server, the common on-premise case.
func controllerType(sys map[string]any) string {
	if str(sys, "virtualMode") == "yes" {
		return "compute.vm"
	}
	return "compute.server"
}

// nodeType resolves the OSIRIS resource type for a fabricNode role,
// using the merged topSystem record for the controller virtual/physical
// split. The second return is false for a role that is not mapped.
func nodeType(role string, sys map[string]any) (string, bool) {
	if role == "controller" {
		return controllerType(sys), true
	}
	t, ok := nodeRoleToType[role]
	return t, ok
}

// TransformNodes converts fabricNode attributes (merged with topSystem
// and firmware) into OSIRIS resources. The systems and firmware slices
// are matched by DN prefix (topology/pod-N/node-N).
func TransformNodes(nodes, systems, firmware []map[string]any) []sdk.Resource {
	sysMap := indexByDNPrefix(systems)
	fwMap := indexByDNPrefix(firmware)

	var resources []sdk.Resource
	for _, n := range nodes {
		dn := str(n, "dn")
		role := str(n, "role")
		resType, ok := nodeType(role, sysMap[dnPrefix(dn)])
		if !ok {
			continue
		}

		name := str(n, "name")
		id := resourceID(dn)

		prov := sdk.Provider{
			Name:     providerName,
			NativeID: dn,
			Type:     "fabricNode",
			Version:  str(n, "version"),
		}

		r, err := sdk.NewResource(id, resType, prov)
		if err != nil {
			continue
		}
		r.Name = name

		// Map fabricSt to OSIRIS status, falling back to topSystem
		// state for controllers where fabricSt is often empty/unknown.
		r.Status = mapNodeStatus(str(n, "fabricSt"))
		if r.Status == "unknown" {
			if sys, ok := sysMap[dnPrefix(dn)]; ok {
				if st := str(sys, "state"); st == "in-service" {
					r.Status = "active"
				}
			}
		}

		props := map[string]any{
			"manufacturer": "Cisco",
			"role":         role,
			"serial":       str(n, "serial"),
			"model":        str(n, "model"),
			"address":      str(n, "address"),
			"node_id":      str(n, "id"),
			"pod":          extractPod(dn),
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

// extractPod extracts the pod number from a DN like
// "topology/pod-1/node-101".
func extractPod(dn string) string {
	parts := strings.Split(dn, "/")
	for _, p := range parts {
		if strings.HasPrefix(p, "pod-") {
			return strings.TrimPrefix(p, "pod-")
		}
	}
	return ""
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
