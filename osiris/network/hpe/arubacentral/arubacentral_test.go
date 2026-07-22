// arubacentral_test.go - End-to-end Collect() test against a fake API
// Gateway server, exercising the full collection/wiring/build pipeline
// for one switch, one access point, one gateway and one client.
//
// OSIRIS JSON Producer for HPE Aruba Networking Central introduction:
// [OSIRIS-JSON-HPE-ARUBA-NETWORKING]: https://docs.osirisjson.org/osiris-producers/network/hpe-aruba-networking-central
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package arubacentral

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.osirisjson.org/producers/pkg/sdk"
	"go.osirisjson.org/producers/pkg/testharness"
)

// jsonHandler is a tiny path-prefix router returning canned list/object
// responses for every endpoint Producer.Collect touches.
func fakeArubaCentralServer(t *testing.T) *httptest.Server {
	t.Helper()

	items := func(v ...any) map[string]any { return map[string]any{"items": v} }

	routes := map[string]any{
		"/network-config/v1/sites": items(
			map[string]any{"scopeId": "1", "scopeName": "example-campus-1", "city": "Rome", "deviceCount": 3},
		),
		"/network-monitoring/v1/sites-health": items(
			map[string]any{"siteId": "1", "siteName": "example-campus-1", "siteHealth": "Good", "deviceHealth": "Good", "clientHealth": "Fair"},
		),
		"/network-monitoring/v1/sites-device-health": items(
			map[string]any{"siteId": "1", "siteName": "example-campus-1", "apHealth": "Good", "switchHealth": "Good", "gatewayHealth": "Good", "bridgeHealth": "Good"},
		),
		"/network-monitoring/v1/isolated-devices/1": map[string]any{"id": "1", "type": "site", "isolatedDevices": []any{}},
		"/network-config/v1/device-groups": items(
			map[string]any{"id": "example-group", "scopeId": "1", "deviceCount": 3},
		),
		"/network-monitoring/v1/switches": items(
			map[string]any{"serialNumber": "SERIAL-EXAMPLE-0001", "deviceName": "switch-example-01", "status": "Up", "siteName": "example-campus-1"},
		),
		"/network-monitoring/v1/switches/SERIAL-EXAMPLE-0001/interfaces": items(
			map[string]any{"name": "1/1/1", "status": "Up"},
		),
		"/network-monitoring/v1/switches/SERIAL-EXAMPLE-0001/vlans": items(
			map[string]any{"id": "10", "name": "example-users", "taggedPorts": []string{"1/1/1"}},
		),
		"/network-monitoring/v1/switches/SERIAL-EXAMPLE-0001/lag": items(
			map[string]any{"id": "1", "name": "lag1", "ports": []string{"1/1/1"}},
		),
		"/network-monitoring/v1/switches/SERIAL-EXAMPLE-0001/hardware-categories": items(
			map[string]any{"cpu": map[string]any{"health": "Good"}},
		),
		"/network-monitoring/v1/stack/SERIAL-EXAMPLE-0001/members": map[string]any{},
		"/network-monitoring/v1/switches/SERIAL-EXAMPLE-0001/vsx":  map[string]any{},
		"/network-monitoring/v1/config-health/SERIAL-EXAMPLE-0001/summary": map[string]any{
			"configStatus": "Up-to-date", "deviceGroupName": "example-group",
		},
		"/network-monitoring/v1/config-health/SERIAL-EXAMPLE-0001/issues": map[string]any{},
		// Unlike every other list endpoint here, /neighbours returns a
		// bare JSON array, not an {"items": [...]} envelope
		// so this is []any, not items(...).
		"/network-monitoring/v1/neighbours/SERIAL-EXAMPLE-0001": []any{
			map[string]any{"type": "Switch", "_serial": "SERIAL-EXAMPLE-0001", "serial": "SERIAL-EXAMPLE-0099", "localPort": "1/1/1", "toPort": "1/1/2", "health": "Good"},
		},

		"/network-monitoring/v1/aps": items(
			map[string]any{"serialNumber": "SERIAL-EXAMPLE-0010", "deviceName": "ap-example-01", "status": "Up", "siteName": "example-campus-1"},
		),
		"/network-monitoring/v1/wlans": items(
			map[string]any{"wlanName": "ssid-example-corp", "status": "Up"},
		),
		"/network-monitoring/v1/radios": items(
			map[string]any{"serialNumber": "SERIAL-EXAMPLE-0010", "radioNumber": 0, "status": "Up"},
		),
		"/network-monitoring/v1/bssids": items(
			map[string]any{"bssid": "00:00:5E:00:53:00", "serialNumber": "SERIAL-EXAMPLE-0010", "radioNumber": 0, "wlanName": "ssid-example-corp"},
		),
		"/network-monitoring/v1/swarms": items(),
		"/network-monitoring/v1/aps/SERIAL-EXAMPLE-0010/ports": items(
			map[string]any{"name": "eth0", "status": "Up"},
		),
		"/network-monitoring/v1/aps/SERIAL-EXAMPLE-0010/tunnels": items(
			map[string]any{"tunnelName": "tunnel1", "destinationName": "gateway-example-01", "status": "Up"},
		),
		"/network-monitoring/v1/aps/SERIAL-EXAMPLE-0010/wlans": items(
			map[string]any{"wlanName": "ssid-example-corp", "band": "5GHz", "status": "Up"},
		),
		"/network-monitoring/v1/config-health/SERIAL-EXAMPLE-0010/summary": map[string]any{"configStatus": "Up-to-date"},
		"/network-monitoring/v1/config-health/SERIAL-EXAMPLE-0010/issues":  map[string]any{},
		"/network-monitoring/v1/neighbours/SERIAL-EXAMPLE-0010":            []any{},

		"/network-monitoring/v1/gateways": items(
			map[string]any{"serialNumber": "SERIAL-EXAMPLE-0020", "deviceName": "gateway-example-01", "status": "Up", "siteName": "example-campus-1"},
		),
		"/network-monitoring/v1/gateways/SERIAL-EXAMPLE-0020/ports": items(
			map[string]any{"name": "wan1", "health": "Good"},
		),
		"/network-monitoring/v1/gateways/SERIAL-EXAMPLE-0020/vlans": items(
			map[string]any{"vlanId": 20, "name": "example-guest"},
		),
		"/network-monitoring/v1/gateways/SERIAL-EXAMPLE-0020/uplinks": items(
			map[string]any{"name": "wan1", "status": "Up"},
		),
		"/network-monitoring/v1/config-health/SERIAL-EXAMPLE-0020/summary": map[string]any{"configStatus": "Up-to-date"},
		"/network-monitoring/v1/config-health/SERIAL-EXAMPLE-0020/issues":  map[string]any{},
		"/network-monitoring/v1/neighbours/SERIAL-EXAMPLE-0020":            []any{},

		"/network-monitoring/v1/clients": items(
			map[string]any{"macAddress": "00:00:5E:00:53:01", "clientName": "client-example-01", "status": "Up", "connectedDeviceSerial": "SERIAL-EXAMPLE-0001", "port": "1/1/1", "authenticationType": "802.1X"},
		),
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimRight(r.URL.Path, "/")
		body, ok := routes[path]
		if !ok {
			t.Errorf("unexpected request path in test fixture server: %s", path)
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"not found in test fixture"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(body)
	}))
}

