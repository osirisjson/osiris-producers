// config.go - Configuration types for the
// HPE Aruba Networking Central OSIRIS JSON producer.
//
// Aruba Central is a multi-tenant SaaS network management platform:
// one OAuth2 credential pair authenticates against one customer account
// which may contain many sites.
// For every run, one site or many, writes one OSIRIS JSON document per
// site so document scope always matches a single Aruba Central site.
//
// Output hierarchy (see OutputPath below), one document per site
// regardless of how many sites were collected:
//
//	<output-dir>/
//	  <site-name>/
//	    hpe-arubacentral-<timestamp>-<site-name>.json
//
// <output-dir> defaults to defaultOutputDir when --output is not given.
// Using a fixed, non-timestamped name allows re-running from the same
// working directory (or declaring it with -o or --output) to naturally
// reuse it. Since os.MkdirAll is a no-op if a directory already exists,
// a second run simply adds each site's new timestamped file alongside
// the previous ones in the subdirectory, rather than scattering a new
// dated directory per run. Running against a fresh working directory
// (or an explicit --output that does not yet exist) builds the whole
// hierarchy from scratch.
//
// OSIRIS JSON Producer for HPE Aruba Networking Central introduction:
// [OSIRIS-JSON-HPE-ARUBA-NETWORKING]: https://docs.osirisjson.org/osiris-producers/network/hpe-aruba-networking
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package arubacentral

import (
	"fmt"
	"path/filepath"
)

// defaultOutputDir is the --output value used when the flag is omitted.
const defaultOutputDir = "osirisjson-hpe-arubacentral"

// OutputPath returns the hierarchical output for one site's document:
// <output-dir>/<site-name>/hpe-arubacentral-<timestamp>-<site-name>.json
//
// collection model: one directory per site instead of per tenant, with
// the timestamp folded into the filename instead of an intermediate
// directory segment, matching this producer's existing filename
// convention. siteName == "" is the unfiltered-collection fallback
// (runExport in arubacentral.go) and maps to the "all-sites" segment.
func OutputPath(outputDir, timestamp, siteName string) string {
	segment := sanitizeFilenameSegment(siteName)
	if segment == "" {
		segment = "all-sites"
	}
	filename := fmt.Sprintf("hpe-arubacentral-%s-%s.json", timestamp, segment)
	return filepath.Join(outputDir, segment, filename)
}

// clusterBaseURLs maps the Aruba Central cluster short code (as shown
// in the Central UI under Global Menu -> API Gateway)
// to its API Gateway Base URL.
// Source: Aruba Central API Gateway documentation, "Base URLs" table.
var clusterBaseURLs = map[string]string{
	"eu":            "https://de1.api.central.arubanetworks.com",
	"eucentral2":    "https://de2.api.central.arubanetworks.com",
	"eucentral3":    "https://de3.api.central.arubanetworks.com",
	"ukwest2":       "https://gb1.api.central.arubanetworks.com",
	"prod":          "https://us1.api.central.arubanetworks.com",
	"central-prod2": "https://us2.api.central.arubanetworks.com",
	"uswest4":       "https://us4.api.central.arubanetworks.com",
	"uswest5":       "https://us5.api.central.arubanetworks.com",
	"us-east-1":     "https://us6.api.central.arubanetworks.com",
	"starman":       "https://ca1.api.central.arubanetworks.com",
	"apac":          "https://in1.api.central.arubanetworks.com",
	"apaceast":      "https://jp1.api.central.arubanetworks.com",
	"apacsouth":     "https://au1.api.central.arubanetworks.com",
	"uaenorth1":     "https://ae1.api.central.arubanetworks.com",
	"china-prod":    "https://cn1.api.central.arubanetworks.com.cn",
	"internal":      "https://internal.api.central.arubanetworks.com",
}

// ClusterBaseURL resolves a cluster short code to its API Gateway base
// URL. Returns "" and ok=false when the cluster code is not recognized.
func ClusterBaseURL(cluster string) (string, bool) {
	url, ok := clusterBaseURLs[cluster]
	return url, ok
}

// Credentials carries the OAuth2 client and token material needed to
// authenticate against the Aruba Central API Gateway. Two distinct
// credential shapes are supported:
//
//  1. Classic API Gateway application: Central issues access tokens
//     (short-lived) and refresh tokens (single-use, rotating) from an
//     "API Gateway" application (client_id/client_secret) created in
//     the Central UI. This producer never performs the interactive
//     authorization-code login itself: the user creates the app
//     performs the one-time browser authorization out of band, then
//     supplies the resulting token pair here. On a 401 or expired
//     access token, the client exchanges the refresh token for a new
//     access token (grant_type=refresh_token) and, since Central
//     rotates refresh tokens, persists the new pair back to TokenFile
//     when one was supplied.
//  2. GreenLake Personal API client (personal_api.go): a self-service
//     client_id/client_secret pair generated under user Workspace
//     https://common.cloud.hpe.com/manage-account/api, useful when no
//     IT administrator has provisioned an API Gateway application.
//     There is no pre-issued access/refresh token pair: an access
//     token is minted (and re-minted on 401, in place of a
//     refresh_token exchange) via grant_type=client_credentials
//     against HPE GreenLake's SSO token endpoint. This mode is used
//     automatically whenever client_id and client_secret are both
//     present and refresh_token is not.
type Credentials struct {
	ClientID     string
	ClientSecret string
	AccessToken  string
	RefreshToken string

	// TokenFile, when set, is the JSON file the token pair was loaded
	// from and is rewritten to after a successful refresh so subsequent
	// runs reuse the rotated refresh token instead of failing.
	TokenFile string
}

// Config carries runtime settings resolved from CLI flags and env.
type Config struct {
	BaseURL     string
	Credentials Credentials

	// Cluster is the resolved cluster short code (e.g. "eucentral3"),
	// from --cluster or auto-detection.
	// Recorded in metadata.scope.clusters (see arubacentral.go)
	// document-wide context for which API Gateway deployment this
	// export came from, per OSIRIS JSON spec chapter 4.3.6.
	Cluster string

	// Sites filters collection to the named sites
	// (site-scope "name" field).
	// Empty means collect every site reachable by the credential.
	Sites []string

	OutputDir       string
	SafeFailureMode string // "fail-closed" | "log-and-redact" | "off".

	// Purpose is the OSIRIS JSON spec chapter 13.1.3 output grade.
	Purpose string

	// when true and --purpose audit, attach the full API response body
	// under extensions["osiris.arubacentral"].raw useful for dev.
	IncludeRawBody bool

	// The --exclude flag allows users to prune unwanted fields
	// from the output.
	Exclude []excludePath
}
