// transform_neighbors_test.go - Unit tests for the LLDP/CDP neighbor
// merge of the Cisco IOS-XE OSIRIS JSON Producer.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package iosxe

import (
	"fmt"
	"testing"

	"go.osirisjson.org/producers/pkg/sdk"
)

// neighborsCases are the synthetic device replies the neighbors unit
// tests decode: one minimal NETCONF <data> payload per case, keyed by
// the behavior it exercises.
var neighborsCases = map[string]string{
	"cdp-abbreviated":       `<cdp-neighbor-details xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-cdp-oper"><cdp-neighbor-detail><device-name>LAB-SW03</device-name><local-intf-name>Te0/1/0</local-intf-name><port-id>Te1/1/1</port-id><platform-name>cisco C9300-24T</platform-name></cdp-neighbor-detail></cdp-neighbor-details>`,
	"cdp-lab-sw01-fqdn":     `<cdp-neighbor-details xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-cdp-oper"><cdp-neighbor-detail><device-name>LAB-SW01.example.com</device-name><local-intf-name>GigabitEthernet1</local-intf-name><port-id>GigabitEthernet1/0/1</port-id><platform-name>cisco C9300-24T</platform-name></cdp-neighbor-detail></cdp-neighbor-details>`,
	"cdp-local-not-emitted": `<cdp-neighbor-details xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-cdp-oper"><cdp-neighbor-detail><device-name>LAB-SW02</device-name><local-intf-name>GigabitEthernet9</local-intf-name><port-id>GigabitEthernet1/0/2</port-id><platform-name>cisco C9300-24T</platform-name></cdp-neighbor-detail></cdp-neighbor-details>`,
	"cdp-missing-port":      `<cdp-neighbor-details xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-cdp-oper"><cdp-neighbor-detail><device-name>LAB-SW02</device-name><local-intf-name>GigabitEthernet2</local-intf-name><port-id></port-id><platform-name>cisco C9300-24T</platform-name></cdp-neighbor-detail></cdp-neighbor-details>`,
	"cdp-namesake":          `<cdp-neighbor-details xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-cdp-oper"><cdp-neighbor-detail><device-name>LAB-SW01</device-name><local-intf-name>GigabitEthernet1</local-intf-name><port-id>GigabitEthernet2</port-id><mgmt-address>203.0.113.99</mgmt-address></cdp-neighbor-detail></cdp-neighbor-details>`,
	"cdp-self-loop":         `<cdp-neighbor-details xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-cdp-oper"><cdp-neighbor-detail><device-name>LAB-SW01.example.com</device-name><local-intf-name>GigabitEthernet1</local-intf-name><port-id>GigabitEthernet2</port-id><mgmt-address>198.51.100.20</mgmt-address></cdp-neighbor-detail></cdp-neighbor-details>`,
	"lldp-lab-sw01-mgmt":    `<lldp-entries xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-lldp-oper"><lldp-intf-details><if-name>GigabitEthernet1</if-name><lldp-neighbor-details><identifier>1</identifier><chassis-id>00:00:5e:00:53:a1</chassis-id><port-id>Gi1/0/1</port-id><system-name>LAB-SW01</system-name><mgmt-addrs><mgmt-addr>198.51.100.2</mgmt-addr></mgmt-addrs></lldp-neighbor-details></lldp-intf-details></lldp-entries>`,
	"lldp-lab-sw01":         `<lldp-entries xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-lldp-oper"><lldp-intf-details><if-name>GigabitEthernet1</if-name><lldp-neighbor-details><identifier>1</identifier><chassis-id>00:00:5e:00:53:a1</chassis-id><port-id>Gi1/0/1</port-id><system-name>LAB-SW01</system-name></lldp-neighbor-details></lldp-intf-details></lldp-entries>`,
	"lldp-missing-peer":     `<lldp-entries xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-lldp-oper"><lldp-intf-details><if-name>GigabitEthernet1</if-name><lldp-neighbor-details><identifier>1</identifier><chassis-id>00:00:5e:00:53:a1</chassis-id><port-id></port-id><system-name></system-name></lldp-neighbor-details></lldp-intf-details></lldp-entries>`,
	"lldp-self-loop":        `<lldp-entries xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-lldp-oper"><lldp-intf-details><if-name>GigabitEthernet2</if-name><lldp-neighbor-details><identifier>1</identifier><chassis-id>00:00:5e:00:53:a1</chassis-id><port-id>Gi1</port-id><system-name>LAB-SW01</system-name></lldp-neighbor-details></lldp-intf-details></lldp-entries>`,
	"lldp-summary-only":     `<lldp-entries xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-lldp-oper"><lldp-entry><device-id>LAB-SW01</device-id><local-interface>GigabitEthernet1</local-interface><connecting-interface>Gi1/0/1</connecting-interface></lldp-entry></lldp-entries>`,
	"lldp-two-neighbors":    `<lldp-entries xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-lldp-oper"><lldp-intf-details><if-name>GigabitEthernet1</if-name><lldp-neighbor-details><identifier>1</identifier><chassis-id>00:00:5e:00:53:a1</chassis-id><port-id>Gi1/0/1</port-id><system-name>LAB-SW01</system-name></lldp-neighbor-details></lldp-intf-details><lldp-intf-details><if-name>GigabitEthernet2</if-name><lldp-neighbor-details><identifier>1</identifier><chassis-id>00:00:5e:00:53:a1</chassis-id><port-id>Gi1/0/1</port-id><system-name>LAB-SW02</system-name></lldp-neighbor-details></lldp-intf-details></lldp-entries>`,
}

