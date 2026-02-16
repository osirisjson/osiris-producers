# OSIRIS JSON Architecture Producer SDK Internals<!-- omit in toc -->
| Field     | Value |
| --------- | ----- |
| Authors   | Tia Zanella [skhell](https://github.com/skhell) |
| Revision  | 1.0.0-DRAFT |
| Creation date      | 16 February 2026 |
| Last revision date | 16 February 2026 |
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
