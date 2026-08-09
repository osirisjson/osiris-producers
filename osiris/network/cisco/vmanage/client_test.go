// client_test.go - Tests for the vManage REST API client: session+XSRF
// login, and both response envelope shapes GetDevices/ListTenants must
// handle.
//
// OSIRIS JSON Producer for Cisco SD-WAN Manager (vManage) introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package vmanage

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"go.osirisjson.org/producers/pkg/testharness"
)

func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return testharness.NewTestContext(t).Logger
}

func newTestClient(t *testing.T, ts *httptest.Server) *Client {
	t.Helper()
	return &Client{
		baseURL:    ts.URL,
		httpClient: ts.Client(),
		logger:     testLogger(t),
	}
}

func TestLogin_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/j_security_check":
			w.WriteHeader(http.StatusOK)
		case "/dataservice/client/token":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"token":"test-xsrf-token"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	c := newTestClient(t, ts)
	if err := c.Login("user", "changeme"); err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	if c.xsrfToken != "test-xsrf-token" {
		t.Errorf("xsrfToken = %q, want %q", c.xsrfToken, "test-xsrf-token")
	}
}

func TestLogin_InvalidCredentials(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/j_security_check" {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, "<html><body>login page</body></html>")
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	c := newTestClient(t, ts)
	if err := c.Login("user", "wrong"); err == nil {
		t.Fatal("expected Login to fail on re-served login page")
	}
}

func TestGetDevices_EnvelopeShape(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[{"deviceId":"192.0.2.10","system-ip":"192.0.2.10","host-name":"TEST-VMANAGE1","uuid":"11111111-1111-1111-1111-111111111111","site-id":"100","personality":"vmanage"}]}`)
	}))
	defer ts.Close()

	c := newTestClient(t, ts)
	devices, err := c.GetDevices()
	if err != nil {
		t.Fatalf("GetDevices failed: %v", err)
	}
	if len(devices) != 1 || devices[0].HostName != "TEST-VMANAGE1" {
		t.Fatalf("unexpected devices: %+v", devices)
	}
}

func TestGetDevices_BareArrayShape(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"deviceId":"192.0.2.20","system-ip":"192.0.2.20","host-name":"TEST-VEDGE1","uuid":"22222222-2222-2222-2222-222222222222","site-id":"200","personality":"vedge"}]`)
	}))
	defer ts.Close()

	c := newTestClient(t, ts)
	devices, err := c.GetDevices()
	if err != nil {
		t.Fatalf("GetDevices failed: %v", err)
	}
	if len(devices) != 1 || devices[0].HostName != "TEST-VEDGE1" {
		t.Fatalf("unexpected devices: %+v", devices)
	}
}

func TestGetDevices_HealthAndInventoryFields(t *testing.T) {
	// Field values taken verbatim from the vManage OpenAPI spec's own
	// documented "Devices List" example for GET /dataservice/device.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[{"deviceId":"192.0.2.20","system-ip":"192.0.2.20","host-name":"vm200","personality":"vmanage","board-serial":"12345789","state":"green","state_description":"All daemons up","uptime-date":1634626320000,"lastupdated":1634627015139,"connectedVManages":["192.0.2.20"]}]}`)
	}))
	defer ts.Close()

	c := newTestClient(t, ts)
	devices, err := c.GetDevices()
	if err != nil {
		t.Fatalf("GetDevices failed: %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(devices))
	}
	d := devices[0]
	if d.BoardSerial != "12345789" {
		t.Errorf("BoardSerial = %q, want %q", d.BoardSerial, "12345789")
	}
	if d.HealthState != "green" {
		t.Errorf("HealthState = %q, want %q", d.HealthState, "green")
	}
	if d.StateDescription != "All daemons up" {
		t.Errorf("StateDescription = %q, want %q", d.StateDescription, "All daemons up")
	}
	if d.UptimeDate != 1634626320000 {
		t.Errorf("UptimeDate = %d, want %d", d.UptimeDate, 1634626320000)
	}
	if d.LastUpdated != 1634627015139 {
		t.Errorf("LastUpdated = %d, want %d", d.LastUpdated, 1634627015139)
	}
	if len(d.ConnectedVManages) != 1 || d.ConnectedVManages[0] != "192.0.2.20" {
		t.Errorf("ConnectedVManages = %v, want [192.0.2.20]", d.ConnectedVManages)
	}
}

