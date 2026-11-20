//go:build unix

// output_unix_test.go - Unix file mode tests for the Cisco Catalyst
// SD-WAN Manager OSIRIS JSON Producer document. Created directories are
// 0700 and documents 0600 under both a permissive (022) and a
// restrictive (077) umask.
//
// OSIRIS JSON Producer for Cisco Catalyst SD-WAN Manager introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification
package catalystsdwanmanager

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestWriteDocument_PrivateAndAtomic(t *testing.T) {
	for _, mask := range []int{0o022, 0o077} {
		t.Run(fmt.Sprintf("umask-%03o", mask), func(t *testing.T) {
			old := syscall.Umask(mask)
			defer syscall.Umask(old)
			root := filepath.Join(t.TempDir(), "out")
			path := OutputPath(root, "2026-08-08T10-00-00Z", "BRANCH-00")
			if err := writeDocument(root, path, []byte(`{"a":1}`)); err != nil {
				t.Fatalf("writeDocument: %v", err)
			}
			for _, d := range []string{root, filepath.Dir(path)} {
				fi, err := os.Stat(d)
				if err != nil {
					t.Fatal(err)
				}
				if fi.Mode().Perm() != outputDirMode {
					t.Errorf("umask %03o: %s mode = %v, want %v", mask, d, fi.Mode().Perm(), outputDirMode)
				}
			}
			fi, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if fi.Mode().Perm() != outputFileMode {
				t.Errorf("umask %03o: file mode = %v, want %v", mask, fi.Mode().Perm(), outputFileMode)
			}
			assertNoTempFiles(t, filepath.Dir(path))
		})
	}
}
