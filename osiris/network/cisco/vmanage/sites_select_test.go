// sites_select_test.go - Tests for site-id discovery, --site/--all
// resolution, and the interactive picker's selection-string parsing.
//
// OSIRIS JSON Producer for Cisco SD-WAN Manager (vManage) introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package vmanage

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func testGroups() map[string][]Device {
	return map[string][]Device{
		"200": {{UUID: "u1"}, {UUID: "u2"}},
		"100": {{UUID: "u3"}},
		"":    {{UUID: "u4"}}, // unclaimed.
	}
}

func TestSiteSummaries_SortedNumericallyWithUnsitedLast(t *testing.T) {
	summaries := siteSummaries(testGroups())
	var ids []string
	for _, s := range summaries {
		ids = append(ids, s.id)
	}
	want := []string{"100", "200", ""}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("siteSummaries order = %v, want %v", ids, want)
	}
}

func TestResolveSiteSelection_All(t *testing.T) {
	cfg := &Config{AllSites: true}
	ids, names, err := resolveSiteSelection(cfg, testGroups(), nil, nil)
	if err != nil {
		t.Fatalf("resolveSiteSelection failed: %v", err)
	}
	if len(ids) != 3 {
		t.Fatalf("expected 3 site ids, got %d: %v", len(ids), ids)
	}
	// --all deliberately does not bulk-resolve names up front each
	// site's name is resolved individually, lazily, once the export
	// loop reaches it (vmanage.go's fallback), so the first document
	// starts being written immediately instead of waiting on every
	// site's name first.
	if names != nil {
		t.Errorf("expected a nil names map (no bulk resolution for --all), got %v", names)
	}
}

func TestResolveSiteSelection_ExplicitSiteFilter(t *testing.T) {
	cfg := &Config{SiteFilter: []string{"100"}}
	ids, names, err := resolveSiteSelection(cfg, testGroups(), nil, nil)
	if err != nil {
		t.Fatalf("resolveSiteSelection failed: %v", err)
	}
	if !reflect.DeepEqual(ids, []string{"100"}) {
		t.Errorf("ids = %v, want [100]", ids)
	}
	if names != nil {
		t.Errorf("--site should not bulk-resolve names, got %v", names)
	}
}

func TestResolveSiteSelection_UnsitedToken(t *testing.T) {
	cfg := &Config{SiteFilter: []string{unsitedSegment}}
	ids, _, err := resolveSiteSelection(cfg, testGroups(), nil, nil)
	if err != nil {
		t.Fatalf("resolveSiteSelection failed: %v", err)
	}
	if !reflect.DeepEqual(ids, []string{""}) {
		t.Errorf("ids = %v, want [\"\"] (unclaimed maps to empty site-id)", ids)
	}
}

func TestResolveSiteSelection_UnknownSiteErrors(t *testing.T) {
	// "999" matches no raw site-id, so this falls back to name
	// resolution (client is nil here, so siteDisplayName degrades to
	// returning each site-id as its own "name" still legitimately no
	// match for "999") before concluding there's no such site. A fast
	// SiteNameRateLimit keeps that fallback pass from sleeping in this
	// test.
	cfg := &Config{SiteFilter: []string{"999"}, SiteNameRateLimit: 1000}
	if _, _, err := resolveSiteSelection(cfg, testGroups(), nil, nil); err == nil {
		t.Fatal("expected error for a site-id/name that was not discovered")
	}
}

