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
	"encoding/base64"
	"fmt"
	"regexp"
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

// ScanProperties scans a properties or extensions map for secrets.
// It checks both key names and string values. Returns all findings.
// The path parameter is the JSON Pointer prefix
// (e.g. "topology.resources[0].properties").
func ScanProperties(m map[string]any, path string) []SecretFinding {
	if len(m) == 0 {
		return nil
	}
	var findings []SecretFinding
	for key, val := range m {
		fieldPath := path + "." + key

		// Check key name.
		if IsSensitiveKey(key) {
			findings = append(findings, SecretFinding{
				Path: fieldPath,
				Rule: "key_name:" + matchedKeyPattern(key),
			})
			continue // Key itself is sensitive; skip value scan to avoid logging the value.
		}

		// Check string values for secret patterns.
		if sv, ok := val.(string); ok {
			if rule := ScanValue(sv); rule != "" {
				findings = append(findings, SecretFinding{
					Path: fieldPath,
					Rule: "value_pattern:" + rule,
				})
			}
		}

		// Recurse into nested maps.
		if nested, ok := val.(map[string]any); ok {
			findings = append(findings, ScanProperties(nested, fieldPath)...)
		}
	}
	return findings
}

// ScanTags scans a tags map for secrets (both keys and values).
func ScanTags(m map[string]string, path string) []SecretFinding {
	if len(m) == 0 {
		return nil
	}
	var findings []SecretFinding
	for key, val := range m {
		fieldPath := path + "." + key

		if IsSensitiveKey(key) {
			findings = append(findings, SecretFinding{
				Path: fieldPath,
				Rule: "key_name:" + matchedKeyPattern(key),
			})
			continue
		}

		if rule := ScanValue(val); rule != "" {
			findings = append(findings, SecretFinding{
				Path: fieldPath,
				Rule: "value_pattern:" + rule,
			})
		}
	}
	return findings
}

// ScanResource scans a single resource for secrets in properties,
// extensions and tags.
func ScanResource(r *Resource, index int) []SecretFinding {
	prefix := fmt.Sprintf("topology.resources[%d]", index)
	var findings []SecretFinding
	findings = append(findings, ScanProperties(r.Properties, prefix+".properties")...)
	findings = append(findings, ScanProperties(r.Extensions, prefix+".extensions")...)
	findings = append(findings, ScanTags(r.Tags, prefix+".tags")...)
	return findings
}

// ScanConnection scans a single connection for secrets in properties,
// extensions and tags.
func ScanConnection(c *Connection, index int) []SecretFinding {
	prefix := fmt.Sprintf("topology.connections[%d]", index)
	var findings []SecretFinding
	findings = append(findings, ScanProperties(c.Properties, prefix+".properties")...)
	findings = append(findings, ScanProperties(c.Extensions, prefix+".extensions")...)
	findings = append(findings, ScanTags(c.Tags, prefix+".tags")...)
	return findings
}

// ScanGroup scans a single group for secrets in properties,
// extensions and tags.
func ScanGroup(g *Group, index int) []SecretFinding {
	prefix := fmt.Sprintf("topology.groups[%d]", index)
	var findings []SecretFinding
	findings = append(findings, ScanProperties(g.Properties, prefix+".properties")...)
	findings = append(findings, ScanProperties(g.Extensions, prefix+".extensions")...)
	findings = append(findings, ScanTags(g.Tags, prefix+".tags")...)
	return findings
}

// ScanDocument scans an entire OSIRIS JSON document for secrets.
// Returns all findings across resources, connections and groups.
func ScanDocument(doc *Document) []SecretFinding {
	var findings []SecretFinding
	for i := range doc.Topology.Resources {
		findings = append(findings, ScanResource(&doc.Topology.Resources[i], i)...)
	}
	for i := range doc.Topology.Connections {
		findings = append(findings, ScanConnection(&doc.Topology.Connections[i], i)...)
	}
	for i := range doc.Topology.Groups {
		findings = append(findings, ScanGroup(&doc.Topology.Groups[i], i)...)
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
