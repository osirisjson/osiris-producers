// identity.go - Deterministic ID generation for OSIRIS connections and groups.
// Implements canonical key serialization, SHA-256 hash truncation, collision-resistant
// ID registry and human-readable hint derivation per [OSIRIS-ADG-PR-SDK-1.0 chapter 2].
//
// For an introduction to OSIRIS JSON Producer Development Guidelines see:
// "[OSIRIS-PRODUCER-GUIDELINES]."
//
// [OSIRIS-PRODUCER-GUIDELINES]: https://osirisjson.org/en/docs/getting-started/osiris-producer-guidelines
// [OSIRIS-ADG-PR-SDK-1.0]: https://github.com/osirisjson/osiris-producers/blob/main/docs/guidelines/v1.0/OSIRIS-PRODUCER-SDK.md#2-developer-utilities

package sdk

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// ErrIDCollision is returned when hash collision cannot be resolved even at 32 characters.
var ErrIDCollision = errors.New("unresolvable ID collision at maximum hash length (32)")

var hintCleanRe = regexp.MustCompile(`[^a-z0-9]+`)

// Hash16 computes the first 16 characters of the lowercase hex SHA-256
// of the canonical key.
func Hash16(canonicalKey string) string {
	return HashN(canonicalKey, 16)
}

// HashN computes the first n characters of the lowercase hex SHA-256
// of the canonical key. n must be between 1 and 64.
func HashN(canonicalKey string, n int) string {
	if n < 1 {
		n = 1
	}
	if n > 64 {
		n = 64
	}
	h := sha256.Sum256([]byte(canonicalKey))
	full := hex.EncodeToString(h[:])
	return full[:n]
}

// EncodeComponent percent-encodes the characters that are reserved in
// canonical key serialization: % | , =
func EncodeComponent(s string) string {
	// Order matters: encode % first to avoid double-encoding.
	s = strings.ReplaceAll(s, "%", "%25")
	s = strings.ReplaceAll(s, "|", "%7C")
	s = strings.ReplaceAll(s, ",", "%2C")
	s = strings.ReplaceAll(s, "=", "%3D")
	return s
}

/*
DeriveHint implements the normative hint derivation rule:
 1. Take substring after the rightmost "::" or "/" (whichever occurs later)
 2. Lowercase
 3. Replace any sequence of non-[a-z0-9] with "-"
 4. Trim leading/trailing "-"
 5. Truncate to max 24 chars

If the result is empty, returns the first 8 characters of the hash.
*/
func DeriveHint(id, hash string) string {
	s := id
	colonIdx := strings.LastIndex(s, "::")
	slashIdx := strings.LastIndex(s, "/")

	// Take substring after whichever delimiter occurs rightmost.
	if colonIdx >= 0 && colonIdx > slashIdx {
		s = s[colonIdx+2:]
	} else if slashIdx >= 0 {
		s = s[slashIdx+1:]
	}
	s = strings.ToLower(s)
	s = hintCleanRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 24 {
		s = s[:24]
	}
	if s == "" && len(hash) >= 8 {
		s = hash[:8]
	}
	return s
}

// ConnectionIDInput carries the stable parts needed to derive a connection ID.
type ConnectionIDInput struct {
	Type       string
	Direction  string
	Source     string
	Target     string
	Qualifiers map[string]string
}

// ConnectionCanonicalKey returns the canonical key for a connection.
// Format: v1|{type}|{direction}|{sourceId}|{targetId}|{qualifiers}
// For bidirectional connections, endpoints are sorted lexicographically.
func ConnectionCanonicalKey(in ConnectionIDInput) string {
	dir := in.Direction
	if dir == "" {
		dir = "bidirectional"
	}

	source := in.Source
	target := in.Target
	if dir == "bidirectional" && source > target {
		source, target = target, source
	}

	var b strings.Builder
	b.WriteString("v1|")
	b.WriteString(EncodeComponent(in.Type))
	b.WriteString("|")
	b.WriteString(EncodeComponent(dir))
	b.WriteString("|")
	b.WriteString(EncodeComponent(source))
	b.WriteString("|")
	b.WriteString(EncodeComponent(target))

	if len(in.Qualifiers) > 0 {
		b.WriteString("|")
		b.WriteString(serializeQualifiers(in.Qualifiers))
	}

	return b.String()
}

// BuildConnectionID formats the final connection ID from a canonical key and hash length.
// Output format: conn-{type}-{hintA}-to-{hintB}-{hash}.
func BuildConnectionID(canonicalKey string, hashLen int) string {
	hash := HashN(canonicalKey, hashLen)

	// Parse type, source, target from the canonical key.
	parts := strings.SplitN(canonicalKey, "|", 6)
	if len(parts) < 5 {
		return "conn-unknown-" + hash
	}
	connType := decodeComponent(parts[1])
	source := decodeComponent(parts[3])
	target := decodeComponent(parts[4])

	hintA := DeriveHint(source, hash)
	hintB := DeriveHint(target, hash)

	return fmt.Sprintf("conn-%s-%s-to-%s-%s", connType, hintA, hintB, hash)
}

// GroupIDInput carries the stable boundary needed to derive a group ID.
type GroupIDInput struct {
	Type          string
	BoundaryToken string
	ScopeFields   map[string]string
}

