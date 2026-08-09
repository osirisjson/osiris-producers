// flags.go - CLI flag parsing for the Cisco Catalyst SD-WAN Manager
// (vManage) OSIRIS JSON producer.
//
// vManage is a single WAN-overlay controller (see config.go), not a
// per-device CSV batch target, so this producer intentionally has no
// -s/--source CSV batch mode, unlike apic/nxos/iosxe it does not
// conform to cisco/run's ProducerFactory/RunConfig shape at all. It
// reuses only the vendor-agnostic helpers from cisco/run
// (ParseHostPort, PromptPassword).
//
// There is deliberately no -p/--password flag: a CLI flag value is
// visible to any local user via `ps` and gets written to shell history,
// exactly the trace a password must never leave. The password is
// instead read from --token-file, or, when --token-file did not supply
// one, prompted for interactively on the controlling terminal (see
// tty.go) and held only in memory for the run. --host/--username follow
// the same fallback chain (flag, then --token-file, then an interactive
// prompt), so a bare "osirisjson-producer cisco vmanage" with no flags
// at all still works end to end.
//
// OSIRIS JSON Producer for Cisco SD-WAN Manager (vManage) introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package vmanage

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"go.osirisjson.org/producers/osiris/network/cisco/run"
	"go.osirisjson.org/producers/pkg/osirismeta"
)

// defaultPort is the vManage HTTPS port used when --host has no
// explicit :port and --port is not given.
const defaultPort = 443

// vmanageFlagValues holds every vmanage CLI flag's bound destination.
// registerFlags is the single place these flags are named and
// described - both ParseFlags (which parses them) and FlagsUsage
// (which renders their registered descriptions for vmanage.go's
// printHelp) build off this same registration, so the flag list shown
// in --help and the one flag.Parse falls back to printing on a parse
// error (e.g. an unrecognized flag) can never drift apart.
type vmanageFlagValues struct {
	host       string
	username   string
	tokenFile  string
	port       int
	output     string
	safeFail   string
	purposeStr string
	insecure   bool
	siteFlag   string
	allSites   bool
	siteRate   int

	includeRawBody bool
}

// registerFlags binds every vmanage CLI flag onto fs and returns their
// destinations. See vmanageFlagValues' doc comment for why this is
// factored out instead of living inline in ParseFlags.
func registerFlags(fs *flag.FlagSet) *vmanageFlagValues {
	v := &vmanageFlagValues{}

	fs.StringVar(&v.host, "h", "", "vManage controller host (FQDN or IP, e.g. acme.sdwan.cisco.com), optionally with :port; prompted for interactively when omitted")
	fs.StringVar(&v.host, "host", "", "vManage controller host, see -h")
	fs.StringVar(&v.username, "u", "", "username for authentication; prompted for interactively when omitted")
	fs.StringVar(&v.username, "username", "", "username for authentication, see -u")
	fs.StringVar(&v.tokenFile, "token-file", "", "JSON file with {host, username, password}; whatever it omits still falls back to its own flag or an interactive prompt")
	fs.IntVar(&v.port, "P", 0, fmt.Sprintf("override port (default: %d)", defaultPort))
	fs.IntVar(&v.port, "port", 0, "override port, see -P")
	fs.StringVar(&v.output, "o", "", fmt.Sprintf("output directory - every run writes <output-dir>/<site-name>/cisco-vmanage-<timestamp>-<site-name>.json, created if missing and reused if it already exists (default: %q in the current directory)", defaultOutputDir))
	fs.StringVar(&v.output, "output", "", "output directory, see -o")
	fs.StringVar(&v.safeFail, "safe-failure-mode", "fail-closed", "secret handling: fail-closed, log-and-redact, or off")
	fs.StringVar(&v.purposeStr, "purpose", "", osirismeta.PurposeHelp())
	fs.BoolVar(&v.insecure, "insecure", false, "skip TLS certificate verification")
	fs.StringVar(&v.siteFlag, "site", "", fmt.Sprintf("comma-separated site(s) to collect, by raw site-id or resolved site name (use %q for devices with no site-id, or \"all\" for --all); omit for an interactive picker", unsitedSegment))
	fs.BoolVar(&v.allSites, "all", false, "collect every site discovered, non-interactively (skips the site picker; mutually exclusive with --site)")
	fs.IntVar(&v.siteRate, "site-name-rate", defaultSiteNameRateLimit, fmt.Sprintf("requests/second for bulk site-name resolution (default: %d). vManage's own rate limit is typically shared across every API consumer hitting it, monitoring systems, other automation, etc. not just this producer, so raise or lower this based on how much headroom your environment actually has; the default assumes this producer is one of several concurrent consumers", defaultSiteNameRateLimit))
	fs.BoolVar(&v.includeRawBody, "include-raw-body", false, "attach each collected endpoint's full, unmodified API response body under extensions[\"osiris.cisco.vmanage\"] on the owning device resource (requires --purpose audit; a lossless fallback for fields not yet modeled by this producer)")

	// Overriding fs.Usage (rather than leaving flag.Parse's default,
	// which is "Usage of %s:" followed by fs.PrintDefaults()) means a
	// real parse error e.g. an unrecognized flag, which flag.Parse
	// reports by calling fs.Usage() before returning the error shows
	// the exact same aligned table as vmanage.go's --help (both call
	// renderFlagsTable against this same fs), not the stdlib's raw
	// two-line-per-flag dump. See renderFlagsTable doc comment for why
	// that default format was replaced: it splits each flag's name and
	// description across separate, unaligned lines with no merging of
	// short/long aliases, which reads poorly.
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage of %s:\n", fs.Name())
		fmt.Fprint(fs.Output(), renderFlagsTable(fs))
	}

	return v
}

