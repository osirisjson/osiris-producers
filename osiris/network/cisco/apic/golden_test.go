// golden_test.go - Source-to-golden tests for the Cisco ACI/APIC OSIRIS
// JSON Producer.
//
// Drives Producer.Collect end to end (APIC REST responses -> transform
// -> wiring -> DocumentBuilder.Build -> MarshalDocument) against an
// in-memory APIC that serves one synthetic class response per discovery
// operation and compares the emitted document byte for byte with the
// expected document at both --purpose flag levels.
//
// Fixtures live under testdata/osiris/network/cisco/apic/ at the
// repository root: one snake_case directory per scenario, holding the
// APIC response body per class (api/<class>.json, an imdata envelope),
// an optional failures.json (class -> HTTP status the query fails with)
// and the expected document per purpose (purpose/golden_<purpose>.json).
// Their provenance and coverage are documented in that directory's
// README.md. Regenerate the expected documents with
// OSIRIS_UPDATE_GOLDEN=1 and review the diff: they are the producer's
// output contract.
//
// The audit-purpose expected document is produced with
// --include-raw-body, so it also pins the raw attribute fallback;
// TestGolden_RawIsAdditive proves that removing the raw extension keys
// from it yields exactly the audit document without the flag.
//
// TestGolden_OrderIndependent serves every class with its elements in
// reverse order and requires the identical document, so output never
// depends on APIC row order.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package apic

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.osirisjson.org/producers/osiris/network/cisco/run"
	"go.osirisjson.org/producers/pkg/sdk"
	"go.osirisjson.org/producers/pkg/testharness"
)

// goldenRoot holds one directory per scenario, relative to this package
// directory.
const goldenRoot = "../../../../testdata/osiris/network/cisco/apic"

// goldenScenarios are the scenario directories under goldenRoot: a
// complete single-pod fabric (controllers, spines, leaves, tenants,
// physical topology, endpoints and faults) and the same fabric with
// failed, malformed and unresolvable optional data.
var goldenScenarios = []string{"lab_fabric", "lab_fabric_partial"}

// goldenClock is the fixed build time stamped into every golden
// document.
var goldenClock = func() time.Time { return time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC) }

// goldenServer serves the scenario in dir. A class without a fixture
// file is answered with a valid empty imdata; a class listed in
// failures.json fails with that HTTP status. With reverse set, every
// class response lists its elements in reverse order.
func goldenServer(t *testing.T, dir string, reverse bool) *httptest.Server {
	t.Helper()
	failures := map[string]int{}
	if data, err := os.ReadFile(filepath.Join(dir, "failures.json")); err == nil {
		if err := json.Unmarshal(data, &failures); err != nil {
			t.Fatalf("failures.json: %v", err)
		}
	}
	fixtures := map[string][]byte{}
	entries, err := os.ReadDir(filepath.Join(dir, "api"))
	if err != nil {
		t.Fatalf("api: %v", err)
	}
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, "api", e.Name()))
		if err != nil {
			t.Fatalf("api/%s: %v", e.Name(), err)
		}
		fixtures[name] = data
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		class, ok := strings.CutPrefix(r.URL.Path, "/api/class/")
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		class = strings.TrimSuffix(class, ".json")
		if code, fail := failures[class]; fail {
			w.WriteHeader(code)
			_, _ = io.WriteString(w, `{"imdata":[]}`)
			return
		}
		body, found := fixtures[class]
		if !found {
			_, _ = io.WriteString(w, `{"totalCount":"0","imdata":[]}`)
			return
		}
		if reverse {
			var env map[string]any
			if err := json.Unmarshal(body, &env); err != nil {
				t.Errorf("%s: %v", class, err)
				return
			}
			if items, ok := env["imdata"].([]any); ok {
				for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
					items[i], items[j] = items[j], items[i]
				}
			}
			body, _ = json.Marshal(env)
		}
		_, _ = w.Write(body)
	}))
}

// collectGolden runs Collect for one scenario and purpose with the
// golden clock and returns the document. At audit purpose
// --include-raw-body is set.
func collectGolden(t *testing.T, scenario, purpose string, reverse bool) *sdk.Document {
	t.Helper()
	return collectGoldenRaw(t, scenario, purpose, reverse, purpose == "audit")
}

// collectGoldenRaw is collectGolden with --include-raw-body set as
// given.
func collectGoldenRaw(t *testing.T, scenario, purpose string, reverse, raw bool) *sdk.Document {
	t.Helper()
	ts := goldenServer(t, filepath.Join(goldenRoot, scenario), reverse)
	defer ts.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	producer := &Producer{
		target: run.TargetConfig{Host: "192.0.2.1", Hostname: "LAB-APIC1"},
		cfg:    &Config{Purpose: purpose},
		ctx:    context.Background(),
		client: &Client{baseURL: ts.URL, httpClient: ts.Client(), logger: logger, ctx: context.Background(), retryBase: time.Millisecond},
	}
	ctx := testharness.NewTestContext(t,
		testharness.WithConfig(&sdk.ProducerConfig{Purpose: purpose, SafeFailureMode: sdk.FailClosed, IncludeRawBody: raw}),
		testharness.WithClock(goldenClock))
	ctx.Logger = logger
	doc, err := producer.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect(%s, %s): %v", scenario, purpose, err)
	}
	return doc
}

