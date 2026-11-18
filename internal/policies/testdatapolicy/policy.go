// policy.go - Rule catalog and approved values of the OSIRIS JSON
// Producers test data policy: rule identifiers, remediation hints,
// documentation address blocks, approved names, ranges, the
// hyperscaler service hostname suffix list and the hyperscaler
// identity values.
//
// Every value here mirrors the policy document. Changing one is a
// policy change and must be reflected there.
//
// OSIRIS JSON Producers Test Data Policy:
// [OSIRIS-PRODUCER-TEST-DATA-POLICY]: https://github.com/osirisjson/osiris-producers/blob/main/docs/policies/OSIRIS-PRODUCER-TEST-DATA-POLICY.md

package testdatapolicy

import (
	"net/netip"
	"strings"
)

// Rule identifiers. They are local to this repository and unrelated to
// OSIRIS JSON validation diagnostic codes (policy section 2).
const (
	RuleIPv4       = "OSIRIS-PR-TDP-001"
	RuleIPv6       = "OSIRIS-PR-TDP-002"
	RuleMAC        = "OSIRIS-PR-TDP-003"
	RuleDNS        = "OSIRIS-PR-TDP-004"
	RuleDevice     = "OSIRIS-PR-TDP-005"
	RuleGeo        = "OSIRIS-PR-TDP-006"
	RuleSite       = "OSIRIS-PR-TDP-007"
	RuleASN        = "OSIRIS-PR-TDP-008"
	RuleSerial     = "OSIRIS-PR-TDP-009"
	RuleCredential = "OSIRIS-PR-TDP-010"
	RuleException  = "OSIRIS-PR-TDP-016"
	RuleInventory  = "OSIRIS-PR-TDP-017"

	RuleAzureGUID       = "OSIRIS-PR-TDP-011"
	RuleAzureARM        = "OSIRIS-PR-TDP-012"
	RuleAWSAccount      = "OSIRIS-PR-TDP-013"
	RuleAWSResource     = "OSIRIS-PR-TDP-014"
	RuleHyperscalerName = "OSIRIS-PR-TDP-015"
)

// remedies maps each rule to the approved alternative printed with a
// finding.
var remedies = map[string]string{
	RuleIPv4:       "an address in 192.0.2.0/24, 198.51.100.0/24 or 203.0.113.0/24 (RFC 5737)",
	RuleIPv6:       "an address in 2001:db8::/32 (RFC 3849)",
	RuleMAC:        "00:00:5E:00:53:xx (RFC 9542), a locally administered unicast MAC or AA:BB:CC:DD:xx:xx",
	RuleDNS:        "a name under example.com, example.net or example.org",
	RuleDevice:     "a name starting with LAB-, TEST-, FAKE- or STUB-",
	RuleGeo:        "latitude 45.5433411 and longitude 10.2241237",
	RuleSite:       "MXP, BRANCH-00, OFFICE-00 or FACTORY-00",
	RuleASN:        "an AS number in 64496-64511, 64512-65534 or 4200000000-4294967294",
	RuleSerial:     "a value starting with TST",
	RuleCredential: "admin, test, password, secret or changeme, optionally followed by -N",
	RuleException:  "fix or remove the allowlist entry",
	RuleInventory:  "a regular text file, or an allowlist entry for a reviewed binary",

	RuleAzureGUID:       "00000000-0000-N000-8000-00000000XXXX with N in 1-5 and XXXX four hex digits",
	RuleAzureARM:        "/subscriptions/<approved GUID>/resourceGroups/<synthetic name>/providers/...",
	RuleAWSAccount:      "123456789012, 111122223333 or 444455556666",
	RuleAWSResource:     "a zero-padded ID such as vpc-00000000000000001 (17 hex) or sg-00000001 (8 hex)",
	RuleHyperscalerName: "a name starting with LAB, TEST, FAKE or STUB in the service's own syntax",
}

