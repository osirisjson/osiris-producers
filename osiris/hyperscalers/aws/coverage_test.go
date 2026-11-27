// coverage_test.go - Tests for paged collection and operation outcomes
// (coverage.go) in the OSIRIS JSON Amazon AWS producer.
//
// The AWS SDK EC2 paginators are exercised with a fake HTTP transport
// (see identity_test.go), so page boundaries, errors after a first page
// and repeated tokens are tested against the real SDK behavior.
//
// For an introduction to OSIRIS JSON Producer for Amazon AWS see:
// [OSIRIS-JSON-AWS]: https://docs.osirisjson.org/osiris-producers/hyperscalers/amazon-aws/

package aws

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// fakePager returns canned pages and errors in order.
type fakePager struct {
	pages []int // items per page
	errAt int   // page index that fails, -1 for none
	err   error
	next  int
}

func (p *fakePager) HasMorePages() bool { return p.next < len(p.pages) }

func (p *fakePager) NextPage(ctx context.Context, _ ...func(*ec2.Options)) ([]int, error) {
	i := p.next
	p.next++
	if i == p.errAt {
		return nil, p.err
	}
	out := make([]int, p.pages[i])
	return out, nil
}

func TestCollectPagesOutcomes(t *testing.T) {
	denied := errors.New("api error UnauthorizedOperation: TEST")
	failed := errors.New("dial tcp: no such host")
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	tests := []struct {
		name      string
		ctx       context.Context
		pager     *fakePager
		maxPages  int
		wantItems int
		want      string
	}{
		{"all pages", context.Background(), &fakePager{pages: []int{2, 2, 1}, errAt: -1}, 10, 5, outcomeSucceeded},
		{"empty", context.Background(), &fakePager{pages: []int{0}, errAt: -1}, 10, 0, outcomeSucceededEmpty},
		{"no pages", context.Background(), &fakePager{errAt: -1}, 10, 0, outcomeSucceededEmpty},
		{"failure after first page keeps items", context.Background(), &fakePager{pages: []int{3, 3}, errAt: 1, err: failed}, 10, 3, outcomePartial},
		{"denied first page", context.Background(), &fakePager{pages: []int{3}, errAt: 0, err: denied}, 10, 0, outcomeDenied},
		{"failed first page", context.Background(), &fakePager{pages: []int{3}, errAt: 0, err: failed}, 10, 0, outcomeFailed},
		{"budget", context.Background(), &fakePager{pages: []int{1, 1, 1}, errAt: -1}, 2, 2, outcomeBudgetLimited},
		{"cancelled", canceled, &fakePager{pages: []int{1}, errAt: -1}, 10, 0, outcomeCancelled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items, res := collectPages(tt.ctx, ec2Pager[[]int](tt.pager), tt.maxPages, func(o []int) []int { return o })
			o := classifyPages("ec2", "DescribeTest", len(items), res)
			if len(items) != tt.wantItems || o.Outcome != tt.want {
				t.Errorf("items = %d outcome = %s, want %d %s", len(items), o.Outcome, tt.wantItems, tt.want)
			}
			if o.complete() != (tt.want == outcomeSucceeded || tt.want == outcomeSucceededEmpty) {
				t.Errorf("complete() = %v for %s", o.complete(), o.Outcome)
			}
		})
	}
}

func ec2VPCPage(ids []string, next string) stsResponse {
	body := `<DescribeVpcsResponse xmlns="http://ec2.amazonaws.com/doc/2016-11-15/"><requestId>TEST-REQUEST-ID</requestId><vpcSet>`
	for _, id := range ids {
		body += `<item><vpcId>` + id + `</vpcId></item>`
	}
	body += `</vpcSet>`
	if next != "" {
		body += `<nextToken>` + next + `</nextToken>`
	}
	return stsResponse{200, body + `</DescribeVpcsResponse>`}
}

func ec2Denied() stsResponse {
	return stsResponse{403, `<Response><Errors><Error><Code>UnauthorizedOperation</Code><Message>TEST denied</Message></Error></Errors><RequestID>TEST-REQUEST-ID</RequestID></Response>`}
}

