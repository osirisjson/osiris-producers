# OSIRIS JSON Architecture Producer SDK Internals<!-- omit in toc -->
| Field     | Value |
| --------- | ----- |
| Authors   | Tia Zanella [skhell](https://github.com/skhell) |
| Revision  | 1.0.0-DRAFT |
| Creation date      | 16 February 2026 |
| Last revision date | 18 February 2026 |
| Status    | Draft |
| Document ID | OSIRIS-ADG-PR-SDK-1.0 |
| Document URI | [OSIRIS-ADG-PR-SDK-1.0](https://github.com/osirisjson/osiris-producers/tree/main/docs/guidelines/v1.0/OSIRIS-PRODUCER-SDK.md) |
| Document Name | OSIRIS JSON Architecture Producer SDK Internals |
| Specification ID | OSIRIS-1.0 |
| Specification URI | [OSIRIS-1.0](https://github.com/osirisjson/osiris/tree/main/specification/v1.0/OSIRIS-JSON-v1.0.md) |
| Schema URI | [OSIRIS-1.0](https://osirisjson.org/schema/v1.0/osiris.schema.json) |
| License   | [CC BY 4.0](https://creativecommons.org/licenses/by/4.0/) |
| Repository | [github.com/osirisjson/osiris-producers](https://github.com/osirisjson/osiris-producers) |

# Table of Content
<!-- work in progress -->

# 1 SDK composition
The Go producer SDK (`pkg/sdk` within `osiris-producers`) provides foundational types, helpers and conventions that every first-party producer builds on Go structs to generate OSIRIS documents and a test harness.

> [!NOTE]
> Back-reference: Ecosystem boundaries and dependency rules are defined in [OSIRIS-ADG-1.0](https://github.com/osirisjson/osiris/tree/main/docs/guidelines/v1.0/OSIRIS-ARCHITECTURE.md).  
> The producer mapping contract (identity, normalization, redaction, emission) is defined in [OSIRIS-ADG-PR-1.0](https://github.com/osirisjson/osiris/tree/main/docs/guidelines/v1.0/OSIRIS-PRODUCER-GUIDELINES.md).  
> All `V-*` diagnostic codes are owned by the canonical validation engine (`@osirisjson/core`). The SDK **MUST NOT** emit or interpret validation diagnostics.

---

## 1.1 Base contracts

### 1.1.1 The producer interface
Every vendor producer implements a single interface that the dispatcher invokes.

```go
// Producer is the contract every vendor backend MUST satisfy.
type Producer interface {
    // Collect discovers vendor inventory and returns an assembled OSIRIS document.
    // Partial failures SHOULD be logged and skipped (non-fatal).
    Collect(ctx *Context) (*Document, error)
}
```

**Contract rules:**
- `Collect` receives a fully initialized `Context` (configuration, logger, scope).
- `Collect` returns a `*Document` ready for JSON marshaling when the export succeeded.
- `Collect` returns an error only when the export cannot proceed at all (e.g. authentication failure, invalid configuration, unreachable endpoint).
- Partial failures for individual resources **SHOULD NOT** abort the entire export; producers log the failure via `ctx.Logger` and continue (see [OSIRIS-ADG-PR-1.0](https://github.com/osirisjson/osiris/tree/main/docs/guidelines/v1.0/OSIRIS-PRODUCER-GUIDELINES.md) section 2.1).
- If safe failure mode is `fail-closed`, producers **MUST** halt emission for the affected resource or fail the entire run (producer policy). If failing the run, `Collect` returns an error and exits non-zero (see section 1.2).


### 1.1.2 The context struct
`Context` carries everything a producer needs at runtime without relying on package-level globals.

```go
// Context provides shared runtime state for a producer execution.
type Context struct {
    Config *ProducerConfig
    Logger Logger
    Clock  func() time.Time // injectable for deterministic tests
}
```

| Field | Purpose | Default |
|---|---|---|
| `Config` | Parsed producer configuration (credentials path, scope filters, detail level) | Required; set by the dispatcher before calling `Collect` |
| `Logger` | Structured logger for operational messages (not OSIRIS diagnostics) | `slog`-compatible default logger writing to `stderr` |
| `Clock` | Time source for `metadata.timestamp` | `time.Now`; tests inject a fixed clock for deterministic output |

---

## 1.2 Shared context (logging, environment, configuration)
The SDK defines a minimal `Logger` interface and a `ProducerConfig` struct. Producers **MUST NOT** introduce their own logging abstractions.

|Logging|Configuration|
|---|---|
| Operational messages go to `stderr` via `ctx.Logger`. The OSIRIS document goes to `stdout` (or a configured output path). Producers **MUST NOT** write diagnostics in the `V-*` code format; diagnostic codes are owned by `@osirisjson/core`. | `ProducerConfig` carries common fields (output path, profile hint, scope filters, detail level, redaction mode). Vendor-specific fields (endpoint URLs, credential paths, SSH options) are defined by each producer as an embedded struct extending `ProducerConfig`. The SDK provides `EnvOrDefault(key, fallback)` for safe environment variable lookups. |

**Safe failure behavior modes aligned to OSIRIS-ADG-PR-1.0 section 3.3:**
- `fail-closed` **recommended default**: when a secret is detected, the producer **MUST** halt emission for the affected resource **or** fail the entire run (producer policy).  
  - Always log **field path only** (never the value).
  - If the producer chooses “fail run”, it exits non-zero and returns an error.
- `log-and-redact` (opt-in): when a secret is detected, the producer **MAY** replace the value with a stable placeholder (e.g. `"REDACTED"`) and continue emission.  
  - Always log **field path only** (never the value).
  - The producer **MUST** set `metadata.redacted: true` and `metadata.redaction_policy: "log-and-redact"`.
- `off` (development only): secret scanning disabled. **MUST NOT** be the default in released producers and **MUST NOT** be used in CI pipelines.

---

## 1.3 Relationship to @osirisjson/core (dev-dependency only constraint)
The Go producer SDK **MUST NOT** import, embed or link against `@osirisjson/core` (TypeScript/NPM). This is a hard architectural boundary established in [OSIRIS-ADG-1.0](https://github.com/osirisjson/osiris/tree/main/docs/guidelines/v1.0/OSIRIS-ARCHITECTURE.md) section 2.4.1.

| What the SDK does | What the SDK does NOT |
|---|---|
| Assembles structurally valid OSIRIS JSON using typed Go structs that mirror the [osiris.schema.json](https://osirisjson.org/schema/v1.0/osiris.schema.json) | Run schema validation, semantic checks or domain rules |
| Provides deterministic ID generation following the normative algorithms in [OSIRIS-ADG-PR-1.0](https://github.com/osirisjson/osiris/tree/main/docs/guidelines/v1.0/OSIRIS-PRODUCER-GUIDELINES.md) section 2.2 | Emit `V-*` diagnostic codes or interpret validation severity |
| Ships golden-file test helpers that invoke `@osirisjson/cli` as an external process in CI | Depend on `@osirisjson/core` as a library at build time or runtime |

**CI validation pattern:**
```bash
# Producers validate output via the canonical CLI (external process, not a library import)
npx @osirisjson/cli validate --profile strict output.json
```

---

# 2 Developer utilities
Utilities are pure functions or lightweight builders with no side effects. They operate on SDK types and return values; they never perform I/O tasks.

---

## 2.1 Identity hashing and naming helpers
The SDK implements the normative ID generation algorithms defined in [OSIRIS-ADG-PR-1.0](https://github.com/osirisjson/osiris/tree/main/docs/guidelines/v1.0/OSIRIS-PRODUCER-GUIDELINES.md) section 2.2. Producers call these helpers rather than implementing their own hashing.

**Core hash primitive:**

```go
// Hash16 computes the first 16 characters of the lowercase hex SHA-256 of the canonical key. It is the foundation for connection and group IDs.
func Hash16(canonicalKey string) string
```

When a collision is detected within a document, producers extend the hash length (24 or 32 characters) by calling `HashN(canonicalKey, n)` rather than adding randomness.

**Deterministic map serialization (normative for SDK helpers):**
- Serialize maps as sorted `k=v` pairs.
- Map ordering **MUST** sort by the **encoded key** (`EncodeComponent(key)`), ascending.
- `Qualifiers`: comma-joined, omit absent/empty qualifiers.
- `ScopeFields`: pipe-joined, omit absent/empty keys.
- Producers **MUST** avoid delimiter ambiguity by applying percent-encoding to every serialized component before concatenation.
  - At minimum, the SDK **MUST** encode: `%` -> `%25`, `|` -> `%7C`, `,` -> `%2C`, `=` -> `%3D`



### 2.1.1 Collision handling
Collision handling **MUST** be standardized by the SDK to prevent producer drift and to remain order-independent.

Rule (order-independent):
- The SDK **MUST** generate candidate IDs using `Hash16`.
- If multiple canonical keys produce the same ID, the SDK MUST upgrade the entire collision set to `HashN(..., 24)`.
- If collisions persist, upgrade that collision set to `HashN(..., 32)`.
- If collisions persist at `HashN(..., 32)`, the SDK **MUST** fail the build with a deterministic error (e.g. `ErrIDCollision`) rather than emitting unstable IDs.
- Producers **MUST NOT** add randomness.

Two-phase resolution (required):
- Producers register canonical keys, not final IDs, during collection/build.
- The SDK resolves final IDs during `Build()` (or an explicit `Finalize()`), when the full set of keys is known.
- The SDK provides `IDRegistry` with a two-phase API:

```go
type IDRegistry struct {
    // internal: kind -> []canonicalKey
}

func NewIDRegistry() *IDRegistry

// RegisterKey records a canonical key for later deterministic resolution.
func (r *IDRegistry) RegisterKey(kind string, canonicalKey string)

// ResolveAll returns a stable mapping canonicalKey -> resolvedId for the given kind.
// Resolution is order-independent and uses the upgrade rules above.
func (r *IDRegistry) ResolveAll(kind string, buildID func(canonicalKey string, hashLen int) string) map[string]string

```


### 2.1.2 Connection ID helper
Implements the canonical key serialization `v1|{type}|{direction}|{sourceId}|{targetId}|{qualifiers}` from [OSIRIS-ADG-PR-1.0](https://github.com/osirisjson/osiris/tree/main/docs/guidelines/v1.0/OSIRIS-PRODUCER-GUIDELINES.md) section 2.2.2.

```go
// ConnectionIDInput carries the stable parts needed to derive a connection ID.
type ConnectionIDInput struct {
    Type       string            // connection type (e.g. "network", "dataflow.tcp")
    Direction  string            // "bidirectional" | "forward" | "reverse"
    Source     string            // full resource ID
    Target     string            // full resource ID
    Qualifiers map[string]string // optional stable qualifiers (port, protocol, etc.)
}

// ConnectionCanonicalKey returns the canonical key for a connection.
// For bidirectional connections, endpoints are canonicalized (sorted) before key construction.
func ConnectionCanonicalKey(in ConnectionIDInput) string

// BuildConnectionID formats the final connection ID from a canonical key and chosen hash length.
// Output format: conn-{type}-{hintA}-to-{hintB}-{hash}
func BuildConnectionID(canonicalKey string, hashLen int) string
```

- For `direction != "bidirectional"`, the helper **MUST NOT** reorder endpoints; the producer-provided (`Source`, `Target`) order is preserved.
- For `direction = "bidirectional"`, the helper **MUST** canonicalize (`Source`, `Target`) by sorting the two resource IDs lexicographically and then use that canonical order both for:
  - canonical key serialization (hash input)
  - hint derivation + final ID formatting (`{hintA}-to-{hintB}`),
  ensuring the same connection yields the same ID even if the producer provides endpoints swapped.

Producers **MUST** register the canonical key with the document `IDRegistry` during collection/build.
Final IDs are materialized during `Build()` (or `Finalize()`) using `IDRegistry.ResolveAll(...)` with `BuildConnectionID` as the `buildID` callback.


### 2.1.3 Group ID helper
Implements the canonical key serialization `v1|{type}|boundary={boundaryToken}|{scopePairs}` from [OSIRIS-ADG-PR-1.0](https://github.com/osirisjson/osiris/tree/main/docs/guidelines/v1.0/OSIRIS-PRODUCER-GUIDELINES.md) section 2.2.3.

```go
// GroupIDInput carries the stable boundary needed to derive a group ID.
type GroupIDInput struct {
    Type           string            // group type (e.g. "physical.site", "logical.environment")
    BoundaryToken  string            // stable token (site code, region, rack id, etc.)
    ScopeFields    map[string]string // provider scope pairs (name, account, region, etc.)
}

// GroupID returns the deterministic ID: group-{type}-{boundaryHint}-{hash16}
func GroupID(in GroupIDInput) string
```

Membership (`members`, `children`) is excluded from the canonical key; group IDs remain stable even when membership changes.


### 2.1.4 Hint derivation
Both ID helpers use a shared `DeriveHint` function implementing the normative rule from [OSIRIS-ADG-PR-1.0](https://github.com/osirisjson/osiris/tree/main/docs/guidelines/v1.0/OSIRIS-PRODUCER-GUIDELINES.md) section 2.2.2: take substring after last `::` (or `/`), lowercase, replace non-`[a-z0-9]` with `-`, trim, truncate to 24 chars. If empty, fall back to the first 8 characters of the hash.

---

## 2.2 Resource factory patterns
Producers construct resources through a factory function that enforces required fields and sensible defaults. The factory returns a `Resource` struct; optional fields are set via functional options or direct field assignment.

```go
// NewResource creates a Resource with required fields pre-populated.
// Optional fields (Name, Description, Status, Properties, Extensions, Tags) are set directly on the returned struct.
func NewResource(id, resourceType string, provider Provider) Resource
```

**Provider helper:**

```go
// NewProvider creates a Provider with the required name field.
func NewProvider(name string) Provider

// NewCustomProvider creates a Provider with name="custom" and the required namespace.
func NewCustomProvider(namespace string) Provider
```

`NewCustomProvider` enforces the required namespace when `provider.name = "custom"`: it **MUST** follow the `osiris.<identifier>` pattern using reverse-domain notation (e.g. `osiris.com.acme`), preventing a common Level 1 validation failure.

Factories enforce compile-time safety for required fields (`id`, `type`, `provider` for resources; `id`, `type`, `source`, `target` for connections; `id`, `type` for groups). Optional fields are plain struct fields, not hidden behind builder chains, keeping the API idiomatic for Go.

---

## 2.3 Document builder (metadata + topology assembly)
The `DocumentBuilder` assembles the top-level OSIRIS document structure: `version`, `metadata` and `topology`. It enforces structural invariants and produces deterministic, diff-friendly output.

Example for Hyperscaler Microsoft Azure:

```go
builder := sdk.NewDocumentBuilder(ctx).
    WithGenerator("osirisjson-producer-azure", "1.0.0").
    WithScope(sdk.Scope{
        Providers: []string{"azure"},
        Accounts:  []string{"sub-abc-123"},
        Regions:   []string{"westeurope"},
    })

// Add topology elements
builder.AddResource(res1)
builder.AddResource(res2)
builder.AddConnection(conn1)
builder.AddGroup(grp1)

// Build emits the document with sorted arrays and populated metadata
doc, err := builder.Build()
```

**Builder invariants:**
- `version` and `$schema` are derived from SDK constants (e.g. `sdk.SpecVersion`, `sdk.SchemaURI`) so producers don’t hardcode strings in multiple places (currently `1.0.0` and `https://osirisjson.org/schema/v1.0/osiris.schema.json`).
- `metadata.timestamp` is set from `ctx.Clock()` at `Build()` time.
- `metadata.generator.name` and `metadata.generator.version` are required; `Build()` returns an error if unset.
- Redaction metadata is owned by `Build()`:
  - If `ctx.Config.SafeFailureMode == "log-and-redact"`, `Build()` **MUST** set `metadata.redacted: true` and `metadata.redaction_policy: "log-and-redact"`.
  - Producers **MUST** ensure secret values are already replaced with a stable placeholder (e.g. `"REDACTED"`) before calling `Build()`.
  - For `fail-closed`, `Build()` **MUST NOT** redact values; producers skip the affected resource or fail the run before emission.
- `topology.resources`, `topology.connections` and `topology.groups` are sorted by `id` before serialization (deterministic diffs).
- Group internals are made deterministic: `members` and `children` are deduplicated and sorted lexicographically before serialization.
- `Build()` is pure, but the producer pipeline **MUST** run canonical validation (CLI) before emitting/publishing artifacts.

---

## 2.4 Relationship builders (connections and groups)
Connection and group construction mirrors the resource factory pattern: required fields at construction, optional fields via direct assignment.

```go
// NewConnection creates a Connection with required fields. Direction defaults to "bidirectional" when empty.
func NewConnection(id, connType, source, target string) Connection

// NewGroup creates a Group with required fields.
func NewGroup(id, groupType string) Group
```

**Group membership helpers:**

```go
// AddMembers appends resource IDs to the group's members slice (deduplicates).
func (g *Group) AddMembers(resourceIDs ...string)

// AddChildren appends child group IDs to the group's children slice (deduplicates).
func (g *Group) AddChildren(groupIDs ...string)
```

`AddMembers` and `AddChildren` deduplicate entries at insertion time and keep slices deterministic (sorted lexicographically) to prevent duplicate member warnings at Level 2 validation and to keep golden-file diffs stable. Producers are still responsible for ensuring that referenced IDs correspond to actual resources or groups in the document.

---

## 2.5 Normalization utilities
Normalization converts vendor-native representations into **canonical OSIRIS-friendly forms** (units, timestamps, tokens) without inventing meaning. This implements the producer “Normalization” step from [OSIRIS-ADG-PR-1.0](https://github.com/osirisjson/osiris/tree/main/docs/guidelines/v1.0/OSIRIS-PRODUCER-GUIDELINES.md) chapter 2 and keeps output stable across runs.

All normalization helpers are **pure functions** with no I/O and are intended to be shared across producers so that the same input produces the same canonical output.


### 2.5.1 Timestamp normalization
Producers **MUST** emit timestamps in [RFC3339](https://datatracker.ietf.org/doc/html/rfc3339) (UTC) when a timestamp is required or recommended.

```go
// NormalizeRFC3339UTC returns an RFC3339 string in UTC (e.g. "2026-01-15T10:00:00Z").
func NormalizeRFC3339UTC(t time.Time) string
```


### 2.5.2 Token normalization
Producers **SHOULD** normalize vendor tokens used in stable fields (scope fields, boundary tokens, tags, names) to prevent diff churn and canonical-key drift.

```go
// NormalizeToken returns a stable token form:
// - trim surrounding whitespace
// - lowercase
// - collapse internal whitespace to single `-`
// - remove leading/trailing `-`
func NormalizeToken(s string) string
```


### 2.5.3 Common network normalizers
When producers map common network identifiers, they **SHOULD** normalize formatting to a canonical form.

```go
// NormalizeMAC returns lowercase colon-separated MAC (e.g. "aa:bb:cc:dd:ee:ff") or "" if invalid.
func NormalizeMAC(s string) string

// NormalizeIP returns the canonical string form (IPv4 dotted quad / IPv6 compressed) or "" if invalid.
func NormalizeIP(s string) string
```

> [!NOTE]
> These helpers normalize representation only. If a value is ambiguous or unavailable, producers **MUST** omit the optional field rather than guessing.