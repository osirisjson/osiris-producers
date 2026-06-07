// transform_automation.go - H1 resource transforms (Logic workflows, Data Factory,
// Synapse, Communication Services, Automation Accounts, Azure Arc machines).
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

// TransformVMSSes converts Microsoft.Compute/virtualMachineScaleSets into OSIRIS JSON
// resources of type osiris.azure.vmss.
func TransformVMSSes(vmsses []VMSS, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(vmsses))

	for _, v := range vmsses {
		id := resourceID("osiris.azure.vmss", v.ID)
		idMap[v.ID] = id

		prov := azureProvider(v.ID, "Microsoft.Compute/virtualMachineScaleSets", v.Location, sub)

		r, err := sdk.NewResource(id, "osiris.azure.vmss", prov)
		if err != nil {
			continue
		}
		r.Name = v.Name
		r.Tags = v.Tags

		props := map[string]any{
			"resource_group": v.ResourceGroup,
		}
		if v.SKU != nil {
			if v.SKU.Name != "" {
				props["sku"] = v.SKU.Name
			}
			if v.SKU.Capacity > 0 {
				props["capacity"] = v.SKU.Capacity
			}
		}
		if p := v.Properties; p != nil {
			r.Status = mapProvisioningState(p.ProvisioningState)
			r.State = p.ProvisioningState
			if p.OrchestrationMode != "" {
				props["orchestration_mode"] = p.OrchestrationMode
			}
			if p.UpgradePolicy != nil && p.UpgradePolicy.Mode != "" {
				props["upgrade_mode"] = p.UpgradePolicy.Mode
			}
			if p.PlatformFaultDomainCount > 0 {
				props["fault_domain_count"] = p.PlatformFaultDomainCount
			}
		} else {
			r.Status = "active"
			r.State = "Succeeded"
		}
		r.Properties = props

		attachArmBody(&r, &v)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformLogicWorkflows converts Microsoft.Logic/workflows into OSIRIS JSON
// resources of type osiris.azure.logic.workflow.
func TransformLogicWorkflows(workflows []LogicWorkflow, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(workflows))

	for _, w := range workflows {
		id := resourceID("osiris.azure.logic.workflow", w.ID)
		idMap[w.ID] = id

		prov := azureProvider(w.ID, "Microsoft.Logic/workflows", w.Location, sub)

		r, err := sdk.NewResource(id, "osiris.azure.logic.workflow", prov)
		if err != nil {
			continue
		}
		r.Name = w.Name
		r.Tags = w.Tags

		props := map[string]any{
			"resource_group": w.ResourceGroup,
		}

		if p := w.Properties; p != nil {
			r.Status = mapLogicWorkflowState(p.State)
			r.State = p.State
			if p.AccessEndpoint != "" {
				props["access_endpoint"] = p.AccessEndpoint
			}
		} else {
			r.Status = "active"
			r.State = "Enabled"
		}
		r.Properties = props

		attachArmBody(&r, &w)
		resources = append(resources, r)
	}
	return resources, idMap
}

func mapLogicWorkflowState(state string) string {
	switch state {
	case "Enabled":
		return "active"
	case "Disabled":
		return "inactive"
	case "Suspended":
		return "inactive"
	case "Completed", "Deleted":
		return "terminated"
	default:
		return "active"
	}
}

