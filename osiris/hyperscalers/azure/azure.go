// Package azure implements the Microsoft Azure OSIRIS JSON producer.
// Collects networking and compute resources from Azure subscriptions via the
// Azure CLI (az) and generates OSIRIS JSON documents.
//
// The producer requires the user to be authenticated via 'az login' and have
// Reader access to the target subscriptions.
//
// Operating modes:
//
//	Single:   osirisjson-producer azure -S <subscription-id>
//	Multi:    osirisjson-producer azure -S sub1,sub2,sub3 -o ./output
//	All:      osirisjson-producer azure --all -o ./output
//	CSV:      osirisjson-producer azure -s subscriptions.csv -o ./output
//	Template: osirisjson-producer azure template --generate
//
// Output hierarchy (batch/multi/all modes):
//
//	<output-dir>/
//	  <TenantID>/
//	    <timestamp>/
//	      <SubscriptionName>.json
//
// Each subscription is a self-contained OSIRIS JSON document. Consumers can
// correlate documents across subscriptions (e.g. cross-subscription VNet
// peerings reference remote subscription IDs as resources).
//
// For multi-tenant environments, users run the producer once per tenant
// (each az login authenticates to one tenant). The output hierarchy
// naturally separates tenants into their own directories.
//
// For an introduction to OSIRIS JSON Producer for Microsoft Azure see:
// [OSIRIS-JSON-AZURE]: https://osirisjson.org/en/docs/producers/hyperscalers/microsoft-azure
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/docs/spec/v10/00-preface

package azure

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.osirisjson.org/producers/pkg/osirismeta"
	"go.osirisjson.org/producers/pkg/sdk"
)

const (
	generatorName    = "osirisjson-producer-azure"
	generatorVersion = "0.5.0"
	generatorURL     = "https://osirisjson.org/en/docs/producers/hyperscalers/microsoft-azure"
)

// Producer implements the OSIRIS JSON sdk.Producer interface for Microsoft Azure.
type Producer struct {
	target SubscriptionTarget
	cfg    *Config
	client *Client // injectable for testing.
}

// NewProducer creates an Azure producer for the given subscription target.
func NewProducer(target SubscriptionTarget, cfg *Config) *Producer {
	return &Producer{target: target, cfg: cfg}
}

