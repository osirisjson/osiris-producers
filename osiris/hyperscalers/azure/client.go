// client.go - Azure CLI wrapper for the Microsoft Azure OSIRIS JSON producer.
// Executes 'az' CLI commands to collect networking resources from a subscription.
// Requires the user to be authenticated via 'az login' before running the producer.
//
// The client fetches all resource types that appear in real Azure production
// environments: VNets, subnets, NICs, NSGs, route tables, public IPs, load
// balancers, private endpoints, VNet peerings, gateways, DNS zones, NAT gateways,
// ExpressRoute circuits, firewalls, application gateways and virtual machines.
//
// For an introduction to OSIRIS JSON Producer for Microsoft Azure see:
// [OSIRIS-JSON-AZURE]: https://osirisjson.org/en/docs/producers/hyperscalers/microsoft-azure
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/docs/spec/v10/00-preface

package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// SubscriptionInfo carries the resolved subscription metadata.
// Field names match the az account show JSON output:
//
//	id          -> subscription UUID
//	name        -> human-readable subscription name
//	tenantId    -> Azure AD tenant UUID
//	state       -> Enabled / Disabled
type SubscriptionInfo struct {
	SubscriptionID string            `json:"id"`
	DisplayName    string            `json:"name"`
	State          string            `json:"state"`
	TenantID       string            `json:"tenantId"`
	Tags           map[string]string `json:"tags"`
}

// ResourceGroup represents an Azure resource group.
type ResourceGroup struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Location  string            `json:"location"`
	Tags      map[string]string `json:"tags"`
	ManagedBy string            `json:"managedBy"`
	// provisioningState is nested under properties in az group list output.
	Properties struct {
		ProvisioningState string `json:"provisioningState"`
	} `json:"properties"`
}

// MGEntity represents a management group or subscription entity as returned by
// `az account management-group entities list`. The list is tenant-scoped and
// includes both management groups (Type = "Microsoft.Management/managementGroups")
// and subscriptions (Type = "/subscriptions").
type MGEntity struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Type        string `json:"type"`
	TenantID    string `json:"tenantId"`
	Parent      struct {
		ID string `json:"id"`
	} `json:"parent"`
	ParentNameChain        []string `json:"parentNameChain"`
	ParentDisplayNameChain []string `json:"parentDisplayNameChain"`
	Permissions            string   `json:"permissions"`
	NumberOfChildGroups    int      `json:"numberOfChildGroups"`
	NumberOfChildren       int      `json:"numberOfChildren"`
	NumberOfDescendants    int      `json:"numberOfDescendants"`
}

// azAddressSpace matches the az CLI nested addressSpace object.
type azAddressSpace struct {
	AddressPrefixes []string `json:"addressPrefixes"`
}

// azDHCPOptions matches the az CLI nested dhcpOptions object.
type azDHCPOptions struct {
	DNSServers []string `json:"dnsServers"`
}

// VirtualNetwork represents an Azure VNet.
type VirtualNetwork struct {
	ID                   string            `json:"id"`
	Name                 string            `json:"name"`
	Location             string            `json:"location"`
	ResourceGroup        string            `json:"resourceGroup"`
	Tags                 map[string]string `json:"tags"`
	ProvisioningState    string            `json:"provisioningState"`
	AddressSpace         azAddressSpace    `json:"addressSpace"`
	DhcpOptions          azDHCPOptions     `json:"dhcpOptions"`
	Subnets              []azSubnetRef     `json:"subnets"`
	EnableDdosProtection bool              `json:"enableDdosProtection"`
}

// azServiceEndpoint matches the az CLI service endpoint object.
type azServiceEndpoint struct {
	Service   string   `json:"service"`
	Locations []string `json:"locations"`
}

// azNSGRef matches az CLI nested NSG reference.
type azNSGRef struct {
	ID string `json:"id"`
}

// azRouteTableRef matches az CLI nested route table reference.
type azRouteTableRef struct {
	ID string `json:"id"`
}

// azNATGatewayRef matches az CLI nested NAT gateway reference.
type azNATGatewayRef struct {
	ID string `json:"id"`
}

// azDelegation matches az CLI subnet delegation object.
type azDelegation struct {
	Name        string `json:"name"`
	ServiceName string `json:"serviceName"`
}

// Subnet represents an Azure subnet.
type Subnet struct {
	ID                   string              `json:"id"`
	Name                 string              `json:"name"`
	ResourceGroup        string              `json:"resourceGroup"`
	ProvisioningState    string              `json:"provisioningState"`
	AddressPrefixes      []string            `json:"addressPrefixes"`
	AddressPrefix        string              `json:"addressPrefix"`
	NetworkSecurityGroup *azNSGRef           `json:"networkSecurityGroup"`
	RouteTable           *azRouteTableRef    `json:"routeTable"`
	NatGateway           *azNATGatewayRef    `json:"natGateway"`
	Delegations          []azDelegation      `json:"delegations"`
	ServiceEndpoints     []azServiceEndpoint `json:"serviceEndpoints"`
}

// NSGId returns the NSG ID from the nested reference.
func (s Subnet) NSGId() string {
	if s.NetworkSecurityGroup != nil {
		return s.NetworkSecurityGroup.ID
	}
	return ""
}

// RouteTableId returns the route table ID from the nested reference.
func (s Subnet) RouteTableId() string {
	if s.RouteTable != nil {
		return s.RouteTable.ID
	}
	return ""
}

// VNetID extracts the parent VNet ID from the subnet's own ID.
func (s Subnet) VNetID() string {
	// Subnet ID format: /subscriptions/.../virtualNetworks/VNET/subnets/SUBNET
	idx := strings.Index(s.ID, "/subnets/")
	if idx < 0 {
		return ""
	}
	return s.ID[:idx]
}

// azSubnetRef matches az CLI nested subnet reference.
type azSubnetRef struct {
	ID string `json:"id"`
}

// azASGRef matches az CLI nested application security group reference.
type azASGRef struct {
	ID string `json:"id"`
}

// IPConfiguration represents a NIC IP configuration.
type IPConfiguration struct {
	Name                      string       `json:"name"`
	Subnet                    *azSubnetRef `json:"subnet"`
	PrivateIPAddress          string       `json:"privateIpAddress"`
	PrivateIPAllocationMethod string       `json:"privateIpAllocationMethod"`
	ApplicationSecurityGroups []azASGRef   `json:"applicationSecurityGroups"`
}

// SubnetID returns the subnet ID from the nested reference.
func (c IPConfiguration) SubnetID() string {
	if c.Subnet != nil {
		return c.Subnet.ID
	}
	return ""
}

// ASGIDs returns the ASG ARM IDs referenced by this IP configuration.
func (c IPConfiguration) ASGIDs() []string {
	if len(c.ApplicationSecurityGroups) == 0 {
		return nil
	}
	ids := make([]string, 0, len(c.ApplicationSecurityGroups))
	for _, a := range c.ApplicationSecurityGroups {
		if a.ID != "" {
			ids = append(ids, a.ID)
		}
	}
	return ids
}

// NetworkInterface represents an Azure NIC.
type NetworkInterface struct {
	ID                          string            `json:"id"`
	Name                        string            `json:"name"`
	Location                    string            `json:"location"`
	ResourceGroup               string            `json:"resourceGroup"`
	Tags                        map[string]string `json:"tags"`
	ProvisioningState           string            `json:"provisioningState"`
	IPConfigurations            []IPConfiguration `json:"ipConfigurations"`
	NetworkSecurityGroup        *azNSGRef         `json:"networkSecurityGroup"`
	EnableIPForwarding          bool              `json:"enableIPForwarding"`
	EnableAcceleratedNetworking bool              `json:"enableAcceleratedNetworking"`
	Primary                     bool              `json:"primary"`
	EffectiveRoutes             []EffectiveRoute  `json:"-"` // populated separately
}

// NSGId returns the NSG ID from the nested reference.
func (n NetworkInterface) NSGId() string {
	if n.NetworkSecurityGroup != nil {
		return n.NetworkSecurityGroup.ID
	}
	return ""
}

// NSGSecurityRule represents a single security rule in an NSG.
type NSGSecurityRule struct {
	Name                       string   `json:"name"`
	Priority                   int      `json:"priority"`
	Direction                  string   `json:"direction"`
	Access                     string   `json:"access"`
	Protocol                   string   `json:"protocol"`
	Description                string   `json:"description"`
	SourcePortRange            string   `json:"sourcePortRange"`
	SourcePortRanges           []string `json:"sourcePortRanges"`
	DestinationPortRange       string   `json:"destinationPortRange"`
	DestinationPortRanges      []string `json:"destinationPortRanges"`
	SourceAddressPrefix        string   `json:"sourceAddressPrefix"`
	SourceAddressPrefixes      []string `json:"sourceAddressPrefixes"`
	DestinationAddressPrefix   string   `json:"destinationAddressPrefix"`
	DestinationAddressPrefixes []string `json:"destinationAddressPrefixes"`
}

// NetworkSecurityGroup represents an Azure NSG.
type NetworkSecurityGroup struct {
	ID                   string            `json:"id"`
	Name                 string            `json:"name"`
	Location             string            `json:"location"`
	ResourceGroup        string            `json:"resourceGroup"`
	Tags                 map[string]string `json:"tags"`
	ProvisioningState    string            `json:"provisioningState"`
	SecurityRules        []NSGSecurityRule `json:"securityRules"`
	DefaultSecurityRules []NSGSecurityRule `json:"defaultSecurityRules"`
	Subnets              []azSubnetRef     `json:"subnets"`
	NetworkInterfaces    []azNICRef        `json:"networkInterfaces"`
}

// SubnetIDs returns the subnet IDs from nested references.
func (n NetworkSecurityGroup) SubnetIDs() []string {
	ids := make([]string, 0, len(n.Subnets))
	for _, s := range n.Subnets {
		if s.ID != "" {
			ids = append(ids, s.ID)
		}
	}
	return ids
}

// NetworkInterfaceIDs returns the NIC IDs from nested references.
func (n NetworkSecurityGroup) NetworkInterfaceIDs() []string {
	ids := make([]string, 0, len(n.NetworkInterfaces))
	for _, nic := range n.NetworkInterfaces {
		if nic.ID != "" {
			ids = append(ids, nic.ID)
		}
	}
	return ids
}

// Route represents a single route in a route table.
type Route struct {
	Name             string `json:"name"`
	AddressPrefix    string `json:"addressPrefix"`
	NextHopType      string `json:"nextHopType"`
	NextHopIPAddress string `json:"nextHopIpAddress"`
	HasBgpOverride   bool   `json:"hasBgpOverride"`
}

// RouteTable represents an Azure route table.
type RouteTable struct {
	ID                         string            `json:"id"`
	Name                       string            `json:"name"`
	Location                   string            `json:"location"`
	ResourceGroup              string            `json:"resourceGroup"`
	Tags                       map[string]string `json:"tags"`
	ProvisioningState          string            `json:"provisioningState"`
	DisableBgpRoutePropagation bool              `json:"disableBgpRoutePropagation"`
	DisablePeeringRoute        string            `json:"disablePeeringRoute"`
	Routes                     []Route           `json:"routes"`
	Subnets                    []azSubnetRef     `json:"subnets"`
}

// SubnetIDs returns the subnet IDs from nested references.
func (t RouteTable) SubnetIDs() []string {
	ids := make([]string, 0, len(t.Subnets))
	for _, s := range t.Subnets {
		if s.ID != "" {
			ids = append(ids, s.ID)
		}
	}
	return ids
}

// azSKU matches the az CLI nested SKU object.
type azSKU struct {
	Name   string `json:"name"`
	Tier   string `json:"tier"`
	Family string `json:"family"`
}

// PublicIPAddress represents an Azure public IP.
type PublicIPAddress struct {
	ID                       string            `json:"id"`
	Name                     string            `json:"name"`
	Location                 string            `json:"location"`
	ResourceGroup            string            `json:"resourceGroup"`
	Tags                     map[string]string `json:"tags"`
	ProvisioningState        string            `json:"provisioningState"`
	Zones                    []string          `json:"zones"`
	IPAddress                string            `json:"ipAddress"`
	PublicIPAllocationMethod string            `json:"publicIpAllocationMethod"`
	SKU                      azSKU             `json:"sku"`
}

// PublicIPPrefix represents a Microsoft.Network/publicIPPrefixes resource.
type PublicIPPrefix struct {
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	Location          string            `json:"location"`
	ResourceGroup     string            `json:"resourceGroup"`
	ProvisioningState string            `json:"provisioningState"`
	Tags              map[string]string `json:"tags"`
	SKU               azSKU             `json:"sku"`
	PrefixLength      int               `json:"prefixLength"`
	IPPrefix          string            `json:"ipPrefix"`
	Zones             []string          `json:"zones"`
	PublicIPAddresses []azPublicIPRef   `json:"publicIpAddresses"`
}

// AvailabilitySet represents a Microsoft.Compute/availabilitySets resource.
type AvailabilitySet struct {
	ID                        string            `json:"id"`
	Name                      string            `json:"name"`
	Location                  string            `json:"location"`
	ResourceGroup             string            `json:"resourceGroup"`
	ProvisioningState         string            `json:"provisioningState"`
	Tags                      map[string]string `json:"tags"`
	SKU                       azSKU             `json:"sku"`
	PlatformFaultDomainCount  int               `json:"platformFaultDomainCount"`
	PlatformUpdateDomainCount int               `json:"platformUpdateDomainCount"`
	VirtualMachines           []azGatewayRef    `json:"virtualMachines"`
}

// azPublicIPRef matches az CLI nested public IP reference in frontend configs.
type azPublicIPRef struct {
	ID string `json:"id"`
}

// FrontendIPConfig represents a load balancer frontend IP configuration.
type FrontendIPConfig struct {
	Name                      string         `json:"name"`
	PublicIPAddress           *azPublicIPRef `json:"publicIpAddress"`
	PrivateIPAddress          string         `json:"privateIpAddress"`
	PrivateIPAllocationMethod string         `json:"privateIpAllocationMethod"`
	Subnet                    *azSubnetRef   `json:"subnet"`
}

// PublicIPAddressID returns the public IP ID from the nested reference.
func (f FrontendIPConfig) PublicIPAddressID() string {
	if f.PublicIPAddress != nil {
		return f.PublicIPAddress.ID
	}
	return ""
}

// azBackendIPConfigRef matches az CLI backend IP configuration reference.
type azBackendIPConfigRef struct {
	ID string `json:"id"`
}

// BackendAddressPool represents a load balancer backend pool.
type BackendAddressPool struct {
	Name                    string                 `json:"name"`
	BackendIPConfigurations []azBackendIPConfigRef `json:"backendIpConfigurations"`
}

// LBRule represents a load balancing rule.
type LBRule struct {
	Name         string `json:"name"`
	Protocol     string `json:"protocol"`
	FrontendPort int    `json:"frontendPort"`
	BackendPort  int    `json:"backendPort"`
}

// LBProbe represents a load balancer health probe.
type LBProbe struct {
	Name              string `json:"name"`
	Protocol          string `json:"protocol"`
	Port              int    `json:"port"`
	IntervalInSeconds int    `json:"intervalInSeconds"`
	NumberOfProbes    int    `json:"numberOfProbes"`
}

// LoadBalancer represents an Azure load balancer.
type LoadBalancer struct {
	ID                       string               `json:"id"`
	Name                     string               `json:"name"`
	Location                 string               `json:"location"`
	ResourceGroup            string               `json:"resourceGroup"`
	Tags                     map[string]string    `json:"tags"`
	ProvisioningState        string               `json:"provisioningState"`
	Zones                    []string             `json:"zones"`
	SKU                      azSKU                `json:"sku"`
	FrontendIPConfigurations []FrontendIPConfig   `json:"frontendIpConfigurations"`
	BackendAddressPools      []BackendAddressPool `json:"backendAddressPools"`
	LoadBalancingRules       []LBRule             `json:"loadBalancingRules"`
	Probes                   []LBProbe            `json:"probes"`
	InboundNatRules          []struct{}           `json:"inboundNatRules"`
	OutboundRules            []struct{}           `json:"outboundRules"`
}

// azNICRef matches az CLI nested NIC reference.
type azNICRef struct {
	ID string `json:"id"`
}

// PrivateEndpoint represents an Azure private endpoint.
type PrivateEndpoint struct {
	ID                            string                           `json:"id"`
	Name                          string                           `json:"name"`
	Location                      string                           `json:"location"`
	ResourceGroup                 string                           `json:"resourceGroup"`
	Tags                          map[string]string                `json:"tags"`
	ProvisioningState             string                           `json:"provisioningState"`
	Subnet                        *azSubnetRef                     `json:"subnet"`
	NetworkInterfaces             []azNICRef                       `json:"networkInterfaces"`
	PrivateLinkServiceConnections []azPrivateLinkServiceConnection `json:"privateLinkServiceConnections"`
	CustomDNSConfigs              []azPrivateEndpointDNSConfig     `json:"customDnsConfigs"`
}

// azPrivateLinkServiceConnection is the PE -> target PaaS service binding.
// OSIRIS JSON specification chapter 2.1 requires exposing unique identifier, group_id + target service ID
// so consumers can build dependency edges and classify PE target types.
type azPrivateLinkServiceConnection struct {
	Name                 string   `json:"name"`
	PrivateLinkServiceID string   `json:"privateLinkServiceId"`
	GroupIDs             []string `json:"groupIds"`
}

// azPrivateEndpointDNSConfig mirrors PE.customDnsConfigs[] for DNS integration documentation.
type azPrivateEndpointDNSConfig struct {
	FQDN        string   `json:"fqdn"`
	IPAddresses []string `json:"ipAddresses"`
}

// TargetServiceID returns the private-link-service-id of the first binding.
// PEs almost always have exactly one binding in practice; the helper keeps
// callers terse.
func (pe PrivateEndpoint) TargetServiceID() string {
	if len(pe.PrivateLinkServiceConnections) == 0 {
		return ""
	}
	return pe.PrivateLinkServiceConnections[0].PrivateLinkServiceID
}

// TargetGroupID returns the first groupId of the first binding (e.g. "blob",
// "vault", "registry"). Empty when no binding or no group ids.
func (pe PrivateEndpoint) TargetGroupID() string {
	if len(pe.PrivateLinkServiceConnections) == 0 {
		return ""
	}
	gs := pe.PrivateLinkServiceConnections[0].GroupIDs
	if len(gs) == 0 {
		return ""
	}
	return gs[0]
}

// SubnetID returns the subnet ID from the nested reference.
func (pe PrivateEndpoint) SubnetID() string {
	if pe.Subnet != nil {
		return pe.Subnet.ID
	}
	return ""
}

// NetworkInterfaceIDs returns the NIC IDs from nested references.
func (pe PrivateEndpoint) NetworkInterfaceIDs() []string {
	ids := make([]string, 0, len(pe.NetworkInterfaces))
	for _, nic := range pe.NetworkInterfaces {
		if nic.ID != "" {
			ids = append(ids, nic.ID)
		}
	}
	return ids
}

// azVNetRef matches az CLI nested VNet reference.
type azVNetRef struct {
	ID string `json:"id"`
}

// VNetPeering represents an Azure VNet peering.
type VNetPeering struct {
	ID                        string     `json:"id"`
	Name                      string     `json:"name"`
	ResourceGroup             string     `json:"resourceGroup"`
	RemoteVirtualNetwork      *azVNetRef `json:"remoteVirtualNetwork"`
	PeeringState              string     `json:"peeringState"`
	AllowGatewayTransit       bool       `json:"allowGatewayTransit"`
	AllowForwardedTraffic     bool       `json:"allowForwardedTraffic"`
	UseRemoteGateways         bool       `json:"useRemoteGateways"`
	AllowVirtualNetworkAccess bool       `json:"allowVirtualNetworkAccess"`
}

// RemoteVNetID returns the remote VNet ARM ID from the nested reference.
func (p VNetPeering) RemoteVNetID() string {
	if p.RemoteVirtualNetwork != nil {
		return p.RemoteVirtualNetwork.ID
	}
	return ""
}

// VNetID extracts the parent VNet ARM ID from the peering's own ID.
// Peering ID format: /subscriptions/.../virtualNetworks/VNET/virtualNetworkPeerings/PEER
func (p VNetPeering) VNetID() string {
	idx := strings.Index(p.ID, "/virtualNetworkPeerings/")
	if idx < 0 {
		return ""
	}
	return p.ID[:idx]
}

// GatewayIPConfig represents a VNet gateway IP configuration.
type GatewayIPConfig struct {
	PublicIPAddress           *azPublicIPRef `json:"publicIpAddress"`
	Subnet                    *azSubnetRef   `json:"subnet"`
	PrivateIPAllocationMethod string         `json:"privateIpAllocationMethod"`
}

// PublicIPAddressID returns the public IP ID from the nested reference.
func (g GatewayIPConfig) PublicIPAddressID() string {
	if g.PublicIPAddress != nil {
		return g.PublicIPAddress.ID
	}
	return ""
}

// SubnetID returns the subnet ID from the nested reference.
func (g GatewayIPConfig) SubnetID() string {
	if g.Subnet != nil {
		return g.Subnet.ID
	}
	return ""
}

// VNetGateway represents an Azure virtual network gateway.
type VNetGateway struct {
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	Location          string            `json:"location"`
	ResourceGroup     string            `json:"resourceGroup"`
	Tags              map[string]string `json:"tags"`
	ProvisioningState string            `json:"provisioningState"`
	GatewayType       string            `json:"gatewayType"`
	VPNType           string            `json:"vpnType"`
	EnableBGP         bool              `json:"enableBgp"`
	ActiveActive      bool              `json:"activeActive"`
	SKU               azSKU             `json:"sku"`
	IPConfigurations  []GatewayIPConfig `json:"ipConfigurations"`
}

// azGatewayRef matches az CLI nested gateway reference.
type azGatewayRef struct {
	ID string `json:"id"`
}

// GatewayConnection represents a VNet gateway connection.
type GatewayConnection struct {
	ID                     string        `json:"id"`
	Name                   string        `json:"name"`
	Location               string        `json:"location"`
	ResourceGroup          string        `json:"resourceGroup"`
	ProvisioningState      string        `json:"provisioningState"`
	ConnectionType         string        `json:"connectionType"`
	EnableBgp              bool          `json:"enableBgp"`
	RoutingWeight          int           `json:"routingWeight"`
	VirtualNetworkGateway1 *azGatewayRef `json:"virtualNetworkGateway1"`
	Peer                   *azGatewayRef `json:"peer"`
	ExpressRouteCircuit    *azGatewayRef `json:"expressRouteCircuit"`
}

// VirtualNetworkGateway1ID returns the gateway ID from the nested reference.
func (gc GatewayConnection) VirtualNetworkGateway1ID() string {
	if gc.VirtualNetworkGateway1 != nil {
		return gc.VirtualNetworkGateway1.ID
	}
	return ""
}

// PeerID returns the peer ID from the nested reference.
// For ExpressRoute connections, the peer is the ER circuit; for VPN/VNet2VNet it's the peer field.
func (gc GatewayConnection) PeerID() string {
	if gc.Peer != nil {
		return gc.Peer.ID
	}
	if gc.ExpressRouteCircuit != nil {
		return gc.ExpressRouteCircuit.ID
	}
	return ""
}

// RouteServerIPConfig represents an IP configuration on an Azure Route Server.
type RouteServerIPConfig struct {
	Name             string        `json:"name"`
	PrivateIPAddress string        `json:"privateIpAddress"`
	PublicIPAddress  *azGatewayRef `json:"publicIpAddress"`
	Subnet           *azGatewayRef `json:"subnet"`
}

// RouteServerBGPPeer represents a BGP connection configured on an Azure Route Server.
type RouteServerBGPPeer struct {
	Name              string `json:"name"`
	PeerAsn           int64  `json:"peerAsn"`
	PeerIp            string `json:"peerIp"`
	ProvisioningState string `json:"provisioningState"`
}

// RouteServer represents a Microsoft.Network/virtualHubs resource deployed as a
// standalone Azure Route Server (not part of a Virtual WAN hub).
type RouteServer struct {
	ID                         string                `json:"id"`
	Name                       string                `json:"name"`
	Location                   string                `json:"location"`
	ResourceGroup              string                `json:"resourceGroup"`
	ProvisioningState          string                `json:"provisioningState"`
	Tags                       map[string]string     `json:"tags"`
	VirtualRouterAsn           int64                 `json:"virtualRouterAsn"`
	VirtualRouterIps           []string              `json:"virtualRouterIps"`
	HubRoutingPreference       string                `json:"hubRoutingPreference"`
	AllowBranchToBranchTraffic bool                  `json:"allowBranchToBranchTraffic"`
	IPConfigurations           []RouteServerIPConfig `json:"ipConfigurations"`
	BGPConnections             []RouteServerBGPPeer  `json:"bgpConnections"`
}

