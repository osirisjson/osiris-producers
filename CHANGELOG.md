# Changelog

All notable changes to the OSIRIS JSON Producers packages will be documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Package versioning follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

> [!INFO]
> For changes to the architectural guidelines and documentation, see [`docs/guidelines/v1.0/CHANGELOG.md`](docs/guidelines/v1.0/CHANGELOG.md).
> For changes to the OSIRIS specification and core schema itself, see the [osiris repository](https://github.com/osirisjson/osiris).

---

## [Unreleased]

### Added - 2026-03-05
- **producers/cisco/apic**: APIC fault extensions (`osiris.cisco`)
  - `Fault` type: curated fault representation (code, severity, cause, description, timestamps, lifecycle, domain, subject)
  - `TransformFaults`: groups `faultInst` by DN prefix, filters out cleared faults (snapshot = "what's wrong now")
  - `WireFaultsToNodes`: attaches faults to node resources via `topology/pod-N/node-N` DN prefix match
  - `WireFaultsToTenants`: attaches faults to tenant groups via `uni/tn-NAME` DN prefix match
  - ACI-specific node metadata in `extensions["osiris.cisco"]`: `fabric_mac`, `control_plane_mtu`, `last_reboot_time`, `fabric_id` (from topSystem)
  - `faultInst` always queried (audit-critical, not detail-level gated)
  - 10 new tests (fault transform, DN grouping, cleared filtering, fault wiring, extension merging, integration)
- **producers/cisco/apic**: Cisco ACI/APIC fabric topology producer
  - `client.go`: APIC REST client with `Login` (aaaLogin), `QueryClass` (paginated class queries), automatic cookie-jar session handling
  - `transform.go`: pure APIC->OSIRIS mapping functions (no I/O)
    - `TransformNodes`: fabricNode + topSystem + firmwareRunning merge by DN prefix -> `network.controller`, `network.switch.spine`, `network.switch.leaf`
    - `TransformTenants`: fvTenant -> `logical.tenant` groups with DN->ID mapping
    - `TransformVRFs`: fvCtx -> `logical.vrf` groups with DN->ID mapping
    - `TransformBridgeDomains`: fvBD -> `network.domain.bridge` resources with DN->ID mapping
    - `TransformSubnets`: fvSubnet -> `network.subnet` resources
    - `TransformEPGs`: fvAEPg -> `logical.epg` groups with DN->ID mapping
    - `TransformEndpoints`: fvCEp -> `network.endpoint` resources (detailed mode only)
    - `TransformL3Outs`: l3extOut -> `network.l3out` resources (skips `__ui_svi_dummy_id_*`)
    - Relationship wiring: `WireBDsToTenants`, `WireSubnetsToTenants`, `WireVRFsToTenants`, `WireEPGsToTenants`, `WireL3OutsToTenants`, `WireEndpointsToEPGs`
  - `apic.go`: `Producer.Collect()` orchestrates query->transform->wire->Build() pipeline; `NewFactory()` for cisco.go wiring
  - Full ACI containment hierarchy: tenants own BDs/subnets/L3Outs (members) and VRFs/EPGs (children); EPGs own endpoints (detailed mode)
  - Detail level support: minimal (nodes, tenants, VRFs, BDs, subnets, EPGs, L3Outs) vs detailed (adds endpoints with EPG membership)
  - Deterministic resource IDs via `Hash16` on APIC DN-based canonical keys
  - 30 tests (client: 7, transform: 16, integration: 4, wiring: 6 included in transform)
- **producers/cisco/cisco.go**: wired `apic.NewFactory()` into subProducers (APIC now shows as "ready")

### Added - 2026-03-01
- **producers/cisco/shared**: shared transport layer for all Cisco producers
  - `config.go`: `TargetConfig` with datacenter hierarchy (DC/Floor/Room/Zone), `Type`, `Owner`, `Notes`; `RunConfig`; `ParseHostPort`, `ResolveAddr`, `OutputPath`
  - `flags.go`: `ParseFlags` with stdlib `flag.FlagSet`, short+long aliases, single/batch mode detection, mutual exclusivity validation
  - `httpclient.go`: `NewHTTPClient` with TLS config and cookie jar
  - `tty.go`: `PromptPassword` via `/dev/tty` with echo-disabled input (`golang.org/x/term`)
  - `batch.go`: datacenter-aware CSV (dc,floor,room,zone,hostname,type,ip,port,owner,notes), `FactoryRegistry` for multi-type dispatch, `RunBatch` with hierarchical output (DC/Floor/Room/Zone/Hostname.json), per-target failure isolation
  - Owner metadata: `self` (own device), `isp` (ISP-managed), `colo` (colocation) - human-only, does not affect OSIRIS documents
  - 30 tests for shared layer (config, flags, CSV parsing, batch orchestration)
- **producers/cisco/cisco.go**: Cisco vendor entry point
  - Sub-producer dispatch: `apic`, `nxos`, `iosxr` subcommands
  - `template --generate [apic|nxos|iosxr]` for CSV batch templates with precompiled 3-row examples
  - Single mode (stdout) and batch mode (hierarchical output directory) execution paths
- **cmd/osirisjson-producer**: core CLI dispatcher (plugin architecture)
  - Discovers `osirisjson-producer-<vendor>` binaries on `$PATH` and execs them (like git/kubectl plugin model)
  - Known vendor table with install hints (`go install ...@latest`) for `--help` and error messages
  - `[installed]` marker in vendor listing for discovered binaries
  - Unknown vendors still discovered on `$PATH` (third-party producer support)
- **cmd/osirisjson-producer-cisco**: standalone Cisco producer binary
  - Fully self-contained - works without the core dispatcher
  - Also discovered and dispatched to by core via `$PATH`

### Changed - 2026-03-01
- `go.mod`: added `golang.org/x/term` v0.40.0 dependency (echo-suppressed password input)
- `go.mod`: Go directive updated to 1.24.0 (required by `golang.org/x/term` v0.40.0)
- **cmd/osirisjson-producer**: refactored from monolithic dispatcher to plugin-based `$PATH` discovery (no vendor imports)

### Added - 2026-02-28
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

### Changed - 2026-02-28
- Restructured repository: producers now organized vendor-first under `producers/` (was category-based: hyperscalers/, networking/, etc.)
- Removed empty `common/` stubs (replaced by `pkg/sdk/`)
- Updated `CLAUDE.md` for Go project (was toolbox-specific)
- Updated `.gitignore` for Go (removed Node.js entries)