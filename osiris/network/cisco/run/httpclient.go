// httpclient.go - HTTP client factory for Cisco OSIRIS JSON Producer.
// Returns a configured *http.Client with TLS settings and a cookie jar.
// Each producer builds its own API client on top of this transport.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package run

import (
	"crypto/tls"
	"net/http"
	"net/http/cookiejar"
)

// NewHTTPClient returns an *http.Client with TLS configuration and a
// cookie jar.
// When insecure is true, TLS certificate verification is skipped.
// TLS 1.2 is the enforced floor regardless of insecure: skipping
// certificate verification is an explicit, opt-in trust decision about
// the peer's identity, not license to also negotiate a weak protocol
// version. Callers that need a request timeout, context cancellation,
// or bounded response bodies (all connection/request-level concerns,
// not transport-level) add those on their own *http.Client.
func NewHTTPClient(insecure bool) *http.Client {
	jar, _ := cookiejar.New(nil) // nil options is valid; error is always nil.
	return &http.Client{
		Jar: jar,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: insecure, //nolint:gosec // user-requested via --insecure flag.
				MinVersion:         tls.VersionTLS12,
			},
		},
	}
}
