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

---

## [0.4.0] - 2026-11-15

Logical paths, vPC and pods; L3Out external connectivity (external
EPGs, policy prefixes, border-leaf and routed-path attachments) and
external neighbours identified by the identifier LLDP or CDP reports
instead of a shortened system name.

All fabric, tenant, policy, port and group IDs emitted by `0.3.0`
remain unchanged. External neighbour IDs and the IDs of the
`physical.ethernet` connections that reach them, change (see Changed);
every new neighbour lists the `0.3.0` IDs it replaces in
`extensions["osiris.cisco"].legacy_ids`.

### Added
- Logical paths, collected in both purposes from `fabricPathEp`,
  `vpcIf` and `vpcRsVpcConf` (optional classes):
  - `osiris.cisco.path` resources identified by the exact
    `fabricPathEp` DN, for every port-channel and vPC path and for
    every path an EPG, endpoint or L3Out attaches to. A single-port
    path nothing attaches to is not emitted. Properties: `pod`,
    `node_ids`, `fex_ids` (FEX paths), `path_type` (`pathT`),
    `aggregation` (`lagT`) and, for a vPC, `vpc_domain_id`.
  - `osiris.cisco.path.member` forward edges, one per leg, path ->
    the interface that realizes it: the physical port, the
    port-channel, or for a vPC the leg port-channel named by `vpcIf`
    and `vpcRsVpcConf` (`resolved_by: vpcIf`) or by the leg's
    `pcAggrIf` name (`resolved_by: pcAggrIf-name`). A leg that names no
    emitted interface, and every FEX leg, points to the leaf node
    (`member_level: node`, with `fex_id`). A vPC never gets a single
    owner. At `--purpose audit` the vPC leg edge carries the `vpcIf`
    local and remote operational state.
  - `osiris.cisco.epg.path` forward edges for every `fvRsPathAtt`, EPG
    resource anchor -> path, qualified by the relation DN, with
    `encap`, `primary_encap`, `mode`, `instr_immediacy` and `state`.
  - At `--purpose audit`, `osiris.cisco.endpoint.learned` forward
    edges endpoint -> path (`fvCEp.fabricPathDn`), marked
    `semantics: observed`.
  - An attachment to a path that `fabricPathEp` does not report
    (an unformed vPC, a denied or failed query) emits no edge and is
    counted as `unresolved`; no placeholder path is created.
- vPC domains, from `fabricExplicitGEp` and `fabricNodePEp` (optional):
  `osiris.cisco.vpc.domain` resources with `domain_id` and
  `config_state` (`virtual_ip` at `--purpose audit`) and an
  `osiris.cisco.vpc.member` forward edge to each member leaf. At
  `--purpose audit` the edge carries the leaf's `vpcDom` role and peer
  state (`vpcDom`, audit-only optional class).
- Pods, from `fabricPod` (optional): `physical.pod` groups whose
  members are the pod's fabric nodes and paths.
- Port-channel membership from `pcRsMbrIfs` (optional, both purposes),
  the configured pcAggrIf -> member relation. A member it reports is
  wired even when the bundle is down; `ethpmPhysIf.bundleIndex` stays a
  fallback and the two never duplicate an edge. A relation whose
  port-channel or member port is not emitted is counted as
  `unresolved`.
- L3Out external connectivity, collected in both purposes from
  `l3extInstP`, `l3extSubnet`, `l3extRsNodeL3OutAtt`,
  `l3extRsPathL3OutAtt` and `fvRsBDToOut` (optional classes):
  - `osiris.cisco.l3out.epg` resources (external EPGs), contained by
    their L3Out and members of their tenant group. External EPGs
    provide and consume contracts like EPGs
    (`osiris.cisco.contract.provides` / `.consumes`).
  - `osiris.cisco.l3out.prefix` resources (policy prefixes under an
    external EPG), contained by it, with `cidr`, `scope` and
    `aggregate`. A prefix is configured classification and
    route-control intent, never an installed or advertised route.
  - `osiris.cisco.l3out.node` forward edges L3Out -> border leaf and
    `osiris.cisco.l3out.path` forward edges L3Out -> path (routed
    port, sub-interface, SVI), qualified by the relation DN, with the
    node and interface profile names, `interface_type`, `encap`,
    `encap_scope`, `mode` and `mtu`. The router id and interface
    address are carried at `--purpose audit` only.
  - `osiris.cisco.bd.l3out` forward edges bridge domain -> L3Out,
    resolved by `tDn`, then by name in the bridge domain's tenant,
    then in tenant `common`.
  - `l3out_scope` on the controller resources: the model is configured
    intent and lists what it does not model (routing protocol peers,
    installed routes, route-control profiles, vPC SVI side addresses).