// Collect queries Azure via the CLI and builds an OSIRIS JSON document.
func (p *Producer) Collect(ctx *sdk.Context) (*sdk.Document, error) {
	client := p.client
	if client == nil {
		client = NewClient(p.target.SubscriptionID, ctx.Logger)
	}
	client.purpose = p.cfg.Purpose
	// Inject pre-fetched MG entities from a batch run to avoid N redundant
	// tenant-scoped API calls (one per subscription, all returning the same data).
	if len(p.cfg.MGEntitiesCache) > 0 {
		client.mgEntitiesOverride = p.cfg.MGEntitiesCache
	}

	ctx.Logger.Info("collecting Azure subscription data",
		"subscription", p.target.SubscriptionID,
		"name", p.target.SubscriptionName,
	)

	// Fetch all resources from the subscription.
	data, err := client.Collect()
	if err != nil {
		return nil, fmt.Errorf("Azure collection failed: %w", err)
	}

	sub := data.Subscription

	// Backfill the target's tenant ID from the subscription metadata
	// so the batch runner can build the correct output path.
	if p.target.TenantID == "" && sub.TenantID != "" {
		p.target.TenantID = sub.TenantID
	}

	// Transform Azure data to OSIRIS JSON types.
	vnetResources := TransformVNets(data.VirtualNetworks, data.VNetPeerings, sub)
	subnetResources, subnetIDMap := TransformSubnets(data.Subnets, data.VirtualNetworks, sub)
	nicResources, nicIDMap := TransformNICs(data.NetworkInterfaces, sub)
	nsgResources, nsgIDMap := TransformNSGs(data.SecurityGroups, sub)
	rtResources, rtIDMap := TransformRouteTables(data.RouteTables, sub)
	publicIPResources := TransformPublicIPs(data.PublicIPs, sub)
	lbResources := TransformLoadBalancers(data.LoadBalancers, sub)
	peResources := TransformPrivateEndpoints(data.PrivateEndpoints, sub)
	gwResources := TransformVNetGateways(data.VNetGateways, data.GatewayConnections, sub)
	natGWResources := TransformNATGateways(data.NATGateways, sub)
	fwResources := TransformFirewalls(data.AzureFirewalls, sub)
	appGWResources := TransformAppGateways(data.ApplicationGateways, sub)
	dnsResources := TransformDNSZones(data.DNSZones, sub)
	privateDNSResources := TransformPrivateDNSZones(data.PrivateDNSZones, sub)
	erResources := TransformExpressRouteCircuits(data.ExpressRouteCircuits, sub)
	vmResources := TransformVMs(data.VirtualMachines, sub)
	aspResources, aspIDMap := TransformAppServicePlans(data.AppServicePlans, sub)
	webAppResources, webAppIDMap := TransformWebApps(data.WebApps, sub)
	asgResources, asgIDMap := TransformApplicationSecurityGroups(data.ApplicationSecurityGroups, sub)
	storageResources, storageIDMap := TransformStorageAccounts(data.StorageAccounts, sub)
	keyVaultResources, keyVaultIDMap := TransformKeyVaults(data.KeyVaults, sub)
	acrResources, acrIDMap := TransformContainerRegistries(data.ContainerRegistries, sub)
	miResources, _ := TransformManagedIdentities(data.ManagedIdentities, sub)
	diskResources, diskIDMap := TransformDisks(data.Disks, sub)
	snapshotResources, snapshotIDMap := TransformSnapshots(data.Snapshots, sub)
	aiResources, aiIDMap := TransformApplicationInsights(data.ApplicationInsights, sub)
	laResources, laIDMap := TransformLogAnalyticsWorkspaces(data.LogAnalyticsWorkspaces, sub)
	rsvResources, rsvIDMap := TransformRecoveryServicesVaults(data.RecoveryServicesVaults, sub)
	bvResources, bvIDMap := TransformBackupVaults(data.BackupVaults, sub)
	sqlServerResources, sqlServerIDMap := TransformSQLServers(data.SQLServers, sub)
	sqlDatabaseResources, sqlDatabaseIDMap := TransformSQLDatabases(data.SQLServers, sub)
	pgResources, pgIDMap := TransformPostgreSQLServers(data.PostgreSQLServers, sub)
	mysqlResources, mysqlIDMap := TransformMySQLServers(data.MySQLServers, sub)
	cosmosResources, cosmosIDMap := TransformCosmosAccounts(data.CosmosAccounts, sub)
	redisResources, redisIDMap := TransformRedisCaches(data.RedisCaches, sub)
	aksResources, aksIDMap := TransformAKSClusters(data.AKSClusters, sub)
	aksPoolResources, aksPoolIDMap := TransformAKSAgentPools(data.AKSClusters, sub)
	containerEnvResources, containerEnvIDMap := TransformContainerAppEnvironments(data.ContainerAppEnvironments, sub)
	containerAppResources, containerAppIDMap := TransformContainerApps(data.ContainerApps, sub)
	containerGroupResources, containerGroupIDMap := TransformContainerGroups(data.ContainerGroups, sub)
	serviceBusResources, serviceBusIDMap := TransformServiceBusNamespaces(data.ServiceBusNamespaces, sub)
	eventHubsResources, eventHubsIDMap := TransformEventHubsNamespaces(data.EventHubsNamespaces, sub)
	apimResources, apimIDMap := TransformAPIMServices(data.APIMServices, sub)
	frontDoorResources, _ := TransformFrontDoorProfiles(data.FrontDoorProfiles, sub)
	var metricAlertResources []sdk.Resource // Metric alerts and action groups are observation/policy resources that inflate the default-purpose output. Gate to audit only.
	metricAlertIDMap := map[string]string{}
	var actionGroupResources []sdk.Resource
	agIDMap := map[string]string{}
	if p.cfg.Purpose == "audit" {
		metricAlertResources, metricAlertIDMap = TransformMetricAlerts(data.MetricAlerts, sub)
		actionGroupResources, agIDMap = TransformActionGroups(data.ActionGroups, sub)
	}
	bastionResources, bastionIDMap := TransformBastionHosts(data.BastionHosts, sub)
	tmResources, tmIDMap := TransformTrafficManagerProfiles(data.TrafficManagerProfiles, sub)
	dnsResolverResources, dnsResolverIDMap := TransformDNSPrivateResolvers(data.DNSPrivateResolvers, sub)
	dnsRulesetResources, dnsRulesetIDMap := TransformDNSForwardingRulesets(data.DNSForwardingRulesets, sub)
	pipPrefixResources, pipPrefixIDMap := TransformPublicIPPrefixes(data.PublicIPPrefixes, sub)
	asetResources, asetIDMap := TransformAvailabilitySets(data.AvailabilitySets, sub)
	routeServerResources, routeServerIDMap := TransformRouteServers(data.RouteServers, sub)
	gwConnResources, gwConnIDMap := TransformGatewayConnectionResources(data.GatewayConnections, sub)
	// H1 resources
	vmssResources, vmssIDMap := TransformVMSSes(data.VMSSes, sub)
	sqlMIResources, sqlMIIDMap := TransformSQLManagedInstances(data.SQLManagedInstances, sub)
	sqlMIDBResources, sqlMIDBIDMap := TransformSQLMIDatabases(data.SQLManagedInstances, sub)
	sqlElasticPoolResources, sqlElasticPoolIDMap := TransformSQLElasticPools(data.SQLServers, sub)
	sqlVMResources, sqlVMIDMap := TransformSQLVMs(data.SQLVirtualMachines, sub)
	logicResources, _ := TransformLogicWorkflows(data.LogicWorkflows, sub)
	dataFactoryResources, _ := TransformDataFactories(data.DataFactories, sub)
	synapseResources, _ := TransformSynapseWorkspaces(data.SynapseWorkspaces, sub)
	commSvcResources, _ := TransformCommunicationServices(data.CommunicationServices, sub)
	automationResources, _ := TransformAutomationAccounts(data.AutomationAccounts, sub)
	arcMachineResources, _ := TransformArcMachines(data.ArcMachines, sub)
	dcrResources, dcrIDMap := TransformDataCollectionRules(data.DataCollectionRules, sub)
	dceResources, _ := TransformDataCollectionEndpoints(data.DataCollectionEndpoints, sub)
	autoscaleResources, autoscaleIDMap := TransformAutoscaleSettings(data.AutoscaleSettings, sub)
	// H2 resources
	logicAPIConnResources, logicAPIConnIDMap := TransformLogicAPIConnections(data.LogicAPIConnections, sub)
	emailSvcResources, emailSvcIDMap := TransformEmailServices(data.EmailServices, sub)
	emailDomainResources, emailDomainIDMap := TransformEmailDomains(data.EmailDomains, sub)
	streamAnalyticsResources, _ := TransformStreamAnalyticsJobs(data.StreamAnalyticsJobs, sub)
	eventGridResources, _ := TransformEventGridSystemTopics(data.EventGridSystemTopics, sub)
	slotResources, slotIDMap := TransformAppServiceSlots(data.AppServiceSlots, sub)

	// Build ID maps for connection wiring.
	vnetIDMap := BuildVNetIDMap(data.VirtualNetworks)
	publicIPIDMap := BuildPublicIPIDMap(data.PublicIPs)
	peIDMap := BuildPrivateEndpointIDMap(data.PrivateEndpoints)
	vmIDMap := BuildVMIDMap(data.VirtualMachines)
	lbIDMap := BuildLBIDMap(data.LoadBalancers)

	// Network topology connections - the full end-to-end path.
	//
	// Layer 1: VNet peering (cross-VNet and cross-subscription connectivity)
	peeringConns, peeringStubs := TransformVNetPeerings(data.VNetPeerings, vnetIDMap)
	//
	// Layer 2: Subnet <-> VNet containment
	subnetVNetConns := TransformSubnetToVNetConnections(data.Subnets, subnetIDMap, vnetIDMap)
	//
	// Layer 3: Subnet policy and routing
	nsgConns := TransformSubnetNSGConnections(data.Subnets, subnetIDMap, nsgIDMap)
	rtConns := TransformSubnetRouteTableConnections(data.Subnets, subnetIDMap, rtIDMap)
	//
	// Layer 4: NIC <-> Subnet (e.g. how VMs attach to the network)
	nicSubnetConns := TransformNICToSubnetConnections(data.NetworkInterfaces, nicIDMap, subnetIDMap)
	//
	// Layer 5: Private endpoints (private link connectivity)
	peSubnetConns := TransformPrivateEndpointToSubnetConnections(data.PrivateEndpoints, subnetIDMap)
	peNICConns := TransformPrivateEndpointToNICConnections(data.PrivateEndpoints, nicIDMap)
	//
	// Layer 6: Load balancer frontend -> public IP / subnet
	lbPIPConns := TransformLBFrontendToPublicIPConnections(data.LoadBalancers, publicIPIDMap)
	lbSubnetConns := TransformLBToSubnetConnections(data.LoadBalancers, lbIDMap, subnetIDMap)
	//
	// Layer 7: VNet gateways (ExpressRoute/VPN ingress)
	gwSubnetConns := TransformVNetGatewayToSubnetConnections(data.VNetGateways, subnetIDMap)
	gwPIPConns := TransformVNetGatewayToPublicIPConnections(data.VNetGateways, publicIPIDMap)
	//
	// Layer 8: NAT gateways (outbound SNAT)
	natSubnetConns := TransformNATGatewayToSubnetConnections(data.NATGateways, subnetIDMap)
	natPIPConns := TransformNATGatewayToPublicIPConnections(data.NATGateways, publicIPIDMap)
	//
	// Layer 9: Private DNS zone -> VNet links (stubs for cross-subscription VNets)
	dnsVNetConns, dnsVNetStubs := TransformPrivateDNSToVNetConnections(data.PrivateDNSZones, vnetIDMap)
	//
	// Layer 10: NIC -> Application Security Group membership
	nicASGConns := TransformNICToASGConnections(data.NetworkInterfaces, nicIDMap, asgIDMap)
	//
	// Layer 11: App Service Plan -> Web App / Function App (contains)
	planAppConns := TransformWebAppToPlanConnections(data.WebApps, webAppIDMap, aspIDMap)
	//
	// Layer 12: Web App / Function App -> VNet integration subnet
	webAppSubnetConns := TransformWebAppToSubnetConnections(data.WebApps, webAppIDMap, subnetIDMap)
	//
	// Layer 13: Private Endpoint -> Web App / Function App (Private Link binding)
	peWebAppConns := TransformPEToWebAppConnections(data.WebApps, webAppIDMap, peIDMap)
	//
	// Layer 14: Private Endpoint -> Storage Account / Key Vault / Container Registry
	peStorageConns := TransformPEToStorageConnections(data.StorageAccounts, storageIDMap, peIDMap)
	peKeyVaultConns := TransformPEToKeyVaultConnections(data.KeyVaults, keyVaultIDMap, peIDMap)
	peACRConns := TransformPEToContainerRegistryConnections(data.ContainerRegistries, acrIDMap, peIDMap)
	//
	// Layer 15: Snapshot -> source disk/snapshot (creation lineage, contains/reverse)
	snapshotDiskConns := TransformSnapshotToDiskConnections(data.Snapshots, snapshotIDMap, diskIDMap)
	//
	// Layer 16: Disk -> VM (managedBy attachment, contains/reverse)
	diskVMConns := TransformDiskToVMConnections(data.Disks, diskIDMap, vmIDMap)
	//
	// Layer 17: App Insights -> Log Analytics workspace (workspace-based AI only)
	aiWorkspaceConns := TransformAppInsightsToWorkspaceConnections(data.ApplicationInsights, aiIDMap, laIDMap)
	//
	// Layer 18: Web App / Function App -> App Insights (via hidden-link tag)
	webAppAIConns := TransformWebAppToAppInsightsConnections(data.WebApps, webAppIDMap, aiIDMap)
	//
	// Layer 19: Private Endpoint -> Recovery Services Vault
	peRSVConns := TransformPEToRecoveryServicesVaultConnections(data.RecoveryServicesVaults, rsvIDMap, peIDMap)
	//
	// Layer 20: Protected resource -> vault (backup data flow).
	// Merges every ARM ID -> resource ID map so a backup item's source (VM,
	// disk, storage account, web/function app, etc.) can be wired to the vault.
	backupSrcIDMap := BuildAllResourceIDMap(
		vmIDMap, diskIDMap, storageIDMap, keyVaultIDMap, acrIDMap,
		webAppIDMap, snapshotIDMap,
	)
	rsvProtectedConns := TransformBackupProtectedItemConnections(data.RecoveryServicesVaults, rsvIDMap, backupSrcIDMap)
	bvInstanceConns := TransformBackupInstanceConnections(data.BackupVaults, bvIDMap, backupSrcIDMap)
	//
	// Layer 21: SQL Server -> SQL Database (contains)
	sqlServerDBConns := TransformSQLServerContainsDatabaseConnections(data.SQLServers, sqlServerIDMap, sqlDatabaseIDMap)
	//
	// Layer 22: Private Endpoint -> SQL Server / Cosmos DB / Redis
	peSQLConns := TransformPEToSQLServerConnections(data.SQLServers, sqlServerIDMap, peIDMap)
	peCosmosConns := TransformPEToCosmosAccountConnections(data.CosmosAccounts, cosmosIDMap, peIDMap)
	peRedisConns := TransformPEToRedisConnections(data.RedisCaches, redisIDMap, peIDMap)
	//
	// Layer 23: PG / MySQL flexible server -> delegated subnet (VNet integrated),
	// Redis Premium -> injected subnet. Combined DB IDs into a single lookup so
	// the helper can resolve either source type.
	flexServerIDMap := BuildAllResourceIDMap(pgIDMap, mysqlIDMap)
	flexSubnetConns := TransformFlexServerToSubnetConnections(data.PostgreSQLServers, data.MySQLServers, flexServerIDMap, subnetIDMap)
	redisSubnetConns := TransformRedisToSubnetConnections(data.RedisCaches, redisIDMap, subnetIDMap)
	//
	// Layer 24: AKS cluster -> agent pool (contains)
	aksPoolConns := TransformAKSClusterContainsAgentPoolConnections(data.AKSClusters, aksIDMap, aksPoolIDMap)
	//
	// Layer 25: AKS node pool -> delegated subnet (network)
	aksPoolSubnetConns := TransformAKSNodePoolToSubnetConnections(data.AKSClusters, aksPoolIDMap, subnetIDMap)
	//
	// Layer 26: Private Endpoint -> AKS cluster (private cluster Private Link)
	peAKSConns := TransformPEToAKSClusterConnections(data.AKSClusters, aksIDMap, peIDMap)
	//
	// Layer 27: Container App Environment -> Container App (contains)
	envAppConns := TransformContainerEnvContainsAppConnections(data.ContainerApps, containerEnvIDMap, containerAppIDMap)
	//
	// Layer 28: Container App Environment -> infrastructure subnet (VNet integrated)
	envSubnetConns := TransformContainerEnvToSubnetConnections(data.ContainerAppEnvironments, containerEnvIDMap, subnetIDMap)
	//
	// Layer 29: ACI Container Group -> subnet (VNet integrated)
	cgSubnetConns := TransformContainerGroupToSubnetConnections(data.ContainerGroups, containerGroupIDMap, subnetIDMap)
	//
	// Layer 30: Private Endpoint -> Service Bus / Event Hubs / APIM
	peServiceBusConns := TransformPEToServiceBusConnections(data.ServiceBusNamespaces, serviceBusIDMap, peIDMap)
	peEventHubsConns := TransformPEToEventHubsConnections(data.EventHubsNamespaces, eventHubsIDMap, peIDMap)
	peAPIMConns := TransformPEToAPIMConnections(data.APIMServices, apimIDMap, peIDMap)
	//
	// Layer 31: APIM -> subnet (VNet integrated, External / Internal mode)
	apimSubnetConns := TransformAPIMToSubnetConnections(data.APIMServices, apimIDMap, subnetIDMap)
	//
	// Layer 32: Gateway connections (ExpressRoute circuit <-> gateway)
	allIDMap := BuildAllResourceIDMap(vnetIDMap, subnetIDMap, nicIDMap, nsgIDMap, rtIDMap)
	for _, gw := range data.VNetGateways {
		allIDMap[gw.ID] = resourceID("osiris.azure.gateway.vnet", gw.ID)
	}
	for _, er := range data.ExpressRouteCircuits {
		allIDMap[er.ID] = resourceID("osiris.azure.expressroute", er.ID)
	}
	gwConns, gwStubs := TransformGatewayConnections(data.GatewayConnections, allIDMap)
	//
	// Layer 33: Metric Alert -> Action Group (audit only, gated with resources above)
	var alertAGConns []sdk.Connection
	if p.cfg.Purpose == "audit" {
		alertAGConns = TransformMetricAlertToActionGroupConnections(data.MetricAlerts, metricAlertIDMap, agIDMap)
	}
	//
	// Layer 34: scope ID map always needed for Traffic Manager (layer 36).
	// Alert-to-scope connections are audit-only because alerts are audit-only.
	alertScopeIDMap := BuildAllResourceIDMap(
		vmIDMap, diskIDMap, storageIDMap, keyVaultIDMap, acrIDMap,
		webAppIDMap, snapshotIDMap,
		serviceBusIDMap, eventHubsIDMap, apimIDMap,
		cosmosIDMap, redisIDMap, pgIDMap, mysqlIDMap,
		sqlServerIDMap, sqlDatabaseIDMap,
		aksIDMap, aksPoolIDMap,
		containerEnvIDMap, containerAppIDMap, containerGroupIDMap,
		rsvIDMap, bvIDMap,
		aiIDMap, laIDMap,
		subnetIDMap, nicIDMap, nsgIDMap, rtIDMap,
		vnetIDMap, publicIPIDMap, peIDMap,
		lbIDMap, asgIDMap,
		bastionIDMap, tmIDMap, dnsResolverIDMap, dnsRulesetIDMap,
		routeServerIDMap, gwConnIDMap,
		pipPrefixIDMap, asetIDMap,
		// H1
		vmssIDMap, sqlMIIDMap,
		// H2
		logicAPIConnIDMap, emailSvcIDMap, emailDomainIDMap, slotIDMap,
	)
	var alertScopeConns []sdk.Connection
	if p.cfg.Purpose == "audit" {
		alertScopeConns = TransformMetricAlertToScopeConnections(data.MetricAlerts, metricAlertIDMap, alertScopeIDMap)
	}
	//
	// Layer 35: Bastion -> subnet (AzureBastionSubnet) and public IP
	bastionSubnetConns := TransformBastionToSubnetConnections(data.BastionHosts, bastionIDMap, subnetIDMap)
	bastionPIPConns := TransformBastionToPublicIPConnections(data.BastionHosts, bastionIDMap, publicIPIDMap)
	//
	// Layer 36: Traffic Manager -> Azure endpoint target resources
	tmTargetConns := TransformTrafficManagerToTargetConnections(data.TrafficManagerProfiles, tmIDMap, alertScopeIDMap)
	//
	// Layer 37: DNS private resolver -> bound VNet
	dnsResolverVNetConns := TransformDNSResolverToVNetConnections(data.DNSPrivateResolvers, dnsResolverIDMap, vnetIDMap)
	//
	// Layer 38: DNS forwarding ruleset -> DNS private resolver (via outbound endpoint)
	dnsRulesetResolverConns := TransformDNSRulesetToResolverConnections(data.DNSForwardingRulesets, dnsRulesetIDMap, dnsResolverIDMap)
	//
	// Layer 39: Route Server -> RouteServerSubnet and public IP
	routeServerConns := TransformRouteServerConnections(data.RouteServers, routeServerIDMap, subnetIDMap, publicIPIDMap)
	//
	// Layer 40: Availability Set -> member VMs (containment)
	asetVMConns := TransformAvailabilitySetToVMConnections(data.AvailabilitySets, asetIDMap, vmIDMap)
	//
	// Layer 41: VMSS -> subnet (network - scale set NIC config)
	vmssSubnetConns := TransformVMSSToSubnetConnections(data.VMSSes, vmssIDMap, subnetIDMap)
	//
	// Layer 42: SQL Managed Instance -> subnet (VNet injection)
	sqlMISubnetConns := TransformSQLMIToSubnetConnections(data.SQLManagedInstances, sqlMIIDMap, subnetIDMap)
	//
	// Layer 42a: SQL MI -> databases (containment)
	sqlMIDBConns := TransformSQLMIContainsDatabaseConnections(data.SQLManagedInstances, sqlMIIDMap, sqlMIDBIDMap)
	//
	// Layer 42b: SQL Server -> elastic pools (containment)
	sqlElasticPoolConns := TransformSQLServerContainsElasticPoolConnections(data.SQLServers, sqlServerIDMap, sqlElasticPoolIDMap)
	//
	// Layer 42c: SQL VM -> underlying Azure VM (dependency)
	sqlVMToVMConns := TransformSQLVMToVMConnections(data.SQLVirtualMachines, sqlVMIDMap, vmIDMap)
	//
	// Layer 43: DCR -> Log Analytics workspace (dependency - telemetry routing)
	dcrWorkspaceConns := TransformDCRToWorkspaceConnections(data.DataCollectionRules, dcrIDMap, laIDMap)
	//
	// Layer 44: Autoscale -> target resource (dependency - governs scaling)
	// Build a merged ID map covering all resource types that can be autoscale targets.
	autoscaleScopeIDMap := BuildAllResourceIDMap(vmIDMap, vmssIDMap, aksIDMap, webAppIDMap, aspIDMap)
	autoscaleTargetConns := TransformAutoscaleToTargetConnections(data.AutoscaleSettings, autoscaleIDMap, autoscaleScopeIDMap)
	//
	// Layer 45: Email service -> domain (contains)
	emailDomainConns := TransformEmailServiceContainsDomainConnections(data.EmailDomains, emailSvcIDMap, emailDomainIDMap)
	//
	// Layer 46: Web app -> slot (contains)
	webAppSlotConns := TransformWebAppContainsSlotConnections(data.AppServiceSlots, webAppIDMap, slotIDMap)
	// Build resource group resources (container.resourcegroup per OSIRIS JSON specification Appendix C.5).
	rgResources := TransformResourceGroupResources(data.ResourceGroups, sub)

	// Build groups.
	subGroup := TransformSubscriptionGroup(sub)

	// Management group hierarchy (tenant-scoped, best-effort).
	// Finds the ancestor MG chain for this subscription, creates logical.managementgroup
	// groups, and wires the subscription group as a child of its direct parent MG.
	mgAncestors, mgPath := buildMGAncestors(data.ManagementGroupEntities, sub.SubscriptionID)
	mgGroups, _ := TransformManagementGroupGroups(mgAncestors)
	if len(mgPath) > 0 {
		subGroup.Extensions = map[string]any{
			extensionNamespace: map[string]any{
				"management_group_path": mgPath,
			},
		}
	}
	WireMGHierarchy(mgGroups, &subGroup)

	rgGroups, rgNameToID := TransformResourceGroupGroups(data.ResourceGroups, sub)

	// Collect all resources for group wiring.
	allResources := make([]sdk.Resource, 0,
		len(rgResources)+len(vnetResources)+len(subnetResources)+len(nicResources)+len(nsgResources)+
			len(rtResources)+len(publicIPResources)+len(lbResources)+len(peResources)+
			len(gwResources)+len(natGWResources)+len(fwResources)+len(appGWResources)+
			len(dnsResources)+len(privateDNSResources)+len(erResources)+len(vmResources)+
			len(aspResources)+len(webAppResources)+len(asgResources)+
			len(storageResources)+len(keyVaultResources)+len(acrResources)+
			len(miResources)+len(diskResources)+len(snapshotResources)+
			len(aiResources)+len(laResources)+
			len(rsvResources)+len(bvResources)+
			len(sqlServerResources)+len(sqlDatabaseResources)+
			len(pgResources)+len(mysqlResources)+
			len(cosmosResources)+len(redisResources)+
			len(aksResources)+len(aksPoolResources)+
			len(containerEnvResources)+len(containerAppResources)+
			len(containerGroupResources)+
			len(serviceBusResources)+len(eventHubsResources)+
			len(apimResources)+len(frontDoorResources)+
			len(metricAlertResources)+len(actionGroupResources)+
			len(bastionResources)+len(tmResources)+
			len(dnsResolverResources)+len(dnsRulesetResources)+
			len(routeServerResources)+len(gwConnResources)+
			len(pipPrefixResources)+len(asetResources)+
			// H1
			len(vmssResources)+len(sqlMIResources)+len(sqlMIDBResources)+
			len(sqlElasticPoolResources)+len(sqlVMResources)+
			len(logicResources)+
			len(dataFactoryResources)+len(synapseResources)+len(commSvcResources)+
			len(automationResources)+len(arcMachineResources)+
			len(dcrResources)+len(dceResources)+len(autoscaleResources)+
			// H2
			len(logicAPIConnResources)+len(emailSvcResources)+len(emailDomainResources)+
			len(streamAnalyticsResources)+len(eventGridResources)+len(slotResources))

	allResources = append(allResources, rgResources...)
	allResources = append(allResources, vnetResources...)
	allResources = append(allResources, subnetResources...)
	allResources = append(allResources, nicResources...)
	allResources = append(allResources, nsgResources...)
	allResources = append(allResources, rtResources...)
	allResources = append(allResources, publicIPResources...)
	allResources = append(allResources, lbResources...)
	allResources = append(allResources, peResources...)
	allResources = append(allResources, gwResources...)
	allResources = append(allResources, natGWResources...)
	allResources = append(allResources, fwResources...)
	allResources = append(allResources, appGWResources...)
	allResources = append(allResources, dnsResources...)
	allResources = append(allResources, privateDNSResources...)
	allResources = append(allResources, erResources...)
	allResources = append(allResources, vmResources...)
	allResources = append(allResources, aspResources...)
	allResources = append(allResources, webAppResources...)
	allResources = append(allResources, asgResources...)
	allResources = append(allResources, storageResources...)
	allResources = append(allResources, keyVaultResources...)
	allResources = append(allResources, acrResources...)
	allResources = append(allResources, miResources...)
	allResources = append(allResources, diskResources...)
	allResources = append(allResources, snapshotResources...)
	allResources = append(allResources, aiResources...)
	allResources = append(allResources, laResources...)
	allResources = append(allResources, rsvResources...)
	allResources = append(allResources, bvResources...)
	allResources = append(allResources, sqlServerResources...)
	allResources = append(allResources, sqlDatabaseResources...)
	allResources = append(allResources, pgResources...)
	allResources = append(allResources, mysqlResources...)
	allResources = append(allResources, cosmosResources...)
	allResources = append(allResources, redisResources...)
	allResources = append(allResources, aksResources...)
	allResources = append(allResources, aksPoolResources...)
	allResources = append(allResources, containerEnvResources...)
	allResources = append(allResources, containerAppResources...)
	allResources = append(allResources, containerGroupResources...)
	allResources = append(allResources, serviceBusResources...)
	allResources = append(allResources, eventHubsResources...)
	allResources = append(allResources, apimResources...)
	allResources = append(allResources, frontDoorResources...)
	allResources = append(allResources, metricAlertResources...)
	allResources = append(allResources, actionGroupResources...)
	allResources = append(allResources, bastionResources...)
	allResources = append(allResources, tmResources...)
	allResources = append(allResources, dnsResolverResources...)
	allResources = append(allResources, dnsRulesetResources...)
	allResources = append(allResources, routeServerResources...)
	allResources = append(allResources, gwConnResources...)
	allResources = append(allResources, pipPrefixResources...)
	allResources = append(allResources, asetResources...)
	// H1 resources
	allResources = append(allResources, vmssResources...)
	allResources = append(allResources, sqlMIResources...)
	allResources = append(allResources, sqlMIDBResources...)
	allResources = append(allResources, sqlElasticPoolResources...)
	allResources = append(allResources, sqlVMResources...)
	allResources = append(allResources, logicResources...)
	allResources = append(allResources, dataFactoryResources...)
	allResources = append(allResources, synapseResources...)
	allResources = append(allResources, commSvcResources...)
	allResources = append(allResources, automationResources...)
	allResources = append(allResources, arcMachineResources...)
	allResources = append(allResources, dcrResources...)
	allResources = append(allResources, dceResources...)
	allResources = append(allResources, autoscaleResources...)
	// H2 resources
	allResources = append(allResources, logicAPIConnResources...)
	allResources = append(allResources, emailSvcResources...)
	allResources = append(allResources, emailDomainResources...)
	allResources = append(allResources, streamAnalyticsResources...)
	allResources = append(allResources, eventGridResources...)
	allResources = append(allResources, slotResources...)
	// Merge cross-subscription VNet stubs with deduplication.
	// The same VNet can appear as a remote peer AND as a DNS zone link target,
	// producing duplicate IDs. Keep the first occurrence and skip the rest.
	{
		seen := make(map[string]bool)
		for _, stubs := range [][]sdk.Resource{peeringStubs, gwStubs, dnsVNetStubs} {
			for _, s := range stubs {
				if !seen[s.ID] {
					seen[s.ID] = true
					allResources = append(allResources, s)
				}
			}
		}
	}

	// Backfill resource.description for every resource that doesn't have one.
	// Prefers tags["description"] / tags["Description"]; falls back to "name in rg".
	for i := range allResources {
		if allResources[i].Description == "" {
			rg, _ := allResources[i].Properties["resource_group"].(string)
			allResources[i].Description = deriveDescription(allResources[i].Name, rg, allResources[i].Tags)
		}
	}

	// Attach universal osiris.azure extension envelope (arm_id, kind, sku, identity,
	// timestamps, public_network_access, locks, diag_settings) to every resource.
	for i := range allResources {
		key := strings.ToLower(allResources[i].Provider.NativeID)
		attachUniversalEnvelope(&allResources[i], data.GraphEnvelopes[key])
	}

	// Wire resources to resource groups.
	WireResourcesToResourceGroups(allResources, rgNameToID, rgGroups)

	// Attach group-scope role assignments (RG / subscription / MG) from bulk list.
	// Resource-scope RAs are already on individual graph envelopes and projected by
	// attachUniversalEnvelope above.
	if p.cfg.Purpose == "audit" && len(data.BulkRoleAssignments) > 0 {
		// RG scope: match by full ARM ID (case-insensitive).
		rgIDToName := make(map[string]string, len(data.ResourceGroups))
		for _, rg := range data.ResourceGroups {
			rgIDToName[strings.ToLower(rg.ID)] = strings.ToLower(rg.Name)
		}
		rgGroupIdx := make(map[string]int, len(rgGroups))
		for i, g := range rgGroups {
			rgGroupIdx[strings.ToLower(g.Name)] = i
		}
		// MG scope: match last path segment (mg.Name) to mgGroups by properties.management_group_id.
		mgIDToIdx := make(map[string]int, len(mgAncestors))
		for i, mg := range mgAncestors {
			mgIDToIdx[strings.ToLower(mg.Name)] = i
		}
		subScopeKey := strings.ToLower("/subscriptions/" + sub.SubscriptionID)

		for _, sra := range data.BulkRoleAssignments {
			scopeKey := strings.ToLower(sra.Scope)
			rm := raToMap(sra.RoleAssignment)
			switch {
			case scopeKey == subScopeKey:
				appendGroupRA(&subGroup, rm)
			case strings.HasPrefix(scopeKey, "/providers/"):
				// MG scope: /providers/microsoft.management/managementgroups/<mg-name>
				parts := strings.Split(strings.TrimPrefix(scopeKey, "/"), "/")
				if len(parts) >= 4 {
					if idx, ok := mgIDToIdx[parts[3]]; ok {
						appendGroupRA(&mgGroups[idx], rm)
					}
				}
			default:
				// RG scope
				if rgName, ok := rgIDToName[scopeKey]; ok {
					if idx, ok2 := rgGroupIdx[rgName]; ok2 {
						appendGroupRA(&rgGroups[idx], rm)
					}
				}
			}
		}
	}

	// Attach lock state from GraphEnvelopes to RG groups (Step D).
	if len(data.GraphEnvelopes) > 0 && len(rgGroups) > 0 {
		rgGroupIdx := make(map[string]int, len(rgGroups))
		for i, g := range rgGroups {
			rgGroupIdx[strings.ToLower(g.Name)] = i
		}
		for _, rg := range data.ResourceGroups {
			env := data.GraphEnvelopes[strings.ToLower(rg.ID)]
			if env == nil || len(env.Locks) == 0 {
				continue
			}
			idx, ok := rgGroupIdx[strings.ToLower(rg.Name)]
			if !ok {
				continue
			}
			ext, _ := rgGroups[idx].Extensions[extensionNamespace].(map[string]any)
			if ext == nil {
				ext = map[string]any{}
			}
			locks := make([]map[string]any, len(env.Locks))
			for i, l := range env.Locks {
				lm := map[string]any{"name": l.Name, "level": l.Level}
				if l.Notes != "" {
					lm["notes"] = l.Notes
				}
				locks[i] = lm
			}
			ext["locks"] = locks
			rgGroups[idx].Extensions = map[string]any{extensionNamespace: ext}
		}
	}

	// Wire resource groups as children of the subscription group.
	WireResourceGroupsToSubscription(&subGroup, rgGroups)

	// Collect scope regions from all resources.
	regionSet := map[string]bool{}
	for _, r := range allResources {
		if r.Provider.Region != "" && r.Provider.Region != "global" {
			regionSet[r.Provider.Region] = true
		}
	}
	regions := make([]string, 0, len(regionSet))
	for reg := range regionSet {
		regions = append(regions, reg)
	}

	// Build scope name: "SubscriptionID - SubscriptionName".
	scopeName := sub.SubscriptionID
	if sub.DisplayName != "" {
		scopeName = sub.SubscriptionID + " - " + sub.DisplayName
	}

	// Build scope.
	purpose, err := osirismeta.ParsePurpose(p.cfg.Purpose)
	if err != nil {
		return nil, fmt.Errorf("invalid purpose in config: %w", err)
	}
	scope := sdk.Scope{
		Name:          scopeName,
		Purpose:       purpose.String(),
		Providers:     []string{providerName},
		Accounts:      []string{sub.TenantID},
		Subscriptions: []string{sub.SubscriptionID},
		Regions:       regions,
	}
	if p.target.Environment != "" {
		scope.Environments = []string{p.target.Environment}
	}

	// Assemble the document.
	builder := sdk.NewDocumentBuilder(ctx).
		WithGenerator(generatorName, generatorVersion, generatorURL).
		WithScope(scope)

	for _, r := range allResources {
		builder.AddResource(r)
	}

	var allConns []sdk.Connection
	allConns = append(allConns, peeringConns...)
	allConns = append(allConns, subnetVNetConns...)
	allConns = append(allConns, nsgConns...)
	allConns = append(allConns, rtConns...)
	allConns = append(allConns, nicSubnetConns...)
	allConns = append(allConns, peSubnetConns...)
	allConns = append(allConns, peNICConns...)
	allConns = append(allConns, lbPIPConns...)
	allConns = append(allConns, gwSubnetConns...)
	allConns = append(allConns, gwPIPConns...)
	allConns = append(allConns, natSubnetConns...)
	allConns = append(allConns, natPIPConns...)
	allConns = append(allConns, dnsVNetConns...)
	allConns = append(allConns, nicASGConns...)
	allConns = append(allConns, planAppConns...)
	allConns = append(allConns, webAppSubnetConns...)
	allConns = append(allConns, peWebAppConns...)
	allConns = append(allConns, peStorageConns...)
	allConns = append(allConns, peKeyVaultConns...)
	allConns = append(allConns, peACRConns...)
	allConns = append(allConns, snapshotDiskConns...)
	allConns = append(allConns, diskVMConns...)
	allConns = append(allConns, aiWorkspaceConns...)
	allConns = append(allConns, webAppAIConns...)
	allConns = append(allConns, peRSVConns...)
	allConns = append(allConns, rsvProtectedConns...)
	allConns = append(allConns, bvInstanceConns...)
	allConns = append(allConns, sqlServerDBConns...)
	allConns = append(allConns, peSQLConns...)
	allConns = append(allConns, peCosmosConns...)
	allConns = append(allConns, peRedisConns...)
	allConns = append(allConns, flexSubnetConns...)
	allConns = append(allConns, redisSubnetConns...)
	allConns = append(allConns, aksPoolConns...)
	allConns = append(allConns, aksPoolSubnetConns...)
	allConns = append(allConns, peAKSConns...)
	allConns = append(allConns, envAppConns...)
	allConns = append(allConns, envSubnetConns...)
	allConns = append(allConns, cgSubnetConns...)
	allConns = append(allConns, peServiceBusConns...)
	allConns = append(allConns, peEventHubsConns...)
	allConns = append(allConns, peAPIMConns...)
	allConns = append(allConns, apimSubnetConns...)
	allConns = append(allConns, gwConns...)
	allConns = append(allConns, alertAGConns...)
	allConns = append(allConns, alertScopeConns...)
	allConns = append(allConns, lbSubnetConns...)
	allConns = append(allConns, bastionSubnetConns...)
	allConns = append(allConns, bastionPIPConns...)
	allConns = append(allConns, tmTargetConns...)
	allConns = append(allConns, dnsResolverVNetConns...)
	allConns = append(allConns, dnsRulesetResolverConns...)
	allConns = append(allConns, routeServerConns...)
	allConns = append(allConns, asetVMConns...)
	allConns = append(allConns, vmssSubnetConns...)
	allConns = append(allConns, sqlMISubnetConns...)
	allConns = append(allConns, sqlMIDBConns...)
	allConns = append(allConns, sqlElasticPoolConns...)
	allConns = append(allConns, sqlVMToVMConns...)
	allConns = append(allConns, dcrWorkspaceConns...)
	allConns = append(allConns, autoscaleTargetConns...)
	allConns = append(allConns, emailDomainConns...)
	allConns = append(allConns, webAppSlotConns...)
	allConns = append(allConns, TransformFlowLogConnections(data.FlowLogs, nsgIDMap, storageIDMap)...)

	// Layer 41: Resource -> Log Analytics / Storage diagnostic dependency (audit only).
	// Each diag_settings entry in the graph envelope describes one diagnostic setting
	// that routes logs/metrics to a workspace or storage account. These edges are
	// audit-gated because they require the full diagnostic settings Pass 3 collection.
	if p.cfg.Purpose == "audit" {
		laIDMapLow := make(map[string]string, len(laIDMap))
		for k, v := range laIDMap {
			laIDMapLow[strings.ToLower(k)] = v
		}
		storageIDMapLow := make(map[string]string, len(storageIDMap))
		for k, v := range storageIDMap {
			storageIDMapLow[strings.ToLower(k)] = v
		}
		// Seed seen set from all connections already built so we don't collide
		// with existing edges (e.g. App Insights -> workspace is already in aiWorkspaceConns).
		seenConnIDs := make(map[string]struct{}, len(allConns))
		for _, c := range allConns {
			seenConnIDs[c.ID] = struct{}{}
		}
		for _, r := range allResources {
			armKey := strings.ToLower(r.Provider.NativeID)
			env := data.GraphEnvelopes[armKey]
			if env == nil {
				continue
			}
			for _, d := range env.DiagSettings {
				var targetID string
				if d.WorkspaceID != "" {
					targetID = laIDMapLow[strings.ToLower(d.WorkspaceID)]
				} else if d.StorageID != "" {
					targetID = storageIDMapLow[strings.ToLower(d.StorageID)]
				}
				if targetID == "" || targetID == r.ID {
					continue
				}
				canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
					Type:      "dependency",
					Direction: "forward",
					Source:    r.ID,
					Target:    targetID,
				})
				connID := sdk.BuildConnectionID(canonicalKey, 16)
				if _, dup := seenConnIDs[connID]; dup {
					continue
				}
				seenConnIDs[connID] = struct{}{}
				conn, err := sdk.NewConnection(connID, "dependency", r.ID, targetID)
				if err != nil {
					continue
				}
				conn.Name = d.Name
				conn.Description = "diagnostic settings"
				_ = conn.SetDirection("forward")
				allConns = append(allConns, conn)
			}
		}
	}

	// Backfill connection.tags from source resource.
	// Connections inherit the source resource's tags so graph consumers
	// can filter/colour edges by the same ownership/environment labels they use for nodes.
	if len(allConns) > 0 {
		resourceTagMap := make(map[string]map[string]string, len(allResources))
		for _, r := range allResources {
			if len(r.Tags) > 0 {
				resourceTagMap[r.ID] = r.Tags
			}
		}
		for i := range allConns {
			if allConns[i].Tags == nil {
				if tags, ok := resourceTagMap[allConns[i].Source]; ok {
					allConns[i].Tags = tags
				}
			}
		}
	}

	for _, c := range allConns {
		builder.AddConnection(c)
	}

	for _, g := range mgGroups {
		builder.AddGroup(g)
	}
	builder.AddGroup(subGroup)
	for _, g := range rgGroups {
		builder.AddGroup(g)
	}
	for _, g := range TransformRegionGroups(allResources, sub) {
		builder.AddGroup(g)
	}

	doc, err := builder.Build()
	if err != nil {
		return nil, fmt.Errorf("document build failed: %w", err)
	}

	// Shape the emitted document per OSIRIS JSON spec chapter 13.1.3 based on the declared purpose.
	// Collection itself is always exhaustive; the projection trims fields.
	osirismeta.Project(doc, purpose)

	ctx.Logger.Info("Azure collection complete",
		"subscription", sub.DisplayName,
		"purpose", purpose.String(),
		"resources", len(doc.Topology.Resources),
		"connections", len(doc.Topology.Connections),
		"groups", len(doc.Topology.Groups),
	)

	return doc, nil
}

