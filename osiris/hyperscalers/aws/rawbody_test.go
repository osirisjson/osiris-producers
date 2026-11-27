// rawbody_test.go - Tests for secret handling (rawbody.go) in the
// OSIRIS JSON Amazon AWS producer: raw body capture and redaction, the
// log-and-redact replacement, the final publication check and AWS
// error text reduction.
//
// Every secret below is a synthetic canary built from documentation
// placeholders. A test fails if any canary reaches serialized output.
//
// For an introduction to OSIRIS JSON Producer for Amazon AWS see:
// [OSIRIS-JSON-AWS]: https://docs.osirisjson.org/osiris-producers/hyperscalers/amazon-aws/

package aws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"go.osirisjson.org/producers/pkg/osirismeta"
	"go.osirisjson.org/producers/pkg/sdk"
	"go.osirisjson.org/producers/pkg/testharness"
)

// Synthetic canaries. Each one either matches an SDK secret value
// pattern or sits under a sensitive key.
const (
	canaryPlain   = "changeme-canary-plain"
	canaryURI     = "https://admin:changeme@example.com/hook"
	canaryURI2    = "ftp://test:secret@example.org/drop"
	canaryUserDat = "IyEvYmluL3NoCmVjaG8gY2hhbmdlbWUK"
)

var allCanaries = []string{canaryPlain, "admin:changeme", "test:secret", canaryUserDat, "changeme"}

func fixedClock() time.Time {
	return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
}

// canaryVPC returns a VPC whose tags carry canaries under a sensitive
// key and in a value.
func canaryVPC() ec2types.Vpc {
	return ec2types.Vpc{
		VpcId:     aws.String("vpc-00000000000000001"),
		CidrBlock: aws.String("192.0.2.0/24"),
		IsDefault: aws.Bool(false),
		OwnerId:   aws.String(testAccountID),
		State:     ec2types.VpcStateAvailable,
		Tags: []ec2types.Tag{
			{Key: aws.String("Name"), Value: aws.String("TEST-VPC")},
			{Key: aws.String("db_password"), Value: aws.String(canaryPlain)},
			{Key: aws.String("webhook"), Value: aws.String(canaryURI)},
		},
	}
}

func assertNoCanary(t *testing.T, label string, out []byte) {
	t.Helper()
	for _, c := range allCanaries {
		if strings.Contains(string(out), c) {
			t.Errorf("%s: canary %q present in output", label, c)
		}
	}
}

// renderVPCDoc runs the document path of Collect on a transformed VPC:
// raw body resolution, fail-closed build, purpose projection and the
// final publication check. It returns the serialized document.
func renderVPCDoc(t *testing.T, purpose osirismeta.Purpose, includeRaw bool) []byte {
	t.Helper()
	ctx := testharness.NewTestContext(t,
		testharness.WithClock(fixedClock),
		testharness.WithConfig(&sdk.ProducerConfig{SafeFailureMode: sdk.FailClosed}),
	)
	resources := TransformVPCs([]ec2types.Vpc{canaryVPC()}, nil, testRegion, testAccountID)
	applyRawBodies(resources, includeRaw && purpose == osirismeta.PurposeAudit, ctx.Clock())

	b := sdk.NewDocumentBuilder(ctx).
		WithGenerator(generatorName, generatorVersion, generatorURL).
		WithScope(sdk.Scope{Name: testAccountID, Providers: []string{providerName}, Purpose: purpose.String()})
	for _, r := range resources {
		b.AddResource(r)
	}
	doc, err := b.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	osirismeta.Project(doc, purpose)
	if err := checkPublishable(doc); err != nil {
		t.Fatalf("checkPublishable: %v", err)
	}
	data, err := sdk.MarshalDocument(doc)
	if err != nil {
		t.Fatalf("MarshalDocument: %v", err)
	}
	return data
}