const testDeviceID = "cisco.iosxe::TST0000XE01"

var testIfNameToID = map[string]string{
	"GigabitEthernet1":        testDeviceID + "/GigabitEthernet1",
	"GigabitEthernet2":        testDeviceID + "/GigabitEthernet2",
	"TenGigabitEthernet0/1/0": testDeviceID + "/TenGigabitEthernet0/1/0",
}

// neighborFixture decodes the neighbors/<name> unit fixture, or returns
// the zero value when name is empty (the protocol reported nothing).
func neighborFixture[T model](t *testing.T, name string) T {
	t.Helper()
	var zero T
	if name == "" {
		return zero
	}
	return mustDecode[T](t, unitXML(t, neighborsCases, name))
}

func TestTransformNeighbors(t *testing.T) {
	tests := []struct {
		name       string
		lldp, cdp  string // keys of neighborsCases; "" = none
		wantConns  int
		wantVia    []string // discovered_via of the first connection
		wantStubID string   // ID of the first stub
	}{
		{
			name:       "LLDP only",
			lldp:       "lldp-lab-sw01-mgmt",
			wantConns:  1,
			wantVia:    []string{"lldp"},
			wantStubID: testDeviceID + "/neighbor/LAB-SW01/GigabitEthernet1/0/1",
		},
		{
			name:       "CDP only",
			cdp:        "cdp-lab-sw01-fqdn",
			wantConns:  1,
			wantVia:    []string{"cdp"},
			wantStubID: testDeviceID + "/neighbor/LAB-SW01/GigabitEthernet1/0/1",
		},
		{
			name:       "duplicate LLDP and CDP report of one link",
			lldp:       "lldp-lab-sw01",
			cdp:        "cdp-lab-sw01-fqdn",
			wantConns:  1,
			wantVia:    []string{"lldp", "cdp"},
			wantStubID: testDeviceID + "/neighbor/LAB-SW01/GigabitEthernet1/0/1",
		},
		{
			name:      "missing peer system or port",
			lldp:      "lldp-missing-peer",
			cdp:       "cdp-missing-port",
			wantConns: 0,
		},
		{
			name:      "one-sided: local interface not emitted",
			cdp:       "cdp-local-not-emitted",
			wantConns: 0,
		},
		{
			name:       "abbreviated local interface resolves",
			cdp:        "cdp-abbreviated",
			wantConns:  1,
			wantVia:    []string{"cdp"},
			wantStubID: testDeviceID + "/neighbor/LAB-SW03/TenGigabitEthernet1/1/1",
		},
		{
			name:      "two different neighbors on two ports",
			lldp:      "lldp-two-neighbors",
			wantConns: 2,
			wantVia:   []string{"lldp"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conns, stubs := TransformNeighbors(testDeviceID, "LAB-RTR01", selfIdentity{},
				neighborFixture[lldpEntries](t, tt.lldp), neighborFixture[cdpNeighborDetails](t, tt.cdp), testIfNameToID)
			if len(conns) != tt.wantConns || len(stubs) != tt.wantConns {
				t.Fatalf("got %d connections, %d stubs, want %d each", len(conns), len(stubs), tt.wantConns)
			}
			if tt.wantConns == 0 {
				return
			}
			c := conns[0]
			if c.Type != "physical.ethernet" || c.Status != "active" || c.Target != stubs[0].ID {
				t.Errorf("connection = %+v", c)
			}
			if fmt.Sprint(c.Properties["discovered_via"]) != fmt.Sprint(tt.wantVia) {
				t.Errorf("discovered_via = %v, want %v", c.Properties["discovered_via"], tt.wantVia)
			}
			if tt.wantStubID != "" && stubs[0].ID != tt.wantStubID {
				t.Errorf("stub id = %q, want %q", stubs[0].ID, tt.wantStubID)
			}
			if stubs[0].Provider.Name != unknownProviderName || stubs[0].Status != "unknown" {
				t.Errorf("stub provider/status = %q/%q", stubs[0].Provider.Name, stubs[0].Status)
			}
		})
	}
}

