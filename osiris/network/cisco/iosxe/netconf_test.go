// netconf_test.go - Unit tests for NETCONF message framing
// (netconf.go): end-of-message and chunked reads and writes,
// malformed-frame and size limits, capability negotiation and the
// <hello> exchange. Synthetic byte streams only; no device data.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [RFC 6242]: https://www.rfc-editor.org/rfc/rfc6242#section-4

package iosxe

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"testing/iotest"
)

// oversizeReader yields non-delimiter bytes on every Read and never
// returns EOF, forcing a reader to hit its byte cap.
type oversizeReader struct{}

func (oversizeReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

func readerFramer(s string, chunked bool) *framer {
	f := newFramer(strings.NewReader(s), nil)
	f.chunked = chunked
	return f
}

func TestReadEOM(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    []string
		wantErr error
	}{
		{"single", "<a/>]]>]]>", []string{"<a/>"}, nil},
		{"back to back", "<a/>]]>]]><b/>]]>]]>", []string{"<a/>", "<b/>"}, nil},
		{"payload contains >", "<a>x&gt;y</a>]]>]]>", []string{"<a>x&gt;y</a>"}, nil},
		{"partial delimiter in payload", "<a>]]></a>]]>]]>", []string{"<a>]]></a>"}, nil},
		{"eof before delimiter", "<a/>", nil, errMalformedFrame},
		{"eof inside delimiter", "<a/>]]>]]", nil, errMalformedFrame},
	}
	for _, tc := range cases {
		for _, oneByte := range []bool{false, true} {
			name := tc.name
			if oneByte {
				name += "/one-byte reads"
			}
			t.Run(name, func(t *testing.T) {
				var f *framer
				if oneByte {
					f = newFramer(iotest.OneByteReader(strings.NewReader(tc.in)), nil)
				} else {
					f = readerFramer(tc.in, false)
				}
				for _, want := range tc.want {
					got, err := f.readMessage()
					if err != nil {
						t.Fatalf("readMessage: %v", err)
					}
					if string(got) != want {
						t.Errorf("got %q, want %q", got, want)
					}
				}
				if tc.wantErr != nil {
					if _, err := f.readMessage(); !errors.Is(err, tc.wantErr) {
						t.Errorf("err = %v, want %v", err, tc.wantErr)
					}
				}
			})
		}
	}
}

func TestReadEOM_RejectsOversizeReply(t *testing.T) {
	f := newFramer(oversizeReader{}, nil)
	if _, err := f.readMessage(); !errors.Is(err, errReplyTooLarge) {
		t.Fatalf("expected errReplyTooLarge, got: %v", err)
	}
}

func TestReadEOM_LimitIsInclusive(t *testing.T) {
	f := readerFramer("12345678]]>]]>", false)
	f.limit = 8
	if got, err := f.readMessage(); err != nil || string(got) != "12345678" {
		t.Fatalf("message at exactly the limit: got %q, %v", got, err)
	}
	f = readerFramer("123456789]]>]]>", false)
	f.limit = 8
	if _, err := f.readMessage(); !errors.Is(err, errReplyTooLarge) {
		t.Fatalf("message over the limit: expected errReplyTooLarge, got %v", err)
	}
}

func TestReadChunked(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"single chunk", "\n#4\n<a/>\n##\n", []string{"<a/>"}},
		{"multiple chunks joined", "\n#3\n<rp\n#9\nc-reply/>\n##\n", []string{"<rpc-reply/>"}},
		{"back to back", "\n#4\n<a/>\n##\n\n#4\n<b/>\n##\n", []string{"<a/>", "<b/>"}},
		{"payload contains framing bytes", "\n#8\n\n##\n]]>]\n##\n", []string{"\n##\n]]>]"}},
		{"multi-digit size", "\n#12\n<rpc-reply/>\n##\n", []string{"<rpc-reply/>"}},
	}
	for _, tc := range cases {
		for _, oneByte := range []bool{false, true} {
			name := tc.name
			if oneByte {
				name += "/one-byte reads"
			}
			t.Run(name, func(t *testing.T) {
				var f *framer
				if oneByte {
					f = newFramer(iotest.OneByteReader(strings.NewReader(tc.in)), nil)
					f.chunked = true
				} else {
					f = readerFramer(tc.in, true)
				}
				for _, want := range tc.want {
					got, err := f.readMessage()
					if err != nil {
						t.Fatalf("readMessage: %v", err)
					}
					if string(got) != want {
						t.Errorf("got %q, want %q", got, want)
					}
				}
			})
		}
	}
}

