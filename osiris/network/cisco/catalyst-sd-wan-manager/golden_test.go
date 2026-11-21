// golden_test.go - Source-to-golden test for the Cisco Catalyst SD-WAN
// Manager OSIRIS JSON Producer.
//
// Drives runExport end to end, login included, against an in-memory
// HTTPS SD-WAN Manager that serves synthetic /dataservice response
// bodies and compares the emitted per-site document byte for byte with
// the expected document, at both --purpose flag levels.
//
// Fixtures live under
// testdata/osiris/network/cisco/catalyst-sd-wan-manager/ at the
// repository root: one snake_case directory per scenario, holding one
// response body per endpoint (api/<path>.json, see goldenServer for the
// path mapping) and the expected document per purpose
// (purpose/golden_<purpose>.json). Their provenance, value sources and
// deliberate omissions are documented in that directory's README.md.
// Regenerate the expected documents with OSIRIS_UPDATE_GOLDEN=1 and
// review the diff: they are the producer's output contract.
//
// runExport stamps the wall clock into metadata.timestamp and the dial
// host into metadata.scope.clusters and every resource's
// provider.system. The test checks that the dial host is the only
// value, then pins these fields to fixed values
// before the comparison, so the documents stay reproducible and carry
// no loopback address. Every other byte is the producer's own output.
//
// TestGolden_PurposeContract proves the documentation-purpose goldens
// carry no audit-only field and the audit-purpose goldens do.
//
// TestGolden_GraphIntegrity proves every connection endpoint and every
// group member of each golden document is an emitted resource.
//
// OSIRIS JSON Producer for Cisco Catalyst SD-WAN Manager introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package catalystsdwanmanager

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
const goldenRoot = "../../../../testdata/osiris/network/cisco/catalyst-sd-wan-manager"

// goldenScenario is one site document of a scenario directory under
// goldenRoot: the site name it is written under and, for a scenario
// with several sites, the name prefix of its expected documents.
type goldenScenario struct{ dir, site, name string }

// goldenScenarios are the scenarios under goldenRoot: lab_sdwan, one
// control-plane site holding a Manager, a Controller and a Validator;
// lab_cross_site, two WAN edge sites and a controller site joined by a
// cross-site tunnel and OMP sessions, one document per site;
// contract_<release>, the same fabric shape built from each documented
// release's example responses (see contract_test.go).
var goldenScenarios = []goldenScenario{
	{dir: "lab_sdwan", site: "MXP"},
	{dir: "lab_cross_site", site: "BRANCH-00", name: "branch_00"},
	{dir: "lab_cross_site", site: "OFFICE-00", name: "office_00"},
	{dir: "lab_cross_site", site: "MXP", name: "mxp"},
	{dir: "contract_20_18", site: "BRANCH-00", name: "branch_00"},
	{dir: "contract_26_1", site: "BRANCH-00", name: "branch_00"},
	{dir: "contract_26_2", site: "BRANCH-00", name: "branch_00"},
}

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
	for i := range doc.Topology.Resources {
		p := &doc.Topology.Resources[i].Provider
		if p.System != host {
			t.Fatalf("resource %s provider.system = %q, want the dial host", doc.Topology.Resources[i].ID, p.System)
		}
		p.System = goldenCluster
	}
	return &doc
}

// goldenPath is the expected document of sc at purpose.
func goldenPath(sc goldenScenario, purpose string) string {
	if sc.name != "" {
		return filepath.Join(goldenRoot, sc.dir, "purpose", sc.name+"_golden_"+purpose+".json")
	}
	return filepath.Join(goldenRoot, sc.dir, "purpose", "golden_"+purpose+".json")
}

func TestGolden(t *testing.T) {
	for _, sc := range goldenScenarios {
		for _, purpose := range []string{"documentation", "audit"} {
			t.Run(sc.dir+"/"+sc.site+"/"+purpose, func(t *testing.T) {
				testharness.AssertGolden(t, collectGolden(t, sc, purpose), goldenPath(sc, purpose))
			})
		}
	}
}

// auditOnlyKeys are members the audit golden must carry and the
// documentation golden must not (see purpose.go).
var auditOnlyKeys = []string{
	`"serial_number"`, `"certificate_validity"`, `"ip_addresses"`, `"mac_address"`,
	`"system_ip"`, `"geo_location"`, `"device_groups"`, `"up_since"`,
}

// TestGolden_PurposeContract proves the documentation goldens carry
// nothing outside the purpose.go allowlist, and that the audit goldens
// still carry the audit-only fields and the same graph.
func TestGolden_PurposeContract(t *testing.T) {
	seenInAudit := map[string]bool{}
	for _, sc := range goldenScenarios {
		docBytes := testharness.LoadFixture(t, goldenPath(sc, "documentation"))
		auditBytes := testharness.LoadFixture(t, goldenPath(sc, "audit"))
		for _, key := range auditOnlyKeys {
			if bytes.Contains(docBytes, []byte(key)) {
				t.Errorf("%s/%s documentation golden carries audit-only %s", sc.dir, sc.site, key)
			}
			if bytes.Contains(auditBytes, []byte(key)) {
				seenInAudit[key] = true
			}
		}

		var doc, audit sdk.Document
		if err := json.Unmarshal(docBytes, &doc); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(auditBytes, &audit); err != nil {
			t.Fatal(err)
		}
		check := func(kind, id string, props, ext map[string]any, al fieldAllowlist) {
			for k := range props {
				if !al.properties[k] {
					t.Errorf("%s documentation %s %s: properties.%s is not allowlisted", sc.dir, kind, id, k)
				}
			}
			for ns, v := range ext {
				m, _ := v.(map[string]any)
				if ns != extensionKey {
					t.Errorf("%s documentation %s %s: foreign extension namespace %s", sc.dir, kind, id, ns)
				}
				for k := range m {
					if !al.extension[k] {
						t.Errorf("%s documentation %s %s: extension %s is not allowlisted", sc.dir, kind, id, k)
					}
				}
			}
		}
		for _, r := range doc.Topology.Resources {
			check("resource", r.ID, r.Properties, r.Extensions, resourceAllowlist(r.Type))
		}
		for _, c := range doc.Topology.Connections {
			check("connection", c.ID, c.Properties, c.Extensions, documentationConnection)
		}
		for _, g := range doc.Topology.Groups {
			check("group", g.ID, g.Properties, g.Extensions, documentationGroup)
		}

		if len(doc.Topology.Resources) != len(audit.Topology.Resources) ||
			len(doc.Topology.Connections) != len(audit.Topology.Connections) ||
			len(doc.Topology.Groups) != len(audit.Topology.Groups) {
			t.Errorf("%s: documentation and audit goldens differ in graph size", sc.dir)
		}
	}
	for _, key := range auditOnlyKeys {
		if !seenInAudit[key] {
			t.Errorf("no audit golden carries %s", key)
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
