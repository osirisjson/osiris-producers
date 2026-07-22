// discover.go - Cluster auto-detection for
// the HPE Aruba Networking Central OSIRIS JSON producer.
//
// Aruba Central does not expose a "which cluster is my account on"
// lookup endpoint. Each cluster short code maps to a fully independent
// API Gateway deployment, so an access token minted for one cluster is
// rejected (401) by every other cluster. This lets the producer skip
// asking the user for --cluster/--base-url: probe every known cluster
// concurrently with the supplied token and use whichever accepts it.
//
// OSIRIS JSON Producer for HPE Aruba Networking Central introduction:
// [OSIRIS-JSON-HPE-ARUBA-NETWORKING]: https://docs.osirisjson.org/osiris-producers/network/hpe-aruba-networking-central
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package arubacentral

import (
	"fmt"
	"net/http"
	"sort"
	"time"
)

// defaultProbeTimeout bounds how long a single cluster probe may take,
// so a slow/unreachable cluster (e.g. china-prod from outside its
// network) cannot stall the whole detection past a few seconds;
// probes run concurrently.
const defaultProbeTimeout = 6 * time.Second

// probePath is a cheap, always-present authenticated endpoint used only
// to test whether an access token is accepted by a given cluster.
const probePath = "/network-monitoring/v1/switches?limit=1"

// clusterCandidate pairs a cluster short code with its base URL.
type clusterCandidate struct {
	cluster string
	baseURL string
}

// candidateClusters returns every publicly reachable cluster, sorted by
// short code for deterministic probe ordering/logging. "internal" is
// excluded as HPE-internal only, not a customer-facing cluster.
func candidateClusters() []clusterCandidate {
	candidates := make([]clusterCandidate, 0, len(clusterBaseURLs))
	for cluster, url := range clusterBaseURLs {
		if cluster == "internal" {
			continue
		}
		candidates = append(candidates, clusterCandidate{cluster: cluster, baseURL: url})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].cluster < candidates[j].cluster })
	return candidates
}

// DetectCluster probes every known Aruba Central cluster concurrently
// with accessToken and returns the short code and base URL of the first
// one that accepts it.
// Used when the user does not supply --cluster/--base-url.
func DetectCluster(accessToken string) (cluster string, baseURL string, err error) {
	return detectClusterAmong(candidateClusters(), accessToken, defaultProbeTimeout)
}

// detectClusterAmong implements DetectCluster over an explicit
// candidate list and timeout, so tests can substitute httptest servers
// for the real cluster URLs.
func detectClusterAmong(candidates []clusterCandidate, accessToken string, probeTimeout time.Duration) (string, string, error) {
	if len(candidates) == 0 {
		return "", "", fmt.Errorf("no candidate clusters to probe")
	}

	type result struct {
		cluster string
		baseURL string
		ok      bool
	}

	probeClient := &http.Client{Timeout: probeTimeout}
	results := make(chan result, len(candidates))

	for _, c := range candidates {
		go func(c clusterCandidate) {
			results <- result{cluster: c.cluster, baseURL: c.baseURL, ok: probeCluster(probeClient, c.baseURL, accessToken)}
		}(c)
	}

	tried := make([]string, 0, len(candidates))
	for range candidates {
		r := <-results
		tried = append(tried, r.cluster)
		if r.ok {
			return r.cluster, r.baseURL, nil
		}
	}

	sort.Strings(tried)
	return "", "", fmt.Errorf("access token was not accepted by any known cluster (tried: %v); verify the token or specify --cluster/--base-url explicitly", tried)
}

// probeCluster returns true if baseURL accepts accessToken (HTTP 200 on
// a minimal authenticated request). Any other outcome, 401/403 (wrong
// cluster), network error, timeout, is treated as "not this cluster".
func probeCluster(client *http.Client, baseURL, accessToken string) bool {
	req, err := http.NewRequest(http.MethodGet, baseURL+probePath, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
