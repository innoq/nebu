package admin

// Story 9.14: Admin UI — OIDC Token Refresh (Silent Session Renewal) — Acceptance Tests
//
// RED PHASE — these tests MUST fail until the feature is implemented.
//
// AC coverage:
//   AC2  — TestOfflineAccessScopeInCallback
//   AC3  — TestCallbackHandlerStoresEncryptedRefreshToken
//   AC5a — TestSilentRefreshPreExpiryWindow (session valid but within 5-min window)
//   AC5b — TestSilentRefreshExtendSession (just-expired, grace window)
//   AC5c — TestSilentRefreshFailsRedirectsToLogin
//   AC5d — TestNoRefreshTokenRedirectsToLogin
//   AC8  — TestNoSessionCookieNoRefreshAttempt (bootstrap/unauthenticated state)
//   AC9  — TestSilentRefreshAuditLogEntry
//
// What causes each test to fail right now:
//   TestSilentRefreshExtendSession:
//     - AdminSession does not have EncryptedRefreshToken field (AC4) → t.Fatal at runtime
//     - AdminSessionStore interface lacks UpdateExpiry method (AC6) → compile error on UpdateExpiry
//     - SessionGuardWithStore has no silent-refresh logic (AC5) → would redirect 302 even if field existed
//   TestSilentRefreshFailsRedirectsToLogin:
//     - Same EncryptedRefreshToken / UpdateExpiry / refresh-logic gaps → t.Fatal at runtime
//   TestNoRefreshTokenRedirectsToLogin:
//     - Same EncryptedRefreshToken / refresh-logic gaps → t.Fatal at runtime
//   TestOfflineAccessScopeInCallback:
//     - LoginStartHandler does not include "offline_access" in scopes (AC2) → assertion fails
//
// Compile-time contract:
//   fakeAdminSessionStoreWithRefresh implements the AdminSessionStore interface.
//   UpdateExpiry is declared on the fake — the interface must gain this method before
//   the fake can satisfy it. Until then the file compiles because the fake is a concrete
//   type that satisfies the interface via a compile-time check below.

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/nebu/nebu/internal/auth"
)

// ---------------------------------------------------------------------------
// fakeAdminSessionStoreWithRefresh — extended fake that records UpdateExpiry
// calls. This type satisfies AdminSessionStore AND declares UpdateExpiry.
//
// AC-FAIL: AdminSessionStore interface does not have UpdateExpiry yet (AC6).
//          Once the interface gains UpdateExpiry, this fake satisfies it.
//          Until then this is a concrete struct — it will compile but the
//          interface check below will produce a build error, which is intentional.
// ---------------------------------------------------------------------------

type fakeAdminSessionStoreWithRefresh struct {
	mu                  sync.Mutex
	sessions            map[string]*AdminSession
	updateExpiryCalled  bool
	updateExpirySID     string
	updateExpiryTime    time.Time
	updateExpiryEncRT   string
	revokeCalled        bool
	revokeSID           string
}

// Compile-time interface check — AdminSessionStore.UpdateExpiry is now implemented.
var _ AdminSessionStore = (*fakeAdminSessionStoreWithRefresh)(nil)

func newFakeStoreWithRefresh() *fakeAdminSessionStoreWithRefresh {
	return &fakeAdminSessionStoreWithRefresh{
		sessions: make(map[string]*AdminSession),
	}
}

func (f *fakeAdminSessionStoreWithRefresh) seedBasic(sess AdminSession) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := sess
	f.sessions[sess.SID] = &cp
}

// Create satisfies the AdminSessionStore.Create signature.
func (f *fakeAdminSessionStoreWithRefresh) Create(ctx context.Context, userID string, expiresAt time.Time, refreshToken string) (string, error) {
	sid := "refresh-test-sid-" + userID
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions[sid] = &AdminSession{
		SID:                   sid,
		UserID:                userID,
		ExpiresAt:             expiresAt,
		EncryptedRefreshToken: refreshToken,
	}
	return sid, nil
}