func TestTransformNeighbors_MergedProperties(t *testing.T) {
	conns, stubs := TransformNeighbors(testDeviceID, "LAB-RTR01", selfIdentity{},
		neighborFixture[lldpEntries](t, "lldp-lab-sw01-mgmt"),
		neighborFixture[cdpNeighborDetails](t, "cdp-lab-sw01-fqdn"),
		testIfNameToID)
	if len(conns) != 1 || len(stubs) != 1 {
		t.Fatalf("got %d/%d", len(conns), len(stubs))
	}
	p := stubs[0].Properties
	if p["remote_system"] != "LAB-SW01.example.com" || p["remote_mgmt_addr"] != "198.51.100.2" || p["remote_platform"] != "cisco C9300-24T" {
		t.Errorf("stub properties = %v", p)
	}
	if stubs[0].Name != "LAB-SW01:GigabitEthernet1/0/1" || conns[0].Name != "LAB-RTR01:GigabitEthernet1 <-> LAB-SW01:GigabitEthernet1/0/1" {
		t.Errorf("names = %q / %q", stubs[0].Name, conns[0].Name)
	}
}

func TestTransformNeighbors_SummaryListFallback(t *testing.T) {
	conns, _ := TransformNeighbors(testDeviceID, "LAB-RTR01", selfIdentity{}, neighborFixture[lldpEntries](t, "lldp-summary-only"), cdpNeighborDetails{}, testIfNameToID)
	if len(conns) != 1 {
		t.Fatalf("the lldp-entry summary list must be used when no detail list is present, got %d", len(conns))
	}
}

func TestExpandIfName(t *testing.T) {
	for in, want := range map[string]string{
		"Gi1/0/1":                 "GigabitEthernet1/0/1",
		"Gig 1/0/1":               "GigabitEthernet1/0/1",
		"GigabitEthernet1/0/1":    "GigabitEthernet1/0/1",
		"Te1/1/1":                 "TenGigabitEthernet1/1/1",
		"Twe1/0/1":                "TwentyFiveGigE1/0/1",
		"Tw1/0/1":                 "TwoGigabitEthernet1/0/1",
		"Fo1/0/1":                 "FortyGigabitEthernet1/0/1",
		"Hu1/0/1":                 "HundredGigE1/0/1",
		"Po10":                    "Port-channel10",
		"Vl10":                    "Vlan10",
		"Lo0":                     "Loopback0",
		"Ethernet1/1":             "Ethernet1/1",
		"ge-0/0/1":                "ge-0/0/1",
		"mgmt0":                   "mgmt0",
		"00:00:5e:00:53:c1":       "00:00:5e:00:53:c1",
		"GigabitEthernet0/0/0.10": "GigabitEthernet0/0/0.10",
	} {
		if got := expandIfName(in); got != want {
			t.Errorf("expandIfName(%q) = %q, want %q", in, got, want)
		}
	}
}

