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
// "[OSIRIS-JSON-AZURE]."
//
// [OSIRIS-JSON-AZURE]: https://osirisjson.org/en/docs/producers/hyperscalers/microsoft-azure
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/docs/spec/v10/00-preface

package azure

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
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
	ID       string `json:"id"`
	Name     string `json:"name"`
	Location string `json:"location"`
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
	Name                     string `json:"name"`
	Priority                 int    `json:"priority"`
	Direction                string `json:"direction"`
	Access                   string `json:"access"`
	Protocol                 string `json:"protocol"`
	SourcePortRange          string `json:"sourcePortRange"`
	DestinationPortRange     string `json:"destinationPortRange"`
	SourceAddressPrefix      string `json:"sourceAddressPrefix"`
	DestinationAddressPrefix string `json:"destinationAddressPrefix"`
}

// NetworkSecurityGroup represents an Azure NSG.
type NetworkSecurityGroup struct {
	ID                   string            `json:"id"`
	Name                 string            `json:"name"`
	Location             string            `json:"location"`
	ResourceGroup        string            `json:"resourceGroup"`
	Tags                 map[string]string `json:"tags"`
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
}

// RouteTable represents an Azure route table.
type RouteTable struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Location      string            `json:"location"`
	ResourceGroup string            `json:"resourceGroup"`
	Tags          map[string]string `json:"tags"`
	Routes        []Route           `json:"routes"`
	Subnets       []azSubnetRef     `json:"subnets"`
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
	Name string `json:"name"`
	Tier string `json:"tier"`
}

// PublicIPAddress represents an Azure public IP.
type PublicIPAddress struct {
	ID                       string            `json:"id"`
	Name                     string            `json:"name"`
	Location                 string            `json:"location"`
	ResourceGroup            string            `json:"resourceGroup"`
	Tags                     map[string]string `json:"tags"`
	IPAddress                string            `json:"ipAddress"`
	PublicIPAllocationMethod string            `json:"publicIpAllocationMethod"`
	SKU                      azSKU             `json:"sku"`
}

// azPublicIPRef matches az CLI nested public IP reference in frontend configs.
type azPublicIPRef struct {
	ID string `json:"id"`
}

// FrontendIPConfig represents a load balancer frontend IP configuration.
type FrontendIPConfig struct {
	Name                      string         `json:"name"`
	PublicIPAddress           *azPublicIPRef `json:"publicIpAddress"`
	PrivateIPAllocationMethod string         `json:"privateIpAllocationMethod"`
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

// LoadBalancer represents an Azure load balancer.
type LoadBalancer struct {
	ID                       string               `json:"id"`
	Name                     string               `json:"name"`
	Location                 string               `json:"location"`
	ResourceGroup            string               `json:"resourceGroup"`
	Tags                     map[string]string    `json:"tags"`
	SKU                      azSKU                `json:"sku"`
	FrontendIPConfigurations []FrontendIPConfig   `json:"frontendIpConfigurations"`
	BackendAddressPools      []BackendAddressPool `json:"backendAddressPools"`
	LoadBalancingRules       []LBRule             `json:"loadBalancingRules"`
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
	ID                    string     `json:"id"`
	Name                  string     `json:"name"`
	ResourceGroup         string     `json:"resourceGroup"`
	RemoteVirtualNetwork  *azVNetRef `json:"remoteVirtualNetwork"`
	PeeringState          string     `json:"peeringState"`
	AllowGatewayTransit   bool       `json:"allowGatewayTransit"`
	AllowForwardedTraffic bool       `json:"allowForwardedTraffic"`
	UseRemoteGateways     bool       `json:"useRemoteGateways"`
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
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Location         string            `json:"location"`
	ResourceGroup    string            `json:"resourceGroup"`
	Tags             map[string]string `json:"tags"`
	GatewayType      string            `json:"gatewayType"`
	VPNType          string            `json:"vpnType"`
	EnableBGP        bool              `json:"enableBgp"`
	ActiveActive     bool              `json:"activeActive"`
	SKU              azSKU             `json:"sku"`
	IPConfigurations []GatewayIPConfig `json:"ipConfigurations"`
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
	ConnectionType         string        `json:"connectionType"`
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
	ID            string           `json:"id"`
	Name          string           `json:"name"`
	ResourceGroup string           `json:"resourceGroup"`
	Links         []PrivateDNSLink `json:"links"`
}

// DNSZone represents an Azure public DNS zone.
type DNSZone struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	ResourceGroup string `json:"resourceGroup"`
}

