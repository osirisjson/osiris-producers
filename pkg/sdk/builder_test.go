// builder_test.go - Tests for DocumentBuilder invariants: generator requirement, deterministic
// sorting, duplicate ID detection, reference integrity, extension key validation and secret scanning.
// These tests exercise the SDK's internal logic (sorting, dedup, ref integrity), not vendor-specific behavior.
// The provider name "aws" is just a string that happens to pass ValidateProviderName.
//
// For an introduction to OSIRIS JSON Producer Development Guidelines see:
// "[OSIRIS-PRODUCER-GUIDELINES]."
//
// [OSIRIS-PRODUCER-GUIDELINES]: https://osirisjson.org/en/docs/getting-started/osiris-producer-guidelines

package sdk

import (
	"strings"
	"testing"
	"time"
)

func fixedClock() time.Time {
	return time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
}

func testContext() *Context {
	return &Context{
		Config: &ProducerConfig{SafeFailureMode: FailClosed},
		Logger: nil,
		Clock:  fixedClock,
	}
}

// [test only] mustResource creates a resource or panics.
func mustResource(id, rtype string, p Provider) Resource {
	r, err := NewResource(id, rtype, p)
	if err != nil {
		panic(err)
	}
	return r
}

// [test only] mustProvider creates a provider or panics.
func mustProvider(name string) Provider {
	p, err := NewProvider(name)
	if err != nil {
		panic(err)
	}
	return p
}

// [test only] mustConnection creates a connection or panics.
func mustConnection(id, ctype, src, tgt string) Connection {
	c, err := NewConnection(id, ctype, src, tgt)
	if err != nil {
		panic(err)
	}
	return c
}

// [test only] mustGroup creates a group or panics.
func mustGroup(id, gtype string) Group {
	g, err := NewGroup(id, gtype)
	if err != nil {
		panic(err)
	}
	return g
}

func TestBuildRequiresGenerator(t *testing.T) {
	b := NewDocumentBuilder(testContext())
	_, err := b.Build()
	if err == nil {
		t.Error("expected error when generator is not set")
	}
}

func TestBuildSetsSchemaAndVersion(t *testing.T) {
	b := NewDocumentBuilder(testContext()).
		WithGenerator("test-producer", "0.2.2")
	doc, err := b.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc.Schema != SchemaURI {
		t.Errorf("Schema = %q, want %q", doc.Schema, SchemaURI)
	}
	if doc.Version != SpecVersion {
		t.Errorf("Version = %q, want %q", doc.Version, SpecVersion)
	}
}

func TestBuildSetsTimestamp(t *testing.T) {
	b := NewDocumentBuilder(testContext()).
		WithGenerator("test-producer", "0.2.2")
	doc, err := b.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "2026-01-15T10:00:00Z"
	if doc.Metadata.Timestamp != want {
		t.Errorf("Timestamp = %q, want %q", doc.Metadata.Timestamp, want)
	}
}

func TestBuildSortsResourcesByID(t *testing.T) {
	b := NewDocumentBuilder(testContext()).
		WithGenerator("test-producer", "0.2.2")
	b.AddResource(mustResource("z-resource", "compute.vm", mustProvider("aws")))
	b.AddResource(mustResource("a-resource", "compute.vm", mustProvider("aws")))
	b.AddResource(mustResource("m-resource", "compute.vm", mustProvider("aws")))

	doc, err := b.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ids := []string{doc.Topology.Resources[0].ID, doc.Topology.Resources[1].ID, doc.Topology.Resources[2].ID}
	if ids[0] != "a-resource" || ids[1] != "m-resource" || ids[2] != "z-resource" {
		t.Errorf("resources not sorted: %v", ids)
	}
}

func TestBuildSortsConnectionsByID(t *testing.T) {
	b := NewDocumentBuilder(testContext()).
		WithGenerator("test-producer", "0.2.2")
	b.AddResource(mustResource("a", "compute.vm", mustProvider("aws")))
	b.AddResource(mustResource("b", "compute.vm", mustProvider("aws")))
	b.AddResource(mustResource("c", "compute.vm", mustProvider("aws")))
	b.AddResource(mustResource("d", "compute.vm", mustProvider("aws")))
	b.AddConnection(mustConnection("z-conn", "network", "a", "b"))
	b.AddConnection(mustConnection("a-conn", "network", "c", "d"))

	doc, err := b.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc.Topology.Connections[0].ID != "a-conn" {
		t.Errorf("connections not sorted: first = %q", doc.Topology.Connections[0].ID)
	}
}

