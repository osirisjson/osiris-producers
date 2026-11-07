// transform_groups_test.go - Administrative group acceptance tests for
// the OSIRIS JSON Microsoft Azure producer:
// management group -> subscription -> resource group hierarchy via
// children, resource group and region membership as independent
// dimensions, group ID stability, and subscription context of
// stub resources.

package azure

import (
	"errors"
	"testing"

	"go.osirisjson.org/producers/pkg/sdk"
	"go.osirisjson.org/producers/pkg/testharness"
)

const (
	gTenant = "00000000-0000-0000-0000-0000000000aa"
	gSubID  = "00000000-0000-0000-0000-000000000001"
	gRemote = "00000000-0000-0000-0000-000000000002"
)

var gSub = SubscriptionInfo{SubscriptionID: gSubID, DisplayName: "LAB-SUB01", TenantID: gTenant}

// gEntities is a tenant with root -> LAB-MG-A -> LAB-MG-B -> LAB-SUB01,
// plus a second subscription LAB-SUB02 used as the owner of a
// stub resource.
func gEntities() []MGEntity {
	mg := func(name, display string) MGEntity {
		return MGEntity{ID: mgARMIDPrefix + name, Name: name, DisplayName: display, Type: "Microsoft.Management/managementGroups", TenantID: gTenant}
	}
	return []MGEntity{
		mg(gTenant, "Tenant Root Group"),
		mg("lab-mg-a", "LAB-MG-A"),
		mg("lab-mg-b", "LAB-MG-B"),
		{
			ID: "/subscriptions/" + gSubID, Name: gSubID, DisplayName: "LAB-SUB01", Type: "/subscriptions", TenantID: gTenant,
			ParentNameChain:        []string{gTenant, "lab-mg-a", "lab-mg-b"},
			ParentDisplayNameChain: []string{"Tenant Root Group", "LAB-MG-A", "LAB-MG-B"},
		},
		{ID: "/subscriptions/" + gRemote, Name: gRemote, DisplayName: "LAB-SUB02", Type: "/subscriptions", TenantID: gTenant},
	}
}

// assertAcyclic fails if the children relation over groups has a
// cycle or a child that is not a group in the set.
func assertAcyclic(t *testing.T, groups []sdk.Group) {
	t.Helper()
	byID := map[string]sdk.Group{}
	for _, g := range groups {
		byID[g.ID] = g
	}
	const (
		visiting = 1
		done     = 2
	)
	state := map[string]int{}
	var visit func(id string)
	visit = func(id string) {
		switch state[id] {
		case visiting:
			t.Fatalf("group hierarchy cycle through %s", id)
		case done:
			return
		}
		state[id] = visiting
		for _, c := range byID[id].Children {
			if _, ok := byID[c]; !ok {
				t.Fatalf("group %s has dangling child %s", id, c)
			}
			visit(c)
		}
		state[id] = done
	}
	for id := range byID {
		visit(id)
	}
}

func TestManagementGroupHierarchy(t *testing.T) {
	ancestors, _ := buildMGAncestors(gEntities(), gSubID)
	mgGroups, _ := TransformManagementGroupGroups(ancestors)
	subGroup := TransformSubscriptionGroup(gSub)
	WireMGHierarchy(mgGroups, &subGroup)

	if len(mgGroups) != 3 {
		t.Fatalf("expected 3 ancestor management groups, got %d", len(mgGroups))
	}
	want := []string{"Tenant Root Group", "LAB-MG-A", "LAB-MG-B"}
	for i, g := range mgGroups {
		if g.Name != want[i] {
			t.Errorf("ancestor %d = %q, want %q (root to leaf)", i, g.Name, want[i])
		}
	}
	// Each level is the only child of its parent; the leaf parents
	// the subscription.
	for i := 0; i < len(mgGroups)-1; i++ {
		if len(mgGroups[i].Children) != 1 || mgGroups[i].Children[0] != mgGroups[i+1].ID {
			t.Errorf("%s children = %v, want [%s]", mgGroups[i].Name, mgGroups[i].Children, mgGroups[i+1].ID)
		}
	}
	if leaf := mgGroups[len(mgGroups)-1]; len(leaf.Children) != 1 || leaf.Children[0] != subGroup.ID {
		t.Errorf("leaf management group must parent the subscription, got %v", leaf.Children)
	}
	assertAcyclic(t, append(mgGroups, subGroup))
}

