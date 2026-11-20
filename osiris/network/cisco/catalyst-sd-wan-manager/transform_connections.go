// transform_connections.go - SD-WAN tunnel and OMP control-plane
// peering connection transforms.
//
// OSIRIS JSON section 2.2.3 requires a connection's source and target
// to resolve within the same document. The document stage
// (document.go, neighbors.go) resolves every endpoint against the whole
// controller inventory and adds a remote endpoint from another site as
// a neighbor resource, so a cross-site relationship stays in each site
// document that takes part in it. The functions here only build
// connections from already resolved endpoints.
//
// Both kinds of connection are bidirectional and emitted with their
// endpoints in canonical (sorted) order, so the same relationship has
// the same connection ID, source and target in every site document
// that carries it and an OSIRIS JSON Consumer merging documents can
// deduplicate it by ID. Nothing here depends on API row order.
//
// OSIRIS JSON Producer for Cisco Catalyst SD-WAN Manager introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification
package catalystsdwanmanager

import (
	"regexp"
	"sort"
	"strings"

	"go.osirisjson.org/producers/pkg/sdk"
)

// tunnelNameRe parses a site topology tunnel name of the form
// "{system-ip}:{color}-{system-ip}:{color}". Colors are matched
// non-greedily since real SD-WAN Manager color names commonly contain
// hyphens (e.g. "biz-internet", "public-internet"). Only IPv4 system
// IPs are recognized; any other name is left unresolved rather than
// guessed.
var tunnelNameRe = regexp.MustCompile(`^(\d{1,3}(?:\.\d{1,3}){3}):([a-z0-9-]+?)-(\d{1,3}(?:\.\d{1,3}){3}):([a-z0-9-]+)$`)

// parseTunnelEndpoints splits a tunnel name into its two
// "{system-ip}:{color}" endpoint keys.
func parseTunnelEndpoints(name string) (keyA, keyB string, ok bool) {
	m := tunnelNameRe.FindStringSubmatch(name)
	if m == nil {
		return "", "", false
	}
	return m[1] + ":" + m[2], m[3] + ":" + m[4], true
}

// splitTunnelKey splits "{system-ip}:{color}" into its parts.
func splitTunnelKey(key string) (systemIP, color string) {
	i := strings.IndexByte(key, ':')
	if i < 0 {
		return key, ""
	}
	return key[:i], key[i+1:]
}

// canonicalPair orders two endpoint IDs for a bidirectional connection.
func canonicalPair(a, b string) (string, string) {
	if a > b {
		return b, a
	}
	return a, b
}

// tunnelObservation is one resolved tunnel between two interface
// resources.
type tunnelObservation struct {
	a, b         string // interface resource IDs.
	color, state string
	vqoe         float64
	hasVQoE      bool
}

// tunnelStats counts tunnel entries that did not become connections.
type tunnelStats struct {
	unparsed   int // name not in the documented form.
	unresolved int // an endpoint could not be resolved unambiguously.
}

