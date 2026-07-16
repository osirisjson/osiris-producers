// personal_api.go - HPE GreenLake "Personal API client" authentication
// for the HPE Aruba Networking Central OSIRIS JSON producer.
//
// Alongside the company IT provisioned API Gateway application
// (client_id/client_secret bound to a browser-authorized access/refresh
// token pair, see config.go and client.go's refresh), a GreenLake
// account user can self-service generate up to 7 "Personal API clients"
// under https://common.cloud.hpe.com/manage-account/api useful when no
// IT-provisioned gateway application has been set up yet.
// These are a client_id/client_secret pair only: no access/refresh
// token pair is issued ahead of time. An access token is minted
// (and re-minted on expiry/401, in place of a refresh_token exchange)
// via the OAuth2 client_credentials grant against HPE GreenLake's
// SSO token endpoint, which is fixed and independent of any Aruba
// Central cluster/region.
//
// This path is selected whenever client_id and client_secret are both
// present and no refresh_token is available: a classic gateway app's
// token pair always includes a refresh_token, so refresh_token=="" is
// what distinguishes a Personal API client credential set from a
// gateway app one.
//
// OSIRIS JSON Producer for HPE Aruba Networking Central introduction:
// [OSIRIS-JSON-HPE-ARUBA-NETWORKING]: https://docs.osirisjson.org/osiris-producers/network/hpe-aruba-networking

package arubacentral

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// greenLakeTokenURL is the fixed HPE GreenLake SSO OAuth2 token
// endpoint used to mint access tokens for Personal API clients. Unlike
// the classic Aruba Central API Gateway, this single URL serves every
// cluster/region, and its own rate limits are independent of Central's
// account-wide 8 req/s throttle (see minRequestInterval), so calls here
// are not passed through c.throttle().
//
// A var, not a const, so tests can point it at an httptest server.
var greenLakeTokenURL = "https://sso.common.cloud.hpe.com/as/token.oauth2"

// isPersonalAPIClient reports whether creds should authenticate via the
// GreenLake Personal API client (client_credentials) flow rather than
// the classic API Gateway refresh_token flow.
func isPersonalAPIClient(creds Credentials) bool {
	return creds.RefreshToken == "" && creds.ClientID != "" && creds.ClientSecret != ""
}

// mintPersonalAPIToken exchanges a Personal API client's
// client_id/client_secret for an access token via
// grant_type=client_credentials.
func mintPersonalAPIToken(httpClient *http.Client, clientID, clientSecret string) (string, error) {
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)

	req, err := http.NewRequest(http.MethodPost, greenLakeTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("building GreenLake token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("GreenLake token request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading GreenLake token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GreenLake token request failed (HTTP %d): %s", resp.StatusCode, truncateBody(body))
	}

	var tok tokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return "", fmt.Errorf("parsing GreenLake token response: %w", err)
	}
	if tok.AccessToken == "" {
		return "", fmt.Errorf("GreenLake token response did not contain an access_token")
	}
	return tok.AccessToken, nil
}

// defaultMintPersonalAPIToken is the real network-backed mintToken function
// used outside of tests (see resolveCredentialsInteractive and Client.refresh).
func defaultMintPersonalAPIToken(clientID, clientSecret string) (string, error) {
	return mintPersonalAPIToken(&http.Client{Timeout: 30 * time.Second}, clientID, clientSecret)
}
