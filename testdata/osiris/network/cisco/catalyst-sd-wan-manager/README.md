# OSIRIS JSON Cisco Catalyst SD-WAN Manager producer fixtures

Synthetic SD-WAN Manager `/dataservice` REST response bodies and the
OSIRIS JSON documents the Cisco Catalyst SD-WAN Manager producer
(`osiris/network/cisco/catalyst-sd-wan-manager`) is expected to emit
from them.
`TestGolden` in `golden_test.go` logs in to an in-memory HTTPS SD-WAN
Manager that serves these bodies, runs the full export and compares the
emitted per-site document byte for byte with `purpose/golden_<purpose>.json`.

## API names from before the Catalyst SD-WAN Manager rebranding

The SD-WAN Manager REST API still uses the component names from before
the Catalyst SD-WAN Manager rebranding, in the releases this producer
targets API release (20.18, 26.1 and 26.2). The fixtures reproduce them
exactly because the tests must decode what the API actually sends:

| API name | Kind | Meaning |
|---|---|---|
| `vmanage` | `personality`, `device-type`, `device-model` value | Catalyst SD-WAN Manager |
| `vsmart` | `personality`, `device-type`, `device-model` value | Catalyst SD-WAN Controller |
| `vbond` | `personality`, `device-type` value | Catalyst SD-WAN Validator |
| `vedge`, `cedge` | `personality` value | WAN edge |
| `connectedVManages` | `GET /dataservice/device` field | Managers the device is connected to |
| `num-vmanages`, `vmanage-connection-preference` | `GET /dataservice/device/control/waninterface` field | Manager connection count and preference (not modeled) |

Names the OSIRIS JSON Producer chooses itself use the current naming:
provider `cisco.catalystsdwanmanager`, extension namespace
`osiris.cisco.catalystsdwanmanager` and extension keys such as
`connected_manager` (from `connectedVManages`). The personality and
device-model values pass through unchanged into `provider.type` and
`properties.model`, so the expected documents carry them too.

## Layout

```text
<scenario>
  api/<path>.json               # response body of GET /dataservice/<path>
  purpose/
    <site>_golden_documentation.json   # expected default OSIRIS JSON document
    <site>_golden_audit.json           # expected OSIRIS JSON document using --purpose audit flag
```

`<path>` is the endpoint path after `/dataservice/`. A `deviceId` or
`state` query parameter becomes the last path segment:

| Request | Fixture |
|---|---|
| `GET /dataservice/device` | `api/device.json` |
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
other byte is the OSIRIS JSON producer own output.

Besides the byte-for-byte comparison:

- `TestGolden_PurposeContract` requires that every documentation
  document carries only the fields of the producer's documentation
  allowlist, that no audit-only field (`serial_number`,
  `certificate_validity`, `ip_addresses`, `mac_address`, `system_ip`,
  `geo_location`, `device_groups`, `up_since`) appears in one, that
  each of them appears in at least one audit document, and that both
  purposes hold the same graph.
- `TestGolden_GraphIntegrity` requires that every connection endpoint
  and every group member is an emitted resource.

Unit cases for single response shapes (bare arrays, numeric `speed-mbps`
failed endpoints, raw bodies) are inline in the `_test.go` file that
uses them.

Validate the expected documents with the canonical CLI before
committing a change to them:

```bash
./scripts/validate_golden.sh testdata/osiris/network/cisco/catalyst-sd-wan-manager
```

## Scenarios

