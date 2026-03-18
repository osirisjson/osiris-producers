// cisco.go - Cisco vendor entry point for the OSIRIS JSON producer CLI.
// Dispatches to sub-producers (APIC, NX-OS, IOS-XR) and handles the
// template generation command.
//
// Usage:
//
//	osirisjson-producer cisco apic [flags]
//	osirisjson-producer cisco nxos [flags]
//	osirisjson-producer cisco iosxr [flags]
//	osirisjson-producer cisco template --generate [apic|nxos|iosxr]
//
// For an introduction to OSIRIS JSON Producer for Cisco see:
// "[OSIRIS-JSON-CISCO]."
//
// [OSIRIS-JSON-CISCO]: https://osirisjson.org/en/docs/producers/cisco

package cisco

import (
	"fmt"
	"log/slog"
	"os"

	"go.osirisjson.org/producers/pkg/sdk"
	"go.osirisjson.org/producers/producers/cisco/apic"
	"go.osirisjson.org/producers/producers/cisco/shared"
)

// subProducer describes a Cisco sub-producer.
type subProducer struct {
	name        string
	description string
	factory     shared.ProducerFactory // nil until the producer is implemented.
}

var subProducers = []subProducer{
	{"apic", "Cisco ACI/APIC fabric topology", apic.NewFactory()},
	{"nxos", "Cisco NX-OS device inventory", nil},
	{"iosxr", "Cisco IOS-XR device inventory", nil},
}

// factoryRegistry returns the set of implemented producer factories,
// keyed by type name as used in the CSV type column.
func factoryRegistry() shared.FactoryRegistry {
	reg := shared.FactoryRegistry{}
	for _, sp := range subProducers {
		if sp.factory != nil {
			reg[sp.name] = sp.factory
		}
	}
	return reg
}

// Run is the entry point called by the CLI dispatcher.
// It receives the arguments after "cisco" (e.g. ["apic", "-h", "10.0.0.1"]).
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
	// Handle --help for unimplemented producers. Note: -h is NOT help here, it's the short flag for --host in our flag set.
	if len(args) > 0 && args[0] == "--help" {
		fmt.Printf("osirisjson-producer cisco %s - %s\n\n", sp.name, sp.description)
		if sp.factory == nil {
			fmt.Printf("Status: not yet implemented\n")
			return nil
		}
		// When implemented, ParseFlags will handle --help via flag.FlagSet.
	}

	if sp.factory == nil {
		return fmt.Errorf("cisco %s producer is not yet implemented", sp.name)
	}

	cfg, err := shared.ParseFlags(sp.name, args, shared.PromptPassword)
	if err != nil {
		return err
	}

	if cfg.IsBatch() {
		return shared.RunBatch(cfg, factoryRegistry(), defaultLogger())
	}

	return runSingle(cfg, sp.factory)
}

// runSingle executes a single-target collection and writes to stdout.
func runSingle(cfg *shared.RunConfig, factory shared.ProducerFactory) error {
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

	_, err = os.Stdout.Write(data)
	return err
}

func runTemplate(args []string) error {
	if len(args) == 0 || (args[0] != "--generate" && args[0] != "-g") {
		fmt.Println("Usage: osirisjson-producer cisco template --generate [apic|nxos|iosxr]")
		return nil
	}

	if len(args) < 2 {
		return fmt.Errorf("--generate requires a sub-producer name: apic, nxos, or iosxr")
	}

	name := args[1]
	for _, sp := range subProducers {
		if sp.name == name {
			fmt.Print(shared.CSVTemplate(name))
			return nil
		}
	}

	return fmt.Errorf("unknown sub-producer %q for template generation; valid: apic, nxos, iosxr", name)
}

func defaultLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, nil))
}

func newSDKContext(cfg *shared.RunConfig) *sdk.Context {
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

	fmt.Print(`
  template  Generate CSV batch template

Single mode flags:
  -h, --host        	Target host (IP or FQDN, optionally with :port)
  -u, --username    	Username for authentication
  -p, --password    	Password (omit for interactive prompt)
  -P, --port        	Override port (default: producer-specific)
  --detail          	Detail level: minimal or detailed (default: minimal)
  --safe-failure-mode	Secret handling: fail-closed, log-and-redact, off (default: fail-closed)
  --insecure        	Skip TLS certificate verification

Batch mode flags:
  -s, --source      	CSV file with targets (dc,floor,room,zone,hostname,type,ip,port,owner,notes)
  -o, --output      	Output directory (files organized as DC/Floor/Room/Zone/Hostname.json)
  -u, --username    	Default username for all targets
  -p, --password    	Default password for all targets

  Generate a CSV template:
    osirisjson-producer cisco template --generate apic

Examples:
  osirisjson-producer cisco apic -h 10.0.0.1 -u admin -p secret
  osirisjson-producer cisco nxos -h switch.lab:8443 -u admin --insecure
  osirisjson-producer cisco apic -s datacenter.csv -o ./output -u admin -p secret
  osirisjson-producer cisco template --generate apic
`)
}
