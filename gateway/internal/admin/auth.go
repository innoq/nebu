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
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/nebu/nebu/internal/auth"
	"golang.org/x/oauth2"
)

// ErrOIDCConfigMissing is returned when required OIDC configuration is absent from server_config.
var ErrOIDCConfigMissing = errors.New("OIDC configuration missing in server_config")

// ErrAlreadyCompleted is returned by CompleteBootstrap when bootstrap_completed is already set.
// The handler maps this sentinel to 403 Forbidden (defense-in-depth against replay attacks).
var ErrAlreadyCompleted = errors.New("bootstrap already completed")

// sqlQuerier is a minimal interface satisfied by both *sql.DB and *sql.Tx.
// It allows internal DB helpers to be called within a transaction or standalone.
type sqlQuerier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// ServerConfigReader reads and writes config from/to server_config.
type ServerConfigReader interface {
	LoadOIDCConfig(ctx context.Context) (issuer, clientID, clientSecret string, err error)
	CompleteBootstrap(ctx context.Context) error
	// LoadAdminGroupClaim returns the configured admin group claim value (e.g. "instance_admin").
	// Returns the default "instance_admin" if not set.
	LoadAdminGroupClaim(ctx context.Context) (string, error)
	// SaveAdminGroupClaim persists the admin group claim value to server_config.
	SaveAdminGroupClaim(ctx context.Context, claimValue string) error
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

// LoadAdminGroupClaim returns the configured admin group claim value from server_config.
// Falls back to "instance_admin" if the key is not set.
func (r *postgresServerConfigReader) LoadAdminGroupClaim(ctx context.Context) (string, error) {
	var value string
	err := r.db.QueryRowContext(ctx,
		"SELECT value FROM server_config WHERE key = 'admin_group_claim'").Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "instance_admin", nil
	}
	if err != nil {
		return "", err
	}
	return value, nil
}

