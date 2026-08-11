// templates_test.go - Tests for shared "template --generate" file
// generation: CSV/secrets-file content and the 0600-permission
// write-and-report helper.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package run

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCredentialFileTemplate(t *testing.T) {
	got := CredentialFileTemplate("192.0.2.1")
	if !strings.Contains(got, `"host": "192.0.2.1"`) {
		t.Errorf("template missing example host: %s", got)
	}
	if !strings.Contains(got, `"username": "user"`) || !strings.Contains(got, `"password": "changeme"`) {
		t.Errorf("template missing username/password placeholders: %s", got)
	}
}

func TestCredentialFileTemplate_IsInformative(t *testing.T) {
	got := CredentialFileTemplate("192.0.2.1")
	if !strings.Contains(got, "$comment") {
		t.Error("template missing explanatory $comment field")
	}
	// Points to the companion multi-host template rather than trying to
	// explain both shapes in one file.
	if !strings.Contains(got, "multihost") {
		t.Error("template comment should point to the companion multi-host template")
	}
}

func TestCredentialFileTemplate_CommentDoesNotBreakParsing(t *testing.T) {
	// The generated file must still load as a valid, usable
	// CredentialFile despite the extra "$comment" field
	// LoadCredentialFile (via encoding/json's default struct decoding)
	// must silently ignore any key it doesn't declare.
	got := CredentialFileTemplate("192.0.2.1")

	var cf CredentialFile
	if err := json.Unmarshal([]byte(got), &cf); err != nil {
		t.Fatalf("generated template is not valid JSON for CredentialFile: %v", err)
	}
	if cf.Host != "192.0.2.1" || cf.Username != "user" || cf.Password != "changeme" {
		t.Errorf("parsed = %+v, want host=192.0.2.1 username=user password=changeme", cf)
	}
}

func TestCredentialRulesFileTemplate_IsInformative(t *testing.T) {
	got := CredentialRulesFileTemplate("192.0.2.0/24", "198.51.100.10,198.51.100.11")
	if !strings.Contains(got, "$comment") {
		t.Error("template missing explanatory $comment field")
	}
	for _, want := range []string{"default", "rules", "hosts", "CIDR", "first match"} {
		if !strings.Contains(got, want) {
			t.Errorf("template comment missing mention of %q", want)
		}
	}
}

func TestCredentialRulesFileTemplate_UsesGenericPlaceholderCredentials(t *testing.T) {
	// Every username/password in the template must be the same
	// "user"/"changeme" placeholder used elsewhere the "hosts"
	// pattern is what distinguishes the rules, not a per-rule
	// credential name like "dc1-admin".
	got := CredentialRulesFileTemplate("192.0.2.0/24", "198.51.100.10,198.51.100.11")

	var cf CredentialFile
	if err := json.Unmarshal([]byte(got), &cf); err != nil {
		t.Fatalf("generated template is not valid JSON for CredentialFile: %v", err)
	}
	if cf.Default == nil || cf.Default.Username != "user" || cf.Default.Password != "changeme" {
		t.Errorf("Default = %+v, want user/changeme", cf.Default)
	}
	for i, rule := range cf.Rules {
		if rule.Username != "user" || rule.Password != "changeme" {
			t.Errorf("rule[%d] = %+v, want user/changeme", i, rule)
		}
	}
}

func TestCredentialRulesFileTemplate_ParsesAndResolves(t *testing.T) {
	got := CredentialRulesFileTemplate("192.0.2.0/24", "198.51.100.10,198.51.100.11")

	var cf CredentialFile
	if err := json.Unmarshal([]byte(got), &cf); err != nil {
		t.Fatalf("generated template is not valid JSON for CredentialFile: %v", err)
	}
	if cf.Default == nil {
		t.Fatal("expected Default to be set")
	}
	if len(cf.Rules) != 2 {
		t.Fatalf("expected 2 example rules, got %d", len(cf.Rules))
	}

	// The example rules should actually resolve against their own
	// documented example hosts, not just parse.
	u, p := cf.ResolveForHost("192.0.2.42")
	if u == "" || p == "" {
		t.Error("expected the first example rule's CIDR to match an address inside it")
	}
	u, p = cf.ResolveForHost("198.51.100.10")
	if u == "" || p == "" {
		t.Error("expected the second example rule's exact-match list to match its own address")
	}
	// An address outside both examples must fall back to Default.
	defaultUser, defaultPass := cf.ResolveForHost("203.0.113.1")
	if defaultUser != cf.Default.Username || defaultPass != cf.Default.Password {
		t.Errorf("unmatched host resolved to %q/%q, want Default %q/%q", defaultUser, defaultPass, cf.Default.Username, cf.Default.Password)
	}
}

func TestCredentialRulesFileTemplate_DomainHosts(t *testing.T) {
	// vmanage passes example.com-based FQDNs (RFC 2606) instead of RFC
	// 5737 IP/CIDR patterns, since its targets are controller hostnames
	// not bare IPs. Exact-string matching (hostMatches' non-CIDR
	// branch) must work identically for a domain name as for an IP.
	got := CredentialRulesFileTemplate("vmanage01.example.com", "vmanage02.example.com")

	var cf CredentialFile
	if err := json.Unmarshal([]byte(got), &cf); err != nil {
		t.Fatalf("generated template is not valid JSON for CredentialFile: %v", err)
	}

	u, p := cf.ResolveForHost("vmanage01.example.com")
	if u != "user" || p != "changeme" {
		t.Errorf("ResolveForHost(vmanage01.example.com) = %q/%q, want user/changeme", u, p)
	}
	u, p = cf.ResolveForHost("vmanage02.example.com")
	if u != "user" || p != "changeme" {
		t.Errorf("ResolveForHost(vmanage02.example.com) = %q/%q, want user/changeme", u, p)
	}
}

func TestWriteTemplateFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")

	if err := WriteTemplateFile(path, "test"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading written file: %v", err)
	}
	if string(data) != "test" {
		t.Errorf("content = %q, want %q", string(data), "test")
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0600 {
			t.Errorf("permissions = %04o, want 0600", perm)
		}
	}
}

func TestGenerateTemplates(t *testing.T) {
	dir := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)

	if err := GenerateTemplates("nxos", "192.0.2.1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	csvData, err := os.ReadFile("cisco-nxos-template.csv")
	if err != nil {
		t.Fatalf("CSV template not written: %v", err)
	}
	if !strings.Contains(string(csvData), "nxos") {
		t.Error("CSV template missing producer name")
	}

	credData, err := os.ReadFile("cisco-nxos-secrets.json")
	if err != nil {
		t.Fatalf("credentials template not written: %v", err)
	}
	if !strings.Contains(string(credData), "192.0.2.1") {
		t.Error("credentials template missing example host")
	}

	multihostData, err := os.ReadFile("cisco-nxos-secrets-multihost.json")
	if err != nil {
		t.Fatalf("multi-host credentials template not written: %v", err)
	}
	if !strings.Contains(string(multihostData), "rules") {
		t.Error("multi-host credentials template missing rules content")
	}
}
