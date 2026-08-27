// Package nxos implements the Cisco NX-OS producer for OSIRIS JSON.
// Queries the NX-API CLI interface to discover device topology and
// generates an OSIRIS JSON document with resources, groups, connections.
//
// This file holds the whole CLI surface (Run, the single/batch runners,
// --help, template generation) alongside the Producer type and its
// Collect the same one-file shape osiris/network/cisco/vmanage uses
// instead of a separate dispatch.go. The per-domain NX-API -> OSIRIS
// mapping lives in the transform_<area>.go files; wire handling and the
// typed response DTOs are in client.go and dto.go.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification
package nxos

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"go.osirisjson.org/producers/osiris/network/cisco/run"
	"go.osirisjson.org/producers/pkg/osirismeta"
	"go.osirisjson.org/producers/pkg/sdk"
)

const (
	generatorName    = "osirisjson-producer-cisco-nxos"
	generatorVersion = "0.2.0"
	generatorURL     = "https://docs.osirisjson.org/osiris-producers/network/cisco"
)

// templateExampleHost is the documentation-only address shown in the
// generated --secrets-file/CSV template skeletons (RFC 5737, never a
// real device address).
const templateExampleHost = "192.0.2.1"

// Run is the CLI entry point for the NX-OS producer. It receives the
// arguments after "nxos".
func Run(args []string) error {
	// Only the long form is checked here: -h is the documented short
	// flag for --host (see flags.go), so it must reach ParseFlags
	// instead of being intercepted as a help request the same
	// -h/--host vs. -h/help ambiguity osiris/network/cisco/cisco.go's
	// runSubProducer already handles for apic/iosxe, and vmanage.go's
	// own Run handles for vmanage.
	if len(args) > 0 {
		switch args[0] {
		case "--help":
			printHelp()
			return nil
		case "template":
			return runTemplate(args[1:])
		}
	}

	cfg, err := ParseFlags(args, run.PromptVisible, run.PromptPassword)
	if err != nil {
		return err
	}
	cfg.Timestamp = run.FormatTimestamp(time.Now())

	if cfg.Mode == run.ModeBatch {
		return runBatch(cfg)
	}
	return runSingle(cfg)
}

// runTemplate writes this producer's CSV batch template
// (cisco-nxos-template.csv) and both --secrets-file shapes
// (cisco-nxos-secrets.json, cisco-nxos-secrets-multihost.json) via the
// shared run.GenerateTemplates CSV batch mode is still this
// producer's own feature (unlike vmanage), so unlike vmanage's
// runTemplate this does call GenerateTemplates, not just the two
// --secrets-file variants.
func runTemplate(args []string) error {
	if len(args) == 0 || (args[0] != "--generate" && args[0] != "-g") {
		fmt.Println("Usage: osirisjson-producer cisco nxos template --generate")
		return nil
	}
	return run.GenerateTemplates("nxos", templateExampleHost)
}

// runSingle executes a single-target collection and writes it to a
// local file: cisco-nxos-<timestamp>-<hostname>.json.
func runSingle(cfg *Config) error {
	target := cfg.Targets[0]
	logger := defaultLogger()

	producer := &Producer{target: target, cfg: cfg}
	ctx := newSDKContext(cfg)
	ctx.Logger = logger

	doc, err := producer.Collect(ctx)
	if err != nil {
		return fmt.Errorf("collection failed for %s: %w", target.Host, err)
	}

	data, err := sdk.MarshalDocument(doc)
	if err != nil {
		return fmt.Errorf("marshal failed: %w", err)
	}

	name := target.Hostname
	if name == "" {
		name = target.Host
	}
	name, err = run.SanitizePathSegment(name)
	if err != nil {
		return fmt.Errorf("invalid output filename: %w", err)
	}
	filename := fmt.Sprintf("cisco-nxos-%s-%s.json", cfg.Timestamp, name)

	// 0600: emitted documents are infrastructure inventory snapshots
	// (hostnames, serials, topology) and should not be world/group
	// readable by default, only the invoking user.
	if err := os.WriteFile(filename, data, 0600); err != nil {
		return fmt.Errorf("failed to write %s: %w", filename, err)
	}
	fmt.Fprintf(os.Stderr, "Saved to %s\n", filename)
	return nil
}

