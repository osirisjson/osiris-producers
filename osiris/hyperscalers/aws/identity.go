// identity.go - Caller identity, partition and scope validation for the
// OSIRIS JSON Amazon AWS producer.
//
// Every target is anchored to the caller identity verified by STS
// GetCallerIdentity (see [Client.ResolveIdentity]). A declared account
// ID from the CSV is only an expectation: it is compared with the
// verified caller and never used as proof. This file holds the
// validation helpers that support that rule:
//
//   - [validAccountID], [validRegion]: syntax checks applied to flags
//     and CSV input before any AWS call or output path is built.
//   - [stsRegions]: the STS endpoints tried for one target.
//   - [arnPartition], [rewritePartition]: constructed resource IDs use
//     the partition of the verified caller, not a literal "aws".
//   - [resolveConnectionEndpoints]: a connection endpoint that was not
//     collected becomes a reference stub only when an AWS-returned
//     owner proves it belongs to another account; otherwise the
//     connection is dropped and counted.
//
// For an introduction to OSIRIS JSON Producer for Amazon AWS see:
// [OSIRIS-JSON-AWS]: https://docs.osirisjson.org/osiris-producers/hyperscalers/amazon-aws/

package aws

import (
	"errors"
	"regexp"
	"strings"

	"go.osirisjson.org/producers/pkg/sdk"
)

// errIdentityUnverified reports that STS did not return a usable caller
// identity. No resource is collected for the target.
var errIdentityUnverified = errors.New("caller identity unverified")

// errIdentityMismatch reports that the verified caller belongs to a
// different account than the target declares.
var errIdentityMismatch = errors.New("caller identity does not match target account")

var (
	accountIDPattern = regexp.MustCompile(`^[0-9]{12}$`)
	// regionPattern accepts AWS region codes such as us-east-1,
	// us-gov-west-1, cn-northwest-1 or eusc-de-east-1.
	regionPattern = regexp.MustCompile(`^[a-z]{2,4}(-[a-z]+)+-[0-9]{1,2}$`)
)

// validAccountID reports whether s is a 12-digit AWS account ID.
func validAccountID(s string) bool {
	return accountIDPattern.MatchString(s)
}

// validRegion reports whether s is syntactically an AWS region code.
func validRegion(s string) bool {
	return regionPattern.MatchString(s)
}

// commercialRegion reports whether region belongs to the commercial
// aws partition, where the us-east-1 STS endpoint is valid.
func commercialRegion(region string) bool {
	for _, p := range []string{"cn-", "us-gov-", "us-iso", "eusc-"} {
		if strings.HasPrefix(region, p) {
			return false
		}
	}
	return true
}

// stsRegions returns the STS endpoint regions tried for a client in
// region: the region itself, then us-east-1 once in the commercial
// partition.
func stsRegions(region string) []string {
	if region == "" || region == GlobalRegion {
		return []string{GlobalRegion}
	}
	if commercialRegion(region) {
		return []string{region, GlobalRegion}
	}
	return []string{region}
}

// arnPartition returns the partition component of arn, or "" when arn
// is not a well-formed ARN.
func arnPartition(arn string) string {
	parts := strings.SplitN(arn, ":", 6)
	if len(parts) != 6 || parts[0] != "arn" || parts[1] == "" {
		return ""
	}
	return parts[1]
}

// constructedIDPrefix is the prefix of every resource ID built by
// [resourceID] with the commercial partition.
const constructedIDPrefix = "aws::arn:aws:"

// rewritePartition rewrites constructed IDs in resources, connections
// and groups to partition. IDs taken from AWS-returned ARNs already
// carry the right partition, so in a non-commercial partition only
// constructed IDs start with [constructedIDPrefix]. It is a no-op for
// the commercial partition.
func rewritePartition(resources []sdk.Resource, conns []sdk.Connection, groups []sdk.Group, partition string) {
	if partition == "" || partition == "aws" {
		return
	}
	newPrefix := "aws::arn:" + partition + ":"
	fix := func(id string) string {
		if strings.HasPrefix(id, constructedIDPrefix) {
			return newPrefix + strings.TrimPrefix(id, constructedIDPrefix)
		}
		return id
	}
	for i := range resources {
		resources[i].ID = fix(resources[i].ID)
	}
	for i := range conns {
		conns[i].Source = fix(conns[i].Source)
		conns[i].Target = fix(conns[i].Target)
	}
	for i := range groups {
		g := &groups[i]
		g.ID = fix(g.ID)
		for j := range g.Members {
			g.Members[j] = fix(g.Members[j])
		}
		for j := range g.Children {
			g.Children[j] = fix(g.Children[j])
		}
	}
}

// resolveConnectionEndpoints returns the connections whose endpoints
// are both known or proven remote, the reference stubs for proven
// remote endpoints and the number of connections dropped.
//
// An endpoint is proven remote when its ID carries a valid account that
// differs from callerAccount: transforms only build such IDs from an
// AWS-returned owner or ARN (for example a peering accepter OwnerId or
// a shared Transit Gateway ARN). An unknown endpoint in the caller
// account, or one whose ID cannot be parsed, means a local collector
// failed or filtered it; the connection is dropped rather than repaired
// with a fabricated node.
func resolveConnectionEndpoints(conns []sdk.Connection, known []sdk.Resource, callerAccount string) ([]sdk.Connection, []sdk.Resource, int) {
	knownIDs := make(map[string]bool, len(known))
	for _, r := range known {
		knownIDs[r.ID] = true
	}
	// provenRemote reports whether an unknown endpoint may be stubbed.
	provenRemote := func(id string) bool {
		_, _, _, account, _ := parseStubARN(id)
		return validAccountID(account) && account != callerAccount
	}
	stubbed := map[string]bool{}
	var stubs []sdk.Resource
	kept := conns[:0]
	dropped := 0
	for _, c := range conns {
		ok := true
		for _, id := range []string{c.Source, c.Target} {
			if !knownIDs[id] && !stubbed[id] && !provenRemote(id) {
				ok = false
			}
		}
		if !ok {
			dropped++
			continue
		}
		for _, id := range []string{c.Source, c.Target} {
			if !knownIDs[id] && !stubbed[id] {
				stubbed[id] = true
				stubs = append(stubs, crossAccountStub(id))
			}
		}
		kept = append(kept, c)
	}
	return kept, stubs, dropped
}