// PrivateDNSLink represents a VNet link within a private DNS zone.
type PrivateDNSLink struct {
	ID                  string     `json:"id"`
	Name                string     `json:"name"`
	VirtualNetwork      *azVNetRef `json:"virtualNetwork"`
	RegistrationEnabled bool       `json:"registrationEnabled"`
}

// VirtualNetworkID returns the VNet ID from the nested reference.
func (l PrivateDNSLink) VirtualNetworkID() string {
	if l.VirtualNetwork != nil {
		return l.VirtualNetwork.ID
	}
	return ""
}

// PrivateDNSZone represents an Azure private DNS zone.
type PrivateDNSZone struct {
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	ResourceGroup     string            `json:"resourceGroup"`
	ProvisioningState string            `json:"provisioningState"`
	Tags              map[string]string `json:"tags,omitempty"`
	Links             []PrivateDNSLink  `json:"links"`
}

// FlowLog represents a Microsoft.Network/networkWatchers/flowLogs resource.
// Collected via az graph query so all flow logs are returned in a single call.
// Note: ARM / Resource Graph returns "enabled" as 0/1 or true/false depending
// on the API version, so the field is omitted here to avoid unmarshal errors.
// All configured flow logs (enabled or not) describe a topology edge.
type FlowLog struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Location         string `json:"location"`
	ResourceGroup    string `json:"resourceGroup"`
	TargetResourceID string `json:"targetResourceId"` // ARM ID of the NSG being monitored
	StorageID        string `json:"storageId"`        // ARM ID of the storage account receiving logs
}

// DNSZone represents an Azure public DNS zone.
type DNSZone struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	ResourceGroup     string `json:"resourceGroup"`
	ProvisioningState string `json:"provisioningState"`
}

// NATGateway represents an Azure NAT gateway.
type NATGateway struct {
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	Location          string            `json:"location"`
	ResourceGroup     string            `json:"resourceGroup"`
	Tags              map[string]string `json:"tags"`
	ProvisioningState string            `json:"provisioningState"`
	PublicIPAddresses []azPublicIPRef   `json:"publicIpAddresses"`
	Subnets           []azSubnetRef     `json:"subnets"`
}

// PublicIPAddressIDs returns the public IP IDs from nested references.
func (n NATGateway) PublicIPAddressIDs() []string {
	ids := make([]string, 0, len(n.PublicIPAddresses))
	for _, p := range n.PublicIPAddresses {
		if p.ID != "" {
			ids = append(ids, p.ID)
		}
	}
	return ids
}

// SubnetIDs returns the subnet IDs from nested references.
func (n NATGateway) SubnetIDs() []string {
	ids := make([]string, 0, len(n.Subnets))
	for _, s := range n.Subnets {
		if s.ID != "" {
			ids = append(ids, s.ID)
		}
	}
	return ids
}

// azServiceProviderProperties matches the az CLI nested serviceProviderProperties object.
type azServiceProviderProperties struct {
	PeeringLocation     string `json:"peeringLocation"`
	ServiceProviderName string `json:"serviceProviderName"`
	BandwidthInMbps     int    `json:"bandwidthInMbps"`
}

// ExpressRoutePeering represents a peering configuration on an ExpressRoute circuit.
type ExpressRoutePeering struct {
	Name                       string `json:"name"`
	PeeringType                string `json:"peeringType"`
	State                      string `json:"state"`
	ProvisioningState          string `json:"provisioningState"`
	AzureASN                   int64  `json:"azureASN"`
	PeerASN                    int64  `json:"peerASN"`
	VlanID                     int    `json:"vlanId"`
	PrimaryPeerAddressPrefix   string `json:"primaryPeerAddressPrefix"`
	SecondaryPeerAddressPrefix string `json:"secondaryPeerAddressPrefix"`
	PrimaryAzurePort           string `json:"primaryAzurePort"`
	SecondaryAzurePort         string `json:"secondaryAzurePort"`
	LastModifiedBy             string `json:"lastModifiedBy"`
}

// ExpressRouteCircuit represents an Azure ExpressRoute circuit.
type ExpressRouteCircuit struct {
	ID                               string                       `json:"id"`
	Name                             string                       `json:"name"`
	Location                         string                       `json:"location"`
	ResourceGroup                    string                       `json:"resourceGroup"`
	Tags                             map[string]string            `json:"tags"`
	ProvisioningState                string                       `json:"provisioningState"`
	SKU                              azSKU                        `json:"sku"`
	ServiceProviderProperties        *azServiceProviderProperties `json:"serviceProviderProperties"`
	CircuitProvisioningState         string                       `json:"circuitProvisioningState"`
	ServiceProviderProvisioningState string                       `json:"serviceProviderProvisioningState"`
	ServiceKey                       string                       `json:"serviceKey"`
	AllowGlobalReach                 bool                         `json:"allowGlobalReach"`
	GlobalReachEnabled               bool                         `json:"globalReachEnabled"`
	AllowClassicOperations           bool                         `json:"allowClassicOperations"`
	EnableDirectPortRateLimit        bool                         `json:"enableDirectPortRateLimit"`
	Peerings                         []ExpressRoutePeering        `json:"peerings"`
}

// AzureFirewall represents an Azure Firewall resource.
type AzureFirewall struct {
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	Location          string            `json:"location"`
	ResourceGroup     string            `json:"resourceGroup"`
	Tags              map[string]string `json:"tags"`
	ProvisioningState string            `json:"provisioningState"`
}

// azAGWSKU is the SKU block on an Application Gateway.
type azAGWSKU struct {
	Name     string `json:"name"`
	Tier     string `json:"tier"`
	Capacity int    `json:"capacity"`
}

// azWAFConfig is the WAF configuration block on an Application Gateway.
type azWAFConfig struct {
	Enabled        bool   `json:"enabled"`
	FirewallMode   string `json:"firewallMode"`
	RuleSetType    string `json:"ruleSetType"`
	RuleSetVersion string `json:"ruleSetVersion"`
}

// ApplicationGateway represents an Azure Application Gateway.
type ApplicationGateway struct {
	ID                                  string            `json:"id"`
	Name                                string            `json:"name"`
	Location                            string            `json:"location"`
	ResourceGroup                       string            `json:"resourceGroup"`
	Tags                                map[string]string `json:"tags"`
	Zones                               []string          `json:"zones"`
	ProvisioningState                   string            `json:"provisioningState"`
	OperationalState                    string            `json:"operationalState"`
	SKU                                 azAGWSKU          `json:"sku"`
	WebApplicationFirewallConfiguration *azWAFConfig      `json:"webApplicationFirewallConfiguration"`
	HTTPListeners                       []struct{}        `json:"httpListeners"`
	BackendAddressPools                 []struct{}        `json:"backendAddressPools"`
	FrontendIPConfigurations            []struct{}        `json:"frontendIPConfigurations"`
	EnableHttp2                         *bool             `json:"enableHttp2"`
}

// azVMHardwareProfile captures the vmSize from the nested hardwareProfile block.
type azVMHardwareProfile struct {
	VMSize string `json:"vmSize"`
}

// azVMImageRef captures the image reference (OS image) from storageProfile.
type azVMImageRef struct {
	Publisher string `json:"publisher"`
	Offer     string `json:"offer"`
	Sku       string `json:"sku"`
	Version   string `json:"version"`
}

// azVMOSDisk captures OS disk info from storageProfile.
type azVMOSDisk struct {
	OSType string `json:"osType"`
}

// azVMStorageProfile captures image reference and OS disk.
type azVMStorageProfile struct {
	ImageReference azVMImageRef `json:"imageReference"`
	OSDisk         azVMOSDisk   `json:"osDisk"`
}

// azVMOSProfile captures OS profile (computer name, admin user).
type azVMOSProfile struct {
	ComputerName  string `json:"computerName"`
	AdminUsername string `json:"adminUsername"`
}

// azVMNICRef is a reference to a NIC attached to a VM.
type azVMNICRef struct {
	ID string `json:"id"`
}

// azVMNetworkProfile captures network interface references.
type azVMNetworkProfile struct {
	NetworkInterfaces []azVMNICRef `json:"networkInterfaces"`
}

// VMExtension represents a single VM extension collected via az vm extension list.
// Collected separately from az vm list; not part of the ARM list response body.
type VMExtension struct {
	Name                    string `json:"name"`
	Publisher               string `json:"publisher"`
	ExtType                 string `json:"typePropertiesType"` // extension type name
	TypeHandlerVersion      string `json:"typeHandlerVersion"`
	ProvisioningState       string `json:"provisioningState"`
	AutoUpgradeMinorVersion bool   `json:"autoUpgradeMinorVersion"`
	EnableAutomaticUpgrade  *bool  `json:"enableAutomaticUpgrade"`
}

// VirtualMachine represents an Azure VM.
// Field names match both `az vm list -d` (top-level flattened) and `az vm list` (nested ARM).
type VirtualMachine struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Location      string            `json:"location"`
	ResourceGroup string            `json:"resourceGroup"`
	Tags          map[string]string `json:"tags"`
	Zones         []string          `json:"zones"`
	// Top-level fields added by az vm list -d / az vm list (flattened by CLI)
	VMSize            string `json:"vmSize"`
	PowerState        string `json:"powerState"`
	ProvisioningState string `json:"provisioningState"`
	VMId              string `json:"vmId"`
	LicenseType       string `json:"licenseType"`
	// Nested ARM structure fields (from az vm list full JSON)
	HardwareProfile azVMHardwareProfile `json:"hardwareProfile"`
	StorageProfile  azVMStorageProfile  `json:"storageProfile"`
	OsProfile       azVMOSProfile       `json:"osProfile"`
	NetworkProfile  azVMNetworkProfile  `json:"networkProfile"`
	// Collected separately under --purpose=audit via collectVMExtensions.
	VMExtensions []VMExtension `json:"-"`
}

// azAppServicePlanSKU matches the az CLI nested SKU object for App Service Plans,
// which exposes richer fields than the generic azSKU.
type azAppServicePlanSKU struct {
	Name     string `json:"name"`
	Tier     string `json:"tier"`
	Size     string `json:"size"`
	Family   string `json:"family"`
	Capacity int    `json:"capacity"`
}

// AppServicePlan represents an Azure App Service Plan (Microsoft.Web/serverfarms).
// Field names match the az appservice plan list JSON output.
type AppServicePlan struct {
	ID                        string              `json:"id"`
	Name                      string              `json:"name"`
	Location                  string              `json:"location"`
	ResourceGroup             string              `json:"resourceGroup"`
	Tags                      map[string]string   `json:"tags"`
	Kind                      string              `json:"kind"`
	SKU                       azAppServicePlanSKU `json:"sku"`
	Reserved                  bool                `json:"reserved"`
	PerSiteScaling            bool                `json:"perSiteScaling"`
	ZoneRedundant             bool                `json:"zoneRedundant"`
	NumberOfSites             int                 `json:"numberOfSites"`
	NumberOfWorkers           int                 `json:"numberOfWorkers"`
	MaximumElasticWorkerCount int                 `json:"maximumElasticWorkerCount"`
	Status                    string              `json:"status"`
}

// azPrivateEndpointConnRef matches the az CLI nested privateEndpoint reference
// inside a site's privateEndpointConnections entry.
type azPrivateEndpointConnRef struct {
	Properties struct {
		PrivateEndpoint struct {
			ID string `json:"id"`
		} `json:"privateEndpoint"`
	} `json:"properties"`
}

// PrivateEndpointID returns the ARM ID of the referenced private endpoint.
func (p azPrivateEndpointConnRef) PrivateEndpointID() string {
	return p.Properties.PrivateEndpoint.ID
}

// azOutboundVnetRouting matches the az CLI nested outboundVnetRouting flags.
type azOutboundVnetRouting struct {
	AllTraffic             bool `json:"allTraffic"`
	ApplicationTraffic     bool `json:"applicationTraffic"`
	ContentShareTraffic    bool `json:"contentShareTraffic"`
	ImagePullTraffic       bool `json:"imagePullTraffic"`
	BackupRestoreTraffic   bool `json:"backupRestoreTraffic"`
	ManagedIdentityTraffic bool `json:"managedIdentityTraffic"`
}

// azSiteIdentity matches the az CLI nested identity block on a site.
type azSiteIdentity struct {
	Type                   string         `json:"type"`
	PrincipalID            string         `json:"principalId"`
	TenantID               string         `json:"tenantId"`
	UserAssignedIdentities map[string]any `json:"userAssignedIdentities"`
}

// UserAssignedIdentityIDs returns the ARM IDs of the user-assigned identities.
func (s azSiteIdentity) UserAssignedIdentityIDs() []string {
	if len(s.UserAssignedIdentities) == 0 {
		return nil
	}
	ids := make([]string, 0, len(s.UserAssignedIdentities))
	for k := range s.UserAssignedIdentities {
		if k != "" {
			ids = append(ids, k)
		}
	}
	return ids
}

// WebApp represents an Azure App Service site (Microsoft.Web/sites).
// Covers web apps, function apps, and container apps hosted on an App Service Plan.
// Field names match the az webapp list JSON output, which flattens the
// ARM "properties" block to the top level.
type WebApp struct {
	ID                         string                     `json:"id"`
	Name                       string                     `json:"name"`
	Location                   string                     `json:"location"`
	ResourceGroup              string                     `json:"resourceGroup"`
	Tags                       map[string]string          `json:"tags"`
	Kind                       string                     `json:"kind"`
	Identity                   *azSiteIdentity            `json:"identity"`
	State                      string                     `json:"state"`
	Enabled                    bool                       `json:"enabled"`
	DefaultHostName            string                     `json:"defaultHostName"`
	HostNames                  []string                   `json:"hostNames"`
	HTTPSOnly                  bool                       `json:"httpsOnly"`
	ClientCertEnabled          bool                       `json:"clientCertEnabled"`
	ClientCertMode             string                     `json:"clientCertMode"`
	ServerFarmID               string                     `json:"serverFarmId"`
	AppServicePlanID           string                     `json:"appServicePlanId"`
	VirtualNetworkSubnetID     string                     `json:"virtualNetworkSubnetId"`
	PublicNetworkAccess        string                     `json:"publicNetworkAccess"`
	InboundIPAddress           string                     `json:"inboundIpAddress"`
	OutboundIPAddresses        string                     `json:"outboundIpAddresses"`
	PossibleOutboundIPs        string                     `json:"possibleOutboundIpAddresses"`
	RedundancyMode             string                     `json:"redundancyMode"`
	ManagedEnvironmentID       string                     `json:"managedEnvironmentId"`
	OutboundVnetRouting        *azOutboundVnetRouting     `json:"outboundVnetRouting"`
	PrivateEndpointConnections []azPrivateEndpointConnRef `json:"privateEndpointConnections"`
	SiteConfig                 *WebAppSiteConfig          `json:"siteConfig"`
}

// WebAppSiteConfig carries the site configuration sub-fields relevant for
// topology: runtime version, worker counts and high-level flags.
type WebAppSiteConfig struct {
	LinuxFxVersion              string `json:"linuxFxVersion"`
	WindowsFxVersion            string `json:"windowsFxVersion"`
	NumberOfWorkers             int    `json:"numberOfWorkers"`
	AlwaysOn                    bool   `json:"alwaysOn"`
	HTTP20Enabled               bool   `json:"http20Enabled"`
	MinTLSVersion               string `json:"minTlsVersion"`
	FunctionAppScaleLimit       int    `json:"functionAppScaleLimit"`
	MinimumElasticInstanceCount int    `json:"minimumElasticInstanceCount"`
	ACRUseManagedIdentityCreds  bool   `json:"acrUseManagedIdentityCreds"`
}

// IsFunctionApp returns true when the site is a Function App.
// Azure marks function apps with a "functionapp" token in the kind field.
func (w WebApp) IsFunctionApp() bool {
	return strings.Contains(strings.ToLower(w.Kind), "functionapp")
}

// HostPlanID returns the ARM ID of the App Service Plan hosting this site.
// `az webapp list` flattens the ARM `.properties.serverFarmId` to
// `appServicePlanId` at the top level, while other az commands and raw ARM
// keep the `serverFarmId` name. Both are accepted transparently.
func (w WebApp) HostPlanID() string {
	if w.ServerFarmID != "" {
		return w.ServerFarmID
	}
	return w.AppServicePlanID
}

// ApplicationSecurityGroup represents an Azure Application Security Group
// (Microsoft.Network/applicationSecurityGroups). ASGs are identity-only
// resources; membership is expressed via NIC ipConfigurations.
type ApplicationSecurityGroup struct {
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	Location          string            `json:"location"`
	ResourceGroup     string            `json:"resourceGroup"`
	Tags              map[string]string `json:"tags"`
	ProvisioningState string            `json:"provisioningState"`
}

// azStorageSKU is the SKU sub-object on a storage account.
type azStorageSKU struct {
	Name string `json:"name"`
	Tier string `json:"tier"`
}

// azStorageEndpoints holds the service endpoint URLs for a storage account.
type azStorageEndpoints struct {
	Blob  string `json:"blob"`
	Queue string `json:"queue"`
	Table string `json:"table"`
	File  string `json:"file"`
	Web   string `json:"web"`
	Dfs   string `json:"dfs"`
}

// azStorageNetworkRuleSet is the top-level network rule set block on a storage
// account. az storage account list returns this as "networkRuleSet" at top level.
type azStorageNetworkRuleSet struct {
	DefaultAction       string              `json:"defaultAction"`
	Bypass              string              `json:"bypass"`
	IPRules             []azStorageIPRule   `json:"ipRules"`
	VirtualNetworkRules []azStorageVNetRule `json:"virtualNetworkRules"`
}
type azStorageIPRule struct {
	Value  string `json:"value"`
	Action string `json:"action"`
}
type azStorageVNetRule struct {
	ID string `json:"virtualNetworkResourceId"`
}

// azStorageEncryption captures the encryption key source block.
type azStorageEncryption struct {
	KeySource          string                  `json:"keySource"`
	KeyVaultProperties *azStorageKeyVaultProps `json:"keyVaultProperties"`
}
type azStorageKeyVaultProps struct {
	KeyName     string `json:"keyname"`
	KeyVaultURI string `json:"keyvaulturi"`
}

// StorageAccount represents Microsoft.Storage/storageAccounts.
// Field names match the flattened `az storage account list` JSON output.
type StorageAccount struct {
	ID                          string                     `json:"id"`
	Name                        string                     `json:"name"`
	Location                    string                     `json:"location"`
	ResourceGroup               string                     `json:"resourceGroup"`
	Tags                        map[string]string          `json:"tags"`
	Kind                        string                     `json:"kind"`
	SKU                         azStorageSKU               `json:"sku"`
	AccessTier                  string                     `json:"accessTier"`
	AllowBlobPublicAccess       *bool                      `json:"allowBlobPublicAccess"`
	AllowSharedKeyAccess        *bool                      `json:"allowSharedKeyAccess"`
	AllowCrossTenantReplication *bool                      `json:"allowCrossTenantReplication"`
	EnableHTTPSTrafficOnly      bool                       `json:"enableHttpsTrafficOnly"`
	MinimumTLSVersion           string                     `json:"minimumTlsVersion"`
	PublicNetworkAccess         string                     `json:"publicNetworkAccess"`
	IsHnsEnabled                bool                       `json:"isHnsEnabled"`
	ProvisioningState           string                     `json:"provisioningState"`
	StatusOfPrimary             string                     `json:"statusOfPrimary"`
	PrimaryEndpoints            *azStorageEndpoints        `json:"primaryEndpoints"`
	NetworkRuleSet              *azStorageNetworkRuleSet   `json:"networkRuleSet"`
	Encryption                  *azStorageEncryption       `json:"encryption"`
	PrivateEndpointConnections  []azPrivateEndpointConnRef `json:"privateEndpointConnections"`
}

// azKeyVaultSKU is the SKU block on a Key Vault.
type azKeyVaultSKU struct {
	Family string `json:"family"`
	Name   string `json:"name"`
}

// azKeyVaultNetworkACLs captures the Key Vault network ACL block.
type azKeyVaultNetworkACLs struct {
	Bypass              string               `json:"bypass"`
	DefaultAction       string               `json:"defaultAction"`
	IPRules             []azKeyVaultIPRule   `json:"ipRules"`
	VirtualNetworkRules []azKeyVaultVNetRule `json:"virtualNetworkRules"`
}
type azKeyVaultIPRule struct {
	Value string `json:"value"`
}
type azKeyVaultVNetRule struct {
	ID string `json:"id"`
}

// KeyVault represents Microsoft.KeyVault/vaults.
// `az keyvault list` emits properties.* flattened to the top level; a few
// fields (sku, networkAcls, privateEndpointConnections, properties.vaultUri,
// tenantId) come from the nested properties block and are carried here via
// the properties alias.
type KeyVault struct {
	ID            string              `json:"id"`
	Name          string              `json:"name"`
	Location      string              `json:"location"`
	ResourceGroup string              `json:"resourceGroup"`
	Tags          map[string]string   `json:"tags"`
	Properties    *KeyVaultProperties `json:"properties"`
}

// KeyVaultProperties holds the nested block returned by `az keyvault list`.
type KeyVaultProperties struct {
	SKU                          azKeyVaultSKU              `json:"sku"`
	TenantID                     string                     `json:"tenantId"`
	VaultURI                     string                     `json:"vaultUri"`
	EnableRbacAuthorization      bool                       `json:"enableRbacAuthorization"`
	EnableSoftDelete             *bool                      `json:"enableSoftDelete"`
	SoftDeleteRetentionInDays    int                        `json:"softDeleteRetentionInDays"`
	EnablePurgeProtection        *bool                      `json:"enablePurgeProtection"`
	EnabledForDeployment         bool                       `json:"enabledForDeployment"`
	EnabledForDiskEncryption     bool                       `json:"enabledForDiskEncryption"`
	EnabledForTemplateDeployment bool                       `json:"enabledForTemplateDeployment"`
	PublicNetworkAccess          string                     `json:"publicNetworkAccess"`
	ProvisioningState            string                     `json:"provisioningState"`
	MinimumTLSVersion            string                     `json:"minimumTlsVersion"`
	NetworkACLs                  *azKeyVaultNetworkACLs     `json:"networkAcls"`
	PrivateEndpointConnections   []azPrivateEndpointConnRef `json:"privateEndpointConnections"`
}

// azACRSKU is the SKU block on a Container Registry.
type azACRSKU struct {
	Name string `json:"name"`
	Tier string `json:"tier"`
}

// ContainerRegistry represents Microsoft.ContainerRegistry/registries.
// `az acr list` flattens properties to the top level.
type ContainerRegistry struct {
	ID                         string                     `json:"id"`
	Name                       string                     `json:"name"`
	Location                   string                     `json:"location"`
	ResourceGroup              string                     `json:"resourceGroup"`
	Tags                       map[string]string          `json:"tags"`
	SKU                        azACRSKU                   `json:"sku"`
	LoginServer                string                     `json:"loginServer"`
	AdminUserEnabled           bool                       `json:"adminUserEnabled"`
	AnonymousPullEnabled       bool                       `json:"anonymousPullEnabled"`
	DataEndpointEnabled        bool                       `json:"dataEndpointEnabled"`
	PublicNetworkAccess        string                     `json:"publicNetworkAccess"`
	ZoneRedundancy             string                     `json:"zoneRedundancy"`
	ProvisioningState          string                     `json:"provisioningState"`
	PrivateEndpointConnections []azPrivateEndpointConnRef `json:"privateEndpointConnections"`
	Replications               []ACRReplication           `json:"-"`
}

// ACRReplication represents Microsoft.ContainerRegistry/registries/replications.
type ACRReplication struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Location          string `json:"location"`
	ProvisioningState string `json:"provisioningState"`
	RegionEndpoint    bool   `json:"regionEndpointEnabled"`
	ZoneRedundancy    string `json:"zoneRedundancy"`
}

// ManagedIdentity represents a Microsoft.ManagedIdentity/userAssignedIdentities
// resource. The system-assigned variant is exposed via the parent resource's
// identity block (webapp, VM, etc.) and is NOT a standalone ARM resource.
type ManagedIdentity struct {
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	Location          string            `json:"location"`
	ResourceGroup     string            `json:"resourceGroup"`
	Tags              map[string]string `json:"tags"`
	ProvisioningState string            `json:"provisioningState"`
	PrincipalID       string            `json:"principalId"`
	ClientID          string            `json:"clientId"`
	TenantID          string            `json:"tenantId"`
}

