// golden_test.go - Source-to-golden test for the Cisco IOS-XE OSIRIS
// JSON Producer.
//
// Drives Producer.Collect end to end against an in-memory NETCONF
// server that speaks the real wire protocol: <hello> with base:1.0 and
// base:1.1, chunked framing afterwards and a message-id echoed on
// every reply. The emitted document is compared byte for byte with the
// expected document below, at both --purpose flag levels.
//
// Fixtures live under testdata/osiris/network/cisco/iosxe/ at the
// repository root: one snake_case directory per scenario, holding a
// synthetic <data> payload per discovery operation
// (netconf/<operation>.xml, the operation name in snake_case) and the
// expected document per purpose (purpose/golden_<purpose>.json). Their
// provenance, value sources and deliberate omissions are documented in
// that directory's README.md. Regenerate the expected documents with
// OSIRIS_UPDATE_GOLDEN=1 and review the diff: they are the producer's
// output contract.
//
// Decoders consume NETCONF XML only; CLI "show" text is never a decoder
// input (see TestDecodeModel_RejectsCLIText).
//
// TestDataMinimization proves the default exclusions: no discovery
// operation reads volatile state or a whole datastore, no golden
// document carries counters, utilization, or raw replies and the
// documentation-purpose goldens carry no audit-only posture.
//
// TestSecretCanary replaces every credential placeholder in the golden
// fixtures with a random value generated per run, plants the same value
// under credential-named elements of an operational reply and proves
// it never reaches the emitted document or the log stream, at either
// purpose, with and without --include-raw-body, over NETCONF and
// RESTCONF.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package iosxe

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"go.osirisjson.org/producers/osiris/network/cisco/run"
	"go.osirisjson.org/producers/pkg/sdk"
	"go.osirisjson.org/producers/pkg/testharness"
)

// goldenRoot holds one directory per scenario, relative to this package
// directory.
const goldenRoot = "../../../../testdata/osiris/network/cisco/iosxe"

// goldenScenarios are the scenario directories under goldenRoot: a
// Catalyst 8000V router, a Catalyst 9300 switch and an ISR 4451
// router.
var goldenScenarios = []string{"c8000v", "C9300_24T", "isr4451"}

// goldenReply is one discovery operation's filter and the <data>
// content returned for it.
type goldenReply struct{ filter, data string }

// loadGoldenReplies reads every discovery operation's fixture from dir,
// named after the operation. An operation without a fixture file is
// answered with an empty <data/>, a device with no instances of that
// model.
func loadGoldenReplies(t *testing.T, dir string) []goldenReply {
	t.Helper()
	replies := make([]goldenReply, 0, len(discoveryQueries))
	for _, q := range discoveryQueries {
		path := filepath.Join(dir, "netconf", fixtureFile(q.op))
		var data []byte
		if _, err := os.Stat(path); err == nil {
			data = testharness.LoadFixture(t, path)
		}
		replies = append(replies, goldenReply{filter: q.filter, data: string(data)})
	}
	return replies
}

var goldenMessageIDRe = regexp.MustCompile(`message-id="([0-9]+)"`)