// NATGateway represents an Azure NAT gateway.
type NATGateway struct {
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	Location          string            `json:"location"`
	ResourceGroup     string            `json:"resourceGroup"`
	Tags              map[string]string `json:"tags"`
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
	PeerASN                    int64  `json:"peerASN"`
	VlanID                     int    `json:"vlanId"`
	PrimaryPeerAddressPrefix   string `json:"primaryPeerAddressPrefix"`
	SecondaryPeerAddressPrefix string `json:"secondaryPeerAddressPrefix"`
	PrimaryAzurePort           string `json:"primaryAzurePort"`
	SecondaryAzurePort         string `json:"secondaryAzurePort"`
}

// ExpressRouteCircuit represents an Azure ExpressRoute circuit.
type ExpressRouteCircuit struct {
	ID                               string                       `json:"id"`
	Name                             string                       `json:"name"`
	Location                         string                       `json:"location"`
	ResourceGroup                    string                       `json:"resourceGroup"`
	Tags                             map[string]string            `json:"tags"`
	SKU                              azSKU                        `json:"sku"`
	ServiceProviderProperties        *azServiceProviderProperties `json:"serviceProviderProperties"`
	CircuitProvisioningState         string                       `json:"circuitProvisioningState"`
	ServiceProviderProvisioningState string                       `json:"serviceProviderProvisioningState"`
	Peerings                         []ExpressRoutePeering        `json:"peerings"`
}

// AzureFirewall represents an Azure Firewall resource.
type AzureFirewall struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Location      string            `json:"location"`
	ResourceGroup string            `json:"resourceGroup"`
	Tags          map[string]string `json:"tags"`
}

// ApplicationGateway represents an Azure Application Gateway.
type ApplicationGateway struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Location      string            `json:"location"`
	ResourceGroup string            `json:"resourceGroup"`
	Tags          map[string]string `json:"tags"`
}

// VirtualMachine represents an Azure VM.
type VirtualMachine struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Location      string            `json:"location"`
	ResourceGroup string            `json:"resourceGroup"`
	Tags          map[string]string `json:"tags"`
	VMSize        string            `json:"vmSize"`
	PowerState    string            `json:"powerState"`
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
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Location      string            `json:"location"`
	ResourceGroup string            `json:"resourceGroup"`
	Tags          map[string]string `json:"tags"`
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

// azStorageNetworkRuleSet is the top-level network ACL block on a storage
// account (flattened out of properties.networkAcls by az storage account list).
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
	NetworkRuleSet              *azStorageNetworkRuleSet   `json:"networkAcls"`
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
}