func TestBuildSortsGroupsByID(t *testing.T) {
	b := NewDocumentBuilder(testContext()).
		WithGenerator("test-producer", "0.2.2")
	b.AddGroup(mustGroup("z-group", "physical.site"))
	b.AddGroup(mustGroup("a-group", "physical.site"))

	doc, err := b.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc.Topology.Groups[0].ID != "a-group" {
		t.Errorf("groups not sorted: first = %q", doc.Topology.Groups[0].ID)
	}
}

func TestBuildEmptyResourcesNotNil(t *testing.T) {
	b := NewDocumentBuilder(testContext()).
		WithGenerator("test-producer", "0.2.2")
	doc, err := b.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc.Topology.Resources == nil {
		t.Error("Resources should be empty slice, not nil")
	}
}

func TestBuildRedactionMetadata(t *testing.T) {
	ctx := &Context{
		Config: &ProducerConfig{SafeFailureMode: LogAndRedact},
		Clock:  fixedClock,
	}
	b := NewDocumentBuilder(ctx).
		WithGenerator("test-producer", "0.2.2")
	doc, err := b.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc.Metadata.Redacted == nil || !*doc.Metadata.Redacted {
		t.Error("expected Redacted = true for log-and-redact mode")
	}
	if doc.Metadata.RedactionPolicy != LogAndRedact {
		t.Errorf("RedactionPolicy = %q, want %q", doc.Metadata.RedactionPolicy, LogAndRedact)
	}
}

func TestBuildNoRedactionForFailClosed(t *testing.T) {
	b := NewDocumentBuilder(testContext()).
		WithGenerator("test-producer", "0.2.2")
	doc, err := b.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc.Metadata.Redacted != nil {
		t.Error("expected Redacted = nil for fail-closed mode")
	}
	if doc.Metadata.RedactionPolicy != "" {
		t.Errorf("RedactionPolicy = %q, want empty", doc.Metadata.RedactionPolicy)
	}
}

func TestBuildWithScope(t *testing.T) {
	b := NewDocumentBuilder(testContext()).
		WithGenerator("test-producer", "0.2.2").
		WithScope(Scope{
			Providers: []string{"cisco"},
			Sites:     []string{"mxp"},
		})
	doc, err := b.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc.Metadata.Scope == nil {
		t.Fatal("expected scope to be set")
	}
	if len(doc.Metadata.Scope.Providers) != 1 || doc.Metadata.Scope.Providers[0] != "cisco" {
		t.Errorf("Scope.Providers = %v", doc.Metadata.Scope.Providers)
	}
}

func TestBuildDeduplicatesGroupMembers(t *testing.T) {
	b := NewDocumentBuilder(testContext()).
		WithGenerator("test-producer", "0.2.2")
	b.AddResource(mustResource("a", "compute.vm", mustProvider("aws")))
	b.AddResource(mustResource("b", "compute.vm", mustProvider("aws")))
	b.AddResource(mustResource("c", "compute.vm", mustProvider("aws")))

	g := mustGroup("g1", "physical.site")
	g.Members = []string{"b", "a", "b", "c", "a"}
	b.AddGroup(g)

	doc, err := b.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	members := doc.Topology.Groups[0].Members
	want := []string{"a", "b", "c"}
	if len(members) != len(want) {
		t.Fatalf("Members = %v, want %v", members, want)
	}
	for i, v := range want {
		if members[i] != v {
			t.Errorf("Members[%d] = %q, want %q", i, members[i], v)
		}
	}
}

// Duplicate ID detection.
func TestBuildDuplicateResourceIDs(t *testing.T) {
	b := NewDocumentBuilder(testContext()).
		WithGenerator("test-producer", "0.2.2")
	b.AddResource(mustResource("dup-id", "compute.vm", mustProvider("aws")))
	b.AddResource(mustResource("dup-id", "compute.vm", mustProvider("aws")))

	_, err := b.Build()
	if err == nil {
		t.Error("expected error for duplicate resource IDs")
	}
	if !strings.Contains(err.Error(), "duplicate id") {
		t.Errorf("error should mention duplicate id, got: %v", err)
	}
}

