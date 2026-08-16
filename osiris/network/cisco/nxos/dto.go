// dto.go - Typed NX-API response DTOs for the Cisco NX-OS
// OSIRIS JSON Producer.
// Defines one Go struct per "show ... | json" command this producer
// issues, decoded directly from that command's raw ShowResult body.
// NX-API's own TABLE_x/ROW_x polymorphism (a single row is returned as
// a bare object, multiple rows as an array) is handled exactly once
// here, by rowList's UnmarshalJSON. Every TABLE_x wrapper is its
// own named type (rather than an inline anonymous struct) purely so
// tests can build literals without repeating the wrapper's shape.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification
package nxos

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// rowList decodes NX-API's TABLE_x/ROW_x row polymorphism into a typed
// slice: a single row arrives as a bare JSON object, multiple rows as a
// JSON array. Every command response in this file nests its rows
// through a rowList field instead of handling that ambiguity itself.
type rowList[T any] []T

// UnmarshalJSON implements the array-or-single-object polymorphism
// decode.
func (rl *rowList[T]) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		*rl = nil
		return nil
	}
	var arr []T
	if err := json.Unmarshal(data, &arr); err == nil {
		*rl = arr
		return nil
	}
	var single T
	if err := json.Unmarshal(data, &single); err != nil {
		return fmt.Errorf("rowList: %w", err)
	}
	*rl = rowList[T]{single}
	return nil
}

// flexString decodes an NX-API scalar field that may arrive as either a
// JSON string or a JSON number (device/platform dependent) into a Go
// string. A whole-valued number formats without a trailing ".0" this
// is the same tolerance policy the pre-DTO str() helper used.
type flexString string

// UnmarshalJSON accepts a JSON string or number; anything else (or
// absent/null input) decodes as the empty string rather than erroring
// matching str()'s own behavior of returning "" for a missing or
// unexpected-type field instead of failing the whole command.
func (s *flexString) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		*s = ""
		return nil
	}
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		*s = flexString(str)
		return nil
	}
	var f float64
	if err := json.Unmarshal(data, &f); err != nil {
		*s = ""
		return nil
	}
	if f == float64(int64(f)) {
		*s = flexString(strconv.FormatInt(int64(f), 10))
	} else {
		*s = flexString(strconv.FormatFloat(f, 'f', -1, 64))
	}
	return nil
}

// flexInt64 decodes an NX-API numeric field that may arrive as a JSON
// number or a numeric JSON string into an int64, preserving the pre-DTO
// num() helper's tolerance. Absent, null, or unparsable
// input decodes as zero rather than erroring num() never failed the
// caller over a missing or malformed numeric field either, it simply
// meant "no value."
type flexInt64 int64

// UnmarshalJSON accepts a JSON number or a numeric JSON string.
func (n *flexInt64) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		*n = 0
		return nil
	}
	var i int64
	if err := json.Unmarshal(data, &i); err == nil {
		*n = flexInt64(i)
		return nil
	}
	var f float64
	if err := json.Unmarshal(data, &f); err == nil {
		*n = flexInt64(int64(f))
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		if i, err := strconv.ParseInt(s, 10, 64); err == nil {
			*n = flexInt64(i)
			return nil
		}
	}
	*n = 0
	return nil
}

// versionResponse is "show version" decoded body.
type versionResponse struct {
	ChassisID    flexString `json:"chassis_id"`
	ProcBoardID  flexString `json:"proc_board_id"`
	SysVerStr    flexString `json:"sys_ver_str"`
	HostName     flexString `json:"host_name"`
	BiosVerStr   flexString `json:"bios_ver_str"`
	RRReason     flexString `json:"rr_reason"`
	KernUptmDays flexString `json:"kern_uptm_days"`
	KernUptmHrs  flexString `json:"kern_uptm_hrs"`
	KernUptmMins flexString `json:"kern_uptm_mins"`
	KernUptmSecs flexString `json:"kern_uptm_secs"`
	RRSysVer     flexString `json:"rr_sys_ver"`
	Memory       flexInt64  `json:"memory"`
	MemType      flexInt64  `json:"mem_type"`
}

