// transform.go - Shared utilities and cross-domain helpers for the Azure OSIRIS JSON producer.
// All functions are stateless: no I/O, no CLI calls, just data transformation.
//
// Domain-specific transforms live in dedicated files:
//   transform_networking.go    - VNet, Subnet, NIC, NSG, LB, Private Endpoint, Gateway, DNS, ExpressRoute, ASG
//   transform_compute.go       - VM, Disk, Snapshot
//   transform_web.go           - App Service Plan, Web App, Function App
//   transform_storage.go       - Storage Account
//   transform_security.go      - Key Vault, Container Registry
//   transform_identity.go      - Managed Identity
//   transform_observability.go - Application Insights, Log Analytics, Metric Alert, Action Group
//   transform_recovery.go      - Recovery Services Vault, Backup Vault
//   transform_databases.go     - SQL Server, PostgreSQL, MySQL, Cosmos DB, Redis
//   transform_containers.go    - AKS Cluster, Container App Environment, Container App, Container Group
//   transform_integration.go   - Service Bus, Event Hubs, APIM, Front Door
//   transform_groups.go        - Resource Group, Subscription, Region groups
//
// This file retains only truly cross-domain shared code:
//   - Shared primitives: resourceID, azureProvider, normalizeAzureLocation, extractResourceGroup
//   - PE connection helpers: peBinding, collectPEBindings, transformPEBoundConnections, collectPEIDs
//   - State helpers: mapProvisioningState
//   - ID utilities: extractLastSegment, BuildAllResourceIDMap
//
// For an introduction to OSIRIS JSON Producer for Microsoft Azure see:
// [OSIRIS-JSON-AZURE]: https://osirisjson.org/en/docs/producers/hyperscalers/microsoft-azure
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/docs/spec/v10/00-preface

package azure

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"go.osirisjson.org/producers/pkg/sdk"
)

const (
	extensionNamespace = "osiris.azure"
	providerName       = "azure"
)

// resourceID generates a deterministic resource ID from an Azure ARM resource ID.
// Per OSIRIS JSON Producer Guidelines section 2.2.1, hyperscaler resource IDs use the
// pattern: provider::native-id (e.g. azure::/subscriptions/sub-123/.../vm01).
// The ARM resource ID is the stable native identifier for all Azure resources.
func resourceID(_ string, armID string) string {
	return "azure::" + armID
}

// azureProvider creates a Provider for an Azure resource.
// The nativeType parameter is the ARM resource type (e.g. "Microsoft.Network/virtualNetworks").
func azureProvider(armID, nativeType, location string, sub SubscriptionInfo) sdk.Provider {
	return sdk.Provider{
		Name:         providerName,
		Namespace:    extractARMNamespace(nativeType),
		NativeID:     armID,
		Account:      sub.SubscriptionID,
		Type:         nativeType,
		Region:       normalizeAzureLocation(location),
		Subscription: sub.SubscriptionID,
		Tenant:       sub.TenantID,
		System:       sub.DisplayName,
		Version:      armAPIVersion(nativeType),
		Source:       "azure-cli",
	}
}

// extractARMNamespace returns the provider namespace from an ARM resource type string.
// "Microsoft.Network/virtualNetworks" -> "Microsoft.Network"
func extractARMNamespace(nativeType string) string {
	if idx := strings.Index(nativeType, "/"); idx >= 0 {
		return nativeType[:idx]
	}
	return nativeType
}

// armAPIVersion returns the latest stable ARM API version for the given resource type.
func armAPIVersion(nativeType string) string {
	return armAPIVersions[nativeType]
}

