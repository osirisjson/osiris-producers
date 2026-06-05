// transform_web.go - Web/App Service resource and connection transforms (App Service Plan, Web App, Function App).
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

// TransformAppServicePlans converts Azure App Service Plans (Microsoft.Web/serverfarms)
// into OSIRIS JSON resources. Returns resources and a map of ASP ARM ID -> resource ID
// so site->plan connections can be wired without an extra lookup.
func TransformAppServicePlans(plans []AppServicePlan, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(plans))

	for _, p := range plans {
		id := resourceID("osiris.azure.appserviceplan", p.ID)
		idMap[p.ID] = id

		prov := azureProvider(p.ID, "Microsoft.Web/serverfarms", p.Location, sub)

		r, err := sdk.NewResource(id, "osiris.azure.appserviceplan", prov)
		if err != nil {
			continue
		}
		r.Name = p.Name
		r.Status = mapAppServicePlanStatus(p.Status)
		r.State = p.Status
		r.Tags = p.Tags

		props := map[string]any{
			"resource_group": p.ResourceGroup,
		}
		if p.Kind != "" {
			props["kind"] = p.Kind
		}
		if p.SKU.Name != "" {
			props["sku"] = p.SKU.Name
		}
		if p.SKU.Tier != "" {
			props["sku_tier"] = p.SKU.Tier
		}
		if p.SKU.Size != "" {
			props["sku_size"] = p.SKU.Size
		}
		if p.SKU.Family != "" {
			props["sku_family"] = p.SKU.Family
		}
		if p.SKU.Capacity > 0 {
			props["sku_capacity"] = p.SKU.Capacity
		}
		if p.Reserved {
			props["linux"] = true
		}
		if p.PerSiteScaling {
			props["per_site_scaling"] = true
		}
		if p.ZoneRedundant {
			props["zone_redundant"] = true
		}
		if p.NumberOfWorkers > 0 {
			props["number_of_workers"] = p.NumberOfWorkers
		}
		if p.MaximumElasticWorkerCount > 0 {
			props["max_elastic_worker_count"] = p.MaximumElasticWorkerCount
		}
		if p.NumberOfSites > 0 {
			props["number_of_sites"] = p.NumberOfSites
		}
		r.Properties = props
		attachArmBody(&r, &p)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformWebApps converts Azure App Service sites (Microsoft.Web/sites) into
// OSIRIS JSON resources. Kind routing:
//
//	kind contains "functionapp" -> osiris.azure.functionapp
//	otherwise                   -> osiris.azure.webapp
//


	for _, a := range apps {
		osirisType := "osiris.azure.webapp"
		if a.IsFunctionApp() {
			osirisType = "osiris.azure.functionapp"
		}
		id := resourceID(osirisType, a.ID)
		idMap[a.ID] = id

		prov := azureProvider(a.ID, "Microsoft.Web/sites", a.Location, sub)

		r, err := sdk.NewResource(id, osirisType, prov)
		if err != nil {
			continue
		}
		r.Name = a.Name
		r.Status = mapWebAppState(a.State, a.Enabled)
		r.State = a.State
		r.Tags = a.Tags

		props := map[string]any{
			"resource_group": a.ResourceGroup,
		}
		if a.Kind != "" {
			props["kind"] = a.Kind
		}
		if a.State != "" {
			props["state"] = a.State
		}
		props["enabled"] = a.Enabled
		if a.DefaultHostName != "" {
			props["default_hostname"] = a.DefaultHostName
		}
		if len(a.HostNames) > 0 {
			props["hostnames"] = a.HostNames
		}
		props["https_only"] = a.HTTPSOnly
		if a.ClientCertEnabled {
			props["client_cert_enabled"] = true
			if a.ClientCertMode != "" {
				props["client_cert_mode"] = a.ClientCertMode
			}
		}
		if hp := a.HostPlanID(); hp != "" {
			props["host_plan_id"] = hp
		}
		if a.VirtualNetworkSubnetID != "" {
			props["vnet_integration_subnet_id"] = a.VirtualNetworkSubnetID
		}
		if a.PublicNetworkAccess != "" {
			props["public_network_access"] = a.PublicNetworkAccess
		}
		if a.InboundIPAddress != "" {
			props["inbound_ip"] = a.InboundIPAddress
		}
		if a.OutboundIPAddresses != "" {
			props["outbound_ips"] = splitCSV(a.OutboundIPAddresses)
		}
		if a.RedundancyMode != "" && strings.ToLower(a.RedundancyMode) != "none" {
			props["redundancy_mode"] = a.RedundancyMode
		}
		if a.ManagedEnvironmentID != "" {
			props["managed_environment_id"] = a.ManagedEnvironmentID
		}
		if cfg := a.SiteConfig; cfg != nil {
			if cfg.LinuxFxVersion != "" {
				props["linux_fx_version"] = cfg.LinuxFxVersion
			}
			if cfg.WindowsFxVersion != "" {
				props["windows_fx_version"] = cfg.WindowsFxVersion
			}
			if cfg.NumberOfWorkers > 0 {
				props["number_of_workers"] = cfg.NumberOfWorkers
			}
			if cfg.AlwaysOn {
				props["always_on"] = true
			}
			if cfg.HTTP20Enabled {
				props["http20_enabled"] = true
			}
			if cfg.MinTLSVersion != "" {
				props["min_tls_version"] = cfg.MinTLSVersion
			}
			if a.IsFunctionApp() && cfg.FunctionAppScaleLimit > 0 {
				props["function_scale_limit"] = cfg.FunctionAppScaleLimit
			}
			if cfg.MinimumElasticInstanceCount > 0 {
				props["min_elastic_instance_count"] = cfg.MinimumElasticInstanceCount
			}
			if cfg.ACRUseManagedIdentityCreds {
				props["acr_use_managed_identity"] = true
			}
		}
		r.Properties = props

		// Extensions: Azure-specific site fields (identity, VNet routing flags, PE connections).
		ext := map[string]any{}
		if id := a.Identity; id != nil && id.Type != "" {
			idMap := map[string]any{"type": id.Type}
			if id.PrincipalID != "" {
				idMap["principal_id"] = id.PrincipalID
			}
			if ids := id.UserAssignedIdentityIDs(); len(ids) > 0 {
				idMap["user_assigned_identity_ids"] = ids
			}
			ext["identity"] = idMap
		}
		if r := a.OutboundVnetRouting; r != nil {
			routing := map[string]any{}
			if r.AllTraffic {
				routing["all_traffic"] = true
			}
			if r.ApplicationTraffic {
				routing["application_traffic"] = true
			}
			if r.ContentShareTraffic {
				routing["content_share_traffic"] = true
			}
			if r.ImagePullTraffic {
				routing["image_pull_traffic"] = true
			}
			if r.BackupRestoreTraffic {
				routing["backup_restore_traffic"] = true
			}
			if r.ManagedIdentityTraffic {
				routing["managed_identity_traffic"] = true
			}
			if len(routing) > 0 {
				ext["outbound_vnet_routing"] = routing
			}
		}
		if len(a.PrivateEndpointConnections) > 0 {
			peIDs := make([]string, 0, len(a.PrivateEndpointConnections))
			for _, pec := range a.PrivateEndpointConnections {
				if peID := pec.PrivateEndpointID(); peID != "" {
					peIDs = append(peIDs, peID)
				}
			}
			if len(peIDs) > 0 {
				ext["private_endpoint_ids"] = peIDs
			}
		}
		if aiID := appInsightsFromTags(a.Tags); aiID != "" {
			ext["app_insights_id"] = aiID
		}
		if cfg := a.SiteConfig; cfg != nil && cfg.MinTLSVersion != "" {
			ext["min_tls_version"] = cfg.MinTLSVersion
		}
		if len(ext) > 0 {
			r.Extensions = map[string]any{extensionNamespace: ext}
		}

		attachArmBody(&r, &a)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformWebAppToPlanConnections creates contains connections from an App
// Service Plan to each of its hosted sites (web apps / function apps).
func TransformWebAppToPlanConnections(apps []WebApp, webAppIDMap, planIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, a := range apps {
		planArmID := a.HostPlanID()
		if planArmID == "" {
			continue
		}
		targetID, ok := webAppIDMap[a.ID]
		if !ok {
			continue
		}
		sourceID, ok := planIDMap[planArmID]
		if !ok {
			continue
		}

		canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
			Type:      "contains",
			Direction: "forward",
			Source:    sourceID,
			Target:    targetID,
		})
		connID := sdk.BuildConnectionID(canonicalKey, 16)

		conn, err := sdk.NewConnection(connID, "contains", sourceID, targetID)
		if err != nil {
			continue
		}
		conn.Name = fmt.Sprintf("%s -> %s", extractLastSegment(planArmID), a.Name)
		_ = conn.SetDirection("forward")
		connections = append(connections, conn)
	}
	return connections
}

