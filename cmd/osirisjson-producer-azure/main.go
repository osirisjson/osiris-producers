/*
main.go - Standalone Azure OSIRIS JSON producer binary.

This binary is fully self-contained and can be invoked directly:

	osirisjson-producer-azure -S a1b2c3d4-e5f6-7890-abcd-ef1234567890
	osirisjson-producer-azure -s subscriptions.csv -o ./output
	osirisjson-producer-azure template --generate
	osirisjson-producer-azure --help

It is also discovered automatically by the core osirisjson-producer
dispatcher when the user runs:

	osirisjson-producer azure -S a1b2c3d4-e5f6-7890-abcd-ef1234567890

Exit codes:

	0 - producer completed successfully
	1 - producer encountered a validation or runtime error
	2 - operational error (missing flags, unknown subcommand, etc.)
*/
package main

import (
	"fmt"
	"os"
	"runtime/debug"

	"go.osirisjson.org/producers/osiris/hyperscalers/azure"
)

// version is set at build time via -ldflags.
// Falls back to the module version from go install (e.g. v0.1.1).
var version = "dev"

func init() {
	if version != "dev" {
		return
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		version = info.Main.Version
	}
}

func main() {
	args := os.Args[1:]

	// Handle top-level --version before delegating to azure.Run.
	if len(args) > 0 {
		switch args[0] {
		case "--version", "-v", "version":
			fmt.Printf("osirisjson-producer-azure %s\n", version)
			os.Exit(0)
		}
	}

	if err := azure.Run(args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
