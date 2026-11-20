// raw.go - --include-raw-body support and producer-side secret
// redaction for the Cisco Catalyst SD-WAN Manager OSIRIS JSON Producer.
//
// With --include-raw-body (accepted only together with --purpose audit)
// each collected API row is attached to the device resource that owns
// it under extensions["osiris.cisco.catalystsdwanmanager"], keyed by
// endpoint (raw, interfaces_raw, wan_interfaces_raw, omp_peers_raw,
// site_topology_raw), as a fallback for fields this producer does not
// model into OSIRIS JSON yet. Rows are never attached unmodified:
//
//   - every row is decoded and walked recursively (objects and arrays
//     at any depth); the value under any key that sdk.IsSensitiveKey
//     flags, or that is an SD-WAN credential attribute
//     (rawSensitiveKeys), and every string value sdk.ScanValue flags,
//     is replaced with sdk.RedactedValue;
//   - a row that is not valid JSON cannot be redacted and is dropped;
//   - one row is capped at maxRawRowBytes after redaction and all rows
//     of one document at maxRawTotalBytes.
//
// Every withheld row is counted in raw_omitted on its owning device
// resource, by endpoint and reason, so the fallback never fails
// silently.
//
// The same redaction walk backs --safe-failure-mode log-and-redact and
// off: redactElements replaces detected values in every
// producer-defined field (name, description, properties, extensions)
// before Build, so neither mode can emit a detected secret. The
// document secret scan still runs over the result.
//
// OSIRIS JSON Producer for Cisco Catalyst SD-WAN Manager introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification
package catalystsdwanmanager

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"go.osirisjson.org/producers/pkg/sdk"
)

const (
	// maxRawRowBytes caps the JSON size of one attached row.
	maxRawRowBytes = 256 << 10
	// maxRawTotalBytes caps the JSON size of all rows of one document.
	maxRawTotalBytes = 16 << 20
)

// Reasons a raw row is withheld, reported in raw_omitted.
const (
	rawOmitUnparseable = "unparseable"
	rawOmitTooLarge    = "row-too-large"
	rawOmitTotalLimit  = "total-limit"
	rawOmitUnowned     = "unowned" // no device resource of the document owns the row.
)

// rawSensitiveKeys are SD-WAN Manager field names (lower case) that can
// carry credential or key material but match no shared sensitive-key
// pattern: shared keys, pre-shared keys, SNMP communities and password
// hashes.
var rawSensitiveKeys = map[string]bool{
	"pwd":            true,
	"passwd":         true,
	"key":            true,
	"psk":            true,
	"pre-shared-key": true,
	"community":      true,
	"passphrase":     true,
	"hash":           true,
}

// sensitiveKey reports whether the value under key k must be redacted.
// Hyphenated SD-WAN names are also checked in their underscore form so
// they match the shared patterns (for example "auth-key").
func sensitiveKey(k string) bool {
	lower := strings.ToLower(k)
	return rawSensitiveKeys[lower] || sdk.IsSensitiveKey(strings.ReplaceAll(lower, "-", "_"))
}

// redactValue returns a copy of v (a decoded JSON value) with every
// value under a sensitive key and every secret-shaped string replaced
// with sdk.RedactedValue, recursing through objects and arrays. A null
// or empty value under a sensitive key is kept as is: it carries no
// secret. n counts the replacements.
func redactValue(v any, n *int) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if sensitiveKey(k) {
				if s, ok := val.(string); val == nil || (ok && (s == "" || s == sdk.RedactedValue)) {
					out[k] = val
					continue
				}
				out[k] = sdk.RedactedValue
				*n++
				continue
			}
			out[k] = redactValue(val, n)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = redactValue(val, n)
		}
		return out
	case []map[string]any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = redactValue(val, n)
		}
		return out
	case []string:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = redactValue(val, n)
		}
		return out
	case string:
		if sdk.ScanValue(t) != "" {
			*n++
			return sdk.RedactedValue
		}
		return t
	}
	return v
}

// decodeRaw decodes one raw row, keeping numbers exact.
func decodeRaw(raw json.RawMessage) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

// rawOmitKey identifies one raw_omitted counter.
type rawOmitKey struct {
	resourceID string
	endpoint   string
	reason     string
}

// rawCollector redacts and attaches raw rows for one document and
// accounts for every row it withholds. A nil *rawCollector attaches
// nothing, so callers can use it unconditionally.
type rawCollector struct {
	total   int
	omitted map[rawOmitKey]int
}

// newRawCollector returns a collector when enabled, otherwise nil.
func newRawCollector(enabled bool) *rawCollector {
	if !enabled {
		return nil
	}
	return &rawCollector{omitted: map[rawOmitKey]int{}}
}

// redactRow decodes, redacts and size-checks one row for endpoint,
// charging omissions to resourceID. ok is false when the row is
// withheld.
func (rc *rawCollector) redactRow(resourceID, endpoint string, raw json.RawMessage) (any, bool) {
	v, err := decodeRaw(raw)
	if err != nil {
		rc.omitted[rawOmitKey{resourceID, endpoint, rawOmitUnparseable}]++
		return nil, false
	}
	var n int
	red := redactValue(v, &n)
	b, err := json.Marshal(red)
	if err != nil {
		rc.omitted[rawOmitKey{resourceID, endpoint, rawOmitUnparseable}]++
		return nil, false
	}
	if len(b) > maxRawRowBytes {
		rc.omitted[rawOmitKey{resourceID, endpoint, rawOmitTooLarge}]++
		return nil, false
	}
	if rc.total+len(b) > maxRawTotalBytes {
		rc.omitted[rawOmitKey{resourceID, endpoint, rawOmitTotalLimit}]++
		return nil, false
	}
	rc.total += len(b)
	return red, true
}

