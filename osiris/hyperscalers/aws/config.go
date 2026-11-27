// config.go - Configuration types for the Amazon AWS OSIRIS JSON
// producer.
// Defines account-scoped targeting and runtime settings for AWS
// resource collection via the AWS Go SDK v2.
//
// Output hierarchy follows an account-scoped layout designed for
// multi-account and multi-region enterprise environments.
//
// For an introduction to OSIRIS JSON Producer for Amazon AWS see:
// [OSIRIS-JSON-AWS]: https://docs.osirisjson.org/osiris-producers/hyperscalers/amazon-aws/
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package aws

import (
	"path/filepath"
	"strings"
	"time"
)

// DefaultRegions lists the standard AWS regions to iterate when
// --all-regions is used.
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
// (Route53, Global Accelerator, WAFv2 global). Global resources are
// merged into this region's OSIRIS JSON document.
const GlobalRegion = "us-east-1"

// AccountTarget describes a single AWS account to collect from.
type AccountTarget struct {
	// AccountID is the AWS account number (12-digit).
	AccountID string

	// AccountName is a human-readable label
	// (from CSV or interactive picker).
	AccountName string

	// Profile is the AWS CLI / SDK profile name for authentication.
	Profile string

	// Regions lists the specific regions to collect. Empty = all
	// default regions.
	Regions []string

	// Environment is the deployment stage. Human-only metadata.
	Environment string

	// Notes is free-text user notes (ignored by producer).
	Notes string
}

// Config carries runtime settings resolved from CLI flags and CSV.
type Config struct {
	Targets         []AccountTarget
	OutputDir       string // batch/export mode; empty = single mode.
	Timestamp       string // shared timestamp for the batch run.
	SafeFailureMode string // "fail-closed" | "log-and-redact".
	Purpose         string // OSIRIS JSON spec chapter 13.1.3 output grade: "documentation" (default) | "audit".
	IncludeRawBody  bool   // when true and --purpose audit, attach the redacted SDK response body under extensions["osiris.aws.sdk"].body.
}

// IsBatch returns true when the run targets multiple accounts or has a
// output dir.
func (c *Config) IsBatch() bool {
	return len(c.Targets) > 1 || c.OutputDir != ""
}

// OutputPath returns the flat output path for a region target:
// baseDir/amazon-aws-<timestamp>-<region>-<name>.json. name is the
// sanitized target name (see targetName). It returns an error when the
// region is not a valid region code or the file name is unsafe.
func OutputPath(baseDir, name, timestamp, region string) (string, error) {
	if name == "" {
		name = "unknown-account"
	}
	file, err := outputFileName(timestamp, region, name)
	if err != nil {
		return "", err
	}
	return filepath.Join(baseDir, file), nil
}

// sanitizeFilename replaces filesystem-unsafe and control characters
// with dashes, collapses ".." and strips leading dots, so the result is
// always a single path element. An empty result becomes "unknown".
func sanitizeFilename(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|' || r < 0x20 || r == 0x7f {
			return '-'
		}
		return r
	}, s)
	for strings.Contains(s, "..") {
		s = strings.ReplaceAll(s, "..", "-")
	}
	s = strings.TrimLeft(s, ".")
	if s == "" {
		return "unknown"
	}
	return s
}

// FormatTimestamp returns a filesystem-safe UTC timestamp string for
// batch run directories.
func FormatTimestamp(t time.Time) string {
	return t.UTC().Format("2006-01-02T15-04-05Z")
}
