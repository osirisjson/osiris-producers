// raw.go - --include-raw-body support for the Cisco IOS-XE OSIRIS JSON
// Producer.
//
// With --include-raw-body (accepted only together with --purpose audit)
// the reply of each operational discovery operation is attached to the
// device resource under extensions["osiris.cisco"].raw_replies, keyed
// by operation, as a lossless fallback for fields this producer does
// not model yet into OSIRIS JSON. The fallback is deliberately narrow:
//
//   - configuration replies (every operation that reads
//     Cisco-IOS-XE-native) are never attached, because running
//     configuration carries secrets material;
//   - every attached reply is redacted: the content of any element
//     whose name denotes secrets material and any text or attribute
//     value matching a known secret pattern, is replaced with
//     "REDACTED"; a reply that is not well-formed XML cannot be
//     redacted and is dropped;
//   - one reply is capped at maxRawReplyBytes and all replies together
//     at maxRawTotalBytes.
//
// Every operation whose reply was withheld is listed in raw_omitted
// with the reason, so the fallback never fails silently.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package iosxe

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strings"

	"go.osirisjson.org/producers/pkg/sdk"
)

const (
	// maxRawReplyBytes caps one attached reply after redaction.
	maxRawReplyBytes = 256 << 10
	// maxRawTotalBytes caps all attached replies together.
	maxRawTotalBytes = 2 << 20
	// rawRedacted replaces redacted content.
	rawRedacted = "REDACTED"
)

// Reasons a reply is withheld from raw_replies.
const (
	rawOmitConfiguration = "configuration"
	rawOmitUnredactable  = "unredactable"
	rawOmitTooLarge      = "too-large"
	rawOmitTotalLimit    = "total-limit"
)

// rawSensitiveElements are YANG leaf and container names, beyond the
// SDK's sensitive-key patterns, that hold secrets material in IOS-XE
// models: shared keys, SNMP community strings, NTP and routing-protocol
// digests, IKE pre-shared keys and password hashes.
var rawSensitiveElements = map[string]bool{
	"key": true, "community": true, "community-or-user": true,
	"md5": true, "md5-cfg": true, "sha1": true, "sha2": true,
	"hmac-sha1": true, "hmac-sha2-256": true, "cmac-aes-128": true,
	"pre-shared-key": true, "hash": true, "pac": true,
}

// sensitiveElement reports whether an element's content must be
// redacted.
func sensitiveElement(local string) bool {
	return rawSensitiveElements[local] || sdk.IsSensitiveKey(strings.ReplaceAll(local, "-", "_"))
}

// rawCollector accumulates attached and withheld replies. A nil
// *rawCollector collects nothing.
type rawCollector struct {
	replies map[string]string
	omitted []map[string]any
	total   int
}

// add attaches q's reply, or records why it is withheld.
func (c *rawCollector) add(q query, data []byte) {
	if c == nil {
		return
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return
	}
	if q.config {
		c.omit(q.op, rawOmitConfiguration)
		return
	}
	red, ok := redactXML(data)
	switch {
	case !ok:
		c.omit(q.op, rawOmitUnredactable)
	case len(red) > maxRawReplyBytes:
		c.omit(q.op, rawOmitTooLarge)
	case c.total+len(red) > maxRawTotalBytes:
		c.omit(q.op, rawOmitTotalLimit)
	default:
		if c.replies == nil {
			c.replies = map[string]string{}
		}
		c.replies[q.op] = string(red)
		c.total += len(red)
	}
}

func (c *rawCollector) omit(op, reason string) {
	c.omitted = append(c.omitted, map[string]any{"operation": op, "reason": reason})
}

// apply sets raw_replies and raw_omitted on the cisco extension map.
func (c *rawCollector) apply(cisco map[string]any) {
	if c == nil {
		return
	}
	if len(c.replies) > 0 {
		cisco["raw_replies"] = c.replies
	}
	if len(c.omitted) > 0 {
		cisco["raw_omitted"] = c.omitted
	}
}

// qname renders a raw XML name with its prefix.
func qname(n xml.Name) string {
	if n.Space != "" {
		return n.Space + ":" + n.Local
	}
	return n.Local
}

// redactXML re-serializes an XML payload with secrets content
// replaced by rawRedacted: the whole content of every sensitive
// element, every text node and attribute value matching an SDK secret
// pattern and every comment, processing instruction and directive
// (dropped). It returns false when the payload is not well-formed, so
// an unparseable reply is never attached.
func redactXML(data []byte) ([]byte, bool) {
	d := xml.NewDecoder(bytes.NewReader(data))
	var b bytes.Buffer
	var stack []string
	skip := 0 // >0 while inside a redacted element, counting its depth.
	for {
		tok, err := d.RawToken()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, false
		}
		switch t := tok.(type) {
		case xml.StartElement:
			name := qname(t.Name)
			stack = append(stack, name)
			if skip > 0 {
				skip++
				continue
			}
			b.WriteString("<" + name)
			for _, a := range t.Attr {
				v := a.Value
				if sdk.ScanValue(v) != "" || sensitiveElement(a.Name.Local) && a.Name.Space != "xmlns" && a.Name.Local != "xmlns" {
					v = rawRedacted
				}
				b.WriteString(" " + qname(a.Name) + `="`)
				xml.EscapeText(&b, []byte(v))
				b.WriteString(`"`)
			}
			b.WriteString(">")
			if sensitiveElement(t.Name.Local) {
				b.WriteString(rawRedacted)
				skip = 1
			}
		case xml.EndElement:
			name := qname(t.Name)
			if len(stack) == 0 || stack[len(stack)-1] != name {
				return nil, false
			}
			stack = stack[:len(stack)-1]
			if skip > 0 {
				skip--
				if skip > 0 {
					continue
				}
			}
			b.WriteString("</" + name + ">")
		case xml.CharData:
			if skip > 0 {
				continue
			}
			s := []byte(t)
			if sdk.ScanValue(string(s)) != "" {
				s = []byte(rawRedacted)
			}
			xml.EscapeText(&b, s)
		}
	}
	if len(stack) != 0 {
		return nil, false
	}
	return b.Bytes(), true
}
