// normalize.go - Value normalization utilities for OSIRIS document properties.
// Provides deterministic formatting for timestamps [RFC3339 UTC], MAC addresses
// (colon-separated lowercase), IP addresses (canonical net.IP form) and free-text
// tokens (lowercase, collapsed whitespace).
//
// For an introduction to OSIRIS JSON Producer Development Guidelines see:
// "[OSIRIS-PRODUCER-GUIDELINES]."
//
// [RFC3339]: https://datatracker.ietf.org/doc/html/rfc3339
// [OSIRIS-PRODUCER-GUIDELINES]: https://osirisjson.org/en/docs/getting-started/osiris-producer-guidelines

package sdk

import (
	"net"
	"regexp"
	"strings"
	"time"
)

var whitespaceRe = regexp.MustCompile(`\s+`)

// NormalizeRFC3339UTC returns an RFC3339 string in UTC (e.g. "2026-01-15T10:00:00Z").
func NormalizeRFC3339UTC(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

// NormalizeToken returns a stable token form:
//   - trim surrounding whitespace
//   - lowercase
//   - collapse internal whitespace to single "-"
//   - remove leading/trailing "-"
func NormalizeToken(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ToLower(s)
	s = whitespaceRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}

// NormalizeMAC returns a lowercase colon-separated MAC address (e.g. "aa:bb:cc:dd:ee:ff") or "" if the input is invalid.
// Accepts colon, hyphen, or dot-separated formats.
func NormalizeMAC(s string) string {
	hw, err := net.ParseMAC(s)
	if err != nil {
		return ""
	}
	return hw.String()
}

// NormalizeIP returns the canonical string form of an IP address
// (IPv4 dotted quad or IPv6 compressed) or "" if the input is invalid.
func NormalizeIP(s string) string {
	ip := net.ParseIP(strings.TrimSpace(s))
	if ip == nil {
		return ""
	}
	return ip.String()
}