// attachOne attaches a single redacted row under endpoint.
func (rc *rawCollector) attachOne(r *sdk.Resource, endpoint string, raw json.RawMessage) {
	if rc == nil {
		return
	}
	if red, ok := rc.redactRow(r.ID, endpoint, raw); ok {
		setExtension(r, endpoint, red)
	}
}

// attachList attaches the redacted rows that pass under endpoint,
// preserving their order. Nothing is attached when every row is
// withheld.
func (rc *rawCollector) attachList(r *sdk.Resource, endpoint string, raws []json.RawMessage) {
	if rc == nil || len(raws) == 0 {
		return
	}
	rows := make([]any, 0, len(raws))
	for _, raw := range raws {
		if red, ok := rc.redactRow(r.ID, endpoint, raw); ok {
			rows = append(rows, red)
		}
	}
	if len(rows) > 0 {
		setExtension(r, endpoint, rows)
	}
}

// omit counts n rows of endpoint withheld for reason, charged to the
// resource resourceID.
func (rc *rawCollector) omit(resourceID, endpoint, reason string, n int) {
	if rc == nil || n <= 0 {
		return
	}
	rc.omitted[rawOmitKey{resourceID, endpoint, reason}] += n
}

// finish records raw_omitted on every resource that had a row
// withheld, as a list of {endpoint, reason, count} ordered by endpoint
// then reason.
func (rc *rawCollector) finish(resources []sdk.Resource) {
	if rc == nil || len(rc.omitted) == 0 {
		return
	}
	byResource := map[string][]rawOmitKey{}
	for k := range rc.omitted {
		byResource[k.resourceID] = append(byResource[k.resourceID], k)
	}
	for i := range resources {
		keys := byResource[resources[i].ID]
		if len(keys) == 0 {
			continue
		}
		sort.Slice(keys, func(a, b int) bool {
			if keys[a].endpoint != keys[b].endpoint {
				return keys[a].endpoint < keys[b].endpoint
			}
			return keys[a].reason < keys[b].reason
		})
		list := make([]any, 0, len(keys))
		for _, k := range keys {
			list = append(list, map[string]any{"endpoint": k.endpoint, "reason": k.reason, "count": rc.omitted[k]})
		}
		setExtension(&resources[i], "raw_omitted", list)
	}
}

// redactString replaces s with sdk.RedactedValue when it matches a
// secret pattern.
func redactString(s string, n *int) string {
	if s != "" && sdk.ScanValue(s) != "" {
		*n++
		return sdk.RedactedValue
	}
	return s
}

// redactMap redacts a properties or extensions map in place of a copy.
func redactMap(m map[string]any, n *int) map[string]any {
	if m == nil {
		return nil
	}
	out, _ := redactValue(m, n).(map[string]any)
	return out
}

// verifyRedacted enforces the no-secret invariant for log-and-redact:
// sdk.DocumentBuilder.Build only logs findings in that mode and marks
// the document redacted, so a finding left after redactElements (in a
// field the producer does not redact, such as an ID) must not be
// written under a redacted=true claim. Findings are reported by path
// and rule only, never by value. fail-closed is already enforced by
// Build and off disables the check by request.
func verifyRedacted(doc *sdk.Document, mode string) error {
	if mode != sdk.LogAndRedact {
		return nil
	}
	findings := sdk.ScanDocument(doc)
	if len(findings) == 0 {
		return nil
	}
	paths := make([]string, len(findings))
	for i, f := range findings {
		paths[i] = f.Path + " (" + f.Rule + ")"
	}
	return fmt.Errorf("secret scanning detected %d unredacted finding(s) after redaction, aborting (log-and-redact): %s", len(findings), strings.Join(paths, "; "))
}

// redactElements replaces every detected secret in the
// producer-defined fields of resources, connections and groups (name,
// description, properties, extensions) with sdk.RedactedValue, and
// returns the number of replaced values. It backs
// --safe-failure-mode log-and-redact and off (see the file comment).
func redactElements(resources []sdk.Resource, connections []sdk.Connection, groups []sdk.Group) int {
	var n int
	for i := range resources {
		r := &resources[i]
		r.Name = redactString(r.Name, &n)
		r.Description = redactString(r.Description, &n)
		r.Properties = redactMap(r.Properties, &n)
		r.Extensions = redactMap(r.Extensions, &n)
	}
	for i := range connections {
		c := &connections[i]
		c.Name = redactString(c.Name, &n)
		c.Description = redactString(c.Description, &n)
		c.Properties = redactMap(c.Properties, &n)
		c.Extensions = redactMap(c.Extensions, &n)
	}
	for i := range groups {
		g := &groups[i]
		g.Name = redactString(g.Name, &n)
		g.Description = redactString(g.Description, &n)
		g.Properties = redactMap(g.Properties, &n)
		g.Extensions = redactMap(g.Extensions, &n)
	}
	return n
}
