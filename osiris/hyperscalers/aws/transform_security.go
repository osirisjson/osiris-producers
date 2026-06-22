// transform_security.go - security and identity resource transforms.
// Maps KMS customer-managed keys, Secrets Manager secrets, ECR repositories,
// and WAFv2 web ACLs to OSIRIS JSON types.
//
// All types are custom osiris.aws.* namespace (no standard chapter-7 equivalents):
//   kms:key                          -> osiris.aws.kms.key
//   secretsmanager:secret            -> osiris.aws.secretsmanager.secret
//   ecr:repository                   -> osiris.aws.ecr.repository
//   wafv2:webacl (REGIONAL)          -> osiris.aws.waf.webacl
//   acm:certificate                  -> osiris.aws.acm.certificate
//   ram:resource-share               -> osiris.aws.ram.resourceshare
//
// [OSIRIS-JSON-AWS]: https://osirisjson.org/en/docs/producers/hyperscalers/amazon-aws
// [OSIRIS-JSON-SPEC-CH07]: https://osirisjson.org/en/docs/spec/v10/07-resourcetypetaxonomy

package aws

import (
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	acmtypes "github.com/aws/aws-sdk-go-v2/service/acm/types"
	ecrtypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
	kmstypes "github.com/aws/aws-sdk-go-v2/service/kms/types"
	ramtypes "github.com/aws/aws-sdk-go-v2/service/ram/types"
	smtypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
	"go.osirisjson.org/producers/pkg/sdk"
)

// TransformKMSKeys converts KMS customer-managed key metadata to
// osiris.aws.kms.key resources.
func TransformKMSKeys(keys []kmstypes.KeyMetadata, region, accountID string) []sdk.Resource {
	var resources []sdk.Resource

	for _, k := range keys {
		arn := aws.ToString(k.Arn)
		keyID := aws.ToString(k.KeyId)
		if keyID == "" {
			continue
		}
		nativeID := arn
		if nativeID == "" {
			nativeID = fmt.Sprintf("arn:aws:kms:%s:%s:key/%s", region, accountID, keyID)
		}
		id := resourceIDFromARN(nativeID)

		prov := awsProvider(nativeID, "kms:key", region, accountID)
		r, err := sdk.NewResource(id, "osiris.aws.kms.key", prov)
		if err != nil {
			continue
		}
		r.Name = keyID
		r.Status = mapKMSKeyState(k.KeyState)

		props := map[string]any{}
		if k.Description != nil && aws.ToString(k.Description) != "" {
			props["description"] = aws.ToString(k.Description)
		}
		if k.KeySpec != "" {
			props["key_spec"] = string(k.KeySpec)
		}
		if k.KeyUsage != "" {
			props["key_usage"] = string(k.KeyUsage)
		}
		if len(k.EncryptionAlgorithms) > 0 {
			algs := make([]string, len(k.EncryptionAlgorithms))
			for i, a := range k.EncryptionAlgorithms {
				algs[i] = string(a)
			}
			props["encryption_algorithms"] = algs
		}
		if len(props) > 0 {
			r.Properties = props
		}
		attachRawBody(&r, &k)
		resources = append(resources, r)
	}
	return resources
}

// TransformSecretsManagerSecrets converts Secrets Manager secret list entries to
// osiris.aws.secretsmanager.secret resources.
// Only name and ARN are collected, no secret values are accessed.
func TransformSecretsManagerSecrets(secrets []smtypes.SecretListEntry, region, accountID string) []sdk.Resource {
	var resources []sdk.Resource

	for _, s := range secrets {
		arn := aws.ToString(s.ARN)
		name := aws.ToString(s.Name)
		if arn == "" {
			continue
		}
		id := resourceIDFromARN(arn)

		prov := awsProvider(arn, "secretsmanager:secret", region, accountID)
		r, err := sdk.NewResource(id, "osiris.aws.secretsmanager.secret", prov)
		if err != nil {
			continue
		}
		r.Name = name
		r.Status = "active"
		if s.DeletedDate != nil {
			r.Status = "inactive"
		}

		props := map[string]any{}
		if s.Description != nil && aws.ToString(s.Description) != "" {
			props["description"] = aws.ToString(s.Description)
		}
		if s.RotationEnabled != nil {
			props["rotation_enabled"] = aws.ToBool(s.RotationEnabled)
		}
		if s.RotationLambdaARN != nil && aws.ToString(s.RotationLambdaARN) != "" {
			props["rotation_lambda_arn"] = aws.ToString(s.RotationLambdaARN)
		}
		if len(props) > 0 {
			r.Properties = props
		}

		tags := smTagMap(s.Tags)
		if len(tags) > 0 {
			r.Tags = sanitizeTags(tags)
		}
		attachRawBody(&r, &s)
		resources = append(resources, r)
	}
	return resources
}

