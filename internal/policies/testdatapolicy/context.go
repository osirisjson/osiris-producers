// context.go - Context rules of the test data policy scanner
// (section 4): validation of hostnames, device names, coordinates,
// sites, AS numbers, serial numbers and credentials, identified by
// their field name or surrounding syntax.
//
// Key-value pairs are extracted lexically from JSON, YAML, XML and Go
// literals, the key is normalized and mapped to a rule and only the
// value is evaluated. Free-text patterns cover CLI host arguments,
// email domains, URL user information, AS number labels and
// well-known credential shapes. Context rules never apply to
// arbitrary strings or numbers.
//
// OSIRIS JSON Producers Test Data Policy:
// [OSIRIS-PRODUCER-TEST-DATA-POLICY]: https://github.com/osirisjson/osiris-producers/blob/main/docs/policies/OSIRIS-PRODUCER-TEST-DATA-POLICY.md

package testdatapolicy

import (
	"net/netip"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
)

var (
	// reKV matches "key": value, key: value, Key: "value" and
	// key := "value". Group 1 is the key, group 2 the separator and
	// group 3 the raw value.
	reKV = regexp.MustCompile(`(?:^|[^\w.$-])["']?([A-Za-z_][\w.-]*)["']?\s*(:=|[:=])\s*("(?:[^"\\]|\\.)*"|'[^'\n]*'|[^\s,;{}\[\]()"'<>]+)`)
	// reXML matches <key>value</key> on one line.
	reXML = regexp.MustCompile(`<([A-Za-z_][\w.:-]*)(?:\s[^<>]*)?>([^<>]{0,512})</([A-Za-z_][\w.:-]*)>`)

	reCLIHost  = regexp.MustCompile(`(?:^|\s)(?:-h|--host|--start-url)(?:\s+|=)([^\s'"]+)`)
	reEmail    = regexp.MustCompile(`[A-Za-z0-9._%+-]+@([A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)+)`)
	reUserinfo = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://[^/\s:@"'<>\\]+:([^/\s@"'<>\\]+)@`)
	reASNLabel = regexp.MustCompile(`(?:^|[^\w-])ASN? ?(\d{1,12})\b`)

	// reAuthScheme matches an HTTP authorization scheme prefix.
	reAuthScheme = regexp.MustCompile(`^(?i:basic|bearer|token|digest) +`)

	reCredentialPlaceholder = regexp.MustCompile(`^(?:` + strings.Join(credentialPlaceholders, "|") + `)(?:-\d+)?$`)

	// credentialShapes recognize well-known real credential formats
	// anywhere in a text file.
	credentialShapes = []struct {
		name string
		re   *regexp.Regexp
	}{
		{"AWS access key ID", regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`)},
		{"GitHub token", regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{36,}\b`)},
		{"JSON Web Token", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`)},
		{"private key header", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
	}
)

// contextFinding is one context or hyperscaler rule finding before
// its line, path and fingerprint are attached.
type contextFinding struct {
	col       int
	rule      string
	canonical string
	message   string
}

// keyValue is one extracted key-value pair.
type keyValue struct {
	key    string // normalized
	value  string // unquoted
	col    int    // 1-based column of the value
	quoted bool   // value was quoted or an XML element body

	// hyperscalersFile marks a line from an AWS or Azure producer file,
	// where the hyperscaler resource name fields (bucketName, roleName,
	// functionName) are checked.
	hyperscalersFile bool
}

// contextScope reports whether the key-value context rules apply to
// path: every file except production (non-test) Go sources.
func contextScope(p string) bool {
	return !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go")
}

// unquotedStringsOK reports whether unquoted scalars in a file are
// string values (YAML and similar configuration formats) rather than
// identifiers or prose.
func unquotedStringsOK(p string) bool {
	switch path.Ext(p) {
	case ".yml", ".yaml", ".properties", ".toml", ".ini":
		return true
	}
	return false
}

// extractKeyValues returns the key-value pairs found in line.
func extractKeyValues(line string) []keyValue {
	var out []keyValue
	for _, m := range reKV.FindAllStringSubmatchIndex(line, -1) {
		key := line[m[2]:m[3]]
		sepEnd := m[5]
		if sepEnd < len(line) && line[sepEnd] == '=' {
			continue // ==, :==
		}
		raw := line[m[6]:m[7]]
		if strings.ContainsRune(raw, '`') {
			continue // a Go raw string concatenation, not a literal
		}
		kv := keyValue{key: normalizeKey(key), col: m[6] + 1}
		switch raw[0] {
		case '"':
			if v, err := strconv.Unquote(raw); err == nil {
				kv.value = v
			} else {
				kv.value = strings.Trim(raw, `"`)
			}
			kv.quoted = true
			kv.col++
		case '\'':
			kv.value = strings.Trim(raw, "'")
			kv.quoted = true
			kv.col++
		default:
			kv.value = raw
		}
		out = append(out, kv)
	}
	for _, m := range reXML.FindAllStringSubmatchIndex(line, -1) {
		open, closing := line[m[2]:m[3]], line[m[6]:m[7]]
		body := line[m[4]:m[5]]
		if open != closing || strings.ContainsAny(body, "\"`") {
			continue // mismatched tags or a Go string concatenation
		}
		out = append(out, keyValue{
			key:    normalizeKey(open),
			value:  strings.TrimSpace(body),
			col:    m[4] + 1,
			quoted: true,
		})
	}
	return out
}

