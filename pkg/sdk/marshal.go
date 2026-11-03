// marshal.go - JSON serialization for OSIRIS JSON documents.
// Produces deterministic, human-readable output (2-space indent,
// trailing newline) suitable for golden-file comparison and
// version control.
//
// For the OSIRIS JSON Producer SDK reference see [OSIRIS-ADG-PR-SDK-1.0].
//
// [OSIRIS-ADG-PR-SDK-1.0]: https://github.com/osirisjson/osiris-producers/blob/main/docs/guidelines/v1.0/OSIRIS-PRODUCER-SDK.md

package sdk

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
)

// MarshalDocument serializes an OSIRIS JSON document to JSON with
// 2-space indentation and a trailing newline, producing deterministic,
// diff-friendly output.
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

// MarshalJSON encodes m with its standard members in declaration order,
// followed by each Extensions entry whose key is namespaced
// ("osiris.<ns>") as an additional member, in key order. HTML
// characters are not escaped, matching MarshalDocument.
func (m Metadata) MarshalJSON() ([]byte, error) {
	type plain Metadata
	base, err := encodeNoEscape(plain(m))
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(m.Extensions))
	for k := range m.Extensions {
		if strings.HasPrefix(k, "osiris.") {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return base, nil
	}
	sort.Strings(keys)
	var buf bytes.Buffer
	buf.Write(bytes.TrimSuffix(base, []byte("}")))
	for _, k := range keys {
		key, err := encodeNoEscape(k)
		if err != nil {
			return nil, err
		}
		val, err := encodeNoEscape(m.Extensions[k])
		if err != nil {
			return nil, err
		}
		buf.WriteByte(',')
		buf.Write(key)
		buf.WriteByte(':')
		buf.Write(val)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// UnmarshalJSON decodes m and collects namespaced additional members
// ("osiris.<ns>") into Extensions.
func (m *Metadata) UnmarshalJSON(data []byte) error {
	type plain Metadata
	if err := json.Unmarshal(data, (*plain)(m)); err != nil {
		return err
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return err
	}
	for k, raw := range all {
		if !strings.HasPrefix(k, "osiris.") {
			continue
		}
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
		if m.Extensions == nil {
			m.Extensions = map[string]any{}
		}
		m.Extensions[k] = v
	}
	return nil
}

// encodeNoEscape returns the compact JSON encoding of v without HTML
// escaping and without a trailing newline.
func encodeNoEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}