func TestRenderedDocumentHasNoCanary(t *testing.T) {
	for _, purpose := range []osirismeta.Purpose{osirismeta.PurposeDocumentation, osirismeta.PurposeAudit} {
		for _, raw := range []bool{false, true} {
			label := fmt.Sprintf("purpose=%s raw=%v", purpose, raw)
			t.Run(label, func(t *testing.T) {
				out := renderVPCDoc(t, purpose, raw)
				assertNoCanary(t, label, out)
				hasRaw := strings.Contains(string(out), rawExtensionKey)
				wantRaw := raw && purpose == osirismeta.PurposeAudit
				if hasRaw != wantRaw {
					t.Errorf("raw extension present = %v, want %v", hasRaw, wantRaw)
				}
			})
		}
	}
}

func TestApplyRawBodiesDisabledDropsUnserialized(t *testing.T) {
	resources := []sdk.Resource{{ID: "x"}}
	attachRawBody(&resources[0], map[string]string{"Name": "TEST-VPC"})
	st := applyRawBodies(resources, false, fixedClock())
	if st.Dropped != 1 || st.Attached != 0 {
		t.Fatalf("stats = %+v, want 1 dropped", st)
	}
	if resources[0].Extensions != nil {
		t.Fatalf("Extensions = %v, want nil", resources[0].Extensions)
	}
}

func TestApplyRawBodiesEnvelope(t *testing.T) {
	resources := []sdk.Resource{{ID: "x"}}
	attachRawBody(&resources[0], map[string]any{"Name": "TEST-VPC", "Count": 3})
	st := applyRawBodies(resources, true, fixedClock())
	if st.Attached != 1 {
		t.Fatalf("stats = %+v, want 1 attached", st)
	}
	env, ok := resources[0].Extensions[rawExtensionKey].(map[string]any)
	if !ok {
		t.Fatalf("envelope missing: %#v", resources[0].Extensions)
	}
	if env["schema"] != rawSchema {
		t.Errorf("schema = %v, want %s", env["schema"], rawSchema)
	}
	if env["fetched_at"] != "2026-01-02T03:04:05Z" {
		t.Errorf("fetched_at = %v, want run clock", env["fetched_at"])
	}
	if got, want := env["body"], `{"Count": 3, "Name": "TEST-VPC"}`; got != want {
		t.Errorf("body = %v, want %s", got, want)
	}
}

func TestApplyRawBodiesRejectsUnserializable(t *testing.T) {
	resources := []sdk.Resource{{ID: "x"}}
	attachRawBody(&resources[0], map[string]any{"bad": make(chan int)})
	st := applyRawBodies(resources, true, fixedClock())
	env := resources[0].Extensions[rawExtensionKey].(map[string]any)
	if st.Rejected != 1 || env["rejected"] != "unscannable:json" || env["body"] != nil {
		t.Fatalf("stats = %+v env = %v, want rejected without body", st, env)
	}
}

