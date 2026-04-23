package matrix

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// ── SSO state store ───────────────────────────────────────────────────────────

// ssoStateEntry holds the PKCE verifier and client redirectUrl for one SSO flow.
// Keyed by the opaque `state` value sent to the IdP.
type ssoStateEntry struct {
	verifier    string
	redirectURL string
	exp         time.Time
}

// ssoStateStore is a short-lived in-memory store for pending SSO states.
// FluffyChat (and most Matrix clients) make the initial redirect request
// themselves and then open the Location URL in a browser — which means any
// cookie set on the initial response is never sent back by the browser on the
// callback. Server-side state storage avoids this cross-process cookie problem.
type ssoStateStore struct {
	mu    sync.Mutex
	store map[string]ssoStateEntry
}

var globalSSOState = &ssoStateStore{store: make(map[string]ssoStateEntry)}

// ssoStateStoreMaxEntries caps the number of pending SSO states to prevent
// unbounded memory growth from unauthenticated requests (Story 5.21, AC 5).
const ssoStateStoreMaxEntries = 10_000

func (s *ssoStateStore) save(state, verifier, redirectURL string, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	// Expire stale entries before checking capacity so legitimate flows are not
	// blocked by entries that have already timed out.
	for k, v := range s.store {
		if now.After(v.exp) {
			delete(s.store, k)
		}
	}
	if len(s.store) >= ssoStateStoreMaxEntries {
		return errors.New("sso state store full")
	}
	s.store[state] = ssoStateEntry{
		verifier:    verifier,
		redirectURL: redirectURL,
		exp:         now.Add(ttl),
	}
	return nil
}

func (s *ssoStateStore) pop(state string) (ssoStateEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.store[state]
	if !ok {
		return ssoStateEntry{}, false
	}
	delete(s.store, state)
	if time.Now().After(entry.exp) {
		return ssoStateEntry{}, false
	}
	return entry, true
}

// ── Login token store (MAJOR-2 fix) ──────────────────────────────────────────

// loginTokenTTL is the lifetime of a short-lived opaque loginToken.
// 30 seconds matches the original design intent and minimises the replay window
// for a token that passes through browser history, Referer headers, and proxy logs.
const loginTokenTTL = 30 * time.Second

// loginTokenEntry maps a short-lived opaque loginToken to the real OIDC id_token.
// TTL is 30 seconds — single-use (popped on first read).
type loginTokenEntry struct {
	idToken string
	exp     time.Time
}

type loginTokenStore struct {
	mu    sync.Mutex
	store map[string]loginTokenEntry
}

// globalLoginTokens is the package-level store shared between GetSSOCallback
// (writer) and PostLogin (reader). Single-use: pop removes the entry.
var globalLoginTokens = &loginTokenStore{store: make(map[string]loginTokenEntry)}

func (s *loginTokenStore) save(opaqueToken, idToken string, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, v := range s.store {
		if now.After(v.exp) {
			delete(s.store, k)
		}
	}
	s.store[opaqueToken] = loginTokenEntry{idToken: idToken, exp: now.Add(ttl)}
}

// pop returns the id_token for opaqueToken and removes the entry (single-use).
// Returns ("", false) if the token is unknown or expired.
func (s *loginTokenStore) pop(opaqueToken string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.store[opaqueToken]
	if !ok {
		return "", false
	}
	delete(s.store, opaqueToken)
	if time.Now().After(entry.exp) {
		return "", false
	}
	return entry.idToken, true
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// ssoCallbackURL returns the redirect_uri registered with Dex for Matrix SSO.
func ssoCallbackURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host + "/_matrix/client/v3/login/sso/redirect/oidc"
}

// schemeOf extracts the lowercase URL scheme for logging purposes.
// Returns "<invalid>" if the URL cannot be parsed. Never returns user-controlled
// payloads beyond the scheme component.
func schemeOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return "<invalid>"
	}
	return strings.ToLower(u.Scheme)
}

// defaultDeepLinkSchemes is the default set of Matrix-client deep-link URL
// schemes that are always accepted by isRedirectURLAllowed.
// Operators can extend this list via NEBU_SSO_REDIRECT_SCHEMES but cannot
// remove these defaults.
var defaultDeepLinkSchemes = []string{
	"element",
	"io.element.fluffychat",
	"fluffychat",
}

