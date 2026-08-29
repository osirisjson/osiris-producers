// transform_neighbors_test.go - Unit tests for transform_neighbors.go.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package nxos

import (
	"testing"

	"go.osirisjson.org/producers/pkg/sdk"
)

func TestTransformNeighbors_LLDPOnly(t *testing.T) {
	lldp := lldpNeighborsResponse{
		TableNborDetail: lldpTable{
			RowNborDetail: rowList[lldpNeighborRow]{
				{LocalPortID: "Ethernet1/1", SysName: "REMOTE-SW01", PortID: "Ethernet1/49", MgmtAddr: "192.0.2.10"},
				{LocalPortID: "Ethernet1/2", SysName: "REMOTE-SW02", PortID: "Ethernet1/50"},
			},
		},
	}

	ifNameToID := map[string]string{
		"Ethernet1/1": "res-network.interface-eth1-1-abc123",
		"Ethernet1/2": "res-network.interface-eth1-2-def456",
	}

	connections, stubs := TransformNeighbors("cisco.nxos::TST0000NX01", "LAB-SW01", lldp, cdpNeighborsResponse{}, ifNameToID, nil)

	if len(connections) != 2 {
		t.Fatalf("expected 2 connections, got %d", len(connections))
	}
	if len(stubs) != 2 {
		t.Fatalf("expected 2 stubs, got %d", len(stubs))
	}

	// Verify stub properties.
	if stubs[0].Status != "unknown" {
		t.Errorf("stub status should be unknown, got %s", stubs[0].Status)
	}
	if stubs[0].Provider.Name != unknownProviderName {
		t.Errorf("stub provider should be %q (LLDP does not establish remote vendor), got %q", unknownProviderName, stubs[0].Provider.Name)
	}
	if stubs[0].ID != "cisco.nxos::TST0000NX01/neighbor/REMOTE-SW01/Ethernet1/49" {
		t.Errorf("stub id should be anchored under the local device's own id, not a separate unknown:: identity, got %q", stubs[0].ID)
	}
	if stubs[0].Properties["remote_system"] != "REMOTE-SW01" {
		t.Errorf("remote_system: %v", stubs[0].Properties["remote_system"])
	}
	if stubs[0].Properties["remote_mgmt_addr"] != "192.0.2.10" {
		t.Errorf("remote_mgmt_addr: %v", stubs[0].Properties["remote_mgmt_addr"])
	}
	if got, ok := stubs[0].Properties["discovered_via"].([]string); !ok || len(got) != 1 || got[0] != "lldp" {
		t.Errorf("discovered_via: %v", stubs[0].Properties["discovered_via"])
	}

	// Verify connection.
	if connections[0].Type != "physical.ethernet" {
		t.Errorf("connection type: %s", connections[0].Type)
	}
	if connections[0].Status != "active" {
		t.Errorf("connection status: %s", connections[0].Status)
	}
}

func TestTransformNeighbors_SourceTransceiverAttachedWhenPresent(t *testing.T) {
	lldp := lldpNeighborsResponse{
		TableNborDetail: lldpTable{
			RowNborDetail: rowList[lldpNeighborRow]{
				{LocalPortID: "Ethernet1/1", SysName: "REMOTE-SW01", PortID: "Ethernet1/49"},
				{LocalPortID: "Ethernet1/2", SysName: "REMOTE-SW02", PortID: "Ethernet1/50"},
			},
		},
	}
	ifNameToID := map[string]string{
		"Ethernet1/1": "res-network.interface-eth1-1-abc123",
		"Ethernet1/2": "res-network.interface-eth1-2-def456",
	}
	transceivers := map[string]map[string]any{
		"Ethernet1/1": {"vendor": "CISCO-ACCELINK", "model": "SFP-10G-SR"},
	}

	connections, _ := TransformNeighbors("cisco.nxos::TST0000NX01", "LAB-SW01", lldp, cdpNeighborsResponse{}, ifNameToID, transceivers)
	if len(connections) != 2 {
		t.Fatalf("expected 2 connections, got %d", len(connections))
	}

	var withXcvr, withoutXcvr *sdk.Connection
	for i := range connections {
		if connections[i].Source == "res-network.interface-eth1-1-abc123" {
			withXcvr = &connections[i]
		} else {
			withoutXcvr = &connections[i]
		}
	}
	if withXcvr == nil || withoutXcvr == nil {
		t.Fatalf("expected both connections to be found: %v", connections)
	}
	xcvr, ok := withXcvr.Properties["source_transceiver"].(map[string]any)
	if !ok || xcvr["model"] != "SFP-10G-SR" {
		t.Errorf("source_transceiver on Ethernet1/1's connection: %v", withXcvr.Properties["source_transceiver"])
	}
	if _, ok := withXcvr.Properties["target_transceiver"]; ok {
		t.Errorf("target_transceiver must never be populated (remote device not queried): %v", withXcvr.Properties["target_transceiver"])
	}
	if _, ok := withoutXcvr.Properties["source_transceiver"]; ok {
		t.Errorf("Ethernet1/2 has no transceiver data, source_transceiver should be absent: %v", withoutXcvr.Properties["source_transceiver"])
	}
}

