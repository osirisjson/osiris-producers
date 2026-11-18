# OSIRIS JSON Cisco NX-OS producer fixtures

Synthetic NX-API command bodies and the OSIRIS JSON documents the Cisco
NX-OS producer (`osiris/network/cisco/nxos`) is expected to emit from
them. `TestGolden` in `golden_test.go` logs in to an in-memory NX-API
that answers `cli_show` requests on `/ins` with these bodies, runs
`Collect` and compares the emitted document byte for byte with
`purpose/golden_<purpose>.json`.

## Layout

```text
<scenario>
  nxapi/<command>.json          # NX-API "body" of one show command
  failures.json                 # commands the device rejects: command -> NX-API code and msg
  purpose/
    golden_documentation.json   # expected default OSIRIS JSON document
    golden_audit.json           # expected OSIRIS JSON document using --purpose audit flag
```

`<command>` is the show command in snake_case, with spaces and hyphens
as `_`: `show interface brief` is `nxapi/show_interface_brief.json`,
`show ip interface brief vrf all` is
`nxapi/show_ip_interface_brief_vrf_all.json`. A file holding `""` is a
command that succeeded with nothing to report, which NX-API returns as
an empty string body.

The server answers a single-command request with one output object and
a multi-command request with an output array, as NX-API does and a
`failures.json` entry with its code and message instead of a body. The
device is collected from its address with no hostname configured, as
in a single-host run, so the producer derives the VRF group identity
from the address.

Besides the byte-for-byte comparison:

- `TestGolden_FixturesMatchCommands` requires that every command
  `Collect` issues has exactly one fixture file or failure entry and
  that no fixture is left unused.
- `TestGolden_PurposeContract` requires that the documentation
  document carries none of the audit-only data (inventory serials,
  interface detail, the AAA resource, BGP) and that the audit document
  carries all of it.
- `TestGolden_GraphIntegrity` requires that every connection endpoint
  and every group member is an emitted resource.

Unit cases for single response shapes (single-row objects, numeric
strings, undecodable bodies, transport failures, raw bodies) are inline
in the `_test.go` file that uses them.

Validate the expected documents with the canonical CLI before committing
a change to them:

```bash
./scripts/validate_golden.sh testdata/osiris/network/cisco/nxos
```

## Scenarios

Addresses are RFC 5737 (`192.0.2.0/24`, `198.51.100.0/24`,
`203.0.113.0/24`), MAC addresses are locally administered
(`02:00:5E:10:xx:xx`) and the host MACs named in port descriptions are
RFC 9542 documentation unicast (`00:00:5E:00:53:xx`) or locally
administered, the device and every host named in a port description use
the `LAB-` prefix, serial numbers use the `TST` prefix, AS numbers are
from the RFC 5398 documentation range and the site named in descriptions
is `MXP`.

### Cisco Nexus 9000 c9300v virtual model

**Shape:** a virtual Nexus 9300v (`N9K-C9300v`, supervisor `N9K-vSUP`,
one 64-port `N9K-X9364v` line card), NX-OS 10.6(4), observed on 
2026-11-18.

**Contains:** `LAB-N9K01`, collected from `203.0.113.200`:

| Area | Content |
|---|---|
| Ports | `mgmt0` (VRF `management`) and `Ethernet1/1` to `Ethernet1/64`: trunks `Ethernet1/1`, `1/4`, `1/5` (VLANs 101 to 120), access ports `Ethernet1/6` to `1/25` (one per VLAN 101 to 120), routed ports `Ethernet1/2`, `1/3` (addressed, eBGP and OSPF peer descriptions) and `Ethernet1/26` to `1/64` |
| Interfaces | SVIs `Vlan1` and `Vlan101` to `Vlan120`, loopbacks `loopback0` and `loopback10` to `loopback19` |
| VLANs | `1` (`default`) and `101` to `120` (`LAB-*` names) |
| VRFs | `default`, `management`, the platform VRF `egress-loadbalance-resolution-` and ten `VRF-*` VRFs with two SVIs and one loopback each |
| Routing | one eBGP neighbor `203.0.113.6` (AS 64497, `Idle`) in VRF `default` |
| AAA | local login and accounting methods, the `radius` server group, RADIUS globals with no servers |
| Inventory | chassis, line card and supervisor, two modules |

The emitted document holds 119 resources, 577 connections (65
`contains.physical`, 32 `contains.logical`, 480 `network.l2`) and 13
`logical.vrf` groups at documentation purpose; audit purpose adds the
AAA resource, the BGP neighbor and their connections and the interface
detail.

Response details that exercise specific transform paths:

- `show vpc brief`, `show vpc peer-keepalive` (vPC not enabled) and
  `show tacacs-server` (TACACS+ not enabled) are rejected with code
  `400`, as on the sandbox and appear as `failed` in the device's
  `coverage`.
- LLDP, CDP, port-channel, OSPF neighbor and environment commands
  succeed with an empty body.
- `show vrf all detail` lists no member interfaces, so membership
  comes from `show vrf interface`.
- Single-row tables (BGP VRF, address family and neighbor, AAA
  accounting and groups) are returned as an object, not an array.
- `show interface switchport` reports the default `trunk_vlans`
  `1-4094` for access ports, as NX-OS does, so every access port is
  also wired to every VLAN: 3 trunks x 20 VLANs plus 20 access ports x
  21 VLANs make the 480 `network.l2` connections.
- Routed ports share one MAC address; L2 ports each have their own.
- SVIs report `svi_*` detail fields, so they carry no MTU, bandwidth or
  MAC address.
- Ports report speed `auto`, so only `mgmt0` carries `speed_mbps`.

**Deliberately omits:**

- vPC, port-channels, LLDP and CDP neighbors, OSPF adjacencies and
  transceivers (every port reports `not present`), none of which the
  sandbox device has.
- Interface counters and environment readings.
- `--include-raw-body` output.
