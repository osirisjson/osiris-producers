// policy_test.go - Positive and negative tests for every address rule
// classifier (IPv4, IPv6, MAC) and every context rule evaluator
// (hostname, device, coordinate, site, ASN, serial, credential),
// including both boundaries of every numeric range.
//
// The package scans its own sources, so forbidden sample values are
// assembled at runtime with the helpers below rather than written as
// literals.

package testdatapolicy

import (
	"fmt"
	"strings"
	"testing"
)

// v4 assembles a dotted-quad IPv4 literal.
func v4(a, b, c, d int) string { return fmt.Sprintf("%d.%d.%d.%d", a, b, c, d) }

// v6 joins IPv6 groups with ':'; an empty group yields "::".
func v6(groups ...string) string { return strings.Join(groups, ":") }

// macOf assembles a colon-separated MAC literal.
func macOf(b ...byte) string {
	parts := make([]string, len(b))
	for i, x := range b {
		parts[i] = fmt.Sprintf("%02x", x)
	}
	return strings.Join(parts, ":")
}

func TestClassifyIPv4(t *testing.T) {
	tests := []struct {
		raw     string
		test    bool
		allowed bool
		class   string
	}{
		{"192.0.2.1", false, true, "documentation"},
		{"198.51.100.255", false, true, "documentation"},
		{"203.0.113.0/24", false, true, "documentation"},
		{"192.0.2.128/25", false, true, "documentation"},
		{v4(192, 0, 2, 0) + "/23", false, false, "public or reserved"},
		{v4(192, 0, 2, 0) + "/16", false, false, "public or reserved"},
		{v4(192, 0, 3, 1), false, false, "public or reserved"},
		{v4(10, 0, 0, 1), false, false, "private (RFC 1918)"},
		{v4(172, 16, 0, 1), false, false, "private (RFC 1918)"},
		{v4(192, 168, 1, 1), false, false, "private (RFC 1918)"},
		{v4(8, 8, 8, 8), false, false, "public or reserved"},
		{v4(169, 254, 0, 1), false, false, "link-local"},
		{v4(224, 0, 0, 5), false, false, "multicast"},
		{v4(100, 64, 0, 1), false, false, "public or reserved"},
		{"0.0.0.0", false, true, "unspecified"},
		{"0.0.0.0/0", false, true, "default route"},
		{"0.0.0.0/32", false, true, "unspecified"},
		{"255.255.255.0", false, true, "netmask"},
		{"255.255.255.255", false, true, "netmask"},
		{"255.0.0.0", false, true, "netmask"},
		{v4(254, 0, 0, 0), false, false, "public or reserved"},
		{v4(255, 0, 255, 0), false, false, "non-contiguous netmask"},
		{"0.0.0.255", false, true, "wildcard mask"},
		{"0.255.255.255", false, true, "wildcard mask"},
		{v4(0, 0, 0, 254), false, false, "public or reserved"},
		{v4(127, 0, 0, 1), true, true, "loopback"},
		{v4(127, 0, 0, 1), false, false, "loopback outside a _test.go file"},
		{v4(192, 0, 2, 0) + "/33", false, false, "malformed prefix"},
	}
	for _, tt := range tests {
		r, ok := classifyIPv4(tt.raw, tt.test)
		if !ok {
			t.Errorf("classifyIPv4(%q) did not parse", tt.raw)
			continue
		}
		if r.allowed != tt.allowed || r.class != tt.class {
			t.Errorf("classifyIPv4(%q, %v) = %v %q, want %v %q", tt.raw, tt.test, r.allowed, r.class, tt.allowed, tt.class)
		}
	}
	for _, raw := range []string{"999.1.1.1", "01.2.3.4", "1.2.3"} {
		if _, ok := classifyIPv4(raw, false); ok {
			t.Errorf("classifyIPv4(%q) parsed, want rejected", raw)
		}
	}
}

func TestClassifyIPv6(t *testing.T) {
	tests := []struct {
		raw     string
		test    bool
		allowed bool
		class   string
	}{
		{"2001:db8::1", false, true, "documentation"},
		{"2001:0db8:0000:0000:0000:0000:0000:0001", false, true, "documentation"},
		{"2001:DB8::/32", false, true, "documentation"},
		{"2001:db8:100::/64", false, true, "documentation"},
		{v6("2001", "db8", "", "") + "/31", false, false, "global or reserved"},
		{v6("2001", "db9", "", "1"), false, false, "global or reserved"},
		{v6("fe80", "", "1") + "/64", false, false, "link-local"},
		{v6("ff02", "", "1"), false, false, "multicast"},
		{v6("fd00", "", "1"), false, false, "unique local"},
		{v6("", "", "ffff", v4(10, 0, 0, 1)), false, false, "IPv4-mapped"},
		{"::", false, true, "unspecified"},
		{"::/0", false, true, "default route"},
		{"::/128", false, true, "unspecified"},
		{"::1", true, true, "loopback"},
		{"::1", false, false, "loopback outside a _test.go file"},
		{v6("2001", "db8", "", "") + "/129", false, false, "malformed prefix"},
	}
	for _, tt := range tests {
		r, ok := classifyIPv6(tt.raw, tt.test)
		if !ok {
			t.Errorf("classifyIPv6(%q) did not parse", tt.raw)
			continue
		}
		if r.allowed != tt.allowed || r.class != tt.class {
			t.Errorf("classifyIPv6(%q, %v) = %v %q, want %v %q", tt.raw, tt.test, r.allowed, r.class, tt.allowed, tt.class)
		}
	}
	if r, _ := classifyIPv6("2001:0DB8::0001", false); r.canonical != "2001:db8::1" {
		t.Errorf("canonical = %q, want 2001:db8::1", r.canonical)
	}
}

