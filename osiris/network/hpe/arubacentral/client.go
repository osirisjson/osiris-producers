// client.go - API Gateway client for the HPE ArubaNetworking
// Central OSIRIS JSON producer.
//
// Implements OAuth2 bearer-token authentication (with refresh-on-401),
// account-wide request throttling (Central enforces 10 API calls/second
// across all tokens on an account).
//
// Endpoint paths and query parameters below are sourced from the Aruba
// Central "network-monitoring/v1" API reference (a 404/failure is
// logged and skipped, never fatal for the OSIRIS JSON producer).
//
// OSIRIS JSON Producer for HPE Aruba Networking Central introduction:
// [OSIRIS-JSON-HPE-ARUBA-NETWORKING]: https://docs.osirisjson.org/osiris-producers/network/hpe-aruba-networking-central
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package arubacentral

import (
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
)

// minRequestInterval throttles to 8 requests/second, a safety margin
// below the account-wide 10 requests/second limit documented for
// Aruba Central (applies across all tokens on the user account).
const minRequestInterval = 125 * time.Millisecond

const pageLimit = 1000

// configPageLimit is the page size used for network-config/v1
// endpoints (sites, device-groups). These reject pageLimit (1000) with
// a 400 PAGE_LIMIT_SIZE_EXCEEDED, unlike the network-monitoring/v1
// endpoints, which accept it.
const configPageLimit = 100

// gatewayVLANPageLimit is the page size for /gateways/{serial}/vlans.
// Observed in production: the API reference documents 0-100 for this
// endpoint specifically (every other network-monitoring/v1 list endpoint
// checked allows up to at least pageLimit).
const gatewayVLANPageLimit = 100

// Client is a rate-limited, auth-refreshing HTTP client for the Aruba
// Central API Gateway.
type Client struct {
	baseURL    string
	httpClient *http.Client
	logger     *slog.Logger
	purpose    string

	creds Credentials

	mu          sync.Mutex
	lastRequest time.Time

	// minInterval overridable by tests to avoid slowing the suite down.
	minInterval time.Duration
}

// NewClient creates an Aruba Central client for
// the given account credentials.
func NewClient(cfg *Config, logger *slog.Logger) *Client {
	return &Client{
		baseURL:     cfg.BaseURL,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
		logger:      logger,
		purpose:     cfg.Purpose,
		creds:       cfg.Credentials,
		minInterval: minRequestInterval,
	}
}

// throttle blocks until at least minInterval has passed
// since the previous request.
func (c *Client) throttle() {
	c.mu.Lock()
	defer c.mu.Unlock()
	wait := c.minInterval - time.Since(c.lastRequest)
	if wait > 0 {
		time.Sleep(wait)
	}
	c.lastRequest = time.Now()
}

// tokenResponse is the OAuth2 token endpoint response shape.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
}

// refresh obtains a replacement access token after a 401, and persists
// updated credentials to TokenFile when one was configured.
// For a GreenLake Personal API client (see personal_api.go), this
// re-mints an access token via client_credentials since no
// refresh_token exists; otherwise it exchanges the current refresh
// token for a new access token (grant_type=refresh_token) as before.
func (c *Client) refresh() error {
	if isPersonalAPIClient(c.creds) {
		token, err := mintPersonalAPIToken(c.httpClient, c.creds.ClientID, c.creds.ClientSecret)
		if err != nil {
			return fmt.Errorf("access token rejected and re-minting a GreenLake Personal API client token failed: %w", err)
		}
		c.creds.AccessToken = token

		if c.creds.TokenFile != "" {
			if err := saveTokenFile(c.creds.TokenFile, c.creds); err != nil {
				c.logger.Warn("failed to persist re-minted token", "file", c.creds.TokenFile, "err", err)
			}
		}
		if c.logger != nil {
			c.logger.Info("re-minted Aruba Central access token via GreenLake Personal API client")
		}
		return nil
	}

	if c.creds.RefreshToken == "" {
		return fmt.Errorf("access token rejected and no refresh token is available")
	}

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", c.creds.RefreshToken)
	form.Set("client_id", c.creds.ClientID)
	form.Set("client_secret", c.creds.ClientSecret)

	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/oauth2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("building token refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	c.throttle()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("token refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading token refresh response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token refresh failed (HTTP %d): %s", resp.StatusCode, truncateBody(body))
	}

	var tok tokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return fmt.Errorf("parsing token refresh response: %w", err)
	}
	if tok.AccessToken == "" {
		return fmt.Errorf("token refresh response did not contain an access_token")
	}

	c.creds.AccessToken = tok.AccessToken
	if tok.RefreshToken != "" {
		c.creds.RefreshToken = tok.RefreshToken
	}

	if c.creds.TokenFile != "" {
		if err := saveTokenFile(c.creds.TokenFile, c.creds); err != nil {
			c.logger.Warn("failed to persist refreshed token", "file", c.creds.TokenFile, "err", err)
		}
	}

	if c.logger != nil {
		c.logger.Info("refreshed Aruba Central access token")
	}
	return nil
}

// get performs a single authenticated GET, refreshing the access token
// once on a 401 before retrying.
func (c *Client) get(path string, query url.Values) ([]byte, error) {
	body, status, err := c.doGet(path, query)
	if err != nil {
		return nil, err
	}
	if status == http.StatusUnauthorized {
		if refreshErr := c.refresh(); refreshErr != nil {
			return nil, fmt.Errorf("GET %s returned 401 and refresh failed: %w", path, refreshErr)
		}
		body, status, err = c.doGet(path, query)
		if err != nil {
			return nil, err
		}
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("GET %s failed (HTTP %d): %s", path, status, truncateBody(body))
	}
	return body, nil
}

