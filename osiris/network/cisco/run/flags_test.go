// flags_test.go - Tests for CLI flag parsing, single/batch mode
// detection, mutual exclusivity validation, credential precedence
// (--secrets-file, environment variable, interactive prompt) and
// positional-argument rejection.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package run

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCredsFileForFlagsTest(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "creds-test.json")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseFlagsSingleMode(t *testing.T) {
	dir := t.TempDir()
	credFile := writeCredsFileForFlagsTest(t, dir, `{"password":"secret"}`)
	args := []string{"-h", "192.0.2.1", "-u", "admin", "--secrets-file", credFile}
	cfg, err := ParseFlags("apic", args, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(cfg.Targets))
	}
	tgt := cfg.Targets[0]
	if tgt.Host != "192.0.2.1" {
		t.Errorf("host = %q, want 192.0.2.1", tgt.Host)
	}
	if tgt.Username != "admin" {
		t.Errorf("username = %q, want admin", tgt.Username)
	}
	if tgt.Password != "secret" {
		t.Errorf("password = %q, want secret", tgt.Password)
	}
	if tgt.Type != "apic" {
		t.Errorf("type = %q, want apic", tgt.Type)
	}
	if tgt.Hostname != "192.0.2.1" {
		t.Errorf("hostname = %q, want 192.0.2.1", tgt.Hostname)
	}
	if cfg.Mode != ModeSingle {
		t.Errorf("mode = %q, want %q", cfg.Mode, ModeSingle)
	}
	if cfg.DetailLevel != "minimal" {
		t.Errorf("detail = %q, want minimal", cfg.DetailLevel)
	}
	if cfg.SafeFailureMode != "fail-closed" {
		t.Errorf("safe-failure-mode = %q, want fail-closed", cfg.SafeFailureMode)
	}
}

