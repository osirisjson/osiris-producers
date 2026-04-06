// httpclient.go - HTTP client factory for Cisco producers.
// Returns a configured *http.Client with TLS settings and a cookie jar.
// Each producer builds its own API client on top of this transport.
//
// For an introduction to OSIRIS JSON Producer for Cisco see:
// "[OSIRIS-JSON-CISCO]."
//
// [OSIRIS-JSON-CISCO]: https://osirisjson.org/en/docs/producers/network/cisco

package run

import (
	"crypto/tls"
	"net/http"
	"net/http/cookiejar"
)

// NewHTTPClient returns an *http.Client with TLS configuration and a cookie jar.
// When insecure is true, TLS certificate verification is skipped.
func NewHTTPClient(insecure bool) *http.Client {
	jar, _ := cookiejar.New(nil) // nil options is valid; error is always nil.
	return &http.Client{
		Jar: jar,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: insecure, //nolint:gosec // user-requested via --insecure flag.
			},
		},
	}
}
