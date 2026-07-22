// client_test.go - Unit tests for the Aruba Central API Gateway client.
// Covers cursor and offset pagination, token refresh-on-401 and error
// handling using httptest servers with canned, sanitized responses.
//
// OSIRIS JSON Producer for HPE Aruba Networking Central introduction:
// [OSIRIS-JSON-HPE-ARUBA-NETWORKING]: https://docs.osirisjson.org/osiris-producers/network/hpe-aruba-networking-central

package arubacentral

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func newTestClient(ts *httptest.Server, creds Credentials) *Client {
	return &Client{
		baseURL:     ts.URL,
		httpClient:  ts.Client(),
		logger:      testLogger(),
		creds:       creds,
		minInterval: 0,
	}
}

func TestListSwitches_CursorPagination(t *testing.T) {
	calls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if !strings.HasPrefix(r.URL.Path, "/network-monitoring/v1/switches") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-access-token" {
			t.Errorf("unexpected Authorization header: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("next") == "" {
			items := make([]map[string]any, pageLimit)
			for i := range items {
				items[i] = map[string]any{"serialNumber": "SERIAL-EXAMPLE-0000", "deviceName": "switch-example-01", "status": "Up"}
			}
			json.NewEncoder(w).Encode(map[string]any{"items": items, "next": "page-2-cursor"})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{{"serialNumber": "SERIAL-EXAMPLE-0001", "deviceName": "switch-example-02", "status": "Down"}},
		})
	}))
	defer ts.Close()

	c := newTestClient(ts, Credentials{AccessToken: "test-access-token"})
	switches, err := c.ListSwitches()
	if err != nil {
		t.Fatalf("ListSwitches failed: %v", err)
	}
	if calls != 2 {
		t.Errorf("expected 2 API calls for pagination, got %d", calls)
	}
	if len(switches) != pageLimit+1 {
		t.Fatalf("expected %d switches, got %d", pageLimit+1, len(switches))
	}
	if switches[pageLimit].SerialNumber != "SERIAL-EXAMPLE-0001" {
		t.Errorf("unexpected last switch serial: %q", switches[pageLimit].SerialNumber)
	}
}

// TestListSwitches_NumericStackMemberID guards against production crash:
// the API returns stackMemberId as a JSON number, not a string, and a
// json.Unmarshal type mismatch here previously failed the entire
// collection run (not just this one field).
func TestListSwitches_NumericStackMemberID(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{{"serialNumber": "SERIAL-EXAMPLE-0000", "stackMemberId": 2}},
		})
	}))
	defer ts.Close()

	c := newTestClient(ts, Credentials{AccessToken: "test-access-token"})
	switches, err := c.ListSwitches()
	if err != nil {
		t.Fatalf("ListSwitches failed: %v", err)
	}
	if len(switches) != 1 || switches[0].StackMemberID != 2 {
		t.Fatalf("expected one switch with StackMemberID 2, got %+v", switches)
	}
}

// TestListSwitches_FractionalTrendValues guard against production crash:
// switchTrends.systemTemperature (and cpuUtilization/memoryUtilization)
// can arrive as fractional numbers (e.g. 31.25), not integers.
func TestListSwitches_FractionalTrendValues(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{{
				"serialNumber": "SERIAL-EXAMPLE-0000",
				"switchTrends": []map[string]any{{
					"cpuUtilization":    12.5,
					"memoryUtilization": 40.75,
					"systemTemperature": 31.25,
				}},
			}},
		})
	}))
	defer ts.Close()

	c := newTestClient(ts, Credentials{AccessToken: "test-access-token"})
	switches, err := c.ListSwitches()
	if err != nil {
		t.Fatalf("ListSwitches failed: %v", err)
	}
	if len(switches) != 1 || len(switches[0].SwitchTrends) != 1 {
		t.Fatalf("expected 1 switch with 1 trend sample, got %+v", switches)
	}
	trend := switches[0].SwitchTrends[0]
	if trend.SystemTemperature != 31.25 {
		t.Errorf("expected SystemTemperature 31.25, got %v", trend.SystemTemperature)
	}
}

