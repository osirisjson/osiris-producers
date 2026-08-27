// dto_test.go - Unit tests for the typed NX-API response DTOs (dto.go).
// Exercises rowList's array-or-single-object polymorphism decode and
// flexString/flexInt64's string-or-number tolerance directly against
// real JSON text, every other test in this package can then trust a
// rowList/flexString/flexInt64 field behaves correctly
// without re-verifying it per command shape.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package nxos

import (
	"encoding/json"
	"testing"
)

func TestRowList_SingleObjectDecodesAsOneElement(t *testing.T) {
	var rl rowList[inventoryRow]
	if err := json.Unmarshal([]byte(`{"name":"Chassis"}`), &rl); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(rl) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rl))
	}
	if rl[0].Name != "Chassis" {
		t.Errorf("name: %v", rl[0].Name)
	}
}

func TestRowList_ArrayDecodesAsMultipleElements(t *testing.T) {
	var rl rowList[inventoryRow]
	if err := json.Unmarshal([]byte(`[{"name":"Chassis"},{"name":"Slot 1"}]`), &rl); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(rl) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rl))
	}
	if rl[0].Name != "Chassis" || rl[1].Name != "Slot 1" {
		t.Errorf("unexpected rows: %+v", rl)
	}
}

func TestRowList_EmptyArrayDecodesAsEmpty(t *testing.T) {
	var rl rowList[inventoryRow]
	if err := json.Unmarshal([]byte(`[]`), &rl); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(rl) != 0 {
		t.Errorf("expected 0 rows, got %d", len(rl))
	}
}

func TestRowList_NullDecodesAsNil(t *testing.T) {
	var rl rowList[inventoryRow]
	if err := json.Unmarshal([]byte(`null`), &rl); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if rl != nil {
		t.Errorf("expected nil, got %+v", rl)
	}
}

func TestRowList_MissingTableKeyDecodesAsNil(t *testing.T) {
	// A missing TABLE_x/ROW_x key entirely (not merely present-and-null)
	// must leave the field at its zero value matches parseTableRows
	// returning nil for a missing table.
	var resp inventoryResponse
	if err := json.Unmarshal([]byte(`{}`), &resp); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if resp.TableInv.RowInv != nil {
		t.Errorf("expected nil rows for missing TABLE_inv, got %+v", resp.TableInv.RowInv)
	}
}

func TestRowList_NeitherObjectNorArrayIsAnError(t *testing.T) {
	// A malformed row list (a bare string here) must surface as a real
	// decode error, not silently become an empty or single-zero-value
	// list the DTO-layer equivalent of per-command undecodable-body
	// detection, now at the row-list level.
	var rl rowList[inventoryRow]
	err := json.Unmarshal([]byte(`"not a row"`), &rl)
	if err == nil {
		t.Fatal("expected a decode error for a bare string row list")
	}
}

func TestFlexString_DecodesJSONString(t *testing.T) {
	var s flexString
	if err := json.Unmarshal([]byte(`"10.3(4a)"`), &s); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if s != "10.3(4a)" {
		t.Errorf("got %q", s)
	}
}

func TestFlexString_DecodesWholeNumberWithoutTrailingZero(t *testing.T) {
	var s flexString
	if err := json.Unmarshal([]byte(`100`), &s); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if s != "100" {
		t.Errorf("got %q, want \"100\" (not \"100.0\")", s)
	}
}

func TestFlexString_DecodesDecimalNumber(t *testing.T) {
	var s flexString
	if err := json.Unmarshal([]byte(`95.5`), &s); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if s != "95.5" {
		t.Errorf("got %q", s)
	}
}

func TestFlexString_MissingOrNullDecodesEmpty(t *testing.T) {
	var s flexString = "leftover"
	if err := json.Unmarshal([]byte(`null`), &s); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if s != "" {
		t.Errorf("got %q, want empty string for null", s)
	}
}

func TestFlexString_UnexpectedTypeDecodesEmptyNotError(t *testing.T) {
	// A field that arrives as a bool/object/array (never observed in
	// practice, but not impossible on some future platform) must not
	// fail the whole command matches str()'s pre-DTO behavior of
	// silently returning "" for an unexpected type.
	var s flexString = "leftover"
	if err := json.Unmarshal([]byte(`true`), &s); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s != "" {
		t.Errorf("got %q, want empty string for an unexpected type", s)
	}
}

func TestFlexInt64_DecodesJSONNumber(t *testing.T) {
	var n flexInt64
	if err := json.Unmarshal([]byte(`9216`), &n); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if n != 9216 {
		t.Errorf("got %d", n)
	}
}

func TestFlexInt64_DecodesNumericString(t *testing.T) {
	var n flexInt64
	if err := json.Unmarshal([]byte(`"9216"`), &n); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if n != 9216 {
		t.Errorf("got %d", n)
	}
}

func TestFlexInt64_DecodesFloatNumberTruncated(t *testing.T) {
	var n flexInt64
	if err := json.Unmarshal([]byte(`9216.7`), &n); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if n != 9216 {
		t.Errorf("got %d, want truncated 9216", n)
	}
}

func TestFlexInt64_MissingNullOrUnparsableDecodesZero(t *testing.T) {
	cases := []string{`null`, `"not-a-number"`, `""`}
	for _, raw := range cases {
		var n flexInt64 = 99
		if err := json.Unmarshal([]byte(raw), &n); err != nil {
			t.Fatalf("unmarshal(%s) failed: %v", raw, err)
		}
		if n != 0 {
			t.Errorf("unmarshal(%s) = %d, want 0", raw, n)
		}
	}
}

