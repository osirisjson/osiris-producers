// transform_security.go - Audit-purpose security posture for the Cisco
// IOS-XE OSIRIS JSON Producer.
//
// The device's own security configuration is vendor-specific device
// configuration, so it is emitted in the device resource's osiris.cisco
// extension: AAA method lists and servers (aaa), local accounts and the
// enable credential (local_accounts, enable_mode), management-plane
// services and VTY lines (management_services), DNS (dns), SNMP posture
// (snmp), syslog (logging), NTP (ntp), PKI trustpoints and IKEv2/IPsec
// proposals (crypto) and the control-plane policing policy
// (control_plane_policy).
//
// Two kinds of security object are emitted as resources of their own,
// each contained by the device (contains.logical): every access list as
// an osiris.cisco.acl resource with its entries and its interface and
// VTY bindings (OSIRIS JSON has no standard type for a device-local
// rule list) and every installed certificate as a
// network.security.certificate resource.
//
// Credential material is never emitted, because it is never decoded
// (see dto_security.go): an account or the enable credential reports
// only how its credential is stored (for example "secret-type-9"), an
// SNMP community reports its permission and access list but never the
// community string and NTP reports key numbers but never key values.
// Property and extension keys avoid the SDK's sensitive-key patterns,
// so the document passes fail-closed secret scanning.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package iosxe

import (
	"strconv"
	"strings"
	"time"

	"go.osirisjson.org/producers/pkg/sdk"
)

// putStr sets m[k] to v when v is not blank.
func putStr(m map[string]any, k, v string) {
	if v = strings.TrimSpace(v); v != "" {
		m[k] = v
	}
}

// putInt sets m[k] to v as an integer when v parses as one.
func putInt(m map[string]any, k, v string) {
	if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
		m[k] = n
	}
}

// putBool sets m[k] when v parses as a YANG boolean.
func putBool(m map[string]any, k, v string) {
	if b, err := strconv.ParseBool(strings.TrimSpace(v)); err == nil {
		m[k] = b
	}
}

