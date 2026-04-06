# Changelog

All notable changes to the OSIRIS JSON Producers packages will be documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Package versioning follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

> [!INFO]
> For changes to the architectural guidelines and documentation, see [`docs/guidelines/v1.0/CHANGELOG.md`](docs/guidelines/v1.0/CHANGELOG.md).
> For changes to the OSIRIS specification and core schema itself, see the [osiris repository](https://github.com/osirisjson/osiris).

---

## [Unreleased]

---

## [0.2.0] - 2026-04-06

### Added
- **azure**: Microsoft Azure producer - full fetch of subscription topology via Azure CLI (`az`) [Azure CLI](https://learn.microsoft.com/en-us/cli/azure/?view=azure-cli-latest)
  - Collects VNets, subnets, NICs, NSGs, route tables, public IPs, load balancers, private endpoints, VNet gateways, NAT gateways, firewalls, app gateways, DNS zones, ExpressRoute circuits, VMs
  - Cross-subscription VNet peering stubs with `provider.type` and `provider.subscription`
  - Resource group resources (`container.resourcegroup`) per [OSIRIS spec Appendix C.5](https://osirisjson.org/en/docs/spec/v10/15-appendices#c5-container-and-organization-resources)
  - Resource group and subscription group hierarchy
  - `provider.type` populated with native ARM resource types on all resources
  - Interactive subscription picker when no flags provided
  - CSV batch mode, multi-subscription, and auto-discover (`--all`) modes
  - Output batcg hierarchy: `<output>/<TenantID>/<timestamp>/<SubscriptionName>.json`
  - Output single filename convention `microsoft-azure-<timestamp>-<SubscriptionName>.json`
- **cmd/osirisjson-producer-azure**: standalone Azure producer binary

### Changed
- **cisco/apic**: resource types - `osiris.cisco.controller`, `osiris.cisco.switch.spine`, `osiris.cisco.switch.leaf`, `osiris.cisco.domain.bridge`, `osiris.cisco.endpoint`, `osiris.cisco.l3out`, `osiris.cisco.epg`
- **cisco/nxos**: resource types - `osiris.cisco.switch.spine`, `osiris.cisco.switch.leaf`, `osiris.cisco.interface.lag`; connection type `physical.ethernet` (was `network.link`)
- **cisco/iosxe**: resource types - `osiris.cisco.interface.lag`; connection type `physical.ethernet` (was `network.link`)
- **cisco**: output filename convention changed to `cisco-<type>-<timestamp>-<hostname>.json`
- **pkg/sdk**: `MarshalDocument` now uses `json.Encoder` with `SetEscapeHTML(false)` to emit literal `<` `>` instead of `\u003c` `\u003e`

### Fixed
- **cisco/apic**: controller nodes no longer report `unknown` status when `fabricSt` is empty - falls back to `topSystem.state` field
- **cisco/**: Head guide URI reference for **OSIRIS-JSON-CISCO**.

---

## [0.1.1] - 2026-03-30

### Fixed
- **cisco**: wired NX-OS producer factory into CLI dispatcher - was returning "not yet implemented" despite full implementation being present since v0.1.0

---

## [0.1.0] - 2026-03-21

### Added
- **cisco**: APIC producer - full ACI fabric topology, fault extensions, tenant hierarchy ([details](osiris/network/cisco/CHANGELOG.md))
- **cisco**: IOS-XE producer - NETCONF/YANG over SSH, device/interfaces/CDP/VRFs, BGP/OSPF in detailed mode ([details](osiris/network/cisco/CHANGELOG.md))
- **cisco**: NX-OS producer - NX-API CLI, device/interfaces/VLANs/VRFs/vPC/LLDP ([details](osiris/network/cisco/CHANGELOG.md))
- **cisco**: shared runtime layer - CLI flags, batch CSV, TLS, interactive password prompt ([details](osiris/network/cisco/CHANGELOG.md))
- **cmd/osirisjson-producer**: core CLI dispatcher (plugin architecture)
  - Discovers `osirisjson-producer-<vendor>` binaries on `$PATH` (like git/kubectl plugin model)
  - Known vendor table with install hints for `--help` and error messages
  - Unknown vendors still discovered on `$PATH` (third-party producer support)
- **cmd/osirisjson-producer-cisco**: standalone Cisco producer binary
  - Fully self-contained - works without the core dispatcher
  - Also discovered and dispatched to by core via `$PATH`

### Changed
- `go.mod`: Go directive updated to 1.25.0
- `go.mod`: added `golang.org/x/term` v0.40.0, `golang.org/x/crypto` v0.48.0
- **cmd/osirisjson-producer**: refactored from monolithic dispatcher to plugin-based `$PATH` discovery (no vendor imports)
- **cisco**: relocated packages from `producers/cisco/` to `osiris/network/cisco/` (category-based taxonomy)

---

## [0.0.1] - 2026-02-28

### Added
- **pkg/sdk**: Go producer SDK implementing OSIRIS-ADG-PR-SDK-1.0
  - Core types: `Document`, `Metadata`, `Topology`, `Resource`, `Connection`, `Group`, `Provider` with JSON tags
  - Constants: `SpecVersion` ("1.0.0"), `SchemaURI`
  - Interfaces: `Producer`, `Context`, `ProducerConfig`, `NewContext`, `EnvOrDefault`
  - Validating factories: `NewResource`, `NewProvider`, `NewCustomProvider`, `NewConnection`, `NewGroup`
  - Identity helpers: `Hash16`, `HashN`, `EncodeComponent`, `DeriveHint`, `ConnectionCanonicalKey`, `BuildConnectionID`, `GroupCanonicalKey`, `GroupID`
  - `IDRegistry` with two-phase collision resolution (Hash16 -> Hash24 -> Hash32 -> `ErrIDCollision`)
  - `DocumentBuilder` with `Build()` enforcing: sorted arrays, duplicate ID detection, reference integrity, extension key validation, secret scanning per safe failure mode, redaction metadata
  - Normalization: `NormalizeRFC3339UTC`, `NormalizeToken`, `NormalizeMAC`, `NormalizeIP`
  - Secret scanning: `IsSensitiveKey`, `ScanValue`, `ScanProperties`, `ScanDocument` (key-name + value-pattern detection per OSIRIS-ADG-PR-1.0 ch. 3)
  - Schema validators: `ValidateResourceType`, `ValidateConnectionType`, `ValidateGroupType`, `ValidateProviderName`, `ValidateNamespace`, `ValidateStatus`, `ValidateDirection`
  - `MarshalDocument`: deterministic JSON (2-space indent, trailing newline)
  - `SetStatus`, `SetDirection`: validated setters for enum fields
- **pkg/testharness**: test utilities for producers
  - `NewTestContext`: deterministic context (fixed clock 2026-01-15T10:00:00Z, captured logger)
  - `AssertGolden`: golden-file comparison with `OSIRIS_UPDATE_GOLDEN=1` update mode
  - `AssertDeterministic`: runs producer twice, asserts byte-identical output
  - `LoadFixture`: reads test fixture files
- **scripts/validate-golden.sh**: CI script for validating golden files via `@osirisjson/cli`
- Go module initialized at `go.osirisjson.org/producers` (stdlib only, zero dependencies)
- 90 tests passing (85 sdk + 5 testharness)

### Changed
- Restructured repository: producers now organized vendor-first under `producers/` (was category-based: hyperscalers/, networking/, etc.)
- Removed empty `common/` stubs (replaced by `pkg/sdk/`)
- Updated `.gitignore` for Go (removed Node.js entries)