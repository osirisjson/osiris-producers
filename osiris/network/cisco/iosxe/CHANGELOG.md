# Changelog - Cisco IOS-XE OSIRIS JSON producer

All notable behavioral changes to the **`osirisjson-producer-cisco`**
producer's IOS-XE backend are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Producer versioning follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

This file tracks the **producer's behavior version** (`metadata.generator.version` in emitted documents).
It is independent of the repository's git tag - a single git tag may bump several producers.
See the root [`CHANGELOG.md`](../../../../CHANGELOG.md) for the release-level index of which producers shipped under each tag.

---

## [Unreleased]

---

## [0.1.0] - 2026-03-21

Initial Cisco IOS-XE producer release. The `generatorVersion` constant has
remained at `0.1.0` through later module tags; in-place behavioral changes
are listed below with their module-tag context.

### Added
- NETCONF/YANG over SSH transport.
- Device, interfaces, CDP, VRFs collection; BGP/OSPF in detailed mode.
- Shared runtime layer (CLI flags, batch CSV, TLS, interactive password prompt) with the rest of the Cisco producer family.

### Changed 
- Resource types updated to align with [OSIRIS JSON spec taxonomy](https://github.com/osirisjson/osiris/blob/main/specification/v1.0/OSIRIS-JSON-v1.0.md#7-resource-type-taxonomy): introduced `osiris.cisco.interface.lag`.
- Connection type `network.link` renamed to `physical.ethernet`.
- Output filename convention changed to `cisco-iosxe-<timestamp>-<hostname>.json`.

[Unreleased]: ../../../../CHANGELOG.md
[0.1.0]: ../../../../CHANGELOG.md#010---2026-03-21
