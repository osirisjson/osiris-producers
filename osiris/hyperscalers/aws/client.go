// client.go - AWS SDK v2 wrapper for the AWS OSIRIS JSON producer.
// Uses the AWS Go SDK v2 to collect networking and compute resources from
// an AWS account and region. Requires valid AWS credentials (profiles,
// environment variables, IAM roles, or SSO).
//
// The client fetches all resource types that appear in real AWS production
// environments: VPCs, subnets, security groups, ENIs, route tables, internet
// gateways, NAT gateways, elastic IPs, load balancers, VPC endpoints, transit
// gateways, Direct Connect, VPN, network firewalls, EC2 instances and more.
//
// For an introduction to OSIRIS JSON Producer for Amazon Web Services see:
// [OSIRIS-JSON-AWS]: https://osirisjson.org/en/docs/producers/hyperscalers/amazon-aws

package aws

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/acm"
	acmtypes "github.com/aws/aws-sdk-go-v2/service/acm/types"
	"github.com/aws/aws-sdk-go-v2/service/apigateway"
	apigwtypes "github.com/aws/aws-sdk-go-v2/service/apigateway/types"
	"github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	apigwv2types "github.com/aws/aws-sdk-go-v2/service/apigatewayv2/types"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
	astypes "github.com/aws/aws-sdk-go-v2/service/autoscaling/types"
	"github.com/aws/aws-sdk-go-v2/service/backup"
	backuptypes "github.com/aws/aws-sdk-go-v2/service/backup/types"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront"
	cftypes "github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	cwlogstypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/aws/aws-sdk-go-v2/service/directconnect"
	dctypes "github.com/aws/aws-sdk-go-v2/service/directconnect/types"
	"github.com/aws/aws-sdk-go-v2/service/docdb"
	docdbtypes "github.com/aws/aws-sdk-go-v2/service/docdb/types"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dynamodbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	ecrtypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/aws/aws-sdk-go-v2/service/efs"
	efstypes "github.com/aws/aws-sdk-go-v2/service/efs/types"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	elasticachetypes "github.com/aws/aws-sdk-go-v2/service/elasticache/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing"
	elbtypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	ebtypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	"github.com/aws/aws-sdk-go-v2/service/fsx"
	fsxtypes "github.com/aws/aws-sdk-go-v2/service/fsx/types"
	"github.com/aws/aws-sdk-go-v2/service/globalaccelerator"
	gatypes "github.com/aws/aws-sdk-go-v2/service/globalaccelerator/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/aws-sdk-go-v2/service/kafka"
	kafkatypes "github.com/aws/aws-sdk-go-v2/service/kafka/types"
	"github.com/aws/aws-sdk-go-v2/service/kinesis"
	kinesistypes "github.com/aws/aws-sdk-go-v2/service/kinesis/types"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	kmstypes "github.com/aws/aws-sdk-go-v2/service/kms/types"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"github.com/aws/aws-sdk-go-v2/service/memorydb"
	memorydbtypes "github.com/aws/aws-sdk-go-v2/service/memorydb/types"
	"github.com/aws/aws-sdk-go-v2/service/neptune"
	neptunetypes "github.com/aws/aws-sdk-go-v2/service/neptune/types"
	"github.com/aws/aws-sdk-go-v2/service/networkfirewall"
	nfwtypes "github.com/aws/aws-sdk-go-v2/service/networkfirewall/types"
	"github.com/aws/aws-sdk-go-v2/service/opensearch"
	openstypes "github.com/aws/aws-sdk-go-v2/service/opensearch/types"
	"github.com/aws/aws-sdk-go-v2/service/ram"
	ramtypes "github.com/aws/aws-sdk-go-v2/service/ram/types"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/aws/aws-sdk-go-v2/service/redshift"
	redshifttypes "github.com/aws/aws-sdk-go-v2/service/redshift/types"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	r53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
	"github.com/aws/aws-sdk-go-v2/service/route53resolver"
	r53rtypes "github.com/aws/aws-sdk-go-v2/service/route53resolver/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	smtypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
	"github.com/aws/aws-sdk-go-v2/service/sfn"
	sfntypes "github.com/aws/aws-sdk-go-v2/service/sfn/types"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	snstypes "github.com/aws/aws-sdk-go-v2/service/sns/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/aws-sdk-go-v2/service/wafv2"
	wafv2types "github.com/aws/aws-sdk-go-v2/service/wafv2/types"

	"go.osirisjson.org/producers/pkg/sdk"
)

// EKSNodeGroupEntry pairs a node group with its cluster name.
// DescribeNodegroup embeds the cluster name but ListNodegroups does not,
// so we carry it through collection to make transform lookups O(1).
type EKSNodeGroupEntry struct {
	ClusterName string
	Nodegroup   ekstypes.Nodegroup
}

// CallerIdentity carries the resolved account identity from STS.
type CallerIdentity struct {
	AccountID string
	Arn       string
	UserID    string
}

// RegionData holds all collected AWS resources for a single region.
type RegionData struct {
	Identity                         CallerIdentity
	Region                           string
	VPCs                             []ec2types.Vpc
	VPCDNSAttributes                 map[string]map[string]bool // vpcID -> {"enableDnsHostnames", "enableDnsSupport"}
	Subnets                          []ec2types.Subnet
	SecurityGroups                   []ec2types.SecurityGroup
	NetworkInterfaces                []ec2types.NetworkInterface
	RouteTables                      []ec2types.RouteTable
	InternetGateways                 []ec2types.InternetGateway
	NATGateways                      []ec2types.NatGateway
	ElasticIPs                       []ec2types.Address
	Instances                        []ec2types.Instance
	NetworkACLs                      []ec2types.NetworkAcl
	VPCEndpoints                     []ec2types.VpcEndpoint
	VPCPeeringConnections            []ec2types.VpcPeeringConnection
	TransitGateways                  []ec2types.TransitGateway
	TransitGatewayAttachments        []ec2types.TransitGatewayAttachment
	TransitGatewayRouteTables        []ec2types.TransitGatewayRouteTable
	TransitGatewayPeeringAttachments []ec2types.TransitGatewayPeeringAttachment
	VPNGateways                      []ec2types.VpnGateway
	VPNConnections                   []ec2types.VpnConnection
	CustomerGateways                 []ec2types.CustomerGateway
	DHCPOptions                      []ec2types.DhcpOptions
	EgressOnlyIGWs                   []ec2types.EgressOnlyInternetGateway
	ManagedPrefixLists               []ec2types.ManagedPrefixList
	FlowLogs                         []ec2types.FlowLog
	AvailabilityZones                []ec2types.AvailabilityZone
	LoadBalancersV2                  []elbv2types.LoadBalancer
	TargetGroups                     []elbv2types.TargetGroup
	LoadBalancersClassic             []elbtypes.LoadBalancerDescription
	DirectConnectConnections         []dctypes.Connection
	DirectConnectGateways            []dctypes.DirectConnectGateway
	DirectConnectVIFs                []dctypes.VirtualInterface
	NetworkFirewalls                 []nfwtypes.FirewallMetadata
	ResolverRules                    []r53rtypes.ResolverRule
	ResolverEndpoints                []r53rtypes.ResolverEndpoint
	// Global resources (only populated for GlobalRegion).
	Route53HostedZones  []r53types.HostedZone
	GlobalAccelerators  []gatypes.Accelerator
	IAMRoles            []iamtypes.Role
	IAMInstanceProfiles []iamtypes.InstanceProfile
	IAMOIDCProviders    []iamtypes.OpenIDConnectProviderListEntry
	IAMSAMLProviders    []iamtypes.SAMLProviderListEntry

	// Compute orchestration.
	EKSClusters       []ekstypes.Cluster
	EKSNodeGroups     []EKSNodeGroupEntry
	ECSClusters       []ecstypes.Cluster
	ECSServices       []ecstypes.Service
	AutoScalingGroups []astypes.AutoScalingGroup

	// Managed data.
	RDSInstances                 []rdstypes.DBInstance
	RDSClusters                  []rdstypes.DBCluster
	RDSSubnetGroups              []rdstypes.DBSubnetGroup
	DynamoDBTables               []dynamodbtypes.TableDescription
	ElastiCacheReplicationGroups []elasticachetypes.ReplicationGroup
	ElastiCacheSubnetGroups      []elasticachetypes.CacheSubnetGroup

	// Serverless and messaging.
	LambdaFunctions []lambdatypes.FunctionConfiguration
	SQSQueues       []SQSQueueInfo
	KinesisStreams  []kinesistypes.StreamDescriptionSummary
	MSKClusters     []kafkatypes.ClusterInfo

	// Storage.
	EBSVolumes     []ec2types.Volume
	S3Buckets      []S3BucketInfo
	EFSFileSystems []EFSFileSystemEntry
	FSxFileSystems []fsxtypes.FileSystem

	// Security and identity.
	KMSKeys               []kmstypes.KeyMetadata
	SecretsManagerSecrets []smtypes.SecretListEntry
	ECRRepositories       []ecrtypes.Repository
	WAFv2WebACLs          []WAFv2WebACLEntry
	ACMCertificates       []acmtypes.CertificateDetail
	RAMResourceShares     []ramtypes.ResourceShare

	// Extended networking.
	ELBv2Listeners      []elbv2types.Listener
	DirectConnectLAGs   []dctypes.Lag
	VPCEndpointServices []ec2types.ServiceConfiguration
	APIGatewayRestAPIs  []apigwtypes.RestApi
	APIGatewayV2APIs    []apigwv2types.Api
	// CloudFrontDistributions is global (populated for us-east-1 only).
	CloudFrontDistributions []cftypes.DistributionSummary

	// Observability and backup.
	CloudWatchLogGroups []cwlogstypes.LogGroup
	BackupVaults        []backuptypes.BackupVaultListMember

	// Extended databases.
	DocDBClusters        []docdbtypes.DBCluster
	DocDBSubnetGroups    []docdbtypes.DBSubnetGroup
	NeptuneClusters      []neptunetypes.DBCluster
	NeptuneSubnetGroups  []neptunetypes.DBSubnetGroup
	RedshiftClusters     []redshifttypes.Cluster
	RedshiftSubnetGroups []redshifttypes.ClusterSubnetGroup
	OpenSearchDomains    []openstypes.DomainStatus
	MemoryDBClusters     []memorydbtypes.Cluster
	MemoryDBSubnetGroups []memorydbtypes.SubnetGroup

	// Event-driven and integration.
	SNSTopics        []snstypes.Topic
	EventBridgeBuses []ebtypes.EventBus
	SFNStateMachines []sfntypes.StateMachineListItem
}