func (c *Client) doGet(path string, query url.Values) ([]byte, int, error) {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("building request for %s: %w", path, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.creds.AccessToken)
	req.Header.Set("Accept", "application/json")

	c.throttle()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request to %s failed: %w", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("reading response from %s: %w", path, err)
	}
	return body, resp.StatusCode, nil
}

// listEnvelope covers the list response shapes observed across Aruba
// Central monitoring endpoints: a top-level "items" array (cursor or
// offset paginated) or the same nested one level under "response".
type listEnvelope struct {
	Total    int             `json:"total"`
	Count    int             `json:"count"`
	Next     string          `json:"next"`
	Items    json.RawMessage `json:"items"`
	Response *struct {
		Items json.RawMessage `json:"items"`
		Count int             `json:"count"`
	} `json:"response"`
}

// paginate fetches every page of a list endpoint at the default
// pageLimit page size and returns the raw item messages in order.
// useCursor selects "next"-cursor pagination (top-level device lists);
// otherwise offset-based pagination is used
// (per-device sub-resource lists), matching the two shapes documented
// for this API.
func (c *Client) paginate(path string, query url.Values, useCursor bool) ([]json.RawMessage, error) {
	return c.paginateWithLimit(path, query, useCursor, pageLimit)
}

// paginateWithLimit is paginate with the page size overridable, for
// endpoints that reject the default pageLimit (see configPageLimit).
func (c *Client) paginateWithLimit(path string, query url.Values, useCursor bool, limit int) ([]json.RawMessage, error) {
	if query == nil {
		query = url.Values{}
	}
	query.Set("limit", strconv.Itoa(limit))

	var all []json.RawMessage
	offset := 0
	if !useCursor {
		query.Set("offset", "0")
	}

	for {
		body, err := c.get(path, query)
		if err != nil {
			return nil, err
		}

		var env listEnvelope
		if err := json.Unmarshal(body, &env); err != nil {
			return nil, fmt.Errorf("parsing list response from %s: %w", path, err)
		}

		itemsRaw := env.Items
		if len(itemsRaw) == 0 && env.Response != nil {
			itemsRaw = env.Response.Items
		}

		var page []json.RawMessage
		if len(itemsRaw) > 0 {
			if err := json.Unmarshal(itemsRaw, &page); err != nil {
				return nil, fmt.Errorf("parsing items from %s: %w", path, err)
			}
		}
		all = append(all, page...)

		if len(page) < limit {
			break
		}

		if useCursor {
			if env.Next == "" {
				break
			}
			query.Set("next", env.Next)
		} else {
			offset += limit
			query.Set("offset", strconv.Itoa(offset))
		}
	}

	return all, nil
}

// getOne fetches a single-object endpoint (no list envelope) and
// unmarshals it directly into out. Used for per-device detail endpoints
// that return bare object (e.g. switch VSX peering, stack membership).
func (c *Client) getOne(path string, query url.Values, out any) error {
	body, err := c.get(path, query)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, out)
}

func truncateBody(body []byte) string {
	if len(body) > 300 {
		return string(body[:300]) + "..."
	}
	return string(body)
}

// --- Response types --------------------------------------------------
// Field names and JSON tags mirror the Aruba Central
// network-monitoring/v1 response shapes.

// Switch is a "GET /network-monitoring/v1/switches" list item.
type Switch struct {
	ID              string        `json:"id"`
	Kind            string        `json:"_kind"`
	Deployment      string        `json:"deployment"`
	DeviceName      string        `json:"deviceName"`
	FirmwareVersion string        `json:"firmwareVersion"`
	IPv4            string        `json:"ipv4"`
	IPv6            string        `json:"ipv6"`
	JNumber         string        `json:"jNumber"`
	LastSeenAt      int64         `json:"lastSeenAt"`
	MACAddress      string        `json:"macAddress"`
	Model           string        `json:"model"`
	PublicIP        string        `json:"publicIp"`
	SerialNumber    string        `json:"serialNumber"`
	SiteID          string        `json:"siteId"`
	SiteName        string        `json:"siteName"`
	StackID         string        `json:"stackId"`
	StackMemberID   int           `json:"stackMemberId"`
	Status          string        `json:"status"`
	SwitchRole      string        `json:"switchRole"`
	SwitchType      string        `json:"switchType"`
	Type            string        `json:"type"`
	UptimeInMillis  int64         `json:"uptimeInMillis"`
	SwitchTrends    []SwitchTrend `json:"switchTrends"`
}

// SwitchTrend is the embedded latest-sample telemetry on a Switch.
type SwitchTrend struct {
	CPUUtilization        float64 `json:"cpuUtilization"`
	MemoryUtilization     float64 `json:"memoryUtilization"`
	PoEAvailable          float64 `json:"poeAvailable"`
	PoEConsumption        float64 `json:"poeConsumption"`
	PowerConsumption      float64 `json:"powerConsumption"`
	SystemTemperature     float64 `json:"systemTemperature"`
	TotalPowerConsumption float64 `json:"totalPowerConsumption"`
	Usage                 float64 `json:"usage"`
}

