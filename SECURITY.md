# Security policy

## Supported versions

| Version | Supported |
|---------|-----------|
| latest  | Yes       |
| < latest | No       |

Only the most recent tagged release receives security fixes.
Older versions should be upgraded rather than patched.

---

## Reporting a Vulnerability

**Please do not open a public GitHub issue for security vulnerabilities.**

Use one of the following private channels:

1. **GitHub private security advisory (preferred)**
   Navigate to the repository on GitHub and open a [private security advisory](https://github.com/osirisjson/osiris/security/advisories/new).
   This keeps the report confidential until a fix is published.

2. **Email**
   Send a report to [tiazanella@osirisjson.org](mailto:tiazanella@osirisjson.org).
   Encrypt sensitive reports with the maintainer's public key if available.


### What to include

A useful report contains:

- A clear description of the vulnerability and its potential impact
- The affected package(s) and version(s)
- Step-by-step reproduction instructions or a minimal proof-of-concept
- Affected versions and packages (e.g., `pkg/sdk`, `osiris/network/cisco/apic`)
- Any relevant log output, stack traces, or example payloads
- Your suggested severity (Critical / High / Medium / Low / Informational)

### Response timeline

| Event | Target |
|---|---|
| Acknowledgement | Within 3 business days |
| Initial assessment | Within 7 business days |
| Fix or mitigation | Depends on severity - critical issues are prioritised immediately |
| Public disclosure | Coordinated with the reporter; typically after a fix is released |

If you have not received an acknowledgement within 3 business days, follow up by email at [tiazanella@osirisjson.org](mailto:tiazanella@osirisjson.org).

---

## Disclosure policy

This project follows **coordinated (responsible) disclosure**:

1. The reporter submits a private report.
2. Maintainers confirm the issue and work on a fix.
3. A patched release is published.
4. A GitHub Security Advisory (CVE if applicable) is published simultaneously.
5. The reporter is credited unless they request anonymity.

---

## Scope

This policy covers the `go.osirisjson.org/producers` Go module, including:

- `pkg/sdk` - OSIRIS producer SDK
- `pkg/testharness` - test utilities
- `osiris/` - all producer packages
- `cmd/` - CLI binaries

---

## Security Best Practices for Contributors

### Credentials and Secrets

- **Never** commit credentials, API keys, tokens, or certificates to the repository.
- Use environment variables or external secret stores to provide credentials at runtime.
- Producers that accept passwords interactively must use `golang.org/x/term` to
  disable terminal echo. **Never** log or print credentials.

### Network and TLS

- All HTTP clients **MUST** enforce TLS 1.2 as the minimum version.
- Do not set `InsecureSkipVerify: true` in production code.
  If needed for development, gate it behind an explicit `--insecure` flag
  and print a warning to stderr.
- Set reasonable timeouts on all HTTP clients and transports to prevent
  resource exhaustion.
- Validate and sanitize all URLs and hostnames before use.

### Input validation

- Validate all data received from vendor APIs before processing.
  Do not trust external payloads to conform to expected schemas.
- Sanitize strings used in file paths, log messages, and identifiers
  to prevent injection or path traversal.
- Use the SDK normalization functions (`NormalizeToken`, `DeriveHint`)
  for all user-facing identifiers.

### Dependencies

- Keep dependencies minimal - `pkg/sdk` must remain stdlib-only.
- Pin dependency versions in `go.mod` and review changes in `go.sum`
  before committing.
- Run `go mod verify` in CI to detect tampered module downloads.
- Monitor dependencies for known vulnerabilities with `govulncheck`.

### Code review

- All changes **MUST** be submitted via pull request and reviewed before merge.
- Security-sensitive changes (authentication, TLS configuration, credential
  handling) require review from a maintainer.

### Build and release

- Release binaries are built from tagged commits on `main`.
- Use `go build -trimpath` and set version via `-ldflags` to avoid
  leaking local paths into binaries.
- Verify module integrity: consumers can run
  `go mod verify` after fetching the module.

---

## Out of scope

The following are **not** considered security vulnerabilities for this project:

- Denial-of-service via very large or deeply nested JSON documents (no size limits are enforced by design; callers are responsible for input size limits).
- Issues in go itself or third-party dependencies - report those upstream.
- Social engineering attacks on maintainers.
- Theoretical vulnerabilities without a working proof of concept.

---

## Security-Related contacts

| Role | Contact |
|---|---|
| Lead Maintainer (email) | Tia Zanella `@skhell` - [tiazanella@osirisjson.org](mailto:tiazanella@osirisjson.org) |
| Lead Maintainer (Matrix) | [@skhell:matrix.org](https://matrix.to/#/@skhell:matrix.org) |
| General community | [community@osirisjson.org](mailto:community@osirisjson.org) |
