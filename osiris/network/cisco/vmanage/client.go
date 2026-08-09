// client.go - vManage REST API client for the
// Cisco SD-WAN Manager OSIRIS JSON Producer.
//
// Authentication is the standard vManage session-cookie + XSRF flow:
// POST /j_security_check (form-encoded username/password) establishes
// a JSESSIONID session cookie (handled automatically by the cookie jar
// from run.NewHTTPClient), then GET /dataservice/client/token mints an
// XSRF token that must be sent as the X-XSRF-TOKEN header on every
// subsequent /dataservice call. /j_security_check is a servlet outside
// /dataservice and is not part of the vManage OpenAPI spec, so its
// failure heuristic (an HTML login page being re-served) follows the
// commonly documented vManage automation pattern rather than a spec
// definition.
//
// OSIRIS JSON Producer for Cisco SD-WAN Manager (vManage) introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package vmanage

import (
	"bytes"
	"encoding/json"
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

// requestTimeout bounds every HTTP call this client makes. Deployments
// with many sites resolve site names via one call per site (see
// GetSiteName / siteDisplayName in sites_select.go) without a
// timeout, a single slow or silently-dropped request can hang the
// whole run indefinitely. run.NewHTTPClient sets no timeout (shared by
// apic/nxos/iosxe too), so it is applied here, scoped to vmanage only.
const requestTimeout = 15 * time.Second

// Client is a thin HTTP client for the vManage dataservice REST API.
type Client struct {
	baseURL    string
	httpClient *http.Client
	xsrfToken  string
	logger     *slog.Logger

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
// vManage OpenAPI spec's documented example for this endpoint.
//
// Latitude/Longitude are typed any because the spec's own example
// mixes a string latitude ("37.666684") with a numeric longitude
// (-122.777023) for the same object.
//
// HealthState ("state") is the dashboard health rollup
// (green/red/yellow), distinct from Reachability and Status.
type Device struct {
	DeviceID            string   `json:"deviceId"`
	SystemIP            string   `json:"system-ip"`
	HostName            string   `json:"host-name"`
	UUID                string   `json:"uuid"`
	SiteID              string   `json:"site-id"`
	Personality         string   `json:"personality"`
	DeviceType          string   `json:"device-type"`
	DeviceModel         string   `json:"device-model"`
	Reachability        string   `json:"reachability"`
	Status              string   `json:"status"`
	Validity            string   `json:"validity"`
	Version             string   `json:"version"`
	Platform            string   `json:"platform"`
	BoardSerial         string   `json:"board-serial"`
	CertificateValidity string   `json:"certificate-validity"`
	DeviceGroups        []string `json:"device-groups"`
	Latitude            any      `json:"latitude"`
	Longitude           any      `json:"longitude"`
	LastUpdated         int64    `json:"lastupdated"`
	HealthState         string   `json:"state"`
	StateDescription    string   `json:"state_description"`
	UptimeDate          int64    `json:"uptime-date"`
	ConnectedVManages   []string `json:"connectedVManages"`
}

// NewClient creates a vManage client targeting the given controller.
func NewClient(cfg *Config, logger *slog.Logger) *Client {
	addr := cfg.Host
	if strings.Contains(addr, ":") {
		addr = "[" + addr + "]"
	}
	httpClient := run.NewHTTPClient(cfg.InsecureTLS)
	httpClient.Timeout = requestTimeout
	return &Client{
		baseURL:             fmt.Sprintf("https://%s:%d", addr, cfg.Port),
		httpClient:          httpClient,
		logger:              logger,
		siteNameMinInterval: rateToInterval(cfg.SiteNameRateLimit),
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
		time.Sleep(wait)
	}
	c.siteNameLastRequest = time.Now()
}

// Login authenticates against the vManage controller: session-cookie
// login via /j_security_check, followed by minting an XSRF token via
// GET /dataservice/client/token. The XSRF token is stored and attached
// as X-XSRF-TOKEN on every subsequent request.
func (c *Client) Login(username, password string) error {
	form := url.Values{
		"j_username": {username},
		"j_password": {password},
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/j_security_check", strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("vManage login: failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("vManage login request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("vManage login: failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("vManage login failed (HTTP %d): %s", resp.StatusCode, truncateBody(body))
	}
	// A re-served HTML login page is the commonly documented vManage
	// indicator of a rejected username/password (j_security_check
	// itself always answers 200 either way).
	if bytes.Contains(bytes.ToLower(body), []byte("<html")) {
		return fmt.Errorf("vManage login failed: invalid credentials")
	}

	token, err := c.fetchXSRFToken()
	if err != nil {
		return fmt.Errorf("vManage login: %w", err)
	}
	c.xsrfToken = token

	c.logger.Info("vManage login successful", "url", c.baseURL)
	return nil
}

// fetchXSRFToken retrieves the XSRF token via GET /dataservice/client/token.
func (c *Client) fetchXSRFToken() (string, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+"/dataservice/client/token?json=true", nil)
	if err != nil {
		return "", fmt.Errorf("failed to create token request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token request failed (HTTP %d): %s", resp.StatusCode, truncateBody(body))
	}

	var parsed struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("failed to parse token response: %w", err)
	}
	if parsed.Token == "" {
		return "", fmt.Errorf("empty XSRF token in response")
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

// get performs an authenticated GET against the vManage dataservice
// API and returns the raw response body. HTTP 429 responses are
// retried with backoff (honoring a Retry-After header when the server
// sends one); any other non-200 status returns immediately.
func (c *Client) get(path string) ([]byte, error) {
	var lastErr error

	for attempt := 0; attempt <= max429Retries; attempt++ {
		req, err := http.NewRequest(http.MethodGet, c.baseURL+path, nil)
		if err != nil {
			return nil, fmt.Errorf("vManage %s: failed to create request: %w", path, err)
		}
		if c.xsrfToken != "" {
			req.Header.Set("X-XSRF-TOKEN", c.xsrfToken)
		}
		req.Header.Set("Accept", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("vManage %s: request failed: %w", path, err)
		}

		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("vManage %s: failed to read response: %w", path, readErr)
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			lastErr = fmt.Errorf("vManage %s failed (HTTP 429): %s", path, truncateBody(body))
			if attempt < max429Retries {
				time.Sleep(retryDelay(resp, attempt))
				continue
			}
			return nil, lastErr
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("vManage %s failed (HTTP %d): %s", path, resp.StatusCode, truncateBody(body))
		}
		return body, nil
	}

	return nil, lastErr
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

// GetDevices fetches the full device inventory via GET
// /dataservice/device.
func (c *Client) GetDevices() ([]Device, error) {
	devices, _, err := c.GetDevicesWithRaw()
	return devices, err
}

// GetDevicesWithRaw is GetDevices plus each device's own raw JSON
// object, aligned index-for-index with the returned []Device used
// for --include-raw-body (see wantRawBody in vmanage.go), a lossless
// fallback for fields this producer doesn't model yet.
func (c *Client) GetDevicesWithRaw() ([]Device, []json.RawMessage, error) {
	body, err := c.get("/dataservice/device")
	if err != nil {
		return nil, nil, err
	}
	return decodeListResponseWithRaw[Device]("/dataservice/device", body)
}

// decodeListResponse parses a vManage list endpoint response, trying
// the common {"data": [...]} envelope first (decided by the first
// non-whitespace byte) and falling back to a bare JSON array. Shared by
// every list endpoint on this client so the envelope-vs-bare-array
// defensiveness isn't duplicated per endpoint. The spec's own
// documented examples show a bare array for most of these endpoints,
// but real vManage deployments commonly wrap list responses as
// {"data": [...]} instead, this handles either shape without guessing
// which one a given controller version actually returns.
func decodeListResponse[T any](path string, body []byte) ([]T, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, nil
	}

	if trimmed[0] == '{' {
		var envelope struct {
			Data []T `json:"data"`
		}
		if err := json.Unmarshal(trimmed, &envelope); err != nil {
			return nil, fmt.Errorf("vManage %s: failed to parse envelope response: %w", path, err)
		}
		return envelope.Data, nil
	}

	var items []T
	if err := json.Unmarshal(trimmed, &items); err != nil {
		return nil, fmt.Errorf("vManage %s: failed to parse response: %w", path, err)
	}
	return items, nil
}

// decodeListResponseWithRaw parses body exactly like decodeListResponse
// into []T, and additionally decodes the same body a second time into
// each item's own raw json.RawMessage (json.RawMessage's UnmarshalJSON
// just captures the original bytes, so this is a second cheap decode
// pass, not a second HTTP round-trip) used only when
// --include-raw-body is requested (see wantRawBody in vmanage.go), so
// a field this producer doesn't model yet is never silently lost.
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
// string. vManage's OpenAPI spec documents fields like mtu/speed-mbps
// as quoted strings (e.g. "1500"), but some controllers return
// speed-mbps as a bare JSON number instead which a string-typed
// struct field rejects outright, failing the whole response's
// unmarshal and silently dropping every interface for the device, not
// just the one field. flexString tolerates either representation so
// one inconsistently-typed field can't take out the entire response.
type flexString string

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
// taken verbatim from the vManage OpenAPI spec's documented example.
// The endpoint returns one row per (interface, address-family) pair
// af-type distinguishes an interface's IPv4 row from its IPv6 row
// see selectIPv4Interfaces in transform_interfaces.go for how these
// are deduplicated. Mtu/SpeedMbps use flexString (not string) since
// vManage does not consistently type these as JSON strings across
// controller versions/platforms, see flexString's doc comment.
// Description is absent from the vManage OpenAPI spec's own documented
// example for this endpoint, but is present on real interfaces in
// practice, not every interface sets it, an unconfigured one simply
// omits the field.
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
// vmanage.go).
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
// Field names taken verbatim from the vManage OpenAPI spec's documented
// example.
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
// JSON object, for --include-raw-body (see wantRawBody in vmanage.go).
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
	Name      string  `json:"name"`
	Health    string  `json:"health"`
	State     string  `json:"state"`
	VqoeScore float64 `json:"vqoe_score"`
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
// taken verbatim from the vManage OpenAPI spec's documented example.
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
// wantRawBody in vmanage.go).
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
// deviceId, one call covers every device the credential can see.
// Field names taken verbatim from the vManage OpenAPI spec's documented
// example.
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
// endpoint's "state" query parameter is required, but the vManage
// OpenAPI spec never documents its accepted values, no enum, no
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
// names taken verbatim from the vManage OpenAPI spec's documented
// example.
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
// object, for --include-raw-body (see wantRawBody in vmanage.go).
func (c *Client) GetOMPPeersWithRaw(systemIP string) ([]OMPPeer, []json.RawMessage, error) {
	path := "/dataservice/device/omp/peers?deviceId=" + url.QueryEscape(systemIP)
	body, err := c.get(path)
	if err != nil {
		return nil, nil, err
	}
	return decodeListResponseWithRaw[OMPPeer]("/dataservice/device/omp/peers", body)
}

// GetSiteName fetches the human-readable site name for a site-id via
// GET /dataservice/topology/device/site/{siteId}. Unlike GetDevices,
// this endpoint's response shape is not documented in the vManage
// OpenAPI spec (the schema is a bare, empty {"type": "object"}); the
// {"site_name": "...", "data": [...]} shape used here is the observed
// response. Callers should treat this as best-effort (see
// siteDisplayName in sites_select.go): some credentials may lack
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
		return "", fmt.Errorf("vManage /dataservice/topology/device/site/%s: failed to parse response: %w", siteID, err)
	}
	return parsed.SiteName, nil
}

