// Package arubacentral implements the HPE Aruba Networking Central
// OSIRIS JSON producer.
// Collects from each site resources like devices, wireless and topology
// inventory from an Aruba Central account via the API Gateway or
// GreenLake Personal API client credential and generates
// an OSIRIS JSON document.
//
// Aruba Central is a multi-tenant SaaS network management platform: one
// OAuth2 credential authenticates against one customer account, which
// may span many sites.
// Every run, one site or many, writes one OSIRIS JSON document per site
// into the same output hierarchy (see config.go's OutputPath):
//
//	<output-dir>/<site-name>/hpe-arubacentral-<timestamp>-<site-name>.json
//
// Operating modes:
//
//	Single site (interactive login): osirisjson-producer arubacentral
//	Multi site cron/CI (all): osirisjson-producer arubacentral --all --token-file ./arubacentral-token.json -o ./output
//
// <output-dir> defaults to "osirisjson-hpe-arubacentral" (in the current
// directory) when -o/--output is not given. Re-running from the same
// working directory or with -o pointing at the same directory reuses
// the existing hierarchy and adds each site's new timestamped file
// alongside prior ones; running from a fresh working directory (or a
// new -o target) builds the hierarchy from scratch.
//
// OSIRIS JSON Producer for HPE Aruba Networking Central introduction:
// [OSIRIS-JSON-HPE-ARUBA-NETWORKING]: https://docs.osirisjson.org/osiris-producers/network/hpe-aruba-networking-central
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification
package arubacentral

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.osirisjson.org/producers/pkg/sdk"
)

const (
	generatorName    = "osirisjson-producer-hpe-arubacentral"
	generatorVersion = "0.1.0"
	generatorURL     = "https://docs.osirisjson.org/osiris-producers/network/hpe-aruba-networking-central"
)

// Producer implements the OSIRIS JSON sdk.Producer interface
// for Aruba Central.
type Producer struct {
	cfg    *Config
	client *Client // injectable for testing.
}

// NewProducer creates an Aruba Central producer for the given
// account config.
func NewProducer(cfg *Config) *Producer {
	return &Producer{cfg: cfg}
}

