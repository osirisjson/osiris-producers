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

## [0.2.0] - 2026-11-12

### Security
- SSH host-key verification is now on by default: the NETCONF connection
  checks the device key against `~/.ssh/known_hosts` and aborts on an
  unknown or changed key, mirroring how the HTTPS members of the Cisco
  producer family verify TLS certificates. `--insecure` (already
  accepted) skips the check and now logs a prominent warning each run;
  previously the check was skipped unconditionally.
- Every NETCONF network phase is bounded by a fixed deadline (TCP dial
  15s; SSH handshake, each RPC, and close-session 30s). The prior read
  deadline was applied to a value that could never satisfy the required
  type, so no timeout took effect.
- A NETCONF reply that grows past the 10 MiB cap before its framing
  delimiter now fails with a classified size error instead of an opaque
  read failure.
- A reply stream that ends before its framing is complete now fails as
  a malformed frame; it was previously returned as a partial reply.
- Device `<rpc-error>` details are sanitized before they reach logs or
  errors: control characters are dropped, the message is capped at 200
  characters, and `error-path`/`error-info` are never surfaced. Errors
  no longer embed a fragment of the raw reply.
- `--include-raw-body` now requires `--purpose audit` (rejected
  otherwise) and attaches only operational replies, under
  `extensions["osiris.cisco"].raw_replies` keyed by operation. Every
  attached reply is redacted: the content of credential-named elements
  and any value matching a secret pattern is replaced with `REDACTED`,
  and a reply that is not well-formed XML is dropped. Replies are capped
  at 256 KiB each and 2 MiB in total. Configuration replies are never
  attached. Every withheld reply is listed in `raw_omitted` with its
  reason. The flag was previously accepted but had no effect.
- Output documents are written owner-only (0600) also when the file
  already exists; batch output directories are created 0700.

### Added
- `metadata.generator.url` now links the OSIRIS JSON Cisco producer
  documentation page.
- RESTCONF transport (RFC 8040) for devices that keep NETCONF disabled.
  `--transport auto|netconf|restconf` (default `auto`): `auto` uses
  NETCONF and falls back to RESTCONF only when the NETCONF port cannot
  be reached (timeout or refusal), never on an authentication or
  host-key failure. RESTCONF uses HTTPS (TLS 1.2 minimum, certificate
  verification unless `--insecure`, default port 443), sends
  credentials only in the Authorization header, does not follow
  redirects, and bounds each request (15s connect, 30s total, 10 MiB
  body). It requests the XML encoding, so both transports emit the same
  document. HTTP 204 is recorded as no instances, 404 as unsupported,
  401/403 as access denied.
- Device containment: a `contains.physical` connection from the device
  to every physical Ethernet port and `contains.logical` to every other
  interface.
- Interfaces are built from both the configuration model and
  `Cisco-IOS-XE-interfaces-oper`, adding `admin_status`, `oper_status`
  (RFC 2863 vocabulary: `up`, `down`, `lower-layer-down`, ...),
  `speed_mbps` (integer), `duplex`, `mtu`, and `mac_address`. Every
  configured IPv4 and IPv6 address is kept as `ipv4_addresses` and
  `ipv6_addresses` in CIDR form, deduplicated in configured order;
  `ip_address`/`subnet_mask` carry the first IPv4 address. Unspecified
  addresses and invalid masks are dropped.
- Configured `boot system` images are reported as
  `extensions["osiris.cisco"].boot_images`, in configured order.
- Discovery coverage. Every query is recorded as `succeeded`, `empty`
  (valid reply, no instances), `unsupported` (the device rejects the
  model), `failed` (with a fixed category such as `timeout`,
  `decode-error`, `access-denied`), or `skipped` (not run at the
  selected `--purpose`). The record is attached to the device resource
  as `extensions["osiris.cisco"].coverage`, and `metadata.scope` now
  sets `name`, `purpose`, and a one-line `description` naming the
  operations that returned data, had no instances, or are not
  represented.