func TestGolden(t *testing.T) {
	for _, scenario := range goldenScenarios {
		for _, purpose := range []string{"documentation", "audit"} {
			t.Run(scenario+"/"+purpose, func(t *testing.T) {
				doc := collectGolden(t, scenario, purpose, false)
				testharness.AssertGolden(t, doc,
					filepath.Join(goldenRoot, scenario, "purpose", "golden_"+purpose+".json"))
			})
		}
	}
}

func TestGolden_OrderIndependent(t *testing.T) {
	for _, scenario := range goldenScenarios {
		for _, purpose := range []string{"documentation", "audit"} {
			t.Run(scenario+"/"+purpose, func(t *testing.T) {
				forward, err := sdk.MarshalDocument(collectGolden(t, scenario, purpose, false))
				if err != nil {
					t.Fatal(err)
				}
				reversed, err := sdk.MarshalDocument(collectGolden(t, scenario, purpose, true))
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(forward, reversed) {
					t.Error("document depends on APIC row order")
				}
			})
		}
	}
}

// TestGolden_GraphIntegrity proves, on every golden document: every
// connection endpoint and group member is an emitted resource, every
// group child is an emitted group, no group is its own ancestor and
// documentation purpose carries no endpoint resources.
func TestGolden_GraphIntegrity(t *testing.T) {
	for _, scenario := range goldenScenarios {
		for _, purpose := range []string{"documentation", "audit"} {
			t.Run(scenario+"/"+purpose, func(t *testing.T) {
				doc := collectGolden(t, scenario, purpose, false)
				resources := map[string]string{}
				for _, r := range doc.Topology.Resources {
					resources[r.ID] = r.Type
				}
				for _, c := range doc.Topology.Connections {
					if resources[c.Source] == "" || resources[c.Target] == "" {
						t.Errorf("connection %s has a non-resource endpoint: %s -> %s", c.ID, c.Source, c.Target)
					}
				}
				children := map[string][]string{}
				for _, g := range doc.Topology.Groups {
					for _, m := range g.Members {
						if resources[m] == "" {
							t.Errorf("group %s member %s is not a resource", g.ID, m)
						}
					}
					children[g.ID] = g.Children
				}
				for _, g := range doc.Topology.Groups {
					for _, ch := range g.Children {
						if _, ok := children[ch]; !ok {
							t.Errorf("group %s child %s is not a group", g.ID, ch)
						}
					}
					if reachesGroup(g.ID, g.ID, children, map[string]bool{}) {
						t.Errorf("group %s is its own ancestor", g.ID)
					}
				}
				if purpose == "documentation" {
					for id, typ := range resources {
						if typ == "network.endpoint" || strings.Contains(id, "/cep-") {
							t.Errorf("documentation purpose emits endpoint %s", id)
						}
					}
				}
			})
		}
	}
}

// reachesGroup reports whether target is reachable from through
// group children.
func reachesGroup(from, target string, children map[string][]string, seen map[string]bool) bool {
	for _, ch := range children[from] {
		if ch == target {
			return true
		}
		if !seen[ch] {
			seen[ch] = true
			if reachesGroup(ch, target, children, seen) {
				return true
			}
		}
	}
	return false
}

// TestGolden_RawIsAdditive proves: --include-raw-body only adds the
// raw, raw_unowned and raw_omitted keys to osiris.cisco extensions;
// with them removed the document is byte-identical to the audit
// document without the flag.
func TestGolden_RawIsAdditive(t *testing.T) {
	strip := func(ext map[string]any) {
		c, ok := ext[extensionNamespace].(map[string]any)
		if !ok {
			return
		}
		delete(c, "raw")
		delete(c, "raw_unowned")
		delete(c, "raw_omitted")
		if len(c) == 0 {
			delete(ext, extensionNamespace)
		}
	}
	for _, scenario := range goldenScenarios {
		t.Run(scenario, func(t *testing.T) {
			withRaw := collectGoldenRaw(t, scenario, "audit", false, true)
			for i := range withRaw.Topology.Resources {
				strip(withRaw.Topology.Resources[i].Extensions)
				if len(withRaw.Topology.Resources[i].Extensions) == 0 {
					withRaw.Topology.Resources[i].Extensions = nil
				}
			}
			for i := range withRaw.Topology.Groups {
				strip(withRaw.Topology.Groups[i].Extensions)
				if len(withRaw.Topology.Groups[i].Extensions) == 0 {
					withRaw.Topology.Groups[i].Extensions = nil
				}
			}
			a, err := sdk.MarshalDocument(withRaw)
			if err != nil {
				t.Fatal(err)
			}
			b, err := sdk.MarshalDocument(collectGoldenRaw(t, scenario, "audit", false, false))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(a, b) {
				t.Error("--include-raw-body changed more than the raw extension keys")
			}
		})
	}
}