func TestGetDevices_EmptyEnvelope(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[]}`)
	}))
	defer ts.Close()

	c := newTestClient(t, ts)
	devices, err := c.GetDevices()
	if err != nil {
		t.Fatalf("GetDevices failed: %v", err)
	}
	if len(devices) != 0 {
		t.Fatalf("expected 0 devices, got %d", len(devices))
	}
}

func TestListTenants_BareArray(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"name":"TEST-TENANT-1"},{"name":"TEST-TENANT-2"}]`)
	}))
	defer ts.Close()

	c := newTestClient(t, ts)
	tenants, err := c.ListTenants()
	if err != nil {
		t.Fatalf("ListTenants failed: %v", err)
	}
	if len(tenants) != 2 {
		t.Fatalf("expected 2 tenants, got %d", len(tenants))
	}
	if got := tenantLabel(tenants[0]); got != "TEST-TENANT-1" {
		t.Errorf("tenantLabel = %q, want %q", got, "TEST-TENANT-1")
	}
}

func TestGetSiteName_UndocumentedResponseShape(t *testing.T) {
	// The observed response shape for
	// GET /dataservice/topology/device/site/{siteId} the spec itself
	// documents no fields for this endpoint at all.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"site_name":"MXP","data":[{"device-id":"TST0000001","device-model":"C8200L-1N-4T","uuid":"C8200L-1N-4T-TEST","site_id":"123456789","host_name":"TEST-DEVICE1","system_ip":"192.0.2.10"}]}`)
	}))
	defer ts.Close()

	c := newTestClient(t, ts)
	name, err := c.GetSiteName("123456789")
	if err != nil {
		t.Fatalf("GetSiteName failed: %v", err)
	}
	if name != "MXP" {
		t.Errorf("GetSiteName = %q, want %q", name, "MXP")
	}
}

func TestGetSiteName_ThrottledBySiteNameRate(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"site_name":"MXP","data":[]}`)
	}))
	defer ts.Close()

	c := newTestClient(t, ts)
	c.siteNameMinInterval = rateToInterval(10) // 100ms.

	if _, err := c.GetSiteName("100"); err != nil {
		t.Fatalf("GetSiteName failed: %v", err)
	}

	start := time.Now()
	if _, err := c.GetSiteName("100"); err != nil {
		t.Fatalf("GetSiteName failed: %v", err)
	}
	// Loose bound to avoid timing flakiness in CI.
	if elapsed := time.Since(start); elapsed < 80*time.Millisecond {
		t.Errorf("second GetSiteName should be throttled to ~%v, took %v", c.siteNameMinInterval, elapsed)
	}
}

func TestRateToInterval_NonPositiveDefaultsToOne(t *testing.T) {
	for _, rate := range []int{0, -5} {
		if got := rateToInterval(rate); got != time.Second {
			t.Errorf("rateToInterval(%d) = %v, want %v", rate, got, time.Second)
		}
	}
}

func TestGetSiteName_HTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer ts.Close()

	c := newTestClient(t, ts)
	if _, err := c.GetSiteName("100"); err == nil {
		t.Fatal("expected GetSiteName to fail on HTTP 403")
	}
}

func TestGet_RetriesOn429ThenSucceeds(t *testing.T) {
	var attempts int64
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt64(&attempts, 1)
		if n <= 2 {
			// Retry-After: 0 keeps this test fast instead of waiting
			// out real exponential backoff.
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"site_name":"MXP","data":[]}`)
	}))
	defer ts.Close()

	c := newTestClient(t, ts)
	name, err := c.GetSiteName("100")
	if err != nil {
		t.Fatalf("GetSiteName failed: %v", err)
	}
	if name != "MXP" {
		t.Errorf("GetSiteName = %q, want %q", name, "MXP")
	}
	if got := atomic.LoadInt64(&attempts); got != 3 {
		t.Errorf("expected 3 attempts (2 x 429 + 1 success), got %d", got)
	}
}

