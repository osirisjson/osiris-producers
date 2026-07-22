// Package hpe implements the HPE vendor entry point for the OSIRIS JSON
// producer CLI. Dispatches to sub-producers for HPE's network product
// lines: Aruba Central (cloud-managed campus/branch), Aruba Apstra
// (data center fabric intent) and Juniper (acquired alongside Aruba
// Networking; per-device CLI/NETCONF).
//
// Unlike the OSIRIS JSON producer for Cisco family (APIC/NX-OS/IOS-XE:
// one vendor, differentiated only by device role), these initial three
// HPE product lines are unrelated platforms with unrelated
// authentication and collection models, so each sub-producer owns its
// full CLI surface rather than sharing a common run.TargetConfig.
//
// Operating modes:
//
// osirisjson-producer hpe arubacentral [flags]
// osirisjson-producer hpe apstra [flags]  (see the roadmap for updates)
// osirisjson-producer hpe juniper [flags] (see the roadmap for updates)
package hpe

import (
	"fmt"

	"go.osirisjson.org/producers/osiris/network/hpe/arubacentral"
)

// subProducer describes an HPE sub-producer.
type subProducer struct {
	name        string
	description string
	run         func([]string) error // nil until the producer is implemented.
}

var subProducers = []subProducer{
	{"arubacentral", "HPE Aruba Networking Central cloud-managed inventory", arubacentral.Run},
	{"apstra", "HPE Aruba Networking Apstra data center fabric intent", nil},
	{"juniper", "Juniper device inventory (Junos)", nil},
}

// Run is the entry point called by the CLI dispatcher.
// It receives the arguments after "hpe"
// (e.g. ["arubacentral", "--cluster", "eu"]).
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
			if sp.run == nil {
				return fmt.Errorf("hpe %s producer is not yet implemented", sp.name)
			}
			return sp.run(subArgs)
		}
	}

	return fmt.Errorf("unknown hpe subcommand %q; run 'osirisjson-producer hpe --help' for usage", cmd)
}

func printHelp() {
	fmt.Print(`HPE OSIRIS JSON producers

Usage:
  osirisjson-producer hpe <subcommand> [flags]

Subcommands:
`)
	for _, sp := range subProducers {
		status := "ready"
		if sp.run == nil {
			status = "not yet implemented"
		}
		fmt.Printf("  %-13s  %s (%s)\n", sp.name, sp.description, status)
	}

	fmt.Print(`
Run 'osirisjson-producer hpe <subcommand> --help' for subcommand-specific flags.

Examples:
  osirisjson-producer arubacentral --cluster eu --token-file ./arubacentral-token.json
`)
}
