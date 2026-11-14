# OSIRIS JSON Cisco ACI/APIC producer fixtures

Synthetic APIC REST class responses and the OSIRIS JSON documents the
Cisco ACI/APIC producer (`osiris/network/cisco/apic`) is expected to
emit from them. `TestGolden` in `golden_test.go` serves the responses
through an in-memory APIC and compares the emitted document byte for
byte with `purpose/golden_<purpose>.json`.

## Layout

```text
<scenario>
  api/<class>.json              # class query response body (imdata envelope)
  failures.json                 # optional: class -> HTTP status the query fails with
  purpose/
    golden_documentation.json   # expected default OSIRIS JSON document
    golden_audit.json           # expected OSIRIS JSON document using --purpose audit flag,
                                  with optional --include-raw-body
```

`<class>` is the Cisco APIC class the producer queries and records in it
discovery coverage (`fabricNode`, `topSystem`, `firmwareRunning`,
`fvTenant`, `fvCtx`, `fvBD`, `fvSubnet`, `fvAEPg`, `l3extOut`,
`fvRsCtx`, `fvRsBd`, `l3extRsEctx`, `faultInst`, `fvAp`, `vzBrCP`,
`vzSubj`, `vzFilter`, `fvRsProv`, `fvRsCons`, `vzRsSubjFiltAtt`,
`l1PhysIf`, `ethpmPhysIf`, `fabricLink`, `lldpAdjEp`, `cdpAdjEp`,
`pcAggrIf`, `fvRsPathAtt` and at audit purpose `vzEntry`, `fvCEp`,
`fvIp`). A class without a fixture file is answered with a valid empty
`imdata`.

Besides the byte-for-byte comparison:

- `TestGolden_RawIsAdditive` removes the `--include-raw-body` keys
  (`raw`, `raw_unowned`, `raw_omitted`) from the audit document and
  requires the audit document without the flag.
- `TestGolden_OrderIndependent` serves every class with its elements in
  reverse order and requires the identical document.
- `TestGolden_GraphIntegrity` checks the rules the schema cannot
  express: every connection endpoint and group member is an emitted
  resource, every group child is an emitted group, no group is its own
  ancestor and documentation purpose carries no endpoint.

Unit cases for single response shapes (missing or null `imdata`,
error objects, foreign classes, missing `dn`, redirects, HTTP error
bodies, secret-shaped values) are inline in the `_test.go` file that
uses them.

Validate the expected documents with the canonical CLI before
committing a change to them:

```bash
./scripts/validate_golden.sh testdata/osiris/network/cisco/apic
```

## Scenarios

Each scenario was tested against the matching platform in the
[Cisco DevNet Sandbox](https://developer.cisco.com/sandbox).
Only the structure was taken from the sandbox. Every value is
synthetic, as described below.

Every value is synthetic and hand-authored. Addresses are RFC 5737
(`192.0.2.0/24`, `198.51.100.0/24`, `203.0.113.0/24`) and RFC 3849
(`2001:db8::/32`); MAC addresses are RFC 9542 documentation unicast
(`00:00:5E:00:53:xx`); device names use the `LAB-` prefix; serial
numbers use the `TST` prefix; the fabric domain is `MXP`.

Real shape followed: the APIC REST API class query response
(`GET /api/class/<class>.json`, `{"totalCount": "<n>", "imdata":
[{"<class>": {"attributes": {...}}}]}`), with the class names, DN
formats and attribute names of the Cisco APIC 6.0 management
information model. Only the attributes the producer reads are present;
every other attribute an APIC returns is omitted.

Deliberately omitted from both scenarios: multi-pod and multi-site
fabrics, remote leaves, FEX, vPC domain objects, more than one page per
class and every class the producer does not query (vzAny, taboo
contracts, endpoint security groups, external EPGs, service graphs,
subject terms, access policies, routing protocol state).

### Cisco ACI Complete fabric (lab_fabric)

- Fabric: one pod, fabric domain `MXP`; a physical controller
  (`LAB-APIC1`) and a virtual one (`LAB-APIC2`, `virtualMode=yes`), two
  spines and two leaves, one leaf `inactive`.
- Tenants: `common` and `tn_Example` with a VRF, two bridge domains
  (IPv4 and IPv6 subnets), two EPGs, an L3Out and their `fvRsCtx`,
  `fvRsBd` and `l3extRsEctx` relations.
- Physical topology: leaf and spine ports with operational state, a
  down unused port (kept only at audit purpose), a two-member
  port-channel, four spine-leaf `fabricLink` links, an LLDP adjacency
  that confirms one of them, an off-fabric switch seen by both LLDP and
  CDP (merged into one external neighbour) and a host seen by LLDP with
  a MAC-only port id.
- EPG path attachments to a port, a port-channel and a vPC (the vPC has
  no single owning resource and is not wired).
- Endpoints (audit purpose): on a port, on a port-channel and on a vPC,
  with IPv4 and IPv6 addresses.
- Faults on a port, a node and a tenant, plus a cleared fault.
- Configured policy: tenants `tn_Example`, `tn_Other` and `common`;
  application profiles `app1` and `shared`; contracts with several
  subjects and filters, filter entries (audit purpose) including
  `unspecified` sentinels and a non-TCP/UDP protocol. Relations cover
  each resolution path: by `tDn`, by name in the relation's own tenant
  (a contract named `web` exists in two tenants and each EPG resolves
  its own) and by name in tenant `common`. One EPG pair is linked
  through two different contracts.
- Management tenant `mgmt`: an out-of-band contract subject with its
  filter relation and an in-band management EPG consuming a contract.
  None is modeled; each is counted as `out_of_scope` in the coverage
  record and the scenario has no `unresolved` policy object.

### Cisco ACI Partial fabric (lab_fabric_partial)

The complete fabric with failures and malformed optional data:

- `faultInst` fails with HTTP 500 and `lldpAdjEp` and `vzFilter` with
  HTTP 403 (`failures.json`); they appear as failed in the coverage
  record and in `metadata.scope.description`. Without filters, no
  subject -> filter edge is emitted and every `vzRsSubjFiltAtt` relation
  is counted as unresolved.
- A consumed contract whose target does not exist (`missing-target`)
  emits no edge and is counted as `unresolved`; a provided contract
  under an external EPG (not modeled) is counted as `out_of_scope`.
- `fvRsBd` carries an element without `dn` and an element of another
  class; both are rejected and counted (`rejected: 2`).
- `ethpmPhysIf` repeats one object, as across a page boundary; it is
  dropped and counted (`duplicates: 1`).
- `fvRsCtx` reports a `totalCount` larger than the objects it returns;
  the coverage record carries `received` and `reported_total`.
- A fabric node of a role the producer does not model is named by a CDP
  neighbour; no connection or external neighbour is emitted for it and
  the coverage record carries `unresolved: 1`.
