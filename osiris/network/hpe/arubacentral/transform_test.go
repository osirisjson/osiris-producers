// transform_test.go - Unit tests for the HPE Aruba Networking Central
// OSIRIS JSON transforms.
//
// OSIRIS JSON Producer for HPE Aruba Networking Central introduction:
// [OSIRIS-JSON-HPE-ARUBA-NETWORKING]: https://docs.osirisjson.org/osiris-producers/network/hpe-aruba-networking
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package arubacentral

import (
	"testing"

	"go.osirisjson.org/producers/pkg/sdk"
)

// newTestResource builds a minimal, valid resource of the given type
// for tests that only need a well-formed resource to mutate
// (e.g. enrichment tests).
func newTestResource(resourceType string) (sdk.Resource, error) {
	return sdk.NewResource(resourceID("SERIAL-EXAMPLE-TEST-"+resourceType), resourceType, sdk.Provider{Name: providerName})
}

func TestTransformSites(t *testing.T) {
	sites := []Site{
		{ScopeID: "100000000000000001", ScopeName: "example-campus-1", City: "Rome", Country: "IT", DeviceCount: 12},
		{ScopeID: "", ScopeName: "no-scope-id-skipped"},
	}

	resources, nameToID := TransformSites(sites)
	if len(resources) != 1 {
		t.Fatalf("expected 1 site resource (missing scope ID skipped), got %d", len(resources))
	}
	r := resources[0]
	if r.Type != "osiris.hpe.arubacentral.site" {
		t.Errorf("expected type osiris.hpe.arubacentral.site, got %q", r.Type)
	}
	if r.Name != "example-campus-1" {
		t.Errorf("unexpected name: %q", r.Name)
	}
	if r.Properties["device_count"] != 12 {
		t.Errorf("expected device_count 12, got %v", r.Properties["device_count"])
	}
	if nameToID["example-campus-1"] != r.ID {
		t.Errorf("nameToID map does not point at the resource ID")
	}
}

func TestEnrichSiteHealth(t *testing.T) {
	r, err := newTestResource("osiris.hpe.arubacentral.site")
	if err != nil {
		t.Fatal(err)
	}
	r.Status = "active"
	health := SiteHealth{SiteHealth: "Poor", DeviceHealth: "Poor", ClientHealth: "Good"}

	EnrichSiteHealth(&r, health, "documentation")
	if r.Status != "degraded" {
		t.Errorf("expected status degraded from Poor site health, got %q", r.Status)
	}
	if r.Properties["health_status"] != "Poor" {
		t.Errorf("expected health_status Poor, got %v", r.Properties["health_status"])
	}
	if _, present := r.Properties["device_health"]; present {
		t.Error("expected device_health to be audit-only, not present in documentation purpose")
	}

	r2, _ := newTestResource("osiris.hpe.arubacentral.site")
	r2.Status = "active"
	EnrichSiteHealth(&r2, health, "audit")
	if r2.Properties["device_health"] != "Poor" || r2.Properties["client_health"] != "Good" {
		t.Errorf("expected device_health/client_health in audit purpose, got %+v", r2.Properties)
	}
}

func TestEnrichSiteDeviceHealth(t *testing.T) {
	deviceHealth := SiteDeviceHealth{APHealth: "Fair", SwitchHealth: "Poor", GatewayHealth: "Good", BridgeHealth: "Good"}

	rDoc, _ := newTestResource("osiris.hpe.arubacentral.site")
	EnrichSiteDeviceHealth(&rDoc, deviceHealth, "documentation")
	if len(rDoc.Properties) != 0 {
		t.Errorf("expected no properties set in documentation purpose, got %+v", rDoc.Properties)
	}

	rAudit, _ := newTestResource("osiris.hpe.arubacentral.site")
	EnrichSiteDeviceHealth(&rAudit, deviceHealth, "audit")
	if rAudit.Properties["ap_health"] != "Fair" || rAudit.Properties["switch_health"] != "Poor" ||
		rAudit.Properties["gateway_health"] != "Good" || rAudit.Properties["bridge_health"] != "Good" {
		t.Errorf("expected all four health breakdown fields in audit purpose, got %+v", rAudit.Properties)
	}
}

