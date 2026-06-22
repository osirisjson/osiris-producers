// sso.go - AWS IAM Identity Center (SSO) setup for the OSIRIS JSON producer.
// Implements the device authorization flow to discover all accounts and roles
// available to the user RBAC, then generates ~/.aws/config profiles automatically.
//
// Usage:
//
//	osirisjson-producer aws setup-sso --start-url https://myorg.awsapps.com/start --region eu-west-1
//
// This eliminates the need to manually run "aws configure sso" for each
// account. A single invocation discovers all accounts and roles, writes
// the config, and leaves the user ready to collect and generate OSIRIS JSON document.
//
// For an introduction to OSIRIS JSON Producer for Amazon Web Services see:
// [OSIRIS-JSON-AWS]: https://osirisjson.org/en/docs/producers/hyperscalers/amazon-aws

package aws

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sso"
	ssotypes "github.com/aws/aws-sdk-go-v2/service/sso/types"
	"github.com/aws/aws-sdk-go-v2/service/ssooidc"
)

const ssoClientName = "osirisjson-producer-aws"

// ssoAccount holds AWS account info with its available roles.
type ssoAccount struct {
	AccountID   string
	AccountName string
	Roles       []string
}

// runSetupSSO implements the "setup-sso" subcommand.
func runSetupSSO(args []string) error {
	var startURL, ssoRegion string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--start-url":
			if i+1 < len(args) {
				i++
				startURL = args[i]
			}
		case "--region":
			if i+1 < len(args) {
				i++
				ssoRegion = args[i]
			}
		case "--help", "-h":
			printSetupSSOHelp()
			return nil
		}
	}

	if startURL == "" {
		printSetupSSOHelp()
		return fmt.Errorf("--start-url is required")
	}

	ctx := context.Background()

	// Auto-detect SSO region if not provided.
	if ssoRegion == "" {
		fmt.Fprintf(os.Stderr, "No --region specified, detecting SSO region...\n")
		detected, err := detectSSORegion(ctx, startURL)
		if err != nil {
			return fmt.Errorf("could not auto-detect SSO region: %w\n\nSpecify it manually with --region <region>", err)
		}
		ssoRegion = detected
		fmt.Fprintf(os.Stderr, "Detected SSO region: %s\n", ssoRegion)
	}

	// Load a minimal AWS config for the SSO region (no credentials needed yet).
	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(ssoRegion),
	)
	if err != nil {
		return fmt.Errorf("loading AWS config: %w", err)
	}

	// Step 1: Register OIDC client.
	oidcClient := ssooidc.NewFromConfig(cfg)

	fmt.Fprintf(os.Stderr, "Registering OIDC client...\n")
	regOut, err := oidcClient.RegisterClient(ctx, &ssooidc.RegisterClientInput{
		ClientName: strPtr(ssoClientName),
		ClientType: strPtr("public"),
	})
	if err != nil {
		return fmt.Errorf("registering OIDC client: %w", err)
	}

	// Step 2: Start device authorization.
	fmt.Fprintf(os.Stderr, "Starting device authorization...\n")
	authOut, err := oidcClient.StartDeviceAuthorization(ctx, &ssooidc.StartDeviceAuthorizationInput{
		ClientId:     regOut.ClientId,
		ClientSecret: regOut.ClientSecret,
		StartUrl:     &startURL,
	})
	if err != nil {
		return fmt.Errorf("starting device authorization: %w", err)
	}

	// Step 3: Open browser for user to authorize.
	verifyURL := derefStr(authOut.VerificationUriComplete)
	if verifyURL == "" {
		verifyURL = derefStr(authOut.VerificationUri)
	}

	fmt.Fprintf(os.Stderr, "\nAuthorize this device:\n")
	if authOut.UserCode != nil {
		fmt.Fprintf(os.Stderr, "  Code: %s\n", *authOut.UserCode)
	}
	fmt.Fprintf(os.Stderr, "  URL:  %s\n\n", verifyURL)

	if err := openBrowser(verifyURL); err != nil {
		fmt.Fprintf(os.Stderr, "Could not open browser automatically.\nPlease open the URL above manually in your browser.\n\n")
	} else {
		fmt.Fprintf(os.Stderr, "Browser opened. Complete authorization, then return here.\n\n")
	}

	// Step 4: Poll for token.
	pollInterval := time.Duration(authOut.Interval) * time.Second
	if pollInterval < time.Second {
		pollInterval = time.Second
	}
	deadline := time.Now().Add(time.Duration(authOut.ExpiresIn) * time.Second)

	grantType := "urn:ietf:params:oauth:grant-type:device_code"
	var accessToken string
	var tokenExpiresIn int32

	fmt.Fprintf(os.Stderr, "Waiting for authorization...")
	for time.Now().Before(deadline) {
		time.Sleep(pollInterval)

		tokenOut, err := oidcClient.CreateToken(ctx, &ssooidc.CreateTokenInput{
			ClientId:     regOut.ClientId,
			ClientSecret: regOut.ClientSecret,
			GrantType:    &grantType,
			DeviceCode:   authOut.DeviceCode,
		})
		if err != nil {
			// "authorization_pending" means user hasn't approved yet - keep polling.
			if strings.Contains(err.Error(), "AuthorizationPendingException") ||
				strings.Contains(err.Error(), "authorization_pending") {
				fmt.Fprint(os.Stderr, ".")
				continue
			}
			// "slow_down" means increase interval.
			if strings.Contains(err.Error(), "SlowDownException") ||
				strings.Contains(err.Error(), "slow_down") {
				pollInterval += time.Second
				fmt.Fprint(os.Stderr, ".")
				continue
			}
			return fmt.Errorf("creating token: %w", err)
		}

		accessToken = derefStr(tokenOut.AccessToken)
		tokenExpiresIn = tokenOut.ExpiresIn
		break
	}
	fmt.Fprintln(os.Stderr)

	if accessToken == "" {
		return fmt.Errorf("authorization timed out - please try again")
	}
	fmt.Fprintf(os.Stderr, "Authorization successful.\n\n")

	// Cache the SSO token so profiles work immediately (same format as "aws sso login").
	if err := cacheSSOToken(startURL, ssoRegion, accessToken, tokenExpiresIn); err != nil {
		fmt.Fprintf(os.Stderr, "  warning: could not cache SSO token: %v\n", err)
		fmt.Fprintf(os.Stderr, "  You may need to run: aws sso login --profile <profile>\n\n")
	}

	// Step 5: List all accounts and roles.
	ssoClient := sso.NewFromConfig(cfg)

	accounts, err := listAllAccounts(ctx, ssoClient, accessToken)
	if err != nil {
		return fmt.Errorf("listing accounts: %w", err)
	}
	if len(accounts) == 0 {
		return fmt.Errorf("no AWS accounts found for this SSO user")
	}

	fmt.Fprintf(os.Stderr, "Discovered %d accounts. Fetching roles...\n", len(accounts))

	var ssoAccounts []ssoAccount
	for i, acct := range accounts {
		// Throttle requests to prevent 429 TooManyRequests from SSO API.
		if i > 0 && i%10 == 0 {
			time.Sleep(500 * time.Millisecond)
		}

		roles, err := listAccountRolesWithRetry(ctx, ssoClient, accessToken, derefStr(acct.AccountId))
		if err != nil {
			fmt.Fprintf(os.Stderr, "  warning: could not list roles for %s: %v\n",
				derefStr(acct.AccountId), err)
			continue
		}
		sa := ssoAccount{
			AccountID:   derefStr(acct.AccountId),
			AccountName: derefStr(acct.AccountName),
		}
		for _, r := range roles {
			sa.Roles = append(sa.Roles, derefStr(r.RoleName))
		}
		sort.Strings(sa.Roles)
		ssoAccounts = append(ssoAccounts, sa)
	}

	sort.Slice(ssoAccounts, func(i, j int) bool {
		return ssoAccounts[i].AccountName < ssoAccounts[j].AccountName
	})

	// Step 6: Generate config profiles.
	profiles := generateSSOProfiles(ssoAccounts, startURL, ssoRegion)

	fmt.Fprintf(os.Stderr, "\nReady to write %d profiles to ~/.aws/config.\n", len(profiles))
	fmt.Fprintf(os.Stderr, "Existing profiles will NOT be overwritten.\n")
	fmt.Fprintf(os.Stderr, "\nProceed? [Y/n]: ")

	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer != "" && answer != "y" && answer != "yes" {
		fmt.Fprintf(os.Stderr, "Aborted.\n")
		return nil
	}

	written, skipped, err := writeSSOConfig(profiles)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "\nDone. %d profiles written, %d skipped (already exist).\n", written, skipped)
	fmt.Fprintf(os.Stderr, "\nSSO session is active. You can start generating OSIRIS JSON documents:\n")
	fmt.Fprintf(os.Stderr, "  osirisjson-producer aws                                         (interactive)\n")
	fmt.Fprintf(os.Stderr, "  osirisjson-producer aws --profile <name> --all-regions -o out   (single account)\n")
	fmt.Fprintf(os.Stderr, "  osirisjson-producer aws -s accounts.csv -o out                  (batch CSV)\n")
	fmt.Fprintf(os.Stderr, "\nWhen the session expires, re-login with:\n")
	fmt.Fprintf(os.Stderr, "  aws sso login --profile <any-profile>\n")

	return nil
}

