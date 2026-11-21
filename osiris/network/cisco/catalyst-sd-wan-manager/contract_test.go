// contract_test.go - Per-release API contract tests (20.18, 26.1, 26.2)
// for the Cisco Catalyst SD-WAN Manager OSIRIS JSON Producer.
//
// Each testdata contract_<release> scenario answers every endpoint the
// producer calls with bodies that carry exactly the keys of that
// release's documented example response (the 26.2 interface rows add
// fec-mode), with synthetic values. The inventory mixes the example's
// typing (integer timestamps) with the published schema's (string
// timestamps and longitude). For every release, every site document
// must be complete, keep the cross-site tunnel and the OMP sessions,
// resolve every endpoint, and describe the same topology as every
// other release: the documented contract changes between these
// releases do not change the emitted graph.
//
// OSIRIS JSON Producer for Cisco Catalyst SD-WAN Manager introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification
package catalystsdwanmanager

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"go.osirisjson.org/producers/pkg/sdk"
)

var contractReleases = []string{"contract_20_18", "contract_26_1", "contract_26_2"}

// contractExport runs the full export against one release scenario and
// returns its documents by site name.
func contractExport(t *testing.T, dir, purpose string) map[string]sdk.Document {
	t.Helper()
	ts := goldenServer(t, filepath.Join(goldenRoot, dir))
	defer ts.Close()
	host, port := splitHostPort(t, ts.Listener.Addr().String())
	cfg := &Config{
		Host: host, Port: port, Username: "admin", Password: "changeme",
		OutputDir: t.TempDir(), SafeFailureMode: sdk.FailClosed, Purpose: purpose,
		InsecureTLS: true, AllSites: true, SiteNameRateLimit: 1000, RequireComplete: true,
	}
	if err := runExport(cfg); err != nil {
		t.Fatalf("%s: runExport: %v", dir, err)
	}
	docs := map[string]sdk.Document{}
	for _, p := range writtenFiles(t, cfg.OutputDir) {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		var d sdk.Document
		if err := json.Unmarshal(data, &d); err != nil {
			t.Fatal(err)
		}
		docs[d.Metadata.Scope.Sites[0]] = d
	}
	return docs
}

// graphShape is the release-independent view of a document: resource
// IDs and types, connection IDs with endpoints, and group members.
func graphShape(d sdk.Document) []string {
	var out []string
	for _, r := range d.Topology.Resources {
		out = append(out, "r "+r.ID+" "+r.Type)
	}
	for _, c := range d.Topology.Connections {
		out = append(out, "c "+c.ID+" "+c.Source+" "+c.Target)
	}
	for _, g := range d.Topology.Groups {
		for _, m := range g.Members {
			out = append(out, "g "+g.ID+" "+m)
		}
	}
	sort.Strings(out)
	return out
}

func TestContract_EveryReleaseYieldsTheSameCompleteGraph(t *testing.T) {
	for _, purpose := range []string{"documentation", "audit"} {
		var reference map[string][]string
		for _, rel := range contractReleases {
			docs := contractExport(t, rel, purpose)
			if len(docs) != 3 {
				t.Fatalf("%s/%s: %d documents, want 3", rel, purpose, len(docs))
			}
			shapes := map[string][]string{}
			for site, d := range docs {
				meta := extOf(d.Metadata.Extensions)
				if meta["complete"] != true {
					t.Errorf("%s/%s/%s: document is partial: %v", rel, purpose, site, meta["coverage"])
				}
				graph := meta["graph"].(map[string]any)
				for k, v := range graph {
					if k != "neighbor_devices" && k != "neighbor_interfaces" && v != float64(0) {
						t.Errorf("%s/%s/%s: graph %s = %v, want 0", rel, purpose, site, k, v)
					}
				}
				shapes[site] = graphShape(d)
			}
			for _, site := range []string{"BRANCH-00", "OFFICE-00"} {
				d := docs[site]
				if n := len(connsOfType(d, "network.vpn")); n != 1 {
					t.Errorf("%s/%s/%s: %d tunnels, want 1", rel, purpose, site, n)
				}
				if n := len(connsOfType(d, "network")); n != 1 {
					t.Errorf("%s/%s/%s: %d OMP sessions, want 1", rel, purpose, site, n)
				}
			}
			if reference == nil {
				reference = shapes
			} else if !reflect.DeepEqual(shapes, reference) {
				t.Errorf("%s/%s: graph differs from %s", rel, purpose, contractReleases[0])
			}
		}
	}
}