// An ancestor the caller cannot read is missing from the entity list
// but named in the subscription's parent chains. It must be rebuilt,
// not skipped, so its child is never attached to the grandparent.
func TestBuildMGAncestorsUnreadableLevel(t *testing.T) {
	var entities []MGEntity
	for _, e := range gEntities() {
		if e.Name != "lab-mg-a" {
			entities = append(entities, e)
		}
	}
	ancestors, _ := buildMGAncestors(entities, gSubID)
	if len(ancestors) != 3 {
		t.Fatalf("expected 3 ancestors with the unreadable level rebuilt, got %d", len(ancestors))
	}
	mid := ancestors[1]
	if mid.Name != "lab-mg-a" || mid.DisplayName != "LAB-MG-A" || mid.ID != mgARMIDPrefix+"lab-mg-a" || mid.TenantID != gTenant {
		t.Errorf("rebuilt ancestor = %+v", mid)
	}
	// The rebuilt group must have the same ID as when it is readable.
	full, _ := buildMGAncestors(gEntities(), gSubID)
	a, _ := TransformManagementGroupGroups(ancestors)
	b, _ := TransformManagementGroupGroups(full)
	if a[1].ID != b[1].ID {
		t.Errorf("rebuilt management group ID %s differs from readable ID %s", a[1].ID, b[1].ID)
	}
}

func TestResourceGroupAndRegionMembership(t *testing.T) {
	rgArm := "/subscriptions/" + gSubID + "/resourceGroups/RG-LAB"
	rgs := []ResourceGroup{{ID: rgArm, Name: "RG-LAB", Location: "westeurope"}}
	rgGroups, rgNameToID := TransformResourceGroupGroups(rgs, gSub)
	subGroup := TransformSubscriptionGroup(gSub)

	res := func(armID, region string) sdk.Resource {
		return sdk.Resource{ID: resourceID("", armID), Provider: sdk.Provider{Name: providerName, NativeID: armID, Region: region, Account: extractSubscriptionID(armID)}}
	}
	vmWEU := res(rgArm+"/providers/Microsoft.Compute/virtualMachines/LAB-VM01", "westeurope")
	vmEUS := res(rgArm+"/providers/Microsoft.Compute/virtualMachines/LAB-VM02", "eastus")
	alert := res(rgArm+"/providers/Microsoft.Insights/metricAlerts/LAB-ALERT01", "global")
	// Lower-cased resource group segment, as some ARM APIs return it.
	nic := res("/subscriptions/"+gSubID+"/resourcegroups/rg-lab/providers/Microsoft.Network/networkInterfaces/LAB-NIC01", "westeurope")
	// Stub owned by another subscription, with a same-named resource group.
	stub := res("/subscriptions/"+gRemote+"/resourceGroups/RG-LAB/providers/Microsoft.Network/virtualNetworks/LAB-VNET02", "")
	all := []sdk.Resource{vmWEU, vmEUS, alert, nic, stub}

	WireResourcesToResourceGroups(all, gSubID, rgNameToID, rgGroups)
	WireResourceGroupsToSubscription(&subGroup, rgGroups)
	regionGroups := TransformRegionGroups(all, gSub)

	// Subscription -> resource group via children only.
	if len(subGroup.Children) != 1 || subGroup.Children[0] != rgGroups[0].ID || len(subGroup.Members) != 0 {
		t.Errorf("subscription group: children %v members %v", subGroup.Children, subGroup.Members)
	}
	members := map[string]bool{}
	for _, m := range rgGroups[0].Members {
		members[m] = true
	}
	for _, r := range []sdk.Resource{vmWEU, vmEUS, alert, nic} {
		if !members[r.ID] {
			t.Errorf("%s is not a member of its resource group", r.Provider.NativeID)
		}
	}

	// Region is an orthogonal dimension: one group per region, global
	// and region-less resources excluded, and never a parent of
	// resource groups.
	byRegion := map[string]sdk.Group{}
	for _, g := range regionGroups {
		byRegion[g.Name] = g
		if len(g.Children) != 0 {
			t.Errorf("region group %s must have no children, got %v", g.Name, g.Children)
		}
	}
	if len(byRegion) != 2 {
		t.Fatalf("expected region groups westeurope and eastus, got %v", len(byRegion))
	}
	if got := byRegion["westeurope"].Members; len(got) != 2 {
		t.Errorf("westeurope members = %v, want VM01 and NIC01", got)
	}
	for _, g := range regionGroups {
		for _, m := range g.Members {
			if m == alert.ID || m == stub.ID {
				t.Errorf("region group %s must not contain %s", g.Name, m)
			}
		}
	}
	assertAcyclic(t, append(append(rgGroups, subGroup), regionGroups...))
}