// firstOf returns the first non-blank value.
func firstOf(vs ...string) string {
	for _, v := range vs {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

// AAA Methods.
// aaaMethodLeaves are the method elements of an AAA method list that
// carry no value: their element name is the method.
var aaaMethodLeaves = map[string]bool{
	"local": true, "local-case": true, "none": true, "enable": true,
	"line": true, "if-authenticated": true, "krb5": true,
	"krb5-telnet": true, "krb5-instance": true, "radius": true,
	"tacacs": true, "logger": true, "broadcast": true,
}

// aaaMethods collects, in configured order, the methods of one method
// list entry: "group <name>" for a server group and the element name
// for every other method. Accounting entries also report their record
// type (start-stop, stop-only, or the action-type value).
func aaaMethods(n xmlNode, methods *[]string, record *string) {
	for _, c := range n.Nodes {
		name := c.XMLName.Local
		switch name {
		case "name", "level", "list-name":
			continue
		case "group":
			if t := strings.TrimSpace(c.Text); t != "" && len(c.Nodes) == 0 {
				*methods = append(*methods, "group "+t)
				continue
			}
		case "action-type":
			*record = strings.TrimSpace(c.Text)
			continue
		case "start-stop", "stop-only":
			*record = name
		}
		if len(c.Nodes) == 0 {
			if aaaMethodLeaves[name] {
				*methods = append(*methods, name)
			}
			continue
		}
		aaaMethods(c, methods, record)
	}
}

// methodLists converts the children of aaa authentication,
// authorization or accounting into method-list rows of the given kind.
func methodLists(kind string, entries []xmlNode) []map[string]any {
	var out []map[string]any
	for _, e := range entries {
		service := e.XMLName.Local
		name := firstOf(e.text("name"), e.text("list-name"))
		if name == "" {
			if d, ok := e.child("default"); ok {
				e, name = d, "default"
			}
		}
		var methods []string
		record := ""
		aaaMethods(e, &methods, &record)
		if len(methods) == 0 && record == "" {
			continue
		}
		row := map[string]any{"type": kind, "service": service}
		putStr(row, "name", name)
		putInt(row, "level", e.text("level"))
		if len(methods) > 0 {
			row["methods"] = methods
		}
		putStr(row, "record_type", record)
		out = append(out, row)
	}
	return out
}

// TransformAAA converts the AAA configuration into the aaa object of
// the device's osiris.cisco extension: whether AAA new-model is
// enabled, the authentication, authorization and accounting method
// lists, the TACACS+ and RADIUS server groups and the configured
// servers. Server keys are never decoded.
func TransformAAA(a nativeAAA) map[string]any {
	out := map[string]any{}
	if a.AAA.NewModel != nil {
		out["new_model"] = true
	}
	var lists []map[string]any
	lists = append(lists, methodLists("authentication", a.AAA.Authentication.Entries)...)
	lists = append(lists, methodLists("authorization", a.AAA.Authorization.Entries)...)
	lists = append(lists, methodLists("accounting", a.AAA.Accounting.Entries)...)
	if len(lists) > 0 {
		out["method_lists"] = lists
	}

	var groups []map[string]any
	addGroups := func(protocol string, gs []aaaServerGroup) {
		for _, g := range gs {
			row := map[string]any{"protocol": protocol}
			putStr(row, "name", g.Name)
			var members []string
			for _, m := range g.Members {
				if v := strings.TrimSpace(m.Name); v != "" {
					members = append(members, v)
				}
			}
			if len(members) > 0 {
				row["servers"] = members
			}
			var private []map[string]any
			for _, p := range g.Private {
				s := map[string]any{}
				putStr(s, "address", p.Name)
				putInt(s, "port", firstOf(p.Port, p.AuthPort))
				putInt(s, "accounting_port", p.AcctPort)
				if len(s) > 0 {
					private = append(private, s)
				}
			}
			if len(private) > 0 {
				row["private_servers"] = private
			}
			groups = append(groups, row)
		}
	}
	addGroups("tacacs+", a.AAA.Groups.Tacacs)
	addGroups("radius", a.AAA.Groups.Radius)
	if len(groups) > 0 {
		out["server_groups"] = groups
	}

	var tacacs []map[string]any
	for _, s := range a.Tacacs {
		row := map[string]any{}
		putStr(row, "name", s.Name)
		putStr(row, "address", firstOf(s.IPv4, s.IPv6, s.FQDN))
		putInt(row, "port", s.Port)
		putInt(row, "timeout_seconds", s.Timeout)
		if len(row) > 0 {
			tacacs = append(tacacs, row)
		}
	}
	if len(tacacs) > 0 {
		out["tacacs_servers"] = tacacs
	}

	var radius []map[string]any
	for _, s := range a.Radius {
		row := map[string]any{}
		putStr(row, "name", s.ID)
		putStr(row, "address", firstOf(s.IPv4, s.IPv6, s.FQDN))
		putInt(row, "port", s.AuthPort)
		putInt(row, "accounting_port", s.AcctPort)
		putInt(row, "timeout_seconds", s.Timeout)
		if len(row) > 0 {
			radius = append(radius, row)
		}
	}
	if len(radius) > 0 {
		out["radius_servers"] = radius
	}
	return out
}

// Accounts.
// credentialStorage names how a credential is stored: its kind
// ("secret" or "password") and, when configured, its IOS encryption
// type, e.g. "secret-type-9" or "password-type-7".
func credentialStorage(kind, encryption string) string {
	if e := strings.TrimSpace(encryption); e != "" {
		return kind + "-type-" + e
	}
	return kind
}

// TransformAccounts converts local accounts into rows of name,
// privilege level and credential storage and the enable credential
// into its storage and privilege level. No credential text is decoded.
func TransformAccounts(a nativeAccounts) ([]map[string]any, map[string]any) {
	var users []map[string]any
	for _, u := range a.Users {
		row := map[string]any{}
		putStr(row, "name", u.Name)
		putInt(row, "privilege", u.Privilege)
		switch {
		case u.Secret != nil:
			row["storage"] = credentialStorage("secret", u.Secret.Encryption)
		case u.Password != nil:
			row["storage"] = credentialStorage("password", u.Password.Encryption)
		}
		if len(row) > 0 {
			users = append(users, row)
		}
	}
	enable := map[string]any{}
	switch {
	case a.Enable.Secret != nil:
		enable["storage"] = credentialStorage("secret", a.Enable.Secret.Type)
		putInt(enable, "level", a.Enable.Secret.Level)
	case a.Enable.Password != nil:
		enable["storage"] = credentialStorage("password", a.Enable.Password.Type)
		putInt(enable, "level", a.Enable.Password.Level)
	}
	return users, enable
}

// Management plane.
// vtyTransport returns the configured VTY transport input protocols.
func vtyTransport(v vtyLine) []string {
	t := v.Transport
	switch {
	case t.All != nil:
		return []string{"all"}
	case t.None != nil:
		return []string{"none"}
	}
	var out []string
	for _, p := range t.Input {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		if b, err := strconv.ParseBool(strings.TrimSpace(t.SSH)); err == nil && b {
			out = []string{"ssh"}
		}
	}
	return out
}

// TransformManagement converts the management-plane services into the
// management_services object: the HTTP and HTTPS servers, SSH and SCP
// settings, whether type 7 password obfuscation is on and every VTY
// line range with its transport, access classes, exec timeout
// and login method.
func TransformManagement(m nativeManagement) map[string]any {
	out := map[string]any{}
	h := m.IP.HTTP
	putBool(out, "http_server", h.Server)
	putBool(out, "https_server", h.SecureServer)
	putStr(out, "https_trustpoint", h.SecureTrustpoint)
	putStr(out, "http_access_class", h.AccessClass)
	putInt(out, "http_max_connections", h.MaxConnections)
	s := m.IP.SSH
	putInt(out, "ssh_version", s.Version)
	putInt(out, "ssh_timeout_seconds", s.TimeOut)
	putInt(out, "ssh_login_retries", s.Retries)
	putStr(out, "ssh_source_interface", s.SourceInterface)
	if m.IP.SCPServer != nil {
		out["scp_server"] = true
	}
	if m.PasswordEncryption != nil {
		out["type7_config_obfuscation"] = true
	}

	var lines []map[string]any
	for _, v := range m.VTY {
		row := map[string]any{}
		putInt(row, "first", v.First)
		putInt(row, "last", v.Last)
		if t := vtyTransport(v); len(t) > 0 {
			row["transport_input"] = t
		}
		var classes []map[string]any
		for _, c := range v.AccessClass {
			ac := map[string]any{}
			putStr(ac, "direction", c.Direction)
			putStr(ac, "acl", c.ACL)
			if len(ac) > 0 {
				classes = append(classes, ac)
			}
		}
		if len(classes) > 0 {
			row["access_class"] = classes
		}
		minutes, mErr := strconv.Atoi(strings.TrimSpace(v.ExecTimeout.Minutes))
		seconds, sErr := strconv.Atoi(strings.TrimSpace(v.ExecTimeout.Seconds))
		if mErr == nil || sErr == nil {
			row["exec_timeout_seconds"] = minutes*60 + seconds
		}
		if v.LoginLocal != nil {
			row["login"] = "local"
		}
		putStr(row, "login_method_list", v.LoginList)
		if len(row) > 0 {
			lines = append(lines, row)
		}
	}
	if len(lines) > 0 {
		out["vty_lines"] = lines
	}
	return out
}

// TransformDNS converts the resolver configuration into the dns object:
// the domain name (global and per VRF), whether lookup is enabled and
// the name servers with their VRF.
func TransformDNS(d nativeDNS) map[string]any {
	out := map[string]any{}
	putStr(out, "domain_name", firstOf(d.Domain.Name, d.Domain.NoVRF))
	putBool(out, "lookup", d.Domain.Lookup)
	var vrfNames []map[string]any
	for _, v := range d.Domain.VRFNames {
		row := map[string]any{}
		putStr(row, "vrf", v.VRF)
		putStr(row, "domain_name", v.Name)
		if len(row) > 0 {
			vrfNames = append(vrfNames, row)
		}
	}
	if len(vrfNames) > 0 {
		out["vrf_domain_names"] = vrfNames
	}
	var servers []map[string]any
	add := func(vrf string, addrs ...[]string) {
		for _, list := range addrs {
			for _, a := range list {
				row := map[string]any{}
				putStr(row, "address", a)
				putStr(row, "vrf", vrf)
				if _, ok := row["address"]; ok {
					servers = append(servers, row)
				}
			}
		}
	}
	add("", d.NameServers.Ordered, d.NameServers.NoVRF)
	for _, v := range d.NameServers.VRFs {
		add(v.VRF, v.Ordered, v.List)
	}
	if len(servers) > 0 {
		out["name_servers"] = servers
	}
	return out
}

// TransformSNMP converts the SNMP agent configuration into the snmp
// object: each community's permission, view and access list (never the
// community string), the groups with their version and v3 security
// levels, the notification receivers and the location and contact.
func TransformSNMP(n nativeSNMP) map[string]any {
	s := n.SNMP
	out := map[string]any{}
	var communities []map[string]any
	for _, c := range s.Communities {
		row := map[string]any{}
		putStr(row, "permission", firstOf(c.Permission, "ro"))
		putStr(row, "view", c.View)
		putStr(row, "acl", c.ACL)
		communities = append(communities, row)
	}
	for _, c := range s.LegacyCommunities {
		row := map[string]any{}
		switch {
		case c.RW != nil:
			row["permission"] = "rw"
		default:
			row["permission"] = "ro"
		}
		putStr(row, "view", c.View)
		putStr(row, "acl", c.ACL)
		communities = append(communities, row)
	}
	if len(communities) > 0 {
		out["communities"] = communities
	}

	var groups []map[string]any
	for _, g := range s.Groups {
		row := map[string]any{}
		putStr(row, "name", g.ID)
		switch {
		case g.V3 != nil:
			row["version"] = "3"
		case g.V2c != nil:
			row["version"] = "2c"
		case g.V1 != nil:
			row["version"] = "1"
		}
		var levels []string
		if g.V3 != nil {
			for _, l := range g.V3.Levels {
				if l = strings.TrimSpace(l); l != "" {
					levels = append(levels, l)
				}
			}
		}
		if len(levels) > 0 {
			row["security_levels"] = levels
		}
		if len(row) > 0 {
			groups = append(groups, row)
		}
	}
	if len(groups) > 0 {
		out["groups"] = groups
	}

	var hosts []map[string]any
	seen := map[string]bool{}
	for _, list := range [][]snmpHost{s.Hosts.Community, s.Hosts.CommunityPort, s.Hosts.VRF, s.Hosts.VRFPort} {
		for _, h := range list {
			row := map[string]any{}
			putStr(row, "address", h.Address)
			putStr(row, "vrf", h.VRF)
			putStr(row, "version", h.Version)
			putStr(row, "security_level", h.SecurityLevel)
			if _, ok := row["address"]; !ok {
				continue
			}
			k := strings.Join([]string{h.Address, h.VRF, h.Version, h.SecurityLevel}, "|")
			if seen[k] {
				continue
			}
			seen[k] = true
			hosts = append(hosts, row)
		}
	}
	if len(hosts) > 0 {
		out["hosts"] = hosts
	}
	putStr(out, "location", s.Location)
	putStr(out, "contact", s.Contact)
	return out
}

// TransformLogging converts the syslog configuration into the logging
// object: the remote hosts with their VRF and transport, the trap and
// buffered severities, the buffer size and the source interface.
func TransformLogging(n nativeLogging) map[string]any {
	l := n.Logging
	out := map[string]any{}
	var hosts []map[string]any
	for _, list := range [][]logHost{l.IPv4, l.IPv4Xport, l.IPv4VRF, l.IPv4VRFX, l.IPv6, l.IPv6Xport, l.IPv6VRF, l.IPv6VRFX} {
		for _, h := range list {
			row := map[string]any{}
			putStr(row, "address", firstOf(h.IPv4, h.IPv6))
			if _, ok := row["address"]; !ok {
				continue
			}
			putStr(row, "vrf", h.VRF)
			switch {
			case h.TLS != nil:
				row["transport"] = "tls"
			case h.TCP != nil:
				row["transport"] = "tcp"
			case h.UDP != nil:
				row["transport"] = "udp"
			}
			hosts = append(hosts, row)
		}
	}
	if len(hosts) > 0 {
		out["hosts"] = hosts
	}
	putStr(out, "trap_severity", l.Trap)
	putInt(out, "buffered_size", l.Buffered.Size)
	putStr(out, "buffered_severity", l.Buffered.Severity)
	putStr(out, "source_interface", l.Source)
	return out
}

// TransformNTP converts the NTP configuration into the ntp object: the
// servers and peers (global and per VRF), whether source verification
// ("ntp authenticate") is enforced, the configured and trusted key
// numbers, the master stratum and the access groups. Key values are
// never decoded.
func TransformNTP(n nativeNTP) map[string]any {
	t := n.NTP
	out := map[string]any{}
	assoc := func(vrf string, as []ntpAssoc) []map[string]any {
		var rows []map[string]any
		for _, a := range as {
			row := map[string]any{}
			putStr(row, "address", a.Address)
			if _, ok := row["address"]; !ok {
				continue
			}
			putStr(row, "vrf", vrf)
			putStr(row, "source", a.Source)
			putInt(row, "key_id", a.Key)
			putInt(row, "version", a.Version)
			if a.Prefer != nil {
				row["prefer"] = true
			}
			rows = append(rows, row)
		}
		return rows
	}
	servers := assoc("", t.Servers)
	for _, v := range t.ServerVRFs {
		servers = append(servers, assoc(v.Name, v.Assoc)...)
	}
	if len(servers) > 0 {
		out["servers"] = servers
	}
	peers := assoc("", t.Peers)
	for _, v := range t.PeerVRFs {
		peers = append(peers, assoc(v.Name, v.Assoc)...)
	}
	if len(peers) > 0 {
		out["peers"] = peers
	}
	if t.Authenticate != nil {
		out["source_verification"] = true
	}
	numbers := func(rows []struct {
		Number string `xml:"number"`
	}) []int {
		var ns []int
		for _, r := range rows {
			if v, err := strconv.Atoi(strings.TrimSpace(r.Number)); err == nil {
				ns = append(ns, v)
			}
		}
		return ns
	}
	if ks := numbers(t.Keys); len(ks) > 0 {
		out["key_ids"] = ks
	}
	if ks := numbers(t.Trusted); len(ks) > 0 {
		out["trusted_key_ids"] = ks
	}
	putInt(out, "master_stratum", t.MasterStratum)

	var groups []map[string]any
	for _, g := range []struct{ kind, acl string }{
		{"peer", firstOf(t.AccessGroups.IPv4.Peer, t.AccessGroups.Peer)},
		{"serve", firstOf(t.AccessGroups.IPv4.Serve, t.AccessGroups.Serve)},
		{"serve-only", firstOf(t.AccessGroups.IPv4.ServeOnly, t.AccessGroups.ServeOnly)},
		{"query-only", firstOf(t.AccessGroups.IPv4.QueryOnly, t.AccessGroups.QueryOnly)},
	} {
		if g.acl != "" {
			groups = append(groups, map[string]any{"type": g.kind, "acl": g.acl})
		}
	}
	if len(groups) > 0 {
		out["access_groups"] = groups
	}
	return out
}

// childNames returns the element names of n's children: the selected
// options of a YANG container of empty leaves.
func childNames(n xmlNode) []string {
	var out []string
	for _, c := range n.Nodes {
		out = append(out, c.XMLName.Local)
	}
	return out
}

// TransformCrypto converts the crypto configuration into the crypto
// object: PKI trustpoints (enrollment method, revocation checking, key
// pair label, subject, hash, usage; never the enrollment URL or
// password), IKEv2 proposals and IPsec transform sets.
func TransformCrypto(c nativeCrypto) map[string]any {
	out := map[string]any{}
	var tps []map[string]any
	for _, tp := range c.Trustpoints {
		row := map[string]any{}
		putStr(row, "name", tp.ID)
		e, m := tp.Enrollment, tp.Enrollment.Method
		switch {
		case e.Selfsigned != nil || m.Selfsigned != nil:
			row["enrollment"] = "selfsigned"
		case strings.TrimSpace(e.URL) != "" || strings.TrimSpace(m.URL) != "":
			row["enrollment"] = "url"
		case e.Terminal != nil || m.Terminal != nil:
			row["enrollment"] = "terminal"
		case e.PKCS12 != nil || m.PKCS12 != nil:
			row["enrollment"] = "pkcs12"
		case strings.TrimSpace(e.Profile) != "" || strings.TrimSpace(m.Profile) != "":
			row["enrollment"] = "profile"
		}
		var rev []string
		for _, r := range tp.Revocation {
			if r = strings.TrimSpace(r); r != "" {
				rev = append(rev, r)
			}
		}
		if len(rev) > 0 {
			row["revocation_check"] = rev
		}
		putStr(row, "key_label", firstOf(tp.KeyLabel, tp.ECKeyPair))
		putStr(row, "subject_name", tp.SubjectName)
		putStr(row, "hash", tp.Hash)
		putStr(row, "usage", tp.Usage)
		if len(row) > 0 {
			tps = append(tps, row)
		}
	}
	if len(tps) > 0 {
		out["pki_trustpoints"] = tps
	}

	var props []map[string]any
	for _, p := range c.IKEv2Proposals {
		row := map[string]any{}
		putStr(row, "name", p.Name)
		for k, n := range map[string]xmlNode{"encryption": p.Encryption, "integrity": p.Integrity, "prf": p.PRF, "group": p.Group} {
			if v := childNames(n); len(v) > 0 {
				row[k] = v
			}
		}
		if len(row) > 0 {
			props = append(props, row)
		}
	}
	if len(props) > 0 {
		out["ikev2_proposals"] = props
	}

	var sets []map[string]any
	for _, t := range c.TransformSets {
		row := map[string]any{}
		putStr(row, "name", t.Tag)
		putStr(row, "esp", t.ESP)
		putInt(row, "key_bits", t.KeyBit)
		putStr(row, "esp_hmac", t.ESPHMAC)
		putStr(row, "ah_hmac", t.AHHMAC)
		switch {
		case t.Tunnel != nil:
			row["mode"] = "tunnel"
		case t.Transp != nil:
			row["mode"] = "transport"
		}
		if len(row) > 0 {
			sets = append(sets, row)
		}
	}
	if len(sets) > 0 {
		out["ipsec_transform_sets"] = sets
	}
	return out
}

// TransformControlPlane converts the control-plane service policies
// into the control_plane_policy object.
func TransformControlPlane(c nativeControlPlane) map[string]any {
	out := map[string]any{}
	putStr(out, "input", c.Input)
	putStr(out, "output", c.Output)
	return out
}

// Access lists.
// aclType values of osiris.cisco.acl resources.
const (
	aclStandard = "standard"
	aclExtended = "extended"
	aclIPv6     = "ipv6"
)

// aclFamily returns the address family segment of an ACL resource ID.
func aclFamily(aclType string) string {
	if aclType == aclIPv6 {
		return "ipv6"
	}
	return "ipv4"
}

// aclKey identifies an ACL by family and name, for binding lookup.
func aclKey(family, name string) string { return family + "/" + strings.TrimSpace(name) }

// aclAddress formats one match of an entry: "any", "host <address>",
// "<prefix>", or "<address> <wildcard>".
func aclAddress(any bool, host, addr, wildcard string) string {
	switch {
	case any:
		return "any"
	case strings.TrimSpace(host) != "":
		return "host " + strings.TrimSpace(host)
	case strings.TrimSpace(addr) != "" && strings.TrimSpace(wildcard) != "":
		return strings.TrimSpace(addr) + " " + strings.TrimSpace(wildcard)
	}
	return strings.TrimSpace(addr)
}

// aclPorts formats a port match: "eq <p> ...", "gt <p>", "lt <p>",
// "neq <p>", or "range <a> <b>". eq gathers its first value and the
// numbered additional values (<side>-eq-port2 .. -port10).
func aclPorts(side string, eq []string, gt, lt, neq, r1, r2 string, extra []xmlNode) string {
	var eqs []string
	for _, v := range eq {
		if v = strings.TrimSpace(v); v != "" {
			eqs = append(eqs, v)
		}
	}
	for _, x := range extra {
		if strings.HasPrefix(x.XMLName.Local, side+"-eq-port") {
			if v := strings.TrimSpace(x.Text); v != "" {
				eqs = append(eqs, v)
			}
		}
	}
	switch {
	case len(eqs) > 0:
		return "eq " + strings.Join(eqs, " ")
	case strings.TrimSpace(r1) != "" && strings.TrimSpace(r2) != "":
		return "range " + strings.TrimSpace(r1) + " " + strings.TrimSpace(r2)
	case strings.TrimSpace(gt) != "":
		return "gt " + strings.TrimSpace(gt)
	case strings.TrimSpace(lt) != "":
		return "lt " + strings.TrimSpace(lt)
	case strings.TrimSpace(neq) != "":
		return "neq " + strings.TrimSpace(neq)
	}
	return ""
}

// extendedEntry converts one extended IPv4 or IPv6 entry.
func extendedEntry(r aclRuleX) (map[string]any, bool) {
	row := map[string]any{}
	putInt(row, "sequence", r.Sequence)
	putStr(row, "remark", r.Remark)
	a := r.ACE
	if a == nil {
		// A remark-only entry documents the list but matches nothing.
		return row, false
	}
	putStr(row, "action", a.Action)
	putStr(row, "protocol", a.Protocol)
	putStr(row, "source", firstOf(
		aclAddress(a.SrcAny != nil, firstOf(a.SrcHost, a.SrcHost2, a.SrcHost6), a.SrcAddr, a.SrcMask),
		a.SrcPrefix6, a.SrcAddr6))
	putStr(row, "source_port", aclPorts("src", a.SrcEq, a.SrcGt, a.SrcLt, a.SrcNeq, a.SrcR1, a.SrcR2, a.Extra))
	putStr(row, "destination", firstOf(
		aclAddress(a.DstAny != nil || a.DstAny6 != nil, firstOf(a.DstHost, a.DstHost2, a.DstHost6), a.DstAddr, a.DstMask),
		a.DstPrefix6, a.DstAddr6))
	putStr(row, "destination_port", aclPorts("dst", a.DstEq, a.DstGt, a.DstLt, a.DstNeq, a.DstR1, a.DstR2, a.Extra))
	if a.Established != nil {
		row["established"] = true
	}
	if a.Log != nil || a.LogInput != nil {
		row["log"] = true
	}
	_, isRule := row["action"]
	return row, isRule
}

// aclBindings collects where each ACL is applied: interface access
// groups and IPv6 traffic filters and VTY access classes.
func aclBindings(ifs nativeInterfaces, mgmt nativeManagement) map[string][]map[string]any {
	out := map[string][]map[string]any{}
	add := func(family, acl string, b map[string]any) {
		if acl = strings.TrimSpace(acl); acl != "" {
			k := aclKey(family, acl)
			out[k] = append(out[k], b)
		}
	}
	for _, e := range ifs.Interface.Entries {
		name := e.fullName()
		if name == "" {
			continue
		}
		add("ipv4", e.ACLIn, map[string]any{"interface": name, "direction": "in"})
		add("ipv4", e.ACLOut, map[string]any{"interface": name, "direction": "out"})
		for _, f := range e.IPv6Filter {
			add("ipv6", f.ACL, map[string]any{"interface": name, "direction": strings.TrimSpace(f.Direction)})
		}
	}
	for _, v := range mgmt.VTY {
		line := "vty " + strings.TrimSpace(v.First)
		if l := strings.TrimSpace(v.Last); l != "" {
			line += " " + l
		}
		for _, c := range v.AccessClass {
			add("ipv4", c.ACL, map[string]any{"line": line, "direction": strings.TrimSpace(c.Direction)})
		}
	}
	return out
}

// TransformACLs converts every standard, extended and IPv6 access list
// into an osiris.cisco.acl resource with its type, rule count, entries
// in sequence order as configured and the interfaces and VTY lines it
// is applied to. IDs are cisco.iosxe::<device-key>/acl/<family>/<name>.
func TransformACLs(deviceKey string, acls nativeACLs, ifs nativeInterfaces, mgmt nativeManagement) []sdk.Resource {
	bindings := aclBindings(ifs, mgmt)
	var out []sdk.Resource
	build := func(aclType, name string, entries []map[string]any, rules int) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		family := aclFamily(aclType)
		native := deviceKey + "/acl/" + family + "/" + name
		r, err := sdk.NewResource(resourceID(providerName, native), "osiris.cisco.acl", sdk.Provider{
			Name:     providerName,
			NativeID: native,
			Type:     aclType,
		})
		if err != nil {
			return
		}
		r.Name = name
		r.Status = "active"
		props := map[string]any{"acl_type": aclType, "rule_count": rules}
		if len(entries) > 0 {
			props["entries"] = entries
		}
		if b := bindings[aclKey(family, name)]; len(b) > 0 {
			props["bindings"] = b
		}
		r.Properties = props
		out = append(out, r)
	}

	for _, a := range acls.Standard {
		var entries []map[string]any
		rules := 0
		for _, rule := range a.Rules {
			row := map[string]any{}
			putInt(row, "sequence", rule.Sequence)
			putStr(row, "remark", rule.Remark)
			var ace *stdACE
			switch {
			case rule.Permit != nil:
				row["action"], ace = "permit", &rule.Permit.ACE
			case rule.Deny != nil:
				row["action"], ace = "deny", &rule.Deny.ACE
			}
			if ace != nil {
				rules++
				putStr(row, "source", aclAddress(ace.Any != nil, firstOf(ace.Host, ace.Host2), firstOf(ace.Prefix, ace.Prefix2), ace.Mask))
				if ace.Log != nil {
					row["log"] = true
				}
			}
			if len(row) > 0 {
				entries = append(entries, row)
			}
		}
		build(aclStandard, a.Name, entries, rules)
	}
	extended := func(aclType, name string, rs []aclRuleX) {
		var entries []map[string]any
		rules := 0
		for _, rule := range rs {
			row, isRule := extendedEntry(rule)
			if isRule {
				rules++
			}
			if len(row) > 0 {
				entries = append(entries, row)
			}
		}
		build(aclType, name, entries, rules)
	}
	for _, a := range acls.Extended {
		extended(aclExtended, a.Name, a.Rules)
	}
	for _, a := range acls.IPv6 {
		extended(aclIPv6, a.Name, a.Rules)
	}
	return out
}

// Certificates.
// normalizeYANGTime returns a YANG date-and-time as RFC 3339 UTC, or
// the reported value unchanged when it does not parse.
func normalizeYANGTime(v string) string {
	v = strings.TrimSpace(v)
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return sdk.NormalizeRFC3339UTC(t)
	}
	return v
}

