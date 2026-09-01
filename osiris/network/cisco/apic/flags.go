// flags.go - CLI flag parsing for the Cisco ACI/APIC OSIRIS JSON
// producer.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package apic

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"go.osirisjson.org/producers/osiris/network/cisco/run"
	"go.osirisjson.org/producers/pkg/osirismeta"
)

// passwordEnvVar is the environment variable checked for a password
// when neither --secrets-file nor an interactive prompt supplied one.
const passwordEnvVar = "OSIRISJSON_CISCO_APIC_PASSWORD"

// apicFlagValues holds every APIC CLI flag's bound destination.
// registerFlags is the single place these flags are named and
// described both ParseFlags (which parses them) and FlagsUsage (which
// renders their registered descriptions for apic.go's printHelp) build
// off that same registration, so the flag list shown in --help and the
// one flag.Parse falls back to printing on a parse error (an
// unrecognized flag, say) can never drift apart.
type apicFlagValues struct {
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

// registerFlags binds every APIC CLI flag onto fs and returns their
// destinations.
func registerFlags(fs *flag.FlagSet) *apicFlagValues {
	v := &apicFlagValues{}

	fs.StringVar(&v.host, "h", "", "target host (IP or FQDN, optionally with :port); prompted for interactively when omitted")
	fs.StringVar(&v.host, "host", "", "target host, see -h")
	fs.StringVar(&v.username, "u", "", "username for authentication; prompted for interactively when omitted")
	fs.StringVar(&v.username, "username", "", "username for authentication, see -u")
	fs.StringVar(&v.secretsFile, "secrets-file", "", "JSON file with {host, username, password}; whatever it omits still falls back to its own flag or an interactive prompt")
	fs.IntVar(&v.port, "P", 0, fmt.Sprintf("override port (default: %d)", defaultAPICPort))
	fs.IntVar(&v.port, "port", 0, "override port, see -P")
	fs.StringVar(&v.source, "s", "", "CSV file for batch mode")
	fs.StringVar(&v.source, "source", "", "CSV file for batch mode, see -s")
	fs.StringVar(&v.output, "o", "", "output directory for batch mode")
	fs.StringVar(&v.output, "output", "", "output directory for batch mode, see -o")
	fs.StringVar(&v.purposeStr, "purpose", "", osirismeta.PurposeHelp())
	fs.BoolVar(&v.includeRawBody, "include-raw-body", false, "attach each collected command's full, unmodified response body under extensions[\"osiris.cisco\"] on the owning resource (requires --purpose audit; a lossless fallback for fields not yet modeled by this producer)")
	fs.StringVar(&v.safeFail, "safe-failure-mode", "fail-closed", "secret handling: fail-closed, log-and-redact, or off")
	fs.BoolVar(&v.insecure, "insecure", false, "skip TLS certificate verification")

	// Overriding fs.Usage (rather than leaving flag.Parse's default,
	// "Usage of %s:" followed by fs.PrintDefaults()) means a real parse
	// error an unrecognized flag, which flag.Parse reports by calling
	// fs.Usage() before returning shows the exact same aligned table
	// as apic.go's --help (both render renderFlagsTable against this
	// same fs), not the stdlib's raw two-line-per-flag dump.
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage of %s:\n", fs.Name())
		fmt.Fprint(fs.Output(), renderFlagsTable(fs))
	}

	return v
}

// flagRows defines FlagsUsage's display order and short/long alias
// grouping: each entry lists every flag name bound to the same
// destination in registerFlags (e.g. {"h", "host"}) so they render as
// one merged row instead of flag.PrintDefaults() one-row-per-name
// default. Ordered by topic (auth, batch/output, then cross-cutting
// flags) rather than fs.VisitAll alphabetical order, which scatters
// related flags apart. A flag registered on fs but missing here still
// appears in --help, appended at the end in fs.VisitAll order.
var flagRows = [][]string{
	{"h", "host"},
	{"u", "username"},
	{"P", "port"},
	{"secrets-file"},
	{"s", "source"},
	{"o", "output"},
	{"purpose"},
	{"include-raw-body"},
	{"safe-failure-mode"},
	{"insecure"},
}

// flagLabel renders a flag name in its conventional dash form: "-x" for
// a one-character short flag, "--long" otherwise.
func flagLabel(name string) string {
	if len(name) == 1 {
		return "-" + name
	}
	return "--" + name
}

// helpWidth is the fixed total column budget renderFlagsTable wraps
// descriptions to, matching this repository's line-width convention
// [RFC 7994](https://datatracker.ietf.org/doc/html/rfc7994#section-4.3).
const helpWidth = 72