func TestUnmanagedDeviceMAC(t *testing.T) {
	tests := []struct {
		name   string
		serial string
		want   string
	}{
		{name: "valid tpd_ prefix", serial: "tpd_003a9c3d2e4a", want: "00:00:5E:00:53:14"},
		{name: "no tpd_ prefix", serial: "SERIAL-EXAMPLE-0001", want: ""},
		{name: "wrong length after prefix", serial: "tpd_003a9c3d2e", want: ""},
		{name: "non-hex after prefix", serial: "tpd_00zz9c3d2e4a", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := unmanagedDeviceMAC(tt.serial); got != tt.want {
				t.Errorf("unmanagedDeviceMAC(%q) = %q, want %q", tt.serial, got, tt.want)
			}
		})
	}
}

func TestFilenameTimestamp_HasNoColons(t *testing.T) {
	ts := filenameTimestamp(time.Date(2026, 7, 6, 9, 33, 42, 0, time.UTC))
	if strings.Contains(ts, ":") {
		t.Errorf("filename timestamp must not contain ':' (illegal on Windows), got %q", ts)
	}
	if want := "2026-07-06T09-33-42Z"; ts != want {
		t.Errorf("got %q, want %q", ts, want)
	}
}

func TestOutputPath(t *testing.T) {
	tests := []struct {
		name string
		site string
		want string
	}{
		{name: "unfiltered fallback", site: "", want: filepath.Join("out", "all-sites", "hpe-arubacentral-2026-07-06T09-33-42Z-all-sites.json")},
		{name: "single site", site: "example-campus-1", want: filepath.Join("out", "example-campus-1", "hpe-arubacentral-2026-07-06T09-33-42Z-example-campus-1.json")},
		{name: "unsafe characters sanitized", site: "MXP/LAB: Main", want: filepath.Join("out", "MXP-LAB--Main", "hpe-arubacentral-2026-07-06T09-33-42Z-MXP-LAB--Main.json")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := OutputPath("out", "2026-07-06T09-33-42Z", tt.site); got != tt.want {
				t.Errorf("OutputPath(%q) = %q, want %q", tt.site, got, tt.want)
			}
		})
	}
}