// SwitchInterface is a "GET /switches/{serial}/interfaces" list item.
type SwitchInterface struct {
	ID              string   `json:"id"`
	SerialNumber    string   `json:"serialNumber"`
	Name            string   `json:"name"`
	Index           int      `json:"index"`
	PortIndex       int      `json:"portIndex"`
	Description     string   `json:"description"`
	Alias           string   `json:"alias"`
	AdminStatus     string   `json:"adminStatus"`
	OperStatus      string   `json:"operStatus"`
	Status          string   `json:"status"`
	Connector       string   `json:"connector"`
	Duplex          string   `json:"duplex"`
	Speed           int      `json:"speed"`
	MTU             int      `json:"mtu"`
	IPv4            string   `json:"ipv4"`
	NativeVlan      int      `json:"nativeVlan"`
	AllowedVlans    []string `json:"allowedVlans"`
	Lag             string   `json:"lag"`
	Module          string   `json:"module"`
	PoEStatus       string   `json:"poeStatus"`
	PoEClass        string   `json:"poeClass"`
	Neighbour       string   `json:"neighbour"`
	NeighbourSerial string   `json:"neighbourSerial"`
	NeighbourPort   string   `json:"neighbourPort"`
	NeighbourHealth string   `json:"neighbourHealth"`
}

// SwitchVLAN is a "GET /switches/{serial}/vlans" list item.
type SwitchVLAN struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Status        string   `json:"status"`
	IPv4          string   `json:"ipv4"`
	Interfaces    []string `json:"interfaces"`
	TaggedPorts   []string `json:"taggedPorts"`
	UntaggedPorts []string `json:"untaggedPorts"`
	Voice         string   `json:"voice"`
}

// SwitchHardware is "GET /switches/{serial}/hardware-categories" item.
type SwitchHardware struct {
	ID            string             `json:"id"`
	Model         string             `json:"model"`
	Role          string             `json:"role"`
	SerialNumber  string             `json:"serialNumber"`
	StackMemberID int                `json:"stackMemberId"`
	Status        string             `json:"status"`
	CPU           SwitchHealthStatus `json:"cpu"`
	Memory        SwitchHealthStatus `json:"memory"`
	Temperature   SwitchHealthStatus `json:"temperature"`
	Fans          SwitchComponentSet `json:"fans"`
	PowerSupplies SwitchComponentSet `json:"powerSupplies"`
}

// SwitchHealthStatus is a simple {health} status block used
// across hardware categories.
type SwitchHealthStatus struct {
	Health string `json:"health"`
}

// SwitchComponentSet describes redundant component group (fans, PSUs).
type SwitchComponentSet struct {
	Health     string `json:"health"`
	TotalCount int    `json:"totalCount"`
	UpCount    int    `json:"upCount"`
	FailedIDs  []int  `json:"failedIds"`
}

// SwitchLAG is a "GET /switches/{serial}/lag" list item.
type SwitchLAG struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Count int      `json:"count"`
	Ports []string `json:"ports"`
}

// StackMembers is the "GET /stack/{serial}/members"
// response (a bare object, not a list).
type StackMembers struct {
	ID        string             `json:"id"`
	StackType string             `json:"stackType"`
	Topology  string             `json:"topology"`
	Members   []StackMember      `json:"members"`
	PortLinks []StackPortLinkSet `json:"portLinks"`
}

// StackMember is one chassis participating in a switch stack.
type StackMember struct {
	SerialNumber  string `json:"serialNumber"`
	MACAddress    string `json:"macAddress"`
	Model         string `json:"model"`
	JNumber       string `json:"jNumber"`
	StackMemberID int    `json:"stackMemberId"`
	SwitchRole    string `json:"switchRole"`
	Status        string `json:"status"`
	Health        string `json:"health"`
}

// StackPortLinkSet groups the inter-member links reported
// by one stack member.
type StackPortLinkSet struct {
	SerialNumber string          `json:"serialNumber"`
	Links        []StackPortLink `json:"links"`
}

// StackPortLink is a single inter-member stacking link.
type StackPortLink struct {
	Name         string `json:"name"`
	PeerMemberID int    `json:"peerMemberId"`
	PeerPort     string `json:"peerPort"`
	Serial       string `json:"serial"`
	Speed        int    `json:"speed"`
	Status       string `json:"status"`
	ErrorReason  string `json:"errorReason"`
}

// SwitchVSX is the "GET /switches/{serial}/vsx" response
// (a bare object, not a list).
type SwitchVSX struct {
	Role              string `json:"role"`
	PeerRole          string `json:"peerRole"`
	VSXPeerSerial     string `json:"vsxPeerSerial"`
	VSXPeerSiteID     string `json:"vsxPeerSiteId"`
	VSXPeerName       string `json:"vsxPeerName"`
	ISLPort           string `json:"islPort"`
	PeerISLPort       string `json:"peerIslPort"`
	KeepaliveStatus   string `json:"keepaliveStatus"`
	KeepaliveHealth   string `json:"keepaliveHealth"`
	ConfigSyncDisable bool   `json:"configSyncDisable"`
	VSXHealth         struct {
		Health            string `json:"health"`
		HealthDescription string `json:"healthDescription"`
	} `json:"vsxHealth"`
}

