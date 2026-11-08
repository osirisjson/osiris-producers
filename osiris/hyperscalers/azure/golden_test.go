// golden_test.go - Golden document tests for the OSIRIS JSON Microsoft
// Azure producer.
//
// Each scenario under testdata/osiris/hyperscalers/azure/<scenario>/
// covers one synthetic subscription, or one per subdirectory
// (<scenario>/<subscription>/) when the scenario spans several
// subscriptions exported as separate documents. az/*.json hold the
// JSON the az CLI returns for each command and commands.json maps
// commands to those files: the first rule whose "prefix" starts the
// command line, or whose "contains" (used for Resource Graph queries)
// occurs in it, wins; a rule without a file and any unmatched command
// answer an empty result.
// The full Collect pipeline runs against them with a fixed clock, once
// per --purpose flag and the document must match
// purpose/golden_<purpose>.json byte for byte, be identical across two
// runs and contain none of the planted secrets.
//
// Regenerate after an intended output change, then validate:
//
//	1. OSIRIS_UPDATE_GOLDEN=1 go test ./osiris/hyperscalers/azure/ -run TestGolden
//	2. ./scripts/validate_golden.sh testdata/osiris/hyperscalers/azure

package azure

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"go.osirisjson.org/producers/pkg/sdk"
	"go.osirisjson.org/producers/pkg/testharness"
)

const goldenRoot = "../../../testdata/osiris/hyperscalers/azure"

