// client.go - Azure CLI wrapper for the Azure OSIRIS JSON producer.
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
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Location      string         `json:"location"`
	ResourceGroup string         `json:"resourceGroup"`
	AddressSpace  azAddressSpace `json:"addressSpace"`
	DhcpOptions   azDHCPOptions  `json:"dhcpOptions"`
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

// Subnet represents an Azure subnet.
type Subnet struct {
	ID                   string              `json:"id"`
	Name                 string              `json:"name"`
	ResourceGroup        string              `json:"resourceGroup"`
	AddressPrefixes      []string            `json:"addressPrefixes"`
	AddressPrefix        string              `json:"addressPrefix"`
	NetworkSecurityGroup *azNSGRef           `json:"networkSecurityGroup"`
	RouteTable           *azRouteTableRef    `json:"routeTable"`
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

// IPConfiguration represents a NIC IP configuration.
type IPConfiguration struct {
	Name                      string       `json:"name"`
	Subnet                    *azSubnetRef `json:"subnet"`
	PrivateIPAddress          string       `json:"privateIpAddress"`
	PrivateIPAllocationMethod string       `json:"privateIpAllocationMethod"`
}

// SubnetID returns the subnet ID from the nested reference.
func (c IPConfiguration) SubnetID() string {
	if c.Subnet != nil {
		return c.Subnet.ID
	}
	return ""
}

// NetworkInterface represents an Azure NIC.
type NetworkInterface struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Location         string            `json:"location"`
	ResourceGroup    string            `json:"resourceGroup"`
	IPConfigurations []IPConfiguration `json:"ipConfigurations"`
}

// NetworkSecurityGroup represents an Azure NSG.
type NetworkSecurityGroup struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Location      string `json:"location"`
	ResourceGroup string `json:"resourceGroup"`
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
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Location      string  `json:"location"`
	ResourceGroup string  `json:"resourceGroup"`
	Routes        []Route `json:"routes"`
}

// azSKU matches the az CLI nested SKU object.
type azSKU struct {
	Name string `json:"name"`
	Tier string `json:"tier"`
}

// PublicIPAddress represents an Azure public IP.
type PublicIPAddress struct {
	ID                       string `json:"id"`
	Name                     string `json:"name"`
	Location                 string `json:"location"`
	ResourceGroup            string `json:"resourceGroup"`
	IPAddress                string `json:"ipAddress"`
	PublicIPAllocationMethod string `json:"publicIpAllocationMethod"`
	SKU                      azSKU  `json:"sku"`
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
	ID                string       `json:"id"`
	Name              string       `json:"name"`
	Location          string       `json:"location"`
	ResourceGroup     string       `json:"resourceGroup"`
	Subnet            *azSubnetRef `json:"subnet"`
	NetworkInterfaces []azNICRef   `json:"networkInterfaces"`
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
	ID                   string     `json:"id"`
	Name                 string     `json:"name"`
	ResourceGroup        string     `json:"resourceGroup"`
	RemoteVirtualNetwork *azVNetRef `json:"remoteVirtualNetwork"`
	PeeringState         string     `json:"peeringState"`
	AllowGatewayTransit  bool       `json:"allowGatewayTransit"`
	UseRemoteGateways    bool       `json:"useRemoteGateways"`
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
	GatewayType      string            `json:"gatewayType"`
	VPNType          string            `json:"vpnType"`
	EnableBGP        bool              `json:"enableBgp"`
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
}

// VirtualNetworkGateway1ID returns the gateway ID from the nested reference.
func (gc GatewayConnection) VirtualNetworkGateway1ID() string {
	if gc.VirtualNetworkGateway1 != nil {
		return gc.VirtualNetworkGateway1.ID
	}
	return ""
}

// PeerID returns the peer ID from the nested reference.
func (gc GatewayConnection) PeerID() string {
	if gc.Peer != nil {
		return gc.Peer.ID
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
	ID                string          `json:"id"`
	Name              string          `json:"name"`
	Location          string          `json:"location"`
	ResourceGroup     string          `json:"resourceGroup"`
	PublicIPAddresses []azPublicIPRef `json:"publicIpAddresses"`
	Subnets           []azSubnetRef   `json:"subnets"`
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

// ExpressRouteCircuit represents an Azure ExpressRoute circuit.
type ExpressRouteCircuit struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Location      string `json:"location"`
	ResourceGroup string `json:"resourceGroup"`
}

// AzureFirewall represents an Azure Firewall resource.
type AzureFirewall struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Location      string `json:"location"`
	ResourceGroup string `json:"resourceGroup"`
}

// ApplicationGateway represents an Azure Application Gateway.
type ApplicationGateway struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Location      string `json:"location"`
	ResourceGroup string `json:"resourceGroup"`
}

// VirtualMachine represents an Azure VM.
type VirtualMachine struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Location      string `json:"location"`
	ResourceGroup string `json:"resourceGroup"`
	VMSize        string `json:"vmSize"`
	PowerState    string `json:"powerState"`
}

// SubscriptionData holds all collected Azure resources for a subscription.
type SubscriptionData struct {
	Subscription         SubscriptionInfo
	ResourceGroups       []ResourceGroup
	VirtualNetworks      []VirtualNetwork
	Subnets              []Subnet
	NetworkInterfaces    []NetworkInterface
	SecurityGroups       []NetworkSecurityGroup
	RouteTables          []RouteTable
	PublicIPs            []PublicIPAddress
	LoadBalancers        []LoadBalancer
	PrivateEndpoints     []PrivateEndpoint
	VNetPeerings         []VNetPeering
	VNetGateways         []VNetGateway
	GatewayConnections   []GatewayConnection
	PrivateDNSZones      []PrivateDNSZone
	DNSZones             []DNSZone
	NATGateways          []NATGateway
	ExpressRouteCircuits []ExpressRouteCircuit
	AzureFirewalls       []AzureFirewall
	ApplicationGateways  []ApplicationGateway
	VirtualMachines      []VirtualMachine
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
	}

	for _, item := range items {
		if err := c.queryInto(item.cmd, item.dest); err != nil {
			c.logger.Warn("failed to collect resource type, skipping", "type", item.name, "error", err)
			continue
		}
		c.logger.Info("collected", "type", item.name, "count", sliceLen(item.dest))
	}

	// Subnets require iterating per VNet (no list-all command).
	data.Subnets = c.collectSubnets(data.VirtualNetworks)

	// VNet peerings require iterating per VNet.
	data.VNetPeerings = c.collectVNetPeerings(data.VirtualNetworks)

	// VNet gateways require iterating per resource group that has networking resources.
	data.VNetGateways = c.collectVNetGateways(data.ResourceGroups)

	// Gateway connections (ExpressRoute + VPN).
	data.GatewayConnections = c.collectGatewayConnections()
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

// collectGatewayConnections fetches gateway connections (ExpressRoute + VPN).
func (c *Client) collectGatewayConnections() []GatewayConnection {
	// Try both connection types - vpn-connection covers site-to-site VPN,
	// while network connection list covers ExpressRoute + VNet-to-VNet.
	var connections []GatewayConnection

	// az network vpn-connection list covers IPsec/VPN connections.
	var vpnConns []GatewayConnection
	if err := c.queryInto("network vpn-connection list", &vpnConns); err != nil {
		c.logger.Debug("no VPN connections found", "error", err)
	} else {
		connections = append(connections, vpnConns...)
	}

	if len(connections) > 0 {
		c.logger.Info("collected", "type", "gateway connections", "count", len(connections))
	}
	return connections
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
	default:
		return 0
	}
}
