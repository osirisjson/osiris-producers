// factory_test.go - Tests for validating factory functions (NewResource, NewProvider,
// NewConnection, NewGroup) and member/children deduplication and sorting.
//
// For an introduction to OSIRIS JSON Producer Development Guidelines see:
// "[OSIRIS-PRODUCER-GUIDELINES]."
//
// [OSIRIS-PRODUCER-GUIDELINES]: https://osirisjson.org/en/docs/getting-started/osiris-producer-guidelines

package sdk

import (
	"testing"
)

func TestNewResource(t *testing.T) {
	p, err := NewProvider("cisco")
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	r, err := NewResource("mxp::sw-01", "network.switch", p)
	if err != nil {
		t.Fatalf("NewResource: %v", err)
	}
	if r.ID != "mxp::sw-01" {
		t.Errorf("ID = %q, want %q", r.ID, "mxp::sw-01")
	}
	if r.Type != "network.switch" {
		t.Errorf("Type = %q, want %q", r.Type, "network.switch")
	}
	if r.Provider.Name != "cisco" {
		t.Errorf("Provider.Name = %q, want %q", r.Provider.Name, "cisco")
	}
}

func TestNewResourceEmptyID(t *testing.T) {
	p, _ := NewProvider("cisco")
	_, err := NewResource("", "network.switch", p)
	if err == nil {
		t.Error("expected error for empty ID")
	}
}

func TestNewResourceInvalidType(t *testing.T) {
	p, _ := NewProvider("cisco")
	_, err := NewResource("test-01", "INVALID", p)
	if err == nil {
		t.Error("expected error for invalid type")
	}
}

func TestNewResourceSingleSegmentType(t *testing.T) {
	p, _ := NewProvider("cisco")
	_, err := NewResource("test-01", "network", p)
	if err == nil {
		t.Error("expected error for single-segment resource type (needs 2+ segments)")
	}
}

func TestNewResourceInvalidProvider(t *testing.T) {
	_, err := NewResource("test-01", "network.switch", Provider{Name: "UPPER"})
	if err == nil {
		t.Error("expected error for invalid provider name")
	}
}

func TestNewProvider(t *testing.T) {
	p, err := NewProvider("arista")
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if p.Name != "arista" {
		t.Errorf("Name = %q, want %q", p.Name, "arista")
	}
}

func TestNewProviderInvalid(t *testing.T) {
	_, err := NewProvider("UPPER_CASE")
	if err == nil {
		t.Error("expected error for invalid provider name")
	}
	_, err = NewProvider("")
	if err == nil {
		t.Error("expected error for empty provider name")
	}
}

func TestNewCustomProvider(t *testing.T) {
	p, err := NewCustomProvider("osiris.com.acme")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name != "custom" {
		t.Errorf("Name = %q, want %q", p.Name, "custom")
	}
	if p.Namespace != "osiris.com.acme" {
		t.Errorf("Namespace = %q, want %q", p.Namespace, "osiris.com.acme")
	}
}

func TestNewCustomProviderInvalidNamespace(t *testing.T) {
	_, err := NewCustomProvider("invalid.namespace")
	if err == nil {
		t.Error("expected error for invalid namespace")
	}
	_, err = NewCustomProvider("osiris.UPPER")
	if err == nil {
		t.Error("expected error for uppercase namespace")
	}
}

func TestNewConnection(t *testing.T) {
	c, err := NewConnection("conn-1", "network", "sw-01", "sw-02")
	if err != nil {
		t.Fatalf("NewConnection: %v", err)
	}
	if c.Direction != "bidirectional" {
		t.Errorf("Direction = %q, want %q", c.Direction, "bidirectional")
	}
	if c.Source != "sw-01" {
		t.Errorf("Source = %q, want %q", c.Source, "sw-01")
	}
	if c.Target != "sw-02" {
		t.Errorf("Target = %q, want %q", c.Target, "sw-02")
	}
}

func TestNewConnectionInvalid(t *testing.T) {
	_, err := NewConnection("", "network", "a", "b")
	if err == nil {
		t.Error("expected error for empty connection ID")
	}
	_, err = NewConnection("c1", "INVALID!", "a", "b")
	if err == nil {
		t.Error("expected error for invalid connection type")
	}
	_, err = NewConnection("c1", "network", "", "b")
	if err == nil {
		t.Error("expected error for empty source")
	}
	_, err = NewConnection("c1", "network", "a", "")
	if err == nil {
		t.Error("expected error for empty target")
	}
}

func TestNewGroup(t *testing.T) {
	g, err := NewGroup("group-1", "physical.site")
	if err != nil {
		t.Fatalf("NewGroup: %v", err)
	}
	if g.ID != "group-1" {
		t.Errorf("ID = %q, want %q", g.ID, "group-1")
	}
	if g.Type != "physical.site" {
		t.Errorf("Type = %q, want %q", g.Type, "physical.site")
	}
}

func TestNewGroupInvalid(t *testing.T) {
	_, err := NewGroup("", "physical.site")
	if err == nil {
		t.Error("expected error for empty group ID")
	}
	_, err = NewGroup("g1", "INVALID!")
	if err == nil {
		t.Error("expected error for invalid group type")
	}
}

func TestGroupAddMembers(t *testing.T) {
	g, _ := NewGroup("g1", "physical.site")
	g.AddMembers("c", "a", "b")
	g.AddMembers("b", "d") // b is duplicate.

	want := []string{"a", "b", "c", "d"}
	if len(g.Members) != len(want) {
		t.Fatalf("Members length = %d, want %d", len(g.Members), len(want))
	}
	for i, v := range want {
		if g.Members[i] != v {
			t.Errorf("Members[%d] = %q, want %q", i, g.Members[i], v)
		}
	}
}

func TestGroupAddChildren(t *testing.T) {
	g, _ := NewGroup("g1", "logical.region")
	g.AddChildren("z", "a", "m")
	g.AddChildren("a") // duplicate.

	want := []string{"a", "m", "z"}
	if len(g.Children) != len(want) {
		t.Fatalf("Children length = %d, want %d", len(g.Children), len(want))
	}
	for i, v := range want {
		if g.Children[i] != v {
			t.Errorf("Children[%d] = %q, want %q", i, g.Children[i], v)
		}
	}
}

func TestSetStatus(t *testing.T) {
	p, _ := NewProvider("cisco")
	r, _ := NewResource("r1", "compute.vm", p)

	if err := r.SetStatus("active"); err != nil {
		t.Errorf("unexpected error for valid status: %v", err)
	}
	if r.Status != "active" {
		t.Errorf("Status = %q, want %q", r.Status, "active")
	}

	if err := r.SetStatus("invalid"); err == nil {
		t.Error("expected error for invalid status")
	}
}

func TestSetDirection(t *testing.T) {
	c, _ := NewConnection("c1", "network", "a", "b")

	if err := c.SetDirection("forward"); err != nil {
		t.Errorf("unexpected error for valid direction: %v", err)
	}
	if c.Direction != "forward" {
		t.Errorf("Direction = %q, want %q", c.Direction, "forward")
	}

	if err := c.SetDirection("invalid"); err == nil {
		t.Error("expected error for invalid direction")
	}
}
