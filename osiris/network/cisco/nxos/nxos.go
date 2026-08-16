// Package nxos implements the Cisco NX-OS producer for OSIRIS JSON.
// Queries the NX-API CLI interface to discover device topology and
// generates an OSIRIS JSON document with resources, groups, connections.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification
package nxos

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"go.osirisjson.org/producers/osiris/network/cisco/run"
	"go.osirisjson.org/producers/pkg/sdk"
)

const (
	generatorName    = "osirisjson-producer-cisco-nxos"
	generatorVersion = "0.1.0"
)

// Producer implements the NX-OS sub-producer.
type Producer struct {
	target run.TargetConfig
	cfg    *run.RunConfig
	client *Client // injectable for testing
}

// NewFactory returns a ProducerFactory for the NX-OS producer.
func NewFactory() run.ProducerFactory {
	return func(target run.TargetConfig, cfg *run.RunConfig) sdk.Producer {
		return &Producer{target: target, cfg: cfg}
	}
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
	ifBriefData := decodeBody[interfaceBriefResponse](batch1, offset+1, ctx.Logger)
	vlanBriefData := decodeBody[vlanBriefResponse](batch1, offset+2, ctx.Logger)
	vrfDetailData := decodeBody[vrfDetailResponse](batch1, offset+3, ctx.Logger)
	vrfInterfaceData := decodeBody[vrfInterfaceResponse](batch1, offset+4, ctx.Logger)

	// Batch 2: optional features LLDP, vPC, port-channel. These may
	// not be enabled/configured on all devices; each command's own
	// success or failure is independent of its siblings
	// LLDP being disabled, for example, must not also discard vPC or
	// port-channel data that was fetched successfully in the same call.
	batch2Commands := []string{
		"show lldp neighbors detail",
		"show vpc brief",
		"show port-channel summary",
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

	// Transform device.
	deviceResource, _ := TransformDevice(hostname, *versionData)

	// Add inventory to device extension.
	inventory := TransformInventory(inventoryData)
	if len(inventory) > 0 {
		ensureCiscoExtension(&deviceResource.Extensions)
		deviceResource.Extensions[extensionNamespace].(map[string]any)["inventory"] = inventory
	}

	// Transform interfaces.
	ifResources, ifNameToID := TransformInterfaces(hostname, ifBriefData)

	// Transform LLDP neighbors -> connections + stubs.
	connections, stubs := TransformLLDPNeighbors(hostname, lldpData, ifNameToID)

	// Transform groups.
	vlanGroups, vlanIDToGroupID := TransformVLANs(hostname, vlanBriefData)
	vrfGroups, vrfNameToGroupID := TransformVRFs(hostname, vrfDetailData)
	vpcGroup, _ := TransformVPC(hostname, vpcBriefData)

	// Wire relationships.
	vlanMembers := WireInterfacesToVLANs(vlanBriefData, ifBriefData, ifNameToID, vlanGroups, vlanIDToGroupID)
	vrfMembers := WireInterfacesToVRFs(vrfDetailData, vrfInterfaceData, ifNameToID, vrfGroups, vrfNameToGroupID)
	ctx.Logger.Debug("group wiring complete", "vlan_members", vlanMembers, "vrf_members", vrfMembers)
	if vpcGroup != nil {
		WirePortChannelsToVPC(vpcBriefData, ifNameToID, vpcGroup)
	}

	// Wire port-channel bundle membership: "contains" connections from
	// each LAG resource (already created above by TransformInterfaces)
	// to its physical member interfaces, plus a member_count property
	// on the LAG resource itself.
	pcConnections := TransformPortChannels(portChannelData, ifResources, ifNameToID)
	connections = append(connections, pcConnections...)

	// Detailed mode: interface counters, system resources, environment.
	// Each command is independent: "show environment" failing on a
	// platform that doesn't support it, for example, must
	// not also discard interface counters or system resources fetched
	// successfully in the same call.
	if ctx.Config != nil && ctx.Config.DetailLevel == "detailed" {
		batch3Commands := []string{
			"show interface",
			"show system resources",
			"show environment",
		}
		batch3, err := client.ShowMulti(batch3Commands)
		if err != nil {
			ctx.Logger.Warn("NX-OS detailed batch transport failure", "err", err)
			batch3 = nil
		}
		recordCoverage(&coverage, batch3Commands, batch3)

		ifDetailData := decodeBody[interfaceDetailResponse](batch3, 0, ctx.Logger)
		sysResData := decodeBody[systemResourcesResponse](batch3, 1, ctx.Logger)
		envData := decodeBody[environmentResponse](batch3, 2, ctx.Logger)

		// Enrich interfaces with detailed counters.
		EnrichInterfaceDetails(hostname, ifDetailData, ifResources, ifNameToID)

		// Add system resources to device extension.
		sysExt := TransformSystemResources(sysResData)
		if len(sysExt) > 0 {
			ensureCiscoExtension(&deviceResource.Extensions)
			cisco := deviceResource.Extensions[extensionNamespace].(map[string]any)
			for k, v := range sysExt {
				cisco[k] = v
			}
		}

		// Add environment to device extension.
		envExt := TransformEnvironment(envData)
		if len(envExt) > 0 {
			ensureCiscoExtension(&deviceResource.Extensions)
			cisco := deviceResource.Extensions[extensionNamespace].(map[string]any)
			for k, v := range envExt {
				cisco[k] = v
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

	// Assemble the document.
	builder := sdk.NewDocumentBuilder(ctx).
		WithGenerator(generatorName, generatorVersion).
		WithScope(sdk.Scope{
			Providers: []string{providerName},
		})

	// Add device resource.
	builder.AddResource(deviceResource)

	// Add interface resources.
	for _, r := range ifResources {
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

	// Add groups.
	for _, g := range vlanGroups {
		builder.AddGroup(g)
	}
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
