# Changelog

Release-level index for the `go.osirisjson.org/producers` Go module.

The format follows:
- [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Producer versioning follows:
- [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

To maintain a consistent line width across `CHANGELOG.md` and all file
comments (excluding links, code examples, and tables), we adhere to:
- [RFC 7994](https://datatracker.ietf.org/doc/html/rfc7994#section-4.3)

A single git tag covers the entire Go module (Go's proxy resolves
`@latest` to the highest SemVer tag on the module). Each producer keeps
its own behavioral version (`metadata.generator.version` in emitted
documents) in its own per-producer `CHANGELOG.md`. This file lists which
producer behavior versions shipped under each module tag.

- OSIRIS JSON producer for Amazon AWS:
  [`osiris/hyperscalers/aws/CHANGELOG.md`](osiris/hyperscalers/aws/CHANGELOG.md)
- OSIRIS JSON producer for Microsoft Azure:
  [`osiris/hyperscalers/azure/CHANGELOG.md`](osiris/hyperscalers/azure/CHANGELOG.md)
- OSIRIS JSON producer for Cisco APIC:
  [`osiris/network/cisco/apic/CHANGELOG.md`](osiris/network/cisco/apic/CHANGELOG.md)
- OSIRIS JSON producer for Cisco IOS-XE:
  [`osiris/network/cisco/iosxe/CHANGELOG.md`](osiris/network/cisco/iosxe/CHANGELOG.md)
- OSIRIS JSON producer for Cisco NX-OS:
  [`osiris/network/cisco/nxos/CHANGELOG.md`](osiris/network/cisco/nxos/CHANGELOG.md)
- OSIRIS JSON producer for Cisco vManage:
  [`osiris/network/cisco/vmanage/CHANGELOG.md`](osiris/network/cisco/vmanage/CHANGELOG.md)
- OSIRIS JSON producer for HPE Aruba Networking Central:
  [`osiris/network/hpe/arubacentral/CHANGELOG.md`](osiris/network/hpe/arubacentral/CHANGELOG.md)

For changes to the OSIRIS JSON Producer SDK architectural guidelines and
documentation, see [`docs/guidelines/v1.0/CHANGELOG.md`](docs/guidelines/v1.0/CHANGELOG.md).
For changes to the OSIRIS JSON specification, core documents and 
OSIRIS JSON core schema itself, see the [OSIRIS JSON Repository](https://github.com/osirisjson/osiris).

---

## [Unreleased]

## [0.8.0] - 2026-09-01

Cisco APIC producer resource-model, identity, transport,
partial-coverage and topology-graph overhaul (behavior version 0.2.0,
its first `generatorVersion` bump since the initial release).
No other producer changed.

| Producer | Behavior version |
|----------|------------------|
| Amazon Web Services OSIRIS JSON producer | 0.1.1 (no change) |
| Microsoft Azure OSIRIS JSON producer | 0.5.2 (no change) |
| Cisco APIC OSIRIS JSON producer | [0.2.0](osiris/network/cisco/apic/CHANGELOG.md#020---2026-09-01) |
| Cisco IOS-XE OSIRIS JSON producer | 0.1.0 (no change) |
| Cisco NX-OS OSIRIS JSON producer | 0.2.0 (no change) |
| Cisco vManage OSIRIS JSON producer | 0.1.0 (no change) |
| HPE Aruba Networking Central OSIRIS JSON producer | 0.1.0 (no change) |

### Highlights (Cisco APIC 0.2.0)
- **Core taxonomy and spec-form identity:** leaf/spine fabric nodes are
  `network.switch`, the APIC controller is `compute.server` (or
  `compute.vm` for a vAPIC), ACI endpoints are `network.interface`, and
  subnets carry `cidr` plus `gateway_ip`. Resource IDs move from an
  opaque hash to `cisco.apic::<apic-dn>` (OSIRIS-JSON-v1.0 2.1.2
  namespaced-native-ID), so an object's ID no longer depends on the
  controller alias it was collected through. `provider.name` and
  `metadata.scope.providers` become `cisco.apic`.
- **Physical topology graph (the producer emitted zero connections
  before):** `l1PhysIf` enriched by `ethpmPhysIf` becomes
  `network.switch.port` resources wired to their node by `contains`;
  `fabricLink`, LLDP and CDP are merged into one `physical.ethernet`
  connection per link with a `discovered_by` provenance list; an
  off-fabric neighbour gets a minimal `network.switch` stub so the
  connection resolves.
- **Truthful, partial-tolerant collection:** typed `imdata` envelope
  decoding, pagination that advances on source-object count (a malformed
  object can no longer truncate a page), and a criticality-based
  discovery plan (fabric-identity and tenant-model failures abort;
  enrichment failures degrade to a partial document). Every controller
  resource carries a `coverage` extension, and
  `metadata.scope.description` carries a one-line prose summary of what
  was and was not represented.
- **Hardened transport and credential safety:** per-request and overall
  timeouts, SIGINT/SIGTERM cancellation, bounded response bodies,
  classified retry, JSON-built login/logout bodies, `aaaLogout` on exit,
  and an `--insecure` warning. The inline `-p`/`--password` flag is gone
  (`--secrets-file` plus env var plus prompt); `--detail` becomes
  `--purpose documentation|audit` following OSIRIS JSON specification.
- **`--purpose audit`** adds `fvCEp`/`fvIp` endpoints with joined
  `ip_addresses`, endpoint-to-port wiring, and `fvRsPathAtt` EPG path
  attachments as group members. `--help` is single-sourced from the real
  flag set.

**Breaking** to every already-emitted APIC resource `id`, to
`provider.name`/`metadata.scope.providers`, and to the `type` of fabric
nodes, the APIC controller, subnets and endpoints. `--include-raw-body`
is accepted but inert until a later release.

See the [Cisco APIC 0.2.0 entry](osiris/network/cisco/apic/CHANGELOG.md#020---2026-09-01)
for the full list of changes and known limitations.

---

## [0.7.0] - 2026-08-29

First shipped release of the Cisco Catalyst SD-WAN Manager (vManage)
OSIRIS JSON producer (behavior version 0.1.0); the Cisco NX-OS
producer's first behavior-version bump since its initial release a
resource-model, identity, transport and CLI overhaul (0.2.0); and
Microsoft Azure schema-conformance fixes (0.5.2). The shared
`osiris/network/cisco/run` orchestrator is retired (CLI-only change for
Cisco APIC and IOS-XE, no emitted-document change).

| Producer | Behavior version |
|----------|------------------|
| Microsoft Azure OSIRIS JSON producer | [0.5.2](osiris/hyperscalers/azure/CHANGELOG.md#052---2026-08-26) |
| Amazon Web Services OSIRIS JSON producer | 0.1.1 (no change) |
| Cisco APIC OSIRIS JSON producer | 0.1.0 (CLI restructure only, no osiris document output change) |
| Cisco IOS-XE OSIRIS JSON producer | 0.1.0 (CLI restructure only, no osiris document output change) |
| Cisco NX-OS OSIRIS JSON producer | [0.2.0](osiris/network/cisco/nxos/CHANGELOG.md#020---2026-08-29) |
| Cisco vManage OSIRIS JSON producer | [0.1.0](osiris/network/cisco/vmanage/CHANGELOG.md#010---2026-08-09) (initial release) |
| HPE Aruba Networking Central OSIRIS JSON producer | 0.1.0 (no change) |

### Highlights (Microsoft Azure 0.5.2)
- **Removed the invalid `provider.namespace` field** emitted on every
  resource it was set to the raw ARM namespace (e.g.
  `"Microsoft.Compute"`), which never matched the `osiris.*` pattern the
  schema requires there and failed `validate --profile strict`; per spec
  4.3.5 that field is reserved for `provider.name = "custom"`. The ARM
  namespace is still available as the prefix of `provider.type`.
- **`osiris.azure.*` resource types reclassified** to their
  standard OSIRIS JSON type, per an audit against chapter 7 and Appendix
  C. **Breaking** to the emitted `type` field on those resources;
  resource IDs are unaffected (built from the ARM ID, not the type
  string).

### Highlights (Cisco NX-OS 0.2.0)
- **Core taxonomy instead of custom types:** the switch is
  `network.switch` (not `osiris.cisco.switch.leaf`/`.spine`), physical
  ports are `network.switch.port`, port-channels/loopbacks/SVIs are
  `network.interface`, and VLANs are `network.vlan` resources with
  `network.l2` port-membership connections (previously `topology.groups`
  entries).
- **Stable, spec-form identity:** resource IDs are
  `cisco.nxos::<chassis-serial>[/<sub-resource>]`
  (OSIRIS-JSON-v1.0 2.1.2 namespaced-native-ID), derived from the
  device's own serial, so renaming a target in inventory no longer
  changes any ID. `provider.name` and `metadata.scope.providers` become
  `cisco.nxos`.
- **`--purpose documentation|audit`** replaces the initial `--detail`
  flag, matching every other producer. `audit` adds BGP/OSPF neighbor
  sessions, an `osiris.cisco.aaa` posture resource, and
  chassis/PSU/fan/transceiver detail; `--include-raw-body` (audit-only)
  attaches each command's raw response after a secret-redaction pass.
- **Truthful, isolated collection:** typed NX-API decoding, per-command
  failure isolation (one failed command no longer erases unrelated
  domains), a `coverage` extension recording every command's
  attempted/succeeded/failed status, hardened transport (timeouts,
  bounded bodies, classified retries), and no inline `-p`/`--password`.
- **More topology:** merged LLDP + CDP neighbor discovery, switch
  `contains` connections to its own ports, interface IP / native-VLAN /
  trunk-VLAN properties, and a single-sourced `--help`.

**Breaking** to every already-emitted NX-OS resource ID, several
resource types, `provider.name`, and the `--detail` flag. Field names
for `show vpc brief` / `show vpc peer-keepalive` remain unverified
against a real switch.

### Highlights (Cisco vManage 0.1.0)
- **Full WAN edge site topology:** devices (controllers and WAN edges),
  interfaces, SD-WAN tunnels, OMP control-plane peering, and a
  `physical.room` group per site with geo-coordinates.
- **One document per site:** a single controller login fans out into one
  OSIRIS JSON document per WAN edge site, grouped by device site-id.
- **No inline password:** there is deliberately no `-p`/`--password`
  flag. Host/username/password each fall back through flag, then
  `--secrets-file`, then an interactive prompt a bare
  `osirisjson-producer cisco vmanage` with no flags works end to end.
- **`--include-raw-body`:** opt-in lossless fallback for `--purpose
  audit` runs. Each collected endpoint's raw response body is attached
  to the owning device resource, so any field not yet promoted to a
  typed property is immediately accessible without extra API calls.
- **Single-sourced `--help`:** the flag table shown by `--help` and the
  one shown as a parse-error fallback are generated from the same flag
  registration, so they can never drift apart.

### Cisco APIC / IOS-XE (CLI only, `generatorVersion` stays 0.1.0)
The shared `osiris/network/cisco/run` orchestrator
(`ParseFlags`/`RunConfig`/`RunBatch`/`ProducerFactory`) is retired;
`apic` and `iosxe` each own their `flags.go`/`config.go`/`dispatch.go`
like `nxos`/`vmanage`. `-p`/`--password` is removed in favour of
`--secrets-file` + env var + prompt; `--detail` becomes
`--purpose documentation|audit`; `template --generate` moves under the
subcommand. **No change to emitted documents.**

See the [Cisco vManage 0.1.0 entry](osiris/network/cisco/vmanage/CHANGELOG.md#010---2026-08-09),
the [Cisco NX-OS 0.2.0 entry](osiris/network/cisco/nxos/CHANGELOG.md#020---2026-08-29),
and the [Microsoft Azure 0.5.2 entry](osiris/hyperscalers/azure/CHANGELOG.md#052---2026-08-26)
for the full list of changes and known limitations.

---

## [0.6.4] - 2026-07-23

HPE Aruba Networking Central OSIRIS JSON producer.

| Producer | Behavior version |
|----------|------------------|
| HPE Aruba Networking Central OSIRIS JSON producer | [0.1.0](osiris/network/hpe/arubacentral/CHANGELOG.md#010---2026-07-23) (initial release) |
| Amazon Web Services OSIRIS JSON producer | 0.1.1 (no change) |
| Microsoft Azure OSIRIS JSON producer | 0.5.1 (no change) |
| Cisco APIC OSIRIS JSON producer | 0.1.0 (no change) |
| Cisco IOS-XE OSIRIS JSON producer | 0.1.0 (no change) |
| Cisco NX-OS OSIRIS JSON producer | 0.1.0 (no change) |

### Highlights (HPE Aruba Networking Central 0.1.0)
- **Full site topology:** switches, access points, gateways and their
  interfaces/ports, VLANs, stacks, VSX, WLANs, radios, BSSIDs,
  IAP swarms, unified wired/wireless clients, sites, device
  groups, and device-to-device neighbor adjacency (LLDP/CDP-like).
- **Two credential types:** classic API Gateway OAuth2 and self-service.
- **`--all` flag:** auto-discovers and exports every accessible site
  non-interactively, ideal for automated cron/CI use.
- **Multi-site output:** a run spanning two or more sites (`--all`, a
  `--site` list, or a numeric multi-pick) writes one OSIRIS JSON
  document per site; `-o`/`--output` becomes an organized
  output directory in that case.

See the [HPE Aruba Networking Central 0.1.0 entry](osiris/network/hpe/arubacentral/CHANGELOG.md#010---2026-07-23)
for the full list of resources, properties, fixes and known limitations.

---

## [0.6.3] - 2026-06-24

AWS SSO auto-refresh fix for single and interactive modes.

| Producer | Behavior version |
|----------|------------------|
| Amazon Web Services OSIRIS JSON producer | [0.1.1](osiris/hyperscalers/aws/CHANGELOG.md#011---2026-06-24) |
| Microsoft Azure OSIRIS JSON producer | 0.5.1 (no change) |
| Cisco APIC OSIRIS JSON producer | 0.1.0 (no change) |
| Cisco IOS-XE OSIRIS JSON producer | 0.1.0 (no change) |
| Cisco NX-OS OSIRIS JSON producer | 0.1.0 (no change) |

### Fixed
- **AWS**: single and interactive modes now run the same preflight SSO
  credential check that batch mode runs. When the session is expired,
  the producer automatically triggers `aws sso login` instead of failing
  with a manual re-authentication error.

---

## [0.6.2] - 2026-06-23

Homebrew packaging fix. No producer behavior changes.

| Producer | Behavior version |
|----------|------------------|
| Amazon Web Services OSIRIS JSON producer | 0.1.0 (no change) |
| Microsoft Azure OSIRIS JSON producer | 0.5.1 (no change) |
| Cisco APIC OSIRIS JSON producer | 0.1.0 (no change) |
| Cisco IOS-XE OSIRIS JSON producer | 0.1.0 (no change) |
| Cisco NX-OS OSIRIS JSON producer | 0.1.0 (no change) |

### Fixed
- **Release**: place Homebrew formula in `Formula/` subdirectory to
  satisfy Homebrew 4.x tap trust requirement.

---

## [0.6.1] - 2026-06-22

Security hardening. No producer behavior changes.

| Producer | Behavior version |
|----------|------------------|
| Amazon Web Services OSIRIS JSON producer | 0.1.0 (no change) |
| Microsoft Azure OSIRIS JSON producer | 0.5.1 (no change) |
| Cisco APIC OSIRIS JSON producer | 0.1.0 (no change) |
| Cisco IOS-XE OSIRIS JSON producer | 0.1.0 (no change) |
| Cisco NX-OS OSIRIS JSON producer | 0.1.0 (no change) |

### Fixed
- **AWS**: resolve `aws` CLI path via `exec.LookPath` in
  `initiateSSOLogin`. Fixes
  [CWE-426](https://cwe.mitre.org/data/definitions/426).
- **AWS**: resolve browser launcher (`xdg-open`, `open`, `rundll32`) via
  `exec.LookPath` in `openBrowser`. Fixes
  [CWE-426](https://cwe.mitre.org/data/definitions/426).
- **CI**: pin all GitHub Actions steps to full commit SHAs in
  `release.yml` and `sonarqube.yml`.

---

## [0.6.0] - 2026-06-22

Amazon Web Services OSIRIS JSON producer.

| Producer | Behavior version |
|----------|------------------|
| Amazon Web Services OSIRIS JSON producer | [0.1.0](osiris/hyperscalers/aws/CHANGELOG.md#010---2026-06-22) |
| Microsoft Azure OSIRIS JSON producer | 0.5.1 (no change) |
| Cisco APIC OSIRIS JSON producer | 0.1.0 (no change) |
| Cisco IOS-XE OSIRIS JSON producer | 0.1.0 (no change) |
| Cisco NX-OS OSIRIS JSON producer | 0.1.0 (no change) |

### Highlights (AWS 0.1.0)
- **Full account topology GA:** 50 distinct resource types across
  compute (EKS, ECS, EC2, Lambda, ASG), data (RDS, DynamoDB,
  ElastiCache, SQS, Kinesis, MSK, DocDB, Neptune, Redshift, OpenSearch,
  MemoryDB), storage (EBS, S3, EFS, FSx), security (KMS, SecretsManager,
  WAFv2, ECR), identity (IAM roles, OIDC/SAML providers), observability
  (CloudWatch, Backup), networking completeness (ELBv2 listeners,
  CloudFront, API Gateway, Direct Connect LAGs, VPC PrivateLink),
  serverless (Step Functions), and event-driven (SNS, EventBridge) on
  top of the full networking foundation.
- **Built-in secret scanner:** fail-closed pre-flight scan on every
  document; sensitive key names and credential value patterns detected
  and either redacted or blocked before any file is written.
- **`metadata.coverage` transparency block:** every file records
  `services_attempted`, `services_succeeded`, and `errors[]` so
  consumers can distinguish empty-because-nothing-there from
  empty-because-error.
- **OSIRIS JSON coreschema validation** successfully tested.
- **`--include-raw-body`:** opt-in lossless fallback for `--purpose
  audit` runs. Full SDK response structs attached under
  `extensions["osiris.aws.sdk"].body` as JSON strings. Any field not yet
  promoted to a typed `properties` entry (VPC DNS attributes, subnet
  map, EKS node group config, etc.) is immediately accessible without
  extra API calls.

---

## [0.5.1] - 2026-06-14

Azure VNet and route-table collection regression fix. No changes to
other producers in this release.

| Producer | Behavior version |
|----------|------------------|
| Microsoft Azure OSIRIS JSON producer | [0.5.1](osiris/hyperscalers/azure/CHANGELOG.md#051---2026-06-14) |
| Cisco APIC OSIRIS JSON producer | 0.1.0 (no change) |
| Cisco IOS-XE OSIRIS JSON producer | 0.1.0 (no change) |
| Cisco NX-OS OSIRIS JSON producer | 0.1.0 (no change) |

### Highlights (Azure 0.5.1)
- **Fix** VNet and route-table collection regression
- **`--include-raw-body`** opt-in flag: attaches full ARM response body
  under `extensions["osiris.azure.arm"].body` for `--purpose audit` runs

---

## [0.5.0] - 2026-06-07

Azure Hub and connectivity resource expansion. No changes to other
producers in this release.

| Producer | Behavior version |
|----------|------------------|
| Microsoft Azure OSIRIS JSON producer | [0.5.0](osiris/hyperscalers/azure/CHANGELOG.md#050---2026-06-07) |
| Cisco APIC OSIRIS JSON producer | 0.1.0 (no change) |
| Cisco IOS-XE OSIRIS JSON producer | 0.1.0 (no change) |
| Cisco NX-OS OSIRIS JSON producer | 0.1.0 (no change) |

### Highlights (Azure 0.5.0)
- **Azure Bastion** (`osiris.azure.bastion`), **Traffic Manager**
  (`osiris.azure.trafficmanager`), **DNS Private Resolver**
  (`osiris.azure.dns.resolver`), and **DNS Forwarding Ruleset**
  (`osiris.azure.dns.forwardingruleset`): 4 new resource types with full
  connection wiring.
- **Data-quality fixes:** NIC `private_ip` hoisted to `properties`; NSG
  `security_rules` moved from `extensions` to `properties`; ExpressRoute
  gains 3 previously missing fields.
- **Management Group hierarchy:** new `logical.managementgroup` groups
  with full ancestry chain (root-to-leaf); subscription group gains
  `extensions.osiris.azure.management_group_path` (JSON array,
  root-to-leaf display names).
- **MetricAlert collection restored:** criteria unmarshal bug fixed;
  `microsoft.insights/metricalerts` is now fully collected and emitted
  with per-condition `criteria[]` in properties.
- **Transform layer split:** `transform_networking.go` and (compute,
  web, storage, security, identity, observability, recovery, databases,
  containers, integration, groups) extracted from the monolithic
  `transform.go`, covering all 16 networking resource types and 15
  connection types. This refactor align the Microsoft Azure producer
  layout with the Microsoft Azure service-category taxonomy; compute,
  containers, storage, and other domain splits will follow. Residual
  `transform.go` is now few source lines of pure shared helpers. No
  behavior change; identical output for all resource and connection
  types.
- **Bug fixes promoted from [0.4.0]:** Application Gateway
  reclassification, subnet CIDR fallback, Managed Identity IDs in
  properties, VNet peering `allowVirtualNetworkAccess` flag.
- **Additions promoted from [0.4.0]:** VM field depth, Azure Monitor
  Metric Alerts (with criteria), Azure Monitor Action Groups.

---

## [0.4.0] - 2026-04-25

Azure resource and connection coverage expansion. No changes to other
producers in this release.

| Producer | Behavior version |
|----------|------------------|
| Microsoft Azure OSIRIS JSON producer | [0.4.0](osiris/hyperscalers/azure/CHANGELOG.md#040---2026-04-25) |
| Cisco APIC OSIRIS JSON producer | 0.1.0 (no change) |
| Cisco IOS-XE OSIRIS JSON producer | 0.1.0 (no change) |
| Cisco NX-OS OSIRIS JSON producer | 0.1.0 (no change) |

### Highlights (Azure 0.4.0)
- New resource types discovery: App Service Plan, Web App / Function
  App, ASG, Storage Account, Key Vault, Container Registry, Managed
  Identity, Disk, Snapshot, Recovery Services Vault, Backup Vault, SQL
  Server, SQL DB, PostgreSQL / MySQL Flexible Server, Cosmos DB, Redis,
  AKS cluster + node pool, Container App Environment + Container App,
  ACI Container Group, Service Bus / Event Hubs namespace, APIM, Front
  Door, App Insights, Log Analytics workspace.
- [OSIRIS JSON specification chapter 5.2.3 connection
  subtypes](https://github.com/osirisjson/osiris/blob/main/specification/v1.0/OSIRIS-JSON-v1.0.md#523-standard-connection-types-v10):
- `network.peering`, `network.vpn`, `network.bgp`, `dependency`,
  `dependency.storage`, `dependency.database`.
- Private Endpoint `private_link_service_id` / `group_id` /
  `custom_dns_configs`.
- Cross-subscription gateway-peer stub fix.
- Region slug canonicalization fix.

See the [Azure 0.4.0 entry](osiris/hyperscalers/azure/CHANGELOG.md#040---2026-04-25)
for the full list of resources, properties, edges and out-of-scope notes.

---

## [0.2.1] - 2026-04-06

| Producer | Behavior version |
|----------|------------------|
| Microsoft Azure OSIRIS JSON producer | [0.2.1](osiris/hyperscalers/azure/CHANGELOG.md#021---2026-04-06) |
| Cisco APIC OSIRIS JSON producer | 0.1.0 (no change) |
| Cisco IOS-XE OSIRIS JSON producer | 0.1.0 (no change) |
| Cisco NX-OS OSIRIS JSON producer | 0.1.0 (no change) |

### Highlights
- **Azure**: resolve `az` binary path via `exec.LookPath`. Fixes
  [CWE-426](https://cwe.mitre.org/data/definitions/426).

---

## [0.2.0] - 2026-04-06

Adds the Microsoft Azure producer and aligns Cisco resource taxonomy
with the [OSIRIS JSON
spec](https://github.com/osirisjson/osiris/blob/main/specification/v1.0/OSIRIS-JSON-v1.0.md).

| Producer | Behavior version |
|----------|------------------|
| Microsoft Azure OSIRIS JSON producer | [0.2.0](osiris/hyperscalers/azure/CHANGELOG.md#020---2026-04-06) (initial release) |
| Cisco APIC OSIRIS JSON producer | [0.1.0](osiris/network/cisco/apic/CHANGELOG.md#010---2026-03-21) (in-place changes; constant not bumped) |
| Cisco IOS-XE OSIRIS JSON producer | [0.1.0](osiris/network/cisco/iosxe/CHANGELOG.md#010---2026-03-21) (in-place changes; constant not bumped) |
| Cisco NX-OS OSIRIS JSON producer | [0.1.0](osiris/network/cisco/nxos/CHANGELOG.md#010---2026-03-21) (in-place changes; constant not bumped) |

### Highlights
- **Azure**: new producer - full subscription topology via the Azure
  CLI. See [Azure 0.2.0](osiris/hyperscalers/azure/CHANGELOG.md#020---2026-04-06).
- **Cisco**: resource type taxonomy aligned with the OSIRIS spec across
  APIC, NX-OS and IOS-XE; connection type `network.link` renamed to
  `physical.ethernet`; output filename convention changed to
  `cisco-<type>-<timestamp>-<hostname>.json`. See per-producer entries.
- **pkg/sdk**: `MarshalDocument` now uses `json.Encoder` with
  `SetEscapeHTML(false)` to emit literal `<` `>` instead of `\u003c`
  `\u003e`.
- **cmd/osirisjson-producer-azure**: standalone Azure producer binary.

---

## [0.1.1] - 2026-03-30

| Producer | Behavior version |
|----------|------------------|
| Cisco NX-OS OSIRIS JSON producer | [0.1.0](osiris/network/cisco/nxos/CHANGELOG.md#010---2026-03-21) (factory wiring fix; constant not bumped) |
| Cisco APIC OSIRIS JSON producer | 0.1.0 (no change) |
| Cisco IOS-XE OSIRIS JSON producer | 0.1.0 (no change) |

### Highlights
- **Cisco NX-OS**: wired the producer factory into the CLI dispatcher -
  was returning `not yet implemented` despite the full implementation
  being present since v0.1.0.

---

## [0.1.0] - 2026-03-21

First Cisco producer release and core CLI dispatcher.

| Producer | Behavior version |
|----------|------------------|
| Cisco APIC OSIRIS JSON producer | [0.1.0](osiris/network/cisco/apic/CHANGELOG.md#010---2026-03-21) (initial release) |
| Cisco IOS-XE OSIRIS JSON producer | [0.1.0](osiris/network/cisco/iosxe/CHANGELOG.md#010---2026-03-21) (initial release) |
| Cisco NX-OS OSIRIS JSON producer | [0.1.0](osiris/network/cisco/nxos/CHANGELOG.md#010---2026-03-21) (initial release) |

### Highlights
- **Cisco APIC**: full ACI fabric topology, fault extensions, tenant
  hierarchy.
- **Cisco IOS-XE**: NETCONF/YANG over SSH; device/interfaces/CDP/VRFs;
  BGP/OSPF in detailed mode.
- **Cisco NX-OS**: NX-API CLI; device/interfaces/VLANs/VRFs/vPC/LLDP.
- **Shared Cisco runtime**: CLI flags, batch CSV, TLS, interactive
  password prompt.
- **cmd/osirisjson-producer**: core CLI dispatcher with plugin
  architecture (discovers `osirisjson-producer-<vendor>` binaries on
  `$PATH`, like git/kubectl plugins).
- **cmd/osirisjson-producer-cisco**: standalone Cisco producer binary.

### Module-level changes
- `go.mod`: Go directive updated to 1.25.0.
- `go.mod`: added `golang.org/x/term` v0.40.0, `golang.org/x/crypto`
  v0.48.0.
- Refactored `cmd/osirisjson-producer` from monolithic dispatcher to
  plugin-based `$PATH` discovery (no vendor imports).
- Relocated Cisco packages from `producers/cisco/` to
  `osiris/network/cisco/` (category-based taxonomy).

---

## [0.0.1] - 2026-02-28

Initial SDK release. No producers shipped under this tag.

### Added
- **pkg/sdk**: Go producer SDK implementing
  [OSIRIS-ADG-PR-SDK-1.0](https://github.com/osirisjson/osiris-producers/blob/main/docs/guidelines/v1.0/OSIRIS-PRODUCER-SDK.md)
  - Core types: `Document`, `Metadata`, `Topology`, `Resource`,
    `Connection`, `Group`, `Provider` with JSON schema tags.
  - Constants: `SpecVersion` (`"1.0.0"`), `SchemaURI`.
  - Interfaces: `Producer`, `Context`, `ProducerConfig`, `NewContext`,
    `EnvOrDefault`.
  - Validating factories: `NewResource`, `NewProvider`,
    `NewCustomProvider`, `NewConnection`, `NewGroup`.
  - Identity helpers: `Hash16`, `HashN`, `EncodeComponent`,
    `DeriveHint`, `ConnectionCanonicalKey`, `BuildConnectionID`,
    `GroupCanonicalKey`, `GroupID`.
  - `IDRegistry` with two-phase collision resolution (Hash16 -> Hash24
    -> Hash32 -> `ErrIDCollision`).
  - `DocumentBuilder` with `Build()` enforcing: sorted arrays, duplicate
    ID detection, reference integrity, extension key validation, secret
    scanning per safe failure mode, redaction metadata.
  - Normalization: `NormalizeRFC3339UTC`, `NormalizeToken`,
    `NormalizeMAC`, `NormalizeIP`.
  - Security and redaction: `IsSensitiveKey`, `ScanValue`,
    `ScanProperties`, `ScanDocument` (key-name + value-pattern detection
    per [OSIRIS-ADG-PR-1.0 chapter 3](https://github.com/osirisjson/osiris/blob/main/docs/guidelines/v1.0/OSIRIS-PRODUCER-GUIDELINES.md#3-security-and-redaction-deep-dive)).
  - Schema validators: `ValidateResourceType`, `ValidateConnectionType`,
    `ValidateGroupType`, `ValidateProviderName`, `ValidateNamespace`,
    `ValidateStatus`, `ValidateDirection`.
  - `MarshalDocument`: deterministic JSON (2-space indent, trailing
    newline).
  - `SetStatus`, `SetDirection`: validated setters for enum fields.
- **pkg/testharness**: test utilities for producers.
- **scripts/validate-golden.sh**: CI script for validating golden files
  via `npm install @osirisjson/cli`.
- Go module initialized at `go.osirisjson.org/producers` (stdlib only,
  zero dependencies).
- 90 tests passing (85 sdk + 5 testharness).

### Changed
- Restructured repository: producers organized vendor-first under
  `osiris/` (was root category-based: hyperscalers/, networking/, etc.).
- Removed empty `common/` stubs (replaced by `pkg/sdk/`).
- Updated `.gitignore` for Go.

[Unreleased]: https://github.com/osirisjson/osiris-producers/compare/v0.8.0...HEAD
[0.8.0]: https://github.com/osirisjson/osiris-producers/compare/v0.7.0...v0.8.0
[0.7.0]: https://github.com/osirisjson/osiris-producers/compare/v0.6.4...v0.7.0
[0.6.4]: https://github.com/osirisjson/osiris-producers/compare/v0.6.3...v0.6.4
[0.6.3]: https://github.com/osirisjson/osiris-producers/compare/v0.6.2...v0.6.3
[0.6.2]: https://github.com/osirisjson/osiris-producers/compare/v0.6.1...v0.6.2
[0.6.1]: https://github.com/osirisjson/osiris-producers/compare/v0.6.0...v0.6.1
[0.6.0]: https://github.com/osirisjson/osiris-producers/compare/v0.5.1...v0.6.0
[0.5.1]: https://github.com/osirisjson/osiris-producers/compare/v0.5.0...v0.5.1
[0.5.0]: https://github.com/osirisjson/osiris-producers/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/osirisjson/osiris-producers/compare/v0.2.1...v0.4.0
[0.2.1]: https://github.com/osirisjson/osiris-producers/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/osirisjson/osiris-producers/compare/v0.1.1...v0.2.0
[0.1.1]: https://github.com/osirisjson/osiris-producers/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/osirisjson/osiris-producers/releases/tag/v0.1.0
