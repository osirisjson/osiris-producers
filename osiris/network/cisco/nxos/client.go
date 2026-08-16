// client.go - NX-API CLI HTTP client for the Cisco NX-OS producer.
// Implements authentication via HTTP Basic Auth with session cookie
// persistence, and command execution via JSON-RPC POST requests to /ins.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package nxos

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.osirisjson.org/producers/osiris/network/cisco/run"
)

const (
	defaultNXOSPort  = 443
	maxMultiCommands = 10

	// requestTimeout bounds every NX-API call this client makes,
	// applied both as the http.Client's total request timeout and as a
	// per-attempt context deadline (see ShowMulti), so a stalled or
	// silently-dropped connection cannot hang a whole collection run.
	requestTimeout = 30 * time.Second

	// maxResponseBodyBytes bounds how much of an NX-API response this
	// client will read. "show interface"/"show tech" style commands can
	// return a very large body on a device with many interfaces;
	// without a bound, a misbehaving or malicious endpoint
	// could exhaust memory.
	maxResponseBodyBytes = 32 * 1024 * 1024 // 32 MiB

	// max429Retries and baseRetryDelay bound the retry-with-backoff
	// applied to transient HTTP 429/503 responses. Authentication
	// failures (401/403) are never retried see ShowMulti.
	max429Retries  = 3
	baseRetryDelay = 500 * time.Millisecond
	maxRetryDelay  = 4 * time.Second
)

// Client is a thin HTTP client for the NX-API CLI interface.
type Client struct {
	baseURL     string
	httpClient  *http.Client
	username    string
	password    string
	logger      *slog.Logger
	versionData *versionResponse // captured by Login; see VersionData
}

// NewClient creates an NX-API client targeting the given address.
// TLS certificate verification is on by default; passing insecure=true
// (the --insecure flag) skips it and is logged loudly here so a
// disabled verification is never silent.
func NewClient(target run.TargetConfig, insecure bool, logger *slog.Logger) *Client {
	addr := run.ResolveAddr(target, defaultNXOSPort)
	if insecure {
		logger.Warn("TLS certificate verification disabled for NX-API connection (--insecure)", "host", addr)
	}
	httpClient := run.NewHTTPClient(insecure)
	httpClient.Timeout = requestTimeout
	return &Client{
		baseURL:    "https://" + addr,
		httpClient: httpClient,
		logger:     logger,
	}
}

// Login stores credentials and validates them by sending "show version".
// The cookie jar captures the nxapi_auth session cookie
// for subsequent requests.
//
// The "show version" body is decoded and cached on the Client, exposed
// via VersionData, so a caller that reaches this device through Login
// (as opposed to injecting an already-authenticated Client, e.g. in
// tests) does not need a second "show version" call later just to build
// the device resource.
func (c *Client) Login(username, password string) error {
	c.username = username
	c.password = password

	// Validate credentials with a lightweight command.
	body, err := c.Show("show version")
	if err != nil {
		return fmt.Errorf("NX-API login failed: %w", err)
	}
	var vd versionResponse
	if err := json.Unmarshal(body, &vd); err != nil {
		return fmt.Errorf("NX-API login: failed to decode show version: %w", err)
	}
	c.versionData = &vd

	c.logger.Info("NX-API login successful", "url", c.baseURL)
	return nil
}

// VersionData returns the "show version" body captured during Login, or
// nil if Login has never been called on this Client which is the case
// for a pre-authenticated Client injected directly into Producer,
// bypassing Login entirely (see Producer.Collect and this package's own
// tests).
func (c *Client) VersionData() *versionResponse {
	return c.versionData
}

// Show executes a single NX-API CLI show command and returns the raw
// response body. Unlike ShowMulti, a command-level failure here is
// returned as a hard error (there is only one command, so there is no
// sibling result to isolate it from) used by Login, where the caller
// genuinely needs this one command to succeed. The body is returned raw
// (undecoded); the caller decodes it into whatever typed shape it
// expects.
func (c *Client) Show(command string) (json.RawMessage, error) {
	results, err := c.ShowMulti([]string{command})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("NX-API show %q: empty response", command)
	}
	if results[0].Err != nil {
		return nil, results[0].Err
	}
	return results[0].Body, nil
}

// ShowResult is one command's outcome within a ShowMulti batch. Body is
// the command's raw, undecoded JSON response body, valid only when Err
// is nil decoding it into a typed shape is left to the caller, this
// layer only confirms it is a well-formed JSON object
// (see parseNXAPIResponse). A command-level Err (e.g. the CLI
// rejected the command, or a feature is not configured) never affects
// any other ShowResult in the same batch see ShowMulti and
// parseNXAPIResponse.
type ShowResult struct {
	Command string
	Body    json.RawMessage
	Err     error
}