// SaveAdminGroupClaim persists the admin group claim value to server_config.
func (r *postgresServerConfigReader) SaveAdminGroupClaim(ctx context.Context, claimValue string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO server_config (key, value, set_at) VALUES ('admin_group_claim', $1, $2)
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, set_at = EXCLUDED.set_at`,
		claimValue, time.Now().UnixMilli())
	return err
}

// CompleteBootstrap writes bootstrap_completed = true to server_config.
// Returns an error if bootstrap was already completed (0 rows affected) — this
// prevents privilege escalation via mode=bootstrap replay after initial setup.
func (r *postgresServerConfigReader) CompleteBootstrap(ctx context.Context) error {
	return completeBootstrapTx(ctx, r.db)
}

// completeBootstrapTx executes the CompleteBootstrap write via the given sqlQuerier
// (which may be a *sql.Tx or a *sql.DB). Returns ErrAlreadyCompleted when the row
// already exists (ON CONFLICT DO NOTHING → 0 rows affected).
func completeBootstrapTx(ctx context.Context, q sqlQuerier) error {
	result, err := q.ExecContext(ctx,
		`INSERT INTO server_config (key, value, set_at) VALUES ($1, $2, $3)
		 ON CONFLICT (key) DO NOTHING`,
		"bootstrap_completed", "true", time.Now().UnixMilli())
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrAlreadyCompleted
	}
	return nil
}

// saveAdminGroupClaimTx executes the SaveAdminGroupClaim upsert via the given sqlQuerier.
func saveAdminGroupClaimTx(ctx context.Context, q sqlQuerier, claimValue string) error {
	_, err := q.ExecContext(ctx,
		`INSERT INTO server_config (key, value, set_at) VALUES ('admin_group_claim', $1, $2)
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, set_at = EXCLUDED.set_at`,
		claimValue, time.Now().UnixMilli())
	return err
}

// AdminAuth handles PKCE-protected OIDC Authorization Code flow for the Admin UI.
type AdminAuth struct {
	provider         *auth.Provider
	clientID         string
	clientSecret     string
	claimName        string // from cfg.OIDCClaimRole (legacy env-var fallback)
	secret           []byte // HMAC key — same internalSecret as PSK
	configReader     ServerConfigReader
	tmpl             *TemplateHandler
	draftStore       BootstrapDraftStore    // for bootstrap claim-selection flow
	bootstrapChecker BootstrapStatusChecker // guards mode=bootstrap replay after completion
	// runInTx executes fn inside a transaction. On success fn must return nil and the
	// caller commits; on error the transaction is rolled back before returning.
	// Injected at construction time — production uses *sql.Tx, tests inject a fake.
	runInTx func(ctx context.Context, fn func(q sqlQuerier) error) error
}

// NewAdminAuth creates an AdminAuth instance.
func NewAdminAuth(provider *auth.Provider, clientID, clientSecret, claimName string, secret []byte, db *sql.DB, tmpl *TemplateHandler) *AdminAuth {
	var reader ServerConfigReader
	var draft BootstrapDraftStore
	var checker BootstrapStatusChecker
	var runInTx func(ctx context.Context, fn func(q sqlQuerier) error) error
	if db != nil {
		reader = &postgresServerConfigReader{db: db, secret: secret}
		draft = &postgresBootstrapDraftStore{db: db}
		checker = NewPostgresBootstrapChecker(db)
		runInTx = func(ctx context.Context, fn func(q sqlQuerier) error) error {
			tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
			if err != nil {
				return err
			}
			defer tx.Rollback() //nolint:errcheck // best-effort rollback; commit error is returned below
			if err := fn(tx); err != nil {
				return err
			}
			return tx.Commit()
		}
	}
	return &AdminAuth{
		provider:         provider,
		clientID:         clientID,
		clientSecret:     clientSecret,
		claimName:        claimName,
		secret:           secret,
		configReader:     reader,
		tmpl:             tmpl,
		draftStore:       draft,
		bootstrapChecker: checker,
		runInTx:          runInTx,
	}
}

type oidcStateCookie struct {
	State    string `json:"state"`
	Verifier string `json:"verifier"`
	Exp      int64  `json:"exp"`  // Unix timestamp (seconds)
	Mode     string `json:"mode"` // "bootstrap" during initial setup, empty otherwise
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
		http.Error(w, "OIDC configuration not found in server config. Please complete the Bootstrap Wizard first: /admin/bootstrap", http.StatusServiceUnavailable)
		return
	}

	issuer, clientID, clientSecret, err := a.configReader.LoadOIDCConfig(r.Context())
	if err != nil {
		if errors.Is(err, ErrOIDCConfigMissing) {
			http.Redirect(w, r, "/admin/bootstrap", http.StatusFound)
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
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email", "groups"},
		Endpoint:     provider.Endpoint(),
	}

	verifier := oauth2.GenerateVerifier()

	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	state := hex.EncodeToString(stateBytes)

	mode := r.URL.Query().Get("mode") // "bootstrap" during initial setup

	if mode == "bootstrap" && a.bootstrapChecker != nil {
		active, err := a.bootstrapChecker.IsBootstrapActive(r.Context())
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		if !active {
			http.Error(w, "Bootstrap already completed", http.StatusForbidden)
			return
		}
	}

	sc := oidcStateCookie{
		State:    state,
		Verifier: verifier,
		Exp:      time.Now().Add(10 * time.Minute).Unix(),
		Mode:     mode,
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
		Path:     "/admin/callback",
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
	if a.configReader == nil {
		http.Error(w, "OIDC configuration not available", http.StatusServiceUnavailable)
		return
	}

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

	// Load OIDC config from DB (set by bootstrap wizard — env-var config may be absent).
	issuer, clientID, clientSecret, err := a.configReader.LoadOIDCConfig(r.Context())
	if err != nil {
		slog.Error("callback: failed to load OIDC config", "err", err)
		http.Redirect(w, r, "/admin/login?error=auth_failed", http.StatusFound)
		return
	}
	ctx := oidc.ClientContext(r.Context(), http.DefaultClient)
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		slog.Error("callback: OIDC provider discovery failed", "err", err)
		http.Redirect(w, r, "/admin/login?error=auth_failed", http.StatusFound)
		return
	}

	scheme := "https"
	if r.TLS == nil {
		scheme = "http"
	}
	// Scopes must match the original auth request so that Dex includes the groups claim
	// in the token exchange response. The Go oauth2 library sends the scope parameter
	// on exchange; omitting "groups" here causes Dex to strip it from the returned tokens.
	oauth2Config := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  scheme + "://" + r.Host + "/admin/callback",
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email", "groups"},
		Endpoint:     provider.Endpoint(),
	}

	token, err := oauth2Config.Exchange(r.Context(), code, oauth2.VerifierOption(sc.Verifier))
	if err != nil {
		slog.Error("callback: token exchange failed", "err", err)
		http.Redirect(w, r, "/admin/login?error=auth_failed", http.StatusFound)
		return
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		http.Redirect(w, r, "/admin/login?error=auth_failed", http.StatusFound)
		return
	}

	idToken, err := provider.Verifier(&oidc.Config{ClientID: clientID}).Verify(r.Context(), rawIDToken)
	if err != nil {
		slog.Error("callback: token verification failed", "err", err)
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
	// The role claim may be a string or a []interface{} (e.g. Dex "groups" array).
	// Try string first; fall back to first matching element of an array.
	// Dex v2.41+ returns the groups claim via the UserInfo endpoint, not in the ID token.
	// Fetch UserInfo to supplement ID token claims with the role claim if absent.
	// Dex v2.41 did not include the groups claim in the ID token exchange response.
	// From v2.45+ groups are in the ID token. Keep the UserInfo fallback for robustness.
	if _, ok := claims[a.claimName]; !ok {
		uiCtx := oidc.ClientContext(r.Context(), http.DefaultClient)
		if userInfo, uiErr := provider.UserInfo(uiCtx, oauth2.StaticTokenSource(token)); uiErr == nil {
			var uiClaims map[string]interface{}
			if uiErr = userInfo.Claims(&uiClaims); uiErr == nil {
				if v, ok := uiClaims[a.claimName]; ok {
					claims[a.claimName] = v
				}
			}
		} else {
			slog.Warn("callback: userinfo fetch failed", "err", uiErr)
		}
	}

	// Delete the OIDC state cookie before branching.
	http.SetCookie(w, &http.Cookie{
		Name:     "admin_oidc_state",
		Value:    "",
		Path:     "/admin/callback",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})

	if sc.Mode == "bootstrap" {
		// Bootstrap flow: store sub+email in draft, then show claim-selection page.
		// The ClaimSelectionHandler will complete bootstrap and create the session.
		if a.draftStore != nil {
			for _, kv := range []struct{ k, v string }{
				{"bootstrap_sub", sub},
				{"bootstrap_email", email},
			} {
				if err := a.draftStore.SaveDraft(r.Context(), kv.k, kv.v); err != nil {
					slog.Error("callback: failed to save bootstrap identity draft", "key", kv.k, "err", err)
					http.Redirect(w, r, "/admin/login?error=auth_failed", http.StatusFound)
					return
				}
			}
		}
		// Render claim selection page with all string/array claims from the token.
		data := ClaimSelectionPageData{
			PageData: PageData{BootstrapMode: true, ActiveNav: "bootstrap"},
			Claims:   extractDiscoveredClaims(claims),
			Email:    email,
		}
		a.tmpl.render(w, "bootstrap-claims", data)
		return
	}

	// Non-bootstrap login: check admin group claim from server_config.
	adminGroupClaim := "instance_admin" // default fallback
	if a.configReader != nil {
		if loaded, err := a.configReader.LoadAdminGroupClaim(r.Context()); err == nil {
			adminGroupClaim = loaded
		}
	}
	if !auth.MatchesAdminGroupClaim(claims, adminGroupClaim) {
		http.Error(w, "Access denied: admin group claim not present in token.", http.StatusForbidden)
		return
	}

	sess := adminSessionCookie{
		Sub:   sub,
		Email: email,
		Role:  "instance_admin",
		Exp:   time.Now().Add(8 * time.Hour).Unix(),
	}
	sessPayload, err := json.Marshal(sess)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// SameSite=Lax (not Strict): the OIDC callback is initiated cross-site (from Dex).
	// Chrome treats the entire redirect chain as cross-site, so SameSite=Strict would
	// prevent the session cookie from being sent on the immediate post-callback redirect.
	// Lax still protects against CSRF for all non-GET requests.
	http.SetCookie(w, &http.Cookie{
		Name:     "admin_session",
		Value:    a.signCookie(sessPayload),
		Path:     "/admin",
		MaxAge:   28800,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})

	http.Redirect(w, r, "/admin/dashboard", http.StatusSeeOther)
}

// extractDiscoveredClaims converts a raw claims map into a slice of DiscoveredClaim
// for display on the claim selection page. Only string and string-array values are included.
func extractDiscoveredClaims(claims map[string]interface{}) []DiscoveredClaim {
	result := make([]DiscoveredClaim, 0, len(claims))
	for k, v := range claims {
		switch val := v.(type) {
		case string:
			if val != "" {
				result = append(result, DiscoveredClaim{Key: k, Values: []string{val}})
			}
		case []interface{}:
			var values []string
			for _, item := range val {
				if s, ok := item.(string); ok && s != "" {
					values = append(values, s)
				}
			}
			if len(values) > 0 {
				result = append(result, DiscoveredClaim{Key: k, Values: values})
			}
		}
	}
	return result
}

// ClaimSelectionHandler handles POST /admin/bootstrap/select-claim.
// Saves the selected admin group claim, completes bootstrap, creates the admin session, and redirects to dashboard.
func (a *AdminAuth) ClaimSelectionHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	selectedClaim := r.FormValue("admin_group_claim")
	if selectedClaim == "" {
		selectedClaim = "instance_admin"
	}

	if a.draftStore == nil || a.configReader == nil || a.runInTx == nil {
		http.Error(w, "bootstrap not available", http.StatusServiceUnavailable)
		return
	}

	// Load sub + email stored by CallbackHandler during bootstrap OIDC flow.
	sub, subOK, err := a.draftStore.LoadDraft(r.Context(), "bootstrap_sub")
	if err != nil || !subOK || sub == "" {
		slog.Error("claim selection: missing bootstrap_sub in draft", "err", err)
		http.Redirect(w, r, "/admin/login?error=auth_failed", http.StatusFound)
		return
	}
	email, _, _ := a.draftStore.LoadDraft(r.Context(), "bootstrap_email")

	// Load OIDC config from draft.
	instanceName, _, _ := a.draftStore.LoadDraft(r.Context(), "instance_name")
	oidcIssuer, _, _ := a.draftStore.LoadDraft(r.Context(), "oidc_issuer")
	oidcClientID, _, _ := a.draftStore.LoadDraft(r.Context(), "oidc_client_id")
	encryptedSecret, secretFound, err := a.draftStore.LoadDraft(r.Context(), "oidc_client_secret")
	if err != nil || !secretFound {
		slog.Error("claim selection: missing oidc_client_secret in draft", "err", err)
		http.Error(w, "internal error: bootstrap draft incomplete — please restart the wizard", http.StatusInternalServerError)
		return
	}

	// AC1: SaveBootstrapConfig, SaveAdminGroupClaim, ClearDraft, and CompleteBootstrap
	// run inside a single transaction. If any step fails the transaction is rolled back
	// and no server_config changes persist.
	txErr := a.runInTx(r.Context(), func(q sqlQuerier) error {
		if err := saveBootstrapConfigTx(r.Context(), q, instanceName, oidcIssuer, oidcClientID, encryptedSecret); err != nil {
			return fmt.Errorf("save bootstrap config: %w", err)
		}
		if err := saveAdminGroupClaimTx(r.Context(), q, selectedClaim); err != nil {
			return fmt.Errorf("save admin group claim: %w", err)
		}
		// AC4: ClearDraft runs inside the same TX — its failure aborts the transaction.
		if err := clearDraftTx(r.Context(), q); err != nil {
			return fmt.Errorf("clear draft: %w", err)
		}
		// completeBootstrapTx is the commit sentinel — returns ErrAlreadyCompleted on replay.
		if err := completeBootstrapTx(r.Context(), q); err != nil {
			return err // preserve ErrAlreadyCompleted sentinel unwrapped
		}
		return nil
	})
	if errors.Is(txErr, ErrAlreadyCompleted) {
		slog.Error("claim selection: bootstrap already completed", "err", txErr)
		http.Error(w, "Bootstrap already completed", http.StatusForbidden)
		return
	} else if txErr != nil {
		slog.Error("claim selection: transaction failed", "err", txErr)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Create admin session cookie — operator is now authenticated.
	sess := adminSessionCookie{
		Sub:   sub,
		Email: email,
		Role:  "instance_admin",
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
