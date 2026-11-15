// Package apic implements the Cisco ACI/APIC producer for OSIRIS JSON.
// Queries the APIC REST API to discover ACI fabric topology and
// generates an OSIRIS JSON document with resources, groups
// and connections.
//
// This file holds the whole CLI surface (Run, the single/batch runners,
// --help, template generation) alongside the Producer type and its
// Collect, the same one-file shape. The per-domain APIC ->
// OSIRIS mapping lives in the transform_<area>.go files; relationship
// wiring is in wire.go, the discovery plan in discover.go and the
// typed response envelope in client.go.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification
package apic

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"go.osirisjson.org/producers/osiris/network/cisco/run"
	"go.osirisjson.org/producers/pkg/sdk"
)

const (
	generatorName    = "osirisjson-producer-cisco-apic"
	generatorVersion = "0.4.0"
	generatorURL     = "https://docs.osirisjson.org/osiris-producers/network/cisco"
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
	// coverage report and the run continues with a partial document.
	cov := &coverageRecorder{}
	raw := make(map[string][]map[string]any)
	for i, dc := range plan {
		ctx.Logger.Info("querying APIC class",
			"step", fmt.Sprintf("%d/%d", i+1, len(plan)), "class", dc.name, "criticality", dc.crit.String())
		q, qErr := client.queryClass(dc.name)
		if qErr == nil && q.rejected > 0 && dc.crit.aborts() {
			qErr = fmt.Errorf("%d object(s): %w", q.rejected, errRejectedObjects)
		}
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
		objs := dedupeByDN(append([]map[string]any(nil), q.objs...), dc.name, ctx.Logger)
		raw[dc.name] = objs
		cov.succeededQuery(dc.name, q, len(objs))
	}
	if !audit {
		cov.skipped("vpcDom")
		cov.skipped("vzEntry")
		cov.skipped("fvCEp")
		cov.skipped("fvIp")
	}
	covOK, covFailed, covSkipped := cov.tally()
	ctx.Logger.Info("APIC discovery complete",
		"succeeded", covOK, "failed", covFailed, "skipped", covSkipped)

	nodes := raw["fabricNode"]
	systems := raw["topSystem"]

	// Minimum visible inventory. An APIC always reports at least its
	// own controller in fabricNode and topSystem; an empty answer means
	// the account cannot see the fabric (security-domain or role
	// limits) and a document claiming an empty fabric would be false.
	if len(nodes) == 0 || len(systems) == 0 {
		return nil, fmt.Errorf("APIC discovery returned no fabric inventory (fabricNode=%d, topSystem=%d): %w",
			len(nodes), len(systems), errNoFabricInventory)
	}
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

	// Physical-topology classes.
	l1Attrs := raw["l1PhysIf"]
	ethpmAttrs := raw["ethpmPhysIf"]
	fabricLinkAttrs := raw["fabricLink"]
	lldpAttrs := raw["lldpAdjEp"]
	cdpAttrs := raw["cdpAdjEp"]
	pcAttrs := raw["pcAggrIf"]
	pathAttAttrs := raw["fvRsPathAtt"]

	// Transform APIC data to OSIRIS types.
	ctx.Logger.Info("transforming ACI objects to OSIRIS resources and groups")
	nodeResources := TransformNodes(nodes, systems, firmware)
	if len(nodeResources) == 0 {
		return nil, fmt.Errorf("APIC fabricNode returned %d object(s) but none has a supported role: %w",
			len(nodes), errNoFabricInventory)
	}
	tenantGroups, tenantDNToID := TransformTenants(tenantAttrs)
	vrfGroups, vrfDNToID := TransformVRFs(vrfAttrs)
	bdResources, bdDNToID := TransformBridgeDomains(bdAttrs)
	subnetResources := TransformSubnets(subnetAttrs)
	epgGroups, epgDNToID := TransformEPGs(epgAttrs)
	l3outResources, l3outDNToID := TransformL3Outs(l3outAttrs)
	appGroups, appDNToID := TransformAppProfiles(raw["fvAp"])

	// Physical fabric: switch ports (bounded to topology-participating
	// ports in documentation mode, every port under --purpose audit)
	// and port-channel aggregate interfaces.
	numToDN := nodeNumIndex(nodes)
	nodeIDs := resourceIDSet(nodeResources)
	pathRes := newPathResolver(nodes, pcAttrs)
	var portKeep map[string]bool
	if !audit {
		attachments := append(append([]map[string]any(nil), pathAttAttrs...), raw["l3extRsPathL3OutAtt"]...)
		portKeep = topologyPortDNs(fabricLinkAttrs, lldpAttrs, cdpAttrs, attachments, ethpmAttrs, raw["pcRsMbrIfs"], numToDN, pathRes)
	}
	portResources, portDNToID := TransformSwitchPorts(l1Attrs, ethpmAttrs, portKeep)
	pcResources, pcDNToID := TransformPortChannels(pcAttrs)

	// Wire relationships ("how it relates").
	ctx.Logger.Info("wiring tenant, VRF, bridge-domain and EPG relationships")
	// Tenant children: VRFs and EPGs are child groups of
	// their parent tenant.
	WireVRFsToTenants(vrfAttrs, vrfDNToID, tenantDNToID, tenantGroups)
	// EPGs nest under their application-profile group when it was
	// collected, otherwise directly under their tenant.
	placed := WireAppProfiles(raw["fvAp"], appDNToID, appGroups, epgAttrs, epgDNToID, tenantDNToID, tenantGroups)
	var unplacedEPGs []map[string]any
	for _, e := range epgAttrs {
		if !placed[str(e, "dn")] {
			unplacedEPGs = append(unplacedEPGs, e)
		}
	}
	WireEPGsToTenants(unplacedEPGs, epgDNToID, tenantDNToID, tenantGroups)

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

	// Wire physical topology: node contains port, port-channel contains
	// member, bridge-domain contains subnet and the merged
	// fabricLink/LLDP/CDP adjacency graph.
	ctx.Logger.Info("wiring physical fabric topology (ports, links, neighbours)")
	extNeighbourResources, adjacencyConns, cdpUnresolved := WireFabricAdjacencies(fabricLinkAttrs, lldpAttrs, cdpAttrs, nodes, nodeResources)
	if cdpUnresolved > 0 {
		ctx.Logger.Warn("CDP neighbours name a fabric node that is not in the document; adjacency skipped",
			"count", cdpUnresolved)
		cov.unresolved("cdpAdjEp", cdpUnresolved)
	}
	var topoConns []sdk.Connection
	topoConns = append(topoConns, WireNodePorts(portDNToID, nodeIDs)...)
	topoConns = append(topoConns, WireNodePorts(pcDNToID, nodeIDs)...)
	pcMemberConns, pcMemberUnresolved := WirePortChannelMembers(ethpmAttrs, raw["pcRsMbrIfs"], pcDNToID, portDNToID)
	if pcMemberUnresolved > 0 {
		cov.unresolved("pcRsMbrIfs", pcMemberUnresolved)
	}
	topoConns = append(topoConns, pcMemberConns...)
	topoConns = append(topoConns, WireBDSubnets(subnetAttrs, bdDNToID)...)
	topoConns = append(topoConns, adjacencyConns...)

	// Audit mode: wire each endpoint to the port it was learned on and
	// each EPG group to its attached ports/port-channels.
	if audit {
		topoConns = append(topoConns, WireEndpointPorts(endpointAttrs, portDNToID, pcDNToID, pathRes)...)
		WireEPGPathAttachments(pathAttAttrs, epgDNToID, epgGroups, portDNToID, pcDNToID, pathRes)
	}

	// Configured policy intent: EPG/VRF resource anchors, contracts,
	// subjects and filters and forward relation edges between them.
	ctx.Logger.Info("transforming configured policy intent (contracts, subjects, filters)")
	anchorResources, epgAnchors, vrfAnchors := TransformPolicyAnchors(epgAttrs, epgDNToID, epgGroups, vrfAttrs, vrfDNToID, vrfGroups)
	contractResources, contractIdx := TransformContracts(raw["vzBrCP"])
	tally := newPolicyTally()
	extEPGResources, extEPGDNToID, extEPGConns := TransformExternalEPGs(raw["l3extInstP"], l3outDNToID, tally)
	prefixResources, prefixConns := TransformL3OutPrefixes(raw["l3extSubnet"], extEPGDNToID, tally)
	subjectResources, subjectDNToID, subjectConns := TransformSubjects(raw["vzSubj"], contractIdx, tally)
	filterResources, filterIdx := TransformFilters(raw["vzFilter"], raw["vzEntry"])
	policyConns := WirePolicyRelations(raw, policyRefs{
		epgAnchors:  epgAnchors,
		extEPGs:     extEPGDNToID,
		vrfAnchors:  vrfAnchors,
		bdDNToID:    bdDNToID,
		l3outDNToID: l3outDNToID,
		l3outs:      l3outIndex(l3outAttrs, l3outDNToID),
		contracts:   contractIdx,
		subjects:    subjectDNToID,
		filters:     filterIdx,
	}, tally)
	topoConns = append(topoConns, subjectConns...)
	topoConns = append(topoConns, policyConns...)
	topoConns = append(topoConns, extEPGConns...)
	topoConns = append(topoConns, prefixConns...)

	// Logical paths: path resources with member edges to the ports,
	// port-channels or leaf nodes that realize them, EPG -> path static
	// attachments, endpoint -> path observations, vPC domains and pods.
	ctx.Logger.Info("transforming logical paths, vPC domains and pods")
	pathResources, paths := TransformPaths(raw["fabricPathEp"], referencedPaths(raw))
	topoConns = append(topoConns, WirePathMembers(paths, pathMemberRefs{
		resolver:   pathRes,
		portDNToID: portDNToID,
		pcIDs:      pcDNToID,
		nodeIDs:    nodeIDs,
		vpcLegs:    indexVPCLegs(raw["vpcIf"], raw["vpcRsVpcConf"]),
		audit:      audit,
	}, tally)...)
	topoConns = append(topoConns, WireEPGPaths(pathAttAttrs, epgAnchors, paths, tally)...)
	topoConns = append(topoConns, WireL3OutAttachments(raw["l3extRsNodeL3OutAtt"], raw["l3extRsPathL3OutAtt"], l3outAttachRefs{
		l3outDNToID: l3outDNToID,
		nodeIDs:     nodeIDs,
		paths:       paths,
		audit:       audit,
	}, tally)...)
	if audit {
		topoConns = append(topoConns, WireEndpointPaths(endpointAttrs, paths, tally)...)
	}
	vpcResources, vpcConns, vpcPairs := TransformVPCDomains(raw["fabricExplicitGEp"], raw["fabricNodePEp"], raw["vpcDom"], numToDN, nodeIDs, audit, tally)
	LabelVPCPaths(pathResources, paths, vpcPairs)
	topoConns = append(topoConns, vpcConns...)
	podGroups, podDNToID := TransformPods(raw["fabricPod"], append(append([]sdk.Resource(nil), nodeResources...), pathResources...))
	for _, class := range sortedKeys(tally.unresolved) {
		ctx.Logger.Warn("APIC relations without an emitted source or target; no edge emitted",
			"class", class, "count", tally.unresolved[class])
		cov.unresolved(class, tally.unresolved[class])
	}
	for _, class := range sortedKeys(tally.outOfScope) {
		total := 0
		for _, scope := range sortedKeys(tally.outOfScope[class]) {
			n := tally.outOfScope[class][scope]
			total += n
			ctx.Logger.Info("APIC policy objects outside the modeled scope; not emitted",
				"class", class, "scope", scope, "count", n)
		}
		cov.outOfScope(class, total)
	}
	var policyResources []sdk.Resource
	policyResources = append(policyResources, anchorResources...)
	policyResources = append(policyResources, contractResources...)
	policyResources = append(policyResources, subjectResources...)
	policyResources = append(policyResources, filterResources...)
	policyResources = append(policyResources, extEPGResources...)
	policyResources = append(policyResources, prefixResources...)
	WirePolicyToTenants(policyResources, tenantDNToID, tenantGroups)

	// Wire fault extensions.
	faultsByDN := TransformFaults(faultAttrs)
	WireFaultsToNodes(nodeResources, faultsByDN)
	WireFaultsToTenants(tenantGroups, tenantDNToID, faultsByDN)

	// Surface the discovery-coverage record on the fabric-identity
	// resources (the controllers) so a consumer holding only the
	// document can see which optional domains are absent and why.
	attachCoverage(nodeResources, cov.asSlice())
	if cov.anySucceeded("fvAp", "vzBrCP", "vzSubj", "vzFilter", "fvRsProv", "fvRsCons", "vzRsSubjFiltAtt") {
		attachControllerExtension(nodeResources, "policy_scope", policyScope)
	}
	if cov.anySucceeded(l3outClasses...) {
		attachControllerExtension(nodeResources, "l3out_scope", l3outScope)
	}

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
		WithGenerator(generatorName, generatorVersion, generatorURL).
		WithScope(sdk.Scope{
			Name:        scopeName,
			Providers:   []string{providerName},
			Purpose:     scopePurpose,
			Description: coverageSummary,
		})

	var resources []sdk.Resource
	resources = append(resources, nodeResources...)
	resources = append(resources, bdResources...)
	resources = append(resources, subnetResources...)
	resources = append(resources, l3outResources...)
	resources = append(resources, portResources...)
	resources = append(resources, pcResources...)
	resources = append(resources, extNeighbourResources...)
	resources = append(resources, policyResources...)
	resources = append(resources, pathResources...)
	resources = append(resources, vpcResources...)
	if len(endpointAttrs) > 0 {
		resources = append(resources, TransformEndpoints(endpointAttrs, endpointIPAttrs)...)
	}
	var groups []sdk.Group
	groups = append(groups, tenantGroups...)
	groups = append(groups, vrfGroups...)
	groups = append(groups, epgGroups...)
	groups = append(groups, appGroups...)
	groups = append(groups, podGroups...)

	// --include-raw-body (audit purpose only): attach every collected
	// object's redacted attributes to the entity that owns it.
	if audit && ctx.Config.IncludeRawBody {
		ctx.Logger.Info("attaching raw APIC object attributes (--include-raw-body)")
		groupDNs := map[string]string{}
		for _, m := range []map[string]string{tenantDNToID, vrfDNToID, epgDNToID, appDNToID, podDNToID} {
			for dn, gid := range m {
				groupDNs[gid] = dn
			}
		}
		attachRawBodies(raw, resources, groups, groupDNs)
	}

	for _, r := range resources {
		builder.AddResource(r)
	}
	for _, c := range topoConns {
		builder.AddConnection(c)
	}
	for _, g := range groups {
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

// errNoFabricInventory reports that the essential fabric inventory was
// empty or contained no supported node: no document is produced.
var errNoFabricInventory = errors.New("no visible fabric inventory; check the account's role and security domains")

// errRejectedObjects reports that an essential or structural class
// returned elements that could not be identified (see queryClass).
var errRejectedObjects = errors.New("rejected: not an identified object of the requested class")

// dedupeByDN removes objects whose dn already appeared earlier in the
// same class response, preserving order. APIC class-query pagination
// can repeat an object across a page boundary when the managed-object
// tree changes during the query (see QueryClass); a repeated dn would
// otherwise produce a duplicate "cisco.apic::<dn>" resource id and fail
// the document build fail-closed. Objects with no dn are left untouched.
func dedupeByDN(objs []map[string]any, class string, logger *slog.Logger) []map[string]any {
	seen := make(map[string]struct{}, len(objs))
	out := objs[:0]
	dropped := 0
	for _, o := range objs {
		dn := str(o, "dn")
		if dn != "" {
			if _, dup := seen[dn]; dup {
				dropped++
				continue
			}
			seen[dn] = struct{}{}
		}
		out = append(out, o)
	}
	if dropped > 0 {
		logger.Warn("APIC class response contained duplicate objects; deduplicated by dn",
			"class", class, "dropped", dropped)
	}
	return out
}

// defaultTotalTimeout bounds an entire single-target collection as a
// backstop above the per-request timeout in client.go. A fabric-wide
// APIC pull is minutes, not tens of minutes; anything longer is a
// stalled run and should abort.
const defaultTotalTimeout = 30 * time.Minute

// Run is the CLI entry point for the APIC producer. It receives the
// arguments after "apic".
func Run(args []string) error {
	// Only the long form is checked here: -h is the documented short
	// flag for --host (see flags.go), so it must reach ParseFlags
	// instead of being intercepted as a help request.
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

	if cfg.InsecureTLS {
		fmt.Fprintln(os.Stderr, "warning: --insecure: TLS certificate verification is disabled; the APIC's identity is not authenticated")
	}

	if cfg.Mode == run.ModeBatch {
		return runBatch(cfg)
	}
	return runSingle(cfg)
}

// rootContext returns a context cancelled on SIGINT/SIGTERM or after
// defaultTotalTimeout, plus its stop func. Callers must defer stop().
func rootContext() (context.Context, context.CancelFunc) {
	ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	ctx, cancelTimeout := context.WithTimeout(ctx, defaultTotalTimeout)
	return ctx, func() {
		cancelTimeout()
		stopSignals()
	}
}

// templateExampleHost is the documentation-only address shown in the
// generated --secrets-file/CSV template skeletons (RFC 5737).
const templateExampleHost = "192.0.2.1"

// runTemplate writes this producer's CSV batch template
// (cisco-apic-template.csv) and both --secrets-file shapes
// (cisco-apic-secrets.json, cisco-apic-secrets-multihost.json) via the
// shared run.GenerateTemplates.
func runTemplate(args []string) error {
	if len(args) == 0 || (args[0] != "--generate" && args[0] != "-g") {
		fmt.Println("Usage: osirisjson-producer cisco apic template --generate")
		return nil
	}
	return run.GenerateTemplates("apic", templateExampleHost)
}

// runSingle executes a single-target collection and writes it to a
// local file: cisco-apic-<timestamp>-<hostname>.json.
func runSingle(cfg *Config) error {
	target := cfg.Targets[0]
	logger := defaultLogger()

	reqCtx, stop := rootContext()
	defer stop()

	producer := &Producer{target: target, cfg: cfg, ctx: reqCtx}
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
	filename := fmt.Sprintf("cisco-apic-%s-%s.json", cfg.Timestamp, name)

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
// hierarchical output path OutputDir/Datacenter/Floor/Room/Rack/<file>.json
// (see run.OutputPath). A single target's failure is logged and
// skipped; only a fully failed run (succeeded == 0) is fatal.
func runBatch(cfg *Config) error {
	logger := defaultLogger()

	if err := os.MkdirAll(cfg.OutputDir, 0755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	var succeeded, failed int

	for _, target := range cfg.Targets {
		log := logger.With("target", target.Host, "hostname", target.Hostname)
		log.Info("collecting")

		doc, err := collectOne(cfg, target, log)
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

// collectOne runs a single target's collection under its own
// signal-cancellable, time-bounded context. Isolating it in a function
// keeps the deferred context stop per iteration rather than piling up
// for the whole batch.
func collectOne(cfg *Config, target run.TargetConfig, log *slog.Logger) (*sdk.Document, error) {
	reqCtx, stop := rootContext()
	defer stop()

	producer := &Producer{target: target, cfg: cfg, ctx: reqCtx}
	ctx := newSDKContext(cfg)
	ctx.Logger = log
	return producer.Collect(ctx)
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

// printHelp prints the producer's usage. The flag list is rendered from
// the real flag set by FlagsUsage (flags.go), never hand-typed here, so
// it cannot drift from what ParseFlags actually accepts. Only the
// narrative sections below are prose.
func printHelp() {
	fmt.Print(`osirisjson-producer cisco apic - OSIRIS JSON producer for Cisco ACI/APIC

Usage:
  osirisjson-producer cisco apic [flags]

Authentication: there is deliberately no -p/--password flag - a CLI flag
value is visible to any local user (via ps) and is written to shell
history. host/username/password are each resolved in this order: their
own -h/-u flag, then --secrets-file, then (for whichever is still
missing, single mode only) an interactive prompt on the controlling
terminal. Nothing entered at the prompt is written to disk.

Flags:
`)
	fmt.Print(FlagsUsage())
	fmt.Print(`
--secrets-file accepts two shapes, each generated as its own file by
"apic template --generate" so you never have to write the JSON by hand:
  cisco-apic-secrets.json           a single login: {"host","username","password"}
  cisco-apic-secrets-multihost.json different logins per target in a batch,
                                    matched by exact host/IP or CIDR, with a
                                    "default" fallback for anything unmatched
The file must be a regular file (not a symlink) readable only by its
owner (e.g. chmod 0600); a looser file is rejected.

Discovery and failure behavior:
  Every class is read with paged GET /api/class/<class>.json queries.
  The producer never writes to the APIC. fvCEp and fvIp (endpoints),
  vzEntry (filter entries: protocols and ports) and vpcDom (per-leaf vPC
  state) are read only with --purpose audit flag.
  fabricNode and topSystem (fabric inventory) and the tenant, VRF,
  bridge-domain, subnet, EPG and L3Out classes are required: if one
  fails, returns a malformed response or an object without a dn, or the
  fabric inventory is empty (the account cannot see the fabric), no
  document is written and the run exits non-zero.
  Every other class is optional: a failure is logged and the document
  is written as partial. The per-class coverage record on the controller
  resources and metadata.scope.description name what is missing.
  Policy is configured intent only: application profiles, contracts,
  subjects, filters and which EPGs provide or consume each contract.
  It never states that traffic is permitted or flows. External EPGs
  provide and consume contracts like EPGs. vzAny, taboo contracts,
  endpoint security groups, management EPGs, out-of-band contracts,
  contract inheritance, service graphs and direction-specific subject
  terms are not modeled (policy_scope on the controller resources lists
  them; coverage counts them as out_of_scope).
  Logical paths (fabricPathEp) are resources with one member edge per
  leg: port, port-channel, vPC leg port-channel or, when no interface is
  named, the leaf node. A vPC is never given a single owner. EPG static
  attachments, endpoints and L3Out routed interfaces point to the path;
  an attachment to a path the APIC does not report is counted as
  unresolved, never invented.
  L3Out is configured intent: external EPGs, their policy prefixes and
  the border leaves and routed paths an L3Out is attached to. No
  installed route or routing session is implied (l3out_scope).
  External neighbours are identified by the LLDP chassis id, otherwise
  the CDP device id, never by a shortened name; legacy_ids lists the
  identifiers used before behavior version 0.4.0.
  Redirects are never followed. Errors and logs carry the class, HTTP
  status and APIC error code only, never response content.
  A secret-shaped value anywhere in the document aborts the run
  (--safe-failure-mode fail-closed). log-and-redact is not supported
  and is rejected.
  --include-raw-body (with --purpose audit only) attaches the full
  attribute set of every collected object, redacted, under
  extensions["osiris.cisco"].raw on the resource or group that owns it
  (longest DN prefix); objects without an owner go to raw_unowned and
  withheld objects are counted in raw_omitted on the controllers.

Batch mode (-s/--source): a CSV file with columns
datacenter,floor,room,rack,hostname,management_ip,port
datacenter/floor/room/rack are optional; when present they build the
output directory structure:
<output>/Datacenter/Floor/Room/Rack/Hostname.json.

Other commands:
  template --generate	Write cisco-apic-template.csv and both --secrets-file skeletons

Output:
  single mode saves to: cisco-apic-<timestamp>-<hostname>.json (0600 permissions)
  batch mode saves to:  <output>/Datacenter/Floor/Room/Rack/Hostname.json (0600 permissions)

Examples:
  osirisjson-producer cisco apic -h 192.0.2.1 -u username
  osirisjson-producer cisco apic -h 192.0.2.1 -u username --purpose audit
  osirisjson-producer cisco apic -s datacenter.csv -o ./output -u username
  osirisjson-producer cisco apic template --generate
`)
}
