# Changelog - Microsoft Azure OSIRIS JSON producer

All notable behavioral changes to the **`osirisjson-producer-azure`**
producer are documented in this file.

The format follows:
- [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Producer versioning follows:
- [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

To maintain a consistent line width across `CHANGELOG.md` and all file 
comments (excluding links, code examples, and tables), we adhere to:
- [RFC 7994](https://datatracker.ietf.org/doc/html/rfc7994#section-4.3)

This file tracks the **producer's behavior version**
(`metadata.generator.version` in emitted documents). It is independent
of the repository's git tag - a single git tag may bump several
producers. See the root [`CHANGELOG.md`](../../../../CHANGELOG.md) for
the release-level index of which producers shipped under each tag.

---

## [0.6.0] - 2026-11-08

Topology enrichment. Relationships the ARM data states explicitly are
now emitted as connections; nothing is inferred from resource names,
resource group, region or cardinality. Connections describe configured
relationships, typed by kind (configuration association, data path, BGP
control plane, configured forwarding intent); two resources being in the
same connected component does not show that traffic can flow between
them. No Route Server <-> gateway route exchange is emitted;
`allow_branch_to_branch_traffic` states the Route Server setting.
Several collection gaps that silently dropped resource fields, status
and relationships are fixed, and credentials found in tags and raw
bodies are redacted (see Security).

### Added
- **VM -> NIC** `contains` (forward) connections, from the VM
  `networkProfile.networkInterfaces[].id` and the NIC
  `virtualMachine.id`. An attachment visible from both sides is
  emitted once.
- **NIC -> NSG** `osiris.azure.nsg.association` (forward)
  connections, from the NIC `networkSecurityGroup.id`.
- **NIC -> Public IP** `network` (forward) connections, from the NIC
  `ipConfigurations[].publicIpAddress.id`. `properties.ip_configuration`
  names the IP configuration carrying the binding.
- **VM -> Disk** `dependency.storage` (forward) connections, from the
  VM `storageProfile.osDisk.managedDisk.id` and
  `storageProfile.dataDisks[].managedDisk.id`, and from the disk
  `managedBy` field. `properties.disk_role` is `os` or `data`;
  data disks also carry `properties.lun`. One connection per VM/disk
  pair regardless of how many fields report it.
- **Private endpoint -> Private Link target** `network` (forward)
  connections, from the endpoint's own
  `privateLinkServiceConnections[].privateLinkServiceId` and
  `manualPrivateLinkServiceConnections[].privateLinkServiceId`, for any
  target resource in the document. `status` is `active` for an
  `Approved` binding and `inactive` for `Pending`, `Rejected` or
  `Disconnected`; `state` keeps the ARM approval state.
  `properties.group_ids` lists the Private Link sub-resources and
  `properties.manual_approval` marks manual bindings. Several bindings
  of one endpoint to the same target are one connection.
- **Load balancer -> backend NIC / VMSS** `network` (forward)
  connections, for backend pools that a load-balancing rule sends
  traffic to. Membership is read from the pool
  (`backendIPConfigurations`, `loadBalancerBackendAddresses`), the NIC
  IP configurations and the VMSS model
  (`loadBalancerBackendAddressPools`). `properties.backend_pools` lists
  the pool names. Pools and IP configurations are not emitted as
  resources.
- **VMSS -> NSG** `osiris.azure.nsg.association` (forward)
  connections, from the VMSS model NIC configurations.
- **SQL database -> elastic pool** `dependency` (forward)
  connections, from the database `elasticPoolId`.
- **Workload -> user-assigned managed identity**
  `osiris.azure.identity.assignment` (forward) connections, from each
  resource's ARM `identity.userAssignedIdentities` and the AKS
  `identityProfile` (kubelet identity). `properties.assigned_as` lists
  `resource` and/or the identity profile key.
- **Monitored resource -> data collection rule** `dataflow` (forward)
  connections, from data collection rule associations (one paginated
  Resource Graph query). `properties.association` names the
  association.
- **SQL failover groups** as `osiris.azure.sql.failovergroup`
  resources (`az sql failover-group list` per server), with
  `replication_role`, `partner_server_ids`, `database_count`,
  `failover_policy`, `grace_period_minutes`; `state` is the replication
  state. Connections: SQL server -> failover group `contains`, failover
  group -> member database `contains`, primary -> secondary server
  `dataflow` (geo-replication). A replication stated by both servers is
  one connection. A partner server outside the export (another
  subscription or region) resolves to an `osiris.azure.sqlserver` stub
  (status `unknown`, no region or group placement), so the
  geo-replication edge is kept.
- **ExpressRoute connection -> gateway and -> circuit**
  `osiris.azure.expressroute.connection.endpoint` (forward)
  connections, from the connection's `virtualNetworkGateway1.id` and
  `peer.id` (a circuit in another subscription resolves to its stub),
  so every `osiris.azure.expressroute.connection` resource is reachable
  by graph traversal. Properties: `endpoint_role` (`gateway` or
  `circuit`), `evidence_field` (`virtualNetworkGateway1.id` or
  `peer.id`) and `source_arm_id` (the ARM ID exactly as the connection
  states it). Configuration relationships next to the gateway <->
  circuit `network` edge, which remains the data path; its connection
  ID is unchanged.
- **Route table -> next hop** `osiris.azure.route.nexthop` (forward)
  connections: a user-defined route with next hop type
  `VirtualAppliance` links its route table to the internal load
  balancer or NIC whose private IP is the next hop, when exactly one
  exported frontend or NIC IP configuration carries that IP in the
  route table's VNet or a VNet an exported peering links with it.
  Configured forwarding intent, not observed traffic. Properties:
  `next_hop_ips`, `route_count`, `address_prefix_count`,
  `route_names`. Unmatched, ambiguous and out-of-scope next hops are
  not linked (counts logged to stderr).
- **Route Server placement and BGP peers.** The Route Server IP
  configurations (`virtualHubs/{hub}/ipConfigurations`, via `az rest`)
  and BGP peers (`az network routeserver peering list`) are collected,
  both read-only. Route Server -> subnet and -> public IP `network`
  (forward) connections are emitted from them. A `network.bgp`
  (bidirectional) connection links the Route Server and the NIC
  carrying a peer IP when exactly one exported NIC IP configuration
  carries it in the Route Server's VNet or a peered VNet; it is a
  control-plane session (the Route Server does not forward traffic).
  Every peer is kept in `extensions["osiris.azure"].bgp_connections`
  with `peer_ip`, `peer_asn`, `connection_state` and `resolution`
  (`resolved` with `resource_id`, `unresolved`, `ambiguous` or
  `no_placement`); unresolved peers carry no association.
- **Gateway transit settings** `allow_remote_vnet_traffic` and
  `allow_virtual_wan_traffic` on virtual network gateways, and
  `allow_branch_to_branch_traffic` on Route Servers, are always stated,
  also when `false`. No Route Server <-> gateway route exchange is
  emitted.
- **Subscription catalog** `metadata["osiris.azure"]`: an Azure-specific
  producer extension on `metadata` (OSIRIS JSON schema permit additional
  members there; it is not an OSIRIS JSON v1.0 field). Shape:
  `{"tenants": {"<tenant id>": {"name"}}, "subscriptions": {"<subscription id>": {"coverage", "name", "tenant_id"}}}`. `tenants`
  and `subscriptions` are sibling objects keyed by unique, lower-case
  Azure IDs; a record never repeats its own key. `tenants` lists exactly
  the tenants the listed subscriptions belong to and states each
  tenant's name once; the exported tenant is the `tenant_id` of the
  exported subscription's entry. `coverage` is `exported` for the
  subscription this document inventories (the only one in
  `metadata.scope.accounts`) and `reference_only` for every other
  subscription the document names in ARM IDs (stubs, peerings, network
  ACL subnets, diagnostic destinations, connection properties), whose
  resources were not inventoried. Raw ARM bodies are not scanned, so the
  catalog does not depend on `--include-raw-body` or `--purpose`. Names
  and tenants come only from read-only authoritative sources:
  `az account show` (the exported subscription and its
  `tenantDisplayName`), the management group entity list (subscriptions
  of the same tenant), `az account list --all` (subscriptions the caller
  can see in other tenants) and the ARM tenants API through `az rest`
  (tenant names).
  Anything unknown is omitted (a subscription no source names keeps
  `coverage` only), an ID is never used as a name, and the exported
  tenant is never assumed for another subscription. When no source
  states a tenant name, the tenant is listed without one.
- **Subscription and tenant IDs on groups.** The
  `logical.subscription` group carries `properties.subscription_id`,
  `tenant_id` and, when known, `tenant_name`; its `name` stays the
  subscription display name. Management groups gain `tenant_name` for
  their tenant when known. Group IDs and membership are unchanged; no
  group is created for a `reference_only` subscription.
- **`extensions["osiris.azure.arm"].source`** (with
  `--include-raw-body`): `arm` when `body` is the ARM object returned by
  the az CLI, `struct` when only the decoded fields are available.

### Changed
- **Breaking: subnet/VNet containment direction corrected.** The
  connection is now VNet -> Subnet (`contains`, forward), matching the
  "source contains target" definition of OSIRIS JSON spec chapter 5
  section 5.2.3. It was previously emitted as Subnet -> VNet. The
  connection IDs of these edges change.
- **Breaking: subnet -> NSG connection type** changed from `network`
  to `osiris.azure.nsg.association`. An NSG attachment is a policy
  association, not network reachability. The connection IDs of these
  edges change.
- **Breaking: subnet -> route table connection type** changed from
  `network` to `osiris.azure.routetable.association`. The connection
  IDs of these edges change.
- **Breaking: NAT gateway attachment** is now Subnet -> NAT gateway
  `osiris.azure.natgateway.association` (forward), read from the
  subnet `natGateway` and the gateway `subnets`, replacing NAT gateway
  -> Subnet `network`.
- **Breaking: private endpoint -> target connections** are typed
  `network` and built from the private endpoint, replacing the
  per-service `dependency`, `dependency.storage` and
  `dependency.database` builders that read the target's
  `privateEndpointConnections` (which emitted none in practice, see
  Fixed).
- **Breaking: ExpressRoute gateway <-> circuit** connections changed
  from `network.bgp` to `network` (bidirectional): the connection
  carries the private-peering data path as well as the route exchange.
  Properties: `connection_id` (the `Microsoft.Network/connections`
  resource in the document), `connection_type`, `routing_weight`,
  `enable_bgp`. The connection IDs of these edges change.
- **Breaking: `Microsoft.Network/connections` resources** are typed by
  their `connectionType`: ExpressRoute ->
  `osiris.azure.expressroute.connection`; IPsec, Vnet2Vnet and
  VPNClient stay `network.vpn`. Resource IDs are unchanged.
- **Breaking: backup protection** connections (protected resource ->
  Recovery Services vault / Backup vault) changed from `network` to
  `dataflow`: backup data flows from the resource to the vault. The
  connection IDs of these edges change.
- **Breaking: snapshot lineage** is now source disk (or source
  snapshot) -> snapshot `dataflow` (forward), from the snapshot
  `creationData.sourceResourceId`. It was emitted as snapshot -> disk
  `contains` (reverse), which reads "snapshot contains disk".
- **Breaking: Application Insights -> Log Analytics workspace**
  connection type changed from `network` to `dataflow`: telemetry is
  written into the workspace. The connection IDs of these edges
  change.
- **Breaking: NIC -> application security group** connection type
  changed from `network` to `osiris.azure.asg.association`. ASG
  membership is a policy association, not network reachability.
- **Breaking: `status` and `state` are no longer invented.** When Azure
  reports no state for a resource, both are omitted (previously
  `status: active` plus a fixed `state` such as `Succeeded`, `Enabled`,
  `Online`, `Ready` or `Connected`). Status mappers now return only the
  schema values `active`, `inactive`, `degraded`, `retired` and
  `unknown`: paused or stopped -> `inactive`, transitional (starting,
  stopping, updating, scaling) -> `degraded`, deleted or completed ->
  `retired`, unrecognized -> `unknown`. `state` keeps the Azure value.
- **Breaking: disk -> VM `contains` (reverse) connections removed**,
  replaced by the VM -> Disk `dependency.storage` connections above.
- ARM resource ID references are resolved case-insensitively
  everywhere. `provider.native_id` and resource IDs keep the casing
  Azure returned; no resource ID changes.
- Every connection passes one final resolution step before the
  document is built: connections whose source or target is not a
  resource in the document, self-references and duplicates of the same
  relationship are dropped. A summary (candidates, emitted, duplicates,
  dangling, per type) is logged to stderr.
- `--include-raw-body` now attaches the ARM object as returned by
  the az CLI, including fields not mapped to OSIRIS JSON, instead of
  re-serializing the producer's decoded struct (which kept only modeled
  fields). Objects are captured from the list output the producer
  already fetches; no additional az calls are made. Nested objects that
  are resources themselves (e.g. subnets inside a VNet) are captured
  too.
  Secrets are redacted before attachment and replaced with `[REDACTED]`:
  - strings under a sensitive key, including ARM spellings
    (`connectionString`, `primaryKey`, `accessKey`, `customData`) and
    keys whose value is a secret-bearing URL (`callbackUrl`,
    `serviceUri`, `triggerUrl`, `webhookUrl`);
  - value fields (`value`, `defaultValue`, `body`, `content`, `data`,
    `text`, `key`) of objects below a sensitive key, while structural
    fields such as `type`, `method`, `uri` and `audience` are kept;
  - the value of ARM `securestring` and `secureobject` parameters;
  - any string matching a secret value pattern, including Azure SAS
    signatures (`sig=`), Function keys (`code=`), storage and Service
    Bus connection strings, and credentials in form-encoded bodies,
    checked both as text and in its JSON-encoded form (so the lines of
    a multi-line string cannot match together once encoded).
  Booleans and numbers are kept. Keys of Logic App `runAfter` maps
  (action names) and of Key Vault access policy `permissions`
  (permission categories) are not field names and never trigger
  redaction. Application Insights `InstrumentationKey` and everything
  under Logic App API connection `parameterValues` /
  `parameterValueSet` are redacted. The body is encoded with every
  element on its own line after a space, and a space after each colon,
  so a value pattern cannot match across two fields, also once the body
  is stored as a JSON string. A body that still matches a secret pattern
  after redaction is discarded and the decoded struct is attached
  instead, with `extensions["osiris.azure.arm"].raw_rejected` naming the
  matched rule.
  A capture summary (attached, fallback per ARM type, rejected,
  redacted values) is logged to stderr.
- `extensions["osiris.azure.arm"].fetched_at` is the run's snapshot
  time, identical for every resource of a document, instead of the
  moment each resource was transformed. Repeated runs over the same
  data produce identical documents.
- **Breaking: provider and scope identity fields made consistent.**
  For consumers:
  - `provider.account` is the owning Azure subscription of every
    resource, read from its own ARM ID (`provider.native_id`) and
    written in lower case (ARM IDs may spell the GUID in either case);
    a stub keeps its foreign subscription. Previously every transformed
    resource was given the exported subscription. Subscription IDs in
    `metadata.scope.accounts`, the subscription catalog and the
    subscription group's `subscription_id` use the same lower-case
    form.
    VNet stubs no longer repeat the owner as
    `properties.subscription_id`.
  - `provider.subscription` is no longer emitted (it duplicated
    `provider.account`).
  - `provider.tenant` is set only when the owning tenant is known: the
    exported subscription's tenant for its resources, or the tenant the
    management group entity list states for a stub's subscription.
  - `provider.system` is no longer emitted. It carried the subscription
    display name, which is not a producing or source system; names are
    in the subscription catalog and the `logical.subscription` group.
  - `metadata.scope.accounts` is the exported subscription ID (it
    previously held the tenant ID, now the `tenant_id` of the exported
    subscription's entry in the `metadata["osiris.azure"]` catalog);
    `metadata.scope.subscriptions` is no longer emitted.
    Subscriptions only referenced by ARM IDs are not added to scope.
  - Key Vault `extensions["osiris.azure"].network_acls` is removed: it
    was an identical copy of `properties.network_acls`, which is
    unchanged. Storage accounts keep their `network_acls` in extensions.
  Resource, connection and group IDs are unchanged, and no connection
  or group membership changes.
- `metadata.scope.name` is now the subscription display name alone
  (previously `"<subscription-id> - <display-name>"`). The subscription
  ID remains in `metadata.scope.accounts`; it is used as the name only
  when Azure returns no display name.

### Fixed
- **Management group hierarchy skipped unreadable levels.** An ancestor
  management group the caller cannot read was dropped, attaching its
  child to the grandparent. It is now rebuilt from the subscription's
  own parent name chain, with the same group ID it has when readable.
- **Resource group membership across subscriptions.** A resource owned
  by another subscription could become a member of a local resource
  group with the same name. Membership is now limited to resources of
  the exported subscription.
- **Disk attachments silently dropped.** The disk `managedBy` field
  frequently differs from the VM ARM ID only in casing, so no
  disk/VM connection was emitted for those disks.
- **Resource fields lost for flat az list output.** Several az list
  commands return resource fields at the top level of each object
  instead of under `properties`, which the decoder expected. Those
  fields decoded to nothing: properties, status and relationships
  derived from them were missing for API Management, AKS, Key Vault,
  SQL server, SQL database, SQL managed instance, Logic Apps, data
  collection rules and endpoints, Automation accounts and VMSS. Both
  forms are now decoded. This adds properties to those resources and
  emits relationships that were silently skipped (VMSS -> Subnet,
  SQL managed instance -> Subnet, data collection rule -> Log
  Analytics workspace). Disabled Logic App workflows were reported as
  `Enabled` / `active`; they now carry their real state.
- **Private endpoint references on targets** returned flattened by az
  (`privateEndpoint.id` instead of `properties.privateEndpoint.id`)
  were not read, so the private endpoint ID extensions
  (`private_endpoint_ids`, `private_endpoint_connection_ids`) of
  targets listed in that form were empty.
- **Status values outside the schema enum.** SQL database, SQL
  managed instance, PostgreSQL / MySQL flexible server, Stream
  Analytics and Logic App states could map to `transitioning`,
  `terminated` or a lower-cased Azure value, which fail strict schema
  validation.
- SQL managed instance databases, SQL elastic pools and SQL virtual
  machines now carry `extensions["osiris.azure.arm"]` like every
  other resource (with `--include-raw-body`, the ARM object).
- **Transient az failures dropped whole collections.** A single
  network or service error (e.g. an SSL EOF, a connection reset,
  throttling, a 5xx) on one az call, such as the tenant-wide management
  group entity list, removed that data for the run. az calls failing
  with such an error are now retried twice (after 2 s and 5 s);
  authorization, not-found, validation errors and timeouts are not
  retried. Retries are logged to stderr.
- **`--include-raw-body` redacted boolean flags stored as text** below
  a secret-named key (e.g. the AKS Key Vault add-on
  `enableSecretRotation: "false"`). `"true"` / `"false"` strings are
  now kept; secure parameters and value patterns still apply.
- Resource fields missing for 13 types collected with
  `az resource list` (Application Insights, Logic App API
  connections, Communication and email services and domains, Data
  Factory, Synapse, Arc machines, autoscale settings, DNS private
  resolvers and forwarding rulesets, Stream Analytics, Event Grid
  system topics). That command returns `properties` as null, so these
  resources had no properties, status or relationships derived from
  them (e.g. Application Insights -> Log Analytics workspace).
  `properties` are now filled from Resource Graph (one paginated
  query) by ARM ID; resource IDs and names still come from the list.
  With `--include-raw-body` the body's null `properties` is filled the
  same way.
- **Key Vault properties.** `az keyvault list` returns no vault
  properties and the `az keyvault show` enrichment ran only when the
  TLS version was missing, copying two fields. It now runs whenever
  the vault URI is missing and uses the full show properties (SKU,
  vault URI, soft delete, purge protection, RBAC), keeping Resource
  Graph values where show has none. With `--include-raw-body` the show
  object is attached.
- **VM `status` was always `unknown`.** `az vm list` returns no power
  state, so the power state mapper never had input. The power state is
  now read from Resource Graph (one paginated query) and mapped:
  running -> `active`, stopped / deallocated -> `inactive`, starting /
  stopping -> `degraded`. `state` remains the provisioning state.

- **ExpressRoute gateway connections were not emitted** when a
  connection referenced its gateway or circuit with different casing
  than the referenced resource, and the gateway's `connections`
  property and a VNet's peering list missed entries in the same case.

### Security
- **Credentials in tags.** Resource tags are free text and are emitted
  for every `--purpose`; the Azure portal stores an Application
  Insights connection string (`InstrumentationKey=...`) as the value of
  the `hidden-link` tag, and connections inherit their source's tags.
  Every tag value, and every string in properties and extensions, of
  resources, connections and groups that matches a secret value pattern
  is now replaced with `[REDACTED]` after the document is built.
  Documents produced by earlier versions for subscriptions using that
  tag contain the connection string.
- Fail-closed scan aborted documents on ARM bodies that would not be
  emitted, and struct bodies were not redacted.
  Without `--include-raw-body`, the decoded-struct ARM bodies were
  removed only after the document was built, so the fail-closed secret
  scan could reject a whole subscription because of a body that would
  never be emitted; they are now removed before the build. With
  `--include-raw-body`, a resource whose ARM object was not captured, or
  whose capture was rejected, kept its struct body unredacted; struct
  bodies now get the same redaction and checks as captured ones, and a
  struct body that still matches a secret pattern is dropped, with
  `extensions["osiris.azure.arm"].struct_rejected` naming the rule.
  The capture summary on stderr reports `struct_rejected`.
- **ExpressRoute circuit service keys are no longer emitted.** Earlier
  versions wrote the circuit `serviceKey` (the key handed to the
  connectivity provider to provision the circuit) to
  `extensions["osiris.azure"].service_key` at `--purpose audit`. The
  field is removed, the key is no longer decoded, and `serviceKey` is
  redacted in `--include-raw-body` bodies. Audit documents produced by
  earlier versions for subscriptions with ExpressRoute circuits contain
  the key.
- **Application Insights connection strings** (`InstrumentationKey=`)
  are a secret value pattern: with `--include-raw-body` they are
  redacted wherever they appear (tags, container app environment
  variables, app settings).

---

## [0.5.2] - 2026-08-26

### Changed
- **16 custom `osiris.azure.*` types reclassified to their
  standard OSIRIS JSON type**, per an audit against both
  OSIRIS-JSON-v1.0 chapter 7 and Appendix C.
  Each of these had a direct standard-type equivalent the
  producer was bypassing.

  Resource IDs are unaffected (they are built from the ARM ID alone, not
  the type string). Other `osiris.azure.*` types were
  reviewed against the same criteria and kept as-is each represents
  either a management/container resource that does not itself hold the
  data or workload (e.g. `osiris.azure.sqlserver`, `osiris.azure.sqlmi`,
  `osiris.azure.sqlvm` kept distinct from `compute.vm` since it is
  already wired to the underlying VM resource via its own connection), a
  composite multi-protocol resource with no single standard-type fit
  (`osiris.azure.storage`), or a resource with no still OSIRIS
  standard-type equivalent (route tables, public IPs, DNS,
  bastion, traffic manager, observability, automation, key vault,
  container registry, managed identity, etc.).

### Fixed
- **Invalid `provider.namespace` on every resource:**
  `provider.namespace` was set to the raw ARM provider namespace
  (e.g. `"Microsoft.Compute"`, `"Microsoft.Network"`), which never
  matched the `osiris.*` pattern the OSIRIS JSON schema requires for
  that field and failed `validate --profile strict`.
  Per spec section 4.3.5, `provider.namespace` is reserved for
  `provider.name = "custom"` and does not apply here.
  The field is no longer set the ARM namespace remains fully available
  as the prefix of `provider.type`
  (e.g. `"Microsoft.Network/virtualNetworks"`).

---

## [0.5.1] - 2026-06-14

### Added
- **`--include-raw-body` flag:** When passed together with
  `--purpose audit`, the full ARM response struct for each resource is
  serialised to a JSON string and attached under
  `extensions["osiris.azure.arm"].body`.

### Fixed
- **VNet and route-table collection regression:**
  `az network vnet list` and `az network route-table list` require
  `--resource-group/-g` on some Azure CLI builds and RBAC scopes,
  causing subscriptions to silently skip VNets and route tables.

---

## [0.5.0] - 2026-06-07

### Added
- **Logic App API connections** (`osiris.azure.logic.apiconnection`):
  collects all `Microsoft.Web/connections` resources via
  `az resource list`. Includes API name, display name,
  and connection status.
- **Email Services** (`osiris.azure.emailservice`):
  collects all `Microsoft.Communication/EmailServices` resources
  including provisioning state and data location.
- **Email Domains** (`osiris.azure.emailservice.domain`):
  collects all `Microsoft.Communication/EmailServices/Domains` resources
  including domain management type and mailFrom address.
  Wired to parent email service (`contains/forward`).
- **Stream Analytics jobs** (`osiris.azure.streamanalytics`):
  collects all `Microsoft.StreamAnalytics/streamingjobs` resources
  including job state, SKU, compatibility level, and output start mode.
- **Event Grid system topics** (`osiris.azure.eventgrid.systemtopic`):
  collects all `Microsoft.EventGrid/systemTopics` resources
  including source ARM ID and topic type.
- **App Service slots** (`osiris.azure.webapp.slot`):
  collects deployment slots for each App Service / Function App via
  `az webapp deployment slot list`. Includes kind, state, and site
  config. Wired to parent webapp (`contains/forward`).
- **Arc extension parse fix**:
  removed `autoUpgradeMinorVersion` from the Resource
  Graph projection for Arc extensions ARM returns this as a number
  (0/1) which the JSON decoder rejects as a bool.
  Field is now excluded from collection.
- **`network_acls` in envelope**:
  Key Vault `network_acls` (default_action, bypass, ip_ranges,
  vnet_subnet_ids) is now written to `extensions["osiris.azure"]`
  in addition to `properties`. Previously only went to properties.
- **`default_action` fallback**:
  For KVs with no explicit ACL configured (networkAcls = null),
  `default_action` is derived from `publicNetworkAccess`
  (`Disabled` -> `Deny`, otherwise `Allow`) so all 22/22 vaults
  carry the signal.
- **`min_tls_version` per-vault show**:
  Added `enrichKVWithShow` fallback vaults still missing
  `minimumTlsVersion` after `az keyvault list` + Resource
  Graph are fetched individually via `az keyvault show`.
- **`default_action`**:
  SQL Server has no `networkAcls.defaultAction`; `default_action` is now
  derived from `publicNetworkAccess` (`Disabled` -> `Deny`,
  `Enabled` -> `Allow`) and written to `extensions["osiris.azure"]`.
- **SQL Managed Instance databases** (`osiris.azure.sqlmidatabase`):
  collects all `Microsoft.Sql/managedInstances/databases` via
  `az sql midb list` per instance. Includes collation and provisioning
  state. Wired to parent MI (`contains/forward`).
- **SQL elastic pools** (`osiris.azure.sql.elasticpool`):
  collects all `Microsoft.Sql/servers/elasticPools` via
  `az sql elastic-pool list` per server.
  Includes SKU/tier, capacity, max size, zone redundancy.
  Wired to parent SQL server (`contains/forward`).
- **SQL virtual machines** (`osiris.azure.sqlvm`):
  collects all `Microsoft.SqlVirtualMachine/SqlVirtualMachines` via
  `az sql vm list`. Includes license type, management type, image SKU.
  Wired to underlying Azure VM (`dependency/forward`) via
  `properties.virtualMachineResourceId`.
- **ACR geo-replications** (folded into `osiris.azure.containerregistry`):
  collects `Microsoft.ContainerRegistry/registries/replications` via
  `az acr replication list` per registry.
  Non-home-region replications are folded into
  `extensions["osiris.azure"].replications[]` on the parent
  registry resource.
- **Arc machine extensions** (folded into `osiris.azure.arc.machine`):
  collects `Microsoft.HybridCompute/machines/extensions` via Resource
  Graph query. Extensions are folded into
  `extensions["osiris.azure"].extensions[]` on the parent Arc machine
  resource (name, type, publisher, version, status).
- **Hub and connectivity resource expansion**:
  4 new resource types with connection wiring, plus 3 data-quality fixes
  (NIC primary IP, NSG rule location, ExpressRoute fields).
- **Azure Bastion** (`osiris.azure.bastion`):
  collects all `Microsoft.Network/bastionHosts` resources including SKU,
  scale units, feature flags (tunneling, IP connect, shareable link,
  Kerberos, copy-paste). Wired to its `AzureBastionSubnet`
  (`network/forward`) and public IP (`network/forward`).
- **Traffic Manager** (`osiris.azure.trafficmanager`):
  collects all `Microsoft.Network/trafficManagerProfiles` including
  routing method, DNS config (FQDN, TTL), health monitor settings, and
  endpoint list (name, type, status, target, weight, priority).
  Azure endpoints with a `targetResourceId` are wired to the backing
  resource (`dependency/forward`). External (IP/FQDN) endpoints have no
  OSIRIS JSON target and are recorded in `properties.endpoints` only.
- **DNS Private Resolver** (`osiris.azure.dns.resolver`):
  collects all `Microsoft.Network/dnsResolvers` resources including
  provisioning state, resolver state, and bound VNet ID.
  Wired to its VNet (`network/forward`).
- **DNS Forwarding Ruleset** (`osiris.azure.dns.forwardingruleset`):
  collects all `Microsoft.Network/dnsForwardingRulesets` resources
  including outbound endpoint IDs. Wired to each attached DNS private
  resolver (`dependency/forward`) by stripping `/outboundEndpoints/{name}`
  from the endpoint ARM ID.
- All four new types are included in the metric alert scope resolution
  map so alerts monitoring them produce correct scope edges.
- **Management Group hierarchy:**
  `az account management-group entities list` is called once per run
  (tenant-scoped, best-effort).
  The ancestry chain of the current subscription is emitted as
  `logical.managementgroup` groups ordered root-to-leaf, each with
  `properties.management_group_id`, `properties.tenant_id`, and
  `children` wired to the next MG in the chain. The leaf MG gains the
  subscription group as a child. The `logical.subscription` group gains
  `extensions.osiris.azure.management_group_path` as a JSON array of
  display names ordered root-to-leaf (e.g.
  `["Tenant Root Group","IT Hub", "Production"]`) so consumers can
  determine the subscription's position in the MG tree without
  traversing the group hierarchy. If the call fails (insufficient
  permissions, management-group extension not installed),
  a warning is logged and the run continues without MG groups.
- **VM field depth:** `TransformVMs` now emits `provisioning_state`,
  `vm_id`, `license_type`, `os_type`, `image_publisher`, `image_offer`,
  `image_sku`, `image_version`, `computer_name`, and `nic_count`.
  `provider.zone` is set for zonal VMs. Both the top-level flattened
  `vmSize` (from `az vm list -d`) and the nested
  `hardwareProfile.vmSize` are handled.
- **Azure Monitor Metric Alerts** (`osiris.azure.monitor.metricalert`):
  collected via `az monitor metrics alert list`. Properties: `severity`,
  `enabled`, `evaluation_frequency`, `window_size`, `auto_mitigate`,
  `description`, `target_resource_type`, `scopes`, `criteria`
  (metric name/namespace/operator/ threshold/time-aggregation per
  condition), `action_group_ids`.
- **Azure Monitor Action Groups** (`osiris.azure.monitor.actiongroup`):
  collected via `az monitor action-group list`. Properties:
  `group_short_name`, `enabled`, `receiver_count`,
  `email_receiver_count`, `webhook_receiver_count`.

### Fixed
- **NIC `private_ip`**:
  primary private IP address is now hoisted from
  `ip_configurations[0].private_ip_address` to `properties.private_ip`
  for direct access.
- **NSG `security_rules`**:
  custom security rules moved from `extensions.osiris.azure` to
  `properties.security_rules` (easier consumer access).
  Default (platform) rules remain in
  `extensions.osiris.azure.default_security_rules`. Array field variants
  (`source_port_ranges`, `destination_port_ranges`,
  `source_address_prefixes`, `destination_address_prefixes`) are now
  emitted when ARM uses multi-value arrays.
  Rule `description` field is now also included when non-empty.
- **ExpressRoute `service_key`, `allow_classic_operations`,`enable_direct_port_rate_limit`**:
  three previously missing fields now emitted in
  `extensions.osiris.azure`.
- **Metric Alert criteria unmarshal failure:**
  The Azure Monitor API returns the `criteria`
  field as a discriminated-union object (`{ "odata.type": "...", "allOf": [...] }`)
  rather than a JSON array. The previous struct typed it as
  `[]azMetricAlertCriteria`, causing every `az monitor metrics alert list`
  response to fail with `json: cannot unmarshal object into Go struct field MetricAlert.criteria of type []azure.azMetricAlertCriteria`
  and silently skip the entire resource type. Fixed by introducing
  `azMetricAlertCriteriaContainer` (wrapping `ODataType` + `AllOf []azMetricAlertCriteria`)
  and updating `TransformMetricAlerts` to iterate `a.Criteria.AllOf`.
  Individual criteria are now emitted as a `criteria` array in resource
  properties (metric name/namespace, operator, threshold, time
  aggregation).
- **Application Gateway misclassification:**
  `TransformAppGateways` previously emitted `network.loadbalancer` for
  every AGW. Application Gateway is an L7 reverse proxy / WAF, not a
  load balancer; it is now emitted as `osiris.azure.applicationgateway`.
  Properties added: `sku_name`, `sku_tier`, `capacity`,
  `operational_state`, `listener_count`, `backend_pool_count`,
  `enable_http2`, `waf_enabled`, `waf_mode`. `provider.zone` is set
  from `zones[]` for zonal deployments.
- **Subnet CIDR missing for most subnets:**
  `TransformSubnets` only checked the plural `addressPrefixes` array.
  Most subnets (single-CIDR) use the singular `addressPrefix` field
  returned by the ARM API. The transform now falls back to
  `addressPrefix` and wraps it in a one-element slice so
  `address_prefixes` is always populated.
- **Managed Identity IDs stripped in documentation mode:**
  `clientId`, `principalId`, and `tenantId` were placed in `extensions`
  (stripped by default in documentation mode). They are now emitted in
  `properties` so they appear in all output modes.
- **VNet peering missing `allowVirtualNetworkAccess`:**
  The peering summary embedded in VNet properties now includes
  `allow_virtual_network_access` when true, alongside the existing
  gateway-transit and forwarded-traffic flags.

### Changed
- **Transform layer split - all remaining domains:**
  `transform.go` split into 11 additional domain-specific focused files
  following the Microsoft Azure category taxonomy, completing the
  restructuring started in v0.5.0. New files:
  `transform_compute.go` (VM, Disk, Snapshot),
  `transform_web.go` (App Service Plan, Web App, Function App),
  `transform_storage.go` (Storage Account),
  `transform_security.go` (Key Vault, Container Registry),
  `transform_identity.go` (Managed Identity),
  `transform_observability.go` (App Insights, Log Analytics,
  Metric Alert, Action Group),
  `transform_recovery.go` (Recovery Services Vault, Backup Vault),
  `transform_databases.go` (SQL Server, PostgreSQL, MySQL,
  Cosmos DB, Redis),
  `transform_containers.go` (AKS, Container App, Container Group),
  `transform_integration.go` (Service Bus, Event Hubs, APIM, Front Door),
  `transform_groups.go` (Resource Group, Subscription, Region groups).
  The residual `transform.go` now contains only truly cross-domain
  shared code. No behavior change - identical output for all resource
  and connection types.

---

## [0.4.0] - 2026-04-25

Expands resource and connection coverage on top of the v0.3.0 `--purpose`
foundation. All new resources honour the same contract: collection
remainsexhaustive, emission is shaped by `--purpose`
(documentation is default; full ARM fidelity under `--purpose audit`).

Scope rule applied: OSIRIS JSON is a topology/document interchange
schema, not an IaC/policy format. Monitoring policy (alert rules, action
groups, metric alerts, scheduled query rules) and backup policy
(backup/recovery plans, retention rules) are explicitly OUT of Scope.

### Added - App Service / web tier
- App Service Plan (`Microsoft.Web/serverfarms`) as
  `osiris.azure.appserviceplan`. Captures SKU family/tier/size/capacity,
  Linux flag, per-site scaling, zone redundancy, worker counts and
  hosted site count.
- App Service (`Microsoft.Web/sites`) with kind routing:
  - sites whose `kind` contains `functionapp` are emitted as `osiris.azure.functionapp`
  - all other sites are emitted as `osiris.azure.webapp`
  - Captures state/enabled, default hostname and custom host names,
  HTTPS-only, client-cert mode, hosting plan ID, VNet integration subnet,
  public network access, inbound/outbound IPs, managed-environment
  binding, redundancy mode and site-config fields (Linux/Windows runtime,
  workers, always-on, HTTP/2, min TLS, function scale limit,
  ACR managed identity)
  - `osiris.azure` extensions carry managed identity (type, principal ID,
  user-assigned identity IDs), outbound VNet routing flags,
  private-endpoint connection IDs and the linked Application Insights
  resource (resolved from the Azure portal
  `hidden-link: /app-insights-resource-id` tag)
- Application Security Group (`Microsoft.Network/applicationSecurityGroups`)
  as `osiris.azure.asg`.
- NIC IP configurations now collect referenced ASGs so NIC->ASG
  membership can be wired even though ASGs have no membership
  list of their own.

### Added - data plane and platform services
- Storage Account (`Microsoft.Storage/storageAccounts`) as
  `osiris.azure.storageaccount`. Captures SKU tier/name, kind, access
  tier, HTTPS-only, min TLS, public network access,
  allow-blob-public-access, network ACLs (default action, bypass, IP and
  VNet rules), primary/secondary endpoints, encryption key source and
  Key Vault reference, private endpoint connection IDs.
- Key Vault (`Microsoft.KeyVault/vaults`) as `osiris.azure.keyvault`.
  Captures SKU, tenant ID, soft-delete / purge-protection flags,
  enabled-for (deployment/disk encryption/template deployment), RBAC
  auth mode, network ACLs and bypass, public network access, private
  endpoint connection IDs.
- Container Registry (`Microsoft.ContainerRegistry/registries`) as
  `osiris.azure.containerregistry`. Captures SKU, admin-enabled flag,
  login server, zone redundancy, public network access, data endpoint
  flags and private endpoint connection IDs.
- Managed Identity (`Microsoft.ManagedIdentity/userAssignedIdentities`)
  as `osiris.azure.managedidentity`. Captures tenant ID,
  principal/client identifiers.
- Managed Disk (`Microsoft.Compute/disks`) as `osiris.azure.disk`.
  State derived from attachment: attached disks are `active`, unattached
  are `inactive`. Captures SKU tier, size (GiB), OS type, disk state,
  creation option and source resource ID, attached VM ID (when present),
  zone, tier,  IOPS/throughput, network access policy, public network
  access, data-access authentication mode, encryption type and disk
  encryption set.
- Managed Snapshot (`Microsoft.Compute/snapshots`) as
  `osiris.azure.snapshot`. Captures SKU, size, OS type, disk state,
  creation source and incremental flag.

### Added - backup and disaster recovery
- Recovery Services Vault (`Microsoft.RecoveryServices/vaults`) as
  `osiris.azure.recoveryvault`. Captures SKU name/tier, storage
  redundancy, cross-region restore flag, public network access,
  provisioning state and the protected-item count observed under the
  vault. Private endpoint connection IDs surface as
  an `osiris.azure` extension.
- Backup Vault (`Microsoft.DataProtection/backupVaults`) as
  `osiris.azure.backupvault`. Captures storage settings (datastore type,
  redundancy), immutability state, soft-delete state and retention
  duration, security settings (public network access, monitoring flags)
  and the backup-instance count observed under the vault.

### Added - databases
- SQL Server (`Microsoft.Sql/servers`) as `osiris.azure.sqlserver`.
  Captures version, FQDN, admin login (principal name, not auth material),
  state, public network access, minimal TLS version, outbound-access
  restriction. Private endpoint connection IDs surface as an
  `osiris.azure` extension.
- SQL Database (`Microsoft.Sql/servers/databases`) as
- `osiris.azure.sqldatabase`. Captures SKU name/tier/capacity/family,
  collation, status, max size, zone redundancy, read scale, storage
  account type and the parent server ID. The implicit `master` system
  database is skipped at collection time (not a workload DB).
- PostgreSQL Flexible Server (`Microsoft.DBforPostgreSQL/flexibleServers`)
  as `osiris.azure.postgresqlserver`. Captures version, SKU/tier,
  storage (size/tier/IOPS/auto-grow), HA (mode/standby zone/state),
  availability zone, replication role, FQDN, admin login, public network
  access and delegated subnet ARM ID. Legacy
  `Microsoft.DBforPostgreSQL/servers` (single server) is end-of-life on
  Azure's roadmap and intentionally not modeled.
- MySQL Flexible Server (`Microsoft.DBforMySQL/flexibleServers`) as
  `osiris.azure.mysqlserver`. Same property surface as PostgreSQL
  Flexible Server.
- Cosmos DB account (`Microsoft.DocumentDB/databaseAccounts`) as
  `osiris.azure.cosmosaccount`. Captures kind (GlobalDocumentDB /
  MongoDB / Parse), offer type, document endpoint, public network access,
  automatic-failover, multi-region writes, VNet filter flag, free tier,
  local-auth disabled, default consistency level, capabilities
  (Cassandra / Table / Gremlin / Mongo / Serverless), flattened
  `locations[]` (name, failover priority, zone redundancy),
  virtual network rules (subnet IDs) and private endpoint connection IDs
  under the `osiris.azure` extension.
- Redis Cache (`Microsoft.Cache/Redis`) as `osiris.azure.redis`.
  Captures SKU (Basic/Standard/Premium), family (C or P), capacity,
  Redis version, TLS config, public network access, host name / port /
  SSL port, shard count, replicas-per-master, injected subnet ID
  (Premium VNet injection), static IP and zones.

### Added - containers and integration
- AKS cluster (`Microsoft.ContainerService/managedClusters`) as
  `osiris.azure.aks.cluster`. Captures SKU (Base/Automatic +
  Free/Standard/ Premium tier), Kubernetes version, DNS prefix, FQDN,
  node resource group, RBAC/AAD integration (managed AAD, Azure RBAC
  flag), private-cluster flag and private DNS zone, and a flattened
  `network_profile` (plugin/policy, service/pod CIDR, LB SKU, outbound
  type). Agent pool count is surfaced as a summary. Private endpoint
  connection IDs surface as an `osiris.azure` extension
  (private cluster only).
- AKS agent pool (`Microsoft.ContainerService/managedClusters/agentPools`)
  as `osiris.azure.aks.nodepool`. Captures VM size, count (with
  autoscale min/max when enabled), OS type / SKU, mode (System / User),
  orchestrator version, VNet subnet and pod subnet ARM IDs and
  availability zones.
- Container App managed environment (`Microsoft.App/managedEnvironments`)
  as `osiris.azure.containerapp.environment`. Captures default domain,
  static IP, zone redundancy, and VNet configuration (infrastructure
  subnet ID + internal flag).
- Container App (`Microsoft.App/containerApps`) as
  `osiris.azure.containerapp`. Captures environment binding (resolved
  from either `environmentId` or `managedEnvironmentId` depending on CLI
  version), latest revision name / FQDN, workload profile and ingress
  shape (external/target port/transport/allow-insecure/FQDN).
  Secrets and full revision bodies are out of scope.
- Container Group / ACI (`Microsoft.ContainerInstance/containerGroups`)
  as `osiris.azure.containergroup`. Captures OS type, restart policy,
  SKU, IP address (IP / type / FQDN / DNS label) and the list of
  integrated subnet ARM IDs. Container images, env vars and commands are
  intentionally not emitted - topology models the group, not the
  workload.
- Service Bus namespace (`Microsoft.ServiceBus/namespaces`) as
  `osiris.azure.servicebus.namespace`. Captures SKU name/tier/capacity,
  service endpoint, zone redundancy, disable-local-auth flag, public
  network access, minimum TLS version. Queue / topic / subscription
  enumeration is out of scope. Private endpoint connection IDs surface
  as an `osiris.azure` extension (Premium tier only).
- Event Hubs namespace (`Microsoft.EventHub/namespaces`) as
  `osiris.azure.eventhubs.namespace`.
  Same property surface as Service Bus.
- API Management service (`Microsoft.ApiManagement/service`) as
  `osiris.azure.apim`. Captures SKU name/capacity (Developer / Basic /
  Standard / Premium / StandardV2 / PremiumV2), gateway / portal /
  management URLs, VNet integration type (None / External / Internal)
  and subnet ARM ID, public / private IP addresses, disable-gateway flag.
  Individual APIs, operations, products and policy documents are out of
  scope they are policy / IaC, not topology.
- Front Door profile (`Microsoft.Cdn/profiles` with
  Standard_AzureFrontDoor / Premium_AzureFrontDoor SKU) as
  `osiris.azure.frontdoor.profile`. Captures SKU, kind, provisioning /
  resource state and the Front Door ID. Routes, rules, endpoints and
  WAF policy associations are routing configuration, not topology and
  are intentionally omitted. Classic Azure Front Door
  (`Microsoft.Network/frontDoors`, deprecated) is not modeled.

### Added - observability
- Application Insights (`Microsoft.Insights/components`) as
  `osiris.azure.applicationinsights`. Captures kind, application type,
  ingestion mode, retention, sampling percentage, public network access
  (ingestion/query), IP masking and local-auth flags. The bound Log
  Analytics workspace ARM ID is surfaced under the `osiris.azure`
  extension as `workspace_resource_id`. Classic (non-workspace)
  components are still collected but have no workspace extension.
- Log Analytics workspace (`Microsoft.OperationalInsights/workspaces`)
  as `osiris.azure.loganalytics`.
  Captures SKU, retention, public network access (ingestion/query),
  CMK-required-for-query flag and daily ingestion cap. The `customer_id`
  (workspace UUID used by KQL) is surfaced under the
  `osiris.azure` extension.

### Added - connection layers
- App Service Plan `contains` web/function app
- Web/function app `network` -> VNet integration subnet
- Private Endpoint `network` -> hosted web/function app (sourced from
  the site's `privateEndpointConnections` array)
- NIC `network` -> Application Security Group (sourced from each IP
  configuration's `applicationSecurityGroups` array)
- Private Endpoint `network` -> Storage Account
- Private Endpoint `network` -> Key Vault
- Private Endpoint `network` -> Container Registry
- Snapshot `contains` source Disk (direction reversed so the source disk
  is the parent and snapshots appear as children)
- Disk `contains` attached VM (direction reversed so the VM is the
  parent and its attached managed disks appear as children)
- App Insights `network` -> Log Analytics workspace (workspace-based
  components only; classic AI is skipped)
- Web App / Function App `network` -> Application Insights (sourced from
  the `hidden-link: /app-insights-resource-id` portal tag)
- Private Endpoint `network` -> Recovery Services Vault
- Protected resource `network` -> Recovery Services Vault (VM, disk,
  storage, Key Vault, ACR, Web App, snapshot sources resolved from each
  vault's `backup item list`; orphan / unknown sources silently skipped)
- Protected resource `network` -> Backup Vault (datasource resolved from
  each vault's `dataprotection backup-instance list`; orphans skipped)
- SQL Server `contains` SQL Database (per-server `az sql db list`
  iteration; `master` system DB skipped)
- Private Endpoint `network` -> SQL Server
- Private Endpoint `network` -> Cosmos DB account
- Private Endpoint `network` -> Redis cache (Premium tier only; Basic /
  Standard tiers have no PE support and silently skip)
- PostgreSQL / MySQL flexible server `network` -> delegated subnet
  (VNet-integrated deployments; public-access servers skipped)
- Redis cache `network` -> injected subnet (Premium VNet injection only)
- AKS cluster `contains` agent pool (direction forward; one
  parent-to-child edge per node pool)
- AKS agent pool `network` -> delegated subnet (kubenet clusters without
  a BYO subnet silently skip)
- Private Endpoint `network` -> AKS cluster (private cluster only)
- Container App Environment `contains` Container App (sourced from the
  app's `environmentId` / `managedEnvironmentId`)
- Container App Environment `network` -> infrastructure subnet
  (VNet integrated environments only)
- ACI Container Group `network` -> subnet (VNet-integrated ACI only;
  public-IP groups silently skip)
- Private Endpoint `network` -> Service Bus namespace (Premium tier only)
- Private Endpoint `network` -> Event Hubs namespace
- Private Endpoint `network` -> API Management service
- API Management service `network` -> subnet (External / Internal modes
  only; `None` mode services silently skip)

### Added
- Private Endpoint `private_link_service_connection` is now emitted as a
  structured property (`{ group_id, private_link_service_id }`) draw
  from the ARM `privateLinkServiceConnections[0]`. This allows future
  OSIRIS JSON consumers to identify the PaaS target service type
  (blob, vault, registry, etc.) and to link each PE to its exact
  target resource.
- Private Endpoint `custom_dns_configs` array is emitted when present so
  future OSIRIS JSON consumers can verify DNS-integration completeness.
- `container.region` groups [OSIRIS JSON spec chapter 6.2](https://github.com/osirisjson/osiris/blob/main/specification/v1.0/OSIRIS-JSON-v1.0.md#62-group-types)
  are emitted one group per distinct `provider.region` observed across
  the subscription's resources, membering every resource in that region.
  Region `global` and empty-region resources are skipped (they are not
  geographically scoped). Boundary token is `<subscription-id>/<region>`
  so groups never collide across subscriptions.

### Changed
- Connection `type` is now emitted with
  [OSIRIS JSON spec chapter 5.2.3](https://github.com/osirisjson/osiris/blob/main/specification/v1.0/OSIRIS-JSON-v1.0.md#523-standard-connection-types-v10)
  so downstream future OSIRIS JSON consumers can tell topology edges apart:
  - VNet peering edges: `network` -> `network.peering`
  - VPN gateway connections: `network` -> `network.vpn`
  - ExpressRoute circuit connections: `network` -> `network.bgp`
  - Private Endpoint -> target PaaS edges (WebApp, Function App, KV, ACR,
  RSV, SQL Server, Cosmos DB, Redis, AKS, Service Bus, Event Hubs, APIM): `network` -> `dependency`
  - Private Endpoint -> Storage Account: `network` -> `dependency.storage`
  - Private Endpoint -> database (SQL Server, Cosmos DB, Redis):
    `network` -> `dependency.database` where applicable
- Key Vault `purge_protection` renamed to `purge_protection_enabled`
  (matches Azure ARM field name and OSIRIS JSON convention for
  boolean flags).
- Key Vault `network_acls` is now emitted as a top-level property rather
  than nested under `osiris.azure` extensions - it mirrors the Storage
  Account `network_acls` shape and is needed for compliance BP checks
  without `--purpose audit`.
- Log Analytics Workspace `retention_days` renamed to `retention_in_days`
  to match the ARM source field exactly.

### Fixed
- resource `provider.region` and `metadata.scope.regions` are now
  canonicalized to Azure's slug form (lowercase, no spaces).
  The `az` CLI returns `location` inconsistently - most ARM resources
  use `westeurope`, `eastus2` etc. while App Service Plans and Web Apps
  surface the display name (`West Europe`). Before this fix, a
  single-region subscription could emit `["westeurope", "West Europe"]`
  in its scope regions, which looked like two regions into the same
  OSIRIS JSON document.
- managed disk collection now iterates per resource group instead of
  calling `az disk list` subscription-wide. Some Azure CLI
  configurations reject the global form with `ERROR: the following arguments are required: --resource-group/-g`,
  which was silently swallowed by the graceful-skip path and left every
  managed disk (and every Disk->VM / Snapshot->Disk edge)
  missing from the document.
- gateway connections pointing at a peer in a different subscription
  (typically a central connectivity hub owning the ExpressRoute circuit)
  no longer break the document build. Previously `TransformGatewayConnections`
  fabricated a synthetic target ID for the out-of-scope peer, which the
  SDK's resource-existence invariant rejected with `target ... does not reference an existing resource`. 
  The transform now emits a stub resource for the cross-subscription
  peer mirroring the VNet peering pattern preserving the topology edge
  while keeping the document valid. Supported stub types:
  `Microsoft.Network/expressRouteCircuits`,
  `Microsoft.Network/virtualNetworkGateways`,
  `Microsoft.Network/localNetworkGateways`.

### Changed
- `az` collection is extended with `appservice plan list`, `webapp list`
  `network asg list`, `storage account list`, `keyvault list`, `acr list`
  `identity list`, `disk list`, `snapshot list`,
  `resource list --resource-type microsoft.insights/components`,
  `monitor log-analytics workspace list`, `backup vault list`,
  `dataprotection backup-vault list`, per-vault `backup item list`,
  `dataprotection backup-instance list`, `sql server list`, per-server
  `sql db list`, `postgres flexible-server list`,
  `mysql flexible-server list`, `cosmosdb list`, `redis list`, `aks list`
  with per-cluster `aks nodepool list`, `containerapp env list`,
  `containerapp list`, `container list`, `servicebus namespace list`,
  `eventhubs namespace list`, `apim list` and `afd profile list`.

### Security
- App Insights `InstrumentationKey`, `ConnectionString` and `AppId` are
  never emitted (authentication material). Log Analytics
  `primarySharedKey` / `secondarySharedKey` are never collected.
  The workspace `customerId` IS emitted it is the query-scope UUID used
  by KQL and is not a secret.
- Database admin passwords (SQL / PostgreSQL / MySQL) are never
  collected; only the admin login **name** (a principal identifier, not
  a credential) is emitted. Cosmos DB primary / secondary keys and
  connection strings are never collected - the OSIRIS JSON Azure
  producer does not call `listKeys` or `listConnectionStrings`.
  Redis primary / secondary access keys are never collected the OSIRIS
  JSON Azure producer does not call `redis list-keys`.
- Service Bus / Event Hubs namespace access keys and connection strings
  are never collected the OSIRIS JSON Azure producer does not call
  `servicebus namespace authorization-rule keys list` or the Event Hubs
  equivalent. APIM subscription keys and named values are similarly not
  collected. Container App secrets (configuration.secrets array) and ACI
  container environment variables are never emitted.

> [!NOTE]
>  - Private endpoint fan-out to Storage / Key Vault /
> Container Registry / WebApp / Recovery Services Vault is factored
> through a shared helper so additional PE-bound target types in later
> releases can reuse the same wiring.
>  - Per-resource diagnostic-setting enumeration (VM/AKS/WebApp/etc -> workspace)
> is deliberately not included. Wiring every ARM resource's diagnostic
> settings requires an extra `az monitor diagnostic-settings list --resource <id>`
> call per resource and materially extends collection time for large
> subscriptions. Only cheap, directly-derivable edges are emitted
> (workspace-based AI -> LA, WebApp -> AI, PE -> storage/KV/ACR).
>  - Backup protected-item enumeration is per-vault (one `az` call per
> Recovery Services Vault, one per Backup Vault) because there is no
> subscription-wide list API. Collection cost scales with vault count,
> not subscription size. Backup/retention **policies** themselves are
> out of scope per the OSIRIS JSON topology/documentation versus IaC
> model rule at the top of this section.
>  - SQL database enumeration is per-server (one `az sql db list` call
> per SQL server); PG / MySQL flexible servers and Cosmos accounts
> expose their databases via their own data-plane APIs and are
> intentionally not drilled into the server / account is the topology
> edge, the workload databases inside are an application concern.
> SQL is the exception because database-tier sizing (SKU) and
> zone-redundancy are topology attributes, not application settings.
>  - Database auditing / threat-detection / TDE / firewall-rules /
> security-alert-policy objects are **policy** and out of scope per the
> OSIRIS JSON topology/documentation versus IaC model. Firewall rules
> and VNet rules on Cosmos are exceptions worth surfacing only as a
> count or subnet-ID list because they describe a topology edge, not
> a retention policy.
>  - AKS node pools are enumerated per-cluster
> (one `az aks nodepool list --cluster-name` call per AKS cluster)
> because the cluster's flat `properties.agentPoolProfiles` does not
> carry the pool ARM IDs needed for cluster -> nodepool `contains` edges.
>  - APIM operation policies, product subscriptions and
> Front Door routes / rule sets / WAF policies are routing / admission
> configuration, not topology, and are intentionally omitted per the
> OSIRIS JSON topology/documentation versus IaC model.
> Event Grid topics / Storage queues are in the same category as the
> "workload-under-service" rule used for SQL databases vs Cosmos DBs
> they belong to application configuration and are deferred until there
> is a concrete OSIRIS JSON topology/documentation reason to surface them.

---

## [0.3.0] - 2026-04-20

Adopted the `pkg/osirismeta` `--purpose` contract. Collection stays
exhaustive, but emission is shaped by a declared purpose (documentation
default, audit for full ARM fidelity). Lifted every existing Azure
resource to its full-fidelity shape.

> [!WARNING]
> **Breaking default.** Documents produced by 0.3.0 without an explicit
> `--purpose` flag are smaller than those produced by 0.2.x: `properties`
> and `extensions` maps are stripped from resources, connections and
> groups. Consumers that depend on the rich payload must run with
> `--purpose audit` to reproduce the previous output.

### Added
- `--purpose {documentation|audit}` flag with validation via
  `osirismeta.ParsePurpose`, wired through single, CSV, `--all`
  and interactive modes.
- `metadata.generator.url` set to `https://osirisjson.org`.
- `metadata.scope.name` set to "SubscriptionID - SubscriptionName".
- `metadata.scope.environments` populated from CSV `environment` column.
- `provider.source` set to `azure-cli`.
- Resource tags now collected and emitted for all resource types (VNets,
  NICs, NSGs, route tables, public IPs, LBs, private endpoints, VNet
  gateways, NAT gateways, firewalls, app gateways, ExpressRoute, VMs).
- NSG security rules emitted as `osiris.azure` extensions (name,
  priority, direction, access, protocol, source/destination).
- NSG default security rules emitted as `osiris.azure` extensions.
- NSG `subnet_ids` and `nic_ids` associations in properties.
- NIC `subnet_id` in each `ip_configurations` entry, `nsg_id`,
  `enable_ip_forwarding`, `primary` in properties.
- NIC `enable_accelerated_networking` as `osiris.azure` extension.
- NIC effective routes collected and emitted as `osiris.azure`
  extensions (source, state, prefix, next_hop_type, next_hop_ip) requires
  `effectiveRouteTable/action` permission, gracefully skipped with
  INFO log if read-only.
- Subnet `route_table_id`, `nsg_id`, `nat_gateway_id`, `delegations`
  in properties.
- Route table `subnets` back-reference (associated subnet ARM IDs)
  in properties.
- VNet `subnet_count` and `enable_ddos_protection` in properties.
- ExpressRoute circuit `circuit_state`, `provider_state`, `bandwidth_mbps`,
  `peering_location` in properties; `sku`, `sku_tier`, `service_provider`
  as `osiris.azure` extensions.
- ExpressRoute circuit peerings (BGP details) collected per circuit and
  emitted as `osiris.azure` extensions (name, peering_type, state,
  peer_asn, vlan_id, address prefixes).
- VNet peerings array in VNet properties (name, peering_state,
  remote_vnet_id, gateway transit flags, allow_forwarded_traffic).
- VNet gateway `sku`, `active_active`, and `connections[]` array in
  properties (name, connection_type, peer_id).
- Private DNS zone `virtual_network_links` array with VNet ID, name,
  registration status in properties.

### Changed
- Generator version `0.2.2` -> `0.3.0`.
- Default emission is now `documentation` grade compliant per
  [OSIRIS JSON spec 13.1.3](https://github.com/osirisjson/osiris/blob/main/specification/v1.0/OSIRIS-JSON-v1.0.md#1313-data-minimization)
  `properties` and `extensions` are stripped from resources,
  connections and groups unless `--purpose audit` is passed.
  Collection itself is unchanged and remains exhaustive.
- Removed `--detail` flag, all data aligned to user RBAC collected by
  default per [OSIRIS JSON spec 11.3.1](https://github.com/osirisjson/osiris/blob/main/specification/v1.0/OSIRIS-JSON-v1.0.md#1131-best-practices-for-producers)
  (route table routes, LB rules/pools, public IP SKU tier are
  always included).
- NIC `nsg_id` emits explicit `null` when no NSG is attache
  (was omitted).
- Private DNS zone VNet links now collected via per-zone API call
  (was returning 0 links from list).

---

## [0.2.1] - 2026-04-06

### Fixed
- Resolve Azure CLI (`az`) binary path once via `exec.LookPath` instead
  of relying on bare command name per invocation.
  Fixes [CWE-426](https://cwe.mitre.org/data/definitions/426)
  untrusted search path.

---

## [0.2.0] - 2026-04-06

Initial Microsoft Azure producer release.

### Added
- Full fetch of subscription topology via Azure CLI (`az`).
- Collects VNets, subnets, NICs, NSGs, route tables, public IPs, load
  balancers, private endpoints, VNet gateways, NAT gateways, firewalls,
  app gateways, DNS zones, ExpressRoute circuits, VMs.
- Cross-subscription VNet peering stubs with `provider.type` and
  `provider.subscription`.
- Resource group resources (`container.resourcegroup`) per [OSIRIS spec Appendix C.5](https://github.com/osirisjson/osiris/blob/main/specification/v1.0/OSIRIS-JSON-v1.0.md#c5-container-and-organization-resources).
- Resource group and subscription group hierarchy.
- `provider.type` populated with native ARM resource types on all
  resources.
- Interactive subscription picker when no flags provided.
- CSV batch mode, multi-subscription, and auto-discover (`--all`) modes.
- Output batch hierarchy:
  `<output>/<TenantID>/<timestamp>/<SubscriptionName>.json`.
- Output single filename convention:
  `microsoft-azure-<timestamp>-<SubscriptionName>.json`.

[Unreleased]: ../../../CHANGELOG.md
[0.5.2]: ../../../CHANGELOG.md#070---2026-08-29
[0.6.0]: ../../../CHANGELOG.md#090---2026-11-08
[0.5.0]: ../../../CHANGELOG.md#050---2026-04-29
[0.4.0]: ../../../CHANGELOG.md#040---2026-04-25
[0.3.0]: ../../../CHANGELOG.md#030---2026-04-20
[0.2.1]: ../../../CHANGELOG.md#021---2026-04-06
[0.2.0]: ../../../CHANGELOG.md#020---2026-04-06