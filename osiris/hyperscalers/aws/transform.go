// transform.go - Shared utilities and cross-domain helpers for the AWS OSIRIS JSON producer.
// All functions are stateless: no I/O, no API calls, just data transformation.
//
// Domain-specific transforms live in dedicated files:
//   transform_networking.go      - VPC, Subnet, SG, ENI, Route Table, IGW, NAT, EIP, NACL,
//                                  Prefix List, Flow Log, AZ, DHCP Options, VPC Endpoints,
//                                  VPC Peering, TGW, VPN, Direct Connect, Network Firewall,
//                                  Route53, Global Accelerator, ELBv2, Target Groups, Classic ELB
//                                  + all networking connections and groups
//   transform_networking_ext.go  - ELBv2 listeners, Direct Connect LAGs, VPC PrivateLink
//                                  endpoint services, API Gateway, CloudFront
//   transform_compute.go         - EKS, ECS, EC2 Auto Scaling Groups
//   transform_serverless.go      - Lambda, SQS, Kinesis, MSK
//   transform_data.go            - RDS, DynamoDB, ElastiCache
//   transform_databases_ext.go   - DocumentDB, Neptune, Redshift, OpenSearch, MemoryDB
//   transform_storage.go         - EBS volumes, S3, EFS, FSx
//   transform_security.go        - KMS, Secrets Manager, ECR, WAFv2, ACM, RAM
//   transform_iam.go             - IAM roles, instance profiles, OIDC/SAML providers
//   transform_observability.go   - CloudWatch log groups, AWS Backup vaults
//   transform_events.go          - SNS, EventBridge, Step Functions
//
// This file retains only cross-domain shared code:
//   - Resource ID builders: resourceID, resourceIDFromARN
//   - CloudFormation type map: cfTypeMap, cfType
//   - Provider builders: awsProvider, awsProviderWithZone
//   - Raw body passthrough: attachRawBody
//   - Tag utilities: sanitizeTags, tagMapDX
//   - Cross-account stubs: CrossAccountStubs and helpers
//
// [OSIRIS-JSON-AWS]: https://osirisjson.org/en/docs/producers/hyperscalers/amazon-aws
// [OSIRIS-JSON-SPEC-CH07]: https://osirisjson.org/en/docs/spec/v10/07-resourcetypetaxonomy
// [OSIRIS-JSON-PRODUCER-GUIDELINES]: https://osirisjson.org/en/docs/getting-started/producers/osiris-producer-guidelines

package aws

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	dctypes "github.com/aws/aws-sdk-go-v2/service/directconnect/types"
	"go.osirisjson.org/producers/pkg/sdk"
)

const (
	providerName = "aws"
)

// resourceID generates a deterministic resource ID from an AWS ARN or constructed ID.
// Per OSIRIS JSON Producer Guidelines section 2.2.1, hyperscaler resource IDs use the
// pattern: provider::native-id (e.g. aws::arn:aws:ec2:us-east-1:123456789012:vpc/vpc-123).
func resourceID(accountID, region, resourceType, nativeID string) string {
	return fmt.Sprintf("aws::arn:aws:%s:%s:%s:%s", resourceType, region, accountID, nativeID)
}

// resourceIDFromARN generates a resource ID directly from an existing ARN.
func resourceIDFromARN(arn string) string {
	return "aws::" + arn
}

