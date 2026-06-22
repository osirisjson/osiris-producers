// flags.go - CLI flag parsing for the AWS OSIRIS JSON producer.
// Supports operating modes:
//
//   - Single mode: --profile <profile> --region <region>
//   - Multi-region mode: --profile <profile> --all-regions -o dir
//   - CSV mode: --source accounts.csv -o dir
//
// Authentication relies on standard AWS credential resolution
// (profiles, environment variables, IAM roles, SSO).
//
// For an introduction to OSIRIS JSON Producer for Amazon Web Services see:
// [OSIRIS-JSON-AWS]: https://osirisjson.org/en/docs/producers/hyperscalers/amazon-aws

package aws

import (
	"bufio"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"go.osirisjson.org/producers/pkg/osirismeta"
)

// ParseFlags parses CLI flags for the AWS producer and returns a Config.
func ParseFlags(args []string) (*Config, error) {
	fs := flag.NewFlagSet("osirisjson-producer aws", flag.ContinueOnError)

	var (
		profile        string
		region         string
		allRegions     bool
		source         string
		output         string
		safeFail       string
		purposeStr     string
		includeRawBody bool
	)

	fs.StringVar(&profile, "P", "", "AWS CLI profile name")
	fs.StringVar(&profile, "profile", "", "AWS CLI profile name")
	fs.StringVar(&region, "R", "", "AWS region(s), comma-separated")
	fs.StringVar(&region, "region", "", "AWS region(s), comma-separated")
	fs.BoolVar(&allRegions, "all-regions", false, "iterate all default AWS regions")
	fs.StringVar(&source, "s", "", "CSV file for batch mode")
	fs.StringVar(&source, "source", "", "CSV file for batch mode")
	fs.StringVar(&output, "o", "", "output directory (required for --all-regions, --source, or multi-region)")
	fs.StringVar(&output, "output", "", "output directory")
	fs.StringVar(&safeFail, "safe-failure-mode", "fail-closed", "secret handling: fail-closed, log-and-redact, or off")
	fs.StringVar(&purposeStr, "purpose", "", "OSIRIS JSON spec chapter 13.1.3 output grade: documentation (default) or audit")
	fs.BoolVar(&includeRawBody, "include-raw-body", false, "attach full AWS SDK response body under extensions[\"osiris.aws.sdk\"].body (audit mode only; lossless fallback for unmodelled fields)")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	// Validate safe failure mode.
	switch safeFail {
	case "fail-closed", "log-and-redact", "off":
		// valid
	default:
		return nil, fmt.Errorf("invalid --safe-failure-mode value %q: must be fail-closed, log-and-redact, or off", safeFail)
	}

	purpose, err := osirismeta.ParsePurpose(purposeStr)
	if err != nil {
		return nil, err
	}

	// Mutual exclusivity checks.
	modes := 0
	if source != "" {
		modes++
	}
	if allRegions {
		modes++
	}
	if profile != "" {
		modes++
	}
	// region alone is not a "mode" - it's used with or without other flags.

	cfg := &Config{
		OutputDir:       output,
		SafeFailureMode: safeFail,
		Purpose:         purpose.String(),
		IncludeRawBody:  includeRawBody,
	}

	// No mode specified - launch interactive picker.
	if modes == 0 {
		targets, interactiveOutput, err := selectProfilesInteractive()
		if err != nil {
			return nil, err
		}
		cfg.Targets = targets
		if interactiveOutput != "" {
			cfg.OutputDir = interactiveOutput
		}
		return cfg, nil
	}

	// CSV batch mode.
	if source != "" {
		if output == "" {
			return nil, fmt.Errorf("--source requires --output directory")
		}
		targets, err := parseCSV(source)
		if err != nil {
			return nil, fmt.Errorf("parsing CSV %q: %w", source, err)
		}
		cfg.Targets = targets
		return cfg, nil
	}

	// Build a single target from flags.
	target := AccountTarget{
		Profile: profile,
	}

	if allRegions {
		if output == "" {
			return nil, fmt.Errorf("--all-regions requires --output directory")
		}
		target.Regions = DefaultRegions
	} else if region != "" {
		regions := strings.Split(region, ",")
		var cleaned []string
		for _, r := range regions {
			r = strings.TrimSpace(r)
			if r != "" {
				cleaned = append(cleaned, r)
			}
		}
		target.Regions = cleaned
		if len(cleaned) > 1 && output == "" {
			return nil, fmt.Errorf("multiple regions require --output directory")
		}
	}

	cfg.Targets = []AccountTarget{target}
	return cfg, nil
}

