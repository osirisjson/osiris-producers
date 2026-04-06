// transform.go - Pure Microsoft Azure->OSIRIS mapping functions.
// Converts Azure resource types (from CLI queries) into SDK types.
// All functions are stateless: no I/O, no CLI calls, just data transformation.
//
// Resource type mapping follows OSIRIS JSON specification Chapter 7 and Azure ARM types:
//
//   Standard types (spec-defined):
//   Microsoft.Resources/resourceGroups      	-> container.resourcegroup
//   Microsoft.Network/virtualNetworks       	-> network.vpc
//   Microsoft.Network/virtualNetworks/subnets 	-> network.subnet
//   Microsoft.Network/networkInterfaces     	-> network.interface
//   Microsoft.Network/networkSecurityGroups 	-> network.security.group
//   Microsoft.Network/loadBalancers         	-> network.loadbalancer
//   Microsoft.Network/azureFirewalls        	-> network.firewall
//   Microsoft.Compute/virtualMachines       	-> compute.vm
//
//   Custom types (osiris.azure.* namespace):
//   Microsoft.Network/routeTables           	-> osiris.azure.routetable
//   Microsoft.Network/publicIPAddresses     	-> osiris.azure.publicip
//   Microsoft.Network/privateEndpoints      	-> osiris.azure.privateendpoint
//   Microsoft.Network/virtualNetworkGateways 	-> osiris.azure.gateway.vnet
//   Microsoft.Network/natGateways           	-> osiris.azure.gateway.nat
//   Microsoft.Network/privateDnsZones       	-> osiris.azure.dns.privatezone
//   Microsoft.Network/dnsZones              	-> osiris.azure.dns.zone
//   Microsoft.Network/expressRouteCircuits  	-> osiris.azure.expressroute
//
// For an introduction to OSIRIS JSON Producer for Microsoft Azure see:
// "[OSIRIS-JSON-AZURE]."
//
// [OSIRIS-JSON-AZURE]: https://osirisjson.org/en/docs/producers/hyperscalers/microsoft-azure
// [OSIRIS-JSON-SPEC-CHAPTER-07]: https://osirisjson.org/en/docs/spec/v10/07-resourcetypetaxonomy
// [OSIRIS-JSON-SPEC-APPENDICES-C5]: https://osirisjson.org/en/docs/spec/v10/15-appendices#c5-container-and-organization-resources
// [OSIRIS-JSON-PRODUCER-GUIDELINES]: https://osirisjson.org/en/docs/getting-started/osiris-producer-guidelines

package azure

import (
	"fmt"
	"strings"

	"go.osirisjson.org/producers/pkg/sdk"
)

const (
	extensionNamespace = "osiris.azure"
	providerName       = "azure"
)

// resourceID generates a deterministic resource ID from an Azure ARM resource ID.
// Per OSIRIS JSON Producer Guidelines section 2.2.1, hyperscaler resource IDs use the
// pattern: provider::native-id (e.g. azure::/subscriptions/sub-123/.../vm01).
// The ARM resource ID is the stable native identifier for all Azure resources.
func resourceID(_ string, armID string) string {
	return "azure::" + armID
}

// azureProvider creates a Provider for an Azure resource.
// The nativeType parameter is the ARM resource type (e.g. "Microsoft.Network/virtualNetworks").
func azureProvider(armID, nativeType, location, subscription, tenant string) sdk.Provider {
	return sdk.Provider{
		Name:         providerName,
		NativeID:     armID,
		Type:         nativeType,
		Region:       location,
		Subscription: subscription,
		Tenant:       tenant,
	}
}

// extractResourceGroup extracts the resource group name from an ARM resource ID.
func extractResourceGroup(armID string) string {
	lower := strings.ToLower(armID)
	idx := strings.Index(lower, "/resourcegroups/")
	if idx < 0 {
		return ""
	}
	rest := armID[idx+len("/resourcegroups/"):]
	if slashIdx := strings.Index(rest, "/"); slashIdx >= 0 {
		return rest[:slashIdx]
	}
	return rest
}

