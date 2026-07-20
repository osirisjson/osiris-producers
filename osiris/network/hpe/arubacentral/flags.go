// flags.go - CLI flag parsing for the HPE Aruba Networking Central
// OSIRIS JSON producer.
//
// Authentication requires an API Gateway application
// (client_id/client_secret) and a token pair
// (access_token/refresh_token) created once via the Central UI or the
// standard OAuth2 authorization-code browser flow. This producer
// only performs the machine-to-machine calls: it uses the access token
// as-is and refreshes it (grant_type=refresh_token) when the
// API returns 401.
//
// Deliberately no --client-id/--client-secret/--access-token/--refresh-token
// flags: CLI flag values are visible to any local user via `ps` and get
// written to shell history. Credentials are instead read from
// --token-file (in case you run scheduled jobs), environment variables,
// or for whichever of those two are still missing an interactive
// /dev/tty prompt (see tty.go) that never echoes secrets and never
// persists what was typed unless --token-file was also given.
//
// OSIRIS JSON Producer for HPE Aruba Networking Central introduction:
// [OSIRIS-JSON-HPE-ARUBA-NETWORKING]: https://docs.osirisjson.org/osiris-producers/network/hpe-aruba-networking
// [OSIRIS-JSON-SPEC]: https://osirisjson.org/en/specification

package arubacentral

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"go.osirisjson.org/producers/pkg/osirismeta"
)