// TestListAPs_RequestsBothStatuses guards a bug found in API with a
// discussion open on Airheads portal https://airheads.hpe.com/discussion/new-central-api-get-a-list-of-access-points
// /aps returned only ONLINE devices when called with no filter leaving
// OFFLINE access points silently absent from the result entirely (not
// merely mis-reported).
func TestListAPs_RequestsBothStatuses(t *testing.T) {
	var gotFilter string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotFilter = r.URL.Query().Get("filter")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{
				{"serialNumber": "SERIAL-EXAMPLE-ONLINE", "status": "Up"},
				{"serialNumber": "SERIAL-EXAMPLE-OFFLINE", "status": "Down"},
			},
		})
	}))
	defer ts.Close()

	c := newTestClient(ts, Credentials{AccessToken: "test-access-token"})
	aps, err := c.ListAPs(nil)
	if err != nil {
		t.Fatalf("ListAPs failed: %v", err)
	}
	if gotFilter != "status in ('ONLINE','OFFLINE')" {
		t.Errorf("expected the request to filter for both statuses, got filter=%q", gotFilter)
	}
	if len(aps) != 2 {
		t.Fatalf("expected 2 access points (online and offline), got %d", len(aps))
	}
}

// TestListAPs_ScopesToSiteIDsWhenGiven guards a performance follow-up:
// every --site-scoped run fetched the entire account's access points
// regardless of --site, relying on client-side filtering to discard
// everything outside the requested site(s) wasteful and slow (an
// account of ove ~1000 devices for a single ~100-device site),
// and compounding under --all where every site's Collect() call
// re-fetched the whole account from scratch. ListAPsWithRaw now adds a
// server-side "siteId in (...)" clause when site IDs are known.
func TestListAPs_ScopesToSiteIDsWhenGiven(t *testing.T) {
	var gotFilter string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotFilter = r.URL.Query().Get("filter")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{}})
	}))
	defer ts.Close()

	c := newTestClient(ts, Credentials{AccessToken: "test-access-token"})
	if _, err := c.ListAPs([]string{"site-a-id"}); err != nil {
		t.Fatalf("ListAPs failed: %v", err)
	}
	if want := "status in ('ONLINE','OFFLINE') and siteId in ('site-a-id')"; gotFilter != want {
		t.Errorf("expected filter %q, got %q", want, gotFilter)
	}

	if _, err := c.ListAPs(nil); err != nil {
		t.Fatalf("ListAPs failed: %v", err)
	}
	if want := "status in ('ONLINE','OFFLINE')"; gotFilter != want {
		t.Errorf("expected no siteId clause when siteIDs is nil, got filter=%q", gotFilter)
	}
}

func TestListSwitchInterfaces_OffsetPagination(t *testing.T) {
	calls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offset := r.URL.Query().Get("offset")
		w.Header().Set("Content-Type", "application/json")
		if offset == "0" {
			items := make([]map[string]any, pageLimit)
			for i := range items {
				items[i] = map[string]any{"name": "1/1/1", "status": "Up"}
			}
			json.NewEncoder(w).Encode(map[string]any{"items": items, "total": pageLimit + 1})
			calls++
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{"name": "1/1/2", "status": "Down"}}, "total": pageLimit + 1})
		calls++
	}))
	defer ts.Close()

	c := newTestClient(ts, Credentials{AccessToken: "test-access-token"})
	ifaces, err := c.ListSwitchInterfaces("SERIAL-EXAMPLE-0000")
	if err != nil {
		t.Fatalf("ListSwitchInterfaces failed: %v", err)
	}
	if calls != 2 {
		t.Errorf("expected 2 API calls for pagination, got %d", calls)
	}
	if len(ifaces) != pageLimit+1 {
		t.Fatalf("expected %d interfaces, got %d", pageLimit+1, len(ifaces))
	}
}

