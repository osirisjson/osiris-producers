/*
main.go - Standalone HPE OSIRIS JSON producer binary.

This binary is fully self-contained and dispatches to HPE sub-producers
(Aruba Central and future Apstra, Juniper).
It must be named osirisjson-producer-hpe (not e.g.
osirisjson-producer-arubacentral) because the core dispatcher
(cmd/osirisjson-producer) resolves the binary name from the first CLI
argument: "osirisjson-producer hpe arubacentral" looks up
"osirisjson-producer-hpe" on $PATH and execs it  ["arubacentral", ...].
See cmd/osirisjson-producer-arubacentral for a second, Aruba-Central
only binary aimed at users who don't need the "hpe"
family subcommand layer.

Examples:

	osirisjson-producer-hpe arubacentral --cluster eu --token-file ./arubacentral-token.json
	osirisjson-producer-hpe --help

It is also discovered automatically by the core osirisjson-producer
dispatcher when the user runs:

	osirisjson-producer hpe arubacentral --cluster eu --token-file ./arubacentral-token.json

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

	"go.osirisjson.org/producers/osiris/network/hpe"
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

	if len(args) > 0 {
		switch args[0] {
		case "--version", "-v", "version":
			fmt.Printf("osirisjson-producer-hpe %s\n", version)
			os.Exit(0)
		}
	}

	if err := hpe.Run(args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