// TransformECRRepositories converts ECR repository descriptions to
// osiris.aws.ecr.repository resources.
func TransformECRRepositories(repos []ecrtypes.Repository, region, accountID string) []sdk.Resource {
	var resources []sdk.Resource

	for _, repo := range repos {
		arn := aws.ToString(repo.RepositoryArn)
		name := aws.ToString(repo.RepositoryName)
		if arn == "" {
			continue
		}
		id := resourceIDFromARN(arn)

		prov := awsProvider(arn, "ecr:repository", region, accountID)
		r, err := sdk.NewResource(id, "osiris.aws.ecr.repository", prov)
		if err != nil {
			continue
		}
		r.Name = name
		r.Status = "active"

		props := map[string]any{}
		if repo.RepositoryUri != nil {
			props["repository_uri"] = aws.ToString(repo.RepositoryUri)
		}
		if repo.ImageTagMutability != "" {
			props["image_tag_mutability"] = string(repo.ImageTagMutability)
		}
		if repo.EncryptionConfiguration != nil && repo.EncryptionConfiguration.EncryptionType != "" {
			props["encryption_type"] = string(repo.EncryptionConfiguration.EncryptionType)
		}
		if repo.ImageScanningConfiguration != nil {
			props["scan_on_push"] = repo.ImageScanningConfiguration.ScanOnPush
		}
		if len(props) > 0 {
			r.Properties = props
		}
		resources = append(resources, r)
	}
	return resources
}