func TestGet_ResponseWrapperEnvelope(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"response": map[string]any{
				"items": []map[string]any{{"name": "1/1/1"}},
				"count": 1,
			},
		})
	}))
	defer ts.Close()

	c := newTestClient(ts, Credentials{AccessToken: "test-access-token"})
	ifaces, err := c.ListSwitchInterfaces("SERIAL-EXAMPLE-0000")
	if err != nil {
		t.Fatalf("ListSwitchInterfaces failed: %v", err)
	}
	if len(ifaces) != 1 {
		t.Fatalf("expected 1 interface from response-wrapper envelope, got %d", len(ifaces))
	}
}

func TestGet_RefreshOn401(t *testing.T) {
	var currentToken = "expired-token"
	refreshCalls := 0

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth2/token" {
			refreshCalls++
			currentToken = "refreshed-token"
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "refreshed-token",
				"refresh_token": "new-refresh-token",
				"token_type":    "bearer",
				"expires_in":    7200,
			})
			return
		}

		if r.Header.Get("Authorization") != "Bearer refreshed-token" {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"message":"Unauthorized"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{"serialNumber": "SERIAL-EXAMPLE-0000"}}})
	}))
	defer ts.Close()

	c := newTestClient(ts, Credentials{AccessToken: currentToken, RefreshToken: "old-refresh-token", ClientID: "cid", ClientSecret: "csecret"})
	switches, err := c.ListSwitches()
	if err != nil {
		t.Fatalf("ListSwitches failed: %v", err)
	}
	if refreshCalls != 1 {
		t.Errorf("expected exactly 1 refresh call, got %d", refreshCalls)
	}
	if len(switches) != 1 {
		t.Fatalf("expected 1 switch after refresh+retry, got %d", len(switches))
	}
	if c.creds.AccessToken != "refreshed-token" {
		t.Errorf("client did not retain refreshed access token: %q", c.creds.AccessToken)
	}
}

func TestGet_RefreshOn401_PersonalAPIClientRemint(t *testing.T) {
	mintCalls := 0
	mintServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mintCalls++
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parsing mint request form: %v", err)
		}
		if r.FormValue("grant_type") != "client_credentials" || r.FormValue("client_id") != "personal-id" || r.FormValue("client_secret") != "personal-secret" {
			t.Fatalf("unexpected mint request form: %v", r.Form)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"access_token": "minted-token", "token_type": "bearer", "expires_in": 7200})
	}))
	defer mintServer.Close()

	original := greenLakeTokenURL
	greenLakeTokenURL = mintServer.URL
	defer func() { greenLakeTokenURL = original }()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer minted-token" {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"message":"Unauthorized"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{"serialNumber": "SERIAL-EXAMPLE-0000"}}})
	}))
	defer ts.Close()

	c := newTestClient(ts, Credentials{AccessToken: "expired-token", ClientID: "personal-id", ClientSecret: "personal-secret"})
	switches, err := c.ListSwitches()
	if err != nil {
		t.Fatalf("ListSwitches failed: %v", err)
	}
	if mintCalls != 1 {
		t.Errorf("expected exactly 1 re-mint call, got %d", mintCalls)
	}
	if len(switches) != 1 {
		t.Fatalf("expected 1 switch after re-mint+retry, got %d", len(switches))
	}
	if c.creds.AccessToken != "minted-token" {
		t.Errorf("client did not retain re-minted access token: %q", c.creds.AccessToken)
	}
}

func TestGet_RefreshFailsWhenPersonalAPIClientMintFails(t *testing.T) {
	mintServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid_client"}`))
	}))
	defer mintServer.Close()

	original := greenLakeTokenURL
	greenLakeTokenURL = mintServer.URL
	defer func() { greenLakeTokenURL = original }()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"Unauthorized"}`))
	}))
	defer ts.Close()

	c := newTestClient(ts, Credentials{AccessToken: "expired-token", ClientID: "personal-id", ClientSecret: "personal-secret"})
	if _, err := c.ListSwitches(); err == nil {
		t.Fatal("expected an error when the GreenLake re-mint request fails")
	}
}

func TestGet_RefreshFailsWithoutRefreshToken(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"Unauthorized"}`))
	}))
	defer ts.Close()

	c := newTestClient(ts, Credentials{AccessToken: "expired-token"})
	_, err := c.ListSwitches()
	if err == nil {
		t.Fatal("expected an error when refresh token is unavailable")
	}
}