func TestBuildDuplicateConnectionIDs(t *testing.T) {
	b := NewDocumentBuilder(testContext()).
		WithGenerator("test-producer", "0.2.2")
	b.AddResource(mustResource("r1", "compute.vm", mustProvider("aws")))
	b.AddResource(mustResource("r2", "compute.vm", mustProvider("aws")))
	b.AddConnection(mustConnection("dup-conn", "network", "r1", "r2"))
	b.AddConnection(mustConnection("dup-conn", "network", "r1", "r2"))

	_, err := b.Build()
	if err == nil {
		t.Error("expected error for duplicate connection IDs")
	}
}

func TestBuildDuplicateIDsAcrossTypes(t *testing.T) {
	b := NewDocumentBuilder(testContext()).
		WithGenerator("test-producer", "0.2.2")
	b.AddResource(mustResource("shared-id", "compute.vm", mustProvider("aws")))
	b.AddGroup(mustGroup("shared-id", "physical.site"))

	_, err := b.Build()
	if err == nil {
		t.Error("expected error for duplicate IDs across resource and group")
	}
}

// Reference integrity.
func TestBuildConnectionBrokenSource(t *testing.T) {
	b := NewDocumentBuilder(testContext()).
		WithGenerator("test-producer", "0.2.2")
	b.AddResource(mustResource("r1", "compute.vm", mustProvider("aws")))
	b.AddConnection(mustConnection("c1", "network", "nonexistent", "r1"))

	_, err := b.Build()
	if err == nil {
		t.Error("expected error for broken connection source reference")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("error should mention the broken ref, got: %v", err)
	}
}

func TestBuildConnectionBrokenTarget(t *testing.T) {
	b := NewDocumentBuilder(testContext()).
		WithGenerator("test-producer", "0.2.2")
	b.AddResource(mustResource("r1", "compute.vm", mustProvider("aws")))
	b.AddConnection(mustConnection("c1", "network", "r1", "nonexistent"))

	_, err := b.Build()
	if err == nil {
		t.Error("expected error for broken connection target reference")
	}
}

func TestBuildGroupBrokenMember(t *testing.T) {
	b := NewDocumentBuilder(testContext()).
		WithGenerator("test-producer", "0.2.2")
	b.AddResource(mustResource("r1", "compute.vm", mustProvider("aws")))

	g := mustGroup("g1", "physical.site")
	g.AddMembers("r1", "nonexistent")
	b.AddGroup(g)

	_, err := b.Build()
	if err == nil {
		t.Error("expected error for broken group member reference")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("error should mention the broken ref, got: %v", err)
	}
}

func TestBuildGroupBrokenChild(t *testing.T) {
	b := NewDocumentBuilder(testContext()).
		WithGenerator("test-producer", "0.2.2")

	g1 := mustGroup("g1", "physical.site")
	g1.AddChildren("nonexistent-child")
	b.AddGroup(g1)

	_, err := b.Build()
	if err == nil {
		t.Error("expected error for broken group child reference")
	}
}

func TestBuildGroupValidChild(t *testing.T) {
	b := NewDocumentBuilder(testContext()).
		WithGenerator("test-producer", "0.2.2")

	g1 := mustGroup("g-parent", "logical.region")
	g2 := mustGroup("g-child", "physical.site")
	g1.AddChildren("g-child")
	b.AddGroup(g1)
	b.AddGroup(g2)

	_, err := b.Build()
	if err != nil {
		t.Fatalf("unexpected error for valid child reference: %v", err)
	}
}

// Extension key validation.
func TestBuildInvalidExtensionKeyOnResource(t *testing.T) {
	b := NewDocumentBuilder(testContext()).
		WithGenerator("test-producer", "0.2.2")

	r := mustResource("r1", "compute.vm", mustProvider("aws"))
	r.Extensions = map[string]any{
		"bad_key": "value",
	}
	b.AddResource(r)

	_, err := b.Build()
	if err == nil {
		t.Error("expected error for invalid extension key")
	}
	if !strings.Contains(err.Error(), "bad_key") {
		t.Errorf("error should mention the bad key, got: %v", err)
	}
}

