// neighbors.go - Cross-site endpoint resolution ("site plus neighbors")
// for the Cisco Catalyst SD-WAN Manager OSIRIS JSON Producer.
//
// Each site document is portable and self-contained (OSIRIS JSON
// section 2.2.3: no reference outside the document). An SD-WAN site,
// however, is mostly connected to other sites and to the controllers:
// OMP sessions and transport tunnels. Rather than dropping those
// relationships, a site document carries every remote endpoint they
// need as a neighbor resource:
//
//   - a neighbor device keeps only its identity, type, name and
//     provider fields, with extension site_role "neighbor" and its own
//     site_id (provider.site only when that site was collected in the
//     same run, since its name is otherwise unknown); it is never a
//     member of the site group;
//   - a neighbor transport interface (the remote end of a tunnel) keeps
//     its identity, name and color, with the same "contains"
//     connection from its device that its own site document emits;
//   - a neighbor's ID is exactly the ID of the full resource in its own
//     site document, so an OSIRIS JSON Consumer reading several site
//     documents merges a neighbor with the full resource by ID (soft
//     merge, preferring the full record) and a relationship carried by
//     both site documents has the same connection ID in both.
//
// Remote endpoints are resolved against the whole controller inventory
// (GET /dataservice/device returns every device the credential can
// see), not just the selected sites. A system IP shared by several
// devices is ambiguous and never resolved. A remote transport interface
// needs that device's WAN interface listing: collect.go fetches it for
// tunnel peers outside the selected sites. An endpoint that cannot be
// resolved unambiguously is counted in the document's graph record and
// the relationship is omitted no endpoint is ever invented.
//
// OSIRIS JSON Producer for Cisco Catalyst SD-WAN Manager introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification
package catalystsdwanmanager

import (
	"sort"

	"go.osirisjson.org/producers/pkg/sdk"
)

// siteRoleNeighbor marks a resource that belongs to another site.
const siteRoleNeighbor = "neighbor"

// indexedDevice is one inventory device with its resolved identity.
type indexedDevice struct {
	Device
	resType string
	key     string // native key (see deviceNativeKey).
	id      string // resource ID.
}

// fabricIndex indexes the whole controller inventory by system IP.
type fabricIndex struct {
	bySystemIP map[string]*indexedDevice
	ambiguous  map[string]bool
}

// newFabricIndex indexes every inventory device with a known
// personality, a native identity and a system IP.
func newFabricIndex(devices []Device) *fabricIndex {
	fx := &fabricIndex{bySystemIP: map[string]*indexedDevice{}, ambiguous: map[string]bool{}}
	for _, d := range devices {
		resType, ok := personalityToType[d.Personality]
		if !ok || d.SystemIP == "" {
			continue
		}
		key := deviceNativeKey(d, resType)
		if key == "" {
			continue
		}
		x := &indexedDevice{Device: d, resType: resType, key: key, id: resourceID(key)}
		if prev, dup := fx.bySystemIP[d.SystemIP]; dup && prev.id != x.id {
			fx.ambiguous[d.SystemIP] = true
			continue
		}
		fx.bySystemIP[d.SystemIP] = x
	}
	for ip := range fx.ambiguous {
		delete(fx.bySystemIP, ip)
	}
	return fx
}

// device returns the unambiguous device with systemIP.
func (fx *fabricIndex) device(systemIP string) (*indexedDevice, bool) {
	x, ok := fx.bySystemIP[systemIP]
	return x, ok
}

// interfaceResourceType is the interface type for a device type.
func interfaceResourceType(deviceType string) string {
	if deviceType == "network.router" {
		return "network.router.port"
	}
	return "network.interface"
}

// neighborSet collects the neighbor resources one site document needs.
// Candidates are proposed while endpoints are resolved only those an
// emitted connection actually uses are kept (see use).
type neighborSet struct {
	host      string
	siteNames map[string]string         // site-id -> name, for sites collected in this run.
	devices   map[string]*indexedDevice // resource ID -> device.
	ifaces    map[string]neighborIface  // resource ID -> interface.
	used      map[string]bool
	resources []sdk.Resource
}

