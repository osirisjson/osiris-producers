// transform.go - Pure Microsoft Azure to OSIRIS JSON mapping functions.
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
		Region:       normalizeAzureLocation(location),
		Subscription: subscription,
		Tenant:       tenant,
		Source:       "azure-cli",
	}
}

// normalizeAzureLocation canonicalizes an Azure location to its slug form
// (lowercase, no spaces). `az` returns `location` inconsistently: most ARM
// APIs return the slug (e.g. `westeurope`, `eastus2`) while some surface the
// display name (`West Europe`, `East US 2`). Azure location slugs are all
// `[a-z0-9]+` with no separators, so lowercasing and stripping spaces yields
// the canonical form for every standard Azure region.
func normalizeAzureLocation(loc string) string {
	if loc == "" {
		return ""
	}
	return strings.ToLower(strings.ReplaceAll(loc, " ", ""))
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

// TransformVNets converts Azure VirtualNetworks into OSIRIS JSON resources.
// Peerings are passed in to embed peering summary in VNet properties.
func TransformVNets(vnets []VirtualNetwork, peerings []VNetPeering, sub SubscriptionInfo) []sdk.Resource {
	// Build peering lookup: VNet ARM ID -> peerings for that VNet.
	peeringsByVNet := make(map[string][]VNetPeering)
	for _, p := range peerings {
		peeringsByVNet[p.VNetID()] = append(peeringsByVNet[p.VNetID()], p)
	}
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
		r.Tags = v.Tags

		props := map[string]any{
			"resource_group": v.ResourceGroup,
		}
		if len(v.AddressSpace.AddressPrefixes) > 0 {
			props["address_space"] = v.AddressSpace.AddressPrefixes
		}
		if len(v.DhcpOptions.DNSServers) > 0 {
			props["dns_servers"] = v.DhcpOptions.DNSServers
		}
		props["subnet_count"] = len(v.Subnets)
		if v.EnableDdosProtection {
			props["enable_ddos_protection"] = true
		}
		if vnetPeerings := peeringsByVNet[v.ID]; len(vnetPeerings) > 0 {
			peerList := make([]map[string]any, 0, len(vnetPeerings))
			for _, p := range vnetPeerings {
				entry := map[string]any{
					"name":          p.Name,
					"peering_state": p.PeeringState,
				}
				if p.RemoteVNetID() != "" {
					entry["remote_vnet_id"] = p.RemoteVNetID()
				}
				if p.AllowGatewayTransit {
					entry["allow_gateway_transit"] = true
				}
				if p.UseRemoteGateways {
					entry["use_remote_gateways"] = true
				}
				if p.AllowForwardedTraffic {
					entry["allow_forwarded_traffic"] = true
				}
				peerList = append(peerList, entry)
			}
			props["peerings"] = peerList
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
		if s.RouteTableId() != "" {
			props["route_table_id"] = s.RouteTableId()
		}
		if s.NSGId() != "" {
			props["nsg_id"] = s.NSGId()
		}
		if s.NatGateway != nil && s.NatGateway.ID != "" {
			props["nat_gateway_id"] = s.NatGateway.ID
		}
		if len(s.Delegations) > 0 {
			delegations := make([]string, 0, len(s.Delegations))
			for _, d := range s.Delegations {
				if d.ServiceName != "" {
					delegations = append(delegations, d.ServiceName)
				}
			}
			if len(delegations) > 0 {
				props["delegations"] = delegations
			}
		}
		if len(s.ServiceEndpoints) > 0 {
			eps := make([]string, 0, len(s.ServiceEndpoints))
			for _, ep := range s.ServiceEndpoints {
				if ep.Service != "" {
					eps = append(eps, ep.Service)
				}
			}
			if len(eps) > 0 {
				props["service_endpoints"] = eps
			}
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
		r.Tags = n.Tags

		props := map[string]any{
			"resource_group":       n.ResourceGroup,
			"enable_ip_forwarding": n.EnableIPForwarding,
			"primary":              n.Primary,
		}
		if n.NSGId() != "" {
			props["nsg_id"] = n.NSGId()
		} else {
			props["nsg_id"] = nil
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
				if ip.SubnetID() != "" {
					ipMap["subnet_id"] = ip.SubnetID()
				}
				ips = append(ips, ipMap)
			}
			props["ip_configurations"] = ips
		}
		r.Properties = props

		// Extensions: Azure-specific NIC fields.
		ext := map[string]any{}
		if n.EnableAcceleratedNetworking {
			ext["enable_accelerated_networking"] = true
		}
		if len(n.EffectiveRoutes) > 0 {
			routes := make([]map[string]any, 0, len(n.EffectiveRoutes))
			for _, er := range n.EffectiveRoutes {
				entry := map[string]any{
					"source":        er.Source,
					"state":         er.State,
					"next_hop_type": er.NextHopType,
				}
				if len(er.AddressPrefix) > 0 {
					entry["prefix"] = er.AddressPrefix
				}
				if len(er.NextHopIPAddress) > 0 {
					entry["next_hop_ip"] = er.NextHopIPAddress
				}
				if er.DisableBgpPropagation {
					entry["disable_bgp_propagation"] = true
				}
				routes = append(routes, entry)
			}
			ext["effective_routes"] = routes
		}
		if len(ext) > 0 {
			r.Extensions = map[string]any{extensionNamespace: ext}
		}

		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformNSGs converts Azure NetworkSecurityGroups into OSIRIS JSON resources.
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
		r.Tags = n.Tags

		props := map[string]any{
			"resource_group": n.ResourceGroup,
			"rule_count":     len(n.SecurityRules),
		}
		if subnetIDs := n.SubnetIDs(); len(subnetIDs) > 0 {
			props["subnet_ids"] = subnetIDs
		}
		if nicIDs := n.NetworkInterfaceIDs(); len(nicIDs) > 0 {
			props["nic_ids"] = nicIDs
		}
		r.Properties = props

		// Extensions: NSG security rules and default rules.
		ext := map[string]any{}
		if len(n.SecurityRules) > 0 {
			ext["security_rules"] = transformNSGRules(n.SecurityRules)
		}
		if len(n.DefaultSecurityRules) > 0 {
			ext["default_security_rules"] = transformNSGRules(n.DefaultSecurityRules)
		}
		if len(ext) > 0 {
			r.Extensions = map[string]any{extensionNamespace: ext}
		}

		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformRouteTables converts Azure RouteTables into OSIRIS JSON resources.
// Returns resources and a map of route table ARM ID -> resource ID.
func TransformRouteTables(tables []RouteTable, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
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
		r.Tags = t.Tags

		props := map[string]any{
			"resource_group": t.ResourceGroup,
			"route_count":    len(t.Routes),
		}
		if subnetIDs := t.SubnetIDs(); len(subnetIDs) > 0 {
			props["subnets"] = subnetIDs
		}
		if len(t.Routes) > 0 {
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
func TransformPublicIPs(ips []PublicIPAddress, sub SubscriptionInfo) []sdk.Resource {
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
		r.Tags = p.Tags

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
		if p.SKU.Tier != "" {
			props["sku_tier"] = p.SKU.Tier
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources
}

// TransformLoadBalancers converts Azure LoadBalancers into OSIRIS JSON resources.
func TransformLoadBalancers(lbs []LoadBalancer, sub SubscriptionInfo) []sdk.Resource {
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
		r.Tags = lb.Tags

		props := map[string]any{
			"resource_group":     lb.ResourceGroup,
			"frontend_count":     len(lb.FrontendIPConfigurations),
			"backend_pool_count": len(lb.BackendAddressPools),
			"rule_count":         len(lb.LoadBalancingRules),
		}
		if lb.SKU.Name != "" {
			props["sku"] = lb.SKU.Name
		}
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
		r.Tags = pe.Tags
		props := map[string]any{
			"resource_group": pe.ResourceGroup,
		}
		if svcID := pe.TargetServiceID(); svcID != "" {
			props["private_link_service_id"] = svcID
		}
		if groupID := pe.TargetGroupID(); groupID != "" {
			props["group_id"] = groupID
		}
		if len(pe.CustomDNSConfigs) > 0 {
			configs := make([]map[string]any, 0, len(pe.CustomDNSConfigs))
			for _, c := range pe.CustomDNSConfigs {
				entry := map[string]any{}
				if c.FQDN != "" {
					entry["fqdn"] = c.FQDN
				}
				if len(c.IPAddresses) > 0 {
					entry["ip_addresses"] = c.IPAddresses
				}
				if len(entry) > 0 {
					configs = append(configs, entry)
				}
			}
			if len(configs) > 0 {
				props["custom_dns_configs"] = configs
			}
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources
}

// TransformVNetGateways converts Azure VNetGateways into OSIRIS resources.
// Gateway connections are passed in to embed connection summary in gateway properties.
func TransformVNetGateways(gws []VNetGateway, gwConns []GatewayConnection, sub SubscriptionInfo) []sdk.Resource {
	// Build connection lookup: gateway ARM ID -> connections.
	connsByGW := make(map[string][]GatewayConnection)
	for _, gc := range gwConns {
		if gwID := gc.VirtualNetworkGateway1ID(); gwID != "" {
			connsByGW[gwID] = append(connsByGW[gwID], gc)
		}
	}

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
		r.Tags = gw.Tags

		props := map[string]any{
			"resource_group": gw.ResourceGroup,
			"gateway_type":   gw.GatewayType,
		}
		if gw.SKU.Name != "" {
			props["sku"] = gw.SKU.Name
		}
		if gw.VPNType != "" {
			props["vpn_type"] = gw.VPNType
		}
		if gw.EnableBGP {
			props["bgp_enabled"] = true
		}
		if gw.ActiveActive {
			props["active_active"] = true
		}
		if conns := connsByGW[gw.ID]; len(conns) > 0 {
			connList := make([]map[string]any, 0, len(conns))
			for _, gc := range conns {
				entry := map[string]any{
					"name":            gc.Name,
					"connection_type": gc.ConnectionType,
				}
				if gc.PeerID() != "" {
					entry["peer_id"] = gc.PeerID()
				}
				connList = append(connList, entry)
			}
			props["connections"] = connList
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
		r.Tags = gw.Tags
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
		r.Tags = fw.Tags
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
		r.Tags = gw.Tags
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
		if len(z.Links) > 0 {
			links := make([]map[string]any, 0, len(z.Links))
			for _, link := range z.Links {
				entry := map[string]any{
					"name":                 link.Name,
					"registration_enabled": link.RegistrationEnabled,
				}
				if link.VirtualNetworkID() != "" {
					entry["virtual_network_id"] = link.VirtualNetworkID()
				}
				links = append(links, entry)
			}
			props["virtual_network_links"] = links
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
		r.Tags = c.Tags

		props := map[string]any{
			"resource_group": c.ResourceGroup,
		}
		if c.CircuitProvisioningState != "" {
			props["circuit_state"] = c.CircuitProvisioningState
		}
		if c.ServiceProviderProvisioningState != "" {
			props["provider_state"] = c.ServiceProviderProvisioningState
		}
		if c.ServiceProviderProperties != nil {
			if c.ServiceProviderProperties.BandwidthInMbps > 0 {
				props["bandwidth_mbps"] = c.ServiceProviderProperties.BandwidthInMbps
			}
			if c.ServiceProviderProperties.PeeringLocation != "" {
				props["peering_location"] = c.ServiceProviderProperties.PeeringLocation
			}
		}
		r.Properties = props

		// Extensions: Azure-specific ExpressRoute details.
		ext := map[string]any{}
		if c.SKU.Name != "" {
			ext["sku"] = c.SKU.Name
		}
		if c.SKU.Tier != "" {
			ext["sku_tier"] = c.SKU.Tier
		}
		if c.ServiceProviderProperties != nil && c.ServiceProviderProperties.ServiceProviderName != "" {
			ext["service_provider"] = c.ServiceProviderProperties.ServiceProviderName
		}
		if len(c.Peerings) > 0 {
			var peerings []map[string]any
			for _, p := range c.Peerings {
				pm := map[string]any{
					"name":         p.Name,
					"peering_type": p.PeeringType,
					"state":        p.State,
				}
				if p.PeerASN != 0 {
					pm["peer_asn"] = p.PeerASN
				}
				if p.VlanID != 0 {
					pm["vlan_id"] = p.VlanID
				}
				if p.PrimaryPeerAddressPrefix != "" {
					pm["primary_peer_address_prefix"] = p.PrimaryPeerAddressPrefix
				}
				if p.SecondaryPeerAddressPrefix != "" {
					pm["secondary_peer_address_prefix"] = p.SecondaryPeerAddressPrefix
				}
				peerings = append(peerings, pm)
			}
			ext["peerings"] = peerings
		}
		if len(ext) > 0 {
			r.Extensions = map[string]any{extensionNamespace: ext}
		}

		resources = append(resources, r)
	}
	return resources
}

// TransformAppServicePlans converts Azure App Service Plans (Microsoft.Web/serverfarms)
// into OSIRIS JSON resources. Returns resources and a map of ASP ARM ID -> resource ID
// so site->plan connections can be wired without an extra lookup.
func TransformAppServicePlans(plans []AppServicePlan, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(plans))

	for _, p := range plans {
		id := resourceID("osiris.azure.appserviceplan", p.ID)
		idMap[p.ID] = id

		prov := azureProvider(p.ID, "Microsoft.Web/serverfarms", p.Location, sub.SubscriptionID, sub.TenantID)

		r, err := sdk.NewResource(id, "osiris.azure.appserviceplan", prov)
		if err != nil {
			continue
		}
		r.Name = p.Name
		r.Status = mapAppServicePlanStatus(p.Status)
		r.Tags = p.Tags

		props := map[string]any{
			"resource_group": p.ResourceGroup,
		}
		if p.Kind != "" {
			props["kind"] = p.Kind
		}
		if p.SKU.Name != "" {
			props["sku"] = p.SKU.Name
		}
		if p.SKU.Tier != "" {
			props["sku_tier"] = p.SKU.Tier
		}
		if p.SKU.Size != "" {
			props["sku_size"] = p.SKU.Size
		}
		if p.SKU.Family != "" {
			props["sku_family"] = p.SKU.Family
		}
		if p.SKU.Capacity > 0 {
			props["sku_capacity"] = p.SKU.Capacity
		}
		if p.Reserved {
			props["linux"] = true
		}
		if p.PerSiteScaling {
			props["per_site_scaling"] = true
		}
		if p.ZoneRedundant {
			props["zone_redundant"] = true
		}
		if p.NumberOfWorkers > 0 {
			props["number_of_workers"] = p.NumberOfWorkers
		}
		if p.MaximumElasticWorkerCount > 0 {
			props["max_elastic_worker_count"] = p.MaximumElasticWorkerCount
		}
		if p.NumberOfSites > 0 {
			props["number_of_sites"] = p.NumberOfSites
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformWebApps converts Azure App Service sites (Microsoft.Web/sites) into
// OSIRIS JSON resources. Kind routing:
//
//	kind contains "functionapp" -> osiris.azure.functionapp
//	otherwise                   -> osiris.azure.webapp
//
// Returns resources and a map of site ARM ID -> resource ID for connection wiring.
func TransformWebApps(apps []WebApp, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(apps))

	for _, a := range apps {
		osirisType := "osiris.azure.webapp"
		if a.IsFunctionApp() {
			osirisType = "osiris.azure.functionapp"
		}
		id := resourceID(osirisType, a.ID)
		idMap[a.ID] = id

		prov := azureProvider(a.ID, "Microsoft.Web/sites", a.Location, sub.SubscriptionID, sub.TenantID)

		r, err := sdk.NewResource(id, osirisType, prov)
		if err != nil {
			continue
		}
		r.Name = a.Name
		r.Status = mapWebAppState(a.State, a.Enabled)
		r.Tags = a.Tags

		props := map[string]any{
			"resource_group": a.ResourceGroup,
		}
		if a.Kind != "" {
			props["kind"] = a.Kind
		}
		if a.State != "" {
			props["state"] = a.State
		}
		props["enabled"] = a.Enabled
		if a.DefaultHostName != "" {
			props["default_hostname"] = a.DefaultHostName
		}
		if len(a.HostNames) > 0 {
			props["hostnames"] = a.HostNames
		}
		props["https_only"] = a.HTTPSOnly
		if a.ClientCertEnabled {
			props["client_cert_enabled"] = true
			if a.ClientCertMode != "" {
				props["client_cert_mode"] = a.ClientCertMode
			}
		}
		if hp := a.HostPlanID(); hp != "" {
			props["host_plan_id"] = hp
		}
		if a.VirtualNetworkSubnetID != "" {
			props["vnet_integration_subnet_id"] = a.VirtualNetworkSubnetID
		}
		if a.PublicNetworkAccess != "" {
			props["public_network_access"] = a.PublicNetworkAccess
		}
		if a.InboundIPAddress != "" {
			props["inbound_ip"] = a.InboundIPAddress
		}
		if a.OutboundIPAddresses != "" {
			props["outbound_ips"] = splitCSV(a.OutboundIPAddresses)
		}
		if a.RedundancyMode != "" && strings.ToLower(a.RedundancyMode) != "none" {
			props["redundancy_mode"] = a.RedundancyMode
		}
		if a.ManagedEnvironmentID != "" {
			props["managed_environment_id"] = a.ManagedEnvironmentID
		}
		if cfg := a.SiteConfig; cfg != nil {
			if cfg.LinuxFxVersion != "" {
				props["linux_fx_version"] = cfg.LinuxFxVersion
			}
			if cfg.WindowsFxVersion != "" {
				props["windows_fx_version"] = cfg.WindowsFxVersion
			}
			if cfg.NumberOfWorkers > 0 {
				props["number_of_workers"] = cfg.NumberOfWorkers
			}
			if cfg.AlwaysOn {
				props["always_on"] = true
			}
			if cfg.HTTP20Enabled {
				props["http20_enabled"] = true
			}
			if cfg.MinTLSVersion != "" {
				props["min_tls_version"] = cfg.MinTLSVersion
			}
			if a.IsFunctionApp() && cfg.FunctionAppScaleLimit > 0 {
				props["function_scale_limit"] = cfg.FunctionAppScaleLimit
			}
			if cfg.MinimumElasticInstanceCount > 0 {
				props["min_elastic_instance_count"] = cfg.MinimumElasticInstanceCount
			}
			if cfg.ACRUseManagedIdentityCreds {
				props["acr_use_managed_identity"] = true
			}
		}
		r.Properties = props

		// Extensions: Azure-specific site fields (identity, VNet routing flags, PE connections).
		ext := map[string]any{}
		if id := a.Identity; id != nil && id.Type != "" {
			idMap := map[string]any{"type": id.Type}
			if id.PrincipalID != "" {
				idMap["principal_id"] = id.PrincipalID
			}
			if ids := id.UserAssignedIdentityIDs(); len(ids) > 0 {
				idMap["user_assigned_identity_ids"] = ids
			}
			ext["identity"] = idMap
		}
		if r := a.OutboundVnetRouting; r != nil {
			routing := map[string]any{}
			if r.AllTraffic {
				routing["all_traffic"] = true
			}
			if r.ApplicationTraffic {
				routing["application_traffic"] = true
			}
			if r.ContentShareTraffic {
				routing["content_share_traffic"] = true
			}
			if r.ImagePullTraffic {
				routing["image_pull_traffic"] = true
			}
			if r.BackupRestoreTraffic {
				routing["backup_restore_traffic"] = true
			}
			if r.ManagedIdentityTraffic {
				routing["managed_identity_traffic"] = true
			}
			if len(routing) > 0 {
				ext["outbound_vnet_routing"] = routing
			}
		}
		if len(a.PrivateEndpointConnections) > 0 {
			peIDs := make([]string, 0, len(a.PrivateEndpointConnections))
			for _, pec := range a.PrivateEndpointConnections {
				if peID := pec.PrivateEndpointID(); peID != "" {
					peIDs = append(peIDs, peID)
				}
			}
			if len(peIDs) > 0 {
				ext["private_endpoint_ids"] = peIDs
			}
		}
		if aiID := appInsightsFromTags(a.Tags); aiID != "" {
			ext["app_insights_id"] = aiID
		}
		if len(ext) > 0 {
			r.Extensions = map[string]any{extensionNamespace: ext}
		}

		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformApplicationSecurityGroups converts Azure ASGs into OSIRIS JSON resources.
// Returns resources and a map of ASG ARM ID -> resource ID for NIC->ASG wiring.
func TransformApplicationSecurityGroups(asgs []ApplicationSecurityGroup, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(asgs))

	for _, a := range asgs {
		id := resourceID("osiris.azure.asg", a.ID)
		idMap[a.ID] = id

		prov := azureProvider(a.ID, "Microsoft.Network/applicationSecurityGroups", a.Location, sub.SubscriptionID, sub.TenantID)

		r, err := sdk.NewResource(id, "osiris.azure.asg", prov)
		if err != nil {
			continue
		}
		r.Name = a.Name
		r.Status = "active"
		r.Tags = a.Tags
		r.Properties = map[string]any{
			"resource_group": a.ResourceGroup,
		}
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformStorageAccounts converts Azure storage accounts into OSIRIS JSON
// resources of type osiris.azure.storage. Returns resources and the ARM ID ->
// resource ID map for wiring private endpoint connections.
func TransformStorageAccounts(accts []StorageAccount, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(accts))

	for _, a := range accts {
		id := resourceID("osiris.azure.storage", a.ID)
		idMap[a.ID] = id

		prov := azureProvider(a.ID, "Microsoft.Storage/storageAccounts", a.Location, sub.SubscriptionID, sub.TenantID)

		r, err := sdk.NewResource(id, "osiris.azure.storage", prov)
		if err != nil {
			continue
		}
		r.Name = a.Name
		r.Status = mapProvisioningState(a.ProvisioningState)
		r.Tags = a.Tags

		props := map[string]any{
			"resource_group": a.ResourceGroup,
		}
		if a.Kind != "" {
			props["kind"] = a.Kind
		}
		if a.SKU.Name != "" {
			props["sku"] = a.SKU.Name
		}
		if a.SKU.Tier != "" {
			props["sku_tier"] = a.SKU.Tier
		}
		if a.AccessTier != "" {
			props["access_tier"] = a.AccessTier
		}
		props["https_only"] = a.EnableHTTPSTrafficOnly
		if a.MinimumTLSVersion != "" {
			props["min_tls_version"] = a.MinimumTLSVersion
		}
		if a.PublicNetworkAccess != "" {
			props["public_network_access"] = a.PublicNetworkAccess
		}
		if a.IsHnsEnabled {
			props["hierarchical_namespace"] = true
		}
		if a.AllowBlobPublicAccess != nil {
			props["allow_blob_public_access"] = *a.AllowBlobPublicAccess
		}
		if a.AllowSharedKeyAccess != nil {
			props["allow_shared_key_access"] = *a.AllowSharedKeyAccess
		}
		if a.AllowCrossTenantReplication != nil {
			props["allow_cross_tenant_replication"] = *a.AllowCrossTenantReplication
		}
		r.Properties = props

		// Extensions: Azure-specific fields (endpoints, network ACLs, encryption, PE IDs).
		ext := map[string]any{}
		if ep := a.PrimaryEndpoints; ep != nil {
			endpoints := map[string]any{}
			if ep.Blob != "" {
				endpoints["blob"] = ep.Blob
			}
			if ep.Queue != "" {
				endpoints["queue"] = ep.Queue
			}
			if ep.Table != "" {
				endpoints["table"] = ep.Table
			}
			if ep.File != "" {
				endpoints["file"] = ep.File
			}
			if ep.Web != "" {
				endpoints["web"] = ep.Web
			}
			if ep.Dfs != "" {
				endpoints["dfs"] = ep.Dfs
			}
			if len(endpoints) > 0 {
				ext["endpoints"] = endpoints
			}
		}
		if n := a.NetworkRuleSet; n != nil {
			acls := map[string]any{}
			if n.DefaultAction != "" {
				acls["default_action"] = n.DefaultAction
			}
			if n.Bypass != "" {
				acls["bypass"] = n.Bypass
			}
			if len(n.IPRules) > 0 {
				ips := make([]string, 0, len(n.IPRules))
				for _, rule := range n.IPRules {
					if rule.Value != "" {
						ips = append(ips, rule.Value)
					}
				}
				if len(ips) > 0 {
					acls["ip_rules"] = ips
				}
			}
			if len(n.VirtualNetworkRules) > 0 {
				subnets := make([]string, 0, len(n.VirtualNetworkRules))
				for _, rule := range n.VirtualNetworkRules {
					if rule.ID != "" {
						subnets = append(subnets, rule.ID)
					}
				}
				if len(subnets) > 0 {
					acls["vnet_subnet_ids"] = subnets
				}
			}
			if len(acls) > 0 {
				ext["network_acls"] = acls
			}
		}
		if e := a.Encryption; e != nil {
			enc := map[string]any{}
			if e.KeySource != "" {
				enc["key_source"] = e.KeySource
			}
			if kv := e.KeyVaultProperties; kv != nil && kv.KeyVaultURI != "" {
				enc["keyvault_uri"] = kv.KeyVaultURI
				if kv.KeyName != "" {
					enc["keyvault_key_name"] = kv.KeyName
				}
			}
			if len(enc) > 0 {
				ext["encryption"] = enc
			}
		}
		if peIDs := collectPEIDs(a.PrivateEndpointConnections); len(peIDs) > 0 {
			ext["private_endpoint_ids"] = peIDs
		}
		if len(ext) > 0 {
			r.Extensions = map[string]any{extensionNamespace: ext}
		}

		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformKeyVaults converts Azure Key Vaults into OSIRIS JSON resources of
// type osiris.azure.keyvault. Returns resources and ARM ID -> resource ID map.
func TransformKeyVaults(vaults []KeyVault, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(vaults))

	for _, v := range vaults {
		id := resourceID("osiris.azure.keyvault", v.ID)
		idMap[v.ID] = id

		prov := azureProvider(v.ID, "Microsoft.KeyVault/vaults", v.Location, sub.SubscriptionID, sub.TenantID)

		r, err := sdk.NewResource(id, "osiris.azure.keyvault", prov)
		if err != nil {
			continue
		}
		r.Name = v.Name
		r.Status = "active"
		r.Tags = v.Tags

		props := map[string]any{
			"resource_group": v.ResourceGroup,
		}
		p := v.Properties
		if p != nil {
			r.Status = mapProvisioningState(p.ProvisioningState)
			if p.SKU.Name != "" {
				props["sku"] = p.SKU.Name
			}
			if p.SKU.Family != "" {
				props["sku_family"] = p.SKU.Family
			}
			if p.VaultURI != "" {
				props["vault_uri"] = p.VaultURI
			}
			if p.EnableRbacAuthorization {
				props["rbac_authorization"] = true
			}
			if p.EnableSoftDelete != nil {
				props["soft_delete_enabled"] = *p.EnableSoftDelete
			}
			if p.SoftDeleteRetentionInDays > 0 {
				props["soft_delete_retention_days"] = p.SoftDeleteRetentionInDays
			}
			if p.EnablePurgeProtection != nil {
				props["purge_protection_enabled"] = *p.EnablePurgeProtection
			}
			if p.EnabledForDeployment {
				props["enabled_for_deployment"] = true
			}
			if p.EnabledForDiskEncryption {
				props["enabled_for_disk_encryption"] = true
			}
			if p.EnabledForTemplateDeployment {
				props["enabled_for_template_deployment"] = true
			}
			if p.PublicNetworkAccess != "" {
				props["public_network_access"] = p.PublicNetworkAccess
			}
			if n := p.NetworkACLs; n != nil {
				acls := map[string]any{}
				if n.DefaultAction != "" {
					acls["default_action"] = n.DefaultAction
				}
				if n.Bypass != "" {
					acls["bypass"] = n.Bypass
				}
				if len(n.IPRules) > 0 {
					ips := make([]string, 0, len(n.IPRules))
					for _, rule := range n.IPRules {
						if rule.Value != "" {
							ips = append(ips, rule.Value)
						}
					}
					if len(ips) > 0 {
						acls["ip_rules"] = ips
					}
				}
				if len(n.VirtualNetworkRules) > 0 {
					subnets := make([]string, 0, len(n.VirtualNetworkRules))
					for _, rule := range n.VirtualNetworkRules {
						if rule.ID != "" {
							subnets = append(subnets, rule.ID)
						}
					}
					if len(subnets) > 0 {
						acls["vnet_subnet_ids"] = subnets
					}
				}
				if len(acls) > 0 {
					props["network_acls"] = acls
				}
			}
		}
		r.Properties = props

		// Extensions: tenant, PE IDs.
		if p != nil {
			ext := map[string]any{}
			if p.TenantID != "" {
				ext["tenant_id"] = p.TenantID
			}
			if peIDs := collectPEIDs(p.PrivateEndpointConnections); len(peIDs) > 0 {
				ext["private_endpoint_ids"] = peIDs
			}
			if len(ext) > 0 {
				r.Extensions = map[string]any{extensionNamespace: ext}
			}
		}

		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformContainerRegistries converts Azure Container Registries into
// OSIRIS JSON resources of type osiris.azure.containerregistry.
func TransformContainerRegistries(regs []ContainerRegistry, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(regs))

	for _, reg := range regs {
		id := resourceID("osiris.azure.containerregistry", reg.ID)
		idMap[reg.ID] = id

		prov := azureProvider(reg.ID, "Microsoft.ContainerRegistry/registries", reg.Location, sub.SubscriptionID, sub.TenantID)

		r, err := sdk.NewResource(id, "osiris.azure.containerregistry", prov)
		if err != nil {
			continue
		}
		r.Name = reg.Name
		r.Status = mapProvisioningState(reg.ProvisioningState)
		r.Tags = reg.Tags

		props := map[string]any{
			"resource_group": reg.ResourceGroup,
		}
		if reg.SKU.Name != "" {
			props["sku"] = reg.SKU.Name
		}
		if reg.SKU.Tier != "" {
			props["sku_tier"] = reg.SKU.Tier
		}
		if reg.LoginServer != "" {
			props["login_server"] = reg.LoginServer
		}
		if reg.AdminUserEnabled {
			props["admin_user_enabled"] = true
		}
		if reg.AnonymousPullEnabled {
			props["anonymous_pull_enabled"] = true
		}
		if reg.DataEndpointEnabled {
			props["data_endpoint_enabled"] = true
		}
		if reg.PublicNetworkAccess != "" {
			props["public_network_access"] = reg.PublicNetworkAccess
		}
		if reg.ZoneRedundancy != "" && strings.EqualFold(reg.ZoneRedundancy, "Enabled") {
			props["zone_redundant"] = true
		}
		r.Properties = props

		if peIDs := collectPEIDs(reg.PrivateEndpointConnections); len(peIDs) > 0 {
			r.Extensions = map[string]any{extensionNamespace: map[string]any{"private_endpoint_ids": peIDs}}
		}

		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformManagedIdentities converts Azure User-Assigned Managed Identities
// into OSIRIS JSON resources of type osiris.azure.managedidentity. Returns
// resources and ARM ID -> resource ID map so webapps/VMs can reference them.
func TransformManagedIdentities(ids []ManagedIdentity, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(ids))

	for _, mi := range ids {
		id := resourceID("osiris.azure.managedidentity", mi.ID)
		idMap[mi.ID] = id

		prov := azureProvider(mi.ID, "Microsoft.ManagedIdentity/userAssignedIdentities", mi.Location, sub.SubscriptionID, sub.TenantID)

		r, err := sdk.NewResource(id, "osiris.azure.managedidentity", prov)
		if err != nil {
			continue
		}
		r.Name = mi.Name
		r.Status = "active"
		r.Tags = mi.Tags
		r.Properties = map[string]any{
			"resource_group": mi.ResourceGroup,
		}

		ext := map[string]any{}
		if mi.PrincipalID != "" {
			ext["principal_id"] = mi.PrincipalID
		}
		if mi.ClientID != "" {
			ext["client_id"] = mi.ClientID
		}
		if mi.TenantID != "" {
			ext["tenant_id"] = mi.TenantID
		}
		if len(ext) > 0 {
			r.Extensions = map[string]any{extensionNamespace: ext}
		}

		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformDisks converts Azure managed disks into OSIRIS JSON resources of
// type osiris.azure.disk. Returns resources and ARM ID -> resource ID map so
// snapshots can reference the source disk.
func TransformDisks(disks []Disk, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(disks))

	for _, d := range disks {
		id := resourceID("osiris.azure.disk", d.ID)
		idMap[d.ID] = id

		prov := azureProvider(d.ID, "Microsoft.Compute/disks", d.Location, sub.SubscriptionID, sub.TenantID)

		r, err := sdk.NewResource(id, "osiris.azure.disk", prov)
		if err != nil {
			continue
		}
		r.Name = d.Name
		r.Status = mapDiskState(d.DiskState, d.ProvisioningState)
		r.Tags = d.Tags

		props := map[string]any{
			"resource_group": d.ResourceGroup,
		}
		if d.SKU.Name != "" {
			props["sku"] = d.SKU.Name
		}
		if d.SKU.Tier != "" {
			props["sku_tier"] = d.SKU.Tier
		}
		if d.DiskSizeGB > 0 {
			props["size_gb"] = d.DiskSizeGB
		}
		if d.DiskIOPSReadWrite > 0 {
			props["iops"] = d.DiskIOPSReadWrite
		}
		if d.DiskMBPSReadWrite > 0 {
			props["mbps"] = d.DiskMBPSReadWrite
		}
		if d.DiskState != "" {
			props["disk_state"] = d.DiskState
		}
		if d.OSType != "" {
			props["os_type"] = d.OSType
		}
		if d.ManagedBy != "" {
			props["managed_by"] = d.ManagedBy
		}
		if len(d.Zones) > 0 {
			props["zones"] = d.Zones
		}
		r.Properties = props

		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformSnapshots converts Azure disk snapshots into OSIRIS JSON resources
// of type osiris.azure.snapshot.
func TransformSnapshots(snaps []Snapshot, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(snaps))

	for _, s := range snaps {
		id := resourceID("osiris.azure.snapshot", s.ID)
		idMap[s.ID] = id

		prov := azureProvider(s.ID, "Microsoft.Compute/snapshots", s.Location, sub.SubscriptionID, sub.TenantID)

		r, err := sdk.NewResource(id, "osiris.azure.snapshot", prov)
		if err != nil {
			continue
		}
		r.Name = s.Name
		r.Status = mapProvisioningState(s.ProvisioningState)
		r.Tags = s.Tags

		props := map[string]any{
			"resource_group": s.ResourceGroup,
		}
		if s.SKU.Name != "" {
			props["sku"] = s.SKU.Name
		}
		if s.DiskSizeGB > 0 {
			props["size_gb"] = s.DiskSizeGB
		}
		if s.Incremental {
			props["incremental"] = true
		}
		if s.OSType != "" {
			props["os_type"] = s.OSType
		}
		if cd := s.CreationData; cd != nil {
			if cd.CreateOption != "" {
				props["create_option"] = cd.CreateOption
			}
			if cd.SourceResourceID != "" {
				props["source_resource_id"] = cd.SourceResourceID
			}
		}
		r.Properties = props

		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformApplicationInsights converts Microsoft.Insights/components into
// OSIRIS JSON resources of type osiris.azure.applicationinsights. Returns
// resources and ARM ID -> resource ID map so WebApps can be wired to their
// bound App Insights component and workspace-based AI can be linked to its
// Log Analytics workspace.
//
// Secret fields (InstrumentationKey, ConnectionString, AppID) are never
// emitted - they carry authentication material and add no topology value.
func TransformApplicationInsights(comps []ApplicationInsights, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(comps))

	for _, c := range comps {
		id := resourceID("osiris.azure.applicationinsights", c.ID)
		idMap[c.ID] = id

		prov := azureProvider(c.ID, "Microsoft.Insights/components", c.Location, sub.SubscriptionID, sub.TenantID)

		r, err := sdk.NewResource(id, "osiris.azure.applicationinsights", prov)
		if err != nil {
			continue
		}
		r.Name = c.Name
		r.Tags = c.Tags

		props := map[string]any{
			"resource_group": c.ResourceGroup,
		}
		if c.Kind != "" {
			props["kind"] = c.Kind
		}

		if p := c.Properties; p != nil {
			r.Status = mapProvisioningState(p.ProvisioningState)
			if p.ApplicationType != "" {
				props["application_type"] = p.ApplicationType
			}
			if p.IngestionMode != "" {
				props["ingestion_mode"] = p.IngestionMode
			}
			if p.RetentionInDays > 0 {
				props["retention_days"] = p.RetentionInDays
			}
			if p.SamplingPercentage > 0 {
				props["sampling_percentage"] = p.SamplingPercentage
			}
			if p.PublicNetworkAccessForIngestion != "" {
				props["public_network_access_ingestion"] = p.PublicNetworkAccessForIngestion
			}
			if p.PublicNetworkAccessForQuery != "" {
				props["public_network_access_query"] = p.PublicNetworkAccessForQuery
			}
			if p.DisableIPMasking {
				props["disable_ip_masking"] = true
			}
			if p.DisableLocalAuth {
				props["disable_local_auth"] = true
			}
		} else {
			r.Status = "active"
		}
		r.Properties = props

		if wsID := c.WorkspaceResourceID(); wsID != "" {
			r.Extensions = map[string]any{extensionNamespace: map[string]any{"workspace_resource_id": wsID}}
		}

		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformLogAnalyticsWorkspaces converts Microsoft.OperationalInsights/
// workspaces into OSIRIS JSON resources of type osiris.azure.loganalytics.
// Returns resources and ARM ID -> resource ID map so App Insights, VMs, AKS
// and other diagnostic-setting sources can be wired to their destination
// workspace.
//
// Shared keys (primary/secondary) are never emitted - they are auth material.
// The customer_id (workspace UUID) is not a secret; it is the query-scope ID
// used in KQL and appears in monitoring tooling, so it is kept under audit.
func TransformLogAnalyticsWorkspaces(wss []LogAnalyticsWorkspace, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(wss))

	for _, w := range wss {
		id := resourceID("osiris.azure.loganalytics", w.ID)
		idMap[w.ID] = id

		prov := azureProvider(w.ID, "Microsoft.OperationalInsights/workspaces", w.Location, sub.SubscriptionID, sub.TenantID)

		r, err := sdk.NewResource(id, "osiris.azure.loganalytics", prov)
		if err != nil {
			continue
		}
		r.Name = w.Name
		r.Status = mapProvisioningState(w.ProvisioningState)
		r.Tags = w.Tags

		props := map[string]any{
			"resource_group": w.ResourceGroup,
		}
		if w.SKU != nil && w.SKU.Name != "" {
			props["sku"] = w.SKU.Name
		}
		if w.RetentionInDays > 0 {
			props["retention_in_days"] = w.RetentionInDays
		}
		if w.PublicNetworkAccessForIngestion != "" {
			props["public_network_access_ingestion"] = w.PublicNetworkAccessForIngestion
		}
		if w.PublicNetworkAccessForQuery != "" {
			props["public_network_access_query"] = w.PublicNetworkAccessForQuery
		}
		if w.ForceCmkForQuery {
			props["force_cmk_for_query"] = true
		}
		if w.WorkspaceCapping != nil && w.WorkspaceCapping.DailyQuotaGB > 0 {
			props["daily_quota_gb"] = w.WorkspaceCapping.DailyQuotaGB
		}
		r.Properties = props

		if w.CustomerID != "" {
			r.Extensions = map[string]any{extensionNamespace: map[string]any{"customer_id": w.CustomerID}}
		}

		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformRecoveryServicesVaults converts Microsoft.RecoveryServices/vaults
// into OSIRIS JSON resources of type osiris.azure.recoveryservicesvault.
// Returns resources and ARM ID -> resource ID map for wiring private endpoint
// connections and protected-item edges.
func TransformRecoveryServicesVaults(vaults []RecoveryServicesVault, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(vaults))

	for _, v := range vaults {
		id := resourceID("osiris.azure.recoveryservicesvault", v.ID)
		idMap[v.ID] = id

		prov := azureProvider(v.ID, "Microsoft.RecoveryServices/vaults", v.Location, sub.SubscriptionID, sub.TenantID)

		r, err := sdk.NewResource(id, "osiris.azure.recoveryservicesvault", prov)
		if err != nil {
			continue
		}
		r.Name = v.Name
		r.Tags = v.Tags

		props := map[string]any{
			"resource_group": v.ResourceGroup,
		}
		if v.SKU != nil && v.SKU.Name != "" {
			props["sku"] = v.SKU.Name
		}
		if v.SKU != nil && v.SKU.Tier != "" {
			props["sku_tier"] = v.SKU.Tier
		}

		var peIDs []string
		if p := v.Properties; p != nil {
			r.Status = mapProvisioningState(p.ProvisioningState)
			if p.PublicNetworkAccess != "" {
				props["public_network_access"] = p.PublicNetworkAccess
			}
			if p.RedundancySettings != nil {
				if p.RedundancySettings.StandardTierStorageRedundancy != "" {
					props["storage_redundancy"] = p.RedundancySettings.StandardTierStorageRedundancy
				}
				if p.RedundancySettings.CrossRegionRestore != "" {
					props["cross_region_restore"] = p.RedundancySettings.CrossRegionRestore
				}
			}
			peIDs = collectPEIDs(p.PrivateEndpointConnections)
		} else {
			r.Status = "active"
		}
		props["protected_item_count"] = len(v.ProtectedItems)
		r.Properties = props

		if len(peIDs) > 0 {
			r.Extensions = map[string]any{extensionNamespace: map[string]any{"private_endpoint_ids": peIDs}}
		}

		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformBackupVaults converts Microsoft.DataProtection/backupVaults into
// OSIRIS JSON resources of type osiris.azure.backupvault. Returns resources
// and ARM ID -> resource ID map for wiring backup-instance edges.
func TransformBackupVaults(vaults []BackupVault, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(vaults))

	for _, v := range vaults {
		id := resourceID("osiris.azure.backupvault", v.ID)
		idMap[v.ID] = id

		prov := azureProvider(v.ID, "Microsoft.DataProtection/backupVaults", v.Location, sub.SubscriptionID, sub.TenantID)

		r, err := sdk.NewResource(id, "osiris.azure.backupvault", prov)
		if err != nil {
			continue
		}
		r.Name = v.Name
		r.Tags = v.Tags

		props := map[string]any{
			"resource_group": v.ResourceGroup,
		}

		if p := v.Properties; p != nil {
			r.Status = mapProvisioningState(p.ProvisioningState)
			if len(p.StorageSettings) > 0 {
				storages := make([]map[string]any, 0, len(p.StorageSettings))
				for _, s := range p.StorageSettings {
					entry := map[string]any{}
					if s.DatastoreType != "" {
						entry["datastore_type"] = s.DatastoreType
					}
					if s.Type != "" {
						entry["redundancy"] = s.Type
					}
					storages = append(storages, entry)
				}
				props["storage_settings"] = storages
			}
			if p.SecuritySettings != nil {
				if p.SecuritySettings.ImmutabilitySettings != nil && p.SecuritySettings.ImmutabilitySettings.State != "" {
					props["immutability"] = p.SecuritySettings.ImmutabilitySettings.State
				}
				if p.SecuritySettings.SoftDeleteSettings != nil && p.SecuritySettings.SoftDeleteSettings.State != "" {
					props["soft_delete"] = p.SecuritySettings.SoftDeleteSettings.State
					if p.SecuritySettings.SoftDeleteSettings.RetentionDurationInDays > 0 {
						props["soft_delete_retention_days"] = p.SecuritySettings.SoftDeleteSettings.RetentionDurationInDays
					}
				}
			}
		} else {
			r.Status = "active"
		}
		props["backup_instance_count"] = len(v.ProtectedInstances)
		r.Properties = props

		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformSQLServers converts Microsoft.Sql/servers into OSIRIS JSON resources
// of type osiris.azure.sqlserver. Administrator passwords are never emitted as per OSIRIS JSON spec chapter 13;
// the login name is treated as non-secret (it is a user principal, not authentication material on its own).
func TransformSQLServers(servers []SQLServer, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(servers))

	for _, s := range servers {
		id := resourceID("osiris.azure.sqlserver", s.ID)
		idMap[s.ID] = id

		prov := azureProvider(s.ID, "Microsoft.Sql/servers", s.Location, sub.SubscriptionID, sub.TenantID)

		r, err := sdk.NewResource(id, "osiris.azure.sqlserver", prov)
		if err != nil {
			continue
		}
		r.Name = s.Name
		r.Tags = s.Tags

		props := map[string]any{
			"resource_group": s.ResourceGroup,
		}
		if s.Kind != "" {
			props["kind"] = s.Kind
		}
		if p := s.Properties; p != nil {
			r.Status = mapSQLServerState(p.State)
			if p.Version != "" {
				props["version"] = p.Version
			}
			if p.FullyQualifiedDomainName != "" {
				props["fqdn"] = p.FullyQualifiedDomainName
			}
			if p.AdministratorLogin != "" {
				props["administrator_login"] = p.AdministratorLogin
			}
			if p.PublicNetworkAccess != "" {
				props["public_network_access"] = p.PublicNetworkAccess
			}
			if p.MinimalTLSVersion != "" {
				props["minimal_tls_version"] = p.MinimalTLSVersion
			}
			if p.RestrictOutboundNetworkAccess != "" {
				props["restrict_outbound_network_access"] = p.RestrictOutboundNetworkAccess
			}
			if peIDs := collectPEIDs(p.PrivateEndpointConnections); len(peIDs) > 0 {
				r.Extensions = map[string]any{extensionNamespace: map[string]any{"private_endpoint_connection_ids": peIDs}}
			}
		} else {
			r.Status = "active"
		}
		props["database_count"] = len(s.Databases)
		r.Properties = props

		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformSQLDatabases converts Microsoft.Sql/servers/databases into
// OSIRIS JSON resources of type osiris.azure.sqldatabase. The implicit `master` database is skipped at collection time.
func TransformSQLDatabases(servers []SQLServer, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string)

	for _, s := range servers {
		for _, db := range s.Databases {
			id := resourceID("osiris.azure.sqldatabase", db.ID)
			idMap[db.ID] = id

			prov := azureProvider(db.ID, "Microsoft.Sql/servers/databases", db.Location, sub.SubscriptionID, sub.TenantID)

			r, err := sdk.NewResource(id, "osiris.azure.sqldatabase", prov)
			if err != nil {
				continue
			}
			r.Name = db.Name
			r.Tags = db.Tags

			props := map[string]any{
				"resource_group": db.ResourceGroup,
				"server_name":    s.Name,
				"server_id":      db.ServerID,
			}
			if db.Kind != "" {
				props["kind"] = db.Kind
			}
			if db.SKU != nil {
				if db.SKU.Name != "" {
					props["sku"] = db.SKU.Name
				}
				if db.SKU.Tier != "" {
					props["tier"] = db.SKU.Tier
				}
				if db.SKU.Capacity > 0 {
					props["capacity"] = db.SKU.Capacity
				}
				if db.SKU.Family != "" {
					props["family"] = db.SKU.Family
				}
			}
			if p := db.Properties; p != nil {
				r.Status = mapSQLDatabaseStatus(p.Status)
				if p.Collation != "" {
					props["collation"] = p.Collation
				}
				if p.MaxSizeBytes > 0 {
					props["max_size_bytes"] = p.MaxSizeBytes
				}
				if p.ZoneRedundant {
					props["zone_redundant"] = true
				}
				if p.ReadScale != "" {
					props["read_scale"] = p.ReadScale
				}
				if p.StorageAccountType != "" {
					props["storage_account_type"] = p.StorageAccountType
				}
			} else {
				r.Status = "active"
			}
			r.Properties = props

			resources = append(resources, r)
		}
	}
	return resources, idMap
}

// TransformPostgreSQLServers converts Microsoft.DBforPostgreSQL/flexibleServers
// into OSIRIS JSON resources of type osiris.azure.postgresqlserver.
func TransformPostgreSQLServers(servers []PostgreSQLServer, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	return transformFlexServers(
		"osiris.azure.postgresqlserver",
		"Microsoft.DBforPostgreSQL/flexibleServers",
		flexServerIter(servers),
		sub,
	)
}

// TransformMySQLServers converts Microsoft.DBforMySQL/flexibleServers into
// OSIRIS JSON resources of type osiris.azure.mysqlserver.
func TransformMySQLServers(servers []MySQLServer, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	return transformFlexServers(
		"osiris.azure.mysqlserver",
		"Microsoft.DBforMySQL/flexibleServers",
		flexServerIterMySQL(servers),
		sub,
	)
}

// flexServerView is the common read view over PG and MySQL flexible servers
// so the transform logic can be shared.
type flexServerView struct {
	ID            string
	Name          string
	Location      string
	ResourceGroup string
	Tags          map[string]string
	SKU           *azFlexServerSKU
	Properties    *azFlexServerProperties
}

func flexServerIter(servers []PostgreSQLServer) []flexServerView {
	out := make([]flexServerView, len(servers))
	for i, s := range servers {
		out[i] = flexServerView{
			ID: s.ID, Name: s.Name, Location: s.Location,
			ResourceGroup: s.ResourceGroup, Tags: s.Tags,
			SKU: s.SKU, Properties: s.Properties,
		}
	}
	return out
}

func flexServerIterMySQL(servers []MySQLServer) []flexServerView {
	out := make([]flexServerView, len(servers))
	for i, s := range servers {
		out[i] = flexServerView{
			ID: s.ID, Name: s.Name, Location: s.Location,
			ResourceGroup: s.ResourceGroup, Tags: s.Tags,
			SKU: s.SKU, Properties: s.Properties,
		}
	}
	return out
}

func transformFlexServers(osirisType, armType string, views []flexServerView, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(views))

	for _, s := range views {
		id := resourceID(osirisType, s.ID)
		idMap[s.ID] = id

		prov := azureProvider(s.ID, armType, s.Location, sub.SubscriptionID, sub.TenantID)

		r, err := sdk.NewResource(id, osirisType, prov)
		if err != nil {
			continue
		}
		r.Name = s.Name
		r.Tags = s.Tags

		props := map[string]any{
			"resource_group": s.ResourceGroup,
		}
		if s.SKU != nil {
			if s.SKU.Name != "" {
				props["sku"] = s.SKU.Name
			}
			if s.SKU.Tier != "" {
				props["tier"] = s.SKU.Tier
			}
		}
		ext := map[string]any{}
		if p := s.Properties; p != nil {
			r.Status = mapFlexServerState(p.State)
			if p.Version != "" {
				props["version"] = p.Version
			}
			if p.AdministratorLogin != "" {
				props["administrator_login"] = p.AdministratorLogin
			}
			if p.FullyQualifiedDomainName != "" {
				props["fqdn"] = p.FullyQualifiedDomainName
			}
			if p.AvailabilityZone != "" {
				props["availability_zone"] = p.AvailabilityZone
			}
			if p.ReplicationRole != "" {
				props["replication_role"] = p.ReplicationRole
			}
			if st := p.Storage; st != nil {
				if st.StorageSizeGB > 0 {
					props["storage_size_gb"] = st.StorageSizeGB
				}
				if st.Tier != "" {
					props["storage_tier"] = st.Tier
				}
				if st.Iops > 0 {
					props["storage_iops"] = st.Iops
				}
				if st.AutoGrow != "" {
					props["storage_auto_grow"] = st.AutoGrow
				}
			}
			if n := p.Network; n != nil {
				if n.PublicNetworkAccess != "" {
					props["public_network_access"] = n.PublicNetworkAccess
				}
				if n.DelegatedSubnetResourceID != "" {
					props["delegated_subnet_id"] = n.DelegatedSubnetResourceID
				}
				if n.PrivateDNSZoneArmResourceID != "" {
					ext["private_dns_zone_id"] = n.PrivateDNSZoneArmResourceID
				}
			}
			if h := p.HighAvailability; h != nil {
				if h.Mode != "" {
					props["ha_mode"] = h.Mode
				}
				if h.StandbyAvailabilityZone != "" {
					props["ha_standby_zone"] = h.StandbyAvailabilityZone
				}
				if h.State != "" {
					props["ha_state"] = h.State
				}
			}
		} else {
			r.Status = "active"
		}
		r.Properties = props
		if len(ext) > 0 {
			r.Extensions = map[string]any{extensionNamespace: ext}
		}

		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformCosmosAccounts converts Microsoft.DocumentDB/databaseAccounts into
// OSIRIS JSON resources of type osiris.azure.cosmosaccount. Primary/secondary
// keys and connection strings are never collected (credentials, not topology).
func TransformCosmosAccounts(accts []CosmosAccount, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(accts))

	for _, a := range accts {
		id := resourceID("osiris.azure.cosmosaccount", a.ID)
		idMap[a.ID] = id

		prov := azureProvider(a.ID, "Microsoft.DocumentDB/databaseAccounts", a.Location, sub.SubscriptionID, sub.TenantID)

		r, err := sdk.NewResource(id, "osiris.azure.cosmosaccount", prov)
		if err != nil {
			continue
		}
		r.Name = a.Name
		r.Tags = a.Tags

		props := map[string]any{
			"resource_group": a.ResourceGroup,
		}
		if a.Kind != "" {
			props["kind"] = a.Kind
		}
		ext := map[string]any{}
		if p := a.Properties; p != nil {
			r.Status = mapProvisioningState(p.ProvisioningState)
			if p.DatabaseAccountOfferType != "" {
				props["offer_type"] = p.DatabaseAccountOfferType
			}
			if p.DocumentEndpoint != "" {
				props["document_endpoint"] = p.DocumentEndpoint
			}
			if p.PublicNetworkAccess != "" {
				props["public_network_access"] = p.PublicNetworkAccess
			}
			if p.EnableAutomaticFailover {
				props["enable_automatic_failover"] = true
			}
			if p.EnableMultipleWriteLocations {
				props["enable_multiple_write_locations"] = true
			}
			if p.IsVirtualNetworkFilterEnabled {
				props["virtual_network_filter_enabled"] = true
			}
			if p.EnableFreeTier {
				props["enable_free_tier"] = true
			}
			if p.DisableLocalAuth {
				props["disable_local_auth"] = true
			}
			if c := p.ConsistencyPolicy; c != nil && c.DefaultConsistencyLevel != "" {
				props["consistency_level"] = c.DefaultConsistencyLevel
			}
			if locs := flattenCosmosLocations(p.Locations); len(locs) > 0 {
				props["locations"] = locs
			}
			if caps := flattenCosmosCapabilities(p.Capabilities); len(caps) > 0 {
				props["capabilities"] = caps
			}
			if rules := flattenCosmosVNetRules(p.VirtualNetworkRules); len(rules) > 0 {
				ext["virtual_network_rules"] = rules
			}
			if peIDs := collectPEIDs(p.PrivateEndpointConnections); len(peIDs) > 0 {
				ext["private_endpoint_connection_ids"] = peIDs
			}
		} else {
			r.Status = "active"
		}
		r.Properties = props
		if len(ext) > 0 {
			r.Extensions = map[string]any{extensionNamespace: ext}
		}

		resources = append(resources, r)
	}
	return resources, idMap
}

func flattenCosmosLocations(locs []azCosmosLocation) []map[string]any {
	if len(locs) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(locs))
	for _, l := range locs {
		entry := map[string]any{}
		if l.LocationName != "" {
			entry["name"] = l.LocationName
		}
		entry["failover_priority"] = l.FailoverPriority
		if l.IsZoneRedundant {
			entry["zone_redundant"] = true
		}
		out = append(out, entry)
	}
	return out
}

func flattenCosmosCapabilities(caps []azCosmosCapability) []string {
	if len(caps) == 0 {
		return nil
	}
	out := make([]string, 0, len(caps))
	for _, c := range caps {
		if c.Name != "" {
			out = append(out, c.Name)
		}
	}
	return out
}

func flattenCosmosVNetRules(rules []azCosmosVNetRule) []map[string]any {
	if len(rules) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(rules))
	for _, r := range rules {
		if r.ID == "" {
			continue
		}
		entry := map[string]any{"subnet_id": r.ID}
		if r.IgnoreMissingVNetServiceEndpoint {
			entry["ignore_missing_vnet_service_endpoint"] = true
		}
		out = append(out, entry)
	}
	return out
}

// TransformRedisCaches converts Microsoft.Cache/Redis into OSIRIS JSON resources
// of type osiris.azure.redis. Access keys are never collected.
func TransformRedisCaches(caches []RedisCache, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(caches))

	for _, c := range caches {
		id := resourceID("osiris.azure.redis", c.ID)
		idMap[c.ID] = id

		prov := azureProvider(c.ID, "Microsoft.Cache/Redis", c.Location, sub.SubscriptionID, sub.TenantID)

		r, err := sdk.NewResource(id, "osiris.azure.redis", prov)
		if err != nil {
			continue
		}
		r.Name = c.Name
		r.Tags = c.Tags
		r.Status = mapProvisioningState(c.ProvisioningState)

		props := map[string]any{
			"resource_group": c.ResourceGroup,
		}
		if c.SKU != nil {
			if c.SKU.Name != "" {
				props["sku"] = c.SKU.Name
			}
			if c.SKU.Family != "" {
				props["family"] = c.SKU.Family
			}
			if c.SKU.Capacity > 0 {
				props["capacity"] = c.SKU.Capacity
			}
		}
		if c.RedisVersion != "" {
			props["redis_version"] = c.RedisVersion
		}
		if c.EnableNonSSLPort {
			props["enable_non_ssl_port"] = true
		}
		if c.MinimumTLSVersion != "" {
			props["minimum_tls_version"] = c.MinimumTLSVersion
		}
		if c.PublicNetworkAccess != "" {
			props["public_network_access"] = c.PublicNetworkAccess
		}
		if c.HostName != "" {
			props["host_name"] = c.HostName
		}
		if c.Port > 0 {
			props["port"] = c.Port
		}
		if c.SSLPort > 0 {
			props["ssl_port"] = c.SSLPort
		}
		if c.ShardCount > 0 {
			props["shard_count"] = c.ShardCount
		}
		if c.ReplicasPerMaster > 0 {
			props["replicas_per_master"] = c.ReplicasPerMaster
		}
		if c.SubnetID != "" {
			props["subnet_id"] = c.SubnetID
		}
		if c.StaticIP != "" {
			props["static_ip"] = c.StaticIP
		}
		if len(c.Zones) > 0 {
			props["zones"] = c.Zones
		}
		r.Properties = props

		ext := map[string]any{}
		if peIDs := collectPEIDs(c.PrivateEndpointConnections); len(peIDs) > 0 {
			ext["private_endpoint_connection_ids"] = peIDs
		}
		if len(ext) > 0 {
			r.Extensions = map[string]any{extensionNamespace: ext}
		}

		resources = append(resources, r)
	}
	return resources, idMap
}

// mapSQLServerState maps SQL server `properties.state` to OSIRIS JSON status.
func mapSQLServerState(state string) string {
	switch strings.ToLower(state) {
	case "ready":
		return "active"
	case "disabled":
		return "inactive"
	case "":
		return "active"
	default:
		return strings.ToLower(state)
	}
}

// mapSQLDatabaseStatus maps SQL database `properties.status` to OSIRIS JSON status.
func mapSQLDatabaseStatus(status string) string {
	switch strings.ToLower(status) {
	case "online":
		return "active"
	case "paused", "pausing", "scaling":
		return "transitioning"
	case "offline", "shutdown":
		return "inactive"
	case "":
		return "active"
	default:
		return strings.ToLower(status)
	}
}

// mapFlexServerState maps PG/MySQL flexible server state to OSIRIS JSON status.
func mapFlexServerState(state string) string {
	switch strings.ToLower(state) {
	case "ready":
		return "active"
	case "stopped", "disabled":
		return "inactive"
	case "starting", "stopping", "updating":
		return "transitioning"
	case "":
		return "active"
	default:
		return strings.ToLower(state)
	}
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
		r.Tags = vm.Tags

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
			Type:      "network.peering",
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

		conn, err := sdk.NewConnection(connID, "network.peering", sourceID, targetID)
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
// A gateway connection's peer (ExpressRoute circuit, remote gateway) often lives in a
// different subscription - typically a central connectivity/hub subscription. For those out-of-scope
// peers we emit a stub resource (mirroring the VNet peering pattern) so the topology edge
// is preserved without violating the document-builder invariant that every connection
// endpoint references an existing resource.
func TransformGatewayConnections(gwConns []GatewayConnection, allResourceIDs map[string]string) ([]sdk.Connection, []sdk.Resource) {
	var connections []sdk.Connection
	var stubs []sdk.Resource
	stubSeen := map[string]bool{}
	for _, gc := range gwConns {
		gw1ARM := gc.VirtualNetworkGateway1ID()
		sourceID, ok := allResourceIDs[gw1ARM]
		if !ok {
			continue
		}

		peerARM := gc.PeerID()
		if peerARM == "" {
			continue
		}
		targetID, ok := allResourceIDs[peerARM]
		if !ok {
			osirisType := gatewayPeerOsirisType(peerARM)
			if osirisType == "" {
				continue
			}
			targetID = resourceID(osirisType, peerARM)
			if !stubSeen[targetID] {
				stubSeen[targetID] = true
				prov := sdk.Provider{
					Name:         providerName,
					NativeID:     peerARM,
					Type:         gatewayPeerARMType(peerARM),
					Subscription: extractSubscriptionID(peerARM),
					Source:       "azure-cli",
				}
				stub, err := sdk.NewResource(targetID, osirisType, prov)
				if err == nil {
					stub.Name = extractLastSegment(peerARM)
					stub.Status = "unknown"
					stubs = append(stubs, stub)
				}
			}
		}

		connType := gatewayConnectionSubtype(gc.ConnectionType)
		canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
			Type:      connType,
			Direction: "bidirectional",
			Source:    sourceID,
			Target:    targetID,
		})
		connID := sdk.BuildConnectionID(canonicalKey, 16)

		conn, err := sdk.NewConnection(connID, connType, sourceID, targetID)
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
	return connections, stubs
}

// gatewayConnectionSubtype maps the Azure ConnectionType field to an OSIRIS JSON spec chapter 5 section 5.2.3.
// ExpressRoute uses BGP sessions; site-to-site, VNet-to-VNet, and P2S are all IPsec/IKEv2 VPNs.
func gatewayConnectionSubtype(azConnType string) string {
	switch strings.ToLower(azConnType) {
	case "expressroute":
		return "network.bgp"
	case "ipsec", "vnet2vnet", "vpnclient":
		return "network.vpn"
	default:
		return "network"
	}
}

// gatewayPeerARMType classifies an ARM ID by its /providers/<ns>/<type>/ segment.
// Returns the ARM provider/type string (e.g. Microsoft.Network/expressRouteCircuits)
// or empty when the ID can't be parsed.
func gatewayPeerARMType(armID string) string {
	idx := strings.Index(strings.ToLower(armID), "/providers/")
	if idx < 0 {
		return ""
	}
	rest := armID[idx+len("/providers/"):]
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) < 2 {
		return ""
	}
	return parts[0] + "/" + parts[1]
}

// gatewayPeerOsirisType maps the peer's ARM type to its OSIRIS type namespace.
func gatewayPeerOsirisType(armID string) string {
	switch strings.ToLower(gatewayPeerARMType(armID)) {
	case "microsoft.network/expressroutecircuits":
		return "osiris.azure.expressroute"
	case "microsoft.network/virtualnetworkgateways":
		return "osiris.azure.gateway.vnet"
	case "microsoft.network/localnetworkgateways":
		return "osiris.azure.gateway.local"
	default:
		return ""
	}
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

// TransformWebAppToPlanConnections creates contains connections from an App
// Service Plan to each of its hosted sites (web apps / function apps).
func TransformWebAppToPlanConnections(apps []WebApp, webAppIDMap, planIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, a := range apps {
		planArmID := a.HostPlanID()
		if planArmID == "" {
			continue
		}
		targetID, ok := webAppIDMap[a.ID]
		if !ok {
			continue
		}
		sourceID, ok := planIDMap[planArmID]
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
		conn.Name = fmt.Sprintf("%s -> %s", extractLastSegment(planArmID), a.Name)
		_ = conn.SetDirection("forward")
		connections = append(connections, conn)
	}
	return connections
}

// TransformWebAppToSubnetConnections creates network connections from a web/function app
// to its VNet integration subnet (regional VNet integration).
func TransformWebAppToSubnetConnections(apps []WebApp, webAppIDMap, subnetIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, a := range apps {
		if a.VirtualNetworkSubnetID == "" {
			continue
		}
		sourceID, ok := webAppIDMap[a.ID]
		if !ok {
			continue
		}
		targetID, ok := subnetIDMap[a.VirtualNetworkSubnetID]
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
		conn.Name = fmt.Sprintf("%s -> %s", a.Name, extractLastSegment(a.VirtualNetworkSubnetID))
		_ = conn.SetDirection("forward")
		connections = append(connections, conn)
	}
	return connections
}

// TransformPEToWebAppConnections creates network connections from a private
// endpoint to the web/function app it fronts. The binding lives on the site's
// privateEndpointConnections array.
func TransformPEToWebAppConnections(apps []WebApp, webAppIDMap, peIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, a := range apps {
		if len(a.PrivateEndpointConnections) == 0 {
			continue
		}
		targetID, ok := webAppIDMap[a.ID]
		if !ok {
			continue
		}
		for _, pec := range a.PrivateEndpointConnections {
			peArmID := pec.PrivateEndpointID()
			if peArmID == "" {
				continue
			}
			sourceID, ok := peIDMap[peArmID]
			if !ok {
				continue
			}

			canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
				Type:      "dependency",
				Direction: "forward",
				Source:    sourceID,
				Target:    targetID,
			})
			connID := sdk.BuildConnectionID(canonicalKey, 16)

			conn, err := sdk.NewConnection(connID, "dependency", sourceID, targetID)
			if err != nil {
				continue
			}
			conn.Name = fmt.Sprintf("%s -> %s", extractLastSegment(peArmID), a.Name)
			_ = conn.SetDirection("forward")
			connections = append(connections, conn)
		}
	}
	return connections
}

// TransformPEToStorageConnections creates network connections from each
// private endpoint to the storage account it fronts. The binding lives on the storage account's privateEndpointConnections array.
func TransformPEToStorageConnections(accts []StorageAccount, storageIDMap, peIDMap map[string]string) []sdk.Connection {
	return transformPEBoundConnections(
		collectPEBindings(func(yield func(targetArmID string, conns []azPrivateEndpointConnRef, name string)) {
			for _, a := range accts {
				yield(a.ID, a.PrivateEndpointConnections, a.Name)
			}
		}),
		storageIDMap, peIDMap, "dependency.storage",
	)
}

// TransformPEToKeyVaultConnections mirrors TransformPEToStorageConnections
// for Key Vaults. The PE list lives under properties.privateEndpointConnections.
func TransformPEToKeyVaultConnections(vaults []KeyVault, kvIDMap, peIDMap map[string]string) []sdk.Connection {
	return transformPEBoundConnections(
		collectPEBindings(func(yield func(targetArmID string, conns []azPrivateEndpointConnRef, name string)) {
			for _, v := range vaults {
				var conns []azPrivateEndpointConnRef
				if v.Properties != nil {
					conns = v.Properties.PrivateEndpointConnections
				}
				yield(v.ID, conns, v.Name)
			}
		}),
		kvIDMap, peIDMap, "dependency",
	)
}

// TransformPEToContainerRegistryConnections mirrors the pattern for ACR.
func TransformPEToContainerRegistryConnections(regs []ContainerRegistry, acrIDMap, peIDMap map[string]string) []sdk.Connection {
	return transformPEBoundConnections(
		collectPEBindings(func(yield func(targetArmID string, conns []azPrivateEndpointConnRef, name string)) {
			for _, r := range regs {
				yield(r.ID, r.PrivateEndpointConnections, r.Name)
			}
		}),
		acrIDMap, peIDMap, "dependency",
	)
}

// peBinding is an intermediate shape used to collect "resource X has these Private Endpoint (PE)connections" tuples from
// heterogeneous resource types so the common wiring loop can build connections once.
type peBinding struct {
	TargetArmID string
	Conns       []azPrivateEndpointConnRef
	Name        string
}

// collectPEBindings turns a per-resource-type iterator into a flat slice of peBinding tuples.
func collectPEBindings(iter func(yield func(targetArmID string, conns []azPrivateEndpointConnRef, name string))) []peBinding {
	var out []peBinding
	iter(func(targetArmID string, conns []azPrivateEndpointConnRef, name string) {
		out = append(out, peBinding{TargetArmID: targetArmID, Conns: conns, Name: name})
	})
	return out
}

// transformPEBoundConnections emits connections from each private endpoint
// referenced by a binding's privateEndpointConnections array to the target
// resource identified by targetArmID. connType lets callers attach an
// OSIRIS JSON spec chapter 5 section 5.2.3 ("dependency", "dependency.storage", "dependency.database").
func transformPEBoundConnections(bindings []peBinding, targetIDMap, peIDMap map[string]string, connType string) []sdk.Connection {
	var connections []sdk.Connection
	for _, b := range bindings {
		if len(b.Conns) == 0 {
			continue
		}
		targetID, ok := targetIDMap[b.TargetArmID]
		if !ok {
			continue
		}
		for _, pec := range b.Conns {
			peArmID := pec.PrivateEndpointID()
			if peArmID == "" {
				continue
			}
			sourceID, ok := peIDMap[peArmID]
			if !ok {
				continue
			}

			canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
				Type:      connType,
				Direction: "forward",
				Source:    sourceID,
				Target:    targetID,
			})
			connID := sdk.BuildConnectionID(canonicalKey, 16)

			conn, err := sdk.NewConnection(connID, connType, sourceID, targetID)
			if err != nil {
				continue
			}
			conn.Name = fmt.Sprintf("%s -> %s", extractLastSegment(peArmID), b.Name)
			_ = conn.SetDirection("forward")
			connections = append(connections, conn)
		}
	}
	return connections
}

// TransformSnapshotToDiskConnections wires each snapshot back to the disk
// (or other snapshot) it was taken from via creationData.sourceResourceId.
// Uses the "contains" type with direction=reverse so the topology reads
// "snapshot of disk X" rather than "disk X contains snapshot".
func TransformSnapshotToDiskConnections(snaps []Snapshot, snapshotIDMap, diskIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, s := range snaps {
		if s.CreationData == nil || s.CreationData.SourceResourceID == "" {
			continue
		}
		sourceID, ok := snapshotIDMap[s.ID]
		if !ok {
			continue
		}
		// Source can be a disk OR another snapshot (chained snapshots).
		targetID, ok := diskIDMap[s.CreationData.SourceResourceID]
		if !ok {
			targetID, ok = snapshotIDMap[s.CreationData.SourceResourceID]
			if !ok {
				continue
			}
		}

		canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
			Type:      "contains",
			Direction: "reverse",
			Source:    sourceID,
			Target:    targetID,
		})
		connID := sdk.BuildConnectionID(canonicalKey, 16)

		conn, err := sdk.NewConnection(connID, "contains", sourceID, targetID)
		if err != nil {
			continue
		}
		conn.Name = fmt.Sprintf("%s -> %s", s.Name, extractLastSegment(s.CreationData.SourceResourceID))
		_ = conn.SetDirection("reverse")
		connections = append(connections, conn)
	}
	return connections
}

// TransformDiskToVMConnections wires each managed disk to the VM that has
// attached it (via managedBy on the disk). Emits as contains/reverse so the
// topology reads "disk attached to VM".
func TransformDiskToVMConnections(disks []Disk, diskIDMap, vmIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, d := range disks {
		if d.ManagedBy == "" {
			continue
		}
		sourceID, ok := diskIDMap[d.ID]
		if !ok {
			continue
		}
		targetID, ok := vmIDMap[d.ManagedBy]
		if !ok {
			continue
		}

		canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
			Type:      "contains",
			Direction: "reverse",
			Source:    sourceID,
			Target:    targetID,
		})
		connID := sdk.BuildConnectionID(canonicalKey, 16)

		conn, err := sdk.NewConnection(connID, "contains", sourceID, targetID)
		if err != nil {
			continue
		}
		conn.Name = fmt.Sprintf("%s -> %s", d.Name, extractLastSegment(d.ManagedBy))
		_ = conn.SetDirection("reverse")
		connections = append(connections, conn)
	}
	return connections
}

// TransformNICToASGConnections creates network connections from a NIC to each
// Application Security Group referenced by any of its IP configurations.
func TransformNICToASGConnections(nics []NetworkInterface, nicIDMap, asgIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	seen := map[string]bool{}
	for _, n := range nics {
		sourceID, ok := nicIDMap[n.ID]
		if !ok {
			continue
		}
		for _, ip := range n.IPConfigurations {
			for _, asgArmID := range ip.ASGIDs() {
				targetID, ok := asgIDMap[asgArmID]
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
				conn.Name = fmt.Sprintf("%s -> %s", n.Name, extractLastSegment(asgArmID))
				_ = conn.SetDirection("forward")
				connections = append(connections, conn)
			}
		}
	}
	return connections
}

// TransformAppInsightsToWorkspaceConnections wires workspace-based App
// Insights components to their backing Log Analytics workspace via a
// "network" connection (diagnostic-data flow). Classic App Insights (without a workspace binding) is skipped.
func TransformAppInsightsToWorkspaceConnections(comps []ApplicationInsights, aiIDMap, laIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, c := range comps {
		wsArm := c.WorkspaceResourceID()
		if wsArm == "" {
			continue
		}
		sourceID, ok := aiIDMap[c.ID]
		if !ok {
			continue
		}
		targetID, ok := laIDMap[wsArm]
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
		conn.Name = fmt.Sprintf("%s -> %s", c.Name, extractLastSegment(wsArm))
		_ = conn.SetDirection("forward")
		connections = append(connections, conn)
	}
	return connections
}

// TransformWebAppToAppInsightsConnections wires App Service / Function App
// sites to their bound Application Insights component. The binding is
// declared on the site via the Azure portal `hidden-link` tag, which is
// parsed once at WebApp transform time and reused here to emit the edge.
func TransformWebAppToAppInsightsConnections(webApps []WebApp, webAppIDMap, aiIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, w := range webApps {
		aiArm := appInsightsFromTags(w.Tags)
		if aiArm == "" {
			continue
		}
		sourceID, ok := webAppIDMap[w.ID]
		if !ok {
			continue
		}
		targetID, ok := aiIDMap[aiArm]
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
		conn.Name = fmt.Sprintf("%s -> %s", w.Name, extractLastSegment(aiArm))
		_ = conn.SetDirection("forward")
		connections = append(connections, conn)
	}
	return connections
}

// TransformBackupProtectedItemConnections wires each backed-up resource to
// its Recovery Services Vault via a "network" edge (backup data flow).
// Protected items whose SourceResourceID is not among collected resources
// are emitted as stub edges only when the source resource is known; unknown
// sources are skipped silently (the backed-up resource may be in another
// subscription or outside collection scope).
//
// resourceIDMap carries the merged ARM ID -> OSIRIS JSON resource ID for all
// possible protected resource types (VM, SQL server, file share in storage, managed disk, etc.).
func TransformBackupProtectedItemConnections(vaults []RecoveryServicesVault, rsvIDMap, resourceIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	seen := map[string]bool{}
	for _, v := range vaults {
		vaultID, ok := rsvIDMap[v.ID]
		if !ok {
			continue
		}
		for _, item := range v.ProtectedItems {
			srcArm := item.SourceResourceID()
			if srcArm == "" {
				continue
			}
			srcID, ok := resourceIDMap[srcArm]
			if !ok {
				continue
			}
			pairKey := srcID + "|" + vaultID
			if seen[pairKey] {
				continue
			}
			seen[pairKey] = true

			canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
				Type:      "network",
				Direction: "forward",
				Source:    srcID,
				Target:    vaultID,
			})
			connID := sdk.BuildConnectionID(canonicalKey, 16)

			conn, err := sdk.NewConnection(connID, "network", srcID, vaultID)
			if err != nil {
				continue
			}
			conn.Name = fmt.Sprintf("%s -> %s", extractLastSegment(srcArm), v.Name)
			_ = conn.SetDirection("forward")
			connections = append(connections, conn)
		}
	}
	return connections
}

// TransformBackupInstanceConnections wires each backup instance in a Backup
// Vault to its source resource via a "network" edge. Same semantics as
// TransformBackupProtectedItemConnections but for the DataProtection service.
func TransformBackupInstanceConnections(vaults []BackupVault, bvIDMap, resourceIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	seen := map[string]bool{}
	for _, v := range vaults {
		vaultID, ok := bvIDMap[v.ID]
		if !ok {
			continue
		}
		for _, inst := range v.ProtectedInstances {
			srcArm := inst.SourceResourceID()
			if srcArm == "" {
				continue
			}
			srcID, ok := resourceIDMap[srcArm]
			if !ok {
				continue
			}
			pairKey := srcID + "|" + vaultID
			if seen[pairKey] {
				continue
			}
			seen[pairKey] = true

			canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
				Type:      "network",
				Direction: "forward",
				Source:    srcID,
				Target:    vaultID,
			})
			connID := sdk.BuildConnectionID(canonicalKey, 16)

			conn, err := sdk.NewConnection(connID, "network", srcID, vaultID)
			if err != nil {
				continue
			}
			conn.Name = fmt.Sprintf("%s -> %s", extractLastSegment(srcArm), v.Name)
			_ = conn.SetDirection("forward")
			connections = append(connections, conn)
		}
	}
	return connections
}

// TransformSQLServerContainsDatabaseConnections wires each SQL Server to its
// child SQL Databases with a `contains` edge.
func TransformSQLServerContainsDatabaseConnections(servers []SQLServer, sqlServerIDMap, sqlDatabaseIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, s := range servers {
		serverID, ok := sqlServerIDMap[s.ID]
		if !ok {
			continue
		}
		for _, db := range s.Databases {
			dbID, ok := sqlDatabaseIDMap[db.ID]
			if !ok {
				continue
			}
			canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
				Type:      "contains",
				Direction: "forward",
				Source:    serverID,
				Target:    dbID,
			})
			connID := sdk.BuildConnectionID(canonicalKey, 16)
			conn, err := sdk.NewConnection(connID, "contains", serverID, dbID)
			if err != nil {
				continue
			}
			conn.Name = fmt.Sprintf("%s contains %s", s.Name, db.Name)
			_ = conn.SetDirection("forward")
			connections = append(connections, conn)
		}
	}
	return connections
}

// TransformPEToSQLServerConnections wires Private Endpoints to SQL Servers via
// the server's properties.privateEndpointConnections.
func TransformPEToSQLServerConnections(servers []SQLServer, sqlIDMap, peIDMap map[string]string) []sdk.Connection {
	bindings := make([]peBinding, 0, len(servers))
	for _, s := range servers {
		if s.Properties == nil {
			continue
		}
		bindings = append(bindings, peBinding{
			TargetArmID: s.ID,
			Name:        s.Name,
			Conns:       s.Properties.PrivateEndpointConnections,
		})
	}
	return transformPEBoundConnections(bindings, sqlIDMap, peIDMap, "dependency.database")
}

// TransformPEToCosmosAccountConnections wires Private Endpoints to Cosmos DB
// accounts via the account's properties.privateEndpointConnections.
func TransformPEToCosmosAccountConnections(accts []CosmosAccount, cosmosIDMap, peIDMap map[string]string) []sdk.Connection {
	bindings := make([]peBinding, 0, len(accts))
	for _, a := range accts {
		if a.Properties == nil {
			continue
		}
		bindings = append(bindings, peBinding{
			TargetArmID: a.ID,
			Name:        a.Name,
			Conns:       a.Properties.PrivateEndpointConnections,
		})
	}
	return transformPEBoundConnections(bindings, cosmosIDMap, peIDMap, "dependency.database")
}

// TransformPEToRedisConnections wires Private Endpoints to Redis caches.
// Only Premium tier supports Private Link; Basic/Standard return an empty
// privateEndpointConnections slice, which the shared helper silently skips.
func TransformPEToRedisConnections(caches []RedisCache, redisIDMap, peIDMap map[string]string) []sdk.Connection {
	bindings := make([]peBinding, 0, len(caches))
	for _, c := range caches {
		bindings = append(bindings, peBinding{
			TargetArmID: c.ID,
			Name:        c.Name,
			Conns:       c.PrivateEndpointConnections,
		})
	}
	return transformPEBoundConnections(bindings, redisIDMap, peIDMap, "dependency.database")
}

// TransformFlexServerToSubnetConnections emits `network` edges from each
// PG / MySQL flexible server to its delegated subnet (VNet-integrated mode).
// Public-access servers without a delegated subnet are silently skipped.
func TransformFlexServerToSubnetConnections(pgs []PostgreSQLServer, mys []MySQLServer, serverIDMap, subnetIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	emit := func(serverArmID, serverName, subnetArmID string) {
		if subnetArmID == "" {
			return
		}
		srcID, ok := serverIDMap[serverArmID]
		if !ok {
			return
		}
		dstID, ok := subnetIDMap[subnetArmID]
		if !ok {
			return
		}
		canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
			Type:      "network",
			Direction: "forward",
			Source:    srcID,
			Target:    dstID,
		})
		connID := sdk.BuildConnectionID(canonicalKey, 16)
		conn, err := sdk.NewConnection(connID, "network", srcID, dstID)
		if err != nil {
			return
		}
		conn.Name = fmt.Sprintf("%s -> %s", serverName, extractLastSegment(subnetArmID))
		_ = conn.SetDirection("forward")
		connections = append(connections, conn)
	}
	for _, s := range pgs {
		if s.Properties == nil || s.Properties.Network == nil {
			continue
		}
		emit(s.ID, s.Name, s.Properties.Network.DelegatedSubnetResourceID)
	}
	for _, s := range mys {
		if s.Properties == nil || s.Properties.Network == nil {
			continue
		}
		emit(s.ID, s.Name, s.Properties.Network.DelegatedSubnetResourceID)
	}
	return connections
}

// TransformRedisToSubnetConnections emits `network` edges from each Premium
// tier Redis cache to its injected subnet. Basic/Standard caches have no subnet ID and are silently skipped.
func TransformRedisToSubnetConnections(caches []RedisCache, redisIDMap, subnetIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, c := range caches {
		if c.SubnetID == "" {
			continue
		}
		srcID, ok := redisIDMap[c.ID]
		if !ok {
			continue
		}
		dstID, ok := subnetIDMap[c.SubnetID]
		if !ok {
			continue
		}
		canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
			Type:      "network",
			Direction: "forward",
			Source:    srcID,
			Target:    dstID,
		})
		connID := sdk.BuildConnectionID(canonicalKey, 16)
		conn, err := sdk.NewConnection(connID, "network", srcID, dstID)
		if err != nil {
			continue
		}
		conn.Name = fmt.Sprintf("%s -> %s", c.Name, extractLastSegment(c.SubnetID))
		_ = conn.SetDirection("forward")
		connections = append(connections, conn)
	}
	return connections
}

// TransformPEToRecoveryServicesVaultConnections wires Private Endpoints to
// Recovery Services Vaults via the vault's privateEndpointConnections.
func TransformPEToRecoveryServicesVaultConnections(vaults []RecoveryServicesVault, rsvIDMap, peIDMap map[string]string) []sdk.Connection {
	bindings := make([]peBinding, 0, len(vaults))
	for _, v := range vaults {
		if v.Properties == nil {
			continue
		}
		bindings = append(bindings, peBinding{
			TargetArmID: v.ID,
			Name:        v.Name,
			Conns:       v.Properties.PrivateEndpointConnections,
		})
	}
	return transformPEBoundConnections(bindings, rsvIDMap, peIDMap, "dependency")
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

// TransformRegionGroups builds one container.region group per distinct
// provider.region value found on the resources, membering every resource in
// that region. Region "global" and empty-region resources are skipped -
// they are not geographically scoped. The boundary token is
// "<subscription-id>/<region>" so groups do not collide across subscriptions.
//
// OSIRIS JSON spec chapter 6 section 6.5 defines container.region as a standard group type for
// geographical or regional metadata distribution visualisation.
func TransformRegionGroups(resources []sdk.Resource, sub SubscriptionInfo) []sdk.Group {
	regions := map[string][]string{}
	for _, r := range resources {
		reg := r.Provider.Region
		if reg == "" || reg == "global" {
			continue
		}
		regions[reg] = append(regions[reg], r.ID)
	}
	groups := make([]sdk.Group, 0, len(regions))
	for reg, members := range regions {
		gid := sdk.GroupID(sdk.GroupIDInput{
			Type:          "container.region",
			BoundaryToken: sub.SubscriptionID + "/" + reg,
		})
		g, err := sdk.NewGroup(gid, "container.region")
		if err != nil {
			continue
		}
		g.Name = reg
		g.Properties = map[string]any{"region": reg}
		g.AddMembers(members...)
		groups = append(groups, g)
	}
	return groups
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

// mapAppServicePlanStatus converts Azure App Service Plan status to OSIRIS JSON status.
func mapAppServicePlanStatus(status string) string {
	switch strings.ToLower(status) {
	case "ready":
		return "active"
	case "pending", "creating":
		return "degraded"
	default:
		return "unknown"
	}
}

// mapWebAppState converts Azure App Service (site) state + enabled flag to OSIRIS JSON status.
func mapWebAppState(state string, enabled bool) string {
	if !enabled {
		return "inactive"
	}
	switch strings.ToLower(state) {
	case "running":
		return "active"
	case "stopped":
		return "inactive"
	default:
		return "unknown"
	}
}

// splitCSV splits a comma-separated string into a trimmed, non-empty slice.
func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		v := strings.TrimSpace(p)
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

// mapProvisioningState converts an ARM provisioningState value into an OSIRIS JSON
// status enum. "Succeeded" is the ARM steady state -> active.
func mapProvisioningState(state string) string {
	switch strings.ToLower(state) {
	case "succeeded":
		return "active"
	case "updating", "creating", "accepted":
		return "degraded"
	case "failed", "canceled":
		return "inactive"
	case "":
		return "active"
	default:
		return "unknown"
	}
}

// mapDiskState converts an Azure managed-disk diskState to an OSIRIS JSON status.
// ActiveSAS / Attached / ReadyToUpload all mean the disk is serving a client.
func mapDiskState(diskState, provisioningState string) string {
	switch strings.ToLower(diskState) {
	case "attached", "activesas", "activeupload", "readytoupload":
		return "active"
	case "unattached", "reserved":
		return "inactive"
	}
	return mapProvisioningState(provisioningState)
}

// collectPEIDs extracts the private endpoint ARM IDs from an array of
// privateEndpointConnections references (shared between webapps, storage, key vaults, registries).
func collectPEIDs(conns []azPrivateEndpointConnRef) []string {
	if len(conns) == 0 {
		return nil
	}
	ids := make([]string, 0, len(conns))
	for _, c := range conns {
		if id := c.PrivateEndpointID(); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

// appInsightsFromTags extracts the Application Insights ARM ID from the Azure
// portal hidden-link tag (`hidden-link: /app-insights-resource-id`).
func appInsightsFromTags(tags map[string]string) string {
	for k, v := range tags {
		if strings.EqualFold(strings.TrimSpace(k), "hidden-link: /app-insights-resource-id") {
			return v
		}
	}
	return ""
}

// groupIndex builds a map of group ID -> index in slice for efficient mutation.
func groupIndex(groups []sdk.Group) map[string]int {
	idx := make(map[string]int, len(groups))
	for i, g := range groups {
		idx[g.ID] = i
	}
	return idx
}

// transformNSGRules converts NSG security rules into OSIRIS JSON compatible maps.
func transformNSGRules(rules []NSGSecurityRule) []map[string]any {
	out := make([]map[string]any, 0, len(rules))
	for _, rule := range rules {
		entry := map[string]any{
			"name":      rule.Name,
			"priority":  rule.Priority,
			"direction": rule.Direction,
			"access":    rule.Access,
			"protocol":  rule.Protocol,
		}
		if rule.SourcePortRange != "" {
			entry["source_port_range"] = rule.SourcePortRange
		}
		if rule.DestinationPortRange != "" {
			entry["destination_port_range"] = rule.DestinationPortRange
		}
		if rule.SourceAddressPrefix != "" {
			entry["source_address_prefix"] = rule.SourceAddressPrefix
		}
		if rule.DestinationAddressPrefix != "" {
			entry["destination_address_prefix"] = rule.DestinationAddressPrefix
		}
		out = append(out, entry)
	}
	return out
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

// BuildPrivateEndpointIDMap builds a map of private endpoint ARM ID -> OSIRIS JSON resource ID.
func BuildPrivateEndpointIDMap(pes []PrivateEndpoint) map[string]string {
	m := make(map[string]string, len(pes))
	for _, pe := range pes {
		m[pe.ID] = resourceID("osiris.azure.privateendpoint", pe.ID)
	}
	return m
}

// BuildVMIDMap builds a map of VM ARM ID -> OSIRIS JSON resource ID, used for
// wiring disks -> VM via the disk's managedBy field.
func BuildVMIDMap(vms []VirtualMachine) map[string]string {
	m := make(map[string]string, len(vms))
	for _, vm := range vms {
		m[vm.ID] = resourceID("compute.vm", vm.ID)
	}
	return m
}

// TransformAKSClusters converts Microsoft.ContainerService/managedClusters into
// OSIRIS JSON resources of type osiris.azure.aks.cluster. Policy-like fields
// (admission configs, audit settings, identity profiles) are omitted per the topology-vs-IaC rule.
func TransformAKSClusters(clusters []AKSCluster, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(clusters))

	for _, c := range clusters {
		id := resourceID("osiris.azure.aks.cluster", c.ID)
		idMap[c.ID] = id

		prov := azureProvider(c.ID, "Microsoft.ContainerService/managedClusters", c.Location, sub.SubscriptionID, sub.TenantID)

		r, err := sdk.NewResource(id, "osiris.azure.aks.cluster", prov)
		if err != nil {
			continue
		}
		r.Name = c.Name
		r.Tags = c.Tags

		props := map[string]any{
			"resource_group": c.ResourceGroup,
		}
		if c.SKU != nil {
			if c.SKU.Name != "" {
				props["sku_name"] = c.SKU.Name
			}
			if c.SKU.Tier != "" {
				props["sku_tier"] = c.SKU.Tier
			}
		}
		if p := c.Properties; p != nil {
			r.Status = mapProvisioningState(p.ProvisioningState)
			if p.KubernetesVersion != "" {
				props["kubernetes_version"] = p.KubernetesVersion
			}
			if p.DNSPrefix != "" {
				props["dns_prefix"] = p.DNSPrefix
			}
			if p.FQDN != "" {
				props["fqdn"] = p.FQDN
			}
			if p.NodeResourceGroup != "" {
				props["node_resource_group"] = p.NodeResourceGroup
			}
			props["enable_rbac"] = p.EnableRBAC
			if p.NetworkProfile != nil {
				np := map[string]any{}
				if p.NetworkProfile.NetworkPlugin != "" {
					np["network_plugin"] = p.NetworkProfile.NetworkPlugin
				}
				if p.NetworkProfile.NetworkPolicy != "" {
					np["network_policy"] = p.NetworkProfile.NetworkPolicy
				}
				if p.NetworkProfile.ServiceCIDR != "" {
					np["service_cidr"] = p.NetworkProfile.ServiceCIDR
				}
				if p.NetworkProfile.PodCIDR != "" {
					np["pod_cidr"] = p.NetworkProfile.PodCIDR
				}
				if p.NetworkProfile.DNSServiceIP != "" {
					np["dns_service_ip"] = p.NetworkProfile.DNSServiceIP
				}
				if p.NetworkProfile.LoadBalancerSKU != "" {
					np["load_balancer_sku"] = p.NetworkProfile.LoadBalancerSKU
				}
				if p.NetworkProfile.OutboundType != "" {
					np["outbound_type"] = p.NetworkProfile.OutboundType
				}
				if len(np) > 0 {
					props["network_profile"] = np
				}
			}
			if p.APIServerAccessProfile != nil {
				props["private_cluster"] = p.APIServerAccessProfile.EnablePrivateCluster
				if p.APIServerAccessProfile.PrivateDNSZone != "" {
					props["private_dns_zone"] = p.APIServerAccessProfile.PrivateDNSZone
				}
			}
			if p.AADProfile != nil && p.AADProfile.Managed {
				props["aad_managed"] = true
				props["aad_azure_rbac"] = p.AADProfile.EnableAzureRBAC
			}
			props["agent_pool_count"] = len(c.AgentPools)
			if peIDs := collectPEIDs(p.PrivateEndpointConnections); len(peIDs) > 0 {
				r.Extensions = map[string]any{extensionNamespace: map[string]any{"private_endpoint_connection_ids": peIDs}}
			}
		} else {
			r.Status = "active"
		}
		r.Properties = props

		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformAKSAgentPools converts AKS agent pools into OSIRIS JSON resources of
// type osiris.azure.aks.nodepool. Pools are collected per-cluster so that each
// carries its ARM ID (needed for cluster -> nodepool contains edges).
func TransformAKSAgentPools(clusters []AKSCluster, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string)

	for _, c := range clusters {
		for _, p := range c.AgentPools {
			id := resourceID("osiris.azure.aks.nodepool", p.ID)
			idMap[p.ID] = id

			prov := azureProvider(p.ID, "Microsoft.ContainerService/managedClusters/agentPools", c.Location, sub.SubscriptionID, sub.TenantID)

			r, err := sdk.NewResource(id, "osiris.azure.aks.nodepool", prov)
			if err != nil {
				continue
			}
			r.Name = p.Name
			r.Status = mapProvisioningState(p.ProvisioningState)

			props := map[string]any{
				"cluster_name": c.Name,
				"cluster_id":   p.ClusterID,
			}
			if p.VMSize != "" {
				props["vm_size"] = p.VMSize
			}
			props["count"] = p.Count
			if p.EnableAutoScaling {
				props["autoscale"] = true
				props["min_count"] = p.MinCount
				props["max_count"] = p.MaxCount
			}
			if p.OSType != "" {
				props["os_type"] = p.OSType
			}
			if p.OSSKU != "" {
				props["os_sku"] = p.OSSKU
			}
			if p.Mode != "" {
				props["mode"] = p.Mode
			}
			if p.OrchestratorVer != "" {
				props["orchestrator_version"] = p.OrchestratorVer
			}
			if p.VNetSubnetID != "" {
				props["vnet_subnet_id"] = p.VNetSubnetID
			}
			if p.PodSubnetID != "" {
				props["pod_subnet_id"] = p.PodSubnetID
			}
			if len(p.AvailabilityZones) > 0 {
				props["availability_zones"] = p.AvailabilityZones
			}
			r.Properties = props

			resources = append(resources, r)
		}
	}
	return resources, idMap
}

// TransformContainerAppEnvironments converts Microsoft.App/managedEnvironments
// into OSIRIS JSON resources of type osiris.azure.containerapp.environment.
func TransformContainerAppEnvironments(envs []ContainerAppEnvironment, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(envs))

	for _, e := range envs {
		id := resourceID("osiris.azure.containerapp.environment", e.ID)
		idMap[e.ID] = id

		prov := azureProvider(e.ID, "Microsoft.App/managedEnvironments", e.Location, sub.SubscriptionID, sub.TenantID)

		r, err := sdk.NewResource(id, "osiris.azure.containerapp.environment", prov)
		if err != nil {
			continue
		}
		r.Name = e.Name
		r.Tags = e.Tags

		props := map[string]any{
			"resource_group": e.ResourceGroup,
		}
		if p := e.Properties; p != nil {
			r.Status = mapProvisioningState(p.ProvisioningState)
			if p.DefaultDomain != "" {
				props["default_domain"] = p.DefaultDomain
			}
			if p.StaticIP != "" {
				props["static_ip"] = p.StaticIP
			}
			if p.ZoneRedundant {
				props["zone_redundant"] = true
			}
			if p.VNetConfiguration != nil {
				if p.VNetConfiguration.InfrastructureSubnetID != "" {
					props["infrastructure_subnet_id"] = p.VNetConfiguration.InfrastructureSubnetID
				}
				props["internal"] = p.VNetConfiguration.Internal
			}
		} else {
			r.Status = "active"
		}
		r.Properties = props

		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformContainerApps converts Microsoft.App/containerApps into OSIRIS JSON
// resources of type osiris.azure.containerapp. Ingress secrets and revision history are excluded as per OSIRIS JSON spec chapter 13.
func TransformContainerApps(apps []ContainerApp, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(apps))

	for _, a := range apps {
		id := resourceID("osiris.azure.containerapp", a.ID)
		idMap[a.ID] = id

		prov := azureProvider(a.ID, "Microsoft.App/containerApps", a.Location, sub.SubscriptionID, sub.TenantID)

		r, err := sdk.NewResource(id, "osiris.azure.containerapp", prov)
		if err != nil {
			continue
		}
		r.Name = a.Name
		r.Tags = a.Tags

		props := map[string]any{
			"resource_group": a.ResourceGroup,
		}
		if envID := a.EnvironmentID(); envID != "" {
			props["environment_id"] = envID
		}
		if p := a.Properties; p != nil {
			r.Status = mapProvisioningState(p.ProvisioningState)
			if p.LatestRevisionName != "" {
				props["latest_revision_name"] = p.LatestRevisionName
			}
			if p.LatestRevisionFQDN != "" {
				props["latest_revision_fqdn"] = p.LatestRevisionFQDN
			}
			if p.WorkloadProfileName != "" {
				props["workload_profile_name"] = p.WorkloadProfileName
			}
			if p.Configuration != nil {
				if p.Configuration.ActiveRevisionsMode != "" {
					props["active_revisions_mode"] = p.Configuration.ActiveRevisionsMode
				}
				if ing := p.Configuration.Ingress; ing != nil {
					ingress := map[string]any{
						"external":       ing.External,
						"allow_insecure": ing.AllowInsecure,
					}
					if ing.TargetPort > 0 {
						ingress["target_port"] = ing.TargetPort
					}
					if ing.Transport != "" {
						ingress["transport"] = ing.Transport
					}
					if ing.FQDN != "" {
						ingress["fqdn"] = ing.FQDN
					}
					props["ingress"] = ingress
				}
			}
		} else {
			r.Status = "active"
		}
		r.Properties = props

		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformContainerGroups converts Microsoft.ContainerInstance/containerGroups
// (ACI) into OSIRIS JSON resources of type osiris.azure.containergroup.
// Container-level config (images, env vars, commands) is intentionally omitted as topology models the group, not the workload.
func TransformContainerGroups(groups []ContainerGroup, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(groups))

	for _, g := range groups {
		id := resourceID("osiris.azure.containergroup", g.ID)
		idMap[g.ID] = id

		prov := azureProvider(g.ID, "Microsoft.ContainerInstance/containerGroups", g.Location, sub.SubscriptionID, sub.TenantID)

		r, err := sdk.NewResource(id, "osiris.azure.containergroup", prov)
		if err != nil {
			continue
		}
		r.Name = g.Name
		r.Tags = g.Tags

		props := map[string]any{
			"resource_group": g.ResourceGroup,
		}
		if p := g.Properties; p != nil {
			r.Status = mapProvisioningState(p.ProvisioningState)
			if p.OSType != "" {
				props["os_type"] = p.OSType
			}
			if p.RestartPolicy != "" {
				props["restart_policy"] = p.RestartPolicy
			}
			if p.Sku != "" {
				props["sku"] = p.Sku
			}
			if ip := p.IPAddress; ip != nil {
				addr := map[string]any{}
				if ip.IP != "" {
					addr["ip"] = ip.IP
				}
				if ip.Type != "" {
					addr["type"] = ip.Type
				}
				if ip.FQDN != "" {
					addr["fqdn"] = ip.FQDN
				}
				if ip.DNSLabel != "" {
					addr["dns_label"] = ip.DNSLabel
				}
				if len(addr) > 0 {
					props["ip_address"] = addr
				}
			}
			props["container_count"] = len(p.Containers)
		} else {
			r.Status = "active"
		}
		r.Properties = props

		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformServiceBusNamespaces converts Microsoft.ServiceBus/namespaces into
// OSIRIS JSON resources of type osiris.azure.servicebus.namespace. Queue topic and subscription enumeration is out of scope.
func TransformServiceBusNamespaces(namespaces []ServiceBusNamespace, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	return transformMessagingNamespaces(
		"osiris.azure.servicebus.namespace",
		"Microsoft.ServiceBus/namespaces",
		messagingIterServiceBus(namespaces),
		sub,
	)
}

// TransformEventHubsNamespaces converts Microsoft.EventHub/namespaces into
// OSIRIS JSON resources of type osiris.azure.eventhubs.namespace.
func TransformEventHubsNamespaces(namespaces []EventHubsNamespace, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	return transformMessagingNamespaces(
		"osiris.azure.eventhubs.namespace",
		"Microsoft.EventHub/namespaces",
		messagingIterEventHubs(namespaces),
		sub,
	)
}

// messagingNamespaceView unifies ServiceBus + EventHubs namespace iteration for the shared transform body.
type messagingNamespaceView struct {
	ID            string
	Name          string
	Location      string
	ResourceGroup string
	Tags          map[string]string
	SKU           *azMessagingSKU
	Properties    *azMessagingProperties
}

func messagingIterServiceBus(namespaces []ServiceBusNamespace) func(yield func(messagingNamespaceView)) {
	return func(yield func(messagingNamespaceView)) {
		for _, n := range namespaces {
			yield(messagingNamespaceView{
				ID:            n.ID,
				Name:          n.Name,
				Location:      n.Location,
				ResourceGroup: n.ResourceGroup,
				Tags:          n.Tags,
				SKU:           n.SKU,
				Properties:    n.Properties,
			})
		}
	}
}

func messagingIterEventHubs(namespaces []EventHubsNamespace) func(yield func(messagingNamespaceView)) {
	return func(yield func(messagingNamespaceView)) {
		for _, n := range namespaces {
			yield(messagingNamespaceView{
				ID:            n.ID,
				Name:          n.Name,
				Location:      n.Location,
				ResourceGroup: n.ResourceGroup,
				Tags:          n.Tags,
				SKU:           n.SKU,
				Properties:    n.Properties,
			})
		}
	}
}

func transformMessagingNamespaces(osirisType, nativeType string, iter func(yield func(messagingNamespaceView)), sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string)

	iter(func(n messagingNamespaceView) {
		id := resourceID(osirisType, n.ID)
		idMap[n.ID] = id

		prov := azureProvider(n.ID, nativeType, n.Location, sub.SubscriptionID, sub.TenantID)

		r, err := sdk.NewResource(id, osirisType, prov)
		if err != nil {
			return
		}
		r.Name = n.Name
		r.Tags = n.Tags

		props := map[string]any{
			"resource_group": n.ResourceGroup,
		}
		if n.SKU != nil {
			if n.SKU.Name != "" {
				props["sku_name"] = n.SKU.Name
			}
			if n.SKU.Tier != "" {
				props["sku_tier"] = n.SKU.Tier
			}
			if n.SKU.Capacity > 0 {
				props["capacity"] = n.SKU.Capacity
			}
		}
		if p := n.Properties; p != nil {
			r.Status = mapProvisioningState(p.ProvisioningState)
			if p.ServiceBusEndpoint != "" {
				props["endpoint"] = p.ServiceBusEndpoint
			}
			if p.ZoneRedundant {
				props["zone_redundant"] = true
			}
			if p.DisableLocalAuth {
				props["disable_local_auth"] = true
			}
			if p.PublicNetworkAccess != "" {
				props["public_network_access"] = p.PublicNetworkAccess
			}
			if p.MinimumTLSVersion != "" {
				props["minimum_tls_version"] = p.MinimumTLSVersion
			}
			if peIDs := collectPEIDs(p.PrivateEndpointConnections); len(peIDs) > 0 {
				r.Extensions = map[string]any{extensionNamespace: map[string]any{"private_endpoint_connection_ids": peIDs}}
			}
		} else {
			r.Status = "active"
		}
		r.Properties = props

		resources = append(resources, r)
	})
	return resources, idMap
}

// TransformAPIMServices converts Microsoft.ApiManagement/service into OSIRIS JSON resources of type osiris.azure.apim.
// Individual APIs, operations, products, and policy documents are out of scope.
func TransformAPIMServices(services []APIMService, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(services))

	for _, s := range services {
		id := resourceID("osiris.azure.apim", s.ID)
		idMap[s.ID] = id

		prov := azureProvider(s.ID, "Microsoft.ApiManagement/service", s.Location, sub.SubscriptionID, sub.TenantID)

		r, err := sdk.NewResource(id, "osiris.azure.apim", prov)
		if err != nil {
			continue
		}
		r.Name = s.Name
		r.Tags = s.Tags

		props := map[string]any{
			"resource_group": s.ResourceGroup,
		}
		if s.SKU != nil {
			if s.SKU.Name != "" {
				props["sku_name"] = s.SKU.Name
			}
			if s.SKU.Capacity > 0 {
				props["capacity"] = s.SKU.Capacity
			}
		}
		if p := s.Properties; p != nil {
			r.Status = mapProvisioningState(p.ProvisioningState)
			if p.GatewayURL != "" {
				props["gateway_url"] = p.GatewayURL
			}
			if p.PortalURL != "" {
				props["portal_url"] = p.PortalURL
			}
			if p.ManagementURL != "" {
				props["management_url"] = p.ManagementURL
			}
			if p.VirtualNetworkType != "" {
				props["virtual_network_type"] = p.VirtualNetworkType
			}
			if p.VirtualNetworkConfiguration != nil && p.VirtualNetworkConfiguration.SubnetResourceID != "" {
				props["subnet_id"] = p.VirtualNetworkConfiguration.SubnetResourceID
			}
			if len(p.PublicIPAddresses) > 0 {
				props["public_ip_addresses"] = p.PublicIPAddresses
			}
			if len(p.PrivateIPAddresses) > 0 {
				props["private_ip_addresses"] = p.PrivateIPAddresses
			}
			if p.PublicNetworkAccess != "" {
				props["public_network_access"] = p.PublicNetworkAccess
			}
			if p.DisableGateway {
				props["disable_gateway"] = true
			}
			if peIDs := collectPEIDs(p.PrivateEndpointConnections); len(peIDs) > 0 {
				r.Extensions = map[string]any{extensionNamespace: map[string]any{"private_endpoint_connection_ids": peIDs}}
			}
		} else {
			r.Status = "active"
		}
		r.Properties = props

		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformFrontDoorProfiles converts Microsoft.Cdn/profiles entries with an Azure Front Door SKU (Standard / Premium)
// into OSIRIS JSON resources of type osiris.azure.frontdoor.profile. Routes, rules, and WAF policies are out of scope.
func TransformFrontDoorProfiles(profiles []FrontDoorProfile, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(profiles))

	for _, fp := range profiles {
		id := resourceID("osiris.azure.frontdoor.profile", fp.ID)
		idMap[fp.ID] = id

		prov := azureProvider(fp.ID, "Microsoft.Cdn/profiles", fp.Location, sub.SubscriptionID, sub.TenantID)

		r, err := sdk.NewResource(id, "osiris.azure.frontdoor.profile", prov)
		if err != nil {
			continue
		}
		r.Name = fp.Name
		r.Tags = fp.Tags

		props := map[string]any{
			"resource_group": fp.ResourceGroup,
		}
		if fp.Kind != "" {
			props["kind"] = fp.Kind
		}
		if fp.SKU != nil && fp.SKU.Name != "" {
			props["sku_name"] = fp.SKU.Name
		}
		if p := fp.Properties; p != nil {
			r.Status = mapProvisioningState(p.ProvisioningState)
			if p.ResourceState != "" {
				props["resource_state"] = p.ResourceState
			}
			if p.FrontDoorID != "" {
				props["front_door_id"] = p.FrontDoorID
			}
		} else {
			r.Status = "active"
		}
		r.Properties = props

		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformAKSClusterContainsAgentPoolConnections emits `contains` edges from each AKS cluster to its agent pools.
func TransformAKSClusterContainsAgentPoolConnections(clusters []AKSCluster, clusterIDMap, poolIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, c := range clusters {
		srcID, ok := clusterIDMap[c.ID]
		if !ok {
			continue
		}
		for _, p := range c.AgentPools {
			dstID, ok := poolIDMap[p.ID]
			if !ok {
				continue
			}
			canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
				Type:      "contains",
				Direction: "forward",
				Source:    srcID,
				Target:    dstID,
			})
			connID := sdk.BuildConnectionID(canonicalKey, 16)
			conn, err := sdk.NewConnection(connID, "contains", srcID, dstID)
			if err != nil {
				continue
			}
			conn.Name = fmt.Sprintf("%s contains %s", c.Name, p.Name)
			_ = conn.SetDirection("forward")
			connections = append(connections, conn)
		}
	}
	return connections
}

// TransformAKSNodePoolToSubnetConnections emits `network` edges from each AKS
// agent pool to its delegated VNet subnet. Pools without a subnet (kubenet + managed VNet) are silently skipped.
func TransformAKSNodePoolToSubnetConnections(clusters []AKSCluster, poolIDMap, subnetIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, c := range clusters {
		for _, p := range c.AgentPools {
			if p.VNetSubnetID == "" {
				continue
			}
			srcID, ok := poolIDMap[p.ID]
			if !ok {
				continue
			}
			dstID, ok := subnetIDMap[p.VNetSubnetID]
			if !ok {
				continue
			}
			canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
				Type:      "network",
				Direction: "forward",
				Source:    srcID,
				Target:    dstID,
			})
			connID := sdk.BuildConnectionID(canonicalKey, 16)
			conn, err := sdk.NewConnection(connID, "network", srcID, dstID)
			if err != nil {
				continue
			}
			conn.Name = fmt.Sprintf("%s -> %s", p.Name, extractLastSegment(p.VNetSubnetID))
			_ = conn.SetDirection("forward")
			connections = append(connections, conn)
		}
	}
	return connections
}

// TransformPEToAKSClusterConnections wires Private Endpoints to AKS clusters
// via the cluster's properties.privateEndpointConnections (private cluster only).
func TransformPEToAKSClusterConnections(clusters []AKSCluster, aksIDMap, peIDMap map[string]string) []sdk.Connection {
	bindings := make([]peBinding, 0, len(clusters))
	for _, c := range clusters {
		if c.Properties == nil {
			continue
		}
		bindings = append(bindings, peBinding{
			TargetArmID: c.ID,
			Name:        c.Name,
			Conns:       c.Properties.PrivateEndpointConnections,
		})
	}
	return transformPEBoundConnections(bindings, aksIDMap, peIDMap, "dependency")
}

// TransformContainerEnvContainsAppConnections emits `contains` edges from each
// managed environment to the container apps that live inside it.
func TransformContainerEnvContainsAppConnections(apps []ContainerApp, envIDMap, appIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, a := range apps {
		envArmID := a.EnvironmentID()
		if envArmID == "" {
			continue
		}
		srcID, ok := envIDMap[envArmID]
		if !ok {
			continue
		}
		dstID, ok := appIDMap[a.ID]
		if !ok {
			continue
		}
		canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
			Type:      "contains",
			Direction: "forward",
			Source:    srcID,
			Target:    dstID,
		})
		connID := sdk.BuildConnectionID(canonicalKey, 16)
		conn, err := sdk.NewConnection(connID, "contains", srcID, dstID)
		if err != nil {
			continue
		}
		conn.Name = fmt.Sprintf("%s contains %s", extractLastSegment(envArmID), a.Name)
		_ = conn.SetDirection("forward")
		connections = append(connections, conn)
	}
	return connections
}

// TransformContainerEnvToSubnetConnections emits `network` edges from each
// managed environment to its infrastructure subnet. Non-VNet-integrated environments are silently skipped.
func TransformContainerEnvToSubnetConnections(envs []ContainerAppEnvironment, envIDMap, subnetIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, e := range envs {
		if e.Properties == nil || e.Properties.VNetConfiguration == nil {
			continue
		}
		subnetArmID := e.Properties.VNetConfiguration.InfrastructureSubnetID
		if subnetArmID == "" {
			continue
		}
		srcID, ok := envIDMap[e.ID]
		if !ok {
			continue
		}
		dstID, ok := subnetIDMap[subnetArmID]
		if !ok {
			continue
		}
		canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
			Type:      "network",
			Direction: "forward",
			Source:    srcID,
			Target:    dstID,
		})
		connID := sdk.BuildConnectionID(canonicalKey, 16)
		conn, err := sdk.NewConnection(connID, "network", srcID, dstID)
		if err != nil {
			continue
		}
		conn.Name = fmt.Sprintf("%s -> %s", e.Name, extractLastSegment(subnetArmID))
		_ = conn.SetDirection("forward")
		connections = append(connections, conn)
	}
	return connections
}

// TransformContainerGroupToSubnetConnections emits `network` edges from each
// VNet-integrated ACI container group to each of its subnet references.
func TransformContainerGroupToSubnetConnections(groups []ContainerGroup, cgIDMap, subnetIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, g := range groups {
		if g.Properties == nil || len(g.Properties.SubnetIDs) == 0 {
			continue
		}
		srcID, ok := cgIDMap[g.ID]
		if !ok {
			continue
		}
		for _, ref := range g.Properties.SubnetIDs {
			if ref.ID == "" {
				continue
			}
			dstID, ok := subnetIDMap[ref.ID]
			if !ok {
				continue
			}
			canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
				Type:      "network",
				Direction: "forward",
				Source:    srcID,
				Target:    dstID,
			})
			connID := sdk.BuildConnectionID(canonicalKey, 16)
			conn, err := sdk.NewConnection(connID, "network", srcID, dstID)
			if err != nil {
				continue
			}
			conn.Name = fmt.Sprintf("%s -> %s", g.Name, extractLastSegment(ref.ID))
			_ = conn.SetDirection("forward")
			connections = append(connections, conn)
		}
	}
	return connections
}