// cfTypeMap maps internal AWS type identifiers (service:resource) to
// CloudFormation resource type strings (AWS::Service::ResourceType).
var cfTypeMap = map[string]string{
	// EC2
	"ec2:vpc":                          "AWS::EC2::VPC",
	"ec2:subnet":                       "AWS::EC2::Subnet",
	"ec2:security-group":               "AWS::EC2::SecurityGroup",
	"ec2:network-interface":            "AWS::EC2::NetworkInterface",
	"ec2:route-table":                  "AWS::EC2::RouteTable",
	"ec2:internet-gateway":             "AWS::EC2::InternetGateway",
	"ec2:nat-gateway":                  "AWS::EC2::NatGateway",
	"ec2:eip":                          "AWS::EC2::EIP",
	"ec2:instance":                     "AWS::EC2::Instance",
	"ec2:network-acl":                  "AWS::EC2::NetworkAcl",
	"ec2:vpc-endpoint":                 "AWS::EC2::VPCEndpoint",
	"ec2:vpc-endpoint-service":         "AWS::EC2::VPCEndpointService",
	"ec2:vpc-peering-connection":       "AWS::EC2::VPCPeeringConnection",
	"ec2:transit-gateway":              "AWS::EC2::TransitGateway",
	"ec2:transit-gateway-attachment":   "AWS::EC2::TransitGatewayAttachment",
	"ec2:transit-gateway-route-table":  "AWS::EC2::TransitGatewayRouteTable",
	"ec2:vpn-gateway":                  "AWS::EC2::VPNGateway",
	"ec2:vpn-connection":               "AWS::EC2::VPNConnection",
	"ec2:customer-gateway":             "AWS::EC2::CustomerGateway",
	"ec2:dhcp-options":                 "AWS::EC2::DHCPOptions",
	"ec2:egress-only-internet-gateway": "AWS::EC2::EgressOnlyInternetGateway",
	"ec2:managed-prefix-list":          "AWS::EC2::PrefixList",
	"ec2:flow-log":                     "AWS::EC2::FlowLog",
	"ec2:availability-zone":            "AWS::EC2::AvailabilityZone",
	"ec2:volume":                       "AWS::EC2::Volume",
	// ELB
	"elbv2:loadbalancer": "AWS::ElasticLoadBalancingV2::LoadBalancer",
	"elbv2:target-group": "AWS::ElasticLoadBalancingV2::TargetGroup",
	"elbv2:listener":     "AWS::ElasticLoadBalancingV2::Listener",
	"elb:loadbalancer":   "AWS::ElasticLoadBalancing::LoadBalancer",
	// DirectConnect
	"directconnect:connection": "AWS::DirectConnect::Connection",
	"directconnect:gateway":    "AWS::DirectConnect::Gateway",
	"directconnect:vif":        "AWS::DirectConnect::VirtualInterface",
	"directconnect:lag":        "AWS::DirectConnect::LAG",
	// Network Firewall
	"network-firewall:firewall": "AWS::NetworkFirewall::Firewall",
	// Route53
	"route53resolver:rule":     "AWS::Route53Resolver::ResolverRule",
	"route53resolver:endpoint": "AWS::Route53Resolver::ResolverEndpoint",
	"route53:hosted-zone":      "AWS::Route53::HostedZone",
	// Global Accelerator
	"globalaccelerator:accelerator": "AWS::GlobalAccelerator::Accelerator",
	// Compute orchestration
	"eks:cluster":       "AWS::EKS::Cluster",
	"eks:nodegroup":     "AWS::EKS::Nodegroup",
	"ecs:cluster":       "AWS::ECS::Cluster",
	"ecs:service":       "AWS::ECS::Service",
	"autoscaling:group": "AWS::AutoScaling::AutoScalingGroup",
	// Managed databases
	"rds:db-instance":               "AWS::RDS::DBInstance",
	"rds:cluster":                   "AWS::RDS::DBCluster",
	"rds:subnet-group":              "AWS::RDS::DBSubnetGroup",
	"dynamodb:table":                "AWS::DynamoDB::Table",
	"elasticache:replication-group": "AWS::ElastiCache::ReplicationGroup",
	"elasticache:subnet-group":      "AWS::ElastiCache::SubnetGroup",
	// Serverless + messaging
	"lambda:function": "AWS::Lambda::Function",
	"sqs:queue":       "AWS::SQS::Queue",
	"kinesis:stream":  "AWS::Kinesis::Stream",
	"msk:cluster":     "AWS::MSK::Cluster",
	// Storage
	"s3:bucket":      "AWS::S3::Bucket",
	"efs:filesystem": "AWS::EFS::FileSystem",
	"fsx:filesystem": "AWS::FSx::FileSystem",
	// Security + identity
	"kms:key":               "AWS::KMS::Key",
	"secretsmanager:secret": "AWS::SecretsManager::Secret",
	"ecr:repository":        "AWS::ECR::Repository",
	"wafv2:webacl":          "AWS::WAFv2::WebACL",
	"acm:certificate":       "AWS::CertificateManager::Certificate",
	"ram:resource-share":    "AWS::RAM::ResourceShare",
	// IAM
	"iam:role":             "AWS::IAM::Role",
	"iam:instance-profile": "AWS::IAM::InstanceProfile",
	"iam:oidc-provider":    "AWS::IAM::OIDCProvider",
	"iam:saml-provider":    "AWS::IAM::SAMLProvider",
	// Global networking + API
	"cloudfront:distribution": "AWS::CloudFront::Distribution",
	"apigateway:rest-api":     "AWS::ApiGateway::RestApi",
	"apigatewayv2:api":        "AWS::ApiGatewayV2::Api",
	// Observability + backup
	"logs:log-group": "AWS::Logs::LogGroup",
	"backup:vault":   "AWS::Backup::BackupVault",
	// Extended databases
	"docdb:cluster":         "AWS::DocDB::DBCluster",
	"docdb:subnet-group":    "AWS::DocDB::DBSubnetGroup",
	"neptune:cluster":       "AWS::Neptune::DBCluster",
	"neptune:subnet-group":  "AWS::Neptune::DBSubnetGroup",
	"redshift:cluster":      "AWS::Redshift::Cluster",
	"redshift:subnet-group": "AWS::Redshift::ClusterSubnetGroup",
	"opensearch:domain":     "AWS::OpenSearchService::Domain",
	"memorydb:cluster":      "AWS::MemoryDB::Cluster",
	"memorydb:subnet-group": "AWS::MemoryDB::SubnetGroup",
	// Event-driven
	"sns:topic":                   "AWS::SNS::Topic",
	"events:event-bus":            "AWS::Events::EventBus",
	"stepfunctions:state-machine": "AWS::StepFunctions::StateMachine",
}

