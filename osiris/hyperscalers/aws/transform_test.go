// transform_test.go - Unit tests for Amazon Web Services data transformation to OSIRIS JSON mapping functions.
//
// For an introduction to OSIRIS JSON Producer for Amazon Web Services see:
// [OSIRIS-JSON-AWS]: https://osirisjson.org/en/docs/producers/hyperscalers/amazon-aws
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/docs/spec/v10/00-preface

package aws

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	dctypes "github.com/aws/aws-sdk-go-v2/service/directconnect/types"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
)

const (
	testAccountID = "123456789012"
	testRegion    = "eu-west-1"
)

func TestTransformVPCs(t *testing.T) {
	vpcs := []ec2types.Vpc{
		{
			VpcId:     aws.String("vpc-0a1b2c3d4e5f00001"),
			CidrBlock: aws.String("10.0.0.0/24"),
			IsDefault: aws.Bool(false),
			OwnerId:   aws.String(testAccountID),
			State:     ec2types.VpcStateAvailable,
			Tags: []ec2types.Tag{
				{Key: aws.String("Name"), Value: aws.String("My_Test_VPC")},
			},
		},
	}

	resources := TransformVPCs(vpcs, nil, testRegion, testAccountID)
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}

	r := resources[0]
	if r.Type != "network.vpc" {
		t.Errorf("expected type network.vpc, got %s", r.Type)
	}
	if r.Name != "My_Test_VPC" {
		t.Errorf("expected name My_Test_VPC, got %s", r.Name)
	}
	if r.Provider.Name != "aws" {
		t.Errorf("expected provider aws, got %s", r.Provider.Name)
	}
	if r.Provider.Region != testRegion {
		t.Errorf("expected region %s, got %s", testRegion, r.Provider.Region)
	}
	if r.Provider.Account != testAccountID {
		t.Errorf("expected account %s, got %s", testAccountID, r.Provider.Account)
	}
	if r.Status != "active" {
		t.Errorf("expected status active, got %s", r.Status)
	}
	cidr, ok := r.Properties["cidr_block"].(string)
	if !ok || cidr != "10.0.0.0/24" {
		t.Errorf("expected cidr_block 10.0.0.0/24, got %v", r.Properties["cidr_block"])
	}
}

func TestTransformSubnets(t *testing.T) {
	subnets := []ec2types.Subnet{
		{
			SubnetId:                aws.String("subnet-0a1b2c3d4e5f0001"),
			VpcId:                   aws.String("vpc-0a1b2c3d4e5f00002"),
			CidrBlock:               aws.String("10.0.1.0/28"),
			AvailabilityZone:        aws.String("eu-central-1a"),
			AvailableIpAddressCount: aws.Int32(9),
			Tags: []ec2types.Tag{
				{Key: aws.String("Name"), Value: aws.String("NAT subnet 1")},
			},
		},
	}

	resources, idMap := TransformSubnets(subnets, testRegion, testAccountID)
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	if len(idMap) != 1 {
		t.Fatalf("expected 1 ID map entry, got %d", len(idMap))
	}

	r := resources[0]
	if r.Type != "network.subnet" {
		t.Errorf("expected type network.subnet, got %s", r.Type)
	}
	if r.Name != "NAT subnet 1" {
		t.Errorf("expected name NAT subnet 1, got %s", r.Name)
	}

	// Verify ID map.
	if _, ok := idMap["subnet-0a1b2c3d4e5f0001"]; !ok {
		t.Error("expected subnet ID in map")
	}
}

func TestTransformSecurityGroups(t *testing.T) {
	sgs := []ec2types.SecurityGroup{
		{
			GroupId:     aws.String("sg-0a1b2c3d4e5f00001"),
			GroupName:   aws.String("default"),
			VpcId:       aws.String("vpc-0a1b2c3d4e5f00001"),
			Description: aws.String("default VPC security group"),
			OwnerId:     aws.String(testAccountID),
		},
	}

	resources, idMap := TransformSecurityGroups(sgs, testRegion, testAccountID)
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}

	r := resources[0]
	if r.Type != "network.security.group" {
		t.Errorf("expected type network.security.group, got %s", r.Type)
	}
	if r.Name != "default" {
		t.Errorf("expected name default, got %s", r.Name)
	}

	if _, ok := idMap["sg-0a1b2c3d4e5f00001"]; !ok {
		t.Error("expected SG ID in map")
	}
}

