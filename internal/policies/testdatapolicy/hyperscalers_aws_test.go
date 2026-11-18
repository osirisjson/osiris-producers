// hyperscalers_aws_test.go - Positive, negative and boundary tests for
// the Amazon AWS identity rules (sections 5.3 and 5.4): account IDs in
// fields, ARNs and AWS producer files, cross-account ARNs, and
// hexadecimal resource IDs.
//
// The package scans its own sources, so every non-approved sample is
// assembled at runtime by the helpers in hyperscalers_test.go.
//
// OSIRIS JSON Producers Test Data Policy:
// [OSIRIS-PRODUCER-TEST-DATA-POLICY]: https://github.com/osirisjson/osiris-producers/blob/main/docs/policies/OSIRIS-PRODUCER-TEST-DATA-POLICY.md

package testdatapolicy

import (
	"fmt"
	"testing"
)

func TestAWSAccounts(t *testing.T) {
	ok := []string{
		fmt.Sprintf(`{"accountId": %q}`, acctA),
		fmt.Sprintf(`{"OwnerId": %q, "PeerOwnerId": %q}`, acctB, acctC),
		fmt.Sprintf(`{"provider": {"account": %q}}`, acctA),
		arn("iam", "", acctA, "role/LAB-ROLE-01") + " assumed from " + arn("sts", "", acctB, "assumed-role/LAB-ROLE-01/session"),
		arn("iam", "", "aws", "policy/AdministratorAccess"),
		arn("s3", "", "", "lab-test-bucket-01/key"),
		arn("iam", "", acctC, "role/aws-service-role/elasticloadbalancing.amazonaws.com/AWSServiceRoleForElasticLoadBalancing"),
		"arn:aws:s3:::%s",
	}
	for _, l := range ok {
		if got := hyperscalerRules(t, "testdata/x.json", l); len(got) != 0 {
			t.Errorf("%s: %v", l, got)
		}
	}
	other := "2345" + "67890123"
	bad := []struct {
		line string
		want string
	}{
		{fmt.Sprintf(`{"accountId": %q}`, other), RuleAWSAccount},
		{fmt.Sprintf(`{"provider": {"account": %q}}`, other), RuleAWSAccount},
		{arn("ec2", "us-east-1", other, "vpc/vpc-00000000000000001"), RuleAWSAccount},
		{arn("ec2", "us-east-1", "12345", "vpc/vpc-00000000000000001"), RuleAWSAccount},
		{arn("iam", "", acctA, "role/"+"prod-admin"), RuleHyperscalerName},
		{arn("s3", "", "", "Lab_Bucket"), RuleHyperscalerName},
		{arn("lambda", "eu-west-1", acctB, "function:"+"handler"), RuleHyperscalerName},
		{arn("elasticloadbalancing", "eu-west-1", acctB, "loadbalancer/app/"+"web-alb/0123456789abcdef"), RuleHyperscalerName},
	}
	for _, tt := range bad {
		got := hyperscalerRules(t, "testdata/x.json", tt.line)
		if len(got) != 1 || got[0] != tt.want {
			t.Errorf("%s: rules %v, want [%s]", tt.line, got, tt.want)
		}
	}
	// A 12-digit number is an account only in AWS files or AWS fields.
	if got := hyperscalerRules(t, "testdata/x.json", `{"count": `+other+`}`); len(got) != 0 {
		t.Errorf("12-digit number outside AWS context reported: %v", got)
	}
	if got := hyperscalerRules(t, "osiris/hyperscalers/aws/x_test.go", "profile_"+other+",x"); len(got) != 1 {
		t.Errorf("12-digit number in AWS file not reported: %v", got)
	}
	if got := hyperscalerRules(t, "osiris/hyperscalers/aws/x_test.go", "ts := 1"+other); len(got) != 0 {
		t.Errorf("13-digit number reported as account: %v", got)
	}
}

func TestAWSHexIDs(t *testing.T) {
	tests := []struct {
		id string
		ok bool
	}{
		{"vpc-00000000000000001", true},
		{"i-0000000000000ffff", true},
		{"subnet-0000000000000abc1", true},
		{"tgw-attach-00000000000000001", true},
		{"sg-00000001", true},
		{"eni-0000abcd", true},
		{awsID("vpc", "000000000000000010"), false}, // 18 digits
		{awsID("vpc", "0000000000000001"), false},   // 16 digits
		{awsID("vpc", "0a1b0001"), false},           // 8 digits not zero-padded
		{awsID("vpc", "0a1b2c3d4e5f00001"), false},  // 17 digits not zero-padded
		{awsID("vpc", "0000000000000ABC1"), false},  // uppercase
		{awsID("vpc", "abc123"), false},
		{awsID("i", "0abc123def456"), false},
	}
	for _, tt := range tests {
		got := hyperscalerRules(t, "testdata/x.json", fmt.Sprintf(`{"VpcId": %q}`, tt.id))
		if ok := len(got) == 0; ok != tt.ok {
			t.Errorf("%s: rules %v, want ok=%v", tt.id, got, tt.ok)
		}
	}
	for _, l := range []string{"multi-0000abcd", "LAB-acl-0000abcd", "acl-add rule", "i-face"} {
		if got := hyperscalerRules(t, "testdata/x.json", l); len(got) != 0 {
			t.Errorf("%q reported as an AWS ID: %v", l, got)
		}
	}
}
