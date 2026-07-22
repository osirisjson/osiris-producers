// sites_select_test.go - Unit tests for interactive
// site selection parsing.
//
// OSIRIS JSON Producer for HPE Aruba Networking Central introduction:
// [OSIRIS-JSON-HPE-ARUBA-NETWORKING]: https://docs.osirisjson.org/osiris-producers/network/hpe-aruba-networking-central
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package arubacentral

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
)

// withStdin temporarily replaces os.Stdin with a pipe pre-loaded with
// input, restoring the original on cleanup. Site names are not secret,
// so the interactive picker (unlike the credential prompts in tty.go)
// reads from stdin.
func withStdin(t *testing.T, input string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating pipe: %v", err)
	}
	if _, err := w.WriteString(input); err != nil {
		t.Fatalf("writing test stdin: %v", err)
	}
	w.Close()

	original := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = original
		r.Close()
	})
}

func TestParseSiteSelection(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		count   int
		want    []int
		wantErr bool
	}{
		{name: "all keyword", input: "all", count: 3, want: []int{0, 1, 2}},
		{name: "ALL is case insensitive", input: "ALL", count: 2, want: []int{0, 1}},
		{name: "single index", input: "2", count: 3, want: []int{1}},
		{name: "comma list", input: "1,3", count: 3, want: []int{0, 2}},
		{name: "range", input: "1-3", count: 5, want: []int{0, 1, 2}},
		{name: "combination", input: "1,3-4", count: 5, want: []int{0, 2, 3}},
		{name: "dedupes overlapping selections", input: "1,1-2", count: 3, want: []int{0, 1}},
		{name: "out of range index errors", input: "9", count: 3, wantErr: true},
		{name: "zero index errors", input: "0", count: 3, wantErr: true},
		{name: "reversed range errors", input: "3-1", count: 3, wantErr: true},
		{name: "non-numeric errors", input: "abc", count: 3, wantErr: true},
		{name: "empty input errors", input: "", count: 3, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSiteSelection(tt.input, tt.count)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error for input %q, got indices %v", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for input %q: %v", tt.input, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("input %q: got %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestResolveSites_ExplicitFlagSkipsPrompting(t *testing.T) {
	got, err := resolveSites(&Config{}, "MXP, Branch-1 ,")
	if err != nil {
		t.Fatalf("resolveSites failed: %v", err)
	}
	want := []string{"MXP", "Branch-1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSelectSitesInteractive_PicksBySelection(t *testing.T) {
	withStdin(t, "2\n")
	sites := []Site{
		{ScopeName: "example-campus-1", DeviceCount: 10},
		{ScopeName: "example-branch-1", DeviceCount: 3},
	}
	got, err := selectSitesInteractive(sites)
	if err != nil {
		t.Fatalf("selectSitesInteractive failed: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"example-branch-1"}) {
		t.Errorf("got %v, want [example-branch-1]", got)
	}
}

func TestSelectSitesInteractive_BlankMeansAll(t *testing.T) {
	withStdin(t, "\n")
	sites := []Site{{ScopeName: "example-campus-1"}, {ScopeName: "example-branch-1"}}
	got, err := selectSitesInteractive(sites)
	if err != nil {
		t.Fatalf("selectSitesInteractive failed: %v", err)
	}
	want := []string{"example-campus-1", "example-branch-1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("expected every site name for a blank selection, got %v want %v", got, want)
	}
}

func TestSelectSitesFromAccount_NoSitesFallsBackToAll(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
	}))
	defer ts.Close()

	client := newTestClient(ts, Credentials{AccessToken: "test-access-token"})
	got, err := selectSitesFromAccount(client)
	if err != nil {
		t.Fatalf("expected no error when no sites are available, got %v", err)
	}
	if got != nil {
		t.Errorf("expected nil (no filter) when the account has no sites, got %v", got)
	}
}

func TestAllSitesFromAccount_ReturnsEveryNameWithoutPrompting(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{
			{"scopeName": "example-campus-1"},
			{"scopeName": "example-branch-1"},
		}})
	}))
	defer ts.Close()

	// No withStdin: --all must never read from stdin/prompt.
	client := newTestClient(ts, Credentials{AccessToken: "test-access-token"})
	got, err := allSitesFromAccount(client)
	if err != nil {
		t.Fatalf("allSitesFromAccount failed: %v", err)
	}
	want := []string{"example-campus-1", "example-branch-1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestAllSitesFromAccount_NoSitesFallsBackToUnfiltered(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
	}))
	defer ts.Close()

	client := newTestClient(ts, Credentials{AccessToken: "test-access-token"})
	got, err := allSitesFromAccount(client)
	if err != nil {
		t.Fatalf("expected no error when no sites are available, got %v", err)
	}
	if got != nil {
		t.Errorf("expected nil (no filter) when the account has no sites, got %v", got)
	}
}

func TestSelectSitesFromAccount_ListsAndSelects(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{
			{"scopeName": "example-campus-1"},
			{"scopeName": "example-branch-1"},
		}})
	}))
	defer ts.Close()

	withStdin(t, "1\n")
	client := newTestClient(ts, Credentials{AccessToken: "test-access-token"})
	got, err := selectSitesFromAccount(client)
	if err != nil {
		t.Fatalf("selectSitesFromAccount failed: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"example-campus-1"}) {
		t.Errorf("got %v, want [example-campus-1]", got)
	}
}