// azDiskSKU is the SKU block on a managed disk / snapshot.
type azDiskSKU struct {
	Name string `json:"name"`
	Tier string `json:"tier"`
}

// azDiskCreationData captures the creation source. For snapshots this points
// to the disk (or VM image) the snapshot was taken from.
type azDiskCreationData struct {
	CreateOption     string `json:"createOption"`
	SourceResourceID string `json:"sourceResourceId"`
	SourceURI        string `json:"sourceUri"`
}

// Disk represents Microsoft.Compute/disks (managed disks).
// `az disk list` flattens properties to the top level.
type Disk struct {
	ID                string              `json:"id"`
	Name              string              `json:"name"`
	Location          string              `json:"location"`
	ResourceGroup     string              `json:"resourceGroup"`
	Tags              map[string]string   `json:"tags"`
	SKU               azDiskSKU           `json:"sku"`
	DiskSizeGB        int                 `json:"diskSizeGb"`
	DiskIOPSReadWrite int                 `json:"diskIopsReadWrite"`
	DiskMBPSReadWrite int                 `json:"diskMBpsReadWrite"`
	DiskState         string              `json:"diskState"`
	OSType            string              `json:"osType"`
	ManagedBy         string              `json:"managedBy"`
	ProvisioningState string              `json:"provisioningState"`
	Zones             []string            `json:"zones"`
	CreationData      *azDiskCreationData `json:"creationData"`
}

// Snapshot represents Microsoft.Compute/snapshots.
// `az snapshot list` flattens properties to the top level.
type Snapshot struct {
	ID                string              `json:"id"`
	Name              string              `json:"name"`
	Location          string              `json:"location"`
	ResourceGroup     string              `json:"resourceGroup"`
	Tags              map[string]string   `json:"tags"`
	SKU               azDiskSKU           `json:"sku"`
	DiskSizeGB        int                 `json:"diskSizeGb"`
	Incremental       bool                `json:"incremental"`
	OSType            string              `json:"osType"`
	ProvisioningState string              `json:"provisioningState"`
	CreationData      *azDiskCreationData `json:"creationData"`
}

// ApplicationInsights represents a Microsoft.Insights/components resource.
// `az resource list --resource-type microsoft.insights/components` returns a
// generic ARM envelope with `properties` nested (not flattened) so the struct
// mirrors that shape.
type ApplicationInsights struct {
	ID            string                 `json:"id"`
	Name          string                 `json:"name"`
	Location      string                 `json:"location"`
	ResourceGroup string                 `json:"resourceGroup"`
	Tags          map[string]string      `json:"tags"`
	Kind          string                 `json:"kind"`
	Properties    *AppInsightsProperties `json:"properties"`
}

// AppInsightsProperties holds the nested properties block for App Insights.
// Secret fields (InstrumentationKey, ConnectionString, AppID) are deliberately
// NOT captured as described in the OSIRIS JSON specification chapter 13.
type AppInsightsProperties struct {
	ApplicationType                 string  `json:"Application_Type"`
	WorkspaceResourceID             string  `json:"WorkspaceResourceId"`
	RetentionInDays                 int     `json:"RetentionInDays"`
	SamplingPercentage              float64 `json:"SamplingPercentage"`
	PublicNetworkAccessForIngestion string  `json:"publicNetworkAccessForIngestion"`
	PublicNetworkAccessForQuery     string  `json:"publicNetworkAccessForQuery"`
	DisableIPMasking                bool    `json:"DisableIpMasking"`
	DisableLocalAuth                bool    `json:"DisableLocalAuth"`
	ProvisioningState               string  `json:"provisioningState"`
	IngestionMode                   string  `json:"IngestionMode"`
}

// WorkspaceResourceID returns the bound Log Analytics workspace ARM ID for
// workspace-based App Insights, or "" for classic (retiring) components.
func (a ApplicationInsights) WorkspaceResourceID() string {
	if a.Properties == nil {
		return ""
	}
	return a.Properties.WorkspaceResourceID
}

// azLAWorkspaceSKU is the SKU block on a Log Analytics workspace.
type azLAWorkspaceSKU struct {
	Name          string `json:"name"`
	LastSKUUpdate string `json:"lastSkuUpdate"`
}

// azLAWorkspaceCapping captures the daily ingestion cap.
type azLAWorkspaceCapping struct {
	DailyQuotaGB float64 `json:"dailyQuotaGb"`
}

// LogAnalyticsWorkspace represents Microsoft.OperationalInsights/workspaces.
// `az monitor log-analytics workspace list` flattens properties to the top
// level. CustomerID is the workspace UUID used in KQL queries (not a secret).
// Shared keys (primary/secondary) are NOT captured here - they are
// authentication material.
type LogAnalyticsWorkspace struct {
	ID                              string                `json:"id"`
	Name                            string                `json:"name"`
	Location                        string                `json:"location"`
	ResourceGroup                   string                `json:"resourceGroup"`
	Tags                            map[string]string     `json:"tags"`
	CustomerID                      string                `json:"customerId"`
	ProvisioningState               string                `json:"provisioningState"`
	SKU                             *azLAWorkspaceSKU     `json:"sku"`
	RetentionInDays                 int                   `json:"retentionInDays"`
	PublicNetworkAccessForIngestion string                `json:"publicNetworkAccessForIngestion"`
	PublicNetworkAccessForQuery     string                `json:"publicNetworkAccessForQuery"`
	ForceCmkForQuery                bool                  `json:"forceCmkForQuery"`
	WorkspaceCapping                *azLAWorkspaceCapping `json:"workspaceCapping"`
}

// azRSVaultSKU is the SKU block on a Recovery Services Vault.
type azRSVaultSKU struct {
	Name string `json:"name"`
	Tier string `json:"tier"`
}

// azRSVaultRedundancy captures the storage redundancy configuration.
type azRSVaultRedundancy struct {
	StandardTierStorageRedundancy string `json:"standardTierStorageRedundancy"`
	CrossRegionRestore            string `json:"crossRegionRestore"`
}

// RSVaultProperties holds the nested properties block for a RS vault.
type RSVaultProperties struct {
	ProvisioningState          string                     `json:"provisioningState"`
	PublicNetworkAccess        string                     `json:"publicNetworkAccess"`
	RedundancySettings         *azRSVaultRedundancy       `json:"redundancySettings"`
	PrivateEndpointConnections []azPrivateEndpointConnRef `json:"privateEndpointConnections"`
}

// RecoveryServicesVault represents Microsoft.RecoveryServices/vaults.
// `az backup vault list` returns the full ARM envelope with properties nested.
// Protected items are populated separately via a per-vault enumeration.
type RecoveryServicesVault struct {
	ID             string                `json:"id"`
	Name           string                `json:"name"`
	Location       string                `json:"location"`
	ResourceGroup  string                `json:"resourceGroup"`
	Tags           map[string]string     `json:"tags"`
	SKU            *azRSVaultSKU         `json:"sku"`
	Properties     *RSVaultProperties    `json:"properties"`
	ProtectedItems []BackupProtectedItem `json:"-"`
}

// azBackupVaultStorageSetting captures the storage redundancy setting.
type azBackupVaultStorageSetting struct {
	DatastoreType string `json:"datastoreType"`
	Type          string `json:"type"`
}

// azBackupVaultImmutability captures the immutability state for a Backup Vault.
type azBackupVaultImmutability struct {
	State string `json:"state"`
}

// azBackupVaultSoftDelete captures soft-delete state and retention.
type azBackupVaultSoftDelete struct {
	State                   string  `json:"state"`
	RetentionDurationInDays float64 `json:"retentionDurationInDays"`
}

// azBackupVaultSecuritySettings captures the security configuration.
type azBackupVaultSecuritySettings struct {
	ImmutabilitySettings *azBackupVaultImmutability `json:"immutabilitySettings"`
	SoftDeleteSettings   *azBackupVaultSoftDelete   `json:"softDeleteSettings"`
}

// BackupVaultProperties holds the nested properties block for a Backup Vault.
type BackupVaultProperties struct {
	ProvisioningState string                         `json:"provisioningState"`
	StorageSettings   []azBackupVaultStorageSetting  `json:"storageSettings"`
	SecuritySettings  *azBackupVaultSecuritySettings `json:"securitySettings"`
}

// BackupVault represents Microsoft.DataProtection/backupVaults.
// `az resource list --resource-type Microsoft.DataProtection/backupVaults`
// returns a generic ARM envelope; we enrich per-vault for full properties.
// Backup instances are populated separately via per-vault enumeration.
type BackupVault struct {
	ID                 string                 `json:"id"`
	Name               string                 `json:"name"`
	Location           string                 `json:"location"`
	ResourceGroup      string                 `json:"resourceGroup"`
	Tags               map[string]string      `json:"tags"`
	Properties         *BackupVaultProperties `json:"properties"`
	ProtectedInstances []BackupInstance       `json:"-"`
}

// azProtectedItemProperties captures the `properties` block on an item
// returned by `az backup item list`. SourceResourceID points to the
// protected ARM resource (VM, SQL server, file share, etc).
type azProtectedItemProperties struct {
	FriendlyName      string `json:"friendlyName"`
	ProtectedItemType string `json:"protectedItemType"`
	WorkloadType      string `json:"workloadType"`
	SourceResourceID  string `json:"sourceResourceId"`
	ProtectionState   string `json:"protectionState"`
	ProtectionStatus  string `json:"protectionStatus"`
	PolicyName        string `json:"policyName"`
}

// BackupProtectedItem represents one backed-up resource inside an RS Vault.
// `az backup item list` returns the ARM envelope with properties nested.
type BackupProtectedItem struct {
	ID         string                     `json:"id"`
	Name       string                     `json:"name"`
	Properties *azProtectedItemProperties `json:"properties"`
}

// SourceResourceID returns the ARM ID of the protected resource.
func (b BackupProtectedItem) SourceResourceID() string {
	if b.Properties == nil {
		return ""
	}
	return b.Properties.SourceResourceID
}

// azDataSourceInfo captures the dataSourceInfo block inside a backup instance.
type azDataSourceInfo struct {
	ResourceID     string `json:"resourceID"`
	DatasourceType string `json:"datasourceType"`
	ResourceName   string `json:"resourceName"`
	ResourceType   string `json:"resourceType"`
}

// azBackupInstanceStatus captures the current protection status.
type azBackupInstanceStatus struct {
	Status string `json:"status"`
}

// azBackupInstanceProperties captures the `properties` block on a backup instance.
type azBackupInstanceProperties struct {
	FriendlyName           string                  `json:"friendlyName"`
	DataSourceInfo         *azDataSourceInfo       `json:"dataSourceInfo"`
	ProtectionStatus       *azBackupInstanceStatus `json:"protectionStatus"`
	CurrentProtectionState string                  `json:"currentProtectionState"`
	PolicyInfo             map[string]any          `json:"policyInfo"`
}

// BackupInstance represents one backed-up resource inside a Backup Vault.
// `az dataprotection backup-instance list` returns the ARM envelope.
type BackupInstance struct {
	ID         string                      `json:"id"`
	Name       string                      `json:"name"`
	Properties *azBackupInstanceProperties `json:"properties"`
}

// SourceResourceID returns the ARM ID of the protected resource from the
// dataSourceInfo block.
func (b BackupInstance) SourceResourceID() string {
	if b.Properties == nil || b.Properties.DataSourceInfo == nil {
		return ""
	}
	return b.Properties.DataSourceInfo.ResourceID
}

// azSQLServerProperties holds the nested `properties` block on a SQL server.
// We only model topology-relevant fields; auditing / threat-detection /
// backup policy / TDE settings are operational policy and stay out of scope.
type azSQLServerProperties struct {
	Version                       string                     `json:"version"`
	AdministratorLogin            string                     `json:"administratorLogin"`
	FullyQualifiedDomainName      string                     `json:"fullyQualifiedDomainName"`
	State                         string                     `json:"state"`
	PublicNetworkAccess           string                     `json:"publicNetworkAccess"`
	MinimalTLSVersion             string                     `json:"minimalTlsVersion"`
	RestrictOutboundNetworkAccess string                     `json:"restrictOutboundNetworkAccess"`
	PrivateEndpointConnections    []azPrivateEndpointConnRef `json:"privateEndpointConnections"`
}

// SQLServer represents Microsoft.Sql/servers.
// Databases are populated via per-server `az sql db list` iteration.
type SQLServer struct {
	ID            string                 `json:"id"`
	Name          string                 `json:"name"`
	Location      string                 `json:"location"`
	ResourceGroup string                 `json:"resourceGroup"`
	Tags          map[string]string      `json:"tags"`
	Kind          string                 `json:"kind"`
	Properties    *azSQLServerProperties `json:"properties"`
	Databases     []SQLDatabase          `json:"-"`
	ElasticPools  []SQLElasticPool       `json:"-"`
}

// azSQLDatabaseSKU captures the SKU block on a SQL database.
type azSQLDatabaseSKU struct {
	Name     string `json:"name"`
	Tier     string `json:"tier"`
	Capacity int    `json:"capacity"`
	Family   string `json:"family"`
}

// azSQLDatabaseProperties captures the nested `properties` block on a SQL DB.
type azSQLDatabaseProperties struct {
	Collation          string `json:"collation"`
	Status             string `json:"status"`
	MaxSizeBytes       int64  `json:"maxSizeBytes"`
	ZoneRedundant      bool   `json:"zoneRedundant"`
	ReadScale          string `json:"readScale"`
	StorageAccountType string `json:"storageAccountType"`
	DatabaseID         string `json:"databaseId"`
}

// SQLDatabase represents Microsoft.Sql/servers/databases.
type SQLDatabase struct {
	ID            string                   `json:"id"`
	Name          string                   `json:"name"`
	Location      string                   `json:"location"`
	ResourceGroup string                   `json:"resourceGroup"`
	Tags          map[string]string        `json:"tags"`
	Kind          string                   `json:"kind"`
	SKU           *azSQLDatabaseSKU        `json:"sku"`
	Properties    *azSQLDatabaseProperties `json:"properties"`
	// ServerID is the parent Microsoft.Sql/servers ARM ID; derived at collection
	// time because az returns it only implicitly via the db ID path.
	ServerID string `json:"-"`
}

// azFlexServerSKU is the SKU block on PG/MySQL flexible server.
type azFlexServerSKU struct {
	Name string `json:"name"`
	Tier string `json:"tier"`
}

// azFlexServerStorage captures the storage block on PG/MySQL flexible server.
type azFlexServerStorage struct {
	StorageSizeGB int    `json:"storageSizeGb"`
	AutoGrow      string `json:"autoGrow"`
	Tier          string `json:"tier"`
	Iops          int    `json:"iops"`
}

// azFlexServerNetwork captures VNet integration details.
type azFlexServerNetwork struct {
	DelegatedSubnetResourceID   string `json:"delegatedSubnetResourceId"`
	PrivateDNSZoneArmResourceID string `json:"privateDnsZoneArmResourceId"`
	PublicNetworkAccess         string `json:"publicNetworkAccess"`
}

// azFlexServerHA captures the high-availability config.
type azFlexServerHA struct {
	Mode                    string `json:"mode"`
	StandbyAvailabilityZone string `json:"standbyAvailabilityZone"`
	State                   string `json:"state"`
}

// azFlexServerProperties is shared by PG and MySQL flexible server outputs.
type azFlexServerProperties struct {
	Version                  string               `json:"version"`
	AdministratorLogin       string               `json:"administratorLogin"`
	FullyQualifiedDomainName string               `json:"fullyQualifiedDomainName"`
	State                    string               `json:"state"`
	AvailabilityZone         string               `json:"availabilityZone"`
	ReplicationRole          string               `json:"replicationRole"`
	Storage                  *azFlexServerStorage `json:"storage"`
	Network                  *azFlexServerNetwork `json:"network"`
	HighAvailability         *azFlexServerHA      `json:"highAvailability"`
}

// PostgreSQLServer represents Microsoft.DBforPostgreSQL/flexibleServers.
// Single-server (the legacy Microsoft.DBforPostgreSQL/servers API) is
// end-of-life per Azure roadmap and intentionally not modeled.
type PostgreSQLServer struct {
	ID            string                  `json:"id"`
	Name          string                  `json:"name"`
	Location      string                  `json:"location"`
	ResourceGroup string                  `json:"resourceGroup"`
	Tags          map[string]string       `json:"tags"`
	SKU           *azFlexServerSKU        `json:"sku"`
	Properties    *azFlexServerProperties `json:"properties"`
}

// MySQLServer represents Microsoft.DBforMySQL/flexibleServers.
type MySQLServer struct {
	ID            string                  `json:"id"`
	Name          string                  `json:"name"`
	Location      string                  `json:"location"`
	ResourceGroup string                  `json:"resourceGroup"`
	Tags          map[string]string       `json:"tags"`
	SKU           *azFlexServerSKU        `json:"sku"`
	Properties    *azFlexServerProperties `json:"properties"`
}

// azCosmosLocation is one geo-replica entry on a Cosmos account.
type azCosmosLocation struct {
	LocationName     string `json:"locationName"`
	FailoverPriority int    `json:"failoverPriority"`
	IsZoneRedundant  bool   `json:"isZoneRedundant"`
}

// azCosmosConsistency captures the consistency policy.
type azCosmosConsistency struct {
	DefaultConsistencyLevel string `json:"defaultConsistencyLevel"`
	MaxStalenessPrefix      int64  `json:"maxStalenessPrefix"`
	MaxIntervalInSeconds    int64  `json:"maxIntervalInSeconds"`
}

// azCosmosCapability captures one entry of the capabilities array
// (e.g. EnableMongo, EnableCassandra, EnableTable, EnableGremlin).
type azCosmosCapability struct {
	Name string `json:"name"`
}

// azCosmosVNetRule captures a virtualNetworkRules entry.
type azCosmosVNetRule struct {
	ID                               string `json:"id"`
	IgnoreMissingVNetServiceEndpoint bool   `json:"ignoreMissingVNetServiceEndpoint"`
}

// azCosmosProperties captures the `properties` block on a Cosmos account.
// Account primary/secondary keys and connection strings are returned only
// from `listKeys` / `listConnectionStrings`, which we deliberately do not
// call - those are credentials.
type azCosmosProperties struct {
	DatabaseAccountOfferType      string                     `json:"databaseAccountOfferType"`
	ProvisioningState             string                     `json:"provisioningState"`
	DocumentEndpoint              string                     `json:"documentEndpoint"`
	PublicNetworkAccess           string                     `json:"publicNetworkAccess"`
	EnableAutomaticFailover       bool                       `json:"enableAutomaticFailover"`
	EnableMultipleWriteLocations  bool                       `json:"enableMultipleWriteLocations"`
	IsVirtualNetworkFilterEnabled bool                       `json:"isVirtualNetworkFilterEnabled"`
	EnableFreeTier                bool                       `json:"enableFreeTier"`
	DisableLocalAuth              bool                       `json:"disableLocalAuth"`
	ConsistencyPolicy             *azCosmosConsistency       `json:"consistencyPolicy"`
	WriteLocations                []azCosmosLocation         `json:"writeLocations"`
	ReadLocations                 []azCosmosLocation         `json:"readLocations"`
	Locations                     []azCosmosLocation         `json:"locations"`
	Capabilities                  []azCosmosCapability       `json:"capabilities"`
	VirtualNetworkRules           []azCosmosVNetRule         `json:"virtualNetworkRules"`
	PrivateEndpointConnections    []azPrivateEndpointConnRef `json:"privateEndpointConnections"`
}

// CosmosAccount represents Microsoft.DocumentDB/databaseAccounts.
// Kind drives the API surface: GlobalDocumentDB (SQL API), MongoDB, Parse.
// The API family is further refined by Properties.Capabilities entries
// (EnableCassandra, EnableTable, EnableGremlin, EnableMongo).
type CosmosAccount struct {
	ID            string              `json:"id"`
	Name          string              `json:"name"`
	Location      string              `json:"location"`
	ResourceGroup string              `json:"resourceGroup"`
	Tags          map[string]string   `json:"tags"`
	Kind          string              `json:"kind"`
	Properties    *azCosmosProperties `json:"properties"`
}

// azRedisSKU is the SKU block on a Redis cache (Basic/Standard/Premium + C/P family).
type azRedisSKU struct {
	Name     string `json:"name"`
	Family   string `json:"family"`
	Capacity int    `json:"capacity"`
}

// RedisCache represents Microsoft.Cache/Redis. Azure CLI flattens properties
// onto the top level (`hostName`, `port`, `sslPort`, `redisVersion`, ...)
// while nesting `sku` as a sibling.  Access keys are never collected - they
// live behind `az redis list-keys` which we don't call.
type RedisCache struct {
	ID                         string                     `json:"id"`
	Name                       string                     `json:"name"`
	Location                   string                     `json:"location"`
	ResourceGroup              string                     `json:"resourceGroup"`
	Tags                       map[string]string          `json:"tags"`
	Zones                      []string                   `json:"zones"`
	SKU                        *azRedisSKU                `json:"sku"`
	RedisVersion               string                     `json:"redisVersion"`
	ProvisioningState          string                     `json:"provisioningState"`
	EnableNonSSLPort           bool                       `json:"enableNonSslPort"`
	MinimumTLSVersion          string                     `json:"minimumTlsVersion"`
	PublicNetworkAccess        string                     `json:"publicNetworkAccess"`
	HostName                   string                     `json:"hostName"`
	Port                       int                        `json:"port"`
	SSLPort                    int                        `json:"sslPort"`
	ShardCount                 int                        `json:"shardCount"`
	ReplicasPerMaster          int                        `json:"replicasPerMaster"`
	SubnetID                   string                     `json:"subnetId"`
	StaticIP                   string                     `json:"staticIP"`
	PrivateEndpointConnections []azPrivateEndpointConnRef `json:"privateEndpointConnections"`
}

// azAKSNetworkProfile captures the nested networkProfile block.
type azAKSNetworkProfile struct {
	NetworkPlugin   string `json:"networkPlugin"`
	NetworkPolicy   string `json:"networkPolicy"`
	ServiceCIDR     string `json:"serviceCidr"`
	PodCIDR         string `json:"podCidr"`
	DNSServiceIP    string `json:"dnsServiceIp"`
	LoadBalancerSKU string `json:"loadBalancerSku"`
	OutboundType    string `json:"outboundType"`
}

// azAKSAPIServerAccessProfile captures the api-server access profile.
type azAKSAPIServerAccessProfile struct {
	EnablePrivateCluster           bool     `json:"enablePrivateCluster"`
	PrivateDNSZone                 string   `json:"privateDnsZone"`
	AuthorizedIPRanges             []string `json:"authorizedIpRanges"`
	EnablePrivateClusterPublicFQDN bool     `json:"enablePrivateClusterPublicFqdn"`
}

// azAKSAADProfile captures AAD integration.
type azAKSAADProfile struct {
	Managed             bool     `json:"managed"`
	EnableAzureRBAC     bool     `json:"enableAzureRbac"`
	AdminGroupObjectIDs []string `json:"adminGroupObjectIDs"`
	TenantID            string   `json:"tenantID"`
}

// AKSAgentPool represents one node pool within an AKS cluster.
// Agent pools are fetched per cluster via `az aks nodepool list`.
type AKSAgentPool struct {
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	VMSize            string            `json:"vmSize"`
	Count             int               `json:"count"`
	MinCount          int               `json:"minCount"`
	MaxCount          int               `json:"maxCount"`
	EnableAutoScaling bool              `json:"enableAutoScaling"`
	OSType            string            `json:"osType"`
	OSSKU             string            `json:"osSku"`
	Mode              string            `json:"mode"`
	OrchestratorVer   string            `json:"orchestratorVersion"`
	VNetSubnetID      string            `json:"vnetSubnetId"`
	PodSubnetID       string            `json:"podSubnetId"`
	AvailabilityZones []string          `json:"availabilityZones"`
	ProvisioningState string            `json:"provisioningState"`
	PowerState        map[string]string `json:"powerState"`
	ClusterID         string            `json:"-"`
	ClusterName       string            `json:"-"`
}

// azAKSSKU captures the SKU block (Free/Standard + Base/Automatic name).
type azAKSSKU struct {
	Name string `json:"name"`
	Tier string `json:"tier"`
}