func TestTransformInstances(t *testing.T) {
	instances := []ec2types.Instance{
		{
			InstanceId:       aws.String("i-0a1b2c3d4e5f00001"),
			InstanceType:     ec2types.InstanceTypeC5Xlarge,
			VpcId:            aws.String("vpc-0a1b2c3d4e5f00003"),
			SubnetId:         aws.String("subnet-0a1b2c3d4e5f0002"),
			PrivateIpAddress: aws.String("10.0.2.10"),
			PublicIpAddress:  aws.String("203.0.113.10"),
			State: &ec2types.InstanceState{
				Name: ec2types.InstanceStateNameRunning,
			},
			Tags: []ec2types.Tag{
				{Key: aws.String("Name"), Value: aws.String("demo-vm-1")},
			},
		},
	}

	resources := TransformInstances(instances, testRegion, testAccountID)
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}

	r := resources[0]
	if r.Type != "compute.vm" {
		t.Errorf("expected type compute.vm, got %s", r.Type)
	}
	if r.Name != "demo-vm-1" {
		t.Errorf("expected name demo-vm-1, got %s", r.Name)
	}
	if r.Status != "active" {
		t.Errorf("expected status active, got %s", r.Status)
	}
}

func TestTransformTransitGateways(t *testing.T) {
	tgws := []ec2types.TransitGateway{
		{
			TransitGatewayId:  aws.String("tgw-0a1b2c3d4e5f0001"),
			TransitGatewayArn: aws.String("arn:aws:ec2:eu-central-1:234567890123:transit-gateway/tgw-0a1b2c3d4e5f0001"),
			State:             ec2types.TransitGatewayStateAvailable,
			OwnerId:           aws.String("234567890123"),
			Description:       aws.String("Transit Gateway EUR region"),
			Options: &ec2types.TransitGatewayOptions{
				AmazonSideAsn: aws.Int64(65531),
				DnsSupport:    ec2types.DnsSupportValueEnable,
			},
		},
	}

	resources := TransformTransitGateways(tgws, testRegion, testAccountID)
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}

	r := resources[0]
	if r.Type != "osiris.aws.transitgateway" {
		t.Errorf("expected type osiris.aws.transitgateway, got %s", r.Type)
	}
	if r.Status != "active" {
		t.Errorf("expected status active, got %s", r.Status)
	}
	if r.Name != "Transit Gateway EUR region" {
		t.Errorf("expected description as name, got %s", r.Name)
	}
}

func TestTransformDirectConnectConnections(t *testing.T) {
	conns := []dctypes.Connection{
		{
			ConnectionId:    aws.String("dxcon-0a1b2c3d"),
			ConnectionName:  aws.String("DEMO-DC1-TO-EU-AWS"),
			ConnectionState: dctypes.ConnectionStateAvailable,
			Location:        aws.String("MXP-LOC1"),
			Bandwidth:       aws.String("50Mbps"),
			Vlan:            100,
			PartnerName:     aws.String("DEMO-ISP NNI"),
		},
	}

	resources := TransformDirectConnectConnections(conns, testRegion, testAccountID)
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}

	r := resources[0]
	if r.Type != "osiris.aws.directconnect" {
		t.Errorf("expected type osiris.aws.directconnect, got %s", r.Type)
	}
	if r.Status != "active" {
		t.Errorf("expected status active, got %s", r.Status)
	}
	if r.Name != "DEMO-DC1-TO-EU-AWS" {
		t.Errorf("expected name DEMO-DC1-TO-EU-AWS, got %s", r.Name)
	}
}

func TestTransformLoadBalancersV2(t *testing.T) {
	lbs := []elbv2types.LoadBalancer{
		{
			LoadBalancerArn:  aws.String("arn:aws:elasticloadbalancing:eu-central-1:345678901234:loadbalancer/gwy/demo-gwlb-1/a1b2c3d4e5f60000"),
			LoadBalancerName: aws.String("demo-gwlb-1"),
			VpcId:            aws.String("vpc-0a1b2c3d4e5f00003"),
			Type:             elbv2types.LoadBalancerTypeEnumGateway,
			State: &elbv2types.LoadBalancerState{
				Code: elbv2types.LoadBalancerStateEnumActive,
			},
			AvailabilityZones: []elbv2types.AvailabilityZone{
				{ZoneName: aws.String("eu-central-1b"), SubnetId: aws.String("subnet-0a1b2c3d4e5f0002")},
			},
		},
	}

	resources := TransformLoadBalancersV2(lbs, testRegion, testAccountID)
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}

	r := resources[0]
	if r.Type != "network.loadbalancer" {
		t.Errorf("expected type network.loadbalancer, got %s", r.Type)
	}
	if r.Name != "demo-gwlb-1" {
		t.Errorf("expected name demo-gwlb-1, got %s", r.Name)
	}
	if r.Status != "active" {
		t.Errorf("expected status active, got %s", r.Status)
	}
}

func TestTransformSubnetToVPCConnections(t *testing.T) {
	subnets := []ec2types.Subnet{
		{
			SubnetId: aws.String("subnet-001"),
			VpcId:    aws.String("vpc-001"),
		},
	}
	subnetIDMap := map[string]string{
		"subnet-001": resourceID(testAccountID, testRegion, "ec2", "subnet/subnet-001"),
	}

	conns := TransformSubnetToVPCConnections(subnets, subnetIDMap, testRegion, testAccountID)
	if len(conns) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(conns))
	}

	c := conns[0]
	if c.Type != "contains" {
		t.Errorf("expected type contains, got %s", c.Type)
	}
	if c.Direction != "forward" {
		t.Errorf("expected direction forward, got %s", c.Direction)
	}
}

