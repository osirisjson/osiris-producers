// client.go - SD-WAN Manager REST API client for the Cisco SD-WAN
// Manager OSIRIS JSON Producer.
//
// Authentication is the standard SD-WAN Manager session-cookie + XSRF
// flow: POST /j_security_check (form-encoded username/password)
// establishes a JSESSIONID session cookie (handled automatically by the
// cookie jar from run.NewHTTPClient), then GET
// /dataservice/client/token mints an XSRF token that must be sent as
// the X-XSRF-TOKEN header on every subsequent /dataservice call.
// /j_security_check is a servlet outside /dataservice and is not part
// of the SD-WAN Manager OpenAPI spec, so its failure heuristic (an HTML
// login page being re-served) follows the commonly documented SD-WAN
// Manager automation pattern rather than a spec definition.
//
// OSIRIS JSON Producer for Cisco Catalyst SD-WAN Manager introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package catalystsdwanmanager

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.osirisjson.org/producers/osiris/network/cisco/run"
)

// requestTimeout bounds every HTTP call this client makes, so a slow or
// silently dropped request cannot hang the run. run.NewHTTPClient sets
// no timeout (shared by apic/nxos/iosxe too), so it is applied here,
// scoped to SD-WAN Manager only. 60 seconds leaves room for the
// per-site monitoring endpoints of large sites, which can take well
// over 15 seconds to answer.
const requestTimeout = 60 * time.Second

// maxTransientRetries is how many times a read request is retried after
// a timeout or an HTTP 5xx reply, with backoff from baseRetryDelay.
// Permission denials and malformed replies are never retried.
const maxTransientRetries = 1

// maxReauthentications bounds how many times one run logs in again
// after the Manager session expired (see reauthenticate).
const maxReauthentications = 1

// errSessionExpired marks a request rejected because the session ended
// and could not be renewed.
var errSessionExpired = errors.New("session expired")

// maxResponseBytes bounds every response body read into memory (64
// MiB). A larger reply is reported as too_large instead of being
// buffered without limit. A variable only so tests can lower it.
var maxResponseBytes = 64 << 20

// errResponseTooLarge reports a body over maxResponseBytes.
var errResponseTooLarge = errors.New("response exceeds the size limit")

// readBounded reads at most maxResponseBytes from r.
func readBounded(r io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, int64(maxResponseBytes)+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxResponseBytes {
		return nil, errResponseTooLarge
	}
	return body, nil
}

// Client is a thin HTTP client for the SD-WAN Manager dataservice REST
// API.
type Client struct {
	baseURL    string
	httpClient *http.Client
	logger     *slog.Logger

	// authMu guards the session state below, shared by concurrent
	// site-name lookups. authGen counts logins, so concurrent requests
	// that all saw an expired session renew it only once.
	authMu    sync.Mutex
	xsrfToken string
	authGen   int
	reauths   int
	username  string
	password  string

	// ctx cancels every request and retry wait of this client (for
	// example on Ctrl-C); nil means context.Background().
	ctx context.Context

	// siteNameMu/siteNameLastRequest/siteNameMinInterval throttle
	// GetSiteName specifically (bulk site-name resolution in
	// sites_select.go issues one call per discovered site across a
	// worker pool concurrency alone does not bound requests/second).
	// Every other endpoint on this client is unthrottled.
	siteNameMu          sync.Mutex
	siteNameLastRequest time.Time
	siteNameMinInterval time.Duration
}