// azAKSProperties captures the AKS `properties` block.
type azAKSProperties struct {
	KubernetesVersion          string                       `json:"kubernetesVersion"`
	DNSPrefix                  string                       `json:"dnsPrefix"`
	FQDN                       string                       `json:"fqdn"`
	AzurePortalFQDN            string                       `json:"azurePortalFqdn"`
	EnableRBAC                 bool                         `json:"enableRbac"`
	ProvisioningState          string                       `json:"provisioningState"`
	PowerState                 map[string]string            `json:"powerState"`
	NetworkProfile             *azAKSNetworkProfile         `json:"networkProfile"`
	APIServerAccessProfile     *azAKSAPIServerAccessProfile `json:"apiServerAccessProfile"`
	AADProfile                 *azAKSAADProfile             `json:"aadProfile"`
	NodeResourceGroup          string                       `json:"nodeResourceGroup"`
	DisableLocalAccounts       bool                         `json:"disableLocalAccounts"`
	OIDCIssuerProfile          map[string]any               `json:"oidcIssuerProfile"`
	PrivateLinkResources       []map[string]any             `json:"privateLinkResources"`
	PrivateEndpointConnections []azPrivateEndpointConnRef   `json:"privateEndpointConnections"`
}

// AKSCluster represents Microsoft.ContainerService/managedClusters.
type AKSCluster struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Location      string            `json:"location"`
	ResourceGroup string            `json:"resourceGroup"`
	Tags          map[string]string `json:"tags"`
	SKU           *azAKSSKU         `json:"sku"`
	Properties    *azAKSProperties  `json:"properties"`
	// AgentPools is populated via per-cluster `az aks nodepool list`. The
	// cluster's flat `properties.agentPoolProfiles` is available but it
	// omits the ARM IDs of the pools, so a second call is needed to get
	// individual resources with IDs that can be wired.
	AgentPools []AKSAgentPool `json:"-"`
}

// azContainerEnvVNetConfig captures the vnetConfiguration block on a managed env.
type azContainerEnvVNetConfig struct {
	InfrastructureSubnetID string `json:"infrastructureSubnetId"`
	Internal               bool   `json:"internal"`
	PlatformReservedCIDR   string `json:"platformReservedCidr"`
	PlatformReservedDNSIP  string `json:"platformReservedDnsIP"`
	DockerBridgeCIDR       string `json:"dockerBridgeCidr"`
}

// azContainerEnvProperties captures the managed environment properties block.
type azContainerEnvProperties struct {
	ProvisioningState    string                    `json:"provisioningState"`
	DefaultDomain        string                    `json:"defaultDomain"`
	StaticIP             string                    `json:"staticIp"`
	ZoneRedundant        bool                      `json:"zoneRedundant"`
	VNetConfiguration    *azContainerEnvVNetConfig `json:"vnetConfiguration"`
	WorkloadProfiles     []map[string]any          `json:"workloadProfiles"`
	AppLogsConfiguration map[string]any            `json:"appLogsConfiguration"`
}

// ContainerAppEnvironment represents Microsoft.App/managedEnvironments.
type ContainerAppEnvironment struct {
	ID            string                    `json:"id"`
	Name          string                    `json:"name"`
	Location      string                    `json:"location"`
	ResourceGroup string                    `json:"resourceGroup"`
	Tags          map[string]string         `json:"tags"`
	Properties    *azContainerEnvProperties `json:"properties"`
}

// azContainerAppConfigIngress captures the ingress section of configuration.
type azContainerAppConfigIngress struct {
	External      bool   `json:"external"`
	TargetPort    int    `json:"targetPort"`
	Transport     string `json:"transport"`
	AllowInsecure bool   `json:"allowInsecure"`
	FQDN          string `json:"fqdn"`
}

// azContainerAppConfig captures the configuration block. Secrets list is
// *not* emitted; only the top-level app shape.
type azContainerAppConfig struct {
	ActiveRevisionsMode string                       `json:"activeRevisionsMode"`
	Ingress             *azContainerAppConfigIngress `json:"ingress"`
}

// azContainerAppProperties captures the container app properties block.
type azContainerAppProperties struct {
	ProvisioningState    string                `json:"provisioningState"`
	ManagedEnvironmentID string                `json:"managedEnvironmentId"`
	EnvironmentID        string                `json:"environmentId"`
	LatestRevisionName   string                `json:"latestRevisionName"`
	LatestRevisionFQDN   string                `json:"latestRevisionFqdn"`
	WorkloadProfileName  string                `json:"workloadProfileName"`
	Configuration        *azContainerAppConfig `json:"configuration"`
}

// ContainerApp represents Microsoft.App/containerApps.
// EnvironmentID() returns whichever of properties.environmentId or
// properties.managedEnvironmentId is populated, because Azure CLI versions
// differ on which field they flatten at the top level.
type ContainerApp struct {
	ID            string                    `json:"id"`
	Name          string                    `json:"name"`
	Location      string                    `json:"location"`
	ResourceGroup string                    `json:"resourceGroup"`
	Tags          map[string]string         `json:"tags"`
	Properties    *azContainerAppProperties `json:"properties"`
}

// EnvironmentID returns the parent managed-environment ARM ID.
func (a ContainerApp) EnvironmentID() string {
	if a.Properties == nil {
		return ""
	}
	if a.Properties.EnvironmentID != "" {
		return a.Properties.EnvironmentID
	}
	return a.Properties.ManagedEnvironmentID
}

// azContainerGroupIPAddress captures the ipAddress block on a container group.
type azContainerGroupIPAddress struct {
	IP       string           `json:"ip"`
	Type     string           `json:"type"` // Public / Private
	DNSLabel string           `json:"dnsNameLabel"`
	FQDN     string           `json:"fqdn"`
	Ports    []map[string]any `json:"ports"`
}

// azContainerGroupSubnetRef captures one subnetIds entry (VNet-integrated).
type azContainerGroupSubnetRef struct {
	ID string `json:"id"`
}

// azContainerGroupProperties captures the container group properties.
type azContainerGroupProperties struct {
	ProvisioningState string                      `json:"provisioningState"`
	OSType            string                      `json:"osType"`
	RestartPolicy     string                      `json:"restartPolicy"`
	Sku               string                      `json:"sku"`
	IPAddress         *azContainerGroupIPAddress  `json:"ipAddress"`
	SubnetIDs         []azContainerGroupSubnetRef `json:"subnetIds"`
	Containers        []map[string]any            `json:"containers"`
	InitContainers    []map[string]any            `json:"initContainers"`
}

// ContainerGroup represents Microsoft.ContainerInstance/containerGroups.
type ContainerGroup struct {
	ID            string                      `json:"id"`
	Name          string                      `json:"name"`
	Location      string                      `json:"location"`
	ResourceGroup string                      `json:"resourceGroup"`
	Tags          map[string]string           `json:"tags"`
	Properties    *azContainerGroupProperties `json:"properties"`
}

// azMessagingSKU captures Service Bus / Event Hubs namespace SKU.
type azMessagingSKU struct {
	Name     string `json:"name"`
	Tier     string `json:"tier"`
	Capacity int    `json:"capacity"`
}

// azMessagingProperties captures the properties block for Service Bus and
// Event Hubs namespaces (fields overlap enough to share).
type azMessagingProperties struct {
	ProvisioningState          string                     `json:"provisioningState"`
	Status                     string                     `json:"status"`
	ServiceBusEndpoint         string                     `json:"serviceBusEndpoint"`
	ZoneRedundant              bool                       `json:"zoneRedundant"`
	DisableLocalAuth           bool                       `json:"disableLocalAuth"`
	PublicNetworkAccess        string                     `json:"publicNetworkAccess"`
	MinimumTLSVersion          string                     `json:"minimumTlsVersion"`
	PrivateEndpointConnections []azPrivateEndpointConnRef `json:"privateEndpointConnections"`
}

// ServiceBusNamespace represents Microsoft.ServiceBus/namespaces.
type ServiceBusNamespace struct {
	ID            string                 `json:"id"`
	Name          string                 `json:"name"`
	Location      string                 `json:"location"`
	ResourceGroup string                 `json:"resourceGroup"`
	Tags          map[string]string      `json:"tags"`
	SKU           *azMessagingSKU        `json:"sku"`
	Properties    *azMessagingProperties `json:"properties"`
}

// EventHubsNamespace represents Microsoft.EventHub/namespaces.
type EventHubsNamespace struct {
	ID            string                 `json:"id"`
	Name          string                 `json:"name"`
	Location      string                 `json:"location"`
	ResourceGroup string                 `json:"resourceGroup"`
	Tags          map[string]string      `json:"tags"`
	SKU           *azMessagingSKU        `json:"sku"`
	Properties    *azMessagingProperties `json:"properties"`
}

// azAPIMSKU captures the APIM sku block.
type azAPIMSKU struct {
	Name     string `json:"name"`
	Capacity int    `json:"capacity"`
}

// azAPIMVirtualNetworkConfig captures VNet integration config.
type azAPIMVirtualNetworkConfig struct {
	SubnetResourceID string `json:"subnetResourceId"`
}

// azAPIMProperties captures the properties block on an APIM service.
type azAPIMProperties struct {
	ProvisioningState           string                      `json:"provisioningState"`
	GatewayURL                  string                      `json:"gatewayUrl"`
	PortalURL                   string                      `json:"portalUrl"`
	ManagementURL               string                      `json:"managementApiUrl"`
	PublisherEmail              string                      `json:"publisherEmail"`
	PublisherName               string                      `json:"publisherName"`
	VirtualNetworkType          string                      `json:"virtualNetworkType"` // None / External / Internal
	VirtualNetworkConfiguration *azAPIMVirtualNetworkConfig `json:"virtualNetworkConfiguration"`
	PublicIPAddresses           []string                    `json:"publicIPAddresses"`
	PrivateIPAddresses          []string                    `json:"privateIPAddresses"`
	PublicNetworkAccess         string                      `json:"publicNetworkAccess"`
	DisableGateway              bool                        `json:"disableGateway"`
	EnableClientCertificate     bool                        `json:"enableClientCertificate"`
	PrivateEndpointConnections  []azPrivateEndpointConnRef  `json:"privateEndpointConnections"`
}

// APIMService represents Microsoft.ApiManagement/service.
type APIMService struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Location      string            `json:"location"`
	ResourceGroup string            `json:"resourceGroup"`
	Tags          map[string]string `json:"tags"`
	SKU           *azAPIMSKU        `json:"sku"`
	Properties    *azAPIMProperties `json:"properties"`
}

// azFrontDoorSKU captures the Front Door profile SKU
// (Standard_AzureFrontDoor or Premium_AzureFrontDoor).
type azFrontDoorSKU struct {
	Name string `json:"name"`
}

// azFrontDoorProperties captures the profile properties.
type azFrontDoorProperties struct {
	ProvisioningState string `json:"provisioningState"`
	ResourceState     string `json:"resourceState"`
	FrontDoorID       string `json:"frontDoorId"`
}

// FrontDoorProfile represents a Microsoft.Cdn/profiles entry whose SKU is
// Standard_AzureFrontDoor or Premium_AzureFrontDoor. Classic Azure Front Door
// (Microsoft.Network/frontDoors) is deprecated and intentionally not modeled.
type FrontDoorProfile struct {
	ID            string                 `json:"id"`
	Name          string                 `json:"name"`
	Location      string                 `json:"location"`
	ResourceGroup string                 `json:"resourceGroup"`
	Tags          map[string]string      `json:"tags"`
	SKU           *azFrontDoorSKU        `json:"sku"`
	Kind          string                 `json:"kind"`
	Properties    *azFrontDoorProperties `json:"properties"`
}

// azMetricAlertCriteriaDimension captures a single dimension filter on a metric alert criterion.
type azMetricAlertCriteriaDimension struct {
	Name   string   `json:"name"`
	Values []string `json:"values"`
}

// azMetricAlertCriteria captures a single criteria entry in a metric alert.
type azMetricAlertCriteria struct {
	MetricName      string                           `json:"metricName"`
	MetricNamespace string                           `json:"metricNamespace"`
	Operator        string                           `json:"operator"`
	Threshold       float64                          `json:"threshold"`
	TimeAggregation string                           `json:"timeAggregation"`
	Dimensions      []azMetricAlertCriteriaDimension `json:"dimensions"`
}

// azMetricAlertCriteriaContainer is the discriminated-union wrapper Azure Monitor returns for
// the "criteria" field. The API encodes it as an object with an odata.type discriminator and
// an "allOf" array of individual criteria, NOT as a JSON array.
type azMetricAlertCriteriaContainer struct {
	ODataType string                  `json:"odata.type"`
	AllOf     []azMetricAlertCriteria `json:"allOf"`
}

// azMetricAlertActionGroup is a reference to an action group in a metric alert.
type azMetricAlertActionGroup struct {
	ActionGroupID string `json:"actionGroupId"`
}

// MetricAlert represents an Azure Monitor metric alert rule.
type MetricAlert struct {
	ID                  string                         `json:"id"`
	Name                string                         `json:"name"`
	Location            string                         `json:"location"`
	ResourceGroup       string                         `json:"resourceGroup"`
	Tags                map[string]string              `json:"tags"`
	Description         string                         `json:"description"`
	Severity            int                            `json:"severity"`
	Enabled             bool                           `json:"enabled"`
	EvaluationFrequency string                         `json:"evaluationFrequency"`
	WindowSize          string                         `json:"windowSize"`
	Scopes              []string                       `json:"scopes"`
	Criteria            azMetricAlertCriteriaContainer `json:"criteria"`
	Actions             []azMetricAlertActionGroup     `json:"actions"`
	AutoMitigate        bool                           `json:"autoMitigate"`
	TargetResourceType  string                         `json:"targetResourceType"`
}

// ActionGroup represents an Azure Monitor action group.
type ActionGroup struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Location       string            `json:"location"`
	ResourceGroup  string            `json:"resourceGroup"`
	Tags           map[string]string `json:"tags"`
	GroupShortName string            `json:"groupShortName"`
	Enabled        bool              `json:"enabled"`
	// Receiver arrays: full payloads not stored; len() gives the count per channel.
	EmailReceivers             []struct{} `json:"emailReceivers"`
	SmsReceivers               []struct{} `json:"smsReceivers"`
	WebhookReceivers           []struct{} `json:"webhookReceivers"`
	ArmRoleReceivers           []struct{} `json:"armRoleReceivers"`
	AzureFunctionReceivers     []struct{} `json:"azureFunctionReceivers"`
	EventHubReceivers          []struct{} `json:"eventHubReceivers"`
	ItsmReceivers              []struct{} `json:"itsmReceivers"`
	AzureAppPushReceivers      []struct{} `json:"azureAppPushReceivers"`
	AutomationRunbookReceivers []struct{} `json:"automationRunbookReceivers"`
	VoiceReceivers             []struct{} `json:"voiceReceivers"`
	LogicAppReceivers          []struct{} `json:"logicAppReceivers"`
}

// BastionIPConfig holds an Azure Bastion host IP configuration.
type BastionIPConfig struct {
	Name            string         `json:"name"`
	PublicIPAddress *azPublicIPRef `json:"publicIpAddress"`
	Subnet          *azSubnetRef   `json:"subnet"`
}

// BastionHost represents a Microsoft.Network/bastionHosts resource.
type BastionHost struct {
	ID                  string            `json:"id"`
	Name                string            `json:"name"`
	Location            string            `json:"location"`
	ResourceGroup       string            `json:"resourceGroup"`
	Tags                map[string]string `json:"tags"`
	ProvisioningState   string            `json:"provisioningState"`
	SKU                 azSKU             `json:"sku"`
	DNSName             string            `json:"dnsName"`
	ScaleUnits          int               `json:"scaleUnits"`
	EnableTunneling     bool              `json:"enableTunneling"`
	EnableIpConnect     bool              `json:"enableIpConnect"`
	DisableCopyPaste    bool              `json:"disableCopyPaste"`
	EnableShareableLink bool              `json:"enableShareableLink"`
	EnableKerberos      bool              `json:"enableKerberos"`
	IPConfigurations    []BastionIPConfig `json:"ipConfigurations"`
}

// TrafficManagerDNSConfig holds the DNS configuration for a Traffic Manager profile.
type TrafficManagerDNSConfig struct {
	RelativeName string `json:"relativeName"`
	FQDN         string `json:"fqdn"`
	TTL          int    `json:"ttl"`
}

// TrafficManagerMonitorConfig holds the health monitoring configuration.
type TrafficManagerMonitorConfig struct {
	Protocol                  string `json:"protocol"`
	Port                      int    `json:"port"`
	Path                      string `json:"path"`
	IntervalInSeconds         int    `json:"intervalInSeconds"`
	ToleratedNumberOfFailures int    `json:"toleratedNumberOfFailures"`
	TimeoutInSeconds          int    `json:"timeoutInSeconds"`
	ProfileMonitorStatus      string `json:"profileMonitorStatus"`
}

// TrafficManagerEndpoint holds a single endpoint within a Traffic Manager profile.
type TrafficManagerEndpoint struct {
	Name                  string `json:"name"`
	Type                  string `json:"type"`
	EndpointStatus        string `json:"endpointStatus"`
	EndpointMonitorStatus string `json:"endpointMonitorStatus"`
	TargetResourceID      string `json:"targetResourceId"`
	Target                string `json:"target"`
	Weight                int    `json:"weight"`
	Priority              int    `json:"priority"`
	EndpointLocation      string `json:"endpointLocation"`
}

// TrafficManagerProfile represents a Microsoft.Network/trafficManagerProfiles resource.
type TrafficManagerProfile struct {
	ID                   string                      `json:"id"`
	Name                 string                      `json:"name"`
	Location             string                      `json:"location"`
	ResourceGroup        string                      `json:"resourceGroup"`
	Tags                 map[string]string           `json:"tags"`
	ProfileStatus        string                      `json:"profileStatus"`
	TrafficRoutingMethod string                      `json:"trafficRoutingMethod"`
	DNSConfig            TrafficManagerDNSConfig     `json:"dnsConfig"`
	MonitorConfig        TrafficManagerMonitorConfig `json:"monitorConfig"`
	Endpoints            []TrafficManagerEndpoint    `json:"endpoints"`
}

// DNSPrivateResolverProperties holds the nested properties block for a DNS private resolver.
// az resource list --resource-type keeps properties nested (not flattened).
type DNSPrivateResolverProperties struct {
	ProvisioningState string       `json:"provisioningState"`
	DNSResolverState  string       `json:"dnsResolverState"`
	VirtualNetwork    *azSubnetRef `json:"virtualNetwork"`
}

// DNSPrivateResolver represents a Microsoft.Network/dnsResolvers resource.
// Collected via az resource list --resource-type (no extension required).
type DNSPrivateResolver struct {
	ID            string                        `json:"id"`
	Name          string                        `json:"name"`
	Location      string                        `json:"location"`
	ResourceGroup string                        `json:"resourceGroup"`
	Tags          map[string]string             `json:"tags"`
	Properties    *DNSPrivateResolverProperties `json:"properties"`
}

// DNSForwardingRulesetProperties holds the nested properties block for a DNS forwarding ruleset.
type DNSForwardingRulesetProperties struct {
	ProvisioningState            string        `json:"provisioningState"`
	DNSResolverOutboundEndpoints []azSubnetRef `json:"dnsResolverOutboundEndpoints"`
}

// DNSForwardingRuleset represents a Microsoft.Network/dnsForwardingRulesets resource.
// Collected via az resource list --resource-type (no extension required).
type DNSForwardingRuleset struct {
	ID            string                          `json:"id"`
	Name          string                          `json:"name"`
	Location      string                          `json:"location"`
	ResourceGroup string                          `json:"resourceGroup"`
	Tags          map[string]string               `json:"tags"`
	Properties    *DNSForwardingRulesetProperties `json:"properties"`
}

// azGraphSKU holds the SKU object returned by Azure Resource Graph.
type azGraphSKU struct {
	Name     string `json:"name"`
	Tier     string `json:"tier"`
	Size     string `json:"size"`
	Capacity int    `json:"capacity"`
}

// azGraphIdentity holds the managed-identity block returned by Azure Resource Graph.
type azGraphIdentity struct {
	Type        string `json:"type"`
	PrincipalID string `json:"principalId"`
	TenantID    string `json:"tenantId"`
}

// azGraphSystemData holds the ARM systemData block (lifecycle authorship + timestamps).
type azGraphSystemData struct {
	CreatedAt          string `json:"createdAt"`
	CreatedBy          string `json:"createdBy,omitempty"`
	CreatedByType      string `json:"createdByType,omitempty"`
	LastModifiedAt     string `json:"lastModifiedAt"`
	LastModifiedBy     string `json:"lastModifiedBy,omitempty"`
	LastModifiedByType string `json:"lastModifiedByType,omitempty"`
}

// azGraphPlan holds the Marketplace plan block from the ARM resource envelope.
type azGraphPlan struct {
	Name          string `json:"name"`
	Publisher     string `json:"publisher"`
	Product       string `json:"product"`
	PromotionCode string `json:"promotionCode"`
}

// GraphEnvelope holds the per-resource fields collected via az graph query and
// supplementary context calls (locks, diagnostic settings). The map key is the
// lowercase ARM resource ID; armID stores the original-case ID for CLI calls.
type GraphEnvelope struct {
	armID               string             // original-case ARM ID, not serialized
	Kind                string             `json:"kind,omitempty"`
	SKU                 *azGraphSKU        `json:"sku,omitempty"`
	Identity            *azGraphIdentity   `json:"identity,omitempty"`
	ManagedBy           string             `json:"managed_by,omitempty"`
	Etag                string             `json:"etag,omitempty"`
	Plan                *azGraphPlan       `json:"plan,omitempty"`
	CreatedTime         string             `json:"created_at,omitempty"`
	ChangedTime         string             `json:"changed_at,omitempty"`
	ProvisioningState   string             `json:"provisioning_state,omitempty"`
	SystemData          *azGraphSystemData `json:"system_data,omitempty"`
	PublicNetworkAccess string             `json:"public_network_access,omitempty"`
	Locks               []ResourceLock     `json:"locks,omitempty"`
	DiagSettings        []DiagSetting      `json:"diag_settings,omitempty"`
	RoleAssignments     []RoleAssignment   `json:"role_assignments,omitempty"`
}

// ResourceLock represents an Azure management lock attached to a resource.
type ResourceLock struct {
	Name  string `json:"name"`
	Level string `json:"level"` // CanNotDelete or ReadOnly
	Notes string `json:"notes,omitempty"`
}