// SQSQueueInfo pairs a queue URL with its resolved attributes.
// The SQS API does not return a unified queue object - ListQueues gives URLs
// and GetQueueAttributes gives the rest (ARN, type, depth, etc.).
type SQSQueueInfo struct {
	URL        string
	Attributes map[string]string
}

// S3BucketInfo holds the name, resolved region, and optional tags for an S3 bucket.
type S3BucketInfo struct {
	Name                string
	Region              string
	Tags                map[string]string
	Versioning          string // "Enabled", "Suspended", or "" (never enabled)
	EncryptionAlgorithm string // "AES256", "aws:kms", or ""
	EncryptionKeyARN    string // KMS key ARN when EncryptionAlgorithm == "aws:kms"
	BlockPublicAccess   bool   // true when all four public-access-block settings are enabled
}

// EFSFileSystemEntry pairs an EFS file system with its mount targets so
// subnet connections can be built without a second pass over the data.
type EFSFileSystemEntry struct {
	FileSystem   efstypes.FileSystemDescription
	MountTargets []efstypes.MountTargetDescription
}

// WAFv2WebACLEntry pairs a WAFv2 web ACL with the ARNs of resources it protects
// (ALBs, API Gateways, etc.) from ListResourcesForWebACL.
type WAFv2WebACLEntry struct {
	WebACL                 wafv2types.WebACL
	AssociatedResourceARNs []string
}

// Client wraps the AWS SDK v2 to collect resources for a region.
type Client struct {
	accountID     string
	region        string
	profile       string
	awsCfg        aws.Config
	logger        *slog.Logger
	collectErrors []sdk.CoverageError
}

// NewClient creates a new AWS SDK v2 client for the given profile and region.
func NewClient(profile, region string, logger *slog.Logger) (*Client, error) {
	var opts []func(*awsconfig.LoadOptions) error
	if profile != "" {
		opts = append(opts, awsconfig.WithSharedConfigProfile(profile))
	}
	if region != "" {
		opts = append(opts, awsconfig.WithRegion(region))
	}

	cfg, err := awsconfig.LoadDefaultConfig(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("loading AWS config: %w", err)
	}

	return &Client{
		region:  region,
		profile: profile,
		awsCfg:  cfg,
		logger:  logger,
	}, nil
}

// ResolveIdentity calls STS GetCallerIdentity to resolve the account ID.
// Uses the global STS endpoint (us-east-1) to avoid enterprise SCPs that
// restrict regional sts:GetCallerIdentity calls while allowing the global one.
func (c *Client) ResolveIdentity() (CallerIdentity, error) {
	stsClient := sts.NewFromConfig(c.awsCfg, func(o *sts.Options) {
		o.Region = GlobalRegion
	})
	out, err := stsClient.GetCallerIdentity(context.Background(), &sts.GetCallerIdentityInput{})
	if err != nil {
		if c.accountID != "" {
			// STS failed but we have a pre-configured account ID (e.g. from the
			// CSV target). This can happen when enterprise SCPs restrict STS even
			// on the global endpoint. Log a warning and continue with collection.
			c.logger.Warn("STS identity resolution failed, using pre-configured account ID",
				"account_id", c.accountID, "error", err)
			return CallerIdentity{AccountID: c.accountID}, nil
		}
		return CallerIdentity{}, fmt.Errorf("STS GetCallerIdentity: %w", err)
	}
	id := CallerIdentity{
		AccountID: aws.ToString(out.Account),
		Arn:       aws.ToString(out.Arn),
		UserID:    aws.ToString(out.UserId),
	}
	c.accountID = id.AccountID
	return id, nil
}

// isExpiredTokenError returns true if the error indicates expired or invalid
// AWS credentials (SSO session expired, temporary creds expired, etc.).
func isExpiredTokenError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "ExpiredToken") ||
		strings.Contains(s, "ExpiredTokenException") ||
		strings.Contains(s, "RequestExpired") ||
		strings.Contains(s, "InvalidIdentityToken") ||
		strings.Contains(s, "UnrecognizedClientException") ||
		strings.Contains(s, "security token included in the request is expired") ||
		strings.Contains(s, "security token included in the request is invalid") ||
		strings.Contains(s, "SSO session") ||
		strings.Contains(s, "token has expired")
}

// isPermissionDeniedError returns true if the error is an IAM permission denial
// (UnauthorizedOperation, AccessDenied, 403). These occur on restricted IAM roles
// and should be logged at Debug rather than Warn - they are expected, not transient.
func isPermissionDeniedError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "UnauthorizedOperation") ||
		strings.Contains(s, "AccessDenied") ||
		strings.Contains(s, "AccessDeniedException") ||
		strings.Contains(s, "AuthorizationError") ||
		strings.Contains(s, "is not authorized to perform")
}

// logCollectErr logs a resource collection failure and records it for coverage telemetry.
// RBAC/permission denials (expected on restricted IAM roles) go to Debug;
// all other failures go to Warn.
func (c *Client) logCollectErr(resource string, err error) {
	if isPermissionDeniedError(err) {
		c.logger.Debug("permission denied collecting resource", "resource", resource, "error", err)
	} else {
		c.logger.Warn("failed to collect "+resource, "error", err)
	}
	c.collectErrors = append(c.collectErrors, sdk.CoverageError{
		Region:       c.region,
		Resource:     resource,
		ErrorMessage: err.Error(),
	})
}

// CollectedErrors returns all resource collection failures recorded during the last Collect call.
func (c *Client) CollectedErrors() []sdk.CoverageError {
	return c.collectErrors
}

// Collect fetches all networking and compute resources for the configured region.
func (c *Client) Collect(includeGlobals bool) (*RegionData, error) {
	data := &RegionData{Region: c.region}

	// Resolve identity first.
	id, err := c.ResolveIdentity()
	if err != nil {
		return nil, err
	}
	data.Identity = id

	ec2Client := ec2.NewFromConfig(c.awsCfg)

	// Collect all resource types. Partial failures are logged and skipped.
	c.collectEC2Resources(ec2Client, data)
	c.collectELBResources(data)
	c.collectDirectConnectResources(data)
	c.collectNetworkFirewallResources(data)
	c.collectResolverResources(data)
	c.collectComputeOrchestrationResources(data)
	c.collectManagedDataResources(data)
	c.collectServerlessMessagingResources(data)
	c.collectStorageResources(data)
	c.collectSecurityIdentityResources(data)
	c.collectNetworkingExtendedResources(data)
	c.collectObservabilityResources(data)
	c.collectExtendedDatabaseResources(data)
	c.collectEventDrivenResources(data)

	if includeGlobals {
		c.collectGlobalResources(data)
	}

	return data, nil
}

