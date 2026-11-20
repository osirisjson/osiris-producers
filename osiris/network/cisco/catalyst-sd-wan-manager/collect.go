// collect.go - Bounded, independent collection stage of the Cisco
// Catalyst SD-WAN Manager OSIRIS JSON Producer.
//
// collect runs every API operation for the selected sites once and
// returns the observations together with their coverage record. It
// talks to the controller only through managerAPI, so tests and later
// stages can inject a fake. Building and publishing documents is a
// separate stage (document.go, output.go): a collection is never
// written as it is gathered.
//
// Operation classes:
//
//   - essential: the device inventory (GET /dataservice/device). Its
//     failure aborts the run, because no document can be built without
//     it;
//   - optional: every other operation. A failure is logged with its
//     category, recorded in coverage and leaves the affected document
//     partial. Optional operations are independent of each other: a
//     failed interface listing for a device does not skip that
//     device's WAN interfaces or OMP peers.
//
// GET /dataservice/tenant is no longer called: the tenants a
// credential can see are not the accounts this producer collected
// from, so they no longer feed metadata.scope.accounts.
//
// OSIRIS JSON Producer for Cisco Catalyst SD-WAN Manager introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification
package catalystsdwanmanager

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"sync"
)

// siteNamer resolves a site-id to its display name.
type siteNamer interface {
	GetSiteName(siteID string) (string, error)
}

// managerAPI is the controller surface the collection stage uses.
// *Client implements it.
type managerAPI interface {
	siteNamer
	GetDevicesWithRaw() ([]Device, []json.RawMessage, error)
	GetOMPLinks() ([]OMPLink, error)
	GetDeviceInterfacesWithRaw(systemIP string) ([]Interface, []json.RawMessage, error)
	GetWANInterfacesWithRaw(systemIP string) ([]WANInterface, []json.RawMessage, error)
	GetOMPPeersWithRaw(systemIP string) ([]OMPPeer, []json.RawMessage, error)
	GetSiteTopologyMonitorWithRaw(siteID string) ([]SiteTopologyDevice, []json.RawMessage, error)
	GetPeerWANInterfaces(systemIP string) ([]WANInterface, error)
}

var _ managerAPI = (*Client)(nil)

// deviceObs is one device and everything collected for it.
type deviceObs struct {
	Device
	raw json.RawMessage

	ifaces    []Interface
	ifacesRaw []json.RawMessage
	ifacesOK  bool // the interface listing answered (possibly empty).

	wan    []WANInterface
	wanRaw []json.RawMessage

	peers    []OMPPeer
	peersRaw []json.RawMessage
}

// siteObs is one selected site and its observations.
type siteObs struct {
	id, name string
	devices  []*deviceObs

	topology    []SiteTopologyDevice
	topologyRaw []json.RawMessage
	topologyOK  bool

	cov *coverage // operations scoped to this site.
}

// collection is the result of one collection run.
type collection struct {
	ompLinks []OMPLink
	sites    []*siteObs
	global   *coverage // controller-wide operations.
	wantRaw  bool

	// fabric indexes the whole inventory for cross-site endpoint
	// resolution (neighbors.go), wanBySystemIP holds every WAN
	// interface listing collected, for selected devices and for tunnel
	// peers outside the selected sites.
	fabric        *fabricIndex
	wanBySystemIP map[string][]WANInterface
	// unidentified counts inventory devices with a known personality
	// but no native identity (no serial, uuid or deviceId): they are
	// never emitted.
	unidentified int
}

// siteCoverage is the coverage record of one site's document: the
// controller-wide operations plus the site's own.
func (col *collection) siteCoverage(s *siteObs) *coverage {
	return merged(col.global, s.cov)
}

