// transform_endpoints_test.go - Tests for the APIC endpoint transform
// and EPG wiring.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package apic

import (
	"testing"

	"go.osirisjson.org/producers/pkg/sdk"
)

func TestTransformEndpoints(t *testing.T) {
	endpoints := []map[string]any{
		{"dn": "uni/tn-tn_Lab/ap-appl/epg-epg1/cep-00:00:5E:00:53:CC", "mac": "00:00:5E:00:53:CC", "encap": "vlan-914", "fabricPathDn": "topology/pod-2/paths-219/pathep-[eth1/46]"},
	}
	ips := []map[string]any{
		{"dn": "uni/tn-tn_Lab/ap-appl/epg-epg1/cep-00:00:5E:00:53:CC/ip-[203.0.113.20]", "addr": "203.0.113.20"},
		{"dn": "uni/tn-tn_Lab/ap-appl/epg-epg1/cep-00:00:5E:00:53:CC/ip-[203.0.113.10]", "addr": "203.0.113.10"},
		{"dn": "uni/tn-tn_Lab/ap-appl/epg-epg1/cep-00:00:5E:00:53:CC/ip-[203.0.113.10]", "addr": "203.0.113.10"}, // dup
	}

	resources := TransformEndpoints(endpoints, ips)

	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	r := resources[0]
	if r.Type != "network.interface" {
		t.Errorf("type: expected network.interface, got %s", r.Type)
	}
	if r.Properties["mac_address"] != "00:00:5e:00:53:cc" {
		t.Errorf("normalized mac_address: %v", r.Properties["mac_address"])
	}
	addrs, ok := r.Properties["ip_addresses"].([]string)
	if !ok {
		t.Fatalf("ip_addresses should be a []string, got %T", r.Properties["ip_addresses"])
	}
	if len(addrs) != 2 || addrs[0] != "203.0.113.10" || addrs[1] != "203.0.113.20" {
		t.Errorf("ip_addresses should be deduplicated and sorted: %v", addrs)
	}
	cisco, ok := r.Extensions[extensionNamespace].(map[string]any)
	if !ok {
		t.Fatal("endpoint should carry an osiris.cisco extension for encap/fabric_path")
	}
	if cisco["encap"] != "vlan-914" {
		t.Errorf("encap: %v", cisco["encap"])
	}
}

func TestWireEndpointsToEPGs(t *testing.T) {
	epAttrs := []map[string]any{
		{"dn": "uni/tn-tn_Example/ap-app1/epg-epg_WEB/cep-00:00:5E:00:53:AA"},
		{"dn": "uni/tn-tn_Example/ap-app1/epg-epg_WEB/cep-00:00:5E:00:53:BB"},
	}
	epgDNToID := map[string]string{
		"uni/tn-tn_Example/ap-app1/epg-epg_WEB": "group-epg-web",
	}
	epgGroups := []sdk.Group{
		{ID: "group-epg-web", Type: "osiris.cisco.epg", Name: "epg_WEB"},
	}

	WireEndpointsToEPGs(epAttrs, epgDNToID, epgGroups)

	if len(epgGroups[0].Members) != 2 {
		t.Fatalf("expected 2 endpoint members, got %d", len(epgGroups[0].Members))
	}
}
