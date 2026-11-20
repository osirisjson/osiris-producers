// output.go - Private, contained and atomic publication of the Cisco
// Catalyst SD-WAN Manager OSIRIS JSON Producer documents.
//
// writeDocument publishes one serialized document at a path built by
// OutputPath:
//
//   - containment: the path must resolve inside the output root, and
//     the site directory must not be a symbolic link, so a site label
//     can never steer a write outside --output;
//   - privacy: directories this producer creates are 0700 and the
//     document is 0600, independent of the process umask (an exported
//     topology is sensitive even without secrets);
//   - atomicity: the document is written to a temporary file in the
//     target directory, synced and closed, then published under its
//     final name, so a reader never sees a partial document and a
//     failed write leaves every existing file untouched;
//   - no silent overwrite: publication fails when the final name
//     already exists (for example two runs of the same site within one
//     second) instead of replacing it.
//
// OSIRIS JSON Producer for Cisco Catalyst SD-WAN Manager introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification
package catalystsdwanmanager

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	// outputDirMode is the mode of every directory this producer
	// creates.
	outputDirMode fs.FileMode = 0o700
	// outputFileMode is the mode of every published document.
	outputFileMode fs.FileMode = 0o600
)

// errOutputExists is returned when the final document path already
// exists; nothing is overwritten.
var errOutputExists = errors.New("output file already exists")

// writeDocument publishes data at path, which must lie strictly inside
// root (see the file comment for the guarantees).
func writeDocument(root, path string, data []byte) error {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("output path escapes the output directory")
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, outputDirMode); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}
	if dir != root {
		fi, err := os.Lstat(dir)
		if err != nil {
			return fmt.Errorf("checking output directory: %w", err)
		}
		if fi.Mode()&fs.ModeSymlink != 0 || !fi.IsDir() {
			return fmt.Errorf("output site directory is not a plain directory")
		}
	}
	if _, err := os.Lstat(path); err == nil {
		return errOutputExists
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("checking output file: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".osiris-*.tmp")
	if err != nil {
		return fmt.Errorf("creating temporary file: %w", err)
	}
	tmpName := tmp.Name()
	published := false
	defer func() {
		if !published {
			os.Remove(tmpName)
		}
	}()

	if err := tmp.Chmod(outputFileMode); err != nil {
		tmp.Close()
		return fmt.Errorf("setting file mode: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("writing temporary file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("syncing temporary file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temporary file: %w", err)
	}

	if err := publish(tmpName, path); err != nil {
		return err
	}
	published = true
	syncDir(dir)
	return nil
}

// publish moves the completed temporary file to its final name without
// replacing an existing file. A hard link fails atomically when the
// target exists where hard links are unsupported (some network and
// FAT filesystems) it falls back to a rename after a fresh existence
// check, which narrows but cannot fully close the race on such
// filesystems.
func publish(tmpName, path string) error {
	if err := os.Link(tmpName, path); err == nil {
		os.Remove(tmpName)
		return nil
	} else if errors.Is(err, fs.ErrExist) {
		return errOutputExists
	}
	if _, err := os.Lstat(path); err == nil {
		return errOutputExists
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("publishing output file: %w", err)
	}
	return nil
}

// syncDir flushes a directory entry change to disk where the platform
// supports it failures are ignored because the document itself is
// already synced and published.
func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		d.Sync()
		d.Close()
	}
}
