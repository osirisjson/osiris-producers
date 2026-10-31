// client_test.go - Unit tests for the NETCONF/SSH client.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package iosxe

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"testing"

	"go.osirisjson.org/producers/osiris/network/cisco/run"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

var (
	reqMessageIDRe   = regexp.MustCompile(`message-id="([^"]*)"`)
	replyRootRe      = regexp.MustCompile(`<((?:[A-Za-z_][\w.-]*:)?rpc-reply)\b`)
	replyMessageIDRe = regexp.MustCompile(`^(<(?:[A-Za-z_][\w.-]*:)?rpc-reply\b[^>]*?)\s+message-id="[^"]*"`)
)

// echoMessageID makes a canned reply carry the request's message-id on
// its rpc-reply root, replacing any id already there, as a conforming
// device does.
func echoMessageID(req, reply []byte) []byte {
	m := reqMessageIDRe.FindSubmatch(req)
	if m == nil {
		return reply
	}
	loc := replyRootRe.FindIndex(reply)
	if loc == nil {
		return reply
	}
	head, tail := reply[:loc[0]], reply[loc[0]:]
	tail = replyMessageIDRe.ReplaceAll(tail, []byte("$1"))
	root := replyRootRe.FindSubmatchIndex(tail)
	out := append([]byte{}, head...)
	out = append(out, tail[:root[3]]...)
	out = append(out, []byte(fmt.Sprintf(` message-id="%s"`, m[1]))...)
	return append(out, tail[root[3]:]...)
}

func truncateBytes(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n]) + "..."
	}
	return string(b)
}

// mockTransport implements Transport for testing.
// It maps RPC request substrings to canned XML responses.
type mockTransport struct {
	// responses maps a substring of the RPC request to a canned reply.
	responses map[string]string
	closed    bool
	sendCount int
}

func (m *mockTransport) Send(rpc []byte) ([]byte, error) {
	if m.closed {
		return nil, fmt.Errorf("transport closed")
	}
	m.sendCount++
	req := string(rpc)
	for key, reply := range m.responses {
		if strings.Contains(req, key) {
			return echoMessageID(rpc, []byte(reply)), nil
		}
	}
	return nil, fmt.Errorf("no mock response for RPC: %s", truncateBytes(rpc, 100))
}

func (m *mockTransport) Close() error {
	m.closed = true
	return nil
}

func newMockClient(responses map[string]string) *Client {
	return &Client{
		transport: &mockTransport{responses: responses},
		logger:    testLogger(),
		addr:      "192.0.2.1:830",
	}
}

func TestGet_Success(t *testing.T) {
	responses := map[string]string{
		"ietf-interfaces": `<?xml version="1.0" encoding="UTF-8"?>
<rpc-reply xmlns="urn:ietf:params:xml:ns:netconf:base:1.0" message-id="1">
  <data>
    <interfaces xmlns="urn:ietf:params:xml:ns:yang:ietf-interfaces">
      <interface>
        <name>GigabitEthernet0/0/0</name>
        <type>ianaift:ethernetCsmacd</type>
        <enabled>true</enabled>
      </interface>
    </interfaces>
  </data>
</rpc-reply>`,
	}

	c := newMockClient(responses)
	data, err := c.Get(`<interfaces xmlns="urn:ietf:params:xml:ns:yang:ietf-interfaces"/>`)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if !strings.Contains(string(data), "GigabitEthernet0/0/0") {
		t.Errorf("expected interface name in data, got: %s", string(data))
	}
	// Should not contain <data> wrapper tags.
	if strings.Contains(string(data), "</data>") {
		t.Error("data should not contain </data> wrapper")
	}
}

func TestGet_RPCError(t *testing.T) {
	responses := map[string]string{
		"bogus-model": `<?xml version="1.0" encoding="UTF-8"?>
<rpc-reply xmlns="urn:ietf:params:xml:ns:netconf:base:1.0" message-id="1">
  <rpc-error>
    <error-type>application</error-type>
    <error-tag>invalid-value</error-tag>
    <error-severity>error</error-severity>
    <error-message>No such YANG module</error-message>
  </rpc-error>
</rpc-reply>`,
	}

	c := newMockClient(responses)
	_, err := c.Get(`<bogus-model xmlns="urn:example:bogus"/>`)
	if err == nil {
		t.Fatal("expected error for RPC error response")
	}
	if !strings.Contains(err.Error(), "invalid-value") {
		t.Errorf("error should mention error-tag: %v", err)
	}
	if !strings.Contains(err.Error(), "No such YANG module") {
		t.Errorf("error should mention message: %v", err)
	}
}

