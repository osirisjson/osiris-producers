// config_test.go - Tests for target configuration, host:port parsing,
// address resolution and hierarchical output path generation.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package run

import (
	"testing"
	"time"
)

func TestFormatTimestamp(t *testing.T) {
	got := FormatTimestamp(time.Date(2026, time.August, 16, 12, 54, 22, 0, time.UTC))
	want := "2026-08-16T12-54-22Z"
	if got != want {
		t.Errorf("FormatTimestamp = %q, want %q", got, want)
	}
}

func TestParseHostPort(t *testing.T) {
	tests := []struct {
		input    string
		wantHost string
		wantPort int
		wantErr  bool
	}{
		// Plain hostname.
		{"switch-01", "switch-01", 0, false},
		// FQDN.
		{"apic.lab.local", "apic.lab.local", 0, false},
		// IPv4 no port.
		{"192.0.2.1", "192.0.2.1", 0, false},
		// IPv4 with port.
		{"192.0.2.1:443", "192.0.2.1", 443, false},
		// IPv6 bare brackets.
		{"[::1]", "::1", 0, false},
		// IPv6 with port.
		{"[::1]:8443", "::1", 8443, false},
		// Host with high port.
		{"host:65535", "host", 65535, false},
		// Empty.
		{"", "", 0, true},
		// Port out of range.
		{"host:0", "", 0, true},
		{"host:99999", "", 0, true},
		// Non-numeric port.
		{"host:abc", "", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			host, port, err := ParseHostPort(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseHostPort(%q) error = %v, wantErr = %v", tt.input, err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if host != tt.wantHost {
				t.Errorf("ParseHostPort(%q) host = %q, want %q", tt.input, host, tt.wantHost)
			}
			if port != tt.wantPort {
				t.Errorf("ParseHostPort(%q) port = %d, want %d", tt.input, port, tt.wantPort)
			}
		})
	}
}

func TestResolveAddr(t *testing.T) {
	tests := []struct {
		name        string
		target      TargetConfig
		defaultPort int
		want        string
	}{
		{
			name:        "ipv4 default port",
			target:      TargetConfig{Host: "192.0.2.1"},
			defaultPort: 443,
			want:        "192.0.2.1:443",
		},
		{
			name:        "ipv4 explicit port",
			target:      TargetConfig{Host: "192.0.2.1", Port: 8443},
			defaultPort: 443,
			want:        "192.0.2.1:8443",
		},
		{
			name:        "ipv6 default port",
			target:      TargetConfig{Host: "::1"},
			defaultPort: 443,
			want:        "[::1]:443",
		},
		{
			name:        "hostname default port",
			target:      TargetConfig{Host: "apic.lab"},
			defaultPort: 443,
			want:        "apic.lab:443",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveAddr(tt.target, tt.defaultPort)
			if got != tt.want {
				t.Errorf("ResolveAddr() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSanitizePathSegment(t *testing.T) {
	tests := []struct {
		seg     string
		wantErr bool
	}{
		{"MXP", false},
		{"spine-01", false},
		{"", true},
		{".", true},
		{"..", true},
		{"../escape", true},
		{"a/b", true},
		{"a\\b", true},
		{"/etc/passwd", true},
		{"name\x00withnull", true},
		{"name\nwithnewline", true},
	}
	for _, tt := range tests {
		t.Run(tt.seg, func(t *testing.T) {
			_, err := SanitizePathSegment(tt.seg)
			if (err != nil) != tt.wantErr {
				t.Errorf("SanitizePathSegment(%q) error = %v, wantErr = %v", tt.seg, err, tt.wantErr)
			}
		})
	}
}

func TestOutputPath_RejectsTraversal(t *testing.T) {
	tests := []TargetConfig{
		{Host: "192.0.2.1", Hostname: "../../etc/passwd"},
		{Host: "192.0.2.2", Hostname: "ok", Datacenter: "../escape"},
		{Host: "192.0.2.3", Hostname: "ok", Rack: "a/b"},
	}
	for _, tgt := range tests {
		if _, err := OutputPath("/output", "2026-01-15T10-00-00Z", tgt); err == nil {
			t.Errorf("OutputPath(%+v) expected error, got none", tgt)
		}
	}
}

func TestOutputPath(t *testing.T) {
	const ts = "2026-01-15T10-00-00Z"
	tests := []struct {
		name   string
		target TargetConfig
		want   string
	}{
		{
			name: "full hierarchy",
			target: TargetConfig{
				Host: "192.0.2.1", Hostname: "apic-01", Type: "apic",
				Datacenter: "MXP", Floor: "F3", Room: "R301", Rack: "RACK-A",
			},
			want: "/output/MXP/F3/R301/RACK-A/cisco-apic-" + ts + "-apic-01.json",
		},
		{
			name: "partial hierarchy (datacenter and rack only)",
			target: TargetConfig{
				Host: "192.0.2.2", Hostname: "spine-01", Type: "nxos",
				Datacenter: "MXP", Rack: "RACK-B",
			},
			want: "/output/MXP/RACK-B/cisco-nxos-" + ts + "-spine-01.json",
		},
		{
			name: "no hierarchy (flat)",
			target: TargetConfig{
				Host: "192.0.2.3", Hostname: "leaf-01", Type: "nxos",
			},
			want: "/output/cisco-nxos-" + ts + "-leaf-01.json",
		},
		{
			name: "hostname defaults to Host",
			target: TargetConfig{
				Host: "192.0.2.4", Datacenter: "MXP", Type: "iosxe",
			},
			want: "/output/MXP/cisco-iosxe-" + ts + "-192.0.2.4.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := OutputPath("/output", ts, tt.target)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("OutputPath() = %q, want %q", got, tt.want)
			}
		})
	}
}
