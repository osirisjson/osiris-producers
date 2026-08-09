// transform_connections.go - SD-WAN tunnel and OMP control-plane
// peering connection transforms.
//
// vmanage.go writes one OSIRIS JSON document per site (grouped by
// device site-id). OSIRIS-JSON-v1.0 section 2.2.3 requires a
// connection's source and target to both resolve within the same
// document, so every function here only emits a connection when both
// endpoints are already present in the current site's resource index
// a peer belonging to a different site is silently skipped (logged by
// the caller, not here, since these are pure transform functions with
// no I/O), never fabricated as a stub. See CHANGELOG.md's "Known
// limitations".
//
// OSIRIS JSON Producer for Cisco SD-WAN Manager (vManage) introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package vmanage

import (
	"regexp"
	"strings"

	"go.osirisjson.org/producers/pkg/sdk"
)

// tunnelNameRe parses a GET /dataservice/topology/monitor/site/{siteId}
// tunnel "name" field: "{ipA}:{colorA}-{ipB}:{colorB}". colorA/colorB
// are matched non-greedily since real vManage color names commonly
// contain hyphens themselves (e.g. "biz-internet", "public-internet") -
// a naive strings.Split on "-" would misparse those.
var tunnelNameRe = regexp.MustCompile(`^(\d{1,3}(?:\.\d{1,3}){3}):([a-z0-9-]+?)-(\d{1,3}(?:\.\d{1,3}){3}):([a-z0-9-]+)$`)

// parseTunnelEndpoints splits a tunnel name into its two
// "{system-ip}:{color}" endpoint keys, matching the index keys
// TransformInterfaces builds in its returned tunnelIndex.
func parseTunnelEndpoints(name string) (keyA, keyB string, ok bool) {
	m := tunnelNameRe.FindStringSubmatch(name)
	if m == nil {
		return "", "", false
	}
	return m[1] + ":" + m[2], m[3] + ":" + m[4], true
}

// TransformTunnels converts GET
// /dataservice/topology/monitor/site/{siteId} circuit/tunnel data into
// network.vpn connections between the two WAN-transport interfaces the
// tunnel runs over. ifaceIndex is the merged "{system-ip}:{color}" ->
// interface resource ID index from every device's TransformInterfaces
// call for the current site.
func TransformTunnels(devices []SiteTopologyDevice, ifaceIndex map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	seen := make(map[string]bool)

	for _, dev := range devices {
		for _, circuit := range dev.Circuits {
			for _, tunnel := range circuit.Tunnels {
				keyA, keyB, ok := parseTunnelEndpoints(tunnel.Name)
				if !ok {
					continue
				}
				sourceID, okA := ifaceIndex[keyA]
				targetID, okB := ifaceIndex[keyB]
				if !okA || !okB {
					continue
				}

				canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
					Type:      "network.vpn",
					Direction: "bidirectional",
					Source:    sourceID,
					Target:    targetID,
				})
				connID := sdk.BuildConnectionID(canonicalKey, 16)
				if seen[connID] {
					continue
				}

				conn, err := sdk.NewConnection(connID, "network.vpn", sourceID, targetID)
				if err != nil {
					continue
				}
				conn.Name = "SD-WAN Site-to-Site Tunnel"
				conn.Status = mapUpDownStatus(tunnel.State)
				if tunnel.State != "" {
					conn.State = strings.ToLower(tunnel.State)
				}
				ext := map[string]any{}
				if circuit.Color != "" {
					ext["color"] = circuit.Color
				}
				if tunnel.VqoeScore != 0 {
					ext["vqoe_score"] = tunnel.VqoeScore
				}
				if len(ext) > 0 {
					conn.Extensions = map[string]any{extensionKey: ext}
				}

				connections = append(connections, conn)
				seen[connID] = true
			}
		}
	}

	return connections
}

// TransformOMPLinks converts the global GET
// /dataservice/device/omp/links result into "network" connections
// between two devices' OMP control-plane sessions. systemIPToDeviceID
// is the merged system-ip -> resource ID index built by
// TransformDevices for the current site.
func TransformOMPLinks(links []OMPLink, systemIPToDeviceID map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	seen := make(map[string]bool)

	for _, link := range links {
		sourceID, okA := systemIPToDeviceID[link.ASystemIP]
		targetID, okB := systemIPToDeviceID[link.BSystemIP]
		if !okA || !okB || sourceID == targetID {
			continue
		}

		conn, connID, ok := buildOMPConnection(sourceID, targetID, link.State, seen)
		if !ok {
			continue
		}
		connections = append(connections, conn)
		seen[connID] = true
	}

	return connections
}

// TransformOMPPeers converts one device's GET
// /dataservice/device/omp/peers result into "network" connections to
// its vsmart/vbond controllers. Uses the same canonical-key shape as
// TransformOMPLinks (endpoints sorted for a bidirectional key) so that
// a pair already covered by the global omp/links call collapses onto
// the same connection ID instead of producing a duplicate callers
// should merge and re-dedupe both functions' output (see
// dedupeConnections) when both are used for the same site.
func TransformOMPPeers(deviceResourceID string, peers []OMPPeer, systemIPToDeviceID map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	seen := make(map[string]bool)

	for _, peer := range peers {
		targetID, ok := systemIPToDeviceID[peer.Peer]
		if !ok || targetID == deviceResourceID {
			continue
		}

		conn, connID, ok := buildOMPConnection(deviceResourceID, targetID, peer.State, seen)
		if !ok {
			continue
		}
		_ = conn.SetDirection("forward")
		connections = append(connections, conn)
		seen[connID] = true
	}

	return connections
}

// buildOMPConnection is the shared "network" connection builder for
// TransformOMPLinks and TransformOMPPeers both use a bidirectional
// canonical key (regardless of the emitted connection's own Direction
// field) so the same device pair always resolves to the same
// connection ID across either source.
func buildOMPConnection(sourceID, targetID, state string, seen map[string]bool) (sdk.Connection, string, bool) {
	canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
		Type:      "network",
		Direction: "bidirectional",
		Source:    sourceID,
		Target:    targetID,
	})
	connID := sdk.BuildConnectionID(canonicalKey, 16)
	if seen[connID] {
		return sdk.Connection{}, "", false
	}

	conn, err := sdk.NewConnection(connID, "network", sourceID, targetID)
	if err != nil {
		return sdk.Connection{}, "", false
	}
	conn.Name = "OMP Peering"
	conn.Status = mapUpDownStatus(state)
	if state != "" {
		conn.State = strings.ToLower(state)
	}
	return conn, connID, true
}

// dedupeConnections drops connections with a duplicate ID, keeping the
// first occurrence. Used when merging OMP peering connections gathered
// from more than one source (global omp/links plus per-device
// omp/peers can describe the same edge<->controller pair).
func dedupeConnections(conns []sdk.Connection) []sdk.Connection {
	seen := make(map[string]bool, len(conns))
	result := make([]sdk.Connection, 0, len(conns))
	for _, c := range conns {
		if seen[c.ID] {
			continue
		}
		seen[c.ID] = true
		result = append(result, c)
	}
	return result
}
