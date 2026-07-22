// discover_test.go - Unit tests for cluster auto-detection.
//
// OSIRIS JSON Producer for HPE Aruba Networking Central introduction:
// [OSIRIS-JSON-HPE-ARUBA-NETWORKING]: https://docs.osirisjson.org/osiris-producers/network/hpe-aruba-networking-central

package arubacentral

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func fakeClusterServer(t *testing.T, acceptToken string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+acceptToken {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"message":"Unauthorized"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"items":[]}`))
	}))
}

func TestDetectClusterAmong_FindsAcceptingCluster(t *testing.T) {
	wrong1 := fakeClusterServer(t, "some-other-token")
	defer wrong1.Close()
	wrong2 := fakeClusterServer(t, "yet-another-token")
	defer wrong2.Close()
	correct := fakeClusterServer(t, "the-real-token")
	defer correct.Close()

	candidates := []clusterCandidate{
		{cluster: "eu", baseURL: wrong1.URL},
		{cluster: "prod", baseURL: correct.URL},
		{cluster: "apac", baseURL: wrong2.URL},
	}

	cluster, baseURL, err := detectClusterAmong(candidates, "the-real-token", 2*time.Second)
	if err != nil {
		t.Fatalf("detectClusterAmong failed: %v", err)
	}
	if cluster != "prod" {
		t.Errorf("expected cluster %q, got %q", "prod", cluster)
	}
	if baseURL != correct.URL {
		t.Errorf("expected baseURL %q, got %q", correct.URL, baseURL)
	}
}

func TestDetectClusterAmong_NoneAccept(t *testing.T) {
	wrong1 := fakeClusterServer(t, "some-other-token")
	defer wrong1.Close()
	wrong2 := fakeClusterServer(t, "yet-another-token")
	defer wrong2.Close()

	candidates := []clusterCandidate{
		{cluster: "eu", baseURL: wrong1.URL},
		{cluster: "apac", baseURL: wrong2.URL},
	}

	_, _, err := detectClusterAmong(candidates, "the-real-token", 2*time.Second)
	if err == nil {
		t.Fatal("expected an error when no cluster accepts the token")
	}
}

func TestDetectClusterAmong_NoCandidates(t *testing.T) {
	_, _, err := detectClusterAmong(nil, "token", time.Second)
	if err == nil {
		t.Fatal("expected an error for an empty candidate list")
	}
}

func TestCandidateClusters_ExcludesInternal(t *testing.T) {
	for _, c := range candidateClusters() {
		if c.cluster == "internal" {
			t.Fatal("expected \"internal\" to be excluded from auto-detection candidates")
		}
	}
	if len(candidateClusters()) != len(clusterBaseURLs)-1 {
		t.Errorf("expected exactly one cluster excluded, got %d candidates from %d total", len(candidateClusters()), len(clusterBaseURLs))
	}
}
