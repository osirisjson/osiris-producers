// rawbody.go - Secret handling for the OSIRIS JSON Amazon AWS producer.
//
// This file owns every step that keeps secrets out of an emitted
// OSIRIS JSON document and out of producer diagnostics:
//
//   - Raw body capture ([attachRawBody], [applyRawBodies]): transforms
//     record the typed AWS SDK value they mapped, but nothing is
//     serialized until the document is assembled. The body is built
//     only for --purpose audit with --include-raw-body; otherwise it is
//     dropped unserialized. A built body is redacted ([redactTree]),
//     encoded with spaced separators ([encodeSpaced]) and checked once
//     more as a whole; a body that still matches a secret value pattern
//     is withheld and only the matching rule name is recorded.
//   - log-and-redact mode ([redactElements]): elements that the
//     document secret scanner flags have their properties, extensions
//     and tags replaced with redacted copies before the document is
//     built, so the mode redacts rather than only reporting.
//   - Publication gate ([checkPublishable]): every document, including
//     the empty fallback document, is scanned in its final serialized
//     form. A document with any residual finding is never written.
//   - Error text ([coverageErrorMessage], [logErr]): AWS API errors
//     are reduced to service, operation and error code, so request
//     IDs, principal ARNs and free-form API messages never reach
//     metadata.coverage or stderr.
//
// The raw body is stored as a JSON string so that SDK field names do
// not trip the document key-name rule; this file is therefore the
// only place its content is checked.
//
// For an introduction to OSIRIS JSON Producer for Amazon AWS see:
// [OSIRIS-JSON-AWS]: https://docs.osirisjson.org/osiris-producers/hyperscalers/amazon-aws/

package aws

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.osirisjson.org/producers/pkg/sdk"
)

// rawExtensionKey is the resource extension that carries the raw body.
const rawExtensionKey = "osiris.aws.sdk"

// rawSchema identifies the raw body envelope. v2 bodies are redacted
// and spaced; v1 bodies were unredacted compact JSON.
const rawSchema = "raw-passthrough/v2"

// rawSnapshot is the unserialized SDK value a transform attached. It
// has no exported fields, so if it ever reached a serializer it would
// encode as an empty object and never leak the wrapped value.
type rawSnapshot struct {
	body any
}

// awsDeniedRawKeys are AWS SDK field names, compared lower-case and
// exactly, whose whole value is withheld from raw bodies. They hold
// credentials, user-supplied scripts or configuration, or policy
// documents that the generic key patterns of [sdk.IsSensitiveKey]
// do not catch (for example "PreSharedKey" or "PrivateKey").
var awsDeniedRawKeys = map[string]bool{
	"userdata":                     true,
	"presharedkey":                 true,
	"privatekey":                   true,
	"accesskey":                    true,
	"accesskeyid":                  true,
	"apikey":                       true,
	"customergatewayconfiguration": true,
	"environment":                  true,
	"variables":                    true,
	"policy":                       true,
	"policydocument":               true,
	"assumerolepolicydocument":     true,
	"certificatebody":              true,
	"certificatechain":             true,
}

// isSensitiveRawKey reports whether the value under key is withheld.
func isSensitiveRawKey(key string) bool {
	return sdk.IsSensitiveKey(key) || awsDeniedRawKeys[strings.ToLower(key)]
}

// isRawSetting reports whether key and val are the EC2 instance
// metadata setting HttpTokens ("required" or "optional"): its name
// matches the token key pattern, but the value is an enum, not a
// credential. Any other value under that key is still withheld.
func isRawSetting(key string, val any) bool {
	return key == "HttpTokens" && (val == "required" || val == "optional")
}

// attachRawBody records body, the typed AWS SDK value a transform
// mapped, under extensions["osiris.aws.sdk"]. Nothing is serialized
// here; [applyRawBodies] decides at document assembly whether the body
// is built at all. Transforms call it unconditionally.
func attachRawBody(r *sdk.Resource, body any) {
	if body == nil {
		return
	}
	if r.Extensions == nil {
		r.Extensions = make(map[string]any)
	}
	r.Extensions[rawExtensionKey] = rawSnapshot{body: body}
}