// serveNETCONF plays the device side of a NETCONF session over r/w
// until the client sends <close-session/> or the stream ends.
func serveNETCONF(t *testing.T, replies []goldenReply, r io.Reader, w io.Writer) {
	f := newFramer(r, w)
	hello := `<?xml version="1.0" encoding="UTF-8"?><hello xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><capabilities><capability>
        urn:ietf:params:netconf:base:1.0
      </capability><capability>
        urn:ietf:params:netconf:base:1.1
      </capability></capabilities><session-id>42</session-id></hello>`
	if err := f.writeMessage([]byte(hello)); err != nil {
		t.Errorf("server hello: %v", err)
		return
	}
	if _, err := f.readMessage(); err != nil {
		t.Errorf("client hello: %v", err)
		return
	}
	f.chunked = true

	for {
		req, err := f.readMessage()
		if err != nil {
			if !errors.Is(err, errMalformedFrame) {
				t.Errorf("server read: %v", err)
			}
			return
		}
		id := goldenMessageIDRe.FindSubmatch(req)
		if id == nil {
			t.Errorf("request without message-id: %s", req)
			return
		}
		body := "<data/>"
		if strings.Contains(string(req), "<close-session/>") {
			body = "<ok/>"
		} else {
			for _, g := range replies {
				if strings.Contains(string(req), g.filter) {
					body = "<data>" + g.data + "</data>"
					break
				}
			}
		}
		reply := `<?xml version="1.0" encoding="UTF-8"?><rpc-reply xmlns="urn:ietf:params:xml:ns:netconf:base:1.0" message-id="` + string(id[1]) + `">` + body + `</rpc-reply>`
		if err := f.writeMessage([]byte(reply)); err != nil {
			t.Errorf("server write: %v", err)
			return
		}
		if body == "<ok/>" {
			return
		}
	}
}

// pipeTransport is a Transport over an in-memory framer.
type pipeTransport struct {
	f       *framer
	closers []io.Closer
}

func (p *pipeTransport) Send(rpc []byte) ([]byte, error) {
	if err := p.f.writeMessage(rpc); err != nil {
		return nil, err
	}
	return p.f.readMessage()
}

func (p *pipeTransport) Close() error {
	for _, c := range p.closers {
		c.Close()
	}
	return nil
}

// collectGolden runs Collect against the synthetic device serving
// replies at purpose and returns the document.
func collectGolden(t *testing.T, replies []goldenReply, purpose string) *sdk.Document {
	t.Helper()
	return collectGoldenWith(t, replies, &sdk.ProducerConfig{Purpose: purpose, SafeFailureMode: sdk.FailClosed}, nil)
}

// collectGoldenWith is collectGolden with an explicit producer
// configuration and, when logger is not nil, the logger Collect
// writes to.
func collectGoldenWith(t *testing.T, replies []goldenReply, cfg *sdk.ProducerConfig, logger *slog.Logger) *sdk.Document {
	t.Helper()
	srvR, cliW := io.Pipe()
	cliR, srvW := io.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		serveNETCONF(t, replies, srvR, srvW)
		srvW.Close()
	}()

	f := newFramer(cliR, cliW)
	if _, err := exchangeHello(f); err != nil {
		t.Fatalf("hello: %v", err)
	}
	if !f.chunked {
		t.Fatal("the session should negotiate base:1.1 chunked framing")
	}

	ctx := testharness.NewTestContext(t, testharness.WithConfig(cfg))
	if logger != nil {
		ctx.Logger = logger
	}
	p := &Producer{
		target: run.TargetConfig{Host: "192.0.2.10", Hostname: "LAB-TARGET"},
		cfg:    &Config{Purpose: cfg.Purpose, IncludeRawBody: cfg.IncludeRawBody},
		client: &Client{transport: &pipeTransport{f: f, closers: []io.Closer{cliW, cliR}}, logger: ctx.Logger, addr: "192.0.2.10:830"},
	}
	doc, err := p.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if err := p.client.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	<-done
	return doc
}

func TestGolden(t *testing.T) {
	for _, scenario := range goldenScenarios {
		dir := filepath.Join(goldenRoot, scenario)
		replies := loadGoldenReplies(t, dir)
		for _, purpose := range []string{"documentation", "audit"} {
			t.Run(scenario+"/"+purpose, func(t *testing.T) {
				doc := collectGolden(t, replies, purpose)
				assertTopologyInvariants(t, doc)
				assertNoFixtureCredential(t, doc)
				testharness.AssertGolden(t, doc, filepath.Join(dir, "purpose", "golden_"+purpose+".json"))
			})
		}
	}
}

