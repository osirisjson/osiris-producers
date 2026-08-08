// Package vmanage implements the Cisco Catalyst SD-WAN Manager
// (vManage) OSIRIS JSON Producer.
//
// A single vManage controller login fans out into many documents: one
// per WAN edge site, grouped by device site-id. It is
// dispatched directly by osiris/network/cisco/cisco.go's Run function.
//
// Multi-tenant (Provider) collection is out of scope for 0.1.0: the
// vManage OpenAPI spec has no documented tenant-switch/session-context
// endpoint, so GET /dataservice/tenant is used only for best-effort
// metadata.scope.accounts population, never to iterate or switch into a
// tenant's own device inventory.
//
// OSIRIS JSON Producer for Cisco SD-WAN Manager (vManage) introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification
package vmanage

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"go.osirisjson.org/producers/osiris/network/cisco/run"
	"go.osirisjson.org/producers/pkg/sdk"
)

const (
	generatorName    = "osirisjson-producer-cisco-vmanage"
	generatorVersion = "0.1.0"
	generatorURL     = "https://docs.osirisjson.org/osiris-producers/network/cisco"
)

// Run is the CLI entry point for the vManage producer. It receives the
// arguments after "vmanage" (e.g. ["-h", "acme.sdwan.cisco.com", "-u", "admin"]).
func Run(args []string) error {
	// Only the long form is checked here: -h is the documented short
	// flag for --host (see flags.go), so it must reach ParseFlags
	// instead of being intercepted as a help request. This mirrors
	// osiris/network/cisco/cisco.go's runSubProducer, which has the
	// same -h/--host vs. -h/help ambiguity for apic/nxos/iosxe.
	if len(args) > 0 {
		switch args[0] {
		case "--help":
			printHelp()
			return nil
		case "template":
			return runTemplate(args[1:])
		}
	}

	cfg, err := ParseFlags(args, promptVisible, run.PromptPassword)
	if err != nil {
		return err
	}

	return runExport(cfg)
}

// runTemplate writes a --token-file skeleton to
// cisco-vmanage-secrets.json.
func runTemplate(args []string) error {
	if len(args) == 0 || (args[0] != "--generate" && args[0] != "-g") {
		fmt.Println("Usage: osirisjson-producer cisco vmanage template --generate")
		return nil
	}
	filename := "cisco-vmanage-secrets.json"
	if err := os.WriteFile(filename, []byte(TokenFileTemplate()), 0600); err != nil {
		return fmt.Errorf("failed to write template: %w", err)
	}
	fmt.Printf("Template saved to %s\n", filename)
	return nil
}

