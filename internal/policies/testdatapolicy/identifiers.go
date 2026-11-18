// identifiers.go - Address rules of the test data policy scanner
// (section 3): extraction and classification of IPv4, IPv6 and
// MAC literals in raw text, in every file.
//
// Candidates are found with permissive patterns, filtered by token
// boundaries, parsed with net/netip and then classified as approved
// documentation values, built-in protocol constants (unspecified
// addresses, default routes, netmasks, wildcard masks, null and
// broadcast MACs), loopback (approved only in _test.go files) or a
// violation with a descriptive class.
//
// OSIRIS JSON Producers Test Data Policy:
// [OSIRIS-PRODUCER-TEST-DATA-POLICY]: https://github.com/osirisjson/osiris-producers/blob/main/docs/policies/OSIRIS-PRODUCER-TEST-DATA-POLICY.md

package testdatapolicy

import (
	"encoding/binary"
	"encoding/hex"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
)

var (
	reIPv4 = regexp.MustCompile(`\d{1,3}(?:\.\d{1,3}){3}(?:/\d{1,3})?`)
	reIPv6 = regexp.MustCompile(`([0-9A-Fa-f:.]*:[0-9A-Fa-f:.]*)(/\d{1,3})?`)
	reMAC  = regexp.MustCompile(`[0-9A-Fa-f]{2}(?::[0-9A-Fa-f]{2}){5}|[0-9A-Fa-f]{2}(?:-[0-9A-Fa-f]{2}){5}|[0-9A-Fa-f]{4}\.[0-9A-Fa-f]{4}\.[0-9A-Fa-f]{4}`)

	// reSectionRef and reVersionRef recognize dotted quads that are
	// document section numbers or version strings, not addresses.
	reSectionRef = regexp.MustCompile(`(?i)(?:\bsections?|\bchapter|\bclause|\x{00A7})\s*$`)
	reVersionRef = regexp.MustCompile(`(?i)version["'\\\s:=]*$`)
)

// addrResult is the classification of one address literal.
type addrResult struct {
	canonical string // normalized form, hashed into the fingerprint
	class     string // e.g. "documentation", "private (RFC 1918)"
	allowed   bool
}

// addrCandidate is one address literal found in a line.
type addrCandidate struct {
	col  int // 1-based byte column
	rule string
	raw  string
	res  addrResult
}

