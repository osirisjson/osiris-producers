// transform_networking.go - Core networking resource and connection transforms.
// Maps the full AWS networking stack to OSIRIS JSON types following the spec
// chapter 7 type taxonomy.
//
// Standard types (OSIRIS JSON spec chapter 7):
//   ec2:vpc                              -> network.vpc               (7.1.1)
//   ec2:subnet                           -> network.subnet            (7.1.2)
//   ec2:security-group                   -> network.security.group    (7.1.5)
//   ec2:network-interface                -> network.interface         (7.1.3)
//   elbv2:loadbalancer                   -> network.loadbalancer      (7.1.7)
//   elb:loadbalancer                     -> network.loadbalancer      (7.1.7)
//   network-firewall:firewall            -> network.firewall          (7.1.6)
//
// Custom types (osiris.aws.* namespace):
//   ec2:route-table                      -> osiris.aws.routetable
//   ec2:internet-gateway                 -> osiris.aws.gateway.internet
//   ec2:nat-gateway                      -> osiris.aws.gateway.nat
//   ec2:vpn-gateway                      -> osiris.aws.gateway.vpn
//   ec2:customer-gateway                 -> osiris.aws.gateway.customer
//   ec2:eip                              -> osiris.aws.elasticip
//   ec2:vpc-endpoint                     -> osiris.aws.vpc.endpoint
//   ec2:transit-gateway                  -> osiris.aws.transitgateway
//   ec2:transit-gateway-attachment       -> osiris.aws.transitgateway.attachment
//   ec2:transit-gateway-route-table      -> osiris.aws.transitgateway.routetable
//   ec2:network-acl                      -> osiris.aws.nacl
//   directconnect:connection             -> osiris.aws.directconnect
//   directconnect:gateway                -> osiris.aws.directconnect.gateway
//   directconnect:vif                    -> osiris.aws.directconnect.vif
//   ec2:vpn-connection                   -> osiris.aws.vpn.connection
//   ec2:dhcp-options                     -> osiris.aws.dhcpoptions
//   ec2:vpc-peering-connection           -> osiris.aws.vpc.peering
//   ec2:egress-only-internet-gateway     -> osiris.aws.gateway.egressonly
//   ec2:managed-prefix-list              -> osiris.aws.prefixlist
//   ec2:flow-log                         -> osiris.aws.flowlog
//   ec2:availability-zone                -> osiris.aws.availabilityzone
//   elbv2:target-group                   -> osiris.aws.targetgroup
//   route53resolver:rule                 -> osiris.aws.resolver.rule
//   route53resolver:endpoint             -> osiris.aws.resolver.endpoint
//   route53:hosted-zone                  -> osiris.aws.route53.zone
//   globalaccelerator:accelerator        -> osiris.aws.globalaccelerator
//
// [OSIRIS-JSON-AWS]: https://osirisjson.org/en/docs/producers/hyperscalers/amazon-aws
// [OSIRIS-JSON-SPEC-CH07]: https://osirisjson.org/en/docs/spec/v10/07-resourcetypetaxonomy

package aws

import (
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	dctypes "github.com/aws/aws-sdk-go-v2/service/directconnect/types"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	elbtypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing/types"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	gatypes "github.com/aws/aws-sdk-go-v2/service/globalaccelerator/types"
	nfwtypes "github.com/aws/aws-sdk-go-v2/service/networkfirewall/types"
	r53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
	r53rtypes "github.com/aws/aws-sdk-go-v2/service/route53resolver/types"
	"go.osirisjson.org/producers/pkg/sdk"
)

// TransformVPCs converts AWS VPCs into OSIRIS JSON resources.
// dnsAttrs is an optional map of vpcID -> dns attribute bools (from DescribeVpcAttribute calls).
func TransformVPCs(vpcs []ec2types.Vpc, dnsAttrs map[string]map[string]bool, region, accountID string) []sdk.Resource {
	var resources []sdk.Resource
	for _, v := range vpcs {
		vpcID := aws.ToString(v.VpcId)
		id := resourceID(accountID, region, "ec2", "vpc/"+vpcID)
		prov := awsProvider(vpcID, "ec2:vpc", region, accountID)

		r, err := sdk.NewResource(id, "network.vpc", prov)
		if err != nil {
			continue
		}
		name := tagName(v.Tags)
		if name != "" {
			r.Name = name
		} else {
			r.Name = vpcID
		}
		r.Status = "active"
		r.Tags = tagMap(v.Tags)

		props := map[string]any{
			"cidr_block": aws.ToString(v.CidrBlock),
			"is_default": aws.ToBool(v.IsDefault),
			"owner_id":   aws.ToString(v.OwnerId),
		}
		if v.DhcpOptionsId != nil {
			props["dhcp_options_id"] = aws.ToString(v.DhcpOptionsId)
		}
		if len(v.CidrBlockAssociationSet) > 1 {
			cidrs := make([]string, 0, len(v.CidrBlockAssociationSet))
			for _, assoc := range v.CidrBlockAssociationSet {
				cidrs = append(cidrs, aws.ToString(assoc.CidrBlock))
			}
			props["cidr_blocks"] = cidrs
		}
		if attrs, ok := dnsAttrs[vpcID]; ok {
			if v, ok := attrs["enableDnsHostnames"]; ok {
				props["enable_dns_hostnames"] = v
			}
			if v, ok := attrs["enableDnsSupport"]; ok {
				props["enable_dns_support"] = v
			}
		}
		r.Properties = props
		attachRawBody(&r, &v)
		resources = append(resources, r)
	}
	return resources
}

