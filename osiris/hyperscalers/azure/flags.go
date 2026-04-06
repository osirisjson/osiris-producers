// flags.go - CLI flag parsing for the Azure OSIRIS JSON producer.
// Supports three operating modes:
//
//   - Single mode: --subscription <id> (writes to stdout)
//   - Multi mode: --subscription sub1,sub2 -o dir (specific subscriptions)
//   - All mode: --all -o dir (auto-discovers every accessible subscription)
//   - CSV mode: --source subscriptions.csv -o dir (batch from file)
//
// Authentication relies on the Azure CLI login context (az login).
//
// For an introduction to OSIRIS JSON Producer for Microsoft Azure see:
// "[OSIRIS-JSON-AZURE]."
//
// [OSIRIS-JSON-AZURE]: https://osirisjson.org/en/docs/producers/hyperscalers/microsoft-azure

package azure

import (
	"bufio"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// ParseFlags parses CLI flags for the Azure producer and returns a Config.
func ParseFlags(args []string) (*Config, error) {
	fs := flag.NewFlagSet("osirisjson-producer azure", flag.ContinueOnError)

	var (
		subscription string
		tenant       string
		region       string
		all          bool
		source       string
		output       string
		detail       string
		safeFail     string
	)

	fs.StringVar(&subscription, "S", "", "Azure subscription ID(s), comma-separated")
	fs.StringVar(&subscription, "subscription", "", "Azure subscription ID(s), comma-separated")
	fs.StringVar(&tenant, "tenant", "", "Azure AD tenant ID (optional; defaults to CLI context)")
	fs.StringVar(&region, "region", "", "filter to a specific Azure region (optional)")
	fs.BoolVar(&all, "all", false, "auto-discover and export all accessible subscriptions")
	fs.StringVar(&source, "s", "", "CSV file for batch mode")
	fs.StringVar(&source, "source", "", "CSV file for batch mode")
	fs.StringVar(&output, "o", "", "output directory (required for --all, --source, or multi-subscription)")
	fs.StringVar(&output, "output", "", "output directory")
	fs.StringVar(&detail, "detail", "minimal", "detail level: minimal or detailed")
	fs.StringVar(&safeFail, "safe-failure-mode", "fail-closed", "secret handling: fail-closed, log-and-redact, or off")

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

	// Mutual exclusivity checks.
	modes := 0
	if subscription != "" {
		modes++
	}
	if all {
		modes++
	}
	if source != "" {
		modes++
	}
	if modes > 1 {
		return nil, fmt.Errorf("--subscription, --all, and --source are mutually exclusive: use one")
	}
	if modes == 0 {
		// No mode specified - launch interactive subscription picker.
		targets, err := selectSubscriptionsInteractive(tenant, region)
		if err != nil {
			return nil, err
		}
		cfg := &Config{
			OutputDir:       output,
			DetailLevel:     detail,
			SafeFailureMode: safeFail,
			Targets:         targets,
		}
		return cfg, nil
	}

	cfg := &Config{
		OutputDir:       output,
		DetailLevel:     detail,
		SafeFailureMode: safeFail,
	}

	// --all: auto-discover subscriptions.
	if all {
		if output == "" {
			return nil, fmt.Errorf("--all requires --output directory")
		}
		targets, err := discoverSubscriptions(tenant)
		if err != nil {
			return nil, fmt.Errorf("discovering subscriptions: %w", err)
		}
		// Apply region filter.
		for i := range targets {
			if targets[i].Region == "" && region != "" {
				targets[i].Region = region
			}
		}
		cfg.Targets = targets
		return cfg, nil
	}

	// --source CSV.
	if source != "" {
		if output == "" {
			return nil, fmt.Errorf("--source requires --output directory")
		}
		targets, err := parseCSV(source)
		if err != nil {
			return nil, fmt.Errorf("parsing CSV %q: %w", source, err)
		}
		for i := range targets {
			if targets[i].TenantID == "" && tenant != "" {
				targets[i].TenantID = tenant
			}
			if targets[i].Region == "" && region != "" {
				targets[i].Region = region
			}
		}
		cfg.Targets = targets
		return cfg, nil
	}

	// --subscription (one or more, comma-separated).
	subs := strings.Split(subscription, ",")
	for _, s := range subs {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		cfg.Targets = append(cfg.Targets, SubscriptionTarget{
			SubscriptionID:   s,
			SubscriptionName: s,
			TenantID:         tenant,
			Region:           region,
		})
	}
	if len(cfg.Targets) == 0 {
		return nil, fmt.Errorf("--subscription requires at least one subscription ID")
	}

	return cfg, nil
}

// CSVTemplate returns a CSV template string for batch collection of Azure subscriptions.
//
// Columns:
//
//	subscription_id   - Azure subscription UUID (required)
//	subscription_name - Human-readable label used as output filename (required)
//	tenant_id         - Azure AD / Entra ID tenant UUID (optional)
//	environment       - Deployment stage: dv, np, pr (optional)
//	region            - Filter to Azure region (optional; empty = all regions)
//	notes             - Free-text operator notes (ignored by producer)
//
// Authentication uses the Azure CLI context (az login).
// Ensure the logged-in principal has Reader access to all target subscriptions.
// Output hierarchy: <output-dir>/<TenantID>/<timestamp>/<SubscriptionName>.json
func CSVTemplate() string {
	return `subscription_id,subscription_name,tenant_id,environment,region,notes
00000000-0000-0000-0000-000000000001,my-nonprod-subscription,,np,,Non-prod subscription
00000000-0000-0000-0000-000000000002,my-prod-subscription,,pr,,Production subscription
`
}

// csvColumns defines the recognized column names and their indices.
type csvColumns struct {
	subscriptionID   int
	subscriptionName int
	tenantID         int
	environment      int
	region           int
	notes            int
}

// resolveColumns maps header names to column indices.
func resolveColumns(header []string) (*csvColumns, error) {
	idx := map[string]int{}
	for i, col := range header {
		idx[strings.TrimSpace(strings.ToLower(col))] = i
	}

	col := &csvColumns{
		subscriptionID: -1, subscriptionName: -1, tenantID: -1,
		environment: -1, region: -1, notes: -1,
	}

	var missing []string
	if v, ok := idx["subscription_id"]; ok {
		col.subscriptionID = v
	} else {
		missing = append(missing, "subscription_id")
	}
	if v, ok := idx["subscription_name"]; ok {
		col.subscriptionName = v
	} else {
		missing = append(missing, "subscription_name")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("CSV missing required columns: %s", strings.Join(missing, ", "))
	}

	if v, ok := idx["tenant_id"]; ok {
		col.tenantID = v
	}
	if v, ok := idx["environment"]; ok {
		col.environment = v
	}
	if v, ok := idx["region"]; ok {
		col.region = v
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

// parseCSV reads an Azure subscription batch CSV file.
func parseCSV(path string) ([]SubscriptionTarget, error) {
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

	var targets []SubscriptionTarget
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

		subID := field(record, col.subscriptionID)
		if subID == "" {
			continue
		}

		subName := field(record, col.subscriptionName)
		if subName == "" {
			return nil, fmt.Errorf("line %d: subscription_name is required", lineNum)
		}

		targets = append(targets, SubscriptionTarget{
			SubscriptionID:   subID,
			SubscriptionName: subName,
			TenantID:         field(record, col.tenantID),
			Environment:      field(record, col.environment),
			Region:           field(record, col.region),
			Notes:            field(record, col.notes),
		})
	}

	if len(targets) == 0 {
		return nil, fmt.Errorf("CSV file %q contains no targets", path)
	}

	return targets, nil
}

// selectSubscriptionsInteractive discovers available subscriptions and presents
// an interactive numbered list for the user to pick from.
func selectSubscriptionsInteractive(tenantFilter, regionFilter string) ([]SubscriptionTarget, error) {
	available, err := discoverSubscriptions(tenantFilter)
	if err != nil {
		return nil, err
	}

	fmt.Fprintf(os.Stderr, "\nRetrieving tenants and subscriptions for the selection...\n\n")

	// Calculate column widths for alignment.
	maxName := len("Subscription name")
	maxID := len("Subscription ID")
	for _, sub := range available {
		if len(sub.SubscriptionName) > maxName {
			maxName = len(sub.SubscriptionName)
		}
		if len(sub.SubscriptionID) > maxID {
			maxID = len(sub.SubscriptionID)
		}
	}

	// Header.
	noW := 4
	sep := fmt.Sprintf("%-*s  %-*s  %-*s  %s\n", noW, "No", maxName, "Subscription name", maxID, "Subscription ID", "Tenant")
	fmt.Fprint(os.Stderr, sep)
	fmt.Fprintf(os.Stderr, "%-*s  %-*s  %-*s  %s\n",
		noW, strings.Repeat("-", noW),
		maxName, strings.Repeat("-", maxName),
		maxID, strings.Repeat("-", maxID),
		strings.Repeat("-", 36))

	// Rows.
	for i, sub := range available {
		fmt.Fprintf(os.Stderr, "%-*d  %-*s  %-*s  %s\n",
			noW, i+1,
			maxName, sub.SubscriptionName,
			maxID, sub.SubscriptionID,
			sub.TenantID)
	}

	fmt.Fprintf(os.Stderr, "\nSelect subscriptions (comma-separated numbers, or 'all'): ")

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("reading input: %w", err)
	}
	input = strings.TrimSpace(input)

	if input == "" {
		return nil, fmt.Errorf("no subscriptions selected")
	}

	if strings.EqualFold(input, "all") {
		targets := make([]SubscriptionTarget, len(available))
		copy(targets, available)
		for i := range targets {
			if targets[i].Region == "" && regionFilter != "" {
				targets[i].Region = regionFilter
			}
		}
		return targets, nil
	}

	var targets []SubscriptionTarget
	parts := strings.Split(input, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		num, err := strconv.Atoi(p)
		if err != nil || num < 1 || num > len(available) {
			return nil, fmt.Errorf("invalid selection %q: enter numbers between 1 and %d", p, len(available))
		}
		target := available[num-1]
		if target.Region == "" && regionFilter != "" {
			target.Region = regionFilter
		}
		targets = append(targets, target)
	}

	if len(targets) == 0 {
		return nil, fmt.Errorf("no subscriptions selected")
	}

	return targets, nil
}
