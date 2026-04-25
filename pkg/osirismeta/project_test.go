package osirismeta

import (
	"testing"

	"go.osirisjson.org/producers/pkg/sdk"
)

func newSampleDoc() *sdk.Document {
	return &sdk.Document{
		Metadata: sdk.Metadata{
			Generator: sdk.Generator{Name: "t", Version: "0.0.0"},
		},
		Topology: sdk.Topology{
			Resources: []sdk.Resource{
				{
					ID:   "r1",
					Type: "network.subnet",
					Name: "subnet-a",
					Properties: map[string]any{
						"address_prefix": "10.0.0.0/24",
					},
					Extensions: map[string]any{
						"osiris.azure": map[string]any{"nsg_id": "/subscriptions/x"},
					},
					Tags: map[string]string{"env": "dv"},
				},
			},
			Connections: []sdk.Connection{
				{
					ID:     "c1",
					Type:   "net.contains",
					Source: "r1",
					Target: "r2",
					Properties: map[string]any{
						"bgp_asn": 65001,
					},
					Extensions: map[string]any{
						"osiris.azure": map[string]any{"peering_state": "Connected"},
					},
				},
			},
			Groups: []sdk.Group{
				{
					ID:      "g1",
					Type:    "container.resourcegroup",
					Members: []string{"r1"},
					Properties: map[string]any{
						"location": "westeurope",
					},
					Extensions: map[string]any{
						"osiris.azure": map[string]any{"rg_id": "/sub/x/rg/y"},
					},
				},
			},
		},
	}
}

func TestProject_AuditKeepsPayload(t *testing.T) {
	doc := newSampleDoc()
	Project(doc, PurposeAudit)

	if got := doc.Metadata.Scope.Purpose; got != "audit" {
		t.Fatalf("scope.purpose = %q, want %q", got, "audit")
	}
	if len(doc.Topology.Resources[0].Properties) == 0 {
		t.Error("audit must keep resource properties")
	}
	if len(doc.Topology.Resources[0].Extensions) == 0 {
		t.Error("audit must keep resource extensions")
	}
	if len(doc.Topology.Connections[0].Properties) == 0 {
		t.Error("audit must keep connection properties")
	}
	if len(doc.Topology.Groups[0].Properties) == 0 {
		t.Error("audit must keep group properties")
	}
	if doc.Topology.Resources[0].Tags["env"] != "dv" {
		t.Error("audit must keep tags")
	}
}

func TestProject_DocumentationStripsPayload(t *testing.T) {
	doc := newSampleDoc()
	Project(doc, PurposeDocumentation)

	if got := doc.Metadata.Scope.Purpose; got != "documentation" {
		t.Fatalf("scope.purpose = %q, want %q", got, "documentation")
	}
	if doc.Topology.Resources[0].Properties != nil {
		t.Error("documentation must strip resource properties")
	}
	if doc.Topology.Resources[0].Extensions != nil {
		t.Error("documentation must strip resource extensions")
	}
	if doc.Topology.Connections[0].Properties != nil {
		t.Error("documentation must strip connection properties")
	}
	if doc.Topology.Connections[0].Extensions != nil {
		t.Error("documentation must strip connection extensions")
	}
	if doc.Topology.Groups[0].Properties != nil {
		t.Error("documentation must strip group properties")
	}
	if doc.Topology.Groups[0].Extensions != nil {
		t.Error("documentation must strip group extensions")
	}
	// Identity, relationships and tags must survive.
	if doc.Topology.Resources[0].ID != "r1" || doc.Topology.Resources[0].Name != "subnet-a" {
		t.Error("documentation must keep resource id/name")
	}
	if doc.Topology.Resources[0].Tags["env"] != "dv" {
		t.Error("documentation must keep resource tags")
	}
	if doc.Topology.Connections[0].Source != "r1" || doc.Topology.Connections[0].Target != "r2" {
		t.Error("documentation must keep connection source/target")
	}
	if len(doc.Topology.Groups[0].Members) != 1 {
		t.Error("documentation must keep group members")
	}
}

func TestProject_EmptyPurposeUsesDefault(t *testing.T) {
	doc := newSampleDoc()
	Project(doc, "")

	if got := doc.Metadata.Scope.Purpose; got != "documentation" {
		t.Fatalf("scope.purpose = %q, want documentation", got)
	}
	if doc.Topology.Resources[0].Properties != nil {
		t.Error("empty purpose should behave like documentation")
	}
}

func TestProject_NilDocIsSafe(t *testing.T) {
	// Must not panic.
	Project(nil, PurposeAudit)
}

func TestProject_ExistingScopePurposeNotOverwritten(t *testing.T) {
	doc := newSampleDoc()
	doc.Metadata.Scope = &sdk.Scope{Purpose: "audit"}

	Project(doc, PurposeDocumentation)

	if got := doc.Metadata.Scope.Purpose; got != "audit" {
		t.Fatalf("pre-set scope.purpose must not be overwritten, got %q", got)
	}
	// Projection still applies.
	if doc.Topology.Resources[0].Properties != nil {
		t.Error("documentation projection should still strip properties")
	}
}

func TestPurposeHelpIsNonEmpty(t *testing.T) {
	if PurposeHelp() == "" {
		t.Fatal("PurposeHelp() returned empty string")
	}
}
