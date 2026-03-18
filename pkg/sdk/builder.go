// builder.go - DocumentBuilder assembles and validates a complete OSIRIS JSON document.
// Build() enforces invariants: generator metadata, deterministic sorting, duplicate ID
// detection, reference integrity (connection endpoints, group members/children),
// extension key validation and secret scanning per the configured safe failure mode.
//
// For an introduction to OSIRIS JSON Producer Development Guidelines see:
// "[OSIRIS-PRODUCER-GUIDELINES]."
//
// [OSIRIS-PRODUCER-GUIDELINES]: https://osirisjson.org/en/docs/producers/getting-started

package sdk

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// DocumentBuilder assembles the top-level OSIRIS document structure.
// It enforces structural invariants and produces deterministic, diff-friendly output.
type DocumentBuilder struct {
	ctx           *Context
	generatorName string
	generatorVer  string
	scope         *Scope
	resources     []Resource
	connections   []Connection
	groups        []Group
}

// NewDocumentBuilder creates a new builder bound to the given context.
func NewDocumentBuilder(ctx *Context) *DocumentBuilder {
	return &DocumentBuilder{ctx: ctx}
}

// WithGenerator sets the generator name and version (both required).
func (b *DocumentBuilder) WithGenerator(name, version string) *DocumentBuilder {
	b.generatorName = name
	b.generatorVer = version
	return b
}

// WithScope sets the export scope metadata.
func (b *DocumentBuilder) WithScope(scope Scope) *DocumentBuilder {
	b.scope = &scope
	return b
}

// AddResource appends a resource to the topology.
func (b *DocumentBuilder) AddResource(r Resource) {
	b.resources = append(b.resources, r)
}

// AddConnection appends a connection to the topology.
func (b *DocumentBuilder) AddConnection(c Connection) {
	b.connections = append(b.connections, c)
}

// AddGroup appends a group to the topology.
func (b *DocumentBuilder) AddGroup(g Group) {
	b.groups = append(b.groups, g)
}

/*
Build assembles the final OSIRIS Document with all invariants enforced.

Invariants:
  - $schema and version are set from SDK constants.
  - metadata.timestamp is set from ctx.Clock() at Build() time.
  - metadata.generator.name and version are required.
  - No duplicate IDs across resources, connections, or groups.
  - Connection source/target must reference existing resource IDs.
  - Group members must reference existing resource IDs.
  - Group children must reference existing group IDs.
  - Topology arrays are sorted by id.
  - Group members and children are deduplicated and sorted.
  - Redaction metadata is set based on config safe failure mode.
  - Extension keys must match the osiris.* namespace pattern.
  - Secret scanning is enforced per safe failure mode (unless mode is "off").
*/
func (b *DocumentBuilder) Build() (*Document, error) {
	if b.generatorName == "" || b.generatorVer == "" {
		return nil, errors.New("generator name and version are required")
	}

	// Duplicate ID detection.
	if err := b.checkDuplicateIDs(); err != nil {
		return nil, err
	}

	// Reference integrity.
	if err := b.checkReferenceIntegrity(); err != nil {
		return nil, err
	}

	// Extension key validation.
	if err := b.checkExtensionKeys(); err != nil {
		return nil, err
	}

	// Sort topology arrays by ID for deterministic output.
	sort.Slice(b.resources, func(i, j int) bool {
		return b.resources[i].ID < b.resources[j].ID
	})
	sort.Slice(b.connections, func(i, j int) bool {
		return b.connections[i].ID < b.connections[j].ID
	})
	sort.Slice(b.groups, func(i, j int) bool {
		return b.groups[i].ID < b.groups[j].ID
	})

	// Ensure group members/children are sorted and deduplicated.
	for i := range b.groups {
		b.groups[i].Members = dedup(b.groups[i].Members)
		b.groups[i].Children = dedup(b.groups[i].Children)
	}

	// Build metadata
	meta := Metadata{
		Timestamp: NormalizeRFC3339UTC(b.ctx.Clock()),
		Generator: Generator{
			Name:    b.generatorName,
			Version: b.generatorVer,
		},
		Scope: b.scope,
	}

	// Set redaction metadata based on safe failure mode.
	sfm := ""
	if b.ctx.Config != nil {
		sfm = b.ctx.Config.SafeFailureMode
	}
	if sfm == LogAndRedact {
		t := true
		meta.Redacted = &t
		meta.RedactionPolicy = LogAndRedact
	}

	// Ensure resources is never nil (schema requires the array).
	resources := b.resources
	if resources == nil {
		resources = []Resource{}
	}

	doc := &Document{
		Schema:   SchemaURI,
		Version:  SpecVersion,
		Metadata: meta,
		Topology: Topology{
			Resources:   resources,
			Connections: b.connections,
			Groups:      b.groups,
		},
	}

	// Secret scanning.
	if sfm != Off {
		findings := ScanDocument(doc)
		if len(findings) > 0 {
			if sfm == LogAndRedact {
				// Log findings (path only, never values) and continue.
				for _, f := range findings {
					if b.ctx.Logger != nil {
						b.ctx.Logger.Warn("secret detected (redacted)", "path", f.Path, "rule", f.Rule)
					}
				}
				// Note: actual value replacement is the producer's responsibility
				// before calling Build(). The SDK logs and flags the document.
			} else {
				// fail-closed (default): return error with paths only.
				paths := make([]string, len(findings))
				for i, f := range findings {
					paths[i] = f.Path + " (" + f.Rule + ")"
				}
				return nil, fmt.Errorf("secret scanning detected %d finding(s), aborting (fail-closed): %s",
					len(findings), strings.Join(paths, "; "))
			}
		}
	}

	return doc, nil
}

