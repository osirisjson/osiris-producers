// Package nxos implements the Cisco NX-OS producer for OSIRIS JSON.
// Queries the NX-API CLI interface to discover device topology and generates
// an OSIRIS JSON document with resources, groups and connections.
//
// For an introduction to OSIRIS JSON Producer for Cisco see:
// "[OSIRIS-JSON-CISCO]."
//
// [OSIRIS-JSON-CISCO]: https://osirisjson.org/en/docs/producers/cisco
package nxos

import (
	"fmt"

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

	// Batch 1: core device data (6 commands).
	batch1, err := client.ShowMulti([]string{
		"show version",
		"show inventory",
		"show interface brief",
		"show vlan brief",
		"show vrf all detail",
		"show lldp neighbors detail",
	})
	if err != nil {
		return nil, fmt.Errorf("NX-OS batch 1 query failed: %w", err)
	}

	versionData := batch1[0]
	inventoryData := batch1[1]
	ifBriefData := batch1[2]
	vlanBriefData := batch1[3]
	vrfDetailData := batch1[4]
	lldpData := batch1[5]

	// Batch 2: vPC and port-channel (2 commands).
	// vPC may not be configured - handle gracefully.
	batch2, err := client.ShowMulti([]string{
		"show vpc brief",
		"show port-channel summary",
	})
	if err != nil {
		// vPC not configured is common; treat as empty.
		ctx.Logger.Warn("NX-OS batch 2 query failed (vPC may not be configured)", "err", err)
		batch2 = []map[string]any{{}, {}}
	}

	vpcBriefData := batch2[0]

	// Transform device.
	deviceResource, _ := TransformDevice(hostname, versionData)

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
	WireInterfacesToVLANs(vlanBriefData, ifNameToID, vlanGroups, vlanIDToGroupID)
	WireInterfacesToVRFs(vrfDetailData, ifNameToID, vrfGroups, vrfNameToGroupID)
	if vpcGroup != nil {
		WirePortChannelsToVPC(vpcBriefData, ifNameToID, vpcGroup)
	}

	// Detailed mode: interface counters, system resources, environment.
	if ctx.Config != nil && ctx.Config.DetailLevel == "detailed" {
		batch3, err := client.ShowMulti([]string{
			"show interface",
			"show system resources",
			"show environment",
		})
		if err != nil {
			ctx.Logger.Warn("NX-OS detailed query failed", "err", err)
		} else {
			ifDetailData := batch3[0]
			sysResData := batch3[1]
			envData := batch3[2]

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