func TestBuildInvalidExtensionKeyOnConnection(t *testing.T) {
	b := NewDocumentBuilder(testContext()).
		WithGenerator("test-producer", "0.2.2")
	b.AddResource(mustResource("r1", "compute.vm", mustProvider("aws")))
	b.AddResource(mustResource("r2", "compute.vm", mustProvider("aws")))

	c := mustConnection("c1", "network", "r1", "r2")
	c.Extensions = map[string]any{
		"vendor.custom": "value",
	}
	b.AddConnection(c)

	_, err := b.Build()
	if err == nil {
		t.Error("expected error for invalid extension key on connection")
	}
}

func TestBuildInvalidExtensionKeyOnGroup(t *testing.T) {
	b := NewDocumentBuilder(testContext()).
		WithGenerator("test-producer", "0.2.2")

	g := mustGroup("g1", "physical.site")
	g.Extensions = map[string]any{
		"not_namespaced": "value",
	}
	b.AddGroup(g)

	_, err := b.Build()
	if err == nil {
		t.Error("expected error for invalid extension key on group")
	}
}

func TestBuildValidExtensionKeys(t *testing.T) {
	b := NewDocumentBuilder(testContext()).
		WithGenerator("test-producer", "0.2.2")

	r := mustResource("r1", "compute.vm", mustProvider("aws"))
	r.Extensions = map[string]any{
		"osiris.aws": map[string]any{
			"instance_type": "t3.medium",
		},
	}
	b.AddResource(r)

	_, err := b.Build()
	if err != nil {
		t.Fatalf("valid extension keys should pass, got: %v", err)
	}
}

// Secret scanning in Build, the regex is protocol-agnostic.
func TestBuildFailClosedBlocksSecrets(t *testing.T) {
	b := NewDocumentBuilder(testContext()).
		WithGenerator("test-producer", "0.2.2")

	r := mustResource("r1", "compute.vm", mustProvider("aws"))
	r.Properties = map[string]any{
		"connection_string": "postgresql://admin:s3cret@db:5432/app",
	}
	b.AddResource(r)

	_, err := b.Build()
	if err == nil {
		t.Error("expected error: fail-closed should block secrets")
	}
	if !strings.Contains(err.Error(), "secret scanning") {
		t.Errorf("error should mention secret scanning, got: %v", err)
	}
}

func TestBuildLogAndRedactAllowsSecrets(t *testing.T) {
	ctx := &Context{
		Config: &ProducerConfig{SafeFailureMode: LogAndRedact},
		Clock:  fixedClock,
	}
	b := NewDocumentBuilder(ctx).
		WithGenerator("test-producer", "0.2.2")

	r := mustResource("r1", "compute.vm", mustProvider("aws"))
	r.Properties = map[string]any{
		"connection_string": "postgresql://admin:s3cret@db:5432/app",
	}
	b.AddResource(r)

	doc, err := b.Build()
	if err != nil {
		t.Fatalf("log-and-redact should not return error, got: %v", err)
	}
	if doc.Metadata.Redacted == nil || !*doc.Metadata.Redacted {
		t.Error("expected Redacted = true")
	}
}

func TestBuildOffModeSkipsScanning(t *testing.T) {
	ctx := &Context{
		Config: &ProducerConfig{SafeFailureMode: Off},
		Clock:  fixedClock,
	}
	b := NewDocumentBuilder(ctx).
		WithGenerator("test-producer", "0.2.2")

	r := mustResource("r1", "compute.vm", mustProvider("aws"))
	r.Properties = map[string]any{
		"api_key": "AKIAIOSFODNN7EXAMPLE",
	}
	b.AddResource(r)

	_, err := b.Build()
	if err != nil {
		t.Fatalf("off mode should skip scanning, got: %v", err)
	}
}

func TestBuildCleanDocumentPassesScanning(t *testing.T) {
	b := NewDocumentBuilder(testContext()).
		WithGenerator("test-producer", "0.2.2")

	r := mustResource("r1", "compute.vm", mustProvider("aws"))
	r.Properties = map[string]any{
		"hostname":  "web-01",
		"memory_gb": 64,
	}
	b.AddResource(r)

	_, err := b.Build()
	if err != nil {
		t.Fatalf("clean document should pass scanning, got: %v", err)
	}
}