func TestProducerCollect_EndToEnd(t *testing.T) {
	ts := fakeArubaCentralServer(t)
	defer ts.Close()

	cfg := &Config{
		BaseURL: ts.URL,
		Credentials: Credentials{
			AccessToken: "test-access-token",
		},
		Purpose:         "documentation",
		SafeFailureMode: "fail-closed",
	}

	producer := NewProducer(cfg)
	producer.client = &Client{
		baseURL:     ts.URL,
		httpClient:  ts.Client(),
		logger:      testLogger(),
		creds:       cfg.Credentials,
		purpose:     cfg.Purpose,
		minInterval: 0,
	}

	ctx := testharness.NewTestContext(t)
	doc, err := producer.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	if len(doc.Topology.Resources) == 0 {
		t.Fatal("expected at least one resource")
	}

	wantTypes := map[string]bool{
		"osiris.hpe.arubacentral.site":        false,
		"network.switch":                      false,
		"osiris.hpe.arubacentral.accesspoint": false,
		"network.gateway":                     false,
		"network.interface":                   false,
		"osiris.hpe.arubacentral.radio":       false,
		"osiris.hpe.arubacentral.bssid":       false,
		"osiris.hpe.arubacentral.wlan":        false,
		"osiris.hpe.arubacentral.client":      false,
	}
	for _, r := range doc.Topology.Resources {
		if _, tracked := wantTypes[r.Type]; tracked {
			wantTypes[r.Type] = true
		}
	}
	for typ, seen := range wantTypes {
		if !seen {
			t.Errorf("expected at least one resource of type %q", typ)
		}
	}

	// The switch's neighbor adjacency (a "Switch"-type /neighbours
	// entry, see the bare-array route above) must produce a network
	// connection to a stub for the unresolved remote device.
	foundNeighborConn := false
	for _, c := range doc.Topology.Connections {
		if c.Type == "network" && c.Properties["local_port"] == "1/1/1" {
			foundNeighborConn = true
		}
	}
	if !foundNeighborConn {
		t.Error("expected a neighbor adjacency connection from the switch's /neighbours entry")
	}

	wantGroupTypes := map[string]bool{
		"logical.devicegroup":         false,
		"network.vlan":                false,
		"osiris.hpe.arubacentral.lag": false,
	}
	for _, g := range doc.Topology.Groups {
		if _, tracked := wantGroupTypes[g.Type]; tracked {
			wantGroupTypes[g.Type] = true
		}
	}
	for typ, seen := range wantGroupTypes {
		if !seen {
			t.Errorf("expected at least one group of type %q", typ)
		}
	}

	// The switch should have picked up its device-group membership via
	// the config-health summary's deviceGroupName field.
	var group *struct{ members []string }
	for _, g := range doc.Topology.Groups {
		if g.Type == "logical.devicegroup" {
			if len(g.Members) != 1 {
				t.Errorf("expected 1 device wired into the device group, got %v", g.Members)
			}
			group = &struct{ members []string }{g.Members}
		}
	}
	if group == nil {
		t.Error("expected a logical.devicegroup group to be present")
	}
}

// TestProducerCollect_AuditClientPropertiesDoNotTripSecretScanner
// guards a test: --purpose audit --safe-failure-mode fail-closed
// aborted the entire site's document build with "secret scanning
// detected N finding(s)" against every client's "authentication_type"
// property (key_name:auth), pkg/sdk's cross-producer secret scanner
// (redact.go, SensitiveKeyPatterns) flags any property key containing
// "auth", and the value ("802.1X", "WPA3-PSK", etc.) is a
// classification string, not a secret. The property is now named
// "security_type" instead (see TransformClients), matching
// TransformWLANs' "security"/"security_level" naming convention.
func TestProducerCollect_AuditClientPropertiesDoNotTripSecretScanner(t *testing.T) {
	ts := fakeArubaCentralServer(t)
	defer ts.Close()

	cfg := &Config{
		BaseURL:         ts.URL,
		Credentials:     Credentials{AccessToken: "test-access-token"},
		Purpose:         "audit",
		SafeFailureMode: "fail-closed",
	}
	producer := NewProducer(cfg)
	producer.client = &Client{
		baseURL: ts.URL, httpClient: ts.Client(), logger: testLogger(),
		creds: cfg.Credentials, purpose: cfg.Purpose, minInterval: 0,
	}

	doc, err := producer.Collect(testharness.NewTestContext(t))
	if err != nil {
		t.Fatalf("Collect failed (this is the production bug: fail-closed secret scanning aborting the whole document over a false-positive key-name match): %v", err)
	}

	found := false
	for _, r := range doc.Topology.Resources {
		if r.Type != "osiris.hpe.arubacentral.client" {
			continue
		}
		if r.Properties["security_type"] == "802.1X" {
			found = true
		}
		if _, present := r.Properties["authentication_type"]; present {
			t.Errorf("property must be named security_type, not authentication_type (the old name trips pkg/sdk's secret scanner): %+v", r.Properties)
		}
	}
	if !found {
		t.Error("expected a client resource with security_type=802.1X")
	}
}

// TestRunExport_WritesOneFilePerSiteDirectory exercises the
// <output-dir>/<site-name>/hpe-arubacentral-<timestamp>-<site-name>.json
// hierarchy for a multi-site run (here, two sites against the same fake
// server the fixture data only tags devices to "example-campus-1", so
// the second site legitimately collects zero devices, which must still
// succeed and produce a file).
func TestRunExport_WritesOneFilePerSiteDirectory(t *testing.T) {
	ts := fakeArubaCentralServer(t)
	defer ts.Close()

	outDir := filepath.Join(t.TempDir(), "output")

	cfg := &Config{
		BaseURL: ts.URL,
		Credentials: Credentials{
			AccessToken: "test-access-token",
		},
		Purpose:         "documentation",
		SafeFailureMode: "fail-closed",
		Sites:           []string{"example-campus-1", "other-site"},
		OutputDir:       outDir,
	}

	if err := runExport(cfg); err != nil {
		t.Fatalf("runExport failed: %v", err)
	}

	for _, site := range cfg.Sites {
		siteDir := filepath.Join(outDir, site)
		entries, err := os.ReadDir(siteDir)
		if err != nil {
			t.Fatalf("expected site directory %q to be created: %v", siteDir, err)
		}
		if len(entries) != 1 || !strings.Contains(entries[0].Name(), site) {
			t.Errorf("expected exactly one %s file in %q, got %v", site, siteDir, entries)
		}
	}
}

