// client.go - APIC REST API client for the Cisco ACI producer.
// Implements authentication via aaaLogin, class-based queries with
// pagination, and best-effort aaaLogout session cleanup. Every request
// runs under a caller-supplied context with a per-request timeout, a
// bounded response body, and classified retry of transient failures
// (network errors, HTTP 429, HTTP 5xx). The underlying http.Client
// cookie jar carries the APIC session token automatically.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package apic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"go.osirisjson.org/producers/osiris/network/cisco/run"
)

const (
	defaultAPICPort = 443
	pageSize        = 100

	// defaultRequestTimeout bounds a single HTTP request/response
	// exchange (connect, send, headers, body read). A stalled APIC
	// therefore fails an attempt within this budget rather than
	// hanging the whole collection.
	defaultRequestTimeout = 60 * time.Second

	// defaultMaxBodyBytes caps a single response body. APIC
	// fabric-wide class dumps (fvCEp, the MAC endpoint table) are the
	// largest legitimate payloads and run into the tens of MiB on a
	// big fabric; 256 MiB leaves generous headroom while still
	// rejecting a runaway or hostile response.
	defaultMaxBodyBytes = 256 << 20

	// defaultMaxRetries is the number of extra attempts after the
	// first for a transient failure.
	defaultMaxRetries = 3

	// defaultRetryBaseDelay is the first backoff pause; it doubles
	// each subsequent retry.
	defaultRetryBaseDelay = 500 * time.Millisecond
)

// Client is a bounded HTTP client for the APIC REST API.
type Client struct {
	baseURL    string
	httpClient *http.Client
	token      string
	username   string // captured at Login so Logout can name the session.
	logger     *slog.Logger

	// ctx is the root context for every request. A nil ctx is treated
	// as context.Background(). Cancelling it aborts an in-flight run.
	ctx context.Context

	// Transport knobs. A zero value selects the package default; tests
	// override them for fast, deterministic behaviour.
	reqTimeout   time.Duration
	maxBodyBytes int64
	retryBase    time.Duration
	maxRetries   int
}

// NewClient creates an APIC client targeting the given address. ctx is
// the root context for every request the client issues; pass
// context.Background() when there is nothing to cancel on.
func NewClient(ctx context.Context, target run.TargetConfig, insecure bool, logger *slog.Logger) *Client {
	addr := run.ResolveAddr(target, defaultAPICPort)
	return &Client{
		baseURL:    "https://" + addr,
		httpClient: run.NewHTTPClient(insecure),
		logger:     logger,
		ctx:        ctx,
	}
}

func (c *Client) rootCtx() context.Context {
	if c.ctx != nil {
		return c.ctx
	}
	return context.Background()
}

func (c *Client) requestTimeout() time.Duration {
	if c.reqTimeout > 0 {
		return c.reqTimeout
	}
	return defaultRequestTimeout
}

func (c *Client) maxBody() int64 {
	if c.maxBodyBytes > 0 {
		return c.maxBodyBytes
	}
	return defaultMaxBodyBytes
}

func (c *Client) retryBackoff() time.Duration {
	if c.retryBase > 0 {
		return c.retryBase
	}
	return defaultRetryBaseDelay
}

func (c *Client) retryAttempts() int {
	if c.maxRetries != 0 {
		return c.maxRetries
	}
	return defaultMaxRetries
}

// aaaLoginRequest is the aaaLogin/aaaLogout request envelope. Building
// it with encoding/json (rather than string interpolation) keeps
// credentials containing quotes or backslashes valid JSON.
type aaaLoginRequest struct {
	AaaUser aaaUserObject `json:"aaaUser"`
}

type aaaUserObject struct {
	Attributes aaaUserAttributes `json:"attributes"`
}

type aaaUserAttributes struct {
	Name string `json:"name"`
	Pwd  string `json:"pwd,omitempty"`
}

