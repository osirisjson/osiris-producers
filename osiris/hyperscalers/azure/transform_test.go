// transform_test.go Unit tests for Microsoft Azure data
// transformation to OSIRIS JSON mapping functions.
//
// For an introduction to OSIRIS JSON Producer for Microsoft Azure see:
// [OSIRIS-JSON-AZURE]: https://docs.osirisjson.org/osiris-producers/hyperscalers/microsoft-azure/
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package azure

import (
	"strings"
	"testing"

	"go.osirisjson.org/producers/pkg/sdk"
)

var testSub = SubscriptionInfo{
	SubscriptionID: "00000000-0000-4000-8000-00000000a1b2",
	DisplayName:    "my-nonprod-subscription",
	State:          "Enabled",
	TenantID:       "00000000-0000-4000-8000-00000000f1e2",
	Tags: map[string]string{
		"environment": "np",
		"department":  "information technology",
	},
}

func TestTransformVNets(t *testing.T) {
	vnets := []VirtualNetwork{
		{
			ID:            "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Network/virtualNetworks/labvnet1",
			Name:          "labvnet1",
			Location:      "westeurope",
			ResourceGroup: "rg-lab-01",
			AddressSpace:  azAddressSpace{AddressPrefixes: []string{"192.0.2.0/24"}},
			DhcpOptions:   azDHCPOptions{DNSServers: []string{"192.0.2.4"}},
		},
	}

	resources := TransformVNets(vnets, nil, testSub)

	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}

	r := resources[0]
	if r.Type != "network.vpc" {
		t.Errorf("expected type network.vnet, got %q", r.Type)
	}
	if r.Name != "labvnet1" {
		t.Errorf("expected name labvnet1, got %q", r.Name)
	}
	if r.Provider.Name != "azure" {
		t.Errorf("expected provider azure, got %q", r.Provider.Name)
	}
	if r.Provider.Region != "westeurope" {
		t.Errorf("expected region westeurope, got %q", r.Provider.Region)
	}
	if r.Provider.Account != "00000000-0000-4000-8000-000000001111" || r.Provider.Subscription != "" || r.Provider.Tenant != "" {
		t.Errorf("provider = account %q subscription %q tenant %q", r.Provider.Account, r.Provider.Subscription, r.Provider.Tenant)
	}
	// The input carries no provisioningState, so status is omitted.
	if r.Status != "" {
		t.Errorf("expected no status without provisioningState, got %q", r.Status)
	}
	if r.Properties["resource_group"] != "rg-lab-01" {
		t.Errorf("expected resource_group rg-lab-01, got %v", r.Properties["resource_group"])
	}
	addrs, ok := r.Properties["address_space"].([]string)
	if !ok || len(addrs) != 1 || addrs[0] != "192.0.2.0/24" {
		t.Errorf("expected address_space [192.0.2.0/24], got %v", r.Properties["address_space"])
	}
}

func TestTransformSubnets(t *testing.T) {
	subnets := []Subnet{
		{
			ID:                   "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Network/virtualNetworks/labvnet1/subnets/labsubnet1",
			Name:                 "labsubnet1",
			ResourceGroup:        "rg-lab-01",
			AddressPrefixes:      []string{"192.0.2.128/25"},
			NetworkSecurityGroup: &azNSGRef{ID: "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Network/networkSecurityGroups/labnsg1"},
			RouteTable:           &azRouteTableRef{ID: "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Network/routeTables/labrt1"},
			ServiceEndpoints:     []azServiceEndpoint{{Service: "Microsoft.Storage", Locations: []string{"*"}}},
		},
	}

	resources, idMap := TransformSubnets(subnets, nil, testSub)

	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	if len(idMap) != 1 {
		t.Fatalf("expected 1 ID mapping, got %d", len(idMap))
	}

	r := resources[0]
	if r.Type != "network.subnet" {
		t.Errorf("expected type network.subnet, got %q", r.Type)
	}
	if r.Name != "labsubnet1" {
		t.Errorf("expected name labsubnet1, got %q", r.Name)
	}

	// Verify the ID map contains the subnet's ARM ID.
	if _, ok := idMap[armKey(subnets[0].ID)]; !ok {
		t.Error("expected subnet ARM ID in ID map")
	}
}

func TestTransformSubnetsSingularAddressPrefix(t *testing.T) {
	subnets := []Subnet{
		{
			ID:            "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Network/virtualNetworks/labvnet1/subnets/labs1",
			Name:          "s1",
			ResourceGroup: "rg-lab-01",
			AddressPrefix: "192.0.2.64/27",
		},
	}

	resources, _ := TransformSubnets(subnets, nil, testSub)
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	r := resources[0]
	prefixes, ok := r.Properties["address_prefixes"].([]string)
	if !ok || len(prefixes) != 1 || prefixes[0] != "192.0.2.64/27" {
		t.Errorf("expected address_prefixes [192.0.2.64/27] from singular addressPrefix, got %v", r.Properties["address_prefixes"])
	}
}

func TestTransformNICs(t *testing.T) {
	nics := []NetworkInterface{
		{
			ID:            "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Network/networkInterfaces/labnic1",
			Name:          "labnic1",
			Location:      "westeurope",
			ResourceGroup: "rg-lab-01",
			IPConfigurations: []IPConfiguration{
				{
					Name:                      "ipconfig1",
					Subnet:                    &azSubnetRef{ID: "/subscriptions/00000000-0000-4000-8000-000000001111/.../subnets/labsubnet1"},
					PrivateIPAddress:          "192.0.2.132",
					PrivateIPAllocationMethod: "Dynamic",
				},
			},
		},
	}

	resources, idMap := TransformNICs(nics, testSub)

	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}

	r := resources[0]
	if r.Type != "network.interface" {
		t.Errorf("expected type network.interface, got %q", r.Type)
	}

	ips, ok := r.Properties["ip_configurations"].([]map[string]any)
	if !ok || len(ips) != 1 {
		t.Fatalf("expected 1 ip_configuration, got %v", r.Properties["ip_configurations"])
	}
	if ips[0]["private_ip"] != "192.0.2.132" {
		t.Errorf("expected private_ip 192.0.2.132, got %v", ips[0]["private_ip"])
	}

	if _, ok := idMap[armKey(nics[0].ID)]; !ok {
		t.Error("expected NIC ARM ID in ID map")
	}
}

func TestTransformVMs(t *testing.T) {
	vms := []VirtualMachine{
		{
			ID:            "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Compute/virtualMachines/labvm1",
			Name:          "labvm1",
			Location:      "westeurope",
			ResourceGroup: "rg-lab-01",
			VMSize:        "Standard_D2s_v3",
			PowerState:    "VM running",
		},
		{
			ID:            "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Compute/virtualMachines/labvm2",
			Name:          "labvm2",
			Location:      "westeurope",
			ResourceGroup: "rg-lab-01",
			VMSize:        "Standard_B2s",
			PowerState:    "VM deallocated",
		},
	}

	resources := TransformVMs(vms, testSub)

	if len(resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(resources))
	}

	if resources[0].Status != "active" {
		t.Errorf("expected running VM status active, got %q", resources[0].Status)
	}
	if resources[1].Status != "inactive" {
		t.Errorf("expected deallocated VM status inactive, got %q", resources[1].Status)
	}
	if resources[0].Type != "compute.vm" {
		t.Errorf("expected type compute.vm, got %q", resources[0].Type)
	}
	if resources[0].Provider.Type != "Microsoft.Compute/virtualMachines" {
		t.Errorf("expected provider.type Microsoft.Compute/virtualMachines, got %q", resources[0].Provider.Type)
	}
}

func TestTransformVNetPeerings(t *testing.T) {
	vnetIDMap := map[string]string{
		"/subscriptions/00000000-0000-4000-8000-000000001111/.../virtualNetworks/labvnet1": "azure::/subscriptions/00000000-0000-4000-8000-000000001111/.../virtualNetworks/labvnet1",
		"/subscriptions/00000000-0000-4000-8000-000000001111/.../virtualNetworks/labvnet2": "azure::/subscriptions/00000000-0000-4000-8000-000000001111/.../virtualNetworks/labvnet2",
	}

	peerings := []VNetPeering{
		{
			ID:                   "/subscriptions/00000000-0000-4000-8000-000000001111/.../virtualNetworks/labvnet1/virtualNetworkPeerings/peer1",
			Name:                 "vnet1-to-vnet2",
			RemoteVirtualNetwork: &azVNetRef{ID: "/subscriptions/00000000-0000-4000-8000-000000001111/.../virtualNetworks/labvnet2"},
			PeeringState:         "Connected",
			AllowGatewayTransit:  true,
		},
	}

	conns, stubs := TransformVNetPeerings(peerings, vnetIDMap)

	if len(conns) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(conns))
	}
	if len(stubs) != 0 {
		t.Errorf("expected 0 stubs for local peering, got %d", len(stubs))
	}

	c := conns[0]
	if c.Type != "network.peering" {
		t.Errorf("expected type network.peering, got %q", c.Type)
	}
	if c.Status != "active" {
		t.Errorf("expected status active for Connected peering, got %q", c.Status)
	}
	if c.Direction != "bidirectional" {
		t.Errorf("expected bidirectional direction, got %q", c.Direction)
	}
}

func TestTransformSubnetNSGConnections(t *testing.T) {
	subnets := []Subnet{
		{
			ID:                   "/sub/labsubnet1",
			Name:                 "labsubnet1",
			NetworkSecurityGroup: &azNSGRef{ID: "/sub/labnsg1"},
		},
		{
			ID:   "/sub/subnet2",
			Name: "subnet2",
			// No NSG.
		},
	}

	subnetIDMap := map[string]string{
		"/sub/labsubnet1": "azure::/sub/labsubnet1",
		"/sub/subnet2": "azure::/sub/subnet2",
	}
	nsgIDMap := map[string]string{
		"/sub/labnsg1": "azure::/sub/labnsg1",
	}

	conns := TransformSubnetNSGConnections(subnets, subnetIDMap, nsgIDMap)

	if len(conns) != 1 {
		t.Fatalf("expected 1 connection (only subnet with NSG), got %d", len(conns))
	}
	if conns[0].Type != connNSGAssociation {
		t.Errorf("expected type %s, got %q", connNSGAssociation, conns[0].Type)
	}
	if conns[0].Direction != "forward" {
		t.Errorf("expected forward direction, got %q", conns[0].Direction)
	}
	if conns[0].Source != "azure::/sub/labsubnet1" || conns[0].Target != "azure::/sub/labnsg1" {
		t.Errorf("expected labsubnet1 -> labnsg1, got %q -> %q", conns[0].Source, conns[0].Target)
	}
}

func TestTransformSubscriptionGroup(t *testing.T) {
	g := TransformSubscriptionGroup(testSub)

	if g.Type != "logical.subscription" {
		t.Errorf("expected type logical.subscription, got %q", g.Type)
	}
	if g.Name != "my-nonprod-subscription" {
		t.Errorf("expected subscription display name, got %q", g.Name)
	}
	if g.Tags["environment"] != "np" {
		t.Errorf("expected tag environment=np, got %v", g.Tags)
	}
}

func TestTransformResourceGroupGroups(t *testing.T) {
	rgs := []ResourceGroup{
		{
			ID:       "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01",
			Name:     "rg-lab-01",
			Location: "westeurope",
		},
		{
			ID:       "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-02",
			Name:     "rg-lab-02",
			Location: "eastus",
		},
	}

	groups, nameToID := TransformResourceGroupGroups(rgs, testSub)

	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	if len(nameToID) != 2 {
		t.Fatalf("expected 2 name mappings, got %d", len(nameToID))
	}

	if groups[0].Type != "logical.resourcegroup" {
		t.Errorf("expected type logical.resourcegroup, got %q", groups[0].Type)
	}
	if groups[0].Name != "rg-lab-01" {
		t.Errorf("expected name rg-lab-01, got %q", groups[0].Name)
	}

	// Verify case-insensitive name lookup.
	if _, ok := nameToID["rg-lab-01"]; !ok {
		t.Error("expected rg-lab-01 in nameToID map")
	}
}

func TestTransformRegionGroups(t *testing.T) {
	resources := []sdk.Resource{
		{ID: "r1", Provider: sdk.Provider{Region: "westeurope"}},
		{ID: "r2", Provider: sdk.Provider{Region: "westeurope"}},
		{ID: "r3", Provider: sdk.Provider{Region: "eastus"}},
		{ID: "r4", Provider: sdk.Provider{Region: "global"}},
		{ID: "r5", Provider: sdk.Provider{Region: ""}},
	}

	groups := TransformRegionGroups(resources, testSub)

	if len(groups) != 2 {
		t.Fatalf("expected 2 region groups (westeurope + eastus), got %d", len(groups))
	}

	byName := map[string]sdk.Group{}
	for _, g := range groups {
		if g.Type != "container.region" {
			t.Errorf("expected type container.region, got %q", g.Type)
		}
		byName[g.Name] = g
	}

	we, ok := byName["westeurope"]
	if !ok {
		t.Fatalf("expected westeurope group")
	}
	if len(we.Members) != 2 {
		t.Errorf("expected 2 members in westeurope, got %d", len(we.Members))
	}
	if we.Properties["region"] != "westeurope" {
		t.Errorf("expected region property westeurope, got %v", we.Properties["region"])
	}

	eu, ok := byName["eastus"]
	if !ok {
		t.Fatalf("expected eastus group")
	}
	if len(eu.Members) != 1 {
		t.Errorf("expected 1 member in eastus, got %d", len(eu.Members))
	}

	if _, bad := byName["global"]; bad {
		t.Error("region=global should be skipped")
	}
	if _, bad := byName[""]; bad {
		t.Error("empty region should be skipped")
	}
}

func TestExtractResourceGroup(t *testing.T) {
	tests := []struct {
		armID    string
		expected string
	}{
		{
			armID:    "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-my/providers/Microsoft.Network/virtualNetworks/labvnet1",
			expected: "rg-lab-my",
		},
		{
			armID:    "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/RG-LAB-MY",
			expected: "RG-LAB-MY",
		},
		{
			armID:    "/subscriptions/00000000-0000-4000-8000-000000000093",
			expected: "",
		},
	}

	for _, tt := range tests {
		got := extractResourceGroup(tt.armID)
		if got != tt.expected {
			t.Errorf("extractResourceGroup(%q) = %q, want %q", tt.armID, got, tt.expected)
		}
	}
}