// inventoryRow is one "show inventory" TABLE_inv/ROW_inv entry.
type inventoryRow struct {
	Name      flexString `json:"name"`
	Desc      flexString `json:"desc"`
	ProductID flexString `json:"productid"`
	VendorID  flexString `json:"vendorid"`
	SerialNum flexString `json:"serialnum"`
}

// inventoryTable wraps "show inventory" ROW_inv list.
type inventoryTable struct {
	RowInv rowList[inventoryRow] `json:"ROW_inv"`
}

// inventoryResponse is "show inventory" decoded body.
type inventoryResponse struct {
	TableInv inventoryTable `json:"TABLE_inv"`
}

// interfaceBriefRow is one "show interface brief" TABLE_interface/
// ROW_interface entry.
type interfaceBriefRow struct {
	Interface flexString `json:"interface"`
	State     flexString `json:"state"`
	Speed     flexString `json:"speed"`
	Type      flexString `json:"type"`
	PortMode  flexString `json:"portmode"`
	Status    flexString `json:"status"`
	VLAN      flexString `json:"vlan"`
}

// interfaceTable wraps "show interface brief" ROW_interface list.
type interfaceTable struct {
	RowInterface rowList[interfaceBriefRow] `json:"ROW_interface"`
}

// interfaceBriefResponse is "show interface brief" decoded body.
type interfaceBriefResponse struct {
	TableInterface interfaceTable `json:"TABLE_interface"`
}

// vlanBriefRow is one "show vlan brief" TABLE_vlanbriefxbrief/
// ROW_vlanbriefxbrief entry.
type vlanBriefRow struct {
	VLANID    flexString `json:"vlanshowbr-vlanid"`
	VLANName  flexString `json:"vlanshowbr-vlanname"`
	VLANState flexString `json:"vlanshowbr-vlanstate"`
	ShutState flexString `json:"vlanshowbr-shutstate"`
	PortList  flexString `json:"vlanshowplist-ifidx"`
}

// vlanBriefTable wraps "show vlan brief" ROW_vlanbriefxbrief list.
type vlanBriefTable struct {
	RowVlanBrief rowList[vlanBriefRow] `json:"ROW_vlanbriefxbrief"`
}

// vlanBriefResponse is "show vlan brief" decoded body.
type vlanBriefResponse struct {
	TableVlanBrief vlanBriefTable `json:"TABLE_vlanbriefxbrief"`
}

// vrfInterfaceIfRow is one nested "TABLE_if/ROW_if" entry within a
// "show vrf all detail" VRF row.
type vrfInterfaceIfRow struct {
	IfName flexString `json:"if_name"`
}

// vrfIfTable wraps a "show vrf all detail" VRF row's nested
// TABLE_if/ROW_if member interface list.
type vrfIfTable struct {
	RowIf rowList[vrfInterfaceIfRow] `json:"ROW_if"`
}

// vrfInterfaceIntfRow is one nested "TABLE_intf/ROW_intf" entry within
// a "show vrf all detail" VRF row some NX-OS versions use this shape
// instead of TABLE_if/ROW_if for the same purpose.
type vrfInterfaceIntfRow struct {
	IntfName flexString `json:"intf_name"`
}

// vrfIntfTable wraps a "show vrf all detail" VRF row's nested
// TABLE_intf/ROW_intf member interface list (the alternate shape some
// NX-OS versions use in place of TABLE_if/ROW_if).
type vrfIntfTable struct {
	RowIntf rowList[vrfInterfaceIntfRow] `json:"ROW_intf"`
}

// vrfDetailRow is one "show vrf all detail" TABLE_vrf/ROW_vrf entry.
type vrfDetailRow struct {
	VRFName   flexString   `json:"vrf_name"`
	VRFID     flexString   `json:"vrf_id"`
	VRFState  flexString   `json:"vrf_state"`
	RD        flexString   `json:"rd"`
	TableIf   vrfIfTable   `json:"TABLE_if"`
	TableIntf vrfIntfTable `json:"TABLE_intf"`
}

