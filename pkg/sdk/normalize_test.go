// normalize_test.go - Tests for value normalization [RFC 3339] (timestamps, MAC addresses, IP addresses, free-text tokens).
//
// For an introduction to OSIRIS JSON Producer Development Guidelines see:
// "[OSIRIS-PRODUCER-GUIDELINES]."
//
// [OSIRIS-PRODUCER-GUIDELINES]: https://osirisjson.org/en/docs/developers/producers/welcome
// [RFC 3339]: https://datatracker.ietf.org/doc/html/rfc3339

package sdk

import (
	"testing"
	"time"
)

func TestNormalizeRFC3339UTC(t *testing.T) {
	tests := []struct {
		input time.Time
		want  string
	}{
		{time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC), "2026-01-15T10:00:00Z"},
		{time.Date(2026, 2, 14, 11, 30, 0, 0, time.FixedZone("CET", 3600)), "2026-02-14T10:30:00Z"},
	}
	for _, tt := range tests {
		got := NormalizeRFC3339UTC(tt.input)
		if got != tt.want {
			t.Errorf("NormalizeRFC3339UTC(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestNormalizeToken(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"  West Europe  ", "west-europe"},
		{"UPPER CASE", "upper-case"},
		{"  multi   spaces  ", "multi-spaces"},
		{"already-clean", "already-clean"},
		{"  -trimmed-  ", "trimmed"},
		{"", ""},
	}
	for _, tt := range tests {
		got := NormalizeToken(tt.input)
		if got != tt.want {
			t.Errorf("NormalizeToken(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestNormalizeMAC(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"AA:BB:CC:DD:EE:FF", "aa:bb:cc:dd:ee:ff"},
		{"aa:bb:cc:dd:ee:ff", "aa:bb:cc:dd:ee:ff"},
		{"AA-BB-CC-DD-EE-FF", "aa:bb:cc:dd:ee:ff"},
		{"aabb.ccdd.eeff", "aa:bb:cc:dd:ee:ff"},
		{"invalid", ""},
		{"", ""},
		{"12:34:56:78", ""},
	}
	for _, tt := range tests {
		got := NormalizeMAC(tt.input)
		if got != tt.want {
			t.Errorf("NormalizeMAC(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestNormalizeIP(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"192.168.1.1", "192.168.1.1"},
		{"  10.0.0.1  ", "10.0.0.1"},
		{"::1", "::1"},
		{"2001:0db8:0000:0000:0000:0000:0000:0001", "2001:db8::1"},
		{"invalid", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := NormalizeIP(tt.input)
		if got != tt.want {
			t.Errorf("NormalizeIP(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