// ShowMulti executes multiple semicolon-separated NX-API CLI show
// commands and returns one ShowResult per command, in order. Maximum
// 10 commands per call.
//
// The returned error is reserved for transport-level failures (cannot
// reach the device, malformed envelope, wrong result count) that make
// it impossible to produce any per-command result at all. A single
// command being rejected by the CLI (e.g. "show lldp neighbors detail"
// on a device with LLDP disabled) is never such a failure it becomes
// that one ShowResult's Err, leaving every other command's result
// (successful or not) unaffected. Callers must check each ShowResult's
// Err individually rather than treating any command's failure as
// invalidating the whole batch.
//
// Each attempt runs under its own requestTimeout-bounded context (in
// addition to the http.Client-level Timeout set in NewClient, which
// only bounds a single attempt, not the retry loop as a whole).
// Transient HTTP 429 (Too Many Requests) and 503 (Service Unavailable)
// responses are retried with backoff, honoring a Retry-After header
// when the device sends one; 401/403 (authentication failures) are
// classified as permanent and never retried.
func (c *Client) ShowMulti(commands []string) ([]ShowResult, error) {
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

	var lastErr error
	for attempt := 0; attempt <= max429Retries; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
		if err != nil {
			cancel()
			return nil, fmt.Errorf("NX-API: failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.SetBasicAuth(c.username, c.password)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("NX-API request failed: %w", err)
		}

		respBody, readErr := readLimitedBody(resp.Body, maxResponseBodyBytes)
		resp.Body.Close()
		cancel()
		if readErr != nil {
			return nil, fmt.Errorf("NX-API: failed to read response: %w", readErr)
		}

		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return nil, fmt.Errorf("NX-API authentication failed (HTTP %d)", resp.StatusCode)
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
			lastErr = fmt.Errorf("NX-API request failed (HTTP %d): %s", resp.StatusCode, truncateBody(respBody))
			if attempt < max429Retries {
				time.Sleep(retryDelay(resp, attempt))
				continue
			}
			return nil, lastErr
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("NX-API request failed (HTTP %d): %s", resp.StatusCode, truncateBody(respBody))
		}

		return parseNXAPIResponse(respBody, commands)
	}

	return nil, lastErr
}

// readLimitedBody reads at most limit+1 bytes from r, returning an
// error if the body turns out to exceed limit this bounds memory use
// against an oversized or runaway response without silently truncating
// (and thus corrupting) a body that happens to fit exactly at the
// boundary.
func readLimitedBody(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response body exceeds %d byte limit", limit)
	}
	return data, nil
}

// retryDelay honors a Retry-After response header (seconds form) when
// present, otherwise backs off exponentially from baseRetryDelay,
// capped at maxRetryDelay.
func retryDelay(resp *http.Response, attempt int) time.Duration {
	if v := resp.Header.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
			return time.Duration(secs) * time.Second
		}
	}
	delay := baseRetryDelay * time.Duration(1<<attempt)
	if delay > maxRetryDelay {
		delay = maxRetryDelay
	}
	return delay
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

// parseNXAPIResponse extracts one ShowResult per command from the
// NX-API JSON envelope. Handles NX-API polymorphism: single command
// returns output as an object, multiple as an array.
//
// Only failures that prevent producing any per-command result at all
// (an unparseable envelope, or a result count that does not match the
// number of commands sent) are returned as the function-level error.
// A per-command CLI rejection (code != "200") or an undecodable body
// becomes that command's ShowResult.Err instead - see ShowMulti's doc
// comment for why this distinction matters (one
// command's failure must never erase or be indistinguishable from
// another command's success).
func parseNXAPIResponse(data []byte, commands []string) ([]ShowResult, error) {
	var resp nxapiResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("NX-API: failed to parse response: %w", err)
	}

	raw := resp.InsAPI.Outputs.Output

	// NX-API polymorphism: single command = object, multiple = array.
	var outputs []nxapiOutput
	if len(commands) == 1 {
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

	if len(outputs) != len(commands) {
		return nil, fmt.Errorf("NX-API: expected %d command result(s), got %d", len(commands), len(outputs))
	}

	results := make([]ShowResult, len(outputs))
	for i, out := range outputs {
		results[i].Command = commands[i]

		if out.Code != "200" {
			results[i].Err = fmt.Errorf("NX-API command %q failed (code %s): %s", commands[i], out.Code, out.Msg)
			continue
		}

		// An empty or bare-string body (`""`) means "no output" and is
		// not an error most commonly seen on commands with nothing to
		// report. Any other non-empty body that fails to decode as an
		// object is a genuine decode failure and must be visible as
		// this command's Err, not silently swallowed into an empty body
		// indistinguishable from a real empty response. This check is
		// only ever "is this a well-formed JSON object" it never
		// commits to a specific typed shape, which stays the caller's
		// decision. On success, the raw bytes are kept as
		// Body rather than the scratch map used only for validation.
		if len(out.Body) > 0 && string(out.Body) != `""` {
			var probe map[string]any
			if err := json.Unmarshal(out.Body, &probe); err != nil {
				results[i].Err = fmt.Errorf("NX-API command %q: failed to decode body: %w", commands[i], err)
				continue
			}
			results[i].Body = out.Body
		} else {
			results[i].Body = json.RawMessage(`{}`)
		}
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
