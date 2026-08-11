// tty.go - Interactive prompts for Cisco OSIRIS JSON Producer.
// Opens /dev/tty directly (rather than reading stdin) so prompting
// still works when stdin is piped. PromptPassword hides input for
// secrets; PromptVisible echoes input normally for non-secret values
// like host and username, used by the same credential fallback chain
// (flag, then --secrets-file, then interactive prompt) that
// ParseFlags applies in flags.go.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package run

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// PromptPassword prompts the user for a password via /dev/tty with
// echo disabled. The prompt string is written to stderr so it appears
// even when stdout is redirected.
func PromptPassword(prompt string) (string, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "", fmt.Errorf("cannot open /dev/tty: %w", err)
	}
	defer tty.Close()

	fmt.Fprint(tty, prompt)

	password, err := term.ReadPassword(int(tty.Fd()))
	fmt.Fprintln(tty) // newline after hidden input.
	if err != nil {
		return "", fmt.Errorf("reading password: %w", err)
	}
	return string(password), nil
}

// PromptVisible prompts for a non-secret value (host, username) via
// /dev/tty with normal echo, returning the trimmed input.
func PromptVisible(prompt string) (string, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "", fmt.Errorf("no interactive terminal available (cannot open /dev/tty): %w", err)
	}
	defer tty.Close()

	fmt.Fprint(tty, prompt)
	line, err := bufio.NewReader(tty).ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("reading input: %w", err)
	}
	return strings.TrimSpace(line), nil
}