// GroupCanonicalKey returns the canonical key for a group.
// Format: v1|{type}|boundary={boundaryToken}|{scopePairs}
func GroupCanonicalKey(in GroupIDInput) string {
	var b strings.Builder
	b.WriteString("v1|")
	b.WriteString(EncodeComponent(in.Type))
	b.WriteString("|boundary=")
	b.WriteString(EncodeComponent(in.BoundaryToken))

	if len(in.ScopeFields) > 0 {
		b.WriteString("|")
		b.WriteString(serializeScopeFields(in.ScopeFields))
	}

	return b.String()
}

// BuildGroupID formats the final group ID from a canonical key, boundary token and hash length.
// Output format: group-{type}-{boundaryHint}-{hash}
func BuildGroupID(in GroupIDInput, hashLen int) string {
	canonicalKey := GroupCanonicalKey(in)
	hash := HashN(canonicalKey, hashLen)
	hint := DeriveHint(in.BoundaryToken, hash)

	if hint == "" {
		return fmt.Sprintf("group-%s-%s", in.Type, hash)
	}
	return fmt.Sprintf("group-%s-%s-%s", in.Type, hint, hash)
}

// GroupID returns the deterministic group ID using the default Hash16 length.
func GroupID(in GroupIDInput) string {
	return BuildGroupID(in, 16)
}

// serializeQualifiers produces a comma-joined sorted list of k=v pairs.
// Keys are sorted by their encoded form.
func serializeQualifiers(qualifiers map[string]string) string {
	type kv struct {
		encodedKey string
		pair       string
	}
	pairs := make([]kv, 0, len(qualifiers))
	for k, v := range qualifiers {
		ek := EncodeComponent(k)
		pairs = append(pairs, kv{
			encodedKey: ek,
			pair:       ek + "=" + EncodeComponent(v),
		})
	}
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].encodedKey < pairs[j].encodedKey
	})
	parts := make([]string, len(pairs))
	for i, p := range pairs {
		parts[i] = p.pair
	}
	return strings.Join(parts, ",")
}

// serializeScopeFields produces a pipe-joined sorted list of k=v pairs.
// Keys are sorted by their encoded form. Empty values are omitted.
func serializeScopeFields(fields map[string]string) string {
	type kv struct {
		encodedKey string
		pair       string
	}
	pairs := make([]kv, 0, len(fields))
	for k, v := range fields {
		if v == "" {
			continue
		}
		ek := EncodeComponent(k)
		pairs = append(pairs, kv{
			encodedKey: ek,
			pair:       ek + "=" + EncodeComponent(v),
		})
	}
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].encodedKey < pairs[j].encodedKey
	})
	parts := make([]string, len(pairs))
	for i, p := range pairs {
		parts[i] = p.pair
	}
	return strings.Join(parts, "|")
}

// decodeComponent reverses EncodeComponent for parsing canonical keys.
func decodeComponent(s string) string {
	s = strings.ReplaceAll(s, "%7C", "|")
	s = strings.ReplaceAll(s, "%2C", ",")
	s = strings.ReplaceAll(s, "%3D", "=")
	s = strings.ReplaceAll(s, "%25", "%")
	return s
}

// IDRegistry implements the two-phase ID resolution strategy.
// Producers register canonical keys during collection, then resolve
// all final IDs at Build() time when the full set of keys is known.
type IDRegistry struct {
	kinds map[string][]string // kind -> []canonicalKey.
}

// NewIDRegistry creates a new empty ID registry.
func NewIDRegistry() *IDRegistry {
	return &IDRegistry{
		kinds: make(map[string][]string),
	}
}

// RegisterKey records a canonical key for later deterministic resolution.
func (r *IDRegistry) RegisterKey(kind string, canonicalKey string) {
	r.kinds[kind] = append(r.kinds[kind], canonicalKey)
}

// ResolveAll returns a stable mapping canonicalKey -> resolvedId for the given kind.
// It uses the upgrade rules: Hash16 -> Hash24 -> Hash32 -> error.
// The buildID callback formats the final ID string from a canonical key and hash length.
func (r *IDRegistry) ResolveAll(kind string, buildID func(canonicalKey string, hashLen int) string) (map[string]string, error) {
	keys := r.kinds[kind]
	if len(keys) == 0 {
		return map[string]string{}, nil
	}

	// Start with Hash16 for all keys.
	result := make(map[string]string, len(keys))
	for _, hashLen := range []int{16, 24, 32} {
		// Build candidate IDs at the current hash length.
		candidates := make(map[string]string, len(keys))
		for _, ck := range keys {
			candidates[ck] = buildID(ck, hashLen)
		}

		// Detect collisions: group canonical keys by their generated ID.
		idToKeys := make(map[string][]string, len(candidates))
		for ck, id := range candidates {
			idToKeys[id] = append(idToKeys[id], ck)
		}

		// Assign non-colliding IDs, track which keys still collide.
		var colliding []string
		for id, cks := range idToKeys {
			if len(cks) == 1 {
				result[cks[0]] = id
			} else {
				colliding = append(colliding, cks...)
			}
		}

		if len(colliding) == 0 {
			return result, nil
		}

		// If we're at max hash length and still have collisions, fail.
		if hashLen == 32 {
			return nil, fmt.Errorf("%w: kind=%q, colliding keys: %v", ErrIDCollision, kind, colliding)
		}

		// Upgrade only the colliding keys to the next hash length.
		// Remove them from result so they get re-resolved.
		keys = colliding
	}

	return result, nil
}
