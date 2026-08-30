// config.go - Runtime configuration for the Cisco ACI/APIC OSIRIS JSON
// producer.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package apic

import "go.osirisjson.org/producers/osiris/network/cisco/run"

// Config carries runtime settings resolved from CLI flags or CSV.
type Config struct {
	Targets        []run.TargetConfig
	Mode           string // run.ModeSingle | run.ModeBatch.
	OutputDir      string // batch only; empty = single-mode local file.
	Timestamp      string // filesystem-safe UTC timestamp for output filenames.
	Purpose        string
	IncludeRawBody bool

	SafeFailureMode string
	InsecureTLS     bool
}