// Device is the subset of the GET /dataservice/device response fields
// (tag "Monitoring - Device Details") this producer maps to OSIRIS
// resources. Field names and shapes are taken verbatim from the
// SD-WAN Manager OpenAPI spec's documented example for this endpoint.
//
// Latitude/Longitude are typed any because the spec's own example
// mixes a string latitude ("37.666684") with a numeric longitude
// (-122.777023) for the same object.
//
// LastUpdated/UptimeDate are epoch milliseconds; the published schema
// types them as strings while the published example uses integers, so
// both are accepted (see flexInt64).
//
// HealthState ("state") is the dashboard health rollup
// (green/red/yellow), distinct from Reachability and Status.
type Device struct {
	DeviceID            string    `json:"deviceId"`
	SystemIP            string    `json:"system-ip"`
	HostName            string    `json:"host-name"`
	UUID                string    `json:"uuid"`
	SiteID              string    `json:"site-id"`
	Personality         string    `json:"personality"`
	DeviceType          string    `json:"device-type"`
	DeviceModel         string    `json:"device-model"`
	Reachability        string    `json:"reachability"`
	Status              string    `json:"status"`
	Validity            string    `json:"validity"`
	Version             string    `json:"version"`
	Platform            string    `json:"platform"`
	BoardSerial         string    `json:"board-serial"`
	CertificateValidity string    `json:"certificate-validity"`
	DeviceGroups        []string  `json:"device-groups"`
	Latitude            any       `json:"latitude"`
	Longitude           any       `json:"longitude"`
	LastUpdated         flexInt64 `json:"lastupdated"`
	HealthState         string    `json:"state"`
	StateDescription    string    `json:"state_description"`
	UptimeDate          flexInt64 `json:"uptime-date"`
	ConnectedManagers   []string  `json:"connectedVManages"` // API field name keeps still the Cisco vManage pre-rebrand spelling.
}

// NewClient creates an SD-WAN Manager client targeting the given
// controller.
func NewClient(cfg *Config, logger *slog.Logger) *Client {
	addr := cfg.Host
	if strings.Contains(addr, ":") {
		addr = "[" + addr + "]"
	}
	baseURL := fmt.Sprintf("https://%s:%d", addr, cfg.Port)
	httpClient := run.NewHTTPClient(cfg.InsecureTLS)
	httpClient.Timeout = requestTimeout
	if origin, err := url.Parse(baseURL); err == nil {
		httpClient.CheckRedirect = sameOriginRedirect(origin)
	}
	return &Client{
		baseURL:             baseURL,
		httpClient:          httpClient,
		logger:              logger,
		siteNameMinInterval: rateToInterval(cfg.SiteNameRateLimit),
	}
}

// maxRedirects bounds the same-origin redirects one request follows.
const maxRedirects = 5

// errCrossOriginRedirect is returned for a redirect that leaves the
// controller origin.
var errCrossOriginRedirect = errors.New("refusing redirect outside the controller origin")

// errTooManyRedirects is returned after maxRedirects same-origin
// redirects.
var errTooManyRedirects = fmt.Errorf("stopped after %d redirects", maxRedirects)

// sameOriginRedirect returns an http.Client CheckRedirect policy that
// follows a redirect only to the exact controller origin (https, same
// host and port) and at most maxRedirects times. net/http forwards the
// login form body (307/308) and custom headers such as X-XSRF-TOKEN to
// the redirect target, so a redirect to any other origin is refused
// rather than followed; the 3xx response then surfaces as an HTTP
// error. The redirect target is never included in the error.
func sameOriginRedirect(origin *url.URL) func(*http.Request, []*http.Request) error {
	want := originKey(origin)
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return errTooManyRedirects
		}
		if originKey(req.URL) != want {
			return errCrossOriginRedirect
		}
		return nil
	}
}

// originKey returns scheme://host:port with the default https port
// made explicit and the host lower-cased, so equivalent origins compare
// equal.
func originKey(u *url.URL) string {
	port := u.Port()
	if port == "" && u.Scheme == "https" {
		port = "443"
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Hostname()) + ":" + port
}

// context returns the client's run context.
func (c *Client) context() context.Context {
	if c.ctx == nil {
		return context.Background()
	}
	return c.ctx
}

// sleepContext waits d or until ctx is done, returning ctx's error in
// the latter case.
func sleepContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// rateToInterval converts a requests/second rate to the minimum
// interval between requests. perSecond <= 0 is treated as 1 (never
// fully disabled pass a large value instead if throttling genuinely
// isn't wanted).
func rateToInterval(perSecond int) time.Duration {
	if perSecond <= 0 {
		perSecond = 1
	}
	return time.Second / time.Duration(perSecond)
}

// throttleSiteName blocks until at least siteNameMinInterval has
// passed since the previous GetSiteName call, so concurrent bulk
// resolution (resolveSiteNamesConcurrently in sites_select.go) never
// exceeds --site-name-rate requests/second regardless of worker count.
func (c *Client) throttleSiteName() {
	c.siteNameMu.Lock()
	defer c.siteNameMu.Unlock()
	wait := c.siteNameMinInterval - time.Since(c.siteNameLastRequest)
	if wait > 0 {
		_ = sleepContext(c.context(), wait)
	}
	c.siteNameLastRequest = time.Now()
}