// rawBodyStats counts the outcome of [applyRawBodies].
type rawBodyStats struct {
	Attached int // bodies emitted
	Redacted int // values replaced or elements dropped inside bodies
	Rejected int // bodies withheld after a residual secret match
	Dropped  int // bodies discarded because raw output was not requested
}

// log reports raw body statistics. Counts only, never content.
func (s rawBodyStats) log(l *slog.Logger) {
	if l == nil {
		return
	}
	l.Info("raw bodies processed",
		"attached", s.Attached,
		"redacted_values", s.Redacted,
		"rejected", s.Rejected,
	)
}

// applyRawBodies resolves every recorded raw snapshot in resources.
// When enabled is false the snapshot is removed without being
// serialized. When enabled is true it is replaced by the v2 envelope:
// a redacted, spaced JSON string under "body", or a "rejected" rule
// name when the redacted body still matched a secret value pattern.
// fetchedAt is the run clock, so equal inputs give equal output.
func applyRawBodies(resources []sdk.Resource, enabled bool, fetchedAt time.Time) rawBodyStats {
	var st rawBodyStats
	stamp := fetchedAt.UTC().Format(time.RFC3339)
	for i := range resources {
		ext := resources[i].Extensions
		snap, ok := ext[rawExtensionKey].(rawSnapshot)
		if !ok {
			continue
		}
		if !enabled {
			delete(ext, rawExtensionKey)
			if len(ext) == 0 {
				resources[i].Extensions = nil
			}
			st.Dropped++
			continue
		}
		env := map[string]any{
			"schema":      rawSchema,
			"api_version": "aws-sdk-go-v2",
			"kind":        "snapshot",
			"fetched_at":  stamp,
		}
		body, n, rule := encodeRawBody(snap.body)
		st.Redacted += n
		if rule != "" {
			env["rejected"] = rule
			st.Rejected++
		} else {
			env["body"] = body
			st.Attached++
		}
		ext[rawExtensionKey] = env
	}
	return st
}

// encodeRawBody serializes body, redacts it and encodes it with spaced
// separators. It returns the encoded body, the number of redactions and
// a non-empty rule name when the body must be withheld (it could not be
// serialized, or it still matches a secret value pattern as a whole).
func encodeRawBody(body any) (string, int, string) {
	data, err := json.Marshal(body)
	if err != nil {
		return "", 0, "unscannable:json"
	}
	tree, ok := decodeJSON(data)
	if !ok {
		return "", 0, "unscannable:json"
	}
	var n int
	tree = redactTree(tree, &n)
	out := encodeSpaced(tree)
	if rule := sdk.ScanValue(out); rule != "" {
		return "", n, "value_pattern:" + rule
	}
	return out, n, ""
}

// decodeJSON decodes data into generic values, keeping numbers exact.
func decodeJSON(data []byte) (any, bool) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, false
	}
	if dec.More() {
		return nil, false
	}
	return v, true
}

// redactTree returns v with secrets removed and adds the number of
// changes to n. In objects, a value under a sensitive key
// ([isSensitiveRawKey]) is replaced with [sdk.RedactedValue] unless it
// is a boolean, number, null or the setting of [isRawSetting]
// (configuration, not a secret). In
// arrays, an AWS tag element ({"Key": ..., "Value": ...}) whose key is
// sensitive is dropped, matching the tag filtering of the document.
// A string matching a secret value pattern is replaced; a string that
// holds JSON, directly or URL-encoded, is redacted recursively and
// re-encoded when anything inside it changed.
func redactTree(v any, n *int) any {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if isSensitiveRawKey(k) && !isRawSetting(k, val) {
				switch val.(type) {
				case nil, bool, json.Number:
				default:
					if s, ok := val.(string); ok && (s == "" || s == sdk.RedactedValue) {
						continue
					}
					t[k] = sdk.RedactedValue
					*n++
				}
				continue
			}
			t[k] = redactTree(val, n)
		}
		return t
	case []any:
		out := t[:0]
		for _, item := range t {
			if m, ok := item.(map[string]any); ok && isSensitiveTagElement(m) {
				*n++
				continue
			}
			out = append(out, redactTree(item, n))
		}
		return out
	case string:
		return redactString(t, n)
	}
	return v
}

