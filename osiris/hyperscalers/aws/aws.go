// Package aws implements the Amazon Web Services OSIRIS JSON producer.
// Collects networking and compute resources from AWS accounts via the
// AWS Go SDK v2 and generates an OSIRIS JSON documents.
//
// The OSIRIS JSON producer requires valid AWS credentials (profiles, environment
// variables, IAM roles, or SSO) with ReadOnly access to the target accounts.
//
// Operating modes:
//
//	Single:   osirisjson-producer aws --profile <profile> --region us-east-1
//	Multi:    osirisjson-producer aws --profile <profile> --all-regions -o ./output
//	CSV:      osirisjson-producer aws -s accounts.csv -o ./output
//	Template: osirisjson-producer aws template --generate
//
// Output naming (all modes):
//
//	amazon-aws-<timestamp>-<region>-<account>.json
//
// Each region is a self-contained OSIRIS JSON document. Global resources
// (Route53, Global Accelerator) are merged into the us-east-1 document.
//
// For an introduction to OSIRIS JSON Producer for Amazon Web Services see
// [OSIRIS-JSON-AWS]: https://osirisjson.org/en/docs/producers/hyperscalers/amazon-aws
package aws

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go.osirisjson.org/producers/pkg/osirismeta"
	"go.osirisjson.org/producers/pkg/sdk"
)

// errSessionExpired is a sentinel error returned when AWS credentials have
// expired. Callers should instruct the user to re-authenticate.
var errSessionExpired = errors.New("AWS session expired")

const (
	generatorName    = "osirisjson-producer-aws"
	generatorVersion = "0.1.0"
	generatorURL     = "https://osirisjson.org/en/docs/producers/hyperscalers/amazon-aws"
)

// Producer implements the OSIRIS JSON sdk.Producer interface for AWS.
type Producer struct {
	target AccountTarget
	cfg    *Config
	region string
	client *Client // injectable for dev testing.
}

// NewProducer creates an AWS producer for the given target and region.
func NewProducer(target AccountTarget, region string, cfg *Config) *Producer {
	return &Producer{target: target, region: region, cfg: cfg}
}