// Collect queries Aruba Central via the API Gateway and builds an
// OSIRIS JSON document.
func (p *Producer) Collect(ctx *sdk.Context) (*sdk.Document, error) {
	client := p.client
	if client == nil {
		client = NewClient(p.cfg, ctx.Logger)
	}

	siteFilter := map[string]bool{}
	for _, s := range p.cfg.Sites {
		siteFilter[s] = true
	}
	keepSite := func(siteName string) bool {
		if len(siteFilter) == 0 {
			return true
		}
		return siteFilter[siteName]
	}

	var allResources []sdk.Resource
	var allConnections []sdk.Connection
	var allGroups []sdk.Group

	// wantRawBody gates attaching the true, unmodified API response
	// body per device to extensions[osiris.hpe.arubacentral].raw a
	// lossless fallback for fields not yet modeled, available in audit
	// purpose only (see --include-raw-body).
	wantRawBody := p.cfg.Purpose == "audit" && p.cfg.IncludeRawBody

	ctx.Logger.Info("starting Aruba Central collection", "site_filter", p.cfg.Sites, "purpose", p.cfg.Purpose)

	// Scope management: sites and device groups. Best-effort these
	// endpoint paths are inferred (see client.go doc comments) and a
	// failure here MUST NOT abort the whole collection.
	ctx.Logger.Info("collecting sites and device groups")
	sites, err := client.ListSites()
	if err != nil {
		ctx.Logger.Warn("sites collection failed (continuing)", "err", err)
	}
	var filteredSites []Site
	for _, s := range sites {
		if keepSite(s.ScopeName) {
			filteredSites = append(filteredSites, s)
		}
	}

	targetSiteIDs := map[string]bool{}
	for _, s := range filteredSites {
		if s.ScopeID != "" {
			targetSiteIDs[s.ScopeID] = true
		}
	}
	keepDeviceSite := func(siteName, siteID string) bool {
		if len(siteFilter) == 0 {
			return true
		}
		return siteFilter[siteName] || targetSiteIDs[siteID]
	}

	// targetSiteIDList: server-side siteId scoping for endpoints that
	// support it (see ListAPsWithRaw), so a filtered run fetches only
	// the requested site's devices instead of the whole account. Left
	// nil (no server-side scoping) when no --site filter is active, so
	// the unfiltered fallback still lists every device in the account.
	var targetSiteIDList []string
	if len(siteFilter) > 0 {
		for id := range targetSiteIDs {
			targetSiteIDList = append(targetSiteIDList, id)
		}
	}

	siteResources, siteNameToID := TransformSites(filteredSites)
	siteResIdx := indexResources(siteResources)

	// siteGroups/siteGroupIdx/siteGroupNameToID: the "logical.site"
	// presentation-layer counterpart to siteResources (see
	// TransformSiteGroups doc comment). Devices are wired in as members
	// via WireDeviceToGroup below, alongside a "contains" connection
	// from the site resource, the authoritative graph edge per OSIRIS
	// JSON spec section 6.4.3, to each switch/AP/gateway found at that
	// site. Clients are deliberately not wired into either
	// representation: they are numerous and already carry their site
	// via provider.site, and site device_count semantics refer to
	// infrastructure, not end-user clients.
	siteGroups, siteGroupNameToID := TransformSiteGroups(filteredSites)
	siteGroupIdx := groupIndex(siteGroups)

	if healthBySite, err := client.ListSitesHealth(); err != nil {
		ctx.Logger.Warn("site health collection failed (continuing)", "err", err)
	} else {
		for _, h := range healthBySite {
			id, ok := siteNameToID[h.SiteName]
			if !ok {
				continue
			}
			if i, ok := siteResIdx[id]; ok {
				EnrichSiteHealth(&siteResources[i], h, p.cfg.Purpose)
			}
		}
	}

	if deviceHealthBySite, err := client.ListSitesDeviceHealth(); err != nil {
		ctx.Logger.Warn("site device health collection failed (continuing)", "err", err)
	} else {
		for _, h := range deviceHealthBySite {
			id, ok := siteNameToID[h.SiteName]
			if !ok {
				continue
			}
			if i, ok := siteResIdx[id]; ok {
				EnrichSiteDeviceHealth(&siteResources[i], h, p.cfg.Purpose)
			}
		}
	}

	// Isolated-device report per site: no confirmed item field names
	// (see ListIsolatedDevices doc comment), so attached as raw data on
	// the site resource rather than modeled as named resources
	// audit + raw-body only.
	if wantRawBody {
		for _, s := range filteredSites {
			siteID := s.ScopeID
			if siteID == "" {
				siteID = s.ID
			}
			if siteID == "" {
				continue
			}
			isolated, err := client.ListIsolatedDevices(siteID)
			if err != nil {
				ctx.Logger.Warn("isolated devices collection failed (continuing)", "site", s.ScopeName, "err", err)
				continue
			}
			if len(isolated) == 0 {
				continue
			}
			if id, ok := siteNameToID[s.ScopeName]; ok {
				if i, ok := siteResIdx[id]; ok {
					setExtension(&siteResources[i], "isolated_devices_raw", isolated)
				}
			}
		}
	}
	allResources = append(allResources, siteResources...)

	deviceGroupsRaw, err := client.ListDeviceGroups()
	if err != nil {
		ctx.Logger.Warn("device groups collection failed (continuing)", "err", err)
	}
	groupResources, groupNameToID := TransformDeviceGroups(deviceGroupsRaw)
	groupIdx := groupIndex(groupResources)

	// deviceIDMap accumulates serial -> resourceID across every device
	// type, used to wire neighbor adjacency and client connections
	// regardless of which device role the other end turns out to be.
	deviceIDMap := map[string]string{}

	// --- Switches ---
	ctx.Logger.Info("collecting switches")
	switches, switchesRaw, err := client.ListSwitchesWithRaw()
	if err != nil {
		return nil, fmt.Errorf("listing switches: %w", err)
	}
	var filteredSwitches []Switch
	var filteredSwitchesRaw []json.RawMessage
	for i, sw := range switches {
		if keepDeviceSite(sw.SiteName, sw.SiteID) {
			filteredSwitches = append(filteredSwitches, sw)
			filteredSwitchesRaw = append(filteredSwitchesRaw, switchesRaw[i])
		}
	}
	switchResources, switchIDMap := TransformSwitches(filteredSwitches)
	switchResIdx := indexResources(switchResources)
	maps.Copy(deviceIDMap, switchIDMap)
	ctx.Logger.Info("found switches", "count", len(filteredSwitches))

	for i, sw := range filteredSwitches {
		switchID, ok := switchIDMap[sw.SerialNumber]
		if !ok {
			continue
		}
		ctx.Logger.Info("processing switch", "index", i+1, "total", len(filteredSwitches), "serial", sw.SerialNumber, "name", sw.DeviceName)

		if siteID, ok := siteNameToID[sw.SiteName]; ok {
			allConnections = append(allConnections, containsConnection(siteID, switchID, sw.DeviceName))
		}
		WireDeviceToGroup(siteGroups, siteGroupIdx, siteGroupNameToID, sw.SiteName, switchID)

		// For a switch stack, /interfaces and /hardware-categories 404
		// when queried by a non-conductor member's own serial: both
		// endpoints only work against the conductor, their response
		// then covers every physical member, each item carrying its own
		// serialNumber. So only the conductor (or a standalone,
		// non-stacked switch) queries these two; the results are routed
		// back to each member's own resource by that field.
		isStackMember := sw.Deployment == "Stack" && sw.SwitchRole != "Conductor"

		if isStackMember {
			// Nothing to query here: covered by the stack's conductor iteration.
		} else if ifaces, err := client.ListSwitchInterfaces(sw.SerialNumber); err != nil {
			ctx.Logger.Warn("switch interfaces query failed (continuing)", "serial", sw.SerialNumber, "err", err)
		} else {
			ifRes, ifConns, ifNameToID := TransformSwitchInterfaces(switchIDMap, sw.SerialNumber, ifaces)
			allResources = append(allResources, ifRes...)
			allConnections = append(allConnections, ifConns...)

			if vlans, err := client.ListSwitchVLANs(sw.SerialNumber); err != nil {
				ctx.Logger.Warn("switch VLANs query failed (continuing)", "serial", sw.SerialNumber, "err", err)
			} else {
				allGroups = append(allGroups, TransformSwitchVLANs(sw.SerialNumber, vlans, ifNameToID)...)
			}

			if lags, err := client.ListSwitchLAG(sw.SerialNumber); err != nil {
				ctx.Logger.Warn("switch LAG query failed (continuing)", "serial", sw.SerialNumber, "err", err)
			} else {
				allGroups = append(allGroups, TransformSwitchLAGs(sw.SerialNumber, lags, ifNameToID)...)
			}
		}

		if isStackMember {
			// Nothing to query here: covered by the stack's conductor iteration.
		} else if hw, err := client.ListSwitchHardware(sw.SerialNumber); err != nil {
			ctx.Logger.Warn("switch hardware query failed (continuing)", "serial", sw.SerialNumber, "err", err)
		} else {
			EnrichSwitchHardwareForStack(switchResources, switchResIdx, switchIDMap, hw)
		}

		if stack, err := client.GetStackMembers(sw.SerialNumber); err == nil {
			if g := TransformSwitchStack(sw.SerialNumber, stack, switchIDMap); g != nil {
				allGroups = append(allGroups, *g)
			}
		}

		if vsx, err := client.GetSwitchVSX(sw.SerialNumber); err == nil {
			if conn, stub := TransformSwitchVSX(switchID, vsx, switchIDMap); conn != nil {
				allConnections = append(allConnections, *conn)
				if stub != nil {
					allResources = append(allResources, *stub)
				}
			}
		}

		if summary, err := client.GetConfigHealthSummary(sw.SerialNumber); err == nil {
			issues, _ := client.GetConfigHealthIssues(sw.SerialNumber)
			if i, ok := switchResIdx[switchID]; ok {
				EnrichConfigHealth(&switchResources[i], summary, issues, p.cfg.Purpose)
			}
			WireDeviceToGroup(groupResources, groupIdx, groupNameToID, deviceGroupName(summary), switchID)
		}

		if neighbors, err := client.ListNeighbors(sw.SerialNumber); err != nil {
			ctx.Logger.Warn("switch neighbors query failed (continuing)", "serial", sw.SerialNumber, "err", err)
		} else {
			conns, stubs := TransformNeighbors(switchID, sw.SerialNumber, neighbors, deviceIDMap)
			enrichUnmanagedNeighborStubs(client, neighbors, stubs, wantRawBody)
			allConnections = append(allConnections, conns...)
			allResources = append(allResources, stubs...)
		}
	}
	if wantRawBody {
		attachRawBodies(switchResources, switchIDMap, switchSerials(filteredSwitches), filteredSwitchesRaw)
	}
	allResources = append(allResources, switchResources...)

	// --- Access points ---
	ctx.Logger.Info("collecting access points")
	aps, apsRaw, err := client.ListAPsWithRaw(targetSiteIDList)
	if err != nil {
		return nil, fmt.Errorf("listing access points: %w", err)
	}
	var filteredAPs []AccessPoint
	var filteredAPsRaw []json.RawMessage
	for i, ap := range aps {
		if keepDeviceSite(ap.SiteName, ap.SiteID) {
			filteredAPs = append(filteredAPs, ap)
			filteredAPsRaw = append(filteredAPsRaw, apsRaw[i])
		}
	}
	apResources, apIDMap := TransformAccessPoints(filteredAPs)
	apResIdx := indexResources(apResources)
	maps.Copy(deviceIDMap, apIDMap)
	ctx.Logger.Info("found access points", "count", len(filteredAPs))

	wlans, err := client.ListWLANs()
	if err != nil {
		ctx.Logger.Warn("WLANs collection failed (continuing)", "err", err)
	}
	wlanResources, wlanNameToID := TransformWLANs(wlans)
	allResources = append(allResources, wlanResources...)

	radios, err := client.ListRadios()
	if err != nil {
		ctx.Logger.Warn("radios collection failed (continuing)", "err", err)
	}
	radioResources, radioConns, radioIDMap := TransformRadios(radios, apIDMap)
	allResources = append(allResources, radioResources...)
	allConnections = append(allConnections, radioConns...)

	bssids, err := client.ListBSSIDs()
	if err != nil {
		ctx.Logger.Warn("BSSIDs collection failed (continuing)", "err", err)
	}
	bssidResources, bssidConns := TransformBSSIDs(bssids, radioIDMap, wlanNameToID)
	allResources = append(allResources, bssidResources...)
	allConnections = append(allConnections, bssidConns...)

	swarms, err := client.ListSwarms()
	if err != nil {
		ctx.Logger.Warn("swarms collection failed (continuing)", "err", err)
	}
	allGroups = append(allGroups, TransformSwarms(swarms, apIDMap)...)

	for i, ap := range filteredAPs {
		apID, ok := apIDMap[ap.SerialNumber]
		if !ok {
			continue
		}
		ctx.Logger.Info("processing access point", "index", i+1, "total", len(filteredAPs), "serial", ap.SerialNumber, "name", ap.DeviceName)

		if siteID, ok := siteNameToID[ap.SiteName]; ok {
			allConnections = append(allConnections, containsConnection(siteID, apID, ap.DeviceName))
		}
		WireDeviceToGroup(siteGroups, siteGroupIdx, siteGroupNameToID, ap.SiteName, apID)

		if ports, err := client.ListAPPorts(ap.SerialNumber); err != nil {
			ctx.Logger.Warn("AP ports query failed (continuing)", "serial", ap.SerialNumber, "err", err)
		} else {
			portRes, portConns := TransformAPPorts(apID, ap.SerialNumber, ports)
			allResources = append(allResources, portRes...)
			allConnections = append(allConnections, portConns...)
		}

		if tunnels, err := client.ListAPTunnels(ap.SerialNumber); err != nil {
			ctx.Logger.Warn("AP tunnels query failed (continuing)", "serial", ap.SerialNumber, "err", err)
		} else {
			tunnelConns, tunnelStubs := TransformAPTunnels(apID, ap.SerialNumber, tunnels)
			allConnections = append(allConnections, tunnelConns...)
			allResources = append(allResources, tunnelStubs...)
		}

		if apWLANs, err := client.ListAPWLANs(ap.SerialNumber); err != nil {
			ctx.Logger.Warn("AP WLANs query failed (continuing)", "serial", ap.SerialNumber, "err", err)
		} else {
			allConnections = append(allConnections, TransformAPWLANConnections(apID, apWLANs, wlanNameToID)...)
		}

		if summary, err := client.GetConfigHealthSummary(ap.SerialNumber); err == nil {
			issues, _ := client.GetConfigHealthIssues(ap.SerialNumber)
			if i, ok := apResIdx[apID]; ok {
				EnrichConfigHealth(&apResources[i], summary, issues, p.cfg.Purpose)
			}
			WireDeviceToGroup(groupResources, groupIdx, groupNameToID, deviceGroupName(summary), apID)
		}

		if neighbors, err := client.ListNeighbors(ap.SerialNumber); err != nil {
			ctx.Logger.Warn("access point neighbors query failed (continuing)", "serial", ap.SerialNumber, "err", err)
		} else {
			conns, stubs := TransformNeighbors(apID, ap.SerialNumber, neighbors, deviceIDMap)
			enrichUnmanagedNeighborStubs(client, neighbors, stubs, wantRawBody)
			allConnections = append(allConnections, conns...)
			allResources = append(allResources, stubs...)
		}
	}
	if wantRawBody {
		attachRawBodies(apResources, apIDMap, apSerials(filteredAPs), filteredAPsRaw)
	}
	allResources = append(allResources, apResources...)

	// --- Gateways ---
	ctx.Logger.Info("collecting gateways")
	gateways, gatewaysRaw, err := client.ListGatewaysWithRaw()
	if err != nil {
		ctx.Logger.Warn("gateways collection failed (continuing)", "err", err)
	}
	var filteredGateways []Gateway
	var filteredGatewaysRaw []json.RawMessage
	for i, gw := range gateways {
		if keepDeviceSite(gw.SiteName, gw.SiteID) {
			filteredGateways = append(filteredGateways, gw)
			filteredGatewaysRaw = append(filteredGatewaysRaw, gatewaysRaw[i])
		}
	}
	gwResources, gwIDMap := TransformGateways(filteredGateways)
	gwResIdx := indexResources(gwResources)
	maps.Copy(deviceIDMap, gwIDMap)
	ctx.Logger.Info("found gateways", "count", len(filteredGateways))

	for i, gw := range filteredGateways {
		gwID, ok := gwIDMap[gw.SerialNumber]
		if !ok {
			continue
		}
		ctx.Logger.Info("processing gateway", "index", i+1, "total", len(filteredGateways), "serial", gw.SerialNumber, "name", gw.DeviceName)

		if siteID, ok := siteNameToID[gw.SiteName]; ok {
			allConnections = append(allConnections, containsConnection(siteID, gwID, gw.DeviceName))
		}
		WireDeviceToGroup(siteGroups, siteGroupIdx, siteGroupNameToID, gw.SiteName, gwID)

		if ports, err := client.ListGatewayPorts(gw.SerialNumber); err != nil {
			ctx.Logger.Warn("gateway ports query failed (continuing)", "serial", gw.SerialNumber, "err", err)
		} else {
			portRes, portConns := TransformGatewayPorts(gwID, gw.SerialNumber, ports)
			allResources = append(allResources, portRes...)
			allConnections = append(allConnections, portConns...)
		}

		if vlans, err := client.ListGatewayVLANs(gw.SerialNumber); err != nil {
			ctx.Logger.Warn("gateway VLANs query failed (continuing)", "serial", gw.SerialNumber, "err", err)
		} else {
			allGroups = append(allGroups, TransformGatewayVLANs(gw.SerialNumber, vlans)...)
		}

		if uplinks, err := client.ListGatewayUplinks(gw.SerialNumber); err != nil {
			ctx.Logger.Warn("gateway uplinks query failed (continuing)", "serial", gw.SerialNumber, "err", err)
		} else {
			uplinkConns, uplinkStubs := TransformGatewayUplinks(gwID, gw.SerialNumber, uplinks)
			allConnections = append(allConnections, uplinkConns...)
			allResources = append(allResources, uplinkStubs...)
		}

		if summary, err := client.GetConfigHealthSummary(gw.SerialNumber); err == nil {
			issues, _ := client.GetConfigHealthIssues(gw.SerialNumber)
			if i, ok := gwResIdx[gwID]; ok {
				EnrichConfigHealth(&gwResources[i], summary, issues, p.cfg.Purpose)
			}
			WireDeviceToGroup(groupResources, groupIdx, groupNameToID, deviceGroupName(summary), gwID)
		}

		if neighbors, err := client.ListNeighbors(gw.SerialNumber); err != nil {
			ctx.Logger.Warn("gateway neighbors query failed (continuing)", "serial", gw.SerialNumber, "err", err)
		} else {
			conns, stubs := TransformNeighbors(gwID, gw.SerialNumber, neighbors, deviceIDMap)
			enrichUnmanagedNeighborStubs(client, neighbors, stubs, wantRawBody)
			allConnections = append(allConnections, conns...)
			allResources = append(allResources, stubs...)
		}
	}
	if wantRawBody {
		attachRawBodies(gwResources, gwIDMap, gatewaySerials(filteredGateways), filteredGatewaysRaw)
	}
	allResources = append(allResources, gwResources...)

	// --- Clients ---
	ctx.Logger.Info("collecting clients")
	clients, err := client.ListClients()
	if err != nil {
		ctx.Logger.Warn("clients collection failed (continuing)", "err", err)
	}
	var filteredClients []ClientDevice
	for _, cl := range clients {
		if keepDeviceSite(cl.SiteName, cl.SiteID) {
			filteredClients = append(filteredClients, cl)
		}
	}
	ctx.Logger.Info("found clients", "count", len(filteredClients))
	clientResources, clientConns := TransformClients(filteredClients, deviceIDMap, p.cfg.Purpose)
	allResources = append(allResources, clientResources...)
	allConnections = append(allConnections, clientConns...)

	// Assemble the document.
	var clusters []string
	if p.cfg.Cluster != "" {
		clusters = []string{fmt.Sprintf("%s (%s)", p.cfg.Cluster, p.cfg.BaseURL)}
	}
	builder := sdk.NewDocumentBuilder(ctx).
		WithGenerator(generatorName, generatorVersion, generatorURL).
		WithScope(sdk.Scope{
			Providers: []string{providerName},
			Sites:     p.cfg.Sites,
			Clusters:  clusters,
			Purpose:   p.cfg.Purpose,
		})

	for _, r := range dedupeResources(allResources) {
		builder.AddResource(r)
	}
	for _, c := range dedupeConnections(allConnections) {
		builder.AddConnection(c)
	}
	// groupResources (device groups) and siteGroups are added
	// separately from allGroups (VLANs/LAGs/stacks/swarms) because
	// WireDeviceToGroup mutates both in place by index throughout the
	// collection above; all three must reach the builder for the
	// document to be complete.
	for _, g := range groupResources {
		builder.AddGroup(g)
	}
	for _, g := range siteGroups {
		builder.AddGroup(g)
	}
	for _, g := range allGroups {
		builder.AddGroup(g)
	}

	doc, err := builder.Build()
	if err != nil {
		return nil, fmt.Errorf("document build failed: %w", err)
	}

	ctx.Logger.Info("HPE Aruba Central collection complete",
		"resources", len(doc.Topology.Resources),
		"connections", len(doc.Topology.Connections),
		"groups", len(doc.Topology.Groups),
	)

	return doc, nil
}

