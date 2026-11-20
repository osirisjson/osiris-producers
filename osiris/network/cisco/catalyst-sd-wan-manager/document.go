// document.go - Per-site document build stage of the Cisco Catalyst
// SD-WAN Manager OSIRIS JSON Producer.
//
// buildSiteDocument turns one collected site (collect.go) into a
// portable OSIRIS JSON document without any API call:
//
//  1. the site's own devices and interfaces, with provider.site (the
//     site name, OSIRIS JSON's facility identifier; the raw site-id is
//     extension site_id) and provider.system (the Manager host);
//  2. OMP sessions and transport tunnels with at least one endpoint in
//     the site, the remote endpoint resolved against the whole
//     controller inventory and carried as a neighbor resource when it
//     belongs to another site (neighbors.go);
//  3. the logical.site group of the site's own resources;
//  4. raw row attachment (raw.go), the --purpose field contract
//     (purpose.go) and secret handling;
//  5. the SDK build plus the producer metadata: coverage (coverage.go)
//     and the graph record below.
//
// The graph record, under metadata["osiris.cisco.catalystsdwanmanager"]
// "graph", counts what crossed or failed to cross the site boundary:
// neighbor devices and interfaces included, OMP sessions and tunnels
// omitted because an endpoint was unknown or ambiguous, tunnel names
// not in the documented form, and inventory devices skipped for lack of
// a native identity.
//
// Every document of one run shares the run's single timestamp.
//
// OSIRIS JSON Producer for Cisco Catalyst SD-WAN Manager introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification
package catalystsdwanmanager

import (
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"go.osirisjson.org/producers/pkg/sdk"
)

// graphStats is the graph record of one site document.
type graphStats struct {
	neighborDevices    int
	neighborInterfaces int
	unresolvedOMP      int
	unresolvedTunnels  int
	unparsedTunnels    int
	unidentified       int
}

func (g graphStats) metadata() map[string]any {
	return map[string]any{
		"neighbor_devices":     g.neighborDevices,
		"neighbor_interfaces":  g.neighborInterfaces,
		"unresolved_omp":       g.unresolvedOMP,
		"unresolved_tunnels":   g.unresolvedTunnels,
		"unparsed_tunnels":     g.unparsedTunnels,
		"unidentified_devices": g.unidentified,
	}
}

