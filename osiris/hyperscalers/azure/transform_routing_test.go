// transform_routing_test.go - Tests for configured routing
// (route table -> next hop) and BGP control-plane
// (Route Server <-> NVA NIC) relationships of the OSIRIS JSON Microsoft
// Azure producer.
//
// An IP-addressed reference becomes a connection only on a unique match
// in scope (the referencing VNet and the VNets an exported peering
// links it with); every other case must stay unresolved, never guessed.

package azure

import (
	"testing"

	"go.osirisjson.org/producers/pkg/sdk"
)

const (
	rVNet1   = tRG + "/Microsoft.Network/virtualNetworks/LAB-VNET01"
	rVNet2   = tRG + "/Microsoft.Network/virtualNetworks/LAB-VNET02"
	rVNet3   = tRG + "/Microsoft.Network/virtualNetworks/LAB-VNET03"
	rSnApp   = rVNet1 + "/subnets/LAB-SNET-APP"
	rSnARS   = rVNet1 + "/subnets/RouteServerSubnet"
	rSnV2    = rVNet2 + "/subnets/LAB-SNET-NVA"
	rSnV3    = rVNet3 + "/subnets/LAB-SNET-ISO"
	rRT      = tRG + "/Microsoft.Network/routeTables/LAB-RT01"
	rRTOrph  = tRG + "/Microsoft.Network/routeTables/LAB-RT02"
	rILB     = tRG + "/Microsoft.Network/loadBalancers/LAB-ILB01"
	rNVA1    = tRG + "/Microsoft.Network/networkInterfaces/LAB-NVA01-nic"
	rNVA2    = tRG + "/Microsoft.Network/networkInterfaces/LAB-NVA02-nic"
	rDupA    = tRG + "/Microsoft.Network/networkInterfaces/LAB-DUP-A-nic"
	rDupB    = tRG + "/Microsoft.Network/networkInterfaces/LAB-DUP-B-nic"
	rIso     = tRG + "/Microsoft.Network/networkInterfaces/LAB-ISO-nic"
	rARS     = tRG + "/Microsoft.Network/virtualHubs/LAB-ARS01"
	rARSNoIP = tRG + "/Microsoft.Network/virtualHubs/LAB-ARS02"
)

// routingFixture: VNet1 (route table, Route Server, ILB, NVA01) is
// peered with VNet2 (NVA02); VNet3 is not peered. 192.0.2.50 is carried
// by a NIC in VNet1 and one in VNet2 (ambiguous); 192.0.2.60 only
// in VNet3.
func routingFixture(t *testing.T) ([]NetworkInterface, []LoadBalancer, []VNetPeering) {
	nic := func(id, subnet, ip string) string {
		return `{"id":"` + id + `","ipConfigurations":[{"name":"ipconfig1","privateIPAddress":"` + ip + `","subnet":{"id":"` + subnet + `"}}]}`
	}
	nics := decode[[]NetworkInterface](t, `[`+
		nic(rNVA1, lowerRG(rSnApp), "192.0.2.4")+`,`+
		nic(rNVA2, rSnV2, "198.51.100.4")+`,`+
		nic(rDupA, rSnApp, "192.0.2.50")+`,`+
		nic(rDupB, rSnV2, "192.0.2.50")+`,`+
		nic(rIso, rSnV3, "192.0.2.60")+`]`)
	lbs := decode[[]LoadBalancer](t, `[{"id":"`+rILB+`","frontendIPConfigurations":[{"name":"LAB-FE","privateIPAddress":"192.0.2.10","subnet":{"id":"`+rSnApp+`"}}]}]`)
	peerings := decode[[]VNetPeering](t, `[{"id":"`+rVNet1+`/virtualNetworkPeerings/LAB-V1-V2","name":"LAB-V1-V2","peeringState":"Connected","remoteVirtualNetwork":{"id":"`+lowerRG(rVNet2)+`"}}]`)
	return nics, lbs, peerings
}

