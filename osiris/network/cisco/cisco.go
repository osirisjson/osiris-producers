// Package cisco implements the Cisco vendor entry point for the
// OSIRIS JSON producer CLI.
//
// Dispatches to sub-producers (APIC, NX-OS, IOS-XE, vManage). Each
// sub-producer owns its own CLI flag parsing, Config type, and
// single/batch runner (see each producer's own flags.go/dispatch.go
// doc comments) this package only routes a subcommand name to the
// right one and prints the top-level --help summary. The shared
// osiris/network/cisco/run package remains only for genuinely
// vendor-agnostic mechanisms (TargetConfig, CSV parsing, credential-
// file handling, prompting, path/timestamp helpers, template
// generation) that every producer still reuses directly.
//
// Usage:
//
//	osirisjson-producer cisco <apic|nxos|iosxe|vmanage> [flags]
//	osirisjson-producer cisco <apic|nxos|iosxe|vmanage> template --generate
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification
package cisco

import (
	"fmt"

	"go.osirisjson.org/producers/osiris/network/cisco/apic"
	"go.osirisjson.org/producers/osiris/network/cisco/iosxe"
	"go.osirisjson.org/producers/osiris/network/cisco/nxos"
	"go.osirisjson.org/producers/osiris/network/cisco/vmanage"
)

// subProducer describes a Cisco sub-producer for the top-level --help
// listing and dispatch table.
type subProducer struct {
	name        string
	description string
	run         func([]string) error
}

var subProducers = []subProducer{
	{"apic", "Cisco ACI/APIC", apic.Run},
	{"nxos", "Cisco NX-OS", nxos.Run},
	{"iosxe", "Cisco IOS-XE", iosxe.Run},
	{"vmanage", "Cisco Catalyst SD-WAN Manager (vManage)", vmanage.Run},
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
	}

	for _, sp := range subProducers {
		if sp.name == cmd {
			return sp.run(subArgs)
		}
	}

	return fmt.Errorf("unknown cisco subcommand %q; run 'osirisjson-producer cisco --help' for usage", cmd)
}

func printHelp() {
	fmt.Print(`osirisjson-producer cisco - Cisco OSIRIS JSON producers

Usage:
  osirisjson-producer cisco <subcommand> [flags]

Subcommands:
`)
	for _, sp := range subProducers {
		fmt.Printf("  %-8s  %s (ready)\n", sp.name, sp.description)
	}

	fmt.Print(`
Each subcommand owns its own flags and has its own --help run
'osirisjson-producer cisco <subcommand> --help' for the full flag list.
All four share the same broad shape:

  -h, --host            Target host (IP or FQDN, optionally with :port);
  						prompted for interactively when omitted
  -u, --username        Username for authentication; prompted for
  						interactively when omitted
  --secrets-file        JSON file with {host, username, password};
  						overrides -h/-u when they omit a field
  --purpose             Output per OSIRIS JSON chapter 13.1.3
  --include-raw-body    Attach each collected command's full, unmodified
  						response (requires --purpose audit).
						For development purpose.
  --safe-failure-mode   Secret handling: fail-closed, log-and-redact, or
  						off (default: fail-closed)
  --insecure            Skip TLS/SSH host-key verification

apic/nxos/iosxe additionally support CSV batch mode (-s/--source,
-o/--output) see each producer's own --help for the CSV column
layout and output path convention. vmanage has no CSV batch mode: one
run targets one controller and fans out into one document per
WAN edge site.

Examples:
  osirisjson-producer cisco apic -h 192.0.2.1 -u username
  osirisjson-producer cisco nxos -h 192.0.2.1 -u username --insecure
  osirisjson-producer cisco apic -s datacenter.csv -o ./output -u username
  osirisjson-producer cisco vmanage -h vmanage.example.com -u username
  osirisjson-producer cisco <subcommand> template --generate
`)
}
