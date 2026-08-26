// transform_hub.go - Hub/connectivity resource transforms: Bastion,
// Traffic Manager, DNS Private Resolver, DNS Forwarding Ruleset.
//
// For an introduction to OSIRIS JSON Producer for Microsoft Azure see:
// [OSIRIS-JSON-AZURE]: https://docs.osirisjson.org/osiris-producers/hyperscalers/microsoft-azure/
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package azure

import (
	"fmt"
	"strings"

	"go.osirisjson.org/producers/pkg/sdk"
)

// TransformBastionHosts converts Microsoft.Network/bastionHosts into OSIRIS JSON resources.
func TransformBastionHosts(hosts []BastionHost, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(hosts))

	for _, h := range hosts {
		id := resourceID("osiris.azure.bastion", h.ID)
		idMap[h.ID] = id

		prov := azureProvider(h.ID, "Microsoft.Network/bastionHosts", h.Location, sub)

		r, err := sdk.NewResource(id, "osiris.azure.bastion", prov)
		if err != nil {
			continue
		}
		r.Name = h.Name
		r.Status = mapProvisioningState(h.ProvisioningState)
		r.State = h.ProvisioningState
		r.Tags = h.Tags

		props := map[string]any{
			"resource_group": h.ResourceGroup,
		}
		if h.SKU.Name != "" {
			props["sku"] = h.SKU.Name
		}
		if h.DNSName != "" {
			props["dns_name"] = h.DNSName
		}
		if h.ScaleUnits > 0 {
			props["scale_units"] = h.ScaleUnits
		}
		if h.EnableTunneling {
			props["enable_tunneling"] = true
		}
		if h.EnableIpConnect {
			props["enable_ip_connect"] = true
		}
		if h.DisableCopyPaste {
			props["disable_copy_paste"] = true
		}
		if h.EnableShareableLink {
			props["enable_shareable_link"] = true
		}
		if h.EnableKerberos {
			props["enable_kerberos"] = true
		}
		r.Properties = props
		attachArmBody(&r, &h)
		resources = append(resources, r)
	}
	return resources, idMap
}

// BuildBastionIDMap builds ARM ID -> OSIRIS JSON resources ID map for Bastion hosts.
func BuildBastionIDMap(hosts []BastionHost) map[string]string {
	m := make(map[string]string, len(hosts))
	for _, h := range hosts {
		m[h.ID] = resourceID("osiris.azure.bastion", h.ID)
	}
	return m
}

// TransformBastionToSubnetConnections wires each Bastion host to its AzureBastionSubnet.
func TransformBastionToSubnetConnections(hosts []BastionHost, bastionIDMap, subnetIDMap map[string]string) []sdk.Connection {
	subnetLower := make(map[string]string, len(subnetIDMap))
	for k, v := range subnetIDMap {
		subnetLower[strings.ToLower(k)] = v
	}

	var connections []sdk.Connection
	for _, h := range hosts {
		sourceID, ok := bastionIDMap[h.ID]
		if !ok {
			continue
		}
		for _, ipc := range h.IPConfigurations {
			if ipc.Subnet == nil || ipc.Subnet.ID == "" {
				continue
			}
			targetID, ok := subnetLower[strings.ToLower(ipc.Subnet.ID)]
			if !ok {
				continue
			}
			canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
				Type:      "network",
				Direction: "forward",
				Source:    sourceID,
				Target:    targetID,
			})
			connID := sdk.BuildConnectionID(canonicalKey, 16)
			conn, err := sdk.NewConnection(connID, "network", sourceID, targetID)
			if err != nil {
				continue
			}
			conn.Name = fmt.Sprintf("%s -> %s", h.Name, extractLastSegment(ipc.Subnet.ID))
			_ = conn.SetDirection("forward")
			connections = append(connections, conn)
		}
	}
	return connections
}

