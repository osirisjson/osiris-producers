// transform_data.go - managed data resource transforms.
// Maps RDS, DynamoDB, and ElastiCache resources to OSIRIS JSON types
// following the spec chapter 7 type taxonomy.
//
// Standard types (OSIRIS JSON spec chapter 7):
//   rds:db                         -> application.database (7.2.3)
//   rds:cluster                    -> application.database (7.2.3, properties.cluster=true)
//   dynamodb:table                 -> application.database (7.2.3)
//   elasticache:replicationgroup   -> application.cache    (7.2.4)
//
// Custom types (osiris.aws.* namespace):
//   rds:subnetgroup                -> osiris.aws.rds.subnetgroup
//   elasticache:subnetgroup        -> osiris.aws.elasticache.subnetgroup
//
// [OSIRIS-JSON-AWS]: https://osirisjson.org/en/docs/producers/hyperscalers/amazon-aws
// [OSIRIS-JSON-SPEC-CH07]: https://osirisjson.org/en/docs/spec/v10/07-resourcetypetaxonomy

package aws

import (
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	dynamodbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	elasticachetypes "github.com/aws/aws-sdk-go-v2/service/elasticache/types"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
	"go.osirisjson.org/producers/pkg/sdk"
)

// TransformRDSInstances converts RDS DB instances to application.database resources.
// Returns resources and an instanceARN->resourceID map for connection wiring.
func TransformRDSInstances(instances []rdstypes.DBInstance, region, accountID string) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(instances))

	for _, inst := range instances {
		arn := aws.ToString(inst.DBInstanceArn)
		if arn == "" {
			continue
		}
		if _, seen := idMap[arn]; seen {
			continue
		}
		id := resourceIDFromARN(arn)
		idMap[arn] = id

		name := aws.ToString(inst.DBInstanceIdentifier)
		prov := awsProvider(arn, "rds:db-instance", region, accountID)
		r, err := sdk.NewResource(id, "application.database", prov)
		if err != nil {
			continue
		}
		if name != "" {
			r.Name = name
		}
		r.Status = mapRDSInstanceStatus(aws.ToString(inst.DBInstanceStatus))

		props := map[string]any{
			"engine":         aws.ToString(inst.Engine),
			"engine_version": aws.ToString(inst.EngineVersion),
			"instance_class": aws.ToString(inst.DBInstanceClass),
		}
		if aws.ToInt32(inst.AllocatedStorage) > 0 {
			props["allocated_storage_gb"] = aws.ToInt32(inst.AllocatedStorage)
		}
		if inst.StorageType != nil {
			props["storage_type"] = aws.ToString(inst.StorageType)
		}
		props["multi_az"] = aws.ToBool(inst.MultiAZ)
		props["publicly_accessible"] = aws.ToBool(inst.PubliclyAccessible)
		props["deletion_protection"] = aws.ToBool(inst.DeletionProtection)
		if aws.ToInt32(inst.BackupRetentionPeriod) > 0 {
			props["backup_retention_period_days"] = aws.ToInt32(inst.BackupRetentionPeriod)
		}
		if inst.DBSubnetGroup != nil && inst.DBSubnetGroup.DBSubnetGroupName != nil {
			props["db_subnet_group_name"] = aws.ToString(inst.DBSubnetGroup.DBSubnetGroupName)
		}
		if inst.AvailabilityZone != nil {
			props["availability_zone"] = aws.ToString(inst.AvailabilityZone)
		}
		if clusterID := aws.ToString(inst.DBClusterIdentifier); clusterID != "" {
			props["db_cluster_identifier"] = clusterID
		}
		if inst.DbInstancePort != nil {
			props["port"] = aws.ToInt32(inst.DbInstancePort)
		}
		if inst.PreferredMaintenanceWindow != nil {
			props["preferred_maintenance_window"] = aws.ToString(inst.PreferredMaintenanceWindow)
		}

		var sgIDs []string
		for _, sg := range inst.VpcSecurityGroups {
			if sg.VpcSecurityGroupId != nil {
				sgIDs = append(sgIDs, aws.ToString(sg.VpcSecurityGroupId))
			}
		}
		if len(sgIDs) > 0 {
			props["vpc_security_group_ids"] = sgIDs
		}

		r.Properties = props

		tags := rdsTagMap(inst.TagList)
		if len(tags) > 0 {
			r.Tags = sanitizeTags(tags)
		}
		attachRawBody(&r, &inst)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformRDSClusters converts Aurora DB clusters to application.database resources.
// Returns resources and a clusterARN->resourceID map for connection wiring.
func TransformRDSClusters(clusters []rdstypes.DBCluster, region, accountID string) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(clusters))

	for _, c := range clusters {
		arn := aws.ToString(c.DBClusterArn)
		if arn == "" {
			continue
		}
		if _, seen := idMap[arn]; seen {
			continue
		}
		id := resourceIDFromARN(arn)
		idMap[arn] = id

		name := aws.ToString(c.DBClusterIdentifier)
		prov := awsProvider(arn, "rds:cluster", region, accountID)
		r, err := sdk.NewResource(id, "application.database", prov)
		if err != nil {
			continue
		}
		if name != "" {
			r.Name = name
		}
		r.Status = mapRDSClusterStatus(aws.ToString(c.Status))

		props := map[string]any{
			"engine":         aws.ToString(c.Engine),
			"engine_version": aws.ToString(c.EngineVersion),
			"cluster":        true,
		}
		if len(c.AvailabilityZones) > 0 {
			props["availability_zones"] = c.AvailabilityZones
		}
		if c.MultiAZ != nil {
			props["multi_az"] = aws.ToBool(c.MultiAZ)
		}
		props["deletion_protection"] = c.DeletionProtection
		if c.BackupRetentionPeriod != nil {
			props["backup_retention_period_days"] = aws.ToInt32(c.BackupRetentionPeriod)
		}
		if c.Port != nil {
			props["port"] = aws.ToInt32(c.Port)
		}
		if sg := aws.ToString(c.DBSubnetGroup); sg != "" {
			props["db_subnet_group_name"] = sg
		}
		if c.PreferredMaintenanceWindow != nil {
			props["preferred_maintenance_window"] = aws.ToString(c.PreferredMaintenanceWindow)
		}
		props["member_count"] = len(c.DBClusterMembers)

		var sgIDs []string
		for _, sg := range c.VpcSecurityGroups {
			if sg.VpcSecurityGroupId != nil {
				sgIDs = append(sgIDs, aws.ToString(sg.VpcSecurityGroupId))
			}
		}
		if len(sgIDs) > 0 {
			props["vpc_security_group_ids"] = sgIDs
		}

		r.Properties = props

		tags := rdsTagMap(c.TagList)
		if len(tags) > 0 {
			r.Tags = sanitizeTags(tags)
		}
		attachRawBody(&r, &c)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformRDSSubnetGroups converts RDS DB subnet groups to osiris.aws.rds.subnetgroup resources.
// Returns resources and a subnetGroupName->resourceID map for connection wiring.
func TransformRDSSubnetGroups(groups []rdstypes.DBSubnetGroup, region, accountID string) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(groups))

	for _, g := range groups {
		arn := aws.ToString(g.DBSubnetGroupArn)
		name := aws.ToString(g.DBSubnetGroupName)
		if arn == "" && name == "" {
			continue
		}
		nativeID := arn
		if nativeID == "" {
			nativeID = fmt.Sprintf("arn:aws:rds:%s:%s:subgrp:%s", region, accountID, name)
		}
		id := resourceIDFromARN(nativeID)
		if name != "" {
			idMap[name] = id
		}

		prov := awsProvider(nativeID, "rds:subnet-group", region, accountID)
		r, err := sdk.NewResource(id, "osiris.aws.rds.subnetgroup", prov)
		if err != nil {
			continue
		}
		if name != "" {
			r.Name = name
		}
		r.Status = "active"

		props := map[string]any{}
		if g.VpcId != nil {
			props["vpc_id"] = aws.ToString(g.VpcId)
		}
		var subnetIDs []string
		for _, s := range g.Subnets {
			if s.SubnetIdentifier != nil {
				subnetIDs = append(subnetIDs, aws.ToString(s.SubnetIdentifier))
			}
		}
		if len(subnetIDs) > 0 {
			props["subnet_ids"] = subnetIDs
		}
		if g.SubnetGroupStatus != nil {
			props["status"] = aws.ToString(g.SubnetGroupStatus)
		}

		r.Properties = props
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformDynamoDBTables converts DynamoDB tables to application.database resources.
// Returns resources and a tableARN->resourceID map.
func TransformDynamoDBTables(tables []dynamodbtypes.TableDescription, region, accountID string) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(tables))

	for _, t := range tables {
		arn := aws.ToString(t.TableArn)
		if arn == "" {
			continue
		}
		id := resourceIDFromARN(arn)
		idMap[arn] = id

		name := aws.ToString(t.TableName)
		prov := awsProvider(arn, "dynamodb:table", region, accountID)
		r, err := sdk.NewResource(id, "application.database", prov)
		if err != nil {
			continue
		}
		if name != "" {
			r.Name = name
		}
		r.Status = mapDynamoDBTableStatus(t.TableStatus)

		props := map[string]any{}
		if t.ItemCount != nil {
			props["item_count"] = aws.ToInt64(t.ItemCount)
		}
		if t.BillingModeSummary != nil {
			props["billing_mode"] = string(t.BillingModeSummary.BillingMode)
		}
		if t.ProvisionedThroughput != nil {
			props["read_capacity_units"] = aws.ToInt64(t.ProvisionedThroughput.ReadCapacityUnits)
			props["write_capacity_units"] = aws.ToInt64(t.ProvisionedThroughput.WriteCapacityUnits)
		}
		if len(t.GlobalSecondaryIndexes) > 0 {
			props["global_secondary_index_count"] = len(t.GlobalSecondaryIndexes)
		}
		if len(t.LocalSecondaryIndexes) > 0 {
			props["local_secondary_index_count"] = len(t.LocalSecondaryIndexes)
		}
		if t.StreamSpecification != nil && t.StreamSpecification.StreamEnabled != nil {
			props["stream_enabled"] = aws.ToBool(t.StreamSpecification.StreamEnabled)
		}

		var keySchema []map[string]string
		for _, k := range t.KeySchema {
			keySchema = append(keySchema, map[string]string{
				"attribute_name": aws.ToString(k.AttributeName),
				"key_type":       string(k.KeyType),
			})
		}
		if len(keySchema) > 0 {
			props["key_schema"] = keySchema
		}

		r.Properties = props
		attachRawBody(&r, &t)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformElastiCacheReplicationGroups converts ElastiCache replication groups
// to application.cache resources. Returns resources and a groupID->resourceID map.
func TransformElastiCacheReplicationGroups(groups []elasticachetypes.ReplicationGroup, region, accountID string) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(groups))

	for _, g := range groups {
		arn := aws.ToString(g.ARN)
		rgID := aws.ToString(g.ReplicationGroupId)
		if rgID == "" {
			continue
		}
		nativeID := arn
		if nativeID == "" {
			nativeID = fmt.Sprintf("arn:aws:elasticache:%s:%s:replicationgroup:%s", region, accountID, rgID)
		}
		id := resourceIDFromARN(nativeID)
		idMap[rgID] = id

		prov := awsProvider(nativeID, "elasticache:replication-group", region, accountID)
		r, err := sdk.NewResource(id, "application.cache", prov)
		if err != nil {
			continue
		}
		r.Name = rgID
		r.Status = mapElastiCacheRGStatus(aws.ToString(g.Status))

		props := map[string]any{}
		if g.Description != nil {
			props["description"] = aws.ToString(g.Description)
		}
		if g.ClusterEnabled != nil {
			props["cluster_enabled"] = aws.ToBool(g.ClusterEnabled)
		}
		props["node_group_count"] = len(g.NodeGroups)
		props["automatic_failover"] = string(g.AutomaticFailover)
		if g.AtRestEncryptionEnabled != nil {
			props["at_rest_encryption_enabled"] = aws.ToBool(g.AtRestEncryptionEnabled)
		}
		if g.TransitEncryptionEnabled != nil {
			props["transit_encryption_enabled"] = aws.ToBool(g.TransitEncryptionEnabled)
		}
		if g.SnapshotRetentionLimit != nil {
			props["snapshot_retention_limit_days"] = aws.ToInt32(g.SnapshotRetentionLimit)
		}

		// Collect member cluster IDs for context.
		var memberIDs []string
		for _, ng := range g.NodeGroups {
			for _, nm := range ng.NodeGroupMembers {
				if nm.CacheClusterId != nil {
					memberIDs = append(memberIDs, aws.ToString(nm.CacheClusterId))
				}
			}
		}
		if len(memberIDs) > 0 {
			props["member_cluster_ids"] = memberIDs
		}

		r.Properties = props
		attachRawBody(&r, &g)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformElastiCacheSubnetGroups converts ElastiCache cache subnet groups
// to osiris.aws.elasticache.subnetgroup resources.
// Returns resources and a subnetGroupName->resourceID map.
func TransformElastiCacheSubnetGroups(groups []elasticachetypes.CacheSubnetGroup, region, accountID string) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(groups))

	for _, g := range groups {
		arn := aws.ToString(g.ARN)
		name := aws.ToString(g.CacheSubnetGroupName)
		if name == "" {
			continue
		}
		nativeID := arn
		if nativeID == "" {
			nativeID = fmt.Sprintf("arn:aws:elasticache:%s:%s:subnetgroup:%s", region, accountID, name)
		}
		id := resourceIDFromARN(nativeID)
		idMap[name] = id

		prov := awsProvider(nativeID, "elasticache:subnet-group", region, accountID)
		r, err := sdk.NewResource(id, "osiris.aws.elasticache.subnetgroup", prov)
		if err != nil {
			continue
		}
		r.Name = name
		r.Status = "active"

		props := map[string]any{}
		if g.VpcId != nil {
			props["vpc_id"] = aws.ToString(g.VpcId)
		}
		var subnetIDs []string
		for _, s := range g.Subnets {
			if s.SubnetIdentifier != nil {
				subnetIDs = append(subnetIDs, aws.ToString(s.SubnetIdentifier))
			}
		}
		if len(subnetIDs) > 0 {
			props["subnet_ids"] = subnetIDs
		}

		r.Properties = props
		resources = append(resources, r)
	}
	return resources, idMap
}


