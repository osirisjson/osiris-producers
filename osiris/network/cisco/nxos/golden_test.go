// golden_test.go - Source-to-golden test for the Cisco NX-OS OSIRIS
// JSON Producer.
//
// Drives Producer.Collect end to end, login included, against an
// in-memory NX-API that answers JSON-RPC style "cli_show" requests on
// /ins with synthetic command bodies and compares the emitted document
// byte for byte with the expected document, at both --purpose flag
// levels.
//
// Fixtures live under testdata/osiris/network/cisco/nxos/ at the
// repository root: one snake_case directory per scenario, holding the
// NX-API "body" of each show command (nxapi/<command>.json, the command
// in snake_case), the commands the device rejects with their NX-API
// code and message (failures.json) and the expected document per
// purpose (purpose/golden_<purpose>.json). Their provenance, value
// sources and deliberate omissions are documented in that directory's
// README.md. Regenerate the expected documents with
// OSIRIS_UPDATE_GOLDEN=1 and review the diff: they are the producer's
// output contract.
//
// TestGolden_FixturesMatchCommands proves every command Collect issues
// has exactly one fixture file or failure entry and no fixture is
// left unused.
//
// TestGolden_PurposeContract proves the documentation-purpose goldens
// carry none of the audit-only data and the audit-purpose goldens do.
//
// TestGolden_GraphIntegrity proves every connection endpoint and every
// group member of each golden document is an emitted resource.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package nxos

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"go.osirisjson.org/producers/osiris/network/cisco/run"
	"go.osirisjson.org/producers/pkg/sdk"
	"go.osirisjson.org/producers/pkg/testharness"
)

// goldenRoot holds one directory per scenario, relative to this package
// directory.
const goldenRoot = "../../../../testdata/osiris/network/cisco/nxos"

// goldenScenario is one scenario directory under goldenRoot and the
// address the device is collected from. No hostname is configured, as
// in a single-host run, so the producer derives it from the address.
type goldenScenario struct{ dir, host string }

// goldenScenarios are the scenarios under goldenRoot: a Nexus 9300v
// with trunk, access and routed ports, SVIs and loopbacks across
// thirteen VRFs, one BGP neighbor and three rejected commands.
var goldenScenarios = []goldenScenario{{dir: "n9k_c9300v", host: "203.0.113.200"}}

// goldenClock is the fixed build time stamped into every golden
// document.
var goldenClock = func() time.Time { return time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC) }

// nxapiFailure is one failures.json entry: the NX-API output code and
// message a rejected command is answered with.
type nxapiFailure struct {
	Code string `json:"code"`
	Msg  string `json:"msg"`
}

// fixtureName is the nxapi/ file name of command.
func fixtureName(command string) string {
	return strings.NewReplacer(" ", "_", "-", "_").Replace(command) + ".json"
}

// goldenDevice is a scenario's synthetic NX-API: the command bodies,
// the rejected commands and every command it has been asked for.
type goldenDevice struct {
	bodies   map[string]json.RawMessage
	failures map[string]nxapiFailure

	mu     sync.Mutex
	issued []string
}

