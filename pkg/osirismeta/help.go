// help.go - Shared CLI help text for the --purpose flag.
//
// Every OSIRIS JSON producer exposes --purpose with identical semantics, so they
// share the exact same help block. Callers embed PurposeHelp() inside their
// own printHelp() so the wording stays consistent across producers.

package osirismeta

// PurposeHelp returns the shared help text for the --purpose flag.
// The text has no leading or trailing newlines so callers can place it
// inside their existing help layout. Lines are indented by two spaces to
// match the conventional flag-description indentation used in the per-producer help strings.
func PurposeHelp() string {
	return `  --purpose string      Output detail grade per OSIRIS JSON spec chapter 13.1.3. One of:
                          documentation (default) - Minimal fields: names, types,
                            IDs, provider traceability, high-level relationships.
                            Omits detailed IP addresses, serial numbers,
                            SKU subfields, per-rule NSG/ACL/firewall detail,
                            counters and other configuration specifics.
                            Suited for visualization and high-level docs.
                          audit - All readable fields, after sensitive-field
                            redaction. Suited for compliance, audit and
                            reconciliation use cases.
                          Secrets are always redacted regardless of purpose.`
}
