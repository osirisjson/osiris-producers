// credentials.go - Credential-file loading
// for Cisco OSIRIS JSON Producer.
// Replaces (present in the initial release) plaintext -p/--password CLI
// flags (visible via ps and shell history) with a JSON file the caller
// points at via --secrets-file (apic/iosxe/nxos and vmanage all use
// this same flag name, aligning with the --secrets-file convention
// already established).
// Loading enforces that the file cannot be read back by anyone but the
// user who created it: no symlinks, no group/other permission bits, and
// (where the platform exposes ownership) no owner other than the
// current user. See credentials_unix.go and credentials_other.go for
// the platform-specific half of that check.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package run

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
)

// CredentialFile is the on-disk JSON shape read by every cisco producer
// --secrets-file (apic/iosxe/nxos via LoadCredentialFile below, vmanage
// via its own ParseFlags). Two shapes are accepted, distinguished by
// whether Default or Rules is present (either one alone is enough to
// select the rules shape a file with only "default" and no "rules"
// array at all is valid and common, e.g. a batch that shares one login
// across every target but was authored using the rules shape's
// wrapper instead of the flat shape's bare fields):
//
//   - flat (the original single host target shape):
//     {host, username, password} one credential set used for a
//     single target, or shared by every target in a batch.
//     Any field left out still falls back to its own flag or an
//     interactive prompt, so a partially-filled file is fine.
//     See templates.go CredentialFileTemplate for the skeleton
//     document "template --generate" writes.
//   - rules (for a batch against devices that do not all share one
//     login): {"default": {username, password}, "rules": [{"hosts":
//     "...", "username", "password"}, ...]}. "rules" may be omitted
//     entirely when every target shares the "default" credentials.
//     Host/Username/Password at the top level are ignored whenever
//     Default is set or Rules is non-empty. There is deliberately no
//     per-rule port: the CSV batch file's own port column already owns
//     that, and letting a second file also claim it would give a
//     target's port two disagreeing sources of truth.
//
// Call ResolveForHost to get the right username/password for a given
// target regardless of which shape the file uses.
type CredentialFile struct {
	Host     string `json:"host,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`

	Default *CredentialEntry `json:"default,omitempty"`
	Rules   []CredentialRule `json:"rules,omitempty"`
}

// CredentialEntry is a bare {username, password} pair, used as the
// rules shape's "default" fallback (a default has no host pattern of
// its own, so it does not embed CredentialRule's Hosts field).
type CredentialEntry struct {
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

// CredentialRule maps a host pattern to the credentials a matching
// target should use. Hosts is a comma-separated list where each token
// is either an exact host/IP/hostname (case-insensitive) or a CIDR
// block matched against the target's IP a hostname target never matches
// a CIDR token, since it has no address to test until DNS resolution,
// which this deliberately does not perform (rule matching must stay a
// pure string/IP comparison, not a network call).
type CredentialRule struct {
	Hosts string `json:"hosts"`
	CredentialEntry
}

// ResolveForHost returns the username/password this credential file
// supplies for host (a bare host/IP with no port, as returned by
// ParseHostPort). Nil-safe: a nil *CredentialFile (no --secrets-file
// given) resolves to "", "".
//
// The file is using the rules shape whenever Default is set or Rules
// is non-empty either alone is enough, so a file with only "default"
// and no "rules" array at all (a batch that shares one login, authored
// using the rules wrapper) is handled correctly, not misread as the
// flat shape with empty fields. When neither is set (the flat shape,
// including "no --secrets-file at all"), host is ignored and the
// top-level Username/Password are returned unconditionally.
// Otherwise, rules are tried in order and the first match wins; a
// target matching no rule falls back to Default (or to ", "" if there
// is no Default), never to the top-level Username/Password (which are
// documented as ignored once Default/Rules is used, so a rule file with
// both cannot silently apply the wrong one to an unmatched host).
func (cf *CredentialFile) ResolveForHost(host string) (username, password string) {
	if cf == nil {
		return "", ""
	}
	if cf.Default == nil && len(cf.Rules) == 0 {
		return cf.Username, cf.Password
	}
	for _, rule := range cf.Rules {
		if hostMatches(host, rule.Hosts) {
			return rule.Username, rule.Password
		}
	}
	if cf.Default != nil {
		return cf.Default.Username, cf.Default.Password
	}
	return "", ""
}

// hostMatches reports whether host matches any comma-separated token
// in pattern. A token containing "/" is parsed as a CIDR block and
// matched against host as an IP address (a non-IP host, or a
// malformed CIDR token, simply never matches that token); any other
// token is compared as an exact, case-insensitive string.
func hostMatches(host, pattern string) bool {
	host = strings.TrimSpace(host)
	for _, tok := range strings.Split(pattern, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		if strings.Contains(tok, "/") {
			_, cidr, err := net.ParseCIDR(tok)
			if err != nil {
				continue
			}
			if ip := net.ParseIP(host); ip != nil && cidr.Contains(ip) {
				return true
			}
			continue
		}
		if strings.EqualFold(tok, host) {
			return true
		}
	}
	return false
}

// LoadCredentialFile reads and validates a --secrets-file. It
// refuses to read the file at all unless every one of these holds:
//
//   - the path is not a symlink (checked via Lstat, before ever
//     following it);
//   - the target is a regular file, not a device, pipe, or directory;
//   - the file's permission bits grant no access to group or other
//     (platform-specific, see checkCredentialFileOwnerAndPerms);
//   - the file is owned by the user running this process
//     (platform-specific, same function).
//
// A file that fails any check is never opened for reading past Lstat,
// so its contents cannot leak through an error message either.
func LoadCredentialFile(path string) (*CredentialFile, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("credentials file %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("credentials file %q: refusing to follow a symlink", path)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("credentials file %q: not a regular file", path)
	}
	if err := checkCredentialFileOwnerAndPerms(path, info); err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("credentials file %q: %w", path, err)
	}

	var cf CredentialFile
	if err := json.Unmarshal(data, &cf); err != nil {
		return nil, fmt.Errorf("credentials file %q: invalid JSON: %w", path, err)
	}
	return &cf, nil
}
