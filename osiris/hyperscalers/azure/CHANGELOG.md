# Changelog - Microsoft Azure OSIRIS JSON producer

All notable behavioral changes to the **`osirisjson-producer-azure`** producer are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Producer versioning follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

This file tracks the **producer's behavior version** (`metadata.generator.version` in emitted documents).
It is independent of the repository's git tag - a single git tag may bump several producers.
See the root [`CHANGELOG.md`](../../../../CHANGELOG.md) for the release-level index of which producers shipped under each tag.

---

## [Unreleased]

---

## [0.4.0] - 2026-04-25

Expands resource and connection coverage on top of the v0.3.0 `--purpose` foundation. All new resources honour the same contract: collection remains
exhaustive, emission is shaped by `--purpose` (documentation is default; full ARM fidelity under `--purpose audit`).

Scope rule applied: OSIRIS JSON is a topology/document interchange schema, not an IaC/policy format. Monitoring policy (alert rules, action groups, metric
alerts, scheduled query rules) and backup policy (backup/recovery plans, retention rules) are explicitly OUT of Scope.

### Added - App Service / web tier
- App Service Plan (`Microsoft.Web/serverfarms`) as `osiris.azure.appserviceplan`. Captures SKU family/tier/size/capacity,
  Linux flag, per-site scaling, zone redundancy, worker counts and hosted site count.
- App Service (`Microsoft.Web/sites`) with kind routing:
  - sites whose `kind` contains `functionapp` are emitted as `osiris.azure.functionapp`
  - all other sites are emitted as `osiris.azure.webapp`
  - Captures state/enabled, default hostname and custom host names, HTTPS-only, client-cert mode, hosting plan ID, VNet integration subnet,
    public network access, inbound/outbound IPs, managed-environment binding, redundancy mode and site-config fields (Linux/Windows runtime, workers,
    always-on, HTTP/2, min TLS, function scale limit, ACR managed identity)
  - `osiris.azure` extensions carry managed identity (type, principal ID, user-assigned identity IDs), outbound VNet routing flags, private-endpoint
    connection IDs and the linked Application Insights resource (resolved from the Azure portal `hidden-link: /app-insights-resource-id` tag)
- Application Security Group (`Microsoft.Network/applicationSecurityGroups`) as `osiris.azure.asg`.
- NIC IP configurations now collect referenced ASGs so NIC->ASG membership can be wired even though ASGs have no membership list of their own.

### Added - data plane and platform services
- Storage Account (`Microsoft.Storage/storageAccounts`) as `osiris.azure.storageaccount`. Captures SKU tier/name, kind, access tier,
  HTTPS-only, min TLS, public network access, allow-blob-public-access, network ACLs (default action, bypass, IP and VNet rules), primary/secondary
  endpoints, encryption key source and Key Vault reference, private endpoint connection IDs.
- Key Vault (`Microsoft.KeyVault/vaults`) as `osiris.azure.keyvault`.
  Captures SKU, tenant ID, soft-delete / purge-protection flags, enabled-for (deployment/disk encryption/template deployment), RBAC auth mode, network
  ACLs and bypass, public network access, private endpoint connection IDs.
- Container Registry (`Microsoft.ContainerRegistry/registries`) as `osiris.azure.containerregistry`. Captures SKU, admin-enabled flag, login
  server, zone redundancy, public network access, data endpoint flags and private endpoint connection IDs.
- Managed Identity (`Microsoft.ManagedIdentity/userAssignedIdentities`) as `osiris.azure.managedidentity`. Captures tenant ID, principal/client
  identifiers.
- Managed Disk (`Microsoft.Compute/disks`) as `osiris.azure.disk`. State derived from attachment: attached disks are `active`, unattached are
  `inactive`. Captures SKU tier, size (GiB), OS type, disk state, creation option and source resource ID, attached VM ID (when present), zone, tier,
  IOPS/throughput, network access policy, public network access, data-access authentication mode, encryption type and disk encryption set.
