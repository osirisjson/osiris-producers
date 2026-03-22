// tty.go - Interactive password prompt for Cisco producers.
// Opens /dev/tty directly to read passwords with echo disabled,
// so it works even when stdin is piped.
//
// For an introduction to OSIRIS JSON Producer for Cisco see:
// "[OSIRIS-JSON-CISCO]."
//
// [OSIRIS-JSON-CISCO]: https://osirisjson.org/en/docs/producers/cisco

package run

import (
	"fmt"
	"os"

	"golang.org/x/term"
)

// PromptPassword prompts the user for a password via /dev/tty with echo disabled.
// The prompt string is written to stderr so it appears even when stdout is redirected.
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
