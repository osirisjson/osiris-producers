// Package iosxe implements the Cisco IOS-XE producer for OSIRIS JSON.
// It queries the device's YANG models over NETCONF (SSH) or RESTCONF
// (HTTPS), selected by --transport, to discover device topology and
// generates an OSIRIS JSON document with resources, groups and
// connections. Both transports return the same XML and emit the same
// document.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification
package iosxe

import (
	"errors"
	"fmt"
	"log/slog"

	"go.osirisjson.org/producers/osiris/network/cisco/run"
	"go.osirisjson.org/producers/pkg/sdk"
)

const (
	generatorName    = "osirisjson-producer-cisco-iosxe"
	generatorVersion = "0.2.0"
	generatorURL     = "https://docs.osirisjson.org/osiris-producers/network/cisco"
)

type Producer struct {
	target run.TargetConfig
	cfg    *Config
	client *Client     // NETCONF client, injectable for testing.
	rest   *restClient // RESTCONF client, injectable for testing.
}

// Transports accepted by --transport.
const (
	transportAuto     = "auto"
	transportNETCONF  = "netconf"
	transportRESTCONF = "restconf"
)

// connect returns the querier for the configured transportAuto tries
// NETCONF and falls back to RESTCONF only when the NETCONF port cannot
// be reached (TCP connect timeout or refusal); an authentication,
// host-key, or protocol failure never falls back, so a changed SSH key
// stops the run instead of switching protocols.
// The returned name is the transport actually used.
func (p *Producer) connect(logger *slog.Logger) (querier, string, error) {
	transport := transportAuto
	if p.cfg != nil && p.cfg.Transport != "" {
		transport = p.cfg.Transport
	}
	insecure := p.cfg != nil && p.cfg.InsecureTLS

	if p.rest != nil {
		return p.rest, transportRESTCONF, nil
	}
	if p.client != nil {
		return p.client, transportNETCONF, nil
	}

	restconf := func(target run.TargetConfig) (querier, string, error) {
		if insecure {
			logger.Warn("TLS certificate verification disabled for RESTCONF connection (--insecure)", "host", target.Host)
		}
		return newRESTClient(target, insecure, logger), transportRESTCONF, nil
	}
	if transport == transportRESTCONF {
		return restconf(p.target)
	}

	client := NewClient(p.target, insecure, logger)
	err := client.Connect(p.target.Username, p.target.Password)
	if err == nil {
		return client, transportNETCONF, nil
	}
	if transport == transportAuto && errors.Is(err, errNETCONFUnreachable) {
		logger.Warn("NETCONF port unreachable, falling back to RESTCONF (--transport auto)",
			"host", p.target.Host, "err", err)
		// An explicit port was meant for NETCONF; RESTCONF uses its own
		// default.
		target := p.target
		target.Port = 0
		return restconf(target)
	}
	return nil, "", fmt.Errorf("IOS-XE NETCONF connection failed: %w", err)
}