// checkDuplicateIDs verifies that no two elements share the same ID
// within their respective arrays, or across arrays.
func (b *DocumentBuilder) checkDuplicateIDs() error {
	seen := make(map[string]string) // id -> "resource"|"connection"|"group".

	for _, r := range b.resources {
		if prev, ok := seen[r.ID]; ok {
			return fmt.Errorf("duplicate id %q: found in both %s and resource", r.ID, prev)
		}
		seen[r.ID] = "resource"
	}
	for _, c := range b.connections {
		if prev, ok := seen[c.ID]; ok {
			return fmt.Errorf("duplicate id %q: found in both %s and connection", c.ID, prev)
		}
		seen[c.ID] = "connection"
	}
	for _, g := range b.groups {
		if prev, ok := seen[g.ID]; ok {
			return fmt.Errorf("duplicate id %q: found in both %s and group", g.ID, prev)
		}
		seen[g.ID] = "group"
	}
	return nil
}

// checkReferenceIntegrity verifies that all connection source/target references
// and group member/children references point to existing IDs.
func (b *DocumentBuilder) checkReferenceIntegrity() error {
	resourceIDs := make(map[string]bool, len(b.resources))
	for _, r := range b.resources {
		resourceIDs[r.ID] = true
	}

	groupIDs := make(map[string]bool, len(b.groups))
	for _, g := range b.groups {
		groupIDs[g.ID] = true
	}

	// Connection source and target must reference existing resources.
	for _, c := range b.connections {
		if !resourceIDs[c.Source] {
			return fmt.Errorf("connection %q: source %q does not reference an existing resource", c.ID, c.Source)
		}
		if !resourceIDs[c.Target] {
			return fmt.Errorf("connection %q: target %q does not reference an existing resource", c.ID, c.Target)
		}
	}

	// Group members must reference existing resources.
	for _, g := range b.groups {
		for _, m := range g.Members {
			if !resourceIDs[m] {
				return fmt.Errorf("group %q: member %q does not reference an existing resource", g.ID, m)
			}
		}
		// Group children must reference existing groups.
		for _, child := range g.Children {
			if !groupIDs[child] {
				return fmt.Errorf("group %q: child %q does not reference an existing group", g.ID, child)
			}
		}
	}

	return nil
}

// checkExtensionKeys validates that all extension keys across resources,
// connections and groups match the required osiris.* namespace pattern.
func (b *DocumentBuilder) checkExtensionKeys() error {
	for i, r := range b.resources {
		for key := range r.Extensions {
			if err := ValidateNamespace(key); err != nil {
				return fmt.Errorf("resource %q (index %d): invalid extension key %q: must match osiris.<namespace> pattern", r.ID, i, key)
			}
		}
	}
	for i, c := range b.connections {
		for key := range c.Extensions {
			if err := ValidateNamespace(key); err != nil {
				return fmt.Errorf("connection %q (index %d): invalid extension key %q: must match osiris.<namespace> pattern", c.ID, i, key)
			}
		}
	}
	for i, g := range b.groups {
		for key := range g.Extensions {
			if err := ValidateNamespace(key); err != nil {
				return fmt.Errorf("group %q (index %d): invalid extension key %q: must match osiris.<namespace> pattern", g.ID, i, key)
			}
		}
	}
	return nil
}

// dedup sorts and deduplicates a string slice.
func dedup(s []string) []string {
	if len(s) == 0 {
		return s
	}
	sort.Strings(s)
	j := 0
	for i := 1; i < len(s); i++ {
		if s[i] != s[j] {
			j++
			s[j] = s[i]
		}
	}
	return s[:j+1]
}