// TransformBastionToPublicIPConnections wires each Bastion host to its public IP.
func TransformBastionToPublicIPConnections(hosts []BastionHost, bastionIDMap, publicIPIDMap map[string]string) []sdk.Connection {
	pipLower := make(map[string]string, len(publicIPIDMap))
	for k, v := range publicIPIDMap {
		pipLower[strings.ToLower(k)] = v
	}

	var connections []sdk.Connection
	for _, h := range hosts {
		sourceID, ok := bastionIDMap[h.ID]
		if !ok {
			continue
		}
		for _, ipc := range h.IPConfigurations {
			if ipc.PublicIPAddress == nil || ipc.PublicIPAddress.ID == "" {
				continue
			}
			targetID, ok := pipLower[strings.ToLower(ipc.PublicIPAddress.ID)]
			if !ok {
				continue
			}
			canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
				Type:      "network",
				Direction: "forward",
				Source:    sourceID,
				Target:    targetID,
			})
			connID := sdk.BuildConnectionID(canonicalKey, 16)
			conn, err := sdk.NewConnection(connID, "network", sourceID, targetID)
			if err != nil {
				continue
			}
			conn.Name = fmt.Sprintf("%s -> %s", h.Name, extractLastSegment(ipc.PublicIPAddress.ID))
			_ = conn.SetDirection("forward")
			connections = append(connections, conn)
		}
	}
	return connections
}

// TransformTrafficManagerProfiles converts Microsoft.Network/trafficManagerProfiles
// into OSIRIS JSON resources.
func TransformTrafficManagerProfiles(profiles []TrafficManagerProfile, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(profiles))

	for _, p := range profiles {
		id := resourceID("osiris.azure.trafficmanager", p.ID)
		idMap[p.ID] = id

		// Traffic Manager is a global service; location from ARM is always "global".
		prov := azureProvider(p.ID, "Microsoft.Network/trafficManagerProfiles", p.Location, sub)

		r, err := sdk.NewResource(id, "osiris.azure.trafficmanager", prov)
		if err != nil {
			continue
		}
		r.Name = p.Name
		r.Tags = p.Tags

		r.State = p.ProfileStatus
		if strings.EqualFold(p.ProfileStatus, "Enabled") {
			r.Status = "active"
		} else {
			r.Status = "inactive"
		}

		props := map[string]any{
			"resource_group":         p.ResourceGroup,
			"traffic_routing_method": p.TrafficRoutingMethod,
		}
		if p.DNSConfig.FQDN != "" {
			props["fqdn"] = p.DNSConfig.FQDN
		}
		if p.DNSConfig.TTL > 0 {
			props["dns_ttl"] = p.DNSConfig.TTL
		}
		if p.MonitorConfig.Protocol != "" {
			monitor := map[string]any{
				"protocol": p.MonitorConfig.Protocol,
				"port":     p.MonitorConfig.Port,
			}
			if p.MonitorConfig.Path != "" {
				monitor["path"] = p.MonitorConfig.Path
			}
			if p.MonitorConfig.IntervalInSeconds > 0 {
				monitor["interval_seconds"] = p.MonitorConfig.IntervalInSeconds
			}
			if p.MonitorConfig.ToleratedNumberOfFailures > 0 {
				monitor["tolerated_failures"] = p.MonitorConfig.ToleratedNumberOfFailures
			}
			if p.MonitorConfig.TimeoutInSeconds > 0 {
				monitor["timeout_seconds"] = p.MonitorConfig.TimeoutInSeconds
			}
			if p.MonitorConfig.ProfileMonitorStatus != "" {
				monitor["status"] = p.MonitorConfig.ProfileMonitorStatus
			}
			props["monitor"] = monitor
		}
		if len(p.Endpoints) > 0 {
			type endpointEntry struct {
				Name     string `json:"name"`
				Type     string `json:"type,omitempty"`
				Status   string `json:"status,omitempty"`
				Target   string `json:"target,omitempty"`
				Weight   int    `json:"weight,omitempty"`
				Priority int    `json:"priority,omitempty"`
				Location string `json:"location,omitempty"`
			}
			entries := make([]endpointEntry, 0, len(p.Endpoints))
			for _, ep := range p.Endpoints {
				e := endpointEntry{
					Name:     ep.Name,
					Status:   ep.EndpointStatus,
					Weight:   ep.Weight,
					Priority: ep.Priority,
					Location: ep.EndpointLocation,
				}
				// Simplify type: strip prefix up to last slash.
				if ep.Type != "" {
					e.Type = extractLastSegment(ep.Type)
				}
				// For external endpoints Target is the IP/FQDN; for Azure endpoints it is the resource FQDN.
				if ep.Target != "" {
					e.Target = ep.Target
				}
				entries = append(entries, e)
			}
			props["endpoints"] = entries
		}
		r.Properties = props
		attachArmBody(&r, &p)
		resources = append(resources, r)
	}
	return resources, idMap
}

