// transform_integration.go - Integration resource and connection transforms (Service Bus, Event Hubs, APIM, Front Door).
//
// For an introduction to OSIRIS JSON Producer for Microsoft Azure see:
// [OSIRIS-JSON-AZURE]: https://osirisjson.org/en/docs/producers/hyperscalers/microsoft-azure
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/docs/spec/v10/00-preface

package azure

import (
	"fmt"

	"go.osirisjson.org/producers/pkg/sdk"
)

// TransformServiceBusNamespaces converts Microsoft.ServiceBus/namespaces into
// OSIRIS JSON resources of type osiris.azure.servicebus.namespace. Queue topic and subscription enumeration is out of scope.
func TransformServiceBusNamespaces(namespaces []ServiceBusNamespace, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	return transformMessagingNamespaces(
		"osiris.azure.servicebus.namespace",
		"Microsoft.ServiceBus/namespaces",
		messagingIterServiceBus(namespaces),
		sub,
	)
}

// TransformEventHubsNamespaces converts Microsoft.EventHub/namespaces into
// OSIRIS JSON resources of type osiris.azure.eventhubs.namespace.
func TransformEventHubsNamespaces(namespaces []EventHubsNamespace, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	return transformMessagingNamespaces(
		"osiris.azure.eventhubs.namespace",
		"Microsoft.EventHub/namespaces",
		messagingIterEventHubs(namespaces),
		sub,
	)
}

// messagingNamespaceView unifies ServiceBus + EventHubs namespace iteration for the shared transform body.
type messagingNamespaceView struct {
	ID            string
	Name          string
	Location      string
	ResourceGroup string
	Tags          map[string]string
	SKU           *azMessagingSKU
	Properties    *azMessagingProperties
}

func messagingIterServiceBus(namespaces []ServiceBusNamespace) func(yield func(messagingNamespaceView)) {
	return func(yield func(messagingNamespaceView)) {
		for _, n := range namespaces {
			yield(messagingNamespaceView{
				ID:            n.ID,
				Name:          n.Name,
				Location:      n.Location,
				ResourceGroup: n.ResourceGroup,
				Tags:          n.Tags,
				SKU:           n.SKU,
				Properties:    n.Properties,
			})
		}
	}
}

func messagingIterEventHubs(namespaces []EventHubsNamespace) func(yield func(messagingNamespaceView)) {
	return func(yield func(messagingNamespaceView)) {
		for _, n := range namespaces {
			yield(messagingNamespaceView{
				ID:            n.ID,
				Name:          n.Name,
				Location:      n.Location,
				ResourceGroup: n.ResourceGroup,
				Tags:          n.Tags,
				SKU:           n.SKU,
				Properties:    n.Properties,
			})
		}
	}
}

func transformMessagingNamespaces(osirisType, nativeType string, iter func(yield func(messagingNamespaceView)), sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string)

	iter(func(n messagingNamespaceView) {
		id := resourceID(osirisType, n.ID)
		idMap[n.ID] = id

		prov := azureProvider(n.ID, nativeType, n.Location, sub)

		r, err := sdk.NewResource(id, osirisType, prov)
		if err != nil {
			return
		}
		r.Name = n.Name
		r.Tags = n.Tags

		props := map[string]any{
			"resource_group": n.ResourceGroup,
		}
		if n.SKU != nil {
			if n.SKU.Name != "" {
				props["sku_name"] = n.SKU.Name
			}
			if n.SKU.Tier != "" {
				props["sku_tier"] = n.SKU.Tier
			}
			if n.SKU.Capacity > 0 {
				props["capacity"] = n.SKU.Capacity
			}
		}
		ext := map[string]any{}
		if p := n.Properties; p != nil {
			r.Status = mapProvisioningState(p.ProvisioningState)
			r.State = p.ProvisioningState
			if p.ServiceBusEndpoint != "" {
				props["endpoint"] = p.ServiceBusEndpoint
			}
			if p.ZoneRedundant {
				props["zone_redundant"] = true
			}
			if p.DisableLocalAuth {
				props["entra_only"] = true
			}
			if p.PublicNetworkAccess != "" {
				props["public_network_access"] = p.PublicNetworkAccess
			}
			if p.MinimumTLSVersion != "" {
				props["minimum_tls_version"] = p.MinimumTLSVersion
				ext["min_tls_version"] = p.MinimumTLSVersion
			}
			if peIDs := collectPEIDs(p.PrivateEndpointConnections); len(peIDs) > 0 {
				ext["private_endpoint_connection_ids"] = peIDs
			}
		} else {
			r.Status = "active"
			r.State = "Succeeded"
		}
		r.Properties = props
		if len(ext) > 0 {
			r.Extensions = map[string]any{extensionNamespace: ext}
		}

		attachArmBody(&r, &n)
		resources = append(resources, r)
	})
	return resources, idMap
}

