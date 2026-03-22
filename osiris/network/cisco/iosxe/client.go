// client.go - NETCONF over SSH client for the Cisco IOS-XE producer.
// Implements the NETCONF 1.0 protocol (RFC 4741) over an SSH subsystem,
// with a Transport interface that enables test injection without real SSH.
//
// For an introduction to OSIRIS JSON Producer for Cisco see:
// "[OSIRIS-JSON-CISCO]."
//
// [OSIRIS-JSON-CISCO]: https://osirisjson.org/en/docs/producers/cisco

package iosxe

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"go.osirisjson.org/producers/osiris/network/cisco/run"
)

const (
	defaultPort   = 830
	sshTimeout    = 30 * time.Second
	netconfDelim  = "]]>]]>"
	maxReplyBytes = 10 * 1024 * 1024 // 10 MiB safety limit
)

// Transport abstracts the NETCONF message exchange, enabling mock injection for tests.
type Transport interface {
	Send(rpc []byte) ([]byte, error)
	Close() error
}

// Client is a NETCONF client for IOS-XE devices.
type Client struct {
	transport Transport
	msgID     int
	mu        sync.Mutex
	logger    *slog.Logger
	addr      string
	insecure  bool
}

// NewClient creates a NETCONF client targeting the given address.
// It does not connect; call Connect to establish the SSH session.
func NewClient(target run.TargetConfig, insecure bool, logger *slog.Logger) *Client {
	addr := run.ResolveAddr(target, defaultPort)
	return &Client{
		addr:     addr,
		insecure: insecure,
		logger:   logger,
	}
}

