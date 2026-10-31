// coverage.go - Discovery-coverage record for the Cisco IOS-XE OSIRIS
// JSON Producer.
//
// Accumulates one outcome per discovery operation and renders it twice:
// as the osiris.cisco "coverage" extension on the device resource and
// as a one-line metadata.scope.description summary. Outcomes are:
//
//   - succeeded: the model returned at least one modeled entry;
//   - empty: a valid reply with no instances (nothing configured);
//   - unsupported: the device rejected the model or operation as not
//     implemented;
//   - failed: the query or its decode failed, with a sanitized category;
//   - skipped: not attempted at the selected --purpose flag.
//
// An OSIRIS JSON Consumer holding only the document can therefore tell
// "the device has none" from "this producer could not ask".
// Categories are fixed labels derived from classified errors and never
// carry raw device or transport text.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package iosxe

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
)

// Coverage outcome statuses.
const (
	statusSucceeded   = "succeeded"
	statusEmpty       = "empty"
	statusUnsupported = "unsupported"
	statusFailed      = "failed"
	statusSkipped     = "skipped"
)

// opOutcome is one discovery operation's result.
type opOutcome struct {
	Operation string
	Status    string
	Count     int
	Category  string // set for failed and skipped outcomes.
}

// coverageRecorder accumulates outcomes in discovery order. The zero
// value is ready to use.
type coverageRecorder struct {
	ops []opOutcome
}

// record classifies one attempted operation from its row count and
// error. A nil error with zero rows, or errEmptyModel, is empty.
func (r *coverageRecorder) record(op string, count int, err error) opOutcome {
	o := opOutcome{Operation: op, Count: count}
	switch {
	case err == nil && count > 0:
		o.Status = statusSucceeded
	case err == nil, errors.Is(err, errEmptyModel):
		o.Status, o.Count = statusEmpty, 0
	case errors.Is(err, errRPCUnsupported):
		o.Status, o.Count = statusUnsupported, 0
	default:
		o.Status, o.Count, o.Category = statusFailed, 0, failureCategory(err)
	}
	r.ops = append(r.ops, o)
	return o
}

// skipped records an operation not attempted at the selected purpose.
func (r *coverageRecorder) skipped(op, reason string) {
	r.ops = append(r.ops, opOutcome{Operation: op, Status: statusSkipped, Category: reason})
}

// asSlice renders the record for the osiris.cisco coverage extension.
func (r *coverageRecorder) asSlice() []map[string]any {
	out := make([]map[string]any, 0, len(r.ops))
	for _, o := range r.ops {
		m := map[string]any{"operation": o.Operation, "status": o.Status, "count": o.Count}
		if o.Category != "" {
			m["category"] = o.Category
		}
		out = append(out, m)
	}
	return out
}

// summary is the one-line form for metadata.scope.description: the
// device, how many operations returned data, which had no instances,
// and every operation not represented in the document and why.
func (r *coverageRecorder) summary(device string) string {
	var ok int
	var empty, gaps []string
	for _, o := range r.ops {
		switch o.Status {
		case statusSucceeded:
			ok++
		case statusEmpty:
			empty = append(empty, o.Operation)
		case statusUnsupported:
			gaps = append(gaps, o.Operation+" (unsupported)")
		case statusFailed:
			gaps = append(gaps, o.Operation+" (failed: "+o.Category+")")
		case statusSkipped:
			gaps = append(gaps, o.Operation+" (skipped: "+o.Category+")")
		}
	}

	b := "Cisco IOS-XE device"
	if device != "" {
		b += " " + device
	}
	b += fmt.Sprintf(": %d/%d discovery operations returned data.", ok, len(r.ops))
	if len(empty) > 0 {
		b += " No instances: " + strings.Join(empty, ", ") + "."
	}
	if len(gaps) > 0 {
		b += " Not represented: " + strings.Join(gaps, ", ") + "."
	}
	return b
}

// failureCategory reduces a discovery error to a stable, non-sensitive
// label. It inspects only error identity, never error text.
func failureCategory(err error) string {
	var ne net.Error
	switch {
	case err == nil:
		return ""
	case errors.Is(err, errSessionUnusable):
		return "session-unusable"
	case errors.Is(err, os.ErrDeadlineExceeded), errors.As(err, &ne) && ne.Timeout():
		return "timeout"
	case errors.Is(err, errReplyTooLarge):
		return "reply-too-large"
	case errors.Is(err, errMalformedFrame):
		return "malformed-frame"
	case errors.Is(err, errMessageIDMismatch):
		return "message-id-mismatch"
	case errors.Is(err, errInvalidReply):
		return "invalid-reply"
	case errors.Is(err, errRPCAccessDenied):
		return "access-denied"
	case errors.Is(err, errRPCFailed):
		return "rpc-error"
	case errors.Is(err, errDecode):
		return "decode-error"
	default:
		return "transport-error"
	}
}
