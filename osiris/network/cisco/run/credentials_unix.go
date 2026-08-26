//go:build unix

// credentials_unix.go - Unix-specific half of LoadCredentialFile's
// safety checks (see credentials.go). Unix file modes expose real
// group/other permission bits and a real owning uid, so both are
// enforced here; credentials_other.go is the no-op counterpart for
// platforms (Windows) where os.FileInfo's permission bits don't carry
// this meaning.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package run

import (
	"fmt"
	"os"
	"syscall"
)

// checkCredentialFileOwnerAndPerms rejects a credentials file that
// grants any access to group or other (anything looser than 0600/0400),
// and rejects one not owned by the user running this process. The root
// user (uid 0) is exempt from the ownership check.
func checkCredentialFileOwnerAndPerms(path string, info os.FileInfo) error {
	if perm := info.Mode().Perm(); perm&0077 != 0 {
		return fmt.Errorf("credentials file %q: permissions %04o are too open, remove group/other access (e.g. chmod 0600 %s)", path, perm, path)
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	if uid := os.Getuid(); uid != 0 && int(stat.Uid) != uid {
		return fmt.Errorf("credentials file %q: not owned by the current user", path)
	}
	return nil
}
