// transform_test.go - Unit tests for Device->OSIRIS JSON
// resource mapping.
//
// OSIRIS JSON Producer for Cisco SD-WAN Manager (vManage) introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package vmanage

import "testing"

func TestTransformDevices_TypeMapping(t *testing.T) {
	devices := []Device{
		{UUID: "11111111-1111-1111-1111-111111111111", HostName: "TEST-VMANAGE1", SiteID: "100", Personality: "vmanage", DeviceModel: "vmanage", Status: "normal", Reachability: "reachable"},
		{UUID: "uuid-2", HostName: "TEST-VSMART1", SiteID: "100", Personality: "vsmart", DeviceModel: "vsmart", Status: "normal", Reachability: "reachable"},
		{UUID: "uuid-3", HostName: "TEST-VBOND1", SiteID: "100", Personality: "vbond", DeviceModel: "vedge-cloud", Status: "normal", Reachability: "reachable"},
		{UUID: "uuid-4", HostName: "TEST-VEDGE1", SiteID: "200", Personality: "vedge", DeviceModel: "vedge-100", Status: "normal", Reachability: "reachable"},
		{UUID: "uuid-5", HostName: "TEST-CEDGE1", SiteID: "200", Personality: "cedge", DeviceModel: "C8000v", Status: "normal", Reachability: "reachable"},
		{UUID: "uuid-6", HostName: "TEST-UNKNOWN1", SiteID: "300", Personality: "unknown-role", Status: "normal", Reachability: "reachable"},
	}

	resources, _ := TransformDevices(devices, "documentation")

	// uuid-6 has an unrecognized personality and must be skipped.
	if len(resources) != 5 {
		t.Fatalf("expected 5 resources (unknown personality skipped), got %d", len(resources))
	}

	counts := map[string]int{}
	for _, r := range resources {
		counts[r.Type]++
	}
	if counts["osiris.cisco.controller"] != 3 {
		t.Errorf("expected 3 osiris.cisco.controller resources, got %d", counts["osiris.cisco.controller"])
	}
	if counts["network.router"] != 2 {
		t.Errorf("expected 2 network.router resources, got %d", counts["network.router"])
	}
}

func TestTransformDevices_ResourceID(t *testing.T) {
	devices := []Device{
		{UUID: "11111111-1111-1111-1111-111111111111", HostName: "TEST-VEDGE1", SiteID: "100", Personality: "vedge"},
	}
	resources, systemIPToID := TransformDevices(devices, "documentation")
	want := "cisco.vmanage::11111111-1111-1111-1111-111111111111"
	if resources[0].ID != want {
		t.Errorf("resource ID = %q, want %q", resources[0].ID, want)
	}
	if resources[0].Provider.NativeID != "11111111-1111-1111-1111-111111111111" {
		t.Errorf("provider.native_id = %q, want %q", resources[0].Provider.NativeID, "11111111-1111-1111-1111-111111111111")
	}
	if len(systemIPToID) != 0 {
		t.Errorf("expected no system-ip index entries without a system-ip, got %v", systemIPToID)
	}
}

func TestTransformDevices_RouterIDUsesSerial(t *testing.T) {
	// network.router devices use board-serial for id/provider.native_id
	// when available a real hardware serial, unlike "uuid" which
	// vManage populates as a "<model>-<chassis-serial>" string for
	// vEdge/cEdge platforms, not an actual UUID. That string is
	// preserved separately as extensions.chassis_number.
	devices := []Device{
		{UUID: "C8200L-1N-4T-S123456789L", HostName: "TEST-CEDGE1", SiteID: "100", Personality: "cedge", BoardSerial: "S123456789L"},
	}
	resources, _ := TransformDevices(devices, "documentation")

	want := "cisco.vmanage::S123456789L"
	if resources[0].ID != want {
		t.Errorf("resource ID = %q, want %q", resources[0].ID, want)
	}
	if resources[0].Provider.NativeID != "S123456789L" {
		t.Errorf("provider.native_id = %q, want %q", resources[0].Provider.NativeID, "S123456789L")
	}
	ext := resources[0].Extensions[extensionKey].(map[string]any)
	if ext["chassis_number"] != "C8200L-1N-4T-S123456789L" {
		t.Errorf("extensions.chassis_number = %v, want %q", ext["chassis_number"], "C8200L-1N-4T-S123456789L")
	}
}

