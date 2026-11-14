// flags_test.go - CLI flag parsing tests for the Cisco ACI/APIC
// producer. Covers explicit single/batch mode selection
// and rejection of malformed invocations.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package apic

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.osirisjson.org/producers/osiris/network/cisco/run"
)

// TestParseFlags_OneRowCSVIsBatch proves: batch vs single is
// decided by --source, not by target count, so a one-row CSV is still
// batch input and still honours its --output directory.
func TestParseFlags_OneRowCSVIsBatch(t *testing.T) {
	dir := t.TempDir()
	csv := filepath.Join(dir, "one.csv")
	content := "datacenter,floor,room,rack,hostname,management_ip,port\n" +
		"MXP,,,,LAB-APIC1,192.0.2.1,\n"
	if err := os.WriteFile(csv, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out")

	cfg, err := ParseFlags([]string{"-s", csv, "-o", out, "-u", "admin"}, nil, nil)
	if err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	if cfg.Mode != run.ModeBatch {
		t.Errorf("one-row CSV: Mode = %q, want %q", cfg.Mode, run.ModeBatch)
	}
	if cfg.OutputDir != out {
		t.Errorf("OutputDir = %q, want %q", cfg.OutputDir, out)
	}
	if len(cfg.Targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(cfg.Targets))
	}
	if cfg.Targets[0].Hostname != "LAB-APIC1" {
		t.Errorf("target hostname = %q", cfg.Targets[0].Hostname)
	}
}

func TestParseFlags_BatchRequiresOutput(t *testing.T) {
	dir := t.TempDir()
	csv := filepath.Join(dir, "one.csv")
	if err := os.WriteFile(csv, []byte("datacenter,floor,room,rack,hostname,management_ip,port\nMXP,,,,LAB-APIC1,192.0.2.1,\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseFlags([]string{"-s", csv, "-u", "admin"}, nil, nil); err == nil {
		t.Fatal("batch mode without --output should fail")
	}
}

func TestParseFlags_RejectsPositionalArgs(t *testing.T) {
	_, err := ParseFlags([]string{"-h", "192.0.2.1", "-u", "admin", "stray"}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "positional") {
		t.Fatalf("expected positional-argument rejection, got: %v", err)
	}
}

func TestParseFlags_HostAndSourceMutuallyExclusive(t *testing.T) {
	_, err := ParseFlags([]string{"-h", "192.0.2.1", "-s", "test.csv"}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected mutual-exclusion error, got: %v", err)
	}
}

func TestParseFlags_SingleModeBuildsOneTarget(t *testing.T) {
	cfg, err := ParseFlags([]string{"-h", "192.0.2.1:8443", "-u", "admin"}, nil, nil)
	if err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	if cfg.Mode != run.ModeSingle {
		t.Errorf("Mode = %q, want %q", cfg.Mode, run.ModeSingle)
	}
	if len(cfg.Targets) != 1 || cfg.Targets[0].Port != 8443 {
		t.Fatalf("unexpected targets: %+v", cfg.Targets)
	}
}

// TestParseFlags_RejectsLogAndRedact proves: log-and-redact is refused,
// because the APIC producer performs no value replacement and the
// document metadata would otherwise claim a redaction that never
// happened. fail-closed and off remain accepted.
func TestParseFlags_RejectsLogAndRedact(t *testing.T) {
	_, err := ParseFlags([]string{"-h", "192.0.2.1", "-u", "admin", "--safe-failure-mode", "log-and-redact"}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("expected log-and-redact rejection, got: %v", err)
	}
	for _, mode := range []string{"fail-closed", "off"} {
		cfg, err := ParseFlags([]string{"-h", "192.0.2.1", "-u", "admin", "--safe-failure-mode", mode}, nil, nil)
		if err != nil {
			t.Fatalf("mode %s: %v", mode, err)
		}
		if cfg.SafeFailureMode != mode {
			t.Errorf("SafeFailureMode = %q, want %q", cfg.SafeFailureMode, mode)
		}
	}
}

// TestParseFlags_IncludeRawBodyRequiresAudit proves: --include-raw-body
// is refused without --purpose audit (it would silently do nothing) and
// accepted with it.
func TestParseFlags_IncludeRawBodyRequiresAudit(t *testing.T) {
	for _, args := range [][]string{
		{"-h", "192.0.2.1", "-u", "admin", "--include-raw-body"},
		{"-h", "192.0.2.1", "-u", "admin", "--purpose", "documentation", "--include-raw-body"},
	} {
		if _, err := ParseFlags(args, nil, nil); err == nil || !strings.Contains(err.Error(), "requires --purpose audit") {
			t.Errorf("%v: expected rejection, got %v", args, err)
		}
	}
	cfg, err := ParseFlags([]string{"-h", "192.0.2.1", "-u", "admin", "--purpose", "audit", "--include-raw-body"}, nil, nil)
	if err != nil || !cfg.IncludeRawBody {
		t.Fatalf("audit + --include-raw-body: cfg=%+v err=%v", cfg, err)
	}
}