// Run is the CLI entry point for the Aruba Central producer. It
// receives the arguments after "arubacentral"
// (e.g. ["--cluster", "eu", "--token-file", "./aruba.json"]).
func Run(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "--help", "-h", "help":
			printHelp()
			return nil
		case "template":
			return runTemplate(args[1:])
		}
	}

	cfg, err := ParseFlags(args)
	if err != nil {
		return err
	}

	return runExport(cfg)
}

// runExport collects and writes one OSIRIS JSON document per site into
// cfg.OutputDir, regardless of whether one site or many were resolved
// (see OutputPath and the package doc comment in config.go for the
// directory hierarchy and reuse behavior). Sites are collected
// independently, each into its own document, so metadata.scope always
// describes exactly one site, and so one slow or failing site cannot
// block the others.
//
// An empty cfg.Sites means the sites endpoint was unavailable and
// collection falls back to unfiltered (see resolveSites/resolveAllSites
// in sites_select.go); that is handled here as a single iteration with
// no site name, landing in the "all-sites" directory (see OutputPath).
func runExport(cfg *Config) error {
	logger := defaultLogger()

	// Resolved to an absolute path so every "Saved to" line and the
	// final output_dir log field are immediately usable (cd/open)
	// regardless of whether --output was relative or the default was
	// used from some other working directory. Falls back to the
	// as-given value on the rare os.Getwd failure rather than aborting
	// the run over a cosmetic concern.
	outputDir := cfg.OutputDir
	if abs, err := filepath.Abs(outputDir); err == nil {
		outputDir = abs
	}

	sites := cfg.Sites
	if len(sites) == 0 {
		sites = []string{""}
	}

	var succeeded, failed int
	var failures []string
	for _, site := range sites {
		log := logger.With("site", site)

		siteCfg := *cfg
		if site == "" {
			siteCfg.Sites = nil
		} else {
			siteCfg.Sites = []string{site}
		}

		producer := NewProducer(&siteCfg)
		ctx := sdk.NewContext(&sdk.ProducerConfig{
			SafeFailureMode: cfg.SafeFailureMode,
			Purpose:         cfg.Purpose,
		})
		ctx.Logger = log

		doc, err := producer.Collect(ctx)
		if err != nil {
			log.Error("collection failed (continuing with remaining sites)", "err", err)
			failures = append(failures, fmt.Sprintf("%s: %v", siteLabel(site), err))
			failed++
			continue
		}

		data, err := sdk.MarshalDocument(doc)
		if err != nil {
			log.Error("marshal failed (continuing with remaining sites)", "err", err)
			failures = append(failures, fmt.Sprintf("%s: %v", siteLabel(site), err))
			failed++
			continue
		}

		outPath := OutputPath(outputDir, filenameTimestamp(ctx.Clock()), site)
		if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
			log.Error("creating output directory failed (continuing with remaining sites)", "err", err, "path", filepath.Dir(outPath))
			failures = append(failures, fmt.Sprintf("%s: %v", siteLabel(site), err))
			failed++
			continue
		}
		if err := os.WriteFile(outPath, data, 0644); err != nil {
			log.Error("write failed (continuing with remaining sites)", "err", err, "path", outPath)
			failures = append(failures, fmt.Sprintf("%s: %v", siteLabel(site), err))
			failed++
			continue
		}

		fmt.Fprintf(os.Stderr, "Saved to %s\n", outPath)
		succeeded++
	}

	if succeeded == 0 {
		return fmt.Errorf("all %d site(s) failed: %s", failed, strings.Join(failures, "; "))
	}
	if failed > 0 {
		logger.Warn("export completed with failures", "succeeded", succeeded, "failed", failed)
	} else {
		logger.Info("export complete", "succeeded", succeeded, "output_dir", outputDir)
	}
	return nil
}