func TestGet_HTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer ts.Close()

	c := newTestClient(ts, Credentials{AccessToken: "test-access-token"})
	_, err := c.ListSwitches()
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention 500: %v", err)
	}
}

func TestGetOne_BareObjectEndpoint(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/vsx") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"role":          "primary",
			"peerRole":      "secondary",
			"vsxPeerSerial": "SERIAL-EXAMPLE-0002",
		})
	}))
	defer ts.Close()

	c := newTestClient(ts, Credentials{AccessToken: "test-access-token"})
	vsx, err := c.GetSwitchVSX("SERIAL-EXAMPLE-0000")
	if err != nil {
		t.Fatalf("GetSwitchVSX failed: %v", err)
	}
	if vsx.VSXPeerSerial != "SERIAL-EXAMPLE-0002" {
		t.Errorf("unexpected peer serial: %q", vsx.VSXPeerSerial)
	}
}

// TestListSites_UsesConfigPageLimit guards against a production failure
func TestListSites_UsesConfigPageLimit(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("limit"); got != strconv.Itoa(configPageLimit) {
			t.Errorf("expected limit=%d, got %q", configPageLimit, got)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{"scopeName": "example-campus-1"}}})
	}))
	defer ts.Close()

	c := newTestClient(ts, Credentials{AccessToken: "test-access-token"})
	sites, err := c.ListSites()
	if err != nil {
		t.Fatalf("ListSites failed: %v", err)
	}
	if len(sites) != 1 {
		t.Fatalf("expected 1 site, got %d", len(sites))
	}
}

// TestListDeviceGroups_UsesConfigPageLimit mirrors
// TestListSites_UsesConfigPageLimit for the sibling
// /network-config/v1/device-groups endpoint.
func TestListDeviceGroups_UsesConfigPageLimit(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("limit"); got != strconv.Itoa(configPageLimit) {
			t.Errorf("expected limit=%d, got %q", configPageLimit, got)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{"scopeName": "example-devicegroup-1"}}})
	}))
	defer ts.Close()

	c := newTestClient(ts, Credentials{AccessToken: "test-access-token"})
	groups, err := c.ListDeviceGroups()
	if err != nil {
		t.Fatalf("ListDeviceGroups failed: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 device group, got %d", len(groups))
	}
}

func TestListSitesHealth(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{
			{"siteId": "site-a-id", "siteName": "site-a", "siteHealth": "Good", "deviceHealth": "Good", "clientHealth": "Fair"},
		}})
	}))
	defer ts.Close()

	c := newTestClient(ts, Credentials{AccessToken: "test-access-token"})
	health, err := c.ListSitesHealth()
	if err != nil {
		t.Fatalf("ListSitesHealth failed: %v", err)
	}
	if len(health) != 1 || health[0].SiteName != "site-a" || health[0].ClientHealth != "Fair" {
		t.Fatalf("unexpected result: %+v", health)
	}
}

func TestListSitesDeviceHealth(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{
			{"siteId": "site-a-id", "siteName": "site-a", "apHealth": "Fair", "switchHealth": "Poor", "gatewayHealth": "Good", "bridgeHealth": "Good"},
		}})
	}))
	defer ts.Close()

	c := newTestClient(ts, Credentials{AccessToken: "test-access-token"})
	health, err := c.ListSitesDeviceHealth()
	if err != nil {
		t.Fatalf("ListSitesDeviceHealth failed: %v", err)
	}
	if len(health) != 1 || health[0].SwitchHealth != "Poor" {
		t.Fatalf("unexpected result: %+v", health)
	}
}

