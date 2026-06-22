// transform_observability.go - observability and backup resource transforms.
// Maps CloudWatch log groups and AWS Backup vaults to OSIRIS JSON types.
//
// All types are custom osiris.aws.* namespace:
//   cloudwatchlogs:loggroup   -> osiris.aws.cloudwatch.loggroup
//   backup:vault              -> osiris.aws.backup.vault
//
// [OSIRIS-JSON-AWS]: https://osirisjson.org/en/docs/producers/hyperscalers/amazon-aws
// [OSIRIS-JSON-SPEC-CH07]: https://osirisjson.org/en/docs/spec/v10/07-resourcetypetaxonomy

package aws

import (
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	backuptypes "github.com/aws/aws-sdk-go-v2/service/backup/types"
	cwlogstypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"go.osirisjson.org/producers/pkg/sdk"
)

// TransformCloudWatchLogGroups converts CloudWatch log group descriptions to
// osiris.aws.cloudwatch.loggroup resources.
func TransformCloudWatchLogGroups(groups []cwlogstypes.LogGroup, region, accountID string) []sdk.Resource {
	var resources []sdk.Resource

	for _, g := range groups {
		name := aws.ToString(g.LogGroupName)
		if name == "" {
			continue
		}
		arn := aws.ToString(g.LogGroupArn)
		nativeID := arn
		if nativeID == "" {
			nativeID = fmt.Sprintf("arn:aws:logs:%s:%s:log-group:%s", region, accountID, name)
		}
		// CloudWatch ARNs end with :* - strip the trailing :* for a clean ID.
		if len(nativeID) > 2 && nativeID[len(nativeID)-2:] == ":*" {
			nativeID = nativeID[:len(nativeID)-2]
		}
		id := resourceIDFromARN(nativeID)

		prov := awsProvider(nativeID, "logs:log-group", region, accountID)
		r, err := sdk.NewResource(id, "osiris.aws.cloudwatch.loggroup", prov)
		if err != nil {
			continue
		}
		r.Name = name
		r.Status = "active"

		props := map[string]any{}
		if g.RetentionInDays != nil {
			props["retention_in_days"] = aws.ToInt32(g.RetentionInDays)
		}
		if g.MetricFilterCount != nil {
			props["metric_filter_count"] = aws.ToInt32(g.MetricFilterCount)
		}
		if g.StoredBytes != nil {
			props["stored_bytes"] = aws.ToInt64(g.StoredBytes)
		}
		if g.KmsKeyId != nil && aws.ToString(g.KmsKeyId) != "" {
			props["kms_key_id"] = aws.ToString(g.KmsKeyId)
		}
		if g.LogGroupClass != "" {
			props["log_group_class"] = string(g.LogGroupClass)
		}
		if len(props) > 0 {
			r.Properties = props
		}
		resources = append(resources, r)
	}
	return resources
}

// TransformBackupVaults converts AWS Backup vault list members to
// osiris.aws.backup.vault resources.
func TransformBackupVaults(vaults []backuptypes.BackupVaultListMember, region, accountID string) []sdk.Resource {
	var resources []sdk.Resource

	for _, v := range vaults {
		arn := aws.ToString(v.BackupVaultArn)
		name := aws.ToString(v.BackupVaultName)
		if name == "" {
			continue
		}
		nativeID := arn
		if nativeID == "" {
			nativeID = fmt.Sprintf("arn:aws:backup:%s:%s:backup-vault:%s", region, accountID, name)
		}
		id := resourceIDFromARN(nativeID)

		prov := awsProvider(nativeID, "backup:vault", region, accountID)
		r, err := sdk.NewResource(id, "osiris.aws.backup.vault", prov)
		if err != nil {
			continue
		}
		r.Name = name
		r.Status = mapBackupVaultState(v.VaultState)

		props := map[string]any{
			"number_of_recovery_points": v.NumberOfRecoveryPoints,
		}
		if v.EncryptionKeyArn != nil && aws.ToString(v.EncryptionKeyArn) != "" {
			props["encryption_key_arn"] = aws.ToString(v.EncryptionKeyArn)
		}
		if v.Locked != nil {
			props["locked"] = aws.ToBool(v.Locked)
		}
		if v.MinRetentionDays != nil {
			props["min_retention_days"] = aws.ToInt64(v.MinRetentionDays)
		}
		if v.MaxRetentionDays != nil {
			props["max_retention_days"] = aws.ToInt64(v.MaxRetentionDays)
		}
		if v.VaultType != "" {
			props["vault_type"] = string(v.VaultType)
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources
}


func mapBackupVaultState(s backuptypes.VaultState) string {
	switch s {
	case backuptypes.VaultStateAvailable:
		return "active"
	case backuptypes.VaultStateCreating:
		return "pending"
	case backuptypes.VaultStateFailed:
		return "inactive"
	default:
		return "unknown"
	}
}