// schemeDenylist contains URL schemes that are unconditionally rejected even
// if an operator accidentally adds them to the allowlist (AC 3, defense in
// depth). These schemes can be used to exfiltrate the loginToken via script
// execution, file access, or opaque data URIs.
var schemeDenylist = map[string]bool{
	"javascript": true,
	"data":       true,
	"file":       true,
	"vbscript":   true,
	"blob":       true,
}

// isRedirectURLAllowed validates the Matrix client's redirectUrl against a
// strict allowlist to prevent open-redirect token exfiltration (Story 5.24).
//
// Allowed:
//   - https:// — always, any host (HTTPS is safe for open-redirect since TLS
//     prevents MITM token capture and the host is validated by the browser)
//   - http:// — only when the host is localhost or 127.0.0.1 (development)
//   - default deep-link schemes: element://, io.element.fluffychat://, fluffychat://
//   - operator-configured schemes from NEBU_SSO_REDIRECT_SCHEMES (comma-separated)
//
// Blocked unconditionally (blocklist wins over allowlist):
//   - javascript, data, file, vbscript, blob
//
// All other schemes are rejected.
func isRedirectURLAllowed(raw string) bool {
	return isRedirectURLAllowedWithSchemes(raw, nil)
}

// isRedirectURLAllowedWithSchemes is the parameterised variant of
// isRedirectURLAllowed. extraSchemes are merged with the defaultDeepLinkSchemes
// but the schemeDenylist always takes precedence.
//
// This function is called by the operator-configured path (NEBU_SSO_REDIRECT_SCHEMES
// is parsed once at startup and passed here via LoginHandler).
func isRedirectURLAllowedWithSchemes(raw string, extraSchemes []string) bool {
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	// Scheme must be present (rejects bare strings like "just-a-string" and
	// relative paths like "/relative/path" that url.Parse gives an empty scheme).
	scheme := strings.ToLower(u.Scheme)
	if scheme == "" {
		return false
	}

	// Hard deny — blocklist wins over every allowlist entry (AC 3).
	if schemeDenylist[scheme] {
		return false
	}

	// https:// — always allowed (AC 1).
	if scheme == "https" {
		return true
	}

	// http:// — only loopback (AC 1).
	if scheme == "http" {
		host := u.Hostname()
		return host == "localhost" || host == "127.0.0.1"
	}

	// Deep-link schemes — check default allowlist first (AC 2).
	for _, s := range defaultDeepLinkSchemes {
		if scheme == strings.ToLower(s) {
			return true
		}
	}

	// Operator-configured extra schemes (AC 1 + NEBU_SSO_REDIRECT_SCHEMES).
	for _, s := range extraSchemes {
		if scheme == strings.ToLower(s) {
			return true
		}
	}

	return false
}

// ── Handlers ──────────────────────────────────────────────────────────────────

