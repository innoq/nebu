package admin

// scim_client.go — Story 14-3c: SCIM 2.0 User Fetch + Progress Tracking
//
// Implements the SCIMClient which fetches all users from a SCIM 2.0 /Users endpoint
// using RFC 7644 pagination (startIndex, count, totalResults) and maps them to
// OIDCDirectoryUser for bulk import.
//
// Security requirements (from _bmad-output/implementation-artifacts/security-guide-scim-2026-05-16.md):
//   CR-1: bearer token never in logs — secretString type from oidc_directory.go
//   CR-2: HTTPS-only — validateEndpoint() reused, validated at FetchUsers() call
//   CR-4: 5 MB response size cap per page — io.LimitReader
//   HR-1: 100,000 user total cap — abort with error when exceeded
//   HR-2: SSRF trust boundary — Option B (documented below)
//   HR-3: untrusted string fields — truncate() enforced
//
// SECURITY (HR-2): scim_base_url is admin-configured and trusted.
// Private IP ranges are NOT blocked. Rationale: same threat model as oidc_directory.go —
// an admin with malicious intent can probe internal services. Mitigated by: admin access
// requires valid OIDC + admin group claim. NOT mitigated against: compromised admin credentials.
// Follow-up: implement isPrivateIP guard in a future story.
//
// RFC 7644 pagination:
//   - startIndex is 1-based (first page: startIndex=1)
//   - count is the requested page size (we use 100)
//   - totalResults may be -1 or 0 when not known; we treat ≤0 as "unknown"
//   - iteration terminates when the returned Resources slice is empty

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	// scimPageSize is the number of users requested per SCIM page (RFC 7644 §3.4.2.4).
	scimPageSize = 100

	// scimMaxTotalUsers is the hard cap on total users we will import (HR-1).
	// Abort with an error if a SCIM server returns more than this many users.
	scimMaxTotalUsers = 100_000

	// scimMaxResponseBytes is the hard cap on a single SCIM page response (CR-4).
	// 5 MB per page; pages are small (100 users) so this is very conservative.
	scimMaxResponseBytes = 5 * 1024 * 1024 // 5 MB

	// scimRequestTimeout is the HTTP client timeout for outbound SCIM calls.
	// Uses the same constant as the OIDC directory service (directoryRequestTimeout = 10s).
	// Reused here via constant alias for readability — same value.
	scimRequestTimeout = directoryRequestTimeout // 10s
)

// scimListResponse is the RFC 7644 §3.4.2 ListResponse schema.
// Only the fields we need are decoded; extra SCIM fields are ignored.
type scimListResponse struct {
	TotalResults int              `json:"totalResults"`
	StartIndex   int              `json:"startIndex"`
	ItemsPerPage int              `json:"itemsPerPage"`
	Resources    []scimUserRecord `json:"Resources"`
}

// scimUserRecord is a single SCIM User resource (RFC 7643 §4.1).
// We extract sub-equivalent (id or userName), displayName, and primary email.
type scimUserRecord struct {
	ID          string          `json:"id"`
	UserName    string          `json:"userName"`
	DisplayName string          `json:"displayName"`
	Emails      []scimEmailAttr `json:"emails"`
}

// scimEmailAttr is a SCIM multi-valued attribute entry for emails (RFC 7643 §2.4).
type scimEmailAttr struct {
	Value   string `json:"value"`
	Type    string `json:"type"`    // "work" | "home" | ""
	Primary bool   `json:"primary"` // true = primary address
}

// primaryEmail returns the primary email from a scimUserRecord's Emails list.
// Preference: primary=true first, then first entry, then "".
func (r *scimUserRecord) primaryEmail() string {
	for _, e := range r.Emails {
		if e.Primary {
			return e.Value
		}
	}
	if len(r.Emails) > 0 {
		return r.Emails[0].Value
	}
	return ""
}

// scimSub returns the stable identifier used as OIDCDirectoryUser.Sub.
// Preference: userName → id → "".
// userName is human-readable and matches OIDC "sub" semantics for SCIM providers
// that use email addresses as userNames (e.g. Azure AD, Okta).
// The id field (opaque UUID-like) is a fallback when userName is absent.
func (r *scimUserRecord) scimSub() string {
	if r.UserName != "" {
		return r.UserName
	}
	return r.ID
}

