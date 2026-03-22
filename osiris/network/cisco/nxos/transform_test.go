// transform_test.go - Unit tests for NX-OS->OSIRIS transform functions.
// All test data is invented - no real device data.
//
// For an introduction to OSIRIS JSON Producer for Cisco see:
// "[OSIRIS-JSON-CISCO]."
//
// [OSIRIS-JSON-CISCO]: https://osirisjson.org/en/docs/producers/cisco

package nxos

import (
	"testing"

	"go.osirisjson.org/producers/pkg/sdk"
)

func TestTransformDevice(t *testing.T) {
	version := map[string]any{
		"chassis_id":     "Nexus9000 C9508",
		"proc_board_id":  "TST0000NX01",
		"sys_ver_str":    "10.3(4a)",
		"host_name":      "LAB-SPINE01",
		"bios_ver_str":   "08.42",
		"rr_reason":      "Reset Requested by CLI command reload",
		"kern_uptm_days": "10",
		"kern_uptm_hrs":  "5",
		"kern_uptm_mins": "30",
		"kern_uptm_secs": "15",
		"memory":         float64(65536000),
	}

	r, id := TransformDevice("LAB-SPINE01", version)
	if id == "" {
		t.Fatal("expected non-empty resource ID")
	}
	if r.Name != "LAB-SPINE01" {
		t.Errorf("name: %s", r.Name)
	}
	if r.Type != "network.switch.spine" {
		t.Errorf("type: %s", r.Type)
	}
	if r.Status != "active" {
		t.Errorf("status: %s", r.Status)
	}
	if r.Provider.Name != "cisco" {
		t.Errorf("provider: %s", r.Provider.Name)
	}
	if r.Properties["serial"] != "TST0000NX01" {
		t.Errorf("serial: %v", r.Properties["serial"])
	}
	if r.Properties["model"] != "Nexus9000 C9508" {
		t.Errorf("model: %v", r.Properties["model"])
	}

	// Check extensions.
	cisco := r.Extensions[extensionNamespace].(map[string]any)
	if cisco["bios_version"] != "08.42" {
		t.Errorf("bios_version: %v", cisco["bios_version"])
	}
	if cisco["last_reset_reason"] != "Reset Requested by CLI command reload" {
		t.Errorf("last_reset_reason: %v", cisco["last_reset_reason"])
	}
	if cisco["kernel_uptime"] != "10d 5h 30m 15s" {
		t.Errorf("kernel_uptime: %v", cisco["kernel_uptime"])
	}
}

func TestTransformDevice_Leaf(t *testing.T) {
	version := map[string]any{
		"chassis_id":    "Nexus9000 C93108TC-FX",
		"proc_board_id": "TST0000NX02",
		"sys_ver_str":   "10.3(4a)",
		"host_name":     "LAB-LEAF01",
	}

	r, _ := TransformDevice("LAB-LEAF01", version)
	if r.Type != "network.switch.leaf" {
		t.Errorf("expected leaf type, got: %s", r.Type)
	}
}

func TestTransformInterfaces(t *testing.T) {
	ifBrief := map[string]any{
		"TABLE_interface": map[string]any{
			"ROW_interface": []any{
				map[string]any{"interface": "Ethernet1/1", "state": "up", "speed": "10G", "type": "eth", "vlan": "100"},
				map[string]any{"interface": "Ethernet1/2", "state": "down", "speed": "10G", "type": "eth", "vlan": "200"},
				map[string]any{"interface": "port-channel10", "state": "up", "speed": "20G"},
				map[string]any{"interface": "loopback0", "state": "up"},
				map[string]any{"interface": "mgmt0", "state": "up"},
			},
		},
	}

	resources, nameToID := TransformInterfaces("LAB-SW01", ifBrief)

	if len(resources) != 5 {
		t.Fatalf("expected 5 resources, got %d", len(resources))
	}
	if len(nameToID) != 5 {
		t.Fatalf("expected 5 name mappings, got %d", len(nameToID))
	}

	// Check Ethernet type.
	if resources[0].Type != "network.interface" {
		t.Errorf("Ethernet should be network.interface, got %s", resources[0].Type)
	}
	if resources[0].Status != "active" {
		t.Errorf("up interface should be active, got %s", resources[0].Status)
	}
	if resources[1].Status != "inactive" {
		t.Errorf("down interface should be inactive, got %s", resources[1].Status)
	}

	// Check port-channel type.
	if resources[2].Type != "network.interface.lag" {
		t.Errorf("port-channel should be network.interface.lag, got %s", resources[2].Type)
	}
}

