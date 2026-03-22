// client_test.go - Unit tests for the NETCONF/SSH client.
// Uses a mockTransport implementing the Transport interface to test
// RPC operations without real SSH connections. All test data is invented.
//
// For an introduction to OSIRIS JSON Producer for Cisco see:
// "[OSIRIS-JSON-CISCO]."
//
// [OSIRIS-JSON-CISCO]: https://osirisjson.org/en/docs/producers/cisco

package iosxe

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"

	"go.osirisjson.org/producers/osiris/network/cisco/run"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// mockTransport implements Transport for testing. It maps RPC request substrings
// to canned XML responses.
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
			return []byte(reply), nil
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
		addr:      "10.99.0.1:830",
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
	target := run.TargetConfig{Host: "10.99.0.1", Port: 2830}
	c := NewClient(target, true, testLogger())
	if c.addr != "10.99.0.1:2830" {
		t.Errorf("unexpected addr: %s", c.addr)
	}
	if !c.insecure {
		t.Error("insecure should be true")
	}
}

func TestNewClient_DefaultPort(t *testing.T) {
	target := run.TargetConfig{Host: "10.99.0.1"}
	c := NewClient(target, false, testLogger())
	if c.addr != "10.99.0.1:830" {
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
	responses := map[string]string{
		"<get>": `<rpc-reply xmlns="urn:ietf:params:xml:ns:netconf:base:1.0" message-id="1"><data></data></rpc-reply>`,
	}
	c := newMockClient(responses)

	msg1 := c.wrapRPC("<get/>")
	msg2 := c.wrapRPC("<get/>")

	if !strings.Contains(string(msg1), `message-id="1"`) {
		t.Errorf("first message should have id 1: %s", msg1)
	}
	if !strings.Contains(string(msg2), `message-id="2"`) {
		t.Errorf("second message should have id 2: %s", msg2)
	}
}

func TestExtractDataContent(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			"normal",
			`<rpc-reply><data><foo>bar</foo></data></rpc-reply>`,
			`<foo>bar</foo>`,
		},
		{
			"data with attributes",
			`<rpc-reply><data xmlns="urn:example"><foo/></data></rpc-reply>`,
			`<foo/>`,
		},
		{
			"no data element",
			`<rpc-reply><ok/></rpc-reply>`,
			`<rpc-reply><ok/></rpc-reply>`,
		},
		{
			"empty data",
			`<rpc-reply><data></data></rpc-reply>`,
			``,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := string(extractDataContent([]byte(tc.input)))
			if got != tc.want {
				t.Errorf("extractDataContent(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