// BuildTrafficManagerIDMap builds ARM ID -> OSIRIS JSON resources ID map for Traffic Manager profiles.
func BuildTrafficManagerIDMap(profiles []TrafficManagerProfile) map[string]string {
	m := make(map[string]string, len(profiles))
	for _, p := range profiles {
		m[p.ID] = resourceID("osiris.azure.trafficmanager", p.ID)
	}
	return m
}

// TransformTrafficManagerToTargetConnections wires Traffic Manager profiles to
// the Azure resources they front (Azure endpoints only - external/IP endpoints have
// no resolvable OSIRIS JSON resources).
func TransformTrafficManagerToTargetConnections(profiles []TrafficManagerProfile, tmIDMap, allResourceIDMap map[string]string) []sdk.Connection {
	targetLower := make(map[string]string, len(allResourceIDMap))
	for k, v := range allResourceIDMap {
		targetLower[strings.ToLower(k)] = v
	}

	var connections []sdk.Connection
	for _, p := range profiles {
		sourceID, ok := tmIDMap[p.ID]
		if !ok {
			continue
		}
		for _, ep := range p.Endpoints {
			if ep.TargetResourceID == "" {
				continue
			}
			targetID, ok := targetLower[strings.ToLower(ep.TargetResourceID)]
			if !ok {
				continue
			}
			canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
				Type:      "dependency",
				Direction: "forward",
				Source:    sourceID,
				Target:    targetID,
			})
			connID := sdk.BuildConnectionID(canonicalKey, 16)
			conn, err := sdk.NewConnection(connID, "dependency", sourceID, targetID)
			if err != nil {
				continue
			}
			conn.Name = fmt.Sprintf("%s -> %s", p.Name, ep.Name)
			_ = conn.SetDirection("forward")
			connections = append(connections, conn)
		}
	}
	return connections
}

// TransformDNSPrivateResolvers converts Microsoft.Network/dnsResolvers into OSIRIS JSON resources.
func TransformDNSPrivateResolvers(resolvers []DNSPrivateResolver, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(resolvers))

	for _, res := range resolvers {
		id := resourceID("osiris.azure.dns.resolver", res.ID)
		idMap[res.ID] = id

		prov := azureProvider(res.ID, "Microsoft.Network/dnsResolvers", res.Location, sub)

		r, err := sdk.NewResource(id, "osiris.azure.dns.resolver", prov)
		if err != nil {
			continue
		}
		r.Name = res.Name
		r.Tags = res.Tags

		props := map[string]any{
			"resource_group": res.ResourceGroup,
		}
		if p := res.Properties; p != nil {
			r.Status = mapProvisioningState(p.ProvisioningState)
			r.State = p.ProvisioningState
			if p.DNSResolverState != "" {
				props["dns_resolver_state"] = p.DNSResolverState
			}
			if p.VirtualNetwork != nil && p.VirtualNetwork.ID != "" {
				props["virtual_network_id"] = p.VirtualNetwork.ID
			}
		} else {
			r.Status = "active"
			r.State = "Succeeded"
		}
		r.Properties = props
		attachArmBody(&r, &res)
		resources = append(resources, r)
	}
	return resources, idMap
}

// BuildDNSResolverIDMap builds ARM ID -> OSIRIS JSON resources ID map for DNS private resolvers.
func BuildDNSResolverIDMap(resolvers []DNSPrivateResolver) map[string]string {
	m := make(map[string]string, len(resolvers))
	for _, r := range resolvers {
		m[r.ID] = resourceID("osiris.azure.dns.resolver", r.ID)
	}
	return m
}

// TransformDNSResolverToVNetConnections wires each DNS private resolver to its bound VNet.
func TransformDNSResolverToVNetConnections(resolvers []DNSPrivateResolver, resolverIDMap, vnetIDMap map[string]string) []sdk.Connection {
	vnetLower := make(map[string]string, len(vnetIDMap))
	for k, v := range vnetIDMap {
		vnetLower[strings.ToLower(k)] = v
	}

	var connections []sdk.Connection
	for _, res := range resolvers {
		if res.Properties == nil || res.Properties.VirtualNetwork == nil || res.Properties.VirtualNetwork.ID == "" {
			continue
		}
		sourceID, ok := resolverIDMap[res.ID]
		if !ok {
			continue
		}
		targetID, ok := vnetLower[strings.ToLower(res.Properties.VirtualNetwork.ID)]
		if !ok {
			continue
		}
		canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
			Type:      "network",
			Direction: "forward",
			Source:    sourceID,
			Target:    targetID,
		})
		connID := sdk.BuildConnectionID(canonicalKey, 16)
		conn, err := sdk.NewConnection(connID, "network", sourceID, targetID)
		if err != nil {
			continue
		}
		conn.Name = fmt.Sprintf("%s -> %s", res.Name, extractLastSegment(res.Properties.VirtualNetwork.ID))
		_ = conn.SetDirection("forward")
		connections = append(connections, conn)
	}
	return connections
}

