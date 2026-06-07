// transform_containers.go - Container resource and connection transforms (AKS, Container App, Container Group).
//
// For an introduction to OSIRIS JSON Producer for Microsoft Azure see:
// [OSIRIS-JSON-AZURE]: https://osirisjson.org/en/docs/producers/hyperscalers/microsoft-azure
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/docs/spec/v10/00-preface

package azure

import (
	"fmt"
	"strings"

	"go.osirisjson.org/producers/pkg/sdk"
)

// TransformAKSClusters converts Microsoft.ContainerService/managedClusters into
// OSIRIS JSON resources of type osiris.azure.aks.cluster. Policy-like fields
// (admission configs, audit settings, identity profiles) are omitted per the topology-vs-IaC rule.
func TransformAKSClusters(clusters []AKSCluster, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(clusters))

	for _, c := range clusters {
		id := resourceID("osiris.azure.aks.cluster", c.ID)
		idMap[c.ID] = id

		prov := azureProvider(c.ID, "Microsoft.ContainerService/managedClusters", c.Location, sub)

		r, err := sdk.NewResource(id, "osiris.azure.aks.cluster", prov)
		if err != nil {
			continue
		}
		r.Name = c.Name
		r.Tags = c.Tags

		props := map[string]any{
			"resource_group": c.ResourceGroup,
		}
		if c.SKU != nil {
			if c.SKU.Name != "" {
				props["sku_name"] = c.SKU.Name
			}
			if c.SKU.Tier != "" {
				props["sku_tier"] = c.SKU.Tier
			}
		}
		if p := c.Properties; p != nil {
			r.Status = mapProvisioningState(p.ProvisioningState)
			r.State = p.ProvisioningState
			if p.KubernetesVersion != "" {
				props["kubernetes_version"] = p.KubernetesVersion
			}
			if p.DNSPrefix != "" {
				props["dns_prefix"] = p.DNSPrefix
			}
			if p.FQDN != "" {
				props["fqdn"] = p.FQDN
			}
			if p.NodeResourceGroup != "" {
				props["node_resource_group"] = p.NodeResourceGroup
			}
			props["enable_rbac"] = p.EnableRBAC
			if p.NetworkProfile != nil {
				np := map[string]any{}
				if p.NetworkProfile.NetworkPlugin != "" {
					np["network_plugin"] = p.NetworkProfile.NetworkPlugin
				}
				if p.NetworkProfile.NetworkPolicy != "" {
					np["network_policy"] = p.NetworkProfile.NetworkPolicy
				}
				if p.NetworkProfile.ServiceCIDR != "" {
					np["service_cidr"] = p.NetworkProfile.ServiceCIDR
				}
				if p.NetworkProfile.PodCIDR != "" {
					np["pod_cidr"] = p.NetworkProfile.PodCIDR
				}
				if p.NetworkProfile.DNSServiceIP != "" {
					np["dns_service_ip"] = p.NetworkProfile.DNSServiceIP
				}
				if p.NetworkProfile.LoadBalancerSKU != "" {
					np["load_balancer_sku"] = p.NetworkProfile.LoadBalancerSKU
				}
				if p.NetworkProfile.OutboundType != "" {
					np["outbound_type"] = p.NetworkProfile.OutboundType
				}
				if len(np) > 0 {
					props["network_profile"] = np
				}
			}
			if p.APIServerAccessProfile != nil {
				props["private_cluster"] = p.APIServerAccessProfile.EnablePrivateCluster
				if p.APIServerAccessProfile.PrivateDNSZone != "" {
					props["private_dns_zone"] = p.APIServerAccessProfile.PrivateDNSZone
				}
			}
			if p.AADProfile != nil && p.AADProfile.Managed {
				props["aad_managed"] = true
				props["aad_azure_rbac"] = p.AADProfile.EnableAzureRBAC
			}
			props["agent_pool_count"] = len(c.AgentPools)
			if peIDs := collectPEIDs(p.PrivateEndpointConnections); len(peIDs) > 0 {
				r.Extensions = map[string]any{extensionNamespace: map[string]any{"private_endpoint_connection_ids": peIDs}}
			}
		} else {
			r.Status = "active"
			r.State = "Succeeded"
		}
		r.Properties = props

		attachArmBody(&r, &c)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformAKSAgentPools converts AKS agent pools into OSIRIS JSON resources of
// type osiris.azure.aks.nodepool. Pools are collected per-cluster so that each
// carries its ARM ID (needed for cluster -> nodepool contains edges).
func TransformAKSAgentPools(clusters []AKSCluster, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string)

	for _, c := range clusters {
		for _, p := range c.AgentPools {
			id := resourceID("osiris.azure.aks.nodepool", p.ID)
			idMap[p.ID] = id

			prov := azureProvider(p.ID, "Microsoft.ContainerService/managedClusters/agentPools", c.Location, sub)
			if len(p.AvailabilityZones) > 0 {
				prov.Zone = strings.Join(p.AvailabilityZones, ",")
			}
			r, err := sdk.NewResource(id, "osiris.azure.aks.nodepool", prov)
			if err != nil {
				continue
			}
			r.Name = p.Name
			r.Status = mapProvisioningState(p.ProvisioningState)
			r.State = p.ProvisioningState

			props := map[string]any{
				"cluster_name": c.Name,
				"cluster_id":   p.ClusterID,
			}
			if p.VMSize != "" {
				props["vm_size"] = p.VMSize
			}
			props["count"] = p.Count
			if p.EnableAutoScaling {
				props["autoscale"] = true
				props["min_count"] = p.MinCount
				props["max_count"] = p.MaxCount
			}
			if p.OSType != "" {
				props["os_type"] = p.OSType
			}
			if p.OSSKU != "" {
				props["os_sku"] = p.OSSKU
			}
			if p.Mode != "" {
				props["mode"] = p.Mode
			}
			if p.OrchestratorVer != "" {
				props["orchestrator_version"] = p.OrchestratorVer
			}
			if p.VNetSubnetID != "" {
				props["vnet_subnet_id"] = p.VNetSubnetID
			}
			if p.PodSubnetID != "" {
				props["pod_subnet_id"] = p.PodSubnetID
			}
			if len(p.AvailabilityZones) > 0 {
				props["availability_zones"] = p.AvailabilityZones
			}
			r.Properties = props

			attachArmBody(&r, &p)
			resources = append(resources, r)
		}
	}
	return resources, idMap
}

