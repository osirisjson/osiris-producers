// config.go - Configuration types for the Cisco Catalyst SD-WAN Manager
// (vManage) OSIRIS JSON producer.
//
// vManage is a single WAN-overlay controller: one login authenticates
// against one controller (reachable at a customer-specific domain such
// as acme.sdwan.cisco.com) which manages many WAN edge sites. A single
// vManage run fans out into many documents: one per WAN edge site,
// grouped by device site-id.
//
// Output hierarchy (see OutputPath below), one document per site
// regardless of how many sites the controller reports:
//
//	<output-dir>/
//	  <site-name>/
//	    cisco-vmanage-<timestamp>-<site-name>.json
//
// <output-dir> defaults to defaultOutputDir when --output is not given.
// A fixed, non-timestamped directory name lets a re-run from the same
// working directory (or the same explicit --output) naturally reuse it:
// os.MkdirAll is a no-op against an existing directory, so a second run
// adds each site's new timestamped file alongside the previous ones
// instead of scattering a new dated directory per run.
//
// vManage's device inventory (GET /dataservice/device) has no
// human-readable site name field, only a numeric site-id, the display
// name used here and in metadata.scope.sites is resolved separately via
// GET /dataservice/topology/device/site/{siteId} (see sites_select.go);
// OutputPath falls back to the "unsited" segment when no name is
// available (empty siteName), the same fallback used for devices with
// no site-id at all.
//
// OSIRIS JSON Producer for Cisco SD-WAN Manager (vManage) introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package vmanage

import (
	"fmt"
	"path/filepath"
	"strings"
)

// defaultOutputDir is the --output value used when the flag is omitted.
const defaultOutputDir = "osirisjson-cisco-vmanage"

// defaultSiteNameRateLimit is the --site-name-rate value used when the
// flag is omitted: 10 requests/second. vManage's rate limit is
// typically shared across every consumer polling the same controller
// (monitoring systems, other automation, not just this producer), so
// this defaults conservatively, tune with --site-name-rate based on
// your own environment's actual headroom, see client.go's
// throttleSiteName.
const defaultSiteNameRateLimit = 10

// unsitedSegment is the OutputPath/scope fallback for devices vManage
// has not assigned to a site (empty site-id).
const unsitedSegment = "unclaimed"

// Config carries runtime settings resolved from CLI flags.
type Config struct {
	Host        string // controller FQDN or IP.
	Port        int    // 0 = use defaultPort.
	Username    string
	Password    string
	InsecureTLS bool // --insecure: skip TLS verify.

	OutputDir       string
	SafeFailureMode string

	// Purpose is the OSIRIS JSON spec section 13.1.3 output grade.
	Purpose string

	// SiteFilter is the explicit --site value (site-id list, unsitedSegment
	// selects devices with no site-id). Empty when --site was not given.
	SiteFilter []string
	// AllSites is --all: collect every discovered site non-interactively,
	// skipping the picker. Mutually exclusive with SiteFilter.
	AllSites bool

	// SiteNameRateLimit caps requests/second for bulk site-name
	// resolution (see client.go's throttleSiteName), independent of
	// worker concurrency. Defaults to defaultSiteNameRateLimit.
	SiteNameRateLimit int

	// IncludeRawBody attaches each collected endpoint's full,
	// unmodified API response body under
	// extensions["osiris.cisco.vmanage"] on the owning device resource
	// (see wantRawBody in vmanage.go), a lossless fallback for fields
	// this producer doesn't model yet. Only takes effect when
	// '--purpose audit' flag is declared.
	IncludeRawBody bool
}

// OutputPath returns the hierarchical output path for one site's
// document: <output-dir>/<site-name>/cisco-vmanage-<timestamp>-<site-name>.json
//
// Empty site name or no site-id at all, or a site-id whose display name
// could not be resolved) maps to the "unclaimed" segment. The directory
// is not required to be new: os.MkdirAll (called by the caller before
// writing) is a no-op if <site-name> already exists, so this simply
// adds the new timestamped file alongside whatever is already there.
func OutputPath(outputDir, timestamp, siteName string) string {
	segment := sanitizeFilenameSegment(siteName)
	if segment == "" {
		segment = unsitedSegment
	}
	filename := fmt.Sprintf("cisco-vmanage-%s-%s.json", timestamp, segment)
	return filepath.Join(outputDir, segment, filename)
}

// sanitizeFilenameSegment replaces characters that are unsafe in a
// filename (on any of Linux/macOS/Windows) with "-".
func sanitizeFilenameSegment(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|', ' ':
			return '-'
		}
		return r
	}, s)
}
