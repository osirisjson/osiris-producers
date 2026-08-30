# Changelog Cisco NX-OS OSIRIS JSON producer

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

## [0.2.0] - 2026-08-29

### Added
- `metadata.generator.url` is now emitted
  (`https://docs.osirisjson.org/osiris-producers/network/cisco`), and
  `metadata.scope` now carries `name` (the collected switch's own
  reported hostname) and `purpose` (the `--purpose` level the run
  collected at, so a consumer holding only the document can tell a
  documentation snapshot from an audit one).
- External/unresolved neighbor stub resources (LLDP/CDP, OSPF, BGP,
  vPC-keepalive) now set `provider.source` to the discovery protocol
  that observed them (`lldp`, `cdp`, `lldp+cdp`, `ospf`, `bgp`,
  `vpc-keepalive`) the "additional context" OSIRIS-JSON-v1.0 4.1.2
  calls for alongside `provider.name: "unknown"`.
- New `osiris.cisco.aaa` resource (detailed mode only), one per device,
  capturing AAA/RADIUS/TACACS+ posture: `login_methods` and
  `accounting_methods` (from `show aaa authentication`
  `show aaa accounting`), `server_groups` (`show aaa groups`), `radius`/
  `tacacs` global posture plus `tacacs_servers` (server IP/port/timeout
  only) from `show radius-server`/`show tacacs-server`. The switch
  `contains` this resource. No standard OSIRIS resource type exist still
  for authentication server or its client-side configuration, so this
  uses the vendor-namespaced custom type per OSIRIS-JSON-v1.0 section
  7.7.2, matching the `osiris.cisco.aci` precedent that section's own
  guideline table names. TACACS's `secretKey`/`global_testPassword`
  fields (and RADIUS's per-server equivalent, not yet modeled at all
  this device had 0 RADIUS servers configured, so there is no real
  capture to ground that row shape against) are never given a struct
  field to decode into, so no code path can emit them regardless of
  purpose. Local-account/role mapping (`show user-account`) is
  deliberately out of this pass no raw capture and no vendor-doc JSON
  shape exists to ground it against.
- `--secrets-file` now accepts an alternate "rules" shape for a batch
  against devices that do not all share one login:
  `{"default": {username, password}, "rules": [{"hosts": "...",
  "username", "password"}, ...]}`. A target's host is matched against
  each rule's `hosts` (a comma-separated list of exact hosts/IPs and/or
  CIDR blocks, e.g. `"10.0.1.0/24"` or `"10.0.2.10,10.0.2.11"`) in
  order, first match wins; an unmatched target falls back to `default`.
  Applies to both single mode (matched against the resolved `--host`)
  and batch mode (matched per CSV row). There is no per-rule port: the
  CSV `port` column remains the sole source of truth for a target's
  port. The original flat `{host, username, password}` shape is
  unchanged and still works exactly as before this is purely
  additive.
- `show port-channel summary`'s response (already fetched every run, but
  previously discarded unread) now produces `contains` connections from
  each port-channel (LAG) interface resource to its bundled physical
  member interfaces, plus a `member_count` property on the LAG resource
  itself. A member port not otherwise present in `show interface brief`
  is skipped rather than producing a dangling connection. The per-row
  LACP/PAgP/static protocol field (`prtcl`) is now extracted as
  `properties.protocol` on the LAG resource.
- Device resources now carry a `coverage` extension array (one entry per
  command issued this run: `command`, `status` of `succeeded`/`failed`/
  `unavailable`, and `error` when applicable) so a malformed or dropped
  command is visible in the emitted document itself, not only inferable
  from stderr logs.
- The switch resource now `contains` every interface resource it owns:
  a `contains.physical` connection to each physical port
  (`network.switch.port`), a `contains.logical` connection to each
  logical interface (`network.interface` port-channel/LAG, loopback,
  Vlan SVI). Neither subtype is spec-enumerated for `contains`, but
  follows the same optional dot-notation extension pattern
  OSIRIS-JSON-v1.0 documents for its other standard connection types.
- Switch resource properties gained `manufacturer` (constant `"Cisco"`),
  `version` (previously only on `provider.version`), and `port_count`
  (a count of its `network.switch.port` resources).
- Physical port (`network.switch.port`) resources gained
  `interface_name`, and their `speed` (raw string) is now `speed_mbps`
  (a JSON integer, parsed only when the source value is already
  numeric a symbolic value like `"auto"` is omitted, never guessed at
  a number).