// exceptableRules lists the rules an allowlist entry may name.
// [RuleException] itself can never be excepted.
var exceptableRules = map[string]bool{
	RuleIPv4: true, RuleIPv6: true, RuleMAC: true, RuleDNS: true,
	RuleDevice: true, RuleGeo: true, RuleSite: true, RuleASN: true,
	RuleSerial: true, RuleCredential: true, RuleInventory: true,
	RuleAzureGUID: true, RuleAzureARM: true, RuleAWSAccount: true,
	RuleAWSResource: true, RuleHyperscalerName: true,
}

// IPv4 documentation blocks (RFC 5737) and the IPv6 documentation
// prefix (RFC 3849).
var (
	docIPv4 = []netip.Prefix{
		netip.MustParsePrefix("192.0.2.0/24"),
		netip.MustParsePrefix("198.51.100.0/24"),
		netip.MustParsePrefix("203.0.113.0/24"),
	}
	docIPv6 = netip.MustParsePrefix("2001:db8::/32")
)

// Approved latitude and longitude.
const (
	approvedLatitude  = 45.5433411
	approvedLongitude = 10.2241237
)

// approvedSites lists the approved site and location labels.
var approvedSites = map[string]bool{
	"MXP": true, "BRANCH-00": true, "OFFICE-00": true, "FACTORY-00": true,
}

// devicePrefixes lists the approved device name prefixes, compared
// case-insensitively.
var devicePrefixes = []string{"LAB-", "TEST-", "FAKE-", "STUB-"}

// syntheticLabelPrefixes lists the approved leading words for the
// leftmost label of a name under a hyperscaler service suffix.
var syntheticLabelPrefixes = []string{"lab", "test", "fake", "stub", "example"}

// approvedDomains lists the domains whose name and subdomains are
// always approved: the RFC 2606 example domains and the project's own
// domain.
var approvedDomains = []string{"example.com", "example.net", "example.org", "osirisjson.org"}

// projectGitHubOrg is the project's GitHub organization. URLs into it
// are project references, not example data.
const projectGitHubOrg = "osirisjson"

// hyperscalerSuffixes lists the approved official hyperscaler service
// hostname suffixes (policy section 4.1).
var hyperscalerSuffixes = []string{
	"blob.core.windows.net", "file.core.windows.net",
	"queue.core.windows.net", "table.core.windows.net",
	"dfs.core.windows.net", "vault.azure.net", "azurewebsites.net",
	"azurecr.io", "database.windows.net", "servicebus.windows.net",
	"redis.cache.windows.net", "documents.azure.com",
	"postgres.database.azure.com", "management.azure-api.net",
	"developer.azure-api.net", "azure-api.net", "azmk8s.io",
	"azurecontainerapps.io", "azurecontainer.io",
	"awsapps.com",
}

// privateLinkLabel is the fixed leftmost label of Azure private DNS
// zones (privatelink.<service suffix>).
const privateLinkLabel = "privatelink"

// credentialPlaceholders lists the approved credential placeholders.
var credentialPlaceholders = []string{"admin", "test", "password", "secret", "changeme"}

// ASN ranges approved by the policy (inclusive).
var asnRanges = [][2]uint64{
	{64496, 64511},           // RFC 5398 documentation
	{64512, 65534},           // RFC 6996 private use
	{4200000000, 4294967294}, // RFC 6996 private use
}

