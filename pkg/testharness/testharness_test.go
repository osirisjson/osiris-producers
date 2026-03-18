// testharness_test.go - Tests for the test harness utilities (context defaults, fixture loading,
// golden-file comparison, determinism assertions).
//
// For an introduction to OSIRIS JSON Producer Development Guidelines see:
// "[OSIRIS-PRODUCER-GUIDELINES]."
//
// [OSIRIS-PRODUCER-GUIDELINES]: https://osirisjson.org/en/docs/getting-started/osiris-producer-guidelines

package testharness

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.osirisjson.org/producers/pkg/sdk"
)

func TestNewTestContextDefaults(t *testing.T) {
	ctx := NewTestContext(t)

	if ctx.Config == nil {
		t.Fatal("Config should not be nil")
	}
	if ctx.Config.SafeFailureMode != sdk.FailClosed {
		t.Errorf("SafeFailureMode = %q, want %q", ctx.Config.SafeFailureMode, sdk.FailClosed)
	}
	if ctx.Logger == nil {
		t.Error("Logger should not be nil")
	}

	got := ctx.Clock()
	if got != FixedTestTime {
		t.Errorf("Clock() = %v, want %v", got, FixedTestTime)
	}
}

func TestNewTestContextWithConfig(t *testing.T) {
	cfg := &sdk.ProducerConfig{
		DetailLevel:     "detailed",
		SafeFailureMode: sdk.LogAndRedact,
	}
	ctx := NewTestContext(t, WithConfig(cfg))

	if ctx.Config.DetailLevel != "detailed" {
		t.Errorf("DetailLevel = %q, want %q", ctx.Config.DetailLevel, "detailed")
	}
	if ctx.Config.SafeFailureMode != sdk.LogAndRedact {
		t.Errorf("SafeFailureMode = %q, want %q", ctx.Config.SafeFailureMode, sdk.LogAndRedact)
	}
}

func TestNewTestContextWithClock(t *testing.T) {
	custom := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	ctx := NewTestContext(t, WithClock(func() time.Time { return custom }))

	if ctx.Clock() != custom {
		t.Errorf("Clock() = %v, want %v", ctx.Clock(), custom)
	}
}

func TestAssertGoldenUpdate(t *testing.T) {
	// Create a temp dir for the golden file.
	dir := t.TempDir()
	goldenPath := filepath.Join(dir, "golden.json")

	ctx := NewTestContext(t)
	b := sdk.NewDocumentBuilder(ctx).
		WithGenerator("test-producer", "0.1.0")
	doc, err := b.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Set env to update mode.
	t.Setenv("OSIRIS_UPDATE_GOLDEN", "1")
	AssertGolden(t, doc, goldenPath)

	// Verify file was created.
	if _, err := os.Stat(goldenPath); err != nil {
		t.Fatalf("golden file not created: %v", err)
	}

	// Now unset and verify it matches.
	t.Setenv("OSIRIS_UPDATE_GOLDEN", "")
	AssertGolden(t, doc, goldenPath)
}

// mockProducer is a trivial Producer for testing AssertDeterministic.
type mockProducer struct{}

func (m *mockProducer) Collect(ctx *sdk.Context) (*sdk.Document, error) {
	p, _ := sdk.NewProvider("test")
	r1, _ := sdk.NewResource("res-a", "compute.vm", p)
	r2, _ := sdk.NewResource("res-b", "compute.vm", p)

	b := sdk.NewDocumentBuilder(ctx).
		WithGenerator("mock-producer", "0.1.0")
	b.AddResource(r1)
	b.AddResource(r2)
	return b.Build()
}

func TestAssertDeterministic(t *testing.T) {
	ctx := NewTestContext(t)
	p := &mockProducer{}
	AssertDeterministic(t, p, ctx)
}