- External neighbours carry `identity_basis` (`lldp-chassis-id`,
  `cdp-device-id`, `lldp-system-name` or `cdp-system-name`),
  `chassis_id_type`, `cdp_device_id` when CDP reported one and, in
  `extensions["osiris.cisco"]`, `legacy_ids` (the `0.3.0` IDs this
  neighbour replaces) and `identity_conflict` (see Changed).

### Changed
- External neighbour identity. A neighbour is identified by the LLDP
  chassis id (`cisco.apic::external/lldp/<chassisIdT>/<chassisIdV>`),
  otherwise by the CDP device id
  (`cisco.apic::external/cdp/<devId>`), otherwise by its full
  reported system name; never by a shortened name. Two devices that
  share a short name in different DNS domains are no longer merged. A
  CDP observation is folded into an LLDP neighbour only when both were
  seen on the same local port with the same system name and that CDP
  device id matches exactly one LLDP chassis. Observations on one
  local port that disagree stay separate neighbours, each listing the
  other in `identity_conflict`. Because identity follows the protocol
  that reported it, a neighbour seen only by CDP (for example when the
  LLDP query fails) has a `cdp/` ID.
- `external-epgs` is no longer listed in `policy_scope.not_modeled`.
  A provide or consume relation under an external EPG, counted as
  `out_of_scope` in `0.3.0`, is now an edge.
- In documentation purpose, ports named by an L3Out path attachment
  and member ports reported by `pcRsMbrIfs` are emitted, like ports
  named by an EPG path attachment.
- With `--include-raw-body`, an object under `topology/pod-<n>` that no
  resource owns (for example `fabricLink`, or a `fabricPathEp` that is
  not emitted) is attached to its `physical.pod` group instead of
  `raw_unowned` on the controller resources.

### Fixed
- A port-channel path is resolved by its interface policy group name,
  which is the `pcAggrIf` name on the leaf (the `pcAggrIf` id is still
  accepted). `0.3.0` matched only paths whose name started with `po`,
  which an APIC does not use, so port-channel EPG membership and
  endpoint edges were missing on real fabrics.
- A FEX path (`.../paths-<leaf>/extpaths-<fex>/pathep-[...]`) is no
  longer resolved to the leaf port of the same name.
- Bridge-domain, subnet, L3Out and endpoint resources now carry the APIC
  class they were built from in `provider.type` (`fvBD`, `fvSubnet`,
  `l3extOut`, `fvCEp`), like every other APIC resource. It was empty
  since `0.2.0`. IDs are unchanged.

---

## [0.3.0] - 2026-11-14

Configured policy intent (application profiles, contracts, subjects,
filters and EPG provide/consume relations) as traversable resources and
relation edges, plus a corrective baseline: complete secret scanning,
diagnostics without response content, validated APIC responses, refused
redirects, honest partial-result accounting and synthetic golden
documents at both purposes.

All resource and group IDs emitted by `0.2.0` remain unchanged. The
document gains resources, connections and application groups, and EPG
groups move under their application group (see Changed).

