// transform_platform_test.go - Tests for flat ARM decoding and the
// platform networking and SQL relationships of the OSIRIS JSON
// Microsoft Azure producer: private endpoint -> target,
// oad balancer -> backend, VMSS -> NSG, subnet -> route table,
// NAT gateway, SQL database -> elastic pool.
//
// Inputs are decoded from JSON in the flat shape az list commands
// return, so the decoding fallback is exercised together with each
// builder. Every positive case names its ARM evidence; every negative
// case removes it while keeping names correlated and asserts
// that no edge is inferred.

package azure

import (
	"encoding/json"
	"strings"
	"testing"

	"go.osirisjson.org/producers/pkg/sdk"
)

const (
	tRG      = tSub + "/resourceGroups/RG-LAB/providers"
	tPE      = tRG + "/Microsoft.Network/privateEndpoints/LAB-PE01"
	tSA      = tRG + "/Microsoft.Storage/storageAccounts/labsa01"
	tKV      = tRG + "/Microsoft.KeyVault/vaults/LAB-KV01"
	tLB      = tRG + "/Microsoft.Network/loadBalancers/LAB-LB01"
	tPoolWeb = tLB + "/backendAddressPools/LAB-POOL-WEB"
	tPoolOut = tLB + "/backendAddressPools/LAB-POOL-OUT"
	tVMSS    = tRG + "/Microsoft.Compute/virtualMachineScaleSets/LAB-VMSS01"
	tSubnet  = tRG + "/Microsoft.Network/virtualNetworks/LAB-VNET01/subnets/LAB-SNET01"
	tNAT     = tRG + "/Microsoft.Network/natGateways/LAB-NAT01"
	tSQL     = tRG + "/Microsoft.Sql/servers/lab-sql01"
	tSQLDB   = tSQL + "/databases/LAB-DB01"
	tSQLPool = tSQL + "/elasticPools/LAB-POOL01"
)

