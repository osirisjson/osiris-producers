// personal_api_test.go - Unit tests for
// GreenLake Personal API client token minting.
//
// OSIRIS JSON Producer for HPE Aruba Networking Central introduction:
// [OSIRIS-JSON-HPE-ARUBA-NETWORKING]: https://docs.osirisjson.org/osiris-producers/network/hpe-aruba-networking
package arubacentral

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsPersonalAPIClient(t *testing.T) {
	tests := []struct {
		name  string
		creds Credentials
		want  bool
	}{
		{name: "client id/secret with no refresh token", creds: Credentials{ClientID: "id", ClientSecret: "secret"}, want: true},
		{name: "classic gateway app with a refresh token", creds: Credentials{ClientID: "id", ClientSecret: "secret", RefreshToken: "refresh"}, want: false},
		{name: "missing client secret", creds: Credentials{ClientID: "id"}, want: false},
		{name: "missing client id", creds: Credentials{ClientSecret: "secret"}, want: false},
		{name: "access token only", creds: Credentials{AccessToken: "token"}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPersonalAPIClient(tt.creds); got != tt.want {
				t.Errorf("isPersonalAPIClient(%+v) = %v, want %v", tt.creds, got, tt.want)
			}
		})
	}
}

// withGreenLakeTokenURL points greenLakeTokenURL at ts for the duration
// of the test, restoring the real HPE SSO URL on cleanup, so these
// tests never make a live network call to sso.common.cloud.hpe.com.
func withGreenLakeTokenURL(t *testing.T, ts *httptest.Server) {
	t.Helper()
	original := greenLakeTokenURL
	greenLakeTokenURL = ts.URL
	t.Cleanup(func() { greenLakeTokenURL = original })
}

func TestMintPersonalAPIToken_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parsing form: %v", err)
		}
		if r.FormValue("grant_type") != "client_credentials" {
			t.Errorf("expected grant_type=client_credentials, got %q", r.FormValue("grant_type"))
		}
		if r.FormValue("client_id") != "personal-id" || r.FormValue("client_secret") != "personal-secret" {
			t.Errorf("unexpected client_id/client_secret in request: %v", r.Form)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"access_token": "minted-token", "token_type": "bearer", "expires_in": 7200})
	}))
	defer ts.Close()
	withGreenLakeTokenURL(t, ts)

	token, err := mintPersonalAPIToken(ts.Client(), "personal-id", "personal-secret")
	if err != nil {
		t.Fatalf("mintPersonalAPIToken failed: %v", err)
	}
	if token != "minted-token" {
		t.Errorf("got token %q, want %q", token, "minted-token")
	}
}

func TestMintPersonalAPIToken_HTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid_client"}`))
	}))
	defer ts.Close()
	withGreenLakeTokenURL(t, ts)

	if _, err := mintPersonalAPIToken(ts.Client(), "bad-id", "bad-secret"); err == nil {
		t.Fatal("expected an error on a non-200 response")
	}
}

func TestMintPersonalAPIToken_MissingAccessTokenInResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"token_type": "bearer"})
	}))
	defer ts.Close()
	withGreenLakeTokenURL(t, ts)

	if _, err := mintPersonalAPIToken(ts.Client(), "id", "secret"); err == nil {
		t.Fatal("expected an error when the response has no access_token")
	}
}
