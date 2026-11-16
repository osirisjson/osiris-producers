# Changelog - OSIRIS JSON Producers policies

All notable changes to the repository policies under `docs/policies/`
are documented in this file.

The format follows:

- [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

Policy versioning follows:

- [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

For consistent line widths across policy documents (excluding links,
code examples and tables), we follow:

- [RFC 7994](https://datatracker.ietf.org/doc/html/rfc7994#section-4.3)

A policy governs committed repository content (source, tests, fixtures,
golden documents, documentation and configuration). It **MUST** never
change what an OSIRIS JSON producer collects or emits at runtime.

See the root [`CHANGELOG.md`](../../CHANGELOG.md) for the release-level.

---

## [Unreleased]

## [1.0.0-DRAFT.1] - 2026-11-16

### Added

- [`OSIRIS-PRODUCER-TEST-DATA-POLICY.md`](OSIRIS-PRODUCER-TEST-DATA-POLICY.md):
  the OSIRIS JSON Producers test data policy. It defines the approved
  example identifiers for committed content, checker coverage and
  review obligations.
  - Rule catalog with permanent local identifiers `OSIRIS-PR-TDP-001`
    through `OSIRIS-PR-TDP-017`.
  - Address rules: RFC 5737 IPv4 blocks (`OSIRIS-PR-TDP-001`), the
    RFC 3849 IPv6 prefix (`OSIRIS-PR-TDP-002`) and RFC 9542
    documentation MAC ranges (`OSIRIS-PR-TDP-003`), with full
    containment for IP prefixes.
    Locally administered unicast and `AA:BB:CC:DD:xx:xx` MACs are
    also approved.
  - Protocol constant classes retained for necessary semantic use:
    unspecified addresses, default routes, contiguous IPv4 netmasks,
    wildcard masks, and null and broadcast MACs.
  - Loopback addresses without individual exceptions limited to
    `_test.go` files that use or verify loopback behavior, necessary
    occurrences elsewhere and IPv6 link-local behavior use reviewed
    exact exceptions.
  - Context rules for hostnames (`OSIRIS-PR-TDP-004`: `example.com`,
    `example.net`, `example.org` and approved hyperscalers service suffixes),
    device names (`OSIRIS-PR-TDP-005`: `LAB-`, `TEST-`, `FAKE-`,
    `STUB-` prefixes), coordinates (`OSIRIS-PR-TDP-006`), site labels
    (`OSIRIS-PR-TDP-007`), AS numbers (`OSIRIS-PR-TDP-008`: RFC 5398
    and RFC 6996), `TST` serial numbers (`OSIRIS-PR-TDP-009`) and
    credential placeholders (`OSIRIS-PR-TDP-010`).
  - Allowlist integrity (`OSIRIS-PR-TDP-016`) and the exception
    contract for `testdata-policy-allowlist.json`: one entry per text
    occurrence, or one exact reviewed binary artifact, matched by
    fingerprint with a mandatory reason, owner, tracking reference and
    review date. Stale, expired, duplicate, unsorted or broad entries
    fail, integrity failures cannot be waived.
  - Inventory integrity (`OSIRIS-PR-TDP-017`) covering symlinks,
    submodules, unlisted binaries and read failures.
  - Context-rule coverage matrix distinguishing detection from
    repository-wide obligations. Current lexical checks include
    quoted and numeric values and labelled ASN patterns, production
    Go and unsupported syntax remain review responsibilities.
    Project references, redaction markers and value-free templates
    are classified by purpose.
  - Cloud identity rules: `OSIRIS-PR-TDP-011` (synthetic
    `00000000-0000-N000-8000-00000000XXXX` GUIDs, nil UUID only by
    exception), `OSIRIS-PR-TDP-012` (ARM paths validated by segment),
    `OSIRIS-PR-TDP-013` (accounts `123456789012`, `111122223333`,
    `444455556666`), `OSIRIS-PR-TDP-014` (zero-padded hexadecimal
    resource IDs) and `OSIRIS-PR-TDP-015` (`LAB`, `TEST`, `FAKE` or
    `STUB` names with service naming restrictions and a context-aware
    catalog of service-defined Microsoft Azure names).
  - Diagnostic format, run summary and steps for resolving a finding,
    plus a local mode that also scans untracked files.

[Unreleased]: ../../CHANGELOG.md#unreleased
[1.0.0-DRAFT.1]: ../../CHANGELOG.md#unreleased