// TestRunExport_SingleSiteAlsoUsesDirectoryHierarchy confirms a
// single-site run gets the same <output-dir>/<site-name>/<file>.json
// treatment as a multi-site run, rather than writing a
// literal file at --output.
func TestRunExport_SingleSiteAlsoUsesDirectoryHierarchy(t *testing.T) {
	ts := fakeArubaCentralServer(t)
	defer ts.Close()

	outDir := filepath.Join(t.TempDir(), "output")

	cfg := &Config{
		BaseURL: ts.URL,
		Credentials: Credentials{
			AccessToken: "test-access-token",
		},
		Purpose:         "documentation",
		SafeFailureMode: "fail-closed",
		Sites:           []string{"example-campus-1"},
		OutputDir:       outDir,
	}

	if err := runExport(cfg); err != nil {
		t.Fatalf("runExport failed: %v", err)
	}

	siteDir := filepath.Join(outDir, "example-campus-1")
	entries, err := os.ReadDir(siteDir)
	if err != nil {
		t.Fatalf("expected site directory %q to be created: %v", siteDir, err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly one file in %q, got %v", siteDir, entries)
	}
}

// TestRunExport_RerunReusesExistingOutputDir confirms the "incremental"
// contract: running twice against the same --output adds a second
// timestamped file into the existing site directory instead of failing
// or clobbering the first (os.MkdirAll is a no-op against a directory
// that already exists).
func TestRunExport_RerunReusesExistingOutputDir(t *testing.T) {
	ts := fakeArubaCentralServer(t)
	defer ts.Close()

	outDir := filepath.Join(t.TempDir(), "output")
	cfg := &Config{
		BaseURL: ts.URL,
		Credentials: Credentials{
			AccessToken: "test-access-token",
		},
		Purpose:         "documentation",
		SafeFailureMode: "fail-closed",
		Sites:           []string{"example-campus-1"},
		OutputDir:       outDir,
	}

	if err := runExport(cfg); err != nil {
		t.Fatalf("first runExport failed: %v", err)
	}
	// filenameTimestamp has whole-second resolution: without this, a
	// fast rerun within the same second would produce the same filename
	// twice and overwrite rather than accumulate.
	time.Sleep(1100 * time.Millisecond)
	if err := runExport(cfg); err != nil {
		t.Fatalf("second runExport failed: %v", err)
	}

	siteDir := filepath.Join(outDir, "example-campus-1")
	entries, err := os.ReadDir(siteDir)
	if err != nil {
		t.Fatalf("expected site directory %q to still exist: %v", siteDir, err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 accumulated files in %q after 2 runs, got %d: %v", siteDir, len(entries), entries)
	}
}

// TestProducerCollect_RawBodyOnlyInAuditWithIncludeRawBody guards the
// --include-raw-body contract: the raw API response body must appear
// under extensions[osiris.hpe.arubacentral].raw only when both
// --purpose audit and --include-raw-body are set, never otherwise.
func TestProducerCollect_RawBodyOnlyInAuditWithIncludeRawBody(t *testing.T) {
	collect := func(t *testing.T, purpose string, includeRawBody bool) *sdk.Document {
		t.Helper()
		ts := fakeArubaCentralServer(t)
		defer ts.Close()

		cfg := &Config{
			BaseURL:         ts.URL,
			Credentials:     Credentials{AccessToken: "test-access-token"},
			Purpose:         purpose,
			SafeFailureMode: "fail-closed",
			IncludeRawBody:  includeRawBody,
		}
		producer := NewProducer(cfg)
		producer.client = &Client{
			baseURL: ts.URL, httpClient: ts.Client(), logger: testLogger(),
			creds: cfg.Credentials, purpose: cfg.Purpose, minInterval: 0,
		}

		doc, err := producer.Collect(testharness.NewTestContext(t))
		if err != nil {
			t.Fatalf("Collect failed: %v", err)
		}
		return doc
	}

	hasRaw := func(doc *sdk.Document) bool {
		for _, r := range doc.Topology.Resources {
			if r.Type != "network.switch" {
				continue
			}
			ext, ok := r.Extensions[extensionNamespace].(map[string]any)
			if !ok {
				return false
			}
			_, ok = ext["raw"]
			return ok
		}
		t.Fatal("expected a network.switch resource in the document")
		return false
	}

	if got := hasRaw(collect(t, "documentation", false)); got {
		t.Error("documentation purpose must never include the raw body")
	}
	if got := hasRaw(collect(t, "audit", false)); got {
		t.Error("audit purpose without --include-raw-body must not include the raw body")
	}
	if got := hasRaw(collect(t, "documentation", true)); got {
		t.Error("--include-raw-body without audit purpose must not include the raw body")
	}
	if got := hasRaw(collect(t, "audit", true)); !got {
		t.Error("audit purpose with --include-raw-body must include the raw body")
	}
}

// TestProducerCollect_StackInterfacesAndHardwareRouteToEachMember
// guards: for a switch stack, /switches/{serial}/interfaces and
// /hardware-categories 404 when queried by a non-conductor member's own
// serial, and the conductor's response covers every physical member
// (each item carrying its own serialNumber). Collect must query only
// the conductor's serial never a member's own and route each returned
// item back to its own switch resource rather than attributing
// everything to the conductor.
func TestProducerCollect_StackInterfacesAndHardwareRouteToEachMember(t *testing.T) {
	items := func(v ...any) map[string]any { return map[string]any{"items": v} }
	empty := items()

	routes := map[string]any{
		"/network-config/v1/sites":                   empty,
		"/network-monitoring/v1/sites-health":        empty,
		"/network-monitoring/v1/sites-device-health": empty,
		"/network-config/v1/device-groups":           empty,
		"/network-monitoring/v1/switches": items(
			map[string]any{"serialNumber": "SERIAL-0001", "deviceName": "switch-conductor", "status": "Up", "deployment": "Stack", "switchRole": "Conductor", "stackId": "stack-1"},
			map[string]any{"serialNumber": "SERIAL-0002", "deviceName": "switch-member-1", "status": "Up", "deployment": "Stack", "switchRole": "Member", "stackId": "stack-1"},
			map[string]any{"serialNumber": "SERIAL-0003", "deviceName": "switch-member-2", "status": "Up", "deployment": "Stack", "switchRole": "Standby", "stackId": "stack-1"},
		),
		"/network-monitoring/v1/switches/SERIAL-0001/interfaces": items(
			map[string]any{"name": "1/1/1", "status": "Up", "serialNumber": "SERIAL-0001"},
			map[string]any{"name": "2/1/1", "status": "Up", "serialNumber": "SERIAL-0002"},
			map[string]any{"name": "3/1/1", "status": "Down", "serialNumber": "SERIAL-0003"},
		),
		"/network-monitoring/v1/switches/SERIAL-0001/hardware-categories": items(
			map[string]any{"serialNumber": "SERIAL-0001", "cpu": map[string]any{"health": "Good"}},
			map[string]any{"serialNumber": "SERIAL-0002", "cpu": map[string]any{"health": "Good"}},
			map[string]any{"serialNumber": "SERIAL-0003", "cpu": map[string]any{"health": "Poor"}},
		),
		"/network-monitoring/v1/switches/SERIAL-0001/vlans": empty,
		"/network-monitoring/v1/switches/SERIAL-0001/lag":   empty,
	}
	for _, serial := range []string{"SERIAL-0001", "SERIAL-0002", "SERIAL-0003"} {
		routes["/network-monitoring/v1/stack/"+serial+"/members"] = map[string]any{}
		routes["/network-monitoring/v1/switches/"+serial+"/vsx"] = map[string]any{}
		routes["/network-monitoring/v1/config-health/"+serial+"/summary"] = map[string]any{}
		routes["/network-monitoring/v1/config-health/"+serial+"/issues"] = map[string]any{}
		routes["/network-monitoring/v1/neighbours/"+serial] = []any{} // bare array, not an {"items": [...]} envelope
	}
	for _, path := range []string{
		"/network-monitoring/v1/aps", "/network-monitoring/v1/wlans", "/network-monitoring/v1/radios",
		"/network-monitoring/v1/bssids", "/network-monitoring/v1/swarms", "/network-monitoring/v1/gateways",
		"/network-monitoring/v1/clients",
	} {
		routes[path] = empty
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimRight(r.URL.Path, "/")
		body, ok := routes[path]
		if !ok {
			t.Errorf("unexpected request path (member serials must never be queried directly for interfaces/hardware): %s", path)
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"not found in test fixture"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(body)
	}))
	defer ts.Close()

	cfg := &Config{
		BaseURL:         ts.URL,
		Credentials:     Credentials{AccessToken: "test-access-token"},
		Purpose:         "documentation",
		SafeFailureMode: "fail-closed",
	}
	producer := NewProducer(cfg)
	producer.client = &Client{
		baseURL: ts.URL, httpClient: ts.Client(), logger: testLogger(),
		creds: cfg.Credentials, purpose: cfg.Purpose, minInterval: 0,
	}

	doc, err := producer.Collect(testharness.NewTestContext(t))
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	switchByName := map[string]*sdk.Resource{}
	interfacesBySwitch := map[string]int{}
	for i := range doc.Topology.Resources {
		r := &doc.Topology.Resources[i]
		if r.Type == "network.switch" {
			switchByName[r.Name] = r
		}
	}
	for _, c := range doc.Topology.Connections {
		if c.Type != "contains" {
			continue
		}
		for name, sw := range switchByName {
			if c.Source == sw.ID {
				interfacesBySwitch[name]++
			}
		}
	}

	for _, name := range []string{"switch-conductor", "switch-member-1", "switch-member-2"} {
		if _, ok := switchByName[name]; !ok {
			t.Fatalf("expected a switch resource named %q", name)
		}
	}
	if interfacesBySwitch["switch-conductor"] != 1 || interfacesBySwitch["switch-member-1"] != 1 || interfacesBySwitch["switch-member-2"] != 1 {
		t.Errorf("expected each stack member to own exactly its own interface, got %v", interfacesBySwitch)
	}

	mem2 := switchByName["switch-member-2"]
	hwProps, ok := mem2.Properties["hardware"].(map[string]any)
	if !ok || hwProps["cpu_health"] != "Poor" {
		t.Errorf("expected switch-member-2's own (unhealthy) hardware to be applied to its own resource, got %+v", mem2.Properties["hardware"])
	}
	if mem2.Status != "degraded" {
		t.Errorf("expected switch-member-2 to be degraded from its own unhealthy CPU, got %q", mem2.Status)
	}

	cond := switchByName["switch-conductor"]
	if hwProps, ok := cond.Properties["hardware"].(map[string]any); !ok || hwProps["cpu_health"] != "Good" {
		t.Errorf("expected switch-conductor to keep its own (healthy) hardware, got %+v", cond.Properties["hardware"])
	}
	if cond.Status == "degraded" {
		t.Error("expected switch-conductor to stay healthy: member-2's bad CPU must not bleed onto the conductor's resource")
	}
}

// TestProducerCollect_DeviceSiteFilterFallsBackToSiteID guards on a
// bug apparently found during tests for which a discussion is open
// https://airheads.hpe.com/discussion/new-central-api-get-a-list-of-access-points
// crosschecking a site export against Aruba Central's own data showed
// /aps reporting siteName inconsistently for at least one device while
// siteId stayed correct, so the device was silently dropped from the
// export even though it genuinely belongs to the requested site.
// Device-to-site filtering now matches on siteName OR siteId (see
// keepDeviceSite in arubacentral.go).
func TestProducerCollect_DeviceSiteFilterFallsBackToSiteID(t *testing.T) {
	items := func(v ...any) map[string]any { return map[string]any{"items": v} }
	empty := items()

	routes := map[string]any{
		"/network-config/v1/sites": items(
			map[string]any{"scopeId": "it-mxp-id", "scopeName": "IT-MXP", "deviceCount": 1},
		),
		"/network-config/v1/device-groups":           empty,
		"/network-monitoring/v1/sites-health":        empty,
		"/network-monitoring/v1/sites-device-health": empty,
		"/network-monitoring/v1/switches":            empty,
		"/network-monitoring/v1/gateways":            empty,
		"/network-monitoring/v1/clients":             empty,
		"/network-monitoring/v1/wlans":               empty, "/network-monitoring/v1/radios": empty,
		"/network-monitoring/v1/bssids": empty, "/network-monitoring/v1/swarms": empty,
		// The mismatch: siteName is "ITMXP" (no hyphen), but siteId
		// matches the site's own scopeId exactly mirroring the real
		// record found via crosscheck.
		"/network-monitoring/v1/aps": items(
			map[string]any{"serialNumber": "SERIAL-EXAMPLE-MISMATCH", "deviceName": "ap-example-mismatch", "status": "Up", "siteName": "ITMXP", "siteId": "it-mxp-id"},
		),
		"/network-monitoring/v1/aps/SERIAL-EXAMPLE-MISMATCH/ports":             empty,
		"/network-monitoring/v1/aps/SERIAL-EXAMPLE-MISMATCH/tunnels":           empty,
		"/network-monitoring/v1/aps/SERIAL-EXAMPLE-MISMATCH/wlans":             empty,
		"/network-monitoring/v1/config-health/SERIAL-EXAMPLE-MISMATCH/summary": map[string]any{},
		"/network-monitoring/v1/config-health/SERIAL-EXAMPLE-MISMATCH/issues":  map[string]any{},
		"/network-monitoring/v1/neighbours/SERIAL-EXAMPLE-MISMATCH":            []any{},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimRight(r.URL.Path, "/")
		body, ok := routes[path]
		if !ok {
			t.Errorf("unexpected request path: %s", path)
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"not found in test fixture"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(body)
	}))
	defer ts.Close()

	cfg := &Config{
		BaseURL:         ts.URL,
		Cluster:         "eucentral3",
		Credentials:     Credentials{AccessToken: "test-fake-access-token"},
		Sites:           []string{"IT-MXP"},
		Purpose:         "documentation",
		SafeFailureMode: "fail-closed",
	}
	producer := NewProducer(cfg)
	producer.client = &Client{
		baseURL: ts.URL, httpClient: ts.Client(), logger: testLogger(),
		creds: cfg.Credentials, purpose: cfg.Purpose, minInterval: 0,
	}

	doc, err := producer.Collect(testharness.NewTestContext(t))
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	found := false
	for _, r := range doc.Topology.Resources {
		if r.Type == "osiris.hpe.arubacentral.accesspoint" && r.Name == "ap-example-mismatch" {
			found = true
		}
	}
	if !found {
		t.Error("expected the AP with mismatched siteName but matching siteId to be kept, not dropped")
	}
}

