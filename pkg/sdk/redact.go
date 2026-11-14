// redact.go - Secret scanning for OSIRIS JSON documents.
// Detects sensitive data by key name patterns (password, token,
// api_key, etc.) and value patterns (AWS keys, GitHub PATs, PEM keys,
// JWTs, credential URIs).
// Used by DocumentBuilder to enforce safe failure modes per
// [OSIRIS-ADG-PR-1.0 section 3.1.2].
//
// For the OSIRIS JSON Producer SDK reference see [OSIRIS-ADG-PR-SDK-1.0].
//
// [OSIRIS-ADG-PR-1.0 section 3.1.2]: https://github.com/osirisjson/osiris/blob/main/docs/guidelines/v1.0/OSIRIS-PRODUCER-GUIDELINES.md#312-detection-patterns
// [OSIRIS-ADG-PR-SDK-1.0]: https://github.com/osirisjson/osiris-producers/blob/main/docs/guidelines/v1.0/OSIRIS-PRODUCER-SDK.md

package sdk

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// SecretFinding reports a detected secret at a specific field path.
// The finding MUST NOT contain the actual secret value - only the
// path and rule name.
type SecretFinding struct {
	Path string // JSON Pointer-style field path (e.g. "topology.resources[2].properties.api_key").
	Rule string // which rule matched (e.g. "key_name:password", "value_pattern:aws_access_key").
}

func (f SecretFinding) String() string {
	return fmt.Sprintf("secret detected at %s (rule: %s)", f.Path, f.Rule)
}

// SensitiveKeyPatterns are substrings that, when found in a property
// key (case-insensitive), indicate the value is likely a secret.
// Per OSIRIS-ADG-PR-1.0 section 3.1.2.
var SensitiveKeyPatterns = []string{
	"password",
	"secret",
	"token",
	"credential",
	"private_key",
	"api_key",
	"access_key",
	"client_secret",
	"auth",
}

// sensitiveValuePatterns are compiled regexps that match common secret
// formats. Per OSIRIS-ADG-PR-1.0 section 3.1.2. When confirm is set, a
// regexp match is reported only if confirm accepts the submatches of
// one of the matches, so a pattern that also matches ordinary prose can
// require the structure a real credential has.
var sensitiveValuePatterns = []struct {
	name    string
	re      *regexp.Regexp
	confirm func(submatch []string) bool
}{
	{"aws_access_key", regexp.MustCompile(`AKIA[0-9A-Z]{16}`), nil},
	{"github_pat", regexp.MustCompile(`ghp_[A-Za-z0-9]{36}`), nil},
	{"github_oauth", regexp.MustCompile(`gho_[A-Za-z0-9]{36}`), nil},
	{"pem_private_key", regexp.MustCompile(`-----BEGIN [A-Z ]* ?PRIVATE KEY-----`), nil},
	{"bearer_token", regexp.MustCompile(`Bearer [A-Za-z0-9\-._~+/]{8,}={0,2}`), nil},
	{"basic_auth", regexp.MustCompile(`Basic ([A-Za-z0-9+/]{8,}={0,2})`), isBasicCredential},
	{"jwt", regexp.MustCompile(`eyJ[A-Za-z0-9_-]*\.eyJ[A-Za-z0-9_-]*\.`), nil},
	{"slack_token", regexp.MustCompile(`xox[boaprs]-[A-Za-z0-9-]+`), nil},
	{"credential_uri", regexp.MustCompile(`://[^/\s]+:[^/\s]+@`), nil},
}

// IsSensitiveKey returns true if the key name matches a
// sensitive pattern (case-insensitive substring match).
func IsSensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	for _, pattern := range SensitiveKeyPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

// ScanValue checks a string value against known secret patterns.
// Returns the rule name that matched, or "" if no match.
func ScanValue(value string) string {
	for _, p := range sensitiveValuePatterns {
		if p.confirm == nil {
			if p.re.MatchString(value) {
				return p.name
			}
			continue
		}
		for _, m := range p.re.FindAllStringSubmatch(value, -1) {
			if p.confirm(m) {
				return p.name
			}
		}
	}
	return ""
}

