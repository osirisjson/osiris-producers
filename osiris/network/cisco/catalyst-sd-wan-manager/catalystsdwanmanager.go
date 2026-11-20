// Package catalystsdwanmanager implements the Cisco Catalyst SD-WAN
// Manager OSIRIS JSON Producer.
//
// A single SD-WAN Manager login fans out into one document per
// selected site (logical.site group of the devices sharing a site-id).
// Each document also carries neighbor stubs for the devices and
// transport interfaces of other sites that its tunnels and OMP
// sessions reach, with the same IDs in every document, so an OSIRIS
// JSON consumer merges the per-site documents by ID. It is dispatched
// directly by osiris/network/cisco/cisco.go's Run function.
//
// OSIRIS JSON Producer for Cisco Catalyst SD-WAN Manager introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification
package catalystsdwanmanager

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"go.osirisjson.org/producers/osiris/network/cisco/run"
	"go.osirisjson.org/producers/pkg/sdk"
)

const (
	generatorName    = "osirisjson-producer-cisco-catalyst-sdwan-manager"
	generatorVersion = "0.2.0"
	generatorURL     = "https://docs.osirisjson.org/osiris-producers/network/cisco"
)

// Run is the CLI entry point for the SD-WAN Manager producer.
// It receives the arguments after "catalyst-sdwan-manager".
func Run(args []string) error {
	// Only the long form is checked here: -h is the documented short
	// flag for --host (see flags.go), so it must reach ParseFlags
	// instead of being intercepted as a help request. This mirrors
	// osiris/network/cisco/cisco.go's runSubProducer, which has the
	// same -h/--host vs. -h/help ambiguity for apic/nxos/iosxe.
	if len(args) > 0 {
		switch args[0] {
		case "--help":
			printHelp()
			return nil
		case "template":
			return runTemplate(args[1:])
		}
	}

	cfg, err := ParseFlags(args, promptVisible, run.PromptPassword)
	if err != nil {
		return err
	}

	return runExport(cfg)
}

// runTemplate writes both --secrets-file shapes, flat
// (cisco-catalyst-sdwan-manager-secrets.json) and multi-host
// (cisco-catalyst-sdwan-manager-secrets-multihost.json) via the shared
// run.WriteTemplateFile SD-WAN Manager has no CSV batch mode, so unlike
// apic/iosxe/nxos it does not call run.GenerateTemplates only the two
// --secrets-file variants apply here, not a CSV template.
func runTemplate(args []string) error {
	if len(args) == 0 || (args[0] != "--generate" && args[0] != "-g") {
		fmt.Println("Usage: osirisjson-producer cisco sdwan template --generate")
		return nil
	}
	if err := run.WriteTemplateFile("cisco-catalyst-sdwan-manager-secrets.json", run.CredentialFileTemplate("sdwan-manager.example.com")); err != nil {
		return err
	}
	return run.WriteTemplateFile("cisco-catalyst-sdwan-manager-secrets-multihost.json", run.CredentialRulesFileTemplate("sdwan-manager01.example.com", "sdwan-manager02.example.com"))
}

// runExport authenticates once against the controller, collects the
// device inventory and every selected site (collect.go), then builds
// and publishes one OSIRIS JSON document per site (document.go,
// output.go). Every document of a run carries the same timestamp.
//
// Outcome contract:
//
//   - authentication, inventory or site selection failure: error.
//   - any selected site whose document could not be built or written:
//     error, after every other site has been written.
//   - an optional operation failed (recorded in each affected
//     document's coverage): success, unless --require-complete is set.
func runExport(cfg *Config) error {
	logger := defaultLogger()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	client := NewClient(cfg, logger)
	client.ctx = ctx
	return export(client, cfg, logger, time.Now().UTC())
}

