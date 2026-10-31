// transform_security_test.go - Tests for the audit-purpose security
// posture transforms and their DTOs in the Cisco IOS-XE OSIRIS JSON
// Producer.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package iosxe

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"strings"
	"testing"
)

// securityCases are the synthetic device replies the security unit
// tests decode: one minimal NETCONF <data> payload per case, keyed by
// the behavior it exercises.
var securityCases = map[string]string{
	"aaa-method-lists": `<native xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-native"><aaa>
  <authentication>
    <login><name>default</name><a1><group>LAB-TACACS</group></a1><a2><local/></a2></login>
    <enable><default><a1><group>LAB-TACACS</group></a1><a2><enable/></a2></default></enable>
    <dot1x/>
  </authentication>
  <accounting>
    <exec><name>LAB-EXEC</name><stop-only><broadcast/><group1><group>LAB-RADIUS</group></group1></stop-only></exec>
    <exec><name>LAB-NONE</name><none/></exec>
  </accounting>
</aaa></native>`,
	"acl-bound-interfaces": `<native xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-native"><interface>
  <GigabitEthernet><name>1</name><ip><access-group><out><acl><acl-name>10</acl-name><out/></acl></out></access-group></ip></GigabitEthernet>
</interface></native>`,
	"acls": `<native xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-native">
  <ip><access-list><standard><name>10</name><access-list-seq-rule><sequence>10</sequence><permit><std-ace><any/></std-ace></permit></access-list-seq-rule></standard></access-list></ip>
  <ipv6><access-list><named-acl><name>10</name></named-acl></access-list></ipv6>
</native>`,
	"certificates": `<crypto-pki-oper-data xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-crypto-pki-oper">
  <crypto-pki-bundle><label>LAB-TP</label>
    <cert><cert-avail>crypto-pki-cert-not-available</cert-avail><cert-usage>crypto-pki-cert-usage-unset</cert-usage><cert-key-type>crypto-pki-cert-key-none</cert-key-type><subject-name>cn=LAB</subject-name><validity-end>not-a-time</validity-end></cert>
    <cert/>
  </crypto-pki-bundle>
  <crypto-pki-bundle><cert><serial-number>01</serial-number></cert></crypto-pki-bundle>
</crypto-pki-oper-data>`,
	"local-accounts": `<native xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-native">
  <username><name>admin</name><privilege>15</privilege><secret><encryption>9</encryption><secret>changeme</secret></secret></username>
  <username><name>test</name><password><password>changeme</password></password></username>
  <username><name>LAB-NOPASS</name></username>
  <enable><password><password>changeme</password></password></enable>
</native>`,
	"management-vty": `<native xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-native"><line>
  <vty><first>0</first><last>4</last><transport><input><all/></input></transport></vty>
  <vty><first>5</first><transport><input><ssh>true</ssh></input></transport><exec-timeout><minutes>5</minutes></exec-timeout></vty>
</line></native>`,
	"ntp-keys": `<native xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-native"><ntp>
  <authentication-key><number>7</number><md5-cfg>changeme</md5-cfg></authentication-key>
  <peer><server-list><ip-address>192.0.2.124</ip-address><key>7</key></server-list></peer>
</ntp></native>`,
	"snmp-communities": `<native xmlns="http://cisco.com/ns/yang/Cisco-IOS-XE-native"><snmp-server>
  <community-config><name>changeme</name></community-config>
  <community><name>changeme</name><RW/></community>
  <host-config><ip-community><ip-address>192.0.2.60</ip-address><community-or-user>changeme</community-or-user><version>2c</version></ip-community>
    <ip-community-port><ip-address>192.0.2.60</ip-address><community-or-user>changeme</community-or-user><udp-port>1162</udp-port><version>2c</version></ip-community-port></host-config>
</snmp-server></native>`,
}