// TransformWebAppToSubnetConnections creates network connections from a web/function app
// to its VNet integration subnet (regional VNet integration).
func TransformWebAppToSubnetConnections(apps []WebApp, webAppIDMap, subnetIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, a := range apps {
		if a.VirtualNetworkSubnetID == "" {
			continue
		}
		sourceID, ok := webAppIDMap[a.ID]
		if !ok {
			continue
		}
		targetID, ok := subnetIDMap[a.VirtualNetworkSubnetID]
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
		conn.Name = fmt.Sprintf("%s -> %s", a.Name, extractLastSegment(a.VirtualNetworkSubnetID))
		_ = conn.SetDirection("forward")
		connections = append(connections, conn)
	}
	return connections
}

// TransformPEToWebAppConnections creates network connections from a private
// endpoint to the web/function app it fronts. The binding lives on the site's
// privateEndpointConnections array.
func TransformPEToWebAppConnections(apps []WebApp, webAppIDMap, peIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, a := range apps {
		if len(a.PrivateEndpointConnections) == 0 {
			continue
		}
		targetID, ok := webAppIDMap[a.ID]
		if !ok {
			continue
		}
		for _, pec := range a.PrivateEndpointConnections {
			peArmID := pec.PrivateEndpointID()
			if peArmID == "" {
				continue
			}
			sourceID, ok := peIDMap[peArmID]
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
			conn.Name = fmt.Sprintf("%s -> %s", extractLastSegment(peArmID), a.Name)
			_ = conn.SetDirection("forward")
			connections = append(connections, conn)
		}
	}
	return connections
}

