// redact_test.go - Tests for secret scanning (key name detection,
// value pattern matching, property/tag/resource/document scanning).
//
// For the OSIRIS JSON Producer SDK reference see [OSIRIS-ADG-PR-SDK-1.0].
//
// [OSIRIS-ADG-PR-SDK-1.0]: https://github.com/osirisjson/osiris-producers/blob/main/docs/guidelines/v1.0/OSIRIS-PRODUCER-SDK.md

package sdk

import (
	"fmt"
	"strings"
	"testing"
)

func TestIsSensitiveKey(t *testing.T) {
	tests := []struct {
		key  string
		want bool
	}{
		{"password", true},
		{"db_password", true},
		{"PASSWORD", true},
		{"api_key", true},
		{"API_KEY_ID", true},
		{"access_key", true},
		{"client_secret", true},
		{"auth_token", true},
		{"private_key_path", true},
		{"credential_file", true},
		{"hostname", false},
		{"memory_gb", false},
		{"serial_number", false},
		{"ip_address", false},
		{"description", false},
	}
	for _, tt := range tests {
		got := IsSensitiveKey(tt.key)
		if got != tt.want {
			t.Errorf("IsSensitiveKey(%q) = %v, want %v", tt.key, got, tt.want)
		}
	}
}

func TestScanValue(t *testing.T) {
	tests := []struct {
		value string
		want  string // expected rule name, "" for no match.
	}{
		// AWS access key.
		{"AKIAIOSFODNN7EXAMPLE", "aws_access_key"},
		// GitHub PAT.
		{"ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij", "github_pat"},
		// GitHub OAuth.
		{"gho_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij", "github_oauth"},
		// PEM private key.
		{"-----BEGIN RSA PRIVATE KEY-----", "pem_private_key"},
		{"-----BEGIN PRIVATE KEY-----", "pem_private_key"},
		// Bearer token.
		{"Bearer eyJhbGciOiJIUzI1NiJ9.test", "bearer_token"},
		// Basic auth: base64 of user-id:password, padded or not,
		// anywhere in the value.
		{"Authorization: Basic YWRtaW46Y2hhbmdlbWU=", "basic_auth"},
		{"Basic YWRtaW46dGVzdA", "basic_auth"},
		{"Basic Settings then Basic YWRtaW46dGVzdA", "basic_auth"},
		// Basic followed by prose, or by base64 that is not
		// user-id:password.
		{"cn=Cisco Basic Assurance Root CA 2099,o=Cisco", ""},
		{"Basic Configuration Template", ""},
		{"Basic ABCDefghij", ""},
		// An AWS access key after "Basic " is reported by the
		// earlier aws_access_key rule.
		{"Basic AKIAIOSFODNN7EXAMPLE", "aws_access_key"},
		// JWT.
		{"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.signature", "jwt"},
		// Slack token.
		{"xoxb-123456789-abcdefgh", "slack_token"},
		// Credential URI.
		{"postgresql://admin:s3cret@db.prod.internal:5432/app", "credential_uri"},
		{"https://user:pass@example.com", "credential_uri"},
		// Safe values - no match.
		{"192.0.2.1", ""},
		{"compute.vm", ""},
		{"my-resource-name", ""},
		{"2026-01-15T10:00:00Z", ""},
		{"https://example.com/api", ""},
	}
	for _, tt := range tests {
		got := ScanValue(tt.value)
		if got != tt.want {
			t.Errorf("ScanValue(%q) = %q, want %q", tt.value, got, tt.want)
		}
	}
}

func TestScanProperties(t *testing.T) {
	m := map[string]any{
		"hostname":    "web-01",
		"memory_gb":   64,
		"db_password": "hunter2",
		"endpoint":    "https://user:pass@db.internal:5432",
	}
	findings := ScanProperties(m, "test")
	if len(findings) != 2 {
		t.Fatalf("expected 2 findings, got %d: %v", len(findings), findings)
	}

	// Check that findings reference the right paths.
	paths := map[string]bool{}
	for _, f := range findings {
		paths[f.Path] = true
	}
	if !paths["test.db_password"] {
		t.Error("expected finding for test.db_password")
	}
	if !paths["test.endpoint"] {
		t.Error("expected finding for test.endpoint")
	}
}

func TestScanPropertiesNested(t *testing.T) {
	m := map[string]any{
		"config": map[string]any{
			"api_key": "some-key",
		},
	}
	findings := ScanProperties(m, "test")
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %v", len(findings), findings)
	}
	if findings[0].Path != "test.config.api_key" {
		t.Errorf("Path = %q, want %q", findings[0].Path, "test.config.api_key")
	}
}