func TestTransformLLDPNeighbors(t *testing.T) {
	lldp := map[string]any{
		"TABLE_nbor_detail": map[string]any{
			"ROW_nbor_detail": []any{
				map[string]any{
					"l_port_id": "Ethernet1/1",
					"sys_name":  "REMOTE-SW01",
					"port_id":   "Ethernet1/49",
					"mgmt_addr": "10.99.0.10",
				},
				map[string]any{
					"l_port_id": "Ethernet1/2",
					"sys_name":  "REMOTE-SW02",
					"port_id":   "Ethernet1/50",
				},
			},
		},
	}

	ifNameToID := map[string]string{
		"Ethernet1/1": "res-network.interface-eth1-1-abc123",
		"Ethernet1/2": "res-network.interface-eth1-2-def456",
	}

	connections, stubs := TransformLLDPNeighbors("LAB-SW01", lldp, ifNameToID)

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
	if stubs[0].Properties["remote_system"] != "REMOTE-SW01" {
		t.Errorf("remote_system: %v", stubs[0].Properties["remote_system"])
	}
	if stubs[0].Properties["remote_mgmt_addr"] != "10.99.0.10" {
		t.Errorf("remote_mgmt_addr: %v", stubs[0].Properties["remote_mgmt_addr"])
	}

	// Verify connection.
	if connections[0].Type != "network.link" {
		t.Errorf("connection type: %s", connections[0].Type)
	}
	if connections[0].Status != "active" {
		t.Errorf("connection status: %s", connections[0].Status)
	}
}

func TestTransformLLDPNeighbors_MissingLocalInterface(t *testing.T) {
	lldp := map[string]any{
		"TABLE_nbor_detail": map[string]any{
			"ROW_nbor_detail": map[string]any{
				"l_port_id": "Ethernet1/99",
				"sys_name":  "REMOTE-SW01",
				"port_id":   "Ethernet1/1",
			},
		},
	}

	ifNameToID := map[string]string{} // empty - no matching local interface

	connections, stubs := TransformLLDPNeighbors("LAB-SW01", lldp, ifNameToID)
	if len(connections) != 0 {
		t.Errorf("expected 0 connections for missing local interface, got %d", len(connections))
	}
	if len(stubs) != 0 {
		t.Errorf("expected 0 stubs for missing local interface, got %d", len(stubs))
	}
}

func TestTransformVLANs(t *testing.T) {
	vlanBrief := map[string]any{
		"TABLE_vlanbriefxbrief": map[string]any{
			"ROW_vlanbriefxbrief": []any{
				map[string]any{"vlanshowbr-vlanid": "100", "vlanshowbr-vlanname": "PROD", "vlanshowbr-vlanstate": "active", "vlanshowbr-shutstate": "noshutdown"},
				map[string]any{"vlanshowbr-vlanid": "200", "vlanshowbr-vlanname": "MGMT", "vlanshowbr-vlanstate": "active", "vlanshowbr-shutstate": "noshutdown"},
			},
		},
	}

	groups, idMap := TransformVLANs("LAB-SW01", vlanBrief)

	if len(groups) != 2 {
		t.Fatalf("expected 2 VLAN groups, got %d", len(groups))
	}
	if len(idMap) != 2 {
		t.Fatalf("expected 2 VLAN ID mappings, got %d", len(idMap))
	}

	if groups[0].Type != "network.vlan" {
		t.Errorf("type: %s", groups[0].Type)
	}
	if groups[0].Name != "VLAN 100" {
		t.Errorf("name: %s", groups[0].Name)
	}
	if groups[0].Description != "PROD" {
		t.Errorf("description: %s", groups[0].Description)
	}
}

