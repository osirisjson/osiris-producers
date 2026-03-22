# Changelog - Cisco Producer

All notable changes to the Cisco producer packages (`osiris/network/cisco/...`) are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

---

## [Unreleased]

### Added - 2026-03-22
- **apic**: APIC fault extensions (`osiris.cisco`)
  - `Fault` type: curated fault representation (code, severity, cause, description, timestamps, lifecycle, domain, subject)
  - `TransformFaults`: groups `faultInst` by DN prefix, filters out cleared faults (snapshot = "what's wrong now")
  - `WireFaultsToNodes`: attaches faults to node resources via `topology/pod-N/node-N` DN prefix match
  - `WireFaultsToTenants`: attaches faults to tenant groups via `uni/tn-NAME` DN prefix match
  - ACI-specific node metadata in `extensions["osiris.cisco"]`: `fabric_mac`, `control_plane_mtu`, `last_reboot_time`, `fabric_id` (from topSystem)
  - `faultInst` always queried (audit-critical, not detail-level gated)
  - 10 new tests (fault transform, DN grouping, cleared filtering, fault wiring, extension merging, integration)
- **apic**: ACI/APIC fabric topology producer (`osiris.cisco`)
  - `client.go`: APIC REST client with `Login` (aaaLogin), `QueryClass` (paginated class queries), automatic cookie-jar session handling
  - `transform.go`: pure APIC->OSIRIS mapping functions (no I/O)
    - `TransformNodes`: fabricNode + topSystem + firmwareRunning merge by DN prefix -> `network.controller`, `network.switch.spine`, `network.switch.leaf`
    - `TransformTenants`: fvTenant -> `logical.tenant` groups with DN->ID mapping
    - `TransformVRFs`: fvCtx -> `logical.vrf` groups with DN->ID mapping
    - `TransformBridgeDomains`: fvBD -> `network.domain.bridge` resources with DN->ID mapping
    - `TransformSubnets`: fvSubnet -> `network.subnet` resources
    - `TransformEPGs`: fvAEPg -> `logical.epg` groups with DN->ID mapping
    - `TransformEndpoints`: fvCEp -> `network.endpoint` resources (detailed mode only)
    - `TransformL3Outs`: l3extOut -> `network.l3out` resources (skips `__ui_svi_dummy_id_*`)
    - Relationship wiring: `WireBDsToTenants`, `WireSubnetsToTenants`, `WireVRFsToTenants`, `WireEPGsToTenants`, `WireL3OutsToTenants`, `WireEndpointsToEPGs`
  - `apic.go`: `Producer.Collect()` orchestrates query->transform->wire->Build() pipeline; `NewFactory()` for cisco.go wiring
  - Full ACI containment hierarchy: tenants own BDs/subnets/L3Outs (members) and VRFs/EPGs (children); EPGs own endpoints (detailed mode)
  - Detail level support: minimal (nodes, tenants, VRFs, BDs, subnets, EPGs, L3Outs) vs detailed (adds endpoints with EPG membership)
  - Deterministic resource IDs via `Hash16` on APIC DN-based canonical keys
  - 30 tests (client: 7, transform: 16, integration: 4, wiring: 6 included in transform)
- **cisco.go**: wired `apic.NewFactory()` into subProducers (APIC now shows as "ready")
- **iosxe**: Cisco IOS-XE producer via NETCONF/YANG over SSH
  - `client.go`: NETCONF 1.0 client (RFC 4741) over SSH subsystem using `golang.org/x/crypto/ssh`; supports `<get>`, `<get-config>`, and `<close-session>` RPCs with message framing
  - `transform.go`: pure XML->OSIRIS mapping functions (no I/O)
    - `TransformDevice`: native config + hardware data -> `network.router` or `network.switch` resource with model, serial, version
    - `TransformInterfaces`: ietf-interfaces -> `network.interface` resources with admin/oper status, speed, MAC, IPv4
    - `TransformInventory`: device-hardware-data -> hardware inventory list in `extensions["osiris.cisco"]`
    - `TransformCDPNeighbors`: CDP neighbor details -> connections + stub resources for remote devices
    - `TransformVRFs`: VRF definitions -> `logical.vrf` groups
    - `TransformBGPNeighbors`: BGP state data -> BGP neighbor list in extensions (detailed mode)
    - `TransformOSPF`: OSPF operational data -> OSPF process list in extensions (detailed mode)
    - `TransformCPUMemory`: CPU utilization + memory statistics -> extensions (detailed mode)
    - `WireInterfacesToVRFs`: associates interfaces with VRF groups via forwarding config
    - `EnrichInterfaceCounters`: adds in/out octets, errors, discards to interface resources (detailed mode)
  - `iosxe.go`: `Producer.Collect()` orchestrates 5 minimal NETCONF RPCs + 4 detailed RPCs; `NewFactory()` for cisco.go wiring
  - Detail level support: minimal (device, interfaces, inventory, CDP neighbors, VRFs) vs detailed (adds BGP, OSPF, CPU/memory, interface counters)
  - Deterministic resource IDs via `Hash16` on hostname-based canonical keys
  - 35 tests (client: 10, transform: 17, integration: 8)
