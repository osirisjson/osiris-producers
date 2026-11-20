// inventory.go - Tracked file inventory for the test data policy
// scanner.
//
// The authoritative inventory is the Git index, read with
// "git ls-files -z -s" through exec.CommandContext (no shell), with
// git resolved from fixed system directories rather than PATH. Files
// are read through an os.Root confined to the repository root, so a
// path can never resolve outside it, and symlinks are never followed.
// Any enumeration or read failure is returned as an error: the scanner
// never treats a failure as a pass.

package testdatapolicy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// Git index modes.
const (
	modeRegular    = "100644"
	modeExecutable = "100755"
	modeSymlink    = "120000"
	modeSubmodule  = "160000"
)

// An IndexEntry is one tracked path and its Git mode.
type IndexEntry struct {
	Mode string
	Path string
}

// A Source provides the tracked file inventory and file contents.
type Source interface {
	// Entries returns every tracked path, sorted and unique.
	Entries(ctx context.Context) ([]IndexEntry, error)
	// ReadFile returns the content of a tracked regular file.
	ReadFile(path string) ([]byte, error)
}

// A GitSource reads the inventory from a Git working tree.
type GitSource struct {
	dir  string
	root *os.Root

	// IncludeUntracked adds untracked files that are not ignored to
	// the inventory. It is a local pre-commit convenience; CI scans
	// the index only.
	IncludeUntracked bool
}

// OpenRepository locates the root of the Git working tree that
// contains dir and returns a [GitSource] for it. The caller must
// Close it.
func OpenRepository(ctx context.Context, dir string) (*GitSource, error) {
	out, err := runGit(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	top := strings.TrimSpace(string(out))
	if top == "" {
		return nil, errors.New("git rev-parse returned an empty repository root")
	}
	root, err := os.OpenRoot(top)
	if err != nil {
		return nil, fmt.Errorf("open repository root: %w", err)
	}
	return &GitSource{dir: top, root: root}, nil
}

// Dir returns the repository root directory.
func (g *GitSource) Dir() string { return g.dir }

// InventoryMode describes the inventory for the run summary.
func (g *GitSource) InventoryMode() string {
	if g.IncludeUntracked {
		return "tracked and untracked files (TDP_INCLUDE_UNTRACKED=1)"
	}
	return "tracked files (Git index)"
}

// Close releases the repository root handle.
func (g *GitSource) Close() error { return g.root.Close() }

// Entries implements [Source].
func (g *GitSource) Entries(ctx context.Context) ([]IndexEntry, error) {
	out, err := runGit(ctx, g.dir, "ls-files", "-z", "-s")
	if err != nil {
		return nil, err
	}
	if g.IncludeUntracked {
		extra, err := runGit(ctx, g.dir, "ls-files", "-z", "-o", "--exclude-standard")
		if err != nil {
			return nil, err
		}
		out = append(out, untrackedRecords(extra)...)
	}
	return parseIndex(out)
}

// untrackedRecords converts NUL-separated untracked paths into index
// records with a regular file mode, so they share the index parser.
func untrackedRecords(paths []byte) []byte {
	var b bytes.Buffer
	for p := range bytes.SplitSeq(paths, []byte{0}) {
		if len(p) == 0 {
			continue
		}
		b.WriteString(modeRegular + " untracked 0\t")
		b.Write(p)
		b.WriteByte(0)
	}
	return b.Bytes()
}

// ReadFile implements [Source]. It refuses anything that is not a
// regular file inside the repository root.
func (g *GitSource) ReadFile(path string) ([]byte, error) {
	fi, err := g.root.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("read %s: not a regular file (%s)", path, fi.Mode().Type())
	}
	data, err := g.root.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return data, nil
}

// trustedGitDirs are the only directories git is run from. They are
// fixed system install locations that ordinary users cannot write,
// so the scanner never depends on the caller's PATH: a writable PATH
// entry could otherwise put a different git in front of the real
// one. The same list is the PATH handed to git itself.
var trustedGitDirs = map[string][]string{
	"windows": {`C:\Program Files\Git\cmd`, `C:\Program Files\Git\bin`},
	"darwin":  {"/usr/bin", "/usr/local/bin", "/opt/homebrew/bin"},
	"":        {"/usr/bin", "/bin", "/usr/local/bin"},
}

// gitBinary returns the absolute path of the first git executable
// found in trustedGitDirs for the running OS. Outside Windows a
// candidate that is group or world writable is skipped.
func gitBinary() (path string, dirs []string, err error) {
	dirs, ok := trustedGitDirs[runtime.GOOS]
	if !ok {
		dirs = trustedGitDirs[""]
	}
	name := "git"
	if runtime.GOOS == "windows" {
		name = "git.exe"
	}
	for _, d := range dirs {
		p := filepath.Join(d, name)
		fi, err := os.Stat(p)
		if err != nil || !fi.Mode().IsRegular() {
			continue
		}
		if runtime.GOOS != "windows" && (fi.Mode().Perm()&0o111 == 0 || fi.Mode().Perm()&0o022 != 0) {
			continue
		}
		return p, dirs, nil
	}
	return "", nil, fmt.Errorf("git not found in %s", strings.Join(dirs, string(filepath.ListSeparator)))
}

// gitCommand returns a command running the trusted git with args,
// with PATH reduced to the trusted directories. The rest of the
// environment (GIT_INDEX_FILE, HOME) is kept.
func gitCommand(ctx context.Context, args ...string) (*exec.Cmd, error) {
	bin, dirs, err := gitBinary()
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = append(os.Environ(), "PATH="+strings.Join(dirs, string(filepath.ListSeparator)))
	return cmd, nil
}

func runGit(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd, err := gitCommand(ctx, append([]string{"-C", dir}, args...)...)
	if err != nil {
		return nil, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// parseIndex parses NUL-separated "git ls-files -s" records of the
// form "<mode> <object> <stage>\t<path>". Paths staged more than once
// (merge conflicts) are reported once.
func parseIndex(out []byte) ([]IndexEntry, error) {
	var entries []IndexEntry
	seen := map[string]bool{}
	for rec := range bytes.SplitSeq(out, []byte{0}) {
		if len(rec) == 0 {
			continue
		}
		meta, p, ok := strings.Cut(string(rec), "\t")
		if !ok {
			return nil, fmt.Errorf("malformed index record %q", truncate(string(rec)))
		}
		fields := strings.Fields(meta)
		if len(fields) != 3 || len(fields[0]) != 6 {
			return nil, fmt.Errorf("malformed index record %q", truncate(string(rec)))
		}
		if !fs.ValidPath(p) {
			return nil, fmt.Errorf("invalid tracked path %q", truncate(p))
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		entries = append(entries, IndexEntry{Mode: fields[0], Path: p})
	}
	if len(entries) == 0 {
		return nil, errors.New("the Git index lists no tracked files")
	}
	slices.SortFunc(entries, func(a, b IndexEntry) int { return strings.Compare(a.Path, b.Path) })
	return entries, nil
}

func truncate(s string) string {
	if len(s) > maxDisplay {
		return s[:maxDisplay] + "..."
	}
	return s
}