func TestEncodeRawBodyRedaction(t *testing.T) {
	policy := url.QueryEscape(`{"Statement":[{"Effect":"Allow","Principal":{"AWS":"` + canaryURI2 + `"}}]}`)
	embedded := `{"Endpoint":"` + canaryURI + `"}`
	tests := []struct {
		name     string
		body     any
		want     []string // substrings that must remain
		redacted int
	}{
		{
			name: "sensitive tag element dropped",
			body: map[string]any{"Tags": []any{
				map[string]any{"Key": "Name", "Value": "TEST-VPC"},
				map[string]any{"Key": "api_token", "Value": canaryPlain},
			}},
			want:     []string{"TEST-VPC"},
			redacted: 1,
		},
		{
			name:     "sensitive key replaced, boolean kept",
			body:     map[string]any{"MasterUserPassword": canaryPlain, "IAMDatabaseAuthenticationEnabled": true},
			want:     []string{`"MasterUserPassword": "[REDACTED]"`, `"IAMDatabaseAuthenticationEnabled": true`},
			redacted: 1,
		},
		{
			name:     "denied AWS keys withheld",
			body:     map[string]any{"UserData": canaryUserDat, "PreSharedKey": canaryPlain, "Environment": map[string]any{"Variables": map[string]any{"DB": canaryPlain}}},
			want:     []string{`"UserData": "[REDACTED]"`, `"PreSharedKey": "[REDACTED]"`, `"Environment": "[REDACTED]"`},
			redacted: 3,
		},
		{
			name:     "value pattern in nested array",
			body:     map[string]any{"Rules": []any{[]any{"TEST-RULE", canaryURI2}}},
			want:     []string{"TEST-RULE", `"[REDACTED]"`},
			redacted: 1,
		},
		{
			name:     "url-encoded policy under denied key",
			body:     map[string]any{"AssumeRolePolicyDocument": policy},
			want:     []string{`"AssumeRolePolicyDocument": "[REDACTED]"`},
			redacted: 1,
		},
		{
			name:     "url-encoded json string redacted recursively",
			body:     map[string]any{"Document": policy},
			want:     []string{"Allow"},
			redacted: 1,
		},
		{
			name:     "embedded json string redacted recursively",
			body:     map[string]any{"Config": embedded},
			want:     []string{"Endpoint"},
			redacted: 1,
		},
		{
			name:     "credential uri in description",
			body:     map[string]any{"Description": "TEST " + canaryURI2},
			want:     []string{`"Description": "[REDACTED]"`},
			redacted: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, n, rule := encodeRawBody(tt.body)
			if rule != "" {
				t.Fatalf("rejected with rule %q, body %s", rule, out)
			}
			if n != tt.redacted {
				t.Errorf("redacted = %d, want %d (body %s)", n, tt.redacted, out)
			}
			assertNoCanary(t, tt.name, []byte(out))
			for _, w := range tt.want {
				if !strings.Contains(out, w) {
					t.Errorf("body %s missing %q", out, w)
				}
			}
			if !json.Valid([]byte(out)) {
				t.Errorf("body is not valid JSON: %s", out)
			}
		})
	}
}

func TestEncodeRawBodySpacingAvoidsCrossFieldMatch(t *testing.T) {
	// Compact JSON would read `://a.example.com","Owner":"ops@` as a
	// credential URI spanning two unrelated values.
	body := map[string]any{"A": "https://a.example.com", "Owner": "ops@example.com"}
	out, n, rule := encodeRawBody(body)
	if rule != "" || n != 0 {
		t.Fatalf("rule = %q n = %d body %s, want clean", rule, n, out)
	}
}

func TestRedactElementsLogAndRedact(t *testing.T) {
	resources := []sdk.Resource{
		{
			ID:          "r1",
			Name:        "TEST-RES",
			Description: canaryURI,
			Properties:  map[string]any{"nested": []map[string]string{{"client_secret": canaryPlain}}},
			Tags:        map[string]string{"api_key": canaryPlain, "Owner": "TEST"},
		},
		{ID: "r2", Name: "TEST-CLEAN"},
	}
	conns := []sdk.Connection{{ID: "c1", Source: "r1", Target: "r2", Properties: map[string]any{"auth_header": canaryPlain}}}
	groups := []sdk.Group{{ID: "g1", Description: canaryURI2}}

	if n := redactElements(resources, conns, groups); n != 5 {
		t.Errorf("redactElements count = %d, want 5", n)
	}
	out, _ := json.Marshal(map[string]any{"r": resources, "c": conns, "g": groups})
	assertNoCanary(t, "log-and-redact", out)
	if resources[0].Tags["Owner"] != "TEST" || resources[1].Name != "TEST-CLEAN" {
		t.Errorf("unflagged content changed: %s", out)
	}
}