- NETCONF 1.1 chunked framing (RFC 6242 section 4.2). The client now
  advertises `base:1.0` and `base:1.1` and switches to chunked framing
  after `<hello>` when the device supports it, falling back to
  end-of-message framing otherwise. Chunk headers are validated (size
  1-4294967295, no leading zero) and a chunk announcing more than the
  remaining 10 MiB budget is rejected before its payload is read. A
  device hello that cannot be parsed, or that advertises no supported
  base version, now aborts the connection instead of continuing with
  guessed framing.
- LLDP neighbors (`Cisco-IOS-XE-lldp-oper`), merged with CDP into one
  set of `physical.ethernet` links. A link whose local port carries a
  transceiver reporting an optical PMD (`...BASE-SR`, `-LR`, `-SX`,
  `-LX`, ...) is emitted as `physical.fiber` with that `ethernet_pmd`
  as evidence; neighbor protocols alone never set the medium. A link
  reported by both protocols is emitted once, matched by local port,
  remote host name (domain ignored) and remote port (IOS abbreviations
  expanded); connections and stubs list the reporting protocols in
  `discovered_via`. Stub IDs use the host name and full port name, so
  they do not depend on which protocol reported the link.
  A device that reports itself (a cable looped between two of its own
  ports: same host name, an emitted remote port, and a management
  address it owns when one is reported) is linked port to port,
  once, instead of to a stub.
- VLANs (`Cisco-IOS-XE-vlan-oper`) as `network.vlan` resources
  (`cisco.iosxe::<serial>/vlan/<id>`, `vlan_id`, `vlan_name`), with a
  `network.l2` connection from every assigned interface to its VLAN.
  Switchport configuration (`Cisco-IOS-XE-switch`) adds memberships:
  the access VLAN, and every VLAN a trunk allows (its allowed list with
  `add`/`remove`/`except` applied, or all VLANs when no list is
  configured). Switchports carry `port_mode` (`access`/`trunk`) and,
  on trunks with a configured native VLAN, `native_vlan`.
- 802.1Q subinterfaces carry `encapsulation: "dot1q"` and `vlan_id`,
  and SVIs carry `vlan_id`, from the native interface configuration.
- Connected subnets as `network.subnet` resources, one per VRF and
  prefix (`cisco.iosxe::<serial>/subnet/<vrf>/<cidr>`, `vrf` `default`
  for the global table), with a `network.l3` connection from each
  addressed interface and membership in the VRF group. Host prefixes
  and IPv6 link-local prefixes are not emitted.
- Configured static routes (`ip route`, `ipv6 route`, global and per
  VRF) as `extensions["osiris.cisco"].static_routes` on the device:
  `prefix`, `next_hop` and/or `interface`, plus `vrf`, `metric` and
  `name` when configured. The routing table is not collected.
- At `--purpose audit`, OSPF adjacencies as `network.ospf` connections
  from the local interface, and BGP sessions as `network.bgp`
  connections from the device, each to an `unknown`-provider stub
  (`<device-id>/ospf-neighbor/<router-id>`,
  `<device-id>/bgp-neighbor/<vrf>/<address>`). Connections carry the
  session state, area or address families, VRF, and description;
  status is `active` for `full`/`two-way` OSPF and `established` BGP.
- Discovery operations `native-interfaces`, `lldp-neighbors`, `vlans`
  and `static-routes` in the coverage record.
- At `--purpose audit`, the device security posture in the device's
  `extensions["osiris.cisco"]`: `aaa` (new-model, authentication,
  authorization and accounting method lists, TACACS+/RADIUS server
  groups and servers), `local_accounts` (name, privilege, credential
  storage type), `enable_mode`, `management_services` (HTTP/HTTPS, SSH,
  SCP, type 7 obfuscation, VTY transport, access classes, exec timeout,
  login method), `dns`, `snmp` (community permission, view and access
  list; groups; notification hosts; location; contact), `logging`,
  `ntp` (servers, peers, source verification, key numbers, access
  groups), `crypto` (PKI trustpoints, IKEv2 proposals, IPsec transform
  sets) and `control_plane_policy`. Credential material (server keys,
  account secrets and passwords, SNMP community strings, NTP key values,
  line passwords, IKE pre-shared keys) is never decoded or emitted.
