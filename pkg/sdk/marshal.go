// marshal.go - JSON serialization for OSIRIS documents.
// Produces deterministic, human-readable output (2-space indent, trailing newline)
// suitable for golden-file comparison and version control.
//
// For an introduction to OSIRIS JSON Producer Development Guidelines see:
// "[OSIRIS-PRODUCER-GUIDELINES]."
//
// [OSIRIS-PRODUCER-GUIDELINES]: https://osirisjson.org/en/docs/getting-started/osiris-producer-guidelines

package sdk

import (
	"encoding/json"
)

// MarshalDocument serializes an OSIRIS document to JSON with 2-space indentation
// and a trailing newline, producing deterministic, diff-friendly output.
func MarshalDocument(doc *Document) ([]byte, error) {
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	// Append trailing newline for POSIX compliance and clean diffs.
	data = append(data, '\n')
	return data, nil
}