// TransformSubnets converts AWS Subnets into OSIRIS JSON resources.
func TransformSubnets(subnets []ec2types.Subnet, region, accountID string) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(subnets))

	for _, s := range subnets {
		subnetID := aws.ToString(s.SubnetId)
		id := resourceID(accountID, region, "ec2", "subnet/"+subnetID)
		idMap[subnetID] = id
		prov := awsProviderWithZone(subnetID, "ec2:subnet", region, accountID, aws.ToString(s.AvailabilityZone))

		r, err := sdk.NewResource(id, "network.subnet", prov)
		if err != nil {
			continue
		}
		name := tagName(s.Tags)
		if name != "" {
			r.Name = name
		} else {
			r.Name = subnetID
		}
		r.Status = "active"
		r.Tags = tagMap(s.Tags)

		props := map[string]any{
			"cidr_block":        aws.ToString(s.CidrBlock),
			"availability_zone": aws.ToString(s.AvailabilityZone),
			"vpc_id":            aws.ToString(s.VpcId),
			"available_ips":     aws.ToInt32(s.AvailableIpAddressCount),
		}
		if aws.ToBool(s.MapPublicIpOnLaunch) {
			props["map_public_ip_on_launch"] = true
		}
		r.Properties = props
		attachRawBody(&r, &s)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformSecurityGroups converts AWS Security Groups into OSIRIS JSON resources.
func TransformSecurityGroups(sgs []ec2types.SecurityGroup, region, accountID string) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(sgs))

	for _, sg := range sgs {
		sgID := aws.ToString(sg.GroupId)
		id := resourceID(accountID, region, "ec2", "security-group/"+sgID)
		idMap[sgID] = id
		prov := awsProvider(sgID, "ec2:security-group", region, accountID)

		r, err := sdk.NewResource(id, "network.security.group", prov)
		if err != nil {
			continue
		}
		name := aws.ToString(sg.GroupName)
		if n := tagName(sg.Tags); n != "" {
			name = n
		}
		r.Name = name
		r.Status = "active"
		r.Tags = tagMap(sg.Tags)

		props := map[string]any{
			"group_name":  aws.ToString(sg.GroupName),
			"vpc_id":      aws.ToString(sg.VpcId),
			"description": aws.ToString(sg.Description),
		}
		r.Properties = props

		// Extensions: SG ingress/egress rules.
		ext := map[string]any{}
		if len(sg.IpPermissions) > 0 {
			ingress := make([]map[string]any, 0, len(sg.IpPermissions))
			for _, p := range sg.IpPermissions {
				rule := map[string]any{"protocol": aws.ToString(p.IpProtocol)}
				if p.FromPort != nil {
					rule["from_port"] = aws.ToInt32(p.FromPort)
				}
				if p.ToPort != nil {
					rule["to_port"] = aws.ToInt32(p.ToPort)
				}
				if len(p.IpRanges) > 0 {
					cidrs := make([]string, 0, len(p.IpRanges))
					for _, r := range p.IpRanges {
						cidrs = append(cidrs, aws.ToString(r.CidrIp))
					}
					rule["cidr_blocks"] = cidrs
				}
				if len(p.UserIdGroupPairs) > 0 {
					sgRefs := make([]string, 0, len(p.UserIdGroupPairs))
					for _, g := range p.UserIdGroupPairs {
						sgRefs = append(sgRefs, aws.ToString(g.GroupId))
					}
					rule["security_groups"] = sgRefs
				}
				ingress = append(ingress, rule)
			}
			ext["ingress_rules"] = ingress
		}
		if len(sg.IpPermissionsEgress) > 0 {
			egress := make([]map[string]any, 0, len(sg.IpPermissionsEgress))
			for _, p := range sg.IpPermissionsEgress {
				rule := map[string]any{"protocol": aws.ToString(p.IpProtocol)}
				if p.FromPort != nil {
					rule["from_port"] = aws.ToInt32(p.FromPort)
				}
				if p.ToPort != nil {
					rule["to_port"] = aws.ToInt32(p.ToPort)
				}
				if len(p.IpRanges) > 0 {
					cidrs := make([]string, 0, len(p.IpRanges))
					for _, r := range p.IpRanges {
						cidrs = append(cidrs, aws.ToString(r.CidrIp))
					}
					rule["cidr_blocks"] = cidrs
				}
				if len(p.UserIdGroupPairs) > 0 {
					sgRefs := make([]string, 0, len(p.UserIdGroupPairs))
					for _, g := range p.UserIdGroupPairs {
						sgRefs = append(sgRefs, aws.ToString(g.GroupId))
					}
					rule["security_groups"] = sgRefs
				}
				egress = append(egress, rule)
			}
			ext["egress_rules"] = egress
		}
		if len(ext) > 0 {
			r.Extensions = map[string]any{"osiris.aws": ext}
		}
		attachRawBody(&r, &sg)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformNetworkInterfaces converts AWS ENIs into OSIRIS JSON resources.
func TransformNetworkInterfaces(enis []ec2types.NetworkInterface, region, accountID string) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(enis))

	for _, eni := range enis {
		eniID := aws.ToString(eni.NetworkInterfaceId)
		id := resourceID(accountID, region, "ec2", "network-interface/"+eniID)
		idMap[eniID] = id
		prov := awsProviderWithZone(eniID, "ec2:network-interface", region, accountID, aws.ToString(eni.AvailabilityZone))

		r, err := sdk.NewResource(id, "network.interface", prov)
		if err != nil {
			continue
		}
		name := tagName(eni.TagSet)
		if name == "" {
			name = eniID
		}
		r.Name = name
		r.Status = mapENIStatus(eni.Status)
		r.Tags = tagMap(eni.TagSet)

		props := map[string]any{
			"vpc_id":         aws.ToString(eni.VpcId),
			"subnet_id":      aws.ToString(eni.SubnetId),
			"interface_type": string(eni.InterfaceType),
			"private_ip":     aws.ToString(eni.PrivateIpAddress),
		}
		if eni.Description != nil && *eni.Description != "" {
			props["description"] = aws.ToString(eni.Description)
		}
		if eni.AvailabilityZone != nil {
			props["availability_zone"] = aws.ToString(eni.AvailabilityZone)
		}

		// Collect security group IDs for connection wiring.
		if len(eni.Groups) > 0 {
			sgIDs := make([]string, 0, len(eni.Groups))
			for _, g := range eni.Groups {
				sgIDs = append(sgIDs, aws.ToString(g.GroupId))
			}
			props["security_groups"] = sgIDs
		}
		r.Properties = props
		attachRawBody(&r, &eni)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformRouteTables converts AWS Route Tables into OSIRIS JSON resources.
func TransformRouteTables(rts []ec2types.RouteTable, region, accountID string) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(rts))

	for _, rt := range rts {
		rtID := aws.ToString(rt.RouteTableId)
		id := resourceID(accountID, region, "ec2", "route-table/"+rtID)
		idMap[rtID] = id
		prov := awsProvider(rtID, "ec2:route-table", region, accountID)

		r, err := sdk.NewResource(id, "osiris.aws.routetable", prov)
		if err != nil {
			continue
		}
		name := tagName(rt.Tags)
		if name == "" {
			name = rtID
		}
		r.Name = name
		r.Status = "active"
		r.Tags = tagMap(rt.Tags)

		props := map[string]any{
			"vpc_id":      aws.ToString(rt.VpcId),
			"route_count": len(rt.Routes),
		}

		// Track subnet associations.
		var assocSubnets []string
		for _, assoc := range rt.Associations {
			if assoc.SubnetId != nil {
				assocSubnets = append(assocSubnets, aws.ToString(assoc.SubnetId))
			}
			if aws.ToBool(assoc.Main) {
				props["is_main"] = true
			}
		}
		if len(assocSubnets) > 0 {
			props["associated_subnets"] = assocSubnets
		}

		if len(rt.Routes) > 0 {
			routes := make([]map[string]any, 0, len(rt.Routes))
			for _, route := range rt.Routes {
				entry := map[string]any{}
				if route.DestinationCidrBlock != nil {
					entry["destination"] = aws.ToString(route.DestinationCidrBlock)
				}
				if route.DestinationIpv6CidrBlock != nil {
					entry["destination_ipv6"] = aws.ToString(route.DestinationIpv6CidrBlock)
				}
				if route.DestinationPrefixListId != nil {
					entry["destination_prefix_list_id"] = aws.ToString(route.DestinationPrefixListId)
				}
				if route.GatewayId != nil {
					entry["gateway_id"] = aws.ToString(route.GatewayId)
				}
				if route.NatGatewayId != nil {
					entry["nat_gateway_id"] = aws.ToString(route.NatGatewayId)
				}
				if route.TransitGatewayId != nil {
					entry["transit_gateway_id"] = aws.ToString(route.TransitGatewayId)
				}
				if route.VpcPeeringConnectionId != nil {
					entry["vpc_peering_connection_id"] = aws.ToString(route.VpcPeeringConnectionId)
				}
				if route.NetworkInterfaceId != nil {
					entry["network_interface_id"] = aws.ToString(route.NetworkInterfaceId)
				}
				entry["state"] = string(route.State)
				routes = append(routes, entry)
			}
			props["routes"] = routes
		}
		r.Properties = props
		attachRawBody(&r, &rt)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformInternetGateways converts AWS Internet Gateways into OSIRIS JSON resources.
func TransformInternetGateways(igws []ec2types.InternetGateway, region, accountID string) []sdk.Resource {
	var resources []sdk.Resource
	for _, igw := range igws {
		igwID := aws.ToString(igw.InternetGatewayId)
		id := resourceID(accountID, region, "ec2", "internet-gateway/"+igwID)
		prov := awsProvider(igwID, "ec2:internet-gateway", region, accountID)

		r, err := sdk.NewResource(id, "osiris.aws.gateway.internet", prov)
		if err != nil {
			continue
		}
		name := tagName(igw.Tags)
		if name == "" {
			name = igwID
		}
		r.Name = name
		r.Status = "active"
		r.Tags = tagMap(igw.Tags)

		if len(igw.Attachments) > 0 {
			vpcIDs := make([]string, 0, len(igw.Attachments))
			for _, att := range igw.Attachments {
				vpcIDs = append(vpcIDs, aws.ToString(att.VpcId))
			}
			r.Properties = map[string]any{
				"attached_vpcs": vpcIDs,
			}
		}
		resources = append(resources, r)
	}
	return resources
}

// TransformNATGateways converts AWS NAT Gateways into OSIRIS JSON resources.
func TransformNATGateways(ngws []ec2types.NatGateway, region, accountID string) []sdk.Resource {
	var resources []sdk.Resource
	for _, ngw := range ngws {
		ngwID := aws.ToString(ngw.NatGatewayId)
		id := resourceID(accountID, region, "ec2", "nat-gateway/"+ngwID)
		prov := awsProvider(ngwID, "ec2:nat-gateway", region, accountID)

		r, err := sdk.NewResource(id, "osiris.aws.gateway.nat", prov)
		if err != nil {
			continue
		}
		name := tagName(ngw.Tags)
		if name == "" {
			name = ngwID
		}
		r.Name = name
		r.Status = mapNATGatewayState(ngw.State)
		r.Tags = tagMap(ngw.Tags)

		props := map[string]any{
			"vpc_id":            aws.ToString(ngw.VpcId),
			"subnet_id":         aws.ToString(ngw.SubnetId),
			"connectivity_type": string(ngw.ConnectivityType),
		}
		if len(ngw.NatGatewayAddresses) > 0 {
			var publicIPs []string
			var eipAllocIDs []string
			for _, addr := range ngw.NatGatewayAddresses {
				if addr.PublicIp != nil {
					publicIPs = append(publicIPs, aws.ToString(addr.PublicIp))
				}
				if addr.AllocationId != nil {
					eipAllocIDs = append(eipAllocIDs, aws.ToString(addr.AllocationId))
				}
			}
			if len(publicIPs) > 0 {
				props["public_ips"] = publicIPs
			}
			if len(eipAllocIDs) > 0 {
				props["eip_allocation_ids"] = eipAllocIDs
			}
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources
}

// TransformElasticIPs converts AWS Elastic IPs into OSIRIS JSON resources.
func TransformElasticIPs(eips []ec2types.Address, region, accountID string) []sdk.Resource {
	var resources []sdk.Resource
	for _, eip := range eips {
		allocID := aws.ToString(eip.AllocationId)
		id := resourceID(accountID, region, "ec2", "elastic-ip/"+allocID)
		prov := awsProvider(allocID, "ec2:eip", region, accountID)

		r, err := sdk.NewResource(id, "osiris.aws.elasticip", prov)
		if err != nil {
			continue
		}
		name := tagName(eip.Tags)
		if name == "" {
			name = aws.ToString(eip.PublicIp)
		}
		r.Name = name
		r.Status = "active"
		r.Tags = tagMap(eip.Tags)

		props := map[string]any{
			"domain": string(eip.Domain),
		}
		if eip.PublicIp != nil {
			props["public_ip"] = aws.ToString(eip.PublicIp)
		}
		if eip.PrivateIpAddress != nil {
			props["private_ip"] = aws.ToString(eip.PrivateIpAddress)
		}
		if eip.AssociationId != nil {
			props["association_id"] = aws.ToString(eip.AssociationId)
		}
		if eip.NetworkInterfaceId != nil {
			props["network_interface_id"] = aws.ToString(eip.NetworkInterfaceId)
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources
}

// TransformInstances converts AWS EC2 Instances into OSIRIS JSON resources.
func TransformInstances(instances []ec2types.Instance, region, accountID string) []sdk.Resource {
	var resources []sdk.Resource
	for _, inst := range instances {
		instID := aws.ToString(inst.InstanceId)
		id := resourceID(accountID, region, "ec2", "instance/"+instID)
		az := ""
		if inst.Placement != nil {
			az = aws.ToString(inst.Placement.AvailabilityZone)
		}
		prov := awsProviderWithZone(instID, "ec2:instance", region, accountID, az)

		r, err := sdk.NewResource(id, "compute.vm", prov)
		if err != nil {
			continue
		}
		name := tagName(inst.Tags)
		if name == "" {
			name = instID
		}
		r.Name = name
		r.Status = mapInstanceState(inst.State)
		r.Tags = tagMap(inst.Tags)

		props := map[string]any{
			"instance_type": string(inst.InstanceType),
			"vpc_id":        aws.ToString(inst.VpcId),
			"subnet_id":     aws.ToString(inst.SubnetId),
		}
		if inst.PrivateIpAddress != nil {
			props["private_ip"] = aws.ToString(inst.PrivateIpAddress)
		}
		if inst.PublicIpAddress != nil {
			props["public_ip"] = aws.ToString(inst.PublicIpAddress)
		}
		if inst.Architecture != "" {
			props["architecture"] = string(inst.Architecture)
		}
		if inst.PlatformDetails != nil {
			props["platform"] = aws.ToString(inst.PlatformDetails)
		}
		if inst.ImageId != nil {
			props["image_id"] = aws.ToString(inst.ImageId)
		}
		if inst.KeyName != nil {
			props["key_name"] = aws.ToString(inst.KeyName)
		}
		if inst.IamInstanceProfile != nil {
			props["iam_instance_profile_arn"] = aws.ToString(inst.IamInstanceProfile.Arn)
		}
		if inst.MetadataOptions != nil {
			props["imds_v2_enforcement"] = string(inst.MetadataOptions.HttpTokens)
			props["metadata_http_endpoint"] = string(inst.MetadataOptions.HttpEndpoint)
		}
		if len(inst.BlockDeviceMappings) > 0 {
			bdms := make([]map[string]any, 0, len(inst.BlockDeviceMappings))
			for _, bdm := range inst.BlockDeviceMappings {
				entry := map[string]any{
					"device_name": aws.ToString(bdm.DeviceName),
				}
				if bdm.Ebs != nil {
					entry["volume_id"] = aws.ToString(bdm.Ebs.VolumeId)
					entry["delete_on_termination"] = aws.ToBool(bdm.Ebs.DeleteOnTermination)
				}
				bdms = append(bdms, entry)
			}
			props["block_device_mappings"] = bdms
		}
		r.Properties = props
		attachRawBody(&r, &inst)
		resources = append(resources, r)
	}
	return resources
}

// TransformNetworkACLs converts AWS Network ACLs into OSIRIS JSON resources.
func TransformNetworkACLs(nacls []ec2types.NetworkAcl, region, accountID string) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(nacls))

	for _, nacl := range nacls {
		naclID := aws.ToString(nacl.NetworkAclId)
		id := resourceID(accountID, region, "ec2", "network-acl/"+naclID)
		idMap[naclID] = id
		prov := awsProvider(naclID, "ec2:network-acl", region, accountID)

		r, err := sdk.NewResource(id, "osiris.aws.nacl", prov)
		if err != nil {
			continue
		}
		name := tagName(nacl.Tags)
		if name == "" {
			name = naclID
		}
		r.Name = name
		r.Status = "active"
		r.Tags = tagMap(nacl.Tags)

		props := map[string]any{
			"vpc_id":      aws.ToString(nacl.VpcId),
			"is_default":  aws.ToBool(nacl.IsDefault),
			"entry_count": len(nacl.Entries),
		}

		// Track subnet associations.
		var assocSubnets []string
		for _, assoc := range nacl.Associations {
			assocSubnets = append(assocSubnets, aws.ToString(assoc.SubnetId))
		}
		if len(assocSubnets) > 0 {
			props["associated_subnets"] = assocSubnets
		}
		r.Properties = props

		// Extensions: NACL entries.
		if len(nacl.Entries) > 0 {
			entries := make([]map[string]any, 0, len(nacl.Entries))
			for _, e := range nacl.Entries {
				entry := map[string]any{
					"rule_number": aws.ToInt32(e.RuleNumber),
					"protocol":    aws.ToString(e.Protocol),
					"egress":      aws.ToBool(e.Egress),
				}
				if e.RuleAction != "" {
					entry["action"] = string(e.RuleAction)
				}
				if e.CidrBlock != nil {
					entry["cidr_block"] = aws.ToString(e.CidrBlock)
				}
				if e.PortRange != nil {
					entry["from_port"] = aws.ToInt32(e.PortRange.From)
					entry["to_port"] = aws.ToInt32(e.PortRange.To)
				}
				entries = append(entries, entry)
			}
			r.Extensions = map[string]any{"osiris.aws": map[string]any{"entries": entries}}
		}

		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformVPCEndpoints converts AWS VPC Endpoints into OSIRIS JSON resources.
func TransformVPCEndpoints(endpoints []ec2types.VpcEndpoint, region, accountID string) []sdk.Resource {
	var resources []sdk.Resource
	for _, ep := range endpoints {
		epID := aws.ToString(ep.VpcEndpointId)
		id := resourceID(accountID, region, "ec2", "vpc-endpoint/"+epID)
		prov := awsProvider(epID, "ec2:vpc-endpoint", region, accountID)

		r, err := sdk.NewResource(id, "osiris.aws.vpc.endpoint", prov)
		if err != nil {
			continue
		}
		name := tagName(ep.Tags)
		if name == "" {
			name = epID
		}
		r.Name = name
		r.Status = mapVPCEndpointState(ep.State)
		r.Tags = tagMap(ep.Tags)

		props := map[string]any{
			"vpc_id":        aws.ToString(ep.VpcId),
			"service_name":  aws.ToString(ep.ServiceName),
			"endpoint_type": string(ep.VpcEndpointType),
		}
		if len(ep.RouteTableIds) > 0 {
			props["route_table_ids"] = ep.RouteTableIds
		}
		if len(ep.SubnetIds) > 0 {
			props["subnet_ids"] = ep.SubnetIds
		}
		if len(ep.NetworkInterfaceIds) > 0 {
			props["network_interface_ids"] = ep.NetworkInterfaceIds
		}
		if len(ep.Groups) > 0 {
			sgIDs := make([]string, 0, len(ep.Groups))
			for _, g := range ep.Groups {
				sgIDs = append(sgIDs, aws.ToString(g.GroupId))
			}
			props["security_group_ids"] = sgIDs
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources
}

// TransformVPCPeeringConnections converts AWS VPC Peering Connections into OSIRIS JSON resources.
func TransformVPCPeeringConnections(peerings []ec2types.VpcPeeringConnection, region, accountID string) []sdk.Resource {
	var resources []sdk.Resource
	for _, p := range peerings {
		pID := aws.ToString(p.VpcPeeringConnectionId)
		id := resourceID(accountID, region, "ec2", "vpc-peering-connection/"+pID)
		prov := awsProvider(pID, "ec2:vpc-peering-connection", region, accountID)

		r, err := sdk.NewResource(id, "osiris.aws.vpc.peering", prov)
		if err != nil {
			continue
		}
		name := tagName(p.Tags)
		if name == "" {
			name = pID
		}
		r.Name = name
		r.Tags = tagMap(p.Tags)

		props := map[string]any{}
		if p.Status != nil {
			r.Status = mapPeeringStatus(p.Status.Code)
			props["status_message"] = aws.ToString(p.Status.Message)
		}
		if p.RequesterVpcInfo != nil {
			props["requester_vpc_id"] = aws.ToString(p.RequesterVpcInfo.VpcId)
			props["requester_account"] = aws.ToString(p.RequesterVpcInfo.OwnerId)
			props["requester_region"] = aws.ToString(p.RequesterVpcInfo.Region)
		}
		if p.AccepterVpcInfo != nil {
			props["accepter_vpc_id"] = aws.ToString(p.AccepterVpcInfo.VpcId)
			props["accepter_account"] = aws.ToString(p.AccepterVpcInfo.OwnerId)
			props["accepter_region"] = aws.ToString(p.AccepterVpcInfo.Region)
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources
}

// TransformTransitGateways converts AWS Transit Gateways into OSIRIS JSON resources.
func TransformTransitGateways(tgws []ec2types.TransitGateway, region, accountID string) []sdk.Resource {
	var resources []sdk.Resource
	for _, tgw := range tgws {
		tgwID := aws.ToString(tgw.TransitGatewayId)
		id := resourceID(accountID, region, "ec2", "transit-gateway/"+tgwID)
		prov := awsProvider(tgwID, "ec2:transit-gateway", region, accountID)
		if tgw.TransitGatewayArn != nil {
			prov.NativeID = aws.ToString(tgw.TransitGatewayArn)
		}

		r, err := sdk.NewResource(id, "osiris.aws.transitgateway", prov)
		if err != nil {
			continue
		}
		name := tagName(tgw.Tags)
		if name == "" {
			name = aws.ToString(tgw.Description)
		}
		if name == "" {
			name = tgwID
		}
		r.Name = name
		r.Status = mapTGWState(tgw.State)
		r.Tags = tagMap(tgw.Tags)

		props := map[string]any{
			"owner_id": aws.ToString(tgw.OwnerId),
		}
		if tgw.Options != nil {
			props["amazon_side_asn"] = tgw.Options.AmazonSideAsn
			props["dns_support"] = string(tgw.Options.DnsSupport)
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources
}

// TransformTransitGatewayAttachments converts AWS TGW Attachments into OSIRIS JSON resources.
func TransformTransitGatewayAttachments(attachments []ec2types.TransitGatewayAttachment, region, accountID string) []sdk.Resource {
	var resources []sdk.Resource
	for _, att := range attachments {
		attID := aws.ToString(att.TransitGatewayAttachmentId)
		id := resourceID(accountID, region, "ec2", "transit-gateway-attachment/"+attID)
		prov := awsProvider(attID, "ec2:transit-gateway-attachment", region, accountID)

		r, err := sdk.NewResource(id, "osiris.aws.transitgateway.attachment", prov)
		if err != nil {
			continue
		}
		name := tagName(att.Tags)
		if name == "" {
			name = attID
		}
		r.Name = name
		r.Status = mapTGWAttachmentState(att.State)
		r.Tags = tagMap(att.Tags)

		props := map[string]any{
			"transit_gateway_id": aws.ToString(att.TransitGatewayId),
			"resource_type":      string(att.ResourceType),
			"resource_id":        aws.ToString(att.ResourceId),
			"resource_owner_id":  aws.ToString(att.ResourceOwnerId),
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources
}

// TransformTransitGatewayRouteTables converts AWS TGW Route Tables into OSIRIS JSON resources.
func TransformTransitGatewayRouteTables(rts []ec2types.TransitGatewayRouteTable, region, accountID string) []sdk.Resource {
	var resources []sdk.Resource
	for _, rt := range rts {
		rtID := aws.ToString(rt.TransitGatewayRouteTableId)
		id := resourceID(accountID, region, "ec2", "transit-gateway-route-table/"+rtID)
		prov := awsProvider(rtID, "ec2:transit-gateway-route-table", region, accountID)

		r, err := sdk.NewResource(id, "osiris.aws.transitgateway.routetable", prov)
		if err != nil {
			continue
		}
		name := tagName(rt.Tags)
		if name == "" {
			name = rtID
		}
		r.Name = name
		r.Status = mapTGWRTState(rt.State)
		r.Tags = tagMap(rt.Tags)
		r.Properties = map[string]any{
			"transit_gateway_id": aws.ToString(rt.TransitGatewayId),
			"is_default":         aws.ToBool(rt.DefaultAssociationRouteTable),
		}
		resources = append(resources, r)
	}
	return resources
}

// TransformVPNGateways converts AWS VPN Gateways into OSIRIS JSON resources.
func TransformVPNGateways(vgws []ec2types.VpnGateway, region, accountID string) []sdk.Resource {
	var resources []sdk.Resource
	for _, vgw := range vgws {
		vgwID := aws.ToString(vgw.VpnGatewayId)
		id := resourceID(accountID, region, "ec2", "vpn-gateway/"+vgwID)
		prov := awsProvider(vgwID, "ec2:vpn-gateway", region, accountID)

		r, err := sdk.NewResource(id, "osiris.aws.gateway.vpn", prov)
		if err != nil {
			continue
		}
		name := tagName(vgw.Tags)
		if name == "" {
			name = vgwID
		}
		r.Name = name
		r.Status = mapVGWState(vgw.State)
		r.Tags = tagMap(vgw.Tags)

		props := map[string]any{
			"type": string(vgw.Type),
		}
		if vgw.AmazonSideAsn != nil {
			props["amazon_side_asn"] = aws.ToInt64(vgw.AmazonSideAsn)
		}
		var attachedVPCs []string
		for _, att := range vgw.VpcAttachments {
			attachedVPCs = append(attachedVPCs, aws.ToString(att.VpcId))
		}
		if len(attachedVPCs) > 0 {
			props["attached_vpcs"] = attachedVPCs
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources
}

// TransformVPNConnections converts AWS VPN Connections into OSIRIS JSON resources.
func TransformVPNConnections(conns []ec2types.VpnConnection, region, accountID string) []sdk.Resource {
	var resources []sdk.Resource
	for _, conn := range conns {
		connID := aws.ToString(conn.VpnConnectionId)
		id := resourceID(accountID, region, "ec2", "vpn-connection/"+connID)
		prov := awsProvider(connID, "ec2:vpn-connection", region, accountID)

		r, err := sdk.NewResource(id, "osiris.aws.vpn.connection", prov)
		if err != nil {
			continue
		}
		name := tagName(conn.Tags)
		if name == "" {
			name = connID
		}
		r.Name = name
		r.Status = mapVPNState(conn.State)
		r.Tags = tagMap(conn.Tags)

		props := map[string]any{
			"type": string(conn.Type),
		}
		if conn.VpnGatewayId != nil {
			props["vpn_gateway_id"] = aws.ToString(conn.VpnGatewayId)
		}
		if conn.CustomerGatewayId != nil {
			props["customer_gateway_id"] = aws.ToString(conn.CustomerGatewayId)
		}
		if conn.TransitGatewayId != nil {
			props["transit_gateway_id"] = aws.ToString(conn.TransitGatewayId)
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources
}

// TransformCustomerGateways converts AWS Customer Gateways into OSIRIS JSON resources.
func TransformCustomerGateways(cgws []ec2types.CustomerGateway, region, accountID string) []sdk.Resource {
	var resources []sdk.Resource
	for _, cgw := range cgws {
		cgwID := aws.ToString(cgw.CustomerGatewayId)
		id := resourceID(accountID, region, "ec2", "customer-gateway/"+cgwID)
		prov := awsProvider(cgwID, "ec2:customer-gateway", region, accountID)

		r, err := sdk.NewResource(id, "osiris.aws.gateway.customer", prov)
		if err != nil {
			continue
		}
		name := tagName(cgw.Tags)
		if name == "" {
			name = cgwID
		}
		r.Name = name
		r.Status = "active"
		r.Tags = tagMap(cgw.Tags)

		props := map[string]any{
			"type": aws.ToString(cgw.Type),
		}
		if cgw.IpAddress != nil {
			props["ip_address"] = aws.ToString(cgw.IpAddress)
		}
		if cgw.BgpAsn != nil {
			props["bgp_asn"] = aws.ToString(cgw.BgpAsn)
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources
}

// TransformDHCPOptions converts AWS DHCP Options into OSIRIS JSON resources.
func TransformDHCPOptions(opts []ec2types.DhcpOptions, region, accountID string) []sdk.Resource {
	var resources []sdk.Resource
	for _, opt := range opts {
		optID := aws.ToString(opt.DhcpOptionsId)
		id := resourceID(accountID, region, "ec2", "dhcp-options/"+optID)
		prov := awsProvider(optID, "ec2:dhcp-options", region, accountID)

		r, err := sdk.NewResource(id, "osiris.aws.dhcpoptions", prov)
		if err != nil {
			continue
		}
		name := tagName(opt.Tags)
		if name == "" {
			name = optID
		}
		r.Name = name
		r.Status = "active"
		r.Tags = tagMap(opt.Tags)

		props := map[string]any{}
		for _, cfg := range opt.DhcpConfigurations {
			key := aws.ToString(cfg.Key)
			var vals []string
			for _, v := range cfg.Values {
				vals = append(vals, aws.ToString(v.Value))
			}
			props[key] = vals
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources
}

// TransformEgressOnlyIGWs converts AWS Egress-Only Internet Gateways into OSIRIS JSON resources.
func TransformEgressOnlyIGWs(igws []ec2types.EgressOnlyInternetGateway, region, accountID string) []sdk.Resource {
	var resources []sdk.Resource
	for _, igw := range igws {
		igwID := aws.ToString(igw.EgressOnlyInternetGatewayId)
		id := resourceID(accountID, region, "ec2", "egress-only-internet-gateway/"+igwID)
		prov := awsProvider(igwID, "ec2:egress-only-internet-gateway", region, accountID)

		r, err := sdk.NewResource(id, "osiris.aws.gateway.egressonly", prov)
		if err != nil {
			continue
		}
		r.Name = igwID
		r.Status = "active"
		r.Tags = tagMap(igw.Tags)

		if len(igw.Attachments) > 0 {
			var vpcIDs []string
			for _, att := range igw.Attachments {
				vpcIDs = append(vpcIDs, aws.ToString(att.VpcId))
			}
			r.Properties = map[string]any{"attached_vpcs": vpcIDs}
		}
		resources = append(resources, r)
	}
	return resources
}

// TransformManagedPrefixLists converts AWS Managed Prefix Lists into OSIRIS JSON resources.
func TransformManagedPrefixLists(pls []ec2types.ManagedPrefixList, region, accountID string) []sdk.Resource {
	var resources []sdk.Resource
	for _, pl := range pls {
		plID := aws.ToString(pl.PrefixListId)
		id := resourceID(accountID, region, "ec2", "prefix-list/"+plID)
		prov := awsProvider(plID, "ec2:managed-prefix-list", region, accountID)

		r, err := sdk.NewResource(id, "osiris.aws.prefixlist", prov)
		if err != nil {
			continue
		}
		name := aws.ToString(pl.PrefixListName)
		if name == "" {
			name = plID
		}
		r.Name = name
		r.Status = "active"
		r.Tags = tagMap(pl.Tags)

		props := map[string]any{
			"address_family": aws.ToString(pl.AddressFamily),
			"owner_id":       aws.ToString(pl.OwnerId),
		}
		if pl.MaxEntries != nil {
			props["max_entries"] = aws.ToInt32(pl.MaxEntries)
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources
}

// TransformFlowLogs converts AWS Flow Logs into OSIRIS JSON resources.
func TransformFlowLogs(fls []ec2types.FlowLog, region, accountID string) []sdk.Resource {
	var resources []sdk.Resource
	for _, fl := range fls {
		flID := aws.ToString(fl.FlowLogId)
		id := resourceID(accountID, region, "ec2", "flow-log/"+flID)
		prov := awsProvider(flID, "ec2:flow-log", region, accountID)

		r, err := sdk.NewResource(id, "osiris.aws.flowlog", prov)
		if err != nil {
			continue
		}
		r.Name = flID
		r.Status = mapFlowLogStatus(fl.FlowLogStatus)
		r.Tags = tagMap(fl.Tags)

		props := map[string]any{
			"resource_id":          aws.ToString(fl.ResourceId),
			"traffic_type":         string(fl.TrafficType),
			"log_destination_type": string(fl.LogDestinationType),
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources
}

// TransformAvailabilityZones converts AWS Availability Zones into OSIRIS JSON resources.
func TransformAvailabilityZones(azs []ec2types.AvailabilityZone, region, accountID string) []sdk.Resource {
	var resources []sdk.Resource
	for _, az := range azs {
		azName := aws.ToString(az.ZoneName)
		azID := aws.ToString(az.ZoneId)
		id := resourceID(accountID, region, "ec2", "availability-zone/"+azID)
		prov := awsProvider(azID, "ec2:availability-zone", region, accountID)

		r, err := sdk.NewResource(id, "osiris.aws.availabilityzone", prov)
		if err != nil {
			continue
		}
		r.Name = azName
		r.Status = mapAZState(az.State)

		props := map[string]any{
			"zone_id":   azID,
			"zone_type": aws.ToString(az.ZoneType),
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources
}

// TransformLoadBalancersV2 converts AWS ELBv2 Load Balancers into OSIRIS JSON resources.
func TransformLoadBalancersV2(lbs []elbv2types.LoadBalancer, region, accountID string) []sdk.Resource {
	var resources []sdk.Resource
	for _, lb := range lbs {
		lbARN := aws.ToString(lb.LoadBalancerArn)
		id := resourceIDFromARN(lbARN)
		prov := awsProvider(lbARN, "elbv2:loadbalancer", region, accountID)

		r, err := sdk.NewResource(id, "network.loadbalancer", prov)
		if err != nil {
			continue
		}
		r.Name = aws.ToString(lb.LoadBalancerName)
		r.Status = mapLBState(lb.State)

		props := map[string]any{
			"vpc_id": aws.ToString(lb.VpcId),
			"type":   string(lb.Type),
			"scheme": string(lb.Scheme),
		}
		if len(lb.AvailabilityZones) > 0 {
			var azNames []string
			var subnetIDs []string
			for _, az := range lb.AvailabilityZones {
				azNames = append(azNames, aws.ToString(az.ZoneName))
				if az.SubnetId != nil {
					subnetIDs = append(subnetIDs, aws.ToString(az.SubnetId))
				}
			}
			props["availability_zones"] = azNames
			if len(subnetIDs) > 0 {
				props["subnet_ids"] = subnetIDs
			}
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources
}

// TransformTargetGroups converts AWS ELBv2 Target Groups into OSIRIS JSON resources.
func TransformTargetGroups(tgs []elbv2types.TargetGroup, region, accountID string) []sdk.Resource {
	var resources []sdk.Resource
	for _, tg := range tgs {
		tgARN := aws.ToString(tg.TargetGroupArn)
		id := resourceIDFromARN(tgARN)
		prov := awsProvider(tgARN, "elbv2:target-group", region, accountID)

		r, err := sdk.NewResource(id, "osiris.aws.targetgroup", prov)
		if err != nil {
			continue
		}
		r.Name = aws.ToString(tg.TargetGroupName)
		r.Status = "active"

		props := map[string]any{
			"target_type": string(tg.TargetType),
			"protocol":    string(tg.Protocol),
			"port":        aws.ToInt32(tg.Port),
		}
		if tg.VpcId != nil {
			props["vpc_id"] = aws.ToString(tg.VpcId)
		}
		if len(tg.LoadBalancerArns) > 0 {
			props["load_balancer_arns"] = tg.LoadBalancerArns
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources
}

// TransformLoadBalancersClassic converts AWS Classic Load Balancers into OSIRIS JSON resources.
func TransformLoadBalancersClassic(lbs []elbtypes.LoadBalancerDescription, region, accountID string) []sdk.Resource {
	var resources []sdk.Resource
	for _, lb := range lbs {
		lbName := aws.ToString(lb.LoadBalancerName)
		// Classic ELBs don't have ARNs in the describe output, construct an ID.
		id := resourceID(accountID, region, "elasticloadbalancing", "loadbalancer/"+lbName)
		prov := awsProvider(lbName, "elb:loadbalancer", region, accountID)

		r, err := sdk.NewResource(id, "network.loadbalancer", prov)
		if err != nil {
			continue
		}
		r.Name = lbName
		r.Status = "active"

		props := map[string]any{
			"vpc_id": aws.ToString(lb.VPCId),
			"type":   "classic",
			"scheme": aws.ToString(lb.Scheme),
		}
		if len(lb.AvailabilityZones) > 0 {
			props["availability_zones"] = lb.AvailabilityZones
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources
}

// TransformDirectConnectConnections converts AWS Direct Connect connections into OSIRIS JSON resources.
func TransformDirectConnectConnections(conns []dctypes.Connection, region, accountID string) []sdk.Resource {
	var resources []sdk.Resource
	for _, conn := range conns {
		connID := aws.ToString(conn.ConnectionId)
		id := resourceID(accountID, region, "directconnect", "connection/"+connID)
		prov := awsProvider(connID, "directconnect:connection", region, accountID)

		r, err := sdk.NewResource(id, "osiris.aws.directconnect", prov)
		if err != nil {
			continue
		}
		name := aws.ToString(conn.ConnectionName)
		if name == "" {
			name = connID
		}
		r.Name = name
		r.Status = mapDXConnectionState(conn.ConnectionState)
		r.Tags = tagMapDX(conn.Tags)

		props := map[string]any{
			"location":  aws.ToString(conn.Location),
			"bandwidth": aws.ToString(conn.Bandwidth),
		}
		if conn.Vlan != 0 {
			props["vlan"] = conn.Vlan
		}
		if conn.PartnerName != nil {
			props["partner_name"] = aws.ToString(conn.PartnerName)
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources
}

// TransformDirectConnectGateways converts AWS Direct Connect gateways into OSIRIS JSON resources.
func TransformDirectConnectGateways(gws []dctypes.DirectConnectGateway, region, accountID string) []sdk.Resource {
	var resources []sdk.Resource
	for _, gw := range gws {
		gwID := aws.ToString(gw.DirectConnectGatewayId)
		id := resourceID(accountID, region, "directconnect", "gateway/"+gwID)
		prov := awsProvider(gwID, "directconnect:gateway", region, accountID)

		r, err := sdk.NewResource(id, "osiris.aws.directconnect.gateway", prov)
		if err != nil {
			continue
		}
		name := aws.ToString(gw.DirectConnectGatewayName)
		if name == "" {
			name = gwID
		}
		r.Name = name
		r.Status = mapDXGatewayState(gw.DirectConnectGatewayState)

		props := map[string]any{
			"owner_account": aws.ToString(gw.OwnerAccount),
		}
		if gw.AmazonSideAsn != nil {
			props["amazon_side_asn"] = aws.ToInt64(gw.AmazonSideAsn)
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources
}

// TransformDirectConnectVIFs converts AWS Direct Connect virtual interfaces into OSIRIS JSON resources.
func TransformDirectConnectVIFs(vifs []dctypes.VirtualInterface, region, accountID string) []sdk.Resource {
	var resources []sdk.Resource
	for _, vif := range vifs {
		vifID := aws.ToString(vif.VirtualInterfaceId)
		id := resourceID(accountID, region, "directconnect", "vif/"+vifID)
		prov := awsProvider(vifID, "directconnect:vif", region, accountID)

		r, err := sdk.NewResource(id, "osiris.aws.directconnect.vif", prov)
		if err != nil {
			continue
		}
		name := aws.ToString(vif.VirtualInterfaceName)
		if name == "" {
			name = vifID
		}
		r.Name = name
		r.Status = mapDXVIFState(vif.VirtualInterfaceState)
		r.Tags = tagMapDX(vif.Tags)

		props := map[string]any{
			"connection_id": aws.ToString(vif.ConnectionId),
			"vif_type":      aws.ToString(vif.VirtualInterfaceType),
			"vlan":          vif.Vlan,
		}
		if vif.DirectConnectGatewayId != nil {
			props["direct_connect_gateway_id"] = aws.ToString(vif.DirectConnectGatewayId)
		}
		if vif.Asn != 0 {
			props["customer_asn"] = vif.Asn
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources
}

// TransformNetworkFirewalls converts AWS Network Firewall metadata into OSIRIS JSON resources.
func TransformNetworkFirewalls(fws []nfwtypes.FirewallMetadata, region, accountID string) []sdk.Resource {
	var resources []sdk.Resource
	for _, fw := range fws {
		fwARN := aws.ToString(fw.FirewallArn)
		id := resourceIDFromARN(fwARN)
		prov := awsProvider(fwARN, "network-firewall:firewall", region, accountID)

		r, err := sdk.NewResource(id, "network.firewall", prov)
		if err != nil {
			continue
		}
		r.Name = aws.ToString(fw.FirewallName)
		r.Status = "active"
		resources = append(resources, r)
	}
	return resources
}

// TransformResolverRules converts AWS Route53 Resolver Rules into OSIRIS JSON resources.
func TransformResolverRules(rules []r53rtypes.ResolverRule, region, accountID string) []sdk.Resource {
	var resources []sdk.Resource
	for _, rule := range rules {
		ruleARN := aws.ToString(rule.Arn)
		id := resourceIDFromARN(ruleARN)
		prov := awsProvider(ruleARN, "route53resolver:rule", region, accountID)

		r, err := sdk.NewResource(id, "osiris.aws.resolver.rule", prov)
		if err != nil {
			continue
		}
		r.Name = aws.ToString(rule.Name)
		r.Status = mapResolverRuleStatus(rule.Status)

		props := map[string]any{
			"domain_name": aws.ToString(rule.DomainName),
			"rule_type":   string(rule.RuleType),
		}
		if rule.ResolverEndpointId != nil {
			props["resolver_endpoint_id"] = aws.ToString(rule.ResolverEndpointId)
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources
}

// TransformResolverEndpoints converts AWS Route53 Resolver Endpoints into OSIRIS JSON resources.
func TransformResolverEndpoints(eps []r53rtypes.ResolverEndpoint, region, accountID string) []sdk.Resource {
	var resources []sdk.Resource
	for _, ep := range eps {
		epARN := aws.ToString(ep.Arn)
		id := resourceIDFromARN(epARN)
		prov := awsProvider(epARN, "route53resolver:endpoint", region, accountID)

		r, err := sdk.NewResource(id, "osiris.aws.resolver.endpoint", prov)
		if err != nil {
			continue
		}
		r.Name = aws.ToString(ep.Name)
		r.Status = mapResolverEndpointStatus(ep.Status)

		props := map[string]any{
			"direction":        string(ep.Direction),
			"ip_address_count": aws.ToInt32(ep.IpAddressCount),
		}
		if ep.HostVPCId != nil {
			props["host_vpc_id"] = aws.ToString(ep.HostVPCId)
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources
}

// TransformRoute53HostedZones converts AWS Route53 Hosted Zones into OSIRIS JSON resources.
func TransformRoute53HostedZones(zones []r53types.HostedZone, accountID string) []sdk.Resource {
	var resources []sdk.Resource
	for _, z := range zones {
		zoneID := aws.ToString(z.Id)
		// Route53 zone IDs have a leading slash, strip it.
		zoneID = strings.TrimPrefix(zoneID, "/hostedzone/")
		id := resourceID(accountID, "global", "route53", "hostedzone/"+zoneID)
		prov := awsProvider(zoneID, "route53:hosted-zone", "global", accountID)

		r, err := sdk.NewResource(id, "osiris.aws.route53.zone", prov)
		if err != nil {
			continue
		}
		r.Name = aws.ToString(z.Name)
		r.Status = "active"

		props := map[string]any{
			"record_count": aws.ToInt64(z.ResourceRecordSetCount),
		}
		if z.Config != nil {
			props["private_zone"] = z.Config.PrivateZone
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources
}

// TransformGlobalAccelerators converts AWS Global Accelerators into OSIRIS JSON resources.
func TransformGlobalAccelerators(accs []gatypes.Accelerator, accountID string) []sdk.Resource {
	var resources []sdk.Resource
	for _, a := range accs {
		accARN := aws.ToString(a.AcceleratorArn)
		id := resourceIDFromARN(accARN)
		prov := awsProvider(accARN, "globalaccelerator:accelerator", "global", accountID)

		r, err := sdk.NewResource(id, "osiris.aws.globalaccelerator", prov)
		if err != nil {
			continue
		}
		r.Name = aws.ToString(a.Name)
		r.Status = mapGAStatus(a.Status)

		props := map[string]any{
			"enabled": aws.ToBool(a.Enabled),
		}
		if len(a.IpSets) > 0 {
			var ips []string
			for _, ipSet := range a.IpSets {
				ips = append(ips, ipSet.IpAddresses...)
			}
			if len(ips) > 0 {
				props["ip_addresses"] = ips
			}
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources
}

// TransformSubnetToVPCConnections creates containment connections between subnets and their parent VPC.
func TransformSubnetToVPCConnections(subnets []ec2types.Subnet, subnetIDMap map[string]string, region, accountID string) []sdk.Connection {
	var connections []sdk.Connection
	for _, s := range subnets {
		subnetNativeID := aws.ToString(s.SubnetId)
		sourceID, ok := subnetIDMap[subnetNativeID]
		if !ok {
			continue
		}
		vpcNativeID := aws.ToString(s.VpcId)
		targetID := resourceID(accountID, region, "ec2", "vpc/"+vpcNativeID)

		canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
			Type:      "contains",
			Direction: "forward",
			Source:    targetID,
			Target:    sourceID,
		})
		connID := sdk.BuildConnectionID(canonicalKey, 16)

		conn, err := sdk.NewConnection(connID, "contains", targetID, sourceID)
		if err != nil {
			continue
		}
		conn.Name = fmt.Sprintf("%s contains %s", vpcNativeID, subnetNativeID)
		_ = conn.SetDirection("forward")
		connections = append(connections, conn)
	}
	return connections
}

// TransformENIToSubnetConnections creates connections between ENIs and their subnets.
func TransformENIToSubnetConnections(enis []ec2types.NetworkInterface, eniIDMap, subnetIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, eni := range enis {
		eniNativeID := aws.ToString(eni.NetworkInterfaceId)
		sourceID, ok := eniIDMap[eniNativeID]
		if !ok {
			continue
		}
		subnetNativeID := aws.ToString(eni.SubnetId)
		targetID, ok := subnetIDMap[subnetNativeID]
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
		conn.Name = fmt.Sprintf("%s -> %s", eniNativeID, subnetNativeID)
		_ = conn.SetDirection("forward")
		if eni.PrivateIpAddress != nil {
			conn.Properties = map[string]any{
				"private_ip": aws.ToString(eni.PrivateIpAddress),
			}
		}
		connections = append(connections, conn)
	}
	return connections
}

// TransformSGToENIConnections creates connections between security groups and ENIs.
func TransformSGToENIConnections(enis []ec2types.NetworkInterface, eniIDMap, sgIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	seen := map[string]bool{}
	for _, eni := range enis {
		eniNativeID := aws.ToString(eni.NetworkInterfaceId)
		targetID, ok := eniIDMap[eniNativeID]
		if !ok {
			continue
		}
		for _, g := range eni.Groups {
			sgNativeID := aws.ToString(g.GroupId)
			sourceID, ok := sgIDMap[sgNativeID]
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
			conn.Name = fmt.Sprintf("%s -> %s", sgNativeID, eniNativeID)
			_ = conn.SetDirection("forward")
			connections = append(connections, conn)
		}
	}
	return connections
}

// TransformNACLToSubnetConnections creates connections between NACLs and their associated subnets.
func TransformNACLToSubnetConnections(nacls []ec2types.NetworkAcl, naclIDMap, subnetIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, nacl := range nacls {
		naclNativeID := aws.ToString(nacl.NetworkAclId)
		sourceID, ok := naclIDMap[naclNativeID]
		if !ok {
			continue
		}
		for _, assoc := range nacl.Associations {
			subnetNativeID := aws.ToString(assoc.SubnetId)
			targetID, ok := subnetIDMap[subnetNativeID]
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
			conn.Name = fmt.Sprintf("%s -> %s", naclNativeID, subnetNativeID)
			_ = conn.SetDirection("forward")
			connections = append(connections, conn)
		}
	}
	return connections
}

// TransformRouteTableToSubnetConnections creates connections between route tables and their subnets.
func TransformRouteTableToSubnetConnections(rts []ec2types.RouteTable, rtIDMap, subnetIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, rt := range rts {
		rtNativeID := aws.ToString(rt.RouteTableId)
		sourceID, ok := rtIDMap[rtNativeID]
		if !ok {
			continue
		}
		for _, assoc := range rt.Associations {
			if assoc.SubnetId == nil {
				continue
			}
			subnetNativeID := aws.ToString(assoc.SubnetId)
			targetID, ok := subnetIDMap[subnetNativeID]
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
			conn.Name = fmt.Sprintf("%s -> %s", rtNativeID, subnetNativeID)
			_ = conn.SetDirection("forward")
			connections = append(connections, conn)
		}
	}
	return connections
}

// TransformNATGatewayToSubnetConnections creates connections between NAT gateways and subnets.
func TransformNATGatewayToSubnetConnections(ngws []ec2types.NatGateway, subnetIDMap map[string]string, region, accountID string) []sdk.Connection {
	var connections []sdk.Connection
	for _, ngw := range ngws {
		ngwID := aws.ToString(ngw.NatGatewayId)
		sourceID := resourceID(accountID, region, "ec2", "nat-gateway/"+ngwID)
		subnetNativeID := aws.ToString(ngw.SubnetId)
		targetID, ok := subnetIDMap[subnetNativeID]
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
		conn.Name = fmt.Sprintf("%s -> %s", ngwID, subnetNativeID)
		_ = conn.SetDirection("forward")
		connections = append(connections, conn)
	}
	return connections
}

// TransformNATGatewayToEIPConnections creates connections between NAT gateways and elastic IPs.
func TransformNATGatewayToEIPConnections(ngws []ec2types.NatGateway, region, accountID string) []sdk.Connection {
	var connections []sdk.Connection
	for _, ngw := range ngws {
		ngwID := aws.ToString(ngw.NatGatewayId)
		sourceID := resourceID(accountID, region, "ec2", "nat-gateway/"+ngwID)
		for _, addr := range ngw.NatGatewayAddresses {
			if addr.AllocationId == nil {
				continue
			}
			allocID := aws.ToString(addr.AllocationId)
			targetID := resourceID(accountID, region, "ec2", "elastic-ip/"+allocID)

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
			conn.Name = fmt.Sprintf("%s -> %s", ngwID, allocID)
			_ = conn.SetDirection("forward")
			connections = append(connections, conn)
		}
	}
	return connections
}

// TransformIGWToVPCConnections creates connections between internet gateways and VPCs.
func TransformIGWToVPCConnections(igws []ec2types.InternetGateway, region, accountID string) []sdk.Connection {
	var connections []sdk.Connection
	for _, igw := range igws {
		igwID := aws.ToString(igw.InternetGatewayId)
		sourceID := resourceID(accountID, region, "ec2", "internet-gateway/"+igwID)
		for _, att := range igw.Attachments {
			vpcNativeID := aws.ToString(att.VpcId)
			targetID := resourceID(accountID, region, "ec2", "vpc/"+vpcNativeID)

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
			conn.Name = fmt.Sprintf("%s -> %s", igwID, vpcNativeID)
			_ = conn.SetDirection("forward")
			connections = append(connections, conn)
		}
	}
	return connections
}

// TransformVPCPeeringConnectionConns creates bidirectional connections for VPC peerings.
func TransformVPCPeeringConnectionConns(peerings []ec2types.VpcPeeringConnection, region, accountID string) []sdk.Connection {
	var connections []sdk.Connection
	seen := map[string]bool{}
	for _, p := range peerings {
		if p.RequesterVpcInfo == nil || p.AccepterVpcInfo == nil {
			continue
		}
		reqVPCID := aws.ToString(p.RequesterVpcInfo.VpcId)
		reqAccount := aws.ToString(p.RequesterVpcInfo.OwnerId)
		reqRegion := aws.ToString(p.RequesterVpcInfo.Region)
		accVPCID := aws.ToString(p.AccepterVpcInfo.VpcId)
		accAccount := aws.ToString(p.AccepterVpcInfo.OwnerId)
		accRegion := aws.ToString(p.AccepterVpcInfo.Region)

		sourceID := resourceID(reqAccount, reqRegion, "ec2", "vpc/"+reqVPCID)
		targetID := resourceID(accAccount, accRegion, "ec2", "vpc/"+accVPCID)

		canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
			Type:      "network",
			Direction: "bidirectional",
			Source:    sourceID,
			Target:    targetID,
		})
		connID := sdk.BuildConnectionID(canonicalKey, 16)

		if seen[connID] {
			continue
		}
		seen[connID] = true

		conn, err := sdk.NewConnection(connID, "network", sourceID, targetID)
		if err != nil {
			continue
		}
		peeringID := aws.ToString(p.VpcPeeringConnectionId)
		conn.Name = fmt.Sprintf("VPC peering %s", peeringID)
		if p.Status != nil {
			conn.Status = mapPeeringStatus(p.Status.Code)
		}
		connections = append(connections, conn)
	}
	return connections
}

// TransformVPCEndpointToVPCConnections creates connections between VPC endpoints and their VPCs.
func TransformVPCEndpointToVPCConnections(endpoints []ec2types.VpcEndpoint, region, accountID string) []sdk.Connection {
	var connections []sdk.Connection
	for _, ep := range endpoints {
		epID := aws.ToString(ep.VpcEndpointId)
		sourceID := resourceID(accountID, region, "ec2", "vpc-endpoint/"+epID)
		vpcNativeID := aws.ToString(ep.VpcId)
		targetID := resourceID(accountID, region, "ec2", "vpc/"+vpcNativeID)

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
		conn.Name = fmt.Sprintf("%s -> %s", epID, vpcNativeID)
		_ = conn.SetDirection("forward")
		connections = append(connections, conn)
	}
	return connections
}

// TransformVPCEndpointToSubnetConnections creates connections between interface VPC endpoints and their subnets.
func TransformVPCEndpointToSubnetConnections(endpoints []ec2types.VpcEndpoint, subnetIDMap map[string]string, region, accountID string) []sdk.Connection {
	var connections []sdk.Connection
	for _, ep := range endpoints {
		if ep.VpcEndpointType != ec2types.VpcEndpointTypeInterface && ep.VpcEndpointType != ec2types.VpcEndpointTypeGatewayLoadBalancer {
			continue
		}
		epID := aws.ToString(ep.VpcEndpointId)
		sourceID := resourceID(accountID, region, "ec2", "vpc-endpoint/"+epID)
		for _, subnetNativeID := range ep.SubnetIds {
			targetID, ok := subnetIDMap[subnetNativeID]
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
			conn.Name = fmt.Sprintf("%s -> %s", epID, subnetNativeID)
			_ = conn.SetDirection("forward")
			connections = append(connections, conn)
		}
	}
	return connections
}

// TransformVPCEndpointToRouteTableConnections creates connections from gateway VPC endpoints to
// their associated route tables.
func TransformVPCEndpointToRouteTableConnections(endpoints []ec2types.VpcEndpoint, rtIDMap map[string]string, region, accountID string) []sdk.Connection {
	var connections []sdk.Connection
	for _, ep := range endpoints {
		if ep.VpcEndpointType != ec2types.VpcEndpointTypeGateway {
			continue
		}
		epID := aws.ToString(ep.VpcEndpointId)
		sourceID := resourceID(accountID, region, "ec2", "vpc-endpoint/"+epID)
		for _, rtNativeID := range ep.RouteTableIds {
			targetID, ok := rtIDMap[rtNativeID]
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
			conn.Name = fmt.Sprintf("%s -> %s", epID, rtNativeID)
			_ = conn.SetDirection("forward")
			connections = append(connections, conn)
		}
	}
	return connections
}

// TransformVPCEndpointToSGConnections creates connections from interface VPC endpoints to their
// associated security groups.
func TransformVPCEndpointToSGConnections(endpoints []ec2types.VpcEndpoint, sgIDMap map[string]string, region, accountID string) []sdk.Connection {
	var connections []sdk.Connection
	for _, ep := range endpoints {
		epID := aws.ToString(ep.VpcEndpointId)
		sourceID := resourceID(accountID, region, "ec2", "vpc-endpoint/"+epID)
		for _, g := range ep.Groups {
			sgNativeID := aws.ToString(g.GroupId)
			targetID, ok := sgIDMap[sgNativeID]
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
			conn.Name = fmt.Sprintf("%s -> %s", epID, sgNativeID)
			_ = conn.SetDirection("forward")
			connections = append(connections, conn)
		}
	}
	return connections
}

// TransformSGToSGConnections creates connections between security groups based on
// UserIdGroupPairs references in ingress/egress rules.
func TransformSGToSGConnections(sgs []ec2types.SecurityGroup, sgIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, sg := range sgs {
		sgNativeID := aws.ToString(sg.GroupId)
		sourceID, ok := sgIDMap[sgNativeID]
		if !ok {
			continue
		}
		seen := make(map[string]bool)
		addConn := func(targetNativeID string) {
			if seen[targetNativeID] || targetNativeID == sgNativeID {
				return
			}
			seen[targetNativeID] = true
			targetID, ok := sgIDMap[targetNativeID]
			if !ok {
				return
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
				return
			}
			conn.Name = fmt.Sprintf("%s -> %s", sgNativeID, targetNativeID)
			_ = conn.SetDirection("forward")
			connections = append(connections, conn)
		}
		for _, p := range sg.IpPermissions {
			for _, g := range p.UserIdGroupPairs {
				addConn(aws.ToString(g.GroupId))
			}
		}
		for _, p := range sg.IpPermissionsEgress {
			for _, g := range p.UserIdGroupPairs {
				addConn(aws.ToString(g.GroupId))
			}
		}
	}
	return connections
}

// TransformTGWAttachmentToVPCConnections creates connections between TGW attachments and VPCs.
func TransformTGWAttachmentToVPCConnections(attachments []ec2types.TransitGatewayAttachment, region, accountID string) []sdk.Connection {
	var connections []sdk.Connection
	for _, att := range attachments {
		if att.ResourceType != ec2types.TransitGatewayAttachmentResourceTypeVpc {
			continue
		}
		attID := aws.ToString(att.TransitGatewayAttachmentId)
		sourceID := resourceID(accountID, region, "ec2", "transit-gateway-attachment/"+attID)
		vpcNativeID := aws.ToString(att.ResourceId)
		ownerID := aws.ToString(att.ResourceOwnerId)
		targetID := resourceID(ownerID, region, "ec2", "vpc/"+vpcNativeID)

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
		conn.Name = fmt.Sprintf("%s -> %s", attID, vpcNativeID)
		_ = conn.SetDirection("forward")
		connections = append(connections, conn)
	}
	return connections
}

// TransformTGWAttachmentToTGWConnections creates connections between TGW attachments and their transit gateways.
func TransformTGWAttachmentToTGWConnections(attachments []ec2types.TransitGatewayAttachment, region, accountID string) []sdk.Connection {
	var connections []sdk.Connection
	for _, att := range attachments {
		attID := aws.ToString(att.TransitGatewayAttachmentId)
		sourceID := resourceID(accountID, region, "ec2", "transit-gateway-attachment/"+attID)
		tgwNativeID := aws.ToString(att.TransitGatewayId)
		tgwOwnerID := aws.ToString(att.TransitGatewayOwnerId)
		targetID := resourceID(tgwOwnerID, region, "ec2", "transit-gateway/"+tgwNativeID)

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
		conn.Name = fmt.Sprintf("%s -> %s", attID, tgwNativeID)
		_ = conn.SetDirection("forward")
		connections = append(connections, conn)
	}
	return connections
}

// TransformDXVIFToGatewayConnections creates connections between DX VIFs and their DX gateways.
func TransformDXVIFToGatewayConnections(vifs []dctypes.VirtualInterface, region, accountID string) []sdk.Connection {
	var connections []sdk.Connection
	for _, vif := range vifs {
		if vif.DirectConnectGatewayId == nil {
			continue
		}
		vifID := aws.ToString(vif.VirtualInterfaceId)
		sourceID := resourceID(accountID, region, "directconnect", "vif/"+vifID)
		gwID := aws.ToString(vif.DirectConnectGatewayId)
		targetID := resourceID(accountID, region, "directconnect", "gateway/"+gwID)

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
		conn.Name = fmt.Sprintf("%s -> DX GW %s", vifID, gwID)
		_ = conn.SetDirection("forward")
		connections = append(connections, conn)
	}
	return connections
}

// TransformVPNGatewayToVPCConnections creates connections between VPN gateways and their attached VPCs.
func TransformVPNGatewayToVPCConnections(vgws []ec2types.VpnGateway, region, accountID string) []sdk.Connection {
	var connections []sdk.Connection
	for _, vgw := range vgws {
		vgwID := aws.ToString(vgw.VpnGatewayId)
		sourceID := resourceID(accountID, region, "ec2", "vpn-gateway/"+vgwID)
		for _, att := range vgw.VpcAttachments {
			if att.State != ec2types.AttachmentStatusAttached {
				continue
			}
			vpcNativeID := aws.ToString(att.VpcId)
			targetID := resourceID(accountID, region, "ec2", "vpc/"+vpcNativeID)

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
			conn.Name = fmt.Sprintf("%s -> %s", vgwID, vpcNativeID)
			_ = conn.SetDirection("forward")
			connections = append(connections, conn)
		}
	}
	return connections
}

// TransformVPNConnectionConns creates connections between VPN connections and their gateways.
func TransformVPNConnectionConns(conns []ec2types.VpnConnection, region, accountID string) []sdk.Connection {
	var connections []sdk.Connection
	for _, vpn := range conns {
		vpnID := aws.ToString(vpn.VpnConnectionId)
		vpnResID := resourceID(accountID, region, "ec2", "vpn-connection/"+vpnID)

		// VPN Connection -> VPN Gateway
		if vpn.VpnGatewayId != nil {
			vgwID := aws.ToString(vpn.VpnGatewayId)
			targetID := resourceID(accountID, region, "ec2", "vpn-gateway/"+vgwID)

			canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
				Type:      "network",
				Direction: "forward",
				Source:    vpnResID,
				Target:    targetID,
			})
			connID := sdk.BuildConnectionID(canonicalKey, 16)

			conn, err := sdk.NewConnection(connID, "network", vpnResID, targetID)
			if err == nil {
				conn.Name = fmt.Sprintf("%s -> VGW %s", vpnID, vgwID)
				_ = conn.SetDirection("forward")
				connections = append(connections, conn)
			}
		}

		// VPN Connection -> Customer Gateway
		if vpn.CustomerGatewayId != nil {
			cgwID := aws.ToString(vpn.CustomerGatewayId)
			targetID := resourceID(accountID, region, "ec2", "customer-gateway/"+cgwID)

			canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
				Type:      "network",
				Direction: "forward",
				Source:    vpnResID,
				Target:    targetID,
			})
			connID := sdk.BuildConnectionID(canonicalKey, 16)

			conn, err := sdk.NewConnection(connID, "network", vpnResID, targetID)
			if err == nil {
				conn.Name = fmt.Sprintf("%s -> CGW %s", vpnID, cgwID)
				_ = conn.SetDirection("forward")
				connections = append(connections, conn)
			}
		}
	}
	return connections
}

// TransformDHCPToVPCConnections creates connections between DHCP options and VPCs.
func TransformDHCPToVPCConnections(vpcs []ec2types.Vpc, region, accountID string) []sdk.Connection {
	var connections []sdk.Connection
	for _, vpc := range vpcs {
		if vpc.DhcpOptionsId == nil || aws.ToString(vpc.DhcpOptionsId) == "default" {
			continue
		}
		dhcpID := aws.ToString(vpc.DhcpOptionsId)
		sourceID := resourceID(accountID, region, "ec2", "dhcp-options/"+dhcpID)
		vpcNativeID := aws.ToString(vpc.VpcId)
		targetID := resourceID(accountID, region, "ec2", "vpc/"+vpcNativeID)

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
		conn.Name = fmt.Sprintf("%s -> %s", dhcpID, vpcNativeID)
		_ = conn.SetDirection("forward")
		connections = append(connections, conn)
	}
	return connections
}

// TransformLBToTargetGroupConnections creates connections between load balancers and target groups.
func TransformLBToTargetGroupConnections(tgs []elbv2types.TargetGroup, region, accountID string) []sdk.Connection {
	var connections []sdk.Connection
	for _, tg := range tgs {
		tgARN := aws.ToString(tg.TargetGroupArn)
		targetID := resourceIDFromARN(tgARN)
		for _, lbARN := range tg.LoadBalancerArns {
			sourceID := resourceIDFromARN(lbARN)

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
			conn.Name = fmt.Sprintf("LB -> %s", aws.ToString(tg.TargetGroupName))
			_ = conn.SetDirection("forward")
			connections = append(connections, conn)
		}
	}
	return connections
}

// TransformAccountGroup creates an OSIRIS JSON group for the AWS account.
func TransformAccountGroup(accountID string) sdk.Group {
	gid := sdk.GroupID(sdk.GroupIDInput{
		Type:          "osiris.aws.account",
		BoundaryToken: "aws-account::" + accountID,
	})
	g, _ := sdk.NewGroup(gid, "osiris.aws.account")
	g.Name = fmt.Sprintf("AWS Account %s", accountID)
	return g
}

// TransformVPCGroups creates OSIRIS JSON groups for each VPC.
func TransformVPCGroups(vpcs []ec2types.Vpc, region, accountID string) ([]sdk.Group, map[string]string) {
	var groups []sdk.Group
	vpcGroupMap := make(map[string]string, len(vpcs))

	for _, v := range vpcs {
		vpcID := aws.ToString(v.VpcId)
		gid := sdk.GroupID(sdk.GroupIDInput{
			Type:          "network.vpc",
			BoundaryToken: "aws-vpc::" + vpcID,
			ScopeFields: map[string]string{
				"account": accountID,
				"region":  region,
			},
		})
		vpcGroupMap[vpcID] = gid

		g, err := sdk.NewGroup(gid, "network.vpc")
		if err != nil {
			continue
		}
		name := tagName(v.Tags)
		if name == "" {
			name = vpcID
		}
		g.Name = fmt.Sprintf("VPC %s", name)
		groups = append(groups, g)
	}
	return groups, vpcGroupMap
}

// WireResourcesToVPCGroups assigns resources to their VPC groups based on vpc_id property.
func WireResourcesToVPCGroups(resources []sdk.Resource, vpcGroupMap map[string]string, groups []sdk.Group) {
	groupIndex := make(map[string]int, len(groups))
	for i, g := range groups {
		groupIndex[g.ID] = i
	}

	for _, r := range resources {
		if r.Properties == nil {
			continue
		}
		vpcID, ok := r.Properties["vpc_id"].(string)
		if !ok || vpcID == "" {
			continue
		}
		gid, ok := vpcGroupMap[vpcID]
		if !ok {
			continue
		}
		idx, ok := groupIndex[gid]
		if !ok {
			continue
		}
		groups[idx].AddMembers(r.ID)
	}
}

// WireVPCGroupsToAccount wires VPC groups as children of the account group.
func WireVPCGroupsToAccount(accountGroup *sdk.Group, vpcGroups []sdk.Group) {
	for _, g := range vpcGroups {
		accountGroup.AddChildren(g.ID)
	}
}

func mapENIStatus(s ec2types.NetworkInterfaceStatus) string {
	switch s {
	case ec2types.NetworkInterfaceStatusInUse:
		return "active"
	case ec2types.NetworkInterfaceStatusAvailable:
		return "inactive"
	default:
		return "unknown"
	}
}

func mapInstanceState(s *ec2types.InstanceState) string {
	if s == nil {
		return "unknown"
	}
	switch s.Name {
	case ec2types.InstanceStateNameRunning:
		return "active"
	case ec2types.InstanceStateNameStopped:
		return "inactive"
	case ec2types.InstanceStateNameTerminated:
		return "decommissioned"
	case ec2types.InstanceStateNamePending:
		return "provisioning"
	case ec2types.InstanceStateNameStopping, ec2types.InstanceStateNameShuttingDown:
		return "maintenance"
	default:
		return "unknown"
	}
}

func mapNATGatewayState(s ec2types.NatGatewayState) string {
	switch s {
	case ec2types.NatGatewayStateAvailable:
		return "active"
	case ec2types.NatGatewayStatePending:
		return "provisioning"
	case ec2types.NatGatewayStateDeleting, ec2types.NatGatewayStateDeleted:
		return "decommissioned"
	case ec2types.NatGatewayStateFailed:
		return "degraded"
	default:
		return "unknown"
	}
}

func mapVPCEndpointState(s ec2types.State) string {
	// The VPC endpoint API returns lowercase strings ("available", "pending") while the
	// SDK constants are title-cased ("Available", "Pending"). Normalize before comparing.
	switch strings.ToLower(string(s)) {
	case "available", "":
		return "active"
	case "pending", "pendingacceptance":
		return "provisioning"
	case "deleting", "deleted":
		return "decommissioned"
	case "rejected", "failed", "expired":
		return "degraded"
	default:
		return "unknown"
	}
}

func mapPeeringStatus(code ec2types.VpcPeeringConnectionStateReasonCode) string {
	switch code {
	case ec2types.VpcPeeringConnectionStateReasonCodeActive:
		return "active"
	case ec2types.VpcPeeringConnectionStateReasonCodePendingAcceptance, ec2types.VpcPeeringConnectionStateReasonCodeProvisioning:
		return "provisioning"
	case ec2types.VpcPeeringConnectionStateReasonCodeDeleting, ec2types.VpcPeeringConnectionStateReasonCodeDeleted:
		return "decommissioned"
	case ec2types.VpcPeeringConnectionStateReasonCodeRejected, ec2types.VpcPeeringConnectionStateReasonCodeFailed, ec2types.VpcPeeringConnectionStateReasonCodeExpired:
		return "degraded"
	default:
		return "unknown"
	}
}

func mapTGWState(s ec2types.TransitGatewayState) string {
	switch s {
	case ec2types.TransitGatewayStateAvailable:
		return "active"
	case ec2types.TransitGatewayStatePending:
		return "provisioning"
	case ec2types.TransitGatewayStateDeleting, ec2types.TransitGatewayStateDeleted:
		return "decommissioned"
	default:
		return "unknown"
	}
}

func mapTGWAttachmentState(s ec2types.TransitGatewayAttachmentState) string {
	switch s {
	case ec2types.TransitGatewayAttachmentStateAvailable:
		return "active"
	case ec2types.TransitGatewayAttachmentStatePending, ec2types.TransitGatewayAttachmentStatePendingAcceptance:
		return "provisioning"
	case ec2types.TransitGatewayAttachmentStateDeleting, ec2types.TransitGatewayAttachmentStateDeleted:
		return "decommissioned"
	case ec2types.TransitGatewayAttachmentStateRejecting, ec2types.TransitGatewayAttachmentStateRejected, ec2types.TransitGatewayAttachmentStateFailing, ec2types.TransitGatewayAttachmentStateFailed:
		return "degraded"
	default:
		return "unknown"
	}
}

func mapTGWRTState(s ec2types.TransitGatewayRouteTableState) string {
	switch s {
	case ec2types.TransitGatewayRouteTableStateAvailable:
		return "active"
	case ec2types.TransitGatewayRouteTableStatePending:
		return "provisioning"
	case ec2types.TransitGatewayRouteTableStateDeleting, ec2types.TransitGatewayRouteTableStateDeleted:
		return "decommissioned"
	default:
		return "unknown"
	}
}

func mapVGWState(s ec2types.VpnState) string {
	switch s {
	case ec2types.VpnStateAvailable:
		return "active"
	case ec2types.VpnStatePending:
		return "provisioning"
	case ec2types.VpnStateDeleting, ec2types.VpnStateDeleted:
		return "decommissioned"
	default:
		return "unknown"
	}
}

func mapVPNState(s ec2types.VpnState) string {
	return mapVGWState(s)
}

func mapFlowLogStatus(s *string) string {
	if s == nil {
		return "unknown"
	}
	if *s == "ACTIVE" {
		return "active"
	}
	return "inactive"
}

func mapAZState(s ec2types.AvailabilityZoneState) string {
	switch s {
	case ec2types.AvailabilityZoneStateAvailable:
		return "active"
	case ec2types.AvailabilityZoneStateUnavailable, ec2types.AvailabilityZoneStateImpaired:
		return "degraded"
	default:
		return "unknown"
	}
}

func mapLBState(s *elbv2types.LoadBalancerState) string {
	if s == nil {
		return "unknown"
	}
	switch s.Code {
	case elbv2types.LoadBalancerStateEnumActive:
		return "active"
	case elbv2types.LoadBalancerStateEnumProvisioning:
		return "provisioning"
	case elbv2types.LoadBalancerStateEnumFailed:
		return "degraded"
	default:
		return "unknown"
	}
}

func mapDXConnectionState(s dctypes.ConnectionState) string {
	switch s {
	case dctypes.ConnectionStateAvailable:
		return "active"
	case dctypes.ConnectionStateRequested, dctypes.ConnectionStateOrdering, dctypes.ConnectionStatePending:
		return "provisioning"
	case dctypes.ConnectionStateDeleted, dctypes.ConnectionStateDeleting:
		return "decommissioned"
	case dctypes.ConnectionStateDown:
		return "degraded"
	default:
		return "unknown"
	}
}

func mapDXGatewayState(s dctypes.DirectConnectGatewayState) string {
	switch s {
	case dctypes.DirectConnectGatewayStateAvailable:
		return "active"
	case dctypes.DirectConnectGatewayStatePending:
		return "provisioning"
	case dctypes.DirectConnectGatewayStateDeleting, dctypes.DirectConnectGatewayStateDeleted:
		return "decommissioned"
	default:
		return "unknown"
	}
}

func mapDXVIFState(s dctypes.VirtualInterfaceState) string {
	switch s {
	case dctypes.VirtualInterfaceStateAvailable:
		return "active"
	case dctypes.VirtualInterfaceStatePending, dctypes.VirtualInterfaceStateConfirming:
		return "provisioning"
	case dctypes.VirtualInterfaceStateDeleting, dctypes.VirtualInterfaceStateDeleted:
		return "decommissioned"
	case dctypes.VirtualInterfaceStateDown:
		return "degraded"
	default:
		return "unknown"
	}
}

func mapResolverRuleStatus(s r53rtypes.ResolverRuleStatus) string {
	switch s {
	case r53rtypes.ResolverRuleStatusComplete:
		return "active"
	case r53rtypes.ResolverRuleStatusUpdating:
		return "provisioning"
	case r53rtypes.ResolverRuleStatusDeleting:
		return "decommissioned"
	case r53rtypes.ResolverRuleStatusFailed:
		return "degraded"
	default:
		return "unknown"
	}
}

func mapResolverEndpointStatus(s r53rtypes.ResolverEndpointStatus) string {
	switch s {
	case r53rtypes.ResolverEndpointStatusOperational:
		return "active"
	case r53rtypes.ResolverEndpointStatusCreating, r53rtypes.ResolverEndpointStatusUpdating:
		return "provisioning"
	case r53rtypes.ResolverEndpointStatusDeleting:
		return "decommissioned"
	case r53rtypes.ResolverEndpointStatusActionNeeded, r53rtypes.ResolverEndpointStatusAutoRecovering:
		return "degraded"
	default:
		return "unknown"
	}
}

func mapGAStatus(s gatypes.AcceleratorStatus) string {
	switch s {
	case gatypes.AcceleratorStatusDeployed:
		return "active"
	case gatypes.AcceleratorStatusInProgress:
		return "provisioning"
	default:
		return "unknown"
	}
}