- Managed Snapshot (`Microsoft.Compute/snapshots`) as `osiris.azure.snapshot`. Captures SKU, size, OS type, disk state, creation
  source and incremental flag.

### Added - backup and disaster recovery
- Recovery Services Vault (`Microsoft.RecoveryServices/vaults`) as `osiris.azure.recoveryvault`.
  Captures SKU name/tier, storage redundancy, cross-region restore flag, public network access, provisioning state and the protected-item count
  observed under the vault. Private endpoint connection IDs surface as an `osiris.azure` extension.
- Backup Vault (`Microsoft.DataProtection/backupVaults`) as `osiris.azure.backupvault`.
  Captures storage settings (datastore type, redundancy), immutability state, soft-delete state and retention duration, security settings
  (public network access, monitoring flags) and the backup-instance count observed under the vault.

### Added - databases
- SQL Server (`Microsoft.Sql/servers`) as `osiris.azure.sqlserver`.
  Captures version, FQDN, admin login (principal name, not auth material), state, public network access, minimal TLS version, outbound-access
  restriction. Private endpoint connection IDs surface as an `osiris.azure` extension.
- SQL Database (`Microsoft.Sql/servers/databases`) as `osiris.azure.sqldatabase`. Captures SKU name/tier/capacity/family,
  collation, status, max size, zone redundancy, read scale, storage account type and the parent server ID. The implicit `master` system
  database is skipped at collection time (not a workload DB).
- PostgreSQL Flexible Server (`Microsoft.DBforPostgreSQL/flexibleServers`) as `osiris.azure.postgresqlserver`. Captures version, SKU/tier, storage
  (size/tier/IOPS/auto-grow), HA (mode/standby zone/state), availability zone, replication role, FQDN, admin login, public network access and
  delegated subnet ARM ID. Legacy `Microsoft.DBforPostgreSQL/servers` (single server) is end-of-life on Azure's roadmap and intentionally not modeled.
- MySQL Flexible Server (`Microsoft.DBforMySQL/flexibleServers`) as `osiris.azure.mysqlserver`. Same property surface as PostgreSQL Flexible Server.
- Cosmos DB account (`Microsoft.DocumentDB/databaseAccounts`) as `osiris.azure.cosmosaccount`.
  Captures kind (GlobalDocumentDB / MongoDB / Parse), offer type, document endpoint, public network access, automatic-failover,
  multi-region writes, VNet filter flag, free tier, local-auth disabled, default consistency level, capabilities (Cassandra / Table / Gremlin /
  Mongo / Serverless), flattened `locations[]` (name, failover priority, zone redundancy), virtual network rules (subnet IDs) and private
  endpoint connection IDs under the `osiris.azure` extension.
- Redis Cache (`Microsoft.Cache/Redis`) as `osiris.azure.redis`. Captures SKU (Basic/Standard/Premium), family (C or P), capacity,
  Redis version, TLS config, public network access, host name / port / SSL port, shard count, replicas-per-master, injected subnet ID
  (Premium VNet injection), static IP and zones.

### Added - containers and integration
- AKS cluster (`Microsoft.ContainerService/managedClusters`) as `osiris.azure.aks.cluster`. Captures SKU (Base/Automatic + Free/Standard/
  Premium tier), Kubernetes version, DNS prefix, FQDN, node resource group, RBAC/AAD integration (managed AAD, Azure RBAC flag), private-cluster flag
  and private DNS zone, and a flattened `network_profile` (plugin/policy, service/pod CIDR, LB SKU, outbound type). Agent pool count is surfaced as
  a summary. Private endpoint connection IDs surface as an `osiris.azure` extension (private cluster only).
- AKS agent pool (`Microsoft.ContainerService/managedClusters/agentPools`) as `osiris.azure.aks.nodepool`. Captures VM size, count (with autoscale
  min/max when enabled), OS type / SKU, mode (System / User), orchestrator version, VNet subnet and pod subnet ARM IDs and availability zones.