func TestResolveSiteSelection_SiteFilterByName(t *testing.T) {
	// --site accepts the resolved display name too, not just the raw
	// numeric site-id.
	names := map[string]string{"100": "MXP", "200": "BRANCH-1"}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		siteID := r.URL.Path[len("/dataservice/topology/device/site/"):]
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"site_name":%q,"data":[]}`, names[siteID])
	}))
	defer ts.Close()
	client := &Client{baseURL: ts.URL, httpClient: ts.Client(), logger: testLogger(t)}

	cfg := &Config{SiteFilter: []string{"mxp"}, SiteNameRateLimit: 1000}
	ids, resolvedNames, err := resolveSiteSelection(cfg, testGroups(), client, testLogger(t))
	if err != nil {
		t.Fatalf("resolveSiteSelection failed: %v", err)
	}
	if !reflect.DeepEqual(ids, []string{"100"}) {
		t.Errorf("ids = %v, want [100] (matched by name, case-insensitively)", ids)
	}
	if resolvedNames["100"] != "MXP" {
		t.Errorf("resolvedNames[100] = %q, want MXP - a name-matched filter should return the names it already had to resolve", resolvedNames["100"])
	}
}

func TestResolveSiteSelection_SiteFilterByNameNoMatch(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"site_name":"","data":[]}`)
	}))
	defer ts.Close()
	client := &Client{baseURL: ts.URL, httpClient: ts.Client(), logger: testLogger(t)}

	cfg := &Config{SiteFilter: []string{"NO-SUCH-SITE"}, SiteNameRateLimit: 1000}
	if _, _, err := resolveSiteSelection(cfg, testGroups(), client, testLogger(t)); err == nil {
		t.Fatal("expected error for a name that matches no discovered site")
	}
}

func TestSiteDisplayName_UnsitedSkipsLookup(t *testing.T) {
	if got := siteDisplayName(nil, "", nil); got != unsitedSegment {
		t.Errorf("siteDisplayName(_, \"\", _) = %q, want %q", got, unsitedSegment)
	}
}

func TestSiteDisplayName_NilClientFallsBackToID(t *testing.T) {
	if got := siteDisplayName(nil, "100", nil); got != "100" {
		t.Errorf("siteDisplayName(nil, \"100\", _) = %q, want %q", got, "100")
	}
}

func TestSiteDisplayName_ResolvesRealName(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"site_name":"MXP","data":[]}`)
	}))
	defer ts.Close()

	client := &Client{baseURL: ts.URL, httpClient: ts.Client(), logger: testLogger(t)}
	if got := siteDisplayName(client, "123456789", nil); got != "MXP" {
		t.Errorf("siteDisplayName = %q, want %q", got, "MXP")
	}
}

func TestSiteDisplayName_FallsBackOnError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer ts.Close()

	client := &Client{baseURL: ts.URL, httpClient: ts.Client(), logger: testLogger(t)}
	if got := siteDisplayName(client, "100", testLogger(t)); got != "100" {
		t.Errorf("siteDisplayName = %q, want fallback to site-id %q", got, "100")
	}
}

func TestSiteDisplayName_FallsBackOnEmptyName(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"site_name":"","data":[]}`)
	}))
	defer ts.Close()

	client := &Client{baseURL: ts.URL, httpClient: ts.Client(), logger: testLogger(t)}
	if got := siteDisplayName(client, "100", nil); got != "100" {
		t.Errorf("siteDisplayName = %q, want fallback to site-id %q on empty name", got, "100")
	}
}

func TestParseSiteSelection(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		count   int
		want    []int
		wantErr bool
	}{
		{"single", "2", 5, []int{1}, false},
		{"multiple", "1,3,5", 5, []int{0, 2, 4}, false},
		{"range", "2-4", 5, []int{1, 2, 3}, false},
		{"combination", "1,3-4", 5, []int{0, 2, 3}, false},
		{"all", "all", 5, []int{0, 1, 2, 3, 4}, false},
		{"dedup", "1,1,2", 5, []int{0, 1}, false},
		{"out of range", "6", 5, nil, true},
		{"reversed range", "4-2", 5, nil, true},
		{"garbage", "abc", 5, nil, true},
		{"empty", "", 5, nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseSiteSelection(c.input, c.count)
			if c.wantErr {
				if err == nil {
					t.Fatalf("parseSiteSelection(%q) expected error, got %v", c.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseSiteSelection(%q) failed: %v", c.input, err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("parseSiteSelection(%q) = %v, want %v", c.input, got, c.want)
			}
		})
	}
}