// ManagedIdentity represents a Microsoft.ManagedIdentity/userAssignedIdentities
// resource. The system-assigned variant is exposed via the parent resource's
// identity block (webapp, VM, etc.) and is NOT a standalone ARM resource.
type ManagedIdentity struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Location      string            `json:"location"`
	ResourceGroup string            `json:"resourceGroup"`
	Tags          map[string]string `json:"tags"`
	PrincipalID   string            `json:"principalId"`
	ClientID      string            `json:"clientId"`
	TenantID      string            `json:"tenantId"`
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
	LoadBalancers             []LoadBalancer
	PrivateEndpoints          []PrivateEndpoint
	VNetPeerings              []VNetPeering
	VNetGateways              []VNetGateway
	GatewayConnections        []GatewayConnection
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
	subscriptionID string
	logger         *slog.Logger
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

	// Resource groups.
	if err := c.queryInto("group list", &data.ResourceGroups); err != nil {
		return nil, fmt.Errorf("fetching resource groups: %w", err)
	}
	c.logger.Info("collected resource groups", "count", len(data.ResourceGroups))

	// Network resources - each is independent, partial failures are logged and skipped.
	c.collectNetworkResources(data)

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
		{"virtual networks", "network vnet list", &data.VirtualNetworks},
		{"network interfaces", "network nic list", &data.NetworkInterfaces},
		{"network security groups", "network nsg list", &data.SecurityGroups},
		{"application security groups", "network asg list", &data.ApplicationSecurityGroups},
		{"route tables", "network route-table list", &data.RouteTables},
		{"public IPs", "network public-ip list", &data.PublicIPs},
		{"load balancers", "network lb list", &data.LoadBalancers},
		{"private endpoints", "network private-endpoint list", &data.PrivateEndpoints},
		{"private DNS zones", "network private-dns zone list", &data.PrivateDNSZones},
		{"DNS zones", "network dns zone list", &data.DNSZones},
		{"NAT gateways", "network nat gateway list", &data.NATGateways},
		{"ExpressRoute circuits", "network express-route list", &data.ExpressRouteCircuits},
		{"firewalls", "network firewall list", &data.AzureFirewalls},
		{"application gateways", "network application-gateway list", &data.ApplicationGateways},
		{"virtual machines", "vm list -d", &data.VirtualMachines},
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
		{"backup vaults", "resource list --resource-type Microsoft.DataProtection/backupVaults", &data.BackupVaults},
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
	}

	for _, item := range items {
		if err := c.queryInto(item.cmd, item.dest); err != nil {
			c.logger.Warn("failed to collect resource type, skipping", "type", item.name, "error", err)
			continue
		}
		c.logger.Info("collected", "type", item.name, "count", sliceLen(item.dest))
	}

	// Managed disks: some CLI builds / RBAC scopes reject the subscription-wide
	// `az disk list` with "--resource-group/-g required", so iterate per RG.
	data.Disks = c.collectDisks(data.ResourceGroups)

	// SQL databases: one `az sql db list` per server (no subscription-wide list).
	c.collectSQLDatabases(data.SQLServers)

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

	// Effective routes per NIC (requires effectiveRouteTable/action permission).
	c.collectEffectiveRoutes(data.NetworkInterfaces)

	// Backup protected items per Recovery Services Vault.
	c.collectBackupProtectedItems(data.RecoveryServicesVaults)

	// Backup instances per Backup Vault.
	c.collectBackupInstances(data.BackupVaults)

	// AKS node pools: one `az aks nodepool list` per cluster.
	c.collectAKSNodePools(data.AKSClusters)
}

