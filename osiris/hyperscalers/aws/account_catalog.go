// account_catalog.go - AWS organization and account catalog for the
// OSIRIS JSON Amazon AWS producer.
//
// The catalog is emitted in metadata["osiris.aws"], before coverage. It
// is a producer extension the schema permits on metadata (it is not an
// OSIRIS JSON v1.0 field):
//
//	"osiris.aws": {
//	  "organizations": {
//	    "<organization id>": {
//	      "management_account_id": "<account id>"
//	    }
//	  },
//	  "accounts": {
//	    "<account id>": {
//	      "coverage":        "exported" | "reference_only",
//	      "name":            "<account name, when known>",
//	      "organization_id": "<organization, when known>"
//	    }
//	  }
//	}
//
// organizations and accounts are sibling objects keyed by AWS IDs (a
// key is never repeated inside its own record).
//
// "exported" marks the account this document inventories (the one in
// metadata.scope.accounts). "reference_only" marks an account that only
// appears in references of the document (an owner, a stub, an ARN) or
// as the management account; its resources were not inventoried.
//
// The organization comes from Organizations DescribeOrganization and
// the exported account name from Account Management
// GetAccountInformation. Names and membership of other accounts come
// from Organizations ListAccounts, which AWS allows only from the
// management account or a delegated administrator; a member account is
// normally denied, which is not a coverage error.
// AWS organizations have no name and the account emails are never read
// into the catalog. Anything unknown is omitted: an ID, profile name or
// CSV label is never used as a name and the caller's organization is
// never assumed for another account. Both reads are optional; a failure
// is recorded in metadata.coverage.errors.
//
// The exported account name is also metadata.scope.name (see
// [scopeName]).
//
// For an introduction to OSIRIS JSON Producer for Amazon AWS see:
// [OSIRIS-JSON-AWS]: https://docs.osirisjson.org/osiris-producers/hyperscalers/amazon-aws/

package aws

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/account"
	"github.com/aws/aws-sdk-go-v2/service/organizations"
	orgtypes "github.com/aws/aws-sdk-go-v2/service/organizations/types"

	"go.osirisjson.org/producers/pkg/sdk"
)

// Account catalog coverage values.
const (
	coverageExported      = "exported"
	coverageReferenceOnly = "reference_only"
)

// catalogTimeout bounds each optional catalog read, so a region or
// partition without the endpoint cannot stall collection.
const catalogTimeout = 15 * time.Second

var (
	organizationIDPattern = regexp.MustCompile(`^o-[a-z0-9]{10,32}$`)
	// arnAccountRe matches the account component of an ARN.
	arnAccountRe = regexp.MustCompile(`arn:[a-z0-9-]+:[a-z0-9-]+:[a-z0-9-]*:([0-9]{12}):`)
)

// accountReferenceKeys are the property names whose value is an AWS
// account ID.
var accountReferenceKeys = map[string]bool{
	"accepter_account":  true,
	"owner_account":     true,
	"owner_account_id":  true,
	"owner_id":          true,
	"requester_account": true,
	"resource_owner_id": true,
}

// OrganizationInfo is the AWS organization of the verified caller. The
// zero value means the organization is unknown or does not exist.
type OrganizationInfo struct {
	ID                  string
	ManagementAccountID string
	// AccountNames holds the name of every member account AWS listed.
	AccountNames map[string]string
}

// awsMetadata is the metadata["osiris.aws"] value (field order is the
// emitted order).
type awsMetadata struct {
	Organizations map[string]catalogOrganization `json:"organizations,omitempty"`
	Accounts      map[string]catalogAccount      `json:"accounts,omitempty"`
	Coverage      map[string]any                 `json:"coverage"`
}

// catalogOrganization is one organization, keyed by its ID.
type catalogOrganization struct {
	ManagementAccountID string `json:"management_account_id,omitempty"`
}

// catalogAccount is one account of the catalog, keyed by its ID.
type catalogAccount struct {
	Coverage       string `json:"coverage"`
	Name           string `json:"name,omitempty"`
	OrganizationID string `json:"organization_id,omitempty"`
}

// collectAccountCatalog reads the caller's organization and account
// name into data. Each read has its own catalogTimeout budget.
func (c *Client) collectAccountCatalog(data *RegionData) {
	c.collectOrganization(data)
	c.collectAccountName(data)
}

// collectOrganization reads the caller's organization and, when it
// exists, the names of its accounts into data. A caller outside any
// organization is not an error.
func (c *Client) collectOrganization(data *RegionData) {
	ctx, cancel := context.WithTimeout(context.Background(), catalogTimeout)
	defer cancel()
	org, err := organizations.NewFromConfig(c.awsCfg).DescribeOrganization(ctx, &organizations.DescribeOrganizationInput{})
	var notInUse *orgtypes.AWSOrganizationsNotInUseException
	switch {
	case errors.As(err, &notInUse):
		c.logger.Debug("account is not a member of an organization")
	case err != nil:
		c.logCollectErr("organization", err)
	case org.Organization != nil && organizationIDPattern.MatchString(aws.ToString(org.Organization.Id)):
		data.Organization.ID = aws.ToString(org.Organization.Id)
		if id := aws.ToString(org.Organization.MasterAccountId); validAccountID(id) {
			data.Organization.ManagementAccountID = id
		}
		c.collectOrganizationAccounts(data)
	}
}