// Login authenticates against the SD-WAN Manager controller:
// session-cookie login via /j_security_check, followed by minting an
// XSRF token via GET /dataservice/client/token. The XSRF token is
// stored and attached as X-XSRF-TOKEN on every subsequent request.
func (c *Client) Login(username, password string) error {
	c.authMu.Lock()
	defer c.authMu.Unlock()
	c.username, c.password = username, password
	if err := c.loginLocked(); err != nil {
		return err
	}
	c.logger.Info("SD-WAN Manager login successful", "url", c.baseURL)
	return nil
}

// loginLocked performs the session login and XSRF token request with
// authMu held, and bumps authGen on success.
func (c *Client) loginLocked() error {
	const op = "/j_security_check"
	form := url.Values{
		"j_username": {c.username},
		"j_password": {c.password},
	}
	req, err := http.NewRequestWithContext(c.context(), http.MethodPost, c.baseURL+op, strings.NewReader(form.Encode()))
	if err != nil {
		return &apiError{Op: op, Category: categoryTransport}
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return transportError(op, err)
	}
	defer resp.Body.Close()

	body, err := readBounded(resp.Body)
	if err != nil {
		if errors.Is(err, errResponseTooLarge) {
			return &apiError{Op: op, Status: resp.StatusCode, Category: categoryTooLarge}
		}
		return transportError(op, err)
	}

	if resp.StatusCode != http.StatusOK {
		return &apiError{Op: op, Status: resp.StatusCode, Category: statusCategory(resp.StatusCode)}
	}
	// A re-served HTML login page is the commonly documented SD-WAN
	// Manager indicator of a rejected username/password
	// (j_security_check itself always answers 200 either way).
	if isHTML(body) {
		return &apiError{Op: op, Status: resp.StatusCode, Category: categoryDenied, Err: errInvalidCredentials}
	}

	token, err := c.fetchXSRFToken()
	if err != nil {
		return err
	}
	c.xsrfToken = token
	c.authGen++
	return nil
}

// reauthenticate renews an expired session once per run. gen is the
// login generation the failed request was sent with: when another
// request already renewed the session since, the caller simply retries.
// It reports whether the caller should retry its request.
func (c *Client) reauthenticate(gen int) bool {
	c.authMu.Lock()
	defer c.authMu.Unlock()
	if c.authGen != gen {
		return true
	}
	if c.reauths >= maxReauthentications || c.username == "" {
		return false
	}
	c.reauths++
	c.xsrfToken = ""
	if err := c.loginLocked(); err != nil {
		c.logger.Warn("SD-WAN Manager session renewal failed", "category", failureCategory(err), "err", err)
		return false
	}
	c.logger.Info("SD-WAN Manager session renewed after expiry")
	return true
}

// session returns the current XSRF token and login generation.
func (c *Client) session() (string, int) {
	c.authMu.Lock()
	defer c.authMu.Unlock()
	return c.xsrfToken, c.authGen
}

// isHTML reports whether a reply body is an HTML page (the Manager
// re-serves its login page for a rejected login or an expired session).
func isHTML(body []byte) bool {
	t := bytes.ToLower(bytes.TrimSpace(body))
	if len(t) > 512 {
		t = t[:512]
	}
	return bytes.HasPrefix(t, []byte("<!doctype html")) || bytes.Contains(t, []byte("<html"))
}

// errInvalidCredentials marks a login rejected by the controller.
var errInvalidCredentials = errors.New("invalid credentials")

// fetchXSRFToken retrieves the XSRF token via GET
// /dataservice/client/token?json=true.
func (c *Client) fetchXSRFToken() (string, error) {
	const op = "/dataservice/client/token"
	body, err := c.fetch(op+"?json=true", false)
	if err != nil {
		return "", err
	}
	var parsed struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil || parsed.Token == "" {
		return "", &apiError{Op: op, Status: http.StatusOK, Category: categoryMalformed, Err: errors.New("no token in reply")}
	}
	return parsed.Token, nil
}