func TestRouteNextHopConnections(t *testing.T) {
	nics, lbs, peerings := routingFixture(t)
	tables := decode[[]RouteTable](t, `[
		{"id":"`+rRT+`","name":"LAB-RT01","subnets":[{"id":"`+lowerRG(rSnApp)+`"}],"routes":[
			{"name":"LAB-TO-ILB-A","addressPrefix":"198.51.100.0/25","nextHopType":"VirtualAppliance","nextHopIpAddress":"192.0.2.10"},
			{"name":"LAB-TO-ILB-B","addressPrefix":"198.51.100.128/25","nextHopType":"VirtualAppliance","nextHopIpAddress":"192.0.2.10"},
			{"name":"LAB-TO-NVA01","addressPrefix":"203.0.113.0/25","nextHopType":"VirtualAppliance","nextHopIpAddress":"192.0.2.4"},
			{"name":"LAB-TO-NVA02-PEERED","addressPrefix":"203.0.113.128/25","nextHopType":"VirtualAppliance","nextHopIpAddress":"198.51.100.4"},
			{"name":"LAB-AMBIGUOUS","addressPrefix":"192.0.2.192/27","nextHopType":"VirtualAppliance","nextHopIpAddress":"192.0.2.50"},
			{"name":"LAB-NOT-PEERED","addressPrefix":"192.0.2.224/27","nextHopType":"VirtualAppliance","nextHopIpAddress":"192.0.2.60"},
			{"name":"LAB-UNKNOWN","addressPrefix":"192.0.2.240/28","nextHopType":"VirtualAppliance","nextHopIpAddress":"192.0.2.99"},
			{"name":"LAB-INTERNET","addressPrefix":"203.0.113.0/24","nextHopType":"Internet"}]},
		{"id":"`+rRTOrph+`","name":"LAB-RT02","routes":[
			{"name":"LAB-ORPHAN","addressPrefix":"198.51.100.0/24","nextHopType":"VirtualAppliance","nextHopIpAddress":"192.0.2.10"}]}]`)
	idx := buildPrivateIPIndex(nics, lbs, true)
	conns, st := TransformRouteNextHopConnections(tables, nil, peerings, idx, idMapOf(rRT, rRTOrph), idMapOf(rILB, rNVA1, rNVA2, rDupA, rDupB, rIso))

	if st.Resolved != 4 || st.Ambiguous != 1 || st.Unresolved != 2 || st.NoScope != 1 {
		t.Errorf("stats = %+v, want resolved 4, ambiguous 1, unresolved 2, route table without subnet 1", st)
	}
	byTarget := map[string]sdk.Connection{}
	for _, c := range conns {
		if c.Type != connRouteNextHop || c.Direction != "forward" || c.Source != resourceID("", rRT) {
			t.Errorf("unexpected connection %+v", c)
		}
		byTarget[c.Target] = c
	}
	if len(conns) != 3 {
		t.Fatalf("want 3 next-hop connections (ILB, NVA01, NVA02 via peering), got %d", len(conns))
	}
	ilb := byTarget[resourceID("", rILB)]
	if ilb.Properties["route_count"] != 2 || ilb.Properties["address_prefix_count"] != 2 {
		t.Errorf("two routes to one ILB must aggregate into one connection: %v", ilb.Properties)
	}
	if _, ok := byTarget[resourceID("", rNVA2)]; !ok {
		t.Error("next hop in a peered VNet must resolve")
	}
	for _, bad := range []string{rDupA, rDupB, rIso} {
		if _, ok := byTarget[resourceID("", bad)]; ok {
			t.Errorf("ambiguous or out-of-scope next hop resolved to %s", bad)
		}
	}
}

func TestRouteServerPeerConnections(t *testing.T) {
	nics, _, peerings := routingFixture(t)
	servers := decode[[]RouteServer](t, `[
		{"id":"`+rARS+`","name":"LAB-ARS01","virtualRouterAsn":65515,"allowBranchToBranchTraffic":false,
		 "ipConfigurations":[{"name":"ipconfig1","subnet":{"id":"`+lowerRG(rSnARS)+`"}}],
		 "bgpConnections":[
			{"name":"LAB-PEER-NVA01","peerIp":"192.0.2.4","peerAsn":64501,"connectionState":"Connected"},
			{"name":"LAB-PEER-NVA02","peerIp":"198.51.100.4","peerAsn":64502},
			{"name":"LAB-PEER-DUP","peerIp":"192.0.2.50","peerAsn":64503},
			{"name":"LAB-PEER-ONPREM","peerIp":"203.0.113.9","peerAsn":64504}]},
		{"id":"`+rARSNoIP+`","name":"LAB-ARS02","bgpConnections":[{"name":"LAB-PEER-X","peerIp":"192.0.2.4","peerAsn":64505}]}]`)
	idx := buildPrivateIPIndex(nics, nil, false)
	conns, res := TransformRouteServerPeerConnections(servers, peerings, idx, idMapOf(rARS, rARSNoIP), idMapOf(rNVA1, rNVA2, rDupA, rDupB, rIso))

	if len(conns) != 2 {
		t.Fatalf("want BGP sessions to NVA01 (same VNet) and NVA02 (peered VNet) only, got %d: %+v", len(conns), conns)
	}
	for _, c := range conns {
		if c.Type != "network.bgp" || c.Direction != "bidirectional" || c.Target != resourceID("", rARS) {
			t.Errorf("unexpected BGP connection %+v", c)
		}
	}
	if conns[0].State != "Connected" || conns[0].Properties["peer_asn"] != int64(64501) || conns[0].Properties["route_server_asn"] != int64(65515) {
		t.Errorf("BGP session properties = %v state %q", conns[0].Properties, conns[0].State)
	}
	want := map[string]string{"LAB-PEER-NVA01": "resolved", "LAB-PEER-NVA02": "resolved", "LAB-PEER-DUP": "ambiguous", "LAB-PEER-ONPREM": "unresolved"}
	for name, outcome := range want {
		if got := res[armKey(rARS)][name].outcome; got != outcome {
			t.Errorf("peer %s outcome = %q, want %q", name, got, outcome)
		}
	}
	if got := res[armKey(rARSNoIP)]["LAB-PEER-X"].outcome; got != "no_placement" {
		t.Errorf("Route Server without IP configurations must not resolve peers, got %q", got)
	}

	// Every peer, resolved or not, is preserved on the Route Server.
	resources := []sdk.Resource{{ID: resourceID("", rARS), Provider: sdk.Provider{NativeID: rARS}, Properties: map[string]any{}}}
	annotateRouteServerPeers(resources, servers, res)
	peers := resources[0].Extensions[extensionNamespace].(map[string]any)["bgp_connections"].([]map[string]any)
	if len(peers) != 4 || resources[0].Properties["bgp_peer_count"] != 4 {
		t.Fatalf("all 4 peers must be preserved, got %v", peers)
	}
	for _, p := range peers {
		if p["name"] == "LAB-PEER-ONPREM" && (p["resolution"] != "unresolved" || p["resource_id"] != nil) {
			t.Errorf("unresolved peer must carry no resource association: %v", p)
		}
		if p["name"] == "LAB-PEER-NVA01" && p["resource_id"] != resourceID("", rNVA1) {
			t.Errorf("resolved peer must name its NIC: %v", p)
		}
	}
}