// flagRows defines FlagsUsage's display order and short/long alias
// grouping, each entry lists every flag name bound to the same
// destination in registerFlags (e.g. {"h", "host"}), so they render as
// one merged row instead of flag.PrintDefaults()'s one-row-per-name
// default. Ordered by topic (auth, output, site selection, then
// cross-cutting flags) rather than fs.VisitAll's alphabetical order,
// which scatters related flags apart (e.g. "-P" sorting before "-all").
//
// renderFlagsTable falls back to appending any flag missing from this
// list at the end (in fs.VisitAll's order), so a flag added to
// registerFlags but forgotten here still appears in --help - just
// without deliberate placement rather than silently vanishing.
var flagRows = [][]string{
	{"h", "host"},
	{"u", "username"},
	{"P", "port"},
	{"token-file"},
	{"o", "output"},
	{"site"},
	{"all"},
	{"site-name-rate"},
	{"purpose"},
	{"safe-failure-mode"},
	{"insecure"},
	{"include-raw-body"},
}

// flagLabel renders a single flag name in its conventional dash form:
// "-x" for a one-character short flag, "--long" otherwise.
func flagLabel(name string) string {
	if len(name) == 1 {
		return "-" + name
	}
	return "--" + name
}

// helpWidth is the fixed total column budget renderFlagsTable wraps
// descriptions to, matching this repository's own line-width
// convention [RFC 7994](https://datatracker.ietf.org/doc/html/rfc7994#section-4.3).
const helpWidth = 72