// max429Retries and baseRetryDelay bound the retry-with-backoff applied
// to HTTP 429 (Too Many Requests) responses in get(). Bulk site-name
// resolution (resolveSiteNamesConcurrently in sites_select.go) is the
// most likely caller to trip rate limiting. 429 means "retry later,"
// unlike a permanent failure (403/404), so it is worth a few
// backed-off attempts before giving up.
const (
	max429Retries  = 4
	baseRetryDelay = 500 * time.Millisecond
	maxRetryDelay  = 8 * time.Second
)

// get performs an authenticated GET against the SD-WAN Manager
// dataservice API and returns the raw response body. HTTP 429 responses
// are retried with backoff (honoring a Retry-After header when the
// server sends one); any other non-200 status returns immediately.
func (c *Client) get(path string) ([]byte, error) {
	return c.fetch(path, true)
}

// fetch performs an authenticated GET. See get; allowReauth is false
// for the token request made during login itself, which runs with
// authMu already held: it sends no XSRF token and never renews the
// session, so it must not touch authMu.
func (c *Client) fetch(path string, allowReauth bool) ([]byte, error) {
	op := operationName(path)
	var rateLimited, transient int
	reauthed := !allowReauth

	for {
		var token string
		var gen int
		if allowReauth {
			token, gen = c.session()
		}
		req, err := http.NewRequestWithContext(c.context(), http.MethodGet, c.baseURL+path, nil)
		if err != nil {
			return nil, &apiError{Op: op, Category: categoryTransport}
		}
		if token != "" {
			req.Header.Set("X-XSRF-TOKEN", token)
		}
		req.Header.Set("Accept", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			te := transportError(op, err)
			if te.Category == categoryTimeout && transient < maxTransientRetries {
				transient++
				if werr := sleepContext(c.context(), baseRetryDelay<<transient); werr != nil {
					return nil, transportError(op, werr)
				}
				continue
			}
			return nil, te
		}

		body, readErr := readBounded(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			if errors.Is(readErr, errResponseTooLarge) {
				return nil, &apiError{Op: op, Status: resp.StatusCode, Category: categoryTooLarge}
			}
			te := transportError(op, readErr)
			if te.Category == categoryTimeout && transient < maxTransientRetries {
				transient++
				if werr := sleepContext(c.context(), baseRetryDelay<<transient); werr != nil {
					return nil, transportError(op, werr)
				}
				continue
			}
			return nil, te
		}

		// An expired session answers 401, or re-serves the HTML login
		// page with 200; renew it once and retry this request once.
		expired := resp.StatusCode == http.StatusUnauthorized || (resp.StatusCode == http.StatusOK && isHTML(body))
		if expired {
			if !reauthed && c.reauthenticate(gen) {
				reauthed = true
				continue
			}
			return nil, &apiError{Op: op, Status: resp.StatusCode, Category: categoryDenied, Err: errSessionExpired}
		}

		switch {
		case resp.StatusCode == http.StatusTooManyRequests:
			if rateLimited < max429Retries {
				rateLimited++
				if werr := sleepContext(c.context(), retryDelay(resp, rateLimited-1)); werr != nil {
					return nil, transportError(op, werr)
				}
				continue
			}
			return nil, &apiError{Op: op, Status: resp.StatusCode, Category: categoryRateLimited}
		case resp.StatusCode >= 500 && transient < maxTransientRetries:
			transient++
			if werr := sleepContext(c.context(), baseRetryDelay<<transient); werr != nil {
				return nil, transportError(op, werr)
			}
			continue
		case resp.StatusCode != http.StatusOK:
			return nil, &apiError{Op: op, Status: resp.StatusCode, Category: statusCategory(resp.StatusCode)}
		}
		return body, nil
	}
}

// retryDelay honors a Retry-After response header (seconds form) when
// present, otherwise backs off exponentially from baseRetryDelay; both
// are capped at maxRetryDelay so a server cannot stall the run.
func retryDelay(resp *http.Response, attempt int) time.Duration {
	if v := resp.Header.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
			return min(time.Duration(secs)*time.Second, maxRetryDelay)
		}
	}
	delay := baseRetryDelay * time.Duration(1<<attempt)
	if delay > maxRetryDelay {
		delay = maxRetryDelay
	}
	return delay
}

