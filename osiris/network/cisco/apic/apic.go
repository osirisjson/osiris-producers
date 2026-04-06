// Package apic implements the Cisco ACI/APIC producer for OSIRIS JSON.
// Queries the APIC REST API to discover ACI fabric topology and generates
// an OSIRIS JSON document with resources, groups and connections.
//
// For an introduction to OSIRIS JSON Producer for Cisco see:
// "[OSIRIS-JSON-CISCO]."
//
// [OSIRIS-JSON-CISCO]: https://osirisjson.org/en/docs/producers/network/cisco
package apic

import (
	"fmt"

	"go.osirisjson.org/producers/osiris/network/cisco/run"
	"go.osirisjson.org/producers/pkg/sdk"
)

const (
	generatorName    = "osirisjson-producer-cisco-apic"
	generatorVersion = "0.1.0"
)

// Producer implements the APIC sub-producer.
type Producer struct {
	target run.TargetConfig
	cfg    *run.RunConfig
	client *Client // injectable for testing
}

// NewFactory returns a ProducerFactory for the APIC producer.
func NewFactory() run.ProducerFactory {
	return func(target run.TargetConfig, cfg *run.RunConfig) sdk.Producer {
		return &Producer{target: target, cfg: cfg}
	}
}

// Collect queries the APIC and builds an OSIRIS document.
func (p *Producer) Collect(ctx *sdk.Context) (*sdk.Document, error) {
	client := p.client
	if client == nil {
		client = NewClient(p.target, p.cfg.InsecureTLS, ctx.Logger)
		if err := client.Login(p.target.Username, p.target.Password); err != nil {
			return nil, fmt.Errorf("APIC authentication failed: %w", err)
		}
	}

	ctx.Logger.Info("collecting APIC fabric data", "host", p.target.Host)

	// Query all required classes.
	nodes, err := client.QueryClass("fabricNode")
	if err != nil {
		return nil, fmt.Errorf("query fabricNode: %w", err)
	}

	systems, err := client.QueryClass("topSystem")
	if err != nil {
		return nil, fmt.Errorf("query topSystem: %w", err)
	}

	firmware, err := client.QueryClass("firmwareRunning")
	if err != nil {
		return nil, fmt.Errorf("query firmwareRunning: %w", err)
	}

	tenantAttrs, err := client.QueryClass("fvTenant")
	if err != nil {
		return nil, fmt.Errorf("query fvTenant: %w", err)
	}

	vrfAttrs, err := client.QueryClass("fvCtx")
	if err != nil {
		return nil, fmt.Errorf("query fvCtx: %w", err)
	}

	bdAttrs, err := client.QueryClass("fvBD")
	if err != nil {
		return nil, fmt.Errorf("query fvBD: %w", err)
	}

	subnetAttrs, err := client.QueryClass("fvSubnet")
	if err != nil {
		return nil, fmt.Errorf("query fvSubnet: %w", err)
	}

	epgAttrs, err := client.QueryClass("fvAEPg")
	if err != nil {
		return nil, fmt.Errorf("query fvAEPg: %w", err)
	}

	l3outAttrs, err := client.QueryClass("l3extOut")
	if err != nil {
		return nil, fmt.Errorf("query l3extOut: %w", err)
	}

	// Relationship classes for connections.
	bdToCtxAttrs, err := client.QueryClass("fvRsCtx")
	if err != nil {
		return nil, fmt.Errorf("query fvRsCtx: %w", err)
	}

	epgToBdAttrs, err := client.QueryClass("fvRsBd")
	if err != nil {
		return nil, fmt.Errorf("query fvRsBd: %w", err)
	}

	l3outToCtxAttrs, err := client.QueryClass("l3extRsEctx")
	if err != nil {
		return nil, fmt.Errorf("query l3extRsEctx: %w", err)
	}

	// Faults are always fetched - audit-critical regardless of detail level.
	faultAttrs, err := client.QueryClass("faultInst")
	if err != nil {
		return nil, fmt.Errorf("query faultInst: %w", err)
	}

	// Detailed mode: also fetch endpoints.
	var endpointAttrs []map[string]any
	if ctx.Config != nil && ctx.Config.DetailLevel == "detailed" {
		endpointAttrs, err = client.QueryClass("fvCEp")
		if err != nil {
			return nil, fmt.Errorf("query fvCEp: %w", err)
		}
	}

	// Transform APIC data to OSIRIS types.
	nodeResources := TransformNodes(nodes, systems, firmware)
	tenantGroups, tenantDNToID := TransformTenants(tenantAttrs)
	vrfGroups, vrfDNToID := TransformVRFs(vrfAttrs)
	bdResources, bdDNToID := TransformBridgeDomains(bdAttrs)
	subnetResources := TransformSubnets(subnetAttrs)
	epgGroups, epgDNToID := TransformEPGs(epgAttrs)
	l3outResources, l3outDNToID := TransformL3Outs(l3outAttrs)

	// Wire relationships ("how it relates").
	// Tenant children: VRFs and EPGs are child groups of their parent tenant.
	WireVRFsToTenants(vrfAttrs, vrfDNToID, tenantDNToID, tenantGroups)
	WireEPGsToTenants(epgAttrs, epgDNToID, tenantDNToID, tenantGroups)

	// Tenant members: BDs, subnets and L3Outs are resource members of their tenant.
	WireBDsToTenants(bdAttrs, bdDNToID, tenantDNToID, tenantGroups)
	WireSubnetsToTenants(subnetAttrs, tenantDNToID, tenantGroups)
	WireL3OutsToTenants(l3outAttrs, tenantDNToID, tenantGroups)

	// Wire ACI relationship classes into group membership.
	// BD -> VRF: BDs become members of their VRF group.
	WireBDsToVRFs(bdToCtxAttrs, bdDNToID, vrfDNToID, vrfGroups)
	// L3Out -> VRF: L3Outs become members of their VRF group.
	WireL3OutsToVRFs(l3outToCtxAttrs, l3outDNToID, vrfDNToID, vrfGroups)
	// EPG -> BD: BDs become members of their EPG group.
	WireEPGsToBDs(epgToBdAttrs, epgDNToID, bdDNToID, epgGroups)

	// Detailed mode: wire endpoints as members of their EPG groups.
	if len(endpointAttrs) > 0 {
		WireEndpointsToEPGs(endpointAttrs, epgDNToID, epgGroups)
	}

	// Wire fault extensions.
	faultsByDN := TransformFaults(faultAttrs)
	WireFaultsToNodes(nodeResources, faultsByDN)
	WireFaultsToTenants(tenantGroups, tenantDNToID, faultsByDN)

	// Assemble the document.
	builder := sdk.NewDocumentBuilder(ctx).
		WithGenerator(generatorName, generatorVersion).
		WithScope(sdk.Scope{
			Providers: []string{providerName},
		})

	for _, r := range nodeResources {
		builder.AddResource(r)
	}
	for _, r := range bdResources {
		builder.AddResource(r)
	}
	for _, r := range subnetResources {
		builder.AddResource(r)
	}
	for _, r := range l3outResources {
		builder.AddResource(r)
	}

	if len(endpointAttrs) > 0 {
		for _, r := range TransformEndpoints(endpointAttrs) {
			builder.AddResource(r)
		}
	}

	for _, g := range tenantGroups {
		builder.AddGroup(g)
	}
	for _, g := range vrfGroups {
		builder.AddGroup(g)
	}
	for _, g := range epgGroups {
		builder.AddGroup(g)
	}

	doc, err := builder.Build()
	if err != nil {
		return nil, fmt.Errorf("document build failed: %w", err)
	}

	ctx.Logger.Info("APIC collection complete",
		"resources", len(doc.Topology.Resources),
		"connections", len(doc.Topology.Connections),
		"groups", len(doc.Topology.Groups),
	)

	return doc, nil
}