// fixtureAZ returns an az executor answering from a scenario directory.
func fixtureAZ(t *testing.T, dir string) func([]string) ([]byte, string, error) {
	t.Helper()
	var manifest struct {
		Rules []struct {
			Prefix   string  `json:"prefix"`
			Contains string  `json:"contains"`
			File     *string `json:"file"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(testharness.LoadFixture(t, filepath.Join(dir, "commands.json")), &manifest); err != nil {
		t.Fatalf("commands.json: %v", err)
	}
	files := map[string][]byte{}
	for _, r := range manifest.Rules {
		if r.File != nil {
			files[*r.File] = testharness.LoadFixture(t, filepath.Join(dir, *r.File))
		}
	}
	return func(args []string) ([]byte, string, error) {
		line := strings.Join(args, " ")
		for _, r := range manifest.Rules {
			if (r.Prefix != "" && strings.HasPrefix(line, r.Prefix+" ")) || (r.Contains != "" && strings.Contains(line, r.Contains)) {
				if r.File != nil {
					return files[*r.File], "", nil
				}
				break
			}
		}
		switch {
		case args[0] == "graph":
			return []byte(`{"count":0,"data":[]}`), "", nil
		case args[0] == "rest":
			return []byte(`{"value":[]}`), "", nil
		case strings.Contains(line, " show"):
			return []byte(`{}`), "", nil
		}
		return []byte(`[]`), "", nil
	}
}

// collectScenario runs Collect for a scenario with a fixed clock.
func collectScenario(t *testing.T, dir, purpose string, rawBody bool) *sdk.Document {
	t.Helper()
	return collectScenarioWith(t, dir, purpose, rawBody, nil)
}

// collectScenarioWith is collectScenario with an optional wrapper
// around the fixture executor (to make a command fail, for example).
func collectScenarioWith(t *testing.T, dir, purpose string, rawBody bool, wrap func(func([]string) ([]byte, string, error)) func([]string) ([]byte, string, error)) *sdk.Document {
	t.Helper()
	clock := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	ctx := testharness.NewTestContext(t, testharness.WithClock(clock))
	var acct SubscriptionInfo
	if err := json.Unmarshal(testharness.LoadFixture(t, filepath.Join(dir, "az", "account-show.json")), &acct); err != nil {
		t.Fatal(err)
	}
	client := NewClient(acct.SubscriptionID, ctx.Logger)
	client.azExec = fixtureAZ(t, dir)
	if wrap != nil {
		client.azExec = wrap(client.azExec)
	}
	p := NewProducer(SubscriptionTarget{SubscriptionID: acct.SubscriptionID, SubscriptionName: acct.DisplayName},
		&Config{Purpose: purpose, IncludeRawBody: rawBody})
	p.client = client
	doc, err := p.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	return doc
}

// scenarioManifests returns every commands.json under goldenRoot: one
// per single-subscription scenario (<scenario>/commands.json) and one
// per subscription of a multi-subscription scenario
// (<scenario>/<subscription>/commands.json).
func scenarioManifests() ([]string, error) {
	var out []string
	for _, pattern := range []string{"*/commands.json", "*/*/commands.json"} {
		m, err := filepath.Glob(filepath.Join(goldenRoot, pattern))
		if err != nil {
			return nil, err
		}
		out = append(out, m...)
	}
	sort.Strings(out)
	return out, nil
}

func TestGolden(t *testing.T) {
	scenarios, err := scenarioManifests()
	if err != nil || len(scenarios) == 0 {
		t.Fatalf("no scenarios under %s: %v", goldenRoot, err)
	}
	// Credentials planted in the fixtures that MUST never be emitted.
	planted := []string{"sig=changeme", "changeme-apiconn", "InstrumentationKey=00000000", "0000000000cc", "00000000beef"}
	for _, manifest := range scenarios {
		dir := filepath.Dir(manifest)
		for _, tc := range []struct {
			purpose string
			rawBody bool
		}{{"documentation", false}, {"audit", true}} {
			name, _ := filepath.Rel(goldenRoot, dir)
			t.Run(filepath.ToSlash(name)+"/"+tc.purpose, func(t *testing.T) {
				doc := collectScenario(t, dir, tc.purpose, tc.rawBody)
				first, err := sdk.MarshalDocument(doc)
				if err != nil {
					t.Fatal(err)
				}
				again, _ := sdk.MarshalDocument(collectScenario(t, dir, tc.purpose, tc.rawBody))
				if !bytes.Equal(first, again) {
					t.Error("output is not deterministic across two runs")
				}
				for _, s := range planted {
					if bytes.Contains(first, []byte(s)) {
						t.Errorf("planted credential %q emitted", s)
					}
				}
				testharness.AssertGolden(t, doc, filepath.Join(dir, "purpose", "golden_"+tc.purpose+".json"))
			})
		}
	}
}

// hubDir and spokeDir are the two subscriptions of the hub-spoke
// Microsoft Azure scenario.
var (
	hubDir   = filepath.Join(goldenRoot, "hub-spoke", "hub")
	spokeDir = filepath.Join(goldenRoot, "hub-spoke", "spoke")
)

// TestGoldenScenarioCoverage asserts that each hub-spoke document still
// exercises every relationship family it was written for, so a fixture
// or builder regression cannot silently shrink the golden documents.
func TestGoldenScenarioCoverage(t *testing.T) {
	for _, tc := range []struct {
		dir  string
		want []string
	}{
		{hubDir, []string{
			"contains virtualNetworks->subnets",
			"contains virtualMachines->networkInterfaces",
			"dependency.storage virtualMachines->disks",
			"network networkInterfaces->subnets",
			"network networkInterfaces->publicIPAddresses",
			"network.peering virtualNetworks->virtualNetworks",
			"osiris.azure.nsg.association subnets->networkSecurityGroups",
			"osiris.azure.nsg.association networkInterfaces->networkSecurityGroups",
			"osiris.azure.nsg.association virtualMachineScaleSets->networkSecurityGroups",
			"osiris.azure.routetable.association subnets->routeTables",
			"osiris.azure.route.nexthop routeTables->loadBalancers",
			"network loadBalancers->networkInterfaces",
			"network loadBalancers->subnets",
			"network loadBalancers->publicIPAddresses",
			"network loadBalancers->virtualMachineScaleSets",
			"network virtualMachineScaleSets->subnets",
			"dependency autoscalesettings->virtualMachineScaleSets",
			"network privateEndpoints->automationAccounts",
			"network privateEndpoints->networkInterfaces",
			"network privateEndpoints->subnets",
			"network virtualHubs->subnets",
			"network virtualHubs->publicIPAddresses",
			"network.bgp networkInterfaces->virtualHubs",
			"network virtualNetworkGateways->subnets",
			"network virtualNetworkGateways->publicIPAddresses",
			"network virtualNetworkGateways->expressRouteCircuits",
			"osiris.azure.expressroute.connection.endpoint connections->virtualNetworkGateways",
			"osiris.azure.expressroute.connection.endpoint connections->expressRouteCircuits",
			"network dnsResolvers->virtualNetworks",
			"dependency dnsForwardingRulesets->dnsResolvers",
			"dataflow virtualMachines->vaults",
			"dependency dataCollectionRules->workspaces",
			"dependency metricAlerts->actionGroups",
			"dependency metricAlerts->loadBalancers",
			"dependency metricAlerts->virtualHubs",
			"dependency metricAlerts->storageAccounts",
			"dependency metricAlerts->vaults",
			"dependency networkSecurityGroups->storageAccounts",
			"dependency networkSecurityGroups->workspaces",
			"dependency virtualNetworks->workspaces",
			"dependency loadBalancers->workspaces",
			"dependency publicIPAddresses->workspaces",
			"dependency vaults->workspaces",
			"dependency namespaces->workspaces",
			"dependency workspaces->workspaces",
		}},
		{spokeDir, []string{
			"contains virtualNetworks->subnets",
			"contains virtualMachines->networkInterfaces",
			"contains serverfarms->sites",
			"contains managedInstances->databases",
			"dependency.storage virtualMachines->disks",
			"network networkInterfaces->subnets",
			"network.peering virtualNetworks->virtualNetworks",
			"osiris.azure.nsg.association subnets->networkSecurityGroups",
			"osiris.azure.routetable.association subnets->routeTables",
			"osiris.azure.asg.association networkInterfaces->applicationSecurityGroups",
			"network privateEndpoints->networkInterfaces",
			"network privateEndpoints->subnets",
			"network privateEndpoints->sites",
			"network privateEndpoints->vaults",
			"network privateEndpoints->storageAccounts",
			"network privateEndpoints->registries",
			"network sites->subnets",
			"network managedInstances->subnets",
			"dependency SqlVirtualMachines->virtualMachines",
			"dependency autoscalesettings->serverfarms",
			"dataflow virtualMachines->vaults",
			"dependency metricAlerts->actionGroups",
			"dependency metricAlerts->sites",
			"dependency metricAlerts->managedInstances",
			"dependency metricAlerts->storageAccounts",
			"dependency metricAlerts->vaults",
		}},
	} {
		name := filepath.Base(tc.dir)
		doc := collectScenario(t, tc.dir, "audit", true)
		types := map[string]string{}
		for _, r := range doc.Topology.Resources {
			types[r.ID] = r.Provider.Type
		}
		got := map[string]int{}
		for _, c := range doc.Topology.Connections {
			short := func(id string) string { return types[id][strings.LastIndex(types[id], "/")+1:] }
			got[c.Type+" "+short(c.Source)+"->"+short(c.Target)]++
		}
		failed := false
		for _, w := range tc.want {
			if got[w] == 0 {
				t.Errorf("%s: scenario no longer produces %q", name, w)
				failed = true
			}
		}
		if failed {
			t.Logf("%s: connections by class: %v", name, got)
		}
		for _, r := range doc.Topology.Resources {
			if r.Provider.Type == "Microsoft.Compute/virtualMachines" && r.Status != "active" {
				t.Errorf("%s: VM %s status = %q, want active from the Resource Graph power state", name, r.Name, r.Status)
			}
		}
		assertProviderIdentity(t, doc)
		if tc.dir == hubDir {
			assertRoutingSemantics(t, doc, types)
		}
	}
}

// assertRoutingSemantics checks the hybrid-routing acceptance rules on
// the hub document: the ExpressRoute connection links its gateway and
// circuit and its connection resource reaches both ends, no
// Route Server <-> gateway route exchange is inferred (branch-to-branch
// is only stated as a property), BGP peers resolve only to a NIC
// holding the peer IP and no semantic edge is duplicated.
func assertRoutingSemantics(t *testing.T, doc *sdk.Document, types map[string]string) {
	t.Helper()
	byID := map[string]sdk.Resource{}
	for _, r := range doc.Topology.Resources {
		byID[r.ID] = r
	}
	semantic := map[string]int{}
	gwCircuit := 0
	endpoints := map[string]map[string]string{} // connection resource -> role -> target
	dataPath := map[string][2]string{}          // connection resource -> gateway, circuit
	for _, c := range doc.Topology.Connections {
		semantic[c.Type+"|"+c.Direction+"|"+c.Source+"|"+c.Target]++
		st, tt := types[c.Source], types[c.Target]
		if st == "Microsoft.Network/virtualHubs" && tt == "Microsoft.Network/virtualNetworkGateways" ||
			tt == "Microsoft.Network/virtualHubs" && st == "Microsoft.Network/virtualNetworkGateways" {
			t.Errorf("Route Server <-> gateway route exchange inferred: %+v", c)
		}
		if c.Type == connERConnectionEndpoint {
			if endpoints[c.Source] == nil {
				endpoints[c.Source] = map[string]string{}
			}
			endpoints[c.Source][c.Properties["endpoint_role"].(string)] = c.Target
		}
		if st == "Microsoft.Network/virtualNetworkGateways" && tt == "Microsoft.Network/expressRouteCircuits" {
			gwCircuit++
			cid, _ := c.Properties["connection_id"].(string)
			dataPath[cid] = [2]string{c.Source, c.Target}
			if conn, ok := byID[cid]; !ok || conn.Type != "osiris.azure.expressroute.connection" {
				t.Errorf("gateway <-> circuit edge must name its ExpressRoute connection resource, got %q", cid)
			}
		}
	}
	for k, n := range semantic {
		if n > 1 {
			t.Errorf("duplicate semantic edge %s (x%d)", k, n)
		}
	}
	if gwCircuit != 1 {
		t.Errorf("want 1 gateway <-> circuit edge (one ExpressRoute connection), got %d", gwCircuit)
	}
	// Every ExpressRoute connection resource is traversable: it reaches
	// the gateway and the circuit of the data-path edge that names it.
	for _, r := range doc.Topology.Resources {
		if r.Type != "osiris.azure.expressroute.connection" {
			continue
		}
		ends, path := endpoints[r.ID], dataPath[r.ID]
		if ends["gateway"] == "" || ends["gateway"] != path[0] || ends["circuit"] != path[1] {
			t.Errorf("connection resource %s endpoints %v do not match its gateway <-> circuit edge %v", r.Name, ends, path)
		}
	}
	servers := 0
	for _, r := range doc.Topology.Resources {
		if r.Provider.Type != "Microsoft.Network/virtualHubs" {
			continue
		}
		servers++
		if v, ok := r.Properties["allow_branch_to_branch_traffic"]; !ok || v != true {
			t.Errorf("allow_branch_to_branch_traffic must be stated as true, got %v", v)
		}
		peers, _ := r.Extensions[extensionNamespace].(map[string]any)["bgp_connections"].([]map[string]any)
		outcome := map[string]string{}
		for _, p := range peers {
			outcome[p["name"].(string)], _ = p["resolution"].(string)
			if p["resolution"] != "resolved" && p["resource_id"] != nil {
				t.Errorf("unresolved peer carries an association: %v", p)
			}
		}
		want := map[string]string{"LAB-PEER-NVA01": "resolved", "LAB-PEER-NVA02": "resolved", "LAB-PEER-UNKNOWN": "unresolved"}
		for name, o := range want {
			if outcome[name] != o {
				t.Errorf("peer %s resolution = %q, want %q", name, outcome[name], o)
			}
		}
	}
	if servers != 1 {
		t.Errorf("want 1 Route Server, got %d", servers)
	}
}

// assertProviderIdentity checks provider identity on every resource:
// provider.account is the canonical lower-case owner of its ARM ID and
// provider.subscription/system are never emitted.
func assertProviderIdentity(t *testing.T, doc *sdk.Document) {
	t.Helper()
	for _, r := range doc.Topology.Resources {
		if owner := extractSubscriptionID(r.Provider.NativeID); owner != "" && !strings.EqualFold(owner, r.Provider.Account) {
			t.Errorf("%s: provider.account %q is not the owner %q of its ARM ID", r.Name, r.Provider.Account, owner)
		}
		if r.Provider.Account != strings.ToLower(r.Provider.Account) {
			t.Errorf("%s: provider.account %q is not canonical lower case", r.Name, r.Provider.Account)
		}
		if r.Provider.Subscription != "" || r.Provider.System != "" {
			t.Errorf("%s: provider.subscription/system must not be emitted", r.Name)
		}
	}
}

// TestGoldenTenantNameUnavailable runs the hub document with the
// optional tenant name lookup failing: the document must still build,
// carry no tenant name anywhere (no ID used as a name) and keep every
// resource, connection and group ID of the normal run.
func TestGoldenTenantNameUnavailable(t *testing.T) {
	dir := hubDir
	fail := func(next func([]string) ([]byte, string, error)) func([]string) ([]byte, string, error) {
		return func(args []string) ([]byte, string, error) {
			line := strings.Join(args, " ")
			if strings.Contains(line, "/tenants?api-version") {
				return nil, "ERROR: (AuthorizationFailed) not allowed", errors.New("exit status 1")
			}
			out, stderr, err := next(args)
			if strings.HasPrefix(line, "account show") && err == nil {
				// An az version that does not report tenantDisplayName.
				var m map[string]any
				if json.Unmarshal(out, &m) == nil {
					delete(m, "tenantDisplayName")
					out, _ = json.Marshal(m)
				}
			}
			return out, stderr, err
		}
	}
	with := collectScenario(t, dir, "audit", true)
	without := collectScenarioWith(t, dir, "audit", true, fail)

	cat := without.Metadata.Extensions["osiris.azure"].(azureCatalog)
	if len(cat.Tenants) == 0 {
		t.Error("the tenant ID must still be listed without its name")
	}
	for id, tn := range cat.Tenants {
		if tn.Name != "" {
			t.Errorf("tenant %s has a name although none was resolved", id)
		}
	}
	if len(without.Metadata.Scope.Accounts) != 1 {
		t.Errorf("scope.accounts = %v, want the exported subscription only", without.Metadata.Scope.Accounts)
	}
	for _, g := range without.Topology.Groups {
		if _, ok := g.Properties["tenant_name"]; ok {
			t.Errorf("group %s has tenant_name without a resolved tenant name", g.Name)
		}
	}
	ids := func(d *sdk.Document) []string {
		var out []string
		for _, r := range d.Topology.Resources {
			out = append(out, "r:"+r.ID)
		}
		for _, c := range d.Topology.Connections {
			out = append(out, "c:"+c.ID)
		}
		for _, g := range d.Topology.Groups {
			out = append(out, "g:"+g.ID)
		}
		return out
	}
	if a, b := ids(with), ids(without); strings.Join(a, "\n") != strings.Join(b, "\n") {
		t.Errorf("IDs differ when the tenant name is unavailable (%d vs %d)", len(a), len(b))
	}
	if c := with.Metadata.Extensions["osiris.azure"].(azureCatalog); c.Tenants["00000000-0000-0000-0000-0000000000aa"].Name == "" {
		t.Error("the normal run must resolve the tenant name from the fixture")
	}
}

// TestGoldenSubscriptionCatalog checks metadata["osiris.azure"] in the
// emitted bytes of the hub document: tenants and
// subscriptions are siblings with unique, lower-case Azure ID keys, a
// tenant record does not repeat its key, scope.accounts holds only the
// exported subscription and each kind of reference is resolved only
// from authoritative evidence:
//   - 0011 exported (named by az account show);
//   - 0012 same-tenant foreign reference, the spoke (peering stub;
//     named by the management group entity list);
//   - 0014 cross-tenant foreign reference (Key Vault firewall subnet;
//     named by az account list --all, its own tenant named by the
//     tenants API);
//   - 0015 unresolved reference (storage firewall subnet; no source:
//     coverage only).
func TestGoldenSubscriptionCatalog(t *testing.T) {
	doc := collectScenario(t, hubDir, "audit", true)
	raw, err := sdk.MarshalDocument(doc)
	if err != nil {
		t.Fatal(err)
	}
	var top struct {
		Metadata map[string]json.RawMessage `json:"metadata"`
	}
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatal(err)
	}
	catRaw := top.Metadata["osiris.azure"]
	if len(catRaw) == 0 {
		t.Fatal(`metadata["osiris.azure"] missing`)
	}
	assertUniqueKeys(t, catRaw)

	var cat struct {
		Tenants       map[string]map[string]any `json:"tenants"`
		Subscriptions map[string]map[string]any `json:"subscriptions"`
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(catRaw, &members); err != nil || json.Unmarshal(catRaw, &cat) != nil {
		t.Fatalf("catalog: %v", err)
	}
	if len(members) != 2 || members["tenants"] == nil || members["subscriptions"] == nil {
		t.Errorf("catalog members = %v, want exactly the siblings tenants and subscriptions", keysOf(members))
	}
	guid := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	for id, rec := range cat.Tenants {
		if !guid.MatchString(id) {
			t.Errorf("tenant key %q is not a lower-case Azure ID", id)
		}
		if _, nested := rec["subscriptions"]; nested {
			t.Errorf("tenant %s nests subscriptions", id)
		}
		if _, rep := rec["tenant_id"]; rep {
			t.Errorf("tenant %s repeats its key as tenant_id", id)
		}
	}
	for id, rec := range cat.Subscriptions {
		if !guid.MatchString(id) {
			t.Errorf("subscription key %q is not a lower-case Azure ID", id)
		}
		if tid, ok := rec["tenant_id"].(string); ok {
			if _, listed := cat.Tenants[tid]; !listed {
				t.Errorf("subscription %s references tenant %s missing from tenants", id, tid)
			}
		}
	}

	const tA, tB = "00000000-0000-0000-0000-0000000000aa", "00000000-0000-0000-0000-0000000000bb"
	want := map[string]map[string]any{
		"00000000-0000-0000-0000-000000000011": {"coverage": "exported", "name": "LAB-HUB-SUB", "tenant_id": tA},
		"00000000-0000-0000-0000-000000000012": {"coverage": "reference_only", "name": "LAB-SPOKE-SUB", "tenant_id": tA},
		"00000000-0000-0000-0000-000000000014": {"coverage": "reference_only", "name": "LAB-PARTNER-SUB", "tenant_id": tB},
		"00000000-0000-0000-0000-000000000015": {"coverage": "reference_only"},
	}
	if len(cat.Subscriptions) != len(want) {
		t.Errorf("catalog subscriptions = %v, want %d", keysOf(cat.Subscriptions), len(want))
	}
	for id, w := range want {
		got := cat.Subscriptions[id]
		if len(got) != len(w) {
			t.Errorf("subscription %s = %v, want %v", id, got, w)
			continue
		}
		for k, v := range w {
			if got[k] != v {
				t.Errorf("subscription %s.%s = %v, want %v", id, k, got[k], v)
			}
		}
	}
	if len(cat.Tenants) != 2 || cat.Tenants[tA]["name"] != "LAB-TENANT" || cat.Tenants[tB]["name"] != "LAB-PARTNER" {
		t.Errorf("tenants = %v", cat.Tenants)
	}

	// Scope holds only the exported subscription.
	if a := doc.Metadata.Scope.Accounts; len(a) != 1 || a[0] != "00000000-0000-0000-0000-000000000011" {
		t.Errorf("scope.accounts = %v", a)
	}
	// Provider identity on every resource: native_id kept,
	// account = the owner in it, tenant only where known (never the
	// partner tenant, which owns no exported resource or stub here).
	for _, r := range doc.Topology.Resources {
		if r.Provider.NativeID == "" || r.Provider.Account != canonicalSubscriptionID(extractSubscriptionID(r.Provider.NativeID)) {
			t.Errorf("%s: provider native_id %q / account %q", r.Name, r.Provider.NativeID, r.Provider.Account)
		}
		if r.Provider.Tenant != "" && r.Provider.Tenant != tA {
			t.Errorf("%s: unexpected tenant %q", r.Name, r.Provider.Tenant)
		}
		if r.Provider.Account == "00000000-0000-0000-0000-000000000011" && r.Provider.Tenant != tA {
			t.Errorf("%s: exported resource without its known tenant", r.Name)
		}
	}
}

// assertUniqueKeys fails if any JSON object in raw has a duplicate key
// (encoding/json would silently keep the last one).
func assertUniqueKeys(t *testing.T, raw []byte) {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	var walk func() error
	walk = func() error {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		switch tok {
		case json.Delim('{'):
			seen := map[string]bool{}
			for dec.More() {
				k, err := dec.Token()
				if err != nil {
					return err
				}
				key := k.(string)
				if seen[key] {
					t.Errorf("duplicate key %q", key)
				}
				seen[key] = true
				if err := walk(); err != nil {
					return err
				}
			}
			_, err = dec.Token()
			return err
		case json.Delim('['):
			for dec.More() {
				if err := walk(); err != nil {
					return err
				}
			}
			_, err = dec.Token()
			return err
		}
		return nil
	}
	if err := walk(); err != nil {
		t.Fatal(err)
	}
}

func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestGoldenAuditWithoutRawBody: without --include-raw-body no ARM body
// is emitted and none is scanned by the builder (they are removed
// before the build), so a body that would never be emitted cannot abort
// the document.
func TestGoldenAuditWithoutRawBody(t *testing.T) {
	for _, dir := range []string{hubDir, spokeDir} {
		doc := collectScenario(t, dir, "audit", false)
		for _, r := range doc.Topology.Resources {
			if _, has := r.Extensions["osiris.azure.arm"]; has {
				t.Errorf("%s: %s carries an ARM body without --include-raw-body", filepath.Base(dir), r.Name)
			}
		}
	}
}

// TestGoldenHubSpokeIdentityContract checks the hub-spoke scenario: two
// subscriptions exported independently, one document each. The producer
// never reads the other document; this test reads both only to assert
// that each one, on its own, carries what an OSIRIS JSON consumer needs
// to correlate them:
//   - scope.accounts holds only the exported subscription and the
//     catalog marks the other subscription reference_only;
//   - every stub owned by the other subscription has the id, native_id,
//     account and type of the real resource in the other document;
//   - the peering connection has the same id and endpoints in both;
//   - the shared management group root has the same group id in both;
//   - a route whose next hop exists only in the other document stays a
//     next_hop_ip property, with no cross-document edge.
func TestGoldenHubSpokeIdentityContract(t *testing.T) {
	hub := collectScenario(t, hubDir, "audit", true)
	spoke := collectScenario(t, spokeDir, "audit", true)

	type view struct {
		name    string
		account string
		doc     *sdk.Document
		res     map[string]sdk.Resource
		catalog map[string]catalogSubscription
	}
	load := func(name string, doc *sdk.Document) view {
		v := view{name: name, doc: doc, res: map[string]sdk.Resource{}}
		if len(doc.Metadata.Scope.Accounts) != 1 {
			t.Fatalf("%s: scope.accounts = %v, want only the exported subscription", name, doc.Metadata.Scope.Accounts)
		}
		v.account = doc.Metadata.Scope.Accounts[0]
		for _, r := range doc.Topology.Resources {
			v.res[r.ID] = r
		}
		raw, err := json.Marshal(doc.Metadata.Extensions["osiris.azure"])
		if err != nil {
			t.Fatal(err)
		}
		var cat struct {
			Subscriptions map[string]catalogSubscription `json:"subscriptions"`
		}
		if err := json.Unmarshal(raw, &cat); err != nil {
			t.Fatalf("%s: catalog: %v", name, err)
		}
		v.catalog = cat.Subscriptions
		return v
	}
	h, s := load("hub", hub), load("spoke", spoke)

	for _, pair := range [][2]view{{h, s}, {s, h}} {
		self, other := pair[0], pair[1]
		if c := self.catalog[self.account].Coverage; c != "exported" {
			t.Errorf("%s: own subscription coverage = %q, want exported", self.name, c)
		}
		if c := self.catalog[other.account].Coverage; c != "reference_only" {
			t.Errorf("%s: %s subscription coverage = %q, want reference_only", self.name, other.name, c)
		}
		stubs := 0
		for _, r := range self.doc.Topology.Resources {
			if r.Provider.Account != other.account {
				continue
			}
			stubs++
			real, ok := other.res[r.ID]
			if !ok {
				t.Errorf("%s: stub %s has no resource with the same id in %s", self.name, r.ID, other.name)
				continue
			}
			if real.Provider.NativeID != r.Provider.NativeID || real.Provider.Account != r.Provider.Account ||
				real.Provider.Type != r.Provider.Type || real.Type != r.Type {
				t.Errorf("%s: stub %s provider/type differ from %s: %+v vs %+v", self.name, r.ID, other.name, r.Provider, real.Provider)
			}
		}
		if stubs == 0 {
			t.Errorf("%s: no stub owned by the %s subscription", self.name, other.name)
		}
	}

	peerings := func(doc *sdk.Document) map[string][2]string {
		out := map[string][2]string{}
		for _, c := range doc.Topology.Connections {
			if c.Type == "network.peering" {
				ends := []string{c.Source, c.Target}
				sort.Strings(ends)
				out[c.ID] = [2]string{ends[0], ends[1]}
			}
		}
		return out
	}
	hp, sp := peerings(hub), peerings(spoke)
	if len(hp) != 1 || len(sp) != 1 {
		t.Fatalf("peerings: hub %d, spoke %d, want 1 each", len(hp), len(sp))
	}
	for id, ends := range hp {
		if sp[id] != ends {
			t.Errorf("peering %s: hub endpoints %v, spoke endpoints %v", id, ends, sp[id])
		}
	}

	groupIDs := func(doc *sdk.Document) map[string]string {
		out := map[string]string{}
		for _, g := range doc.Topology.Groups {
			if g.Type == "logical.managementgroup" {
				out[g.Name] = g.ID
			}
		}
		return out
	}
	if hg, sg := groupIDs(hub)["Tenant Root Group"], groupIDs(spoke)["Tenant Root Group"]; hg == "" || hg != sg {
		t.Errorf("root management group id: hub %q, spoke %q", hg, sg)
	}

	for _, c := range spoke.Topology.Connections {
		if c.Type == connRouteNextHop {
			t.Errorf("spoke: unexpected next-hop edge %s (next hop is only in the hub document)", c.ID)
		}
	}
	hops := 0
	for _, r := range spoke.Topology.Resources {
		if r.Provider.Type != "Microsoft.Network/routeTables" {
			continue
		}
		raw, _ := json.Marshal(r.Properties["routes"])
		hops += strings.Count(string(raw), `"next_hop_ip":"192.0.2.10"`)
	}
	if hops != 2 {
		t.Errorf("spoke: %d routes keep next_hop_ip 192.0.2.10, want 2", hops)
	}
	resolved := false
	for _, c := range hub.Topology.Connections {
		if c.Type == connRouteNextHop && h.res[c.Target].Provider.Type == "Microsoft.Network/loadBalancers" {
			resolved = true
		}
	}
	if !resolved {
		t.Error("hub: route table -> internal load balancer next hop not resolved inside the hub document")
	}
}
