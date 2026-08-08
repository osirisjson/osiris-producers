// vmanage_test.go - Integration tests for the vManage producer's
// end-to-end export: login, device fetch, per-site document split and
// file layout.
//
// OSIRIS JSON Producer for Cisco SD-WAN Manager (vManage) introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package vmanage

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"go.osirisjson.org/producers/pkg/sdk"
)

// fixtureSiteNames maps the fixture's site-ids to a human-readable
// name, as served by /dataservice/topology/device/site/{siteId}.
var fixtureSiteNames = map[string]string{
	"100": "TEST-SITE-ONE",
	"200": "TEST-SITE-TWO",
}

// fixtureHandler serves canned vManage responses: two WAN edge sites
// plus one unclaimed device, one tenant.
func fixtureHandler(w http.ResponseWriter, r *http.Request) {
	devicesJSON := `[
		{"deviceId":"192.0.2.1","system-ip":"192.0.2.1","host-name":"TEST-VMANAGE1","uuid":"11111111-1111-1111-1111-111111111111","site-id":"100","personality":"vmanage","device-type":"vmanage","device-model":"vmanage","reachability":"reachable","status":"normal","validity":"valid","version":"20.9.1","platform":"x86_64"},
		{"deviceId":"192.0.2.10","system-ip":"192.0.2.10","host-name":"TEST-CEDGE1","uuid":"22222222-2222-2222-2222-222222222222","site-id":"200","personality":"cedge","device-type":"cedge","device-model":"C8000v","reachability":"reachable","status":"normal","validity":"valid","version":"17.09.01","platform":"x86_64"},
		{"deviceId":"192.0.2.20","system-ip":"192.0.2.20","host-name":"TEST-VEDGE-UNSITED","uuid":"33333333-3333-3333-3333-333333333333","site-id":"","personality":"vedge","device-type":"vedge","device-model":"vedge-cloud","reachability":"reachable","status":"normal","validity":"valid","version":"20.9.1","platform":"x86_64"}
	]`

	switch r.URL.Path {
	case "/j_security_check":
		w.WriteHeader(http.StatusOK)
	case "/dataservice/client/token":
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"token":"test-xsrf-token"}`)
	case "/dataservice/device":
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, devicesJSON)
	case "/dataservice/tenant":
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"name":"TEST-TENANT-1"}]`)
	default:
		if strings.HasPrefix(r.URL.Path, "/dataservice/topology/device/site/") {
			siteID := strings.TrimPrefix(r.URL.Path, "/dataservice/topology/device/site/")
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"site_name":%q,"data":[]}`, fixtureSiteNames[siteID])
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}
}

func TestRunExport_EndToEnd(t *testing.T) {
	// runExport dials https via NewClient, so the fixture must be an
	// HTTPS test server (with --insecure to skip the self-signed
	// certificate check) to exercise the same code path Run() does.
	ts := httptest.NewTLSServer(http.HandlerFunc(fixtureHandler))
	defer ts.Close()

	host, port := splitHostPort(t, ts.Listener.Addr().String())
	cfg := &Config{
		Host:              host,
		Port:              port,
		Username:          "user",
		Password:          "changeme",
		OutputDir:         t.TempDir(),
		SafeFailureMode:   sdk.FailClosed,
		Purpose:           "documentation",
		InsecureTLS:       true,
		AllSites:          true, // avoid the interactive picker reading os.Stdin in tests.
		SiteNameRateLimit: 1000, // fast, deterministic tests not exercising throttling here.
	}

	if err := runExport(cfg); err != nil {
		t.Fatalf("runExport failed: %v", err)
	}

	var files []string
	err := filepath.Walk(cfg.OutputDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking output dir failed: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("expected 3 output files (site 100, site 200, unclaimed), got %d: %v", len(files), files)
	}

	// Every written file must parse as a valid OSIRIS JSON document
	// with the expected generator and exactly one resource.
	var sawResolvedSiteName bool
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		var doc sdk.Document
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatalf("unmarshaling %s: %v", f, err)
		}
		if doc.Metadata.Generator.Name != generatorName {
			t.Errorf("%s: generator = %q, want %q", f, doc.Metadata.Generator.Name, generatorName)
		}
		if doc.Metadata.Generator.Version != generatorVersion {
			t.Errorf("%s: generator version = %q, want %q", f, doc.Metadata.Generator.Version, generatorVersion)
		}
		if doc.Metadata.Generator.URL != generatorURL {
			t.Errorf("%s: generator url = %q, want %q", f, doc.Metadata.Generator.URL, generatorURL)
		}
		if len(doc.Topology.Resources) != 1 {
			t.Errorf("%s: expected 1 resource, got %d", f, len(doc.Topology.Resources))
		}
		if doc.Metadata.Scope == nil {
			t.Fatalf("%s: metadata.scope is nil", f)
		}
		if len(doc.Metadata.Scope.Providers) != 1 || doc.Metadata.Scope.Providers[0] != providerName {
			t.Errorf("%s: scope.providers = %v, want [%q]", f, doc.Metadata.Scope.Providers, providerName)
		}
		if len(doc.Metadata.Scope.Clusters) != 1 || doc.Metadata.Scope.Clusters[0] != host {
			t.Errorf("%s: scope.clusters = %v, want [%q]", f, doc.Metadata.Scope.Clusters, host)
		}
		// The unclaimed document has no site to resolve a name for.
		if len(doc.Metadata.Scope.Sites) == 1 {
			for _, name := range fixtureSiteNames {
				if doc.Metadata.Scope.Sites[0] == name {
					sawResolvedSiteName = true
				}
			}
		}
	}
	if !sawResolvedSiteName {
		t.Error("expected at least one document's scope.sites to contain a resolved site name, not just the numeric site-id")
	}

	// Confirm the unclaimed fallback directory exists for the device
	// with an empty site-id.
	if _, err := os.Stat(filepath.Join(cfg.OutputDir, unsitedSegment)); err != nil {
		t.Errorf("expected %q directory to exist: %v", unsitedSegment, err)
	}
}

func TestRunExport_AccountsPopulatedFromTenants(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(fixtureHandler))
	defer ts.Close()

	host, port := splitHostPort(t, ts.Listener.Addr().String())
	cfg := &Config{
		Host:              host,
		Port:              port,
		Username:          "user",
		Password:          "changeme",
		OutputDir:         t.TempDir(),
		SafeFailureMode:   sdk.FailClosed,
		Purpose:           "documentation",
		InsecureTLS:       true,
		AllSites:          true, // avoid the interactive picker reading os.Stdin in tests.
		SiteNameRateLimit: 1000, // fast, deterministic tests - not exercising throttling here.
	}

	if err := runExport(cfg); err != nil {
		t.Fatalf("runExport failed: %v", err)
	}

	var found bool
	_ = filepath.Walk(cfg.OutputDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var doc sdk.Document
		if err := json.Unmarshal(data, &doc); err != nil {
			return err
		}
		if doc.Metadata.Scope == nil {
			return nil
		}
		for _, a := range doc.Metadata.Scope.Accounts {
			if a == "TEST-TENANT-1" {
				found = true
			}
		}
		return nil
	})
	if !found {
		t.Error("expected metadata.scope.accounts to contain the fixture tenant label")
	}
}

// fixtureHandlerWithRawBody extends fixtureHandler with canned
// responses for the per-device/per-site endpoints
// (interface/waninterface/omp-peers/site-topology-monitor) that
// fixtureHandler alone 404s on, so --include-raw-body has something to
// attach for TEST-CEDGE1 (system-ip 192.0.2.10, site 200).
func fixtureHandlerWithRawBody(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/dataservice/device/interface" && r.URL.Query().Get("deviceId") == "192.0.2.10":
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"vdevice-host-name":"TEST-CEDGE1","ifname":"GigabitEthernet0/0/0","af-type":"ipv4","ip-address":"192.0.2.10/24","if-admin-status":"Up","if-oper-status":"Up"}]`)
	case r.URL.Path == "/dataservice/device/control/waninterface" && r.URL.Query().Get("deviceId") == "192.0.2.10":
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[]`)
	case r.URL.Path == "/dataservice/device/omp/peers" && r.URL.Query().Get("deviceId") == "192.0.2.10":
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"vdevice-host-name":"TEST-CEDGE1","peer":"192.0.2.1","type":"vsmart","state":"up"}]`)
	case r.URL.Path == "/dataservice/topology/monitor/site/200":
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"device-id":"192.0.2.10","device-health":"good","circuits":[]}]`)
	default:
		fixtureHandler(w, r)
	}
}

// TestRunExport_IncludeRawBody confirms --include-raw-body (only
// active under --purpose audit, see wantRawBody in vmanage.go) attaches
// each collected endpoint's raw JSON response to the owning device
// resource under extensions["osiris.cisco.vmanage"], and that nothing
// is attached when either --purpose isn't audit or --include-raw-body
// is off - the two-flag gate documented on Config.IncludeRawBody.
func TestRunExport_IncludeRawBody(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(fixtureHandlerWithRawBody))
	defer ts.Close()
	host, port := splitHostPort(t, ts.Listener.Addr().String())

	baseCfg := func() *Config {
		return &Config{
			Host:              host,
			Port:              port,
			Username:          "user",
			Password:          "changeme",
			SafeFailureMode:   sdk.FailClosed,
			InsecureTLS:       true,
			AllSites:          true,
			SiteNameRateLimit: 1000,
		}
	}

	findSite200Resource := func(t *testing.T, outputDir string) sdk.Resource {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(outputDir, "TEST-SITE-TWO", singleFileIn(t, filepath.Join(outputDir, "TEST-SITE-TWO"))))
		if err != nil {
			t.Fatalf("reading site 200 output: %v", err)
		}
		var doc sdk.Document
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatalf("unmarshaling site 200 output: %v", err)
		}
		for _, r := range doc.Topology.Resources {
			if r.Type == "network.router" {
				return r
			}
		}
		t.Fatal("no network.router resource found in site 200 output")
		return sdk.Resource{}
	}

	t.Run("audit+include-raw-body attaches raw bodies", func(t *testing.T) {
		cfg := baseCfg()
		cfg.OutputDir = t.TempDir()
		cfg.Purpose = "audit"
		cfg.IncludeRawBody = true

		if err := runExport(cfg); err != nil {
			t.Fatalf("runExport failed: %v", err)
		}

		r := findSite200Resource(t, cfg.OutputDir)
		ext, _ := r.Extensions[extensionKey].(map[string]any)
		if ext == nil {
			t.Fatal("expected extensions[osiris.cisco.vmanage] to be set")
		}
		for _, key := range []string{"raw", "interfaces_raw", "omp_peers_raw", "site_topology_raw"} {
			if _, ok := ext[key]; !ok {
				t.Errorf("expected extensions[osiris.cisco.vmanage][%q] to be set", key)
			}
		}
		// wan_interfaces_raw is deliberately absent: the fixture returns
		// an empty WAN interface list for this device, and setExtension
		// is only called for a non-empty slice (see vmanage.go).
		if _, ok := ext["wan_interfaces_raw"]; ok {
			t.Error("expected extensions[osiris.cisco.vmanage][wan_interfaces_raw] to be absent for an empty WAN interface list")
		}
	})

	t.Run("documentation purpose does not attach raw bodies even with include-raw-body", func(t *testing.T) {
		cfg := baseCfg()
		cfg.OutputDir = t.TempDir()
		cfg.Purpose = "documentation"
		cfg.IncludeRawBody = true

		if err := runExport(cfg); err != nil {
			t.Fatalf("runExport failed: %v", err)
		}

		r := findSite200Resource(t, cfg.OutputDir)
		if ext, ok := r.Extensions[extensionKey].(map[string]any); ok {
			if _, ok := ext["raw"]; ok {
				t.Error("expected no raw body attachment when --purpose is not audit")
			}
		}
	})

	t.Run("audit purpose without include-raw-body does not attach raw bodies", func(t *testing.T) {
		cfg := baseCfg()
		cfg.OutputDir = t.TempDir()
		cfg.Purpose = "audit"
		cfg.IncludeRawBody = false

		if err := runExport(cfg); err != nil {
			t.Fatalf("runExport failed: %v", err)
		}

		r := findSite200Resource(t, cfg.OutputDir)
		if ext, ok := r.Extensions[extensionKey].(map[string]any); ok {
			if _, ok := ext["raw"]; ok {
				t.Error("expected no raw body attachment when --include-raw-body is off")
			}
		}
	})
}

// singleFileIn returns the name of the single file expected inside dir,
// failing the test if there isn't exactly one.
func singleFileIn(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading dir %s: %v", dir, err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly 1 file in %s, got %d", dir, len(entries))
	}
	return entries[0].Name()
}

func TestRunExport_DeviceFetchFailureIsFatal(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/j_security_check":
			w.WriteHeader(http.StatusOK)
		case "/dataservice/client/token":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"token":"test-xsrf-token"}`)
		case "/dataservice/device":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	host, port := splitHostPort(t, ts.Listener.Addr().String())
	cfg := &Config{
		Host:            host,
		Port:            port,
		Username:        "user",
		Password:        "changeme",
		OutputDir:       t.TempDir(),
		SafeFailureMode: sdk.FailClosed,
		Purpose:         "documentation",
		InsecureTLS:     true,
	}

	if err := runExport(cfg); err == nil {
		t.Fatal("expected runExport to fail when the device fetch fails")
	}
}

