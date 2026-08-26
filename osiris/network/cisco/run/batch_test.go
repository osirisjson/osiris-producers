// batch_test.go - Tests for CSV template generation,
// datacenter-hierarchy CSV parsing, batch orchestration with
// hierarchical output, mixed producer types and partial failures.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package run

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.osirisjson.org/producers/pkg/sdk"
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

// stubProducer implements sdk.Producer for testing RunBatch.
type stubProducer struct {
	fail bool
}

func (s *stubProducer) Collect(ctx *sdk.Context) (*sdk.Document, error) {
	if s.fail {
		return nil, os.ErrNotExist
	}
	return &sdk.Document{
		Schema:  sdk.SchemaURI,
		Version: sdk.SpecVersion,
		Metadata: sdk.Metadata{
			Timestamp: "2026-01-15T10:00:00Z",
			Generator: sdk.Generator{Name: "test", Version: "0.0.1"},
		},
		Topology: sdk.Topology{
			Resources: []sdk.Resource{},
		},
	}, nil
}

func stubFactories() FactoryRegistry {
	okFactory := func(target TargetConfig, cfg *RunConfig) sdk.Producer {
		return &stubProducer{fail: false}
	}
	return FactoryRegistry{
		"apic":  okFactory,
		"nxos":  okFactory,
		"iosxe": okFactory,
	}
}

func TestRunBatch(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "output")

	cfg := &RunConfig{
		Targets: []TargetConfig{
			{Host: "192.0.2.50", Hostname: "apic-01", Type: "apic", Datacenter: "MXP", Floor: "F3", Room: "R301", Rack: "RACK-A", Username: "admin", Password: "test"},
			{Host: "192.0.2.60", Hostname: "switch-spine01", Type: "nxos", Datacenter: "MXP", Floor: "F3", Room: "R301", Rack: "RACK-A", Username: "admin", Password: "test"},
		},
		OutputDir:       outDir,
		DetailLevel:     "minimal",
		SafeFailureMode: "fail-closed",
		Timestamp:       "2026-01-15T10-00-00Z",
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := RunBatch(cfg, stubFactories(), logger); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check hierarchical output files exist, named like single mode's
	// own cisco-<type>-<timestamp>-<hostname>.json convention so a
	// repeated batch run does not silently overwrite prior output.
	expected := []string{
		"output/MXP/F3/R301/RACK-A/cisco-apic-2026-01-15T10-00-00Z-apic-01.json",
		"output/MXP/F3/R301/RACK-A/cisco-nxos-2026-01-15T10-00-00Z-switch-spine01.json",
	}
	for _, rel := range expected {
		path := filepath.Join(dir, rel)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected output file %s", path)
		}
	}
}

func TestRunBatchFlatOutput(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "output")

	cfg := &RunConfig{
		Targets: []TargetConfig{
			{Host: "192.0.2.1", Hostname: "spine-01", Type: "nxos", Username: "admin", Password: "test"},
			{Host: "192.0.2.2", Hostname: "spine-02", Type: "nxos", Username: "admin", Password: "test"},
		},
		OutputDir:       outDir,
		DetailLevel:     "minimal",
		SafeFailureMode: "fail-closed",
		Timestamp:       "2026-01-15T10-00-00Z",
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := RunBatch(cfg, stubFactories(), logger); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No hierarchy - files at root of output dir.
	for _, name := range []string{"cisco-nxos-2026-01-15T10-00-00Z-spine-01.json", "cisco-nxos-2026-01-15T10-00-00Z-spine-02.json"} {
		path := filepath.Join(outDir, name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected output file %s", path)
		}
	}
}

func TestRunBatchUnknownType(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "output")

	cfg := &RunConfig{
		Targets: []TargetConfig{
			{Host: "192.0.2.1", Hostname: "device-01", Type: "unknown"},
		},
		OutputDir:       outDir,
		DetailLevel:     "minimal",
		SafeFailureMode: "fail-closed",
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	err := RunBatch(cfg, stubFactories(), logger)
	if err == nil {
		t.Fatal("expected error when all targets have unknown type")
	}
}

func TestRunBatchAllFail(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "output")

	failFactory := func(target TargetConfig, cfg *RunConfig) sdk.Producer {
		return &stubProducer{fail: true}
	}

	cfg := &RunConfig{
		Targets: []TargetConfig{
			{Host: "192.0.2.1", Hostname: "spine-01", Type: "nxos"},
		},
		OutputDir:       outDir,
		DetailLevel:     "minimal",
		SafeFailureMode: "fail-closed",
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	err := RunBatch(cfg, FactoryRegistry{"nxos": failFactory}, logger)
	if err == nil {
		t.Fatal("expected error when all targets fail")
	}
}

func TestRunBatchPartialFailure(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "output")

	factories := FactoryRegistry{
		"nxos": func(target TargetConfig, cfg *RunConfig) sdk.Producer {
			return &stubProducer{fail: target.Hostname == "bad-device"}
		},
	}

	cfg := &RunConfig{
		Targets: []TargetConfig{
			{Host: "192.0.2.1", Hostname: "ok-device", Type: "nxos"},
			{Host: "192.0.2.2", Hostname: "bad-device", Type: "nxos"},
		},
		OutputDir:       outDir,
		DetailLevel:     "minimal",
		SafeFailureMode: "fail-closed",
		Timestamp:       "2026-01-15T10-00-00Z",
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	err := RunBatch(cfg, factories, logger)
	if err != nil {
		t.Fatalf("partial failure should not return error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(outDir, "cisco-nxos-2026-01-15T10-00-00Z-ok-device.json")); os.IsNotExist(err) {
		t.Error("expected ok-device output file to exist")
	}
	if _, err := os.Stat(filepath.Join(outDir, "cisco-nxos-2026-01-15T10-00-00Z-bad-device.json")); !os.IsNotExist(err) {
		t.Error("expected bad-device output file to NOT exist")
	}
}

func TestRunBatchMixedTypes(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "output")

	cfg := &RunConfig{
		Targets: []TargetConfig{
			{Host: "192.0.2.1", Hostname: "apic-01", Type: "apic", Datacenter: "MXP"},
			{Host: "192.0.2.2", Hostname: "nx-spine", Type: "nxos", Datacenter: "MXP"},
			{Host: "192.0.2.3", Hostname: "xr-pe", Type: "iosxe", Datacenter: "MXP"},
		},
		OutputDir:       outDir,
		DetailLevel:     "minimal",
		SafeFailureMode: "fail-closed",
		Timestamp:       "2026-01-15T10-00-00Z",
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := RunBatch(cfg, stubFactories(), logger); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, name := range []string{"cisco-apic-2026-01-15T10-00-00Z-apic-01.json", "cisco-nxos-2026-01-15T10-00-00Z-nx-spine.json", "cisco-iosxe-2026-01-15T10-00-00Z-xr-pe.json"} {
		path := filepath.Join(outDir, "MXP", name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected output file %s", path)
		}
	}
}