// runExport authenticates once against the controller, fetches the
// full device inventory, partitions it by site-id, and writes one
// OSIRIS JSON document per site into cfg.OutputDir. Per-site write
// failures are logged and skipped; only a fully failed run
// (succeeded == 0) is fatal.
func runExport(cfg *Config) error {
	logger := defaultLogger()

	client := NewClient(cfg, logger)
	if err := client.Login(cfg.Username, cfg.Password); err != nil {
		return fmt.Errorf("vManage authentication failed: %w", err)
	}

	// wantRawBody gates attaching each collected endpoint's true,
	// unmodified API response body to the owning device resource's
	// extensions[osiris.cisco.vmanage] (see setExtension in
	// transform.go) a lossless fallback for fields not yet modeled by
	// this producer, available in audit purpose only (see
	// --include-raw-body).
	wantRawBody := cfg.Purpose == "audit" && cfg.IncludeRawBody

	devices, devicesRaw, err := client.GetDevicesWithRaw()
	if err != nil {
		return fmt.Errorf("fetching device inventory failed: %w", err)
	}
	var deviceRawByID map[string]json.RawMessage
	if wantRawBody {
		deviceRawByID = deviceRawIndex(devices, devicesRaw)
	}

	// Best-effort only: tenant labels feed metadata.scope.accounts when
	// available. A failure here does not abort the run since tenant
	// listing may require Provider-level privileges the credential
	// doesn't have.
	var accounts []string
	if tenants, err := client.ListTenants(); err != nil {
		logger.Warn("tenant listing failed (continuing without it)", "err", err)
	} else {
		for _, t := range tenants {
			if label := tenantLabel(t); label != "" {
				accounts = append(accounts, label)
			}
		}
	}

	// Best-effort, fetched once and reused for every site: GET
	// /dataservice/device/omp/links is not scoped to a single device,so
	// one call covers every site's OMP peering. TransformOMPLinks only
	// emits a connection when both ends resolve within the current
	// site's own systemIPToID index (OSIRIS-JSON section 2.2.3), so
	// passing the full unfiltered list into each site's transform below
	// is safe it naturally narrows to intra-site pairs.
	var ompLinks []OMPLink
	if links, err := client.GetOMPLinks(); err != nil {
		logger.Warn("OMP link listing failed (continuing without OMP peering connections)", "err", err)
	} else {
		ompLinks = links
	}

	outputDir := cfg.OutputDir
	if abs, err := filepath.Abs(outputDir); err == nil {
		outputDir = abs
	}

	groups := GroupDevicesBySiteID(devices)

	// siteNames covers whatever resolveSiteSelection already had to
	// resolve (--all and the interactive picker both need every
	// discovered site's name; --site's short explicit list does not,
	// so siteNames is nil there and the per-site fallback below
	// resolves just the few selected sites individually).
	selectedSiteIDs, siteNames, err := resolveSiteSelection(cfg, groups, client, logger)
	if err != nil {
		return fmt.Errorf("resolving site selection: %w", err)
	}
	logger.Info("site selection resolved", "sites", len(selectedSiteIDs))

	var succeeded, failed int
	var failures []string
	for _, siteID := range selectedSiteIDs {
		siteDevices := groups[siteID]

		// siteName drives metadata.scope.sites, OutputPath's
		// directory/file segment, and (below) every console log line
		// for this site resolved once, up front, rather than
		// separately per use. Resolved against a site_id-only logger
		// since siteName isn't known yet. Not set on individual
		// resources (provider.site) see TransformDevices comment.
		var siteName string
		if siteID != "" {
			// Reuse the name resolved during selection when available
			// (--all / interactive picker) instead of calling
			// GetSiteName every time for the same site-id; only the
			// --site path (siteNames == nil) resolve individually here.
			// Either way this is best-effort: falls back to the numeric
			// site-id itself if the name is unavailable, so it's never
			// left empty for a real site.
			name, ok := siteNames[siteID]
			if !ok {
				name = siteDisplayName(client, siteID, logger.With("site_id", siteID))
			}
			siteName = name
		}

		// Every log line for this site carries both site_id (stable,
		// unambiguous correlation key) and site (the resolved display
		// name).
		log := logger.With("site_id", siteID, "site", siteLabel(siteID, siteName))

		ctx := sdk.NewContext(&sdk.ProducerConfig{
			SafeFailureMode: cfg.SafeFailureMode,
			Purpose:         cfg.Purpose,
		})
		ctx.Logger = log

		resources, systemIPToID := TransformDevices(siteDevices, cfg.Purpose)
		var connections []sdk.Connection

		// Captured before interfaces are appended to resources below,
		// so this holds exactly the site's device (router/controller)
		// resource IDs the physical.room group's membership
		// OSIRIS JSON section 7.6.5.
		deviceResourceIDs := make([]string, 0, len(resources))
		for _, r := range resources {
			deviceResourceIDs = append(deviceResourceIDs, r.ID)
		}

		// deviceResIdx maps each device resource's ID to its position
		// in resources stable across the later interface appends below
		// (positions are only ever appended past, never reordered), so
		// it is safe to compute once here and reuse for every raw-body
		// attachment (device object now, per-device interfaces/WAN/OMP
		// peers further down the per-device loop).
		deviceResIdx := make(map[string]int, len(resources))
		for i, r := range resources {
			deviceResIdx[r.ID] = i
		}

		if wantRawBody {
			for _, d := range siteDevices {
				resType, ok := personalityToType[d.Personality]
				if !ok {
					continue // TransformDevices skipped this device too.
				}
				raw, ok := deviceRawByID[d.DeviceID]
				if !ok {
					continue
				}
				if idx, ok := deviceResIdx[resourceID(deviceNativeKey(d, resType))]; ok {
					setExtension(&resources[idx], "raw", raw)
				}
			}
		}

		// Per-device interfaces (contains connections to their owning
		// router/controller) and OMP peers. Sequential per device: a
		// site's own edge count is small, unlike the multi-hundred-site
		// count defaultSiteNameRateLimit was built for (see config.go)
		// no bounded-concurrency worker pool needed here. Any single
		// device/endpoint failure is logged and skipped, never aborts
		// the site's document.
		//
		// Every stage below logs an Info line on success and a Warn
		// line on failure, so a healthy run still shows visible
		// progress between "site selection resolved" and "Saved to ...".
		deviceCount := 0
		for _, d := range siteDevices {
			if d.SystemIP != "" {
				deviceCount++
			}
		}
		if deviceCount > 0 {
			log.Info("collecting device interfaces and connections", "devices", deviceCount)
		}

		ifaceIndex := make(map[string]string) // "{system-ip}:{color}" -> interface resource ID, merged across the site's devices.
		var ompPeerConns []sdk.Connection
		for _, d := range siteDevices {
			if d.SystemIP == "" {
				continue
			}
			deviceResourceID, ok := systemIPToID[d.SystemIP]
			if !ok {
				// Personality not in personalityToType
				// TransformDevices already skipped this device.
				continue
			}
			deviceResourceType := personalityToType[d.Personality]
			deviceKey := deviceNativeKey(d, deviceResourceType)
			dlog := log.With("device", d.HostName)

			ifaces, ifacesRaw, err := client.GetDeviceInterfacesWithRaw(d.SystemIP)
			if err != nil {
				dlog.Warn("interface listing failed (continuing without this device's interfaces)", "err", err)
				continue
			}
			wanIfaces, wanIfacesRaw, err := client.GetWANInterfacesWithRaw(d.SystemIP)
			if err != nil {
				dlog.Warn("WAN interface listing failed (continuing with base interface data only)", "err", err)
			}

			ifaceResources, containsConns, deviceTunnelIndex := TransformInterfaces(deviceResourceID, deviceResourceType, deviceKey, d.SystemIP, ifaces, wanIfaces)
			resources = append(resources, ifaceResources...)
			connections = append(connections, containsConns...)
			for k, v := range deviceTunnelIndex {
				ifaceIndex[k] = v
			}
			dlog.Info("device interfaces collected", "interfaces", len(ifaceResources))

			// Raw endpoinmt bodies used for development attach to the
			// owning device resource, not a separate per-interface
			// resource.
			if wantRawBody {
				if idx, ok := deviceResIdx[deviceResourceID]; ok {
					if len(ifacesRaw) > 0 {
						setExtension(&resources[idx], "interfaces_raw", ifacesRaw)
					}
					if len(wanIfacesRaw) > 0 {
						setExtension(&resources[idx], "wan_interfaces_raw", wanIfacesRaw)
					}
				}
			}

			peers, peersRaw, err := client.GetOMPPeersWithRaw(d.SystemIP)
			if err != nil {
				dlog.Warn("OMP peer listing failed (continuing without this device's OMP peering)", "err", err)
				continue
			}
			ompPeerConns = append(ompPeerConns, TransformOMPPeers(deviceResourceID, peers, systemIPToID)...)
			if wantRawBody && len(peersRaw) > 0 {
				if idx, ok := deviceResIdx[deviceResourceID]; ok {
					setExtension(&resources[idx], "omp_peers_raw", peersRaw)
				}
			}
		}

		ompConns := dedupeConnections(append(TransformOMPLinks(ompLinks, systemIPToID), ompPeerConns...))
		if len(ompConns) > 0 {
			log.Info("OMP peering connections resolved", "count", len(ompConns))
		}
		connections = append(connections, ompConns...)

		// GET /dataservice/topology/monitor/site/{siteId} has no siteId
		// to call with for the unclaimed fallback group.
		if siteID != "" {
			if siteTopology, siteTopologyRaw, err := client.GetSiteTopologyMonitorWithRaw(siteID); err != nil {
				log.Warn("site topology monitor failed (continuing without SD-WAN tunnel connections)", "err", err)
			} else {
				tunnelConns := TransformTunnels(siteTopology, ifaceIndex)
				if len(tunnelConns) > 0 {
					log.Info("SD-WAN tunnel connections resolved", "count", len(tunnelConns))
				}
				connections = append(connections, tunnelConns...)

				// Each entry is matched back to its owning device
				// resource by vManage's own device-id, shared between
				// this endpoint's "device-id" field and GET
				// /dataservice/device's "deviceId"
				// field (see Device.DeviceID).
				if wantRawBody && len(siteTopology) == len(siteTopologyRaw) {
					deviceIDToResID := make(map[string]string, len(siteDevices))
					for _, d := range siteDevices {
						resType, ok := personalityToType[d.Personality]
						if !ok || d.DeviceID == "" {
							continue
						}
						deviceIDToResID[d.DeviceID] = resourceID(deviceNativeKey(d, resType))
					}
					for i, st := range siteTopology {
						resID, ok := deviceIDToResID[st.DeviceID]
						if !ok {
							continue
						}
						if idx, ok := deviceResIdx[resID]; ok {
							setExtension(&resources[idx], "site_topology_raw", siteTopologyRaw[i])
						}
					}
				}
			}
		}

		scope := sdk.Scope{
			Providers: []string{providerName},
			Purpose:   cfg.Purpose,
			Accounts:  accounts,
			Clusters:  []string{cfg.Host},
		}
		if siteName != "" {
			scope.Sites = []string{siteName}
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
		if group, ok := TransformSiteGroup(siteID, siteName, siteDevices, deviceResourceIDs); ok {
			builder.AddGroup(group)
		}

		doc, err := builder.Build()
		if err != nil {
			log.Error("document build failed (continuing with remaining sites)", "err", err)
			failures = append(failures, fmt.Sprintf("%s: %v", siteLabel(siteID, siteName), err))
			failed++
			continue
		}

		data, err := sdk.MarshalDocument(doc)
		if err != nil {
			log.Error("marshal failed (continuing with remaining sites)", "err", err)
			failures = append(failures, fmt.Sprintf("%s: %v", siteLabel(siteID, siteName), err))
			failed++
			continue
		}

		// outPath is keyed by site name, not site-id (see OutputPath's
		// comment): a directory that already exists under this name
		// from a previous vmanage run, or another producer entirely
		// writing to the same --output is reused as-is; MkdirAll below
		// is a no-op against it and the new timestamped file is simply
		// added alongside whatever is already there.
		outPath := OutputPath(outputDir, run.FormatTimestamp(ctx.Clock()), siteName)
		if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
			log.Error("creating output directory failed (continuing with remaining sites)", "err", err, "path", filepath.Dir(outPath))
			failures = append(failures, fmt.Sprintf("%s: %v", siteLabel(siteID, siteName), err))
			failed++
			continue
		}
		if err := os.WriteFile(outPath, data, 0644); err != nil {
			log.Error("write failed (continuing with remaining sites)", "err", err, "path", outPath)
			failures = append(failures, fmt.Sprintf("%s: %v", siteLabel(siteID, siteName), err))
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

// deviceRawIndex builds a Device.DeviceID -> raw JSON object lookup
// from GetDevicesWithRaw's parallel devices/raws slices, so each
// site's per-device raw body (see wantRawBody) can still be found
// after GroupDevicesBySiteID has split the flat device list into
// per-site slices.
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

// siteLabel returns a human-readable label for a site, matching the
// resolved name OutputPath used for this site's directory segment
// (siteName) when available, falling back to the raw site-id and then
// to the unclaimed segment name.
func siteLabel(siteID, siteName string) string {
	if siteName != "" {
		return siteName
	}
	if siteID == "" {
		return unsitedSegment
	}
	return siteID
}

func defaultLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, nil))
}