// renderFlagsTable renders every flag registered on fs (via
// registerFlags) as a column-aligned, word-wrapped table one row per
// flagRows entry, short/long aliases merged onto a single row.
//
// Column width and wrapping are computed explicitly in one pass here
// (fmt/strings, both stdlib) rather than left to text/tabwriter's
// automatic per-block sizing, which this replaced: tabwriter only
// aligns already-terminated lines, it never wraps long text to a
// width, so a narrow terminal broke a long description back to column
// 0 mid-sentence; and it recomputes column width independently for each
// contiguous run of same-shaped rows, so the --purpose row (see below)
// which doesn't fit the regular "name, tab, description" shape silently
// split the table into two differently-sized column blocks on either
// side of it. A single explicit pass avoids both: one label width is
// computed across every row up front, so the column position is
// identical throughout the whole table regardless of what
// any individual row contains.
func renderFlagsTable(fs *flag.FlagSet) string {
	descByName := make(map[string]string)
	fs.VisitAll(func(f *flag.Flag) {
		descByName[f.Name] = f.Usage
	})

	type row struct {
		label string // "" for a self-labeled block (see below) excluded from the column width and left unindented.
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

		// Some shared help text (e.g. pkg/osirismeta.PurposeHelp() used
		// for --purpose) is pre-formatted for standalone embedding: it
		// already opens with its own "--name ..." header and has own
		// multi-level indentation for nested bullets, chosen to look
		// right when embedded on its own not to match this table's
		// column width. Prefixing our own label and column padding onto
		// it would double-indent every line without actually aligning
		// anything meaningful, so it is shown as its own unindented
		// block instead not column-aligned with the single-line flags
		// around it, but not mangled either. Every other producer that
		// uses this shared text embeds it the same standalone way.
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
	// (e.g. a new flag added to registerFlags without updating the
	// table above) still shows up, just without deliberate placement.
	fs.VisitAll(func(f *flag.Flag) {
		if !seen[f.Name] {
			addRow([]string{f.Name})
		}
	})

	const margin = 2 // leading spaces before the label column.
	const gap = 2    // spaces between the label column and the description column.
	labelWidth := 0
	for _, r := range rows {
		if len(r.label) > labelWidth {
			labelWidth = len(r.label)
		}
	}
	descWidth := helpWidth - margin - labelWidth - gap
	if descWidth < 20 {
		descWidth = 20 // never wrap so tight the text becomes unreadable.
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
// spaces only a single word longer than width is kept whole rather
// than broken mid-word, so it can run past width instead of splitting
// nonsensically. Any existing newlines in s are preserved as hard
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

// FlagsUsage renders the vmanage flag table exactly as registerFlags
// defines it the same source ParseFlags binds against so
// vmanage.go's printHelp can show it in --help without hand-retyping
// each flag and risking it drifting out of sync with the real flag
// set. See renderFlagsTable for the table layout.
func FlagsUsage() string {
	fs := flag.NewFlagSet("osirisjson-producer cisco vmanage", flag.ContinueOnError)
	registerFlags(fs)
	return renderFlagsTable(fs)
}

// tokenFileContents is the on-disk JSON shape for --token-file: a
// deliberately simple {host, username, password} object, generated by
// "osirisjson-producer cisco vmanage template --generate" (see
// runTemplate in vmanage.go). Any field left out here is still
// resolvable from its own -h/-u flag or, failing that, an interactive
// prompt (see ParseFlags), so a partially-filled file is fine.
type tokenFileContents struct {
	Host     string `json:"host,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

// ParseFlags parses CLI flags for the vManage producer and returns a
// Config. host/username/password are resolved in this order: the
// -h/-u flag, then --token-file, then (for whichever of the three is
// still missing) an interactive prompt on the controlling terminal
// promptVisible for host/username, promptHidden for password. Either
// prompt function may be nil (e.g. no controlling terminal available,
// such as under cron/CI), in which case a still-missing host or
// username is a hard error and a still-missing password is left empty
// with a warning, matching today's headless/automation behavior.
func ParseFlags(args []string, promptVisible, promptHidden func(string) (string, error)) (*Config, error) {
	fs := flag.NewFlagSet("osirisjson-producer cisco vmanage", flag.ContinueOnError)
	v := registerFlags(fs)

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	host, username, tokenFile := v.host, v.username, v.tokenFile
	port := v.port
	output, safeFail, purposeStr := v.output, v.safeFail, v.purposeStr
	insecure := v.insecure
	siteFlag, allSites, siteRate := v.siteFlag, v.allSites, v.siteRate
	includeRawBody := v.includeRawBody

	// --site all is an alias for --all: the interactive picker already
	// treats a typed "all" as "every site" (selectSitesInteractive), so
	// --site all matches that same expectation instead of being looked
	// up as a literal site-id/name (which would trigger a full bulk
	// name-resolution pass just to fail to match anything).
	if strings.EqualFold(strings.TrimSpace(siteFlag), "all") {
		allSites = true
		siteFlag = ""
	}

	if allSites && siteFlag != "" {
		return nil, fmt.Errorf("--all and --site are mutually exclusive: use one")
	}

	if siteRate <= 0 {
		return nil, fmt.Errorf("invalid --site-name-rate value %d: must be a positive integer", siteRate)
	}

	// Whatever the -h/-u flags didn't provide falls back to --token-file
	// next, before any interactive prompt is attempted.
	var password string
	if tokenFile != "" {
		loaded, err := loadTokenFile(tokenFile)
		if err != nil {
			return nil, fmt.Errorf("loading --token-file %q: %w", tokenFile, err)
		}
		if host == "" {
			host = loaded.Host
		}
		if username == "" {
			username = loaded.Username
		}
		password = loaded.Password
	}

	// Still-missing host/username are asked for interactively (see
	// tty.go's promptVisible) rather than failing outright, so a bare
	// "osirisjson-producer cisco vmanage" works end to end. No
	// controlling terminal (e.g. cron/CI with neither the flag nor
	// --token-file set) is a hard error, same as before this fallback
	// chain existed.
	if host == "" {
		if promptVisible == nil {
			return nil, fmt.Errorf("--host is required (e.g. --host acme.sdwan.cisco.com)")
		}
		v, err := promptVisible("vManage controller host (FQDN or IP, e.g. acme.sdwan.cisco.com): ")
		if err != nil {
			return nil, fmt.Errorf("reading host: %w", err)
		}
		if v == "" {
			return nil, fmt.Errorf("--host is required")
		}
		host = v
	}
	if username == "" {
		if promptVisible == nil {
			return nil, fmt.Errorf("--username is required")
		}
		v, err := promptVisible("Username for vManage authentication: ")
		if err != nil {
			return nil, fmt.Errorf("reading username: %w", err)
		}
		if v == "" {
			return nil, fmt.Errorf("--username is required")
		}
		username = v
	}

	switch safeFail {
	case "fail-closed", "log-and-redact", "off":
	default:
		return nil, fmt.Errorf("invalid --safe-failure-mode value %q: must be fail-closed, log-and-redact, or off", safeFail)
	}

	purpose, err := osirismeta.ParsePurpose(purposeStr)
	if err != nil {
		return nil, err
	}

	// Interactive password prompt when neither -h/-u... nor
	// --token-file supplied one.
	if password == "" && promptHidden != nil {
		p, err := promptHidden(fmt.Sprintf("Password for %s@%s: ", username, host))
		if err != nil {
			return nil, fmt.Errorf("reading password: %w", err)
		}
		password = p
	}
	if password == "" {
		fmt.Fprintf(os.Stderr, "warning: no password provided; authentication may fail\n")
	}

	h, p, err := run.ParseHostPort(host)
	if err != nil {
		return nil, fmt.Errorf("invalid --host value: %w", err)
	}
	if port != 0 {
		p = port // explicit --port overrides host:port.
	}
	if p == 0 {
		p = defaultPort
	}

	outputDir := output
	if outputDir == "" {
		outputDir = defaultOutputDir
	}

	var siteFilter []string
	for _, s := range strings.Split(siteFlag, ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			siteFilter = append(siteFilter, s)
		}
	}

	return &Config{
		Host:              h,
		Port:              p,
		Username:          username,
		Password:          password,
		InsecureTLS:       insecure,
		OutputDir:         outputDir,
		SafeFailureMode:   safeFail,
		Purpose:           purpose.String(),
		SiteFilter:        siteFilter,
		AllSites:          allSites,
		SiteNameRateLimit: siteRate,
		IncludeRawBody:    includeRawBody,
	}, nil
}

// loadTokenFile reads and parses a --token-file JSON document.
func loadTokenFile(path string) (*tokenFileContents, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var contents tokenFileContents
	if err := json.Unmarshal(data, &contents); err != nil {
		return nil, fmt.Errorf("parsing JSON: %w", err)
	}
	return &contents, nil
}

// TokenFileTemplate returns a skeleton --token-file JSON document,
// written by "osirisjson-producer cisco vmanage template --generate"
// (see runTemplate in vmanage.go).
func TokenFileTemplate() string {
	return `{
  "host": "acme.sdwan.cisco.com",
  "username": "user",
  "password": "changeme"
}
`
}