// Collect queries the IOS-XE device over NETCONF or RESTCONF and builds
// an OSIRIS JSON document. Each query is one discovery operation
// recorded in the coverage record.
// The native system config and interfaces are essential: their failure
// aborts the run. The hardware inventory supplies the device key every
// ID derives from, so a failed hardware query also aborts, while an
// empty or unsupported one falls back to the target address.
// Every other operation is optional: its failure is logged, recorded
// and leaves the document partial.
func (p *Producer) Collect(ctx *sdk.Context) (*sdk.Document, error) {
	src, transport, err := p.connect(ctx.Logger)
	if err != nil {
		return nil, err
	}
	if p.client == nil && p.rest == nil {
		defer src.Close()
	}

	ctx.Logger.Info("collecting IOS-XE device data", "host", p.target.Host, "transport", transport)

	d := &discovery{src: src, logger: ctx.Logger, cov: &coverageRecorder{}}
	audit := ctx.Config != nil && ctx.Config.Purpose == "audit"
	if audit && ctx.Config.IncludeRawBody {
		d.raw = &rawCollector{}
	}

	native, err := fetch[nativeSystem](d, qNativeSystem, essential)
	if err != nil {
		return nil, err
	}
	ifaces, err := fetch[ietfInterfaces](d, qInterfaces, essential)
	if err != nil {
		return nil, err
	}
	ifOper, _ := fetch[interfacesOper](d, qInterfacesOper, optional)
	hw, err := fetch[hwInventory](d, qHardware, identity)
	if err != nil {
		return nil, err
	}
	boot, _ := fetch[nativeBoot](d, qBoot, optional)
	nativeIfs, _ := fetch[nativeInterfaces](d, qNativeInterfaces, optional)
	lldp, _ := fetch[lldpEntries](d, qLLDP, optional)
	cdp, _ := fetch[cdpNeighborDetails](d, qCDP, optional)
	vrfs, _ := fetch[nativeVRFList](d, qVRFs, optional)
	vlans, _ := fetch[vlansOper](d, qVLANs, optional)
	routes, _ := fetch[nativeRoutes](d, qStaticRoutes, optional)
	xcvrs, _ := fetch[transceiverOper](d, qTransceivers, optional)

	deviceResource, deviceID, deviceKey := TransformDevice(p.target.Host, p.target.Hostname, native, hw)
	if deviceKey == p.target.Host {
		ctx.Logger.Warn("IOS-XE device reported no chassis serial; resource IDs fall back to the target address and will change if it does",
			"host", p.target.Host)
	}
	ensureCiscoExtension(&deviceResource.Extensions)
	cisco := deviceResource.Extensions[extensionNamespace].(map[string]any)
	setExt := func(key string, v map[string]any) {
		if len(v) > 0 {
			cisco[key] = v
		}
	}
	setList := func(key string, v []map[string]any) {
		if len(v) > 0 {
			cisco[key] = v
		}
	}

	if images := boot.images(); len(images) > 0 {
		cisco["boot_images"] = images
	}
	setList("inventory", TransformInventory(hw, audit))

	ifSet := TransformInterfaces(deviceKey, deviceResource.Type, ifaces, ifOper)
	ifResources, ifNameToID := ifSet.Resources, ifSet.NameToID
	EnrichEncapsulation(nativeIfs, ifResources, ifNameToID)
	setList("transceivers", EnrichTransceivers(xcvrs, ifResources, ifNameToID, audit))
	connections := TransformDeviceContainment(deviceID, deviceResource.Name, ifSet)
	self := newSelfIdentity(native.Hostname, p.target.Host, ifSet)
	neighborConns, stubs := TransformNeighbors(deviceID, deviceResource.Name, self, lldp, cdp, ifNameToID)
	ApplyLinkMedium(neighborConns, ifResources)
	connections = append(connections, neighborConns...)

	vlanResources, vlanConns := TransformVLANs(deviceKey, vlans, nativeIfs, ifNameToID)
	connections = append(connections, vlanConns...)
	vrfGroups, vrfNameToGroupID := TransformVRFs(deviceKey, vrfs)
	ifVRF := interfaceVRFs(ifOper, nativeIfs)
	WireInterfacesToVRFs(ifVRF, ifNameToID, vrfGroups, vrfNameToGroupID)
	subnetResources, subnetConns := TransformSubnets(deviceKey, ifSet, ifVRF, vrfGroups, vrfNameToGroupID)
	connections = append(connections, subnetConns...)

	setList("static_routes", TransformStaticRoutes(routes))

	// Audit purpose flag: routing sessions and the security posture.
	// Each operation is independent; one failing leaves others intact.
	var securityResources []sdk.Resource
	if audit {
		bgp, _ := fetch[bgpStateData](d, qBGP, optional)
		bgpConns, bgpStubs := TransformBGPNeighbors(deviceID, bgp)
		connections = append(connections, bgpConns...)
		stubs = append(stubs, bgpStubs...)

		ospf, _ := fetch[ospfOperData](d, qOSPF, optional)
		setList("ospf_processes", TransformOSPFProcesses(ospf))
		ospfConns, ospfStubs := TransformOSPFNeighbors(deviceID, ospf, ifNameToID)
		connections = append(connections, ospfConns...)
		stubs = append(stubs, ospfStubs...)

		aaa, _ := fetch[nativeAAA](d, qAAA, optional)
		setExt("aaa", TransformAAA(aaa))
		accounts, _ := fetch[nativeAccounts](d, qAccounts, optional)
		users, enable := TransformAccounts(accounts)
		setList("local_accounts", users)
		setExt("enable_mode", enable)
		mgmt, _ := fetch[nativeManagement](d, qManagement, optional)
		setExt("management_services", TransformManagement(mgmt))
		dns, _ := fetch[nativeDNS](d, qDNS, optional)
		setExt("dns", TransformDNS(dns))
		snmp, _ := fetch[nativeSNMP](d, qSNMP, optional)
		setExt("snmp", TransformSNMP(snmp))
		logging, _ := fetch[nativeLogging](d, qLogging, optional)
		setExt("logging", TransformLogging(logging))
		ntp, _ := fetch[nativeNTP](d, qNTP, optional)
		setExt("ntp", TransformNTP(ntp))
		acls, _ := fetch[nativeACLs](d, qACLs, optional)
		securityResources = append(securityResources, TransformACLs(deviceKey, acls, nativeIfs, mgmt)...)
		crypto, _ := fetch[nativeCrypto](d, qCrypto, optional)
		setExt("crypto", TransformCrypto(crypto))
		cp, _ := fetch[nativeControlPlane](d, qControlPlane, optional)
		setExt("control_plane_policy", TransformControlPlane(cp))
		certs, _ := fetch[pkiOper](d, qCertificates, optional)
		securityResources = append(securityResources, TransformCertificates(deviceKey, certs)...)

		connections = append(connections, TransformDeviceContainment(deviceID, deviceResource.Name, interfaceSet{Resources: securityResources})...)
	} else {
		for _, q := range auditOnlyQueries {
			d.cov.skipped(q.op, "audit purpose only")
		}
	}

	// The coverage record is attached regardless of purpose, so a
	// partial document always states what it is missing.
	cisco["coverage"] = d.cov.asSlice()
	d.raw.apply(cisco)

	scopePurpose := ""
	if ctx.Config != nil {
		scopePurpose = ctx.Config.Purpose
	}
	builder := sdk.NewDocumentBuilder(ctx).
		WithGenerator(generatorName, generatorVersion, generatorURL).
		WithScope(sdk.Scope{
			Name:        deviceResource.Name,
			Description: d.cov.summary(deviceResource.Name),
			Purpose:     scopePurpose,
			Providers:   []string{providerName},
		})

	builder.AddResource(deviceResource)
	for _, r := range ifResources {
		builder.AddResource(r)
	}
	for _, r := range vlanResources {
		builder.AddResource(r)
	}
	for _, r := range subnetResources {
		builder.AddResource(r)
	}
	for _, r := range securityResources {
		builder.AddResource(r)
	}
	for _, r := range stubs {
		builder.AddResource(r)
	}
	for _, c := range connections {
		builder.AddConnection(c)
	}
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

// criticality decides what failed discovery operation does to the run.
type criticality int

const (
	// optional failures are recorded and leave the document partial.
	optional criticality = iota
	// identity operations supply the device key: a failed query aborts
	// the run, because falling back would silently change every emitted
	// ID, but an empty or unsupported reply falls back to the target
	// address with a warning.
	identity
	// essential failures of any kind, including unsupported, abort.
	essential
)

// discovery carries what every fetch needs: the transport, the logger,
// and the coverage record being built.
type discovery struct {
	src    querier
	logger *slog.Logger
	cov    *coverageRecorder
	raw    *rawCollector // nil unless --include-raw-body is in effect.
}

// auditOnlyQueries are the operations Collect issues only at audit
// purpose, in issue order.
var auditOnlyQueries = []query{
	qBGP, qOSPF, qAAA, qAccounts, qManagement, qDNS, qSNMP, qLogging,
	qNTP, qACLs, qCrypto, qControlPlane, qCertificates,
}

// fetch runs one discovery operation: it issues q, decodes the reply
// into T and records the outcome in d's coverage. An empty reply yields
// the zero value and a nil error. Whether any other failure is returned
// or logged and swallowed (yielding the zero value) depends on crit.
func fetch[T model](d *discovery, q query, crit criticality) (T, error) {
	var zero T
	op := q.op
	data, err := d.src.query(q)
	if err == nil {
		d.raw.add(q, data)
		var v T
		v, err = decodeModel[T](data)
		if err == nil || errors.Is(err, errEmptyModel) {
			d.cov.record(op, v.rows(), err)
			return v, nil
		}
	}

	o := d.cov.record(op, 0, err)
	if crit == essential || (crit == identity && o.Status == statusFailed) {
		return zero, fmt.Errorf("IOS-XE %s query failed: %w", op, err)
	}
	if o.Status == statusUnsupported {
		d.logger.Info("IOS-XE model not supported by the device, continuing without it", "operation", op)
	} else {
		d.logger.Warn("IOS-XE query failed, continuing without it", "operation", op, "category", o.Category, "err", err)
	}
	return zero, nil
}
