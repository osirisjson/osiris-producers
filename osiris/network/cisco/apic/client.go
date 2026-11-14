// client.go - APIC REST API client for the Cisco ACI producer.
// Implements authentication via aaaLogin, class-based queries with
// pagination and best-effort aaaLogout session cleanup. Every request
// runs under a caller-supplied context with a per-request timeout, a
// bounded response body and classified retry of transient failures
// (network errors, HTTP 429, HTTP 5xx). The underlying http.Client
// cookie jar carries the APIC session token automatically.
//
// Errors returned by this client carry stable diagnostic categories
// only: the operation, APIC class, HTTP status and numeric APIC error
// code. Response bodies and APIC error text are never included, because
// they are untrusted controller data that can reach stderr, batch logs
// and CI output.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package apic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
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
	hc := run.NewHTTPClient(insecure)
	hc.CheckRedirect = refuseRedirect
	return &Client{
		baseURL:    "https://" + addr,
		httpClient: hc,
		logger:     logger,
		ctx:        ctx,
	}
}

// refuseRedirect is the client's redirect policy: no redirect is ever
// followed, for login or collection. A 307/308 would replay the
// aaaLogin body (the password) to whatever the Location names and no
// APIC REST endpoint this producer uses legitimately redirects.
// The 3xx response is returned as-is and fails the request as
// "HTTP 3xx"; its Location is never echoed into a diagnostic.
func refuseRedirect(*http.Request, []*http.Request) error {
	return http.ErrUseLastResponse
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
	env, err := decodeEnvelope(body)
	if err != nil {
		return fmt.Errorf("APIC login: decode response: %w", err)
	}
	if apiErr := env.asError(); apiErr != nil {
		return fmt.Errorf("APIC login failed: %w", apiErr)
	}
	if len(env.ImData) > 0 {
		if attrs, ok := extractOneAttributes(env.ImData[0], "aaaLogin"); ok {
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

// classQuery is the outcome of one paginated class query.
type classQuery struct {
	// objs holds the attribute map of every accepted object.
	objs []map[string]any
	// received counts imdata elements across all pages.
	received int
	// rejected counts elements that were not an object of the requested
	// class with an attributes map and a non-empty string dn.
	rejected int
	// reportedTotal is the totalCount the APIC reported on the first
	// page, or -1 when it was absent or not a number.
	reportedTotal int
}

// QueryClass fetches all objects of a given APIC class, handling
// pagination. Returns a slice of attribute maps (one per accepted
// object). See queryClass for the validation applied.
func (c *Client) QueryClass(class string) ([]map[string]any, error) {
	q, err := c.queryClass(class)
	if err != nil {
		return nil, err
	}
	return q.objs, nil
}

// queryClass fetches all objects of a given APIC class, handling
// pagination and reports how many elements were received and rejected.
//
// Every page must be a JSON object carrying an imdata array; an absent
// or null imdata, or any APIC error object among the elements (the APIC
// can return one with HTTP 200), fails the query. A transport failure
// also fails it, so a caller can distinguish a failed query from a
// legitimately empty one (imdata: []). An element is accepted only when
// it is an object of exactly the requested class with an attributes map
// and a non-empty string dn; any other element is rejected and counted,
// never turned into an unidentified resource. Pagination advances on
// the count of elements the page carried, not on how many were
// accepted, so a malformed element cannot look like a short final page
// and truncate the pages after it.
//
// The query is ordered by "dn" (unique on every managed object). APIC
// does not guarantee a stable row order across separate page requests,
// so without an explicit sort an object can repeat on the next page (or
// be skipped) when the managed-object tree changes mid-query, which for
// a churny class such as fvCEp is routine on a live fabric. A stable
// order makes the pages disjoint and complete; dedupeByDN in the caller
// is the backstop. Pages are still read over a time window, not as an
// atomic snapshot.
func (c *Client) queryClass(class string) (classQuery, error) {
	q := classQuery{reportedTotal: -1}
	page := 0

	for {
		url := fmt.Sprintf("%s/api/class/%s.json?page=%d&page-size=%d&order-by=%s.dn",
			c.baseURL, class, page, pageSize, class)

		body, _, err := c.doRequest(http.MethodGet, url, nil)
		if err != nil {
			return classQuery{}, fmt.Errorf("APIC query %s: %w", class, err)
		}

		env, err := decodeEnvelope(body)
		if err != nil {
			return classQuery{}, fmt.Errorf("APIC query %s: decode envelope: %w", class, err)
		}
		if apiErr := env.asError(); apiErr != nil {
			return classQuery{}, fmt.Errorf("APIC query %s: %w", class, apiErr)
		}
		if page == 0 {
			q.reportedTotal = env.totalCount()
		}

		received := len(env.ImData)
		q.received += received
		var accepted int
		for _, item := range env.ImData {
			attrs, ok := extractOneAttributes(item, class)
			if !ok || str(attrs, "dn") == "" {
				q.rejected++
				continue
			}
			q.objs = append(q.objs, attrs)
			accepted++
		}

		c.logger.Debug("APIC query", "class", class, "page", page, "received", received, "accepted", accepted)

		if received < pageSize {
			break
		}
		page++
		// Heartbeat for a large paginated class (faultInst on a big
		// fabric is many pages at several seconds each): without this
		// the run looks frozen between the "querying" line and the
		// eventual "query complete".
		c.logger.Info("APIC query in progress", "class", class, "page", page, "objects_so_far", len(q.objs))
	}

	if q.rejected > 0 {
		c.logger.Warn("APIC objects rejected: not an identified object of the requested class",
			"class", class, "rejected", q.rejected)
	}
	if q.reportedTotal >= 0 && q.reportedTotal != q.received {
		c.logger.Warn("APIC totalCount differs from objects received (the tree may have changed during the query)",
			"class", class, "total_count", q.reportedTotal, "received", q.received)
	}
	c.logger.Info("APIC query complete", "class", class, "total", len(q.objs))
	return q, nil
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
		return data, resp.StatusCode, true, fmt.Errorf("HTTP %d", resp.StatusCode)
	default:
		return data, resp.StatusCode, false, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
}

// imdataEnvelope is the common APIC JSON response envelope.
// Elements are kept as raw messages so one malformed element cannot
// fail the decode of the whole page
// (see queryClass and extractOneAttributes).
type imdataEnvelope struct {
	TotalCount json.RawMessage   `json:"totalCount"`
	ImData     []json.RawMessage `json:"imdata"`
}

// decodeEnvelope decodes an APIC response body. It fails with
// errInvalidJSON when the body is not a JSON object and with
// errInvalidEnvelope when imdata is absent, null or not an array, so
// "{}" or {"imdata":null} can never pass as an empty result.
func decodeEnvelope(body []byte) (imdataEnvelope, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil || top == nil {
		return imdataEnvelope{}, errInvalidJSON
	}
	rawIm, ok := top["imdata"]
	if !ok || bytes.Equal(bytes.TrimSpace(rawIm), []byte("null")) {
		return imdataEnvelope{}, errInvalidEnvelope
	}
	var env imdataEnvelope
	if err := json.Unmarshal(rawIm, &env.ImData); err != nil {
		return imdataEnvelope{}, errInvalidEnvelope
	}
	env.TotalCount = top["totalCount"]
	return env, nil
}

// totalCount returns the envelope's totalCount (the APIC sends it as a
// decimal string) or -1 when it is absent or not a non-negative number.
func (e imdataEnvelope) totalCount() int {
	var s string
	if err := json.Unmarshal(e.TotalCount, &s); err != nil {
		var n json.Number
		if err := json.Unmarshal(e.TotalCount, &n); err != nil {
			return -1
		}
		s = n.String()
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return -1
	}
	return n
}

// asError reports an APIC error object
// ({"error":{"attributes":{"code","text"}}}) anywhere among the imdata
// elements, which the APIC can return with HTTP 200. A page mixing data
// with an error object is treated as failed. Returns nil for a normal
// response. Only a numeric code is retained; the error text is
// discarded (see the file comment).
func (e imdataEnvelope) asError() *apicAPIError {
	for _, item := range e.ImData {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(item, &obj); err != nil {
			continue
		}
		errBody, ok := obj["error"]
		if !ok {
			continue
		}
		var wrap struct {
			Attributes struct {
				Code json.RawMessage `json:"code"`
			} `json:"attributes"`
		}
		_ = json.Unmarshal(errBody, &wrap)
		var code string
		_ = json.Unmarshal(wrap.Attributes.Code, &code)
		return &apicAPIError{code: numericCode(code)}
	}
	return nil
}

// apicAPIError is an error carried in an APIC imdata error envelope.
// code is the APIC error code when it is a short decimal number, or
// empty otherwise.
type apicAPIError struct {
	code string
}

func (e *apicAPIError) Error() string {
	if e.code == "" {
		return "APIC error envelope"
	}
	return "APIC error " + e.code
}

// errInvalidJSON replaces a JSON decoder error in a returned error,
// whose message can quote bytes of the untrusted response.
var errInvalidJSON = errors.New("response is not valid JSON")

// errInvalidEnvelope reports a JSON response without an imdata array.
var errInvalidEnvelope = errors.New("response has no imdata array")

// numericCode returns code when it is 1 to 6 ASCII digits, the shape of
// an APIC error code and "" otherwise, so an unexpected value is never
// echoed into a diagnostic.
func numericCode(code string) string {
	if len(code) == 0 || len(code) > 6 {
		return ""
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return code
}

// extractOneAttributes pulls the attributes map from a single imdata
// element ({"<class>":{"attributes":{...}}}). Returns ok=false unless
// the element is a JSON object whose only key is class and whose body
// carries an attributes object, so an element of an unexpected class is
// rejected rather than read as the requested one.
func extractOneAttributes(raw json.RawMessage, class string) (map[string]any, bool) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || len(obj) != 1 {
		return nil, false
	}
	classBody, ok := obj[class]
	if !ok {
		return nil, false
	}
	var inner struct {
		Attributes map[string]any `json:"attributes"`
	}
	if err := json.Unmarshal(classBody, &inner); err != nil || inner.Attributes == nil {
		return nil, false
	}
	return inner.Attributes, true
}