// interfaceNames returns this VRF row's member interface names, trying
// the common TABLE_if/ROW_if shape first and falling back to the
// TABLE_intf/ROW_intf shape some NX-OS versions use instead the exact
// fallback WireInterfacesToVRFs always applied, now typed rather than
// re-parsed from a generic map on every call.
func (r vrfDetailRow) interfaceNames() []string {
	if len(r.TableIf.RowIf) > 0 {
		names := make([]string, 0, len(r.TableIf.RowIf))
		for _, row := range r.TableIf.RowIf {
			names = append(names, string(row.IfName))
		}
		return names
	}
	names := make([]string, 0, len(r.TableIntf.RowIntf))
	for _, row := range r.TableIntf.RowIntf {
		names = append(names, string(row.IntfName))
	}
	return names
}

// vrfTable wraps "show vrf all detail" ROW_vrf list.
type vrfTable struct {
	RowVRF rowList[vrfDetailRow] `json:"ROW_vrf"`
}

// vrfDetailResponse is "show vrf all detail" decoded body.
type vrfDetailResponse struct {
	TableVRF vrfTable `json:"TABLE_vrf"`
}

// vrfInterfaceFlatRow is one "show vrf interface" TABLE_if/ROW_if
// entry a flat VRF-to-interface mapping, distinct from the nested
// per-VRF shape "show vrf all detail" uses.
type vrfInterfaceFlatRow struct {
	IfName  flexString `json:"if_name"`
	VRFName flexString `json:"vrf_name"`
}

// vrfInterfaceTable wraps "show vrf interface" ROW_if list.
type vrfInterfaceTable struct {
	RowIf rowList[vrfInterfaceFlatRow] `json:"ROW_if"`
}

// vrfInterfaceResponse is "show vrf interface" decoded body.
type vrfInterfaceResponse struct {
	TableIf vrfInterfaceTable `json:"TABLE_if"`
}

// lldpNeighborRow is one "show lldp neighbors detail"
// TABLE_nbor_detail/ROW_nbor_detail entry.
type lldpNeighborRow struct {
	LocalPortID flexString `json:"l_port_id"`
	SysName     flexString `json:"sys_name"`
	PortID      flexString `json:"port_id"`
	MgmtAddr    flexString `json:"mgmt_addr"`
}

// lldpTable wraps "show lldp neighbors detail" ROW_nbor_detail list.
type lldpTable struct {
	RowNborDetail rowList[lldpNeighborRow] `json:"ROW_nbor_detail"`
}

// lldpNeighborsResponse is "show lldp neighbors detail" decoded body.
type lldpNeighborsResponse struct {
	TableNborDetail lldpTable `json:"TABLE_nbor_detail"`
}

// vpcMemberRow is one "show vpc brief" TABLE_vpc/ROW_vpc entry.
type vpcMemberRow struct {
	IfIndex flexString `json:"vpc-ifindex"`
}

// vpcTable wraps "show vpc brief" ROW_vpc list.
type vpcTable struct {
	RowVPC rowList[vpcMemberRow] `json:"ROW_vpc"`
}

// vpcBriefResponse is "show vpc brief" decoded body.
type vpcBriefResponse struct {
	DomainID            flexString `json:"vpc-domain-id"`
	Role                flexString `json:"vpc-role"`
	PeerStatus          flexString `json:"vpc-peer-status"`
	PeerKeepaliveStatus flexString `json:"vpc-peer-keepalive-status"`
	TableVPC            vpcTable   `json:"TABLE_vpc"`
}

// portChannelMemberRow is one "show port-channel summary" nested
// TABLE_member/ROW_member entry a single bundled physical interface.
type portChannelMemberRow struct {
	Port       flexString `json:"port"`
	PortStatus flexString `json:"port-status"`
}

