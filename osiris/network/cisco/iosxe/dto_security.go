// dto_security.go - Typed YANG reply DTOs for the audit-purpose
// security posture of the Cisco IOS-XE OSIRIS JSON Producer.
//
// Each struct decodes one Cisco-IOS-XE-native configuration subtree, or
// one operational model and declares only the leaves this producer
// emits. Leaves that carry secrets material are never declared, so
// they are never decoded into memory structures the transforms can
// reach: the TACACS+ and RADIUS key, the local account secret and
// password text, the enable secret, SNMP community strings (the list
// key of community-config and the community-or-user of host-config),
// NTP authentication-key values, line passwords, PKI trustpoint
// passwords and IKE keyrings. Only the storage type of a hashed
// secrets (its encryption leaf) is decoded.
//
// Element paths follow the IOS-XE 17.15 YANG models. Child elements
// match by local name, so a leaf contributed by an augmenting module
// (Cisco-IOS-XE-aaa, -snmp, -ntp, -http, -acl, -crypto, -policy)
// decodes the same as one defined by Cisco-IOS-XE-native itself.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package iosxe

import (
	"encoding/xml"
	"strings"
)

// YANG module namespaces of the security and inventory models.
// Cisco defines them as http URIs. They are XML namespace identifiers
// that must match the device models exactly and are never fetched.
const (
	nsHTTP            = "http://cisco.com/ns/yang/Cisco-IOS-XE-http"             // NOSONAR
	nsCrypto          = "http://cisco.com/ns/yang/Cisco-IOS-XE-crypto"           // NOSONAR
	nsCryptoPKIOper   = "http://cisco.com/ns/yang/Cisco-IOS-XE-crypto-pki-oper"  // NOSONAR
	nsTransceiverOper = "http://cisco.com/ns/yang/Cisco-IOS-XE-transceiver-oper" // NOSONAR
)

// present is a YANG empty leaf: non-nil when the element is present.
type present *struct{}

// xmlNode is a generic element tree, used only for the AAA method-list
// subtrees, whose method leaves vary by service and release and never
// carry secrets material.
type xmlNode struct {
	XMLName xml.Name
	Text    string    `xml:",chardata"`
	Nodes   []xmlNode `xml:",any"`
}

// child returns the first direct child named local.
func (n xmlNode) child(local string) (xmlNode, bool) {
	for _, c := range n.Nodes {
		if c.XMLName.Local == local {
			return c, true
		}
	}
	return xmlNode{}, false
}

// text returns the trimmed text of the first direct child named local.
func (n xmlNode) text(local string) string {
	if c, ok := n.child(local); ok {
		return strings.TrimSpace(c.Text)
	}
	return ""
}

// nativeAAA is Cisco-IOS-XE-native:native filtered to aaa, tacacs and
// radius.
type nativeAAA struct {
	XMLName xml.Name `xml:"http://cisco.com/ns/yang/Cisco-IOS-XE-native native"`
	AAA     struct {
		NewModel present `xml:"new-model"`
		Groups   struct {
			Tacacs []aaaServerGroup `xml:"tacacsplus"`
			Radius []aaaServerGroup `xml:"radius"`
		} `xml:"group>server"`
		// Each child of authentication, authorization and accounting is
		// one method-list entry of the service its element names
		// (login, exec, commands, network, ...).
		Authentication struct {
			Entries []xmlNode `xml:",any"`
		} `xml:"authentication"`
		Authorization struct {
			Entries []xmlNode `xml:",any"`
		} `xml:"authorization"`
		Accounting struct {
			Entries []xmlNode `xml:",any"`
		} `xml:"accounting"`
	} `xml:"aaa"`
	Tacacs []tacacsServer `xml:"tacacs>server"`
	Radius []radiusServer `xml:"radius>server"`
}

func (n nativeAAA) rows() int {
	c := len(n.AAA.Groups.Tacacs) + len(n.AAA.Groups.Radius) +
		len(n.AAA.Authentication.Entries) + len(n.AAA.Authorization.Entries) +
		len(n.AAA.Accounting.Entries) + len(n.Tacacs) + len(n.Radius)
	if n.AAA.NewModel != nil {
		c++
	}
	return c
}

