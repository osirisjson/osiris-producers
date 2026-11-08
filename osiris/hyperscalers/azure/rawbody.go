// rawbody.go - Lossless ARM body capture for --include-raw-body in the
// OSIRIS JSON Microsoft Azure producer.
//
// The az CLI list commands already return the full ARM object for most
// resource types, but the collectors decode that output into typed
// structs that keep only the fields the producer models.
// With --include-raw-body under --purpose audit, the client
// additionally keeps each returned ARM object as received (see
// [rawStore.capture]), keyed by canonical ARM ID, and [applyRawBodies]
// replaces the struct-derived body under
// extensions["osiris.azure.arm"].body with it. No extra az calls made.
//
// The captured JSON is redacted before it is attached
// ([redactRawBody]): the body is stored as a JSON string so the
// document secret scanner does not recurse into it, so this file is the
// only place its content is checked.
// A body that still matches a secret value pattern after redaction is
// discarded and the struct-derived body is kept, so a capture can never
// make the fail-closed document build abort.
//
// extensions["osiris.azure.arm"].source records which body was
// attached: "arm" (as returned by Azure, redacted) or "struct"
// (decoded fields only). When a capture existed but was discarded,
// raw_rejected names the secret pattern rule that matched (never
// the matched text).

package azure

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync"

	"go.osirisjson.org/producers/pkg/sdk"
)

// redactedValue replaces every redacted string in a captured body.
const redactedValue = "[REDACTED]"

// rawStore holds captured ARM objects keyed by [armKey]. It is safe for
// concurrent use; collectors run in parallel goroutines.
// A nil *rawStore captures nothing.
type rawStore struct {
	mu sync.Mutex
	m  map[string]rawEntry
}

type rawEntry struct {
	body   json.RawMessage
	nested bool // found inside another ARM object rather than as a list item
}

func newRawStore() *rawStore {
	return &rawStore{m: make(map[string]rawEntry)}
}