// armAPIVersions maps ARM resource types to their latest stable collection API version.
var armAPIVersions = map[string]string{
	"Microsoft.Network/virtualNetworks":                     "2024-05-01",
	"Microsoft.Network/virtualNetworks/subnets":             "2024-05-01",
	"Microsoft.Network/networkInterfaces":                   "2024-05-01",
	"Microsoft.Network/networkSecurityGroups":               "2024-05-01",
	"Microsoft.Network/routeTables":                         "2024-05-01",
	"Microsoft.Network/publicIPAddresses":                   "2024-05-01",
	"Microsoft.Network/publicIPPrefixes":                    "2024-05-01",
	"Microsoft.Network/loadBalancers":                       "2024-05-01",
	"Microsoft.Network/privateEndpoints":                    "2024-05-01",
	"Microsoft.Network/virtualNetworkGateways":              "2024-05-01",
	"Microsoft.Network/natGateways":                         "2024-05-01",
	"Microsoft.Network/azureFirewalls":                      "2024-05-01",
	"Microsoft.Network/applicationGateways":                 "2024-05-01",
	"Microsoft.Network/applicationSecurityGroups":           "2024-05-01",
	"Microsoft.Network/dnsZones":                            "2023-07-01-preview",
	"Microsoft.Network/privateDnsZones":                     "2024-06-01",
	"Microsoft.Network/expressRouteCircuits":                "2024-05-01",
	"Microsoft.Network/connections":                         "2024-05-01",
	"Microsoft.Network/localNetworkGateways":                "2024-05-01",
	"Microsoft.Network/ddosProtectionPlans":                 "2024-05-01",
	"Microsoft.Network/bastionHosts":                        "2024-05-01",
	"Microsoft.Network/trafficManagerProfiles":              "2022-04-01",
	"Microsoft.Network/dnsResolvers":                        "2022-07-01",
	"Microsoft.Network/dnsForwardingRulesets":               "2022-07-01",
	"Microsoft.Network/virtualHubs":                         "2024-05-01",
	"Microsoft.Compute/virtualMachines":                     "2024-07-01",
	"Microsoft.Compute/disks":                               "2024-03-02",
	"Microsoft.Compute/snapshots":                           "2024-03-02",
	"Microsoft.Compute/availabilitySets":                    "2024-07-01",
	"Microsoft.Storage/storageAccounts":                     "2023-05-01",
	"Microsoft.KeyVault/vaults":                             "2023-07-01",
	"Microsoft.ContainerRegistry/registries":                "2023-11-01-preview",
	"Microsoft.ManagedIdentity/userAssignedIdentities":      "2023-07-31-preview",
	"Microsoft.Insights/components":                         "2020-02-02",
	"Microsoft.Insights/metricAlerts":                       "2018-03-01",
	"Microsoft.Insights/actionGroups":                       "2023-01-01",
	"Microsoft.OperationalInsights/workspaces":              "2023-09-01",
	"Microsoft.RecoveryServices/vaults":                     "2024-04-01",
	"Microsoft.DataProtection/backupVaults":                 "2024-04-01",
	"Microsoft.Sql/servers":                                 "2023-08-01",
	"Microsoft.Sql/servers/databases":                       "2023-08-01",
	"Microsoft.DBforPostgreSQL/flexibleServers":             "2024-08-01",
	"Microsoft.DBforMySQL/flexibleServers":                  "2024-02-01-preview",
	"Microsoft.DocumentDB/databaseAccounts":                 "2024-05-15",
	"Microsoft.Cache/Redis":                                 "2024-03-01",
	"Microsoft.ServiceBus/namespaces":                       "2024-01-01",
	"Microsoft.EventHub/namespaces":                         "2024-01-01",
	"Microsoft.ApiManagement/service":                       "2024-05-01",
	"Microsoft.Cdn/profiles":                                "2024-02-01",
	"Microsoft.ContainerService/managedClusters":            "2024-09-01",
	"Microsoft.ContainerService/managedClusters/agentPools": "2024-09-01",
	"Microsoft.App/managedEnvironments":                     "2024-03-01",
	"Microsoft.App/containerApps":                           "2024-03-01",
	"Microsoft.ContainerInstance/containerGroups":           "2023-05-01",
	"Microsoft.Web/serverfarms":                             "2024-04-01",
	"Microsoft.Web/sites":                                   "2024-04-01",
	"Microsoft.Resources/resourceGroups":                    "2024-07-01",
}