func TestTransformVRFs(t *testing.T) {
	vrfDetail := map[string]any{
		"TABLE_vrf": map[string]any{
			"ROW_vrf": []any{
				map[string]any{"vrf_name": "PROD", "vrf_id": "3", "vrf_state": "Up", "rd": "10.99.0.1:3"},
				map[string]any{"vrf_name": "MGMT", "vrf_id": "4", "vrf_state": "Up"},
			},
		},
	}

	groups, nameMap := TransformVRFs("LAB-SW01", vrfDetail)

	if len(groups) != 2 {
		t.Fatalf("expected 2 VRF groups, got %d", len(groups))
	}
	if len(nameMap) != 2 {
		t.Fatalf("expected 2 VRF name mappings, got %d", len(nameMap))
	}

	if groups[0].Type != "logical.vrf" {
		t.Errorf("type: %s", groups[0].Type)
	}
	if groups[0].Name != "PROD" {
		t.Errorf("name: %s", groups[0].Name)
	}
	if groups[0].Properties["route_distinguisher"] != "10.99.0.1:3" {
		t.Errorf("rd: %v", groups[0].Properties["route_distinguisher"])
	}
}

func TestTransformVPC(t *testing.T) {
	vpcBrief := map[string]any{
		"vpc-domain-id":             "10",
		"vpc-role":                  "primary",
		"vpc-peer-status":           "peer-ok",
		"vpc-peer-keepalive-status": "peer-alive",
	}

	g, gid := TransformVPC("LAB-SW01", vpcBrief)
	if g == nil {
		t.Fatal("expected vPC group")
	}
	if gid == "" {
		t.Fatal("expected vPC group ID")
	}
	if g.Type != "network.vpc" {
		t.Errorf("type: %s", g.Type)
	}
	if g.Properties["domain_id"] != "10" {
		t.Errorf("domain_id: %v", g.Properties["domain_id"])
	}
	if g.Properties["role"] != "primary" {
		t.Errorf("role: %v", g.Properties["role"])
	}
}

func TestTransformVPC_NotConfigured(t *testing.T) {
	g, _ := TransformVPC("LAB-SW01", map[string]any{})
	if g != nil {
		t.Error("expected nil group when vPC not configured")
	}
}

func TestTransformInventory(t *testing.T) {
	inv := map[string]any{
		"TABLE_inv": map[string]any{
			"ROW_inv": []any{
				map[string]any{"name": "Chassis", "desc": "Nexus9000 C9508 Chassis", "productid": "N9K-C9508", "vendorid": "V01", "serialnum": "TST0000NX01"},
				map[string]any{"name": "Slot 1", "desc": "Supervisor Module", "productid": "N9K-SUP-B+", "serialnum": "TST0000SUP1"},
			},
		},
	}

	items := TransformInventory(inv)
	if len(items) != 2 {
		t.Fatalf("expected 2 inventory items, got %d", len(items))
	}
	if items[0]["name"] != "Chassis" {
		t.Errorf("name: %v", items[0]["name"])
	}
	if items[0]["serial"] != "TST0000NX01" {
		t.Errorf("serial: %v", items[0]["serial"])
	}
}

func TestTransformSystemResources(t *testing.T) {
	sysRes := map[string]any{
		"cpu_state_idle":    "95.50",
		"memory_usage_used": "8000000",
		"memory_usage_free": "4000000",
		"load_avg_1min":     "0.25",
	}

	ext := TransformSystemResources(sysRes)
	if ext["cpu_idle"] != 95.50 {
		t.Errorf("cpu_idle: %v", ext["cpu_idle"])
	}
	if ext["memory_used"] != int64(8000000) {
		t.Errorf("memory_used: %v", ext["memory_used"])
	}
	if ext["memory_free"] != int64(4000000) {
		t.Errorf("memory_free: %v", ext["memory_free"])
	}
	if ext["load_avg_1min"] != 0.25 {
		t.Errorf("load_avg_1min: %v", ext["load_avg_1min"])
	}
}