- At `--purpose audit`, every IPv4 standard, IPv4 extended and IPv6
  access list as an `osiris.cisco.acl` resource
  (`cisco.iosxe::<serial>/acl/<ipv4|ipv6>/<name>`) with `acl_type`,
  `rule_count`, ordered `entries` (sequence, action, protocol, source,
  destination, ports, flags, remark) and `bindings` (interface access
  groups, IPv6 traffic filters, VTY access classes), and every installed
  PKI certificate as a `network.security.certificate` resource
  (`cisco.iosxe::<serial>/certificate/<trustpoint>/<serial>`) with
  trustpoint, serial, subject, issuer, validity, usage and key type.
  Both are contained by the device (`contains.logical`).
- Pluggable transceivers from `Cisco-IOS-XE-transceiver-oper` as the
  port's `properties.transceiver` (vendor, part_number, revision,
  identifier, connector, ethernet_pmd; serial_number at audit only).
  A transceiver named `Transceiver<slot>/<subslot>/<port>` is attached
  to the one port at that position; present transceivers matching no
  emitted port, or more than one, are listed in the device extension
  `transceivers`.
- Discovery operations `transceivers`, `aaa`, `local-accounts`,
  `management-services`, `dns`, `snmp`, `logging`, `ntp`, `acls`,
  `crypto`, `control-plane` and `certificates` in the coverage record;
  the audit-only ones are recorded `skipped` at documentation purpose.

### Fixed
- An unreachable NETCONF port now fails with a message naming the
  likely cause (netconf-yang disabled or the port blocked) instead of a
  bare dial timeout, and is detected before any host-key lookup.
- A reply whose `<data>` holds only comments is recorded as having no
  instances instead of failing to decode.
- Device replies are decoded into typed structs per YANG model with the
  root element checked against the model namespace; malformed XML, the
  wrong model, or trailing content now fail the query instead of being
  silently decoded as an empty result. A failure in the native system or
  interfaces query aborts the run; any other query failure leaves a
  partial document whose coverage states what is missing.
- NETCONF replies are decoded by XML namespace and element name instead
  of byte-substring search, so prefixed (`<nc:rpc-reply>`) and
  default-namespace replies are handled identically, and an
  `<rpc-error>` or `<data>` element outside the NETCONF namespace is no
  longer mistaken for one. A reply that is not a well-formed
  `<rpc-reply>`, or has no `<data>`, now fails instead of being passed
  through whole as if it were data.
- Each reply's `message-id` must match its request. A mismatched or
  missing id, or a transport failure mid-exchange, marks the session
  unusable: later queries fail immediately and `<close-session>` is
  skipped, rather than pairing replies with the wrong request.
- Device `<rpc-error>` replies are classified as unsupported
  (`operation-not-supported`, `unknown-element`, `unknown-namespace`,
  `unknown-attribute`), access denied, or failed; `error-severity`
  `warning` entries are logged and no longer fail the query.

### Changed
- **Breaking**: interface types follow the device role. On a router
  every interface is `network.router.port`; on a switch physical
  Ethernet ports are `network.switch.port` and other interfaces
  `network.interface`. Port-channels are `network.interface.lag` (was
  `osiris.cisco.interface.lag`). Interface `status` is derived from the
  operational state instead of always `unknown`. The `enabled`,
  `netmask`, and string `speed` properties are replaced by
  `admin_status`, `subnet_mask`, and `speed_mbps`. Async terminal lines
  are not emitted.
- **Breaking**: the hard-coded `boot_image` extension
  (`bootflash:packages.conf`) is removed; boot images are emitted only
  when configured.
- **Breaking**: VRF groups are typed `network.vrf` (a network-boundary
  group, as OSIRIS JSON lists VRFs) instead of `logical.vrf`; group IDs
  change with the type.
- **Breaking**: VRF group membership comes from the VRF each interface
  is operationally bound to, instead of interface references inside VRF
  definitions, which the native model does not carry. When the
  operational model does not report an interface, or the query fails,
  the configured `vrf forwarding` binds it instead.