// TransformVNets converts Azure VirtualNetworks into OSIRIS resources.
func TransformVNets(vnets []VirtualNetwork, sub SubscriptionInfo) []sdk.Resource {
	var resources []sdk.Resource
	for _, v := range vnets {
		id := resourceID("network.vpc", v.ID)
		prov := azureProvider(v.ID, "Microsoft.Network/virtualNetworks", v.Location, sub.SubscriptionID, sub.TenantID)

		r, err := sdk.NewResource(id, "network.vpc", prov)
		if err != nil {
			continue
		}
		r.Name = v.Name
		r.Status = "active"

		props := map[string]any{
			"resource_group": v.ResourceGroup,
		}
		if len(v.AddressSpace.AddressPrefixes) > 0 {
			props["address_space"] = v.AddressSpace.AddressPrefixes
		}
		if len(v.DhcpOptions.DNSServers) > 0 {
			props["dns_servers"] = v.DhcpOptions.DNSServers
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources
}

// TransformSubnets converts Azure Subnets into OSIRIS JSON resources.
// Returns resources and a map of subnet ARM ID -> resource ID for wiring connections.
func TransformSubnets(subnets []Subnet, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(subnets))

	for _, s := range subnets {
		id := resourceID("network.subnet", s.ID)
		idMap[s.ID] = id

		prov := azureProvider(s.ID, "Microsoft.Network/virtualNetworks/subnets", "", sub.SubscriptionID, sub.TenantID)

		r, err := sdk.NewResource(id, "network.subnet", prov)
		if err != nil {
			continue
		}
		r.Name = s.Name
		r.Status = "active"

		props := map[string]any{
			"resource_group": s.ResourceGroup,
		}
		if len(s.AddressPrefixes) > 0 {
			props["address_prefixes"] = s.AddressPrefixes
		}
		if len(s.ServiceEndpoints) > 0 {
			props["service_endpoints"] = s.ServiceEndpoints
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformNICs converts Azure NetworkInterfaces into OSIRIS JSON resources.
// Returns resources and a map of NIC ARM ID -> resource ID.
func TransformNICs(nics []NetworkInterface, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(nics))

	for _, n := range nics {
		id := resourceID("network.interface", n.ID)
		idMap[n.ID] = id

		prov := azureProvider(n.ID, "Microsoft.Network/networkInterfaces", n.Location, sub.SubscriptionID, sub.TenantID)

		r, err := sdk.NewResource(id, "network.interface", prov)
		if err != nil {
			continue
		}
		r.Name = n.Name
		r.Status = "active"

		props := map[string]any{
			"resource_group": n.ResourceGroup,
		}
		if len(n.IPConfigurations) > 0 {
			ips := make([]map[string]any, 0, len(n.IPConfigurations))
			for _, ip := range n.IPConfigurations {
				ipMap := map[string]any{
					"name":              ip.Name,
					"allocation_method": ip.PrivateIPAllocationMethod,
				}
				if ip.PrivateIPAddress != "" {
					ipMap["private_ip"] = ip.PrivateIPAddress
				}
				ips = append(ips, ipMap)
			}
			props["ip_configurations"] = ips
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformNSGs converts Azure NetworkSecurityGroups into OSIRIS resources.
// Returns resources and a map of NSG ARM ID -> resource ID.
func TransformNSGs(nsgs []NetworkSecurityGroup, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(nsgs))

	for _, n := range nsgs {
		id := resourceID("network.security.group", n.ID)
		idMap[n.ID] = id

		prov := azureProvider(n.ID, "Microsoft.Network/networkSecurityGroups", n.Location, sub.SubscriptionID, sub.TenantID)

		r, err := sdk.NewResource(id, "network.security.group", prov)
		if err != nil {
			continue
		}
		r.Name = n.Name
		r.Status = "active"
		r.Properties = map[string]any{
			"resource_group": n.ResourceGroup,
		}
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformRouteTables converts Azure RouteTables into OSIRIS JSON resources.
// Returns resources and a map of route table ARM ID -> resource ID.
func TransformRouteTables(tables []RouteTable, sub SubscriptionInfo, detailed ...bool) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(tables))

	for _, t := range tables {
		id := resourceID("osiris.azure.routetable", t.ID)
		idMap[t.ID] = id

		prov := azureProvider(t.ID, "Microsoft.Network/routeTables", t.Location, sub.SubscriptionID, sub.TenantID)

		r, err := sdk.NewResource(id, "osiris.azure.routetable", prov)
		if err != nil {
			continue
		}
		r.Name = t.Name
		r.Status = "active"

		props := map[string]any{
			"resource_group": t.ResourceGroup,
			"route_count":    len(t.Routes),
		}
		if len(detailed) > 0 && detailed[0] && len(t.Routes) > 0 {
			routes := make([]map[string]any, 0, len(t.Routes))
			for _, rt := range t.Routes {
				entry := map[string]any{
					"name":           rt.Name,
					"address_prefix": rt.AddressPrefix,
					"next_hop_type":  rt.NextHopType,
				}
				if rt.NextHopIPAddress != "" {
					entry["next_hop_ip"] = rt.NextHopIPAddress
				}
				routes = append(routes, entry)
			}
			props["routes"] = routes
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformPublicIPs converts Azure PublicIPAddresses into OSIRIS JSON resources.
func TransformPublicIPs(ips []PublicIPAddress, sub SubscriptionInfo, detailed ...bool) []sdk.Resource {
	var resources []sdk.Resource
	for _, p := range ips {
		id := resourceID("osiris.azure.publicip", p.ID)
		prov := azureProvider(p.ID, "Microsoft.Network/publicIPAddresses", p.Location, sub.SubscriptionID, sub.TenantID)

		r, err := sdk.NewResource(id, "osiris.azure.publicip", prov)
		if err != nil {
			continue
		}
		r.Name = p.Name
		r.Status = "active"

		props := map[string]any{
			"resource_group":    p.ResourceGroup,
			"allocation_method": p.PublicIPAllocationMethod,
		}
		if p.IPAddress != "" {
			props["ip_address"] = p.IPAddress
		}
		if p.SKU.Name != "" {
			props["sku"] = p.SKU.Name
		}
		if len(detailed) > 0 && detailed[0] {
			if p.SKU.Tier != "" {
				props["sku_tier"] = p.SKU.Tier
			}
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources
}

// TransformLoadBalancers converts Azure LoadBalancers into OSIRIS JSON resources.
func TransformLoadBalancers(lbs []LoadBalancer, sub SubscriptionInfo, detailed ...bool) []sdk.Resource {
	var resources []sdk.Resource
	for _, lb := range lbs {
		id := resourceID("network.loadbalancer", lb.ID)
		prov := azureProvider(lb.ID, "Microsoft.Network/loadBalancers", lb.Location, sub.SubscriptionID, sub.TenantID)

		r, err := sdk.NewResource(id, "network.loadbalancer", prov)
		if err != nil {
			continue
		}
		r.Name = lb.Name
		r.Status = "active"

		props := map[string]any{
			"resource_group":     lb.ResourceGroup,
			"frontend_count":     len(lb.FrontendIPConfigurations),
			"backend_pool_count": len(lb.BackendAddressPools),
			"rule_count":         len(lb.LoadBalancingRules),
		}
		if lb.SKU.Name != "" {
			props["sku"] = lb.SKU.Name
		}
		if len(detailed) > 0 && detailed[0] {
			if lb.SKU.Tier != "" {
				props["sku_tier"] = lb.SKU.Tier
			}
			if len(lb.LoadBalancingRules) > 0 {
				rules := make([]map[string]any, 0, len(lb.LoadBalancingRules))
				for _, rule := range lb.LoadBalancingRules {
					rules = append(rules, map[string]any{
						"name":          rule.Name,
						"protocol":      rule.Protocol,
						"frontend_port": rule.FrontendPort,
						"backend_port":  rule.BackendPort,
					})
				}
				props["rules"] = rules
			}
			if len(lb.BackendAddressPools) > 0 {
				pools := make([]map[string]any, 0, len(lb.BackendAddressPools))
				for _, pool := range lb.BackendAddressPools {
					pools = append(pools, map[string]any{
						"name":             pool.Name,
						"backend_ip_count": len(pool.BackendIPConfigurations),
					})
				}
				props["backend_pools"] = pools
			}
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources
}

// TransformPrivateEndpoints converts Azure PrivateEndpoints into OSIRIS JSON resources.
func TransformPrivateEndpoints(pes []PrivateEndpoint, sub SubscriptionInfo) []sdk.Resource {
	var resources []sdk.Resource
	for _, pe := range pes {
		id := resourceID("osiris.azure.privateendpoint", pe.ID)
		prov := azureProvider(pe.ID, "Microsoft.Network/privateEndpoints", pe.Location, sub.SubscriptionID, sub.TenantID)

		r, err := sdk.NewResource(id, "osiris.azure.privateendpoint", prov)
		if err != nil {
			continue
		}
		r.Name = pe.Name
		r.Status = "active"
		r.Properties = map[string]any{
			"resource_group": pe.ResourceGroup,
		}
		resources = append(resources, r)
	}
	return resources
}

// TransformVNetGateways converts Azure VNetGateways into OSIRIS resources.
func TransformVNetGateways(gws []VNetGateway, sub SubscriptionInfo) []sdk.Resource {
	var resources []sdk.Resource
	for _, gw := range gws {
		id := resourceID("osiris.azure.gateway.vnet", gw.ID)
		prov := azureProvider(gw.ID, "Microsoft.Network/virtualNetworkGateways", gw.Location, sub.SubscriptionID, sub.TenantID)

		r, err := sdk.NewResource(id, "osiris.azure.gateway.vnet", prov)
		if err != nil {
			continue
		}
		r.Name = gw.Name
		r.Status = "active"

		props := map[string]any{
			"resource_group": gw.ResourceGroup,
			"gateway_type":   gw.GatewayType,
		}
		if gw.VPNType != "" {
			props["vpn_type"] = gw.VPNType
		}
		if gw.EnableBGP {
			props["bgp_enabled"] = true
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources
}

// TransformNATGateways converts Azure NATGateways into OSIRIS JSON resources.
func TransformNATGateways(gws []NATGateway, sub SubscriptionInfo) []sdk.Resource {
	var resources []sdk.Resource
	for _, gw := range gws {
		id := resourceID("osiris.azure.gateway.nat", gw.ID)
		prov := azureProvider(gw.ID, "Microsoft.Network/natGateways", gw.Location, sub.SubscriptionID, sub.TenantID)

		r, err := sdk.NewResource(id, "osiris.azure.gateway.nat", prov)
		if err != nil {
			continue
		}
		r.Name = gw.Name
		r.Status = "active"
		r.Properties = map[string]any{
			"resource_group":     gw.ResourceGroup,
			"public_ip_count":    len(gw.PublicIPAddresses),
			"associated_subnets": len(gw.Subnets),
		}
		resources = append(resources, r)
	}
	return resources
}

// TransformFirewalls converts Azure Firewalls into OSIRIS JSON resources.
func TransformFirewalls(fws []AzureFirewall, sub SubscriptionInfo) []sdk.Resource {
	var resources []sdk.Resource
	for _, fw := range fws {
		id := resourceID("network.firewall", fw.ID)
		prov := azureProvider(fw.ID, "Microsoft.Network/azureFirewalls", fw.Location, sub.SubscriptionID, sub.TenantID)

		r, err := sdk.NewResource(id, "network.firewall", prov)
		if err != nil {
			continue
		}
		r.Name = fw.Name
		r.Status = "active"
		r.Properties = map[string]any{
			"resource_group": fw.ResourceGroup,
		}
		resources = append(resources, r)
	}
	return resources
}

// TransformAppGateways converts Azure ApplicationGateways into OSIRIS JSON resources.
func TransformAppGateways(gws []ApplicationGateway, sub SubscriptionInfo) []sdk.Resource {
	var resources []sdk.Resource
	for _, gw := range gws {
		id := resourceID("network.loadbalancer", gw.ID)
		prov := azureProvider(gw.ID, "Microsoft.Network/applicationGateways", gw.Location, sub.SubscriptionID, sub.TenantID)

		r, err := sdk.NewResource(id, "network.loadbalancer", prov)
		if err != nil {
			continue
		}
		r.Name = gw.Name
		r.Status = "active"
		r.Properties = map[string]any{
			"resource_group": gw.ResourceGroup,
		}
		resources = append(resources, r)
	}
	return resources
}

// TransformDNSZones converts Azure DNS zones into OSIRIS JSON resources.
func TransformDNSZones(zones []DNSZone, sub SubscriptionInfo) []sdk.Resource {
	var resources []sdk.Resource
	for _, z := range zones {
		id := resourceID("osiris.azure.dns.zone", z.ID)
		prov := azureProvider(z.ID, "Microsoft.Network/dnsZones", "global", sub.SubscriptionID, sub.TenantID)

		r, err := sdk.NewResource(id, "osiris.azure.dns.zone", prov)
		if err != nil {
			continue
		}
		r.Name = z.Name
		r.Status = "active"
		r.Properties = map[string]any{
			"resource_group": z.ResourceGroup,
		}
		resources = append(resources, r)
	}
	return resources
}

// TransformPrivateDNSZones converts Azure private DNS zones into OSIRIS JSON resources.
func TransformPrivateDNSZones(zones []PrivateDNSZone, sub SubscriptionInfo) []sdk.Resource {
	var resources []sdk.Resource
	for _, z := range zones {
		id := resourceID("osiris.azure.dns.privatezone", z.ID)
		prov := azureProvider(z.ID, "Microsoft.Network/privateDnsZones", "global", sub.SubscriptionID, sub.TenantID)

		r, err := sdk.NewResource(id, "osiris.azure.dns.privatezone", prov)
		if err != nil {
			continue
		}
		r.Name = z.Name
		r.Status = "active"

		props := map[string]any{
			"resource_group": z.ResourceGroup,
			"link_count":     len(z.Links),
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources
}

// TransformExpressRouteCircuits converts Azure ExpressRoute circuits into OSIRIS JSON resources.
func TransformExpressRouteCircuits(circuits []ExpressRouteCircuit, sub SubscriptionInfo) []sdk.Resource {
	var resources []sdk.Resource
	for _, c := range circuits {
		id := resourceID("osiris.azure.expressroute", c.ID)
		prov := azureProvider(c.ID, "Microsoft.Network/expressRouteCircuits", c.Location, sub.SubscriptionID, sub.TenantID)

		r, err := sdk.NewResource(id, "osiris.azure.expressroute", prov)
		if err != nil {
			continue
		}
		r.Name = c.Name
		r.Status = "active"
		r.Properties = map[string]any{
			"resource_group": c.ResourceGroup,
		}
		resources = append(resources, r)
	}
	return resources
}

// TransformVMs converts Azure VirtualMachines into OSIRIS JSON resources.
func TransformVMs(vms []VirtualMachine, sub SubscriptionInfo) []sdk.Resource {
	var resources []sdk.Resource
	for _, vm := range vms {
		id := resourceID("compute.vm", vm.ID)
		prov := azureProvider(vm.ID, "Microsoft.Compute/virtualMachines", vm.Location, sub.SubscriptionID, sub.TenantID)

		r, err := sdk.NewResource(id, "compute.vm", prov)
		if err != nil {
			continue
		}
		r.Name = vm.Name
		r.Status = mapVMPowerState(vm.PowerState)

		props := map[string]any{
			"resource_group": vm.ResourceGroup,
		}
		if vm.VMSize != "" {
			props["vm_size"] = vm.VMSize
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources
}

// OSIRIS JSON Connection transforms

// TransformVNetPeerings converts Azure VNet peerings into OSIRIS JSON connections.
// Requires a map of VNet ARM ID -> OSIRIS JSON resource ID to wire source/target.
// Returns connections and stub resources for remote VNets in other subscriptions.
func TransformVNetPeerings(peerings []VNetPeering, vnetIDMap map[string]string) ([]sdk.Connection, []sdk.Resource) {
	var connections []sdk.Connection
	var stubs []sdk.Resource
	seen := map[string]bool{}
	stubSeen := map[string]bool{}
	for _, p := range peerings {
		sourceID, ok := vnetIDMap[p.VNetID()]
		if !ok {
			continue
		}

		// The remote VNet may be in a different subscription (not in our ID map).
		// Create a stub resource for it so peering connections can reference it.
		targetID, ok := vnetIDMap[p.RemoteVNetID()]
		if !ok {
			targetID = resourceID("network.vpc", p.RemoteVNetID())
			if !stubSeen[targetID] {
				stubSeen[targetID] = true
				prov := sdk.Provider{
					Name:         providerName,
					NativeID:     p.RemoteVNetID(),
					Type:         "Microsoft.Network/virtualNetworks",
					Subscription: extractSubscriptionID(p.RemoteVNetID()),
				}
				stub, err := sdk.NewResource(targetID, "network.vpc", prov)
				if err == nil {
					stub.Name = vnetNameFromARM(p.RemoteVNetID())
					stub.Status = "unknown"
					stubs = append(stubs, stub)
				}
			}
		}

		canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
			Type:      "network",
			Direction: "bidirectional",
			Source:    sourceID,
			Target:    targetID,
		})
		connID := sdk.BuildConnectionID(canonicalKey, 16)

		// Azure returns a peering record from each side of the link.
		// Bidirectional canonical keys produce the same ID for both
		// directions, so skip duplicates.
		if seen[connID] {
			continue
		}
		seen[connID] = true

		conn, err := sdk.NewConnection(connID, "network", sourceID, targetID)
		if err != nil {
			continue
		}
		conn.Name = p.Name
		conn.Status = mapPeeringState(p.PeeringState)

		props := map[string]any{}
		if p.AllowGatewayTransit {
			props["allow_gateway_transit"] = true
		}
		if p.UseRemoteGateways {
			props["use_remote_gateways"] = true
		}
		if len(props) > 0 {
			conn.Properties = props
		}

		connections = append(connections, conn)
	}
	return connections, stubs
}

// extractSubscriptionID extracts the subscription UUID from an ARM resource ID.
// ARM IDs follow the pattern: /subscriptions/<uuid>/...
func extractSubscriptionID(armID string) string {
	lower := strings.ToLower(armID)
	idx := strings.Index(lower, "/subscriptions/")
	if idx < 0 {
		return ""
	}
	rest := armID[idx+len("/subscriptions/"):]
	if slashIdx := strings.Index(rest, "/"); slashIdx >= 0 {
		return rest[:slashIdx]
	}
	return rest
}

// vnetNameFromARM extracts the VNet name from an ARM resource ID.
func vnetNameFromARM(armID string) string {
	parts := strings.Split(armID, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return armID
}

// TransformSubnetNSGConnections creates connections between subnets and their associated NSGs.
func TransformSubnetNSGConnections(subnets []Subnet, subnetIDMap, nsgIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, s := range subnets {
		if s.NSGId() == "" {
			continue
		}
		sourceID, ok := subnetIDMap[s.ID]
		if !ok {
			continue
		}
		targetID, ok := nsgIDMap[s.NSGId()]
		if !ok {
			continue
		}

		canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
			Type:      "network",
			Direction: "forward",
			Source:    sourceID,
			Target:    targetID,
		})
		connID := sdk.BuildConnectionID(canonicalKey, 16)

		conn, err := sdk.NewConnection(connID, "network", sourceID, targetID)
		if err != nil {
			continue
		}
		conn.Name = fmt.Sprintf("%s -> NSG", s.Name)
		_ = conn.SetDirection("forward")
		connections = append(connections, conn)
	}
	return connections
}

// TransformSubnetRouteTableConnections creates connections between subnets and their route tables.
func TransformSubnetRouteTableConnections(subnets []Subnet, subnetIDMap, rtIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, s := range subnets {
		if s.RouteTableId() == "" {
			continue
		}
		sourceID, ok := subnetIDMap[s.ID]
		if !ok {
			continue
		}
		targetID, ok := rtIDMap[s.RouteTableId()]
		if !ok {
			continue
		}

		canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
			Type:      "network",
			Direction: "forward",
			Source:    sourceID,
			Target:    targetID,
		})
		connID := sdk.BuildConnectionID(canonicalKey, 16)

		conn, err := sdk.NewConnection(connID, "network", sourceID, targetID)
		if err != nil {
			continue
		}
		conn.Name = fmt.Sprintf("%s -> route table", s.Name)
		_ = conn.SetDirection("forward")
		connections = append(connections, conn)
	}
	return connections
}

// TransformGatewayConnections converts Azure gateway connections into OSIRIS JSON connections.
func TransformGatewayConnections(gwConns []GatewayConnection, allResourceIDs map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, gc := range gwConns {
		sourceID, ok := allResourceIDs[gc.VirtualNetworkGateway1ID()]
		if !ok {
			sourceID = resourceID("osiris.azure.gateway.vnet", gc.VirtualNetworkGateway1ID())
		}

		// The peer could be an ExpressRoute circuit, another gateway, etc.
		targetID, ok := allResourceIDs[gc.PeerID()]
		if !ok {
			targetID = resourceID("osiris.azure.gateway.vnet", gc.PeerID())
		}

		canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
			Type:      "network",
			Direction: "bidirectional",
			Source:    sourceID,
			Target:    targetID,
		})
		connID := sdk.BuildConnectionID(canonicalKey, 16)

		conn, err := sdk.NewConnection(connID, "network", sourceID, targetID)
		if err != nil {
			continue
		}
		conn.Name = gc.Name
		conn.Status = "active"

		if gc.ConnectionType != "" {
			conn.Properties = map[string]any{
				"connection_type": gc.ConnectionType,
			}
		}
		connections = append(connections, conn)
	}
	return connections
}

// TransformSubnetToVNetConnections creates connections between subnets and their parent VNet.
// This is the fundamental containment relationship in Azure networking.
func TransformSubnetToVNetConnections(subnets []Subnet, subnetIDMap, vnetIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, s := range subnets {
		if s.VNetID() == "" {
			continue
		}
		sourceID, ok := subnetIDMap[s.ID]
		if !ok {
			continue
		}
		targetID, ok := vnetIDMap[s.VNetID()]
		if !ok {
			continue
		}

		canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
			Type:      "contains",
			Direction: "forward",
			Source:    sourceID,
			Target:    targetID,
		})
		connID := sdk.BuildConnectionID(canonicalKey, 16)

		conn, err := sdk.NewConnection(connID, "contains", sourceID, targetID)
		if err != nil {
			continue
		}
		conn.Name = fmt.Sprintf("%s -> %s", s.Name, extractLastSegment(s.VNetID()))
		_ = conn.SetDirection("forward")
		connections = append(connections, conn)
	}
	return connections
}

// TransformNICToSubnetConnections creates connections between NICs and their subnets.
// Each NIC ipConfiguration references a subnet - this is how VMs attach to the network.
func TransformNICToSubnetConnections(nics []NetworkInterface, nicIDMap, subnetIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	seen := map[string]bool{} // deduplicate NIC->subnet pairs
	for _, n := range nics {
		sourceID, ok := nicIDMap[n.ID]
		if !ok {
			continue
		}
		for _, ip := range n.IPConfigurations {
			if ip.SubnetID() == "" {
				continue
			}
			targetID, ok := subnetIDMap[ip.SubnetID()]
			if !ok {
				continue
			}
			pairKey := sourceID + "|" + targetID
			if seen[pairKey] {
				continue
			}
			seen[pairKey] = true

			canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
				Type:      "network",
				Direction: "forward",
				Source:    sourceID,
				Target:    targetID,
			})
			connID := sdk.BuildConnectionID(canonicalKey, 16)

			conn, err := sdk.NewConnection(connID, "network", sourceID, targetID)
			if err != nil {
				continue
			}
			conn.Name = fmt.Sprintf("%s -> %s", n.Name, extractLastSegment(ip.SubnetID()))
			_ = conn.SetDirection("forward")

			if ip.PrivateIPAddress != "" {
				conn.Properties = map[string]any{
					"private_ip": ip.PrivateIPAddress,
				}
			}
			connections = append(connections, conn)
		}
	}
	return connections
}

// TransformPrivateEndpointToSubnetConnections creates connections between private endpoints and subnets.
func TransformPrivateEndpointToSubnetConnections(pes []PrivateEndpoint, subnetIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, pe := range pes {
		if pe.SubnetID() == "" {
			continue
		}
		sourceID := resourceID("osiris.azure.privateendpoint", pe.ID)
		targetID, ok := subnetIDMap[pe.SubnetID()]
		if !ok {
			continue
		}

		canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
			Type:      "network",
			Direction: "forward",
			Source:    sourceID,
			Target:    targetID,
		})
		connID := sdk.BuildConnectionID(canonicalKey, 16)

		conn, err := sdk.NewConnection(connID, "network", sourceID, targetID)
		if err != nil {
			continue
		}
		conn.Name = fmt.Sprintf("%s -> %s", pe.Name, extractLastSegment(pe.SubnetID()))
		_ = conn.SetDirection("forward")
		connections = append(connections, conn)
	}
	return connections
}

// TransformPrivateEndpointToNICConnections creates connections between private endpoints and their NICs.
func TransformPrivateEndpointToNICConnections(pes []PrivateEndpoint, nicIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, pe := range pes {
		sourceID := resourceID("osiris.azure.privateendpoint", pe.ID)
		for _, nicArmID := range pe.NetworkInterfaceIDs() {
			targetID, ok := nicIDMap[nicArmID]
			if !ok {
				continue
			}

			canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
				Type:      "network",
				Direction: "forward",
				Source:    sourceID,
				Target:    targetID,
			})
			connID := sdk.BuildConnectionID(canonicalKey, 16)

			conn, err := sdk.NewConnection(connID, "network", sourceID, targetID)
			if err != nil {
				continue
			}
			conn.Name = fmt.Sprintf("%s -> %s", pe.Name, extractLastSegment(nicArmID))
			_ = conn.SetDirection("forward")
			connections = append(connections, conn)
		}
	}
	return connections
}

// TransformLBFrontendToPublicIPConnections creates connections between load balancer frontends and public IPs.
func TransformLBFrontendToPublicIPConnections(lbs []LoadBalancer, publicIPIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, lb := range lbs {
		sourceID := resourceID("network.loadbalancer", lb.ID)
		for _, fe := range lb.FrontendIPConfigurations {
			if fe.PublicIPAddressID() == "" {
				continue
			}
			targetID, ok := publicIPIDMap[fe.PublicIPAddressID()]
			if !ok {
				continue
			}

			canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
				Type:      "network",
				Direction: "forward",
				Source:    sourceID,
				Target:    targetID,
			})
			connID := sdk.BuildConnectionID(canonicalKey, 16)

			conn, err := sdk.NewConnection(connID, "network", sourceID, targetID)
			if err != nil {
				continue
			}
			conn.Name = fmt.Sprintf("%s frontend -> %s", lb.Name, extractLastSegment(fe.PublicIPAddressID()))
			_ = conn.SetDirection("forward")
			connections = append(connections, conn)
		}
	}
	return connections
}

// TransformVNetGatewayToSubnetConnections creates connections between VNet gateways and their GatewaySubnet.
func TransformVNetGatewayToSubnetConnections(gws []VNetGateway, subnetIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, gw := range gws {
		sourceID := resourceID("osiris.azure.gateway.vnet", gw.ID)
		for _, ip := range gw.IPConfigurations {
			if ip.SubnetID() == "" {
				continue
			}
			targetID, ok := subnetIDMap[ip.SubnetID()]
			if !ok {
				continue
			}

			canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
				Type:      "network",
				Direction: "forward",
				Source:    sourceID,
				Target:    targetID,
			})
			connID := sdk.BuildConnectionID(canonicalKey, 16)

			conn, err := sdk.NewConnection(connID, "network", sourceID, targetID)
			if err != nil {
				continue
			}
			conn.Name = fmt.Sprintf("%s -> GatewaySubnet", gw.Name)
			_ = conn.SetDirection("forward")
			connections = append(connections, conn)
		}
	}
	return connections
}

