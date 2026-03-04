// flags.go - CLI flag parsing for Cisco producers.
// Uses stdlib flag.FlagSet with short+long aliases. Detects single vs batch mode
// and validates mutual exclusivity of -h/--host and -s/--source.
//
// For an introduction to OSIRIS JSON Producer for Cisco see:
// "[OSIRIS-JSON-CISCO]."
//
// [OSIRIS-JSON-CISCO]: https://osirisjson.org/en/docs/developers/producers/cisco/

package shared

import (
	"flag"
	"fmt"
	"os"
)

// ParseFlags parses CLI flags for a Cisco sub-producer and returns a RunConfig.
// In single mode (-h/--host), one target is created. In batch mode (-s/--source),
// targets are loaded from a CSV file.
// The producerName is used to set the Type field on single-mode targets.
//
// When promptPassword is non-nil and password is empty in single mode, it is called
// to interactively prompt the user.
func ParseFlags(producerName string, args []string, promptPassword func(string) (string, error)) (*RunConfig, error) {
	fs := flag.NewFlagSet("osirisjson-producer cisco "+producerName, flag.ContinueOnError)

	var (
		host string
		username string
		password string
		port int
		source string
		output string
		detail string
		safeFail string
		insecure bool
	)

	// Short and long flag aliases.
	fs.StringVar(&host, "h", "", "target host (IP or FQDN, optionally with :port)")
	fs.StringVar(&host, "host", "", "target host (IP or FQDN, optionally with :port)")
	fs.StringVar(&username, "u", "", "username for authentication")
	fs.StringVar(&username, "username", "", "username for authentication")
	fs.StringVar(&password, "p", "", "password for authentication")
	fs.StringVar(&password, "password", "", "password for authentication")
	fs.IntVar(&port, "P", 0, "override port (default: producer-specific)")
	fs.IntVar(&port, "port", 0, "override port (default: producer-specific)")
	fs.StringVar(&source, "s", "", "CSV file for batch mode")
	fs.StringVar(&source, "source", "", "CSV file for batch mode")
	fs.StringVar(&output, "o", "", "output directory for batch mode")
	fs.StringVar(&output, "output", "", "output directory for batch mode")
	fs.StringVar(&detail, "detail", "minimal", "detail level: minimal or detailed")
	fs.StringVar(&safeFail, "safe-failure-mode", "fail-closed", "secret handling: fail-closed, log-and-redact, or off")
	fs.BoolVar(&insecure, "insecure", false, "skip TLS certificate verification")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	// Validate detail level.
	if detail != "minimal" && detail != "detailed" {
		return nil, fmt.Errorf("invalid --detail value %q: must be minimal or detailed", detail)
	}

	// Validate safe failure mode.
	switch safeFail {
	case "fail-closed", "log-and-redact", "off":
		// valid
	default:
		return nil, fmt.Errorf("invalid --safe-failure-mode value %q: must be fail-closed, log-and-redact, or off", safeFail)
	}

	// Mutual exclusivity: -h and -s.
	if host != "" && source != "" {
		return nil, fmt.Errorf("--host and --source are mutually exclusive: use one or the other")
	}

	cfg := &RunConfig{
		DetailLevel: detail,
		SafeFailureMode: safeFail,
		InsecureTLS: insecure,
	}

	if source != "" {
		// Batch mode.
		if output == "" {
			return nil, fmt.Errorf("batch mode (--source) requires --output directory")
		}
		cfg.OutputDir = output

		targets, err := ParseCSV(source)
		if err != nil {
			return nil, fmt.Errorf("parsing CSV %q: %w", source, err)
		}

		// Apply flag-level credentials as defaults for all targets.
		for i := range targets {
			if targets[i].Username == "" {
				targets[i].Username = username
			}
			if targets[i].Password == "" {
				targets[i].Password = password
			}
			if port != 0 && targets[i].Port == 0 {
				targets[i].Port = port
			}
		}

		cfg.Targets = targets
		return cfg, nil
	}

	// Single mode.
	if host == "" {
		return nil, fmt.Errorf("either --host (single mode) or --source (batch mode) is required")
	}
	if username == "" {
		return nil, fmt.Errorf("--username is required in single mode")
	}

	// Interactive password prompt when not provided.
	if password == "" && promptPassword != nil {
		p, err := promptPassword(fmt.Sprintf("Password for %s@%s: ", username, host))
		if err != nil {
			return nil, fmt.Errorf("reading password: %w", err)
		}
		password = p
	}
	if password == "" {
		fmt.Fprintf(os.Stderr, "warning: no password provided; authentication may fail\n")
	}

	h, p, err := ParseHostPort(host)
	if err != nil {
		return nil, fmt.Errorf("invalid --host value: %w", err)
	}
	if port != 0 {
		p = port // explicit --port overrides host:port
	}

	cfg.Targets = []TargetConfig{{
		Host: h,
		Port: p,
		Hostname: h,
		Type: producerName,
		Username: username,
		Password: password,
		Owner: OwnerSelf,
	}}

	return cfg, nil
}