func TestMapVMPowerState(t *testing.T) {
	tests := []struct {
		state    string
		expected string
	}{
		{"VM running", "active"},
		{"VM deallocated", "inactive"},
		{"VM stopped", "inactive"},
		{"VM starting", "degraded"},
		{"PowerState/running", "active"},
		{"", ""},
		{"VM unknown-state", "unknown"},
	}

	for _, tt := range tests {
		got := mapVMPowerState(tt.state)
		if got != tt.expected {
			t.Errorf("mapVMPowerState(%q) = %q, want %q", tt.state, got, tt.expected)
		}
	}
}

func TestMapPeeringState(t *testing.T) {
	tests := []struct {
		state    string
		expected string
	}{
		{"Connected", "active"},
		{"Disconnected", "inactive"},
		{"Initiated", "degraded"},
		{"Unknown", "unknown"},
	}

	for _, tt := range tests {
		got := mapPeeringState(tt.state)
		if got != tt.expected {
			t.Errorf("mapPeeringState(%q) = %q, want %q", tt.state, got, tt.expected)
		}
	}
}

func TestResourceIDFormat(t *testing.T) {
	armID := "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Network/virtualNetworks/labvnet1"
	id := resourceID("network.vpc", armID)

	// Per OSIRIS Producer Guidelines chapter 2 section 2.2.1:
	// hyperscaler IDs use provider::native-id.
	expected := "azure::" + armID
	if id != expected {
		t.Errorf("resourceID(%q) = %q, want %q", armID, id, expected)
	}

	// Deterministic: same input produces same output.
	id2 := resourceID("network.vpc", armID)
	if id != id2 {
		t.Errorf("resourceID not deterministic: %q != %q", id, id2)
	}

	// Different ARM IDs produce different resource IDs.
	armID2 := "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Network/virtualNetworks/labvnet2"
	id3 := resourceID("network.vpc", armID2)
	if id == id3 {
		t.Errorf("different ARM IDs should produce different resource IDs")
	}
}

func TestTransformVNetToSubnetConnections(t *testing.T) {
	subnets := []Subnet{
		{
			ID:   "/sub/labvnet1/subnets/labsubnet1",
			Name: "labsubnet1",
			// VNetID() derives "/sub/labvnet1" from the subnet ARM ID.
		},
		{
			ID:   "/sub/nosubnets",
			Name: "subnet2",
			// No /subnets/ in ID VNetID() returns "" should be skipped.
		},
	}
	subnetIDMap := map[string]string{
		"/sub/labvnet1/subnets/labsubnet1": "azure::/sub/labvnet1/subnets/labsubnet1",
		"/sub/nosubnets":             "azure::/sub/nosubnets",
	}
	vnetIDMap := map[string]string{
		"/sub/labvnet1": "azure::/sub/labvnet1",
	}

	conns := TransformVNetToSubnetConnections(subnets, subnetIDMap, vnetIDMap)

	if len(conns) != 1 {
		t.Fatalf("expected 1 connection (only subnet with VNet), got %d", len(conns))
	}
	if conns[0].Type != "contains" {
		t.Errorf("expected type contains, got %q", conns[0].Type)
	}
	if conns[0].Direction != "forward" {
		t.Errorf("expected forward direction, got %q", conns[0].Direction)
	}
	// Source contains target: the VNet contains the subnet.
	if conns[0].Source != "azure::/sub/labvnet1" {
		t.Errorf("expected source to be vnet, got %q", conns[0].Source)
	}
	if conns[0].Target != "azure::/sub/labvnet1/subnets/labsubnet1" {
		t.Errorf("expected target to be subnet, got %q", conns[0].Target)
	}
}

func TestTransformNICToSubnetConnections(t *testing.T) {
	nics := []NetworkInterface{
		{
			ID:   "/sub/labnic1",
			Name: "labnic1",
			IPConfigurations: []IPConfiguration{
				{Subnet: &azSubnetRef{ID: "/sub/labsubnet1"}, PrivateIPAddress: "192.0.2.132"},
				{Subnet: &azSubnetRef{ID: "/sub/labsubnet1"}, PrivateIPAddress: "192.0.2.133"}, // same subnet should deduplicate.
			},
		},
	}
	nicIDMap := map[string]string{"/sub/labnic1": "azure::/sub/labnic1"}
	subnetIDMap := map[string]string{"/sub/labsubnet1": "azure::/sub/labsubnet1"}

	conns := TransformNICToSubnetConnections(nics, nicIDMap, subnetIDMap)

	if len(conns) != 1 {
		t.Fatalf("expected 1 connection (deduplicated), got %d", len(conns))
	}
	if conns[0].Type != "network" {
		t.Errorf("expected type network, got %q", conns[0].Type)
	}
	if conns[0].Properties["private_ip"] != "192.0.2.132" {
		t.Errorf("expected private_ip 192.0.2.132, got %v", conns[0].Properties["private_ip"])
	}
}

func TestTransformPrivateEndpointToSubnetConnections(t *testing.T) {
	pes := []PrivateEndpoint{
		{ID: "/sub/labpe1", Name: "labpe1", Subnet: &azSubnetRef{ID: "/sub/labsubnet1"}},
		{ID: "/sub/pe2", Name: "pe2"}, // no subnet skipped.
	}
	subnetIDMap := map[string]string{"/sub/labsubnet1": "azure::/sub/labsubnet1"}

	conns := TransformPrivateEndpointToSubnetConnections(pes, subnetIDMap)

	if len(conns) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(conns))
	}
}

func TestTransformVNetGatewayToSubnetConnections(t *testing.T) {
	gws := []VNetGateway{
		{
			ID:   "/sub/labgw1",
			Name: "labgw1",
			IPConfigurations: []GatewayIPConfig{
				{Subnet: &azSubnetRef{ID: "/sub/labvnet1/subnets/GatewaySubnet"}, PublicIPAddress: &azPublicIPRef{ID: "/sub/pip1"}},
			},
		},
	}
	subnetIDMap := map[string]string{"/sub/labvnet1/subnets/GatewaySubnet": "azure::/sub/labvnet1/subnets/GatewaySubnet"}

	conns := TransformVNetGatewayToSubnetConnections(gws, subnetIDMap)

	if len(conns) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(conns))
	}
	if conns[0].Source != "azure::/sub/labgw1" {
		t.Errorf("expected source to be gateway, got %q", conns[0].Source)
	}
	if conns[0].Target != "azure::/sub/labvnet1/subnets/GatewaySubnet" {
		t.Errorf("expected target to be GatewaySubnet, got %q", conns[0].Target)
	}
}

func TestTransformPrivateDNSToVNetConnections(t *testing.T) {
	zones := []PrivateDNSZone{
		{
			ID:   "/sub/pdns1",
			Name: "privatelink.blob.core.windows.net",
			Links: []PrivateDNSLink{
				{Name: "vnetlink1", VirtualNetwork: &azVNetRef{ID: "/sub/labvnet1"}, RegistrationEnabled: true},
				{Name: "vnetlink2", VirtualNetwork: &azVNetRef{ID: "/sub/vnet-unknown"}}, // not in map.
			},
		},
	}
	vnetIDMap := map[string]string{"/sub/labvnet1": "azure::/sub/labvnet1"}

	conns, stubs := TransformPrivateDNSToVNetConnections(zones, vnetIDMap)

	if len(conns) != 2 {
		t.Fatalf("expected 2 connections (known + cross-sub stub), got %d", len(conns))
	}
	if conns[0].Type != "network" {
		t.Errorf("expected type network, got %q", conns[0].Type)
	}
	if conns[0].Properties["registration_enabled"] != true {
		t.Errorf("expected registration_enabled=true")
	}
	if len(stubs) != 1 {
		t.Errorf("expected 1 cross-subscription stub, got %d", len(stubs))
	}
	if stubs[0].Properties["cross_subscription"] != true {
		t.Errorf("expected cross_subscription=true on stub")
	}
	// The owner is never repeated in properties (this placeholder ID
	// has no subscription segment, so provider.account is right empty).
	if _, dup := stubs[0].Properties["subscription_id"]; dup {
		t.Errorf("stub properties %v repeat the owner", stubs[0].Properties)
	}
}

// TestPeeringStubOwner: a remote VNet in another subscription becomes a
// stub whose owner is in provider.account (canonical lower case), not
// in a duplicated property.
func TestPeeringStubOwner(t *testing.T) {
	local := "/subscriptions/00000000-0000-4000-8000-000000000001/resourceGroups/RG-LAB/providers/Microsoft.Network/virtualNetworks/LAB-VNET01"
	remote := "/subscriptions/00000000-0000-4000-8000-00000000AB02/resourceGroups/RG-LAB-DR/providers/Microsoft.Network/virtualNetworks/LAB-VNET02"
	peerings := decode[[]VNetPeering](t, `[{"id":"`+local+`/virtualNetworkPeerings/LAB-P1","name":"LAB-P1","peeringState":"Connected","remoteVirtualNetwork":{"id":"`+remote+`"}}]`)
	_, stubs := TransformVNetPeerings(peerings, idMapOf(local))
	if len(stubs) != 1 {
		t.Fatalf("expected 1 stub, got %d", len(stubs))
	}
	if stubs[0].Provider.Account != "00000000-0000-4000-8000-00000000ab02" {
		t.Errorf("stub account = %q", stubs[0].Provider.Account)
	}
	if _, dup := stubs[0].Properties["subscription_id"]; dup {
		t.Errorf("stub repeats the owner in properties: %v", stubs[0].Properties)
	}
}

func TestExtractLastSegment(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Network/virtualNetworks/labvnet1", "labvnet1"},
		{"simple", "simple"},
		{"/a/b/c", "c"},
	}
	for _, tt := range tests {
		got := extractLastSegment(tt.input)
		if got != tt.expected {
			t.Errorf("extractLastSegment(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestWireResourcesToResourceGroups(t *testing.T) {
	rgs := []ResourceGroup{
		{ID: "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01", Name: "rg-lab-01", Location: "westeurope"},
	}
	groups, nameToID := TransformResourceGroupGroups(rgs, testSub)

	vnets := []VirtualNetwork{
		{
			ID:            "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Network/virtualNetworks/labvnet1",
			Name:          "labvnet1",
			Location:      "westeurope",
			ResourceGroup: "rg-lab-01",
		},
	}
	resources := TransformVNets(vnets, nil, testSub)

	WireResourcesToResourceGroups(resources, "00000000-0000-4000-8000-000000001111", nameToID, groups)

	if len(groups[0].Members) != 1 {
		t.Fatalf("expected 1 member in rg-lab-01 group, got %d", len(groups[0].Members))
	}
	if groups[0].Members[0] != resources[0].ID {
		t.Errorf("expected member %q, got %q", resources[0].ID, groups[0].Members[0])
	}
}

func TestWireResourceGroupsToSubscription(t *testing.T) {
	rgs := []ResourceGroup{
		{ID: "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01", Name: "rg-lab-01", Location: "westeurope"},
		{ID: "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-02", Name: "rg-lab-02", Location: "eastus"},
	}
	groups, _ := TransformResourceGroupGroups(rgs, testSub)
	subGroup := TransformSubscriptionGroup(testSub)

	WireResourceGroupsToSubscription(&subGroup, groups)

	if len(subGroup.Children) != 2 {
		t.Fatalf("expected 2 children in subscription group, got %d", len(subGroup.Children))
	}
}

func TestTransformAppServicePlans(t *testing.T) {
	plans := []AppServicePlan{
		{
			ID:            "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Web/serverfarms/labplan1",
			Name:          "labplan1",
			Location:      "westeurope",
			ResourceGroup: "rg-lab-01",
			Kind:          "linux",
			SKU:           azAppServicePlanSKU{Name: "P1v3", Tier: "PremiumV3", Size: "P1v3", Family: "Pv3", Capacity: 2},
			Reserved:      true,
			ZoneRedundant: true,
			NumberOfSites: 3,
			Status:        "Ready",
		},
	}

	resources, idMap := TransformAppServicePlans(plans, testSub)

	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	r := resources[0]
	if r.Type != "osiris.azure.appserviceplan" {
		t.Errorf("expected type osiris.azure.appserviceplan, got %q", r.Type)
	}
	if r.Status != "active" {
		t.Errorf("expected status active for Ready plan, got %q", r.Status)
	}
	if r.Properties["sku"] != "P1v3" {
		t.Errorf("expected sku P1v3, got %v", r.Properties["sku"])
	}
	if r.Properties["linux"] != true {
		t.Errorf("expected linux=true when Reserved is true, got %v", r.Properties["linux"])
	}
	if r.Properties["zone_redundant"] != true {
		t.Errorf("expected zone_redundant=true, got %v", r.Properties["zone_redundant"])
	}
	if idMap[armKey(plans[0].ID)] != r.ID {
		t.Errorf("idMap mismatch: got %q, want %q", idMap[armKey(plans[0].ID)], r.ID)
	}
}

func TestTransformWebApps(t *testing.T) {
	apps := []WebApp{
		{
			ID:                  "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Web/sites/labsite1",
			Name:                "labsite1",
			Location:            "westeurope",
			ResourceGroup:       "rg-lab-01",
			Kind:                "app,linux,container",
			State:               "Running",
			Enabled:             true,
			DefaultHostName:     "labsite1.azurewebsites.net",
			HostNames:           []string{"labsite1.azurewebsites.net"},
			HTTPSOnly:           true,
			ServerFarmID:        "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Web/serverfarms/labplan1",
			OutboundIPAddresses: "203.0.113.1,203.0.113.2,203.0.113.3",
			Identity: &azSiteIdentity{
				Type:        "SystemAssigned",
				PrincipalID: "00000000-0000-4000-8000-000000000001",
			},
			OutboundVnetRouting: &azOutboundVnetRouting{AllTraffic: true},
			Tags: map[string]string{
				"hidden-link: /app-insights-resource-id": "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Insights/components/labai1",
			},
		},
		{
			ID:            "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Web/sites/labfn1",
			Name:          "labfn1",
			Location:      "westeurope",
			ResourceGroup: "rg-lab-01",
			Kind:          "functionapp,linux",
			State:         "Stopped",
			Enabled:       false,
		},
	}

	resources, idMap := TransformWebApps(apps, testSub)

	if len(resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(resources))
	}
	if resources[0].Type != "application.service" {
		t.Errorf("expected first site application.service, got %q", resources[0].Type)
	}
	if resources[0].Status != "active" {
		t.Errorf("expected first site status active, got %q", resources[0].Status)
	}
	if ips, _ := resources[0].Properties["public_ip"].([]string); len(ips) != 3 {
		t.Errorf("expected 3 outbound ips, got %v", resources[0].Properties["public_ip"])
	}
	ext, _ := resources[0].Extensions[extensionNamespace].(map[string]any)
	if ext == nil {
		t.Fatalf("expected extensions, got nil")
	}
	if ext["app_insights_id"] == "" {
		t.Errorf("expected app_insights_id from hidden-link tag, got %v", ext["app_insights_id"])
	}
	if ident, ok := ext["identity"].(map[string]any); !ok || ident["type"] != "SystemAssigned" {
		t.Errorf("expected identity.type SystemAssigned, got %v", ext["identity"])
	}

	if resources[1].Type != "compute.function.serverless" {
		t.Errorf("expected second site compute.function.serverless (kind routing), got %q", resources[1].Type)
	}
	if resources[1].Status != "inactive" {
		t.Errorf("expected function app inactive (disabled), got %q", resources[1].Status)
	}

	if len(idMap) != 2 {
		t.Errorf("expected 2 entries in idMap, got %d", len(idMap))
	}
}

func TestTransformApplicationSecurityGroups(t *testing.T) {
	asgs := []ApplicationSecurityGroup{
		{
			ID:            "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Network/applicationSecurityGroups/labasg1",
			Name:          "labasg1",
			Location:      "westeurope",
			ResourceGroup: "rg-lab-01",
		},
	}

	resources, idMap := TransformApplicationSecurityGroups(asgs, testSub)

	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	r := resources[0]
	if r.Type != "osiris.azure.asg" {
		t.Errorf("expected type osiris.azure.asg, got %q", r.Type)
	}
	if r.Provider.Type != "Microsoft.Network/applicationSecurityGroups" {
		t.Errorf("expected provider type Microsoft.Network/applicationSecurityGroups, got %q", r.Provider.Type)
	}
	if idMap[armKey(asgs[0].ID)] != r.ID {
		t.Errorf("idMap mismatch: got %q, want %q", idMap[armKey(asgs[0].ID)], r.ID)
	}
}

func TestTransformWebAppToPlanConnections(t *testing.T) {
	planID := "/sub/labplan1"
	planID2 := "/sub/plan2"
	siteID := "/sub/labsite1"
	siteID2 := "/sub/labsite2"
	apps := []WebApp{
		{ID: siteID, Name: "labsite1", ServerFarmID: planID},
		// az webapp list flattens serverFarmId -> appServicePlanId; accept either.
		{ID: siteID2, Name: "labsite2", AppServicePlanID: planID2},
		{ID: "/sub/orphan", Name: "orphan"}, // neither set -> skipped.
	}
	webAppIDMap := map[string]string{
		siteID:        "azure::" + siteID,
		siteID2:       "azure::" + siteID2,
		"/sub/orphan": "azure::/sub/orphan",
	}
	planIDMap := map[string]string{
		planID:  "azure::" + planID,
		planID2: "azure::" + planID2,
	}

	conns := TransformWebAppToPlanConnections(apps, webAppIDMap, planIDMap)
	if len(conns) != 2 {
		t.Fatalf("expected 2 connections (one per plan field variant), got %d", len(conns))
	}
	for _, c := range conns {
		if c.Type != "contains" {
			t.Errorf("expected type contains, got %q", c.Type)
		}
	}
}

func TestTransformWebAppToSubnetConnections(t *testing.T) {
	apps := []WebApp{
		{ID: "/sub/labsite1", Name: "labsite1", VirtualNetworkSubnetID: "/sub/labsubnet1"},
		{ID: "/sub/nosubnet", Name: "nosubnet"},
	}
	webAppIDMap := map[string]string{"/sub/labsite1": "azure::/sub/labsite1", "/sub/nosubnet": "azure::/sub/nosubnet"}
	subnetIDMap := map[string]string{"/sub/labsubnet1": "azure::/sub/labsubnet1"}

	conns := TransformWebAppToSubnetConnections(apps, webAppIDMap, subnetIDMap)
	if len(conns) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(conns))
	}
	if conns[0].Type != "network" {
		t.Errorf("expected type network, got %q", conns[0].Type)
	}
}