func TestTransformDeviceGroups(t *testing.T) {
	groups := []DeviceGroup{
		{ID: "example-switch-group", ScopeID: "1", DeviceCount: 5, Type: "switch"},
	}
	result, nameToID := TransformDeviceGroups(groups)
	if len(result) != 1 {
		t.Fatalf("expected 1 group, got %d", len(result))
	}
	if result[0].Type != "logical.devicegroup" {
		t.Errorf("expected group type logical.devicegroup, got %q", result[0].Type)
	}
	if nameToID["example-switch-group"] != result[0].ID {
		t.Errorf("nameToID does not resolve to the group ID")
	}
}

func TestWireDeviceToGroup(t *testing.T) {
	groups := []DeviceGroup{{ID: "example-switch-group", ScopeID: "1"}}
	groupResources, nameToID := TransformDeviceGroups(groups)
	idx := groupIndex(groupResources)

	WireDeviceToGroup(groupResources, idx, nameToID, "example-switch-group", "hpe.arubacentral::SERIAL-EXAMPLE-0001")
	if len(groupResources[0].Members) != 1 || groupResources[0].Members[0] != "hpe.arubacentral::SERIAL-EXAMPLE-0001" {
		t.Fatalf("expected device wired into group members, got %v", groupResources[0].Members)
	}

	// Unknown group name is a no-op, not an error.
	WireDeviceToGroup(groupResources, idx, nameToID, "unknown-group", "hpe.arubacentral::SERIAL-EXAMPLE-0002")
	if len(groupResources[0].Members) != 1 {
		t.Fatalf("unknown group name should not mutate members, got %v", groupResources[0].Members)
	}
}

func TestTransformSwitches(t *testing.T) {
	switches := []Switch{
		{
			SerialNumber: "SERIAL-EXAMPLE-0001", DeviceName: "switch-example-01", Status: "Up",
			Model: "6300F-JL658A", FirmwareVersion: "10.13.1000", SiteName: "example-campus-1",
			IPv4: "192.0.2.10", MACAddress: "00:00:5E:00:53:00",
			SwitchTrends: []SwitchTrend{{CPUUtilization: 12.5, MemoryUtilization: 40, PowerConsumption: 55.5}},
		},
		{SerialNumber: "", DeviceName: "skipped-missing-serial"},
	}

	resources, idMap := TransformSwitches(switches)
	if len(resources) != 1 {
		t.Fatalf("expected 1 switch resource, got %d", len(resources))
	}
	r := resources[0]
	if r.Type != "network.switch" {
		t.Errorf("expected type network.switch, got %q", r.Type)
	}
	if r.Status != "active" {
		t.Errorf("expected status active for Up switch, got %q", r.Status)
	}
	if r.Provider.Name != providerName {
		t.Errorf("expected provider name %q, got %q", providerName, r.Provider.Name)
	}
	if r.Provider.Source != providerSource {
		t.Errorf("expected provider source %q, got %q", providerSource, r.Provider.Source)
	}
	if r.Properties["cpu_utilization_pct"] != 12.5 {
		t.Errorf("expected cpu_utilization_pct 12.5, got %v", r.Properties["cpu_utilization_pct"])
	}
	if idMap["SERIAL-EXAMPLE-0001"] != r.ID {
		t.Errorf("idMap does not resolve to the resource ID")
	}
}

