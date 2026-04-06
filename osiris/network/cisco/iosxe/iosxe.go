// Package iosxe implements the Cisco IOS-XE producer for OSIRIS JSON.
// Queries the NETCONF/YANG interface over SSH to discover device topology and
// generates an OSIRIS JSON document with resources, groups and connections.
//
// For an introduction to OSIRIS JSON Producer for Cisco see:
// "[OSIRIS-JSON-CISCO]."
//
// [OSIRIS-JSON-CISCO]: https://osirisjson.org/en/docs/producers/network/cisco
package iosxe

import (
	"fmt"

	"go.osirisjson.org/producers/osiris/network/cisco/run"
	"go.osirisjson.org/producers/pkg/sdk"
)

const (
	generatorName    = "osirisjson-producer-cisco-iosxe"
	generatorVersion = "0.1.0"
)

// Producer implements the IOS-XE sub-producer.
type Producer struct {
	target run.TargetConfig
	cfg    *run.RunConfig
	client *Client // injectable for testing
}

// NewFactory returns a ProducerFactory for the IOS-XE producer.
func NewFactory() run.ProducerFactory {
	return func(target run.TargetConfig, cfg *run.RunConfig) sdk.Producer {
		return &Producer{target: target, cfg: cfg}
	}
}

// Collect queries the IOS-XE device via NETCONF and builds an OSIRIS document.
func (p *Producer) Collect(ctx *sdk.Context) (*sdk.Document, error) {
	client := p.client
	if client == nil {
		client = NewClient(p.target, p.cfg.InsecureTLS, ctx.Logger)
		if err := client.Connect(p.target.Username, p.target.Password); err != nil {
			return nil, fmt.Errorf("IOS-XE NETCONF connection failed: %w", err)
		}
		defer client.Close()
	}

	hostname := p.target.Hostname
	if hostname == "" {
		hostname = p.target.Host
	}

	ctx.Logger.Info("collecting IOS-XE device data", "host", p.target.Host)

	// Minimal queries (5 RPCs).

	// 1. Native config (version, hostname).
	nativeXML, err := client.GetConfig(`<native xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-native"><version/><hostname/></native>`)
	if err != nil {
		return nil, fmt.Errorf("IOS-XE native config query failed: %w", err)
	}

	// 2. Interfaces.
	ifXML, err := client.Get(`<interfaces xmlns="urn:ietf:params:xml:ns:yang:ietf-interfaces"/>`)
	if err != nil {
		return nil, fmt.Errorf("IOS-XE interfaces query failed: %w", err)
	}

	// 3. Hardware inventory.
	hwXML, err := client.Get(`<device-hardware-data xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-device-hardware-oper"/>`)
	if err != nil {
		ctx.Logger.Warn("IOS-XE hardware query failed (continuing)", "err", err)
		hwXML = nil
	}

	// 4. CDP neighbors.
	cdpXML, err := client.Get(`<cdp-neighbor-details xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-cdp-oper"/>`)
	if err != nil {
		ctx.Logger.Warn("IOS-XE CDP query failed (continuing)", "err", err)
		cdpXML = nil
	}

	// 5. VRF definitions.
	vrfXML, err := client.GetConfig(`<native xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-native"><vrf/></native>`)
	if err != nil {
		ctx.Logger.Warn("IOS-XE VRF query failed (continuing)", "err", err)
		vrfXML = nil
	}

	// Transform device.
	deviceResource, _ := TransformDevice(hostname, nativeXML, hwXML)

	// Add inventory to device extension.
	inventory := TransformInventory(hwXML)
	if len(inventory) > 0 {
		ensureCiscoExtension(&deviceResource.Extensions)
		deviceResource.Extensions[extensionNamespace].(map[string]any)["inventory"] = inventory
	}

	// Transform interfaces.
	ifResources, ifNameToID := TransformInterfaces(hostname, ifXML)

	// Transform CDP neighbors -> connections + stubs.
	connections, stubs := TransformCDPNeighbors(hostname, cdpXML, ifNameToID)

	// Transform groups.
	vrfGroups, vrfNameToGroupID := TransformVRFs(hostname, vrfXML)

	// Wire relationships.
	WireInterfacesToVRFs(vrfXML, ifNameToID, vrfGroups, vrfNameToGroupID)

	// Detailed mode: BGP, OSPF, CPU, memory, interface counters
	if ctx.Config != nil && ctx.Config.DetailLevel == "detailed" {
		// BGP neighbors.
		bgpXML, err := client.Get(`<bgp-state-data xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-bgp-oper"/>`)
		if err != nil {
			ctx.Logger.Warn("IOS-XE BGP query failed", "err", err)
		} else {
			bgpNeighbors := TransformBGPNeighbors(bgpXML)
			if len(bgpNeighbors) > 0 {
				ensureCiscoExtension(&deviceResource.Extensions)
				deviceResource.Extensions[extensionNamespace].(map[string]any)["bgp_neighbors"] = bgpNeighbors
			}
		}

		// OSPF processes.
		ospfXML, err := client.Get(`<ospf-oper-data xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-ospf-oper"/>`)
		if err != nil {
			ctx.Logger.Warn("IOS-XE OSPF query failed", "err", err)
		} else {
			ospfProcesses := TransformOSPF(ospfXML)
			if len(ospfProcesses) > 0 {
				ensureCiscoExtension(&deviceResource.Extensions)
				deviceResource.Extensions[extensionNamespace].(map[string]any)["ospf_processes"] = ospfProcesses
			}
		}

		// CPU utilization.
		cpuXML, err := client.Get(`<cpu-usage xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-process-cpu-oper"/>`)
		if err != nil {
			ctx.Logger.Warn("IOS-XE CPU query failed", "err", err)
			cpuXML = nil
		}

		// Memory statistics.
		memXML, err := client.Get(`<memory-statistics xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-memory-oper"/>`)
		if err != nil {
			ctx.Logger.Warn("IOS-XE memory query failed", "err", err)
			memXML = nil
		}

		cpuMemExt := TransformCPUMemory(cpuXML, memXML)
		if len(cpuMemExt) > 0 {
			ensureCiscoExtension(&deviceResource.Extensions)
			cisco := deviceResource.Extensions[extensionNamespace].(map[string]any)
			for k, v := range cpuMemExt {
				cisco[k] = v
			}
		}

		// Interface counters enrichment.
		EnrichInterfaceCounters(ifXML, ifResources, ifNameToID)
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

	// Add CDP stub resources.
	for _, r := range stubs {
		builder.AddResource(r)
	}

	// Add connections.
	for _, c := range connections {
		builder.AddConnection(c)
	}

	// Add groups.
	for _, g := range vrfGroups {
		builder.AddGroup(g)
	}

	doc, err := builder.Build()
	if err != nil {
		return nil, fmt.Errorf("document build failed: %w", err)
	}

	ctx.Logger.Info("IOS-XE collection complete",
		"resources", len(doc.Topology.Resources),
		"connections", len(doc.Topology.Connections),
		"groups", len(doc.Topology.Groups),
	)

	return doc, nil
}
