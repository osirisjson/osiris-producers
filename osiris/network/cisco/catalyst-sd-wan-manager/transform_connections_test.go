// transform_connections_test.go - Unit tests for tunnel and OMP peering
// connection mapping: canonical endpoint order, protocol-qualified OMP
// identity, merged observations and row-order independence.
//
// OSIRIS JSON Producer for Cisco Catalyst SD-WAN Manager introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package catalystsdwanmanager

import (
	"math/rand"
	"reflect"
	"testing"

	"go.osirisjson.org/producers/pkg/sdk"
)

func TestParseTunnelEndpoints(t *testing.T) {
	cases := []struct {
		name         string
		wantA, wantB string
		wantOK       bool
	}{
		{"198.51.100.1:lte-198.51.100.2:lte", "198.51.100.1:lte", "198.51.100.2:lte", true},
		{"203.0.113.1:biz-internet-203.0.113.2:public-internet", "203.0.113.1:biz-internet", "203.0.113.2:public-internet", true},
		{"not-a-valid-tunnel-name", "", "", false},
		{"2001:db8::1:lte-2001:db8::2:lte", "", "", false},
	}
	for _, c := range cases {
		gotA, gotB, ok := parseTunnelEndpoints(c.name)
		if ok != c.wantOK {
			t.Errorf("parseTunnelEndpoints(%q) ok = %v, want %v", c.name, ok, c.wantOK)
			continue
		}
		if ok && (gotA != c.wantA || gotB != c.wantB) {
			t.Errorf("parseTunnelEndpoints(%q) = (%q, %q), want (%q, %q)", c.name, gotA, gotB, c.wantA, c.wantB)
		}
	}
}

func score(v float64) *float64 { return &v }

// tunnelResolver resolves keys from a fixed map.
func tunnelResolver(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		id, ok := m[k]
		return id, ok && id != ""
	}
}

func TestTransformTunnels_CanonicalAndMerged(t *testing.T) {
	idx := map[string]string{
		"198.51.100.1:lte": "cisco.catalystsdwanmanager::TST0000001-ge0/1",
		"198.51.100.2:lte": "cisco.catalystsdwanmanager::TST0000002-ge0/1",
	}
	// Both ends report the tunnel, in opposite directions, with
	// different states; one reports a zero quality score.
	devices := []SiteTopologyDevice{
		{Circuits: []SiteCircuit{{Color: "lte", Tunnels: []SiteTunnel{{Name: "198.51.100.2:lte-198.51.100.1:lte", State: "Up", VqoeScore: score(0)}}}}},
		{Circuits: []SiteCircuit{{Color: "lte", Tunnels: []SiteTunnel{{Name: "198.51.100.1:lte-198.51.100.2:lte", State: "Down"}}}}},
	}
	conns, stats := TransformTunnels(devices, tunnelResolver(idx))
	if len(conns) != 1 || stats != (tunnelStats{}) {
		t.Fatalf("got %d connections, stats %+v; want 1, none skipped", len(conns), stats)
	}
	c := conns[0]
	if c.Source > c.Target || c.Direction != "bidirectional" {
		t.Errorf("endpoints not canonical: %s -> %s (%s)", c.Source, c.Target, c.Direction)
	}
	ext := c.Extensions[extensionKey].(map[string]any)
	if !reflect.DeepEqual(ext["observed_states"], []string{"down", "up"}) || c.State != "" {
		t.Errorf("conflicting states = %v / %q, want observed_states [down up] and no state", ext["observed_states"], c.State)
	}
	if ext["vqoe_score"] != 0.0 {
		t.Errorf("a real zero quality score must be kept, got %v", ext["vqoe_score"])
	}
}