func TestTransformSwitchInterfacesVLANsAndLAGs(t *testing.T) {
	switchID := resourceID("SERIAL-EXAMPLE-0001")
	switchIDMap := map[string]string{"SERIAL-EXAMPLE-0001": switchID}
	ifaces := []SwitchInterface{
		{Name: "1/1/1", Status: "Up", NativeVlan: 10, SerialNumber: "SERIAL-EXAMPLE-0001"},
		{Name: "1/1/2", Status: "Down", SerialNumber: "SERIAL-EXAMPLE-0001"},
	}

	ifRes, ifConns, ifNameToID := TransformSwitchInterfaces(switchIDMap, "SERIAL-EXAMPLE-0001", ifaces)
	if len(ifRes) != 2 {
		t.Fatalf("expected 2 interface resources, got %d", len(ifRes))
	}
	if len(ifConns) != 2 {
		t.Fatalf("expected 2 contains connections, got %d", len(ifConns))
	}
	for _, c := range ifConns {
		if c.Type != "contains" || c.Source != switchID {
			t.Errorf("unexpected connection: %+v", c)
		}
	}

	vlans := []SwitchVLAN{
		{ID: "10", Name: "example-users", TaggedPorts: []string{"1/1/1"}, UntaggedPorts: []string{"1/1/2"}},
	}
	vlanGroups := TransformSwitchVLANs("SERIAL-EXAMPLE-0001", vlans, ifNameToID)
	if len(vlanGroups) != 1 {
		t.Fatalf("expected 1 VLAN group, got %d", len(vlanGroups))
	}
	if len(vlanGroups[0].Members) != 2 {
		t.Errorf("expected 2 VLAN members (tagged+untagged ports resolved), got %v", vlanGroups[0].Members)
	}

	lags := []SwitchLAG{{ID: "1", Name: "lag1", Ports: []string{"1/1/1", "1/1/2"}}}
	lagGroups := TransformSwitchLAGs("SERIAL-EXAMPLE-0001", lags, ifNameToID)
	if len(lagGroups) != 1 || len(lagGroups[0].Members) != 2 {
		t.Fatalf("expected 1 LAG group with 2 members, got %+v", lagGroups)
	}
}

func TestEnrichSwitchHardware_DegradesStatus(t *testing.T) {
	r, err := newTestResource("network.switch")
	if err != nil {
		t.Fatal(err)
	}
	r.Status = "active"

	hw := SwitchHardware{
		CPU:           SwitchHealthStatus{Health: "Good"},
		Fans:          SwitchComponentSet{Health: "Poor", TotalCount: 2, UpCount: 1},
		PowerSupplies: SwitchComponentSet{Health: "Good", TotalCount: 2, UpCount: 2},
	}
	EnrichSwitchHardware(&r, hw)

	if r.Status != "degraded" {
		t.Errorf("expected status degraded due to unhealthy fan, got %q", r.Status)
	}
	hwProps, ok := r.Properties["hardware"].(map[string]any)
	if !ok {
		t.Fatalf("expected hardware properties map, got %v", r.Properties["hardware"])
	}
	if hwProps["fans_up_count"] != 1 {
		t.Errorf("expected fans_up_count 1, got %v", hwProps["fans_up_count"])
	}
}

func TestTransformSwitchVSX_CreatesStubForUnknownPeer(t *testing.T) {
	switchID := resourceID("SERIAL-EXAMPLE-0001")
	vsx := &SwitchVSX{Role: "primary", PeerRole: "secondary", VSXPeerSerial: "SERIAL-EXAMPLE-0002", VSXPeerName: "switch-example-02"}

	conn, stub := TransformSwitchVSX(switchID, vsx, map[string]string{})
	if conn == nil {
		t.Fatal("expected a connection")
	}
	if conn.Type != vsxConnectionType {
		t.Errorf("expected connection type %q, got %q", vsxConnectionType, conn.Type)
	}
	if stub == nil {
		t.Fatal("expected a stub resource for the unresolved peer")
	}
	if stub.Status != "unknown" {
		t.Errorf("expected stub status unknown, got %q", stub.Status)
	}

	// When the peer is already known, no stub should be created.
	knownMap := map[string]string{"SERIAL-EXAMPLE-0002": resourceID("SERIAL-EXAMPLE-0002")}
	conn2, stub2 := TransformSwitchVSX(switchID, vsx, knownMap)
	if conn2 == nil {
		t.Fatal("expected a connection")
	}
	if stub2 != nil {
		t.Errorf("expected no stub when peer is already known, got %+v", stub2)
	}
}

