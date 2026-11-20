// neighbors_test.go - Cross-site ("site plus neighbors") tests for the
// Cisco Catalyst SD-WAN Manager OSIRIS JSON Producer: a two-site fabric
// with a controller, exported per site, must keep its cross-site OMP
// sessions and tunnels in every site document that takes part, with
// identical IDs, so an OSIRIS JSON Consumer can merge the documents.
//
// OSIRIS JSON Producer for Cisco Catalyst SD-WAN Manager introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification
package catalystsdwanmanager

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"os"
	"reflect"
	"strings"
	"testing"

	"go.osirisjson.org/producers/pkg/sdk"
)

// fabricFake is two WAN edge sites (100, 200) plus an unsited
// controller. RTR01 and RTR02 hold an OMP session with the controller
// and a biz-internet tunnel with each other.
func fabricFake() *fakeAPI {
	f := twoSiteFake()
	f.devices = append(f.devices, Device{DeviceID: "192.0.2.1", SystemIP: "192.0.2.1", HostName: "LAB-CONTROLLER01", UUID: "TST-UUID-CTRL", Personality: "vsmart"})
	f.ifaces = map[string][]Interface{
		"192.0.2.11": {{IfName: "ge0/0", AFType: "ipv4", VPNID: "0", IPAddress: "198.51.100.11/24"}, {IfName: "ge0/1", AFType: "ipv4", VPNID: "10"}},
		"192.0.2.12": {{IfName: "ge0/0", AFType: "ipv4", VPNID: "0", IPAddress: "198.51.100.12/24"}},
	}
	f.wan = map[string][]WANInterface{
		"192.0.2.11": {{Interface: "ge0/0", Color: "biz-internet"}},
		"192.0.2.12": {{Interface: "ge0/0", Color: "biz-internet"}},
	}
	f.peers = map[string][]OMPPeer{
		"192.0.2.11": {{Peer: "192.0.2.1", State: "up"}},
		"192.0.2.12": {{Peer: "192.0.2.1", State: "up"}, {Peer: "192.0.2.99", State: "up"}}, // unknown peer.
	}
	f.topo = map[string][]SiteTopologyDevice{
		"100": {{DeviceID: "192.0.2.11", Circuits: []SiteCircuit{{Color: "biz-internet", Tunnels: []SiteTunnel{{Name: "192.0.2.11:biz-internet-192.0.2.12:biz-internet", State: "up"}}}}}},
		"200": {{DeviceID: "192.0.2.12", Circuits: []SiteCircuit{{Color: "biz-internet", Tunnels: []SiteTunnel{{Name: "192.0.2.12:biz-internet-192.0.2.11:biz-internet", State: "up"}}}}}},
	}
	return f
}

// exportDocs runs export and returns the documents by site name.
func exportDocs(t *testing.T, f *fakeAPI, cfg *Config) map[string]sdk.Document {
	t.Helper()
	if err := export(f, cfg, testLogger(t), fixedRun); err != nil {
		t.Fatalf("export: %v", err)
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
		name := unsitedSegment
		if len(d.Metadata.Scope.Sites) > 0 {
			name = d.Metadata.Scope.Sites[0]
		}
		docs[name] = d
	}
	return docs
}

func connsOfType(d sdk.Document, typ string) map[string]sdk.Connection {
	out := map[string]sdk.Connection{}
	for _, c := range d.Topology.Connections {
		if c.Type == typ {
			out[c.ID] = c
		}
	}
	return out
}

func extOf(m map[string]any) map[string]any {
	e, _ := m[extensionKey].(map[string]any)
	return e
}

