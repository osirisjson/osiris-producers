// tty_test.go - Unit tests for interactive credential resolution.
// The real /dev/tty prompt functions (and the GreenLake token-minting
// network call) are swapped for fakes so these tests don't depend on
// (or touch) an actual terminal or network.
//
// OSIRIS JSON Producer for HPE Aruba Networking Central introduction:
// [OSIRIS-JSON-HPE-ARUBA-NETWORKING]: https://docs.osirisjson.org/osiris-producers/network/hpe-aruba-networking

package arubacentral

import (
	"errors"
	"testing"
)

func failMint(clientID, clientSecret string) (string, error) {
	return "", errors.New("mintToken should not be called")
}

func TestResolveCredentials_AllPresentSkipsPrompts(t *testing.T) {
	creds := Credentials{ClientID: "id", ClientSecret: "secret", AccessToken: "token", RefreshToken: "refresh"}

	calls := 0
	fail := func(string) (string, error) { calls++; return "", errors.New("should not be called") }

	got, err := resolveCredentials(creds, fail, fail, failMint)
	if err != nil {
		t.Fatalf("expected no error when all fields are already present, got %v", err)
	}
	if calls != 0 {
		t.Errorf("expected no prompts when nothing is missing, got %d calls", calls)
	}
	if got != creds {
		t.Errorf("expected credentials to pass through unchanged, got %+v", got)
	}
}

func TestResolveCredentials_FillsMissingClientCredentials(t *testing.T) {
	creds := Credentials{AccessToken: "token"} // only the required field present

	askVisible := func(prompt string) (string, error) { return "prompted-id", nil }
	askHidden := func(prompt string) (string, error) {
		switch prompt {
		case "Aruba Central / HPE GreenLake API client secret (optional, press Enter to skip): ":
			return "prompted-secret", nil
		}
		t.Fatalf("unexpected hidden prompt: %q", prompt)
		return "", nil
	}

	// Once client_id and client_secret are both filled in with no
	// refresh_token, that combination reads as a GreenLake Personal API
	// client (see isPersonalAPIClient), so the refresh-token prompt is
	// deliberately skipped.
	// A Personal API client has no refresh token to give.
	got, err := resolveCredentials(creds, askVisible, askHidden, failMint)
	if err != nil {
		t.Fatalf("resolveCredentials failed: %v", err)
	}
	if got.ClientID != "prompted-id" || got.ClientSecret != "prompted-secret" {
		t.Errorf("expected missing client_id/client_secret to be filled from prompts, got %+v", got)
	}
	if got.RefreshToken != "" {
		t.Errorf("expected no refresh-token prompt once client_id/client_secret are both present, got %q", got.RefreshToken)
	}
	if got.AccessToken != "token" {
		t.Errorf("expected the already-present access token to be left untouched, got %q", got.AccessToken)
	}
}

func TestResolveCredentials_RefreshTokenStillPromptedWithoutClientCredentials(t *testing.T) {
	creds := Credentials{AccessToken: "token"}

	askVisible := func(string) (string, error) { return "", nil } // client_id skipped
	askHidden := func(prompt string) (string, error) {
		if prompt == "Aruba Central refresh token (optional, press Enter to skip): " {
			return "prompted-refresh", nil
		}
		return "", nil // client_secret skipped
	}

	got, err := resolveCredentials(creds, askVisible, askHidden, failMint)
	if err != nil {
		t.Fatalf("resolveCredentials failed: %v", err)
	}
	if got.RefreshToken != "prompted-refresh" {
		t.Errorf("expected the refresh-token prompt to still run without both client_id and client_secret, got %+v", got)
	}
}

func TestResolveCredentials_OptionalFieldsToleratesNoTerminal(t *testing.T) {
	creds := Credentials{AccessToken: "token"}
	noTTY := func(string) (string, error) { return "", errors.New("no interactive terminal available") }

	got, err := resolveCredentials(creds, noTTY, noTTY, failMint)
	if err != nil {
		t.Fatalf("expected no error: optional fields must not fail the run when no terminal is available, got %v", err)
	}
	if got.ClientID != "" || got.ClientSecret != "" || got.RefreshToken != "" {
		t.Errorf("expected optional fields to stay blank, got %+v", got)
	}
}

func TestResolveCredentials_MissingAccessTokenWithNoTerminalErrors(t *testing.T) {
	noTTY := func(string) (string, error) { return "", errors.New("no interactive terminal available") }

	_, err := resolveCredentials(Credentials{}, noTTY, noTTY, failMint)
	if err == nil {
		t.Fatal("expected an error when the required access token cannot be prompted for")
	}
}

func TestResolveCredentials_BlankAccessTokenIsRejected(t *testing.T) {
	askHidden := func(string) (string, error) { return "", nil } // user just pressed Enter
	askVisible := func(string) (string, error) { return "", nil }

	_, err := resolveCredentials(Credentials{}, askVisible, askHidden, failMint)
	if err == nil {
		t.Fatal("expected an error when the access token prompt is left blank")
	}
}

func TestResolveCredentials_MintsAccessTokenFromPersonalAPIClient(t *testing.T) {
	creds := Credentials{ClientID: "id", ClientSecret: "secret"} // no access_token, no refresh_token

	refreshPromptCalls := 0
	askHidden := func(prompt string) (string, error) {
		if prompt == "Aruba Central refresh token (optional, press Enter to skip): " {
			refreshPromptCalls++
		}
		return "", errors.New("should not be prompted")
	}
	askVisible := func(string) (string, error) { return "", errors.New("should not be prompted") }
	mint := func(clientID, clientSecret string) (string, error) {
		if clientID != "id" || clientSecret != "secret" {
			t.Fatalf("mintToken called with unexpected creds: %q/%q", clientID, clientSecret)
		}
		return "minted-token", nil
	}

	got, err := resolveCredentials(creds, askVisible, askHidden, mint)
	if err != nil {
		t.Fatalf("resolveCredentials failed: %v", err)
	}
	if got.AccessToken != "minted-token" {
		t.Errorf("expected the access token to come from mintToken, got %q", got.AccessToken)
	}
	if refreshPromptCalls != 0 {
		t.Error("expected no refresh-token prompt for a GreenLake Personal API client")
	}
}

func TestResolveCredentials_MintFailurePropagatesError(t *testing.T) {
	creds := Credentials{ClientID: "id", ClientSecret: "secret"}
	askHidden := func(string) (string, error) { return "", errors.New("should not be prompted") }
	askVisible := func(string) (string, error) { return "", errors.New("should not be prompted") }
	mint := func(clientID, clientSecret string) (string, error) {
		return "", errors.New("invalid client credentials")
	}

	_, err := resolveCredentials(creds, askVisible, askHidden, mint)
	if err == nil {
		t.Fatal("expected an error when minting a Personal API client token fails")
	}
}
