// batch_test.go - Tests for CSV template generation and
// hierarchy CSV parsing.
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

func TestCSVTemplate(t *testing.T) {
	tests := []struct {
		producer     string
		wantHostname string
		wantIP       string
	}{
		{"apic", "apic-01", "192.0.2.1"},
		{"nxos", "nxos-01", "192.0.2.10"},
		{"iosxe", "iosxe-01", "192.0.2.20"},
	}
	for _, tt := range tests {
		t.Run(tt.producer, func(t *testing.T) {
			tmpl := CSVTemplate(tt.producer)
			if !strings.Contains(tmpl, "datacenter,floor,room,rack,hostname,management_ip,port") {
				t.Error("template missing header row")
			}
			if !strings.Contains(tmpl, tt.wantHostname) {
				t.Errorf("template missing expected hostname %s", tt.wantHostname)
			}
			if !strings.Contains(tmpl, tt.wantIP) {
				t.Errorf("template missing expected RFC 5737 address %s", tt.wantIP)
			}
			// No "type" column: a batch CSV is inherently
			// single-producer, so it must not repeat the producer name
			// as a data value.
			if strings.Contains(tmpl, ","+tt.producer+",") {
				t.Errorf("template should not have a type column value %q", tt.producer)
			}
			// Exactly one data row (plus the header line).
			lines := strings.Split(strings.TrimRight(tmpl, "\n"), "\n")
			if len(lines) != 2 {
				t.Errorf("expected header + exactly 1 data row, got %d lines: %v", len(lines), lines)
			}
		})
	}
}

func TestCSVTemplate_UnknownProducerFallsBackGracefully(t *testing.T) {
	tmpl := CSVTemplate("futurevendor")
	if !strings.Contains(tmpl, "futurevendor-01") {
		t.Errorf("expected a fallback example row for an unrecognized producer name, got: %s", tmpl)
	}
}

func TestParseCSV(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "targets.csv")
	content := `# Comment line
datacenter,floor,room,rack,hostname,management_ip,port
MXP,F3,R301,RACK-A,switch-spine01,192.0.2.50,
MXP,F3,R301,RACK-A,switch-spine02,192.0.2.60,8443
MXP,F3,R302,RACK-B,switch-spine03,192.0.2.70,
`
	if err := os.WriteFile(csvPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	targets, err := ParseCSV(csvPath, "nxos")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 3 {
		t.Fatalf("expected 3 targets, got %d", len(targets))
	}

	// A batch CSV is inherently single-producer: every target gets the
	// type passed in by the caller, never a per-row CSV value.
	for i, tgt := range targets {
		if tgt.Type != "nxos" {
			t.Errorf("target[%d].Type = %q, want nxos", i, tgt.Type)
		}
	}

	if targets[0].Host != "192.0.2.50" || targets[0].Hostname != "switch-spine01" {
		t.Errorf("target[0] = %+v", targets[0])
	}
	if targets[0].Datacenter != "MXP" || targets[0].Floor != "F3" || targets[0].Room != "R301" || targets[0].Rack != "RACK-A" {
		t.Errorf("target[0] location = %+v", targets[0])
	}

	if targets[1].Port != 8443 {
		t.Errorf("target[1] port = %d, want 8443", targets[1].Port)
	}

	if targets[2].Room != "R302" || targets[2].Rack != "RACK-B" {
		t.Errorf("target[2] location = %+v", targets[2])
	}
}

func TestParseCSVMissingRequiredColumns(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "targets.csv")
	content := "datacenter,floor,management_ip\nMXP,F3,192.0.2.1\n"
	if err := os.WriteFile(csvPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := ParseCSV(csvPath, "nxos")
	if err == nil {
		t.Fatal("expected error for missing required columns")
	}
	if !strings.Contains(err.Error(), "hostname") {
		t.Errorf("error = %q, want mention of hostname", err.Error())
	}
}

func TestParseCSVMinimalColumns(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "targets.csv")
	content := "hostname,management_ip\nspine-01,192.0.2.1\nspine-02,192.0.2.2\n"
	if err := os.WriteFile(csvPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	targets, err := ParseCSV(csvPath, "nxos")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(targets))
	}
	if targets[0].Hostname != "spine-01" || targets[0].Type != "nxos" {
		t.Errorf("target[0] = %+v", targets[0])
	}
}

func TestParseCSVEmpty(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "targets.csv")
	content := "hostname,management_ip\n"
	if err := os.WriteFile(csvPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := ParseCSV(csvPath, "nxos")
	if err == nil {
		t.Fatal("expected error for empty CSV")
	}
}

func TestParseCSVMissingHostname(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "targets.csv")
	content := "hostname,management_ip\n,192.0.2.1\n"
	if err := os.WriteFile(csvPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := ParseCSV(csvPath, "nxos")
	if err == nil {
		t.Fatal("expected error for missing hostname")
	}
}

func TestParseCSVPortColumn(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "targets.csv")
	content := "hostname,management_ip,port\nswitch-01,192.0.2.1,8443\n"
	if err := os.WriteFile(csvPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	targets, err := ParseCSV(csvPath, "nxos")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if targets[0].Port != 8443 {
		t.Errorf("port = %d, want 8443", targets[0].Port)
	}
}

func TestParseCSVPortOutOfRange(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "targets.csv")
	content := "hostname,management_ip,port\nswitch-01,192.0.2.1,99999\n"
	if err := os.WriteFile(csvPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := ParseCSV(csvPath, "nxos")
	if err == nil {
		t.Fatal("expected error for out-of-range port")
	}
}

func TestParseCSVPortTrailingGarbage(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "targets.csv")
	content := "hostname,management_ip,port\nswitch-01,192.0.2.1,8443abc\n"
	if err := os.WriteFile(csvPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := ParseCSV(csvPath, "nxos")
	if err == nil {
		t.Fatal("expected error for port with trailing non-numeric characters")
	}
}
