// netconf.go - NETCONF message framing for the Cisco IOS-XE producer.
//
// Implements both framing mechanisms of RFC 6242 section 4 on top of
// any io.Reader/io.Writer pair, independent of the SSH session that
// carries them:
//
//   - end-of-message framing (base:1.0): each message is terminated by
//     the ]]>]]> delimiter;
//   - chunked framing (base:1.1): each message is one or more
//     "\n#<size>\n<data>" chunks followed by "\n##\n".
//
// The <hello> exchange always uses end-of-message framing. Both peers
// then switch to chunked framing only when both advertised
// urn:ietf:params:netconf:base:1.1 (see negotiateFraming).
//
// Every read is bounded by maxReplyBytes. A chunk header announcing
// more data than the remaining budget is rejected before its payload
// is read, so a malicious or broken peer cannot force unbounded
// allocation. Truncated or malformed frames fail with errMalformedFrame
// instead of being returned as a partial message.
//
// Replies are decoded by XML namespace and local name (parseRPCReply),
// so default-namespace and prefixed <rpc-reply> documents are handled
// identically. Each reply's message-id must match its request. Device
// <rpc-error> elements become classified, sanitized errors that never
// carry raw reply bytes, error-path, or error-info content.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [RFC 6242]: https://www.rfc-editor.org/rfc/rfc6242#section-4

package iosxe

import (
	"bufio"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"
)

const (
	netconfDelim  = "]]>]]>"
	maxReplyBytes = 10 * 1024 * 1024 // 10 MiB safety limit on a single NETCONF reply.

	// ncBaseNS is the XML namespace of every NETCONF protocol element
	// (rpc, rpc-reply, rpc-error, data, ok), for base:1.0 and base:1.1.
	ncBaseNS = "urn:ietf:params:xml:ns:netconf:base:1.0"

	capBase10 = "urn:ietf:params:netconf:base:1.0"
	capBase11 = "urn:ietf:params:netconf:base:1.1"

	// maxChunkSize is the largest chunk-size RFC 6242 section 4.2 allows.
	maxChunkSize = 4294967295
	// maxChunkSizeDigits is the length of maxChunkSize in decimal.
	maxChunkSizeDigits = 10
)

var (
	// errReplyTooLarge is returned when a NETCONF message grows, or a
	// chunk header announces growth, past maxReplyBytes. It lets
	// callers tell a resource limit apart from a transport or decode
	// failure.
	errReplyTooLarge = errors.New("NETCONF reply exceeds the maximum allowed size")

	// errMalformedFrame is returned when the byte stream violates the
	// negotiated framing: a bad chunk header, a zero or oversized chunk
	// size, a message with no chunks, or the stream ending mid-message.
	errMalformedFrame = errors.New("malformed NETCONF frame")

	// errUnsupportedBase is returned when the server's <hello>
	// advertises neither base:1.0 nor base:1.1, so no common
	// framing exists.
	errUnsupportedBase = errors.New("NETCONF server advertises no supported base protocol version")

	// errInvalidReply is returned when a framed message is not a usable
	// <rpc-reply>: malformed XML, a different root element or
	// namespace, or no <data> where data was requested.
	errInvalidReply = errors.New("invalid NETCONF rpc-reply")

	// errMessageIDMismatch is returned when a reply's message-id does
	// not echo the request's, meaning the reply belongs to another
	// request and the session can no longer be trusted to pair replies.
	errMessageIDMismatch = errors.New("NETCONF reply message-id does not match the request")

	// errRPCUnsupported classifies an <rpc-error> (or a RESTCONF 404)
	// stating the device does not implement the requested operation,
	// model, or element.
	errRPCUnsupported = errors.New("operation or model not supported by the device")

	// errRPCAccessDenied classifies an <rpc-error> with error-tag
	// access-denied, or a RESTCONF 401/403.
	errRPCAccessDenied = errors.New("access denied by the device")

	// errRPCFailed classifies every other <rpc-error> of severity
	// error, or any other unsuccessful RESTCONF status.
	errRPCFailed = errors.New("device operation failed")
)

