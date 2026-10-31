// restconf.go - RESTCONF (RFC 8040) transport for the Cisco IOS-XE
// OSIRIS JSON Producer.
//
// RESTCONF is the alternative to NETCONF for devices that keep the
// NETCONF SSH subsystem disabled. The producer asks for the XML
// encoding (application/yang-data+xml), in which a container resource
// is returned as the same element, in the same module namespace, that a
// NETCONF <data> payload holds.
// Every reply therefore goes through the same decoders and transforms
// as over NETCONF and both transports emit the same document for
// the same device state.
//
// Security and bounds: HTTPS only, TLS 1.2 minimum, certificate
// verification on unless --insecure; secretss travel in the
// Authorization header, never in the URL; redirects are not followed,
// so secrets are never replayed to another host; every request has a
// dial and a total timeout and a reply body larger than maxReplyBytes
// is rejected.
//
// Status mapping, for the discovery coverage record: 200 is data, 204
// means the model has no instances, 404 means the device does not
// implement the resource, 401/403 is access denied and any other status
// is a failure carrying the sanitized RESTCONF error-tag and message.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package iosxe

import (
	"bytes"
	"crypto/tls"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"go.osirisjson.org/producers/osiris/network/cisco/run"
)

const (
	// defaultRESTCONFPort is the HTTPS port RESTCONF listens on.
	defaultRESTCONFPort = 443

	// restTimeout bounds one RESTCONF request end to end.
	restTimeout = 30 * time.Second

	// restMediaType is the RESTCONF XML encoding
	// (RFC 8040 section 11.3).
	restMediaType = "application/yang-data+xml"
)

// restClient issues RESTCONF GET requests against one device.
type restClient struct {
	http     *http.Client
	base     string // https://<host>[:port]/restconf/data/
	username string
	password string
	logger   *slog.Logger
}

// newRESTClient creates a RESTCONF client for target. When insecure is
// true the TLS certificate is not verified (the --insecure flag).
func newRESTClient(target run.TargetConfig, insecure bool, logger *slog.Logger) *restClient {
	addr := run.ResolveAddr(target, defaultRESTCONFPort)
	return &restClient{
		http: &http.Client{
			Timeout: restTimeout,
			Transport: &http.Transport{
				DialContext:         (&net.Dialer{Timeout: dialTimeout}).DialContext,
				TLSHandshakeTimeout: dialTimeout,
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: insecure, //nolint:gosec // user-requested via --insecure flag.
					MinVersion:         tls.VersionTLS12,
				},
			},
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		base:     "https://" + addr + "/restconf/data/",
		username: target.Username,
		password: target.Password,
		logger:   logger,
	}
}

// restErrors is the RESTCONF error envelope (RFC 8040 section 7.1).
type restErrors struct {
	XMLName xml.Name `xml:"urn:ietf:params:xml:ns:yang:ietf-restconf errors"`
	Errors  []struct {
		Type    string `xml:"error-type"`
		Tag     string `xml:"error-tag"`
		Message string `xml:"error-message"`
	} `xml:"error"`
}

// get fetches one data resource and returns its body; an empty body
// means the resource exists but has no instances (HTTP 204).
func (c *restClient) get(path string, config bool) ([]byte, error) {
	url := c.base + path
	if config {
		url += "?content=config"
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("RESTCONF request for %s: %w", path, err)
	}
	req.Header.Set("Accept", restMediaType)
	req.SetBasicAuth(c.username, c.password)

	resp, err := c.http.Do(req)
	if err != nil {
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return nil, fmt.Errorf("RESTCONF GET %s: %w", path, os.ErrDeadlineExceeded)
		}
		return nil, fmt.Errorf("RESTCONF GET %s: %w", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxReplyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("RESTCONF GET %s: reading body: %w", path, err)
	}
	if int64(len(body)) > maxReplyBytes {
		return nil, fmt.Errorf("RESTCONF GET %s: %w", path, errReplyTooLarge)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		return stripXMLDecl(body), nil
	case http.StatusNoContent:
		return nil, nil
	case http.StatusNotFound:
		return nil, fmt.Errorf("RESTCONF GET %s: HTTP 404: %w", path, errRPCUnsupported)
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, fmt.Errorf("RESTCONF GET %s: HTTP %d: %w", path, resp.StatusCode, errRPCAccessDenied)
	}
	detail := ""
	var re restErrors
	if xml.Unmarshal(body, &re) == nil && len(re.Errors) > 0 {
		e := re.Errors[0]
		detail = " [" + sanitizeErrorText(e.Tag, 64) + "]"
		if m := sanitizeErrorText(e.Message, maxErrorMessageRunes); m != "" {
			detail += " " + m
		}
	}
	return nil, fmt.Errorf("RESTCONF GET %s: HTTP %d%s: %w", path, resp.StatusCode, detail, errRPCFailed)
}

// stripXMLDecl removes a leading XML declaration so a reply body can be
// embedded under a wrapper element.
func stripXMLDecl(b []byte) []byte {
	t := bytes.TrimSpace(b)
	if bytes.HasPrefix(t, []byte("<?xml")) {
		if i := bytes.Index(t, []byte("?>")); i >= 0 {
			return bytes.TrimSpace(t[i+2:])
		}
	}
	return t
}

// query runs one discovery operation over RESTCONF: it fetches every
// resource of q.rest, wraps each body as configured and returns one XML
// payload shaped like the NETCONF <data> content for q. Resources with
// no instances are skipped; the payload is empty when none had data.
// The operation is unsupported only when every resource is unsupported.
func (c *restClient) query(q query) ([]byte, error) {
	var buf bytes.Buffer
	got, unsupported := 0, 0
	var unsupportedErr error
	for _, p := range q.rest.parts {
		body, err := c.get(p.path, q.config)
		switch {
		case err == nil && len(body) > 0:
			buf.WriteString(p.open)
			buf.Write(body)
			buf.WriteString(p.close)
			got++
		case err == nil:
		case errors.Is(err, errRPCUnsupported):
			unsupported++
			unsupportedErr = err
		default:
			return nil, err
		}
	}
	if got == 0 {
		if unsupported == len(q.rest.parts) && unsupportedErr != nil {
			return nil, unsupportedErr
		}
		return []byte{}, nil
	}
	return []byte(q.rest.open + buf.String() + q.rest.close), nil
}

// Close releases idle connections.
func (c *restClient) Close() error {
	c.http.CloseIdleConnections()
	return nil
}
