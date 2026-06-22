// transform_iam.go - IAM resource transforms.
// Maps IAM roles, instance profiles, OIDC providers and SAML providers to
// OSIRIS JSON types. IAM is a global service; these resources are collected
// only for GlobalRegion (us-east-1) and merged into that document.
//
// All types are custom osiris.aws.* namespace:
//   iam:role                -> osiris.aws.iam.role
//   iam:instance-profile    -> osiris.aws.iam.instanceprofile
//   iam:oidc-provider       -> osiris.aws.iam.oidcprovider
//   iam:saml-provider       -> osiris.aws.iam.samlprovider
//
// [OSIRIS-JSON-AWS]: https://osirisjson.org/en/docs/producers/hyperscalers/amazon-aws
// [OSIRIS-JSON-SPEC-CH07]: https://osirisjson.org/en/docs/spec/v10/07-resourcetypetaxonomy

package aws

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"go.osirisjson.org/producers/pkg/sdk"
)

// TransformIAMRoles converts IAM roles to osiris.aws.iam.role resources.
// Returns resources and an ARN->resourceID map for connection wiring.
func TransformIAMRoles(roles []iamtypes.Role, accountID string) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(roles))

	for _, role := range roles {
		arn := aws.ToString(role.Arn)
		name := aws.ToString(role.RoleName)
		if arn == "" {
			continue
		}
		id := resourceIDFromARN(arn)
		idMap[arn] = id

		prov := awsProvider(arn, "iam:role", "", accountID)
		r, err := sdk.NewResource(id, "osiris.aws.iam.role", prov)
		if err != nil {
			continue
		}
		r.Name = name
		r.Status = "active"

		props := map[string]any{}
		if role.Path != nil && aws.ToString(role.Path) != "/" {
			props["path"] = aws.ToString(role.Path)
		}
		if role.Description != nil && aws.ToString(role.Description) != "" {
			props["description"] = aws.ToString(role.Description)
		}
		if role.MaxSessionDuration != nil {
			props["max_session_duration"] = aws.ToInt32(role.MaxSessionDuration)
		}
		if len(props) > 0 {
			r.Properties = props
		}

		if role.Tags != nil {
			tags := iamTagMap(role.Tags)
			if len(tags) > 0 {
				r.Tags = sanitizeTags(tags)
			}
		}
		attachRawBody(&r, &role)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformIAMInstanceProfiles converts IAM instance profiles to osiris.aws.iam.instanceprofile resources.
// Returns resources and a profileARN->resourceID map.
func TransformIAMInstanceProfiles(profiles []iamtypes.InstanceProfile, accountID string) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(profiles))

	for _, p := range profiles {
		arn := aws.ToString(p.Arn)
		name := aws.ToString(p.InstanceProfileName)
		if arn == "" {
			continue
		}
		id := resourceIDFromARN(arn)
		idMap[arn] = id

		prov := awsProvider(arn, "iam:instance-profile", "", accountID)
		r, err := sdk.NewResource(id, "osiris.aws.iam.instanceprofile", prov)
		if err != nil {
			continue
		}
		r.Name = name
		r.Status = "active"

		if p.Path != nil && aws.ToString(p.Path) != "/" {
			r.Properties = map[string]any{
				"path": aws.ToString(p.Path),
			}
		}

		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformIAMOIDCProviders converts IAM OIDC providers to osiris.aws.iam.oidcprovider resources.
func TransformIAMOIDCProviders(entries []iamtypes.OpenIDConnectProviderListEntry, accountID string) []sdk.Resource {
	var resources []sdk.Resource

	for _, e := range entries {
		arn := aws.ToString(e.Arn)
		if arn == "" {
			continue
		}
		id := resourceIDFromARN(arn)

		prov := awsProvider(arn, "iam:oidc-provider", "", accountID)
		r, err := sdk.NewResource(id, "osiris.aws.iam.oidcprovider", prov)
		if err != nil {
			continue
		}
		r.Name = arn
		r.Status = "active"
		resources = append(resources, r)
	}
	return resources
}

// TransformIAMSAMLProviders converts IAM SAML providers to osiris.aws.iam.samlprovider resources.
func TransformIAMSAMLProviders(entries []iamtypes.SAMLProviderListEntry, accountID string) []sdk.Resource {
	var resources []sdk.Resource

	for _, e := range entries {
		arn := aws.ToString(e.Arn)
		if arn == "" {
			continue
		}
		id := resourceIDFromARN(arn)

		prov := awsProvider(arn, "iam:saml-provider", "", accountID)
		r, err := sdk.NewResource(id, "osiris.aws.iam.samlprovider", prov)
		if err != nil {
			continue
		}
		r.Name = arn
		r.Status = "active"
		resources = append(resources, r)
	}
	return resources
}

// TransformIAMInstanceProfileToRoleConnections wires instance profiles to their IAM roles.
func TransformIAMInstanceProfileToRoleConnections(profiles []iamtypes.InstanceProfile, profileIDMap, roleIDMap map[string]string) []sdk.Connection {
	var conns []sdk.Connection
	for _, p := range profiles {
		profileARN := aws.ToString(p.Arn)
		srcID, ok := profileIDMap[profileARN]
		if !ok {
			continue
		}
		for _, role := range p.Roles {
			roleARN := aws.ToString(role.Arn)
			tgtID, ok := roleIDMap[roleARN]
			if !ok {
				continue
			}
			conn := makeConn("contains", "forward", srcID, tgtID,
				"IAM instance profile -> role "+aws.ToString(role.RoleName))
			if conn != nil {
				conns = append(conns, *conn)
			}
		}
	}
	return conns
}


func iamTagMap(tags []iamtypes.Tag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	m := make(map[string]string, len(tags))
	for _, t := range tags {
		m[aws.ToString(t.Key)] = aws.ToString(t.Value)
	}
	return m
}