func TestGetUnmanagedDevice(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/unmanaged-device/00:00:5E:00:53:00") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("site-id"); got != "123456789012" {
			t.Errorf("expected site-id query param, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"name": "example-unmanaged-switch", "vendor": "Example Corp"})
	}))
	defer ts.Close()

	c := newTestClient(ts, Credentials{AccessToken: "test-access-token"})
	detail, err := c.GetUnmanagedDevice("00:00:5E:00:53:00", "123456789012")
	if err != nil {
		t.Fatalf("GetUnmanagedDevice failed: %v", err)
	}
	if detail["vendor"] != "Example Corp" {
		t.Errorf("unexpected result: %+v", detail)
	}
}

func TestListIsolatedDevices(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id": "123456789012", "type": "site",
			"isolatedDevices": []map[string]any{{"macAddress": "00:00:5E:00:53:00"}},
		})
	}))
	defer ts.Close()

	c := newTestClient(ts, Credentials{AccessToken: "test-access-token"})
	devices, err := c.ListIsolatedDevices("123456789012")
	if err != nil {
		t.Fatalf("ListIsolatedDevices failed: %v", err)
	}
	if len(devices) != 1 || devices[0]["macAddress"] != "00:00:5E:00:53:00" {
		t.Fatalf("unexpected result: %+v", devices)
	}
}

// TestListNeighbors_ParsesBareArray covers one of the shapes
// parseNeighborsResponse tolerates: a bare JSON array.
func TestListNeighbors_ParsesBareArray(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{"type": "Switch", "serial": "SERIAL-EXAMPLE-0099", "localPort": "1/1/1", "toPort": "1/1/2", "health": "Good"},
		})
	}))
	defer ts.Close()

	c := newTestClient(ts, Credentials{AccessToken: "test-access-token"})
	neighbors, err := c.ListNeighbors("SERIAL-EXAMPLE-0001")
	if err != nil {
		t.Fatalf("ListNeighbors failed: %v", err)
	}
	if len(neighbors) != 1 || neighbors[0].RemoteSerial != "SERIAL-EXAMPLE-0099" {
		t.Fatalf("expected 1 neighbor with remote serial SERIAL-EXAMPLE-0099, got %+v", neighbors)
	}
}

// TestListNeighbors_ParsesFlatKeyedObject guards a production bug:
// the bare-array assumption above was disproved against a live test
// ("cannot unmarshal object into Go value of type []Neighbor") - the
// real response is some kind of JSON object.
func TestListNeighbors_ParsesFlatKeyedObject(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"SERIAL-EXAMPLE-0099": map[string]any{"type": "Switch", "serial": "SERIAL-EXAMPLE-0099", "localPort": "1/1/1", "toPort": "1/1/2", "health": "Good"},
		})
	}))
	defer ts.Close()

	c := newTestClient(ts, Credentials{AccessToken: "test-access-token"})
	neighbors, err := c.ListNeighbors("SERIAL-EXAMPLE-0001")
	if err != nil {
		t.Fatalf("ListNeighbors failed: %v", err)
	}
	if len(neighbors) != 1 || neighbors[0].RemoteSerial != "SERIAL-EXAMPLE-0099" {
		t.Fatalf("expected 1 neighbor with remote serial SERIAL-EXAMPLE-0099, got %+v", neighbors)
	}
}

// TestListNeighbors_ParsesItemsEnvelope covers the standard envelope
// shape used by most other list endpoints in this API, in case
// /neighbours turns out to follow it after all.
func TestListNeighbors_ParsesItemsEnvelope(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{
			{"type": "Switch", "serial": "SERIAL-EXAMPLE-0099", "localPort": "1/1/1", "toPort": "1/1/2", "health": "Good"},
		}})
	}))
	defer ts.Close()

	c := newTestClient(ts, Credentials{AccessToken: "test-access-token"})
	neighbors, err := c.ListNeighbors("SERIAL-EXAMPLE-0001")
	if err != nil {
		t.Fatalf("ListNeighbors failed: %v", err)
	}
	if len(neighbors) != 1 || neighbors[0].RemoteSerial != "SERIAL-EXAMPLE-0099" {
		t.Fatalf("expected 1 neighbor with remote serial SERIAL-EXAMPLE-0099, got %+v", neighbors)
	}
}