// aaaServerGroup is one "aaa group server tacacs+|radius" group: its
// member server names and its private servers (address and port only;
// the private server key is not decoded).
type aaaServerGroup struct {
	Name    string `xml:"name"`
	Members []struct {
		Name string `xml:"name"`
	} `xml:"server>name"`
	Private []struct {
		Name     string `xml:"name"`
		Port     string `xml:"port"`
		AuthPort string `xml:"auth-port"`
		AcctPort string `xml:"acct-port"`
	} `xml:"server-private"`
}

// tacacsServer is one "tacacs server" definition, without its key.
type tacacsServer struct {
	Name    string `xml:"name"`
	IPv4    string `xml:"address>ipv4"`
	IPv6    string `xml:"address>ipv6"`
	FQDN    string `xml:"address>fqdn"`
	Port    string `xml:"port"`
	Timeout string `xml:"timeout"`
}

// radiusServer is one "radius server" definition, without its key.
type radiusServer struct {
	ID       string `xml:"id"`
	IPv4     string `xml:"address>ipv4"`
	IPv6     string `xml:"address>ipv6"`
	FQDN     string `xml:"address>fqdn"`
	AuthPort string `xml:"address>auth-port"`
	AcctPort string `xml:"address>acct-port"`
	Timeout  string `xml:"timeout"`
}

// nativeAccounts is Cisco-IOS-XE-native:native filtered to username and
// enable. Only the storage type of each secrets is decoded.
type nativeAccounts struct {
	XMLName xml.Name `xml:"http://cisco.com/ns/yang/Cisco-IOS-XE-native native"`
	Users   []struct {
		Name      string `xml:"name"`
		Privilege string `xml:"privilege"`
		Secret    *struct {
			Encryption string `xml:"encryption"`
		} `xml:"secret"`
		Password *struct {
			Encryption string `xml:"encryption"`
		} `xml:"password"`
	} `xml:"username"`
	Enable struct {
		Secret *struct {
			Type  string `xml:"type"`
			Level string `xml:"level"`
		} `xml:"secret"`
		Password *struct {
			Type  string `xml:"type"`
			Level string `xml:"level"`
		} `xml:"password"`
	} `xml:"enable"`
}

func (n nativeAccounts) rows() int {
	c := len(n.Users)
	if n.Enable.Secret != nil || n.Enable.Password != nil {
		c++
	}
	return c
}

// nativeManagement is Cisco-IOS-XE-native:native filtered to the
// management-plane services: ip http (Cisco-IOS-XE-http), ip ssh,
// ip scp, line and service.
type nativeManagement struct {
	XMLName xml.Name `xml:"http://cisco.com/ns/yang/Cisco-IOS-XE-native native"`
	IP      struct {
		HTTP struct {
			Server           string `xml:"server"`
			SecureServer     string `xml:"secure-server"`
			SecureTrustpoint string `xml:"secure-trustpoint"`
			AccessClass      string `xml:"access-class"`
			MaxConnections   string `xml:"max-connections"`
		} `xml:"http"`
		SSH struct {
			Version         string `xml:"version"`
			TimeOut         string `xml:"time-out"`
			Retries         string `xml:"authentication-retries"`
			SourceInterface string `xml:"source-interface"`
		} `xml:"ssh"`
		SCPServer present `xml:"scp>server>enable"`
	} `xml:"ip"`
	VTY                []vtyLine `xml:"line>vty"`
	PasswordEncryption present   `xml:"service>password-encryption"`
}

func (n nativeManagement) rows() int {
	c := len(n.VTY)
	if n.IP.HTTP.Server != "" || n.IP.HTTP.SecureServer != "" {
		c++
	}
	if n.IP.SSH.Version != "" || n.IP.SSH.TimeOut != "" {
		c++
	}
	return c
}

// vtyLine is one "line vty <first> [<last>]" range. The line password
// is not decoded.
type vtyLine struct {
	First       string `xml:"first"`
	Last        string `xml:"last"`
	AccessClass []struct {
		Direction string `xml:"direction"`
		ACL       string `xml:"access-list"`
	} `xml:"access-class>acccess-list"` // the YANG model spells it "acccess-list".
	ExecTimeout struct {
		Minutes string `xml:"minutes"`
		Seconds string `xml:"seconds"`
	} `xml:"exec-timeout"`
	LoginLocal present `xml:"login>local"`
	LoginList  string  `xml:"login>authentication"`
	Transport  struct {
		All   present  `xml:"all"`
		None  present  `xml:"none"`
		Input []string `xml:"input"`
		SSH   string   `xml:"ssh"`
	} `xml:"transport>input"`
}