// TestModels_DecodeEveryDTO decode the bare root element of every model
// this producer queries. encoding/xml reports conflicting field tags
// only when a type is first decoded, so a DTO with such a conflict
// would otherwise fail on the first device that returns data.
func TestModels_DecodeEveryDTO(t *testing.T) {
	nativeRoot := `<native xmlns="` + nsNative + `"><hostname>LAB-RTR01</hostname></native>`
	cases := []struct {
		name   string
		decode func([]byte) error
		root   string
	}{
		{"nativeSystem", decodeErr[nativeSystem], nativeRoot},
		{"nativeBoot", decodeErr[nativeBoot], nativeRoot},
		{"nativeVRFList", decodeErr[nativeVRFList], nativeRoot},
		{"nativeInterfaces", decodeErr[nativeInterfaces], nativeRoot},
		{"nativeRoutes", decodeErr[nativeRoutes], nativeRoot},
		{"nativeAAA", decodeErr[nativeAAA], nativeRoot},
		{"nativeAccounts", decodeErr[nativeAccounts], nativeRoot},
		{"nativeManagement", decodeErr[nativeManagement], nativeRoot},
		{"nativeDNS", decodeErr[nativeDNS], nativeRoot},
		{"nativeSNMP", decodeErr[nativeSNMP], nativeRoot},
		{"nativeLogging", decodeErr[nativeLogging], nativeRoot},
		{"nativeNTP", decodeErr[nativeNTP], nativeRoot},
		{"nativeACLs", decodeErr[nativeACLs], nativeRoot},
		{"nativeCrypto", decodeErr[nativeCrypto], nativeRoot},
		{"nativeControlPlane", decodeErr[nativeControlPlane], nativeRoot},
		{"ietfInterfaces", decodeErr[ietfInterfaces], `<interfaces xmlns="` + nsIETFInterface + `"/>`},
		{"interfacesOper", decodeErr[interfacesOper], `<interfaces xmlns="` + nsInterfaceOper + `"/>`},
		{"hwInventory", decodeErr[hwInventory], `<device-hardware-data xmlns="` + nsHardwareOper + `"/>`},
		{"cdpNeighborDetails", decodeErr[cdpNeighborDetails], `<cdp-neighbor-details xmlns="` + nsCDPOper + `"/>`},
		{"lldpEntries", decodeErr[lldpEntries], `<lldp-entries xmlns="` + nsLLDPOper + `"/>`},
		{"vlansOper", decodeErr[vlansOper], `<vlans xmlns="` + nsVLANOper + `"/>`},
		{"bgpStateData", decodeErr[bgpStateData], `<bgp-state-data xmlns="` + nsBGPOper + `"/>`},
		{"ospfOperData", decodeErr[ospfOperData], `<ospf-oper-data xmlns="` + nsOSPFOper + `"/>`},
		{"transceiverOper", decodeErr[transceiverOper], `<transceiver-oper-data xmlns="` + nsTransceiverOper + `"/>`},
		{"pkiOper", decodeErr[pkiOper], `<crypto-pki-oper-data xmlns="` + nsCryptoPKIOper + `"/>`},
	}
	for _, tc := range cases {
		if err := tc.decode([]byte(tc.root)); err != nil {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
}

func decodeErr[T model](data []byte) error {
	_, err := decodeModel[T](data)
	if errors.Is(err, errEmptyModel) {
		return nil
	}
	return err
}

func TestMethodLists(t *testing.T) {
	aaa := mustDecode[nativeAAA](t, unitXML(t, securityCases, "aaa-method-lists"))
	got := TransformAAA(aaa)
	want := `[{"methods":["group LAB-TACACS","local"],"name":"default","service":"login","type":"authentication"},` +
		`{"methods":["group LAB-TACACS","enable"],"name":"default","service":"enable","type":"authentication"},` +
		`{"methods":["broadcast","group LAB-RADIUS"],"name":"LAB-EXEC","record_type":"stop-only","service":"exec","type":"accounting"},` +
		`{"methods":["none"],"name":"LAB-NONE","service":"exec","type":"accounting"}]`
	if s := mustJSON(t, got["method_lists"]); s != want {
		t.Errorf("method_lists =\n%s\nwant\n%s", s, want)
	}
	if _, ok := got["new_model"]; ok {
		t.Error("new_model must be set only when aaa new-model is configured")
	}
}

func TestTransformAccounts(t *testing.T) {
	a := mustDecode[nativeAccounts](t, unitXML(t, securityCases, "local-accounts"))
	users, enable := TransformAccounts(a)
	if s := mustJSON(t, users); s != `[{"name":"admin","privilege":15,"storage":"secret-type-9"},{"name":"test","storage":"password"},{"name":"LAB-NOPASS"}]` {
		t.Errorf("local accounts = %s", s)
	}
	if s := mustJSON(t, enable); s != `{"storage":"password"}` {
		t.Errorf("enable = %s", s)
	}
	if strings.Contains(mustJSON(t, users)+mustJSON(t, enable), "changeme") {
		t.Error("credential text must never be emitted")
	}
}

func TestTransformSNMP_NeverEmitsCommunities(t *testing.T) {
	n := mustDecode[nativeSNMP](t, unitXML(t, securityCases, "snmp-communities"))
	got := mustJSON(t, TransformSNMP(n))
	if strings.Contains(got, "changeme") {
		t.Fatalf("community string emitted: %s", got)
	}
	if got != `{"communities":[{"permission":"ro"},{"permission":"rw"}],"hosts":[{"address":"192.0.2.60","version":"2c"}]}` {
		t.Errorf("snmp = %s", got)
	}
}

func TestTransformNTP_KeyNumbersOnly(t *testing.T) {
	n := mustDecode[nativeNTP](t, unitXML(t, securityCases, "ntp-keys"))
	got := mustJSON(t, TransformNTP(n))
	if got != `{"key_ids":[7],"peers":[{"address":"192.0.2.124","key_id":7}]}` {
		t.Errorf("ntp = %s", got)
	}
}

func TestACLFormatting(t *testing.T) {
	cases := []struct {
		got, want string
	}{
		{aclAddress(true, "", "", ""), "any"},
		{aclAddress(false, "192.0.2.1", "", ""), "host 192.0.2.1"},
		{aclAddress(false, "", "192.0.2.0", "0.0.0.255"), "192.0.2.0 0.0.0.255"},
		{aclAddress(false, "", "192.0.2.0", ""), "192.0.2.0"},
		{aclPorts("dst", []string{"22"}, "", "", "", "", "", []xmlNode{{XMLName: xmlName("dst-eq-port2"), Text: "443"}, {XMLName: xmlName("src-eq-port2"), Text: "80"}}), "eq 22 443"},
		{aclPorts("src", nil, "", "", "", "1024", "2048", nil), "range 1024 2048"},
		{aclPorts("src", nil, "1023", "", "", "", "", nil), "gt 1023"},
		{aclPorts("src", nil, "", "", "", "", "", nil), ""},
	}
	for i, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("case %d: %q, want %q", i, tc.got, tc.want)
		}
	}
}

