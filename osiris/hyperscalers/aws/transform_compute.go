// transform_compute.go - compute orchestration resource transforms.
// Maps EKS clusters/node groups, ECS clusters/services, and Auto Scaling Groups
// to OSIRIS JSON types following the spec chapter 7 type taxonomy.
//
// Standard types (OSIRIS JSON spec chapter 7):
//   ecs:service              -> application.service (7.2.5)
//
// Custom types (osiris.aws.* namespace):
//   eks:cluster              -> osiris.aws.eks.cluster
//   eks:nodegroup            -> osiris.aws.eks.nodegroup
//   ecs:cluster              -> osiris.aws.ecs.cluster
//   autoscaling:group        -> osiris.aws.asg
//
// [OSIRIS-JSON-AWS]: https://osirisjson.org/en/docs/producers/hyperscalers/amazon-aws
// [OSIRIS-JSON-SPEC-CH07]: https://osirisjson.org/en/docs/spec/v10/07-resourcetypetaxonomy

package aws

import (
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	astypes "github.com/aws/aws-sdk-go-v2/service/autoscaling/types"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"go.osirisjson.org/producers/pkg/sdk"
)

// TransformEKSClusters converts EKS clusters to osiris.aws.eks.cluster resources.
// Returns resources and a clusterName->resourceID map for node group connection wiring.
func TransformEKSClusters(clusters []ekstypes.Cluster, region, accountID string) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(clusters))

	for _, c := range clusters {
		arn := aws.ToString(c.Arn)
		if arn == "" {
			continue
		}
		id := resourceIDFromARN(arn)
		name := aws.ToString(c.Name)
		idMap[name] = id

		prov := awsProvider(arn, "eks:cluster", region, accountID)
		r, err := sdk.NewResource(id, "osiris.aws.eks.cluster", prov)
		if err != nil {
			continue
		}
		if name != "" {
			r.Name = name
		}
		r.Status = mapEKSClusterStatus(c.Status)

		props := map[string]any{
			"kubernetes_version": aws.ToString(c.Version),
		}
		if c.ResourcesVpcConfig != nil {
			props["vpc_id"] = aws.ToString(c.ResourcesVpcConfig.VpcId)
			props["endpoint_public_access"] = c.ResourcesVpcConfig.EndpointPublicAccess
			props["endpoint_private_access"] = c.ResourcesVpcConfig.EndpointPrivateAccess
			if len(c.ResourcesVpcConfig.SubnetIds) > 0 {
				props["subnet_ids"] = c.ResourcesVpcConfig.SubnetIds
			}
			if len(c.ResourcesVpcConfig.SecurityGroupIds) > 0 {
				props["security_group_ids"] = c.ResourcesVpcConfig.SecurityGroupIds
			}
		}
		if c.Endpoint != nil {
			props["endpoint"] = aws.ToString(c.Endpoint)
		}
		if c.PlatformVersion != nil {
			props["platform_version"] = aws.ToString(c.PlatformVersion)
		}
		r.Properties = props

		if len(c.Tags) > 0 {
			r.Tags = sanitizeTags(c.Tags)
		}
		ext := map[string]any{}
		if c.RoleArn != nil {
			ext["role_arn"] = aws.ToString(c.RoleArn)
		}
		if c.Logging != nil && c.Logging.ClusterLogging != nil {
			var enabled []string
			for _, lc := range c.Logging.ClusterLogging {
				if aws.ToBool(lc.Enabled) {
					for _, t := range lc.Types {
						enabled = append(enabled, string(t))
					}
				}
			}
			if len(enabled) > 0 {
				ext["enabled_log_types"] = enabled
			}
		}
		if len(ext) > 0 {
			r.Extensions = map[string]any{"osiris.aws": ext}
		}
		attachRawBody(&r, &c)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformEKSNodeGroups converts EKS node groups to osiris.aws.eks.nodegroup resources.
// Returns resources and a nodegroupARN->resourceID map.
func TransformEKSNodeGroups(entries []EKSNodeGroupEntry, region, accountID string) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(entries))

	for _, e := range entries {
		ng := e.Nodegroup
		arn := aws.ToString(ng.NodegroupArn)
		if arn == "" {
			continue
		}
		id := resourceIDFromARN(arn)
		idMap[arn] = id

		prov := awsProvider(arn, "eks:nodegroup", region, accountID)
		r, err := sdk.NewResource(id, "osiris.aws.eks.nodegroup", prov)
		if err != nil {
			continue
		}
		if ng.NodegroupName != nil {
			r.Name = aws.ToString(ng.NodegroupName)
		}
		r.Status = mapEKSNodeGroupStatus(ng.Status)

		props := map[string]any{
			"cluster_name":  e.ClusterName,
			"ami_type":      string(ng.AmiType),
			"capacity_type": string(ng.CapacityType),
		}
		if len(ng.InstanceTypes) > 0 {
			props["instance_types"] = ng.InstanceTypes
		}
		if len(ng.Subnets) > 0 {
			props["subnet_ids"] = ng.Subnets
		}
		if ng.DiskSize != nil {
			props["disk_size_gb"] = aws.ToInt32(ng.DiskSize)
		}
		if ng.ScalingConfig != nil {
			props["desired_size"] = aws.ToInt32(ng.ScalingConfig.DesiredSize)
			props["min_size"] = aws.ToInt32(ng.ScalingConfig.MinSize)
			props["max_size"] = aws.ToInt32(ng.ScalingConfig.MaxSize)
		}
		r.Properties = props

		if len(ng.Tags) > 0 {
			r.Tags = sanitizeTags(ng.Tags)
		}
		ext := map[string]any{}
		if ng.NodeRole != nil {
			ext["node_role_arn"] = aws.ToString(ng.NodeRole)
		}
		if ng.ReleaseVersion != nil {
			ext["release_version"] = aws.ToString(ng.ReleaseVersion)
		}
		if len(ext) > 0 {
			r.Extensions = map[string]any{"osiris.aws": ext}
		}

		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformECSClusters converts ECS clusters to osiris.aws.ecs.cluster resources.
// Returns resources and a clusterARN->resourceID map.
func TransformECSClusters(clusters []ecstypes.Cluster, region, accountID string) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(clusters))

	for _, c := range clusters {
		arn := aws.ToString(c.ClusterArn)
		if arn == "" {
			continue
		}
		id := resourceIDFromARN(arn)
		idMap[arn] = id

		prov := awsProvider(arn, "ecs:cluster", region, accountID)
		r, err := sdk.NewResource(id, "osiris.aws.ecs.cluster", prov)
		if err != nil {
			continue
		}
		if c.ClusterName != nil {
			r.Name = aws.ToString(c.ClusterName)
		}
		r.Status = mapECSClusterStatus(aws.ToString(c.Status))

		props := map[string]any{
			"running_tasks_count":   c.RunningTasksCount,
			"pending_tasks_count":   c.PendingTasksCount,
			"active_services_count": c.ActiveServicesCount,
		}
		if len(c.CapacityProviders) > 0 {
			props["capacity_providers"] = c.CapacityProviders
		}
		r.Properties = props

		if len(c.Tags) > 0 {
			tagM := make(map[string]string, len(c.Tags))
			for _, t := range c.Tags {
				tagM[aws.ToString(t.Key)] = aws.ToString(t.Value)
			}
			r.Tags = sanitizeTags(tagM)
		}
		attachRawBody(&r, &c)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformECSServices converts ECS services to application.service resources.
// subnetVPCMap (subnetID->vpcID) is used to populate vpc_id for VPC group wiring.
// Returns resources and a serviceARN->resourceID map.
func TransformECSServices(services []ecstypes.Service, subnetVPCMap map[string]string, region, accountID string) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(services))

	for _, s := range services {
		arn := aws.ToString(s.ServiceArn)
		if arn == "" {
			continue
		}
		id := resourceIDFromARN(arn)
		idMap[arn] = id

		prov := awsProvider(arn, "ecs:service", region, accountID)
		r, err := sdk.NewResource(id, "application.service", prov)
		if err != nil {
			continue
		}
		if s.ServiceName != nil {
			r.Name = aws.ToString(s.ServiceName)
		}
		r.Status = mapECSServiceStatus(aws.ToString(s.Status))

		props := map[string]any{
			"cluster_arn":   aws.ToString(s.ClusterArn),
			"desired_count": s.DesiredCount,
			"running_count": s.RunningCount,
			"pending_count": s.PendingCount,
			"launch_type":   string(s.LaunchType),
			"service_type":  "container",
		}
		if s.TaskDefinition != nil {
			props["task_definition"] = aws.ToString(s.TaskDefinition)
		}
		if s.SchedulingStrategy != "" {
			props["scheduling_strategy"] = string(s.SchedulingStrategy)
		}
		if s.DeploymentController != nil {
			props["deployment_controller"] = string(s.DeploymentController.Type)
		}
		// VPC placement from network configuration.
		if s.NetworkConfiguration != nil && s.NetworkConfiguration.AwsvpcConfiguration != nil {
			nc := s.NetworkConfiguration.AwsvpcConfiguration
			if len(nc.Subnets) > 0 {
				props["subnet_ids"] = nc.Subnets
				// Derive vpc_id from first subnet for group wiring.
				if vpcID, ok := subnetVPCMap[nc.Subnets[0]]; ok {
					props["vpc_id"] = vpcID
				}
			}
			if len(nc.SecurityGroups) > 0 {
				props["security_group_ids"] = nc.SecurityGroups
			}
		}
		r.Properties = props

		if len(s.Tags) > 0 {
			tagM := make(map[string]string, len(s.Tags))
			for _, t := range s.Tags {
				tagM[aws.ToString(t.Key)] = aws.ToString(t.Value)
			}
			r.Tags = sanitizeTags(tagM)
		}
		attachRawBody(&r, &s)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformAutoScalingGroups converts ASGs to osiris.aws.asg resources.
// subnetVPCMap (subnetID->vpcID) is used to populate vpc_id for VPC group wiring.
// Returns resources and an asgName->resourceID map.
func TransformAutoScalingGroups(groups []astypes.AutoScalingGroup, subnetVPCMap map[string]string, region, accountID string) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(groups))

	for _, g := range groups {
		arn := aws.ToString(g.AutoScalingGroupARN)
		name := aws.ToString(g.AutoScalingGroupName)
		if arn == "" && name == "" {
			continue
		}
		var id string
		if arn != "" {
			id = resourceIDFromARN(arn)
		} else {
			id = resourceID(accountID, region, "autoscaling", "autoScalingGroup/"+name)
		}
		idMap[name] = id

		prov := awsProvider(arn, "autoscaling:group", region, accountID)
		r, err := sdk.NewResource(id, "osiris.aws.asg", prov)
		if err != nil {
			continue
		}
		r.Name = name
		r.Status = mapASGStatus(g.Status)

		subnetIDs := parseVPCZoneIdentifier(aws.ToString(g.VPCZoneIdentifier))
		props := map[string]any{
			"min_size":         aws.ToInt32(g.MinSize),
			"max_size":         aws.ToInt32(g.MaxSize),
			"desired_capacity": aws.ToInt32(g.DesiredCapacity),
		}
		if len(subnetIDs) > 0 {
			props["subnet_ids"] = subnetIDs
			if vpcID, ok := subnetVPCMap[subnetIDs[0]]; ok {
				props["vpc_id"] = vpcID
			}
		}
		if len(g.AvailabilityZones) > 0 {
			props["availability_zones"] = g.AvailabilityZones
		}
		if g.DefaultCooldown != nil {
			props["default_cooldown_seconds"] = aws.ToInt32(g.DefaultCooldown)
		}
		if g.HealthCheckType != nil {
			props["health_check_type"] = aws.ToString(g.HealthCheckType)
		}
		if g.HealthCheckGracePeriod != nil {
			props["health_check_grace_period_seconds"] = aws.ToInt32(g.HealthCheckGracePeriod)
		}
		if len(g.Instances) > 0 {
			props["instance_count"] = len(g.Instances)
		}
		if g.LaunchTemplate != nil {
			props["launch_template_id"] = aws.ToString(g.LaunchTemplate.LaunchTemplateId)
			props["launch_template_name"] = aws.ToString(g.LaunchTemplate.LaunchTemplateName)
			props["launch_template_version"] = aws.ToString(g.LaunchTemplate.Version)
		} else if g.LaunchConfigurationName != nil {
			props["launch_configuration_name"] = aws.ToString(g.LaunchConfigurationName)
		}
		r.Properties = props

		if len(g.Tags) > 0 {
			tagM := make(map[string]string, len(g.Tags))
			for _, t := range g.Tags {
				tagM[aws.ToString(t.Key)] = aws.ToString(t.Value)
			}
			r.Tags = sanitizeTags(tagM)
		}
		attachRawBody(&r, &g)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformEKSClusterToSubnetConnections creates network connections from EKS clusters to their subnets.
func TransformEKSClusterToSubnetConnections(clusters []ekstypes.Cluster, clusterIDMap, subnetIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, c := range clusters {
		if c.ResourcesVpcConfig == nil {
			continue
		}
		clusterName := aws.ToString(c.Name)
		sourceID, ok := clusterIDMap[clusterName]
		if !ok {
			continue
		}
		for _, subnetNativeID := range c.ResourcesVpcConfig.SubnetIds {
			targetID, ok := subnetIDMap[subnetNativeID]
			if !ok {
				continue
			}
			conn := makeConn("network", "forward", sourceID, targetID,
				fmt.Sprintf("EKS %s -> %s", clusterName, subnetNativeID))
			if conn != nil {
				connections = append(connections, *conn)
			}
		}
	}
	return connections
}

// TransformEKSClusterContainsNodeGroupConnections creates contains edges from EKS clusters to node groups.
func TransformEKSClusterContainsNodeGroupConnections(entries []EKSNodeGroupEntry, clusterIDMap, nodegroupIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, e := range entries {
		sourceID, ok := clusterIDMap[e.ClusterName]
		if !ok {
			continue
		}
		ngARN := aws.ToString(e.Nodegroup.NodegroupArn)
		targetID, ok := nodegroupIDMap[ngARN]
		if !ok {
			continue
		}
		conn := makeConn("contains", "forward", sourceID, targetID,
			fmt.Sprintf("EKS %s contains %s", e.ClusterName, aws.ToString(e.Nodegroup.NodegroupName)))
		if conn != nil {
			connections = append(connections, *conn)
		}
	}
	return connections
}

// TransformEKSNodeGroupToSubnetConnections creates network connections from node groups to their subnets.
func TransformEKSNodeGroupToSubnetConnections(entries []EKSNodeGroupEntry, nodegroupIDMap, subnetIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, e := range entries {
		ngARN := aws.ToString(e.Nodegroup.NodegroupArn)
		sourceID, ok := nodegroupIDMap[ngARN]
		if !ok {
			continue
		}
		for _, subnetNativeID := range e.Nodegroup.Subnets {
			targetID, ok := subnetIDMap[subnetNativeID]
			if !ok {
				continue
			}
			conn := makeConn("network", "forward", sourceID, targetID,
				fmt.Sprintf("nodegroup %s -> %s", aws.ToString(e.Nodegroup.NodegroupName), subnetNativeID))
			if conn != nil {
				connections = append(connections, *conn)
			}
		}
	}
	return connections
}

// TransformECSClusterContainsServiceConnections creates contains edges from ECS clusters to services.
func TransformECSClusterContainsServiceConnections(services []ecstypes.Service, clusterIDMap, serviceIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, s := range services {
		clusterARN := aws.ToString(s.ClusterArn)
		sourceID, ok := clusterIDMap[clusterARN]
		if !ok {
			continue
		}
		svcARN := aws.ToString(s.ServiceArn)
		targetID, ok := serviceIDMap[svcARN]
		if !ok {
			continue
		}
		conn := makeConn("contains", "forward", sourceID, targetID,
			fmt.Sprintf("ECS cluster -> %s", aws.ToString(s.ServiceName)))
		if conn != nil {
			connections = append(connections, *conn)
		}
	}
	return connections
}

// TransformECSServiceToSubnetConnections creates network connections from ECS services to their subnets.
func TransformECSServiceToSubnetConnections(services []ecstypes.Service, serviceIDMap, subnetIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, s := range services {
		if s.NetworkConfiguration == nil || s.NetworkConfiguration.AwsvpcConfiguration == nil {
			continue
		}
		svcARN := aws.ToString(s.ServiceArn)
		sourceID, ok := serviceIDMap[svcARN]
		if !ok {
			continue
		}
		for _, subnetNativeID := range s.NetworkConfiguration.AwsvpcConfiguration.Subnets {
			targetID, ok := subnetIDMap[subnetNativeID]
			if !ok {
				continue
			}
			conn := makeConn("network", "forward", sourceID, targetID,
				fmt.Sprintf("%s -> %s", aws.ToString(s.ServiceName), subnetNativeID))
			if conn != nil {
				connections = append(connections, *conn)
			}
		}
	}
	return connections
}

// TransformECSServiceToSGConnections creates network connections from ECS services to their security groups.
func TransformECSServiceToSGConnections(services []ecstypes.Service, serviceIDMap, sgIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, s := range services {
		if s.NetworkConfiguration == nil || s.NetworkConfiguration.AwsvpcConfiguration == nil {
			continue
		}
		svcARN := aws.ToString(s.ServiceArn)
		sourceID, ok := serviceIDMap[svcARN]
		if !ok {
			continue
		}
		for _, sgNativeID := range s.NetworkConfiguration.AwsvpcConfiguration.SecurityGroups {
			targetID, ok := sgIDMap[sgNativeID]
			if !ok {
				continue
			}
			conn := makeConn("network", "forward", sourceID, targetID,
				fmt.Sprintf("%s -> %s", aws.ToString(s.ServiceName), sgNativeID))
			if conn != nil {
				connections = append(connections, *conn)
			}
		}
	}
	return connections
}

// TransformASGToSubnetConnections creates network connections from ASGs to their subnets.
func TransformASGToSubnetConnections(groups []astypes.AutoScalingGroup, asgIDMap, subnetIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, g := range groups {
		name := aws.ToString(g.AutoScalingGroupName)
		sourceID, ok := asgIDMap[name]
		if !ok {
			continue
		}
		for _, subnetNativeID := range parseVPCZoneIdentifier(aws.ToString(g.VPCZoneIdentifier)) {
			targetID, ok := subnetIDMap[subnetNativeID]
			if !ok {
				continue
			}
			conn := makeConn("network", "forward", sourceID, targetID,
				fmt.Sprintf("ASG %s -> %s", name, subnetNativeID))
			if conn != nil {
				connections = append(connections, *conn)
			}
		}
	}
	return connections
}

// TransformASGToInstanceConnections creates containment connections from ASGs to their current EC2 instances.
func TransformASGToInstanceConnections(groups []astypes.AutoScalingGroup, asgIDMap map[string]string, region, accountID string) []sdk.Connection {
	var connections []sdk.Connection
	for _, g := range groups {
		name := aws.ToString(g.AutoScalingGroupName)
		sourceID, ok := asgIDMap[name]
		if !ok {
			continue
		}
		for _, inst := range g.Instances {
			instID := aws.ToString(inst.InstanceId)
			if instID == "" {
				continue
			}
			targetID := resourceID(accountID, region, "ec2", "instance/"+instID)
			conn := makeConn("contains", "forward", sourceID, targetID,
				fmt.Sprintf("ASG %s -> instance %s", name, instID))
			if conn != nil {
				connections = append(connections, *conn)
			}
		}
	}
	return connections
}

func mapEKSClusterStatus(s ekstypes.ClusterStatus) string {
	switch s {
	case ekstypes.ClusterStatusActive:
		return "active"
	case ekstypes.ClusterStatusCreating:
		return "provisioning"
	case ekstypes.ClusterStatusUpdating:
		return "maintenance"
	case ekstypes.ClusterStatusDeleting:
		return "decommissioned"
	case ekstypes.ClusterStatusFailed:
		return "degraded"
	default:
		return "unknown"
	}
}

func mapEKSNodeGroupStatus(s ekstypes.NodegroupStatus) string {
	switch s {
	case ekstypes.NodegroupStatusActive:
		return "active"
	case ekstypes.NodegroupStatusCreating:
		return "provisioning"
	case ekstypes.NodegroupStatusUpdating:
		return "maintenance"
	case ekstypes.NodegroupStatusDeleting:
		return "decommissioned"
	case ekstypes.NodegroupStatusDegraded, ekstypes.NodegroupStatusCreateFailed, ekstypes.NodegroupStatusDeleteFailed:
		return "degraded"
	default:
		return "unknown"
	}
}

func mapECSClusterStatus(s string) string {
	switch strings.ToUpper(s) {
	case "ACTIVE":
		return "active"
	case "INACTIVE", "DEPROVISIONING":
		return "decommissioned"
	case "PROVISIONING":
		return "provisioning"
	case "FAILED":
		return "degraded"
	default:
		return "unknown"
	}
}

func mapECSServiceStatus(s string) string {
	switch strings.ToUpper(s) {
	case "ACTIVE":
		return "active"
	case "INACTIVE":
		return "decommissioned"
	case "DRAINING":
		return "maintenance"
	default:
		return "unknown"
	}
}

func mapASGStatus(s *string) string {
	if s == nil || *s == "" {
		return "active"
	}
	if strings.Contains(*s, "Delete") {
		return "decommissioned"
	}
	return "active"
}

// makeConn builds an sdk.Connection using the canonical key pattern, returning
// nil on error so callers can skip with a single nil check.
func makeConn(connType, direction, sourceID, targetID, name string) *sdk.Connection {
	canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
		Type:      connType,
		Direction: direction,
		Source:    sourceID,
		Target:    targetID,
	})
	connID := sdk.BuildConnectionID(canonicalKey, 16)
	conn, err := sdk.NewConnection(connID, connType, sourceID, targetID)
	if err != nil {
		return nil
	}
	conn.Name = name
	_ = conn.SetDirection(direction)
	return &conn
}

// parseVPCZoneIdentifier splits a comma-separated ASG subnet ID string.
func parseVPCZoneIdentifier(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	var ids []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			ids = append(ids, p)
		}
	}
	return ids
}

// buildSubnetVPCMap builds a subnetID->vpcID lookup from collected subnets.
// Used by ECS service and ASG transforms to derive vpc_id for group wiring.
func buildSubnetVPCMap(subnets []ec2types.Subnet) map[string]string {
	m := make(map[string]string, len(subnets))
	for _, s := range subnets {
		subnetID := aws.ToString(s.SubnetId)
		vpcID := aws.ToString(s.VpcId)
		if subnetID != "" && vpcID != "" {
			m[subnetID] = vpcID
		}
	}
	return m
}