func TestClassifyMAC(t *testing.T) {
	tests := []struct {
		raw     string
		allowed bool
		class   string
	}{
		{"00:00:5E:00:53:00", true, "documentation unicast"},
		{"00-00-5e-00-53-ff", true, "documentation unicast"},
		{"0000.5e00.5301", true, "documentation unicast"},
		{"01:00:5E:90:10:00", true, "documentation multicast"},
		{"01:00:5e:90:10:ff", true, "documentation multicast"},
		{"AA:BB:CC:DD:00:01", true, "synthetic sequence"},
		{"02:00:00:00:00:01", true, "locally administered unicast"},
		{"06:12:34:56:78:9a", true, "locally administered unicast"},
		{"00:00:00:00:00:00", true, "null MAC"},
		{"FF:FF:FF:FF:FF:FF", true, "broadcast MAC"},
		{macOf(0x00, 0x00, 0x5e, 0x00, 0x54, 0x00), false, "globally administered OUI"},
		{macOf(0x01, 0x00, 0x5e, 0x90, 0x11, 0x00), false, "multicast outside the documentation range"},
		{macOf(0x03, 0x00, 0x00, 0x00, 0x00, 0x01), false, "multicast outside the documentation range"},
		{macOf(0x00, 0x1a, 0xa1, 0x00, 0x00, 0x01), false, "globally administered OUI"},
	}
	for _, tt := range tests {
		r, ok := classifyMAC(tt.raw)
		if !ok {
			t.Errorf("classifyMAC(%q) did not parse", tt.raw)
			continue
		}
		if r.allowed != tt.allowed || r.class != tt.class {
			t.Errorf("classifyMAC(%q) = %v %q, want %v %q", tt.raw, r.allowed, r.class, tt.allowed, tt.class)
		}
	}
	if _, ok := classifyMAC("00:00:5e:00:53"); ok {
		t.Error("classifyMAC accepted a 5-octet value")
	}
}

func TestCheckHostname(t *testing.T) {
	ok := []string{
		"example.com", "lab-rtr01.example.com", "a.b.example.net",
		"example.org", "docs.osirisjson.org", "labhubsa01.blob.core.windows.net",
		"testkv01.vault.azure.net", "example-sso.awsapps.com",
		"privatelink.blob.core.windows.net", "fakeapim.management.azure-api.net",
	}
	for _, h := range ok {
		if msg, pass := checkHostname(h); !pass {
			t.Errorf("checkHostname(%q) failed: %s", h, msg)
		}
	}
	bad := []string{
		"example.com.notexample.net", "notexample.com", "switch" + ".lab.local",
		"db" + ".internal", "myorg" + ".awsapps.com", "kv1" + ".vault.azure.net",
		"host" + ".test", "host" + ".invalid",
	}
	for _, h := range bad {
		if _, pass := checkHostname(h); pass {
			t.Errorf("checkHostname(%q) passed, want failure", h)
		}
	}
}