// tokenFileContents is the on-disk JSON shape for --token-file.
// client_id/client_secret are optional in the file (may be supplied via
// flags or environment instead) but are included so a
// single file is portable.
type tokenFileContents struct {
	ClientID     string `json:"client_id,omitempty"`
	ClientSecret string `json:"client_secret,omitempty"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// ParseFlags parses CLI flags for the Aruba Central producer
// and returns a Config.
func ParseFlags(args []string) (*Config, error) {
	fs := flag.NewFlagSet("osirisjson-producer hpe arubacentral", flag.ContinueOnError)

	var (
		cluster        string
		baseURL        string
		tokenFile      string
		sites          string
		all            bool
		output         string
		safeFail       string
		purposeStr     string
		includeRawBody bool
	)

	fs.StringVar(&cluster, "cluster", envOrDefault("ARUBA_CENTRAL_CLUSTER", ""), "Aruba Central cluster short code (e.g. eu, eucentral3, prod, uswest4); see --help for the full list")
	fs.StringVar(&baseURL, "base-url", envOrDefault("ARUBA_CENTRAL_BASE_URL", ""), "override the API Gateway base URL (takes precedence over --cluster)")
	fs.StringVar(&tokenFile, "token-file", envOrDefault("ARUBA_CENTRAL_TOKEN_FILE", ""), "JSON file with {client_id, client_secret, access_token, refresh_token}; rewritten in place when the access token is refreshed")
	fs.StringVar(&sites, "site", "", "comma-separated site name(s) to collect (optional; empty = every site reachable by the credential)")
	fs.BoolVar(&all, "all", false, "auto-discover and export every accessible site non-interactively (skips the site picker; mutually exclusive with --site)")
	fs.StringVar(&output, "o", "", fmt.Sprintf("output directory - every run writes <output-dir>/<site-name>/hpe-arubacentral-<timestamp>-<site-name>.json, created if missing and reused if it already exists (default: %q in the current directory)", defaultOutputDir))
	fs.StringVar(&output, "output", "", "output directory, see -o")
	fs.StringVar(&safeFail, "safe-failure-mode", "fail-closed", "secret handling: fail-closed, log-and-redact, or off")
	fs.StringVar(&purposeStr, "purpose", "", "OSIRIS JSON spec chapter 13.1.3 output grade: documentation (default) or audit")
	fs.BoolVar(&includeRawBody, "include-raw-body", false, "attach full API response body under extensions[\"osiris.hpe.arubacentral\"].raw (audit mode only; lossless fallback for still OSIRIS unmodelled fields)")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	switch safeFail {
	case "fail-closed", "log-and-redact", "off":
	default:
		return nil, fmt.Errorf("invalid --safe-failure-mode value %q: must be fail-closed, log-and-redact, or off", safeFail)
	}

	if all && sites != "" {
		return nil, fmt.Errorf("--all and --site are mutually exclusive: use one")
	}

	purpose, err := osirismeta.ParsePurpose(purposeStr)
	if err != nil {
		return nil, err
	}
	purposeString := purpose.String()

	creds := Credentials{
		ClientID:     envOrDefault("ARUBA_CENTRAL_CLIENT_ID", ""),
		ClientSecret: envOrDefault("ARUBA_CENTRAL_CLIENT_SECRET", ""),
		AccessToken:  envOrDefault("ARUBA_CENTRAL_ACCESS_TOKEN", ""),
		RefreshToken: envOrDefault("ARUBA_CENTRAL_REFRESH_TOKEN", ""),
	}

	if tokenFile != "" {
		loaded, err := loadTokenFile(tokenFile)
		if err != nil {
			return nil, fmt.Errorf("loading --token-file %q: %w", tokenFile, err)
		}
		if creds.ClientID == "" {
			creds.ClientID = loaded.ClientID
		}
		if creds.ClientSecret == "" {
			creds.ClientSecret = loaded.ClientSecret
		}
		if creds.AccessToken == "" {
			creds.AccessToken = loaded.AccessToken
		}
		if creds.RefreshToken == "" {
			creds.RefreshToken = loaded.RefreshToken
		}
		creds.TokenFile = tokenFile
	}

	// Whatever --token-file/environment variables didn't provide is
	// asked for interactively (see tty.go); nothing typed there is
	// written to disk unless --token-file was also given.
	creds, err = resolveCredentialsInteractive(creds)
	if err != nil {
		return nil, err
	}

	// Resolve base URL: explicit --base-url wins, then --cluster lookup,
	// then auto-detection by probing every known cluster with the
	// access token (each cluster is an independent API Gateway that
	// rejects tokens minted for any other cluster, so the one that
	// accepts it is the account's home cluster).
	resolvedBaseURL := strings.TrimRight(baseURL, "/")
	resolvedCluster := cluster
	if resolvedBaseURL == "" && cluster != "" {
		url, ok := ClusterBaseURL(cluster)
		if !ok {
			return nil, fmt.Errorf("unknown --cluster %q; run 'osirisjson-producer hpe arubacentral --help' for the list of valid cluster codes", cluster)
		}
		resolvedBaseURL = url
	}
	if resolvedBaseURL == "" {
		fmt.Fprintln(os.Stderr, "no --cluster/--base-url given; detecting cluster from the access token...")
		detectedCluster, detectedURL, err := DetectCluster(creds.AccessToken)
		if err != nil {
			return nil, fmt.Errorf("cluster auto-detection failed: %w", err)
		}
		fmt.Fprintf(os.Stderr, "detected cluster %q (%s); pass --cluster %s next time to skip detection\n", detectedCluster, detectedURL, detectedCluster)
		resolvedBaseURL = detectedURL
		resolvedCluster = detectedCluster
	}

	outputDir := output
	if outputDir == "" {
		outputDir = defaultOutputDir
	}

	cfg := &Config{
		BaseURL:         resolvedBaseURL,
		Cluster:         resolvedCluster,
		Credentials:     creds,
		OutputDir:       outputDir,
		SafeFailureMode: safeFail,
		Purpose:         purposeString,
		IncludeRawBody:  includeRawBody,
	}

	// --all skips the picker entirely (non-interactive: every site,
	// no TTY needed). Otherwise, when --site is not given, list the
	// account's sites and let the user pick a subset interactively
	// instead of silently collecting everything.
	var siteList []string
	if all {
		siteList, err = resolveAllSites(cfg)
	} else {
		siteList, err = resolveSites(cfg, sites)
	}
	if err != nil {
		return nil, err
	}
	cfg.Sites = siteList

	return cfg, nil
}

// loadTokenFile reads and parses a --token-file JSON document.
func loadTokenFile(path string) (*tokenFileContents, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var contents tokenFileContents
	if err := json.Unmarshal(data, &contents); err != nil {
		return nil, fmt.Errorf("parsing JSON: %w", err)
	}
	return &contents, nil
}

// saveTokenFile persists a refreshed token pair back to path,
// preserving the client_id/client_secret fields so the file remains a
// complete credential set.
func saveTokenFile(path string, creds Credentials) error {
	contents := tokenFileContents{
		ClientID:     creds.ClientID,
		ClientSecret: creds.ClientSecret,
		AccessToken:  creds.AccessToken,
		RefreshToken: creds.RefreshToken,
	}
	data, err := json.MarshalIndent(contents, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0600)
}

// TokenFileTemplate returns a skeleton --token-file JSON document.
func TokenFileTemplate() string {
	return `{
  "client_id": "your-api-gateway-client-id",
  "client_secret": "your-api-gateway-client-secret",
  "access_token": "your-access-token",
  "refresh_token": "your-refresh-token"
}
`
}

// envOrDefault returns the value of the environment variable
// named by key, or fallback if unset or empty.
func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
