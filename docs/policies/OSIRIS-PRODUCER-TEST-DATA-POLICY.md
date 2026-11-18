# OSIRIS JSON Producers - Test Data Policy<!-- omit in toc -->

| Field     | Value |
| --------- | ----- |
| Authors   | Tia Zanella [skhell](https://github.com/skhell) |
| Revision  | 1.0.0-DRAFT.1 |
| Creation date      | 16 November 2026 |
| Last revision date | 16 November 2026 |
| Status    | Draft |
| Document ID | OSIRIS-PR-TDP-1.0 |
| Document URI | Pending publication |
| Document Name | OSIRIS JSON Producers Test Data Policy |
| Specification ID | OSIRIS-1.0 |
| Specification URI | [OSIRIS-1.0](https://github.com/osirisjson/osiris/tree/main/specification/v1.0/OSIRIS-JSON-v1.0.md) |
| Schema URI | [OSIRIS-1.0](https://osirisjson.org/schema/v1.0/osiris.schema.json) |
| License   | [CC BY 4.0](https://creativecommons.org/licenses/by/4.0/) |
| Repository | [github.com/osirisjson/osiris-producers](https://github.com/osirisjson/osiris-producers) |

## Table of contents<!-- omit in toc -->

- [Purpose](#purpose)
- [1. Scope](#1-scope)
- [2. Rule catalog TDP Test Data Policy](#2-rule-catalog-tdp-test-data-policy)
- [3. Address rules](#3-address-rules)
  - [3.1 OSIRIS-PR-TDP-001 IPv4](#31-osiris-pr-tdp-001-ipv4)
  - [3.2 OSIRIS-PR-TDP-002 IPv6](#32-osiris-pr-tdp-002-ipv6)
  - [3.3 OSIRIS-PR-TDP-003 Media Access Control](#33-osiris-pr-tdp-003-media-access-control)
  - [3.4 Protocol constants](#34-protocol-constants)
  - [3.5 Loopback](#35-loopback)
  - [3.6 IPv6 link-local](#36-ipv6-link-local)
- [4. Context rules](#4-context-rules)
    - [Current detection coverage](#current-detection-coverage)
  - [4.1 OSIRIS-PR-TDP-004 Hostnames and FQDNs](#41-osiris-pr-tdp-004-hostnames-and-fqdns)
    - [Approved vendor suffixes](#approved-vendor-suffixes)
    - [Private Link and fixed-zone catalog](#private-link-and-fixed-zone-catalog)
  - [4.2 OSIRIS-PR-TDP-005 Device names](#42-osiris-pr-tdp-005-device-names)
  - [4.3 OSIRIS-PR-TDP-006 Geographic coordinates](#43-osiris-pr-tdp-006-geographic-coordinates)
  - [4.4 OSIRIS-PR-TDP-007 Sites and locations](#44-osiris-pr-tdp-007-sites-and-locations)
  - [4.5 OSIRIS-PR-TDP-008 AS numbers](#45-osiris-pr-tdp-008-as-numbers)
  - [4.6 OSIRIS-PR-TDP-009 Serial numbers and asset tags](#46-osiris-pr-tdp-009-serial-numbers-and-asset-tags)
  - [4.7 OSIRIS-PR-TDP-010 Credentials](#47-osiris-pr-tdp-010-credentials)
  - [4.8 Negative tests](#48-negative-tests)
- [5. Hyperscalers identity rules](#5-hyperscalers-identity-rules)
  - [5.1 OSIRIS-PR-TDP-011 Microsoft Azure GUIDs](#51-osiris-pr-tdp-011-microsoft-azure-guids)
  - [5.2 OSIRIS-PR-TDP-012 Microsoft Azure Resource Manager paths](#52-osiris-pr-tdp-012-microsoft-azure-resource-manager-paths)
  - [5.3 OSIRIS-PR-TDP-013 AWS account IDs](#53-osiris-pr-tdp-013-aws-account-ids)
  - [5.4 OSIRIS-PR-TDP-014 AWS resource IDs](#54-osiris-pr-tdp-014-aws-resource-ids)
  - [5.5 OSIRIS-PR-TDP-015 Hyperscalers resource names](#55-osiris-pr-tdp-015-hyperscalers-resource-names)
  - [5.6 Hyperscalers rule limitations](#56-hyperscalers-rule-limitations)
- [6. Diagnostics](#6-diagnostics)
- [7. Exceptions](#7-exceptions)
  - [7.1 OSIRIS-PR-TDP-016 Allowlist file](#71-osiris-pr-tdp-016-allowlist-file)
  - [7.2 Fingerprint](#72-fingerprint)
  - [7.3 Exception failures](#73-exception-failures)
- [8. Running the Test Data Policy scanner](#8-running-the-test-data-policy-scanner)
  - [8.1 REQUIRED CI enforcement](#81-required-ci-enforcement)
- [9. Resolving a finding](#9-resolving-a-finding)
- [10. References](#10-references)
- [11. Governance and enforcement verification](#11-governance-and-enforcement-verification)
  - [11.1 Decision record](#111-decision-record)
  - [11.2 Implementation alignment](#112-implementation-alignment)

## Purpose

This document defines which example infrastructure identifiers **MAY**
appear in the OSIRIS JSON Producers repository, the **REQUIRED** checks
and review obligations and how to resolve a finding. Descriptions of
current detection coverage are separate from policy requirements.

This policy governs committed repository content: source code, source
comments, tests, fixtures, golden OSIRIS JSON documents, documentation,
changelogs, CLI help text and CI or release configuration. It does not
change what an OSIRIS JSON producer collects or emits at runtime.

All example infrastructure identifiers in committed content **MUST** use
the approved values or satisfy an eligible exception. This includes
addresses, hostnames, device names, serial numbers, sites, coordinates,
AS numbers, credentials and hyperscalers or hosting providers
identities and resource names.
The obligation applies regardless of whether the current scanner
recognizes an occurrence. Legitimate protocol constants and published
platform identifiers retain their semantics under the rules below.

New fixtures **SHOULD** be authored synthetically. Raw exports, actual
credentials and partially sanitized environment-specific data
**MUST NOT** be committed or approved through an exception. An existing
export-derived fixture **MAY** be retained only after complete
sanitization and documented review. Review **MUST** cover content as
well as identifiers and verify that intended relationships and
assertions remain intact. Adding a lab prefix or editing a few
characters **MUST NOT** be intended as a sufficient sanitization.

File inventory, automatic detection and policy compliance are
distinct. Detection limitations do not authorize non-compliant
content. Reviewers **MUST** inspect examples outside automated coverage.
A successful scan establishes that the implemented checks found no
unsuppressed findings in the scanned content, it does not prove
synthetic provenance or completion of repository-wide enforcement.

Uppercase requirement terms in this policy follow BCP 14, as defined by
[RFC 2119](https://www.rfc-editor.org/info/rfc2119/) and
[RFC 8174](https://www.rfc-editor.org/info/rfc8174/).
These meanings apply only to uppercase usage, lowercase words retain
their ordinary English meaning.

## 1. Scope

The scanner obtains tracked paths and object modes from the Git
index (`git ls-files -z -s`) and reads regular-file contents from the
working tree. This inventories tracked files, it does not validate
staged blob contents. There are no directory, vendor or extension
exclusions from inventory.

| Object | Handling |
|---|---|
| Text file (any extension) | Scanned. A file is text when it is valid UTF-8 and has no NUL byte. |
| Binary or other non-text content | Counted and listed by path. Requires an exact content-bound review with inspection evidence (section 7). Non-UTF-8 textual content **MUST** be reviewed as text in its actual encoding. |
| Symlink | Reported as a non-waivable finding. Permitting symlinks requires a policy update. |
| Submodule | Reported as a finding. Submodules are not permitted. |
| Unsupported object, missing/unreadable file or Git failure | The scanner fails. Inventory failures cannot be allowlisted. |

Address detection runs on raw text, including Go strings, raw
strings, comments, Markdown code fences, JSON, YAML and XML. This
does not mean every semantic category is detected in every context,
section 4 describes the current context-rule coverage.

CI **MUST** use a complete checkout of the revision under test. Sparse
checkout or an unreviewed modification before scanning **MUST NOT**
silently reduce or change coverage. Missing tracked content fails
the run, local untracked-file coverage is described in section 8.

## 2. Rule catalog TDP Test Data Policy

Rule identifiers below are permanent and independent of section order,
new rules receive the next unused identifier. Retired identifiers
**MUST NOT** be reassigned.

| Rule ID | Category | Applies to |
|---|---|---|
| `OSIRIS-PR-TDP-001` | IPv4 addresses and prefixes | Every text file |
| `OSIRIS-PR-TDP-002` | IPv6 addresses and prefixes | Every text file |
| `OSIRIS-PR-TDP-003` | MAC addresses | Every text file |
| `OSIRIS-PR-TDP-004` | Hostnames and FQDNs | Host fields, URL hosts and CLI host arguments (section 4.1) |
| `OSIRIS-PR-TDP-005` | Device names | Device name fields (section 4.2) |
| `OSIRIS-PR-TDP-006` | Geographic coordinates | Latitude/longitude fields and recognizable coordinate pairs |
| `OSIRIS-PR-TDP-007` | Site and location labels | Site name fields (section 4.4) |
| `OSIRIS-PR-TDP-008` | Autonomous System numbers | ASN fields and `AS`/`ASN` labelled examples |
| `OSIRIS-PR-TDP-009` | Serial numbers and asset tags | Serial and asset tag fields |
| `OSIRIS-PR-TDP-010` | Credentials | Password, secret, token and key fields |
| `OSIRIS-PR-TDP-011` | Hyperscaler Microsoft Azure tenant, subscription, object and principal GUIDs | Azure fields, ARM paths, hyperscalers CLI arguments and Azure producer files (section 5.1) |
| `OSIRIS-PR-TDP-012` | Hyperscaler Microsoft Azure Resource Manager path structure | ARM paths in every file (section 5.2) |
| `OSIRIS-PR-TDP-013` | Hyperscaler Amazon AWS account IDs | AWS fields, ARNs and AWS producer files (section 5.3) |
| `OSIRIS-PR-TDP-014` | Hyperscaler Amazon AWS hexadecimal resource IDs | AWS resource ID families in every file (section 5.4) |
| `OSIRIS-PR-TDP-015` | Hyperscaler Microsoft Azure and Hyperscaler Amazon AWS resource group and resource names | ARM paths, ARNs and hyperscalers name fields (section 5.5) |
| `OSIRIS-PR-TDP-016` | Allowlist integrity | Repository root `testdata-policy-allowlist.json` |
| `OSIRIS-PR-TDP-017` | Inventory integrity | Symlinks, submodules, unlisted binaries, read failures |

These identifiers are local to this repository, they are unrelated to
the `V-*` diagnostic codes of OSIRIS JSON validation.

The catalog states policy applicability, detection limitations in
sections 4 and 5.6 do not narrow these obligations.

## 3. Address rules

### 3.1 OSIRIS-PR-TDP-001 IPv4

Approved prefix blocks [RFC 5737](https://www.rfc-editor.org/info/rfc5737/):

- `192.0.2.0/24`
- `198.51.100.0/24`
- `203.0.113.0/24`

A prefix **MUST** lie entirely inside one approved block: `192.0.2.0/25`
passes, while the same network with a `/16` mask fails. Addresses are
parsed with `net/netip`, `host:port` forms are recognized.
[RFC 1918](https://www.rfc-editor.org/info/rfc1918/) private ranges and
other public addresses are not approved as ordinary example data.
Necessary protocol constants follow section 3.4.

Containment uses the complete prefix, including its length. A host
address with a prefix **MAY** retain host bits when the field represents
an interface, the containing network **MUST** still lie inside an
approved block. A field representing a network uses its network
address. Malformed addresses and masks are not valid examples.

Dotted sequences representing document sections or versions are not
addresses. The current detector recognizes `section`, `chapter`,
`clause` and version-key contexts, including ARM `contentVersion`,
and skips sequences of more than four numbers.
These heuristics **MUST NOT** be used to conceal an address in a
recognizable address field.

### 3.2 OSIRIS-PR-TDP-002 IPv6

Approved prefix block [RFC 3849](https://rfc-editor.org/info/rfc3849/):

- `2001:db8::/32`

Compressed and expanded forms are normalized before comparison.
A prefix **MUST** lie entirely inside `2001:db8::/32`, bracketed forms
in URLs (`[2001:db8::1]:443`) are recognized.

The additional documentation space of [RFC 9637](https://www.rfc-editor.org/info/rfc9637/)
is not approved.

IPv4-mapped IPv6 does not inherit approval from its embedded IPv4
address. Zone-qualified literals are not ordinary approved examples,
tests requiring either representation need an exact semantic
exception. The host-bit and malformed-value requirements of section
3.1 also apply.

### 3.3 OSIRIS-PR-TDP-003 Media Access Control

Approved values:

| Kind | Range |
|---|---|
| Documentation unicast [RFC 9542 section 2.1.4](https://www.rfc-editor.org/info/rfc9542/#section-2.1.4) | `00:00:5E:00:53:00` - `00:00:5E:00:53:FF` |
| Documentation multicast [RFC 9542 section 2.1.4](https://www.rfc-editor.org/info/rfc9542/#section-2.1.4) | `01:00:5E:90:10:00` - `01:00:5E:90:10:FF` |
| Locally administered unicast | first octet with bit `0x02` set and bit `0x01` clear, e.g. `02:00:00:00:00:01` |
| Synthetic sequence (subset of local unicast) | `AA:BB:CC:DD:xx:xx` |

Colon, hyphen and Cisco dotted (`0000.5e00.5301`) forms are normalized
before comparison. Values **MUST** be valid 48-bit MAC addresses, a
globally administered vendor OUI is not approved as ordinary example
data, even with sequential trailing octets. A necessary 
protocol-specific value requires an exact exception.

### 3.4 Protocol constants

The following literals are permitted when the occurrence represents the
corresponding protocol operation or field.
Their shape alone does not authorize ordinary example device addresses,
preserve necessary semantics rather than replacing them with
documentation values.

Recognized contexts **MAY** be classified automatically, other necessary
occurrences require an exact reviewed exception.

| Class | Accepted literals |
|---|---|
| Unspecified address | `0.0.0.0`, `::`, bare or with a full-length prefix |
| Default route | `0.0.0.0/0`, `::/0` |
| Contiguous IPv4 netmask | `255.0.0.0` through `255.255.255.255` with contiguous one bits |
| IPv4 wildcard mask | `0.0.0.0` through `0.255.255.255` with contiguous one bits from the right, e.g. `0.0.0.255` |
| Null MAC | `00:00:00:00:00:00` |
| Broadcast MAC | `FF:FF:FF:FF:FF:FF` |

Netmasks shorter than `/8`, non-contiguous masks and any other
protocol-specific literal (multicast groups, link-local addresses) are
not built in. Each occurrence needs an allowlist entry that explains
why a documentation value cannot preserve the test (section 7).

The mask bounds are project choices, not a statement that shorter
masks are invalid networking constructs. The current scanner accepts
the listed constant shapes without verifying semantic use.

### 3.5 Loopback

IPv4 and IPv6 loopback addresses are permitted without an individual
exception only in `_test.go` files where the test uses or verifies
loopback behavior, such as a local listener, both conditions apply.
Ordinary infrastructure examples in those files use documentation
addresses. Necessary loopback constants elsewhere require semantic
review and an exact exception. A filename check alone does not establish
the **REQUIRED** behavior.

### 3.6 IPv6 link-local

IPv6 link-local addresses [RFC 4291](https://rfc-editor.org/info/rfc4291)
are not approved as example data.
A test that verifies link-local behaviour **MAY** keep the literal
with an allowlist entry, provided the test asserts that behaviour.

## 4. Context rules

Context rules apply where the field name or surrounding syntax shows
what a value represents. Field names are compared case-insensitively
after removing `_` and `-`, so `host_name`, `hostName` and `host-name`
are the same field. For a Go selector or an XML namespace only the last
segment counts (`c.token` is `token`).
Context rules never apply to arbitrary strings, identifiers or numbers.

#### Current detection coverage

the scanner uses line-oriented lexical extraction rather than full
document/language parsing. The following describes detection, not
permission to use unapproved values elsewhere:

| Context | Current automatic coverage | Review obligation |
|---|---|---|
| Every textual file, including production Go | Address candidates, CLI host arguments, known credential shapes, recognized hyperscalers syntax | Classify examples and necessary semantic constants. |
| Tests, fixtures, documentation and configuration, all non-production-Go text | Recognized quoted key/value strings, numeric values and single-line XML bodies, email domains, URL passwords and labelled ASN patterns | Review unrecognized fields, malformed values and other syntax. |
| YAML, TOML, INI and properties files | Also recognizes unquoted string scalars | Review multiline and unsupported constructs. |
| Markdown fences | Uses the Markdown file's lexical rules, no switch to an embedded-language parser | Review unquoted string examples and multiline values. Numeric and quoted key/value examples remain candidates. |
| Production Go examples and comments | General context key/value, email, URL-password and ASN-label checks are omitted | Apply the same policy manually, this is a coverage gap. |
| Other formats and constructed values | Recognizable raw-text patterns only | Review examples that extraction cannot recognize. |

Numeric candidates are not limited to quoted strings.
Labelled ASN examples have their own pattern detector, a parse failure
or skipped template does not authorize malformed or embedded
literal data.

### 4.1 OSIRIS-PR-TDP-004 Hostnames and FQDNs

Applies to example host, hostname, FQDN and URL fields, email domains,
URL hosts and CLI host arguments (`-h`, `--host`, `--start-url`).
In recognized host/device fields, IP literals use the address rules,
single-label device names use section 4.2 and dotted DNS names use
this rule. Section 4 describes current detection coverage.

Approved domain names [RFC 6761](https://www.rfc-editor.org/info/rfc6761/#section-6.5):

- `example.com`, `example.net`, `example.org` and their subdomains.
- A name under an approved vendor suffix whose leftmost label
  starts with `lab`, `test`, `fake`, `stub` or `example`
  (case-insensitive), for example for Microsoft Azure platform this
  is an approved hostname `labhubsa01.blob.core.windows.net`.
- Exact platform zone names in the catalog below, when used as zone
  names for the corresponding service.

The hyperscalers suffix list is a [MAINTAINERS.md](../../MAINTAINERS.md)
approved convention for examples whose service syntax matters.
Names **MUST** be authored synthetically, a permitted prefix under an
operational vendor domain does not prove that the endpoint is
unallocated or suitable for live requests.
These names do not have the reservation status of the
[RFC 6761](https://www.rfc-editor.org/info/rfc6761/#section-6.5) domains.

DNS comparison is case-insensitive and matches complete labels.
Remove one terminal DNS root dot before comparing. A permitted suffix
**MUST** be the actual hostname suffix, not an interior substring.

#### Approved vendor suffixes

| Vendor | Platform | Suffixes |
|---|---|---|
| Microsoft | Azure | `blob.core.windows.net`, `file.core.windows.net`, `queue.core.windows.net`, `table.core.windows.net`, `dfs.core.windows.net`, `vault.azure.net`, `azurewebsites.net`, `azurecr.io`, `azmk8s.io`, `azurecontainerapps.io`, `azurecontainer.io`, `database.windows.net`, `redis.cache.windows.net`, `servicebus.windows.net`, `azure-api.net`, `management.azure-api.net`, `developer.azure-api.net` |
| Amazon | Amazon Web Services | `awsapps.com` |

#### Private Link and fixed-zone catalog

| Vendor | Platform | Context | Suffixes |
|---|---|---|---|
| Microsoft | Azure | Storage Blob, File, Queue, Table and Data Lake endpoints | `privatelink.blob.core.windows.net`, `privatelink.file.core.windows.net`, `privatelink.queue.core.windows.net`, `privatelink.table.core.windows.net`, `privatelink.dfs.core.windows.net` |
| Microsoft | Azure | Azure SQL Database | `privatelink.database.windows.net` |
| Microsoft | Azure | Azure Container Registry | `privatelink.azurecr.io` |
| Microsoft | Azure | Azure Service Bus and Event Hubs | `privatelink.servicebus.windows.net` |
| Microsoft | Azure | Azure Cosmos DB SQL API | `privatelink.documents.azure.com` |
| Microsoft | Azure | Azure Database for PostgreSQL | `privatelink.postgres.database.azure.com` |
| Microsoft | Azure | Azure Cache for Redis | `privatelink.redis.cache.windows.net` |
| Microsoft | Azure | Azure API Management gateway | `privatelink.azure-api.net` |


These are exact zone-name allowances, not additional host suffixes.
`privatelink.*` is descriptive notation, not a wildcard exemption.
Other necessary fixed zones require an exact exception or a catalog
revision supported by the [Azure private endpoint DNS reference](https://learn.microsoft.com/en-us/azure/private-link/private-endpoint-dns).
Adding a suffix or catalog allowance is a policy change
requiring [MAINTAINERS.md](../../MAINTAINERS.md) approval.

Official vendor API endpoints, documentation links and project links
are classified by purpose in every file type. They are not ordinary
infrastructure examples merely because they contain a hostname.
Project references include `osirisjson.org` and its subdomains and URLs
into the `github.com/osirisjson` organization.

Match the actual URL host and organization path, not a substring.
Reverse-domain names such as `osiris.com.acme` are exempt only where
used as namespaces, not as connection targets.

Other namespaces, including `.test`, `.invalid`, `.local` and
`.internal`, are not approved for ordinary DNS examples.

### 4.2 OSIRIS-PR-TDP-005 Device names

Checked in device name fields: `hostname`, `hostName`, `host-name`,
`sysName`, `deviceName`, `device_name`, `nodeName`, `switchName`,
`clientName` and remote or neighbour device name fields.

Approved prefixes (case-insensitive): `LAB-`, `TEST-`, `FAKE-`, `STUB-`.

Examples: `LAB-RTR01`, `TEST-SW01`, `FAKE-SPINE1`, `LAB-REMOTE-SW01`.

IP literals use the address rules, dotted DNS names use section 4.1.
Empty values are permitted for deliberate missing-data tests, not as
ordinary device identities. Value-free substitutions are not literal
identifiers, literal portions of mixed templates remain subject to
the policy, a skipped template is not proof of compliance.

Vendor structural identifiers are not device names:  for example Cisco
APIC distinguished-name grammar such as `node-101`, `paths-101` or
`protpaths-101-102` is preserved in that structural context.

### 4.3 OSIRIS-PR-TDP-006 Geographic coordinates

Applies to `latitude`, `lat`, `longitude`, `lon` and `lng` fields and
recognizable coordinate pairs. Current detection uses named fields,
review pairs outside that coverage manually.

Approved: latitude `45.5433411`, longitude `10.2241237`. Equivalent
numeric forms (string or number, insignificant trailing zeroes) are
accepted.

Values **MUST** be finite and numerically valid. Do not use a tolerance
to approve a different coordinate. The current numeric comparison
uses parsed 64-bit floating-point values, a genuine geospatial boundary
test follows the exception process.

### 4.4 OSIRIS-PR-TDP-007 Sites and locations

Checked in fields naming an example site: `siteName`, `site_name`,
`site` and `locationName`.

Approved, case-sensitive: `MXP`, `BRANCH-00`, `OFFICE-00`, `FACTORY-00`.
Empty values are permitted for deliberate missing-data tests, not as
ordinary site labels.

**Not checked:** vendor structural values such as hyperscalers region names
in `location` fields like (`westeurope`) and specific Cisco numeric
SD-WAN site IDs.

### 4.5 OSIRIS-PR-TDP-008 AS numbers

Checked in ASN fields (`asn`, `as`, `localAs`, `remoteAs`, `peerAsn`,
`bgpAsn` and equivalents) and in `AS64500`/`ASN 64500` style examples.

Approved ranges:

| Range | Reference |
|---|---|
| `64496` - `64511` | [RFC 5398](https://www.rfc-editor.org/info/rfc5398/) documentation |
| `64512` - `65534` | [RFC 6996](http://rfc-editor.org/info/rfc6996) private use |
| `4200000000` - `4294967294` | [RFC 6996](http://rfc-editor.org/info/rfc6996) private use |

The [RFC 5398](https://www.rfc-editor.org/info/rfc5398/) 32-bit
documentation range `65536` - `65551` is not approved.

Ordinary examples use decimal integers, as numbers or quoted decimal
strings. Fractional, negative, malformed and out-of-range values are
not valid ASN examples. Asdot notation is outside the current detector
contract, a test requiring its syntax needs semantic review and an exact
exception where a finding is produced. Missing detection does not waive
review or authorize unrelated noncompliant ASNs.

### 4.6 OSIRIS-PR-TDP-009 Serial numbers and asset tags

Checked in serial, chassis serial, board serial and asset tag fields.

Approved: 

any value starting with the case-sensitive prefix `TST`, for example
`TST0000001` or `TST-CHASSIS-01`. The bare value `TST` satisfies this
prefix rule, but distinct identities **MUST** remain distinct.
Empty values are permitted for deliberate missing-data tests.

Vendor pseudo-serials that encode another identifier are checked as that
identifier instead, for example an HPE Aruba Central `tpd_<mac>` value
is checked under `OSIRIS-PR-TDP-003`.

### 4.7 OSIRIS-PR-TDP-010 Credentials

Checked in every field whose name ends in `password`, `passwd`,
`passphrase`, `secret`, `token`, `apikey`, `privatekey` or
`authorization` (so `clientSecret` and `accessToken` are covered) and
in the password part of URL user information, subject to the current
detection coverage in section 4.

Pagination fields (`nextToken`, `pageToken`, `continuationToken`,
`skipToken`, `boundaryToken`) are excluded from credential
classification only when they actually represent pagination state.
This does not authorize copied production cursors or suppress
other rules.

Approved values: `admin`, `test`, `password`, `secret`, `changeme`, each
optionally followed by `-` and a decimal number where a test needs
distinct values (`secret-1`, `secret-2`).
The empty string is accepted, for tests of missing credentials. The SDK
redaction markers `[REDACTED]` and `REDACTED` and template or variable
references that contain no value, such `{{ .Env.HOMEBREW_TAP_TOKEN }}`
or `${{ secrets.SONAR_TOKEN }}`, are not credentials.

Actual issued credentials, tokens and private authentication material
**MUST NOT** be committed or allowlisted, including when expired or
revoked. Literal credentials embedded in mixed templates remain
prohibited. A variable reference in one part of a value does not
authorize a hardcoded credential in another part.

Well-known real credential shapes are reported in every file, including
production sources: AWS access key IDs, GitHub tokens, JSON Web Tokens
and PEM private key headers.

Fabricated credential-shaped values **MAY** be retained only where a
detector, redaction or authentication-handling test requires that shape
and asserts the relevant behavior. Each stored prohibited occurrence
requires an exact exception identifying its test purpose and fabricated
origin. Diagnostics **MUST NOT** expose credential values through any
rule's output or surrounding source excerpts.

This rule is a repository hygiene control. It does not replace the SDK
secret scanner, which keeps running independently.

### 4.8 Negative tests

The policy scanner scans its own source and tests. Its negative tests
**SHOULD** construct prohibited samples dynamically rather than storing
complete forbidden literals. This narrow test technique **MUST NOT** be
used in ordinary fixtures or examples to evade the policy.

Other tests that require stored prohibited literals follow section 7
and **MUST** preserve the assertion proving why an approved replacement
would change the behavior under test.

## 5. Hyperscalers identity rules

Hyperscalers identity rules apply to customer identities recognized from
hyperscalers fields and syntax. File location is a detector hint, not
proof that every GUID or integer is a customer identity.

Classify recognized protocol identifiers, version fields and published
platform constants before applying synthetic-identity requirements.

UUIDs of other protocols, such as SD-WAN device identifiers, are not
hyperscalers identities.

Necessary published constants require a precise, sourced catalog entry
or an exact semantic exception. For example, a published Microsoft Azure
built-in role ID is not a tenant identity, retain it when its exact
semantics matter. Ordinary tenant, subscription, principal, account and
resource identities retain the approved synthetic forms.

The current scanner treats every GUID in an Microsoft Azure producer
file and every 12-digit integer candidate and in an AWS producer file
as an identity candidate.
Microsoft Azure paths include `osiris/hyperscalers/azure/`, the
Microsoft Azure command and fixtures, Amazon AWS paths follow the
corresponding layout `osiris/hyperscalers/aws/`.
Review false positives using the classification above.
This broad heuristic and its limits are recorded in section 5.6.

### 5.1 OSIRIS-PR-TDP-011 Microsoft Azure GUIDs

Checked in `tenant`, `tenantId`, `subscription`, `subscriptionId`,
`objectId`, `principalId`, `clientId`, `applicationId` and `appId`
fields, in `account` fields holding a GUID (OSIRIS provider metadata),
in ARM path subscription and GUID name segments, in `-S`,
`--subscription` and `--tenant` CLI arguments. File-based GUID detection
additionally applies as described in section 5, it does not change the
meaning of a published constant.

Approved form, case-insensitive:

```text
00000000-0000-N000-8000-00000000XXXX
```

where `N` is a digit from `1` to `5` and `XXXX` is four hexadecimal
digits. These are synthetic identifiers shaped like UUID versions 1
to 5, they do not claim to be generated by those algorithms.

| Purpose | Example |
|---|---|
| Tenant | `00000000-0000-1000-8000-000000000001` |
| Subscription A | `00000000-0000-1000-8000-000000000011` |
| Subscription B | `00000000-0000-2000-8000-000000000012` |
| Subscription C | `00000000-0000-3000-8000-000000000013` |
| Object ID | `00000000-0000-4000-8000-000000000021` |
| Additional identity | `00000000-0000-5000-8000-000000000022` |

Distinct identities keep distinct values. Hand-typed, repeated-digit
and other GUID forms are not approved.

The nil UUID (all zeroes) is rejected by default. A test that needs
nil or empty identity semantics keeps it with one exact allowlist
entry per occurrence (section 7).

### 5.2 OSIRIS-PR-TDP-012 Microsoft Azure Resource Manager paths

ARM IDs exist at resource-group, subscription, management-group and
tenant scopes. The current structural detector recognizes paths starting
with `/subscriptions/`, it does not provide full structural coverage for
tenant or management-group-rooted IDs.
Those forms remain governed by the policy and require review.

See the [ARM resource ID formats](https://learn.microsoft.com/en-us/azure/azure-resource-manager/templates/template-functions-resource#resourceid).

For a recognized subscription-rooted path:

- The subscription segment **MUST** be an approved GUID (section 5.1),
  any other literal fails this rule.
- The name following `resourceGroups` is checked under section 5.5.
- After `providers`, the namespace (such as `Microsoft.Network` or
  `microsoft.insights`) is preserved and each following type and
  name pair is checked, a nested `providers` segment starts a new
  extension resource.
- A name under `locations` is an Azure region and is not checked.
- A GUID name segment, such as a role assignment, is checked as a GUID.
- A composite name with `,`, such as a Recovery Services container
  `iaasvmcontainerv2,<resource group>,<vm>`, keeps its leading service
  tokens, the embedded resource group and resource names are checked.

Value-free placeholders such as `%s`, `{rg}` or `...` are not literal
identities. Literal segments and literal parts around placeholders
remain subject to the applicable rules. A skipped placeholder does not
authorize an unchecked resource name elsewhere in the path.

### 5.3 OSIRIS-PR-TDP-013 AWS account IDs

Approved Amazon AWS accounts, used consistently for single, cross and
multi-account cases:

| Account example | Use |
|---|---|
| `123456789012` | Default example account |
| `111122223333` | Second account |
| `444455556666` | Third account |

Checked in `accountId`, `awsAccountId`, `ownerId`, `ownerAccount`,
`sourceAccount`, `targetAccount`, `accountNumber`, `peerOwnerId` and
`requesterId` fields, in `account` fields holding 12 digits (OSIRIS JSON
provider metadata), in the account component of every ARN (IAM and STS
included). File-based account detection additionally applies as
described in section 5.

An empty ARN account component (for example an S3 bucket ARN) and the
AWS-owned account `aws` (for example AWS managed policies) are valid.

### 5.4 OSIRIS-PR-TDP-014 AWS resource IDs

Applies to the AWS resource families with hexadecimal ID suffixes:
`vpc`, `subnet`, `sg`, `i`, `eni`, `rtb`, `igw`, `nat`, `acl`, `vol`,
`snap`, `ami`, `tgw`, `tgw-attach`, `pcx`, `vpce`, `eigw`, `eipalloc`,
`dopt`, `pl`, `lt`, `cgw`, `vgw` and `vpn`.

Approved suffixes are lowercase and zero-padded:

| Length | Form | Example |
|---|---|---|
| 17 | 13 zeroes and four hexadecimal digits | `vpc-00000000000000001` |
| 8 (legacy) | 4 zeroes and four hexadecimal digits | `sg-00000001` |

Any other length, an unpadded value or uppercase digits fail. Other AWS
identifiers (IAM names, S3 buckets, load balancer IDs, resource paths)
follow their own grammar and section 5.5.
The approved eight-digit legacy form needs no exception, a test
requiring an invalid or otherwise unapproved legacy form needs an
exact exception.

### 5.5 OSIRIS-PR-TDP-015 Hyperscalers resource names

Invented resource group and resource names start with `LAB`, `TEST`,
`FAKE` or `STUB`, in any case and with or without a separator, in the
syntax the service supports:

| Kind | Examples |
|---|---|
| Microsoft Azure resource group | `RG-LAB`, `RG-LAB-HUB-NET`, `RG-TEST-01` (`rg-` or `rg_` **MAY** precede the word) |
| Microsoft Azure and Amazon AWS resources | `LAB-VNET-HUB`, `LAB-VM-01`, `labstorage01`, `lab-test-bucket-01`, `LAB-ROLE-01` |

Checked in ARM path resource group and name segments, in `resourceGroup`
and `resourceGroupName` fields, in ARN resource names (IAM roles, users,
groups, policies and instance profiles, STS assumed roles, S3 buckets,
Lambda functions, load balancers and target groups) and in `bucketName`,
`roleName` and `functionName` fields of Amazon AWS and Microsoft Azure
producer files.

The table defines OSIRIS JSON example constraints and selected service
syntax requirements. It is not a complete deployment validator.
Examples **MUST** preserve the relevant service grammar, the scanner
currently implements only part of that grammar.

| Vendor | Platform service | Rule |
|---|---|---|
| Microsoft | Azure resource group | OSIRIS JSON examples use 1-90 ASCII letters, digits, `_`, `-`, `.`, `(`, `)`, not ending in `.`. This ASCII subset is narrower than Microsoft Azure's Unicode letter/digit support. |
| Microsoft | Azure storage account | 3-24 lowercase letters and digits |
| Microsoft | Azure Key Vault | 3-24 letters, digits and `-`, starts with a letter, ends with a letter or digit, no consecutive hyphens |
| Amazon | AWS S3 general purpose bucket | 3-63 lowercase letters, digits, `.` and `-`, starts and ends with a letter or digit, no adjacent periods, IP-address form or reserved prefix/suffix |
| Amazon | AWS IAM name | OSIRIS JSON examples retain a 1-64 character limit using ASCII letters, digits and `+=,.@_-`. This is a project limit, not the AWS maximum for every IAM resource type. |
| Amazon | AWS Lambda function | 1-64 of letters, digits, `_` and `-` |

Amazon AWS S3 examples use the shared global namespace unless the test
explicitly requires a different namespace or bucket category.
Apply the vendor's reserved-prefix and suffix restrictions, including
`-s3alias`, `--ol-s3`, `.mrap`, `--x-s3` and `--table-s3`.

The `-an` suffix requires the account regional namespace, special
category or invalid syntax tests follow section 7 rather than changing
ordinary examples.

See the [S3 naming rules](https://docs.aws.amazon.com/AmazonS3/latest/userguide/bucketnamingrules.html).

Amazon AWS permits 128 characters for group, instance-profile and
managed policy names, while roles and users have 64-character limits.
Tests requiring the longer valid forms can use exact semantic exceptions
to the stricter OSIRIS example limit.

See the:
- [Amazon AWS IAM character limits](https://docs.aws.amazon.com/IAM/latest/UserGuide/reference_iam-quotas.html)
- [Microsoft Azure syntaxresource naming rules](https://learn.microsoft.com/en-us/azure/azure-resource-manager/management/resource-name-rules)

Service-defined names are never renamed, they are approved only in their
own context:

| Literal | Approved context |
|---|---|
| `GatewaySubnet` | Subnet of a VPN or ExpressRoute gateway |
| `RouteServerSubnet` | Subnet of Azure Route Server |
| `AzureFirewallSubnet` | Subnet of Azure Firewall |
| `AzureBastionSubnet` | Subnet of Azure Bastion |
| `default` | Child of `Microsoft.Storage/storageAccounts` with type `blobServices`, `fileServices` or `managementPolicies`, not an arbitrary child resource |
| `azureblob` | `managedApis` name |
| `ipconfig1` | Explicit compatibility allowance for `ipConfigurations` fixture names, it is not a service-mandated name |
| `microsoft.insights` | Resource provider namespace (never a resource name) |

Outside its context the same literal is an ordinary name and **MUST** be
synthetic, preserve AWS-defined managed policy names and the fixed parts
of service-linked roles in their actual service contexts.
A user-chosen suffix or path component still requires sanitization,
`AWSServiceRole*` is not a blanket permission for arbitrary names.

See the [service-linked role reference](https://docs.aws.amazon.com/IAM/latest/UserGuide/id_roles_create-service-linked-role.html).

### 5.6 Hyperscalers rule limitations

These are detection limitations, not exemptions from synthetic-data
requirements. Reviewers **MUST** inspect the omitted contexts:

- Plain `name` fields are not checked, because they also hold
  service-defined values (SKUs, regions, built-in roles), invented
  names are checked where the same name appears in an ARM path, an ARN
  or a dedicated name field.
- Amazon AWS resource names held only in tags (`Name`) and values passed
  as function arguments are not checked.
- Amazon AWS IAM and STS unique IDs (for example `AROA...`) are not
  checked.
- Microsoft Azure ARM structural detection does not cover tenant or
  management-group-rooted paths.
- File-based GUID/account heuristics do not prove customer-identity
  semantics, published constants **MAY** need exact exceptions.
- Fixed-name, template and service-linked-role heuristics are broader
  than the policy's contextual allowances.

## 6. Diagnostics

The current scanner reports each finding on one line:

```text
<path>:<line>:<column>: <rule-id>: <safe explanation>, use <alternative> [<fingerprint>]
```

The bracketed fingerprint is included only for findings eligible for
exception matching, text locations use one-based lines and byte columns.
Binary and inventory findings without a text location omit the line
and column.

Ordinary findings and exception problems are each sorted by path, line,
column and rule ID. The scanner reports all collected findings rather
than stopping at the first ordinary violation, an inventory or read
failure **MAY** terminate the run, that is a failure, not an empty
successful report.

Credential values and sensitive source fragments **MUST** NOT be printed
by any diagnostic path.

The run ends with a summary:

```text
Test Data Policy
Inventory: tracked files (Git index)|tracked and untracked files
Allowlist: testdata-policy-allowlist.json, N entries applied|invalid|NOT USED|none
Files inventoried: N
Text files scanned: N
Binary/unsupported files accounted for: N
Candidates inspected: N
Violations: N
Approved exceptions: N
Stale exceptions: N
Result: PASS|FAIL
```

`Inventory` states whether untracked files were included (section 8).
`Allowlist` states whether exceptions were applied. An allowlist present
in the working tree but not tracked by Git is `NOT USED` in the default
mode: its entries are not applied, the occurrences it covers are
reported as violations and the summary ends with a note explaining how
to stage the file or use local untracked mode.

`Violations` counts unsuppressed findings. `Approved exceptions` counts
findings matched by valid entries.
`Stale exceptions` counts entries whose path or finding no longer
exists. Other invalid entries are reported as `OSIRIS-PR-TDP-016`
diagnostics, the current summary does not contain a separate total for
them. Invalid entries cause failure even when `Violations` is zero.

Counts describe the actual inventory and recognized candidates, not
proof that every policy-relevant value was detected. CI evidence
**MUST** also record the revision, tracked/untracked inventory mode,
working-tree content source and UTC evaluation date.

The summary emits the inventory mode, the revision, content source
and evaluation date are not currently emitted.

## 7. Exceptions

### 7.1 OSIRIS-PR-TDP-016 Allowlist file

Reviewed exceptions live in `testdata-policy-allowlist.json` at the
repository root, as an `exceptions` array.

Each text entry **MUST** cover exactly one occurrence **REQUIRED** for
protocol semantics, parser/error-handling behavior, geospatial boundary
testing or a fabricated security regression test.
Explain why an approved replacement would change that behavior and
identify the supporting assertion or review evidence.

A binary review is a separate eligible case: it approves one exact
artifact at one path, bound to its content fingerprint.
The tracking reference records provenance, an inspection method
appropriate to the format and the review result, for images or
animations, inspect all relevant visual content, for containers, inspect
their contents.

Non-UTF-8 textual content **MUST** be recognized and reviewed in its
actual encoding, content that cannot be adequately inspected **MUST**
not be approved merely because a hash can be computed.

Changing any binary bytes requires a new review, this is the sole
whole-artifact allowance, it does not permit whole-text-file
suppression. `OSIRIS-PR-TDP-017` is eligible only for this defined
content review.
Enumeration/read failures, unsupported objects, prohibited symlinks or
submodules and exception integrity failures cannot be suppressed.
`OSIRIS-PR-TDP-016` is never eligible.

Each new or changed entry requires pull-request review by the
responsible maintainer. A minimum reason length is only a mechanical
check, not evidence that the reason is technically sufficient.

The following JSON schema illustrates the contract, its path,
fingerprint, tracking reference and date are placeholders, not an entry
to copy unchanged:

```json
{
  "exceptions": [
    {
      "path": "osiris/category/example/example_test.go",
      "rule": "OSIRIS-PR-TDP-002",
      "fingerprint": "sha256:<32 hex digits>",
      "reason": "Verifies that link-local prefixes are skipped",
      "owner": "FullName <address@domain>",
      "issue": "<tracking reference>",
      "review_by": "YYYY-MM-DD"
    }
  ]
}
```

Unknown fields and trailing data are rejected.

| Field | Requirement |
|---|---|
| `path` | Exact repository-relative path. No globs, no directories. |
| `rule` | An existing rule ID eligible for this finding under section 7.1. |
| `fingerprint` | The occurrence fingerprint printed in the finding. |
| `reason` | Specific technical justification of at least four words. Generic text such as "needed" or "legacy" is rejected. |
| `owner` | Responsible team or designated maintainers as appear in [MAINTAINERS.md](../../MAINTAINERS.md) |
| `issue` | Tracking reference for the review. |
| `review_by` | Mandatory valid ISO calendar date, `YYYY-MM-DD`, expires when the current UTC date is later than this date. |

Entries are sorted by `path`, then `rule`, then `fingerprint`.
The allowlist never contains the excepted literal itself.

Capture the UTC evaluation date once per run. An entry is valid through
its `review_by` date, invalid dates fail, results are deterministic for
identical content, configuration and evaluation date, renewal requires
review, not only advancing the date.

### 7.2 Fingerprint

**Fingerprint contract, revision 1:** the current implementation
serializes these fields in order, appending one NUL byte after each:

1. Rule ID.
2. Repository-relative path.
3. Detector-supplied canonical value.
4. Line text with leading and trailing whitespace removed.
5. Zero-based decimal ordinal among retained findings with identical
   rule, path, canonical value and trimmed line text.

The fingerprint is `sha256:` followed by the lowercase hexadecimal
encoding of the first 16 bytes of SHA-256 over that serialization
(32 hexadecimal characters, a 128-bit truncation). Copy the value
printed by the scanner rather than computing it manually.

Canonicalization is rule-specific, the authoritative current definitions
are in the [address evaluators](../../internal/testdatapolicy/identifiers.go),
[context evaluators](../../internal/testdatapolicy/context.go) and
[hyperscalers evaluators](../../internal/testdatapolicy/hyperscalers.go)
serialization and ordinal assignment are in [diagnostics.go](../../internal/testdatapolicy/diagnostics.go).

Absolute line numbers are not hashed, stability depends on the
canonical value, normalized line context and duplicate-occurrence
identity remaining unchanged. Leading/trailing whitespace edits **MAY**
preserve a hash, edits or reordering involving duplicates **MAY** change
it. A new matching hash does not remove the obligation to review a
changed test's meaning.

For a binary, the canonical value is the full lowercase SHA-256
digest of its bytes, line text is empty and the ordinal is zero.
That value is hashed with the rule and path using the same contract.
Changing the bytes or path therefore requires a new entry and review.

The current scanner coalesces same-line findings with the same rule
and canonical value, this can merge distinct literal occurrences,
do not treat a shared fingerprint as approval of multiple occurrences.

Changes to normalization, deduplication or serialization require a
documented contract revision and reviewed exception migration.

### 7.3 Exception failures

`OSIRIS-PR-TDP-016` fails the run when an entry:

1. matches no current finding (stale),
2. names a path that no longer exists,
3. names an unknown or non-exceptable rule,
4. duplicates another entry,
5. has an empty or generic reason, or a missing maintainers or issue,
6. has an invalid or expired `review_by` date,
7. tries to cover a directory, a glob or a whole text file, or
8. is not in sorted order.

Malformed allowlist content also fails. A binary approval **MUST** meet
section 7.1 rather than act as a general whole-file exclusion.
Actual credentials, raw exports and partially sanitized environment
data are never eligible, fully sanitized derivatives follow the
purpose's provenance review, an exception cannot substitute for it.

## 8. Running the Test Data Policy scanner

Run commands from the repository root.

Focused run:

```bash
go test ./internal/testdatapolicy -run TestRepositoryDataPolicy -count=1 -v
```

The same test also runs as part of the full suite:

```bash
go test ./...
```

The current repository has one root Go module. If nested modules are
introduced, document and run their relevant regression suites separately
a root `go test ./...` does not cover nested modules.

Policy evaluation works offline and **MUST** never modify repository
files. It needs a Git working tree, outside one it fails rather
than passing.

Go toolchain/dependency setup is separate from offline evaluation, the
scanner uses tracked paths from the index and working-tree contents as
described in section 1.

To also scan untracked files that are not ignored, before staging them:

```bash
TDP_INCLUDE_UNTRACKED=1 go test ./internal/testdatapolicy -run TestRepositoryDataPolicy -count=1 -v
```

CI uses the tracked-path inventory on a complete checkout of the
revision under test. Local untracked mode adds non-ignored untracked
paths to that inventory, it does not validate staged blobs, a local pass
during partial staging does not establish that the staged snapshot
passes. The eventual committed revision **MUST** be checked in CI.

### 8.1 REQUIRED CI enforcement

A local pass or green CI run does not establish merge enforcement.
The repository-wide gate is complete only after **REQUIRED** remediation
is finished, the checks cover the intended repository and the stable
check is verified as **REQUIRED** by branch protection. No vendor/path
filter or broad skip condition **MAY** bypass repository-wide coverage.

Any diagnostic-only stage and unresolved fixture dependency, including
deferred sandbox-derived golden sanitization, **MUST** be recorded in
release evidence, policy revision and enforcement status are recorded
separately.

## 9. Resolving a finding

1. Classify the occurrence as an ordinary example, necessary semantic
   value, fabricated negative-test input or artifact requiring review.
2. Replace ordinary examples with approved values from sections 3–5.
   Keep relationships intact: distinct objects remain distinct, hosts
   stay inside their subnets, peers remain peers and meaningful
   boundary conditions and assertions are preserved.
3. For eligible non-replaceable occurrences, follow section 7 and
   explain the justification in the pull request. Do not replace a
   necessary protocol constant merely to pass the scanner.
4. Update dependent assertions and regenerate affected goldens through
   the established offline fixture workflow. Review the diff for
   unintended graph or behavioral changes. See the [fixture update instructions](../../testdata/README.md#updating-and-validating).
5. Run affected package regressions, the applicable module suites,
   independent security checks and pinned canonical strict validation
   for modified OSIRIS JSON goldens. Preserve producer behavior and
   OSIRIS JSON compatibility.
6. Confirm that the eventual committed revision receives the
   **REQUIRED** CI checks and that no exception is stale.

The current canonical validation command, matching the pin in the
[golden workflow](../../.github/workflows/golden.yml), is:

```bash
OSIRIS_CLI_VERSION=0.1.0 ./scripts/validate_golden.sh --profile strict testdata
```

Use the workflow's reviewed pin if it changes. Tool installation **MAY**
require network access, fixture regeneration **MUST NOT** collect live
customer data. Canonical validation and the SDK secret scanner remain
separate from this policy scanner.

## 10. References

Project inventories **MAY** intentionally be narrower than the
referenced standards. An external allocation or service change does not
silently expand the approved policy.

- [RFC 5737 - IPv4 Address Blocks Reserved for Documentation](https://www.rfc-editor.org/rfc/rfc5737.html)
- [RFC 3849 - IPv6 Address Prefix Reserved for Documentation](https://www.rfc-editor.org/rfc/rfc3849.html)
- [RFC 9637 - Expanding the IPv6 Documentation Space](https://www.rfc-editor.org/rfc/rfc9637.html)
- [RFC 9542 - IEEE 802 parameters and documentation values](https://www.rfc-editor.org/rfc/rfc9542.html#section-2.1.4)
- [RFC 1918 - Private IPv4 allocation](https://www.rfc-editor.org/rfc/rfc1918.html)
- [RFC 4291 - IPv6 addressing architecture](https://www.rfc-editor.org/rfc/rfc4291.html)
- [RFC 2606 - Reserved example DNS names](https://www.rfc-editor.org/rfc/rfc2606.html)
- [RFC 6761](https://www.rfc-editor.org/info/rfc6761/#section-6.5)
- [RFC 5398 - Documentation AS numbers](https://www.rfc-editor.org/rfc/rfc5398.html)
- [RFC 6996 - Private-use AS numbers](https://www.rfc-editor.org/rfc/rfc6996.html)
- [Microsoft Azure resource naming rules](https://learn.microsoft.com/en-us/azure/azure-resource-manager/management/resource-name-rules)
- [Microsoft Azure private endpoint DNS zones](https://learn.microsoft.com/en-us/azure/private-link/private-endpoint-dns)
- [Microsoft Azure ARM resource ID formats](https://learn.microsoft.com/en-us/azure/azure-resource-manager/templates/template-functions-resource#resourceid)
- [Microsoft Azure role definitions and published IDs](https://learn.microsoft.com/en-us/azure/role-based-access-control/role-definitions)
- [Amazon AWS S3 general purpose bucket naming rules](https://docs.aws.amazon.com/AmazonS3/latest/userguide/bucketnamingrules.html)
- [Amazon AWS IAM naming and character limits](https://docs.aws.amazon.com/IAM/latest/UserGuide/reference_iam-quotas.html)
- [Amazon AWS service-linked roles](https://docs.aws.amazon.com/IAM/latest/UserGuide/id_roles_create-service-linked-role.html)
- [Git tracked-file inventory](https://git-scm.com/docs/git-ls-files)

## 11. Governance and enforcement verification

The repository [MAINTAINERS.md](../../MAINTAINERS.md) approves changes
to allowed values, rule scope, semantic/compatibility allowances and the
exception contract.
Record each accepted change in this policy and its [CHANGELOG](CHANGELOG.md).
Keep stable document identity separate from revision and effective
status, rule IDs **MUST NOT** silently acquire incompatible meanings.

### 11.1 Decision record

| Date | Decision | Status |
|---|---|---|
| 16 November 2026 | Microsoft Azure and Amazon AWS identity rules and synthetic hostnames under the listed real hyperscalers service suffixes | Approved, retained in this revision. |
| 16 November 2026 | Apply the reviewed document replacements and insertions, including numbered credential placeholders, exact binary review and controlled sanitization of existing derived fixtures | Documentation revision authorized, recorded as `1.0.0-DRAFT.1`. |
| 21 November 2026 | Adopt an effective policy revision and complete **REQUIRED** CI enforcement | Requires implementation-alignment and enforcement evidence, not established by document editing or a local scan. |

### 11.2 Implementation alignment

Maintainers **MUST** track known enforcement gaps and verification
evidence in the implementation records. Detection limitations do not
waive this policy, reviewers **MUST** inspect examples outside
automated coverage.

Full enforcement **MUST NOT** be claimed until the required checks,
remediation and merge protections have been verified.