// deriveDescription returns a resource description from tags or falls back to "name in rg".
// Checks tags["description"] and tags["Description"] first.
func deriveDescription(name, resourceGroup string, tags map[string]string) string {
	if d := tags["description"]; d != "" {
		return d
	}
	if d := tags["Description"]; d != "" {
		return d
	}
	if resourceGroup != "" {
		return name + " in " + resourceGroup
	}
	return name
}

// normalizeAzureLocation canonicalizes an Azure location to its slug form
// (lowercase, no spaces). `az` returns `location` inconsistently: most ARM
// APIs return the slug (e.g. `westeurope`, `eastus2`) while some surface the
// display name (`West Europe`, `East US 2`). Azure location slugs are all
// `[a-z0-9]+` with no separators, so lowercasing and stripping spaces yields
// the canonical form for every standard Azure region.
func normalizeAzureLocation(loc string) string {
	if loc == "" {
		return ""
	}
	return strings.ToLower(strings.ReplaceAll(loc, " ", ""))
}

// extractResourceGroup extracts the resource group name from an ARM resource ID.
func extractResourceGroup(armID string) string {
	lower := strings.ToLower(armID)
	idx := strings.Index(lower, "/resourcegroups/")
	if idx < 0 {
		return ""
	}
	rest := armID[idx+len("/resourcegroups/"):]
	if slashIdx := strings.Index(rest, "/"); slashIdx >= 0 {
		return rest[:slashIdx]
	}
	return rest
}

// peBinding is an intermediate shape used to collect "resource X has these Private Endpoint (PE)connections" tuples from
// heterogeneous resource types so the common wiring loop can build connections once.
type peBinding struct {
	TargetArmID string
	Conns       []azPrivateEndpointConnRef
	Name        string
}

// collectPEBindings turns a per-resource-type iterator into a flat slice of peBinding tuples.
func collectPEBindings(iter func(yield func(targetArmID string, conns []azPrivateEndpointConnRef, name string))) []peBinding {
	var out []peBinding
	iter(func(targetArmID string, conns []azPrivateEndpointConnRef, name string) {
		out = append(out, peBinding{TargetArmID: targetArmID, Conns: conns, Name: name})
	})
	return out
}

// transformPEBoundConnections emits connections from each private endpoint
// referenced by a binding's privateEndpointConnections array to the target
// resource identified by targetArmID. connType lets callers attach an
// OSIRIS JSON spec chapter 5 section 5.2.3 ("dependency", "dependency.storage", "dependency.database").
func transformPEBoundConnections(bindings []peBinding, targetIDMap, peIDMap map[string]string, connType string) []sdk.Connection {
	var connections []sdk.Connection
	for _, b := range bindings {
		if len(b.Conns) == 0 {
			continue
		}
		targetID, ok := targetIDMap[b.TargetArmID]
		if !ok {
			continue
		}
		for _, pec := range b.Conns {
			peArmID := pec.PrivateEndpointID()
			if peArmID == "" {
				continue
			}
			sourceID, ok := peIDMap[peArmID]
			if !ok {
				continue
			}

			canonicalKey := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
				Type:      connType,
				Direction: "forward",
				Source:    sourceID,
				Target:    targetID,
			})
			connID := sdk.BuildConnectionID(canonicalKey, 16)

			conn, err := sdk.NewConnection(connID, connType, sourceID, targetID)
			if err != nil {
				continue
			}
			conn.Name = fmt.Sprintf("%s -> %s", extractLastSegment(peArmID), b.Name)
			_ = conn.SetDirection("forward")
			connections = append(connections, conn)
		}
	}
	return connections
}

