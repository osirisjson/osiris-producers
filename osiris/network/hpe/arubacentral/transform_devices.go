// transform_devices.go - Config-health enrichment shared across device
// resource types (switch, access point, gateway).
//
// Aruba Central's device identity/config-compliance signal
// (config-health summary + active issues, keyed by serial) is folded
// onto the already role-specific resource built from the
// switches/aps/gateways endpoints, rather than emitted as a fourth
// duplicate "generic device" resource per physical box.
//
// OSIRIS JSON Producer for HPE Aruba Networking Central introduction:
// [OSIRIS-JSON-HPE-ARUBA-NETWORKING]: https://docs.osirisjson.org/osiris-producers/network/hpe-aruba-networking
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package arubacentral

import (
	"go.osirisjson.org/producers/pkg/sdk"
)

// EnrichConfigHealth folds a device's configuration compliance summary
// and active issue counts onto its resource. When the compliance status
// indicates drift or failure, the resource status is downgraded to
// "degraded" (a documentation-purpose signal); the full issue payload
// is only attached under audit purpose flag.
func EnrichConfigHealth(r *sdk.Resource, summary *ConfigHealthSummary, issues *ConfigHealthIssue, purpose string) {
	if r.Properties == nil {
		r.Properties = map[string]any{}
	}

	if summary != nil {
		setIfNotEmpty(r.Properties, "config_status", summary.ConfigStatus)
		setIfNotEmpty(r.Properties, "top_priority_issue", summary.TopPriorityIssue)
		setIfNotEmpty(r.Properties, "recommended_action", summary.RecommendedAction)
		setIfNotEmpty(r.Properties, "last_config_timestamp", summary.LastConfigTimestamp)

		if mapped := mapConfigStatus(summary.ConfigStatus); mapped == "degraded" {
			r.Status = "degraded"
		}
	}

	if issues == nil {
		return
	}
	issueCount := len(issues.ConfigPullFailures) + len(issues.ConfigPushFailures) +
		len(issues.InvalidConfig) + len(issues.FilteredConfig)
	if issueCount == 0 {
		return
	}

	r.Properties["config_issue_count"] = issueCount
	if r.Status == "active" {
		r.Status = "degraded"
	}

	if purpose == "audit" {
		if r.Extensions == nil {
			r.Extensions = map[string]any{}
		}
		r.Extensions[extensionNamespace] = map[string]any{
			"config_pull_failures": issues.ConfigPullFailures,
			"config_push_failures": issues.ConfigPushFailures,
			"invalid_config":       issues.InvalidConfig,
			"filtered_config":      issues.FilteredConfig,
		}
	}
}

// deviceGroupName returns the device group name from a config-health
// summary, or "" if unavailable. Used to wire switch/AP/gateway
// resources into their logical.devicegroup group, since only the
// config-health endpoint (not the switches/aps/gateways list endpoints)
// reliably report device group membership across all three device types.
func deviceGroupName(summary *ConfigHealthSummary) string {
	if summary == nil {
		return ""
	}
	return summary.DeviceGroupName
}