func TestTransformAccessPointsAndWireless(t *testing.T) {
	aps := []AccessPoint{
		{SerialNumber: "SERIAL-EXAMPLE-0010", DeviceName: "ap-example-01", Status: "Up", SiteName: "example-campus-1", ClientCount: 3},
	}
	apResources, apIDMap := TransformAccessPoints(aps)
	if len(apResources) != 1 {
		t.Fatalf("expected 1 AP resource, got %d", len(apResources))
	}
	if apResources[0].Type != "osiris.hpe.arubacentral.accesspoint" {
		t.Errorf("expected type osiris.hpe.arubacentral.accesspoint, got %q", apResources[0].Type)
	}

	wlans := []APWLAN{{WLANName: "ssid-example-corp", Security: "wpa3", Status: "Up"}}
	wlanResources, wlanNameToID := TransformWLANs(wlans)
	if len(wlanResources) != 1 || wlanResources[0].Type != "osiris.hpe.arubacentral.wlan" {
		t.Fatalf("expected 1 osiris.hpe.arubacentral.wlan resource, got %+v", wlanResources)
	}

	radios := []Radio{{SerialNumber: "SERIAL-EXAMPLE-0010", RadioNumber: 0, Band: "5GHz", Status: "Up"}}
	radioResources, radioConns, radioIDMap := TransformRadios(radios, apIDMap)
	if len(radioResources) != 1 {
		t.Fatalf("expected 1 radio resource, got %d", len(radioResources))
	}
	if len(radioConns) != 1 || radioConns[0].Type != "contains" {
		t.Fatalf("expected 1 contains connection AP->radio, got %+v", radioConns)
	}

	bssids := []BSSID{{BSSID: "00:00:5E:00:53:01", SerialNumber: "SERIAL-EXAMPLE-0010", RadioNumber: 0, WLANName: "ssid-example-corp"}}
	bssidResources, bssidConns := TransformBSSIDs(bssids, radioIDMap, wlanNameToID)
	if len(bssidResources) != 1 {
		t.Fatalf("expected 1 BSSID resource, got %d", len(bssidResources))
	}
	if len(bssidConns) != 2 {
		t.Fatalf("expected 2 connections (radio->bssid contains, bssid->wlan network), got %d", len(bssidConns))
	}

	apWLANConns := TransformAPWLANConnections(apIDMap["SERIAL-EXAMPLE-0010"], []APWLAN{{WLANName: "ssid-example-corp", Band: "5GHz", Status: "Up"}}, wlanNameToID)
	if len(apWLANConns) != 1 {
		t.Fatalf("expected 1 AP->WLAN broadcast connection, got %d", len(apWLANConns))
	}
}

func TestTransformSwarms(t *testing.T) {
	apIDMap := map[string]string{"SERIAL-EXAMPLE-0010": resourceID("SERIAL-EXAMPLE-0010")}
	swarms := []Swarm{{ClusterID: "cluster-1", ClusterName: "example-mesh", ConductorSerialNumber: "SERIAL-EXAMPLE-0010"}}
	groups := TransformSwarms(swarms, apIDMap)
	if len(groups) != 1 {
		t.Fatalf("expected 1 swarm group, got %d", len(groups))
	}
	if len(groups[0].Members) != 1 {
		t.Errorf("expected conductor AP wired as member, got %v", groups[0].Members)
	}
}

func TestTransformGatewaysAndUplinks(t *testing.T) {
	gateways := []Gateway{{SerialNumber: "SERIAL-EXAMPLE-0020", DeviceName: "gateway-example-01", Status: "Up", IPAddress: "203.0.113.5"}}
	gwResources, gwIDMap := TransformGateways(gateways)
	if len(gwResources) != 1 || gwResources[0].Type != "network.gateway" {
		t.Fatalf("expected 1 network.gateway resource, got %+v", gwResources)
	}

	uplinks := []GatewayUplink{{Name: "wan1", Status: "Up"}}
	conns, stubs := TransformGatewayUplinks(gwIDMap["SERIAL-EXAMPLE-0020"], "SERIAL-EXAMPLE-0020", uplinks)
	if len(conns) != 1 || conns[0].Type != "osiris.hpe.arubacentral.uplink" {
		t.Fatalf("expected 1 osiris.hpe.arubacentral.uplink connection, got %+v", conns)
	}
	if len(stubs) != 1 {
		t.Fatalf("expected 1 stub WAN endpoint resource, got %d", len(stubs))
	}
}