// nativeDNS is Cisco-IOS-XE-native:native filtered to ip domain and ip
// name-server.
type nativeDNS struct {
	XMLName xml.Name `xml:"http://cisco.com/ns/yang/Cisco-IOS-XE-native native"`
	Domain  struct {
		Name     string `xml:"name"`
		Lookup   string `xml:"lookup"`
		NoVRF    string `xml:"name-container>name-no-vrf"`
		VRFNames []struct {
			VRF  string `xml:"word"`
			Name string `xml:"name-vrf"`
		} `xml:"name-container>vrf"`
	} `xml:"ip>domain"`
	NameServers struct {
		Ordered []string `xml:"no-vrf-ordered"`
		NoVRF   []string `xml:"no-vrf"`
		VRFs    []struct {
			VRF     string   `xml:"word"`
			Ordered []string `xml:"server-ip-list-ordered"`
			List    []string `xml:"server-ip-list"`
		} `xml:"vrf"`
	} `xml:"ip>name-server"`
}

func (n nativeDNS) rows() int {
	c := len(n.NameServers.Ordered) + len(n.NameServers.NoVRF) + len(n.NameServers.VRFs)
	if n.Domain.Name != "" || n.Domain.NoVRF != "" {
		c++
	}
	return c
}

// nativeSNMP is Cisco-IOS-XE-native:native filtered to snmp-server
// (Cisco-IOS-XE-snmp). Community strings are list keys here and are
// never decoded: a community row keeps only its permission, view and
// access list and a notification host keeps only its address, VRF,
// version and security level.
type nativeSNMP struct {
	XMLName xml.Name `xml:"http://cisco.com/ns/yang/Cisco-IOS-XE-native native"`
	SNMP    struct {
		Communities []struct {
			Permission string `xml:"permission"`
			View       string `xml:"view"`
			ACL        string `xml:"access-list-name"`
		} `xml:"community-config"`
		// LegacyCommunities is the deprecated community list.
		LegacyCommunities []struct {
			RO   present `xml:"RO"`
			RW   present `xml:"RW"`
			View string  `xml:"view"`
			ACL  string  `xml:"access-list-name"`
		} `xml:"community"`
		Groups []struct {
			ID  string  `xml:"id"`
			V1  present `xml:"v1"`
			V2c present `xml:"v2c"`
			V3  *struct {
				Levels []string `xml:"security-level-list-config>security-level"`
			} `xml:"v3"`
		} `xml:"group"`
		Hosts struct {
			Community     []snmpHost `xml:"ip-community"`
			CommunityPort []snmpHost `xml:"ip-community-port"`
			VRF           []snmpHost `xml:"ip-vrf-community"`
			VRFPort       []snmpHost `xml:"ip-vrf-community-port"`
		} `xml:"host-config"`
		Location string `xml:"location"`
		Contact  string `xml:"contact"`
	} `xml:"snmp-server"`
}

func (n nativeSNMP) rows() int {
	s := n.SNMP
	c := len(s.Communities) + len(s.LegacyCommunities) + len(s.Groups) +
		len(s.Hosts.Community) + len(s.Hosts.CommunityPort) + len(s.Hosts.VRF) + len(s.Hosts.VRFPort)
	if s.Location != "" || s.Contact != "" {
		c++
	}
	return c
}

// snmpHost is one notification receiver, without its community-or-user.
type snmpHost struct {
	Address       string `xml:"ip-address"`
	VRF           string `xml:"vrf"`
	Version       string `xml:"version"`
	SecurityLevel string `xml:"security-level"`
}

// nativeLogging is Cisco-IOS-XE-native:native filtered to logging.
type nativeLogging struct {
	XMLName xml.Name `xml:"http://cisco.com/ns/yang/Cisco-IOS-XE-native native"`
	Logging struct {
		IPv4      []logHost `xml:"host>ipv4-host-list"`
		IPv4Xport []logHost `xml:"host>ipv4-host-transport-list"`
		IPv4VRF   []logHost `xml:"host>ipv4-host-vrf-list"`
		IPv4VRFX  []logHost `xml:"host>ipv4-host-vrf-transport-list"`
		IPv6      []logHost `xml:"host>ipv6>ipv6-host-list"`
		IPv6Xport []logHost `xml:"host>ipv6>ipv6-host-transport-list"`
		IPv6VRF   []logHost `xml:"host>ipv6>ipv6-host-vrf-list"`
		IPv6VRFX  []logHost `xml:"host>ipv6>ipv6-host-vrf-transport-list"`
		Trap      string    `xml:"trap>severity"`
		Buffered  struct {
			Size     string `xml:"size-value"`
			Severity string `xml:"severity-level"`
		} `xml:"buffered"`
		Source string `xml:"source-interface-conf>interface-name-non-vrf"`
	} `xml:"logging"`
}

