// transform_databases_ext.go - extended database resource transforms.
// Maps DocumentDB, Neptune, Redshift, OpenSearch, and MemoryDB resources to
// OSIRIS JSON types.
//
// All types are custom osiris.aws.* namespace:
//   docdb:cluster              -> osiris.aws.docdb.cluster
//   docdb:subnetgroup          -> osiris.aws.docdb.subnetgroup
//   neptune:cluster            -> osiris.aws.neptune.cluster
//   neptune:subnetgroup        -> osiris.aws.neptune.subnetgroup
//   redshift:cluster           -> osiris.aws.redshift.cluster
//   redshift:subnetgroup       -> osiris.aws.redshift.subnetgroup
//   opensearch:domain          -> osiris.aws.opensearch.domain
//   memorydb:cluster           -> osiris.aws.memorydb.cluster
//   memorydb:subnetgroup       -> osiris.aws.memorydb.subnetgroup
//
// [OSIRIS-JSON-AWS]: https://osirisjson.org/en/docs/producers/hyperscalers/amazon-aws
// [OSIRIS-JSON-SPEC-CH07]: https://osirisjson.org/en/docs/spec/v10/07-resourcetypetaxonomy

package aws

import (
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	docdbtypes "github.com/aws/aws-sdk-go-v2/service/docdb/types"
	memorydbtypes "github.com/aws/aws-sdk-go-v2/service/memorydb/types"
	neptunetypes "github.com/aws/aws-sdk-go-v2/service/neptune/types"
	openstypes "github.com/aws/aws-sdk-go-v2/service/opensearch/types"
	redshifttypes "github.com/aws/aws-sdk-go-v2/service/redshift/types"
	"go.osirisjson.org/producers/pkg/sdk"
)