// Tenant is an opaque vManage tenant record. The spec documents
// GET /dataservice/tenant as returning an array of untyped objects (no
// field names documented anywhere), so tenants are kept as raw maps
// and only used for best-effort metadata.scope population, never for
// switching/iterating into a tenant's own device inventory (see
// package doc comment in vmanage.go).
type Tenant map[string]any

// ListTenants fetches the tenant list via GET /dataservice/tenant, for
// metadata.scope population only. Per the spec's documented example,
// this endpoint returns a bare JSON array (unlike GetDevices, whose
// envelope shape is ambiguous).
func (c *Client) ListTenants() ([]Tenant, error) {
	body, err := c.get("/dataservice/tenant")
	if err != nil {
		return nil, err
	}

	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, nil
	}

	var tenants []Tenant
	if err := json.Unmarshal(trimmed, &tenants); err != nil {
		return nil, fmt.Errorf("vManage /dataservice/tenant: failed to parse response: %w", err)
	}
	return tenants, nil
}

// tenantLabel returns a best-effort display string for a tenant
// record, trying the commonly used identity keys in priority order
// since the spec does not document any field names for this schema.
func tenantLabel(t Tenant) string {
	for _, key := range []string{"name", "tenantName", "orgName", "tenantId", "id"} {
		if v, ok := t[key]; ok {
			switch s := v.(type) {
			case string:
				if s != "" {
					return s
				}
			case float64:
				return strconv.FormatFloat(s, 'f', -1, 64)
			}
		}
	}
	return ""
}

// truncateBody returns the first 200 bytes of a response body for error messages.
func truncateBody(body []byte) string {
	if len(body) > 200 {
		return string(body[:200]) + "..."
	}
	return string(body)
}