func TestGetConfig_Success(t *testing.T) {
	responses := map[string]string{
		"get-config": `<?xml version="1.0" encoding="UTF-8"?>
<rpc-reply xmlns="urn:ietf:params:xml:ns:netconf:base:1.0" message-id="1">
  <data>
    <native xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-native">
      <version>16.9</version>
      <hostname>LAB-RTR01</hostname>
    </native>
  </data>
</rpc-reply>`,
	}

	c := newMockClient(responses)
	data, err := c.GetConfig(`<native xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-native"><version/></native>`)
	if err != nil {
		t.Fatalf("GetConfig failed: %v", err)
	}

	if !strings.Contains(string(data), "LAB-RTR01") {
		t.Errorf("expected hostname in data, got: %s", string(data))
	}
}

func TestClose(t *testing.T) {
	mock := &mockTransport{
		responses: map[string]string{
			"close-session": `<?xml version="1.0" encoding="UTF-8"?>
<rpc-reply xmlns="urn:ietf:params:xml:ns:netconf:base:1.0" message-id="1"><ok/></rpc-reply>`,
		},
	}
	c := &Client{
		transport: mock,
		logger:    testLogger(),
	}

	err := c.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if !mock.closed {
		t.Error("transport should be closed")
	}
}

func TestClose_NilTransport(t *testing.T) {
	c := &Client{logger: testLogger()}
	err := c.Close()
	if err != nil {
		t.Fatalf("Close on nil transport should not error: %v", err)
	}
}

func TestNewClient(t *testing.T) {
	target := run.TargetConfig{Host: "192.0.2.1", Port: 2830}
	c := NewClient(target, true, testLogger())
	if c.addr != "192.0.2.1:2830" {
		t.Errorf("unexpected addr: %s", c.addr)
	}
	if !c.insecure {
		t.Error("insecure should be true")
	}
}

func TestNewClient_DefaultPort(t *testing.T) {
	target := run.TargetConfig{Host: "192.0.2.1"}
	c := NewClient(target, false, testLogger())
	if c.addr != "192.0.2.1:830" {
		t.Errorf("unexpected addr: %s", c.addr)
	}
}

func TestGet_NotConnected(t *testing.T) {
	c := &Client{logger: testLogger()}
	_, err := c.Get(`<interfaces/>`)
	if err == nil {
		t.Fatal("expected error when not connected")
	}
	if !strings.Contains(err.Error(), "not connected") {
		t.Errorf("error should mention not connected: %v", err)
	}
}

func TestMessageID_AutoIncrement(t *testing.T) {
	c := newMockClient(nil)

	msg1, id1 := c.wrapRPC("<get/>")
	msg2, id2 := c.wrapRPC("<get/>")

	if id1 != "1" || !strings.Contains(string(msg1), `message-id="1"`) {
		t.Errorf("first message should have id 1: %s", msg1)
	}
	if id2 != "2" || !strings.Contains(string(msg2), `message-id="2"`) {
		t.Errorf("second message should have id 2: %s", msg2)
	}
}

func TestEchoMessageID(t *testing.T) {
	req := []byte(`<rpc xmlns="urn:ietf:params:xml:ns:netconf:base:1.0" message-id="7"><get/></rpc>`)
	cases := map[string]string{
		`<rpc-reply xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><ok/></rpc-reply>`:                                  `<rpc-reply message-id="7" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><ok/></rpc-reply>`,
		`<rpc-reply xmlns="urn:ietf:params:xml:ns:netconf:base:1.0" message-id="1"><ok/></rpc-reply>`:                   `<rpc-reply message-id="7" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><ok/></rpc-reply>`,
		`<?xml version="1.0"?><nc:rpc-reply xmlns:nc="urn:ietf:params:xml:ns:netconf:base:1.0"><nc:ok/></nc:rpc-reply>`: `<?xml version="1.0"?><nc:rpc-reply message-id="7" xmlns:nc="urn:ietf:params:xml:ns:netconf:base:1.0"><nc:ok/></nc:rpc-reply>`,
	}
	for in, want := range cases {
		if got := string(echoMessageID(req, []byte(in))); got != want {
			t.Errorf("echoMessageID(%q)\n got %q\nwant %q", in, got, want)
		}
	}
}

