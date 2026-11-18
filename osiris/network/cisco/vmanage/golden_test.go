// golden_test.go - Source-to-golden test for the Cisco Catalyst SD-WAN
// Manager (vManage) OSIRIS JSON Producer.
//
// Drives runExport end to end, login included, against an in-memory
// HTTPS vManage that serves synthetic /dataservice response bodies and
// compares the emitted per-site document byte for byte with the
// expected document, at both --purpose flag levels.
//
// Fixtures live under testdata/osiris/network/cisco/vmanage/ at the
// repository root: one snake_case directory per scenario, holding one
// response body per endpoint (api/<path>.json, see goldenServer for the
// path mapping) and the expected document per purpose
// (purpose/golden_<purpose>.json). Their provenance, value sources and
// deliberate omissions are documented in that directory's README.md.
// Regenerate the expected documents with OSIRIS_UPDATE_GOLDEN=1 and
// review the diff: they are the producer's output contract.
//
// runExport stamps the wall clock into metadata.timestamp and the dial
// host into metadata.scope.clusters. The test checks that the dial
// host is the only cluster, then pins both fields to fixed values
// before the comparison, so the documents stay reproducible and carry
// no loopback address. Every other byte is the producer's own output.
//
// TestGolden_PurposeContract proves the documentation-purpose goldens
// carry no audit-only field and the audit-purpose goldens do.
//
// TestGolden_GraphIntegrity proves every connection endpoint and every
// group member of each golden document is an emitted resource.
//
// OSIRIS JSON Producer for Cisco SD-WAN Manager (vManage) introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package vmanage

import (
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.osirisjson.org/producers/pkg/sdk"
	"go.osirisjson.org/producers/pkg/testharness"
)

// goldenRoot holds one directory per scenario, relative to this package
// directory.
const goldenRoot = "../../../../testdata/osiris/network/cisco/vmanage"

// goldenScenario is one scenario directory under goldenRoot and the
// site name its single emitted document is written under.
type goldenScenario struct{ dir, site string }

// goldenScenarios are the scenarios under goldenRoot: one control-plane
// site holding a Manager, a Controller and a Validator.
var goldenScenarios = []goldenScenario{{dir: "lab_sdwan", site: "MXP"}}

// goldenTimestamp and goldenCluster replace the wall-clock timestamp
// and the dial host in every golden document.
const (
	goldenTimestamp = "2026-11-13T12:00:00Z"
	goldenCluster   = "lab-manager01.example.com"
)

// emptyList is the body served for an endpoint without a fixture file:
// a valid list response with no entries.
const emptyList = `{"data":[]}`

// goldenServer serves the scenario in dir over HTTPS. A request for
// /dataservice/<path> is answered with api/<path>.json, where a
// deviceId or state query parameter becomes a last path segment:
// /dataservice/device/interface?deviceId=192.0.2.1 is served from
// api/device/interface/192.0.2.1.json and /dataservice/device/omp/links?state=up
// from api/device/omp/links/up.json.
// An endpoint without a fixture file is answered with an empty list.
// Every fixture is read before the server starts, so a request only
// selects one by name and never reaches the file system.
func goldenServer(t *testing.T, dir string) *httptest.Server {
	t.Helper()
	bodies := loadAPIBodies(t, filepath.Join(dir, "api"))
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/j_security_check":
			w.WriteHeader(http.StatusOK)
			return
		case "/dataservice/client/token":
			_, _ = io.WriteString(w, `{"token":"test"}`)
			return
		}
		rel, ok := strings.CutPrefix(r.URL.Path, "/dataservice/")
		if !ok || rel == "" {
			t.Errorf("unexpected request %s", r.URL)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		query := r.URL.Query()
		for key := range query {
			if key != "deviceId" && key != "state" {
				t.Errorf("unexpected query parameter %q in %s", key, r.URL)
			}
		}
		if v := query.Get("deviceId"); v != "" {
			rel += "/" + v
		}
		if v := query.Get("state"); v != "" {
			rel += "/" + v
		}
		w.Header().Set("Content-Type", "application/json")
		body, ok := bodies[rel]
		if !ok {
			_, _ = io.WriteString(w, emptyList)
			return
		}
		_, _ = w.Write(body)
	}))
}

// loadAPIBodies reads every .json file under apiDir, keyed by its
// slash-separated path relative to apiDir without the extension:
// api/device/interface/192.0.2.1.json is "device/interface/192.0.2.1".
func loadAPIBodies(t *testing.T, apiDir string) map[string][]byte {
	t.Helper()
	bodies := map[string][]byte{}
	err := filepath.WalkDir(apiDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".json" {
			return err
		}
		rel, err := filepath.Rel(apiDir, path)
		if err != nil {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		bodies[strings.TrimSuffix(filepath.ToSlash(rel), ".json")] = body
		return nil
	})
	if err != nil {
		t.Fatalf("api: %v", err)
	}
	return bodies
}