// capture indexes the ARM objects in one az command output: every
// element of a top-level array (or a single top-level object), plus
// every nested object that carries an ARM resource ID and at least one
// field besides it (so a bare {"id": ...} reference never stands in
// for a resource body). A top-level item always wins over a nested
// copy of the same resource.
func (s *rawStore) capture(out []byte) {
	if s == nil {
		return
	}
	out = bytes.TrimSpace(out)
	var items []json.RawMessage
	if len(out) > 0 && out[0] == '[' {
		if err := json.Unmarshal(out, &items); err != nil {
			return
		}
	} else {
		items = []json.RawMessage{out}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, it := range items {
		s.index(it, false)
	}
}

// index records obj when it is an ARM resource body and recurses
// into its fields. Callers hold s.mu.
func (s *rawStore) index(obj json.RawMessage, nested bool) {
	obj = bytes.TrimSpace(obj)
	if len(obj) == 0 {
		return
	}
	switch obj[0] {
	case '[':
		var arr []json.RawMessage
		if json.Unmarshal(obj, &arr) == nil {
			for _, a := range arr {
				s.index(a, true)
			}
		}
		return
	case '{':
	default:
		return
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(obj, &fields) != nil {
		return
	}
	var id string
	if raw, ok := fields["id"]; ok {
		_ = json.Unmarshal(raw, &id)
	}
	if strings.HasPrefix(strings.ToLower(id), "/subscriptions/") && len(fields) > 1 {
		key := armKey(id)
		if prev, ok := s.m[key]; !ok || (prev.nested && !nested) {
			s.m[key] = rawEntry{body: append(json.RawMessage(nil), obj...), nested: nested}
		}
	}
	for k, v := range fields {
		if k != "id" {
			s.index(v, true)
		}
	}
}

// replace stores the top-level objects of out as the bodies of their
// ARM IDs, replacing earlier captures: out is a fuller view of the same
// resources (e.g. az ... show after az ... list).
func (s *rawStore) replace(out []byte) {
	if s == nil {
		return
	}
	out = bytes.TrimSpace(out)
	var items []json.RawMessage
	if len(out) > 0 && out[0] == '[' {
		if json.Unmarshal(out, &items) != nil {
			return
		}
	} else {
		items = []json.RawMessage{out}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, it := range items {
		var head struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(it, &head) == nil && head.ID != "" {
			s.m[armKey(head.ID)] = rawEntry{body: append(json.RawMessage(nil), it...)}
		}
	}
}

// setProperties fills the "properties" member of the captured body of
// armID when it is missing or null (list commands that return no
// resource properties), keeping every other member as captured.
func (s *rawStore) setProperties(armID string, props json.RawMessage) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := armKey(armID)
	e, ok := s.m[key]
	if !ok {
		return
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(e.body, &fields) != nil {
		return
	}
	if cur, has := fields["properties"]; has && string(bytes.TrimSpace(cur)) != "null" {
		return
	}
	fields["properties"] = props
	if body, err := json.Marshal(fields); err == nil {
		e.body = body
		s.m[key] = e
	}
}

// get returns the captured body for an ARM ID.
func (s *rawStore) get(armID string) (json.RawMessage, bool) {
	if s == nil {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.m[armKey(armID)]
	return e.body, ok
}

// azureSensitiveKeyPatterns extends [sdk.SensitiveKeyPatterns] with the
// camelCase spellings ARM uses, and with keys whose value is a URL that
// carries a secret in its path or query (Logic App trigger URLs, action
// group webhooks, Function trigger URLs). Keys are compared after
// lower-casing and removing '_' and '-', so "accessKey", "access_key"
// and "access-key" all match "accesskey".
var azureSensitiveKeyPatterns = []string{
	"accesskey",
	"accountkey",
	"apikey",
	"callbackurl",
	"connectionstring",
	"customdata",
	"instrumentationkey",
	"primarykey",
	"privatekey",
	"secondarykey",
	"servicekey",
	"serviceuri",
	"sharedkey",
	"signingkey",
	"storagekey",
	"triggerurl",
	"userdata",
	"webhookurl",
}

// valueHolderKeys name the fields that carry the payload of a sensitive
// object (e.g. {"type": "Raw", "value": "<token>"} under
// "authentication"). Below a sensitive key only these, and keys that
// are sensitive themselves, are redacted; structural fields such as
// "type", "method" or "audience" are kept and still checked against
// the value patterns.
var valueHolderKeys = map[string]bool{
	"body":         true,
	"content":      true,
	"data":         true,
	"defaultvalue": true,
	"key":          true,
	"text":         true,
	"value":        true,
}

// azureSensitiveValuePatterns extend the SDK value patterns
// ([sdk.ScanValue]) with Azure credential formats embedded in strings:
// SAS signatures and Function keys in URL queries, storage and Service
// Bus connection strings, and credentials in form-encoded HTTP bodies.
var azureSensitiveValuePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)[?&](sig|code)=[^&\s]+`),
	regexp.MustCompile(`(?i)(AccountKey|SharedAccessKey|SharedAccessSignature|SharedSecretValue)=`),
	regexp.MustCompile(`(?i)(^|[?&\s;])(client_secret|password|passwd|api[_-]?key|access_token|refresh_token)=[^&\s]+`),
	regexp.MustCompile(`(?i)InstrumentationKey=[0-9a-f]{8}-?[0-9a-f]{4}`),
}

// isSensitiveARMKey reports whether a JSON key in an ARM body names a
// value that must be redacted.
func isSensitiveARMKey(key string) bool {
	if sdk.IsSensitiveKey(key) {
		return true
	}
	norm := strings.NewReplacer("_", "", "-", "").Replace(strings.ToLower(key))
	for _, p := range azureSensitiveKeyPatterns {
		if strings.Contains(norm, p) {
			return true
		}
	}
	return false
}

// isSensitiveARMValue reports whether a string matches a secret value
// pattern of the SDK or of Azure, either as text or in its JSON-encoded
// form. The encoded form matters because the document secret scanner
// sees the body as encoded JSON: there a line break inside a multi-line
// string (an APIM policy, a Logic App expression) is the two characters
// `\n`, which whitespace-excluding patterns do not stop at, so lines
// that are clean on their own can match together.
func isSensitiveARMValue(v string) bool {
	if matchesSecretPattern(v) {
		return true
	}
	if strings.ContainsAny(v, "\n\r\t\"\\") {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false) // same encoding as the stored body
		if enc.Encode(v) == nil && matchesSecretPattern(buf.String()) {
			return true
		}
	}
	return false
}

func matchesSecretPattern(v string) bool {
	if sdk.ScanValue(v) != "" {
		return true
	}
	for _, re := range azureSensitiveValuePatterns {
		if re.MatchString(v) {
			return true
		}
	}
	return false
}

// redactRawBody returns body with secrets replaced by [redactedValue]
// and the number of values replaced. A string is redacted when:
//   - its key is sensitive ([isSensitiveARMKey]), or it is a string
//     element of an array under a sensitive key (objects below a
//     sensitive key are not blanked out; their fields are judged by
//     the next rule);
//   - it is a value-holder field ([valueHolderKeys]) of an object
//     below a sensitive key;
//   - it is in the value or defaultValue of an ARM securestring or
//     secureobject parameter (the whole subtree, for secureobject);
//   - it match a secret value pattern ([isSensitiveARMValue]) anywhere.
//
// Booleans, numbers and nulls are kept (e.g. "enableRbacAuthorization": true
// is configuration, not a secret). ok is false when body
// is not valid JSON.
func redactRawBody(body json.RawMessage) (out json.RawMessage, redacted int, ok bool) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, 0, false
	}
	v = redactValue(v, redactCtx{}, &redacted)

	// Every element starts on a new line after a one-space prefix, and
	// the encoder puts a space after each colon. The document secret
	// scanner matches value patterns against the whole body string; in
	// compact JSON a pattern that excludes whitespace, such as a
	// credential URI, can match across two unrelated fields (a URL in
	// one field and an e-mail address in a later one). The spaces
	// confine every match to a single JSON value, which is already
	// checked and redacted individually above.
	// A space, unlike a line break, is not escaped when the body is
	// stored as a JSON string. Prefix and indent must not both be
	// empty: that disables indentation and the output stays compact.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent(" ", "")
	if err := enc.Encode(v); err != nil {
		return nil, 0, false
	}
	return json.RawMessage(bytes.TrimRight(buf.Bytes(), "\n")), redacted, true
}

// redactCtx carries the redaction state of the value being visited.
type redactCtx struct {
	// redact: strings of this value are redacted. It covers a string
	// and the string elements of an array, but stops at objects, whose
	// fields are judged by their own keys.
	redact bool
	// all: every string in this value's whole subtree is redacted
	// (the value of an ARM securestring or secureobject parameter).
	all bool
	// underSecret: some enclosing key is sensitive.
	underSecret bool
	// keysAreNames: the keys of this object are names of other elements,
	// not field names, so they are never treated as sensitive keys (a
	// Logic App runAfter map such as {"Get_secret": ["Succeeded"]}
	// names the preceding action; its values are run statuses).
	keysAreNames bool
}

// secretContainerKeys are object fields whose whole subtree holds
// credentials (the parameter values of a Logic App API connection).
var secretContainerKeys = map[string]bool{
	"parametervalues":   true,
	"parametervalueset": true,
}

// nameKeyedObjects are object fields whose own keys are element names.
var nameKeyedObjects = map[string]bool{
	"runafter": true,
	// Key Vault access policy permissions: {"secrets": ["get", "list"]}
	// names permission categories; values are permission names.
	"permissions": true,
}

func redactValue(v any, ctx redactCtx, n *int) any {
	switch t := v.(type) {
	case map[string]any:
		secure := false
		if typ, ok := t["type"].(string); ok {
			switch strings.ToLower(typ) {
			case "securestring", "secureobject":
				secure = true
			}
		}
		for k, child := range t {
			lk := strings.ToLower(k)
			direct := !ctx.keysAreNames && isSensitiveARMKey(k)
			holder := !ctx.keysAreNames && valueHolderKeys[lk]
			t[k] = redactValue(child, redactCtx{
				redact:       direct || (holder && ctx.underSecret),
				all:          ctx.all || (holder && secure) || (!ctx.keysAreNames && secretContainerKeys[lk]),
				underSecret:  ctx.underSecret || direct,
				keysAreNames: nameKeyedObjects[lk],
			}, n)
		}
		return t
	case []any:
		for i, child := range t {
			t[i] = redactValue(child, ctx, n)
		}
		return t
	case string:
		// A "true"/"false" string under a sensitive key is a flag
		// stored as text (e.g. the AKS Key Vault add-on
		// "enableSecretRotation"), not a secret; secure parameters and
		// value patterns still apply.
		if ctx.redact && !ctx.all && isFlagString(t) {
			return t
		}
		if t != "" && (ctx.redact || ctx.all || isSensitiveARMValue(t)) {
			*n++
			return redactedValue
		}
		return t
	default:
		return v
	}
}

// isFlagString reports whether s is a boolean written as text.
func isFlagString(s string) bool {
	return strings.EqualFold(s, "true") || strings.EqualFold(s, "false")
}

// containsAzureSecret reports whether s matches an Azure value pattern.
func containsAzureSecret(s string) bool {
	for _, re := range azureSensitiveValuePatterns {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

// rawBodyStats counts the outcome of [applyRawBodies].
type rawBodyStats struct {
	ARM            int            // bodies replaced with the captured ARM object
	Struct         int            // bodies left as decoded struct (no capture for that ID)
	Rejected       int            // captures discarded because a secret pattern survived redaction
	StructRejected int            // StructRejected counts struct bodies dropped because a secret pattern survived redaction.
	Redacted       int            // values redacted across all attached bodies
	StructByType   map[string]int // StructByType counts Struct fallbacks per provider.type (ARM type), to show which list commands do not return a full body.
}

// applyRawBodies replaces the struct-derived ARM body of every resource
// that has one with the captured ARM object for its provider.native_id,
// after redaction. A resource without a usable capture keeps its struct
// body, which is redacted and checked the same way; a struct body that
// still matches a secret pattern is dropped (only the rule name is
// recorded). No body leaves this function unredacted.
func applyRawBodies(resources []sdk.Resource, store *rawStore) rawBodyStats {
	st := rawBodyStats{StructByType: map[string]int{}}
	for i := range resources {
		ext, ok := resources[i].Extensions["osiris.azure.arm"].(map[string]any)
		if !ok {
			continue
		}
		raw, captured := store.get(resources[i].Provider.NativeID)
		if !captured {
			st.Struct++
			st.StructByType[resources[i].Provider.Type]++
			st.Redacted += sanitizeStructBody(ext, &st)
			continue
		}
		red, n, rule := redactAndCheck(raw)
		if rule != "" {
			// Record only the rule name, never the matched text.
			ext["raw_rejected"] = rule
			st.Rejected++
			st.Redacted += sanitizeStructBody(ext, &st)
			continue
		}
		ext["body"] = string(red)
		ext["source"] = "arm"
		st.ARM++
		st.Redacted += n
	}
	return st
}

// redactAndCheck redacts an ARM body and reports the rule of any secret
// pattern that still matches ("invalid_json" when it is not JSON),
// or "" when the redacted body is safe to emit.
func redactAndCheck(body json.RawMessage) (json.RawMessage, int, string) {
	red, n, ok := redactRawBody(body)
	if !ok {
		return nil, 0, "invalid_json"
	}
	if rule := sdk.ScanValue(string(red)); rule != "" {
		return nil, 0, rule
	}
	// Belt and braces: no Azure credential format may survive either.
	if containsAzureSecret(string(red)) {
		return nil, 0, "azure_value_pattern"
	}
	return red, n, ""
}

// sanitizeStructBody redacts the struct-derived body in ext in place
// and sets source "struct". When the redacted body still matches a
// secret pattern the body is removed and struct_rejected names the
// rule. It returns the number of values redacted.
func sanitizeStructBody(ext map[string]any, st *rawBodyStats) int {
	ext["source"] = "struct"
	body, _ := ext["body"].(string)
	if body == "" {
		return 0
	}
	red, n, rule := redactAndCheck(json.RawMessage(body))
	if rule != "" {
		delete(ext, "body")
		ext["struct_rejected"] = rule
		st.StructRejected++
		return 0
	}
	ext["body"] = string(red)
	return n
}

func (s rawBodyStats) log(l *slog.Logger) {
	if l == nil {
		return
	}
	attrs := []any{
		"arm", s.ARM,
		"struct_fallback", s.Struct,
		"rejected", s.Rejected,
		"struct_rejected", s.StructRejected,
		"redacted_values", s.Redacted,
	}
	types := make([]string, 0, len(s.StructByType))
	for t := range s.StructByType {
		types = append(types, t)
	}
	sort.Strings(types)
	for _, t := range types {
		attrs = append(attrs, "struct."+t, s.StructByType[t])
	}
	l.Info("raw ARM body capture", attrs...)
}

// redactDocumentValues replaces every tag value, and every string in
// the properties and extensions of resources, connections and groups,
// that matches an SDK or Azure secret value pattern with
// [redactedValue]. The raw ARM body string is skipped: it is redacted
// value by value when attached. It returns the number of values
// replaced.
//
// Tags are emitted for every --purpose and are free text set by users
// and by the Azure portal (which stores an Application Insights
// connection string in the "hidden-link"
// tag), so they are never trusted to be free of credentials.
func redactDocumentValues(doc *sdk.Document) int {
	if doc == nil {
		return 0
	}
	n := 0
	tags := func(m map[string]string) map[string]string {
		var out map[string]string
		for k, v := range m {
			if v != "" && isSensitiveARMValue(v) {
				if out == nil {
					// Copy: tag maps may be shared between resources
					// and groups.
					out = make(map[string]string, len(m))
					for k2, v2 := range m {
						out[k2] = v2
					}
				}
				out[k] = redactedValue
				n++
			}
		}
		if out == nil {
			return m
		}
		return out
	}
	var walk func(v any) any
	walk = func(v any) any {
		switch t := v.(type) {
		case map[string]any:
			for k, c := range t {
				t[k] = walk(c)
			}
			return t
		case []any:
			for i, c := range t {
				t[i] = walk(c)
			}
			return t
		case []string:
			for i, c := range t {
				if c != "" && isSensitiveARMValue(c) {
					t[i] = redactedValue
					n++
				}
			}
			return t
		case string:
			if t != "" && isSensitiveARMValue(t) {
				n++
				return redactedValue
			}
			return t
		default:
			return v
		}
	}
	extensions := func(ext map[string]any) {
		for k, v := range ext {
			if k == "osiris.azure.arm" {
				if arm, ok := v.(map[string]any); ok {
					for ak, av := range arm {
						if ak != "body" {
							arm[ak] = walk(av)
						}
					}
				}
				continue
			}
			ext[k] = walk(v)
		}
	}
	for i := range doc.Topology.Resources {
		r := &doc.Topology.Resources[i]
		r.Tags = tags(r.Tags)
		walk(r.Properties)
		extensions(r.Extensions)
	}
	for i := range doc.Topology.Connections {
		c := &doc.Topology.Connections[i]
		c.Tags = tags(c.Tags)
		walk(c.Properties)
		extensions(c.Extensions)
	}
	for i := range doc.Topology.Groups {
		g := &doc.Topology.Groups[i]
		g.Tags = tags(g.Tags)
		walk(g.Properties)
		extensions(g.Extensions)
	}
	return n
}