// framer reads and writes NETCONF messages in the currently negotiated
// framing. It starts in end-of-message mode, as RFC 6242 requires for
// the <hello> exchange. A framer is not safe for concurrent use; the
// owning transport serializes access.
type framer struct {
	r       *bufio.Reader
	w       io.Writer
	chunked bool  // true once base:1.1 framing has been negotiated.
	limit   int64 // maximum message size; maxReplyBytes when zero.
}

// newFramer returns a framer in end-of-message mode over r and w.
func newFramer(r io.Reader, w io.Writer) *framer {
	return &framer{r: bufio.NewReader(r), w: w}
}

func (f *framer) maxBytes() int64 {
	if f.limit > 0 {
		return f.limit
	}
	return maxReplyBytes
}

// readMessage returns the next complete message with its framing
// removed.
func (f *framer) readMessage() ([]byte, error) {
	if f.chunked {
		return f.readChunked()
	}
	return f.readEOM()
}

// writeMessage frames msg in the negotiated framing and writes it with
// a single Write call.
func (f *framer) writeMessage(msg []byte) error {
	var out []byte
	if f.chunked {
		if len(msg) == 0 {
			return fmt.Errorf("%w: cannot send an empty chunked message", errMalformedFrame)
		}
		out = make([]byte, 0, len(msg)+24)
		out = append(out, "\n#"...)
		out = strconv.AppendInt(out, int64(len(msg)), 10)
		out = append(out, '\n')
		out = append(out, msg...)
		out = append(out, "\n##\n"...)
	} else {
		out = make([]byte, 0, len(msg)+len(netconfDelim))
		out = append(out, msg...)
		out = append(out, netconfDelim...)
	}
	_, err := f.w.Write(out)
	return err
}

// readEOM reads up to and including the ]]>]]> delimiter. It reads in
// slices ending at '>' (the delimiter's last byte), so the delimiter is
// only ever checked at a possible end position and no byte past it is
// consumed from the underlying reader.
func (f *framer) readEOM() ([]byte, error) {
	var buf bytes.Buffer
	limit := f.maxBytes()
	for {
		part, err := f.r.ReadSlice('>')
		buf.Write(part)
		if int64(buf.Len()) > limit+int64(len(netconfDelim)) {
			return nil, fmt.Errorf("%w (%d bytes)", errReplyTooLarge, limit)
		}
		if bytes.HasSuffix(buf.Bytes(), []byte(netconfDelim)) {
			return buf.Bytes()[:buf.Len()-len(netconfDelim)], nil
		}
		if err != nil && !errors.Is(err, bufio.ErrBufferFull) {
			if errors.Is(err, io.EOF) {
				return nil, fmt.Errorf("%w: stream ended before the ]]>]]> delimiter", errMalformedFrame)
			}
			return nil, fmt.Errorf("NETCONF read failed: %w", err)
		}
	}
}

// readChunked reads one chunked-framing message (RFC 6242 section 4.2).
func (f *framer) readChunked() ([]byte, error) {
	var buf bytes.Buffer
	limit := f.maxBytes()
	for {
		size, end, err := f.readChunkHeader()
		if err != nil {
			return nil, err
		}
		if end {
			if buf.Len() == 0 {
				return nil, fmt.Errorf("%w: end-of-chunks with no preceding chunk", errMalformedFrame)
			}
			return buf.Bytes(), nil
		}
		if int64(buf.Len())+size > limit {
			return nil, fmt.Errorf("%w (%d bytes)", errReplyTooLarge, limit)
		}
		// CopyN grows the buffer only as data actually arrives, so a
		// header that lies about its size cannot pre-allocate memory.
		if _, err := io.CopyN(&buf, f.r, size); err != nil {
			if errors.Is(err, io.EOF) {
				return nil, fmt.Errorf("%w: stream ended inside a %d-byte chunk", errMalformedFrame, size)
			}
			return nil, fmt.Errorf("NETCONF read failed: %w", err)
		}
	}
}

