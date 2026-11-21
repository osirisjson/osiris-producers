# Changelog - Cisco Catalyst SD-WAN Manager OSIRIS JSON Producer

All notable behavioral changes to the **`osirisjson-producer-cisco`**
producer's Cisco Catalyst SD-WAN Manager (formerly vManage) backend
are documented in this file.

The format follows:
- [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Producer versioning follows:
- [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

To maintain a consistent line width across `CHANGELOG.md` and all file
comments (excluding links, code examples and tables), we adhere to:
- [RFC 7994](https://datatracker.ietf.org/doc/html/rfc7994#section-4.3)

This file tracks the **producer's behavior version**
(`metadata.generator.version` in emitted documents). It is independent
of the repository's git tag - a single git tag may bump several
producers. See the root [`CHANGELOG.md`](../../../../CHANGELOG.md) for
the release-level index of which producers shipped under each tag.

---

## [Unreleased]

## [0.2.0] - 2026-11-21

> [!WARNING]
> **Breaking release:** the OSIRIS JSON Producer is renamed from vManage
> to Cisco Catalyst SD-WAN Manager aligning to Cisco re-branding.

### Changed
- **Breaking:** every user-visible and emitted identifier changes there
  is no compatibility alias. Regenerate OSIRIS JSON snapshot documents
  exports and update any of your workflows that matches on the old
  vManage exports strings:

  | Item | Before | Now |
  |---|---|---|
  | CLI subcommand | `osirisjson-producer cisco vmanage` | `osirisjson-producer cisco sdwan` |
  | Go import path | `osiris/network/cisco/vmanage` | `osiris/network/cisco/catalyst-sd-wan-manager` |
  | Go package | `vmanage` | `catalystsdwanmanager` |
  | `provider.name` | `cisco.vmanage` | `cisco.catalystsdwanmanager` |
  | Resource ID prefix | `cisco.vmanage::` | `cisco.catalystsdwanmanager::` |
  | Extension namespace | `osiris.cisco.vmanage` | `osiris.cisco.catalystsdwanmanager` |
  | `metadata.generator.name` | `osirisjson-producer-cisco-vmanage` | `osirisjson-producer-cisco-catalyst-sdwan-manager` |
  | Default output directory | `osirisjson-cisco-vmanage` | `osirisjson-cisco-catalyst-sdwan-manager` |
  | Output file name | `cisco-vmanage-<ts>-<site>.json` | `cisco-catalyst-sdwan-manager-<ts>-<site>.json` |
  | Secrets templates | `cisco-vmanage-secrets*.json` | `cisco-catalyst-sdwan-manager-secrets*.json` |

  Connection IDs derived from resource IDs change accordingly. Cisco
  API values are untouched: the `vmanage` device personality still maps
  to `osiris.cisco.controller`. Topology content is otherwise unchanged.
- **Breaking:** `--purpose documentation` (the default) now keeps an
  explicit field allowlist per OSIRIS JSON section 13.1.3: device
  `model`, `manufacturer`, `version`, `site_id`, `health`; interface
  `interface_name`, `interface_type`, `admin_status`, `oper_status`,
  `speed_mbps`, `vpn_id` and transport `color`; connection `color`.
  Addresses, MAC addresses, management IPs, site coordinates, device
  groups, connected managers, timestamps, state text, duplex, MTU,
  encapsulation, NAT type, quality scores, serial and chassis numbers
  move to `--purpose audit` only. Identity, names, descriptions,
  connections and group membership are unchanged. Documentation is
  minimized, not anonymized.
- `--include-raw-body` no longer attaches unmodified API bodies. Every
  row is decoded and redacted recursively (values under sensitive keys
  and secret-shaped strings become `[REDACTED]`), rows that are not
  valid JSON or exceed 256 KiB are withheld, all rows of one document
  are capped at 16 MiB and every withheld row is counted in
  `raw_omitted` on its device resource by endpoint and reason. Site
  topology rows are matched to their device by Manager device-id or
  system IP (including their circuits system IP); a row no device
  owns is counted as `unowned` instead of being dropped silently.
- Documents are published privately and atomically: directories this
  producer creates are `0700`, documents `0600` regardless of umask,
  a document is written to a temporary file and published under its
  final name only when complete and an existing file is never
  overwritten (the site fails with an explicit error instead).
- Site labels are made safe before use as a path component: control
  characters and whitespace become `-` and leading/trailing dots are
  trimmed, so a label can never be `.` or `..`. Two sites of one run
  whose labels sanitize to the same component get distinct
  directories (the later one is suffixed with its site-id).
- **Breaking:** cross-site relationships are kept. OMP sessions and
  transport tunnels with one end in another site were silently dropped
  from every site document; each site document now carries the remote
  endpoints it needs as neighbor resources (extension `site_role:
  "neighbor"`): a remote device keeps only its identity, type, name and
  provider fields and a remote transport interface its name, color
  and `contains` connection. Neighbors are never site group members.
  Remote endpoints are resolved against the whole controller inventory,
  so a peer outside the selected `--site` list is resolved too; the WAN
  interface listing of such a tunnel peer is fetched once
  (`neighbor_wan_interface` in coverage), by a pool of 8 workers capped
  at `--site-name-rate` requests per second with progress logged every
  25 devices, since a hub site can peer with hundreds of devices; each
  lookup counts in the coverage of the site documents that need it. A
  neighbor has exactly the ID of the full resource in its own site
  document and a relationship carried by several site documents has the
  same connection ID, source and target in each, so an OSIRIS JSON
  Consumer analyzing and merging the documents by ID obtains one 
  end-to-end graph. Endpoints that are unknown or ambiguous (a system IP
  shared by several devices, a color on several interfaces) are never
  guessed and are counted in `metadata["osiris.cisco.catalystsdwanmanager"].graph`.
- **Breaking:** OMP sessions are one bidirectional `network` connection
  per device pair, with endpoints in canonical order and extension
  `protocol: "omp"`, built from every observation of the pair (the
  global OMP link list and each device's peer list, from either end).
  The connection ID is protocol-qualified, so every OMP connection ID
  changes. `sources` lists which operations reported the session;
  disagreeing observed states leave the state unset and are listed in
  `observed_states` instead of the first observation winning.
  Previously the per-device peer form was emitted `forward` under a
  bidirectional ID.
- Tunnels are emitted with endpoints in canonical order, disagreeing
  observations are listed in `observed_states`, several colors in
  `colors` and a quality score of `0` is no longer dropped.
- **Breaking:** the site group is `logical.site` (was
  `physical.room`): the API carries no room, building or rack evidence.
  Its members are the site's devices and their interfaces (previously
  devices only), its ID is derived from the raw site-id (group IDs
  change) and it carries extension `site_id` (or `unsited` for the
  bucket of devices without a site-id). Coordinates outside the valid
  latitude/longitude ranges are ignored.
- **Breaking:** the device system IP moves from
  `properties.management_ip` to extension `system_ip` (audit only): it
  is the SD-WAN overlay identifier, not a proven management address.
- Every resource carries `provider.site` (the site name) and
  `provider.system` (the Manager host the run collected from).
- **Breaking:** interface reconciliation. Rows of the same interface
  name in different VPNs are separate interfaces (previously merged
  into one): the lowest VPN keeps the existing ID and every other VPN
  gets an `@vpn<id>` suffix. Address-family rows of one interface are
  reconciled, keeping a description (or MAC address, status, speed)
  that only the IPv6 row carries. A WAN color carried by several
  interfaces of one device is ambiguous and no longer attached to the
  last one listed.
- An interface address is `public_ip` only when it is globally routable,
  shared (RFC 6598), loopback and link-local addresses are `private_ip`.
- `device_groups` and `connected_manager` are sorted and documents do
  not depend on Cisco API row order.
- Devices without a native identity (no serial, uuid or deviceId) are
  skipped and counted instead of being emitted with an empty ID.
- **Breaking:** run outcome. A run now exits with an error when any
  selected site's document could not be built or written (after every
  other site is written) previously it succeeded as long as one site
  was written. A failed optional API operation leaves its document
  partial and the run still succeeds the new `--require-complete`
  flag turns any partial document into an error, for CI.
- **Breaking:** `metadata.scope.accounts` is no longer populated and
  `GET /dataservice/tenant` is no longer called: the tenants a
  credential can see are not the accounts the run collected from.
- Collection runs every selected site before any document is built,
  through an injectable API interface and optional operations are
  independent: a failed interface listing for a device no longer
  skips that device's WAN interfaces and OMP peers.
- Every document carries a discovery coverage record in
  `metadata["osiris.cisco.catalystsdwanmanager"]`: per operation its
  status (`succeeded`, `empty`, `partial`, `failed`, `skipped`),
  attempts, row count and failure categories (`denied`, `not_found`,
  `rate_limited`, `server_error`, `http_error`, `malformed`,
  `too_large`, `redirect`, `timeout`, `canceled`, `transport`), plus
  `complete` and `api_profile`. `api_profile` is `unverified`: the
  Manager release is not discovered and the reply shapes documented
  for 20.18, 26.1 and 26.2 are all accepted.
- Every document of one run carries the same `metadata.timestamp`
  and file-name timestamp.
- `--secrets-file` now loads through the shared
  `osiris/network/cisco/run` credential-file loader (also used by
  apic/iosxe/nxos `--secrets-file`), which rejects symlinks,
  group/other permission bits and files not owned by the invoking
  user. Previously loaded via a local, unchecked `os.ReadFile` +
  `json.Unmarshal` pair with none of those guarantees.
- `template --generate`'s file-writing now goes through the same shared
  helper apic/iosxe/nxos use (`run.WriteTemplateFile`); the written file
  (`cisco-catalyst-sdwan-manager-secrets.json`) and its `{host, username, password}`
  shape are unchanged.
- `--secrets-file` now also accepts the "rules" shape apic/iosxe/nxos
  for multi-host targets. SD-WAN Manager has exactly one target host per
  run, so this is most useful for reusing one `--secrets-file` across
  multiple controllers (matched by host/CIDR) rather than for a
  per-target batch, but the resolution logic and the file shape is
  now identical to every other Cisco producer.

### Added
- `--require-complete` (see Changed).
- Per-release API contract fixtures for SD-WAN Manager API 20.18, 26.1
  and 26.2 (`testdata/.../contract_<release>`): every endpoint answered
  with the keys of that release's documented example response and
  synthetic values; the contract test requires the same complete graph
  for all three releases.
- Source-to-golden test (`TestGolden`): an in-memory HTTPS SD-WAN
  Manager serves synthetic `/dataservice` response bodies for a
  control-plane site (Manager, Controller and Validator) and the emitted
  document is compared byte for byte with the expected documents at both
  `--purpose` levels. `TestGolden_PurposeContract` and
  `TestGolden_GraphIntegrity` check the audit-only fields and that
  every connection endpoint and group member is an emitted resource.
  Emitted documents are unchanged.

### Fixed
- A `200` reply that is empty, carries an error object or lacks the
  documented `data` member is reported as `malformed` or
  `server_error` instead of being read as an empty listing.
- Inventory `lastupdated` and `uptime-date` are accepted as numbers or
  numeric strings (the published schema types them as strings and its
  example as integers); previously a string value failed the whole
  inventory and aborted the run.
- Response bodies are capped at 64 MiB, `Retry-After` waits are
  capped at 8 seconds and Ctrl-C cancels in-flight requests and
  retry waits.
- The per-request timeout is 60 seconds (was 15): the site topology
  monitor of a large site could time out and leave its document
  without tunnels. A read that times out or answers HTTP 5xx is
  retried once with backoff, denials, missing endpoints and malformed
  replies are never retried.
- An expired Manager session (HTTP 401, or the login page re-served
  with HTTP 200) is renewed with one new login per run and the request
  retried; concurrent requests share that renewal. A session that
  cannot be renewed is reported as `denied` and the login page is
  never read as an empty or malformed listing.
- A positional argument is now rejected instead of silently ending
  flag parsing. Previously a stray value such as
  `--include-raw-body true` made every later flag, including
  `--site`, be ignored, so the run fell back to the interactive site
  picker and resolved the name of every site on the controller. Use
  boolean flags bare (`--include-raw-body`) or as `--flag=false`.
- Every documentation-only example host across this producer (CLI help
  text, doc comments, the generated `cisco-catalyst-sdwan-manager-secrets.json`
  template and test fixtures) changed from `acme.sdwan.cisco.com` to
  `sdwan-manager.example.com` the former was not an RFC 2606 reserved
  domain and could collide with a real registered domain; the latter
  uses the `example.com` domain RFC 2606 reserves for exactly this
  purpose. No behavioral change.
- `template --generate` now also writes
  `cisco-catalyst-sdwan-manager-secrets-multihost.json` (the rules
  shape) alongside `cisco-catalyst-sdwan-manager-secrets.json` (the
  flat single-host shape), each carrying its own explanatory
  `"$comment"` field. Unlike apic/iosxe/nxos (whose example "hosts"
  patterns are RFC 5737 CIDR/exact-IP blocks), the multi-host file
  uses two example RFC 2606 FQDNs (`sdwan-manager01.example.com`,
  `sdwan-manager02.example.com`), matching how SD-WAN Manager targets
  are actually addressed: a controller hostname, not a bare IP. Both
  example files use the same plain `user`/`changeme` credential
  placeholders.

### Security
- `--safe-failure-mode log-and-redact` and `off` now replace every
  detected secret in names, descriptions, properties and extensions
  before the document is built; previously `log-and-redact` only
  logged findings and marked the document redacted while leaving the
  values in place. `log-and-redact` also refuses to write a document
  in which any finding remains. `fail-closed` (default) is unchanged.
- Error messages and logs no longer include HTTP response bodies
  (which can carry credentials or tokens), request query parameters
  or request URLs; they report the endpoint path and HTTP status only.
- Redirects are followed only within the controller origin (`https`,
  same host and port, at most 5). A redirect to any other origin is
  refused, so the login form and the `X-XSRF-TOKEN` header can never
  be sent elsewhere.

### Known limitations
- The Cisco SD-WAN Manager release is not discovered; documents are
  labelled `api_profile: "unverified"` and the reply shapes documented
  for 20.18, 26.1 and 26.2 are all accepted. Exact patch-level behavior
  is not certified.
- Sessions are not logged out at the end of a run (the session is
  renewed at most once if it expires) and JWT authentication is not
  supported.
- Interface IPv6 addresses are not emitted; IPv6 rows only complete
  fields their IPv4 row lacks.
- Tunnel endpoints are resolved by system IP and color only; two
  interfaces of one device on the same color, or parallel tunnels
  that differ only by encapsulation, stay unresolved or merged.
- Only IPv4 system IPs are recognized in tunnel names.
- A neighbor device from a site outside the run carries no
  `provider.site` (its name is unknown); its `site_id` extension is set.

## [0.1.0] - 2026-08-09

Initial Cisco vManage producer release.

### Added
- Device inventory collection via `GET /dataservice/device`, mapped to
  `osiris.cisco.controller` (vManage/vSmart/vBond personalities) and
  `network.router` (vEdge/cEdge WAN edges), matching
  OSIRIS-JSON-v1.0 7.5.2's network.router shape. Resource IDs use
  the namespaced native-ID form (2.1.2): `network.router` uses the
  device's hardware serial (`board-serial`) when available, falling
  back to `uuid`/`deviceId` otherwise - vManage does not populate
  `uuid` as an actual UUID for vEdge/cEdge platforms, only a
  `"<model>-<chassis-serial>"` chassis reference string (preserved as
  `extensions.osiris.cisco.vmanage.chassis_number` when the serial
  substitution applies). Controller personalities (vmanage/vsmart/vbond)
  are always keyed by `uuid`/`deviceId`. `properties` carries
  `manufacturer` (constant `"Cisco"`, vManage only manages
  Cisco/Viptela hardware), `model`, `version`, `management_ip` (the
  device's system-ip, no separate `ip_addresses` object since it would
  just duplicate this single value) and, `audit` purpose only,
  `serial_number` (`board-serial`). `routing_protocol` is not included
  (no vManage endpoint reports it without new per-device BGP/OSPF
  calls). `provider.site`/`provider.version` are not set 7.5.2's own
  network.router example places site placement under a top-level
  `location` object this producer doesn't emit yet (`sdk.Resource` has
  no `Location` field; adding one is a shared, cross-producer `pkg/sdk`
  change which we will add in upcoming releases).
  The site name remains available at the document level via
  `metadata.scope.sites`. `extensions.osiris.cisco.vmanage` carries
  `site_id`, `device_groups`, `health` (vManage's dashboard health
  rollup `green`/`red`/`yellow`, distinct from `reachability`/`status`
  which map to the resource's top-level `state`/`status`),
  `state_description`, `connected_manager`, `last_update` and
  `up_since` (all regardless of `--purpose`, same tier as `site_id`),
  and, `audit` purpose only, `certificate_validity`. `extensions` is
  omitted entirely (not an empty placeholder object) when nothing
  remains to put there.
- Interface collection via `GET /dataservice/device/interface` enriched
  by `GET /dataservice/device/control/waninterface` for WAN-transport
  interfaces (public/NAT IP, SD-WAN color). Interfaces belonging to a
  `network.router` device are mapped to OSIRIS-JSON-v1.0 7.5.2's
  `network.router.port` (interfaces on any other device type keep the
  generic `network.interface`), with composite IDs
  `cisco.vmanage::<device-key>-<ifname>` and a `contains` connection
  wiring each one to its owning device. `properties` carries
  `interface_name`, `ip_addresses` (each candidate address classified
  by whether it is actually RFC 1918/4193 private via
  `net.IP.IsPrivate()`, not by which vManage field it came from a
  Direct Internet Access circuit with no NAT reports the same real,
  publicly-routable address in both vManage's `private-ip` and
  `public-ip` fields, which mean "before/after NAT," not "private/
  public"), `mac_address`, `admin_status`, `oper_status`,
  `speed_mbps`/`mtu` (parsed to integers, per OSIRIS JSON section 7.5.2
  types), `duplex`, `encapsulation`, `interface_type`
  (`primary`/`secondary`, from vManage's transport/service port-type),
  and `vpn_id` (vManage's own native VPN-ID term functionally
  equivalent to a VRF, per Cisco's own SD-WAN documentation, but kept
  under its native name since OSIRIS-JSON-v1.0 network.router.port
  properties table does not document a `vrf`/`vrf_id` property).
  `description` is set on the resource's top-level `description` field
  when vManage reports one not every interface sets it. 
  `vlan_id` is not included: no vManage
  endpoint reports a VLAN tag for a service-side sub-interface.
  Interface `status` recognizes both the vEdge (Viptela OS) `Up`/`Down`
  vocabulary and the cEdge (IOS-XE) `ietf-interfaces` oper-status
  vocabulary (`if-oper-state-ready`, `if-oper-state-no-pass`, etc.)
  the latter is not documented in the vManage OpenAPI spec.
- SD-WAN site-to-site tunnel connections (`network.vpn`) via
  `GET /dataservice/topology/monitor/site/{siteId}`, carrying tunnel
  color and vQoE score under `extensions.osiris.cisco.vmanage`.
- OMP control-plane peering connections (`network`) via the global
  `GET /dataservice/device/omp/links` (queried once per `up`/`down`
  state and merged - the endpoint's `state` query parameter is
  required but its accepted values are undocumented anywhere in the
  vManage OpenAPI spec, so an unverified `all` is not relied on) and
  per-device `GET /dataservice/device/omp/peers`, deduplicated across
  both sources when they describe the same device pair.
- `topology.groups` support: a `physical.room` group (OSIRIS-JSON-v1.0 
  7.6.5, not `logical.site` 6.2.3 a WAN edge site is a real single
  physical location with its own coordinates, not the organizational/
  conceptual grouping `logical.*` covers) is emitted per document, with
  every device (`network.router`/`osiris.cisco.controller`) resource in
  that site as a member (interfaces are not added directly their
  `contains` connection to the owning device already implies site
  membership). `physical.room`, not `physical.building`/`datacenter`/
  `floor`/`rack`: vManage WAN edges are network equipment in a rack that
  is not necessarily inside a datacenter or dedicated building often a
  network closet or equipment room in a small office or factory, which
  is exactly what OSIRIS 7.6.5's `physical.room` definition names
  ("network closets", "equipment rooms"); vManage unfortunately do not
  report rack-level detail, ruling out `physical.rack`.
  This stays a lightweight group (membership + `geo_location` only),
  not 7.6.5's fuller resource shape
  (`room_number`/`room_sqft`/`room_type`/`cooling_capacity_tons`)
  vManage has none of that data. `properties.geo_location`
  (`{latitude, longitude}`, matching OSIRIS-JSON-v1.0 section 6.5.1.1
  own group example shape) is sourced from the first site device with
  resolvable coordinates vManage reports identical latitude/longitude
  for every device at a site, so this is set once on the group rather
  than duplicated per device. Not purpose-gated unlike
  `serial_number`/`certificate_validity`, site coordinates are not
  sensitive enough to withhold at the default `documentation` purpose.
- One OSIRIS JSON document per WAN edge site, grouped by device
  `site-id`, written to
  `<output-dir>/<site-name>/cisco-vmanage-<timestamp>-<site-name>.json`
  (the resolved display name, not the raw numeric site-id matches
  `metadata.scope.sites`). Devices without a site-id land under an
  `unsited` fallback segment. A `<site-name>` directory already
  populated by a previous run of this producer, or of a different
  producer entirely against the same `--output`, is reused as-is the
  new timestamped file is simply added alongside what's already there.
- `Info`-level progress logging for the per-site interface/tunnel/OMP
  collection stage (device count, per-device interface count, tunnel
  and OMP connection counts), so a healthy run shows visible progress
  between site selection and the final "Saved to" line. Every log line
  for a site carries both `site_id` (the numeric id) and `site` (the
  resolved display name).
- Session-cookie + XSRF-token authentication
  (`POST /j_security_check` then `GET /dataservice/client/token`).
- `--purpose documentation|audit` support: audit adds serial numbers,
  certificate validity and geo-coordinates.
- `-h/--host` accepts customer-specific domains (e.g.
  `vmanage.example.com`) as well as bare IPs, optionally with `:port`
  (`-P/--port` overrides). `-u/--username` for authentication. There is
  deliberately no `-p/--password` flag a CLI flag value is visible to
  any local user (e.g. via `ps`) and gets written to shell history.
  `-h`/`-u`/password are each resolved in this order: their own flag,
  then `--secrets-file`, then (for whichever is still missing) an
  interactive prompt on the controlling terminal host/username
  visibly, password hidden so a bare
  `osirisjson-producer cisco vmanage` with no flags at all still works
  end to end, asking for whatever it needs one at a time. Nothing
  entered interactively is written to disk unless `--secrets-file` was
  also given.
- `--secrets-file`: a JSON file with `{host, username, password}` any
  field it omits still falls back to its own flag or an interactive
  prompt, so a partially-filled file is fine.
  `template --generate` writes a skeleton to
  `cisco-vmanage-secrets.json`.
- `--include-raw-body` (requires `--purpose audit`): attaches each
  collected endpoint's full, unmodified API response body to the
  owning device resource under `extensions["osiris.cisco.vmanage"]`
  the device's own `GET /dataservice/device` object (`raw`),
  `interfaces_raw`, `wan_interfaces_raw` (when non-empty),
  `omp_peers_raw` and `site_topology_raw` (matched back to the device
  by vManage's own device-id) a lossless fallback for fields not yet
  modeled by this producer.
- Best-effort tenant listing (`GET /dataservice/tenant`) feeds
  `metadata.scope.accounts` when the credential has visibility; never
  used to switch into another tenant's device inventory (no such
  endpoint is documented in the vManage OpenAPI spec).
- Site selection: `--site <id-or-name>[,<id-or-name>...]` to name
  specific sites accepts either the raw numeric site-id or the
  resolved display name (e.g. `--site MXP`), matched
  case-insensitively; a value matching a raw site-id short-circuits
  without any name resolution, a value that doesn't triggers a full
  name-resolution pass (the same bounded/rate-limited one `--all` and
  the interactive picker use) to check it against every discovered
  site's name. `--site all` is an alias for `--all`. `--all` collects
  every discovered site non-interactively (site names are resolved
  lazily, once the export loop reaches each site, rather than all up
  front - the first document is written immediately instead of paying
  the full bulk site-name-resolution cost before anything starts); an
  interactive picker (pick by number, e.g. `1,3,5` or `1-4` or `all`)
  is shown when neither `--site` nor `--all` is given. Site-ids are
  only known after the device inventory is fetched (vManage has no
  standalone sites API), so discovery and selection happen after
  `GET /dataservice/device` rather than before collection.
- Human-readable site names via
  `GET /dataservice/topology/device/site/{siteId}` (`site_name` field
  the spec itself documents no fields for this endpoint): shown as a
  "Site name" column (No / Site name / Devices) in the interactive
  picker and used in `metadata.scope.sites` for the emitted document
  (falls back to the numeric site-id if the lookup fails or the
  credential lacks access best-effort, never blocks collection).
  Resolved with a bounded concurrent worker pool (8 at a time) and
  progress output rather than sequentially, since deployments can have
  thousands of sites. `--site-name-rate` (default 10 requests/second)
  caps bulk site-name resolution independently of worker concurrency,
  since actual throughput also depends on server latency; vManage's own
  rate limit is typically shared across every consumer polling the same
  controller, so this defaults conservatively tune it from 1 up to a
  controller's full observed limit based on actual available headroom.
  Names resolved once for `--all` or the interactive picker are reused
  for the final per-site documents instead of being fetched a second
  time.
- The vManage HTTP client sets a 15s request timeout (scoped to this
  producer only, not the shared `cisco/run` package) without one, a
  single slow or silently-dropped request could hang the whole run
  indefinitely. `GET /dataservice/device/interface`'s `mtu`/
  `speed-mbps` fields decode via a type tolerant of both a quoted
  string and a bare JSON number some controllers return `speed-mbps`
  as a number, not the quoted string the vManage OpenAPI spec's own
  example documents and Go's default JSON decoding into a
  string-typed field would reject that outright, failing the *entire*
  interface response's unmarshal. HTTP 429 (Too Many Requests)
  responses are retried with backoff (honoring a `Retry-After` header
  when present, otherwise capped exponential backoff).
- `metadata.scope.providers` is `cisco.vmanage` a dotted
  vendor.product-line provider name, matching
  `osiris/network/hpe/arubacentral`'s convention
  (apic/nxos/iosxe still use plain `cisco` today).
- `metadata.scope.clusters` records the controller host/FQDN used for
  the run (`--host` value).
- `metadata.generator.url` is set
  (`https://docs.osirisjson.org/osiris-producers/network/cisco`).
- `--help` renders a single, column-aligned, word-wrapped flag table
  generated directly from the same flag registration `ParseFlags`
  binds against, so it can never drift out of sync with the real flag
  set the same table is also shown as the fallback usage on a real
  parse error (e.g. an unrecognized flag).

### Known limitations
- Multi-tenant (Provider) collection across tenants is not implemented:
  the vManage OpenAPI spec has no documented tenant-switch/session
  endpoint.
- No CSV batch mode (`-s/--source`): one run targets one controller.
- Resource type mapping (personality -> OSIRIS type) is a first pass,
  pending review against a wider range of devices.
- Tunnel and OMP peering connections are only emitted when both
  endpoints resolve within the current site's own document
  (OSIRIS-JSON-v1.0 2.2.3 forbids a connection referencing a resource
  outside its document) since one document is written per site, a
  connection whose peer belongs to a different site is not represented
  in either document.
- Raw IPsec IKE session detail (`GET /dataservice/device/ipsec/ike/sessions`)
  and standalone BFD session/link data
  (`GET /dataservice/device/bfd/sessions`, `.../bfd/links`) are not
  collected - the interface, tunnel and OMP-link state already gathered
  covers `state` at every level these would otherwise inform, so the
  extra per-device calls did not add new modeled fields.

[Unreleased]: ../../../../CHANGELOG.md
[0.2.0]: ../../../../CHANGELOG.md#0130---2026-11-21
[0.1.0]: ../../../../CHANGELOG.md#070---2026-08-29
