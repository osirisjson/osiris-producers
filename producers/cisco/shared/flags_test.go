// flags_test.go - Tests for CLI flag parsing, single/batch mode detection,
// mutual exclusivity validation and interactive password prompting.
//
// For an introduction to OSIRIS JSON Producer for Cisco see:
// "[OSIRIS-JSON-CISCO]."
//
// [OSIRIS-JSON-CISCO]: https://osirisjson.org/en/docs/producers/cisco

package shared

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseFlagsSingleMode(t *testing.T) {
	args := []string{"-h", "10.0.0.1", "-u", "admin", "-p", "secret"}
	cfg, err := ParseFlags("apic", args, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(cfg.Targets))
	}
	tgt := cfg.Targets[0]
	if tgt.Host != "10.0.0.1" {
		t.Errorf("host = %q, want 10.0.0.1", tgt.Host)
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
	if tgt.Hostname != "10.0.0.1" {
		t.Errorf("hostname = %q, want 10.0.0.1", tgt.Hostname)
	}
	if tgt.Owner != "self" {
		t.Errorf("owner = %q, want self", tgt.Owner)
	}
	if cfg.DetailLevel != "minimal" {
		t.Errorf("detail = %q, want minimal", cfg.DetailLevel)
	}
	if cfg.SafeFailureMode != "fail-closed" {
		t.Errorf("safe-failure-mode = %q, want fail-closed", cfg.SafeFailureMode)
	}
}

func TestParseFlagsSingleModeWithPort(t *testing.T) {
	args := []string{"--host", "10.0.0.1:8443", "--username", "admin", "--password", "secret"}
	cfg, err := ParseFlags("apic", args, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tgt := cfg.Targets[0]
	if tgt.Port != 8443 {
		t.Errorf("port = %d, want 8443", tgt.Port)
	}
}

func TestParseFlagsPortOverride(t *testing.T) {
	args := []string{"-h", "10.0.0.1:443", "-u", "admin", "-p", "s", "-P", "8443"}
	cfg, err := ParseFlags("apic", args, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Targets[0].Port != 8443 {
		t.Errorf("port = %d, want 8443 (explicit override)", cfg.Targets[0].Port)
	}
}

func TestParseFlagsMissingHost(t *testing.T) {
	args := []string{"-u", "admin", "-p", "secret"}
	_, err := ParseFlags("apic", args, nil)
	if err == nil {
		t.Fatal("expected error for missing --host")
	}
	if !strings.Contains(err.Error(), "required") {
		t.Errorf("error = %q, want mention of required", err.Error())
	}
}

func TestParseFlagsMissingUsername(t *testing.T) {
	args := []string{"-h", "10.0.0.1", "-p", "secret"}
	_, err := ParseFlags("apic", args, nil)
	if err == nil {
		t.Fatal("expected error for missing --username")
	}
}

func TestParseFlagsMutualExclusion(t *testing.T) {
	args := []string{"-h", "10.0.0.1", "-s", "targets.csv"}
	_, err := ParseFlags("apic", args, nil)
	if err == nil {
		t.Fatal("expected error for --host + --source")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error = %q, want mention of mutually exclusive", err.Error())
	}
}

func TestParseFlagsPasswordPrompt(t *testing.T) {
	args := []string{"-h", "10.0.0.1", "-u", "admin"}
	prompted := false
	prompt := func(msg string) (string, error) {
		prompted = true
		return "prompted-pass", nil
	}
	cfg, err := ParseFlags("apic", args, prompt)
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

func TestParseFlagsBatchMode(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "targets.csv")
	content := "hostname,type,ip\nspine-01,nxos,10.0.0.1\nspine-02,nxos,10.0.0.2\n"
	if err := os.WriteFile(csvPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	outDir := filepath.Join(dir, "out")
	args := []string{"-s", csvPath, "-o", outDir, "-u", "admin", "-p", "secret"}
	cfg, err := ParseFlags("nxos", args, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(cfg.Targets))
	}
	if cfg.Targets[0].Username != "admin" {
		t.Errorf("target[0] username = %q, want admin", cfg.Targets[0].Username)
	}
	if cfg.Targets[0].Type != "nxos" {
		t.Errorf("target[0] type = %q, want nxos", cfg.Targets[0].Type)
	}
	if cfg.OutputDir != outDir {
		t.Errorf("output dir = %q, want %q", cfg.OutputDir, outDir)
	}
}

func TestParseFlagsBatchMissingOutput(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "targets.csv")
	content := "hostname,type,ip\nspine-01,nxos,10.0.0.1\n"
	if err := os.WriteFile(csvPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	args := []string{"-s", csvPath}
	_, err := ParseFlags("nxos", args, nil)
	if err == nil {
		t.Fatal("expected error for batch mode without --output")
	}
}

func TestParseFlagsInvalidDetail(t *testing.T) {
	args := []string{"-h", "10.0.0.1", "-u", "admin", "-p", "s", "--detail", "verbose"}
	_, err := ParseFlags("apic", args, nil)
	if err == nil {
		t.Fatal("expected error for invalid --detail")
	}
}

func TestParseFlagsInsecure(t *testing.T) {
	args := []string{"-h", "10.0.0.1", "-u", "admin", "-p", "s", "--insecure"}
	cfg, err := ParseFlags("apic", args, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.InsecureTLS {
		t.Error("expected InsecureTLS to be true")
	}
}