func TestCheckPublishableCoverage(t *testing.T) {
	ctx := testharness.NewTestContext(t, testharness.WithClock(fixedClock))
	doc, err := sdk.NewDocumentBuilder(ctx).
		WithGenerator(generatorName, generatorVersion, generatorURL).
		WithScope(sdk.Scope{Name: testAccountID, Providers: []string{providerName}}).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	if err := checkPublishable(doc); err != nil {
		t.Fatalf("clean document rejected: %v", err)
	}
	doc.Metadata.Coverage = &sdk.CoverageBlock{Errors: []sdk.CoverageError{{Resource: "vpc", ErrorMessage: canaryURI}}}
	err = checkPublishable(doc)
	if err == nil {
		t.Fatal("document with secret in coverage accepted")
	}
	assertNoCanary(t, "checkPublishable error", []byte(err.Error()))
}

// fakeOperationError and fakeAPIError mimic the AWS SDK error chain.
type fakeOperationError struct{ err error }

func (e fakeOperationError) Error() string {
	return "operation error EC2: DescribeVpcs, https response error StatusCode: 403, RequestID: TEST-REQUEST-ID, " + e.err.Error()
}
func (e fakeOperationError) Unwrap() error     { return e.err }
func (e fakeOperationError) Service() string   { return "EC2" }
func (e fakeOperationError) Operation() string { return "DescribeVpcs" }

type fakeAPIError struct{}

func (fakeAPIError) Error() string {
	return "api error UnauthorizedOperation: User: arn:aws:sts::123456789012:assumed-role/TEST-ROLE/user@example.com is not authorized"
}
func (fakeAPIError) ErrorCode() string { return "UnauthorizedOperation" }

func TestErrorTextReduction(t *testing.T) {
	apiErr := fmt.Errorf("AWS collection failed: %w", fakeOperationError{err: fakeAPIError{}})
	tests := []struct {
		name         string
		err          error
		wantCoverage string
		wantLog      string
	}{
		{"aws api error", apiErr, "EC2 DescribeVpcs: UnauthorizedOperation", "EC2 DescribeVpcs: UnauthorizedOperation"},
		{"canceled", fmt.Errorf("wrap: %w", context.Canceled), "canceled", "wrap: context canceled"},
		{"secret in plain error", errors.New("dial " + canaryURI), "request failed", "error text withheld (credential_uri)"},
		{"plain error", errors.New("dial tcp: no such host"), "request failed", "dial tcp: no such host"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := coverageErrorMessage(tt.err); got != tt.wantCoverage {
				t.Errorf("coverageErrorMessage = %q, want %q", got, tt.wantCoverage)
			}
			if got := logErr(tt.err); got != tt.wantLog {
				t.Errorf("logErr = %q, want %q", got, tt.wantLog)
			}
		})
	}
}

func TestParseFlagsRejectsSafeFailureOff(t *testing.T) {
	_, err := ParseFlags([]string{"--profile", "test", "--safe-failure-mode", "off"})
	if err == nil || !strings.Contains(err.Error(), "fail-closed or log-and-redact") {
		t.Fatalf("err = %v, want rejection of off", err)
	}
}

func TestEncodeRawBodyHTTPTokensSetting(t *testing.T) {
	tests := []struct {
		name, key, value string
		kept             bool
	}{
		{"required", "HttpTokens", "required", true},
		{"optional", "HttpTokens", "optional", true},
		{"other value", "HttpTokens", "changeme", false},
		{"other case", "httptokens", "required", false},
		{"other token key", "ClientToken", "required", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, n, rule := encodeRawBody(map[string]any{"MetadataOptions": map[string]any{tt.key: tt.value}})
			if rule != "" {
				t.Fatalf("body rejected: %s", rule)
			}
			if kept := strings.Contains(body, tt.value); kept != tt.kept || (n == 0) != tt.kept {
				t.Errorf("body = %s (%d redactions), want kept = %v", body, n, tt.kept)
			}
		})
	}
}
