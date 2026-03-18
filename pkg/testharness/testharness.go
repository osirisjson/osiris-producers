// Package testharness provides test utilities for OSIRIS producers.
//
// It implements the golden-file workflow defined in [OSIRIS-ADG-PR-1.0 chapter 4]
// and provides deterministic test contexts for reproducible producer tests.
//
// For an introduction to OSIRIS JSON Producer Development Guidelines see:
// "[OSIRIS-PRODUCER-GUIDELINES]."
//
// [OSIRIS-ADG-PR-1.0 chapter 4]: https://github.com/osirisjson/osiris/blob/dev/docs/guidelines/v1.0/OSIRIS-PRODUCER-GUIDELINES.md#4-quality-assurance
// [OSIRIS-PRODUCER-GUIDELINES]: https://osirisjson.org/en/docs/getting-started/osiris-producer-guidelines

package testharness

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.osirisjson.org/producers/pkg/sdk"
)

// TestOption configures a test context.
type TestOption func(*sdk.Context)

// WithConfig sets the producer configuration on the test context.
func WithConfig(cfg *sdk.ProducerConfig) TestOption {
	return func(ctx *sdk.Context) {
		ctx.Config = cfg
	}
}

// WithClock overrides the clock function on the test context.
func WithClock(clock func() time.Time) TestOption {
	return func(ctx *sdk.Context) {
		ctx.Clock = clock
	}
}

// FixedTestTime is the canonical fixed time used in test contexts.
var FixedTestTime = time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)

/*
NewTestContext returns a Context with deterministic defaults for testing

Defaults:
  - Clock: fixed to 2026-01-15T10:00:00Z
  - Logger: writes to t.Log for test visibility
  - Config: minimal defaults with fail-closed safe failure mode
*/
func NewTestContext(t *testing.T, opts ...TestOption) *sdk.Context {
	t.Helper()

	ctx := &sdk.Context{
		Config: &sdk.ProducerConfig{
			SafeFailureMode: sdk.FailClosed,
		},
		Logger: slog.New(slog.NewTextHandler(testWriter{t}, nil)),
		Clock:  func() time.Time { return FixedTestTime },
	}
	for _, opt := range opts {
		opt(ctx)
	}
	return ctx
}

// testWriter adapts testing.T to io.Writer for slog.
type testWriter struct {
	t *testing.T
}

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Helper()
	w.t.Log(string(bytes.TrimRight(p, "\n")))
	return len(p), nil
}

// LoadFixture reads a test fixture file and fails the test if it cannot be read.
func LoadFixture(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to load fixture %q: %v", path, err)
	}
	return data
}

// AssertGolden compares the document against a golden file.
// On mismatch it fails the test with a diff summary.
// When the OSIRIS_UPDATE_GOLDEN env var is set, it overwrites the golden file instead.
func AssertGolden(t *testing.T, got *sdk.Document, goldenPath string) {
	t.Helper()

	gotBytes, err := sdk.MarshalDocument(got)
	if err != nil {
		t.Fatalf("failed to marshal document: %v", err)
	}

	if os.Getenv("OSIRIS_UPDATE_GOLDEN") != "" {
		dir := filepath.Dir(goldenPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("failed to create golden dir %q: %v", dir, err)
		}
		if err := os.WriteFile(goldenPath, gotBytes, 0o644); err != nil {
			t.Fatalf("failed to write golden file %q: %v", goldenPath, err)
		}
		t.Logf("updated golden file: %s", goldenPath)
		return
	}

	wantBytes, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("failed to read golden file %q (run with OSIRIS_UPDATE_GOLDEN=1 to create): %v", goldenPath, err)
	}

	if !bytes.Equal(gotBytes, wantBytes) {
		t.Errorf("golden file mismatch: %s\n--- got (first 500 bytes) ---\n%s\n--- want (first 500 bytes) ---\n%s",
			goldenPath,
			truncate(string(gotBytes), 500),
			truncate(string(wantBytes), 500),
		)
	}
}

// AssertDeterministic runs the producer twice with the same context and asserts
// byte-identical JSON output. This catches non-deterministic ID generation,
// unstable map iteration, or inconsistent timestamp handling.
func AssertDeterministic(t *testing.T, p sdk.Producer, ctx *sdk.Context) {
	t.Helper()

	doc1, err := p.Collect(ctx)
	if err != nil {
		t.Fatalf("first Collect failed: %v", err)
	}
	data1, err := sdk.MarshalDocument(doc1)
	if err != nil {
		t.Fatalf("first MarshalDocument failed: %v", err)
	}

	doc2, err := p.Collect(ctx)
	if err != nil {
		t.Fatalf("second Collect failed: %v", err)
	}
	data2, err := sdk.MarshalDocument(doc2)
	if err != nil {
		t.Fatalf("second MarshalDocument failed: %v", err)
	}

	if !bytes.Equal(data1, data2) {
		t.Error("producer output is not deterministic: two Collect calls with the same input produced different output")
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
