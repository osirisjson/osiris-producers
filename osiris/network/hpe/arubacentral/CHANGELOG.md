# Changelog - HPE Aruba Networking Central OSIRIS JSON producer

All notable behavioral changes to the **`osirisjson-producer-hpe-arubacentral`**
producer (also reachable as `osirisjson-producer-arubacentral`) are
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

## [Unreleased]

## [0.1.0] - 2026-07-23

Initial HPE Aruba Networking Central producer release.

### Added
- OAuth2 API Gateway authentication (access/refresh token pair,
  refresh-on-401, `--token-file` persistence for rotated refresh tokens)
  and account-wide throttling to 8 req/s (a safety margin below the
  documented 10 req/s account limit).
- No `--client-id`/`--client-secret`/`--access-token`/`--refresh-token`
  CLI flags (visible via `ps` and shell history); credentials come from
  `--token-file`, environment variables, or an interactive `/dev/tty`
  prompt (hidden echo for secrets) for whatever those two didn't supply.
  Interactively-entered values are never written to disk.
- Cluster short-code -> base URL resolution for all 15 published Aruba
  Central clusters, plus `--base-url` override, plus automatic cluster
  detection (probes every cluster with the access token and uses
  whichever one accepts it) when neither is supplied.
- Interactive site picker when `--site` is omitted: lists the account's
  sites and accepts a numeric selection (`1`, `1,3,5`, `1-4`,
  combinations, or `all`), mirroring the Azure producer's subscription
  picker syntax. Falls back to collecting every site (no prompt) when
  the sites endpoint is unavailable or empty.
- Devices: switches (`network.switch`), access points
  (`osiris.hpe.arubacentral.accesspoint` - OSIRIS JSON's standard
  resource type taxonomy, chapter 7, defines no wireless family still,
  so this is vendor-namespaced per the taxonomy's custom-type fallback
  rather than invented under `network.*`), gateways (`network.gateway`),
  interfaces/ports (`network.interface`) with `contains` connections to
  their parent device.
- Switch sub-resources: VLANs and LAGs as `network.vlan` (standard) /
  `osiris.hpe.arubacentral.lag` (no standard link-aggregation group type
  exists in OSIRIS JSON spec chapter 6.2.3) groups, hardware health
  (CPU/memory/temperature/fans/PSU) folded into switch properties and
  status, stack membership (`osiris.hpe.arubacentral.stack` group - same
  rationale as LAG), VSX peering (`osiris.hpe.arubacentral.vsx`
  connection - VSX is proprietary Aruba AOS-CX technology, namespaced
  per OSIRIS JSON spec chapter 5.2.4 rather than treated as a standard
  `network.*` subtype).
- Wireless: WLANs (`osiris.hpe.arubacentral.wlan`), radios
  (`osiris.hpe.arubacentral.radio`), BSSIDs
  (`osiris.hpe.arubacentral.bssid`) with `contains`/broadcast
  connections from AP -> radio -> BSSID -> WLAN, IAP mesh swarms
  (`osiris.hpe.arubacentral.swarm` group) - same vendor-namespace
  rationale as access points, above.
- AP sub-resources: wired ports, tunnels
  (`osiris.hpe.arubacentral.tunnel` connection to a stub remote endpoint
  `network.tunnel` is not a standard connection subtype under OSIRIS
  JSON spec chapter 5.2.3, so this follows chapter 5.2.4's
  vendor-namespace pattern instead), per-AP WLAN broadcast connections.
- Gateway sub-resources: ports, VLANs, WAN uplinks
  (`osiris.hpe.arubacentral.uplink` connection to a stub WAN endpoint
  "uplink"/"uplinkendpoint" are not standard OSIRIS JSON types either,
  same rationale as the tunnel pair above).
- Unified wired/wireless clients (`osiris.hpe.arubacentral.client` no
  standard "client device" resource type exists in chapter 7.5)
  connected to their access device.
- Device-to-device neighbor adjacency (LLDP/CDP-like `network`
  connections, mirroring the Cisco NX-OS/IOS-XE neighbor-stub pattern)
  across switches, APs and gateways; a neighbor of not-yet-known device
  category is stubbed as `osiris.hpe.arubacentral.device` (no standard
  OSIRIS JSON type exists for a generic, unclassified network device).