func TestNeighbors_CrossSiteRelationshipsInEverySiteDocument(t *testing.T) {
	for _, purpose := range []string{"documentation", "audit"} {
		t.Run(purpose, func(t *testing.T) {
			cfg := testConfig(t)
			cfg.Purpose = purpose
			docs := exportDocs(t, fabricFake(), cfg)
			branch, office := docs["BRANCH-00"], docs["OFFICE-00"]

			tb, to := connsOfType(branch, "network.vpn"), connsOfType(office, "network.vpn")
			if len(tb) != 1 || len(to) != 1 {
				t.Fatalf("tunnels per document = %d, %d; want 1 each", len(tb), len(to))
			}
			for id, c := range tb {
				o, ok := to[id]
				if !ok || o.Source != c.Source || o.Target != c.Target {
					t.Errorf("the cross-site tunnel differs between documents: %+v vs %+v", c, o)
				}
			}

			// RTR01 -> controller and RTR02 -> controller OMP sessions.
			ob, oo := connsOfType(branch, "network"), connsOfType(office, "network")
			if len(ob) != 1 || len(oo) != 1 {
				t.Errorf("OMP sessions per document = %d, %d; want 1 each", len(ob), len(oo))
			}

			for name, d := range docs {
				ids := map[string]sdk.Resource{}
				for _, r := range d.Topology.Resources {
					ids[r.ID] = r
				}
				for _, c := range d.Topology.Connections {
					if _, ok := ids[c.Source]; !ok {
						t.Errorf("%s: connection %s source is not in the document", name, c.ID)
					}
					if _, ok := ids[c.Target]; !ok {
						t.Errorf("%s: connection %s target is not in the document", name, c.ID)
					}
				}
				if len(d.Topology.Groups) != 1 || d.Topology.Groups[0].Type != "logical.site" {
					t.Fatalf("%s: groups = %+v", name, d.Topology.Groups)
				}
				members := map[string]bool{}
				for _, m := range d.Topology.Groups[0].Members {
					members[m] = true
				}
				var neighbors int
				for id, r := range ids {
					isNeighbor := extOf(r.Extensions)["site_role"] == siteRoleNeighbor
					if isNeighbor {
						neighbors++
						if members[id] {
							t.Errorf("%s: neighbor %s is a site group member", name, id)
						}
						if r.Properties["ip_addresses"] != nil || r.Status != "" {
							t.Errorf("%s: neighbor %s carries more than identity: %+v", name, id, r)
						}
					} else if !members[id] {
						t.Errorf("%s: site resource %s is not a group member", name, id)
					}
					if r.Provider.System != cfg.Host {
						t.Errorf("%s: %s provider.system = %q", name, id, r.Provider.System)
					}
				}
				// Controller + remote router + remote transport
				// interface the unsited controller document has no
				// session of its own in this fixture.
				if name != unsitedSegment && neighbors != 3 {
					t.Errorf("%s: %d neighbor resources, want 3", name, neighbors)
				}
			}

			graph := extOf(office.Metadata.Extensions)["graph"].(map[string]any)
			if graph["unresolved_omp"] != float64(1) {
				t.Errorf("OFFICE-00 unresolved_omp = %v, want 1 (the unknown peer)", graph["unresolved_omp"])
			}
		})
	}
}

// TestNeighbors_MergeByIDGivesOneEndToEndGraph merges the two site
// documents the way an OSIRIS JSON Consumer does (union by ID, full
// record preferred over a neighbor) and requires one complete graph.
func TestNeighbors_MergeByIDGivesOneEndToEndGraph(t *testing.T) {
	cfg := testConfig(t)
	cfg.Purpose = "audit"
	docs := exportDocs(t, fabricFake(), cfg)

	res := map[string]sdk.Resource{}
	conns := map[string]sdk.Connection{}
	for _, d := range docs {
		for _, r := range d.Topology.Resources {
			prev, seen := res[r.ID]
			if !seen || extOf(prev.Extensions)["site_role"] == siteRoleNeighbor {
				if seen && prev.Type != r.Type {
					t.Errorf("%s has type %s in one document and %s in another", r.ID, prev.Type, r.Type)
				}
				res[r.ID] = r
			}
		}
		for _, c := range d.Topology.Connections {
			if prev, seen := conns[c.ID]; seen && (prev.Source != c.Source || prev.Target != c.Target) {
				t.Errorf("connection %s differs between documents", c.ID)
			}
			conns[c.ID] = c
		}
	}
	var vpn, omp int
	for _, c := range conns {
		switch c.Type {
		case "network.vpn":
			vpn++
		case "network":
			omp++
		}
	}
	if vpn != 1 || omp != 2 {
		t.Errorf("merged graph: %d tunnels, %d OMP sessions; want 1 and 2", vpn, omp)
	}
	// Every site, the controller's unsited bucket included, was
	// exported, so every neighbor is replaced by its full record.
	for id, r := range res {
		if extOf(r.Extensions)["site_role"] == siteRoleNeighbor {
			t.Errorf("after merge %s should be its full site record", id)
		}
	}
	if _, ok := res[resourceID("TST-UUID-CTRL")]; !ok {
		t.Error("merged graph lacks the controller")
	}
}

func TestNeighbors_PeerOutsideSelectedSitesStillResolves(t *testing.T) {
	f := fabricFake()
	cfg := testConfig(t)
	cfg.AllSites = false
	cfg.SiteFilter = []string{"100"}
	docs := exportDocs(t, f, cfg)
	if len(docs) != 1 {
		t.Fatalf("documents = %d, want 1", len(docs))
	}
	if !f.called("wan_interface:192.0.2.12") {
		t.Error("the tunnel peer's WAN interfaces were not fetched")
	}
	if f.called("device_interface:192.0.2.12") || f.called("omp_peers:192.0.2.12") {
		t.Error("an unselected site's devices must not be fully collected")
	}
	if n := len(connsOfType(docs["BRANCH-00"], "network.vpn")); n != 1 {
		t.Errorf("tunnels = %d, want 1", n)
	}
}