// readChunkHeader parses "\n#<size>\n" or the end-of-chunks marker
// "\n##\n". It returns end=true for the marker.
func (f *framer) readChunkHeader() (size int64, end bool, err error) {
	if err := f.expect('\n'); err != nil {
		return 0, false, err
	}
	if err := f.expect('#'); err != nil {
		return 0, false, err
	}
	b, err := f.readByte()
	if err != nil {
		return 0, false, err
	}
	if b == '#' {
		if err := f.expect('\n'); err != nil {
			return 0, false, err
		}
		return 0, true, nil
	}

	// chunk-size = [1-9] *DIGIT, at most maxChunkSize.
	if b < '1' || b > '9' {
		return 0, false, fmt.Errorf("%w: chunk size must start with a digit 1-9, got %q", errMalformedFrame, b)
	}
	digits := []byte{b}
	for {
		b, err := f.readByte()
		if err != nil {
			return 0, false, err
		}
		if b == '\n' {
			break
		}
		if b < '0' || b > '9' {
			return 0, false, fmt.Errorf("%w: invalid byte %q in chunk size", errMalformedFrame, b)
		}
		digits = append(digits, b)
		if len(digits) > maxChunkSizeDigits {
			return 0, false, fmt.Errorf("%w: chunk size has more than %d digits", errMalformedFrame, maxChunkSizeDigits)
		}
	}
	n, err := strconv.ParseInt(string(digits), 10, 64)
	if err != nil || n > maxChunkSize {
		return 0, false, fmt.Errorf("%w: chunk size %s out of range", errMalformedFrame, digits)
	}
	return n, false, nil
}

// expect reads one byte and fails unless it equals want.
func (f *framer) expect(want byte) error {
	b, err := f.readByte()
	if err != nil {
		return err
	}
	if b != want {
		return fmt.Errorf("%w: expected %q in chunk framing, got %q", errMalformedFrame, want, b)
	}
	return nil
}

// readByte reads one byte, reporting end of stream as a malformed frame
// because chunked framing never ends between messages mid-header.
func (f *framer) readByte() (byte, error) {
	b, err := f.r.ReadByte()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return 0, fmt.Errorf("%w: stream ended inside a chunk header", errMalformedFrame)
		}
		return 0, fmt.Errorf("NETCONF read failed: %w", err)
	}
	return b, nil
}

// negotiateFraming decides the framing for every message after <hello>
// from the server's advertised capabilities, given that the client
// advertises both base:1.0 and base:1.1. Capability URIs are compared
// after trimming surrounding whitespace, which some servers emit inside
// <capability> elements.
func negotiateFraming(serverCaps []string) (chunked bool, err error) {
	var has10, has11 bool
	for _, c := range serverCaps {
		switch strings.TrimSpace(c) {
		case capBase10:
			has10 = true
		case capBase11:
			has11 = true
		}
	}
	switch {
	case has11:
		return true, nil
	case has10:
		return false, nil
	default:
		return false, errUnsupportedBase
	}
}

// maxErrorMessageRunes bounds how much of a device error-message is
// kept.
const maxErrorMessageRunes = 200

// rpcReplyEnvelope is the decoded <rpc-reply> wrapper. Child elements
// match only in the NETCONF base namespace, whatever prefix the device
// uses for it.
type rpcReplyEnvelope struct {
	XMLName   xml.Name
	MessageID string     `xml:"message-id,attr"`
	Errors    []rpcError `xml:"urn:ietf:params:xml:ns:netconf:base:1.0 rpc-error"`
	Data      *struct {
		Inner []byte `xml:",innerxml"`
	} `xml:"urn:ietf:params:xml:ns:netconf:base:1.0 data"`
}

// rpcError is one NETCONF <rpc-error> (RFC 6241 section 4.3). It
// implements error with a sanitized message and errors.Is reports its
// class: errRPCUnsupported, errRPCAccessDenied, or errRPCFailed.
// error-path and error-info are deliberately not decoded: they can echo
// configuration values.
type rpcError struct {
	Type     string `xml:"urn:ietf:params:xml:ns:netconf:base:1.0 error-type"`
	Tag      string `xml:"urn:ietf:params:xml:ns:netconf:base:1.0 error-tag"`
	Severity string `xml:"urn:ietf:params:xml:ns:netconf:base:1.0 error-severity"`
	AppTag   string `xml:"urn:ietf:params:xml:ns:netconf:base:1.0 error-app-tag"`
	Message  string `xml:"urn:ietf:params:xml:ns:netconf:base:1.0 error-message"`
}