// listAllAccounts paginates through all SSO accounts.
func listAllAccounts(ctx context.Context, client *sso.Client, token string) ([]ssotypes.AccountInfo, error) {
	var all []ssotypes.AccountInfo
	var nextToken *string

	for {
		out, err := client.ListAccounts(ctx, &sso.ListAccountsInput{
			AccessToken: &token,
			NextToken:   nextToken,
		})
		if err != nil {
			return nil, err
		}
		all = append(all, out.AccountList...)
		if out.NextToken == nil {
			break
		}
		nextToken = out.NextToken
	}
	return all, nil
}

// listAccountRoles paginates through all roles for a single account.
func listAccountRoles(ctx context.Context, client *sso.Client, token, accountID string) ([]ssotypes.RoleInfo, error) {
	var all []ssotypes.RoleInfo
	var nextToken *string

	for {
		out, err := client.ListAccountRoles(ctx, &sso.ListAccountRolesInput{
			AccessToken: &token,
			AccountId:   &accountID,
			NextToken:   nextToken,
		})
		if err != nil {
			return nil, err
		}
		all = append(all, out.RoleList...)
		if out.NextToken == nil {
			break
		}
		nextToken = out.NextToken
	}
	return all, nil
}

// listAccountRolesWithRetry wraps listAccountRoles with retry and backoff
// for 429 TooManyRequests errors from the SSO API.
func listAccountRolesWithRetry(ctx context.Context, client *sso.Client, token, accountID string) ([]ssotypes.RoleInfo, error) {
	backoff := time.Second
	for attempt := 0; attempt < 5; attempt++ {
		roles, err := listAccountRoles(ctx, client, token, accountID)
		if err == nil {
			return roles, nil
		}
		if !strings.Contains(err.Error(), "TooManyRequests") &&
			!strings.Contains(err.Error(), "429") {
			return nil, err
		}
		time.Sleep(backoff)
		backoff *= 2
	}
	return listAccountRoles(ctx, client, token, accountID)
}

