// coverage.go - Paged collection and per-operation outcomes for the
// OSIRIS JSON Amazon AWS producer.
//
// The core VPC slice (VPCs, subnets, route tables, network interfaces
// and instances) is read with the AWS SDK paginators through
// [collectPages], bounded by [maxCorePages] and stopping on a repeated
// token. Each core operation records an [operationOutcome] that
// distinguishes succeeded, empty, partial, budget-limited, cancelled,
// denied and failed reads, so an empty result is never confused with a
// failed one.
//
// Outcomes are emitted under metadata["osiris.aws"] (see
// [buildAWSMetadata]) together with the list of legacy collectors whose
// pagination is not verified. The legacy metadata.coverage fields are
// kept for compatibility.
//
// For an introduction to OSIRIS JSON Producer for Amazon AWS see:
// [OSIRIS-JSON-AWS]: https://docs.osirisjson.org/osiris-producers/hyperscalers/amazon-aws/

package aws

import (
	"context"
	"errors"
	"sort"

	"github.com/aws/aws-sdk-go-v2/service/ec2"

	"go.osirisjson.org/producers/pkg/sdk"
)

// Operation outcomes.
const (
	outcomeSucceeded      = "succeeded"
	outcomeSucceededEmpty = "succeeded-empty"
	outcomePartial        = "partial"
	outcomeBudgetLimited  = "budget-limited"
	outcomeCancelled      = "cancelled"
	outcomeDenied         = "denied"
	outcomeFailed         = "failed"
)

// coverageSchema identifies the metadata["osiris.aws"] coverage shape.
const coverageSchema = "operation-outcomes/v1"

// maxCorePages bounds the pages read per core operation. At the page
// sizes used here this covers far more resources than a region quota
// allows; reaching it marks the operation budget-limited.
const maxCorePages = 200

// operationOutcome is the result of one collection operation.
type operationOutcome struct {
	Service   string `json:"service"`
	Operation string `json:"operation"`
	Outcome   string `json:"outcome"`
	Pages     int    `json:"pages"`
	Items     int    `json:"items"`
	Reason    string `json:"reason,omitempty"`
}

// complete reports whether the operation read every item.
func (o operationOutcome) complete() bool {
	return o.Outcome == outcomeSucceeded || o.Outcome == outcomeSucceededEmpty
}

// ec2Pager is the shape shared by the AWS SDK EC2 paginators.
type ec2Pager[O any] interface {
	HasMorePages() bool
	NextPage(ctx context.Context, optFns ...func(*ec2.Options)) (O, error)
}

// pageResult is the raw outcome of [collectPages].
type pageResult struct {
	Pages     int
	Truncated bool
	Err       error
}

// collectPages reads pages from p until it is exhausted, an error
// occurs, ctx is done or maxPages pages were read. Items from pages
// read before an error are kept.
func collectPages[O, I any](ctx context.Context, p ec2Pager[O], maxPages int, items func(O) []I) ([]I, pageResult) {
	var all []I
	var res pageResult
	for p.HasMorePages() {
		if res.Pages >= maxPages {
			res.Truncated = true
			break
		}
		if err := ctx.Err(); err != nil {
			res.Err = err
			break
		}
		out, err := p.NextPage(ctx)
		if err != nil {
			res.Err = err
			break
		}
		res.Pages++
		all = append(all, items(out)...)
	}
	return all, res
}

// classifyPages turns a page result into an operation outcome.
func classifyPages(service, operation string, items int, res pageResult) operationOutcome {
	o := operationOutcome{Service: service, Operation: operation, Pages: res.Pages, Items: items}
	switch {
	case res.Err != nil && (errors.Is(res.Err, context.Canceled) || errors.Is(res.Err, context.DeadlineExceeded)):
		o.Outcome = outcomeCancelled
	case res.Err != nil && res.Pages > 0:
		o.Outcome = outcomePartial
	case res.Err != nil && isPermissionDeniedError(res.Err):
		o.Outcome = outcomeDenied
	case res.Err != nil:
		o.Outcome = outcomeFailed
	case res.Truncated:
		o.Outcome = outcomeBudgetLimited
	case items == 0:
		o.Outcome = outcomeSucceededEmpty
	default:
		o.Outcome = outcomeSucceeded
	}
	if res.Err != nil {
		o.Reason = coverageErrorMessage(res.Err)
	} else if res.Truncated {
		o.Reason = "page limit reached"
	}
	return o
}

// buildAWSMetadata returns the metadata["osiris.aws"] coverage member.
// core_complete is true only when every core operation read all items.
// unverified_pagination lists the attempted service families whose
// collectors read a single response without verified pagination; for
// ec2 this applies to every operation outside core_operations. Their
// results may be incomplete in large accounts.
func buildAWSMetadata(outcomes []operationOutcome, attempted []string) awsMetadata {
	sorted := make([]operationOutcome, len(outcomes))
	copy(sorted, outcomes)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Service != sorted[j].Service {
			return sorted[i].Service < sorted[j].Service
		}
		return sorted[i].Operation < sorted[j].Operation
	})
	complete := len(sorted) > 0
	for _, o := range sorted {
		if !o.complete() {
			complete = false
		}
	}
	unverified := make([]string, len(attempted))
	copy(unverified, attempted)
	sort.Strings(unverified)
	return awsMetadata{
		Coverage: map[string]any{
			"schema":                coverageSchema,
			"core_complete":         complete,
			"core_operations":       sorted,
			"unverified_pagination": unverified,
		},
	}
}

// coreOperationComplete reports whether the core operation named op was
// read completely.
func coreOperationComplete(outcomes []operationOutcome, op string) bool {
	for _, o := range outcomes {
		if o.Operation == op {
			return o.complete()
		}
	}
	return false
}

// normalizeStatuses replaces every resource or connection status that
// is not an OSIRIS JSON schema value with "unknown" and returns the
// number replaced. An empty status is left empty.
func normalizeStatuses(resources []sdk.Resource, conns []sdk.Connection) int {
	n := 0
	for i := range resources {
		if s := resources[i].Status; s != "" && sdk.ValidateStatus(s) != nil {
			resources[i].Status = "unknown"
			n++
		}
	}
	for i := range conns {
		if s := conns[i].Status; s != "" && sdk.ValidateStatus(s) != nil {
			conns[i].Status = "unknown"
			n++
		}
	}
	return n
}
