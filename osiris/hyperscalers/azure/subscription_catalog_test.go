// subscription_catalog_test.go - Tests for the Azure subscription
// catalog (metadata["osiris.azure"]) of the OSIRIS JSON Microsoft Azure
// producer: exported versus reference-only coverage, names and tenants
// only from authoritative sources, and no substitution of
// the exported tenant.

package azure

import (
	"strings"
	"testing"

	"go.osirisjson.org/producers/pkg/sdk"
)

func TestBuildSubscriptionCatalog(t *testing.T) {
	const unknown = "00000000-0000-0000-0000-000000000003"
	const rawOnly = "00000000-0000-0000-0000-000000000004"
	doc := &sdk.Document{}
	doc.Topology.Resources = []sdk.Resource{
		{
			ID:       "azure::kv",
			Provider: sdk.Provider{Account: gSubID, NativeID: "/subscriptions/" + gSubID + "/resourceGroups/RG-LAB/providers/Microsoft.KeyVault/vaults/LAB-KV01"},
			Properties: map[string]any{"network_acls": map[string]any{"vnet_subnet_ids": []string{
				"/subscriptions/" + unknown + "/resourceGroups/RG-X/providers/Microsoft.Network/virtualNetworks/LAB-VNET03/subnets/LAB-SNET03",
			}}},
			Extensions: map[string]any{"osiris.azure.arm": map[string]any{"body": `{"id":"/subscriptions/` + rawOnly + `/resourceGroups/RG/providers/X/y/z"}`}},
		},
		// Stub owned by another subscription known to the entity list.
		{ID: "azure::stub", Provider: sdk.Provider{Account: gRemote, NativeID: "/subscriptions/" + gRemote + "/resourceGroups/RG-LAB/providers/Microsoft.Network/virtualNetworks/LAB-VNET02"}},
	}
	cat := buildSubscriptionCatalog(doc, gSub, gEntities(), nil, map[string]string{gTenant: "LAB-TENANT"})

	want := map[string]catalogSubscription{
		gSubID:  {Coverage: "exported", Name: "LAB-SUB01", TenantID: gTenant},
		gRemote: {Coverage: "reference_only", Name: "LAB-SUB02", TenantID: gTenant},
		unknown: {Coverage: "reference_only"},
	}
	if len(cat.Subscriptions) != len(want) {
		t.Errorf("catalog subscriptions = %v, want exactly %d (raw bodies are not scanned)", cat.Subscriptions, len(want))
	}
	for id, w := range want {
		if got := cat.Subscriptions[id]; got != w {
			t.Errorf("%s = %+v, want %+v", id, got, w)
		}
	}
	// One tenant, named once in tenants; subscriptions only
	// reference it.
	if len(cat.Tenants) != 1 || cat.Tenants[gTenant] != (catalogTenant{Name: "LAB-TENANT"}) {
		t.Errorf("tenants = %+v", cat.Tenants)
	}

	// No tenant names known, and an exported subscription whose name is
	// its ID: nothing is invented and no ID stands in for a name.
	bare := gSub
	bare.DisplayName = gSubID
	cat = buildSubscriptionCatalog(doc, bare, nil, nil, nil)
	if e := cat.Subscriptions[gSubID]; e.Name != "" || e.TenantID != gTenant {
		t.Errorf("exported entry = %+v, want no name, its tenant", e)
	}
	if e := cat.Subscriptions[gRemote]; e != (catalogSubscription{Coverage: "reference_only"}) {
		t.Errorf("reference without an entity = %+v, want coverage only (no exported tenant substituted)", e)
	}
	if len(cat.Tenants) != 1 || cat.Tenants[gTenant].Name != "" {
		t.Errorf("tenants = %+v, want the exported tenant without a name", cat.Tenants)
	}
}

// TestCatalogCrossTenantReference: a referenced subscription the entity
// list does not know is named, with its own tenant, from az account
// list --all; the exported tenant is not assumed for it.
func TestCatalogCrossTenantReference(t *testing.T) {
	const partnerSub = "00000000-0000-0000-0000-000000000004"
	const partnerTenant = "00000000-0000-0000-0000-0000000000bb"
	doc := &sdk.Document{}
	doc.Topology.Resources = []sdk.Resource{{
		ID:         "azure::kv",
		Provider:   sdk.Provider{Account: gSubID},
		Properties: map[string]any{"network_acls": map[string]any{"vnet_subnet_ids": []string{"/subscriptions/" + strings.ToUpper(partnerSub) + "/resourceGroups/RG/providers/Microsoft.Network/virtualNetworks/v/subnets/s"}}},
	}}
	accessible := []AccessibleSubscription{{ID: partnerSub, Name: "LAB-PARTNER-SUB", TenantID: partnerTenant}}
	cat := buildSubscriptionCatalog(doc, gSub, nil, accessible, map[string]string{partnerTenant: "LAB-PARTNER"})
	if e := cat.Subscriptions[partnerSub]; e != (catalogSubscription{Coverage: "reference_only", Name: "LAB-PARTNER-SUB", TenantID: partnerTenant}) {
		t.Errorf("cross-tenant entry = %+v", e)
	}
	if cat.Tenants[partnerTenant].Name != "LAB-PARTNER" || len(cat.Tenants) != 2 {
		t.Errorf("tenants = %+v, want the exported and the partner tenant", cat.Tenants)
	}
}
