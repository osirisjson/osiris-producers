// dispatch.go - CLI entry point, batch runner, and --help for the
// Cisco ACI/APIC OSIRIS JSON producer.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package apic

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"go.osirisjson.org/producers/osiris/network/cisco/run"
	"go.osirisjson.org/producers/pkg/sdk"
)

// Run is the CLI entry point for the APIC producer. It receives the
// arguments after "apic".
func Run(args []string) error {
	// Only the long form is checked here: -h is the documented short
	// flag for --host (see flags.go), so it must reach ParseFlags
	// instead of being intercepted as a help request.
	if len(args) > 0 {
		switch args[0] {
		case "--help":
			printHelp()
			return nil
		case "template":
			return runTemplate(args[1:])
		}
	}

	cfg, err := ParseFlags(args, run.PromptVisible, run.PromptPassword)
	if err != nil {
		return err
	}
	cfg.Timestamp = run.FormatTimestamp(time.Now())

	if cfg.Mode == run.ModeBatch {
		return runBatch(cfg)
	}
	return runSingle(cfg)
}

// templateExampleHost is the documentation-only address shown in the
// generated --secrets-file/CSV template skeletons (RFC 5737).
const templateExampleHost = "192.0.2.1"

// runTemplate writes this producer's CSV batch template
// (cisco-apic-template.csv) and both --secrets-file shapes
// (cisco-apic-secrets.json, cisco-apic-secrets-multihost.json) via the
// shared run.GenerateTemplates.
func runTemplate(args []string) error {
	if len(args) == 0 || (args[0] != "--generate" && args[0] != "-g") {
		fmt.Println("Usage: osirisjson-producer cisco apic template --generate")
		return nil
	}
	return run.GenerateTemplates("apic", templateExampleHost)
}

// runSingle executes a single-target collection and writes it to a
// local file: cisco-apic-<timestamp>-<hostname>.json.
func runSingle(cfg *Config) error {
	target := cfg.Targets[0]
	logger := defaultLogger()

	producer := &Producer{target: target, cfg: cfg}
	ctx := newSDKContext(cfg)
	ctx.Logger = logger

	doc, err := producer.Collect(ctx)
	if err != nil {
		return fmt.Errorf("collection failed for %s: %w", target.Host, err)
	}

	data, err := sdk.MarshalDocument(doc)
	if err != nil {
		return fmt.Errorf("marshal failed: %w", err)
	}

	name := target.Hostname
	if name == "" {
		name = target.Host
	}
	name, err = run.SanitizePathSegment(name)
	if err != nil {
		return fmt.Errorf("invalid output filename: %w", err)
	}
	filename := fmt.Sprintf("cisco-apic-%s-%s.json", cfg.Timestamp, name)

	// 0600: emitted documents are infrastructure inventory snapshots
	// (hostnames, serials, topology) and should not be world/group
	// readable by default, only the invoking user.
	if err := os.WriteFile(filename, data, 0600); err != nil {
		return fmt.Errorf("failed to write %s: %w", filename, err)
	}
	fmt.Fprintf(os.Stderr, "Saved to %s\n", filename)
	return nil
}

