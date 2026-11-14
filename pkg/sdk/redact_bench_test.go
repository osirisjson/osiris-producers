// redact_bench_test.go - Benchmarks for the OSIRIS JSON document secret
// scan against a synthetic, vendor-neutral document of moderate size
// (600 resources, some carrying typed records in extension slices, 600
// connections, 40 groups), with MarshalDocument as the reference cost.
//
// For the OSIRIS JSON Producer SDK reference see [OSIRIS-ADG-PR-SDK-1.0].
//
// [OSIRIS-ADG-PR-SDK-1.0]: https://github.com/osirisjson/osiris-producers/blob/main/docs/guidelines/v1.0/OSIRIS-PRODUCER-SDK.md

package sdk

import (
	"fmt"
	"testing"
)

// benchRecord is a typed extension record, the shape a producer
// attaches when it stores structs rather than maps in an extension.
type benchRecord struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// benchDocument returns the synthetic benchmark document.
func benchDocument() *Document {
	doc := &Document{
		Metadata: Metadata{Generator: Generator{Name: "bench", Version: "1"}, Scope: &Scope{Name: "bench-scope", Description: "benchmark"}},
	}
	for i := range 600 {
		r := Resource{
			ID: fmt.Sprintf("bench::resource-%04d", i), Type: "network.switch.port",
			Name: fmt.Sprintf("port-%04d", i), Provider: Provider{Name: "bench", NativeID: fmt.Sprintf("resource-%04d", i)},
			Status:     "active",
			Properties: map[string]any{"speed": "25G", "mtu": 9216, "admin_state": "up", "description": "benchmark port"},
		}
		if i%10 == 0 {
			records := make([]benchRecord, 5)
			for j := range records {
				records[j] = benchRecord{Code: "R1", Severity: "minor", Message: "benchmark record message"}
			}
			r.Extensions = map[string]any{"osiris.bench": map[string]any{"records": records, "labels": []any{"a", "b"}}}
		}
		doc.Topology.Resources = append(doc.Topology.Resources, r)
	}
	for i := range 600 {
		doc.Topology.Connections = append(doc.Topology.Connections, Connection{
			ID: fmt.Sprintf("conn-%04d", i), Type: "contains", Source: doc.Topology.Resources[i].ID, Target: doc.Topology.Resources[(i+1)%600].ID,
			Properties: map[string]any{"source": "bench"},
		})
	}
	for i := range 40 {
		doc.Topology.Groups = append(doc.Topology.Groups, Group{
			ID: fmt.Sprintf("group-%02d", i), Type: "logical.environment", Name: fmt.Sprintf("group-%02d", i),
			Members: []string{doc.Topology.Resources[i].ID}, Extensions: map[string]any{"osiris.bench": map[string]any{"ref": "bench"}},
		})
	}
	return doc
}

func BenchmarkScanDocument(b *testing.B) {
	doc := benchDocument()
	b.ReportAllocs()
	for b.Loop() {
		ScanDocument(doc)
	}
}

func BenchmarkMarshalDocument(b *testing.B) {
	doc := benchDocument()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := MarshalDocument(doc); err != nil {
			b.Fatal(err)
		}
	}
}
