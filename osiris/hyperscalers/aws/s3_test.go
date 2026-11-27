// s3_test.go - Tests for S3 bucket security settings in the OSIRIS
// JSON Amazon AWS producer: Client.readBucketSecurity and
// TransformS3Buckets. A setting whose read fails is unverified and is
// never reported as disabled.
//
// For an introduction to OSIRIS JSON Producer for Amazon AWS see:
// [OSIRIS-JSON-AWS]: https://docs.osirisjson.org/osiris-producers/hyperscalers/amazon-aws/

package aws

import (
	"context"
	"io"
	"log/slog"
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// codeError is an AWS API error carrying only an error code.
type codeError string

func (e codeError) Error() string     { return "api error " + string(e) }
func (e codeError) ErrorCode() string { return string(e) }

// fakeS3 answers the three security reads with canned results.
type fakeS3 struct {
	versioning    *s3.GetBucketVersioningOutput
	versioningErr error
	encryption    *s3.GetBucketEncryptionOutput
	encryptionErr error
	pab           *s3.GetPublicAccessBlockOutput
	pabErr        error
}

func (f fakeS3) GetBucketVersioning(context.Context, *s3.GetBucketVersioningInput, ...func(*s3.Options)) (*s3.GetBucketVersioningOutput, error) {
	return f.versioning, f.versioningErr
}

func (f fakeS3) GetBucketEncryption(context.Context, *s3.GetBucketEncryptionInput, ...func(*s3.Options)) (*s3.GetBucketEncryptionOutput, error) {
	return f.encryption, f.encryptionErr
}

func (f fakeS3) GetPublicAccessBlock(context.Context, *s3.GetPublicAccessBlockInput, ...func(*s3.Options)) (*s3.GetPublicAccessBlockOutput, error) {
	return f.pab, f.pabErr
}

func allBlocked() *s3.GetPublicAccessBlockOutput {
	return &s3.GetPublicAccessBlockOutput{PublicAccessBlockConfiguration: &s3types.PublicAccessBlockConfiguration{
		BlockPublicAcls: aws.Bool(true), IgnorePublicAcls: aws.Bool(true),
		BlockPublicPolicy: aws.Bool(true), RestrictPublicBuckets: aws.Bool(true),
	}}
}

func kmsEncryption() *s3.GetBucketEncryptionOutput {
	return &s3.GetBucketEncryptionOutput{ServerSideEncryptionConfiguration: &s3types.ServerSideEncryptionConfiguration{
		Rules: []s3types.ServerSideEncryptionRule{{ApplyServerSideEncryptionByDefault: &s3types.ServerSideEncryptionByDefault{
			SSEAlgorithm: s3types.ServerSideEncryptionAwsKms,
		}}},
	}}
}

func TestReadBucketSecurity(t *testing.T) {
	denied := codeError("AccessDenied")
	tests := []struct {
		name           string
		api            fakeS3
		wantUnverified []string
		wantVersioning string
		wantBlock      bool
		wantAlgorithm  string
	}{
		{
			name:           "all read",
			api:            fakeS3{versioning: &s3.GetBucketVersioningOutput{Status: s3types.BucketVersioningStatusEnabled}, encryption: kmsEncryption(), pab: allBlocked()},
			wantVersioning: "Enabled", wantBlock: true, wantAlgorithm: "aws:kms",
		},
		{
			name:      "not configured is off, not unverified",
			api:       fakeS3{versioning: &s3.GetBucketVersioningOutput{}, encryptionErr: codeError("ServerSideEncryptionConfigurationNotFoundError"), pabErr: codeError("NoSuchPublicAccessBlockConfiguration")},
			wantBlock: false,
		},
		{
			name:           "access denied is unverified",
			api:            fakeS3{versioningErr: denied, encryptionErr: denied, pabErr: denied},
			wantUnverified: []string{"versioning", "encryption", "block_public_access"},
		},
		{
			name:           "one denied",
			api:            fakeS3{versioning: &s3.GetBucketVersioningOutput{Status: s3types.BucketVersioningStatusSuspended}, encryption: kmsEncryption(), pabErr: denied},
			wantUnverified: []string{"block_public_access"},
			wantVersioning: "Suspended", wantAlgorithm: "aws:kms",
		},
	}
	c := &Client{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var info S3BucketInfo
			c.readBucketSecurity(context.Background(), tt.api, "test-bucket", &info)
			if !reflect.DeepEqual(info.Unverified, tt.wantUnverified) {
				t.Errorf("Unverified = %v, want %v", info.Unverified, tt.wantUnverified)
			}
			if info.Versioning != tt.wantVersioning || info.BlockPublicAccess != tt.wantBlock || info.EncryptionAlgorithm != tt.wantAlgorithm {
				t.Errorf("info = %+v", info)
			}
		})
	}
}

func TestTransformS3BucketsUnverified(t *testing.T) {
	buckets := []S3BucketInfo{
		{Name: "test-bucket-read", Region: testRegion, Versioning: "", BlockPublicAccess: false},
		{Name: "test-bucket-denied", Region: testRegion, Unverified: []string{"versioning", "block_public_access", "encryption"}},
	}
	rs := TransformS3Buckets(buckets, testRegion, testAccountID)
	if len(rs) != 2 {
		t.Fatalf("resources = %d", len(rs))
	}
	read, denied := rs[0].Properties, rs[1].Properties
	if read["versioning"] != "Disabled" || read["block_public_access"] != false {
		t.Errorf("read bucket props = %v", read)
	}
	if _, ok := read["unverified_settings"]; ok {
		t.Errorf("read bucket has unverified_settings: %v", read)
	}
	for _, k := range []string{"versioning", "block_public_access", "encryption_algorithm"} {
		if _, ok := denied[k]; ok {
			t.Errorf("denied bucket reports %s = %v", k, denied[k])
		}
	}
	if got := denied["unverified_settings"]; !reflect.DeepEqual(got, []string{"block_public_access", "encryption", "versioning"}) {
		t.Errorf("unverified_settings = %v", got)
	}
}