- **nxos**: Cisco NX-OS producer via NX-API CLI (code complete, disabled in dispatcher)
  - `client.go`: NX-API CLI client with JSON-RPC payloads; `Login` (aaaLogin), `Show` (single command), `ShowMulti` (batched commands)
  - `transform.go`: pure NX-API JSON->OSIRIS mapping functions (no I/O)
    - `TransformDevice`: show version -> `network.switch` resource with model, serial, NX-OS version, uptime
    - `TransformInterfaces`: show interface brief -> `network.interface` resources with state, speed, type
    - `TransformLLDPNeighbors`: LLDP neighbor detail -> connections + stub resources for remote devices
    - `TransformVLANs`: show vlan brief -> `network.vlan` groups
    - `TransformVRFs`: show vrf all detail -> `logical.vrf` groups
    - `TransformVPC`: show vpc brief -> `network.vpc` group with role, peer-link, keepalive status
    - `TransformInventory`: show inventory -> hardware inventory list in `extensions["osiris.cisco"]`
    - `TransformSystemResources`: show system resources -> CPU/memory in extensions (detailed mode)
    - `TransformEnvironment`: show environment -> power supply, fan, temperature in extensions (detailed mode)
    - `WireInterfacesToVLANs`, `WireInterfacesToVRFs`, `WirePortChannelsToVPC`: relationship wiring
    - `EnrichInterfaceDetails`: adds counters, MTU, bandwidth to interface resources (detailed mode)
  - `nxos.go`: `Producer.Collect()` orchestrates 2 NX-API batches (6+2 commands) + 3 detailed commands; `NewFactory()` ready but not wired in dispatcher
  - Detail level support: minimal (device, interfaces, VLANs, VRFs, vPC, LLDP, inventory) vs detailed (adds interface counters, system resources, environment)
  - Deterministic resource IDs via `Hash16` on hostname-based canonical keys
  - 42 tests (client: 11, transform: 23, integration: 8)

### Added - 2026-03-04
- **run**: shared transport layer for all Cisco producers
  - `config.go`: `TargetConfig` with datacenter hierarchy (DC/Floor/Room/Zone), `Type`, `Owner`, `Notes`; `RunConfig`; `ParseHostPort`, `ResolveAddr`, `OutputPath`
  - `flags.go`: `ParseFlags` with stdlib `flag.FlagSet`, short+long aliases, single/batch mode detection, mutual exclusivity validation
  - `httpclient.go`: `NewHTTPClient` with TLS config and cookie jar
  - `tty.go`: `PromptPassword` via `/dev/tty` with echo-disabled input (`golang.org/x/term`)
  - `batch.go`: datacenter-aware CSV (dc,floor,room,zone,hostname,type,ip,port,owner,notes), `FactoryRegistry` for multi-type dispatch, `RunBatch` with hierarchical output (DC/Floor/Room/Zone/Hostname.json), per-target failure isolation
  - Owner metadata: `self` (own device), `isp` (ISP-managed), `colo` (colocation) - human-only, does not affect OSIRIS documents
  - 30 tests for shared layer (config, flags, CSV parsing, batch orchestration)
- **cisco.go**: Cisco vendor entry point
  - Sub-producer dispatch: `apic`, `nxos`, `iosxe` subcommands
  - `template --generate [apic|nxos|iosxe]` for CSV batch templates with precompiled 3-row examples
  - Single mode (stdout) and batch mode (hierarchical output directory) execution paths