// TransformContainerAppEnvironments converts Microsoft.App/managedEnvironments
// into OSIRIS JSON resources of type osiris.azure.containerapp.environment.
func TransformContainerAppEnvironments(envs []ContainerAppEnvironment, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(envs))

	for _, e := range envs {
		id := resourceID("osiris.azure.containerapp.environment", e.ID)
		idMap[e.ID] = id

		prov := azureProvider(e.ID, "Microsoft.App/managedEnvironments", e.Location, sub)

		r, err := sdk.NewResource(id, "osiris.azure.containerapp.environment", prov)
		if err != nil {
			continue
		}
		r.Name = e.Name
		r.Tags = e.Tags

		props := map[string]any{
			"resource_group": e.ResourceGroup,
		}
		if p := e.Properties; p != nil {
			r.Status = mapProvisioningState(p.ProvisioningState)
			r.State = p.ProvisioningState
			if p.DefaultDomain != "" {
				props["default_domain"] = p.DefaultDomain
			}
			if p.StaticIP != "" {
				props["static_ip"] = p.StaticIP
			}
			if p.ZoneRedundant {
				props["zone_redundant"] = true
			}
			if p.VNetConfiguration != nil {
				if p.VNetConfiguration.InfrastructureSubnetID != "" {
					props["infrastructure_subnet_id"] = p.VNetConfiguration.InfrastructureSubnetID
				}
				props["internal"] = p.VNetConfiguration.Internal
			}
		} else {
			r.Status = "active"
			r.State = "Succeeded"
		}
		r.Properties = props

		attachArmBody(&r, &e)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformContainerApps converts Microsoft.App/containerApps into OSIRIS JSON
// resources of type osiris.azure.containerapp. Ingress secrets and revision history are excluded as per OSIRIS JSON spec chapter 13.
func TransformContainerApps(apps []ContainerApp, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(apps))

	for _, a := range apps {
		id := resourceID("osiris.azure.containerapp", a.ID)
		idMap[a.ID] = id

		prov := azureProvider(a.ID, "Microsoft.App/containerApps", a.Location, sub)

		r, err := sdk.NewResource(id, "osiris.azure.containerapp", prov)
		if err != nil {
			continue
		}
		r.Name = a.Name
		r.Tags = a.Tags

		props := map[string]any{
			"resource_group": a.ResourceGroup,
		}
		if envID := a.EnvironmentID(); envID != "" {
			props["environment_id"] = envID
		}
		if p := a.Properties; p != nil {
			r.Status = mapProvisioningState(p.ProvisioningState)
			r.State = p.ProvisioningState
			if p.LatestRevisionName != "" {
				props["latest_revision_name"] = p.LatestRevisionName
			}
			if p.LatestRevisionFQDN != "" {
				props["latest_revision_fqdn"] = p.LatestRevisionFQDN
			}
			if p.WorkloadProfileName != "" {
				props["workload_profile_name"] = p.WorkloadProfileName
			}
			if p.Configuration != nil {
				if p.Configuration.ActiveRevisionsMode != "" {
					props["active_revisions_mode"] = p.Configuration.ActiveRevisionsMode
				}
				if ing := p.Configuration.Ingress; ing != nil {
					ingress := map[string]any{
						"external":       ing.External,
						"allow_insecure": ing.AllowInsecure,
					}
					if ing.TargetPort > 0 {
						ingress["target_port"] = ing.TargetPort
					}
					if ing.Transport != "" {
						ingress["transport"] = ing.Transport
					}
					if ing.FQDN != "" {
						ingress["fqdn"] = ing.FQDN
					}
					props["ingress"] = ingress
				}
			}
		} else {
			r.Status = "active"
			r.State = "Succeeded"
		}
		r.Properties = props

		attachArmBody(&r, &a)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformContainerGroups converts Microsoft.ContainerInstance/containerGroups
// (ACI) into OSIRIS JSON resources of type osiris.azure.containergroup.
// Container-level config (images, env vars, commands) is intentionally omitted as topology models the group, not the workload.
func TransformContainerGroups(groups []ContainerGroup, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(groups))

	for _, g := range groups {
		id := resourceID("osiris.azure.containergroup", g.ID)
		idMap[g.ID] = id

		prov := azureProvider(g.ID, "Microsoft.ContainerInstance/containerGroups", g.Location, sub)

		r, err := sdk.NewResource(id, "osiris.azure.containergroup", prov)
		if err != nil {
			continue
		}
		r.Name = g.Name
		r.Tags = g.Tags

		props := map[string]any{
			"resource_group": g.ResourceGroup,
		}
		if p := g.Properties; p != nil {
			r.Status = mapProvisioningState(p.ProvisioningState)
			r.State = p.ProvisioningState
			if p.OSType != "" {
				props["os_type"] = p.OSType
			}
			if p.RestartPolicy != "" {
				props["restart_policy"] = p.RestartPolicy
			}
			if p.Sku != "" {
				props["sku"] = p.Sku
			}
			if ip := p.IPAddress; ip != nil {
				addr := map[string]any{}
				if ip.IP != "" {
					addr["ip"] = ip.IP
				}
				if ip.Type != "" {
					addr["type"] = ip.Type
				}
				if ip.FQDN != "" {
					addr["fqdn"] = ip.FQDN
				}
				if ip.DNSLabel != "" {
					addr["dns_label"] = ip.DNSLabel
				}
				if len(addr) > 0 {
					props["ip_address"] = addr
				}
			}
			props["container_count"] = len(p.Containers)
		} else {
			r.Status = "active"
			r.State = "Succeeded"
		}
		r.Properties = props

		attachArmBody(&r, &g)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformAKSClusterContainsAgentPoolConnections emits `contains` edges from each AKS cluster to its agent pools.
func TransformAKSClusterContainsAgentPoolConnections(clusters []AKSCluster, clusterIDMap, poolIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, c := range clusters {
		srcID, ok := clusterIDMap[c.ID]
		if !ok {
			continue
		}
		for _, p := range c.AgentPools {
			dstID, ok := poolIDMap[p.ID]
			if !ok {
				continue
			}
			canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
				Type:      "contains",
				Direction: "forward",
				Source:    srcID,
				Target:    dstID,
			})
			connID := sdk.BuildConnectionID(canonicalKey, 16)
			conn, err := sdk.NewConnection(connID, "contains", srcID, dstID)
			if err != nil {
				continue
			}
			conn.Name = fmt.Sprintf("%s contains %s", c.Name, p.Name)
			_ = conn.SetDirection("forward")
			connections = append(connections, conn)
		}
	}
	return connections
}

// TransformAKSNodePoolToSubnetConnections emits `network` edges from each AKS
// agent pool to its delegated VNet subnet. Pools without a subnet (kubenet + managed VNet) are silently skipped.
func TransformAKSNodePoolToSubnetConnections(clusters []AKSCluster, poolIDMap, subnetIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, c := range clusters {
		for _, p := range c.AgentPools {
			if p.VNetSubnetID == "" {
				continue
			}
			srcID, ok := poolIDMap[p.ID]
			if !ok {
				continue
			}
			dstID, ok := subnetIDMap[p.VNetSubnetID]
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
			conn.Name = fmt.Sprintf("%s -> %s", p.Name, extractLastSegment(p.VNetSubnetID))
			_ = conn.SetDirection("forward")
			connections = append(connections, conn)
		}
	}
	return connections
}

// TransformPEToAKSClusterConnections wires Private Endpoints to AKS clusters
// via the cluster's properties.privateEndpointConnections (private cluster only).
func TransformPEToAKSClusterConnections(clusters []AKSCluster, aksIDMap, peIDMap map[string]string) []sdk.Connection {
	bindings := make([]peBinding, 0, len(clusters))
	for _, c := range clusters {
		if c.Properties == nil {
			continue
		}
		bindings = append(bindings, peBinding{
			TargetArmID: c.ID,
			Name:        c.Name,
			Conns:       c.Properties.PrivateEndpointConnections,
		})
	}
	return transformPEBoundConnections(bindings, aksIDMap, peIDMap, "dependency")
}

// TransformContainerEnvContainsAppConnections emits `contains` edges from each
// managed environment to the container apps that live inside it.
func TransformContainerEnvContainsAppConnections(apps []ContainerApp, envIDMap, appIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, a := range apps {
		envArmID := a.EnvironmentID()
		if envArmID == "" {
			continue
		}
		srcID, ok := envIDMap[envArmID]
		if !ok {
			continue
		}
		dstID, ok := appIDMap[a.ID]
		if !ok {
			continue
		}
		canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
			Type:      "contains",
			Direction: "forward",
			Source:    srcID,
			Target:    dstID,
		})
		connID := sdk.BuildConnectionID(canonicalKey, 16)
		conn, err := sdk.NewConnection(connID, "contains", srcID, dstID)
		if err != nil {
			continue
		}
		conn.Name = fmt.Sprintf("%s contains %s", extractLastSegment(envArmID), a.Name)
		_ = conn.SetDirection("forward")
		connections = append(connections, conn)
	}
	return connections
}