func TestMergeInterfaceRows_VPNAndAddressFamily(t *testing.T) {
	rows := []Interface{
		{IfName: "ge0/1", AFType: "ipv6", VPNID: "10", Description: "uplink to LAB-SW01"},
		{IfName: "ge0/1", AFType: "ipv4", VPNID: "10", IPAddress: "198.51.100.1/24"},
		{IfName: "ge0/1", AFType: "ipv4", VPNID: "0"},
		{IfName: "ge0/2", AFType: "ipv4", VPNID: "512"},
	}
	got := mergeInterfaceRows(rows)
	keys := map[string]mergedInterface{}
	for _, m := range got {
		keys[m.key] = m
	}
	if len(got) != 3 {
		t.Fatalf("merged = %d interfaces, want 3: %+v", len(got), got)
	}
	if _, ok := keys["ge0/1"]; !ok {
		t.Error("VPN 0 ge0/1 must keep the plain key")
	}
	v10, ok := keys["ge0/1@vpn10"]
	if !ok || v10.Description != "uplink to LAB-SW01" || v10.IPAddress != "198.51.100.1/24" {
		t.Errorf("VPN 10 ge0/1 = %+v, want the IPv4 row with the IPv6 row's description", v10)
	}
	if _, ok := keys["ge0/2"]; !ok {
		t.Error("an interface used in one VPN keeps its plain key")
	}
}

// TestIsPublicIP checks the classification boundaries. The non-public
// addresses are built from their octets: they are the protocol ranges
// under test (RFC 1918, RFC 6598, RFC 3927, RFC 4193), not data.
func TestIsPublicIP(t *testing.T) {
	ula := net.IP{0xfd, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}
	for ip, want := range map[string]bool{
		"198.51.100.1":                    true,
		"2001:db8::1":                     true,
		"127.0.0.1":                       false,
		net.IPv4(10, 0, 0, 1).String():    false,
		net.IPv4(100, 64, 0, 1).String():  false,
		net.IPv4(169, 254, 0, 1).String(): false,
		ula.String():                      false,
	} {
		if got := isPublicIP(ip); got != want {
			t.Errorf("isPublicIP(%s) = %v, want %v", ip, got, want)
		}
	}
}

// TestExport_DocumentsIndependentOfAPIOrder shuffles every API list of
// the two-site fabric 100 times and requires byte-identical documents.
func TestExport_DocumentsIndependentOfAPIOrder(t *testing.T) {
	render := func(f *fakeAPI) map[string]string {
		cfg := testConfig(t)
		cfg.Purpose = "audit"
		if err := export(f, cfg, testLogger(t), fixedRun); err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for _, p := range writtenFiles(t, cfg.OutputDir) {
			data, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			out[p[len(cfg.OutputDir):]] = string(data)
		}
		return out
	}
	want := render(fabricFake())
	r := rand.New(rand.NewSource(7))
	for i := 0; i < 100; i++ {
		f := fabricFake()
		r.Shuffle(len(f.devices), func(a, b int) { f.devices[a], f.devices[b] = f.devices[b], f.devices[a] })
		for _, m := range []map[string][]Interface{f.ifaces} {
			for k, v := range m {
				r.Shuffle(len(v), func(a, b int) { v[a], v[b] = v[b], v[a] })
				m[k] = v
			}
		}
		for k, v := range f.peers {
			r.Shuffle(len(v), func(a, b int) { v[a], v[b] = v[b], v[a] })
			f.peers[k] = v
		}
		got := render(f)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("permutation %d changed the documents", i)
		}
	}
}

func TestTransformInterfaces_SharedColorIsAmbiguous(t *testing.T) {
	ifaces := []Interface{
		{IfName: "GigabitEthernet1", AFType: "ipv4", VPNID: "0"},
		{IfName: "GigabitEthernet2", AFType: "ipv4", VPNID: "0"},
	}
	wan := []WANInterface{
		{Interface: "GigabitEthernet1", Color: "mpls"},
		{Interface: "GigabitEthernet2", Color: "mpls"},
	}
	_, _, idx := TransformInterfaces(resourceID("TST0000011"), "network.router", "TST0000011", "192.0.2.11", ifaces, wan)
	if id, ok := idx["192.0.2.11:mpls"]; !ok || id != "" {
		t.Errorf("tunnel index for a color on two interfaces = %q, %v; want ambiguous", id, ok)
	}
}