### Added
- Configured policy intent, collected in both purposes from `fvAp`,
  `vzBrCP`, `vzSubj`, `vzFilter`, `fvRsProv`, `fvRsCons` and
  `vzRsSubjFiltAtt` (all optional classes):
  - `osiris.cisco.policy.contract`, `osiris.cisco.policy.subject` and
    `osiris.cisco.policy.filter` resources, identified by their native
    DN and members of their tenant group. A subject is contained by its
    contract (`contains`).
  - `osiris.cisco.policy.epg` and `osiris.cisco.policy.vrf` resource
    anchors, one per EPG and VRF group, so EPGs and VRFs can be
    connection endpoints. The group's `osiris.cisco.resource` names its
    anchor; group membership is unchanged.
  - Forward relation edges, each qualified by the relation object's DN:
    `osiris.cisco.contract.provides` and
    `osiris.cisco.contract.consumes` (EPG -> contract),
    `osiris.cisco.subject.filter` (subject -> filter),
    `osiris.cisco.epg.bd` (`fvRsBd`), `osiris.cisco.bd.vrf` (`fvRsCtx`)
    and `osiris.cisco.l3out.vrf` (`l3extRsEctx`). A target is resolved
    by `tDn`; without one, by name in the relation's own tenant, then in
    tenant `common` (`properties.resolved_by`). A relation or subject
    under an object the model does not cover (external or management
    EPG, out-of-band contract) emits nothing and is counted as
    `out_of_scope`; one whose modeled source or target is not in the
    document emits no edge and is counted as `unresolved`.
  - `logical.application` groups for application profiles.
  - At `--purpose audit`, filter entries from `vzEntry` as an ordered,
    allowlisted `properties.entries` list (protocol, ports, flags),
    bounded to 256 entries per filter (`entries_truncated`).
  - `policy_scope` on the controller resources: the model is configured
    intent and lists the mechanisms it does not model (vzAny, taboo
    contracts, endpoint security groups, external EPGs, management
    EPGs, out-of-band contracts, contract inheritance, service graphs,
    direction-specific subject terms).
    No edge links two EPGs directly; nothing states that traffic is
    permitted or flows.
- `--include-raw-body` (`--purpose audit` only), a fallback for fields
  this producer does not model: the full attribute set of every
  collected APIC object is attached under
  `extensions["osiris.cisco"].raw`, keyed by class and ordered by DN,
  on the resource or group that owns it (longest DN prefix; a resource
  wins over a group with the same DN). Objects without an owner go to
  `raw_unowned` on the controller resources. Every attribute with a
  sensitive name (the shared patterns plus APIC credential attributes
  such as `pwd` and `key`) and every secret-shaped value is replaced by
  `[REDACTED]`. One object is capped at 64 KiB and all objects at
  32 MiB; withheld objects are counted by class and reason in
  `raw_omitted`. Removing these keys yields exactly the document
  without the flag.
- `metadata.generator.url` is now emitted as
  (`https://docs.osirisjson.org/osiris-producers/network/cisco`).
- The per-operation `coverage` record carries `rejected`,
  `duplicates`, `unresolved` and `out_of_scope` counts when non-zero
  and `received` with `reported_total` when the APIC `totalCount`
  disagrees with the objects received. `count` keeps its meaning
  (objects carried into the transform).

### Changed
- EPG groups are children of their `logical.application` group, which
  is a child of the tenant group. An EPG whose application profile was
  not collected stays a direct child of its tenant, as before.
- `--safe-failure-mode log-and-redact` is rejected with an error. This
  producer performs no value replacement, so a document built in that
  mode could carry a detected secret while its metadata claimed
  redaction. `fail-closed` (default) and `off` are unchanged.
- `--include-raw-body` requires `--purpose audit` and now works (see
  Added). In `0.2.0` it was accepted but had no effect.
- A run whose `fabricNode` or `topSystem` answer is empty, or whose
  `fabricNode` answer has no node of a supported role, fails without
  writing a document. An APIC always reports its own controller, so
  empty inventory means the account cannot see the fabric (role or
  security-domain limits); previously an empty-fabric document was
  written.
- Class responses are validated. A body without an `imdata` array
  (`{}`, `"imdata": null`) or with an APIC error object among its
  elements now fails the query instead of reading as an empty result.
  An element that is not an object of the requested class with an
  `attributes` map and a non-empty `dn` is rejected: in an essential
  or structural class this aborts the run (category
  `rejected-objects`); in an optional class it is dropped and counted.
  A valid empty `imdata: []` is still a success with zero objects.
- Redirects are never followed, for login or collection. A redirect
  response fails the request as `HTTP 3xx`.
- `--help` documents the discovery plan, which classes are required
  and which optional, partial-document behavior and the rejected
  flags.