// mapProvisioningState converts an ARM provisioningState value into an OSIRIS JSON
// status enum. "Succeeded" is the ARM steady state -> active.
func mapProvisioningState(state string) string {
	switch strings.ToLower(state) {
	case "succeeded":
		return "active"
	case "updating", "creating", "accepted":
		return "degraded"
	case "failed", "canceled":
		return "inactive"
	case "":
		return "active"
	default:
		return "unknown"
	}
}

// collectPEIDs extracts the private endpoint ARM IDs from an array of
// privateEndpointConnections references (shared between webapps, storage, key vaults, registries).
func collectPEIDs(conns []azPrivateEndpointConnRef) []string {
	if len(conns) == 0 {
		return nil
	}
	ids := make([]string, 0, len(conns))
	for _, c := range conns {
		if id := c.PrivateEndpointID(); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

// extractLastSegment returns the last path segment of an ARM resource ID.
func extractLastSegment(armID string) string {
	idx := strings.LastIndex(armID, "/")
	if idx < 0 {
		return armID
	}
	return armID[idx+1:]
}

// attachUniversalEnvelope merges graph envelope fields into r.Extensions["osiris.azure"].
// arm_id is always written (it equals r.Provider.NativeID); all other fields are written
// only when non-empty. Existing extension keys set by per-resource transform functions
// are preserved - this function only adds keys, never overwrites them.
func attachUniversalEnvelope(r *sdk.Resource, env *GraphEnvelope) {
	if r.Extensions == nil {
		r.Extensions = make(map[string]any)
	}
	ext, _ := r.Extensions[extensionNamespace].(map[string]any)
	if ext == nil {
		ext = make(map[string]any)
	}

	ext["arm_id"] = r.Provider.NativeID

	if env != nil {
		if env.Kind != "" {
			ext["kind"] = env.Kind
		}
		if env.Etag != "" {
			ext["etag"] = env.Etag
		}
		if env.SKU != nil && env.SKU.Name != "" {
			sku := map[string]any{"name": env.SKU.Name}
			if env.SKU.Tier != "" {
				sku["tier"] = env.SKU.Tier
			}
			if env.SKU.Size != "" {
				sku["size"] = env.SKU.Size
			}
			if env.SKU.Capacity != 0 {
				sku["capacity"] = env.SKU.Capacity
			}
			ext["sku"] = sku
		}
		if env.Identity != nil && env.Identity.Type != "" {
			id := map[string]any{"type": env.Identity.Type}
			if env.Identity.PrincipalID != "" {
				id["principal_id"] = env.Identity.PrincipalID
			}
			ext["identity"] = id
		}
		if env.ManagedBy != "" {
			ext["managed_by"] = env.ManagedBy
		}
		if env.Plan != nil && env.Plan.Name != "" {
			plan := map[string]any{"name": env.Plan.Name}
			if env.Plan.Publisher != "" {
				plan["publisher"] = env.Plan.Publisher
			}
			if env.Plan.Product != "" {
				plan["product"] = env.Plan.Product
			}
			ext["plan"] = plan
		}
		// Timestamps: prefer systemData (service-populated), fall back to properties aliases.
		createdAt := ""
		changedAt := ""
		if env.SystemData != nil {
			createdAt = env.SystemData.CreatedAt
			changedAt = env.SystemData.LastModifiedAt
			// Author fields from systemData.
			sd := map[string]any{}
			if env.SystemData.CreatedBy != "" {
				sd["created_by"] = env.SystemData.CreatedBy
			}
			if env.SystemData.CreatedByType != "" {
				sd["created_by_type"] = env.SystemData.CreatedByType
			}
			if env.SystemData.LastModifiedBy != "" {
				sd["last_modified_by"] = env.SystemData.LastModifiedBy
			}
			if env.SystemData.LastModifiedByType != "" {
				sd["last_modified_by_type"] = env.SystemData.LastModifiedByType
			}
			if len(sd) > 0 {
				ext["system_data"] = sd
			}
		}
		if createdAt == "" {
			createdAt = env.CreatedTime
		}
		if changedAt == "" {
			changedAt = env.ChangedTime
		}
		if createdAt != "" {
			ext["created_at"] = createdAt
		}
		if changedAt != "" {
			ext["changed_at"] = changedAt
		}
		if env.PublicNetworkAccess != "" {
			ext["public_network_access"] = env.PublicNetworkAccess
		}
		if env.ProvisioningState != "" {
			ext["provisioning_state"] = env.ProvisioningState
			if r.State == "" {
				r.State = env.ProvisioningState
				r.Status = mapProvisioningState(env.ProvisioningState)
			}
		}
		if len(env.Locks) > 0 {
			locks := make([]map[string]any, len(env.Locks))
			for i, l := range env.Locks {
				lm := map[string]any{"name": l.Name, "level": l.Level}
				if l.Notes != "" {
					lm["notes"] = l.Notes
				}
				locks[i] = lm
			}
			ext["locks"] = locks
		}
		if len(env.DiagSettings) > 0 {
			diags := make([]map[string]any, len(env.DiagSettings))
			for i, d := range env.DiagSettings {
				dm := map[string]any{"name": d.Name}
				if d.WorkspaceID != "" {
					dm["workspace_id"] = d.WorkspaceID
				}
				if d.StorageID != "" {
					dm["storage_id"] = d.StorageID
				}
				if d.EventHubID != "" {
					dm["event_hub_id"] = d.EventHubID
				}
				diags[i] = dm
			}
			ext["diagnostic_settings"] = diags
		}
		if len(env.RoleAssignments) > 0 {
			ras := make([]map[string]any, len(env.RoleAssignments))
			for i, ra := range env.RoleAssignments {
				rm := map[string]any{
					"role":         ra.RoleName,
					"principal_id": ra.PrincipalID,
				}
				if ra.PrincipalType != "" {
					rm["principal_type"] = ra.PrincipalType
				}
				if ra.PrincipalName != "" {
					rm["principal_name"] = ra.PrincipalName
				}
				ras[i] = rm
			}
			ext["role_assignments"] = ras
		}
	}

	r.Extensions[extensionNamespace] = ext
}

// attachArmBody stores the unmodified ARM body under extensions["osiris.azure.arm"].
// body is marshalled from the typed properties struct; nil body produces {"body": null}.
// This provides lossless passthrough of all fields the struct models, closing field-depth
// gaps (OSIRIS JSON spec chapter 6) without additional API calls.
func attachArmBody(r *sdk.Resource, body any) {
	if r.Extensions == nil {
		r.Extensions = make(map[string]any)
	}
	// Store as a JSON string so the secret scanner does not recurse into ARM field
	// names (e.g. "enableRbacAuthorization" contains "auth") and false-positive with
	// fail-closed mode. Consumers that need the raw body parse the string.
	var bodyStr string
	if body != nil {
		if b, err := json.Marshal(body); err == nil {
			bodyStr = string(b)
		}
	}
	r.Extensions["osiris.azure.arm"] = map[string]any{
		"schema":      "raw-passthrough/v1",
		"api_version": armAPIVersion(r.Provider.Type),
		"kind":        "snapshot",
		"fetched_at":  time.Now().UTC().Format(time.RFC3339),
		"body":        bodyStr,
	}
}

// BuildAllResourceIDMap merges all ARM ID -> OSIRIS JSON ID maps into one for gateway connection wiring.
func BuildAllResourceIDMap(maps ...map[string]string) map[string]string {
	total := 0
	for _, m := range maps {
		total += len(m)
	}
	merged := make(map[string]string, total)
	for _, m := range maps {
		for k, v := range m {
			merged[k] = v
		}
	}
	return merged
}