// portChannelMemberTable wraps a port-channel row's nested
// TABLE_member/ROW_member bundled-interface list.
type portChannelMemberTable struct {
	RowMember rowList[portChannelMemberRow] `json:"ROW_member"`
}

// portChannelRow is one "show port-channel summary" TABLE_channel/
// ROW_channel entry a single port-channel bundle.
type portChannelRow struct {
	Group       flexString             `json:"group"`
	PortChannel flexString             `json:"port-channel"`
	TableMember portChannelMemberTable `json:"TABLE_member"`
}

// portChannelTable wraps "show port-channel summary" ROW_channel list.
type portChannelTable struct {
	RowChannel rowList[portChannelRow] `json:"ROW_channel"`
}

// portChannelSummaryResponse is "show port-channel summary" decoded
// body.
type portChannelSummaryResponse struct {
	TableChannel portChannelTable `json:"TABLE_channel"`
}

// interfaceDetailRow is one detailed "show interface" TABLE_interface/
// ROW_interface entry a different row shape than interfaceBriefRow
// even though NX-API nests both under the same TABLE_interface/
// ROW_interface key names.
type interfaceDetailRow struct {
	Interface flexString `json:"interface"`
	MTU       flexInt64  `json:"eth_mtu"`
	Bandwidth flexInt64  `json:"eth_bw"`
	Duplex    flexString `json:"eth_duplex"`
	HWAddr    flexString `json:"eth_hw_addr"`
	Desc      flexString `json:"desc"`
	OutBytes  flexInt64  `json:"eth_outbytes"`
	InBytes   flexInt64  `json:"eth_inbytes"`
	OutPkts   flexInt64  `json:"eth_outpkts"`
	InPkts    flexInt64  `json:"eth_inpkts"`
}

// interfaceDetailTable wraps the detailed "show interface"
// ROW_interface list.
type interfaceDetailTable struct {
	RowInterface rowList[interfaceDetailRow] `json:"ROW_interface"`
}

// interfaceDetailResponse is the detailed "show interface" decoded
// body.
type interfaceDetailResponse struct {
	TableInterface interfaceDetailTable `json:"TABLE_interface"`
}

// systemResourcesResponse is "show system resources" decoded body.
// Fields stay as flexString (not flexInt64/a float type) because
// TransformSystemResources parses them itself with its own
// error-tolerant strconv calls, via str() only the field access changed
// from map lookups to struct fields, not the parsing policy.
type systemResourcesResponse struct {
	CPUStateIdle    flexString `json:"cpu_state_idle"`
	MemoryUsageUsed flexString `json:"memory_usage_used"`
	MemoryUsageFree flexString `json:"memory_usage_free"`
	LoadAvg1Min     flexString `json:"load_avg_1min"`
}

// psuRow is one "show environment" TABLE_psinfo/ROW_psinfo entry.
type psuRow struct {
	PSNum     flexString `json:"psnum"`
	PSModel   flexString `json:"psmodel"`
	PSStatus  flexString `json:"ps_status"`
	ActualOut flexString `json:"actual_out"`
}

// psuTable wraps "show environment" ROW_psinfo list.
type psuTable struct {
	RowPSInfo rowList[psuRow] `json:"ROW_psinfo"`
}

// tempRow is one "show environment" TABLE_tempinfo/ROW_tempinfo entry.
type tempRow struct {
	TempMod     flexString `json:"tempmod"`
	Sensor      flexString `json:"sensor"`
	CurTemp     flexString `json:"curtemp"`
	AlarmStatus flexString `json:"alarmstatus"`
}

// tempTable wraps "show environment" ROW_tempinfo list.
type tempTable struct {
	RowTempInfo rowList[tempRow] `json:"ROW_tempinfo"`
}

// environmentResponse is "show environment" decoded body.
type environmentResponse struct {
	TablePSInfo   psuTable  `json:"TABLE_psinfo"`
	TableTempInfo tempTable `json:"TABLE_tempinfo"`
}
