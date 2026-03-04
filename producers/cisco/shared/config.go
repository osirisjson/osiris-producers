// config.go - Target and run configuration for Cisco producers.
// Defines the shared data structures that all Cisco sub-producers (APIC, NX-OS, IOS-XR)
// use for connection targets and runtime settings.
//
// For an introduction to OSIRIS JSON Producer for Cisco see:
// "[OSIRIS-JSON-CISCO]."
//
// [OSIRIS-JSON-CISCO]: https://osirisjson.org/en/docs/developers/producers/cisco/

package shared

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// Owner values for batch CSV. These are human-only metadata they do not affect the producer or the OSIRIS document.
const (
	OwnerSelf = "self" // your own device (default)
	OwnerISP  = "isp"  // ISP-managed device you have read access to
	OwnerColo = "colo" // colocation equipment
)

// ValidOwners is the set of accepted owner values in batch CSV.
var ValidOwners = []string{OwnerSelf, OwnerISP, OwnerColo}

// TargetConfig describes a single device to collect from.
type TargetConfig struct {
	// Connection.
	Host string // IP or FQDN
	Port int    // 0 = use producer default
	Username string
	Password string

	// Identity.
	Hostname string // device label (from CSV or derived from Host)
	Type string // producer type: "apic", "nxos", "iosxr"

	// Location hierarchy (batch CSV only, used for output path).
	DC string // datacenter name
	Floor string // floor identifier
	Room string // room identifier
	Zone string // zone/pod identifier

	// Human-only metadata (not used by producer or OSIRIS document).
	Owner string // "self", "isp", "colo"
	Notes string // free-text operator notes
}

// RunConfig carries runtime settings resolved from flags and CSV.
type RunConfig struct {
	Targets []TargetConfig
	OutputDir string // batch only; empty = stdout single mode
	DetailLevel string // "minimal" | "detailed"
	SafeFailureMode string // "fail-closed" | "log-and-redact" | "off"
	InsecureTLS bool   // --insecure: skip TLS verify
}

// IsBatch returns true when the run targets multiple devices.
func (c *RunConfig) IsBatch() bool {
	return len(c.Targets) > 1
}

// OutputPath returns the hierarchical output path for a target within the
// output directory: DC/Floor/Room/Zone/Hostname.json.
// Empty location segments are omitted. If all segments are empty, returns
// just Hostname.json.
func OutputPath(baseDir string, t TargetConfig) string {
	parts := []string{baseDir}
	for _, seg := range []string{t.DC, t.Floor, t.Room, t.Zone} {
		if seg != "" {
			parts = append(parts, seg)
		}
	}
	name := t.Hostname
	if name == "" {
		name = t.Host
	}
	parts = append(parts, name+".json")
	return strings.Join(parts, "/")
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

// ResolveAddr returns a host:port string using the given default port
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