func (n nativeLogging) rows() int {
	l := n.Logging
	c := len(l.IPv4) + len(l.IPv4Xport) + len(l.IPv4VRF) + len(l.IPv4VRFX) +
		len(l.IPv6) + len(l.IPv6Xport) + len(l.IPv6VRF) + len(l.IPv6VRFX)
	if l.Trap != "" || l.Buffered.Size != "" || l.Source != "" {
		c++
	}
	return c
}

// logHost is one syslog receiver and the transport it is reached over.
type logHost struct {
	IPv4 string    `xml:"ipv4-host"`
	IPv6 string    `xml:"ipv6-host"`
	VRF  string    `xml:"vrf"`
	UDP  *struct{} `xml:"transport>udp"`
	TCP  *struct{} `xml:"transport>tcp"`
	TLS  *struct{} `xml:"transport>tls"`
}

// nativeNTP is Cisco-IOS-XE-native:native filtered to ntp
// (Cisco-IOS-XE-ntp). Authentication-key values are never decoded: only
// the key numbers.
type nativeNTP struct {
	XMLName xml.Name `xml:"http://cisco.com/ns/yang/Cisco-IOS-XE-native native"`
	NTP     struct {
		Authenticate present `xml:"authenticate"`
		Keys         []struct {
			Number string `xml:"number"`
		} `xml:"authentication-key"`
		Trusted []struct {
			Number string `xml:"number"`
		} `xml:"trusted-key"`
		Servers    []ntpAssoc `xml:"server>server-list"`
		ServerVRFs []struct {
			Name  string     `xml:"name"`
			Assoc []ntpAssoc `xml:"server-list"`
		} `xml:"server>vrf"`
		Peers    []ntpAssoc `xml:"peer>server-list"`
		PeerVRFs []struct {
			Name  string     `xml:"name"`
			Assoc []ntpAssoc `xml:"server-list"`
		} `xml:"peer>vrf"`
		MasterStratum string `xml:"master>stratum-number"`
		AccessGroups  struct {
			Peer      string `xml:"peer>acl"`
			Serve     string `xml:"serve>acl"`
			ServeOnly string `xml:"serve-only>acl"`
			QueryOnly string `xml:"query-only>acl"`
			IPv4      struct {
				Peer      string `xml:"peer>acl"`
				Serve     string `xml:"serve>acl"`
				ServeOnly string `xml:"serve-only>acl"`
				QueryOnly string `xml:"query-only>acl"`
			} `xml:"ipv4"`
		} `xml:"access-group"`
	} `xml:"ntp"`
}

func (n nativeNTP) rows() int {
	c := len(n.NTP.Servers) + len(n.NTP.Peers) + len(n.NTP.Keys) + len(n.NTP.Trusted)
	for _, v := range n.NTP.ServerVRFs {
		c += len(v.Assoc)
	}
	for _, v := range n.NTP.PeerVRFs {
		c += len(v.Assoc)
	}
	return c
}

// ntpAssoc is one NTP server or peer association.
type ntpAssoc struct {
	Address string  `xml:"ip-address"`
	Source  string  `xml:"source"`
	Key     string  `xml:"key"` // key number, not key material.
	Prefer  present `xml:"prefer"`
	Version string  `xml:"version"`
}

// nativeACLs is Cisco-IOS-XE-native:native filtered to ip access-list
// and ipv6 access-list (Cisco-IOS-XE-acl).
type nativeACLs struct {
	XMLName  xml.Name `xml:"http://cisco.com/ns/yang/Cisco-IOS-XE-native native"`
	Standard []struct {
		Name  string `xml:"name"`
		Rules []struct {
			Sequence string     `xml:"sequence"`
			Remark   string     `xml:"remark"`
			Permit   *stdACEBox `xml:"permit"`
			Deny     *stdACEBox `xml:"deny"`
		} `xml:"access-list-seq-rule"`
	} `xml:"ip>access-list>standard"`
	Extended []struct {
		Name  string     `xml:"name"`
		Rules []aclRuleX `xml:"access-list-seq-rule"`
	} `xml:"ip>access-list>extended"`
	IPv6 []struct {
		Name  string     `xml:"name"`
		Rules []aclRuleX `xml:"access-list-seq-rule"`
	} `xml:"ipv6>access-list>named-acl"`
}