// collect gathers the inventory, selects sites and collects every
// selected site's per-device and per-site operations (see the file
// comment for operation classes).
func collect(api managerAPI, cfg *Config, logger *slog.Logger) (*collection, error) {
	col := &collection{
		global:        &coverage{},
		wantRaw:       cfg.Purpose == "audit" && cfg.IncludeRawBody,
		wanBySystemIP: map[string][]WANInterface{},
	}

	devices, devicesRaw, err := api.GetDevicesWithRaw()
	col.global.record(opDevice, len(devices), err)
	if err != nil {
		return nil, fmt.Errorf("fetching device inventory failed: %w", err)
	}
	var rawByID map[string]json.RawMessage
	if col.wantRaw {
		rawByID = deviceRawIndex(devices, devicesRaw)
	}
	col.fabric = newFabricIndex(devices)
	for _, d := range devices {
		if rt, ok := personalityToType[d.Personality]; ok && deviceNativeKey(d, rt) == "" {
			col.unidentified++
		}
	}
	if col.unidentified > 0 {
		logger.Warn("devices without native identity skipped", "count", col.unidentified)
	}

	// One controller-wide call covers every site's OMP peering;
	// each site document keeps the sessions of its own devices.
	links, err := api.GetOMPLinks()
	col.global.record(opOMPLinks, len(links), err)
	if err != nil {
		logger.Warn("OMP link listing failed (continuing without OMP peering connections)", "category", failureCategory(err), "err", err)
	} else {
		col.ompLinks = links
	}

	groups := GroupDevicesBySiteID(devices)
	selected, names, err := resolveSiteSelection(cfg, groups, api, logger)
	if err != nil {
		return nil, fmt.Errorf("resolving site selection: %w", err)
	}
	logger.Info("site selection resolved", "sites", len(selected))

	for _, siteID := range selected {
		s := &siteObs{id: siteID, cov: &coverage{}}
		col.sites = append(col.sites, s)

		if siteID == "" {
			s.cov.skip(opSiteName, "unsited")
		} else if name, ok := names[siteID]; ok {
			s.name = name
		} else {
			name, err := api.GetSiteName(siteID)
			s.cov.record(opSiteName, boolCount(name != ""), err)
			switch {
			case err != nil:
				logger.Warn("site name lookup failed (using site-id instead)", "site_id", siteID, "category", failureCategory(err), "err", err)
				s.name = siteID
			case name == "":
				s.name = siteID
			default:
				s.name = name
			}
		}

		log := logger.With("site_id", siteID, "site", siteLabel(siteID, s.name))
		collectSite(api, col, s, groups[siteID], rawByID, log)
	}
	collectNeighborWAN(api, col, logger)
	return col, nil
}

// collectNeighborWAN fetches the WAN interface listing of every tunnel
// peer outside the selected sites, once per device, so the remote end
// of a cross-site tunnel can be resolved to its transport interface
// (neighbors.go). Peers that are unknown or ambiguous in the inventory
// are not fetched. Each lookup is recorded in the coverage of every
// site whose tunnels need that peer, so a document's record counts only
// the lookups behind it.
func collectNeighborWAN(api managerAPI, col *collection, logger *slog.Logger) {
	selected := map[string]bool{}
	for _, s := range col.sites {
		for _, o := range s.devices {
			if o.SystemIP != "" {
				selected[o.SystemIP] = true
			}
		}
	}
	needBy := map[*siteObs][]string{}
	need := map[string]bool{}
	for _, s := range col.sites {
		mine := map[string]bool{}
		for _, dev := range s.topology {
			for _, c := range dev.Circuits {
				for _, t := range c.Tunnels {
					a, b, ok := parseTunnelEndpoints(t.Name)
					if !ok {
						continue
					}
					for _, k := range []string{a, b} {
						ip, _ := splitTunnelKey(k)
						if selected[ip] || mine[ip] {
							continue // its own listing was already attempted.
						}
						if _, known := col.fabric.device(ip); known {
							mine[ip] = true
							need[ip] = true
							needBy[s] = append(needBy[s], ip)
						}
					}
				}
			}
		}
	}
	ips := make([]string, 0, len(need))
	for ip := range need {
		ips = append(ips, ip)
	}
	sort.Strings(ips)
	if len(ips) == 0 {
		return
	}
	logger.Info("collecting WAN interfaces of tunnel peers outside the selected sites", "devices", len(ips), "workers", siteNameWorkers)

	type result struct {
		rows int
		err  error
	}
	results := make(map[string]result, len(ips))

	// Bounded worker pool; the requests/second cap is the client's
	// --site-name-rate throttle (see GetPeerWANInterfaces).
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, siteNameWorkers)
	done := 0
	for _, ip := range ips {
		wg.Add(1)
		sem <- struct{}{}
		go func(ip string) {
			defer wg.Done()
			defer func() { <-sem }()
			wan, err := api.GetPeerWANInterfaces(ip)

			mu.Lock()
			defer mu.Unlock()
			results[ip] = result{len(wan), err}
			if err != nil {
				logger.Warn("neighbor WAN interface listing failed (its tunnels stay unresolved)", "category", failureCategory(err), "err", err)
			} else {
				col.wanBySystemIP[ip] = wan
			}
			done++
			if done%25 == 0 || done == len(ips) {
				logger.Info("tunnel peer WAN interfaces collected", "done", done, "total", len(ips))
			}
		}(ip)
	}
	wg.Wait()

	for s, list := range needBy {
		for _, ip := range list {
			r := results[ip]
			s.cov.record(opNeighborWAN, r.rows, r.err)
		}
	}
}

