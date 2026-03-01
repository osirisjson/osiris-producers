// redact.go - Secret scanning for OSIRIS documents.
// Detects sensitive data by key name patterns (password, token, api_key, etc.)
// and value patterns (AWS keys, GitHub PATs, PEM keys, JWTs, credential URIs).
// Used by DocumentBuilder to enforce safe failure modes per OSIRIS-ADG-PR-1.0 section 3.1.2.
//
// For an introduction to OSIRIS JSON Producer Development Guidelines see:
// "[OSIRIS-PRODUCER-GUIDELINES]."
//
// [OSIRIS-ADG-PR-1.0 section 3.1.2]: https://github.com/osirisjson/osiris/blob/main/docs/guidelines/v1.0/OSIRIS-PRODUCER-GUIDELINES.md#312-detection-patterns
// [OSIRIS-PRODUCER-GUIDELINES]: https://osirisjson.org/en/docs/developers/producers/welcome

package sdk

import (
	"fmt"
	"regexp"
	"strings"
)

// SecretFinding reports a detected secret at a specific field path.
// The finding MUST NOT contain the actual secret value - only the path and rule name.
type SecretFinding struct {
	Path string // JSON Pointer-style field path (e.g. "topology.resources[2].properties.api_key").
	Rule string // which rule matched (e.g. "key_name:password", "value_pattern:aws_access_key").
}

func (f SecretFinding) String() string {
	return fmt.Sprintf("secret detected at %s (rule: %s)", f.Path, f.Rule)
}

// SensitiveKeyPatterns are substrings that, when found in a property key
// (case-insensitive), indicate the value is likely a secret.
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

// sensitiveValuePatterns are compiled regexps that match common secret formats.
// Per OSIRIS-ADG-PR-1.0 section 3.1.2.
var sensitiveValuePatterns = []struct {
	name string
	re *regexp.Regexp
}{
	{"aws_access_key", regexp.MustCompile(`AKIA[0-9A-Z]{16}`)},
	{"github_pat", regexp.MustCompile(`ghp_[A-Za-z0-9]{36}`)},
	{"github_oauth", regexp.MustCompile(`gho_[A-Za-z0-9]{36}`)},
	{"pem_private_key", regexp.MustCompile(`-----BEGIN [A-Z ]* ?PRIVATE KEY-----`)},
	{"bearer_token", regexp.MustCompile(`Bearer [A-Za-z0-9\-._~+/]+=*`)},
	{"basic_auth", regexp.MustCompile(`Basic [A-Za-z0-9+/]+=*`)},
	{"jwt", regexp.MustCompile(`eyJ[A-Za-z0-9_-]*\.eyJ[A-Za-z0-9_-]*\.`)},
	{"slack_token", regexp.MustCompile(`xox[boaprs]-[A-Za-z0-9-]+`)},
	{"credential_uri", regexp.MustCompile(`://[^/\s]+:[^/\s]+@`)},
}

// IsSensitiveKey returns true if the key name matches a sensitive pattern
// (case-insensitive substring match).
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
		if p.re.MatchString(value) {
			return p.name
		}
	}
	return ""
}

// ScanProperties scans a properties or extensions map for secrets.
// It checks both key names and string values. Returns all findings.
// The path parameter is the JSON Pointer prefix (e.g. "topology.resources[0].properties").
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

// ScanResource scans a single resource for secrets in properties, extensions and tags.
func ScanResource(r *Resource, index int) []SecretFinding {
	prefix := fmt.Sprintf("topology.resources[%d]", index)
	var findings []SecretFinding
	findings = append(findings, ScanProperties(r.Properties, prefix+".properties")...)
	findings = append(findings, ScanProperties(r.Extensions, prefix+".extensions")...)
	findings = append(findings, ScanTags(r.Tags, prefix+".tags")...)
	return findings
}

// ScanConnection scans a single connection for secrets in properties, extensions and tags.
func ScanConnection(c *Connection, index int) []SecretFinding {
	prefix := fmt.Sprintf("topology.connections[%d]", index)
	var findings []SecretFinding
	findings = append(findings, ScanProperties(c.Properties, prefix+".properties")...)
	findings = append(findings, ScanProperties(c.Extensions, prefix+".extensions")...)
	findings = append(findings, ScanTags(c.Tags, prefix+".tags")...)
	return findings
}

// ScanGroup scans a single group for secrets in properties, extensions and tags.
func ScanGroup(g *Group, index int) []SecretFinding {
	prefix := fmt.Sprintf("topology.groups[%d]", index)
	var findings []SecretFinding
	findings = append(findings, ScanProperties(g.Properties, prefix+".properties")...)
	findings = append(findings, ScanProperties(g.Extensions, prefix+".extensions")...)
	findings = append(findings, ScanTags(g.Tags, prefix+".tags")...)
	return findings
}

// ScanDocument scans an entire OSIRIS document for secrets.
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
