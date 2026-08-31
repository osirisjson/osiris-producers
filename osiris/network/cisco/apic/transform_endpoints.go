// transform_endpoints.go - APIC endpoint mapping. Converts fvCEp (with
// its fvIp children) into core network.interface resources and wires
// each endpoint into its parent EPG group. Audit purpose only.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package apic

import (
	"sort"
	"strings"

	"go.osirisjson.org/producers/pkg/sdk"
)

// TransformEndpoints converts fvCEp attributes into core
// network.interface resources (audit mode only). Every fvIp child of an
// endpoint is joined into properties.ip_addresses, deduplicated and
// sorted. The ACI-specific encapsulation and fabric-path attributes are
// kept in the vendor extension, not in the core interface properties.
func TransformEndpoints(endpoints, ips []map[string]any) []sdk.Resource {
	ipsByEndpoint := indexIPsByEndpoint(ips)

	var resources []sdk.Resource
	for _, ep := range endpoints {
		dn := str(ep, "dn")
		mac := str(ep, "mac")

		id := resourceID(dn)
		prov := sdk.Provider{
			Name:     providerName,
			NativeID: dn,
		}

		r, err := sdk.NewResource(id, "network.interface", prov)
		if err != nil {
			continue
		}
		r.Name = mac
		r.Status = "active"

		props := map[string]any{
			"mac_address": sdk.NormalizeMAC(mac),
		}
		if addrs := ipsByEndpoint[dn]; len(addrs) > 0 {
			props["ip_addresses"] = addrs
		}
		r.Properties = props

		ext := map[string]any{}
		if v := str(ep, "encap"); v != "" {
			ext["encap"] = v
		}
		if v := str(ep, "fabricPathDn"); v != "" {
			ext["fabric_path"] = v
		}
		if len(ext) > 0 {
			r.Extensions = map[string]any{extensionNamespace: ext}
		}

		resources = append(resources, r)
	}
	return resources
}

// WireEndpointsToEPGs adds endpoint resource IDs as members of their
// parent EPG groups.
// Endpoint DN format: uni/tn-NAME/ap-NAME/epg-NAME/cep-MAC
// EPG DN format: uni/tn-NAME/ap-NAME/epg-NAME
func WireEndpointsToEPGs(endpointAttrs []map[string]any, epgDNToID map[string]string, epgGroups []sdk.Group) {
	idx := groupIndex(epgGroups)
	for _, ep := range endpointAttrs {
		dn := str(ep, "dn")
		epID := resourceID(dn)
		epgDN := extractEPGDN(dn)
		parentID, ok := epgDNToID[epgDN]
		if !ok {
			continue
		}
		if i, ok := idx[parentID]; ok {
			epgGroups[i].AddMembers(epID)
		}
	}
}

// indexIPsByEndpoint groups fvIp objects by their parent fvCEp DN and
// returns the deduplicated, sorted address list per endpoint.
// An fvIp DN is "<cep-dn>/ip-[<address>]".
func indexIPsByEndpoint(ips []map[string]any) map[string][]string {
	sets := make(map[string]map[string]struct{})
	for _, ip := range ips {
		dn := str(ip, "dn")
		addr := str(ip, "addr")
		if addr == "" {
			continue
		}
		i := strings.LastIndex(dn, "/ip-[")
		if i < 0 {
			continue
		}
		cepDN := dn[:i]
		if sets[cepDN] == nil {
			sets[cepDN] = make(map[string]struct{})
		}
		sets[cepDN][addr] = struct{}{}
	}

	out := make(map[string][]string, len(sets))
	for cepDN, set := range sets {
		addrs := make([]string, 0, len(set))
		for a := range set {
			addrs = append(addrs, a)
		}
		sort.Strings(addrs)
		out[cepDN] = addrs
	}
	return out
}