// collectSite runs the per-device and per-site operations of one site.
func collectSite(api managerAPI, col *collection, s *siteObs, devices []Device, rawByID map[string]json.RawMessage, log *slog.Logger) {
	var withIP int
	for _, d := range devices {
		if d.SystemIP != "" {
			withIP++
		}
	}
	if withIP > 0 {
		log.Info("collecting device interfaces and connections", "devices", withIP)
	}

	for _, d := range devices {
		o := &deviceObs{Device: d}
		if col.wantRaw {
			o.raw = rawByID[d.DeviceID]
		}
		s.devices = append(s.devices, o)

		if d.SystemIP == "" {
			continue
		}
		if _, ok := personalityToType[d.Personality]; !ok {
			continue // TransformDevices skips this device too.
		}
		dlog := log.With("device", d.HostName)

		ifaces, ifacesRaw, err := api.GetDeviceInterfacesWithRaw(d.SystemIP)
		s.cov.record(opDeviceInterface, len(ifaces), err)
		if err != nil {
			dlog.Warn("interface listing failed (continuing without this device's interfaces)", "category", failureCategory(err), "err", err)
		} else {
			o.ifaces, o.ifacesRaw, o.ifacesOK = ifaces, ifacesRaw, true
		}

		wan, wanRaw, err := api.GetWANInterfacesWithRaw(d.SystemIP)
		s.cov.record(opWANInterface, len(wan), err)
		if err != nil {
			dlog.Warn("WAN interface listing failed (continuing with base interface data only)", "category", failureCategory(err), "err", err)
		} else {
			o.wan, o.wanRaw = wan, wanRaw
			col.wanBySystemIP[d.SystemIP] = wan
		}

		peers, peersRaw, err := api.GetOMPPeersWithRaw(d.SystemIP)
		s.cov.record(opOMPPeers, len(peers), err)
		if err != nil {
			dlog.Warn("OMP peer listing failed (continuing without this device's OMP peering)", "category", failureCategory(err), "err", err)
		} else {
			o.peers, o.peersRaw = peers, peersRaw
		}

		if o.ifacesOK {
			dlog.Info("device interfaces collected", "rows", len(ifaces))
		}
	}

	// GET /dataservice/topology/monitor/site/{siteId} has no siteId to
	// call with for the unsited group.
	if s.id == "" {
		s.cov.skip(opSiteTopology, "unsited")
		return
	}
	topo, topoRaw, err := api.GetSiteTopologyMonitorWithRaw(s.id)
	s.cov.record(opSiteTopology, len(topo), err)
	if err != nil {
		log.Warn("site topology monitor failed (continuing without SD-WAN tunnel connections)", "category", failureCategory(err), "err", err)
		return
	}
	s.topology, s.topologyRaw, s.topologyOK = topo, topoRaw, true
}

// boolCount is 1 for true, for operations that return a single value.
func boolCount(b bool) int {
	if b {
		return 1
	}
	return 0
}
