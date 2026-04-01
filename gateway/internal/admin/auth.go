package admin

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/nebu/nebu/internal/auth"
	"golang.org/x/oauth2"
)

// ErrOIDCConfigMissing is returned when required OIDC configuration is absent from server_config.
var ErrOIDCConfigMissing = errors.New("OIDC configuration missing in server_config")

// ServerConfigReader reads OIDC config from server_config.
type ServerConfigReader interface {
	LoadOIDCConfig(ctx context.Context) (issuer, clientID, clientSecret string, err error)
}

// postgresServerConfigReader wraps *sql.DB to implement ServerConfigReader.
type postgresServerConfigReader struct {
	db     *sql.DB
	secret []byte
}

// LoadOIDCConfig queries server_config for OIDC settings and decrypts the client secret.
func (r *postgresServerConfigReader) LoadOIDCConfig(ctx context.Context) (issuer, clientID, clientSecret string, err error) {
	rows, err := r.db.QueryContext(ctx,
		"SELECT key, value FROM server_config WHERE key IN ('oidc_issuer', 'oidc_client_id', 'oidc_client_secret')")
	if err != nil {
		return "", "", "", err
	}
	defer rows.Close()

	vals := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return "", "", "", err
		}
		vals[k] = v
	}
	if err := rows.Err(); err != nil {
		return "", "", "", err
	}

	issuer = vals["oidc_issuer"]
	clientID = vals["oidc_client_id"]
	encSecret := vals["oidc_client_secret"]

	if issuer == "" || clientID == "" || encSecret == "" {
		return "", "", "", ErrOIDCConfigMissing
	}

	plain, err := decryptAES256GCM(r.secret, encSecret)
	if err != nil {
		return "", "", "", fmt.Errorf("decrypting oidc_client_secret: %w", err)
	}
	return issuer, clientID, plain, nil
}

// AdminAuth handles PKCE-protected OIDC Authorization Code flow for the Admin UI.
type AdminAuth struct {
	provider     *auth.Provider
	clientID     string
	clientSecret string
	claimName    string // from cfg.OIDCClaimRole
	secret       []byte // HMAC key — same internalSecret as PSK
	configReader ServerConfigReader
	tmpl         *TemplateHandler
}

// NewAdminAuth creates an AdminAuth instance.
func NewAdminAuth(provider *auth.Provider, clientID, clientSecret, claimName string, secret []byte, db *sql.DB, tmpl *TemplateHandler) *AdminAuth {
	var reader ServerConfigReader
	if db != nil {
		reader = &postgresServerConfigReader{db: db, secret: secret}
	}
	return &AdminAuth{
		provider:     provider,
		clientID:     clientID,
		clientSecret: clientSecret,
		claimName:    claimName,
		secret:       secret,
		configReader: reader,
		tmpl:         tmpl,
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
	redirectURL := scheme + "://" + r.Host + "/admin/callback"
	return &oauth2.Config{
		ClientID:     a.clientID,
		ClientSecret: a.clientSecret,
		RedirectURL:  redirectURL,
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
		Endpoint:     a.provider.Inner().Endpoint(),
	}
}

// LoginPageHandler handles GET /admin/login.
// Renders the login page with an SSO button. Accepts an optional ?error= query parameter.
func (a *AdminAuth) LoginPageHandler(w http.ResponseWriter, r *http.Request) {
	errorMsg := r.URL.Query().Get("error")
	data := LoginPageData{
		PageData: PageData{ActiveNav: "login"},
		Error:    errorMsg,
	}
	a.tmpl.render(w, "login", data)
}

// LoginStartHandler handles GET /admin/login/start.
// Reads OIDC config from DB, generates PKCE verifier + state, sets signed cookie, redirects to OIDC provider.
func (a *AdminAuth) LoginStartHandler(w http.ResponseWriter, r *http.Request) {
	if a.configReader == nil {
		http.Error(w, "OIDC configuration not found in server config. Please complete the Bootstrap Wizard first.", http.StatusServiceUnavailable)
		return
	}

	issuer, clientID, clientSecret, err := a.configReader.LoadOIDCConfig(r.Context())
	if err != nil {
		if errors.Is(err, ErrOIDCConfigMissing) {
			http.Error(w, "OIDC configuration not found in server config. Please complete the Bootstrap Wizard first.", http.StatusServiceUnavailable)
			return
		}
		http.Error(w, "Failed to load OIDC configuration: "+err.Error(), http.StatusServiceUnavailable)
		return
	}

	ctx := oidc.ClientContext(r.Context(), http.DefaultClient)
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		http.Error(w, "OIDC provider discovery failed: "+err.Error(), http.StatusServiceUnavailable)
		return
	}

	scheme := "https"
	if r.TLS == nil {
		scheme = "http"
	}

	oauth2Config := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  scheme + "://" + r.Host + "/admin/callback",
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
		Endpoint:     provider.Endpoint(),
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
		Path:     "/admin",
		MaxAge:   600,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})

	authURL := oauth2Config.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))
	http.Redirect(w, r, authURL, http.StatusFound)
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

// CallbackHandler handles GET /admin/callback (and legacy GET /admin/auth/callback).
// Validates state cookie, exchanges code for tokens, checks role, creates an admin session cookie.
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
		http.Redirect(w, r, "/admin/login?error=auth_failed", http.StatusFound)
		return
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		http.Redirect(w, r, "/admin/login?error=auth_failed", http.StatusFound)
		return
	}

	idToken, err := a.provider.Inner().Verifier(&oidc.Config{ClientID: a.clientID}).Verify(r.Context(), rawIDToken)
	if err != nil {
		http.Redirect(w, r, "/admin/login?error=auth_failed", http.StatusFound)
		return
	}

	var claims map[string]interface{}
	if err := idToken.Claims(&claims); err != nil {
		http.Redirect(w, r, "/admin/login?error=auth_failed", http.StatusFound)
		return
	}
	sub, _ := claims["sub"].(string)
	email, _ := claims["email"].(string)
	if sub == "" {
		http.Error(w, "invalid token: missing sub claim", http.StatusBadRequest)
		return
	}
	rawRole, _ := claims[a.claimName].(string)
	systemRole := auth.MapSystemRole(rawRole)

	if systemRole != "instance_admin" {
		http.Error(w, "Access denied: instance_admin role required.", http.StatusForbidden)
		return
	}

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

	// Delete the OIDC state cookie (covers both /admin/auth and /admin paths)
	http.SetCookie(w, &http.Cookie{
		Name:     "admin_oidc_state",
		Value:    "",
		Path:     "/admin",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})

	http.Redirect(w, r, "/admin/dashboard", http.StatusSeeOther)
}

// LogoutHandler handles GET /admin/logout.
// Deletes the admin session cookie and redirects to /admin/login.
func (a *AdminAuth) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "admin_session",
		Value:    "",
		Path:     "/admin",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
	})
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}