// runBatch executes the batch: one document per target, written to the
// hierarchical output path (see run.OutputPath).
// A single target's failure is logged and skipped; only a fully failed
// run (succeeded == 0) is fatal the same contract run.RunBatch
// provided when this producer went through it.
func runBatch(cfg *Config) error {
	logger := defaultLogger()

	if err := os.MkdirAll(cfg.OutputDir, 0755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	var succeeded, failed int

	for _, target := range cfg.Targets {
		log := logger.With("target", target.Host, "hostname", target.Hostname)
		log.Info("collecting")

		producer := &Producer{target: target, cfg: cfg}
		ctx := newSDKContext(cfg)
		ctx.Logger = log

		doc, err := producer.Collect(ctx)
		if err != nil {
			log.Error("collection failed", "error", err)
			failed++
			continue
		}

		data, err := sdk.MarshalDocument(doc)
		if err != nil {
			log.Error("marshal failed", "error", err)
			failed++
			continue
		}

		outPath, err := run.OutputPath(cfg.OutputDir, cfg.Timestamp, target)
		if err != nil {
			log.Error("invalid output path", "error", err)
			failed++
			continue
		}
		if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
			log.Error("creating output path", "error", err, "path", outPath)
			failed++
			continue
		}
		if err := os.WriteFile(outPath, data, 0600); err != nil {
			log.Error("write failed", "error", err, "path", outPath)
			failed++
			continue
		}

		log.Info("written", "path", outPath)
		succeeded++
	}

	if succeeded == 0 {
		return fmt.Errorf("all %d targets failed", failed)
	}
	if failed > 0 {
		logger.Warn("batch completed with failures", "succeeded", succeeded, "failed", failed)
	} else {
		logger.Info("batch completed", "succeeded", succeeded)
	}
	return nil
}

func defaultLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, nil))
}

// newSDKContext maps this producer's own Config onto the generic
// sdk.ProducerConfig every sdk.Producer.Collect call expects.
func newSDKContext(cfg *Config) *sdk.Context {
	return sdk.NewContext(&sdk.ProducerConfig{
		Purpose:         cfg.Purpose,
		IncludeRawBody:  cfg.IncludeRawBody,
		SafeFailureMode: cfg.SafeFailureMode,
	})
}

// Producer implements the NX-OS producer. It owns its own CLI entry
// point (Run, above) instead of going through
// osiris/network/cisco/run.ProducerFactory/RunBatch see flags.go's
// doc comment for why.
type Producer struct {
	target run.TargetConfig
	cfg    *Config
	client *Client // injectable for testing
}