- Container App managed environment (`Microsoft.App/managedEnvironments`) as `osiris.azure.containerapp.environment`. Captures default domain,
  static IP, zone redundancy, and VNet configuration (infrastructure subnet ID + internal flag).
- Container App (`Microsoft.App/containerApps`) as `osiris.azure.containerapp`. Captures environment binding (resolved
  from either `environmentId` or `managedEnvironmentId` depending on CLI version), latest revision name / FQDN, workload profile and ingress
  shape (external/target port/transport/allow-insecure/FQDN). Secrets and full revision bodies are out of scope.
- Container Group / ACI (`Microsoft.ContainerInstance/containerGroups`) as `osiris.azure.containergroup`. Captures OS type, restart policy, SKU,
  IP address (IP / type / FQDN / DNS label) and the list of integrated subnet ARM IDs. Container images, env vars and commands are
  intentionally not emitted - topology models the group, not the workload.
- Service Bus namespace (`Microsoft.ServiceBus/namespaces`) as `osiris.azure.servicebus.namespace`. Captures SKU name/tier/capacity,
  service endpoint, zone redundancy, disable-local-auth flag, public network access, minimum TLS version. Queue / topic / subscription
  enumeration is out of scope. Private endpoint connection IDs surface as an `osiris.azure` extension (Premium tier only).
- Event Hubs namespace (`Microsoft.EventHub/namespaces`) as `osiris.azure.eventhubs.namespace`. Same property surface as Service Bus.
- API Management service (`Microsoft.ApiManagement/service`) as `osiris.azure.apim`. Captures SKU name/capacity (Developer / Basic / Standard / Premium /
  StandardV2 / PremiumV2), gateway / portal / management URLs, VNet integration type (None / External / Internal) and subnet ARM ID,
  public / private IP addresses, disable-gateway flag. Individual APIs, operations, products and policy documents are out of scope - they are
  policy / IaC, not topology.
- Front Door profile (`Microsoft.Cdn/profiles` with Standard_AzureFrontDoor / Premium_AzureFrontDoor SKU) as `osiris.azure.frontdoor.profile`. 
  Captures SKU, kind, provisioning / resource state and the Front Door ID. Routes, rules, endpoints and
  WAF policy associations are routing configuration, not topology and are intentionally omitted. Classic Azure Front Door
  (`Microsoft.Network/frontDoors`, deprecated) is not modeled.

### Added - observability
- Application Insights (`Microsoft.Insights/components`) as `osiris.azure.applicationinsights`. Captures kind, application type,
  ingestion mode, retention, sampling percentage, public network access (ingestion/query), IP masking and local-auth flags. The bound Log
  Analytics workspace ARM ID is surfaced under the `osiris.azure` extension as `workspace_resource_id`. Classic (non-workspace) components are still
  collected but have no workspace extension.
- Log Analytics workspace (`Microsoft.OperationalInsights/workspaces`) as `osiris.azure.loganalytics`.
  Captures SKU, retention, public network access (ingestion/query), CMK-required-for-query flag and daily ingestion cap. The `customer_id`
  (workspace UUID used by KQL) is surfaced under the `osiris.azure` extension.