// cfType converts an internal AWS type identifier to the CloudFormation
// resource type format. Unmapped types pass through unchanged.
func cfType(nativeType string) string {
	if t, ok := cfTypeMap[nativeType]; ok {
		return t
	}
	return nativeType
}

// awsProvider creates a Provider for an AWS resource.
func awsProvider(nativeID, nativeType, region, accountID string) sdk.Provider {
	return sdk.Provider{
		Name:     providerName,
		NativeID: nativeID,
		Type:     cfType(nativeType),
		Region:   region,
		Account:  accountID,
		Source:   "aws-sdk-go-v2",
	}
}

// awsProviderWithZone creates a Provider for an AWS resource with an availability zone.
func awsProviderWithZone(nativeID, nativeType, region, accountID, zone string) sdk.Provider {
	p := awsProvider(nativeID, nativeType, region, accountID)
	p.Zone = zone
	return p
}

// attachRawBody stores the unmodified AWS SDK response struct as a JSON string
// under extensions["osiris.aws.sdk"].body. Stored as a string (not a nested
// object) so the secret scanner does not recurse into SDK field names that
// happen to contain sensitive substrings (e.g. "MasterUsername" contains
// "auth" via "MasterUser"). Consumers that need the raw body parse the string.
// Call this unconditionally in every transform loop; the producer strips it
// from allResources when --include-raw-body is absent.
func attachRawBody(r *sdk.Resource, body any) {
	if r.Extensions == nil {
		r.Extensions = make(map[string]any)
	}
	var bodyStr string
	if body != nil {
		if b, err := json.Marshal(body); err == nil {
			bodyStr = string(b)
		}
	}
	r.Extensions["osiris.aws.sdk"] = map[string]any{
		"schema":      "raw-passthrough/v1",
		"api_version": "aws-sdk-go-v2",
		"kind":        "snapshot",
		"fetched_at":  time.Now().UTC().Format(time.RFC3339),
		"body":        bodyStr,
	}
}