// assertTopologyInvariants checks the semantic rules the schema cannot
// express: every connection endpoint and group member resolves, every
// owned port is contained by exactly one device and no two physical
// links join the same local port to the same remote port.
func assertTopologyInvariants(t *testing.T, doc *sdk.Document) {
	t.Helper()
	ids := map[string]sdk.Resource{}
	for _, r := range doc.Topology.Resources {
		ids[r.ID] = r
	}
	contained := map[string]int{}
	links := map[string]bool{}
	for _, c := range doc.Topology.Connections {
		if _, ok := ids[c.Source]; !ok {
			t.Errorf("connection %s: source %q does not resolve", c.ID, c.Source)
		}
		if _, ok := ids[c.Target]; !ok {
			t.Errorf("connection %s: target %q does not resolve", c.ID, c.Target)
		}
		if strings.HasPrefix(c.Type, "contains") {
			contained[c.Target]++
		}
		if strings.HasPrefix(c.Type, "physical.") {
			k := c.Source + "|" + c.Target
			if links[k] {
				t.Errorf("duplicate physical link %s", k)
			}
			links[k] = true
		}
	}
	for _, g := range doc.Topology.Groups {
		for _, m := range g.Members {
			if _, ok := ids[m]; !ok {
				t.Errorf("group %s: member %q does not resolve", g.Name, m)
			}
		}
	}
	for _, r := range doc.Topology.Resources {
		switch r.Type {
		case "network.router.port", "network.switch.port", "network.interface.lag":
		case "network.interface":
			if r.Provider.Name == unknownProviderName {
				continue
			}
		default:
			continue
		}
		if contained[r.ID] != 1 {
			t.Errorf("port %s is contained %d times, want exactly once", r.ID, contained[r.ID])
		}
	}
}

// fixtureCredential is the placeholder every credential leaf in the
// golden fixtures holds (key, secret, password, community, NTP key,
// IKE pre-shared key). It must never reach an emitted document.
const fixtureCredential = "changeme"

// assertNoFixtureCredential fails when the fixture credential appears
// anywhere in doc.
func assertNoFixtureCredential(t *testing.T, doc *sdk.Document) {
	t.Helper()
	data, err := sdk.MarshalDocument(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), fixtureCredential) {
		t.Errorf("a fixture credential reached the document")
	}
}

func TestDecodeModel_RejectsCLIText(t *testing.T) {
	cli := []string{
		"Cisco IOS XE Software, Version 17.15.04c\nCisco IOS Software [IOSXE], Virtual XE Software (X86_64_LINUX_IOSD-UNIVERSALK9-M)\n",
		"Interface              IP-Address      OK? Method Status                Protocol\nGigabitEthernet1       192.0.2.10      YES NVRAM  up                    up\n",
		"Device ID        Local Intrfce     Holdtme    Capability  Platform  Port ID\nLAB-SW01         Gig 2             150             S I   C9300-24T Gig 1/0/24\n",
	}
	for _, text := range cli {
		if _, err := decodeModel[ietfInterfaces]([]byte(text)); !errors.Is(err, errDecode) {
			t.Errorf("CLI text must be rejected as errDecode, got %v", err)
		}
		if _, err := decodeModel[hwInventory]([]byte(text)); !errors.Is(err, errDecode) {
			t.Errorf("CLI text must be rejected as errDecode, got %v", err)
		}
	}
}

// volatileModels are YANG models and paths of volatile state that no
// discovery operation may query.
var volatileModels = []string{
	"process-cpu-oper", "memory-oper", "ietf-routing", "routing-state",
	"arp-oper", "matm-oper", "mac-address-table", "ipv6-nd", "nd-oper",
	"environment-oper", "<statistics", "ip-sla-oper",
}

// forbiddenKeys may not appear anywhere in an emitted document without
// --include-raw-body under --purpose audit flag.
var forbiddenKeys = map[string]bool{
	"rx_bytes": true, "tx_bytes": true, "rx_errors": true, "tx_errors": true,
	"cpu_utilization_1min": true, "cpu_utilization_5min": true,
	"memory_used": true, "memory_free": true,
	"raw_replies": true, "raw_omitted": true,
	"arp_table": true, "mac_table": true, "routing_table": true,
}

