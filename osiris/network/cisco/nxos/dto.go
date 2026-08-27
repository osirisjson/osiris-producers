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

// versionResponse is "show version" decoded body. NXOSVerStr
// (nxos_ver_str) is the running system image version; KickstartVerStr
// (kickstart_ver_str) is used as a fallback on platforms/releases that
// only populate the kickstart field.
// MemUnit ("mem_type") is a unit label not a numeric type code it was
// originally typed flexInt64, which silently decoded a real "kB"
// value to 0 on every run since a non-numeric string never parses as
// an int64. TransformDevice uses it to convert Memory into a
// spec-conventional memory_mb property.
type versionResponse struct {
	ChassisID       flexString `json:"chassis_id"`
	ProcBoardID     flexString `json:"proc_board_id"`
	NXOSVerStr      flexString `json:"nxos_ver_str"`
	KickstartVerStr flexString `json:"kickstart_ver_str"`
	HostName        flexString `json:"host_name"`
	BiosVerStr      flexString `json:"bios_ver_str"`
	RRReason        flexString `json:"rr_reason"`
	KernUptmDays    flexString `json:"kern_uptm_days"`
	KernUptmHrs     flexString `json:"kern_uptm_hrs"`
	KernUptmMins    flexString `json:"kern_uptm_mins"`
	KernUptmSecs    flexString `json:"kern_uptm_secs"`
	RRSysVer        flexString `json:"rr_sys_ver"`
	Memory          flexInt64  `json:"memory"`
	MemUnit         flexString `json:"mem_type"`
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
// ROW_interface entry. Real captures show this command returns a
// heterogeneous row shape per interface class: physical/logical
// Ethernet-family ports report "state", while VLAN SVI rows report only
// "interface"/"svi_admin_state"[/"svi_rsn_desc"] and never "state" at
// all. TransformInterfaces falls back to SVIAdminState when State is
// empty so SVI resources (which OSPF/BGP connections attach to) are not
// silently left at an "unknown" status.
type interfaceBriefRow struct {
	Interface     flexString `json:"interface"`
	State         flexString `json:"state"`
	SVIAdminState flexString `json:"svi_admin_state"`
	Speed         flexString `json:"speed"`
	Type          flexString `json:"type"`
	PortMode      flexString `json:"portmode"`
	VLAN          flexString `json:"vlan"`
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
// ROW_channel entry a single port-channel bundle. Protocol values seen
// in practice are LACP; PAgP/static are the same field's other
// documented NX-OS values.
type portChannelRow struct {
	Group       flexString             `json:"group"`
	PortChannel flexString             `json:"port-channel"`
	Protocol    flexString             `json:"prtcl"`
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

// psuRow is one "show environment" TABLE_psinfo/ROW_psinfo entry.
// actual_input/actual_out (real-time watts drawn) are deliberately not
// modeled here live power draw is volatile telemetry, excluded per
// OSIRIS-JSON-v1.0 13.1.3 ("not time-series metrics or telemetry"),
// not stable posture. tot_capa (rated capacity) is stable and kept.
type psuRow struct {
	PSNum    flexString `json:"psnum"`
	PSModel  flexString `json:"psmodel"`
	PSStatus flexString `json:"ps_status"`
	TotCapa  flexString `json:"tot_capa"`
}

// psuTable wraps "show environment" powersup.TABLE_psinfo's ROW_psinfo
// list.
type psuTable struct {
	RowPSInfo rowList[psuRow] `json:"ROW_psinfo"`
}

// environmentPowerSup is "show environment"'s "powersup" object the
// power-supply table nests one level deeper than temperature does.
type environmentPowerSup struct {
	TablePSInfo psuTable `json:"TABLE_psinfo"`
}

// fanRow is one "show environment" fandetails.TABLE_faninfo/ROW_faninfo
// entry.
type fanRow struct {
	FanName   flexString `json:"fanname"`
	FanModel  flexString `json:"fanmodel"`
	FanStatus flexString `json:"fanstatus"`
	FanDir    flexString `json:"fandir"`
}

// fanTable wraps "show environment" fandetails.TABLE_faninfo's
// ROW_faninfo list.
type fanTable struct {
	RowFanInfo rowList[fanRow] `json:"ROW_faninfo"`
}

// environmentFanDetails is "show environment" "fandetails" object
// the fan table nests one level deeper, the same shape as "powersup".
// Confirmed against a real production capture.
type environmentFanDetails struct {
	TableFanInfo fanTable `json:"TABLE_faninfo"`
}

// tempRow is one "show environment" TABLE_tempinfo/ROW_tempinfo entry.
type tempRow struct {
	TempMod     flexString `json:"tempmod"`
	Sensor      flexString `json:"sensor"`
	AlarmStatus flexString `json:"alarmstatus"`
	MajThres    flexString `json:"majthres"`
	MinThres    flexString `json:"minthres"`
}

// tempTable wraps "show environment" ROW_tempinfo list.
type tempTable struct {
	RowTempInfo rowList[tempRow] `json:"ROW_tempinfo"`
}

// environmentResponse is "show environment" decoded body.
type environmentResponse struct {
	PowerSup      environmentPowerSup   `json:"powersup"`
	FanDetails    environmentFanDetails `json:"fandetails"`
	TableTempInfo tempTable             `json:"TABLE_tempinfo"`
}

// moduleInfoRow is one "show module" TABLE_modinfo/ROW_modinfo entry.
type moduleInfoRow struct {
	ModInf  flexString `json:"modinf"`
	Model   flexString `json:"model"`
	ModType flexString `json:"modtype"`
	Ports   flexString `json:"ports"`
	Status  flexString `json:"status"`
}

// moduleInfoTable wraps "show module" TABLE_modinfo's ROW_modinfo list.
type moduleInfoTable struct {
	RowModInfo rowList[moduleInfoRow] `json:"ROW_modinfo"`
}

// moduleDiagRow is one "show module" TABLE_moddiaginfo/ROW_moddiaginfo
// entry the module's own POST diagnostic result, distinct from its
// operational status in moduleInfoRow.
type moduleDiagRow struct {
	Mod        flexString `json:"mod"`
	DiagStatus flexString `json:"diagstatus"`
}

// moduleDiagTable wraps "show module" TABLE_moddiaginfo's
// ROW_moddiaginfo list.
type moduleDiagTable struct {
	RowModDiagInfo rowList[moduleDiagRow] `json:"ROW_moddiaginfo"`
}

// moduleWwnRow is one "show module" TABLE_modwwninfo/ROW_modwwninfo
// entry hardware/software version and slot type. modmacinfo (the
// fourth table this command returns: a MAC address range plus a
// serial number that always duplicates the chassis serial already
// captured from "show version"/"show inventory" on this single-module
// platform) is deliberately not modeled no value beyond what is
// already captured elsewhere.
type moduleWwnRow struct {
	ModWwn   flexString `json:"modwwn"`
	HW       flexString `json:"hw"`
	SW       flexString `json:"sw"`
	SlotType flexString `json:"slottype"`
}

// moduleWwnTable wraps "show module" TABLE_modwwninfo's
// ROW_modwwninfo list.
type moduleWwnTable struct {
	RowModWwnInfo rowList[moduleWwnRow] `json:"ROW_modwwninfo"`
}

// moduleResponse is "show module" decoded body.
type moduleResponse struct {
	TableModInfo     moduleInfoTable `json:"TABLE_modinfo"`
	TableModDiagInfo moduleDiagTable `json:"TABLE_moddiaginfo"`
	TableModWwnInfo  moduleWwnTable  `json:"TABLE_modwwninfo"`
}

// transceiverRow is one "show interface transceiver"
// TABLE_interface/ROW_interface entry. Every field beyond Interface/SFP
// is only present when SFP is "present" an empty port reports just
// {"interface": ..., "sfp": "not present"}, confirmed against a real
// production capture (40 of 54 ports on that device).
type transceiverRow struct {
	Interface      flexString `json:"interface"`
	SFP            flexString `json:"sfp"`
	Name           flexString `json:"name"`
	CiscoProductID flexString `json:"cisco_product_id"`
	PartNum        flexString `json:"partnum"`
	SerialNum      flexString `json:"serialnum"`
	Type           flexString `json:"type"`
}

// transceiverTable wraps "show interface transceiver"
// ROW_interface list.
type transceiverTable struct {
	RowInterface rowList[transceiverRow] `json:"ROW_interface"`
}

// transceiverResponse is "show interface transceiver" decoded body.
type transceiverResponse struct {
	TableInterface transceiverTable `json:"TABLE_interface"`
}

// cdpNeighborRow is one "show cdp neighbors detail"
// TABLE_cdp_neighbor_detail_info/ROW_cdp_neighbor_detail_info entry.
// "capability" (a JSON array) is intentionally not decoded here this
// producer's neighbor merge only needs identity/reachability fields,
// not CDP's capability bitmap.
type cdpNeighborRow struct {
	IntfID     flexString `json:"intf_id"`
	PortID     flexString `json:"port_id"`
	SysName    flexString `json:"sysname"`
	V4Addr     flexString `json:"v4addr"`
	V4MgmtAddr flexString `json:"v4mgmtaddr"`
	PlatformID flexString `json:"platform_id"`
}

// cdpTable wraps "show cdp neighbors detail"
// ROW_cdp_neighbor_detail_info list.
type cdpTable struct {
	RowCDP rowList[cdpNeighborRow] `json:"ROW_cdp_neighbor_detail_info"`
}

// cdpNeighborsResponse is "show cdp neighbors detail" decoded body.
type cdpNeighborsResponse struct {
	TableCDP cdpTable `json:"TABLE_cdp_neighbor_detail_info"`
}

// vpcPeerKeepaliveResponse is "show vpc peer-keepalive" decoded body.
//
// UNVERIFIED: no real device JSON capture exists for this command (only
// plain CLI text "Destination : N/A" with keepalive disabled on the
// one vPC-configured device captured). Field names follow this
// producer's own "show vpc brief" dash-case convention
// (vpc-domain-id/vpc-peer-status/...) by analogy, not confirmation.
// Verify against a real device with keepalive actually configured
// before treating as final.
type vpcPeerKeepaliveResponse struct {
	Status      flexString `json:"vpc-peer-keepalive-status"`
	Destination flexString `json:"vpc-dest"`
	VRF         flexString `json:"vpc-keepalive-vrf"`
}

// ipInterfaceRow is one "show ip interface brief vrf all" TABLE_intf/
// ROW_intf entry. "prefix" is a bare IP address, not a CIDR the
// brief-mode command reports no subnet mask; full prefix/subnet
// mapping needs "show ip interface" (not brief), not implemented this
// pass.
type ipInterfaceRow struct {
	IntfName  flexString `json:"intf-name"`
	Prefix    flexString `json:"prefix"`
	VRFName   flexString `json:"vrf-name-out"`
	LinkState flexString `json:"link-state"`
}

// ipInterfaceTable wraps "show ip interface brief vrf all" ROW_intf
// list.
type ipInterfaceTable struct {
	RowIntf rowList[ipInterfaceRow] `json:"ROW_intf"`
}

// ipInterfaceBriefResponse is "show ip interface brief vrf all"
// decoded body.
type ipInterfaceBriefResponse struct {
	TableIntf ipInterfaceTable `json:"TABLE_intf"`
}

// ospfNeighborRow is one per-VRF-context "show ip ospf neighbor vrf
// all" nested TABLE_nbr/ROW_nbr entry. State and DR/BDR role arrive as
// two separate fields ("state": "FULL", "drstate": "DR"), not the
// combined "FULL/BDR" form the CLI's own text table renders. UpTime is
// an ISO 8601 duration string, kept as-is rather than reformatted.
type ospfNeighborRow struct {
	RouterID flexString `json:"rid"`
	Priority flexString `json:"priority"`
	State    flexString `json:"state"`
	DRState  flexString `json:"drstate"`
	UpTime   flexString `json:"uptime"`
	Address  flexString `json:"addr"`
	IfName   flexString `json:"intf"`
}

// ospfNeighborTable wraps a VRF context's nested TABLE_nbr/ROW_nbr
// list.
type ospfNeighborTable struct {
	RowNbr rowList[ospfNeighborRow] `json:"ROW_nbr"`
}

// ospfCtxRow is one "show ip ospf neighbor vrf all" TABLE_ctx/ROW_ctx
// entry - one per OSPF VRF context.
type ospfCtxRow struct {
	VRFName  flexString        `json:"cname"`
	TableNbr ospfNeighborTable `json:"TABLE_nbr"`
}

// ospfCtxTable wraps "show ip ospf neighbor vrf all" ROW_ctx list.
type ospfCtxTable struct {
	RowCtx rowList[ospfCtxRow] `json:"ROW_ctx"`
}

// ospfNeighborsResponse is "show ip ospf neighbor vrf all" decoded
// body.
type ospfNeighborsResponse struct {
	TableCtx ospfCtxTable `json:"TABLE_ctx"`
}

// bgpNeighborRow is one "show bgp all summary" nested TABLE_neighbor/
// ROW_neighbor entry. "time" (not "up_down"/"uptime") is the
// established/reset duration, an ISO 8601 duration string like OSPF's
// "uptime" field.
type bgpNeighborRow struct {
	NeighborID  flexString `json:"neighborid"`
	RemoteAS    flexString `json:"neighboras"`
	State       flexString `json:"state"`
	UpDown      flexString `json:"time"`
	PfxReceived flexString `json:"prefixreceived"`
}

// bgpNeighborTable wraps a BGP AFI/SAFI's nested TABLE_neighbor/
// ROW_neighbor list.
type bgpNeighborTable struct {
	RowNeighbor rowList[bgpNeighborRow] `json:"ROW_neighbor"`
}

// bgpSafRow is one BGP address-family's nested TABLE_saf/ROW_saf
// entry.
type bgpSafRow struct {
	TableNeighbor bgpNeighborTable `json:"TABLE_neighbor"`
}

// bgpSafTable wraps a BGP VRF's nested TABLE_saf/ROW_saf list.
type bgpSafTable struct {
	RowSaf rowList[bgpSafRow] `json:"ROW_saf"`
}

// bgpAfRow is one "show bgp all summary" VRF's nested TABLE_af/ROW_af
// entry.
type bgpAfRow struct {
	TableSaf bgpSafTable `json:"TABLE_saf"`
}

// bgpAfTable wraps a BGP VRF's nested TABLE_af/ROW_af list.
type bgpAfTable struct {
	RowAf rowList[bgpAfRow] `json:"ROW_af"`
}

// bgpVRFRow is one "show bgp all summary" TABLE_vrf/ROW_vrf entry
// one per BGP VRF context. "all" without "vrf all" only ever returns
// the default VRF (confirmed "vrf all" combined with a structured-
// output request is rejected outright by this command); BGP peers in
// a non-default VRF are not collected. No NX-API-JSON-compatible
// command covering every VRF has been found yet.
type bgpVRFRow struct {
	VRFNameOut flexString `json:"vrf-name-out"`
	TableAf    bgpAfTable `json:"TABLE_af"`
}

// bgpVRFTable wraps "show bgp all summary" ROW_vrf list.
type bgpVRFTable struct {
	RowVRF rowList[bgpVRFRow] `json:"ROW_vrf"`
}

// bgpSummaryResponse is "show bgp all summary" decoded body.
type bgpSummaryResponse struct {
	TableVRF bgpVRFTable `json:"TABLE_vrf"`
}

// switchportRow is one "show interface switchport" per-interface
// entry. TrunkVLANs is a comma-separated list that uses NX-OS's own
// range-compression convention see expandVLANRanges in transform.go.
type switchportRow struct {
	Interface  flexString `json:"interface"`
	NativeVLAN flexString `json:"native_vlan"`
	TrunkVLANs flexString `json:"trunk_vlans"`
}

// switchportTable wraps "show interface switchport" ROW_interface
// list.
type switchportTable struct {
	RowInterface rowList[switchportRow] `json:"ROW_interface"`
}

// switchportResponse is "show interface switchport" decoded body.
type switchportResponse struct {
	TableInterface switchportTable `json:"TABLE_interface"`
}

// aaaAccountingRow is one "show aaa accounting" TABLE_acctMethods/
// ROW_acctMethods entry.
type aaaAccountingRow struct {
	Service flexString `json:"service"`
	Methods flexString `json:"methods"`
}

// aaaAccountingTable wraps "show aaa accounting" ROW_acctMethods list.
type aaaAccountingTable struct {
	RowAcctMethods rowList[aaaAccountingRow] `json:"ROW_acctMethods"`
}

// aaaAccountingResponse is "show aaa accounting" decoded body.
type aaaAccountingResponse struct {
	TableAcctMethods aaaAccountingTable `json:"TABLE_acctMethods"`
}

// aaaAuthenticationRow is one "show aaa authentication"
// TABLE_AuthenMethods/ROW_AuthenMethods entry.
type aaaAuthenticationRow struct {
	Service flexString `json:"service"`
	Method  flexString `json:"method"`
}

// aaaAuthenticationTable wraps "show aaa authentication"
// ROW_AuthenMethods list.
type aaaAuthenticationTable struct {
	RowAuthenMethods rowList[aaaAuthenticationRow] `json:"ROW_AuthenMethods"`
}

// aaaAuthenticationResponse is "show aaa authentication" decoded body.
type aaaAuthenticationResponse struct {
	TableAuthenMethods aaaAuthenticationTable `json:"TABLE_AuthenMethods"`
}

// aaaGroupRow is one "show aaa groups" TABLE_groups/ROW_groups entry.
type aaaGroupRow struct {
	Group flexString `json:"group"`
}

// aaaGroupTable wraps "show aaa groups" ROW_groups list.
type aaaGroupTable struct {
	RowGroups rowList[aaaGroupRow] `json:"ROW_groups"`
}

// aaaGroupsResponse is "show aaa groups" decoded body.
type aaaGroupsResponse struct {
	TableGroups aaaGroupTable `json:"TABLE_groups"`
}

// radiusServerResponse is "show radius-server" decoded body global
// posture fields only. The per-server TABLE_server/ROW_server shape
// (this device has 0 RADIUS servers configured, so no real capture
// exists for it) is deliberately not modeled here rather than guessed
// by analogy to tacacsServerRow below; add it once a real non-empty
// capture is available. Any server-level secret field (this command's
// equivalent of tacacsServerRow's secretKey) must never be given a
// struct field here even once that row shape is grounded see the
// comment on tacacsServerRow.
type radiusServerResponse struct {
	GlobalDeadtime   flexString `json:"global_deadtime"`
	GlobalSecureMode flexString `json:"global_secure_mode"`
	GlobalSourceIntf flexString `json:"global_source_intf"`
	GlobalTimeout    flexString `json:"global_timeout"`
	RetransmitCount  flexString `json:"retransmissionCount"`
	ServerCount      flexString `json:"server_count"`
}

// tacacsServerRow is one "show tacacs-server" TABLE_server/ROW_server
// entry. secretKey is deliberately never given a struct field.
type tacacsServerRow struct {
	ServerIP flexString `json:"server_ip"`
	Port     flexString `json:"port"`
	Timeout  flexString `json:"timeout"`
}

// tacacsServerTable wraps "show tacacs-server" ROW_server list.
type tacacsServerTable struct {
	RowServer rowList[tacacsServerRow] `json:"ROW_server"`
}

// tacacsServerResponse is "show tacacs-server" decoded body. Global
// TestUsername/TestPassword fields (a configured TACACS connectivity
// test account, not security posture) are deliberately not modeled,
// same reasoning as GlobalTestPassword would be if it were the
// former isn't a secret but reveals an operational test-account name
// that isn't part of "posture" in the sense this deliverable means.
type tacacsServerResponse struct {
	GlobalDeadtime   flexString        `json:"global_deadtime"`
	GlobalSourceIntf flexString        `json:"global_source_intf"`
	GlobalTimeout    flexString        `json:"global_timeout"`
	ServerCount      flexString        `json:"server_count"`
	TableServer      tacacsServerTable `json:"TABLE_server"`
}
