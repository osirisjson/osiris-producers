// raw_test.go - Tests for --include-raw-body redaction, size limits,
// and the configuration exclusion of the Cisco IOS-XE OSIRIS JSON
// Producer.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package iosxe

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strings"
	"testing"

	"go.osirisjson.org/producers/pkg/sdk"
)

func TestRedactXML(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"plain content kept",
			`<a xmlns="urn:x"><b>192.0.2.1</b></a>`,
			`<a xmlns="urn:x"><b>192.0.2.1</b></a>`},
		{"sensitive subtree replaced",
			`<a><key><encryption>7</encryption><key>changeme</key></key><c>1</c></a>`,
			`<a><key>REDACTED</key><c>1</c></a>`},
		{"sensitive-key pattern on element name",
			`<a><auth-password>changeme</auth-password><tp-authenticated>true</tp-authenticated></a>`,
			`<a><auth-password>REDACTED</auth-password><tp-authenticated>REDACTED</tp-authenticated></a>`},
		{"community string",
			`<a><community-or-user>changeme</community-or-user></a>`,
			`<a><community-or-user>REDACTED</community-or-user></a>`},
		{"secret value pattern in text",
			`<a><description>Bearer abcdefghijklmnop</description></a>`,
			`<a><description>REDACTED</description></a>`},
		{"secret value pattern in attribute",
			`<a note="https://admin:changeme@ca.example.com/">x</a>`,
			`<a note="REDACTED">x</a>`},
		{"prefixes, escaping, comments and declarations",
			`<?xml version="1.0"?><!-- note --><nc:a xmlns:nc="urn:x">&lt;1&amp;</nc:a>`,
			`<nc:a xmlns:nc="urn:x">&lt;1&amp;</nc:a>`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := redactXML([]byte(tc.in))
			if !ok {
				t.Fatal("redactXML rejected well-formed input")
			}
			if string(got) != tc.want {
				t.Errorf("got  %s\nwant %s", got, tc.want)
			}
		})
	}
}

func TestRedactXML_RejectsMalformed(t *testing.T) {
	for _, in := range []string{`<a><b></a>`, `<a>`, `</a>`, `<a><key>x</a>`} {
		if _, ok := redactXML([]byte(in)); ok {
			t.Errorf("redactXML(%q) must reject malformed input", in)
		}
	}
}

func TestRawCollector(t *testing.T) {
	c := &rawCollector{}
	c.add(qInterfacesOper, []byte(`<interfaces><interface><name>Gi1</name></interface></interfaces>`))
	c.add(qAccounts, []byte(`<native><username><name>admin</name></username></native>`))
	c.add(qLLDP, []byte(`<lldp-entries><x>`))
	c.add(qCDP, []byte(" \n "))
	c.add(qVLANs, []byte(`<vlans>`+strings.Repeat("v", maxRawReplyBytes)+`</vlans>`))

	cisco := map[string]any{}
	c.apply(cisco)
	replies := cisco["raw_replies"].(map[string]string)
	if len(replies) != 1 || replies["interfaces-oper"] == "" {
		t.Errorf("raw_replies = %v", replies)
	}
	if s := mustJSON(t, cisco["raw_omitted"]); s != `[{"operation":"local-accounts","reason":"configuration"},{"operation":"lldp-neighbors","reason":"unredactable"},{"operation":"vlans","reason":"too-large"}]` {
		t.Errorf("raw_omitted = %s", s)
	}

	total := &rawCollector{total: maxRawTotalBytes - 10}
	total.add(qBGP, []byte(`<bgp-state-data><neighbors/></bgp-state-data>`))
	if s := mustJSON(t, total.omitted); s != `[{"operation":"bgp","reason":"total-limit"}]` {
		t.Errorf("total limit: %s", s)
	}

	var none *rawCollector
	none.add(qBGP, []byte(`<a/>`))
	none.apply(cisco)
}

// TestQueries_ConfigurationFlag pins which operations read
// configuration: raw replies are never attached for them.
func TestQueries_ConfigurationFlag(t *testing.T) {
	config := map[string]bool{}
	for _, q := range discoveryQueries {
		if strings.Contains(q.filter, nsNative) != q.config {
			t.Errorf("%s: config=%v, but its filter %s Cisco-IOS-XE-native", q.op, q.config,
				map[bool]string{true: "reads", false: "does not read"}[strings.Contains(q.filter, nsNative)])
		}
		config[q.op] = q.config
	}
	for _, op := range []string{"aaa", "local-accounts", "management-services", "dns", "snmp", "logging", "ntp", "acls", "crypto", "control-plane"} {
		if !config[op] {
			t.Errorf("%s must be a configuration operation", op)
		}
	}
}

// FuzzRedactXML checks that redactXML output is always well-formed and
// never carries a value an SDK secret pattern matches.
func FuzzRedactXML(f *testing.F) {
	f.Add([]byte(`<a><key>changeme</key><b x="1">Bearer abcdefghijklmnop</b></a>`))
	f.Add([]byte(`<nc:a xmlns:nc="urn:x"><nc:secret>x</nc:secret></nc:a>`))
	f.Fuzz(func(t *testing.T, data []byte) {
		out, ok := redactXML(data)
		if !ok {
			return
		}
		d := xml.NewDecoder(bytes.NewReader(out))
		for {
			tok, err := d.RawToken()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatalf("redacted output does not reparse: %v\n%s", err, out)
			}
			switch v := tok.(type) {
			case xml.CharData:
				if sdk.ScanValue(string(v)) != "" {
					t.Fatalf("secret pattern survived redaction: %q", v)
				}
			case xml.StartElement:
				for _, a := range v.Attr {
					if sdk.ScanValue(a.Value) != "" {
						t.Fatalf("secret pattern survived in an attribute: %q", a.Value)
					}
				}
			}
		}
	})
}