// loadGoldenDevice reads the scenario in dir.
func loadGoldenDevice(t *testing.T, dir string) *goldenDevice {
	t.Helper()
	d := &goldenDevice{bodies: map[string]json.RawMessage{}, failures: map[string]nxapiFailure{}}
	if err := json.Unmarshal(testharness.LoadFixture(t, filepath.Join(dir, "failures.json")), &d.failures); err != nil {
		t.Fatalf("failures.json: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "nxapi"))
	if err != nil {
		t.Fatalf("nxapi: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		d.bodies[e.Name()] = testharness.LoadFixture(t, filepath.Join(dir, "nxapi", e.Name()))
	}
	return d
}

// output answers one command: its failure entry, or its body with code
// 200. A command with neither fails the test.
func (d *goldenDevice) output(t *testing.T, command string) map[string]any {
	d.mu.Lock()
	d.issued = append(d.issued, command)
	d.mu.Unlock()
	if f, ok := d.failures[command]; ok {
		return map[string]any{"code": f.Code, "msg": f.Msg, "clierror": f.Msg + "\n"}
	}
	body, ok := d.bodies[fixtureName(command)]
	if !ok {
		t.Errorf("no fixture for %q", command)
		return map[string]any{"code": "400", "msg": "Input CLI command error"}
	}
	return map[string]any{"code": "200", "msg": "Success", "body": body}
}

// serve returns an NX-API server for d. A single-command request is
// answered with one output object, a multi-command request with an
// array, as NX-API does.
func (d *goldenDevice) serve(t *testing.T) *httptest.Server {
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/ins" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var req struct {
			InsAPI struct {
				Type  string `json:"type"`
				Input string `json:"input"`
			} `json:"ins_api"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.InsAPI.Type != "cli_show" {
			t.Errorf("malformed NX-API request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var outputs []map[string]any
		for command := range strings.SplitSeq(req.InsAPI.Input, " ;") {
			outputs = append(outputs, d.output(t, strings.TrimSpace(command)))
		}
		var output any = outputs
		if len(outputs) == 1 {
			output = outputs[0]
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ins_api": map[string]any{
			"type": "cli_show", "version": "1.2", "sid": "eoc",
			"outputs": map[string]any{"output": output},
		}})
	}))
}

// collectGolden logs in to the scenario's NX-API and runs Collect at
// purpose with the golden clock. It returns the document and the
// device, which records the commands issued.
func collectGolden(t *testing.T, sc goldenScenario, purpose string) (*sdk.Document, *goldenDevice) {
	t.Helper()
	device := loadGoldenDevice(t, filepath.Join(goldenRoot, sc.dir))
	ts := device.serve(t)
	defer ts.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client := &Client{baseURL: ts.URL, httpClient: ts.Client(), logger: logger}
	if err := client.Login("admin", "changeme"); err != nil {
		t.Fatalf("Login: %v", err)
	}
	producer := &Producer{
		target: run.TargetConfig{Host: sc.host, Username: "admin", Password: "changeme"},
		cfg:    &Config{Purpose: purpose},
		client: client,
	}
	ctx := testharness.NewTestContext(t,
		testharness.WithConfig(&sdk.ProducerConfig{Purpose: purpose, SafeFailureMode: sdk.FailClosed}),
		testharness.WithClock(goldenClock))
	ctx.Logger = logger
	doc, err := producer.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect(%s, %s): %v", sc.dir, purpose, err)
	}
	return doc, device
}

// goldenPath is the expected document of sc at purpose.
func goldenPath(sc goldenScenario, purpose string) string {
	return filepath.Join(goldenRoot, sc.dir, "purpose", "golden_"+purpose+".json")
}

func TestGolden(t *testing.T) {
	for _, sc := range goldenScenarios {
		for _, purpose := range []string{"documentation", "audit"} {
			t.Run(sc.dir+"/"+purpose, func(t *testing.T) {
				doc, _ := collectGolden(t, sc, purpose)
				testharness.AssertGolden(t, doc, goldenPath(sc, purpose))
			})
		}
	}
}

func TestGolden_FixturesMatchCommands(t *testing.T) {
	for _, sc := range goldenScenarios {
		_, device := collectGolden(t, sc, "audit")
		used := map[string]bool{}
		for _, command := range device.issued {
			used[fixtureName(command)] = true
			if _, failed := device.failures[command]; failed {
				if _, both := device.bodies[fixtureName(command)]; both {
					t.Errorf("%s: %q has both a fixture file and a failure entry", sc.dir, command)
				}
			}
		}
		for name := range device.bodies {
			if !used[name] {
				t.Errorf("%s: nxapi/%s is never requested", sc.dir, name)
			}
		}
		for command := range device.failures {
			if !slices.Contains(device.issued, command) {
				t.Errorf("%s: failures.json entry %q is never requested", sc.dir, command)
			}
		}
	}
}

// auditOnly are members only the audit-purpose documents carry:
// inventory serials, interface detail, the AAA resource and BGP.
var auditOnly = []string{`"serial":`, `"mtu":`, `"osiris.cisco.aaa"`, `"network.bgp"`}

func TestGolden_PurposeContract(t *testing.T) {
	for _, sc := range goldenScenarios {
		doc := testharness.LoadFixture(t, goldenPath(sc, "documentation"))
		audit := testharness.LoadFixture(t, goldenPath(sc, "audit"))
		for _, key := range auditOnly {
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
