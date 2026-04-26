// transform_test.go - Unit tests for Microsoft Azure data transformation.
//
// For an introduction to OSIRIS JSON Producer for Microsoft Azure see:
// "[OSIRIS-JSON-AZURE]."
//
// [OSIRIS-JSON-AZURE]: https://osirisjson.org/en/docs/producers/hyperscalers/microsoft-azure
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/docs/spec/v10/00-preface

package azure

import (
	"strings"
	"testing"

	"go.osirisjson.org/producers/pkg/sdk"
)

var testSub = SubscriptionInfo{
	SubscriptionID: "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
	DisplayName:    "my-nonprod-subscription",
	State:          "Enabled",
	TenantID:       "f1e2d3c4-b5a6-9078-fedc-ba9876543210",
	Tags: map[string]string{
		"environment": "np",
		"department":  "information technology",
	},
}

func TestTransformVNets(t *testing.T) {
	vnets := []VirtualNetwork{
		{
			ID:            "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Network/virtualNetworks/vnet1",
			Name:          "vnet1",
			Location:      "westeurope",
			ResourceGroup: "rg1",
			AddressSpace:  azAddressSpace{AddressPrefixes: []string{"10.0.0.0/16"}},
			DhcpOptions:   azDHCPOptions{DNSServers: []string{"10.0.0.4"}},
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
	if r.Name != "vnet1" {
		t.Errorf("expected name vnet1, got %q", r.Name)
	}
	if r.Provider.Name != "azure" {
		t.Errorf("expected provider azure, got %q", r.Provider.Name)
	}
	if r.Provider.Region != "westeurope" {
		t.Errorf("expected region westeurope, got %q", r.Provider.Region)
	}
	if r.Provider.Subscription != "a1b2c3d4-e5f6-7890-abcd-ef1234567890" {
		t.Errorf("expected subscription ID in provider, got %q", r.Provider.Subscription)
	}
	if r.Status != "active" {
		t.Errorf("expected status active, got %q", r.Status)
	}
	if r.Properties["resource_group"] != "rg1" {
		t.Errorf("expected resource_group rg1, got %v", r.Properties["resource_group"])
	}
	addrs, ok := r.Properties["address_space"].([]string)
	if !ok || len(addrs) != 1 || addrs[0] != "10.0.0.0/16" {
		t.Errorf("expected address_space [10.0.0.0/16], got %v", r.Properties["address_space"])
	}
}

func TestTransformSubnets(t *testing.T) {
	subnets := []Subnet{
		{
			ID:                   "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Network/virtualNetworks/vnet1/subnets/subnet1",
			Name:                 "subnet1",
			ResourceGroup:        "rg1",
			AddressPrefixes:      []string{"10.0.1.0/24"},
			NetworkSecurityGroup: &azNSGRef{ID: "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Network/networkSecurityGroups/nsg1"},
			RouteTable:           &azRouteTableRef{ID: "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Network/routeTables/rt1"},
			ServiceEndpoints:     []azServiceEndpoint{{Service: "Microsoft.Storage", Locations: []string{"*"}}},
		},
	}

	resources, idMap := TransformSubnets(subnets, testSub)

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
	if r.Name != "subnet1" {
		t.Errorf("expected name subnet1, got %q", r.Name)
	}

	// Verify the ID map contains the subnet's ARM ID.
	if _, ok := idMap[subnets[0].ID]; !ok {
		t.Error("expected subnet ARM ID in ID map")
	}
}

func TestTransformNICs(t *testing.T) {
	nics := []NetworkInterface{
		{
			ID:            "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Network/networkInterfaces/nic1",
			Name:          "nic1",
			Location:      "westeurope",
			ResourceGroup: "rg1",
			IPConfigurations: []IPConfiguration{
				{
					Name:                      "ipconfig1",
					Subnet:                    &azSubnetRef{ID: "/subscriptions/11111111-1111-1111-1111-111111111111/.../subnets/subnet1"},
					PrivateIPAddress:          "10.0.1.4",
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
	if ips[0]["private_ip"] != "10.0.1.4" {
		t.Errorf("expected private_ip 10.0.1.4, got %v", ips[0]["private_ip"])
	}

	if _, ok := idMap[nics[0].ID]; !ok {
		t.Error("expected NIC ARM ID in ID map")
	}
}

func TestTransformVMs(t *testing.T) {
	vms := []VirtualMachine{
		{
			ID:            "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Compute/virtualMachines/vm1",
			Name:          "vm1",
			Location:      "westeurope",
			ResourceGroup: "rg1",
			VMSize:        "Standard_D2s_v3",
			PowerState:    "VM running",
		},
		{
			ID:            "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Compute/virtualMachines/vm2",
			Name:          "vm2",
			Location:      "westeurope",
			ResourceGroup: "rg1",
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
		"/subscriptions/11111111-1111-1111-1111-111111111111/.../virtualNetworks/vnet1": "azure::/subscriptions/11111111-1111-1111-1111-111111111111/.../virtualNetworks/vnet1",
		"/subscriptions/11111111-1111-1111-1111-111111111111/.../virtualNetworks/vnet2": "azure::/subscriptions/11111111-1111-1111-1111-111111111111/.../virtualNetworks/vnet2",
	}

	peerings := []VNetPeering{
		{
			ID:                   "/subscriptions/11111111-1111-1111-1111-111111111111/.../virtualNetworks/vnet1/virtualNetworkPeerings/peer1",
			Name:                 "vnet1-to-vnet2",
			RemoteVirtualNetwork: &azVNetRef{ID: "/subscriptions/11111111-1111-1111-1111-111111111111/.../virtualNetworks/vnet2"},
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
			ID:                   "/sub/subnet1",
			Name:                 "subnet1",
			NetworkSecurityGroup: &azNSGRef{ID: "/sub/nsg1"},
		},
		{
			ID:   "/sub/subnet2",
			Name: "subnet2",
			// No NSG.
		},
	}

	subnetIDMap := map[string]string{
		"/sub/subnet1": "azure::/sub/subnet1",
		"/sub/subnet2": "azure::/sub/subnet2",
	}
	nsgIDMap := map[string]string{
		"/sub/nsg1": "azure::/sub/nsg1",
	}

	conns := TransformSubnetNSGConnections(subnets, subnetIDMap, nsgIDMap)

	if len(conns) != 1 {
		t.Fatalf("expected 1 connection (only subnet with NSG), got %d", len(conns))
	}
	if conns[0].Type != "network" {
		t.Errorf("expected type network.policy, got %q", conns[0].Type)
	}
	if conns[0].Direction != "forward" {
		t.Errorf("expected forward direction, got %q", conns[0].Direction)
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
			ID:       "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1",
			Name:     "rg1",
			Location: "westeurope",
		},
		{
			ID:       "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg2",
			Name:     "rg2",
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
	if groups[0].Name != "rg1" {
		t.Errorf("expected name rg1, got %q", groups[0].Name)
	}

	// Verify case-insensitive name lookup.
	if _, ok := nameToID["rg1"]; !ok {
		t.Error("expected rg1 in nameToID map")
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
			armID:    "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/my-rg/providers/Microsoft.Network/virtualNetworks/vnet1",
			expected: "my-rg",
		},
		{
			armID:    "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/MyRG",
			expected: "MyRG",
		},
		{
			armID:    "/subscriptions/xxx",
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
		{"VM starting", "unknown"},
		{"", "unknown"},
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
	armID := "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Network/virtualNetworks/vnet1"
	id := resourceID("network.vpc", armID)

	// Per OSIRIS Producer Guidelines chapter 2 section 2.2.1: hyperscaler IDs use provider::native-id.
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
	armID2 := "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Network/virtualNetworks/vnet2"
	id3 := resourceID("network.vpc", armID2)
	if id == id3 {
		t.Errorf("different ARM IDs should produce different resource IDs")
	}
}

func TestTransformSubnetToVNetConnections(t *testing.T) {
	subnets := []Subnet{
		{
			ID:   "/sub/vnet1/subnets/subnet1",
			Name: "subnet1",
			// VNetID() derives "/sub/vnet1" from the subnet ARM ID.
		},
		{
			ID:   "/sub/nosubnets",
			Name: "subnet2",
			// No /subnets/ in ID - VNetID() returns "" - should be skipped.
		},
	}
	subnetIDMap := map[string]string{
		"/sub/vnet1/subnets/subnet1": "azure::/sub/vnet1/subnets/subnet1",
		"/sub/nosubnets":             "azure::/sub/nosubnets",
	}
	vnetIDMap := map[string]string{
		"/sub/vnet1": "azure::/sub/vnet1",
	}

	conns := TransformSubnetToVNetConnections(subnets, subnetIDMap, vnetIDMap)

	if len(conns) != 1 {
		t.Fatalf("expected 1 connection (only subnet with VNet), got %d", len(conns))
	}
	if conns[0].Type != "contains" {
		t.Errorf("expected type network.membership, got %q", conns[0].Type)
	}
	if conns[0].Source != "azure::/sub/vnet1/subnets/subnet1" {
		t.Errorf("expected source to be subnet, got %q", conns[0].Source)
	}
	if conns[0].Target != "azure::/sub/vnet1" {
		t.Errorf("expected target to be vnet, got %q", conns[0].Target)
	}
}

func TestTransformNICToSubnetConnections(t *testing.T) {
	nics := []NetworkInterface{
		{
			ID:   "/sub/nic1",
			Name: "nic1",
			IPConfigurations: []IPConfiguration{
				{Subnet: &azSubnetRef{ID: "/sub/subnet1"}, PrivateIPAddress: "10.0.1.4"},
				{Subnet: &azSubnetRef{ID: "/sub/subnet1"}, PrivateIPAddress: "10.0.1.5"}, // same subnet - should deduplicate.
			},
		},
	}
	nicIDMap := map[string]string{"/sub/nic1": "azure::/sub/nic1"}
	subnetIDMap := map[string]string{"/sub/subnet1": "azure::/sub/subnet1"}

	conns := TransformNICToSubnetConnections(nics, nicIDMap, subnetIDMap)

	if len(conns) != 1 {
		t.Fatalf("expected 1 connection (deduplicated), got %d", len(conns))
	}
	if conns[0].Type != "network" {
		t.Errorf("expected type network, got %q", conns[0].Type)
	}
	if conns[0].Properties["private_ip"] != "10.0.1.4" {
		t.Errorf("expected private_ip 10.0.1.4, got %v", conns[0].Properties["private_ip"])
	}
}

func TestTransformPrivateEndpointToSubnetConnections(t *testing.T) {
	pes := []PrivateEndpoint{
		{ID: "/sub/pe1", Name: "pe1", Subnet: &azSubnetRef{ID: "/sub/subnet1"}},
		{ID: "/sub/pe2", Name: "pe2"}, // no subnet - skipped.
	}
	subnetIDMap := map[string]string{"/sub/subnet1": "azure::/sub/subnet1"}

	conns := TransformPrivateEndpointToSubnetConnections(pes, subnetIDMap)

	if len(conns) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(conns))
	}
}

func TestTransformVNetGatewayToSubnetConnections(t *testing.T) {
	gws := []VNetGateway{
		{
			ID:   "/sub/gw1",
			Name: "gw1",
			IPConfigurations: []GatewayIPConfig{
				{Subnet: &azSubnetRef{ID: "/sub/vnet1/subnets/GatewaySubnet"}, PublicIPAddress: &azPublicIPRef{ID: "/sub/pip1"}},
			},
		},
	}
	subnetIDMap := map[string]string{"/sub/vnet1/subnets/GatewaySubnet": "azure::/sub/vnet1/subnets/GatewaySubnet"}

	conns := TransformVNetGatewayToSubnetConnections(gws, subnetIDMap)

	if len(conns) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(conns))
	}
	if conns[0].Source != "azure::/sub/gw1" {
		t.Errorf("expected source to be gateway, got %q", conns[0].Source)
	}
	if conns[0].Target != "azure::/sub/vnet1/subnets/GatewaySubnet" {
		t.Errorf("expected target to be GatewaySubnet, got %q", conns[0].Target)
	}
}

func TestTransformPrivateDNSToVNetConnections(t *testing.T) {
	zones := []PrivateDNSZone{
		{
			ID:   "/sub/pdns1",
			Name: "privatelink.blob.core.windows.net",
			Links: []PrivateDNSLink{
				{Name: "vnetlink1", VirtualNetwork: &azVNetRef{ID: "/sub/vnet1"}, RegistrationEnabled: true},
				{Name: "vnetlink2", VirtualNetwork: &azVNetRef{ID: "/sub/vnet-unknown"}}, // not in map.
			},
		},
	}
	vnetIDMap := map[string]string{"/sub/vnet1": "azure::/sub/vnet1"}

	conns := TransformPrivateDNSToVNetConnections(zones, vnetIDMap)

	if len(conns) != 1 {
		t.Fatalf("expected 1 connection (only known VNet), got %d", len(conns))
	}
	if conns[0].Type != "network" {
		t.Errorf("expected type network, got %q", conns[0].Type)
	}
	if conns[0].Properties["registration_enabled"] != true {
		t.Errorf("expected registration_enabled=true")
	}
}

func TestExtractLastSegment(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Network/virtualNetworks/vnet1", "vnet1"},
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
		{ID: "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1", Name: "rg1", Location: "westeurope"},
	}
	groups, nameToID := TransformResourceGroupGroups(rgs, testSub)

	vnets := []VirtualNetwork{
		{
			ID:            "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Network/virtualNetworks/vnet1",
			Name:          "vnet1",
			Location:      "westeurope",
			ResourceGroup: "rg1",
		},
	}
	resources := TransformVNets(vnets, nil, testSub)

	WireResourcesToResourceGroups(resources, nameToID, groups)

	if len(groups[0].Members) != 1 {
		t.Fatalf("expected 1 member in rg1 group, got %d", len(groups[0].Members))
	}
	if groups[0].Members[0] != resources[0].ID {
		t.Errorf("expected member %q, got %q", resources[0].ID, groups[0].Members[0])
	}
}

func TestWireResourceGroupsToSubscription(t *testing.T) {
	rgs := []ResourceGroup{
		{ID: "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1", Name: "rg1", Location: "westeurope"},
		{ID: "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg2", Name: "rg2", Location: "eastus"},
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
			ID:            "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Web/serverfarms/plan1",
			Name:          "plan1",
			Location:      "westeurope",
			ResourceGroup: "rg1",
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
	if idMap[plans[0].ID] != r.ID {
		t.Errorf("idMap mismatch: got %q, want %q", idMap[plans[0].ID], r.ID)
	}
}

func TestTransformWebApps(t *testing.T) {
	apps := []WebApp{
		{
			ID:                  "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Web/sites/site1",
			Name:                "site1",
			Location:            "westeurope",
			ResourceGroup:       "rg1",
			Kind:                "app,linux,container",
			State:               "Running",
			Enabled:             true,
			DefaultHostName:     "site1.azurewebsites.net",
			HostNames:           []string{"site1.azurewebsites.net"},
			HTTPSOnly:           true,
			ServerFarmID:        "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Web/serverfarms/plan1",
			OutboundIPAddresses: "20.0.0.1,20.0.0.2,20.0.0.3",
			Identity: &azSiteIdentity{
				Type:        "SystemAssigned",
				PrincipalID: "00000000-0000-0000-0000-000000000001",
			},
			OutboundVnetRouting: &azOutboundVnetRouting{AllTraffic: true},
			Tags: map[string]string{
				"hidden-link: /app-insights-resource-id": "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Insights/components/ai1",
			},
		},
		{
			ID:            "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Web/sites/fn1",
			Name:          "fn1",
			Location:      "westeurope",
			ResourceGroup: "rg1",
			Kind:          "functionapp,linux",
			State:         "Stopped",
			Enabled:       false,
		},
	}

	resources, idMap := TransformWebApps(apps, testSub)

	if len(resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(resources))
	}
	if resources[0].Type != "osiris.azure.webapp" {
		t.Errorf("expected first site osiris.azure.webapp, got %q", resources[0].Type)
	}
	if resources[0].Status != "active" {
		t.Errorf("expected first site status active, got %q", resources[0].Status)
	}
	if ips, _ := resources[0].Properties["outbound_ips"].([]string); len(ips) != 3 {
		t.Errorf("expected 3 outbound ips, got %v", resources[0].Properties["outbound_ips"])
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

	if resources[1].Type != "osiris.azure.functionapp" {
		t.Errorf("expected second site osiris.azure.functionapp (kind routing), got %q", resources[1].Type)
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
			ID:            "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Network/applicationSecurityGroups/asg1",
			Name:          "asg1",
			Location:      "westeurope",
			ResourceGroup: "rg1",
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
	if idMap[asgs[0].ID] != r.ID {
		t.Errorf("idMap mismatch: got %q, want %q", idMap[asgs[0].ID], r.ID)
	}
}

func TestTransformWebAppToPlanConnections(t *testing.T) {
	planID := "/sub/plan1"
	planID2 := "/sub/plan2"
	siteID := "/sub/site1"
	siteID2 := "/sub/site2"
	apps := []WebApp{
		{ID: siteID, Name: "site1", ServerFarmID: planID},
		// az webapp list flattens serverFarmId -> appServicePlanId; accept either.
		{ID: siteID2, Name: "site2", AppServicePlanID: planID2},
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
		{ID: "/sub/site1", Name: "site1", VirtualNetworkSubnetID: "/sub/subnet1"},
		{ID: "/sub/nosubnet", Name: "nosubnet"},
	}
	webAppIDMap := map[string]string{"/sub/site1": "azure::/sub/site1", "/sub/nosubnet": "azure::/sub/nosubnet"}
	subnetIDMap := map[string]string{"/sub/subnet1": "azure::/sub/subnet1"}

	conns := TransformWebAppToSubnetConnections(apps, webAppIDMap, subnetIDMap)
	if len(conns) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(conns))
	}
	if conns[0].Type != "network" {
		t.Errorf("expected type network, got %q", conns[0].Type)
	}
}

func TestTransformPEToWebAppConnections(t *testing.T) {
	apps := []WebApp{
		{
			ID:   "/sub/site1",
			Name: "site1",
			PrivateEndpointConnections: []azPrivateEndpointConnRef{
				{Properties: struct {
					PrivateEndpoint struct {
						ID string `json:"id"`
					} `json:"privateEndpoint"`
				}{PrivateEndpoint: struct {
					ID string `json:"id"`
				}{ID: "/sub/pe1"}}},
			},
		},
	}
	webAppIDMap := map[string]string{"/sub/site1": "azure::/sub/site1"}
	peIDMap := map[string]string{"/sub/pe1": "azure::/sub/pe1"}

	conns := TransformPEToWebAppConnections(apps, webAppIDMap, peIDMap)
	if len(conns) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(conns))
	}
	if conns[0].Source != "azure::/sub/pe1" {
		t.Errorf("expected source PE, got %q", conns[0].Source)
	}
	if conns[0].Target != "azure::/sub/site1" {
		t.Errorf("expected target site, got %q", conns[0].Target)
	}
	if conns[0].Type != "dependency" {
		t.Errorf("expected PE->WebApp type 'dependency' per spec §5.2.3, got %q", conns[0].Type)
	}
}

func TestTransformNICToASGConnections(t *testing.T) {
	nics := []NetworkInterface{
		{
			ID:   "/sub/nic1",
			Name: "nic1",
			IPConfigurations: []IPConfiguration{
				{
					ApplicationSecurityGroups: []azASGRef{
						{ID: "/sub/asg1"},
						{ID: "/sub/asg1"}, // duplicate ASG on same NIC -> dedup.
					},
				},
			},
		},
	}
	nicIDMap := map[string]string{"/sub/nic1": "azure::/sub/nic1"}
	asgIDMap := map[string]string{"/sub/asg1": "azure::/sub/asg1"}

	conns := TransformNICToASGConnections(nics, nicIDMap, asgIDMap)
	if len(conns) != 1 {
		t.Fatalf("expected 1 deduplicated connection, got %d", len(conns))
	}
	if conns[0].Type != "network" {
		t.Errorf("expected type network, got %q", conns[0].Type)
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
		{"", "unknown"},
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
		"hidden-link: /app-insights-resource-id": "/subs/xxx/ai1",
		"other":                                  "value",
	}
	if got := appInsightsFromTags(tags); got != "/subs/xxx/ai1" {
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
			ID:                     "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Storage/storageAccounts/stg1",
			Name:                   "stg1",
			Location:               "westeurope",
			ResourceGroup:          "rg1",
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
				Blob: "https://stg1.blob.core.windows.net/",
				File: "https://stg1.file.core.windows.net/",
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
	if idMap[accts[0].ID] != r.ID {
		t.Errorf("idMap mismatch")
	}
}

func TestTransformKeyVaults(t *testing.T) {
	purge := true
	softDel := true
	vaults := []KeyVault{
		{
			ID:            "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.KeyVault/vaults/kv1",
			Name:          "kv1",
			Location:      "westeurope",
			ResourceGroup: "rg1",
			Properties: &KeyVaultProperties{
				SKU:                       azKeyVaultSKU{Family: "A", Name: "standard"},
				TenantID:                  "f1e2d3c4-b5a6-9078-fedc-ba9876543210",
				VaultURI:                  "https://kv1.vault.azure.net/",
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
					VirtualNetworkRules: []azKeyVaultVNetRule{{ID: "/subs/s/vnet/subnet1"}},
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
	if r.Properties["rbac_authorization"] != true {
		t.Errorf("expected RBAC, got %v", r.Properties["rbac_authorization"])
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
}

func TestTransformContainerRegistries(t *testing.T) {
	regs := []ContainerRegistry{
		{
			ID:                   "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.ContainerRegistry/registries/acr1",
			Name:                 "acr1",
			Location:             "westeurope",
			ResourceGroup:        "rg1",
			SKU:                  azACRSKU{Name: "Premium", Tier: "Premium"},
			LoginServer:          "acr1.azurecr.io",
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
	if r.Properties["login_server"] != "acr1.azurecr.io" {
		t.Errorf("expected login_server")
	}
	if r.Properties["zone_redundant"] != true {
		t.Errorf("expected zone_redundant=true when ZoneRedundancy=Enabled")
	}
}

func TestTransformManagedIdentities(t *testing.T) {
	ids := []ManagedIdentity{
		{
			ID:            "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.ManagedIdentity/userAssignedIdentities/mi1",
			Name:          "mi1",
			Location:      "westeurope",
			ResourceGroup: "rg1",
			PrincipalID:   "00000000-0000-0000-0000-000000000001",
			ClientID:      "00000000-0000-0000-0000-000000000002",
			TenantID:      "f1e2d3c4-b5a6-9078-fedc-ba9876543210",
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
	ext, _ := r.Extensions[extensionNamespace].(map[string]any)
	if ext == nil || ext["principal_id"] != "00000000-0000-0000-0000-000000000001" {
		t.Errorf("expected principal_id extension")
	}
	if idMap[ids[0].ID] != r.ID {
		t.Errorf("idMap mismatch")
	}
}

func TestTransformDisks(t *testing.T) {
	disks := []Disk{
		{
			ID:                "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Compute/disks/disk1",
			Name:              "disk1",
			Location:          "westeurope",
			ResourceGroup:     "rg1",
			SKU:               azDiskSKU{Name: "Premium_LRS", Tier: "Premium"},
			DiskSizeGB:        128,
			DiskIOPSReadWrite: 500,
			DiskMBPSReadWrite: 100,
			DiskState:         "Attached",
			OSType:            "Linux",
			ManagedBy:         "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Compute/virtualMachines/vm1",
			ProvisioningState: "Succeeded",
			Zones:             []string{"1"},
		},
		{
			ID:                "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Compute/disks/disk2",
			Name:              "disk2",
			Location:          "westeurope",
			ResourceGroup:     "rg1",
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
			ID:            "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Compute/snapshots/snap1",
			Name:          "snap1",
			Location:      "westeurope",
			ResourceGroup: "rg1",
			SKU:           azDiskSKU{Name: "Standard_LRS"},
			DiskSizeGB:    128,
			Incremental:   true,
			OSType:        "Linux",
			CreationData: &azDiskCreationData{
				CreateOption:     "Copy",
				SourceResourceID: "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Compute/disks/disk1",
			},
			ProvisioningState: "Succeeded",
		},
	}

	resources, _ := TransformSnapshots(snaps, testSub)
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	r := resources[0]
	if r.Type != "osiris.azure.snapshot" {
		t.Errorf("expected type osiris.azure.snapshot, got %q", r.Type)
	}
	if r.Properties["incremental"] != true {
		t.Errorf("expected incremental=true")
	}
	if r.Properties["source_resource_id"] == "" {
		t.Errorf("expected source_resource_id set")
	}
}

func TestTransformSnapshotToDiskConnections(t *testing.T) {
	snaps := []Snapshot{
		{
			ID:           "/sub/snap1",
			Name:         "snap1",
			CreationData: &azDiskCreationData{SourceResourceID: "/sub/disk1"},
		},
		{
			ID: "/sub/snap2", Name: "snap2", // no creation data -> skipped.
		},
	}
	snapshotIDMap := map[string]string{"/sub/snap1": "azure::/sub/snap1", "/sub/snap2": "azure::/sub/snap2"}
	diskIDMap := map[string]string{"/sub/disk1": "azure::/sub/disk1"}

	conns := TransformSnapshotToDiskConnections(snaps, snapshotIDMap, diskIDMap)
	if len(conns) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(conns))
	}
	if conns[0].Type != "contains" {
		t.Errorf("expected contains, got %q", conns[0].Type)
	}
	if conns[0].Direction != "reverse" {
		t.Errorf("expected reverse direction, got %q", conns[0].Direction)
	}
}

func TestTransformDiskToVMConnections(t *testing.T) {
	disks := []Disk{
		{ID: "/sub/disk1", Name: "disk1", ManagedBy: "/sub/vm1"},
		{ID: "/sub/disk2", Name: "disk2"}, // unattached -> skipped.
	}
	diskIDMap := map[string]string{"/sub/disk1": "azure::/sub/disk1", "/sub/disk2": "azure::/sub/disk2"}
	vmIDMap := map[string]string{"/sub/vm1": "azure::/sub/vm1"}

	conns := TransformDiskToVMConnections(disks, diskIDMap, vmIDMap)
	if len(conns) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(conns))
	}
	if conns[0].Type != "contains" {
		t.Errorf("expected contains, got %q", conns[0].Type)
	}
	if conns[0].Direction != "reverse" {
		t.Errorf("expected reverse direction, got %q", conns[0].Direction)
	}
}

func TestTransformPEToStorageConnections(t *testing.T) {
	accts := []StorageAccount{
		{
			ID:   "/sub/stg1",
			Name: "stg1",
			PrivateEndpointConnections: []azPrivateEndpointConnRef{
				{Properties: struct {
					PrivateEndpoint struct {
						ID string `json:"id"`
					} `json:"privateEndpoint"`
				}{PrivateEndpoint: struct {
					ID string `json:"id"`
				}{ID: "/sub/pe1"}}},
			},
		},
	}
	storageIDMap := map[string]string{"/sub/stg1": "azure::/sub/stg1"}
	peIDMap := map[string]string{"/sub/pe1": "azure::/sub/pe1"}

	conns := TransformPEToStorageConnections(accts, storageIDMap, peIDMap)
	if len(conns) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(conns))
	}
	if conns[0].Source != "azure::/sub/pe1" || conns[0].Target != "azure::/sub/stg1" {
		t.Errorf("unexpected source/target: %q -> %q", conns[0].Source, conns[0].Target)
	}
	if conns[0].Type != "dependency.storage" {
		t.Errorf("expected PE->Storage type 'dependency.storage' per OSIRIS JSON spec section 5.2.3, got %q", conns[0].Type)
	}
}

func TestMapProvisioningState(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"Succeeded", "active"},
		{"", "active"},
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
	wsArm := "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.OperationalInsights/workspaces/ws1"
	comps := []ApplicationInsights{
		{
			ID:            "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/microsoft.insights/components/app1",
			Name:          "app1",
			Location:      "westeurope",
			ResourceGroup: "rg1",
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
			ID:            "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/microsoft.insights/components/classic1",
			Name:          "classic1",
			Location:      "westeurope",
			ResourceGroup: "rg1",
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
	if r.Properties["disable_local_auth"] != true {
		t.Errorf("expected disable_local_auth=true")
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
	// Classic App Insights (no Properties): fallback status=active and no workspace extension.
	rc := resources[1]
	if rc.Status != "active" {
		t.Errorf("classic AI should default to active when properties absent, got %q", rc.Status)
	}
	if rc.Extensions != nil {
		t.Errorf("classic AI should have no workspace extension")
	}
	if len(idMap) != 2 {
		t.Errorf("expected 2 idMap entries, got %d", len(idMap))
	}
}

func TestTransformLogAnalyticsWorkspaces(t *testing.T) {
	wss := []LogAnalyticsWorkspace{
		{
			ID:                              "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.OperationalInsights/workspaces/ws1",
			Name:                            "ws1",
			Location:                        "westeurope",
			ResourceGroup:                   "rg1",
			CustomerID:                      "11111111-2222-3333-4444-555555555555",
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
	if ext == nil || ext["customer_id"] != "11111111-2222-3333-4444-555555555555" {
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
	if idMap[wss[0].ID] != r.ID {
		t.Errorf("idMap mismatch")
	}
}

func TestTransformAppInsightsToWorkspaceConnections(t *testing.T) {
	aiArm := "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/microsoft.insights/components/app1"
	wsArm := "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.OperationalInsights/workspaces/ws1"
	comps := []ApplicationInsights{
		{
			ID:         aiArm,
			Name:       "app1",
			Properties: &AppInsightsProperties{WorkspaceResourceID: wsArm},
		},
		{
			// Classic AI with no workspace binding - should not emit an edge.
			ID:         "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/microsoft.insights/components/classic1",
			Name:       "classic1",
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
	if c.Type != "network" {
		t.Errorf("expected type network, got %q", c.Type)
	}
	if c.Source != "ai-resource-id" || c.Target != "la-resource-id" {
		t.Errorf("source/target mismatch: %+v", c)
	}
}

func TestTransformRecoveryServicesVaults(t *testing.T) {
	peArm := "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Network/privateEndpoints/pe1"
	vaults := []RecoveryServicesVault{
		{
			ID:            "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.RecoveryServices/vaults/rsv1",
			Name:          "rsv1",
			Location:      "westeurope",
			ResourceGroup: "rg1",
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
	if idMap[vaults[0].ID] != r.ID {
		t.Errorf("idMap mismatch")
	}
}

func TestTransformBackupVaults(t *testing.T) {
	vaults := []BackupVault{
		{
			ID:            "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.DataProtection/backupVaults/bv1",
			Name:          "bv1",
			Location:      "westeurope",
			ResourceGroup: "rg1",
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
	if idMap[vaults[0].ID] != r.ID {
		t.Errorf("idMap mismatch")
	}
}

func TestTransformBackupProtectedItemConnections(t *testing.T) {
	vmArm := "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Compute/virtualMachines/vm1"
	vaultArm := "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.RecoveryServices/vaults/rsv1"
	vaults := []RecoveryServicesVault{
		{
			ID:   vaultArm,
			Name: "rsv1",
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
						SourceResourceID: "/subscriptions/other/resourceGroups/rgZ/providers/Microsoft.Compute/virtualMachines/ghost",
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
	if c.Type != "network" {
		t.Errorf("expected type network, got %q", c.Type)
	}
}

func TestTransformBackupInstanceConnections(t *testing.T) {
	diskArm := "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Compute/disks/disk1"
	vaultArm := "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.DataProtection/backupVaults/bv1"
	vaults := []BackupVault{
		{
			ID:   vaultArm,
			Name: "bv1",
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
	aiArm := "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/microsoft.insights/components/app1"
	webApps := []WebApp{
		{
			ID:   "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Web/sites/site1",
			Name: "site1",
			Tags: map[string]string{
				"hidden-link: /app-insights-resource-id": aiArm,
			},
		},
		{
			// No hidden-link tag -> no edge.
			ID:   "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Web/sites/site2",
			Name: "site2",
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
	peArm := "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Network/privateEndpoints/pe-sql"
	servers := []SQLServer{
		{
			ID:            "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Sql/servers/sqlsrv1",
			Name:          "sqlsrv1",
			Location:      "westeurope",
			ResourceGroup: "rg1",
			Properties: &azSQLServerProperties{
				Version:                  "12.0",
				AdministratorLogin:       "sqladmin",
				FullyQualifiedDomainName: "sqlsrv1.database.windows.net",
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
				{Name: "db1"},
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
	if idMap[servers[0].ID] != r.ID {
		t.Errorf("idMap mismatch")
	}
}

func TestTransformSQLDatabases(t *testing.T) {
	servers := []SQLServer{
		{
			ID:   "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Sql/servers/sqlsrv1",
			Name: "sqlsrv1",
			Databases: []SQLDatabase{
				{
					ID:            "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Sql/servers/sqlsrv1/databases/db1",
					Name:          "db1",
					Location:      "westeurope",
					ResourceGroup: "rg1",
					ServerID:      "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Sql/servers/sqlsrv1",
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
	if r.Type != "osiris.azure.sqldatabase" {
		t.Errorf("expected type osiris.azure.sqldatabase, got %q", r.Type)
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
	if r.Properties["server_name"] != "sqlsrv1" {
		t.Errorf("expected server_name=sqlsrv1")
	}
	if idMap[servers[0].Databases[0].ID] != r.ID {
		t.Errorf("idMap mismatch")
	}
}

func TestTransformSQLServerContainsDatabaseConnections(t *testing.T) {
	srvArm := "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Sql/servers/sqlsrv1"
	dbArm := "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Sql/servers/sqlsrv1/databases/db1"
	servers := []SQLServer{
		{ID: srvArm, Name: "sqlsrv1", Databases: []SQLDatabase{{ID: dbArm, Name: "db1", ServerID: srvArm}}},
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
	subnetArm := "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Network/virtualNetworks/vnet1/subnets/pgsubnet"
	servers := []PostgreSQLServer{
		{
			ID:            "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.DBforPostgreSQL/flexibleServers/pg1",
			Name:          "pg1",
			Location:      "westeurope",
			ResourceGroup: "rg1",
			SKU:           &azFlexServerSKU{Name: "Standard_D2ds_v4", Tier: "GeneralPurpose"},
			Properties: &azFlexServerProperties{
				Version:                  "15",
				AdministratorLogin:       "pgadmin",
				FullyQualifiedDomainName: "pg1.postgres.database.azure.com",
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
	if r.Type != "osiris.azure.postgresqlserver" {
		t.Errorf("expected type osiris.azure.postgresqlserver, got %q", r.Type)
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
	if idMap[servers[0].ID] != r.ID {
		t.Errorf("idMap mismatch")
	}
}

func TestTransformMySQLServers(t *testing.T) {
	servers := []MySQLServer{
		{
			ID:            "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.DBforMySQL/flexibleServers/mysql1",
			Name:          "mysql1",
			Location:      "westeurope",
			ResourceGroup: "rg1",
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
	if resources[0].Type != "osiris.azure.mysqlserver" {
		t.Errorf("expected type osiris.azure.mysqlserver, got %q", resources[0].Type)
	}
	if resources[0].Properties["tier"] != "Burstable" {
		t.Errorf("expected tier=Burstable")
	}
	if resources[0].Properties["public_network_access"] != "Enabled" {
		t.Errorf("expected public_network_access=Enabled")
	}
}

func TestTransformCosmosAccounts(t *testing.T) {
	peArm := "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Network/privateEndpoints/pe-cosmos"
	accts := []CosmosAccount{
		{
			ID:            "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.DocumentDB/databaseAccounts/cosmos1",
			Name:          "cosmos1",
			Location:      "westeurope",
			ResourceGroup: "rg1",
			Kind:          "GlobalDocumentDB",
			Properties: &azCosmosProperties{
				ProvisioningState:             "Succeeded",
				DatabaseAccountOfferType:      "Standard",
				DocumentEndpoint:              "https://cosmos1.documents.azure.com:443/",
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
	if r.Type != "osiris.azure.cosmosaccount" {
		t.Errorf("expected type osiris.azure.cosmosaccount, got %q", r.Type)
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
	if idMap[accts[0].ID] != r.ID {
		t.Errorf("idMap mismatch")
	}
}

func TestTransformRedisCaches(t *testing.T) {
	peArm := "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Network/privateEndpoints/pe-redis"
	subnetArm := "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Network/virtualNetworks/vnet1/subnets/redis"
	caches := []RedisCache{
		{
			ID:                  "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Cache/Redis/redis1",
			Name:                "redis1",
			Location:            "westeurope",
			ResourceGroup:       "rg1",
			SKU:                 &azRedisSKU{Name: "Premium", Family: "P", Capacity: 1},
			RedisVersion:        "6.0",
			ProvisioningState:   "Succeeded",
			EnableNonSSLPort:    false,
			MinimumTLSVersion:   "1.2",
			PublicNetworkAccess: "Disabled",
			HostName:            "redis1.redis.cache.windows.net",
			Port:                6379,
			SSLPort:             6380,
			ShardCount:          3,
			SubnetID:            subnetArm,
			StaticIP:            "10.0.0.5",
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
	if r.Type != "osiris.azure.redis" {
		t.Errorf("expected type osiris.azure.redis, got %q", r.Type)
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
	if idMap[caches[0].ID] != r.ID {
		t.Errorf("idMap mismatch")
	}
}

func TestTransformFlexServerToSubnetConnections(t *testing.T) {
	subnetArm := "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Network/virtualNetworks/vnet1/subnets/pgsubnet"
	pgs := []PostgreSQLServer{
		{
			ID:   "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.DBforPostgreSQL/flexibleServers/pg1",
			Name: "pg1",
			Properties: &azFlexServerProperties{
				Network: &azFlexServerNetwork{DelegatedSubnetResourceID: subnetArm},
			},
		},
		// Public-access server: no delegated subnet; must be skipped.
		{
			ID:   "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.DBforPostgreSQL/flexibleServers/pg-public",
			Name: "pg-public",
			Properties: &azFlexServerProperties{
				Network: &azFlexServerNetwork{PublicNetworkAccess: "Enabled"},
			},
		},
	}
	mys := []MySQLServer{
		{
			ID:   "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.DBforMySQL/flexibleServers/mysql1",
			Name: "mysql1",
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
	subnetArm := "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Network/virtualNetworks/vnet1/subnets/redis"
	caches := []RedisCache{
		{ID: "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Cache/Redis/redis1", Name: "redis1", SubnetID: subnetArm},
		// Basic tier, no VNet injection: must be skipped.
		{ID: "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Cache/Redis/redis-basic", Name: "redis-basic"},
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
	peArm := "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Network/privateEndpoints/pe-aks"
	clusters := []AKSCluster{
		{
			ID:            "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.ContainerService/managedClusters/aks1",
			Name:          "aks1",
			Location:      "westeurope",
			ResourceGroup: "rg1",
			SKU:           &azAKSSKU{Name: "Base", Tier: "Standard"},
			Properties: &azAKSProperties{
				KubernetesVersion: "1.29.0",
				DNSPrefix:         "aks1-dns",
				FQDN:              "aks1.hcp.westeurope.azmk8s.io",
				EnableRBAC:        true,
				ProvisioningState: "Succeeded",
				NodeResourceGroup: "MC_rg1_aks1_westeurope",
				NetworkProfile: &azAKSNetworkProfile{
					NetworkPlugin:   "azure",
					NetworkPolicy:   "calico",
					ServiceCIDR:     "10.0.0.0/16",
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
	if r.Type != "osiris.azure.aks.cluster" {
		t.Errorf("expected type osiris.azure.aks.cluster, got %q", r.Type)
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
	if idMap[clusters[0].ID] != r.ID {
		t.Errorf("idMap mismatch")
	}
}

func TestTransformAKSAgentPools(t *testing.T) {
	subnetArm := "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Network/virtualNetworks/vnet1/subnets/aks"
	clusters := []AKSCluster{
		{
			ID:       "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.ContainerService/managedClusters/aks1",
			Name:     "aks1",
			Location: "westeurope",
			AgentPools: []AKSAgentPool{
				{
					ID:                "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.ContainerService/managedClusters/aks1/agentPools/nodepool1",
					Name:              "nodepool1",
					VMSize:            "Standard_DS2_v2",
					Count:             3,
					EnableAutoScaling: true, MinCount: 1, MaxCount: 5,
					OSType: "Linux", OSSKU: "Ubuntu", Mode: "System",
					OrchestratorVer:   "1.29.0",
					VNetSubnetID:      subnetArm,
					ProvisioningState: "Succeeded",
					ClusterID:         "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.ContainerService/managedClusters/aks1",
					ClusterName:       "aks1",
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
	if idMap[clusters[0].AgentPools[0].ID] != r.ID {
		t.Errorf("idMap mismatch")
	}
}

func TestTransformContainerAppEnvironments(t *testing.T) {
	subnetArm := "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Network/virtualNetworks/vnet1/subnets/ace"
	envs := []ContainerAppEnvironment{
		{
			ID:            "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.App/managedEnvironments/env1",
			Name:          "env1",
			Location:      "westeurope",
			ResourceGroup: "rg1",
			Properties: &azContainerEnvProperties{
				ProvisioningState: "Succeeded",
				DefaultDomain:     "proudforest-abc.westeurope.azurecontainerapps.io",
				StaticIP:          "10.0.0.4",
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
	if idMap[envs[0].ID] != r.ID {
		t.Errorf("idMap mismatch")
	}
}

func TestTransformContainerApps(t *testing.T) {
	envArm := "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.App/managedEnvironments/env1"
	apps := []ContainerApp{
		// Uses environmentId.
		{
			ID:            "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.App/containerApps/app1",
			Name:          "app1",
			Location:      "westeurope",
			ResourceGroup: "rg1",
			Properties: &azContainerAppProperties{
				ProvisioningState:   "Succeeded",
				EnvironmentID:       envArm,
				LatestRevisionFQDN:  "app1.proudforest.westeurope.azurecontainerapps.io",
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
			ID:   "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.App/containerApps/app2",
			Name: "app2",
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
		t.Errorf("expected app1 environment_id=%s", envArm)
	}
	if resources[1].Properties["environment_id"] != envArm {
		t.Errorf("expected app2 environment_id=%s (fallback)", envArm)
	}
	ing, ok := resources[0].Properties["ingress"].(map[string]any)
	if !ok {
		t.Fatalf("expected ingress map on app1")
	}
	if ing["external"] != true || ing["target_port"] != 8080 {
		t.Errorf("ingress fields wrong: %+v", ing)
	}
}

func TestTransformContainerGroups(t *testing.T) {
	subnetArm := "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Network/virtualNetworks/vnet1/subnets/aci"
	groups := []ContainerGroup{
		{
			ID:            "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.ContainerInstance/containerGroups/cg1",
			Name:          "cg1",
			Location:      "westeurope",
			ResourceGroup: "rg1",
			Properties: &azContainerGroupProperties{
				ProvisioningState: "Succeeded",
				OSType:            "Linux",
				RestartPolicy:     "Always",
				Sku:               "Standard",
				IPAddress: &azContainerGroupIPAddress{
					IP: "10.0.0.5", Type: "Private", FQDN: "cg1.westeurope.azurecontainer.io",
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
	if r.Type != "osiris.azure.containergroup" {
		t.Errorf("expected type osiris.azure.containergroup, got %q", r.Type)
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
	peArm := "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Network/privateEndpoints/pe-sb"
	namespaces := []ServiceBusNamespace{
		{
			ID:            "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.ServiceBus/namespaces/sb1",
			Name:          "sb1",
			Location:      "westeurope",
			ResourceGroup: "rg1",
			SKU:           &azMessagingSKU{Name: "Premium", Tier: "Premium", Capacity: 1},
			Properties: &azMessagingProperties{
				ProvisioningState:   "Succeeded",
				ServiceBusEndpoint:  "https://sb1.servicebus.windows.net:443/",
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
	if r.Type != "osiris.azure.servicebus.namespace" {
		t.Errorf("expected type osiris.azure.servicebus.namespace, got %q", r.Type)
	}
	if r.Properties["sku_name"] != "Premium" {
		t.Errorf("expected sku_name=Premium")
	}
	if r.Properties["zone_redundant"] != true {
		t.Errorf("expected zone_redundant=true")
	}
	if r.Properties["disable_local_auth"] != true {
		t.Errorf("expected disable_local_auth=true")
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
	subnetArm := "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Network/virtualNetworks/vnet1/subnets/apim"
	services := []APIMService{
		{
			ID:            "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.ApiManagement/service/apim1",
			Name:          "apim1",
			Location:      "westeurope",
			ResourceGroup: "rg1",
			SKU:           &azAPIMSKU{Name: "Developer", Capacity: 1},
			Properties: &azAPIMProperties{
				ProvisioningState:  "Succeeded",
				GatewayURL:         "https://apim1.azure-api.net",
				PortalURL:          "https://apim1.developer.azure-api.net",
				ManagementURL:      "https://apim1.management.azure-api.net",
				VirtualNetworkType: "Internal",
				VirtualNetworkConfiguration: &azAPIMVirtualNetworkConfig{
					SubnetResourceID: subnetArm,
				},
				PublicIPAddresses:  []string{"20.1.2.3"},
				PrivateIPAddresses: []string{"10.0.0.4"},
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
			ID:            "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Cdn/profiles/afd1",
			Name:          "afd1",
			Location:      "global",
			ResourceGroup: "rg1",
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
			Name: "aks1",
			AgentPools: []AKSAgentPool{
				{ID: "pool1-arm", Name: "nodepool1"},
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
		{ID: "app1-arm", Name: "app1", Properties: &azContainerAppProperties{EnvironmentID: envArm}},
		{ID: "app2-arm", Name: "app2", Properties: &azContainerAppProperties{ManagedEnvironmentID: envArm}},
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
	subnetArm := "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Network/virtualNetworks/vnet1/subnets/aci"
	groups := []ContainerGroup{
		{
			ID:   "cg1-arm",
			Name: "cg1",
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
	subnetArm := "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Network/virtualNetworks/vnet1/subnets/apim"
	services := []APIMService{
		{
			ID:   "apim1-arm",
			Name: "apim1",
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
	localGW := "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg1/providers/Microsoft.Network/virtualNetworkGateways/gw1"
	remoteER := "/subscriptions/22222222-2222-2222-2222-222222222222/resourceGroups/hub-rg/providers/Microsoft.Network/expressRouteCircuits/hub-xpr"

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

	conns, stubs := TransformGatewayConnections(gwConns, allIDMap)
	if len(conns) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(conns))
	}
	if len(stubs) != 1 {
		t.Fatalf("expected 1 stub for cross-sub ER circuit, got %d", len(stubs))
	}
	if stubs[0].Type != "osiris.azure.expressroute" {
		t.Errorf("stub type = %q, want osiris.azure.expressroute", stubs[0].Type)
	}
	if stubs[0].Provider.Subscription != "22222222-2222-2222-2222-222222222222" {
		t.Errorf("stub subscription = %q, want the remote sub", stubs[0].Provider.Subscription)
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
	if conns[0].Type != "network.bgp" {
		t.Errorf("expected ExpressRoute gateway connection to use network.bgp subtype, got %q", conns[0].Type)
	}
}

func TestGatewayConnectionSubtype(t *testing.T) {
	cases := map[string]string{
		"ExpressRoute": "network.bgp",
		"expressroute": "network.bgp",
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
	localGW := "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg/providers/Microsoft.Network/virtualNetworkGateways/gw1"
	localER := "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg/providers/Microsoft.Network/expressRouteCircuits/er1"
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

	conns, stubs := TransformGatewayConnections(gwConns, allIDMap)
	if len(conns) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(conns))
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
			ID:   "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg/providers/Microsoft.Network/privateEndpoints/pe-blob",
			Name: "pe-blob",
			PrivateLinkServiceConnections: []azPrivateLinkServiceConnection{
				{
					Name:                 "pe-blob-conn",
					PrivateLinkServiceID: "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/mystg",
					GroupIDs:             []string{"blob"},
				},
			},
			CustomDNSConfigs: []azPrivateEndpointDNSConfig{
				{FQDN: "mystg.blob.core.windows.net", IPAddresses: []string{"10.0.0.5"}},
			},
		},
		{
			ID:   "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg/providers/Microsoft.Network/privateEndpoints/pe-bare",
			Name: "pe-bare",
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
	if got := enriched.Properties["private_link_service_id"]; got != "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/mystg" {
		t.Errorf("private_link_service_id = %v", got)
	}
	configs, ok := enriched.Properties["custom_dns_configs"].([]map[string]any)
	if !ok || len(configs) != 1 {
		t.Fatalf("custom_dns_configs missing or wrong shape: %#v", enriched.Properties["custom_dns_configs"])
	}
	if configs[0]["fqdn"] != "mystg.blob.core.windows.net" {
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