// scanContext evaluates the context rules and the hyperscaler identity
// rules on one line. It returns the findings and the number of
// candidates inspected.
func scanContext(p, line string) ([]contextFinding, int) {
	var out []contextFinding
	n := 0
	inScope := contextScope(p)
	unquotedOK := unquotedStringsOK(p)

	if inScope {
		hyperscalersFile := isAzureFile(p) || isAWSFile(p)
		for _, kv := range extractKeyValues(line) {
			if !kv.quoted && !unquotedOK && !isNumeric(kv.value) {
				continue
			}
			kv.hyperscalersFile = hyperscalersFile
			f, inspected := evaluateKeyValue(kv)
			if inspected {
				n++
			}
			out = append(out, f...)
		}
		for _, m := range reEmail.FindAllStringSubmatchIndex(line, -1) {
			host := strings.ToLower(line[m[2]:m[3]])
			if !looksLikeHostname(host) {
				continue
			}
			n++
			if msg, ok := checkHostname(host); !ok {
				out = append(out, contextFinding{col: m[2] + 1, rule: RuleDNS, canonical: host, message: "email domain " + msg})
			}
		}
		for _, m := range reUserinfo.FindAllStringSubmatchIndex(line, -1) {
			n++
			if v := line[m[2]:m[3]]; !credentialOK(v) {
				out = append(out, credentialFinding(m[2]+1, v, "password in URL user information"))
			}
		}
		for _, m := range reASNLabel.FindAllStringSubmatchIndex(line, -1) {
			n++
			if v := line[m[2]:m[3]]; !asnOK(v) {
				out = append(out, contextFinding{col: m[2] + 1, rule: RuleASN, canonical: v, message: "AS number " + v + " is outside the approved ranges"})
			}
		}
	}

	// CLI host arguments and credential shapes apply to every file,
	// including production help text.
	for _, m := range reCLIHost.FindAllStringSubmatchIndex(line, -1) {
		v := line[m[2]:m[3]]
		host, ok := hostOf(v)
		if !ok || isProjectReference(v) {
			continue
		}
		n++
		if msg, ok := checkHostname(host); !ok {
			out = append(out, contextFinding{col: m[2] + 1, rule: RuleDNS, canonical: host, message: "CLI host argument " + msg})
		}
	}
	for _, s := range credentialShapes {
		for _, m := range s.re.FindAllStringIndex(line, -1) {
			n++
			out = append(out, credentialFinding(m[0]+1, line[m[0]:m[1]], s.name))
		}
	}
	// Hyperscaler identity syntax (ARM paths, ARNs, AWS resource IDs,
	// Azure CLI identity arguments and Azure or AWS producer file
	// scope) applies to every file.
	cfs, cn := scanHyperscalers(p, line)
	return append(out, cfs...), n + cn
}