func TestTransformClients(t *testing.T) {
	deviceIDMap := map[string]string{"SERIAL-EXAMPLE-0001": resourceID("SERIAL-EXAMPLE-0001")}
	clients := []ClientDevice{
		{MACAddress: "00:00:5E:00:53:02", ClientName: "client-example-01", Status: "Up", ConnectedDeviceSerial: "SERIAL-EXAMPLE-0001", Port: "1/1/1"},
		{MACAddress: "not-a-mac", ClientName: "skipped-invalid-mac"},
	}
	resources, conns := TransformClients(clients, deviceIDMap, "documentation")
	if len(resources) != 1 {
		t.Fatalf("expected 1 client resource (invalid MAC skipped), got %d", len(resources))
	}
	if resources[0].Type != "osiris.hpe.arubacentral.client" {
		t.Errorf("expected type osiris.hpe.arubacentral.client, got %q", resources[0].Type)
	}
	if len(conns) != 1 || conns[0].Type != "network" {
		t.Fatalf("expected 1 network connection to the connected device, got %+v", conns)
	}
}

// TestTransformClients_IdentifyingFieldsAreAuditOnly guards the
// documentation/audit purpose split: fields that fingerprint the
// specific person/device (host name, user name, OS, manufacturer,
// auth type, connection timestamp) must only appear under
// --purpose audit flag.
func TestTransformClients_IdentifyingFieldsAreAuditOnly(t *testing.T) {
	clients := []ClientDevice{{
		MACAddress: "00:00:5E:00:53:03", Status: "Up",
		HostName: "host-example-01", UserName: "user-example-01",
		ClientFunction: "employee", ClientManufacturer: "Example Corp",
		ClientOperatingSystem: "ExampleOS 1.0", AuthenticationType: "802.1X",
		ConnectedAt: "2026-07-06T00:00:00Z",
	}}

	docResources, _ := TransformClients(clients, nil, "documentation")
	if len(docResources) != 1 {
		t.Fatalf("expected 1 client resource, got %d", len(docResources))
	}
	for _, key := range []string{"host_name", "user_name", "client_function", "client_manufacturer", "client_operating_system", "security_type", "connected_at"} {
		if _, present := docResources[0].Properties[key]; present {
			t.Errorf("documentation purpose must not include identifying field %q, got %v", key, docResources[0].Properties[key])
		}
	}

	auditResources, _ := TransformClients(clients, nil, "audit")
	for _, key := range []string{"host_name", "user_name", "client_function", "client_manufacturer", "client_operating_system", "security_type", "connected_at"} {
		if _, present := auditResources[0].Properties[key]; !present {
			t.Errorf("audit purpose must include identifying field %q, got %+v", key, auditResources[0].Properties)
		}
	}
}

func TestTransformNeighbors_CreatesStubForUnknownDevice(t *testing.T) {
	deviceID := resourceID("SERIAL-EXAMPLE-0001")
	neighbors := []Neighbor{{RemoteSerial: "SERIAL-EXAMPLE-0099", LocalPort: "1/1/1", ToPort: "1/1/2", Health: "Good"}}

	conns, stubs := TransformNeighbors(deviceID, "SERIAL-EXAMPLE-0001", neighbors, map[string]string{})
	if len(conns) != 1 {
		t.Fatalf("expected 1 neighbor connection, got %d", len(conns))
	}
	if conns[0].Status != "active" {
		t.Errorf("expected status active for Good health, got %q", conns[0].Status)
	}
	if len(stubs) != 1 {
		t.Fatalf("expected 1 stub resource for the unresolved neighbor, got %d", len(stubs))
	}
}