// mustDecodeOrZero decodes wrap(body) into T, or returns the zero value
// when body is empty.
func TestTransformNeighbors_SelfLoop(t *testing.T) {
	self := selfIdentity{hostname: "LAB-SW01", addrs: map[string]bool{"198.51.100.20": true}}
	// GigabitEthernet1 is cabled to the device's own GigabitEthernet2;
	// both ends report it, once via CDP (FQDN) and once via LLDP.
	conns, stubs := TransformNeighbors(testDeviceID, "LAB-SW01", self, neighborFixture[lldpEntries](t, "lldp-self-loop"), neighborFixture[cdpNeighborDetails](t, "cdp-self-loop"), testIfNameToID)
	if len(stubs) != 0 || len(conns) != 1 {
		t.Fatalf("got %d connections, %d stubs, want 1 port-to-port link and no stub", len(conns), len(stubs))
	}
	c := conns[0]
	if c.Source != testIfNameToID["GigabitEthernet1"] || c.Target != testIfNameToID["GigabitEthernet2"] {
		t.Errorf("link = %s -> %s", c.Source, c.Target)
	}

	// Same host name but a management address the device does not own:
	// another device that shares the name, so a stub.
	conns, stubs = TransformNeighbors(testDeviceID, "LAB-SW01", self, lldpEntries{}, neighborFixture[cdpNeighborDetails](t, "cdp-namesake"), testIfNameToID)
	if len(stubs) != 1 || len(conns) != 1 {
		t.Errorf("a namesake with a foreign address must stay a stub, got %d/%d", len(conns), len(stubs))
	}
}

func TestOpticalPMD(t *testing.T) {
	for pmd, want := range map[string]bool{
		"10GBASE-SR": true, "10GBASE-LR": true, "10GBASE-LRM": true, "1000BASE-SX": true,
		"1000BASE-LX": true, "100GBASE-ER4": true, "10GBASE-ZR": true, "1000BASE-BX10-D": true,
		"ETH_10GBASE_LR": true, "100GBASE-DR": true, "100BASE-FX": true,
		"1000BASE-T": false, "10GBASE-CR": false, "40GBASE-CR4": false, "10GBASE-KR": false,
		"": false, "UNKNOWN": false, "10GBASE": false,
	} {
		if got := opticalPMD(pmd); got != want {
			t.Errorf("opticalPMD(%q) = %v, want %v", pmd, got, want)
		}
	}
}

func TestApplyLinkMedium(t *testing.T) {
	ports := []sdk.Resource{
		{ID: "p-sr", Properties: map[string]any{"transceiver": map[string]any{"ethernet_pmd": "10GBASE-SR"}}},
		{ID: "p-t", Properties: map[string]any{"transceiver": map[string]any{"ethernet_pmd": "1000BASE-T"}}},
		{ID: "p-none"},
	}
	mk := func(src, dst string) sdk.Connection {
		c, err := sdk.NewConnection("c-"+src, "physical.ethernet", src, dst)
		if err != nil {
			t.Fatal(err)
		}
		c.Status = "active"
		c.Properties = map[string]any{"discovered_via": []string{"lldp"}}
		return c
	}
	conns := []sdk.Connection{mk("p-sr", "stub-1"), mk("p-t", "stub-2"), mk("p-none", "stub-3"), mk("stub-4", "p-sr")}
	ApplyLinkMedium(conns, ports)
	for i, want := range []string{"physical.fiber", "physical.ethernet", "physical.ethernet", "physical.fiber"} {
		if conns[i].Type != want {
			t.Errorf("conns[%d].Type = %s, want %s", i, conns[i].Type, want)
		}
	}
	if conns[0].Properties["ethernet_pmd"] != "10GBASE-SR" || conns[0].Status != "active" || conns[0].Properties["discovered_via"] == nil {
		t.Errorf("fiber link must keep its fields and record the PMD: %+v", conns[0])
	}
	if conns[0].ID == "c-p-sr" {
		t.Error("the connection ID must be derived from the new type")
	}
	if _, ok := conns[1].Properties["ethernet_pmd"]; ok {
		t.Error("a copper link must not be annotated")
	}
}