// Collect queries AWS via the SDK and builds an OSIRIS JSOM document.
func (p *Producer) Collect(ctx *sdk.Context) (*sdk.Document, error) {
	client := p.client
	if client == nil {
		var err error
		client, err = NewClient(p.target.Profile, p.region, ctx.Logger)
		if err != nil {
			return nil, fmt.Errorf("creating AWS client: %w", err)
		}
		// Seed the pre-configured account ID so ResolveIdentity can fall back to
		// it when enterprise SCPs block regional sts:GetCallerIdentity calls.
		if p.target.AccountID != "" {
			client.accountID = p.target.AccountID
		}
	}

	ctx.Logger.Info("collecting AWS region data",
		"region", p.region,
		"profile", p.target.Profile,
	)

	includeGlobals := p.region == GlobalRegion
	data, err := client.Collect(includeGlobals)
	if err != nil {
		return nil, fmt.Errorf("AWS collection failed: %w", err)
	}

	// Backfill account ID from STS identity.
	if p.target.AccountID == "" && data.Identity.AccountID != "" {
		p.target.AccountID = data.Identity.AccountID
	}
	accountID := data.Identity.AccountID
	region := data.Region

	// Transform AWS data to OSIRIS JSON types.
	vpcResources := TransformVPCs(data.VPCs, data.VPCDNSAttributes, region, accountID)
	subnetResources, subnetIDMap := TransformSubnets(data.Subnets, region, accountID)
	sgResources, sgIDMap := TransformSecurityGroups(data.SecurityGroups, region, accountID)
	eniResources, eniIDMap := TransformNetworkInterfaces(data.NetworkInterfaces, region, accountID)
	rtResources, rtIDMap := TransformRouteTables(data.RouteTables, region, accountID)
	igwResources := TransformInternetGateways(data.InternetGateways, region, accountID)
	natGWResources := TransformNATGateways(data.NATGateways, region, accountID)
	eipResources := TransformElasticIPs(data.ElasticIPs, region, accountID)
	instanceResources := TransformInstances(data.Instances, region, accountID)
	naclResources, naclIDMap := TransformNetworkACLs(data.NetworkACLs, region, accountID)
	vpcEndpointResources := TransformVPCEndpoints(data.VPCEndpoints, region, accountID)
	peeringResources := TransformVPCPeeringConnections(data.VPCPeeringConnections, region, accountID)
	tgwResources := TransformTransitGateways(data.TransitGateways, region, accountID)
	tgwAttResources := TransformTransitGatewayAttachments(data.TransitGatewayAttachments, region, accountID)
	tgwRTResources := TransformTransitGatewayRouteTables(data.TransitGatewayRouteTables, region, accountID)
	vpnGWResources := TransformVPNGateways(data.VPNGateways, region, accountID)
	vpnConnResources := TransformVPNConnections(data.VPNConnections, region, accountID)
	cgwResources := TransformCustomerGateways(data.CustomerGateways, region, accountID)
	dhcpResources := TransformDHCPOptions(data.DHCPOptions, region, accountID)
	eoigwResources := TransformEgressOnlyIGWs(data.EgressOnlyIGWs, region, accountID)
	prefixListResources := TransformManagedPrefixLists(data.ManagedPrefixLists, region, accountID)
	flowLogResources := TransformFlowLogs(data.FlowLogs, region, accountID)
	azResources := TransformAvailabilityZones(data.AvailabilityZones, region, accountID)
	lbV2Resources := TransformLoadBalancersV2(data.LoadBalancersV2, region, accountID)
	tgResources := TransformTargetGroups(data.TargetGroups, region, accountID)
	lbClassicResources := TransformLoadBalancersClassic(data.LoadBalancersClassic, region, accountID)
	dxConnResources := TransformDirectConnectConnections(data.DirectConnectConnections, region, accountID)
	dxGWResources := TransformDirectConnectGateways(data.DirectConnectGateways, region, accountID)
	dxVIFResources := TransformDirectConnectVIFs(data.DirectConnectVIFs, region, accountID)
	nfwResources := TransformNetworkFirewalls(data.NetworkFirewalls, region, accountID)
	resolverRuleResources := TransformResolverRules(data.ResolverRules, region, accountID)
	resolverEndpointResources := TransformResolverEndpoints(data.ResolverEndpoints, region, accountID)

	// Global resources (only for us-east-1).
	var r53Resources []sdk.Resource
	var gaResources []sdk.Resource
	if includeGlobals {
		r53Resources = TransformRoute53HostedZones(data.Route53HostedZones, accountID)
		gaResources = TransformGlobalAccelerators(data.GlobalAccelerators, accountID)
	}

	// Compute orchestration.
	subnetVPCMap := buildSubnetVPCMap(data.Subnets)
	eksClusterResources, eksClusterIDMap := TransformEKSClusters(data.EKSClusters, region, accountID)
	eksNodeGroupResources, eksNodeGroupIDMap := TransformEKSNodeGroups(data.EKSNodeGroups, region, accountID)
	ecsClusterResources, ecsClusterIDMap := TransformECSClusters(data.ECSClusters, region, accountID)
	ecsServiceResources, ecsServiceIDMap := TransformECSServices(data.ECSServices, subnetVPCMap, region, accountID)
	asgResources, asgIDMap := TransformAutoScalingGroups(data.AutoScalingGroups, subnetVPCMap, region, accountID)

	// Managed data.
	rdsInstanceResources, rdsInstanceIDMap := TransformRDSInstances(data.RDSInstances, region, accountID)
	rdsClusterResources, rdsClusterIDMap := TransformRDSClusters(data.RDSClusters, region, accountID)
	rdsSubnetGroupResources, rdsSubnetGroupIDMap := TransformRDSSubnetGroups(data.RDSSubnetGroups, region, accountID)
	dynamoResources, _ := TransformDynamoDBTables(data.DynamoDBTables, region, accountID)
	ecRGResources, _ := TransformElastiCacheReplicationGroups(data.ElastiCacheReplicationGroups, region, accountID)
	ecSGResources, ecSGIDMap := TransformElastiCacheSubnetGroups(data.ElastiCacheSubnetGroups, region, accountID)

	// Serverless and messaging.
	lambdaResources, lambdaIDMap := TransformLambdaFunctions(data.LambdaFunctions, region, accountID)
	sqsResources, _ := TransformSQSQueues(data.SQSQueues, region, accountID)
	kinesisResources, _ := TransformKinesisStreams(data.KinesisStreams, region, accountID)
	mskResources, mskIDMap := TransformMSKClusters(data.MSKClusters, region, accountID)

	// Storage.
	ebsResources, ebsIDMap := TransformEBSVolumes(data.EBSVolumes, region, accountID)
	s3Resources := TransformS3Buckets(data.S3Buckets, region, accountID)
	efsResources, efsIDMap := TransformEFSFileSystems(data.EFSFileSystems, region, accountID)
	fsxResources, fsxIDMap := TransformFSxFileSystems(data.FSxFileSystems, region, accountID)

	// Security and identity.
	kmsResources := TransformKMSKeys(data.KMSKeys, region, accountID)
	smResources := TransformSecretsManagerSecrets(data.SecretsManagerSecrets, region, accountID)
	ecrResources := TransformECRRepositories(data.ECRRepositories, region, accountID)
	wafResources, _ := TransformWAFv2WebACLs(data.WAFv2WebACLs, region, accountID)
	acmResources := TransformACMCertificates(data.ACMCertificates, region, accountID)
	ramResources := TransformRAMResourceShares(data.RAMResourceShares, region, accountID)

	// IAM (global resources only).
	var iamRoleResources []sdk.Resource
	var iamIPResources []sdk.Resource
	var iamOIDCResources []sdk.Resource
	var iamSAMLResources []sdk.Resource
	var iamRoleIDMap map[string]string
	var iamIPIDMap map[string]string
	if includeGlobals {
		iamRoleResources, iamRoleIDMap = TransformIAMRoles(data.IAMRoles, accountID)
		iamIPResources, iamIPIDMap = TransformIAMInstanceProfiles(data.IAMInstanceProfiles, accountID)
		iamOIDCResources = TransformIAMOIDCProviders(data.IAMOIDCProviders, accountID)
		iamSAMLResources = TransformIAMSAMLProviders(data.IAMSAMLProviders, accountID)
	}

	// Extended networking.
	listenerResources, listenerIDMap := TransformELBv2Listeners(data.ELBv2Listeners, region, accountID)
	lagResources, lagIDMap := TransformDirectConnectLAGs(data.DirectConnectLAGs, region, accountID)
	epSvcResources, epSvcIDMap := TransformVPCEndpointServices(data.VPCEndpointServices, region, accountID)
	restAPIResources := TransformAPIGatewayRestAPIs(data.APIGatewayRestAPIs, region, accountID)
	apiv2Resources := TransformAPIGatewayV2APIs(data.APIGatewayV2APIs, region, accountID)
	cfResources := TransformCloudFrontDistributions(data.CloudFrontDistributions, accountID)

	// Observability and backup.
	logGroupResources := TransformCloudWatchLogGroups(data.CloudWatchLogGroups, region, accountID)
	backupVaultResources := TransformBackupVaults(data.BackupVaults, region, accountID)

	// Extended databases.
	docdbClusterResources, docdbClusterIDMap := TransformDocDBClusters(data.DocDBClusters, region, accountID)
	docdbSGResources, docdbSGIDMap := TransformDocDBSubnetGroups(data.DocDBSubnetGroups, region, accountID)
	neptuneClusterResources, neptuneClusterIDMap := TransformNeptuneClusters(data.NeptuneClusters, region, accountID)
	neptuneSGResources, neptuneSGIDMap := TransformNeptuneSubnetGroups(data.NeptuneSubnetGroups, region, accountID)
	redshiftClusterResources, redshiftClusterIDMap := TransformRedshiftClusters(data.RedshiftClusters, region, accountID)
	redshiftSGResources, redshiftSGIDMap := TransformRedshiftSubnetGroups(data.RedshiftSubnetGroups, region, accountID)
	openSearchResources, openSearchIDMap := TransformOpenSearchDomains(data.OpenSearchDomains, region, accountID)
	memdbClusterResources, memdbClusterIDMap := TransformMemoryDBClusters(data.MemoryDBClusters, region, accountID)
	memdbSGResources, memdbSGIDMap := TransformMemoryDBSubnetGroups(data.MemoryDBSubnetGroups, region, accountID)

	// Event-driven and integration.
	snsResources := TransformSNSTopics(data.SNSTopics, region, accountID)
	ebResources := TransformEventBridgeBuses(data.EventBridgeBuses, region, accountID)
	sfnResources := TransformSFNStateMachines(data.SFNStateMachines, region, accountID)

	// Collect all resources.
	var allResources []sdk.Resource
	allResources = append(allResources, vpcResources...)
	allResources = append(allResources, subnetResources...)
	allResources = append(allResources, sgResources...)
	allResources = append(allResources, eniResources...)
	allResources = append(allResources, rtResources...)
	allResources = append(allResources, igwResources...)
	allResources = append(allResources, natGWResources...)
	allResources = append(allResources, eipResources...)
	allResources = append(allResources, instanceResources...)
	allResources = append(allResources, naclResources...)
	allResources = append(allResources, vpcEndpointResources...)
	allResources = append(allResources, peeringResources...)
	allResources = append(allResources, tgwResources...)
	allResources = append(allResources, tgwAttResources...)
	allResources = append(allResources, tgwRTResources...)
	allResources = append(allResources, vpnGWResources...)
	allResources = append(allResources, vpnConnResources...)
	allResources = append(allResources, cgwResources...)
	allResources = append(allResources, dhcpResources...)
	allResources = append(allResources, eoigwResources...)
	allResources = append(allResources, prefixListResources...)
	allResources = append(allResources, flowLogResources...)
	allResources = append(allResources, azResources...)
	allResources = append(allResources, lbV2Resources...)
	allResources = append(allResources, tgResources...)
	allResources = append(allResources, lbClassicResources...)
	allResources = append(allResources, dxConnResources...)
	allResources = append(allResources, dxGWResources...)
	allResources = append(allResources, dxVIFResources...)
	allResources = append(allResources, nfwResources...)
	allResources = append(allResources, resolverRuleResources...)
	allResources = append(allResources, resolverEndpointResources...)
	allResources = append(allResources, r53Resources...)
	allResources = append(allResources, gaResources...)
	allResources = append(allResources, eksClusterResources...)
	allResources = append(allResources, eksNodeGroupResources...)
	allResources = append(allResources, ecsClusterResources...)
	allResources = append(allResources, ecsServiceResources...)
	allResources = append(allResources, asgResources...)
	allResources = append(allResources, rdsInstanceResources...)
	allResources = append(allResources, rdsClusterResources...)
	allResources = append(allResources, rdsSubnetGroupResources...)
	allResources = append(allResources, dynamoResources...)
	allResources = append(allResources, ecRGResources...)
	allResources = append(allResources, ecSGResources...)
	allResources = append(allResources, lambdaResources...)
	allResources = append(allResources, sqsResources...)
	allResources = append(allResources, kinesisResources...)
	allResources = append(allResources, mskResources...)
	allResources = append(allResources, ebsResources...)
	allResources = append(allResources, s3Resources...)
	allResources = append(allResources, efsResources...)
	allResources = append(allResources, fsxResources...)
	allResources = append(allResources, kmsResources...)
	allResources = append(allResources, smResources...)
	allResources = append(allResources, ecrResources...)
	allResources = append(allResources, wafResources...)
	allResources = append(allResources, acmResources...)
	allResources = append(allResources, ramResources...)
	allResources = append(allResources, iamRoleResources...)
	allResources = append(allResources, iamIPResources...)
	allResources = append(allResources, iamOIDCResources...)
	allResources = append(allResources, iamSAMLResources...)
	allResources = append(allResources, listenerResources...)
	allResources = append(allResources, lagResources...)
	allResources = append(allResources, epSvcResources...)
	allResources = append(allResources, restAPIResources...)
	allResources = append(allResources, apiv2Resources...)
	allResources = append(allResources, cfResources...)
	allResources = append(allResources, logGroupResources...)
	allResources = append(allResources, backupVaultResources...)
	allResources = append(allResources, docdbClusterResources...)
	allResources = append(allResources, docdbSGResources...)
	allResources = append(allResources, neptuneClusterResources...)
	allResources = append(allResources, neptuneSGResources...)
	allResources = append(allResources, redshiftClusterResources...)
	allResources = append(allResources, redshiftSGResources...)
	allResources = append(allResources, openSearchResources...)
	allResources = append(allResources, memdbClusterResources...)
	allResources = append(allResources, memdbSGResources...)
	allResources = append(allResources, snsResources...)
	allResources = append(allResources, ebResources...)
	allResources = append(allResources, sfnResources...)

	// Build connections.
	subnetVPCConns := TransformSubnetToVPCConnections(data.Subnets, subnetIDMap, region, accountID)
	eniSubnetConns := TransformENIToSubnetConnections(data.NetworkInterfaces, eniIDMap, subnetIDMap)
	sgENIConns := TransformSGToENIConnections(data.NetworkInterfaces, eniIDMap, sgIDMap)
	naclSubnetConns := TransformNACLToSubnetConnections(data.NetworkACLs, naclIDMap, subnetIDMap)
	rtSubnetConns := TransformRouteTableToSubnetConnections(data.RouteTables, rtIDMap, subnetIDMap)
	natSubnetConns := TransformNATGatewayToSubnetConnections(data.NATGateways, subnetIDMap, region, accountID)
	natEIPConns := TransformNATGatewayToEIPConnections(data.NATGateways, region, accountID)
	igwVPCConns := TransformIGWToVPCConnections(data.InternetGateways, region, accountID)
	peeringConns := TransformVPCPeeringConnectionConns(data.VPCPeeringConnections, region, accountID)
	vpceVPCConns := TransformVPCEndpointToVPCConnections(data.VPCEndpoints, region, accountID)
	vpceSubnetConns := TransformVPCEndpointToSubnetConnections(data.VPCEndpoints, subnetIDMap, region, accountID)
	vpceRTConns := TransformVPCEndpointToRouteTableConnections(data.VPCEndpoints, rtIDMap, region, accountID)
	vpceSGConns := TransformVPCEndpointToSGConnections(data.VPCEndpoints, sgIDMap, region, accountID)
	tgwAttVPCConns := TransformTGWAttachmentToVPCConnections(data.TransitGatewayAttachments, region, accountID)
	tgwAttTGWConns := TransformTGWAttachmentToTGWConnections(data.TransitGatewayAttachments, region, accountID)
	dxVIFGWConns := TransformDXVIFToGatewayConnections(data.DirectConnectVIFs, region, accountID)
	vgwVPCConns := TransformVPNGatewayToVPCConnections(data.VPNGateways, region, accountID)
	vpnConns := TransformVPNConnectionConns(data.VPNConnections, region, accountID)
	dhcpVPCConns := TransformDHCPToVPCConnections(data.VPCs, region, accountID)
	lbTGConns := TransformLBToTargetGroupConnections(data.TargetGroups, region, accountID)

	var allConns []sdk.Connection
	allConns = append(allConns, subnetVPCConns...)
	allConns = append(allConns, eniSubnetConns...)
	allConns = append(allConns, sgENIConns...)
	allConns = append(allConns, naclSubnetConns...)
	allConns = append(allConns, rtSubnetConns...)
	allConns = append(allConns, natSubnetConns...)
	allConns = append(allConns, natEIPConns...)
	allConns = append(allConns, igwVPCConns...)
	allConns = append(allConns, peeringConns...)
	allConns = append(allConns, vpceVPCConns...)
	allConns = append(allConns, vpceSubnetConns...)
	allConns = append(allConns, vpceRTConns...)
	allConns = append(allConns, vpceSGConns...)
	allConns = append(allConns, TransformSGToSGConnections(data.SecurityGroups, sgIDMap)...)
	allConns = append(allConns, tgwAttVPCConns...)
	allConns = append(allConns, tgwAttTGWConns...)
	allConns = append(allConns, dxVIFGWConns...)
	allConns = append(allConns, vgwVPCConns...)
	allConns = append(allConns, vpnConns...)
	allConns = append(allConns, dhcpVPCConns...)
	allConns = append(allConns, lbTGConns...)
	allConns = append(allConns, TransformEKSClusterToSubnetConnections(data.EKSClusters, eksClusterIDMap, subnetIDMap)...)
	allConns = append(allConns, TransformEKSClusterContainsNodeGroupConnections(data.EKSNodeGroups, eksClusterIDMap, eksNodeGroupIDMap)...)
	allConns = append(allConns, TransformEKSNodeGroupToSubnetConnections(data.EKSNodeGroups, eksNodeGroupIDMap, subnetIDMap)...)
	allConns = append(allConns, TransformECSClusterContainsServiceConnections(data.ECSServices, ecsClusterIDMap, ecsServiceIDMap)...)
	allConns = append(allConns, TransformECSServiceToSubnetConnections(data.ECSServices, ecsServiceIDMap, subnetIDMap)...)
	allConns = append(allConns, TransformECSServiceToSGConnections(data.ECSServices, ecsServiceIDMap, sgIDMap)...)
	allConns = append(allConns, TransformASGToSubnetConnections(data.AutoScalingGroups, asgIDMap, subnetIDMap)...)
	allConns = append(allConns, TransformASGToInstanceConnections(data.AutoScalingGroups, asgIDMap, region, accountID)...)
	allConns = append(allConns, TransformEBSVolumeToInstanceConnections(data.EBSVolumes, ebsIDMap, data.Instances, region, accountID)...)
	allConns = append(allConns, TransformEFSToSubnetConnections(data.EFSFileSystems, efsIDMap, subnetIDMap)...)
	allConns = append(allConns, TransformFSxToSubnetConnections(data.FSxFileSystems, fsxIDMap, subnetIDMap)...)
	allConns = append(allConns, TransformLambdaToSubnetConnections(data.LambdaFunctions, lambdaIDMap, subnetIDMap)...)
	allConns = append(allConns, TransformLambdaToSGConnections(data.LambdaFunctions, lambdaIDMap, sgIDMap)...)
	allConns = append(allConns, TransformMSKToSubnetConnections(data.MSKClusters, mskIDMap, subnetIDMap)...)
	allConns = append(allConns, TransformMSKToSGConnections(data.MSKClusters, mskIDMap, sgIDMap)...)
	allConns = append(allConns, TransformRDSInstanceToSubnetGroupConnections(data.RDSInstances, rdsInstanceIDMap, rdsSubnetGroupIDMap)...)
	allConns = append(allConns, TransformRDSInstanceToSGConnections(data.RDSInstances, rdsInstanceIDMap, sgIDMap)...)
	allConns = append(allConns, TransformRDSClusterToSubnetGroupConnections(data.RDSClusters, rdsClusterIDMap, rdsSubnetGroupIDMap)...)
	allConns = append(allConns, TransformRDSClusterToSGConnections(data.RDSClusters, rdsClusterIDMap, sgIDMap)...)
	allConns = append(allConns, TransformRDSClusterContainsInstanceConnections(data.RDSClusters, rdsClusterIDMap, rdsInstanceIDMap)...)
	allConns = append(allConns, TransformRDSSubnetGroupToSubnetConnections(data.RDSSubnetGroups, rdsSubnetGroupIDMap, subnetIDMap)...)
	allConns = append(allConns, TransformElastiCacheSubnetGroupToSubnetConnections(data.ElastiCacheSubnetGroups, ecSGIDMap, subnetIDMap)...)
	allConns = append(allConns, TransformListenerToLBConnections(data.ELBv2Listeners, data.LoadBalancersV2, listenerIDMap, region, accountID)...)
	allConns = append(allConns, TransformLAGContainsConnectionConns(data.DirectConnectLAGs, lagIDMap, region, accountID)...)
	allConns = append(allConns, TransformEndpointServiceToNLBConnections(data.VPCEndpointServices, epSvcIDMap, data.LoadBalancersV2, region, accountID)...)
	allConns = append(allConns, TransformDocDBClusterToSGConnections(data.DocDBClusters, docdbClusterIDMap, sgIDMap)...)
	allConns = append(allConns, TransformDocDBClusterToSubnetGroupConnections(data.DocDBClusters, docdbClusterIDMap, docdbSGIDMap)...)
	allConns = append(allConns, TransformDocDBSubnetGroupToSubnetConnections(data.DocDBSubnetGroups, docdbSGIDMap, subnetIDMap)...)
	allConns = append(allConns, TransformNeptuneClusterToSGConnections(data.NeptuneClusters, neptuneClusterIDMap, sgIDMap)...)
	allConns = append(allConns, TransformNeptuneClusterToSubnetGroupConnections(data.NeptuneClusters, neptuneClusterIDMap, neptuneSGIDMap)...)
	allConns = append(allConns, TransformNeptuneSubnetGroupToSubnetConnections(data.NeptuneSubnetGroups, neptuneSGIDMap, subnetIDMap)...)
	allConns = append(allConns, TransformRedshiftClusterToSGConnections(data.RedshiftClusters, redshiftClusterIDMap, sgIDMap)...)
	allConns = append(allConns, TransformRedshiftClusterToSubnetGroupConnections(data.RedshiftClusters, redshiftClusterIDMap, redshiftSGIDMap)...)
	allConns = append(allConns, TransformRedshiftSubnetGroupToSubnetConnections(data.RedshiftSubnetGroups, redshiftSGIDMap, subnetIDMap)...)
	allConns = append(allConns, TransformOpenSearchToSubnetConnections(data.OpenSearchDomains, openSearchIDMap, subnetIDMap)...)
	allConns = append(allConns, TransformOpenSearchToSGConnections(data.OpenSearchDomains, openSearchIDMap, sgIDMap)...)
	allConns = append(allConns, TransformMemoryDBClusterToSGConnections(data.MemoryDBClusters, memdbClusterIDMap, sgIDMap)...)
	allConns = append(allConns, TransformMemoryDBClusterToSubnetGroupConnections(data.MemoryDBClusters, memdbClusterIDMap, memdbSGIDMap)...)
	allConns = append(allConns, TransformMemoryDBSubnetGroupToSubnetConnections(data.MemoryDBSubnetGroups, memdbSGIDMap, subnetIDMap)...)
	allConns = append(allConns, TransformIAMInstanceProfileToRoleConnections(data.IAMInstanceProfiles, iamIPIDMap, iamRoleIDMap)...)

	// Add stubs for cross-account resources referenced by connections but not
	// collected via the current profile (shared TGWs, cross-account VPC peerings, etc.).
	crossAccountResources := CrossAccountStubs(allConns, allResources)
	if len(crossAccountResources) > 0 {
		ctx.Logger.Info("adding cross-account stubs", "count", len(crossAccountResources))
		allResources = append(allResources, crossAccountResources...)
	}

	// Build groups.
	accountGroup := TransformAccountGroup(accountID)
	vpcGroups, vpcGroupMap := TransformVPCGroups(data.VPCs, region, accountID)
	WireResourcesToVPCGroups(allResources, vpcGroupMap, vpcGroups)
	WireVPCGroupsToAccount(&accountGroup, vpcGroups)

	// Collect scope regions.
	regions := []string{region}
	if includeGlobals {
		regions = append(regions, "global")
	}

	// Build scope name: "AccountID - AccountName" or just AccountID.
	scopeName := accountID
	if p.target.AccountName != "" {
		scopeName = accountID + " - " + p.target.AccountName
	}

	// Parse purpose from config (defaults to documentation).
	purpose, err := osirismeta.ParsePurpose(p.cfg.Purpose)
	if err != nil {
		return nil, fmt.Errorf("invalid purpose in config: %w", err)
	}

	// Build scope.
	scope := sdk.Scope{
		Name:      scopeName,
		Purpose:   purpose.String(),
		Providers: []string{providerName},
		Accounts:  []string{accountID},
		Regions:   regions,
	}
	if p.target.Environment != "" {
		scope.Environments = []string{p.target.Environment}
	}

	// Assemble the document.
	builder := sdk.NewDocumentBuilder(ctx).
		WithGenerator(generatorName, generatorVersion, generatorURL).
		WithScope(scope)

	// Deduplicate by ID before adding to builder. Cross-account data sources
	// (e.g., Aurora Global Databases shared via RAM) can surface the same ARN
	// multiple times across DescribeDB* calls; the builder enforces unique IDs.
	seenResourceIDs := make(map[string]bool, len(allResources))
	for _, r := range allResources {
		if seenResourceIDs[r.ID] {
			ctx.Logger.Warn("skipping duplicate resource ID", "id", r.ID, "type", r.Type)
			continue
		}
		seenResourceIDs[r.ID] = true
		builder.AddResource(r)
	}
	seenConnIDs := make(map[string]bool, len(allConns))
	for _, c := range allConns {
		if seenConnIDs[c.ID] {
			continue
		}
		seenConnIDs[c.ID] = true
		builder.AddConnection(c)
	}
	builder.AddGroup(accountGroup)
	for _, g := range vpcGroups {
		builder.AddGroup(g)
	}

	doc, err := builder.Build()
	if err != nil {
		return nil, fmt.Errorf("document build failed: %w", err)
	}

	// Attach coverage telemetry to metadata (specification chapter 3.3.4 additionalProperties).
	doc.Metadata.Coverage = buildCoverage(allResources, client.CollectedErrors(), includeGlobals)

	// Strip raw SDK bodies unless the caller opted in. In documentation mode
	// osirismeta.Project strips all extensions anyway; this handles audit mode.
	if !p.cfg.IncludeRawBody {
		for i := range doc.Topology.Resources {
			delete(doc.Topology.Resources[i].Extensions, "osiris.aws.sdk")
		}
	}

	// Shape the emitted document per OSIRIS JSON spec chapter 13.1.3.
	// Collection is always exhaustive; the projection trims fields for documentation mode.
	osirismeta.Project(doc, purpose)

	ctx.Logger.Info("AWS collection complete",
		"region", region,
		"account", accountID,
		"purpose", purpose.String(),
		"resources", len(doc.Topology.Resources),
		"connections", len(doc.Topology.Connections),
		"groups", len(doc.Topology.Groups),
	)

	return doc, nil
}