func (f *fakeAdminSessionStoreWithRefresh) Get(ctx context.Context, sid string) (*AdminSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sess, ok := f.sessions[sid]
	if !ok {
		return nil, nil
	}
	cp := *sess
	return &cp, nil
}

func (f *fakeAdminSessionStoreWithRefresh) Revoke(ctx context.Context, sid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.revokeCalled = true
	f.revokeSID = sid
	sess, ok := f.sessions[sid]
	if ok {
		now := time.Now()
		sess.RevokedAt = &now
	}
	return nil
}

// UpdateExpiry is the new method required by AC6.
// AC-FAIL: AdminSessionStore interface does not have UpdateExpiry yet.
//          This method is defined here so that the fake is ready once the
//          interface gains UpdateExpiry. The concrete method compiles fine.
func (f *fakeAdminSessionStoreWithRefresh) UpdateExpiry(ctx context.Context, sid string, expiresAt time.Time, encryptedRefreshToken string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updateExpiryCalled = true
	f.updateExpirySID = sid
	f.updateExpiryTime = expiresAt
	f.updateExpiryEncRT = encryptedRefreshToken
	sess, ok := f.sessions[sid]
	if !ok {
		return fmt.Errorf("session %q not found", sid)
	}
	sess.ExpiresAt = expiresAt
	return nil
}

// ---------------------------------------------------------------------------
// Helper: buildRefreshTokenOIDCServer — returns 200 from /token with a new
// access_token and refresh_token (simulates successful Dex refresh).
// ---------------------------------------------------------------------------

func buildRefreshTokenOIDCServer(t *testing.T) *httptest.Server {
	t.Helper()
	var serverURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                 serverURL,
			"authorization_endpoint": serverURL + "/authorize",
			"token_endpoint":         serverURL + "/token",
			"jwks_uri":               serverURL + "/keys",
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-access-token",
			"refresh_token": "new-refresh-token",
			"token_type":    "Bearer",
			"expires_in":    3600,
		})
	})
	srv := httptest.NewServer(mux)
	serverURL = srv.URL
	t.Cleanup(srv.Close)
	return srv
}

