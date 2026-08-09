// sites_select.go - Site selection for the Cisco Catalyst SD-WAN
// Manager (vManage) OSIRIS JSON producer.
//
// vManage still has no standalone sites API, site-id is only known
// per-device, from GET /dataservice/device. So site discovery and
// selection necessarily happen after the device inventory has already
// been fetched and grouped by site-id in runExport, not during flag
// parsing.
//
// When neither --site nor --all is given, the discovered sites (with
// device counts) are listed on stderr and the user picks a subset by
// number ("1", "1,3,5", "1-4", combinations of these, or "all"). Site
// ids are not sensitive, so this reads from stdin (unlike the
// credential prompt in cisco/run/tty.go, which uses /dev/tty so it
// still works when stdin is piped and so the password can be hidden).
//
// Deployments can have thousands of sites, so site-name resolution
// (one GetSiteName call per site) is done concurrently with a bounded
// worker pool and progress output rather than a sequential loop when it
// does happen only the interactive picker (which needs every
// discovered site's name to print its table) and a --site value that
// falls back to name matching trigger it. --all deliberately skips
// bulk resolution entirely: each site's name is instead resolved
// individually, lazily, as vmanage.go's export loop reaches that site,
// so the first document starts being written immediately instead of
// waiting on every site's name up front.
//
// OSIRIS JSON Producer for Cisco SD-WAN Manager (vManage) introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package vmanage

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// siteNameWorkers bounds how many concurrent GetSiteName calls run at
// once during bulk resolution (--all / interactive picker). Kept
// conservative since vManage rate-limits concurrent requests
// client.go's get() retries 429s with backoff, so occasional
// throttling at this concurrency degrades to "a bit slower," not
// "falls back to site-id."
const siteNameWorkers = 8

// siteSummary is one discovered site-id and how many devices belong to
// it, used to render the interactive picker table. name is populated
// only when bulk-resolved (see resolveSiteNamesConcurrently) it is a
// display concern, not part of site identity.
type siteSummary struct {
	id    string // device site-id, "" for the unclaimed fallback.
	name  string // best-effort human-readable name, "" if unresolved.
	count int
}

// resolveSiteSelection determines which site-ids to collect: --all
// (every discovered site, non-interactively), --site (the explicit
// list, matched against what was actually discovered by either raw
// site-id or resolved display name), or an interactive pick from the
// sites found in groups.
//
// It also returns a site-id -> name map for whichever sites it already
// had to resolve names for as part of selection. --all deliberately
// does not bulk-resolve names unlike the interactive picker, it has
// no table to print them into, so pre-resolving every site's name
// before the export loop even starts would only delay the first
// document being written, for no benefit (each site's name still gets
// resolved individually via vmanage.go's fallback once its own turn in
// the export loop comes up). A --site list that matches entirely by
// raw site-id also returns a nil map for the same reason - a --site
// list that needed name matching already has every name resolved
// though, so that map is returned instead of resolving the same sites
// a second time. client and logger are only used when name resolution
// actually happens - both may be nil for --all or a --site list that
// matches entirely by raw site-id.
func resolveSiteSelection(cfg *Config, groups map[string][]Device, client *Client, logger *slog.Logger) ([]string, map[string]string, error) {
	summaries := siteSummaries(groups)

	if cfg.AllSites {
		return siteIDs(summaries), nil, nil
	}

	if len(cfg.SiteFilter) > 0 {
		return resolveSiteFilter(cfg, groups, summaries, client, logger)
	}

	return selectSitesInteractive(summaries, client, logger)
}

// resolveSiteFilter matches --site's requested values against the
// discovered sites. Each value may be the raw numeric site-id (as
// vManage reports it) or the resolved display name (what the
// interactive picker, OutputPath's directory segment and
// metadata.scope.sites all actually show).
//
// Raw site-id matches short-circuit without any name resolution. Only
// once at least one requested value fails to match a raw id does this
// fall back to resolving every discovered site's name (via the same
// bounded, rate-limited resolver --all and the interactive picker
// already use) and matching case-insensitively against those there
// is no vManage endpoint to look up a site-id by name directly, so
// finding one requires having resolved all of them.
func resolveSiteFilter(cfg *Config, groups map[string][]Device, summaries []siteSummary, client *Client, logger *slog.Logger) ([]string, map[string]string, error) {
	normalized := make([]string, len(cfg.SiteFilter))
	allMatchByID := true
	for i, s := range cfg.SiteFilter {
		id := s
		if strings.EqualFold(s, unsitedSegment) {
			id = ""
		}
		normalized[i] = id
		if _, ok := groups[id]; !ok {
			allMatchByID = false
		}
	}
	if allMatchByID {
		return normalized, nil, nil
	}

	names := resolveSiteNamesConcurrently(client, summaries, logger)
	idByName := make(map[string]string, len(names))
	for id, name := range names {
		idByName[strings.ToLower(name)] = id
	}

	selected := make([]string, 0, len(cfg.SiteFilter))
	for i, s := range cfg.SiteFilter {
		id := normalized[i]
		if _, ok := groups[id]; ok {
			selected = append(selected, id)
			continue
		}
		nameMatch, ok := idByName[strings.ToLower(s)]
		if !ok {
			return nil, nil, fmt.Errorf("--site %q: no devices found with that site-id or site name", s)
		}
		selected = append(selected, nameMatch)
	}
	return selected, names, nil
}

