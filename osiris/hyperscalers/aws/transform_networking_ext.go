// transform_networking_ext.go - extended networking resource transforms.
// Maps ELBv2 listeners, Direct Connect LAGs, VPC PrivateLink endpoint service
// configurations, API Gateway REST APIs (v1), API Gateway v2 APIs, and
// CloudFront distributions to OSIRIS JSON types.
//
// All types are custom osiris.aws.* namespace:
//   elbv2:listener                   -> osiris.aws.elbv2.listener
//   directconnect:lag                -> osiris.aws.directconnect.lag
//   ec2:endpointservice              -> osiris.aws.vpc.endpointservice
//   apigateway:restapi               -> osiris.aws.apigateway.restapi
//   apigatewayv2:api                 -> osiris.aws.apigatewayv2.api
//   cloudfront:distribution          -> osiris.aws.cloudfront.distribution
//
// [OSIRIS-JSON-AWS]: https://osirisjson.org/en/docs/producers/hyperscalers/amazon-aws
// [OSIRIS-JSON-SPEC-CH07]: https://osirisjson.org/en/docs/spec/v10/07-resourcetypetaxonomy

package aws

import (
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	apigwtypes "github.com/aws/aws-sdk-go-v2/service/apigateway/types"
	apigwv2types "github.com/aws/aws-sdk-go-v2/service/apigatewayv2/types"
	cftypes "github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
	dctypes "github.com/aws/aws-sdk-go-v2/service/directconnect/types"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"go.osirisjson.org/producers/pkg/sdk"
)