// collectEC2Resources fetches all EC2 networking resources.
func (c *Client) collectEC2Resources(ec2Client *ec2.Client, data *RegionData) {
	ctx := context.Background()

	// VPCs
	if out, err := ec2Client.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{}); err != nil {
		c.logCollectErr("VPCs", err)
	} else {
		data.VPCs = out.Vpcs
		c.logger.Info("collected", "type", "VPCs", "count", len(out.Vpcs))

		// Fetch DNS attributes per VPC (2 calls each: enableDnsHostnames + enableDnsSupport).
		if len(out.Vpcs) > 0 {
			data.VPCDNSAttributes = make(map[string]map[string]bool, len(out.Vpcs))
			for _, v := range out.Vpcs {
				vid := aws.ToString(v.VpcId)
				attrs := map[string]bool{}
				if r, err := ec2Client.DescribeVpcAttribute(ctx, &ec2.DescribeVpcAttributeInput{
					VpcId:     aws.String(vid),
					Attribute: ec2types.VpcAttributeNameEnableDnsHostnames,
				}); err == nil && r.EnableDnsHostnames != nil {
					attrs["enableDnsHostnames"] = aws.ToBool(r.EnableDnsHostnames.Value)
				}
				if r, err := ec2Client.DescribeVpcAttribute(ctx, &ec2.DescribeVpcAttributeInput{
					VpcId:     aws.String(vid),
					Attribute: ec2types.VpcAttributeNameEnableDnsSupport,
				}); err == nil && r.EnableDnsSupport != nil {
					attrs["enableDnsSupport"] = aws.ToBool(r.EnableDnsSupport.Value)
				}
				data.VPCDNSAttributes[vid] = attrs
			}
		}
	}

	// Subnets
	if out, err := ec2Client.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{}); err != nil {
		c.logCollectErr("subnets", err)
	} else {
		data.Subnets = out.Subnets
		c.logger.Info("collected", "type", "subnets", "count", len(out.Subnets))
	}

	// Security Groups
	if out, err := ec2Client.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{}); err != nil {
		c.logCollectErr("security groups", err)
	} else {
		data.SecurityGroups = out.SecurityGroups
		c.logger.Info("collected", "type", "security groups", "count", len(out.SecurityGroups))
	}

	// Network Interfaces
	if out, err := ec2Client.DescribeNetworkInterfaces(ctx, &ec2.DescribeNetworkInterfacesInput{}); err != nil {
		c.logCollectErr("network interfaces", err)
	} else {
		data.NetworkInterfaces = out.NetworkInterfaces
		c.logger.Info("collected", "type", "network interfaces", "count", len(out.NetworkInterfaces))
	}

	// Route Tables
	if out, err := ec2Client.DescribeRouteTables(ctx, &ec2.DescribeRouteTablesInput{}); err != nil {
		c.logCollectErr("route tables", err)
	} else {
		data.RouteTables = out.RouteTables
		c.logger.Info("collected", "type", "route tables", "count", len(out.RouteTables))
	}

	// Internet Gateways
	if out, err := ec2Client.DescribeInternetGateways(ctx, &ec2.DescribeInternetGatewaysInput{}); err != nil {
		c.logCollectErr("internet gateways", err)
	} else {
		data.InternetGateways = out.InternetGateways
		c.logger.Info("collected", "type", "internet gateways", "count", len(out.InternetGateways))
	}

	// NAT Gateways
	if out, err := ec2Client.DescribeNatGateways(ctx, &ec2.DescribeNatGatewaysInput{}); err != nil {
		c.logCollectErr("NAT gateways", err)
	} else {
		data.NATGateways = out.NatGateways
		c.logger.Info("collected", "type", "NAT gateways", "count", len(out.NatGateways))
	}

	// Elastic IPs
	if out, err := ec2Client.DescribeAddresses(ctx, &ec2.DescribeAddressesInput{}); err != nil {
		c.logCollectErr("elastic IPs", err)
	} else {
		data.ElasticIPs = out.Addresses
		c.logger.Info("collected", "type", "elastic IPs", "count", len(out.Addresses))
	}

	// EC2 Instances
	if out, err := ec2Client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{}); err != nil {
		c.logCollectErr("instances", err)
	} else {
		for _, res := range out.Reservations {
			data.Instances = append(data.Instances, res.Instances...)
		}
		c.logger.Info("collected", "type", "instances", "count", len(data.Instances))
	}

	// Network ACLs
	if out, err := ec2Client.DescribeNetworkAcls(ctx, &ec2.DescribeNetworkAclsInput{}); err != nil {
		c.logCollectErr("network ACLs", err)
	} else {
		data.NetworkACLs = out.NetworkAcls
		c.logger.Info("collected", "type", "network ACLs", "count", len(out.NetworkAcls))
	}

	// VPC Endpoints
	if out, err := ec2Client.DescribeVpcEndpoints(ctx, &ec2.DescribeVpcEndpointsInput{}); err != nil {
		c.logCollectErr("VPC endpoints", err)
	} else {
		data.VPCEndpoints = out.VpcEndpoints
		c.logger.Info("collected", "type", "VPC endpoints", "count", len(out.VpcEndpoints))
	}

	// VPC Peering Connections
	if out, err := ec2Client.DescribeVpcPeeringConnections(ctx, &ec2.DescribeVpcPeeringConnectionsInput{}); err != nil {
		c.logCollectErr("VPC peering connections", err)
	} else {
		data.VPCPeeringConnections = out.VpcPeeringConnections
		c.logger.Info("collected", "type", "VPC peering connections", "count", len(out.VpcPeeringConnections))
	}

	// Transit Gateways
	if out, err := ec2Client.DescribeTransitGateways(ctx, &ec2.DescribeTransitGatewaysInput{}); err != nil {
		c.logCollectErr("transit gateways", err)
	} else {
		data.TransitGateways = out.TransitGateways
		c.logger.Info("collected", "type", "transit gateways", "count", len(out.TransitGateways))
	}

	// Transit Gateway Attachments
	if out, err := ec2Client.DescribeTransitGatewayAttachments(ctx, &ec2.DescribeTransitGatewayAttachmentsInput{}); err != nil {
		c.logCollectErr("transit gateway attachments", err)
	} else {
		data.TransitGatewayAttachments = out.TransitGatewayAttachments
		c.logger.Info("collected", "type", "transit gateway attachments", "count", len(out.TransitGatewayAttachments))
	}

	// Transit Gateway Route Tables
	if out, err := ec2Client.DescribeTransitGatewayRouteTables(ctx, &ec2.DescribeTransitGatewayRouteTablesInput{}); err != nil {
		c.logger.Debug("no transit gateway route tables", "error", err)
	} else {
		data.TransitGatewayRouteTables = out.TransitGatewayRouteTables
		if len(out.TransitGatewayRouteTables) > 0 {
			c.logger.Info("collected", "type", "transit gateway route tables", "count", len(out.TransitGatewayRouteTables))
		}
	}

	// Transit Gateway Peering Attachments
	if out, err := ec2Client.DescribeTransitGatewayPeeringAttachments(ctx, &ec2.DescribeTransitGatewayPeeringAttachmentsInput{}); err != nil {
		c.logger.Debug("no transit gateway peering attachments", "error", err)
	} else {
		data.TransitGatewayPeeringAttachments = out.TransitGatewayPeeringAttachments
		if len(out.TransitGatewayPeeringAttachments) > 0 {
			c.logger.Info("collected", "type", "transit gateway peering attachments", "count", len(out.TransitGatewayPeeringAttachments))
		}
	}

	// VPN Gateways
	if out, err := ec2Client.DescribeVpnGateways(ctx, &ec2.DescribeVpnGatewaysInput{}); err != nil {
		c.logCollectErr("VPN gateways", err)
	} else {
		data.VPNGateways = out.VpnGateways
		c.logger.Info("collected", "type", "VPN gateways", "count", len(out.VpnGateways))
	}

	// VPN Connections
	if out, err := ec2Client.DescribeVpnConnections(ctx, &ec2.DescribeVpnConnectionsInput{}); err != nil {
		c.logCollectErr("VPN connections", err)
	} else {
		data.VPNConnections = out.VpnConnections
		c.logger.Info("collected", "type", "VPN connections", "count", len(out.VpnConnections))
	}

	// Customer Gateways
	if out, err := ec2Client.DescribeCustomerGateways(ctx, &ec2.DescribeCustomerGatewaysInput{}); err != nil {
		c.logCollectErr("customer gateways", err)
	} else {
		data.CustomerGateways = out.CustomerGateways
		c.logger.Info("collected", "type", "customer gateways", "count", len(out.CustomerGateways))
	}

	// DHCP Options
	if out, err := ec2Client.DescribeDhcpOptions(ctx, &ec2.DescribeDhcpOptionsInput{}); err != nil {
		c.logCollectErr("DHCP options", err)
	} else {
		data.DHCPOptions = out.DhcpOptions
		c.logger.Info("collected", "type", "DHCP options", "count", len(out.DhcpOptions))
	}

	// Egress-Only Internet Gateways
	if out, err := ec2Client.DescribeEgressOnlyInternetGateways(ctx, &ec2.DescribeEgressOnlyInternetGatewaysInput{}); err != nil {
		c.logger.Debug("no egress-only internet gateways", "error", err)
	} else {
		data.EgressOnlyIGWs = out.EgressOnlyInternetGateways
		if len(out.EgressOnlyInternetGateways) > 0 {
			c.logger.Info("collected", "type", "egress-only internet gateways", "count", len(out.EgressOnlyInternetGateways))
		}
	}

	// Managed Prefix Lists
	if out, err := ec2Client.DescribeManagedPrefixLists(ctx, &ec2.DescribeManagedPrefixListsInput{}); err != nil {
		c.logCollectErr("managed prefix lists", err)
	} else {
		data.ManagedPrefixLists = out.PrefixLists
		c.logger.Info("collected", "type", "managed prefix lists", "count", len(out.PrefixLists))
	}

	// Flow Logs
	if out, err := ec2Client.DescribeFlowLogs(ctx, &ec2.DescribeFlowLogsInput{}); err != nil {
		c.logger.Debug("no flow logs", "error", err)
	} else {
		data.FlowLogs = out.FlowLogs
		if len(out.FlowLogs) > 0 {
			c.logger.Info("collected", "type", "flow logs", "count", len(out.FlowLogs))
		}
	}

	// Availability Zones
	if out, err := ec2Client.DescribeAvailabilityZones(ctx, &ec2.DescribeAvailabilityZonesInput{}); err != nil {
		c.logCollectErr("availability zones", err)
	} else {
		data.AvailabilityZones = out.AvailabilityZones
		c.logger.Info("collected", "type", "availability zones", "count", len(out.AvailabilityZones))
	}
}

// collectELBResources fetches all load balancer resources.
func (c *Client) collectELBResources(data *RegionData) {
	ctx := context.Background()

	// ELBv2 (ALB/NLB/GWLB)
	elbv2Client := elasticloadbalancingv2.NewFromConfig(c.awsCfg)
	if out, err := elbv2Client.DescribeLoadBalancers(ctx, &elasticloadbalancingv2.DescribeLoadBalancersInput{}); err != nil {
		c.logCollectErr("ELBv2 load balancers", err)
	} else {
		data.LoadBalancersV2 = out.LoadBalancers
		c.logger.Info("collected", "type", "ELBv2 load balancers", "count", len(out.LoadBalancers))
	}

	// Target Groups
	if out, err := elbv2Client.DescribeTargetGroups(ctx, &elasticloadbalancingv2.DescribeTargetGroupsInput{}); err != nil {
		c.logCollectErr("target groups", err)
	} else {
		data.TargetGroups = out.TargetGroups
		c.logger.Info("collected", "type", "target groups", "count", len(out.TargetGroups))
	}

	// Classic ELB
	elbClient := elasticloadbalancing.NewFromConfig(c.awsCfg)
	if out, err := elbClient.DescribeLoadBalancers(ctx, &elasticloadbalancing.DescribeLoadBalancersInput{}); err != nil {
		c.logger.Debug("no classic load balancers", "error", err)
	} else {
		data.LoadBalancersClassic = out.LoadBalancerDescriptions
		if len(out.LoadBalancerDescriptions) > 0 {
			c.logger.Info("collected", "type", "classic load balancers", "count", len(out.LoadBalancerDescriptions))
		}
	}
}