- `show cdp neighbors detail` is now collected alongside LLDP and merged
  into one neighbor-discovery pass: a neighbor reported by both
  protocol on the same local port yields exactly one `physical.ethernet`
  connection (not two), with `properties.discovered_via` listing every
  protocol that saw it (`["lldp"]`, `["cdp"]`, or `["lldp","cdp"]`); a
  local port with only a CDP observation (e.g. `mgmt0`, which often
  speaks CDP but not LLDP) still surfaces on its own.
- Physical port resources gained `ip_address` (a bare address, no
  subnet mask `show ip interface brief vrf all` does not report one;
  full prefix/subnet mapping needs `show ip interface`, not
  implemented this pass) and `native_vlan` (trunk mode's untagged
  VLAN, from `show interface switchport`).
- Trunk ports now get a `network.l2` connection to every `network.vlan`
  resource named in their `show interface switchport` `trunk_vlans`
  list (the same VLAN resources `show vlan brief` already produces)
  NX-OS's own range-compressed format (e.g. `"85,900,906-909"`) is
  expanded to individual VLAN IDs first. A VLAN allowed on the trunk
  but not itself present in `show vlan brief` has no resource to
  connect to and is skipped, matching every other connection-wiring
  function's own resolvable-relationships-only policy.
- The switch resource gains a new `network` connection (role
  `vpc_keepalive`) to an external stub resource when vPC keepalive is
  configured (`show vpc peer-keepalive`) a distinct control-plane
  relationship from the peer-link/vPC-member wiring, never conflated
  with it.
- OSPF (`show ip ospf neighbor vrf all`), BGP (`show bgp all summary`)
  neighbor sessions now produce `network.ospf`/`network.bgp`
  connections to an external stub resource per peer OSPF from the
  local interface the neighbor was seen on, BGP from the switch itself
  (this command's own output has no per-neighbor local-interface
  field). BGP peers are limited to the default VRF no NX-API-JSON-
  compatible command covering every VRF at once has been found;
  `show ip bgp summary vrf all` is rejected outright for structured
  output on this platform. A neighbor reported under more than one
  local interface (OSPF) or address family (BGP) both real,
  unremarkable topologies produces exactly one stub resource and one
  connection per interface adjacency (OSPF) or exactly one of each
  overall (BGP), never a duplicate resource ID. OSPF's `dr_state` also
  omits its own "no DR/BDR role" placeholder even when the source value
  carries a leading space (a real device reported it padded). No route
  tables are collected or emitted for either protocol only the
  session/peer relationship itself, per this producer's existing
  data-minimization policy.
- **Field-name confidence, disclosed explicitly per command**: CDP,
  `show ip interface brief vrf all`, `show ip ospf neighbor vrf all`,
  `show bgp all summary`, and `show interface switchport` field names
  are all grounded in real device JSON. `show vpc peer-keepalive`
  remains this producer's best-effort convention-based guess no real
  device JSON exists for it yet (only plain CLI text). Every guessed
  field is isolated by this producer's
  existing per-command failure/shape-mismatch handling (see the
  `ShowResult`/`decodeBody` `[Unreleased]` entry below): a wrong guess
  silently yields no data for that one command, logged as a warning,
  never a wrong or corrupted document. Each affected DTO in `dto.go`
  carries its own `UNVERIFIED` doc comment naming exactly which fields
  remain unconfirmed.
- New `osiris.cisco.aaa` resource (`--purpose audit` only), one per
  device, capturing AAA/RADIUS/TACACS+ posture: `login_methods` and
  `accounting_methods` (from `show aaa authentication`/
  `show aaa accounting`), `server_groups` (`show aaa groups`), and 
  `radius`/`tacacs` global posture plus `tacacs_servers`
  (server IP/port/timeout only) from `show radius-server`
  `show tacacs-server`. The switch `contains` this resource.
  No standard OSIRIS resource type exists for
  an authentication server or its client-side configuration, so this
  uses the vendor-namespaced custom type per OSIRIS-JSON-v1.0 section
  7.7.2 (`osiris.<vendor>.<type>`), matching the `osiris.cisco.aci`
  precedent that same section's guideline table names. TACACS's
  `secretKey`/`global_testPassword` fields (and RADIUS's per-server
  equivalent, not yet modeled at all no real capture with configured
  RADIUS servers exists to ground that row shape against) are never
  given a struct field to decode into, so no code path can emit them
  regardless of purpose. Local-account/role mapping
  (`show user-account`) is deliberately out of this pass no raw capture
  and no vendor-doc JSON shape exists to ground it against.