func (e *rpcError) Error() string {
	msg := fmt.Sprintf("NETCONF rpc-error [%s/%s]", sanitizeErrorText(e.Type, 32), sanitizeErrorText(e.Tag, 64))
	if m := sanitizeErrorText(e.Message, maxErrorMessageRunes); m != "" {
		msg += ": " + m
	}
	return msg
}

// Is reports whether target is this error's class sentinel.
func (e *rpcError) Is(target error) bool {
	return target == e.class()
}

// class maps error-tag (RFC 6241 Appendix A) to a sentinel.
func (e *rpcError) class() error {
	switch strings.TrimSpace(e.Tag) {
	case "operation-not-supported", "unknown-element", "unknown-namespace", "unknown-attribute":
		return errRPCUnsupported
	case "access-denied":
		return errRPCAccessDenied
	default:
		return errRPCFailed
	}
}

// isWarning reports whether the device marked this error as a warning,
// which does not make the operation fail.
func (e *rpcError) isWarning() bool {
	return strings.TrimSpace(e.Severity) == "warning"
}

// parseRPCReply validates one <rpc-reply> against the message-id of its
// request and returns the verbatim content of its <data> element.
// Warnings are returned separately for the caller to log; any rpc-error
// of severity error fails the reply with the first such error.
// The <data> content is returned byte for byte; YANG payloads declare
// their own namespaces on their top-level elements, so it
// decodes standalone.
func parseRPCReply(reply []byte, wantID string) (data []byte, warnings []rpcError, err error) {
	var env rpcReplyEnvelope
	if err := xml.Unmarshal(reply, &env); err != nil {
		return nil, nil, fmt.Errorf("%w: %v", errInvalidReply, err)
	}
	if env.XMLName.Space != ncBaseNS || env.XMLName.Local != "rpc-reply" {
		return nil, nil, fmt.Errorf("%w: root element is <%s> in namespace %q, want <rpc-reply> in %q",
			errInvalidReply, sanitizeErrorText(env.XMLName.Local, 64), sanitizeErrorText(env.XMLName.Space, 128), ncBaseNS)
	}
	if env.MessageID != wantID {
		return nil, nil, fmt.Errorf("%w: got %q, want %q", errMessageIDMismatch, sanitizeErrorText(env.MessageID, 32), wantID)
	}

	var failures []rpcError
	for _, e := range env.Errors {
		if e.isWarning() {
			warnings = append(warnings, e)
		} else {
			failures = append(failures, e)
		}
	}
	if len(failures) > 0 {
		first := failures[0]
		if len(failures) > 1 {
			return nil, warnings, fmt.Errorf("%w (and %d more)", &first, len(failures)-1)
		}
		return nil, warnings, &first
	}

	if env.Data == nil {
		return nil, warnings, fmt.Errorf("%w: no <data> element in the reply", errInvalidReply)
	}
	if env.Data.Inner == nil {
		return []byte{}, warnings, nil
	}
	return env.Data.Inner, warnings, nil
}

// sanitizeErrorText makes device-supplied text safe to embed in an
// error or log line: control characters are dropped, whitespace runs
// collapse to one space and the result is cut to maxRunes.
func sanitizeErrorText(s string, maxRunes int) string {
	var b strings.Builder
	space := false
	n := 0
	for _, r := range strings.TrimSpace(s) {
		if unicode.IsSpace(r) {
			space = true
			continue
		}
		if unicode.IsControl(r) {
			continue
		}
		if space && b.Len() > 0 {
			if n == maxRunes {
				break
			}
			b.WriteByte(' ')
			n++
		}
		space = false
		if n == maxRunes {
			b.WriteString("...")
			break
		}
		b.WriteRune(r)
		n++
	}
	return b.String()
}