// TransformTunnels builds one "network.vpn" connection per distinct
// interface pair from the site topology monitor entries. resolve maps
// a "{system-ip}:{color}" key to an interface resource ID; ok is false
// for an unknown or ambiguous endpoint. A pair observed several times
// (both ends report it) becomes one connection; differing observed
// states are kept in observed_states instead of letting row order
// decide.
func TransformTunnels(devices []SiteTopologyDevice, resolve func(key string) (string, bool)) ([]sdk.Connection, tunnelStats) {
	var stats tunnelStats
	byPair := map[[2]string][]tunnelObservation{}
	for _, dev := range devices {
		for _, circuit := range dev.Circuits {
			for _, tunnel := range circuit.Tunnels {
				keyA, keyB, ok := parseTunnelEndpoints(tunnel.Name)
				if !ok {
					stats.unparsed++
					continue
				}
				a, okA := resolve(keyA)
				b, okB := resolve(keyB)
				if !okA || !okB || a == b {
					stats.unresolved++
					continue
				}
				a, b = canonicalPair(a, b)
				o := tunnelObservation{
					a: a, b: b, color: circuit.Color, state: strings.ToLower(tunnel.State),
				}
				if tunnel.VqoeScore != nil {
					o.vqoe, o.hasVQoE = *tunnel.VqoeScore, true
				}
				byPair[[2]string{a, b}] = append(byPair[[2]string{a, b}], o)
			}
		}
	}

	pairs := make([][2]string, 0, len(byPair))
	for p := range byPair {
		pairs = append(pairs, p)
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i][0]+"\x00"+pairs[i][1] < pairs[j][0]+"\x00"+pairs[j][1] })

	connections := make([]sdk.Connection, 0, len(pairs))
	for _, p := range pairs {
		obs := byPair[p]
		key := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
			Type: "network.vpn", Direction: "bidirectional", Source: p[0], Target: p[1],
		})
		conn, err := sdk.NewConnection(sdk.BuildConnectionID(key, 16), "network.vpn", p[0], p[1])
		if err != nil {
			continue
		}
		_ = conn.SetDirection("bidirectional")
		conn.Name = "SD-WAN Site-to-Site Tunnel"

		states := map[string]bool{}
		colors := map[string]bool{}
		minVQoE, hasVQoE := 0.0, false
		for _, o := range obs {
			if o.state != "" {
				states[o.state] = true
			}
			if o.color != "" {
				colors[o.color] = true
			}
			if o.hasVQoE && (!hasVQoE || o.vqoe < minVQoE) {
				minVQoE, hasVQoE = o.vqoe, true
			}
		}
		ext := map[string]any{}
		applyObservedState(&conn, sortedKeys(states), ext)
		if c := sortedKeys(colors); len(c) == 1 {
			ext["color"] = c[0]
		} else if len(c) > 1 {
			ext["colors"] = c
		}
		if hasVQoE {
			ext["vqoe_score"] = minVQoE
		}
		conn.Extensions = map[string]any{extensionKey: ext}
		connections = append(connections, conn)
	}
	return connections, stats
}

// ompObservation is one observed OMP session between two resolved
// device resources, from source "omp_links" or "omp_peers".
type ompObservation struct {
	a, b   string
	state  string
	source string
}

// TransformOMP builds one bidirectional "network" connection with
// protocol "omp" per device pair from every observation of that pair,
// whichever API reported it and from whichever end. The connection ID
// is qualified by the protocol, so it cannot collide with another
// "network" relationship between the same devices. sources lists which
// operations reported the session; differing observed states are kept
// in observed_states.
func TransformOMP(observations []ompObservation) []sdk.Connection {
	byPair := map[[2]string][]ompObservation{}
	for _, o := range observations {
		if o.a == "" || o.b == "" || o.a == o.b {
			continue
		}
		a, b := canonicalPair(o.a, o.b)
		byPair[[2]string{a, b}] = append(byPair[[2]string{a, b}], o)
	}
	pairs := make([][2]string, 0, len(byPair))
	for p := range byPair {
		pairs = append(pairs, p)
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i][0]+"\x00"+pairs[i][1] < pairs[j][0]+"\x00"+pairs[j][1] })

	connections := make([]sdk.Connection, 0, len(pairs))
	for _, p := range pairs {
		key := sdk.ConnectionCanonicalKey(sdk.ConnectionIDInput{
			Type: "network", Direction: "bidirectional", Source: p[0], Target: p[1],
			Qualifiers: map[string]string{"protocol": "omp"},
		})
		conn, err := sdk.NewConnection(sdk.BuildConnectionID(key, 16), "network", p[0], p[1])
		if err != nil {
			continue
		}
		_ = conn.SetDirection("bidirectional")
		conn.Name = "OMP Peering"

		states := map[string]bool{}
		sources := map[string]bool{}
		for _, o := range byPair[p] {
			if o.state != "" {
				states[strings.ToLower(o.state)] = true
			}
			sources[o.source] = true
		}
		ext := map[string]any{"protocol": "omp", "sources": sortedKeys(sources)}
		applyObservedState(&conn, sortedKeys(states), ext)
		conn.Extensions = map[string]any{extensionKey: ext}
		connections = append(connections, conn)
	}
	return connections
}

// applyObservedState sets a connection's state and status from its
// observed states: a single state is used as is; disagreeing
// observations leave the status unknown and are listed in
// observed_states.
func applyObservedState(conn *sdk.Connection, states []string, ext map[string]any) {
	switch len(states) {
	case 0:
		conn.Status = mapUpDownStatus("")
	case 1:
		conn.State = states[0]
		conn.Status = mapUpDownStatus(states[0])
	default:
		conn.Status = mapUpDownStatus("")
		ext["observed_states"] = states
	}
}

// sortedKeys returns the keys of a string set in sorted order.
func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
