// config.go - Target and run configuration
// for Cisco OSIRIS JSON Producer.
// Defines the shared data structures that all Cisco sub-producers
// (APIC, NX-OS, IOS-XE) use for connection targets and runtime settings.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package run

import (
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Run modes. ParseFlags sets RunConfig.Mode explicitly based on which
// flag (-h/--host vs. -s/--source) selected it, rather than IsBatch
// inferring batch from len(Targets) > 1 a one-row CSV is still batch
// input and must honor its requested --output directory, which
// inference alone would get wrong.
const (
	ModeSingle = "single"
	ModeBatch  = "batch"
)

// TargetConfig describes a single device to collect from. Type is
// always set from the CLI subcommand that parsed this target (e.g.
// "nxos" for "cisco nxos -s ..."), never read from the batch CSV a
// batch file is inherently single-producer, since which producer's
// flags/dispatch parsed it already answers that question.
type TargetConfig struct {
	// Connection.
	Host     string // IP or FQDN (from the CSV's management_ip column, or --host).
	Port     int    // 0 = use producer default.
	Username string
	Password string

	// Identity.
	Hostname string // device label (from CSV or derived from Host).
	Type     string // producer type: "apic", "nxos", "iosxe".

	// Location hierarchy (batch CSV only, used for output path).
	// Optional per OSIRIS-JSON-v1.0 section 7.6.5's physical
	// containment levels; used to build the output directory structure
	// when present, and reserved for a future
	// physical.datacenter/room/rack group mapping (not yet implemented).
	Datacenter string // datacenter name.
	Floor      string // floor identifier.
	Room       string // room identifier.
	Rack       string // rack identifier.
}

// RunConfig carries runtime settings resolved from flags and CSV.
type RunConfig struct {
	Targets         []TargetConfig
	Mode            string // ModeSingle or ModeBatch; set explicitly by ParseFlags.
	OutputDir       string // batch only; empty = stdout single mode.
	DetailLevel     string // "minimal" | "detailed".
	SafeFailureMode string // "fail-closed" | "log-and-redact" | "off".
	InsecureTLS     bool   // --insecure: skip TLS verify.
	Timestamp       string // filesystem-safe UTC timestamp for output filenames.
}

// FormatTimestamp returns a filesystem-safe UTC timestamp string.
func FormatTimestamp(t time.Time) string {
	return t.UTC().Format("2026-01-02T15-04-05Z")
}

// IsBatch returns true when the run was explicitly started in batch
// mode (-s/--source), regardless of how many targets that CSV
// contained. A RunConfig built without going through ParseFlags (e.g.
// directly in a test) with Mode left unset falls back to the target
// count for backward compatibility.
func (c *RunConfig) IsBatch() bool {
	if c.Mode != "" {
		return c.Mode == ModeBatch
	}
	return len(c.Targets) > 1
}

// SanitizePathSegment validates a single path component (a CSV
// datacenter/floor/room/rack/hostname field, or a --host value used as
// a filename) before it is used to build an output file path. Rejects
// anything that could escape the intended output directory or corrupt
// the path: embedded path separators (so a single CSV field can never
// inject extra directory levels or an absolute path), the special "."
// and ".." segments, and control characters. Returns the segment
// unchanged when it is safe to use as-is.
func SanitizePathSegment(seg string) (string, error) {
	if seg == "" {
		return "", fmt.Errorf("empty path segment")
	}
	if seg == "." || seg == ".." {
		return "", fmt.Errorf("invalid path segment %q", seg)
	}
	if strings.ContainsAny(seg, "/\\") {
		return "", fmt.Errorf("path segment %q must not contain a path separator", seg)
	}
	for _, r := range seg {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("path segment %q contains a control character", seg)
		}
	}
	return seg, nil
}

// OutputPath returns the hierarchical output path for a target within
// the output directory:
// Datacenter/Floor/Room/Rack/cisco-<type>-<timestamp>-<hostname>.json.
// Empty location segments are omitted; if all are empty, returns just
// the filename. The filename matches single mode's own
// cisco-<type>-<timestamp>-<hostname>.json convention (see cisco.go's
// runSingle) rather than a bare Hostname.json, so a repeated batch run
// against the same targets does not silently overwrite the previous
// run's output, and a file can be identified by producer type and
// capture time without needing its directory context. Every non-empty
// segment is validated by SanitizePathSegment first, and the final
// path is confirmed to still resolve inside baseDir, so a hostile CSV
// field (absolute path, "../.." traversal, embedded separator) cannot
// write outside the requested output directory.
func OutputPath(baseDir, timestamp string, t TargetConfig) (string, error) {
	parts := []string{baseDir}
	for _, seg := range []string{t.Datacenter, t.Floor, t.Room, t.Rack} {
		if seg == "" {
			continue
		}
		clean, err := SanitizePathSegment(seg)
		if err != nil {
			return "", fmt.Errorf("output path: %w", err)
		}
		parts = append(parts, clean)
	}

	name := t.Hostname
	if name == "" {
		name = t.Host
	}
	clean, err := SanitizePathSegment(name)
	if err != nil {
		return "", fmt.Errorf("output path: %w", err)
	}
	parts = append(parts, fmt.Sprintf("cisco-%s-%s-%s.json", t.Type, timestamp, clean))

	full := filepath.Join(parts...)

	rel, err := filepath.Rel(filepath.Clean(baseDir), full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("output path for %q escapes the output directory", name)
	}

	return full, nil
}

// ParseHostPort splits a host string into host and port components.
// Accepted formats: "host", "host:port", "[ipv6]:port".
// Returns port=0 if no port is specified.
func ParseHostPort(addr string) (host string, port int, err error) {
	if addr == "" {
		return "", 0, fmt.Errorf("empty address")
	}

	// Try net.SplitHostPort first (handles host:port and [ipv6]:port).
	h, p, splitErr := net.SplitHostPort(addr)
	if splitErr == nil {
		pn, err := strconv.Atoi(p)
		if err != nil || pn < 1 || pn > 65535 {
			return "", 0, fmt.Errorf("invalid port %q in %q", p, addr)
		}
		return h, pn, nil
	}

	// No port component - treat entire string as host.
	// Strip brackets from bare IPv6 addresses like "[::1]".
	if strings.HasPrefix(addr, "[") && strings.HasSuffix(addr, "]") {
		addr = addr[1 : len(addr)-1]
	}
	return addr, 0, nil
}

// ResolveAddr returns a host:port string using the given default port.
// when the target has no explicit port.
func ResolveAddr(t TargetConfig, defaultPort int) string {
	p := t.Port
	if p == 0 {
		p = defaultPort
	}
	// Use bracket notation for IPv6 addresses.
	if strings.Contains(t.Host, ":") {
		return fmt.Sprintf("[%s]:%d", t.Host, p)
	}
	return fmt.Sprintf("%s:%d", t.Host, p)
}
