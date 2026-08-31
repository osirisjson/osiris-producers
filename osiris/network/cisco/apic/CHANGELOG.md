# Changelog - Cisco APIC OSIRIS JSON producer

All notable behavioral changes to the **`osirisjson-producer-cisco`**
producer's APIC (ACI fabric) backend are documented in this file.

The format follows:
- [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Producer versioning follows:
- [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

To maintain a consistent line width across `CHANGELOG.md` and all file 
comments (excluding links, code examples, and tables), we adhere to:
- [RFC 7994](https://datatracker.ietf.org/doc/html/rfc7994#section-4.3)

This file tracks the **producer's behavior version**
(`metadata.generator.version` in emitted documents). It is independent
of the repository's git tag a single git tag may bump several
producers. See the root [`CHANGELOG.md`](../../../../CHANGELOG.md) for
the release-level index of which producers shipped under each tag.

---

## [Unreleased]

`generatorVersion` moves to `0.2.0`: the first change in this section
that alters the emitted document (the discovery-coverage record below).

### Added
- Discovery-coverage record. Every controller resource now carries
  `extensions["osiris.cisco"]["coverage"]`: one entry per discovery
  operation with `operation`, `status` (`succeeded` / `failed` /
  `skipped`), object `count`, and, on failure, a sanitized `category`
  (for example `http-5xx`, `auth`, `decode-error`) that never contains
  raw error or response text. `metadata.scope.description` carries a
  one-line prose summary of the same (fabric domain, succeeded/total
  ratio, and each operation not represented). `metadata.scope.purpose`
  is now set from `--purpose`.
- `fvIp` is queried under `--purpose audit` (optional criticality,
  recorded in the coverage record; `skipped` otherwise) to populate
  endpoint `properties.ip_addresses`.

### Changed
- **Breaking:** `provider.name` on every emitted resource and
  `metadata.scope.providers` change from `cisco` to `cisco.apic`, the
  dotted vendor.product identity, matching the NX-OS producer's move to
  `cisco.nxos`. The vendor extension namespace stays `osiris.cisco`.
  `metadata.scope.name` is now populated with the collection target's
  hostname.
- **Breaking:** every resource `id` changes from the opaque
  `res-<type>-<hint>-<hash>` form to `cisco.apic::<apic-dn>`, following
  OSIRIS-JSON-v1.0 section 2.1.2's preferred `<provider>::<native-id>`
  construction. The APIC distinguished name was already carried in
  `provider.native_id`; it is now also the ID input, so an object's ID no
  longer depends on the controller alias it was collected through.
- **Breaking:** leaf and spine fabric nodes are emitted as the core
  `network.switch` type instead of `osiris.cisco.switch.leaf` /
  `osiris.cisco.switch.spine`; the role is kept in `properties.role`.
  `properties.manufacturer` (`"Cisco"`) is added to every node, and
  `provider.type` is now the native class (`fabricNode`) rather than the
  hardware model (still in `properties.model`).
- **Breaking:** the APIC controller node is emitted as a core compute
  type instead of `osiris.cisco.controller`: `compute.server` for a
  physical APIC appliance, `compute.vm` for a virtual APIC (vAPIC),
  distinguished by `topSystem.virtualMode`. `properties.role` stays
  `controller` and `provider.type` stays `fabricNode`; the discovery
  coverage record still rides the controller resource.
- **Breaking:** `provider.site` no longer carries the ACI fabric-state
  string (`active` / `unknown`). Fabric state already drives
  `status`; APIC reports no sourced location for a node, so the field is
  omitted.
- **Breaking:** `network.subnet` resources replace `properties.ip` (the
  raw `<gateway>/<prefix>` pair) with `properties.cidr` (the network
  prefix) and `properties.gateway_ip` (the host address). The ACI routing
  scope moves from `properties.scope` to
  `extensions["osiris.cisco"]["aci_scope"]` and is no longer implied to
  mean Internet public/private; `preferred` moves to the same extension.
- **Breaking:** ACI endpoints (`fvCEp`, `--purpose audit`) are emitted as
  the core `network.interface` type instead of `osiris.cisco.endpoint`.
  `properties.mac` becomes `properties.mac_address`, and every `fvIp`
  child address is joined into `properties.ip_addresses` (deduplicated,
  sorted). The ACI encapsulation and fabric-path attributes move to
  `extensions["osiris.cisco"]`.
- Discovery now has an explicit per-domain failure policy. Fabric
  identity (`fabricNode`, `topSystem`) and the tenant object model
  (`fvTenant`, `fvCtx`, `fvBD`, `fvSubnet`, `fvAEPg`, `l3extOut`) are
  essential/structural: a query failure aborts the run with no
  document. Enrichment and relationship classes (`firmwareRunning`,
  `faultInst`, `fvRsCtx`, `fvRsBd`, `l3extRsEctx`, and `fvCEp` under
  `--purpose audit`) are optional: a failure is logged, recorded in the
  coverage record, and the run continues with a partial document.
  Previously any single class query failure aborted the whole run.
- APIC responses are decoded through a typed `imdata` envelope. A
  malformed body or an APIC error envelope which can arrive with
  HTTP 200 is a classified error, so a failed query is distinguishable
  from a legitimately empty one. A single malformed object in a page is
  skipped with a warning instead of failing the page, and pagination
  advances on source-object count so it can no longer be truncated
  early by a bad object.
- Progress logging now narrates each phase to stderr the way others
  producers do: a `connecting to APIC` line before the
  login round-trip, a start line per discovery operation (`step=N/M`,
  class, criticality) paired with its completion count, a discovery
  summary (`succeeded` / `failed` / `skipped`), `transforming` /
  `wiring` / `assembling` milestones, and a per-page heartbeat for a
  large paginated class (`faultInst` on a big fabric is many pages at
  several seconds each). A partial document logs a `WARN` carrying the
  coverage summary.
- Login and logout request bodies are built with `encoding/json` instead
  of string interpolation, so a username or password containing a quote
  or backslash still produces valid JSON.
- HTTP transport is now bounded: a per-request timeout, an overall
  run deadline, context cancellation on SIGINT/SIGTERM, a response-body
  size cap, and classified retry (network errors, HTTP 429 and 5xx are
  retried with backoff; 401/403 and other 4xx are not).
- `--insecure` now prints a warning to stderr on every run that TLS
  certificate verification is disabled.
- The APIC session is released with `aaaLogout` when the run finishes; a
  logout failure is logged and never replaces the collection result.
- CLI only, no change to emitted documents (`generatorVersion` stays
  `0.1.0`): this producer now owns its own flag parsing, `Config` type,
  and single/batch runner (`flags.go`/`config.go`/`dispatch.go`),
  dispatched directly by `osiris/network/cisco/cisco.go` instead of the
  retired shared `run.ParseFlags`/`RunConfig`/`RunBatch`/`ProducerFactory`
  orchestrator. The `-p`/`--password` flag is gone (a CLI value leaks via
  `ps` and shell history); host/username/password resolve through
  `--secrets-file` (a permission-checked JSON file, flat or per-host/CIDR
  "rules" shape), then `OSIRISJSON_CISCO_APIC_PASSWORD`, then an
  interactive prompt. `--detail minimal|detailed` is replaced by the
  standard `--purpose documentation|audit` contract (`audit` selects the
  same extra fabric detail `detailed` did); `--include-raw-body` is
  accepted (audit only). `template --generate` moves under the
  subcommand (`apic template --generate`) and writes both `--secrets-file`
  shapes.

### Fixed
- Class queries are now ordered by `dn`. APIC does not guarantee a
  stable row order across separate page requests, so on a fabric with
  live churn (routine for `fvCEp`) an object could repeat on the next
  page or be skipped, which produced a duplicate `cisco.apic::<dn>`
  resource and aborted the document build (or silently dropped
  endpoints and faults). Each class response is also deduplicated by
  `dn` as a backstop, with a `WARN` when a repeat is dropped.

---

## [0.1.0] - 2026-03-21

Initial Cisco APIC producer release. The `generatorVersion` constant has
remained at `0.1.0` through later module tags; in-place behavioral
changes are listed below with their module-tag context.

### Added
- Full ACI fabric topology, fault extensions, tenant hierarchy.
- Shared runtime layer (CLI flags, batch CSV, TLS, interactive password
- prompt) with the rest of the Cisco producer family.

### Changed
- Resource types renamed to align with [OSIRIS JSON spec taxonomy](https://github.com/osirisjson/osiris/blob/main/specification/v1.0/OSIRIS-JSON-v1.0.md#7-resource-type-taxonomy):
  `osiris.cisco.controller`, `osiris.cisco.switch.spine`,
  `osiris.cisco.switch.leaf`, `osiris.cisco.domain.bridge`,
  `osiris.cisco.endpoint`, `osiris.cisco.l3out`, `osiris.cisco.epg`.
- Output filename convention changed to
  `cisco-apic-<timestamp>-<hostname>.json`.

### Fixed
- Controller nodes no longer report `unknown` status when `fabricSt` is
  empty falls back to `topSystem.state` field.

[Unreleased]: ../../../../CHANGELOG.md
[0.1.0]: ../../../../CHANGELOG.md#010---2026-03-21