// SCIMClientConfig holds configuration for SCIMClient.
// All fields correspond to the server_config rows added in migration 000049.
type SCIMClientConfig struct {
	// BaseURL is the HTTPS URL of the SCIM 2.0 /Users endpoint.
	// Must use HTTPS (CR-2). Example: "https://idp.corp.com/scim/v2/Users"
	BaseURL string

	// BearerToken is the SCIM auth token.
	// Stored as secretString so it is never printed by accident (CR-1).
	BearerToken string

	// Enabled mirrors scim_enabled from the server config.
	// When false, FetchUsers returns an empty list immediately.
	Enabled bool

	// HTTPClient allows injection of a custom http.Client (used in tests with httptest.NewTLSServer).
	// When nil, a hardened default client is constructed (no redirect following, 10s timeout).
	HTTPClient *http.Client

	// Logger allows injection of a structured logger (used in tests to capture log output).
	// When nil, the package-level slog.Default() is used.
	Logger *slog.Logger
}

// SCIMClient fetches users from a SCIM 2.0 /Users endpoint using paginated GET requests.
// It implements the SCIMFetcher interface defined in bootstrap.go.
type SCIMClient struct {
	baseURL string
	token   secretString // CR-1: never logged
	enabled bool
	client  *http.Client
	logger  *slog.Logger
}

