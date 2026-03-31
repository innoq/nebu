package admin

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/nebu/nebu/internal/auth"
	"golang.org/x/oauth2"
)

// AdminAuth handles PKCE-protected OIDC Authorization Code flow for the Admin UI.
type AdminAuth struct {
	provider     *auth.Provider
	clientID     string
	clientSecret string
	claimName    string // from cfg.OIDCClaimRole
	secret       []byte // HMAC key — same internalSecret as PSK
}

// NewAdminAuth creates an AdminAuth instance.
func NewAdminAuth(provider *auth.Provider, clientID, clientSecret, claimName string, secret []byte) *AdminAuth {
	return &AdminAuth{
		provider:     provider,
		clientID:     clientID,
		clientSecret: clientSecret,
		claimName:    claimName,
		secret:       secret,
	}
}

type oidcStateCookie struct {
	State    string `json:"state"`
	Verifier string `json:"verifier"`
	Exp      int64  `json:"exp"` // Unix timestamp (seconds)
}

type adminSessionCookie struct {
	Sub   string `json:"sub"`
	Email string `json:"email"`
	Role  string `json:"role"` // mapped system_role
	Exp   int64  `json:"exp"`
}

// signCookie returns base64url(payload) + "." + base64url(HMAC-SHA256(secret, base64url(payload))).
func (a *AdminAuth) signCookie(payload []byte) string {
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(encoded))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encoded + "." + sig
}

// verifyCookie splits on ".", recomputes HMAC in constant time, and decodes the payload.
func (a *AdminAuth) verifyCookie(value string) ([]byte, error) {
	idx := strings.LastIndex(value, ".")
	if idx < 0 {
		return nil, errors.New("invalid cookie format")
	}
	encoded, sigPart := value[:idx], value[idx+1:]
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(encoded))
	expectedSig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expectedSig), []byte(sigPart)) {
		return nil, errors.New("invalid cookie signature")
	}
	return base64.RawURLEncoding.DecodeString(encoded)
}

func (a *AdminAuth) buildOAuth2Config(r *http.Request) *oauth2.Config {
	scheme := "https"
	if r.TLS == nil {
		scheme = "http"
	}
	redirectURL := scheme + "://" + r.Host + "/admin/auth/callback"
	return &oauth2.Config{
		ClientID:     a.clientID,
		ClientSecret: a.clientSecret,
		RedirectURL:  redirectURL,
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
		Endpoint:     a.provider.Inner().Endpoint(),
	}
}

// LoginHandler handles GET /admin/auth/login.
// Generates PKCE verifier + state, stores them in a signed cookie, and redirects to OIDC provider.
func (a *AdminAuth) LoginHandler(w http.ResponseWriter, r *http.Request) {
	if a.provider.Inner() == nil {
		http.Error(w, "OIDC provider unavailable", http.StatusServiceUnavailable)
		return
	}

	verifier := oauth2.GenerateVerifier()

	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	state := hex.EncodeToString(stateBytes)

	sc := oidcStateCookie{
		State:    state,
		Verifier: verifier,
		Exp:      time.Now().Add(10 * time.Minute).Unix(),
	}
	payload, err := json.Marshal(sc)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	cookieValue := a.signCookie(payload)
	http.SetCookie(w, &http.Cookie{
		Name:     "admin_oidc_state",
		Value:    cookieValue,
		Path:     "/admin/auth",
		MaxAge:   600,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})

	oauth2Config := a.buildOAuth2Config(r)
	authURL := oauth2Config.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))
	http.Redirect(w, r, authURL, http.StatusFound)
}

// CallbackHandler handles GET /admin/auth/callback.
// Validates state cookie, exchanges code for tokens, creates an admin session cookie.
func (a *AdminAuth) CallbackHandler(w http.ResponseWriter, r *http.Request) {
	queryState := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")

	cookie, err := r.Cookie("admin_oidc_state")
	if err != nil {
		http.Error(w, "missing state cookie", http.StatusBadRequest)
		return
	}

	payload, err := a.verifyCookie(cookie.Value)
	if err != nil {
		http.Error(w, "invalid state cookie", http.StatusBadRequest)
		return
	}

	var sc oidcStateCookie
	if err := json.Unmarshal(payload, &sc); err != nil {
		http.Error(w, "invalid state cookie", http.StatusBadRequest)
		return
	}

	if time.Now().Unix() > sc.Exp {
		http.Error(w, "state cookie expired", http.StatusBadRequest)
		return
	}

	if queryState != sc.State {
		http.Error(w, "state mismatch", http.StatusBadRequest)
		return
	}

	if a.provider.Inner() == nil {
		http.Error(w, "OIDC provider unavailable", http.StatusServiceUnavailable)
		return
	}

	oauth2Config := a.buildOAuth2Config(r)
	token, err := oauth2Config.Exchange(r.Context(), code, oauth2.VerifierOption(sc.Verifier))
	if err != nil {
		http.Redirect(w, r, "/admin/auth/login?error=auth_failed", http.StatusFound)
		return
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		http.Redirect(w, r, "/admin/auth/login?error=auth_failed", http.StatusFound)
		return
	}

	idToken, err := a.provider.Inner().Verifier(&oidc.Config{ClientID: a.clientID}).Verify(r.Context(), rawIDToken)
	if err != nil {
		http.Redirect(w, r, "/admin/auth/login?error=auth_failed", http.StatusFound)
		return
	}

	var claims map[string]interface{}
	if err := idToken.Claims(&claims); err != nil {
		http.Redirect(w, r, "/admin/auth/login?error=auth_failed", http.StatusFound)
		return
	}
	sub, _ := claims["sub"].(string)
	email, _ := claims["email"].(string)
	rawRole, _ := claims[a.claimName].(string)
	systemRole := auth.MapSystemRole(rawRole)

	sess := adminSessionCookie{
		Sub:   sub,
		Email: email,
		Role:  systemRole,
		Exp:   time.Now().Add(8 * time.Hour).Unix(),
	}
	sessPayload, err := json.Marshal(sess)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "admin_session",
		Value:    a.signCookie(sessPayload),
		Path:     "/admin",
		MaxAge:   28800,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
	})

	// Delete the OIDC state cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "admin_oidc_state",
		Value:    "",
		Path:     "/admin/auth",
		MaxAge:   -1,
		HttpOnly: true,
	})

	http.Redirect(w, r, "/admin/", http.StatusSeeOther)
}
