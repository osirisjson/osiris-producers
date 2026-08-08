// tty.go - Interactive host/username prompt for the Cisco vManage
// OSIRIS JSON producer.
//
// Password prompting reuses osiris/network/cisco/run.PromptPassword
// (hidden echo, shared with apic/nxos/iosxe). Host/username are visible
// prompts specific to vmanage's fuller interactive login flow (see
// flags.go's ParseFlags fallback chain: flag, then --token-file, then
// this prompt) - apic/nxos/iosxe never need to ask for host/username
// interactively, so this stays local to vmanage rather than living in
// the shared run package.
//
// OSIRIS JSON Producer for Cisco SD-WAN Manager (vManage) introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package vmanage

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// promptVisible prompts for a non-secret value (host, username) via
// /dev/tty with normal echo, returning the trimmed input. Opens the
// controlling terminal directly (rather than reading stdin) so it
// still works when stdin is piped, matching run.PromptPassword
// approach for the hidden password prompt.
func promptVisible(prompt string) (string, error) {
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