// A connection failure here (not a nil return) proves Run() reached
// ParseFlags/runExport instead of short-circuiting to printHelp.
// Password is supplied via --token-file rather than -p (there is no
// such flag - see flags.go) so this doesn't depend on a controlling
// terminal being available to satisfy the interactive prompt fallback.
func TestRun_ShortHostFlagNotMistakenForHelp(t *testing.T) {
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "cisco-vmanage-secrets.json")
	if err := os.WriteFile(tokenFile, []byte(`{"password":"changeme"}`), 0600); err != nil {
		t.Fatalf("writing token file: %v", err)
	}

	// 127.0.0.1:1 is loopback with a closed port: connection refused
	// immediately, no external network dependency, no timeout wait.
	err := Run([]string{"-h", "127.0.0.1:1", "-u", "user", "--token-file", tokenFile})
	if err == nil {
		t.Fatal("expected Run to attempt a connection and fail, not silently succeed via the help path")
	}
}

func TestRun_LongHelpFlagStillPrintsHelp(t *testing.T) {
	if err := Run([]string{"--help"}); err != nil {
		t.Errorf("Run([--help]) should return nil, got: %v", err)
	}
}

func splitHostPort(t *testing.T, addr string) (string, int) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("splitting %q: %v", addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parsing port %q: %v", portStr, err)
	}
	return host, port
}
