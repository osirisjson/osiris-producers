# Changelog

All notable changes to the OSIRIS JSON Producers packages will be documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Package versioning follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

> [!INFO]
> For changes to the architectural guidelines and documentation, see [`docs/guidelines/v1.0/CHANGELOG.md`](docs/guidelines/v1.0/CHANGELOG.md).
> For changes to the OSIRIS specification and core schema itself, see the [osiris repository](https://github.com/osirisjson/osiris).

---

## [Unreleased]

### Added - 2026-03-21
- **cisco**: APIC producer - full ACI fabric topology, fault extensions, tenant hierarchy ([details](osiris/network/cisco/CHANGELOG.md))
- **cisco**: IOS-XE producer - NETCONF/YANG over SSH, device/interfaces/CDP/VRFs, BGP/OSPF in detailed mode ([details](osiris/network/cisco/CHANGELOG.md))
- **cisco**: NX-OS producer - NX-API CLI, device/interfaces/VLANs/VRFs/vPC/LLDP (code complete, disabled in dispatcher) ([details](osiris/network/cisco/CHANGELOG.md))
- **cisco**: shared runtime layer - CLI flags, batch CSV, TLS, interactive password prompt ([details](osiris/network/cisco/CHANGELOG.md))
- **cmd/osirisjson-producer**: core CLI dispatcher (plugin architecture)
  - Discovers `osirisjson-producer-<vendor>` binaries on `$PATH` (like git/kubectl plugin model)
  - Known vendor table with install hints for `--help` and error messages
  - Unknown vendors still discovered on `$PATH` (third-party producer support)
- **cmd/osirisjson-producer-cisco**: standalone Cisco producer binary
  - Fully self-contained - works without the core dispatcher
  - Also discovered and dispatched to by core via `$PATH`

### Changed - 2026-03-21
- `go.mod`: Go directive updated to 1.25.0
- `go.mod`: added `golang.org/x/term` v0.40.0, `golang.org/x/crypto` v0.48.0
- **cmd/osirisjson-producer**: refactored from monolithic dispatcher to plugin-based `$PATH` discovery (no vendor imports)
- **cisco**: relocated packages from `producers/cisco/` to `osiris/network/cisco/` (category-based taxonomy)

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
- Updated `.gitignore` for Go (removed Node.js entries)