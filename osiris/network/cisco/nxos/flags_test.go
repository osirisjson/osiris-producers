// flags_test.go - Tests for the NX-OS producer's own CLI flag parsing:
// single/batch mode detection, --purpose/--include-raw-body, mutual
// exclusivity of -h/-s, credential precedence (--secrets-file,
// environment variable, interactive prompt) and positional-argument
// rejection.
//
// OSIRIS JSON Producer for Cisco NX-OS introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package nxos

import (
	"os"
	"path/filepath"
	"testing"

	"go.osirisjson.org/producers/osiris/network/cisco/run"
)

func writeCredsFileForFlagsTest(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "secrets-test.json")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseFlagsSingleMode(t *testing.T) {
	dir := t.TempDir()
	credFile := writeCredsFileForFlagsTest(t, dir, `{"password":"secret"}`)
	args := []string{"-h", "192.0.2.1", "-u", "admin", "--secrets-file", credFile}
	cfg, err := ParseFlags(args, nil, nil)
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
	if tgt.Type != "nxos" {
		t.Errorf("type = %q, want nxos", tgt.Type)
	}
	if cfg.Mode != run.ModeSingle {
		t.Errorf("mode = %q, want %q", cfg.Mode, run.ModeSingle)
	}
	if cfg.Purpose != "documentation" {
		t.Errorf("purpose = %q, want documentation", cfg.Purpose)
	}
	if cfg.IncludeRawBody {
		t.Error("include-raw-body should default to false")
	}
	if cfg.SafeFailureMode != "fail-closed" {
		t.Errorf("safe-failure-mode = %q, want fail-closed", cfg.SafeFailureMode)
	}
}

func TestParseFlagsPurposeAudit(t *testing.T) {
	args := []string{"-h", "192.0.2.1", "-u", "admin", "--purpose", "audit", "--include-raw-body"}
	cfg, err := ParseFlags(args, nil, func(string) (string, error) { return "s", nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Purpose != "audit" {
		t.Errorf("purpose = %q, want audit", cfg.Purpose)
	}
	if !cfg.IncludeRawBody {
		t.Error("include-raw-body should be true")
	}
}

func TestParseFlagsInvalidPurpose(t *testing.T) {
	args := []string{"-h", "192.0.2.1", "-u", "admin", "--purpose", "verbose"}
	_, err := ParseFlags(args, nil, func(string) (string, error) { return "s", nil })
	if err == nil {
		t.Fatal("expected error for invalid --purpose")
	}
}

func TestParseFlagsInvalidSafeFailureMode(t *testing.T) {
	args := []string{"-h", "192.0.2.1", "-u", "admin", "--safe-failure-mode", "bogus"}
	_, err := ParseFlags(args, nil, func(string) (string, error) { return "s", nil })
	if err == nil {
		t.Fatal("expected error for invalid --safe-failure-mode")
	}
}

func TestParseFlagsHostAndSourceMutuallyExclusive(t *testing.T) {
	args := []string{"-h", "192.0.2.1", "-s", "targets.csv"}
	_, err := ParseFlags(args, nil, nil)
	if err == nil {
		t.Fatal("expected error for --host and --source together")
	}
}

func TestParseFlagsBatchModeRequiresOutput(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "targets.csv")
	if err := os.WriteFile(csvPath, []byte("datacenter,floor,room,rack,hostname,management_ip,port\n,,,,,192.0.2.1,\n"), 0600); err != nil {
		t.Fatal(err)
	}

	args := []string{"-s", csvPath}
	_, err := ParseFlags(args, nil, nil)
	if err == nil {
		t.Fatal("expected error for batch mode without --output")
	}
}

func TestParseFlagsBatchMode(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "targets.csv")
	csv := "datacenter,floor,room,rack,hostname,management_ip,port\n" +
		"MXP,F1,R101,RACK-A,switch-01,192.0.2.10,\n"
	if err := os.WriteFile(csvPath, []byte(csv), 0600); err != nil {
		t.Fatal(err)
	}

	args := []string{"-s", csvPath, "-o", filepath.Join(dir, "out"), "-u", "admin"}
	cfg, err := ParseFlags(args, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Mode != run.ModeBatch {
		t.Errorf("mode = %q, want %q", cfg.Mode, run.ModeBatch)
	}
	if len(cfg.Targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(cfg.Targets))
	}
	if cfg.Targets[0].Username != "admin" {
		t.Errorf("target username = %q, want admin (global fallback)", cfg.Targets[0].Username)
	}
	if cfg.Targets[0].Type != "nxos" {
		t.Errorf("target type = %q, want nxos", cfg.Targets[0].Type)
	}
}

func TestParseFlagsPasswordFromEnv(t *testing.T) {
	t.Setenv(passwordEnvVar, "env-secret")
	args := []string{"-h", "192.0.2.1", "-u", "admin"}
	cfg, err := ParseFlags(args, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Targets[0].Password != "env-secret" {
		t.Errorf("password = %q, want env-secret", cfg.Targets[0].Password)
	}
}

func TestParseFlagsUnexpectedPositionalArgument(t *testing.T) {
	args := []string{"-h", "192.0.2.1", "-u", "admin", "extra-positional"}
	_, err := ParseFlags(args, nil, func(string) (string, error) { return "s", nil })
	if err == nil {
		t.Fatal("expected error for unexpected positional argument")
	}
}

func TestParseFlagsPortOverridesHostPort(t *testing.T) {
	args := []string{"-h", "192.0.2.1:8443", "-u", "admin", "-P", "443"}
	cfg, err := ParseFlags(args, nil, func(string) (string, error) { return "s", nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Targets[0].Port != 443 {
		t.Errorf("port = %d, want 443 (explicit --port overrides host:port)", cfg.Targets[0].Port)
	}
}

func TestParseFlagsHostFromSecretsFile(t *testing.T) {
	dir := t.TempDir()
	credFile := writeCredsFileForFlagsTest(t, dir, `{"host":"192.0.2.5","username":"opuser","password":"secret"}`)
	args := []string{"--secrets-file", credFile}
	cfg, err := ParseFlags(args, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Targets[0].Host != "192.0.2.5" {
		t.Errorf("host = %q, want 192.0.2.5 (from --secrets-file)", cfg.Targets[0].Host)
	}
	if cfg.Targets[0].Username != "opuser" {
		t.Errorf("username = %q, want opuser", cfg.Targets[0].Username)
	}
}

func TestParseFlagsNoHostNoPromptIsError(t *testing.T) {
	_, err := ParseFlags(nil, nil, nil)
	if err == nil {
		t.Fatal("expected error when no --host/--source and no prompt function")
	}
}