// CSVTemplate returns a CSV template string for batch collection of AWS accounts.
//
// Columns:
//
//	profile           - AWS CLI profile name (required)
//	account_id        - AWS account number, 12-digit (optional, resolved from STS if empty)
//	account_name      - Human-readable label (optional)
//	regions           - Comma-separated region list (optional; empty = all default regions)
//	environment       - Deployment stage: dv, np, pr (optional)
//	notes             - Free-text user notes (ignored by OSIRIS JSON producer)
//
// Authentication uses the AWS credential chain (profiles, env vars, IAM roles, SSO).
// Ensure the credentials have ReadOnly access to all target accounts.
// Output hierarchy: <output-dir>/<AccountID>/<timestamp>/<region>.json
func CSVTemplate() string {
	return `profile,account_id,account_name,regions,environment,notes
123456789012_ReadOnlyAccess,123456789012,my-nonprod-account,,np,Non-prod account
987654321098_ReadOnlyAccess,987654321098,my-prod-account,"us-east-1,eu-west-1",pr,Production account
`
}

// csvColumns defines the recognized column names and their indices.
type csvColumns struct {
	profile     int
	accountID   int
	accountName int
	regions     int
	environment int
	notes       int
}

// resolveColumns maps header names to column indices.
func resolveColumns(header []string) (*csvColumns, error) {
	idx := map[string]int{}
	for i, col := range header {
		idx[strings.TrimSpace(strings.ToLower(col))] = i
	}

	col := &csvColumns{
		profile: -1, accountID: -1, accountName: -1,
		regions: -1, environment: -1, notes: -1,
	}

	if v, ok := idx["profile"]; ok {
		col.profile = v
	} else {
		return nil, fmt.Errorf("CSV missing required column: profile")
	}

	if v, ok := idx["account_id"]; ok {
		col.accountID = v
	}
	if v, ok := idx["account_name"]; ok {
		col.accountName = v
	}
	if v, ok := idx["regions"]; ok {
		col.regions = v
	}
	if v, ok := idx["environment"]; ok {
		col.environment = v
	}
	if v, ok := idx["notes"]; ok {
		col.notes = v
	}

	return col, nil
}

// field safely reads a column value from a CSV record.
func field(record []string, idx int) string {
	if idx < 0 || idx >= len(record) {
		return ""
	}
	return strings.TrimSpace(record[idx])
}

// parseCSV reads an AWS account batch CSV file.
func parseCSV(path string) ([]AccountTarget, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.Comment = '#'
	r.TrimLeadingSpace = true

	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("reading CSV header: %w", err)
	}

	col, err := resolveColumns(header)
	if err != nil {
		return nil, err
	}

	var targets []AccountTarget
	lineNum := 1
	for {
		record, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading CSV row: %w", err)
		}
		lineNum++

		profile := field(record, col.profile)
		if profile == "" {
			continue
		}

		target := AccountTarget{
			Profile:     profile,
			AccountID:   field(record, col.accountID),
			AccountName: field(record, col.accountName),
			Environment: field(record, col.environment),
			Notes:       field(record, col.notes),
		}

		// Parse regions.
		regionsStr := field(record, col.regions)
		if regionsStr != "" {
			parts := strings.Split(regionsStr, ",")
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" {
					target.Regions = append(target.Regions, p)
				}
			}
		}

		targets = append(targets, target)
	}

	if len(targets) == 0 {
		return nil, fmt.Errorf("CSV file %q contains no targets", path)
	}

	_ = lineNum
	return targets, nil
}

// selectProfilesInteractive discovers AWS CLI profiles from ~/.aws/config
// and presents an interactive numbered list for the user to pick from.
// Returns the selected targets and the output directory (may be empty for single-region).
func selectProfilesInteractive() ([]AccountTarget, string, error) {
	profiles, err := discoverAWSProfiles()
	if err != nil {
		return nil, "", err
	}
	if len(profiles) == 0 {
		return nil, "", fmt.Errorf("no AWS profiles found in ~/.aws/config or ~/.aws/credentials\n\nConfigure profiles with:\n  aws configure --profile <name>\n  aws configure sso\n  osirisjson-producer aws setup-sso --start-url <URL>")
	}

	fmt.Fprintf(os.Stderr, "\nDiscovered AWS CLI profiles:\n\n")

	// Calculate column widths.
	maxName := len("Profile")
	for _, p := range profiles {
		if len(p) > maxName {
			maxName = len(p)
		}
	}

	noW := 4
	fmt.Fprintf(os.Stderr, "%-*s  %s\n", noW, "No", "Profile")
	fmt.Fprintf(os.Stderr, "%-*s  %s\n", noW, strings.Repeat("-", noW), strings.Repeat("-", maxName))

	for i, p := range profiles {
		fmt.Fprintf(os.Stderr, "%-*d  %s\n", noW, i+1, p)
	}

	fmt.Fprintf(os.Stderr, "\nSelect profiles (e.g. 1,3,5 or 30-55 or 'all'): ")
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return nil, "", fmt.Errorf("reading input: %w", err)
	}
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, "", fmt.Errorf("no profiles selected")
	}

	indices, err := parseSelection(input, len(profiles))
	if err != nil {
		return nil, "", err
	}
	selected := make([]string, len(indices))
	for i, idx := range indices {
		selected[i] = profiles[idx]
	}

	// Ask user about regions to select.
	fmt.Fprintf(os.Stderr, "\nAWS Region selection:\n")
	fmt.Fprintf(os.Stderr, "  1) All default regions (%d regions)\n", len(DefaultRegions))
	fmt.Fprintf(os.Stderr, "  2) Select specific region(s)\n")
	fmt.Fprintf(os.Stderr, "\nChoice [1]: ")

	regionInput, err := reader.ReadString('\n')
	if err != nil {
		return nil, "", fmt.Errorf("reading input: %w", err)
	}
	regionInput = strings.TrimSpace(regionInput)

	var regions []string
	switch regionInput {
	case "", "1":
		regions = DefaultRegions
	case "2":
		fmt.Fprintf(os.Stderr, "Select region(s), comma-separated (e.g. us-east-1,eu-west-1): ")
		regStr, err := reader.ReadString('\n')
		if err != nil {
			return nil, "", fmt.Errorf("reading input: %w", err)
		}
		regStr = strings.TrimSpace(regStr)
		if regStr == "" {
			return nil, "", fmt.Errorf("no regions specified")
		}
		for _, r := range strings.Split(regStr, ",") {
			r = strings.TrimSpace(r)
			if r != "" {
				regions = append(regions, r)
			}
		}
		if len(regions) == 0 {
			return nil, "", fmt.Errorf("no regions specified")
		}
	default:
		return nil, "", fmt.Errorf("invalid choice %q: enter 1 or 2", regionInput)
	}

	targets := make([]AccountTarget, len(selected))
	for i, p := range selected {
		targets[i] = AccountTarget{
			Profile: p,
			Regions: regions,
		}
	}

	return targets, "", nil
}