// DiagSetting represents a single Azure Monitor diagnostic setting on a resource.
type DiagSetting struct {
	Name        string `json:"name"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	StorageID   string `json:"storage_id,omitempty"`
	EventHubID  string `json:"event_hub_id,omitempty"`
}

// RoleAssignment represents an Azure RBAC role assignment.
type RoleAssignment struct {
	RoleName      string `json:"role"`
	PrincipalID   string `json:"principal_id"`
	PrincipalType string `json:"principal_type,omitempty"`
	PrincipalName string `json:"principal_name,omitempty"`
}

// ScopedRoleAssignment pairs a role assignment with its ARM scope string.
// Used for group-level (RG / subscription / MG) role assignments that are
// stored in bulk on SubscriptionData rather than on individual GraphEnvelopes.
type ScopedRoleAssignment struct {
	Scope string
	RoleAssignment
}

// azRawRoleAssignment is the wire shape returned by az role assignment list.
type azRawRoleAssignment struct {
	PrincipalID        string `json:"principalId"`
	PrincipalName      string `json:"principalName"`
	PrincipalType      string `json:"principalType"`
	RoleDefinitionName string `json:"roleDefinitionName"`
	Scope              string `json:"scope"`
}

// H1 resource type structs

// VMSS is an Azure Virtual Machine Scale Set.
type VMSS struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Location      string            `json:"location"`
	ResourceGroup string            `json:"resourceGroup"`
	Tags          map[string]string `json:"tags"`
	SKU           *azVMSSSKU        `json:"sku"`
	Properties    *VMSSProperties   `json:"properties"`
}

type azVMSSSKU struct {
	Name     string `json:"name"`
	Tier     string `json:"tier"`
	Capacity int64  `json:"capacity"`
}

type VMSSProperties struct {
	ProvisioningState        string               `json:"provisioningState"`
	OrchestrationMode        string               `json:"orchestrationMode"`
	PlatformFaultDomainCount int                  `json:"platformFaultDomainCount"`
	UpgradePolicy            *azVMSSUpgradePolicy `json:"upgradePolicy"`
	VirtualMachineProfile    *azVMSSVMProfile     `json:"virtualMachineProfile"`
}

type azVMSSUpgradePolicy struct {
	Mode string `json:"mode"` // Automatic, Manual, Rolling
}

type azVMSSVMProfile struct {
	NetworkProfile *azVMSSNetworkProfile `json:"networkProfile"`
}

type azVMSSNetworkProfile struct {
	NetworkInterfaceConfigurations []azVMSSNICConfig `json:"networkInterfaceConfigurations"`
}

type azVMSSNICConfig struct {
	Properties *azVMSSNICConfigProps `json:"properties"`
}

type azVMSSNICConfigProps struct {
	Primary          bool             `json:"primary"`
	IPConfigurations []azVMSSIPConfig `json:"ipConfigurations"`
}

type azVMSSIPConfig struct {
	Properties *azVMSSIPConfigProps `json:"properties"`
}

type azVMSSIPConfigProps struct {
	Subnet *azVNetRef `json:"subnet"`
}

// VMSSSubnetIDs extracts all subnet ARM IDs wired to this VMSS (primary NIC, all IP configs).
func (v *VMSS) VMSSSubnetIDs() []string {
	if v.Properties == nil || v.Properties.VirtualMachineProfile == nil {
		return nil
	}
	np := v.Properties.VirtualMachineProfile.NetworkProfile
	if np == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, nic := range np.NetworkInterfaceConfigurations {
		if nic.Properties == nil {
			continue
		}
		for _, ip := range nic.Properties.IPConfigurations {
			if ip.Properties == nil || ip.Properties.Subnet == nil {
				continue
			}
			id := ip.Properties.Subnet.ID
			if id != "" && !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out
}

// SQLManagedInstance is an Azure SQL Managed Instance.
type SQLManagedInstance struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Location      string            `json:"location"`
	ResourceGroup string            `json:"resourceGroup"`
	Tags          map[string]string `json:"tags"`
	SKU           *azSQLMISKU       `json:"sku"`
	Properties    *SQLMIProperties  `json:"properties"`
	Databases     []SQLMIDatabase   `json:"-"`
}

// SQLMIDatabase represents Microsoft.Sql/managedInstances/databases.
type SQLMIDatabase struct {
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	Location          string            `json:"location"`
	ResourceGroup     string            `json:"resourceGroup"`
	Tags              map[string]string `json:"tags"`
	Properties        *SQLMIDBProps     `json:"properties"`
	ManagedInstanceID string            `json:"-"`
}

type SQLMIDBProps struct {
	ProvisioningState string `json:"provisioningState"`
	Status            string `json:"status"`
	Collation         string `json:"collation"`
}

// SQLElasticPool represents Microsoft.Sql/servers/elasticPools.
type SQLElasticPool struct {
	ID            string               `json:"id"`
	Name          string               `json:"name"`
	Location      string               `json:"location"`
	ResourceGroup string               `json:"resourceGroup"`
	Tags          map[string]string    `json:"tags"`
	Kind          string               `json:"kind"`
	SKU           *azSQLDatabaseSKU    `json:"sku"`
	Properties    *SQLElasticPoolProps `json:"properties"`
	ServerID      string               `json:"-"`
}

type SQLElasticPoolProps struct {
	ProvisioningState string `json:"provisioningState"`
	State             string `json:"state"`
	MaxSizeBytes      int64  `json:"maxSizeBytes"`
	ZoneRedundant     bool   `json:"zoneRedundant"`
	HighAvailability  *struct {
		HighAvailabilityReplicaCount int `json:"highAvailabilityReplicaCount"`
	} `json:"highAvailabilityReplicaConfiguration"`
}

// SQLVirtualMachine represents Microsoft.SqlVirtualMachine/SqlVirtualMachines.
type SQLVirtualMachine struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Location      string            `json:"location"`
	ResourceGroup string            `json:"resourceGroup"`
	Tags          map[string]string `json:"tags"`
	Properties    *SQLVMProps       `json:"properties"`
}

type SQLVMProps struct {
	ProvisioningState    string `json:"provisioningState"`
	SQLServerLicenseType string `json:"sqlServerLicenseType"`
	SQLManagementType    string `json:"sqlManagementType"`
	SQLImageSKU          string `json:"sqlImageSku"`
	VirtualMachineID     string `json:"virtualMachineResourceId"`
}

type azSQLMISKU struct {
	Name     string `json:"name"`
	Tier     string `json:"tier"`
	Family   string `json:"family"`
	Capacity int    `json:"capacity"`
}

type SQLMIProperties struct {
	ProvisioningState         string `json:"provisioningState"`
	State                     string `json:"state"`
	SubnetID                  string `json:"subnetId"`
	VCores                    int    `json:"vCores"`
	StorageSizeInGB           int    `json:"storageSizeInGB"`
	LicenseType               string `json:"licenseType"`
	PublicDataEndpointEnabled bool   `json:"publicDataEndpointEnabled"`
	MinimalTLSVersion         string `json:"minimalTlsVersion"`
	StorageAccountType        string `json:"storageAccountType"`
	FullyQualifiedDomainName  string `json:"fullyQualifiedDomainName"`
}

// LogicWorkflow is an Azure Logic Apps Standard workflow.
type LogicWorkflow struct {
	ID            string              `json:"id"`
	Name          string              `json:"name"`
	Location      string              `json:"location"`
	ResourceGroup string              `json:"resourceGroup"`
	Tags          map[string]string   `json:"tags"`
	Properties    *LogicWorkflowProps `json:"properties"`
}

type LogicWorkflowProps struct {
	ProvisioningState string `json:"provisioningState"`
	State             string `json:"state"` // Enabled, Disabled, Suspended, Deleted, Completed
	AccessEndpoint    string `json:"accessEndpoint"`
}

// DataFactory is an Azure Data Factory.
type DataFactory struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Location      string            `json:"location"`
	ResourceGroup string            `json:"resourceGroup"`
	Tags          map[string]string `json:"tags"`
	Properties    *DataFactoryProps `json:"properties"`
}

type DataFactoryProps struct {
	ProvisioningState   string `json:"provisioningState"`
	Version             string `json:"version"`
	PublicNetworkAccess string `json:"publicNetworkAccess"`
}

// SynapseWorkspace is an Azure Synapse Analytics workspace.
type SynapseWorkspace struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Location      string            `json:"location"`
	ResourceGroup string            `json:"resourceGroup"`
	Tags          map[string]string `json:"tags"`
	Properties    *SynapseProps     `json:"properties"`
}

type SynapseProps struct {
	ProvisioningState     string            `json:"provisioningState"`
	PublicNetworkAccess   string            `json:"publicNetworkAccess"`
	ManagedVirtualNetwork string            `json:"managedVirtualNetwork"`
	ConnectivityEndpoints map[string]string `json:"connectivityEndpoints"`
}

// CommunicationService is an Azure Communication Service.
type CommunicationService struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Location      string            `json:"location"`
	ResourceGroup string            `json:"resourceGroup"`
	Tags          map[string]string `json:"tags"`
	Properties    *CommServiceProps `json:"properties"`
}

type CommServiceProps struct {
	ProvisioningState string `json:"provisioningState"`
	DataLocation      string `json:"dataLocation"`
	HostName          string `json:"hostName"`
}

// AutomationAccount is an Azure Automation Account.
type AutomationAccount struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Location      string            `json:"location"`
	ResourceGroup string            `json:"resourceGroup"`
	Tags          map[string]string `json:"tags"`
	SKU           *azAutoAccountSKU `json:"sku"`
	Properties    *AutomationProps  `json:"properties"`
}

type azAutoAccountSKU struct {
	Name string `json:"name"` // Free, Basic
}

type AutomationProps struct {
	ProvisioningState   string `json:"provisioningState"`
	State               string `json:"state"` // Ok, Suspended, Unavailable
	PublicNetworkAccess *bool  `json:"publicNetworkAccess"`
}

// ArcMachine is an Azure Arc-enabled server.
type ArcMachine struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Location      string            `json:"location"`
	ResourceGroup string            `json:"resourceGroup"`
	Tags          map[string]string `json:"tags"`
	Properties    *ArcMachineProps  `json:"properties"`
	Extensions    []ArcExtension    `json:"-"`
}

// ArcExtension represents Microsoft.HybridCompute/machines/extensions.
type ArcExtension struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Type               string `json:"type"`
	Publisher          string `json:"publisher"`
	TypeHandlerVersion string `json:"typeHandlerVersion"`
	ProvisioningState  string `json:"provisioningState"`
	AutoUpgradeMinor   bool   `json:"autoUpgradeMinorVersion"`
}

type ArcMachineProps struct {
	ProvisioningState string `json:"provisioningState"`
	Status            string `json:"status"` // Connected, Disconnected, Expired, Error
	OSName            string `json:"osName"`
	OSVersion         string `json:"osVersion"`
	OSType            string `json:"osType"`
	AgentVersion      string `json:"agentVersion"`
	FQDN              string `json:"dnsFqdn"`
}

// DataCollectionRule is an Azure Monitor Data Collection Rule.
type DataCollectionRule struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Location      string            `json:"location"`
	ResourceGroup string            `json:"resourceGroup"`
	Tags          map[string]string `json:"tags"`
	Properties    *DCRProperties    `json:"properties"`
}

type DCRProperties struct {
	ProvisioningState string      `json:"provisioningState"`
	Description       string      `json:"description"`
	DataFlows         []azDCRFlow `json:"dataFlows"`
	Destinations      *azDCRDests `json:"destinations"`
}

type azDCRFlow struct {
	Streams      []string `json:"streams"`
	Destinations []string `json:"destinations"`
}

type azDCRDests struct {
	LogAnalytics []azDCRLogAnalyticsDest `json:"logAnalytics"`
}

type azDCRLogAnalyticsDest struct {
	WorkspaceResourceID string `json:"workspaceResourceId"`
	Name                string `json:"name"`
}

// WorkspaceResourceIDs returns the Log Analytics workspace ARM IDs configured as destinations.
func (d *DataCollectionRule) WorkspaceResourceIDs() []string {
	if d.Properties == nil || d.Properties.Destinations == nil {
		return nil
	}
	out := make([]string, 0, len(d.Properties.Destinations.LogAnalytics))
	for _, la := range d.Properties.Destinations.LogAnalytics {
		if la.WorkspaceResourceID != "" {
			out = append(out, la.WorkspaceResourceID)
		}
	}
	return out
}

// DataCollectionEndpoint is an Azure Monitor Data Collection Endpoint.
type DataCollectionEndpoint struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Location      string            `json:"location"`
	ResourceGroup string            `json:"resourceGroup"`
	Tags          map[string]string `json:"tags"`
	Properties    *DCEProperties    `json:"properties"`
}

type DCEProperties struct {
	ProvisioningState string        `json:"provisioningState"`
	NetworkAcls       *azDCENetAcls `json:"networkAcls"`
}

type azDCENetAcls struct {
	PublicNetworkAccess string `json:"publicNetworkAccess"`
}

// AutoscaleSetting is an Azure Monitor autoscale setting.
type AutoscaleSetting struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Location      string            `json:"location"`
	ResourceGroup string            `json:"resourceGroup"`
	Tags          map[string]string `json:"tags"`
	Properties    *AutoscaleProps   `json:"properties"`
}

type AutoscaleProps struct {
	Enabled           bool                 `json:"enabled"`
	TargetResourceURI string               `json:"targetResourceUri"`
	Profiles          []azAutoscaleProfile `json:"profiles"`
}

type azAutoscaleProfile struct {
	Name     string               `json:"name"`
	Capacity *azAutoscaleCapacity `json:"capacity"`
}

type azAutoscaleCapacity struct {
	Default string `json:"default"`
	Maximum string `json:"maximum"`
	Minimum string `json:"minimum"`
}

// LogicAPIConnection represents Microsoft.Web/connections (API connections consumed by Logic Apps).
type LogicAPIConnection struct {
	ID            string                   `json:"id"`
	Name          string                   `json:"name"`
	Location      string                   `json:"location"`
	ResourceGroup string                   `json:"resourceGroup"`
	Tags          map[string]string        `json:"tags"`
	Kind          string                   `json:"kind"`
	Properties    *LogicAPIConnectionProps `json:"properties"`
}

type LogicAPIConnectionProps struct {
	ProvisioningState string                    `json:"provisioningState"`
	API               *LogicAPIConnectionAPIRef `json:"api"`
	Statuses          []LogicAPIConnStatus      `json:"statuses"`
}

type LogicAPIConnectionAPIRef struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
}

type LogicAPIConnStatus struct {
	Status string `json:"status"`
	Target string `json:"target"`
}

// EmailService represents Microsoft.Communication/EmailServices.
type EmailService struct {
	ID            string             `json:"id"`
	Name          string             `json:"name"`
	Location      string             `json:"location"`
	ResourceGroup string             `json:"resourceGroup"`
	Tags          map[string]string  `json:"tags"`
	Properties    *EmailServiceProps `json:"properties"`
}

type EmailServiceProps struct {
	ProvisioningState string `json:"provisioningState"`
	DataLocation      string `json:"dataLocation"`
}

// EmailDomain represents Microsoft.Communication/EmailServices/Domains.
type EmailDomain struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Location       string            `json:"location"`
	ResourceGroup  string            `json:"resourceGroup"`
	Tags           map[string]string `json:"tags"`
	Properties     *EmailDomainProps `json:"properties"`
	EmailServiceID string            `json:"-"` // parent email service ARM ID (parsed from domain ID)
}

type EmailDomainProps struct {
	ProvisioningState string `json:"provisioningState"`
	DataLocation      string `json:"dataLocation"`
	DomainManagement  string `json:"domainManagement"`
	MailFrom          string `json:"mailFromSenderDomain"`
}

// StreamAnalyticsJob represents Microsoft.StreamAnalytics/streamingjobs.
type StreamAnalyticsJob struct {
	ID            string                `json:"id"`
	Name          string                `json:"name"`
	Location      string                `json:"location"`
	ResourceGroup string                `json:"resourceGroup"`
	Tags          map[string]string     `json:"tags"`
	SKU           *StreamAnalyticsSKU   `json:"sku"`
	Properties    *StreamAnalyticsProps `json:"properties"`
}

type StreamAnalyticsSKU struct {
	Name string `json:"name"`
}

type StreamAnalyticsProps struct {
	ProvisioningState  string `json:"provisioningState"`
	JobState           string `json:"jobState"`
	CompatibilityLevel string `json:"compatibilityLevel"`
	OutputStartMode    string `json:"outputStartMode"`
}

// EventGridSystemTopic represents Microsoft.EventGrid/systemTopics.
type EventGridSystemTopic struct {
	ID            string                     `json:"id"`
	Name          string                     `json:"name"`
	Location      string                     `json:"location"`
	ResourceGroup string                     `json:"resourceGroup"`
	Tags          map[string]string          `json:"tags"`
	Properties    *EventGridSystemTopicProps `json:"properties"`
}

type EventGridSystemTopicProps struct {
	ProvisioningState string `json:"provisioningState"`
	Source            string `json:"source"`
	TopicType         string `json:"topicType"`
}

// AppServiceSlot represents a Microsoft.Web/sites/slots deployment slot.
type AppServiceSlot struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Location        string            `json:"location"`
	ResourceGroup   string            `json:"resourceGroup"`
	Tags            map[string]string `json:"tags"`
	Kind            string            `json:"kind"`
	State           string            `json:"state"`
	Enabled         bool              `json:"enabled"`
	DefaultHostName string            `json:"defaultHostName"`
	SiteConfig      *WebAppSiteConfig `json:"siteConfig"`
	WebAppID        string            `json:"-"` // parent webapp ARM ID (derived from slot ID)
}

// SubscriptionData holds all collected Azure resources for a subscription.
type SubscriptionData struct {
	Subscription              SubscriptionInfo
	ResourceGroups            []ResourceGroup
	VirtualNetworks           []VirtualNetwork
	Subnets                   []Subnet
	NetworkInterfaces         []NetworkInterface
	SecurityGroups            []NetworkSecurityGroup
	RouteTables               []RouteTable
	PublicIPs                 []PublicIPAddress
	PublicIPPrefixes          []PublicIPPrefix
	AvailabilitySets          []AvailabilitySet
	LoadBalancers             []LoadBalancer
	PrivateEndpoints          []PrivateEndpoint
	VNetPeerings              []VNetPeering
	VNetGateways              []VNetGateway
	GatewayConnections        []GatewayConnection
	RouteServers              []RouteServer
	PrivateDNSZones           []PrivateDNSZone
	DNSZones                  []DNSZone
	NATGateways               []NATGateway
	ExpressRouteCircuits      []ExpressRouteCircuit
	AzureFirewalls            []AzureFirewall
	ApplicationGateways       []ApplicationGateway
	VirtualMachines           []VirtualMachine
	AppServicePlans           []AppServicePlan
	WebApps                   []WebApp
	ApplicationSecurityGroups []ApplicationSecurityGroup
	StorageAccounts           []StorageAccount
	KeyVaults                 []KeyVault
	ContainerRegistries       []ContainerRegistry
	ManagedIdentities         []ManagedIdentity
	Disks                     []Disk
	Snapshots                 []Snapshot
	ApplicationInsights       []ApplicationInsights
	LogAnalyticsWorkspaces    []LogAnalyticsWorkspace
	RecoveryServicesVaults    []RecoveryServicesVault
	BackupVaults              []BackupVault
	SQLServers                []SQLServer
	PostgreSQLServers         []PostgreSQLServer
	MySQLServers              []MySQLServer
	CosmosAccounts            []CosmosAccount
	RedisCaches               []RedisCache
	AKSClusters               []AKSCluster
	ContainerAppEnvironments  []ContainerAppEnvironment
	ContainerApps             []ContainerApp
	ContainerGroups           []ContainerGroup
	ServiceBusNamespaces      []ServiceBusNamespace
	EventHubsNamespaces       []EventHubsNamespace
	APIMServices              []APIMService
	FrontDoorProfiles         []FrontDoorProfile
	MetricAlerts              []MetricAlert
	ActionGroups              []ActionGroup
	ManagementGroupEntities   []MGEntity
	BastionHosts              []BastionHost
	TrafficManagerProfiles    []TrafficManagerProfile
	DNSPrivateResolvers       []DNSPrivateResolver
	DNSForwardingRulesets     []DNSForwardingRuleset
	// H1 resources
	VMSSes                  []VMSS
	SQLManagedInstances     []SQLManagedInstance
	LogicWorkflows          []LogicWorkflow
	DataFactories           []DataFactory
	SynapseWorkspaces       []SynapseWorkspace
	CommunicationServices   []CommunicationService
	AutomationAccounts      []AutomationAccount
	ArcMachines             []ArcMachine
	DataCollectionRules     []DataCollectionRule
	DataCollectionEndpoints []DataCollectionEndpoint
	AutoscaleSettings       []AutoscaleSetting
	SQLVirtualMachines      []SQLVirtualMachine
	LogicAPIConnections     []LogicAPIConnection
	EmailServices           []EmailService
	EmailDomains            []EmailDomain
	StreamAnalyticsJobs     []StreamAnalyticsJob
	EventGridSystemTopics   []EventGridSystemTopic
	AppServiceSlots         []AppServiceSlot
	GraphEnvelopes          map[string]*GraphEnvelope
	FlowLogs                []FlowLog
	// Group-scope (RG / subscription / MG) role assignments collected under audit.
	// Resource-scope RAs are stored on the individual GraphEnvelope entries.
	BulkRoleAssignments []ScopedRoleAssignment
}

// azPath resolves the absolute path to the Azure CLI binary once.
// Using the resolved path instead of the bare "az" name prevents
// [CWE-426](https://cwe.mitre.org/data/definitions/426) by pinning the executable location
// at startup rather than relying on PATH at each invocation.
var (
	azOnce sync.Once
	azBin  string
	azErr  error
)

func resolveAZPath() (string, error) {
	azOnce.Do(func() {
		azBin, azErr = exec.LookPath("az")
	})
	return azBin, azErr
}

// discoverSubscriptions queries az account list to find all accessible subscriptions.
// If tenantFilter is non-empty, only subscriptions in that tenant are returned.
func discoverSubscriptions(tenantFilter string) ([]SubscriptionTarget, error) {
	azPath, err := resolveAZPath()
	if err != nil {
		return nil, fmt.Errorf("Azure CLI (az) not found in PATH: %w", err)
	}
	args := []string{"account", "list", "--output", "json", "--all"}
	out, err := exec.Command(azPath, args...).Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("az account list failed: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("az account list: %w", err)
	}

	var accounts []struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		TenantID string `json:"tenantId"`
		State    string `json:"state"`
	}
	if err := json.Unmarshal(out, &accounts); err != nil {
		return nil, fmt.Errorf("parsing account list: %w", err)
	}

	var targets []SubscriptionTarget
	for _, a := range accounts {
		if a.State != "Enabled" {
			continue
		}
		if tenantFilter != "" && a.TenantID != tenantFilter {
			continue
		}
		targets = append(targets, SubscriptionTarget{
			SubscriptionID:   a.ID,
			SubscriptionName: a.Name,
			TenantID:         a.TenantID,
		})
	}

	if len(targets) == 0 {
		if tenantFilter != "" {
			return nil, fmt.Errorf("no enabled subscriptions found in tenant %s", tenantFilter)
		}
		return nil, fmt.Errorf("no enabled subscriptions found (is az login active?)")
	}

	return targets, nil
}

// Client wraps the Azure CLI to collect subscription resources.
type Client struct {
	subscriptionID     string
	logger             *slog.Logger
	mgEntitiesOverride []MGEntity // pre-fetched tenant-level MG entities; nil = fetch on demand.
	purpose            string     // osirismeta purpose: "documentation" (default) or "audit"
}

// NewClient creates a new Azure CLI client for the given subscription.
func NewClient(subscriptionID string, logger *slog.Logger) *Client {
	return &Client{
		subscriptionID: subscriptionID,
		logger:         logger,
	}
}

// Collect fetches all networking resources for the subscription.
func (c *Client) Collect() (*SubscriptionData, error) {
	data := &SubscriptionData{}

	// Subscription info.
	sub, err := c.fetchSubscription()
	if err != nil {
		return nil, fmt.Errorf("fetching subscription info: %w", err)
	}
	data.Subscription = sub

	// Management group hierarchy runs in the background - tenant-scoped,
	// best-effort, independent of subscription resource collection.
	var mgWg sync.WaitGroup
	mgWg.Add(1)
	go func() {
		defer mgWg.Done()
		c.collectManagementGroupEntities(data)
	}()

	// Resource groups.
	if err := c.queryInto("group list", &data.ResourceGroups); err != nil {
		mgWg.Wait()
		return nil, fmt.Errorf("fetching resource groups: %w", err)
	}
	c.logger.Info("collected resource groups", "count", len(data.ResourceGroups))

	// Network resources - each is independent, partial failures are logged and skipped.
	c.collectNetworkResources(data)

	// Universal graph envelope: kind, sku, identity, timestamps, public_network_access.
	// Runs for all purposes - provides the normalised osiris.azure envelope fields.
	c.collectGraphEnvelopes(data)

	// Pass-3 context collectors are expensive (per-resource calls) and only
	// collected for audit-grade runs per OSIRIS JSON spec chapter 13 section 13.1.3.
	if c.purpose == "audit" {
		c.collectResourceTimestamps(data)
		c.collectChildTimestamps(data)
		c.collectResourceLocks(data)
		c.collectRoleAssignments(data)
		c.collectDiagnosticSettings(data)
		c.collectVMExtensions(data)
		// Effective routes per NIC: high-volume (can add 100+ MB to output).
		// Only collected under flag "--purpose audit" Detailed (audit-focused) following OSIRIS JSON spec chapter 13 section 13.1.3.
		c.collectEffectiveRoutes(data.NetworkInterfaces)
	}

	// Wait for background MG collection before returning.
	mgWg.Wait()

	return data, nil
}

// collectNetworkResources fetches all network resource types.
// Partial failures are logged and skipped per the OSIRIS JSON producer contract.
func (c *Client) collectNetworkResources(data *SubscriptionData) {
	type collectable struct {
		name string
		cmd  string
		dest any
	}

	items := []collectable{
		{"network interfaces", "network nic list", &data.NetworkInterfaces},
		{"network security groups", "network nsg list", &data.SecurityGroups},
		{"application security groups", "network asg list", &data.ApplicationSecurityGroups},
		{"public IPs", "network public-ip list", &data.PublicIPs},
		{"public IP prefixes", "network public-ip prefix list", &data.PublicIPPrefixes},
		{"availability sets", "vm availability-set list", &data.AvailabilitySets},
		{"load balancers", "network lb list", &data.LoadBalancers},
		{"private endpoints", "network private-endpoint list", &data.PrivateEndpoints},
		{"private DNS zones", "network private-dns zone list", &data.PrivateDNSZones},
		{"DNS zones", "network dns zone list", &data.DNSZones},
		{"NAT gateways", "network nat gateway list", &data.NATGateways},
		{"ExpressRoute circuits", "network express-route list", &data.ExpressRouteCircuits},
		{"firewalls", "network firewall list", &data.AzureFirewalls},
		{"application gateways", "network application-gateway list", &data.ApplicationGateways},
		{"virtual machines", "vm list", &data.VirtualMachines},
		{"app service plans", "appservice plan list", &data.AppServicePlans},
		{"app service sites", "webapp list", &data.WebApps},
		{"storage accounts", "storage account list", &data.StorageAccounts},
		{"key vaults", "keyvault list", &data.KeyVaults},
		{"container registries", "acr list", &data.ContainerRegistries},
		{"managed identities", "identity list", &data.ManagedIdentities},
		{"disk snapshots", "snapshot list", &data.Snapshots},
		{"application insights components", "resource list --resource-type microsoft.insights/components", &data.ApplicationInsights},
		{"log analytics workspaces", "monitor log-analytics workspace list", &data.LogAnalyticsWorkspaces},
		{"recovery services vaults", "backup vault list", &data.RecoveryServicesVaults},
		{"backup vaults", "dataprotection backup-vault list", &data.BackupVaults},
		{"sql servers", "sql server list", &data.SQLServers},
		{"postgresql flexible servers", "postgres flexible-server list", &data.PostgreSQLServers},
		{"mysql flexible servers", "mysql flexible-server list", &data.MySQLServers},
		{"cosmos db accounts", "cosmosdb list", &data.CosmosAccounts},
		{"redis caches", "redis list", &data.RedisCaches},
		{"aks clusters", "aks list", &data.AKSClusters},
		{"container app environments", "containerapp env list", &data.ContainerAppEnvironments},
		{"container apps", "containerapp list", &data.ContainerApps},
		{"container groups", "container list", &data.ContainerGroups},
		{"service bus namespaces", "servicebus namespace list", &data.ServiceBusNamespaces},
		{"event hubs namespaces", "eventhubs namespace list", &data.EventHubsNamespaces},
		{"api management services", "apim list", &data.APIMServices},
		{"front door profiles", "afd profile list", &data.FrontDoorProfiles},
		{"route servers", "network routeserver list", &data.RouteServers},
		{"metric alert rules", "monitor metrics alert list", &data.MetricAlerts},
		{"action groups", "monitor action-group list", &data.ActionGroups},
		{"bastions", "network bastion list", &data.BastionHosts},
		{"traffic manager profiles", "network traffic-manager profile list", &data.TrafficManagerProfiles},
		{"DNS private resolvers", "resource list --resource-type Microsoft.Network/dnsResolvers", &data.DNSPrivateResolvers},
		{"DNS forwarding rulesets", "resource list --resource-type Microsoft.Network/dnsForwardingRulesets", &data.DNSForwardingRulesets},
		// H1 resources
		{"virtual machine scale sets", "vmss list", &data.VMSSes},
		{"SQL managed instances", "sql mi list", &data.SQLManagedInstances},
		{"logic workflows", "logic workflow list", &data.LogicWorkflows},
		{"data factories", "resource list --resource-type Microsoft.DataFactory/factories", &data.DataFactories},
		{"synapse workspaces", "resource list --resource-type Microsoft.Synapse/workspaces", &data.SynapseWorkspaces},
		{"communication services", "resource list --resource-type Microsoft.Communication/CommunicationServices", &data.CommunicationServices},
		{"automation accounts", "automation account list", &data.AutomationAccounts},
		{"arc machines", "resource list --resource-type Microsoft.HybridCompute/machines", &data.ArcMachines},
		{"data collection rules", "monitor data-collection rule list", &data.DataCollectionRules},
		{"data collection endpoints", "monitor data-collection endpoint list", &data.DataCollectionEndpoints},
		{"autoscale settings", "resource list --resource-type Microsoft.Insights/autoscalesettings", &data.AutoscaleSettings},
		{"sql virtual machines", "sql vm list", &data.SQLVirtualMachines},
		{"logic api connections", "resource list --resource-type Microsoft.Web/connections", &data.LogicAPIConnections},
		{"email services", "resource list --resource-type Microsoft.Communication/EmailServices", &data.EmailServices},
		{"email domains", "resource list --resource-type Microsoft.Communication/EmailServices/Domains", &data.EmailDomains},
		{"stream analytics jobs", "resource list --resource-type Microsoft.StreamAnalytics/streamingjobs", &data.StreamAnalyticsJobs},
		{"event grid system topics", "resource list --resource-type Microsoft.EventGrid/systemTopics", &data.EventGridSystemTopics},
	}

	const baseConcurrency = 8
	baseSem := make(chan struct{}, baseConcurrency)
	var baseWg sync.WaitGroup
	for _, item := range items {
		baseWg.Add(1)
		baseSem <- struct{}{}
		go func(item collectable) {
			defer baseWg.Done()
			defer func() { <-baseSem }()
			if err := c.queryInto(item.cmd, item.dest); err != nil {
				c.logger.Warn("failed to collect resource type, skipping", "type", item.name, "error", err)
				return
			}
			c.logger.Info("collected", "type", item.name, "count", sliceLen(item.dest))
		}(item)
	}
	baseWg.Wait()

	// VNets and route tables: some CLI builds reject the subscription-wide forms
	// (`az network vnet list`, `az network route-table list`) with
	// "--resource-group/-g required", so iterate per RG like collectDisks does.
	// Must run before collectSubnets / collectVNetPeerings which consume VNets.
	data.VirtualNetworks = c.collectVNets(data.ResourceGroups)
	data.RouteTables = c.collectRouteTables(data.ResourceGroups)

	// Backfill minimumTlsVersion / networkAcls.defaultAction for KV and SQL -
	// az keyvault list / az sql server list omit these from their output.
	c.collectKVAndSQLEnrichments(data)

	// Managed disks: some CLI builds / RBAC scopes reject the subscription-wide
	// `az disk list` with "--resource-group/-g required", so iterate per RG.
	data.Disks = c.collectDisks(data.ResourceGroups)

	// SQL databases: one `az sql db list` per server (no subscription-wide list).
	c.collectSQLDatabases(data.SQLServers)

	// SQL elastic pools: one `az sql elastic-pool list` per server.
	c.collectSQLElasticPools(data.SQLServers)

	// SQL MI databases: one `az sql midb list` per managed instance.
	c.collectSQLMIDatabases(data.SQLManagedInstances)

	// ACR replications: one `az acr replication list` per registry.
	c.collectACRReplications(data.ContainerRegistries)

	// Arc extensions: single Resource Graph query across all machines.
	c.collectArcExtensions(data)

	// App Service slots: one `az webapp deployment slot list` per webapp.
	c.collectAppServiceSlots(data)

	// Email domain parent-ID backfill: parse parent email service ARM ID from slot ID.
	for i := range data.EmailDomains {
		d := &data.EmailDomains[i]
		// Email domain ID: .../providers/Microsoft.Communication/emailServices/<svc>/domains/<domain>
		if idx := strings.LastIndex(strings.ToLower(d.ID), "/domains/"); idx >= 0 {
			d.EmailServiceID = d.ID[:idx]
		}
	}

	// Subnets require iterating per VNet (no list-all command).
	data.Subnets = c.collectSubnets(data.VirtualNetworks)

	// VNet peerings require iterating per VNet.
	data.VNetPeerings = c.collectVNetPeerings(data.VirtualNetworks)

	// VNet gateways require iterating per resource group that has networking resources.
	data.VNetGateways = c.collectVNetGateways(data.ResourceGroups)

	// Gateway connections (ExpressRoute + VPN) - requires per-RG iteration.
	data.GatewayConnections = c.collectGatewayConnections(data.ResourceGroups)

	// ExpressRoute peerings (BGP details, per circuit).
	c.collectExpressRoutePeerings(data.ExpressRouteCircuits)

	// Private DNS zone VNet links (requires per-zone iteration).
	c.collectPrivateDNSLinks(data.PrivateDNSZones)

	// NSG flow logs via Resource Graph (single query, avoids per-watcher iteration).
	c.collectFlowLogs(data)

	// Backup protected items per Recovery Services Vault.
	c.collectBackupProtectedItems(data.RecoveryServicesVaults)

	// Backup instances per Backup Vault.
	c.collectBackupInstances(data.BackupVaults)

	// AKS node pools: one `az aks nodepool list` per cluster.
	c.collectAKSNodePools(data.AKSClusters)

	// DNS resolver and ruleset properties are not returned by az resource list
	// for these resource types (resource-provider behavior); fetch individually.
	c.collectDNSResourceDetails(data)
}

// collectDNSResourceDetails fetches full properties for DNS resolvers and forwarding
// rulesets via az resource show --ids. az resource list returns an empty properties
// block for these resource types; az resource show calls the provider GET endpoint
// directly and always includes the full detail.
func (c *Client) collectDNSResourceDetails(data *SubscriptionData) {
	for i := range data.DNSPrivateResolvers {
		r := &data.DNSPrivateResolvers[i]
		if r.Properties != nil {
			continue
		}
		var full DNSPrivateResolver
		if err := c.queryInto("resource show --ids "+r.ID, &full); err != nil {
			c.logger.Debug("failed to fetch DNS resolver details", "id", r.ID, "error", err)
			continue
		}
		r.Properties = full.Properties
	}
	for i := range data.DNSForwardingRulesets {
		rs := &data.DNSForwardingRulesets[i]
		if rs.Properties != nil {
			continue
		}
		var full DNSForwardingRuleset
		if err := c.queryInto("resource show --ids "+rs.ID, &full); err != nil {
			c.logger.Debug("failed to fetch DNS forwarding ruleset details", "id", rs.ID, "error", err)
			continue
		}
		rs.Properties = full.Properties
	}
}

// collectKVAndSQLEnrichments backfills fields that az keyvault list and
// az sql server list omit from their standard output. A single Resource Graph
// query fetches minimumTlsVersion / networkAcls.defaultAction for Key Vaults
// and minimalTlsVersion / publicNetworkAccess for SQL Servers, then merges
// the results only where the existing struct fields are empty.
func (c *Client) collectKVAndSQLEnrichments(data *SubscriptionData) {
	if len(data.KeyVaults) == 0 && len(data.SQLServers) == 0 {
		return
	}
	if err := c.ensureAZExtension("resource-graph"); err != nil {
		c.logger.Debug("resource-graph unavailable; KV/SQL enrichment skipped", "error", err)
		return
	}

	kql := fmt.Sprintf(
		`Resources | where subscriptionId =~ '%s' | where type =~ 'microsoft.keyvault/vaults' or type =~ 'microsoft.sql/servers' | project id, type, tlsVersion = tostring(coalesce(properties.minimumTlsVersion, properties.minimalTlsVersion)), defaultAction = tostring(properties.networkAcls.defaultAction), publicNetworkAccess = tostring(properties.publicNetworkAccess), restrictOutbound = tostring(properties.restrictOutboundNetworkAccess)`,
		c.subscriptionID,
	)

	type enrichItem struct {
		ID                  string `json:"id"`
		Type                string `json:"type"`
		TLSVersion          string `json:"tlsVersion"`
		DefaultAction       string `json:"defaultAction"`
		PublicNetworkAccess string `json:"publicNetworkAccess"`
		RestrictOutbound    string `json:"restrictOutbound"`
	}
	type enrichPage struct {
		Count int          `json:"count"`
		Data  []enrichItem `json:"data"`
	}

	args := []string{"graph", "query", "-q", kql, "--first", "1000"}
	out, err := c.execAZArgsNoSub(args)
	if err != nil {
		c.logger.Debug("KV/SQL enrichment graph query failed", "error", err)
		return
	}
	var page enrichPage
	if err := json.Unmarshal(out, &page); err != nil {
		c.logger.Debug("failed to parse KV/SQL enrichment response", "error", err)
		return
	}

	kvMap := make(map[string]enrichItem, len(data.KeyVaults))
	sqlMap := make(map[string]enrichItem, len(data.SQLServers))
	for _, item := range page.Data {
		lower := strings.ToLower(item.ID)
		if strings.Contains(lower, "/microsoft.keyvault/vaults/") {
			kvMap[lower] = item
		} else if strings.Contains(lower, "/microsoft.sql/servers/") {
			sqlMap[lower] = item
		}
	}

	for i := range data.KeyVaults {
		kv := &data.KeyVaults[i]
		enrich, ok := kvMap[strings.ToLower(kv.ID)]
		if !ok {
			continue
		}
		if kv.Properties == nil {
			kv.Properties = &KeyVaultProperties{}
		}
		if kv.Properties.MinimumTLSVersion == "" && enrich.TLSVersion != "" {
			kv.Properties.MinimumTLSVersion = enrich.TLSVersion
		}
		if kv.Properties.PublicNetworkAccess == "" && enrich.PublicNetworkAccess != "" {
			kv.Properties.PublicNetworkAccess = enrich.PublicNetworkAccess
		}
		if enrich.DefaultAction != "" {
			if kv.Properties.NetworkACLs == nil {
				kv.Properties.NetworkACLs = &azKeyVaultNetworkACLs{}
			}
			if kv.Properties.NetworkACLs.DefaultAction == "" {
				kv.Properties.NetworkACLs.DefaultAction = enrich.DefaultAction
			}
		}
	}

	// Per-vault show fallback: az keyvault list omits minimumTlsVersion and
	// networkAcls.defaultAction; Resource Graph returns "" for vaults that have
	// never explicitly configured these fields (platform defaults aren't stored
	// in ARM). Fetch the full properties for any vault still missing both values.
	c.enrichKVWithShow(data.KeyVaults)

	for i := range data.SQLServers {
		s := &data.SQLServers[i]
		enrich, ok := sqlMap[strings.ToLower(s.ID)]
		if !ok {
			continue
		}
		if s.Properties == nil {
			s.Properties = &azSQLServerProperties{}
		}
		if s.Properties.MinimalTLSVersion == "" && enrich.TLSVersion != "" {
			s.Properties.MinimalTLSVersion = enrich.TLSVersion
		}
		if s.Properties.PublicNetworkAccess == "" && enrich.PublicNetworkAccess != "" {
			s.Properties.PublicNetworkAccess = enrich.PublicNetworkAccess
		}
		if s.Properties.RestrictOutboundNetworkAccess == "" && enrich.RestrictOutbound != "" {
			s.Properties.RestrictOutboundNetworkAccess = enrich.RestrictOutbound
		}
	}
}

// enrichKVWithShow fetches full KV properties via az keyvault show for any vault
// where minimumTlsVersion or networkAcls.defaultAction is still empty after the
// Resource Graph pass. These fields are omitted from az keyvault list and may not
// be present in Resource Graph when they equal the platform default (never stored).
func (c *Client) enrichKVWithShow(vaults []KeyVault) {
	// Identify which vaults need enrichment.
	var targets []int
	for i := range vaults {
		kv := &vaults[i]
		if kv.Properties == nil || kv.Properties.MinimumTLSVersion == "" {
			targets = append(targets, i)
		}
	}
	if len(targets) == 0 {
		return
	}

	const concurrency = 8
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for _, idx := range targets {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			kv := &vaults[i]
			var full KeyVault
			if err := c.queryInto("keyvault show --name "+kv.Name+" -g "+kv.ResourceGroup, &full); err != nil {
				c.logger.Debug("keyvault show failed", "name", kv.Name, "error", err)
				return
			}
			if full.Properties == nil {
				return
			}
			if kv.Properties == nil {
				kv.Properties = &KeyVaultProperties{}
			}
			if kv.Properties.MinimumTLSVersion == "" && full.Properties.MinimumTLSVersion != "" {
				kv.Properties.MinimumTLSVersion = full.Properties.MinimumTLSVersion
			}
			if kv.Properties.NetworkACLs == nil && full.Properties.NetworkACLs != nil {
				kv.Properties.NetworkACLs = full.Properties.NetworkACLs
			} else if full.Properties.NetworkACLs != nil &&
				kv.Properties.NetworkACLs.DefaultAction == "" &&
				full.Properties.NetworkACLs.DefaultAction != "" {
				kv.Properties.NetworkACLs.DefaultAction = full.Properties.NetworkACLs.DefaultAction
			}
		}(idx)
	}
	wg.Wait()
}

// collectAKSNodePools enumerates agent pools per AKS cluster. `az aks nodepool
// list` is the only CLI command that returns pools with their ARM IDs, which
// are needed for the cluster -> nodepool `contains` edge and the nodepool ->
// subnet `network` edge. Missing permissions are logged at debug level.
func (c *Client) collectAKSNodePools(clusters []AKSCluster) {
	const concurrency = 8
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i := range clusters {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			cmd := fmt.Sprintf("aks nodepool list --cluster-name %s --resource-group %s", clusters[i].Name, clusters[i].ResourceGroup)
			var pools []AKSAgentPool
			if err := c.queryInto(cmd, &pools); err != nil {
				c.logger.Debug("failed to list AKS node pools", "cluster", clusters[i].Name, "error", err)
				return
			}
			for j := range pools {
				pools[j].ClusterID = clusters[i].ID
				pools[j].ClusterName = clusters[i].Name
			}
			clusters[i].AgentPools = pools
		}(i)
	}
	wg.Wait()
	total := 0
	for _, cl := range clusters {
		total += len(cl.AgentPools)
	}
	if total > 0 {
		c.logger.Info("collected", "type", "aks node pools", "count", total)
	}
}