// collectDirectConnectResources fetches Direct Connect resources.
func (c *Client) collectDirectConnectResources(data *RegionData) {
	ctx := context.Background()
	dcClient := directconnect.NewFromConfig(c.awsCfg)

	// Direct Connect Connections
	if out, err := dcClient.DescribeConnections(ctx, &directconnect.DescribeConnectionsInput{}); err != nil {
		c.logger.Debug("no Direct Connect connections", "error", err)
	} else {
		data.DirectConnectConnections = out.Connections
		if len(out.Connections) > 0 {
			c.logger.Info("collected", "type", "Direct Connect connections", "count", len(out.Connections))
		}
	}

	// Direct Connect Gateways
	if out, err := dcClient.DescribeDirectConnectGateways(ctx, &directconnect.DescribeDirectConnectGatewaysInput{}); err != nil {
		c.logger.Debug("no Direct Connect gateways", "error", err)
	} else {
		data.DirectConnectGateways = out.DirectConnectGateways
		if len(out.DirectConnectGateways) > 0 {
			c.logger.Info("collected", "type", "Direct Connect gateways", "count", len(out.DirectConnectGateways))
		}
	}

	// Direct Connect Virtual Interfaces
	if out, err := dcClient.DescribeVirtualInterfaces(ctx, &directconnect.DescribeVirtualInterfacesInput{}); err != nil {
		c.logger.Debug("no Direct Connect virtual interfaces", "error", err)
	} else {
		data.DirectConnectVIFs = out.VirtualInterfaces
		if len(out.VirtualInterfaces) > 0 {
			c.logger.Info("collected", "type", "Direct Connect virtual interfaces", "count", len(out.VirtualInterfaces))
		}
	}
}

// collectNetworkFirewallResources fetches AWS Network Firewall resources.
func (c *Client) collectNetworkFirewallResources(data *RegionData) {
	ctx := context.Background()
	nfwClient := networkfirewall.NewFromConfig(c.awsCfg)

	out, err := nfwClient.ListFirewalls(ctx, &networkfirewall.ListFirewallsInput{})
	if err != nil {
		c.logger.Debug("no network firewalls", "error", err)
		return
	}
	data.NetworkFirewalls = out.Firewalls
	if len(out.Firewalls) > 0 {
		c.logger.Info("collected", "type", "network firewalls", "count", len(out.Firewalls))
	}
}

// collectResolverResources fetches Route53 Resolver resources.
func (c *Client) collectResolverResources(data *RegionData) {
	ctx := context.Background()
	resolverClient := route53resolver.NewFromConfig(c.awsCfg)

	// Resolver Rules
	if out, err := resolverClient.ListResolverRules(ctx, &route53resolver.ListResolverRulesInput{}); err != nil {
		c.logger.Debug("no resolver rules", "error", err)
	} else {
		data.ResolverRules = out.ResolverRules
		if len(out.ResolverRules) > 0 {
			c.logger.Info("collected", "type", "resolver rules", "count", len(out.ResolverRules))
		}
	}

	// Resolver Endpoints
	if out, err := resolverClient.ListResolverEndpoints(ctx, &route53resolver.ListResolverEndpointsInput{}); err != nil {
		c.logger.Debug("no resolver endpoints", "error", err)
	} else {
		data.ResolverEndpoints = out.ResolverEndpoints
		if len(out.ResolverEndpoints) > 0 {
			c.logger.Info("collected", "type", "resolver endpoints", "count", len(out.ResolverEndpoints))
		}
	}
}

// collectComputeOrchestrationResources fetches EKS, ECS, and Auto Scaling resources.
func (c *Client) collectComputeOrchestrationResources(data *RegionData) {
	ctx := context.Background()

	eksClient := eks.NewFromConfig(c.awsCfg)

	var clusterNames []string
	clusterPager := eks.NewListClustersPaginator(eksClient, &eks.ListClustersInput{})
	for clusterPager.HasMorePages() {
		page, err := clusterPager.NextPage(ctx)
		if err != nil {
			c.logCollectErr("EKS clusters", err)
			break
		}
		clusterNames = append(clusterNames, page.Clusters...)
	}

	for _, name := range clusterNames {
		desc, err := eksClient.DescribeCluster(ctx, &eks.DescribeClusterInput{
			Name: aws.String(name),
		})
		if err != nil {
			c.logCollectErr("EKS cluster "+name, err)
			continue
		}
		if desc.Cluster == nil {
			continue
		}
		data.EKSClusters = append(data.EKSClusters, *desc.Cluster)

		// Node groups for this cluster.
		ngPager := eks.NewListNodegroupsPaginator(eksClient, &eks.ListNodegroupsInput{
			ClusterName: aws.String(name),
		})
		for ngPager.HasMorePages() {
			ngPage, err := ngPager.NextPage(ctx)
			if err != nil {
				c.logCollectErr("EKS node groups for "+name, err)
				break
			}
			for _, ngName := range ngPage.Nodegroups {
				ngDesc, err := eksClient.DescribeNodegroup(ctx, &eks.DescribeNodegroupInput{
					ClusterName:   aws.String(name),
					NodegroupName: aws.String(ngName),
				})
				if err != nil {
					c.logCollectErr("EKS nodegroup "+ngName, err)
					continue
				}
				if ngDesc.Nodegroup != nil {
					data.EKSNodeGroups = append(data.EKSNodeGroups, EKSNodeGroupEntry{
						ClusterName: name,
						Nodegroup:   *ngDesc.Nodegroup,
					})
				}
			}
		}
	}
	if len(data.EKSClusters) > 0 {
		c.logger.Info("collected", "type", "EKS clusters", "count", len(data.EKSClusters))
		c.logger.Info("collected", "type", "EKS node groups", "count", len(data.EKSNodeGroups))
	}

	ecsClient := ecs.NewFromConfig(c.awsCfg)

	var clusterARNs []string
	clusterARNPager := ecs.NewListClustersPaginator(ecsClient, &ecs.ListClustersInput{})
	for clusterARNPager.HasMorePages() {
		page, err := clusterARNPager.NextPage(ctx)
		if err != nil {
			c.logCollectErr("ECS clusters", err)
			break
		}
		clusterARNs = append(clusterARNs, page.ClusterArns...)
	}

	if len(clusterARNs) > 0 {
		// DescribeClusters accepts up to 100 ARNs.
		descOut, err := ecsClient.DescribeClusters(ctx, &ecs.DescribeClustersInput{
			Clusters: clusterARNs,
			Include:  []ecstypes.ClusterField{ecstypes.ClusterFieldTags, ecstypes.ClusterFieldStatistics},
		})
		if err != nil {
			c.logCollectErr("ECS cluster details", err)
		} else {
			data.ECSClusters = descOut.Clusters
			if len(data.ECSClusters) > 0 {
				c.logger.Info("collected", "type", "ECS clusters", "count", len(data.ECSClusters))
			}

			// Services per cluster.
			for _, cluster := range data.ECSClusters {
				clusterARN := aws.ToString(cluster.ClusterArn)
				var svcARNs []string
				svcPager := ecs.NewListServicesPaginator(ecsClient, &ecs.ListServicesInput{
					Cluster: aws.String(clusterARN),
				})
				for svcPager.HasMorePages() {
					svcPage, err := svcPager.NextPage(ctx)
					if err != nil {
						c.logCollectErr("ECS services for "+clusterARN, err)
						break
					}
					svcARNs = append(svcARNs, svcPage.ServiceArns...)
				}

				// DescribeServices max 10 per call.
				for i := 0; i < len(svcARNs); i += 10 {
					end := i + 10
					if end > len(svcARNs) {
						end = len(svcARNs)
					}
					descSvcs, err := ecsClient.DescribeServices(ctx, &ecs.DescribeServicesInput{
						Cluster:  aws.String(clusterARN),
						Services: svcARNs[i:end],
						Include:  []ecstypes.ServiceField{ecstypes.ServiceFieldTags},
					})
					if err != nil {
						c.logCollectErr("ECS service batch for "+clusterARN, err)
						continue
					}
					data.ECSServices = append(data.ECSServices, descSvcs.Services...)
				}
			}
			if len(data.ECSServices) > 0 {
				c.logger.Info("collected", "type", "ECS services", "count", len(data.ECSServices))
			}
		}
	}

	asgClient := autoscaling.NewFromConfig(c.awsCfg)
	asgPager := autoscaling.NewDescribeAutoScalingGroupsPaginator(asgClient, &autoscaling.DescribeAutoScalingGroupsInput{})
	for asgPager.HasMorePages() {
		page, err := asgPager.NextPage(ctx)
		if err != nil {
			c.logCollectErr("Auto Scaling Groups", err)
			break
		}
		data.AutoScalingGroups = append(data.AutoScalingGroups, page.AutoScalingGroups...)
	}
	if len(data.AutoScalingGroups) > 0 {
		c.logger.Info("collected", "type", "Auto Scaling Groups", "count", len(data.AutoScalingGroups))
	}
}

