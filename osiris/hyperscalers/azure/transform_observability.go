// transform_observability.go - Observability resource and connection
// transforms (App Insights, Log Analytics, Metric Alert, Action Group).
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

// TransformApplicationInsights converts Microsoft.Insights/components into
// OSIRIS JSON resources of type osiris.azure.applicationinsights.
// Returns resources and ARM ID -> resource ID map so WebApps can be wired to their
// bound App Insights component and workspace-based AI can be linked to its
// Log Analytics workspace.
//
// Secret fields (InstrumentationKey, ConnectionString, AppID) are never
// emitted - they carry authentication material and add no topology value.
func TransformApplicationInsights(comps []ApplicationInsights, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(comps))

	for _, c := range comps {
		id := resourceID("osiris.azure.applicationinsights", c.ID)
		idMap[c.ID] = id

		prov := azureProvider(c.ID, "Microsoft.Insights/components", c.Location, sub)

		r, err := sdk.NewResource(id, "osiris.azure.applicationinsights", prov)
		if err != nil {
			continue
		}
		r.Name = c.Name
		r.Tags = c.Tags

		props := map[string]any{
			"resource_group": c.ResourceGroup,
		}
		if c.Kind != "" {
			props["kind"] = c.Kind
		}

		if p := c.Properties; p != nil {
			r.Status = mapProvisioningState(p.ProvisioningState)
			r.State = p.ProvisioningState
			if p.ApplicationType != "" {
				props["application_type"] = p.ApplicationType
			}
			if p.IngestionMode != "" {
				props["ingestion_mode"] = p.IngestionMode
			}
			if p.RetentionInDays > 0 {
				props["retention_days"] = p.RetentionInDays
			}
			if p.SamplingPercentage > 0 {
				props["sampling_percentage"] = p.SamplingPercentage
			}
			if p.PublicNetworkAccessForIngestion != "" {
				props["public_network_access_ingestion"] = p.PublicNetworkAccessForIngestion
			}
			if p.PublicNetworkAccessForQuery != "" {
				props["public_network_access_query"] = p.PublicNetworkAccessForQuery
			}
			if p.DisableIPMasking {
				props["disable_ip_masking"] = true
			}
			if p.DisableLocalAuth {
				props["entra_only"] = true
			}
		} else {
			r.Status = "active"
			r.State = "Succeeded"
		}
		r.Properties = props

		if wsID := c.WorkspaceResourceID(); wsID != "" {
			r.Extensions = map[string]any{extensionNamespace: map[string]any{"workspace_resource_id": wsID}}
		}

		attachArmBody(&r, &c)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformLogAnalyticsWorkspaces converts Microsoft.OperationalInsights/
// workspaces into OSIRIS JSON resources of type osiris.azure.loganalytics.
// Returns resources and ARM ID -> resource ID map so App Insights, VMs, AKS
// and other diagnostic-setting sources can be wired to their destination
// workspace.
//
// Shared keys (primary/secondary) are never emitted - they are auth material.
// The customer_id (workspace UUID) is not a secret; it is the query-scope ID
// used in KQL and appears in monitoring tooling, so it is kept under audit.
func TransformLogAnalyticsWorkspaces(wss []LogAnalyticsWorkspace, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(wss))

	for _, w := range wss {
		id := resourceID("osiris.azure.loganalytics", w.ID)
		idMap[w.ID] = id

		prov := azureProvider(w.ID, "Microsoft.OperationalInsights/workspaces", w.Location, sub)

		r, err := sdk.NewResource(id, "osiris.azure.loganalytics", prov)
		if err != nil {
			continue
		}
		r.Name = w.Name
		r.Status = mapProvisioningState(w.ProvisioningState)
		r.State = w.ProvisioningState
		r.Tags = w.Tags

		props := map[string]any{
			"resource_group": w.ResourceGroup,
		}
		if w.SKU != nil && w.SKU.Name != "" {
			props["sku"] = w.SKU.Name
		}
		if w.RetentionInDays > 0 {
			props["retention_in_days"] = w.RetentionInDays
		}
		if w.PublicNetworkAccessForIngestion != "" {
			props["public_network_access_ingestion"] = w.PublicNetworkAccessForIngestion
		}
		if w.PublicNetworkAccessForQuery != "" {
			props["public_network_access_query"] = w.PublicNetworkAccessForQuery
		}
		if w.ForceCmkForQuery {
			props["force_cmk_for_query"] = true
		}
		if w.WorkspaceCapping != nil && w.WorkspaceCapping.DailyQuotaGB > 0 {
			props["daily_quota_gb"] = w.WorkspaceCapping.DailyQuotaGB
		}
		r.Properties = props

		if w.CustomerID != "" {
			r.Extensions = map[string]any{extensionNamespace: map[string]any{"customer_id": w.CustomerID}}
		}

		attachArmBody(&r, &w)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformAppInsightsToWorkspaceConnections wires workspace-based App
// Insights components to their backing Log Analytics workspace via a
// "network" connection (diagnostic-data flow). Classic App Insights (without a workspace binding) is skipped.
func TransformAppInsightsToWorkspaceConnections(comps []ApplicationInsights, aiIDMap, laIDMap map[string]string) []sdk.Connection {
	var connections []sdk.Connection
	for _, c := range comps {
		wsArm := c.WorkspaceResourceID()
		if wsArm == "" {
			continue
		}
		sourceID, ok := aiIDMap[c.ID]
		if !ok {
			continue
		}
		targetID, ok := laIDMap[wsArm]
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
		conn.Name = fmt.Sprintf("%s -> %s", c.Name, extractLastSegment(wsArm))
		_ = conn.SetDirection("forward")
		connections = append(connections, conn)
	}
	return connections
}

// TransformMetricAlerts converts Azure Monitor metric alert rules into OSIRIS JSON resources.
func TransformMetricAlerts(alerts []MetricAlert, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(alerts))

	for _, a := range alerts {
		id := resourceID("osiris.azure.monitor.metricalert", a.ID)
		idMap[a.ID] = id

		prov := azureProvider(a.ID, "Microsoft.Insights/metricAlerts", a.Location, sub)

		r, err := sdk.NewResource(id, "osiris.azure.monitor.metricalert", prov)
		if err != nil {
			continue
		}
		r.Name = a.Name
		r.Tags = a.Tags
		if a.Enabled {
			r.Status = "active"
			r.State = "Enabled"
		} else {
			r.Status = "inactive"
			r.State = "Disabled"
		}

		props := map[string]any{
			"resource_group":       a.ResourceGroup,
			"severity":             a.Severity,
			"enabled":              a.Enabled,
			"evaluation_frequency": a.EvaluationFrequency,
			"window_size":          a.WindowSize,
			"auto_mitigate":        a.AutoMitigate,
		}
		if a.Description != "" {
			props["description"] = a.Description
		}
		if a.TargetResourceType != "" {
			props["target_resource_type"] = a.TargetResourceType
		}
		if len(a.Scopes) > 0 {
			props["scopes"] = a.Scopes
		}
		if len(a.Criteria.AllOf) > 0 {
			type criteriaDim struct {
				Name   string   `json:"name,omitempty"`
				Values []string `json:"values,omitempty"`
			}
			type criteriaEntry struct {
				MetricName      string        `json:"metric_name"`
				MetricNamespace string        `json:"metric_namespace,omitempty"`
				Operator        string        `json:"operator,omitempty"`
				Threshold       float64       `json:"threshold"`
				TimeAggregation string        `json:"time_aggregation,omitempty"`
				Dimensions      []criteriaDim `json:"dimensions,omitempty"`
			}
			entries := make([]criteriaEntry, 0, len(a.Criteria.AllOf))
			for _, c := range a.Criteria.AllOf {
				entry := criteriaEntry{
					MetricName:      c.MetricName,
					MetricNamespace: c.MetricNamespace,
					Operator:        c.Operator,
					Threshold:       c.Threshold,
					TimeAggregation: c.TimeAggregation,
				}
				for _, d := range c.Dimensions {
					if d.Name != "" {
						entry.Dimensions = append(entry.Dimensions, criteriaDim{Name: d.Name, Values: d.Values})
					}
				}
				entries = append(entries, entry)
			}
			props["criteria"] = entries
		}
		if len(a.Actions) > 0 {
			agIDs := make([]string, 0, len(a.Actions))
			for _, ac := range a.Actions {
				if ac.ActionGroupID != "" {
					agIDs = append(agIDs, ac.ActionGroupID)
				}
			}
			if len(agIDs) > 0 {
				props["action_group_ids"] = agIDs
			}
		}
		r.Properties = props
		attachArmBody(&r, &a)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformActionGroups converts Azure Monitor action groups into OSIRIS JSON resources.
func TransformActionGroups(groups []ActionGroup, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(groups))

	for _, g := range groups {
		id := resourceID("osiris.azure.monitor.actiongroup", g.ID)
		idMap[g.ID] = id

		prov := azureProvider(g.ID, "Microsoft.Insights/actionGroups", g.Location, sub)

		r, err := sdk.NewResource(id, "osiris.azure.monitor.actiongroup", prov)
		if err != nil {
			continue
		}
		r.Name = g.Name
		r.Tags = g.Tags
		if g.Enabled {
			r.Status = "active"
			r.State = "Enabled"
		} else {
			r.Status = "inactive"
			r.State = "Disabled"
		}

		props := map[string]any{
			"resource_group":   g.ResourceGroup,
			"group_short_name": g.GroupShortName,
			"enabled":          g.Enabled,
		}
		// Emit receiver counts so consumers know what notification channels are wired.
		receiverCount := len(g.EmailReceivers) + len(g.SmsReceivers) + len(g.WebhookReceivers) +
			len(g.ArmRoleReceivers) + len(g.AzureFunctionReceivers) + len(g.EventHubReceivers) +
			len(g.ItsmReceivers) + len(g.AzureAppPushReceivers) + len(g.AutomationRunbookReceivers) +
			len(g.VoiceReceivers) + len(g.LogicAppReceivers)
		props["receiver_count"] = receiverCount
		if len(g.EmailReceivers) > 0 {
			props["email_receiver_count"] = len(g.EmailReceivers)
		}
		if len(g.SmsReceivers) > 0 {
			props["sms_receiver_count"] = len(g.SmsReceivers)
		}
		if len(g.WebhookReceivers) > 0 {
			props["webhook_receiver_count"] = len(g.WebhookReceivers)
		}
		if len(g.ArmRoleReceivers) > 0 {
			props["arm_role_receiver_count"] = len(g.ArmRoleReceivers)
		}
		if len(g.AzureFunctionReceivers) > 0 {
			props["azure_function_receiver_count"] = len(g.AzureFunctionReceivers)
		}
		if len(g.EventHubReceivers) > 0 {
			props["event_hub_receiver_count"] = len(g.EventHubReceivers)
		}
		if len(g.ItsmReceivers) > 0 {
			props["itsm_receiver_count"] = len(g.ItsmReceivers)
		}
		if len(g.AzureAppPushReceivers) > 0 {
			props["azure_app_push_receiver_count"] = len(g.AzureAppPushReceivers)
		}
		if len(g.AutomationRunbookReceivers) > 0 {
			props["automation_runbook_receiver_count"] = len(g.AutomationRunbookReceivers)
		}
		if len(g.VoiceReceivers) > 0 {
			props["voice_receiver_count"] = len(g.VoiceReceivers)
		}
		if len(g.LogicAppReceivers) > 0 {
			props["logic_app_receiver_count"] = len(g.LogicAppReceivers)
		}
		r.Properties = props
		attachArmBody(&r, &g)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformMetricAlertToActionGroupConnections creates dependency connections
// from each metric alert to its configured action groups.
// ARM stores action group IDs case-insensitively; both sides are normalised to
// lowercase before lookup so GUID casing mismatches between API surfaces do not
// silently drop edges.
func TransformMetricAlertToActionGroupConnections(alerts []MetricAlert, alertIDMap, agIDMap map[string]string) []sdk.Connection {
	agLower := make(map[string]string, len(agIDMap))
	for k, v := range agIDMap {
		agLower[strings.ToLower(k)] = v
	}

	var connections []sdk.Connection
	for _, a := range alerts {
		sourceID, ok := alertIDMap[a.ID]
		if !ok {
			continue
		}
		for _, ac := range a.Actions {
			if ac.ActionGroupID == "" {
				continue
			}
			targetID, ok := agLower[strings.ToLower(ac.ActionGroupID)]
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
			conn.Name = fmt.Sprintf("%s -> %s", a.Name, extractLastSegment(ac.ActionGroupID))
			_ = conn.SetDirection("forward")
			connections = append(connections, conn)
		}
	}
	return connections
}

// TransformDataCollectionRules converts Microsoft.Insights/dataCollectionRules into
// OSIRIS JSON resources of type osiris.azure.monitor.datacollectionrule. Returns
// resources and ARM ID -> resource ID map for wiring DCRs to their workspace destinations.
func TransformDataCollectionRules(rules []DataCollectionRule, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(rules))

	for _, d := range rules {
		id := resourceID("osiris.azure.monitor.datacollectionrule", d.ID)
		idMap[d.ID] = id

		prov := azureProvider(d.ID, "Microsoft.Insights/dataCollectionRules", d.Location, sub)

		r, err := sdk.NewResource(id, "osiris.azure.monitor.datacollectionrule", prov)
		if err != nil {
			continue
		}
		r.Name = d.Name
		r.Tags = d.Tags

		props := map[string]any{
			"resource_group": d.ResourceGroup,
		}
		if p := d.Properties; p != nil {
			r.Status = mapProvisioningState(p.ProvisioningState)
			r.State = p.ProvisioningState
			if p.Description != "" {
				props["description"] = p.Description
			}
			if len(p.DataFlows) > 0 {
				type flowView struct {
					Streams      []string `json:"streams,omitempty"`
					Destinations []string `json:"destinations,omitempty"`
				}
				flows := make([]flowView, 0, len(p.DataFlows))
				for _, f := range p.DataFlows {
					flows = append(flows, flowView{Streams: f.Streams, Destinations: f.Destinations})
				}
				props["data_flows"] = flows
			}
			if p.Destinations != nil && len(p.Destinations.LogAnalytics) > 0 {
				wsIDs := make([]string, 0, len(p.Destinations.LogAnalytics))
				for _, la := range p.Destinations.LogAnalytics {
					if la.WorkspaceResourceID != "" {
						wsIDs = append(wsIDs, la.WorkspaceResourceID)
					}
				}
				if len(wsIDs) > 0 {
					props["log_analytics_workspace_ids"] = wsIDs
				}
			}
		} else {
			r.Status = "active"
			r.State = "Succeeded"
		}
		r.Properties = props

		attachArmBody(&r, &d)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformDataCollectionEndpoints converts Microsoft.Insights/dataCollectionEndpoints
// into OSIRIS JSON resources of type osiris.azure.monitor.datacollectionendpoint.
func TransformDataCollectionEndpoints(endpoints []DataCollectionEndpoint, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(endpoints))

	for _, e := range endpoints {
		id := resourceID("osiris.azure.monitor.datacollectionendpoint", e.ID)
		idMap[e.ID] = id

		prov := azureProvider(e.ID, "Microsoft.Insights/dataCollectionEndpoints", e.Location, sub)

		r, err := sdk.NewResource(id, "osiris.azure.monitor.datacollectionendpoint", prov)
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
			if p.NetworkAcls != nil && p.NetworkAcls.PublicNetworkAccess != "" {
				props["public_network_access"] = p.NetworkAcls.PublicNetworkAccess
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

// TransformAutoscaleSettings converts Microsoft.Insights/autoscalesettings into
// OSIRIS JSON resources of type osiris.azure.monitor.autoscale. Returns resources
// and ARM ID -> resource ID map for wiring autoscale -> target resource edges.
func TransformAutoscaleSettings(settings []AutoscaleSetting, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(settings))

	for _, a := range settings {
		id := resourceID("osiris.azure.monitor.autoscale", a.ID)
		idMap[a.ID] = id

		prov := azureProvider(a.ID, "Microsoft.Insights/autoscalesettings", a.Location, sub)

		r, err := sdk.NewResource(id, "osiris.azure.monitor.autoscale", prov)
		if err != nil {
			continue
		}
		r.Name = a.Name
		r.Tags = a.Tags

		props := map[string]any{
			"resource_group": a.ResourceGroup,
		}
		if p := a.Properties; p != nil {
			if p.Enabled {
				r.Status = "active"
				r.State = "Enabled"
			} else {
				r.Status = "inactive"
				r.State = "Disabled"
			}
			if p.TargetResourceURI != "" {
				props["target_resource_id"] = p.TargetResourceURI
			}
			if len(p.Profiles) > 0 {
				props["profile_count"] = len(p.Profiles)
				// Emit capacity ranges from each profile.
				type profileView struct {
					Name    string `json:"name"`
					MinCap  string `json:"min_capacity,omitempty"`
					MaxCap  string `json:"max_capacity,omitempty"`
					Default string `json:"default_capacity,omitempty"`
				}
				pviews := make([]profileView, 0, len(p.Profiles))
				for _, prof := range p.Profiles {
					pv := profileView{Name: prof.Name}
					if prof.Capacity != nil {
						pv.MinCap = prof.Capacity.Minimum
						pv.MaxCap = prof.Capacity.Maximum
						pv.Default = prof.Capacity.Default
					}
					pviews = append(pviews, pv)
				}
				props["profiles"] = pviews
			}
		} else {
			r.Status = "active"
			r.State = "Enabled"
		}
		r.Properties = props

		attachArmBody(&r, &a)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformDCRToWorkspaceConnections wires each Data Collection Rule to its
// Log Analytics workspace destination(s) via a "dependency" edge.
func TransformDCRToWorkspaceConnections(rules []DataCollectionRule, dcrIDMap, laIDMap map[string]string) []sdk.Connection {
	laLower := make(map[string]string, len(laIDMap))
	for k, v := range laIDMap {
		laLower[strings.ToLower(k)] = v
	}

	var connections []sdk.Connection
	seen := map[string]bool{}
	for _, d := range rules {
		sourceID, ok := dcrIDMap[d.ID]
		if !ok {
			continue
		}
		for _, wsArm := range d.WorkspaceResourceIDs() {
			targetID, ok := laLower[strings.ToLower(wsArm)]
			if !ok {
				continue
			}
			pairKey := sourceID + "|" + targetID
			if seen[pairKey] {
				continue
			}
			seen[pairKey] = true

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
			conn.Name = fmt.Sprintf("%s -> %s", d.Name, extractLastSegment(wsArm))
			_ = conn.SetDirection("forward")
			connections = append(connections, conn)
		}
	}
	return connections
}

// TransformAutoscaleToTargetConnections wires each autoscale setting to the
// resource it governs via a "dependency" edge.
func TransformAutoscaleToTargetConnections(settings []AutoscaleSetting, autoscaleIDMap, scopeIDMap map[string]string) []sdk.Connection {
	scopeLower := make(map[string]string, len(scopeIDMap))
	for k, v := range scopeIDMap {
		scopeLower[strings.ToLower(k)] = v
	}

	var connections []sdk.Connection
	for _, a := range settings {
		if a.Properties == nil || a.Properties.TargetResourceURI == "" {
			continue
		}
		sourceID, ok := autoscaleIDMap[a.ID]
		if !ok {
			continue
		}
		targetID, ok := scopeLower[strings.ToLower(a.Properties.TargetResourceURI)]
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
		conn.Name = fmt.Sprintf("%s governs %s", a.Name, extractLastSegment(a.Properties.TargetResourceURI))
		_ = conn.SetDirection("forward")
		connections = append(connections, conn)
	}
	return connections
}

// TransformMetricAlertToScopeConnections creates dependency connections from
// each metric alert to the resource(s) it monitors (its scopes array).
// scopeIDMap must be pre-built from all available OSIRIS JSON resources ID maps so
// any resource type can be resolved. ARM ID casing is normalised to lowercase
// on both sides before lookup.
func TransformMetricAlertToScopeConnections(alerts []MetricAlert, alertIDMap, scopeIDMap map[string]string) []sdk.Connection {
	scopeLower := make(map[string]string, len(scopeIDMap))
	for k, v := range scopeIDMap {
		scopeLower[strings.ToLower(k)] = v
	}

	var connections []sdk.Connection
	for _, a := range alerts {
		sourceID, ok := alertIDMap[a.ID]
		if !ok {
			continue
		}
		for _, scope := range a.Scopes {
			targetID, ok := scopeLower[strings.ToLower(scope)]
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
			conn.Name = fmt.Sprintf("%s monitors %s", a.Name, extractLastSegment(scope))
			_ = conn.SetDirection("forward")
			connections = append(connections, conn)
		}
	}
	return connections
}