func TestTransformEnvironment(t *testing.T) {
	env := map[string]any{
		"TABLE_psinfo": map[string]any{
			"ROW_psinfo": map[string]any{
				"psnum": "1", "psmodel": "NXA-PAC-1100W", "ps_status": "ok", "actual_out": "350 W",
			},
		},
		"TABLE_tempinfo": map[string]any{
			"ROW_tempinfo": map[string]any{
				"tempmod": "1", "sensor": "CPU", "curtemp": "42", "alarmstatus": "Ok",
			},
		},
	}

	ext := TransformEnvironment(env)

	psus, ok := ext["power_supplies"].([]map[string]any)
	if !ok || len(psus) != 1 {
		t.Fatalf("expected 1 PSU, got %v", ext["power_supplies"])
	}
	if psus[0]["model"] != "NXA-PAC-1100W" {
		t.Errorf("psu model: %v", psus[0]["model"])
	}

	temps, ok := ext["temperature"].([]map[string]any)
	if !ok || len(temps) != 1 {
		t.Fatalf("expected 1 temp sensor, got %v", ext["temperature"])
	}
	if temps[0]["current"] != "42" {
		t.Errorf("temp current: %v", temps[0]["current"])
	}
}

func TestWireInterfacesToVLANs(t *testing.T) {
	vlanBrief := map[string]any{
		"TABLE_vlanbriefxbrief": map[string]any{
			"ROW_vlanbriefxbrief": map[string]any{
				"vlanshowbr-vlanid":   "100",
				"vlanshowplist-ifidx": "Ethernet1/1,Ethernet1/2",
			},
		},
	}

	ifNameToID := map[string]string{
		"Ethernet1/1": "res-if-1",
		"Ethernet1/2": "res-if-2",
	}

	groups := []sdk.Group{{ID: "grp-vlan-100", Type: "network.vlan"}}
	vlanIDToGroupID := map[string]string{"100": "grp-vlan-100"}

	WireInterfacesToVLANs(vlanBrief, ifNameToID, groups, vlanIDToGroupID)

	if len(groups[0].Members) != 2 {
		t.Errorf("expected 2 VLAN members, got %d: %v", len(groups[0].Members), groups[0].Members)
	}
}

func TestWireInterfacesToVRFs(t *testing.T) {
	vrfDetail := map[string]any{
		"TABLE_vrf": map[string]any{
			"ROW_vrf": map[string]any{
				"vrf_name": "PROD",
				"TABLE_if": map[string]any{
					"ROW_if": []any{
						map[string]any{"if_name": "Ethernet1/1"},
						map[string]any{"if_name": "loopback0"},
					},
				},
			},
		},
	}

	ifNameToID := map[string]string{
		"Ethernet1/1": "res-if-1",
		"loopback0":   "res-if-lo0",
	}

	groups := []sdk.Group{{ID: "grp-vrf-prod", Type: "logical.vrf"}}
	vrfNameToGroupID := map[string]string{"PROD": "grp-vrf-prod"}

	WireInterfacesToVRFs(vrfDetail, ifNameToID, groups, vrfNameToGroupID)

	if len(groups[0].Members) != 2 {
		t.Errorf("expected 2 VRF members, got %d: %v", len(groups[0].Members), groups[0].Members)
	}
}

func TestWirePortChannelsToVPC(t *testing.T) {
	vpcBrief := map[string]any{
		"TABLE_vpc": map[string]any{
			"ROW_vpc": []any{
				map[string]any{"vpc-ifindex": "port-channel10"},
				map[string]any{"vpc-ifindex": "port-channel20"},
			},
		},
	}

	ifNameToID := map[string]string{
		"port-channel10": "res-pc-10",
		"port-channel20": "res-pc-20",
	}

	g := &sdk.Group{ID: "grp-vpc-10", Type: "network.vpc"}

	WirePortChannelsToVPC(vpcBrief, ifNameToID, g)

	if len(g.Members) != 2 {
		t.Errorf("expected 2 vPC members, got %d: %v", len(g.Members), g.Members)
	}
}

func TestParseTableRows_Single(t *testing.T) {
	body := map[string]any{
		"TABLE_test": map[string]any{
			"ROW_test": map[string]any{"name": "single"},
		},
	}

	rows := parseTableRows(body, "TABLE_test", "ROW_test")
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0]["name"] != "single" {
		t.Errorf("name: %v", rows[0]["name"])
	}
}