func TestTransformNICToASGConnections(t *testing.T) {
	nics := []NetworkInterface{
		{
			ID:   "/sub/labnic1",
			Name: "labnic1",
			IPConfigurations: []IPConfiguration{
				{
					ApplicationSecurityGroups: []azASGRef{
						{ID: "/sub/labasg1"},
						{ID: "/sub/labasg1"}, // duplicate ASG on same NIC -> dedup.
					},
				},
			},
		},
	}
	nicIDMap := map[string]string{"/sub/labnic1": "azure::/sub/labnic1"}
	asgIDMap := map[string]string{"/sub/labasg1": "azure::/sub/labasg1"}

	conns := TransformNICToASGConnections(nics, nicIDMap, asgIDMap)
	if len(conns) != 1 {
		t.Fatalf("expected 1 deduplicated connection, got %d", len(conns))
	}
	if conns[0].Type != connASGAssociation {
		t.Errorf("expected type %s, got %q", connASGAssociation, conns[0].Type)
	}
}

func TestMapAppServicePlanStatus(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"Ready", "active"},
		{"Pending", "degraded"},
		{"Creating", "degraded"},
		{"", ""},
		{"Mystery", "unknown"},
	}
	for _, tt := range tests {
		if got := mapAppServicePlanStatus(tt.in); got != tt.want {
			t.Errorf("mapAppServicePlanStatus(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestMapWebAppState(t *testing.T) {
	tests := []struct {
		state   string
		enabled bool
		want    string
	}{
		{"Running", true, "active"},
		{"Running", false, "inactive"},
		{"Stopped", true, "inactive"},
		{"Stopped", false, "inactive"},
		{"Unknown", true, "unknown"},
	}
	for _, tt := range tests {
		if got := mapWebAppState(tt.state, tt.enabled); got != tt.want {
			t.Errorf("mapWebAppState(%q, %v) = %q, want %q", tt.state, tt.enabled, got, tt.want)
		}
	}
}

func TestSplitCSV(t *testing.T) {
	if got := splitCSV(""); got != nil {
		t.Errorf("expected nil for empty string, got %v", got)
	}
	got := splitCSV("a, b ,,c")
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("expected %d elements, got %d (%v)", len(want), len(got), got)
	}
	for i, v := range want {
		if got[i] != v {
			t.Errorf("splitCSV[%d] = %q, want %q", i, got[i], v)
		}
	}
}

func TestAppInsightsFromTags(t *testing.T) {
	tags := map[string]string{
		"hidden-link: /app-insights-resource-id": "/subs/xxx/labai1",
		"other":                                  "value",
	}
	if got := appInsightsFromTags(tags); got != "/subs/xxx/labai1" {
		t.Errorf("appInsightsFromTags = %q", got)
	}
	if got := appInsightsFromTags(map[string]string{"foo": "bar"}); got != "" {
		t.Errorf("expected empty string when tag missing, got %q", got)
	}
}

func TestTransformStorageAccounts(t *testing.T) {
	yes := true
	accts := []StorageAccount{
		{
			ID:                     "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Storage/storageAccounts/labstg1",
			Name:                   "labstg1",
			Location:               "westeurope",
			ResourceGroup:          "rg-lab-01",
			Kind:                   "StorageV2",
			SKU:                    azStorageSKU{Name: "Standard_LRS", Tier: "Standard"},
			AccessTier:             "Hot",
			EnableHTTPSTrafficOnly: true,
			MinimumTLSVersion:      "TLS1_2",
			PublicNetworkAccess:    "Disabled",
			IsHnsEnabled:           true,
			ProvisioningState:      "Succeeded",
			AllowBlobPublicAccess:  &yes,
			PrimaryEndpoints: &azStorageEndpoints{
				Blob: "https://labstg1.blob.core.windows.net/",
				File: "https://labstg1.file.core.windows.net/",
			},
			NetworkRuleSet: &azStorageNetworkRuleSet{
				DefaultAction: "Deny",
				Bypass:        "AzureServices",
				IPRules:       []azStorageIPRule{{Value: "203.0.113.0/24", Action: "Allow"}},
			},
			Encryption: &azStorageEncryption{KeySource: "Microsoft.Storage"},
		},
	}

	resources, idMap := TransformStorageAccounts(accts, testSub)

	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	r := resources[0]
	if r.Type != "osiris.azure.storage" {
		t.Errorf("expected type osiris.azure.storage, got %q", r.Type)
	}
	if r.Status != "active" {
		t.Errorf("expected status active for Succeeded, got %q", r.Status)
	}
	if r.Properties["kind"] != "StorageV2" {
		t.Errorf("kind missing")
	}
	if r.Properties["hierarchical_namespace"] != true {
		t.Errorf("expected HNS=true, got %v", r.Properties["hierarchical_namespace"])
	}
	ext, _ := r.Extensions[extensionNamespace].(map[string]any)
	if ext == nil {
		t.Fatalf("expected extensions")
	}
	if _, ok := ext["endpoints"].(map[string]any); !ok {
		t.Errorf("expected endpoints extension")
	}
	if acls, ok := ext["network_acls"].(map[string]any); !ok || acls["default_action"] != "Deny" {
		t.Errorf("expected network_acls.default_action Deny, got %v", ext["network_acls"])
	}
	if idMap[armKey(accts[0].ID)] != r.ID {
		t.Errorf("idMap mismatch")
	}
}

func TestTransformKeyVaults(t *testing.T) {
	purge := true
	softDel := true
	vaults := []KeyVault{
		{
			ID:            "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.KeyVault/vaults/labkv1",
			Name:          "labkv1",
			Location:      "westeurope",
			ResourceGroup: "rg-lab-01",
			Properties: &KeyVaultProperties{
				SKU:                       azKeyVaultSKU{Family: "A", Name: "standard"},
				TenantID:                  "00000000-0000-4000-8000-00000000f1e2",
				VaultURI:                  "https://labkv1.vault.azure.net/",
				EnableRbacAuthorization:   true,
				EnableSoftDelete:          &softDel,
				SoftDeleteRetentionInDays: 90,
				EnablePurgeProtection:     &purge,
				PublicNetworkAccess:       "Disabled",
				ProvisioningState:         "Succeeded",
				NetworkACLs: &azKeyVaultNetworkACLs{
					Bypass:              "AzureServices",
					DefaultAction:       "Deny",
					IPRules:             []azKeyVaultIPRule{{Value: "203.0.113.0/24"}},
					VirtualNetworkRules: []azKeyVaultVNetRule{{ID: "/subs/s/vnet/labsubnet1"}},
				},
			},
		},
	}

	resources, _ := TransformKeyVaults(vaults, testSub)
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	r := resources[0]
	if r.Type != "osiris.azure.keyvault" {
		t.Errorf("expected type osiris.azure.keyvault, got %q", r.Type)
	}
	if r.Properties["rbac_enabled"] != true {
		t.Errorf("expected RBAC, got %v", r.Properties["rbac_enabled"])
	}
	if r.Properties["soft_delete_retention_days"] != 90 {
		t.Errorf("expected soft delete retention 90, got %v", r.Properties["soft_delete_retention_days"])
	}
	if r.Properties["purge_protection_enabled"] != true {
		t.Errorf("expected purge_protection_enabled=true, got %v", r.Properties["purge_protection_enabled"])
	}
	if r.Properties["public_network_access"] != "Disabled" {
		t.Errorf("expected public_network_access=Disabled, got %v", r.Properties["public_network_access"])
	}
	if r.Properties["sku"] != "standard" {
		t.Errorf("expected sku=standard, got %v", r.Properties["sku"])
	}
	acls, ok := r.Properties["network_acls"].(map[string]any)
	if !ok {
		t.Fatalf("expected network_acls at properties level, got %#v", r.Properties["network_acls"])
	}
	if acls["default_action"] != "Deny" {
		t.Errorf("network_acls default_action = %v", acls["default_action"])
	}
	ext, _ := r.Extensions[extensionNamespace].(map[string]any)
	if ext == nil || ext["tenant_id"] == "" {
		t.Errorf("expected tenant_id extension")
	}
	// network_acls is emitted once, in properties; no equivalent
	// extensions copy.
	if _, dup := ext["network_acls"]; dup {
		t.Error("network_acls must not be duplicated in extensions")
	}
}

func TestTransformContainerRegistries(t *testing.T) {
	regs := []ContainerRegistry{
		{
			ID:                   "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.ContainerRegistry/registries/labacr1",
			Name:                 "labacr1",
			Location:             "westeurope",
			ResourceGroup:        "rg-lab-01",
			SKU:                  azACRSKU{Name: "Premium", Tier: "Premium"},
			LoginServer:          "labacr1.azurecr.io",
			AdminUserEnabled:     false,
			AnonymousPullEnabled: false,
			PublicNetworkAccess:  "Disabled",
			ZoneRedundancy:       "Enabled",
			ProvisioningState:    "Succeeded",
		},
	}

	resources, _ := TransformContainerRegistries(regs, testSub)
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	r := resources[0]
	if r.Type != "osiris.azure.containerregistry" {
		t.Errorf("expected type osiris.azure.containerregistry, got %q", r.Type)
	}
	if r.Properties["login_server"] != "labacr1.azurecr.io" {
		t.Errorf("expected login_server")
	}
	if r.Properties["zone_redundant"] != true {
		t.Errorf("expected zone_redundant=true when ZoneRedundancy=Enabled")
	}
}

func TestTransformManagedIdentities(t *testing.T) {
	ids := []ManagedIdentity{
		{
			ID:            "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.ManagedIdentity/userAssignedIdentities/labmi1",
			Name:          "labmi1",
			Location:      "westeurope",
			ResourceGroup: "rg-lab-01",
			PrincipalID:   "00000000-0000-4000-8000-000000000001",
			ClientID:      "00000000-0000-4000-8000-000000000002",
			TenantID:      "00000000-0000-4000-8000-00000000f1e2",
		},
	}

	resources, idMap := TransformManagedIdentities(ids, testSub)
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	r := resources[0]
	if r.Type != "osiris.azure.managedidentity" {
		t.Errorf("expected type osiris.azure.managedidentity, got %q", r.Type)
	}
	if r.Properties["principal_id"] != "00000000-0000-4000-8000-000000000001" {
		t.Errorf("expected principal_id in properties, got %v", r.Properties["principal_id"])
	}
	if r.Properties["client_id"] != "00000000-0000-4000-8000-000000000002" {
		t.Errorf("expected client_id in properties, got %v", r.Properties["client_id"])
	}
	if idMap[armKey(ids[0].ID)] != r.ID {
		t.Errorf("idMap mismatch")
	}
}

func TestTransformDisks(t *testing.T) {
	disks := []Disk{
		{
			ID:                "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Compute/disks/labdisk1",
			Name:              "labdisk1",
			Location:          "westeurope",
			ResourceGroup:     "rg-lab-01",
			SKU:               azDiskSKU{Name: "Premium_LRS", Tier: "Premium"},
			DiskSizeGB:        128,
			DiskIOPSReadWrite: 500,
			DiskMBPSReadWrite: 100,
			DiskState:         "Attached",
			OSType:            "Linux",
			ManagedBy:         "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Compute/virtualMachines/labvm1",
			ProvisioningState: "Succeeded",
			Zones:             []string{"1"},
		},
		{
			ID:                "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Compute/disks/labdisk2",
			Name:              "labdisk2",
			Location:          "westeurope",
			ResourceGroup:     "rg-lab-01",
			DiskState:         "Unattached",
			ProvisioningState: "Succeeded",
		},
	}

	resources, idMap := TransformDisks(disks, testSub)
	if len(resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(resources))
	}
	if resources[0].Status != "active" {
		t.Errorf("expected attached disk status active, got %q", resources[0].Status)
	}
	if resources[1].Status != "inactive" {
		t.Errorf("expected unattached disk status inactive, got %q", resources[1].Status)
	}
	if len(idMap) != 2 {
		t.Errorf("expected 2 idMap entries")
	}
}