// collectAccountName reads the verified caller's account name into
// data.
func (c *Client) collectAccountName(data *RegionData) {
	ctx, cancel := context.WithTimeout(context.Background(), catalogTimeout)
	defer cancel()
	info, err := account.NewFromConfig(c.awsCfg).GetAccountInformation(ctx, &account.GetAccountInformationInput{})
	if err != nil {
		c.logCollectErr("account name", err)
		return
	}
	// The name must describe the verified caller and nothing else.
	if aws.ToString(info.AccountId) == data.Identity.AccountID {
		data.AccountName = aws.ToString(info.AccountName)
	}
}

// collectOrganizationAccounts reads the names of the organization's
// accounts into data. Accounts listed before a failing page are kept.
func (c *Client) collectOrganizationAccounts(data *RegionData) {
	ctx, cancel := context.WithTimeout(context.Background(), catalogTimeout)
	defer cancel()
	names := map[string]string{}
	pager := organizations.NewListAccountsPaginator(organizations.NewFromConfig(c.awsCfg), &organizations.ListAccountsInput{},
		func(o *organizations.ListAccountsPaginatorOptions) { o.StopOnDuplicateToken = true })
	for pages := 0; pager.HasMorePages() && pages < maxCorePages; pages++ {
		out, err := pager.NextPage(ctx)
		if err != nil {
			if isPermissionDeniedError(err) && data.Identity.AccountID != data.Organization.ManagementAccountID {
				c.logger.Debug("organization accounts are not visible to this account")
			} else {
				c.logCollectErr("organization accounts", err)
			}
			break
		}
		for _, a := range out.Accounts {
			if id := aws.ToString(a.Id); validAccountID(id) {
				names[id] = aws.ToString(a.Name)
			}
		}
	}
	data.Organization.AccountNames = names
}

// catalogName returns name as an emittable account name, or "" when it
// is empty, repeats the account ID or matches a secret pattern.
func catalogName(name, id string) string {
	name = strings.TrimSpace(name)
	if name == id || sdk.ScanValue(name) != "" {
		return ""
	}
	return name
}

// exportedName returns the name AWS reported for the verified caller
// account, or "".
func exportedName(data *RegionData) string {
	id := data.Identity.AccountID
	if name := catalogName(data.AccountName, id); name != "" {
		return name
	}
	return catalogName(data.Organization.AccountNames[id], id)
}

// scopeName returns the document's metadata.scope.name: the account
// name AWS reported, else the CSV account_name label, else the account
// ID.
func scopeName(data *RegionData, target AccountTarget) string {
	if name := exportedName(data); name != "" {
		return name
	}
	if target.AccountName != "" {
		return target.AccountName
	}
	return data.Identity.AccountID
}

// referencedAccounts returns the IDs of every account the document's
// resources and connections name: provider.account, the account of any
// ARN and the values of [accountReferenceKeys]. Raw bodies, tags and
// descriptions are not scanned, so the catalog does not depend on
// --include-raw-body or on free text.
func referencedAccounts(doc *sdk.Document) map[string]bool {
	found := map[string]bool{}
	var walk func(v any)
	walkKey := func(k string, v any) {
		if k == "description" {
			return
		}
		if s, ok := v.(string); ok && accountReferenceKeys[k] && validAccountID(s) {
			found[s] = true
		}
		walk(v)
	}
	walk = func(v any) {
		switch t := v.(type) {
		case string:
			for _, m := range arnAccountRe.FindAllStringSubmatch(t, -1) {
				found[m[1]] = true
			}
		case []string:
			for _, s := range t {
				walk(s)
			}
		case []any:
			for _, x := range t {
				walk(x)
			}
		case []map[string]any:
			for _, x := range t {
				walk(x)
			}
		case map[string]any:
			for k, x := range t {
				walkKey(k, x)
			}
		case map[string]string:
			for k, x := range t {
				walkKey(k, x)
			}
		}
	}
	extensions := func(ext map[string]any) {
		for k, v := range ext {
			if k != "osiris.aws.sdk" {
				walk(v)
			}
		}
	}
	for _, r := range doc.Topology.Resources {
		if validAccountID(r.Provider.Account) {
			found[r.Provider.Account] = true
		}
		walk(r.ID)
		walk(r.Provider.NativeID)
		walk(r.Properties)
		extensions(r.Extensions)
	}
	for _, c := range doc.Topology.Connections {
		walk(c.Properties)
		extensions(c.Extensions)
	}
	return found
}

// addAccountCatalog sets the organizations and accounts of meta. The
// organization is omitted when unknown.
func addAccountCatalog(meta *awsMetadata, doc *sdk.Document, data *RegionData) {
	org := data.Organization
	accounts := map[string]catalogAccount{}
	reference := func(id string) {
		a := catalogAccount{Coverage: coverageReferenceOnly}
		name, member := org.AccountNames[id]
		if member || id == org.ManagementAccountID {
			a.Name, a.OrganizationID = catalogName(name, id), org.ID
		}
		accounts[id] = a
	}
	for id := range referencedAccounts(doc) {
		reference(id)
	}
	if org.ID != "" {
		meta.Organizations = map[string]catalogOrganization{
			org.ID: {ManagementAccountID: org.ManagementAccountID},
		}
		if org.ManagementAccountID != "" {
			reference(org.ManagementAccountID)
		}
	}
	if id := data.Identity.AccountID; id != "" {
		accounts[id] = catalogAccount{
			Coverage:       coverageExported,
			Name:           exportedName(data),
			OrganizationID: org.ID,
		}
	}
	meta.Accounts = accounts
}