Each scenario was tested against the matching platform in the
[Cisco DevNet Sandbox](https://developer.cisco.com/sandbox).
Only the structure was taken from the sandbox. Every value is synthetic,
as described below.
`lab_sdwan` follows the structure observed on the sandbox, 
`lab_cross_site` is built from the documented response shapes of the
same endpoints.

Addresses are RFC 5737 (`192.0.2.0/24`, `198.51.100.0/24`,
`203.0.113.0/24`) and RFC 3849 (`2001:db8::/32`), MAC addresses are
RFC 9542 documentation unicast (`00:00:5E:00:53:xx`) or locally
administered (`02:00:5E:00:53:xx`), device names use the `LAB-` prefix,
serial numbers use the `TST` prefix, device UUIDs are sequential
synthetic values (`00000000-0000-4000-8000-000000000NNN`) or
`<model>-<TST serial>` strings, site names are `MXP`, `BRANCH-00` and
`OFFICE-00` and the coordinates are the approved example location.

### Cisco Catalyst SD-WAN Manager lab_sdwan

**Shape:** a Catalyst SD-WAN control-plane site, API release 20.18 (see
`version` in `api/device.json`), observed on the DevNet Sandbox SD-WAN
Manager Always On environment.

**Contains:** site-id `100` (`MXP`) with three controllers, every one
`reachable` and `normal`:

| Device | Personality | Model | System IP | Interfaces |
|---|---|---|---|---|
| `LAB-MANAGER01` | `vmanage` | `vmanage` | `192.0.2.1` | `docker0`, `eth0` (VPN 512), `eth1` (transport), `eth2`, `eth3`, `system` |
| `LAB-CONTROLLER01` | `vsmart` | `vsmart` | `192.0.2.101` | `eth0` (service), `eth1` (transport), `eth2`, `eth3`, `system` |
| `LAB-VALIDATOR01` | `vbond` | `vedge-cloud` | `192.0.2.201` | `eth0` (service), `ge0/0` (transport), `system` |

The emitted document holds 3 `osiris.cisco.controller` resources, 14
`network.interface` resources, 14 `contains` connections and one
`logical.site` group listing those 17 resources, with the site's
coordinates at `--purpose audit`.

Response details that exercise specific transform paths:

- `latitude` is a string and `longitude` a number, as in the SD-WAN
  Manager OpenAPI example for `GET /dataservice/device`.
- Interfaces without an address report `ip-address` as `-`.
- `LAB-MANAGER01` `eth0` is listed twice, IPv6 first; both rows are
  reconciled into one interface, with the IPv4 row's fields.
- The Manager and Controller transport interfaces have a WAN interface
  entry with color `default` the Validator has none, so its `ge0/0`
  carries no color.
- Some interfaces are administratively `Down` but operationally `Up`,
  as the sandbox reports for the controllers service and bridge
  interfaces.

**Deliberately omits:**

- WAN edges (`vedge`, `cedge`), and with them `network.router` and
  `network.router.port` resources, OMP links and peers and SD-WAN
  tunnel connections. The sandbox control-plane site has none, the
  OMP and site topology monitor endpoints are answered empty.
- Tenants: the producer does not query them, so
  `metadata.scope.accounts` is never populated.
- `private_ip` addresses: the producer classifies an interface address
  as public only when it is globally routable, and every example
  address here is RFC 5737 (which is globally routable in form), so
  all interface addresses are emitted as `public_ip`.
- `--include-raw-body` output.

### Cisco Catalyst SD-WAN Manager lab_cross_site

**Shape:** two WAN edge sites and a controller site, modeled on the
field shapes of a cEdge (Catalyst 8000V, IOS-XE 17.15) fabric managed
by SD-WAN Manager 20.18. Each site is exported as its own document
(`purpose/<site>_golden_<purpose>.json`).

**Contains:**

| Site | Device | Personality | System IP | Interfaces |
|---|---|---|---|---|
| `1000` `MXP` | `LAB-CONTROLLER01` | `vsmart` | `192.0.2.1` | `eth0` (transport, color `default`), `system` |
| `100` `BRANCH-00` | `LAB-RTR01` | `vedge` (cEdge) | `192.0.2.11` | `GigabitEthernet1` (transport, `biz-internet`), `GigabitEthernet2` (VPN 10), `Loopback1` (VPN 0 and VPN 10) |
| `200` `OFFICE-00` | `LAB-RTR02` | `vedge` (cEdge) | `192.0.2.12` | `GigabitEthernet1` (transport, `biz-internet`), `GigabitEthernet3` (VPN 10) |

Both edges hold an OMP session with the controller and a
`biz-internet` tunnel with each other. Every document keeps the
relationships it takes part in by carrying the remote endpoints as
neighbor resources (`site_role: "neighbor"`, never site group
members): the BRANCH-00 and OFFICE-00 documents both hold the
cross-site tunnel with the same connection ID and endpoints, and each
holds its edge's OMP session with the controller. An OSIRIS JSON
Consumer merging the three documents by ID obtains one end-to-end graph.

Response details that exercise specific transform paths:

- `LAB-RTR01` `GigabitEthernet1` has its `description` only on its
  IPv6 row, it is kept on the reconciled interface.
- `LAB-RTR01` `Loopback1` exists in VPN 0 and VPN 10: two interfaces,
  the VPN 10 one with an `@vpn10` ID suffix.
- `LAB-RTR02` reports its controller session `down` while the global
  OMP link list and the controller report it `up`: the connection has
  no single state and lists `observed_states` `["down", "up"]`
  identically in every document.
- `LAB-RTR02` lists an OMP peer `192.0.2.99` that is not in the
  inventory: it is not invented and `graph.unresolved_omp` counts it.
- The controller's `lastupdated` is a numeric string (the published
  schema's type) the edges an integer (the published example's).
- The two ends report different tunnel quality scores (`9` and `0`);
  each document keeps the score its own site reported and a `0`
  score is kept.

**Deliberately omits:** tenants, `--include-raw-body` output, and
devices without a native identity.

### Cisco Catalyst SD-WAN Manager contract_20_18 contract_26_1 contract_26_2

**Shape:** one scenario per documented API SD-WAN Manager release. Every
response body carries exactly the keys of that release's documented
example response for the endpoint (the official OpenAPI specification
of the release) with synthetic values. The documented response
contracts of the endpoints this producer calls are the same in the
three releases except for `GET /dataservice/device/interface`, whose
26.2 example adds `fec-mode`; the three scenarios differ only there.

**Contains:** the same fabric as `lab_cross_site` (a controller site
`1000` `MXP` and edge sites `100` `BRANCH-00` and `200` `OFFICE-00`
joined by a `biz-internet` tunnel, both edges peering with the
controller), plus the documented fields this producer does not model
(counters, preferences, `if-speed`, `ipv6-address`, and so on). The
inventory mixes the example's typing (the controller, integer
timestamps and numeric longitude) with the published schema's (the
edges, string timestamps and string longitude).

`TestContract_EveryReleaseYieldsTheSameCompleteGraph` exports every
site of every release at both purposes and requires complete
coverage, no unresolved endpoint, the cross-site tunnel and the OMP
sessions in both edge documents, and the same graph for all three
releases. Only the `BRANCH-00` document of each release is kept as a
golden (`purpose/branch_00_golden_<purpose>.json`) the three are
identical.

**Does not establish:** behavior of a deployed Manager of these
releases. The scenarios prove the documented contracts decode into the
expected graph, documents still carry `api_profile: "unverified"`
because the producer does not discover the Manager release.