// collectBackupProtectedItems fetches backup items for each Recovery Services
// Vault. `az backup item list` requires per-vault iteration; there is no
// subscription-wide equivalent. Missing permissions or empty vaults are logged
// at debug level and skipped.
func (c *Client) collectBackupProtectedItems(vaults []RecoveryServicesVault) {
	const concurrency = 8
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i := range vaults {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			cmd := fmt.Sprintf("backup item list --vault-name %s --resource-group %s", vaults[i].Name, vaults[i].ResourceGroup)
			var items []BackupProtectedItem
			if err := c.queryInto(cmd, &items); err != nil {
				c.logger.Debug("no backup items for RS vault", "vault", vaults[i].Name, "error", err)
				return
			}
			vaults[i].ProtectedItems = items
		}(i)
	}
	wg.Wait()
	total := 0
	for _, v := range vaults {
		total += len(v.ProtectedItems)
	}
	if total > 0 {
		c.logger.Info("collected", "type", "backup protected items", "count", total)
	}
}

// collectBackupInstances fetches backup instances for each Backup Vault.
// `az dataprotection backup-instance list` requires per-vault iteration.
func (c *Client) collectBackupInstances(vaults []BackupVault) {
	const concurrency = 8
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i := range vaults {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			cmd := fmt.Sprintf("dataprotection backup-instance list --vault-name %s --resource-group %s", vaults[i].Name, vaults[i].ResourceGroup)
			var insts []BackupInstance
			if err := c.queryInto(cmd, &insts); err != nil {
				c.logger.Debug("no backup instances for Backup Vault", "vault", vaults[i].Name, "error", err)
				return
			}
			vaults[i].ProtectedInstances = insts
		}(i)
	}
	wg.Wait()
	total := 0
	for _, v := range vaults {
		total += len(v.ProtectedInstances)
	}
	if total > 0 {
		c.logger.Info("collected", "type", "backup instances", "count", total)
	}
}

// collectSubnets fetches subnets for all VNets (az has no global subnet list command).
func (c *Client) collectSubnets(vnets []VirtualNetwork) []Subnet {
	const concurrency = 8
	sem := make(chan struct{}, concurrency)
	var mu sync.Mutex
	var wg sync.WaitGroup
	var all []Subnet

	for _, vnet := range vnets {
		wg.Add(1)
		sem <- struct{}{}
		go func(vnet VirtualNetwork) {
			defer wg.Done()
			defer func() { <-sem }()
			cmd := fmt.Sprintf("network vnet subnet list --vnet-name %s --resource-group %s", vnet.Name, vnet.ResourceGroup)
			var subnets []Subnet
			if err := c.queryInto(cmd, &subnets); err != nil {
				c.logger.Warn("failed to collect subnets, skipping", "vnet", vnet.Name, "error", err)
				return
			}
			for i := range subnets {
				if subnets[i].ResourceGroup == "" {
					subnets[i].ResourceGroup = vnet.ResourceGroup
				}
			}
			mu.Lock()
			all = append(all, subnets...)
			mu.Unlock()
		}(vnet)
	}
	wg.Wait()
	if len(all) > 0 {
		c.logger.Info("collected", "type", "subnets", "count", len(all))
	}
	return all
}

// collectVNetPeerings fetches peerings for all VNets.
func (c *Client) collectVNetPeerings(vnets []VirtualNetwork) []VNetPeering {
	const concurrency = 8
	sem := make(chan struct{}, concurrency)
	var mu sync.Mutex
	var wg sync.WaitGroup
	var all []VNetPeering

	for _, vnet := range vnets {
		wg.Add(1)
		sem <- struct{}{}
		go func(vnet VirtualNetwork) {
			defer wg.Done()
			defer func() { <-sem }()
			cmd := fmt.Sprintf("network vnet peering list --vnet-name %s --resource-group %s", vnet.Name, vnet.ResourceGroup)
			var peerings []VNetPeering
			if err := c.queryInto(cmd, &peerings); err != nil {
				c.logger.Warn("failed to collect VNet peerings, skipping", "vnet", vnet.Name, "error", err)
				return
			}
			mu.Lock()
			all = append(all, peerings...)
			mu.Unlock()
		}(vnet)
	}
	wg.Wait()
	if len(all) > 0 {
		c.logger.Info("collected", "type", "VNet peerings", "count", len(all))
	}
	return all
}

// collectVNetGateways fetches VNet gateways per resource group.
func (c *Client) collectVNetGateways(rgs []ResourceGroup) []VNetGateway {
	const concurrency = 8
	sem := make(chan struct{}, concurrency)
	var mu sync.Mutex
	var wg sync.WaitGroup
	var all []VNetGateway

	for _, rg := range rgs {
		wg.Add(1)
		sem <- struct{}{}
		go func(rg ResourceGroup) {
			defer wg.Done()
			defer func() { <-sem }()
			cmd := fmt.Sprintf("network vnet-gateway list --resource-group %s", rg.Name)
			var gateways []VNetGateway
			if err := c.queryInto(cmd, &gateways); err != nil {
				c.logger.Debug("no VNet gateways in resource group", "rg", rg.Name)
				return
			}
			mu.Lock()
			all = append(all, gateways...)
			mu.Unlock()
		}(rg)
	}
	wg.Wait()
	if len(all) > 0 {
		c.logger.Info("collected", "type", "VNet gateways", "count", len(all))
	}
	return all
}

// collectDisks fetches managed disks per resource group. Some Azure CLI
// configurations reject the subscription-wide `az disk list` form with
// "--resource-group/-g is required", so we iterate RGs to be safe.
func (c *Client) collectDisks(rgs []ResourceGroup) []Disk {
	const concurrency = 8
	sem := make(chan struct{}, concurrency)
	var mu sync.Mutex
	var wg sync.WaitGroup
	var all []Disk

	for _, rg := range rgs {
		wg.Add(1)
		sem <- struct{}{}
		go func(rg ResourceGroup) {
			defer wg.Done()
			defer func() { <-sem }()
			cmd := fmt.Sprintf("disk list --resource-group %s", rg.Name)
			var disks []Disk
			if err := c.queryInto(cmd, &disks); err != nil {
				c.logger.Debug("no managed disks in resource group", "rg", rg.Name)
				return
			}
			mu.Lock()
			all = append(all, disks...)
			mu.Unlock()
		}(rg)
	}
	wg.Wait()
	c.logger.Info("collected", "type", "managed disks", "count", len(all))
	return all
}

func (c *Client) collectVNets(rgs []ResourceGroup) []VirtualNetwork {
	const concurrency = 8
	sem := make(chan struct{}, concurrency)
	var mu sync.Mutex
	var wg sync.WaitGroup
	var all []VirtualNetwork

	for _, rg := range rgs {
		wg.Add(1)
		sem <- struct{}{}
		go func(rg ResourceGroup) {
			defer wg.Done()
			defer func() { <-sem }()
			cmd := fmt.Sprintf("network vnet list --resource-group %s", rg.Name)
			var vnets []VirtualNetwork
			if err := c.queryInto(cmd, &vnets); err != nil {
				c.logger.Debug("no virtual networks in resource group", "rg", rg.Name)
				return
			}
			mu.Lock()
			all = append(all, vnets...)
			mu.Unlock()
		}(rg)
	}
	wg.Wait()
	c.logger.Info("collected", "type", "virtual networks", "count", len(all))
	return all
}