// A stub owned by another subscription whose resource group name
// matches a local one must not become a member of the local
// resource group: resource group names are unique only within
// a subscription.
func TestStubNotMemberOfLocalResourceGroup(t *testing.T) {
	rgs := []ResourceGroup{{ID: "/subscriptions/" + gSubID + "/resourceGroups/RG-LAB", Name: "RG-LAB"}}
	rgGroups, rgNameToID := TransformResourceGroupGroups(rgs, gSub)
	arm := "/subscriptions/" + gRemote + "/resourceGroups/RG-LAB/providers/Microsoft.Network/virtualNetworks/LAB-VNET02"
	stub := sdk.Resource{ID: resourceID("", arm), Provider: sdk.Provider{NativeID: arm, Account: gRemote}}
	WireResourcesToResourceGroups([]sdk.Resource{stub}, gSubID, rgNameToID, rgGroups)
	if len(rgGroups[0].Members) != 0 {
		t.Errorf("stub from another subscription became a member of a local resource group: %v", rgGroups[0].Members)
	}
}

func TestGroupIDsIndependentOfMembership(t *testing.T) {
	rgs := []ResourceGroup{{ID: "/subscriptions/" + gSubID + "/resourceGroups/RG-LAB", Name: "RG-LAB"}}
	a, _ := TransformResourceGroupGroups(rgs, gSub)
	b, _ := TransformResourceGroupGroups(rgs, gSub)
	b[0].AddMembers("azure::x", "azure::y")
	if a[0].ID != b[0].ID {
		t.Errorf("resource group ID depends on membership: %s vs %s", a[0].ID, b[0].ID)
	}
	r1 := TransformRegionGroups([]sdk.Resource{{ID: "azure::x", Provider: sdk.Provider{Region: "westeurope"}}}, gSub)
	r2 := TransformRegionGroups([]sdk.Resource{{ID: "azure::y", Provider: sdk.Provider{Region: "westeurope"}}}, gSub)
	if r1[0].ID != r2[0].ID {
		t.Errorf("region group ID depends on membership: %s vs %s", r1[0].ID, r2[0].ID)
	}
}

func TestEnrichStubSubscriptionContext(t *testing.T) {
	stubs := []sdk.Resource{
		// Owner from the stub's own ARM ID.
		{ID: "a", Provider: sdk.Provider{NativeID: "/subscriptions/" + gRemote + "/resourceGroups/RG-LAB/providers/Microsoft.Network/virtualNetworks/LAB-VNET02"}},
		{ID: "b", Provider: sdk.Provider{Account: "00000000-0000-0000-0000-0000000000ff"}},
	}
	enrichStubSubscriptionContext(stubs, gEntities())

	if p := stubs[0].Provider; p.System != "" || p.Tenant != gTenant || p.Account != gRemote || p.Subscription != "" {
		t.Errorf("known subscription stub: provider = %+v, want account = owning subscription, its tenant, no system/subscription", p)
	}
	if stubs[0].Extensions != nil {
		t.Errorf("stubs carry no names (the catalog does): %v", stubs[0].Extensions)
	}
	// Unknown subscription: owner kept, no tenant invented.
	if p := stubs[1].Provider; p.System != "" || p.Tenant != "" || p.Account != "00000000-0000-0000-0000-0000000000ff" {
		t.Errorf("unknown subscription stub got invented context: %+v", p)
	}
}

