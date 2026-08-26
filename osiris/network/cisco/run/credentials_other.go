//go:build !unix

// credentials_other.go - Non-Unix (Windows) half of
// LoadCredentialFile's safety checks (see credentials.go and its Unix
// counterpart, credentials_unix.go). os.FileInfo's permission bits and
// Sys() do not expose real ACL/ownership information on Windows, so
// there is nothing meaningful to enforce here beyond the
// platform-independent symlink and regular-file checks credentials.go
// already performs before calling this. Windows ACL enforcement is left
// to the filesystem and the user's own file permissions.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package run

import "os"

func checkCredentialFileOwnerAndPerms(path string, info os.FileInfo) error {
	return nil
}