// CollectedTenantID returns the tenant ID resolved during collection.
// Used by the batch runner to build the output path after collection.
func (p *Producer) CollectedTenantID() string {
	return p.target.TenantID
}

// Run is the entry point called by the CLI dispatcher.
// It receives the arguments after "azure" (e.g. ["-S", "sub-id"]).
func Run(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "--help", "-h", "help":
			printHelp()
			return nil
		case "template":
			return runTemplate(args[1:])
		}
	}

	if err := RunPreflightChecks(defaultLogger()); err != nil {
		return err
	}

	cfg, err := ParseFlags(args)
	if err != nil {
		return err
	}

	// Shared timestamp for the entire batch run so all files land in the same directory.
	cfg.Timestamp = FormatTimestamp(time.Now())

	if cfg.IsBatch() {
		return runBatch(cfg, defaultLogger())
	}

	return runSingle(cfg)
}

// runSingle executes a single-subscription collection and writes to a local file.
// Output filename: microsoft-azure-<timestamp>-<subscription-name>.json
func runSingle(cfg *Config) error {
	target := cfg.Targets[0]
	logger := defaultLogger()

	producer := NewProducer(target, cfg)
	ctx := newSDKContext(cfg)
	ctx.Logger = logger

	doc, err := producer.Collect(ctx)
	if err != nil {
		return fmt.Errorf("collection failed for %s: %w", target.SubscriptionID, err)
	}

	data, err := sdk.MarshalDocument(doc)
	if err != nil {
		return fmt.Errorf("marshal failed: %w", err)
	}

	name := sanitizeFilename(target.SubscriptionName)
	if name == "" {
		name = target.SubscriptionID
	}
	filename := fmt.Sprintf("microsoft-azure-%s-%s.json", cfg.Timestamp, name)

	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", filename, err)
	}
	fmt.Fprintf(os.Stderr, "Saved to %s\n", filename)
	return nil
}

