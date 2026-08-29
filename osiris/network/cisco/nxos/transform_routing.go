// transform_routing.go - Routing-protocol adjacencies:
// "show ip ospf neighbor vrf all" -> network.ospf connections,
// "show bgp all summary" -> network.bgp connections, each to an
// unresolved neighbor stub. No route tables are collected or emitted
// only the session/peer facts.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package nxos

import (
	"fmt"
	"strings"

	"go.osirisjson.org/producers/pkg/sdk"
)

// TransformOSPFNeighbors converts "show ip ospf neighbor vrf all"
// output into network.ospf connections from the local interface each
// neighbor was seen on to an external stub resource for the neighbor
// itself. A neighbor row whose local interface is not already a known
// resource (ifNameToID) is skipped it can never resolve, per this
// producer's report-only-resolvable-relationships policy.
func TransformOSPFNeighbors(deviceID string, ospf ospfNeighborsResponse, ifNameToID map[string]string) ([]sdk.Connection, []sdk.Resource) {
	var connections []sdk.Connection
	var stubs []sdk.Resource
	// seenRouterID guards against the same OSPF router appearing as a
	// neighbor on more than one local interface (a router with several
	// adjacencies to this switch, one per VLAN/interface, is normal and
	// was seen in production) the stub resource's ID is derived from
	// routerID alone, so creating one per row would emit the same ID
	// twice and fail document assembly. One connection per (interface,
	// neighbor) row is still emitted; only the shared target resource
	// is deduplicated.
	seenRouterID := make(map[string]string) // routerID -> resource ID

	for _, ctx := range ospf.TableCtx.RowCtx {
		vrfName := string(ctx.VRFName)
		for _, row := range ctx.TableNbr.RowNbr {
			routerID := string(row.RouterID)
			address := string(row.Address)
			ifName := normalizeIfName(string(row.IfName))
			if routerID == "" || address == "" || ifName == "" {
				continue
			}
			localID, ok := ifNameToID[ifName]
			if !ok {
				continue
			}

			remoteID, ok := seenRouterID[routerID]
			if !ok {
				remoteCanonical := "ospf|" + routerID
				remoteID = unresolvedStubID(deviceID, "ospf-neighbor", routerID)
				remoteProv := sdk.Provider{Name: unknownProviderName, NativeID: remoteCanonical, Source: "ospf"}
				stub, err := sdk.NewResource(remoteID, "network.interface", remoteProv)
				if err != nil {
					continue
				}
				stub.Name = fmt.Sprintf("ospf-neighbor:%s", routerID)
				stub.Status = "unknown"
				stub.Properties = map[string]any{"remote_mgmt_addr": address, "ospf_router_id": routerID}
				stubs = append(stubs, stub)
				seenRouterID[routerID] = remoteID
			}

			const connType = "network.ospf"
			canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
				Type:      connType,
				Direction: "bidirectional",
				Source:    localID,
				Target:    remoteID,
			})
			connID := sdk.BuildConnectionID(canonicalKey, 16)
			conn, err := sdk.NewConnection(connID, connType, localID, remoteID)
			if err != nil {
				continue
			}
			conn.Name = fmt.Sprintf("OSPF %s <-> %s", ifName, routerID)
			conn.Status = "active"
			props := map[string]any{}
			if v := string(row.State); v != "" {
				props["state"] = v
			}
			if v := strings.TrimSpace(string(row.DRState)); v != "" && v != "-" {
				props["dr_state"] = v
			}
			if v := string(row.Priority); v != "" {
				props["priority"] = v
			}
			if v := string(row.UpTime); v != "" {
				props["uptime"] = v
			}
			if vrfName != "" {
				props["vrf"] = vrfName
			}
			if len(props) > 0 {
				conn.Properties = props
			}

			connections = append(connections, conn)
		}
	}

	return connections, stubs
}

// TransformBGPNeighbors converts "show bgp all summary" output into
// network.bgp connections from the switch itself (BGP sessions are
// device-level, not tied to a single named local interface in this
// command's own output) to an external stub resource per peer. Only
// the default VRF's peers are covered "show bgp all summary vrf all"
// (or any other combination tried) is rejected outright by NX-API
// for structured output on this platform; see bgpVRFRow's own doc
// comment in dto.go.
func TransformBGPNeighbors(deviceID string, bgp bgpSummaryResponse) ([]sdk.Connection, []sdk.Resource) {
	var connections []sdk.Connection
	var stubs []sdk.Resource
	// seenNeighborID guards against the same peer IP appearing under
	// more than one address family (a real NX-OS "show bgp all summary"
	// enumerates neighbors per AFI/SAFI table, and a single
	// dual-stack/multi-AF peer configuration can appear in several)
	// both the stub resource ID and the connection ID (device ->
	// neighbor, unqualified by address family) are derived from
	// neighborID alone, so a second row for the same peer would
	// collide on both rather than adding real information. First
	// occurrence wins.
	seenNeighborID := make(map[string]bool)

	for _, vrf := range bgp.TableVRF.RowVRF {
		vrfName := string(vrf.VRFNameOut)
		for _, af := range vrf.TableAf.RowAf {
			for _, saf := range af.TableSaf.RowSaf {
				for _, row := range saf.TableNeighbor.RowNeighbor {
					neighborID := string(row.NeighborID)
					if neighborID == "" || seenNeighborID[neighborID] {
						continue
					}
					seenNeighborID[neighborID] = true

					remoteCanonical := "bgp|" + neighborID
					remoteID := unresolvedStubID(deviceID, "bgp-neighbor", neighborID)
					remoteProv := sdk.Provider{Name: unknownProviderName, NativeID: remoteCanonical, Source: "bgp"}
					stub, err := sdk.NewResource(remoteID, "network.interface", remoteProv)
					if err != nil {
						continue
					}
					stub.Name = fmt.Sprintf("bgp-neighbor:%s", neighborID)
					stub.Status = "unknown"
					props := map[string]any{"remote_mgmt_addr": neighborID}
					if v := string(row.RemoteAS); v != "" {
						props["remote_asn"] = v
					}
					stub.Properties = props
					stubs = append(stubs, stub)

					const connType = "network.bgp"
					canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
						Type:      connType,
						Direction: "bidirectional",
						Source:    deviceID,
						Target:    remoteID,
					})
					connID := sdk.BuildConnectionID(canonicalKey, 16)
					conn, err := sdk.NewConnection(connID, connType, deviceID, remoteID)
					if err != nil {
						continue
					}
					conn.Name = fmt.Sprintf("BGP -> %s", neighborID)
					conn.Status = "active"
					connProps := map[string]any{}
					if v := string(row.State); v != "" {
						connProps["state"] = v
					}
					if v := string(row.PfxReceived); v != "" {
						connProps["prefixes_received"] = v
					}
					if vrfName != "" {
						connProps["vrf"] = vrfName
					}
					if len(connProps) > 0 {
						conn.Properties = connProps
					}

					connections = append(connections, conn)
				}
			}
		}
	}

	return connections, stubs
}