// CollectedAccountID returns the account ID resolved during collection.
func (p *Producer) CollectedAccountID() string {
	return p.target.AccountID
}

// Run is the entry point called by the CLI dispatcher.
// It receives the arguments after "aws" (e.g. ["--profile", "prod", "--region", "us-east-1"]).
func Run(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "--help", "-h", "help":
			printHelp()
			return nil
		case "setup-sso":
			return runSetupSSO(args[1:])
		case "template":
			return runTemplate(args[1:])
		}
	}

	cfg, err := ParseFlags(args)
	if err != nil {
		return err
	}

	// Shared timestamp for the entire batch run.
	cfg.Timestamp = FormatTimestamp(time.Now())

	if cfg.IsBatch() {
		return runBatch(cfg, defaultLogger())
	}

	return runSingle(cfg)
}

// runSingle executes collection for a single target and writes output files.
// For a single region, writes a flat file: amazon-aws-<ts>-<account>-<region>.json
// For multiple regions, creates a folder: amazon-aws-<ts>-<account>/<region>.json
func runSingle(cfg *Config) error {
	target := cfg.Targets[0]
	logger := defaultLogger()

	regions := target.Regions
	if len(regions) == 0 {
		regions = []string{GlobalRegion}
	}

	for _, region := range regions {
		producer := NewProducer(target, region, cfg)
		ctx := newSDKContext(cfg)
		ctx.Logger = logger.With("region", region)

		doc, err := producer.Collect(ctx)
		if err != nil {
			if isExpiredTokenError(err) {
				return fmt.Errorf("%w: %v\n\nRe-authenticate with:\n  aws sso login --profile %s", errSessionExpired, err, target.Profile)
			}
			return fmt.Errorf("collection failed for %s: %w", region, err)
		}

		data, err := sdk.MarshalDocument(doc)
		if err != nil {
			return fmt.Errorf("marshal failed: %w", err)
		}

		name := targetName(target, producer.CollectedAccountID())

		outPath := fmt.Sprintf("amazon-aws-%s-%s-%s.json", cfg.Timestamp, region, name)

		if dir := filepath.Dir(outPath); dir != "." {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("creating directory %s: %w", dir, err)
			}
		}

		if err := os.WriteFile(outPath, data, 0644); err != nil {
			return fmt.Errorf("failed to write %s: %w", outPath, err)
		}
		fmt.Fprintf(os.Stderr, "Saved to %s\n", outPath)
	}
	return nil
}