// TransformVNetGatewayToPublicIPConnections creates connections between VNet gateways and their public IPs.
func TransformVNetGatewayToPublicIPConnections(gws []VNetGateway, publicIPIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, gw := range gws {
		sourceID := resourceID("osiris.azure.gateway.vnet", gw.ID)
		for _, ip := range gw.IPConfigurations {
			if ip.PublicIPAddressID() == "" {
				continue
			}
			targetID, ok := publicIPIDMap[ip.PublicIPAddressID()]
			if !ok {
				continue
			}

			canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
				Type:      "network",
				Direction: "forward",
				Source:    sourceID,
				Target:    targetID,
			})
			connID := sdk.BuildConnectionID(canonicalKey, 16)

			conn, err := sdk.NewConnection(connID, "network", sourceID, targetID)
			if err != nil {
				continue
			}
			conn.Name = fmt.Sprintf("%s -> %s", gw.Name, extractLastSegment(ip.PublicIPAddressID()))
			_ = conn.SetDirection("forward")
			connections = append(connections, conn)
		}
	}
	return connections
}

// TransformNATGatewayToSubnetConnections creates connections between NAT gateways and their subnets.
func TransformNATGatewayToSubnetConnections(gws []NATGateway, subnetIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, gw := range gws {
		sourceID := resourceID("osiris.azure.gateway.nat", gw.ID)
		for _, subnetArmID := range gw.SubnetIDs() {
			targetID, ok := subnetIDMap[subnetArmID]
			if !ok {
				continue
			}

			canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
				Type:      "network",
				Direction: "forward",
				Source:    sourceID,
				Target:    targetID,
			})
			connID := sdk.BuildConnectionID(canonicalKey, 16)

			conn, err := sdk.NewConnection(connID, "network", sourceID, targetID)
			if err != nil {
				continue
			}
			conn.Name = fmt.Sprintf("%s -> %s", gw.Name, extractLastSegment(subnetArmID))
			_ = conn.SetDirection("forward")
			connections = append(connections, conn)
		}
	}
	return connections
}

