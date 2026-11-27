// golden_test.go - Golden document tests for the OSIRIS JSON Amazon AWS
// producer.
//
// Each scenario under testdata/osiris/hyperscalers/aws/<scenario>/
// holds sdk/region-data.json, the collected AWS SDK data of one account
// and region in the JSON form of the producer's RegionData.
//
// The document pipeline (Producer.buildDocument) runs on it with a
// fixed clock, once per --purpose flag (audit with --include-raw-body)
// and the document must match purpose/golden_<purpose>.json byte for
// byte, be identical across two runs, contain none of the planted
// secrets and satisfy the graph invariants checked by assertGraph.
//
// Regenerate after an intended output change, then validate:
//
//	1. OSIRIS_UPDATE_GOLDEN=1 go test ./osiris/hyperscalers/aws/ -run TestGolden
//	2. ./scripts/validate_golden.sh testdata/osiris/hyperscalers/aws
//
// For an introduction to OSIRIS JSON Producer for Amazon AWS see:
// [OSIRIS-JSON-AWS]: https://docs.osirisjson.org/osiris-producers/hyperscalers/amazon-aws/

package aws

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.osirisjson.org/producers/pkg/osirismeta"
	"go.osirisjson.org/producers/pkg/sdk"
	"go.osirisjson.org/producers/pkg/testharness"
)

const goldenRoot = "../../../testdata/osiris/hyperscalers/aws"

// planted are secrets placed in the scenario inputs none may be emitted.
var planted = []string{"changeme", "admin:"}

func loadRegionData(t *testing.T, scenario string) *RegionData {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(goldenRoot, scenario, "sdk", "region-data.json"))
	if err != nil {
		t.Fatal(err)
	}
	var data RegionData
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&data); err != nil {
		t.Fatalf("decoding region data: %v", err)
	}
	return &data
}

// completeOutcomes reports every core operation of data as fully read
// in one page, with the scenario's item counts.
func completeOutcomes(data *RegionData) []operationOutcome {
	items := map[string]int{
		"DescribeInstances":         len(data.Instances),
		"DescribeNetworkInterfaces": len(data.NetworkInterfaces),
		"DescribeRouteTables":       len(data.RouteTables),
		"DescribeSubnets":           len(data.Subnets),
		"DescribeVpcs":              len(data.VPCs),
	}
	var out []operationOutcome
	for _, op := range []string{"DescribeInstances", "DescribeNetworkInterfaces", "DescribeRouteTables", "DescribeSubnets", "DescribeVpcs"} {
		o := operationOutcome{Service: "ec2", Operation: op, Outcome: outcomeSucceeded, Pages: 1, Items: items[op]}
		if o.Items == 0 {
			o.Outcome = outcomeSucceededEmpty
		}
		out = append(out, o)
	}
	return out
}

// renderScenario builds the document of scenario at purpose. A nil
// outcomes reports every core operation as fully read.
func renderScenario(t *testing.T, scenario string, purpose osirismeta.Purpose, outcomes []operationOutcome) *sdk.Document {
	t.Helper()
	cfg := &Config{
		SafeFailureMode: sdk.FailClosed,
		Purpose:         purpose.String(),
		IncludeRawBody:  purpose == osirismeta.PurposeAudit,
	}
	p := NewProducer(AccountTarget{AccountName: "TEST-ACCOUNT", Environment: "test"}, "eu-west-1", cfg)
	ctx := testharness.NewTestContext(t,
		testharness.WithClock(fixedClock),
		testharness.WithConfig(&sdk.ProducerConfig{SafeFailureMode: sdk.FailClosed, Purpose: purpose.String()}),
	)
	data := loadRegionData(t, scenario)
	if outcomes == nil {
		outcomes = completeOutcomes(data)
	}
	doc, err := p.buildDocument(ctx, data, false, nil, outcomes)
	if err != nil {
		t.Fatalf("buildDocument: %v", err)
	}
	return doc
}

func TestGolden(t *testing.T) {
	for _, scenario := range []string{"core-vpc", "s3-unverified", "shared-tgw"} {
		for _, purpose := range []osirismeta.Purpose{osirismeta.PurposeDocumentation, osirismeta.PurposeAudit} {
			t.Run(scenario+"/"+purpose.String(), func(t *testing.T) {
				doc := renderScenario(t, scenario, purpose, nil)
				testharness.AssertGolden(t, doc, filepath.Join(goldenRoot, scenario, "purpose", "golden_"+purpose.String()+".json"))

				first, _ := sdk.MarshalDocument(doc)
				again, _ := sdk.MarshalDocument(renderScenario(t, scenario, purpose, nil))
				if !bytes.Equal(first, again) {
					t.Error("document differs between two runs")
				}
				for _, s := range planted {
					if strings.Contains(string(first), s) {
						t.Errorf("planted secret %q emitted", s)
					}
				}
				assertGraph(t, doc)
			})
		}
	}
}

