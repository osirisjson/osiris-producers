/*
main.go - Standalone HPE Aruba Networking Central
OSIRIS JSON producer binary.

This binary is fully self-contained and can be invoked directly, without
the "hpe" family subcommand layer used by the core dispatcher:

Examples:

	osirisjson-producer-arubacentral
	osirisjson-producer-arubacentral --purpose audit
	osirisjson-producer-arubacentral --token-file ./arubacentral-token.json
	osirisjson-producer-arubacentral --cluster eu --token-file ./arubacentral-token.json
	osirisjson-producer-arubacentral --cluster prod --token-file ./arubacentral-token.json --site "MXP,Branch-1"
	osirisjson-producer-arubacentral --cluster eu --token-file ./arubacentral-token.json --all -o ./output
	osirisjson-producer-arubacentral template --generate
	osirisjson-producer-arubacentral --help

Naming note: this binary is deliberately named "arubacentral", not the
shorter "aruba" Aruba Central is a cloud controller, and a customer
without Central (on-box ArubaOS-CX/ArubaOS-Switch management instead)
is a distinct, not-yet-implemented collection path that would want the
plain "aruba" name for itself rather than colliding with this one.

The core dispatcher (cmd/osirisjson-producer) resolves vendor binaries
as "osirisjson-producer <first-arg>", so "osirisjson-producer hpe arubacentral"
always resolves to the osirisjson-producer-hpe binary (see
cmd/osirisjson-producer-hpe), not this one. This binary exists in
addition to that one, for users who only want the Aruba Central producer
specifically. It is not reachable through "osirisjson-producer-arubacentral ..."
unless installed as osirisjson-producer-arubacentral and invoked as such
directly (or added to $PATH under that name).

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

	"go.osirisjson.org/producers/osiris/network/hpe/arubacentral"
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
			fmt.Printf("osirisjson-producer-arubacentral %s\n", version)
			os.Exit(0)
		}
	}

	if err := arubacentral.Run(args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
