// identity_test.go - Tests for caller identity verification, partition
// handling, target validation and connection endpoint resolution
// (identity.go, Client.ResolveIdentity) in the OSIRIS JSON Amazon AWS
// producer.
//
// STS is exercised through the real AWS SDK client with a fake HTTP
// transport, so no network access or credentials are needed.
//
// For an introduction to OSIRIS JSON Producer for Amazon AWS see:
// [OSIRIS-JSON-AWS]: https://docs.osirisjson.org/osiris-producers/hyperscalers/amazon-aws/

package aws

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"

	"go.osirisjson.org/producers/pkg/sdk"
)

// stsResponse is one canned STS reply.
type stsResponse struct {
	status int
	body   string
}

// fakeSTS answers STS requests in order and records the hosts called.
type fakeSTS struct {
	replies []stsResponse
	hosts   []string
}

func (f *fakeSTS) Do(req *http.Request) (*http.Response, error) {
	f.hosts = append(f.hosts, req.URL.Host)
	r := stsResponse{status: 500, body: "no reply"}
	if len(f.replies) > 0 {
		r, f.replies = f.replies[0], f.replies[1:]
	}
	return &http.Response{
		StatusCode: r.status,
		Header:     http.Header{"Content-Type": []string{"text/xml"}},
		Body:       io.NopCloser(strings.NewReader(r.body)),
		Request:    req,
	}, nil
}

func stsOK(arn, account string) stsResponse {
	return stsResponse{200, `<GetCallerIdentityResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/"><GetCallerIdentityResult>` +
		`<Arn>` + arn + `</Arn><UserId>TESTUSERID</UserId><Account>` + account + `</Account>` +
		`</GetCallerIdentityResult><ResponseMetadata><RequestId>TEST-REQUEST-ID</RequestId></ResponseMetadata></GetCallerIdentityResponse>`}
}

func stsDenied() stsResponse {
	return stsResponse{403, `<ErrorResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/"><Error><Type>Sender</Type>` +
		`<Code>AccessDenied</Code><Message>TEST denied</Message></Error><RequestId>TEST-REQUEST-ID</RequestId></ErrorResponse>`}
}

func testClient(region, expected string, f *fakeSTS) *Client {
	return &Client{
		expectedAccountID: expected,
		region:            region,
		logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
		awsCfg: aws.Config{
			Region:     region,
			HTTPClient: f,
			Retryer:    func() aws.Retryer { return aws.NopRetryer{} },
			Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
				return aws.Credentials{AccessKeyID: "test", SecretAccessKey: "test"}, nil
			}),
		},
	}
}

func TestResolveIdentity(t *testing.T) {
	const callerARN = "arn:aws:iam::123456789012:user/TEST-USER"
	tests := []struct {
		name          string
		region        string
		expected      string
		replies       []stsResponse
		wantErr       error
		wantAccount   string
		wantPartition string
		wantHosts     []string
	}{
		{
			name: "regional success", region: "eu-west-1", expected: "123456789012",
			replies:     []stsResponse{stsOK(callerARN, "123456789012")},
			wantAccount: "123456789012", wantPartition: "aws",
			wantHosts: []string{"sts.eu-west-1.amazonaws.com"},
		},
		{
			name: "regional denied, us-east-1 retry succeeds", region: "eu-west-1",
			replies:     []stsResponse{stsDenied(), stsOK(callerARN, "123456789012")},
			wantAccount: "123456789012", wantPartition: "aws",
			wantHosts: []string{"sts.eu-west-1.amazonaws.com", "sts.us-east-1.amazonaws.com"},
		},
		{
			name: "both denied, declared account never used", region: "eu-west-1", expected: "123456789012",
			replies:   []stsResponse{stsDenied(), stsDenied()},
			wantErr:   errIdentityUnverified,
			wantHosts: []string{"sts.eu-west-1.amazonaws.com", "sts.us-east-1.amazonaws.com"},
		},
		{
			name: "account mismatch rejected", region: GlobalRegion, expected: "111122223333",
			replies:   []stsResponse{stsOK(callerARN, "123456789012")},
			wantErr:   errIdentityMismatch,
			wantHosts: []string{"sts.us-east-1.amazonaws.com"},
		},
		{
			name: "malformed account rejected", region: GlobalRegion,
			replies:   []stsResponse{stsOK(callerARN, "TEST")},
			wantErr:   errIdentityUnverified,
			wantHosts: []string{"sts.us-east-1.amazonaws.com"},
		},
		{
			name: "non-commercial partition has no us-east-1 retry", region: "cn-north-1",
			replies:   []stsResponse{stsDenied()},
			wantErr:   errIdentityUnverified,
			wantHosts: []string{"sts.cn-north-1.amazonaws.com.cn"},
		},
		{
			name: "partition from caller ARN", region: "cn-north-1",
			replies:     []stsResponse{stsOK("arn:aws-cn:iam::123456789012:user/TEST-USER", "123456789012")},
			wantAccount: "123456789012", wantPartition: "aws-cn",
			wantHosts: []string{"sts.cn-north-1.amazonaws.com.cn"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeSTS{replies: tt.replies}
			c := testClient(tt.region, tt.expected, f)
			id, err := c.ResolveIdentity()
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				if id.AccountID != "" || c.accountID != "" {
					t.Errorf("identity %+v / client account %q set on failure", id, c.accountID)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if id.AccountID != tt.wantAccount || id.Partition != tt.wantPartition {
					t.Errorf("identity = %+v, want account %s partition %s", id, tt.wantAccount, tt.wantPartition)
				}
			}
			if !reflect.DeepEqual(f.hosts, tt.wantHosts) {
				t.Errorf("STS hosts = %v, want %v", f.hosts, tt.wantHosts)
			}
		})
	}
}