// TransformELBv2Listeners converts ELBv2 listener descriptions to
// osiris.aws.elbv2.listener resources.
// Returns resources and a listenerARN->resourceID map.
func TransformELBv2Listeners(listeners []elbv2types.Listener, region, accountID string) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(listeners))

	for _, l := range listeners {
		arn := aws.ToString(l.ListenerArn)
		if arn == "" {
			continue
		}
		id := resourceIDFromARN(arn)
		idMap[arn] = id

		proto := string(l.Protocol)
		port := int32(0)
		if l.Port != nil {
			port = aws.ToInt32(l.Port)
		}
		name := fmt.Sprintf("%s:%d", proto, port)

		prov := awsProvider(arn, "elbv2:listener", region, accountID)
		r, err := sdk.NewResource(id, "osiris.aws.elbv2.listener", prov)
		if err != nil {
			continue
		}
		r.Name = name
		r.Status = "active"

		props := map[string]any{
			"protocol": proto,
		}
		if l.Port != nil {
			props["port"] = port
		}
		if l.SslPolicy != nil && aws.ToString(l.SslPolicy) != "" {
			props["ssl_policy"] = aws.ToString(l.SslPolicy)
		}
		if len(l.AlpnPolicy) > 0 {
			props["alpn_policy"] = l.AlpnPolicy
		}
		if len(l.DefaultActions) > 0 {
			actionTypes := make([]string, 0, len(l.DefaultActions))
			for _, a := range l.DefaultActions {
				actionTypes = append(actionTypes, string(a.Type))
			}
			props["default_action_types"] = actionTypes
		}

		r.Properties = props
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformDirectConnectLAGs converts Direct Connect LAG descriptions to
// osiris.aws.directconnect.lag resources.
// Returns resources and a lagID->resourceID map.
func TransformDirectConnectLAGs(lags []dctypes.Lag, region, accountID string) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(lags))

	for _, lag := range lags {
		lagID := aws.ToString(lag.LagId)
		if lagID == "" {
			continue
		}
		id := resourceID(accountID, region, "directconnect", "lag/"+lagID)
		idMap[lagID] = id

		name := aws.ToString(lag.LagName)
		if name == "" {
			name = lagID
		}
		prov := awsProvider(lagID, "directconnect:lag", region, accountID)
		r, err := sdk.NewResource(id, "osiris.aws.directconnect.lag", prov)
		if err != nil {
			continue
		}
		r.Name = name
		r.Status = mapDCLAGState(lag.LagState)

		props := map[string]any{
			"number_of_connections": lag.NumberOfConnections,
			"minimum_links":         lag.MinimumLinks,
		}
		if lag.ConnectionsBandwidth != nil {
			props["connections_bandwidth"] = aws.ToString(lag.ConnectionsBandwidth)
		}
		if lag.Location != nil {
			props["location"] = aws.ToString(lag.Location)
		}
		if lag.HasLogicalRedundancy != "" {
			props["has_logical_redundancy"] = string(lag.HasLogicalRedundancy)
		}
		if lag.EncryptionMode != nil {
			props["encryption_mode"] = aws.ToString(lag.EncryptionMode)
		}

		r.Properties = props
		if len(lag.Tags) > 0 {
			r.Tags = tagMapDX(lag.Tags)
		}
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformVPCEndpointServices converts VPC endpoint service configurations to
// osiris.aws.vpc.endpointservice resources.
// Returns resources and a serviceID->resourceID map.
func TransformVPCEndpointServices(svcs []ec2types.ServiceConfiguration, region, accountID string) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(svcs))

	for _, svc := range svcs {
		svcID := aws.ToString(svc.ServiceId)
		if svcID == "" {
			continue
		}
		id := resourceID(accountID, region, "ec2", "vpc-endpoint-service/"+svcID)
		idMap[svcID] = id

		name := aws.ToString(svc.ServiceName)
		prov := awsProvider(svcID, "ec2:vpc-endpoint-service", region, accountID)
		r, err := sdk.NewResource(id, "osiris.aws.vpc.endpointservice", prov)
		if err != nil {
			continue
		}
		r.Name = name
		r.Status = mapEndpointServiceState(svc.ServiceState)

		props := map[string]any{}
		if len(svc.ServiceType) > 0 {
			types := make([]string, len(svc.ServiceType))
			for i, t := range svc.ServiceType {
				types[i] = string(t.ServiceType)
			}
			props["service_types"] = types
		}
		if svc.AcceptanceRequired != nil {
			props["acceptance_required"] = aws.ToBool(svc.AcceptanceRequired)
		}
		if svc.ManagesVpcEndpoints != nil {
			props["manages_vpc_endpoints"] = aws.ToBool(svc.ManagesVpcEndpoints)
		}
		if svc.PrivateDnsName != nil && aws.ToString(svc.PrivateDnsName) != "" {
			props["private_dns_name"] = aws.ToString(svc.PrivateDnsName)
		}
		if len(svc.NetworkLoadBalancerArns) > 0 {
			props["network_load_balancer_arns"] = svc.NetworkLoadBalancerArns
		}
		if len(svc.GatewayLoadBalancerArns) > 0 {
			props["gateway_load_balancer_arns"] = svc.GatewayLoadBalancerArns
		}

		r.Properties = props
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformAPIGatewayRestAPIs converts API Gateway REST API descriptions to
// osiris.aws.apigateway.restapi resources.
func TransformAPIGatewayRestAPIs(apis []apigwtypes.RestApi, region, accountID string) []sdk.Resource {
	var resources []sdk.Resource

	for _, api := range apis {
		apiID := aws.ToString(api.Id)
		if apiID == "" {
			continue
		}
		arn := fmt.Sprintf("arn:aws:apigateway:%s::/restapis/%s", region, apiID)
		id := resourceIDFromARN(arn)

		name := aws.ToString(api.Name)
		prov := awsProvider(arn, "apigateway:rest-api", region, accountID)
		r, err := sdk.NewResource(id, "osiris.aws.apigateway.restapi", prov)
		if err != nil {
			continue
		}
		r.Name = name
		r.Status = "active"

		props := map[string]any{}
		if api.Description != nil && aws.ToString(api.Description) != "" {
			props["description"] = aws.ToString(api.Description)
		}
		if api.EndpointConfiguration != nil && len(api.EndpointConfiguration.Types) > 0 {
			endpointTypes := make([]string, len(api.EndpointConfiguration.Types))
			for i, t := range api.EndpointConfiguration.Types {
				endpointTypes[i] = string(t)
			}
			props["endpoint_types"] = endpointTypes
		}
		if api.DisableExecuteApiEndpoint {
			props["disable_execute_api_endpoint"] = true
		}
		if api.Version != nil && aws.ToString(api.Version) != "" {
			props["version"] = aws.ToString(api.Version)
		}
		if len(api.Tags) > 0 {
			r.Tags = sanitizeTags(api.Tags)
		}

		r.Properties = props
		resources = append(resources, r)
	}
	return resources
}

// TransformAPIGatewayV2APIs converts API Gateway v2 API descriptions to
// osiris.aws.apigatewayv2.api resources.
func TransformAPIGatewayV2APIs(apis []apigwv2types.Api, region, accountID string) []sdk.Resource {
	var resources []sdk.Resource

	for _, api := range apis {
		apiID := aws.ToString(api.ApiId)
		if apiID == "" {
			continue
		}
		arn := fmt.Sprintf("arn:aws:apigateway:%s::/apis/%s", region, apiID)
		id := resourceIDFromARN(arn)

		name := aws.ToString(api.Name)
		prov := awsProvider(arn, "apigatewayv2:api", region, accountID)
		r, err := sdk.NewResource(id, "osiris.aws.apigatewayv2.api", prov)
		if err != nil {
			continue
		}
		r.Name = name
		r.Status = "active"

		props := map[string]any{
			"protocol_type": string(api.ProtocolType),
		}
		if api.ApiEndpoint != nil && aws.ToString(api.ApiEndpoint) != "" {
			props["api_endpoint"] = aws.ToString(api.ApiEndpoint)
		}
		if api.Description != nil && aws.ToString(api.Description) != "" {
			props["description"] = aws.ToString(api.Description)
		}
		if api.RouteSelectionExpression != nil {
			props["route_selection_expression"] = aws.ToString(api.RouteSelectionExpression)
		}
		if api.Version != nil && aws.ToString(api.Version) != "" {
			props["version"] = aws.ToString(api.Version)
		}

		r.Properties = props
		resources = append(resources, r)
	}
	return resources
}

// TransformCloudFrontDistributions converts CloudFront distribution summaries to
// osiris.aws.cloudfront.distribution resources.
func TransformCloudFrontDistributions(dists []cftypes.DistributionSummary, accountID string) []sdk.Resource {
	var resources []sdk.Resource

	for _, d := range dists {
		arn := aws.ToString(d.ARN)
		distID := aws.ToString(d.Id)
		if distID == "" {
			continue
		}
		nativeID := arn
		if nativeID == "" {
			nativeID = fmt.Sprintf("arn:aws:cloudfront::%s:distribution/%s", accountID, distID)
		}
		id := resourceIDFromARN(nativeID)

		prov := awsProvider(nativeID, "cloudfront:distribution", "global", accountID)
		r, err := sdk.NewResource(id, "osiris.aws.cloudfront.distribution", prov)
		if err != nil {
			continue
		}
		r.Name = distID
		r.Status = mapCloudFrontStatus(aws.ToString(d.Status), d.Enabled)

		props := map[string]any{}
		if d.DomainName != nil {
			props["domain_name"] = aws.ToString(d.DomainName)
		}
		if d.Enabled != nil {
			props["enabled"] = aws.ToBool(d.Enabled)
		}
		if d.HttpVersion != "" {
			props["http_version"] = string(d.HttpVersion)
		}
		if d.PriceClass != "" {
			props["price_class"] = string(d.PriceClass)
		}
		if d.IsIPV6Enabled != nil {
			props["is_ipv6_enabled"] = aws.ToBool(d.IsIPV6Enabled)
		}
		if d.Comment != nil && aws.ToString(d.Comment) != "" {
			props["comment"] = aws.ToString(d.Comment)
		}
		if d.Origins != nil {
			props["origin_count"] = aws.ToInt32(d.Origins.Quantity)
		}
		if d.WebACLId != nil && aws.ToString(d.WebACLId) != "" {
			props["web_acl_id"] = aws.ToString(d.WebACLId)
		}
		if d.Aliases != nil && d.Aliases.Quantity != nil && aws.ToInt32(d.Aliases.Quantity) > 0 {
			props["alias_count"] = aws.ToInt32(d.Aliases.Quantity)
		}

		r.Properties = props
		resources = append(resources, r)
	}
	return resources
}


// TransformListenerToLBConnections wires ELBv2 listeners to their parent load balancers.
func TransformListenerToLBConnections(listeners []elbv2types.Listener, lbs []elbv2types.LoadBalancer, listenerIDMap map[string]string, region, accountID string) []sdk.Connection {
	lbMap := make(map[string]string, len(lbs))
	for _, lb := range lbs {
		arn := aws.ToString(lb.LoadBalancerArn)
		if arn != "" {
			lbMap[arn] = resourceIDFromARN(arn)
		}
	}

	var conns []sdk.Connection
	for _, l := range listeners {
		lArn := aws.ToString(l.ListenerArn)
		listenerID, ok := listenerIDMap[lArn]
		if !ok {
			continue
		}
		lbArn := aws.ToString(l.LoadBalancerArn)
		lbID, ok := lbMap[lbArn]
		if !ok {
			continue
		}
		conn := makeConn("contains", "forward", lbID, listenerID,
			fmt.Sprintf("LB %s contains listener %s", lbArn, lArn))
		if conn != nil {
			conns = append(conns, *conn)
		}
	}
	return conns
}

// TransformLAGContainsConnectionConns wires Direct Connect LAGs to their bundled connections.
func TransformLAGContainsConnectionConns(lags []dctypes.Lag, lagIDMap map[string]string, region, accountID string) []sdk.Connection {
	var conns []sdk.Connection
	for _, lag := range lags {
		lagID := aws.ToString(lag.LagId)
		srcID, ok := lagIDMap[lagID]
		if !ok {
			continue
		}
		for _, conn := range lag.Connections {
			connID := aws.ToString(conn.ConnectionId)
			if connID == "" {
				continue
			}
			tgtID := resourceID(accountID, region, "directconnect", "connection/"+connID)
			c := makeConn("contains", "forward", srcID, tgtID,
				fmt.Sprintf("LAG %s contains connection %s", lagID, connID))
			if c != nil {
				conns = append(conns, *c)
			}
		}
	}
	return conns
}

// TransformEndpointServiceToNLBConnections wires VPC endpoint services to their NLBs.
func TransformEndpointServiceToNLBConnections(svcs []ec2types.ServiceConfiguration, svcIDMap map[string]string, lbs []elbv2types.LoadBalancer, region, accountID string) []sdk.Connection {
	lbMap := make(map[string]string, len(lbs))
	for _, lb := range lbs {
		arn := aws.ToString(lb.LoadBalancerArn)
		if arn != "" {
			lbMap[arn] = resourceIDFromARN(arn)
		}
	}

	var conns []sdk.Connection
	for _, svc := range svcs {
		svcID := aws.ToString(svc.ServiceId)
		srcID, ok := svcIDMap[svcID]
		if !ok {
			continue
		}
		for _, nlbArn := range svc.NetworkLoadBalancerArns {
			tgtID, ok := lbMap[nlbArn]
			if !ok {
				continue
			}
			c := makeConn("network", "forward", srcID, tgtID,
				fmt.Sprintf("endpoint service %s -> NLB %s", svcID, nlbArn))
			if c != nil {
				conns = append(conns, *c)
			}
		}
	}
	return conns
}


func mapDCLAGState(s dctypes.LagState) string {
	switch s {
	case dctypes.LagStateAvailable:
		return "active"
	case dctypes.LagStateRequested, dctypes.LagStatePending:
		return "pending"
	case dctypes.LagStateDeleted, dctypes.LagStateDeleting:
		return "inactive"
	case dctypes.LagStateDown:
		return "degraded"
	default:
		return "unknown"
	}
}

func mapEndpointServiceState(s ec2types.ServiceState) string {
	switch s {
	case ec2types.ServiceStateAvailable:
		return "active"
	case ec2types.ServiceStatePending:
		return "pending"
	case ec2types.ServiceStateDeleted, ec2types.ServiceStateDeleting:
		return "inactive"
	case ec2types.ServiceStateFailed:
		return "inactive"
	default:
		return "unknown"
	}
}

func mapCloudFrontStatus(status string, enabled *bool) string {
	if enabled != nil && !aws.ToBool(enabled) {
		return "inactive"
	}
	switch status {
	case "Deployed":
		return "active"
	case "InProgress":
		return "pending"
	default:
		return "unknown"
	}
}