func TestTransformDevices_RouterIDFallsBackWithoutSerial(t *testing.T) {
	// No board-serial available: falls back to uuid (previous
	// behavior), and no chassis_number is added since uuid is already
	// the resource identity adding it would just duplicate the ID.
	devices := []Device{
		{UUID: "11111111-1111-1111-1111-111111111111", HostName: "TEST-VEDGE1", SiteID: "100", Personality: "vedge"},
	}
	resources, _ := TransformDevices(devices, "documentation")

	if resources[0].Provider.NativeID != "11111111-1111-1111-1111-111111111111" {
		t.Errorf("provider.native_id = %q, want %q", resources[0].Provider.NativeID, "11111111-1111-1111-1111-111111111111")
	}
	if ext, ok := resources[0].Extensions[extensionKey].(map[string]any); ok {
		if _, ok := ext["chassis_number"]; ok {
			t.Error("chassis_number should not be set when board-serial was unavailable")
		}
	}
}

func TestTransformDevices_ControllerIDIgnoresSerial(t *testing.T) {
	// Controllers (vmanage/vsmart/vbond) keep the uuid/deviceId key
	// the serial substitution only applies to network.router.
	devices := []Device{
		{UUID: "11111111-1111-1111-1111-111111111111", HostName: "TEST-VMANAGE1", SiteID: "100", Personality: "vmanage", BoardSerial: "12345678"},
	}
	resources, _ := TransformDevices(devices, "documentation")

	if resources[0].Provider.NativeID != "11111111-1111-1111-1111-111111111111" {
		t.Errorf("provider.native_id = %q, want the controller's uuid unchanged", resources[0].Provider.NativeID)
	}
	if ext, ok := resources[0].Extensions[extensionKey].(map[string]any); ok {
		if _, ok := ext["chassis_number"]; ok {
			t.Error("chassis_number should not be set for controller personalities")
		}
	}
}