// preflightCheck verifies AWS credentials for a single profile via STS.
// Returns errSessionExpired when the token is explicitly expired, a generic
// error for other failures, or nil on success.
func preflightCheck(profile string, logger *slog.Logger) error {
	client, err := NewClient(profile, GlobalRegion, logger)
	if err != nil {
		if isExpiredTokenError(err) {
			return fmt.Errorf("%w: %v", errSessionExpired, err)
		}
		return fmt.Errorf("pre-flight check failed: %w", err)
	}
	if _, err := client.ResolveIdentity(); err != nil {
		if isExpiredTokenError(err) {
			return fmt.Errorf("%w: %v", errSessionExpired, err)
		}
		return fmt.Errorf("pre-flight check failed: %w", err)
	}
	return nil
}

// runPreflightBatch walks the target list to find the first profile whose
// credentials are valid. When an expired SSO session is detected it calls
// initiateSSOLogin to refresh credentials interactively before continuing.
// AWS IAM Identity Center (SSO) stores one session token shared across all
// profiles with the same start URL, so a single login refreshes all of them.
// Returns true if at least one profile is (or becomes) valid.
func runPreflightBatch(targets []AccountTarget, logger *slog.Logger) bool {
	seen := map[string]bool{}
	loginAttempted := false
	for _, t := range targets {
		if seen[t.Profile] {
			continue
		}
		seen[t.Profile] = true
		logger.Info("pre-flight credential check", "profile", t.Profile)
		err := preflightCheck(t.Profile, logger)
		if err == nil {
			logger.Info("pre-flight check passed", "profile", t.Profile)
			return true
		}
		if errors.Is(err, errSessionExpired) {
			if !loginAttempted {
				loginAttempted = true
				if loginErr := initiateSSOLogin(t.Profile, logger); loginErr != nil {
					logger.Error("SSO login failed", "profile", t.Profile, "error", loginErr)
					return false
				}
				// Retry after login; the refreshed token covers all profiles
				// in the same IAM Identity Center session.
				if retryErr := preflightCheck(t.Profile, logger); retryErr == nil {
					logger.Info("pre-flight check passed after SSO login", "profile", t.Profile)
					return true
				}
			}
			logger.Warn("pre-flight check: expired session", "profile", t.Profile)
			return true
		}
		// Non-expiry failure (stale profile, no credentials, wrong org):
		// log and try the next profile.
		logger.Warn("pre-flight check: skipping inaccessible profile",
			"profile", t.Profile, "reason", err)
	}
	return false
}