- `show module` is now collected (base topology/inventory tier, not
  `--purpose audit`-gated) and merged into the device's `osiris.cisco`
  extension as `modules`: one entry per module with `status`
  (operational state), `diag_status` (POST result), `model`/`type`/
  `ports`, and `hw_version`/`sw_version`. The command's own four
  parallel tables key the same module by a differently-named field
  (`modinf`/`mod`/`modwwn`) merged here by that shared module
  number. Distinct from the existing `inventory` extension
  (`show inventory`, static FRU identity): this is the module's own
  operational status. The command's own MAC-address-range table
  (`TABLE_modmacinfo`) is not modeled its serial number always
  duplicates the chassis serial already captured elsewhere on this
  single-module platform, and the MAC range itself has no use in an
  inventory document.
- `show environment`'s fan data (`fandetails.TABLE_faninfo`, already
  present in every response fetched but previously never decoded) now
  populates a new `fans` array in the same `osiris.cisco` extension as
  `power_supplies`/`temperature`: `name`, `model` (omitted for the
  `"--"` placeholder value fixed fan-in-PSU units report), `status`,
  `direction`.
- `show interface transceiver` is now collected (base topology/
  inventory tier) and attached as `source_transceiver`
  (OSIRIS-JSON-v1.0 5.4.2) on each `physical.ethernet` neighbor
  connection this producer already builds from LLDP/CDP, for whichever
  side is the local device's own port: `vendor`, `model`
  (`cisco_product_id`), `part_number` (`partnum`). Ports reporting no
  SFP/QSFP physically present are omitted entirely, not represented
  with an empty object. `target_transceiver` is never populated this
  producer only queries the local device's own NX-API, never the
  remote neighbor identified by LLDP/CDP, so the far side's transceiver
  is genuinely not discoverable here, not merely unmodeled.
  `form_factor` (spec: `sfp`/`sfp+`/`sfp28`/`qsfp28`/`qsfp-dd`) is
  deliberately not populated NX-API reports no discrete field for it
  on this command, and deriving one from `cisco_product_id` naming
  convention would be an unverified guess presented as sourced data,
  the same category of mistake already made once for a different
  producer's VRF/VPN-ID naming and not repeated here. A port with a
  transceiver present but no LLDP/CDP-discovered neighbor has nowhere
  to attach this data under the spec's connection-only
  `source_transceiver` definition, and is not surfaced a known scope
  limit of the connection-only placement chosen for this data,
  not a defect.

### Changed
- VLANs are now core `network.vlan` **resources** (OSIRIS-JSON-v1.0
  7.5.1), not groups: `properties.vlan_id` (integer),
  `properties.vlan_name`, `properties.admin_state`, and the resource's
  own `status` mapped from the VLAN state. Port membership is now a set
  of `network.l2` connections (switch port <-> `network.vlan` resource,
  5.2.3) rather than group membership a consumer wanting "VLAN N has
  these ports" derives it from those edges. `topology.groups` no longer
  contains any `network.vlan` entries.
- Port-channel interfaces are now `network.interface.lag` (7.5.3 with
  the 4.2.3 extended-hierarchy `.<variant>` suffix), not the generic
  `network.interface` so a consumer can select LAGs by type instead
  of string-matching the name. The same string is used across every
  Cisco producer in this repository.
- The vPC-domain group type is `osiris.cisco.vpc`, not `network.vpc`: a
  Cisco virtual Port Channel (an L2 multi-chassis link-aggregation
  domain) is unrelated to 7.5.1's `network.vpc` (a hyperscaler Virtual
  Private Cloud / VNet); per 4.2.6 / 7.7.2 a vendor construct with no
  standard equivalent takes an `osiris.<vendor>.<type>` type. Only
  emitted on a vPC-configured device.
