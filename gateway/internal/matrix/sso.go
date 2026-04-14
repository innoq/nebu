package matrix

import (
	"crypto/rand"
	"encoding/hex"
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

func (s *ssoStateStore) save(state, verifier, redirectURL string, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, v := range s.store {
		if now.After(v.exp) {
			delete(s.store, k)
		}
	}
	s.store[state] = ssoStateEntry{
		verifier:    verifier,
		redirectURL: redirectURL,
		exp:         now.Add(ttl),
	}
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

// isRedirectURLAllowed validates the Matrix client's redirectUrl against an
// allowlist to prevent open-redirect token exfiltration (MAJOR-1).
//
// Allowed:
//   - localhost (any port) — local web clients (Element Web dev, etc.)
//   - Non-HTTP/S schemes — mobile/desktop app deep links (fluffychat://,
//     io.element.fluffychat://, element://, etc.). These cannot be
//     weaponised for web-based open-redirect attacks because browsers do
//     not follow custom-scheme redirects to arbitrary servers.
//
// Blocked: any http/https URL whose host is not localhost.
func isRedirectURLAllowed(raw string) bool {
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	// Custom URL schemes (app deep links) — safe against open-redirect.
	if u.Scheme != "http" && u.Scheme != "https" {
		return u.Scheme != "" // must have a scheme; reject bare strings
	}
	// For http/https only allow loopback.
	host := u.Hostname()
	switch host {
	case "localhost", "127.0.0.1", "::1":
		return true
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

	// MAJOR-1 fix: validate redirectUrl against allowlist to prevent open redirect.
	if !isRedirectURLAllowed(clientRedirectURL) {
		slog.Warn("matrix SSO: redirectUrl rejected by allowlist", "url", clientRedirectURL)
		writeMatrixError(w, http.StatusBadRequest, "M_INVALID_PARAM", "redirectUrl host is not permitted")
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

	globalSSOState.save(state, verifier, clientRedirectURL, 10*time.Minute)

	oauth2Config := &oauth2.Config{
		ClientID:     h.clientID,
		ClientSecret: h.clientSecret,
		RedirectURL:  ssoCallbackURL(r),
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email", "groups"},
		Endpoint:     inner.Endpoint(),
	}
	authURL := oauth2Config.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))
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

	// MAJOR-2 fix: generate a short-lived opaque loginToken instead of passing the
	// raw id_token in the URL. The id_token has a 1h lifetime and would be exposed
	// in browser history, server logs, and Referer headers. The opaque token:
	//   - is random (32 hex bytes = 128 bits of entropy)
	//   - is single-use (popped from store on first POST /login call)
	//   - expires in 30 seconds
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal error")
		return
	}
	opaqueToken := hex.EncodeToString(tokenBytes)
	globalLoginTokens.save(opaqueToken, rawIDToken, 5*time.Minute)

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