// TransformDNSForwardingRulesets converts Microsoft.Network/dnsForwardingRulesets into OSIRIS JSON resources.
func TransformDNSForwardingRulesets(rulesets []DNSForwardingRuleset, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(rulesets))

	for _, rs := range rulesets {
		id := resourceID("osiris.azure.dns.forwardingruleset", rs.ID)
		idMap[rs.ID] = id

		prov := azureProvider(rs.ID, "Microsoft.Network/dnsForwardingRulesets", rs.Location, sub)

		r, err := sdk.NewResource(id, "osiris.azure.dns.forwardingruleset", prov)
		if err != nil {
			continue
		}
		r.Name = rs.Name
		r.Tags = rs.Tags

		props := map[string]any{
			"resource_group": rs.ResourceGroup,
		}
		if p := rs.Properties; p != nil {
			r.Status = mapProvisioningState(p.ProvisioningState)
			r.State = p.ProvisioningState
			if len(p.DNSResolverOutboundEndpoints) > 0 {
				epIDs := make([]string, 0, len(p.DNSResolverOutboundEndpoints))
				for _, ep := range p.DNSResolverOutboundEndpoints {
					if ep.ID != "" {
						epIDs = append(epIDs, ep.ID)
					}
				}
				if len(epIDs) > 0 {
					props["outbound_endpoint_ids"] = epIDs
				}
			}
		} else {
			r.Status = "active"
			r.State = "Succeeded"
		}
		r.Properties = props
		attachArmBody(&r, &rs)
		resources = append(resources, r)
	}
	return resources, idMap
}

// BuildDNSRulesetIDMap builds ARM ID -> OSIRIS JSON resources ID map for DNS forwarding rulesets.
func BuildDNSRulesetIDMap(rulesets []DNSForwardingRuleset) map[string]string {
	m := make(map[string]string, len(rulesets))
	for _, rs := range rulesets {
		m[rs.ID] = resourceID("osiris.azure.dns.forwardingruleset", rs.ID)
	}
	return m
}

// TransformDNSRulesetToResolverConnections wires each DNS forwarding ruleset to the
// DNS private resolver(s) it is attached to via outbound endpoints.
// The outbound endpoint ARM ID encodes the resolver: strip "/outboundEndpoints/{name}"
// from the endpoint ID to recover the resolver ARM ID.
func TransformDNSRulesetToResolverConnections(rulesets []DNSForwardingRuleset, rulesetIDMap, resolverIDMap map[string]string) []sdk.Connection {
	resolverLower := make(map[string]string, len(resolverIDMap))
	for k, v := range resolverIDMap {
		resolverLower[strings.ToLower(k)] = v
	}

	var connections []sdk.Connection
	seen := map[string]bool{}

	for _, rs := range rulesets {
		sourceID, ok := rulesetIDMap[rs.ID]
		if !ok {
			continue
		}
		if rs.Properties == nil {
			continue
		}
		for _, ep := range rs.Properties.DNSResolverOutboundEndpoints {
			if ep.ID == "" {
				continue
			}
			lower := strings.ToLower(ep.ID)
			idx := strings.LastIndex(lower, "/outboundendpoints/")
			if idx < 0 {
				continue
			}
			resolverARM := lower[:idx]

			dedup := sourceID + "|" + resolverARM
			if seen[dedup] {
				continue
			}
			seen[dedup] = true

			targetID, ok := resolverLower[resolverARM]
			if !ok {
				continue
			}
			canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
				Type:      "dependency",
				Direction: "forward",
				Source:    sourceID,
				Target:    targetID,
			})
			connID := sdk.BuildConnectionID(canonicalKey, 16)
			conn, err := sdk.NewConnection(connID, "dependency", sourceID, targetID)
			if err != nil {
				continue
			}
			conn.Name = fmt.Sprintf("%s -> %s", rs.Name, extractLastSegment(resolverARM))
			_ = conn.SetDirection("forward")
			connections = append(connections, conn)
		}
	}
	return connections
}