// NewSCIMClient creates a new SCIMClient from the given config.
// Endpoint validation (HTTPS check) happens lazily in FetchUsers, not here,
// so tests can construct a client and explicitly call FetchUsers to observe the error.
func NewSCIMClient(cfg SCIMClientConfig) *SCIMClient {
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{
			Timeout: scimRequestTimeout,
			// CR-2 / mirrors oidc_directory.go: never follow redirects — prevents HTTP→HTTPS bypass.
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return &SCIMClient{
		baseURL: cfg.BaseURL,
		token:   secretString(cfg.BearerToken),
		enabled: cfg.Enabled,
		client:  client,
		logger:  logger,
	}
}

// IsEnabled reports whether SCIM integration is enabled.
// Satisfies the SCIMFetcher interface.
func (c *SCIMClient) IsEnabled() bool { return c.enabled }

// FetchUsers fetches all users from the SCIM endpoint using paginated requests.
// Pages are fetched sequentially (RFC 7644 §3.4.2.4 startIndex/count).
//
// Security:
//   - CR-2: validateEndpoint() enforces HTTPS before any HTTP call.
//   - CR-1: bearer token is only in Authorization header, never logged.
//   - CR-4: io.LimitReader caps each page at scimMaxResponseBytes (5 MB).
//   - HR-1: returns error when totalUsers > scimMaxTotalUsers (100k).
//   - HR-3: truncate() applied to all string fields.
//
// Returns (nil, err) for configuration or hard limit errors.
// Returns ([]OIDCDirectoryUser{}, nil) when enabled=false or SCIM server is unreachable.
func (c *SCIMClient) FetchUsers(ctx context.Context) ([]OIDCDirectoryUser, error) {
	if !c.enabled {
		return []OIDCDirectoryUser{}, nil
	}

	// CR-2: validate HTTPS before any outbound call.
	if err := validateEndpoint(c.baseURL); err != nil {
		return nil, fmt.Errorf("scim_base_url: %w", err)
	}

	var allUsers []OIDCDirectoryUser
	startIndex := 1 // RFC 7644: 1-based

	for {
		// Build paginated URL: ?startIndex=N&count=100
		pageURL, err := c.buildPageURL(startIndex)
		if err != nil {
			return nil, fmt.Errorf("building SCIM page URL: %w", err)
		}

		resp, err := c.fetchPage(ctx, pageURL)
		if err != nil {
			return nil, fmt.Errorf("SCIM page fetch (startIndex=%d): %w", startIndex, err)
		}

		// Empty Resources = end of results (RFC 7644).
		if len(resp.Resources) == 0 {
			break
		}

		// HR-1: cap check — abort when totalResults or running count exceeds the limit.
		// Check totalResults first (fast early-exit when the SCIM server reports the total).
		if resp.TotalResults > scimMaxTotalUsers {
			return nil, fmt.Errorf(
				"SCIM server reports totalResults=%d which exceeds maximum user cap (%d): aborting import (HR-1)",
				resp.TotalResults,
				scimMaxTotalUsers,
			)
		}
		if len(allUsers)+len(resp.Resources) > scimMaxTotalUsers {
			return nil, fmt.Errorf(
				"SCIM response exceeds maximum user cap (%d): aborting import (HR-1)",
				scimMaxTotalUsers,
			)
		}

		for _, r := range resp.Resources {
			// HR-3: truncate untrusted strings.
			sub := truncate(r.scimSub(), maxClaimValueLength)
			displayName := truncate(r.DisplayName, maxClaimValueLength)
			email := truncate(r.primaryEmail(), maxClaimValueLength)

			allUsers = append(allUsers, OIDCDirectoryUser{
				Sub:         sub,
				DisplayName: displayName,
				Email:       email,
			})
		}

		// Advance to the next page.
		startIndex += len(resp.Resources)

		// Termination guard: if we got fewer results than requested, we're done.
		if len(resp.Resources) < scimPageSize {
			break
		}
	}

	c.logger.Info("scim fetch complete",
		"host", hostOnly(c.baseURL),
		"total_users", len(allUsers),
	)

	return allUsers, nil
}

// buildPageURL constructs the SCIM /Users page URL with startIndex and count query params.
// scim_base_url is the SCIM service root (e.g. "https://idp.com/scim/v2").
// The client appends "/Users" before adding query params.
// If scim_base_url already ends with "/Users", no double-slash is added.
func (c *SCIMClient) buildPageURL(startIndex int) (string, error) {
	base, err := url.Parse(c.baseURL)
	if err != nil {
		return "", fmt.Errorf("invalid scim_base_url: %w", err)
	}

	// Append /Users to the base path if not already present.
	if !strings.HasSuffix(base.Path, "/Users") {
		base.Path = strings.TrimRight(base.Path, "/") + "/Users"
	}

	q := base.Query()
	q.Set("startIndex", strconv.Itoa(startIndex))
	q.Set("count", strconv.Itoa(scimPageSize))
	base.RawQuery = q.Encode()
	return base.String(), nil
}

// fetchPage performs a single GET request to the SCIM endpoint and returns the parsed response.
func (c *SCIMClient) fetchPage(ctx context.Context, pageURL string) (*scimListResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("constructing SCIM request: %w", err)
	}

	// CR-1: set Authorization header using the secret value accessor — never log this string.
	req.Header.Set("Authorization", "Bearer "+c.token.value())
	req.Header.Set("Accept", "application/scim+json, application/json")

	// CR-1: log only host — never the full URL (may contain startIndex but not the token).
	c.logger.Debug("scim page fetch", "host", req.URL.Host, "startIndex", req.URL.Query().Get("startIndex"))

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http call: %w", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK:
		// continue
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, fmt.Errorf("SCIM auth failed (HTTP %d) — check scim_bearer_token configuration", resp.StatusCode)
	case resp.StatusCode == http.StatusNotFound:
		return nil, fmt.Errorf("SCIM Users endpoint not found (HTTP 404) — check scim_base_url")
	case resp.StatusCode >= 500:
		return nil, fmt.Errorf("SCIM provider error (HTTP %d)", resp.StatusCode)
	default:
		return nil, fmt.Errorf("SCIM unexpected response (HTTP %d)", resp.StatusCode)
	}

	// CR-4: cap response at scimMaxResponseBytes per page.
	limited := io.LimitReader(resp.Body, scimMaxResponseBytes)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("reading SCIM response: %w", err)
	}
	if int64(len(body)) == scimMaxResponseBytes {
		return nil, fmt.Errorf("SCIM page response exceeded %d byte cap — possible oversized payload", scimMaxResponseBytes)
	}

	var listResp scimListResponse
	if err := json.Unmarshal(body, &listResp); err != nil {
		return nil, fmt.Errorf("parsing SCIM JSON response: %w", err)
	}

	return &listResp, nil
}
