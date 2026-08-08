// config_test.go - Tests for the per-site OutputPath convention.
//
// OSIRIS JSON Producer for Cisco SD-WAN Manager (vManage) introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package vmanage

import "testing"

func TestOutputPath(t *testing.T) {
	got := OutputPath("/out", "2026-08-08T10-00-00Z", "MXP")
	want := "/out/MXP/cisco-vmanage-2026-08-08T10-00-00Z-MXP.json"
	if got != want {
		t.Errorf("OutputPath = %q, want %q", got, want)
	}
}

func TestOutputPath_UnsitedFallback(t *testing.T) {
	got := OutputPath("/out", "2026-08-08T10-00-00Z", "")
	want := "/out/unclaimed/cisco-vmanage-2026-08-08T10-00-00Z-unclaimed.json"
	if got != want {
		t.Errorf("OutputPath = %q, want %q", got, want)
	}
}

func TestOutputPath_SanitizesUnsafeCharacters(t *testing.T) {
	got := OutputPath("/out", "2026-08-08T10-00-00Z", "site/with:bad*chars")
	want := "/out/site-with-bad-chars/cisco-vmanage-2026-08-08T10-00-00Z-site-with-bad-chars.json"
	if got != want {
		t.Errorf("OutputPath = %q, want %q", got, want)
	}
}

func TestOutputPath_ReusesExistingSiteNameDirectory(t *testing.T) {
	// Same site name, two different runs (e.g. this producer re-run, or
	// another producer's output sharing --output) - both resolve under
	// the same directory segment, only the timestamp in the filename
	// differs, so nothing is overwritten.
	first := OutputPath("/out", "2026-08-08T10-00-00Z", "MXP")
	second := OutputPath("/out", "2026-08-04T11-30-00Z", "MXP")
	if first == second {
		t.Fatalf("expected different timestamps to produce different filenames, got the same path twice: %q", first)
	}
	wantDir := "/out/MXP"
	for _, got := range []string{first, second} {
		if dir := got[:len(wantDir)]; dir != wantDir {
			t.Errorf("OutputPath = %q, want it to start with %q", got, wantDir)
		}
	}
}
