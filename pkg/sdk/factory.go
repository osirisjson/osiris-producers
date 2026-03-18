// factory.go - Validating factory functions for OSIRIS document elements.
// Provides NewResource, NewProvider, NewConnection, NewGroup constructors that
// enforce schema constraints (type patterns, status enums, direction values)
// at creation time rather than deferring to Build().
//
// For an introduction to OSIRIS JSON Producer Development Guidelines see:
// "[OSIRIS-PRODUCER-GUIDELINES]."
//
// [OSIRIS-PRODUCER-GUIDELINES]: https://osirisjson.org/en/docs/getting-started/osiris-producer-guidelines

package sdk

import (
	"fmt"
	"sort"
	"strings"
)

// NewResource creates a Resource with the required fields pre-populated.
// Returns an error if id is empty, resourceType doesn't match the schema pattern, or provider is invalid.
// Optional fields (Name, Description, Status, Properties, Extensions, Tags)
// are set directly on the returned struct.
func NewResource(id, resourceType string, provider Provider) (Resource, error) {
	if err := ValidateID(id, "resource"); err != nil {
		return Resource{}, err
	}
	if err := ValidateResourceType(resourceType); err != nil {
		return Resource{}, err
	}
	if err := ValidateProvider(provider); err != nil {
		return Resource{}, fmt.Errorf("resource %q: %w", id, err)
	}
	return Resource{
		ID:       id,
		Type:     resourceType,
		Provider: provider,
	}, nil
}

// NewProvider creates a Provider with the required name field.
// Returns an error if name doesn't match the schema pattern.
func NewProvider(name string) (Provider, error) {
	if err := ValidateProviderName(name); err != nil {
		return Provider{}, err
	}
	return Provider{Name: name}, nil
}

// NewCustomProvider creates a Provider with name="custom" and the required namespace.
// The namespace MUST follow the osiris.<identifier> pattern.
func NewCustomProvider(namespace string) (Provider, error) {
	if !strings.HasPrefix(namespace, "osiris.") {
		return Provider{}, fmt.Errorf("custom provider namespace must start with \"osiris.\", got %q", namespace)
	}
	if err := ValidateNamespace(namespace); err != nil {
		return Provider{}, err
	}
	return Provider{
		Name:      "custom",
		Namespace: namespace,
	}, nil
}

// NewConnection creates a Connection with the required fields.
// Direction defaults to "bidirectional".
// Returns an error if any required field is invalid.
func NewConnection(id, connType, source, target string) (Connection, error) {
	if err := ValidateID(id, "connection"); err != nil {
		return Connection{}, err
	}
	if err := ValidateConnectionType(connType); err != nil {
		return Connection{}, err
	}
	if err := ValidateID(source, "connection source"); err != nil {
		return Connection{}, err
	}
	if err := ValidateID(target, "connection target"); err != nil {
		return Connection{}, err
	}
	return Connection{
		ID:        id,
		Type:      connType,
		Source:    source,
		Target:    target,
		Direction: "bidirectional",
	}, nil
}

// NewGroup creates a Group with the required fields.
// Returns an error if id is empty or groupType is invalid.
func NewGroup(id, groupType string) (Group, error) {
	if err := ValidateID(id, "group"); err != nil {
		return Group{}, err
	}
	if err := ValidateGroupType(groupType); err != nil {
		return Group{}, err
	}
	return Group{
		ID:   id,
		Type: groupType,
	}, nil
}

// SetStatus sets the status field on a resource, validating the value.
func (r *Resource) SetStatus(status string) error {
	if err := ValidateStatus(status); err != nil {
		return err
	}
	r.Status = status
	return nil
}

// SetStatus sets the status field on a connection, validating the value.
func (c *Connection) SetStatus(status string) error {
	if err := ValidateStatus(status); err != nil {
		return err
	}
	c.Status = status
	return nil
}

// SetDirection sets the direction field on a connection, validating the value.
func (c *Connection) SetDirection(direction string) error {
	if err := ValidateDirection(direction); err != nil {
		return err
	}
	c.Direction = direction
	return nil
}

// AddMembers appends resource IDs to the group's members slice.
// Entries are deduplicated and kept sorted lexicographically.
func (g *Group) AddMembers(resourceIDs ...string) {
	g.Members = addUniqueSorted(g.Members, resourceIDs...)
}

// AddChildren appends child group IDs to the group's children slice.
// Entries are deduplicated and kept sorted lexicographically.
func (g *Group) AddChildren(groupIDs ...string) {
	g.Children = addUniqueSorted(g.Children, groupIDs...)
}

// addUniqueSorted appends values to a slice, deduplicates and sorts.
func addUniqueSorted(existing []string, values ...string) []string {
	seen := make(map[string]struct{}, len(existing)+len(values))
	for _, v := range existing {
		seen[v] = struct{}{}
	}
	result := make([]string, 0, len(seen)+len(values))
	result = append(result, existing...)
	for _, v := range values {
		if _, ok := seen[v]; !ok {
			seen[v] = struct{}{}
			result = append(result, v)
		}
	}
	sort.Strings(result)
	return result
}