// Collect queries the NX-OS device and builds an OSIRIS document.
func (p *Producer) Collect(ctx *sdk.Context) (*sdk.Document, error) {
	client := p.client
	if client == nil {
		client = NewClient(p.target, p.cfg.InsecureTLS, ctx.Logger)
		if err := client.Login(p.target.Username, p.target.Password); err != nil {
			return nil, fmt.Errorf("NX-OS authentication failed: %w", err)
		}
	}

	hostname := p.target.Hostname
	if hostname == "" {
		hostname = p.target.Host
	}

	ctx.Logger.Info("collecting NX-OS device data", "host", p.target.Host)

	// coverage records attempted/succeeded/failed/unavailable per
	// issued command across every batch this run makes, surfaced on the
	// device resource's extensions below a malformed or dropped command
	// is visible in the emitted document itself,
	// not only in stderr logs.
	var coverage []map[string]any

	// versionData: Login already fetched and decoded "show version"
	// once to validate credentials (see Client.Login), so we reuse it
	// instead of querying it again here. VersionData is nil only when a
	// pre-authenticated Client was injected directly (bypassing Login
	// entirely this package's own tests do this), in which case batch 1
	// below fetches it, still exactly once for the run.
	versionData := client.VersionData()

	// Batch 1: core device data. ShowMulti's returned error is only a
	// transport-level failure (device unreachable, malformed envelope)
	// an individual command being rejected by the CLI becomes that
	// command's own ShowResult.Err instead (see decodeBody), so one
	// command failing here never erases the others' data.
	batch1Commands := []string{
		"show inventory",
		"show module",
		"show interface brief",
		"show vlan brief",
		"show vrf all detail",
		"show vrf interface",
	}
	offset := 0
	if versionData == nil {
		batch1Commands = append([]string{"show version"}, batch1Commands...)
		offset = 1
	}
	batch1, err := client.ShowMulti(batch1Commands)
	if err != nil {
		return nil, fmt.Errorf("NX-OS batch 1 query failed: %w", err)
	}
	recordCoverage(&coverage, batch1Commands, batch1)

	if offset == 1 {
		vd := decodeBody[versionResponse](batch1, 0, ctx.Logger)
		versionData = &vd
	}
	inventoryData := decodeBody[inventoryResponse](batch1, offset+0, ctx.Logger)
	moduleData := decodeBody[moduleResponse](batch1, offset+1, ctx.Logger)
	ifBriefData := decodeBody[interfaceBriefResponse](batch1, offset+2, ctx.Logger)
	vlanBriefData := decodeBody[vlanBriefResponse](batch1, offset+3, ctx.Logger)
	vrfDetailData := decodeBody[vrfDetailResponse](batch1, offset+4, ctx.Logger)
	vrfInterfaceData := decodeBody[vrfInterfaceResponse](batch1, offset+5, ctx.Logger)

	// audit gates every command/resource this producer considers
	// audit-tier per OSIRIS JSON spec chapter 13.1.3: BGP/OSPF neighbor
	// sessions, interface counters, environment, and AAA/RADIUS/TACACS+
	// posture (see batch 3 below). documentation purpose (the default)
	// collects only base topology/inventory.
	audit := ctx.Config != nil && ctx.Config.Purpose == "audit"

	// Batch 2: optional features LLDP/CDP, vPC (brief + keepalive),
	// port-channel, interface IP, switchport detail, plus OSPF/BGP
	// neighbor sessions when auditing. These may not be enabled/
	// configured on all devices; each command's own success or failure
	// is independent of its siblings LLDP being disabled, for example,
	// must not also discard vPC or port-channel data that was fetched
	// successfully in the same call.
	batch2Commands := []string{
		"show lldp neighbors detail",
		"show vpc brief",
		"show port-channel summary",
		"show cdp neighbors detail",
		"show vpc peer-keepalive",
		"show ip interface brief vrf all",
		"show interface switchport",
		"show interface transceiver",
	}
	if audit {
		batch2Commands = append(batch2Commands, "show ip ospf neighbor vrf all", "show bgp all summary")
	}
	batch2, err := client.ShowMulti(batch2Commands)
	if err != nil {
		ctx.Logger.Warn("NX-OS batch 2 transport failure (device unreachable mid-run?)", "err", err)
		batch2 = nil
	}
	recordCoverage(&coverage, batch2Commands, batch2)

	lldpData := decodeBody[lldpNeighborsResponse](batch2, 0, ctx.Logger)
	vpcBriefData := decodeBody[vpcBriefResponse](batch2, 1, ctx.Logger)
	portChannelData := decodeBody[portChannelSummaryResponse](batch2, 2, ctx.Logger)
	cdpData := decodeBody[cdpNeighborsResponse](batch2, 3, ctx.Logger)
	vpcKeepaliveData := decodeBody[vpcPeerKeepaliveResponse](batch2, 4, ctx.Logger)
	ipBriefData := decodeBody[ipInterfaceBriefResponse](batch2, 5, ctx.Logger)
	switchportData := decodeBody[switchportResponse](batch2, 6, ctx.Logger)
	transceiverData := decodeBody[transceiverResponse](batch2, 7, ctx.Logger)
	var ospfData ospfNeighborsResponse
	var bgpData bgpSummaryResponse
	if audit {
		ospfData = decodeBody[ospfNeighborsResponse](batch2, 8, ctx.Logger)
		bgpData = decodeBody[bgpSummaryResponse](batch2, 9, ctx.Logger)
	}

	// Transform device.
	deviceResource, deviceID := TransformDevice(p.target.Host, hostname, *versionData)
	deviceKey := deviceNativeKey(p.target.Host, *versionData)

	// Add inventory and module status to device extension.
	inventory := TransformInventory(inventoryData, audit)
	if len(inventory) > 0 {
		ensureCiscoExtension(&deviceResource.Extensions)
		deviceResource.Extensions[extensionNamespace].(map[string]any)["inventory"] = inventory
	}
	modules := TransformModules(moduleData)
	if len(modules) > 0 {
		ensureCiscoExtension(&deviceResource.Extensions)
		deviceResource.Extensions[extensionNamespace].(map[string]any)["modules"] = modules
	}

	// Transform interfaces.
	ifResources, ifNameToID := TransformInterfaces(deviceKey, ifBriefData)

	// port_count (network.switch "Common properties", OSIRIS-JSON-v1.0
	// 7.5.2) can only be known once interfaces are collected, unlike
	// the rest of TransformDevice's properties, which come straight
	// from "show version".
	portCount := 0
	for _, r := range ifResources {
		if r.Type == "network.switch.port" {
			portCount++
		}
	}
	if portCount > 0 {
		if deviceResource.Properties == nil {
			deviceResource.Properties = make(map[string]any)
		}
		deviceResource.Properties["port_count"] = portCount
	}

	// Switch "contains" its own interfaces (physical ports and logical
	// constructs alike) never the LLDP/CDP stub resources built below,
	// which represent a remote neighbor's port, not this switch's own.
	containment := TransformDeviceContainment(deviceID, deviceResource.Name, ifResources)

	// Transform merged LLDP+CDP neighbors -> connections + stubs,
	// attaching each connection's local-side transceiver when present.
	transceivers := TransformTransceivers(transceiverData, audit)
	connections, stubs := TransformNeighbors(deviceID, hostname, lldpData, cdpData, ifNameToID, transceivers)
	connections = append(connections, containment...)

	// Enrich interfaces with IP address (bare, no prefix see
	// EnrichInterfaceIPs) and native VLAN (trunk mode).
	EnrichInterfaceIPs(ipBriefData, ifResources, ifNameToID)
	EnrichSwitchportDetails(switchportData, ifResources, ifNameToID)

	// vPC keepalive: a distinct control-plane relationship from the
	// peer-link/vPC-member wiring below see TransformVPCKeepalive.
	if keepaliveConn, keepaliveStub := TransformVPCKeepalive(deviceID, deviceResource.Name, vpcKeepaliveData); keepaliveConn != nil {
		connections = append(connections, *keepaliveConn)
		stubs = append(stubs, *keepaliveStub)
	}

	// OSPF/BGP neighbor sessions -> connections + stubs.
	ospfConnections, ospfStubs := TransformOSPFNeighbors(deviceID, ospfData, ifNameToID)
	connections = append(connections, ospfConnections...)
	stubs = append(stubs, ospfStubs...)
	bgpConnections, bgpStubs := TransformBGPNeighbors(deviceID, bgpData)
	connections = append(connections, bgpConnections...)
	stubs = append(stubs, bgpStubs...)

	// VLANs are network.vlan resources (OSIRIS-JSON-v1.0 7.5.1); port
	// membership is a set of network.l2 connections, not group
	// membership. VRFs and the vPC domain remain groups.
	vlanResources, vlanIDToResID := TransformVLANs(deviceKey, vlanBriefData)
	vrfGroups, vrfNameToGroupID := TransformVRFs(hostname, vrfDetailData)
	vpcGroup, _ := TransformVPC(hostname, vpcBriefData)

	// Wire relationships. seenVLANConn deduplicates a port reported as
	// a VLAN member by both "show vlan brief" and
	// "show interface switchport".
	seenVLANConn := make(map[string]bool)
	vlanConns := WireInterfacesToVLANs(vlanBriefData, ifBriefData, ifNameToID, vlanIDToResID, seenVLANConn)
	vlanConns = append(vlanConns, WireTrunkPortsToVLANs(switchportData, ifNameToID, vlanIDToResID, seenVLANConn)...)
	connections = append(connections, vlanConns...)
	vrfMembers := WireInterfacesToVRFs(vrfDetailData, vrfInterfaceData, ifNameToID, vrfGroups, vrfNameToGroupID)
	ctx.Logger.Debug("relationship wiring complete", "vlan_l2_connections", len(vlanConns), "vrf_members", vrfMembers)
	if vpcGroup != nil {
		WirePortChannelsToVPC(vpcBriefData, ifNameToID, vpcGroup)
	}

	// Wire port-channel bundle membership: "contains" connections from
	// each LAG resource (already created above by TransformInterfaces)
	// to its physical member interfaces, plus a member_count property
	// on the LAG resource itself.
	pcConnections := TransformPortChannels(portChannelData, ifResources, ifNameToID)
	connections = append(connections, pcConnections...)

	// extraResources: whole-resource additions beyond the device and
	// its own interfaces/stubs (currently just the AAA posture resource
	// below, detailed-mode only).
	var extraResources []sdk.Resource

	// Audit purpose: interface counters, environment, AAA/RADIUS/TACACS+
	// posture. Each command is independent: "show environment" failing
	// on a platform that doesn't support it, for example, must not also
	// discard interface counters fetched successfully in the same call.
	//
	// "show system resources" is deliberately not collected: its only
	// fields (cpu_idle, load_avg, memory_used/free) are real-time
	// telemetry excluded by OSIRIS-JSON-v1.0 13.1.3 even at audit
	// purpose, and total RAM is already covered by memory_mb from
	// "show version".
	if audit {
		batch3Commands := []string{
			"show interface",
			"show environment",
			"show aaa accounting",
			"show aaa authentication",
			"show aaa groups",
			"show radius-server",
			"show tacacs-server",
		}
		batch3, err := client.ShowMulti(batch3Commands)
		if err != nil {
			ctx.Logger.Warn("NX-OS detailed batch transport failure", "err", err)
			batch3 = nil
		}
		recordCoverage(&coverage, batch3Commands, batch3)

		ifDetailData := decodeBody[interfaceDetailResponse](batch3, 0, ctx.Logger)
		envData := decodeBody[environmentResponse](batch3, 1, ctx.Logger)
		aaaAccountingData := decodeBody[aaaAccountingResponse](batch3, 2, ctx.Logger)
		aaaAuthenticationData := decodeBody[aaaAuthenticationResponse](batch3, 3, ctx.Logger)
		aaaGroupsData := decodeBody[aaaGroupsResponse](batch3, 4, ctx.Logger)
		radiusData := decodeBody[radiusServerResponse](batch3, 5, ctx.Logger)
		tacacsData := decodeBody[tacacsServerResponse](batch3, 6, ctx.Logger)

		// Enrich interfaces with detailed counters.
		EnrichInterfaceDetails(hostname, ifDetailData, ifResources, ifNameToID)

		// Add environment to device extension.
		envExt := TransformEnvironment(envData)
		if len(envExt) > 0 {
			ensureCiscoExtension(&deviceResource.Extensions)
			cisco := deviceResource.Extensions[extensionNamespace].(map[string]any)
			for k, v := range envExt {
				cisco[k] = v
			}
		}

		// AAA/RADIUS/TACACS posture as its own osiris.cisco.aaa
		// resource, contained by the switch see TransformAAA's doc
		// comment for why this is a resource rather than a device
		// extension key.
		if aaaResource, ok := TransformAAA(deviceKey, aaaAccountingData, aaaAuthenticationData, aaaGroupsData, radiusData, tacacsData); ok {
			extraResources = append(extraResources, aaaResource)
			connections = append(connections, TransformAAAContainment(deviceID, deviceResource.Name, aaaResource))
		}

		// --include-raw-body: attach every command's own unmodified
		// NX-API response body, a lossless fallback for fields this
		// producer doesn't model yet. Only takes effect at audit
		// purpose (this whole block is already audit-gated); ignored
		// otherwise, matching sdk.ProducerConfig.IncludeRawBody's own
		// documented contract.
		if ctx.Config != nil && ctx.Config.IncludeRawBody {
			raw := rawCommandBodies(batch1, batch2, batch3)
			if len(raw) > 0 {
				ensureCiscoExtension(&deviceResource.Extensions)
				deviceResource.Extensions[extensionNamespace].(map[string]any)["raw_commands"] = raw
			}
		}
	}

	// Surface per-command coverage on the device resource, regardless
	// of detail level a malformed or unavailable command must be
	// visible in the document itself, not only inferable from stderr.
	if len(coverage) > 0 {
		ensureCiscoExtension(&deviceResource.Extensions)
		deviceResource.Extensions[extensionNamespace].(map[string]any)["coverage"] = coverage
	}

	// Assemble the document. scope.name is the switch's own reported
	// hostname (one OSIRIS document per device); scope.purpose records
	// the --purpose level this run collected at, so a consumer holding
	// only the document can tell a documentation snapshot from an audit
	// one.
	scopeName := deviceResource.Name
	if scopeName == "" {
		scopeName = hostname
	}
	scopePurpose := ""
	if ctx.Config != nil {
		scopePurpose = ctx.Config.Purpose
	}
	builder := sdk.NewDocumentBuilder(ctx).
		WithGenerator(generatorName, generatorVersion, generatorURL).
		WithScope(sdk.Scope{
			Name:      scopeName,
			Purpose:   scopePurpose,
			Providers: []string{providerName},
		})

	// Add device resource.
	builder.AddResource(deviceResource)

	// Add interface resources.
	for _, r := range ifResources {
		builder.AddResource(r)
	}

	// Add VLAN resources (network.vlan, OSIRIS-JSON-v1.0 7.5.1).
	for _, r := range vlanResources {
		builder.AddResource(r)
	}

	// Add whole-resource additions beyond the device/interfaces (AAA
	// posture, detailed mode only).
	for _, r := range extraResources {
		builder.AddResource(r)
	}

	// Add LLDP stub resources.
	for _, r := range stubs {
		builder.AddResource(r)
	}

	// Add connections.
	for _, c := range connections {
		builder.AddConnection(c)
	}

	// Add groups (VRF, and the vPC domain when configured).
	for _, g := range vrfGroups {
		builder.AddGroup(g)
	}
	if vpcGroup != nil {
		builder.AddGroup(*vpcGroup)
	}

	doc, err := builder.Build()
	if err != nil {
		return nil, fmt.Errorf("document build failed: %w", err)
	}

	ctx.Logger.Info("NX-OS collection complete",
		"resources", len(doc.Topology.Resources),
		"connections", len(doc.Topology.Connections),
		"groups", len(doc.Topology.Groups),
	)

	return doc, nil
}

