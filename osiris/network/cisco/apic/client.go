// client.go - APIC REST API client for the Cisco ACI producer.
// Implements authentication via aaaLogin and class-based queries with pagination.
// The underlying http.Client cookie jar handles APIC session tokens automatically.
//
// For an introduction to OSIRIS JSON Producer for Cisco see:
// "[OSIRIS-JSON-CISCO]."
//
// [OSIRIS-JSON-CISCO]: https://osirisjson.org/en/docs/producers/cisco

package apic

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"go.osirisjson.org/producers/osiris/network/cisco/run"
)

const (
	defaultAPICPort = 443
	pageSize        = 100
)

// Client is a thin HTTP client for the APIC REST API.
type Client struct {
	baseURL    string
	httpClient *http.Client
	token      string
	logger     *slog.Logger
}

// NewClient creates an APIC client targeting the given address.
func NewClient(target run.TargetConfig, insecure bool, logger *slog.Logger) *Client {
	addr := run.ResolveAddr(target, defaultAPICPort)
	return &Client{
		baseURL:    "https://" + addr,
		httpClient: run.NewHTTPClient(insecure),
		logger:     logger,
	}
}

// Login authenticates against the APIC and stores the session token.
func (c *Client) Login(username, password string) error {
	payload := fmt.Sprintf(`{"aaaUser":{"attributes":{"name":"%s","pwd":"%s"}}}`, username, password)
	url := c.baseURL + "/api/aaaLogin.json"

	resp, err := c.httpClient.Post(url, "application/json", strings.NewReader(payload))
	if err != nil {
		return fmt.Errorf("APIC login request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("APIC login: failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("APIC login failed (HTTP %d): %s", resp.StatusCode, truncateBody(body))
	}

	// Extract token from response for logging; cookie jar handles session automatically.
	var loginResp imDataResponse
	if err := json.Unmarshal(body, &loginResp); err != nil {
		return fmt.Errorf("APIC login: failed to parse response: %w", err)
	}

	if len(loginResp.ImData) > 0 {
		if aaaLogin, ok := loginResp.ImData[0]["aaaLogin"]; ok {
			if attrs, ok := aaaLogin.(map[string]any); ok {
				if inner, ok := attrs["attributes"].(map[string]any); ok {
					if tok, ok := inner["token"].(string); ok {
						c.token = tok
					}
				}
			}
		}
	}

	if c.token == "" {
		return fmt.Errorf("APIC login: no token in response")
	}

	c.logger.Info("APIC login successful", "url", c.baseURL)
	return nil
}

// QueryClass fetches all objects of a given APIC class, handling pagination.
// Returns a slice of attribute maps (one per object).
func (c *Client) QueryClass(class string) ([]map[string]any, error) {
	var all []map[string]any
	page := 0

	for {
		url := fmt.Sprintf("%s/api/class/%s.json?page=%d&page-size=%d", c.baseURL, class, page, pageSize)

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("APIC query %s: failed to create request: %w", class, err)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("APIC query %s: request failed: %w", class, err)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()

		if err != nil {
			return nil, fmt.Errorf("APIC query %s: failed to read response: %w", class, err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("APIC query %s failed (HTTP %d): %s", class, resp.StatusCode, truncateBody(body))
		}

		var parsed imDataResponse
		if err := json.Unmarshal(body, &parsed); err != nil {
			return nil, fmt.Errorf("APIC query %s: failed to parse response: %w", class, err)
		}

		attrs := extractAttributes(parsed.ImData)
		all = append(all, attrs...)

		c.logger.Debug("APIC query", "class", class, "page", page, "objects", len(attrs))

		if len(attrs) < pageSize {
			break
		}
		page++
	}

	c.logger.Info("APIC query complete", "class", class, "total", len(all))
	return all, nil
}

// imDataResponse is the common APIC JSON envelope.
type imDataResponse struct {
	ImData []map[string]any `json:"imdata"`
}

// extractAttributes pulls the attributes map from each imdata entry.
// APIC responses follow: {"imdata": [{"className": {"attributes": {...}}}]}
func extractAttributes(imdata []map[string]any) []map[string]any {
	var result []map[string]any
	for _, item := range imdata {
		for _, classObj := range item {
			if obj, ok := classObj.(map[string]any); ok {
				if attrs, ok := obj["attributes"].(map[string]any); ok {
					result = append(result, attrs)
				}
			}
		}
	}
	return result
}

// truncateBody returns the first 200 bytes of a response body for error messages.
func truncateBody(body []byte) string {
	if len(body) > 200 {
		return string(body[:200]) + "..."
	}
	return string(body)
}
