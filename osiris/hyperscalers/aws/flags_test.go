// flags_test.go - Tests for account selection (--account, flags.go) in
// the OSIRIS JSON Amazon AWS producer.
//
// For an introduction to OSIRIS JSON Producer for Amazon AWS see:
// [OSIRIS-JSON-AWS]: https://docs.osirisjson.org/osiris-producers/hyperscalers/amazon-aws/

package aws

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// stubProfiles points profile discovery at synthetic AWS files.
func stubProfiles(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	config := filepath.Join(dir, "config")
	creds := filepath.Join(dir, "credentials")
	write := func(path, body string) {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(config, `[default]
[sso-session test]
[profile TEST-A_ReadOnlyAccess]
[profile TEST-B_ReadOnlyAccess]
[profile TEST-B_AdministratorAccess]
[profile TEST-C]
[profile TEST-C_ReadOnlyAccess]
`)
	write(creds, "[TEST-D]\n")
	t.Setenv("AWS_CONFIG_FILE", config)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", creds)
}

func TestParseFlagsAccount(t *testing.T) {
	stubProfiles(t)
	tests := []struct {
		name    string
		args    []string
		want    []string
		regions []string
		wantErr string
	}{
		{"short flag single", []string{"-A", "TEST-D", "-R", "eu-west-1"}, []string{"TEST-D"}, []string{"eu-west-1"}, ""},
		{"account name", []string{"-A", "TEST-A"}, []string{"TEST-A_ReadOnlyAccess"}, nil, ""},
		{"exact name wins", []string{"-A", "TEST-C"}, []string{"TEST-C"}, nil, ""},
		{"several in order", []string{"--account", "TEST-D, TEST-A,", "-R", "eu-west-1,us-east-1", "-o", "out"}, []string{"TEST-D", "TEST-A_ReadOnlyAccess"}, []string{"eu-west-1", "us-east-1"}, ""},
		{"all regions", []string{"-A", "TEST-D,TEST-A", "--all-regions", "-o", "out"}, []string{"TEST-D", "TEST-A_ReadOnlyAccess"}, DefaultRegions, ""},
		{"duplicate", []string{"-A", "TEST-A,TEST-A_ReadOnlyAccess"}, []string{"TEST-A_ReadOnlyAccess"}, nil, ""},
		{"several without output", []string{"-A", "TEST-D,TEST-A"}, nil, nil, "require --output"},
		{"unknown", []string{"-A", "TEST-D,TEST-X", "-o", "out"}, nil, nil, `"TEST-X": no such`},
		{"ambiguous", []string{"-A", "TEST-B"}, nil, nil, "matches several"},
		{"same file name", []string{"-A", "TEST-B_ReadOnlyAccess,TEST-B_AdministratorAccess", "-o", "out"}, nil, nil, "same file name"},
		{"empty list", []string{"-A", " , "}, nil, nil, "no account profile"},
		{"with profile", []string{"-A", "TEST-D", "-P", "TEST-D"}, nil, nil, "cannot be combined"},
		{"with source", []string{"-A", "TEST-D", "-s", "x.csv", "-o", "out"}, nil, nil, "cannot be combined"},
		{"invalid region", []string{"-A", "TEST-D", "-R", "../x"}, nil, nil, "invalid region"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := ParseFlags(tt.args)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, target := range cfg.Targets {
				got = append(got, target.Profile)
				if !slices.Equal(target.Regions, tt.regions) {
					t.Errorf("%s regions = %v, want %v", target.Profile, target.Regions, tt.regions)
				}
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("profiles = %v, want %v", got, tt.want)
			}
		})
	}
}
