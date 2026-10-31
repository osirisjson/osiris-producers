// hostkey_test.go - Unit tests for SSH host-key verification. All keys
// and addresses are generated or use RFC 5737 documentation ranges.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package iosxe

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// genHostKey returns a fresh ed25519 SSH public key for test fixtures.
func genHostKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generating ed25519 key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("wrapping signer: %v", err)
	}
	return signer.PublicKey()
}

// writeKnownHosts creates $HOME/.ssh/known_hosts containing entries for
// the given host address / key pairs and points HOME at a temp dir.
func writeKnownHosts(t *testing.T, entries map[string]ssh.PublicKey) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0700); err != nil {
		t.Fatalf("mkdir .ssh: %v", err)
	}
	var b strings.Builder
	for addr, key := range entries {
		b.WriteString(knownhosts.Line([]string{addr}, key))
		b.WriteString("\n")
	}
	if err := os.WriteFile(filepath.Join(home, ".ssh", "known_hosts"), []byte(b.String()), 0600); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}
}

func TestHostKeyCallback_InsecureAcceptsAnyKey(t *testing.T) {
	c := &Client{addr: "192.0.2.10:830", insecure: true, logger: testLogger()}
	cb, err := c.hostKeyCallback()
	if err != nil {
		t.Fatalf("hostKeyCallback: %v", err)
	}
	remote := &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 830}
	if err := cb("192.0.2.10:830", remote, genHostKey(t)); err != nil {
		t.Errorf("insecure callback rejected a key: %v", err)
	}
}

func TestHostKeyCallback_MissingKnownHostsFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // no .ssh/known_hosts inside.
	c := &Client{addr: "192.0.2.10:830", logger: testLogger()}
	_, err := c.hostKeyCallback()
	if err == nil {
		t.Fatal("expected an error when known_hosts is absent")
	}
	if !strings.Contains(err.Error(), "known_hosts") || !strings.Contains(err.Error(), "--insecure") {
		t.Errorf("error should name known_hosts and the --insecure escape hatch: %v", err)
	}
}

func TestHostKeyCallback_KnownHostsMatchAndMismatch(t *testing.T) {
	good := genHostKey(t)
	other := genHostKey(t)
	writeKnownHosts(t, map[string]ssh.PublicKey{"192.0.2.10:830": good})

	c := &Client{addr: "192.0.2.10:830", logger: testLogger()}
	cb, err := c.hostKeyCallback()
	if err != nil {
		t.Fatalf("hostKeyCallback: %v", err)
	}
	remote := &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 830}

	if err := cb("192.0.2.10:830", remote, good); err != nil {
		t.Errorf("recorded key should be accepted: %v", err)
	}

	err = cb("192.0.2.10:830", remote, other)
	if err == nil {
		t.Fatal("a different key for a recorded host must be rejected")
	}
	var ke *knownhosts.KeyError
	if !errors.As(err, &ke) || len(ke.Want) == 0 {
		t.Errorf("expected a knownhosts.KeyError with Want set (changed key), got: %v", err)
	}
}

func TestHostKeyCallback_UnknownHostReportsKeyError(t *testing.T) {
	writeKnownHosts(t, map[string]ssh.PublicKey{"192.0.2.10:830": genHostKey(t)})

	c := &Client{addr: "198.51.100.20:830", logger: testLogger()}
	cb, err := c.hostKeyCallback()
	if err != nil {
		t.Fatalf("hostKeyCallback: %v", err)
	}
	remote := &net.TCPAddr{IP: net.ParseIP("198.51.100.20"), Port: 830}

	err = cb("198.51.100.20:830", remote, genHostKey(t))
	if err == nil {
		t.Fatal("an unrecorded host must be rejected")
	}
	var ke *knownhosts.KeyError
	if !errors.As(err, &ke) || len(ke.Want) != 0 {
		t.Errorf("expected a knownhosts.KeyError with empty Want (unknown host), got: %v", err)
	}
}

func TestWrapHandshakeErr(t *testing.T) {
	c := &Client{addr: "192.0.2.10:830", logger: testLogger()}

	unknown := c.wrapHandshakeErr(&knownhosts.KeyError{})
	if !strings.Contains(unknown.Error(), "not in ~/.ssh/known_hosts") {
		t.Errorf("unknown-host message: %v", unknown)
	}
	if !strings.Contains(unknown.Error(), "--insecure") {
		t.Errorf("unknown-host message should mention --insecure: %v", unknown)
	}

	changed := c.wrapHandshakeErr(&knownhosts.KeyError{Want: []knownhosts.KnownKey{{}}})
	if !strings.Contains(changed.Error(), "mismatch") {
		t.Errorf("changed-key message should say mismatch: %v", changed)
	}
	if !strings.Contains(changed.Error(), "ssh-keygen -R 192.0.2.10") {
		t.Errorf("changed-key message should carry an ssh-keygen -R hint keyed on host only: %v", changed)
	}

	plain := c.wrapHandshakeErr(errors.New("kex failed"))
	if !strings.Contains(plain.Error(), "handshake failed") {
		t.Errorf("non-KeyError should fall through to a generic handshake error: %v", plain)
	}
}