// decodeBody decodes results[i]'s raw body into a T, or returns a zero
// value T if index i is out of range (results is nil/short, e.g. the
// whole batch suffered a transport failure), that specific command
// failed (results[i].Err != nil), or the body's real shape does not
// match T (a platform/version this producer has not seen) logged as a
// warning in every case, so the gap is visible instead of
// being indistinguishable from the device genuinely reporting nothing
// for that command. A failure here never affects any other index: each
// call is independent, which is the entire point of ShowResult over the
// old all-or-nothing ShowMulti error. Decoding into a typed
// T instead of a generic map, rather than trusting string-keyed lookups
// scattered through transform.go.
func decodeBody[T any](results []ShowResult, i int, logger *slog.Logger) T {
	var v T
	if i >= len(results) {
		logger.Warn("NX-OS command result unavailable (batch transport failure)", "index", i)
		return v
	}
	r := results[i]
	if r.Err != nil {
		logger.Warn("NX-OS command failed, continuing without it", "command", r.Command, "error", r.Err)
		return v
	}
	if err := json.Unmarshal(r.Body, &v); err != nil {
		logger.Warn("NX-OS command body did not match the expected shape, continuing without it", "command", r.Command, "error", err)
		var zero T
		return zero
	}
	return v
}

// recordCoverage appends one entry per command in commands to coverage,
// classifying each as "succeeded", "failed" (results[i].Err != nil) or
// "unavailable" (results is nil/short - the whole batch suffered a
// transport failure, per ShowMulti's contract). A batch that fails
// outright still yields one "unavailable" entry per command instead of
// silently vanishing from the coverage record.
func recordCoverage(coverage *[]map[string]any, commands []string, results []ShowResult) {
	for i, cmd := range commands {
		entry := map[string]any{"command": cmd}
		switch {
		case i >= len(results):
			entry["status"] = "unavailable"
		case results[i].Err != nil:
			entry["status"] = "failed"
			entry["error"] = results[i].Err.Error()
		default:
			entry["status"] = "succeeded"
		}
		*coverage = append(*coverage, entry)
	}
}