// runBatch executes batch collection across multiple subscriptions.
// Output hierarchy: outputDir/TenantID/timestamp/SubscriptionName.json
func runBatch(cfg *Config, logger *slog.Logger) error {
	logger.Info("starting batch collection",
		"subscriptions", len(cfg.Targets),
		"output", cfg.OutputDir,
		"timestamp", cfg.Timestamp,
	)

	// Pre-fetch management group entities once for the whole batch.
	// az account management-group entities list is tenant-scoped: it returns
	// identical data regardless of which subscription context is active, so
	// one call covers all subscriptions in the same tenant. If this fails, each
	// subscription client falls back to fetching independently.
	if len(cfg.Targets) > 0 {
		tmpClient := NewClient(cfg.Targets[0].SubscriptionID, logger)
		var entities []MGEntity
		if err := tmpClient.queryTenant("account management-group entities list", &entities); err != nil {
			logger.Warn("pre-fetch of management group entities failed, will retry per subscription", "error", err)
		} else {
			cfg.MGEntitiesCache = entities
			logger.Info("pre-fetched management group entities for batch", "entities", len(entities))
		}
	}

	var succeeded, failed int

	for i, target := range cfg.Targets {
		// Brief cooldown between subscriptions to let the OS recycle TCP
		// sockets and file descriptors, preventing connection exhaustion
		// on large batch runs (hundreds of subscriptions).
		if i > 0 {
			time.Sleep(5 * time.Second)
		}

		log := logger.With(
			"subscription", target.SubscriptionID,
			"name", target.SubscriptionName,
		)

		log.Info("collecting")

		producer := NewProducer(target, cfg)
		ctx := sdk.NewContext(&sdk.ProducerConfig{
			SafeFailureMode: cfg.SafeFailureMode,
			Purpose:         cfg.Purpose,
		})
		ctx.Logger = log

		doc, err := producer.Collect(ctx)
		if err != nil {
			log.Error("collection failed", "error", err)
			failed++
			continue
		}

		data, err := sdk.MarshalDocument(doc)
		if err != nil {
			log.Error("marshal failed", "error", err)
			failed++
			continue
		}

		// Determine output path.
		var outPath string
		if cfg.OutputDir != "" {
			// Hierarchical output: outputDir/TenantID/timestamp/Name.json
			tenantID := producer.target.TenantID
			outPath = OutputPath(cfg.OutputDir, tenantID, cfg.Timestamp, target)
		} else {
			// No output dir: save as microsoft-azure-<timestamp>-<name>.json in current directory.
			name := sanitizeFilename(target.SubscriptionName)
			if name == "" {
				name = target.SubscriptionID
			}
			outPath = fmt.Sprintf("microsoft-azure-%s-%s.json", cfg.Timestamp, name)
		}
		if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
			log.Error("creating output path", "error", err, "path", outPath)
			failed++
			continue
		}

		if err := os.WriteFile(outPath, data, 0644); err != nil {
			log.Error("write failed", "error", err, "path", outPath)
			failed++
			continue
		}

		log.Info("written", "path", outPath)
		succeeded++
	}

	if succeeded == 0 {
		return fmt.Errorf("all %d targets failed", failed)
	}

	if failed > 0 {
		logger.Warn("batch completed with failures", "succeeded", succeeded, "failed", failed)
	} else {
		logger.Info("batch completed", "succeeded", succeeded)
	}

	return nil
}

