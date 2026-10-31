// flags_test.go - Unit tests for IOS-XE CLI flag parsing and credential
// resolution. No password ever rides on the command line and the
// resolution order is secrets-file, then environment, then
// interactive prompt.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package iosxe

import (
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRegisterFlags_NoCommandLinePasswordFlag(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	registerFlags(fs)

	for _, name := range []string{"p", "password", "pass", "pw", "secret"} {
		if f := fs.Lookup(name); f != nil {
			t.Errorf("flag -%s MUST NOT exist: a command-line secret leaks via ps(1) and shell history", name)
		}
	}
}

func TestResolvePassword_Precedence(t *testing.T) {
	t.Run("secrets-file value wins over environment", func(t *testing.T) {
		t.Setenv(passwordEnvVar, "from-env")
		got, err := resolvePassword("from-file", nil)
		if err != nil {
			t.Fatalf("resolvePassword: %v", err)
		}
		if got != "from-file" {
			t.Errorf("got %q, want the secrets-file value", got)
		}
	})

	t.Run("environment used when secrets-file omits it", func(t *testing.T) {
		t.Setenv(passwordEnvVar, "from-env")
		got, err := resolvePassword("", nil)
		if err != nil {
			t.Fatalf("resolvePassword: %v", err)
		}
		if got != "from-env" {
			t.Errorf("got %q, want the environment value", got)
		}
	})

	t.Run("interactive prompt is the last resort", func(t *testing.T) {
		t.Setenv(passwordEnvVar, "") // empty is treated as unset.
		var prompted bool
		got, err := resolvePassword("", func(string) (string, error) {
			prompted = true
			return "from-prompt", nil
		})
		if err != nil {
			t.Fatalf("resolvePassword: %v", err)
		}
		if !prompted {
			t.Error("expected the interactive prompt to be invoked")
		}
		if got != "from-prompt" {
			t.Errorf("got %q, want the prompted value", got)
		}
	})

	t.Run("no source yields an empty password, not an error", func(t *testing.T) {
		t.Setenv(passwordEnvVar, "")
		got, err := resolvePassword("", nil)
		if err != nil {
			t.Fatalf("resolvePassword: %v", err)
		}
		if got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
}

func TestParseFlags_Transport(t *testing.T) {
	t.Setenv(passwordEnvVar, "secret")
	for _, tt := range []struct {
		args    []string
		want    string
		wantErr bool
	}{
		{[]string{"-h", "192.0.2.1", "-u", "admin"}, transportAuto, false},
		{[]string{"-h", "192.0.2.1", "-u", "admin", "--transport", "netconf"}, transportNETCONF, false},
		{[]string{"-h", "192.0.2.1", "-u", "admin", "--transport", "restconf"}, transportRESTCONF, false},
		{[]string{"-h", "192.0.2.1", "-u", "admin", "--transport", "https"}, "", true},
	} {
		cfg, err := ParseFlags(tt.args, nil, nil)
		if tt.wantErr {
			if err == nil {
				t.Errorf("%v: expected an error", tt.args)
			}
			continue
		}
		if err != nil || cfg.Transport != tt.want {
			t.Errorf("%v: transport %q, err %v; want %q", tt.args, cfg.Transport, err, tt.want)
		}
	}
}

func TestParseFlags_RawBodyRequiresAudit(t *testing.T) {
	t.Setenv(passwordEnvVar, "test")
	base := []string{"-h", "192.0.2.1", "-u", "admin", "--include-raw-body"}
	for _, args := range [][]string{base, append(base, "--purpose", "documentation")} {
		if _, err := ParseFlags(args, nil, nil); err == nil || !strings.Contains(err.Error(), "--include-raw-body requires --purpose audit") {
			t.Errorf("ParseFlags(%v) error = %v, want the audit requirement", args, err)
		}
	}
	cfg, err := ParseFlags(append(base, "--purpose", "audit"), nil, nil)
	if err != nil {
		t.Fatalf("ParseFlags with --purpose audit: %v", err)
	}
	if !cfg.IncludeRawBody || cfg.Purpose != "audit" {
		t.Errorf("cfg = %+v", cfg)
	}
}

func TestWriteOwnerOnly_TightensExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "document.json")
	if err := os.WriteFile(path, []byte("old content, longer than the new one"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeOwnerOnly(path, []byte("{}\n")); err != nil {
		t.Fatalf("writeOwnerOnly: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != outputFileMode {
		t.Errorf("mode = %v, want %v", info.Mode().Perm(), os.FileMode(outputFileMode))
	}
	if b, _ := os.ReadFile(path); string(b) != "{}\n" {
		t.Errorf("content = %q, want the new document only", b)
	}
}