- External/unresolved neighbor stub resources (LLDP, CDP, and the new
  vPC-keepalive/OSPF/BGP peer stubs) now use `provider.name: "unknown"`
  instead of `"cisco"` (this producer's own provider name). LLDP is
  vendor-neutral and CDP does not establish the remote is Cisco
  hardware either asserting `"cisco"` for a device this producer
  never actually queried was an unsupported claim, not sourced data.
- The switch (root device) resource type is now always the core
  `network.switch`, never the custom `osiris.cisco.switch.leaf`/
  `.spine` types those were driven by a hostname/model substring
  guess (`classifyRole`), which could misclassify any device whose name
  or model didn't happen to match the heuristic. No command this
  producer issues reports fabric role today, so nothing is guessed;
  the standard extended-hierarchy forms (`network.switch.leaf`/
  `.spine`, OSIRIS-JSON-v1.0 section 4.2.3) remain available for a
  future increment once a real source exists.
- Interface classification is now physical-vs-logical, not
  port-channel-vs-everything-else: `network.switch.port` for every
  interface with a real transceiver slot on the chassis (`Ethernet*`
  and `mgmt0`), `network.interface` for everything logical
  (port-channel/LAG, loopback, Vlan SVI). The custom
  `osiris.cisco.interface.lag` type is removed a port-channel is now
  a plain `network.interface`, distinguished by name and its own
  `contains` connections, not a separate type.
- Interface `vlan` is now a JSON integer (was a raw string); a
  non-numeric brief-mode value (e.g. `"--"`) is omitted rather than
  coerced.
- Switch and interface resource IDs (and `provider.native_id`) are now
  derived from the device's own chassis serial number
  (`proc_board_id`), falling back to the connection's target host only
  when the device reports no serial. Previously derived from the
  connection hostname, so renaming a target in inventory silently
  minted new resource IDs for the same physical hardware; a target
  alias change no longer changes any resource ID. **Breaking** every
  already-emitted switch/interface resource ID changes; not yet
  released, so no separate migration window.
- Switch resource `name` now prefers the device's own self-reported
  hostname (`show version`'s `host_name`), falling back to the
  connection's target-supplied hostname only when the device reported
  none.
- `--detail minimal|detailed` is replaced by the standard
  `--purpose documentation|audit` contract (`pkg/osirismeta`, already
  used by every other producer). Also adds `--include-raw-body`
  (audit-only, attaches each collected NX-API command's full raw
  response body under `extensions["osiris.cisco"]["raw_commands"]`,
  keyed by command string).
  Raw bodies are run through a redaction pass first:
  any value under a key matching `pkg/sdk`'s sensitive-key patterns
  (TACACS+/RADIUS `secretKey`/`testPassword`, SNMP community strings,
  `*_key`/`*_secret`/`*_token`), and any string value matching its
  secret-value patterns (PEM keys, bearer tokens, ...), is replaced
  with `[REDACTED]`; a body that cannot be parsed for redaction is
  dropped rather than attached. BGP/OSPF neighbor connections
  (`network.bgp`/`network.ospf`), interface counters, environment, and
  the new `osiris.cisco.aaa` posture resource are now all
  `--purpose audit` only a `documentation` purpose run collects only
  base topology/inventory and never even issues those NX-API commands.
- This producer now owns its own CLI flag parsing/config/single-batch
  runner entirely (new `flags.go`/`config.go`; `Run`, the runners and
  `--help` live in `nxos.go`) instead of going through the shared
  `osiris/network/cisco/run` package's `ParseFlags`/`RunConfig`/
  `RunBatch`/`ProducerFactory` matching `vmanage`'s existing
  one-file shape and the parent Cisco architecture review's own
  direction to move orchestration into each producer. `NewFactory`
  is removed; `osiris/network/cisco/cisco.go` now dispatches directly
  to `nxos.Run`. `run` retains only genuinely vendor-agnostic
  mechanisms this producer still calls directly (`ParseCSV`,
  `GenerateTemplates`, `TargetConfig`, credential-file handling,
  prompting, path/timestamp helpers) `RunConfig`/`ParseFlags`/
  `RunBatch`/`ProducerFactory`/`FactoryRegistry` are removed from `run`
  entirely (dead once `apic`/`iosxe` made the same move in the same
  pass). No change to emitted document shape from this reorganization
  itself.
- Batch mode is now selected explicitly by `--source` rather than
  inferred from having more than one CSV row a one-row CSV run now
  correctly honors its requested `--output` directory layout the same
  way a multi-row run does.
- `osirisjson-producer cisco template --generate nxos` is replaced by
  `osirisjson-producer cisco nxos template --generate`, matching
  vManage's own `<producer> template --generate` shape. The new form
  writes `cisco-nxos-template.csv` and both `--secrets-file` shapes as
  separate files (see Fixed below). The old top-level
  `cisco template --generate <name>` command no longer exists for any
  of apic/iosxe/nxos.
- Batch CSV columns changed to align with OSIRIS-JSON-v1.0 section
  7.6.5's physical containment levels and drop columns the producer
  never used: `dc,floor,room,zone,hostname,type,ip,port,owner,notes` is
  replaced by `datacenter,floor,room,rack,hostname,management_ip,port`.
  `type` is removed entirely a batch CSV is inherently single-producer
  (every row in the file passed to `cisco nxos -s ...` is an NX-OS
  target; the subcommand already says so), so naming it a second time
  in a column invited exactly the drift this file's own earlier `type`
  handling had. `owner`/`notes` (human-only metadata, never read by any
  producer or the emitted document) are also removed. `dc`/`zone`
  rename to `datacenter`/`rack`; `ip` renames to `management_ip`.
  `datacenter`/`floor`/`room`/`rack` remain optional and, when present,
  still build the output directory hierarchy (now
  `Datacenter/Floor/Room/Rack/Hostname.json`); reserved for a future
  physical.datacenter/room/rack group mapping, not yet implemented.
  `TargetConfig`'s `DC`/`Zone`/`Owner`/`Notes` fields renamed/removed
  to match (`Datacenter`/`Rack` survive, `Owner`/`Notes` are gone).
- Every NX-API command response this producer decodes now goes through
  a typed Go struct (new `dto.go`) instead of an untyped `map[string]any`
  navigated by string-keyed helpers. NX-API's own single-row-vs-array
  polymorphism (`TABLE_x`/`ROW_x`) is now handled once, generically, by
  a `rowList[T]` type instead of being re-parsed ad hoc in every
  transform function; a scalar field that may arrive as either a JSON
  string or a JSON number is handled the same way, via `flexString`/
  `flexInt64`. `ShowResult.Body` changed from `map[string]any` to
  `json.RawMessage` decoding into a specific shape is now the
  caller's decision, not the client's. Purely an internal
  representation change: every already-emitted field, resource,
  connection and group is unchanged; a command body whose real shape
  does not match its typed struct now logs a warning and is treated as
  empty, the same graceful-degradation policy already applied to a
  failed or unavailable command.
- Hardware serial numbers are now `--purpose audit`-only throughout:
  `inventory[].serial` (`show inventory`, previously emitted at
  `documentation` purpose) and the new `source_transceiver.serial_number`
  above. Matches the serial-number precedent already established
  elsewhere in this repository for asset-identifying hardware data.
  **Breaking** to already-emitted `documentation`-purpose `inventory[]`
  entries (the `serial` key disappears from that field at
  `documentation` purpose); not yet released, so no separate migration
  window.
- `show environment` power-supply (`power_supplies[]`) and
  temperature (`temperature[]`) extension entries no longer include
  live readings: `actual_output` (PSU real-time watts drawn) and
  `current` (instantaneous sensor temperature) are removed both are
  volatile telemetry, excluded per OSIRIS-JSON-v1.0 13.1.3 even though
  this whole extension is already `--purpose audit`-gated (audit widens
  stable-posture depth, it does not admit time-series data).
  `power_supplies[].capacity` (rated PSU capacity, stable) and
  `temperature[].major_threshold_c`/`minor_threshold_c` (configured
  alarm thresholds, stable) are added in their place.
- Switch resource property `memory` is renamed `memory_mb`
  (OSIRIS-JSON-v1.0's key-encodes-its-unit convention, e.g.
  `memory_gb`/`memory_mb` elsewhere in the spec). Confirmed against a
  real production capture, this producer's own `memory_type` property
  never actually worked: `mem_type` is a unit label (`"kB"`), not a
  numeric type code, but was decoded as an integer field that silently
  produced `0` for a non-numeric string on every run. `memory_mb` now
  divides the raw value by 1024 when the (correctly-decoded) unit is
  `kB`, or passes it through unchanged for `MB` (unconfirmed on any
  real device, kept only in case another platform reports it this
  way); an empty or otherwise unrecognized unit omits `memory_mb`
  entirely rather than emitting a value under a `_mb` key that might
  not really be megabytes. `memory_type` is removed redundant once
  the unit is fixed and baked into `memory_mb` own key name.
- Resource `id` values are now OSIRIS-JSON-v1.0 section 2.1.2
  "namespaced native ID" strategy (`<provider>::<native-id>`, the
  spec's own example is `cisco::FOC1234ABCD`) instead of an opaque,
  SDK-generated `res-<type>-<hint>-<hash>` string that never actually
  incorporated the device's own real native identifier. `network.switch`
  is now `cisco.nxos::<chassis-serial>`; `network.switch.port`/
  `network.interface` (physical/logical interfaces) are now
  `cisco.nxos::<chassis-serial>/<interface-name>` (e.g.
  `cisco.nxos::TST0000NX01/Ethernet1/1`); the `osiris.cisco.aaa`
  resource is `cisco.nxos::<chassis-serial>/aaa`. Matches the pattern
  already used by the `vmanage` producer in this
  repository (`resourceID(nativeID) = providerName + "::" + nativeID`).
- External LLDP/CDP/OSPF/BGP/vPC-keepalive neighbor stub resources'
  `id` is now anchored under the owning switch's own already-namespaced
  `id` (e.g. `cisco.nxos::TST0000NX01/neighbor/REMOTE-SW01/Ethernet1/49`,
  `cisco.nxos::TST0000NX01/ospf-neighbor/<router-id>`,
  `cisco.nxos::TST0000NX01/bgp-neighbor/<neighbor-id>`,
  `cisco.nxos::TST0000NX01/vpc-peer-keepalive/<dest>`) instead of a
  separate `unknown::<remote-system>|<remote-port>`-style identity
  this producer is the one minting the id for a resource it never
  actually queried or resolved, so the id itself should not imply a
  independently-verified "unknown"-namespaced identity for the remote
  side. `provider.name` on these stub resources is unchanged
  (`unknown` still accurate, this producer genuinely does not know
  the remote device's real vendor); only the `id` field's own
  construction changed. `provider.native_id` is also unchanged (still
  the raw observed composite, e.g. `REMOTE-SW01|Ethernet1/49`).
- `provider.name` (and `metadata.scope.providers`) changes from the
  plain `cisco` apic/iosxe still use to the dotted `cisco.nxos` matching
  the `cisco.vmanage` precedent the `id` change
  above requires this, since a resource's `id` and its own
  `provider.name` must agree on which namespace it came from. apic/iosxe
  are unaffected by this commit and remain on plain `cisco` for now.
- **Breaking** to every already-emitted `network.switch`/
  `network.switch.port`/`network.interface`/`osiris.cisco.aaa`
  neighbor-stub resource ID and every `network.switch`/`osiris.cisco.aaa`
  resource's `provider.name`; not yet released, so no separate
  migration window.
- vPC keepalive's connection `properties.role`/`properties.status` move
  to `extensions.osiris.cisco.role`/`extensions.osiris.cisco.keepalive_status`.
  OSIRIS-JSON-v1.0 5.2.3 has no vPC-specific `network.*` subtype still
  (l2/l3/bgp/ospf/vpn), so this connection stays the bare standard
  `network` type checked directly against the spec rather than
  inventing a `properties.role` key to disambiguate it, matching 5.4.3
  own worked example of vendor-specific connection data living under
  `extensions.osiris.cisco`. LLDP/CDP-derived neighbor connections keep
  `physical.ethernet` unchanged (deliberate since the 0.1.0 release
  LLDP/CDP frames are not routed past the first hop, matching
  `physical.*`'s own "cables, fiber, power" meaning, and
  `source_transceiver` is a documented `physical.*` connection property
  per 5.4.2, already populated on these connections). OSPF/BGP neighbor
  connections (`network.ospf`/`network.bgp`) were already exact 5.2.3
  matches no change. **Breaking** to the vPC keepalive connection's
  already-emitted `properties.role`/`properties.status`; not yet
  released, so no separate migration window.

### Removed
- `show system resources` is no longer collected at `--purpose audit`,
  and its `extensions.osiris.cisco` fields (`cpu_idle`, `load_avg_1min`,
  `memory_used`, `memory_free`) are no longer emitted real-time
  telemetry excluded by OSIRIS-JSON-v1.0 13.1.3 even at audit purpose.
  Total RAM is already covered by `properties.memory_mb`.
- `properties.serial` (renamed to `serial_number`, matching the
  `network.router` precedent elsewhere in this repository) and
  `properties.chassis_id` (duplicated `properties.model` under a
  second key).
- `properties.hostname` on the switch resource redundant now that
  `name` sources from the same field.
- `properties.mode` on interface resources an undocumented raw
  NX-API value never confirmed against OSIRIS-JSON-v1.0's
  `network.switch.port`/`network.interface` property tables.
- `properties.layer3_capable` is never emitted no command this
  producer issues reports it, and inferring it from the model number
  would be a guess presented as sourced data.
- The custom `osiris.cisco.interface.lag` type port-channels are now
  a plain `network.interface` (see Changed above).

### Fixed
- `--help` and the usage shown on a flag-parse error now render the
  flag list from the registered flag set itself (the same
  `registerFlags` `ParseFlags` binds against), matching the
  `osiris/network/cisco/vmanage` pattern.
- Device `extensions.osiris.cisco.uptime` was populated from
  `show version`'s `rr_sys_ver` field, which is the NX-OS version
  running at the last reset (part of the `rr_*` reset-reason block),
  not an uptime. The key is renamed to `last_reset_version`, pairing
  with the adjacent `last_reset_reason`.
  System uptime is `kernel_uptime`.
- `show inventory` and `show module` string fields (`name`,
  `description`, ...) are now stripped of the literal double-quote
  characters NX-API's JSON serializer wraps them in (`"\"Chassis\""`
  became a value of `"Chassis"` with the quotes; now `Chassis`). The
  shared `trimmed()` helper, previously whitespace-only, now also
  removes one matched surrounding quote pair.
- Removed `-p`/`--password`: a CLI flag value is visible to any local
  user via `ps` and is written to shell history. Passwords now resolve
  via `--secrets-file` (a permission-checked JSON file rejecting
  symlinks, group/other access, and non-owner files), then the
  `OSIRISJSON_CISCO_NXOS_PASSWORD` environment variable, then an
  interactive prompt with echo disabled. `--host`/`--username` gained
  the same interactive-prompt fallback (shared `osiris/network/cisco/run`
  change; also affects `apic` and `iosxe`, see their own changelogs).
- NX-API requests now carry a 30s total timeout and a per-attempt
  context deadline, so a stalled device connection can no longer hang a
  collection run indefinitely.
- NX-API response bodies are now bounded to 32 MiB; an oversized or
  runaway response is rejected instead of being read into memory in
  full.
- NX-API HTTP 429/503 responses are now retried with backoff (honoring
  a `Retry-After` header); 401/403 (authentication failure) are never
  retried.
- `--insecure` now logs an explicit warning naming the target host,
  instead of silently skipping TLS certificate verification.
- Output files (`cisco-nxos-<timestamp>-<hostname>.json` and the batch
  CSV hierarchy) are now written with `0600` permissions instead of
  `0644`.
- Batch CSV `dc`/`floor`/`room`/`zone`/`hostname` fields (and the
  single-mode `--host` value used as a filename) are now validated
  before being used to build an output path: path separators, `.`/`..`,
  and control characters are rejected, and the final path is confirmed
  to resolve inside the requested output directory.
- Batch CSV `port` column now validated as a base-10 integer in
  1-65535, matching `--port`'s own validation; previously accepted an
  out-of-range value or trailing non-numeric characters silently.
- `CSVTemplate` (shared `osiris/network/cisco/run` layer) no longer
  substitutes the requesting producer's own name into its first example
  row generating the template via `nxos template --generate`
  previously produced two `nxos` rows (one substituted, one hardcoded)
  and no `apic` row at all, which was also confusing on its own terms
  (running `iosxe template --generate` and seeing unrelated apic/nxos
  rows). Fixed by giving each producer exactly one, correctly-labeled
  example row in its own context (`nxos template --generate` shows only
  an NX-OS spine switch example); addresses stay in the RFC 5737
  documentation block (`192.0.2.0/24`).
- The generated `--secrets-file` skeleton was indistinguishable
  from a plain, undocumented JSON file a user had no way to discover
  the "rules" shape (per-host/CIDR credentials, see the `[Unreleased]`
  entry above) without reading source or `--help`. Fixed with two
  changes: each shape is now generated as its own file
  (`cisco-nxos-secrets.json` for the flat shape,
  `cisco-nxos-secrets-multihost.json` for the rules shape) rather than
  one file trying to document both; and each file carries a `"$comment"`
  field (the JSON Schema convention for an annotation with no effect on
  parsing, see
  https://json-schema.org/understanding-json-schema/reference/comments)
  describing just its own shape. `LoadCredentialFile` (via
  `encoding/json`'s default struct decoding) silently ignores this
  extra key, so both files still load exactly as if it were absent.
- `cisco-nxos-secrets-multihost.json`'s example rules used distinct
  per-rule usernames/passwords (`dc1-admin`/`dc1-pass`,
  `dc2-admin`/`dc2-pass`), inconsistent with the flat template's plain
  `user`/`changeme` placeholders. Every credential field in both
  generated files is now the same `user`/`changeme` pair the `hosts`
  pattern is what distinguishes one example rule from another, not an
  invented per-site credential name.
- Batch mode (`--source`) wrote output files as bare `<hostname>.json`,
  unlike single mode's `cisco-nxos-<timestamp>-<hostname>.json`. Found
  during live production testing: a repeated batch run against the same
  targets silently overwrote the previous run's output, with no way to
  tell two runs apart by filename alone. `run.OutputPath` now takes the
  run's timestamp and builds
  `<location-hierarchy>/cisco-<type>-<timestamp>-<hostname>.json` in
  batch mode too, matching single mode exactly (only the location
  hierarchy prefix differs between the two modes).
- **Critical**: `--secrets-file`'s `ResolveForHost` used `len(Rules) == 0`
  to detect the flat shape which also matched a rules-shape file
  containing only `{"default": {...}}` with no `"rules"` array at all
  (a batch that shares one login across every target, authored using
  the rules wrapper instead of the flat shape's bare fields a valid
  and, in production, common configuration). Every target in that
  batch silently resolved to the empty top-level `Username`/`Password`
  instead of `Default`, so every device in a real 4-target production
  batch authenticated with an empty password. `ResolveForHost` now
  treats the file as using the rules shape whenever `Default` is set
  *or* `Rules` is non-empty (either alone is enough), not only when
  `Rules` is non-empty.
- `ShowMulti` all-or-nothing handling of a multi-command NX-API batch
  meant a single command being rejected by the device CLI (e.g. `show
  lldp neighbors detail` on a device with LLDP disabled) failed the
  entire batch, and the caller then discarded every other command's
  data in that same batch including ones that had already succeeded
  (vPC, port-channel summary). `ShowMulti` now returns one `ShowResult`
  per command (`{Command, Body, Err}`); a command-level failure only
  sets that command's own `Err` and never affects its siblings. The
  function-level error `ShowMulti` still returns is now reserved for
  genuine transport-level failures (device unreachable, malformed
  envelope, a result count that does not match the number of commands
  sent). A non-empty response body that fails to decode as an object is
  likewise now that command's own `Err` rather than being silently
  swallowed into an empty, successful-looking map indistinguishable
  from the device genuinely reporting nothing. `Show` (the
  single-command helper `Login` uses) keeps its existing
  fail-the-caller-outright behavior, since a single command has no
  sibling to isolate it from.
- `Login`'s own `show version` call (used to validate credentials) is no
  longer followed by a second, redundant `show version` request in the
  main collection batch the body captured during `Login` is reused via
  the new `Client.VersionData`. Only applies when `Collect` performs its
  own `Login` call; a pre-authenticated `Client` injected directly (this
  package's own tests do this) still fetches it once, in the main batch.
- Four field-name/shape mismatches found by cross-checking a full raw
  `show ...` capture from a real testing against the typed DTOs (none
  previously caught by testing, since nothing checked the
  properties below against real JSON before this pass):
  `properties.version` on the switch resource was permanently empty
  `show version`'s decoded field was bound to `sys_ver_str`, a key
  that does not exist on real hardware. Now reads `nxos_ver_str`,
  falling back to `kickstart_ver_str` when absent.
  `properties.oper_status` on every interface resource was permanently
  empty bound to a `status` key that brief-mode `show interface
  brief` never reports at any verbosity (only one up/down field,
  `state`, already surfaced as `admin_status`). Removed rather than
  left silently dead.
  VLAN SVI interfaces (`Vlan<id>`) got no status at all
  `show interface brief` reports SVIs as `{interface, svi_admin_state}`,
  never `state`. `admin_status`/resource `status` now fall back to
  `svi_admin_state` when `state` is absent, fixing every SVI this
  session's OSPF connections (above) attach to.
  `extensions...power_supplies` was permanently empty `show environment`
  power-supply table is nested under `powersup.TABLE_psinfo.ROW_psinfo`,
  not top-level `TABLE_psinfo` as originally decoded.
  Temperature (`TABLE_tempinfo`, correctly top-level) was unaffected.

---

## [0.1.0] 2026-03-21

Initial Cisco NX-OS producer release. The `generatorVersion` constant
has remained at `0.1.0` through later module tags; in-place behavioral
changes are listed below with their module-tag context.

### Added
- NX-API CLI transport.
- Device, interfaces, VLANs, VRFs, vPC, LLDP collection.
- Shared runtime layer (CLI flags, batch CSV, TLS, interactive password
  prompt) with the rest of the Cisco producer family.

### Fixed
- Wired NX-OS producer factory into the CLI dispatcher was returning
  `not yet implemented` despite full implementation being present since
  the initial release.

### Changed
- Resource types renamed to align with [OSIRIS JSON spec taxonomy](https://github.com/osirisjson/osiris/blob/main/specification/v1.0/OSIRIS-JSON-v1.0.md#7-resource-type-taxonomy):
  `osiris.<vendor>.*`, `osiris.cisco.switch.spine`,
  `osiris.cisco.switch.leaf`, `osiris.cisco.interface.lag`.
- Connection type `network.link` renamed to `physical.ethernet`.
- Output filename convention changed to
  `cisco-nxos-<timestamp>-<hostname>.json`.

[Unreleased]: ../../../../CHANGELOG.md
[0.2.0]: ../../../../CHANGELOG.md#070---2026-08-29
[0.1.0]: ../../../../CHANGELOG.md#010---2026-03-21