func TestDescribeVpcsPaginatorWithSDK(t *testing.T) {
	tests := []struct {
		name      string
		replies   []stsResponse
		wantItems int
		wantPages int
		want      string
	}{
		{
			name:      "last page present",
			replies:   []stsResponse{ec2VPCPage([]string{"vpc-00000000000000001"}, "TEST-TOKEN-1"), ec2VPCPage([]string{"vpc-00000000000000002"}, "")},
			wantItems: 2, wantPages: 2, want: outcomeSucceeded,
		},
		{
			name:      "empty page with token continues",
			replies:   []stsResponse{ec2VPCPage(nil, "TEST-TOKEN-1"), ec2VPCPage([]string{"vpc-00000000000000002"}, "")},
			wantItems: 1, wantPages: 2, want: outcomeSucceeded,
		},
		{
			name:      "repeated token stops",
			replies:   []stsResponse{ec2VPCPage([]string{"vpc-00000000000000001"}, "TEST-TOKEN-1"), ec2VPCPage([]string{"vpc-00000000000000002"}, "TEST-TOKEN-1"), ec2VPCPage([]string{"vpc-00000000000000003"}, "")},
			wantItems: 2, wantPages: 2, want: outcomeSucceeded,
		},
		{
			name:      "failed second page retains first",
			replies:   []stsResponse{ec2VPCPage([]string{"vpc-00000000000000001"}, "TEST-TOKEN-1"), ec2Denied()},
			wantItems: 1, wantPages: 1, want: outcomePartial,
		},
		{
			name:      "denied",
			replies:   []stsResponse{ec2Denied()},
			wantItems: 0, wantPages: 0, want: outcomeDenied,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeSTS{replies: tt.replies}
			c := testClient("eu-west-1", "", f)
			client := ec2.NewFromConfig(c.awsCfg)
			p := ec2.NewDescribeVpcsPaginator(client, &ec2.DescribeVpcsInput{MaxResults: aws.Int32(1000)},
				func(o *ec2.DescribeVpcsPaginatorOptions) { o.StopOnDuplicateToken = true })
			vpcs, res := collectPages(context.Background(), p, maxCorePages, func(o *ec2.DescribeVpcsOutput) []ec2types.Vpc { return o.Vpcs })
			c.recordCore("DescribeVpcs", "VPCs", len(vpcs), res)
			o := c.CoreOutcomes()[0]
			if len(vpcs) != tt.wantItems || o.Pages != tt.wantPages || o.Outcome != tt.want {
				t.Errorf("items = %d pages = %d outcome = %s, want %d %d %s", len(vpcs), o.Pages, o.Outcome, tt.wantItems, tt.wantPages, tt.want)
			}
			if tt.want == outcomePartial || tt.want == outcomeDenied {
				errs := c.CollectedErrors()
				if len(errs) != 1 || errs[0].ErrorMessage != "EC2 DescribeVpcs: UnauthorizedOperation" {
					t.Errorf("coverage errors = %+v", errs)
				}
			}
		})
	}
}

func TestBuildAWSMetadata(t *testing.T) {
	ok := []operationOutcome{
		{Service: "ec2", Operation: "DescribeVpcs", Outcome: outcomeSucceeded},
		{Service: "ec2", Operation: "DescribeSubnets", Outcome: outcomeSucceededEmpty},
	}
	m := buildAWSMetadata(ok, []string{"s3", "ec2"}).Coverage
	if m["core_complete"] != true || m["schema"] != coverageSchema {
		t.Errorf("metadata = %v", m)
	}
	ops := m["core_operations"].([]operationOutcome)
	if ops[0].Operation != "DescribeSubnets" {
		t.Errorf("operations not sorted: %+v", ops)
	}
	if got := m["unverified_pagination"].([]string); got[0] != "ec2" || got[1] != "s3" {
		t.Errorf("unverified = %v", got)
	}
	partial := append(ok, operationOutcome{Service: "ec2", Operation: "DescribeInstances", Outcome: outcomePartial})
	if buildAWSMetadata(partial, nil).Coverage["core_complete"] != false {
		t.Error("partial core reported complete")
	}
	if buildAWSMetadata(nil, nil).Coverage["core_complete"] != false {
		t.Error("no operations reported complete")
	}
}