// Connect establishes an SSH connection, requests the netconf subsystem,
// and exchanges NETCONF <hello> capabilities.
func (c *Client) Connect(username, password string) error {
	config := &ssh.ClientConfig{
		User: username,
		Auth: []ssh.AuthMethod{
			ssh.Password(password),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         sshTimeout,
	}

	conn, err := ssh.Dial("tcp", c.addr, config)
	if err != nil {
		return fmt.Errorf("NETCONF SSH dial failed (%s): %w", c.addr, err)
	}

	session, err := conn.NewSession()
	if err != nil {
		conn.Close()
		return fmt.Errorf("NETCONF SSH session failed: %w", err)
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		session.Close()
		conn.Close()
		return fmt.Errorf("NETCONF stdin pipe failed: %w", err)
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		conn.Close()
		return fmt.Errorf("NETCONF stdout pipe failed: %w", err)
	}

	if err := session.RequestSubsystem("netconf"); err != nil {
		session.Close()
		conn.Close()
		return fmt.Errorf("NETCONF subsystem request failed: %w", err)
	}

	t := &sshTransport{
		conn:    conn,
		session: session,
		stdin:   stdin,
		stdout:  stdout,
	}

	// Read server hello.
	serverHello, err := t.readMessage()
	if err != nil {
		t.Close()
		return fmt.Errorf("NETCONF hello read failed: %w", err)
	}

	// Parse server capabilities (informational).
	var hello struct {
		SessionID    int      `xml:"session-id"`
		Capabilities []string `xml:"capabilities>capability"`
	}

	// Strip the delimiter for parsing.
	helloXML := bytes.TrimSuffix(serverHello, []byte(netconfDelim))
	if err := xml.Unmarshal(bytes.TrimSpace(helloXML), &hello); err != nil {
		c.logger.Warn("NETCONF hello parse failed (continuing)", "err", err)
	} else {
		c.logger.Info("NETCONF session established",
			"addr", c.addr,
			"session_id", hello.SessionID,
			"capabilities", len(hello.Capabilities),
		)
	}

	// Send client hello.
	clientHello := []byte(xml.Header + `<hello xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <capabilities>
    <capability>urn:ietf:params:netconf:base:1.0</capability>
  </capabilities>
</hello>` + netconfDelim)

	if err := t.writeMessage(clientHello); err != nil {
		t.Close()
		return fmt.Errorf("NETCONF client hello failed: %w", err)
	}

	c.transport = t
	return nil
}

// Get sends a NETCONF <get> RPC with the given subtree filter and returns
// the raw XML content of the <data> element in the reply.
func (c *Client) Get(filter string) ([]byte, error) {
	rpc := fmt.Sprintf(`<get><filter type="subtree">%s</filter></get>`, filter)
	return c.rpc(rpc)
}

// GetConfig sends a NETCONF <get-config> RPC for the running datastore
// with the given subtree filter and returns the raw XML content of <data>.
func (c *Client) GetConfig(filter string) ([]byte, error) {
	rpc := fmt.Sprintf(`<get-config><source><running/></source><filter type="subtree">%s</filter></get-config>`, filter)
	return c.rpc(rpc)
}

// Close sends a NETCONF <close-session> RPC and closes the transport.
func (c *Client) Close() error {
	if c.transport == nil {
		return nil
	}
	// Best-effort close-session.
	closeRPC := c.wrapRPC("<close-session/>")
	_, _ = c.transport.Send(closeRPC)
	return c.transport.Close()
}

// rpc wraps the operation in an <rpc> envelope, sends it, and extracts the <data> content.
func (c *Client) rpc(operation string) ([]byte, error) {
	if c.transport == nil {
		return nil, fmt.Errorf("NETCONF: not connected")
	}

	msg := c.wrapRPC(operation)
	reply, err := c.transport.Send(msg)
	if err != nil {
		return nil, fmt.Errorf("NETCONF RPC failed: %w", err)
	}

	// Check for rpc-error in the reply.
	if bytes.Contains(reply, []byte("<rpc-error")) {
		return nil, parseRPCError(reply)
	}

	// Extract <data>...</data> content.
	data := extractDataContent(reply)
	return data, nil
}

// wrapRPC wraps an operation in the NETCONF <rpc> envelope with auto-incrementing message-id.
func (c *Client) wrapRPC(operation string) []byte {
	c.mu.Lock()
	c.msgID++
	id := c.msgID
	c.mu.Unlock()

	return []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><rpc xmlns="urn:ietf:params:xml:ns:netconf:base:1.0" message-id="%d">%s</rpc>%s`, id, operation, netconfDelim))
}

// extractDataContent extracts the inner content of the <data> element from an rpc-reply.
func extractDataContent(reply []byte) []byte {
	start := bytes.Index(reply, []byte("<data"))
	if start == -1 {
		return reply
	}
	// Find the end of the opening <data...> tag.
	tagEnd := bytes.Index(reply[start:], []byte(">"))
	if tagEnd == -1 {
		return reply
	}
	contentStart := start + tagEnd + 1

	end := bytes.LastIndex(reply, []byte("</data>"))
	if end == -1 || end < contentStart {
		return reply
	}

	return reply[contentStart:end]
}

// rpcError represents a NETCONF rpc-error element.
type rpcError struct {
	Type     string `xml:"error-type"`
	Tag      string `xml:"error-tag"`
	Severity string `xml:"error-severity"`
	Message  string `xml:"error-message"`
}

// parseRPCError extracts error details from an rpc-reply containing rpc-error.
func parseRPCError(reply []byte) error {
	var rpcReply struct {
		Errors []rpcError `xml:"rpc-error"`
	}
	clean := bytes.TrimSuffix(reply, []byte(netconfDelim))
	if err := xml.Unmarshal(bytes.TrimSpace(clean), &rpcReply); err != nil {
		return fmt.Errorf("NETCONF RPC error (unparseable): %s", truncateBytes(reply, 200))
	}
	if len(rpcReply.Errors) == 0 {
		return fmt.Errorf("NETCONF RPC error: %s", truncateBytes(reply, 200))
	}
	e := rpcReply.Errors[0]
	return fmt.Errorf("NETCONF RPC error [%s/%s]: %s", e.Type, e.Tag, e.Message)
}

// sshTransport implements Transport over an SSH NETCONF session using NETCONF 1.0 framing.
type sshTransport struct {
	conn    *ssh.Client
	session *ssh.Session
	stdin   io.WriteCloser
	stdout  io.Reader
	mu      sync.Mutex
}

func (t *sshTransport) Send(rpc []byte) ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if err := t.writeMessage(rpc); err != nil {
		return nil, err
	}
	reply, err := t.readMessage()
	if err != nil {
		return nil, err
	}
	// Strip trailing delimiter for caller convenience.
	return bytes.TrimSuffix(reply, []byte(netconfDelim)), nil
}

func (t *sshTransport) Close() error {
	t.stdin.Close()
	t.session.Close()
	return t.conn.Close()
}

func (t *sshTransport) writeMessage(msg []byte) error {
	_, err := t.stdin.Write(msg)
	return err
}

// readMessage reads from stdout until the NETCONF 1.0 delimiter ]]>]]> is found.
func (t *sshTransport) readMessage() ([]byte, error) {
	var buf bytes.Buffer
	tmp := make([]byte, 4096)
	delim := []byte(netconfDelim)

	// Set a read deadline via the underlying connection if possible.
	if tc, ok := t.conn.Conn.(net.Conn); ok {
		tc.SetReadDeadline(time.Now().Add(sshTimeout))
		defer tc.SetReadDeadline(time.Time{})
	}

	for {
		n, err := t.stdout.Read(tmp)
		if n > 0 {
			buf.Write(tmp[:n])
			if buf.Len() > maxReplyBytes {
				return nil, fmt.Errorf("NETCONF reply exceeds %d bytes", maxReplyBytes)
			}
			if bytes.Contains(buf.Bytes(), delim) {
				return buf.Bytes(), nil
			}
		}
		if err != nil {
			if err == io.EOF && buf.Len() > 0 {
				return buf.Bytes(), nil
			}
			return nil, fmt.Errorf("NETCONF read failed: %w", err)
		}
	}
}

// truncateBytes returns the first n bytes as a string for error messages.
func truncateBytes(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n]) + "..."
	}
	return string(b)
}
