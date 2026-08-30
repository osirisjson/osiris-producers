# Changelog - Cisco APIC OSIRIS JSON producer

All notable behavioral changes to the **`osirisjson-producer-cisco`**
producer's NX-OS backend are documented in this file.

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

### Changed
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

---

## [0.1.0] - 2026-03-21

Initial Cisco APIC producer release. The `generatorVersion` constant has
remained at `0.1.0` through later module tags; in-place behavioral changes
are listed below with their module-tag context.

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