// TransformAPIMServices converts Microsoft.ApiManagement/service into OSIRIS JSON resources of type osiris.azure.apim.
// Individual APIs, operations, products, and policy documents are out of scope.
func TransformAPIMServices(services []APIMService, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(services))

	for _, s := range services {
		id := resourceID("osiris.azure.apim", s.ID)
		idMap[s.ID] = id

		prov := azureProvider(s.ID, "Microsoft.ApiManagement/service", s.Location, sub)

		r, err := sdk.NewResource(id, "osiris.azure.apim", prov)
		if err != nil {
			continue
		}
		r.Name = s.Name
		r.Tags = s.Tags

		props := map[string]any{
			"resource_group": s.ResourceGroup,
		}
		if s.SKU != nil {
			if s.SKU.Name != "" {
				props["sku_name"] = s.SKU.Name
			}
			if s.SKU.Capacity > 0 {
				props["capacity"] = s.SKU.Capacity
			}
		}
		if p := s.Properties; p != nil {
			r.Status = mapProvisioningState(p.ProvisioningState)
			r.State = p.ProvisioningState
			if p.GatewayURL != "" {
				props["gateway_url"] = p.GatewayURL
			}
			if p.PortalURL != "" {
				props["portal_url"] = p.PortalURL
			}
			if p.ManagementURL != "" {
				props["management_url"] = p.ManagementURL
			}
			if p.VirtualNetworkType != "" {
				props["virtual_network_type"] = p.VirtualNetworkType
			}
			if p.VirtualNetworkConfiguration != nil && p.VirtualNetworkConfiguration.SubnetResourceID != "" {
				props["subnet_id"] = p.VirtualNetworkConfiguration.SubnetResourceID
			}
			if len(p.PublicIPAddresses) > 0 {
				props["public_ip_addresses"] = p.PublicIPAddresses
			}
			if len(p.PrivateIPAddresses) > 0 {
				props["private_ip_addresses"] = p.PrivateIPAddresses
			}
			if p.PublicNetworkAccess != "" {
				props["public_network_access"] = p.PublicNetworkAccess
			}
			if p.DisableGateway {
				props["disable_gateway"] = true
			}
			if peIDs := collectPEIDs(p.PrivateEndpointConnections); len(peIDs) > 0 {
				r.Extensions = map[string]any{extensionNamespace: map[string]any{"private_endpoint_connection_ids": peIDs}}
			}
		} else {
			r.Status = "active"
			r.State = "Succeeded"
		}
		r.Properties = props

		attachArmBody(&r, &s)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformFrontDoorProfiles converts Microsoft.Cdn/profiles entries with an Azure Front Door SKU (Standard / Premium)
// into OSIRIS JSON resources of type osiris.azure.frontdoor.profile. Routes, rules, and WAF policies are out of scope.
func TransformFrontDoorProfiles(profiles []FrontDoorProfile, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(profiles))

	for _, fp := range profiles {
		id := resourceID("osiris.azure.frontdoor.profile", fp.ID)
		idMap[fp.ID] = id

		prov := azureProvider(fp.ID, "Microsoft.Cdn/profiles", fp.Location, sub)

		r, err := sdk.NewResource(id, "osiris.azure.frontdoor.profile", prov)
		if err != nil {
			continue
		}
		r.Name = fp.Name
		r.Tags = fp.Tags

		props := map[string]any{
			"resource_group": fp.ResourceGroup,
		}
		if fp.Kind != "" {
			props["kind"] = fp.Kind
		}
		if fp.SKU != nil && fp.SKU.Name != "" {
			props["sku_name"] = fp.SKU.Name
		}
		if p := fp.Properties; p != nil {
			r.Status = mapProvisioningState(p.ProvisioningState)
			r.State = p.ProvisioningState
			if p.ResourceState != "" {
				props["resource_state"] = p.ResourceState
			}
			if p.FrontDoorID != "" {
				props["front_door_id"] = p.FrontDoorID
			}
		} else {
			r.Status = "active"
			r.State = "Succeeded"
		}
		r.Properties = props

		attachArmBody(&r, &fp)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformPEToServiceBusConnections wires Private Endpoints to Service Bus namespaces (Premium tier only).
func TransformPEToServiceBusConnections(namespaces []ServiceBusNamespace, sbIDMap, peIDMap map[string]string) []sdk.Connection {
	bindings := make([]peBinding, 0, len(namespaces))
	for _, n := range namespaces {
		if n.Properties == nil {
			continue
		}
		bindings = append(bindings, peBinding{
			TargetArmID: n.ID,
			Name:        n.Name,
			Conns:       n.Properties.PrivateEndpointConnections,
		})
	}
	return transformPEBoundConnections(bindings, sbIDMap, peIDMap, "dependency")
}

// TransformPEToEventHubsConnections wires Private Endpoints to Event Hubs namespaces (Standard/Premium tiers).
func TransformPEToEventHubsConnections(namespaces []EventHubsNamespace, ehIDMap, peIDMap map[string]string) []sdk.Connection {
	bindings := make([]peBinding, 0, len(namespaces))
	for _, n := range namespaces {
		if n.Properties == nil {
			continue
		}
		bindings = append(bindings, peBinding{
			TargetArmID: n.ID,
			Name:        n.Name,
			Conns:       n.Properties.PrivateEndpointConnections,
		})
	}
	return transformPEBoundConnections(bindings, ehIDMap, peIDMap, "dependency")
}

// TransformAPIMToSubnetConnections emits `network` edges from each VNet-integrated APIM service (External / Internal mode) to its subnet.
func TransformAPIMToSubnetConnections(services []APIMService, apimIDMap, subnetIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, s := range services {
		if s.Properties == nil || s.Properties.VirtualNetworkConfiguration == nil {
			continue
		}
		subnetArmID := s.Properties.VirtualNetworkConfiguration.SubnetResourceID
		if subnetArmID == "" {
			continue
		}
		srcID, ok := apimIDMap[s.ID]
		if !ok {
			continue
		}
		dstID, ok := subnetIDMap[subnetArmID]
		if !ok {
			continue
		}
		canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
			Type:      "network",
			Direction: "forward",
			Source:    srcID,
			Target:    dstID,
		})
		connID := sdk.BuildConnectionID(canonicalKey, 16)
		conn, err := sdk.NewConnection(connID, "network", srcID, dstID)
		if err != nil {
			continue
		}
		conn.Name = fmt.Sprintf("%s -> %s", s.Name, extractLastSegment(subnetArmID))
		_ = conn.SetDirection("forward")
		connections = append(connections, conn)
	}
	return connections
}

