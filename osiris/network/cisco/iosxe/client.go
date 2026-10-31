// client.go - NETCONF over SSH client for the Cisco IOS-XE producer.
// Implements NETCONF (RFC 6241) over an SSH subsystem (RFC 6242), with
// a Transport interface that enables test injection without real SSH.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package iosxe

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"go.osirisjson.org/producers/osiris/network/cisco/run"
)

const (
	defaultPort = 830
	dialTimeout = 15 * time.Second // TCP connect budget.
	sshTimeout  = 30 * time.Second // SSH handshake, each RPC and close-session budget.
)

// clientHello is the <hello> this client sends. It advertises both base
// versions so the server may select chunked framing.
const clientHello = xml.Header + `<hello xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <capabilities>
    <capability>` + capBase10 + `</capability>
    <capability>` + capBase11 + `</capability>
  </capabilities>
</hello>`

// Transport abstracts the NETCONF message exchange, enabling mock injection for tests.
type Transport interface {
	Send(rpc []byte) ([]byte, error)
	Close() error
}

// errSessionUnusable is returned for every RPC after one that left the
// session's request/reply pairing unknown: a transport failure (timeout,
// oversize or malformed frame) or a reply with the wrong message-id.
var errSessionUnusable = errors.New("NETCONF session unusable after an earlier failure")

// errNETCONFUnreachable is returned when the TCP connection to the
// NETCONF port cannot be established (timeout or refusal), typically
// because netconf-yang is disabled or an ACL blocks the port. It is the
// only connection failure on which --transport auto tries RESTCONF.
var errNETCONFUnreachable = errors.New("NETCONF port unreachable (is netconf-yang enabled on the device and the port allowed?)")

// Client is a NETCONF client for IOS-XE devices. Requests are strictly
// sequential: each reply must carry the message-id of the request just
// sent, otherwise the session is marked unusable.
type Client struct {
	transport Transport
	msgID     int
	broken    error // non-nil once the session can no longer pair replies.
	mu        sync.Mutex
	logger    *slog.Logger
	addr      string
	insecure  bool
}

// NewClient creates a NETCONF client targeting the given address.
// It does not connect; call Connect to establish the SSH session.
// When insecure is true, SSH host-key verification is skipped (the
// --insecure flag); otherwise the server key is checked against user
// ~/.ssh/known_hosts.
func NewClient(target run.TargetConfig, insecure bool, logger *slog.Logger) *Client {
	addr := run.ResolveAddr(target, defaultPort)
	return &Client{
		addr:     addr,
		insecure: insecure,
		logger:   logger,
	}
}

// Connect establishes an SSH connection, requests the netconf subsystem,
// and exchanges NETCONF <hello> capabilities. The TCP dial and SSH
// handshake are each bounded by their own deadline.
func (c *Client) Connect(username, password string) error {
	// Dial first, so an unreachable port is reported as such (and lets
	// --transport auto fall back) even when no host key is on record.
	// Host-key verification still happens before any credential is sent.
	netConn, err := net.DialTimeout("tcp", c.addr, dialTimeout)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", errNETCONFUnreachable, c.addr, err)
	}

	hostKey, err := c.hostKeyCallback()
	if err != nil {
		netConn.Close()
		return err
	}

	config := &ssh.ClientConfig{
		User: username,
		Auth: []ssh.AuthMethod{
			ssh.Password(password),
		},
		HostKeyCallback: hostKey,
	}

	// Bound the SSH handshake (including the host-key callback) with a
	// deadline on the raw connection, then clear it for steady state.
	_ = netConn.SetDeadline(time.Now().Add(sshTimeout))
	sshConn, chans, reqs, err := ssh.NewClientConn(netConn, c.addr, config)
	if err != nil {
		netConn.Close()
		return c.wrapHandshakeErr(err)
	}
	_ = netConn.SetDeadline(time.Time{})
	conn := ssh.NewClient(sshConn, chans, reqs)

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
		framer:  newFramer(stdout, stdin),
		netConn: netConn,
	}

	// Exchange <hello> messages, bounded by the handshake deadline.
	_ = netConn.SetDeadline(time.Now().Add(sshTimeout))
	hello, err := exchangeHello(t.framer)
	_ = netConn.SetDeadline(time.Time{})
	if err != nil {
		t.Close()
		return err
	}

	base := "1.0"
	if t.framer.chunked {
		base = "1.1"
	}
	c.logger.Info("NETCONF session established",
		"addr", c.addr,
		"session_id", hello.SessionID,
		"capabilities", len(hello.Capabilities),
		"netconf_base", base,
	)

	c.transport = t
	return nil
}