// export is runExport after client construction: login when the API is
// a *Client, collect, build and publish. now is the run timestamp.
func export(api managerAPI, cfg *Config, logger *slog.Logger, now time.Time) error {
	if c, ok := api.(*Client); ok {
		if err := c.Login(cfg.Username, cfg.Password); err != nil {
			return fmt.Errorf("SD-WAN Manager authentication failed: %w", err)
		}
	}

	col, err := collect(api, cfg, logger)
	if err != nil {
		return err
	}

	outputDir := cfg.OutputDir
	if abs, err := filepath.Abs(outputDir); err == nil {
		outputDir = abs
	}
	timestamp := run.FormatTimestamp(now)

	var succeeded int
	var failures, incomplete []string
	var segments siteSegments
	for _, s := range col.sites {
		label := siteLabel(s.id, s.name)
		log := logger.With("site_id", s.id, "site", label)

		doc, err := buildSiteDocument(col, s, cfg, now, log)
		var data []byte
		if err == nil {
			data, err = sdk.MarshalDocument(doc)
		}
		if err != nil {
			log.Error("document build failed (continuing with remaining sites)", "err", err)
			failures = append(failures, fmt.Sprintf("%s: %v", label, err))
			continue
		}

		// outPath is keyed by site name, not site-id (see OutputPath):
		// an existing directory under this name is reused and the new
		// timestamped file is added alongside whatever is already
		// there. writeDocument publishes privately and atomically and
		// never overwrites an existing file (see output.go).
		outPath := OutputPath(outputDir, timestamp, segments.segment(s.id, s.name))
		if err := writeDocument(outputDir, outPath, data); err != nil {
			log.Error("write failed (continuing with remaining sites)", "err", err, "path", outPath)
			failures = append(failures, fmt.Sprintf("%s: %v", label, err))
			continue
		}
		fmt.Fprintf(os.Stderr, "Saved to %s\n", outPath)
		succeeded++

		if cov := col.siteCoverage(s); !cov.complete() {
			log.Warn("document is partial", "gaps", strings.Join(cov.gaps(), "; "))
			incomplete = append(incomplete, label)
		}
	}

	if len(failures) > 0 {
		return fmt.Errorf("%d of %d site(s) not written: %s", len(failures), len(col.sites), strings.Join(failures, "; "))
	}
	if len(incomplete) > 0 {
		if cfg.RequireComplete {
			return fmt.Errorf("--require-complete: %d of %d document(s) partial: %s", len(incomplete), len(col.sites), strings.Join(incomplete, ", "))
		}
		logger.Warn("export complete with partial documents", "written", succeeded, "partial", len(incomplete), "output_dir", outputDir)
		return nil
	}
	logger.Info("export complete", "succeeded", succeeded, "output_dir", outputDir)
	return nil
}

// siteLabel returns a human-readable label for a site, matching the
// resolved name OutputPath used for this site's directory segment
// (siteName) when available, falling back to the raw site-id and then
// to the unclaimed segment name.
func siteLabel(siteID, siteName string) string {
	if siteName != "" {
		return siteName
	}
	if siteID == "" {
		return unsitedSegment
	}
	return siteID
}

func defaultLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, nil))
}

func printHelp() {
	fmt.Print(`osirisjson-producer cisco sdwan - Cisco Catalyst SD-WAN Manager OSIRIS JSON producer

Usage:
  osirisjson-producer cisco sdwan [flags]

Site selection: when neither --site nor --all is given, the sites
discovered on the controller are listed interactively (pick by number,
e.g. "1,3,5" or "1-4" or "all"). Use --all (or --site all, an alias)
for non-interactive runs (cron/CI), it does not wait on a bulk
site-name resolution pass. --site also accepts either the raw numeric
site-id (matches instantly without waiting for name resolution) or the
resolved site name (e.g. --site MXP); a name that doesn't match any raw
site-id triggers a full site-name resolution pass to find it, since the
SD-WAN Manager API this producer uses has no lookup of a site-id by
name.

Bulk site-name resolution (only triggered by the interactive picker or
a --site value that falls back to name matching) is capped at
--site-name-rate (default 10/s) since SD-WAN Manager's own rate limit is
typically shared across many tools frequently polling same controller
(monitoring systems, other automation, not just this producer). Raise
or lower it based on how much headroom your environment actually has,
from 1 (very conservative) up to your controller's full observed limit.

Authentication: there is deliberately no -p/--password flag, CLI flag
values are visible to any local user (e.g. via ps) and get written to
shell history. host/username/password are each resolved in this order:
their own -h/-u flag, then --secrets-file, then (for whichever is still
missing) an interactive prompt on the controlling terminal so a bare
"osirisjson-producer cisco sdwan" with no flags at all asks for
host, username and password one at a time, and nothing entered there
is written to disk unless --secrets-file was also given.

Collection and outcome: every selected site is collected first, then
one document per site is written with the run's single timestamp.
Each document records the API operations behind it in
metadata["osiris.cisco.catalystsdwanmanager"] (status, attempts, rows
and failure categories such as denied or malformed, plus "complete").
The run exits with an error when authentication, the device inventory
or site selection fails, or when any selected site's document could
not be built or written. A failed optional operation leaves its
document partial but the run succeeds, unless --require-complete is
given.

Flags:
`)
	fmt.Print(FlagsUsage())
	fmt.Print(`
Other commands:
  template --generate	Write --secrets-file skeletons: cisco-catalyst-sdwan-manager-secrets.json
                      	(single host) and cisco-catalyst-sdwan-manager-secrets-multihost.json
                      	(per-host/CIDR rules)

Examples:
	osirisjson-producer cisco sdwan
	osirisjson-producer cisco sdwan -h sdwan-manager.example.com -u admin
	osirisjson-producer cisco sdwan --secrets-file ./cisco-catalyst-sdwan-manager-secrets.json
	osirisjson-producer cisco sdwan --secrets-file ./cisco-catalyst-sdwan-manager-secrets.json --purpose audit --site "MXP,Branch-1"
	osirisjson-producer cisco sdwan --secrets-file ./cisco-catalyst-sdwan-manager-secrets.json --all -o ./output
	osirisjson-producer cisco sdwan template --generate
`)
}
