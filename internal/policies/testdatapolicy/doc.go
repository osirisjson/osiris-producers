// Package testdatapolicy implements the OSIRIS JSON Producers test data
// policy scanner.
//
// The scanner enumerates every file in the Git index, classifies each
// one as text or binary and evaluates every text file against three
// groups of rules, named as in the policy document:
//
//   - Address rules (policy section 3, identifiers.go).
//   - Context rules (policy section 4, context.go).
//   - Hyperscaler identity rules (policy section 5, hyperscalers.go
//     with hyperscalers_azure.go and hyperscalers_aws.go).
//
// Binary files must be reviewed and listed in the allowlist.
// Reviewed exceptions are read from the tracked allowlist file
// [AllowlistFile] (policy section 7, exceptions.go).
// Each entry matches one occurrence by fingerprint; stale, expired,
// duplicate or broad entries fail the check.
//
// The scanner is offline, uses only the standard library and never
// modifies files. The TestRepositoryDataPolicy test runs it over the
// whole repository as part of go test.
//
// For the policy itself see [OSIRIS-PRODUCER-TEST-DATA-POLICY.md].
//
// [OSIRIS-PRODUCER-TEST-DATA-POLICY.md]: https://github.com/osirisjson/osiris-producers/blob/main/docs/policies/OSIRIS-PRODUCER-TEST-DATA-POLICY.md
package testdatapolicy
