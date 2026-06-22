// transform_events.go - event-driven and integration resource transforms.
// Maps SNS topics, EventBridge buses, and Step Functions state machines to
// OSIRIS JSON types.
//
// All types are custom osiris.aws.* namespace:
//   sns:topic                      -> osiris.aws.sns.topic
//   eventbridge:bus                -> osiris.aws.eventbridge.bus
//   sfn:statemachine               -> osiris.aws.stepfunctions.statemachine
//
// [OSIRIS-JSON-AWS]: https://osirisjson.org/en/docs/producers/hyperscalers/amazon-aws
// [OSIRIS-JSON-SPEC-CH07]: https://osirisjson.org/en/docs/spec/v10/07-resourcetypetaxonomy

package aws

import (
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	ebtypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	sfntypes "github.com/aws/aws-sdk-go-v2/service/sfn/types"
	snstypes "github.com/aws/aws-sdk-go-v2/service/sns/types"
	"go.osirisjson.org/producers/pkg/sdk"
)

// TransformSNSTopics converts SNS topic ARN entries to osiris.aws.sns.topic resources.
func TransformSNSTopics(topics []snstypes.Topic, region, accountID string) []sdk.Resource {
	var resources []sdk.Resource

	for _, t := range topics {
		arn := aws.ToString(t.TopicArn)
		if arn == "" {
			continue
		}
		id := resourceIDFromARN(arn)

		// Topic name is the last segment of the ARN.
		parts := strings.Split(arn, ":")
		name := parts[len(parts)-1]

		prov := awsProvider(arn, "sns:topic", region, accountID)
		r, err := sdk.NewResource(id, "osiris.aws.sns.topic", prov)
		if err != nil {
			continue
		}
		r.Name = name
		r.Status = "active"

		// SNS Topic struct only carries the ARN; all other attributes require a
		// separate GetTopicAttributes call. Store the ARN as the identifying property.
		r.Properties = map[string]any{
			"topic_arn": arn,
		}
		resources = append(resources, r)
	}
	return resources
}

// TransformEventBridgeBuses converts EventBridge event bus descriptions to
// osiris.aws.eventbridge.bus resources.
func TransformEventBridgeBuses(buses []ebtypes.EventBus, region, accountID string) []sdk.Resource {
	var resources []sdk.Resource

	for _, b := range buses {
		arn := aws.ToString(b.Arn)
		name := aws.ToString(b.Name)
		if name == "" {
			continue
		}
		nativeID := arn
		if nativeID == "" {
			nativeID = fmt.Sprintf("arn:aws:events:%s:%s:event-bus/%s", region, accountID, name)
		}
		id := resourceIDFromARN(nativeID)

		prov := awsProvider(nativeID, "events:event-bus", region, accountID)
		r, err := sdk.NewResource(id, "osiris.aws.eventbridge.bus", prov)
		if err != nil {
			continue
		}
		r.Name = name
		r.Status = "active"

		props := map[string]any{}
		if b.Description != nil && aws.ToString(b.Description) != "" {
			props["description"] = aws.ToString(b.Description)
		}
		if len(props) > 0 {
			r.Properties = props
		}
		resources = append(resources, r)
	}
	return resources
}

// TransformSFNStateMachines converts Step Functions state machine list items to
// osiris.aws.stepfunctions.statemachine resources.
func TransformSFNStateMachines(machines []sfntypes.StateMachineListItem, region, accountID string) []sdk.Resource {
	var resources []sdk.Resource

	for _, m := range machines {
		arn := aws.ToString(m.StateMachineArn)
		name := aws.ToString(m.Name)
		if arn == "" {
			continue
		}
		id := resourceIDFromARN(arn)

		prov := awsProvider(arn, "stepfunctions:state-machine", region, accountID)
		r, err := sdk.NewResource(id, "osiris.aws.stepfunctions.statemachine", prov)
		if err != nil {
			continue
		}
		r.Name = name
		r.Status = "active"

		props := map[string]any{
			"type": string(m.Type),
		}
		r.Properties = props
		resources = append(resources, r)
	}
	return resources
}