func (n nativeACLs) rows() int { return len(n.Standard) + len(n.Extended) + len(n.IPv6) }

type stdACEBox struct {
	ACE stdACE `xml:"std-ace"`
}

// stdACE is the match of one standard ACL entry.
type stdACE struct {
	Prefix  string  `xml:"ipv4-address-prefix"`
	Prefix2 string  `xml:"ipv4-prefix"`
	Mask    string  `xml:"mask"`
	Any     present `xml:"any"`
	Host    string  `xml:"host-address"`
	Host2   string  `xml:"host"`
	Log     present `xml:"log"`
}

// aclRuleX is one extended (IPv4 or IPv6) ACL entry.
type aclRuleX struct {
	Sequence string   `xml:"sequence"`
	Remark   string   `xml:"remark"`
	ACE      *aceRule `xml:"ace-rule"`
}

// aceRule holds the leaves of an extended ace-rule this producer emits.
// IPv4 and IPv6 rules use different leaf names for the same match; both
// are declared and the populated one wins.
type aceRule struct {
	Action   string `xml:"action"`
	Protocol string `xml:"protocol"`
	// Source, IPv4.
	SrcAddr  string  `xml:"ipv4-address"`
	SrcMask  string  `xml:"mask"`
	SrcAny   present `xml:"any"`
	SrcHost  string  `xml:"host-address"`
	SrcHost2 string  `xml:"host"`
	// Source, IPv6.
	SrcPrefix6 string `xml:"source-prefix"`
	SrcHost6   string `xml:"source-host"`
	SrcAddr6   string `xml:"source-address"`
	// Destination, IPv4.
	DstAddr  string  `xml:"dest-ipv4-address"`
	DstMask  string  `xml:"dest-mask"`
	DstAny   present `xml:"dst-any"`
	DstHost  string  `xml:"dst-host-address"`
	DstHost2 string  `xml:"dst-host"`
	// Destination, IPv6.
	DstPrefix6 string  `xml:"destination-prefix"`
	DstHost6   string  `xml:"destination-host"`
	DstAddr6   string  `xml:"destination-address"`
	DstAny6    present `xml:"destination-any"`
	// Ports: eq takes up to ten values, range two.
	SrcEq  []string `xml:"src-eq"`
	SrcGt  string   `xml:"src-gt"`
	SrcLt  string   `xml:"src-lt"`
	SrcNeq string   `xml:"src-neq"`
	SrcR1  string   `xml:"src-range1"`
	SrcR2  string   `xml:"src-range2"`
	DstEq  []string `xml:"dst-eq"`
	DstGt  string   `xml:"dst-gt"`
	DstLt  string   `xml:"dst-lt"`
	DstNeq string   `xml:"dst-neq"`
	DstR1  string   `xml:"dst-range1"`
	DstR2  string   `xml:"dst-range2"`
	// Additional eq values (src-eq-port2 .. src-eq-port10 and the dst
	// equivalents) are collected from the remaining elements.
	Extra       []xmlNode `xml:",any"`
	Established present   `xml:"established"`
	Log         present   `xml:"log"`
	LogInput    present   `xml:"log-input"`
}

