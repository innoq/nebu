package admin

// oidc_directory.go — Story 14.2b: Gateway OIDC Directory Service + Cache + Rate Limit
//
// Security requirements (from _bmad-output/implementation-artifacts/security-guide-oidc-directory-2026-05-16.md):
//   CR-1: HTTPS-only — validate at init + each call; fail hard
//   CR-2: No redirect following — CheckRedirect: ErrUseLastResponse
//   CR-3: Bearer token never in logs — use secretString type
//   CR-4: 10 MB response size cap — io.LimitReader
//   CR-5: Rate limit keyed on verified session ID (not IP, not header)
//   HR-1: Admin-only access gate (enforced by caller — router middleware)
//   HR-2: SSRF trust boundary — Option B (documented below)
//   HR-3: Claim values are untrusted strings — length limits enforced
//   MR-1: Cache keyed on endpoint + token hash
//   MR-2: 10-second timeout on outbound call
//   MR-3: Explicit HTTP response status handling
//   MR-4: singleflight.Group for concurrent cache refresh coalescing
//
// SECURITY (HR-2): oidc_directory_endpoint is admin-configured and trusted.
// Private IP ranges are NOT blocked in this story. This means an admin with
// malicious intent can use this endpoint to probe internal services.
// Mitigated by: admin access requires valid OIDC + admin group claim.
// NOT mitigated against: compromised admin credentials.
// Follow-up: implement isPrivateIP guard in a future story (Option A from security guide).

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
	"golang.org/x/time/rate"
)

// secretString wraps a string bearer token so it is never printed in logs or error messages.
// CR-3 from security guide: "secrets in struct fields get printed by %+v".
type secretString string

// String returns a redacted placeholder — never the real value.
// This satisfies the fmt.Stringer interface and is used by slog, fmt.Sprintf, etc.
func (s secretString) String() string { return "[REDACTED]" }

// value returns the raw secret for use in HTTP Authorization headers only.
// Use this method exclusively when constructing the Authorization header.
// Do NOT pass the result to any logger or error formatter.
func (s secretString) value() string { return string(s) }

const (
	// maxDirectoryResponseBytes is the hard cap on the OIDC directory response body (CR-4).
	maxDirectoryResponseBytes = 10 * 1024 * 1024 // 10 MB

	// directoryCacheTTL is how long a fetched user list is considered fresh (AC1).
	directoryCacheTTL = 30 * time.Second

	// directoryRequestTimeout is the HTTP client timeout for outbound calls (MR-2).
	directoryRequestTimeout = 10 * time.Second

	// rateLimitPerSession is the max token rate per session (AC1, CR-5).
	rateLimitPerSession = rate.Limit(5) // 5 req/s

	// maxClaimValueLength enforces a max length on untrusted claim values (HR-3).
	maxClaimValueLength = 512
)