// serverHello is the decoded server <hello> message.
type serverHello struct {
	SessionID    int      `xml:"session-id"`
	Capabilities []string `xml:"capabilities>capability"`
}

// exchangeHello reads the server <hello>, sends the client <hello> and
// switches f to chunked framing when both peers support base:1.1. Both
// hellos always use end-of-message framing (RFC 6242 section 4.1). A
// server hello that cannot be decoded is fatal, because the framing for
// the rest of the session depends on its capability list.
func exchangeHello(f *framer) (*serverHello, error) {
	raw, err := f.readMessage()
	if err != nil {
		return nil, fmt.Errorf("NETCONF hello read failed: %w", err)
	}
	var hello serverHello
	if err := xml.Unmarshal(bytes.TrimSpace(raw), &hello); err != nil {
		return nil, fmt.Errorf("NETCONF server hello is not valid XML: %w", err)
	}
	chunked, err := negotiateFraming(hello.Capabilities)
	if err != nil {
		return nil, err
	}
	if err := f.writeMessage([]byte(clientHello)); err != nil {
		return nil, fmt.Errorf("NETCONF client hello failed: %w", err)
	}
	f.chunked = chunked
	return &hello, nil
}

// hostKeyCallback returns the SSH host-key verification callback. With
// --insecure it logs a prominent warning and returns a callback that
// accepts any key. Otherwise it verifies against ~/.ssh/known_hosts and
// fails if that file is absent, telling the operator how to record the
// key or bypass the check.
func (c *Client) hostKeyCallback() (ssh.HostKeyCallback, error) {
	if c.insecure {
		c.logger.Warn("SSH host-key verification disabled for NETCONF connection (--insecure)", "host", c.addr)
		return ssh.InsecureIgnoreHostKey(), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("cannot locate home directory for SSH known_hosts lookup: %w", err)
	}
	path := filepath.Join(home, ".ssh", "known_hosts")
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf(
			"SSH known_hosts file %s is not readable (%v): connect once with ssh(1) to record the host key, or pass --insecure to skip verification",
			path, err)
	}

	cb, err := knownhosts.New(path)
	if err != nil {
		return nil, fmt.Errorf("parsing SSH known_hosts %s: %w", path, err)
	}
	return cb, nil
}

// wrapHandshakeErr turns a raw SSH handshake failure into an actionable
// message, distinguishing an unknown host key from a changed one (a
// possible man-in-the-middle or a legitimately re-keyed device).
func (c *Client) wrapHandshakeErr(err error) error {
	var ke *knownhosts.KeyError
	if errors.As(err, &ke) {
		if len(ke.Want) > 0 {
			return fmt.Errorf(
				"SSH host-key mismatch for %s: the key the server presented does not match ~/.ssh/known_hosts (possible man-in-the-middle, or the device was re-keyed); run `ssh-keygen -R %s` after verifying the new key, then retry",
				c.addr, hostOnly(c.addr))
		}
		return fmt.Errorf(
			"SSH host-key for %s is not in ~/.ssh/known_hosts: connect once with ssh(1) to record it, or pass --insecure to skip verification",
			c.addr)
	}
	return fmt.Errorf("NETCONF SSH handshake failed (%s): %w", c.addr, err)
}