// AccessPoint is a "GET /network-monitoring/v1/aps" list item.
type AccessPoint struct {
	ID                string  `json:"id"`
	SerialNumber      string  `json:"serialNumber"`
	DeviceName        string  `json:"deviceName"`
	MACAddress        string  `json:"macAddress"`
	Model             string  `json:"model"`
	PartNumber        string  `json:"partNumber"`
	FirmwareVersion   string  `json:"firmwareVersion"`
	Deployment        string  `json:"deployment"`
	DeviceFunction    string  `json:"deviceFunction"`
	Role              string  `json:"role"`
	MeshRole          string  `json:"meshRole"`
	Status            string  `json:"status"`
	IPv4              string  `json:"ipv4"`
	IPv6              string  `json:"ipv6"`
	PublicIPv4        string  `json:"publicIpv4"`
	SubnetMask        string  `json:"subnetMask"`
	SiteID            string  `json:"siteId"`
	SiteName          string  `json:"siteName"`
	ClusterID         string  `json:"clusterId"`
	ClusterName       string  `json:"clusterName"`
	DeviceGroupID     string  `json:"deviceGroupId"`
	DeviceGroupName   string  `json:"deviceGroupName"`
	ClientCount       int     `json:"clientCount"`
	WLANCount         int     `json:"wlanCount"`
	CPUUtilization    float64 `json:"cpuUtilization"`
	MemoryUtilization float64 `json:"memoryUtilization"`
	PowerConsumption  float64 `json:"powerConsumption"`
	UptimeInMillis    int64   `json:"uptimeInMillis"`
	Type              string  `json:"type"`
}

// Gateway is a "GET /network-monitoring/v1/gateways" list item.
type Gateway struct {
	ID                string  `json:"id"`
	SerialNumber      string  `json:"serialNumber"`
	DeviceName        string  `json:"deviceName"`
	MACAddress        string  `json:"macAddress"`
	MACRange          string  `json:"macRange"`
	Model             string  `json:"model"`
	FirmwareVersion   string  `json:"firmwareVersion"`
	DeviceFunction    string  `json:"deviceFunction"`
	Role              string  `json:"role"`
	Mode              string  `json:"mode"`
	Status            string  `json:"status"`
	IPAddress         string  `json:"ipAddress"`
	SiteID            string  `json:"siteId"`
	SiteName          string  `json:"siteName"`
	ClusterName       string  `json:"clusterName"`
	CPUUtilization    float64 `json:"cpuUtilization"`
	MemoryUtilization float64 `json:"memoryUtilization"`
	RebootReason      string  `json:"rebootReason"`
	UptimeInMillis    int64   `json:"uptimeInMillis"`
	Type              string  `json:"type"`
}

// APPort is a "GET /aps/{serial}/ports" list item.
type APPort struct {
	ID          string `json:"id"`
	Serial      string `json:"_serial"`
	Name        string `json:"name"`
	PortIndex   int    `json:"portIndex"`
	Status      string `json:"status"`
	Connector   string `json:"connector"`
	Duplex      string `json:"duplex"`
	Speed       string `json:"speed"`
	MACAddress  string `json:"macAddress"`
	NativeVlan  string `json:"nativeVlan"`
	AllowedVlan string `json:"allowedVlan"`
	AccessVlan  string `json:"accessVlan"`
	VlanMode    string `json:"vlanMode"`
}

// APTunnel is a "GET /aps/{serial}/tunnels" list item.
type APTunnel struct {
	ID                    string `json:"id"`
	Serial                string `json:"_serial"`
	TunnelID              string `json:"tunnelId"`
	TunnelName            string `json:"tunnelName"`
	Active                bool   `json:"active"`
	Status                string `json:"status"`
	CryptoType            string `json:"cryptoType"`
	EncapsulationType     string `json:"encapsulationType"`
	SourceIPAddress       string `json:"sourceIpAddress"`
	DestinationIPAddress  string `json:"destinationIpAddress"`
	DestinationMACAddress string `json:"destinationMacAddress"`
	DestinationName       string `json:"destinationName"`
	LastChangeReason      string `json:"lastChangeReason"`
	UptimeInMillis        int64  `json:"uptimeInMillis"`
}

// APWLAN is a "GET /aps/{serial}/wlans" or top-level
// "GET /wlans" list item.
type APWLAN struct {
	ID            string `json:"id"`
	Serial        string `json:"_serial"`
	WLANName      string `json:"wlanName"`
	Band          string `json:"band"`
	Security      string `json:"security"`
	SecurityLevel string `json:"securityLevel"`
	Status        string `json:"status"`
	VLAN          string `json:"vlan"`
	Type          string `json:"type"`
}

// Radio is a "GET /network-monitoring/v1/radios" list item.
type Radio struct {
	ID                 string `json:"id"`
	SerialNumber       string `json:"serialNumber"`
	DeviceName         string `json:"deviceName"`
	SiteID             string `json:"siteId"`
	SiteName           string `json:"siteName"`
	MACAddress         string `json:"macAddress"`
	RadioNumber        int    `json:"radioNumber"`
	RadioType          string `json:"radioType"`
	Band               string `json:"band"`
	Antenna            string `json:"antenna"`
	SpatialStream      string `json:"spatialStream"`
	Status             string `json:"status"`
	ClientCount        int    `json:"clientCount"`
	ChannelChangeCount int    `json:"channelChangeCount"`
	PowerChangeCount   int    `json:"powerChangeCount"`
}

// BSSID is a "GET /network-monitoring/v1/bssids" list item.
type BSSID struct {
	ID              string `json:"id"`
	BSSID           string `json:"bssid"`
	MACAddress      string `json:"macAddress"`
	RadioMACAddress string `json:"radioMacAddress"`
	RadioNumber     int    `json:"radioNumber"`
	WLANName        string `json:"wlanName"`
	DeviceName      string `json:"deviceName"`
	SerialNumber    string `json:"serialNumber"`
	SiteID          string `json:"siteId"`
	SiteName        string `json:"siteName"`
	ClusterID       string `json:"clusterId"`
	ClientCount     int    `json:"clientCount"`
}