// buildSiteDocument builds the document of site s from col now is the
// run timestamp.
func buildSiteDocument(col *collection, s *siteObs, cfg *Config, now time.Time, log *slog.Logger) (*sdk.Document, error) {
	ctx := sdk.NewContext(&sdk.ProducerConfig{
		SafeFailureMode: cfg.SafeFailureMode,
		Purpose:         cfg.Purpose,
	})
	ctx.Logger = log
	ctx.Clock = func() time.Time { return now }
	stats := graphStats{unidentified: col.unidentified}

	devices := make([]Device, len(s.devices))
	for i, o := range s.devices {
		devices[i] = o.Device
	}
	resources, _ := TransformDevices(devices, cfg.Purpose)
	var connections []sdk.Connection

	deviceResIdx := make(map[string]int, len(resources))
	for i, r := range resources {
		deviceResIdx[r.ID] = i
	}

	rawRows := newRawCollector(col.wantRaw)
	local := map[string]bool{}           // resource IDs of this site.
	localSystemIP := map[string]string{} // system IP -> local device ID.
	ifaceIndex := map[string]string{}    // "{system-ip}:{color}" -> local interface ID, "" = ambiguous.
	deviceIDToResID := map[string]string{}
	for _, o := range s.devices {
		resType, ok := personalityToType[o.Personality]
		if !ok {
			continue
		}
		deviceKey := deviceNativeKey(o.Device, resType)
		if deviceKey == "" {
			continue
		}
		resID := resourceID(deviceKey)
		idx, ok := deviceResIdx[resID]
		if !ok {
			continue
		}
		local[resID] = true
		if o.DeviceID != "" {
			deviceIDToResID[o.DeviceID] = resID
		}
		if o.raw != nil {
			rawRows.attachOne(&resources[idx], "raw", o.raw)
		}
		if o.SystemIP == "" {
			continue
		}
		if x, ok := col.fabric.device(o.SystemIP); ok && x.id == resID {
			localSystemIP[o.SystemIP] = resID
		}
		if o.ifacesOK {
			ifaceResources, containsConns, tunnelIndex := TransformInterfaces(resID, resType, deviceKey, o.SystemIP, o.ifaces, o.wan)
			for _, r := range ifaceResources {
				local[r.ID] = true
			}
			resources = append(resources, ifaceResources...)
			connections = append(connections, containsConns...)
			for k, v := range tunnelIndex {
				addTunnelEndpoint(ifaceIndex, k, v)
			}
		}
		rawRows.attachList(&resources[idx], "interfaces_raw", o.ifacesRaw)
		rawRows.attachList(&resources[idx], "wan_interfaces_raw", o.wanRaw)
		rawRows.attachList(&resources[idx], "omp_peers_raw", o.peersRaw)
	}

	siteNames := map[string]string{}
	for _, other := range col.sites {
		if other.id != "" {
			siteNames[other.id] = other.name
		}
	}
	neighbors := newNeighborSet(cfg.Host, siteNames)

	// resolveDevice maps a system IP to a local device or a neighbor
	// candidate; false for an unknown or ambiguous system IP.
	resolveDevice := func(systemIP string) (string, bool) {
		if id, ok := localSystemIP[systemIP]; ok {
			return id, true
		}
		x, ok := col.fabric.device(systemIP)
		if !ok {
			return "", false
		}
		return neighbors.proposeDevice(x), true
	}

	// OMP: global links touching this site, plus this site's devices
	// own peer lists, reconciled into one connection per pair.
	var omp []ompObservation
	for _, l := range col.ompLinks {
		_, aLocal := localSystemIP[l.ASystemIP]
		_, bLocal := localSystemIP[l.BSystemIP]
		if !aLocal && !bLocal {
			continue // another site's session.
		}
		a, okA := resolveDevice(l.ASystemIP)
		b, okB := resolveDevice(l.BSystemIP)
		if !okA || !okB {
			stats.unresolvedOMP++
			continue
		}
		omp = append(omp, ompObservation{a: a, b: b, state: l.State, source: opOMPLinks})
	}
	// Peer lists of every collected device, so a session seen from both
	// ends (for example an edge and its controller, both selected) is
	// reconciled the same way in every site document that carries it.
	for _, other := range col.sites {
		for _, o := range other.devices {
			for _, p := range o.peers {
				_, selfLocal := localSystemIP[o.SystemIP]
				_, peerLocal := localSystemIP[p.Peer]
				if !selfLocal && !peerLocal {
					continue
				}
				self, okA := resolveDevice(o.SystemIP)
				peer, okB := resolveDevice(p.Peer)
				if !okA || !okB {
					if selfLocal {
						stats.unresolvedOMP++
					}
					continue
				}
				omp = append(omp, ompObservation{a: self, b: peer, state: p.State, source: opOMPPeers})
			}
		}
	}
	ompConns := TransformOMP(omp)

	// Tunnels: the local end resolves through this site's interfaces;
	// the remote end through the peer device's WAN interface listing.
	var tunnelConns []sdk.Connection
	if s.topologyOK {
		resolveTunnelEnd := func(key string) (string, bool) {
			if id, ok := ifaceIndex[key]; ok {
				return id, id != ""
			}
			ip, color := splitTunnelKey(key)
			if _, isLocal := localSystemIP[ip]; isLocal {
				return "", false // a local device without that interface.
			}
			x, ok := col.fabric.device(ip)
			if !ok {
				return "", false
			}
			var ifname string
			for _, w := range col.wanBySystemIP[ip] {
				if w.Color != color || w.Interface == "" {
					continue
				}
				if ifname != "" && ifname != w.Interface {
					return "", false // two interfaces on one color: ambiguous.
				}
				ifname = w.Interface
			}
			if ifname == "" {
				return "", false
			}
			return neighbors.proposeInterface(x, ifname, color), true
		}
		var tstats tunnelStats
		tunnelConns, tstats = TransformTunnels(s.topology, resolveTunnelEnd)
		stats.unresolvedTunnels, stats.unparsedTunnels = tstats.unresolved, tstats.unparsed
		// Keep only tunnels with at least one end in this site.
		kept := tunnelConns[:0]
		for _, c := range tunnelConns {
			if local[c.Source] || local[c.Target] {
				kept = append(kept, c)
			}
		}
		tunnelConns = kept

		// Each entry is matched back to its device resource (see
		// topologyOwner); an entry with no owner is counted in
		// raw_omitted, never dropped silently.
		if col.wantRaw && len(memberDeviceIDs(resources, local)) > 0 {
			fallback := memberDeviceIDs(resources, local)[0]
			if len(s.topology) != len(s.topologyRaw) {
				rawRows.omit(fallback, "site_topology_raw", rawOmitUnparseable, len(s.topologyRaw))
			} else {
				for i, st := range s.topology {
					if resID := topologyOwner(st, deviceIDToResID, localSystemIP); resID != "" {
						rawRows.attachOne(&resources[deviceResIdx[resID]], "site_topology_raw", s.topologyRaw[i])
					} else {
						rawRows.omit(fallback, "site_topology_raw", rawOmitUnowned, 1)
					}
				}
			}
		}
	}
	if len(ompConns) > 0 {
		log.Info("OMP peering connections resolved", "count", len(ompConns))
	}
	if len(tunnelConns) > 0 {
		log.Info("SD-WAN tunnel connections resolved", "count", len(tunnelConns))
	}
	connections = append(connections, ompConns...)
	connections = append(connections, tunnelConns...)

	neighbors.use(ompConns)
	neighbors.use(tunnelConns)
	nRes, nConns, nDev, nIf := neighbors.build()
	stats.neighborDevices, stats.neighborInterfaces = nDev, nIf
	rawRows.finish(resources)

	// The site group lists the site's own resources only.
	memberIDs := make([]string, 0, len(local))
	for _, r := range resources {
		if local[r.ID] {
			memberIDs = append(memberIDs, r.ID)
		}
	}
	setProvenance(resources, s.name, cfg.Host)
	// A neighbor never duplicates one of the site's own resources (for
	// example two inventory devices sharing a serial number).
	for _, r := range nRes {
		if !local[r.ID] {
			resources = append(resources, r)
		}
	}
	seenConn := make(map[string]bool, len(connections))
	for _, c := range connections {
		seenConn[c.ID] = true
	}
	for _, c := range nConns {
		if !seenConn[c.ID] {
			connections = append(connections, c)
		}
	}

	var groups []sdk.Group
	if g, ok := TransformSiteGroup(s.id, s.name, devices, memberIDs); ok {
		groups = append(groups, g)
	}

	// Documentation keeps only its reviewed field set (purpose.go).
	applyPurpose(cfg.Purpose, resources, connections, groups)

	// log-and-redact and off never emit a detected secret: the producer
	// replaces every detected value before Build (the SDK only logs and
	// flags in log-and-redact), and verifyRedacted refuses a document
	// that still carries one. fail-closed keeps the values and lets
	// Build abort.
	if cfg.SafeFailureMode != sdk.FailClosed {
		if n := redactElements(resources, connections, groups); n > 0 {
			log.Warn("secret values redacted before build", "count", n)
		}
	}

	scope := sdk.Scope{
		Providers: []string{providerName},
		Purpose:   cfg.Purpose,
		Clusters:  []string{cfg.Host},
	}
	if s.name != "" {
		scope.Sites = []string{s.name}
	}

	builder := sdk.NewDocumentBuilder(ctx).
		WithGenerator(generatorName, generatorVersion, generatorURL).
		WithScope(scope)
	for _, r := range resources {
		builder.AddResource(r)
	}
	for _, c := range connections {
		builder.AddConnection(c)
	}
	for _, g := range groups {
		builder.AddGroup(g)
	}
	doc, err := builder.Build()
	if err != nil {
		return nil, err
	}
	meta := col.siteCoverage(s).metadata()
	meta["graph"] = stats.metadata()
	doc.Metadata.Extensions = map[string]any{extensionKey: meta}
	if err := verifyRedacted(doc, cfg.SafeFailureMode); err != nil {
		return nil, err
	}
	return doc, nil
}