func runTemplate(args []string) error {
	if len(args) == 0 || (args[0] != "--generate" && args[0] != "-g") {
		fmt.Println("Usage: osirisjson-producer azure template --generate")
		return nil
	}

	filename := "azure-template.csv"
	if err := os.WriteFile(filename, []byte(CSVTemplate()), 0644); err != nil {
		return fmt.Errorf("failed to write template: %w", err)
	}
	fmt.Printf("Template saved to %s\n", filename)
	return nil
}

func defaultLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, nil))
}

func newSDKContext(cfg *Config) *sdk.Context {
	return sdk.NewContext(&sdk.ProducerConfig{
		SafeFailureMode: cfg.SafeFailureMode,
		Purpose:         cfg.Purpose,
	})
}

func printHelp() {
	fmt.Print(`osirisjson-producer azure - Microsoft Azure OSIRIS JSON producer

Collects resources from Azure subscriptions via the Azure CLI (az) and
generates OSIRIS JSON documents. Collection is always exhaustive;
the --purpose flag shapes the emitted document per OSIRIS JSON spec chapter 13.1.3:
documentation (default, minimal) or audit (full detail).
Secrets are always redacted regardless of purpose level.

Requires authentication via 'az login' with Reader access to the
target subscriptions.

Each subscription is exported as a self-contained OSIRIS JSON document.
Cross references (e.g. VNet peerings) use deterministic
resource IDs that consumers can correlate across documents.

Usage:
  osirisjson-producer azure [flags]
  osirisjson-producer azure template --generate

Interactive mode (run without flags):
  osirisjson-producer azure
  Discovers all accessible subscriptions and presents a numbered list.
  Supports selection syntax: 1,3,5 or 30-55 or 'all'.

Single subscription (writes to microsoft-azure-<timestamp>-<name>.json):
  -S, --subscription    Azure subscription ID or name

Multiple subscriptions (writes to output directory):
  -S, --subscription    Comma-separated subscription IDs: sub1,sub2,sub3
  --all                 Auto-discover all accessible subscriptions
  -s, --source          CSV file with subscription targets

Common flags:
  -o, --output          Output directory (required for multi/all/CSV mode)
                        Hierarchy: <output>/<TenantID>/<timestamp>/<SubName>.json
  --tenant              Azure AD / Entra ID tenant ID (optional)
  --region              Filter to a specific Azure region (optional)
  --safe-failure-mode   Secret handling: fail-closed (default), log-and-redact, off
` + osirismeta.PurposeHelp() + `

Other:
  osirisjson-producer azure template --generate   Generate a CSV template for batch collection

Prerequisites:
  1. Install Azure CLI: https://learn.microsoft.com/en-us/cli/azure/install-azure-cli
  2. Authenticate: az login
  3. Ensure your RBAC allow Reader access to target subscriptions

Multi-tenant:
  Run the producer once per tenant. Each 'az login' authenticates to one
  tenant. Use 'az login --tenant <tenant-id>' to switch tenants. The
  output hierarchy groups documents by tenant automatically.

Examples:
  # Interactive mode (pick tenant subscriptions from the list)
  osirisjson-producer azure

  # Single subscription ID - minimal documentation output
  osirisjson-producer azure -S a1b2c3d4-e5f6-7890-abcd-ef1234567890

  # Same subscription, full audit-grade output
  osirisjson-producer azure -S a1b2c3d4-e5f6-7890-abcd-ef1234567890 --purpose audit

  # Multiple specific subscriptions IDs
  osirisjson-producer azure -S sub-id-1,sub-id-2,sub-id-3 -o ./output

  # All accessible subscriptions (auto-discover), audit-grade
  osirisjson-producer azure --all --purpose audit -o ./output

  # All subscriptions in a specific tenant
  osirisjson-producer azure --all --tenant f1e2d3c4-b5a6-9078-fedc-ba9876543210 -o ./output

  # Batch from CSV template
  osirisjson-producer azure -s subscriptions.csv -o ./output
`)
}