// Swarm is a "GET /network-monitoring/v1/swarms"
// list item (IAP mesh cluster).
type Swarm struct {
	ID                    string `json:"id"`
	ClusterID             string `json:"clusterId"`
	ClusterName           string `json:"clusterName"`
	ConductorDeviceName   string `json:"conductorDeviceName"`
	ConductorSerialNumber string `json:"conductorSerialNumber"`
	FirmwareVersion       string `json:"firmwareVersion"`
	IPv4                  string `json:"ipv4"`
	IPv6                  string `json:"ipv6"`
	PublicIPAddress       string `json:"publicIpAddress"`
	SiteID                string `json:"siteId"`
	SiteName              string `json:"siteName"`
}

// GatewayUplink is a "GET /gateways/{serial}/uplinks" list item.
type GatewayUplink struct {
	ID     string `json:"id"`
	Serial string `json:"_serial"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// GatewayVLAN is a "GET /gateways/{serial}/vlans" list item.
type GatewayVLAN struct {
	ID           string `json:"id"`
	Serial       string `json:"_serial"`
	Name         string `json:"name"`
	VLANID       int    `json:"vlanId"`
	VLANType     string `json:"vlanType"`
	Status       string `json:"status"`
	AdminStatus  string `json:"adminStatus"`
	IPv4         string `json:"ipv4"`
	IPv4MaskAddr string `json:"ipv4MaskAddr"`
	Interfaces   string `json:"interfaces"`
}

// GatewayPort is a "GET /gateways/{serial}/ports" list item.
type GatewayPort struct {
	ID            string `json:"id"`
	Serial        string `json:"_serial"`
	Name          string `json:"name"`
	PortNumber    string `json:"portNumber"`
	PortType      string `json:"portType"`
	ConnectorType string `json:"connectorType"`
	AdminState    string `json:"adminState"`
	OperState     string `json:"operState"`
	Health        string `json:"health"`
	Speed         string `json:"speed"`
	Duplex        string `json:"duplex"`
	MACAddress    string `json:"macAddress"`
	VLAN          string `json:"vlan"`
}

// ClientDevice is a "GET /network-monitoring/v1/clients" list item: one
// end-user device on the network (unified wired + wireless client)
type ClientDevice struct {
	ID                    string `json:"id"`
	MACAddress            string `json:"macAddress"`
	HostName              string `json:"hostName"`
	UserName              string `json:"userName"`
	ClientName            string `json:"clientName"`
	ClientCategory        string `json:"clientCategory"`
	ClientConnectionType  string `json:"clientConnectionType"`
	ClientFunction        string `json:"clientFunction"`
	ClientManufacturer    string `json:"clientManufacturer"`
	ClientOperatingSystem string `json:"clientOperatingSystem"`
	ClientVendor          string `json:"clientVendor"`
	AuthenticationType    string `json:"authenticationType"`
	ConnectedAt           string `json:"connectedAt"`
	LastSeenAt            string `json:"lastSeenAt"`
	ConnectedDeviceSerial string `json:"connectedDeviceSerial"`
	ConnectedDeviceType   string `json:"connectedDeviceType"`
	ConnectedTo           string `json:"connectedTo"`
	Port                  string `json:"port"`
	Status                string `json:"status"`
	SiteID                string `json:"siteId"`
	SiteName              string `json:"siteName"`
	VLANID                string `json:"vlanId"`
	VLANName              string `json:"vlanName"`
	WLANName              string `json:"wlanName"`
	WirelessBand          string `json:"wirelessBand"`
	IPv4                  string `json:"ipv4"`
	IPv6                  string `json:"ipv6"`
}

// Neighbor is a "GET /network-monitoring/v1/neighbours/{serial}"
// list item (LLDP/CDP-like adjacency).
type Neighbor struct {
	Serial       string `json:"_serial"`
	Health       string `json:"health"`
	LocalPort    string `json:"localPort"`
	Name         string `json:"name"`
	RemoteSerial string `json:"serial"`
	SiteID       string `json:"siteId"`
	SiteName     string `json:"siteName"`
	ToPort       string `json:"toPort"`
	Type         string `json:"type"`
}

// Site is a scope-management site object.
type Site struct {
	ID          string  `json:"id"`
	ScopeID     string  `json:"scopeId"`
	ScopeName   string  `json:"scopeName"`
	Address     string  `json:"address"`
	City        string  `json:"city"`
	State       string  `json:"state"`
	Country     string  `json:"country"`
	Zipcode     string  `json:"zipcode"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	DeviceCount int     `json:"deviceCount"`
	Timezone    struct {
		TimezoneID   string `json:"timezoneId"`
		TimezoneName string `json:"timezoneName"`
		RawOffset    int    `json:"rawOffset"`
	} `json:"timezone"`
}

// DeviceGroup is a "GET /network-config/v1/device-groups" list.
type DeviceGroup struct {
	ID          string `json:"id"`
	ScopeID     string `json:"scopeId"`
	ScopeName   string `json:"scopeName"`
	Description string `json:"description"`
	DeviceCount int    `json:"deviceCount"`
	IsIap8x     bool   `json:"isIap8x"`
	Type        string `json:"type"`
}