func TestDeviceNativeKey(t *testing.T) {
	cases := []struct {
		name    string
		d       Device
		resType string
		want    string
	}{
		{"router with serial", Device{UUID: "u", DeviceID: "did", BoardSerial: "serial"}, "network.router", "serial"},
		{"router without serial falls to uuid", Device{UUID: "u", DeviceID: "did"}, "network.router", "u"},
		{"router without serial or uuid falls to deviceId", Device{DeviceID: "did"}, "network.router", "did"},
		{"controller ignores serial", Device{UUID: "u", BoardSerial: "serial"}, "osiris.cisco.controller", "u"},
	}
	for _, c := range cases {
		if got := deviceNativeKey(c.d, c.resType); got != c.want {
			t.Errorf("%s: deviceNativeKey() = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestTransformDevices_ProviderHasNoSiteField(t *testing.T) {
	// provider.site is deliberately not set (deferred to a future
	// top-level "location" field per OSIRIS-JSON-v1.0 section 7.5.2)
	devices := []Device{
		{UUID: "11111111-1111-1111-1111-111111111111", HostName: "TEST-VEDGE1", SiteID: "123456789", Personality: "vedge"},
	}

	resources, _ := TransformDevices(devices, "documentation")
	if resources[0].Provider.Site != "" {
		t.Errorf("provider.site = %q, want empty", resources[0].Provider.Site)
	}
	ext := resources[0].Extensions[extensionKey].(map[string]any)
	if ext["site_id"] != "123456789" {
		t.Errorf("extensions.site_id = %v, want %q", ext["site_id"], "123456789")
	}
}

func TestTransformDevices_SystemIPIndex(t *testing.T) {
	devices := []Device{
		{UUID: "11111111-1111-1111-1111-111111111111", HostName: "TEST-VEDGE1", SiteID: "100", Personality: "vedge", SystemIP: "192.0.2.1"},
	}
	resources, systemIPToID := TransformDevices(devices, "documentation")
	if got := systemIPToID["192.0.2.1"]; got != resources[0].ID {
		t.Errorf("systemIPToID[%q] = %q, want %q", "192.0.2.1", got, resources[0].ID)
	}
}

func TestTransformDevices_ManagementIPOnly(t *testing.T) {
	// The device's system-ip is represented once, as management_ip
	// no separate ip_addresses object (it would just duplicate the same
	// single value) and no bare system_ip key.
	devices := []Device{
		{UUID: "11111111-1111-1111-1111-111111111111", HostName: "TEST-VEDGE1", SiteID: "100", Personality: "vedge", SystemIP: "192.0.2.1"},
	}
	resources, _ := TransformDevices(devices, "documentation")

	if resources[0].Properties["management_ip"] != "192.0.2.1" {
		t.Errorf("management_ip = %v, want 192.0.2.1", resources[0].Properties["management_ip"])
	}
	if _, ok := resources[0].Properties["ip_addresses"]; ok {
		t.Error("ip_addresses should not be emitted it would duplicate management_ip")
	}
	if _, ok := resources[0].Properties["system_ip"]; ok {
		t.Error("system_ip should no longer be emitted as a bare property key")
	}
}

func TestTransformDevices_RouterCommonProperties(t *testing.T) {
	// manufacturer/model/version/management_ip match
	// OSIRIS-JSON-v1.0 7.5.2's network.router "Common properties"
	// table; version moves out of provider (7.5.2's own example
	// doesn't set provider.version for network.router).
	devices := []Device{
		{UUID: "11111111-1111-1111-1111-111111111111", HostName: "TEST-VEDGE1", SiteID: "100", Personality: "vedge", DeviceModel: "vedge-C8200L-1N-4T", Version: "17.15.01a.0.193"},
	}
	resources, _ := TransformDevices(devices, "documentation")

	r := resources[0]
	if r.Properties["manufacturer"] != "Cisco" {
		t.Errorf("manufacturer = %v, want Cisco", r.Properties["manufacturer"])
	}
	if r.Properties["model"] != "vedge-C8200L-1N-4T" {
		t.Errorf("model = %v, want vedge-C8200L-1N-4T", r.Properties["model"])
	}
	if r.Properties["version"] != "17.15.01a.0.193" {
		t.Errorf("version = %v, want 17.15.01a.0.193", r.Properties["version"])
	}
	if r.Provider.Version != "" {
		t.Errorf("provider.version = %q, want empty (moved to properties)", r.Provider.Version)
	}
}

func TestTransformDevices_ExtensionsNamespace(t *testing.T) {
	devices := []Device{
		{
			UUID: "11111111-1111-1111-1111-111111111111", HostName: "TEST-VEDGE1", SiteID: "100", Personality: "vedge",
			DeviceType: "vedge", DeviceModel: "vedge-100", Platform: "x86_64",
			DeviceGroups: []string{"No groups"}, Validity: "valid", LastUpdated: 1754325907000,
		},
	}
	resources, _ := TransformDevices(devices, "documentation")

	ext, ok := resources[0].Extensions[extensionKey].(map[string]any)
	if !ok {
		t.Fatalf("expected extensions[%q] to be an object, got %T", extensionKey, resources[0].Extensions[extensionKey])
	}
	for _, key := range []string{"site_id", "device_groups"} {
		if _, ok := ext[key]; !ok {
			t.Errorf("expected extensions[%q][%q] to be set", extensionKey, key)
		}
	}
	// device_type, device_model (moved to properties.model), validity,
	// personality, platform and last_updated are deliberately dropped
	// from extensions.
	for _, key := range []string{"device_type", "device_model", "validity", "personality", "platform", "last_updated"} {
		if _, ok := ext[key]; ok {
			t.Errorf("extensions[%q][%q] should have been dropped, still present", extensionKey, key)
		}
	}
	for _, key := range []string{"personality", "device_type", "device_model", "platform", "site_id", "reachability", "validity", "device_groups", "last_updated"} {
		if _, ok := resources[0].Properties[key]; ok {
			t.Errorf("properties[%q] should not be a bare top-level property key", key)
		}
	}
}

func TestTransformDevices_NoExtensionsWhenEmpty(t *testing.T) {
	// No site-id, no device_groups, documentation purpose: ext ends up
	// empty, and r.Extensions should stay nil rather than emit an
	// empty placeholder object ("extensions": {"osiris.cisco.vmanage": {}}).
	devices := []Device{
		{UUID: "11111111-1111-1111-1111-111111111111", HostName: "TEST-VEDGE1", Personality: "vedge"},
	}
	resources, _ := TransformDevices(devices, "documentation")
	if resources[0].Extensions != nil {
		t.Errorf("Extensions = %v, want nil", resources[0].Extensions)
	}
}

func TestTransformDevices_State(t *testing.T) {
	devices := []Device{
		{UUID: "11111111-1111-1111-1111-111111111111", HostName: "TEST-VEDGE1", SiteID: "100", Personality: "vedge", Reachability: "reachable"},
		{UUID: "uuid-2", HostName: "TEST-VEDGE2", SiteID: "100", Personality: "vedge", Reachability: "unreachable"},
	}
	resources, _ := TransformDevices(devices, "documentation")
	if resources[0].State != "reachable" {
		t.Errorf("resources[0].State = %q, want %q", resources[0].State, "reachable")
	}
	if resources[1].State != "unreachable" {
		t.Errorf("resources[1].State = %q, want %q", resources[1].State, "unreachable")
	}
}

func TestTransformDevices_PurposeGating(t *testing.T) {
	// Latitude/longitude are deliberately not covered here - geo data
	// moved to the physical.room group's properties.geo_location (not
	// purpose-gated there), see TestTransformSiteGroup_GeoLocation.
	devices := []Device{
		{UUID: "11111111-1111-1111-1111-111111111111", HostName: "TEST-VEDGE1", SiteID: "100", Personality: "vedge", BoardSerial: "TST0000001", CertificateValidity: "Valid"},
	}

	doc, _ := TransformDevices(devices, "documentation")
	if _, ok := doc[0].Properties["serial_number"]; ok {
		t.Error("documentation purpose should not include serial_number")
	}
	ext := doc[0].Extensions[extensionKey].(map[string]any)
	if _, ok := ext["certificate_validity"]; ok {
		t.Error("documentation purpose should not include certificate_validity")
	}

	audit, _ := TransformDevices(devices, "audit")
	if audit[0].Properties["serial_number"] != "TST0000001" {
		t.Errorf("audit purpose should include serial_number, got %v", audit[0].Properties["serial_number"])
	}
	auditExt := audit[0].Extensions[extensionKey].(map[string]any)
	if auditExt["certificate_validity"] != "Valid" {
		t.Errorf("audit purpose should include certificate_validity, got %v", auditExt["certificate_validity"])
	}
}

func TestTransformDevices_HealthAndInventoryExtensions(t *testing.T) {
	devices := []Device{
		{
			UUID: "11111111-1111-1111-1111-111111111111", HostName: "TEST-CEDGE1", SiteID: "200", Personality: "cedge",
			HealthState: "green", StateDescription: "All daemons up",
			ConnectedVManages: []string{"192.0.2.5"},
			LastUpdated:       1634627015139,
			UptimeDate:        1634626320000,
		},
	}

	// health/state_description/connected_manager/last_update/up_since
	// are basic device state, not higher-detail data, so they must be
	// present at the default "documentation" purpose too.
	resources, _ := TransformDevices(devices, "documentation")
	ext, ok := resources[0].Extensions[extensionKey].(map[string]any)
	if !ok {
		t.Fatalf("expected extensions[%q] to be an object", extensionKey)
	}
	if ext["health"] != "green" {
		t.Errorf("extensions.health = %v, want %q", ext["health"], "green")
	}
	if ext["state_description"] != "All daemons up" {
		t.Errorf("extensions.state_description = %v, want %q", ext["state_description"], "All daemons up")
	}
	connectedManager, ok := ext["connected_manager"].([]string)
	if !ok || len(connectedManager) != 1 || connectedManager[0] != "192.0.2.5" {
		t.Errorf("extensions.connected_manager = %v, want [192.0.2.5]", ext["connected_manager"])
	}
	if ext["last_update"] != "2026-08-10T07:03:35Z" {
		t.Errorf("extensions.last_update = %v, want %q", ext["last_update"], "2026-08-10T07:03:35Z")
	}
	if ext["up_since"] != "2026-08-10T06:52:00Z" {
		t.Errorf("extensions.up_since = %v, want %q", ext["up_since"], "2026-08-10T06:52:00Z")
	}
}

func TestTransformDevices_HealthExtensionsOmittedWhenEmpty(t *testing.T) {
	devices := []Device{
		{UUID: "11111111-1111-1111-1111-111111111111", HostName: "TEST-CEDGE1", SiteID: "200", Personality: "cedge"},
	}
	resources, _ := TransformDevices(devices, "documentation")
	ext, ok := resources[0].Extensions[extensionKey].(map[string]any)
	if !ok {
		t.Fatalf("expected extensions[%q] to be an object", extensionKey)
	}
	for _, key := range []string{"health", "state_description", "connected_manager", "last_update", "up_since"} {
		if _, ok := ext[key]; ok {
			t.Errorf("extensions[%q] should be omitted when unset, got %v", key, ext[key])
		}
	}
}

func TestTransformDevices_ResourceIDDeterministic(t *testing.T) {
	devices := []Device{
		{UUID: "11111111-1111-1111-1111-111111111111", HostName: "TEST-VEDGE1", SiteID: "100", Personality: "vedge"},
	}
	r1, _ := TransformDevices(devices, "documentation")
	r2, _ := TransformDevices(devices, "documentation")
	if r1[0].ID != r2[0].ID {
		t.Errorf("resource ID not deterministic: %q != %q", r1[0].ID, r2[0].ID)
	}
}

func TestMapDeviceStatus(t *testing.T) {
	cases := []struct {
		status, reachability, want string
	}{
		{"normal", "reachable", "active"},
		{"normal", "unreachable", "inactive"},
		{"error", "reachable", "degraded"},
		{"warning", "reachable", "degraded"},
		{"new", "reachable", "unknown"},
		{"", "", "unknown"},
	}
	for _, c := range cases {
		if got := mapDeviceStatus(c.status, c.reachability); got != c.want {
			t.Errorf("mapDeviceStatus(%q, %q) = %q, want %q", c.status, c.reachability, got, c.want)
		}
	}
}

func TestGroupDevicesBySiteID(t *testing.T) {
	devices := []Device{
		{UUID: "11111111-1111-1111-1111-111111111111", SiteID: "100"},
		{UUID: "uuid-2", SiteID: "100"},
		{UUID: "uuid-3", SiteID: "200"},
		{UUID: "uuid-4", SiteID: ""},
	}
	groups := GroupDevicesBySiteID(devices)
	if len(groups["100"]) != 2 {
		t.Errorf("expected 2 devices in site 100, got %d", len(groups["100"]))
	}
	if len(groups["200"]) != 1 {
		t.Errorf("expected 1 device in site 200, got %d", len(groups["200"]))
	}
	if len(groups[""]) != 1 {
		t.Errorf("expected 1 unclaimed device, got %d", len(groups[""]))
	}
}

func TestToFloat(t *testing.T) {
	if v, ok := toFloat("45.5454"); !ok || v != 45.5454 {
		t.Errorf("toFloat(string) = %v, %v", v, ok)
	}
	if v, ok := toFloat(10.2251); !ok || v != 10.2251 {
		t.Errorf("toFloat(float64) = %v, %v", v, ok)
	}
	if _, ok := toFloat(nil); ok {
		t.Error("toFloat(nil) should not be ok")
	}
	if _, ok := toFloat("not-a-number"); ok {
		t.Error("toFloat(invalid string) should not be ok")
	}
}

func TestResourceKey(t *testing.T) {
	if got := resourceKey("C8200L-1N-4T-TST0000001", "GigabitEthernet1"); got != "C8200L-1N-4T-TST0000001-GigabitEthernet1" {
		t.Errorf("resourceKey() = %q", got)
	}
}

func TestStripCIDRHost(t *testing.T) {
	cases := []struct{ in, want string }{
		{"192.0.2.0/24", "192.0.2.0"},
		{"192.0.2.10", "192.0.2.10"},
		{"", ""},
		{"-", ""},
		{"not-an-ip/24", ""},
		// vManage's placeholder for "no address configured" on a down
		// interface, observed both bare and CIDR-suffixed in real
		// responses not a real address, and not private just because
		// it fails an RFC 1918 check.
		{"0.0.0.0", ""},
		{"0.0.0.0/32", ""},
	}
	for _, c := range cases {
		if got := stripCIDRHost(c.in); got != c.want {
			t.Errorf("stripCIDRHost(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTransformSiteGroup_Basic(t *testing.T) {
	g, ok := TransformSiteGroup("100", "MXP", nil, []string{"cisco.vmanage::a", "cisco.vmanage::b"})
	if !ok {
		t.Fatal("expected ok=true with non-empty deviceResourceIDs")
	}
	if g.Type != "physical.room" {
		t.Errorf("Type = %q, want %q", g.Type, "physical.room")
	}
	if g.Name != "MXP" {
		t.Errorf("Name = %q, want %q", g.Name, "MXP")
	}
	if len(g.Members) != 2 {
		t.Errorf("Members = %v, want 2 entries", g.Members)
	}
}

func TestTransformSiteGroup_UnsitedFallback(t *testing.T) {
	// No siteID and no siteName (the "unclaimed" bucket): group still
	// gets a stable boundary token and a display name instead of an
	// empty one.
	g, ok := TransformSiteGroup("", "", nil, []string{"cisco.vmanage::a"})
	if !ok {
		t.Fatal("expected ok=true")
	}
	if g.Name != unsitedSegment {
		t.Errorf("Name = %q, want %q", g.Name, unsitedSegment)
	}

	// The boundary token (and therefore the group ID) must be stable
	// and distinct from any real site-id same group across runs.
	g2, _ := TransformSiteGroup("", "", nil, []string{"cisco.vmanage::a"})
	if g.ID != g2.ID {
		t.Errorf("group ID not deterministic: %q != %q", g.ID, g2.ID)
	}
}

func TestTransformSiteGroup_GeoLocation(t *testing.T) {
	// vManage reports the same coordinates for every device at a site
	// the first resolvable pair is used, matching OSIRIS-JSON-v1.0
	// section 6.5 common properties.geo_location shape for groups. Not
	// purpose-gated: a site's coordinates are not sensitive the way a
	// serial number or certificate validity is.
	devices := []Device{
		{Latitude: "45.5454", Longitude: 10.2251},
		{Latitude: "45.5454", Longitude: 10.2251},
	}

	g, _ := TransformSiteGroup("100", "MXP", devices, []string{"cisco.vmanage::a"})
	geo, ok := g.Properties["geo_location"].(map[string]any)
	if !ok {
		t.Fatalf("expected properties.geo_location to be an object, got %T", g.Properties["geo_location"])
	}
	if geo["latitude"] != 45.5454 {
		t.Errorf("geo_location.latitude = %v, want 45.5454", geo["latitude"])
	}
	if geo["longitude"] != 10.2251 {
		t.Errorf("geo_location.longitude = %v, want 10.2251", geo["longitude"])
	}
}

func TestTransformSiteGroup_NoGeoLocationWhenUnresolvable(t *testing.T) {
	devices := []Device{{Latitude: nil, Longitude: nil}}
	g, _ := TransformSiteGroup("100", "MXP", devices, []string{"cisco.vmanage::a"})
	if _, ok := g.Properties["geo_location"]; ok {
		t.Error("geo_location should be omitted when no device resolves coordinates")
	}
}

func TestTransformSiteGroup_EmptyReturnsFalse(t *testing.T) {
	if _, ok := TransformSiteGroup("100", "MXP", nil, nil); ok {
		t.Error("expected ok=false with no device resource IDs")
	}
}