// isSensitiveTagElement reports whether m is an AWS tag element whose
// key name is sensitive.
func isSensitiveTagElement(m map[string]any) bool {
	for _, kf := range []string{"Key", "key", "TagKey"} {
		if k, ok := m[kf].(string); ok && isSensitiveRawKey(k) {
			return true
		}
	}
	return false
}

// redactString applies the value rule to s and recurses into JSON
// embedded in s, either directly or URL-encoded (as IAM returns
// policy documents).
func redactString(s string, n *int) any {
	candidate := s
	if strings.Contains(s, "%7B") || strings.Contains(s, "%5B") {
		if dec, err := url.QueryUnescape(s); err == nil {
			candidate = dec
		}
	}
	if trimmed := strings.TrimSpace(candidate); trimmed != "" && (trimmed[0] == '{' || trimmed[0] == '[') {
		if inner, ok := decodeJSON([]byte(trimmed)); ok {
			before := *n
			inner = redactTree(inner, n)
			if *n == before {
				return s
			}
			return encodeSpaced(inner)
		}
	}
	if sdk.ScanValue(s) != "" || (candidate != s && sdk.ScanValue(candidate) != "") {
		*n++
		return sdk.RedactedValue
	}
	return s
}

// encodeSpaced encodes a decoded JSON value with a space after every
// comma and colon and object keys in sorted order. The value patterns
// of [sdk.ScanValue] stop at whitespace, so the spaces keep a whole-body
// match from spanning two unrelated JSON values (for example a URL in
// one field and an e-mail address in the next). A space, unlike a line
// break, is not escaped when the body is itself stored as a string.
func encodeSpaced(v any) string {
	var b bytes.Buffer
	writeSpaced(&b, v)
	return b.String()
}

func writeSpaced(b *bytes.Buffer, v any) {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteString(", ")
			}
			writeJSONString(b, k)
			b.WriteString(": ")
			writeSpaced(b, t[k])
		}
		b.WriteByte('}')
	case []any:
		b.WriteByte('[')
		for i, item := range t {
			if i > 0 {
				b.WriteString(", ")
			}
			writeSpaced(b, item)
		}
		b.WriteByte(']')
	case string:
		writeJSONString(b, t)
	case json.Number:
		b.WriteString(t.String())
	case bool:
		b.WriteString(strconv.FormatBool(t))
	default:
		b.WriteString("null")
	}
}

func writeJSONString(b *bytes.Buffer, s string) {
	enc, _ := json.Marshal(s)
	b.Write(enc)
}

// redactElements replaces secrets in every resource, connection and
// group that the document secret scanner flags: properties, extensions
// and tags are redacted with the raw body rules and a name or
// description matching a secret value pattern is replaced. It returns
// the number of changes. It is used in log-and-redact mode, where the
// document builder only logs findings and leaves replacement to the
// producer.
func redactElements(resources []sdk.Resource, conns []sdk.Connection, groups []sdk.Group) int {
	var n int
	for i := range resources {
		r := &resources[i]
		if len(sdk.ScanResource(r, i)) == 0 {
			continue
		}
		r.Properties = redactMap(r.Properties, &n)
		r.Extensions = redactMap(r.Extensions, &n)
		r.Tags = redactTagMap(r.Tags, &n)
		r.Name = redactPlain(r.Name, &n)
		r.Description = redactPlain(r.Description, &n)
	}
	for i := range conns {
		c := &conns[i]
		if len(sdk.ScanConnection(c, i)) == 0 {
			continue
		}
		c.Properties = redactMap(c.Properties, &n)
		c.Extensions = redactMap(c.Extensions, &n)
		c.Tags = redactTagMap(c.Tags, &n)
		c.Name = redactPlain(c.Name, &n)
		c.Description = redactPlain(c.Description, &n)
	}
	for i := range groups {
		g := &groups[i]
		if len(sdk.ScanGroup(g, i)) == 0 {
			continue
		}
		g.Properties = redactMap(g.Properties, &n)
		g.Extensions = redactMap(g.Extensions, &n)
		g.Tags = redactTagMap(g.Tags, &n)
		g.Name = redactPlain(g.Name, &n)
		g.Description = redactPlain(g.Description, &n)
	}
	return n
}

