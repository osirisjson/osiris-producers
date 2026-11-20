// collect_test.go - Tests for the collection stage, the coverage record
// and the run outcome contract of the Cisco Catalyst SD-WAN Manager
// OSIRIS JSON Producer, driven through a fake SD-WAN managerAPI.
//
// OSIRIS JSON Producer for Cisco Catalyst SD-WAN Manager introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification
package catalystsdwanmanager

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"go.osirisjson.org/producers/pkg/sdk"
)

// fakeAPI is an in-memory managerAPI. Per-device maps are keyed by
// system-ip; a key in the matching err map makes that call fail.
type fakeAPI struct {
	devices    []Device
	devicesErr error
	linksErr   error
	names      map[string]string

	ifaces    map[string][]Interface
	ifacesErr map[string]error
	wan       map[string][]WANInterface
	peers     map[string][]OMPPeer
	peersErr  map[string]error
	topo      map[string][]SiteTopologyDevice
	topoErr   map[string]error

	mu    sync.Mutex // guards calls, which concurrent peer lookups append to.
	calls []string
}

func (f *fakeAPI) GetDevicesWithRaw() ([]Device, []json.RawMessage, error) {
	f.calls = append(f.calls, "device")
	raws := make([]json.RawMessage, len(f.devices))
	for i := range raws {
		raws[i] = json.RawMessage(`{}`)
	}
	return f.devices, raws, f.devicesErr
}

func (f *fakeAPI) GetOMPLinks() ([]OMPLink, error) {
	f.calls = append(f.calls, "omp_links")
	return nil, f.linksErr
}

func (f *fakeAPI) GetSiteName(siteID string) (string, error) {
	f.calls = append(f.calls, "site_name:"+siteID)
	return f.names[siteID], nil
}

func (f *fakeAPI) GetDeviceInterfacesWithRaw(ip string) ([]Interface, []json.RawMessage, error) {
	f.calls = append(f.calls, "device_interface:"+ip)
	return f.ifaces[ip], nil, f.ifacesErr[ip]
}

func (f *fakeAPI) GetWANInterfacesWithRaw(ip string) ([]WANInterface, []json.RawMessage, error) {
	f.calls = append(f.calls, "wan_interface:"+ip)
	return f.wan[ip], nil, nil
}

func (f *fakeAPI) GetOMPPeersWithRaw(ip string) ([]OMPPeer, []json.RawMessage, error) {
	f.calls = append(f.calls, "omp_peers:"+ip)
	return f.peers[ip], nil, f.peersErr[ip]
}

func (f *fakeAPI) GetSiteTopologyMonitorWithRaw(siteID string) ([]SiteTopologyDevice, []json.RawMessage, error) {
	f.calls = append(f.calls, "site_topology:"+siteID)
	var raws []json.RawMessage
	for _, d := range f.topo[siteID] {
		b, _ := json.Marshal(d)
		raws = append(raws, b)
	}
	return f.topo[siteID], raws, f.topoErr[siteID]
}

func (f *fakeAPI) GetPeerWANInterfaces(ip string) ([]WANInterface, error) {
	f.mu.Lock()
	f.calls = append(f.calls, "wan_interface:"+ip)
	f.mu.Unlock()
	return f.wan[ip], nil
}

func (f *fakeAPI) called(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c == name {
			return true
		}
	}
	return false
}

// twoSiteFake is a fabric of one cEdge in each of sites 100 and 200.
func twoSiteFake() *fakeAPI {
	return &fakeAPI{
		devices: []Device{
			{DeviceID: "192.0.2.11", SystemIP: "192.0.2.11", HostName: "LAB-RTR01", UUID: "TST-UUID-0001", SiteID: "100", Personality: "vedge", BoardSerial: "TST0000001"},
			{DeviceID: "192.0.2.12", SystemIP: "192.0.2.12", HostName: "LAB-RTR02", UUID: "TST-UUID-0002", SiteID: "200", Personality: "vedge", BoardSerial: "TST0000002"},
		},
		names: map[string]string{"100": "BRANCH-00", "200": "OFFICE-00"},
		ifaces: map[string][]Interface{
			"192.0.2.11": {{IfName: "ge0/0", AFType: "ipv4", VPNID: "0", IfAdminStatus: "Up", IfOperStatus: "Up"}},
			"192.0.2.12": {{IfName: "ge0/0", AFType: "ipv4", VPNID: "0", IfAdminStatus: "Up", IfOperStatus: "Up"}},
		},
	}
}

