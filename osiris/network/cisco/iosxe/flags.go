// flags.go - CLI flag parsing for the Cisco IOS-XE OSIRIS JSON
// producer.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package iosxe

import (
	"flag"
	"fmt"
	"os"

	"go.osirisjson.org/producers/osiris/network/cisco/run"
	"go.osirisjson.org/producers/pkg/osirismeta"
)

// passwordEnvVar is the environment variable checked for a password
// when neither --secrets-file nor an interactive prompt supplied one.
const passwordEnvVar = "OSIRISJSON_CISCO_IOSXE_PASSWORD"

// iosxeFlagValues holds every IOS-XE CLI flag's bound destination.
type iosxeFlagValues struct {
	host           string
	username       string
	secretsFile    string
	port           int
	source         string
	output         string
	purposeStr     string
	includeRawBody bool
	safeFail       string
	insecure       bool
}

// registerFlags binds every IOS-XE CLI flag onto fs and returns their
// destinations.
func registerFlags(fs *flag.FlagSet) *iosxeFlagValues {
	v := &iosxeFlagValues{}

	fs.StringVar(&v.host, "h", "", "target host (IP or FQDN, optionally with :port); prompted for interactively when omitted")
	fs.StringVar(&v.host, "host", "", "target host, see -h")
	fs.StringVar(&v.username, "u", "", "username for authentication; prompted for interactively when omitted")
	fs.StringVar(&v.username, "username", "", "username for authentication, see -u")
	fs.StringVar(&v.secretsFile, "secrets-file", "", "JSON file with {host, username, password}; whatever it omits still falls back to its own flag or an interactive prompt")
	fs.IntVar(&v.port, "P", 0, fmt.Sprintf("override port (default: %d)", defaultPort))
	fs.IntVar(&v.port, "port", 0, "override port, see -P")
	fs.StringVar(&v.source, "s", "", "CSV file for batch mode")
	fs.StringVar(&v.source, "source", "", "CSV file for batch mode, see -s")
	fs.StringVar(&v.output, "o", "", "output directory for batch mode")
	fs.StringVar(&v.output, "output", "", "output directory for batch mode, see -o")
	fs.StringVar(&v.purposeStr, "purpose", "", osirismeta.PurposeHelp())
	fs.BoolVar(&v.includeRawBody, "include-raw-body", false, "attach each collected command's full, unmodified response body under extensions[\"osiris.cisco\"] on the owning resource (requires --purpose audit; a lossless fallback for fields not yet modeled by this producer)")
	fs.StringVar(&v.safeFail, "safe-failure-mode", "fail-closed", "secret handling: fail-closed, log-and-redact, or off")
	fs.BoolVar(&v.insecure, "insecure", false, "skip SSH host-key verification")

	return v
}

// ParseFlags parses CLI flags for the IOS-XE producer and returns a
// Config. In single mode (-h/--host), one target is created. In batch
// mode (-s/--source), targets are loaded from a CSV file via the
// shared run.ParseCSV.
func ParseFlags(args []string, promptVisible, promptHidden func(string) (string, error)) (*Config, error) {
	fs := flag.NewFlagSet("osirisjson-producer cisco iosxe", flag.ContinueOnError)
	v := registerFlags(fs)

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	if fs.NArg() > 0 {
		return nil, fmt.Errorf("unexpected positional argument(s): %v", fs.Args())
	}

	purpose, err := osirismeta.ParsePurpose(v.purposeStr)
	if err != nil {
		return nil, err
	}

	switch v.safeFail {
	case "fail-closed", "log-and-redact", "off":
		// valid
	default:
		return nil, fmt.Errorf("invalid --safe-failure-mode value %q: must be fail-closed, log-and-redact, or off", v.safeFail)
	}

	if v.host != "" && v.source != "" {
		return nil, fmt.Errorf("--host and --source are mutually exclusive: use one or the other")
	}

	var tf *run.CredentialFile
	if v.secretsFile != "" {
		loaded, err := run.LoadCredentialFile(v.secretsFile)
		if err != nil {
			return nil, fmt.Errorf("loading --secrets-file: %w", err)
		}
		tf = loaded
		if v.host == "" {
			v.host = tf.Host
		}
	}

	cfg := &Config{
		Purpose:         purpose.String(),
		IncludeRawBody:  v.includeRawBody,
		SafeFailureMode: v.safeFail,
		InsecureTLS:     v.insecure,
	}

	if v.source != "" {
		if v.output == "" {
			return nil, fmt.Errorf("batch mode (--source) requires --output directory")
		}
		cfg.Mode = run.ModeBatch
		cfg.OutputDir = v.output

		targets, err := run.ParseCSV(v.source, "iosxe")
		if err != nil {
			return nil, fmt.Errorf("parsing CSV %q: %w", v.source, err)
		}

		globalUser, globalCredPassword := tf.ResolveForHost("")
		if v.username == "" {
			v.username = globalUser
		}
		globalPassword, _ := resolvePassword(globalCredPassword, nil)

		for i := range targets {
			ruleUser, rulePassword := tf.ResolveForHost(targets[i].Host)

			if targets[i].Username == "" {
				if ruleUser != "" {
					targets[i].Username = ruleUser
				} else {
					targets[i].Username = v.username
				}
			}
			if targets[i].Password == "" {
				if rulePassword != "" {
					targets[i].Password = rulePassword
				} else {
					targets[i].Password = globalPassword
				}
			}
			if v.port != 0 && targets[i].Port == 0 {
				targets[i].Port = v.port
			}
		}

		cfg.Targets = targets
		return cfg, nil
	}

	// Single mode.
	cfg.Mode = run.ModeSingle

	if v.host == "" {
		if promptVisible == nil {
			return nil, fmt.Errorf("either --host (single mode) or --source (batch mode) is required")
		}
		hv, err := promptVisible("Target host for iosxe (IP or FQDN, optionally with :port): ")
		if err != nil {
			return nil, fmt.Errorf("reading host: %w", err)
		}
		if hv == "" {
			return nil, fmt.Errorf("--host is required")
		}
		v.host = hv
	}

	h, p, err := run.ParseHostPort(v.host)
	if err != nil {
		return nil, fmt.Errorf("invalid --host value: %w", err)
	}
	if v.port != 0 {
		p = v.port // explicit --port overrides host:port
	}

	tfUsername, tfPassword := tf.ResolveForHost(h)

	if v.username == "" {
		v.username = tfUsername
	}
	if v.username == "" {
		if promptVisible == nil {
			return nil, fmt.Errorf("--username is required in single mode")
		}
		uv, err := promptVisible("Username for authentication: ")
		if err != nil {
			return nil, fmt.Errorf("reading username: %w", err)
		}
		if uv == "" {
			return nil, fmt.Errorf("--username is required")
		}
		v.username = uv
	}

	password, err := resolvePassword(tfPassword, promptHidden)
	if err != nil {
		return nil, fmt.Errorf("reading password: %w", err)
	}
	if password == "" {
		fmt.Fprintf(os.Stderr, "warning: no password provided; authentication may fail\n")
	}

	cfg.Targets = []run.TargetConfig{{
		Host:     h,
		Port:     p,
		Hostname: h,
		Type:     "iosxe",
		Username: v.username,
		Password: password,
	}}

	return cfg, nil
}

func resolvePassword(credPassword string, promptHidden func(string) (string, error)) (string, error) {
	if credPassword != "" {
		return credPassword, nil
	}
	if v := os.Getenv(passwordEnvVar); v != "" {
		return v, nil
	}
	if promptHidden != nil {
		return promptHidden("Password for iosxe authentication: ")
	}
	return "", nil
}