// collectManagedDataResources fetches RDS, DynamoDB and ElastiCache resources.
func (c *Client) collectManagedDataResources(data *RegionData) {
	ctx := context.Background()

	rdsClient := rds.NewFromConfig(c.awsCfg)

	// RDS DB Instances (single-AZ and Multi-AZ, not Aurora cluster members).
	rdsPager := rds.NewDescribeDBInstancesPaginator(rdsClient, &rds.DescribeDBInstancesInput{})
	for rdsPager.HasMorePages() {
		page, err := rdsPager.NextPage(ctx)
		if err != nil {
			c.logCollectErr("RDS DB instances", err)
			break
		}
		data.RDSInstances = append(data.RDSInstances, page.DBInstances...)
	}
	if len(data.RDSInstances) > 0 {
		c.logger.Info("collected", "type", "RDS DB instances", "count", len(data.RDSInstances))
	}

	// RDS DB Clusters (Aurora).
	clusterPager := rds.NewDescribeDBClustersPaginator(rdsClient, &rds.DescribeDBClustersInput{})
	for clusterPager.HasMorePages() {
		page, err := clusterPager.NextPage(ctx)
		if err != nil {
			c.logCollectErr("RDS DB clusters", err)
			break
		}
		data.RDSClusters = append(data.RDSClusters, page.DBClusters...)
	}
	if len(data.RDSClusters) > 0 {
		c.logger.Info("collected", "type", "RDS DB clusters", "count", len(data.RDSClusters))
	}

	// RDS DB Subnet Groups.
	sgPager := rds.NewDescribeDBSubnetGroupsPaginator(rdsClient, &rds.DescribeDBSubnetGroupsInput{})
	for sgPager.HasMorePages() {
		page, err := sgPager.NextPage(ctx)
		if err != nil {
			c.logCollectErr("RDS DB subnet groups", err)
			break
		}
		data.RDSSubnetGroups = append(data.RDSSubnetGroups, page.DBSubnetGroups...)
	}
	if len(data.RDSSubnetGroups) > 0 {
		c.logger.Info("collected", "type", "RDS DB subnet groups", "count", len(data.RDSSubnetGroups))
	}

	dynamoClient := dynamodb.NewFromConfig(c.awsCfg)

	// ListTables returns table names; DescribeTable fetches full detail.
	var tableNames []string
	tablePager := dynamodb.NewListTablesPaginator(dynamoClient, &dynamodb.ListTablesInput{})
	for tablePager.HasMorePages() {
		page, err := tablePager.NextPage(ctx)
		if err != nil {
			c.logCollectErr("DynamoDB tables", err)
			break
		}
		tableNames = append(tableNames, page.TableNames...)
	}
	for _, name := range tableNames {
		desc, err := dynamoClient.DescribeTable(ctx, &dynamodb.DescribeTableInput{
			TableName: aws.String(name),
		})
		if err != nil {
			c.logCollectErr("DynamoDB table "+name, err)
			continue
		}
		if desc.Table != nil {
			data.DynamoDBTables = append(data.DynamoDBTables, *desc.Table)
		}
	}
	if len(data.DynamoDBTables) > 0 {
		c.logger.Info("collected", "type", "DynamoDB tables", "count", len(data.DynamoDBTables))
	}

	ecClient := elasticache.NewFromConfig(c.awsCfg)

	// Replication Groups (Redis / Valkey clusters).
	rgPager := elasticache.NewDescribeReplicationGroupsPaginator(ecClient, &elasticache.DescribeReplicationGroupsInput{})
	for rgPager.HasMorePages() {
		page, err := rgPager.NextPage(ctx)
		if err != nil {
			c.logCollectErr("ElastiCache replication groups", err)
			break
		}
		data.ElastiCacheReplicationGroups = append(data.ElastiCacheReplicationGroups, page.ReplicationGroups...)
	}
	if len(data.ElastiCacheReplicationGroups) > 0 {
		c.logger.Info("collected", "type", "ElastiCache replication groups", "count", len(data.ElastiCacheReplicationGroups))
	}

	// Subnet Groups.
	ecsgPager := elasticache.NewDescribeCacheSubnetGroupsPaginator(ecClient, &elasticache.DescribeCacheSubnetGroupsInput{})
	for ecsgPager.HasMorePages() {
		page, err := ecsgPager.NextPage(ctx)
		if err != nil {
			c.logCollectErr("ElastiCache subnet groups", err)
			break
		}
		data.ElastiCacheSubnetGroups = append(data.ElastiCacheSubnetGroups, page.CacheSubnetGroups...)
	}
	if len(data.ElastiCacheSubnetGroups) > 0 {
		c.logger.Info("collected", "type", "ElastiCache subnet groups", "count", len(data.ElastiCacheSubnetGroups))
	}
}

// collectStorageResources fetches EBS volumes, S3 buckets, EFS file systems and FSx file systems.
func (c *Client) collectStorageResources(data *RegionData) {
	ctx := context.Background()

	ec2Client := ec2.NewFromConfig(c.awsCfg)
	volPager := ec2.NewDescribeVolumesPaginator(ec2Client, &ec2.DescribeVolumesInput{})
	for volPager.HasMorePages() {
		page, err := volPager.NextPage(ctx)
		if err != nil {
			c.logCollectErr("EBS volumes", err)
			break
		}
		data.EBSVolumes = append(data.EBSVolumes, page.Volumes...)
	}
	if len(data.EBSVolumes) > 0 {
		c.logger.Info("collected", "type", "EBS volumes", "count", len(data.EBSVolumes))
	}

	// ListBuckets is a global call; we filter to the current region via GetBucketLocation.
	s3Client := s3.NewFromConfig(c.awsCfg)
	listOut, err := s3Client.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		c.logCollectErr("S3 buckets", err)
	} else {
		for _, b := range listOut.Buckets {
			name := aws.ToString(b.Name)
			if name == "" {
				continue
			}
			locOut, err := s3Client.GetBucketLocation(ctx, &s3.GetBucketLocationInput{
				Bucket: aws.String(name),
			})
			if err != nil {
				c.logCollectErr("S3 bucket location "+name, err)
				continue
			}
			bucketRegion := string(locOut.LocationConstraint)
			if bucketRegion == "" {
				bucketRegion = "us-east-1" // empty constraint means us-east-1
			}
			if bucketRegion != c.region {
				continue
			}
			info := S3BucketInfo{Name: name, Region: bucketRegion}
			// Tags - best-effort.
			tagsOut, err := s3Client.GetBucketTagging(ctx, &s3.GetBucketTaggingInput{
				Bucket: aws.String(name),
			})
			if err == nil && tagsOut != nil {
				m := make(map[string]string, len(tagsOut.TagSet))
				for _, t := range tagsOut.TagSet {
					m[aws.ToString(t.Key)] = aws.ToString(t.Value)
				}
				info.Tags = m
			}
			// Versioning - best-effort.
			verOut, err := s3Client.GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{
				Bucket: aws.String(name),
			})
			if err == nil && verOut != nil {
				info.Versioning = string(verOut.Status)
			}
			// Encryption - best-effort.
			encOut, err := s3Client.GetBucketEncryption(ctx, &s3.GetBucketEncryptionInput{
				Bucket: aws.String(name),
			})
			if err == nil && encOut != nil && encOut.ServerSideEncryptionConfiguration != nil {
				for _, rule := range encOut.ServerSideEncryptionConfiguration.Rules {
					if rule.ApplyServerSideEncryptionByDefault != nil {
						info.EncryptionAlgorithm = string(rule.ApplyServerSideEncryptionByDefault.SSEAlgorithm)
						if rule.ApplyServerSideEncryptionByDefault.KMSMasterKeyID != nil {
							info.EncryptionKeyARN = aws.ToString(rule.ApplyServerSideEncryptionByDefault.KMSMasterKeyID)
						}
						break
					}
				}
			}
			// Public access block - best-effort.
			pabOut, err := s3Client.GetPublicAccessBlock(ctx, &s3.GetPublicAccessBlockInput{
				Bucket: aws.String(name),
			})
			if err == nil && pabOut != nil && pabOut.PublicAccessBlockConfiguration != nil {
				c := pabOut.PublicAccessBlockConfiguration
				info.BlockPublicAccess = aws.ToBool(c.BlockPublicAcls) &&
					aws.ToBool(c.IgnorePublicAcls) &&
					aws.ToBool(c.BlockPublicPolicy) &&
					aws.ToBool(c.RestrictPublicBuckets)
			}
			data.S3Buckets = append(data.S3Buckets, info)
		}
		if len(data.S3Buckets) > 0 {
			c.logger.Info("collected", "type", "S3 buckets", "count", len(data.S3Buckets))
		}
	}

	efsClient := efs.NewFromConfig(c.awsCfg)
	efsPager := efs.NewDescribeFileSystemsPaginator(efsClient, &efs.DescribeFileSystemsInput{})
	for efsPager.HasMorePages() {
		page, err := efsPager.NextPage(ctx)
		if err != nil {
			c.logCollectErr("EFS file systems", err)
			break
		}
		for _, fs := range page.FileSystems {
			entry := EFSFileSystemEntry{FileSystem: fs}
			// Collect mount targets for subnet connection wiring.
			fsID := aws.ToString(fs.FileSystemId)
			mtPager := efs.NewDescribeMountTargetsPaginator(efsClient, &efs.DescribeMountTargetsInput{
				FileSystemId: aws.String(fsID),
			})
			for mtPager.HasMorePages() {
				mtPage, err := mtPager.NextPage(ctx)
				if err != nil {
					c.logCollectErr("EFS mount targets for "+fsID, err)
					break
				}
				entry.MountTargets = append(entry.MountTargets, mtPage.MountTargets...)
			}
			data.EFSFileSystems = append(data.EFSFileSystems, entry)
		}
	}
	if len(data.EFSFileSystems) > 0 {
		c.logger.Info("collected", "type", "EFS file systems", "count", len(data.EFSFileSystems))
	}

	fsxClient := fsx.NewFromConfig(c.awsCfg)
	fsxPager := fsx.NewDescribeFileSystemsPaginator(fsxClient, &fsx.DescribeFileSystemsInput{})
	for fsxPager.HasMorePages() {
		page, err := fsxPager.NextPage(ctx)
		if err != nil {
			c.logCollectErr("FSx file systems", err)
			break
		}
		data.FSxFileSystems = append(data.FSxFileSystems, page.FileSystems...)
	}
	if len(data.FSxFileSystems) > 0 {
		c.logger.Info("collected", "type", "FSx file systems", "count", len(data.FSxFileSystems))
	}
}

