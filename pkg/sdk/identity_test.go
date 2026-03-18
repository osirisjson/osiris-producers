// identity_test.go - Tests for hash truncation, component encoding, hint derivation,
// canonical key generation, connection/group ID building and IDRegistry collision resolution.
//
// For an introduction to OSIRIS JSON Producer Development Guidelines see:
// "[OSIRIS-PRODUCER-GUIDELINES]."
//
// [OSIRIS-PRODUCER-GUIDELINES]: https://osirisjson.org/en/docs/getting-started/osiris-producer-guidelines

package sdk

import (
	"strings"
	"testing"
)

func TestHash16(t *testing.T) {
	// SHA-256 of "hello" = 2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824 .
	got := Hash16("hello")
	want := "2cf24dba5fb0a30e"
	if got != want {
		t.Errorf("Hash16(\"hello\") = %q, want %q", got, want)
	}
}

func TestHashN(t *testing.T) {
	tests := []struct {
		key  string
		n    int
		want string
	}{
		{"hello", 8, "2cf24dba"},
		{"hello", 16, "2cf24dba5fb0a30e"},
		{"hello", 24, "2cf24dba5fb0a30e26e83b2a"},
		{"hello", 32, "2cf24dba5fb0a30e26e83b2ac5b9e29e"},
	}
	for _, tt := range tests {
		got := HashN(tt.key, tt.n)
		if got != tt.want {
			t.Errorf("HashN(%q, %d) = %q, want %q", tt.key, tt.n, got, tt.want)
		}
	}
}

