// marshal.go - JSON serialization for OSIRIS JSON documents.
// Produces deterministic, human-readable output (2-space indent, trailing newline)
// suitable for golden-file comparison and version control.
//
// For an introduction to OSIRIS JSON Producer Development Guidelines see:
// "[OSIRIS-PRODUCER-GUIDELINES]."
//
// [OSIRIS-PRODUCER-GUIDELINES]: https://osirisjson.org/en/docs/getting-started/osiris-producer-guidelines

package sdk

import (
	"bytes"
	"encoding/json"
)

// MarshalDocument serializes an OSIRIS document to JSON with 2-space indentation
// and a trailing newline, producing deterministic, diff-friendly output.
// HTML-safe escaping is disabled so that characters like <, >, & appear
// literally in the JSON output instead of as \u003c, \u003e, \u0026.
func MarshalDocument(doc *Document) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
