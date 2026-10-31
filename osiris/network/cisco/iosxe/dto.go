// dto.go - Typed YANG reply DTOs for the Cisco IOS-XE OSIRIS JSON
// Producer.
//
// Defines one Go struct per YANG container this producer queries, each
// with its root element pinned to the model's XML namespace and a
// single decoder (decodeModel) that turns a NETCONF <data> payload into
// that struct.
// Decoding is strict: a payload whose root element is not the
// expected model, malformed XML, or trailing content after the root all
// return an error, so a broken reply can never look like a device with
// nothing configured. An empty <data/> payload, or one holding only
// comments, is valid and decodes to the zero value with errEmptyModel,
// letting callers record "no instances" distinctly from "decode failed".
//
// Child elements match by local name only: every YANG leaf inherits the
// namespace of its module root, so qualifying them adds nothing.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package iosxe

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
)

// YANG module namespaces of the containers this producer queries.
const (
	nsNative        = "http://cisco.com/ns/yang/Cisco-IOS-XE-native"
	nsIETFInterface = "urn:ietf:params:xml:ns:yang:ietf-interfaces"
	nsHardwareOper  = "http://cisco.com/ns/yang/Cisco-IOS-XE-device-hardware-oper"
	nsCDPOper       = "http://cisco.com/ns/yang/Cisco-IOS-XE-cdp-oper"
	nsBGPOper       = "http://cisco.com/ns/yang/Cisco-IOS-XE-bgp-oper"
	nsOSPFOper      = "http://cisco.com/ns/yang/Cisco-IOS-XE-ospf-oper"
	nsInterfaceOper = "http://cisco.com/ns/yang/Cisco-IOS-XE-interfaces-oper"
	nsLLDPOper      = "http://cisco.com/ns/yang/Cisco-IOS-XE-lldp-oper"
	nsVLANOper      = "http://cisco.com/ns/yang/Cisco-IOS-XE-vlan-oper"
)

var (
	// errEmptyModel reports a valid reply whose <data> was empty: the
	// device has no instance of the queried container. It is not a
	// failure and the decoded value is the zero value.
	errEmptyModel = errors.New("YANG model returned no data")

	// errDecode wraps every malformed or unexpected payload.
	errDecode = errors.New("YANG reply does not match the expected model")
)

// model is implemented by every DTO: rows reports how many modeled
// entries the reply carried, for the discovery coverage record.
type model interface {
	rows() int
}

// decodeModel decodes a NETCONF <data> payload into T. It returns
// errEmptyModel for an empty payload and an errDecode-wrapped error for
// anything else that is not exactly one well-formed root element of T's
// model. Error text carries element names and XML syntax positions
// only, never payload values.
func decodeModel[T model](data []byte) (T, error) {
	var v T
	if len(bytes.TrimSpace(data)) == 0 {
		return v, errEmptyModel
	}
	d := xml.NewDecoder(bytes.NewReader(data))
	if err := d.Decode(&v); err != nil {
		var zero T
		if errors.Is(err, io.EOF) && onlyMarkup(data) {
			return zero, errEmptyModel
		}
		return zero, fmt.Errorf("%w: %v", errDecode, decodeErrText(err))
	}
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			return v, nil
		}
		if err != nil {
			var zero T
			return zero, fmt.Errorf("%w: %v", errDecode, decodeErrText(err))
		}
		switch t := tok.(type) {
		case xml.CharData:
			if len(bytes.TrimSpace(t)) == 0 {
				continue
			}
		case xml.Comment, xml.ProcInst:
			continue
		}
		var zero T
		return zero, fmt.Errorf("%w: unexpected content after the root element", errDecode)
	}
}

// onlyMarkup reports whether data holds nothing but comments,
// processing instructions and whitespace: no element and no text.
func onlyMarkup(data []byte) bool {
	d := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			return true
		}
		if err != nil {
			return false
		}
		switch t := tok.(type) {
		case xml.Comment, xml.ProcInst, xml.Directive:
		case xml.CharData:
			if len(bytes.TrimSpace(t)) != 0 {
				return false
			}
		default:
			return false
		}
	}
}

// decodeErrText keeps encoding/xml's diagnostics but drops any echoed
// character data, which for a syntax error can quote payload content.
func decodeErrText(err error) string {
	var se *xml.SyntaxError
	if errors.As(err, &se) {
		return fmt.Sprintf("XML syntax error on line %d", se.Line)
	}
	var ue xml.UnmarshalError
	if errors.As(err, &ue) {
		return sanitizeErrorText(string(ue), maxErrorMessageRunes)
	}
	return sanitizeErrorText(err.Error(), maxErrorMessageRunes)
}