### Added - connection layers
- App Service Plan `contains` web/function app
- Web/function app `network` -> VNet integration subnet
- Private Endpoint `network` -> hosted web/function app (sourced from the site's `privateEndpointConnections` array)
- NIC `network` -> Application Security Group (sourced from each IP configuration's `applicationSecurityGroups` array)
- Private Endpoint `network` -> Storage Account
- Private Endpoint `network` -> Key Vault
- Private Endpoint `network` -> Container Registry
- Snapshot `contains` source Disk (direction reversed so the source disk is the parent and snapshots appear as children)
- Disk `contains` attached VM (direction reversed so the VM is the parent and its attached managed disks appear as children)
- App Insights `network` -> Log Analytics workspace (workspace-based components only; classic AI is skipped)
- Web App / Function App `network` -> Application Insights (sourced from the `hidden-link: /app-insights-resource-id` portal tag)
- Private Endpoint `network` -> Recovery Services Vault
- Protected resource `network` -> Recovery Services Vault (VM, disk, storage, Key Vault, ACR, Web App, snapshot sources resolved from each
  vault's `backup item list`; orphan / unknown sources silently skipped)
- Protected resource `network` -> Backup Vault (datasource resolved from each vault's `dataprotection backup-instance list`; orphans skipped)
- SQL Server `contains` SQL Database (per-server `az sql db list` iteration; `master` system DB skipped)
- Private Endpoint `network` -> SQL Server
- Private Endpoint `network` -> Cosmos DB account
- Private Endpoint `network` -> Redis cache (Premium tier only; Basic / Standard tiers have no PE support and silently skip)
- PostgreSQL / MySQL flexible server `network` -> delegated subnet (VNet-integrated deployments; public-access servers skipped)
- Redis cache `network` -> injected subnet (Premium VNet injection only)
- AKS cluster `contains` agent pool (direction forward; one parent-to-child edge per node pool)
- AKS agent pool `network` -> delegated subnet (kubenet clusters without a BYO subnet silently skip)
- Private Endpoint `network` -> AKS cluster (private cluster only)
- Container App Environment `contains` Container App (sourced from the app's `environmentId` / `managedEnvironmentId`)
- Container App Environment `network` -> infrastructure subnet (VNet integrated environments only)
- ACI Container Group `network` -> subnet (VNet-integrated ACI only; public-IP groups silently skip)
- Private Endpoint `network` -> Service Bus namespace (Premium tier only)
- Private Endpoint `network` -> Event Hubs namespace
- Private Endpoint `network` -> API Management service
- API Management service `network` -> subnet (External / Internal modes only; `None` mode services silently skip)

### Added
- Private Endpoint `private_link_service_connection` is now emitted as a structured property (`{ group_id, private_link_service_id }`) drawn from
  the ARM `privateLinkServiceConnections[0]`. This allows future OSIRIS JSON consumers to identify the PaaS target service type (blob, vault, registry, etc.) and
  to link each PE to its exact target resource.
- Private Endpoint `custom_dns_configs` array is emitted when present so future OSIRIS JSON consumers can verify DNS-integration completeness.
- `container.region` groups [OSIRIS JSON spec chapter 6.2](https://github.com/osirisjson/osiris/blob/main/specification/v1.0/OSIRIS-JSON-v1.0.md#62-group-types) are emitted
  one group per distinct `provider.region` observed across the subscription's resources, membering every resource in that region.
  Region `global` and empty-region resources are skipped (they are not geographically scoped). Boundary token is `<subscription-id>/<region>`
  so groups never collide across subscriptions.

### Changed
- Connection `type` is now emitted with [OSIRIS JSON spec chapter 5.2.3](https://github.com/osirisjson/osiris/blob/main/specification/v1.0/OSIRIS-JSON-v1.0.md#523-standard-connection-types-v10)
  so downstream future OSIRIS JSON consumers can tell topology edges apart:
  - VNet peering edges: `network` -> `network.peering`
  - VPN gateway connections: `network` -> `network.vpn`
  - ExpressRoute circuit connections: `network` -> `network.bgp`
  - Private Endpoint -> target PaaS edges (WebApp, Function App, KV, ACR, RSV, SQL Server, Cosmos DB, Redis, AKS, Service Bus, Event Hubs, APIM):
    `network` -> `dependency`
  - Private Endpoint -> Storage Account: `network` -> `dependency.storage`
  - Private Endpoint -> database (SQL Server, Cosmos DB, Redis):
    `network` -> `dependency.database` where applicable
- Key Vault `purge_protection` renamed to `purge_protection_enabled` (matches Azure ARM field name and OSIRIS JSON convention for boolean flags).
- Key Vault `network_acls` is now emitted as a top-level property rather than nested under `osiris.azure` extensions - it mirrors the Storage
  Account `network_acls` shape and is needed for compliance BP checks without `--purpose audit`.
- Log Analytics Workspace `retention_days` renamed to `retention_in_days` to match the ARM source field exactly.

### Fixed
- resource `provider.region` and `metadata.scope.regions` are now canonicalized to Azure's slug form (lowercase, no spaces). The `az` CLI
  returns `location` inconsistently - most ARM resources use `westeurope`, `eastus2` etc. while App Service Plans and Web Apps surface the display
  name (`West Europe`). Before this fix, a single-region subscription could emit `["westeurope", "West Europe"]` in its scope regions, which looked 
  like two regions into the same OSIRIS JSON document.
- managed disk collection now iterates per resource group instead of calling `az disk list` subscription-wide. Some Azure CLI configurations
  reject the global form with `ERROR: the following arguments are required: --resource-group/-g`,
  which was silently swallowed by the graceful-skip path and left every managed disk (and every Disk->VM / Snapshot->Disk edge) missing from the document.
- gateway connections pointing at a peer in a different subscription (typically a central connectivity hub owning the ExpressRoute circuit)
  no longer break the document build. Previously `TransformGatewayConnections` fabricated a synthetic target ID for the
  out-of-scope peer, which the SDK's resource-existence invariant rejected with `target ... does not reference an existing resource`. 
  The transform now emits a stub resource for the cross-subscription peer mirroring the VNet peering pattern
  preserving the topology edge while keeping the document valid. Supported stub types:
  `Microsoft.Network/expressRouteCircuits`,
  `Microsoft.Network/virtualNetworkGateways`,
  `Microsoft.Network/localNetworkGateways`.

### Changed
- `az` collection is extended with `appservice plan list`, `webapp list`, `network asg list`, `storage account list`, `keyvault list`, `acr list`,
  `identity list`, `disk list`, `snapshot list`, `resource list --resource-type microsoft.insights/components`, `monitor log-analytics workspace list`, 
  `backup vault list`, `dataprotection backup-vault list`, per-vault `backup item list`, `dataprotection backup-instance list`, `sql server list`, per-server
  `sql db list`, `postgres flexible-server list`, `mysql flexible-server list`, `cosmosdb list`, `redis list`,
  `aks list` with per-cluster `aks nodepool list`, `containerapp env list`, `containerapp list`, `container list`, `servicebus namespace list`,
  `eventhubs namespace list`, `apim list` and `afd profile list`.

### Security
- App Insights `InstrumentationKey`, `ConnectionString` and `AppId` are never emitted (authentication material). Log Analytics
  `primarySharedKey` / `secondarySharedKey` are never collected.
  The workspace `customerId` IS emitted it is the query-scope UUID used by KQL and is not a secret.
- Database admin passwords (SQL / PostgreSQL / MySQL) are never collected; only the admin login **name** (a principal identifier, not a credential)
  is emitted. Cosmos DB primary / secondary keys and connection strings are never collected - the OSIRIS JSON Azure producer does not call `listKeys` or
  `listConnectionStrings`. Redis primary / secondary access keys are never collected - the OSIRIS JSON Azure producer does not call `redis list-keys`.
- Service Bus / Event Hubs namespace access keys and connection strings are never collected the OSIRIS JSON Azure producer does not call
  `servicebus namespace authorization-rule keys list` or the Event Hubs equivalent.
  APIM subscription keys and named values are similarly not collected. Container App secrets (configuration.secrets array) and ACI
  container environment variables are never emitted.

> [!NOTE]
>  - Private endpoint fan-out to Storage / Key Vault / Container Registry / WebApp / Recovery Services Vault is factored through a shared helper so
>    additional PE-bound target types in later releases can reuse the same wiring.
>  - Per-resource diagnostic-setting enumeration (VM/AKS/WebApp/etc -> workspace) is deliberately not included. Wiring every ARM resource's
>    diagnostic settings requires an extra `az monitor diagnostic-settings list --resource <id>` call per resource and materially extends collection
>    time for large subscriptions. Only cheap, directly-derivable edges are emitted (workspace-based AI -> LA, WebApp -> AI, PE -> storage/KV/ACR).
>  - Backup protected-item enumeration is per-vault (one `az` call per Recovery Services Vault, one per Backup Vault) because there is no
>    subscription-wide list API. Collection cost scales with vault count, not subscription size. Backup/retention **policies** themselves are out of
>    scope per the OSIRIS JSON topology/documentation versus IaC model rule at the top of this section.
>  - SQL database enumeration is per-server (one `az sql db list` call per SQL server); PG / MySQL flexible servers and Cosmos accounts expose
>    their databases via their own data-plane APIs and are intentionally not drilled into - the server / account is the topology edge, the workload
>    databases inside are an application concern. SQL is the exception because database-tier sizing (SKU) and zone-redundancy are topology
>    attributes, not application settings.
>  - Database auditing / threat-detection / TDE / firewall-rules / security-alert-policy objects are **policy** and out of scope per the
>    OSIRIS JSON topology/documentation versus IaC model. Firewall rules and VNet rules on Cosmos are exceptions worth surfacing only as a count
>    or subnet-ID list because they describe a topology edge, not a retention policy.
>  - AKS node pools are enumerated per-cluster (one `az aks nodepool list --cluster-name` call per AKS cluster) because the
>    cluster's flat `properties.agentPoolProfiles` does not carry the pool ARM IDs needed for cluster -> nodepool `contains` edges.
>  - APIM operation policies, product subscriptions and Front Door routes / rule sets / WAF policies are routing / admission configuration, not
>    topology, and are intentionally omitted per the OSIRIS JSON topology/documentation versus IaC model.
>    Event Grid topics / Storage queues are in the same category as the "workload-under-service" rule used for SQL databases vs Cosmos DBs -
>    they belong to application configuration and are deferred until there is a concrete OSIRIS JSON topology/documentation reason to surface them.

---

## [0.3.0] - 2026-04-20

Adopted the `pkg/osirismeta` `--purpose` contract. Collection stays exhaustive, but emission is shaped by a declared purpose (documentation
default, audit for full ARM fidelity). Lifted every existing Azure resource to its full-fidelity shape.

> [!WARNING]
> **Breaking default.** Documents produced by 0.3.0 without an explicit `--purpose` flag are smaller than those produced by 0.2.x: `properties`
> and `extensions` maps are stripped from resources, connections and groups. Consumers that depend on the rich payload must run with
> `--purpose audit` to reproduce the previous output.

### Added
- `--purpose {documentation|audit}` flag with validation via `osirismeta.ParsePurpose`, wired through single, CSV, `--all` and interactive modes.
- `metadata.generator.url` set to `https://osirisjson.org`.
- `metadata.scope.name` set to "SubscriptionID - SubscriptionName".
- `metadata.scope.environments` populated from CSV `environment` column.
- `provider.source` set to `azure-cli`.
- Resource tags now collected and emitted for all resource types (VNets, NICs, NSGs, route tables, public IPs, LBs, private endpoints, VNet
  gateways, NAT gateways, firewalls, app gateways, ExpressRoute, VMs).
- NSG security rules emitted as `osiris.azure` extensions (name, priority, direction, access, protocol, source/destination).
- NSG default security rules emitted as `osiris.azure` extensions.
- NSG `subnet_ids` and `nic_ids` associations in properties.
- NIC `subnet_id` in each `ip_configurations` entry, `nsg_id`, `enable_ip_forwarding`, `primary` in properties.
- NIC `enable_accelerated_networking` as `osiris.azure` extension.
- NIC effective routes collected and emitted as `osiris.azure` extensions (source, state, prefix, next_hop_type, next_hop_ip) - requires
  `effectiveRouteTable/action` permission, gracefully skipped with INFO log if read-only.
- Subnet `route_table_id`, `nsg_id`, `nat_gateway_id`, `delegations` in properties.
- Route table `subnets` back-reference (associated subnet ARM IDs) in properties.
- VNet `subnet_count` and `enable_ddos_protection` in properties.
- ExpressRoute circuit `circuit_state`, `provider_state`, `bandwidth_mbps`,
  `peering_location` in properties; `sku`, `sku_tier`, `service_provider` as `osiris.azure` extensions.
- ExpressRoute circuit peerings (BGP details) collected per circuit and
  emitted as `osiris.azure` extensions (name, peering_type, state, peer_asn, vlan_id, address prefixes).
- VNet peerings array in VNet properties (name, peering_state, remote_vnet_id, gateway transit flags, allow_forwarded_traffic).
- VNet gateway `sku`, `active_active`, and `connections[]` array in properties (name, connection_type, peer_id).
- Private DNS zone `virtual_network_links` array with VNet ID, name, registration status in properties.

### Changed
- Generator version `0.2.2` -> `0.3.0`.
- Default emission is now `documentation` grade compliant per [OSIRIS JSON spec 13.1.3](https://github.com/osirisjson/osiris/blob/main/specification/v1.0/OSIRIS-JSON-v1.0.md#1313-data-minimization)
  `properties` and `extensions` are stripped from resources, connections and groups unless `--purpose audit` is passed.
  Collection itself is unchanged and remains exhaustive.
- Removed `--detail` flag, all data aligned to user RBAC collected by default per [OSIRIS JSON spec 11.3.1](https://github.com/osirisjson/osiris/blob/main/specification/v1.0/OSIRIS-JSON-v1.0.md#1131-best-practices-for-producers)
  (route table routes, LB rules/pools, public IP SKU tier are always included).
- NIC `nsg_id` emits explicit `null` when no NSG is attached (was omitted).
- Private DNS zone VNet links now collected via per-zone API call (was returning 0 links from list).

---

## [0.2.1] - 2026-04-06

### Fixed
- Resolve Azure CLI (`az`) binary path once via `exec.LookPath` instead of relying on bare command name per invocation.
  Fixes [CWE-426](https://cwe.mitre.org/data/definitions/426) untrusted search path.

---

## [0.2.0] - 2026-04-06

Initial Microsoft Azure producer release.

### Added
- Full fetch of subscription topology via Azure CLI (`az`).
- Collects VNets, subnets, NICs, NSGs, route tables, public IPs, load balancers, private endpoints, VNet gateways, NAT gateways, firewalls,
  app gateways, DNS zones, ExpressRoute circuits, VMs.
- Cross-subscription VNet peering stubs with `provider.type` and `provider.subscription`.
- Resource group resources (`container.resourcegroup`) per [OSIRIS spec Appendix C.5](https://github.com/osirisjson/osiris/blob/main/specification/v1.0/OSIRIS-JSON-v1.0.md#c5-container-and-organization-resources).
- Resource group and subscription group hierarchy.
- `provider.type` populated with native ARM resource types on all resources.
- Interactive subscription picker when no flags provided.
- CSV batch mode, multi-subscription, and auto-discover (`--all`) modes.
- Output batch hierarchy:
  `<output>/<TenantID>/<timestamp>/<SubscriptionName>.json`.
- Output single filename convention:
  `microsoft-azure-<timestamp>-<SubscriptionName>.json`.

[Unreleased]: ../../../CHANGELOG.md
[0.4.0]: ../../../CHANGELOG.md#040---2026-04-25
[0.3.0]: ../../../CHANGELOG.md#030---2026-04-20
[0.2.1]: ../../../CHANGELOG.md#021---2026-04-06
[0.2.0]: ../../../CHANGELOG.md#020---2026-04-06
