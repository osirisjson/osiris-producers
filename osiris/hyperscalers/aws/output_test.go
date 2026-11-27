// output_test.go - Tests for output file naming and publication
// (output.go, OutputPath, sanitizeFilename) in the OSIRIS JSON Amazon
// AWS producer.
//
// For an introduction to OSIRIS JSON Producer for Amazon AWS see:
// [OSIRIS-JSON-AWS]: https://docs.osirisjson.org/osiris-producers/hyperscalers/amazon-aws/

package aws

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestOutputPath(t *testing.T) {
	got, err := OutputPath("out", "TEST-ACCOUNT", "2026-01-02T03-04-05Z", "eu-west-1")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("out", "amazon-aws-2026-01-02T03-04-05Z-eu-west-1-TEST-ACCOUNT.json"); got != want {
		t.Errorf("OutputPath = %q, want %q", got, want)
	}
	for _, region := range []string{"../eu-west-1", "eu-west-1/..", ""} {
		if _, err := OutputPath("out", "TEST", "2026-01-02T03-04-05Z", region); err == nil {
			t.Errorf("region %q accepted", region)
		}
	}
}

func TestSanitizeFilename(t *testing.T) {
	tests := map[string]string{
		"TEST-ACCOUNT":   "TEST-ACCOUNT",
		"../../etc":      "----etc",
		"a/b\\c":         "a-b-c",
		".hidden":        "hidden",
		"..":             "-",
		"":               "unknown",
		"TEST\nACCOUNT":  "TEST-ACCOUNT",
		"TEST ACCOUNT 1": "TEST ACCOUNT 1",
	}
	for in, want := range tests {
		if got := sanitizeFilename(in); got != want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWriteDocument(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "doc.json")
	if err := writeDocument(path, []byte(`{"test":true}`)); err != nil {
		t.Fatalf("writeDocument: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %o, want 600", perm)
	}

	// An existing file is never overwritten.
	err = writeDocument(path, []byte(`{"other":true}`))
	if !errors.Is(err, errOutputExists) {
		t.Fatalf("second write err = %v, want errOutputExists", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != `{"test":true}` {
		t.Errorf("file overwritten: %s", data)
	}

	// A symlink at the destination is not followed.
	victim := filepath.Join(dir, "victim.json")
	if err := os.WriteFile(victim, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(victim, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	if err := writeDocument(link, []byte("new")); !errors.Is(err, errOutputExists) {
		t.Fatalf("symlink write err = %v, want errOutputExists", err)
	}
	if data, _ := os.ReadFile(victim); string(data) != "keep" {
		t.Errorf("symlink target modified: %s", data)
	}

	// No temporary files are left behind.
	entries, _ := filepath.Glob(filepath.Join(dir, "nested", ".amazon-aws-*.tmp"))
	if len(entries) != 0 {
		t.Errorf("temporary files left: %v", entries)
	}
}
