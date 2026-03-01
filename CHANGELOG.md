# Changelog

All notable changes to the OSIRIS JSON Producers packages will be documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Package versioning follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

> [!INFO]
> For changes to the architectural guidelines and documentation, see [`docs/guidelines/v1.0/CHANGELOG.md`](docs/guidelines/v1.0/CHANGELOG.md).
> For changes to the OSIRIS specification and core schema itself, see the [osiris repository](https://github.com/osirisjson/osiris).

---

## [Unreleased]

### Added
- **pkg/sdk**: Go producer SDK implementing OSIRIS-ADG-PR-SDK-1.0
  - Core types: `Document`, `Metadata`, `Topology`, `Resource`, `Connection`, `Group`, `Provider` with JSON tags
  - Constants: `SpecVersion` ("1.0.0"), `SchemaURI`
  - Interfaces: `Producer`, `Context`, `ProducerConfig`, `NewContext`, `EnvOrDefault`
  - Validating factories: `NewResource`, `NewProvider`, `NewCustomProvider`, `NewConnection`, `NewGroup`
  - Identity helpers: `Hash16`, `HashN`, `EncodeComponent`, `DeriveHint`, `ConnectionCanonicalKey`, `BuildConnectionID`, `GroupCanonicalKey`, `GroupID`
  - `IDRegistry` with two-phase collision resolution (Hash16 → Hash24 → Hash32 → `ErrIDCollision`)
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
- Updated `CLAUDE.md` for Go project (was toolbox-specific)
- Updated `.gitignore` for Go (removed Node.js entries)