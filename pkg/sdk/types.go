// Package sdk provides the Go producer SDK for generating OSIRIS JSON documents.
//
// The SDK implements the contracts defined in OSIRIS-ADG-PR-SDK-1.0:
// typed Go structs mirroring [osiris.schema.json], deterministic ID generation,
// a document builder, normalization utilities and factory functions.
//
// The SDK MUST NOT import, embed or link against @osirisjson/core (TypeScript/NPM).
// Validation is done via the canonical CLI as an external process in CI.
//
// For an introduction to OSIRIS JSON Producer Development Guidelines see:
// "[OSIRIS-PRODUCER-GUIDELINES]."
//
// [OSIRIS-PRODUCER-GUIDELINES]: https://osirisjson.org/en/docs/getting-started/osiris-producer-guidelines
// [osiris.schema.json]: https://osirisjson.org/schema/v1.0/osiris.schema.json
// [OSIRIS-ADG-PR-SDK-1.0]: https://github.com/osirisjson/osiris-producers/blob/main/docs/guidelines/v1.0/OSIRIS-PRODUCER-SDK.md

package sdk

// Document is the top-level OSIRIS JSON structure.
type Document struct {
	Schema   string   `json:"$schema"`
	Version  string   `json:"version"`
	Metadata Metadata `json:"metadata"`
	Topology Topology `json:"topology"`
}

// Metadata carries document-level information.
type Metadata struct {
	Timestamp       string    `json:"timestamp"`
	Generator       Generator `json:"generator"`
	Scope           *Scope    `json:"scope,omitempty"`
	Redacted        *bool     `json:"redacted,omitempty"`
	RedactionPolicy string    `json:"redaction_policy,omitempty"`
}

// Generator identifies the tool that produced the document.
type Generator struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	URL     string `json:"url,omitempty"`
}

// Scope describes the export boundaries.
type Scope struct {
	Name          string   `json:"name,omitempty"`
	Description   string   `json:"description,omitempty"`
	Purpose       string   `json:"purpose,omitempty"`
	Providers     []string `json:"providers,omitempty"`
	Accounts      []string `json:"accounts,omitempty"`
	Regions       []string `json:"regions,omitempty"`
	Sites         []string `json:"sites,omitempty"`
	Environments  []string `json:"environments,omitempty"`
	Subscriptions []string `json:"subscriptions,omitempty"`
	Projects      []string `json:"projects,omitempty"`
	Clusters      []string `json:"clusters,omitempty"`
}

// Topology contains the three topology arrays.
type Topology struct {
	Resources   []Resource   `json:"resources"`
	Connections []Connection `json:"connections,omitempty"`
	Groups      []Group      `json:"groups,omitempty"`
}

// Resource represents an infrastructure entity.
type Resource struct {
	ID          string            `json:"id"`
	Type        string            `json:"type"`
	Name        string            `json:"name,omitempty"`
	Description string            `json:"description,omitempty"`
	Provider    Provider          `json:"provider"`
	Status      string            `json:"status,omitempty"`
	State       string            `json:"state,omitempty"`
	Properties  map[string]any    `json:"properties,omitempty"`
	Extensions  map[string]any    `json:"extensions,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
}

// Provider carries vendor attribution for a resource.
type Provider struct {
	Name         string `json:"name"`
	Namespace    string `json:"namespace,omitempty"`
	NativeID     string `json:"native_id,omitempty"`
	Account      string `json:"account,omitempty"`
	Tenant       string `json:"tenant,omitempty"`
	Type         string `json:"type,omitempty"`
	Region       string `json:"region,omitempty"`
	Zone         string `json:"zone,omitempty"`
	Subscription string `json:"subscription,omitempty"`
	Project      string `json:"project,omitempty"`
	Site         string `json:"site,omitempty"`
	System       string `json:"system,omitempty"`
	Source       string `json:"source,omitempty"`
	Version      string `json:"version,omitempty"`
}

// Connection represents a relationship between two resources.
type Connection struct {
	ID          string            `json:"id"`
	Type        string            `json:"type"`
	Source      string            `json:"source"`
	Target      string            `json:"target"`
	Direction   string            `json:"direction,omitempty"`
	Name        string            `json:"name,omitempty"`
	Description string            `json:"description,omitempty"`
	Status      string            `json:"status,omitempty"`
	State       string            `json:"state,omitempty"`
	Properties  map[string]any    `json:"properties,omitempty"`
	Extensions  map[string]any    `json:"extensions,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
}

// Group represents a classification or organizational boundary.
type Group struct {
	ID          string            `json:"id"`
	Type        string            `json:"type"`
	Name        string            `json:"name,omitempty"`
	Description string            `json:"description,omitempty"`
	Members     []string          `json:"members,omitempty"`
	Children    []string          `json:"children,omitempty"`
	Properties  map[string]any    `json:"properties,omitempty"`
	Extensions  map[string]any    `json:"extensions,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
}
