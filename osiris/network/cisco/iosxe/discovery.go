// discovery.go - Discovery operations of the Cisco IOS-XE OSIRIS JSON
// Producer and the transport-neutral way they are issued.
//
// Each operation is described once, in queries: its name (the coverage
// operation name), whether it reads configuration or operational state,
// the NETCONF subtree filter and the RESTCONF resources that return
// same content. Most operations are one RESTCONF container; operations
// whose NETCONF filter selects several children of a container (native
// version and hostname, static IPv4 and IPv6 routes) map to several
// resources whose bodies are wrapped back under the common parent, so
// either transport yields the same XML payload for the same decoder.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package iosxe

// query is one discovery operation.
type query struct {
	op     string // coverage operation name.
	config bool   // configuration (get-config, ?content=config) or state.
	filter string // NETCONF subtree filter.
	rest   restQuery
}

// restQuery is the RESTCONF form of a query: one or more data resources
// whose bodies, each wrapped in open/close, are joined under the
// open/close root wrapper.
type restQuery struct {
	open, close string
	parts       []restPart
}

// restPart is one RESTCONF data resource path, relative to
// /restconf/data/ and the wrapper its body is placed in.
type restPart struct {
	path        string
	open, close string
}

// querier issues discovery operations over one transport.
type querier interface {
	query(q query) ([]byte, error)
	Close() error
}

// query issues q as a NETCONF <get> or <get-config> with its subtree
// filter.
func (c *Client) query(q query) ([]byte, error) {
	if q.config {
		return c.GetConfig(q.filter)
	}
	return c.Get(q.filter)
}

// single is a restQuery for one top-level container returned as is.
func single(path string) restQuery {
	return restQuery{parts: []restPart{{path: path}}}
}

// under is a restQuery for children of the container root (element name
// rootElem in namespace ns), joined back under that root.
func under(rootElem, ns string, parts ...restPart) restQuery {
	return restQuery{open: `<` + rootElem + ` xmlns="` + ns + `">`, close: `</` + rootElem + `>`, parts: parts}
}

const nativePath = "Cisco-IOS-XE-native:native/"

