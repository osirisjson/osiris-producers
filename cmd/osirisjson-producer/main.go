/*
main.go - Core dispatcher for OSIRIS JSON producer binaries.

Discovers and executes vendor-specific producer binaries on $PATH using the
naming convention osirisjson-producer-<vendor>. This is the unified entry point:

	osirisjson-producer cisco apic -h 10.0.0.1 -u admin
	osirisjson-producer azure --subscription prod
	osirisjson-producer --help

The dispatcher itself has no vendor dependencies it is a thin routing layer.
Each vendor binary (e.g. osirisjson-producer-cisco) is self-contained and can
also be invoked directly.

Exit codes:

	0 - producer completed successfully
	1 - producer encountered a validation or runtime error (passed through from vendor binary)
	2 - operational error (unknown vendor, binary not found, etc.)
*/
package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime/debug"
	"sort"
	"strings"
	"syscall"
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

// knownVendor describes a vendor whose producer binary may be installed.
// This is a string table only, no code is imported.
type knownVendor struct {
	name        string
	description string
	installPkg  string // Go install path (empty = not yet available).
}

// knownVendors lists all planned OSIRIS producer vendors.
// The core dispatcher uses this for --help output and install hints.
// Vendors not in this list are still discovered on $PATH.
var knownVendors = []knownVendor{
	{"aws", "[Under development] AWS cloud OSIRIS JSON producer", ""},
	{"azure", "Microsoft Azure cloud OSIRIS JSON producer", "go.osirisjson.org/producers/cmd/osirisjson-producer-azure"},
	{"gcp", "[Under development] Google Cloud Platform OSIRIS JSON producer", ""},
	{"arista", "[Under development] Arista EOS OSIRIS JSON producer", ""},
	{"cisco", "Cisco OSIRIS JSON producer (APIC, IOS-XR, NX-OS)", "go.osirisjson.org/producers/cmd/osirisjson-producer-cisco"},
	{"nokia", "[Under development] Nokia SR OS OSIRIS JSON producer", ""},
	{"digitalocean", "[Under development] DigitalOcean cloud OSIRIS JSON producer", ""},
	{"leaseweb", "[Under development] Leaseweb bare-metal OSIRIS JSON producer", ""},
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
	vendorArgs := args[1:]

	// Look up the vendor binary on $PATH.
	binaryName := "osirisjson-producer-" + vendor
	binaryPath, err := exec.LookPath(binaryName)
	if err != nil {
		// Binary not found - check if it's a known vendor with install instructions.
		if kv := findKnownVendor(vendor); kv != nil {
			fmt.Fprintf(os.Stderr, "error: vendor %q producer is not installed\n\n", vendor)
			fmt.Fprintf(os.Stderr, "  %s\n\n", kv.description)
			if kv.installPkg != "" {
				fmt.Fprintf(os.Stderr, "Install with:\n")
				fmt.Fprintf(os.Stderr, "  go install %s@latest\n\n", kv.installPkg)
			} else {
				fmt.Fprintf(os.Stderr, "Status: not yet available\n")
				fmt.Fprintf(os.Stderr, "Follow the roadmap: https://osirisjson.org/en/docs/roadmap/01-2026\n\n")
			}
		} else {
			fmt.Fprintf(os.Stderr, "error: unknown vendor %q and no %q binary found on $PATH\n\n", vendor, binaryName)
		}
		fmt.Fprintf(os.Stderr, "Available vendors:\n")
		printVendors(os.Stderr)
		fmt.Fprintf(os.Stderr, "\nRun 'osirisjson-producer --help' for usage.\n")
		os.Exit(2)
	}

	// Execute the vendor binary, replacing this process.
	execArgs := append([]string{binaryName}, vendorArgs...)
	if err := syscall.Exec(binaryPath, execArgs, os.Environ()); err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to exec %s: %v\n", binaryPath, err)
		os.Exit(2)
	}
}

func findKnownVendor(name string) *knownVendor {
	for i := range knownVendors {
		if knownVendors[i].name == name {
			return &knownVendors[i]
		}
	}
	return nil
}

func printUsage() {
	fmt.Print(`osirisjson-producer - OSIRIS JSON Producer CLI Dispatcher

Routes commands to vendor-specific producer binaries (osirisjson-producer-<vendor>)
discovered on $PATH. Each vendor binary can also be invoked directly.

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
  osirisjson-producer cisco apic -h 10.0.0.1 -u admin -p secret
  osirisjson-producer cisco template --generate apic
  osirisjson-producer aws --region us-east-1 --profile prod

Install a vendor producer:
  go install go.osirisjson.org/producers/cmd/osirisjson-producer-cisco@latest

The producer emits a valid OSIRIS JSON document to stdout.
Operational diagnostics are written to stderr.

Vendors marked [Under development] are not yet available.
Follow the roadmap: https://osirisjson.org/en/docs/roadmap/01-2026

Documentation: https://osirisjson.org/en/docs/getting-started/overview
`)
}

func printVendors(w *os.File) {
	// Merge known vendors with any additional vendor binaries found on $PATH.
	vendors := make(map[string]string) // name -> description
	for _, kv := range knownVendors {
		vendors[kv.name] = kv.description
	}

	// Discover unknown vendor binaries on $PATH.
	discovered := discoverVendorBinaries()
	for _, name := range discovered {
		if _, known := vendors[name]; !known {
			vendors[name] = "(discovered on $PATH)"
		}
	}

	names := make([]string, 0, len(vendors))
	for name := range vendors {
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
		desc := vendors[name]
		padding := strings.Repeat(" ", maxLen-len(name)+2)

		// Show install status.
		installed := ""
		binaryName := "osirisjson-producer-" + name
		if _, err := exec.LookPath(binaryName); err == nil {
			installed = " [installed]"
		}

		fmt.Fprintf(w, "  %s%s%s%s\n", name, padding, desc, installed)
	}
}

// discoverVendorBinaries finds osirisjson-producer-* binaries on $PATH
// and returns the vendor name suffixes.
func discoverVendorBinaries() []string {
	pathDirs := strings.Split(os.Getenv("PATH"), ":")
	seen := map[string]bool{}
	var vendors []string

	for _, dir := range pathDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			name := entry.Name()
			if strings.HasPrefix(name, "osirisjson-producer-") {
				vendor := strings.TrimPrefix(name, "osirisjson-producer-")
				if vendor != "" && !seen[vendor] {
					seen[vendor] = true
					vendors = append(vendors, vendor)
				}
			}
		}
	}

	return vendors
}