// nativeCrypto is Cisco-IOS-XE-native:native filtered to the crypto
// posture (Cisco-IOS-XE-crypto): PKI trustpoints, IKEv2 proposals and
// IPsec transform sets. Trustpoint passwords and IKE keyrings are not
// queried or decoded.
type nativeCrypto struct {
	XMLName     xml.Name `xml:"http://cisco.com/ns/yang/Cisco-IOS-XE-native native"`
	Trustpoints []struct {
		ID         string `xml:"id"`
		Enrollment struct {
			Selfsigned present   `xml:"selfsigned"`
			URL        string    `xml:"url"`
			Terminal   *struct{} `xml:"terminal"`
			PKCS12     present   `xml:"pkcs12"`
			Profile    string    `xml:"profile"`
			Method     struct {
				Selfsigned present   `xml:"selfsigned"`
				URL        string    `xml:"url"`
				Terminal   *struct{} `xml:"terminal"`
				PKCS12     present   `xml:"pkcs12"`
				Profile    string    `xml:"profile"`
			} `xml:"enrollment-method"`
		} `xml:"enrollment"`
		Revocation  []string `xml:"revocation-check"`
		KeyLabel    string   `xml:"rsakeypair>key-label"`
		ECKeyPair   string   `xml:"eckeypair"`
		SubjectName string   `xml:"subject-name"`
		Hash        string   `xml:"hash"`
		Usage       string   `xml:"usage"`
	} `xml:"crypto>pki>trustpoint"`
	IKEv2Proposals []struct {
		Name       string  `xml:"name"`
		Encryption xmlNode `xml:"encryption"`
		Integrity  xmlNode `xml:"integrity"`
		PRF        xmlNode `xml:"prf"`
		Group      xmlNode `xml:"group"`
	} `xml:"crypto>ikev2>proposal"`
	TransformSets []struct {
		Tag     string    `xml:"tag"`
		ESP     string    `xml:"esp"`
		KeyBit  string    `xml:"key-bit"`
		ESPHMAC string    `xml:"esp-hmac"`
		AHHMAC  string    `xml:"ah-hmac"`
		Tunnel  *struct{} `xml:"mode>tunnel"`
		Transp  *struct{} `xml:"mode>transport"`
	} `xml:"crypto>ipsec>transform-set"`
}

func (n nativeCrypto) rows() int {
	return len(n.Trustpoints) + len(n.IKEv2Proposals) + len(n.TransformSets)
}

// nativeControlPlane is Cisco-IOS-XE-native:native filtered to
// control-plane: the control-plane policing service policies
// (Cisco-IOS-XE-policy).
type nativeControlPlane struct {
	XMLName xml.Name `xml:"http://cisco.com/ns/yang/Cisco-IOS-XE-native native"`
	Input   string   `xml:"control-plane>service-policy>input"`
	Output  string   `xml:"control-plane>service-policy>output"`
}

func (n nativeControlPlane) rows() int {
	if strings.TrimSpace(n.Input) == "" && strings.TrimSpace(n.Output) == "" {
		return 0
	}
	return 1
}

// pkiOper is Cisco-IOS-XE-crypto-pki-oper:crypto-pki-oper-data: the
// installed certificates per trustpoint. Session counters are not
// decoded.
type pkiOper struct {
	XMLName xml.Name `xml:"http://cisco.com/ns/yang/Cisco-IOS-XE-crypto-pki-oper crypto-pki-oper-data"`
	Bundles []struct {
		Label    string `xml:"label"`
		Enrolled string `xml:"tp-enrolled"`
		Certs    []struct {
			Avail         string `xml:"cert-avail"`
			Usage         string `xml:"cert-usage"`
			KeyType       string `xml:"cert-key-type"`
			SerialNumber  string `xml:"serial-number"`
			SubjectName   string `xml:"subject-name"`
			IssuerName    string `xml:"issuer-name"`
			ValidityStart string `xml:"validity-start"`
			ValidityEnd   string `xml:"validity-end"`
		} `xml:"cert"`
	} `xml:"crypto-pki-bundle"`
}

func (p pkiOper) rows() int {
	c := 0
	for _, b := range p.Bundles {
		c += len(b.Certs)
	}
	return c
}

// transceiverOper is Cisco-IOS-XE-transceiver-oper:transceiver-oper-data,
// keyed by name. Digital optical monitoring readings are volatile and
// not decoded.
type transceiverOper struct {
	XMLName      xml.Name `xml:"http://cisco.com/ns/yang/Cisco-IOS-XE-transceiver-oper transceiver-oper-data"`
	Transceivers []struct {
		Name        string `xml:"name"`
		Present     string `xml:"present"`
		Identifier  string `xml:"identifier"`
		Connector   string `xml:"connector"`
		EthernetPMD string `xml:"ethernet-pmd"`
		Vendor      string `xml:"vendor"`
		VendorPart  string `xml:"vendor-part"`
		VendorRev   string `xml:"vendor-rev"`
		SerialNo    string `xml:"serial-no"`
	} `xml:"transceiver"`
}

func (t transceiverOper) rows() int { return len(t.Transceivers) }