func TestReadChunked_Malformed(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"missing leading newline", "#4\n<a/>\n##\n"},
		{"missing hash", "\n4\n<a/>\n##\n"},
		{"zero size", "\n#0\n\n##\n"},
		{"leading zero", "\n#04\n<a/>\n##\n"},
		{"non-digit size", "\n#4a\n<a/>\n##\n"},
		{"negative size", "\n#-4\n<a/>\n##\n"},
		{"empty size", "\n#\n<a/>\n##\n"},
		{"too many digits", "\n#12345678901\n"},
		{"size above RFC maximum", "\n#4294967296\n"},
		{"end-of-chunks with no chunk", "\n##\n"},
		{"end marker missing newline", "\n#4\n<a/>\n##x"},
		{"eof inside header", "\n#4"},
		{"eof inside chunk", "\n#10\n<a/>"},
		{"eof before end-of-chunks", "\n#4\n<a/>"},
		{"end-of-frame delimiter instead of chunks", "<a/>]]>]]>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := readerFramer(tc.in, true).readMessage()
			if !errors.Is(err, errMalformedFrame) {
				t.Errorf("err = %v, want errMalformedFrame", err)
			}
		})
	}
}

func TestReadChunked_SizeLimit(t *testing.T) {
	// A header announcing more than the budget is rejected before any
	// payload is read: the stream has no payload at all, so reaching
	// the payload would surface as a malformed frame instead.
	f := readerFramer("\n#9\n", true)
	f.limit = 8
	if _, err := f.readMessage(); !errors.Is(err, errReplyTooLarge) {
		t.Fatalf("oversize header: expected errReplyTooLarge, got %v", err)
	}

	// The budget is cumulative across chunks of one message.
	f = readerFramer("\n#5\n12345\n#4\n6789\n##\n", true)
	f.limit = 8
	if _, err := f.readMessage(); !errors.Is(err, errReplyTooLarge) {
		t.Fatalf("cumulative chunks: expected errReplyTooLarge, got %v", err)
	}

	// Exactly at the limit is accepted.
	f = readerFramer("\n#4\n1234\n#4\n5678\n##\n", true)
	f.limit = 8
	if got, err := f.readMessage(); err != nil || string(got) != "12345678" {
		t.Fatalf("at limit: got %q, %v", got, err)
	}

	// The default limit applies when none is set: the largest legal
	// chunk size is still far above maxReplyBytes.
	if _, err := readerFramer("\n#4294967295\n", true).readMessage(); !errors.Is(err, errReplyTooLarge) {
		t.Fatalf("default limit: expected errReplyTooLarge, got %v", err)
	}
}

func TestWriteMessage(t *testing.T) {
	var buf bytes.Buffer
	f := newFramer(nil, &buf)
	if err := f.writeMessage([]byte("<a/>")); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "<a/>]]>]]>" {
		t.Errorf("end-of-message write = %q", got)
	}

	buf.Reset()
	f.chunked = true
	if err := f.writeMessage([]byte("<rpc-reply/>")); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "\n#12\n<rpc-reply/>\n##\n" {
		t.Errorf("chunked write = %q", got)
	}

	if err := f.writeMessage(nil); !errors.Is(err, errMalformedFrame) {
		t.Errorf("empty chunked write: err = %v, want errMalformedFrame", err)
	}
}