// collectGolden runs runExport for one scenario and purpose and
// returns its single document with the timestamp and cluster pinned.
func collectGolden(t *testing.T, sc goldenScenario, purpose string) *sdk.Document {
	t.Helper()
	ts := goldenServer(t, filepath.Join(goldenRoot, sc.dir))
	defer ts.Close()

	host, port := splitHostPort(t, ts.Listener.Addr().String())
	cfg := &Config{
		Host:              host,
		Port:              port,
		Username:          "admin",
		Password:          "changeme",
		OutputDir:         t.TempDir(),
		SafeFailureMode:   sdk.FailClosed,
		Purpose:           purpose,
		InsecureTLS:       true,
		AllSites:          true,
		SiteNameRateLimit: 1000,
	}
	if err := runExport(cfg); err != nil {
		t.Fatalf("runExport(%s, %s): %v", sc.dir, purpose, err)
	}

	siteDir := filepath.Join(cfg.OutputDir, sc.site)
	data, err := os.ReadFile(filepath.Join(siteDir, singleFileIn(t, siteDir)))
	if err != nil {
		t.Fatal(err)
	}
	var doc sdk.Document
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// The comparison below runs on the re-marshaled document, so the
	// round trip itself must not change a byte.
	again, err := sdk.MarshalDocument(&doc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(again, data) {
		t.Fatal("document does not survive an unmarshal and marshal round trip unchanged")
	}

	if doc.Metadata.Scope == nil || len(doc.Metadata.Scope.Clusters) != 1 || doc.Metadata.Scope.Clusters[0] != host {
		t.Fatalf("metadata.scope.clusters = %v, want [%q]", doc.Metadata.Scope, host)
	}
	doc.Metadata.Scope.Clusters = []string{goldenCluster}
	doc.Metadata.Timestamp = goldenTimestamp
	return &doc
}

// goldenPath is the expected document of sc at purpose.
func goldenPath(sc goldenScenario, purpose string) string {
	return filepath.Join(goldenRoot, sc.dir, "purpose", "golden_"+purpose+".json")
}

func TestGolden(t *testing.T) {
	for _, sc := range goldenScenarios {
		for _, purpose := range []string{"documentation", "audit"} {
			t.Run(sc.dir+"/"+purpose, func(t *testing.T) {
				testharness.AssertGolden(t, collectGolden(t, sc, purpose), goldenPath(sc, purpose))
			})
		}
	}
}

// auditOnlyKeys are the members TransformDevices emits at audit
// purpose only.
var auditOnlyKeys = []string{`"serial_number"`, `"certificate_validity"`}

func TestGolden_PurposeContract(t *testing.T) {
	for _, sc := range goldenScenarios {
		doc := testharness.LoadFixture(t, goldenPath(sc, "documentation"))
		audit := testharness.LoadFixture(t, goldenPath(sc, "audit"))
		for _, key := range auditOnlyKeys {
			if bytes.Contains(doc, []byte(key)) {
				t.Errorf("%s documentation golden carries audit-only %s", sc.dir, key)
			}
			if !bytes.Contains(audit, []byte(key)) {
				t.Errorf("%s audit golden carries no %s", sc.dir, key)
			}
		}
	}
}

func TestGolden_GraphIntegrity(t *testing.T) {
	for _, sc := range goldenScenarios {
		for _, purpose := range []string{"documentation", "audit"} {
			var doc sdk.Document
			if err := json.Unmarshal(testharness.LoadFixture(t, goldenPath(sc, purpose)), &doc); err != nil {
				t.Fatalf("%s/%s: %v", sc.dir, purpose, err)
			}
			ids := make(map[string]bool, len(doc.Topology.Resources))
			for _, r := range doc.Topology.Resources {
				ids[r.ID] = true
			}
			for _, c := range doc.Topology.Connections {
				if !ids[c.Source] || !ids[c.Target] {
					t.Errorf("%s/%s: connection %s has an endpoint that is not an emitted resource", sc.dir, purpose, c.ID)
				}
			}
			for _, g := range doc.Topology.Groups {
				for _, m := range g.Members {
					if !ids[m] {
						t.Errorf("%s/%s: group %s member %s is not an emitted resource", sc.dir, purpose, g.ID, m)
					}
				}
			}
		}
	}
}