func TestParseFlagsSingleModeWithPort(t *testing.T) {
	args := []string{"--host", "192.0.2.1:8443", "--username", "admin"}
	cfg, err := ParseFlags("apic", args, nil, func(string) (string, error) { return "secret", nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tgt := cfg.Targets[0]
	if tgt.Port != 8443 {
		t.Errorf("port = %d, want 8443", tgt.Port)
	}
}

func TestParseFlagsPortOverride(t *testing.T) {
	args := []string{"-h", "192.0.2.1:443", "-u", "admin", "-P", "8443"}
	cfg, err := ParseFlags("apic", args, nil, func(string) (string, error) { return "s", nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Targets[0].Port != 8443 {
		t.Errorf("port = %d, want 8443 (explicit override)", cfg.Targets[0].Port)
	}
}

func TestParseFlagsMissingHost(t *testing.T) {
	args := []string{"-u", "admin"}
	_, err := ParseFlags("apic", args, nil, nil)
	if err == nil {
		t.Fatal("expected error for missing --host with no interactive fallback")
	}
	if !strings.Contains(err.Error(), "required") {
		t.Errorf("error = %q, want mention of required", err.Error())
	}
}

func TestParseFlagsMissingUsername(t *testing.T) {
	args := []string{"-h", "192.0.2.1"}
	_, err := ParseFlags("apic", args, nil, nil)
	if err == nil {
		t.Fatal("expected error for missing --username with no interactive fallback")
	}
}

func TestParseFlagsHostAndUsernamePromptFallback(t *testing.T) {
	args := []string{}
	calls := []string{}
	promptVisible := func(prompt string) (string, error) {
		calls = append(calls, prompt)
		if len(calls) == 1 {
			return "192.0.2.1", nil
		}
		return "admin", nil
	}
	cfg, err := ParseFlags("apic", args, promptVisible, func(string) (string, error) { return "secret", nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 interactive prompts (host, username), got %d", len(calls))
	}
	if cfg.Targets[0].Host != "192.0.2.1" || cfg.Targets[0].Username != "admin" {
		t.Errorf("target = %+v", cfg.Targets[0])
	}
}

func TestParseFlagsMutualExclusion(t *testing.T) {
	args := []string{"-h", "192.0.2.1", "-s", "targets.csv"}
	_, err := ParseFlags("apic", args, nil, nil)
	if err == nil {
		t.Fatal("expected error for --host + --source")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error = %q, want mention of mutually exclusive", err.Error())
	}
}

func TestParseFlagsPasswordPrompt(t *testing.T) {
	args := []string{"-h", "192.0.2.1", "-u", "admin"}
	prompted := false
	prompt := func(msg string) (string, error) {
		prompted = true
		return "prompted-pass", nil
	}
	cfg, err := ParseFlags("apic", args, nil, prompt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !prompted {
		t.Error("expected password prompt to be called")
	}
	if cfg.Targets[0].Password != "prompted-pass" {
		t.Errorf("password = %q, want prompted-pass", cfg.Targets[0].Password)
	}
}

func TestParseFlagsPasswordFromEnvVar(t *testing.T) {
	t.Setenv("OSIRISJSON_CISCO_APIC_PASSWORD", "env-pass")
	args := []string{"-h", "192.0.2.1", "-u", "admin"}
	promptCalled := false
	prompt := func(string) (string, error) {
		promptCalled = true
		return "should-not-be-used", nil
	}
	cfg, err := ParseFlags("apic", args, nil, prompt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if promptCalled {
		t.Error("password prompt should not be called when env var is set")
	}
	if cfg.Targets[0].Password != "env-pass" {
		t.Errorf("password = %q, want env-pass", cfg.Targets[0].Password)
	}
}

func TestParseFlagsCredentialsFileTakesPrecedenceOverEnvVar(t *testing.T) {
	t.Setenv("OSIRISJSON_CISCO_APIC_PASSWORD", "env-pass")
	dir := t.TempDir()
	credFile := writeCredsFileForFlagsTest(t, dir, `{"password":"file-pass"}`)
	args := []string{"-h", "192.0.2.1", "-u", "admin", "--secrets-file", credFile}
	cfg, err := ParseFlags("apic", args, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Targets[0].Password != "file-pass" {
		t.Errorf("password = %q, want file-pass (secrets-file beats env var)", cfg.Targets[0].Password)
	}
}

func TestParseFlagsNoPasswordSource(t *testing.T) {
	args := []string{"-h", "192.0.2.1", "-u", "admin"}
	cfg, err := ParseFlags("apic", args, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Targets[0].Password != "" {
		t.Errorf("password = %q, want empty", cfg.Targets[0].Password)
	}
}

func TestParseFlagsRejectsPositionalArgs(t *testing.T) {
	args := []string{"-h", "192.0.2.1", "-u", "admin", "extra-arg"}
	_, err := ParseFlags("apic", args, nil, func(string) (string, error) { return "s", nil })
	if err == nil {
		t.Fatal("expected error for unexpected positional argument")
	}
	if !strings.Contains(err.Error(), "positional") {
		t.Errorf("error = %q, want mention of positional argument", err.Error())
	}
}

func TestParseFlagsNoPasswordFlag(t *testing.T) {
	// -p/--password must no longer exist: a CLI flag value is visible
	// via ps and shell history.
	args := []string{"-h", "192.0.2.1", "-u", "admin", "-p", "secret"}
	_, err := ParseFlags("apic", args, nil, nil)
	if err == nil {
		t.Fatal("expected error: -p is not a recognized flag")
	}
}

func TestParseFlagsSingleMode_SecretsFileRulesShape(t *testing.T) {
	dir := t.TempDir()
	credFile := writeCredsFileForFlagsTest(t, dir, `{
  "default": {"username": "admin", "password": "default-pass"},
  "rules": [
    {"hosts": "198.51.100.0/24", "username": "dc1-admin", "password": "dc1-pass"}
  ]
}`)

	// Host falls inside the dc1 rule's CIDR: should get the rule's
	// credentials, not Default.
	cfg, err := ParseFlags("apic", []string{"-h", "198.51.100.42", "--secrets-file", credFile}, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Targets[0].Username != "dc1-admin" || cfg.Targets[0].Password != "dc1-pass" {
		t.Errorf("target = %+v, want dc1-admin/dc1-pass", cfg.Targets[0])
	}

	// Host outside any rule: should fall back to Default.
	cfg, err = ParseFlags("apic", []string{"-h", "192.0.2.1", "--secrets-file", credFile}, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Targets[0].Username != "admin" || cfg.Targets[0].Password != "default-pass" {
		t.Errorf("target = %+v, want admin/default-pass (Default fallback)", cfg.Targets[0])
	}
}

func TestParseFlagsSingleMode_FlagUsernameOverridesSecretsFileRule(t *testing.T) {
	dir := t.TempDir()
	credFile := writeCredsFileForFlagsTest(t, dir, `{
  "rules": [
    {"hosts": "198.51.100.0/24", "username": "dc1-admin", "password": "dc1-pass"}
  ]
}`)

	cfg, err := ParseFlags("apic", []string{"-h", "198.51.100.42", "-u", "explicit-user", "--secrets-file", credFile}, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Targets[0].Username != "explicit-user" {
		t.Errorf("username = %q, want explicit-user (an explicit -u flag must beat a secrets-file rule)", cfg.Targets[0].Username)
	}
	if cfg.Targets[0].Password != "dc1-pass" {
		t.Errorf("password = %q, want dc1-pass", cfg.Targets[0].Password)
	}
}

func TestParseFlagsBatchMode_SecretsFileRulesPerTarget(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "targets.csv")
	content := "hostname,management_ip\n" +
		"dc1-switch,198.51.100.5\n" +
		"dc2-switch,203.0.113.10\n" +
		"other-switch,192.0.2.1\n"
	if err := os.WriteFile(csvPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	credFile := writeCredsFileForFlagsTest(t, dir, `{
  "default": {"username": "admin", "password": "default-pass"},
  "rules": [
    {"hosts": "198.51.100.0/24", "username": "dc1-admin", "password": "dc1-pass"},
    {"hosts": "203.0.113.10", "username": "dc2-admin", "password": "dc2-pass"}
  ]
}`)

	outDir := filepath.Join(dir, "out")
	cfg, err := ParseFlags("nxos", []string{"-s", csvPath, "-o", outDir, "--secrets-file", credFile}, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Targets) != 3 {
		t.Fatalf("expected 3 targets, got %d", len(cfg.Targets))
	}

	byHost := map[string]TargetConfig{}
	for _, tgt := range cfg.Targets {
		byHost[tgt.Host] = tgt
	}

	if tgt := byHost["198.51.100.5"]; tgt.Username != "dc1-admin" || tgt.Password != "dc1-pass" {
		t.Errorf("dc1-switch = %+v, want dc1-admin/dc1-pass", tgt)
	}
	if tgt := byHost["203.0.113.10"]; tgt.Username != "dc2-admin" || tgt.Password != "dc2-pass" {
		t.Errorf("dc2-switch = %+v, want dc2-admin/dc2-pass", tgt)
	}
	if tgt := byHost["192.0.2.1"]; tgt.Username != "admin" || tgt.Password != "default-pass" {
		t.Errorf("other-switch = %+v, want admin/default-pass (Default fallback)", tgt)
	}
}

func TestParseFlagsBatchMode_SecretsFileDefaultOnlyNoRules(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "targets.csv")
	content := "hostname,management_ip\n" +
		"switch-01,192.0.2.1\n" +
		"switch-02,192.0.2.2\n" +
		"switch-03,192.0.2.3\n" +
		"switch-04,192.0.2.4\n"
	if err := os.WriteFile(csvPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	credFile := writeCredsFileForFlagsTest(t, dir, `{"default": {"username": "admin", "password": "shared-pass"}}`)

	outDir := filepath.Join(dir, "out")
	cfg, err := ParseFlags("nxos", []string{"-s", csvPath, "-o", outDir, "--secrets-file", credFile, "--insecure"}, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Targets) != 4 {
		t.Fatalf("expected 4 targets, got %d", len(cfg.Targets))
	}
	for _, tgt := range cfg.Targets {
		if tgt.Username != "admin" || tgt.Password != "shared-pass" {
			t.Errorf("target %+v: want admin/shared-pass from Default, got %q/%q", tgt, tgt.Username, tgt.Password)
		}
	}
}

func TestParseFlagsBatchMode(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "targets.csv")
	content := "hostname,management_ip\nspine-01,192.0.2.1\nspine-02,192.0.2.2\n"
	if err := os.WriteFile(csvPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	outDir := filepath.Join(dir, "out")
	credFile := writeCredsFileForFlagsTest(t, dir, `{"password":"secret"}`)
	args := []string{"-s", csvPath, "-o", outDir, "-u", "admin", "--secrets-file", credFile}
	cfg, err := ParseFlags("nxos", args, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(cfg.Targets))
	}
	if cfg.Targets[0].Username != "admin" {
		t.Errorf("target[0] username = %q, want admin", cfg.Targets[0].Username)
	}
	if cfg.Targets[0].Password != "secret" {
		t.Errorf("target[0] password = %q, want secret", cfg.Targets[0].Password)
	}
	if cfg.Targets[0].Type != "nxos" {
		t.Errorf("target[0] type = %q, want nxos", cfg.Targets[0].Type)
	}
	if cfg.OutputDir != outDir {
		t.Errorf("output dir = %q, want %q", cfg.OutputDir, outDir)
	}
	if cfg.Mode != ModeBatch {
		t.Errorf("mode = %q, want %q", cfg.Mode, ModeBatch)
	}
	if !cfg.IsBatch() {
		t.Error("expected IsBatch() to be true")
	}
}

func TestParseFlagsBatchMode_OneRowStillBatch(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "targets.csv")
	content := "hostname,management_ip\nspine-01,192.0.2.1\n"
	if err := os.WriteFile(csvPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	outDir := filepath.Join(dir, "out")
	args := []string{"-s", csvPath, "-o", outDir, "-u", "admin"}
	cfg, err := ParseFlags("nxos", args, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(cfg.Targets))
	}
	if !cfg.IsBatch() {
		t.Error("a one-row CSV started via --source must still be treated as batch mode")
	}
}

func TestParseFlagsBatchMissingOutput(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "targets.csv")
	content := "hostname,management_ip\nspine-01,192.0.2.1\n"
	if err := os.WriteFile(csvPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	args := []string{"-s", csvPath}
	_, err := ParseFlags("nxos", args, nil, nil)
	if err == nil {
		t.Fatal("expected error for batch mode without --output")
	}
}

func TestParseFlagsInvalidDetail(t *testing.T) {
	args := []string{"-h", "192.0.2.1", "-u", "admin", "--detail", "verbose"}
	_, err := ParseFlags("apic", args, nil, func(string) (string, error) { return "s", nil })
	if err == nil {
		t.Fatal("expected error for invalid --detail")
	}
}

func TestParseFlagsInsecure(t *testing.T) {
	args := []string{"-h", "192.0.2.1", "-u", "admin", "--insecure"}
	cfg, err := ParseFlags("apic", args, nil, func(string) (string, error) { return "s", nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.InsecureTLS {
		t.Error("expected InsecureTLS to be true")
	}
}
