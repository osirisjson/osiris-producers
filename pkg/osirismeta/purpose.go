// Package osirismeta carries cross-producer document-shaping helpers for the
// OSIRIS JSON producers: the --purpose flag plumbing, the scope value it
// populates and the projection that trims a document to the requested grade
// of detail per OSIRIS JSON spec chapter 13.1.3 (data minimization).
//
// The package is intentionally small and producer-agnostic so producers share
// a single implementation of the --purpose contract.
//
// For an introduction to OSIRIS JSON Producer Development Guidelines see:
// [OSIRIS-PRODUCER-GUIDELINES]: https://osirisjson.org/en/docs/getting-started/osiris-producer-guidelines
package osirismeta

import "fmt"

// Purpose is the declared use-case for an OSIRIS document.
// It drives how much detail the producer emits, per OSIRIS JSON spec chapter 13.1.3.
type Purpose string

const (
	// PurposeDocumentation is the default. Minimal fields only.
	PurposeDocumentation Purpose = "documentation"

	// PurposeAudit emits every readable field, after sensitive-field
	// redaction. Intended for compliance, audit and reconciliation.
	PurposeAudit Purpose = "audit"
)

// Default returns the default purpose used when no --purpose flag is set.
// Per OSIRIS JSON spec chapter 13.1.3, producers SHOULD include only the minimum data
// necessary for the intended use case.
func Default() Purpose { return PurposeDocumentation }

// ParsePurpose validates s against the allowed purpose values. An empty
// string resolves to Default(). Returns an error with the valid options
// on an unknown value.
func ParsePurpose(s string) (Purpose, error) {
	if s == "" {
		return Default(), nil
	}
	switch Purpose(s) {
	case PurposeDocumentation, PurposeAudit:
		return Purpose(s), nil
	default:
		return "", fmt.Errorf("invalid --purpose value %q: must be %q or %q",
			s, PurposeDocumentation, PurposeAudit)
	}
}

// String returns the canonical string form, suitable for metadata.scope.purpose.
func (p Purpose) String() string { return string(p) }