// redactMap returns a redacted generic copy of m. A map that cannot be
// serialized is withheld entirely.
func redactMap(m map[string]any, n *int) map[string]any {
	if len(m) == 0 {
		return m
	}
	data, err := json.Marshal(m)
	if err != nil {
		*n++
		return nil
	}
	tree, ok := decodeJSON(data)
	if !ok {
		*n++
		return nil
	}
	out, _ := redactTree(tree, n).(map[string]any)
	return out
}

// redactTagMap drops sensitive tag keys and replaces secret values.
func redactTagMap(m map[string]string, n *int) map[string]string {
	if len(m) == 0 {
		return m
	}
	out := sanitizeTags(m)
	if len(out) != len(m) {
		*n += len(m) - len(out)
	}
	for k, v := range out {
		if v == sdk.RedactedValue && m[k] != v {
			*n++
		}
	}
	return out
}

// redactPlain replaces s when it matches a secret value pattern.
func redactPlain(s string, n *int) string {
	if s != "" && sdk.ScanValue(s) != "" {
		*n++
		return sdk.RedactedValue
	}
	return s
}

// checkPublishable scans doc in its final serialized form, metadata
// included and returns an error naming each finding by path and rule
// (never by value) when any secret remains. Callers must not write a
// document for which it returns an error.
func checkPublishable(doc *sdk.Document) error {
	if doc == nil {
		return errors.New("no document to publish")
	}
	findings := sdk.ScanDocument(doc)
	if len(findings) == 0 {
		return nil
	}
	paths := make([]string, len(findings))
	for i, f := range findings {
		paths[i] = f.Path + " (" + f.Rule + ")"
	}
	return fmt.Errorf("final secret check found %d finding(s), document withheld: %s",
		len(findings), strings.Join(paths, "; "))
}

// awsOperationError and awsAPIError match the AWS SDK error types by
// behavior, so their codes can be read without importing them.
type awsOperationError interface {
	Service() string
	Operation() string
}

type awsAPIError interface {
	ErrorCode() string
}

// awsErrorCode returns "<service> <operation>: <code>" for an AWS SDK
// error, with whichever parts are present, or "" for any other error.
// Request IDs, principal ARNs and API messages are never included.
func awsErrorCode(err error) string {
	var parts []string
	var op awsOperationError
	if errors.As(err, &op) {
		parts = append(parts, strings.TrimSpace(op.Service()+" "+op.Operation()))
	}
	var api awsAPIError
	if errors.As(err, &api) && api.ErrorCode() != "" {
		parts = append(parts, api.ErrorCode())
	}
	return strings.Join(parts, ": ")
}

// awsAPICode returns the AWS API error code of err, or "".
func awsAPICode(err error) string {
	var api awsAPIError
	if errors.As(err, &api) {
		return api.ErrorCode()
	}
	return ""
}

// coverageErrorMessage returns the text recorded in
// metadata.coverage.errors for err: the AWS error code when there is
// one, otherwise a fixed category. Free-form error text never enters a
// document.
func coverageErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, errIdentityMismatch) {
		return "identity mismatch"
	}
	if errors.Is(err, errIdentityUnverified) {
		if code := awsErrorCode(err); code != "" {
			return "identity unverified: " + code
		}
		return "identity unverified"
	}
	if code := awsErrorCode(err); code != "" {
		return code
	}
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, errSessionExpired), isExpiredTokenError(err):
		return "session expired"
	case isPermissionDeniedError(err):
		return "access denied"
	}
	return "request failed"
}

// logErr returns the text logged to stderr for err: the AWS error code
// when there is one, otherwise the error text unless it matches a
// secret value pattern.
func logErr(err error) string {
	if err == nil {
		return ""
	}
	if code := awsErrorCode(err); code != "" {
		return code
	}
	msg := err.Error()
	if rule := sdk.ScanValue(msg); rule != "" {
		return "error text withheld (" + rule + ")"
	}
	return msg
}