// isBasicCredential reports whether the token after "Basic " is an HTTP
// Basic credential (RFC 7617): base64 that decodes to printable
// "user-id:password" text. Prose such as a certificate issuer named
// "Basic Assurance Root CA" matches the pattern but is not base64 of a
// user-id and password, and is not reported.
func isBasicCredential(m []string) bool {
	if len(m) < 2 {
		return false
	}
	token := m[1]
	decoded, err := base64.StdEncoding.DecodeString(token)
	if err != nil && !strings.Contains(token, "=") {
		decoded, err = base64.RawStdEncoding.DecodeString(token)
	}
	if err != nil || !utf8.Valid(decoded) || !strings.Contains(string(decoded), ":") {
		return false
	}
	for _, r := range string(decoded) {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

// RedactedValue is the placeholder a producer substitutes for a secret
// value before Build. A value under a sensitive key that is exactly
// RedactedValue, an empty string or null is treated as already
// redacted and is not reported, so a producer can keep a redacted
// field's key and shape without failing the scan.
const RedactedValue = "[REDACTED]"

// unscannableRule is reported when a value cannot be converted to its
// JSON form for scanning. Such a value cannot be proven secret-free, so
// it is reported as a finding and a fail-closed build aborts.
const unscannableRule = "unscannable:json"

// elementScopedKeys are the members of a resource, connection or group
// whose content is producer-defined. Inside them the key-name rule
// applies at every depth; elsewhere only the value rule applies, so
// schema-defined member names never match a sensitive key pattern.
var elementScopedKeys = map[string]bool{
	"properties": true,
	"extensions": true,
	"tags":       true,
}

// jsonTree returns the JSON form of v as decoded generic values
// (map[string]any, []any, string, json.Number, bool, nil). Scanning the
// JSON form rather than the Go value covers typed slices, structs and
// custom marshalers exactly as they are serialized.
func jsonTree(v any) (any, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var tree any
	if err := dec.Decode(&tree); err != nil {
		return nil, err
	}
	return tree, nil
}

// scanTree reports every secret found in a decoded JSON value. Every
// string value is checked against the value patterns. When keyRule is
// set, every object key at any depth is also checked against
// SensitiveKeyPatterns; a matching key is reported unless its value is
// already redacted (see [RedactedValue]), and the value under it is
// never inspected further so it is never echoed into a finding.
// Object keys are visited in sorted order so finding are deterministic.
func scanTree(v any, path string, keyRule bool) []SecretFinding {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var findings []SecretFinding
		for _, k := range keys {
			fieldPath := path + "." + k
			if keyRule && IsSensitiveKey(k) {
				if isRedacted(t[k]) {
					continue
				}
				findings = append(findings, SecretFinding{
					Path: fieldPath,
					Rule: "key_name:" + matchedKeyPattern(k),
				})
				continue
			}
			findings = append(findings, scanTree(t[k], fieldPath, keyRule)...)
		}
		return findings
	case []any:
		var findings []SecretFinding
		for i, item := range t {
			findings = append(findings, scanTree(item, fmt.Sprintf("%s[%d]", path, i), keyRule)...)
		}
		return findings
	case string:
		if rule := ScanValue(t); rule != "" {
			return []SecretFinding{{Path: path, Rule: "value_pattern:" + rule}}
		}
	}
	return nil
}

// isRedacted reports whether a decoded value under a sensitive key
// carries no secret: null, an empty string or exactly RedactedValue.
func isRedacted(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return t == "" || t == RedactedValue
	}
	return false
}

// scanValueTree converts v to its JSON form and scans it with scanTree.
// A value that cannot be converted is reported as one unscannable
// finding at path.
func scanValueTree(v any, path string, keyRule bool) []SecretFinding {
	tree, err := jsonTree(v)
	if err != nil {
		return []SecretFinding{{Path: path, Rule: unscannableRule}}
	}
	return scanTree(tree, path, keyRule)
}