func TestTransformIGWToVPCConnections(t *testing.T) {
	igws := []ec2types.InternetGateway{
		{
			InternetGatewayId: aws.String("igw-001"),
			Attachments: []ec2types.InternetGatewayAttachment{
				{VpcId: aws.String("vpc-001"), State: ec2types.AttachmentStatusAttached},
			},
		},
	}

	conns := TransformIGWToVPCConnections(igws, testRegion, testAccountID)
	if len(conns) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(conns))
	}

	c := conns[0]
	if c.Type != "network" {
		t.Errorf("expected type network, got %s", c.Type)
	}
}

func TestTransformAccountGroup(t *testing.T) {
	g := TransformAccountGroup(testAccountID)
	if g.Type != "osiris.aws.account" {
		t.Errorf("expected type osiris.aws.account, got %s", g.Type)
	}
	if g.Name == "" {
		t.Error("expected non-empty name")
	}
}

func TestTransformVPCGroups(t *testing.T) {
	vpcs := []ec2types.Vpc{
		{
			VpcId: aws.String("vpc-001"),
			Tags: []ec2types.Tag{
				{Key: aws.String("Name"), Value: aws.String("test-vpc")},
			},
		},
	}

	groups, vpcGroupMap := TransformVPCGroups(vpcs, testRegion, testAccountID)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if len(vpcGroupMap) != 1 {
		t.Fatalf("expected 1 VPC group map entry, got %d", len(vpcGroupMap))
	}

	g := groups[0]
	if g.Type != "network.vpc" {
		t.Errorf("expected type network.vpc, got %s", g.Type)
	}
	if g.Name != "VPC test-vpc" {
		t.Errorf("expected name VPC test-vpc, got %s", g.Name)
	}
}

func TestResourceIDPattern(t *testing.T) {
	id := resourceID("123456789012", "us-east-1", "ec2", "vpc/vpc-abc123")
	expected := "aws::arn:aws:ec2:us-east-1:123456789012:vpc/vpc-abc123"
	if id != expected {
		t.Errorf("expected %s, got %s", expected, id)
	}
}

func TestResourceIDFromARN(t *testing.T) {
	arn := "arn:aws:elasticloadbalancing:eu-central-1:345678901234:loadbalancer/gwy/test/abc"
	id := resourceIDFromARN(arn)
	expected := "aws::" + arn
	if id != expected {
		t.Errorf("expected %s, got %s", expected, id)
	}
}

func TestTagMap(t *testing.T) {
	tags := []ec2types.Tag{
		{Key: aws.String("Name"), Value: aws.String("test")},
		{Key: aws.String("env"), Value: aws.String("prod")},
	}

	m := tagMap(tags)
	if m["Name"] != "test" {
		t.Errorf("expected Name=test, got %s", m["Name"])
	}
	if m["env"] != "prod" {
		t.Errorf("expected env=prod, got %s", m["env"])
	}
}

func TestTagMapNil(t *testing.T) {
	m := tagMap(nil)
	if m != nil {
		t.Errorf("expected nil for empty tags, got %v", m)
	}
}

func TestTagName(t *testing.T) {
	tags := []ec2types.Tag{
		{Key: aws.String("env"), Value: aws.String("prod")},
		{Key: aws.String("Name"), Value: aws.String("my-resource")},
	}
	if got := tagName(tags); got != "my-resource" {
		t.Errorf("expected my-resource, got %s", got)
	}
}

func TestTagNameMissing(t *testing.T) {
	tags := []ec2types.Tag{
		{Key: aws.String("env"), Value: aws.String("prod")},
	}
	if got := tagName(tags); got != "" {
		t.Errorf("expected empty string, got %s", got)
	}
}

func TestMapInstanceState(t *testing.T) {
	tests := []struct {
		state  *ec2types.InstanceState
		expect string
	}{
		{&ec2types.InstanceState{Name: ec2types.InstanceStateNameRunning}, "active"},
		{&ec2types.InstanceState{Name: ec2types.InstanceStateNameStopped}, "inactive"},
		{&ec2types.InstanceState{Name: ec2types.InstanceStateNameTerminated}, "decommissioned"},
		{&ec2types.InstanceState{Name: ec2types.InstanceStateNamePending}, "provisioning"},
		{nil, "unknown"},
	}

	for _, tt := range tests {
		got := mapInstanceState(tt.state)
		if got != tt.expect {
			name := "nil"
			if tt.state != nil {
				name = string(tt.state.Name)
			}
			t.Errorf("mapInstanceState(%s) = %s, want %s", name, got, tt.expect)
		}
	}
}
