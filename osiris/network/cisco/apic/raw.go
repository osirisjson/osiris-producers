// raw.go - --include-raw-body support for the Cisco ACI/APIC
// OSIRIS JSON Producer.
//
// With --include-raw-body (accepted only together with --purpose audit)
// the full attribute set of every collected APIC object is attached to
// the emitted entity that owns it, as a fallback for fields this
// producer does not model into OSIRIS JSON yet:
//
//   - the owner is the resource, or else the group, whose native DN is
//     the longest DN prefix of the object (the object itself when it
//     was emitted; a node for its topSystem, firmware, faults and
//     LLDP/CDP adjacencies; an EPG anchor for its relations); a
//     resource wins over a group with the same DN;
//   - objects are attached under extensions["osiris.cisco"].raw, keyed
//     by APIC class, each class a list ordered by DN; objects with no
//     owner (for example fabricLink, which sits under the pod) go to
//     the controller resources under raw_unowned in the same shape;
//   - every attribute whose name sdk.IsSensitiveKey flags or is an APIC
//     credential attribute (rawSensitiveAttrs), and every value
//     sdk.ScanValue flags, is replaced with sdk.RedactedValue;
//   - one object is capped at maxRawObjectBytes and all objects
//     together at maxRawTotalBytes, in class then DN order; every
//     withheld object is counted in raw_omitted on the controller
//     resources by class and reason, so fallback never fails silently.
//
// The document secret scan still runs over the result.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package apic

import (
	"encoding/json"
	"sort"
	"strings"

	"go.osirisjson.org/producers/pkg/sdk"
)

const (
	// maxRawObjectBytes caps the JSON size of one attached object.
	maxRawObjectBytes = 64 << 10
	// maxRawTotalBytes caps the JSON size of all attached objects.
	maxRawTotalBytes = 32 << 20
)

// rawSensitiveAttrs are APIC attribute names (lower case) that carry
// credential material but match no shared sensitive-key pattern, such
// as the aaaUser and SNMP/RADIUS/TACACS+ provider "pwd" and "key".
var rawSensitiveAttrs = map[string]bool{
	"pwd":        true,
	"passwd":     true,
	"key":        true,
	"keyring":    true,
	"psk":        true,
	"community":  true,
	"passphrase": true,
}

// rawOwner locates the entity a raw object is attached to.
type rawOwner struct {
	ext *map[string]any // the owner's Extensions field
}

// redactRawAttrs returns a copy of attrs with every attribute whose
// name is sensitive or whose string value matches a secret pattern
// replaced with sdk.RedactedValue. Non-string values are kept only when
// their key is not sensitive.
func redactRawAttrs(attrs map[string]any) map[string]any {
	out := make(map[string]any, len(attrs))
	for k, v := range attrs {
		switch {
		case sdk.IsSensitiveKey(k) || rawSensitiveAttrs[strings.ToLower(k)]:
			out[k] = sdk.RedactedValue
		default:
			if s, ok := v.(string); ok && sdk.ScanValue(s) != "" {
				out[k] = sdk.RedactedValue
				continue
			}
			out[k] = v
		}
	}
	return out
}

// attachRawBodies attaches every collected object in raw to its owner
// among resources and groups (see the file comment). It mutates the
// Extensions of resources and groups in place.
func attachRawBodies(raw map[string][]map[string]any, resources []sdk.Resource, groups []sdk.Group, groupDNs map[string]string) {
	owners := map[string]rawOwner{}
	for gid, dn := range groupDNs {
		for i := range groups {
			if groups[i].ID == gid {
				owners[dn] = rawOwner{ext: &groups[i].Extensions}
			}
		}
	}
	var controllers []*map[string]any
	for i := range resources {
		r := &resources[i]
		if strings.HasPrefix(r.ID, providerName+"::") && r.Provider.NativeID != "" {
			owners[r.Provider.NativeID] = rawOwner{ext: &r.Extensions}
		}
		if role, _ := r.Properties["role"].(string); role == "controller" {
			controllers = append(controllers, &r.Extensions)
		}
	}
	if len(controllers) == 0 && len(resources) > 0 {
		controllers = append(controllers, &resources[0].Extensions)
	}

	type key struct{ class, reason string }
	omitted := map[key]int{}
	unowned := map[string][]map[string]any{}
	total := 0

	classes := make([]string, 0, len(raw))
	for c := range raw {
		classes = append(classes, c)
	}
	sort.Strings(classes)
	for _, class := range classes {
		objs := append([]map[string]any(nil), raw[class]...)
		sort.SliceStable(objs, func(i, j int) bool { return str(objs[i], "dn") < str(objs[j], "dn") })
		for _, o := range objs {
			red := redactRawAttrs(o)
			b, err := json.Marshal(red)
			if err != nil {
				omitted[key{class, "unencodable"}]++
				continue
			}
			if len(b) > maxRawObjectBytes {
				omitted[key{class, "object-too-large"}]++
				continue
			}
			if total+len(b) > maxRawTotalBytes {
				omitted[key{class, "total-cap"}]++
				continue
			}
			total += len(b)

			if ow, ok := ownerOf(str(o, "dn"), owners); ok {
				ensureCiscoExtension(ow.ext)
				cisco := (*ow.ext)[extensionNamespace].(map[string]any)
				byClass, _ := cisco["raw"].(map[string]any)
				if byClass == nil {
					byClass = map[string]any{}
					cisco["raw"] = byClass
				}
				list, _ := byClass[class].([]map[string]any)
				byClass[class] = append(list, red)
				continue
			}
			unowned[class] = append(unowned[class], red)
		}
	}

	var omittedList []map[string]any
	keys := make([]key, 0, len(omitted))
	for k := range omitted {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].class != keys[j].class {
			return keys[i].class < keys[j].class
		}
		return keys[i].reason < keys[j].reason
	})
	for _, k := range keys {
		omittedList = append(omittedList, map[string]any{"class": k.class, "reason": k.reason, "count": omitted[k]})
	}

	for _, ext := range controllers {
		ensureCiscoExtension(ext)
		cisco := (*ext)[extensionNamespace].(map[string]any)
		if len(unowned) > 0 {
			u := make(map[string]any, len(unowned))
			for c, l := range unowned {
				u[c] = l
			}
			cisco["raw_unowned"] = u
		}
		if len(omittedList) > 0 {
			cisco["raw_omitted"] = omittedList
		}
	}
}

// ownerOf returns the owner whose DN is the longest prefix of dn,
// trying dn itself first and then each shorter "/"-delimited prefix.
func ownerOf(dn string, owners map[string]rawOwner) (rawOwner, bool) {
	for p := dn; p != ""; {
		if ow, ok := owners[p]; ok {
			return ow, true
		}
		i := strings.LastIndex(p, "/")
		if i < 0 {
			break
		}
		p = p[:i]
	}
	return rawOwner{}, false
}