// collectServerlessMessagingResources fetches Lambda, SQS, Kinesis and MSK resources.
func (c *Client) collectServerlessMessagingResources(data *RegionData) {
	ctx := context.Background()

	lambdaClient := lambda.NewFromConfig(c.awsCfg)
	lambdaPager := lambda.NewListFunctionsPaginator(lambdaClient, &lambda.ListFunctionsInput{})
	for lambdaPager.HasMorePages() {
		page, err := lambdaPager.NextPage(ctx)
		if err != nil {
			c.logCollectErr("Lambda functions", err)
			break
		}
		data.LambdaFunctions = append(data.LambdaFunctions, page.Functions...)
	}
	if len(data.LambdaFunctions) > 0 {
		c.logger.Info("collected", "type", "Lambda functions", "count", len(data.LambdaFunctions))
	}

	sqsClient := sqs.NewFromConfig(c.awsCfg)
	sqsPager := sqs.NewListQueuesPaginator(sqsClient, &sqs.ListQueuesInput{})
	for sqsPager.HasMorePages() {
		page, err := sqsPager.NextPage(ctx)
		if err != nil {
			c.logCollectErr("SQS queues", err)
			break
		}
		for _, url := range page.QueueUrls {
			attrs, err := sqsClient.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
				QueueUrl:       aws.String(url),
				AttributeNames: []sqstypes.QueueAttributeName{sqstypes.QueueAttributeNameAll},
			})
			if err != nil {
				c.logCollectErr("SQS queue attributes "+url, err)
				continue
			}
			data.SQSQueues = append(data.SQSQueues, SQSQueueInfo{
				URL:        url,
				Attributes: attrs.Attributes,
			})
		}
	}
	if len(data.SQSQueues) > 0 {
		c.logger.Info("collected", "type", "SQS queues", "count", len(data.SQSQueues))
	}

	kinesisClient := kinesis.NewFromConfig(c.awsCfg)
	kinesisPager := kinesis.NewListStreamsPaginator(kinesisClient, &kinesis.ListStreamsInput{})
	for kinesisPager.HasMorePages() {
		page, err := kinesisPager.NextPage(ctx)
		if err != nil {
			c.logCollectErr("Kinesis streams", err)
			break
		}
		for _, summary := range page.StreamSummaries {
			desc, err := kinesisClient.DescribeStreamSummary(ctx, &kinesis.DescribeStreamSummaryInput{
				StreamARN: summary.StreamARN,
			})
			if err != nil {
				c.logCollectErr("Kinesis stream "+aws.ToString(summary.StreamName), err)
				continue
			}
			if desc.StreamDescriptionSummary != nil {
				data.KinesisStreams = append(data.KinesisStreams, *desc.StreamDescriptionSummary)
			}
		}
	}
	if len(data.KinesisStreams) > 0 {
		c.logger.Info("collected", "type", "Kinesis streams", "count", len(data.KinesisStreams))
	}

	kafkaClient := kafka.NewFromConfig(c.awsCfg)
	mskPager := kafka.NewListClustersPaginator(kafkaClient, &kafka.ListClustersInput{})
	for mskPager.HasMorePages() {
		page, err := mskPager.NextPage(ctx)
		if err != nil {
			c.logCollectErr("MSK clusters", err)
			break
		}
		data.MSKClusters = append(data.MSKClusters, page.ClusterInfoList...)
	}
	if len(data.MSKClusters) > 0 {
		c.logger.Info("collected", "type", "MSK clusters", "count", len(data.MSKClusters))
	}
}

// collectExtendedDatabaseResources fetches DocumentDB, Neptune, Redshift, OpenSearch,
// and MemoryDB resources.
func (c *Client) collectExtendedDatabaseResources(data *RegionData) {
	ctx := context.Background()

	docdbClient := docdb.NewFromConfig(c.awsCfg)
	docdbPager := docdb.NewDescribeDBClustersPaginator(docdbClient, &docdb.DescribeDBClustersInput{})
	for docdbPager.HasMorePages() {
		page, err := docdbPager.NextPage(ctx)
		if err != nil {
			c.logCollectErr("DocumentDB clusters", err)
			break
		}
		data.DocDBClusters = append(data.DocDBClusters, page.DBClusters...)
	}
	docdbSGPager := docdb.NewDescribeDBSubnetGroupsPaginator(docdbClient, &docdb.DescribeDBSubnetGroupsInput{})
	for docdbSGPager.HasMorePages() {
		page, err := docdbSGPager.NextPage(ctx)
		if err != nil {
			c.logCollectErr("DocumentDB subnet groups", err)
			break
		}
		data.DocDBSubnetGroups = append(data.DocDBSubnetGroups, page.DBSubnetGroups...)
	}
	if len(data.DocDBClusters) > 0 {
		c.logger.Info("collected", "type", "DocumentDB clusters", "count", len(data.DocDBClusters))
	}

	neptuneClient := neptune.NewFromConfig(c.awsCfg)
	neptunePager := neptune.NewDescribeDBClustersPaginator(neptuneClient, &neptune.DescribeDBClustersInput{})
	for neptunePager.HasMorePages() {
		page, err := neptunePager.NextPage(ctx)
		if err != nil {
			c.logCollectErr("Neptune clusters", err)
			break
		}
		data.NeptuneClusters = append(data.NeptuneClusters, page.DBClusters...)
	}
	neptuneSGPager := neptune.NewDescribeDBSubnetGroupsPaginator(neptuneClient, &neptune.DescribeDBSubnetGroupsInput{})
	for neptuneSGPager.HasMorePages() {
		page, err := neptuneSGPager.NextPage(ctx)
		if err != nil {
			c.logCollectErr("Neptune subnet groups", err)
			break
		}
		data.NeptuneSubnetGroups = append(data.NeptuneSubnetGroups, page.DBSubnetGroups...)
	}
	if len(data.NeptuneClusters) > 0 {
		c.logger.Info("collected", "type", "Neptune clusters", "count", len(data.NeptuneClusters))
	}

	redshiftClient := redshift.NewFromConfig(c.awsCfg)
	rsPager := redshift.NewDescribeClustersPaginator(redshiftClient, &redshift.DescribeClustersInput{})
	for rsPager.HasMorePages() {
		page, err := rsPager.NextPage(ctx)
		if err != nil {
			c.logCollectErr("Redshift clusters", err)
			break
		}
		data.RedshiftClusters = append(data.RedshiftClusters, page.Clusters...)
	}
	rsSGPager := redshift.NewDescribeClusterSubnetGroupsPaginator(redshiftClient, &redshift.DescribeClusterSubnetGroupsInput{})
	for rsSGPager.HasMorePages() {
		page, err := rsSGPager.NextPage(ctx)
		if err != nil {
			c.logCollectErr("Redshift subnet groups", err)
			break
		}
		data.RedshiftSubnetGroups = append(data.RedshiftSubnetGroups, page.ClusterSubnetGroups...)
	}
	if len(data.RedshiftClusters) > 0 {
		c.logger.Info("collected", "type", "Redshift clusters", "count", len(data.RedshiftClusters))
	}

	// ListDomainNames returns all domain names; DescribeDomains returns full details.
	osClient := opensearch.NewFromConfig(c.awsCfg)
	listOut, err := osClient.ListDomainNames(ctx, &opensearch.ListDomainNamesInput{})
	if err != nil {
		c.logCollectErr("OpenSearch domains", err)
	} else if len(listOut.DomainNames) > 0 {
		names := make([]string, 0, len(listOut.DomainNames))
		for _, d := range listOut.DomainNames {
			if d.DomainName != nil {
				names = append(names, aws.ToString(d.DomainName))
			}
		}
		// DescribeDomains supports up to 5 domains per call; batch if needed.
		for i := 0; i < len(names); i += 5 {
			end := i + 5
			if end > len(names) {
				end = len(names)
			}
			descOut, err := osClient.DescribeDomains(ctx, &opensearch.DescribeDomainsInput{
				DomainNames: names[i:end],
			})
			if err != nil {
				c.logCollectErr("OpenSearch domain details", err)
				continue
			}
			data.OpenSearchDomains = append(data.OpenSearchDomains, descOut.DomainStatusList...)
		}
		if len(data.OpenSearchDomains) > 0 {
			c.logger.Info("collected", "type", "OpenSearch domains", "count", len(data.OpenSearchDomains))
		}
	}

	mdbClient := memorydb.NewFromConfig(c.awsCfg)
	mdbPager := memorydb.NewDescribeClustersPaginator(mdbClient, &memorydb.DescribeClustersInput{})
	for mdbPager.HasMorePages() {
		page, err := mdbPager.NextPage(ctx)
		if err != nil {
			c.logCollectErr("MemoryDB clusters", err)
			break
		}
		data.MemoryDBClusters = append(data.MemoryDBClusters, page.Clusters...)
	}
	mdbSGPager := memorydb.NewDescribeSubnetGroupsPaginator(mdbClient, &memorydb.DescribeSubnetGroupsInput{})
	for mdbSGPager.HasMorePages() {
		page, err := mdbSGPager.NextPage(ctx)
		if err != nil {
			c.logCollectErr("MemoryDB subnet groups", err)
			break
		}
		data.MemoryDBSubnetGroups = append(data.MemoryDBSubnetGroups, page.SubnetGroups...)
	}
	if len(data.MemoryDBClusters) > 0 {
		c.logger.Info("collected", "type", "MemoryDB clusters", "count", len(data.MemoryDBClusters))
	}
}