// TransformStreamAnalyticsJobs converts Microsoft.StreamAnalytics/streamingjobs into
// OSIRIS JSON resources of type osiris.azure.streamanalytics.
func TransformStreamAnalyticsJobs(jobs []StreamAnalyticsJob, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(jobs))

	for _, j := range jobs {
		id := resourceID("osiris.azure.streamanalytics", j.ID)
		idMap[j.ID] = id

		prov := azureProvider(j.ID, "Microsoft.StreamAnalytics/streamingjobs", j.Location, sub)

		r, err := sdk.NewResource(id, "osiris.azure.streamanalytics", prov)
		if err != nil {
			continue
		}
		r.Name = j.Name
		r.Tags = j.Tags

		props := map[string]any{
			"resource_group": j.ResourceGroup,
		}
		if j.SKU != nil && j.SKU.Name != "" {
			props["sku"] = j.SKU.Name
		}
		if p := j.Properties; p != nil {
			r.Status = mapStreamAnalyticsState(p.JobState)
			r.State = p.JobState
			if p.CompatibilityLevel != "" {
				props["compatibility_level"] = p.CompatibilityLevel
			}
			if p.OutputStartMode != "" {
				props["output_start_mode"] = p.OutputStartMode
			}
		} else {
			r.Status = "active"
			r.State = "Running"
		}
		r.Properties = props

		attachArmBody(&r, &j)
		resources = append(resources, r)
	}
	return resources, idMap
}

func mapStreamAnalyticsState(state string) string {
	switch state {
	case "Running":
		return "active"
	case "Stopped", "Degraded", "Failed":
		return "inactive"
	case "Deleting", "Deleted":
		return "terminated"
	default:
		return "active"
	}
}

// TransformEventGridSystemTopics converts Microsoft.EventGrid/systemTopics into
// OSIRIS JSON resources of type osiris.azure.eventgrid.systemtopic.
func TransformEventGridSystemTopics(topics []EventGridSystemTopic, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(topics))

	for _, t := range topics {
		id := resourceID("osiris.azure.eventgrid.systemtopic", t.ID)
		idMap[t.ID] = id

		prov := azureProvider(t.ID, "Microsoft.EventGrid/systemTopics", t.Location, sub)

		r, err := sdk.NewResource(id, "osiris.azure.eventgrid.systemtopic", prov)
		if err != nil {
			continue
		}
		r.Name = t.Name
		r.Tags = t.Tags

		props := map[string]any{
			"resource_group": t.ResourceGroup,
		}
		if p := t.Properties; p != nil {
			r.Status = mapProvisioningState(p.ProvisioningState)
			r.State = p.ProvisioningState
			if p.TopicType != "" {
				props["topic_type"] = p.TopicType
			}
			if p.Source != "" {
				props["source"] = p.Source
			}
		} else {
			r.Status = "active"
			r.State = "Succeeded"
		}
		r.Properties = props

		attachArmBody(&r, &t)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformPEToAPIMConnections wires Private Endpoints to APIM services.
func TransformPEToAPIMConnections(services []APIMService, apimIDMap, peIDMap map[string]string) []sdk.Connection {
	bindings := make([]peBinding, 0, len(services))
	for _, s := range services {
		if s.Properties == nil {
			continue
		}
		bindings = append(bindings, peBinding{
			TargetArmID: s.ID,
			Name:        s.Name,
			Conns:       s.Properties.PrivateEndpointConnections,
		})
	}
	return transformPEBoundConnections(bindings, apimIDMap, peIDMap, "dependency")
}
