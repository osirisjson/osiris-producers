// hyperscalers_aws.go - Amazon AWS identity rules of the test data
// policy scanner (sections 5.3, 5.4 and 5.5): account IDs, ARNs with
// their account and resource name components, and hexadecimal
// resource IDs such as vpc-00000000000000001.
//
// Every 12-digit number in an AWS producer file is an account
// candidate. A 12-digit number elsewhere is checked only in an AWS
// account field or the account component of an ARN.
//
// OSIRIS JSON Producers Test Data Policy:
// [OSIRIS-PRODUCER-TEST-DATA-POLICY]: https://github.com/osirisjson/osiris-producers/blob/main/docs/policies/OSIRIS-PRODUCER-TEST-DATA-POLICY.md

package testdatapolicy

import (
	"regexp"
	"strings"
)

var (
	// reARN matches an AWS ARN: partition, service, region, account,
	// resource.
	reARN = regexp.MustCompile("arn:(aws[a-z-]*):([a-z0-9-]*):([a-z0-9-]*):([^:\\s\"'<>`,;)\\\\]*):([^\\s\"'<>`,;)\\\\]+)")

	// reAWSHexID matches a resource ID of a family with a hexadecimal
	// suffix. Group 1 is the family and group 2 the suffix.
	reAWSHexID     = regexp.MustCompile(`(` + strings.Join(awsHexFamilies, "|") + `)-([0-9a-fA-F]+)`)
	reDigits       = regexp.MustCompile(`[0-9]+`)
	reTwelveDigits = regexp.MustCompile(`^[0-9]{12}$`)

	// AWS naming restrictions applied to invented names.
	reS3Bucket   = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)
	reIAMName    = regexp.MustCompile(`^[\w+=,.@-]{1,64}$`)
	reLambdaName = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
)

// isAWSFile reports whether p belongs to the AWS producer, where every
// 12-digit number is an account candidate.
func isAWSFile(p string) bool {
	return strings.Contains(p, "hyperscalers/aws/") || strings.HasPrefix(p, "cmd/osirisjson-producer-aws/")
}

// checkAWSAccount evaluates a 12-digit AWS account ID.
func checkAWSAccount(a string) (string, bool) {
	if approvedAWSAccounts[a] {
		return "", true
	}
	return "AWS account " + display(a) + " is not an approved example account", false
}

// checkAWSHexID evaluates the suffix of an AWS resource ID from a
// hexadecimal family.
func checkAWSHexID(id, suffix string) (string, bool) {
	switch {
	case suffix != strings.ToLower(suffix):
		return "AWS resource ID " + display(id) + " must be lowercase", false
	case len(suffix) == 17 && strings.HasPrefix(suffix, strings.Repeat("0", 13)):
		return "", true
	case len(suffix) == 8 && strings.HasPrefix(suffix, "0000"):
		return "", true
	case len(suffix) == 17 || len(suffix) == 8:
		return "AWS resource ID " + display(id) + " is not zero-padded", false
	}
	return "AWS resource ID " + display(id) + " has an invalid length", false
}

// scanAWS evaluates the AWS identity rules on one line of file p:
// ARNs, hexadecimal resource IDs and, in AWS producer files, every
// 12-digit number. It returns the findings and the number of
// candidates inspected.
func scanAWS(p, line string) ([]contextFinding, int) {
	var out []contextFinding
	n := 0
	add := func(col int, rule, canonical, msg string) {
		out = append(out, contextFinding{col: col, rule: rule, canonical: canonical, message: msg})
	}

	for _, m := range reARN.FindAllStringSubmatchIndex(line, -1) {
		n++
		out = append(out, checkARN(line, m)...)
	}
	for _, m := range reAWSHexID.FindAllStringSubmatchIndex(line, -1) {
		start, end := m[0], m[1]
		if start > 0 && (isWordByte(line[start-1]) || line[start-1] == '-') {
			continue
		}
		if end < len(line) && (isWordByte(line[end]) || line[end] == '-') {
			continue
		}
		suffix := line[m[4]:m[5]]
		if len(suffix) < 8 && !strings.ContainsAny(suffix, "0123456789") {
			continue // a word such as acl-add, not an ID
		}
		if len(suffix) < 3 {
			continue
		}
		n++
		id := line[start:end]
		if msg, ok := checkAWSHexID(id, suffix); !ok {
			add(start+1, RuleAWSResource, id, msg)
		}
	}
	if isAWSFile(p) {
		for _, m := range reDigits.FindAllStringIndex(line, -1) {
			start, end := m[0], m[1]
			if end-start != 12 || start > 0 && line[start-1] == '.' {
				continue
			}
			if end < len(line) && line[end] == '.' && end+1 < len(line) && line[end+1] >= '0' && line[end+1] <= '9' {
				continue
			}
			n++
			a := line[start:end]
			if msg, ok := checkAWSAccount(a); !ok {
				add(start+1, RuleAWSAccount, a, msg)
			}
		}
	}
	return out, n
}