func TestIdentityCoverageMessages(t *testing.T) {
	f := &fakeSTS{replies: []stsResponse{stsDenied()}}
	_, err := testClient(GlobalRegion, "", f).ResolveIdentity()
	if got := coverageErrorMessage(err); got != "identity unverified: STS GetCallerIdentity: AccessDenied" {
		t.Errorf("coverage = %q", got)
	}
	f = &fakeSTS{replies: []stsResponse{stsOK("arn:aws:iam::123456789012:user/TEST-USER", "123456789012")}}
	_, err = testClient(GlobalRegion, "111122223333", f).ResolveIdentity()
	if got := coverageErrorMessage(err); got != "identity mismatch" {
		t.Errorf("coverage = %q", got)
	}
}

func TestValidateTargets(t *testing.T) {
	tests := []struct {
		name   string
		target AccountTarget
		ok     bool
	}{
		{"valid", AccountTarget{Profile: "test", AccountID: "123456789012", Regions: []string{"us-east-1", "us-gov-west-1", "eusc-de-east-1"}}, true},
		{"no account", AccountTarget{Profile: "test", Regions: []string{"eu-west-1"}}, true},
		{"short account", AccountTarget{Profile: "test", AccountID: "12345"}, false},
		{"traversal region", AccountTarget{Profile: "test", Regions: []string{"../../etc"}}, false},
		{"separator region", AccountTarget{Profile: "test", Regions: []string{"us-east-1/x"}}, false},
		{"upper-case region", AccountTarget{Profile: "test", Regions: []string{"US-EAST-1"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTargets([]AccountTarget{tt.target})
			if (err == nil) != tt.ok {
				t.Errorf("validateTargets err = %v, want ok=%v", err, tt.ok)
			}
		})
	}
}

func TestParseFlagsRejectsInvalidRegion(t *testing.T) {
	if _, err := ParseFlags([]string{"--profile", "test", "--region", "../x"}); err == nil {
		t.Fatal("invalid region accepted")
	}
}

func TestRewritePartition(t *testing.T) {
	const local = "aws::arn:aws:ec2:cn-north-1:123456789012:vpc/vpc-00000000000000001"
	const returned = "aws::arn:aws-cn:elasticloadbalancing:cn-north-1:123456789012:loadbalancer/app/TEST/0000000000000001"
	resources := []sdk.Resource{{ID: local}, {ID: returned}}
	conns := []sdk.Connection{{Source: local, Target: returned}}
	groups := []sdk.Group{{ID: "g", Members: []string{local, returned}}}

	rewritePartition(resources, conns, groups, "aws")
	if resources[0].ID != local {
		t.Fatalf("commercial partition changed ID: %s", resources[0].ID)
	}
	rewritePartition(resources, conns, groups, "aws-cn")
	want := "aws::arn:aws-cn:ec2:cn-north-1:123456789012:vpc/vpc-00000000000000001"
	if resources[0].ID != want || conns[0].Source != want || groups[0].Members[0] != want {
		t.Errorf("constructed ID not rewritten: %s %s %s", resources[0].ID, conns[0].Source, groups[0].Members[0])
	}
	if resources[1].ID != returned || conns[0].Target != returned || groups[0].Members[1] != returned {
		t.Errorf("returned ARN changed")
	}
}

func TestResolveConnectionEndpoints(t *testing.T) {
	const caller = "123456789012"
	local := "aws::arn:aws:ec2:eu-west-1:123456789012:vpc/vpc-00000000000000001"
	localMissing := "aws::arn:aws:ec2:eu-west-1:123456789012:route-table/rtb-00000000000000001"
	remote := "aws::arn:aws:ec2:eu-west-1:111122223333:vpc/vpc-00000000000000002"
	malformed := "aws::not-an-arn"
	known := []sdk.Resource{{ID: local}}
	conns := []sdk.Connection{
		{ID: "c-remote", Source: local, Target: remote},
		{ID: "c-local-missing", Source: local, Target: localMissing},
		{ID: "c-malformed", Source: malformed, Target: local},
		{ID: "c-half", Source: remote, Target: localMissing},
	}
	kept, stubs, dropped := resolveConnectionEndpoints(conns, known, caller)
	if dropped != 3 {
		t.Errorf("dropped = %d, want 3", dropped)
	}
	if len(kept) != 1 || kept[0].ID != "c-remote" {
		t.Errorf("kept = %+v, want only c-remote", kept)
	}
	if len(stubs) != 1 || stubs[0].ID != remote || stubs[0].Provider.Account != "111122223333" {
		t.Errorf("stubs = %+v, want one remote stub", stubs)
	}
}

func TestRDSClusterMalformedARNNoPanic(t *testing.T) {
	clusters := []rdstypes.DBCluster{{
		DBClusterArn:        aws.String("malformed"),
		DBClusterIdentifier: aws.String("TEST-CLUSTER"),
		DBClusterMembers:    []rdstypes.DBClusterMember{{DBInstanceIdentifier: aws.String("TEST-DB")}},
	}}
	conns := TransformRDSClusterContainsInstanceConnections(clusters, map[string]string{"malformed": "aws::x"}, map[string]string{})
	if len(conns) != 0 {
		t.Errorf("conns = %+v, want none", conns)
	}
}