// runBatch executes the batch: one document per target, written to the
// hierarchical output path OutputDir/Datacenter/Floor/Room/Rack/<file>.json
// (see run.OutputPath). A single target's failure is logged and
// skipped; only a fully failed run (succeeded == 0) is fatal.
func runBatch(cfg *Config) error {
	logger := defaultLogger()

	if err := os.MkdirAll(cfg.OutputDir, 0755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	var succeeded, failed int

	for _, target := range cfg.Targets {
		log := logger.With("target", target.Host, "hostname", target.Hostname)
		log.Info("collecting")

		producer := &Producer{target: target, cfg: cfg}
		ctx := newSDKContext(cfg)
		ctx.Logger = log

		doc, err := producer.Collect(ctx)
		if err != nil {
			log.Error("collection failed", "error", err)
			failed++
			continue
		}

		data, err := sdk.MarshalDocument(doc)
		if err != nil {
			log.Error("marshal failed", "error", err)
			failed++
			continue
		}

		outPath, err := run.OutputPath(cfg.OutputDir, cfg.Timestamp, target)
		if err != nil {
			log.Error("invalid output path", "error", err)
			failed++
			continue
		}
		if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
			log.Error("creating output path", "error", err, "path", outPath)
			failed++
			continue
		}
		if err := os.WriteFile(outPath, data, 0600); err != nil {
			log.Error("write failed", "error", err, "path", outPath)
			failed++
			continue
		}

		log.Info("written", "path", outPath)
		succeeded++
	}

	if succeeded == 0 {
		return fmt.Errorf("all %d targets failed", failed)
	}
	if failed > 0 {
		logger.Warn("batch completed with failures", "succeeded", succeeded, "failed", failed)
	} else {
		logger.Info("batch completed", "succeeded", succeeded)
	}
	return nil
}

func defaultLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, nil))
}

// newSDKContext maps this producer's own Config onto the generic
// sdk.ProducerConfig every sdk.Producer.Collect call expects.
func newSDKContext(cfg *Config) *sdk.Context {
	return sdk.NewContext(&sdk.ProducerConfig{
		Purpose:         cfg.Purpose,
		IncludeRawBody:  cfg.IncludeRawBody,
		SafeFailureMode: cfg.SafeFailureMode,
	})
}

func printHelp() {
	fmt.Print(`osirisjson-producer cisco apic - Cisco ACI/APIC fabric topology OSIRIS JSON producer

Usage:
  osirisjson-producer cisco apic [flags]

Flags:
  -h, --host          	Target host (IP or FQDN, optionally with :port); prompted for interactively when omitted
  -u, --username      	Username for authentication; prompted for interactively when omitted
  -P, --port          	Override port
  --secrets-file      	JSON file with {host, username, password} (see below); overrides -h/-u when they omit a field
  -s, --source        	CSV file for batch mode (mutually exclusive with -h/--host)
  -o, --output        	Output directory for batch mode

  --include-raw-body  	Attach each collected command's full, unmodified response under
                      	extensions["osiris.cisco"] on the owning resource (requires --purpose audit;
                      	a lossless fallback for fields not yet modeled)
  --safe-failure-mode 	Secret handling: fail-closed, log-and-redact, off (default: fail-closed)
  --insecure          	Skip TLS certificate verification

--secrets-file accepts two shapes, each generated as its own file by
"apic template --generate" (see below) so you never have to guess the
JSON by hand:
  cisco-apic-secrets.json           a single login: {"host", "username", "password"}
  cisco-apic-secrets-multihost.json different logins per target in a batch,
                                    matched by exact host/IP or CIDR, with a
                                    "default" fallback for anything unmatched
The file must be a regular file (not a symlink) readable only by its
owner (e.g. chmod 0600) a looser file is rejected.

Batch mode (-s/--source): a CSV file with columns
datacenter,floor,room,rack,hostname,management_ip,port
datacenter/floor/room/rack are optional; when present they build the output
directory structure: <output>/Datacenter/Floor/Room/Rack/Hostname.json.

  <name> template --generate	Write cisco-apic-template.csv and both --secrets-file skeletons

Output:
  single mode saves to: cisco-apic-<timestamp>-<hostname>.json (0600 permissions)
  batch mode saves to:  <output>/Datacenter/Floor/Room/Rack/Hostname.json (0600 permissions)

Examples:
  osirisjson-producer cisco apic -h 192.0.2.1 -u username
  osirisjson-producer cisco apic -h 192.0.2.1 -u username --purpose audit
  osirisjson-producer cisco apic -s datacenter.csv -o ./output -u username
  osirisjson-producer cisco apic template --generate
`)
}