func TestParseTableRows_Multiple(t *testing.T) {
	body := map[string]any{
		"TABLE_test": map[string]any{
			"ROW_test": []any{
				map[string]any{"name": "first"},
				map[string]any{"name": "second"},
			},
		},
	}

	rows := parseTableRows(body, "TABLE_test", "ROW_test")
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
}

func TestParseTableRows_Empty(t *testing.T) {
	rows := parseTableRows(map[string]any{}, "TABLE_missing", "ROW_missing")
	if len(rows) != 0 {
		t.Errorf("expected 0 rows, got %d", len(rows))
	}
}

func TestResourceID_Deterministic(t *testing.T) {
	id1 := resourceID("network.switch", "LAB-SW01")
	id2 := resourceID("network.switch", "LAB-SW01")
	if id1 != id2 {
		t.Errorf("resourceID not deterministic: %s != %s", id1, id2)
	}

	id3 := resourceID("network.switch", "LAB-SW02")
	if id1 == id3 {
		t.Error("different inputs should produce different IDs")
	}
}

func TestMapInterfaceStatus(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"up", "active"},
		{"down", "inactive"},
		{"Up", "active"},
		{"Down", "inactive"},
		{"unknown", "unknown"},
		{"", "unknown"},
	}
	for _, tc := range cases {
		got := mapInterfaceStatus(tc.in)
		if got != tc.want {
			t.Errorf("mapInterfaceStatus(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeIfName(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Eth1/1", "Ethernet1/1"},
		{"Ethernet1/1", "Ethernet1/1"},
		{"Po10", "port-channel10"},
		{"port-channel10", "port-channel10"},
		{"loopback0", "loopback0"},
		{" Eth1/2 ", "Ethernet1/2"},
	}
	for _, tc := range cases {
		got := normalizeIfName(tc.in)
		if got != tc.want {
			t.Errorf("normalizeIfName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestClassifyRole(t *testing.T) {
	cases := []struct {
		hostname, model, want string
	}{
		{"LAB-SPINE01", "N9K-C9508", "spine"},
		{"LAB-LEAF01", "N9K-C93108TC-FX", "leaf"},
		{"SWITCH01", "N9K-C9508", "spine"},
		{"SWITCH02", "N9K-C93108TC-FX", "leaf"},
		{"SWITCH03", "UNKNOWN-MODEL", ""},
	}
	for _, tc := range cases {
		got := classifyRole(tc.hostname, tc.model)
		if got != tc.want {
			t.Errorf("classifyRole(%q, %q) = %q, want %q", tc.hostname, tc.model, got, tc.want)
		}
	}
}

func TestEnrichInterfaceDetails(t *testing.T) {
	ifNameToID := map[string]string{
		"Ethernet1/1": "res-if-1",
	}

	resources := []sdk.Resource{
		{ID: "res-if-1", Type: "network.interface", Properties: map[string]any{"speed": "10G"}},
	}

	ifDetail := map[string]any{
		"TABLE_interface": map[string]any{
			"ROW_interface": map[string]any{
				"interface":    "Ethernet1/1",
				"eth_mtu":      float64(9216),
				"eth_bw":       float64(10000000),
				"eth_duplex":   "full",
				"eth_hw_addr":  "aabb.ccdd.eeff",
				"desc":         "Uplink to spine",
				"eth_outbytes": float64(1000000),
				"eth_inbytes":  float64(2000000),
			},
		},
	}

	EnrichInterfaceDetails("LAB-SW01", ifDetail, resources, ifNameToID)

	props := resources[0].Properties
	if props["mtu"] != int64(9216) {
		t.Errorf("mtu: %v", props["mtu"])
	}
	if props["bandwidth"] != int64(10000000) {
		t.Errorf("bandwidth: %v", props["bandwidth"])
	}
	if props["duplex"] != "full" {
		t.Errorf("duplex: %v", props["duplex"])
	}
	if props["description"] != "Uplink to spine" {
		t.Errorf("description: %v", props["description"])
	}
	if props["tx_bytes"] != int64(1000000) {
		t.Errorf("tx_bytes: %v", props["tx_bytes"])
	}
	if props["rx_bytes"] != int64(2000000) {
		t.Errorf("rx_bytes: %v", props["rx_bytes"])
	}
}