func TestTransformNeighbors_MissingLocalInterface(t *testing.T) {
	lldp := lldpNeighborsResponse{
		TableNborDetail: lldpTable{
			RowNborDetail: rowList[lldpNeighborRow]{
				{LocalPortID: "Ethernet1/99", SysName: "REMOTE-SW01", PortID: "Ethernet1/1"},
			},
		},
	}

	ifNameToID := map[string]string{} // empty - no matching local interface

	connections, stubs := TransformNeighbors("cisco.nxos::TST0000NX01", "LAB-SW01", lldp, cdpNeighborsResponse{}, ifNameToID, nil)
	if len(connections) != 0 {
		t.Errorf("expected 0 connections for missing local interface, got %d", len(connections))
	}
	if len(stubs) != 0 {
		t.Errorf("expected 0 stubs for missing local interface, got %d", len(stubs))
	}
}

func TestTransformNeighbors_CDPOnly(t *testing.T) {
	// mgmt0 often speaks CDP but not LLDP a local port with only a
	// CDP observation must still surface on its own.
	cdp := cdpNeighborsResponse{
		TableCDP: cdpTable{
			RowCDP: rowList[cdpNeighborRow]{
				{IntfID: "mgmt0", SysName: "REMOTE-MGMT01", PortID: "Ethernet1/18", V4MgmtAddr: "192.0.2.20"},
			},
		},
	}
	ifNameToID := map[string]string{"mgmt0": "res-mgmt0"}

	connections, stubs := TransformNeighbors("cisco.nxos::TST0000NX01", "LAB-SW01", lldpNeighborsResponse{}, cdp, ifNameToID, nil)
	if len(connections) != 1 || len(stubs) != 1 {
		t.Fatalf("expected 1 connection and 1 stub, got %d/%d", len(connections), len(stubs))
	}
	if stubs[0].Properties["remote_mgmt_addr"] != "192.0.2.20" {
		t.Errorf("remote_mgmt_addr: %v", stubs[0].Properties["remote_mgmt_addr"])
	}
	if got, ok := stubs[0].Properties["discovered_via"].([]string); !ok || len(got) != 1 || got[0] != "cdp" {
		t.Errorf("discovered_via: %v", stubs[0].Properties["discovered_via"])
	}
}

func TestTransformNeighbors_CDPFallsBackToV4AddrWhenNoMgmtAddr(t *testing.T) {
	cdp := cdpNeighborsResponse{
		TableCDP: cdpTable{
			RowCDP: rowList[cdpNeighborRow]{
				{IntfID: "Ethernet1/1", SysName: "REMOTE-SW01", PortID: "Ethernet1/5", V4Addr: "192.0.2.30"},
			},
		},
	}
	ifNameToID := map[string]string{"Ethernet1/1": "res-eth1-1"}

	_, stubs := TransformNeighbors("cisco.nxos::TST0000NX01", "LAB-SW01", lldpNeighborsResponse{}, cdp, ifNameToID, nil)
	if len(stubs) != 1 {
		t.Fatalf("expected 1 stub, got %d", len(stubs))
	}
	if stubs[0].Properties["remote_mgmt_addr"] != "192.0.2.30" {
		t.Errorf("remote_mgmt_addr should fall back to v4addr, got %v", stubs[0].Properties["remote_mgmt_addr"])
	}
}

func TestTransformNeighbors_DuplicateAcrossLLDPAndCDPMergesToOneConnection(t *testing.T) {
	// The same neighbor reported by both protocols on the same local
	// port must yield exactly one connection, with both protocols
	// recorded in discovered_via not two separate links.
	lldp := lldpNeighborsResponse{
		TableNborDetail: lldpTable{
			RowNborDetail: rowList[lldpNeighborRow]{
				{LocalPortID: "Ethernet1/1", SysName: "REMOTE-SW01", PortID: "Ethernet1/5", MgmtAddr: "192.0.2.10"},
			},
		},
	}
	cdp := cdpNeighborsResponse{
		TableCDP: cdpTable{
			RowCDP: rowList[cdpNeighborRow]{
				{IntfID: "Ethernet1/1", SysName: "REMOTE-SW01", PortID: "Ethernet1/5", V4MgmtAddr: "192.0.2.10"},
			},
		},
	}
	ifNameToID := map[string]string{"Ethernet1/1": "res-eth1-1"}

	connections, stubs := TransformNeighbors("cisco.nxos::TST0000NX01", "LAB-SW01", lldp, cdp, ifNameToID, nil)
	if len(connections) != 1 {
		t.Fatalf("expected 1 deduplicated connection, got %d", len(connections))
	}
	if len(stubs) != 1 {
		t.Fatalf("expected 1 deduplicated stub, got %d", len(stubs))
	}
	via, ok := stubs[0].Properties["discovered_via"].([]string)
	if !ok || len(via) != 2 || via[0] != "lldp" || via[1] != "cdp" {
		t.Errorf("discovered_via should list both protocols in lldp-then-cdp order, got %v", stubs[0].Properties["discovered_via"])
	}
	connVia, ok := connections[0].Properties["discovered_via"].([]string)
	if !ok || len(connVia) != 2 {
		t.Errorf("connection discovered_via: %v", connections[0].Properties["discovered_via"])
	}
}
