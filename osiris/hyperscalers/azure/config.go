// config.go - Configuration types for the Azure OSIRIS JSON producer.
// Defines subscription-scoped targeting and runtime settings for Azure
// resource collection via the Azure CLI.
//
// Output hierarchy follows a tenant-scoped layout designed for multi-tenant
// and multi-subscription enterprise environments:
//
//	<output-dir>/
//	  <TenantID>/
//	    <timestamp>/
//	      <SubscriptionName>.json
//
// This structure ensures that each OSIRIS document is a self-contained
// subscription snapshot, while the directory tree groups them by tenant
// and point-in-time for easy correlation by consumers.
//
// For an introduction to OSIRIS JSON Producer for Microsoft Azure see:
// "[OSIRIS-JSON-AZURE]."
//
// [OSIRIS-JSON-AZURE]: https://osirisjson.org/en/docs/producers/hyperscalers/microsoft-azure

package azure

import (
	"path/filepath"
	"strings"
	"time"
)

// SubscriptionTarget describes a single Azure subscription to collect from.
type SubscriptionTarget struct {
	// SubscriptionID is the Azure subscription UUID.
	SubscriptionID string

	// SubscriptionName is a human-readable label (from CSV or --subscription flag).
	SubscriptionName string

	// TenantID is the Azure AD / Entra ID tenant UUID (optional; resolved from CLI context if empty).
	TenantID string

	// Environment is the deployment stage (e.g. "np" (non-production), "pr" (production), "dv" (development)). Human-only metadata.
	Environment string

	// Region filters collection to a specific Azure region (optional; empty = all regions).
	Region string

	// Notes is free-text operator notes (ignored by producer).
	Notes string
}

// Config carries runtime settings resolved from CLI flags and CSV.
type Config struct {
	Targets         []SubscriptionTarget
	OutputDir       string // batch/export mode; empty = stdout single mode.
	Timestamp       string // shared timestamp for the batch run.
	SafeFailureMode string // "fail-closed" | "log-and-redact" | "off".
	Purpose         string // OSIRIS JSON spec chapter 13.1.3 output grade: "documentation" (default) | "audit".
}

// IsBatch returns true when the run targets multiple subscriptions or has an output dir.
func (c *Config) IsBatch() bool {
	return len(c.Targets) > 1 || c.OutputDir != ""
}

// OutputPath returns the hierarchical output path for a subscription target:
// baseDir/TenantID/timestamp/SubscriptionName.json
//
// The tenant ID is resolved at collection time (from subscription metadata).
// If the tenant ID is not yet known, the subscription-level TenantID field is used.
func OutputPath(baseDir, tenantID, timestamp string, t SubscriptionTarget) string {
	tenant := tenantID
	if tenant == "" {
		tenant = t.TenantID
	}
	if tenant == "" {
		tenant = "unknown-tenant"
	}

	name := t.SubscriptionName
	if name == "" {
		name = t.SubscriptionID
	}
	name = sanitizeFilename(name)

	return filepath.Join(baseDir, tenant, timestamp, name+".json")
}

// sanitizeFilename replaces filesystem-unsafe characters with dashes.
func sanitizeFilename(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|' {
			return '-'
		}
		return r
	}, s)
}

// FormatTimestamp returns a filesystem-safe UTC timestamp string for batch run directories.
func FormatTimestamp(t time.Time) string {
	return t.UTC().Format("2006-01-02T15-04-05Z")
}
