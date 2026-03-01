/* 
main.go - Entry point for the osirisjson-producer CLI dispatcher.
Routes vendor subcommands to registered producer backends and handles
	global flags (--help, --version, --output, --profile, --safe-failure-mode).

Usage:
	osirisjson-producer <vendor> [flags]
	osirisjson-producer aws --help
	osirisjson-producer cisco --help
	osirisjson-producer cisco apic --output topology.json

Exit codes:
	0 - producer completed successfully
	1 - producer encountered a validation or runtime error
	2 - operational error (unknown vendor, missing flags, etc.)
*/
package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// version is set at build time via -ldflags.
var version = "dev"

// vendorEntry describes a registered vendor backend.
type vendorEntry struct {
	name string
	description string
	// run will be wired when producers are implemented.
	// Signature: func(args []string) error
}

// registry holds all registered vendor producers.
// OSIRIS JSON Producers register themselves in init() via registerVendor.
var registry = map[string]vendorEntry{}

// registerVendor adds a vendor backend to the dispatcher registry.
func registerVendor(name, description string) {
	registry[name] = vendorEntry{name: name, description: description}
}

func init() {
	// Vendor registrations. Each OSIRIS JSON producer package will call registerVendor
	// in its own init() once implemented. For now, I'm declaring the planned vendors for 2026.
	registerVendor("aws", "AWS cloud OSIRIS JSON producer")
	registerVendor("azure", "Microsoft Azure cloud OSIRIS JSON producer")
	registerVendor("gcp", "Google Cloud Platform OSIRIS JSON producer")
	registerVendor("cisco", "Cisco OSIRIS JSON producer (APIC, IOS-XR, NX-OS)")
	registerVendor("arista", "Arista EOS OSIRIS JSON producer")
	registerVendor("nokia", "Nokia SR OS OSIRIS JSON producer")
	registerVendor("leaseweb", "Leaseweb bare-metal OSIRIS JSON producer")
	registerVendor("digitalocean", "DigitalOcean cloud OSIRIS JSON producer")
}

func main() {
	args := os.Args[1:]

	if len(args) == 0 {
		printUsage()
		os.Exit(2)
	}

	switch args[0] {
	case "--help", "-h", "help":
		printUsage()
		os.Exit(0)
	case "--version", "-v", "version":
		fmt.Printf("osirisjson-producer %s\n", version)
		os.Exit(0)
	}

	vendor := args[0]
	entry, ok := registry[vendor]
	if !ok {
		fmt.Fprintf(os.Stderr, "error: unknown vendor %q\n\n", vendor)
		fmt.Fprintf(os.Stderr, "Available vendors:\n")
		printVendors(os.Stderr)
		fmt.Fprintf(os.Stderr, "\nRun 'osirisjson-producer --help' for usage.\n")
		os.Exit(2)
	}

	// Vendor-level help.
	vendorArgs := args[1:]
	if len(vendorArgs) > 0 && (vendorArgs[0] == "--help" || vendorArgs[0] == "-h") {
		fmt.Printf("osirisjson-producer %s - %s\n\n", entry.name, entry.description)
		fmt.Printf("Usage:\n")
		fmt.Printf("  osirisjson-producer %s [subcommand] [flags]\n\n", entry.name)
		fmt.Printf("Status: not yet implemented\n")
		os.Exit(0)
	}

	// TODO: dispatch to the vendor's run function once producers are implemented.
	fmt.Fprintf(os.Stderr, "error: vendor %q is registered but not yet implemented\n", vendor)
	os.Exit(2)
}

func printUsage() {
	fmt.Print(`osirisjson-producer - OSIRIS JSON Producer CLI Dispatcher

Usage:
  osirisjson-producer <vendor> [subcommand] [flags]
  osirisjson-producer --help
  osirisjson-producer --version

Global flags:
  --help, -h       Show this help message
  --version, -v    Show version information

Available vendors:
`)
	printVendors(os.Stdout)
	fmt.Print(`
Examples:
  osirisjson-producer cisco apic --output topology.json
  osirisjson-producer aws --region us-east-1
  osirisjson-producer arista --ssh --host spine-01.lab

The producer emits a valid OSIRIS JSON document to stdout.
Operational diagnostics are written to stderr.

Documentation reference: https://osirisjson.org/en/docs/getting-started/overview
`)
}

func printVendors(w *os.File) {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)

	maxLen := 0
	for _, name := range names {
		if len(name) > maxLen {
			maxLen = len(name)
		}
	}

	for _, name := range names {
		entry := registry[name]
		padding := strings.Repeat(" ", maxLen-len(name)+2)
		fmt.Fprintf(w, "  %s%s%s\n", name, padding, entry.description)
	}
}