func credentialFinding(col int, value, what string) contextFinding {
	return contextFinding{
		col:       col,
		rule:      RuleCredential,
		canonical: value,
		message:   what + " is not an approved placeholder (value not shown)",
	}
}

// evaluateKeyValue applies the rule selected by kv.key. The second
// result reports whether the key selected any rule.
func evaluateKeyValue(kv keyValue) ([]contextFinding, bool) {
	if f, ok := evaluateHyperscalerKeyValue(kv); ok {
		return f, true
	}
	k, v := kv.key, strings.TrimSpace(kv.value)
	one := func(rule, canonical, msg string) []contextFinding {
		return []contextFinding{{col: kv.col, rule: rule, canonical: canonical, message: msg}}
	}
	switch {
	case isCredentialKey(k):
		if credentialOK(v) {
			return nil, true
		}
		return []contextFinding{credentialFinding(kv.col, v, "credential field "+strconv.Quote(kv.key))}, true

	case deviceKeys[k]:
		if v == "" || skipFreeText(v) {
			return nil, true
		}
		if strings.Contains(v, ".") {
			// An FQDN in a device field is governed by the DNS rule.
			host, ok := hostOf(v)
			if !ok {
				return nil, true
			}
			if msg, ok := checkHostname(host); !ok {
				return one(RuleDNS, host, "hostname "+msg), true
			}
			return nil, true
		}
		if !hasDevicePrefix(v) {
			return one(RuleDevice, v, "device name "+display(v)+" has no approved prefix"), true
		}
		return nil, true

	case dnsKeys[k]:
		if isProjectReference(v) {
			return nil, true
		}
		host, ok := hostOf(v)
		if !ok {
			return nil, true
		}
		if msg, ok := checkHostname(host); !ok {
			return one(RuleDNS, host, "hostname "+msg), true
		}
		return nil, true

	case latitudeKeys[k], longitudeKeys[k]:
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return nil, true
		}
		want, what := approvedLatitude, "latitude"
		if longitudeKeys[k] {
			want, what = approvedLongitude, "longitude"
		}
		if f != want {
			return one(RuleGeo, v, what+" "+v+" is not the approved coordinate"), true
		}
		return nil, true

	case siteKeys[k]:
		if v == "" || skipFreeText(v) || approvedSites[v] {
			return nil, true
		}
		return one(RuleSite, v, "site label "+display(v)+" is not approved"), true

	case asnKeys[k]:
		if !isNumeric(v) || asnOK(v) {
			return nil, true
		}
		return one(RuleASN, v, "AS number "+v+" is outside the approved ranges"), true

	case serialKeys[k]:
		if v == "" || skipFreeText(v) || strings.HasPrefix(v, "TST") {
			return nil, true
		}
		if mac, ok := strings.CutPrefix(v, "tpd_"); ok {
			if res, ok := classifyMAC(mac); ok {
				if res.allowed {
					return nil, true
				}
				return one(RuleMAC, res.canonical, "MAC "+res.canonical+" in pseudo-serial is a "+res.class), true
			}
		}
		return one(RuleSerial, v, "serial number "+display(v)+" does not start with TST"), true
	}
	return nil, false
}

// skipFreeText reports whether v is a template, a format verb or prose
// rather than a literal identifier and so is not checked.
func skipFreeText(v string) bool {
	return strings.ContainsAny(v, " \t/%{}$<>")
}

func hasDevicePrefix(label string) bool {
	u := strings.ToUpper(label)
	for _, p := range devicePrefixes {
		if strings.HasPrefix(u, p) {
			return true
		}
	}
	return false
}