// discoverAWSProfiles reads AWS CLI profile names from ~/.aws/config and
// ~/.aws/credentials. In config, profiles appear as [profile <name>] sections
// (plus [default]). In credentials, every section header [<name>] is a profile.
// Results from both files are merged and deduplicated.
func discoverAWSProfiles() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("finding home directory: %w", err)
	}

	seen := map[string]bool{}

	// Parse ~/.aws/config - sections are [default] or [profile <name>].
	configPath := filepath.Join(home, ".aws", "config")
	if err := parseINISections(configPath, true, seen); err != nil {
		return nil, err
	}

	// Parse ~/.aws/credentials - every section [<name>] is a profile directly.
	credPath := filepath.Join(home, ".aws", "credentials")
	if err := parseINISections(credPath, false, seen); err != nil {
		return nil, err
	}

	profiles := make([]string, 0, len(seen))
	for p := range seen {
		profiles = append(profiles, p)
	}
	sort.Strings(profiles)
	return profiles, nil
}

// parseINISections reads INI-style section headers from a file and adds profile
// names to the seen map. When isConfig is true, sections follow the config file
// convention ([default] or [profile <name>]). When false, every section header
// is treated as a profile name (credentials file convention).
func parseINISections(path string, isConfig bool, seen map[string]bool) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "[") || !strings.HasSuffix(line, "]") {
			continue
		}
		section := strings.TrimSpace(line[1 : len(line)-1])

		var name string
		if isConfig {
			if section == "default" {
				name = "default"
			} else if after, ok := strings.CutPrefix(section, "profile "); ok {
				name = strings.TrimSpace(after)
			}
		} else {
			name = section
		}

		if name != "" {
			seen[name] = true
		}
	}
	return scanner.Err()
}

// parseSelection parses an interactive selection string into 0-based indices.
// Supports: "all", individual numbers "1,3,5", ranges "30-55", and combinations "1,3,30-55".
func parseSelection(input string, count int) ([]int, error) {
	if strings.EqualFold(strings.TrimSpace(input), "all") {
		indices := make([]int, count)
		for i := range indices {
			indices[i] = i
		}
		return indices, nil
	}

	seen := map[int]bool{}
	var indices []int

	parts := strings.Split(input, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		// Check for range: e.g. "30-55".
		if lo, hi, ok := strings.Cut(p, "-"); ok {
			loNum, errLo := strconv.Atoi(strings.TrimSpace(lo))
			hiNum, errHi := strconv.Atoi(strings.TrimSpace(hi))
			if errLo != nil || errHi != nil || loNum < 1 || hiNum < 1 || loNum > count || hiNum > count {
				return nil, fmt.Errorf("invalid range %q: enter numbers between 1 and %d", p, count)
			}
			if loNum > hiNum {
				return nil, fmt.Errorf("invalid range %q: start must be <= end", p)
			}
			for n := loNum; n <= hiNum; n++ {
				if !seen[n-1] {
					seen[n-1] = true
					indices = append(indices, n-1)
				}
			}
			continue
		}

		num, err := strconv.Atoi(p)
		if err != nil || num < 1 || num > count {
			return nil, fmt.Errorf("invalid selection %q: enter numbers between 1 and %d", p, count)
		}
		if !seen[num-1] {
			seen[num-1] = true
			indices = append(indices, num-1)
		}
	}

	if len(indices) == 0 {
		return nil, fmt.Errorf("no items selected")
	}
	return indices, nil
}