func TestFramerRoundTrip(t *testing.T) {
	msgs := []string{"<a/>", "<rpc>]]>]]</rpc>", strings.Repeat("y", 70000)}
	for _, chunked := range []bool{false, true} {
		var wire bytes.Buffer
		w := newFramer(nil, &wire)
		w.chunked = chunked
		for _, m := range msgs {
			if err := w.writeMessage([]byte(m)); err != nil {
				t.Fatal(err)
			}
		}
		r := newFramer(&wire, nil)
		r.chunked = chunked
		for _, m := range msgs {
			got, err := r.readMessage()
			if err != nil {
				t.Fatalf("chunked=%v: %v", chunked, err)
			}
			if string(got) != m {
				t.Errorf("chunked=%v: round trip mismatch (%d vs %d bytes)", chunked, len(got), len(m))
			}
		}
	}
}

func TestNegotiateFraming(t *testing.T) {
	cases := []struct {
		name        string
		caps        []string
		wantChunked bool
		wantErr     error
	}{
		{"base 1.0 only", []string{capBase10}, false, nil},
		{"base 1.1 only", []string{capBase11}, true, nil},
		{"both", []string{capBase10, capBase11, "urn:ietf:params:netconf:capability:candidate:1.0"}, true, nil},
		{"whitespace around URI", []string{"\n        " + capBase11 + "\n      "}, true, nil},
		{"no base capability", []string{"urn:ietf:params:netconf:capability:notification:1.0"}, false, errUnsupportedBase},
		{"empty", nil, false, errUnsupportedBase},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			chunked, err := negotiateFraming(tc.caps)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if chunked != tc.wantChunked {
				t.Errorf("chunked = %v, want %v", chunked, tc.wantChunked)
			}
		})
	}
}