// ssoProfile holds the data for a single ~/.aws/config [profile ...] section.
type ssoProfile struct {
	Name      string // profile name: AccountName_RoleName
	AccountID string
	RoleName  string
	StartURL  string
	SSORegion string
}

// generateSSOProfiles creates profile entries for all account/role combinations.
func generateSSOProfiles(accounts []ssoAccount, startURL, ssoRegion string) []ssoProfile {
	var profiles []ssoProfile
	for _, acct := range accounts {
		for _, role := range acct.Roles {
			name := sanitizeProfileName(acct.AccountName) + "_" + role
			profiles = append(profiles, ssoProfile{
				Name:      name,
				AccountID: acct.AccountID,
				RoleName:  role,
				StartURL:  startURL,
				SSORegion: ssoRegion,
			})
		}
	}
	return profiles
}

// sanitizeProfileName replaces spaces and special characters with dashes.
func sanitizeProfileName(s string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, s)
}

// writeSSOConfig appends new profiles to ~/.aws/config, skipping any that
// already exist. Returns the number written and skipped.
func writeSSOConfig(profiles []ssoProfile) (written, skipped int, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return 0, 0, fmt.Errorf("finding home directory: %w", err)
	}

	configDir := filepath.Join(home, ".aws")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return 0, 0, fmt.Errorf("creating %s: %w", configDir, err)
	}

	configPath := filepath.Join(configDir, "config")

	// Read existing config to detect existing profiles.
	existing := map[string]bool{}
	if data, err := os.ReadFile(configPath); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "[profile ") && strings.HasSuffix(line, "]") {
				name := strings.TrimSpace(line[len("[profile ") : len(line)-1])
				existing[name] = true
			} else if line == "[default]" {
				existing["default"] = true
			}
		}
	}

	// Build new sections.
	var buf strings.Builder
	for _, p := range profiles {
		if existing[p.Name] {
			skipped++
			continue
		}
		buf.WriteString(fmt.Sprintf("\n[profile %s]\n", p.Name))
		buf.WriteString(fmt.Sprintf("sso_start_url = %s\n", p.StartURL))
		buf.WriteString(fmt.Sprintf("sso_region = %s\n", p.SSORegion))
		buf.WriteString(fmt.Sprintf("sso_account_id = %s\n", p.AccountID))
		buf.WriteString(fmt.Sprintf("sso_role_name = %s\n", p.RoleName))
		buf.WriteString(fmt.Sprintf("region = %s\n", p.SSORegion))
		written++
	}

	if written == 0 {
		return 0, skipped, nil
	}

	f, err := os.OpenFile(configPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return 0, 0, fmt.Errorf("opening %s: %w", configPath, err)
	}
	defer f.Close()

	if _, err := f.WriteString(buf.String()); err != nil {
		return 0, 0, fmt.Errorf("writing profiles: %w", err)
	}

	return written, skipped, nil
}

