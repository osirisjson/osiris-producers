# Changelog

Release-level index for the `go.osirisjson.org/producers` Go module.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Module versioning follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

A single git tag covers the entire Go module (Go's proxy resolves
`@latest` to the highest SemVer tag on the module). Each producer keeps
its own behavioral version (`metadata.generator.version` in emitted
documents) in its own per-producer `CHANGELOG.md`. This file lists which
producer behavior versions shipped under each module tag.

- OSIRIS JSON producer for Microsoft Azure: [`osiris/hyperscalers/azure/CHANGELOG.md`](osiris/hyperscalers/azure/CHANGELOG.md)
- OSIRIS JSON producer for Cisco APIC: [`osiris/network/cisco/apic/CHANGELOG.md`](osiris/network/cisco/apic/CHANGELOG.md)
- OSIRIS JSON producer for Cisco IOS-XE: [`osiris/network/cisco/iosxe/CHANGELOG.md`](osiris/network/cisco/iosxe/CHANGELOG.md)
- OSIRIS JSON producer for Cisco NX-OS: [`osiris/network/cisco/nxos/CHANGELOG.md`](osiris/network/cisco/nxos/CHANGELOG.md)

For changes to the OSIRIS JSON Producer SDK architectural guidelines and documentation, see
[`docs/guidelines/v1.0/CHANGELOG.md`](docs/guidelines/v1.0/CHANGELOG.md).
For changes to the OSIRIS specification, core documents and core schema itself, see the
[OSIRIS JSON Repository](https://github.com/osirisjson/osiris).

---

## [Unreleased]

---

## [0.4.0] - 2026-04-25

Azure resource and connection coverage expansion. No changes to other producers in this release.

| Producer | Behavior version |
|----------|------------------|
| Azure | [0.4.0](osiris/hyperscalers/azure/CHANGELOG.md#040---2026-04-25) |
| Cisco APIC | 0.1.0 (no change) |
| Cisco IOS-XE | 0.1.0 (no change) |
| Cisco NX-OS | 0.1.0 (no change) |

### Highlights (Azure 0.4.0)
- New resource types discovery: App Service Plan, Web App / Function App, ASG,
  Storage Account, Key Vault, Container Registry, Managed Identity, Disk,
  Snapshot, Recovery Services Vault, Backup Vault, SQL Server, SQL DB,
  PostgreSQL / MySQL Flexible Server, Cosmos DB, Redis, AKS cluster +
  node pool, Container App Environment + Container App, ACI Container
  Group, Service Bus / Event Hubs namespace, APIM, Front Door, App
  Insights, Log Analytics workspace.
- [OSIRIS JSON spec §5.2.3 connection subtypes](https://github.com/osirisjson/osiris/blob/main/specification/v1.0/OSIRIS-JSON-v1.0.md#523-standard-connection-types-v10):
- `network.peering`, `network.vpn`,
  `network.bgp`, `dependency`, `dependency.storage`, `dependency.database`.
- Private Endpoint `private_link_service_id` / `group_id` / `custom_dns_configs`.
- Cross-subscription gateway-peer stub fix.
- Region slug canonicalization fix.

See the [Azure 0.4.0 entry](osiris/hyperscalers/azure/CHANGELOG.md#040---2026-04-25)
for the full list of resources, properties, edges and out-of-scope notes.

---

## [0.2.1] - 2026-04-06

| Producer | Behavior version |
|----------|------------------|
| Azure | [0.2.1](osiris/hyperscalers/azure/CHANGELOG.md#021---2026-04-06) |
| Cisco APIC | 0.1.0 (no change) |
| Cisco IOS-XE | 0.1.0 (no change) |
| Cisco NX-OS | 0.1.0 (no change) |

### Highlights
- **Azure**: resolve `az` binary path via `exec.LookPath`. Fixes
  [CWE-426](https://cwe.mitre.org/data/definitions/426).

---

## [0.2.0] - 2026-04-06

Adds the Microsoft Azure producer and aligns Cisco resource taxonomy with
the [OSIRIS JSON spec](https://github.com/osirisjson/osiris/blob/main/specification/v1.0/OSIRIS-JSON-v1.0.md).

| Producer | Behavior version |
|----------|------------------|
| Azure | [0.2.0](osiris/hyperscalers/azure/CHANGELOG.md#020---2026-04-06) (initial release) |
| Cisco APIC | [0.1.0](osiris/network/cisco/apic/CHANGELOG.md#010---2026-03-21) (in-place changes; constant not bumped) |
| Cisco IOS-XE | [0.1.0](osiris/network/cisco/iosxe/CHANGELOG.md#010---2026-03-21) (in-place changes; constant not bumped) |
| Cisco NX-OS | [0.1.0](osiris/network/cisco/nxos/CHANGELOG.md#010---2026-03-21) (in-place changes; constant not bumped) |

### Highlights
- **Azure**: new producer - full subscription topology via the Azure CLI.
  See [Azure 0.2.0](osiris/hyperscalers/azure/CHANGELOG.md#020---2026-04-06).
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
| Cisco NX-OS | [0.1.0](osiris/network/cisco/nxos/CHANGELOG.md#010---2026-03-21) (factory wiring fix; constant not bumped) |
| Cisco APIC | 0.1.0 (no change) |
| Cisco IOS-XE | 0.1.0 (no change) |

### Highlights
- **Cisco NX-OS**: wired the producer factory into the CLI dispatcher -
  was returning `not yet implemented` despite the full implementation
  being present since v0.1.0.

---

## [0.1.0] - 2026-03-21

First Cisco producer release and core CLI dispatcher.

| Producer | Behavior version |
|----------|------------------|
| Cisco APIC | [0.1.0](osiris/network/cisco/apic/CHANGELOG.md#010---2026-03-21) (initial release) |
| Cisco IOS-XE | [0.1.0](osiris/network/cisco/iosxe/CHANGELOG.md#010---2026-03-21) (initial release) |
| Cisco NX-OS | [0.1.0](osiris/network/cisco/nxos/CHANGELOG.md#010---2026-03-21) (initial release) |

### Highlights
- **Cisco APIC**: full ACI fabric topology, fault extensions, tenant hierarchy.
- **Cisco IOS-XE**: NETCONF/YANG over SSH; device/interfaces/CDP/VRFs; BGP/OSPF in detailed mode.
- **Cisco NX-OS**: NX-API CLI; device/interfaces/VLANs/VRFs/vPC/LLDP.
- **Shared Cisco runtime**: CLI flags, batch CSV, TLS, interactive password prompt.
- **cmd/osirisjson-producer**: core CLI dispatcher with plugin
  architecture (discovers `osirisjson-producer-<vendor>` binaries on
  `$PATH`, like git/kubectl plugins).
- **cmd/osirisjson-producer-cisco**: standalone Cisco producer binary.

### Module-level changes
- `go.mod`: Go directive updated to 1.25.0.
- `go.mod`: added `golang.org/x/term` v0.40.0, `golang.org/x/crypto` v0.48.0.
- Refactored `cmd/osirisjson-producer` from monolithic dispatcher to plugin-based `$PATH` discovery (no vendor imports).
- Relocated Cisco packages from `producers/cisco/` to `osiris/network/cisco/` (category-based taxonomy).

---

## [0.0.1] - 2026-02-28

Initial SDK release. No producers shipped under this tag.

### Added
- **pkg/sdk**: Go producer SDK implementing [OSIRIS-ADG-PR-SDK-1.0](https://github.com/osirisjson/osiris-producers/blob/main/docs/guidelines/v1.0/OSIRIS-PRODUCER-SDK.md)
  - Core types: `Document`, `Metadata`, `Topology`, `Resource`, `Connection`, `Group`, `Provider` with JSON schema tags.
  - Constants: `SpecVersion` (`"1.0.0"`), `SchemaURI`.
  - Interfaces: `Producer`, `Context`, `ProducerConfig`, `NewContext`, `EnvOrDefault`.
  - Validating factories: `NewResource`, `NewProvider`, `NewCustomProvider`, `NewConnection`, `NewGroup`.
  - Identity helpers: `Hash16`, `HashN`, `EncodeComponent`, `DeriveHint`, `ConnectionCanonicalKey`, `BuildConnectionID`, `GroupCanonicalKey`, `GroupID`.
  - `IDRegistry` with two-phase collision resolution (Hash16 -> Hash24 -> Hash32 -> `ErrIDCollision`).
  - `DocumentBuilder` with `Build()` enforcing: sorted arrays, duplicate ID detection, reference integrity, extension key validation, secret scanning per safe failure mode, redaction metadata.
  - Normalization: `NormalizeRFC3339UTC`, `NormalizeToken`, `NormalizeMAC`, `NormalizeIP`.
  - Security and redaction: `IsSensitiveKey`, `ScanValue`, `ScanProperties`, `ScanDocument` (key-name + value-pattern detection per [OSIRIS-ADG-PR-1.0 chapter 3](https://github.com/osirisjson/osiris/blob/main/docs/guidelines/v1.0/OSIRIS-PRODUCER-GUIDELINES.md#3-security-and-redaction-deep-dive)).
  - Schema validators: `ValidateResourceType`, `ValidateConnectionType`, `ValidateGroupType`, `ValidateProviderName`, `ValidateNamespace`, `ValidateStatus`, `ValidateDirection`.
  - `MarshalDocument`: deterministic JSON (2-space indent, trailing newline).
  - `SetStatus`, `SetDirection`: validated setters for enum fields.
- **pkg/testharness**: test utilities for producers.
- **scripts/validate-golden.sh**: CI script for validating golden files via `npm install @osirisjson/cli`.
- Go module initialized at `go.osirisjson.org/producers` (stdlib only, zero dependencies).
- 90 tests passing (85 sdk + 5 testharness).

### Changed
- Restructured repository: producers organized vendor-first under `osiris/` (was root category-based: hyperscalers/, networking/, etc.).
- Removed empty `common/` stubs (replaced by `pkg/sdk/`).
- Updated `.gitignore` for Go.

[Unreleased]: https://github.com/osirisjson/osiris-producers/compare/v0.4.0...HEAD
[0.4.0]: https://github.com/osirisjson/osiris-producers/compare/v0.2.1...v0.4.0
[0.2.1]: https://github.com/osirisjson/osiris-producers/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/osirisjson/osiris-producers/compare/v0.1.1...v0.2.0
[0.1.1]: https://github.com/osirisjson/osiris-producers/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/osirisjson/osiris-producers/releases/tag/v0.1.0
