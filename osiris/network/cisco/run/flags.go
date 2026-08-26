// flags.go - CLI flag parsing for Cisco OSIRIS JSON Producer.
// Uses stdlib flag.FlagSet with short+long aliases.
// Detects single vs batch mode and validates mutual exclusivity
// of -h/--host and -s/--source.
//
// Unlike initial release of producers (apic,nxos,iosxe) we removed
// deliberately the -p/--password flag: a CLI flag value is
// visible to any local user via `ps` and gets written to shell history,
// exactly the trace a password must never leave.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package run

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

// passwordEnvVar returns the producer-specific environment variable
// name checked for a password when neither --secrets-file nor the
// (removed) password flag supplied one:
// e.g. "nxos" -> "OSIRISJSON_CISCO_NXOS_PASSWORD".
func passwordEnvVar(producerName string) string {
	return "OSIRISJSON_CISCO_" + strings.ToUpper(producerName) + "_PASSWORD"
}

// ParseFlags parses CLI flags for a Cisco sub-producer and returns a
// RunConfig. In single mode (-h/--host), one target is created.
// In batch mode (-s/--source), targets are loaded from a CSV file.
// The producerName is used to set the Type field on single-mode targets
// and to derive the credential environment variable
// name (see passwordEnvVar).
//
// promptVisible and promptHidden, when non-nil, are used to
// interactively ask for host/username and password (respectively) on
// the controlling terminal when they are still missing after flags and
// --secrets-file have been applied. Either may be nil (e.g. no
// controlling terminal available, such as under cron/CI), in which case
// a still-missing host or username is a hard error and a still-missing
// password is left empty with a warning.
func ParseFlags(producerName string, args []string, promptVisible, promptHidden func(string) (string, error)) (*RunConfig, error) {
	fs := flag.NewFlagSet("osirisjson-producer cisco "+producerName, flag.ContinueOnError)

	var (
		host     string
		username string
		credFile string
		port     int
		source   string
		output   string
		detail   string
		safeFail string
		insecure bool
	)

	// Short and long flag aliases.
	fs.StringVar(&host, "h", "", "target host (IP or FQDN, optionally with :port); prompted for interactively when omitted")
	fs.StringVar(&host, "host", "", "target host, see -h")
	fs.StringVar(&username, "u", "", "username for authentication; prompted for interactively when omitted")
	fs.StringVar(&username, "username", "", "username for authentication, see -u")
	fs.StringVar(&credFile, "secrets-file", "", "JSON file with {host, username, password}; whatever it omits still falls back to its own flag or an interactive prompt")
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

	if fs.NArg() > 0 {
		return nil, fmt.Errorf("unexpected positional argument(s): %v", fs.Args())
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

	// Whatever -h didn't provide falls back to --secrets-file's flat
	// "host" field next (only meaningful for the flat shape - a rules
	// file has no single host of its own, see CredentialFile's doc
	// comment). Username/password resolution is deferred to
	// ResolveForHost below, once the actual per-target host is known
	// (the bare host, not "host:port"), so a rules-shape file
	// can match on it.
	var tf *CredentialFile
	if credFile != "" {
		loaded, err := LoadCredentialFile(credFile)
		if err != nil {
			return nil, fmt.Errorf("loading --secrets-file: %w", err)
		}
		tf = loaded
		if host == "" {
			host = tf.Host
		}
	}

	cfg := &RunConfig{
		DetailLevel:     detail,
		SafeFailureMode: safeFail,
		InsecureTLS:     insecure,
	}

	if source != "" {
		// Batch mode.
		if output == "" {
			return nil, fmt.Errorf("batch mode (--source) requires --output directory")
		}
		cfg.Mode = ModeBatch
		cfg.OutputDir = output

		targets, err := ParseCSV(source, producerName)
		if err != nil {
			return nil, fmt.Errorf("parsing CSV %q: %w", source, err)
		}

		// Batch mode never prompts interactively (a CSV run may target
		// many devices unattended); promptHidden is nil here so
		// resolvePassword only tries the secrets-file's fallback and
		// the environment variable. tf.ResolveForHost("") deliberately
		// matches no rule (an empty host cannot equal, or fall inside a
		// CIDR of, any real pattern), so it always yields exactly the
		// flat shape's Username/Password, or the rules shape's Default
		// whichever this file actually declares as "shared unless a
		// rule below says otherwise."
		globalUser, globalCredPassword := tf.ResolveForHost("")
		if username == "" {
			username = globalUser
		}
		globalPassword, _ := resolvePassword(producerName, globalCredPassword, nil)

		// Apply flag/secrets-file/env-level credentials as defaults for
		// every target in the batch, with a per-target --secrets-file
		// rule (matched by CredentialRule.Hosts against the CSV row's
		// own ip/hostname see CredentialFile.ResolveForHost) taking
		// priority over those shared defaults when one matches this
		// target's host. A CSV row's own username/password columns
		// (there are none today) would still win over both, same as
		// before this change.
		for i := range targets {
			ruleUser, rulePassword := tf.ResolveForHost(targets[i].Host)

			if targets[i].Username == "" {
				if ruleUser != "" {
					targets[i].Username = ruleUser
				} else {
					targets[i].Username = username
				}
			}
			if targets[i].Password == "" {
				if rulePassword != "" {
					targets[i].Password = rulePassword
				} else {
					targets[i].Password = globalPassword
				}
			}
			if port != 0 && targets[i].Port == 0 {
				targets[i].Port = port
			}
		}

		cfg.Targets = targets
		return cfg, nil
	}

	// Single mode.
	cfg.Mode = ModeSingle

	if host == "" {
		if promptVisible == nil {
			return nil, fmt.Errorf("either --host (single mode) or --source (batch mode) is required")
		}
		v, err := promptVisible(fmt.Sprintf("Target host for %s (IP or FQDN, optionally with :port): ", producerName))
		if err != nil {
			return nil, fmt.Errorf("reading host: %w", err)
		}
		if v == "" {
			return nil, fmt.Errorf("--host is required")
		}
		host = v
	}

	h, p, err := ParseHostPort(host)
	if err != nil {
		return nil, fmt.Errorf("invalid --host value: %w", err)
	}
	if port != 0 {
		p = port // explicit --port overrides host:port
	}

	// Resolved against the bare host (h, no port) so a --secrets-file
	// rule's CIDR/exact-host match works the same way it does for a
	// batch CSV row see CredentialFile.ResolveForHost.
	tfUsername, tfPassword := tf.ResolveForHost(h)

	if username == "" {
		username = tfUsername
	}
	if username == "" {
		if promptVisible == nil {
			return nil, fmt.Errorf("--username is required in single mode")
		}
		v, err := promptVisible("Username for authentication: ")
		if err != nil {
			return nil, fmt.Errorf("reading username: %w", err)
		}
		if v == "" {
			return nil, fmt.Errorf("--username is required")
		}
		username = v
	}

	password, err := resolvePassword(producerName, tfPassword, promptHidden)
	if err != nil {
		return nil, fmt.Errorf("reading password: %w", err)
	}
	if password == "" {
		fmt.Fprintf(os.Stderr, "warning: no password provided; authentication may fail\n")
	}

	cfg.Targets = []TargetConfig{{
		Host:     h,
		Port:     p,
		Hostname: h,
		Type:     producerName,
		Username: username,
		Password: password,
	}}

	return cfg, nil
}

// resolvePassword applies the password precedence documented on
// ParseFlags: an already-loaded --secrets-file value first, then
// the producer-specific environment variable, then an interactive
// hidden prompt (promptHidden may be nil, e.g. no controlling
// terminal or batch mode, which never prompts). Returns "", nil if
// none of the three yields a value.
func resolvePassword(producerName, credPassword string, promptHidden func(string) (string, error)) (string, error) {
	if credPassword != "" {
		return credPassword, nil
	}
	if v := os.Getenv(passwordEnvVar(producerName)); v != "" {
		return v, nil
	}
	if promptHidden != nil {
		return promptHidden(fmt.Sprintf("Password for %s authentication: ", producerName))
	}
	return "", nil
}