// assertGraph checks the graph invariants every AWS document must hold.
func assertGraph(t *testing.T, doc *sdk.Document) {
	t.Helper()
	ids := map[string]sdk.Resource{}
	for _, r := range doc.Topology.Resources {
		ids[r.ID] = r
		if err := sdk.ValidateStatus(r.Status); err != nil {
			t.Errorf("resource %s: %v", r.ID, err)
		}
	}
	assocPerSubnet := map[string]int{}
	for _, c := range doc.Topology.Connections {
		if c.Source == c.Target {
			t.Errorf("self edge %s", c.ID)
		}
		if _, ok := ids[c.Source]; !ok {
			t.Errorf("connection %s: dangling source %s", c.ID, c.Source)
		}
		if _, ok := ids[c.Target]; !ok {
			t.Errorf("connection %s: dangling target %s", c.ID, c.Target)
		}
		if err := sdk.ValidateStatus(c.Status); err != nil {
			t.Errorf("connection %s: %v", c.ID, err)
		}
		if c.Type == routeTableAssociationType {
			assocPerSubnet[c.Target]++
		}
	}
	for _, r := range doc.Topology.Resources {
		if r.Type == "network.subnet" && assocPerSubnet[r.ID] > 1 {
			t.Errorf("subnet %s has %d route table associations", r.ID, assocPerSubnet[r.ID])
		}
	}
	for _, g := range doc.Topology.Groups {
		for _, m := range g.Members {
			if _, ok := ids[m]; !ok {
				t.Errorf("group %s: unresolved member %s", g.ID, m)
			}
		}
	}
}

// edgesOfType returns "source -> target" native IDs for connections of
// type typ.
func edgesOfType(doc *sdk.Document, typ string) map[string]bool {
	native := map[string]string{}
	for _, r := range doc.Topology.Resources {
		native[r.ID] = r.Provider.NativeID
	}
	out := map[string]bool{}
	for _, c := range doc.Topology.Connections {
		if c.Type == typ {
			out[native[c.Source]+" -> "+native[c.Target]] = true
		}
	}
	return out
}

func TestCoreVPCAssociations(t *testing.T) {
	doc := renderScenario(t, "core-vpc", osirismeta.PurposeAudit, nil)

	assoc := edgesOfType(doc, routeTableAssociationType)
	want := map[string]bool{
		"rtb-00000000000000002 -> subnet-00000000000000001": true, // explicit
		"rtb-00000000000000001 -> subnet-00000000000000002": true, // main
	}
	if len(assoc) != len(want) {
		t.Errorf("associations = %v, want %v", assoc, want)
	}
	for k := range want {
		if !assoc[k] {
			t.Errorf("missing association %s", k)
		}
	}
	if n := len(edgesOfType(doc, "network")); n == 0 {
		t.Error("expected network edges to remain")
	}
	for e := range edgesOfType(doc, "network") {
		if strings.HasPrefix(e, "rtb-") {
			t.Errorf("legacy route table network edge still emitted: %s", e)
		}
	}

	att := edgesOfType(doc, eniAttachmentType)
	if len(att) != 1 || !att["i-00000000000000001 -> eni-00000000000000001"] {
		t.Errorf("attachments = %v, want only i-...01 -> eni-...01", att)
	}

	core := doc.Metadata.Extensions["osiris.aws"].(awsMetadata).Coverage
	if core["core_complete"] != true {
		t.Errorf("core_complete = %v", core["core_complete"])
	}
}

func TestCoreVPCNoImplicitAssociationWhenRouteTablesPartial(t *testing.T) {
	outcomes := completeOutcomes(loadRegionData(t, "core-vpc"))
	for i := range outcomes {
		if outcomes[i].Operation == "DescribeRouteTables" {
			outcomes[i].Outcome = outcomePartial
		}
	}
	doc := renderScenario(t, "core-vpc", osirismeta.PurposeDocumentation, outcomes)
	assoc := edgesOfType(doc, routeTableAssociationType)
	if len(assoc) != 1 || !assoc["rtb-00000000000000002 -> subnet-00000000000000001"] {
		t.Errorf("associations = %v, want only the explicit one", assoc)
	}
	core := doc.Metadata.Extensions["osiris.aws"].(awsMetadata).Coverage
	if core["core_complete"] != false {
		t.Error("partial route tables reported core_complete")
	}
	assertGraph(t, doc)
}

func TestS3UnverifiedSettings(t *testing.T) {
	doc := renderScenario(t, "s3-unverified", osirismeta.PurposeAudit, nil)
	props := map[string]map[string]any{}
	for _, r := range doc.Topology.Resources {
		if r.Type == "storage.bucket" {
			props[r.Name] = r.Properties
		}
	}
	read, denied := props["test-bucket-read"], props["test-bucket-denied"]
	if read["versioning"] != "Enabled" || read["block_public_access"] != true || read["encryption_algorithm"] != "aws:kms" {
		t.Errorf("read bucket = %v", read)
	}
	for _, k := range []string{"versioning", "block_public_access", "encryption_algorithm"} {
		if _, ok := denied[k]; ok {
			t.Errorf("denied bucket reports %s = %v", k, denied[k])
		}
	}
	if denied["unverified_settings"] == nil {
		t.Error("denied bucket has no unverified_settings")
	}
}