func TestSubscriptionAndTenantNames(t *testing.T) {
	// Subscription group: IDs always, tenant name only when resolved.
	g := TransformSubscriptionGroup(gSub)
	if g.Properties["subscription_id"] != gSubID || g.Properties["tenant_id"] != gTenant || g.Properties["tenant_name"] != nil {
		t.Errorf("subscription group properties = %v", g.Properties)
	}
	named := gSub
	named.TenantDisplayName = "LAB-TENANT"
	if g := TransformSubscriptionGroup(named); g.Properties["tenant_name"] != "LAB-TENANT" || g.ID != TransformSubscriptionGroup(gSub).ID {
		t.Errorf("tenant name must be added without changing the group ID: %v", g.Properties)
	}

	// Management groups of the tenant get the name; another
	// tenant's do not.
	groups := []sdk.Group{
		{ID: "a", Properties: map[string]any{"tenant_id": gTenant}},
		{ID: "b", Properties: map[string]any{"tenant_id": "00000000-0000-0000-0000-0000000000bb"}},
	}
	setTenantName(groups, gTenant, "")
	if groups[0].Properties["tenant_name"] != nil {
		t.Error("an unknown tenant name must not be set")
	}
	setTenantName(groups, gTenant, "LAB-TENANT")
	if groups[0].Properties["tenant_name"] != "LAB-TENANT" || groups[1].Properties["tenant_name"] != nil {
		t.Errorf("tenant names = %v / %v", groups[0].Properties, groups[1].Properties)
	}
}

// TestResolveTenantName covers the optional, non-fatal tenant name
// lookup: az account show's tenantDisplayName first, then the ARM
// tenants API; a name is used only when it is a real display name,
// never the ID.
func TestResolveTenantName(t *testing.T) {
	api := func(tenants string) string { return `{"value":[` + tenants + `]}` }
	lab := `{"tenantId":"` + gTenant + `","displayName":"LAB-TENANT-API"}`
	other := `{"tenantId":"00000000-0000-0000-0000-0000000000bb","displayName":"LAB-PARTNER"}`
	cases := map[string]struct {
		fromShow, out, stderr, want string
		others                      int
	}{
		"account show wins":     {fromShow: "LAB-TENANT", out: api(lab), want: "LAB-TENANT", others: 1},
		"tenants API fallback":  {out: api(lab + "," + other), want: "LAB-TENANT-API", others: 2},
		"show name is the ID":   {fromShow: gTenant, out: api(lab), want: "LAB-TENANT-API", others: 1},
		"not listed":            {out: api(other), others: 1},
		"API name empty":        {out: api(`{"tenantId":"` + gTenant + `","displayName":""}`)},
		"API name is the ID":    {out: api(`{"tenantId":"` + gTenant + `","displayName":"` + gTenant + `"}`)},
		"API fails, no show":    {stderr: "ERROR: (AuthorizationFailed) not allowed"},
		"API fails, show known": {fromShow: "LAB-TENANT", stderr: "ERROR: (AuthorizationFailed) not allowed", want: "LAB-TENANT", others: 1},
	}
	for name, tc := range cases {
		c := NewClient(gSubID, testharness.NewTestContext(t).Logger)
		c.azExec = func(args []string) ([]byte, string, error) {
			if tc.stderr != "" {
				return nil, tc.stderr, errors.New("exit status 1")
			}
			return []byte(tc.out), "", nil
		}
		if got := c.resolveTenantName(gTenant, tc.fromShow); got != tc.want {
			t.Errorf("%s: tenant name = %q, want %q", name, got, tc.want)
		}
		if len(c.tenantNames) != tc.others {
			t.Errorf("%s: catalog tenant names = %v, want %d entries", name, c.tenantNames, tc.others)
		}
	}
}
