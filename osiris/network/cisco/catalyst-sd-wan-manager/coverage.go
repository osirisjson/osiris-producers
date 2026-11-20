// coverage.go - Discovery-coverage record for the Cisco Catalyst
// SD-WAN Manager OSIRIS JSON Producer.
//
// Every API operation the producer attempts is recorded once per
// attempt and aggregated per operation. Each document carries the
// record of the operations that fed it (the controller-wide ones and
// its own site's) as metadata["osiris.cisco.catalystsdwanmanager"]:
//
//	{
//	  "api_profile": "unverified",
//	  "complete": false,
//	  "coverage": [
//	    {"operation": "device_interface","status": "partial",
//	     "attempts": 3, "failed": 1, "count": 14,
//	     "categories": {"denied": 1}}
//	  ]
//	}
//
// Statuses: succeeded (every attempt answered, at least one row),
// empty (every attempt answered, no rows), partial (some attempts
// failed), failed (every attempt failed) and skipped (not attempted,
// with a reason). complete is false when any operation is partial or
// failed, so an OSIRIS JSON Consumer holding only the document can tell
// "the Manager has none" from "this producer could not ask". Failure
// categories are the fixed labels of apierror.go and never carry
// response or transport text. api_profile is "unverified": no trusted
// operation identifies the Manager release, so the reply shapes of the
// documented 20.18, 26.1 and 26.2 Cisco API contracts are all accepted.
//
// OSIRIS JSON Producer for Cisco Catalyst SD-WAN Manager introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification
package catalystsdwanmanager

import (
	"sort"
	"strings"
)

// Operation names in the coverage record, in render order.
const (
	opDevice          = "device"
	opOMPLinks        = "omp_links"
	opSiteName        = "site_name"
	opDeviceInterface = "device_interface"
	opWANInterface    = "wan_interface"
	opOMPPeers        = "omp_peers"
	opSiteTopology    = "site_topology"
	opNeighborWAN     = "neighbor_wan_interface"
)

var opOrder = []string{opDevice, opOMPLinks, opSiteName, opDeviceInterface, opWANInterface, opOMPPeers, opSiteTopology, opNeighborWAN}

// Coverage statuses.
const (
	statusSucceeded = "succeeded"
	statusEmpty     = "empty"
	statusPartial   = "partial"
	statusFailed    = "failed"
	statusSkipped   = "skipped"
)

// apiProfileUnverified labels documents collected without a declared
// or discovered Manager release.
const apiProfileUnverified = "unverified"

// opStat aggregates every attempt of one operation.
type opStat struct {
	attempts, failed, count int
	categories              map[string]int
	skipReason              string
}

// coverage accumulates operation outcomes. The zero value is ready to
// use.
type coverage struct {
	ops map[string]*opStat
}

func (c *coverage) stat(op string) *opStat {
	if c.ops == nil {
		c.ops = map[string]*opStat{}
	}
	s, ok := c.ops[op]
	if !ok {
		s = &opStat{}
		c.ops[op] = s
	}
	return s
}

// record adds one attempt of op that returned count rows or failed
// with err.
func (c *coverage) record(op string, count int, err error) {
	s := c.stat(op)
	s.attempts++
	if err != nil {
		s.failed++
		if s.categories == nil {
			s.categories = map[string]int{}
		}
		s.categories[failureCategory(err)]++
		return
	}
	s.count += count
}

// skip records op as not attempted for reason, unless it was attempted.
func (c *coverage) skip(op, reason string) {
	s := c.stat(op)
	if s.attempts == 0 && s.skipReason == "" {
		s.skipReason = reason
	}
}

// merged returns a new coverage holding the attempts of every given
// record.
func merged(records ...*coverage) *coverage {
	out := &coverage{}
	for _, r := range records {
		if r == nil {
			continue
		}
		for op, s := range r.ops {
			d := out.stat(op)
			d.attempts += s.attempts
			d.failed += s.failed
			d.count += s.count
			for k, v := range s.categories {
				if d.categories == nil {
					d.categories = map[string]int{}
				}
				d.categories[k] += v
			}
			if d.attempts == 0 && d.skipReason == "" {
				d.skipReason = s.skipReason
			}
		}
	}
	return out
}

func (s *opStat) status() string {
	switch {
	case s.attempts == 0:
		return statusSkipped
	case s.failed == s.attempts:
		return statusFailed
	case s.failed > 0:
		return statusPartial
	case s.count > 0:
		return statusSucceeded
	}
	return statusEmpty
}

// complete reports whether no operation failed, fully or partially.
func (c *coverage) complete() bool {
	for _, s := range c.ops {
		if s.failed > 0 {
			return false
		}
	}
	return true
}

// gaps returns "operation (status: categories)" for every partial or
// failed operation, in render order, for diagnostics.
func (c *coverage) gaps() []string {
	var out []string
	for _, op := range c.order() {
		s := c.ops[op]
		if s.failed == 0 {
			continue
		}
		cats := make([]string, 0, len(s.categories))
		for k := range s.categories {
			cats = append(cats, k)
		}
		sort.Strings(cats)
		out = append(out, op+" ("+s.status()+": "+strings.Join(cats, ",")+")")
	}
	return out
}

// order returns the recorded operations in opOrder, then any other
// operation alphabetically.
func (c *coverage) order() []string {
	known := map[string]bool{}
	var out []string
	for _, op := range opOrder {
		known[op] = true
		if _, ok := c.ops[op]; ok {
			out = append(out, op)
		}
	}
	var rest []string
	for op := range c.ops {
		if !known[op] {
			rest = append(rest, op)
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}

// metadata renders the record as the producer's metadata extension
// value (see the file comment).
func (c *coverage) metadata() map[string]any {
	ops := make([]any, 0, len(c.ops))
	for _, op := range c.order() {
		s := c.ops[op]
		m := map[string]any{
			"operation": op,
			"status":    s.status(),
			"attempts":  s.attempts,
			"count":     s.count,
		}
		if s.failed > 0 {
			m["failed"] = s.failed
			cats := make(map[string]any, len(s.categories))
			for k, v := range s.categories {
				cats[k] = v
			}
			m["categories"] = cats
		}
		if s.skipReason != "" && s.attempts == 0 {
			m["reason"] = s.skipReason
		}
		ops = append(ops, m)
	}
	return map[string]any{
		"api_profile": apiProfileUnverified,
		"complete":    c.complete(),
		"coverage":    ops,
	}
}