// cacheSSOToken writes the access token to ~/.aws/sso/cache/ in the same
// format the AWS CLI uses, so that SSO profiles work immediately without
// requiring a separate "aws sso login" step.
//
// It caches the token under two keys:
//   - SHA1(startUrl) - for legacy-format profiles (sso_start_url inline)
//   - SHA1(sessionName) - for sso-session-format profiles, if any session
//     referencing this startURL is found in ~/.aws/config
//
// This ensures both profile formats can find the cached token.
func cacheSSOToken(startURL, ssoRegion, accessToken string, expiresIn int32) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	cacheDir := filepath.Join(home, ".aws", "sso", "cache")
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		return err
	}

	expiresAt := time.Now().Add(time.Duration(expiresIn) * time.Second)

	token := map[string]string{
		"startUrl":    startURL,
		"region":      ssoRegion,
		"accessToken": accessToken,
		"expiresAt":   expiresAt.UTC().Format(time.RFC3339),
	}

	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return err
	}

	// Cache under SHA1(startUrl) for legacy-format profiles.
	h := sha1.Sum([]byte(startURL))
	filename := hex.EncodeToString(h[:]) + ".json"
	if err := os.WriteFile(filepath.Join(cacheDir, filename), data, 0600); err != nil {
		return err
	}

	// Also cache under SHA1(sessionName) for sso-session-format profiles.
	// The AWS CLI v2 uses [sso-session <name>] blocks; when a profile
	// references sso_session = <name>, the SDK looks for SHA1(name).json.
	sessionNames := findSSOSessionNames(home, startURL)
	for _, name := range sessionNames {
		sh := sha1.Sum([]byte(name))
		sessionFile := hex.EncodeToString(sh[:]) + ".json"
		_ = os.WriteFile(filepath.Join(cacheDir, sessionFile), data, 0600)
	}

	return nil
}