// TransformNATGatewayToPublicIPConnections creates connections between NAT gateways and their public IPs.
func TransformNATGatewayToPublicIPConnections(gws []NATGateway, publicIPIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, gw := range gws {
		sourceID := resourceID("osiris.azure.gateway.nat", gw.ID)
		for _, pipArmID := range gw.PublicIPAddressIDs() {
			targetID, ok := publicIPIDMap[pipArmID]
			if !ok {
				continue
			}

			canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
				Type:      "network",
				Direction: "forward",
				Source:    sourceID,
				Target:    targetID,
			})
			connID := sdk.BuildConnectionID(canonicalKey, 16)

			conn, err := sdk.NewConnection(connID, "network", sourceID, targetID)
			if err != nil {
				continue
			}
			conn.Name = fmt.Sprintf("%s -> %s", gw.Name, extractLastSegment(pipArmID))
			_ = conn.SetDirection("forward")
			connections = append(connections, conn)
		}
	}
	return connections
}

// TransformPrivateDNSToVNetConnections creates connections between private DNS zones and linked VNets.
func TransformPrivateDNSToVNetConnections(zones []PrivateDNSZone, vnetIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, z := range zones {
		sourceID := resourceID("osiris.azure.dns.privatezone", z.ID)
		for _, link := range z.Links {
			if link.VirtualNetworkID() == "" {
				continue
			}
			targetID, ok := vnetIDMap[link.VirtualNetworkID()]
			if !ok {
				continue
			}

			canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
				Type:      "network",
				Direction: "forward",
				Source:    sourceID,
				Target:    targetID,
			})
			connID := sdk.BuildConnectionID(canonicalKey, 16)

			conn, err := sdk.NewConnection(connID, "network", sourceID, targetID)
			if err != nil {
				continue
			}
			conn.Name = fmt.Sprintf("%s -> %s", z.Name, link.Name)
			_ = conn.SetDirection("forward")

			if link.RegistrationEnabled {
				conn.Properties = map[string]any{
					"registration_enabled": true,
				}
			}
			connections = append(connections, conn)
		}
	}
	return connections
}