// TestProducerCollect_SitesFilteredAndHealthEnriched guards three
// things reported from tests:
// (1) osiris.hpe.arubacentral.site resources were not filtered by
// --site, so a single-site export listed every site in the account.
// (2) site health (from /network-monitoring/v1/sites-health) is folded
// onto the matching osiris.hpe.arubacentral.site resource, gated by
// --purpose like the device config-health enrichment.
// (3) ListClients is account-wide (/network-monitoring/v1/clients has
// no site scoping in the API itself) and was never passed through
// keepSite the way switches/APs/gateways/sites are, so every site's
// export bundled every other site's clients too, inflating each
// document and making per-site snapshots inaccurate.
// Also checks metadata.scope.clusters.
func TestProducerCollect_SitesFilteredAndHealthEnriched(t *testing.T) {
	items := func(v ...any) map[string]any { return map[string]any{"items": v} }
	empty := items()

	routes := map[string]any{
		"/network-config/v1/sites": items(
			map[string]any{"scopeId": "site-a-id", "scopeName": "site-a", "deviceCount": 1},
			map[string]any{"scopeId": "site-b-id", "scopeName": "site-b", "deviceCount": 1},
		),
		"/network-config/v1/device-groups": empty,
		"/network-monitoring/v1/sites-health": items(
			map[string]any{"siteId": "site-a-id", "siteName": "site-a", "siteHealth": "Poor", "deviceHealth": "Poor", "clientHealth": "Good"},
			map[string]any{"siteId": "site-b-id", "siteName": "site-b", "siteHealth": "Good", "deviceHealth": "Good", "clientHealth": "Good"},
		),
		"/network-monitoring/v1/sites-device-health": items(
			map[string]any{"siteId": "site-a-id", "siteName": "site-a", "apHealth": "Fair", "switchHealth": "Poor", "gatewayHealth": "Good", "bridgeHealth": "Good"},
			map[string]any{"siteId": "site-b-id", "siteName": "site-b", "apHealth": "Good", "switchHealth": "Good", "gatewayHealth": "Good", "bridgeHealth": "Good"},
		),
		"/network-monitoring/v1/switches": items(
			map[string]any{"serialNumber": "SERIAL-SITE-A", "deviceName": "switch-site-a", "status": "Up", "siteName": "site-a"},
			map[string]any{"serialNumber": "SERIAL-SITE-B", "deviceName": "switch-site-b", "status": "Up", "siteName": "site-b"},
		),
		"/network-monitoring/v1/aps": empty, "/network-monitoring/v1/wlans": empty, "/network-monitoring/v1/radios": empty,
		"/network-monitoring/v1/bssids": empty, "/network-monitoring/v1/swarms": empty, "/network-monitoring/v1/gateways": empty,
		"/network-monitoring/v1/clients": items(
			map[string]any{"macAddress": "00:00:5E:00:53:10", "clientName": "client-site-a", "status": "Up", "siteName": "site-a"},
			map[string]any{"macAddress": "00:00:5E:00:53:11", "clientName": "client-site-b", "status": "Up", "siteName": "site-b"},
		),
	}
	for _, serial := range []string{"SERIAL-SITE-A", "SERIAL-SITE-B"} {
		routes["/network-monitoring/v1/switches/"+serial+"/interfaces"] = empty
		routes["/network-monitoring/v1/switches/"+serial+"/hardware-categories"] = empty
		routes["/network-monitoring/v1/switches/"+serial+"/vlans"] = empty
		routes["/network-monitoring/v1/switches/"+serial+"/lag"] = empty
		routes["/network-monitoring/v1/stack/"+serial+"/members"] = map[string]any{}
		routes["/network-monitoring/v1/switches/"+serial+"/vsx"] = map[string]any{}
		routes["/network-monitoring/v1/config-health/"+serial+"/summary"] = map[string]any{}
		routes["/network-monitoring/v1/config-health/"+serial+"/issues"] = map[string]any{}
		routes["/network-monitoring/v1/neighbours/"+serial] = []any{}
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimRight(r.URL.Path, "/")
		body, ok := routes[path]
		if !ok {
			t.Errorf("unexpected request path: %s", path)
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"not found in test fixture"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(body)
	}))
	defer ts.Close()

	cfg := &Config{
		BaseURL:         ts.URL,
		Cluster:         "eucentral3",
		Credentials:     Credentials{AccessToken: "test-access-token"},
		Sites:           []string{"site-a"},
		Purpose:         "audit",
		SafeFailureMode: "fail-closed",
	}
	producer := NewProducer(cfg)
	producer.client = &Client{
		baseURL: ts.URL, httpClient: ts.Client(), logger: testLogger(),
		creds: cfg.Credentials, purpose: cfg.Purpose, minInterval: 0,
	}

	doc, err := producer.Collect(testharness.NewTestContext(t))
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	var siteResources []sdk.Resource
	for _, r := range doc.Topology.Resources {
		if r.Type == "osiris.hpe.arubacentral.site" {
			siteResources = append(siteResources, r)
		}
	}
	if len(siteResources) != 1 || siteResources[0].Name != "site-a" {
		t.Fatalf("expected only site-a's osiris.hpe.arubacentral.site resource (site-b filtered out), got %+v", siteResources)
	}
	if siteResources[0].Status != "degraded" {
		t.Errorf("expected site-a to be degraded from its Poor site health, got %q", siteResources[0].Status)
	}
	if got := siteResources[0].Properties["health_status"]; got != "Poor" {
		t.Errorf("expected health_status %q, got %v", "Poor", got)
	}
	if got := siteResources[0].Properties["device_health"]; got != "Poor" {
		t.Errorf("expected audit-only device_health to be present, got %v", got)
	}
	if got := siteResources[0].Properties["switch_health"]; got != "Poor" {
		t.Errorf("expected audit-only switch_health (from sites-device-health) to be present, got %v", got)
	}

	switchNames := map[string]bool{}
	for _, r := range doc.Topology.Resources {
		if r.Type == "network.switch" {
			switchNames[r.Name] = true
		}
	}
	if switchNames["switch-site-a"] != true || switchNames["switch-site-b"] {
		t.Errorf("expected only switch-site-a (site filter applied to devices too), got %v", switchNames)
	}

	clientNames := map[string]bool{}
	for _, r := range doc.Topology.Resources {
		if r.Type == "osiris.hpe.arubacentral.client" {
			clientNames[r.Name] = true
		}
	}
	if clientNames["client-site-a"] != true || clientNames["client-site-b"] {
		t.Errorf("expected only client-site-a (ListClients is account-wide and must be filtered by --site like every other resource type), got %v", clientNames)
	}

	if doc.Metadata.Scope == nil || len(doc.Metadata.Scope.Clusters) != 1 || doc.Metadata.Scope.Clusters[0] != "eucentral3 ("+ts.URL+")" {
		t.Errorf("expected metadata.scope.clusters to contain the cluster+base URL, got %+v", doc.Metadata.Scope)
	}
}

