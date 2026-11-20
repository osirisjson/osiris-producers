// apierror.go - Classified API errors for the Cisco Catalyst SD-WAN
// Manager OSIRIS JSON Producer.
//
// Every failed request or decode is an apiError carrying the endpoint
// path (never its query string), the HTTP status when there was one,
// and a fixed failure category. The category drives the discovery
// coverage record (coverage.go) and lets an OSIRIS JSON consumer tell a
// permission gap from an unsupported endpoint, a malformed reply or a
// transport problem. Categories are fixed labels and never carry
// response or transport text:
//
//	denied        HTTP 401 or 403
//	not_found     HTTP 404 (the endpoint or object does not exist on
//	              this Manager release)
//	rate_limited  HTTP 429 after the bounded retries
//	server_error  HTTP 5xx, or a 200 reply carrying an error object
//	http_error    any other non-200 status
//	malformed     a 200 reply that is not the documented shape
//	too_large     a reply larger than maxResponseBytes
//	redirect      a redirect refused by the origin policy
//	timeout       the per-request timeout expired
//	canceled      the run job was canceled (for example Ctrl-C)
//	transport     any other connection or TLS failure
//
// OSIRIS JSON Producer for Cisco Catalyst SD-WAN Manager introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification
package catalystsdwanmanager

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
)

// Failure categories, see the file comment.
const (
	categoryDenied      = "denied"
	categoryNotFound    = "not_found"
	categoryRateLimited = "rate_limited"
	categoryServerError = "server_error"
	categoryHTTPError   = "http_error"
	categoryMalformed   = "malformed"
	categoryTooLarge    = "too_large"
	categoryRedirect    = "redirect"
	categoryTimeout     = "timeout"
	categoryCanceled    = "canceled"
	categoryTransport   = "transport"
)

// apiError is a classified failure of one API operation.
type apiError struct {
	Op       string // endpoint path without query string.
	Status   int    // HTTP status, 0 when no response was received.
	Category string
	Err      error // underlying cause, never response content.
}

func (e *apiError) Error() string {
	if e.Status != 0 && e.Category != categoryMalformed && e.Category != categoryTooLarge {
		return fmt.Sprintf("SD-WAN Manager %s failed (HTTP %d)", e.Op, e.Status)
	}
	if e.Err != nil {
		return fmt.Sprintf("SD-WAN Manager %s: %s: %v", e.Op, e.Category, e.Err)
	}
	return fmt.Sprintf("SD-WAN Manager %s: %s", e.Op, e.Category)
}

func (e *apiError) Unwrap() error { return e.Err }

// statusCategory maps a non-200 HTTP status to its category.
func statusCategory(status int) string {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return categoryDenied
	case status == http.StatusNotFound:
		return categoryNotFound
	case status == http.StatusTooManyRequests:
		return categoryRateLimited
	case status >= 500:
		return categoryServerError
	}
	return categoryHTTPError
}

// transportError classifies an error from http.Client.Do or a body
// read, with the request URL already stripped by transportCause.
func transportError(op string, err error) *apiError {
	cause := transportCause(err)
	cat := categoryTransport
	var ne net.Error
	switch {
	case errors.Is(err, context.Canceled):
		cat = categoryCanceled
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &ne) && ne.Timeout():
		cat = categoryTimeout
	case errors.Is(err, errCrossOriginRedirect), errors.Is(err, errTooManyRedirects):
		cat = categoryRedirect
	}
	return &apiError{Op: op, Category: cat, Err: cause}
}

// failureCategory returns the category of err, categoryTransport for an
// unclassified error.
func failureCategory(err error) string {
	var ae *apiError
	if errors.As(err, &ae) {
		return ae.Category
	}
	if errors.Is(err, context.Canceled) {
		return categoryCanceled
	}
	return categoryTransport
}