// auditOnlyKeys are device extension keys emitted at audit purpose only.
var auditOnlyKeys = []string{
	"aaa", "local_accounts", "enable_mode", "management_services", "dns",
	"snmp", "logging", "ntp", "crypto", "control_plane_policy",
	"ospf_processes",
}

func TestDataMinimization(t *testing.T) {
	for _, q := range discoveryQueries {
		probe := q.filter
		for _, p := range q.rest.parts {
			probe += " " + p.path
		}
		for _, m := range volatileModels {
			if strings.Contains(probe, m) {
				t.Errorf("%s queries volatile state (%s)", q.op, m)
			}
		}
		if q.filter == "" || q.filter == `<native xmlns="`+nsNative+`"/>` {
			t.Errorf("%s reads a whole datastore", q.op)
		}
		for _, p := range q.rest.parts {
			if p.path == "Cisco-IOS-XE-native:native" || p.path == nativePath {
				t.Errorf("%s reads the whole native configuration over RESTCONF", q.op)
			}
		}
	}

	for _, scenario := range goldenScenarios {
		for _, purpose := range []string{"documentation", "audit"} {
			path := filepath.Join(goldenRoot, scenario, "purpose", "golden_"+purpose+".json")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var doc any
			if err := json.Unmarshal(data, &doc); err != nil {
				t.Fatal(err)
			}
			walkJSON(doc, func(key string, v any) {
				if forbiddenKeys[key] {
					t.Errorf("%s: forbidden key %q", path, key)
				}
				if s, ok := v.(string); ok && (strings.Contains(s, "\n!") || strings.Contains(s, "Building configuration")) {
					t.Errorf("%s: %q holds raw configuration text", path, key)
				}
			})
			if purpose != "documentation" {
				continue
			}
			var typed sdk.Document
			if err := json.Unmarshal(data, &typed); err != nil {
				t.Fatal(err)
			}
			for _, r := range typed.Topology.Resources {
				switch r.Type {
				case "osiris.cisco.acl", "network.security.certificate":
					t.Errorf("%s: audit-only resource %s", path, r.ID)
				}
				cisco, _ := r.Extensions[extensionNamespace].(map[string]any)
				for _, k := range auditOnlyKeys {
					if _, ok := cisco[k]; ok {
						t.Errorf("%s: audit-only extension %q on %s", path, k, r.ID)
					}
				}
				if inv, ok := cisco["inventory"].([]any); ok {
					for _, item := range inv {
						if _, ok := item.(map[string]any)["serial"]; ok {
							t.Errorf("%s: inventory serial at documentation purpose", path)
						}
					}
				}
			}
		}
	}
}

// walkJSON calls fn for every object member of v, recursively.
func walkJSON(v any, fn func(key string, v any)) {
	switch t := v.(type) {
	case map[string]any:
		for k, c := range t {
			fn(k, c)
			walkJSON(c, fn)
		}
	case []any:
		for _, c := range t {
			walkJSON(c, fn)
		}
	}
}

// canaryReplies loads the c8000v scenario with every credential
// placeholder replaced by canary and plants canary under
// credential-named elements of the certificates (operational) reply.
func canaryReplies(t *testing.T, canary string) []goldenReply {
	t.Helper()
	replies := loadGoldenReplies(t, filepath.Join(goldenRoot, "c8000v"))
	for i := range replies {
		replies[i].data = strings.ReplaceAll(replies[i].data, fixtureCredential, canary)
		if replies[i].filter == qCertificates.filter {
			replies[i].data = strings.Replace(replies[i].data, "<label>LAB-CA</label>",
				"<label>LAB-CA</label><key>"+canary+"</key><enrollment-password>"+canary+"</enrollment-password>", 1)
		}
	}
	return replies
}

