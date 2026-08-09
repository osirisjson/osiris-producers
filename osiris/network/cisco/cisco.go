// Package cisco implements the Cisco vendor entry point for the
// OSIRIS JSON producer CLI.

// Dispatches to sub-producers (APIC, NX-OS, IOS-XE, vManage) and
// handles the template generation command.
//
// vManage is dispatched separately from the other three: it does not
// implement cisco/run.ProducerFactory (one controller login fans out
// into many per-site documents, unlike the one-CSV-row-per-device shape
// the others share), so it is special-cased in Run below instead of
// going through the subProducers/factoryRegistry table.
//
// Usage:
//
//	osirisjson-producer cisco apic [flags]
//	osirisjson-producer cisco nxos [flags]
//	osirisjson-producer cisco iosxe [flags]
//	osirisjson-producer cisco vmanage [flags]
//	osirisjson-producer cisco template --generate [flags]
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification
package cisco

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"go.osirisjson.org/producers/osiris/network/cisco/apic"
	"go.osirisjson.org/producers/osiris/network/cisco/iosxe"
	"go.osirisjson.org/producers/osiris/network/cisco/nxos"
	"go.osirisjson.org/producers/osiris/network/cisco/run"
	"go.osirisjson.org/producers/osiris/network/cisco/vmanage"
	"go.osirisjson.org/producers/pkg/sdk"
)

// subProducer describes a Cisco sub-producer.
type subProducer struct {
	name        string
	description string
	factory     run.ProducerFactory
}

var subProducers = []subProducer{
	{"apic", "Cisco ACI/APIC fabric topology", apic.NewFactory()},
	{"nxos", "Cisco NX-OS device inventory", nxos.NewFactory()},
	{"iosxe", "Cisco IOS-XE device inventory", iosxe.NewFactory()},
}

// factoryRegistry returns the set of implemented producer factories,
// keyed by type name as used in the CSV type column.
func factoryRegistry() run.FactoryRegistry {
	reg := run.FactoryRegistry{}
	for _, sp := range subProducers {
		if sp.factory != nil {
			reg[sp.name] = sp.factory
		}
	}
	return reg
}

// Run is the entry point called by the CLI dispatcher.
// It receives the arguments after "cisco".
func Run(args []string) error {
	if len(args) == 0 {
		printHelp()
		return nil
	}

	cmd := args[0]
	subArgs := args[1:]

	switch cmd {
	case "--help", "-h", "help":
		printHelp()
		return nil
	case "template":
		return runTemplate(subArgs)
	case "vmanage":
		return vmanage.Run(subArgs)
	}

	// Look up sub-producer.
	for _, sp := range subProducers {
		if sp.name == cmd {
			return runSubProducer(sp, subArgs)
		}
	}

	return fmt.Errorf("unknown cisco subcommand %q; run 'osirisjson-producer cisco --help' for usage", cmd)
}

func runSubProducer(sp subProducer, args []string) error {
	// Handle --help for unimplemented producers.
	// Note: -h is not help here, it's the short flag for --host in
	// the flag set.
	if len(args) > 0 && args[0] == "--help" {
		fmt.Printf("osirisjson-producer cisco %s - %s\n\n", sp.name, sp.description)
		if sp.factory == nil {
			fmt.Printf("Status: not yet implemented\n")
			return nil
		}
		// When implemented ParseFlags handle --help via flag.FlagSet.
	}

	if sp.factory == nil {
		return fmt.Errorf("cisco %s producer is not yet implemented", sp.name)
	}

	cfg, err := run.ParseFlags(sp.name, args, run.PromptPassword)
	if err != nil {
		return err
	}

	cfg.Timestamp = run.FormatTimestamp(time.Now())

	if cfg.IsBatch() {
		return run.RunBatch(cfg, factoryRegistry(), defaultLogger())
	}

	return runSingle(cfg, sp.factory)
}