// Context keys, compared after [normalizeKey].
var (
	deviceKeys = keySet("hostname", "sysname", "devicename", "nodename",
		"switchname", "clientname", "remotedevicename", "remotehostname",
		"remotesysname", "neighborname", "neighbordevice", "devicehostname")
	dnsKeys = keySet("host", "hosts", "fqdn", "defaulthostname",
		"latestrevisionfqdn", "defaultdomain", "domain", "domainname",
		"url", "baseurl", "starturl", "endpoint", "server", "servername")
	latitudeKeys  = keySet("latitude", "lat")
	longitudeKeys = keySet("longitude", "lon", "lng")
	siteKeys      = keySet("sitename", "site", "locationname")
	asnKeys       = keySet("asn", "as", "localas", "remoteas", "peeras",
		"peerasn", "bgpasn", "asnumber", "asnum", "localasn", "remoteasn",
		"virtualrouterasn")
	serialKeys = keySet("serial", "serialnumber", "serialno",
		"chassisserial", "boardserial", "deviceserial",
		"connecteddeviceserial", "assettag")
	// credentialSuffixes match a normalized key by suffix, so
	// clientSecret, accessToken and apiKey are all covered.
	credentialSuffixes = []string{"password", "passwd", "passphrase",
		"secret", "token", "apikey", "privatekey", "secretkey", "accesskey",
		"authorization"}
	// nonSecretTokenKeys end in "token" but name pagination cursors or
	// identity parts, not credentials.
	nonSecretTokenKeys = []string{"nexttoken", "pagetoken",
		"continuationtoken", "skiptoken", "boundarytoken"}
)

// redactionMarkers are the SDK redaction outputs. They replace a
// credential and are never a credential themselves.
var redactionMarkers = map[string]bool{"[REDACTED]": true, "REDACTED": true}

func keySet(keys ...string) map[string]bool {
	m := make(map[string]bool, len(keys))
	for _, k := range keys {
		m[k] = true
	}
	return m
}

// normalizeKey lowercases a field name, keeps only the last segment
// of an XML namespace or Go selector (c.token -> token) and removes
// '_' and '-', so host_name, hostName and host-name compare equal.
func normalizeKey(k string) string {
	if i := strings.LastIndexAny(k, ":."); i >= 0 && i < len(k)-1 {
		k = k[i+1:]
	}
	k = strings.ToLower(k)
	return strings.NewReplacer("_", "", "-", "").Replace(k)
}

func isCredentialKey(k string) bool {
	for _, s := range nonSecretTokenKeys {
		if strings.HasSuffix(k, s) {
			return false
		}
	}
	for _, s := range credentialSuffixes {
		if strings.HasSuffix(k, s) {
			return true
		}
	}
	return false
}

// Hyperscaler identity values (policy section 5).
var (
	// approvedAWSAccounts are the AWS documentation example accounts.
	approvedAWSAccounts = map[string]bool{
		"123456789012": true, "111122223333": true, "444455556666": true,
	}

	// hyperscalerNamePrefixes are the approved leading words of an
	// invented hyperscaler resource name, compared case-insensitively.
	hyperscalerNamePrefixes = []string{"lab", "test", "fake", "stub"}

	// awsHexFamilies lists the AWS resource ID prefixes whose suffix is
	// hexadecimal (8 legacy or 17 current digits).
	awsHexFamilies = []string{"tgw-attach", "eipalloc", "vpce", "eigw",
		"dopt", "subnet", "vpc", "sg", "eni", "rtb", "igw", "nat",
		"acl", "vol", "snap", "ami", "tgw", "pcx", "cgw", "vgw", "vpn",
		"lt", "pl", "i"}

	// azureReservedNames are service-defined Azure names, approved only
	// for the resource type (lowercase) they belong to.
	azureReservedNames = map[string][]string{
		"subnets":          {"GatewaySubnet", "RouteServerSubnet", "AzureFirewallSubnet", "AzureBastionSubnet"},
		"ipconfigurations": {"ipconfig1"},
		"managedapis":      {"azureblob"},
	}
)

// Hyperscaler identity field names, compared after [normalizeKey].
var (
	azureGUIDKeys = keySet("tenant", "tenantid", "subscription",
		"subscriptionid", "objectid", "principalid", "clientid",
		"applicationid", "appid")
	awsAccountKeys = keySet("accountid", "awsaccountid", "ownerid",
		"owneraccount", "sourceaccount", "targetaccount", "accountnumber",
		"peerownerid", "requesterid")
	resourceGroupKeys = keySet("resourcegroup", "resourcegroupname")
	awsNameKeys       = map[string]string{
		"bucket": "s3", "bucketname": "s3", "rolename": "iam",
		"functionname": "lambda", "clustername": "",
	}
)