// credentialOK reports whether v is an approved placeholder, an empty
// value, an SDK redaction marker or a template or variable reference
// that carries no value. An HTTP authorization scheme word (Basic,
// Bearer) in front of the value is ignored.
func credentialOK(v string) bool {
	v = reAuthScheme.ReplaceAllString(v, "")
	if v == "" || redactionMarkers[v] || strings.HasPrefix(v, "{{") || strings.HasPrefix(v, "$") {
		return true
	}
	return reCredentialPlaceholder.MatchString(v)
}

func isNumeric(v string) bool {
	if v == "" {
		return false
	}
	_, err := strconv.ParseFloat(v, 64)
	return err == nil
}

// asnOK reports whether a decimal AS number lies in an approved range.
// Values that overflow uint64 are not approved.
func asnOK(v string) bool {
	n, err := strconv.ParseUint(v, 10, 64)
	if err != nil {
		return false
	}
	for _, r := range asnRanges {
		if n >= r[0] && n <= r[1] {
			return true
		}
	}
	return false
}

// hostOf extracts a DNS hostname from a host, host:port or URL value.
// It reports false for values that are not hostnames: IP literals,
// single labels, templates and placeholders.
func hostOf(v string) (string, bool) {
	if v == "" || strings.ContainsAny(v, " {}$<>%") {
		return "", false
	}
	host := v
	if strings.Contains(v, "://") {
		u, err := url.Parse(v)
		if err != nil {
			return "", false
		}
		host = u.Hostname()
	} else {
		host, _, _ = strings.Cut(host, "/")
		if h, _, ok := strings.Cut(host, ":"); ok && strings.Count(host, ":") == 1 {
			host = h
		}
	}
	host = strings.TrimSuffix(strings.TrimPrefix(strings.ToLower(host), "*."), ".")
	if _, err := netip.ParseAddr(host); err == nil {
		return "", false
	}
	if !looksLikeHostname(host) {
		return "", false
	}
	return host, true
}

// isProjectReference reports whether v is a URL into the project's own
// GitHub organization, which is a reference rather than example data.
func isProjectReference(v string) bool {
	u, err := url.Parse(v)
	if err != nil || u.Scheme == "" {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "github.com", "raw.githubusercontent.com":
		return strings.HasPrefix(strings.ToLower(u.Path), "/"+projectGitHubOrg+"/")
	}
	return false
}

// looksLikeHostname reports whether h is a dotted name of DNS label
// characters whose last label is alphabetic, so version strings such
// as v0.12.0 are not hostnames.
func looksLikeHostname(h string) bool {
	labels := strings.Split(h, ".")
	if len(labels) < 2 {
		return false
	}
	for _, l := range labels {
		if l == "" {
			return false
		}
		for i := 0; i < len(l); i++ {
			c := l[i]
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
				return false
			}
		}
	}
	tld := labels[len(labels)-1]
	for i := 0; i < len(tld); i++ {
		if tld[i] < 'a' || tld[i] > 'z' {
			return false
		}
	}
	return len(tld) >= 2
}

// checkHostname evaluates a lowercase hostname. On failure it returns
// a message fragment describing the problem.
func checkHostname(h string) (string, bool) {
	for _, d := range approvedDomains {
		if h == d || strings.HasSuffix(h, "."+d) {
			return "", true
		}
	}
	for _, s := range hyperscalerSuffixes {
		rest, ok := strings.CutSuffix(h, "."+s)
		if !ok {
			continue
		}
		if rest == privateLinkLabel {
			return "", true
		}
		first, _, _ := strings.Cut(rest, ".")
		for _, p := range syntheticLabelPrefixes {
			if strings.HasPrefix(first, p) {
				return "", true
			}
		}
		return display(h) + " uses an approved hyperscaler suffix but a non-synthetic label", false
	}
	return display(h) + " is not under an approved example domain", false
}