// collectAKSNodePools enumerates agent pools per AKS cluster. `az aks nodepool
// list` is the only CLI command that returns pools with their ARM IDs, which
// are needed for the cluster -> nodepool `contains` edge and the nodepool ->
// subnet `network` edge. Missing permissions are logged at debug level.
func (c *Client) collectAKSNodePools(clusters []AKSCluster) {
	total := 0
	for i := range clusters {
		cl := &clusters[i]
		cmd := fmt.Sprintf("aks nodepool list --cluster-name %s --resource-group %s", cl.Name, cl.ResourceGroup)
		var pools []AKSAgentPool
		if err := c.queryInto(cmd, &pools); err != nil {
			c.logger.Debug("failed to list AKS node pools", "cluster", cl.Name, "error", err)
			continue
		}
		for j := range pools {
			pools[j].ClusterID = cl.ID
			pools[j].ClusterName = cl.Name
		}
		cl.AgentPools = pools
		total += len(pools)
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
	total := 0
	for i, v := range vaults {
		cmd := fmt.Sprintf("backup item list --vault-name %s --resource-group %s", v.Name, v.ResourceGroup)
		var items []BackupProtectedItem
		if err := c.queryInto(cmd, &items); err != nil {
			c.logger.Debug("no backup items for RS vault", "vault", v.Name, "error", err)
			continue
		}
		vaults[i].ProtectedItems = items
		total += len(items)
	}
	if total > 0 {
		c.logger.Info("collected", "type", "backup protected items", "count", total)
	}
}

// collectBackupInstances fetches backup instances for each Backup Vault.
// `az dataprotection backup-instance list` requires per-vault iteration.
func (c *Client) collectBackupInstances(vaults []BackupVault) {
	total := 0
	for i, v := range vaults {
		cmd := fmt.Sprintf("dataprotection backup-instance list --vault-name %s --resource-group %s", v.Name, v.ResourceGroup)
		var insts []BackupInstance
		if err := c.queryInto(cmd, &insts); err != nil {
			c.logger.Debug("no backup instances for Backup Vault", "vault", v.Name, "error", err)
			continue
		}
		vaults[i].ProtectedInstances = insts
		total += len(insts)
	}
	if total > 0 {
		c.logger.Info("collected", "type", "backup instances", "count", total)
	}
}

// collectSubnets fetches subnets for all VNets (az has no global subnet list command).
func (c *Client) collectSubnets(vnets []VirtualNetwork) []Subnet {
	var all []Subnet
	for _, vnet := range vnets {
		cmd := fmt.Sprintf("network vnet subnet list --vnet-name %s --resource-group %s", vnet.Name, vnet.ResourceGroup)
		var subnets []Subnet
		if err := c.queryInto(cmd, &subnets); err != nil {
			c.logger.Warn("failed to collect subnets, skipping", "vnet", vnet.Name, "error", err)
			continue
		}
		// Backfill the resource group since az network vnet subnet list
		// does not always include it.
		for i := range subnets {
			if subnets[i].ResourceGroup == "" {
				subnets[i].ResourceGroup = vnet.ResourceGroup
			}
		}
		all = append(all, subnets...)
	}
	if len(all) > 0 {
		c.logger.Info("collected", "type", "subnets", "count", len(all))
	}
	return all
}

// collectVNetPeerings fetches peerings for all VNets.
func (c *Client) collectVNetPeerings(vnets []VirtualNetwork) []VNetPeering {
	var all []VNetPeering
	for _, vnet := range vnets {
		cmd := fmt.Sprintf("network vnet peering list --vnet-name %s --resource-group %s", vnet.Name, vnet.ResourceGroup)
		var peerings []VNetPeering
		if err := c.queryInto(cmd, &peerings); err != nil {
			c.logger.Warn("failed to collect VNet peerings, skipping", "vnet", vnet.Name, "error", err)
			continue
		}
		all = append(all, peerings...)
	}
	if len(all) > 0 {
		c.logger.Info("collected", "type", "VNet peerings", "count", len(all))
	}
	return all
}

// collectVNetGateways fetches VNet gateways per resource group.
func (c *Client) collectVNetGateways(rgs []ResourceGroup) []VNetGateway {
	var all []VNetGateway
	for _, rg := range rgs {
		cmd := fmt.Sprintf("network vnet-gateway list --resource-group %s", rg.Name)
		var gateways []VNetGateway
		if err := c.queryInto(cmd, &gateways); err != nil {
			// Most resource groups won't have gateways - debug level only.
			c.logger.Debug("no VNet gateways in resource group", "rg", rg.Name)
			continue
		}
		all = append(all, gateways...)
	}
	if len(all) > 0 {
		c.logger.Info("collected", "type", "VNet gateways", "count", len(all))
	}
	return all
}

// collectDisks fetches managed disks per resource group. Some Azure CLI
// configurations reject the subscription-wide `az disk list` form with
// "--resource-group/-g is required", so we iterate RGs to be safe.
func (c *Client) collectDisks(rgs []ResourceGroup) []Disk {
	var all []Disk
	for _, rg := range rgs {
		cmd := fmt.Sprintf("disk list --resource-group %s", rg.Name)
		var disks []Disk
		if err := c.queryInto(cmd, &disks); err != nil {
			c.logger.Debug("no managed disks in resource group", "rg", rg.Name)
			continue
		}
		all = append(all, disks...)
	}
	c.logger.Info("collected", "type", "managed disks", "count", len(all))
	return all
}

// collectSQLDatabases enumerates databases per SQL server. Each database gets
// its parent server's ARM ID stamped into ServerID so transforms can wire the
// server -> database `contains` edge without reparsing the db ID path.
// The implicit `master` system database returned by `az sql db list` is
// skipped - it is not a topology-relevant workload database.
func (c *Client) collectSQLDatabases(servers []SQLServer) {
	for i := range servers {
		srv := &servers[i]
		cmd := fmt.Sprintf("sql db list --resource-group %s --server %s", srv.ResourceGroup, srv.Name)
		var dbs []SQLDatabase
		if err := c.queryInto(cmd, &dbs); err != nil {
			c.logger.Debug("failed to list SQL databases", "server", srv.Name, "error", err)
			continue
		}
		for j := range dbs {
			if strings.EqualFold(dbs[j].Name, "master") {
				continue
			}
			dbs[j].ServerID = srv.ID
			srv.Databases = append(srv.Databases, dbs[j])
		}
	}
	total := 0
	for _, s := range servers {
		total += len(s.Databases)
	}
	if total > 0 {
		c.logger.Info("collected", "type", "sql databases", "count", total)
	}
}

// collectGatewayConnections fetches gateway connections (ExpressRoute + VPN) per resource group.
// az network vpn-connection list requires --resource-group to return results.
func (c *Client) collectGatewayConnections(rgs []ResourceGroup) []GatewayConnection {
	var all []GatewayConnection
	for _, rg := range rgs {
		cmd := fmt.Sprintf("network vpn-connection list --resource-group %s", rg.Name)
		var conns []GatewayConnection
		if err := c.queryInto(cmd, &conns); err != nil {
			c.logger.Debug("no gateway connections in resource group", "rg", rg.Name)
			continue
		}
		all = append(all, conns...)
	}
	if len(all) > 0 {
		c.logger.Info("collected", "type", "gateway connections", "count", len(all))
	}
	return all
}

// collectExpressRoutePeerings fetches peerings for each ExpressRoute circuit.
func (c *Client) collectExpressRoutePeerings(circuits []ExpressRouteCircuit) {
	for i, circuit := range circuits {
		cmd := fmt.Sprintf("network express-route peering list --circuit-name %s --resource-group %s",
			circuit.Name, circuit.ResourceGroup)
		var peerings []ExpressRoutePeering
		if err := c.queryInto(cmd, &peerings); err != nil {
			c.logger.Debug("no peerings for ExpressRoute circuit", "circuit", circuit.Name, "error", err)
			continue
		}
		circuits[i].Peerings = peerings
	}
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

// execAZ runs an Azure CLI command and returns the raw JSON output.
// The az binary path is resolved once via exec.LookPath to avoid
// untrusted search path issues [CWE-426](https://cwe.mitre.org/data/definitions/426).
func (c *Client) execAZ(command string) ([]byte, error) {
	azPath, err := resolveAZPath()
	if err != nil {
		return nil, fmt.Errorf("Azure CLI (az) not found in PATH: %w", err)
	}

	args := strings.Fields(command)
	args = append(args, "--subscription", c.subscriptionID, "--output", "json")

	fullArgs := append([]string{azPath}, args...)
	c.logger.Debug("executing Azure CLI", "command", strings.Join(fullArgs, " "))

	cmd := exec.Command(azPath, args...)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("az %s failed: %s", command, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("az %s: %w", command, err)
	}
	return out, nil
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
	for i, z := range zones {
		cmd := fmt.Sprintf("network private-dns link vnet list --zone-name %s --resource-group %s", z.Name, z.ResourceGroup)
		var links []PrivateDNSLink
		if err := c.queryInto(cmd, &links); err != nil {
			c.logger.Debug("no private DNS VNet links", "zone", z.Name, "error", err)
			continue
		}
		zones[i].Links = links
	}
	total := 0
	for _, z := range zones {
		total += len(z.Links)
	}
	if total > 0 {
		c.logger.Info("collected", "type", "private DNS VNet links", "count", total)
	}
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
	default:
		return 0
	}
}
