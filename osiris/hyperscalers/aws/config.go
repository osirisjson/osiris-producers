// config.go - Configuration types for the Amazon AWS OSIRIS JSON producer.
// Defines account-scoped targeting and runtime settings for AWS resource
// collection via the AWS Go SDK v2.
//
// Output hierarchy follows an account-scoped layout designed for multi-account
// and multi-region enterprise environments.
//
// For an introduction to OSIRIS JSON Producer for Amazon Web Services see:
// [OSIRIS-JSON-AWS]: https://osirisjson.org/en/docs/producers/hyperscalers/amazon-aws
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/docs/spec/v10/00-preface

package aws

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// DefaultRegions lists the standard AWS regions to iterate when --all-regions is used.
var DefaultRegions = []string{
	"us-east-1",
	"us-east-2",
	"us-west-1",
	"us-west-2",
	"ca-central-1",
	"eu-west-1",
	"eu-west-2",
	"eu-west-3",
	"eu-central-1",
	"eu-north-1",
	"ap-south-1",
	"ap-northeast-1",
	"ap-northeast-2",
	"ap-northeast-3",
	"ap-southeast-1",
	"ap-southeast-2",
	"sa-east-1",
}

// GlobalRegion is the region used for account-level global resources
// (Route53, Global Accelerator, WAFv2 global). Global resources are merged
// into this region's OSIRIS JSON document.
const GlobalRegion = "us-east-1"

// AccountTarget describes a single AWS account to collect from.
type AccountTarget struct {
	// AccountID is the AWS account number (12-digit).
	AccountID string

	// AccountName is a human-readable label (from CSV or interactive picker).
	AccountName string

	// Profile is the AWS CLI / SDK profile name for authentication.
	Profile string

	// Regions lists the specific regions to collect. Empty = all default regions.
	Regions []string

	// Environment is the deployment stage (e.g. "np", "pr", "dv"). Human-only metadata.
	Environment string

	// Notes is free-text user notes (ignored by producer).
	Notes string
}

// Config carries runtime settings resolved from CLI flags and CSV.
type Config struct {
	Targets         []AccountTarget
	OutputDir       string // batch/export mode; empty = single mode.
	Timestamp       string // shared timestamp for the batch run.
	SafeFailureMode string // "fail-closed" | "log-and-redact" | "off".
	Purpose         string // OSIRIS JSON spec chapter 13.1.3 output grade: "documentation" (default) | "audit".
	IncludeRawBody  bool   // when true and --purpose audit, attach the full SDK response body under extensions["osiris.aws.sdk"].body.
}

// IsBatch returns true when the run targets multiple accounts or has an output dir.
func (c *Config) IsBatch() bool {
	return len(c.Targets) > 1 || c.OutputDir != ""
}

// OutputPath returns the flat output path for a region target:
// baseDir/amazon-aws-<timestamp>-<accountID>-<region>.json
func OutputPath(baseDir, accountID, timestamp, region string) string {
	if accountID == "" {
		accountID = "unknown-account"
	}
	return filepath.Join(baseDir, fmt.Sprintf("amazon-aws-%s-%s-%s.json", timestamp, region, accountID))
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