// GetDevices fetches the full device inventory via GET
// /dataservice/device.
func (c *Client) GetDevices() ([]Device, error) {
	devices, _, err := c.GetDevicesWithRaw()
	return devices, err
}

// GetDevicesWithRaw is GetDevices plus each device's own raw JSON
// object, aligned index-for-index with the returned []Device used for
// --include-raw-body (see wantRawBody in catalystsdwanmanager.go), a
// lossless fallback for fields this producer doesn't model yet.
func (c *Client) GetDevicesWithRaw() ([]Device, []json.RawMessage, error) {
	body, err := c.get("/dataservice/device")
	if err != nil {
		return nil, nil, err
	}
	return decodeListResponseWithRaw[Device]("/dataservice/device", body)
}

// decodeListResponse parses an SD-WAN Manager list endpoint response,
// trying the common {"data": [...]} envelope first (decided by the
// first non-whitespace byte) and falling back to a bare JSON array.
// Shared by every list endpoint on this client so the
// envelope-vs-bare-array defensiveness isn't duplicated per endpoint.
// Real SD-WAN Manager deployments commonly wrap list responses as
// {"data": [...]} instead, this handles either shape without guessing
// which one a given controller version actually returns.
func decodeListResponse[T any](path string, body []byte) ([]T, error) {
	trimmed := bytes.TrimSpace(body)
	malformed := func(err error) error {
		return &apiError{Op: path, Status: http.StatusOK, Category: categoryMalformed, Err: err}
	}
	if len(trimmed) == 0 {
		return nil, malformed(errors.New("empty reply"))
	}

	if trimmed[0] == '{' {
		var envelope map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &envelope); err != nil {
			return nil, malformed(err)
		}
		data, ok := envelope["data"]
		if !ok {
			// An error object on a 200 reply must never read as an
			// empty, successful listing.
			if _, isErr := envelope["error"]; isErr {
				return nil, &apiError{Op: path, Status: http.StatusOK, Category: categoryServerError, Err: errors.New("error object in reply")}
			}
			return nil, malformed(errors.New("no data member"))
		}
		var items []T
		if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
			return nil, nil
		}
		if err := json.Unmarshal(data, &items); err != nil {
			return nil, malformed(err)
		}
		return items, nil
	}

	var items []T
	if err := json.Unmarshal(trimmed, &items); err != nil {
		return nil, malformed(err)
	}
	return items, nil
}

// decodeListResponseWithRaw parses body exactly like decodeListResponse
// into []T, and additionally decodes the same body a second time into
// each item's own raw json.RawMessage (json.RawMessage's UnmarshalJSON
// just captures the original bytes, so this is a second cheap decode
// pass, not a second HTTP round-trip) used only when --include-raw-body
// is requested (see wantRawBody in catalystsdwanmanager.go), so a field
// this producer doesn't model yet is never silently lost.
func decodeListResponseWithRaw[T any](path string, body []byte) ([]T, []json.RawMessage, error) {
	items, err := decodeListResponse[T](path, body)
	if err != nil {
		return nil, nil, err
	}
	raws, err := decodeListResponse[json.RawMessage](path, body)
	if err != nil {
		return nil, nil, err
	}
	return items, raws, nil
}

// flexString unmarshals a JSON string or a bare JSON number into a Go
// string. SD-WAN Manager's OpenAPI spec documents fields like
// mtu/speed-mbps as quoted strings (e.g. "1500"), but some controllers
// return speed-mbps as a bare JSON number instead which a string-typed
// struct field rejects outright, failing the whole response's unmarshal
// and silently dropping every interface for the device, not just the
// one field. flexString tolerates either representation so one
// inconsistently-typed field can't take out the entire response.
type flexString string

// flexInt64 unmarshals a JSON number or a JSON string holding an
// integer into an int64. An empty string, null, or a string that is not
// an integer decodes to 0 ("unknown") rather than failing the whole
// reply, since these fields are display enrichment, not identity.
type flexInt64 int64

