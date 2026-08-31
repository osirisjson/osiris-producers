// Package apic implements the Cisco ACI/APIC producer for OSIRIS JSON.
// Queries the APIC REST API to discover ACI fabric topology and
// generates an OSIRIS JSON document with resources, groups
// and connections.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification
package apic

import (
	"context"
	"fmt"

	"go.osirisjson.org/producers/osiris/network/cisco/run"
	"go.osirisjson.org/producers/pkg/sdk"
)

const (
	generatorName    = "osirisjson-producer-cisco-apic"
	generatorVersion = "0.2.0"
)

type Producer struct {
	target run.TargetConfig
	cfg    *Config
	client *Client         // injectable for testing
	ctx    context.Context // root context for HTTP; nil -> context.Background
}

// Collect queries the APIC and builds an OSIRIS JSON document.
func (p *Producer) Collect(ctx *sdk.Context) (*sdk.Document, error) {
	reqCtx := p.ctx
	if reqCtx == nil {
		reqCtx = context.Background()
	}

	client := p.client
	ownClient := client == nil
	if ownClient {
		ctx.Logger.Info("connecting to APIC", "host", p.target.Host, "user", p.target.Username)
		client = NewClient(reqCtx, p.target, p.cfg.InsecureTLS, ctx.Logger)
		if err := client.Login(p.target.Username, p.target.Password); err != nil {
			return nil, fmt.Errorf("APIC authentication failed: %w", err)
		}
		// Best-effort session cleanup. A logout failure is logged,
		// never returned: it must not mask the collection result.
		// Skipped for an injected client, which holds no
		// real APIC session.
		defer func() {
			if err := client.Logout(); err != nil {
				ctx.Logger.Warn("APIC logout failed", "error", err)
			}
		}()
	}

	audit := ctx.Config != nil && ctx.Config.Purpose == "audit"
	purpose := "documentation"
	if audit {
		purpose = "audit"
	}

	plan := discoveryPlan(audit)
	ctx.Logger.Info("collecting APIC fabric data",
		"host", p.target.Host, "purpose", purpose, "operations", len(plan))

	// Run the discovery plan. An essential or structural failure aborts
	// the document; an optional failure is logged, recorded in the
	// coverage report, and the run continues with a partial document.
	cov := &coverageRecorder{}
	raw := make(map[string][]map[string]any)
	for i, dc := range plan {
		ctx.Logger.Info("querying APIC class",
			"step", fmt.Sprintf("%d/%d", i+1, len(plan)), "class", dc.name, "criticality", dc.crit.String())
		objs, qErr := client.QueryClass(dc.name)
		if qErr != nil {
			if dc.crit.aborts() {
				return nil, fmt.Errorf("APIC discovery %q (%s) failed: %w", dc.name, dc.crit, qErr)
			}
			cat := sanitizeFailureCategory(qErr)
			ctx.Logger.Warn("optional APIC discovery failed; document will be partial",
				"class", dc.name, "category", cat)
			cov.failed(dc.name, cat)
			continue
		}
		raw[dc.name] = objs
		cov.succeeded(dc.name, len(objs))
	}
	if !audit {
		cov.skipped("fvCEp")
		cov.skipped("fvIp")
	}
	covOK, covFailed, covSkipped := cov.tally()
	ctx.Logger.Info("APIC discovery complete",
		"succeeded", covOK, "failed", covFailed, "skipped", covSkipped)

	nodes := raw["fabricNode"]
	systems := raw["topSystem"]
	firmware := raw["firmwareRunning"]
	tenantAttrs := raw["fvTenant"]
	vrfAttrs := raw["fvCtx"]
	bdAttrs := raw["fvBD"]
	subnetAttrs := raw["fvSubnet"]
	epgAttrs := raw["fvAEPg"]
	l3outAttrs := raw["l3extOut"]
	bdToCtxAttrs := raw["fvRsCtx"]
	epgToBdAttrs := raw["fvRsBd"]
	l3outToCtxAttrs := raw["l3extRsEctx"]
	faultAttrs := raw["faultInst"]
	endpointAttrs := raw["fvCEp"]  // nil unless --purpose audit
	endpointIPAttrs := raw["fvIp"] // nil unless --purpose audit

	// Transform APIC data to OSIRIS types.
	ctx.Logger.Info("transforming ACI objects to OSIRIS resources and groups")
	nodeResources := TransformNodes(nodes, systems, firmware)
	tenantGroups, tenantDNToID := TransformTenants(tenantAttrs)
	vrfGroups, vrfDNToID := TransformVRFs(vrfAttrs)
	bdResources, bdDNToID := TransformBridgeDomains(bdAttrs)
	subnetResources := TransformSubnets(subnetAttrs)
	epgGroups, epgDNToID := TransformEPGs(epgAttrs)
	l3outResources, l3outDNToID := TransformL3Outs(l3outAttrs)

	// Wire relationships ("how it relates").
	ctx.Logger.Info("wiring tenant, VRF, bridge-domain and EPG relationships")
	// Tenant children: VRFs and EPGs are child groups of
	// their parent tenant.
	WireVRFsToTenants(vrfAttrs, vrfDNToID, tenantDNToID, tenantGroups)
	WireEPGsToTenants(epgAttrs, epgDNToID, tenantDNToID, tenantGroups)

	// Tenant members: BDs, subnets and L3Outs are resource
	// members of their tenant.
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

	// Surface the discovery-coverage record on the fabric-identity
	// resources (the controllers) so a consumer holding only the
	// document can see which optional domains are absent and why.
	attachCoverage(nodeResources, cov.asSlice())

	scopePurpose := ""
	if ctx.Config != nil {
		scopePurpose = ctx.Config.Purpose
	}

	// Assemble the document. scope.name is the collection target's
	// hostname (one OSIRIS document per APIC/fabric).
	ctx.Logger.Info("assembling OSIRIS JSON document")
	scopeName := p.target.Hostname
	if scopeName == "" {
		scopeName = p.target.Host
	}
	coverageSummary := cov.summary(firstFabricDomain(systems))
	builder := sdk.NewDocumentBuilder(ctx).
		WithGenerator(generatorName, generatorVersion).
		WithScope(sdk.Scope{
			Name:        scopeName,
			Providers:   []string{providerName},
			Purpose:     scopePurpose,
			Description: coverageSummary,
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
		for _, r := range TransformEndpoints(endpointAttrs, endpointIPAttrs) {
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

	if covFailed > 0 {
		ctx.Logger.Warn("APIC document is partial", "coverage", coverageSummary)
	}
	ctx.Logger.Info("APIC collection complete",
		"resources", len(doc.Topology.Resources),
		"connections", len(doc.Topology.Connections),
		"groups", len(doc.Topology.Groups),
		"coverage", coverageSummary,
	)

	return doc, nil
}