// initiateSSOLogin runs `aws sso login --profile <profile>` with the terminal
// connected so the user can complete the browser authentication. AWS IAM
// Identity Center stores one SSO token that covers all profiles sharing the
// same start URL, so logging in once is enough for an entire batch run.
func initiateSSOLogin(profile string, logger *slog.Logger) error {
	logger.Warn("SSO session expired - starting interactive login", "profile", profile)
	fmt.Fprintf(os.Stderr, "\n  SSO session expired. Opening browser login for profile %q.\n", profile)
	fmt.Fprintf(os.Stderr, "  Complete the authentication in the browser to continue.\n\n")
	cmd := exec.Command("aws", "sso", "login", "--profile", profile)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// runBatch executes batch collection across multiple accounts and regions.
func runBatch(cfg *Config, logger *slog.Logger) error {
	logger.Info("starting batch collection",
		"targets", len(cfg.Targets),
		"output", cfg.OutputDir,
		"timestamp", cfg.Timestamp,
	)

	// pre-flight check: walk profiles to find at least one with valid credentials.
	// Stale or inaccessible profiles are skipped; the main loop handles them.
	if !runPreflightBatch(cfg.Targets, logger) {
		logger.Warn("pre-flight check: no profile could be verified, proceeding anyway")
	}

	var succeeded, failed int
	var lastProfile string
	expiredProfiles := map[string]bool{}

	for _, target := range cfg.Targets {
		// Skip profiles already known to be expired.
		if expiredProfiles[target.Profile] {
			continue
		}

		// Cooldown between different accounts to avoid socket exhaustion.
		if lastProfile != "" && target.Profile != lastProfile {
			time.Sleep(5 * time.Second)
		}
		lastProfile = target.Profile

		regions := target.Regions
		if len(regions) == 0 {
			regions = DefaultRegions
		}

		profileExpired := false
		for _, region := range regions {
			if profileExpired {
				break
			}

			log := logger.With(
				"profile", target.Profile,
				"region", region,
			)

			log.Info("collecting")

			producer := NewProducer(target, region, cfg)
			ctx := sdk.NewContext(&sdk.ProducerConfig{
				SafeFailureMode: cfg.SafeFailureMode,
			})
			ctx.Logger = log

			doc, err := producer.Collect(ctx)
			if err != nil {
				if isExpiredTokenError(err) {
					logger.Warn("session expired, skipping profile",
						"profile", target.Profile,
						"error", err,
					)
					expiredProfiles[target.Profile] = true
					profileExpired = true
					break
				}
				// Collection failed: write an empty-topology file so every
				// (account, region) pair always produces an output file
				// (specification chapter 3.4.8 allows empty topology.resources arrays).
				log.Error("collection failed, writing empty document", "error", err)
				failed++
				// Use the account ID resolved during (partial) collection if
				// available; this avoids "unknown-account" in scope metadata.
				resolvedTarget := target
				if aid := producer.CollectedAccountID(); aid != "" {
					resolvedTarget.AccountID = aid
				}
				doc = buildFallbackDocument(resolvedTarget, region, cfg, err)
				if doc == nil {
					continue
				}
			}

			data, err := sdk.MarshalDocument(doc)
			if err != nil {
				log.Error("marshal failed", "error", err)
				failed++
				continue
			}

			name := targetName(target, producer.CollectedAccountID())

			var outPath string
			if cfg.OutputDir != "" {
				outPath = OutputPath(cfg.OutputDir, name, cfg.Timestamp, region)
			} else {
				outPath = fmt.Sprintf("amazon-aws-%s-%s-%s.json", cfg.Timestamp, region, name)
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
	}

	// Report expired profiles with re-auth instructions.
	if len(expiredProfiles) > 0 {
		fmt.Fprintf(os.Stderr, "\nSession expired for %d profile(s), re-authenticate with:\n", len(expiredProfiles))
		for p := range expiredProfiles {
			fmt.Fprintf(os.Stderr, "  aws sso login --profile %s\n", p)
		}
		fmt.Fprintln(os.Stderr)
	}

	if succeeded == 0 && failed == 0 && len(expiredProfiles) > 0 {
		return fmt.Errorf("%w: all selected profiles have expired sessions", errSessionExpired)
	}
	if succeeded == 0 {
		return fmt.Errorf("all %d targets failed", failed)
	}

	if failed > 0 || len(expiredProfiles) > 0 {
		logger.Warn("batch completed with issues",
			"succeeded", succeeded,
			"failed", failed,
			"expired_profiles", len(expiredProfiles),
		)
	} else {
		logger.Info("batch completed", "succeeded", succeeded)
	}

	return nil
}

// perRegionServices is the list of AWS service families collected in every region.
var perRegionServices = []string{
	"acm", "apigateway", "apigatewayv2", "autoscaling", "backup", "cloudwatchlogs",
	"directconnect", "docdb", "dynamodb", "ec2", "ecr", "ecs", "efs", "eks",
	"elasticache", "elb", "elbv2", "eventbridge", "fsx", "kafka",
	"kinesis", "kms", "lambda", "memorydb", "neptune", "networkfirewall",
	"opensearch", "ram", "rds", "redshift", "route53resolver", "s3",
	"secretsmanager", "sfn", "sns", "sqs", "wafv2",
}

// globalServices is the list of service families collected only for GlobalRegion (us-east-1).
var globalServices = []string{"cloudfront", "globalaccelerator", "iam", "route53"}

// cfNamespaceToService maps a CloudFormation type's service namespace (lowercase) to the
// coverage service name used in perRegionServices / globalServices.
var cfNamespaceToService = map[string]string{
	"apigateway":             "apigateway",
	"apigatewayv2":           "apigatewayv2",
	"autoscaling":            "autoscaling",
	"backup":                 "backup",
	"certificatemanager":     "acm",
	"cloudfront":             "cloudfront",
	"directconnect":          "directconnect",
	"docdb":                  "docdb",
	"dynamodb":               "dynamodb",
	"ec2":                    "ec2",
	"ecr":                    "ecr",
	"ecs":                    "ecs",
	"efs":                    "efs",
	"eks":                    "eks",
	"elasticache":            "elasticache",
	"elasticloadbalancing":   "elb",
	"elasticloadbalancingv2": "elbv2",
	"events":                 "eventbridge",
	"fsx":                    "fsx",
	"globalaccelerator":      "globalaccelerator",
	"iam":                    "iam",
	"kinesis":                "kinesis",
	"kms":                    "kms",
	"lambda":                 "lambda",
	"logs":                   "cloudwatchlogs",
	"memorydb":               "memorydb",
	"msk":                    "kafka",
	"neptune":                "neptune",
	"networkfirewall":        "networkfirewall",
	"opensearchservice":      "opensearch",
	"ram":                    "ram",
	"rds":                    "rds",
	"redshift":               "redshift",
	"route53":                "route53",
	"route53resolver":        "route53resolver",
	"s3":                     "s3",
	"secretsmanager":         "secretsmanager",
	"stepfunctions":          "sfn",
	"sns":                    "sns",
	"sqs":                    "sqs",
	"wafv2":                  "wafv2",
}

// buildCoverage assembles metadata.coverage from collected resources and errors.
func buildCoverage(resources []sdk.Resource, errors []sdk.CoverageError, includeGlobals bool) *sdk.CoverageBlock {
	// services_succeeded: derive from Provider.Type CloudFormation namespace in collected resources.
	// CloudFormation format: "AWS::Service::ResourceType", extract second segment.
	succeededMap := map[string]bool{}
	for _, r := range resources {
		if r.Provider.Type == "" {
			continue
		}
		parts := strings.SplitN(r.Provider.Type, "::", 3)
		if len(parts) == 3 {
			ns := strings.ToLower(parts[1])
			if svc, ok := cfNamespaceToService[ns]; ok {
				succeededMap[svc] = true
			} else {
				succeededMap[ns] = true
			}
		}
	}

	attempted := make([]string, len(perRegionServices))
	copy(attempted, perRegionServices)
	if includeGlobals {
		attempted = append(attempted, globalServices...)
		sort.Strings(attempted)
	}

	succeeded := make([]string, 0, len(succeededMap))
	for s := range succeededMap {
		succeeded = append(succeeded, s)
	}
	sort.Strings(succeeded)

	return &sdk.CoverageBlock{
		ServicesAttempted: attempted,
		ServicesSucceeded: succeeded,
		Errors:            errors,
	}
}

// buildFallbackDocument returns a minimal spec-valid OSIRIS JSON document with an
// empty topology. Used when collection fails so every (account, region) pair
// always produces an output file. The collection error is surfaced in
// metadata.coverage.errors so consumers can distinguish "genuinely empty"
// from "collection failed".
func buildFallbackDocument(target AccountTarget, region string, cfg *Config, collectionErr error) *sdk.Document {
	accountID := target.AccountID
	if accountID == "" {
		accountID = "unknown-account"
	}
	purpose, _ := osirismeta.ParsePurpose(cfg.Purpose)
	scope := sdk.Scope{
		Name:      accountID,
		Purpose:   purpose.String(),
		Providers: []string{providerName},
		Accounts:  []string{accountID},
		Regions:   []string{region},
	}
	if target.Environment != "" {
		scope.Environments = []string{target.Environment}
	}
	// Use Off mode: empty topology has nothing to scan.
	sdkCtx := sdk.NewContext(&sdk.ProducerConfig{SafeFailureMode: sdk.Off})
	doc, err := sdk.NewDocumentBuilder(sdkCtx).
		WithGenerator(generatorName, generatorVersion, generatorURL).
		WithScope(scope).
		Build()
	if err != nil {
		return nil
	}

	// Always emit a coverage block so consumers can see what was attempted
	// and why the topology is empty (specification chapter 3.3.4 additionalProperties: true).
	attempted := make([]string, len(perRegionServices))
	copy(attempted, perRegionServices)
	var coverageErrors []sdk.CoverageError
	if collectionErr != nil {
		coverageErrors = []sdk.CoverageError{{
			Region:       region,
			Resource:     "collection",
			ErrorMessage: collectionErr.Error(),
		}}
	}
	doc.Metadata.Coverage = &sdk.CoverageBlock{
		ServicesAttempted: attempted,
		ServicesSucceeded: []string{},
		Errors:            coverageErrors,
	}
	return doc
}

func runTemplate(args []string) error {
	if len(args) == 0 || (args[0] != "--generate" && args[0] != "-g") {
		fmt.Println("Usage: osirisjson-producer aws template --generate")
		return nil
	}

	filename := "aws-template.csv"
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
	})
}

// targetName returns a filesystem-safe display name for an account target.
// Prefers AccountName, falls back to Profile, then AccountID.
// Strips common IAM role suffixes (_ReadOnlyAccess, _mgt-glbl-msp-networkadmin, etc.)
// that AWS SSO appends to profile names but add no value to filenames.
func targetName(target AccountTarget, accountID string) string {
	name := target.AccountName
	if name == "" {
		name = target.Profile
	}
	if name == "" {
		name = accountID
	}
	if name == "" {
		name = "unknown"
	}
	name = stripIAMSuffix(name)
	return sanitizeFilename(name)
}

// stripIAMSuffix removes trailing IAM role name suffixes that AWS SSO appends
// to CLI profile names (e.g. "myaccount_ReadOnlyAccess" -> "myaccount").
func stripIAMSuffix(name string) string {
	suffixes := []string{
		"_ReadOnlyAccess",
		"_AdministratorAccess",
		"_PowerUserAccess",
		"_ViewOnlyAccess",
		"_mgt-glbl-msp-networkadmin",
	}
	lower := strings.ToLower(name)
	for _, s := range suffixes {
		if strings.HasSuffix(lower, strings.ToLower(s)) {
			return name[:len(name)-len(s)]
		}
	}
	return name
}

func printHelp() {
	fmt.Print(`osirisjson-producer aws - Amazon Web Services OSIRIS JSON producer

Collects resources from AWS accounts via the AWS Go SDK v2 and generates 
OSIRIS JSON documents. Collection is always exhaustive; the --purpose flag 
shapes the emitted document per OSIRIS JSON spec chapter 13.1.3: documentation
(default, minimal) or audit (full detail). Secrets redacted regardless of purpose level.

Requires valid AWS credentials (profiles, environment variables, IAM
roles, or SSO) with ReadOnly access to the target accounts.

Each region is exported as a self-contained OSIRIS JSON document.
Global resources (Route53, Global Accelerator) are merged into the
us-east-1 document.

Usage:
  osirisjson-producer aws [flags]
  osirisjson-producer aws setup-sso --start-url <URL> [--region <region>]
  osirisjson-producer aws template --generate

Interactive mode (run without flags):
  osirisjson-producer aws
  Discovers AWS CLI profiles from ~/.aws/config and ~/.aws/credentials,
  presents a numbered list, and lets you select profiles and regions.
  Supports selection syntax: 1,3,5 or 30-55 or 'all'.

Single or multiple regions (writes amazon-aws-<timestamp>-<region>-<name>.json per region):
  -P, --profile       AWS CLI profile name
  -R, --region        AWS region or comma-separated list: us-east-1,eu-west-1
  --all-regions       Iterate all default AWS regions (17 regions)

Batch mode (multiple accounts from CSV):
  -s, --source        CSV file with account targets
  -o, --output        Output directory (writes amazon-aws-<timestamp>-<region>-<name>.json per region)

Common flags:
  --safe-failure-mode Secret handling: fail-closed (default), log-and-redact, off
` + osirismeta.PurposeHelp() + `
  --include-raw-body    When combined with --purpose audit, attach the full AWS SDK
                          response body for each resource under
                          extensions["osiris.aws.sdk"].body (JSON string). Lossless
                          fallback for any field not yet promoted to a typed
                          properties entry. No effect in documentation mode
                          (all extensions are stripped by osirismeta.Project).

SSO setup (for IAM Identity Center / AWS SSO users):
  osirisjson-producer aws setup-sso --start-url https://myorg.awsapps.com/start
  Discovers all accounts and roles, writes profiles to ~/.aws/config
  automatically. SSO region is auto-detected (or pass --region to skip).

Other:
  osirisjson-producer aws template --generate   Generate a CSV template

Prerequisites:
  1. Configure AWS credentials:
     - aws configure --profile <name> (e.g. aws configure --profile prod)
     - aws configure sso
     - osirisjson-producer aws setup-sso --start-url <URL>
  2. Ensure ReadOnly access to target accounts

Examples:

  # SSO automatic setup for large volume accounts
  osirisjson-producer aws setup-sso --start-url https://myorg.awsapps.com/start

  # Interactive mode (pick profiles and regions)
  osirisjson-producer aws

  # Single region
  osirisjson-producer aws --profile prod --region us-east-1

  # All regions for an account
  osirisjson-producer aws --profile prod --all-regions

  # Specific regions
  osirisjson-producer aws --profile prod --region us-east-1,eu-west-1

  # Batch from CSV
  osirisjson-producer aws -s accounts.csv -o ./output
`)
}