func (c *Client) collectRouteTables(rgs []ResourceGroup) []RouteTable {
	const concurrency = 8
	sem := make(chan struct{}, concurrency)
	var mu sync.Mutex
	var wg sync.WaitGroup
	var all []RouteTable

	for _, rg := range rgs {
		wg.Add(1)
		sem <- struct{}{}
		go func(rg ResourceGroup) {
			defer wg.Done()
			defer func() { <-sem }()
			cmd := fmt.Sprintf("network route-table list --resource-group %s", rg.Name)
			var rts []RouteTable
			if err := c.queryInto(cmd, &rts); err != nil {
				c.logger.Debug("no route tables in resource group", "rg", rg.Name)
				return
			}
			mu.Lock()
			all = append(all, rts...)
			mu.Unlock()
		}(rg)
	}
	wg.Wait()
	c.logger.Info("collected", "type", "route tables", "count", len(all))
	return all
}

// collectSQLDatabases enumerates databases per SQL server. Each database gets
// its parent server's ARM ID stamped into ServerID so transforms can wire the
// server -> database `contains` edge without reparsing the db ID path.
// The implicit `master` system database returned by `az sql db list` is
// skipped - it is not a topology-relevant workload database.
func (c *Client) collectSQLDatabases(servers []SQLServer) {
	const concurrency = 8
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i := range servers {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			cmd := fmt.Sprintf("sql db list --resource-group %s --server %s", servers[i].ResourceGroup, servers[i].Name)
			var dbs []SQLDatabase
			if err := c.queryInto(cmd, &dbs); err != nil {
				c.logger.Debug("failed to list SQL databases", "server", servers[i].Name, "error", err)
				return
			}
			for j := range dbs {
				if strings.EqualFold(dbs[j].Name, "master") {
					continue
				}
				dbs[j].ServerID = servers[i].ID
				servers[i].Databases = append(servers[i].Databases, dbs[j])
			}
		}(i)
	}
	wg.Wait()
	total := 0
	for _, s := range servers {
		total += len(s.Databases)
	}
	if total > 0 {
		c.logger.Info("collected", "type", "sql databases", "count", total)
	}
}

// collectSQLMIDatabases fetches databases for each SQL Managed Instance.
func (c *Client) collectSQLMIDatabases(instances []SQLManagedInstance) {
	const concurrency = 8
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i := range instances {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			cmd := fmt.Sprintf("sql midb list --managed-instance %s --resource-group %s", instances[i].Name, instances[i].ResourceGroup)
			var dbs []SQLMIDatabase
			if err := c.queryInto(cmd, &dbs); err != nil {
				c.logger.Debug("failed to list SQL MI databases", "instance", instances[i].Name, "error", err)
				return
			}
			for j := range dbs {
				dbs[j].ManagedInstanceID = instances[i].ID
			}
			instances[i].Databases = dbs
		}(i)
	}
	wg.Wait()
	total := 0
	for _, mi := range instances {
		total += len(mi.Databases)
	}
	if total > 0 {
		c.logger.Info("collected", "type", "sql mi databases", "count", total)
	}
}

// collectSQLElasticPools fetches elastic pools for each SQL server.
func (c *Client) collectSQLElasticPools(servers []SQLServer) {
	const concurrency = 8
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i := range servers {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			cmd := fmt.Sprintf("sql elastic-pool list --server %s --resource-group %s", servers[i].Name, servers[i].ResourceGroup)
			var pools []SQLElasticPool
			if err := c.queryInto(cmd, &pools); err != nil {
				c.logger.Debug("failed to list SQL elastic pools", "server", servers[i].Name, "error", err)
				return
			}
			for j := range pools {
				pools[j].ServerID = servers[i].ID
			}
			servers[i].ElasticPools = pools
		}(i)
	}
	wg.Wait()
	total := 0
	for _, s := range servers {
		total += len(s.ElasticPools)
	}
	if total > 0 {
		c.logger.Info("collected", "type", "sql elastic pools", "count", total)
	}
}

// collectACRReplications fetches geo-replications for each Container Registry.
func (c *Client) collectACRReplications(registries []ContainerRegistry) {
	const concurrency = 8
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i := range registries {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			cmd := fmt.Sprintf("acr replication list --name %s --resource-group %s", registries[i].Name, registries[i].ResourceGroup)
			var repls []ACRReplication
			if err := c.queryInto(cmd, &repls); err != nil {
				c.logger.Debug("failed to list ACR replications", "registry", registries[i].Name, "error", err)
				return
			}
			// Exclude the home region (provisioned by default, same location as registry).
			var nonHome []ACRReplication
			registryLoc := strings.ToLower(strings.ReplaceAll(registries[i].Location, " ", ""))
			for _, r := range repls {
				replLoc := strings.ToLower(strings.ReplaceAll(r.Location, " ", ""))
				if replLoc != registryLoc {
					nonHome = append(nonHome, r)
				}
			}
			registries[i].Replications = nonHome
		}(i)
	}
	wg.Wait()
	total := 0
	for _, r := range registries {
		total += len(r.Replications)
	}
	if total > 0 {
		c.logger.Info("collected", "type", "acr replications", "count", total)
	}
}

// collectArcExtensions fetches extensions for each Arc-enabled machine via Resource Graph.
func (c *Client) collectArcExtensions(data *SubscriptionData) {
	if len(data.ArcMachines) == 0 {
		return
	}
	if err := c.ensureAZExtension("resource-graph"); err != nil {
		c.logger.Warn("resource-graph not available; skipping arc extensions", "error", err)
		return
	}

	// Note: autoUpgradeMinorVersion is intentionally excluded - ARM / Resource Graph
	// returns it as 0/1 (number) which Go's json decoder rejects for bool fields.
	kql := fmt.Sprintf(
		`Resources | where subscriptionId =~ '%s' and type =~ 'microsoft.hybridcompute/machines/extensions' | project id, name, resourceGroup, publisher = tostring(properties.publisher), extType = tostring(properties.type), typeHandlerVersion = tostring(properties.typeHandlerVersion), provisioningState = tostring(properties.provisioningState)`,
		c.subscriptionID,
	)

	type arcExtItem struct {
		ID                 string `json:"id"`
		Name               string `json:"name"`
		ResourceGroup      string `json:"resourceGroup"`
		Publisher          string `json:"publisher"`
		ExtType            string `json:"extType"`
		TypeHandlerVersion string `json:"typeHandlerVersion"`
		ProvisioningState  string `json:"provisioningState"`
	}
	type graphPage struct {
		Data []arcExtItem `json:"data"`
	}

	args := []string{"graph", "query", "-q", kql, "--first", "1000"}
	out, err := c.execAZArgsNoSub(args)
	if err != nil {
		c.logger.Warn("failed to collect arc extensions", "error", err)
		return
	}

	var page graphPage
	if err := json.Unmarshal(out, &page); err != nil {
		c.logger.Warn("failed to parse arc extension response", "error", err)
		return
	}

	// Build machine ID -> index map for fast lookup.
	machineIdx := make(map[string]int, len(data.ArcMachines))
	for i, m := range data.ArcMachines {
		machineIdx[strings.ToLower(m.ID)] = i
	}

	for _, item := range page.Data {
		// Extension ID format: .../providers/Microsoft.HybridCompute/machines/{name}/extensions/{ext}
		idx := strings.LastIndex(strings.ToLower(item.ID), "/extensions/")
		if idx < 0 {
			continue
		}
		machineArmID := item.ID[:idx]
		i, ok := machineIdx[strings.ToLower(machineArmID)]
		if !ok {
			continue
		}
		data.ArcMachines[i].Extensions = append(data.ArcMachines[i].Extensions, ArcExtension{
			ID:                 item.ID,
			Name:               item.Name,
			Type:               item.ExtType,
			Publisher:          item.Publisher,
			TypeHandlerVersion: item.TypeHandlerVersion,
			ProvisioningState:  item.ProvisioningState,
		})
	}

	total := 0
	for _, m := range data.ArcMachines {
		total += len(m.Extensions)
	}
	if total > 0 {
		c.logger.Info("collected", "type", "arc machine extensions", "count", total)
	}
}

// collectAppServiceSlots fetches deployment slots for each App Service / Function App.
// az webapp deployment slot list requires --name and --resource-group.
func (c *Client) collectAppServiceSlots(data *SubscriptionData) {
	if len(data.WebApps) == 0 {
		return
	}
	const concurrency = 8
	sem := make(chan struct{}, concurrency)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, app := range data.WebApps {
		wg.Add(1)
		sem <- struct{}{}
		go func(app WebApp) {
			defer wg.Done()
			defer func() { <-sem }()
			var slots []AppServiceSlot
			cmd := fmt.Sprintf("webapp deployment slot list -n %s -g %s", app.Name, app.ResourceGroup)
			if err := c.queryInto(cmd, &slots); err != nil || len(slots) == 0 {
				return
			}
			for i := range slots {
				// Derive parent webapp ARM ID from slot ID:
				// <webapp-id>/slots/<slot-name>
				if idx := strings.LastIndex(strings.ToLower(slots[i].ID), "/slots/"); idx >= 0 {
					slots[i].WebAppID = slots[i].ID[:idx]
				}
			}
			mu.Lock()
			data.AppServiceSlots = append(data.AppServiceSlots, slots...)
			mu.Unlock()
		}(app)
	}
	wg.Wait()
	if len(data.AppServiceSlots) > 0 {
		c.logger.Info("collected", "type", "app service slots", "count", len(data.AppServiceSlots))
	}
}

// collectGatewayConnections fetches gateway connections (ExpressRoute + VPN) per resource group.
// az network vpn-connection list requires --resource-group to return results.
func (c *Client) collectGatewayConnections(rgs []ResourceGroup) []GatewayConnection {
	const concurrency = 8
	sem := make(chan struct{}, concurrency)
	var mu sync.Mutex
	var wg sync.WaitGroup
	var all []GatewayConnection

	for _, rg := range rgs {
		wg.Add(1)
		sem <- struct{}{}
		go func(rg ResourceGroup) {
			defer wg.Done()
			defer func() { <-sem }()
			cmd := fmt.Sprintf("network vpn-connection list --resource-group %s", rg.Name)
			var conns []GatewayConnection
			if err := c.queryInto(cmd, &conns); err != nil {
				c.logger.Debug("no gateway connections in resource group", "rg", rg.Name)
				return
			}
			mu.Lock()
			all = append(all, conns...)
			mu.Unlock()
		}(rg)
	}
	wg.Wait()
	if len(all) > 0 {
		c.logger.Info("collected", "type", "gateway connections", "count", len(all))
	}
	return all
}

// collectExpressRoutePeerings fetches peerings for each ExpressRoute circuit.
func (c *Client) collectExpressRoutePeerings(circuits []ExpressRouteCircuit) {
	const concurrency = 8
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i := range circuits {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			cmd := fmt.Sprintf("network express-route peering list --circuit-name %s --resource-group %s",
				circuits[i].Name, circuits[i].ResourceGroup)
			var peerings []ExpressRoutePeering
			if err := c.queryInto(cmd, &peerings); err != nil {
				c.logger.Debug("no peerings for ExpressRoute circuit", "circuit", circuits[i].Name, "error", err)
				return
			}
			circuits[i].Peerings = peerings
		}(i)
	}
	wg.Wait()
	total := 0
	for _, circuit := range circuits {
		total += len(circuit.Peerings)
	}
	if total > 0 {
		c.logger.Info("collected", "type", "ExpressRoute peerings", "count", total)
	}
}

// fetchSubscription gets the subscription metadata.
func (c *Client) fetchSubscription() (SubscriptionInfo, error) {
	out, err := c.execAZ("account show")
	if err != nil {
		return SubscriptionInfo{}, err
	}

	var sub SubscriptionInfo
	if err := json.Unmarshal(out, &sub); err != nil {
		return SubscriptionInfo{}, fmt.Errorf("parsing subscription info: %w", err)
	}
	return sub, nil
}

// queryInto executes an az command and unmarshals the JSON array result into dest.
func (c *Client) queryInto(command string, dest any) error {
	out, err := c.execAZ(command)
	if err != nil {
		return err
	}
	return json.Unmarshal(out, dest)
}

// azCommandTimeout is the per-call deadline for all az CLI subprocesses.
// Prevents any single hung call from blocking collection indefinitely.
const azCommandTimeout = 120 * time.Second