func isWordByte(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func isHexByte(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// scanAddresses returns every IPv6, IPv4 and MAC candidate in line.
// Literals that only look like addresses are not returned: version
// strings, document section numbers and the "::" separator of OSIRIS
// JSON resource IDs such as cisco.nxos::<serial>.
func scanAddresses(line string, testFile bool) []addrCandidate {
	var out []addrCandidate
	var v6spans [][2]int

	for _, m := range reIPv6.FindAllStringSubmatchIndex(line, -1) {
		start, end := m[0], m[1]
		if strings.Count(line[m[2]:m[3]], ":") < 2 {
			continue
		}
		if start > 0 && isWordByte(line[start-1]) {
			continue
		}
		if end < len(line) && isWordByte(line[end]) {
			continue
		}
		raw, res, ok := classifyIPv6Raw(line[m[2]:m[3]], suffix(line, m[4], m[5]), testFile)
		if !ok {
			continue
		}
		v6spans = append(v6spans, [2]int{start, end})
		out = append(out, addrCandidate{col: start + 1, rule: RuleIPv6, raw: raw, res: res})
	}

	for _, m := range reIPv4.FindAllStringIndex(line, -1) {
		start, end := m[0], m[1]
		if start > 0 && (isWordByte(line[start-1]) || line[start-1] == '.') {
			continue
		}
		if end < len(line) && isWordByte(line[end]) {
			continue
		}
		if end+1 < len(line) && line[end] == '.' && line[end+1] >= '0' && line[end+1] <= '9' {
			continue
		}
		if inSpans(v6spans, start) {
			continue
		}
		if reSectionRef.MatchString(line[:start]) || reVersionRef.MatchString(line[:start]) {
			continue
		}
		res, ok := classifyIPv4(line[start:end], testFile)
		if !ok {
			continue
		}
		out = append(out, addrCandidate{col: start + 1, rule: RuleIPv4, raw: line[start:end], res: res})
	}

	for _, m := range reMAC.FindAllStringIndex(line, -1) {
		start, end := m[0], m[1]
		if start > 0 {
			if c := line[start-1]; isWordByte(c) || c == ':' || c == '-' || c == '.' {
				continue
			}
		}
		if end < len(line) {
			c := line[end]
			if isWordByte(c) || c == ':' {
				continue
			}
			if (c == '-' || c == '.') && end+1 < len(line) && isHexByte(line[end+1]) {
				continue
			}
		}
		if inSpans(v6spans, start) {
			continue
		}
		res, ok := classifyMAC(line[start:end])
		if !ok {
			continue
		}
		out = append(out, addrCandidate{col: start + 1, rule: RuleMAC, raw: line[start:end], res: res})
	}
	return out
}

func suffix(s string, from, to int) string {
	if from < 0 {
		return ""
	}
	return s[from:to]
}

func inSpans(spans [][2]int, pos int) bool {
	for _, s := range spans {
		if pos >= s[0] && pos < s[1] {
			return true
		}
	}
	return false
}

// classifyIPv6Raw tries the candidate as matched and with trailing
// sentence punctuation trimmed. It reports false when no variant
// parses as an IPv6 address.
func classifyIPv6Raw(addr, bits string, testFile bool) (string, addrResult, bool) {
	variants := []string{addr}
	if t := strings.TrimRight(addr, "."); t != addr {
		variants = append(variants, t)
	}
	if strings.HasSuffix(addr, ":") && !strings.HasSuffix(addr, "::") {
		variants = append(variants, strings.TrimSuffix(addr, ":"))
	}
	for _, v := range variants {
		raw := v
		if v == addr {
			raw += bits
		}
		if res, ok := classifyIPv6(raw, testFile); ok {
			return raw, res, true
		}
	}
	return "", addrResult{}, false
}

// classifyIPv4 classifies an IPv4 address or prefix literal. It
// reports false when the literal is not a valid IPv4 address.
func classifyIPv4(raw string, testFile bool) (addrResult, bool) {
	addrPart, _, hasBits := strings.Cut(raw, "/")
	a, err := netip.ParseAddr(addrPart)
	if err != nil || !a.Is4() {
		return addrResult{}, false
	}
	bits := 32
	if hasBits {
		p, err := netip.ParsePrefix(raw)
		if err != nil {
			return addrResult{canonical: raw, class: "malformed prefix"}, true
		}
		bits = p.Bits()
	}
	r := addrResult{canonical: a.String()}
	if hasBits {
		r.canonical += "/" + strconv.Itoa(bits)
	}
	switch {
	case a.IsUnspecified() && bits == 0:
		r.class, r.allowed = "default route", true
	case a.IsUnspecified() && bits == 32:
		r.class, r.allowed = "unspecified", true
	case inDocIPv4(a, bits):
		r.class, r.allowed = "documentation", true
	case !hasBits && isNetmask(a):
		r.class, r.allowed = "netmask", true
	case !hasBits && isWildcardMask(a):
		r.class, r.allowed = "wildcard mask", true
	case a.IsLoopback():
		r.class, r.allowed = "loopback", testFile
		if !testFile {
			r.class = "loopback outside a _test.go file"
		}
	default:
		r.class = ipv4Class(a)
	}
	return r, true
}

func inDocIPv4(a netip.Addr, bits int) bool {
	for _, p := range docIPv4 {
		if bits >= p.Bits() && p.Contains(a) {
			return true
		}
	}
	return false
}

func ipv4Uint(a netip.Addr) uint32 {
	b := a.As4()
	return binary.BigEndian.Uint32(b[:])
}

// isNetmask reports whether a is a contiguous IPv4 netmask of length
// /8 or longer, such as 255.255.255.0.
func isNetmask(a netip.Addr) bool {
	v := ipv4Uint(a)
	inv := ^v
	return v >= 0xFF000000 && inv&(inv+1) == 0
}

// isWildcardMask reports whether a is the inverse of a contiguous
// netmask of length /8 or longer, such as 0.0.0.255.
func isWildcardMask(a netip.Addr) bool {
	v := ipv4Uint(a)
	return v <= 0x00FFFFFF && v&(v+1) == 0
}

func ipv4Class(a netip.Addr) string {
	switch {
	case a.IsPrivate():
		return "private (RFC 1918)"
	case a.IsLinkLocalUnicast():
		return "link-local"
	case a.IsMulticast():
		return "multicast"
	case a.As4()[0] == 255:
		return "non-contiguous netmask"
	default:
		return "public or reserved"
	}
}

// classifyIPv6 classifies an IPv6 address or prefix literal. It
// reports false when the literal is not a valid IPv6 address.
func classifyIPv6(raw string, testFile bool) (addrResult, bool) {
	addrPart, _, hasBits := strings.Cut(raw, "/")
	a, err := netip.ParseAddr(addrPart)
	if err != nil || !a.Is6() || a.Zone() != "" {
		return addrResult{}, false
	}
	bits := 128
	if hasBits {
		p, err := netip.ParsePrefix(raw)
		if err != nil {
			return addrResult{canonical: raw, class: "malformed prefix"}, true
		}
		bits = p.Bits()
	}
	r := addrResult{canonical: a.String()}
	if hasBits {
		r.canonical += "/" + strconv.Itoa(bits)
	}
	switch {
	case a.Is4In6():
		r.class = "IPv4-mapped"
	case a.IsUnspecified() && bits == 0:
		r.class, r.allowed = "default route", true
	case a.IsUnspecified() && bits == 128:
		r.class, r.allowed = "unspecified", true
	case bits >= docIPv6.Bits() && docIPv6.Contains(a):
		r.class, r.allowed = "documentation", true
	case a.IsLoopback() && bits == 128:
		r.class, r.allowed = "loopback", testFile
		if !testFile {
			r.class = "loopback outside a _test.go file"
		}
	case a.IsLinkLocalUnicast():
		r.class = "link-local"
	case a.IsMulticast():
		r.class = "multicast"
	case a.IsPrivate():
		r.class = "unique local"
	default:
		r.class = "global or reserved"
	}
	return r, true
}

// classifyMAC classifies a 48-bit MAC literal in colon, hyphen or
// Cisco dotted form.
func classifyMAC(raw string) (addrResult, bool) {
	h := strings.NewReplacer(":", "", "-", "", ".", "").Replace(raw)
	b, err := hex.DecodeString(h)
	if err != nil || len(b) != 6 {
		return addrResult{}, false
	}
	return classifyMACBytes(b), true
}

func classifyMACBytes(b []byte) addrResult {
	parts := make([]string, len(b))
	for i, x := range b {
		parts[i] = hex.EncodeToString([]byte{x})
	}
	r := addrResult{canonical: strings.Join(parts, ":")}
	c := r.canonical
	switch {
	case c == "00:00:00:00:00:00":
		r.class, r.allowed = "null MAC", true
	case c == "ff:ff:ff:ff:ff:ff":
		r.class, r.allowed = "broadcast MAC", true
	case strings.HasPrefix(c, "00:00:5e:00:53:"):
		r.class, r.allowed = "documentation unicast", true
	case strings.HasPrefix(c, "01:00:5e:90:10:"):
		r.class, r.allowed = "documentation multicast", true
	case strings.HasPrefix(c, "aa:bb:cc:dd:"):
		r.class, r.allowed = "synthetic sequence", true
	case b[0]&0x02 != 0 && b[0]&0x01 == 0:
		r.class, r.allowed = "locally administered unicast", true
	case b[0]&0x01 != 0:
		r.class = "multicast outside the documentation range"
	default:
		r.class = "globally administered OUI"
	}
	return r
}