// TestProducerCollect_UnmanagedDeviceAndIsolatedDevices covers two more
// best-effort enrichments, both gated to --purpose audit
// --include-raw-body since neither endpoint has a confirmed response
// schema:
// (1) an "Unmanaged"-type neighbor's stub resource gets enriched via
// GetUnmanagedDevice, using a MAC derived from its "tpd_<hex>" serial.
// (2) isolated-devices for site attach as raw data on the site resource.
func TestProducerCollect_UnmanagedDeviceAndIsolatedDevices(t *testing.T) {
	items := func(v ...any) map[string]any { return map[string]any{"items": v} }
	empty := items()

	routes := map[string]any{
		"/network-config/v1/sites": items(
			map[string]any{"scopeId": "site-a-id", "scopeName": "site-a", "deviceCount": 1},
		),
		"/network-config/v1/device-groups":           empty,
		"/network-monitoring/v1/sites-health":        empty,
		"/network-monitoring/v1/sites-device-health": empty,
		"/network-monitoring/v1/isolated-devices/site-a-id": map[string]any{
			"id": "site-a-id", "type": "site",
			"isolatedDevices": []map[string]any{{"macAddress": "00:00:5E:00:53:00"}},
		},
		"/network-monitoring/v1/switches": items(
			map[string]any{"serialNumber": "SERIAL-EXAMPLE-0001", "deviceName": "switch-example-01", "status": "Up", "siteName": "site-a"},
		),
		"/network-monitoring/v1/switches/SERIAL-EXAMPLE-0001/interfaces":          empty,
		"/network-monitoring/v1/switches/SERIAL-EXAMPLE-0001/hardware-categories": empty,
		"/network-monitoring/v1/switches/SERIAL-EXAMPLE-0001/vlans":               empty,
		"/network-monitoring/v1/switches/SERIAL-EXAMPLE-0001/lag":                 empty,
		"/network-monitoring/v1/stack/SERIAL-EXAMPLE-0001/members":                map[string]any{},
		"/network-monitoring/v1/switches/SERIAL-EXAMPLE-0001/vsx":                 map[string]any{},
		"/network-monitoring/v1/config-health/SERIAL-EXAMPLE-0001/summary":        map[string]any{},
		"/network-monitoring/v1/config-health/SERIAL-EXAMPLE-0001/issues":         map[string]any{},
		"/network-monitoring/v1/neighbours/SERIAL-EXAMPLE-0001": []any{
			map[string]any{"type": "Unmanaged", "_serial": "SERIAL-EXAMPLE-0001", "serial": "tpd_003a9c3d2e4a", "localPort": "1/1/1", "toPort": "eth0", "name": "example-unmanaged-switch", "siteId": "site-a-id"},
		},
		"/network-monitoring/v1/unmanaged-device/00:00:5E:00:53:22": map[string]any{"vendor": "Example Corp"},
		"/network-monitoring/v1/aps":                                empty, "/network-monitoring/v1/wlans": empty, "/network-monitoring/v1/radios": empty,
		"/network-monitoring/v1/bssids": empty, "/network-monitoring/v1/swarms": empty, "/network-monitoring/v1/gateways": empty,
		"/network-monitoring/v1/clients": empty,
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimRight(r.URL.Path, "/")
		body, ok := routes[path]
		if !ok {
			t.Errorf("unexpected request path: %s", path)
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"not found in test fixture"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(body)
	}))
	defer ts.Close()

	cfg := &Config{
		BaseURL: ts.URL, Credentials: Credentials{AccessToken: "test-access-token"},
		Purpose: "audit", IncludeRawBody: true, SafeFailureMode: "fail-closed",
	}
	producer := NewProducer(cfg)
	producer.client = &Client{
		baseURL: ts.URL, httpClient: ts.Client(), logger: testLogger(),
		creds: cfg.Credentials, purpose: cfg.Purpose, minInterval: 0,
	}

	doc, err := producer.Collect(testharness.NewTestContext(t))
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	var stub *sdk.Resource
	for i, r := range doc.Topology.Resources {
		if r.Provider.NativeID == "tpd_003a9c3d2e4a" {
			stub = &doc.Topology.Resources[i]
		}
	}
	if stub == nil {
		t.Fatal("expected a stub resource for the Unmanaged neighbor")
	}
	ext, _ := stub.Extensions[extensionNamespace].(map[string]any)
	detail, _ := ext["unmanaged_device_raw"].(map[string]any)
	if detail["vendor"] != "Example Corp" {
		t.Errorf("expected the unmanaged device stub to be enriched via GetUnmanagedDevice, got %+v", ext)
	}

	var site *sdk.Resource
	for i, r := range doc.Topology.Resources {
		if r.Type == "osiris.hpe.arubacentral.site" {
			site = &doc.Topology.Resources[i]
		}
	}
	if site == nil {
		t.Fatal("expected a osiris.hpe.arubacentral.site resource")
	}
	siteExt, _ := site.Extensions[extensionNamespace].(map[string]any)
	isolated, _ := siteExt["isolated_devices_raw"].([]map[string]any)
	if len(isolated) != 1 || isolated[0]["macAddress"] != "00:00:5E:00:53:12" {
		t.Errorf("expected isolated_devices_raw on the site resource, got %+v", siteExt)
	}
}
