/*
main.go - Standalone Cisco OSIRIS JSON producer binary.

This binary is fully self-contained and can be invoked directly:

	osirisjson-producer-cisco apic -h 10.0.0.1 -u admin -p secret
	osirisjson-producer-cisco nxos --help
	osirisjson-producer-cisco template --generate apic

It is also discovered automatically by the core osirisjson-producer
dispatcher when the user runs:

	osirisjson-producer cisco apic -h 10.0.0.1 -u admin -p secret

Exit codes:

	0 - producer completed successfully
	1 - producer encountered a validation or runtime error
	2 - operational error (missing flags, unknown subcommand, etc.)
*/
package main

import (
	"fmt"
	"os"

	"go.osirisjson.org/producers/osiris/network/cisco"
)

// version is set at build time via -ldflags.
var version = "dev"

func main() {
	args := os.Args[1:]

	// Handle top-level --version before delegating to cisco.Run.
	if len(args) > 0 {
		switch args[0] {
		case "--version", "-v", "version":
			fmt.Printf("osirisjson-producer-cisco %s\n", version)
			os.Exit(0)
		}
	}

	if err := cisco.Run(args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