// TransformDocDBClusters converts DocumentDB cluster descriptions to
// osiris.aws.docdb.cluster resources.
func TransformDocDBClusters(clusters []docdbtypes.DBCluster, region, accountID string) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(clusters))

	for _, c := range clusters {
		arn := aws.ToString(c.DBClusterArn)
		clusterID := aws.ToString(c.DBClusterIdentifier)
		if clusterID == "" {
			continue
		}
		nativeID := arn
		if nativeID == "" {
			nativeID = fmt.Sprintf("arn:aws:rds:%s:%s:cluster:%s", region, accountID, clusterID)
		}
		id := resourceIDFromARN(nativeID)
		idMap[arn] = id

		prov := awsProvider(nativeID, "docdb:cluster", region, accountID)
		r, err := sdk.NewResource(id, "osiris.aws.docdb.cluster", prov)
		if err != nil {
			continue
		}
		r.Name = clusterID
		r.Status = mapSimpleStatus(aws.ToString(c.Status), "available")

		props := map[string]any{}
		if c.Engine != nil {
			props["engine"] = aws.ToString(c.Engine)
		}
		if c.EngineVersion != nil {
			props["engine_version"] = aws.ToString(c.EngineVersion)
		}
		if c.MultiAZ != nil {
			props["multi_az"] = aws.ToBool(c.MultiAZ)
		}
		if c.Port != nil {
			props["port"] = aws.ToInt32(c.Port)
		}
		if c.DeletionProtection != nil {
			props["deletion_protection"] = aws.ToBool(c.DeletionProtection)
		}
		if c.StorageEncrypted != nil {
			props["storage_encrypted"] = aws.ToBool(c.StorageEncrypted)
		}
		if c.BackupRetentionPeriod != nil {
			props["backup_retention_period"] = aws.ToInt32(c.BackupRetentionPeriod)
		}
		if c.MasterUsername != nil {
			props["master_username"] = aws.ToString(c.MasterUsername)
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformDocDBSubnetGroups converts DocumentDB subnet group descriptions to
// osiris.aws.docdb.subnetgroup resources.
func TransformDocDBSubnetGroups(groups []docdbtypes.DBSubnetGroup, region, accountID string) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(groups))

	for _, g := range groups {
		arn := aws.ToString(g.DBSubnetGroupArn)
		name := aws.ToString(g.DBSubnetGroupName)
		if name == "" {
			continue
		}
		nativeID := arn
		if nativeID == "" {
			nativeID = fmt.Sprintf("arn:aws:rds:%s:%s:subgrp:%s", region, accountID, name)
		}
		id := resourceIDFromARN(nativeID)
		idMap[name] = id

		prov := awsProvider(nativeID, "docdb:subnet-group", region, accountID)
		r, err := sdk.NewResource(id, "osiris.aws.docdb.subnetgroup", prov)
		if err != nil {
			continue
		}
		r.Name = name
		r.Status = "active"

		subnetIDs := make([]string, 0, len(g.Subnets))
		for _, s := range g.Subnets {
			if s.SubnetIdentifier != nil {
				subnetIDs = append(subnetIDs, aws.ToString(s.SubnetIdentifier))
			}
		}
		props := map[string]any{}
		if g.VpcId != nil {
			props["vpc_id"] = aws.ToString(g.VpcId)
		}
		if len(subnetIDs) > 0 {
			props["subnet_ids"] = subnetIDs
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformNeptuneClusters converts Neptune cluster descriptions to
// osiris.aws.neptune.cluster resources.
func TransformNeptuneClusters(clusters []neptunetypes.DBCluster, region, accountID string) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(clusters))

	for _, c := range clusters {
		arn := aws.ToString(c.DBClusterArn)
		clusterID := aws.ToString(c.DBClusterIdentifier)
		if clusterID == "" {
			continue
		}
		nativeID := arn
		if nativeID == "" {
			nativeID = fmt.Sprintf("arn:aws:rds:%s:%s:cluster:%s", region, accountID, clusterID)
		}
		id := resourceIDFromARN(nativeID)
		idMap[arn] = id

		prov := awsProvider(nativeID, "neptune:cluster", region, accountID)
		r, err := sdk.NewResource(id, "osiris.aws.neptune.cluster", prov)
		if err != nil {
			continue
		}
		r.Name = clusterID
		r.Status = mapSimpleStatus(aws.ToString(c.Status), "available")

		props := map[string]any{}
		if c.Engine != nil {
			props["engine"] = aws.ToString(c.Engine)
		}
		if c.EngineVersion != nil {
			props["engine_version"] = aws.ToString(c.EngineVersion)
		}
		if c.MultiAZ != nil {
			props["multi_az"] = aws.ToBool(c.MultiAZ)
		}
		if c.Port != nil {
			props["port"] = aws.ToInt32(c.Port)
		}
		if c.DeletionProtection != nil {
			props["deletion_protection"] = aws.ToBool(c.DeletionProtection)
		}
		if c.StorageEncrypted != nil {
			props["storage_encrypted"] = aws.ToBool(c.StorageEncrypted)
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformNeptuneSubnetGroups converts Neptune subnet group descriptions to
// osiris.aws.neptune.subnetgroup resources.
func TransformNeptuneSubnetGroups(groups []neptunetypes.DBSubnetGroup, region, accountID string) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(groups))

	for _, g := range groups {
		arn := aws.ToString(g.DBSubnetGroupArn)
		name := aws.ToString(g.DBSubnetGroupName)
		if name == "" {
			continue
		}
		nativeID := arn
		if nativeID == "" {
			nativeID = fmt.Sprintf("arn:aws:rds:%s:%s:subgrp:%s", region, accountID, name)
		}
		id := resourceIDFromARN(nativeID)
		idMap[name] = id

		prov := awsProvider(nativeID, "neptune:subnet-group", region, accountID)
		r, err := sdk.NewResource(id, "osiris.aws.neptune.subnetgroup", prov)
		if err != nil {
			continue
		}
		r.Name = name
		r.Status = "active"

		subnetIDs := make([]string, 0, len(g.Subnets))
		for _, s := range g.Subnets {
			if s.SubnetIdentifier != nil {
				subnetIDs = append(subnetIDs, aws.ToString(s.SubnetIdentifier))
			}
		}
		props := map[string]any{}
		if g.VpcId != nil {
			props["vpc_id"] = aws.ToString(g.VpcId)
		}
		if len(subnetIDs) > 0 {
			props["subnet_ids"] = subnetIDs
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformRedshiftClusters converts Redshift cluster descriptions to
// osiris.aws.redshift.cluster resources.
func TransformRedshiftClusters(clusters []redshifttypes.Cluster, region, accountID string) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(clusters))

	for _, c := range clusters {
		clusterID := aws.ToString(c.ClusterIdentifier)
		if clusterID == "" {
			continue
		}
		// Redshift clusters have ClusterNamespaceArn (namespace) and no standard cluster ARN.
		// Construct a canonical ARN.
		nativeID := fmt.Sprintf("arn:aws:redshift:%s:%s:cluster:%s", region, accountID, clusterID)
		id := resourceIDFromARN(nativeID)
		idMap[clusterID] = id

		prov := awsProvider(nativeID, "redshift:cluster", region, accountID)
		r, err := sdk.NewResource(id, "osiris.aws.redshift.cluster", prov)
		if err != nil {
			continue
		}
		r.Name = clusterID
		r.Status = mapSimpleStatus(aws.ToString(c.ClusterStatus), "available")

		props := map[string]any{}
		if c.NodeType != nil {
			props["node_type"] = aws.ToString(c.NodeType)
		}
		if c.NumberOfNodes != nil {
			props["number_of_nodes"] = aws.ToInt32(c.NumberOfNodes)
		}
		if c.ClusterVersion != nil {
			props["cluster_version"] = aws.ToString(c.ClusterVersion)
		}
		if c.DBName != nil {
			props["db_name"] = aws.ToString(c.DBName)
		}
		if c.MasterUsername != nil {
			props["master_username"] = aws.ToString(c.MasterUsername)
		}
		if c.Encrypted != nil {
			props["encrypted"] = aws.ToBool(c.Encrypted)
		}
		if c.PubliclyAccessible != nil {
			props["publicly_accessible"] = aws.ToBool(c.PubliclyAccessible)
		}
		if c.VpcId != nil {
			props["vpc_id"] = aws.ToString(c.VpcId)
		}
		if c.Endpoint != nil && c.Endpoint.Port != nil {
			props["port"] = aws.ToInt32(c.Endpoint.Port)
		}
		r.Properties = props
		if len(c.Tags) > 0 {
			tags := make(map[string]string, len(c.Tags))
			for _, t := range c.Tags {
				if t.Key != nil {
					tags[aws.ToString(t.Key)] = aws.ToString(t.Value)
				}
			}
			r.Tags = sanitizeTags(tags)
		}
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformRedshiftSubnetGroups converts Redshift cluster subnet group descriptions to
// osiris.aws.redshift.subnetgroup resources.
func TransformRedshiftSubnetGroups(groups []redshifttypes.ClusterSubnetGroup, region, accountID string) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(groups))

	for _, g := range groups {
		name := aws.ToString(g.ClusterSubnetGroupName)
		if name == "" {
			continue
		}
		nativeID := fmt.Sprintf("arn:aws:redshift:%s:%s:subnetgroup:%s", region, accountID, name)
		id := resourceIDFromARN(nativeID)
		idMap[name] = id

		prov := awsProvider(nativeID, "redshift:subnet-group", region, accountID)
		r, err := sdk.NewResource(id, "osiris.aws.redshift.subnetgroup", prov)
		if err != nil {
			continue
		}
		r.Name = name
		r.Status = "active"

		subnetIDs := make([]string, 0, len(g.Subnets))
		for _, s := range g.Subnets {
			if s.SubnetIdentifier != nil {
				subnetIDs = append(subnetIDs, aws.ToString(s.SubnetIdentifier))
			}
		}
		props := map[string]any{}
		if g.VpcId != nil {
			props["vpc_id"] = aws.ToString(g.VpcId)
		}
		if len(subnetIDs) > 0 {
			props["subnet_ids"] = subnetIDs
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformOpenSearchDomains converts OpenSearch domain status descriptions to
// osiris.aws.opensearch.domain resources.
func TransformOpenSearchDomains(domains []openstypes.DomainStatus, region, accountID string) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(domains))

	for _, d := range domains {
		arn := aws.ToString(d.ARN)
		domainName := aws.ToString(d.DomainName)
		if domainName == "" {
			continue
		}
		nativeID := arn
		if nativeID == "" {
			nativeID = fmt.Sprintf("arn:aws:es:%s:%s:domain/%s", region, accountID, domainName)
		}
		id := resourceIDFromARN(nativeID)
		idMap[arn] = id

		prov := awsProvider(nativeID, "opensearch:domain", region, accountID)
		r, err := sdk.NewResource(id, "osiris.aws.opensearch.domain", prov)
		if err != nil {
			continue
		}
		r.Name = domainName
		r.Status = mapOpenSearchStatus(d)

		props := map[string]any{}
		if d.EngineVersion != nil {
			props["engine_version"] = aws.ToString(d.EngineVersion)
		}
		if d.Endpoint != nil {
			props["endpoint"] = aws.ToString(d.Endpoint)
		}
		if d.VPCOptions != nil {
			if len(d.VPCOptions.SubnetIds) > 0 {
				props["subnet_ids"] = d.VPCOptions.SubnetIds
			}
			if len(d.VPCOptions.SecurityGroupIds) > 0 {
				props["security_group_ids"] = d.VPCOptions.SecurityGroupIds
			}
			if d.VPCOptions.VPCId != nil {
				props["vpc_id"] = aws.ToString(d.VPCOptions.VPCId)
			}
		}
		if d.ClusterConfig != nil && d.ClusterConfig.InstanceType != "" {
			props["instance_type"] = string(d.ClusterConfig.InstanceType)
		}
		if d.ClusterConfig != nil && d.ClusterConfig.InstanceCount != nil {
			props["instance_count"] = aws.ToInt32(d.ClusterConfig.InstanceCount)
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformMemoryDBClusters converts MemoryDB cluster descriptions to
// osiris.aws.memorydb.cluster resources.
func TransformMemoryDBClusters(clusters []memorydbtypes.Cluster, region, accountID string) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(clusters))

	for _, c := range clusters {
		arn := aws.ToString(c.ARN)
		name := aws.ToString(c.Name)
		if name == "" {
			continue
		}
		nativeID := arn
		if nativeID == "" {
			nativeID = fmt.Sprintf("arn:aws:memorydb:%s:%s:cluster/%s", region, accountID, name)
		}
		id := resourceIDFromARN(nativeID)
		idMap[arn] = id

		prov := awsProvider(nativeID, "memorydb:cluster", region, accountID)
		r, err := sdk.NewResource(id, "osiris.aws.memorydb.cluster", prov)
		if err != nil {
			continue
		}
		r.Name = name
		r.Status = mapSimpleStatus(aws.ToString(c.Status), "available")

		props := map[string]any{}
		if c.Engine != nil {
			props["engine"] = aws.ToString(c.Engine)
		}
		if c.EngineVersion != nil {
			props["engine_version"] = aws.ToString(c.EngineVersion)
		}
		if c.NodeType != nil {
			props["node_type"] = aws.ToString(c.NodeType)
		}
		if c.NumberOfShards != nil {
			props["number_of_shards"] = aws.ToInt32(c.NumberOfShards)
		}
		if c.SubnetGroupName != nil {
			props["subnet_group_name"] = aws.ToString(c.SubnetGroupName)
		}
		if c.ClusterEndpoint != nil && c.ClusterEndpoint.Port != 0 {
			props["port"] = c.ClusterEndpoint.Port
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformMemoryDBSubnetGroups converts MemoryDB subnet group descriptions to
// osiris.aws.memorydb.subnetgroup resources.
func TransformMemoryDBSubnetGroups(groups []memorydbtypes.SubnetGroup, region, accountID string) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(groups))

	for _, g := range groups {
		arn := aws.ToString(g.ARN)
		name := aws.ToString(g.Name)
		if name == "" {
			continue
		}
		nativeID := arn
		if nativeID == "" {
			nativeID = fmt.Sprintf("arn:aws:memorydb:%s:%s:subnetgroup/%s", region, accountID, name)
		}
		id := resourceIDFromARN(nativeID)
		idMap[name] = id

		prov := awsProvider(nativeID, "memorydb:subnet-group", region, accountID)
		r, err := sdk.NewResource(id, "osiris.aws.memorydb.subnetgroup", prov)
		if err != nil {
			continue
		}
		r.Name = name
		r.Status = "active"

		subnetIDs := make([]string, 0, len(g.Subnets))
		for _, s := range g.Subnets {
			if s.Identifier != nil {
				subnetIDs = append(subnetIDs, aws.ToString(s.Identifier))
			}
		}
		props := map[string]any{}
		if g.VpcId != nil {
			props["vpc_id"] = aws.ToString(g.VpcId)
		}
		if len(subnetIDs) > 0 {
			props["subnet_ids"] = subnetIDs
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformDocDBClusterToSGConnections wires DocumentDB clusters to their security groups.
func TransformDocDBClusterToSGConnections(clusters []docdbtypes.DBCluster, clusterIDMap, sgIDMap map[string]string) []sdk.Connection {
	var conns []sdk.Connection
	for _, c := range clusters {
		arn := aws.ToString(c.DBClusterArn)
		srcID, ok := clusterIDMap[arn]
		if !ok {
			continue
		}
		clusterID := aws.ToString(c.DBClusterIdentifier)
		for _, sg := range c.VpcSecurityGroups {
			sgID := aws.ToString(sg.VpcSecurityGroupId)
			tgtID, ok := sgIDMap[sgID]
			if !ok {
				continue
			}
			conn := makeConn("network", "forward", srcID, tgtID,
				fmt.Sprintf("DocDB cluster %s -> security group %s", clusterID, sgID))
			if conn != nil {
				conns = append(conns, *conn)
			}
		}
	}
	return conns
}

// TransformDocDBClusterToSubnetGroupConnections wires DocumentDB clusters to their subnet groups.
func TransformDocDBClusterToSubnetGroupConnections(clusters []docdbtypes.DBCluster, clusterIDMap, sgroupIDMap map[string]string) []sdk.Connection {
	var conns []sdk.Connection
	for _, c := range clusters {
		arn := aws.ToString(c.DBClusterArn)
		srcID, ok := clusterIDMap[arn]
		if !ok {
			continue
		}
		sgName := aws.ToString(c.DBSubnetGroup)
		tgtID, ok := sgroupIDMap[sgName]
		if !ok {
			continue
		}
		conn := makeConn("network", "forward", srcID, tgtID,
			fmt.Sprintf("DocDB cluster %s -> subnet group %s", aws.ToString(c.DBClusterIdentifier), sgName))
		if conn != nil {
			conns = append(conns, *conn)
		}
	}
	return conns
}

// TransformDocDBSubnetGroupToSubnetConnections wires DocDB subnet groups to their subnets.
func TransformDocDBSubnetGroupToSubnetConnections(groups []docdbtypes.DBSubnetGroup, sgroupIDMap, subnetIDMap map[string]string) []sdk.Connection {
	var conns []sdk.Connection
	for _, g := range groups {
		name := aws.ToString(g.DBSubnetGroupName)
		srcID, ok := sgroupIDMap[name]
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
				fmt.Sprintf("DocDB subnet group %s -> subnet %s", name, subnetID))
			if conn != nil {
				conns = append(conns, *conn)
			}
		}
	}
	return conns
}

// TransformNeptuneClusterToSGConnections wires Neptune clusters to their security groups.
func TransformNeptuneClusterToSGConnections(clusters []neptunetypes.DBCluster, clusterIDMap, sgIDMap map[string]string) []sdk.Connection {
	var conns []sdk.Connection
	for _, c := range clusters {
		arn := aws.ToString(c.DBClusterArn)
		srcID, ok := clusterIDMap[arn]
		if !ok {
			continue
		}
		clusterID := aws.ToString(c.DBClusterIdentifier)
		for _, sg := range c.VpcSecurityGroups {
			sgID := aws.ToString(sg.VpcSecurityGroupId)
			tgtID, ok := sgIDMap[sgID]
			if !ok {
				continue
			}
			conn := makeConn("network", "forward", srcID, tgtID,
				fmt.Sprintf("Neptune cluster %s -> security group %s", clusterID, sgID))
			if conn != nil {
				conns = append(conns, *conn)
			}
		}
	}
	return conns
}

// TransformNeptuneClusterToSubnetGroupConnections wires Neptune clusters to their subnet groups.
func TransformNeptuneClusterToSubnetGroupConnections(clusters []neptunetypes.DBCluster, clusterIDMap, sgroupIDMap map[string]string) []sdk.Connection {
	var conns []sdk.Connection
	for _, c := range clusters {
		arn := aws.ToString(c.DBClusterArn)
		srcID, ok := clusterIDMap[arn]
		if !ok {
			continue
		}
		sgName := aws.ToString(c.DBSubnetGroup)
		tgtID, ok := sgroupIDMap[sgName]
		if !ok {
			continue
		}
		conn := makeConn("network", "forward", srcID, tgtID,
			fmt.Sprintf("Neptune cluster %s -> subnet group %s", aws.ToString(c.DBClusterIdentifier), sgName))
		if conn != nil {
			conns = append(conns, *conn)
		}
	}
	return conns
}

// TransformNeptuneSubnetGroupToSubnetConnections wires Neptune subnet groups to their subnets.
func TransformNeptuneSubnetGroupToSubnetConnections(groups []neptunetypes.DBSubnetGroup, sgroupIDMap, subnetIDMap map[string]string) []sdk.Connection {
	var conns []sdk.Connection
	for _, g := range groups {
		name := aws.ToString(g.DBSubnetGroupName)
		srcID, ok := sgroupIDMap[name]
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
				fmt.Sprintf("Neptune subnet group %s -> subnet %s", name, subnetID))
			if conn != nil {
				conns = append(conns, *conn)
			}
		}
	}
	return conns
}

// TransformRedshiftClusterToSGConnections wires Redshift clusters to their VPC security groups.
func TransformRedshiftClusterToSGConnections(clusters []redshifttypes.Cluster, clusterIDMap, sgIDMap map[string]string) []sdk.Connection {
	var conns []sdk.Connection
	for _, c := range clusters {
		clusterID := aws.ToString(c.ClusterIdentifier)
		srcID, ok := clusterIDMap[clusterID]
		if !ok {
			continue
		}
		for _, sg := range c.VpcSecurityGroups {
			sgID := aws.ToString(sg.VpcSecurityGroupId)
			tgtID, ok := sgIDMap[sgID]
			if !ok {
				continue
			}
			conn := makeConn("network", "forward", srcID, tgtID,
				fmt.Sprintf("Redshift cluster %s -> security group %s", clusterID, sgID))
			if conn != nil {
				conns = append(conns, *conn)
			}
		}
	}
	return conns
}

// TransformRedshiftClusterToSubnetGroupConnections wires Redshift clusters to their subnet groups.
func TransformRedshiftClusterToSubnetGroupConnections(clusters []redshifttypes.Cluster, clusterIDMap, sgroupIDMap map[string]string) []sdk.Connection {
	var conns []sdk.Connection
	for _, c := range clusters {
		clusterID := aws.ToString(c.ClusterIdentifier)
		srcID, ok := clusterIDMap[clusterID]
		if !ok {
			continue
		}
		sgName := aws.ToString(c.ClusterSubnetGroupName)
		tgtID, ok := sgroupIDMap[sgName]
		if !ok {
			continue
		}
		conn := makeConn("network", "forward", srcID, tgtID,
			fmt.Sprintf("Redshift cluster %s -> subnet group %s", clusterID, sgName))
		if conn != nil {
			conns = append(conns, *conn)
		}
	}
	return conns
}

// TransformRedshiftSubnetGroupToSubnetConnections wires Redshift subnet groups to their subnets.
func TransformRedshiftSubnetGroupToSubnetConnections(groups []redshifttypes.ClusterSubnetGroup, sgroupIDMap, subnetIDMap map[string]string) []sdk.Connection {
	var conns []sdk.Connection
	for _, g := range groups {
		name := aws.ToString(g.ClusterSubnetGroupName)
		srcID, ok := sgroupIDMap[name]
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
				fmt.Sprintf("Redshift subnet group %s -> subnet %s", name, subnetID))
			if conn != nil {
				conns = append(conns, *conn)
			}
		}
	}
	return conns
}

// TransformOpenSearchToSubnetConnections wires VPC-attached OpenSearch domains to their subnets.
func TransformOpenSearchToSubnetConnections(domains []openstypes.DomainStatus, domainIDMap, subnetIDMap map[string]string) []sdk.Connection {
	var conns []sdk.Connection
	for _, d := range domains {
		arn := aws.ToString(d.ARN)
		srcID, ok := domainIDMap[arn]
		if !ok {
			continue
		}
		if d.VPCOptions == nil {
			continue
		}
		name := aws.ToString(d.DomainName)
		for _, subnetID := range d.VPCOptions.SubnetIds {
			tgtID, ok := subnetIDMap[subnetID]
			if !ok {
				continue
			}
			conn := makeConn("network", "forward", srcID, tgtID,
				fmt.Sprintf("OpenSearch domain %s -> subnet %s", name, subnetID))
			if conn != nil {
				conns = append(conns, *conn)
			}
		}
	}
	return conns
}

// TransformOpenSearchToSGConnections wires VPC-attached OpenSearch domains to their security groups.
func TransformOpenSearchToSGConnections(domains []openstypes.DomainStatus, domainIDMap, sgIDMap map[string]string) []sdk.Connection {
	var conns []sdk.Connection
	for _, d := range domains {
		arn := aws.ToString(d.ARN)
		srcID, ok := domainIDMap[arn]
		if !ok {
			continue
		}
		if d.VPCOptions == nil {
			continue
		}
		name := aws.ToString(d.DomainName)
		for _, sgID := range d.VPCOptions.SecurityGroupIds {
			tgtID, ok := sgIDMap[sgID]
			if !ok {
				continue
			}
			conn := makeConn("network", "forward", srcID, tgtID,
				fmt.Sprintf("OpenSearch domain %s -> security group %s", name, sgID))
			if conn != nil {
				conns = append(conns, *conn)
			}
		}
	}
	return conns
}

// TransformMemoryDBClusterToSGConnections wires MemoryDB clusters to their security groups.
func TransformMemoryDBClusterToSGConnections(clusters []memorydbtypes.Cluster, clusterIDMap, sgIDMap map[string]string) []sdk.Connection {
	var conns []sdk.Connection
	for _, c := range clusters {
		arn := aws.ToString(c.ARN)
		srcID, ok := clusterIDMap[arn]
		if !ok {
			continue
		}
		name := aws.ToString(c.Name)
		for _, sg := range c.SecurityGroups {
			sgID := aws.ToString(sg.SecurityGroupId)
			tgtID, ok := sgIDMap[sgID]
			if !ok {
				continue
			}
			conn := makeConn("network", "forward", srcID, tgtID,
				fmt.Sprintf("MemoryDB cluster %s -> security group %s", name, sgID))
			if conn != nil {
				conns = append(conns, *conn)
			}
		}
	}
	return conns
}

// TransformMemoryDBClusterToSubnetGroupConnections wires MemoryDB clusters to their subnet groups.
func TransformMemoryDBClusterToSubnetGroupConnections(clusters []memorydbtypes.Cluster, clusterIDMap, sgroupIDMap map[string]string) []sdk.Connection {
	var conns []sdk.Connection
	for _, c := range clusters {
		arn := aws.ToString(c.ARN)
		srcID, ok := clusterIDMap[arn]
		if !ok {
			continue
		}
		sgName := aws.ToString(c.SubnetGroupName)
		tgtID, ok := sgroupIDMap[sgName]
		if !ok {
			continue
		}
		conn := makeConn("network", "forward", srcID, tgtID,
			fmt.Sprintf("MemoryDB cluster %s -> subnet group %s", aws.ToString(c.Name), sgName))
		if conn != nil {
			conns = append(conns, *conn)
		}
	}
	return conns
}

// TransformMemoryDBSubnetGroupToSubnetConnections wires MemoryDB subnet groups to their subnets.
func TransformMemoryDBSubnetGroupToSubnetConnections(groups []memorydbtypes.SubnetGroup, sgroupIDMap, subnetIDMap map[string]string) []sdk.Connection {
	var conns []sdk.Connection
	for _, g := range groups {
		name := aws.ToString(g.Name)
		srcID, ok := sgroupIDMap[name]
		if !ok {
			continue
		}
		for _, s := range g.Subnets {
			subnetID := aws.ToString(s.Identifier)
			tgtID, ok := subnetIDMap[subnetID]
			if !ok {
				continue
			}
			conn := makeConn("network", "forward", srcID, tgtID,
				fmt.Sprintf("MemoryDB subnet group %s -> subnet %s", name, subnetID))
			if conn != nil {
				conns = append(conns, *conn)
			}
		}
	}
	return conns
}

// mapSimpleStatus maps a raw string status to an OSIRIS JSON status,
// treating the "available" equivalent (configurable) as "active".
func mapSimpleStatus(status, activeVal string) string {
	if status == activeVal {
		return "active"
	}
	switch status {
	case "creating", "modifying", "rebooting", "starting":
		return "pending"
	case "deleting", "deleted", "stopped":
		return "inactive"
	case "failed", "incompatible-parameters", "incompatible-network":
		return "inactive"
	case "maintenance", "upgrading", "resizing":
		return "degraded"
	default:
		if status == "" {
			return "unknown"
		}
		return status
	}
}

func mapOpenSearchStatus(d openstypes.DomainStatus) string {
	if d.Deleted != nil && aws.ToBool(d.Deleted) {
		return "inactive"
	}
	if d.Created != nil && !aws.ToBool(d.Created) {
		return "pending"
	}
	switch d.DomainProcessingStatus {
	case openstypes.DomainProcessingStatusTypeActive:
		return "active"
	case openstypes.DomainProcessingStatusTypeCreating, openstypes.DomainProcessingStatusTypeUpdating:
		return "pending"
	case openstypes.DomainProcessingStatusTypeDeleting:
		return "inactive"
	case openstypes.DomainProcessingStatusTypeIsolated:
		return "degraded"
	default:
		return "active"
	}
}
