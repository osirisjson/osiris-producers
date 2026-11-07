// transform_networking.go - Azure Networking category transforms.
// Covers resources and connections for the CSV "Networking" category:
// VNets, Subnets, NICs, NSGs, Route Tables, Public IPs, Load Balancers,
// Private Endpoints, VNet/NAT Gateways, Firewalls, Application Gateways
// DNS Zones, Private DNS Zones, ExpressRoute Circuits,
// and Application Security Groups.
//
// Resource type mapping:
//
//   Standard types (spec-defined):
//   Microsoft.Network/virtualNetworks          -> network.vpc
//   Microsoft.Network/virtualNetworks/subnets  -> network.subnet
//   Microsoft.Network/networkInterfaces        -> network.interface
//   Microsoft.Network/networkSecurityGroups    -> network.security.group
//   Microsoft.Network/loadBalancers            -> network.loadbalancer
//   Microsoft.Network/azureFirewalls           -> network.firewall
//   Microsoft.Network/privateEndpoints         -> network.endpoint
//   Microsoft.Network/virtualNetworkGateways   -> network.gateway
//   Microsoft.Network/natGateways              -> network.gateway
//   Microsoft.Network/connections              -> network.vpn
//
//   Custom types (osiris.azure.* namespace):
//   Microsoft.Network/routeTables              -> osiris.azure.routetable
//   Microsoft.Network/publicIPAddresses        -> osiris.azure.publicip
//   Microsoft.Network/privateDnsZones          -> osiris.azure.dns.privatezone
//   Microsoft.Network/dnsZones                 -> osiris.azure.dns.zone
//   Microsoft.Network/expressRouteCircuits     -> osiris.azure.expressroute
//   Microsoft.Network/applicationGateways      -> osiris.azure.applicationgateway
//   Microsoft.Network/applicationSecurityGroups -> osiris.azure.asg
//
// For an introduction to OSIRIS JSON Producer for Microsoft Azure see:
// [OSIRIS-JSON-AZURE]: https://docs.osirisjson.org/osiris-producers/hyperscalers/microsoft-azure/
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package azure

import (
	"fmt"
	"strings"

	"go.osirisjson.org/producers/pkg/sdk"
)

// TransformVNets converts Azure VirtualNetworks into OSIRIS JSON
// resources.
// Peerings are passed in to embed peering summary in VNet properties.
func TransformVNets(vnets []VirtualNetwork, peerings []VNetPeering, sub SubscriptionInfo) []sdk.Resource {
	// Build peering lookup: VNet ARM ID -> peerings for that VNet.
	peeringsByVNet := make(map[string][]VNetPeering)
	for _, p := range peerings {
		peeringsByVNet[armKey(p.VNetID())] = append(peeringsByVNet[armKey(p.VNetID())], p)
	}
	var resources []sdk.Resource
	for _, v := range vnets {
		id := resourceID("network.vpc", v.ID)
		prov := azureProvider(v.ID, "Microsoft.Network/virtualNetworks", v.Location, sub)

		r, err := sdk.NewResource(id, "network.vpc", prov)
		if err != nil {
			continue
		}
		r.Name = v.Name
		r.Status = mapProvisioningState(v.ProvisioningState)
		r.State = v.ProvisioningState
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
		if vnetPeerings := peeringsByVNet[armKey(v.ID)]; len(vnetPeerings) > 0 {
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
				if p.AllowVirtualNetworkAccess {
					entry["allow_virtual_network_access"] = true
				}
				peerList = append(peerList, entry)
			}
			props["peerings"] = peerList
		}
		r.Properties = props
		attachArmBody(&r, &v)
		resources = append(resources, r)
	}
	return resources
}

