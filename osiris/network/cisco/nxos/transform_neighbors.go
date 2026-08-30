// transform_neighbors.go - Link-layer neighbor discovery:
// "show lldp neighbors detail" and "show cdp neighbors detail" merged
// into one set of physical.ethernet connections plus unresolved
// remote-endpoint stub resources.
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

// neighborObservation is one deduplicated local-port -> remote-system/
// port pairing, possibly seen via more than one discovery protocol.
type neighborObservation struct {
	localPort      string
	remoteSystem   string
	remotePort     string
	remoteMgmtAddr string
	sources        []string
}

// TransformNeighbors merges "show lldp neighbors detail" and
// "show cdp neighbors detail" into a single set of network.link
// connections and stub network.interface resources for remote endpoints
// one producer-neutral discovery pass instead of two, per
// OSIRIS-JSON-v1.0's own device-agnostic modeling intent.
//
// Deduplication is keyed by (local port, remote system, remote port):
// a neighbor reported identically by both protocols on the same local
// port yields exactly one connection, with properties.discovered_via
// listing every protocol that saw it (["lldp"], ["cdp"], or
// ["lldp","cdp"]) satisfying "duplicate LLDP/CDP reports do not
// create duplicate links" without discarding which protocol(s)
// actually observed the neighbor. LLDP rows are processed first, so an
// LLDP observation's remote_mgmt_addr wins over CDP's when both report
// one; a local port with only a CDP observation (e.g. mgmt0, which
// often speaks CDP but not LLDP) still surfaces on its own
// "CDP-only" and "LLDP-only" both fall out of the same merge, not
// separate code paths.
//
// Every stub resource uses unknownProviderName as its provider.name,
// never providerName LLDP is vendor-neutral and CDP does not
// establish the remote is Cisco hardware either (a third-party device
// can speak CDP), so asserting "cisco.nxos" here would be an
// unsupported claim about a device this producer never actually
// queried. Its id, however, is anchored under deviceID (see
// unresolvedStubID) rather than a separate "unknown::"-namespaced
// composite this producer is the one minting the id, not the remote
// device, so the id should not pretend otherwise.
// transceivers maps a local, normalized interface name to that port's
// own transceiver info (see TransformTransceivers) when present, it
// is attached as the connection's source_transceiver
// (OSIRIS-JSON-v1.0 5.4.2). target_transceiver is never populated:
// this producer only queries the local device's own NX-API, never the
// remote neighbor identified by LLDP/CDP, so the far side's transceiver
// is genuinely not discoverable here omitted rather than guessed.
func TransformNeighbors(deviceID, hostname string, lldp lldpNeighborsResponse, cdp cdpNeighborsResponse, ifNameToID map[string]string, transceivers map[string]map[string]any) ([]sdk.Connection, []sdk.Resource) {
	seen := make(map[string]*neighborObservation)
	var ordered []*neighborObservation

	add := func(localPort, remoteSystem, remotePort, remoteMgmtAddr, source string) {
		localPort = normalizeIfName(localPort)
		if localPort == "" || remoteSystem == "" || remotePort == "" {
			return
		}
		key := localPort + "|" + remoteSystem + "|" + remotePort
		if obs, ok := seen[key]; ok {
			obs.sources = append(obs.sources, source)
			if obs.remoteMgmtAddr == "" {
				obs.remoteMgmtAddr = remoteMgmtAddr
			}
			return
		}
		obs := &neighborObservation{
			localPort:      localPort,
			remoteSystem:   remoteSystem,
			remotePort:     remotePort,
			remoteMgmtAddr: remoteMgmtAddr,
			sources:        []string{source},
		}
		seen[key] = obs
		ordered = append(ordered, obs)
	}

	for _, row := range lldp.TableNborDetail.RowNborDetail {
		add(string(row.LocalPortID), string(row.SysName), string(row.PortID), string(row.MgmtAddr), "lldp")
	}
	for _, row := range cdp.TableCDP.RowCDP {
		mgmtAddr := string(row.V4MgmtAddr)
		if mgmtAddr == "" {
			mgmtAddr = string(row.V4Addr)
		}
		add(string(row.IntfID), string(row.SysName), string(row.PortID), mgmtAddr, "cdp")
	}

	var connections []sdk.Connection
	var stubs []sdk.Resource

	for _, obs := range ordered {
		localID, ok := ifNameToID[obs.localPort]
		if !ok {
			continue
		}

		remoteCanonical := fmt.Sprintf("%s|%s", obs.remoteSystem, obs.remotePort)
		remoteID := unresolvedStubID(deviceID, "neighbor", obs.remoteSystem, obs.remotePort)

		remoteProv := sdk.Provider{
			Name:     unknownProviderName,
			NativeID: remoteCanonical,
			Source:   strings.Join(obs.sources, "+"),
		}
		stub, err := sdk.NewResource(remoteID, "network.interface", remoteProv)
		if err != nil {
			continue
		}
		stub.Name = fmt.Sprintf("%s:%s", obs.remoteSystem, obs.remotePort)
		stub.Status = "unknown"

		props := map[string]any{
			"remote_system":  obs.remoteSystem,
			"remote_port":    obs.remotePort,
			"discovered_via": obs.sources,
		}
		if obs.remoteMgmtAddr != "" {
			props["remote_mgmt_addr"] = obs.remoteMgmtAddr
		}
		stub.Properties = props
		stubs = append(stubs, stub)

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
		conn.Name = fmt.Sprintf("%s:%s <-> %s:%s", hostname, obs.localPort, obs.remoteSystem, obs.remotePort)
		conn.Status = "active"
		connProps := map[string]any{"discovered_via": obs.sources}
		if xcvr, ok := transceivers[obs.localPort]; ok {
			connProps["source_transceiver"] = xcvr
		}
		conn.Properties = connProps

		connections = append(connections, conn)
	}

	return connections, stubs
}