// Login authenticates against the APIC and stores the session token.
func (c *Client) Login(username, password string) error {
	reqBody, err := json.Marshal(aaaLoginRequest{
		AaaUser: aaaUserObject{Attributes: aaaUserAttributes{Name: username, Pwd: password}},
	})
	if err != nil {
		return fmt.Errorf("APIC login: encoding request: %w", err)
	}

	body, _, err := c.doRequest(http.MethodPost, c.baseURL+"/api/aaaLogin.json", reqBody)
	if err != nil {
		return fmt.Errorf("APIC login failed: %w", err)
	}

	// Extract token from response for logging; the cookie jar handles
	// the session automatically for subsequent requests.
	var env imdataEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("APIC login: failed to parse response: %w", err)
	}
	if apiErr := env.asError(); apiErr != nil {
		return fmt.Errorf("APIC login failed: %w", apiErr)
	}
	if len(env.ImData) > 0 {
		if attrs, ok := extractOneAttributes(env.ImData[0]); ok {
			if tok, _ := attrs["token"].(string); tok != "" {
				c.token = tok
			}
		}
	}

	if c.token == "" {
		return fmt.Errorf("APIC login: no token in response")
	}

	c.username = username
	c.logger.Info("APIC login successful", "url", c.baseURL)
	return nil
}

// Logout best-effort invalidates the APIC session. It is safe to call
// unconditionally: with no stored token or username it is a no-op. A
// returned error should be logged by the caller, never allowed to
// replace the primary collection result.
func (c *Client) Logout() error {
	if c.token == "" || c.username == "" {
		return nil
	}

	reqBody, err := json.Marshal(aaaLoginRequest{
		AaaUser: aaaUserObject{Attributes: aaaUserAttributes{Name: c.username}},
	})
	if err != nil {
		return fmt.Errorf("APIC logout: encoding request: %w", err)
	}

	if _, _, err := c.doRequest(http.MethodPost, c.baseURL+"/api/aaaLogout.json", reqBody); err != nil {
		return fmt.Errorf("APIC logout: %w", err)
	}

	c.token = ""
	c.logger.Info("APIC logout complete", "url", c.baseURL)
	return nil
}

// QueryClass fetches all objects of a given APIC class, handling
// pagination. Returns a slice of attribute maps (one per object).
//
// The response is decoded through a typed imdata envelope. A transport
// failure, a body that is not a valid imdata envelope, or an APIC error
// envelope (which can arrive with HTTP 200) all return an error a
// caller can therefore distinguish a failed query from a legitimately
// empty one. Pagination advances on the count of source objects the
// page carried, not on how many survived attribute extraction, so a
// single malformed object cannot look like a short final page and
// truncate the pages after it.
func (c *Client) QueryClass(class string) ([]map[string]any, error) {
	var all []map[string]any
	page := 0

	for {
		url := fmt.Sprintf("%s/api/class/%s.json?page=%d&page-size=%d", c.baseURL, class, page, pageSize)

		body, _, err := c.doRequest(http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("APIC query %s: %w", class, err)
		}

		var env imdataEnvelope
		if err := json.Unmarshal(body, &env); err != nil {
			return nil, fmt.Errorf("APIC query %s: decode envelope: %w", class, err)
		}
		if apiErr := env.asError(); apiErr != nil {
			return nil, fmt.Errorf("APIC query %s: %w", class, apiErr)
		}

		received := len(env.ImData)
		var extracted int
		for _, item := range env.ImData {
			attrs, ok := extractOneAttributes(item)
			if !ok {
				c.logger.Warn("APIC object skipped: unexpected shape", "class", class, "page", page)
				continue
			}
			all = append(all, attrs)
			extracted++
		}

		c.logger.Debug("APIC query", "class", class, "page", page, "received", received, "extracted", extracted)

		if received < pageSize {
			break
		}
		page++
		// Heartbeat for a large paginated class (faultInst on a big
		// fabric is many pages at several seconds each): without this
		// the run looks frozen between the "querying" line and the
		// eventual "query complete".
		c.logger.Info("APIC query in progress", "class", class, "page", page, "objects_so_far", len(all))
	}

	c.logger.Info("APIC query complete", "class", class, "total", len(all))
	return all, nil
}

// doRequest performs one logical HTTP request. Each attempt run under a
// child context bounded by requestTimeout, reads at most maxBody bytes,
// and is retried on a transient failure (network error, HTTP 429, HTTP
// 5xx) up to retryAttempts times with doubling backoff. Client errors
// (400, 401, 403, 404, ...) are returned immediately, never retried.
// Root-context cancellation aborts without further attempts.
func (c *Client) doRequest(method, url string, reqBody []byte) ([]byte, int, error) {
	attempts := c.retryAttempts()
	var lastErr error

	for attempt := 0; attempt <= attempts; attempt++ {
		if attempt > 0 {
			delay := c.retryBackoff() << (attempt - 1)
			select {
			case <-time.After(delay):
			case <-c.rootCtx().Done():
				return nil, 0, fmt.Errorf("APIC request cancelled: %w", c.rootCtx().Err())
			}
			c.logger.Warn("APIC request retry", "url", url, "attempt", attempt+1, "of", attempts+1)
		}

		body, status, retryable, err := c.attempt(method, url, reqBody)
		if err == nil {
			return body, status, nil
		}
		lastErr = err
		if !retryable {
			return body, status, err
		}
	}

	return nil, 0, fmt.Errorf("gave up after %d attempts: %w", attempts+1, lastErr)
}