// scanElement scans a decoded resource, connection or group object.
// Every member is value-scanned; properties, extensions and tags are
// also key-scanned at every depth.
func scanElement(elem any, prefix string) []SecretFinding {
	m, ok := elem.(map[string]any)
	if !ok {
		return scanTree(elem, prefix, false)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var findings []SecretFinding
	for _, k := range keys {
		findings = append(findings, scanTree(m[k], prefix+"."+k, elementScopedKeys[k])...)
	}
	return findings
}

// scanElementValue converts a resource, connection or group to its
// JSON form and scans it with scanElement.
func scanElementValue(v any, prefix string) []SecretFinding {
	tree, err := jsonTree(v)
	if err != nil {
		return []SecretFinding{{Path: prefix, Rule: unscannableRule}}
	}
	return scanElement(tree, prefix)
}

// ScanProperties scans a properties or extensions map for secrets.
// It checks key names and string values at every depth, including
// values nested in slices and typed values such as structs, as they
// will be serialized. Returns all findings. The path parameter is the
// JSON Pointer prefix (e.g. "topology.resources[0].properties").
func ScanProperties(m map[string]any, path string) []SecretFinding {
	if len(m) == 0 {
		return nil
	}
	return scanValueTree(m, path, true)
}

// ScanTags scans a tags map for secrets (both keys and values).
func ScanTags(m map[string]string, path string) []SecretFinding {
	if len(m) == 0 {
		return nil
	}
	return scanValueTree(m, path, true)
}

// ScanResource scans a single resource for secrets. Every serialized
// member is value-scanned (id, name, description, provider, status and
// so on); properties, extensions and tags are also key-scanned.
func ScanResource(r *Resource, index int) []SecretFinding {
	return scanElementValue(r, fmt.Sprintf("topology.resources[%d]", index))
}

// ScanConnection scans a single connection for secrets with the same
// coverage as [ScanResource].
func ScanConnection(c *Connection, index int) []SecretFinding {
	return scanElementValue(c, fmt.Sprintf("topology.connections[%d]", index))
}

// ScanGroup scans a single group for secrets with the same coverage as
// [ScanResource].
func ScanGroup(g *Group, index int) []SecretFinding {
	return scanElementValue(g, fmt.Sprintf("topology.groups[%d]", index))
}

// ScanDocument scans an entire OSIRIS JSON document for secrets, in
// the form it will be serialized. Every string value in the document
// is checked against the value patterns, including metadata (scope,
// coverage, namespaced metadata extensions) and the top-level members
// of every resource, connection and group. Key names are checked inside
// element properties, extensions and tags and inside namespaced
// ("osiris.<ns>") metadata members. A document that cannot be
// serialized is reported as one unscannable finding.
func ScanDocument(doc *Document) []SecretFinding {
	tree, err := jsonTree(doc)
	if err != nil {
		return []SecretFinding{{Path: "document", Rule: unscannableRule}}
	}
	root, ok := tree.(map[string]any)
	if !ok {
		return []SecretFinding{{Path: "document", Rule: unscannableRule}}
	}
	keys := make([]string, 0, len(root))
	for k := range root {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var findings []SecretFinding
	for _, k := range keys {
		switch k {
		case "metadata":
			findings = append(findings, scanMetadata(root[k])...)
		case "topology":
			findings = append(findings, scanTopology(root[k])...)
		default:
			findings = append(findings, scanTree(root[k], k, false)...)
		}
	}
	return findings
}

// scanMetadata scans the decoded metadata object. Namespaced members
// ("osiris.<ns>") are producer-defined and also key-scanned.
func scanMetadata(v any) []SecretFinding {
	m, ok := v.(map[string]any)
	if !ok {
		return scanTree(v, "metadata", false)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var findings []SecretFinding
	for _, k := range keys {
		findings = append(findings, scanTree(m[k], "metadata."+k, strings.HasPrefix(k, "osiris."))...)
	}
	return findings
}

// scanTopology scans the decoded topology object. Each element of the
// resources, connections and groups arrays is scanned with scanElement.
func scanTopology(v any) []SecretFinding {
	m, ok := v.(map[string]any)
	if !ok {
		return scanTree(v, "topology", false)
	}
	var findings []SecretFinding
	for _, k := range []string{"resources", "connections", "groups"} {
		items, ok := m[k].([]any)
		if !ok {
			findings = append(findings, scanTree(m[k], "topology."+k, false)...)
			continue
		}
		for i, item := range items {
			findings = append(findings, scanElement(item, fmt.Sprintf("topology.%s[%d]", k, i))...)
		}
	}
	for k, val := range m {
		if k != "resources" && k != "connections" && k != "groups" {
			findings = append(findings, scanTree(val, "topology."+k, false)...)
		}
	}
	return findings
}

// matchedKeyPattern returns which sensitive pattern a key matched.
func matchedKeyPattern(key string) string {
	lower := strings.ToLower(key)
	for _, pattern := range SensitiveKeyPatterns {
		if strings.Contains(lower, pattern) {
			return pattern
		}
	}
	return "unknown"
}