- **Breaking**: device identity. `provider.name` and
  `metadata.scope.providers` are now `cisco.iosxe` (was `cisco`).
  Resource IDs are namespaced native IDs derived from the chassis
  serial number instead of hashes of the collection target:
  `cisco.iosxe::<serial>` for the device and
  `cisco.iosxe::<serial>/<interface>` for its interfaces, with
  `provider.native_id` matching; VRF group IDs are keyed by the serial
  too. Renaming the target or reaching the device by another address no
  longer changes any ID. When no chassis serial is reported, the target
  address is used and a warning is logged.
- **Breaking**: the device `name` and `metadata.scope.name` are the
  hostname the device reports, falling back to the target label, then
  the target address. Device properties follow the common network
  device set: `manufacturer`, `model`, `version`, `serial_number`
  (renamed from `serial`), and `management_ip` (the target address);
  `hostname` is removed, and `version` is the running release from the
  software banner (for example `17.15.4c`) rather than the configuration
  syntax version, omitted instead of `"unknown"` when unavailable.
- **Breaking**: CDP neighbor stubs have `provider.name` `unknown` and
  `provider.source` `cdp`, and IDs anchored under the device
  (`<device-id>/neighbor/<remote-host>/<remote-port>`); neither CDP nor
  LLDP establishes that the remote device runs IOS-XE.
- **Breaking**: at `--purpose audit`, BGP neighbors are `network.bgp`
  connections instead of the `bgp_neighbors` device extension, and the
  received-prefix counter is no longer emitted. `ospf_processes` keeps
  only `process_id` (integer), `router_id` (dotted quad) and
  `address_family`; its neighbors are `network.ospf` connections. The
  BGP and OSPF decoders now follow the YANG tree the device returns
  (neighbor `session-state`; neighbors under
  `ospf-area/ospf-interface`), so sessions that were previously
  dropped are now reported.
- The device type is `network.switch` when the software banner names a
  Catalyst switch image family (`... Switch Software`), and
  `network.router` otherwise, instead of always `network.router`.
- A failed hardware-inventory query now aborts the run, since the
  device identity depends on it; an empty or unsupported reply falls
  back as above.
- CLI only, no change to emitted documents (`generatorVersion` stays
  `0.1.0`): this producer now owns its own flag parsing, `Config` type,
  and single/batch runner (`flags.go`/`config.go`/`dispatch.go`),
  dispatched directly by `osiris/network/cisco/cisco.go` instead of the
  retired shared `run.ParseFlags`/`RunConfig`/`RunBatch`/`ProducerFactory`
  orchestrator. The `-p`/`--password` flag is gone (a CLI value leaks via
  `ps` and shell history); host/username/password resolve through
  `--secrets-file` (a permission-checked JSON file, flat or per-host/CIDR
  "rules" shape), then `OSIRISJSON_CISCO_IOSXE_PASSWORD`, then an
  interactive prompt. `--detail minimal|detailed` is replaced by the
  standard `--purpose documentation|audit` contract (`audit` selects the
  BGP/OSPF detail `detailed` did); `--include-raw-body` requires
  `--purpose audit`. `template --generate` moves under the
  subcommand (`iosxe template --generate`) and writes both
  `--secrets-file` shapes.

- The device's `inventory` extension is structured: each item carries
  `class` (chassis, cpu, dram, flash, interface-module, transceiver,
  fan-tray, power-entry-module, ...), `index`, `name`, `description`,
  `part_number`, `hw_version`, `field_replaceable` and `virtual`.
  Component serial numbers (`serial`) are emitted at audit purpose only.

### Removed
- CPU and memory utilization (`cpu_utilization_1min`,
  `cpu_utilization_5min`, `memory_used`, `memory_free`) and interface
  counters (`rx_bytes`, `tx_bytes`, `rx_errors`, `tx_errors`) are no
  longer emitted at audit purpose, and the `cpu` and `memory` discovery
  operations are no longer queried: they are volatile telemetry, not
  inventory or configuration state.

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
[0.2.0]: ../../../../CHANGELOG.md#0100---2026-11-12
[0.1.0]: ../../../../CHANGELOG.md#010---2026-03-21