func (f *flexInt64) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) > 0 && data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		if err != nil {
			*f = 0
			return nil
		}
		*f = flexInt64(n)
		return nil
	}
	if bytes.Equal(data, []byte("null")) {
		*f = 0
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(data, &n); err != nil {
		return err
	}
	if i, err := n.Int64(); err == nil {
		*f = flexInt64(i)
		return nil
	}
	if fl, err := n.Float64(); err == nil {
		*f = flexInt64(int64(fl))
		return nil
	}
	*f = 0
	return nil
}

func (f *flexString) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*f = flexString(s)
		return nil
	}
	*f = flexString(bytes.TrimSpace(data))
	return nil
}

// Interface is the subset of the GET /dataservice/device/interface
// response fields (tag "Monitoring - Interface") this producer maps to
// OSIRIS network.router.port/network.interface resources. Field names
// taken verbatim from the SD-WAN Manager OpenAPI spec's documented
// example. The endpoint returns one row per (interface, address-family)
// pair af-type distinguishes an interface's IPv4 row from its IPv6 row
// see mergeInterfaceRows in transform_interfaces.go for how these are
// reconciled. Mtu/SpeedMbps use flexString (not string) since SD-WAN
// Manager does not consistently type these as JSON strings across
// controller versions/platforms, see flexString's doc comment.
// Description is absent from the SD-WAN Manager OpenAPI spec's own
// documented example for this endpoint, but is present on real
// interfaces in practice, not every interface sets it, an unconfigured
// one simply omits the field.
type Interface struct {
	VDeviceHostName string     `json:"vdevice-host-name"`
	IfName          string     `json:"ifname"`
	AFType          string     `json:"af-type"`
	IPAddress       string     `json:"ip-address"`
	HWAddr          string     `json:"hwaddr"`
	IfAdminStatus   string     `json:"if-admin-status"`
	IfOperStatus    string     `json:"if-oper-status"`
	VPNID           string     `json:"vpn-id"`
	PortType        string     `json:"port-type"`
	Duplex          string     `json:"duplex"`
	Mtu             flexString `json:"mtu"`
	SpeedMbps       flexString `json:"speed-mbps"`
	EncapType       string     `json:"encap-type"`
	Description     string     `json:"description"`
}

// GetDeviceInterfaces fetches per-device interface details via GET
// /dataservice/device/interface?deviceId={system-ip}, used for the base
// network.interface resource (MAC, private IP, admin/oper state).
func (c *Client) GetDeviceInterfaces(systemIP string) ([]Interface, error) {
	ifaces, _, err := c.GetDeviceInterfacesWithRaw(systemIP)
	return ifaces, err
}

// GetDeviceInterfacesWithRaw is GetDeviceInterfaces plus each row's own
// raw JSON object, for --include-raw-body (see wantRawBody in
// catalystsdwanmanager.go).
func (c *Client) GetDeviceInterfacesWithRaw(systemIP string) ([]Interface, []json.RawMessage, error) {
	path := "/dataservice/device/interface?deviceId=" + url.QueryEscape(systemIP)
	body, err := c.get(path)
	if err != nil {
		return nil, nil, err
	}
	return decodeListResponseWithRaw[Interface]("/dataservice/device/interface", body)
}

// WANInterface is the subset of the GET
// /dataservice/device/control/waninterface response fields (tag
// "Monitoring - WAN Interface") used to enrich a transport-facing
// network.interface resource with its public/NAT address and color.
// Field names taken verbatim from the SD-WAN Manager OpenAPI spec's
// documented example.
type WANInterface struct {
	VDeviceHostName string `json:"vdevice-host-name"`
	Interface       string `json:"interface"`
	Color           string `json:"color"`
	PrivateIP       string `json:"private-ip"`
	PublicIP        string `json:"public-ip"`
	NatType         string `json:"nat-type"`
	OperationState  string `json:"operation-state"`
}

// GetWANInterfaces fetches per-device WAN transport details via GET
// /dataservice/device/control/waninterface?deviceId={system-ip}, joined
// back to GetDeviceInterfaces results by interface name in
// transform_interfaces.go.
func (c *Client) GetWANInterfaces(systemIP string) ([]WANInterface, error) {
	wanIfaces, _, err := c.GetWANInterfacesWithRaw(systemIP)
	return wanIfaces, err
}

