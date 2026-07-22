// tty.go - Interactive credential prompts for the HPE Aruba Networking
// Central OSIRIS JSON producer.
//
// These prompts exist so
// client_id/client_secret/access_token/refresh_token never need to be
// passed as CLI flags (visible in `ps` output and shell history) or
// logged: values entered here live only in memory for the duration of
// the run and are never written to disk unless the user also
// passed --token-file.
//
// OSIRIS JSON Producer for HPE Aruba Networking Central introduction:
// [OSIRIS-JSON-HPE-ARUBA-NETWORKING]: https://docs.osirisjson.org/osiris-producers/network/hpe-aruba-networking-central

package arubacentral

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// openTTY opens the controlling terminal for interactive prompts.
// Returns an error when none is available (e.g. running under cron/CI
// with no tty)callers can fail with a clear message instead of hanging.
func openTTY() (*os.File, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("no interactive terminal available (cannot open /dev/tty): %w", err)
	}
	return tty, nil
}

// promptVisible prompts for a non-secret value (e.g. client_id) via
// /dev/tty with normal echo, returning the trimmed input.
func promptVisible(prompt string) (string, error) {
	tty, err := openTTY()
	if err != nil {
		return "", err
	}
	defer tty.Close()

	fmt.Fprint(tty, prompt)
	line, err := bufio.NewReader(tty).ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("reading input: %w", err)
	}
	return strings.TrimSpace(line), nil
}

// promptHidden prompts for a secret value (client_secret, access_token,
// refresh_token) via /dev/tty with echo disabled.
func promptHidden(prompt string) (string, error) {
	tty, err := openTTY()
	if err != nil {
		return "", err
	}
	defer tty.Close()

	fmt.Fprint(tty, prompt)
	value, err := term.ReadPassword(int(tty.Fd()))
	fmt.Fprintln(tty) // newline after hidden input.
	if err != nil {
		return "", fmt.Errorf("reading input: %w", err)
	}
	return string(value), nil
}

// resolveCredentialsInteractive fills in any still-missing credential
// fields (after --token-file and environment variables have already
// be applied) by prompting on the controlling terminal, so client_id,
// client_secret, access_token and refresh_token never need to be passed
// as CLI flags. client_id is prompted visibly (it is an identifier,
// not a secret, comparable to a username); client_secret, access_token
// and refresh_token are prompted hidden.
//
// Once client_id/client_secret are known, GreenLake Personal API client
// (see personal_api.go) is tried before ever asking for an access token
// directly: a Personal API client only has a client_id/client_secret
// pair (no pre-issued access/refresh token), so requiring one to be
// pasted in would leave user stuck.
//
// An access token (whether supplied, minted, or typed) is the only
// field required overall: if it is still missing after that and no
// terminal is available to ask for it (e.g. running under cron/CI),
// this returns an error. client_id, client_secret and refresh_token
// are otherwise optional; when no terminal is available for them,
// they are silently left blank rather than failing the run, preserving
// today's headless/env-var-only automation.
//
// Nothing entered here is written to disk: the returned Credentials has
// TokenFile left as passed in, so saveTokenFile only ever fires for
// values that came from an explicit --token-file.
func resolveCredentialsInteractive(creds Credentials) (Credentials, error) {
	return resolveCredentials(creds, promptVisible, promptHidden, defaultMintPersonalAPIToken)
}

// resolveCredentials is resolveCredentialsInteractive with the prompt
// and token-minting functions injected, so tests can exercise the
// "still missing after token-file/env vars" branches without touching
// a real terminal or network.
func resolveCredentials(creds Credentials, askVisible, askHidden func(string) (string, error), mintToken func(clientID, clientSecret string) (string, error)) (Credentials, error) {
	if creds.ClientID == "" {
		if v, err := askVisible("Aruba Central / HPE GreenLake API client ID (optional, press Enter to skip): "); err == nil {
			creds.ClientID = v
		}
	}
	if creds.ClientSecret == "" {
		if v, err := askHidden("Aruba Central / HPE GreenLake API client secret (optional, press Enter to skip): "); err == nil {
			creds.ClientSecret = v
		}
	}

	if creds.AccessToken == "" && isPersonalAPIClient(creds) {
		token, err := mintToken(creds.ClientID, creds.ClientSecret)
		if err != nil {
			return creds, fmt.Errorf("minting an access token from the GreenLake Personal API client failed: %w", err)
		}
		creds.AccessToken = token
	}

	if creds.AccessToken == "" {
		v, err := askHidden("Aruba Central access token: ")
		if err != nil {
			return creds, fmt.Errorf("access token not supplied via --token-file/ARUBA_CENTRAL_ACCESS_TOKEN, could not be minted from a client ID/secret, and could not be prompted for: %w", err)
		}
		if v == "" {
			return creds, fmt.Errorf("an access token is required")
		}
		creds.AccessToken = v
	}

	// A Personal API client has no refresh_token to give
	// (Client.refresh re-mints from client_id/client_secret instead),
	// so skip asking for one.
	if creds.RefreshToken == "" && !isPersonalAPIClient(creds) {
		if v, err := askHidden("Aruba Central refresh token (optional, press Enter to skip): "); err == nil {
			creds.RefreshToken = v
		}
	}
	return creds, nil
}
