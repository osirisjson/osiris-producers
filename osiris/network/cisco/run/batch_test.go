// batch_test.go - Tests for CSV template generation, datacenter-hierarchy CSV parsing,
// batch orchestration with hierarchical output, mixed producer types and partial failures.
//
// For an introduction to OSIRIS JSON Producer for Cisco see:
// "[OSIRIS-JSON-CISCO]."
//
// [OSIRIS-JSON-CISCO]: https://osirisjson.org/en/docs/producers/network/cisco

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
	tmpl := CSVTemplate("apic")
	if !strings.Contains(tmpl, "dc,floor,room,zone,hostname,type,ip,port,owner,notes") {
		t.Error("template missing header row")
	}
	if !strings.Contains(tmpl, "apic") {
		t.Error("template missing producer name")
	}
	// Check that example rows are present.
	if !strings.Contains(tmpl, "nxos") {
		t.Error("template missing nxos example row")
	}
	if !strings.Contains(tmpl, "iosxe") {
		t.Error("template missing iosxe example row")
	}
	// Check owner values are present in example rows.
	if !strings.Contains(tmpl, "self") || !strings.Contains(tmpl, "isp") {
		t.Error("template missing owner values in example rows")
	}
}

func TestParseCSV(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "targets.csv")
	content := `# Comment line
dc,floor,room,zone,hostname,type,ip,port,owner,notes
AMS-01,F3,R301,POD-A,apic-01,apic,10.10.1.1,,self,Primary controller
AMS-01,F3,R301,POD-A,nx-spine-01,nxos,10.10.1.10,8443,self,Spine switch
AMS-01,F3,R302,POD-B,isp-pe-router,iosxr,172.16.0.1,,isp,ISP PE router
`
	if err := os.WriteFile(csvPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	targets, err := ParseCSV(csvPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 3 {
		t.Fatalf("expected 3 targets, got %d", len(targets))
	}

	// First target: APIC.
	if targets[0].Host != "10.10.1.1" || targets[0].Hostname != "apic-01" {
		t.Errorf("target[0] = %+v", targets[0])
	}
	if targets[0].Type != "apic" || targets[0].DC != "AMS-01" || targets[0].Zone != "POD-A" {
		t.Errorf("target[0] type/location = %+v", targets[0])
	}
	if targets[0].Owner != "self" {
		t.Errorf("target[0] owner = %q, want self", targets[0].Owner)
	}

	// Second target: NX-OS with explicit port.
	if targets[1].Port != 8443 || targets[1].Type != "nxos" {
		t.Errorf("target[1] = %+v", targets[1])
	}

	// Third target: IOS-XR with ISP owner.
	if targets[2].Type != "iosxr" || targets[2].Owner != "isp" {
		t.Errorf("target[2] = %+v", targets[2])
	}
}

func TestParseCSVOwnerDefault(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "targets.csv")
	content := "hostname,type,ip\nswitch-01,nxos,10.0.0.1\n"
	if err := os.WriteFile(csvPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	targets, err := ParseCSV(csvPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if targets[0].Owner != "self" {
		t.Errorf("owner should default to self, got %q", targets[0].Owner)
	}
}

func TestParseCSVInvalidOwner(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "targets.csv")
	content := "hostname,type,ip,owner\nswitch-01,nxos,10.0.0.1,vendor\n"
	if err := os.WriteFile(csvPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := ParseCSV(csvPath)
	if err == nil {
		t.Fatal("expected error for invalid owner")
	}
	if !strings.Contains(err.Error(), "invalid owner") {
		t.Errorf("error = %q, want mention of invalid owner", err.Error())
	}
}

func TestParseCSVMissingRequiredColumns(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "targets.csv")
	content := "dc,floor,ip\nAMS-01,F3,10.0.0.1\n"
	if err := os.WriteFile(csvPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := ParseCSV(csvPath)
	if err == nil {
		t.Fatal("expected error for missing required columns")
	}
	if !strings.Contains(err.Error(), "hostname") || !strings.Contains(err.Error(), "type") {
		t.Errorf("error = %q, want mention of hostname and type", err.Error())
	}
}

func TestParseCSVMinimalColumns(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "targets.csv")
	content := "hostname,type,ip\nspine-01,nxos,10.0.0.1\nspine-02,nxos,10.0.0.2\n"
	if err := os.WriteFile(csvPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	targets, err := ParseCSV(csvPath)
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
	content := "hostname,type,ip\n"
	if err := os.WriteFile(csvPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := ParseCSV(csvPath)
	if err == nil {
		t.Fatal("expected error for empty CSV")
	}
}

func TestParseCSVMissingHostname(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "targets.csv")
	content := "hostname,type,ip\n,nxos,10.0.0.1\n"
	if err := os.WriteFile(csvPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := ParseCSV(csvPath)
	if err == nil {
		t.Fatal("expected error for missing hostname")
	}
}

func TestParseCSVPortColumn(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "targets.csv")
	content := "hostname,type,ip,port\nswitch-01,nxos,10.0.0.1,8443\n"
	if err := os.WriteFile(csvPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	targets, err := ParseCSV(csvPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if targets[0].Port != 8443 {
		t.Errorf("port = %d, want 8443", targets[0].Port)
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
		"iosxr": okFactory,
	}
}

func TestRunBatch(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "output")

	cfg := &RunConfig{
		Targets: []TargetConfig{
			{Host: "10.10.1.1", Hostname: "apic-01", Type: "apic", DC: "AMS-01", Floor: "F3", Room: "R301", Zone: "POD-A", Username: "admin", Password: "test"},
			{Host: "10.10.1.10", Hostname: "nx-spine-01", Type: "nxos", DC: "AMS-01", Floor: "F3", Room: "R301", Zone: "POD-A", Username: "admin", Password: "test"},
		},
		OutputDir:       outDir,
		DetailLevel:     "minimal",
		SafeFailureMode: "fail-closed",
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := RunBatch(cfg, stubFactories(), logger); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check hierarchical output files exist.
	expected := []string{
		"output/AMS-01/F3/R301/POD-A/apic-01.json",
		"output/AMS-01/F3/R301/POD-A/nx-spine-01.json",
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
			{Host: "10.0.0.1", Hostname: "spine-01", Type: "nxos", Username: "admin", Password: "test"},
			{Host: "10.0.0.2", Hostname: "spine-02", Type: "nxos", Username: "admin", Password: "test"},
		},
		OutputDir:       outDir,
		DetailLevel:     "minimal",
		SafeFailureMode: "fail-closed",
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := RunBatch(cfg, stubFactories(), logger); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No hierarchy - files at root of output dir.
	for _, name := range []string{"spine-01.json", "spine-02.json"} {
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
			{Host: "10.0.0.1", Hostname: "device-01", Type: "unknown"},
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
			{Host: "10.0.0.1", Hostname: "spine-01", Type: "nxos"},
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
			{Host: "10.0.0.1", Hostname: "ok-device", Type: "nxos"},
			{Host: "10.0.0.2", Hostname: "bad-device", Type: "nxos"},
		},
		OutputDir:       outDir,
		DetailLevel:     "minimal",
		SafeFailureMode: "fail-closed",
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	err := RunBatch(cfg, factories, logger)
	if err != nil {
		t.Fatalf("partial failure should not return error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(outDir, "ok-device.json")); os.IsNotExist(err) {
		t.Error("expected ok-device.json to exist")
	}
	if _, err := os.Stat(filepath.Join(outDir, "bad-device.json")); !os.IsNotExist(err) {
		t.Error("expected bad-device.json to NOT exist")
	}
}

func TestRunBatchMixedTypes(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "output")

	cfg := &RunConfig{
		Targets: []TargetConfig{
			{Host: "10.0.0.1", Hostname: "apic-01", Type: "apic", DC: "DC1"},
			{Host: "10.0.0.2", Hostname: "nx-spine", Type: "nxos", DC: "DC1"},
			{Host: "10.0.0.3", Hostname: "xr-pe", Type: "iosxr", DC: "DC1"},
		},
		OutputDir:       outDir,
		DetailLevel:     "minimal",
		SafeFailureMode: "fail-closed",
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := RunBatch(cfg, stubFactories(), logger); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, name := range []string{"apic-01.json", "nx-spine.json", "xr-pe.json"} {
		path := filepath.Join(outDir, "DC1", name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected output file %s", path)
		}
	}
}