// nativeSystem is Cisco-IOS-XE-native:native filtered to version and
// hostname.
type nativeSystem struct {
	XMLName  xml.Name `xml:"http://cisco.com/ns/yang/Cisco-IOS-XE-native native"`
	Version  string   `xml:"version"`
	Hostname string   `xml:"hostname"`
}

func (n nativeSystem) rows() int {
	if strings.TrimSpace(n.Version) == "" && strings.TrimSpace(n.Hostname) == "" {
		return 0
	}
	return 1
}

// nativeBoot is Cisco-IOS-XE-native:native filtered to boot: the
// configured "boot system" image list.
type nativeBoot struct {
	XMLName  xml.Name `xml:"http://cisco.com/ns/yang/Cisco-IOS-XE-native native"`
	Bootfile struct {
		// Ordered keeps the configured order; List is the same entries
		// keyed by filename, reported by some releases instead.
		Ordered []bootFile `xml:"filename-list-ordered-by-user"`
		List    []bootFile `xml:"filename-list"`
	} `xml:"boot>system>bootfile"`
}

type bootFile struct {
	Filename string `xml:"filename"`
}

// images returns the configured boot images in configured order.
func (b nativeBoot) images() []string {
	src := b.Bootfile.Ordered
	if len(src) == 0 {
		src = b.Bootfile.List
	}
	var out []string
	for _, f := range src {
		if v := strings.TrimSpace(f.Filename); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func (b nativeBoot) rows() int { return len(b.images()) }

// nativeVRFList is Cisco-IOS-XE-native:native filtered to vrf.
type nativeVRFList struct {
	XMLName xml.Name `xml:"http://cisco.com/ns/yang/Cisco-IOS-XE-native native"`
	VRF     struct {
		Definitions []vrfDef `xml:"definition"`
	} `xml:"vrf"`
}

func (n nativeVRFList) rows() int { return len(n.VRF.Definitions) }

type vrfDef struct {
	Name string `xml:"name"`
	RD   string `xml:"rd"`
	Desc string `xml:"description"`
	AF   *vrfAF `xml:"address-family"`
}

type vrfAF struct {
	IPv4RT string `xml:"ipv4>route-target>export"`
	IPv6RT string `xml:"ipv6>route-target>export"`
}

// ietfInterfaces is ietf-interfaces:interfaces.
type ietfInterfaces struct {
	XMLName    xml.Name    `xml:"urn:ietf:params:xml:ns:yang:ietf-interfaces interfaces"`
	Interfaces []ietfIface `xml:"interface"`
}

func (i ietfInterfaces) rows() int { return len(i.Interfaces) }

// ietfIface is one configured interface. Operational leaves (status,
// speed, MAC, MTU) are not part of this configuration model; they come
// from interfacesOper. Interface statistics are high-churn counters and
// are not decoded.
type ietfIface struct {
	Name        string  `xml:"name"`
	Type        string  `xml:"type"`
	Enabled     string  `xml:"enabled"`
	Description string  `xml:"description"`
	IPv4        *ifIPv4 `xml:"ipv4"`
	IPv6        *ifIPv6 `xml:"ipv6"`
}

type ifIPv4 struct {
	Address []ifIPv4Addr `xml:"address"`
}

type ifIPv4Addr struct {
	IP     string `xml:"ip"`
	Prefix string `xml:"netmask"`
}

type ifIPv6 struct {
	Address []ifIPv6Addr `xml:"address"`
}

type ifIPv6Addr struct {
	IP           string `xml:"ip"`
	PrefixLength string `xml:"prefix-length"`
}

// interfacesOper is Cisco-IOS-XE-interfaces-oper:interfaces, the
// operational interface state: oper and admin status, speed, MAC, MTU,
// negotiated duplex and the VRF each interface is bound to.
// Statistics and protocol counters are deliberately not decoded.
type interfacesOper struct {
	XMLName    xml.Name    `xml:"http://cisco.com/ns/yang/Cisco-IOS-XE-interfaces-oper interfaces"`
	Interfaces []operIface `xml:"interface"`
}

func (i interfacesOper) rows() int { return len(i.Interfaces) }

type operIface struct {
	Name          string `xml:"name"`
	InterfaceType string `xml:"interface-type"`
	AdminStatus   string `xml:"admin-status"`
	OperStatus    string `xml:"oper-status"`
	PhysAddress   string `xml:"phys-address"`
	Speed         string `xml:"speed"` // bits per second.
	MTU           string `xml:"mtu"`
	VRF           string `xml:"vrf"`
	Description   string `xml:"description"`
	IPv4          string `xml:"ipv4"`
	IPv4Mask      string `xml:"ipv4-subnet-mask"`
	EtherState    struct {
		NegotiatedDuplex string `xml:"negotiated-duplex-mode"`
	} `xml:"ether-state"`
}

// hwInventory is Cisco-IOS-XE-device-hardware-oper:device-hardware-data.
type hwInventory struct {
	XMLName    xml.Name      `xml:"http://cisco.com/ns/yang/Cisco-IOS-XE-device-hardware-oper device-hardware-data"`
	Components []hwComponent `xml:"device-hardware>device-inventory"`
	SystemData struct {
		// SoftwareVersion is the full IOS banner, e.g. "Cisco IOS
		// Software [IOSXE], Virtual XE Software (...), Version 17.15.1a, ...".
		SoftwareVersion string `xml:"software-version"`
	} `xml:"device-hardware>device-system-data"`
}

func (h hwInventory) rows() int { return len(h.Components) }

type hwComponent struct {
	Name             string `xml:"hw-type"`
	Description      string `xml:"hw-description"`
	PartNumber       string `xml:"part-number"`
	SerialNumber     string `xml:"serial-number"`
	DevName          string `xml:"dev-name"`
	HwDevIndex       string `xml:"hw-dev-index"`
	Version          string `xml:"version"`
	FieldReplaceable string `xml:"field-replaceable"`
	HwClass          string `xml:"hw-class"`
}

// HwType returns the hw-type or dev-name for classification.
func (c hwComponent) HwType() string {
	if c.Name != "" {
		return c.Name
	}
	return c.DevName
}

// cdpNeighborDetails is Cisco-IOS-XE-cdp-oper:cdp-neighbor-details.
type cdpNeighborDetails struct {
	XMLName   xml.Name      `xml:"http://cisco.com/ns/yang/Cisco-IOS-XE-cdp-oper cdp-neighbor-details"`
	Neighbors []cdpNeighbor `xml:"cdp-neighbor-detail"`
}

func (c cdpNeighborDetails) rows() int { return len(c.Neighbors) }

type cdpNeighbor struct {
	DeviceName  string `xml:"device-name"`
	LocalIntf   string `xml:"local-intf-name"`
	PortID      string `xml:"port-id"`
	Platform    string `xml:"platform-name"`
	MgmtAddress string `xml:"mgmt-address"`
	IPAddress   string `xml:"ip-address"`
}

// bgpStateData is Cisco-IOS-XE-bgp-oper:bgp-state-data. Only the
// neighbor list is modeled; route-filter and per-AFI route trees are
// left undecoded. A neighbor appears once per address family it is
// activated in (list key afi-safi, vrf-name, neighbor-id).
type bgpStateData struct {
	XMLName   xml.Name `xml:"http://cisco.com/ns/yang/Cisco-IOS-XE-bgp-oper bgp-state-data"`
	Neighbors struct {
		Entries []bgpNeighbor `xml:"neighbor"`
	} `xml:"neighbors"`
}

func (b bgpStateData) rows() int { return len(b.Neighbors.Entries) }

type bgpNeighbor struct {
	AFISAFI     string `xml:"afi-safi"`
	VRFName     string `xml:"vrf-name"`
	NeighborID  string `xml:"neighbor-id"`
	Description string `xml:"description"`
	// SessionState is the BGP FSM state (fsm-idle .. fsm-established).
	SessionState string `xml:"session-state"`
	AS           string `xml:"as"`
	LocalHost    string `xml:"transport>local-host"`
}

// ospfOperData is Cisco-IOS-XE-ospf-oper:ospf-oper-data, decoded along
// ospf-state/ospf-instance/ospf-area/ospf-interface/ospf-neighbor. LSA
// databases and per-neighbor statistics are left undecoded.
type ospfOperData struct {
	XMLName   xml.Name       `xml:"http://cisco.com/ns/yang/Cisco-IOS-XE-ospf-oper ospf-oper-data"`
	Instances []ospfInstance `xml:"ospf-state>ospf-instance"`
}

func (o ospfOperData) rows() int { return len(o.Instances) }

type ospfInstance struct {
	AF string `xml:"af"`
	// RouterID is the RFC 2328 router ID as a uint32.
	RouterID  string     `xml:"router-id"`
	ProcessID string     `xml:"process-id"`
	Areas     []ospfArea `xml:"ospf-area"`
}

type ospfArea struct {
	AreaID     string          `xml:"area-id"`
	Interfaces []ospfInterface `xml:"ospf-interface"`
}

type ospfInterface struct {
	Name      string         `xml:"name"`
	Neighbors []ospfNeighbor `xml:"ospf-neighbor"`
}

type ospfNeighbor struct {
	NeighborID string `xml:"neighbor-id"`
	Address    string `xml:"address"`
	// State is the neighbor FSM state (ospf-nbr-down .. ospf-nbr-full).
	State string `xml:"state"`
}

// lldpEntries is Cisco-IOS-XE-lldp-oper:lldp-entries. Neighbors are
// read from the per-interface detail list, which carries the remote
// system name and management addresses; the summary lldp-entry list is
// the fallback when the detail list is absent. Global state counters
// are left undecoded.
type lldpEntries struct {
	XMLName  xml.Name        `xml:"http://cisco.com/ns/yang/Cisco-IOS-XE-lldp-oper lldp-entries"`
	Entries  []lldpEntry     `xml:"lldp-entry"`
	IntfDets []lldpIntfEntry `xml:"lldp-intf-details"`
}

func (l lldpEntries) rows() int {
	n := 0
	for _, i := range l.IntfDets {
		n += len(i.Neighbors)
	}
	if n == 0 {
		n = len(l.Entries)
	}
	return n
}

type lldpEntry struct {
	DeviceID            string `xml:"device-id"`
	LocalInterface      string `xml:"local-interface"`
	ConnectingInterface string `xml:"connecting-interface"`
}

type lldpIntfEntry struct {
	IfName    string         `xml:"if-name"`
	Neighbors []lldpNeighbor `xml:"lldp-neighbor-details"`
}

type lldpNeighbor struct {
	ChassisID  string `xml:"chassis-id"`
	PortID     string `xml:"port-id"`
	SystemName string `xml:"system-name"`
	MgmtAddrs  []struct {
		Addr string `xml:"mgmt-addr"`
	} `xml:"mgmt-addrs"`
}

// vlansOper is Cisco-IOS-XE-vlan-oper:vlans: the VLAN database and the
// interfaces assigned to each VLAN.
type vlansOper struct {
	XMLName xml.Name   `xml:"http://cisco.com/ns/yang/Cisco-IOS-XE-vlan-oper vlans"`
	VLANs   []operVLAN `xml:"vlan"`
}

func (v vlansOper) rows() int { return len(v.VLANs) }

type operVLAN struct {
	ID     string       `xml:"id"`
	Name   string       `xml:"name"`
	Status string       `xml:"status"` // active or suspend.
	Ports  []vlanMember `xml:"ports"`
	Intfs  []vlanMember `xml:"vlan-interfaces"`
}

type vlanMember struct {
	Interface    string `xml:"interface"`
	Subinterface string `xml:"subinterface"`
}

// name returns the member interface name, with the subinterface number
// appended when one is set.
func (m vlanMember) name() string {
	n := strings.TrimSpace(m.Interface)
	if s := strings.TrimSpace(m.Subinterface); n != "" && s != "" && s != "0" {
		n += "." + s
	}
	return n
}

// nativeInterfaces is Cisco-IOS-XE-native:native filtered to interface.
// Each child of <interface> is one interface type (GigabitEthernet,
// Vlan, Loopback, ...) keyed by its number, so the full interface name
// is the element name followed by <name>. Only 802.1Q encapsulation,
// the configured VRF ("vrf forwarding"), the IPv4 access-group and IPv6
// traffic-filter ACL bindings and the switchport VLAN configuration
// (Cisco-IOS-XE-switch) are decoded; every other configuration leaf,
// including any credential the interface configuration may hold, is
// ignored.
type nativeInterfaces struct {
	XMLName   xml.Name `xml:"http://cisco.com/ns/yang/Cisco-IOS-XE-native native"`
	Interface struct {
		Entries []nativeInterface `xml:",any"`
	} `xml:"interface"`
}

func (n nativeInterfaces) rows() int { return len(n.Interface.Entries) }

type nativeInterface struct {
	XMLName    xml.Name
	Name       string     `xml:"name"`
	Dot1Q      string     `xml:"encapsulation>dot1Q>vlan-id"`
	VRF        string     `xml:"vrf>forwarding"`
	Switchport switchport `xml:"switchport-config>switchport"`
	ACLIn      string     `xml:"ip>access-group>in>acl>acl-name"`
	ACLOut     string     `xml:"ip>access-group>out>acl>acl-name"`
	IPv6Filter []struct {
		Direction string `xml:"direction"`
		ACL       string `xml:"access-list"`
	} `xml:"ipv6>traffic-filter"`
}

// switchport is the Cisco-IOS-XE-switch switchport configuration of one
// interface: its mode, access VLAN, trunk allowed-VLAN list (the
// current vlan-v2 container and the deprecated vlan container some
// releases still return) and trunk native VLAN.
type switchport struct {
	Mode struct {
		Access *struct{} `xml:"access"`
		Trunk  *struct{} `xml:"trunk"`
	} `xml:"mode"`
	AccessVLAN string `xml:"access>vlan>vlan"`
	Allowed    struct {
		V2 struct {
			VLANs  string    `xml:"vlan-choices>vlans"`
			None   *struct{} `xml:"vlan-choices>none"`
			All    string    `xml:"vlan-choices>all"`
			Add    []string  `xml:"add-vlans>add>vlans"`
			Except string    `xml:"except"`
			Remove string    `xml:"remove"`
		} `xml:"vlan-v2"`
		Legacy struct {
			VLANs  string    `xml:"vlans"`
			None   *struct{} `xml:"none"`
			All    *struct{} `xml:"all"`
			Add    string    `xml:"add"`
			Except string    `xml:"except"`
			Remove string    `xml:"remove"`
		} `xml:"vlan"`
	} `xml:"trunk>allowed"`
	NativeVLAN string `xml:"trunk>native>vlan>vlan-id"`
}

// fullName returns the interface name as the operational models report
// it, e.g. "GigabitEthernet0/0/1.100".
func (n nativeInterface) fullName() string {
	num := strings.TrimSpace(n.Name)
	if num == "" {
		return ""
	}
	return n.XMLName.Local + num
}

// nativeRoutes is Cisco-IOS-XE-native:native filtered to ip/route and
// ipv6/route: configured static routes, in the global table and per
// VRF. It is configuration, not the routing table.
type nativeRoutes struct {
	XMLName xml.Name `xml:"http://cisco.com/ns/yang/Cisco-IOS-XE-native native"`
	IPv4    struct {
		Routes []ipv4Route `xml:"ip-route-interface-forwarding-list"`
		VRFs   []struct {
			Name   string      `xml:"name"`
			Routes []ipv4Route `xml:"ip-route-interface-forwarding-list"`
		} `xml:"vrf"`
	} `xml:"ip>route"`
	IPv6 struct {
		Routes []ipv6Route `xml:"ipv6-route-list"`
		VRFs   []struct {
			Name   string      `xml:"name"`
			Routes []ipv6Route `xml:"ipv6-route-list"`
		} `xml:"vrf"`
	} `xml:"ipv6>route"`
}

func (n nativeRoutes) rows() int {
	c := len(n.IPv4.Routes) + len(n.IPv6.Routes)
	for _, v := range n.IPv4.VRFs {
		c += len(v.Routes)
	}
	for _, v := range n.IPv6.VRFs {
		c += len(v.Routes)
	}
	return c
}

type ipv4Route struct {
	Prefix  string     `xml:"prefix"`
	Mask    string     `xml:"mask"`
	Fwd     []routeFwd `xml:"fwd-list"`
	FwdTrck []routeFwd `xml:"fwd-list-with-track"`
}

type ipv6Route struct {
	Prefix string     `xml:"prefix"`
	Fwd    []routeFwd `xml:"ipv6-fwd-list"`
}

// routeFwd is one forwarding entry: fwd is a next-hop address, an
// interface name, or Null0; interface-next-hop qualifies an interface
// with a next-hop address.
type routeFwd struct {
	Fwd      string `xml:"fwd"`
	Metric   string `xml:"metric"`
	Name     string `xml:"name"`
	NextHops []struct {
		IPv4   string `xml:"ip-address"`
		IPv6   string `xml:"ipv6-address"`
		Metric string `xml:"metric"`
		Name   string `xml:"name"`
	} `xml:"interface-next-hop"`
}
