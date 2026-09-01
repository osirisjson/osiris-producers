// Package apic implements the Cisco ACI/APIC producer for OSIRIS JSON.
// Queries the APIC REST API to discover ACI fabric topology and
// generates an OSIRIS JSON document with resources, groups
// and connections.
//
// This file holds the whole CLI surface (Run, the single/batch runners,
// --help, template generation) alongside the Producer type and its
// Collect, the same one-file shape. The per-domain APIC ->
// OSIRIS mapping lives in the transform_<area>.go files; relationship
// wiring is in wire.go, the discovery plan in discover.go, and the
// typed response envelope in client.go.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification
package apic

import (
	"context"
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
		objs = dedupeByDN(objs, dc.name, ctx.Logger)
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
	tenantGroups, tenantDNToID := TransformTenants(tenantAttrs)
	vrfGroups, vrfDNToID := TransformVRFs(vrfAttrs)
	bdResources, bdDNToID := TransformBridgeDomains(bdAttrs)
	subnetResources := TransformSubnets(subnetAttrs)
	epgGroups, epgDNToID := TransformEPGs(epgAttrs)
	l3outResources, l3outDNToID := TransformL3Outs(l3outAttrs)

	// Physical fabric: switch ports (bounded to topology-participating
	// ports in documentation mode, every port under --purpose audit)
	// and port-channel aggregate interfaces.
	numToDN := nodeNumIndex(nodes)
	nodeIDs := resourceIDSet(nodeResources)
	var portKeep map[string]bool
	if !audit {
		portKeep = topologyPortDNs(fabricLinkAttrs, lldpAttrs, cdpAttrs, pathAttAttrs, ethpmAttrs, numToDN)
	}
	portResources, portDNToID := TransformSwitchPorts(l1Attrs, ethpmAttrs, portKeep)
	pcResources, pcDNToID := TransformPortChannels(pcAttrs)

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

	// Wire physical topology: node contains port, port-channel contains
	// member, bridge-domain contains subnet, and the merged
	// fabricLink/LLDP/CDP adjacency graph.
	ctx.Logger.Info("wiring physical fabric topology (ports, links, neighbours)")
	extNeighbourResources, adjacencyConns := WireFabricAdjacencies(fabricLinkAttrs, lldpAttrs, cdpAttrs, nodes, nodeResources)
	var topoConns []sdk.Connection
	topoConns = append(topoConns, WireNodePorts(portDNToID, nodeIDs)...)
	topoConns = append(topoConns, WireNodePorts(pcDNToID, nodeIDs)...)
	topoConns = append(topoConns, WirePortChannelMembers(ethpmAttrs, pcDNToID, portDNToID)...)
	topoConns = append(topoConns, WireBDSubnets(subnetAttrs, bdDNToID)...)
	topoConns = append(topoConns, adjacencyConns...)

	// Audit mode: wire each endpoint to the port it was learned on and
	// each EPG group to its attached ports/port-channels.
	if audit {
		topoConns = append(topoConns, WireEndpointPorts(endpointAttrs, portDNToID, pcDNToID, numToDN)...)
		WireEPGPathAttachments(pathAttAttrs, epgDNToID, epgGroups, portDNToID, pcDNToID, numToDN)
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
	for _, r := range portResources {
		builder.AddResource(r)
	}
	for _, r := range pcResources {
		builder.AddResource(r)
	}
	for _, r := range extNeighbourResources {
		builder.AddResource(r)
	}

	if len(endpointAttrs) > 0 {
		for _, r := range TransformEndpoints(endpointAttrs, endpointIPAttrs) {
			builder.AddResource(r)
		}
	}

	for _, c := range topoConns {
		builder.AddConnection(c)
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
	fmt.Print(`osirisjson-producer cisco apic - Cisco ACI/APIC fabric topology OSIRIS JSON producer

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
