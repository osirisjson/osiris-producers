// transform_assignments_test.go - Tests for the identity assignment,
// data collection rule association and SQL failover group relationships
// of the OSIRIS JSON Microsoft Azure producer.
//
// Every positive case names its ARM evidence; every negative case
// removes it while keeping names correlated and asserts that no edge
// is inferred.

package azure

import (
	"encoding/json"
	"strings"
	"testing"

	"go.osirisjson.org/producers/pkg/sdk"
)

const (
	tUAMI   = tRG + "/Microsoft.ManagedIdentity/userAssignedIdentities/LAB-UAMI01"
	tUAMI2  = tRG + "/Microsoft.ManagedIdentity/userAssignedIdentities/LAB-UAMI02"
	tAKS    = tRG + "/Microsoft.ContainerService/managedClusters/LAB-AKS01"
	tDCR    = tRG + "/Microsoft.Insights/dataCollectionRules/LAB-DCR01"
	tSQL2   = tRG + "/Microsoft.Sql/servers/lab-sql02"
	tFG1    = tSQL + "/failoverGroups/lab-fg01"
	tFG2    = tSQL2 + "/failoverGroups/lab-fg01"
	tSQLDB2 = tSQL2 + "/databases/LAB-DB01"
)

func resOf(ids ...string) []sdk.Resource {
	out := make([]sdk.Resource, 0, len(ids))
	for _, id := range ids {
		out = append(out, sdk.Resource{ID: resourceID("", id), Provider: sdk.Provider{NativeID: id}})
	}
	return out
}

func TestIdentityAssignment(t *testing.T) {
	envelopes := map[string]*GraphEnvelope{
		strings.ToLower(tVM1): {Identity: &azGraphIdentity{Type: "UserAssigned", UserAssignedIdentities: map[string]json.RawMessage{lowerRG(tUAMI): json.RawMessage(`{}`)}}},
		strings.ToLower(tAKS): {Identity: &azGraphIdentity{Type: "UserAssigned", UserAssignedIdentities: map[string]json.RawMessage{tUAMI: json.RawMessage(`{}`)}}},
		// System-assigned only: no user-assigned identity, no edge.
		strings.ToLower(tVM2): {Identity: &azGraphIdentity{Type: "SystemAssigned", PrincipalID: "00000000-0000-0000-0000-0000000000bb"}},
	}
	clusters := decode[[]AKSCluster](t, `[{"id":"`+tAKS+`","identityProfile":{"kubeletidentity":{"resourceId":"`+tUAMI+`"}}}]`)
	docIDs := idMapOf(tVM1, tVM2, tAKS, tUAMI)

	conns := TransformIdentityAssignmentConnections(resOf(tVM1, tVM2, tAKS, tUAMI), envelopes, clusters, docIDs)
	if len(conns) != 2 {
		t.Fatalf("expected VM01 -> UAMI and AKS -> UAMI, got %d: %+v", len(conns), conns)
	}
	byTarget := map[string]sdk.Connection{}
	for _, c := range conns {
		if c.Type != connIdentityAssignment || c.Target != resourceID("", tUAMI) || c.Direction != "forward" {
			t.Errorf("unexpected connection %+v", c)
		}
		byTarget[c.Source] = c
	}
	// The AKS cluster is assigned the same identity twice; one edge,
	// both roles.
	aks := byTarget[resourceID("", tAKS)]
	if roles, _ := aks.Properties["assigned_as"].([]string); len(roles) != 2 || roles[0] != "kubeletidentity" || roles[1] != "resource" {
		t.Errorf("AKS assigned_as = %v, want [kubeletidentity resource]", aks.Properties["assigned_as"])
	}

	// Negative: identity outside the document.
	envelopes[strings.ToLower(tVM1)].Identity.UserAssignedIdentities = map[string]json.RawMessage{tUAMI2: json.RawMessage(`{}`)}
	if c := TransformIdentityAssignmentConnections(resOf(tVM1), envelopes, nil, docIDs); len(c) != 0 {
		t.Errorf("identity absent from the document must yield no edge, got %+v", c)
	}
}