func TestNeighborConnection_IsBidirectionallyDeterministic(t *testing.T) {
	deviceA := resourceID("SERIAL-EXAMPLE-0001")
	deviceB := resourceID("SERIAL-EXAMPLE-0002")
	known := map[string]string{"SERIAL-EXAMPLE-0001": deviceA, "SERIAL-EXAMPLE-0002": deviceB}

	fromA, _ := TransformNeighbors(deviceA, "SERIAL-EXAMPLE-0001", []Neighbor{{RemoteSerial: "SERIAL-EXAMPLE-0002", LocalPort: "1/1/1", ToPort: "1/1/2"}}, known)
	fromB, _ := TransformNeighbors(deviceB, "SERIAL-EXAMPLE-0002", []Neighbor{{RemoteSerial: "SERIAL-EXAMPLE-0001", LocalPort: "1/1/2", ToPort: "1/1/1"}}, known)

	if len(fromA) != 1 || len(fromB) != 1 {
		t.Fatalf("expected exactly 1 connection from each side")
	}
	if fromA[0].ID != fromB[0].ID {
		t.Errorf("expected the same bidirectional connection ID from both sides: %q vs %q", fromA[0].ID, fromB[0].ID)
	}

	deduped := dedupeConnections(append(append([]sdk.Connection{}, fromA...), fromB...))
	if len(deduped) != 1 {
		t.Errorf("expected dedupeConnections to collapse the two reports into 1, got %d", len(deduped))
	}
}

// TestTransformNeighbors_SkipsClientAndStackTypes guards an API bug
// /neighbours/{serial} returns a mix of neighbor types Switch, Access
// Point, Gateway, Unmanaged (genuine device adjacency) alongside Client
// and Stack (already modeled elsewhere as osiris.hpe.arubacentral.client
// connections and osiris.hpe.arubacentral.stack groups respectively).
// Without filtering, Client/Stack entries produced duplicate/bogus stub
// resources instead of being skipped.
func TestTransformNeighbors_SkipsClientAndStackTypes(t *testing.T) {
	deviceID := resourceID("SERIAL-EXAMPLE-0001")
	neighbors := []Neighbor{
		{Type: "Client", RemoteSerial: "00:00:5E:00:53:04", LocalPort: "1/1/1"},
		{Type: "Stack", RemoteSerial: "stack-uuid-example", LocalPort: "mgmt"},
		{Type: "Switch", RemoteSerial: "SERIAL-EXAMPLE-0099", LocalPort: "1/1/2", ToPort: "1/1/3"},
		{Type: "Unmanaged", RemoteSerial: "tpd_example0001", LocalPort: "1/1/4", ToPort: "Ethernet1/1", Name: "example-unmanaged-switch"},
	}

	conns, stubs := TransformNeighbors(deviceID, "SERIAL-EXAMPLE-0001", neighbors, map[string]string{})
	if len(conns) != 2 {
		t.Fatalf("expected 2 connections (Client/Stack skipped), got %d", len(conns))
	}
	if len(stubs) != 2 {
		t.Fatalf("expected 2 stub resources (Client/Stack skipped), got %d", len(stubs))
	}
	for _, c := range conns {
		if c.Target == resourceID("00:00:5E:00:53:05") || c.Target == resourceID("stack-uuid-example") {
			t.Errorf("Client/Stack neighbor types must be skipped, got connection to %q", c.Target)
		}
	}
}

func TestDedupeResources(t *testing.T) {
	r1, _ := newTestResource("network.switch")
	r2 := r1 // same ID, simulating the same stub reported twice
	r3, _ := newTestResource("osiris.hpe.arubacentral.accesspoint")

	deduped := dedupeResources([]sdk.Resource{r1, r2, r3})
	if len(deduped) != 2 {
		t.Errorf("expected 2 unique resources, got %d", len(deduped))
	}
}