// TransformResourceGroupResources creates OSIRIS JSON resources of type container.resourcegroup
// for each Azure resource group. Per OSIRIS JSON specification Appendix C.5, resource groups are modeled
// as resources to enable full provenance tracking.
func TransformResourceGroupResources(rgs []ResourceGroup, sub SubscriptionInfo) []sdk.Resource {
	var resources []sdk.Resource
	for _, rg := range rgs {
		id := resourceID("container.resourcegroup", rg.ID)
		prov := azureProvider(rg.ID, "Microsoft.Resources/resourceGroups", rg.Location, sub.SubscriptionID, sub.TenantID)

		r, err := sdk.NewResource(id, "container.resourcegroup", prov)
		if err != nil {
			continue
		}
		r.Name = rg.Name
		r.Status = "active"
		r.Properties = map[string]any{
			"location": rg.Location,
		}
		resources = append(resources, r)
	}
	return resources
}

// OSIRIS JSON Group transforms

// TransformSubscriptionGroup creates an OSIRIS JSON group for the subscription.
func TransformSubscriptionGroup(sub SubscriptionInfo) sdk.Group {
	gid := sdk.GroupID(sdk.GroupIDInput{
		Type:          "logical.subscription",
		BoundaryToken: sub.SubscriptionID,
	})

	g, _ := sdk.NewGroup(gid, "logical.subscription")
	g.Name = sub.DisplayName
	g.Tags = sub.Tags
	return g
}