func TestSecretCanary(t *testing.T) {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		t.Fatal(err)
	}
	canary := "LAB" + hex.EncodeToString(buf)
	replies := canaryReplies(t, canary)
	dir := t.TempDir()
	writeCanaryFixtures(t, dir, replies)

	for _, tc := range []struct {
		name    string
		purpose string
		raw     bool
	}{
		{"documentation", "documentation", false},
		{"audit", "audit", false},
		{"audit-raw", "audit", true},
	} {
		for _, transport := range []string{transportNETCONF, transportRESTCONF} {
			t.Run(tc.name+"/"+transport, func(t *testing.T) {
				var logs bytes.Buffer
				logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
				cfg := &sdk.ProducerConfig{Purpose: tc.purpose, IncludeRawBody: tc.raw, SafeFailureMode: sdk.FailClosed}
				var doc *sdk.Document
				if transport == transportNETCONF {
					doc = collectGoldenWith(t, replies, cfg, logger)
				} else {
					doc = collectRESTCONFWith(t, dir, cfg, logger)
				}
				out, err := sdk.MarshalDocument(doc)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(out), canary) {
					t.Error("canary credential reached the document")
				}
				if strings.Contains(logs.String(), canary) {
					t.Error("canary credential reached the log stream")
				}
				if tc.raw {
					assertRawReplies(t, doc)
				}
			})
		}
	}
}

// writeCanaryFixtures writes replies as a scenario directory for the
// RESTCONF test server.
func writeCanaryFixtures(t *testing.T, dir string, replies []goldenReply) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "netconf"), 0o700); err != nil {
		t.Fatal(err)
	}
	for i, q := range discoveryQueries {
		if err := os.WriteFile(filepath.Join(dir, "netconf", fixtureFile(q.op)), []byte(replies[i].data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// collectRESTCONFWith runs Collect over RESTCONF against the scenario
// in dir.
func collectRESTCONFWith(t *testing.T, dir string, cfg *sdk.ProducerConfig, logger *slog.Logger) *sdk.Document {
	t.Helper()
	srv := restconfServer(t, dir)
	defer srv.Close()
	ctx := testharness.NewTestContext(t, testharness.WithConfig(cfg))
	ctx.Logger = logger
	p := &Producer{
		target: run.TargetConfig{Host: "192.0.2.10", Hostname: "LAB-TARGET"},
		cfg:    &Config{Purpose: cfg.Purpose, IncludeRawBody: cfg.IncludeRawBody, Transport: transportRESTCONF},
		rest:   newTestRESTClient(srv),
	}
	doc, err := p.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	return doc
}

// assertRawReplies checks that raw mode attached operational replies
// only and listed every configuration operation as withheld.
func assertRawReplies(t *testing.T, doc *sdk.Document) {
	t.Helper()
	var cisco map[string]any
	for _, r := range doc.Topology.Resources {
		if r.Type == "network.router" {
			cisco = r.Extensions[extensionNamespace].(map[string]any)
		}
	}
	replies, _ := cisco["raw_replies"].(map[string]string)
	if replies["certificates"] == "" || !strings.Contains(replies["certificates"], "<key>REDACTED</key>") {
		t.Errorf("certificates raw reply missing or unredacted")
	}
	config := map[string]bool{}
	for _, q := range discoveryQueries {
		config[q.op] = q.config
	}
	for op := range replies {
		if config[op] {
			t.Errorf("configuration reply %s attached as raw", op)
		}
	}
	withheld := map[string]string{}
	for _, o := range cisco["raw_omitted"].([]map[string]any) {
		withheld[o["operation"].(string)] = o["reason"].(string)
	}
	for _, op := range []string{"native-system", "aaa", "local-accounts", "snmp", "ntp", "crypto"} {
		if withheld[op] != rawOmitConfiguration {
			t.Errorf("%s: withheld reason %q, want %q", op, withheld[op], rawOmitConfiguration)
		}
	}
}
