// flags_test.go - Unit tests for IOS-XE CLI flag parsing and credential
// resolution. No password ever rides on the command line, and the
// resolution order is secrets-file, then environment, then
// interactive prompt.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package iosxe

import (
	"flag"
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