func TestVRFDetailRow_InterfaceNamesPrefersTableIf(t *testing.T) {
	var row vrfDetailRow
	raw := `{"vrf_name":"PROD","TABLE_if":{"ROW_if":[{"if_name":"Ethernet1/1"}]},"TABLE_intf":{"ROW_intf":[{"intf_name":"Ethernet1/2"}]}}`
	if err := json.Unmarshal([]byte(raw), &row); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	names := row.interfaceNames()
	if len(names) != 1 || names[0] != "Ethernet1/1" {
		t.Errorf("expected [Ethernet1/1] (TABLE_if preferred), got %v", names)
	}
}

func TestVRFDetailRow_InterfaceNamesFallsBackToTableIntf(t *testing.T) {
	var row vrfDetailRow
	raw := `{"vrf_name":"PROD","TABLE_intf":{"ROW_intf":{"intf_name":"Ethernet1/2"}}}`
	if err := json.Unmarshal([]byte(raw), &row); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	names := row.interfaceNames()
	if len(names) != 1 || names[0] != "Ethernet1/2" {
		t.Errorf("expected [Ethernet1/2] via TABLE_intf fallback, got %v", names)
	}
}

// TestDecodeFullCommandResponses exercises every DTO response type
// against a realistic full-body JSON payload, covering both the
// single-row (object) and multi-row (array) polymorphism variants in
// one pass per command the deterministic "valid table/row variations"
// coverage acceptance signal calls for, at the level of a
// whole command response rather than one isolated field.
func TestDecodeFullCommandResponses(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		into any
	}{
		{
			name: "version",
			raw:  `{"chassis_id":"Nexus9000 C9508","memory":65536000,"nxos_ver_str":"10.3(6)"}`,
			into: &versionResponse{},
		},
		{
			name: "inventory multi-row array",
			raw:  `{"TABLE_inv":{"ROW_inv":[{"name":"Chassis"},{"name":"Slot 1"}]}}`,
			into: &inventoryResponse{},
		},
		{
			name: "inventory single-row object",
			raw:  `{"TABLE_inv":{"ROW_inv":{"name":"Chassis"}}}`,
			into: &inventoryResponse{},
		},
		{
			name: "interface brief",
			raw:  `{"TABLE_interface":{"ROW_interface":[{"interface":"Ethernet1/1","state":"up"},{"interface":"Vlan900","svi_admin_state":"up"}]}}`,
			into: &interfaceBriefResponse{},
		},
		{
			name: "vlan brief",
			raw:  `{"TABLE_vlanbriefxbrief":{"ROW_vlanbriefxbrief":{"vlanshowbr-vlanid":"100"}}}`,
			into: &vlanBriefResponse{},
		},
		{
			name: "vrf detail",
			raw:  `{"TABLE_vrf":{"ROW_vrf":[{"vrf_name":"PROD","TABLE_if":{"ROW_if":{"if_name":"Ethernet1/1"}}}]}}`,
			into: &vrfDetailResponse{},
		},
		{
			name: "vrf interface",
			raw:  `{"TABLE_if":{"ROW_if":[{"if_name":"Ethernet1/1","vrf_name":"PROD"}]}}`,
			into: &vrfInterfaceResponse{},
		},
		{
			name: "lldp neighbors",
			raw:  `{"TABLE_nbor_detail":{"ROW_nbor_detail":{"l_port_id":"Ethernet1/1","sys_name":"REMOTE-SW01","port_id":"Ethernet1/49"}}}`,
			into: &lldpNeighborsResponse{},
		},
		{
			name: "vpc brief",
			raw:  `{"vpc-domain-id":"10","TABLE_vpc":{"ROW_vpc":[{"vpc-ifindex":"port-channel10"}]}}`,
			into: &vpcBriefResponse{},
		},
		{
			name: "port-channel summary",
			raw:  `{"TABLE_channel":{"ROW_channel":{"port-channel":"Po10","TABLE_member":{"ROW_member":[{"port":"Eth1/1","port-status":"P"}]}}}}`,
			into: &portChannelSummaryResponse{},
		},
		{
			name: "interface detail with numeric-string counters",
			raw:  `{"TABLE_interface":{"ROW_interface":{"interface":"Ethernet1/1","eth_mtu":"9216","eth_bw":10000000}}}`,
			into: &interfaceDetailResponse{},
		},
		{
			name: "environment",
			raw:  `{"powersup":{"TABLE_psinfo":{"ROW_psinfo":{"psnum":"1"}}},"TABLE_tempinfo":{"ROW_tempinfo":[{"tempmod":"1"}]}}`,
			into: &environmentResponse{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := json.Unmarshal([]byte(tc.raw), tc.into); err != nil {
				t.Fatalf("unmarshal failed: %v", err)
			}
		})
	}
}

func TestDecodeCommandResponse_MalformedTopLevelIsAnError(t *testing.T) {
	// A response body that is valid JSON but the wrong top-level shape
	// (an array where an object is expected) must be a real decode
	// error this is the same class of failure
	// ShowMulti/parseNXAPIResponse already classifies as a per-command
	// ShowResult.Err; decodeBody (nxos.go) applies the identical
	// "log and continue with a zero value" policy on top of it.
	var resp interfaceBriefResponse
	err := json.Unmarshal([]byte(`["not", "an", "object"]`), &resp)
	if err == nil {
		t.Fatal("expected a decode error for a top-level array where an object was expected")
	}
}