func decode[T any](t *testing.T, js string) T {
	t.Helper()
	var v T
	if err := json.Unmarshal([]byte(js), &v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return v
}

func TestFlatPropertiesDecoding(t *testing.T) {
	// Flat (az list) and nested (ARM) forms decode to the same fields.
	flat := decode[SQLDatabase](t, `{"id":"`+tSQLDB+`","status":"Online","collation":"SQL_Latin1_General_CP1_CI_AS","elasticPoolId":"`+tSQLPool+`"}`)
	nested := decode[SQLDatabase](t, `{"id":"`+tSQLDB+`","properties":{"status":"Online","collation":"SQL_Latin1_General_CP1_CI_AS","elasticPoolId":"`+tSQLPool+`"}}`)
	for name, db := range map[string]SQLDatabase{"flat": flat, "nested": nested} {
		if db.Properties == nil || db.Properties.Status != "Online" || db.Properties.ElasticPoolID != tSQLPool {
			t.Errorf("%s form: properties = %+v", name, db.Properties)
		}
	}
	// Nested form wins; top-level fields with the same name are
	//not mixed in.
	both := decode[SQLDatabase](t, `{"id":"`+tSQLDB+`","status":"Paused","properties":{"status":"Online"}}`)
	if both.Properties.Status != "Online" {
		t.Errorf("nested properties must not be overridden, got %q", both.Properties.Status)
	}
	// An object carrying none of the fields leaves Properties nil.
	if bare := decode[SQLDatabase](t, `{"id":"`+tSQLDB+`","name":"LAB-DB01"}`); bare.Properties != nil {
		t.Errorf("object without properties fields must leave Properties nil, got %+v", bare.Properties)
	}
	// A field with an unexpected JSON type is skipped, never
	// failing the list.
	list := decode[[]SQLDatabase](t, `[{"id":"`+tSQLDB+`","maxSizeBytes":"large","status":"Online"}]`)
	if len(list) != 1 || list[0].Properties == nil || list[0].Properties.Status != "Online" {
		t.Errorf("type mismatch must not drop the object: %+v", list)
	}
	// Nested flat levels: VMSS -> NIC configuration -> IP configuration.
	v := decode[VMSS](t, `{"id":"`+tVMSS+`","orchestrationMode":"Uniform","virtualMachineProfile":{"networkProfile":{"networkInterfaceConfigurations":[{"name":"nic","networkSecurityGroup":{"id":"`+tNSG+`"},"ipConfigurations":[{"name":"ip","subnet":{"id":"`+tSubnet+`"},"loadBalancerBackendAddressPools":[{"id":"`+tPoolWeb+`"}]}]}]}}}`)
	if v.Properties == nil || v.Properties.OrchestrationMode != "Uniform" {
		t.Fatalf("VMSS flat properties not decoded: %+v", v.Properties)
	}
	if got := v.VMSSSubnetIDs(); len(got) != 1 || got[0] != tSubnet {
		t.Errorf("VMSS subnets = %v", got)
	}
	if got := v.VMSSNSGIDs(); len(got) != 1 || got[0] != tNSG {
		t.Errorf("VMSS NSGs = %v", got)
	}
	if got := v.VMSSBackendPoolIDs(); len(got) != 1 || got[0] != tPoolWeb {
		t.Errorf("VMSS backend pools = %v", got)
	}
	// Private endpoint references on a target, flat and nested.
	refs := decode[[]azPrivateEndpointConnRef](t, `[{"privateEndpoint":{"id":"a"}},{"properties":{"privateEndpoint":{"id":"b"}}}]`)
	if refs[0].PrivateEndpointID() != "a" || refs[1].PrivateEndpointID() != "b" {
		t.Errorf("private endpoint refs = %q %q", refs[0].PrivateEndpointID(), refs[1].PrivateEndpointID())
	}
}

func onlyConn(t *testing.T, conns []sdk.Connection, connType, source, target string) sdk.Connection {
	t.Helper()
	if len(conns) != 1 {
		t.Fatalf("expected 1 connection, got %d: %+v", len(conns), conns)
	}
	c := conns[0]
	if c.Type != connType || c.Source != resourceID("", source) || c.Target != resourceID("", target) || c.Direction != "forward" {
		t.Errorf("connection = %s %s -> %s (%s), want %s %s -> %s forward", c.Type, c.Source, c.Target, c.Direction, connType, source, target)
	}
	return c
}

func TestPrivateEndpointToTarget(t *testing.T) {
	pe := func(js string) []PrivateEndpoint {
		return decode[[]PrivateEndpoint](t, `[{"id":"`+tPE+`","name":"LAB-PE01",`+js+`}]`)
	}
	peIDs, docIDs := idMapOf(tPE), idMapOf(tSA, tKV)

	// Evidence: privateLinkServiceConnections[].privateLinkServiceId,
	// case-differing.
	c := onlyConn(t, TransformPrivateEndpointToTargetConnections(pe(`"privateLinkServiceConnections":[
		{"privateLinkServiceId":"`+lowerRG(tSA)+`","groupIds":["blob"],"privateLinkServiceConnectionState":{"status":"Approved"}},
		{"privateLinkServiceId":"`+tSA+`","groupIds":["file","blob"],"privateLinkServiceConnectionState":{"status":"Approved"}}]`),
		peIDs, docIDs), connNetwork, tPE, tSA)
	if c.Status != "active" || c.State != "Approved" {
		t.Errorf("status/state = %q/%q, want active/Approved", c.Status, c.State)
	}
	if g, _ := c.Properties["group_ids"].([]string); len(g) != 2 || g[0] != "blob" || g[1] != "file" {
		t.Errorf("group_ids = %v, want merged [blob file]", c.Properties["group_ids"])
	}
	if c.Properties["manual_approval"] != false {
		t.Errorf("manual_approval = %v", c.Properties["manual_approval"])
	}

	// Evidence: manualPrivateLinkServiceConnections, not approved.
	c = onlyConn(t, TransformPrivateEndpointToTargetConnections(pe(`"manualPrivateLinkServiceConnections":[
		{"privateLinkServiceId":"`+tKV+`","groupIds":["vault"],"privateLinkServiceConnectionState":{"status":"Disconnected"}}]`),
		peIDs, docIDs), connNetwork, tPE, tKV)
	if c.Status != "inactive" || c.State != "Disconnected" || c.Properties["manual_approval"] != true {
		t.Errorf("manual binding = %+v", c)
	}

	// Negative: target outside the document, empty target, no bindings.
	for name, js := range map[string]string{
		"absent target": `"privateLinkServiceConnections":[{"privateLinkServiceId":"` + tRG + `/Microsoft.Storage/storageAccounts/labsa99"}]`,
		"empty target":  `"manualPrivateLinkServiceConnections":[{"privateLinkServiceId":""}]`,
		"no binding":    `"customDnsConfigs":[{"fqdn":"labsa01.example.com"}]`,
	} {
		if got := TransformPrivateEndpointToTargetConnections(pe(js), peIDs, docIDs); len(got) != 0 {
			t.Errorf("%s: expected no edge, got %+v", name, got)
		}
	}
}

func TestLBBackendConnections(t *testing.T) {
	lbs := decode[[]LoadBalancer](t, `[{"id":"`+tLB+`","name":"LAB-LB01",
		"backendAddressPools":[
			{"id":"`+tPoolWeb+`","name":"LAB-POOL-WEB","backendIpConfigurations":[{"id":"`+tNIC1+`/ipConfigurations/ipconfig1"}]},
			{"id":"`+tPoolOut+`","name":"LAB-POOL-OUT","backendIpConfigurations":[{"id":"`+tNIC2+`/ipConfigurations/ipconfig1"}]}],
		"loadBalancingRules":[{"name":"LAB-RULE-HTTPS","backendAddressPool":{"id":"`+lowerRG(tPoolWeb)+`"}}]}]`)
	// NIC1 also names the pool from its own side; NIC2 only sits in the
	// pool without a rule.
	nics := decode[[]NetworkInterface](t, `[
		{"id":"`+tNIC1+`","ipConfigurations":[{"name":"ipconfig1","loadBalancerBackendAddressPools":[{"id":"`+tPoolWeb+`"}]}]},
		{"id":"`+tNIC2+`","ipConfigurations":[{"name":"ipconfig1","loadBalancerBackendAddressPools":[{"id":"`+tPoolOut+`"}]}]}]`)
	vmss := decode[[]VMSS](t, `[{"id":"`+tVMSS+`","virtualMachineProfile":{"networkProfile":{"networkInterfaceConfigurations":[{"ipConfigurations":[{"loadBalancerBackendAddressPools":[{"id":"`+tPoolWeb+`"}]}]}]}}}]`)

	conns := TransformLBBackendConnections(lbs, nics, vmss, idMapOf(tLB), idMapOf(tNIC1, tNIC2), idMapOf(tVMSS))
	got := map[string]sdk.Connection{}
	for _, c := range conns {
		got[c.Target] = c
	}
	if len(conns) != 2 {
		t.Fatalf("expected LB -> NIC1 and LB -> VMSS (rule-backed pool only, deduplicated), got %d: %+v", len(conns), conns)
	}
	for _, target := range []string{tNIC1, tVMSS} {
		c, ok := got[resourceID("", target)]
		if !ok || c.Type != connNetwork || c.Source != resourceID("", tLB) {
			t.Errorf("missing LB -> %s: %+v", target, c)
			continue
		}
		if p, _ := c.Properties["backend_pools"].([]string); len(p) != 1 || p[0] != "LAB-POOL-WEB" {
			t.Errorf("backend_pools = %v", c.Properties["backend_pools"])
		}
	}

	// Negative: no rule anywhere, so no pool is rule-backed.
	lbs[0].LoadBalancingRules = nil
	if c := TransformLBBackendConnections(lbs, nics, vmss, idMapOf(tLB), idMapOf(tNIC1, tNIC2), idMapOf(tVMSS)); len(c) != 0 {
		t.Errorf("pool without rule must yield no edge, got %+v", c)
	}
}

func TestVMSSToNSG(t *testing.T) {
	vmss := decode[[]VMSS](t, `[{"id":"`+tVMSS+`","name":"LAB-VMSS01","virtualMachineProfile":{"networkProfile":{"networkInterfaceConfigurations":[
		{"networkSecurityGroup":{"id":"`+lowerRG(tNSG)+`"}},{"networkSecurityGroup":{"id":"`+tNSG+`"}}]}}}]`)
	onlyConn(t, TransformVMSSToNSGConnections(vmss, idMapOf(tVMSS), idMapOf(tNSG)), connNSGAssociation, tVMSS, tNSG)

	none := decode[[]VMSS](t, `[{"id":"`+tVMSS+`","name":"LAB-NSG01","virtualMachineProfile":{"networkProfile":{"networkInterfaceConfigurations":[{}]}}}]`)
	if c := TransformVMSSToNSGConnections(none, idMapOf(tVMSS), idMapOf(tNSG)); len(c) != 0 {
		t.Errorf("VMSS without NSG reference must yield no edge, got %+v", c)
	}
}

func TestSubnetAssociations(t *testing.T) {
	tRT := tRG + "/Microsoft.Network/routeTables/LAB-RT01"
	subnets := decode[[]Subnet](t, `[{"id":"`+tSubnet+`","name":"LAB-SNET01","routeTable":{"id":"`+lowerRG(tRT)+`"},"natGateway":{"id":"`+lowerRG(tNAT)+`"}}]`)
	onlyConn(t, TransformSubnetRouteTableConnections(subnets, idMapOf(tSubnet), idMapOf(tRT)), connRouteTableAssociation, tSubnet, tRT)

	// The NAT attachment is stated on both sides and yields one connection ID.
	gws := decode[[]NATGateway](t, `[{"id":"`+tNAT+`","name":"LAB-NAT01","subnets":[{"id":"`+tSubnet+`"}]}]`)
	conns := TransformSubnetToNATGatewayConnections(subnets, gws, idMapOf(tSubnet))
	if len(conns) != 2 || conns[0].ID != conns[1].ID {
		t.Fatalf("subnet and gateway sides must produce the same connection ID: %+v", conns)
	}
	final, _ := finalizeConnections(conns, map[string]struct{}{resourceID("", tSubnet): {}, resourceID("", tNAT): {}})
	onlyConn(t, final, connNATGatewayAssociation, tSubnet, tNAT)

	// Negative: no reference on either side.
	bare := decode[[]Subnet](t, `[{"id":"`+tSubnet+`","name":"LAB-SNET01"}]`)
	gwBare := decode[[]NATGateway](t, `[{"id":"`+tNAT+`","name":"LAB-SNET01-nat"}]`)
	if c := TransformSubnetToNATGatewayConnections(bare, gwBare, idMapOf(tSubnet)); len(c) != 0 {
		t.Errorf("no NAT reference must yield no edge, got %+v", c)
	}
	if c := TransformSubnetRouteTableConnections(bare, idMapOf(tSubnet), idMapOf(tRT)); len(c) != 0 {
		t.Errorf("no route table reference must yield no edge, got %+v", c)
	}
}

func TestSQLDatabaseToElasticPool(t *testing.T) {
	db := decode[SQLDatabase](t, `{"id":"`+tSQLDB+`","name":"LAB-DB01","elasticPoolId":"`+lowerRG(tSQLPool)+`"}`)
	servers := []SQLServer{{ID: tSQL, Databases: []SQLDatabase{db}}}
	onlyConn(t, TransformSQLDatabaseToElasticPoolConnections(servers, idMapOf(tSQLDB), idMapOf(tSQLPool)), connDependency, tSQLDB, tSQLPool)

	// Negative: elasticPoolName alone (no ARM ID) is not evidence.
	db = decode[SQLDatabase](t, `{"id":"`+tSQLDB+`","name":"LAB-DB01","elasticPoolName":"LAB-POOL01","status":"Online"}`)
	servers[0].Databases = []SQLDatabase{db}
	if c := TransformSQLDatabaseToElasticPoolConnections(servers, idMapOf(tSQLDB), idMapOf(tSQLPool)); len(c) != 0 {
		t.Errorf("database without elasticPoolId must yield no edge, got %+v", c)
	}
}

// TestGatewayConnectionsCanonicalKeys builds the ID map exactly as the
// producer does (canonical ARM keys) and decodes the az CLI shape of
// Microsoft.Network/connections, with references whose casing differs
// from the referenced resources, and a circuit in another subscription.
func TestGatewayConnectionsCanonicalKeys(t *testing.T) {
	gw := tRG + "/Microsoft.Network/virtualNetworkGateways/LAB-ERGW01"
	er := tRG + "/Microsoft.Network/expressRouteCircuits/LAB-ER01"
	remoteER := "/subscriptions/00000000-0000-0000-0000-000000000002/resourceGroups/RG-LAB-DR/providers/Microsoft.Network/expressRouteCircuits/LAB-ER02"
	conns := decode[[]GatewayConnection](t, `[
		{"id":"`+tRG+`/Microsoft.Network/connections/LAB-ERCONN01","name":"LAB-ERCONN01","connectionType":"ExpressRoute","provisioningState":"Succeeded",
		 "virtualNetworkGateway1":{"id":"`+lowerRG(gw)+`"},"peer":{"id":"`+lowerRG(er)+`"}},
		{"id":"`+tRG+`/Microsoft.Network/connections/LAB-ERCONN02","name":"LAB-ERCONN02","connectionType":"ExpressRoute","provisioningState":"Succeeded",
		 "virtualNetworkGateway1":{"id":"`+gw+`"},"peer":{"id":"`+remoteER+`"}}]`)
	idMap := BuildAllResourceIDMap(idMapOf(gw), idMapOf(er))

	all, stubs := TransformGatewayConnections(conns, idMap)
	edges, endpoints := splitERConnectionEndpoints(all)
	if len(edges) != 2 {
		t.Fatalf("expected 2 gateway <-> circuit connections, got %d", len(edges))
	}
	// Each ExpressRoute connection resource reaches its gateway and its
	// circuit (a stub for the cross-subscription one) through
	// explicit edges.
	ends := map[string]map[string]string{}
	for _, e := range endpoints {
		if e.Direction != "forward" {
			t.Errorf("endpoint edge direction = %q", e.Direction)
		}
		if ends[e.Source] == nil {
			ends[e.Source] = map[string]string{}
		}
		ends[e.Source][e.Properties["endpoint_role"].(string)] = e.Target
		// The authoritative reference is kept exactly as the connection
		// states it (here with a lower-cased resource group segment).
		arm, _ := e.Properties["source_arm_id"].(string)
		if armKey(arm) != armKey(strings.TrimPrefix(e.Target, "azure::")) {
			t.Errorf("source_arm_id %q does not reference the edge target %s", arm, e.Target)
		}
	}
	for _, e := range endpoints {
		if e.Source == resourceID("", conns[0].ID) && e.Properties["endpoint_role"] == "gateway" && e.Properties["source_arm_id"] != lowerRG(gw) {
			t.Errorf("source_arm_id must be the ARM ID as stated by the connection, got %v", e.Properties["source_arm_id"])
		}
	}
	for i, c := range conns {
		got := ends[resourceID("", c.ID)]
		if got["gateway"] != edges[i].Source || got["circuit"] != edges[i].Target {
			t.Errorf("connection %s endpoints = %v, want gateway %s circuit %s", c.Name, got, edges[i].Source, edges[i].Target)
		}
	}
	if edges[0].Source != resourceID("", gw) || edges[0].Target != resourceID("", er) {
		t.Errorf("in-scope connection = %s -> %s", edges[0].Source, edges[0].Target)
	}
	if len(stubs) != 1 || stubs[0].Provider.NativeID != remoteER || stubs[0].Provider.Account != "00000000-0000-0000-0000-000000000002" {
		t.Fatalf("cross-subscription circuit must become one stub keeping its subscription: %+v", stubs)
	}
	if edges[1].Target != stubs[0].ID {
		t.Errorf("cross-subscription connection must target the stub, got %s", edges[1].Target)
	}
	// The edge names the connection resource that configures it.
	if edges[0].Type != "network" || edges[0].Direction != "bidirectional" ||
		edges[0].Properties["connection_id"] != resourceID("", conns[0].ID) || edges[0].Properties["connection_type"] != "ExpressRoute" {
		t.Errorf("gateway <-> circuit edge = %s %s %v", edges[0].Type, edges[0].Direction, edges[0].Properties)
	}
	res, _ := TransformGatewayConnectionResources(conns, SubscriptionInfo{})
	if len(res) != 2 || res[0].Type != "osiris.azure.expressroute.connection" || res[0].ID != resourceID("", conns[0].ID) {
		t.Errorf("ExpressRoute connection resources = %+v", res)
	}
	vpn := decode[[]GatewayConnection](t, `[{"id":"`+tRG+`/Microsoft.Network/connections/LAB-S2S01","connectionType":"IPsec"}]`)
	if r, _ := TransformGatewayConnectionResources(vpn, SubscriptionInfo{}); r[0].Type != "network.vpn" {
		t.Errorf("IPsec connection type = %q, want network.vpn", r[0].Type)
	}
}