// GetSSORedirect handles GET /_matrix/client/v3/login/sso/redirect?redirectUrl=<url>
// and GET /_matrix/client/v3/login/sso/redirect/{idpId}?redirectUrl=<url>.
// Generates a PKCE verifier + state, stores them server-side, and redirects to Dex.
func (h *LoginHandler) GetSSORedirect(w http.ResponseWriter, r *http.Request) {
	clientRedirectURL := r.URL.Query().Get("redirectUrl")
	if clientRedirectURL == "" {
		writeMatrixError(w, http.StatusBadRequest, "M_MISSING_PARAM", "redirectUrl is required")
		return
	}

	// Story 5.24: validate redirectUrl against strict scheme allowlist to prevent
	// open-redirect token exfiltration. Scheme is not echoed in the error response
	// to avoid XSS reflection (AC 4).
	if !isRedirectURLAllowedWithSchemes(clientRedirectURL, h.ssoRedirectSchemes) {
		slog.Warn("matrix SSO: redirectUrl rejected by allowlist", "scheme", schemeOf(clientRedirectURL))
		writeMatrixError(w, http.StatusBadRequest, "M_INVALID_PARAM", "redirectUrl scheme is not permitted")
		return
	}

	inner := h.provider.Inner()
	if inner == nil {
		writeMatrixError(w, http.StatusServiceUnavailable, "M_UNKNOWN", "Authentication service unavailable")
		return
	}

	verifier := oauth2.GenerateVerifier()
	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal error")
		return
	}
	state := hex.EncodeToString(stateBytes)

	if err := globalSSOState.save(state, verifier, clientRedirectURL, 10*time.Minute); err != nil {
		slog.Warn("matrix SSO: state store full, rejecting redirect request", "err", err)
		writeMatrixError(w, http.StatusTooManyRequests, "M_LIMIT_EXCEEDED", "Too many pending SSO flows")
		return
	}

	oauth2Config := &oauth2.Config{
		ClientID:     h.clientID,
		ClientSecret: h.clientSecret,
		RedirectURL:  ssoCallbackURL(r),
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email", "groups"},
		Endpoint:     inner.Endpoint(),
	}
	// prompt=login forces Dex to re-authenticate the user even if an active Dex
	// session cookie exists. Without this, Dex reuses the cached session and may
	// return the same id_token that was already added to the denylist on logout,
	// causing the first /sync after re-login to return 401 → Element lands on #/welcome.
	// See: bugfix-logout-oidc-dex-session — OIDC Core 1.0 §3.1.2.1
	authURL := oauth2Config.AuthCodeURL(state,
		oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("prompt", "login"),
	)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// GetSSOCallback handles GET /_matrix/client/v3/login/sso/redirect/oidc.
// Dual purpose:
//   - Dex callback: ?code=...&state=... — exchanges code for id_token, generates a
//     short-lived opaque loginToken (MAJOR-2 fix), redirects client to redirectUrl.
//   - IDP-specific initiation: ?redirectUrl=... — same as GetSSORedirect.
func (h *LoginHandler) GetSSOCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")

	// No code → treat as initiation for the "oidc" IDP.
	if code == "" || state == "" {
		h.GetSSORedirect(w, r)
		return
	}

	entry, ok := globalSSOState.pop(state)
	if !ok {
		slog.Warn("matrix SSO: unknown or expired state", "state", state)
		writeMatrixError(w, http.StatusBadRequest, "M_UNKNOWN", "Invalid or expired SSO state")
		return
	}

	inner := h.provider.Inner()
	if inner == nil {
		writeMatrixError(w, http.StatusServiceUnavailable, "M_UNKNOWN", "Authentication service unavailable")
		return
	}

	oauth2Config := &oauth2.Config{
		ClientID:     h.clientID,
		ClientSecret: h.clientSecret,
		RedirectURL:  ssoCallbackURL(r),
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email", "groups"},
		Endpoint:     inner.Endpoint(),
	}

	token, err := oauth2Config.Exchange(r.Context(), code, oauth2.VerifierOption(entry.verifier))
	if err != nil {
		slog.Error("matrix SSO: token exchange failed", "err", err)
		writeMatrixError(w, http.StatusForbidden, "M_FORBIDDEN", "SSO token exchange failed")
		return
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		slog.Error("matrix SSO: id_token missing from token response")
		writeMatrixError(w, http.StatusForbidden, "M_FORBIDDEN", "SSO response missing id_token")
		return
	}

	// Generate a short-lived opaque loginToken instead of passing the raw id_token
	// in the URL. The id_token has a 1h lifetime and would be exposed in browser
	// history, server logs, and Referer headers. The opaque token:
	//   - is random (32 hex bytes = 128 bits of entropy)
	//   - is single-use (popped from store on first POST /login call)
	//   - expires after loginTokenTTL (30 seconds)
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal error")
		return
	}
	opaqueToken := hex.EncodeToString(tokenBytes)
	globalLoginTokens.save(opaqueToken, rawIDToken, loginTokenTTL)

	// Redirect to the Matrix client with the opaque loginToken.
	// The client calls POST /_matrix/client/v3/login with
	// {"type":"m.login.token","token":"<opaqueToken>"}.
	redirectTo := entry.redirectURL
	if strings.Contains(redirectTo, "?") {
		redirectTo += "&loginToken=" + opaqueToken
	} else {
		redirectTo += "?loginToken=" + opaqueToken
	}
	http.Redirect(w, r, redirectTo, http.StatusFound)
}