func TestTransformSnapshots(t *testing.T) {
	snaps := []Snapshot{
		{
			ID:            "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Compute/snapshots/labsnap1",
			Name:          "labsnap1",
			Location:      "westeurope",
			ResourceGroup: "rg-lab-01",
			SKU:           azDiskSKU{Name: "Standard_LRS"},
			DiskSizeGB:    128,
			Incremental:   true,
			OSType:        "Linux",
			CreationData: &azDiskCreationData{
				CreateOption:     "Copy",
				SourceResourceID: "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Compute/disks/labdisk1",
			},
			ProvisioningState: "Succeeded",
		},
	}

	resources, _ := TransformSnapshots(snaps, testSub)
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	r := resources[0]
	if r.Type != "compute.vm.snapshot" {
		t.Errorf("expected type compute.vm.snapshot, got %q", r.Type)
	}
	if r.Properties["incremental"] != true {
		t.Errorf("expected incremental=true")
	}
	if r.Properties["source_resource_id"] == "" {
		t.Errorf("expected source_resource_id set")
	}
}

func TestTransformDiskToSnapshotConnections(t *testing.T) {
	snaps := []Snapshot{
		{
			ID:           "/sub/labsnap1",
			Name:         "labsnap1",
			CreationData: &azDiskCreationData{SourceResourceID: "/sub/labdisk1"},
		},
		{
			ID: "/sub/snap2", Name: "snap2", // no creation data -> skipped.
		},
	}
	snapshotIDMap := map[string]string{"/sub/labsnap1": "azure::/sub/labsnap1", "/sub/snap2": "azure::/sub/snap2"}
	diskIDMap := map[string]string{"/sub/labdisk1": "azure::/sub/labdisk1"}

	conns := TransformDiskToSnapshotConnections(snaps, snapshotIDMap, diskIDMap)
	if len(conns) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(conns))
	}
	c := conns[0]
	if c.Type != connDataflow || c.Direction != "forward" || c.Source != "azure::/sub/labdisk1" || c.Target != "azure::/sub/labsnap1" {
		t.Errorf("want dataflow labdisk1 -> labsnap1 forward, got %s %s -> %s (%s)", c.Type, c.Source, c.Target, c.Direction)
	}
}

// TestStatusMappersUseSchemaEnum checks that every status mapper
// returns a value of the schema status enum, and nothing when Azure
// reports no state.
func TestStatusMappersUseSchemaEnum(t *testing.T) {
	enum := map[string]bool{"": true, "active": true, "inactive": true, "degraded": true, "retired": true, "unknown": true}
	mappers := map[string]func(string) string{
		"provisioning": mapProvisioningState, "sqlserver": mapSQLServerState, "sqldb": mapSQLDatabaseStatus,
		"flexserver": mapFlexServerState, "sqlmi": mapSQLMIState, "streamanalytics": mapStreamAnalyticsState,
		"logic": mapLogicWorkflowState, "automation": mapAutomationState, "arc": mapArcMachineStatus,
		"peering": mapPeeringState, "plan": mapAppServicePlanStatus, "privatelink": privateLinkStatus,
		"vmpower": mapVMPowerState,
	}
	inputs := []string{"", "PowerState/running", "PowerState/deallocated", "PowerState/starting", "Succeeded", "Ready", "Online", "Paused", "Scaling", "Starting", "Stopped",
		"Deleted", "Completed", "Suspended", "Connected", "Disconnected", "Mystery", "Enabled", "Disabled"}
	for name, m := range mappers {
		for _, in := range inputs {
			if got := m(in); !enum[got] {
				t.Errorf("%s(%q) = %q, not a schema status value", name, in, got)
			}
		}
		if got := m(""); got != "" {
			t.Errorf("%s(\"\") = %q, want status omitted", name, got)
		}
	}
}

func TestMapProvisioningState(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"Succeeded", "active"},
		{"", ""},
		{"Updating", "degraded"},
		{"Failed", "inactive"},
		{"Mystery", "unknown"},
	}
	for _, tt := range tests {
		if got := mapProvisioningState(tt.in); got != tt.want {
			t.Errorf("mapProvisioningState(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestMapDiskState(t *testing.T) {
	tests := []struct {
		diskState, provState, want string
	}{
		{"Attached", "Succeeded", "active"},
		{"Unattached", "Succeeded", "inactive"},
		{"ActiveSAS", "Succeeded", "active"},
		{"", "Failed", "inactive"}, // falls through to provisioning state.
		{"", "Succeeded", "active"},
	}
	for _, tt := range tests {
		if got := mapDiskState(tt.diskState, tt.provState); got != tt.want {
			t.Errorf("mapDiskState(%q,%q) = %q, want %q", tt.diskState, tt.provState, got, tt.want)
		}
	}
}

func TestTransformApplicationInsights(t *testing.T) {
	wsArm := "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.OperationalInsights/workspaces/labws1"
	comps := []ApplicationInsights{
		{
			ID:            "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/microsoft.insights/components/labapp1",
			Name:          "labapp1",
			Location:      "westeurope",
			ResourceGroup: "rg-lab-01",
			Kind:          "web",
			Properties: &AppInsightsProperties{
				ApplicationType:                 "web",
				WorkspaceResourceID:             wsArm,
				RetentionInDays:                 90,
				SamplingPercentage:              100,
				PublicNetworkAccessForIngestion: "Enabled",
				PublicNetworkAccessForQuery:     "Enabled",
				DisableLocalAuth:                true,
				ProvisioningState:               "Succeeded",
				IngestionMode:                   "LogAnalytics",
			},
		},
		{
			ID:            "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/microsoft.insights/components/labclassic1",
			Name:          "labclassic1",
			Location:      "westeurope",
			ResourceGroup: "rg-lab-01",
			Properties:    nil,
		},
	}

	resources, idMap := TransformApplicationInsights(comps, testSub)
	if len(resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(resources))
	}
	r := resources[0]
	if r.Type != "osiris.azure.applicationinsights" {
		t.Errorf("expected type osiris.azure.applicationinsights, got %q", r.Type)
	}
	if r.Status != "active" {
		t.Errorf("expected status active, got %q", r.Status)
	}
	if r.Properties["application_type"] != "web" {
		t.Errorf("expected application_type=web, got %v", r.Properties["application_type"])
	}
	if r.Properties["ingestion_mode"] != "LogAnalytics" {
		t.Errorf("expected ingestion_mode=LogAnalytics, got %v", r.Properties["ingestion_mode"])
	}
	if r.Properties["entra_only"] != true {
		t.Errorf("expected entra_only=true")
	}
	ext, _ := r.Extensions[extensionNamespace].(map[string]any)
	if ext == nil || ext["workspace_resource_id"] != wsArm {
		t.Errorf("expected workspace_resource_id extension = %q, got %v", wsArm, ext)
	}
	// Verify secret fields NEVER appear in emitted properties/extensions aligned to OSIRIS JSON spec chapter 13.
	for _, forbidden := range []string{"instrumentation_key", "connection_string", "app_id"} {
		if _, has := r.Properties[forbidden]; has {
			t.Errorf("property %q must not be emitted (auth material)", forbidden)
		}
		if ext != nil {
			if _, has := ext[forbidden]; has {
				t.Errorf("extension %q must not be emitted (auth material)", forbidden)
			}
		}
	}
	// Classic App Insights (no Properties): no invented status and
	// no workspace extension.
	rc := resources[1]
	if rc.Status != "" || rc.State != "" {
		t.Errorf("classic AI without properties must not get a status, got %q/%q", rc.Status, rc.State)
	}
	if rcExt, _ := rc.Extensions[extensionNamespace].(map[string]any); rcExt != nil {
		if _, has := rcExt["workspace_resource_id"]; has {
			t.Errorf("classic AI should have no workspace extension")
		}
	}
	if len(idMap) != 2 {
		t.Errorf("expected 2 idMap entries, got %d", len(idMap))
	}
}

func TestTransformLogAnalyticsWorkspaces(t *testing.T) {
	wss := []LogAnalyticsWorkspace{
		{
			ID:                              "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.OperationalInsights/workspaces/labws1",
			Name:                            "labws1",
			Location:                        "westeurope",
			ResourceGroup:                   "rg-lab-01",
			CustomerID:                      "00000000-0000-4000-8000-000000005555",
			ProvisioningState:               "Succeeded",
			SKU:                             &azLAWorkspaceSKU{Name: "PerGB2018"},
			RetentionInDays:                 30,
			PublicNetworkAccessForIngestion: "Enabled",
			PublicNetworkAccessForQuery:     "Enabled",
			ForceCmkForQuery:                true,
			WorkspaceCapping:                &azLAWorkspaceCapping{DailyQuotaGB: 10.5},
		},
	}

	resources, idMap := TransformLogAnalyticsWorkspaces(wss, testSub)
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	r := resources[0]
	if r.Type != "osiris.azure.loganalytics" {
		t.Errorf("expected type osiris.azure.loganalytics, got %q", r.Type)
	}
	if r.Status != "active" {
		t.Errorf("expected status active, got %q", r.Status)
	}
	if r.Properties["sku"] != "PerGB2018" {
		t.Errorf("expected sku=PerGB2018, got %v", r.Properties["sku"])
	}
	if r.Properties["retention_in_days"] != 30 {
		t.Errorf("expected retention_in_days=30, got %v", r.Properties["retention_in_days"])
	}
	if r.Properties["force_cmk_for_query"] != true {
		t.Errorf("expected force_cmk_for_query=true")
	}
	if r.Properties["daily_quota_gb"] != 10.5 {
		t.Errorf("expected daily_quota_gb=10.5, got %v", r.Properties["daily_quota_gb"])
	}
	ext, _ := r.Extensions[extensionNamespace].(map[string]any)
	if ext == nil || ext["customer_id"] != "00000000-0000-4000-8000-000000005555" {
		t.Errorf("expected customer_id extension")
	}
	// Secret fields (shared keys) must never be emitted.
	for _, forbidden := range []string{"primary_shared_key", "secondary_shared_key", "shared_key"} {
		if _, has := r.Properties[forbidden]; has {
			t.Errorf("property %q must not be emitted (auth material)", forbidden)
		}
		if ext != nil {
			if _, has := ext[forbidden]; has {
				t.Errorf("extension %q must not be emitted (auth material)", forbidden)
			}
		}
	}
	if idMap[armKey(wss[0].ID)] != r.ID {
		t.Errorf("idMap mismatch")
	}
}

func TestTransformAppInsightsToWorkspaceConnections(t *testing.T) {
	aiArm := "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/microsoft.insights/components/labapp1"
	wsArm := "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.OperationalInsights/workspaces/labws1"
	comps := []ApplicationInsights{
		{
			ID:         aiArm,
			Name:       "labapp1",
			Properties: &AppInsightsProperties{WorkspaceResourceID: wsArm},
		},
		{
			// Classic AI with no workspace binding should not emit an edge.
			ID:         "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/microsoft.insights/components/labclassic1",
			Name:       "labclassic1",
			Properties: nil,
		},
	}
	aiIDMap := map[string]string{aiArm: "ai-resource-id"}
	laIDMap := map[string]string{wsArm: "la-resource-id"}

	conns := TransformAppInsightsToWorkspaceConnections(comps, aiIDMap, laIDMap)
	if len(conns) != 1 {
		t.Fatalf("expected 1 connection (classic AI should be skipped), got %d", len(conns))
	}
	c := conns[0]
	if c.Type != connDataflow {
		t.Errorf("expected type dataflow, got %q", c.Type)
	}
	if c.Source != "ai-resource-id" || c.Target != "la-resource-id" {
		t.Errorf("source/target mismatch: %+v", c)
	}
}

func TestTransformRecoveryServicesVaults(t *testing.T) {
	peArm := "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Network/privateEndpoints/labpe1"
	vaults := []RecoveryServicesVault{
		{
			ID:            "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.RecoveryServices/vaults/labrsv1",
			Name:          "labrsv1",
			Location:      "westeurope",
			ResourceGroup: "rg-lab-01",
			SKU:           &azRSVaultSKU{Name: "RS0", Tier: "Standard"},
			Properties: &RSVaultProperties{
				ProvisioningState:   "Succeeded",
				PublicNetworkAccess: "Disabled",
				RedundancySettings: &azRSVaultRedundancy{
					StandardTierStorageRedundancy: "GeoRedundant",
					CrossRegionRestore:            "Enabled",
				},
				PrivateEndpointConnections: []azPrivateEndpointConnRef{
					{Properties: struct {
						PrivateEndpoint struct {
							ID string `json:"id"`
						} `json:"privateEndpoint"`
					}{PrivateEndpoint: struct {
						ID string `json:"id"`
					}{ID: peArm}}},
				},
			},
			ProtectedItems: []BackupProtectedItem{
				{Name: "item1"}, {Name: "item2"},
			},
		},
	}

	resources, idMap := TransformRecoveryServicesVaults(vaults, testSub)
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	r := resources[0]
	if r.Type != "osiris.azure.recoveryservicesvault" {
		t.Errorf("expected type osiris.azure.recoveryservicesvault, got %q", r.Type)
	}
	if r.Status != "active" {
		t.Errorf("expected status active, got %q", r.Status)
	}
	if r.Properties["sku"] != "RS0" {
		t.Errorf("expected sku=RS0, got %v", r.Properties["sku"])
	}
	if r.Properties["storage_redundancy"] != "GeoRedundant" {
		t.Errorf("expected storage_redundancy=GeoRedundant")
	}
	if r.Properties["protected_item_count"] != 2 {
		t.Errorf("expected protected_item_count=2, got %v", r.Properties["protected_item_count"])
	}
	ext, _ := r.Extensions[extensionNamespace].(map[string]any)
	peIDs, ok := ext["private_endpoint_ids"].([]string)
	if !ok || len(peIDs) != 1 || peIDs[0] != peArm {
		t.Errorf("expected private_endpoint_ids=[%q], got %v", peArm, ext["private_endpoint_ids"])
	}
	if idMap[armKey(vaults[0].ID)] != r.ID {
		t.Errorf("idMap mismatch")
	}
}

func TestTransformBackupVaults(t *testing.T) {
	vaults := []BackupVault{
		{
			ID:            "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.DataProtection/backupVaults/labbv1",
			Name:          "labbv1",
			Location:      "westeurope",
			ResourceGroup: "rg-lab-01",
			Properties: &BackupVaultProperties{
				ProvisioningState: "Succeeded",
				StorageSettings: []azBackupVaultStorageSetting{
					{DatastoreType: "VaultStore", Type: "GeoRedundant"},
				},
				SecuritySettings: &azBackupVaultSecuritySettings{
					ImmutabilitySettings: &azBackupVaultImmutability{State: "Unlocked"},
					SoftDeleteSettings:   &azBackupVaultSoftDelete{State: "On", RetentionDurationInDays: 14},
				},
			},
			ProtectedInstances: []BackupInstance{{Name: "bi1"}},
		},
	}

	resources, idMap := TransformBackupVaults(vaults, testSub)
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	r := resources[0]
	if r.Type != "osiris.azure.backupvault" {
		t.Errorf("expected type osiris.azure.backupvault, got %q", r.Type)
	}
	if r.Status != "active" {
		t.Errorf("expected status active, got %q", r.Status)
	}
	if r.Properties["immutability"] != "Unlocked" {
		t.Errorf("expected immutability=Unlocked")
	}
	if r.Properties["soft_delete"] != "On" {
		t.Errorf("expected soft_delete=On")
	}
	if r.Properties["soft_delete_retention_days"] != 14.0 {
		t.Errorf("expected soft_delete_retention_days=14.0, got %v", r.Properties["soft_delete_retention_days"])
	}
	if r.Properties["backup_instance_count"] != 1 {
		t.Errorf("expected backup_instance_count=1")
	}
	if idMap[armKey(vaults[0].ID)] != r.ID {
		t.Errorf("idMap mismatch")
	}
}

func TestTransformBackupProtectedItemConnections(t *testing.T) {
	vmArm := "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Compute/virtualMachines/labvm1"
	vaultArm := "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.RecoveryServices/vaults/labrsv1"
	vaults := []RecoveryServicesVault{
		{
			ID:   vaultArm,
			Name: "labrsv1",
			ProtectedItems: []BackupProtectedItem{
				{
					Name: "vm1-item",
					Properties: &azProtectedItemProperties{
						SourceResourceID: vmArm,
						WorkloadType:     "VM",
					},
				},
				{
					// Unknown source resource -> skipped.
					Name: "orphan",
					Properties: &azProtectedItemProperties{
						SourceResourceID: "/subscriptions/00000000-0000-4000-8000-000000000099/resourceGroups/rg-lab-z/providers/Microsoft.Compute/virtualMachines/labghost",
					},
				},
				{
					// Missing source ID -> skipped.
					Name:       "empty",
					Properties: nil,
				},
			},
		},
	}
	rsvIDMap := map[string]string{vaultArm: "rsv-id"}
	resourceIDMap := map[string]string{vmArm: "vm-id"}

	conns := TransformBackupProtectedItemConnections(vaults, rsvIDMap, resourceIDMap)
	if len(conns) != 1 {
		t.Fatalf("expected 1 connection (orphan+empty skipped), got %d", len(conns))
	}
	c := conns[0]
	if c.Source != "vm-id" || c.Target != "rsv-id" {
		t.Errorf("source/target mismatch: %+v", c)
	}
	if c.Type != connDataflow {
		t.Errorf("expected type dataflow, got %q", c.Type)
	}
}

func TestTransformBackupInstanceConnections(t *testing.T) {
	diskArm := "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Compute/disks/labdisk1"
	vaultArm := "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.DataProtection/backupVaults/labbv1"
	vaults := []BackupVault{
		{
			ID:   vaultArm,
			Name: "labbv1",
			ProtectedInstances: []BackupInstance{
				{
					Name: "disk1-instance",
					Properties: &azBackupInstanceProperties{
						DataSourceInfo: &azDataSourceInfo{
							ResourceID:     diskArm,
							DatasourceType: "Microsoft.Compute/disks",
						},
					},
				},
			},
		},
	}
	bvIDMap := map[string]string{vaultArm: "bv-id"}
	resourceIDMap := map[string]string{diskArm: "disk-id"}

	conns := TransformBackupInstanceConnections(vaults, bvIDMap, resourceIDMap)
	if len(conns) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(conns))
	}
	if conns[0].Source != "disk-id" || conns[0].Target != "bv-id" {
		t.Errorf("source/target mismatch: %+v", conns[0])
	}
}