// siteLabel returns a human-readable label for a site name for use in
// log messages and aggregate error text; "" (the unfiltered-collection
// fallback, see runExport) reads as "all-sites" rather
// than an empty string.
func siteLabel(site string) string {
	if site == "" {
		return "all-sites"
	}
	return site
}

// attachRawBodies attaches each raw JSON body (raws, aligned with
// serials) to the matching resource in resources
// (matched serial -> idMap -> resource ID), under
// extensions[osiris.hpe.arubacentral].raw. Merges into any existing
// extension map (e.g. one EnrichConfigHealth already populated) instead
// of replacing it, so the two don't clobber each
// other regardless of call order.
func attachRawBodies(resources []sdk.Resource, idMap map[string]string, serials []string, raws []json.RawMessage) {
	if len(serials) != len(raws) {
		return
	}
	resIndex := make(map[string]int, len(resources))
	for i, r := range resources {
		resIndex[r.ID] = i
	}
	for i, serial := range serials {
		id, ok := idMap[serial]
		if !ok {
			continue
		}
		idx, ok := resIndex[id]
		if !ok {
			continue
		}
		setExtension(&resources[idx], "raw", raws[i])
	}
}

// setExtension merges a single key into a resource's
// extensions[osiris.hpe.arubacentral] map, preserving whatever else is
// already there (e.g. EnrichConfigHealth's payload) instead of
// replacing it, so multiple enrichment steps don't clobber each other
// regardless of call order.
func setExtension(r *sdk.Resource, key string, value any) {
	if r.Extensions == nil {
		r.Extensions = map[string]any{}
	}
	ext, _ := r.Extensions[extensionNamespace].(map[string]any)
	merged := make(map[string]any, len(ext)+1)
	maps.Copy(merged, ext)
	merged[key] = value
	r.Extensions[extensionNamespace] = merged
}