func TestGet_GivesUpAfterMaxRetries(t *testing.T) {
	var attempts int64
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&attempts, 1)
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer ts.Close()

	c := newTestClient(t, ts)
	if _, err := c.GetSiteName("100"); err == nil {
		t.Fatal("expected GetSiteName to eventually give up and return an error")
	}
	if got := atomic.LoadInt64(&attempts); got != max429Retries+1 {
		t.Errorf("expected %d attempts (initial + %d retries), got %d", max429Retries+1, max429Retries, got)
	}
}

func TestRetryDelay_RespectsRetryAfterHeader(t *testing.T) {
	resp := &http.Response{Header: http.Header{"Retry-After": {"2"}}}
	if got := retryDelay(resp, 0); got != 2*time.Second {
		t.Errorf("retryDelay = %v, want 2s", got)
	}
}

func TestRetryDelay_ExponentialBackoffWithoutHeader(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	if got := retryDelay(resp, 0); got != baseRetryDelay {
		t.Errorf("retryDelay(attempt=0) = %v, want %v", got, baseRetryDelay)
	}
	if got := retryDelay(resp, 1); got != baseRetryDelay*2 {
		t.Errorf("retryDelay(attempt=1) = %v, want %v", got, baseRetryDelay*2)
	}
	if got := retryDelay(resp, 10); got != maxRetryDelay {
		t.Errorf("retryDelay(attempt=10) = %v, want capped at %v", got, maxRetryDelay)
	}
}

func TestGetDeviceInterfaces_EnvelopeShape(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("deviceId"); got != "192.0.2.10" {
			t.Errorf("deviceId query param = %q, want %q", got, "192.0.2.10")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[{"vdevice-host-name":"192.0.2.10","ifname":"GigabitEthernet1","af-type":"ipv4","ip-address":"10.0.1.10/24","hwaddr":"02:00:00:00:53:01","if-admin-status":"Up","if-oper-status":"Up","vpn-id":"0","port-type":"transport"}]}`)
	}))
	defer ts.Close()

	c := newTestClient(t, ts)
	ifaces, err := c.GetDeviceInterfaces("192.0.2.10")
	if err != nil {
		t.Fatalf("GetDeviceInterfaces failed: %v", err)
	}
	if len(ifaces) != 1 || ifaces[0].IfName != "GigabitEthernet1" {
		t.Fatalf("unexpected interfaces: %+v", ifaces)
	}
}

func TestGetDeviceInterfaces_TolerantOfNumericSpeedAndMtu(t *testing.T) {
	// speed-mbps/mtu can arrive as either a bare JSON number or a
	// quoted string (see flexString); mixed in the same response here
	// to confirm both representations are tolerated regardless of
	// which one a given field uses.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[{"ifname":"GigabitEthernet1","af-type":"ipv4","speed-mbps":1000,"mtu":"1500"},{"ifname":"GigabitEthernet2","af-type":"ipv4","speed-mbps":"1000","mtu":1500}]}`)
	}))
	defer ts.Close()

	c := newTestClient(t, ts)
	ifaces, err := c.GetDeviceInterfaces("192.0.2.10")
	if err != nil {
		t.Fatalf("GetDeviceInterfaces failed: %v", err)
	}
	if len(ifaces) != 2 {
		t.Fatalf("expected 2 interfaces, got %d: %+v", len(ifaces), ifaces)
	}
	if ifaces[0].SpeedMbps != "1000" || ifaces[0].Mtu != "1500" {
		t.Errorf("ifaces[0] speed-mbps/mtu = %q/%q, want 1000/1500", ifaces[0].SpeedMbps, ifaces[0].Mtu)
	}
	if ifaces[1].SpeedMbps != "1000" || ifaces[1].Mtu != "1500" {
		t.Errorf("ifaces[1] speed-mbps/mtu = %q/%q, want 1000/1500", ifaces[1].SpeedMbps, ifaces[1].Mtu)
	}
}