// TransformRDSInstanceToSubnetGroupConnections wires RDS instances to their subnet groups.
func TransformRDSInstanceToSubnetGroupConnections(instances []rdstypes.DBInstance, instanceIDMap, sgroupIDMap map[string]string) []sdk.Connection {
	var conns []sdk.Connection
	for _, inst := range instances {
		instARN := aws.ToString(inst.DBInstanceArn)
		srcID, ok := instanceIDMap[instARN]
		if !ok {
			continue
		}
		if inst.DBSubnetGroup == nil {
			continue
		}
		sgName := aws.ToString(inst.DBSubnetGroup.DBSubnetGroupName)
		tgtID, ok := sgroupIDMap[sgName]
		if !ok {
			continue
		}
		instID := aws.ToString(inst.DBInstanceIdentifier)
		conn := makeConn("network", "forward", srcID, tgtID,
			fmt.Sprintf("RDS instance %s -> subnet group %s", instID, sgName))
		if conn != nil {
			conns = append(conns, *conn)
		}
	}
	return conns
}

// TransformRDSInstanceToSGConnections wires RDS instances to security groups.
func TransformRDSInstanceToSGConnections(instances []rdstypes.DBInstance, instanceIDMap, sgIDMap map[string]string) []sdk.Connection {
	var conns []sdk.Connection
	for _, inst := range instances {
		instARN := aws.ToString(inst.DBInstanceArn)
		srcID, ok := instanceIDMap[instARN]
		if !ok {
			continue
		}
		instName := aws.ToString(inst.DBInstanceIdentifier)
		for _, sg := range inst.VpcSecurityGroups {
			sgID := aws.ToString(sg.VpcSecurityGroupId)
			tgtID, ok := sgIDMap[sgID]
			if !ok {
				continue
			}
			conn := makeConn("network", "forward", srcID, tgtID,
				fmt.Sprintf("RDS instance %s -> security group %s", instName, sgID))
			if conn != nil {
				conns = append(conns, *conn)
			}
		}
	}
	return conns
}

