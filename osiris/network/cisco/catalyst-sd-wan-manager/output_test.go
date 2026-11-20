// output_test.go - Tests for the private, contained and atomic document
// publication of the Cisco Catalyst SD-WAN Manager OSIRIS JSON
// Producer and for the per-run output path component policy.
//
// OSIRIS JSON Producer for Cisco Catalyst SD-WAN Manager introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification
package catalystsdwanmanager

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSanitizeFilenameSegment(t *testing.T) {
	tests := []struct{ in, want string }{
		{"BRANCH-00", "BRANCH-00"},
		{"..", ""},
		{".", ""},
		{"../../etc", "-..-etc"},
		{".hidden", "hidden"},
		{"trailing.", "trailing"},
		{"a/b", "a-b"},
		{"a\\b", "a-b"},
		{"tab\there", "tab-here"},
		{"bell\x07", "bell-"},
		{"line\nbreak", "line-break"},
		{"esc\x1b[31m", "esc-[31m"},
		{"OFFICE 00", "OFFICE-00"},
	}
	for _, tt := range tests {
		if got := sanitizeFilenameSegment(tt.in); got != tt.want {
			t.Errorf("sanitizeFilenameSegment(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestOutputPath_DotSegmentsStayInsideRoot(t *testing.T) {
	for _, name := range []string{"..", ".", "../..", "/", "\\.."} {
		p := OutputPath("/out", "2026-08-08T10-00-00Z", name)
		rel, err := filepath.Rel("/out", p)
		if err != nil || strings.HasPrefix(rel, "..") || strings.Count(rel, string(filepath.Separator)) != 1 {
			t.Errorf("OutputPath(%q) = %q escapes or nests under /out", name, p)
		}
	}
}

func TestSiteSegments_DistinctPerSite(t *testing.T) {
	var s siteSegments
	a := s.segment("100", "a/b")
	b := s.segment("200", "a:b")
	c := s.segment("300", "A-B")
	again := s.segment("100", "a/b")
	if a != "a-b" {
		t.Errorf("first site segment = %q, want a-b", a)
	}
	if b == a || c == a || c == b {
		t.Errorf("colliding labels share a segment: %q %q %q", a, b, c)
	}
	if b != "a-b-200" || c != "A-B-300" {
		t.Errorf("segments = %q, %q, want a-b-200, A-B-300", b, c)
	}
	if again != a {
		t.Errorf("same site re-asked = %q, want %q", again, a)
	}
	if u := s.segment("", ""); u != unsitedSegment {
		t.Errorf("unsited segment = %q, want %q", u, unsitedSegment)
	}
	if n := s.segment("400", unsitedSegment); n == unsitedSegment {
		t.Errorf("a site named %q must not share the unsited directory", unsitedSegment)
	}
}

func TestWriteDocument_NeverOverwrites(t *testing.T) {
	root := t.TempDir()
	path := OutputPath(root, "2026-08-08T10-00-00Z", "BRANCH-00")
	if err := writeDocument(root, path, []byte(`{"first":true}`)); err != nil {
		t.Fatal(err)
	}
	if err := writeDocument(root, path, []byte(`{"second":true}`)); !errors.Is(err, errOutputExists) {
		t.Fatalf("second write err = %v, want errOutputExists", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != `{"first":true}` {
		t.Errorf("existing file changed to %s", data)
	}
	assertNoTempFiles(t, filepath.Dir(path))
}

func TestWriteDocument_RejectsEscapeAndSymlinkedSiteDir(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{filepath.Join(root, "..", "x.json"), root, filepath.Join(root, "..")} {
		if err := writeDocument(root, p, []byte(`{}`)); err == nil {
			t.Errorf("writeDocument(%q) succeeded outside the root", p)
		}
	}

	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	elsewhere := t.TempDir()
	if err := os.Symlink(elsewhere, filepath.Join(root, "BRANCH-00")); err != nil {
		t.Fatal(err)
	}
	path := OutputPath(root, "2026-08-08T10-00-00Z", "BRANCH-00")
	if err := writeDocument(root, path, []byte(`{}`)); err == nil {
		t.Error("writeDocument followed a symlinked site directory")
	}
	if entries, _ := os.ReadDir(elsewhere); len(entries) != 0 {
		t.Errorf("symlink target received %d entries", len(entries))
	}
}

func TestWriteDocument_FailedWriteLeavesNothing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix modes")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "BRANCH-00")
	if err := os.Mkdir(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o700)
	path := OutputPath(root, "2026-08-08T10-00-00Z", "BRANCH-00")
	if err := writeDocument(root, path, []byte(`{}`)); err == nil {
		t.Fatal("expected a write into a read-only directory to fail")
	}
	os.Chmod(dir, 0o700)
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("failed write left %d entries", len(entries))
	}
}

func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".osiris-") {
			t.Errorf("temporary file %s left behind", e.Name())
		}
	}
}
