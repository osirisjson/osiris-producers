// transform_portchannel.go - Port-channel (LAG) bundle membership and
// vPC: "show port-channel summary" -> contains connections + member
// counts, "show vpc brief" -> the osiris.cisco.vpc group, and "show vpc
// peer-keepalive" -> the keepalive control connection.
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
// "group" (bundle number), "port-channel" (LAG interface name), "prtcl"
// (LACP/PAgP/static aggregation protocol, surfaced as the LAG
// resource's properties.protocol) and the member "port" list are read.
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
				if v := string(row.Protocol); v != "" {
					resources[ri].Properties["protocol"] = v
				}
			}
		}
	}

	return connections
}

// TransformVPC converts "show vpc brief" output into a vPC group.
// Returns nil group and empty string if vPC is
// not configured (graceful).
//
// The group type is osiris.cisco.vpc, not network.vpc: a Cisco vPC
// (virtual Port Channel, an L2 multi-chassis link-aggregation domain)
// is unrelated to OSIRIS-JSON-v1.0 7.5.1's network.vpc (a hyperscaler
// Virtual Private Cloud / VNet). Per 4.2.6 / 7.7.2 a vendor-specific
// construct with no standard equivalent takes an osiris.<vendor>.<type>
// namespaced type.
func TransformVPC(hostname string, vpcBrief vpcBriefResponse) (*sdk.Group, string) {
	domainID := string(vpcBrief.DomainID)
	if domainID == "" || domainID == "not configured" {
		return nil, ""
	}

	boundaryToken := fmt.Sprintf("%s|vpc-%s", hostname, domainID)
	gid := sdk.GroupID(sdk.GroupIDInput{
		Type:          "osiris.cisco.vpc",
		BoundaryToken: boundaryToken,
	})

	g, err := sdk.NewGroup(gid, "osiris.cisco.vpc")
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

// TransformVPCKeepalive builds a bare "network" connection (per
// OSIRIS-JSON-v1.0 5.2.3 the standard type for network-layer
// connectivity/reachability) from the switch to an external stub
// resource representing its vPC peer's keepalive endpoint, plus that
// stub resource itself. No standard 5.2.3 subtype fits vPC (a
// proprietary Cisco control-plane heartbeat, not l2/l3/bgp/ospf/vpn),
// so the distinguishing role and the raw device-reported keepalive
// status live under extensions.osiris.cisco instead of a
// producer-invented properties key matching OSIRIS 5.4.3's own worked
// example of vendor-specific connection data
// ("extensions.osiris.cisco: {neighbor_address, soft_reconfiguration}").
// Returns a nil connection and stub when keepalive is
// disabled/unconfigured (no destination reported) graceful, matching
// TransformVPC's own not-configured handling.
//
// Modeled as its own connection, not a property on the vPC group,
// specifically so it is never conflated with the peer-link/vPC-member
// relationships TransformVPC/WirePortChannelsToVPC already cover a
// distinct control-plane relationship gets a distinct edge in the
// topology graph.
//
// UNVERIFIED field names see vpcPeerKeepaliveResponse's own doc
// comment in dto.go.
func TransformVPCKeepalive(deviceID, deviceName string, keepalive vpcPeerKeepaliveResponse) (*sdk.Connection, *sdk.Resource) {
	dest := strings.TrimSpace(string(keepalive.Destination))
	if dest == "" || dest == "N/A" {
		return nil, nil
	}

	remoteCanonical := "vpc-peer-keepalive|" + dest
	remoteID := unresolvedStubID(deviceID, "vpc-peer-keepalive", dest)
	remoteProv := sdk.Provider{Name: unknownProviderName, NativeID: remoteCanonical, Source: "vpc-keepalive"}
	stub, err := sdk.NewResource(remoteID, "network.interface", remoteProv)
	if err != nil {
		return nil, nil
	}
	stub.Name = fmt.Sprintf("vpc-peer-keepalive:%s", dest)
	stub.Status = "unknown"
	props := map[string]any{"remote_mgmt_addr": dest}
	if v := string(keepalive.VRF); v != "" {
		props["vrf"] = v
	}
	stub.Properties = props

	const connType = "network"
	canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
		Type:      connType,
		Direction: "bidirectional",
		Source:    deviceID,
		Target:    remoteID,
	})
	connID := sdk.BuildConnectionID(canonicalKey, 16)
	conn, err := sdk.NewConnection(connID, connType, deviceID, remoteID)
	if err != nil {
		return nil, nil
	}
	conn.Name = fmt.Sprintf("%s vPC keepalive -> %s", deviceName, dest)
	conn.Status = "active"
	ensureCiscoExtension(&conn.Extensions)
	cisco := conn.Extensions[extensionNamespace].(map[string]any)
	cisco["role"] = "vpc_keepalive"
	if v := string(keepalive.Status); v != "" {
		cisco["keepalive_status"] = v
	}

	return &conn, &stub
}
