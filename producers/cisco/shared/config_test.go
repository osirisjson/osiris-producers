// config_test.go - Tests for target configuration, host:port parsing, address
// resolution and hierarchical output path generation.
//
// For an introduction to OSIRIS JSON Producer for Cisco see:
// "[OSIRIS-JSON-CISCO]."
//
// [OSIRIS-JSON-CISCO]: https://osirisjson.org/en/docs/developers/producers/cisco/

package shared

import (
	"testing"
)

func TestParseHostPort(t *testing.T) {
	tests := []struct {
		input string
		wantHost string
		wantPort int
		wantErr bool
	}{
		// Plain hostname.
		{"switch-01", "switch-01", 0, false},
		// FQDN.
		{"apic.lab.local", "apic.lab.local", 0, false},
		// IPv4 no port.
		{"10.0.0.1", "10.0.0.1", 0, false},
		// IPv4 with port.
		{"10.0.0.1:443", "10.0.0.1", 443, false},
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
		name string
		target TargetConfig
		defaultPort int
		want string
	}{
		{
			name: "ipv4 default port",
			target: TargetConfig{Host: "10.0.0.1"},
			defaultPort: 443,
			want: "10.0.0.1:443",
		},
		{
			name: "ipv4 explicit port",
			target: TargetConfig{Host: "10.0.0.1", Port: 8443},
			defaultPort: 443,
			want: "10.0.0.1:8443",
		},
		{
			name: "ipv6 default port",
			target: TargetConfig{Host: "::1"},
			defaultPort: 443,
			want: "[::1]:443",
		},
		{
			name: "hostname default port",
			target: TargetConfig{Host: "apic.lab"},
			defaultPort: 443,
			want: "apic.lab:443",
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

func TestRunConfigIsBatch(t *testing.T) {
	single := &RunConfig{Targets: []TargetConfig{{Host: "a"}}}
	if single.IsBatch() {
		t.Error("single target should not be batch")
	}

	batch := &RunConfig{Targets: []TargetConfig{{Host: "a"}, {Host: "b"}}}
	if !batch.IsBatch() {
		t.Error("multiple targets should be batch")
	}
}

func TestOutputPath(t *testing.T) {
	tests := []struct {
		name   string
		target TargetConfig
		want   string
	}{
		{
			name: "full hierarchy",
			target: TargetConfig{
				Host: "10.0.0.1", Hostname: "apic-01",
				DC: "AMS-01", Floor: "F3", Room: "R301", Zone: "POD-A",
			},
			want: "/output/AMS-01/F3/R301/POD-A/apic-01.json",
		},
		{
			name: "partial hierarchy (DC and zone only)",
			target: TargetConfig{
				Host: "10.0.0.2", Hostname: "spine-01",
				DC: "AMS-01", Zone: "POD-B",
			},
			want: "/output/AMS-01/POD-B/spine-01.json",
		},
		{
			name: "no hierarchy (flat)",
			target: TargetConfig{
				Host: "10.0.0.3", Hostname: "leaf-01",
			},
			want: "/output/leaf-01.json",
		},
		{
			name: "hostname defaults to Host",
			target: TargetConfig{
				Host: "10.0.0.4", DC: "DC1",
			},
			want: "/output/DC1/10.0.0.4.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := OutputPath("/output", tt.target)
			if got != tt.want {
				t.Errorf("OutputPath() = %q, want %q", got, tt.want)
			}
		})
	}
}
