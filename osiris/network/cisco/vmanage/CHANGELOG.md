# Changelog - Cisco Catalyst SD-WAN Manager vManage OSIRIS JSON Producer

All notable behavioral changes to the **`osirisjson-producer-cisco`**
producer's vManage (Cisco Catalyst SD-WAN Manager) backend are
documented in this file.

The format follows:
- [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Producer versioning follows:
- [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

To maintain a consistent line width across `CHANGELOG.md` and all file
comments (excluding links, code examples, and tables), we adhere to:
- [RFC 7994](https://datatracker.ietf.org/doc/html/rfc7994#section-4.3)

This file tracks the **producer's behavior version**
(`metadata.generator.version` in emitted documents). It is independent
of the repository's git tag - a single git tag may bump several
producers. See the root [`CHANGELOG.md`](../../../../CHANGELOG.md) for
the release-level index of which producers shipped under each tag.

---

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
  just duplicate this single value), and, `audit` purpose only,
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
  (`{latitude, longitude}`, matching OSIRIS-JSON-v1.0 6.5.1.1 own
  group example shape) is sourced from the first site device with
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
  `acme.sdwan.cisco.com`) as well as bare IPs, optionally with `:port`
  (`-P/--port` overrides). `-u/--username` for authentication. There is
  deliberately no `-p/--password` flag a CLI flag value is visible to
  any local user (e.g. via `ps`) and gets written to shell history.
  `-h`/`-u`/password are each resolved in this order: their own flag,
  then `--token-file`, then (for whichever is still missing) an
  interactive prompt on the controlling terminal host/username
  visibly, password hidden so a bare
  `osirisjson-producer cisco vmanage` with no flags at all still works
  end to end, asking for whatever it needs one at a time. Nothing
  entered interactively is written to disk unless `--token-file` was
  also given.
- `--token-file`: a JSON file with `{host, username, password}` - any
  field it omits still falls back to its own flag or an interactive
  prompt, so a partially-filled file is fine.
  `template --generate` writes a skeleton to
  `cisco-vmanage-secrets.json`.
- `--include-raw-body` (requires `--purpose audit`): attaches each
  collected endpoint's full, unmodified API response body to the
  owning device resource under `extensions["osiris.cisco.vmanage"]` -
  the device's own `GET /dataservice/device` object (`raw`),
  `interfaces_raw`, `wan_interfaces_raw` (when non-empty),
  `omp_peers_raw`, and `site_topology_raw` (matched back to the device
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
  picker, and used in `metadata.scope.sites` for the emitted document
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
  example documents, and Go's default JSON decoding into a
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

[0.1.0]: ../../../../CHANGELOG.md#unreleased