// GetWANInterfacesWithRaw is GetWANInterfaces plus each row's own raw
// JSON object, for --include-raw-body (see wantRawBody in
// catalystsdwanmanager.go).
func (c *Client) GetWANInterfacesWithRaw(systemIP string) ([]WANInterface, []json.RawMessage, error) {
	path := "/dataservice/device/control/waninterface?deviceId=" + url.QueryEscape(systemIP)
	body, err := c.get(path)
	if err != nil {
		return nil, nil, err
	}
	return decodeListResponseWithRaw[WANInterface]("/dataservice/device/control/waninterface", body)
}

// SiteTunnel is a single SD-WAN tunnel entry within a SiteCircuit, as
// returned by GET /dataservice/topology/monitor/site/{siteId}. Name
// encodes both tunnel endpoints as "{ipA}:{colorA}-{ipB}:{colorB}" -
// see parseTunnelEndpoints in transform_connections.go.
type SiteTunnel struct {
	Name      string   `json:"name"`
	Health    string   `json:"health"`
	State     string   `json:"state"`
	VqoeScore *float64 `json:"vqoe_score"` // nil when absent; 0 is a real score.
}

// SiteCircuit is a per-color transport circuit within a
// SiteTopologyDevice.
type SiteCircuit struct {
	Color         string       `json:"color"`
	SystemIP      string       `json:"system_ip"`
	CircuitHealth string       `json:"circuit-health"`
	Tunnels       []SiteTunnel `json:"tunnels"`
}

// SiteTopologyDevice is a single device entry within a GET
// /dataservice/topology/monitor/site/{siteId} response. Field names
// taken verbatim from the SD-WAN Manager OpenAPI spec's documented
// example.
type SiteTopologyDevice struct {
	DeviceID     string        `json:"device-id"`
	DeviceHealth string        `json:"device-health"`
	Circuits     []SiteCircuit `json:"circuits"`
}

// GetSiteTopologyMonitor fetches SD-WAN tunnel/circuit data for a site
// via GET /dataservice/topology/monitor/site/{siteId}.
func (c *Client) GetSiteTopologyMonitor(siteID string) ([]SiteTopologyDevice, error) {
	devices, _, err := c.GetSiteTopologyMonitorWithRaw(siteID)
	return devices, err
}

// GetSiteTopologyMonitorWithRaw is GetSiteTopologyMonitor plus each
// device entry's own raw JSON object, for --include-raw-body (see
// wantRawBody in catalystsdwanmanager.go).
func (c *Client) GetSiteTopologyMonitorWithRaw(siteID string) ([]SiteTopologyDevice, []json.RawMessage, error) {
	path := "/dataservice/topology/monitor/site/" + url.PathEscape(siteID)
	body, err := c.get(path)
	if err != nil {
		return nil, nil, err
	}
	return decodeListResponseWithRaw[SiteTopologyDevice]("/dataservice/topology/monitor/site", body)
}

// OMPLink is a single global OMP control-plane connection pair, as
// returned by GET /dataservice/device/omp/links. Unlike most
// /dataservice/device/* endpoints this is not scoped to a single
// deviceId, one call covers every device the credential can see. Field
// names taken verbatim from the SD-WAN Manager OpenAPI spec's
// documented example.
type OMPLink struct {
	State        string `json:"state"`
	ADeviceID    string `json:"adeviceId"`
	BDeviceID    string `json:"bdeviceId"`
	ASystemIP    string `json:"asystem-ip"`
	BSystemIP    string `json:"bsystem-ip"`
	ASiteID      string `json:"asite-id"`
	BSiteID      string `json:"bsite-id"`
	AHostName    string `json:"ahost-name"`
	BHostName    string `json:"bhost-name"`
	APersonality string `json:"apersonality"`
	BPersonality string `json:"bpersonality"`
}

// ompLinkStates are the two values GetOMPLinks queries for. The
// endpoint's "state" query parameter is required, but the SD-WAN
// Manager OpenAPI spec never documents its accepted values, no enum, no
// component schema, just the free-text description "Connection state".
// Only "up" and "down" appear in the spec's own examples; "all" is not
// documented as accepted, so this queries both states explicitly and
// merges the results rather than gambling on an unverified value.
var ompLinkStates = []string{"up", "down"}