// TransformWAFv2WebACLs converts WAFv2 web ACL entries to osiris.aws.wafv2.webacl resources.
// Returns resources and an aclARN->resourceID map.
func TransformWAFv2WebACLs(entries []WAFv2WebACLEntry, region, accountID string) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(entries))

	for _, entry := range entries {
		acl := entry.WebACL
		arn := aws.ToString(acl.ARN)
		name := aws.ToString(acl.Name)
		if arn == "" {
			continue
		}
		id := resourceIDFromARN(arn)
		idMap[arn] = id

		prov := awsProvider(arn, "wafv2:webacl", region, accountID)
		r, err := sdk.NewResource(id, "osiris.aws.waf.webacl", prov)
		if err != nil {
			continue
		}
		r.Name = name
		r.Status = "active"

		props := map[string]any{
			"capacity":   acl.Capacity,
			"rule_count": len(acl.Rules),
		}
		if acl.Description != nil && aws.ToString(acl.Description) != "" {
			props["description"] = aws.ToString(acl.Description)
		}
		if len(entry.AssociatedResourceARNs) > 0 {
			props["associated_resource_arns"] = entry.AssociatedResourceARNs
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources, idMap
}

func mapKMSKeyState(s kmstypes.KeyState) string {
	switch s {
	case kmstypes.KeyStateEnabled:
		return "active"
	case kmstypes.KeyStateDisabled:
		return "inactive"
	case kmstypes.KeyStatePendingDeletion, kmstypes.KeyStatePendingImport,
		kmstypes.KeyStatePendingReplicaDeletion:
		return "pending"
	case kmstypes.KeyStateUnavailable:
		return "degraded"
	default:
		return "unknown"
	}
}

// TransformACMCertificates converts ACM certificate details to osiris.aws.acm.certificate resources.
func TransformACMCertificates(certs []acmtypes.CertificateDetail, region, accountID string) []sdk.Resource {
	var resources []sdk.Resource

	for _, c := range certs {
		arn := aws.ToString(c.CertificateArn)
		if arn == "" {
			continue
		}
		id := resourceIDFromARN(arn)

		prov := awsProvider(arn, "acm:certificate", region, accountID)
		r, err := sdk.NewResource(id, "osiris.aws.acm.certificate", prov)
		if err != nil {
			continue
		}
		r.Name = aws.ToString(c.DomainName)
		r.Status = mapACMCertStatus(c.Status)

		props := map[string]any{}
		if c.DomainName != nil {
			props["domain_name"] = aws.ToString(c.DomainName)
		}
		if len(c.SubjectAlternativeNames) > 0 {
			props["subject_alternative_names"] = c.SubjectAlternativeNames
		}
		if c.KeyAlgorithm != "" {
			props["key_algorithm"] = string(c.KeyAlgorithm)
		}
		if c.Type != "" {
			props["type"] = string(c.Type)
		}
		if c.NotAfter != nil {
			props["not_after"] = c.NotAfter.Format("2006-01-02")
		}
		if c.InUseBy != nil && len(c.InUseBy) > 0 {
			props["in_use_by"] = c.InUseBy
		}
		if len(props) > 0 {
			r.Properties = props
		}
		resources = append(resources, r)
	}
	return resources
}

func mapACMCertStatus(s acmtypes.CertificateStatus) string {
	switch s {
	case acmtypes.CertificateStatusIssued:
		return "active"
	case acmtypes.CertificateStatusExpired, acmtypes.CertificateStatusRevoked,
		acmtypes.CertificateStatusInactive:
		return "inactive"
	case acmtypes.CertificateStatusPendingValidation:
		return "pending"
	case acmtypes.CertificateStatusFailed, acmtypes.CertificateStatusValidationTimedOut:
		return "inactive"
	default:
		return "unknown"
	}
}

// TransformRAMResourceShares converts RAM resource shares to osiris.aws.ram.resourceshare resources.
// Only shares owned by this account (ResourceOwner=SELF) are collected.
func TransformRAMResourceShares(shares []ramtypes.ResourceShare, region, accountID string) []sdk.Resource {
	var resources []sdk.Resource

	for _, s := range shares {
		arn := aws.ToString(s.ResourceShareArn)
		name := aws.ToString(s.Name)
		if arn == "" {
			continue
		}
		id := resourceIDFromARN(arn)

		prov := awsProvider(arn, "ram:resource-share", region, accountID)
		r, err := sdk.NewResource(id, "osiris.aws.ram.resourceshare", prov)
		if err != nil {
			continue
		}
		r.Name = name
		r.Status = mapRAMShareStatus(s.Status)

		props := map[string]any{}
		if s.AllowExternalPrincipals != nil {
			props["allow_external_principals"] = aws.ToBool(s.AllowExternalPrincipals)
		}
		if s.FeatureSet != "" {
			props["feature_set"] = string(s.FeatureSet)
		}
		if len(props) > 0 {
			r.Properties = props
		}

		tags := ramTagMap(s.Tags)
		if len(tags) > 0 {
			r.Tags = sanitizeTags(tags)
		}

		resources = append(resources, r)
	}
	return resources
}

func mapRAMShareStatus(s ramtypes.ResourceShareStatus) string {
	switch s {
	case ramtypes.ResourceShareStatusActive:
		return "active"
	case ramtypes.ResourceShareStatusPending:
		return "pending"
	case ramtypes.ResourceShareStatusFailed, ramtypes.ResourceShareStatusDeleting,
		ramtypes.ResourceShareStatusDeleted:
		return "inactive"
	default:
		return "unknown"
	}
}

func ramTagMap(tags []ramtypes.Tag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	m := make(map[string]string, len(tags))
	for _, t := range tags {
		m[aws.ToString(t.Key)] = aws.ToString(t.Value)
	}
	return m
}

func smTagMap(tags []smtypes.Tag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	m := make(map[string]string, len(tags))
	for _, t := range tags {
		m[aws.ToString(t.Key)] = aws.ToString(t.Value)
	}
	return m
}