type neighborIface struct {
	dev           *indexedDevice
	ifname, color string
}

func newNeighborSet(host string, siteNames map[string]string) *neighborSet {
	return &neighborSet{
		host:      host,
		siteNames: siteNames,
		devices:   map[string]*indexedDevice{},
		ifaces:    map[string]neighborIface{},
		used:      map[string]bool{},
	}
}

// proposeDevice registers x as a candidate neighbor and returns its ID.
func (ns *neighborSet) proposeDevice(x *indexedDevice) string {
	ns.devices[x.id] = x
	return x.id
}

// proposeInterface registers the transport interface ifname of x as a
// candidate neighbor and returns its ID. The ID is the plain-name
// interface ID its own document emits (see interfaceKey: a transport
// interface is in VPN 0, the lowest VPN).
func (ns *neighborSet) proposeInterface(x *indexedDevice, ifname, color string) string {
	id := resourceID(resourceKey(x.key, ifname))
	ns.ifaces[id] = neighborIface{dev: x, ifname: ifname, color: color}
	return id
}

// use marks every candidate referenced by conns as needed.
func (ns *neighborSet) use(conns []sdk.Connection) {
	for _, c := range conns {
		for _, id := range []string{c.Source, c.Target} {
			if _, ok := ns.devices[id]; ok {
				ns.used[id] = true
			}
			if ni, ok := ns.ifaces[id]; ok {
				ns.used[id] = true
				ns.used[ni.dev.id] = true
				ns.devices[ni.dev.id] = ni.dev
			}
		}
	}
}

// build returns the used neighbor resources and the "contains"
// connections of the used neighbor interfaces, in ID order.
func (ns *neighborSet) build() ([]sdk.Resource, []sdk.Connection, int, int) {
	var resources []sdk.Resource
	var conns []sdk.Connection
	ids := make([]string, 0, len(ns.used))
	for id := range ns.used {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var nDevices, nIfaces int
	for _, id := range ids {
		if x, ok := ns.devices[id]; ok {
			if _, isIface := ns.ifaces[id]; !isIface {
				if r, ok := ns.deviceResource(x); ok {
					resources = append(resources, r)
					nDevices++
				}
				continue
			}
		}
		ni, ok := ns.ifaces[id]
		if !ok {
			continue
		}
		r, err := sdk.NewResource(id, interfaceResourceType(ni.dev.resType), sdk.Provider{
			Name:     providerName,
			NativeID: resourceKey(ni.dev.key, ni.ifname),
			Type:     ni.ifname,
			Site:     ns.siteNames[ni.dev.SiteID],
			System:   ns.host,
		})
		if err != nil {
			continue
		}
		r.Name = ni.ifname
		r.Properties = map[string]any{"interface_name": ni.ifname}
		ext := map[string]any{"site_role": siteRoleNeighbor}
		if ni.color != "" {
			ext["color"] = ni.color
		}
		r.Extensions = map[string]any{extensionKey: ext}
		resources = append(resources, r)
		nIfaces++
		if c, ok := containsConnection(ni.dev.id, ni.dev.key, id, ni.ifname); ok {
			conns = append(conns, c)
		}
	}
	return resources, conns, nDevices, nIfaces
}

// deviceResource builds the reduced neighbor form of a device.
func (ns *neighborSet) deviceResource(x *indexedDevice) (sdk.Resource, bool) {
	rs, _ := TransformDevices([]Device{x.Device}, "documentation")
	if len(rs) != 1 {
		return sdk.Resource{}, false
	}
	r := rs[0]
	r.Properties = nil
	r.Status, r.State = "", ""
	r.Provider.Site = ns.siteNames[x.SiteID]
	r.Provider.System = ns.host
	ext := map[string]any{"site_role": siteRoleNeighbor}
	if x.SiteID != "" {
		ext["site_id"] = x.SiteID
	}
	r.Extensions = map[string]any{extensionKey: ext}
	return r, true
}
