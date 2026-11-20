// config.go - Configuration types for the Cisco Catalyst SD-WAN Manager
// OSIRIS JSON producer.
//
// SD-WAN Manager is a single WAN-overlay controller: one login
// authenticates against one controller (reachable at a
// customer-specific domain such as sdwan-manager.example.com) which
// manages many WAN edge sites. A single SD-WAN Manager run fans out
// into many documents: one per WAN edge site, grouped by device
// site-id.
//
// Output hierarchy (see OutputPath below), one document per site
// regardless of how many sites the controller reports:
//
//	<output-dir>/
//	  <site-name>/
//	    cisco-catalyst-sdwan-manager-<timestamp>-<site-name>.json
//
// <output-dir> defaults to defaultOutputDir when --output is not given.
// A fixed, non-timestamped directory name lets a re-run from the same
// working directory (or the same explicit --output) naturally reuse it:
// a second run adds each site's new timestamped file alongside the
// previous ones instead of scattering a new dated directory per run.
// Directories this producer creates are private (0700) and documents
// are published atomically as private files (0600), see output.go.
//
// SD-WAN Manager's device inventory (GET /dataservice/device) has no
// human-readable site name field, only a numeric site-id, the display
// name used here and in metadata.scope.sites is resolved separately via
// GET /dataservice/topology/device/site/{siteId} (see sites_select.go);
// OutputPath falls back to the "unsited" segment when no name is
// available (empty siteName), the same fallback used for devices with
// no site-id at all.
//
// OSIRIS JSON Producer for Cisco Catalyst SD-WAN Manager introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package catalystsdwanmanager

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
)

// defaultOutputDir is the --output value used when the flag is omitted.
const defaultOutputDir = "osirisjson-cisco-catalyst-sdwan-manager"

// defaultSiteNameRateLimit is the --site-name-rate value used when the
// flag is omitted: 10 requests/second. SD-WAN Manager's rate limit is
// typically shared across every consumer polling the same controller
// (monitoring systems, other automation, not just this producer), so
// this defaults conservatively, tune with --site-name-rate based on
// your own environment's actual headroom, see client.go's
// throttleSiteName.
const defaultSiteNameRateLimit = 10

// unsitedSegment is the OutputPath/scope fallback for devices SD-WAN
// Manager has not assigned to a site (empty site-id).
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

	// RequireComplete is --require-complete: a run whose documents
	// are all written but where an optional operation failed (see the
	// coverage record) exits with an error instead of success.
	RequireComplete bool

	// IncludeRawBody attaches each collected endpoint's API rows,
	// recursively redacted and size-capped (see raw.go), under
	// extensions["osiris.cisco.catalystsdwanmanager"] on the owning
	// device resource, a fallback for fields this producer doesn't
	// model yet into OSIRIS JSON. Only takes effect when
	// '--purpose audit' flag is declared.
	IncludeRawBody bool
}

// OutputPath returns the hierarchical output path for one site's
// document:
// <output-dir>/<segment>/cisco-catalyst-sdwan-manager-<timestamp>-<segment>.json
//
// segment is the site's display name made safe by
// sanitizeFilenameSegment (see siteSegments for the per-run collision
// policy) an empty result (no site-id at all, or a name that
// sanitizes to nothing) maps to the "unclaimed" segment. The directory
// is not required to be new: writeDocument reuses an existing
// <segment> directory and adds the new timestamped file alongside
// whatever is already there, never overwriting an existing file.
func OutputPath(outputDir, timestamp, siteName string) string {
	segment := sanitizeFilenameSegment(siteName)
	if segment == "" {
		segment = unsitedSegment
	}
	filename := fmt.Sprintf("cisco-catalyst-sdwan-manager-%s-%s.json", timestamp, segment)
	return filepath.Join(outputDir, segment, filename)
}

// sanitizeFilenameSegment turns a display label into one safe path
// component (on any of Linux/macOS/Windows): path separators, reserved
// characters, whitespace and control characters become "-", and
// leading/trailing dots are trimmed, so the result can never be "." or
// "..", a hidden name or a name Windows rejects. It returns "" when
// nothing remains; callers fall back to unsitedSegment.
func sanitizeFilenameSegment(s string) string {
	mapped := strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			return '-'
		}
		if r < 0x20 || r == 0x7f || unicode.IsSpace(r) || !unicode.IsPrint(r) {
			return '-'
		}
		return r
	}, s)
	return strings.Trim(mapped, ".")
}

// siteSegments assigns each site of one run a distinct output path
// component. Two different site-ids whose display names sanitize to
// the same component (case-insensitively, so case-folding filesystems
// cannot merge them either) would otherwise write into one directory
// with colliding file names; every site after the first to claim a
// component gets its sanitized site-id appended. The zero value is
// ready to use.
type siteSegments struct {
	owner map[string]string // lower-cased segment -> site-id.
}

// segment returns the output path component for siteID/siteName.
func (s *siteSegments) segment(siteID, siteName string) string {
	if s.owner == nil {
		s.owner = map[string]string{}
	}
	seg := sanitizeFilenameSegment(siteName)
	if seg == "" {
		seg = unsitedSegment
	}
	if id, taken := s.owner[strings.ToLower(seg)]; taken && id != siteID {
		suffix := sanitizeFilenameSegment(siteID)
		if suffix == "" {
			suffix = unsitedSegment
		}
		seg = seg + "-" + suffix
	}
	s.owner[strings.ToLower(seg)] = siteID
	return seg
}
