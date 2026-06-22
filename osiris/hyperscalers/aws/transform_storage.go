// transform_storage.go - storage resource transforms.
// Maps EBS volumes, S3 buckets, EFS file systems, and FSx file systems to
// OSIRIS JSON types following the spec chapter 7 type taxonomy.
//
// Standard types (OSIRIS JSON spec chapter 7):
//   ec2:volume            -> storage.volume      (7.4.1)
//   s3:bucket             -> storage.bucket      (7.4.2)
//   elasticfilesystem:fs  -> storage.filesystem  (7.4.3)
//
// Custom types (osiris.aws.* namespace):
//   fsx:filesystem        -> osiris.aws.fsx.filesystem
//
// [OSIRIS-JSON-AWS]: https://osirisjson.org/en/docs/producers/hyperscalers/amazon-aws
// [OSIRIS-JSON-SPEC-CH07]: https://osirisjson.org/en/docs/spec/v10/07-resourcetypetaxonomy

package aws

import (
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	efstypes "github.com/aws/aws-sdk-go-v2/service/efs/types"
	fsxtypes "github.com/aws/aws-sdk-go-v2/service/fsx/types"
	"go.osirisjson.org/producers/pkg/sdk"
)

// TransformEBSVolumes converts EBS volumes to storage.volume resources.
// Returns resources and a volumeID->resourceID map for attachment connection wiring.
func TransformEBSVolumes(volumes []ec2types.Volume, region, accountID string) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(volumes))

	for _, v := range volumes {
		volID := aws.ToString(v.VolumeId)
		if volID == "" {
			continue
		}
		arn := fmt.Sprintf("arn:aws:ec2:%s:%s:volume/%s", region, accountID, volID)
		id := resourceIDFromARN(arn)
		idMap[volID] = id

		prov := awsProvider(arn, "ec2:volume", region, accountID)
		if v.AvailabilityZone != nil {
			prov = awsProviderWithZone(arn, "ec2:volume", region, accountID, aws.ToString(v.AvailabilityZone))
		}
		r, err := sdk.NewResource(id, "storage.volume", prov)
		if err != nil {
			continue
		}
		r.Name = volID
		r.Status = mapEBSVolumeState(v.State)

		props := map[string]any{
			"volume_type": string(v.VolumeType),
		}
		if v.Size != nil {
			props["size_gb"] = aws.ToInt32(v.Size)
		}
		if v.Encrypted != nil {
			props["encrypted"] = aws.ToBool(v.Encrypted)
		}
		if v.Iops != nil {
			props["iops"] = aws.ToInt32(v.Iops)
		}
		if v.Throughput != nil {
			props["throughput_mbps"] = aws.ToInt32(v.Throughput)
		}
		if v.AvailabilityZone != nil {
			props["availability_zone"] = aws.ToString(v.AvailabilityZone)
		}
		if v.MultiAttachEnabled != nil {
			props["multi_attach_enabled"] = aws.ToBool(v.MultiAttachEnabled)
		}
		if v.SnapshotId != nil && aws.ToString(v.SnapshotId) != "" {
			props["source_snapshot_id"] = aws.ToString(v.SnapshotId)
		}

		// Attachment summary.
		if len(v.Attachments) > 0 {
			var attachments []map[string]any
			for _, att := range v.Attachments {
				a := map[string]any{}
				if att.InstanceId != nil {
					a["instance_id"] = aws.ToString(att.InstanceId)
				}
				if att.Device != nil {
					a["device"] = aws.ToString(att.Device)
				}
				if att.DeleteOnTermination != nil {
					a["delete_on_termination"] = aws.ToBool(att.DeleteOnTermination)
				}
				attachments = append(attachments, a)
			}
			props["attachments"] = attachments
		}

		tags := ec2TagMap(v.Tags)
		if len(tags) > 0 {
			if name := tags["Name"]; name != "" {
				r.Name = name
			}
			r.Tags = sanitizeTags(tags)
		}

		r.Properties = props
		attachRawBody(&r, &v)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformS3Buckets converts S3 bucket info to storage.bucket resources.
// Returns resources (no ID map needed - S3 has no VPC connections).
func TransformS3Buckets(buckets []S3BucketInfo, region, accountID string) []sdk.Resource {
	var resources []sdk.Resource

	for _, b := range buckets {
		if b.Name == "" {
			continue
		}
		arn := fmt.Sprintf("arn:aws:s3:::%s", b.Name)
		id := resourceIDFromARN(arn)

		prov := awsProvider(arn, "s3:bucket", b.Region, accountID)
		r, err := sdk.NewResource(id, "storage.bucket", prov)
		if err != nil {
			continue
		}
		r.Name = b.Name
		r.Status = "active"

		// versioning: emit canonical status; "Disabled" when never enabled.
		versioningStatus := b.Versioning
		if versioningStatus == "" {
			versioningStatus = "Disabled"
		}
		props := map[string]any{
			"block_public_access": b.BlockPublicAccess,
			"versioning":          versioningStatus,
		}
		if b.Region != "" {
			props["region"] = b.Region
		}
		if b.EncryptionAlgorithm != "" {
			props["encryption_algorithm"] = b.EncryptionAlgorithm
		}
		if b.EncryptionKeyARN != "" {
			props["encryption_key_arn"] = b.EncryptionKeyARN
		}

		r.Properties = props
		if len(b.Tags) > 0 {
			r.Tags = sanitizeTags(b.Tags)
		}
		attachRawBody(&r, &b)
		resources = append(resources, r)
	}
	return resources
}

// TransformEFSFileSystems converts EFS file systems to storage.filesystem resources.
// Returns resources and a fileSystemID->resourceID map for mount target connection wiring.
func TransformEFSFileSystems(entries []EFSFileSystemEntry, region, accountID string) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(entries))

	for _, entry := range entries {
		fs := entry.FileSystem
		arn := aws.ToString(fs.FileSystemArn)
		fsID := aws.ToString(fs.FileSystemId)
		if fsID == "" {
			continue
		}
		nativeID := arn
		if nativeID == "" {
			nativeID = fmt.Sprintf("arn:aws:elasticfilesystem:%s:%s:file-system/%s", region, accountID, fsID)
		}
		id := resourceIDFromARN(nativeID)
		idMap[fsID] = id

		name := aws.ToString(fs.Name)
		if name == "" {
			name = fsID
		}
		prov := awsProvider(nativeID, "efs:filesystem", region, accountID)
		r, err := sdk.NewResource(id, "storage.filesystem", prov)
		if err != nil {
			continue
		}
		r.Name = name
		r.Status = mapEFSLifecycleState(fs.LifeCycleState)

		props := map[string]any{
			"performance_mode":      string(fs.PerformanceMode),
			"number_of_mount_targets": fs.NumberOfMountTargets,
		}
		if fs.Encrypted != nil {
			props["encrypted"] = aws.ToBool(fs.Encrypted)
		}
		if fs.ThroughputMode != "" {
			props["throughput_mode"] = string(fs.ThroughputMode)
		}
		if fs.ProvisionedThroughputInMibps != nil {
			props["provisioned_throughput_mibps"] = aws.ToFloat64(fs.ProvisionedThroughputInMibps)
		}
		if fs.AvailabilityZoneName != nil {
			props["availability_zone"] = aws.ToString(fs.AvailabilityZoneName)
		}
		if fs.SizeInBytes != nil {
			props["size_bytes"] = fs.SizeInBytes.Value
		}

		tags := efsTagMap(fs.Tags)
		if len(tags) > 0 {
			r.Tags = sanitizeTags(tags)
		}

		r.Properties = props
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformFSxFileSystems converts FSx file systems to osiris.aws.fsx.filesystem resources.
// Returns resources and a fileSystemID->resourceID map for subnet connection wiring.
func TransformFSxFileSystems(fss []fsxtypes.FileSystem, region, accountID string) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(fss))

	for _, fs := range fss {
		arn := aws.ToString(fs.ResourceARN)
		fsID := aws.ToString(fs.FileSystemId)
		if fsID == "" {
			continue
		}
		nativeID := arn
		if nativeID == "" {
			nativeID = fmt.Sprintf("arn:aws:fsx:%s:%s:file-system/%s", region, accountID, fsID)
		}
		id := resourceIDFromARN(nativeID)
		idMap[fsID] = id

		prov := awsProvider(nativeID, "fsx:filesystem", region, accountID)
		r, err := sdk.NewResource(id, "osiris.aws.fsx.filesystem", prov)
		if err != nil {
			continue
		}
		r.Name = fsID
		r.Status = mapFSxLifecycle(fs.Lifecycle)

		props := map[string]any{
			"filesystem_type": string(fs.FileSystemType),
		}
		if fs.StorageCapacity != nil {
			props["storage_capacity_gb"] = aws.ToInt32(fs.StorageCapacity)
		}
		if fs.StorageType != "" {
			props["storage_type"] = string(fs.StorageType)
		}
		if fs.VpcId != nil {
			props["vpc_id"] = aws.ToString(fs.VpcId)
		}
		if len(fs.SubnetIds) > 0 {
			props["subnet_ids"] = fs.SubnetIds
		}
		if fs.DNSName != nil {
			props["dns_name"] = aws.ToString(fs.DNSName)
		}
		if fs.FileSystemTypeVersion != nil {
			props["filesystem_type_version"] = aws.ToString(fs.FileSystemTypeVersion)
		}

		tags := fsxTagMap(fs.Tags)
		if len(tags) > 0 {
			if name := tags["Name"]; name != "" {
				r.Name = name
			}
			r.Tags = sanitizeTags(tags)
		}

		r.Properties = props
		resources = append(resources, r)
	}
	return resources, idMap
}