func TestEncodeComponent(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"simple", "simple"},
		{"a|b", "a%7Cb"},
		{"a,b", "a%2Cb"},
		{"a=b", "a%3Db"},
		{"a%b", "a%25b"},
		{"a|b,c=d%e", "a%7Cb%2Cc%3Dd%25e"},
	}
	for _, tt := range tests {
		got := EncodeComponent(tt.input)
		if got != tt.want {
			t.Errorf("EncodeComponent(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestDeriveHint(t *testing.T) {
	tests := []struct {
		id   string
		hash string
		want string
	}{
		{"mxp::spine-sw-01", "abcdef12", "spine-sw-01"},
		{"aws::i-0abc123def456", "abcdef12", "i-0abc123def456"},
		{"azure::/subscriptions/sub-123/vm01", "abcdef12", "vm01"},
		{"/some/deep/path/resource", "abcdef12", "resource"},
		{"UPPER::CASE-Test", "abcdef12", "case-test"}, // :: takes priority, "CASE-Test" after :: .
		{"", "abcdef12", "abcdef12"},                  // empty falls back to hash.
		{"mxp::a-very-long-name-that-exceeds-the-limit-of-24", "abcdef12", "a-very-long-name-that-ex"},
	}
	for _, tt := range tests {
		got := DeriveHint(tt.id, tt.hash)
		if got != tt.want {
			t.Errorf("DeriveHint(%q, %q) = %q, want %q", tt.id, tt.hash, got, tt.want)
		}
	}
}

func TestConnectionCanonicalKey(t *testing.T) {
	// Forward: preserve order. Colons are not in the encode set.
	got := ConnectionCanonicalKey(ConnectionIDInput{
		Type:      "network",
		Direction: "forward",
		Source:    "mxp::sw-01",
		Target:    "mxp::sw-02",
	})
	want := "v1|network|forward|mxp::sw-01|mxp::sw-02"
	if got != want {
		t.Errorf("forward key = %q, want %q", got, want)
	}

	// Bidirectional: sort endpoints.
	got1 := ConnectionCanonicalKey(ConnectionIDInput{
		Type:      "network",
		Direction: "bidirectional",
		Source:    "mxp::sw-02",
		Target:    "mxp::sw-01",
	})
	got2 := ConnectionCanonicalKey(ConnectionIDInput{
		Type:      "network",
		Direction: "bidirectional",
		Source:    "mxp::sw-01",
		Target:    "mxp::sw-02",
	})
	if got1 != got2 {
		t.Errorf("bidirectional keys differ: %q vs %q", got1, got2)
	}
}

func TestConnectionCanonicalKeyWithQualifiers(t *testing.T) {
	got := ConnectionCanonicalKey(ConnectionIDInput{
		Type:      "dataflow.tcp",
		Direction: "forward",
		Source:    "mxp::plc-01",
		Target:    "mxp::printer-01",
		Qualifiers: map[string]string{
			"protocol": "tcp",
			"port":     "9100",
		},
	})
	// Qualifiers sorted by encoded key: port < protocol
	// The = between key and value is a structural separator, not encoded.
	if !contains(got, "port=9100") {
		t.Errorf("expected qualifiers in key, got %q", got)
	}
}

func TestBuildConnectionID(t *testing.T) {
	ck := ConnectionCanonicalKey(ConnectionIDInput{
		Type:      "network",
		Direction: "bidirectional",
		Source:    "mxp::sw-01",
		Target:    "mxp::sw-02",
	})
	id := BuildConnectionID(ck, 16)
	if !startsWith(id, "conn-network-") {
		t.Errorf("connection ID should start with conn-network-, got %q", id)
	}
	if !containsStr(id, "-to-") {
		t.Errorf("connection ID should contain -to-, got %q", id)
	}
}

func TestGroupID(t *testing.T) {
	id := GroupID(GroupIDInput{
		Type:          "physical.site",
		BoundaryToken: "mxp",
		ScopeFields: map[string]string{
			"provider.name": "custom",
		},
	})
	if !startsWith(id, "group-physical.site-mxp-") {
		t.Errorf("group ID should start with group-physical.site-mxp-, got %q", id)
	}
	if len(id) < len("group-physical.site-mxp-")+16 {
		t.Errorf("group ID too short: %q", id)
	}
}

func TestGroupIDStableWithoutMembers(t *testing.T) {
	// Membership is excluded from canonical key, so group ID is stable.
	id1 := GroupID(GroupIDInput{
		Type:          "physical.site",
		BoundaryToken: "mxp",
	})
	id2 := GroupID(GroupIDInput{
		Type:          "physical.site",
		BoundaryToken: "mxp",
	})
	if id1 != id2 {
		t.Errorf("group IDs should be stable: %q vs %q", id1, id2)
	}
}

func TestIDRegistryNoCollisions(t *testing.T) {
	reg := NewIDRegistry()
	reg.RegisterKey("conn", "v1|network|bidirectional|a|b")
	reg.RegisterKey("conn", "v1|network|bidirectional|c|d")

	result, err := reg.ResolveAll("conn", func(ck string, hashLen int) string {
		return "conn-" + HashN(ck, hashLen)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}
}

func TestIDRegistryEmptyKind(t *testing.T) {
	reg := NewIDRegistry()
	result, err := reg.ResolveAll("nonexistent", func(ck string, hashLen int) string {
		return HashN(ck, hashLen)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("expected 0 results, got %d", len(result))
	}
}

func TestIDRegistryCollisionUpgrade(t *testing.T) {
	// Force a collision at Hash16 by using a buildID that ignores the canonical key
	// at length 16 but differentiates at length 24.
	reg := NewIDRegistry()
	reg.RegisterKey("test", "key-alpha")
	reg.RegisterKey("test", "key-beta")

	callLog := map[string][]int{} // track which hash lengths were requested per key.
	result, err := reg.ResolveAll("test", func(ck string, hashLen int) string {
		callLog[ck] = append(callLog[ck], hashLen)
		if hashLen == 16 {
			return "collision-id" // force collision at 16.
		}
		return "resolved-" + ck + "-" + HashN(ck, hashLen)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}
	// Both keys should have been called at length 16 (collision) then 24 (resolved).
	for _, key := range []string{"key-alpha", "key-beta"} {
		lengths := callLog[key]
		if len(lengths) < 2 || lengths[0] != 16 || lengths[1] != 24 {
			t.Errorf("key %q: expected calls at [16, 24], got %v", key, lengths)
		}
	}
}

func TestIDRegistryUnresolvableCollision(t *testing.T) {
	reg := NewIDRegistry()
	reg.RegisterKey("test", "key-a")
	reg.RegisterKey("test", "key-b")

	// Always return the same ID regardless of hash length - unresolvable collision.
	_, err := reg.ResolveAll("test", func(ck string, hashLen int) string {
		return "permanent-collision"
	})
	if err == nil {
		t.Fatal("expected ErrIDCollision, got nil")
	}
	if !strings.Contains(err.Error(), "unresolvable ID collision") {
		t.Errorf("expected ErrIDCollision, got: %v", err)
	}
}

func TestEncodeComponentRoundTrip(t *testing.T) {
	original := "a|b,c=d%e"
	encoded := EncodeComponent(original)
	decoded := decodeComponent(encoded)
	if decoded != original {
		t.Errorf("round-trip failed: %q -> %q -> %q", original, encoded, decoded)
	}
}

func TestBidirectionalConnectionKeySymmetry(t *testing.T) {
	// Swapping source and target for bidirectional MUST produce identical canonical keys.
	key1 := ConnectionCanonicalKey(ConnectionIDInput{
		Type:      "network.l2",
		Direction: "bidirectional",
		Source:    "mxp::spine-01",
		Target:    "mxp::leaf-01",
	})
	key2 := ConnectionCanonicalKey(ConnectionIDInput{
		Type:      "network.l2",
		Direction: "bidirectional",
		Source:    "mxp::leaf-01",
		Target:    "mxp::spine-01",
	})
	if key1 != key2 {
		t.Errorf("bidirectional keys should be symmetric:\n  %q\n  %q", key1, key2)
	}

	// Forward direction: order MUST be preserved.
	key3 := ConnectionCanonicalKey(ConnectionIDInput{
		Type:      "network.l2",
		Direction: "forward",
		Source:    "mxp::spine-01",
		Target:    "mxp::leaf-01",
	})
	key4 := ConnectionCanonicalKey(ConnectionIDInput{
		Type:      "network.l2",
		Direction: "forward",
		Source:    "mxp::leaf-01",
		Target:    "mxp::spine-01",
	})
	if key3 == key4 {
		t.Error("forward keys should NOT be symmetric when endpoints are swapped")
	}
}

func TestGroupIDScopeFieldsSorted(t *testing.T) {
	// Scope fields should be sorted by encoded key, so order of map doesn't matter.
	id1 := GroupID(GroupIDInput{
		Type:          "logical.environment",
		BoundaryToken: "prod",
		ScopeFields: map[string]string{
			"provider.name":    "aws",
			"provider.account": "123456",
			"provider.region":  "eu-west-1",
		},
	})
	id2 := GroupID(GroupIDInput{
		Type:          "logical.environment",
		BoundaryToken: "prod",
		ScopeFields: map[string]string{
			"provider.region":  "eu-west-1",
			"provider.name":    "aws",
			"provider.account": "123456",
		},
	})
	if id1 != id2 {
		t.Errorf("group IDs should be stable regardless of scope field order:\n  %q\n  %q", id1, id2)
	}
}

// helpers.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsStr(s, substr)
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
