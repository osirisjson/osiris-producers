// transform_serverless.go - serverless and messaging resource transforms.
// Maps Lambda functions, SQS queues, Kinesis streams, and MSK clusters to OSIRIS JSON
// types following the spec chapter 7 type taxonomy.
//
// Standard types (OSIRIS JSON spec chapter 7):
//   lambda:function              -> compute.function.serverless (7.3.5)
//   sqs:queue                    -> application.queue           (7.2.2)
//   kinesis:stream               -> application.eventstream     (7.2.2)
//
// Custom types (osiris.aws.* namespace):
//   kafka:cluster (MSK)          -> osiris.aws.msk.cluster
//
// [OSIRIS-JSON-AWS]: https://osirisjson.org/en/docs/producers/hyperscalers/amazon-aws
// [OSIRIS-JSON-SPEC-CH07]: https://osirisjson.org/en/docs/spec/v10/07-resourcetypetaxonomy

package aws

import (
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	kafkatypes "github.com/aws/aws-sdk-go-v2/service/kafka/types"
	kinesistypes "github.com/aws/aws-sdk-go-v2/service/kinesis/types"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"go.osirisjson.org/producers/pkg/sdk"
)

// TransformLambdaFunctions converts Lambda function configurations to
// compute.function.serverless resources.
// Returns resources and a functionARN->resourceID map.
func TransformLambdaFunctions(fns []lambdatypes.FunctionConfiguration, region, accountID string) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(fns))

	for _, fn := range fns {
		arn := aws.ToString(fn.FunctionArn)
		if arn == "" {
			continue
		}
		id := resourceIDFromARN(arn)
		idMap[arn] = id

		name := aws.ToString(fn.FunctionName)
		prov := awsProvider(arn, "lambda:function", region, accountID)
		r, err := sdk.NewResource(id, "compute.function.serverless", prov)
		if err != nil {
			continue
		}
		if name != "" {
			r.Name = name
		}
		r.Status = mapLambdaState(fn.State)

		props := map[string]any{}
		if fn.Runtime != "" {
			props["runtime"] = string(fn.Runtime)
		}
		if fn.Handler != nil {
			props["handler"] = aws.ToString(fn.Handler)
		}
		if fn.MemorySize != nil {
			props["memory_mb"] = aws.ToInt32(fn.MemorySize)
		}
		if fn.Timeout != nil {
			props["timeout_seconds"] = aws.ToInt32(fn.Timeout)
		}
		if fn.PackageType != "" {
			props["package_type"] = string(fn.PackageType)
		}
		if len(fn.Architectures) > 0 {
			archs := make([]string, len(fn.Architectures))
			for i, a := range fn.Architectures {
				archs[i] = string(a)
			}
			props["architectures"] = archs
		}
		if fn.Role != nil {
			props["role_arn"] = aws.ToString(fn.Role)
		}
		if fn.EphemeralStorage != nil && fn.EphemeralStorage.Size != nil {
			props["ephemeral_storage_mb"] = aws.ToInt32(fn.EphemeralStorage.Size)
		}
		if fn.VpcConfig != nil {
			if len(fn.VpcConfig.SubnetIds) > 0 {
				props["subnet_ids"] = fn.VpcConfig.SubnetIds
			}
			if len(fn.VpcConfig.SecurityGroupIds) > 0 {
				props["security_group_ids"] = fn.VpcConfig.SecurityGroupIds
			}
			if fn.VpcConfig.VpcId != nil {
				props["vpc_id"] = aws.ToString(fn.VpcConfig.VpcId)
			}
		}
		if fn.Description != nil && aws.ToString(fn.Description) != "" {
			props["description"] = aws.ToString(fn.Description)
		}

		r.Properties = props
		attachRawBody(&r, &fn)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformSQSQueues converts SQS queue info to application.queue resources.
// Returns resources and a queueARN->resourceID map.
func TransformSQSQueues(queues []SQSQueueInfo, region, accountID string) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(queues))

	for _, q := range queues {
		arn := q.Attributes["QueueArn"]
		if arn == "" {
			continue
		}
		id := resourceIDFromARN(arn)
		idMap[arn] = id

		// Queue name is the last segment of the ARN.
		parts := strings.Split(arn, ":")
		name := parts[len(parts)-1]

		prov := awsProvider(arn, "sqs:queue", region, accountID)
		r, err := sdk.NewResource(id, "application.queue", prov)
		if err != nil {
			continue
		}
		r.Name = name
		r.Status = "active"

		props := map[string]any{}
		if q.Attributes["FifoQueue"] == "true" {
			props["queue_type"] = "fifo"
		} else {
			props["queue_type"] = "standard"
		}
		if v := q.Attributes["VisibilityTimeout"]; v != "" {
			props["visibility_timeout_seconds"] = v
		}
		if v := q.Attributes["MessageRetentionPeriod"]; v != "" {
			props["message_retention_seconds"] = v
		}
		if v := q.Attributes["ApproximateNumberOfMessages"]; v != "" {
			props["approximate_message_count"] = v
		}
		if v := q.Attributes["ContentBasedDeduplication"]; v == "true" {
			props["content_based_deduplication"] = true
		}
		if v := q.Attributes["deadLetterTargetArn"]; v != "" {
			props["dead_letter_target_arn"] = v
		}

		r.Properties = props
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformKinesisStreams converts Kinesis stream description summaries to
// application.eventstream resources.
// Returns resources and a streamARN->resourceID map.
func TransformKinesisStreams(streams []kinesistypes.StreamDescriptionSummary, region, accountID string) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(streams))

	for _, s := range streams {
		arn := aws.ToString(s.StreamARN)
		if arn == "" {
			continue
		}
		id := resourceIDFromARN(arn)
		idMap[arn] = id

		name := aws.ToString(s.StreamName)
		prov := awsProvider(arn, "kinesis:stream", region, accountID)
		r, err := sdk.NewResource(id, "application.eventstream", prov)
		if err != nil {
			continue
		}
		if name != "" {
			r.Name = name
		}
		r.Status = mapKinesisStreamStatus(s.StreamStatus)

		props := map[string]any{}
		if s.OpenShardCount != nil {
			props["open_shard_count"] = aws.ToInt32(s.OpenShardCount)
		}
		if s.RetentionPeriodHours != nil {
			props["retention_period_hours"] = aws.ToInt32(s.RetentionPeriodHours)
		}
		if s.EncryptionType != "" {
			props["encryption_type"] = string(s.EncryptionType)
		}
		if s.ConsumerCount != nil {
			props["consumer_count"] = aws.ToInt32(s.ConsumerCount)
		}

		r.Properties = props
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformMSKClusters converts MSK (managed Kafka) cluster info to
// osiris.aws.msk.cluster resources.
// Returns resources and a clusterARN->resourceID map.
func TransformMSKClusters(clusters []kafkatypes.ClusterInfo, region, accountID string) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(clusters))

	for _, c := range clusters {
		arn := aws.ToString(c.ClusterArn)
		if arn == "" {
			continue
		}
		id := resourceIDFromARN(arn)
		idMap[arn] = id

		name := aws.ToString(c.ClusterName)
		prov := awsProvider(arn, "msk:cluster", region, accountID)
		r, err := sdk.NewResource(id, "osiris.aws.msk.cluster", prov)
		if err != nil {
			continue
		}
		if name != "" {
			r.Name = name
		}
		r.Status = mapMSKClusterState(c.State)

		props := map[string]any{}
		if c.NumberOfBrokerNodes != nil {
			props["broker_node_count"] = aws.ToInt32(c.NumberOfBrokerNodes)
		}
		if c.CurrentBrokerSoftwareInfo != nil && c.CurrentBrokerSoftwareInfo.KafkaVersion != nil {
			props["kafka_version"] = aws.ToString(c.CurrentBrokerSoftwareInfo.KafkaVersion)
		}
		if c.EnhancedMonitoring != "" {
			props["enhanced_monitoring"] = string(c.EnhancedMonitoring)
		}
		if c.BrokerNodeGroupInfo != nil {
			if c.BrokerNodeGroupInfo.InstanceType != nil {
				props["broker_instance_type"] = aws.ToString(c.BrokerNodeGroupInfo.InstanceType)
			}
			if len(c.BrokerNodeGroupInfo.ClientSubnets) > 0 {
				props["subnet_ids"] = c.BrokerNodeGroupInfo.ClientSubnets
			}
			if len(c.BrokerNodeGroupInfo.SecurityGroups) > 0 {
				props["security_group_ids"] = c.BrokerNodeGroupInfo.SecurityGroups
			}
		}
		if c.EncryptionInfo != nil && c.EncryptionInfo.EncryptionInTransit != nil {
			props["encryption_in_transit"] = string(c.EncryptionInfo.EncryptionInTransit.ClientBroker)
		}
		if len(c.Tags) > 0 {
			r.Tags = sanitizeTags(c.Tags)
		}

		r.Properties = props
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformLambdaToSubnetConnections wires VPC-attached Lambda functions to their subnets.
func TransformLambdaToSubnetConnections(fns []lambdatypes.FunctionConfiguration, lambdaIDMap, subnetIDMap map[string]string) []sdk.Connection {
	var conns []sdk.Connection
	for _, fn := range fns {
		if fn.VpcConfig == nil || len(fn.VpcConfig.SubnetIds) == 0 {
			continue
		}
		arn := aws.ToString(fn.FunctionArn)
		srcID, ok := lambdaIDMap[arn]
		if !ok {
			continue
		}
		name := aws.ToString(fn.FunctionName)
		for _, subnetID := range fn.VpcConfig.SubnetIds {
			tgtID, ok := subnetIDMap[subnetID]
			if !ok {
				continue
			}
			conn := makeConn("network", "forward", srcID, tgtID,
				fmt.Sprintf("Lambda %s -> subnet %s", name, subnetID))
			if conn != nil {
				conns = append(conns, *conn)
			}
		}
	}
	return conns
}

// TransformLambdaToSGConnections wires VPC-attached Lambda functions to their security groups.
func TransformLambdaToSGConnections(fns []lambdatypes.FunctionConfiguration, lambdaIDMap, sgIDMap map[string]string) []sdk.Connection {
	var conns []sdk.Connection
	for _, fn := range fns {
		if fn.VpcConfig == nil || len(fn.VpcConfig.SecurityGroupIds) == 0 {
			continue
		}
		arn := aws.ToString(fn.FunctionArn)
		srcID, ok := lambdaIDMap[arn]
		if !ok {
			continue
		}
		name := aws.ToString(fn.FunctionName)
		for _, sgID := range fn.VpcConfig.SecurityGroupIds {
			tgtID, ok := sgIDMap[sgID]
			if !ok {
				continue
			}
			conn := makeConn("network", "forward", srcID, tgtID,
				fmt.Sprintf("Lambda %s -> security group %s", name, sgID))
			if conn != nil {
				conns = append(conns, *conn)
			}
		}
	}
	return conns
}

// TransformMSKToSubnetConnections wires MSK clusters to their broker subnets.
func TransformMSKToSubnetConnections(clusters []kafkatypes.ClusterInfo, mskIDMap, subnetIDMap map[string]string) []sdk.Connection {
	var conns []sdk.Connection
	for _, c := range clusters {
		arn := aws.ToString(c.ClusterArn)
		srcID, ok := mskIDMap[arn]
		if !ok {
			continue
		}
		if c.BrokerNodeGroupInfo == nil {
			continue
		}
		name := aws.ToString(c.ClusterName)
		for _, subnetID := range c.BrokerNodeGroupInfo.ClientSubnets {
			tgtID, ok := subnetIDMap[subnetID]
			if !ok {
				continue
			}
			conn := makeConn("network", "forward", srcID, tgtID,
				fmt.Sprintf("MSK cluster %s -> subnet %s", name, subnetID))
			if conn != nil {
				conns = append(conns, *conn)
			}
		}
	}
	return conns
}

// TransformMSKToSGConnections wires MSK clusters to their security groups.
func TransformMSKToSGConnections(clusters []kafkatypes.ClusterInfo, mskIDMap, sgIDMap map[string]string) []sdk.Connection {
	var conns []sdk.Connection
	for _, c := range clusters {
		arn := aws.ToString(c.ClusterArn)
		srcID, ok := mskIDMap[arn]
		if !ok {
			continue
		}
		if c.BrokerNodeGroupInfo == nil {
			continue
		}
		name := aws.ToString(c.ClusterName)
		for _, sgID := range c.BrokerNodeGroupInfo.SecurityGroups {
			tgtID, ok := sgIDMap[sgID]
			if !ok {
				continue
			}
			conn := makeConn("network", "forward", srcID, tgtID,
				fmt.Sprintf("MSK cluster %s -> security group %s", name, sgID))
			if conn != nil {
				conns = append(conns, *conn)
			}
		}
	}
	return conns
}

func mapLambdaState(s lambdatypes.State) string {
	switch s {
	case lambdatypes.StateActive, "":
		// AWS list-functions omits the State field for actively running functions;
		// treat an unset state as active (same as the AWS console behaviour).
		return "active"
	case lambdatypes.StateInactive:
		return "inactive"
	case lambdatypes.StatePending:
		return "pending"
	case lambdatypes.StateFailed:
		return "inactive" // schema enum: {active,inactive,degraded,retired,unknown}
	default:
		return "unknown"
	}
}

func mapKinesisStreamStatus(s kinesistypes.StreamStatus) string {
	switch s {
	case kinesistypes.StreamStatusActive:
		return "active"
	case kinesistypes.StreamStatusCreating:
		return "pending"
	case kinesistypes.StreamStatusDeleting:
		return "pending"
	case kinesistypes.StreamStatusUpdating:
		return "degraded"
	default:
		return "unknown"
	}
}

func mapMSKClusterState(s kafkatypes.ClusterState) string {
	switch s {
	case kafkatypes.ClusterStateActive:
		return "active"
	case kafkatypes.ClusterStateCreating, kafkatypes.ClusterStateDeleting:
		return "pending"
	case kafkatypes.ClusterStateFailed:
		return "inactive"
	case kafkatypes.ClusterStateHealing, kafkatypes.ClusterStateMaintenance,
		kafkatypes.ClusterStateRebootingBroker, kafkatypes.ClusterStateUpdating:
		return "degraded"
	default:
		return "unknown"
	}
}
