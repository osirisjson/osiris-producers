// client.go - NX-API CLI HTTP client for the Cisco NX-OS producer.
// Implements authentication via HTTP Basic Auth with session cookie persistence,
// and command execution via JSON-RPC POST requests to /ins.
//
// For an introduction to OSIRIS JSON Producer for Cisco see:
// "[OSIRIS-JSON-CISCO]."
//
// [OSIRIS-JSON-CISCO]: https://osirisjson.org/en/docs/producers/cisco

package nxos

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"go.osirisjson.org/producers/osiris/network/cisco/run"
)

const (
	defaultNXOSPort  = 443
	maxMultiCommands = 10
)

// Client is a thin HTTP client for the NX-API CLI interface.
type Client struct {
	baseURL    string
	httpClient *http.Client
	username   string
	password   string
	logger     *slog.Logger
}

// NewClient creates an NX-API client targeting the given address.
func NewClient(target run.TargetConfig, insecure bool, logger *slog.Logger) *Client {
	addr := run.ResolveAddr(target, defaultNXOSPort)
	return &Client{
		baseURL:    "https://" + addr,
		httpClient: run.NewHTTPClient(insecure),
		logger:     logger,
	}
}

// Login stores credentials and validates them by sending "show version".
// The cookie jar captures the nxapi_auth session cookie for subsequent requests.
func (c *Client) Login(username, password string) error {
	c.username = username
	c.password = password

	// Validate credentials with a lightweight command.
	_, err := c.Show("show version")
	if err != nil {
		return fmt.Errorf("NX-API login failed: %w", err)
	}

	c.logger.Info("NX-API login successful", "url", c.baseURL)
	return nil
}

// Show executes a single NX-API CLI show command and returns the response body.
func (c *Client) Show(command string) (map[string]any, error) {
	results, err := c.ShowMulti([]string{command})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("NX-API show %q: empty response", command)
	}
	return results[0], nil
}

// ShowMulti executes multiple semicolon-separated NX-API CLI show commands.
// Returns one result body per command. Maximum 10 commands per call.
func (c *Client) ShowMulti(commands []string) ([]map[string]any, error) {
	if len(commands) == 0 {
		return nil, fmt.Errorf("NX-API: no commands provided")
	}
	if len(commands) > maxMultiCommands {
		return nil, fmt.Errorf("NX-API: too many commands (%d > %d)", len(commands), maxMultiCommands)
	}

	input := strings.Join(commands, " ;")

	payload := map[string]any{
		"ins_api": map[string]any{
			"version":       "0.1",
			"type":          "cli_show",
			"chunk":         "0",
			"sid":           "1",
			"input":         input,
			"output_format": "json",
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("NX-API: failed to marshal request: %w", err)
	}

	url := c.baseURL + "/ins"
	req, err := http.NewRequest("POST", url, strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("NX-API: failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(c.username, c.password)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("NX-API request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("NX-API: failed to read response: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("NX-API authentication failed (HTTP %d)", resp.StatusCode)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("NX-API request failed (HTTP %d): %s", resp.StatusCode, truncateBody(respBody))
	}

	return parseNXAPIResponse(respBody, len(commands))
}

// nxapiResponse represents the NX-API JSON envelope.
type nxapiResponse struct {
	InsAPI struct {
		Outputs struct {
			Output json.RawMessage `json:"output"`
		} `json:"outputs"`
	} `json:"ins_api"`
}

// nxapiOutput represents a single command output in the NX-API response.
type nxapiOutput struct {
	Code string          `json:"code"`
	Msg  string          `json:"msg"`
	Body json.RawMessage `json:"body"`
}

// parseNXAPIResponse extracts command result bodies from the NX-API JSON envelope.
// Handles NX-API polymorphism: single command returns output as object, multiple as array.
func parseNXAPIResponse(data []byte, expectedCount int) ([]map[string]any, error) {
	var resp nxapiResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("NX-API: failed to parse response: %w", err)
	}

	raw := resp.InsAPI.Outputs.Output

	// NX-API polymorphism: single command = object, multiple = array.
	var outputs []nxapiOutput
	if expectedCount == 1 {
		var single nxapiOutput
		if err := json.Unmarshal(raw, &single); err != nil {
			return nil, fmt.Errorf("NX-API: failed to parse single output: %w", err)
		}
		outputs = []nxapiOutput{single}
	} else {
		if err := json.Unmarshal(raw, &outputs); err != nil {
			return nil, fmt.Errorf("NX-API: failed to parse output array: %w", err)
		}
	}

	var results []map[string]any
	for i, out := range outputs {
		if out.Code != "200" {
			return nil, fmt.Errorf("NX-API command %d failed (code %s): %s", i, out.Code, out.Msg)
		}

		var body map[string]any
		if len(out.Body) > 0 && string(out.Body) != "\"\"" {
			if err := json.Unmarshal(out.Body, &body); err != nil {
				// Some commands return a string body (e.g., empty output).
				body = make(map[string]any)
			}
		}
		if body == nil {
			body = make(map[string]any)
		}
		results = append(results, body)
	}

	return results, nil
}

// truncateBody returns the first 200 bytes of a response body for error messages.
func truncateBody(body []byte) string {
	if len(body) > 200 {
		return string(body[:200]) + "..."
	}
	return string(body)
}