- Sites (`osiris.hpe.arubacentral.site` no standard OSIRIS JSON
  resource type exists for a physical site/campus; the closest narrative
  example, `logical.site`, is a group type for location-based grouping,
  not this producer's inventory-item-with-health-status shape) and
  device groups (`logical.devicegroup`, with devices wired in via
  config-health device-group membership) best-effort, since the
  scope-management endpoint paths are inferred rather than confirmed
  against the API reference (see `client.go` doc comments). Each site
  also gets a `contains` connection (the authoritative graph edge, per
  OSIRIS JSON spec chapter 6.4.2's own "rack contains servers" example
  for physical, single-parent containment) to every switch/AP/gateway
  found there, plus a parallel, deliberately lean `logical.site` group
  (chapter 6.4.3's presentation layer membership only, no properties)
  with those same devices as members. Clients are intentionally not
  wired into either: they are numerous and already carry their site via
  `provider.site`, and a site's `device_count` refers to infrastructure,
  not end users.
- Config-health compliance summary/issues folded onto the matching
  device resource as a drift-detection signal (status downgrade +
  `config_status`/`top_priority_issue`/`config_issue_count` properties;
  full issue payload under `extensions["osiris.hpe.arubacentral"]` in
  `--purpose audit` only) also best-effort/inferred paths.
- `--site` filter to scope collection to a subset of sites, plus `--all`
  to auto-discover and export every accessible site non-interactively.
- `--purpose documentation|audit` and `--safe-failure-mode` per the
  OSIRIS JSON producer SDK contract the producer's only
  document-shaping contract.
- `metadata.scope.clusters` records which API Gateway cluster/base URL
  the export came from (e.g. `"eucentral3 (https://de3.api.central.arubanetworks.com)"`)
  and `metadata.scope.purpose` records which of `documentation`/`audit`
  the document was generated under document-wide context per OSIRIS JSON
  spec chapter 4.3.6/13.1.3.
- Site health (`GET /network-monitoring/v1/sites-health`) folded onto
  the matching `osiris.hpe.arubacentral.site` resource: overall health
  as a drift-detection signal (documentation; degrades the resource's
  status), plus device/client health breakdowns (audit only) - mirrors
  the device config-health enrichment. Best-effort: only the endpoint's
  documented sortable fields (site/device/client health) are modeled;
  the API also describes poor-health reasons and device/client/VM/host
  counts by tier that aren't confirmed field names against a live
  tenant, so they're not guessed at (see `client.go` doc comment
  re-run with `--purpose audit --include-raw-body` to capture the true
  payload and correct this).
- Per-device-category site health
  (`GET /network-monitoring/v1/sites-device-health` AP/switch/gateway/
  bridge health) folded onto `osiris.hpe.arubacentral.site`
  as `ap_health`/`switch_health`/`gateway_health`/`bridge_health` 
  audit only.
- `Unmanaged` type neighbors (the devices Central sees via LLDP/CDP but
  doesn't manage) get their stub resource enriched via
  `GET /network-monitoring/v1/unmanaged-device/{mac}` audit +
  `--include-raw-body` only, since the response shape isn't documented.
  The MAC is derived from the neighbor's `tpd_<12 hex chars>` synthetic
  serial (inferred pattern, not documented).
- Isolated-device reports
  (`GET /network-monitoring/v1/isolated-devices/{site-id}`) attached as
  raw data on the matching `osiris.hpe.arubacentral.site` resource
  audit + `--include-raw-body` only. Not modeled as named resources:
  the one tested example of this endpoint's response had an empty array,
  so per-device field names are unconfirmed.
- Support for HPE GreenLake "Personal API clients" (self-service
  client_id/client_secret pairs generated under
  `common.cloud.hpe.com/manage-account/api` when no IT-provisioned API
  Gateway application is available). An access token is minted via
  `grant_type=client_credentials` against the fixed GreenLake SSO
  endpoint (`sso.common.cloud.hpe.com/as/token.oauth2`) and re-minted
  the same way on expiry/401, rather than requiring a pre-issued
  access/refresh token pair the user of a personal client would not have.
- `--include-raw-body`: attaches the true, unmodified API response body
  for each switch/AP/gateway under
  `extensions["osiris.hpe.arubacentral"].raw` when combined with
  `--purpose audit` (a lossless fallback for fields not yet modeled).

### Changed
- Output layout unified across every run, one site or many:
  `<output-dir>/<site-name>/hpe-arubacentral-<timestamp>-<site-name>.json`
  `-o`/`--output` is always an output directory for a single site it
  previously meant a literal output file path; that mode is removed.
  Default when `-o` is omitted is a fixed directory name,
  `osirisjson-hpe-arubacentral`, not a timestamped one: re-running from
  the same working directory (or with `-o` pointing at the same
  directory) reuses the existing hierarchy and adds each site's new
  timestamped file alongside prior ones, instead of scattering a new
  directory per run. Running from a fresh working directory (or a new
  `-o` target) builds the hierarchy from scratch.
  See `OutputPath` in `config.go`.
- Every "Saved to" line and the final "export complete" `output_dir` log
  field now show the resolved absolute path, not whatever relative form
  `--output` (or the default) was given in so the log alone is enough
  to locate the files regardless of the working directory a run happened
  to start from.
- `-o`/`--output` is dual-purpose: for a single site it is a literal
  output file path; for a run that resolves to two or more sites
  (`--all`, a comma-separated `--site` list, or a numeric multi-pick) it
  is an output directory created if missing that each site's
  auto-generated filename is written into. With no `-o`, multi-site runs
  write to the current directory.
- A multi-site run (`--all`, interactive "all"/blank, a comma-separated
  `--site` list, or a numeric multi-pick) collects and writes one
  document per site, one `hpe-arubacentral-<timestamp>-<sitename>.json`
  per site, instead of bundling every selected site into one combined
  document (e.g. what would otherwise be
  `hpe-arubacentral-<timestamp>-all-sites.json` for "all"). A single
  site is unaffected. The default output filename is
  `hpe-arubacentral-<filesystem-safe-timestamp>-<sitename>.json`
  (`all-sites` for the unfiltered fallback, the sanitized site name for
  a single site, `+`-joined names for a few, `<n>-sites` beyond that);
  RFC3339 colons are replaced with dashes since they are illegal in
  Windows filenames.
- `--purpose audit` vs `documentation`: client
  (`osiris.hpe.arubacentral.client`) properties that fingerprint the
  specific device host name, user name, OS, manufacturer,
  authentication type, connection timestamp are audit-purpose only;
  `documentation` keeps just connectivity (MAC, VLAN/WLAN, site, IP).
- `ListAPsWithRaw` scopes to the requested site(s) server-side
  (`siteId in (...)`, alongside the status filter) whenever `--site`
  resolves to specific sites, instead of always fetching every access
  point in the account and discarding the rest client-side.

### Fixed
- Fatal crash on every tested account: `Switch.stackMemberId` was typed
  as Go string but the API returns a JSON number, so `ListSwitches` (and
  therefore the whole run) failed with `json: cannot unmarshal number
  into Go struct field Switch.stackMemberId of type string`. Corrected
  to an int.
- Sites/device-groups collection always failing with
  `400 PAGE_LIMIT_SIZE_EXCEEDED`: those two `network-config/v1alpha1`
  endpoints reject the 1000-item page size used elsewhere in this
  client. They now page at 100 items (`configPageLimit`) instead. This
  also confirms both endpoint paths are correct against a test (a
  specific 400, not a 404).
- Another fatal crash on tested switch telemetry:
  `SwitchTrend.cpuUtilization`/`memoryUtilization`/`systemTemperature`
  (and the same two fields on `AccessPoint`/`Gateway`) were typed as Go
  ints but the API reports them as fractional numbers (e.g. `31.25`), so
  `ListSwitches` failed with
  `json: cannot unmarshal number 31.25 into Go struct field ... of type int`.
  Corrected to float64, matching the PoE/power fields already on `SwitchTrend`.
- Device-to-device topology (neighbor adjacency) was silently dropped
  entirely: `/neighbours/{serial}` doesn't use the `{"items": [...]}`
  envelope every other list endpoint in this client does, and since the
  only caller checked `err == nil` with no logging, a parsing failure
  read as "no neighbors" instead of a bug - no switch-to-switch,
  switch-to-AP, or unmanaged-device adjacency was ever collected. A
  first fix (assuming a bare array, based on the fixture-dumping tool
  splitting results into one file per neighbor) was itself disproved
  against a test (`cannot unmarshal object into []Neighbor`);
  parsing is now tolerant of multiple shapes (bare array, the standard
  envelope, or a flat object keyed by neighbor identifier) so the next
  surprise degrades gracefully instead of silently dropping data again.
  Also: `/neighbours` mixes in `Client` and `Stack` type entries that
  duplicate data already modeled elsewhere
  (`osiris.hpe.arubacentral.client` connections,
  `osiris.hpe.arubacentral.stack` groups) - these are filtered out to
  avoid double-counting/bogus stubs; genuine device types (`Switch`,
  `Access Point`, `Gateway`, `Unmanaged`, and any not yet seen) are kept.
- Switch stacks: `/switches/{serial}/interfaces` and
  `/hardware-categories` 404 when queried by a non-conductor stack
  member's own serial (confirmed against a test); only the
  stack's conductor accepts these two, and its response covers every
  physical member (each item carrying its own `serialNumber`).
  Collection now queries only the conductor for a stack and routes each
  returned interface/hardware item back to its own member's switch
  resource, instead of erroring per member and misattributing everything
  it did get to the conductor alone.
- `/gateways/{serial}/vlans` always failing with
  `400 "limit value was either less than 0 or greater than the maximum supported limit"`:
  the API reference caps this endpoint at 100 items/page and requires cursor
  pagination, unlike the 1000/offset used elsewhere in this client. Also
  switched `/gateways/{serial}/ports` and `/aps/{serial}/tunnels` to
  cursor pagination per the reference (dormant bug: within limit range
  today, so not yet observed failing, but wrong pagination style).
- `osiris.hpe.arubacentral.site` resources were not filtered by `--site`:
  a single-site export still listed every site in the account
  (`/network-config/v1alpha1/sites` was never passed through the same
  site filter already applied to switches/APs/gateways). The document
  now only describes the site(s) it was scoped to collect, per OSIRIS
  JSON spec chapter 4.3.6 (`metadata.scope` sets the boundary;
  resources must stay within it).
- Same class of bug, found during tests against a multi-site account:
  `osiris.hpe.arubacentral.client` resources were not filtered by
  `--site` either. `/network-monitoring/v1/clients` is an account-wide
  endpoint with no site-scoped query parameter, and unlike
  switches/APs/gateways/sites its result was never passed through
  `keepSite` before being transformed, so every site's export bundled
  every other site's clients too - inflating each document's size and
  making every per-site snapshot inaccurate (each one described the
  whole account's client population, not just that site's).
- Found during tests with thousands of clients:
  `--purpose audit --safe-failure-mode fail-closed` aborted the entire
  document build for any site with client data, erroring "secret
  scanning detected N finding(s)" against every client's
  `authentication_type` property. The value is a classification string
  (e.g. "802.1X", "WPA3-PSK"), not a secret, but the key name contains
  the substring "auth", which `pkg/sdk`'s cross-producer secret scanner
  (`redact.go`, `SensitiveKeyPatterns`) treats as sensitive by design
  (to catch `auth_token`, `authorization_header`, etc.) - a false
  positive this producer triggered, not a scanner bug, so the fix is
  here rather than in the shared SDK. The property is now named
  `security_type`, matching `TransformWLANs`' `security`/
  `security_level` naming convention.
- `ListSites` called the pre-release `network-config/v1alpha1/sites`
  path (inferred from the `device-groups` endpoint naming convention,
  since sites was never part of the curated API reference this producer
  otherwise follows). Confirmed against the official reference: the
  stable path is `network-config/v1/sites`. Switched to it; the response
  shape (`items`, offset/limit pagination) is unchanged, so no transform
  changes were needed.

### Known limitations (follow-up work)
- Found during tests, confirmed by crosschecking a tested site's export
  against Aruba Central's own CSV status report:
  `/network-monitoring/v1/aps` returned only `ONLINE` access points when
  called with no filter offline APs were silently absent from the result
  entirely, not merely mis-reported.
  Unlike this same gap, `/switches` and `/gateways` do not default this
  way (confirmed: every offline switch/gateway on the test site was
  present, just occasionally with a stale `status` value a separate,
  upstream Aruba Central data-freshness issue this producer cannot
  correct, since it faithfully reports whatever status string the API
  endpoint itself returns).
  [Opened a discussion on Airheads community about this issue](https://airheads.hpe.com/discussion/new-central-api-get-a-list-of-access-points)
- Sites and device-groups endpoint paths are confirmed to work against a
  test (see Fixed, above). `ListSites` now additionally uses the
  confirmed-stable `network-config/v1/sites` path per the official API
  reference, not the pre-release `v1alpha1` path this producer used
  before that reference was checked. `device-groups` and config-health
  summary/issues remain on the unconfirmed `v1alpha1`-pattern paths -
  not yet verified against the official reference.
- Not yet collected: `mon-topology` (redundant with device+neighbor data
  collected today), `mon-isolated-device`, gateway DHCP pools/leases,
  gateway WAN/LAN tunnel health summaries, application visibility,
  firmware/audit/webhook/event services, and the full
  `network-config/v1alpha1` configuration-management surface
  (deliberately out of scope - OSIRIS models topology, not
  configuration/IaC).
- No multi-account batch mode: one credential set collects one Aruba
  Central account per run (unlike the Azure/AWS producers' CSV batch
  across many accounts), since Aruba Central's auth model is
  account-scoped, not subscription-scoped.

[Unreleased]: ../../../../CHANGELOG.md
[0.1.0]: ../../../../CHANGELOG.md#010---2026-07-23
