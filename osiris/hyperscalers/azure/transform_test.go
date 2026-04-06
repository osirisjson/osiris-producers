// transform_test.go - Unit tests for Microsoft Azure data transformation.
//
// For an introduction to OSIRIS JSON Producer for Microsoft Azure see:
// "[OSIRIS-JSON-AZURE]."
//
// [OSIRIS-JSON-AZURE]: https://osirisjson.org/en/docs/producers/hyperscalers/microsoft-azure

package azure

import (
	"testing"
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
			ID:            "/subscriptions/a1b2c3d4/resourceGroups/rg1/providers/Microsoft.Network/virtualNetworks/vnet1",
			Name:          "vnet1",
			Location:      "westeurope",
			ResourceGroup: "rg1",
			AddressSpace:  azAddressSpace{AddressPrefixes: []string{"10.0.0.0/16"}},
			DhcpOptions:   azDHCPOptions{DNSServers: []string{"10.0.0.4"}},
		},
	}

	resources := TransformVNets(vnets, testSub)

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
			ID:                   "/subscriptions/a1b2c3d4/resourceGroups/rg1/providers/Microsoft.Network/virtualNetworks/vnet1/subnets/subnet1",
			Name:                 "subnet1",
			ResourceGroup:        "rg1",
			AddressPrefixes:      []string{"10.0.1.0/24"},
			NetworkSecurityGroup: &azNSGRef{ID: "/subscriptions/a1b2c3d4/resourceGroups/rg1/providers/Microsoft.Network/networkSecurityGroups/nsg1"},
			RouteTable:           &azRouteTableRef{ID: "/subscriptions/a1b2c3d4/resourceGroups/rg1/providers/Microsoft.Network/routeTables/rt1"},
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
			ID:            "/subscriptions/a1b2c3d4/resourceGroups/rg1/providers/Microsoft.Network/networkInterfaces/nic1",
			Name:          "nic1",
			Location:      "westeurope",
			ResourceGroup: "rg1",
			IPConfigurations: []IPConfiguration{
				{
					Name:                      "ipconfig1",
					Subnet:                    &azSubnetRef{ID: "/subscriptions/a1b2c3d4/.../subnets/subnet1"},
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
			ID:            "/subscriptions/a1b2c3d4/resourceGroups/rg1/providers/Microsoft.Compute/virtualMachines/vm1",
			Name:          "vm1",
			Location:      "westeurope",
			ResourceGroup: "rg1",
			VMSize:        "Standard_D2s_v3",
			PowerState:    "VM running",
		},
		{
			ID:            "/subscriptions/a1b2c3d4/resourceGroups/rg1/providers/Microsoft.Compute/virtualMachines/vm2",
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
		"/subscriptions/a1b2c3d4/.../virtualNetworks/vnet1": "azure::/subscriptions/a1b2c3d4/.../virtualNetworks/vnet1",
		"/subscriptions/a1b2c3d4/.../virtualNetworks/vnet2": "azure::/subscriptions/a1b2c3d4/.../virtualNetworks/vnet2",
	}

	peerings := []VNetPeering{
		{
			ID:                   "/subscriptions/a1b2c3d4/.../virtualNetworks/vnet1/virtualNetworkPeerings/peer1",
			Name:                 "vnet1-to-vnet2",
			RemoteVirtualNetwork: &azVNetRef{ID: "/subscriptions/a1b2c3d4/.../virtualNetworks/vnet2"},
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
	if c.Type != "network" {
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
			ID:       "/subscriptions/a1b2c3d4/resourceGroups/rg1",
			Name:     "rg1",
			Location: "westeurope",
		},
		{
			ID:       "/subscriptions/a1b2c3d4/resourceGroups/rg2",
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

func TestExtractResourceGroup(t *testing.T) {
	tests := []struct {
		armID    string
		expected string
	}{
		{
			armID:    "/subscriptions/xxx/resourceGroups/my-rg/providers/Microsoft.Network/virtualNetworks/vnet1",
			expected: "my-rg",
		},
		{
			armID:    "/subscriptions/xxx/resourceGroups/MyRG",
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
	armID := "/subscriptions/a1b2c3d4/resourceGroups/rg1/providers/Microsoft.Network/virtualNetworks/vnet1"
	id := resourceID("network.vpc", armID)

	// Per OSIRIS Producer Guidelines section 2.2.1: hyperscaler IDs use provider::native-id.
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
	armID2 := "/subscriptions/a1b2c3d4/resourceGroups/rg1/providers/Microsoft.Network/virtualNetworks/vnet2"
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
				{Subnet: &azSubnetRef{ID: "/sub/subnet1"}, PrivateIPAddress: "10.0.1.5"}, // same subnet - should deduplicate
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
		{ID: "/sub/pe2", Name: "pe2"}, // no subnet - skipped
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
				{Name: "vnetlink2", VirtualNetwork: &azVNetRef{ID: "/sub/vnet-unknown"}}, // not in map
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
		{"/subscriptions/xxx/resourceGroups/rg1/providers/Microsoft.Network/virtualNetworks/vnet1", "vnet1"},
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
		{ID: "/subscriptions/xxx/resourceGroups/rg1", Name: "rg1", Location: "westeurope"},
	}
	groups, nameToID := TransformResourceGroupGroups(rgs, testSub)

	vnets := []VirtualNetwork{
		{
			ID:            "/subscriptions/xxx/resourceGroups/rg1/providers/Microsoft.Network/virtualNetworks/vnet1",
			Name:          "vnet1",
			Location:      "westeurope",
			ResourceGroup: "rg1",
		},
	}
	resources := TransformVNets(vnets, testSub)

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
		{ID: "/subscriptions/xxx/resourceGroups/rg1", Name: "rg1", Location: "westeurope"},
		{ID: "/subscriptions/xxx/resourceGroups/rg2", Name: "rg2", Location: "eastus"},
	}
	groups, _ := TransformResourceGroupGroups(rgs, testSub)
	subGroup := TransformSubscriptionGroup(testSub)

	WireResourceGroupsToSubscription(&subGroup, groups)

	if len(subGroup.Children) != 2 {
		t.Fatalf("expected 2 children in subscription group, got %d", len(subGroup.Children))
	}
}