func TestTransformWebAppToAppInsightsConnections(t *testing.T) {
	aiArm := "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/microsoft.insights/components/labapp1"
	webApps := []WebApp{
		{
			ID:   "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Web/sites/labsite1",
			Name: "labsite1",
			Tags: map[string]string{
				"hidden-link: /app-insights-resource-id": aiArm,
			},
		},
		{
			// No hidden-link tag -> no edge.
			ID:   "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Web/sites/labsite2",
			Name: "labsite2",
			Tags: map[string]string{"environment": "np"},
		},
	}
	webAppIDMap := map[string]string{
		webApps[0].ID: "site1-id",
		webApps[1].ID: "site2-id",
	}
	aiIDMap := map[string]string{aiArm: "ai-resource-id"}

	conns := TransformWebAppToAppInsightsConnections(webApps, webAppIDMap, aiIDMap)
	if len(conns) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(conns))
	}
	c := conns[0]
	if c.Source != "site1-id" || c.Target != "ai-resource-id" {
		t.Errorf("source/target mismatch: %+v", c)
	}
}

func TestTransformSQLServers(t *testing.T) {
	peArm := "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Network/privateEndpoints/labpe-sql"
	servers := []SQLServer{
		{
			ID:            "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Sql/servers/labsqlsrv1",
			Name:          "labsqlsrv1",
			Location:      "westeurope",
			ResourceGroup: "rg-lab-01",
			Properties: &azSQLServerProperties{
				Version:                  "12.0",
				AdministratorLogin:       "sqladmin",
				FullyQualifiedDomainName: "labsqlsrv1.database.windows.net",
				State:                    "Ready",
				PublicNetworkAccess:      "Disabled",
				MinimalTLSVersion:        "1.2",
				PrivateEndpointConnections: []azPrivateEndpointConnRef{
					{Properties: struct {
						PrivateEndpoint struct {
							ID string `json:"id"`
						} `json:"privateEndpoint"`
					}{PrivateEndpoint: struct {
						ID string `json:"id"`
					}{ID: peArm}}},
				},
			},
			Databases: []SQLDatabase{
				{Name: "labdb1"},
				{Name: "db2"},
			},
		},
	}

	resources, idMap := TransformSQLServers(servers, testSub)
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	r := resources[0]
	if r.Type != "osiris.azure.sqlserver" {
		t.Errorf("expected type osiris.azure.sqlserver, got %q", r.Type)
	}
	if r.Status != "active" {
		t.Errorf("expected status active, got %q", r.Status)
	}
	if r.Properties["version"] != "12.0" {
		t.Errorf("expected version=12.0")
	}
	if r.Properties["administrator_login"] != "sqladmin" {
		t.Errorf("expected administrator_login=sqladmin")
	}
	if r.Properties["public_network_access"] != "Disabled" {
		t.Errorf("expected public_network_access=Disabled")
	}
	if r.Properties["database_count"] != 2 {
		t.Errorf("expected database_count=2")
	}
	ext, _ := r.Extensions[extensionNamespace].(map[string]any)
	peIDs, ok := ext["private_endpoint_connection_ids"].([]string)
	if !ok || len(peIDs) != 1 || peIDs[0] != peArm {
		t.Errorf("expected private_endpoint_connection_ids=[%q], got %v", peArm, ext["private_endpoint_connection_ids"])
	}
	// Secrets must never appear aligned to OSIRIS JSON spec chapter 13.
	for key := range r.Properties {
		if strings.Contains(strings.ToLower(key), "password") ||
			strings.Contains(strings.ToLower(key), "admin_password") {
			t.Errorf("SQL server properties must not contain password field %q", key)
		}
	}
	if idMap[armKey(servers[0].ID)] != r.ID {
		t.Errorf("idMap mismatch")
	}
}

func TestTransformSQLDatabases(t *testing.T) {
	servers := []SQLServer{
		{
			ID:   "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Sql/servers/labsqlsrv1",
			Name: "labsqlsrv1",
			Databases: []SQLDatabase{
				{
					ID:            "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Sql/servers/labsqlsrv1/databases/labdb1",
					Name:          "labdb1",
					Location:      "westeurope",
					ResourceGroup: "rg-lab-01",
					ServerID:      "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Sql/servers/labsqlsrv1",
					SKU:           &azSQLDatabaseSKU{Name: "GP_S_Gen5_2", Tier: "GeneralPurpose", Capacity: 2, Family: "Gen5"},
					Properties: &azSQLDatabaseProperties{
						Collation:     "SQL_Latin1_General_CP1_CI_AS",
						Status:        "Online",
						MaxSizeBytes:  32212254720,
						ZoneRedundant: true,
					},
				},
			},
		},
	}

	resources, idMap := TransformSQLDatabases(servers, testSub)
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	r := resources[0]
	if r.Type != "application.database" {
		t.Errorf("expected type application.database, got %q", r.Type)
	}
	if r.Status != "active" {
		t.Errorf("expected status active (Online -> active), got %q", r.Status)
	}
	if r.Properties["sku"] != "GP_S_Gen5_2" {
		t.Errorf("expected sku=GP_S_Gen5_2")
	}
	if r.Properties["zone_redundant"] != true {
		t.Errorf("expected zone_redundant=true")
	}
	if r.Properties["server_name"] != "labsqlsrv1" {
		t.Errorf("expected server_name=labsqlsrv1")
	}
	if idMap[armKey(servers[0].Databases[0].ID)] != r.ID {
		t.Errorf("idMap mismatch")
	}
}

func TestTransformSQLServerContainsDatabaseConnections(t *testing.T) {
	srvArm := "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Sql/servers/labsqlsrv1"
	dbArm := "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Sql/servers/labsqlsrv1/databases/labdb1"
	servers := []SQLServer{
		{ID: srvArm, Name: "labsqlsrv1", Databases: []SQLDatabase{{ID: dbArm, Name: "labdb1", ServerID: srvArm}}},
	}
	srvIDMap := map[string]string{srvArm: "srv-id"}
	dbIDMap := map[string]string{dbArm: "db-id"}

	conns := TransformSQLServerContainsDatabaseConnections(servers, srvIDMap, dbIDMap)
	if len(conns) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(conns))
	}
	if conns[0].Type != "contains" {
		t.Errorf("expected type=contains, got %q", conns[0].Type)
	}
	if conns[0].Source != "srv-id" || conns[0].Target != "db-id" {
		t.Errorf("source/target mismatch: %+v", conns[0])
	}
}

func TestTransformPostgreSQLServers(t *testing.T) {
	subnetArm := "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Network/virtualNetworks/labvnet1/subnets/labpgsubnet"
	servers := []PostgreSQLServer{
		{
			ID:            "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.DBforPostgreSQL/flexibleServers/labpg1",
			Name:          "labpg1",
			Location:      "westeurope",
			ResourceGroup: "rg-lab-01",
			SKU:           &azFlexServerSKU{Name: "Standard_D2ds_v4", Tier: "GeneralPurpose"},
			Properties: &azFlexServerProperties{
				Version:                  "15",
				AdministratorLogin:       "pgadmin",
				FullyQualifiedDomainName: "labpg1.postgres.database.azure.com",
				State:                    "Ready",
				AvailabilityZone:         "1",
				Storage:                  &azFlexServerStorage{StorageSizeGB: 128, Tier: "P10", Iops: 500, AutoGrow: "Enabled"},
				Network:                  &azFlexServerNetwork{DelegatedSubnetResourceID: subnetArm, PublicNetworkAccess: "Disabled"},
				HighAvailability:         &azFlexServerHA{Mode: "ZoneRedundant", StandbyAvailabilityZone: "2", State: "Healthy"},
			},
		},
	}

	resources, idMap := TransformPostgreSQLServers(servers, testSub)
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	r := resources[0]
	if r.Type != "application.database" {
		t.Errorf("expected type application.database, got %q", r.Type)
	}
	if r.Status != "active" {
		t.Errorf("expected status active (Ready -> active), got %q", r.Status)
	}
	if r.Properties["version"] != "15" {
		t.Errorf("expected version=15")
	}
	if r.Properties["delegated_subnet_id"] != subnetArm {
		t.Errorf("expected delegated_subnet_id=%s", subnetArm)
	}
	if r.Properties["ha_mode"] != "ZoneRedundant" {
		t.Errorf("expected ha_mode=ZoneRedundant")
	}
	if r.Properties["storage_size_gb"] != 128 {
		t.Errorf("expected storage_size_gb=128, got %v", r.Properties["storage_size_gb"])
	}
	// Secrets must never appear aligned to OSIRIS JSON spec chapter 13.
	for key := range r.Properties {
		if strings.Contains(strings.ToLower(key), "password") {
			t.Errorf("PG server properties must not contain password field %q", key)
		}
	}
	if idMap[armKey(servers[0].ID)] != r.ID {
		t.Errorf("idMap mismatch")
	}
}