func TestDCRAssociation(t *testing.T) {
	assocs := decode[[]DCRAssociation](t, `[
		{"id":"`+lowerRG(tVM1)+`/providers/Microsoft.Insights/dataCollectionRuleAssociations/LAB-DCRA01","name":"LAB-DCRA01","dataCollectionRuleId":"`+tDCR+`"},
		{"id":"`+tVM2+`/providers/Microsoft.Insights/dataCollectionRuleAssociations/LAB-DCRA02","name":"LAB-DCRA02","dataCollectionRuleId":"`+tRG+`/Microsoft.Insights/dataCollectionRules/LAB-DCR99"}]`)
	conns, st := TransformDCRAssociationConnections(assocs, idMapOf(tVM1, tVM2, tDCR))
	c := onlyConn(t, conns, connDataflow, tVM1, tDCR)
	if st.Associations != 2 || st.RuleAbsent != 1 || st.ResourceAbsent != 0 {
		t.Errorf("stats = %+v, want 2 associations, 1 rule outside the document", st)
	}
	if c.Properties["association"] != "LAB-DCRA01" {
		t.Errorf("association = %v", c.Properties["association"])
	}
	if got := (DCRAssociation{ID: tDCR}).ResourceID(); got != "" {
		t.Errorf("non-association ID must have no associated resource, got %q", got)
	}
}

func TestSQLFailoverGroup(t *testing.T) {
	// Both servers list their own copy of the group, in flat az form.
	fg1 := decode[[]SQLFailoverGroup](t, `[{"id":"`+tFG1+`","name":"lab-fg01","replicationRole":"Primary","replicationState":"CATCH_UP",
		"partnerServers":[{"id":"`+lowerRG(tSQL2)+`","replicationRole":"Secondary"}],"databases":["`+tSQLDB+`"],
		"readWriteEndpoint":{"failoverPolicy":"Automatic","failoverWithDataLossGracePeriodMinutes":60}}]`)
	fg2 := decode[[]SQLFailoverGroup](t, `[{"id":"`+tFG2+`","name":"lab-fg01","replicationRole":"Secondary",
		"partnerServers":[{"id":"`+tSQL+`","replicationRole":"Primary"}],"databases":["`+tSQLDB2+`"]}]`)
	servers := []SQLServer{{ID: tSQL, Name: "lab-sql01", FailoverGroups: fg1}, {ID: tSQL2, Name: "lab-sql02", FailoverGroups: fg2}}

	res, fgIDs := TransformSQLFailoverGroups(servers, SubscriptionInfo{})
	if len(res) != 2 || res[0].Type != "osiris.azure.sql.failovergroup" || res[0].State != "CATCH_UP" || res[0].Status != "" {
		t.Fatalf("failover group resources = %+v", res)
	}
	if res[0].Properties["failover_policy"] != "Automatic" || res[0].Properties["grace_period_minutes"] != 60 {
		t.Errorf("failover group properties = %v", res[0].Properties)
	}

	conns := TransformSQLFailoverGroupConnections(servers, idMapOf(tSQL, tSQL2), fgIDs, idMapOf(tSQLDB, tSQLDB2))
	set := map[string]struct{}{}
	for _, id := range []string{tSQL, tSQL2, tFG1, tFG2, tSQLDB, tSQLDB2} {
		set[resourceID("", id)] = struct{}{}
	}
	final, _ := finalizeConnections(conns, set)
	count := map[string]int{}
	for _, c := range final {
		count[c.Type]++
		if c.Type == connDataflow && (c.Source != resourceID("", tSQL) || c.Target != resourceID("", tSQL2)) {
			t.Errorf("replication must flow primary -> secondary, got %s -> %s", c.Source, c.Target)
		}
	}
	// 2 server contains group, 2 group contains database, 1 replication
	// (stated by both servers, collapsed).
	if count[connContains] != 4 || count[connDataflow] != 1 {
		t.Errorf("connections by type = %v, want contains 4, dataflow 1", count)
	}

	// Negative: no roles, no replication edge.
	fg1[0].Properties.ReplicationRole = ""
	servers = []SQLServer{{ID: tSQL, Name: "lab-sql01", FailoverGroups: fg1}}
	for _, c := range TransformSQLFailoverGroupConnections(servers, idMapOf(tSQL, tSQL2), fgIDs, idMapOf(tSQLDB)) {
		if c.Type == connDataflow {
			t.Errorf("replication direction must not be guessed without roles: %+v", c)
		}
	}
}