// pkiEnum strips the crypto-pki-oper enumeration prefix from v and
// returns "" for the not-set values.
func pkiEnum(v string, prefixes ...string) string {
	v = strings.TrimSpace(v)
	for _, p := range prefixes {
		v = strings.TrimPrefix(v, p)
	}
	switch v {
	case "", "unset", "none":
		return ""
	}
	return v
}

// TransformCertificates converts every certificate installed in a PKI
// trustpoint into a network.security.certificate resource: trustpoint,
// certificate serial number, subject, issuer, validity, usage and key
// type. The status is active when the certificate is available and
// inactive otherwise. IDs are
// cisco.iosxe::<device-key>/certificate/<trustpoint>/<serial>, with the
// certificate's position in the trustpoint standing in for
// a missing serial.
func TransformCertificates(deviceKey string, p pkiOper) []sdk.Resource {
	var out []sdk.Resource
	seen := map[string]bool{}
	for _, b := range p.Bundles {
		label := strings.TrimSpace(b.Label)
		if label == "" {
			continue
		}
		for i, c := range b.Certs {
			serial := strings.TrimSpace(c.SerialNumber)
			if serial == "" && strings.TrimSpace(c.SubjectName) == "" {
				continue
			}
			key := serial
			if key == "" {
				key = strconv.Itoa(i)
			}
			native := deviceKey + "/certificate/" + label + "/" + key
			id := resourceID(providerName, native)
			if seen[id] {
				continue
			}
			seen[id] = true
			r, err := sdk.NewResource(id, "network.security.certificate", sdk.Provider{
				Name:     providerName,
				NativeID: native,
				Type:     "crypto-pki-certificate",
			})
			if err != nil {
				continue
			}
			r.Name = firstOf(c.SubjectName, label)
			r.Status = "inactive"
			if strings.TrimSpace(c.Avail) == "crypto-pki-cert-available" {
				r.Status = "active"
			}
			props := map[string]any{"trustpoint": label}
			putStr(props, "serial_number", serial)
			putStr(props, "subject", c.SubjectName)
			putStr(props, "issuer", c.IssuerName)
			putStr(props, "valid_from", normalizeYANGTime(c.ValidityStart))
			putStr(props, "valid_to", normalizeYANGTime(c.ValidityEnd))
			putStr(props, "usage", pkiEnum(c.Usage, "crypto-pki-cert-usage-", "crypto-pki-cert-"))
			putStr(props, "key_type", pkiEnum(c.KeyType, "crypto-pki-cert-key-"))
			r.Properties = props
			out = append(out, r)
		}
	}
	return out
}