// collectEventDrivenResources fetches SNS topics, EventBridge buses, and Step Functions state machines.
func (c *Client) collectEventDrivenResources(data *RegionData) {
	ctx := context.Background()

	snsClient := sns.NewFromConfig(c.awsCfg)
	snsPager := sns.NewListTopicsPaginator(snsClient, &sns.ListTopicsInput{})
	for snsPager.HasMorePages() {
		page, err := snsPager.NextPage(ctx)
		if err != nil {
			c.logCollectErr("SNS topics", err)
			break
		}
		data.SNSTopics = append(data.SNSTopics, page.Topics...)
	}
	if len(data.SNSTopics) > 0 {
		c.logger.Info("collected", "type", "SNS topics", "count", len(data.SNSTopics))
	}

	// No built-in paginator; use manual NextToken loop.
	ebClient := eventbridge.NewFromConfig(c.awsCfg)
	var ebNextToken *string
	for {
		out, err := ebClient.ListEventBuses(ctx, &eventbridge.ListEventBusesInput{
			NextToken: ebNextToken,
		})
		if err != nil {
			c.logCollectErr("EventBridge buses", err)
			break
		}
		data.EventBridgeBuses = append(data.EventBridgeBuses, out.EventBuses...)
		if out.NextToken == nil {
			break
		}
		ebNextToken = out.NextToken
	}
	if len(data.EventBridgeBuses) > 0 {
		c.logger.Info("collected", "type", "EventBridge buses", "count", len(data.EventBridgeBuses))
	}

	sfnClient := sfn.NewFromConfig(c.awsCfg)
	sfnPager := sfn.NewListStateMachinesPaginator(sfnClient, &sfn.ListStateMachinesInput{})
	for sfnPager.HasMorePages() {
		page, err := sfnPager.NextPage(ctx)
		if err != nil {
			c.logCollectErr("Step Functions state machines", err)
			break
		}
		data.SFNStateMachines = append(data.SFNStateMachines, page.StateMachines...)
	}
	if len(data.SFNStateMachines) > 0 {
		c.logger.Info("collected", "type", "Step Functions state machines", "count", len(data.SFNStateMachines))
	}
}

// collectNetworkingExtendedResources fetches ELBv2 listeners, Direct Connect LAGs,
// VPC PrivateLink endpoint service configurations, and API Gateway v1/v2 APIs.
func (c *Client) collectNetworkingExtendedResources(data *RegionData) {
	ctx := context.Background()

	// DescribeListeners requires a LoadBalancerArn; a global call returns 400.
	// Iterate each collected LB and fetch its listeners individually.
	elbv2Client := elasticloadbalancingv2.NewFromConfig(c.awsCfg)
	for _, lb := range data.LoadBalancersV2 {
		lbArn := lb.LoadBalancerArn
		if lbArn == nil {
			continue
		}
		pager := elasticloadbalancingv2.NewDescribeListenersPaginator(elbv2Client, &elasticloadbalancingv2.DescribeListenersInput{
			LoadBalancerArn: lbArn,
		})
		for pager.HasMorePages() {
			page, err := pager.NextPage(ctx)
			if err != nil {
				c.logCollectErr("ELBv2 listeners", err)
				break
			}
			data.ELBv2Listeners = append(data.ELBv2Listeners, page.Listeners...)
		}
	}
	if len(data.ELBv2Listeners) > 0 {
		c.logger.Info("collected", "type", "ELBv2 listeners", "count", len(data.ELBv2Listeners))
	}

	dcClient := directconnect.NewFromConfig(c.awsCfg)
	var dcNextToken *string
	for {
		out, err := dcClient.DescribeLags(ctx, &directconnect.DescribeLagsInput{
			NextToken: dcNextToken,
		})
		if err != nil {
			c.logCollectErr("Direct Connect LAGs", err)
			break
		}
		data.DirectConnectLAGs = append(data.DirectConnectLAGs, out.Lags...)
		if out.NextToken == nil {
			break
		}
		dcNextToken = out.NextToken
	}
	if len(data.DirectConnectLAGs) > 0 {
		c.logger.Info("collected", "type", "Direct Connect LAGs", "count", len(data.DirectConnectLAGs))
	}

	ec2Client := ec2.NewFromConfig(c.awsCfg)
	epSvcPager := ec2.NewDescribeVpcEndpointServiceConfigurationsPaginator(ec2Client,
		&ec2.DescribeVpcEndpointServiceConfigurationsInput{})
	for epSvcPager.HasMorePages() {
		page, err := epSvcPager.NextPage(ctx)
		if err != nil {
			c.logCollectErr("VPC endpoint service configurations", err)
			break
		}
		data.VPCEndpointServices = append(data.VPCEndpointServices, page.ServiceConfigurations...)
	}
	if len(data.VPCEndpointServices) > 0 {
		c.logger.Info("collected", "type", "VPC endpoint services", "count", len(data.VPCEndpointServices))
	}

	apigwClient := apigateway.NewFromConfig(c.awsCfg)
	restAPIPager := apigateway.NewGetRestApisPaginator(apigwClient, &apigateway.GetRestApisInput{})
	for restAPIPager.HasMorePages() {
		page, err := restAPIPager.NextPage(ctx)
		if err != nil {
			c.logCollectErr("API Gateway REST APIs", err)
			break
		}
		data.APIGatewayRestAPIs = append(data.APIGatewayRestAPIs, page.Items...)
	}
	if len(data.APIGatewayRestAPIs) > 0 {
		c.logger.Info("collected", "type", "API Gateway REST APIs", "count", len(data.APIGatewayRestAPIs))
	}

	// No built-in paginator; use manual NextToken loop.
	apigwv2Client := apigatewayv2.NewFromConfig(c.awsCfg)
	var apigwv2Token *string
	for {
		out, err := apigwv2Client.GetApis(ctx, &apigatewayv2.GetApisInput{
			NextToken: apigwv2Token,
		})
		if err != nil {
			c.logCollectErr("API Gateway v2 APIs", err)
			break
		}
		data.APIGatewayV2APIs = append(data.APIGatewayV2APIs, out.Items...)
		if out.NextToken == nil {
			break
		}
		apigwv2Token = out.NextToken
	}
	if len(data.APIGatewayV2APIs) > 0 {
		c.logger.Info("collected", "type", "API Gateway v2 APIs", "count", len(data.APIGatewayV2APIs))
	}
}

// collectObservabilityResources fetches CloudWatch log groups and AWS Backup vaults.
func (c *Client) collectObservabilityResources(data *RegionData) {
	ctx := context.Background()

	cwClient := cloudwatchlogs.NewFromConfig(c.awsCfg)
	logGroupPager := cloudwatchlogs.NewDescribeLogGroupsPaginator(cwClient,
		&cloudwatchlogs.DescribeLogGroupsInput{})
	for logGroupPager.HasMorePages() {
		page, err := logGroupPager.NextPage(ctx)
		if err != nil {
			c.logCollectErr("CloudWatch log groups", err)
			break
		}
		data.CloudWatchLogGroups = append(data.CloudWatchLogGroups, page.LogGroups...)
	}
	if len(data.CloudWatchLogGroups) > 0 {
		c.logger.Info("collected", "type", "CloudWatch log groups", "count", len(data.CloudWatchLogGroups))
	}

	backupClient := backup.NewFromConfig(c.awsCfg)
	vaultPager := backup.NewListBackupVaultsPaginator(backupClient, &backup.ListBackupVaultsInput{})
	for vaultPager.HasMorePages() {
		page, err := vaultPager.NextPage(ctx)
		if err != nil {
			c.logCollectErr("Backup vaults", err)
			break
		}
		data.BackupVaults = append(data.BackupVaults, page.BackupVaultList...)
	}
	if len(data.BackupVaults) > 0 {
		c.logger.Info("collected", "type", "Backup vaults", "count", len(data.BackupVaults))
	}
}

