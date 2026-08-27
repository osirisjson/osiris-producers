// credentials_test.go - Tests for --secrets-file loading: valid
// files, rejected symlinks, rejected group/other-readable permissions,
// and missing files.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package run

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writeCredentialFile(t *testing.T, dir, name, content string, perm os.FileMode) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), perm); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadCredentialFile_Valid(t *testing.T) {
	dir := t.TempDir()
	path := writeCredentialFile(t, dir, "secrets-file-demo.json", `{"host":"192.0.2.1","username":"admin","password":"secret"}`, 0600)

	cf, err := LoadCredentialFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cf.Host != "192.0.2.1" || cf.Username != "admin" || cf.Password != "secret" {
		t.Errorf("unexpected contents: %+v", cf)
	}
}

func TestLoadCredentialFile_PartialFields(t *testing.T) {
	dir := t.TempDir()
	path := writeCredentialFile(t, dir, "secrets-file-demo.json", `{"password":"secret"}`, 0600)

	cf, err := LoadCredentialFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cf.Host != "" || cf.Username != "" || cf.Password != "secret" {
		t.Errorf("unexpected contents: %+v", cf)
	}
}

func TestLoadCredentialFile_Missing(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadCredentialFile(filepath.Join(dir, "nofile.json"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadCredentialFile_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := writeCredentialFile(t, dir, "secrets-file-demo.json", `not json`, 0600)

	_, err := LoadCredentialFile(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "invalid JSON") {
		t.Errorf("error = %q, want mention of invalid JSON", err.Error())
	}
}

func TestLoadCredentialFile_RejectsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on Windows")
	}
	dir := t.TempDir()
	real := writeCredentialFile(t, dir, "real-file.json", `{"password":"secret"}`, 0600)
	link := filepath.Join(dir, "link-file.json")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	_, err := LoadCredentialFile(link)
	if err == nil {
		t.Fatal("expected error for symlink")
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Errorf("error = %q, want mention of symlink", err.Error())
	}
}

func TestLoadCredentialFile_RejectsDirectory(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "adir")
	if err := os.Mkdir(sub, 0700); err != nil {
		t.Fatal(err)
	}

	_, err := LoadCredentialFile(sub)
	if err == nil {
		t.Fatal("expected error for directory")
	}
	if !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("error = %q, want mention of not a regular file", err.Error())
	}
}

func TestLoadCredentialFile_RejectsGroupReadable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits are not meaningful on Windows")
	}
	dir := t.TempDir()
	path := writeCredentialFile(t, dir, "secrets-file-demo.json", `{"password":"secret"}`, 0640)

	_, err := LoadCredentialFile(path)
	if err == nil {
		t.Fatal("expected error for group-readable file")
	}
	if !strings.Contains(err.Error(), "too open") {
		t.Errorf("error = %q, want mention of permissions being too open", err.Error())
	}
}

func TestResolveForHost_NilFile(t *testing.T) {
	var cf *CredentialFile
	u, p := cf.ResolveForHost("192.0.2.1")
	if u != "" || p != "" {
		t.Errorf("nil CredentialFile should resolve to empty, got %q/%q", u, p)
	}
}

func TestResolveForHost_FlatShapeIgnoresHost(t *testing.T) {
	cf := &CredentialFile{Username: "admin", Password: "secret"}
	for _, host := range []string{"192.0.2.1", "anything", ""} {
		u, p := cf.ResolveForHost(host)
		if u != "admin" || p != "secret" {
			t.Errorf("ResolveForHost(%q) = %q/%q, want admin/secret", host, u, p)
		}
	}
}

func TestResolveForHost_DefaultOnlyNoRulesArray(t *testing.T) {
	cf := &CredentialFile{
		Default: &CredentialEntry{Username: "admin", Password: "secret"},
	}
	for _, host := range []string{"192.0.2.1", "192.0.2.2", "192.0.2.3", "192.0.2.4"} {
		u, p := cf.ResolveForHost(host)
		if u != "admin" || p != "secret" {
			t.Errorf("ResolveForHost(%q) = %q/%q, want admin/secret (Default, no rules array present)", host, u, p)
		}
	}
}

