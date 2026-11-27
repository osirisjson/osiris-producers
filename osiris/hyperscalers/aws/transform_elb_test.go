// transform_elb_test.go - Tests for load balancer, listener and target
// group properties AWS does not define for every type, in the OSIRIS
// JSON Amazon AWS producer.
//
// For an introduction to OSIRIS JSON Producer for Amazon AWS see:
// [OSIRIS-JSON-AWS]: https://docs.osirisjson.org/osiris-producers/hyperscalers/amazon-aws/

package aws

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"

	"go.osirisjson.org/producers/pkg/sdk"
)

func TestELBv2UndefinedProperties(t *testing.T) {
	const (
		prefix  = "arn:aws:elasticloadbalancing:eu-west-1:123456789012:"
		region  = "eu-west-1"
		account = "123456789012"
	)
	absent := func(r sdk.Resource, keys ...string) {
		t.Helper()
		for _, k := range keys {
			if v, ok := r.Properties[k]; ok {
				t.Errorf("%s: %s = %v, want omitted", r.Type, k, v)
			}
		}
	}

	lbs := TransformLoadBalancersV2([]elbv2types.LoadBalancer{
		{LoadBalancerArn: aws.String(prefix + "loadbalancer/gwy/TEST-GWLB/1"), Type: elbv2types.LoadBalancerTypeEnumGateway},
		{LoadBalancerArn: aws.String(prefix + "loadbalancer/app/TEST-ALB/2"), Type: elbv2types.LoadBalancerTypeEnumApplication, Scheme: elbv2types.LoadBalancerSchemeEnumInternal},
	}, region, account)
	absent(lbs[0], "scheme")
	if lbs[1].Properties["scheme"] != "internal" {
		t.Errorf("scheme = %v, want internal", lbs[1].Properties["scheme"])
	}

	listeners, _ := TransformELBv2Listeners([]elbv2types.Listener{
		{ListenerArn: aws.String(prefix + "listener/gwy/TEST-GWLB/1/10")},
		{ListenerArn: aws.String(prefix + "listener/app/TEST-ALB/2/20"), Protocol: elbv2types.ProtocolEnumHttps, Port: aws.Int32(443)},
	}, region, account)
	absent(listeners[0], "protocol", "port")
	if listeners[0].Name != "10" || listeners[1].Name != "HTTPS:443" || listeners[1].Properties["protocol"] != "HTTPS" {
		t.Errorf("listeners = %q, %q %v", listeners[0].Name, listeners[1].Name, listeners[1].Properties)
	}

	groups := TransformTargetGroups([]elbv2types.TargetGroup{
		{TargetGroupArn: aws.String(prefix + "targetgroup/TEST-LAMBDA/1"), TargetType: elbv2types.TargetTypeEnumLambda},
		{TargetGroupArn: aws.String(prefix + "targetgroup/TEST-WEB/2"), Protocol: elbv2types.ProtocolEnumHttp, Port: aws.Int32(80)},
	}, region, account)
	absent(groups[0], "protocol", "port")
	if groups[1].Properties["protocol"] != "HTTP" || groups[1].Properties["port"] != int32(80) {
		t.Errorf("target group = %v", groups[1].Properties)
	}
}
