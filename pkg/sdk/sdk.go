// Package sdk defines the constants, Producer interface, execution context and configuration.
// Defines the contract every OSIRIS JSON producer must implement (Collect) and the
// runtime context (Config, Logger, Clock) passed to producers at execution time.
//
// For an introduction to OSIRIS JSON Producer Development Guidelines see:
// "[OSIRIS-PRODUCER-GUIDELINES]."
//
// [OSIRIS-PRODUCER-GUIDELINES]: https://osirisjson.org/en/docs/getting-started/osiris-producer-guidelines
//
// [OSIRIS JSON Schema]: https://osirisjson.org/schema/v1.0/osiris.schema.json
package sdk

import (
	"log/slog"
	"os"
	"time"
)

// SpecVersion is the OSIRIS JSON specification version targeted by this SDK.
const SpecVersion = "1.0.0"

// OSIRIS JSON Core SchemaURI is the canonical JSON Schema URI for OSIRIS v1.0.
const SchemaURI = "https://osirisjson.org/schema/v1.0/osiris.schema.json"

// SafeFailureMode defines how the producer handles detected secrets.
const (
	FailClosed   = "fail-closed"
	LogAndRedact = "log-and-redact"
	Off          = "off"
)

// OSIRIS JSON Producer is the contract every vendor backend MUST satisfy.
type Producer interface {
	// Collect discovers vendor inventory and returns an assembled OSIRIS document.
	// Partial failures SHOULD be logged and skipped (non-fatal).
	Collect(ctx *Context) (*Document, error)
}

// Context provides shared runtime state for a producer execution.
type Context struct {
	Config *ProducerConfig
	Logger *slog.Logger
	Clock  func() time.Time
}

// ProducerConfig carries common configuration for all producers.
// Vendor-specific fields are defined by each producer as an embedded struct.
type ProducerConfig struct {
	OutputPath      string `json:"output_path,omitempty"`
	ProfileHint     string `json:"profile_hint,omitempty"`
	DetailLevel     string `json:"detail_level,omitempty"`
	SafeFailureMode string `json:"safe_failure_mode,omitempty"`
	// Purpose shapes the emitted document per OSIRIS JSON spec §13.1.3.
	// Valid values: "documentation" (minimal, default) or "audit" (full detail).
	// See pkg/osirismeta for parsing, defaults and projection helpers.
	Purpose string `json:"purpose,omitempty"`
}

// NewContext creates a Context with sensible defaults.
// Logger defaults to slog writing to stderr. Clock defaults to time.Now.
func NewContext(cfg *ProducerConfig) *Context {
	if cfg == nil {
		cfg = &ProducerConfig{}
	}
	if cfg.SafeFailureMode == "" {
		cfg.SafeFailureMode = FailClosed
	}
	return &Context{
		Config: cfg,
		Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
		Clock:  time.Now,
	}
}

// EnvOrDefault returns the value of the environment variable named by key,
// or fallback if the variable is not set or is empty.
func EnvOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