func TestScanTags(t *testing.T) {
	m := map[string]string{
		"env":      "production",
		"password": "oops",
	}
	findings := ScanTags(m, "test")
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %v", len(findings), findings)
	}
	if findings[0].Path != "test.password" {
		t.Errorf("Path = %q, want %q", findings[0].Path, "test.password")
	}
}

func TestScanResource(t *testing.T) {
	r := Resource{
		ID:   "test-resource",
		Type: "compute.vm",
		Properties: map[string]any{
			"hostname": "web-01",
		},
		Extensions: map[string]any{
			"osiris.vendor": map[string]any{
				"secret_key": "AKIAIOSFODNN7EXAMPLE",
			},
		},
		Tags: map[string]string{
			"env": "prod",
		},
	}
	findings := ScanResource(&r, 0)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %v", len(findings), findings)
	}
	if findings[0].Rule != "key_name:secret" {
		t.Errorf("Rule = %q, want key_name:secret", findings[0].Rule)
	}
}

func TestScanDocumentClean(t *testing.T) {
	doc := &Document{
		Topology: Topology{
			Resources: []Resource{
				{
					ID:   "res-1",
					Type: "compute.vm",
					Properties: map[string]any{
						"hostname":  "web-01",
						"memory_gb": 64,
					},
				},
			},
		},
	}
	findings := ScanDocument(doc)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for clean document, got %d: %v", len(findings), findings)
	}
}

func TestScanDocumentWithSecrets(t *testing.T) {
	doc := &Document{
		Topology: Topology{
			Resources: []Resource{
				{
					ID:   "res-1",
					Type: "compute.vm",
					Properties: map[string]any{
						"connection_string": "postgresql://admin:s3cret@db:5432/app",
					},
				},
			},
			Connections: []Connection{
				{
					ID:   "conn-1",
					Type: "network",
					Properties: map[string]any{
						"auth_token": "Bearer abc123",
					},
				},
			},
			Groups: []Group{
				{
					ID:   "grp-1",
					Type: "physical.site",
					Tags: map[string]string{
						"api_key": "should-not-be-here",
					},
				},
			},
		},
	}
	findings := ScanDocument(doc)
	if len(findings) != 3 {
		t.Fatalf("expected 3 findings, got %d: %v", len(findings), findings)
	}
}

func TestScanPropertiesEmpty(t *testing.T) {
	findings := ScanProperties(nil, "test")
	if findings != nil {
		t.Errorf("expected nil for empty map, got %v", findings)
	}
}

// typedFinding is a typed value as a producer might place it in an
// extension: a struct inside a slice, which a scan of map[string]any
// values alone would never reach.
type typedFinding struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// TestScanDocumentSerializedTree proves: the document scan covers the
// serialized form of every member, not only properties, extensions and
// tags maps of string values.
func TestScanDocumentSerializedTree(t *testing.T) {
	const bearer = "Bearer CANARYsdkTOKEN0001"
	tests := []struct {
		name string
		doc  Document
		path string
		rule string
	}{
		{"typed slice in extensions", Document{Topology: Topology{Resources: []Resource{{ID: "r", Type: "t",
			Extensions: map[string]any{"osiris.test": map[string]any{"records": []typedFinding{{Code: "R1", Message: bearer}}}}}}}},
			"topology.resources[0].extensions.osiris.test.records[0].message", "value_pattern:bearer_token"},
		{"slice of maps in properties", Document{Topology: Topology{Resources: []Resource{{ID: "r", Type: "t",
			Properties: map[string]any{"items": []any{map[string]any{"api_key": "x"}}}}}}},
			"topology.resources[0].properties.items[0].api_key", "key_name:api_key"},
		{"resource name", Document{Topology: Topology{Resources: []Resource{{ID: "r", Type: "t", Name: bearer}}}},
			"topology.resources[0].name", "value_pattern:bearer_token"},
		{"group description", Document{Topology: Topology{Resources: []Resource{}, Groups: []Group{{ID: "g", Type: "t", Description: bearer}}}},
			"topology.groups[0].description", "value_pattern:bearer_token"},
		{"connection name", Document{Topology: Topology{Resources: []Resource{}, Connections: []Connection{{ID: "c", Type: "t", Source: "a", Target: "b", Name: bearer}}}},
			"topology.connections[0].name", "value_pattern:bearer_token"},
		{"provider field", Document{Topology: Topology{Resources: []Resource{{ID: "r", Type: "t", Provider: Provider{Name: "p", NativeID: bearer}}}}},
			"topology.resources[0].provider.native_id", "value_pattern:bearer_token"},
		{"metadata scope", Document{Metadata: Metadata{Scope: &Scope{Description: bearer}}, Topology: Topology{Resources: []Resource{}}},
			"metadata.scope.description", "value_pattern:bearer_token"},
		{"metadata extension key", Document{Metadata: Metadata{Extensions: map[string]any{"osiris.test": map[string]any{"token": "x"}}}, Topology: Topology{Resources: []Resource{}}},
			"metadata.osiris.test.token", "key_name:token"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings := ScanDocument(&tt.doc)
			if len(findings) != 1 || findings[0].Path != tt.path || findings[0].Rule != tt.rule {
				t.Fatalf("findings = %v, want one at %s (%s)", findings, tt.path, tt.rule)
			}
			if strings.Contains(findings[0].String(), "CANARY") {
				t.Error("finding echoes the secret value")
			}
		})
	}
}

