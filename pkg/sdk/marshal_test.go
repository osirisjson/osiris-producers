// marshal_test.go - Tests for deterministic JSON serialization
// (2-space indent, trailing newline).
//
// For the OSIRIS JSON Producer SDK reference see [OSIRIS-ADG-PR-SDK-1.0].
//
// [OSIRIS-ADG-PR-SDK-1.0]: https://github.com/osirisjson/osiris-producers/blob/main/docs/guidelines/v1.0/OSIRIS-PRODUCER-SDK.md

package sdk

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMarshalDocumentFormat(t *testing.T) {
	doc := &Document{
		Schema:  SchemaURI,
		Version: SpecVersion,
		Metadata: Metadata{
			Timestamp: "2026-01-15T10:00:00Z",
			Generator: Generator{
				Name:    "test-producer",
				Version: "0.2.2",
			},
		},
		Topology: Topology{
			Resources: []Resource{},
		},
	}

	data, err := MarshalDocument(doc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := string(data)

	// MUST end with trailing newline.
	if !strings.HasSuffix(s, "\n") {
		t.Error("output should end with trailing newline")
	}

	// MUST use 2-space indentation.
	if !strings.Contains(s, "  \"version\"") {
		t.Error("output should use 2-space indentation")
	}

	// MUST contain $schema.
	if !strings.Contains(s, SchemaURI) {
		t.Error("output should contain schema URI")
	}
}

func TestMarshalDocumentDeterministic(t *testing.T) {
	doc := &Document{
		Schema:  SchemaURI,
		Version: SpecVersion,
		Metadata: Metadata{
			Timestamp: "2026-01-15T10:00:00Z",
			Generator: Generator{Name: "test", Version: "0.2.2"},
		},
		Topology: Topology{
			Resources: []Resource{
				{ID: "b", Type: "compute.vm", Provider: Provider{Name: "aws"}},
				{ID: "a", Type: "compute.vm", Provider: Provider{Name: "aws"}},
			},
		},
	}

	data1, err := MarshalDocument(doc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data2, err := MarshalDocument(doc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(data1) != string(data2) {
		t.Error("MarshalDocument should produce identical output for the same input")
	}
}

func TestMetadataExtensions(t *testing.T) {
	m := Metadata{
		Timestamp: "2026-01-01T00:00:00Z",
		Generator: Generator{Name: "test", Version: "0.1.0"},
		Extensions: map[string]any{
			"osiris.test": map[string]any{"note": "a & b"},
			"plain":       "dropped",
		},
	}
	// Encoded the way MarshalDocument does (no HTML escaping).
	b, err := encodeNoEscape(m)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"timestamp":"2026-01-01T00:00:00Z","generator":{"name":"test","version":"0.1.0"},"osiris.test":{"note":"a & b"}}`
	if string(b) != want {
		t.Errorf("got  %s\nwant %s", b, want)
	}
	var back Metadata
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.Extensions["osiris.test"].(map[string]any)["note"] != "a & b" || back.Generator.Name != "test" {
		t.Errorf("round trip = %+v", back)
	}
	// No extensions: unchanged encoding.
	m.Extensions = nil
	if b, _ := json.Marshal(m); strings.Contains(string(b), "osiris") {
		t.Errorf("unexpected extension output %s", b)
	}
}