// TransformDataFactories converts Microsoft.DataFactory/factories into OSIRIS JSON
// resources of type osiris.azure.datafactory.
func TransformDataFactories(factories []DataFactory, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(factories))

	for _, f := range factories {
		id := resourceID("osiris.azure.datafactory", f.ID)
		idMap[f.ID] = id

		prov := azureProvider(f.ID, "Microsoft.DataFactory/factories", f.Location, sub)

		r, err := sdk.NewResource(id, "osiris.azure.datafactory", prov)
		if err != nil {
			continue
		}
		r.Name = f.Name
		r.Tags = f.Tags

		props := map[string]any{
			"resource_group": f.ResourceGroup,
		}

		if p := f.Properties; p != nil {
			r.Status = mapProvisioningState(p.ProvisioningState)
			r.State = p.ProvisioningState
			if p.Version != "" {
				props["version"] = p.Version
			}
			if p.PublicNetworkAccess != "" {
				props["public_network_access"] = p.PublicNetworkAccess
			}
		} else {
			r.Status = "active"
			r.State = "Succeeded"
		}
		r.Properties = props

		attachArmBody(&r, &f)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformSynapseWorkspaces converts Microsoft.Synapse/workspaces into OSIRIS JSON
// resources of type osiris.azure.synapse.workspace.
func TransformSynapseWorkspaces(workspaces []SynapseWorkspace, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(workspaces))

	for _, w := range workspaces {
		id := resourceID("osiris.azure.synapse.workspace", w.ID)
		idMap[w.ID] = id

		prov := azureProvider(w.ID, "Microsoft.Synapse/workspaces", w.Location, sub)

		r, err := sdk.NewResource(id, "osiris.azure.synapse.workspace", prov)
		if err != nil {
			continue
		}
		r.Name = w.Name
		r.Tags = w.Tags

		props := map[string]any{
			"resource_group": w.ResourceGroup,
		}

		if p := w.Properties; p != nil {
			r.Status = mapProvisioningState(p.ProvisioningState)
			r.State = p.ProvisioningState
			if p.PublicNetworkAccess != "" {
				props["public_network_access"] = p.PublicNetworkAccess
			}
			if p.ManagedVirtualNetwork != "" {
				props["managed_virtual_network"] = p.ManagedVirtualNetwork
			}
			if len(p.ConnectivityEndpoints) > 0 {
				props["connectivity_endpoints"] = p.ConnectivityEndpoints
			}
		} else {
			r.Status = "active"
			r.State = "Succeeded"
		}
		r.Properties = props

		attachArmBody(&r, &w)
		resources = append(resources, r)
	}
	return resources, idMap
}

