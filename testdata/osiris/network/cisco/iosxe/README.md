# OSIRIS JSON Cisco IOS-XE producer fixtures

Synthetic NETCONF replies and the OSIRIS JSON documents the Cisco
IOS-XE producer (`osiris/network/cisco/iosxe`) is expected to emit from
them. `TestGolden` in `golden_test.go` serves the replies through an
in-memory NETCONF session (base:1.1 hello, chunked framing, message-id
echo) and compares the emitted document byte for byte with
`purpose/golden_<purpose>.json`.

## Layout

```text
<model>                         # represent the platform model (`c8000v`, `C9300_24T`)
  netconf/<operation>.xml       # <data> payload for one discovery operation
  purpose/
    golden_documentation.json   # expected document using --purpose documentation flag
    golden_audit.json           # expected document using --purpose audit flag,
                                  with --include-raw-body
```

The minimal per-case replies the transform unit tests decode are inline
in the `_test.go` file that uses them, as are NETCONF and RESTCONF
protocol cases (hello, framing, `rpc-error` envelopes, malformed frames)
and fuzz seeds.

`<operation>` is the operation name the producer records in its
discovery coverage (`native-system`, `interfaces`, `interfaces-oper`,
`hardware-inventory`, `boot-config`, `native-interfaces`,
`lldp-neighbors`, `cdp-neighbors`, `vrf-definitions`, `vlans`,
`static-routes`, `transceivers`, and at audit purpose `bgp`, `ospf`,
`aaa`, `local-accounts`, `management-services`, `dns`, `snmp`,
`logging`, `ntp`, `acls`, `crypto`, `control-plane`, `certificates`).
An operation whose fixture holds only its header comment, or that has
no fixture file at all, models a device with no instances of that
model. `TestGolden_RESTCONF` serves the same files over RESTCONF and
requires byte-identical documents.

Every credential leaf (server keys, account secrets and passwords,
SNMP community strings, NTP keys, line and trustpoint passwords, IKE
pre-shared keys) holds the placeholder `changeme`. `TestGolden` fails
if that value reaches an expected document, and `TestSecretCanary`
replaces it with a random value at run time and checks the document
and the log stream at both purposes, with and without
`--include-raw-body`, over both transports.

Besides the byte-for-byte comparison, `TestGolden` checks the rules
the schema cannot express: every connection endpoint and group member
resolves, every owned port is contained by exactly one device, and no
physical link is emitted twice.

## Scenarios