func TestGetWANInterfaces_EnvelopeShape(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[{"vdevice-host-name":"192.0.2.10","interface":"GigabitEthernet1","color":"lte","private-ip":"10.0.1.10","public-ip":"203.0.113.10","nat-type":"E","operation-state":"up"}]}`)
	}))
	defer ts.Close()

	c := newTestClient(t, ts)
	wanIfaces, err := c.GetWANInterfaces("192.0.2.10")
	if err != nil {
		t.Fatalf("GetWANInterfaces failed: %v", err)
	}
	if len(wanIfaces) != 1 || wanIfaces[0].PublicIP != "203.0.113.10" {
		t.Fatalf("unexpected WAN interfaces: %+v", wanIfaces)
	}
}

func TestGetSiteTopologyMonitor_EnvelopeShape(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[{"device-id":"TST0000001","device-health":"green","circuits":[{"color":"lte","system_ip":"10.0.1.10","circuit-health":"green","tunnels":[{"name":"10.0.1.10:lte-10.0.1.11:lte","health":"green","state":"Up","vqoe_score":9}]}]}]}`)
	}))
	defer ts.Close()

	c := newTestClient(t, ts)
	devices, err := c.GetSiteTopologyMonitor("100")
	if err != nil {
		t.Fatalf("GetSiteTopologyMonitor failed: %v", err)
	}
	if len(devices) != 1 || len(devices[0].Circuits) != 1 || len(devices[0].Circuits[0].Tunnels) != 1 {
		t.Fatalf("unexpected site topology: %+v", devices)
	}
	if devices[0].Circuits[0].Tunnels[0].Name != "10.0.1.10:lte-10.0.1.11:lte" {
		t.Errorf("tunnel name = %q", devices[0].Circuits[0].Tunnels[0].Name)
	}
}

func TestGetOMPLinks_QueriesBothStatesAndMerges(t *testing.T) {
	// The endpoint's "state" query parameter is required but its
	// accepted values are undocumented in the vManage OpenAPI spec (no
	// enum, only "up"/"down" ever appear in worked examples)
	// GetOMPLinks queries both explicitly rather than an unverified
	// "all" and merges the results.
	var seenStates []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state := r.URL.Query().Get("state")
		seenStates = append(seenStates, state)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"data":[{"state":%q,"adeviceId":"10.0.1.10","bdeviceId":"10.0.1.11","asystem-ip":"10.0.1.10","bsystem-ip":"10.0.1.11","asite-id":"100","bsite-id":"100","ahost-name":"TEST-VEDGE1","bhost-name":"TEST-VEDGE2","apersonality":"vedge","bpersonality":"vedge"}]}`, state)
	}))
	defer ts.Close()

	c := newTestClient(t, ts)
	links, err := c.GetOMPLinks()
	if err != nil {
		t.Fatalf("GetOMPLinks failed: %v", err)
	}
	if len(links) != 2 {
		t.Fatalf("expected 2 links (one per queried state), got %d: %+v", len(links), links)
	}
	if len(seenStates) != 2 || seenStates[0] != "up" || seenStates[1] != "down" {
		t.Errorf("state query params = %v, want [up down]", seenStates)
	}
}

func TestGetOMPPeers_EnvelopeShape(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[{"vdevice-host-name":"10.0.1.10","peer":"10.0.1.1","type":"vsmart","state":"up"}]}`)
	}))
	defer ts.Close()

	c := newTestClient(t, ts)
	peers, err := c.GetOMPPeers("10.0.1.10")
	if err != nil {
		t.Fatalf("GetOMPPeers failed: %v", err)
	}
	if len(peers) != 1 || peers[0].Peer != "10.0.1.1" {
		t.Fatalf("unexpected OMP peers: %+v", peers)
	}
}

func TestGetDevices_SendsXSRFHeader(t *testing.T) {
	var gotHeader string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-XSRF-TOKEN")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[]`)
	}))
	defer ts.Close()

	c := newTestClient(t, ts)
	c.xsrfToken = "test-xsrf-token"
	if _, err := c.GetDevices(); err != nil {
		t.Fatalf("GetDevices failed: %v", err)
	}
	if gotHeader != "test-xsrf-token" {
		t.Errorf("X-XSRF-TOKEN header = %q, want %q", gotHeader, "test-xsrf-token")
	}
}