// GetOMPLinks fetches the global OMP control-plane peering list via GET
// /dataservice/device/omp/links, once per state in ompLinkStates
// (merged). A failure on either call returns immediately, "some
// states, silently incomplete" is worse than a clear error for data
// meant to represent the full OMP peering topology.
func (c *Client) GetOMPLinks() ([]OMPLink, error) {
	var links []OMPLink
	for _, state := range ompLinkStates {
		body, err := c.get("/dataservice/device/omp/links?state=" + url.QueryEscape(state))
		if err != nil {
			return nil, err
		}
		batch, err := decodeListResponse[OMPLink]("/dataservice/device/omp/links", body)
		if err != nil {
			return nil, err
		}
		links = append(links, batch...)
	}
	return links, nil
}

// OMPPeer is a single OMP peer entry for one device, as returned by GET
// /dataservice/device/omp/peers?deviceId={system-ip}, typically a WAN
// edge's control-plane sessions to its vsmart/vbond controllers. Field
// names taken verbatim from the SD-WAN Manager OpenAPI spec's
// documented example.
type OMPPeer struct {
	VDeviceHostName string `json:"vdevice-host-name"`
	Peer            string `json:"peer"`
	Type            string `json:"type"`
	State           string `json:"state"`
}

// GetOMPPeers fetches a device's OMP peer list via GET
// /dataservice/device/omp/peers?deviceId={system-ip}.
func (c *Client) GetOMPPeers(systemIP string) ([]OMPPeer, error) {
	peers, _, err := c.GetOMPPeersWithRaw(systemIP)
	return peers, err
}

// GetOMPPeersWithRaw is GetOMPPeers plus each peer's own raw JSON
// object, for --include-raw-body (see wantRawBody in
// catalystsdwanmanager.go).
func (c *Client) GetOMPPeersWithRaw(systemIP string) ([]OMPPeer, []json.RawMessage, error) {
	path := "/dataservice/device/omp/peers?deviceId=" + url.QueryEscape(systemIP)
	body, err := c.get(path)
	if err != nil {
		return nil, nil, err
	}
	return decodeListResponseWithRaw[OMPPeer]("/dataservice/device/omp/peers", body)
}

// GetPeerWANInterfaces is GetWANInterfaces for a tunnel peer outside
// the selected sites (see collectNeighborWAN in collect.go). Like
// GetSiteName it is a bulk, one-call-per-device lookup, so it shares
// the --site-name-rate throttle: a hub site can peer with hundreds of
// devices.
func (c *Client) GetPeerWANInterfaces(systemIP string) ([]WANInterface, error) {
	c.throttleSiteName()
	wan, _, err := c.GetWANInterfacesWithRaw(systemIP)
	return wan, err
}

// GetSiteName fetches the human-readable site name for a site-id via
// GET /dataservice/topology/device/site/{siteId}. Unlike GetDevices,
// this endpoint's response shape is not documented in the SD-WAN
// Manager OpenAPI spec (the schema is a bare, empty {"type":
// "object"}); the {"site_name": "...", "data": [...]} shape used here
// is the observed response. Callers should treat this as best-effort
// (see siteDisplayName in sites_select.go): some credentials may lack
// access to this endpoint, and a failure here should never block
// collection.
func (c *Client) GetSiteName(siteID string) (string, error) {
	c.throttleSiteName()
	body, err := c.get("/dataservice/topology/device/site/" + url.PathEscape(siteID))
	if err != nil {
		return "", err
	}

	var parsed struct {
		SiteName string `json:"site_name"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", &apiError{Op: "/dataservice/topology/device/site", Status: http.StatusOK, Category: categoryMalformed, Err: err}
	}
	return parsed.SiteName, nil
}

// operationName returns the endpoint path of a request without its
// query string, so diagnostics identify the operation without echoing
// request parameters. Response bodies are never part of a diagnostic:
// an error body can carry credentials, tokens or other sensitive
// content, and truncating it bounds only its size, not its
// sensitivity.
func operationName(path string) string {
	if i := strings.IndexByte(path, '?'); i >= 0 {
		return path[:i]
	}
	return path
}

// transportCause strips the request URL that net/http adds to
// transport errors (*url.Error), keeping only the underlying cause, so
// a diagnostic never repeats request parameters or a redirect target.
func transportCause(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}