// TestScanDocumentSchemaMembersNoKeyRule proves: schema-defined member
// names outside properties, extensions and tags are not subject to the
// key-name rule, so a clean document with every member set is clean.
func TestScanDocumentSchemaMembersNoKeyRule(t *testing.T) {
	doc := &Document{
		Metadata: Metadata{Generator: Generator{Name: "g", Version: "1"}, Scope: &Scope{Name: "s", Purpose: "audit"},
			Coverage: &CoverageBlock{Errors: []CoverageError{{Region: "r", Resource: "auth", ErrorMessage: "denied"}}}},
		Topology: Topology{Resources: []Resource{{ID: "r", Type: "t", Name: "n", Provider: Provider{Name: "p", Tenant: "t"}}}},
	}
	if findings := ScanDocument(doc); len(findings) != 0 {
		t.Fatalf("expected no findings, got %v", findings)
	}
}

// TestScanRedactedPlaceholder proves: a sensitive key whose value was
// already replaced (RedactedValue, empty or null) is not reported,
// while any other value under it still is.
func TestScanRedactedPlaceholder(t *testing.T) {
	m := map[string]any{
		"password":   RedactedValue,
		"secret_ref": "",
		"token":      nil,
		"nested":     []any{map[string]any{"api_key": RedactedValue}},
	}
	if findings := ScanProperties(m, "p"); len(findings) != 0 {
		t.Fatalf("expected no findings for redacted values, got %v", findings)
	}
	m["auth"] = map[string]any{"user": "u", "pw": "x"}
	findings := ScanProperties(m, "p")
	if len(findings) != 1 || findings[0].Path != "p.auth" {
		t.Fatalf("expected one finding at p.auth, got %v", findings)
	}
}

// TestScanUnscannableFailsClosed proves: a value that cannot be
// serialized is reported rather than silently skipped.
func TestScanUnscannableFailsClosed(t *testing.T) {
	doc := &Document{Topology: Topology{Resources: []Resource{{ID: "r", Type: "t",
		Properties: map[string]any{"bad": make(chan int)}}}}}
	findings := ScanDocument(doc)
	if len(findings) != 1 || findings[0].Rule != unscannableRule {
		t.Fatalf("expected one unscannable finding, got %v", findings)
	}
}

// TestScanDocumentDeterministicOrder proves: findings are reported in
// a stable order across runs.
func TestScanDocumentDeterministicOrder(t *testing.T) {
	doc := &Document{Topology: Topology{Resources: []Resource{{ID: "r", Type: "t",
		Properties: map[string]any{"z_token": "a", "a_secret": "b", "m_password": "c"}}}}}
	first := fmt.Sprint(ScanDocument(doc))
	for range 20 {
		if got := fmt.Sprint(ScanDocument(doc)); got != first {
			t.Fatalf("order changed: %s vs %s", got, first)
		}
	}
}

// TestScanConnectionAndGroup proves: the per-element scanners cover
// top-level members and key names in properties, extensions and tags
// for connections and groups, as ScanResource does for resources.
func TestScanConnectionAndGroup(t *testing.T) {
	c := Connection{ID: "c", Type: "t", Source: "a", Target: "b", Description: "Bearer CANARYconnTOKEN01",
		Tags: map[string]string{"api_key": "x"}}
	if f := ScanConnection(&c, 3); len(f) != 2 || f[0].Path != "topology.connections[3].description" || f[1].Path != "topology.connections[3].tags.api_key" {
		t.Errorf("ScanConnection = %v", f)
	}
	g := Group{ID: "g", Type: "t", Name: "Bearer CANARYgroupTOKEN01",
		Extensions: map[string]any{"osiris.x": []any{map[string]any{"password": "x"}}}}
	if f := ScanGroup(&g, 1); len(f) != 2 || f[0].Path != "topology.groups[1].extensions.osiris.x[0].password" || f[1].Path != "topology.groups[1].name" {
		t.Errorf("ScanGroup = %v", f)
	}
	if f := ScanTags(nil, "p"); f != nil {
		t.Errorf("ScanTags(nil) = %v", f)
	}
}