// attempt issues a single request. The bool reports whether err (when
// non-nil) is worth retrying.
func (c *Client) attempt(method, url string, reqBody []byte) (body []byte, status int, retryable bool, err error) {
	ctx, cancel := context.WithTimeout(c.rootCtx(), c.requestTimeout())
	defer cancel()

	var rdr io.Reader
	if reqBody != nil {
		rdr = bytes.NewReader(reqBody)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		return nil, 0, false, err // malformed request: not retryable.
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctxErr := c.rootCtx().Err(); ctxErr != nil {
			return nil, 0, false, fmt.Errorf("request cancelled: %w", ctxErr)
		}
		// Includes this attempt's own timeout: retry a stalled call.
		return nil, 0, true, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	data, readErr := io.ReadAll(io.LimitReader(resp.Body, c.maxBody()+1))
	if int64(len(data)) > c.maxBody() {
		return nil, resp.StatusCode, false, fmt.Errorf("response body exceeded %d-byte limit", c.maxBody())
	}
	if readErr != nil {
		if ctxErr := c.rootCtx().Err(); ctxErr != nil {
			return nil, resp.StatusCode, false, fmt.Errorf("request cancelled: %w", ctxErr)
		}
		return nil, resp.StatusCode, true, fmt.Errorf("reading response: %w", readErr)
	}

	switch {
	case resp.StatusCode == http.StatusOK:
		return data, resp.StatusCode, false, nil
	case resp.StatusCode == http.StatusTooManyRequests, resp.StatusCode >= 500:
		return data, resp.StatusCode, true, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncateBody(data))
	default:
		return data, resp.StatusCode, false, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncateBody(data))
	}
}

// imdataEnvelope is the common APIC JSON response envelope.
// Elements are kept as raw messages so one malformed element cannot
// fail the decode of the whole page
// (see QueryClass and extractOneAttributes).
type imdataEnvelope struct {
	TotalCount string            `json:"totalCount"`
	ImData     []json.RawMessage `json:"imdata"`
}

// asError reports an APIC error envelope
// ({"imdata":[{"error":{"attributes":{"code","text"}}}]}), which the
// APIC can return with HTTP 200. Returns nil for any normal response.
func (e imdataEnvelope) asError() *apicAPIError {
	if len(e.ImData) != 1 {
		return nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(e.ImData[0], &obj); err != nil {
		return nil
	}
	errBody, ok := obj["error"]
	if !ok {
		return nil
	}
	var wrap struct {
		Attributes struct {
			Code string `json:"code"`
			Text string `json:"text"`
		} `json:"attributes"`
	}
	_ = json.Unmarshal(errBody, &wrap)
	return &apicAPIError{code: wrap.Attributes.Code, text: truncateText(wrap.Attributes.Text)}
}

// apicAPIError is an error carried in an APIC imdata error envelope.
type apicAPIError struct {
	code string
	text string
}

func (e *apicAPIError) Error() string {
	if e.code == "" {
		return "APIC error envelope: " + e.text
	}
	return fmt.Sprintf("APIC error %s: %s", e.code, e.text)
}

// extractOneAttributes pulls the attributes map from a single imdata
// element ({"className":{"attributes":{...}}}). Returns ok=false when
// the element is not a JSON object or carries no attributes map, so the
// caller can skip it and keep going.
func extractOneAttributes(raw json.RawMessage) (map[string]any, bool) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, false
	}
	for _, classBody := range obj {
		var inner struct {
			Attributes map[string]any `json:"attributes"`
		}
		if err := json.Unmarshal(classBody, &inner); err != nil {
			continue
		}
		if inner.Attributes != nil {
			return inner.Attributes, true
		}
	}
	return nil, false
}

// truncateBody returns the first 200 bytes of a response body for error
// messages. APIC error envelopes carry a code and text, no credentials.
func truncateBody(body []byte) string {
	if len(body) > 200 {
		return string(body[:200]) + "..."
	}
	return string(body)
}

// truncateText bounds an APIC-supplied error string before it goes into
// a Go error.
func truncateText(s string) string {
	const max = 200
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}