func TestTransformACLs_BindingsAndIDs(t *testing.T) {
	acls := mustDecode[nativeACLs](t, unitXML(t, securityCases, "acls"))
	ifs := mustDecode[nativeInterfaces](t, unitXML(t, securityCases, "acl-bound-interfaces"))
	rs := TransformACLs("TST0000RTR01", acls, ifs, nativeManagement{})
	if len(rs) != 2 {
		t.Fatalf("got %d ACL resources, want 2 (same name, two families)", len(rs))
	}
	if rs[0].ID != "cisco.iosxe::TST0000RTR01/acl/ipv4/10" || rs[1].ID != "cisco.iosxe::TST0000RTR01/acl/ipv6/10" {
		t.Errorf("IDs = %s, %s", rs[0].ID, rs[1].ID)
	}
	if s := mustJSON(t, rs[0].Properties["bindings"]); s != `[{"direction":"out","interface":"GigabitEthernet1"}]` {
		t.Errorf("ipv4 bindings = %s", s)
	}
	if _, ok := rs[1].Properties["bindings"]; ok {
		t.Error("an IPv4 binding must not attach to the IPv6 list of the same name")
	}
	if rs[1].Properties["rule_count"] != 0 {
		t.Errorf("empty list rule_count = %v", rs[1].Properties["rule_count"])
	}
}

func TestTransformCertificates(t *testing.T) {
	p := mustDecode[pkiOper](t, unitXML(t, securityCases, "certificates"))
	rs := TransformCertificates("TST0000RTR01", p)
	if len(rs) != 1 {
		t.Fatalf("got %d certificates, want 1 (empty cert and unlabeled bundle skipped)", len(rs))
	}
	r := rs[0]
	if r.ID != "cisco.iosxe::TST0000RTR01/certificate/LAB-TP/0" || r.Status != "inactive" {
		t.Errorf("certificate = %s %s", r.ID, r.Status)
	}
	if s := mustJSON(t, r.Properties); s != `{"subject":"cn=LAB","trustpoint":"LAB-TP","valid_to":"not-a-time"}` {
		t.Errorf("properties = %s", s)
	}
}

func TestTransformManagement_VTY(t *testing.T) {
	m := mustDecode[nativeManagement](t, unitXML(t, securityCases, "management-vty"))
	got := mustJSON(t, TransformManagement(m))
	if got != `{"vty_lines":[{"first":0,"last":4,"transport_input":["all"]},{"exec_timeout_seconds":300,"first":5,"transport_input":["ssh"]}]}` {
		t.Errorf("management = %s", got)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func xmlName(local string) xml.Name { return xml.Name{Local: local} }