func testConfig(t *testing.T) *Config {
	t.Helper()
	return &Config{
		Host: "lab-manager01.example.com", OutputDir: t.TempDir(),
		SafeFailureMode: sdk.FailClosed, Purpose: "documentation",
		AllSites: true, SiteNameRateLimit: 1000,
	}
}

var fixedRun = time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

func denied(op string) error {
	return &apiError{Op: op, Status: http.StatusForbidden, Category: categoryDenied}
}

func TestCollect_InterfaceFailureDoesNotSkipIndependentOperations(t *testing.T) {
	f := twoSiteFake()
	f.ifacesErr = map[string]error{"192.0.2.11": denied("/dataservice/device/interface")}
	col, err := collect(f, testConfig(t), testLogger(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"wan_interface:192.0.2.11", "omp_peers:192.0.2.11", "site_topology:100"} {
		if !f.called(c) {
			t.Errorf("%s was not attempted after the interface listing failed", c)
		}
	}
	cov := col.siteCoverage(col.sites[0])
	if cov.complete() {
		t.Error("site 100 coverage must be incomplete")
	}
	got := cov.metadata()["coverage"].([]any)
	var iface map[string]any
	for _, e := range got {
		if m := e.(map[string]any); m["operation"] == opDeviceInterface {
			iface = m
		}
	}
	if iface["status"] != statusFailed || iface["categories"].(map[string]any)[categoryDenied] != 1 {
		t.Errorf("device_interface coverage = %v, want failed with one denied", iface)
	}
	if !col.siteCoverage(col.sites[1]).complete() {
		t.Error("site 200 coverage must be complete")
	}
}

func TestCollect_InventoryFailureAborts(t *testing.T) {
	f := twoSiteFake()
	f.devicesErr = denied("/dataservice/device")
	if _, err := collect(f, testConfig(t), testLogger(t)); err == nil {
		t.Fatal("expected an inventory failure to abort collection")
	}
	if f.called("omp_links") {
		t.Error("no further operation may run after the inventory failed")
	}
}

func TestCoverage_StatusesAndMerge(t *testing.T) {
	var c coverage
	c.record("a", 2, nil)
	c.record("b", 0, nil)
	c.record("c", 1, nil)
	c.record("c", 0, denied("/x"))
	c.record("d", 0, &apiError{Op: "/y", Category: categoryMalformed})
	c.skip("e", "unsited")
	want := map[string]string{"a": statusSucceeded, "b": statusEmpty, "c": statusPartial, "d": statusFailed, "e": statusSkipped}
	for op, st := range want {
		if got := c.ops[op].status(); got != st {
			t.Errorf("%s status = %s, want %s", op, got, st)
		}
	}
	if c.complete() {
		t.Error("coverage with failures reported complete")
	}
	m := merged(&coverage{}, &c)
	if m.ops["c"].attempts != 2 || m.ops["c"].categories[categoryDenied] != 1 {
		t.Errorf("merged c = %+v", m.ops["c"])
	}
	if gaps := c.gaps(); len(gaps) != 2 || !strings.HasPrefix(gaps[0], "c (partial: denied)") {
		t.Errorf("gaps = %v", gaps)
	}
}

func TestExport_PartialRunContract(t *testing.T) {
	for _, requireComplete := range []bool{false, true} {
		f := twoSiteFake()
		f.peersErr = map[string]error{"192.0.2.12": denied("/dataservice/device/omp/peers")}
		cfg := testConfig(t)
		cfg.RequireComplete = requireComplete
		err := export(f, cfg, testLogger(t), fixedRun)
		if requireComplete && err == nil {
			t.Error("--require-complete: expected an error for a partial document")
		}
		if !requireComplete && err != nil {
			t.Errorf("partial run without --require-complete: %v", err)
		}
		files := writtenFiles(t, cfg.OutputDir)
		if len(files) != 2 {
			t.Fatalf("require-complete=%v: %d documents written, want 2", requireComplete, len(files))
		}
		for _, p := range files {
			doc := readDoc(t, p)
			ext := doc["metadata"].(map[string]any)[extensionKey].(map[string]any)
			wantComplete := !strings.Contains(p, "OFFICE-00")
			if ext["complete"] != wantComplete {
				t.Errorf("%s complete = %v, want %v", filepath.Base(p), ext["complete"], wantComplete)
			}
			if ext["api_profile"] != apiProfileUnverified {
				t.Errorf("api_profile = %v", ext["api_profile"])
			}
		}
	}
}

func TestExport_SiteWriteFailureFailsTheRunAfterOtherSites(t *testing.T) {
	f := twoSiteFake()
	cfg := testConfig(t)
	// A file where site 100's directory should be makes its write fail.
	if err := os.WriteFile(filepath.Join(cfg.OutputDir, "BRANCH-00"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := export(f, cfg, testLogger(t), fixedRun)
	if err == nil || !strings.Contains(err.Error(), "1 of 2 site(s) not written") {
		t.Fatalf("err = %v, want one site not written", err)
	}
	if files := writtenFiles(t, cfg.OutputDir); len(files) != 1 || !strings.Contains(files[0], "OFFICE-00") {
		t.Errorf("written = %v, want only the OFFICE-00 document", files)
	}
}

func TestExport_OneTimestampPerRun(t *testing.T) {
	f := twoSiteFake()
	cfg := testConfig(t)
	if err := export(f, cfg, testLogger(t), fixedRun); err != nil {
		t.Fatal(err)
	}
	for _, p := range writtenFiles(t, cfg.OutputDir) {
		if !strings.Contains(filepath.Base(p), "2026-11-13T12-00-00Z") {
			t.Errorf("%s does not carry the run timestamp", p)
		}
		if ts := readDoc(t, p)["metadata"].(map[string]any)["timestamp"]; ts != "2026-11-13T12:00:00Z" {
			t.Errorf("metadata.timestamp = %v", ts)
		}
	}
}

func TestDecodeListResponse_DistinguishesMalformedFromEmpty(t *testing.T) {
	tests := []struct {
		body     string
		category string // "" = success.
		n        int
	}{
		{`{"data":[{"ifname":"ge0/0"}]}`, "", 1},
		{`[{"ifname":"ge0/0"}]`, "", 1},
		{`{"data":[]}`, "", 0},
		{`{"data":null}`, "", 0},
		{`[]`, "", 0},
		{``, categoryMalformed, 0},
		{`{}`, categoryMalformed, 0},
		{`{"error":{"message":"x"}}`, categoryServerError, 0},
		{`{"data":"x"}`, categoryMalformed, 0},
		{`<html>`, categoryMalformed, 0},
	}
	for _, tt := range tests {
		items, err := decodeListResponse[Interface]("/dataservice/device/interface", []byte(tt.body))
		if got := map[bool]string{true: failureCategory(err), false: ""}[err != nil]; got != tt.category {
			t.Errorf("%q: category = %q, want %q", tt.body, got, tt.category)
		}
		if len(items) != tt.n {
			t.Errorf("%q: %d items, want %d", tt.body, len(items), tt.n)
		}
	}
}

func TestDevice_TimestampsAcceptStringAndNumber(t *testing.T) {
	var devices []Device
	body := `[{"lastupdated":"1634627015139","uptime-date":1634626320000},{"lastupdated":"","uptime-date":null},{"lastupdated":"n/a"}]`
	if err := json.Unmarshal([]byte(body), &devices); err != nil {
		t.Fatalf("schema-shaped string timestamps must decode: %v", err)
	}
	if devices[0].LastUpdated != 1634627015139 || devices[0].UptimeDate != 1634626320000 {
		t.Errorf("decoded %+v", devices[0])
	}
	if devices[1].LastUpdated != 0 || devices[2].LastUpdated != 0 {
		t.Error("empty, null and non-numeric timestamps must decode to 0")
	}
}

func TestReadBounded_TooLarge(t *testing.T) {
	old := maxResponseBytes
	maxResponseBytes = 8
	defer func() { maxResponseBytes = old }()
	if _, err := readBounded(strings.NewReader("123456789")); !errors.Is(err, errResponseTooLarge) {
		t.Errorf("err = %v, want errResponseTooLarge", err)
	}
	if b, err := readBounded(strings.NewReader("12345678")); err != nil || len(b) != 8 {
		t.Errorf("at the limit: %q, %v", b, err)
	}
}

func TestRetryDelay_RetryAfterIsCapped(t *testing.T) {
	resp := &http.Response{Header: http.Header{"Retry-After": []string{"3600"}}}
	if d := retryDelay(resp, 0); d != maxRetryDelay {
		t.Errorf("retryDelay = %v, want capped %v", d, maxRetryDelay)
	}
}

func writtenFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".json") {
			out = append(out, p)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func readDoc(t *testing.T, p string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}