// TransformPEToServiceBusConnections wires Private Endpoints to Service Bus namespaces (Premium tier only).
func TransformPEToServiceBusConnections(namespaces []ServiceBusNamespace, sbIDMap, peIDMap map[string]string) []sdk.Connection {
	bindings := make([]peBinding, 0, len(namespaces))
	for _, n := range namespaces {
		if n.Properties == nil {
			continue
		}
		bindings = append(bindings, peBinding{
			TargetArmID: n.ID,
			Name:        n.Name,
			Conns:       n.Properties.PrivateEndpointConnections,
		})
	}
	return transformPEBoundConnections(bindings, sbIDMap, peIDMap, "dependency")
}

// TransformPEToEventHubsConnections wires Private Endpoints to Event Hubs namespaces (Standard/Premium tiers).
func TransformPEToEventHubsConnections(namespaces []EventHubsNamespace, ehIDMap, peIDMap map[string]string) []sdk.Connection {
	bindings := make([]peBinding, 0, len(namespaces))
	for _, n := range namespaces {
		if n.Properties == nil {
			continue
		}
		bindings = append(bindings, peBinding{
			TargetArmID: n.ID,
			Name:        n.Name,
			Conns:       n.Properties.PrivateEndpointConnections,
		})
	}
	return transformPEBoundConnections(bindings, ehIDMap, peIDMap, "dependency")
}