// renderFlagsTable renders every flag registered on fs (via
// registerFlags) as a column-aligned, word-wrapped table: one row per
// flagRows entry, short/long aliases merged onto a single row. Column
// width and wrapping are computed in one explicit pass (fmt/strings,
// both stdlib) so the column position is identical for every row
// regardless of what any individual row contains unlike text/tabwriter,
// which never wraps long text to a width and re-sizes columns per
// contiguous same-shaped block.
func renderFlagsTable(fs *flag.FlagSet) string {
	descByName := make(map[string]string)
	fs.VisitAll(func(f *flag.Flag) {
		descByName[f.Name] = f.Usage
	})

	type row struct {
		label string // "" for a self-labeled block, left unindented and out of the column-width calc.
		desc  string
	}
	var rows []row
	seen := make(map[string]bool, len(descByName))

	addRow := func(names []string) {
		desc, ok := descByName[names[0]]
		if !ok {
			return // names[0] isn't registered on this fs; skip the row.
		}
		labels := make([]string, len(names))
		for i, n := range names {
			labels[i] = flagLabel(n)
			seen[n] = true
		}
		label := strings.Join(labels, ", ")

		// Shared help text such as pkg/osirismeta.PurposeHelp()
		// (used for --purpose) is pre-formatted for standalone
		// embedding: it already opens with its own "--name ..." header
		// and carries its own nested-bullet indentation, sized to look
		// right on its own, not to match this table's column.
		// Re-prefixing our label and column padding would double-indent
		// every line without aligning anything, so it is emitted as its
		// own unindented block instead.
		if strings.HasPrefix(strings.TrimSpace(desc), label+" ") {
			rows = append(rows, row{label: "", desc: desc})
			return
		}
		rows = append(rows, row{label: label, desc: desc})
	}

	for _, group := range flagRows {
		addRow(group)
	}
	// Safety net: anything registered on fs but missing from flagRows
	// still shows up, just without deliberate placement.
	fs.VisitAll(func(f *flag.Flag) {
		if !seen[f.Name] {
			addRow([]string{f.Name})
		}
	})

	const margin = 2 // leading spaces before the label column.
	const gap = 2    // spaces between the label and description columns.
	labelWidth := 0
	for _, r := range rows {
		if len(r.label) > labelWidth {
			labelWidth = len(r.label)
		}
	}
	descWidth := helpWidth - margin - labelWidth - gap
	if descWidth < 20 {
		descWidth = 20 // never wrap so tight the text is unreadable.
	}

	var buf strings.Builder
	indent := strings.Repeat(" ", margin+labelWidth+gap)
	for _, r := range rows {
		if r.label == "" {
			buf.WriteString(r.desc)
			buf.WriteByte('\n')
			continue
		}
		lines := wordWrap(r.desc, descWidth)
		fmt.Fprintf(&buf, "%s%-*s%s\n", strings.Repeat(" ", margin), labelWidth+gap, r.label, lines[0])
		for _, cont := range lines[1:] {
			buf.WriteString(indent)
			buf.WriteString(cont)
			buf.WriteByte('\n')
		}
	}
	return buf.String()
}

// wordWrap splits s into lines at most width runes long, breaking on
// spaces only: a single word longer than width is kept whole rather
// than broken mid-word. Existing newlines in s are preserved as hard
// paragraph breaks, each wrapped independently.
func wordWrap(s string, width int) []string {
	var out []string
	for _, para := range strings.Split(s, "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			out = append(out, "")
			continue
		}
		line := words[0]
		for _, w := range words[1:] {
			if len(line)+1+len(w) > width {
				out = append(out, line)
				line = w
				continue
			}
			line += " " + w
		}
		out = append(out, line)
	}
	return out
}

// FlagsUsage renders the APIC flag table exactly as registerFlags
// defines it the same source ParseFlags binds against so apic.go's
// printHelp can show it without hand-retyping each flag and risking
// drift from the real flag set.
func FlagsUsage() string {
	fs := flag.NewFlagSet("osirisjson-producer cisco apic", flag.ContinueOnError)
	registerFlags(fs)
	return renderFlagsTable(fs)
}

// ParseFlags parses CLI flags for the APIC producer and returns a
// Config. In single mode (-h/--host), one target is created. In batch
// mode (-s/--source), targets are loaded from a CSV file via the
// shared run.ParseCSV.
func ParseFlags(args []string, promptVisible, promptHidden func(string) (string, error)) (*Config, error) {
	fs := flag.NewFlagSet("osirisjson-producer cisco apic", flag.ContinueOnError)
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

		targets, err := run.ParseCSV(v.source, "apic")
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
		hv, err := promptVisible("Target host for apic (IP or FQDN, optionally with :port): ")
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
		Type:     "apic",
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
		return promptHidden("Password for apic authentication: ")
	}
	return "", nil
}