func TestTransformMySQLServers(t *testing.T) {
	servers := []MySQLServer{
		{
			ID:            "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.DBforMySQL/flexibleServers/labmysql1",
			Name:          "labmysql1",
			Location:      "westeurope",
			ResourceGroup: "rg-lab-01",
			SKU:           &azFlexServerSKU{Name: "Standard_B1ms", Tier: "Burstable"},
			Properties: &azFlexServerProperties{
				Version:            "8.0.21",
				AdministratorLogin: "mysqladmin",
				State:              "Ready",
				Network:            &azFlexServerNetwork{PublicNetworkAccess: "Enabled"},
			},
		},
	}

	resources, _ := TransformMySQLServers(servers, testSub)
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	if resources[0].Type != "application.database" {
		t.Errorf("expected type application.database, got %q", resources[0].Type)
	}
	if resources[0].Properties["tier"] != "Burstable" {
		t.Errorf("expected tier=Burstable")
	}
	if resources[0].Properties["public_network_access"] != "Enabled" {
		t.Errorf("expected public_network_access=Enabled")
	}
}

func TestTransformCosmosAccounts(t *testing.T) {
	peArm := "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Network/privateEndpoints/labpe-cosmos"
	accts := []CosmosAccount{
		{
			ID:            "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.DocumentDB/databaseAccounts/labcosmos1",
			Name:          "labcosmos1",
			Location:      "westeurope",
			ResourceGroup: "rg-lab-01",
			Kind:          "GlobalDocumentDB",
			Properties: &azCosmosProperties{
				ProvisioningState:             "Succeeded",
				DatabaseAccountOfferType:      "Standard",
				DocumentEndpoint:              "https://labcosmos1.documents.azure.com:443/",
				PublicNetworkAccess:           "Disabled",
				EnableAutomaticFailover:       true,
				IsVirtualNetworkFilterEnabled: true,
				DisableLocalAuth:              true,
				ConsistencyPolicy:             &azCosmosConsistency{DefaultConsistencyLevel: "Session"},
				Locations: []azCosmosLocation{
					{LocationName: "West Europe", FailoverPriority: 0, IsZoneRedundant: true},
					{LocationName: "North Europe", FailoverPriority: 1},
				},
				Capabilities: []azCosmosCapability{{Name: "EnableServerless"}},
				PrivateEndpointConnections: []azPrivateEndpointConnRef{
					{Properties: struct {
						PrivateEndpoint struct {
							ID string `json:"id"`
						} `json:"privateEndpoint"`
					}{PrivateEndpoint: struct {
						ID string `json:"id"`
					}{ID: peArm}}},
				},
			},
		},
	}

	resources, idMap := TransformCosmosAccounts(accts, testSub)
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	r := resources[0]
	if r.Type != "application.database" {
		t.Errorf("expected type application.database, got %q", r.Type)
	}
	if r.Properties["kind"] != "GlobalDocumentDB" {
		t.Errorf("expected kind=GlobalDocumentDB")
	}
	if r.Properties["consistency_level"] != "Session" {
		t.Errorf("expected consistency_level=Session")
	}
	if r.Properties["public_network_access"] != "Disabled" {
		t.Errorf("expected public_network_access=Disabled")
	}
	locs, ok := r.Properties["locations"].([]map[string]any)
	if !ok || len(locs) != 2 {
		t.Fatalf("expected 2 locations, got %v", r.Properties["locations"])
	}
	ext, _ := r.Extensions[extensionNamespace].(map[string]any)
	peIDs, _ := ext["private_endpoint_connection_ids"].([]string)
	if len(peIDs) != 1 || peIDs[0] != peArm {
		t.Errorf("expected pe ids=[%q], got %v", peArm, peIDs)
	}
	// Secrets must never appear aligned to OSIRIS JSON spec chapter 13.
	for key := range r.Properties {
		if strings.Contains(strings.ToLower(key), "key") ||
			strings.Contains(strings.ToLower(key), "connection_string") {
			t.Errorf("Cosmos properties must not contain %q", key)
		}
	}
	if idMap[armKey(accts[0].ID)] != r.ID {
		t.Errorf("idMap mismatch")
	}
}

func TestTransformRedisCaches(t *testing.T) {
	peArm := "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Network/privateEndpoints/labpe-redis"
	subnetArm := "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Network/virtualNetworks/labvnet1/subnets/labredis"
	caches := []RedisCache{
		{
			ID:                  "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Cache/Redis/labredis1",
			Name:                "labredis1",
			Location:            "westeurope",
			ResourceGroup:       "rg-lab-01",
			SKU:                 &azRedisSKU{Name: "Premium", Family: "P", Capacity: 1},
			RedisVersion:        "6.0",
			ProvisioningState:   "Succeeded",
			EnableNonSSLPort:    false,
			MinimumTLSVersion:   "1.2",
			PublicNetworkAccess: "Disabled",
			HostName:            "labredis1.redis.cache.windows.net",
			Port:                6379,
			SSLPort:             6380,
			ShardCount:          3,
			SubnetID:            subnetArm,
			StaticIP:            "192.0.2.5",
			Zones:               []string{"1"},
			PrivateEndpointConnections: []azPrivateEndpointConnRef{
				{Properties: struct {
					PrivateEndpoint struct {
						ID string `json:"id"`
					} `json:"privateEndpoint"`
				}{PrivateEndpoint: struct {
					ID string `json:"id"`
				}{ID: peArm}}},
			},
		},
	}

	resources, idMap := TransformRedisCaches(caches, testSub)
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	r := resources[0]
	if r.Type != "application.cache" {
		t.Errorf("expected type application.cache, got %q", r.Type)
	}
	if r.Properties["sku"] != "Premium" {
		t.Errorf("expected sku=Premium")
	}
	if r.Properties["redis_version"] != "6.0" {
		t.Errorf("expected redis_version=6.0")
	}
	if r.Properties["subnet_id"] != subnetArm {
		t.Errorf("expected subnet_id=%s, got %v", subnetArm, r.Properties["subnet_id"])
	}
	if r.Properties["shard_count"] != 3 {
		t.Errorf("expected shard_count=3")
	}
	ext, _ := r.Extensions[extensionNamespace].(map[string]any)
	peIDs, _ := ext["private_endpoint_connection_ids"].([]string)
	if len(peIDs) != 1 || peIDs[0] != peArm {
		t.Errorf("expected pe ids=[%q], got %v", peArm, peIDs)
	}
	// Secrets must never appear aligned to OSIRIS JSON spec chapter 13.
	for key := range r.Properties {
		if strings.Contains(strings.ToLower(key), "access_key") ||
			strings.Contains(strings.ToLower(key), "primary_key") ||
			strings.Contains(strings.ToLower(key), "secondary_key") {
			t.Errorf("Redis properties must not contain %q", key)
		}
	}
	if idMap[armKey(caches[0].ID)] != r.ID {
		t.Errorf("idMap mismatch")
	}
}

func TestTransformFlexServerToSubnetConnections(t *testing.T) {
	subnetArm := "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Network/virtualNetworks/labvnet1/subnets/labpgsubnet"
	pgs := []PostgreSQLServer{
		{
			ID:   "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.DBforPostgreSQL/flexibleServers/labpg1",
			Name: "labpg1",
			Properties: &azFlexServerProperties{
				Network: &azFlexServerNetwork{DelegatedSubnetResourceID: subnetArm},
			},
		},
		// Public-access server: no delegated subnet; must be skipped.
		{
			ID:   "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.DBforPostgreSQL/flexibleServers/labpg-public",
			Name: "labpg-public",
			Properties: &azFlexServerProperties{
				Network: &azFlexServerNetwork{PublicNetworkAccess: "Enabled"},
			},
		},
	}
	mys := []MySQLServer{
		{
			ID:   "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.DBforMySQL/flexibleServers/labmysql1",
			Name: "labmysql1",
			Properties: &azFlexServerProperties{
				Network: &azFlexServerNetwork{DelegatedSubnetResourceID: subnetArm},
			},
		},
	}
	serverIDMap := map[string]string{
		pgs[0].ID: "pg-rid",
		pgs[1].ID: "pgpub-rid",
		mys[0].ID: "mysql-rid",
	}
	subnetIDMap := map[string]string{subnetArm: "subnet-rid"}

	conns := TransformFlexServerToSubnetConnections(pgs, mys, serverIDMap, subnetIDMap)
	if len(conns) != 2 {
		t.Fatalf("expected 2 connections (pg + mysql, public-access skipped), got %d", len(conns))
	}
	for _, c := range conns {
		if c.Target != "subnet-rid" {
			t.Errorf("expected target=subnet-rid, got %q", c.Target)
		}
		if c.Type != "network" {
			t.Errorf("expected type=network")
		}
	}
}

func TestTransformRedisToSubnetConnections(t *testing.T) {
	subnetArm := "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Network/virtualNetworks/labvnet1/subnets/labredis"
	caches := []RedisCache{
		{ID: "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Cache/Redis/labredis1", Name: "labredis1", SubnetID: subnetArm},
		// Basic tier, no VNet injection: must be skipped.
		{ID: "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Cache/Redis/labredis-basic", Name: "labredis-basic"},
	}
	redisIDMap := map[string]string{
		caches[0].ID: "redis-rid",
		caches[1].ID: "redisbasic-rid",
	}
	subnetIDMap := map[string]string{subnetArm: "subnet-rid"}

	conns := TransformRedisToSubnetConnections(caches, redisIDMap, subnetIDMap)
	if len(conns) != 1 {
		t.Fatalf("expected 1 connection (basic tier skipped), got %d", len(conns))
	}
	if conns[0].Source != "redis-rid" || conns[0].Target != "subnet-rid" {
		t.Errorf("source/target mismatch: %+v", conns[0])
	}
}

func TestTransformAKSClusters(t *testing.T) {
	peArm := "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Network/privateEndpoints/labpe-aks"
	clusters := []AKSCluster{
		{
			ID:            "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.ContainerService/managedClusters/labaks1",
			Name:          "labaks1",
			Location:      "westeurope",
			ResourceGroup: "rg-lab-01",
			SKU:           &azAKSSKU{Name: "Base", Tier: "Standard"},
			Properties: &azAKSProperties{
				KubernetesVersion: "1.29.0",
				DNSPrefix:         "aks1-dns",
				FQDN:              "labaks1.hcp.westeurope.azmk8s.io",
				EnableRBAC:        true,
				ProvisioningState: "Succeeded",
				NodeResourceGroup: "MC_rg1_aks1_westeurope",
				NetworkProfile: &azAKSNetworkProfile{
					NetworkPlugin:   "azure",
					NetworkPolicy:   "calico",
					ServiceCIDR:     "198.51.100.0/24",
					LoadBalancerSKU: "Standard",
					OutboundType:    "loadBalancer",
				},
				APIServerAccessProfile: &azAKSAPIServerAccessProfile{
					EnablePrivateCluster: true,
					PrivateDNSZone:       "system",
				},
				AADProfile: &azAKSAADProfile{Managed: true, EnableAzureRBAC: true},
				PrivateEndpointConnections: []azPrivateEndpointConnRef{
					{Properties: struct {
						PrivateEndpoint struct {
							ID string `json:"id"`
						} `json:"privateEndpoint"`
					}{PrivateEndpoint: struct {
						ID string `json:"id"`
					}{ID: peArm}}},
				},
			},
			AgentPools: []AKSAgentPool{{ID: "pool1", Name: "np1"}, {ID: "pool2", Name: "np2"}},
		},
	}

	resources, idMap := TransformAKSClusters(clusters, testSub)
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	r := resources[0]
	if r.Type != "compute.cluster" {
		t.Errorf("expected type compute.cluster, got %q", r.Type)
	}
	if r.Properties["kubernetes_version"] != "1.29.0" {
		t.Errorf("expected kubernetes_version=1.29.0")
	}
	if r.Properties["agent_pool_count"] != 2 {
		t.Errorf("expected agent_pool_count=2, got %v", r.Properties["agent_pool_count"])
	}
	if r.Properties["private_cluster"] != true {
		t.Errorf("expected private_cluster=true")
	}
	if r.Properties["aad_managed"] != true {
		t.Errorf("expected aad_managed=true")
	}
	np, ok := r.Properties["network_profile"].(map[string]any)
	if !ok {
		t.Fatalf("expected network_profile map")
	}
	if np["network_plugin"] != "azure" {
		t.Errorf("expected network_plugin=azure")
	}
	ext, _ := r.Extensions[extensionNamespace].(map[string]any)
	peIDs, _ := ext["private_endpoint_connection_ids"].([]string)
	if len(peIDs) != 1 || peIDs[0] != peArm {
		t.Errorf("expected pe ids=[%q], got %v", peArm, peIDs)
	}
	if idMap[armKey(clusters[0].ID)] != r.ID {
		t.Errorf("idMap mismatch")
	}
}

func TestTransformAKSAgentPools(t *testing.T) {
	subnetArm := "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Network/virtualNetworks/labvnet1/subnets/labaks"
	clusters := []AKSCluster{
		{
			ID:       "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.ContainerService/managedClusters/labaks1",
			Name:     "labaks1",
			Location: "westeurope",
			AgentPools: []AKSAgentPool{
				{
					ID:                "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.ContainerService/managedClusters/labaks1/agentPools/labnodepool1",
					Name:              "labnodepool1",
					VMSize:            "Standard_DS2_v2",
					Count:             3,
					EnableAutoScaling: true, MinCount: 1, MaxCount: 5,
					OSType: "Linux", OSSKU: "Ubuntu", Mode: "System",
					OrchestratorVer:   "1.29.0",
					VNetSubnetID:      subnetArm,
					ProvisioningState: "Succeeded",
					ClusterID:         "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.ContainerService/managedClusters/labaks1",
					ClusterName:       "labaks1",
				},
			},
		},
	}

	resources, idMap := TransformAKSAgentPools(clusters, testSub)
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	r := resources[0]
	if r.Type != "osiris.azure.aks.nodepool" {
		t.Errorf("expected type osiris.azure.aks.nodepool, got %q", r.Type)
	}
	if r.Properties["vm_size"] != "Standard_DS2_v2" {
		t.Errorf("expected vm_size=Standard_DS2_v2")
	}
	if r.Properties["autoscale"] != true {
		t.Errorf("expected autoscale=true")
	}
	if r.Properties["min_count"] != 1 || r.Properties["max_count"] != 5 {
		t.Errorf("expected min/max=1/5")
	}
	if r.Properties["vnet_subnet_id"] != subnetArm {
		t.Errorf("expected vnet_subnet_id=%s", subnetArm)
	}
	if idMap[armKey(clusters[0].AgentPools[0].ID)] != r.ID {
		t.Errorf("idMap mismatch")
	}
}

