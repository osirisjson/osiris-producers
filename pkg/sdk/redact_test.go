// redact_test.go - Tests for secret scanning (key name detection, value pattern matching,
// property/tag/resource/document scanning).
//
// For an introduction to OSIRIS JSON Producer Development Guidelines see:
// "[OSIRIS-PRODUCER-GUIDELINES]."
//
// [OSIRIS-PRODUCER-GUIDELINES]: https://osirisjson.org/en/docs/producers/getting-started

package sdk

import (
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
		// Basic auth.
		{"Basic dXNlcjpwYXNz", "basic_auth"},
		// JWT.
		{"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.signature", "jwt"},
		// Slack token.
		{"xoxb-123456789-abcdefgh", "slack_token"},
		// Credential URI.
		{"postgresql://admin:s3cret@db.prod.internal:5432/app", "credential_uri"},
		{"https://user:pass@example.com", "credential_uri"},
		// Safe values - no match.
		{"192.168.1.1", ""},
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