func TestResolveForHost_DefaultOnlyEmptyRulesArray(t *testing.T) {
	// Same as above but with an explicit empty "rules": [] rather than
	// the key being absent entirely both must behave identically.
	cf := &CredentialFile{
		Default: &CredentialEntry{Username: "admin", Password: "secret"},
		Rules:   []CredentialRule{},
	}
	u, p := cf.ResolveForHost("192.0.2.1")
	if u != "admin" || p != "secret" {
		t.Errorf("ResolveForHost with empty Rules slice = %q/%q, want admin/secret", u, p)
	}
}

func TestResolveForHost_ExactMatch(t *testing.T) {
	cf := &CredentialFile{
		Rules: []CredentialRule{
			{Hosts: "192.0.2.10,192.0.2.11,192.0.2.12", CredentialEntry: CredentialEntry{Username: "dc2-admin", Password: "dc2-pass"}},
		},
	}
	u, p := cf.ResolveForHost("192.0.2.11")
	if u != "dc2-admin" || p != "dc2-pass" {
		t.Errorf("ResolveForHost(exact) = %q/%q, want dc2-admin/dc2-pass", u, p)
	}
}

func TestResolveForHost_ExactMatchCaseInsensitiveHostname(t *testing.T) {
	cf := &CredentialFile{
		Rules: []CredentialRule{
			{Hosts: "switch-edge-01.lab.local", CredentialEntry: CredentialEntry{Username: "edge-admin", Password: "edge-pass"}},
		},
	}
	u, p := cf.ResolveForHost("Switch-Edge-01.Lab.Local")
	if u != "edge-admin" || p != "edge-pass" {
		t.Errorf("ResolveForHost(case-insensitive hostname) = %q/%q, want edge-admin/edge-pass", u, p)
	}
}

func TestResolveForHost_CIDRMatch(t *testing.T) {
	cf := &CredentialFile{
		Rules: []CredentialRule{
			{Hosts: "192.0.2.0/24", CredentialEntry: CredentialEntry{Username: "dc1-admin", Password: "dc1-pass"}},
		},
	}
	u, p := cf.ResolveForHost("192.0.2.42")
	if u != "dc1-admin" || p != "dc1-pass" {
		t.Errorf("ResolveForHost(CIDR) = %q/%q, want dc1-admin/dc1-pass", u, p)
	}
}

func TestResolveForHost_CIDRDoesNotMatchOutsideRange(t *testing.T) {
	cf := &CredentialFile{
		Default: &CredentialEntry{Username: "admin", Password: "default-pass"},
		Rules: []CredentialRule{
			{Hosts: "192.0.2.0/24", CredentialEntry: CredentialEntry{Username: "dc1-admin", Password: "dc1-pass"}},
		},
	}
	u, p := cf.ResolveForHost("198.51.100.1")
	if u != "admin" || p != "default-pass" {
		t.Errorf("ResolveForHost(outside CIDR) = %q/%q, want fallback to default admin/default-pass", u, p)
	}
}

func TestResolveForHost_HostnameNeverMatchesCIDR(t *testing.T) {
	cf := &CredentialFile{
		Default: &CredentialEntry{Username: "admin", Password: "default-pass"},
		Rules: []CredentialRule{
			{Hosts: "192.0.2.0/24", CredentialEntry: CredentialEntry{Username: "dc1-admin", Password: "dc1-pass"}},
		},
	}
	u, p := cf.ResolveForHost("switch-01.lab.local")
	if u != "admin" || p != "default-pass" {
		t.Errorf("a hostname should never match a CIDR rule; got %q/%q, want default admin/default-pass", u, p)
	}
}

func TestResolveForHost_FirstMatchWins(t *testing.T) {
	cf := &CredentialFile{
		Rules: []CredentialRule{
			{Hosts: "192.0.2.0/24", CredentialEntry: CredentialEntry{Username: "broad", Password: "broad-pass"}},
			{Hosts: "192.0.2.42", CredentialEntry: CredentialEntry{Username: "specific", Password: "specific-pass"}},
		},
	}
	u, p := cf.ResolveForHost("192.0.2.42")
	if u != "broad" || p != "broad-pass" {
		t.Errorf("first matching rule should win regardless of specificity; got %q/%q, want broad/broad-pass", u, p)
	}
}