// ConfigHealthSummary is a per-device configuration compliance summary.
type ConfigHealthSummary struct {
	Serial              string `json:"serial"`
	Name                string `json:"name"`
	Model               string `json:"model"`
	Deployment          string `json:"deployment"`
	DeviceFunction      string `json:"deviceFunction"`
	DeviceGroupName     string `json:"deviceGroupName"`
	SiteName            string `json:"siteName"`
	ConfigStatus        string `json:"configStatus"`
	TopPriorityIssue    string `json:"topPriorityIssue"`
	RecommendedAction   string `json:"recommendedAction"`
	LastConfigTimestamp string `json:"lastConfigTimestamp"`
}

// ConfigHealthIssue is a per-device active configuration
// issue list (arrays may all be empty).
type ConfigHealthIssue struct {
	ConfigPullFailures []map[string]any `json:"configPullFailures"`
	ConfigPushFailures []map[string]any `json:"configPushFailures"`
	InvalidConfig      []map[string]any `json:"invalidConfig"`
	FilteredConfig     []map[string]any `json:"filteredConfig"`
}

// --- List methods ----------------------------------------------------

func (c *Client) unmarshalPage(raw []json.RawMessage, out any) error {
	data, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

// ListSwitches returns every switch in the account
// (optionally site-filtered by the caller).
func (c *Client) ListSwitches() ([]Switch, error) {
	out, _, err := c.ListSwitchesWithRaw()
	return out, err
}

// ListSwitchesWithRaw is ListSwitches but also returns each item's raw
// JSON body (same order), for --purpose audit --include-raw-body (see
// attachRawBodies in arubacentral.go): a lossless fallback for fields
// not yet OSIRIS JSON modeled on the Switch struct.
func (c *Client) ListSwitchesWithRaw() ([]Switch, []json.RawMessage, error) {
	raw, err := c.paginate("/network-monitoring/v1/switches", nil, true)
	if err != nil {
		return nil, nil, err
	}
	var out []Switch
	if err := c.unmarshalPage(raw, &out); err != nil {
		return nil, nil, err
	}
	return out, raw, nil
}

// ListSwitchInterfaces returns interfaces for given switch/stack serial.
func (c *Client) ListSwitchInterfaces(serial string) ([]SwitchInterface, error) {
	raw, err := c.paginate(fmt.Sprintf("/network-monitoring/v1/switches/%s/interfaces", serial), nil, false)
	if err != nil {
		return nil, err
	}
	var out []SwitchInterface
	return out, c.unmarshalPage(raw, &out)
}

// ListSwitchVLANs returns VLANs for a given switch/stack serial.
func (c *Client) ListSwitchVLANs(serial string) ([]SwitchVLAN, error) {
	raw, err := c.paginate(fmt.Sprintf("/network-monitoring/v1/switches/%s/vlans", serial), nil, false)
	if err != nil {
		return nil, err
	}
	var out []SwitchVLAN
	return out, c.unmarshalPage(raw, &out)
}

// ListSwitchHardware returns hardware health categories
// for a given switch/stack serial.
func (c *Client) ListSwitchHardware(serial string) ([]SwitchHardware, error) {
	raw, err := c.paginate(fmt.Sprintf("/network-monitoring/v1/switches/%s/hardware-categories", serial), nil, false)
	if err != nil {
		return nil, err
	}
	var out []SwitchHardware
	return out, c.unmarshalPage(raw, &out)
}

// ListSwitchLAG returns link-aggregation groups
// for a given switch/stack serial.
func (c *Client) ListSwitchLAG(serial string) ([]SwitchLAG, error) {
	raw, err := c.paginate(fmt.Sprintf("/network-monitoring/v1/switches/%s/lag", serial), nil, false)
	if err != nil {
		return nil, err
	}
	var out []SwitchLAG
	return out, c.unmarshalPage(raw, &out)
}

// GetStackMembers returns stack membership
// for a given stack ID or conductor serial.
func (c *Client) GetStackMembers(serial string) (*StackMembers, error) {
	var out StackMembers
	if err := c.getOne(fmt.Sprintf("/network-monitoring/v1/stack/%s/members", serial), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetSwitchVSX returns VSX peering details for a given switch serial.
func (c *Client) GetSwitchVSX(serial string) (*SwitchVSX, error) {
	var out SwitchVSX
	if err := c.getOne(fmt.Sprintf("/network-monitoring/v1/switches/%s/vsx", serial), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListAPs returns every access point in the account, online and offline,
// optionally server-side scoped to siteIDs (nil/empty for every site).
func (c *Client) ListAPs(siteIDs []string) ([]AccessPoint, error) {
	out, _, err := c.ListAPsWithRaw(siteIDs)
	return out, err
}

// ListAPsWithRaw is ListAPs but also returns each item's raw JSON body
// (same order); see ListSwitchesWithRaw.
func (c *Client) ListAPsWithRaw(siteIDs []string) ([]AccessPoint, []json.RawMessage, error) {
	filter := "status in ('ONLINE','OFFLINE')"
	if len(siteIDs) > 0 {
		quoted := make([]string, len(siteIDs))
		for i, id := range siteIDs {
			quoted[i] = "'" + id + "'"
		}
		filter += " and siteId in (" + strings.Join(quoted, ",") + ")"
	}
	query := url.Values{"filter": {filter}}
	raw, err := c.paginate("/network-monitoring/v1/aps", query, true)
	if err != nil {
		return nil, nil, err
	}
	var out []AccessPoint
	if err := c.unmarshalPage(raw, &out); err != nil {
		return nil, nil, err
	}
	return out, raw, nil
}

// ListRadios returns every radio across all access points.
func (c *Client) ListRadios() ([]Radio, error) {
	raw, err := c.paginate("/network-monitoring/v1/radios", nil, true)
	if err != nil {
		return nil, err
	}
	var out []Radio
	return out, c.unmarshalPage(raw, &out)
}

// ListWLANs returns every configured WLAN (SSID) in the account.
func (c *Client) ListWLANs() ([]APWLAN, error) {
	raw, err := c.paginate("/network-monitoring/v1/wlans", nil, true)
	if err != nil {
		return nil, err
	}
	var out []APWLAN
	return out, c.unmarshalPage(raw, &out)
}

// ListBSSIDs returns every broadcast BSSID across all radios.
func (c *Client) ListBSSIDs() ([]BSSID, error) {
	raw, err := c.paginate("/network-monitoring/v1/bssids", nil, true)
	if err != nil {
		return nil, err
	}
	var out []BSSID
	return out, c.unmarshalPage(raw, &out)
}

// ListSwarms returns every IAP mesh swarm (cluster) in the account.
func (c *Client) ListSwarms() ([]Swarm, error) {
	raw, err := c.paginate("/network-monitoring/v1/swarms", nil, true)
	if err != nil {
		return nil, err
	}
	var out []Swarm
	return out, c.unmarshalPage(raw, &out)
}

// ListAPWLANs returns the WLANs broadcast by a specific access point.
func (c *Client) ListAPWLANs(serial string) ([]APWLAN, error) {
	raw, err := c.paginate(fmt.Sprintf("/network-monitoring/v1/aps/%s/wlans", serial), nil, false)
	if err != nil {
		return nil, err
	}
	var out []APWLAN
	return out, c.unmarshalPage(raw, &out)
}

// ListAPPorts returns the wired ports of a specific access point.
func (c *Client) ListAPPorts(serial string) ([]APPort, error) {
	raw, err := c.paginate(fmt.Sprintf("/network-monitoring/v1/aps/%s/ports", serial), nil, false)
	if err != nil {
		return nil, err
	}
	var out []APPort
	return out, c.unmarshalPage(raw, &out)
}

// ListAPTunnels returns the tunnels (e.g. to a mobility conductor
// or gateway) of a specific access point.
func (c *Client) ListAPTunnels(serial string) ([]APTunnel, error) {
	// Cursor-paginated per the API reference (limit 0-1000), not offset.
	raw, err := c.paginate(fmt.Sprintf("/network-monitoring/v1/aps/%s/tunnels", serial), nil, true)
	if err != nil {
		return nil, err
	}
	var out []APTunnel
	return out, c.unmarshalPage(raw, &out)
}

// ListGateways returns every gateway in the account.
func (c *Client) ListGateways() ([]Gateway, error) {
	out, _, err := c.ListGatewaysWithRaw()
	return out, err
}

// ListGatewaysWithRaw is ListGateways but also returns each item's raw
// JSON body (same order); see ListSwitchesWithRaw.
func (c *Client) ListGatewaysWithRaw() ([]Gateway, []json.RawMessage, error) {
	raw, err := c.paginate("/network-monitoring/v1/gateways", nil, true)
	if err != nil {
		return nil, nil, err
	}
	var out []Gateway
	if err := c.unmarshalPage(raw, &out); err != nil {
		return nil, nil, err
	}
	return out, raw, nil
}

// ListGatewayUplinks returns WAN uplinks for a specific gateway.
func (c *Client) ListGatewayUplinks(serial string) ([]GatewayUplink, error) {
	raw, err := c.paginate(fmt.Sprintf("/network-monitoring/v1/gateways/%s/uplinks", serial), nil, false)
	if err != nil {
		return nil, err
	}
	var out []GatewayUplink
	return out, c.unmarshalPage(raw, &out)
}

// ListGatewayVLANs returns VLANs configured on a specific gateway.
func (c *Client) ListGatewayVLANs(serial string) ([]GatewayVLAN, error) {
	raw, err := c.paginateWithLimit(fmt.Sprintf("/network-monitoring/v1/gateways/%s/vlans", serial), nil, true, gatewayVLANPageLimit)
	if err != nil {
		return nil, err
	}
	var out []GatewayVLAN
	return out, c.unmarshalPage(raw, &out)
}

// ListGatewayPorts returns physical ports on a specific gateway.
func (c *Client) ListGatewayPorts(serial string) ([]GatewayPort, error) {
	// Cursor-paginated per the API reference (limit 0-1000) not offset.
	raw, err := c.paginate(fmt.Sprintf("/network-monitoring/v1/gateways/%s/ports", serial), nil, true)
	if err != nil {
		return nil, err
	}
	var out []GatewayPort
	return out, c.unmarshalPage(raw, &out)
}

// ListClients returns unified (wired + wireless) client in the account.
func (c *Client) ListClients() ([]ClientDevice, error) {
	raw, err := c.paginate("/network-monitoring/v1/clients", nil, true)
	if err != nil {
		return nil, err
	}
	var out []ClientDevice
	return out, c.unmarshalPage(raw, &out)
}

// ListNeighbors returns LLDP/CDP-like
// neighbor adjacencies for a device serial.
func (c *Client) ListNeighbors(serial string) ([]Neighbor, error) {
	body, err := c.get(fmt.Sprintf("/network-monitoring/v1/neighbours/%s", serial), nil)
	if err != nil {
		return nil, err
	}
	return parseNeighborsResponse(body)
}

func parseNeighborsResponse(body []byte) ([]Neighbor, error) {
	var arr []Neighbor
	if err := json.Unmarshal(body, &arr); err == nil {
		return arr, nil
	}

	var env listEnvelope
	if err := json.Unmarshal(body, &env); err == nil {
		itemsRaw := env.Items
		if len(itemsRaw) == 0 && env.Response != nil {
			itemsRaw = env.Response.Items
		}
		if len(itemsRaw) > 0 {
			var out []Neighbor
			if err := json.Unmarshal(itemsRaw, &out); err == nil {
				return out, nil
			}
		}
	}

	var byKey map[string]json.RawMessage
	if err := json.Unmarshal(body, &byKey); err == nil {
		out := make([]Neighbor, 0, len(byKey))
		for _, raw := range byKey {
			var n Neighbor
			if err := json.Unmarshal(raw, &n); err == nil {
				out = append(out, n)
			}
		}
		return out, nil
	}

	return nil, fmt.Errorf("parsing neighbors response: unrecognized shape: %s", truncateBody(body))
}

// ListSites returns every site (scope-management object) in account.
func (c *Client) ListSites() ([]Site, error) {
	raw, err := c.paginateWithLimit("/network-config/v1/sites", nil, false, configPageLimit)
	if err != nil {
		return nil, err
	}
	var out []Site
	return out, c.unmarshalPage(raw, &out)
}

// SiteHealth is a "GET /network-monitoring/v1/sites-health" list item
// (the "List of sites with health overview" endpoint).
type SiteHealth struct {
	SiteID       string `json:"siteId"`
	SiteName     string `json:"siteName"`
	SiteHealth   string `json:"siteHealth"`
	DeviceHealth string `json:"deviceHealth"`
	ClientHealth string `json:"clientHealth"`
}

// ListSitesHealth returns the health overview for every site in account.
func (c *Client) ListSitesHealth() ([]SiteHealth, error) {
	raw, err := c.paginate("/network-monitoring/v1/sites-health", nil, false)
	if err != nil {
		return nil, err
	}
	var out []SiteHealth
	return out, c.unmarshalPage(raw, &out)
}

// SiteDeviceHealth is a "GET /network-monitoring/v1/sites-device-health"
// list item (the "List of sites with device health" endpoint): a
// per-site breakdown of health by device category, complementing
// SiteHealth's overall/device/client rollup.
type SiteDeviceHealth struct {
	SiteID        string `json:"siteId"`
	SiteName      string `json:"siteName"`
	DeviceHealth  string `json:"deviceHealth"`
	APHealth      string `json:"apHealth"`
	SwitchHealth  string `json:"switchHealth"`
	GatewayHealth string `json:"gatewayHealth"`
	BridgeHealth  string `json:"bridgeHealth"`
}

// ListSitesDeviceHealth returns the per-device-category health
// breakdown for every site in the account.
func (c *Client) ListSitesDeviceHealth() ([]SiteDeviceHealth, error) {
	raw, err := c.paginate("/network-monitoring/v1/sites-device-health", nil, false)
	if err != nil {
		return nil, err
	}
	var out []SiteDeviceHealth
	return out, c.unmarshalPage(raw, &out)
}

// GetUnmanagedDevice returns Central's detail record for an unmanaged
// device detected via LLDP/CDP (see the "Unmanaged" neighbor type in
// ListNeighbors/TransformNeighbors), identified by its
// MAC address and site.
func (c *Client) GetUnmanagedDevice(macAddress, siteID string) (map[string]any, error) {
	body, err := c.get(fmt.Sprintf("/network-monitoring/v1/unmanaged-device/%s", macAddress), url.Values{"site-id": {siteID}})
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("parsing unmanaged device response: %w", err)
	}
	return out, nil
}

// ListIsolatedDevices returns Central's isolated-device report for site.
func (c *Client) ListIsolatedDevices(siteID string) ([]map[string]any, error) {
	body, err := c.get(fmt.Sprintf("/network-monitoring/v1/isolated-devices/%s", siteID), nil)
	if err != nil {
		return nil, err
	}
	var wrapper struct {
		IsolatedDevices []map[string]any `json:"isolatedDevices"`
	}
	if err := json.Unmarshal(body, &wrapper); err != nil {
		return nil, fmt.Errorf("parsing isolated devices response: %w", err)
	}
	return wrapper.IsolatedDevices, nil
}

// ListDeviceGroups returns every device group
// (config-managed device collection) in the account.
func (c *Client) ListDeviceGroups() ([]DeviceGroup, error) {
	raw, err := c.paginateWithLimit("/network-config/v1/device-groups", nil, false, configPageLimit)
	if err != nil {
		return nil, err
	}
	var out []DeviceGroup
	return out, c.unmarshalPage(raw, &out)
}

// GetConfigHealthSummary returns the configuration compliance summary
// for a device serial.
func (c *Client) GetConfigHealthSummary(serial string) (*ConfigHealthSummary, error) {
	var out ConfigHealthSummary
	if err := c.getOne(fmt.Sprintf("/network-monitoring/v1/config-health/%s/summary", serial), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetConfigHealthIssues returns the active configuration issues
// for a device serial.
func (c *Client) GetConfigHealthIssues(serial string) (*ConfigHealthIssue, error) {
	var out ConfigHealthIssue
	if err := c.getOne(fmt.Sprintf("/network-monitoring/v1/config-health/%s/issues", serial), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