// unmanagedDeviceMAC derives a MAC address from an "Unmanaged"
// neighbor's serial, if it matches the "tpd_<12 hex chars>" pattern.
// Inferred, not documented: Aruba Central appears to synthesize
// this identifier for third-party/unmanaged devices as "tpd_" plus the
// device's own MAC with separators stripped confirmed only in that the
// 12 hex characters line up exactly with a bare MAC. Returns "" if the
// serial doesn't match or isn't a valid MAC once colons are inserted.
func unmanagedDeviceMAC(serial string) string {
	hex, ok := strings.CutPrefix(serial, "tpd_")
	if !ok || len(hex) != 12 {
		return ""
	}
	var b strings.Builder
	for i := 0; i < len(hex); i += 2 {
		if i > 0 {
			b.WriteByte(':')
		}
		b.WriteString(hex[i : i+2])
	}
	return sdk.NormalizeMAC(b.String())
}

// enrichUnmanagedNeighborStubs looks up extra detail
// (GetUnmanagedDevice) for each "Unmanaged" type neighbor and attaches
// it to the matching stub resource TransformNeighbors already created.
func enrichUnmanagedNeighborStubs(client *Client, neighbors []Neighbor, stubs []sdk.Resource, wantRawBody bool) {
	if !wantRawBody {
		return
	}
	stubIdx := make(map[string]int, len(stubs))
	for i, s := range stubs {
		stubIdx[s.ID] = i
	}
	for _, n := range neighbors {
		if n.Type != "Unmanaged" {
			continue
		}
		mac := unmanagedDeviceMAC(n.RemoteSerial)
		if mac == "" {
			continue
		}
		idx, ok := stubIdx[resourceID(n.RemoteSerial)]
		if !ok {
			continue
		}
		detail, err := client.GetUnmanagedDevice(mac, n.SiteID)
		if err != nil {
			continue
		}
		setExtension(&stubs[idx], "unmanaged_device_raw", detail)
	}
}

