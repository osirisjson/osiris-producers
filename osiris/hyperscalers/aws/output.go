// output.go - Output file naming and publication for the OSIRIS JSON
// Amazon AWS producer.
//
// Each (account, region) document is written once, privately and
// atomically:
//
//   - [outputFileName] builds amazon-aws-<timestamp>-<region>-<name>.json
//     and rejects a region or name that could leave the output
//     directory.
//   - [writeDocument] writes to an exclusive temporary file (mode
//     0600) in the destination directory and publishes it with a hard
//     link, which never follows a symlink at the destination and fails
//     if the file already exists. An existing file is never
//     overwritten; the target fails with [errOutputExists].
//
// For an introduction to OSIRIS JSON Producer for Amazon AWS see:
// [OSIRIS-JSON-AWS]: https://docs.osirisjson.org/osiris-producers/hyperscalers/amazon-aws/

package aws

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// errOutputExists reports that the destination file already exists.
var errOutputExists = errors.New("output file already exists")

// outputFileName returns the document file name for a target. region
// must be a valid region code and name must already be sanitized
// (see [targetName]).
func outputFileName(timestamp, region, name string) (string, error) {
	if !validRegion(region) {
		return "", fmt.Errorf("invalid region %q", region)
	}
	file := fmt.Sprintf("amazon-aws-%s-%s-%s.json", timestamp, region, name)
	if file != filepath.Base(file) || strings.ContainsAny(file, `/\`) || strings.Contains(file, "..") {
		return "", fmt.Errorf("unsafe output file name %q", file)
	}
	return file, nil
}

// writeDocument publishes data at path. The parent directory is
// created with mode 0750 when missing. The file is created with mode
// 0600 and appears only when complete.
func writeDocument(path string, data []byte) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("creating directory %s: %w", dir, err)
	}
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("%w: %s", errOutputExists, path)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("checking %s: %w", path, err)
	}

	tmp, err := os.CreateTemp(dir, ".amazon-aws-*.tmp")
	if err != nil {
		return fmt.Errorf("creating temporary file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("writing %s: %w", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("syncing %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", tmpName, err)
	}

	if err := os.Link(tmpName, path); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("%w: %s", errOutputExists, path)
		}
		// File systems without hard links: re-check, then rename.
		if _, lerr := os.Lstat(path); lerr == nil {
			return fmt.Errorf("%w: %s", errOutputExists, path)
		}
		if rerr := os.Rename(tmpName, path); rerr != nil {
			return fmt.Errorf("publishing %s: %w", path, rerr)
		}
	}
	return nil
}