// TransformResourceGroupGroups creates OSIRIS JSON groups for each Azure resource group.
// Returns the groups and a map of resource group name (lowered) -> group ID for membership wiring.
func TransformResourceGroupGroups(rgs []ResourceGroup, sub SubscriptionInfo) ([]sdk.Group, map[string]string) {
	var groups []sdk.Group
	nameToID := make(map[string]string, len(rgs))

	for _, rg := range rgs {
		gid := sdk.GroupID(sdk.GroupIDInput{
			Type:          "logical.resourcegroup",
			BoundaryToken: rg.ID,
		})
		nameToID[strings.ToLower(rg.Name)] = gid

		g, err := sdk.NewGroup(gid, "logical.resourcegroup")
		if err != nil {
			continue
		}
		g.Name = rg.Name
		g.Properties = map[string]any{
			"location": rg.Location,
		}
		groups = append(groups, g)
	}
	return groups, nameToID
}

// WireResourcesToResourceGroups assigns resources as members of their resource group.
func WireResourcesToResourceGroups(resources []sdk.Resource, rgNameToGroupID map[string]string, rgGroups []sdk.Group) {
	idx := groupIndex(rgGroups)
	for _, r := range resources {
		rgName := ""
		if r.Properties != nil {
			if rg, ok := r.Properties["resource_group"].(string); ok {
				rgName = strings.ToLower(rg)
			}
		}
		if rgName == "" {
			rgName = strings.ToLower(extractResourceGroup(r.Provider.NativeID))
		}
		if rgName == "" {
			continue
		}
		groupID, ok := rgNameToGroupID[rgName]
		if !ok {
			continue
		}
		if i, ok := idx[groupID]; ok {
			rgGroups[i].AddMembers(r.ID)
		}
	}
}

