# OSIRIS JSON Cisco Catalyst SD-WAN Manager (vManage) producer fixtures

Synthetic vManage `/dataservice` REST response bodies and the OSIRIS
JSON documents the Cisco Catalyst SD-WAN Manager producer
(`osiris/network/cisco/vmanage`) is expected to emit from them.
`TestGolden` in `golden_test.go` logs in to an in-memory HTTPS vManage
that serves these bodies, runs the full export and compares the emitted
per-site document byte for byte with `purpose/golden_<purpose>.json`.

## Layout

```text
<scenario>
  api/<path>.json               # response body of GET /dataservice/<path>
  purpose/
    golden_documentation.json   # expected default OSIRIS JSON document
    golden_audit.json           # expected OSIRIS JSON document using --purpose audit flag
```

`<path>` is the endpoint path after `/dataservice/`. A `deviceId` or
`state` query parameter becomes the last path segment:

| Request | Fixture |
|---|---|
| `GET /dataservice/device` | `api/device.json` |
| `GET /dataservice/tenant` | `api/tenant.json` |
| `GET /dataservice/topology/device/site/{siteId}` | `api/topology/device/site/{siteId}.json` |
| `GET /dataservice/topology/monitor/site/{siteId}` | `api/topology/monitor/site/{siteId}.json` |
| `GET /dataservice/device/interface?deviceId={system-ip}` | `api/device/interface/{system-ip}.json` |
| `GET /dataservice/device/control/waninterface?deviceId={system-ip}` | `api/device/control/waninterface/{system-ip}.json` |
| `GET /dataservice/device/omp/peers?deviceId={system-ip}` | `api/device/omp/peers/{system-ip}.json` |
| `GET /dataservice/device/omp/links?state={state}` | `api/device/omp/links/{state}.json` |

An endpoint without a fixture file is answered with a valid empty list
(`{"data":[]}`). List bodies use the `{"data": [...]}` envelope.

The export stamps the wall clock into `metadata.timestamp` and the
controller address it dialed into `metadata.scope.clusters`. The test
checks that the dialed address is the only cluster and then pins both
fields (`2026-11-13T12:00:00Z`, `lab-manager01.example.com`), every
other byte is the producer's own output.

Besides the byte-for-byte comparison:

- `TestGolden_PurposeContract` requires that the documentation
  document carries no audit-only field (`serial_number`,
  `certificate_validity`) and that the audit document carries them.
- `TestGolden_GraphIntegrity` requires that every connection endpoint
  and every group member is an emitted resource.

Unit cases for single response shapes (bare arrays, numeric `speed-mbps`
failed endpoints, raw bodies) are inline in the `_test.go` file that
uses them.

Validate the expected documents with the canonical CLI before
committing a change to them:

```bash
./scripts/validate_golden.sh testdata/osiris/network/cisco/vmanage
```

## Scenarios

Each scenario was tested against the matching platform in the
[Cisco DevNet Sandbox](https://developer.cisco.com/sandbox).
Only the structure was taken from the sandbox. Every value is
synthetic, as described below.

Addresses are RFC 5737 (`192.0.2.0/24`, `198.51.100.0/24`,
`203.0.113.0/24`) and RFC 3849 (`2001:db8::/32`), MAC addresses are
RFC 9542 documentation unicast (`00:00:5E:00:53:xx`) or locally
administered (`02:00:5E:00:53:xx`), device names use the `LAB-` prefix,
serial numbers use the `TST` prefix, device UUIDs are sequential
synthetic values (`00000000-0000-4000-8000-000000000NNN`), the site name
is `MXP` and the coordinates are the approved example location.

### lab_sdwan

**Shape:** a Catalyst SD-WAN control-plane site, release 20.18 (see
`version` in `api/device.json`), observed on the DevNet Sandbox SD-WAN
Manager Always On environment.

**Contains:** site-id `100` (`MXP`) with three controllers, every one
`reachable` and `normal`:

| Device | Personality | Model | System IP | Interfaces |
|---|---|---|---|---|
| `LAB-MANAGER01` | `vmanage` | `vmanage` | `192.0.2.1` | `cbr-vmanage`, `eth0` (VPN 512), `eth1` (transport), `eth2`, `eth3`, `system` |
| `LAB-CONTROLLER01` | `vsmart` | `vsmart` | `192.0.2.101` | `eth0` (service), `eth1` (transport), `eth2`, `eth3`, `system` |
| `LAB-VALIDATOR01` | `vbond` | `vedge-cloud` | `192.0.2.201` | `eth0` (service), `ge0/0` (transport), `system` |

The emitted document holds 3 `osiris.cisco.controller` resources, 14
`network.interface` resources, 14 `contains` connections and one
`physical.room` group for the site with its coordinates.

Response details that exercise specific transform paths:

- `latitude` is a string and `longitude` a number, as in the vManage
  OpenAPI example for `GET /dataservice/device`.
- Interfaces without an address report `ip-address` as `-`.
- `LAB-MANAGER01` `eth0` is listed twice, IPv6 first, and the IPv4
  entry is the one emitted.
- The Manager and Controller transport interfaces have a WAN interface
  entry with color `default` the Validator has none, so its `ge0/0`
  carries no color.
- Some interfaces are administratively `Down` but operationally `Up`,
  as the sandbox reports for the controllers' service and bridge
  interfaces.

**Deliberately omits:**

- WAN edges (`vedge`, `cedge`), and with them `network.router` and
  `network.router.port` resources, OMP links and peers and SD-WAN
  tunnel connections. The sandbox control-plane site has none; the
  OMP, site topology monitor and tenant endpoints are answered empty.
- Tenants: `metadata.scope.accounts` is not populated.
- `private_ip` addresses: the producer classifies an interface address
  as private only when it is RFC 1918, and every example address here
  is RFC 5737, so all interface addresses are emitted as `public_ip`.
- `--include-raw-body` output.