func helloWith(caps ...string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><hello xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><capabilities>`)
	for _, c := range caps {
		b.WriteString("<capability>" + c + "</capability>")
	}
	b.WriteString("</capabilities><session-id>42</session-id></hello>]]>]]>")
	return b.String()
}

func TestExchangeHello(t *testing.T) {
	cases := []struct {
		name        string
		server      string
		wantChunked bool
		wantErr     error
		wantWritten bool
	}{
		{"server offers 1.1", helloWith(capBase10, capBase11), true, nil, true},
		{"server offers 1.0 only", helloWith(capBase10), false, nil, true},
		{"no common base", helloWith("urn:example:other"), false, errUnsupportedBase, false},
		{"malformed hello XML", "<hello><capabilities>]]>]]>", false, nil, false},
		{"truncated hello", "<hello>", false, errMalformedFrame, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			f := newFramer(strings.NewReader(tc.server), &out)
			hello, err := exchangeHello(f)

			if tc.name == "malformed hello XML" {
				if err == nil || !strings.Contains(err.Error(), "not valid XML") {
					t.Fatalf("err = %v, want an invalid-XML error", err)
				}
			} else if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}

			if f.chunked != tc.wantChunked {
				t.Errorf("chunked = %v, want %v", f.chunked, tc.wantChunked)
			}
			if got := out.Len() > 0; got != tc.wantWritten {
				t.Errorf("client hello written = %v, want %v", got, tc.wantWritten)
			}
			if !tc.wantWritten {
				return
			}
			if hello.SessionID != 42 {
				t.Errorf("session-id = %d, want 42", hello.SessionID)
			}
			// The client hello is always end-of-message framed and
			// advertises both base versions.
			sent := out.String()
			if !strings.HasSuffix(sent, netconfDelim) {
				t.Errorf("client hello not end-of-message framed: %q", sent)
			}
			for _, c := range []string{capBase10, capBase11} {
				if !strings.Contains(sent, c) {
					t.Errorf("client hello missing %s", c)
				}
			}
		})
	}
}

// FuzzReadChunked checks that arbitrary input never panics and never
// yields a message larger than the configured limit.
func FuzzReadChunked(f *testing.F) {
	for _, seed := range []string{
		"\n#4\n<a/>\n##\n",
		"\n#3\n<rp\n#9\nc-reply/>\n##\n",
		"\n##\n",
		"\n#0\n",
		"\n#4294967296\n",
		"<a/>]]>]]>",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, in []byte) {
		fr := newFramer(bytes.NewReader(in), nil)
		fr.chunked = true
		fr.limit = 1024
		for range 4 {
			msg, err := fr.readMessage()
			if err != nil {
				return
			}
			if len(msg) == 0 || len(msg) > 1024 {
				t.Fatalf("message length %d outside (0, 1024]", len(msg))
			}
		}
	})
}

const replyPayload = `<interfaces xmlns="urn:ietf:params:xml:ns:yang:ietf-interfaces"><interface><name>GigabitEthernet1</name></interface></interfaces>`

func TestParseRPCReply_NamespaceForms(t *testing.T) {
	forms := map[string]string{
		"default namespace": `<?xml version="1.0" encoding="UTF-8"?><rpc-reply xmlns="urn:ietf:params:xml:ns:netconf:base:1.0" message-id="5"><data>` + replyPayload + `</data></rpc-reply>`,
		"prefixed":          `<?xml version="1.0" encoding="UTF-8"?><nc:rpc-reply xmlns:nc="urn:ietf:params:xml:ns:netconf:base:1.0" message-id="5"><nc:data>` + replyPayload + `</nc:data></nc:rpc-reply>`,
		"other prefix name": `<x:rpc-reply xmlns:x="urn:ietf:params:xml:ns:netconf:base:1.0" message-id="5"><x:data>` + replyPayload + `</x:data></x:rpc-reply>`,
		"whitespace":        "<rpc-reply xmlns=\"urn:ietf:params:xml:ns:netconf:base:1.0\"\n    message-id=\"5\">\n  <data>" + replyPayload + "</data>\n</rpc-reply>\n",
	}
	for name, reply := range forms {
		t.Run(name, func(t *testing.T) {
			data, warnings, err := parseRPCReply([]byte(reply), "5")
			if err != nil {
				t.Fatalf("parseRPCReply: %v", err)
			}
			if string(data) != replyPayload {
				t.Errorf("data = %q, want %q", data, replyPayload)
			}
			if len(warnings) != 0 {
				t.Errorf("unexpected warnings: %v", warnings)
			}
		})
	}
}

func TestParseRPCReply_Invalid(t *testing.T) {
	cases := []struct {
		name    string
		reply   string
		wantErr error
	}{
		{"malformed XML", `<rpc-reply xmlns="urn:ietf:params:xml:ns:netconf:base:1.0" message-id="5"><data>`, errInvalidReply},
		{"not XML", `]]>]]>`, errInvalidReply},
		{"wrong root element", `<hello xmlns="urn:ietf:params:xml:ns:netconf:base:1.0" message-id="5"/>`, errInvalidReply},
		{"notification instead of reply", `<notification xmlns="urn:ietf:params:xml:ns:netconf:notification:1.0"><eventTime>2026-01-01T00:00:00Z</eventTime></notification>`, errInvalidReply},
		{"rpc-reply without namespace", `<rpc-reply message-id="5"><data/></rpc-reply>`, errInvalidReply},
		{"rpc-reply in foreign namespace", `<rpc-reply xmlns="urn:example:fake" message-id="5"><data/></rpc-reply>`, errInvalidReply},
		{"data in foreign namespace", `<rpc-reply xmlns="urn:ietf:params:xml:ns:netconf:base:1.0" message-id="5"><data xmlns="urn:example:fake"/></rpc-reply>`, errInvalidReply},
		{"ok where data expected", `<rpc-reply xmlns="urn:ietf:params:xml:ns:netconf:base:1.0" message-id="5"><ok/></rpc-reply>`, errInvalidReply},
		{"wrong message-id", `<rpc-reply xmlns="urn:ietf:params:xml:ns:netconf:base:1.0" message-id="4"><data/></rpc-reply>`, errMessageIDMismatch},
		{"missing message-id", `<rpc-reply xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><data/></rpc-reply>`, errMessageIDMismatch},
		{"padded message-id", `<rpc-reply xmlns="urn:ietf:params:xml:ns:netconf:base:1.0" message-id=" 5"><data/></rpc-reply>`, errMessageIDMismatch},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, _, err := parseRPCReply([]byte(tc.reply), "5")
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if data != nil {
				t.Errorf("a rejected reply must not yield data, got %q", data)
			}
		})
	}
}

func TestParseRPCReply_EmptyData(t *testing.T) {
	for _, reply := range []string{
		`<rpc-reply xmlns="urn:ietf:params:xml:ns:netconf:base:1.0" message-id="5"><data/></rpc-reply>`,
		`<rpc-reply xmlns="urn:ietf:params:xml:ns:netconf:base:1.0" message-id="5"><data></data></rpc-reply>`,
	} {
		data, _, err := parseRPCReply([]byte(reply), "5")
		if err != nil {
			t.Fatalf("%s: %v", reply, err)
		}
		if data == nil || len(data) != 0 {
			t.Errorf("%s: data = %q, want non-nil empty", reply, data)
		}
	}
}

// rpcErrorReply builds an rpc-reply carrying one rpc-error with the
// given error-tag, using prefix for every NETCONF element ("" for the
// default namespace).
func rpcErrorReply(prefix, tag string) string {
	p, decl := "", `xmlns="`+ncBaseNS+`"`
	if prefix != "" {
		p, decl = prefix+":", `xmlns:`+prefix+`="`+ncBaseNS+`"`
	}
	el := func(name, v string) string { return "<" + p + name + ">" + v + "</" + p + name + ">" }
	return "<" + p + "rpc-reply " + decl + ` message-id="5">` +
		el("rpc-error", el("error-type", "application")+el("error-tag", tag)+
			el("error-severity", "error")+el("error-message", "model not loaded")) +
		"</" + p + "rpc-reply>"
}

func TestParseRPCReply_RPCErrorClassification(t *testing.T) {
	cases := []struct {
		tag  string
		want error
	}{
		{"operation-not-supported", errRPCUnsupported},
		{"unknown-element", errRPCUnsupported},
		{"unknown-namespace", errRPCUnsupported},
		{"unknown-attribute", errRPCUnsupported},
		{"access-denied", errRPCAccessDenied},
		{"invalid-value", errRPCFailed},
		{"operation-failed", errRPCFailed},
		{"resource-denied", errRPCFailed},
	}
	for _, prefix := range []string{"", "nc"} {
		for _, tc := range cases {
			t.Run(prefix+"/"+tc.tag, func(t *testing.T) {
				_, _, err := parseRPCReply([]byte(rpcErrorReply(prefix, tc.tag)), "5")
				if !errors.Is(err, tc.want) {
					t.Fatalf("err = %v, want class %v", err, tc.want)
				}
				var re *rpcError
				if !errors.As(err, &re) || re.Tag != tc.tag {
					t.Fatalf("errors.As rpcError: %v (%+v)", err, re)
				}
				for _, other := range []error{errRPCUnsupported, errRPCAccessDenied, errRPCFailed} {
					if other != tc.want && errors.Is(err, other) {
						t.Errorf("error also matches %v", other)
					}
				}
				if !strings.Contains(err.Error(), tc.tag) || !strings.Contains(err.Error(), "model not loaded") {
					t.Errorf("error text should carry tag and message: %v", err)
				}
			})
		}
	}
}

func TestParseRPCReply_WarningsAndMultipleErrors(t *testing.T) {
	warn := `<rpc-error><error-type>application</error-type><error-tag>operation-failed</error-tag><error-severity>warning</error-severity><error-message>partial data</error-message></rpc-error>`
	fail := `<rpc-error><error-type>protocol</error-type><error-tag>access-denied</error-tag><error-severity>error</error-severity></rpc-error>`
	fail2 := `<rpc-error><error-type>application</error-type><error-tag>invalid-value</error-tag><error-severity>error</error-severity></rpc-error>`

	// Warnings alone do not fail the reply; data is still returned.
	reply := `<rpc-reply xmlns="urn:ietf:params:xml:ns:netconf:base:1.0" message-id="5">` + warn + `<data>` + replyPayload + `</data></rpc-reply>`
	data, warnings, err := parseRPCReply([]byte(reply), "5")
	if err != nil {
		t.Fatalf("warning-only reply: %v", err)
	}
	if string(data) != replyPayload || len(warnings) != 1 || warnings[0].Message != "partial data" {
		t.Errorf("warning-only reply: data %q, warnings %+v", data, warnings)
	}

	// The first error of severity error wins, with a count of the rest.
	reply = `<rpc-reply xmlns="urn:ietf:params:xml:ns:netconf:base:1.0" message-id="5">` + warn + fail + fail2 + `</rpc-reply>`
	_, warnings, err = parseRPCReply([]byte(reply), "5")
	if !errors.Is(err, errRPCAccessDenied) || errors.Is(err, errRPCFailed) {
		t.Fatalf("multiple errors: err = %v, want the first error's class only", err)
	}
	if !strings.Contains(err.Error(), "and 1 more") {
		t.Errorf("multiple errors: count missing from %v", err)
	}
	if len(warnings) != 1 {
		t.Errorf("multiple errors: warnings = %d, want 1", len(warnings))
	}
}

func TestParseRPCReply_ErrorSanitization(t *testing.T) {
	long := strings.Repeat("a", 500)
	reply := `<rpc-reply xmlns="urn:ietf:params:xml:ns:netconf:base:1.0" message-id="5"><rpc-error>` +
		`<error-type>application</error-type><error-tag>invalid-value</error-tag><error-severity>error</error-severity>` +
		`<error-path>/native/username[name='admin']/secret</error-path>` +
		`<error-message>bad&#x7f;[31m value&#x0a;&#x0a;   here ` + long + `</error-message>` +
		`<error-info><bad-element>secret</bad-element><bad-value>changeme</bad-value></error-info>` +
		`</rpc-error></rpc-reply>`
	_, _, err := parseRPCReply([]byte(reply), "5")
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := err.Error()
	for _, leaked := range []string{"changeme", "username[name='admin']", "error-info", "<", "\x7f", "\n"} {
		if strings.Contains(msg, leaked) {
			t.Errorf("error text leaks %q: %q", leaked, msg)
		}
	}
	if !strings.Contains(msg, "bad[31m value here aaa") {
		t.Errorf("control characters should be dropped and whitespace collapsed: %q", msg)
	}
	if n := strings.Count(msg, "a"); n > maxErrorMessageRunes+10 {
		t.Errorf("error-message not bounded: %d runes of padding kept", n)
	}

	// The invalid-reply path must not echo reply content either.
	_, _, err = parseRPCReply([]byte(`<rpc-reply xmlns="urn:ietf:params:xml:ns:netconf:base:1.0" message-id="5"><data><secret>changeme</secret>`), "5")
	if err == nil || strings.Contains(err.Error(), "changeme") {
		t.Errorf("malformed reply error leaks content: %v", err)
	}
}