// TransformWebAppToAppInsightsConnections wires App Service / Function App
// sites to their bound Application Insights component. The binding is
// declared on the site via the Azure portal `hidden-link` tag, which is
// parsed once at WebApp transform time and reused here to emit the edge.
func TransformWebAppToAppInsightsConnections(webApps []WebApp, webAppIDMap, aiIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, w := range webApps {
		aiArm := appInsightsFromTags(w.Tags)
		if aiArm == "" {
			continue
		}
		sourceID, ok := webAppIDMap[w.ID]
		if !ok {
			continue
		}
		targetID, ok := aiIDMap[aiArm]
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
		conn.Name = fmt.Sprintf("%s -> %s", w.Name, extractLastSegment(aiArm))
		_ = conn.SetDirection("forward")
		connections = append(connections, conn)
	}
	return connections
}

// mapAppServicePlanStatus converts Azure App Service Plan status to OSIRIS JSON status.
func mapAppServicePlanStatus(status string) string {
	switch strings.ToLower(status) {
	case "ready":
		return "active"
	case "pending", "creating":
		return "degraded"
	default:
		return "unknown"
	}
}

// mapWebAppState converts Azure App Service (site) state + enabled flag to OSIRIS JSON status.
func mapWebAppState(state string, enabled bool) string {
	if !enabled {
		return "inactive"
	}
	switch strings.ToLower(state) {
	case "running":
		return "active"
	case "stopped":
		return "inactive"
	default:
		return "unknown"
	}
}

// splitCSV splits a comma-separated string into a trimmed, non-empty slice.
func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		v := strings.TrimSpace(p)
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

// TransformAppServiceSlots converts Microsoft.Web/sites/slots into OSIRIS JSON resources
// of type osiris.azure.webapp.slot.
func TransformAppServiceSlots(slots []AppServiceSlot, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(slots))

	for _, s := range slots {
		id := resourceID("osiris.azure.webapp.slot", s.ID)
		idMap[s.ID] = id

		prov := azureProvider(s.ID, "Microsoft.Web/sites/slots", s.Location, sub)

		r, err := sdk.NewResource(id, "osiris.azure.webapp.slot", prov)
		if err != nil {
			continue
		}
		r.Name = s.Name
		r.Status = mapWebAppState(s.State, s.Enabled)
		r.State = s.State
		r.Tags = s.Tags

		props := map[string]any{
			"resource_group": s.ResourceGroup,
		}
		if s.Kind != "" {
			props["kind"] = s.Kind
		}
		if s.DefaultHostName != "" {
			props["default_host_name"] = s.DefaultHostName
		}
		if cfg := s.SiteConfig; cfg != nil {
			if cfg.LinuxFxVersion != "" {
				props["linux_fx_version"] = cfg.LinuxFxVersion
			}
			if cfg.WindowsFxVersion != "" {
				props["windows_fx_version"] = cfg.WindowsFxVersion
			}
		}
		r.Properties = props

		attachArmBody(&r, &s)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformWebAppContainsSlotConnections wires each slot to its parent webapp
// with a contains/forward connection.
func TransformWebAppContainsSlotConnections(slots []AppServiceSlot, webAppIDMap, slotIDMap map[string]string) []sdk.Connection {
	webAppLower := make(map[string]string, len(webAppIDMap))
	for k, v := range webAppIDMap {
		webAppLower[strings.ToLower(k)] = v
	}

	var connections []sdk.Connection
	for _, s := range slots {
		if s.WebAppID == "" {
			continue
		}
		slotID, ok := slotIDMap[s.ID]
		if !ok {
			continue
		}
		parentID, ok := webAppLower[strings.ToLower(s.WebAppID)]
		if !ok {
			continue
		}
		canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
			Type:      "contains",
			Direction: "forward",
			Source:    parentID,
			Target:    slotID,
		})
		connID := sdk.BuildConnectionID(canonicalKey, 16)
		conn, err := sdk.NewConnection(connID, "contains", parentID, slotID)
		if err != nil {
			continue
		}
		conn.Name = fmt.Sprintf("%s -> slot:%s", extractLastSegment(s.WebAppID), s.Name)
		_ = conn.SetDirection("forward")
		connections = append(connections, conn)
	}
	return connections
}

// appInsightsFromTags extracts the Application Insights ARM ID from the Azure
// portal hidden-link tag (`hidden-link: /app-insights-resource-id`).
func appInsightsFromTags(tags map[string]string) string {
	for k, v := range tags {
		if strings.EqualFold(strings.TrimSpace(k), "hidden-link: /app-insights-resource-id") {
			return v
		}
	}
	return ""
}