// runSingle executes a single-target collection and writes local file.
// Output filename: cisco-<type>-<timestamp>-<hostname>.json
func runSingle(cfg *run.RunConfig, factory run.ProducerFactory) error {
	target := cfg.Targets[0]
	logger := defaultLogger()

	producer := factory(target, cfg)
	ctx := newSDKContext(cfg)
	ctx.Logger = logger

	doc, err := producer.Collect(ctx)
	if err != nil {
		return fmt.Errorf("collection failed for %s: %w", target.Host, err)
	}

	data, err := marshalDocument(doc)
	if err != nil {
		return fmt.Errorf("marshal failed: %w", err)
	}

	name := target.Hostname
	if name == "" {
		name = target.Host
	}
	filename := fmt.Sprintf("cisco-%s-%s-%s.json", target.Type, cfg.Timestamp, name)

	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", filename, err)
	}
	fmt.Fprintf(os.Stderr, "Saved to %s\n", filename)
	return nil
}

func runTemplate(args []string) error {
	if len(args) == 0 || (args[0] != "--generate" && args[0] != "-g") {
		fmt.Println("Usage: osirisjson-producer cisco template --generate [apic|nxos|iosxe]")
		return nil
	}

	if len(args) < 2 {
		return fmt.Errorf("--generate requires a sub-producer name: apic, nxos, or iosxe")
	}

	name := args[1]
	for _, sp := range subProducers {
		if sp.name == name {
			filename := fmt.Sprintf("cisco-%s-template.csv", name)
			if err := os.WriteFile(filename, []byte(run.CSVTemplate(name)), 0644); err != nil {
				return fmt.Errorf("failed to write template: %w", err)
			}
			fmt.Printf("Template saved to %s\n", filename)
			return nil
		}
	}

	return fmt.Errorf("unknown sub-producer %q for template generation; valid: apic, nxos, iosxe", name)
}

func defaultLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, nil))
}

func newSDKContext(cfg *run.RunConfig) *sdk.Context {
	return sdk.NewContext(&sdk.ProducerConfig{
		DetailLevel:     cfg.DetailLevel,
		SafeFailureMode: cfg.SafeFailureMode,
	})
}

func marshalDocument(doc *sdk.Document) ([]byte, error) {
	return sdk.MarshalDocument(doc)
}

func printHelp() {
	fmt.Print(`osirisjson-producer cisco - Cisco OSIRIS JSON producers

Usage:
  osirisjson-producer cisco <subcommand> [flags]

Subcommands:
`)
	for _, sp := range subProducers {
		status := "ready"
		if sp.factory == nil {
			status = "not yet implemented"
		}
		fmt.Printf("  %-8s  %s (%s)\n", sp.name, sp.description, status)
	}
	fmt.Printf("  %-8s  %s (%s)\n", "vmanage", "Cisco Catalyst SD-WAN Manager (vManage) site inventory", "ready")

	fmt.Print(`
  template  Generate CSV batch template

Single mode flags (apic, nxos, iosxe):
  -h, --host        	Target host (IP or FQDN, optionally with :port)
  -u, --username    	Username for authentication
  -P, --port        	Override port (default: producer-specific)
  --detail          	Detail level: minimal or detailed (default: minimal)
  --safe-failure-mode	Secret handling: fail-closed, log-and-redact, off (default: fail-closed)
  --insecure        	Skip TLS certificate verification

Batch mode flags (apic, nxos, iosxe):
  -s, --source      	CSV file with targets (dc,floor,room,zone,hostname,type,ip,port,owner,notes)
  -o, --output      	Output directory (files organized as DC/Floor/Room/Zone/Hostname.json)
  -u, --username    	Default username for all targets

  Generate a CSV template:
    osirisjson-producer cisco template --generate apic

vmanage flags: run 'osirisjson-producer cisco vmanage --help' (no CSV
batch mode one run targets one controller and fans out into one
document per WAN edge site).

Output:
  apic/nxos/iosxe single mode saves to: cisco-<type>-<timestamp>-<hostname>.json
  apic/nxos/iosxe batch mode saves to:  <output>/DC/Floor/Room/Zone/Hostname.json
  vmanage saves to:                     <output>/<site-name>/cisco-vmanage-<timestamp>-<site-name>.json

Examples:
  osirisjson-producer cisco apic -h 10.0.0.1 -u username
  osirisjson-producer cisco nxos -h switch.lab:8443 -u username --insecure
  osirisjson-producer cisco apic -s datacenter.csv -o ./output -u username
  osirisjson-producer cisco vmanage -h acme.sdwan.cisco.com -u username
  osirisjson-producer cisco template --generate apic
`)
}