// checkARN validates the account component and, for services with a
// known grammar, the resource name of one ARN match.
func checkARN(line string, m []int) []contextFinding {
	var out []contextFinding
	add := func(col int, rule, canonical, msg string) {
		out = append(out, contextFinding{col: col, rule: rule, canonical: canonical, message: msg})
	}
	service := line[m[4]:m[5]]
	account := line[m[8]:m[9]]
	res := line[m[10]:m[11]]
	resCol := m[10] + 1

	awsOwned := account == "aws"
	switch {
	case account == "" || awsOwned || isHyperscalerTemplate(account):
	case reTwelveDigits.MatchString(account):
		if msg, ok := checkAWSAccount(account); !ok {
			add(m[8]+1, RuleAWSAccount, account, msg)
		}
	default:
		add(m[8]+1, RuleAWSAccount, account, "ARN account component "+display(account)+" is not a 12-digit account ID")
	}
	if isHyperscalerTemplate(res) {
		return out
	}

	name, svc := "", ""
	switch service {
	case "iam":
		typ, rest, _ := strings.Cut(res, "/")
		switch typ {
		case "role", "user", "group", "policy", "instance-profile":
			if awsOwned || strings.Contains(rest, "aws-service-role/") {
				return out
			}
			parts := strings.Split(rest, "/")
			name, svc = parts[len(parts)-1], "iam"
			if strings.HasPrefix(name, "AWSServiceRole") {
				return out
			}
		}
	case "sts":
		if r, ok := strings.CutPrefix(res, "assumed-role/"); ok {
			name, _, _ = strings.Cut(r, "/")
			svc = "iam"
		}
	case "s3":
		name, _, _ = strings.Cut(res, "/")
		svc = "s3"
	case "lambda":
		if f, ok := strings.CutPrefix(res, "function:"); ok {
			name, _, _ = strings.Cut(f, ":")
			svc = "lambda"
		}
	case "elasticloadbalancing":
		parts := strings.Split(res, "/")
		switch {
		case len(parts) >= 3 && parts[0] == "loadbalancer":
			name = parts[2]
		case len(parts) >= 2 && parts[0] == "targetgroup":
			name = parts[1]
		}
	}
	if name == "" {
		return out
	}
	if msg, ok := checkHyperscalerName(svc, name); !ok {
		add(resCol+strings.Index(res, name), RuleHyperscalerName, name, msg)
	}
	return out
}

// evaluateAWSKeyValue applies the AWS identity field rules: account
// fields (accountId, ownerId and similar, plus an account field
// holding 12 digits) and, in AWS and Azure producer files, the
// resource name fields bucketName, roleName and functionName. The
// second result reports whether the key selected one of those rules.
func evaluateAWSKeyValue(kv keyValue) ([]contextFinding, bool) {
	k, v := kv.key, strings.TrimSpace(kv.value)
	if (awsAccountKeys[k] || k == "account") && reTwelveDigits.MatchString(v) {
		if msg, ok := checkAWSAccount(v); !ok {
			return hyperscalerFinding(kv, RuleAWSAccount, v, msg), true
		}
		return nil, true
	}
	if svc, ok := awsNameKeys[k]; ok && kv.hyperscalersFile {
		if isHyperscalerTemplate(v) || skipFreeText(v) {
			return nil, true
		}
		if msg, ok := checkHyperscalerName(svc, v); !ok {
			return hyperscalerFinding(kv, RuleHyperscalerName, v, msg), true
		}
		return nil, true
	}
	return nil, false
}
