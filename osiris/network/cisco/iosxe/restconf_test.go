// restconf_test.go - Tests for the RESTCONF transport of the Cisco
// IOS-XE OSIRIS JSON Producer: HTTP status mapping, bounds, redirect
// and secrets handling, multi-resource merging, the --transport auto
// fallback and parity with NETCONF on the golden scenarios.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package iosxe

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.osirisjson.org/producers/osiris/network/cisco/run"
	"go.osirisjson.org/producers/pkg/sdk"
	"go.osirisjson.org/producers/pkg/testharness"
)

// newTestRESTClient returns a restClient talking to srv.
func newTestRESTClient(srv *httptest.Server) *restClient {
	c := srv.Client()
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &restClient{http: c, base: srv.URL + "/restconf/data/", username: "admin", password: "secret"}
}

func TestRESTClientGet_StatusMapping(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch strings.TrimPrefix(r.URL.Path, "/restconf/data/") {
		case "ok":
			w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>` + "\n" + `<hostname xmlns="` + nsNative + `">LAB-RTR01</hostname>`))
		case "empty":
			w.WriteHeader(http.StatusNoContent)
		case "missing":
			w.WriteHeader(http.StatusNotFound)
		case "denied":
			w.WriteHeader(http.StatusUnauthorized)
		case "forbidden":
			w.WriteHeader(http.StatusForbidden)
		case "broken":
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`<errors xmlns="urn:ietf:params:xml:ns:yang:ietf-restconf"><error><error-type>application</error-type><error-tag>operation-failed</error-tag><error-message>LAB` + "\n\t  " + `failure</error-message></error></errors>`))
		case "redirect":
			http.Redirect(w, r, "/elsewhere", http.StatusFound)
		case "elsewhere":
			t.Error("a redirect must not be followed")
		case "huge":
			w.Write(bytes.Repeat([]byte("a"), maxReplyBytes+1))
		}
	}))
	defer srv.Close()
	c := newTestRESTClient(srv)

	body, err := c.get("ok", false)
	if err != nil || string(body) != `<hostname xmlns="`+nsNative+`">LAB-RTR01</hostname>` {
		t.Errorf("ok: %q, %v (the XML declaration must be stripped)", body, err)
	}
	if body, err := c.get("empty", false); err != nil || len(body) != 0 {
		t.Errorf("204 must be an empty body, got %q, %v", body, err)
	}
	for path, want := range map[string]error{
		"missing":   errRPCUnsupported,
		"denied":    errRPCAccessDenied,
		"forbidden": errRPCAccessDenied,
		"broken":    errRPCFailed,
		"redirect":  errRPCFailed,
		"huge":      errReplyTooLarge,
	} {
		_, err := c.get(path, false)
		if !errors.Is(err, want) {
			t.Errorf("%s: got %v, want %v", path, err, want)
		}
	}
	_, err = c.get("broken", false)
	if !strings.Contains(err.Error(), "[operation-failed] LAB failure") || strings.ContainsAny(err.Error(), "\n\t") {
		t.Errorf("error detail must be sanitized: %q", err)
	}
	if got := failureCategory(func() error { _, e := c.get("denied", false); return e }()); got != "access-denied" {
		t.Errorf("401 category = %q", got)
	}
}

func TestRESTClientGet_RequestShape(t *testing.T) {
	var gotURL, gotAccept, gotUser, gotPass string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL, gotAccept = r.URL.String(), r.Header.Get("Accept")
		gotUser, gotPass, _ = r.BasicAuth()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c := newTestRESTClient(srv)

	if _, err := c.get("Cisco-IOS-XE-native:native/vrf", true); err != nil {
		t.Fatal(err)
	}
	if gotURL != "/restconf/data/Cisco-IOS-XE-native:native/vrf?content=config" {
		t.Errorf("config URL = %q", gotURL)
	}
	if gotAccept != restMediaType || gotUser != "admin" || gotPass != "secret" {
		t.Errorf("accept/auth = %q %q %q", gotAccept, gotUser, gotPass)
	}
	if strings.Contains(gotURL, "secret") {
		t.Error("secretss must never appear in the URL")
	}
	if _, err := c.get("Cisco-IOS-XE-interfaces-oper:interfaces", false); err != nil || strings.Contains(gotURL, "content=") {
		t.Errorf("state URL = %q, %v", gotURL, err)
	}
}

func TestRESTClientQuery_Merge(t *testing.T) {
	routes := map[string]struct {
		code int
		body string
	}{
		"Cisco-IOS-XE-native:native/ip/route":   {200, `<route xmlns="` + nsNative + `"><ip-route-interface-forwarding-list><prefix>0.0.0.0</prefix><mask>0.0.0.0</mask><fwd-list><fwd>198.51.100.254</fwd></fwd-list></ip-route-interface-forwarding-list></route>`},
		"Cisco-IOS-XE-native:native/ipv6/route": {404, ``},
		"Cisco-IOS-XE-native:native/version":    {204, ``},
		"Cisco-IOS-XE-native:native/hostname":   {204, ``},
		"Cisco-IOS-XE-vlan-oper:vlans":          {404, ``},
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rt := routes[strings.TrimPrefix(r.URL.Path, "/restconf/data/")]
		w.WriteHeader(rt.code)
		w.Write([]byte(rt.body))
	}))
	defer srv.Close()
	c := newTestRESTClient(srv)

	data, err := c.query(qStaticRoutes)
	if err != nil {
		t.Fatal(err)
	}
	r, err := decodeModel[nativeRoutes](data)
	if err != nil || len(TransformStaticRoutes(r)) != 1 {
		t.Errorf("merged routes = %s, %v", data, err)
	}
	// Every resource empty: an empty payload, recorded as no instances.
	if data, err := c.query(qNativeSystem); err != nil || len(data) != 0 {
		t.Errorf("all-204 = %q, %v", data, err)
	}
	// Every resource missing: unsupported.
	if _, err := c.query(qVLANs); !errors.Is(err, errRPCUnsupported) {
		t.Errorf("all-404 = %v", err)
	}
}

func TestConnect_AutoFallsBackOnlyWhenNETCONFUnreachable(t *testing.T) {
	// A port that refuses connections: listen, then close.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()

	ctx := testharness.NewTestContext(t)
	target := run.TargetConfig{Host: "127.0.0.1", Port: port, Username: "admin", Password: "secret"}

	p := &Producer{target: target, cfg: &Config{Transport: transportAuto, InsecureTLS: true}}
	src, transport, err := p.connect(ctx.Logger)
	if err != nil || transport != transportRESTCONF {
		t.Fatalf("auto: transport %q, err %v", transport, err)
	}
	if rc := src.(*restClient); rc.base != "https://127.0.0.1:443/restconf/data/" {
		t.Errorf("fallback must use the RESTCONF default port, base %q", rc.base)
	}

	p = &Producer{target: target, cfg: &Config{Transport: transportNETCONF, InsecureTLS: true}}
	if _, _, err := p.connect(ctx.Logger); !errors.Is(err, errNETCONFUnreachable) {
		t.Errorf("netconf must not fall back, got %v", err)
	}

	p = &Producer{target: target, cfg: &Config{Transport: transportRESTCONF}}
	if _, transport, err := p.connect(ctx.Logger); err != nil || transport != transportRESTCONF {
		t.Errorf("restconf: %q, %v", transport, err)
	}
}

// extractChild returns the raw XML of the element reached by names
// below the root of doc, with xmlns set to ns as a device would send
// it, or nil when absent.
func extractChild(t *testing.T, doc []byte, ns string, names []string) []byte {
	t.Helper()
	d := xml.NewDecoder(bytes.NewReader(doc))
	var stack []string // element names below the root
	var out []byte
	depth := 0
	start := int64(-1)
	want := strings.Join(names, "/")
	for {
		off := d.InputOffset()
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatalf("fixture: %v", err)
		}
		switch e := tok.(type) {
		case xml.StartElement:
			depth++
			if depth > 1 {
				stack = append(stack, e.Name.Local)
			}
			if start < 0 && strings.Join(stack, "/") == want {
				start = off
			}
		case xml.EndElement:
			if start >= 0 && strings.Join(stack, "/") == want {
				// Every matching sibling is returned, as for a list
				// resource with several entries.
				raw := string(doc[start:d.InputOffset()])
				tag := "<" + names[len(names)-1]
				out = append(out, []byte(tag+` xmlns="`+ns+`"`+strings.TrimPrefix(raw, tag))...)
				start = -1
			}
			if depth > 1 {
				stack = stack[:len(stack)-1]
			}
			depth--
		}
	}
}

// restconfServer serves under testdata/ the netconf/<operation>.xml
// fixtures of a golden scenario as a RESTCONF device would: a top-level
// container as is, a child resource as that child element, 204 when
// there is no data.
func restconfServer(t *testing.T, dir string) *httptest.Server {
	t.Helper()
	type resource struct {
		data   []byte
		ns     string
		names  []string
		config bool
	}
	resources := map[string]resource{}
	for _, q := range discoveryQueries {
		path := filepath.Join(dir, "netconf", fixtureFile(q.op))
		var data []byte
		if _, err := os.Stat(path); err == nil {
			data = testharness.LoadFixture(t, path)
		}
		ns := ""
		if i := strings.Index(q.filter, `xmlns="`); i >= 0 {
			ns = q.filter[i+7:]
			ns = ns[:strings.Index(ns, `"`)]
		}
		for _, p := range q.rest.parts {
			segs := strings.Split(p.path, "/")
			names := make([]string, 0, len(segs)-1)
			for _, seg := range segs[1:] {
				// A node from an augmenting module carries its module
				// prefix in the path (RFC 8040 section 3.5.3).
				names = append(names, seg[strings.Index(seg, ":")+1:])
			}
			resources[p.path] = resource{data: data, ns: ns, names: names, config: q.config}
		}
	}
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/restconf/data/")
		res, ok := resources[path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if res.config != (r.URL.Query().Get("content") == "config") {
			t.Errorf("%s: content=config must be sent exactly for configuration resources", path)
		}
		body := bytes.TrimSpace(res.data)
		if len(res.names) > 0 && len(body) > 0 {
			body = extractChild(t, res.data, res.ns, res.names)
		} else if !onlyMarkup(body) {
			body = append([]byte(`<?xml version="1.0" encoding="UTF-8"?>`+"\n"), body...)
		}
		if len(body) == 0 || onlyMarkup(body) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", restMediaType)
		w.Write(body)
	}))
}

// TestGolden_RESTCONF collects every golden scenario over RESTCONF and
// requires the same document, byte for byte, as over NETCONF.
func TestGolden_RESTCONF(t *testing.T) {
	for _, scenario := range goldenScenarios {
		dir := filepath.Join(goldenRoot, scenario)
		for _, purpose := range []string{"documentation", "audit"} {
			t.Run(scenario+"/"+purpose, func(t *testing.T) {
				srv := restconfServer(t, dir)
				defer srv.Close()
				ctx := testharness.NewTestContext(t, testharness.WithConfig(&sdk.ProducerConfig{
					Purpose:         purpose,
					SafeFailureMode: sdk.FailClosed,
				}))
				p := &Producer{
					target: run.TargetConfig{Host: "192.0.2.10", Hostname: "LAB-TARGET"},
					cfg:    &Config{Purpose: purpose, Transport: transportRESTCONF},
					rest:   newTestRESTClient(srv),
				}
				doc, err := p.Collect(ctx)
				if err != nil {
					t.Fatalf("Collect: %v", err)
				}
				assertTopologyInvariants(t, doc)
				testharness.AssertGolden(t, doc, filepath.Join(dir, "purpose", "golden_"+purpose+".json"))
			})
		}
	}
}
