// validate.go - Schema pattern validators for OSIRIS element fields.
// Enforces type patterns (resource, connection, group), provider name/namespace
// conventions, status/direction enums and ID format constraints as defined
// in [osiris.schema.json].
//
// For an introduction to OSIRIS JSON Producer Development Guidelines see:
// "[OSIRIS-PRODUCER-GUIDELINES]."
//
// [OSIRIS-PRODUCER-GUIDELINES]: https://osirisjson.org/en/docs/developers/producers/welcome
// [osiris.schema.json]: https://osirisjson.org/schema/v1.0/osiris.schema.json

package sdk

import (
	"fmt"
	"regexp"
	"strings"
)

// Validation patterns from core osiris.schema.json.
var (
	// resourceTypeRe matches resource types: at least two dot-separated lowercase segments.
	// e.g. "compute.vm", "network.switch.leaf"
	resourceTypeRe = regexp.MustCompile(`^[a-z0-9]+(?:\.[a-z0-9]+)+$`)

	// connectionTypeRe matches connection types: one or more dot-separated lowercase segments.
	// e.g. "network", "network.l2", "dataflow.tcp"
	connectionTypeRe = regexp.MustCompile(`^[a-z0-9]+(?:\.[a-z0-9]+)*$`)

	// groupTypeRe matches group types: same pattern as connection types.
	groupTypeRe = connectionTypeRe

	// providerNameRe matches provider names: lowercase alphanumeric with optional dots.
	// e.g. "aws", "cisco", "custom"
	providerNameRe = regexp.MustCompile(`^[a-z0-9]+(?:\.[a-z0-9]+)*$`)

	// namespaceRe matches custom provider namespaces: osiris.<identifier>
	// e.g. "osiris.com.acme"
	namespaceRe = regexp.MustCompile(`^osiris\.[a-z0-9]+(?:\.[a-z0-9]+)*$`)
)

// Valid status values for resources and connections.
var validStatuses = map[string]bool{
	"active": true,
	"inactive": true,
	"degraded": true,
	"retired": true,
	"unknown": true,
}

// Valid direction values for connections.
var validDirections = map[string]bool{
	"bidirectional": true,
	"forward": true,
	"reverse": true,
}

// ValidateResourceType checks that a resource type matches the schema pattern.
func ValidateResourceType(t string) error {
	if !resourceTypeRe.MatchString(t) {
		return fmt.Errorf("invalid resource type %q: must match pattern %s (e.g. \"compute.vm\", \"network.switch\")",
			t, resourceTypeRe.String())
	}
	return nil
}

// ValidateConnectionType checks that a connection type matches the schema pattern.
func ValidateConnectionType(t string) error {
	if !connectionTypeRe.MatchString(t) {
		return fmt.Errorf("invalid connection type %q: must match pattern %s (e.g. \"network\", \"dataflow.tcp\")",
			t, connectionTypeRe.String())
	}
	return nil
}

// ValidateGroupType checks that a group type matches the schema pattern.
func ValidateGroupType(t string) error {
	if !groupTypeRe.MatchString(t) {
		return fmt.Errorf("invalid group type %q: must match pattern %s (e.g. \"physical.site\", \"logical.environment\")",
			t, groupTypeRe.String())
	}
	return nil
}

// ValidateProviderName checks that a provider name matches the schema pattern.
func ValidateProviderName(name string) error {
	if !providerNameRe.MatchString(name) {
		return fmt.Errorf("invalid provider name %q: must match pattern %s (e.g. \"aws\", \"cisco\", \"custom\")",
			name, providerNameRe.String())
	}
	return nil
}

// ValidateNamespace checks that a custom provider namespace matches the required pattern.
func ValidateNamespace(ns string) error {
	if !namespaceRe.MatchString(ns) {
		return fmt.Errorf("invalid namespace %q: must match pattern %s (e.g. \"osiris.com.acme\")",
			ns, namespaceRe.String())
	}
	return nil
}

// ValidateStatus checks that a status value is one of the allowed enum values.
// Empty string is allowed (status is optional).
func ValidateStatus(s string) error {
	if s == "" {
		return nil
	}
	if !validStatuses[s] {
		return fmt.Errorf("invalid status %q: must be one of active, inactive, degraded, retired, unknown", s)
	}
	return nil
}

// ValidateDirection checks that a connection direction is valid.
// Empty string is allowed (defaults to bidirectional).
func ValidateDirection(d string) error {
	if d == "" {
		return nil
	}
	if !validDirections[d] {
		return fmt.Errorf("invalid direction %q: must be one of bidirectional, forward, reverse", d)
	}
	return nil
}

// ValidateID checks that an ID is non-empty.
func ValidateID(id, context string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("%s id must not be empty", context)
	}
	return nil
}

// ValidateProvider checks that a provider struct has valid fields.
func ValidateProvider(p Provider) error {
	if err := ValidateProviderName(p.Name); err != nil {
		return err
	}
	if p.Name == "custom" {
		if p.Namespace == "" {
			return fmt.Errorf("provider namespace is required when provider.name is \"custom\"")
		}
		if err := ValidateNamespace(p.Namespace); err != nil {
			return err
		}
	}
	return nil
}