// TransformCommunicationServices converts Microsoft.Communication/CommunicationServices
// into OSIRIS JSON resources of type osiris.azure.communicationservice.
func TransformCommunicationServices(services []CommunicationService, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(services))

	for _, s := range services {
		id := resourceID("osiris.azure.communicationservice", s.ID)
		idMap[s.ID] = id

		prov := azureProvider(s.ID, "Microsoft.Communication/CommunicationServices", s.Location, sub)

		r, err := sdk.NewResource(id, "osiris.azure.communicationservice", prov)
		if err != nil {
			continue
		}
		r.Name = s.Name
		r.Tags = s.Tags

		props := map[string]any{
			"resource_group": s.ResourceGroup,
		}

		if p := s.Properties; p != nil {
			r.Status = mapProvisioningState(p.ProvisioningState)
			r.State = p.ProvisioningState
			if p.DataLocation != "" {
				props["data_location"] = p.DataLocation
			}
			if p.HostName != "" {
				props["host_name"] = p.HostName
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

// TransformAutomationAccounts converts Microsoft.Automation/automationAccounts into
// OSIRIS JSON resources of type osiris.azure.automation.account.
func TransformAutomationAccounts(accounts []AutomationAccount, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(accounts))

	for _, a := range accounts {
		id := resourceID("osiris.azure.automation.account", a.ID)
		idMap[a.ID] = id

		prov := azureProvider(a.ID, "Microsoft.Automation/automationAccounts", a.Location, sub)

		r, err := sdk.NewResource(id, "osiris.azure.automation.account", prov)
		if err != nil {
			continue
		}
		r.Name = a.Name
		r.Tags = a.Tags

		props := map[string]any{
			"resource_group": a.ResourceGroup,
		}
		if a.SKU != nil && a.SKU.Name != "" {
			props["sku"] = a.SKU.Name
		}

		if p := a.Properties; p != nil {
			r.Status = mapAutomationState(p.State)
			r.State = p.State
			if p.PublicNetworkAccess != nil {
				props["public_network_access"] = *p.PublicNetworkAccess
			}
		} else {
			r.Status = "active"
			r.State = "Ok"
		}
		r.Properties = props

		attachArmBody(&r, &a)
		resources = append(resources, r)
	}
	return resources, idMap
}

func mapAutomationState(state string) string {
	switch state {
	case "Ok":
		return "active"
	case "Suspended", "Unavailable":
		return "inactive"
	default:
		return "active"
	}
}

// TransformArcMachines converts Microsoft.HybridCompute/machines (Azure Arc-enabled
// servers) into OSIRIS JSON resources of type osiris.azure.arc.machine.
func TransformArcMachines(machines []ArcMachine, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(machines))

	for _, m := range machines {
		id := resourceID("osiris.azure.arc.machine", m.ID)
		idMap[m.ID] = id

		prov := azureProvider(m.ID, "Microsoft.HybridCompute/machines", m.Location, sub)

		r, err := sdk.NewResource(id, "osiris.azure.arc.machine", prov)
		if err != nil {
			continue
		}
		r.Name = m.Name
		r.Tags = m.Tags

		props := map[string]any{
			"resource_group": m.ResourceGroup,
		}

		if p := m.Properties; p != nil {
			r.Status = mapArcMachineStatus(p.Status)
			r.State = p.Status
			if p.OSType != "" {
				props["os_type"] = p.OSType
			}
			if p.OSName != "" {
				props["os_name"] = p.OSName
			}
			if p.OSVersion != "" {
				props["os_version"] = p.OSVersion
			}
			if p.AgentVersion != "" {
				props["agent_version"] = p.AgentVersion
			}
			if p.FQDN != "" {
				props["fqdn"] = p.FQDN
			}
		} else {
			r.Status = "active"
			r.State = "Connected"
		}
		r.Properties = props

		if len(m.Extensions) > 0 {
			exts := make([]map[string]any, 0, len(m.Extensions))
			for _, e := range m.Extensions {
				em := map[string]any{
					"name":      e.Name,
					"type":      e.Type,
					"publisher": e.Publisher,
					"status":    mapProvisioningState(e.ProvisioningState),
				}
				if e.TypeHandlerVersion != "" {
					em["version"] = e.TypeHandlerVersion
				}
				if e.AutoUpgradeMinor {
					em["auto_upgrade_minor"] = true
				}
				exts = append(exts, em)
			}
			ext := map[string]any{"extensions": exts}
			r.Extensions = map[string]any{extensionNamespace: ext}
		}

		attachArmBody(&r, &m)
		resources = append(resources, r)
	}
	return resources, idMap
}

func mapArcMachineStatus(status string) string {
	switch status {
	case "Connected":
		return "active"
	case "Disconnected", "Expired", "Error":
		return "inactive"
	default:
		return "active"
	}
}

// TransformLogicAPIConnections converts Microsoft.Web/connections into OSIRIS JSON
// resources of type osiris.azure.logic.apiconnection.
func TransformLogicAPIConnections(conns []LogicAPIConnection, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(conns))

	for _, c := range conns {
		id := resourceID("osiris.azure.logic.apiconnection", c.ID)
		idMap[c.ID] = id

		prov := azureProvider(c.ID, "Microsoft.Web/connections", c.Location, sub)

		r, err := sdk.NewResource(id, "osiris.azure.logic.apiconnection", prov)
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
			if p.API != nil {
				if p.API.Name != "" {
					props["api_name"] = p.API.Name
				}
				if p.API.DisplayName != "" {
					props["api_display_name"] = p.API.DisplayName
				}
			}
			if len(p.Statuses) > 0 && p.Statuses[0].Status != "" {
				props["connection_status"] = p.Statuses[0].Status
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

// TransformEmailServices converts Microsoft.Communication/EmailServices into OSIRIS JSON
// resources of type osiris.azure.emailservice.
func TransformEmailServices(services []EmailService, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(services))

	for _, s := range services {
		id := resourceID("osiris.azure.emailservice", s.ID)
		idMap[s.ID] = id

		prov := azureProvider(s.ID, "Microsoft.Communication/EmailServices", s.Location, sub)

		r, err := sdk.NewResource(id, "osiris.azure.emailservice", prov)
		if err != nil {
			continue
		}
		r.Name = s.Name
		r.Tags = s.Tags

		props := map[string]any{
			"resource_group": s.ResourceGroup,
		}
		if p := s.Properties; p != nil {
			r.Status = mapProvisioningState(p.ProvisioningState)
			r.State = p.ProvisioningState
			if p.DataLocation != "" {
				props["data_location"] = p.DataLocation
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

// TransformEmailDomains converts Microsoft.Communication/EmailServices/Domains into OSIRIS JSON
// resources of type osiris.azure.emailservice.domain.
func TransformEmailDomains(domains []EmailDomain, sub SubscriptionInfo) ([]sdk.Resource, map[string]string) {
	var resources []sdk.Resource
	idMap := make(map[string]string, len(domains))

	for _, d := range domains {
		id := resourceID("osiris.azure.emailservice.domain", d.ID)
		idMap[d.ID] = id

		prov := azureProvider(d.ID, "Microsoft.Communication/EmailServices/Domains", d.Location, sub)

		r, err := sdk.NewResource(id, "osiris.azure.emailservice.domain", prov)
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
			if p.DataLocation != "" {
				props["data_location"] = p.DataLocation
			}
			if p.DomainManagement != "" {
				props["domain_management"] = p.DomainManagement
			}
			if p.MailFrom != "" {
				props["mail_from"] = p.MailFrom
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

// TransformEmailServiceContainsDomainConnections wires each email domain to its
// parent email service with a contains/forward connection.
func TransformEmailServiceContainsDomainConnections(domains []EmailDomain, emailSvcIDMap, emailDomainIDMap map[string]string) []sdk.Connection {
	svcLower := make(map[string]string, len(emailSvcIDMap))
	for k, v := range emailSvcIDMap {
		svcLower[strings.ToLower(k)] = v
	}

	var connections []sdk.Connection
	for _, d := range domains {
		if d.EmailServiceID == "" {
			continue
		}
		domainID, ok := emailDomainIDMap[d.ID]
		if !ok {
			continue
		}
		svcID, ok := svcLower[strings.ToLower(d.EmailServiceID)]
		if !ok {
			continue
		}
		canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
			Type:      "contains",
			Direction: "forward",
			Source:    svcID,
			Target:    domainID,
		})
		connID := sdk.BuildConnectionID(canonicalKey, 16)
		conn, err := sdk.NewConnection(connID, "contains", svcID, domainID)
		if err != nil {
			continue
		}
		conn.Name = fmt.Sprintf("%s -> %s", extractLastSegment(d.EmailServiceID), d.Name)
		_ = conn.SetDirection("forward")
		connections = append(connections, conn)
	}
	return connections
}

// TransformVMSSToSubnetConnections wires each VMSS to the subnet(s) its
// network interface configurations reference.
func TransformVMSSToSubnetConnections(vmsses []VMSS, vmssIDMap, subnetIDMap map[string]string) []sdk.Connection {
	subnetLower := make(map[string]string, len(subnetIDMap))
	for k, v := range subnetIDMap {
		subnetLower[strings.ToLower(k)] = v
	}

	var connections []sdk.Connection
	seen := map[string]bool{}
	for _, v := range vmsses {
		sourceID, ok := vmssIDMap[v.ID]
		if !ok {
			continue
		}
		for _, subnetArm := range v.VMSSSubnetIDs() {
			targetID, ok := subnetLower[strings.ToLower(subnetArm)]
			if !ok {
				continue
			}
			pairKey := sourceID + "|" + targetID
			if seen[pairKey] {
				continue
			}
			seen[pairKey] = true

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
			conn.Name = fmt.Sprintf("%s -> %s", v.Name, extractLastSegment(subnetArm))
			_ = conn.SetDirection("forward")
			connections = append(connections, conn)
		}
	}
	return connections
}