// rawCommandBodies collects every successfully-returned command's own
// raw NX-API response body across every batch given, keyed by command
// string, for --include-raw-body. A command that failed or was never
// issued (a shorter results slice than its own batch's command list) is
// simply absent from the map rather than represented with an empty/
// error value this is a lossless fallback for real data, not a
// coverage report (see recordCoverage for that).
//
// Every body is passed through redactRawBody first: raw NX-API output
// carries fields this producer deliberately never models elsewhere
// (TACACS+/RADIUS secretKey and testPassword, SNMP community strings,
// key hashes), and attaching a verbatim body would reintroduce exactly
// what --purpose audit's own transforms are careful to exclude. A body
// that cannot be parsed for redaction is dropped, not attached
// un-redacted.
func rawCommandBodies(batches ...[]ShowResult) map[string]json.RawMessage {
	raw := make(map[string]json.RawMessage)
	for _, batch := range batches {
		for _, r := range batch {
			if r.Err != nil || len(r.Body) == 0 {
				continue
			}
			if redacted, ok := redactRawBody(r.Body); ok {
				raw[r.Command] = redacted
			}
		}
	}
	return raw
}

// redactRawBodyMarker replaces any value whose key or content indicates
// a secret. A fixed marker (rather than deletion) keeps the raw body's
// shape intact for the fields that are safe.
const redactRawBodyMarker = "[REDACTED]"