// buildFailingRefreshOIDCServer returns 400 invalid_grant from /token.
func buildFailingRefreshOIDCServer(t *testing.T) *httptest.Server {
	t.Helper()
	var serverURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                 serverURL,
			"authorization_endpoint": serverURL + "/authorize",
			"token_endpoint":         serverURL + "/token",
			"jwks_uri":               serverURL + "/keys",
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"Refresh token expired"}`))
	})
	srv := httptest.NewServer(mux)
	serverURL = srv.URL
	t.Cleanup(srv.Close)
	return srv
}

// buildSIDCookieForRefreshTest builds a signed admin_session SID cookie.
func buildSIDCookieForRefreshTest(t *testing.T, secret []byte, sid string) string {
	t.Helper()
	a := &AdminAuth{secret: secret}
	payload, err := json.Marshal(adminSessionSIDCookie{SID: sid})
	if err != nil {
		t.Fatalf("json.Marshal adminSessionSIDCookie: %v", err)
	}
	return a.signCookie(payload)
}

// ---------------------------------------------------------------------------
// Test 1 — AC5, AC6
//
// TestSilentRefreshExtendSession
//
// Given: An admin session with EncryptedRefreshToken set and ExpiresAt = now-10s.
// When:  A request hits a guarded admin route.
// Then:  store.UpdateExpiry is called with a future ExpiresAt; response is 200.
//
// AC-FAIL: AdminSession.EncryptedRefreshToken does not exist (AC4) — test calls
//          t.Fatal immediately. Once AC4 is implemented, SessionGuardWithStore
//          still redirects (no refresh logic) → fails on UpdateExpiry check and
//          200 assertion.
// ---------------------------------------------------------------------------

func TestSilentRefreshExtendSession(t *testing.T) {
	secret := []byte("test-secret-key-32-bytes-for-aes")
	tokenSrv := buildRefreshTokenOIDCServer(t)

	encryptedRT, err := encryptAES256GCM(secret, "valid-refresh-token")
	if err != nil {
		t.Fatalf("encryptAES256GCM: %v", err)
	}

	const sid = "refresh-sid-test1"
	store := newFakeStoreWithRefresh()
	store.seedBasic(AdminSession{
		SID:                   sid,
		UserID:                "admin-user-1",
		ExpiresAt:             time.Now().Add(-10 * time.Second),
		EncryptedRefreshToken: encryptedRT,
	})

	cookieValue := buildSIDCookieForRefreshTest(t, secret, sid)

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	cfgReader := &fakeServerConfigReader{
		issuer:       tokenSrv.URL,
		clientID:     "test-client-id",
		clientSecret: "test-client-secret",
	}

	handler := SessionGuardWithRefresh(SessionRefreshConfig{
		Secret:       secret,
		Store:        store,
		ConfigReader: cfgReader,
	})(next)

	req := httptest.NewRequest("GET", "/admin/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: "admin_session", Value: cookieValue})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	store.mu.Lock()
	updateCalled := store.updateExpiryCalled
	updateTime := store.updateExpiryTime
	store.mu.Unlock()

	if !updateCalled {
		t.Error("AC5/AC6 FAIL: store.UpdateExpiry was NOT called — " +
			"SessionGuardWithRefresh must call UpdateExpiry when silent refresh succeeds")
	}
	if updateCalled && !updateTime.After(time.Now()) {
		t.Errorf("AC5/AC6 FAIL: UpdateExpiry called with past time %v — must be future", updateTime)
	}
	if rr.Code != http.StatusOK {
		t.Errorf("AC5 FAIL: expected 200 after successful silent refresh, got %d", rr.Code)
	}
	if !nextCalled {
		t.Error("AC5 FAIL: next handler was NOT called after successful refresh")
	}
}

// ---------------------------------------------------------------------------
// Test 2 — AC5
//
// TestSilentRefreshFailsRedirectsToLogin
//
// Given: Session with EncryptedRefreshToken, ExpiresAt in the past.
//        OIDC token endpoint returns 400 invalid_grant.
// When:  Request hits a guarded admin route.
// Then:  store.Revoke called; 302 to /admin/login.
//
// AC-FAIL: AdminSession.EncryptedRefreshToken field missing → t.Fatal.
// ---------------------------------------------------------------------------

func TestSilentRefreshFailsRedirectsToLogin(t *testing.T) {
	secret := []byte("test-secret-key-32-bytes-for-aes")
	failingSrv := buildFailingRefreshOIDCServer(t)

	encryptedRT, err := encryptAES256GCM(secret, "expired-refresh-token")
	if err != nil {
		t.Fatalf("encryptAES256GCM: %v", err)
	}

	const sid = "refresh-sid-test2"
	store := newFakeStoreWithRefresh()
	store.seedBasic(AdminSession{
		SID:                   sid,
		UserID:                "admin-user-2",
		ExpiresAt:             time.Now().Add(-30 * time.Second),
		EncryptedRefreshToken: encryptedRT,
	})

	cookieValue := buildSIDCookieForRefreshTest(t, secret, sid)

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	cfgReader := &fakeServerConfigReader{
		issuer:       failingSrv.URL,
		clientID:     "test-client-id",
		clientSecret: "test-client-secret",
	}

	handler := SessionGuardWithRefresh(SessionRefreshConfig{
		Secret:       secret,
		Store:        store,
		ConfigReader: cfgReader,
	})(next)

	req := httptest.NewRequest("GET", "/admin/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: "admin_session", Value: cookieValue})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	store.mu.Lock()
	revokeCalled := store.revokeCalled
	revokeSID := store.revokeSID
	store.mu.Unlock()

	if !revokeCalled {
		t.Error("AC5 FAIL: store.Revoke was NOT called after failed token refresh — " +
			"SessionGuardWithRefresh must revoke the session when invalid_grant is returned")
	}
	if revokeCalled && revokeSID != sid {
		t.Errorf("AC5 FAIL: store.Revoke called with wrong SID %q, want %q", revokeSID, sid)
	}
	if rr.Code != http.StatusFound {
		t.Errorf("AC5 FAIL: expected 302 after failed token refresh, got %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/admin/login" {
		t.Errorf("AC5 FAIL: expected redirect to /admin/login, got %q", loc)
	}
	if nextCalled {
		t.Error("AC5 FAIL: next handler called after failed refresh — must redirect instead")
	}
}

// ---------------------------------------------------------------------------
// Test 3 — AC5
//
// TestNoRefreshTokenRedirectsToLogin
//
// Given: Session with empty EncryptedRefreshToken + ExpiresAt in the past.
// When:  Request hits a guarded admin route.
// Then:  302 to /admin/login; store.UpdateExpiry NOT called.
//
// AC-FAIL: AdminSession.EncryptedRefreshToken field missing → t.Fatal.
//          Once AC4 exists and is empty, the guard must NOT attempt refresh.
// ---------------------------------------------------------------------------

func TestNoRefreshTokenRedirectsToLogin(t *testing.T) {
	secret := []byte("test-secret-key-32-bytes-for-aes")

	const sid = "no-refresh-sid-test3"
	store := newFakeStoreWithRefresh()
	store.seedBasic(AdminSession{
		SID:                   sid,
		UserID:                "admin-user-3",
		ExpiresAt:             time.Now().Add(-60 * time.Second),
		EncryptedRefreshToken: "", // empty = no refresh token stored
	})

	cookieValue := buildSIDCookieForRefreshTest(t, secret, sid)

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	handler := SessionGuardWithRefresh(SessionRefreshConfig{
		Secret: secret,
		Store:  store,
	})(next)

	req := httptest.NewRequest("GET", "/admin/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: "admin_session", Value: cookieValue})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusFound {
		t.Errorf("AC5 FAIL: expected 302 for expired session with no refresh token, got %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/admin/login" {
		t.Errorf("AC5 FAIL: expected redirect to /admin/login, got %q", loc)
	}

	store.mu.Lock()
	updateCalled := store.updateExpiryCalled
	store.mu.Unlock()

	if updateCalled {
		t.Error("AC5 FAIL: store.UpdateExpiry was called even though EncryptedRefreshToken is empty — " +
			"no refresh should be attempted when there is no stored refresh token")
	}
	if nextCalled {
		t.Error("AC5 FAIL: next handler called for expired session with no refresh token — must redirect")
	}
}

// ---------------------------------------------------------------------------
// Test 5 — AC2
//
// TestOfflineAccessScopeInCallback
//
// Given: LoginStartHandler builds the OIDC auth URL.
// When:  GET /admin/login/start.
// Then:  Location URL's scope parameter contains "offline_access".
//
// AC-FAIL: LoginStartHandler scopes are currently
//          []string{oidc.ScopeOpenID, "profile", "email", "groups"}
//          — "offline_access" is absent (AC2). This test fails at the assertion,
//          not at compile time, so it produces a clear FAIL message today.
// ---------------------------------------------------------------------------

func TestOfflineAccessScopeInCallback(t *testing.T) {
	var discoveryServerURL string
	discoveryMux := http.NewServeMux()
	discoveryMux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                 discoveryServerURL,
			"authorization_endpoint": discoveryServerURL + "/authorize",
			"token_endpoint":         discoveryServerURL + "/token",
			"jwks_uri":               discoveryServerURL + "/keys",
		})
	})
	discoverySrv := httptest.NewServer(discoveryMux)
	discoveryServerURL = discoverySrv.URL
	t.Cleanup(discoverySrv.Close)

	a := NewAdminAuth(nil, "test-client-id", "test-client-secret", "nebu_role",
		[]byte("test-secret-key"), nil, nil)
	a.configReader = &fakeServerConfigReader{
		issuer:       discoveryServerURL,
		clientID:     "test-client-id",
		clientSecret: "test-client-secret",
	}

	req := httptest.NewRequest("GET", "/admin/login/start", nil)
	req.Host = "localhost"
	rr := httptest.NewRecorder()
	a.LoginStartHandler(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("AC2 FAIL: expected 302 from LoginStartHandler, got %d (body: %q)",
			rr.Code, rr.Body.String())
	}

	location := rr.Header().Get("Location")
	if location == "" {
		t.Fatal("AC2 FAIL: Location header is empty")
	}

	parsed, err := url.Parse(location)
	if err != nil {
		t.Fatalf("AC2 FAIL: cannot parse Location URL %q: %v", location, err)
	}

	scopeParam := parsed.Query().Get("scope")
	if scopeParam == "" {
		t.Fatalf("AC2 FAIL: scope parameter missing from OIDC redirect URL: %s", location)
	}

	scopes := strings.Split(scopeParam, " ")
	for _, s := range scopes {
		if s == "offline_access" {
			return // test passes
		}
	}

	t.Errorf("AC2 FAIL: scope parameter %q does not include 'offline_access' — "+
		"add 'offline_access' to the Scopes slice in LoginStartHandler (auth.go). "+
		"Current scopes: %v", scopeParam, scopes)
}

// ---------------------------------------------------------------------------
// MAJOR-1 fix: AC3 — Refresh token stored ENCRYPTED in CallbackHandler
// ---------------------------------------------------------------------------

// buildOIDCServerWithRefreshToken returns a signed JWT id_token + refresh_token
// in the /token response. Used to verify CallbackHandler encrypts the refresh token.
func buildOIDCServerWithRefreshToken(t *testing.T) (*httptest.Server, *rsa.PrivateKey) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	jwk := jose.JSONWebKey{
		Key:       &privateKey.PublicKey,
		KeyID:     "test-key-1", // must match kid header in signAdminJWT
		Algorithm: string(jose.RS256),
		Use:       "sig",
	}
	jwks := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{jwk}}
	var serverURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                 serverURL,
			"authorization_endpoint": serverURL + "/authorize",
			"token_endpoint":         serverURL + "/token",
			"jwks_uri":               serverURL + "/keys",
		})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jwks)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		idToken := signAdminJWT(t, serverURL, privateKey, time.Now().Add(time.Hour), "nebu_role", "instance_admin")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "mock-access-token",
			"token_type":    "Bearer",
			"id_token":      idToken,
			"refresh_token": "raw-test-refresh-token", // the raw plaintext token
		})
	})
	srv := httptest.NewServer(mux)
	serverURL = srv.URL
	t.Cleanup(srv.Close)
	return srv, privateKey
}

// TestCallbackHandlerStoresEncryptedRefreshToken verifies AC3:
// The CallbackHandler encrypts the refresh_token before storing it.
// The stored EncryptedRefreshToken must NOT equal the plaintext and must decrypt correctly.
func TestCallbackHandlerStoresEncryptedRefreshToken(t *testing.T) {
	const rawRefreshToken = "raw-test-refresh-token"
	secret := []byte("test-secret-key-32bytes-for-aes-!")

	srv, _ := buildOIDCServerWithRefreshToken(t)
	provider := auth.NewProvider(context.Background(), srv.URL)

	store := newFakeAdminSessionStore()
	a := NewAdminAuth(provider, "test-client-id", "test-client-secret", "nebu_role", secret, nil, nil)
	a.configReader = &fakeServerConfigReader{issuer: srv.URL, clientID: "test-client-id", clientSecret: "test-client-secret"}
	a.SetSessionStore(store)

	cookieValue := buildValidStateCookie(t, a, "state123")
	req := httptest.NewRequest("GET", "/admin/callback?code=abc&state=state123", nil)
	req.Host = "localhost"
	req.AddCookie(&http.Cookie{Name: "admin_oidc_state", Value: cookieValue})
	rr := httptest.NewRecorder()
	a.CallbackHandler(rr, req)

	if rr.Code != 303 {
		t.Fatalf("AC3: CallbackHandler expected 303, got %d (body: %q)", rr.Code, rr.Body.String())
	}

	// Retrieve the created session from the fake store.
	store.mu.Lock()
	var storedEncRT string
	for _, sess := range store.sessions {
		storedEncRT = sess.EncryptedRefreshToken
	}
	store.mu.Unlock()

	if storedEncRT == "" {
		t.Fatal("AC3 FAIL: EncryptedRefreshToken is empty — CallbackHandler must encrypt and store the refresh_token")
	}
	if storedEncRT == rawRefreshToken {
		t.Errorf("AC3 FAIL: EncryptedRefreshToken equals plaintext %q — MUST be encrypted with AES-256-GCM", rawRefreshToken)
	}
	decrypted, err := decryptAES256GCM(secret, storedEncRT)
	if err != nil {
		t.Errorf("AC3 FAIL: decryptAES256GCM failed on stored token: %v", err)
	}
	if decrypted != rawRefreshToken {
		t.Errorf("AC3 FAIL: decrypted token %q != expected %q", decrypted, rawRefreshToken)
	}
}

// ---------------------------------------------------------------------------
// MAJOR-2 fix: AC5 pre-expiry window — session still valid but within 5 min
// ---------------------------------------------------------------------------

// TestSilentRefreshPreExpiryWindow verifies that SessionGuardWithRefresh proactively
// refreshes a session that is still valid but will expire within 5 minutes (AC5).
func TestSilentRefreshPreExpiryWindow(t *testing.T) {
	secret := []byte("test-secret-key-32-bytes-for-aes")
	tokenSrv := buildRefreshTokenOIDCServer(t)

	encryptedRT, err := encryptAES256GCM(secret, "valid-refresh-token-preexpiry")
	if err != nil {
		t.Fatalf("encryptAES256GCM: %v", err)
	}

	const sid = "preexpiry-sid-test"
	store := newFakeStoreWithRefresh()
	// Session expires in 2 minutes — within the 5-minute window.
	store.seedBasic(AdminSession{
		SID:                   sid,
		UserID:                "admin-user-preexpiry",
		ExpiresAt:             time.Now().Add(2 * time.Minute),
		EncryptedRefreshToken: encryptedRT,
	})

	cookieValue := buildSIDCookieForRefreshTest(t, secret, sid)

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	handler := SessionGuardWithRefresh(SessionRefreshConfig{
		Secret: secret,
		Store:  store,
		ConfigReader: &fakeServerConfigReader{
			issuer:       tokenSrv.URL,
			clientID:     "test-client-id",
			clientSecret: "test-client-secret",
		},
	})(next)

	req := httptest.NewRequest("GET", "/admin/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: "admin_session", Value: cookieValue})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	store.mu.Lock()
	updateCalled := store.updateExpiryCalled
	store.mu.Unlock()

	if !updateCalled {
		t.Error("AC5 FAIL (pre-expiry): store.UpdateExpiry NOT called for session expiring in 2 minutes — " +
			"SessionGuardWithRefresh must proactively refresh sessions within the 5-minute window")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("AC5 FAIL (pre-expiry): expected 200 after proactive refresh, got %d", rr.Code)
	}
	if !nextCalled {
		t.Error("AC5 FAIL (pre-expiry): next handler was NOT called after proactive refresh")
	}
}

// ---------------------------------------------------------------------------
// MAJOR-3 fix: AC8 — no refresh attempted when no session exists (bootstrap state)
// ---------------------------------------------------------------------------

// TestNoSessionCookieNoRefreshAttempt verifies AC8:
// When there is no admin session cookie (the unauthenticated / bootstrap state),
// SessionGuardWithRefresh must redirect to /admin/login without attempting a token refresh.
func TestNoSessionCookieNoRefreshAttempt(t *testing.T) {
	secret := []byte("test-secret-key-32-bytes-for-aes")
	store := newFakeStoreWithRefresh()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := SessionGuardWithRefresh(SessionRefreshConfig{
		Secret: secret,
		Store:  store,
	})(next)

	req := httptest.NewRequest("GET", "/admin/dashboard", nil)
	// No admin_session cookie — simulates bootstrap / unauthenticated state.
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusFound {
		t.Errorf("AC8 FAIL: expected 302 when no session cookie (bootstrap state), got %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/admin/login" {
		t.Errorf("AC8 FAIL: expected redirect to /admin/login, got %q", loc)
	}

	store.mu.Lock()
	updateCalled := store.updateExpiryCalled
	store.mu.Unlock()

	if updateCalled {
		t.Error("AC8 FAIL: store.UpdateExpiry called without a session — must not attempt refresh in bootstrap/unauthenticated state")
	}
}

// ---------------------------------------------------------------------------
// MAJOR-4 fix: AC9 — audit log admin_session_refreshed emitted on success
// ---------------------------------------------------------------------------
// Uses mockCoreClient from auth_audit_test.go (same package — no redeclaration needed).

// TestSilentRefreshAuditLogEntry verifies AC9:
// A successful silent refresh emits an admin_session_refreshed audit event with correct fields.
// An audit failure (gRPC error) must NOT block the refresh — the request still succeeds.
func TestSilentRefreshAuditLogEntry(t *testing.T) {
	secret := []byte("test-secret-key-32-bytes-for-aes")
	tokenSrv := buildRefreshTokenOIDCServer(t)

	encryptedRT, err := encryptAES256GCM(secret, "audit-test-refresh-token")
	if err != nil {
		t.Fatalf("encryptAES256GCM: %v", err)
	}

	const sid = "audit-sid-test9"
	store := newFakeStoreWithRefresh()
	store.seedBasic(AdminSession{
		SID:                   sid,
		UserID:                "audit-user-id",
		ExpiresAt:             time.Now().Add(-10 * time.Second),
		EncryptedRefreshToken: encryptedRT,
	})

	cookieValue := buildSIDCookieForRefreshTest(t, secret, sid)
	mockClient := &mockCoreClient{}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	handler := SessionGuardWithRefresh(SessionRefreshConfig{
		Secret: secret,
		Store:  store,
		ConfigReader: &fakeServerConfigReader{
			issuer:       tokenSrv.URL,
			clientID:     "test-client-id",
			clientSecret: "test-client-secret",
		},
		CoreClient: mockClient,
	})(next)

	req := httptest.NewRequest("GET", "/admin/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: "admin_session", Value: cookieValue})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("AC9: expected 200, got %d", rr.Code)
	}

	mockClient.mu.Lock()
	received := mockClient.received
	mockClient.mu.Unlock()

	if len(received) == 0 {
		t.Fatal("AC9 FAIL: WriteAuditLog was NOT called after successful silent refresh — " +
			"SessionGuardWithRefresh must emit admin_session_refreshed audit event")
	}
	ev := received[0]
	if ev.Action != "admin_session_refreshed" {
		t.Errorf("AC9 FAIL: audit event Action = %q, want 'admin_session_refreshed'", ev.Action)
	}
	if ev.ActorUserId != "audit-user-id" {
		t.Errorf("AC9 FAIL: audit event ActorUserId = %q, want 'audit-user-id'", ev.ActorUserId)
	}
	if ev.TargetId != sid {
		t.Errorf("AC9 FAIL: audit event TargetId = %q, want %q", ev.TargetId, sid)
	}
}