// Discovery operations, in the order Collect issues them.
var (
	qNativeSystem = query{op: "native-system", config: true,
		filter: `<native xmlns="` + nsNative + `"><version/><hostname/></native>`,
		rest:   under("native", nsNative, restPart{path: nativePath + "version"}, restPart{path: nativePath + "hostname"})}
	qInterfaces = query{op: "interfaces",
		filter: `<interfaces xmlns="` + nsIETFInterface + `"/>`,
		rest:   single("ietf-interfaces:interfaces")}
	qInterfacesOper = query{op: "interfaces-oper",
		filter: `<interfaces xmlns="` + nsInterfaceOper + `"/>`,
		rest:   single("Cisco-IOS-XE-interfaces-oper:interfaces")}
	qHardware = query{op: "hardware-inventory",
		filter: `<device-hardware-data xmlns="` + nsHardwareOper + `"/>`,
		rest:   single("Cisco-IOS-XE-device-hardware-oper:device-hardware-data")}
	qBoot = query{op: "boot-config", config: true,
		filter: `<native xmlns="` + nsNative + `"><boot/></native>`,
		rest:   under("native", nsNative, restPart{path: nativePath + "boot"})}
	qNativeInterfaces = query{op: "native-interfaces", config: true,
		filter: `<native xmlns="` + nsNative + `"><interface/></native>`,
		rest:   under("native", nsNative, restPart{path: nativePath + "interface"})}
	qLLDP = query{op: "lldp-neighbors",
		filter: `<lldp-entries xmlns="` + nsLLDPOper + `"/>`,
		rest:   single("Cisco-IOS-XE-lldp-oper:lldp-entries")}
	qCDP = query{op: "cdp-neighbors",
		filter: `<cdp-neighbor-details xmlns="` + nsCDPOper + `"/>`,
		rest:   single("Cisco-IOS-XE-cdp-oper:cdp-neighbor-details")}
	qVRFs = query{op: "vrf-definitions", config: true,
		filter: `<native xmlns="` + nsNative + `"><vrf/></native>`,
		rest:   under("native", nsNative, restPart{path: nativePath + "vrf"})}
	qVLANs = query{op: "vlans",
		filter: `<vlans xmlns="` + nsVLANOper + `"/>`,
		rest:   single("Cisco-IOS-XE-vlan-oper:vlans")}
	qStaticRoutes = query{op: "static-routes", config: true,
		filter: `<native xmlns="` + nsNative + `"><ip><route/></ip><ipv6><route/></ipv6></native>`,
		rest: under("native", nsNative,
			restPart{path: nativePath + "ip/route", open: "<ip>", close: "</ip>"},
			restPart{path: nativePath + "ipv6/route", open: "<ipv6>", close: "</ipv6>"})}
	qBGP = query{op: "bgp",
		filter: `<bgp-state-data xmlns="` + nsBGPOper + `"><neighbors/></bgp-state-data>`,
		rest:   under("bgp-state-data", nsBGPOper, restPart{path: "Cisco-IOS-XE-bgp-oper:bgp-state-data/neighbors"})}
	qOSPF = query{op: "ospf",
		filter: `<ospf-oper-data xmlns="` + nsOSPFOper + `"><ospf-state/></ospf-oper-data>`,
		rest:   under("ospf-oper-data", nsOSPFOper, restPart{path: "Cisco-IOS-XE-ospf-oper:ospf-oper-data/ospf-state"})}
	qTransceivers = query{op: "transceivers",
		filter: `<transceiver-oper-data xmlns="` + nsTransceiverOper + `"/>`,
		rest:   single("Cisco-IOS-XE-transceiver-oper:transceiver-oper-data")}

	// Audit-purpose security posture. Every operation below reads
	// configuration that may hold credential material in leaves this
	// producer never decodes (see dto_security.go).
	qAAA = query{op: "aaa", config: true,
		filter: `<native xmlns="` + nsNative + `"><aaa/><tacacs/><radius/></native>`,
		rest: under("native", nsNative,
			restPart{path: nativePath + "aaa"},
			restPart{path: nativePath + "tacacs"},
			restPart{path: nativePath + "radius"})}
	qAccounts = query{op: "local-accounts", config: true,
		filter: `<native xmlns="` + nsNative + `"><username/><enable/></native>`,
		rest: under("native", nsNative,
			restPart{path: nativePath + "username"},
			restPart{path: nativePath + "enable"})}
	qManagement = query{op: "management-services", config: true,
		filter: `<native xmlns="` + nsNative + `"><ip><http xmlns="` + nsHTTP + `"/><ssh/><scp/></ip><line/><service/></native>`,
		rest: under("native", nsNative,
			restPart{path: nativePath + "ip/Cisco-IOS-XE-http:http", open: "<ip>", close: "</ip>"},
			restPart{path: nativePath + "ip/ssh", open: "<ip>", close: "</ip>"},
			restPart{path: nativePath + "ip/scp", open: "<ip>", close: "</ip>"},
			restPart{path: nativePath + "line"},
			restPart{path: nativePath + "service"})}
	qDNS = query{op: "dns", config: true,
		filter: `<native xmlns="` + nsNative + `"><ip><domain/><name-server/></ip></native>`,
		rest: under("native", nsNative,
			restPart{path: nativePath + "ip/domain", open: "<ip>", close: "</ip>"},
			restPart{path: nativePath + "ip/name-server", open: "<ip>", close: "</ip>"})}
	qSNMP = query{op: "snmp", config: true,
		filter: `<native xmlns="` + nsNative + `"><snmp-server/></native>`,
		rest:   under("native", nsNative, restPart{path: nativePath + "snmp-server"})}
	qLogging = query{op: "logging", config: true,
		filter: `<native xmlns="` + nsNative + `"><logging/></native>`,
		rest:   under("native", nsNative, restPart{path: nativePath + "logging"})}
	qNTP = query{op: "ntp", config: true,
		filter: `<native xmlns="` + nsNative + `"><ntp/></native>`,
		rest:   under("native", nsNative, restPart{path: nativePath + "ntp"})}
	qACLs = query{op: "acls", config: true,
		filter: `<native xmlns="` + nsNative + `"><ip><access-list/></ip><ipv6><access-list/></ipv6></native>`,
		rest: under("native", nsNative,
			restPart{path: nativePath + "ip/access-list", open: "<ip>", close: "</ip>"},
			restPart{path: nativePath + "ipv6/access-list", open: "<ipv6>", close: "</ipv6>"})}
	qCrypto = query{op: "crypto", config: true,
		filter: `<native xmlns="` + nsNative + `"><crypto><pki xmlns="` + nsCrypto + `"><trustpoint/></pki><ikev2 xmlns="` + nsCrypto + `"><proposal/></ikev2><ipsec xmlns="` + nsCrypto + `"><transform-set/></ipsec></crypto></native>`,
		rest: under("native", nsNative,
			restPart{path: nativePath + "crypto/Cisco-IOS-XE-crypto:pki", open: "<crypto>", close: "</crypto>"},
			restPart{path: nativePath + "crypto/Cisco-IOS-XE-crypto:ikev2", open: "<crypto>", close: "</crypto>"},
			restPart{path: nativePath + "crypto/Cisco-IOS-XE-crypto:ipsec", open: "<crypto>", close: "</crypto>"})}
	qControlPlane = query{op: "control-plane", config: true,
		filter: `<native xmlns="` + nsNative + `"><control-plane/></native>`,
		rest:   under("native", nsNative, restPart{path: nativePath + "control-plane"})}
	qCertificates = query{op: "certificates",
		filter: `<crypto-pki-oper-data xmlns="` + nsCryptoPKIOper + `"/>`,
		rest:   single("Cisco-IOS-XE-crypto-pki-oper:crypto-pki-oper-data")}
)

// discoveryQueries lists every discovery operation in the order Collect
// issues it. The device's volatile state (CPU and memory utilization,
// interface counters, the routing table, ARP/ND caches, MAC address
// tables) and whole-datastore reads are deliberately absent: none of
// them is a stable inventory or configuration fact.
var discoveryQueries = []query{
	qNativeSystem, qInterfaces, qInterfacesOper, qHardware, qBoot,
	qNativeInterfaces, qLLDP, qCDP, qVRFs, qVLANs, qStaticRoutes,
	qTransceivers, qBGP, qOSPF,
	qAAA, qAccounts, qManagement, qDNS, qSNMP, qLogging, qNTP, qACLs,
	qCrypto, qControlPlane, qCertificates,
}
