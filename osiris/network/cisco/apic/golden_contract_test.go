// golden_contract_test.go - Cross-run and fixture contract tests for
// the Cisco ACI/APIC OSIRIS JSON Producer.
//
// TestGolden_ManifestMatchesFixtures keeps each scenario's provenance
// manifest in step with its fixture files.
//
// TestGolden_StableUnderStateChange serves a scenario whose
// operational state changed between two runs (vPC leg and peer state,
// port and port-channel state, link state, relation state) and
// requires the same resource, connection and group IDs.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package apic

import (
	"bytes"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"go.osirisjson.org/producers/pkg/sdk"
)

// stateChanges maps a class to the operational attributes rewritten
// for the second run and the value each takes.
var stateChanges = map[string]map[string]string{
	"vpcIf":        {"localOperSt": "down", "remoteOperSt": "down"},
	"vpcDom":       {"peerSt": "down", "operRole": "none-established", "peerStQual": "peer-not-reachable"},
	"ethpmPhysIf":  {"operSt": "down", "operSpeed": "unknown"},
	"pcAggrIf":     {"activePorts": "0"},
	"fabricLink":   {"linkState": "failed"},
	"fvRsPathAtt":  {"state": "missing-target"},
	"pcRsMbrIfs":   {"state": "missing-target"},
	"topSystem":    {"systemUpTime": "00:00:00:01.000"},
	"vpcRsVpcConf": {"state": "missing-target"},
}

// copyWithStateChange copies the scenario in src to a temporary
// directory with stateChanges applied to its class responses.
func copyWithStateChange(t *testing.T, src string) string {
	t.Helper()
	dst := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dst, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(src, "failures.json")); err == nil {
		if err := os.WriteFile(filepath.Join(dst, "failures.json"), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(src, "api"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(src, "api", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		class := e.Name()[:len(e.Name())-len(".json")]
		if changes, ok := stateChanges[class]; ok {
			var env struct {
				TotalCount string                                 `json:"totalCount"`
				Imdata     []map[string]map[string]map[string]any `json:"imdata"`
			}
			if err := json.Unmarshal(data, &env); err != nil {
				t.Fatalf("%s: %v", e.Name(), err)
			}
			for _, obj := range env.Imdata {
				attrs := obj[class]["attributes"]
				for k, v := range changes {
					if _, present := attrs[k]; present {
						attrs[k] = v
					}
				}
			}
			if data, err = json.Marshal(env); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(dst, "api", e.Name()), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dst
}

// documentIDs returns the sorted-independent ID sets of a document.
func documentIDs(doc *sdk.Document) map[string]bool {
	ids := map[string]bool{}
	for _, r := range doc.Topology.Resources {
		ids["resource "+r.ID] = true
	}
	for _, c := range doc.Topology.Connections {
		ids["connection "+c.ID] = true
	}
	for _, g := range doc.Topology.Groups {
		ids["group "+g.ID] = true
	}
	return ids
}

func TestGolden_StableUnderStateChange(t *testing.T) {
	for _, scenario := range goldenScenarios {
		// Audit purpose: every port is emitted, so a port going down
		// cannot change the bounded documentation port set.
		t.Run(scenario, func(t *testing.T) {
			baseDoc := collectGolden(t, scenario, "audit", false)
			changedDoc := collectScenarioDir(t, copyWithStateChange(t, filepath.Join(goldenRoot, scenario)), "audit", false, true)
			a, errA := sdk.MarshalDocument(baseDoc)
			b, errB := sdk.MarshalDocument(changedDoc)
			if errA != nil || errB != nil || bytes.Equal(a, b) {
				t.Fatalf("the state change did not change the document (%v, %v)", errA, errB)
			}
			base, changed := documentIDs(baseDoc), documentIDs(changedDoc)
			for id := range base {
				if !changed[id] {
					t.Errorf("missing after the state change: %s", id)
				}
			}
			for id := range changed {
				if !base[id] {
					t.Errorf("new after the state change: %s", id)
				}
			}
		})
	}
}

// fixtureManifest is the part of <scenario>/manifest.json the tests
// keep in step with the scenario.
type fixtureManifest struct {
	Scenario                string         `json:"scenario"`
	Source                  string         `json:"source"`
	ProducerBehaviorVersion string         `json:"producer_behavior_version"`
	Classes                 []string       `json:"classes"`
	Failures                map[string]int `json:"failures"`
}

// TestGolden_ManifestMatchesFixtures proves every scenario manifest
// names exactly the class responses and failures in its directory and
// the current producer behavior version.
func TestGolden_ManifestMatchesFixtures(t *testing.T) {
	list := func(dir, suffix string) []string {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, e := range entries {
			if name, ok := strings.CutSuffix(e.Name(), suffix); ok && !e.IsDir() {
				out = append(out, name)
			}
		}
		sort.Strings(out)
		return out
	}
	for _, scenario := range goldenScenarios {
		t.Run(scenario, func(t *testing.T) {
			dir := filepath.Join(goldenRoot, scenario)
			data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
			if err != nil {
				t.Fatal(err)
			}
			var m fixtureManifest
			if err := json.Unmarshal(data, &m); err != nil {
				t.Fatal(err)
			}
			if m.Scenario != scenario || m.Source != "synthetic" || m.ProducerBehaviorVersion != generatorVersion {
				t.Errorf("manifest header = %q %q %q", m.Scenario, m.Source, m.ProducerBehaviorVersion)
			}
			if got := list(filepath.Join(dir, "api"), ".json"); !slices.Equal(got, m.Classes) {
				t.Errorf("classes = %v, api/ holds %v", m.Classes, got)
			}
			failures := map[string]int{}
			if data, err := os.ReadFile(filepath.Join(dir, "failures.json")); err == nil {
				if err := json.Unmarshal(data, &failures); err != nil {
					t.Fatal(err)
				}
			}
			if !maps.Equal(failures, m.Failures) {
				t.Errorf("failures = %v, failures.json holds %v", m.Failures, failures)
			}
		})
	}
}