func TestSanitizeErrorText(t *testing.T) {
	cases := []struct {
		in   string
		max  int
		want string
	}{
		{"  plain text  ", 50, "plain text"},
		{"tab\tand\nnewline", 50, "tab and newline"},
		{"bell\x07char", 50, "bellchar"},
		{"abcdef", 3, "abc..."},
		{"", 10, ""},
	}
	for _, tc := range cases {
		if got := sanitizeErrorText(tc.in, tc.max); got != tc.want {
			t.Errorf("sanitizeErrorText(%q, %d) = %q, want %q", tc.in, tc.max, got, tc.want)
		}
	}
}

// FuzzParseRPCReply checks that arbitrary replies never panic and that
// data is only ever returned together with a nil error.
func FuzzParseRPCReply(f *testing.F) {
	for _, seed := range []string{
		`<rpc-reply xmlns="urn:ietf:params:xml:ns:netconf:base:1.0" message-id="5"><data>` + replyPayload + `</data></rpc-reply>`,
		`<nc:rpc-reply xmlns:nc="urn:ietf:params:xml:ns:netconf:base:1.0" message-id="5"><nc:rpc-error><nc:error-tag>access-denied</nc:error-tag></nc:rpc-error></nc:rpc-reply>`,
		`<rpc-reply message-id="5"/>`,
		`<`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, in []byte) {
		data, _, err := parseRPCReply(in, "5")
		if err != nil && data != nil {
			t.Fatalf("data returned alongside error %v", err)
		}
	})
}
