// account_catalog_test.go - Tests for the organization and account
// catalog (account_catalog.go) in the OSIRIS JSON Amazon AWS producer.
//
// Both services are exercised through the real AWS SDK clients with a
// fake HTTP transport, so no network access or credentials are needed.
//
// For an introduction to OSIRIS JSON Producer for Amazon AWS see:
// [OSIRIS-JSON-AWS]: https://docs.osirisjson.org/osiris-producers/hyperscalers/amazon-aws/

package aws

import (
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"go.osirisjson.org/producers/pkg/sdk"
)

// fakeCatalog answers Organizations and Account Management requests.
type fakeCatalog struct{ org, account, list stsResponse }

func (f fakeCatalog) Do(req *http.Request) (*http.Response, error) {
	r := f.org
	if strings.HasPrefix(req.URL.Host, "account.") {
		r = f.account
	}
	if strings.HasSuffix(req.Header.Get("X-Amz-Target"), ".ListAccounts") {
		r = f.list
	}
	return &http.Response{
		StatusCode: r.status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(r.body)),
		Request:    req,
	}, nil
}

func TestCollectAccountCatalog(t *testing.T) {
	const (
		self    = "123456789012"
		orgBody = `{"Organization":{"Id":"o-testorg0001","MasterAccountId":"111122223333","MasterAccountEmail":"admin@example.com"}}`
		denied  = `{"__type":"AccessDeniedException","Message":"TEST denied"}`
	)
	name := func(id string) stsResponse {
		return stsResponse{200, `{"AccountId":"` + id + `","AccountName":"TEST-WORKLOAD"}`}
	}
	org := OrganizationInfo{ID: "o-testorg0001", ManagementAccountID: "111122223333", AccountNames: map[string]string{}}
	listed := org
	listed.AccountNames = map[string]string{"111122223333": "TEST-MANAGEMENT"}
	listOK := stsResponse{200, `{"Accounts":[{"Id":"111122223333","Name":"TEST-MANAGEMENT","Email":"admin@example.com"}]}`}
	listDenied := stsResponse{400, denied}
	tests := []struct {
		name     string
		fake     fakeCatalog
		wantOrg  OrganizationInfo
		wantName string
		wantErrs []string
	}{
		{"member account", fakeCatalog{stsResponse{200, orgBody}, name(self), listDenied}, org, "TEST-WORKLOAD", nil},
		{"accounts listed", fakeCatalog{stsResponse{200, orgBody}, name(self), listOK}, listed, "TEST-WORKLOAD", nil},
		{"accounts failed", fakeCatalog{stsResponse{200, orgBody}, name(self), stsResponse{400, `{"__type":"TooManyRequestsException"}`}}, org, "TEST-WORKLOAD", []string{"organization accounts"}},
		{"organization denied", fakeCatalog{stsResponse{400, denied}, name(self), listOK}, OrganizationInfo{}, "TEST-WORKLOAD", []string{"organization"}},
		{"not in an organization", fakeCatalog{stsResponse{400, `{"__type":"AWSOrganizationsNotInUseException","Message":"TEST"}`}, name(self), listDenied}, OrganizationInfo{}, "TEST-WORKLOAD", nil},
		{"name denied", fakeCatalog{stsResponse{200, orgBody}, stsResponse{403, denied}, listDenied}, org, "", []string{"account name"}},
		{"name of another account", fakeCatalog{stsResponse{200, orgBody}, name("111122223333"), listDenied}, org, "", nil},
		{"malformed organization", fakeCatalog{stsResponse{200, `{"Organization":{"Id":"x"}}`}, name(self), listOK}, OrganizationInfo{}, "TEST-WORKLOAD", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := testClient("eu-west-1", "", &fakeSTS{})
			c.awsCfg.HTTPClient = tt.fake
			data := &RegionData{Identity: CallerIdentity{AccountID: self}}
			c.collectAccountCatalog(data)
			if !reflect.DeepEqual(data.Organization, tt.wantOrg) {
				t.Errorf("organization = %+v, want %+v", data.Organization, tt.wantOrg)
			}
			if data.AccountName != tt.wantName {
				t.Errorf("account name = %q, want %q", data.AccountName, tt.wantName)
			}
			var errs []string
			for _, e := range c.CollectedErrors() {
				errs = append(errs, e.Resource)
				if strings.Contains(e.ErrorMessage, "TEST denied") {
					t.Errorf("raw API message reported: %q", e.ErrorMessage)
				}
			}
			if !reflect.DeepEqual(errs, tt.wantErrs) {
				t.Errorf("coverage errors = %v, want %v", errs, tt.wantErrs)
			}
		})
	}
}