func TestTransformTunnels_UnresolvedAndUnparsedAreCounted(t *testing.T) {
	idx := map[string]string{"198.51.100.1:lte": "cisco.catalystsdwanmanager::TST0000001-ge0/1", "198.51.100.3:lte": ""}
	devices := []SiteTopologyDevice{{Circuits: []SiteCircuit{{Tunnels: []SiteTunnel{
		{Name: "198.51.100.1:lte-198.51.100.2:lte"}, // unknown peer.
		{Name: "198.51.100.1:lte-198.51.100.3:lte"}, // ambiguous peer.
		{Name: "garbage"},
	}}}}}
	conns, stats := TransformTunnels(devices, tunnelResolver(idx))
	if len(conns) != 0 || stats.unresolved != 2 || stats.unparsed != 1 {
		t.Errorf("conns=%d stats=%+v, want 0 and {unparsed:1 unresolved:2}", len(conns), stats)
	}
}

func TestTransformOMP_OneConnectionPerPairAcrossSources(t *testing.T) {
	a, b := "cisco.catalystsdwanmanager::TST0000002", "cisco.catalystsdwanmanager::CTRL-0001"
	conns := TransformOMP([]ompObservation{
		{a: a, b: b, state: "up", source: opOMPLinks},
		{a: b, b: a, state: "up", source: opOMPPeers},
		{a: a, b: a, state: "up", source: opOMPPeers}, // self, ignored.
	})
	if len(conns) != 1 {
		t.Fatalf("got %d connections, want 1", len(conns))
	}
	c := conns[0]
	if c.Source != b || c.Target != a || c.Direction != "bidirectional" || c.State != "up" {
		t.Errorf("connection = %s -> %s %s %s", c.Source, c.Target, c.Direction, c.State)
	}
	ext := c.Extensions[extensionKey].(map[string]any)
	if ext["protocol"] != "omp" || !reflect.DeepEqual(ext["sources"], []string{opOMPLinks, opOMPPeers}) {
		t.Errorf("ext = %v", ext)
	}
	// The protocol qualifier keeps OMP distinct from any other
	// "network" relationship between the same devices.
	plain := sdk.BuildConnectionID(sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{Type: "network", Direction: "bidirectional", Source: b, Target: a}), 16)
	if c.ID == plain {
		t.Error("OMP connection ID is not protocol-qualified")
	}
}

// TestTransforms_RowOrderIndependent shuffles the input 100 times and
// requires byte-identical connections.
func TestTransforms_RowOrderIndependent(t *testing.T) {
	ids := []string{"cisco.catalystsdwanmanager::A", "cisco.catalystsdwanmanager::B", "cisco.catalystsdwanmanager::C"}
	var obs []ompObservation
	for i, x := range ids {
		for _, y := range ids[i+1:] {
			obs = append(obs, ompObservation{a: x, b: y, state: "up", source: opOMPLinks}, ompObservation{a: y, b: x, state: "down", source: opOMPPeers})
		}
	}
	idx := map[string]string{"198.51.100.1:lte": ids[0], "198.51.100.2:lte": ids[1], "198.51.100.3:mpls": ids[2], "198.51.100.1:mpls": ids[0] + "-m"}
	var tunnels []SiteTunnel
	for _, n := range []string{"198.51.100.1:lte-198.51.100.2:lte", "198.51.100.2:lte-198.51.100.1:lte", "198.51.100.3:mpls-198.51.100.1:mpls"} {
		tunnels = append(tunnels, SiteTunnel{Name: n, State: "up", VqoeScore: score(7)})
	}

	marshal := func(o []ompObservation, ts []SiteTunnel) string {
		tc, _ := TransformTunnels([]SiteTopologyDevice{{Circuits: []SiteCircuit{{Color: "lte", Tunnels: ts}}}}, tunnelResolver(idx))
		doc := &sdk.Document{Topology: sdk.Topology{Connections: append(TransformOMP(o), tc...)}}
		b, err := sdk.MarshalDocument(doc)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	want := marshal(obs, tunnels)
	r := rand.New(rand.NewSource(1))
	for i := 0; i < 100; i++ {
		o := append([]ompObservation(nil), obs...)
		ts := append([]SiteTunnel(nil), tunnels...)
		r.Shuffle(len(o), func(i, j int) { o[i], o[j] = o[j], o[i] })
		r.Shuffle(len(ts), func(i, j int) { ts[i], ts[j] = ts[j], ts[i] })
		if got := marshal(o, ts); got != want {
			t.Fatalf("permutation %d changed the output", i)
		}
	}
}