// TransformRDSClusterToSubnetGroupConnections wires Aurora clusters to their subnet groups.
func TransformRDSClusterToSubnetGroupConnections(clusters []rdstypes.DBCluster, clusterIDMap, sgroupIDMap map[string]string) []sdk.Connection {
	var conns []sdk.Connection
	for _, c := range clusters {
		clusterARN := aws.ToString(c.DBClusterArn)
		srcID, ok := clusterIDMap[clusterARN]
		if !ok {
			continue
		}
		sgName := aws.ToString(c.DBSubnetGroup)
		if sgName == "" {
			continue
		}
		tgtID, ok := sgroupIDMap[sgName]
		if !ok {
			continue
		}
		clusterName := aws.ToString(c.DBClusterIdentifier)
		conn := makeConn("network", "forward", srcID, tgtID,
			fmt.Sprintf("RDS cluster %s -> subnet group %s", clusterName, sgName))
		if conn != nil {
			conns = append(conns, *conn)
		}
	}
	return conns
}

// TransformRDSClusterToSGConnections wires Aurora clusters to security groups.
func TransformRDSClusterToSGConnections(clusters []rdstypes.DBCluster, clusterIDMap, sgIDMap map[string]string) []sdk.Connection {
	var conns []sdk.Connection
	for _, c := range clusters {
		clusterARN := aws.ToString(c.DBClusterArn)
		srcID, ok := clusterIDMap[clusterARN]
		if !ok {
			continue
		}
		clusterName := aws.ToString(c.DBClusterIdentifier)
		for _, sg := range c.VpcSecurityGroups {
			sgID := aws.ToString(sg.VpcSecurityGroupId)
			tgtID, ok := sgIDMap[sgID]
			if !ok {
				continue
			}
			conn := makeConn("network", "forward", srcID, tgtID,
				fmt.Sprintf("RDS cluster %s -> security group %s", clusterName, sgID))
			if conn != nil {
				conns = append(conns, *conn)
			}
		}
	}
	return conns
}