func TestTransformContainerAppEnvironments(t *testing.T) {
	subnetArm := "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Network/virtualNetworks/labvnet1/subnets/labace"
	envs := []ContainerAppEnvironment{
		{
			ID:            "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.App/managedEnvironments/labenv1",
			Name:          "labenv1",
			Location:      "westeurope",
			ResourceGroup: "rg-lab-01",
			Properties: &azContainerEnvProperties{
				ProvisioningState: "Succeeded",
				DefaultDomain:     "labenv-abc.westeurope.azurecontainerapps.io",
				StaticIP:          "192.0.2.4",
				ZoneRedundant:     true,
				VNetConfiguration: &azContainerEnvVNetConfig{
					InfrastructureSubnetID: subnetArm,
					Internal:               true,
				},
			},
		},
	}

	resources, idMap := TransformContainerAppEnvironments(envs, testSub)
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	r := resources[0]
	if r.Type != "osiris.azure.containerapp.environment" {
		t.Errorf("expected type osiris.azure.containerapp.environment, got %q", r.Type)
	}
	if r.Properties["infrastructure_subnet_id"] != subnetArm {
		t.Errorf("expected infrastructure_subnet_id=%s", subnetArm)
	}
	if r.Properties["internal"] != true {
		t.Errorf("expected internal=true")
	}
	if r.Properties["zone_redundant"] != true {
		t.Errorf("expected zone_redundant=true")
	}
	if idMap[armKey(envs[0].ID)] != r.ID {
		t.Errorf("idMap mismatch")
	}
}

func TestTransformContainerApps(t *testing.T) {
	envArm := "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.App/managedEnvironments/labenv1"
	apps := []ContainerApp{
		// Uses environmentId.
		{
			ID:            "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.App/containerApps/labapp1",
			Name:          "labapp1",
			Location:      "westeurope",
			ResourceGroup: "rg-lab-01",
			Properties: &azContainerAppProperties{
				ProvisioningState:   "Succeeded",
				EnvironmentID:       envArm,
				LatestRevisionFQDN:  "labapp1.labenv.westeurope.azurecontainerapps.io",
				WorkloadProfileName: "Consumption",
				Configuration: &azContainerAppConfig{
					ActiveRevisionsMode: "Single",
					Ingress: &azContainerAppConfigIngress{
						External: true, TargetPort: 8080, Transport: "http",
					},
				},
			},
		},
		// Uses managedEnvironmentId fallback.
		{
			ID:   "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.App/containerApps/labapp2",
			Name: "labapp2",
			Properties: &azContainerAppProperties{
				ManagedEnvironmentID: envArm,
			},
		},
	}

	resources, _ := TransformContainerApps(apps, testSub)
	if len(resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(resources))
	}
	if resources[0].Properties["environment_id"] != envArm {
		t.Errorf("expected labapp1 environment_id=%s", envArm)
	}
	if resources[1].Properties["environment_id"] != envArm {
		t.Errorf("expected labapp2 environment_id=%s (fallback)", envArm)
	}
	ing, ok := resources[0].Properties["ingress"].(map[string]any)
	if !ok {
		t.Fatalf("expected ingress map on labapp1")
	}
	if ing["external"] != true || ing["target_port"] != 8080 {
		t.Errorf("ingress fields wrong: %+v", ing)
	}
}

func TestTransformContainerGroups(t *testing.T) {
	subnetArm := "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Network/virtualNetworks/labvnet1/subnets/labaci"
	groups := []ContainerGroup{
		{
			ID:            "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.ContainerInstance/containerGroups/labcg1",
			Name:          "labcg1",
			Location:      "westeurope",
			ResourceGroup: "rg-lab-01",
			Properties: &azContainerGroupProperties{
				ProvisioningState: "Succeeded",
				OSType:            "Linux",
				RestartPolicy:     "Always",
				Sku:               "Standard",
				IPAddress: &azContainerGroupIPAddress{
					IP: "192.0.2.5", Type: "Private", FQDN: "labcg1.westeurope.azurecontainer.io",
				},
				SubnetIDs:  []azContainerGroupSubnetRef{{ID: subnetArm}},
				Containers: []map[string]any{{"name": "web"}, {"name": "sidecar"}},
			},
		},
	}

	resources, _ := TransformContainerGroups(groups, testSub)
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	r := resources[0]
	if r.Type != "compute.container" {
		t.Errorf("expected type compute.container, got %q", r.Type)
	}
	if r.Properties["os_type"] != "Linux" {
		t.Errorf("expected os_type=Linux")
	}
	addr, ok := r.Properties["ip_address"].(map[string]any)
	if !ok {
		t.Fatalf("expected ip_address map")
	}
	if addr["type"] != "Private" {
		t.Errorf("expected ip_address.type=Private")
	}
	if r.Properties["container_count"] != 2 {
		t.Errorf("expected container_count=2, got %v", r.Properties["container_count"])
	}
}

func TestTransformServiceBusNamespaces(t *testing.T) {
	peArm := "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Network/privateEndpoints/labpe-sb"
	namespaces := []ServiceBusNamespace{
		{
			ID:            "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.ServiceBus/namespaces/labsb1",
			Name:          "labsb1",
			Location:      "westeurope",
			ResourceGroup: "rg-lab-01",
			SKU:           &azMessagingSKU{Name: "Premium", Tier: "Premium", Capacity: 1},
			Properties: &azMessagingProperties{
				ProvisioningState:   "Succeeded",
				ServiceBusEndpoint:  "https://labsb1.servicebus.windows.net:443/",
				ZoneRedundant:       true,
				DisableLocalAuth:    true,
				PublicNetworkAccess: "Disabled",
				MinimumTLSVersion:   "1.2",
				PrivateEndpointConnections: []azPrivateEndpointConnRef{
					{Properties: struct {
						PrivateEndpoint struct {
							ID string `json:"id"`
						} `json:"privateEndpoint"`
					}{PrivateEndpoint: struct {
						ID string `json:"id"`
					}{ID: peArm}}},
				},
			},
		},
	}

	resources, _ := TransformServiceBusNamespaces(namespaces, testSub)
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	r := resources[0]
	if r.Type != "application.queue" {
		t.Errorf("expected type application.queue, got %q", r.Type)
	}
	if r.Properties["sku_name"] != "Premium" {
		t.Errorf("expected sku_name=Premium")
	}
	if r.Properties["zone_redundant"] != true {
		t.Errorf("expected zone_redundant=true")
	}
	if r.Properties["entra_only"] != true {
		t.Errorf("expected entra_only=true")
	}
	ext, _ := r.Extensions[extensionNamespace].(map[string]any)
	peIDs, _ := ext["private_endpoint_connection_ids"].([]string)
	if len(peIDs) != 1 || peIDs[0] != peArm {
		t.Errorf("expected pe ids=[%q], got %v", peArm, peIDs)
	}
	// Secrets connection strings must never appear aligned to OSIRIS JSON spec chapter 13.
	for key := range r.Properties {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "connection_string") || strings.Contains(lower, "key") {
			t.Errorf("Service Bus properties must not contain %q", key)
		}
	}
}

func TestTransformAPIMServices(t *testing.T) {
	subnetArm := "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Network/virtualNetworks/labvnet1/subnets/labapim"
	services := []APIMService{
		{
			ID:            "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.ApiManagement/service/labapim1",
			Name:          "labapim1",
			Location:      "westeurope",
			ResourceGroup: "rg-lab-01",
			SKU:           &azAPIMSKU{Name: "Developer", Capacity: 1},
			Properties: &azAPIMProperties{
				ProvisioningState:  "Succeeded",
				GatewayURL:         "https://labapim1.azure-api.net",
				PortalURL:          "https://labapim1.developer.azure-api.net",
				ManagementURL:      "https://labapim1.management.azure-api.net",
				VirtualNetworkType: "Internal",
				VirtualNetworkConfiguration: &azAPIMVirtualNetworkConfig{
					SubnetResourceID: subnetArm,
				},
				PublicIPAddresses:  []string{"203.0.113.10"},
				PrivateIPAddresses: []string{"192.0.2.4"},
			},
		},
	}

	resources, _ := TransformAPIMServices(services, testSub)
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	r := resources[0]
	if r.Type != "osiris.azure.apim" {
		t.Errorf("expected type osiris.azure.apim, got %q", r.Type)
	}
	if r.Properties["sku_name"] != "Developer" {
		t.Errorf("expected sku_name=Developer")
	}
	if r.Properties["virtual_network_type"] != "Internal" {
		t.Errorf("expected virtual_network_type=Internal")
	}
	if r.Properties["subnet_id"] != subnetArm {
		t.Errorf("expected subnet_id=%s", subnetArm)
	}
}

func TestTransformFrontDoorProfiles(t *testing.T) {
	profiles := []FrontDoorProfile{
		{
			ID:            "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Cdn/profiles/labafd1",
			Name:          "labafd1",
			Location:      "global",
			ResourceGroup: "rg-lab-01",
			SKU:           &azFrontDoorSKU{Name: "Premium_AzureFrontDoor"},
			Kind:          "frontdoor",
			Properties: &azFrontDoorProperties{
				ProvisioningState: "Succeeded",
				ResourceState:     "Active",
				FrontDoorID:       "fd-xyz",
			},
		},
	}

	resources, _ := TransformFrontDoorProfiles(profiles, testSub)
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	r := resources[0]
	if r.Type != "osiris.azure.frontdoor.profile" {
		t.Errorf("expected type osiris.azure.frontdoor.profile, got %q", r.Type)
	}
	if r.Properties["sku_name"] != "Premium_AzureFrontDoor" {
		t.Errorf("expected sku_name=Premium_AzureFrontDoor")
	}
	if r.Properties["front_door_id"] != "fd-xyz" {
		t.Errorf("expected front_door_id=fd-xyz")
	}
}

func TestTransformAKSClusterContainsAgentPoolConnections(t *testing.T) {
	clusters := []AKSCluster{
		{
			ID:   "cluster-arm",
			Name: "labaks1",
			AgentPools: []AKSAgentPool{
				{ID: "pool1-arm", Name: "labnodepool1"},
				{ID: "pool2-arm", Name: "nodepool2"},
			},
		},
	}
	clusterIDMap := map[string]string{"cluster-arm": "cluster-rid"}
	poolIDMap := map[string]string{"pool1-arm": "pool1-rid", "pool2-arm": "pool2-rid"}

	conns := TransformAKSClusterContainsAgentPoolConnections(clusters, clusterIDMap, poolIDMap)
	if len(conns) != 2 {
		t.Fatalf("expected 2 connections, got %d", len(conns))
	}
	for _, c := range conns {
		if c.Type != "contains" {
			t.Errorf("expected type=contains")
		}
		if c.Source != "cluster-rid" {
			t.Errorf("expected source=cluster-rid, got %q", c.Source)
		}
	}
}

func TestTransformContainerEnvContainsAppConnections(t *testing.T) {
	envArm := "env-arm"
	apps := []ContainerApp{
		{ID: "app1-arm", Name: "labapp1", Properties: &azContainerAppProperties{EnvironmentID: envArm}},
		{ID: "app2-arm", Name: "labapp2", Properties: &azContainerAppProperties{ManagedEnvironmentID: envArm}},
		// No env ID set: must be skipped.
		{ID: "app-orphan", Name: "orphan", Properties: &azContainerAppProperties{}},
	}
	envIDMap := map[string]string{envArm: "env-rid"}
	appIDMap := map[string]string{
		"app1-arm":   "app1-rid",
		"app2-arm":   "app2-rid",
		"app-orphan": "orphan-rid",
	}

	conns := TransformContainerEnvContainsAppConnections(apps, envIDMap, appIDMap)
	if len(conns) != 2 {
		t.Fatalf("expected 2 connections (orphan skipped), got %d", len(conns))
	}
	for _, c := range conns {
		if c.Type != "contains" {
			t.Errorf("expected type=contains")
		}
		if c.Source != "env-rid" {
			t.Errorf("expected source=env-rid, got %q", c.Source)
		}
	}
}

func TestTransformContainerGroupToSubnetConnections(t *testing.T) {
	subnetArm := "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Network/virtualNetworks/labvnet1/subnets/labaci"
	groups := []ContainerGroup{
		{
			ID:   "cg1-arm",
			Name: "labcg1",
			Properties: &azContainerGroupProperties{
				SubnetIDs: []azContainerGroupSubnetRef{{ID: subnetArm}},
			},
		},
		// Public-IP ACI: no SubnetIDs, must be skipped.
		{ID: "cg-public-arm", Name: "cg-public", Properties: &azContainerGroupProperties{}},
	}
	cgIDMap := map[string]string{"cg1-arm": "cg1-rid", "cg-public-arm": "cgpub-rid"}
	subnetIDMap := map[string]string{subnetArm: "subnet-rid"}

	conns := TransformContainerGroupToSubnetConnections(groups, cgIDMap, subnetIDMap)
	if len(conns) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(conns))
	}
	if conns[0].Source != "cg1-rid" || conns[0].Target != "subnet-rid" {
		t.Errorf("source/target mismatch: %+v", conns[0])
	}
	if conns[0].Type != "network" {
		t.Errorf("expected type=network")
	}
}