// scriptTransport returns its replies in order, ignoring the request.
type scriptTransport struct {
	replies []string
	errs    []error
	sent    []string
	closed  bool
}

func (s *scriptTransport) Send(rpc []byte) ([]byte, error) {
	i := len(s.sent)
	s.sent = append(s.sent, string(rpc))
	if i < len(s.errs) && s.errs[i] != nil {
		return nil, s.errs[i]
	}
	if i >= len(s.replies) {
		return nil, fmt.Errorf("unexpected request %d", i+1)
	}
	return []byte(s.replies[i]), nil
}

func (s *scriptTransport) Close() error {
	s.closed = true
	return nil
}

const replyID1 = `<rpc-reply xmlns="urn:ietf:params:xml:ns:netconf:base:1.0" message-id="1"><data><x xmlns="urn:example"/></data></rpc-reply>`

func TestRPC_MessageIDMismatchMakesSessionUnusable(t *testing.T) {
	// The second request (id 2) receives a stale reply for id 1, as
	// happens when an earlier reply arrives late.
	tr := &scriptTransport{replies: []string{replyID1, replyID1, replyID1}}
	c := &Client{transport: tr, logger: testLogger()}

	if _, err := c.Get("<x/>"); err != nil {
		t.Fatalf("first request: %v", err)
	}
	_, err := c.Get("<x/>")
	if !errors.Is(err, errMessageIDMismatch) {
		t.Fatalf("second request: err = %v, want errMessageIDMismatch", err)
	}
	_, err = c.Get("<x/>")
	if !errors.Is(err, errSessionUnusable) {
		t.Fatalf("third request: err = %v, want errSessionUnusable", err)
	}
	if len(tr.sent) != 2 {
		t.Errorf("an unusable session must not send further requests; sent %d", len(tr.sent))
	}

	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if len(tr.sent) != 2 || !tr.closed {
		t.Errorf("Close on an unusable session should close without close-session; sent %d, closed %v", len(tr.sent), tr.closed)
	}
}

func TestRPC_TransportErrorMakesSessionUnusable(t *testing.T) {
	tr := &scriptTransport{errs: []error{errReplyTooLarge}}
	c := &Client{transport: tr, logger: testLogger()}

	if _, err := c.Get("<x/>"); !errors.Is(err, errReplyTooLarge) {
		t.Fatalf("err = %v, want errReplyTooLarge", err)
	}
	if _, err := c.Get("<x/>"); !errors.Is(err, errSessionUnusable) {
		t.Fatalf("err = %v, want errSessionUnusable", err)
	}
}

func TestRPC_DeviceErrorKeepsSessionUsable(t *testing.T) {
	tr := &scriptTransport{replies: []string{
		`<rpc-reply xmlns="urn:ietf:params:xml:ns:netconf:base:1.0" message-id="1"><rpc-error><error-type>application</error-type><error-tag>unknown-namespace</error-tag><error-severity>error</error-severity></rpc-error></rpc-reply>`,
		`<rpc-reply xmlns="urn:ietf:params:xml:ns:netconf:base:1.0" message-id="2"><data/></rpc-reply>`,
	}}
	c := &Client{transport: tr, logger: testLogger()}

	if _, err := c.Get("<x/>"); !errors.Is(err, errRPCUnsupported) {
		t.Fatalf("err = %v, want errRPCUnsupported", err)
	}
	data, err := c.Get("<x/>")
	if err != nil {
		t.Fatalf("a device rpc-error must not make the session unusable: %v", err)
	}
	if data == nil || len(data) != 0 {
		t.Errorf("empty <data/> should yield non-nil empty content, got %q", data)
	}
}