// TransformContainerEnvToSubnetConnections emits `network` edges from each
// managed environment to its infrastructure subnet. Non-VNet-integrated environments are silently skipped.
func TransformContainerEnvToSubnetConnections(envs []ContainerAppEnvironment, envIDMap, subnetIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, e := range envs {
		if e.Properties == nil || e.Properties.VNetConfiguration == nil {
			continue
		}
		subnetArmID := e.Properties.VNetConfiguration.InfrastructureSubnetID
		if subnetArmID == "" {
			continue
		}
		srcID, ok := envIDMap[e.ID]
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
		conn.Name = fmt.Sprintf("%s -> %s", e.Name, extractLastSegment(subnetArmID))
		_ = conn.SetDirection("forward")
		connections = append(connections, conn)
	}
	return connections
}

// TransformContainerGroupToSubnetConnections emits `network` edges from each
// VNet-integrated ACI container group to each of its subnet references.
func TransformContainerGroupToSubnetConnections(groups []ContainerGroup, cgIDMap, subnetIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, g := range groups {
		if g.Properties == nil || len(g.Properties.SubnetIDs) == 0 {
			continue
		}
		srcID, ok := cgIDMap[g.ID]
		if !ok {
			continue
		}
		for _, ref := range g.Properties.SubnetIDs {
			if ref.ID == "" {
				continue
			}
			dstID, ok := subnetIDMap[ref.ID]
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
			conn.Name = fmt.Sprintf("%s -> %s", g.Name, extractLastSegment(ref.ID))
			_ = conn.SetDirection("forward")
			connections = append(connections, conn)
		}
	}
	return connections
}
