// flags_test.go - Tests for CLI flag parsing, including the
// customer-domain --host requirement (e.g. acme.sdwan.cisco.com) and
// the host/username/password fallback chain (flag, then --token-file,
// then an interactive prompt see flags.go's ParseFlags doc comment).
//
// OSIRIS JSON Producer for Cisco SD-WAN Manager (vManage) introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package vmanage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// promptHiddenStub returns a fixed password without touching a real
// terminal, standing in for promptHidden in tests that supply -h/-u
// directly and don't care about the resulting password value.
func promptHiddenStub(string) (string, error) {
	return "changeme", nil
}

func TestParseFlags_DomainHost(t *testing.T) {
	cfg, err := ParseFlags([]string{"-h", "acme.sdwan.cisco.com", "-u", "user"}, nil, promptHiddenStub)
	if err != nil {
		t.Fatalf("ParseFlags failed: %v", err)
	}
	if cfg.Host != "acme.sdwan.cisco.com" {
		t.Errorf("Host = %q, want %q", cfg.Host, "acme.sdwan.cisco.com")
	}
	if cfg.Port != defaultPort {
		t.Errorf("Port = %d, want default %d", cfg.Port, defaultPort)
	}
}

func TestParseFlags_DomainHostWithExplicitPort(t *testing.T) {
	cfg, err := ParseFlags([]string{"--host", "acme.sdwan.cisco.com:8443", "-u", "user"}, nil, promptHiddenStub)
	if err != nil {
		t.Fatalf("ParseFlags failed: %v", err)
	}
	if cfg.Host != "acme.sdwan.cisco.com" || cfg.Port != 8443 {
		t.Errorf("Host/Port = %q/%d, want acme.sdwan.cisco.com/8443", cfg.Host, cfg.Port)
	}
}

func TestParseFlags_MissingHost(t *testing.T) {
	// promptVisible is nil here (no controlling terminal available),
	// so a missing --host must be a hard error rather than hanging on
	// a prompt.
	if _, err := ParseFlags([]string{"-u", "user"}, nil, promptHiddenStub); err == nil {
		t.Fatal("expected error when --host is missing and no interactive prompt is available")
	}
}

func TestParseFlags_MissingUsername(t *testing.T) {
	if _, err := ParseFlags([]string{"-h", "acme.sdwan.cisco.com"}, nil, promptHiddenStub); err == nil {
		t.Fatal("expected error when --username is missing and no interactive prompt is available")
	}
}

func TestParseFlags_InvalidSafeFailureMode(t *testing.T) {
	args := []string{"-h", "acme.sdwan.cisco.com", "-u", "user", "--safe-failure-mode", "bogus"}
	if _, err := ParseFlags(args, nil, promptHiddenStub); err == nil {
		t.Fatal("expected error for invalid --safe-failure-mode")
	}
}

func TestParseFlags_InvalidPurpose(t *testing.T) {
	args := []string{"-h", "acme.sdwan.cisco.com", "-u", "user", "--purpose", "bogus"}
	if _, err := ParseFlags(args, nil, promptHiddenStub); err == nil {
		t.Fatal("expected error for invalid --purpose")
	}
}

func TestParseFlags_DefaultOutputDir(t *testing.T) {
	cfg, err := ParseFlags([]string{"-h", "acme.sdwan.cisco.com", "-u", "user"}, nil, promptHiddenStub)
	if err != nil {
		t.Fatalf("ParseFlags failed: %v", err)
	}
	if cfg.OutputDir != defaultOutputDir {
		t.Errorf("OutputDir = %q, want %q", cfg.OutputDir, defaultOutputDir)
	}
}

func TestParseFlags_SiteAllIsAliasForAllSites(t *testing.T) {
	for _, value := range []string{"all", "All", "ALL", " all "} {
		args := []string{"-h", "acme.sdwan.cisco.com", "-u", "user", "--site", value}
		cfg, err := ParseFlags(args, nil, promptHiddenStub)
		if err != nil {
			t.Fatalf("ParseFlags(--site %q) failed: %v", value, err)
		}
		if !cfg.AllSites {
			t.Errorf("--site %q: AllSites = false, want true", value)
		}
		if len(cfg.SiteFilter) != 0 {
			t.Errorf("--site %q: SiteFilter = %v, want empty", value, cfg.SiteFilter)
		}
	}
}

func TestParseFlags_SiteAllDoesNotConflictWithAllFlag(t *testing.T) {
	// --site all normalizes to AllSites before the --all/--site
	// mutual-exclusivity check, so it must not error even though both
	// end up "set".
	args := []string{"-h", "acme.sdwan.cisco.com", "-u", "user", "--site", "all", "--all"}
	if _, err := ParseFlags(args, nil, promptHiddenStub); err != nil {
		t.Errorf("ParseFlags(--site all --all) failed: %v", err)
	}
}

func TestParseFlags_DefaultSiteNameRateLimit(t *testing.T) {
	cfg, err := ParseFlags([]string{"-h", "acme.sdwan.cisco.com", "-u", "user"}, nil, promptHiddenStub)
	if err != nil {
		t.Fatalf("ParseFlags failed: %v", err)
	}
	if cfg.SiteNameRateLimit != defaultSiteNameRateLimit {
		t.Errorf("SiteNameRateLimit = %d, want default %d", cfg.SiteNameRateLimit, defaultSiteNameRateLimit)
	}
}