// TransformRDSClusterContainsInstanceConnections wires Aurora clusters to their member instances.
func TransformRDSClusterContainsInstanceConnections(clusters []rdstypes.DBCluster, clusterIDMap, instanceIDMap map[string]string) []sdk.Connection {
	var conns []sdk.Connection
	for _, c := range clusters {
		clusterARN := aws.ToString(c.DBClusterArn)
		srcID, ok := clusterIDMap[clusterARN]
		if !ok {
			continue
		}
		clusterName := aws.ToString(c.DBClusterIdentifier)
		for _, member := range c.DBClusterMembers {
			// Members only carry DBInstanceIdentifier; reconstruct ARN to look up the resource ID.
			memberID := aws.ToString(member.DBInstanceIdentifier)
			memberARN := fmt.Sprintf("arn:aws:rds:%s:%s:db:%s",
				strings.Split(clusterARN, ":")[3],
				strings.Split(clusterARN, ":")[4],
				memberID)
			tgtID, ok := instanceIDMap[memberARN]
			if !ok {
				continue
			}
			conn := makeConn("contains", "forward", srcID, tgtID,
				fmt.Sprintf("RDS cluster %s contains instance %s", clusterName, memberID))
			if conn != nil {
				conns = append(conns, *conn)
			}
		}
	}
	return conns
}