// sanitizeTags filters a pre-built string->string tag map: sensitive key names
// are dropped entirely, values that look like secrets are replaced with
// "[REDACTED]". Returns nil when the result is empty. Use this before
// assigning any tag map to r.Tags so the SDK fail-closed scanner never
// sees sensitive material.
func sanitizeTags(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		if sdk.IsSensitiveKey(k) {
			continue
		}
		if sdk.ScanValue(v) != "" {
			v = "[REDACTED]"
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// tagMapDX converts Direct Connect tag slice to map.
// Applies the same secret-scan redaction as tagMap.
func tagMapDX(tags []dctypes.Tag) map[string]string {
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

// CrossAccountStubs returns stub resources for any connection endpoint that is
// not present in knownResources. This handles cross-account scenarios where a
// resource (e.g. a shared Transit Gateway or a cross-account VPC peering
// endpoint) belongs to a different AWS account than the one being collected,
// so the current profile cannot enumerate it.
//
// Stubs carry status "unknown" and state "cross-account" so consumers can
// distinguish them from natively-collected resources.
func CrossAccountStubs(conns []sdk.Connection, knownResources []sdk.Resource) []sdk.Resource {
	known := make(map[string]bool, len(knownResources))
	for _, r := range knownResources {
		known[r.ID] = true
	}

	seen := map[string]bool{}
	var stubs []sdk.Resource

	for _, c := range conns {
		for _, endpointID := range []string{c.Source, c.Target} {
			if known[endpointID] || seen[endpointID] {
				continue
			}
			seen[endpointID] = true
			stubs = append(stubs, crossAccountStub(endpointID))
		}
	}
	return stubs
}

// crossAccountStub builds a minimal OSIRIS JSON resource for a cross-account endpoint.
func crossAccountStub(id string) sdk.Resource {
	svc, rType, region, account, nativeID := parseStubARN(id)
	osirisType := crossAccountOSIRISType(svc, rType)
	providerType := svc + ":" + rType

	desc := "Cross-account resource (not accessible via current profile"
	if account != "" {
		desc += "; owner account " + account
	}
	desc += ")"

	return sdk.Resource{
		ID:          id,
		Type:        osirisType,
		Name:        nativeID,
		Description: desc,
		Provider: sdk.Provider{
			Name:     providerName,
			Type:     providerType,
			NativeID: nativeID,
			Region:   region,
			Account:  account,
			Source:   "aws-sdk-go-v2",
		},
		Status: "unknown",
		State:  "cross-account",
		Properties: map[string]any{
			"cross_account":    true,
			"owner_account_id": account,
		},
	}
}

// parseStubARN extracts fields from a resource ID of the form
// "aws::arn:aws:<service>:<region>:<account>:<type>/<native-id>".
func parseStubARN(id string) (svc, rType, region, account, nativeID string) {
	arn := strings.TrimPrefix(id, "aws::")
	parts := strings.SplitN(arn, ":", 6)
	if len(parts) != 6 {
		return "", "", "", "", id
	}
	svc = parts[2]
	region = parts[3]
	account = parts[4]
	resourcePart := parts[5]
	if before, after, ok := strings.Cut(resourcePart, "/"); ok {
		rType = before
		nativeID = after
	} else {
		rType = resourcePart
		nativeID = resourcePart
	}
	return
}

// crossAccountOSIRISType maps an AWS service+resource-type pair to an OSIRIS JSON type.
func crossAccountOSIRISType(svc, rType string) string {
	switch svc + ":" + rType {
	case "ec2:transit-gateway":
		return "osiris.aws.transitgateway"
	case "ec2:vpc":
		return "network.vpc"
	case "ec2:subnet":
		return "network.subnet"
	case "directconnect:gateway":
		return "osiris.aws.directconnect.gateway"
	default:
		return "network.node"
	}
}