// TransformAPIMToSubnetConnections emits `network` edges from each VNet-integrated APIM service (External / Internal mode) to its subnet.
func TransformAPIMToSubnetConnections(services []APIMService, apimIDMap, subnetIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, s := range services {
		if s.Properties == nil || s.Properties.VirtualNetworkConfiguration == nil {
			continue
		}
		subnetArmID := s.Properties.VirtualNetworkConfiguration.SubnetResourceID
		if subnetArmID == "" {
			continue
		}
		srcID, ok := apimIDMap[s.ID]
		if !ok {
			continue
		}
		dstID, ok := subnetIDMap[subnetArmID]
		if !ok {
			continue
		}
		canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
			Type:      "network",
			Direction: "forward",
			Source:    srcID,
			Target:    dstID,
		})
		connID := sdk.BuildConnectionID(canonicalKey, 16)
		conn, err := sdk.NewConnection(connID, "network", srcID, dstID)
		if err != nil {
			continue
		}
		conn.Name = fmt.Sprintf("%s -> %s", s.Name, extractLastSegment(subnetArmID))
		_ = conn.SetDirection("forward")
		connections = append(connections, conn)
	}
	return connections
}

// TransformPEToAPIMConnections wires Private Endpoints to APIM services.
func TransformPEToAPIMConnections(services []APIMService, apimIDMap, peIDMap map[string]string) []sdk.Connection {
	bindings := make([]peBinding, 0, len(services))
	for _, s := range services {
		if s.Properties == nil {
			continue
		}
		bindings = append(bindings, peBinding{
			TargetArmID: s.ID,
			Name:        s.Name,
			Conns:       s.Properties.PrivateEndpointConnections,
		})
	}
	return transformPEBoundConnections(bindings, apimIDMap, peIDMap, "dependency")
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