// TransformEBSVolumeToInstanceConnections wires attached EBS volumes to their EC2 instances.
// Direction is forward: instance contains volume.
func TransformEBSVolumeToInstanceConnections(volumes []ec2types.Volume, volumeIDMap map[string]string, instances []ec2types.Instance, region, accountID string) []sdk.Connection {
	// Build instance native ID -> resource ID map on the fly.
	instMap := make(map[string]string, len(instances))
	for _, inst := range instances {
		nativeID := aws.ToString(inst.InstanceId)
		if nativeID == "" {
			continue
		}
		instARN := fmt.Sprintf("arn:aws:ec2:%s:%s:instance/%s", region, accountID, nativeID)
		instMap[nativeID] = resourceIDFromARN(instARN)
	}

	var conns []sdk.Connection
	for _, v := range volumes {
		volID := aws.ToString(v.VolumeId)
		tgtID, ok := volumeIDMap[volID]
		if !ok {
			continue
		}
		for _, att := range v.Attachments {
			instNativeID := aws.ToString(att.InstanceId)
			srcID, ok := instMap[instNativeID]
			if !ok {
				continue
			}
			conn := makeConn("contains", "forward", srcID, tgtID,
				fmt.Sprintf("instance %s contains volume %s", instNativeID, volID))
			if conn != nil {
				conns = append(conns, *conn)
			}
		}
	}
	return conns
}

