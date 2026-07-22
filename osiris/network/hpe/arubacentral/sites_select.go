// sites_select.go - Interactive site selection for the HPE Aruba
// Networking Central OSIRIS JSON producer.
//
// When --site is not given, the account's sites are listed on stderr
// and the user picks a subset by number ("1", "1,3,5", "1-4",
// combinations of these, or "all"). Site names are not sensitive, so
// this reads from stdin/writes to stderr (unlike the credential prompts
// in tty.go, which use /dev/tty so they still work when stdin is piped
// and so secrets can be hidden).
//
// OSIRIS JSON Producer for HPE Aruba Networking Central introduction:
// [OSIRIS-JSON-HPE-ARUBA-NETWORKING]: https://docs.osirisjson.org/osiris-producers/network/hpe-aruba-networking-central
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package arubacentral

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// resolveSites determines the site filter to collect: the explicit
// --site flag value when given, otherwise an interactive pick from the
// account's sites. When the sites endpoint is unavailable or returns
// nothing (it is best-effort, see client.go), collection proceeds
// unfiltered rather than blocking on a list that can't be shown.
func resolveSites(cfg *Config, sitesFlag string) ([]string, error) {
	if sitesFlag != "" {
		var siteList []string
		for s := range strings.SplitSeq(sitesFlag, ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				siteList = append(siteList, s)
			}
		}
		return siteList, nil
	}

	return selectSitesFromAccount(NewClient(cfg, defaultLogger()))
}

// selectSitesFromAccount lists sites via client and hands them to the
// interactive picker. Split out from resolveSites so tests can inject a
// client pointed at a fake server instead of a real
// Aruba Central account.
func selectSitesFromAccount(client *Client) ([]string, error) {
	sites, err := client.ListSites()
	if err != nil || len(sites) == 0 {
		fmt.Fprintln(os.Stderr, "no sites available to choose from (continuing with every site); pass --site explicitly to filter")
		return nil, nil
	}

	return selectSitesInteractive(sites)
}

// resolveAllSites returns every site name in the account for --all,
// without prompting the non-interactive equivalent of typing "all" at
// the interactive picker, great for cron/CI automations
// where no TTY is attached.
func resolveAllSites(cfg *Config) ([]string, error) {
	return allSitesFromAccount(NewClient(cfg, defaultLogger()))
}

// allSitesFromAccount lists every site name via client,
// non-interactively. Split out from resolveAllSites so tests can inject
// a client pointed at a fake server instead of a real Aruba Central
// account. Degrades the same way selectSitesFromAccount does when the
// (best-effort, see client.go) sites endpoint is unavailable or empty:
// falls back to an unfiltered single-document collection rather than
// failing the run outright.
func allSitesFromAccount(client *Client) ([]string, error) {
	sites, err := client.ListSites()
	if err != nil || len(sites) == 0 {
		fmt.Fprintln(os.Stderr, "no sites available to enumerate for --all (continuing with every site unfiltered)")
		return nil, nil
	}
	return allSiteNames(sites), nil
}

// allSiteNames extracts every site's ScopeName, in listed order.
func allSiteNames(sites []Site) []string {
	names := make([]string, len(sites))
	for i, s := range sites {
		names[i] = s.ScopeName
	}
	return names
}

// selectSitesInteractive prints a numbered site table to stderr and
// prompts the user to choose a subset (or "all"). Returns every listed
// site name when the user selects "all" or leaves the prompt blank
// runExport (in arubacentral.go) collects and writes one document per
// site regardless of how many were selected, so "all" must resolve to
// an explicit name list like any other multi-site selection rather than
// an empty/unfiltered marker.
func selectSitesInteractive(sites []Site) ([]string, error) {
	fmt.Fprintf(os.Stderr, "\nSites in this Aruba Central account:\n\n")

	maxName := len("Site name")
	for _, s := range sites {
		if len(s.ScopeName) > maxName {
			maxName = len(s.ScopeName)
		}
	}

	noW := 4
	fmt.Fprintf(os.Stderr, "%-*s  %-*s  %s\n", noW, "No", maxName, "Site name", "Devices")
	fmt.Fprintf(os.Stderr, "%-*s  %-*s  %s\n", noW, strings.Repeat("-", noW), maxName, strings.Repeat("-", maxName), strings.Repeat("-", 7))
	for i, s := range sites {
		fmt.Fprintf(os.Stderr, "%-*d  %-*s  %d\n", noW, i+1, maxName, s.ScopeName, s.DeviceCount)
	}

	fmt.Fprintf(os.Stderr, "\nSelect sites (e.g. 1,3,5 or 1-4 or 'all'; Enter for all): ")

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("reading site selection: %w", err)
	}
	input = strings.TrimSpace(input)

	if input == "" || strings.EqualFold(input, "all") {
		return allSiteNames(sites), nil
	}

	indices, err := parseSiteSelection(input, len(sites))
	if err != nil {
		return nil, err
	}

	names := make([]string, len(indices))
	for i, idx := range indices {
		names[i] = sites[idx].ScopeName
	}
	return names, nil
}

// parseSiteSelection parses an interactive selection string into
// 0-based indices. Supports "all", individual numbers "1,3,5", ranges
// "30-55", and combinations "1,3,30-55".
func parseSiteSelection(input string, count int) ([]int, error) {
	if strings.EqualFold(strings.TrimSpace(input), "all") {
		indices := make([]int, count)
		for i := range indices {
			indices[i] = i
		}
		return indices, nil
	}

	seen := map[int]bool{}
	var indices []int

	for p := range strings.SplitSeq(input, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

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
		return nil, fmt.Errorf("no sites selected")
	}
	return indices, nil
}
