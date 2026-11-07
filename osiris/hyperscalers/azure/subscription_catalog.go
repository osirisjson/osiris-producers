// subscription_catalog.go - Azure subscription catalog for the
// OSIRIS JSON Microsoft Azure producer.
//
// The catalog is emitted as metadata["osiris.azure"], a producer
// extension the schema permits on metadata (it is not an OSIRIS JSON
// v1.0 field):
//
//	"osiris.azure": {
//	  "tenants": {
//	    "<tenant id>": {
//	      "name": "<display name, when known>"
//	    }
//	  },
//	  "subscriptions": {
//	    "<subscription id>": {
//	      "coverage":  "exported" | "reference_only",
//	      "name":      "<display name, when known>",
//	      "tenant_id": "<owning tenant, when known>"
//	    }
//	  }
//	}
//
// tenants and subscriptions are sibling objects, keyed by lower-case
// Azure IDs (a key is never repeated inside its own record). tenants
// lists exactly the tenants the listed subscriptions belong to; a
// tenant's name is stated once, there. The exported tenant is
// subscriptions[<metadata.scope.accounts[0]>].tenant_id.
//
// "exported" marks the subscription this document inventories (the one
// in metadata.scope.accounts). "reference_only" marks a subscription
// that only appears in ARM references of the document (a resource's
// provider.account, a stub, a peering, a network ACL subnet, a
// diagnostic destination...); its resources were not inventoried.
//
// Names and tenants come only from authoritative read-only sources:
// az account show (the exported subscription and its tenantDisplayName)
// the management group entity list (other subscriptions of the same
// tenant), az account list --all (subscriptions the caller can see in
// any tenant, so cross-tenant references can be named) and the ARM
// tenants API through az rest (tenant names).
// Anything unknown is omitted; an ID is never used as a name, and the
// exported subscription's tenant is never assumed for
// another subscription.

package azure

import (
	"regexp"
	"strings"

	"go.osirisjson.org/producers/pkg/sdk"
)

// Subscription catalog coverage values.
const (
	coverageExported      = "exported"
	coverageReferenceOnly = "reference_only"
)

// armSubscriptionRe matches the subscription segment of an ARM ID.
var armSubscriptionRe = regexp.MustCompile(`(?i)/subscriptions/([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})`)

// referencedSubscriptions returns the lower-case IDs of every
// subscription the document's resources and connections name:
// provider.account, and the subscription segment of any ARM ID in
// native_id, properties and extensions.
// Raw ARM bodies are not scanned, so the catalog does not
// depend on --include-raw-body.
func referencedSubscriptions(doc *sdk.Document) map[string]bool {
	found := map[string]bool{}
	var walk func(v any)
	walk = func(v any) {
		switch t := v.(type) {
		case string:
			for _, m := range armSubscriptionRe.FindAllStringSubmatch(t, -1) {
				found[strings.ToLower(m[1])] = true
			}
		case []string:
			for _, s := range t {
				walk(s)
			}
		case []any:
			for _, x := range t {
				walk(x)
			}
		case map[string]any:
			for _, x := range t {
				walk(x)
			}
		case []map[string]any:
			for _, x := range t {
				walk(x)
			}
		}
	}
	extensions := func(ext map[string]any) {
		for k, v := range ext {
			if k != "osiris.azure.arm" {
				walk(v)
			}
		}
	}
	for _, r := range doc.Topology.Resources {
		if r.Provider.Account != "" {
			found[strings.ToLower(r.Provider.Account)] = true
		}
		walk(r.Provider.NativeID)
		walk(r.Properties)
		extensions(r.Extensions)
	}
	for _, c := range doc.Topology.Connections {
		walk(c.Properties)
		extensions(c.Extensions)
	}
	return found
}

// azureCatalog is the metadata["osiris.azure"] value (field order
// is the emitted order).
type azureCatalog struct {
	Tenants       map[string]catalogTenant       `json:"tenants,omitempty"`
	Subscriptions map[string]catalogSubscription `json:"subscriptions"`
}

// catalogTenant is one tenant of the catalog, keyed by its ID.
type catalogTenant struct {
	Name string `json:"name,omitempty"`
}

// catalogSubscription is one subscription of the catalog.
type catalogSubscription struct {
	Coverage string `json:"coverage"`
	Name     string `json:"name,omitempty"`
	TenantID string `json:"tenant_id,omitempty"`
}

// buildSubscriptionCatalog returns the metadata["osiris.azure"] value
// for doc: the exported subscription and every referenced subscription,
// and the tenants they belong to, with names and tenants only where an
// authoritative source states them.
func buildSubscriptionCatalog(doc *sdk.Document, sub SubscriptionInfo, entities []MGEntity, accessible []AccessibleSubscription, tenantNames map[string]string) azureCatalog {
	// Authoritative name and tenant per subscription: the management
	// group entity list first, then az account list --all
	// for what it lacks.
	type ref struct{ name, tenant string }
	known := map[string]ref{}
	for _, e := range entities {
		if strings.EqualFold(e.Type, "/subscriptions") && e.Name != "" {
			known[canonicalSubscriptionID(e.Name)] = ref{e.DisplayName, e.TenantID}
		}
	}
	for _, a := range accessible {
		id := canonicalSubscriptionID(a.ID)
		if id == "" {
			continue
		}
		r := known[id]
		if r.name == "" {
			r.name = a.Name
		}
		if r.tenant == "" {
			r.tenant = a.TenantID
		}
		known[id] = r
	}
	cat := azureCatalog{Subscriptions: map[string]catalogSubscription{}}
	add := func(id, coverage, name, tenantID string) {
		if strings.EqualFold(name, id) {
			name = ""
		}
		tenantID = strings.ToLower(strings.TrimSpace(tenantID))
		cat.Subscriptions[id] = catalogSubscription{Coverage: coverage, Name: name, TenantID: tenantID}
		if tenantID == "" {
			return
		}
		if cat.Tenants == nil {
			cat.Tenants = map[string]catalogTenant{}
		}
		cat.Tenants[tenantID] = catalogTenant{Name: tenantNames[tenantID]}
	}

	exported := canonicalSubscriptionID(sub.SubscriptionID)
	for id := range referencedSubscriptions(doc) {
		if id == exported {
			continue
		}
		r := known[id]
		add(id, coverageReferenceOnly, r.name, r.tenant)
	}
	if exported != "" {
		add(exported, coverageExported, sub.DisplayName, sub.TenantID)
	}
	return cat
}