// OIDCDirectoryUser represents a single user entry from the OIDC directory response (HR-3).
// All fields are untrusted strings from an external source — callers must sanitize before
// using as Matrix localparts or inserting into HTML templates.
type OIDCDirectoryUser struct {
	Sub         string `json:"sub"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
}

// OIDCDirectoryConfig holds the configuration for OIDCDirectoryService.
type OIDCDirectoryConfig struct {
	// Endpoint is the HTTPS URL of the OIDC user directory. Must use HTTPS (CR-1).
	Endpoint string

	// BearerToken is the auth token for the OIDC directory endpoint.
	// Stored as secretString so it is never printed by accident (CR-3).
	BearerToken string

	// Enabled mirrors oidc_directory_enabled from the server config.
	// When false, FetchUsers returns an empty list immediately.
	Enabled bool

	// HTTPClient allows injection of a custom http.Client (used in tests with httptest.NewTLSServer).
	// When nil, a hardened default client is constructed (no redirect following, 10s timeout).
	HTTPClient *http.Client

	// Logger allows injection of a structured logger (used in tests to capture log output).
	// When nil, the package-level slog.Default() is used.
	Logger *slog.Logger
}

// directoryCacheEntry holds a fetched OIDC directory user list and its fetch timestamp.
type directoryCacheEntry struct {
	users     []OIDCDirectoryUser
	fetchedAt time.Time
}

// OIDCDirectoryService fetches the user list from the OIDC directory endpoint,
// caches results for 30 seconds, collapses concurrent refreshes via singleflight,
// and enforces per-session rate limiting.
type OIDCDirectoryService struct {
	endpoint    string
	token       secretString
	enabled     bool
	client      *http.Client
	logger      *slog.Logger
	cacheKey    string // SHA-256(endpoint + token) — MR-1
	sfGroup     singleflight.Group

	// cache
	cacheMu sync.RWMutex
	cache   *directoryCacheEntry

	// per-session rate limiters (CR-5)
	limiters sync.Map // map[string]*rate.Limiter
}

// NewOIDCDirectoryService creates a new OIDCDirectoryService from the given config.
// It does NOT validate the endpoint here — validation happens in FetchUsers (lazy,
// so tests can construct a service and call Validate/FetchUsers to trigger the error).
func NewOIDCDirectoryService(cfg OIDCDirectoryConfig) *OIDCDirectoryService {
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{
			Timeout: directoryRequestTimeout,
			// CR-2: never follow redirects — prevents HTTP→HTTPS bypass and SSRF via redirects.
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// MR-1: cache key = SHA-256(endpoint + "|" + token) — different tokens get different cache entries.
	h := sha256.Sum256([]byte(cfg.Endpoint + "|" + cfg.BearerToken))
	cacheKey := fmt.Sprintf("%x", h)

	return &OIDCDirectoryService{
		endpoint: cfg.Endpoint,
		token:    secretString(cfg.BearerToken),
		enabled:  cfg.Enabled,
		client:   client,
		logger:   logger,
		cacheKey: cacheKey,
	}
}

// validateEndpoint validates that the endpoint uses HTTPS (CR-1).
// Returns a descriptive error if validation fails.
func validateEndpoint(endpoint string) error {
	if endpoint == "" {
		return fmt.Errorf("oidc_directory_endpoint is empty")
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("oidc_directory_endpoint is not a valid URL: %w", err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("oidc_directory_endpoint must use HTTPS, got scheme %q in %q", u.Scheme, endpoint)
	}
	return nil
}

// Allow checks whether the given session ID is within the rate limit (CR-5, AC1).
// Returns true if the request is allowed, false if rate-limited.
// The session ID MUST be the verified session identifier from a validated JWT,
// not from a request header or IP address.
func (s *OIDCDirectoryService) Allow(sessionID string) bool {
	limiter := s.limiterFor(sessionID)
	return limiter.Allow()
}

// limiterFor returns (or creates) the rate.Limiter for a given session ID.
func (s *OIDCDirectoryService) limiterFor(sessionID string) *rate.Limiter {
	if v, ok := s.limiters.Load(sessionID); ok {
		return v.(*rate.Limiter)
	}
	// Burst of 5 to allow 5 requests at once before refilling at 5/s.
	lim := rate.NewLimiter(rateLimitPerSession, 5)
	actual, _ := s.limiters.LoadOrStore(sessionID, lim)
	return actual.(*rate.Limiter)
}

// FetchUsers returns the OIDC directory user list.
// It validates the endpoint (CR-1), checks the 30-second cache, and on a cache miss
// performs an outbound HTTP call with a bearer token (CR-2, CR-3, CR-4).
//
// Error propagation contract:
//   - Configuration errors (non-HTTPS endpoint, empty endpoint) are returned to the caller
//     as non-nil errors so callers can detect misconfiguration (AC3).
//   - Runtime fetch errors (unreachable host, non-200 response, JSON parse failure) are
//     swallowed: FetchUsers logs a warning and returns an empty list (AC2).
//
// Rate limiting is the caller's responsibility — call Allow(sessionID) before FetchUsers
// to enforce the per-session rate limit (CR-5). This separation allows rate-limit checks
// to short-circuit before cache or network access.
//
// The provided context is used for the outbound HTTP request when a cache miss occurs.
// Cancelling the context after a singleflight call has already started will not abort
// the in-flight request (singleflight.Do does not propagate per-caller context cancellation),
// but the 10-second client timeout still applies.
func (s *OIDCDirectoryService) FetchUsers(ctx context.Context) ([]OIDCDirectoryUser, error) {
	if !s.enabled {
		return []OIDCDirectoryUser{}, nil
	}

	// CR-1: validate HTTPS at each call (defensive — also checked at config time).
	if err := validateEndpoint(s.endpoint); err != nil {
		return nil, err
	}

	// Check cache first (read lock)
	s.cacheMu.RLock()
	if s.cache != nil && time.Since(s.cache.fetchedAt) < directoryCacheTTL {
		users := s.cache.users
		s.cacheMu.RUnlock()
		return users, nil
	}
	s.cacheMu.RUnlock()

	// Cache miss — use singleflight to collapse concurrent refreshes (MR-4).
	// We pass the caller context to doFetch. If multiple callers arrive simultaneously,
	// only the first goroutine's context is used for the HTTP call (singleflight limitation).
	v, err, _ := s.sfGroup.Do(s.cacheKey, func() (interface{}, error) {
		return s.doFetch(ctx)
	})
	if err != nil {
		// MR-3 / AC2: log warning and return empty list — do not propagate runtime errors.
		s.logger.Warn("oidc directory fetch failed — returning empty list",
			"host", hostOnly(s.endpoint),
			"err", err,
			// CR-3: never log the token; log only the host portion of the URL.
		)
		return []OIDCDirectoryUser{}, nil
	}

	users := v.([]OIDCDirectoryUser)
	return users, nil
}

// doFetch performs the actual HTTP call, reads the response (with size cap), parses JSON,
// and updates the cache.
// CR-2: uses s.client which has CheckRedirect: ErrUseLastResponse.
// CR-3: bearer token is passed only in the Authorization header — never logged.
// CR-4: io.LimitReader caps the response at maxDirectoryResponseBytes.
// MR-3: explicit status code handling.
func (s *OIDCDirectoryService) doFetch(ctx context.Context) ([]OIDCDirectoryUser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("constructing request: %w", err)
	}
	// CR-3: set Authorization header using the secret value accessor — never log this string.
	req.Header.Set("Authorization", "Bearer "+s.token.value())
	req.Header.Set("Accept", "application/json")

	// CR-3: log only method and host — never URL path (may contain sensitive params) or token.
	s.logger.Debug("oidc directory call", "method", "GET", "host", req.URL.Host)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http call: %w", err)
	}
	defer resp.Body.Close()

	// MR-3: explicit status code handling.
	switch {
	case resp.StatusCode == http.StatusOK:
		// continue to parse
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, fmt.Errorf("oidc directory auth failed (HTTP %d) — check bearer token configuration", resp.StatusCode)
	case resp.StatusCode == http.StatusNotFound:
		return nil, fmt.Errorf("oidc directory endpoint not found (HTTP 404)")
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, fmt.Errorf("oidc directory provider rate-limited this gateway (HTTP 429)")
	case resp.StatusCode >= 500:
		return nil, fmt.Errorf("oidc directory provider error (HTTP %d)", resp.StatusCode)
	default:
		return nil, fmt.Errorf("oidc directory unexpected response (HTTP %d)", resp.StatusCode)
	}

	// CR-4: cap response at 10 MB to prevent OOM.
	limited := io.LimitReader(resp.Body, maxDirectoryResponseBytes)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("reading oidc directory response: %w", err)
	}
	if int64(len(body)) == maxDirectoryResponseBytes {
		s.logger.Warn("oidc directory response truncated at limit",
			"host", hostOnly(s.endpoint),
			"limit_bytes", maxDirectoryResponseBytes,
		)
		// Graceful degradation: attempt to parse whatever was received, return empty on failure.
	}

	var users []OIDCDirectoryUser
	if err := json.Unmarshal(body, &users); err != nil {
		return nil, fmt.Errorf("parsing oidc directory response JSON: %w", err)
	}

	// HR-3: enforce length limits on untrusted claim values.
	sanitized := make([]OIDCDirectoryUser, 0, len(users))
	for _, u := range users {
		u.Sub = truncate(u.Sub, maxClaimValueLength)
		u.DisplayName = truncate(u.DisplayName, maxClaimValueLength)
		u.Email = truncate(u.Email, maxClaimValueLength)
		sanitized = append(sanitized, u)
	}

	// Update cache (write lock).
	s.cacheMu.Lock()
	s.cache = &directoryCacheEntry{users: sanitized, fetchedAt: time.Now()}
	s.cacheMu.Unlock()

	return sanitized, nil
}

// hostOnly returns only the host portion of a URL for safe log output.
// This prevents logging URL paths that may contain sensitive path segments.
func hostOnly(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "[invalid URL]"
	}
	return u.Host
}

// truncate clips a string to at most maxLen bytes (HR-3).
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