func TestParseFlags_CustomSiteNameRateLimit(t *testing.T) {
	args := []string{"-h", "acme.sdwan.cisco.com", "-u", "user", "--site-name-rate", "50"}
	cfg, err := ParseFlags(args, nil, promptHiddenStub)
	if err != nil {
		t.Fatalf("ParseFlags failed: %v", err)
	}
	if cfg.SiteNameRateLimit != 50 {
		t.Errorf("SiteNameRateLimit = %d, want 50", cfg.SiteNameRateLimit)
	}
}

func TestParseFlags_InvalidSiteNameRateLimit(t *testing.T) {
	for _, rate := range []string{"0", "-1"} {
		args := []string{"-h", "acme.sdwan.cisco.com", "-u", "user", "--site-name-rate", rate}
		if _, err := ParseFlags(args, nil, promptHiddenStub); err == nil {
			t.Errorf("expected error for --site-name-rate %s", rate)
		}
	}
}

func TestParseFlags_NoPasswordFlag(t *testing.T) {
	// -p/--password must not exist: passing it should be a flag parse
	// error, not silently accepted, since passwords must never be
	// supplied inline (visible in `ps` and shell history).
	if _, err := ParseFlags([]string{"-h", "acme.sdwan.cisco.com", "-u", "user", "-p", "changeme"}, nil, promptHiddenStub); err == nil {
		t.Fatal("expected error: -p/--password should not be a recognized flag")
	}
}

func TestParseFlags_PromptHiddenCalledWhenPasswordOmitted(t *testing.T) {
	called := false
	prompt := func(msg string) (string, error) {
		called = true
		return "prompted-password", nil
	}
	cfg, err := ParseFlags([]string{"-h", "acme.sdwan.cisco.com", "-u", "user"}, nil, prompt)
	if err != nil {
		t.Fatalf("ParseFlags failed: %v", err)
	}
	if !called {
		t.Error("expected promptHidden to be called when no password source is available")
	}
	if cfg.Password != "prompted-password" {
		t.Errorf("Password = %q, want %q", cfg.Password, "prompted-password")
	}
}

func TestParseFlags_PromptVisibleCalledForMissingHostAndUsername(t *testing.T) {
	var prompts []string
	promptVisible := func(msg string) (string, error) {
		prompts = append(prompts, msg)
		if len(prompts) == 1 {
			return "acme.sdwan.cisco.com", nil
		}
		return "user", nil
	}
	cfg, err := ParseFlags(nil, promptVisible, promptHiddenStub)
	if err != nil {
		t.Fatalf("ParseFlags failed: %v", err)
	}
	if len(prompts) != 2 {
		t.Fatalf("expected promptVisible to be called twice (host, username), got %d calls", len(prompts))
	}
	if cfg.Host != "acme.sdwan.cisco.com" {
		t.Errorf("Host = %q, want %q", cfg.Host, "acme.sdwan.cisco.com")
	}
	if cfg.Username != "user" {
		t.Errorf("Username = %q, want %q", cfg.Username, "user")
	}
}

func TestParseFlags_TokenFileSuppliesHostUsernamePassword(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cisco-vmanage-secrets.json")
	contents := tokenFileContents{
		Host:     "acme.sdwan.cisco.com",
		Username: "user",
		Password: "changeme",
	}
	data, err := json.Marshal(contents)
	if err != nil {
		t.Fatalf("marshal token file contents: %v", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("writing token file: %v", err)
	}

	cfg, err := ParseFlags([]string{"--token-file", path}, nil, nil)
	if err != nil {
		t.Fatalf("ParseFlags failed: %v", err)
	}
	if cfg.Host != "acme.sdwan.cisco.com" {
		t.Errorf("Host = %q, want %q", cfg.Host, "acme.sdwan.cisco.com")
	}
	if cfg.Username != "user" {
		t.Errorf("Username = %q, want %q", cfg.Username, "user")
	}
	if cfg.Password != "changeme" {
		t.Errorf("Password = %q, want %q", cfg.Password, "changeme")
	}
}

func TestParseFlags_TokenFileMissingFile(t *testing.T) {
	if _, err := ParseFlags([]string{"--token-file", "/nonexistent/cisco-vmanage-secrets.json"}, nil, promptHiddenStub); err == nil {
		t.Fatal("expected error when --token-file does not exist")
	}
}

func TestParseFlags_FlagOverridesTokenFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cisco-vmanage-secrets.json")
	contents := tokenFileContents{
		Host:     "from-file.sdwan.cisco.com",
		Username: "from-file-user",
		Password: "from-file-password",
	}
	data, err := json.Marshal(contents)
	if err != nil {
		t.Fatalf("marshal token file contents: %v", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("writing token file: %v", err)
	}

	cfg, err := ParseFlags([]string{"-h", "acme.sdwan.cisco.com", "--token-file", path}, nil, nil)
	if err != nil {
		t.Fatalf("ParseFlags failed: %v", err)
	}
	if cfg.Host != "acme.sdwan.cisco.com" {
		t.Errorf("Host = %q, want the explicit -h flag value, not the token file's", cfg.Host)
	}
	if cfg.Username != "from-file-user" {
		t.Errorf("Username = %q, want the token file's value %q", cfg.Username, "from-file-user")
	}
}