// WireResourceGroupsToSubscription adds resource group group IDs as children of the subscription group.
func WireResourceGroupsToSubscription(subGroup *sdk.Group, rgGroups []sdk.Group) {
	for _, rg := range rgGroups {
		subGroup.AddChildren(rg.ID)
	}
}

// OSIRIS JSON Helpers

// mapVMPowerState converts Azure VM power state to OSIRIS JSON status.
func mapVMPowerState(state string) string {
	lower := strings.ToLower(state)
	switch {
	case strings.Contains(lower, "running"):
		return "active"
	case strings.Contains(lower, "deallocat"):
		return "inactive"
	case strings.Contains(lower, "stopped"):
		return "inactive"
	default:
		return "unknown"
	}
}

// mapPeeringState converts Azure peering state to OSIRIS JSON status.
func mapPeeringState(state string) string {
	switch strings.ToLower(state) {
	case "connected":
		return "active"
	case "disconnected":
		return "inactive"
	case "initiated":
		return "degraded"
	default:
		return "unknown"
	}
}

// groupIndex builds a map of group ID -> index in slice for efficient mutation.
func groupIndex(groups []sdk.Group) map[string]int {
	idx := make(map[string]int, len(groups))
	for i, g := range groups {
		idx[g.ID] = i
	}
	return idx
}

