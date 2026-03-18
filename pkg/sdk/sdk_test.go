// sdk_test.go - Tests for NewContext defaults, EnvOrDefault and safe failure mode configuration.
//
// For an introduction to OSIRIS JSON Producer Development Guidelines see:
// "[OSIRIS-PRODUCER-GUIDELINES]."
//
// [OSIRIS-PRODUCER-GUIDELINES]: https://osirisjson.org/en/docs/getting-started/osiris-producer-guidelines

package sdk

import (
	"testing"
)

func TestNewContextDefaults(t *testing.T) {
	ctx := NewContext(nil)

	if ctx.Config == nil {
		t.Fatal("Config should not be nil")
	}
	if ctx.Config.SafeFailureMode != FailClosed {
		t.Errorf("SafeFailureMode = %q, want %q", ctx.Config.SafeFailureMode, FailClosed)
	}
	if ctx.Logger == nil {
		t.Error("Logger should not be nil")
	}
	if ctx.Clock == nil {
		t.Error("Clock should not be nil")
	}
	// Clock should return a non-zero time.
	if ctx.Clock().IsZero() {
		t.Error("Clock should return current time, got zero")
	}
}

func TestNewContextWithConfig(t *testing.T) {
	cfg := &ProducerConfig{
		OutputPath:      "/tmp/out.json",
		SafeFailureMode: LogAndRedact,
	}
	ctx := NewContext(cfg)

	if ctx.Config.OutputPath != "/tmp/out.json" {
		t.Errorf("OutputPath = %q, want %q", ctx.Config.OutputPath, "/tmp/out.json")
	}
	if ctx.Config.SafeFailureMode != LogAndRedact {
		t.Errorf("SafeFailureMode = %q, want %q", ctx.Config.SafeFailureMode, LogAndRedact)
	}
}

func TestNewContextDefaultsSafeFailureMode(t *testing.T) {
	cfg := &ProducerConfig{} // SafeFailureMode is empty.
	ctx := NewContext(cfg)

	if ctx.Config.SafeFailureMode != FailClosed {
		t.Errorf("SafeFailureMode = %q, want %q (should default to fail-closed)",
			ctx.Config.SafeFailureMode, FailClosed)
	}
}

func TestEnvOrDefault(t *testing.T) {
	// Unset env var should return fallback.
	got := EnvOrDefault("OSIRIS_TEST_NONEXISTENT_VAR_12345", "fallback")
	if got != "fallback" {
		t.Errorf("EnvOrDefault for unset var = %q, want %q", got, "fallback")
	}

	// Set env var should return its value.
	t.Setenv("OSIRIS_TEST_VAR", "custom-value")
	got = EnvOrDefault("OSIRIS_TEST_VAR", "fallback")
	if got != "custom-value" {
		t.Errorf("EnvOrDefault for set var = %q, want %q", got, "custom-value")
	}
}

func TestEnvOrDefaultEmptyValue(t *testing.T) {
	// Empty env var should return fallback.
	t.Setenv("OSIRIS_TEST_EMPTY", "")
	got := EnvOrDefault("OSIRIS_TEST_EMPTY", "fallback")
	if got != "fallback" {
		t.Errorf("EnvOrDefault for empty var = %q, want %q", got, "fallback")
	}
}
