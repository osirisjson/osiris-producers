// hyperscalers.go - Shared part of the hyperscaler identity rules of
// the test data policy scanner (section 5): the per-line entry point,
// placeholder detection, synthetic resource name checks with the
// service naming restrictions, and the identity field dispatch.
//
// The provider-specific rules live next to this file:
//
//   - hyperscalers_azure.go: Microsoft Azure GUIDs, Azure Resource
//     Manager paths, resource groups and Azure CLI identity arguments.
//   - hyperscalers_aws.go: Amazon AWS account IDs, ARNs and
//     hexadecimal resource IDs.
//
// Detection relies on recognized hyperscaler syntax only: ARM paths
// parsed segment by segment, ARNs parsed per service, AWS resource ID
// families with hexadecimal suffixes, Azure CLI identity arguments, a
// fixed set of identity field names, and file scope (every GUID in an
// Azure producer file, every 12-digit number in an AWS producer file).
// A GUID or a 12-digit number elsewhere is never checked on shape
// alone.
//
// OSIRIS JSON Producers Test Data Policy:
// [OSIRIS-PRODUCER-TEST-DATA-POLICY]: https://github.com/osirisjson/osiris-producers/blob/main/docs/policies/OSIRIS-PRODUCER-TEST-DATA-POLICY.md

package testdatapolicy

import (
	"regexp"
	"strings"
)

// scanHyperscalers evaluates the hyperscaler identity rules on one
// line of file p. It returns the findings and the number of
// candidates inspected.
func scanHyperscalers(p, line string) ([]contextFinding, int) {
	az, an := scanAzure(p, line)
	aws, wn := scanAWS(p, line)
	return append(az, aws...), an + wn
}

// isHyperscalerTemplate reports whether v is a placeholder such as %s,
// {rg}, <name> or ... rather than a literal value.
func isHyperscalerTemplate(v string) bool {
	return v == "" || strings.ContainsAny(v, "%{}<>$*") || strings.Contains(v, "...")
}

// isSyntheticHyperscalerName reports whether n starts with LAB, TEST,
// FAKE or STUB, in any case and with or without a separator.
func isSyntheticHyperscalerName(n string) bool {
	l := strings.ToLower(n)
	for _, p := range hyperscalerNamePrefixes {
		if strings.HasPrefix(l, p) {
			return true
		}
	}
	return false
}

// checkHyperscalerName evaluates an invented resource name against the
// synthetic prefix and, for known services, the service's syntax.
// service is an Azure resource type (lowercase) or an AWS service key.
func checkHyperscalerName(service, n string) (string, bool) {
	if !isSyntheticHyperscalerName(n) {
		return "resource name " + display(n) + " is not a synthetic name", false
	}
	var re *regexp.Regexp
	switch service {
	case "storageaccounts":
		re = reAzureStorage
	case "vaults":
		re = reAzureVault
	case "s3":
		re = reS3Bucket
	case "iam":
		re = reIAMName
	case "lambda":
		re = reLambdaName
	}
	if re != nil && !re.MatchString(n) {
		return "resource name " + display(n) + " violates " + service + " naming rules", false
	}
	return "", true
}

// evaluateHyperscalerKeyValue applies the hyperscaler identity field
// rules (tenantId, accountId, resourceGroup and similar keys). The
// second result reports whether the key selected one of those rules.
// The Azure and AWS field names do not overlap; the shared account
// key is told apart by its value, a GUID for Azure and 12 digits for
// AWS.
func evaluateHyperscalerKeyValue(kv keyValue) ([]contextFinding, bool) {
	if f, ok := evaluateAzureKeyValue(kv); ok {
		return f, true
	}
	return evaluateAWSKeyValue(kv)
}

// hyperscalerFinding is a single finding at the value column of kv.
func hyperscalerFinding(kv keyValue, rule, canonical, msg string) []contextFinding {
	return []contextFinding{{col: kv.col, rule: rule, canonical: canonical, message: msg}}
}