// TransformRDSSubnetGroupToSubnetConnections wires RDS subnet groups to VPC subnets.
func TransformRDSSubnetGroupToSubnetConnections(groups []rdstypes.DBSubnetGroup, sgroupIDMap, subnetIDMap map[string]string) []sdk.Connection {
	var conns []sdk.Connection
	for _, g := range groups {
		sgName := aws.ToString(g.DBSubnetGroupName)
		srcID, ok := sgroupIDMap[sgName]
		if !ok {
			continue
		}
		for _, s := range g.Subnets {
			subnetID := aws.ToString(s.SubnetIdentifier)
			tgtID, ok := subnetIDMap[subnetID]
			if !ok {
				continue
			}
			conn := makeConn("network", "forward", srcID, tgtID,
				fmt.Sprintf("RDS subnet group %s -> subnet %s", sgName, subnetID))
			if conn != nil {
				conns = append(conns, *conn)
			}
		}
	}
	return conns
}

// TransformElastiCacheSubnetGroupToSubnetConnections wires ElastiCache subnet groups to VPC subnets.
func TransformElastiCacheSubnetGroupToSubnetConnections(groups []elasticachetypes.CacheSubnetGroup, ecsgIDMap, subnetIDMap map[string]string) []sdk.Connection {
	var conns []sdk.Connection
	for _, g := range groups {
		sgName := aws.ToString(g.CacheSubnetGroupName)
		srcID, ok := ecsgIDMap[sgName]
		if !ok {
			continue
		}
		for _, s := range g.Subnets {
			subnetID := aws.ToString(s.SubnetIdentifier)
			tgtID, ok := subnetIDMap[subnetID]
			if !ok {
				continue
			}
			conn := makeConn("network", "forward", srcID, tgtID,
				fmt.Sprintf("ElastiCache subnet group %s -> subnet %s", sgName, subnetID))
			if conn != nil {
				conns = append(conns, *conn)
			}
		}
	}
	return conns
}