// extractLastSegment returns the last path segment of an ARM resource ID.
func extractLastSegment(armID string) string {
	idx := strings.LastIndex(armID, "/")
	if idx < 0 {
		return armID
	}
	return armID[idx+1:]
}

// BuildVNetIDMap builds a map of VNet ARM ID -> OSIRIS JSON resource ID from VNet resources.
func BuildVNetIDMap(vnets []VirtualNetwork) map[string]string {
	m := make(map[string]string, len(vnets))
	for _, v := range vnets {
		m[v.ID] = resourceID("network.vpc", v.ID)
	}
	return m
}

// BuildPublicIPIDMap builds a map of public IP ARM ID -> OSIRIS JSON resource ID.
func BuildPublicIPIDMap(pips []PublicIPAddress) map[string]string {
	m := make(map[string]string, len(pips))
	for _, p := range pips {
		m[p.ID] = resourceID("osiris.azure.publicip", p.ID)
	}
	return m
}

// BuildAllResourceIDMap merges all ARM ID -> OSIRIS JSON ID maps into one for gateway connection wiring.
func BuildAllResourceIDMap(maps ...map[string]string) map[string]string {
	total := 0
	for _, m := range maps {
		total += len(m)
	}
	merged := make(map[string]string, total)
	for _, m := range maps {
		for k, v := range m {
			merged[k] = v
		}
	}
	return merged
}
