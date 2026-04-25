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
// "[OSIRIS-JSON-AZURE]."
//
// [OSIRIS-JSON-AZURE]: https://osirisjson.org/en/docs/producers/hyperscalers/microsoft-azure
package azure

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"go.osirisjson.org/producers/pkg/osirismeta"
	"go.osirisjson.org/producers/pkg/sdk"
)

const (
	generatorName    = "osirisjson-producer-azure"
	generatorVersion = "0.4.0"
	generatorURL     = "https://osirisjson.org"
)

// Producer implements the OSIRIS sdk.Producer interface for Azure.
type Producer struct {
	target SubscriptionTarget
	cfg    *Config
	client *Client // injectable for testing.
}

// NewProducer creates an Azure producer for the given subscription target.
func NewProducer(target SubscriptionTarget, cfg *Config) *Producer {
	return &Producer{target: target, cfg: cfg}
}

// Collect queries Azure via the CLI and builds an OSIRIS document.
func (p *Producer) Collect(ctx *sdk.Context) (*sdk.Document, error) {
	client := p.client
	if client == nil {
		client = NewClient(p.target.SubscriptionID, ctx.Logger)
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
	subnetResources, subnetIDMap := TransformSubnets(data.Subnets, sub)
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

	// Build ID maps for connection wiring.
	vnetIDMap := BuildVNetIDMap(data.VirtualNetworks)
	publicIPIDMap := BuildPublicIPIDMap(data.PublicIPs)
	peIDMap := BuildPrivateEndpointIDMap(data.PrivateEndpoints)
	vmIDMap := BuildVMIDMap(data.VirtualMachines)

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
	// Layer 6: Load balancer frontend -> public IP
	lbPIPConns := TransformLBFrontendToPublicIPConnections(data.LoadBalancers, publicIPIDMap)
	//
	// Layer 7: VNet gateways (ExpressRoute/VPN ingress)
	gwSubnetConns := TransformVNetGatewayToSubnetConnections(data.VNetGateways, subnetIDMap)
	gwPIPConns := TransformVNetGatewayToPublicIPConnections(data.VNetGateways, publicIPIDMap)
	//
	// Layer 8: NAT gateways (outbound SNAT)
	natSubnetConns := TransformNATGatewayToSubnetConnections(data.NATGateways, subnetIDMap)
	natPIPConns := TransformNATGatewayToPublicIPConnections(data.NATGateways, publicIPIDMap)
	//
	// Layer 9: Private DNS zone -> VNet links
	dnsVNetConns := TransformPrivateDNSToVNetConnections(data.PrivateDNSZones, vnetIDMap)
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

	// Build resource group resources (container.resourcegroup per OSIRIS JSON specification Appendix C.5).
	rgResources := TransformResourceGroupResources(data.ResourceGroups, sub)

	// Build groups.
	subGroup := TransformSubscriptionGroup(sub)
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
			len(apimResources)+len(frontDoorResources))

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
	allResources = append(allResources, peeringStubs...)
	allResources = append(allResources, gwStubs...)

	// Wire resources to resource groups.
	WireResourcesToResourceGroups(allResources, rgNameToID, rgGroups)

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
	for _, c := range allConns {
		builder.AddConnection(c)
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
