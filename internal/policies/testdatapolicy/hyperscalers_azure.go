// hyperscalers_azure.go - Microsoft Azure identity rules of the test
// data policy scanner (sections 5.1, 5.2 and 5.5): tenant, subscription
// object and principal GUIDs, Azure Resource Manager paths, resource
// group names and Azure CLI identity arguments (-S, --subscription,
// --tenant).
//
// Every GUID in an Azure producer file is an identity candidate. A
// GUID elsewhere is checked only in an Azure identity field, an ARM
// path or an Azure CLI identity argument, so the UUIDs of other
// protocols are never mistaken for Azure identities.
//
// OSIRIS JSON Producers Test Data Policy:
// [OSIRIS-PRODUCER-TEST-DATA-POLICY]: https://github.com/osirisjson/osiris-producers/blob/main/docs/policies/OSIRIS-PRODUCER-TEST-DATA-POLICY.md

package testdatapolicy

import (
	"regexp"
	"strings"
)

var (
	reGUID         = regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	reApprovedGUID = regexp.MustCompile(`^00000000-0000-[1-5]000-8000-00000000[0-9a-f]{4}$`)

	// reARM matches an Azure Resource Manager path. Group 1 is the
	// subscription segment and group 2 the rest of the path, where ';'
	// is allowed for service composite names.
	reARM = regexp.MustCompile("(?i)/subscriptions/([^/\\s\"'<>`,;)?#\\\\]*)((?:/[^/\\s\"'<>`,)?#\\\\]+)*)")

	// reAzureCLI matches an Azure CLI subscription or tenant argument.
	reAzureCLI = regexp.MustCompile(`(?:^|\s)(?:-S|--subscription|--tenant)(?:\s+|=)([^\s'"]+)`)

	// reIdentLike matches a plain identifier; an ARM subscription
	// segment that is not one is code, such as a regular expression.
	reIdentLike = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

	// Azure naming restrictions applied to invented names.
	reAzureRGName  = regexp.MustCompile(`^[\w.()-]{1,90}$`)
	reAzureStorage = regexp.MustCompile(`^[a-z0-9]{3,24}$`)
	reAzureVault   = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]{1,22}[A-Za-z0-9]$`)
)

const nilUUID = "00000000-0000-0000-0000-000000000000"

// isAzureFile reports whether p belongs to the Azure producer, where
// every GUID is an identity candidate.
func isAzureFile(p string) bool {
	return strings.Contains(p, "hyperscalers/azure/") || strings.HasPrefix(p, "cmd/osirisjson-producer-azure/")
}

// checkGUID evaluates an Azure identity GUID.
func checkGUID(g string) (string, bool) {
	l := strings.ToLower(g)
	switch {
	case l == nilUUID:
		return "nil UUID is allowed only for a nil-identity test with an allowlist entry", false
	case reApprovedGUID.MatchString(l):
		return "", true
	}
	return "Azure GUID " + display(g) + " is not an approved synthetic identifier", false
}

// checkResourceGroup evaluates an Azure resource group name: an
// approved word first, optionally after an "rg-" or "rg_" prefix, and
// valid Azure resource group syntax.
func checkResourceGroup(n string) (string, bool) {
	l := strings.ToLower(n)
	body := n
	if strings.HasPrefix(l, "rg-") || strings.HasPrefix(l, "rg_") {
		body = n[3:]
	}
	if !isSyntheticHyperscalerName(body) {
		return "resource group " + display(n) + " is not a synthetic name", false
	}
	if !reAzureRGName.MatchString(n) || strings.HasSuffix(n, ".") {
		return "resource group " + display(n) + " violates Azure resource group naming rules", false
	}
	return "", true
}

// scanAzure evaluates the Azure identity rules on one line of file p:
// ARM paths, Azure CLI identity arguments and, in Azure producer
// files, every GUID. It returns the findings and the number of
// candidates inspected.
func scanAzure(p, line string) ([]contextFinding, int) {
	var out []contextFinding
	n := 0
	add := func(col int, rule, canonical, msg string) {
		out = append(out, contextFinding{col: col, rule: rule, canonical: canonical, message: msg})
	}

	for _, m := range reARM.FindAllStringSubmatchIndex(line, -1) {
		n++
		out = append(out, checkARMPath(line, m)...)
	}
	for _, m := range reAzureCLI.FindAllStringSubmatchIndex(line, -1) {
		v := line[m[2]:m[3]]
		if !reGUID.MatchString(v) || len(v) != 36 {
			continue
		}
		n++
		if msg, ok := checkGUID(v); !ok {
			add(m[2]+1, RuleAzureGUID, strings.ToLower(v), msg)
		}
	}
	if isAzureFile(p) {
		for _, m := range reGUID.FindAllStringIndex(line, -1) {
			start, end := m[0], m[1]
			if start > 0 && (isHexByte(line[start-1]) || line[start-1] == '-') {
				continue
			}
			if end < len(line) && (isHexByte(line[end]) || line[end] == '-') {
				continue
			}
			n++
			g := line[start:end]
			if msg, ok := checkGUID(g); !ok {
				add(start+1, RuleAzureGUID, strings.ToLower(g), msg)
			}
		}
	}
	return out, n
}

// checkARMPath validates the subscription, resource group and resource
// name segments of one ARM path match.
func checkARMPath(line string, m []int) []contextFinding {
	var out []contextFinding
	add := func(col int, rule, canonical, msg string) {
		out = append(out, contextFinding{col: col, rule: rule, canonical: canonical, message: msg})
	}
	sub := line[m[2]:m[3]]
	switch {
	case isHyperscalerTemplate(sub) || !reIdentLike.MatchString(sub):
		// A placeholder or code, such as a regular expression.
	case reGUID.MatchString(sub) && len(sub) == 36:
		if msg, ok := checkGUID(sub); !ok {
			add(m[2]+1, RuleAzureGUID, strings.ToLower(sub), msg)
		}
	default:
		add(m[2]+1, RuleAzureARM, sub, "ARM subscription segment "+display(sub)+" is not a GUID")
	}

	rest := strings.Trim(line[m[4]:m[5]], "/")
	if rest == "" {
		return out
	}
	segs := strings.Split(rest, "/")
	// Column of each segment, for precise findings.
	cols := make([]int, len(segs))
	c := m[4] + 1
	for i, s := range segs {
		cols[i] = c + 1
		c += len(s) + 1
	}

	nameFinding := func(i int, typ string, depth int) {
		name := segs[i]
		if isHyperscalerTemplate(name) {
			return
		}
		lt := strings.ToLower(typ)
		if lt == "locations" {
			return // an Azure region, not an invented name
		}
		for _, r := range azureReservedNames[lt] {
			if strings.EqualFold(name, r) {
				return
			}
		}
		if depth >= 2 && name == "default" {
			return // service-defined default child resource
		}
		if strings.Contains(name, ";") {
			// Service composite name, such as a Recovery Services
			// container "iaasvmcontainerv2;<resource group>;<vm>": the
			// leading tokens are service-defined, the last two are the
			// customer's resource group and resource.
			parts := strings.Split(name, ";")
			if len(parts) >= 3 {
				rg, vm := parts[len(parts)-2], parts[len(parts)-1]
				if msg, ok := checkResourceGroup(rg); !ok {
					add(cols[i]+strings.LastIndex(name, ";"+rg+";")+1, RuleHyperscalerName, rg, msg)
				}
				if msg, ok := checkHyperscalerName("", vm); !ok {
					add(cols[i]+strings.LastIndex(name, ";"+vm)+1, RuleHyperscalerName, vm, msg)
				}
			}
			return
		}
		if reGUID.MatchString(name) && len(name) == 36 {
			if msg, ok := checkGUID(name); !ok {
				add(cols[i], RuleAzureGUID, strings.ToLower(name), msg)
			}
			return
		}
		if msg, ok := checkHyperscalerName(lt, name); !ok {
			add(cols[i], RuleHyperscalerName, name, msg)
		}
	}

	for i := 0; i+1 < len(segs); {
		key := strings.ToLower(segs[i])
		switch key {
		case "resourcegroups":
			if rg := segs[i+1]; !isHyperscalerTemplate(rg) {
				if msg, ok := checkResourceGroup(rg); !ok {
					add(cols[i+1], RuleHyperscalerName, rg, msg)
				}
			}
			i += 2
		case "providers":
			i += 2 // skip the namespace, such as Microsoft.Network
			depth := 0
			for i+1 < len(segs) && !strings.EqualFold(segs[i], "providers") {
				depth++
				nameFinding(i+1, segs[i], depth)
				i += 2
			}
		default:
			i += 2 // subscription-level collections such as locations
		}
	}
	return out
}

// evaluateAzureKeyValue applies the Azure identity field rules: GUID
// fields (tenantId, subscriptionId, principalId and similar, plus an
// account field holding a GUID) and resource group fields. The second
// result reports whether the key selected one of those rules.
func evaluateAzureKeyValue(kv keyValue) ([]contextFinding, bool) {
	k, v := kv.key, strings.TrimSpace(kv.value)
	switch {
	case (azureGUIDKeys[k] || k == "account") && len(v) == 36 && reGUID.MatchString(v):
		if msg, ok := checkGUID(v); !ok {
			return hyperscalerFinding(kv, RuleAzureGUID, strings.ToLower(v), msg), true
		}
		return nil, true
	case resourceGroupKeys[k]:
		if isHyperscalerTemplate(v) || skipFreeText(v) {
			return nil, true
		}
		if msg, ok := checkResourceGroup(v); !ok {
			return hyperscalerFinding(kv, RuleHyperscalerName, v, msg), true
		}
		return nil, true
	}
	return nil, false
}