func switchSerials(switches []Switch) []string {
	serials := make([]string, len(switches))
	for i, sw := range switches {
		serials[i] = sw.SerialNumber
	}
	return serials
}

func apSerials(aps []AccessPoint) []string {
	serials := make([]string, len(aps))
	for i, ap := range aps {
		serials[i] = ap.SerialNumber
	}
	return serials
}

func gatewaySerials(gateways []Gateway) []string {
	serials := make([]string, len(gateways))
	for i, gw := range gateways {
		serials[i] = gw.SerialNumber
	}
	return serials
}

// filenameTimestamp returns a filesystem-safe UTC timestamp for output
// filenames. time.RFC3339 (used for document metadata) contains colons,
// which are illegal in Windows filenames, so those are
// replaced with dashes.
func filenameTimestamp(t time.Time) string {
	return strings.ReplaceAll(sdk.NormalizeRFC3339UTC(t), ":", "-")
}

// sanitizeFilenameSegment replaces characters that are unsafe in a
// filename (on any of Linux/macOS/Windows) with "-".
func sanitizeFilenameSegment(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|', ' ':
			return '-'
		}
		return r
	}, s)
}

func runTemplate(args []string) error {
	if len(args) == 0 || (args[0] != "--generate" && args[0] != "-g") {
		fmt.Println("Usage: osirisjson-producer arubacentral template --generate")
		return nil
	}
	filename := "arubacentral-token.json"
	if err := os.WriteFile(filename, []byte(TokenFileTemplate()), 0600); err != nil {
		return fmt.Errorf("failed to write template: %w", err)
	}
	fmt.Printf("Template saved to %s\n", filename)
	return nil
}

func defaultLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, nil))
}

func printHelp() {
	fmt.Print(`HPE Aruba Networking Central OSIRIS JSON producer

Usage:
  osirisjson-producer arubacentral [flags]

Authentication:
  Two credential types are supported:

  1. API Gateway application: client_id/client_secret plus an OAuth2
  	 token pair (access_token/refresh_token), created once via the
	 Central UI or the standard browser authorization-code flow.
  2. GreenLake Personal API client: a client_id/client_secret pair only,
     self-service generated (up to 7 per user) under
     https://common.cloud.hpe.com/manage-account/api when no
	 IT-provisioned API Gateway application is available.
	 No access/refresh token pair is needed up front: an access token is
	 minted automatically via grant_type=client_credentials against HPE
	 GreenLake's SSO endpoint (https://sso.common.cloud.hpe.com/as/token.oauth2)
	 and re-minted the same way on expiry/401, in place of a
	 refresh_token exchange.

  There are deliberately no --client-id/--client-secret/--access-token/
  --refresh-token flags: CLI flag values are visible to any local user
  and get written to shell history. Supply credentials via --token-file
  supplying only client_id/client_secret (no access_token, no refresh_token)
  is enough for a Personal API client - the access token is minted for you.

Flags:
  --cluster             Cluster short code (eu, eucentral2, eucentral3,
  						ukwest2, prod, central-prod2, uswest4, uswest5,
						us-east-1, starman, apac, apaceast, apacsouth,
						uaenorth1, china-prod, internal).
                        Optional: when omitted (and no --base-url), the
						cluster is auto-detected by probing every known
						cluster with the access token and using the one
						that accepts it.
  --base-url            Override the API Gateway base URL (takes
  						precedence over --cluster)
  --token-file          JSON file with {client_id, client_secret}
  --site                Comma-separated site name(s) to collect.
  						Optional: when omitted (and --all is not given),
						the account's sites are listed and you pick a
						subset interactively (e.g. "1", "1,3,5", "1-4",
						or "all"). Mutually exclusive with --all.
						Every run one site or many writes one document
						per site into --output (see below).
  --all                 Auto-discover and export every accessible site,
                        non-interactively skips the site picker prompt
                        entirely, ideal for cron/CI use.
  -o, --output          Output directory. Every run writes
                        <output-dir>/<site-name>/hpe-arubacentral-<timestamp>-<site-name>.json,
                        created if missing. Default: "osirisjson-hpe-arubacentral"
                        in the current directory. Re-running against the
						same directory (same working directory with no
						--output, or the same --output path) reuses it
						and adds each site's new file alongside prior
						ones instead of starting over.
  --purpose             documentation (default without purpose flag) or
  						audit. audit additionally includes client
						fingerprinting fields (host name, user name, OS,
						manufacturer, auth type) and, together with
                        --include-raw-body, the full raw response body.
  --safe-failure-mode   fail-closed (default), log-and-redact, or off
  --include-raw-body    Attach the full, unmodified API response body
  						for each switch/AP/gateway under
						extensions["osiris.hpe.arubacentral"].raw
						(requires --purpose audit; a lossless fallback
						for fields not yet modeled by this producer)

Other commands:
  template --generate	Write a --token-file skeleton to
  						arubacentral-token.json

Examples:
	osirisjson-producer arubacentral 
	osirisjson-producer arubacentral --purpose audit
	osirisjson-producer arubacentral --token-file ./arubacentral-token.json
	osirisjson-producer arubacentral --cluster eu --token-file ./arubacentral-token.json
	osirisjson-producer arubacentral --cluster prod --token-file ./arubacentral-token.json --site "MXP,Branch-1"
	osirisjson-producer arubacentral --cluster eu --token-file ./arubacentral-token.json --all -o ./output
	osirisjson-producer arubacentral template --generate
`)
}