func TestReferencedAccounts(t *testing.T) {
	const id = "111122223333"
	resource := func(r sdk.Resource) *sdk.Document {
		return &sdk.Document{Topology: sdk.Topology{Resources: []sdk.Resource{r}}}
	}
	props := func(k string, v any) *sdk.Document {
		return resource(sdk.Resource{Properties: map[string]any{k: v}})
	}
	ext := func(ns string, v any) *sdk.Document {
		return resource(sdk.Resource{Extensions: map[string]any{ns: v}})
	}
	tests := []struct {
		name string
		doc  *sdk.Document
		want bool
	}{
		{"resource ID", resource(sdk.Resource{ID: "aws::arn:aws:ec2:eu-west-1:" + id + ":vpc/vpc-1"}), true},
		{"provider account", resource(sdk.Resource{Provider: sdk.Provider{Account: id}}), true},
		{"native ID", resource(sdk.Resource{Provider: sdk.Provider{NativeID: "arn:aws-cn:sns:cn-north-1:" + id + ":TEST"}}), true},
		{"owner property", props("owner_id", id), true},
		{"embedded ARN", props("policy", `{"Principal":"arn:aws:iam::`+id+`:root"}`), true},
		{"nested list", props("list", []map[string]any{{"owner_account": id}}), true},
		{"nested any list", props("list", []any{map[string]any{"owner_id": id}}), true},
		{"string map", props("peer", map[string]string{"requester_account": id}), true},
		{"extension", ext("osiris.aws", map[string]any{"refs": []string{"arn:aws:kms:eu-west-1:" + id + ":key/1"}}), true},
		{"connection", &sdk.Document{Topology: sdk.Topology{Connections: []sdk.Connection{{Properties: map[string]any{"accepter_account": id}}}}}, true},
		{"raw body", ext("osiris.aws.sdk", map[string]any{"owner_id": id}), false},
		{"tag", resource(sdk.Resource{Tags: map[string]string{"owner_id": id}}), false},
		{"description", props("description", "arn:aws:iam::"+id+":role/TEST"), false},
		{"unrelated number", props("size", id), false},
		{"service owner", props("owner_id", "amazon"), false},
		{"AWS managed ARN", props("policy", "arn:aws:iam::aws:policy/ReadOnlyAccess"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := referencedAccounts(tt.doc)
			if got[id] != tt.want || (!tt.want && len(got) != 0) {
				t.Errorf("referencedAccounts = %v, want found = %v", got, tt.want)
			}
		})
	}
}

func TestAddAccountCatalog(t *testing.T) {
	doc := &sdk.Document{Topology: sdk.Topology{Resources: []sdk.Resource{
		{Provider: sdk.Provider{Account: "123456789012"}},
		{Provider: sdk.Provider{Account: "444455556666"}},
	}}}
	build := func(org OrganizationInfo, name string) (awsMetadata, map[string]catalogAccount) {
		var meta awsMetadata
		addAccountCatalog(&meta, doc, &RegionData{
			Identity: CallerIdentity{AccountID: "123456789012"}, Organization: org, AccountName: name,
		})
		return meta, meta.Accounts
	}

	_, got := build(OrganizationInfo{ID: "o-testorg0001", ManagementAccountID: "111122223333"}, "TEST-WORKLOAD")
	want := map[string]catalogAccount{
		"123456789012": {Coverage: coverageExported, Name: "TEST-WORKLOAD", OrganizationID: "o-testorg0001"},
		"111122223333": {Coverage: coverageReferenceOnly, OrganizationID: "o-testorg0001"},
		"444455556666": {Coverage: coverageReferenceOnly},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("accounts = %v, want %v", got, want)
	}

	// Listed members get their name and organization; others never do.
	_, got = build(OrganizationInfo{ID: "o-testorg0001", ManagementAccountID: "111122223333",
		AccountNames: map[string]string{"111122223333": "TEST-MANAGEMENT", "444455556666": "TEST-NETWORK"}}, "")
	if want := (catalogAccount{Coverage: coverageReferenceOnly, Name: "TEST-NETWORK", OrganizationID: "o-testorg0001"}); got["444455556666"] != want {
		t.Errorf("listed member = %+v, want %+v", got["444455556666"], want)
	}
	if got["111122223333"].Name != "TEST-MANAGEMENT" {
		t.Errorf("management account = %+v", got["111122223333"])
	}

	// The exported account stays exported when it is the management account.
	_, got = build(OrganizationInfo{ID: "o-testorg0001", ManagementAccountID: "123456789012"}, "")
	if got["123456789012"].Coverage != coverageExported {
		t.Errorf("management account = %+v, want exported", got["123456789012"])
	}

	// Nothing known: no organization and an ID is not a name.
	meta, got := build(OrganizationInfo{}, "123456789012")
	if meta.Organizations != nil {
		t.Error("unknown organization emitted")
	}
	if got["123456789012"] != (catalogAccount{Coverage: coverageExported}) {
		t.Errorf("exported account = %+v", got["123456789012"])
	}
}

func TestScopeName(t *testing.T) {
	const id = "123456789012"
	tests := []struct{ name, awsName, label, want string }{
		{"AWS name", "TEST-WORKLOAD", "label", "TEST-WORKLOAD"},
		{"CSV label", "", "label", "label"},
		{"account ID", "", "", id},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &RegionData{Identity: CallerIdentity{AccountID: id}, AccountName: tt.awsName}
			if got := scopeName(data, AccountTarget{AccountName: tt.label}); got != tt.want {
				t.Errorf("scopeName = %q, want %q", got, tt.want)
			}
		})
	}
}
