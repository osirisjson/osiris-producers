// config.go - Runtime configuration for the
// Cisco NX-OS OSIRIS JSON producer.
//
// NX-OS is normally one independently authenticated switch per
// collection target, so, this producer keeps ideally a CSV
// batch mode (many devices, one document each). This package owns its
// own flag surface and Config type (see flags.go) instead of going
// through the shared osiris/network/cisco/run.ParseFlags/RunConfig
// orchestrator only genuinely vendor-agnostic mechanisms are reused
// from that package directly (TargetConfig, ParseHostPort, prompting,
// credential-file loading/resolution, CSV parsing, path sanitization,
// timestamp formatting), matching the same reuse pattern vmanage
// already established and the parent Cisco architecture review's own
// direction to move orchestration into each producer.
//
// OSIRIS JSON Producer for Cisco NX-OS introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package nxos

import "go.osirisjson.org/producers/osiris/network/cisco/run"

// defaultPort is the NX-API HTTPS port used when --host has no
// explicit :port and --port is not given.
const defaultPort = 443

// Config carries runtime settings resolved from CLI flags or CSV.
type Config struct {
	Targets   []run.TargetConfig
	Mode      string // run.ModeSingle | run.ModeBatch.
	OutputDir string // batch only; empty = single-mode local file.
	Timestamp string // filesystem-safe UTC timestamp for output filenames.

	// Purpose is the OSIRIS JSON spec chapter 13.1.3 output grade
	// ("documentation" default, "audit" full detail) see
	// pkg/osirismeta.
	Purpose string

	// IncludeRawBody attaches each collected NX-API command's full,
	// unmodified response body under extensions["osiris.cisco"] on the
	// device resource (see wantRawBody in nxos.go), a lossless fallback
	// for fields not yet modeled. Only takes effect when Purpose is
	// "audit".
	IncludeRawBody bool

	SafeFailureMode string
	InsecureTLS     bool
}