func mapRDSInstanceStatus(s string) string {
	switch s {
	case "available":
		return "active"
	case "stopped":
		return "inactive"
	case "starting", "creating", "deleting", "restoring", "backing-up":
		return "pending"
	case "stopping", "rebooting", "modifying", "upgrading", "maintenance",
		"storage-optimization", "storage-full":
		return "degraded"
	case "failed", "incompatible-network", "incompatible-parameters",
		"incompatible-restore", "inaccessible-encryption-credentials":
		return "inactive"
	default:
		return "unknown"
	}
}

func mapRDSClusterStatus(s string) string {
	switch s {
	case "available":
		return "active"
	case "stopped":
		return "inactive"
	case "creating", "deleting", "restoring", "migrating", "backtracking":
		return "pending"
	case "failing-over", "modifying", "rebooting", "renaming", "upgrading",
		"storage-optimization":
		return "degraded"
	case "failed", "migration-failed", "inaccessible-encryption-credentials":
		return "inactive"
	default:
		return "unknown"
	}
}

func mapDynamoDBTableStatus(s dynamodbtypes.TableStatus) string {
	switch s {
	case dynamodbtypes.TableStatusActive:
		return "active"
	case dynamodbtypes.TableStatusArchived:
		return "inactive"
	case dynamodbtypes.TableStatusCreating, dynamodbtypes.TableStatusDeleting,
		dynamodbtypes.TableStatusArchiving:
		return "pending"
	case dynamodbtypes.TableStatusUpdating:
		return "degraded"
	case dynamodbtypes.TableStatusInaccessibleEncryptionCredentials:
		return "inactive"
	default:
		return "unknown"
	}
}

func mapElastiCacheRGStatus(s string) string {
	switch s {
	case "available":
		return "active"
	case "creating", "deleting":
		return "pending"
	case "modifying", "snapshotting":
		return "degraded"
	case "create-failed":
		return "inactive"
	default:
		return "unknown"
	}
}


// rdsTagMap converts RDS []rdstypes.Tag to a map[string]string.
func rdsTagMap(tags []rdstypes.Tag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	m := make(map[string]string, len(tags))
	for _, t := range tags {
		m[aws.ToString(t.Key)] = aws.ToString(t.Value)
	}
	return m
}