func TestResolveForHost_NoMatchNoDefaultIsEmpty(t *testing.T) {
	cf := &CredentialFile{
		Rules: []CredentialRule{
			{Hosts: "192.0.2.0/24", CredentialEntry: CredentialEntry{Username: "dc1-admin", Password: "dc1-pass"}},
		},
	}
	u, p := cf.ResolveForHost("198.51.100.1")
	if u != "" || p != "" {
		t.Errorf("no match and no default should resolve to empty, got %q/%q", u, p)
	}
}

func TestResolveForHost_RulesIgnoreTopLevelFlatFields(t *testing.T) {
	// A file mixing the flat shape's top-level fields with Rules is
	// documented as ignoring the flat fields entirely once Rules is
	// non-empty, so an unmatched host never silently falls back to
	// them instead of Default.
	cf := &CredentialFile{
		Username: "flat-user",
		Password: "flat-pass",
		Rules: []CredentialRule{
			{Hosts: "192.0.2.0/24", CredentialEntry: CredentialEntry{Username: "dc1-admin", Password: "dc1-pass"}},
		},
	}
	u, p := cf.ResolveForHost("198.51.100.1")
	if u != "" || p != "" {
		t.Errorf("top-level flat fields must be ignored when Rules is set; got %q/%q, want empty", u, p)
	}
}

func TestHostMatches_MalformedCIDRTokenSkipped(t *testing.T) {
	// A malformed CIDR token in the list should not abort matching the
	// remaining, well-formed tokens.
	if !hostMatches("192.0.2.10", "not-a-cidr/99,192.0.2.10") {
		t.Error("expected match against the valid token after skipping the malformed one")
	}
}

func TestLoadCredentialFile_RulesShapeJSON(t *testing.T) {
	dir := t.TempDir()
	content := `{
  "default": {"username": "admin", "password": "default-pass"},
  "rules": [
    {"hosts": "192.0.2.0/24", "username": "dc1-admin", "password": "dc1-pass"},
    {"hosts": "198.51.100.10,198.51.100.11,198.51.100.12", "username": "dc2-admin", "password": "dc2-pass"},
    {"hosts": "switch-edge-01.lab.local", "username": "edge-admin", "password": "edge-pass"}
  ]
}`
	path := writeCredentialFile(t, dir, "rules.json", content, 0600)

	cf, err := LoadCredentialFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		host     string
		wantUser string
		wantPass string
	}{
		{"192.0.2.42", "dc1-admin", "dc1-pass"},
		{"198.51.100.11", "dc2-admin", "dc2-pass"},
		{"switch-edge-01.lab.local", "edge-admin", "edge-pass"},
		{"203.0.113.9", "admin", "default-pass"},
	}
	for _, tt := range tests {
		u, p := cf.ResolveForHost(tt.host)
		if u != tt.wantUser || p != tt.wantPass {
			t.Errorf("ResolveForHost(%q) = %q/%q, want %q/%q", tt.host, u, p, tt.wantUser, tt.wantPass)
		}
	}
}

func TestLoadCredentialFile_DefaultOnlyJSON(t *testing.T) {
	dir := t.TempDir()
	content := `{"default": {"username": "admin", "password": "secret"}}`
	path := writeCredentialFile(t, dir, "default-only.json", content, 0600)

	cf, err := LoadCredentialFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, host := range []string{"192.0.2.1", "192.0.2.2", "192.0.2.3", "192.0.2.4"} {
		u, p := cf.ResolveForHost(host)
		if u != "admin" || p != "secret" {
			t.Errorf("ResolveForHost(%q) = %q/%q, want admin/secret", host, u, p)
		}
	}
}

func TestLoadCredentialFile_RejectsWorldReadable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits are not meaningful on Windows")
	}
	dir := t.TempDir()
	path := writeCredentialFile(t, dir, "secrets-file-demo.json", `{"password":"secret"}`, 0644)

	_, err := LoadCredentialFile(path)
	if err == nil {
		t.Fatal("expected error for world-readable file")
	}
}
