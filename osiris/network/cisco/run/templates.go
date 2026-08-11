// templates.go - Shared "template --generate" file generation for
// Cisco OSIRIS JSON Producer. Every producer that lets a user
// hand-author input (a CSV batch file, a --secrets-file) offers a
// "template --generate" command that writes a starter skeleton instead
// of making the user guess the shape from documentation.
// Centralized here so that behavior (0600 permissions, the confirmation
// message, the credential skeletons JSON shapes) is implemented once
// instead of once per producer apic/iosxe/nxos call GenerateTemplates
// directly; vmanage (which has no CSV batch mode) calls
// WriteTemplateFile, CredentialFileTemplate and
// CredentialRulesFileTemplate individually, see its own runTemplate.
//
// --secrets-file's two shapes (flat and rules, see CredentialFile's doc
// comment) are generated as two separate files rather than one file
// documenting both.
//
// OSIRIS JSON Producer for Cisco introduction:
// [OSIRIS-JSON-CISCO]: https://docs.osirisjson.org/osiris-producers/network/cisco
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package run

import (
	"encoding/json"
	"fmt"
	"os"
)

// WriteTemplateFile writes content to filename with 0600 permissions
// (a template may end up holding a real credential once the user edits
// it, so it starts owner-only) and prints a confirmation message.
func WriteTemplateFile(filename, content string) error {
	if err := os.WriteFile(filename, []byte(content), 0600); err != nil {
		return fmt.Errorf("failed to write %s: %w", filename, err)
	}
	fmt.Printf("Template saved to %s\n", filename)
	return nil
}

// credentialFileTemplateDoc is CredentialFileTemplate's output shape.
// Comment is a "$comment" field: the JSON Schema convention for an
// annotation with no effect on validation/parsing (see
// https://json-schema.org/understanding-json-schema/reference/comments)
// CredentialFile declares no such field, so the stdlib's default
// struct decoding silently ignores it, and the file still loads
// exactly as if the comment were never there.
type credentialFileTemplateDoc struct {
	Comment  string `json:"$comment"`
	Host     string `json:"host"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// CredentialFileTemplate returns a skeleton flat-shape --secrets-file
// JSON document (the same flag name and shape apic/iosxe/nxos and
// vmanage all share): a single login, for one target or shared by
// every target in a batch. exampleHost is shown as the "host" value
// callers pass whatever best illustrates their own target (an RFC 5737
// documentation address for apic/iosxe/nxos, a documentation domain
// for vmanage's controller-style host). See CredentialRulesFileTemplate
// for the alternate per-host/CIDR shape, generated as its own separate
// file.
func CredentialFileTemplate(exampleHost string) string {
	doc := credentialFileTemplateDoc{
		Comment:  `osirisjson-producer --secrets-file: one login, used for a single target or shared by every target in a batch. Leave a field out to fall back to its own flag or an interactive prompt. For different credentials per target, see the companion "-multihost" template instead.`,
		Host:     exampleHost,
		Username: "user",
		Password: "changeme",
	}
	// doc is a fixed literal built entirely from string fields; encode
	// it can never fail, so a MarshalIndent error is unreachable and
	// deliberately not handled as anything other than an empty result.
	data, _ := json.MarshalIndent(doc, "", "  ")
	return string(data) + "\n"
}

// credentialRulesFileTemplateDoc is CredentialRulesFileTemplate's
// output shape reuses CredentialFile's own Default/Rules types
// (credentials.go) so the template can never drift out of sync with
// what LoadCredentialFile actually accepts.
// See credentialFileTemplateDoc doc comment for why Comment uses the
// JSON Schema "$comment" key.
type credentialRulesFileTemplateDoc struct {
	Comment string           `json:"$comment"`
	Default CredentialEntry  `json:"default"`
	Rules   []CredentialRule `json:"rules"`
}

// CredentialRulesFileTemplate returns a skeleton rules-shape
// --secrets-file JSON document: different credentials for different
// targets in one batch, matched by host or CIDR. hostsA and hostsB are
// the two example rules' "hosts" patterns callers pass whatever best
// illustrates their own targets (RFC 5737 CIDR/exact-IP patterns for
// apic/iosxe/nxos, example.com-based FQDNs for vmanage's
// controller-style hosts). Every username/password placeholder in the
// output is the same "user"/"changeme" pair CredentialFileTemplate
// uses, deliberately not a per-rule name like "user" the
// "hosts" pattern is what distinguishes the rules from each other, the
// credential fields are just a fill-in-the-blank placeholder.
func CredentialRulesFileTemplate(hostsA, hostsB string) string {
	doc := credentialRulesFileTemplateDoc{
		Comment: `osirisjson-producer --secrets-file (multi-host shape): per-target credentials, matched by "hosts" (comma-separated exact hosts/IPs and/or CIDR blocks) tried top to bottom, first match wins. A target matching no rule uses "default". There is no per-rule "port" the CSV batch file's own port column is the only source of truth for a target's port. For a single shared login instead, see the companion template without "-multihost" in its name.`,
		Default: CredentialEntry{Username: "user", Password: "changeme"},
		Rules: []CredentialRule{
			{Hosts: hostsA, CredentialEntry: CredentialEntry{Username: "user", Password: "changeme"}},
			{Hosts: hostsB, CredentialEntry: CredentialEntry{Username: "user", Password: "changeme"}},
		},
	}
	data, _ := json.MarshalIndent(doc, "", "  ")
	return string(data) + "\n"
}

// GenerateTemplates writes a CSV batch template
// (cisco-<producerName>-template.csv) plus both --secrets-file shapes
// flat (cisco-<producerName>-secrets.json) and rules
// (cisco-<producerName>-secrets-multihost.json) for producerName,
// used by apic/iosxe/nxos's "<producer> template --generate". vmanage
// has no CSV batch mode, so it does not use this helper see its own
// runTemplate, which writes only the two --secrets-file variants via
// WriteTemplateFile, CredentialFileTemplate and
// CredentialRulesFileTemplate directly.
func GenerateTemplates(producerName, exampleHost string) error {
	csvFilename := fmt.Sprintf("cisco-%s-template.csv", producerName)
	if err := WriteTemplateFile(csvFilename, CSVTemplate(producerName)); err != nil {
		return err
	}

	credFilename := fmt.Sprintf("cisco-%s-secrets.json", producerName)
	if err := WriteTemplateFile(credFilename, CredentialFileTemplate(exampleHost)); err != nil {
		return err
	}

	multihostFilename := fmt.Sprintf("cisco-%s-secrets-multihost.json", producerName)
	return WriteTemplateFile(multihostFilename, CredentialRulesFileTemplate("192.0.2.0/24", "198.51.100.10,198.51.100.11"))
}