// collectSecurityIdentityResources fetches KMS, Secrets Manager, ECR, and WAFv2 resources.
func (c *Client) collectSecurityIdentityResources(data *RegionData) {
	ctx := context.Background()

	kmsClient := kms.NewFromConfig(c.awsCfg)
	kmsPager := kms.NewListKeysPaginator(kmsClient, &kms.ListKeysInput{})
	for kmsPager.HasMorePages() {
		page, err := kmsPager.NextPage(ctx)
		if err != nil {
			c.logCollectErr("KMS keys", err)
			break
		}
		for _, entry := range page.Keys {
			desc, err := kmsClient.DescribeKey(ctx, &kms.DescribeKeyInput{
				KeyId: entry.KeyId,
			})
			if err != nil {
				c.logCollectErr("KMS key "+aws.ToString(entry.KeyId), err)
				continue
			}
			if desc.KeyMetadata == nil {
				continue
			}
			if desc.KeyMetadata.KeyManager != kmstypes.KeyManagerTypeCustomer {
				continue
			}
			data.KMSKeys = append(data.KMSKeys, *desc.KeyMetadata)
		}
	}
	if len(data.KMSKeys) > 0 {
		c.logger.Info("collected", "type", "KMS customer-managed keys", "count", len(data.KMSKeys))
	}

	smClient := secretsmanager.NewFromConfig(c.awsCfg)
	smPager := secretsmanager.NewListSecretsPaginator(smClient, &secretsmanager.ListSecretsInput{})
	for smPager.HasMorePages() {
		page, err := smPager.NextPage(ctx)
		if err != nil {
			c.logCollectErr("Secrets Manager secrets", err)
			break
		}
		data.SecretsManagerSecrets = append(data.SecretsManagerSecrets, page.SecretList...)
	}
	if len(data.SecretsManagerSecrets) > 0 {
		c.logger.Info("collected", "type", "Secrets Manager secrets", "count", len(data.SecretsManagerSecrets))
	}

	ecrClient := ecr.NewFromConfig(c.awsCfg)
	ecrPager := ecr.NewDescribeRepositoriesPaginator(ecrClient, &ecr.DescribeRepositoriesInput{})
	for ecrPager.HasMorePages() {
		page, err := ecrPager.NextPage(ctx)
		if err != nil {
			c.logCollectErr("ECR repositories", err)
			break
		}
		data.ECRRepositories = append(data.ECRRepositories, page.Repositories...)
	}
	if len(data.ECRRepositories) > 0 {
		c.logger.Info("collected", "type", "ECR repositories", "count", len(data.ECRRepositories))
	}

	// WAFv2 ListWebACLs uses NextMarker pagination with no built-in paginator.
	wafClient := wafv2.NewFromConfig(c.awsCfg)
	var wafNextMarker *string
	for {
		out, err := wafClient.ListWebACLs(ctx, &wafv2.ListWebACLsInput{
			Scope:      wafv2types.ScopeRegional,
			NextMarker: wafNextMarker,
		})
		if err != nil {
			c.logCollectErr("WAFv2 web ACLs", err)
			break
		}
		for _, summary := range out.WebACLs {
			full, err := wafClient.GetWebACL(ctx, &wafv2.GetWebACLInput{
				Name:  summary.Name,
				Scope: wafv2types.ScopeRegional,
				Id:    summary.Id,
			})
			if err != nil {
				c.logCollectErr("WAFv2 web ACL "+aws.ToString(summary.Name), err)
				continue
			}
			if full.WebACL == nil {
				continue
			}
			entry := WAFv2WebACLEntry{WebACL: *full.WebACL}
			assocOut, err := wafClient.ListResourcesForWebACL(ctx, &wafv2.ListResourcesForWebACLInput{
				WebACLArn: full.WebACL.ARN,
			})
			if err == nil && assocOut != nil {
				entry.AssociatedResourceARNs = assocOut.ResourceArns
			}
			data.WAFv2WebACLs = append(data.WAFv2WebACLs, entry)
		}
		if out.NextMarker == nil {
			break
		}
		wafNextMarker = out.NextMarker
	}
	if len(data.WAFv2WebACLs) > 0 {
		c.logger.Info("collected", "type", "WAFv2 web ACLs", "count", len(data.WAFv2WebACLs))
	}

	acmClient := acm.NewFromConfig(c.awsCfg)
	var acmNextToken *string
	var certARNs []string
	for {
		listOut, err := acmClient.ListCertificates(ctx, &acm.ListCertificatesInput{
			NextToken: acmNextToken,
		})
		if err != nil {
			c.logCollectErr("ACM certificates", err)
			break
		}
		for _, s := range listOut.CertificateSummaryList {
			if s.CertificateArn != nil {
				certARNs = append(certARNs, *s.CertificateArn)
			}
		}
		if listOut.NextToken == nil {
			break
		}
		acmNextToken = listOut.NextToken
	}
	for _, arn := range certARNs {
		desc, err := acmClient.DescribeCertificate(ctx, &acm.DescribeCertificateInput{
			CertificateArn: &arn,
		})
		if err != nil {
			c.logCollectErr("ACM certificate "+arn, err)
			continue
		}
		if desc.Certificate != nil {
			data.ACMCertificates = append(data.ACMCertificates, *desc.Certificate)
		}
	}
	if len(data.ACMCertificates) > 0 {
		c.logger.Info("collected", "type", "ACM certificates", "count", len(data.ACMCertificates))
	}

	ramClient := ram.NewFromConfig(c.awsCfg)
	ramPager := ram.NewGetResourceSharesPaginator(ramClient, &ram.GetResourceSharesInput{
		ResourceOwner: ramtypes.ResourceOwnerSelf,
	})
	for ramPager.HasMorePages() {
		page, err := ramPager.NextPage(ctx)
		if err != nil {
			c.logCollectErr("RAM resource shares", err)
			break
		}
		data.RAMResourceShares = append(data.RAMResourceShares, page.ResourceShares...)
	}
	if len(data.RAMResourceShares) > 0 {
		c.logger.Info("collected", "type", "RAM resource shares", "count", len(data.RAMResourceShares))
	}
}

// collectGlobalResources fetches account-level global resources.
// Only called for GlobalRegion (us-east-1).
func (c *Client) collectGlobalResources(data *RegionData) {
	ctx := context.Background()

	// Route53 Hosted Zones
	r53Client := route53.NewFromConfig(c.awsCfg)
	if out, err := r53Client.ListHostedZones(ctx, &route53.ListHostedZonesInput{}); err != nil {
		c.logger.Debug("no Route53 hosted zones", "error", err)
	} else {
		data.Route53HostedZones = out.HostedZones
		if len(out.HostedZones) > 0 {
			c.logger.Info("collected", "type", "Route53 hosted zones", "count", len(out.HostedZones))
		}
	}

	// Global Accelerator (must use us-west-2 endpoint)
	gaCfg := c.awsCfg.Copy()
	gaCfg.Region = "us-west-2"
	gaClient := globalaccelerator.NewFromConfig(gaCfg)
	if out, err := gaClient.ListAccelerators(ctx, &globalaccelerator.ListAcceleratorsInput{}); err != nil {
		c.logger.Debug("no Global Accelerators", "error", err)
	} else {
		data.GlobalAccelerators = out.Accelerators
		if len(out.Accelerators) > 0 {
			c.logger.Info("collected", "type", "Global Accelerators", "count", len(out.Accelerators))
		}
	}

	// CloudFront Distributions (global CDN, collected via us-east-1)
	cfClient := cloudfront.NewFromConfig(c.awsCfg)
	cfPager := cloudfront.NewListDistributionsPaginator(cfClient, &cloudfront.ListDistributionsInput{})
	for cfPager.HasMorePages() {
		page, err := cfPager.NextPage(ctx)
		if err != nil {
			c.logCollectErr("CloudFront distributions", err)
			break
		}
		if page.DistributionList != nil {
			data.CloudFrontDistributions = append(data.CloudFrontDistributions, page.DistributionList.Items...)
		}
	}
	if len(data.CloudFrontDistributions) > 0 {
		c.logger.Info("collected", "type", "CloudFront distributions", "count", len(data.CloudFrontDistributions))
	}

	// IAM (truly global - no region dimension)
	iamClient := iam.NewFromConfig(c.awsCfg)

	// IAM Roles
	iamRolePager := iam.NewListRolesPaginator(iamClient, &iam.ListRolesInput{})
	for iamRolePager.HasMorePages() {
		page, err := iamRolePager.NextPage(ctx)
		if err != nil {
			c.logCollectErr("IAM roles", err)
			break
		}
		data.IAMRoles = append(data.IAMRoles, page.Roles...)
	}
	if len(data.IAMRoles) > 0 {
		c.logger.Info("collected", "type", "IAM roles", "count", len(data.IAMRoles))
	}

	// IAM Instance Profiles
	iamIPPager := iam.NewListInstanceProfilesPaginator(iamClient, &iam.ListInstanceProfilesInput{})
	for iamIPPager.HasMorePages() {
		page, err := iamIPPager.NextPage(ctx)
		if err != nil {
			c.logCollectErr("IAM instance profiles", err)
			break
		}
		data.IAMInstanceProfiles = append(data.IAMInstanceProfiles, page.InstanceProfiles...)
	}
	if len(data.IAMInstanceProfiles) > 0 {
		c.logger.Info("collected", "type", "IAM instance profiles", "count", len(data.IAMInstanceProfiles))
	}

	// IAM OIDC Providers
	oidcOut, err := iamClient.ListOpenIDConnectProviders(ctx, &iam.ListOpenIDConnectProvidersInput{})
	if err != nil {
		c.logCollectErr("IAM OIDC providers", err)
	} else {
		data.IAMOIDCProviders = oidcOut.OpenIDConnectProviderList
		if len(data.IAMOIDCProviders) > 0 {
			c.logger.Info("collected", "type", "IAM OIDC providers", "count", len(data.IAMOIDCProviders))
		}
	}

	// IAM SAML Providers
	samlOut, err := iamClient.ListSAMLProviders(ctx, &iam.ListSAMLProvidersInput{})
	if err != nil {
		c.logCollectErr("IAM SAML providers", err)
	} else {
		data.IAMSAMLProviders = samlOut.SAMLProviderList
		if len(data.IAMSAMLProviders) > 0 {
			c.logger.Info("collected", "type", "IAM SAML providers", "count", len(data.IAMSAMLProviders))
		}
	}
}

// tagMap converts AWS []types.Tag to a map[string]string.
// Tags whose keys match secret-scanning patterns are dropped entirely.
// Tags whose values match a known secret pattern have the value replaced
// with "[REDACTED]". This prevents the SDK fail-closed builder from aborting
// the entire document when a resource carries a tag that looks like a secret.
// Governance tags (apmcode, costcenter, environment, etc.) pass through
// unchanged because none of their keys match the sensitive-key patterns.
func tagMap(tags []ec2types.Tag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	m := make(map[string]string, len(tags))
	for _, t := range tags {
		key := aws.ToString(t.Key)
		if sdk.IsSensitiveKey(key) {
			continue
		}
		val := aws.ToString(t.Value)
		if sdk.ScanValue(val) != "" {
			val = "[REDACTED]"
		}
		m[key] = val
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

// tagName extracts the "Name" tag value from an AWS tag slice.
func tagName(tags []ec2types.Tag) string {
	for _, t := range tags {
		if aws.ToString(t.Key) == "Name" {
			return aws.ToString(t.Value)
		}
	}
	return ""
}