Each scenario was tested against the matching platform in the
[Cisco DevNet Sandbox](https://developer.cisco.com/sandbox).
Only the structure was taken from the sandbox. Every value is
synthetic, as described below.

### Cisco Catalyst 8000V Edge Software (c8000v)

- Source class: synthetic, hand-authored. No value comes from a real
  device. The same holds for every scenario below.
- Shape: element structure follows the YANG models each producer filter
  queries, as IOS-XE 17.15 on a Catalyst 8000V returns them, including
  containers the producer does not model (`entity-information`,
  `reload-history`, `diffserv-target-entry`, CPU process lists,
  interface statistics and protocol counters, `storm-control`, empty
  `ietf-ip` containers), so the test also proves unmodeled content is
  ignored.
- Device: one router, `LAB-RTR01`, chassis serial `TST0000RTR01`, five
  interfaces (three physical, one 802.1Q subinterface, one loopback;
  one administratively down) and two VRFs, each bound to one
  interface. Neighbors: `LAB-SW01` reported by both LLDP (short name,
  abbreviated port) and CDP (FQDN, full port), so the two must merge
  into one link, plus an LLDP-only out-of-band neighbor. Static routes
  cover a default route, a Null0 discard route with metric and name, a
  per-VRF route through an interface next hop, and an IPv6 default
  route. At audit, one established dual-stack BGP session, one VRF
  BGP session that is not established, and one full OSPF adjacency.
  No `boot system` is configured, as on a Catalyst 8000V booting its
  default package set, and the VLAN and transceiver models report no
  instances.
- Security posture (audit purpose): AAA new-model with login, exec and
  command method lists over a TACACS+ group, TACACS+ and RADIUS servers
  and a RADIUS private server; two local accounts (type 9 secret, type
  7 password) and a type 9 enable secret; HTTPS only, SSH v2, SCP, two
  VTY ranges (one SSH-only behind an access class); DNS with a VRF
  name server; SNMP with two communities, a v3 group and two
  notification hosts; syslog to IPv4, VRF/TCP and IPv6 hosts; NTP with
  source verification, a trusted key and a VRF server; IPv4 standard
  and extended and IPv6 access lists, bound to `GigabitEthernet2`, the
  VTY lines, SNMP and NTP; a self-signed and an enrollment-URL
  trustpoint, an IKEv2 proposal (plus a keyring that must never be
  emitted) and an IPsec transform set; a control-plane policy; and four
  installed certificates, two of them the device-identity chain rooted
  at `Cisco Basic Assurance Root CA 2099`, whose DN contains the word
  `Basic` and must pass fail-closed secret scanning.
- Values: IPv4 from RFC 5737, IPv6 from RFC 3849, MAC addresses from
  RFC 9542 section 2.1.4, names under RFC 2606 or with a `LAB-` prefix,
  serials with a `TST` prefix, AS numbers from RFC 6996. See the data
  rules in [`testdata/README.md`](../../../../README.md).
- Deliberate omissions: fixtures are NETCONF XML only; CLI `show`
  output is never a decoder input.

### Cisco Catalyst 9300 (C9300_24T)

- Source class: synthetic, hand-authored, shaped after the same YANG
  models for a Catalyst 9300 switch stack on IOS-XE 17.15.
- Device: one switch, `LAB-SW01`, two stack members reported as two
  chassis rows in reverse index order (the identity is the
  lowest-index member, `TST0000SW01A`), a `Catalyst L3 Switch Software`
  banner (classified `network.switch`), a management port in
  `Mgmt-vrf`, two access ports, one trunk uplink (allowed list
  `10,20-30`, native VLAN 99), and two SVIs. Four VLANs, one
  suspended; membership is listed through both the `ports` and
  `vlan-interfaces` lists and the switchport configuration, so the
  trunk joins VLANs 10, 20 and 30 but not 1.
  Neighbors: `LAB-DIST01` reported by LLDP and
  CDP (merged), an LLDP neighbor identified only by its chassis MAC,
  and a CDP neighbor on a port the device does not report (dropped).
  One static default route in `Mgmt-vrf`. Inventory: two stack chassis
  rows, a power entry module, a fan tray and a transceiver.
  Transceivers, named by position as IOS-XE reports them
  (`Transceiver<slot>/<subslot>/<port>`): one present at `1/1/1`
  (attached to `TenGigabitEthernet1/1/1`), one not present (ignored),
  and one present at a position no reported port has (listed in the
  device extension).
- Deliberate omissions: no BGP, OSPF, boot or security posture fixture
  files; those operations are answered with an empty reply.

### Cisco 4451-X Integrated Services Router (isr4451)

- Source class: synthetic, hand-authored, shaped after the same YANG
  models for an ISR 4451 router on IOS-XE 16.9. Besides its expected
  documents, this scenario also answers the `Collect` tests in
  `iosxe_test.go`, which assert on coverage and failure handling.
- Device: one router, `LAB-RTR01`, chassis serial `TST0000XE01`, with a
  NIM module and a transceiver in its inventory, five interfaces (two
  physical, one 802.1Q subinterface, one loopback, one port-channel),
  two VRFs (`CORP`, `MGMT`), one CDP neighbor (`LAB-SW01`), an ordered
  `boot system` list, and at audit one established BGP session and one
  full OSPF adjacency.
- Deliberate omissions: no LLDP, native interface, VLAN, static route,
  transceiver-state or security posture fixture files; those operations
  are answered with an empty reply.

## Updating

The expected documents are the producer's output contract. When a change
alters emitted output on purpose, regenerate them and review the diff:

```bash
OSIRIS_UPDATE_GOLDEN=1 go test ./osiris/network/cisco/iosxe/ -run TestGolden
./scripts/validate_golden.sh testdata/osiris/network/cisco/iosxe
```