// siteDisplayName resolves a best-effort human-readable name for a
// single site-id via client.GetSiteName. The unclaimed ("") bucket has no
// real site-id to query and always resolves to unsitedSegment. Any
// failure (missing permission, unreachable endpoint, empty name) falls
// back to the site-id itself rather than blocking the caller this is
// strictly a display/metadata enrichment, never a correctness
// requirement (see GetSiteName's doc comment in client.go).
//
// For resolving many sites at once, use resolveSiteNamesConcurrently
// instead a sequential loop over hundreds/thousands of sites is too
// slow to be usable.
func siteDisplayName(client *Client, siteID string, logger *slog.Logger) string {
	if siteID == "" {
		return unsitedSegment
	}
	if client == nil {
		return siteID
	}
	name, err := client.GetSiteName(siteID)
	if err != nil {
		if logger != nil {
			logger.Warn("site name lookup failed (using site-id instead)", "site_id", siteID, "err", err)
		}
		return siteID
	}
	if name == "" {
		return siteID
	}
	return name
}

// resolveSiteNamesConcurrently resolves siteDisplayName for every
// summary using a bounded worker pool (siteNameWorkers) the aggregate
// requests/second rate is capped by client's own throttleSiteName
// (client.go), not here, so that cap holds regardless of how many
// workers are in flight; the client was already constructed with that
// rate (see NewClient). Prints periodic progress to stderr since this
// can take a while against controllers with hundreds or thousands of
// sites. Mutates summaries[i].name in place and also returns a
// site-id -> name map for reuse by the caller (see
// resolveSiteSelection's doc comment).
func resolveSiteNamesConcurrently(client *Client, summaries []siteSummary, logger *slog.Logger) map[string]string {
	total := len(summaries)
	names := make(map[string]string, total)
	if total == 0 {
		return names
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, siteNameWorkers)
	done := 0

	for i := range summaries {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()

			name := siteDisplayName(client, summaries[i].id, logger)

			mu.Lock()
			summaries[i].name = name
			names[summaries[i].id] = name
			done++
			if done%100 == 0 || done == total {
				fmt.Fprintf(os.Stderr, "  resolved %d/%d\n", done, total)
			}
			mu.Unlock()
		}(i)
	}
	wg.Wait()

	return names
}

// siteSummaries builds a deterministically ordered summary of the
// discovered sites: numeric site-ids sorted numerically, non-numeric
// ids sorted lexicographically after them, and the unclaimed ("")
// bucket always listed last.
func siteSummaries(groups map[string][]Device) []siteSummary {
	summaries := make([]siteSummary, 0, len(groups))
	for id, devices := range groups {
		summaries = append(summaries, siteSummary{id: id, count: len(devices)})
	}
	sort.Slice(summaries, func(i, j int) bool {
		a, b := summaries[i], summaries[j]
		if a.id == "" {
			return false
		}
		if b.id == "" {
			return true
		}
		an, aErr := strconv.Atoi(a.id)
		bn, bErr := strconv.Atoi(b.id)
		if aErr == nil && bErr == nil {
			return an < bn
		}
		return a.id < b.id
	})
	return summaries
}

// siteIDs extracts the site-id from each summary, in listed order.
func siteIDs(summaries []siteSummary) []string {
	ids := make([]string, len(summaries))
	for i, s := range summaries {
		ids[i] = s.id
	}
	return ids
}

// selectSitesInteractive prints a numbered site table to stderr and
// prompts the user to choose a subset (or "all"). Returns every listed
// site-id when the user selects "all" or leaves the prompt blank
// runExport writes one document per selected site, so "all" must
// resolve to an explicit id list like any other multi-site selection.
//
// The table shows No/Site name/Devices not the raw site-id, which
// stays purely internal (selection is always by row number, never by
// name or id text).
func selectSitesInteractive(summaries []siteSummary, client *Client, logger *slog.Logger) ([]string, map[string]string, error) {
	if len(summaries) == 0 {
		return nil, nil, fmt.Errorf("no devices returned by the controller, nothing to select")
	}

	names := resolveSiteNamesConcurrently(client, summaries, logger)

	fmt.Fprintf(os.Stderr, "\nSites discovered on this vManage controller:\n\n")

	maxName := len("Site name")
	for _, s := range summaries {
		if len(s.name) > maxName {
			maxName = len(s.name)
		}
	}

	noW := 4
	fmt.Fprintf(os.Stderr, "%-*s  %-*s  %s\n", noW, "No", maxName, "Site name", "Devices")
	fmt.Fprintf(os.Stderr, "%-*s  %-*s  %s\n", noW, strings.Repeat("-", noW), maxName, strings.Repeat("-", maxName), strings.Repeat("-", 7))
	for i, s := range summaries {
		fmt.Fprintf(os.Stderr, "%-*d  %-*s  %d\n", noW, i+1, maxName, s.name, s.count)
	}

	fmt.Fprintf(os.Stderr, "\nSelect sites (e.g. 1,3,5 or 1-4 or 'all'; Enter for all): ")

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return nil, nil, fmt.Errorf("reading site selection: %w", err)
	}
	input = strings.TrimSpace(input)

	if input == "" || strings.EqualFold(input, "all") {
		return siteIDs(summaries), names, nil
	}

	indices, err := parseSiteSelection(input, len(summaries))
	if err != nil {
		return nil, nil, err
	}

	ids := make([]string, len(indices))
	for i, idx := range indices {
		ids[i] = summaries[idx].id
	}
	return ids, names, nil
}

// parseSiteSelection parses an interactive selection string into
// 0-based indices. Supports "all", individual numbers "1,3,5", ranges
// "3-5", and combinations "1,3,5-7".
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

	for _, p := range strings.Split(input, ",") {
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
