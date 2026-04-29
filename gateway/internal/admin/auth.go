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
	"github.com/nebu/nebu/internal/audit"
	"github.com/nebu/nebu/internal/auth"
	pb "github.com/nebu/nebu/internal/grpc/pb"
	"github.com/nebu/nebu/internal/validate"
	"golang.org/x/oauth2"
)

// ErrOIDCConfigMissing is returned when required OIDC configuration is absent from server_config.
var ErrOIDCConfigMissing = errors.New("OIDC configuration missing in server_config")

// globalProviderCache caches *oidc.Provider instances by issuer URL to avoid
// redundant OIDC discovery requests. Initialized once at package level.
var globalProviderCache = newOIDCProviderCache(func(ctx context.Context, issuer string) (oidcProvider, error) {
	return oidc.NewProvider(ctx, issuer)
})

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

// loadOIDCConfigFromDraft reads issuer, client ID and (decrypted) client secret
// from the bootstrap_draft table — the wizard's holding area before bootstrap
// completion writes the values into server_config. Returns ("", "", "", nil) if
// the draft does not contain a complete OIDC config (caller falls back to
// server_config).
func (a *AdminAuth) loadOIDCConfigFromDraft(ctx context.Context) (issuer, clientID, clientSecret string, err error) {
	if a.draftStore == nil {
		return "", "", "", nil
	}
	issuer, ok1, err := a.draftStore.LoadDraft(ctx, "oidc_issuer")
	if err != nil {
		return "", "", "", fmt.Errorf("load draft oidc_issuer: %w", err)
	}
	clientID, ok2, err := a.draftStore.LoadDraft(ctx, "oidc_client_id")
	if err != nil {
		return "", "", "", fmt.Errorf("load draft oidc_client_id: %w", err)
	}
	encSecret, ok3, err := a.draftStore.LoadDraft(ctx, "oidc_client_secret")
	if err != nil {
		return "", "", "", fmt.Errorf("load draft oidc_client_secret: %w", err)
	}
	if !ok1 || !ok2 || !ok3 {
		return "", "", "", nil
	}
	plain, err := decryptAES256GCM(a.secret, encSecret)
	if err != nil {
		return "", "", "", fmt.Errorf("decrypt draft oidc_client_secret: %w", err)
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
// Also seeds audit_log_retention_days (Story 5.1 AC3) with the default 2555 days (7 years).
// ON CONFLICT DO NOTHING preserves any manually configured retention period.
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
	_, err = q.ExecContext(ctx,
		`INSERT INTO server_config (key, value, set_at) VALUES ($1, $2, $3)
		 ON CONFLICT (key) DO NOTHING`,
		"audit_log_retention_days", "2555", time.Now().UnixMilli())
	return err
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
	// sessionStore backs the admin session with a server-side record (Story 5.12).
	// nil means the legacy stateless cookie mode is active (backward-compat).
	sessionStore AdminSessionStore
	// coreClient is the gRPC client for Elixir core calls (e.g. WriteAuditLog).
	// nil disables audit logging (backward-compat for test environments without core).
	coreClient pb.CoreServiceClient
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

// SetSessionStore injects the server-side session store into AdminAuth.
// Call this after NewAdminAuth before the HTTP server starts.
func (a *AdminAuth) SetSessionStore(store AdminSessionStore) {
	a.sessionStore = store
}

// SetCoreClient injects the gRPC core client for audit log calls.
// Call this after NewAdminAuth before the HTTP server starts.
func (a *AdminAuth) SetCoreClient(c pb.CoreServiceClient) {
	a.coreClient = c
}

// logAuditEvent sends one audit event to the Elixir core via gRPC (synchronous).
// It relies on audit.LogEvent's never-raise contract — gRPC failures are logged
// at Warn level and swallowed; the primary request path is never blocked by an
// error return. Latency is bounded by the standard gRPC dial/call timeouts on
// coreClient. If coreClient is nil (test environments without a running core),
// this is a no-op.
func (a *AdminAuth) logAuditEvent(ctx context.Context, actorUserID, action, targetType, targetID string, metadata map[string]any, outcome, errorDetail string) {
	if a.coreClient == nil {
		return
	}
	audit.LogEvent(ctx, a.coreClient, actorUserID, action, targetType, targetID, metadata, outcome, errorDetail) //nolint:errcheck // always nil per never-raise contract
}

type oidcStateCookie struct {
	State    string `json:"state"`
	Verifier string `json:"verifier"`
	Exp      int64  `json:"exp"`   // Unix timestamp (seconds)
	Mode     string `json:"mode"`  // "bootstrap" during initial setup, empty otherwise
	Nonce    string `json:"nonce"` // OIDC nonce claim (Story 5.16)
}

type adminSessionCookie struct {
	Sub   string `json:"sub"`
	Email string `json:"email"`
	Role  string `json:"role"` // mapped system_role
	Exp   int64  `json:"exp"`
}

// adminSessionSIDCookie is the new cookie format used when a server-side session
// store is configured (Story 5.12). Only the opaque SID is stored in the cookie;
// user_id and roles are read from the DB on every request.
type adminSessionSIDCookie struct {
	SID string `json:"sid"`
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
	scheme := "http"
	if isRequestSecure(r) {
		scheme = "https"
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

	mode := r.URL.Query().Get("mode") // "bootstrap" during initial setup

	var issuer, clientID, clientSecret string
	var err error

	// Bootstrap mode: OIDC config lives in bootstrap_draft (not yet in server_config).
	// Try draft first; only fall back to server_config if all draft keys are absent.
	if mode == "bootstrap" && a.draftStore != nil {
		issuer, clientID, clientSecret, err = a.loadOIDCConfigFromDraft(r.Context())
		if err != nil {
			slog.Error("failed to load bootstrap OIDC draft", "err", err)
			http.Redirect(w, r, "/admin/bootstrap", http.StatusFound)
			return
		}
	}

	if issuer == "" {
		issuer, clientID, clientSecret, err = a.configReader.LoadOIDCConfig(r.Context())
		if err != nil {
			if errors.Is(err, ErrOIDCConfigMissing) {
				http.Redirect(w, r, "/admin/bootstrap", http.StatusFound)
				return
			}
			slog.Error("failed to load OIDC configuration", "err", err)
			http.Error(w, "Server error: OIDC configuration unavailable. Try again later.", http.StatusServiceUnavailable)
			return
		}
	}

	if err := validateIssuerURL(issuer); err != nil {
		slog.Error("invalid OIDC issuer in config, reconfigure to HTTPS", "issuer", issuer, "err", err)
		http.Error(w, "Server configuration error: OIDC issuer must use HTTPS. Contact the operator.", http.StatusInternalServerError)
		return
	}

	ctx := oidc.ClientContext(r.Context(), http.DefaultClient)
	rawProvider, err := globalProviderCache.load(ctx, issuer)
	if err != nil {
		renderErrorWithID(w, r, http.StatusInternalServerError, "OIDC provider discovery failed", "Please contact your administrator.", err, a.tmpl)
		return
	}
	provider, ok := rawProvider.(*oidc.Provider)
	if !ok {
		renderErrorWithID(w, r, http.StatusInternalServerError, "OIDC provider type assertion failed", "Please contact your administrator.", errors.New("unexpected OIDC provider type"), a.tmpl)
		return
	}

	scheme := "http"
	if isRequestSecure(r) {
		scheme = "https"
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

	nonceBytes := make([]byte, 32)
	if _, err := rand.Read(nonceBytes); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	nonce := base64.RawURLEncoding.EncodeToString(nonceBytes)

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
		Nonce:    nonce,
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
		Secure:   isRequestSecure(r),
		SameSite: http.SameSiteLaxMode,
	})

	authURL := oauth2Config.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier), oidc.Nonce(nonce))
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

	nonceBytes := make([]byte, 32)
	if _, err := rand.Read(nonceBytes); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	nonce := base64.RawURLEncoding.EncodeToString(nonceBytes)

	sc := oidcStateCookie{
		State:    state,
		Verifier: verifier,
		Exp:      time.Now().Add(10 * time.Minute).Unix(),
		Nonce:    nonce,
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
		Secure:   isRequestSecure(r),
		SameSite: http.SameSiteLaxMode,
	})

	oauth2Config := a.buildOAuth2Config(r)
	authURL := oauth2Config.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier), oidc.Nonce(nonce))
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

	// Load OIDC config from DB. Bootstrap mode reads from bootstrap_draft (the wizard
	// has not yet promoted config to server_config); regular login reads from
	// server_config. The state cookie carries the mode set at login/start time.
	var issuer, clientID, clientSecret string
	if sc.Mode == "bootstrap" && a.draftStore != nil {
		issuer, clientID, clientSecret, err = a.loadOIDCConfigFromDraft(r.Context())
		if err != nil {
			slog.Error("callback: failed to load bootstrap OIDC draft", "err", err)
			http.Redirect(w, r, "/admin/login?error=auth_failed", http.StatusFound)
			return
		}
	}

	if issuer == "" {
		issuer, clientID, clientSecret, err = a.configReader.LoadOIDCConfig(r.Context())
		if err != nil {
			slog.Error("callback: failed to load OIDC config", "err", err)
			http.Redirect(w, r, "/admin/login?error=auth_failed", http.StatusFound)
			return
		}
	}

	if err := validateIssuerURL(issuer); err != nil {
		slog.Error("callback: invalid OIDC issuer in config, reconfigure to HTTPS", "issuer", issuer, "err", err)
		http.Error(w, "Server configuration error: OIDC issuer must use HTTPS. Contact the operator.", http.StatusInternalServerError)
		return
	}

	ctx := oidc.ClientContext(r.Context(), http.DefaultClient)
	rawProvider, err := globalProviderCache.load(ctx, issuer)
	if err != nil {
		slog.Error("callback: OIDC provider discovery failed", "err", err)
		http.Redirect(w, r, "/admin/login?error=auth_failed", http.StatusFound)
		return
	}
	provider, ok := rawProvider.(*oidc.Provider)
	if !ok {
		slog.Error("callback: unexpected OIDC provider type")
		http.Redirect(w, r, "/admin/login?error=auth_failed", http.StatusFound)
		return
	}

	scheme := "http"
	if isRequestSecure(r) {
		scheme = "https"
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

	idToken, err := provider.Verifier(&oidc.Config{
		ClientID:             clientID,
		SupportedSigningAlgs: validate.SupportedAlgs(),
	}).Verify(r.Context(), rawIDToken)
	if err != nil {
		slog.Error("callback: token verification failed", "err", err)
		http.Redirect(w, r, "/admin/login?error=auth_failed", http.StatusFound)
		return
	}

	// AC5 (Story 5.16): reject cookies from before nonce was introduced (or
	// from the legacy LoginHandler path that somehow lost its nonce). An empty
	// nonce in the cookie would match an ID token that also has no nonce claim,
	// which would silently bypass the verification.
	if sc.Nonce == "" {
		slog.Warn("callback: state cookie missing nonce — rejecting legacy/corrupt cookie")
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// AC4 (Story 5.16): verify OIDC nonce to prevent token replay attacks.
	if idToken.Nonce != sc.Nonce {
		slog.Warn("callback: nonce mismatch", "expected", sc.Nonce, "got", idToken.Nonce)
		http.Error(w, "Forbidden", http.StatusForbidden)
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
		Secure:   isRequestSecure(r),
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
			PageData: PageData{BootstrapMode: true, ActiveNav: "bootstrap", CSRFToken: CSRFTokenFromContext(r.Context())},
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
		a.logAuditEvent(r.Context(), sub, "admin_login_failed", "user", sub, nil, "failure", "role_check_failed")
		http.Error(w, "Access denied: admin group claim not present in token.", http.StatusForbidden)
		return
	}

	// AC6: cap session expiry to min(idToken.Exp, now+8h).
	const maxSessionDuration = 8 * time.Hour
	expiresAt := time.Now().Add(maxSessionDuration)
	if idToken.Expiry.Before(expiresAt) {
		expiresAt = idToken.Expiry
	}

	// AC2: if a server-side session store is wired, create a session row and store
	// only the SID in the cookie. Fall back to the legacy stateless cookie when no
	// store is configured (backward-compat for environments without the DB migration).
	if a.sessionStore != nil {
		sid, err := a.sessionStore.Create(r.Context(), sub, expiresAt)
		if err != nil {
			slog.Error("callback: failed to create admin session", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		sidPayload, err := json.Marshal(adminSessionSIDCookie{SID: sid})
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		maxAge := int(time.Until(expiresAt).Seconds())
		if maxAge <= 0 {
			maxAge = 1
		}
		// SameSite=Lax (not Strict): the OIDC callback is initiated cross-site (from Dex).
		http.SetCookie(w, &http.Cookie{
			Name:     "admin_session",
			Value:    a.signCookie(sidPayload),
			Path:     "/admin",
			MaxAge:   maxAge,
			HttpOnly: true,
			Secure:   isRequestSecure(r),
			SameSite: http.SameSiteLaxMode,
		})
		a.logAuditEvent(r.Context(), sub, "admin_login", "user", sub, nil, "success", "")
		http.Redirect(w, r, "/admin/dashboard", http.StatusSeeOther)
		return
	}

	// Legacy stateless cookie (no session store configured).
	sess := adminSessionCookie{
		Sub:   sub,
		Email: email,
		Role:  "instance_admin",
		Exp:   expiresAt.Unix(),
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
		Secure:   isRequestSecure(r),
		SameSite: http.SameSiteLaxMode,
	})
	a.logAuditEvent(r.Context(), sub, "admin_login", "user", sub, nil, "success", "")
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

	// AC6: audit bootstrap completion before creating the session.
	a.logAuditEvent(r.Context(), sub, "bootstrap_completed", "server", "",
		map[string]any{"instance_name": instanceName, "oidc_issuer": oidcIssuer}, "success", "")

	// Create admin session cookie — operator is now authenticated.
	// If a server-side session store is wired, create an SID-based session row (Story 5.12).
	expiresAt := time.Now().Add(8 * time.Hour)
	if a.sessionStore != nil {
		sid, err := a.sessionStore.Create(r.Context(), sub, expiresAt)
		if err != nil {
			slog.Error("claim selection: failed to create admin session", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		sidPayload, err := json.Marshal(adminSessionSIDCookie{SID: sid})
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		maxAge := int(time.Until(expiresAt).Seconds())
		if maxAge <= 0 {
			maxAge = 1
		}
		http.SetCookie(w, &http.Cookie{
			Name:     "admin_session",
			Value:    a.signCookie(sidPayload),
			Path:     "/admin",
			MaxAge:   maxAge,
			HttpOnly: true,
			Secure:   isRequestSecure(r),
			SameSite: http.SameSiteLaxMode,
		})
		http.Redirect(w, r, "/admin/dashboard", http.StatusSeeOther)
		return
	}

	// Legacy stateless cookie (no session store configured).
	sess := adminSessionCookie{
		Sub:   sub,
		Email: email,
		Role:  "instance_admin",
		Exp:   expiresAt.Unix(),
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
		Secure:   isRequestSecure(r),
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/admin/dashboard", http.StatusSeeOther)
}

// LogoutHandler handles GET /admin/logout.
// AC4: Revokes the server-side session (if a store is configured) before clearing the cookie.
// Still returns 302 to /admin/login on success.
func (a *AdminAuth) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	// Extract subject for audit log — fall back to "unknown" if unavailable.
	logoutSub := "unknown"

	// AC4: revoke server-side session before clearing the cookie.
	if a.sessionStore != nil {
		if cookie, err := r.Cookie("admin_session"); err == nil {
			if payload, err := a.verifyCookie(cookie.Value); err == nil {
				var sidCookie adminSessionSIDCookie
				if json.Unmarshal(payload, &sidCookie) == nil && sidCookie.SID != "" {
					if sess, err := a.sessionStore.Get(r.Context(), sidCookie.SID); err == nil && sess != nil && sess.UserID != "" {
						logoutSub = sess.UserID
					}
					if err := a.sessionStore.Revoke(r.Context(), sidCookie.SID); err != nil {
						slog.Warn("logout: failed to revoke session in store", "err", err)
						// Continue to clear the cookie regardless — best-effort revocation.
					}
				}
			}
		}
	} else {
		// Legacy stateless cookie — sub is in the cookie payload.
		if cookie, err := r.Cookie("admin_session"); err == nil {
			if payload, err := a.verifyCookie(cookie.Value); err == nil {
				var legacyCookie adminSessionCookie
				if json.Unmarshal(payload, &legacyCookie) == nil && legacyCookie.Sub != "" {
					logoutSub = legacyCookie.Sub
				}
			}
		}
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "admin_session",
		Value:    "",
		Path:     "/admin",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   isRequestSecure(r),
		SameSite: http.SameSiteStrictMode,
	})
	a.logAuditEvent(r.Context(), logoutSub, "admin_logout", "user", logoutSub, nil, "success", "")
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}