// redactRawBody parses body as arbitrary JSON, replaces every value
// under a key sdk.IsSensitiveKey flags and every string value
// sdk.ScanValue flags with redactRawBodyMarker (recursively, through
// objects and arrays), and re-marshals. Returns ok=false if the body is
// not parseable JSON, so an un-redactable body is never attached.
func redactRawBody(body json.RawMessage) (json.RawMessage, bool) {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return nil, false
	}
	v = redactValue(v, false)
	out, err := json.Marshal(v)
	if err != nil {
		return nil, false
	}
	return out, true
}

// redactValue walks a decoded JSON value. keyIsSensitive is true when
// the caller reached this value through a sensitive object key, in
// which case the whole subtree collapses to the marker.
func redactValue(v any, keyIsSensitive bool) any {
	if keyIsSensitive {
		return redactRawBodyMarker
	}
	switch t := v.(type) {
	case map[string]any:
		for key, val := range t {
			t[key] = redactValue(val, sdk.IsSensitiveKey(key))
		}
		return t
	case []any:
		for i, val := range t {
			t[i] = redactValue(val, false)
		}
		return t
	case string:
		if sdk.ScanValue(t) != "" {
			return redactRawBodyMarker
		}
		return t
	default:
		return v
	}
}

func printHelp() {
	fmt.Printf(`osirisjson-producer cisco nxos - Cisco NX-OS OSIRIS JSON producer

Usage:
  osirisjson-producer cisco nxos [flags]

Flags:
  -h, --host          	Target host (IP or FQDN, optionally with :port); prompted for interactively when omitted
  -u, --username      	Username for authentication; prompted for interactively when omitted
  -P, --port          	Override default port
  --secrets-file      	JSON file with {host, username, password} (see below); overrides -h/-u when they omit a field
  -s, --source        	CSV file for batch mode (mutually exclusive with -h/--host)
  -o, --output        	Output directory for batch mode
  --include-raw-body  	Attach each collected NX-API command's full, unmodified response under
                      	extensions["osiris.cisco"] on the device resource (requires --purpose audit;
                      	a lossless fallback for fields not yet modeled into OSIRIS JSON)
  --safe-failure-mode 	Secret handling: fail-closed, log-and-redact, off (default: fail-closed)
  --insecure          	Skip TLS certificate verification

--secrets-file accepts two shapes, each generated as its own file by
"nxos template --generate" (see below) so you never have to guess the JSON by hand:
  cisco-nxos-secrets.json           a single login: {"host", "username", "password"}
  cisco-nxos-secrets-multihost.json different logins per target in a batch,
                                    matched by exact host/IP or CIDR, with a
                                    "default" fallback for anything unmatched
The file must be a regular file (not a symlink) readable only by its
owner (e.g. chmod 0600) a looser file is rejected.

Batch mode (-s/--source): a CSV file with columns datacenter,floor,room,rack,hostname,management_ip,port
datacenter/floor/room/rack are optional; when present they build the output
directory structure: <output>/Datacenter/Floor/Room/Rack/Hostname.json.

  <name> template --generate	Write cisco-nxos-template.csv and both --secrets-file skeletons

Output:
  single mode saves to: cisco-nxos-<timestamp>-<hostname>.json (0600 permissions)
  batch mode saves to:  <output>/Datacenter/Floor/Room/Rack/Hostname.json (0600 permissions)

Examples:
  osirisjson-producer cisco nxos -h switch.lab:8443 -u username --insecure
  osirisjson-producer cisco nxos -h 192.0.2.10 -u username --purpose audit
  osirisjson-producer cisco nxos -s datacenter.csv -o ./output -u username
  osirisjson-producer cisco nxos template --generate
`, passwordEnvVar, defaultPort, osirismeta.PurposeHelp())
}