- Diagnostics no longer include APIC response content. Returned errors
  and log records carry the operation, class, HTTP status and numeric
  APIC error code only; HTTP error bodies, APIC error-envelope text and
  JSON decoder detail are discarded.

### Fixed
- A CDP neighbour whose system name matches a fabric node of an
  unsupported role no longer produces a connection to a resource the
  document does not contain (which aborted the document build). The
  observation is skipped and counted.

### Security
- Redirects are refused, so a 307/308 can no longer replay the
  `aaaLogin` request body to another origin or scheme.
- Secret-shaped values in fault descriptions, tenant, VRF, EPG and
  bridge-domain descriptions, resource names and other emitted strings
  are now detected by the document secret scan, so a `fail-closed` run
  aborts instead of emitting them. Previously fault records held in
  typed extension slices and top-level `name`/`description` members
  were not scanned.

---

## [0.2.0] - 2026-09-01

Resource-model, identity, transport, partial-coverage and topology-graph
overhaul, plus a CLI restructure. This is the first `generatorVersion`
bump since the initial release; behavior `0.1.0` shipped unchanged
through module tags up to `0.7.0`.

### Added
- Physical fabric topology. `l1PhysIf` (enriched by `ethpmPhysIf`) is
  emitted as `network.switch.port` resources and `pcAggrIf` as aggregate
  `network.switch.port` resources (`properties.aggregate = true`); each
  is tied to its owning fabric node by a `contains` connection, and each
  port-channel to its member ports (sourced from
  `ethpmPhysIf.bundleIndex`). In `--purpose documentation` only ports
  that take part in topology are emitted those carrying a fabric link,
  an LLDP/CDP neighbour, an EPG path attachment, or a port-channel
  membership; `--purpose audit` emits every port.
- Physical adjacency graph. `fabricLink`, `lldpAdjEp` and `cdpAdjEp` are
  merged into one `physical.ethernet` connection per link between fabric
  nodes, with `properties.source_port` / `target_port` / `link_state`
  and `properties.discovered_by` listing which of `fabricLink` / `lldp` 
  `cdp` observed it. A neighbour that is not an APIC fabric node gets a
  minimal `network.switch` resource (`id`
  `cisco.apic::external/<name-or-chassis-id>`, `status` `unknown`,
  `extensions["osiris.cisco"]["external"] = true`) so the connection
  resolves; `manufacturer`/`model`/`serial` are set only from a CDP
  neighbour that reports them, never assumed.
- Bridge-domain containment. Each `fvBD` now has a `contains` connection
  to the `network.subnet` resources configured directly under it (the
  tenant-group membership from `0.1.0` is unchanged).
- `--purpose audit` also wires each endpoint (`fvCEp`) to the switch
  port or port-channel it was learned on (a `contains` connection from
  the port to the `network.interface`), and adds each `fvRsPathAtt`
  target port/port-channel as a member of its EPG group. vPC
  (`protpaths`) path targets are recorded but not wired (they span two
  nodes with no single owning resource).
- Discovery plan gains `l1PhysIf`, `ethpmPhysIf`, `fabricLink`,
  `lldpAdjEp`, `cdpAdjEp`, `pcAggrIf` and `fvRsPathAtt`, all optional
  criticality (a query failure degrades to a document without that slice
  of the graph and adds a coverage entry).
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
- CLI only, no change to emitted documents: the `--help` flag list is
  now rendered from the real flag set (`FlagsUsage` in `flags.go`)
  instead of a hand-typed block, so it and the usage `flag.Parse` prints
  on an unrecognized flag are the same aligned, word-wrapped table and
  can no longer drift (the old block already disagreed with the
  registered `--include-raw-body` text). `dispatch.go` is folded into
  `apic.go` (the whole CLI surface now lives beside `Collect`, matching
  `nxos`/`vmanage`); no symbol or behaviour change.

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
[0.4.0]: ../../../../CHANGELOG.md#0120---2026-11-15
[0.3.0]: ../../../../CHANGELOG.md#0110---2026-11-14
[0.2.0]: ../../../../CHANGELOG.md#080---2026-09-01
[0.1.0]: ../../../../CHANGELOG.md#010---2026-03-21