func printHelp() {
	fmt.Print(`osirisjson-producer cisco vmanage - Cisco Catalyst SD-WAN Manager (vManage) OSIRIS JSON producer

Usage:
  osirisjson-producer cisco vmanage [flags]

Site selection: when neither --site nor --all is given, the sites
discovered on the controller are listed interactively (pick by number,
e.g. "1,3,5" or "1-4" or "all"). Use --all (or --site all, an alias)
for non-interactive runs (cron/CI). Every site's document starts being
written immediately, without waiting on any site-name resolution.
--site also accepts either the raw numeric site-id (matches instantly
without waiting for name resolution) or the resolved site name
(e.g. --site MXP); a name that doesn't match any raw site-id
triggers a full site-name resolution pass to find it, since vManage
up to 20.18 still has no endpoint to look up a site-id by name directly.

Bulk site-name resolution (only triggered by the interactive picker or
a --site value that falls back to name matching) is capped at
--site-name-rate (default 10/s) since vManage's own rate limit is
typically shared across many tools frequently polling same controller
(monitoring systems, other automation, not just this producer). Raise
or lower it based on how much headroom your environment actually has,
from 1 (very conservative) up to your controller's full observed limit.

Authentication: there is deliberately no -p/--password flag, CLI flag
values are visible to any local user (e.g. via ps) and get written to
shell history. host/username/password are each resolved in this order:
their own -h/-u flag, then --token-file, then (for whichever is still
missing) an interactive prompt on the controlling terminal so a bare
"osirisjson-producer cisco vmanage" with no flags at all asks for
host, username and password one at a time, and nothing entered there
is written to disk unless --token-file was also given.

Flags:
`)
	fmt.Print(FlagsUsage())
	fmt.Print(`
Other commands:
  template --generate	Write a --token-file skeleton to cisco-vmanage-secrets.json

Examples:
	osirisjson-producer cisco vmanage
	osirisjson-producer cisco vmanage -h acme.sdwan.cisco.com -u admin
	osirisjson-producer cisco vmanage --token-file ./cisco-vmanage-secrets.json
	osirisjson-producer cisco vmanage --token-file ./cisco-vmanage-secrets.json --purpose audit --site "MXP,Branch-1"
	osirisjson-producer cisco vmanage --token-file ./cisco-vmanage-secrets.json --all -o ./output
	osirisjson-producer cisco vmanage template --generate
`)
}