// TransformSubnets converts Azure Subnets into OSIRIS JSON resources.
// VNets are passed in to inherit the parent VNet's region for each
// subnet. Returns resources and a map of subnet ARM ID -> resource ID
// for wiring connections.
func TransformSubnets(subnets []Subnet, vnets []VirtualNetwork, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	vnetLocation := make(map[string]string, len(vnets))
	for _, v := range vnets {
		vnetLocation[strings.ToLower(v.ID)] = v.Location
	}

	var resources []sdk.Resource
	idMap := make(map[string]string, len(subnets))

	for _, s := range subnets {
		id := resourceID("network.subnet", s.ID)
		idMap[armKey(s.ID)] = id

		loc := vnetLocation[strings.ToLower(s.VNetID())]
		prov := azureProvider(s.ID, "Microsoft.Network/virtualNetworks/subnets", loc, sub)

		r, err := sdk.NewResource(id, "network.subnet", prov)
		if err != nil {
			continue
		}
		r.Name = s.Name
		r.Status = mapProvisioningState(s.ProvisioningState)
		r.State = s.ProvisioningState

		props := map[string]any{
			"resource_group": s.ResourceGroup,
		}
		switch {
		case len(s.AddressPrefixes) > 0:
			props["address_prefixes"] = s.AddressPrefixes
		case s.AddressPrefix != "":
			props["address_prefixes"] = []string{s.AddressPrefix}
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
		attachArmBody(&r, &s)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformNICs converts Azure NetworkInterfaces into OSIRIS JSON
// resources.
// Returns resources and a map of NIC ARM ID -> resource ID.
func TransformNICs(nics []NetworkInterface, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(nics))

	for _, n := range nics {
		id := resourceID("network.interface", n.ID)
		idMap[armKey(n.ID)] = id

		prov := azureProvider(n.ID, "Microsoft.Network/networkInterfaces", n.Location, sub)

		r, err := sdk.NewResource(id, "network.interface", prov)
		if err != nil {
			continue
		}
		r.Name = n.Name
		r.Status = mapProvisioningState(n.ProvisioningState)
		r.State = n.ProvisioningState
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
		// Hoist primary private IP to top-level for easy consumer access.
		for _, ipc := range n.IPConfigurations {
			if ipc.PrivateIPAddress != "" {
				props["private_ip"] = ipc.PrivateIPAddress
				break
			}
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
		attachArmBody(&r, &n)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformNSGs converts Azure NetworkSecurityGroups into OSIRIS JSON
// resources.
// Returns resources and a map of NSG ARM ID -> resource ID.
func TransformNSGs(nsgs []NetworkSecurityGroup, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(nsgs))

	for _, n := range nsgs {
		id := resourceID("network.security.group", n.ID)
		idMap[armKey(n.ID)] = id

		prov := azureProvider(n.ID, "Microsoft.Network/networkSecurityGroups", n.Location, sub)

		r, err := sdk.NewResource(id, "network.security.group", prov)
		if err != nil {
			continue
		}
		r.Name = n.Name
		r.Status = mapProvisioningState(n.ProvisioningState)
		r.State = n.ProvisioningState
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
		if len(n.SecurityRules) > 0 {
			props["security_rules"] = transformNSGRules(n.SecurityRules)
		}
		r.Properties = props

		// Extensions: default rules (platform-managed; kept separate
		// from custom rules).
		if len(n.DefaultSecurityRules) > 0 {
			r.Extensions = map[string]any{extensionNamespace: map[string]any{
				"default_security_rules": transformNSGRules(n.DefaultSecurityRules),
			}}
		}
		attachArmBody(&r, &n)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformRouteTables converts Azure RouteTables into OSIRIS JSON
// resources.
// Returns resources and a map of route table ARM ID -> resource ID.
func TransformRouteTables(tables []RouteTable, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(tables))

	for _, t := range tables {
		id := resourceID("osiris.azure.routetable", t.ID)
		idMap[armKey(t.ID)] = id

		prov := azureProvider(t.ID, "Microsoft.Network/routeTables", t.Location, sub)

		r, err := sdk.NewResource(id, "osiris.azure.routetable", prov)
		if err != nil {
			continue
		}
		r.Name = t.Name
		r.Status = mapProvisioningState(t.ProvisioningState)
		r.State = t.ProvisioningState
		r.Tags = t.Tags

		props := map[string]any{
			"resource_group": t.ResourceGroup,
			"route_count":    len(t.Routes),
		}
		if t.DisableBgpRoutePropagation {
			props["disable_bgp_route_propagation"] = true
		}
		if t.DisablePeeringRoute != "" && t.DisablePeeringRoute != "None" {
			props["disable_peering_route"] = t.DisablePeeringRoute
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
				if rt.HasBgpOverride {
					entry["has_bgp_override"] = true
				}
				routes = append(routes, entry)
			}
			props["routes"] = routes
		}
		r.Properties = props
		attachArmBody(&r, &t)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformPublicIPs converts Azure PublicIPAddresses into OSIRIS JSON
// resources.
func TransformPublicIPs(ips []PublicIPAddress, sub SubscriptionInfo) []sdk.Resource {
	var resources []sdk.Resource
	for _, p := range ips {
		id := resourceID("osiris.azure.publicip", p.ID)
		prov := azureProvider(p.ID, "Microsoft.Network/publicIPAddresses", p.Location, sub)
		if len(p.Zones) > 0 {
			prov.Zone = strings.Join(p.Zones, ",")
		}

		r, err := sdk.NewResource(id, "osiris.azure.publicip", prov)
		if err != nil {
			continue
		}
		r.Name = p.Name
		r.Status = mapProvisioningState(p.ProvisioningState)
		r.State = p.ProvisioningState
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
		attachArmBody(&r, &p)
		resources = append(resources, r)
	}
	return resources
}

// TransformLoadBalancers converts Azure LoadBalancers into OSIRIS JSON
// resources.
func TransformLoadBalancers(lbs []LoadBalancer, sub SubscriptionInfo) []sdk.Resource {
	var resources []sdk.Resource
	for _, lb := range lbs {
		id := resourceID("network.loadbalancer", lb.ID)
		prov := azureProvider(lb.ID, "Microsoft.Network/loadBalancers", lb.Location, sub)
		if len(lb.Zones) > 0 {
			prov.Zone = strings.Join(lb.Zones, ",")
		}

		r, err := sdk.NewResource(id, "network.loadbalancer", prov)
		if err != nil {
			continue
		}
		r.Name = lb.Name
		r.Status = mapProvisioningState(lb.ProvisioningState)
		r.State = lb.ProvisioningState
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
		if len(lb.FrontendIPConfigurations) > 0 {
			frontends := make([]map[string]any, 0, len(lb.FrontendIPConfigurations))
			for _, fe := range lb.FrontendIPConfigurations {
				entry := map[string]any{"name": fe.Name}
				if fe.PrivateIPAddress != "" {
					entry["private_ip"] = fe.PrivateIPAddress
				}
				if fe.PrivateIPAllocationMethod != "" {
					entry["allocation_method"] = fe.PrivateIPAllocationMethod
				}
				if fe.Subnet != nil && fe.Subnet.ID != "" {
					entry["subnet_id"] = fe.Subnet.ID
				}
				if fe.PublicIPAddressID() != "" {
					entry["public_ip_id"] = fe.PublicIPAddressID()
				}
				frontends = append(frontends, entry)
			}
			props["frontends"] = frontends
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
		if len(lb.Probes) > 0 {
			probes := make([]map[string]any, 0, len(lb.Probes))
			for _, p := range lb.Probes {
				entry := map[string]any{
					"name":     p.Name,
					"protocol": p.Protocol,
					"port":     p.Port,
				}
				if p.IntervalInSeconds > 0 {
					entry["interval_seconds"] = p.IntervalInSeconds
				}
				if p.NumberOfProbes > 0 {
					entry["threshold"] = p.NumberOfProbes
				}
				probes = append(probes, entry)
			}
			props["probes"] = probes
		}
		if n := len(lb.InboundNatRules); n > 0 {
			props["inbound_nat_rule_count"] = n
		}
		if n := len(lb.OutboundRules); n > 0 {
			props["outbound_rule_count"] = n
		}
		r.Properties = props
		attachArmBody(&r, &lb)
		resources = append(resources, r)
	}
	return resources
}

// TransformPrivateEndpoints converts Azure PrivateEndpoints into
// OSIRIS JSON resources of the standard type network.endpoint
// (Appendix C: "Network endpoint (e.g. private endpoint)", naming
// Azure Private Endpoint directly).
func TransformPrivateEndpoints(pes []PrivateEndpoint, sub SubscriptionInfo) []sdk.Resource {
	var resources []sdk.Resource
	for _, pe := range pes {
		id := resourceID("network.endpoint", pe.ID)
		prov := azureProvider(pe.ID, "Microsoft.Network/privateEndpoints", pe.Location, sub)

		r, err := sdk.NewResource(id, "network.endpoint", prov)
		if err != nil {
			continue
		}
		r.Name = pe.Name
		r.Status = mapProvisioningState(pe.ProvisioningState)
		r.State = pe.ProvisioningState
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
		attachArmBody(&r, &pe)
		resources = append(resources, r)
	}
	return resources
}

// TransformVNetGateways converts Azure VNetGateways into OSIRIS JSON
// resources of the standard type network.gateway (Appendix C: "Network
// gateway", generic the specific kind (VPN vs ExpressRoute) remains
// available via properties.gateway_type/vpn_type below).
// Gateway connections are passed in to embed connection summary in gateway properties.
func TransformVNetGateways(gws []VNetGateway, gwConns []GatewayConnection, sub SubscriptionInfo) []sdk.Resource {
	// Build connection lookup: gateway ARM ID -> connections.
	connsByGW := make(map[string][]GatewayConnection)
	for _, gc := range gwConns {
		if gwID := gc.VirtualNetworkGateway1ID(); gwID != "" {
			connsByGW[armKey(gwID)] = append(connsByGW[armKey(gwID)], gc)
		}
	}

	var resources []sdk.Resource
	for _, gw := range gws {
		id := resourceID("network.gateway", gw.ID)
		prov := azureProvider(gw.ID, "Microsoft.Network/virtualNetworkGateways", gw.Location, sub)

		r, err := sdk.NewResource(id, "network.gateway", prov)
		if err != nil {
			continue
		}
		r.Name = gw.Name
		r.Status = mapProvisioningState(gw.ProvisioningState)
		r.State = gw.ProvisioningState
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
		// Transit settings are configuration either way false is stated.
		props["allow_remote_vnet_traffic"] = gw.AllowRemoteVnetTraffic
		props["allow_virtual_wan_traffic"] = gw.AllowVirtualWanTraffic
		if conns := connsByGW[armKey(gw.ID)]; len(conns) > 0 {
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
		attachArmBody(&r, &gw)
		resources = append(resources, r)
	}
	return resources
}

// TransformNATGateways converts Azure NATGateways into OSIRIS JSON
// resources of the standard type network.gateway (Appendix C names
// "Azure NAT Gateway" directly).
func TransformNATGateways(gws []NATGateway, sub SubscriptionInfo) []sdk.Resource {
	var resources []sdk.Resource
	for _, gw := range gws {
		id := resourceID("network.gateway", gw.ID)
		prov := azureProvider(gw.ID, "Microsoft.Network/natGateways", gw.Location, sub)

		r, err := sdk.NewResource(id, "network.gateway", prov)
		if err != nil {
			continue
		}
		r.Name = gw.Name
		r.Status = mapProvisioningState(gw.ProvisioningState)
		r.State = gw.ProvisioningState
		r.Tags = gw.Tags
		r.Properties = map[string]any{
			"resource_group":     gw.ResourceGroup,
			"public_ip_count":    len(gw.PublicIPAddresses),
			"associated_subnets": len(gw.Subnets),
		}
		attachArmBody(&r, &gw)
		resources = append(resources, r)
	}
	return resources
}

// TransformFirewalls converts Azure Firewalls into OSIRIS JSON resources.
func TransformFirewalls(fws []AzureFirewall, sub SubscriptionInfo) []sdk.Resource {
	var resources []sdk.Resource
	for _, fw := range fws {
		id := resourceID("network.firewall", fw.ID)
		prov := azureProvider(fw.ID, "Microsoft.Network/azureFirewalls", fw.Location, sub)

		r, err := sdk.NewResource(id, "network.firewall", prov)
		if err != nil {
			continue
		}
		r.Name = fw.Name
		r.Status = mapProvisioningState(fw.ProvisioningState)
		r.State = fw.ProvisioningState
		r.Tags = fw.Tags
		r.Properties = map[string]any{
			"resource_group": fw.ResourceGroup,
		}
		attachArmBody(&r, &fw)
		resources = append(resources, r)
	}
	return resources
}

// TransformAppGateways converts Azure ApplicationGateways into
// OSIRIS JSON resources. Application Gateway is an L7 reverse proxy/WAF
// not a load balancer; it gets its own type.
func TransformAppGateways(gws []ApplicationGateway, sub SubscriptionInfo) []sdk.Resource {
	var resources []sdk.Resource
	for _, gw := range gws {
		id := resourceID("osiris.azure.applicationgateway", gw.ID)
		prov := azureProvider(gw.ID, "Microsoft.Network/applicationGateways", gw.Location, sub)
		if len(gw.Zones) > 0 {
			prov.Zone = strings.Join(gw.Zones, ",")
		}

		r, err := sdk.NewResource(id, "osiris.azure.applicationgateway", prov)
		if err != nil {
			continue
		}
		r.Name = gw.Name
		r.Status = mapProvisioningState(gw.ProvisioningState)
		r.State = gw.ProvisioningState
		r.Tags = gw.Tags

		props := map[string]any{
			"resource_group":     gw.ResourceGroup,
			"sku_name":           gw.SKU.Name,
			"sku_tier":           gw.SKU.Tier,
			"capacity":           gw.SKU.Capacity,
			"operational_state":  strings.ToLower(gw.OperationalState),
			"listener_count":     len(gw.HTTPListeners),
			"backend_pool_count": len(gw.BackendAddressPools),
		}
		if gw.EnableHttp2 != nil {
			props["enable_http2"] = *gw.EnableHttp2
		}
		if gw.WebApplicationFirewallConfiguration != nil {
			props["waf_enabled"] = gw.WebApplicationFirewallConfiguration.Enabled
			props["waf_mode"] = strings.ToLower(gw.WebApplicationFirewallConfiguration.FirewallMode)
		}
		r.Properties = props
		attachArmBody(&r, &gw)
		resources = append(resources, r)
	}
	return resources
}

// TransformDNSZones converts Azure DNS zones into OSIRIS JSON resources.
func TransformDNSZones(zones []DNSZone, sub SubscriptionInfo) []sdk.Resource {
	var resources []sdk.Resource
	for _, z := range zones {
		id := resourceID("osiris.azure.dns.zone", z.ID)
		prov := azureProvider(z.ID, "Microsoft.Network/dnsZones", "global", sub)

		r, err := sdk.NewResource(id, "osiris.azure.dns.zone", prov)
		if err != nil {
			continue
		}
		r.Name = z.Name
		r.Status = mapProvisioningState(z.ProvisioningState)
		r.State = z.ProvisioningState
		r.Properties = map[string]any{
			"resource_group": z.ResourceGroup,
		}
		attachArmBody(&r, &z)
		resources = append(resources, r)
	}
	return resources
}

// TransformPrivateDNSZones converts Azure private DNS zones into
// OSIRIS JSON resources.
func TransformPrivateDNSZones(zones []PrivateDNSZone, sub SubscriptionInfo) []sdk.Resource {
	var resources []sdk.Resource
	for _, z := range zones {
		id := resourceID("osiris.azure.dns.privatezone", z.ID)
		prov := azureProvider(z.ID, "Microsoft.Network/privateDnsZones", "global", sub)

		r, err := sdk.NewResource(id, "osiris.azure.dns.privatezone", prov)
		if err != nil {
			continue
		}
		r.Name = z.Name
		r.Status = mapProvisioningState(z.ProvisioningState)
		r.State = z.ProvisioningState
		r.Tags = z.Tags

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
		attachArmBody(&r, &z)
		resources = append(resources, r)
	}
	return resources
}

// TransformExpressRouteCircuits converts Azure ExpressRoute circuits
// into OSIRIS JSON resources.
func TransformExpressRouteCircuits(circuits []ExpressRouteCircuit, sub SubscriptionInfo) []sdk.Resource {
	var resources []sdk.Resource
	for _, c := range circuits {
		id := resourceID("osiris.azure.expressroute", c.ID)
		prov := azureProvider(c.ID, "Microsoft.Network/expressRouteCircuits", c.Location, sub)

		r, err := sdk.NewResource(id, "osiris.azure.expressroute", prov)
		if err != nil {
			continue
		}
		r.Name = c.Name
		r.Status = mapProvisioningState(c.ProvisioningState)
		r.State = c.ProvisioningState
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
		if c.SKU.Family != "" {
			ext["sku_family"] = c.SKU.Family
		}
		if c.AllowGlobalReach {
			ext["allow_global_reach"] = true
		}
		if c.GlobalReachEnabled {
			ext["global_reach_enabled"] = true
		}
		if c.AllowClassicOperations {
			ext["allow_classic_operations"] = true
		}
		if c.EnableDirectPortRateLimit {
			ext["enable_direct_port_rate_limit"] = true
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
				if p.AzureASN != 0 {
					pm["azure_asn"] = p.AzureASN
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
				if p.PrimaryAzurePort != "" {
					pm["primary_azure_port"] = p.PrimaryAzurePort
				}
				if p.SecondaryAzurePort != "" {
					pm["secondary_azure_port"] = p.SecondaryAzurePort
				}
				if p.LastModifiedBy != "" {
					pm["last_modified_by"] = p.LastModifiedBy
				}
				peerings = append(peerings, pm)
			}
			ext["peerings"] = peerings
		}
		if len(ext) > 0 {
			r.Extensions = map[string]any{extensionNamespace: ext}
		}
		attachArmBody(&r, &c)
		resources = append(resources, r)
	}
	return resources
}

// TransformApplicationSecurityGroups converts Azure ApplicationSecurityGroups
// into OSIRIS JSON resources. Returns resources and a map of
// ASG ARM ID -> resource ID for NIC -> ASG connection wiring.
func TransformApplicationSecurityGroups(asgs []ApplicationSecurityGroup, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(asgs))

	for _, a := range asgs {
		id := resourceID("osiris.azure.asg", a.ID)
		idMap[armKey(a.ID)] = id

		prov := azureProvider(a.ID, "Microsoft.Network/applicationSecurityGroups", a.Location, sub)

		r, err := sdk.NewResource(id, "osiris.azure.asg", prov)
		if err != nil {
			continue
		}
		r.Name = a.Name
		r.Status = mapProvisioningState(a.ProvisioningState)
		r.State = a.ProvisioningState
		r.Tags = a.Tags
		r.Properties = map[string]any{
			"resource_group": a.ResourceGroup,
		}
		attachArmBody(&r, &a)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformVNetPeerings converts Azure VNet peerings into OSIRIS JSON
// connections. Requires a map of VNet ARM ID -> OSIRIS JSON resource
// ID to wire source/target. Returns connections and stub resources
// for remote VNets in other subscriptions.
func TransformVNetPeerings(peerings []VNetPeering, vnetIDMap map[string]string) ([]sdk.Connection, []sdk.Resource) {
	var connections []sdk.Connection
	var stubs []sdk.Resource
	seen := map[string]bool{}
	stubSeen := map[string]bool{}
	for _, p := range peerings {
		sourceID, ok := armLookup(vnetIDMap, p.VNetID())
		if !ok {
			continue
		}

		// The remote VNet may be in a different subscription
		// (not in our ID map). Create a stub resource for it so peering
		// connections can reference it.
		targetID, ok := armLookup(vnetIDMap, p.RemoteVNetID())
		if !ok {
			targetID = resourceID("network.vpc", p.RemoteVNetID())
			if !stubSeen[targetID] {
				stubSeen[targetID] = true
				remoteSubID := extractSubscriptionID(p.RemoteVNetID())
				prov := sdk.Provider{
					Name:     providerName,
					NativeID: p.RemoteVNetID(),
					Type:     "Microsoft.Network/virtualNetworks",
					Account:  canonicalSubscriptionID(remoteSubID),
				}
				stub, err := sdk.NewResource(targetID, "network.vpc", prov)
				if err == nil {
					stub.Name = vnetNameFromARM(p.RemoteVNetID())
					stub.Status = "unknown"
					stub.Properties = map[string]any{
						"cross_subscription": true,
					}
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
		conn.State = p.PeeringState
		conn.Properties = map[string]any{
			"peering_state":                p.PeeringState,
			"allow_gateway_transit":        p.AllowGatewayTransit,
			"allow_forwarded_traffic":      p.AllowForwardedTraffic,
			"use_remote_gateways":          p.UseRemoteGateways,
			"allow_virtual_network_access": p.AllowVirtualNetworkAccess,
		}

		connections = append(connections, conn)
	}
	return connections, stubs
}

// TransformSubnetNSGConnections links each subnet to the network
// security group applied to it (Subnet -> NSG, type "osiris.azure.nsg.association").
// An NSG attachment is a policy association rather than network
// reachability, so the namespaced association type of OSIRIS JSON
// spec chapter 5 section 5.2.4 is used instead of "network".
//
// Evidence: subnet networkSecurityGroup.id.
func TransformSubnetNSGConnections(subnets []Subnet, subnetIDMap, nsgIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, s := range subnets {
		sourceID, ok := armLookup(subnetIDMap, s.ID)
		if !ok {
			continue
		}
		targetID, ok := armLookup(nsgIDMap, s.NSGId())
		if !ok {
			continue
		}
		if conn, ok := newEdge(connNSGAssociation, sourceID, targetID, fmt.Sprintf("%s -> %s", s.Name, extractLastSegment(s.NSGId()))); ok {
			connections = append(connections, conn)
		}
	}
	return connections
}

// TransformNICToNSGConnections links each network interface to the
// network security group applied directly to it (NIC -> NSG, type
// "osiris.azure.nsg.association"), the NIC-level counterpart of
// [TransformSubnetNSGConnections].
//
// Evidence: NIC networkSecurityGroup.id.
func TransformNICToNSGConnections(nics []NetworkInterface, nicIDMap, nsgIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, n := range nics {
		sourceID, ok := armLookup(nicIDMap, n.ID)
		if !ok {
			continue
		}
		targetID, ok := armLookup(nsgIDMap, n.NSGId())
		if !ok {
			continue
		}
		if conn, ok := newEdge(connNSGAssociation, sourceID, targetID, fmt.Sprintf("%s -> %s", n.Name, extractLastSegment(n.NSGId()))); ok {
			connections = append(connections, conn)
		}
	}
	return connections
}

// TransformNICToPublicIPConnections links each network interface to the
// public IP addresses bound to its IP configurations (NIC -> Public IP,
// type "network", forward). properties.ip_configuration names the IP
// configuration carrying the binding.
//
// Evidence: NIC ipConfigurations[].publicIpAddress.id.
func TransformNICToPublicIPConnections(nics []NetworkInterface, nicIDMap, publicIPIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	seen := map[string]bool{}
	for _, n := range nics {
		sourceID, ok := armLookup(nicIDMap, n.ID)
		if !ok {
			continue
		}
		for _, ip := range n.IPConfigurations {
			targetID, ok := armLookup(publicIPIDMap, ip.PublicIPAddressID())
			if !ok {
				continue
			}
			conn, ok := newEdge(connNetwork, sourceID, targetID, fmt.Sprintf("%s -> %s", n.Name, extractLastSegment(ip.PublicIPAddressID())))
			if !ok || seen[conn.ID] {
				continue
			}
			seen[conn.ID] = true
			if ip.Name != "" {
				conn.Properties = map[string]any{"ip_configuration": ip.Name}
			}
			connections = append(connections, conn)
		}
	}
	return connections
}

// TransformSubnetRouteTableConnections associates each subnet with the
// route table applied to it (osiris.azure.routetable.association).
func TransformSubnetRouteTableConnections(subnets []Subnet, subnetIDMap, rtIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, s := range subnets {
		if s.RouteTableId() == "" {
			continue
		}
		sourceID, ok := armLookup(subnetIDMap, s.ID)
		if !ok {
			continue
		}
		targetID, ok := armLookup(rtIDMap, s.RouteTableId())
		if !ok {
			continue
		}

		canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
			Type:      connRouteTableAssociation,
			Direction: "forward",
			Source:    sourceID,
			Target:    targetID,
		})
		connID := sdk.BuildConnectionID(canonicalKey, 16)

		conn, err := sdk.NewConnection(connID, connRouteTableAssociation, sourceID, targetID)
		if err != nil {
			continue
		}
		conn.Name = fmt.Sprintf("%s -> route table", s.Name)
		_ = conn.SetDirection("forward")
		connections = append(connections, conn)
	}
	return connections
}

// TransformGatewayConnections converts Azure gateway connections into
// OSIRIS JSON connections.
// A gateway connection's peer (ExpressRoute circuit, remote gateway)
// often lives in a different subscription typically a central
// connectivity/hub subscription. For those out-of-scope peers we emit
// a stub resource (mirroring the VNet peering pattern) so the topology
// edge is preserved without violating the document-builder invariant
// that every connection endpoint references an existing resource.
func TransformGatewayConnections(gwConns []GatewayConnection, allResourceIDs map[string]string) ([]sdk.Connection, []sdk.Resource) {
	var connections []sdk.Connection
	var stubs []sdk.Resource
	stubSeen := map[string]bool{}
	for _, gc := range gwConns {
		gw1ARM := gc.VirtualNetworkGateway1ID()
		sourceID, ok := armLookup(allResourceIDs, gw1ARM)
		if !ok {
			continue
		}

		peerARM := gc.PeerID()
		if peerARM == "" {
			continue
		}
		targetID, ok := armLookup(allResourceIDs, peerARM)
		if !ok {
			osirisType := gatewayPeerOsirisType(peerARM)
			if osirisType == "" {
				continue
			}
			targetID = resourceID(osirisType, peerARM)
			if !stubSeen[targetID] {
				stubSeen[targetID] = true
				prov := sdk.Provider{
					Name:     providerName,
					NativeID: peerARM,
					Type:     gatewayPeerARMType(peerARM),
					Account:  canonicalSubscriptionID(extractSubscriptionID(peerARM)),
					Source:   "azure-cli",
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
		conn.Status = mapProvisioningState(gc.ProvisioningState)
		conn.State = gc.ProvisioningState

		// connection_id names the Microsoft.Network/connections
		// resource that configures this link (emitted in the
		// same document).
		props := map[string]any{
			"connection_id":  resourceID("", gc.ID),
			"routing_weight": gc.RoutingWeight,
		}
		if gc.ConnectionType != "" {
			props["connection_type"] = gc.ConnectionType
		}
		if gc.EnableBgp {
			props["enable_bgp"] = true
		}
		conn.Properties = props
		connections = append(connections, conn)

		// The connection resource itself is linked to both ends it
		// configures, so it is reachable by graph traversal.
		if strings.EqualFold(gc.ConnectionType, "ExpressRoute") {
			connResID := resourceID("", gc.ID)
			// source_arm_id is the ARM ID exactly as the connection
			// states it; evidence_field names the ARM field it was
			// read from.
			for _, end := range []struct{ id, role, field, arm string }{
				{sourceID, "gateway", "virtualNetworkGateway1.id", gw1ARM},
				{targetID, "circuit", "peer.id", peerARM},
			} {
				if e, ok := newEdge(connERConnectionEndpoint, connResID, end.id,
					fmt.Sprintf("%s -> %s", gc.Name, extractLastSegment(end.id))); ok {
					e.Properties = map[string]any{
						"endpoint_role":  end.role,
						"evidence_field": end.field,
						"source_arm_id":  end.arm,
					}
					connections = append(connections, e)
				}
			}
		}
	}
	return connections, stubs
}

// TransformVNetToSubnetConnections links each virtual network to its
// subnets as forward "contains" connections (VNet -> Subnet, source
// contains target per OSIRIS JSON spec chapter 5 section 5.2.3).
// This is the fundamental structural relationship in Azure networking
// and is not duplicated as group membership.
//
// Evidence: the subnet ARM ID is a child path of its VNet ARM ID
// (Subnet.VNetID).
func TransformVNetToSubnetConnections(subnets []Subnet, subnetIDMap, vnetIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, s := range subnets {
		subnetID, ok := armLookup(subnetIDMap, s.ID)
		if !ok {
			continue
		}
		vnetID, ok := armLookup(vnetIDMap, s.VNetID())
		if !ok {
			continue
		}
		if conn, ok := newEdge(connContains, vnetID, subnetID, fmt.Sprintf("%s -> %s", extractLastSegment(s.VNetID()), s.Name)); ok {
			connections = append(connections, conn)
		}
	}
	return connections
}

// TransformNICToSubnetConnections creates connections between NICs and
// their subnets. Each NIC ipConfiguration references a subnet this is
// how VMs attach to the network.
func TransformNICToSubnetConnections(nics []NetworkInterface, nicIDMap, subnetIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	seen := map[string]bool{} // deduplicate NIC->subnet pairs
	for _, n := range nics {
		sourceID, ok := armLookup(nicIDMap, n.ID)
		if !ok {
			continue
		}
		for _, ip := range n.IPConfigurations {
			if ip.SubnetID() == "" {
				continue
			}
			targetID, ok := armLookup(subnetIDMap, ip.SubnetID())
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

// TransformPrivateEndpointToSubnetConnections creates connections
// between private endpoints and subnets.
func TransformPrivateEndpointToSubnetConnections(pes []PrivateEndpoint, subnetIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, pe := range pes {
		if pe.SubnetID() == "" {
			continue
		}
		sourceID := resourceID("network.endpoint", pe.ID)
		targetID, ok := armLookup(subnetIDMap, pe.SubnetID())
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

// TransformPrivateEndpointToNICConnections creates connections between
// private endpoints and their NICs.
func TransformPrivateEndpointToNICConnections(pes []PrivateEndpoint, nicIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, pe := range pes {
		sourceID := resourceID("network.endpoint", pe.ID)
		for _, nicArmID := range pe.NetworkInterfaceIDs() {
			targetID, ok := armLookup(nicIDMap, nicArmID)
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

// TransformLBFrontendToPublicIPConnections creates connections between
// load balancer frontends and public IPs.
func TransformLBFrontendToPublicIPConnections(lbs []LoadBalancer, publicIPIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, lb := range lbs {
		sourceID := resourceID("network.loadbalancer", lb.ID)
		for _, fe := range lb.FrontendIPConfigurations {
			if fe.PublicIPAddressID() == "" {
				continue
			}
			targetID, ok := armLookup(publicIPIDMap, fe.PublicIPAddressID())
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

// TransformVNetGatewayToSubnetConnections creates connections between
// VNet gateways and their GatewaySubnet.
func TransformVNetGatewayToSubnetConnections(gws []VNetGateway, subnetIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, gw := range gws {
		sourceID := resourceID("network.gateway", gw.ID)
		for _, ip := range gw.IPConfigurations {
			if ip.SubnetID() == "" {
				continue
			}
			targetID, ok := armLookup(subnetIDMap, ip.SubnetID())
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

// TransformVNetGatewayToPublicIPConnections creates connections between
// VNet gateways and their public IPs.
func TransformVNetGatewayToPublicIPConnections(gws []VNetGateway, publicIPIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, gw := range gws {
		sourceID := resourceID("network.gateway", gw.ID)
		for _, ip := range gw.IPConfigurations {
			if ip.PublicIPAddressID() == "" {
				continue
			}
			targetID, ok := armLookup(publicIPIDMap, ip.PublicIPAddressID())
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

// TransformNATGatewayToPublicIPConnections creates connections between
// NAT gateways and their public IPs.
func TransformNATGatewayToPublicIPConnections(gws []NATGateway, publicIPIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, gw := range gws {
		sourceID := resourceID("network.gateway", gw.ID)
		for _, pipArmID := range gw.PublicIPAddressIDs() {
			targetID, ok := armLookup(publicIPIDMap, pipArmID)
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

// TransformPrivateDNSToVNetConnections creates connections between
// private DNS zones and linked VNets.
// VNets in other subscriptions generate a cross-subscription stub
// resource (same pattern as peering stubs)
// so the connection can reference a valid resource ID.
// Returns connections and any cross-subscription VNet stubs created.
func TransformPrivateDNSToVNetConnections(zones []PrivateDNSZone, vnetIDMap map[string]string) ([]sdk.Connection, []sdk.Resource) {
	var connections []sdk.Connection
	var stubs []sdk.Resource
	stubSeen := map[string]bool{}
	seenConns := map[string]bool{}

	for _, z := range zones {
		sourceID := resourceID("osiris.azure.dns.privatezone", z.ID)
		for _, link := range z.Links {
			vnetArmID := link.VirtualNetworkID()
			if vnetArmID == "" {
				continue
			}
			targetID, ok := armLookup(vnetIDMap, vnetArmID)
			if !ok {
				targetID = resourceID("network.vpc", vnetArmID)
				if !stubSeen[targetID] {
					stubSeen[targetID] = true
					remoteSubID := extractSubscriptionID(vnetArmID)
					prov := sdk.Provider{
						Name:     providerName,
						NativeID: vnetArmID,
						Type:     "Microsoft.Network/virtualNetworks",
						Account:  canonicalSubscriptionID(remoteSubID),
					}
					stub, err := sdk.NewResource(targetID, "network.vpc", prov)
					if err == nil {
						stub.Name = vnetNameFromARM(vnetArmID)
						stub.Status = "unknown"
						stub.Properties = map[string]any{
							"cross_subscription": true,
						}
						stubs = append(stubs, stub)
					}
				}
			}

			canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
				Type:      "network",
				Direction: "forward",
				Source:    sourceID,
				Target:    targetID,
			})
			connID := sdk.BuildConnectionID(canonicalKey, 16)
			if seenConns[connID] {
				continue
			}
			seenConns[connID] = true

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
	return connections, stubs
}

// TransformFlowLogConnections wires enabled NSG flow logs to their
// storage account targets.
// Each active flow log is represented as a forward dependency
// edge: NSG -> storage account.
func TransformFlowLogConnections(flowLogs []FlowLog, nsgIDMap, storageIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, fl := range flowLogs {
		sourceID, ok := armLookup(nsgIDMap, fl.TargetResourceID)
		if !ok {
			continue
		}
		targetID, ok := armLookup(storageIDMap, fl.StorageID)
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
		conn.Name = fl.Name
		conn.Properties = map[string]any{"log_type": "network_flow"}
		_ = conn.SetDirection("forward")
		connections = append(connections, conn)
	}
	return connections
}

// TransformNICToASGConnections creates network connections from a
// NIC to each Application Security Group referenced by any of its
// IP configurations.
func TransformNICToASGConnections(nics []NetworkInterface, nicIDMap, asgIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	seen := map[string]bool{}
	for _, n := range nics {
		sourceID, ok := armLookup(nicIDMap, n.ID)
		if !ok {
			continue
		}
		for _, ip := range n.IPConfigurations {
			for _, asgArmID := range ip.ASGIDs() {
				targetID, ok := armLookup(asgIDMap, asgArmID)
				if !ok {
					continue
				}
				pairKey := sourceID + "|" + targetID
				if seen[pairKey] {
					continue
				}
				seen[pairKey] = true

				canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
					Type:      connASGAssociation,
					Direction: "forward",
					Source:    sourceID,
					Target:    targetID,
				})
				connID := sdk.BuildConnectionID(canonicalKey, 16)

				conn, err := sdk.NewConnection(connID, connASGAssociation, sourceID, targetID)
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

// BuildVNetIDMap builds a map of VNet ARM ID -> OSIRIS JSON resource
// ID from VNet resources.
func BuildVNetIDMap(vnets []VirtualNetwork) map[string]string {
	m := make(map[string]string, len(vnets))
	for _, v := range vnets {
		m[armKey(v.ID)] = resourceID("network.vpc", v.ID)
	}
	return m
}

// BuildPublicIPIDMap builds a map of public IP ARM ID -> OSIRIS JSON
// resource ID.
func BuildPublicIPIDMap(pips []PublicIPAddress) map[string]string {
	m := make(map[string]string, len(pips))
	for _, p := range pips {
		m[armKey(p.ID)] = resourceID("osiris.azure.publicip", p.ID)
	}
	return m
}

// BuildPrivateEndpointIDMap builds a map of private endpoint
// ARM ID -> OSIRIS JSON resource ID.
func BuildPrivateEndpointIDMap(pes []PrivateEndpoint) map[string]string {
	m := make(map[string]string, len(pes))
	for _, pe := range pes {
		m[armKey(pe.ID)] = resourceID("network.endpoint", pe.ID)
	}
	return m
}

// BuildLBIDMap builds a map of Load Balancer ARM ID -> OSIRIS JSON
// resource ID. Used for metric alert scope resolution (LBs are a
// common monitored resource).
func BuildLBIDMap(lbs []LoadBalancer) map[string]string {
	m := make(map[string]string, len(lbs))
	for _, lb := range lbs {
		m[armKey(lb.ID)] = resourceID("network.loadbalancer", lb.ID)
	}
	return m
}

// TransformLBToSubnetConnections creates network/forward connections
// from each load balancer to the subnets referenced by its frontend
// IP configurations.
// Only internal (private) frontends carry a subnet reference; public
// frontends are skipped. ARM ID casing is normalised to lowercase
// before lookup.
func TransformLBToSubnetConnections(lbs []LoadBalancer, lbIDMap, subnetIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, lb := range lbs {
		sourceID, ok := armLookup(lbIDMap, lb.ID)
		if !ok {
			continue
		}
		seen := make(map[string]bool)
		for _, fe := range lb.FrontendIPConfigurations {
			if fe.Subnet == nil || fe.Subnet.ID == "" {
				continue
			}
			subnetKey := strings.ToLower(fe.Subnet.ID)
			if seen[subnetKey] {
				continue
			}
			seen[subnetKey] = true
			targetID, ok := armLookup(subnetIDMap, subnetKey)
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
			conn.Name = fmt.Sprintf("%s -> %s", lb.Name, extractLastSegment(fe.Subnet.ID))
			_ = conn.SetDirection("forward")
			connections = append(connections, conn)
		}
	}
	return connections
}

// transformNSGRules converts NSG security rules into OSIRIS JSON
// compatible maps.
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
		if rule.Description != "" {
			entry["description"] = rule.Description
		}
		// Single-value fields take precedence when set; fall back to
		// array variants.
		if rule.SourcePortRange != "" {
			entry["source_port_range"] = rule.SourcePortRange
		} else if len(rule.SourcePortRanges) > 0 {
			entry["source_port_ranges"] = rule.SourcePortRanges
		}
		if rule.DestinationPortRange != "" {
			entry["destination_port_range"] = rule.DestinationPortRange
		} else if len(rule.DestinationPortRanges) > 0 {
			entry["destination_port_ranges"] = rule.DestinationPortRanges
		}
		if rule.SourceAddressPrefix != "" {
			entry["source_address_prefix"] = rule.SourceAddressPrefix
		} else if len(rule.SourceAddressPrefixes) > 0 {
			entry["source_address_prefixes"] = rule.SourceAddressPrefixes
		}
		if rule.DestinationAddressPrefix != "" {
			entry["destination_address_prefix"] = rule.DestinationAddressPrefix
		} else if len(rule.DestinationAddressPrefixes) > 0 {
			entry["destination_address_prefixes"] = rule.DestinationAddressPrefixes
		}
		out = append(out, entry)
	}
	return out
}

// extractSubscriptionID extracts the subscription UUID from an ARM
// resource ID. ARM IDs follow the pattern: /subscriptions/<uuid>/...
// canonicalSubscriptionID returns a subscription ID in the lower-case
// form used for provider.account, metadata.scope.accounts and the
// subscription catalog. Subscription IDs are GUIDs, compared
// case-insensitively by ARM.
func canonicalSubscriptionID(id string) string {
	return strings.ToLower(strings.TrimSpace(id))
}

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

// gatewayConnectionResourceType returns the OSIRIS JSON resource type
// of a Microsoft.Network/connections resource from its connectionType:
// an ExpressRoute connection is not a VPN.
func gatewayConnectionResourceType(azConnType string) string {
	if strings.EqualFold(azConnType, "ExpressRoute") {
		return "osiris.azure.expressroute.connection"
	}
	return "network.vpn"
}

// gatewayConnectionSubtype maps the Azure ConnectionType field to an
// OSIRIS JSON connection type (spec chapter 5 section 5.2.3).
// An ExpressRoute connection is plain `network` connectivity (data
// path and route exchange); site-to-site, VNet-to-VNet and P2S are
// IPsec/IKEv2 VPNs.
func gatewayConnectionSubtype(azConnType string) string {
	switch strings.ToLower(azConnType) {
	case "expressroute":
		// The connection carries both the private-peering data path and
		// the gateway <-> Microsoft edge BGP session: plain
		// connectivity.
		return "network"
	case "ipsec", "vnet2vnet", "vpnclient":
		return "network.vpn"
	default:
		return "network"
	}
}

// gatewayPeerARMType classify an ARM ID by its /providers/<ns>/<type>/
// segment. Returns the ARM provider/type string (e.g. Microsoft.Network/expressRouteCircuits)
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

// gatewayPeerOsirisType maps the peer's ARM type to its
// OSIRIS JSON type.
// virtualnetworkgateways and localnetworkgateways both resolve to the
// standard type network.gateway (Appendix C), matching
// TransformVNetGateways/TransformNATGateways above;
// expressroutecircuits has still no standard-type equivalent
// and stays a custom type.
func gatewayPeerOsirisType(armID string) string {
	switch strings.ToLower(gatewayPeerARMType(armID)) {
	case "microsoft.network/expressroutecircuits":
		return "osiris.azure.expressroute"
	case "microsoft.network/virtualnetworkgateways", "microsoft.network/localnetworkgateways":
		return "network.gateway"
	default:
		return ""
	}
}

// TransformPublicIPPrefixes converts Microsoft.Network/publicIPPrefixes into OSIRIS JSON resources.
func TransformPublicIPPrefixes(prefixes []PublicIPPrefix, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(prefixes))

	for _, p := range prefixes {
		id := resourceID("osiris.azure.publicipprefix", p.ID)
		idMap[armKey(p.ID)] = id

		prov := azureProvider(p.ID, "Microsoft.Network/publicIPPrefixes", p.Location, sub)
		if len(p.Zones) > 0 {
			prov.Zone = strings.Join(p.Zones, ",")
		}
		r, err := sdk.NewResource(id, "osiris.azure.publicipprefix", prov)
		if err != nil {
			continue
		}
		r.Name = p.Name
		r.Status = mapProvisioningState(p.ProvisioningState)
		r.State = p.ProvisioningState
		r.Tags = p.Tags

		props := map[string]any{
			"resource_group": p.ResourceGroup,
		}
		if p.PrefixLength > 0 {
			props["prefix_length"] = p.PrefixLength
		}
		if p.IPPrefix != "" {
			props["ip_prefix"] = p.IPPrefix
		}
		if p.SKU.Name != "" {
			props["sku"] = p.SKU.Name
		}
		if len(p.Zones) > 0 {
			props["zones"] = p.Zones
		}
		if len(p.PublicIPAddresses) > 0 {
			ids := make([]string, 0, len(p.PublicIPAddresses))
			for _, pip := range p.PublicIPAddresses {
				ids = append(ids, pip.ID)
			}
			props["public_ip_address_ids"] = ids
		}
		r.Properties = props
		attachArmBody(&r, &p)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformAvailabilitySets converts Microsoft.Compute/availabilitySets
// into OSIRIS JSON resources.
func TransformAvailabilitySets(sets []AvailabilitySet, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(sets))

	for _, s := range sets {
		id := resourceID("osiris.azure.availabilityset", s.ID)
		idMap[armKey(s.ID)] = id

		prov := azureProvider(s.ID, "Microsoft.Compute/availabilitySets", s.Location, sub)
		r, err := sdk.NewResource(id, "osiris.azure.availabilityset", prov)
		if err != nil {
			continue
		}
		r.Name = s.Name
		asPS := s.ProvisioningState
		r.Status = mapProvisioningState(asPS)
		r.State = asPS
		r.Tags = s.Tags

		props := map[string]any{
			"resource_group": s.ResourceGroup,
		}
		if s.SKU.Name != "" {
			props["sku"] = s.SKU.Name
		}
		if s.PlatformFaultDomainCount > 0 {
			props["platform_fault_domain_count"] = s.PlatformFaultDomainCount
		}
		if s.PlatformUpdateDomainCount > 0 {
			props["platform_update_domain_count"] = s.PlatformUpdateDomainCount
		}
		if len(s.VirtualMachines) > 0 {
			props["vm_count"] = len(s.VirtualMachines)
		}
		r.Properties = props
		attachArmBody(&r, &s)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformAvailabilitySetToVMConnections wires each Availability Set
// to its member VMs.
func TransformAvailabilitySetToVMConnections(sets []AvailabilitySet, asIDMap, vmIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, s := range sets {
		sourceID, ok := armLookup(asIDMap, s.ID)
		if !ok {
			continue
		}
		for _, vmRef := range s.VirtualMachines {
			targetID, ok := armLookup(vmIDMap, vmRef.ID)
			if !ok {
				continue
			}
			canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
				Type: "contains", Direction: "forward", Source: sourceID, Target: targetID,
			})
			conn, err := sdk.NewConnection(sdk.BuildConnectionID(canonicalKey, 16), "contains", sourceID, targetID)
			if err == nil {
				conn.Name = fmt.Sprintf("%s -> %s", s.Name, extractLastSegment(vmRef.ID))
				_ = conn.SetDirection("forward")
				connections = append(connections, conn)
			}
		}
	}
	return connections
}

// TransformRouteServers converts Microsoft.Network/virtualHubs
// (Route Server) into OSIRIS JSON resources.
func TransformRouteServers(rs []RouteServer, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(rs))

	for _, r := range rs {
		id := resourceID("osiris.azure.routeserver", r.ID)
		idMap[armKey(r.ID)] = id

		prov := azureProvider(r.ID, "Microsoft.Network/virtualHubs", r.Location, sub)
		res, err := sdk.NewResource(id, "osiris.azure.routeserver", prov)
		if err != nil {
			continue
		}
		res.Name = r.Name
		res.Status = mapProvisioningState(r.ProvisioningState)
		res.State = r.ProvisioningState
		res.Tags = r.Tags

		props := map[string]any{
			"resource_group": r.ResourceGroup,
		}
		if r.VirtualRouterAsn > 0 {
			props["virtual_router_asn"] = r.VirtualRouterAsn
		}
		if len(r.VirtualRouterIps) > 0 {
			props["virtual_router_ips"] = r.VirtualRouterIps
		}
		if r.HubRoutingPreference != "" {
			props["hub_routing_preference"] = r.HubRoutingPreference
		}
		// Always stated: false means the Route Server exchanges no
		// routes with the VNet's ExpressRoute or VPN gateway.
		props["allow_branch_to_branch_traffic"] = r.AllowBranchToBranchTraffic
		if len(r.BGPConnections) > 0 {
			props["bgp_peer_count"] = len(r.BGPConnections)
		}
		res.Properties = props

		if len(r.BGPConnections) > 0 {
			peers := make([]map[string]any, 0, len(r.BGPConnections))
			for _, p := range r.BGPConnections {
				peer := map[string]any{"name": p.Name, "peer_ip": p.PeerIp}
				if p.PeerAsn > 0 {
					peer["peer_asn"] = p.PeerAsn
				}
				peers = append(peers, peer)
			}
			res.Extensions = map[string]any{
				extensionNamespace: map[string]any{"bgp_connections": peers},
			}
		}
		attachArmBody(&res, &r)
		resources = append(resources, res)
	}
	return resources, idMap
}

// BuildRouteServerIDMap builds ARM ID -> OSIRIS JSON resource ID map
// for Route Servers.
func BuildRouteServerIDMap(rs []RouteServer) map[string]string {
	m := make(map[string]string, len(rs))
	for _, r := range rs {
		m[armKey(r.ID)] = resourceID("osiris.azure.routeserver", r.ID)
	}
	return m
}

// TransformRouteServerConnections wires each Route Server to its
// RouteServerSubnet and public IP.
func TransformRouteServerConnections(rs []RouteServer, rsIDMap, subnetIDMap, pipIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, r := range rs {
		sourceID, ok := armLookup(rsIDMap, r.ID)
		if !ok {
			continue
		}
		for _, ipc := range r.IPConfigurations {
			if ipc.Subnet != nil && ipc.Subnet.ID != "" {
				if targetID, ok := armLookup(subnetIDMap, ipc.Subnet.ID); ok {
					canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
						Type: "network", Direction: "forward", Source: sourceID, Target: targetID,
					})
					conn, err := sdk.NewConnection(sdk.BuildConnectionID(canonicalKey, 16), "network", sourceID, targetID)
					if err == nil {
						conn.Name = fmt.Sprintf("%s -> subnet", r.Name)
						_ = conn.SetDirection("forward")
						connections = append(connections, conn)
					}
				}
			}
			if ipc.PublicIPAddress != nil && ipc.PublicIPAddress.ID != "" {
				if targetID, ok := armLookup(pipIDMap, ipc.PublicIPAddress.ID); ok {
					canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
						Type: "network", Direction: "forward", Source: sourceID, Target: targetID,
					})
					conn, err := sdk.NewConnection(sdk.BuildConnectionID(canonicalKey, 16), "network", sourceID, targetID)
					if err == nil {
						conn.Name = fmt.Sprintf("%s -> public IP", r.Name)
						_ = conn.SetDirection("forward")
						connections = append(connections, conn)
					}
				}
			}
		}
	}
	return connections
}

// TransformGatewayConnectionResources emits Microsoft.Network/connections
// as first-class OSIRIS JSON resources of the standard type network.vpn
// (Appendix C: "VPN connection", naming Azure VPN directly).
// The gateway-to-peer topology edges are handled separately by
// TransformGatewayConnections; this function adds the connection object
// itself so consumers can inventory VPN/ER connections independently of
// the routing graph.
func TransformGatewayConnectionResources(gwConns []GatewayConnection, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(gwConns))

	for _, gc := range gwConns {
		resType := gatewayConnectionResourceType(gc.ConnectionType)
		id := resourceID(resType, gc.ID)
		idMap[armKey(gc.ID)] = id

		prov := azureProvider(gc.ID, "Microsoft.Network/connections", gc.Location, sub)
		r, err := sdk.NewResource(id, resType, prov)
		if err != nil {
			continue
		}
		r.Name = gc.Name
		r.Status = mapProvisioningState(gc.ProvisioningState)
		r.State = gc.ProvisioningState

		props := map[string]any{
			"resource_group":  gc.ResourceGroup,
			"connection_type": gc.ConnectionType,
		}
		if gc.EnableBgp {
			props["enable_bgp"] = true
		}
		if gc.RoutingWeight > 0 {
			props["routing_weight"] = gc.RoutingWeight
		}
		gw1ID := gc.VirtualNetworkGateway1ID()
		if gw1ID != "" {
			props["gateway_id"] = gw1ID
		}
		peerID := gc.PeerID()
		if peerID != "" {
			props["peer_id"] = peerID
		}
		r.Properties = props
		attachArmBody(&r, &gc)
		resources = append(resources, r)
	}
	return resources, idMap
}

// BuildGatewayConnectionIDMap builds ARM ID -> OSIRIS JSON resource ID
// map for gateway connections.
func BuildGatewayConnectionIDMap(gwConns []GatewayConnection) map[string]string {
	m := make(map[string]string, len(gwConns))
	for _, gc := range gwConns {
		m[armKey(gc.ID)] = resourceID("network.vpn", gc.ID)
	}
	return m
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
	case "":
		return ""
	default:
		return "unknown"
	}
}