// findSSOSessionNames scans ~/.aws/config for [sso-session <name>] sections
// whose sso_start_url matches the given startURL and returns the session names.
func findSSOSessionNames(home, startURL string) []string {
	configPath := filepath.Join(home, ".aws", "config")
	f, err := os.Open(configPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	var sessions []string
	scanner := bufio.NewScanner(f)

	var currentSession string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Detect [sso-session <name>] sections.
		if strings.HasPrefix(line, "[sso-session ") && strings.HasSuffix(line, "]") {
			currentSession = strings.TrimSpace(line[len("[sso-session ") : len(line)-1])
			continue
		}

		// Any other section header resets.
		if strings.HasPrefix(line, "[") {
			currentSession = ""
			continue
		}

		// Inside a sso-session section, check for matching start URL.
		if currentSession != "" && strings.HasPrefix(line, "sso_start_url") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				val := strings.TrimSpace(parts[1])
				if val == startURL {
					sessions = append(sessions, currentSession)
				}
			}
			currentSession = ""
		}
	}
	return sessions
}

// ssoDetectRegions lists regions to probe when auto-detecting the SSO instance.
// Ordered by likelihood - most common SSO deployments first.
var ssoDetectRegions = []string{
	"us-east-1", "eu-west-1", "eu-central-1", "us-west-2",
	"ap-southeast-1", "ap-northeast-1", "eu-west-2", "us-east-2",
	"ap-south-1", "ap-southeast-2", "ca-central-1", "eu-north-1",
	"sa-east-1", "eu-west-3", "us-west-1", "ap-northeast-2", "ap-northeast-3",
}

// detectSSORegion tries to register an OIDC client in each candidate region.
// The first region where StartDeviceAuthorization succeeds (meaning the SSO
// instance is actually there) is returned. The registered client is discarded
// since we'll register again in the main flow.
func detectSSORegion(ctx context.Context, startURL string) (string, error) {
	for _, region := range ssoDetectRegions {
		cfg, err := awsconfig.LoadDefaultConfig(ctx,
			awsconfig.WithRegion(region),
		)
		if err != nil {
			continue
		}

		oidcClient := ssooidc.NewFromConfig(cfg)
		regOut, err := oidcClient.RegisterClient(ctx, &ssooidc.RegisterClientInput{
			ClientName: strPtr(ssoClientName),
			ClientType: strPtr("public"),
		})
		if err != nil {
			continue
		}

		_, err = oidcClient.StartDeviceAuthorization(ctx, &ssooidc.StartDeviceAuthorizationInput{
			ClientId:     regOut.ClientId,
			ClientSecret: regOut.ClientSecret,
			StartUrl:     &startURL,
		})
		if err == nil {
			return region, nil
		}
	}
	return "", fmt.Errorf("no SSO instance found for %s in any region", startURL)
}

// openBrowser tries to open a URL in the user's default browser.
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return fmt.Errorf("unsupported platform %s", runtime.GOOS)
	}
	return cmd.Start()
}

func strPtr(s string) *string { return &s }

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func printSetupSSOHelp() {
	fmt.Print(`osirisjson-producer aws setup-sso - Configure AWS SSO profiles

Discovers all AWS accounts and roles available through IAM Identity Center
(SSO) and writes them as profiles to ~/.aws/config. This eliminates the need
to manually configure each account.

Usage:
  osirisjson-producer aws setup-sso --start-url <URL> [--region <region>]

Required flags:
  --start-url    IAM Identity Center start URL (e.g. https://myorg.awsapps.com/start)

Optional flags:
  --region       AWS region where IAM Identity Center is configured
                 (auto-detected if not specified)

The command will:
  1. Open your browser for SSO login
  2. Discover all available accounts and roles
  3. Write profiles to ~/.aws/config (existing profiles are not overwritten)

Profile names follow the pattern: <AccountName>_<RoleName>

After setup, authenticate with:
  aws sso login --profile <any-profile>

Then run the OSIRIS JSON producer:
  osirisjson-producer aws                                        (interactive)
  osirisjson-producer aws --profile <name> --all-regions -o out  (single account)
  osirisjson-producer aws -s accounts.csv -o out                 (batch CSV)
`)
}