// execAZ runs an Azure CLI command and returns the raw JSON output.
// The az binary path is resolved once via exec.LookPath to avoid
// untrusted search path issues [CWE-426](https://cwe.mitre.org/data/definitions/426).
// Each invocation carries a 120-second deadline; a timeout is surfaced as an error.
func (c *Client) execAZ(command string) ([]byte, error) {
	azPath, err := resolveAZPath()
	if err != nil {
		return nil, fmt.Errorf("Azure CLI (az) not found in PATH: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), azCommandTimeout)
	defer cancel()

	args := strings.Fields(command)
	args = append(args, "--subscription", c.subscriptionID, "--output", "json")

	fullArgs := append([]string{azPath}, args...)
	c.logger.Debug("executing Azure CLI", "command", strings.Join(fullArgs, " "))

	cmd := exec.CommandContext(ctx, azPath, args...)
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("az %s timed out after %v", command, azCommandTimeout)
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("az %s failed: %s", command, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("az %s: %w", command, err)
	}
	return out, nil
}

// execAZTenant runs an Azure CLI command without a --subscription flag.
// Used for tenant-scoped calls (e.g. management group entities list) where
// the subscription context is irrelevant or actively rejected by the CLI.
func (c *Client) execAZTenant(command string) ([]byte, error) {
	azPath, err := resolveAZPath()
	if err != nil {
		return nil, fmt.Errorf("Azure CLI (az) not found in PATH: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), azCommandTimeout)
	defer cancel()

	args := strings.Fields(command)
	args = append(args, "--output", "json")
	c.logger.Debug("executing Azure CLI (tenant scope)", "command", strings.Join(append([]string{azPath}, args...), " "))
	cmd := exec.CommandContext(ctx, azPath, args...)
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("az %s timed out after %v", command, azCommandTimeout)
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("az %s failed: %s", command, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("az %s: %w", command, err)
	}
	return out, nil
}

// queryTenant executes a tenant-scoped az command and unmarshals the result into dest.
func (c *Client) queryTenant(command string, dest any) error {
	out, err := c.execAZTenant(command)
	if err != nil {
		return err
	}
	return json.Unmarshal(out, dest)
}

// execAZArgs runs an Azure CLI command with a pre-split args slice and returns raw
// JSON output. Unlike execAZ, args are passed directly without strings.Fields splitting,
// which is required when arguments contain spaces or special characters (e.g. KQL query
// strings, skip tokens). Appends --subscription and --output json automatically.
func (c *Client) execAZArgs(args []string) ([]byte, error) {
	azPath, err := resolveAZPath()
	if err != nil {
		return nil, fmt.Errorf("Azure CLI (az) not found in PATH: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), azCommandTimeout)
	defer cancel()

	all := make([]string, len(args), len(args)+4)
	copy(all, args)
	all = append(all, "--subscription", c.subscriptionID, "--output", "json")

	c.logger.Debug("executing Azure CLI", "command", strings.Join(append([]string{azPath}, all...), " "))
	cmd := exec.CommandContext(ctx, azPath, all...)
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("az %s timed out after %v", args[0], azCommandTimeout)
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("az %s failed: %s", args[0], strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("az %s: %w", args[0], err)
	}
	return out, nil
}

// execAZArgsNoSub runs an Azure CLI command with a pre-split args slice and returns
// raw JSON output. No --subscription flag is appended; only --output json is added.
// Used for commands like az graph query where the subscription is filtered via KQL
// or the --subscriptions parameter rather than the global --subscription flag.
func (c *Client) execAZArgsNoSub(args []string) ([]byte, error) {
	azPath, err := resolveAZPath()
	if err != nil {
		return nil, fmt.Errorf("Azure CLI (az) not found in PATH: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), azCommandTimeout)
	defer cancel()

	all := make([]string, len(args), len(args)+2)
	copy(all, args)
	all = append(all, "--output", "json")

	c.logger.Debug("executing Azure CLI (no-sub)", "command", strings.Join(append([]string{azPath}, all...), " "))
	cmd := exec.CommandContext(ctx, azPath, all...)
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("az %s timed out after %v", args[0], azCommandTimeout)
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("az %s failed: %s", args[0], strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("az %s: %w", args[0], err)
	}
	return out, nil
}

// ensureAZExtension runs `az extension add --name <name> --upgrade` to ensure the
// named extension is installed before it is used. Fast when already installed.
func (c *Client) ensureAZExtension(name string) error {
	return installAZExtension(name)
}

// installAZExtension is the package-level implementation shared by ensureAZExtension
// and RunPreflightChecks.
func installAZExtension(name string) error {
	azPath, err := resolveAZPath()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, azPath, "extension", "add", "--name", name, "--upgrade")
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("az extension add %s timed out", name)
		}
		return fmt.Errorf("az extension add %s: %s", name, strings.TrimSpace(string(out)))
	}
	return nil
}

// RunPreflightChecks verifies the az CLI is available and ensures required extensions
// are installed. Called once at startup in Run(), before any subscription discovery
// or interactive prompts appear.
func RunPreflightChecks(logger *slog.Logger) error {
	azPath, err := resolveAZPath()
	if err != nil {
		return fmt.Errorf("Azure CLI (az) not found in PATH - install from https://learn.microsoft.com/en-us/cli/azure/install-azure-cli?view=azure-cli-latest")
	}
	logger.Info("preflight: az CLI found", "path", azPath)

	logger.Info("preflight: ensuring resource-graph extension...")
	if err := installAZExtension("resource-graph"); err != nil {
		logger.Warn("preflight: resource-graph extension unavailable (graph envelopes will be skipped)", "error", err)
	} else {
		logger.Info("preflight: resource-graph extension ready")
	}

	return nil
}

// collectGraphEnvelopes fetches per-resource metadata for all resources in the subscription
// via az graph query (Azure Resource Graph). The query is paginated using skip_token.
// Results are keyed by lowercase ARM resource ID. The resource-graph extension is
// installed automatically if not already present.
func (c *Client) collectGraphEnvelopes(data *SubscriptionData) {
	if err := c.ensureAZExtension("resource-graph"); err != nil {
		c.logger.Warn("failed to ensure resource-graph extension; skipping graph envelopes", "error", err)
		return
	}

	// Note: 'etag' is not a projectable column in all Resource Graph tenants/versions;
	// it is intentionally omitted to avoid query failure.
	// createdTime/changedTime are NOT projected here; properties.createdTime is a
	// resource-specific field, not the ARM metadata timestamp. Those are fetched
	// reliably via collectResourceTimestamps ($expand on the ARM list endpoint).
	kql := fmt.Sprintf(
		`Resources | where subscriptionId =~ '%s' | project id, kind, sku, identity, managedBy, plan, systemData, provisioningState = tostring(properties.provisioningState), publicNetworkAccess = tostring(properties.publicNetworkAccess) | order by id asc`,
		c.subscriptionID,
	)

	type graphItem struct {
		ID                  string             `json:"id"`
		Kind                string             `json:"kind"`
		SKU                 *azGraphSKU        `json:"sku"`
		Identity            *azGraphIdentity   `json:"identity"`
		ManagedBy           string             `json:"managedBy"`
		Plan                *azGraphPlan       `json:"plan"`
		ProvisioningState   string             `json:"provisioningState"`
		SystemData          *azGraphSystemData `json:"systemData"`
		PublicNetworkAccess string             `json:"publicNetworkAccess"`
	}
	type graphPage struct {
		Count     int         `json:"count"`
		Data      []graphItem `json:"data"`
		SkipToken string      `json:"skip_token"`
	}

	envelopes := make(map[string]*GraphEnvelope)
	skipToken := ""

	for {
		args := []string{"graph", "query", "-q", kql, "--first", "1000"}
		if skipToken != "" {
			args = append(args, "--skip-token", skipToken)
		}

		out, err := c.execAZArgsNoSub(args)
		if err != nil {
			c.logger.Warn("failed to collect graph envelopes", "error", err)
			break
		}

		var page graphPage
		if err := json.Unmarshal(out, &page); err != nil {
			c.logger.Warn("failed to parse graph envelope response", "error", err)
			break
		}

		for _, item := range page.Data {
			key := strings.ToLower(item.ID)
			env := &GraphEnvelope{
				armID:               item.ID,
				Kind:                item.Kind,
				SKU:                 item.SKU,
				Identity:            item.Identity,
				ManagedBy:           item.ManagedBy,
				Plan:                item.Plan,
				ProvisioningState:   item.ProvisioningState,
				SystemData:          item.SystemData,
				PublicNetworkAccess: item.PublicNetworkAccess,
			}
			envelopes[key] = env
		}

		if page.SkipToken == "" {
			break
		}
		skipToken = page.SkipToken
	}

	data.GraphEnvelopes = envelopes
	c.logger.Info("collected graph envelopes", "count", len(envelopes))
}

// collectResourceTimestamps fetches createdTime, changedTime, and provisioningState
// for all subscription resources via the ARM list endpoint with $expand. This is
// the only reliable source for ARM metadata timestamps (Resource Graph's
// properties.createdTime is a resource-specific field, not the metadata timestamp).
// Results are merged into existing GraphEnvelope entries; new entries are created
// for resources not already in the map. Runs under audit purpose only since
// created_at/changed_at are audit-gated.
func (c *Client) collectResourceTimestamps(data *SubscriptionData) {
	if len(data.GraphEnvelopes) == 0 {
		return
	}

	type listItem struct {
		ID                string `json:"id"`
		Etag              string `json:"etag"`
		CreatedTime       string `json:"createdTime"`
		ChangedTime       string `json:"changedTime"`
		ProvisioningState string `json:"provisioningState"`
	}
	type listPage struct {
		Value    []listItem `json:"value"`
		NextLink string     `json:"nextLink"`
	}

	url := fmt.Sprintf(
		"/subscriptions/%s/resources?api-version=2021-04-01&$expand=createdTime,changedTime,provisioningState",
		c.subscriptionID,
	)

	total := 0
	for {
		out, err := c.execAZArgsNoSub([]string{"rest", "--method", "GET", "--url", url})
		if err != nil {
			c.logger.Warn("failed to collect resource timestamps", "error", err)
			return
		}

		var page listPage
		if err := json.Unmarshal(out, &page); err != nil {
			c.logger.Warn("failed to parse resource timestamps response", "error", err)
			return
		}

		for _, item := range page.Value {
			key := strings.ToLower(item.ID)
			env := data.GraphEnvelopes[key]
			if env == nil {
				env = &GraphEnvelope{armID: item.ID}
				data.GraphEnvelopes[key] = env
			}
			if item.Etag != "" && env.Etag == "" {
				env.Etag = item.Etag
			}
			if item.CreatedTime != "" {
				env.CreatedTime = item.CreatedTime
			}
			if item.ChangedTime != "" {
				env.ChangedTime = item.ChangedTime
			}
			if item.ProvisioningState != "" && env.ProvisioningState == "" {
				env.ProvisioningState = item.ProvisioningState
			}
			total++
		}

		if page.NextLink == "" {
			break
		}
		url = page.NextLink
	}

	c.logger.Info("collected resource timestamps", "count", total)
}

// collectChildTimestamps fetches createdTime / changedTime for child resources
// (SQL databases, AKS node pools) that are absent from the ARM subscription list
// endpoint and therefore skipped by collectResourceTimestamps. One REST call is
// made per parent (server / cluster), keeping overhead O(parents) not O(children).
// Runs under audit purpose only (caller enforces this).
func (c *Client) collectChildTimestamps(data *SubscriptionData) {
	if len(data.GraphEnvelopes) == 0 {
		return
	}

	type tsItem struct {
		ID          string `json:"id"`
		CreatedTime string `json:"createdTime"`
		ChangedTime string `json:"changedTime"`
	}
	type tsPage struct {
		Value []tsItem `json:"value"`
	}

	mergeTS := func(items []tsItem) {
		for _, item := range items {
			key := strings.ToLower(item.ID)
			env := data.GraphEnvelopes[key]
			if env == nil {
				env = &GraphEnvelope{armID: item.ID}
				data.GraphEnvelopes[key] = env
			}
			if env.CreatedTime != "" && env.ChangedTime != "" {
				continue
			}
			if item.CreatedTime != "" {
				env.CreatedTime = item.CreatedTime
			}
			if item.ChangedTime != "" {
				env.ChangedTime = item.ChangedTime
			}
		}
	}

	// SQL databases - child resources of SQL servers.
	for _, s := range data.SQLServers {
		if len(s.Databases) == 0 {
			continue
		}
		url := fmt.Sprintf(
			"/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Sql/servers/%s/databases?api-version=2023-08-01&$expand=createdTime,changedTime,provisioningState",
			c.subscriptionID, s.ResourceGroup, s.Name,
		)
		out, err := c.execAZArgsNoSub([]string{"rest", "--method", "GET", "--url", url})
		if err != nil {
			c.logger.Warn("failed to collect SQL database timestamps", "server", s.Name, "error", err)
			continue
		}
		var page tsPage
		if err := json.Unmarshal(out, &page); err != nil {
			c.logger.Warn("failed to parse SQL database timestamps", "server", s.Name, "error", err)
			continue
		}
		mergeTS(page.Value)
	}

	// AKS node pools - child resources of managed clusters.
	for _, cl := range data.AKSClusters {
		if len(cl.AgentPools) == 0 {
			continue
		}
		url := fmt.Sprintf(
			"/subscriptions/%s/resourceGroups/%s/providers/Microsoft.ContainerService/managedClusters/%s/agentPools?api-version=2024-09-01&$expand=createdTime,changedTime,provisioningState",
			c.subscriptionID, cl.ResourceGroup, cl.Name,
		)
		out, err := c.execAZArgsNoSub([]string{"rest", "--method", "GET", "--url", url})
		if err != nil {
			c.logger.Warn("failed to collect AKS node pool timestamps", "cluster", cl.Name, "error", err)
			continue
		}
		var page tsPage
		if err := json.Unmarshal(out, &page); err != nil {
			c.logger.Warn("failed to parse AKS node pool timestamps", "cluster", cl.Name, "error", err)
			continue
		}
		mergeTS(page.Value)
	}

	c.logger.Info("collected child resource timestamps", "sql_servers", len(data.SQLServers), "aks_clusters", len(data.AKSClusters))
}

// collectResourceLocks fetches all management locks for the subscription and attaches
// them to matching graph envelope entries. The scope field in the lock response is the
// ARM ID of the locked resource. Non-matching scopes (e.g. subscription/RG-level locks
// without a resource entry in graph envelopes) are silently skipped.
func (c *Client) collectResourceLocks(data *SubscriptionData) {
	if len(data.GraphEnvelopes) == 0 {
		return
	}

	var rawLocks []struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Level string `json:"level"`
		Notes string `json:"notes"`
	}
	if err := c.queryInto("lock list", &rawLocks); err != nil {
		c.logger.Warn("failed to collect resource locks", "error", err)
		return
	}
	attached := 0
	for _, l := range rawLocks {
		// The lock id is the locked resource path + /providers/Microsoft.Authorization/locks/<name>.
		// Strip the suffix to recover the locked resource ARM ID.
		scope := lockScopeFromID(l.ID)
		if scope == "" {
			continue
		}
		env, ok := data.GraphEnvelopes[strings.ToLower(scope)]
		if !ok {
			continue
		}
		env.Locks = append(env.Locks, ResourceLock{
			Name:  l.Name,
			Level: l.Level,
			Notes: l.Notes,
		})
		attached++
	}
	if len(rawLocks) > 0 {
		c.logger.Info("collected resource locks", "total", len(rawLocks), "attached_to_resources", attached)
	}
}

// lockScopeFromID extracts the locked resource ARM ID from a management lock's own ARM ID.
// Lock IDs follow the pattern: <resource-path>/providers/Microsoft.Authorization/locks/<name>.
// Returns empty string when the suffix cannot be found (subscription/RG level locks are skipped).
func lockScopeFromID(lockID string) string {
	const suffix = "/providers/microsoft.authorization/locks/"
	idx := strings.LastIndex(strings.ToLower(lockID), suffix)
	if idx < 0 {
		return ""
	}
	scope := lockID[:idx]
	// Skip subscription-level and RG-level scopes they have no /providers/ segment
	// and won't match any resource in the graph envelope map.
	if !strings.Contains(strings.ToLower(scope), "/providers/") {
		return ""
	}
	return scope
}

// collectRoleAssignments fetches all RBAC role assignments in the subscription.
// Resource-scoped RAs (scope has a /providers/ resource path) are attached to the
// matching GraphEnvelope. Group-scoped RAs (subscription, RG, MG scope) are stored in
// data.BulkRoleAssignments for wiring to group extensions by the caller.
// Runs under audit purpose only (caller enforces this).
func (c *Client) collectRoleAssignments(data *SubscriptionData) {
	var raw []azRawRoleAssignment
	if err := c.queryInto("role assignment list --all", &raw); err != nil {
		c.logger.Warn("failed to collect role assignments", "error", err)
		return
	}

	// Resolve display names for service principals.
	nameCache := c.resolvePrincipalNames(raw)

	attachedToEnvelopes := 0
	groupScoped := 0
	for _, ra := range raw {
		displayName := nameCache[ra.PrincipalID]
		if displayName == "" {
			displayName = ra.PrincipalName
		}
		entry := RoleAssignment{
			RoleName:      ra.RoleDefinitionName,
			PrincipalID:   ra.PrincipalID,
			PrincipalType: ra.PrincipalType,
			PrincipalName: displayName,
		}

		if raIsResourceScope(ra.Scope) {
			key := strings.ToLower(ra.Scope)
			env, ok := data.GraphEnvelopes[key]
			if !ok {
				continue
			}
			env.RoleAssignments = append(env.RoleAssignments, entry)
			attachedToEnvelopes++
		} else {
			data.BulkRoleAssignments = append(data.BulkRoleAssignments, ScopedRoleAssignment{
				Scope:          ra.Scope,
				RoleAssignment: entry,
			})
			groupScoped++
		}
	}
	c.logger.Info("collected role assignments",
		"total", len(raw),
		"attached_to_envelopes", attachedToEnvelopes,
		"group_scoped", groupScoped,
	)
}

// raIsResourceScope returns true when the RA scope identifies a specific ARM resource
// (has a /providers/ segment after the resource-group path). Returns false for
// subscription-scope, RG-scope, and MG-scope assignments.
func raIsResourceScope(scope string) bool {
	lower := strings.ToLower(scope)
	// MG scope: /providers/microsoft.management/...
	if strings.HasPrefix(lower, "/providers/") {
		return false
	}
	// Count meaningful path segments: /subscriptions/<sub>[/resourcegroups/<rg>[/providers/...]]
	parts := strings.Split(strings.TrimPrefix(lower, "/"), "/")
	switch len(parts) {
	case 2: // /subscriptions/<sub>
		return false
	case 4: // /subscriptions/<sub>/resourcegroups/<rg>
		return parts[2] != "resourcegroups" // RG scope is group-scoped, not resource-scoped
	}
	return true // 5+ segments: resource-level scope
}

// resolvePrincipalNames looks up the Azure AD display name for each unique
// ServicePrincipal in raw. Returns a map from principalId -> displayName.
// Calls az ad sp show once per unique service principal; failures are silently
// skipped and the caller falls back to the raw principalName field.
func (c *Client) resolvePrincipalNames(raw []azRawRoleAssignment) map[string]string {
	cache := make(map[string]string)
	for _, ra := range raw {
		if strings.ToLower(ra.PrincipalType) != "serviceprincipal" {
			continue
		}
		if _, seen := cache[ra.PrincipalID]; seen {
			continue
		}
		cache[ra.PrincipalID] = "" // mark seen before the call
		var sp struct {
			DisplayName string `json:"displayName"`
		}
		if err := c.queryTenant(fmt.Sprintf("ad sp show --id %s", ra.PrincipalID), &sp); err != nil {
			continue
		}
		if sp.DisplayName != "" && sp.DisplayName != ra.PrincipalID {
			cache[ra.PrincipalID] = sp.DisplayName
		}
	}
	return cache
}

// collectDiagnosticSettings fetches Azure Monitor diagnostic settings for each resource
// in the graph envelope map. This is a per-resource call and can be slow on large
// subscriptions. Results are attached to the corresponding graph envelope entry.
// Resources that do not support diagnostic settings are silently skipped.
func (c *Client) collectDiagnosticSettings(data *SubscriptionData) {
	if len(data.GraphEnvelopes) == 0 {
		return
	}

	type envEntry struct {
		armID string
		env   *GraphEnvelope
	}
	entries := make([]envEntry, 0, len(data.GraphEnvelopes))
	for _, env := range data.GraphEnvelopes {
		if env.armID != "" && diagSettingsSupported(env.armID) {
			entries = append(entries, envEntry{armID: env.armID, env: env})
		}
	}

	c.logger.Info("collecting diagnostic settings (per-resource, may take a few minutes)", "resource_count", len(entries))

	// diagValue handles both the flat CLI format (workspaceId at top level)
	// and the nested REST API format (workspaceId under properties).
	// Azure CLI versions differ: some flatten properties, some return the raw REST envelope.
	type diagValueProps struct {
		WorkspaceID string `json:"workspaceId"`
		StorageID   string `json:"storageAccountId"`
		EventHubID  string `json:"eventHubAuthorizationRuleId"`
	}
	type diagValue struct {
		Name        string         `json:"name"`
		WorkspaceID string         `json:"workspaceId"`
		StorageID   string         `json:"storageAccountId"`
		EventHubID  string         `json:"eventHubAuthorizationRuleId"`
		Properties  diagValueProps `json:"properties"`
	}
	// parseDiagValues extracts DiagSetting entries from raw CLI output.
	// Handles three formats:
	//   1. {"value": [{flat fields}]}  - Azure CLI flattened
	//   2. [{flat fields}]             - some CLI versions return array directly
	//   3. {"value": [{"properties":{...}}]}  - raw REST API envelope
	parseDiagValues := func(out []byte) []diagValue {
		// Try wrapped object first: {"value": [...]}
		var wrapped struct {
			Value []diagValue `json:"value"`
		}
		if json.Unmarshal(out, &wrapped) == nil && len(wrapped.Value) > 0 {
			return wrapped.Value
		}
		// Try direct array: [{...}]
		var direct []diagValue
		if json.Unmarshal(out, &direct) == nil && len(direct) > 0 {
			return direct
		}
		return nil
	}

	const concurrency = 16
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	totalAttached := 0
	totalErrors := 0

	for _, entry := range entries {
		wg.Add(1)
		sem <- struct{}{}
		go func(e envEntry) {
			defer wg.Done()
			defer func() { <-sem }()

			args := []string{"monitor", "diagnostic-settings", "list", "--resource", e.armID}
			out, err := c.execAZArgs(args)
			if err != nil {
				c.logger.Debug("diagnostic-settings list failed", "resource", e.armID, "error", err)
				mu.Lock()
				totalErrors++
				mu.Unlock()
				return
			}

			values := parseDiagValues(out)
			if len(values) == 0 {
				return
			}

			diags := make([]DiagSetting, 0, len(values))
			for _, v := range values {
				d := DiagSetting{Name: v.Name}
				// Prefer flat fields; fall back to nested properties for REST API format.
				if v.WorkspaceID != "" {
					d.WorkspaceID = v.WorkspaceID
				} else {
					d.WorkspaceID = v.Properties.WorkspaceID
				}
				if v.StorageID != "" {
					d.StorageID = v.StorageID
				} else {
					d.StorageID = v.Properties.StorageID
				}
				if v.EventHubID != "" {
					d.EventHubID = v.EventHubID
				} else {
					d.EventHubID = v.Properties.EventHubID
				}
				diags = append(diags, d)
			}
			e.env.DiagSettings = diags

			mu.Lock()
			totalAttached++
			mu.Unlock()
		}(entry)
	}
	wg.Wait()

	if totalAttached > 0 {
		c.logger.Info("collected diagnostic settings", "resources_with_settings", totalAttached)
	} else if totalErrors > len(entries)/2 {
		c.logger.Warn("diagnostic settings collection: majority of calls failed - account may lack Microsoft.Insights/diagnosticSettings/read permission", "errors", totalErrors, "queried", len(entries))
	} else {
		c.logger.Info("diagnostic settings: no settings configured on any queried resource", "queried", len(entries))
	}
}

// collectVMExtensions fetches the installed extensions for each VM. Runs with bounded
// concurrency (8) to keep latency manageable on large fleets. Runs under audit purpose
// only (caller enforces this). Extensions are stored on VirtualMachine.VMExtensions.
func (c *Client) collectVMExtensions(data *SubscriptionData) {
	if len(data.VirtualMachines) == 0 {
		return
	}
	const concurrency = 8
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	total := 0
	var mu sync.Mutex

	for i := range data.VirtualMachines {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			vm := &data.VirtualMachines[i]
			cmd := fmt.Sprintf("vm extension list --resource-group %s --vm-name %s",
				vm.ResourceGroup, vm.Name)
			var exts []VMExtension
			if err := c.queryInto(cmd, &exts); err != nil {
				c.logger.Debug("failed to collect VM extensions", "vm", vm.Name, "error", err)
				return
			}
			vm.VMExtensions = exts
			mu.Lock()
			total += len(exts)
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	if total > 0 {
		c.logger.Info("collected VM extensions", "vms", len(data.VirtualMachines), "total", total)
	}
}

// collectManagementGroupEntities fetches the full entity tree visible to the current
// user (management groups + subscriptions) via `az account management-group entities list`.
// This is a tenant-level call and does not require a subscription context. Failures are
// treated as non-fatal: the MG hierarchy is omitted and a warning is logged.
func (c *Client) collectManagementGroupEntities(data *SubscriptionData) {
	var entities []MGEntity
	if c.mgEntitiesOverride != nil {
		entities = c.mgEntitiesOverride
		c.logger.Info("using cached management group entities", "total_entities", len(entities))
	} else {
		if err := c.queryTenant("account management-group entities list", &entities); err != nil {
			c.logger.Warn("failed to collect management group entities, skipping MG hierarchy", "error", err)
			return
		}
	}
	data.ManagementGroupEntities = entities
	mgCount := 0
	for _, e := range entities {
		if strings.EqualFold(e.Type, "Microsoft.Management/managementGroups") {
			mgCount++
		}
	}
	if c.mgEntitiesOverride == nil {
		c.logger.Info("collected management group entities", "management_groups", mgCount, "total_entities", len(entities))
	}
}

// EffectiveRoute represents a single effective route entry from the Azure effective route table API.
type EffectiveRoute struct {
	AddressPrefix         []string `json:"addressPrefix"`
	NextHopIPAddress      []string `json:"nextHopIpAddress"`
	NextHopType           string   `json:"nextHopType"`
	Source                string   `json:"source"`
	State                 string   `json:"state"`
	DisableBgpPropagation bool     `json:"disableBgpRoutePropagation"`
}

// effectiveRouteResult wraps the az CLI output for effective routes.
type effectiveRouteResult struct {
	Value []EffectiveRoute `json:"value"`
}

// collectPrivateDNSLinks fetches VNet links for each private DNS zone.
func (c *Client) collectPrivateDNSLinks(zones []PrivateDNSZone) {
	const concurrency = 8
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i := range zones {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			cmd := fmt.Sprintf("network private-dns link vnet list --zone-name %s --resource-group %s", zones[i].Name, zones[i].ResourceGroup)
			var links []PrivateDNSLink
			if err := c.queryInto(cmd, &links); err != nil {
				c.logger.Debug("no private DNS VNet links", "zone", zones[i].Name, "error", err)
				return
			}
			zones[i].Links = links
		}(i)
	}
	wg.Wait()
	total := 0
	for _, z := range zones {
		total += len(z.Links)
	}
	if total > 0 {
		c.logger.Info("collected", "type", "private DNS VNet links", "count", total)
	}
}

// collectFlowLogs fetches NSG flow log definitions via az graph query.
// A single KQL query returns all flow logs in the subscription without per-watcher iteration.
func (c *Client) collectFlowLogs(data *SubscriptionData) {
	if err := c.ensureAZExtension("resource-graph"); err != nil {
		c.logger.Warn("resource-graph not available; skipping flow logs", "error", err)
		return
	}
	kql := fmt.Sprintf(
		`Resources | where subscriptionId =~ '%s' and type =~ 'microsoft.network/networkwatchers/flowlogs' | project id, name, location, resourceGroup, targetResourceId = tostring(properties.targetResourceId), storageId = tostring(properties.storageId), enabled = tobool(properties.enabled)`,
		c.subscriptionID,
	)
	type flowLogPage struct {
		Count int       `json:"count"`
		Data  []FlowLog `json:"data"`
	}
	args := []string{"graph", "query", "-q", kql, "--first", "1000"}
	out, err := c.execAZArgsNoSub(args)
	if err != nil {
		c.logger.Warn("failed to collect flow logs", "error", err)
		return
	}
	var page flowLogPage
	if err := json.Unmarshal(out, &page); err != nil {
		c.logger.Warn("failed to parse flow log response", "error", err)
		return
	}
	data.FlowLogs = page.Data
	c.logger.Info("collected", "type", "NSG flow logs", "count", len(page.Data))
}

// isAuthError returns true if the error indicates a permission/authorization failure.
func isAuthError(err error) bool {
	s := err.Error()
	return strings.Contains(s, "AuthorizationFailed") ||
		strings.Contains(s, "does not have authorization") ||
		strings.Contains(s, "Authorization")
}

// collectEffectiveRoutes fetches effective routes for all NICs with concurrency control.
// Requires Microsoft.Network/networkInterfaces/effectiveRouteTable/action permission.
// On 403 (AuthorizationFailed), logs an INFO message and skips remaining NICs.
// NICs not attached to a running VM are silently skipped (Azure requires a running VM).
func (c *Client) collectEffectiveRoutes(nics []NetworkInterface) {
	if len(nics) == 0 {
		return
	}
	c.logger.Info("collecting effective routes (per-NIC action, may take a few minutes)", "nic_count", len(nics))

	// Probe permission by trying NICs until we get a definitive result:
	// - success or "not attached to VM" -> permission OK, proceed
	// - auth error -> no permission, skip all
	permissionOK := false
	probeStart := 0
	for i, nic := range nics {
		cmd := fmt.Sprintf("network nic show-effective-route-table --name %s --resource-group %s", nic.Name, nic.ResourceGroup)
		out, err := c.execAZ(cmd)
		if err != nil {
			if isAuthError(err) {
				c.logger.Info("effective routes require Microsoft.Network/networkInterfaces/effectiveRouteTable/action permission - skipping collection (account has read-only access)")
				return
			}
			// NIC not attached to running VM or other transient error - try next NIC.
			c.logger.Debug("effective routes probe skipped NIC", "nic", nic.Name, "error", err)
			if i < 4 {
				continue
			}
			// Tried 5 NICs with no success and no auth error - give up.
			c.logger.Info("effective routes probe failed on first 5 NICs (none attached to a running VM) - skipping collection")
			return
		}
		// Success - parse and mark permission as OK.
		var result effectiveRouteResult
		if err := json.Unmarshal(out, &result); err == nil && len(result.Value) > 0 {
			nics[i].EffectiveRoutes = result.Value
		}
		permissionOK = true
		probeStart = i + 1
		break
	}
	if !permissionOK {
		return
	}

	// Collect remaining NICs with concurrency.
	const concurrency = 10
	sem := make(chan struct{}, concurrency)
	var mu sync.Mutex
	collected := 0
	for _, nic := range nics {
		if len(nic.EffectiveRoutes) > 0 {
			collected++
		}
	}

	var wg sync.WaitGroup
	for i := probeStart; i < len(nics); i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int) {
			defer wg.Done()
			defer func() { <-sem }()

			nic := nics[idx]
			nicCmd := fmt.Sprintf("network nic show-effective-route-table --name %s --resource-group %s", nic.Name, nic.ResourceGroup)
			nicOut, nicErr := c.execAZ(nicCmd)
			if nicErr != nil {
				c.logger.Debug("failed to collect effective routes", "nic", nic.Name, "error", nicErr)
				return
			}
			var nicResult effectiveRouteResult
			if err := json.Unmarshal(nicOut, &nicResult); err == nil && len(nicResult.Value) > 0 {
				mu.Lock()
				nics[idx].EffectiveRoutes = nicResult.Value
				collected++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	if collected > 0 {
		c.logger.Info("collected", "type", "effective routes", "nics_with_routes", collected, "total_nics", len(nics))
	}
}

// sliceLen returns the length of a pointer-to-slice via reflection-free type switch.
func sliceLen(v any) int {
	switch s := v.(type) {
	case *[]VirtualNetwork:
		return len(*s)
	case *[]Subnet:
		return len(*s)
	case *[]NetworkInterface:
		return len(*s)
	case *[]NetworkSecurityGroup:
		return len(*s)
	case *[]RouteTable:
		return len(*s)
	case *[]PublicIPAddress:
		return len(*s)
	case *[]LoadBalancer:
		return len(*s)
	case *[]PrivateEndpoint:
		return len(*s)
	case *[]VNetGateway:
		return len(*s)
	case *[]PrivateDNSZone:
		return len(*s)
	case *[]DNSZone:
		return len(*s)
	case *[]NATGateway:
		return len(*s)
	case *[]ExpressRouteCircuit:
		return len(*s)
	case *[]AzureFirewall:
		return len(*s)
	case *[]ApplicationGateway:
		return len(*s)
	case *[]VirtualMachine:
		return len(*s)
	case *[]AppServicePlan:
		return len(*s)
	case *[]WebApp:
		return len(*s)
	case *[]ApplicationSecurityGroup:
		return len(*s)
	case *[]StorageAccount:
		return len(*s)
	case *[]KeyVault:
		return len(*s)
	case *[]ContainerRegistry:
		return len(*s)
	case *[]ManagedIdentity:
		return len(*s)
	case *[]Disk:
		return len(*s)
	case *[]Snapshot:
		return len(*s)
	case *[]ApplicationInsights:
		return len(*s)
	case *[]LogAnalyticsWorkspace:
		return len(*s)
	case *[]RecoveryServicesVault:
		return len(*s)
	case *[]BackupVault:
		return len(*s)
	case *[]SQLServer:
		return len(*s)
	case *[]PostgreSQLServer:
		return len(*s)
	case *[]MySQLServer:
		return len(*s)
	case *[]CosmosAccount:
		return len(*s)
	case *[]RedisCache:
		return len(*s)
	case *[]AKSCluster:
		return len(*s)
	case *[]ContainerAppEnvironment:
		return len(*s)
	case *[]ContainerApp:
		return len(*s)
	case *[]ContainerGroup:
		return len(*s)
	case *[]ServiceBusNamespace:
		return len(*s)
	case *[]EventHubsNamespace:
		return len(*s)
	case *[]APIMService:
		return len(*s)
	case *[]FrontDoorProfile:
		return len(*s)
	case *[]MetricAlert:
		return len(*s)
	case *[]ActionGroup:
		return len(*s)
	case *[]MGEntity:
		return len(*s)
	case *[]BastionHost:
		return len(*s)
	case *[]TrafficManagerProfile:
		return len(*s)
	case *[]DNSPrivateResolver:
		return len(*s)
	case *[]DNSForwardingRuleset:
		return len(*s)
	default:
		return 0
	}
}

// diagSettingsSupported returns true for ARM resource types that Azure Monitor
// supports diagnostic settings on. Filtering to this set avoids making thousands
// of fruitless API calls against NICs, disks, alert rules, etc.
func diagSettingsSupported(armID string) bool {
	lower := strings.ToLower(armID)
	for _, prefix := range diagSettingsTypes {
		if strings.Contains(lower, prefix) {
			return true
		}
	}
	return false
}

// diagSettingsTypes lists the ARM provider/type path segments for resource types
// that support Azure Monitor diagnostic settings.
var diagSettingsTypes = []string{
	"/microsoft.network/virtualnetworks/",
	"/microsoft.network/networksecuritygroups/",
	"/microsoft.network/loadbalancers/",
	"/microsoft.network/applicationgateways/",
	"/microsoft.network/expressroutecircuits/",
	"/microsoft.network/azurefirewalls/",
	"/microsoft.network/virtualnetworkgateways/",
	"/microsoft.network/privateendpoints/",
	"/microsoft.network/publicipaddresses/",
	"/microsoft.compute/virtualmachines/",
	"/microsoft.storage/storageaccounts/",
	"/microsoft.keyvault/vaults/",
	"/microsoft.sql/servers/databases/",
	"/microsoft.sql/servers/",
	"/microsoft.dbforpostgresql/flexibleservers/",
	"/microsoft.dbformysql/flexibleservers/",
	"/microsoft.documentdb/databaseaccounts/",
	"/microsoft.cache/redis/",
	"/microsoft.operationalinsights/workspaces/",
	"/microsoft.insights/components/",
	"/microsoft.web/sites/",
	"/microsoft.containerservice/managedclusters/",
	"/microsoft.eventhub/namespaces/",
	"/microsoft.servicebus/namespaces/",
	"/microsoft.containerregistry/registries/",
	"/microsoft.recoveryservices/vaults/",
	"/microsoft.apimanagement/service/",
	"/microsoft.app/managedenvironments/",
	"/microsoft.app/containerapps/",
	"/microsoft.network/bastionhosts/",
}