func TestVPNLess(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{{"0", "10", true}, {"10", "9", false}, {"2", "x", true}, {"x", "2", false}, {"a", "b", true}} {
		if got := vpnLess(c.a, c.b); got != c.want {
			t.Errorf("vpnLess(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// TestRawTopology_OwnerBySystemIPAndUnownedCounted covers a Manager
// whose site topology "device-id" is not the inventory deviceId: the
// row attaches through its circuit's system IP, and a row nobody owns
// is counted in raw_omitted instead of being dropped silently.
func TestRawTopology_OwnerBySystemIPAndUnownedCounted(t *testing.T) {
	f := fabricFake()
	f.topo["100"] = []SiteTopologyDevice{
		{DeviceID: "TST-OTHER-ID", Circuits: []SiteCircuit{{Color: "biz-internet", SystemIP: "192.0.2.11", Tunnels: []SiteTunnel{{Name: "192.0.2.11:biz-internet-192.0.2.12:biz-internet", State: "up"}}}}},
		{DeviceID: "TST-NOBODY"},
	}
	cfg := testConfig(t)
	cfg.Purpose, cfg.IncludeRawBody = "audit", true
	docs := exportDocs(t, f, cfg)
	var router sdk.Resource
	for _, r := range docs["BRANCH-00"].Topology.Resources {
		if r.ID == resourceID("TST0000001") {
			router = r
		}
	}
	ext := extOf(router.Extensions)
	if _, ok := ext["site_topology_raw"]; !ok {
		t.Error("site_topology_raw not attached through the circuit system IP")
	}
	got, _ := json.Marshal(ext["raw_omitted"])
	if !reflect.DeepEqual(string(got), `[{"count":1,"endpoint":"site_topology_raw","reason":"unowned"}]`) {
		t.Errorf("raw_omitted = %s, want one unowned site_topology_raw row", got)
	}
}

// TestNeighbors_HubWithManyPeersOutsideSelection exports only a hub
// site whose 100 spokes are in other sites: every spoke's WAN listing
// is fetched once (in parallel) and every tunnel is resolved.
func TestNeighbors_HubWithManyPeersOutsideSelection(t *testing.T) {
	const spokes = 100
	hub := Device{DeviceID: "192.0.2.10", SystemIP: "192.0.2.10", HostName: "LAB-RTR00", BoardSerial: "TST0009999", SiteID: "100", Personality: "vedge"}
	f := &fakeAPI{
		devices: []Device{hub},
		names:   map[string]string{"100": "MXP"},
		ifaces:  map[string][]Interface{hub.SystemIP: {{IfName: "GigabitEthernet1", AFType: "ipv4", VPNID: "0"}}},
		wan:     map[string][]WANInterface{hub.SystemIP: {{Interface: "GigabitEthernet1", Color: "mpls"}}},
		topo:    map[string][]SiteTopologyDevice{},
	}
	var tunnels []SiteTunnel
	for i := 0; i < spokes; i++ {
		ip := fmt.Sprintf("198.51.100.%d", i+1)
		f.devices = append(f.devices, Device{DeviceID: ip, SystemIP: ip, HostName: fmt.Sprintf("LAB-RTR%03d", i+1), BoardSerial: fmt.Sprintf("TST%07d", i+1), SiteID: fmt.Sprintf("%d", 1000+i), Personality: "vedge"})
		f.wan[ip] = []WANInterface{{Interface: "GigabitEthernet1", Color: "mpls"}}
		tunnels = append(tunnels, SiteTunnel{Name: hub.SystemIP + ":mpls-" + ip + ":mpls", State: "up"})
	}
	f.topo["100"] = []SiteTopologyDevice{{DeviceID: hub.DeviceID, Circuits: []SiteCircuit{{Color: "mpls", SystemIP: hub.SystemIP, Tunnels: tunnels}}}}

	cfg := testConfig(t)
	cfg.AllSites, cfg.SiteFilter = false, []string{"100"}
	docs := exportDocs(t, f, cfg)
	d := docs["MXP"]
	if n := len(connsOfType(d, "network.vpn")); n != spokes {
		t.Errorf("tunnels = %d, want %d", n, spokes)
	}
	peerCalls := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, "wan_interface:198.51.100.") {
			peerCalls++
		}
	}
	if peerCalls != spokes {
		t.Errorf("peer WAN lookups = %d, want %d (one per spoke)", peerCalls, spokes)
	}
	graph := extOf(d.Metadata.Extensions)["graph"].(map[string]any)
	if graph["unresolved_tunnels"] != float64(0) || graph["neighbor_interfaces"] != float64(spokes) {
		t.Errorf("graph = %v", graph)
	}
	for _, e := range extOf(d.Metadata.Extensions)["coverage"].([]any) {
		if m := e.(map[string]any); m["operation"] == opNeighborWAN && m["attempts"] != float64(spokes) {
			t.Errorf("neighbor_wan_interface attempts = %v, want %d (this site's peers only)", m["attempts"], spokes)
		}
	}
}
