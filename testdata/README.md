# OSIRIS JSON Producer test fixtures guidelines

Small, reviewed, synthetic fixtures for every OSIRIS JSON producer under
the `osiris/` directory: the vendor-native inputs a producer consumes
and the deterministic OSIRIS JSON documents it is expected to emit
from them.

> [!NOTE]
> **Every fixture is hand-authored** with neutral placeholder data, none
> of them represent a capture from a real infrastructure environment.
> Real device or controller captures may guide a fixture's shape during
> development, but MUST NOT enter this tree or any other path in the
> repository.

Every value in every fixture MUST follow the data rules below.

## OSIRIS JSON Producer approved test data by type

**IPv4 addresses**
- RFC 5737 documentation blocks:
  `192.0.2.0/24`, `198.51.100.0/24`, `203.0.113.0/24`
- MUST NOT use any IPv4 address outside these ranges in test fixtures or
  documentation, any leftover MUST remediate and fix.

**IPv6 addresses**
- RFC 3849 documentation prefix only: `2001:DB8::/32`
  (e.g. `2001:db8::1`)
- MUST NOT use any IPv6 address outside these ranges in test fixtures or
  documentation, any leftover MUST remediate and fix.

**MAC addresses**
- Use the IANA-reserved documentation ranges per RFC 9542 section 2.1.4
  (RFC 9542 obsoletes RFC 7042 - cite 9542, not 7042):
  - Unicast: `00:00:5E:00:53:00` through `00:00:5E:00:53:FF`
  - Multicast: `01:00:5E:90:10:00` through `01:00:5E:90:10:FF`
- Alternatively use locally administered unicast MACs (second LSB of
  first octet = 1): `02:xx:xx:xx:xx:xx`
- Clearly fake sequential patterns are also acceptable in tests:
  `AA:BB:CC:DD:00:01`, `AA:BB:CC:DD:00:02`
- MUST NOT use a real vendor OUI (e.g. Cisco `00:1A:A1:xx`, Arista
  `44:4C:A8:xx`) in test data, if any leftover remediate and fix.

**Hostnames and device names**
- Use RFC 2606 reserved names for DNS: `example.com`, `example.net`,
  `example.org`
- Use clearly invented lab prefixes for device names: `LAB-`, `TEST-`,
  `FAKE-`, `STUB-`
- Examples: `LAB-RTR01`, `TEST-SW01`, `FAKE-SPINE01`, `FAKE-LEAF01`

**Email address**
- Using RFC 2606 use this email address as example:
  `osirisjson@example.com`, `test@example.net`, `user@example.org`
- MUST NOT refer to a real person, a real team or an organization.
  (e.g.: `test@example.com`, `lab@example.net`) more email address
  can be created as example always using RFC 2606 domain names.

**Geographical coordinates**
- latitude: `45.5433411`
- longitude: `10.2241237`

**Site or location names**
- Airport IATA codes like `MXP` or invented site names like `BRANCH-00`,
  `OFFICE-00` or `FACTORY-00`

**AS numbers**
- RFC 5398 documentation range: `64496-64511`
- RFC 6996 private/test range: `64512-65534` and `4200000000-4294967294`

**Serial numbers and asset tags**
- Always prefix with `TST`: `TST0000001`, `TST-CHASSIS-01`

**Credentials**
- Use obvious placeholders only: `admin`, `test`, `password`, `secret`,
  `changeme`
- MUST NOT use real-looking passwords or API tokens or secrets of
  any kind.

## Layout

Fixtures mirror the producer package tree under `osiris/`, using the
same category taxonomy as `osiris/`:

```text
osiris/<category>/<vendor>[/<product>]/
  README.md                         # provenance of this producer's fixtures
  <scenario>/
    <inputs>/...                    # vendor-native inputs, in the format
                                      the producer actually consumes
    purpose/golden_<purpose>.json   # expected OSIRIS JSON document per --purpose level
```

- `<category>` is the `../osiris/` category (e.g. `hyperscalers`).
- `<scenario>` names one coherent synthetic environment. When the
  environment spans several documents (one per device, site or
  subscription), each document gets its own subdirectory under
  `<scenario>/` with this same layout.
- `<inputs>` is named after the source format (for example `netconf/`
  for NETCONF XML replies, `api/` for REST JSON bodies). Inputs MUST be
  in the format the producer decodes; MUST NOT feed CLI or other
  human-readable output to a decoder that expects structured data.
- `<purpose>` is `documentation` or `audit`. Expected documents are
  always named `golden_<purpose>.json` under `purpose/`, which is the
  name `scripts/validate_golden.sh` discovers.

Each producer directory MUST carry its own `README.md` stating, per
scenario: that it is synthetic, what real shape it follows (platform,
software release, data models), what it contains, and what it
deliberately omits.

## Updating and validating

Expected documents are an OSIRIS JSON producer output contract.
When a change alters emitted output on purpose, regenerate them and
review the diff before committing:

```bash
OSIRIS_UPDATE_GOLDEN=1 go test ./osiris/<category>/<vendor>/... -run TestGolden

./scripts/validate_golden.sh testdata/osiris/<category>/<vendor>
```

MUST NOT commit a `golden_<purpose>.json` that has not passed
`scripts/validate_golden.sh` at the **strict** profile.

## Interoperability snapshots

Hand-authored, neutral snapshots (10 to 15 resources each) modeled on
the shape of osiris-producer output: native identifier styles, vendor
type namespaces, provider provenance fields, `contains` and `network*`
connections, all three edge directions, and nested groups.
The set is widened to one snapshot per active producer, plus a large
synthetic document for the scale budget, when the interoperability
suite is built.