// hostOnly strips a trailing :port from an address for display in an
// ssh-keygen -R hint (that command keys on host, not host:port).
func hostOnly(addr string) string {
	if h, _, err := net.SplitHostPort(addr); err == nil {
		return h
	}
	return addr
}

// Get sends a NETCONF <get> RPC with the given subtree filter and
// returns the raw XML content of the <data> element in the reply.
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
// close-session is skipped when the session is already unusable, since
// its reply could not be paired and would only wait out the deadline.
func (c *Client) Close() error {
	if c.transport == nil {
		return nil
	}
	c.mu.Lock()
	broken := c.broken
	c.mu.Unlock()
	if broken == nil {
		closeRPC, _ := c.wrapRPC("<close-session/>")
		_, _ = c.transport.Send(closeRPC)
	}
	return c.transport.Close()
}

// rpc wraps the operation in an <rpc> envelope, sends it, validates the
// reply against the request's message-id and returns the content of
// its <data> element. Device warnings are logged; device errors are
// returned as classified rpcError values.
func (c *Client) rpc(operation string) ([]byte, error) {
	if c.transport == nil {
		return nil, fmt.Errorf("NETCONF: not connected")
	}
	c.mu.Lock()
	broken := c.broken
	c.mu.Unlock()
	if broken != nil {
		return nil, fmt.Errorf("%w: %v", errSessionUnusable, broken)
	}

	msg, id := c.wrapRPC(operation)
	reply, err := c.transport.Send(msg)
	if err != nil {
		c.markBroken(err)
		return nil, fmt.Errorf("NETCONF RPC failed: %w", err)
	}

	data, warnings, err := parseRPCReply(reply, id)
	for i := range warnings {
		c.logger.Warn("NETCONF device warning", "addr", c.addr, "warning", warnings[i].Error())
	}
	if err != nil {
		if errors.Is(err, errMessageIDMismatch) {
			c.markBroken(err)
		}
		return nil, err
	}
	return data, nil
}

// markBroken records the first failure that leaves the session unable
// to pair replies with requests.
func (c *Client) markBroken(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.broken == nil {
		c.broken = err
	}
}

// wrapRPC wraps an operation in the NETCONF <rpc> envelope with the
// next message-id, returning the message and that id.
// Framing is added by the transport.
func (c *Client) wrapRPC(operation string) ([]byte, string) {
	c.mu.Lock()
	c.msgID++
	id := strconv.Itoa(c.msgID)
	c.mu.Unlock()

	msg := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><rpc xmlns="%s" message-id="%s">%s</rpc>`, ncBaseNS, id, operation)
	return []byte(msg), id
}

// sshTransport implements Transport over an SSH NETCONF session, using
// the framing negotiated during the <hello> exchange.
type sshTransport struct {
	conn    *ssh.Client
	session *ssh.Session
	stdin   io.WriteCloser
	framer  *framer
	netConn net.Conn // raw connection, used to apply per-exchange deadlines.
	mu      sync.Mutex
}

// Send writes one framed RPC and returns the next framed reply with its
// framing removed. The whole exchange is bounded by sshTimeout.
// When the deadline fires, the SSH layer closes the channel and the
// read surfaces as an end of stream, so any failure after the deadline
// is reported as os.ErrDeadlineExceeded.
func (t *sshTransport) Send(rpc []byte) ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	deadline := time.Now().Add(sshTimeout)
	if t.netConn != nil {
		_ = t.netConn.SetDeadline(deadline)
		defer t.netConn.SetDeadline(time.Time{})
	}

	reply, err := t.exchange(rpc)
	if err != nil && !time.Now().Before(deadline) {
		return nil, fmt.Errorf("%w after %s: %w", os.ErrDeadlineExceeded, sshTimeout, err)
	}
	return reply, err
}

func (t *sshTransport) exchange(rpc []byte) ([]byte, error) {
	if err := t.framer.writeMessage(rpc); err != nil {
		return nil, err
	}
	return t.framer.readMessage()
}

func (t *sshTransport) Close() error {
	t.stdin.Close()
	t.session.Close()
	return t.conn.Close()
}