// topologyOwner finds the local device a site topology entry belongs
// to: by the Manager device-id ("device-id" here, "deviceId" in GET
// /dataservice/device), then by that value read as a system IP (some
// releases report the system IP there), then by the system IP of the
// entry's circuits. "" when none matches.
func topologyOwner(st SiteTopologyDevice, byDeviceID, bySystemIP map[string]string) string {
	if id := byDeviceID[st.DeviceID]; id != "" {
		return id
	}
	if id := bySystemIP[st.DeviceID]; id != "" {
		return id
	}
	for _, c := range st.Circuits {
		if id := bySystemIP[c.SystemIP]; id != "" {
			return id
		}
	}
	return ""
}

// memberDeviceIDs returns the IDs of the site's own device resources
// (not interfaces), in resource order.
func memberDeviceIDs(resources []sdk.Resource, local map[string]bool) []string {
	var out []string
	for _, r := range resources {
		if local[r.ID] && !strings.HasSuffix(r.Type, ".port") && r.Type != "network.interface" {
			out = append(out, r.ID)
		}
	}
	return out
}

// setProvenance records where each of the site's own resources comes
// from: provider.site is the site name (left empty for the unsited
// bucket) and provider.system the Manager the run collected from.
func setProvenance(resources []sdk.Resource, siteName, host string) {
	for i := range resources {
		resources[i].Provider.Site = siteName
		resources[i].Provider.System = host
	}
}

// deviceRawIndex builds a Device.DeviceID -> raw JSON object lookup
// from GetDevicesWithRaw's parallel devices/raws slices.
func deviceRawIndex(devices []Device, raws []json.RawMessage) map[string]json.RawMessage {
	if len(devices) != len(raws) {
		return nil
	}
	idx := make(map[string]json.RawMessage, len(devices))
	for i, d := range devices {
		if d.DeviceID != "" {
			idx[d.DeviceID] = raws[i]
		}
	}
	return idx
}