func TestHostOf(t *testing.T) {
	tests := []struct {
		in, want string
		ok       bool
	}{
		{"https://lab.example.com:8443/api", "lab.example.com", true},
		{"lab.example.com:443", "lab.example.com", true},
		{"LAB.Example.COM.", "lab.example.com", true},
		{"*.example.com", "example.com", true},
		{"192.0.2.1", "", false},
		{"[2001:db8::1]:443", "", false},
		{"localhost", "", false},
		{"v0.12.0", "", false},
		{"{{ .Host }}", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		got, ok := hostOf(tt.in)
		if got != tt.want || ok != tt.ok {
			t.Errorf("hostOf(%q) = %q %v, want %q %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func TestProjectReference(t *testing.T) {
	if !isProjectReference("https://github.com/osirisjson/osiris/issues") {
		t.Error("project repository URL not recognized")
	}
	for _, v := range []string{"https://github.com/someone/else", "github.com/osirisjson/x", "https://gitlab.com/osirisjson/x"} {
		if isProjectReference(v) {
			t.Errorf("isProjectReference(%q) = true", v)
		}
	}
}

func TestASNOK(t *testing.T) {
	tests := []struct {
		v  string
		ok bool
	}{
		{"64495", false}, {"64496", true}, {"64511", true},
		{"64512", true}, {"65534", true}, {"65535", false},
		{"65536", false}, {"65551", false},
		{"4199999999", false}, {"4200000000", true},
		{"4294967294", true}, {"4294967295", false},
		{"18446744073709551616", false}, {"0", false}, {"-1", false},
	}
	for _, tt := range tests {
		if got := asnOK(tt.v); got != tt.ok {
			t.Errorf("asnOK(%s) = %v, want %v", tt.v, got, tt.ok)
		}
	}
}

func TestCredentialOK(t *testing.T) {
	for _, v := range []string{"", "admin", "test", "password", "secret", "changeme", "secret-2", "admin-10", "[REDACTED]", "REDACTED", "{{ .Env.X_TOKEN }}", "${SONAR}", "Basic changeme", "Bearer secret-1"} {
		if !credentialOK(v) {
			t.Errorf("credentialOK(%q) = false", v)
		}
	}
	for _, v := range []string{"hunter" + "2", "secret-", "secret-a", "Secret", "test-" + "token", "admin1", "p", "Bearer " + "abc123"} {
		if credentialOK(v) {
			t.Errorf("credentialOK(%q) = true", v)
		}
	}
}

func TestNormalizeKey(t *testing.T) {
	tests := [][2]string{
		{"host_name", "hostname"}, {"hostName", "hostname"}, {"host-name", "hostname"},
		{"c.token", "token"}, {"ns:serial-number", "serialnumber"}, {"_serial", "serial"},
	}
	for _, tt := range tests {
		in, want := tt[0], tt[1]
		if got := normalizeKey(in); got != want {
			t.Errorf("normalizeKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCredentialKeys(t *testing.T) {
	for _, k := range []string{"password", "clientsecret", "accesstoken", "apikey", "authorization", "privatekey", "secretkey", "awsaccesskey"} {
		if !isCredentialKey(k) {
			t.Errorf("isCredentialKey(%q) = false", k)
		}
	}
	for _, k := range []string{"nexttoken", "boundarytoken", "secretname", "tokenurl", "username"} {
		if isCredentialKey(k) {
			t.Errorf("isCredentialKey(%q) = true", k)
		}
	}
}

func TestEvaluateKeyValue(t *testing.T) {
	q := func(key, value string) keyValue { return keyValue{key: key, value: value, col: 1, quoted: true} }
	tests := []struct {
		name string
		kv   keyValue
		rule string // "" means no finding
	}{
		{"device ok", q("hostname", "LAB-RTR01"), ""},
		{"device lowercase prefix", q("devicename", "test-sw01"), ""},
		{"device bad", q("devicename", "core"+"-sw01"), RuleDevice},
		{"device fqdn ok", q("hostname", "lab-rtr01.example.com"), ""},
		{"device fqdn bad", q("hostname", "rtr01"+".corp.net"), RuleDNS},
		{"device empty", q("hostname", ""), ""},
		{"device template", q("hostname", "{{ .Host }}"), ""},
		{"dns ok", q("url", "https://lab.example.com/x"), ""},
		{"dns bad", q("fqdn", "app"+".internal.net"), RuleDNS},
		{"dns ip skipped", q("host", "192.0.2.1"), ""},
		{"geo ok", keyValue{key: "latitude", value: "45.54334110", col: 1}, ""},
		{"geo lon ok", keyValue{key: "lng", value: "10.2241237", col: 1}, ""},
		{"geo bad", keyValue{key: "lat", value: "4" + "1.9", col: 1}, RuleGeo},
		{"geo lat value in lon field", keyValue{key: "longitude", value: "45.5433411", col: 1}, RuleGeo},
		{"geo non numeric", q("lat", "unknown"), ""},
		{"site ok", q("sitename", "BRANCH-00"), ""},
		{"site bad", q("sitename", "site"+"-a"), RuleSite},
		{"asn ok", keyValue{key: "remoteas", value: "64513", col: 1}, ""},
		{"asn bad", keyValue{key: "peerasn", value: "1" + "2076", col: 1}, RuleASN},
		{"asn non numeric", q("asn", "auto"), ""},
		{"serial ok", q("serialnumber", "TST0000001"), ""},
		{"serial bad", q("serial", "FOC"+"1234"), RuleSerial},
		{"serial pseudo mac ok", q("serial", "tpd_00005e005314"), ""},
		{"serial pseudo mac bad", q("serial", "tpd_"+"001aa1000001"), RuleMAC},
		{"credential ok", q("password", "changeme"), ""},
		{"credential bad", q("clientsecret", "s3"+"cret"), RuleCredential},
		{"unrelated key", q("description", "anything goes"), ""},
	}
	for _, tt := range tests {
		fs, _ := evaluateKeyValue(tt.kv)
		got := ""
		if len(fs) > 0 {
			got = fs[0].rule
		}
		if got != tt.rule || len(fs) > 1 {
			t.Errorf("%s: findings %v, want rule %q", tt.name, fs, tt.rule)
		}
	}
}

func TestCredentialFindingHidesValue(t *testing.T) {
	secretValue := "hunter" + "2-very-real"
	fs, _ := evaluateKeyValue(keyValue{key: "password", value: secretValue, quoted: true})
	if len(fs) != 1 || strings.Contains(fs[0].message, secretValue) {
		t.Fatalf("credential finding leaks the value: %v", fs)
	}
}
