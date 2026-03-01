// marshal_test.go - Tests for deterministic JSON serialization (2-space indent, trailing newline).
//
// For an introduction to OSIRIS JSON Producer Development Guidelines see:
// "[OSIRIS-PRODUCER-GUIDELINES]."
//
// [OSIRIS-PRODUCER-GUIDELINES]: https://osirisjson.org/en/docs/developers/producers/welcome

package sdk

import (
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
				Name: "test-producer",
				Version: "0.1.0",
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
		Schema: SchemaURI,
		Version: SpecVersion,
		Metadata: Metadata{
			Timestamp: "2026-01-15T10:00:00Z",
			Generator: Generator{Name: "test", Version: "1.0.0"},
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