func TestNormalizeAzureLocation(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"westeurope", "westeurope"},
		{"West Europe", "westeurope"},
		{"East US 2", "eastus2"},
		{"UK South", "uksouth"},
		{"global", "global"},
		{"", ""},
	}
	for _, c := range cases {
		if got := normalizeAzureLocation(c.in); got != c.want {
			t.Errorf("normalizeAzureLocation(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTransformAPIMToSubnetConnections(t *testing.T) {
	subnetArm := "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Network/virtualNetworks/labvnet1/subnets/labapim"
	services := []APIMService{
		{
			ID:   "apim1-arm",
			Name: "labapim1",
			Properties: &azAPIMProperties{
				VirtualNetworkType:          "Internal",
				VirtualNetworkConfiguration: &azAPIMVirtualNetworkConfig{SubnetResourceID: subnetArm},
			},
		},
		// None mode: must be skipped.
		{
			ID:         "apim-public-arm",
			Name:       "apim-public",
			Properties: &azAPIMProperties{VirtualNetworkType: "None"},
		},
	}
	apimIDMap := map[string]string{"apim1-arm": "apim-rid", "apim-public-arm": "apimpub-rid"}
	subnetIDMap := map[string]string{subnetArm: "subnet-rid"}

	conns := TransformAPIMToSubnetConnections(services, apimIDMap, subnetIDMap)
	if len(conns) != 1 {
		t.Fatalf("expected 1 connection (None mode skipped), got %d", len(conns))
	}
	if conns[0].Source != "apim-rid" || conns[0].Target != "subnet-rid" {
		t.Errorf("source/target mismatch: %+v", conns[0])
	}
}

func TestTransformGatewayConnections_CrossSubscriptionPeer(t *testing.T) {
	localGW := "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Network/virtualNetworkGateways/labgw1"
	remoteER := "/subscriptions/00000000-0000-4000-8000-000000002222/resourceGroups/rg-lab-hub/providers/Microsoft.Network/expressRouteCircuits/labhub-xpr"

	gwConns := []GatewayConnection{
		{
			ID:                     "conn1-arm",
			Name:                   "conn1",
			ConnectionType:         "ExpressRoute",
			VirtualNetworkGateway1: &azGatewayRef{ID: localGW},
			ExpressRouteCircuit:    &azGatewayRef{ID: remoteER},
		},
	}
	allIDMap := map[string]string{localGW: "gw1-rid"}

	all, stubs := TransformGatewayConnections(gwConns, allIDMap)
	conns, endpoints := splitERConnectionEndpoints(all)
	if len(conns) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(conns))
	}
	if len(endpoints) != 2 {
		t.Fatalf("expected connection -> gateway and connection -> circuit endpoint edges, got %d", len(endpoints))
	}
	if len(stubs) != 1 {
		t.Fatalf("expected 1 stub for cross-sub ER circuit, got %d", len(stubs))
	}
	if stubs[0].Type != "osiris.azure.expressroute" {
		t.Errorf("stub type = %q, want osiris.azure.expressroute", stubs[0].Type)
	}
	if stubs[0].Provider.Account != "00000000-0000-4000-8000-000000002222" {
		t.Errorf("stub account = %q, want the remote sub", stubs[0].Provider.Account)
	}
	if stubs[0].Provider.Type != "Microsoft.Network/expressRouteCircuits" {
		t.Errorf("stub ARM type = %q, want Microsoft.Network/expressRouteCircuits", stubs[0].Provider.Type)
	}
	if stubs[0].Status != "unknown" {
		t.Errorf("stub status = %q, want unknown", stubs[0].Status)
	}
	if conns[0].Source != "gw1-rid" || conns[0].Target != stubs[0].ID {
		t.Errorf("connection endpoints mismatch: source=%q target=%q stub=%q", conns[0].Source, conns[0].Target, stubs[0].ID)
	}
	if conns[0].Type != "network" || conns[0].Direction != "bidirectional" {
		t.Errorf("expected ExpressRoute gateway connection to be network bidirectional, got %q %q", conns[0].Type, conns[0].Direction)
	}
}

func TestGatewayConnectionSubtype(t *testing.T) {
	cases := map[string]string{
		"ExpressRoute": "network",
		"expressroute": "network",
		"IPsec":        "network.vpn",
		"Vnet2Vnet":    "network.vpn",
		"VPNClient":    "network.vpn",
		"":             "network",
		"unknown":      "network",
	}
	for in, want := range cases {
		if got := gatewayConnectionSubtype(in); got != want {
			t.Errorf("gatewayConnectionSubtype(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTransformGatewayConnections_InScopePeer(t *testing.T) {
	localGW := "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab/providers/Microsoft.Network/virtualNetworkGateways/labgw1"
	localER := "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab/providers/Microsoft.Network/expressRouteCircuits/laber1"
	gwConns := []GatewayConnection{
		{
			ID:                     "conn1-arm",
			Name:                   "conn1",
			ConnectionType:         "ExpressRoute",
			VirtualNetworkGateway1: &azGatewayRef{ID: localGW},
			ExpressRouteCircuit:    &azGatewayRef{ID: localER},
		},
	}
	allIDMap := map[string]string{localGW: "gw1-rid", localER: "er1-rid"}

	all, stubs := TransformGatewayConnections(gwConns, allIDMap)
	conns, endpoints := splitERConnectionEndpoints(all)
	if len(conns) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(conns))
	}
	if len(endpoints) != 2 {
		t.Fatalf("expected connection -> gateway and connection -> circuit endpoint edges, got %d", len(endpoints))
	}
	if len(stubs) != 0 {
		t.Fatalf("expected no stubs when peer is in scope, got %d", len(stubs))
	}
	if conns[0].Target != "er1-rid" {
		t.Errorf("target = %q, want er1-rid", conns[0].Target)
	}
}

func TestTransformGatewayConnections_MissingGateway(t *testing.T) {
	gwConns := []GatewayConnection{
		{
			ID:                     "conn1-arm",
			Name:                   "conn1",
			VirtualNetworkGateway1: &azGatewayRef{ID: "gw-never-collected"},
			ExpressRouteCircuit:    &azGatewayRef{ID: "er-whatever"},
		},
	}
	conns, stubs := TransformGatewayConnections(gwConns, map[string]string{})
	if len(conns) != 0 {
		t.Errorf("expected 0 connections when gateway1 is missing, got %d", len(conns))
	}
	if len(stubs) != 0 {
		t.Errorf("expected 0 stubs when gateway1 is missing, got %d", len(stubs))
	}
}

func TestTransformPrivateEndpoints_TargetLinkage(t *testing.T) {
	pes := []PrivateEndpoint{
		{
			ID:   "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab/providers/Microsoft.Network/privateEndpoints/labpe-blob",
			Name: "labpe-blob",
			PrivateLinkServiceConnections: []azPrivateLinkServiceConnection{
				{
					Name:                 "pe-blob-conn",
					PrivateLinkServiceID: "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab/providers/Microsoft.Storage/storageAccounts/labstg",
					GroupIDs:             []string{"blob"},
				},
			},
			CustomDNSConfigs: []azPrivateEndpointDNSConfig{
				{FQDN: "labstg.blob.core.windows.net", IPAddresses: []string{"192.0.2.5"}},
			},
		},
		{
			ID:   "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab/providers/Microsoft.Network/privateEndpoints/labpe-bare",
			Name: "labpe-bare",
		},
	}

	resources := TransformPrivateEndpoints(pes, testSub)
	if len(resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(resources))
	}

	enriched := resources[0]
	if got := enriched.Properties["group_id"]; got != "blob" {
		t.Errorf("group_id = %v, want blob", got)
	}
	if got := enriched.Properties["private_link_service_id"]; got != "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab/providers/Microsoft.Storage/storageAccounts/labstg" {
		t.Errorf("private_link_service_id = %v", got)
	}
	configs, ok := enriched.Properties["custom_dns_configs"].([]map[string]any)
	if !ok || len(configs) != 1 {
		t.Fatalf("custom_dns_configs missing or wrong shape: %#v", enriched.Properties["custom_dns_configs"])
	}
	if configs[0]["fqdn"] != "labstg.blob.core.windows.net" {
		t.Errorf("dns fqdn = %v", configs[0]["fqdn"])
	}

	bare := resources[1]
	if _, has := bare.Properties["group_id"]; has {
		t.Errorf("PE without PLS conn must not emit group_id")
	}
	if _, has := bare.Properties["private_link_service_id"]; has {
		t.Errorf("PE without PLS conn must not emit private_link_service_id")
	}
	if _, has := bare.Properties["custom_dns_configs"]; has {
		t.Errorf("PE without custom DNS must not emit custom_dns_configs")
	}
}

func TestTransformAppGateways(t *testing.T) {
	http2 := true
	gws := []ApplicationGateway{
		{
			ID:                "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Network/applicationGateways/labagw1",
			Name:              "labagw1",
			Location:          "westeurope",
			ResourceGroup:     "rg-lab-01",
			Zones:             []string{"1", "2"},
			ProvisioningState: "Succeeded",
			OperationalState:  "Running",
			SKU:               azAGWSKU{Name: "WAF_v2", Tier: "WAF_v2", Capacity: 2},
			WebApplicationFirewallConfiguration: &azWAFConfig{
				Enabled:      true,
				FirewallMode: "Prevention",
			},
			HTTPListeners:       []struct{}{{}},
			BackendAddressPools: []struct{}{{}, {}},
			EnableHttp2:         &http2,
		},
	}

	resources := TransformAppGateways(gws, testSub)
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	r := resources[0]
	if r.Type != "osiris.azure.applicationgateway" {
		t.Errorf("expected type osiris.azure.applicationgateway, got %q", r.Type)
	}
	if r.Status != "active" {
		t.Errorf("expected status active, got %q", r.Status)
	}
	if r.Properties["sku_name"] != "WAF_v2" {
		t.Errorf("expected sku_name WAF_v2, got %v", r.Properties["sku_name"])
	}
	if r.Properties["waf_enabled"] != true {
		t.Errorf("expected waf_enabled true, got %v", r.Properties["waf_enabled"])
	}
	if r.Properties["listener_count"] != 1 {
		t.Errorf("expected listener_count 1, got %v", r.Properties["listener_count"])
	}
	if r.Properties["backend_pool_count"] != 2 {
		t.Errorf("expected backend_pool_count 2, got %v", r.Properties["backend_pool_count"])
	}
	if r.Properties["enable_http2"] != true {
		t.Errorf("expected enable_http2 true, got %v", r.Properties["enable_http2"])
	}
	if r.Provider.Zone != "1,2" {
		t.Errorf("expected zone 1,2, got %q", r.Provider.Zone)
	}
}

func TestTransformMetricAlerts(t *testing.T) {
	alerts := []MetricAlert{
		{
			ID:                  "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Insights/metricAlerts/labalert1",
			Name:                "labalert1",
			Location:            "global",
			ResourceGroup:       "rg-lab-01",
			Description:         "CPU alert",
			Severity:            2,
			Enabled:             true,
			EvaluationFrequency: "PT1M",
			WindowSize:          "PT5M",
			TargetResourceType:  "Microsoft.Compute/virtualMachines",
			AutoMitigate:        true,
			Scopes:              []string{"/subscriptions/00000000-0000-4000-8000-000000001111"},
			Actions: []azMetricAlertActionGroup{
				{ActionGroupID: "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Insights/actionGroups/labag1"},
			},
		},
	}

	resources, idMap := TransformMetricAlerts(alerts, testSub)
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	r := resources[0]
	if r.Type != "osiris.azure.monitor.metricalert" {
		t.Errorf("expected type osiris.azure.monitor.metricalert, got %q", r.Type)
	}
	if r.Status != "active" {
		t.Errorf("expected status active, got %q", r.Status)
	}
	if r.Properties["severity"] != 2 {
		t.Errorf("expected severity 2, got %v", r.Properties["severity"])
	}
	if r.Properties["evaluation_frequency"] != "PT1M" {
		t.Errorf("expected evaluation_frequency PT1M, got %v", r.Properties["evaluation_frequency"])
	}
	if _, ok := idMap[armKey(alerts[0].ID)]; !ok {
		t.Error("expected ID in idMap")
	}
}

func TestTransformActionGroups(t *testing.T) {
	groups := []ActionGroup{
		{
			ID:               "/subscriptions/00000000-0000-4000-8000-000000001111/resourceGroups/rg-lab-01/providers/Microsoft.Insights/actionGroups/labag1",
			Name:             "labag1",
			Location:         "global",
			ResourceGroup:    "rg-lab-01",
			GroupShortName:   "ag1short",
			Enabled:          true,
			EmailReceivers:   []struct{}{{}, {}},
			WebhookReceivers: []struct{}{{}},
		},
	}

	resources, idMap := TransformActionGroups(groups, testSub)
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	r := resources[0]
	if r.Type != "osiris.azure.monitor.actiongroup" {
		t.Errorf("expected type osiris.azure.monitor.actiongroup, got %q", r.Type)
	}
	if r.Status != "active" {
		t.Errorf("expected status active, got %q", r.Status)
	}
	if r.Properties["group_short_name"] != "ag1short" {
		t.Errorf("expected group_short_name ag1short, got %v", r.Properties["group_short_name"])
	}
	if r.Properties["receiver_count"] != 3 {
		t.Errorf("expected receiver_count 3, got %v", r.Properties["receiver_count"])
	}
	if r.Properties["email_receiver_count"] != 2 {
		t.Errorf("expected email_receiver_count 2, got %v", r.Properties["email_receiver_count"])
	}
	if _, ok := idMap[armKey(groups[0].ID)]; !ok {
		t.Error("expected ID in idMap")
	}
}

// splitERConnectionEndpoints separates gateway <-> circuit data-path
// edges from ExpressRoute connection -> endpoint configuration edges.
func splitERConnectionEndpoints(all []sdk.Connection) (dataPath, endpoints []sdk.Connection) {
	for _, c := range all {
		if c.Type == connERConnectionEndpoint {
			endpoints = append(endpoints, c)
		} else {
			dataPath = append(dataPath, c)
		}
	}
	return dataPath, endpoints
}

// TestProviderAccountCanonicalCase: an ARM ID may spell the
// subscription GUID in upper case; provider.account is the same
// subscription in the canonical lower-case form, so it equals
// metadata.scope.accounts, and the exported tenant applies.
func TestProviderAccountCanonicalCase(t *testing.T) {
	arm := "/subscriptions/00000000-0000-4000-8000-00000000A1B2/resourceGroups/rg-lab-01/providers/microsoft.insights/actionGroups/labag1"
	p := azureProvider(arm, "microsoft.insights/actionGroups", "global", testSub)
	if p.Account != "00000000-0000-4000-8000-00000000a1b2" || p.Tenant != testSub.TenantID {
		t.Errorf("provider = account %q tenant %q, want lower-case exported subscription and its tenant", p.Account, p.Tenant)
	}
	if p.NativeID != arm {
		t.Errorf("native_id must keep the ARM ID as returned, got %q", p.NativeID)
	}
}