// TransformEFSToSubnetConnections wires EFS file systems to their mount target subnets.
func TransformEFSToSubnetConnections(entries []EFSFileSystemEntry, efsIDMap, subnetIDMap map[string]string) []sdk.Connection {
	var conns []sdk.Connection
	for _, entry := range entries {
		fsID := aws.ToString(entry.FileSystem.FileSystemId)
		srcID, ok := efsIDMap[fsID]
		if !ok {
			continue
		}
		for _, mt := range entry.MountTargets {
			subnetID := aws.ToString(mt.SubnetId)
			tgtID, ok := subnetIDMap[subnetID]
			if !ok {
				continue
			}
			conn := makeConn("network", "forward", srcID, tgtID,
				fmt.Sprintf("EFS %s -> subnet %s", fsID, subnetID))
			if conn != nil {
				conns = append(conns, *conn)
			}
		}
	}
	return conns
}

// TransformFSxToSubnetConnections wires FSx file systems to their subnets.
func TransformFSxToSubnetConnections(fss []fsxtypes.FileSystem, fsxIDMap, subnetIDMap map[string]string) []sdk.Connection {
	var conns []sdk.Connection
	for _, fs := range fss {
		fsID := aws.ToString(fs.FileSystemId)
		srcID, ok := fsxIDMap[fsID]
		if !ok {
			continue
		}
		for _, subnetID := range fs.SubnetIds {
			tgtID, ok := subnetIDMap[subnetID]
			if !ok {
				continue
			}
			conn := makeConn("network", "forward", srcID, tgtID,
				fmt.Sprintf("FSx %s -> subnet %s", fsID, subnetID))
			if conn != nil {
				conns = append(conns, *conn)
			}
		}
	}
	return conns
}


func mapEBSVolumeState(s ec2types.VolumeState) string {
	switch s {
	case ec2types.VolumeStateAvailable, ec2types.VolumeStateInUse:
		return "active"
	case ec2types.VolumeStateCreating:
		return "pending"
	case ec2types.VolumeStateDeleting, ec2types.VolumeStateDeleted:
		return "pending"
	case ec2types.VolumeStateError:
		return "inactive"
	default:
		return "unknown"
	}
}

func mapEFSLifecycleState(s efstypes.LifeCycleState) string {
	switch s {
	case efstypes.LifeCycleStateAvailable:
		return "active"
	case efstypes.LifeCycleStateDeleted:
		return "inactive"
	case efstypes.LifeCycleStateCreating, efstypes.LifeCycleStateDeleting:
		return "pending"
	case efstypes.LifeCycleStateUpdating:
		return "degraded"
	case efstypes.LifeCycleStateError:
		return "inactive"
	default:
		return "unknown"
	}
}

func mapFSxLifecycle(s fsxtypes.FileSystemLifecycle) string {
	switch s {
	case fsxtypes.FileSystemLifecycleAvailable:
		return "active"
	case fsxtypes.FileSystemLifecycleCreating, fsxtypes.FileSystemLifecycleDeleting:
		return "pending"
	case fsxtypes.FileSystemLifecycleFailed:
		return "inactive"
	case fsxtypes.FileSystemLifecycleMisconfigured, fsxtypes.FileSystemLifecycleMisconfiguredUnavailable,
		fsxtypes.FileSystemLifecycleUpdating:
		return "degraded"
	default:
		return "unknown"
	}
}


func ec2TagMap(tags []ec2types.Tag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	m := make(map[string]string, len(tags))
	for _, t := range tags {
		m[aws.ToString(t.Key)] = aws.ToString(t.Value)
	}
	return m
}

func efsTagMap(tags []efstypes.Tag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	m := make(map[string]string, len(tags))
	for _, t := range tags {
		m[aws.ToString(t.Key)] = aws.ToString(t.Value)
	}
	return m
}

func fsxTagMap(tags []fsxtypes.Tag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	m := make(map[string]string, len(tags))
	for _, t := range tags {
		m[aws.ToString(t.Key)] = aws.ToString(t.Value)
	}
	return m
}
