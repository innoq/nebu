package admin

// Story 5.12: Server-side Admin Session Store + Revocation — Acceptance Tests
//
// ATDD Gate: These tests encode Acceptance Criteria 2–4 and 7 from Story 5.12.
// They MUST fail until the implementation is complete (Red Phase).
//
// Design decision:
//   All four tests use an injectable AdminSessionStore interface rather than a
//   real PostgreSQL connection. This follows the runInTx injection pattern from
//   Story 5.11 — production wires a *postgresAdminSessionStore; tests inject a
//   fakeAdminSessionStore that records calls in-memory.
//
// What causes the tests to fail initially:
//   1. AdminSessionStore interface does not exist yet
//      (gateway/internal/db/admin_session_store.go is not created).
//   2. AdminAuth has no sessionStore field.
//   3. LogoutHandler does not call sessionStore.Revoke.
//   4. SessionGuard does not accept/look-up a session store.
//   5. CallbackHandler does not cap expires_at to idToken.Exp.
//
// Acceptance Criteria covered:
//   AC2: CallbackHandler stores session in AdminSessionStore on success.
//   AC3: SessionGuard looks up sid — row not found / revoked / expired → 302 to /admin/login.
//   AC4: LogoutHandler calls sessionStore.Revoke(sid) before clearing cookie.
//   AC7: Unit tests listed above.

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

// ---------------------------------------------------------------------------
// AdminSessionStore interface and AdminSession struct are now defined in
// gateway/internal/admin/session_store.go (production code).
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// fakeAdminSessionStore — in-memory implementation used by all four tests.
// ---------------------------------------------------------------------------

type fakeAdminSessionStore struct {
	mu       sync.Mutex
	sessions map[string]*AdminSession
	// createErr, if non-nil, is returned by Create.
	createErr error
}

func newFakeAdminSessionStore() *fakeAdminSessionStore {
	return &fakeAdminSessionStore{sessions: make(map[string]*AdminSession)}
}

// seed pre-populates the store with a session (for guard / revocation tests).
func (f *fakeAdminSessionStore) seed(sess AdminSession) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := sess
	f.sessions[sess.SID] = &cp
}

func (f *fakeAdminSessionStore) Create(ctx context.Context, userID string, expiresAt time.Time, refreshToken string) (string, error) {
	if f.createErr != nil {
		return "", f.createErr
	}
	// Generate a deterministic fake SID for tests.
	sid := "test-sid-" + userID
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

func (f *fakeAdminSessionStore) UpdateExpiry(ctx context.Context, sid string, expiresAt time.Time, encryptedRefreshToken string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sess, ok := f.sessions[sid]
	if !ok {
		return nil
	}
	sess.ExpiresAt = expiresAt
	sess.EncryptedRefreshToken = encryptedRefreshToken
	return nil
}

func (f *fakeAdminSessionStore) Get(ctx context.Context, sid string) (*AdminSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sess, ok := f.sessions[sid]
	if !ok {
		return nil, nil
	}
	cp := *sess
	return &cp, nil
}

func (f *fakeAdminSessionStore) Revoke(ctx context.Context, sid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	sess, ok := f.sessions[sid]
	if !ok {
		return nil
	}
	now := time.Now()
	sess.RevokedAt = &now
	return nil
}

// isRevoked is a test helper that checks in-memory state.
func (f *fakeAdminSessionStore) isRevoked(sid string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	sess, ok := f.sessions[sid]
	if !ok {
		return false
	}
	return sess.RevokedAt != nil
}

// ---------------------------------------------------------------------------
// Helper: buildSignedSIDCookie creates a signed admin_session cookie whose
// payload contains only the SID (the new format expected by Story 5.12).
//
// The cookie format after 5.12:
//   { "sid": "<sid>" }
// (user_id and roles are no longer stored in the cookie — they come from the DB)
//
// adminSessionSIDCookie is now defined in gateway/internal/admin/auth.go.
// ---------------------------------------------------------------------------

func buildSignedSIDCookie(t *testing.T, a *AdminAuth, sid string) string {
	t.Helper()
	payload, err := json.Marshal(adminSessionSIDCookie{SID: sid})
	if err != nil {
		t.Fatalf("json.Marshal adminSessionSIDCookie: %v", err)
	}
	return a.signCookie(payload)
}

// ---------------------------------------------------------------------------
// newTestAdminAuthWithSessionStore creates an AdminAuth pre-wired with a fake
// session store. It uses the existing NewAdminAuth constructor (db=nil) and then
// injects the store field — the field does not exist yet, which causes this
// helper to produce a compile error until the implementation adds it.
// ---------------------------------------------------------------------------

func newTestAdminAuthWithSessionStore(t *testing.T, store AdminSessionStore) *AdminAuth {
	t.Helper()
	a := NewAdminAuth(nil, "test-client-id", "test-client-secret", "nebu_role",
		[]byte("test-secret-key"), nil, nil)
	// AC-FAIL: AdminAuth.sessionStore field does not exist yet.
	// This line will not compile until Story 5.12 implementation adds it.
	a.sessionStore = store
	return a
}

// ---------------------------------------------------------------------------
// AC4 / AC7 — TestLogout_RevokesSessionInDB
//
// Given: an authenticated admin session with sid="test-sid-admin-user"
//        stored in fakeAdminSessionStore
// When:  GET /admin/logout is called with the signed session cookie
// Then:  (a) LogoutHandler calls Revoke(sid) — store.isRevoked(sid) == true
//        (b) HTTP 302 redirect to /admin/login (existing logout behavior)
//        (c) admin_session cookie is cleared (MaxAge <= 0)
// ---------------------------------------------------------------------------

func TestLogout_RevokesSessionInDB(t *testing.T) {
	const sid = "test-sid-admin-user"

	store := newFakeAdminSessionStore()
	store.seed(AdminSession{
		SID:       sid,
		UserID:    "admin-user",
		ExpiresAt: time.Now().Add(8 * time.Hour),
	})

	a := newTestAdminAuthWithSessionStore(t, store)
	cookieValue := buildSignedSIDCookie(t, a, sid)

	req := httptest.NewRequest("GET", "/admin/logout", nil)
	req.Host = "localhost"
	req.AddCookie(&http.Cookie{Name: "admin_session", Value: cookieValue})
	rr := httptest.NewRecorder()
	a.LogoutHandler(rr, req)

	// AC4: revoked_at must be set in the store.
	if !store.isRevoked(sid) {
		t.Error("AC4 FAIL: LogoutHandler did not call Revoke(sid) — " +
			"session is still valid in the store after logout. " +
			"(Expected: AdminSessionStore.Revoke(ctx, sid) is called before clearing the cookie)")
	}

	// Existing behavior: must still redirect 302 to /admin/login (AC4 spec: "Still returns 302").
	if rr.Code != http.StatusSeeOther && rr.Code != http.StatusFound {
		t.Errorf("expected 302/303 redirect on logout, got %d", rr.Code)
	}

	// Cookie must be cleared.
	var sessionCookie *http.Cookie
	for _, c := range rr.Result().Cookies() {
		if c.Name == "admin_session" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("admin_session cookie not present in response (expected deletion cookie)")
	}
	if sessionCookie.MaxAge > 0 {
		t.Errorf("expected admin_session MaxAge <= 0 (deletion), got %d", sessionCookie.MaxAge)
	}
}

// ---------------------------------------------------------------------------
// AC3 / AC7 — TestSessionGuard_RejectsRevokedSID
//
// Given: admin_sessions row with revoked_at=NOW() (SID is revoked)
//        admin_session cookie contains that SID
// When:  GET /admin/dashboard
// Then:  HTTP 302 to /admin/login (next handler must NOT be called)
// ---------------------------------------------------------------------------

func TestSessionGuard_RejectsRevokedSID(t *testing.T) {
	const sid = "revoked-sid"

	store := newFakeAdminSessionStore()
	revokedAt := time.Now()
	store.seed(AdminSession{
		SID:       sid,
		UserID:    "admin-user",
		ExpiresAt: time.Now().Add(8 * time.Hour),
		RevokedAt: &revokedAt,
	})

	secret := []byte("test-secret-key")
	a := &AdminAuth{secret: secret, sessionStore: store}
	cookieValue := buildSignedSIDCookie(t, a, sid)

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	// AC-FAIL: SessionGuard does not accept a store parameter yet.
	// The new signature will be: SessionGuardWithStore(secret []byte, store AdminSessionStore)
	handler := SessionGuardWithStore(secret, store)(next)

	req := httptest.NewRequest("GET", "/admin/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: "admin_session", Value: cookieValue})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusFound {
		t.Errorf("AC3 FAIL: expected 302 for revoked SID, got %d — "+
			"SessionGuard must check revoked_at IS NOT NULL", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/admin/login" {
		t.Errorf("AC3 FAIL: expected redirect to /admin/login, got %q", loc)
	}
	if nextCalled {
		t.Error("AC3 FAIL: next handler must NOT be called for a revoked session")
	}
}

// ---------------------------------------------------------------------------
// AC3 / AC7 — TestSessionGuard_RejectsNotFoundSID
//
// Given: a fakeAdminSessionStore with no sessions (empty store)
//        admin_session cookie contains a valid HMAC-signed SID
// When:  GET /admin/dashboard
// Then:  HTTP 302 to /admin/login (next handler must NOT be called)
//
// NOTE: This tests the "row not found" path in SessionGuardWithStore —
//       the SID is cryptographically valid but not present in the store.
// ---------------------------------------------------------------------------

func TestSessionGuard_RejectsNotFoundSID(t *testing.T) {
	const sid = "unknown-sid"

	// Empty store — no sessions seeded.
	store := newFakeAdminSessionStore()

	secret := []byte("test-secret-key")
	a := &AdminAuth{secret: secret, sessionStore: store}
	cookieValue := buildSignedSIDCookie(t, a, sid)

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	handler := SessionGuardWithStore(secret, store)(next)

	req := httptest.NewRequest("GET", "/admin/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: "admin_session", Value: cookieValue})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusFound {
		t.Errorf("AC3 FAIL: expected 302 for not-found SID, got %d — "+
			"SessionGuard must redirect when SID row is not in the store", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/admin/login" {
		t.Errorf("AC3 FAIL: expected redirect to /admin/login, got %q", loc)
	}
	if nextCalled {
		t.Error("AC3 FAIL: next handler must NOT be called when SID is not found in the store")
	}
}

// ---------------------------------------------------------------------------
// AC3 / AC7 — TestSessionGuard_RejectsExpiredSID
//
// Given: admin_sessions row with expires_at < NOW() (SID is expired in DB)
//        admin_session cookie contains that SID and is HMAC-valid (cookie itself not expired)
// When:  GET /admin/dashboard
// Then:  HTTP 302 to /admin/login (next handler must NOT be called)
//
// NOTE: This tests DB-level expiry, not cookie-level expiry.
//        The cookie's own Exp field may still be valid; the DB row's expires_at governs.
// ---------------------------------------------------------------------------

func TestSessionGuard_RejectsExpiredSID(t *testing.T) {
	const sid = "expired-sid"

	store := newFakeAdminSessionStore()
	store.seed(AdminSession{
		SID:       sid,
		UserID:    "admin-user",
		ExpiresAt: time.Now().Add(-1 * time.Second), // expired 1 second ago
	})

	secret := []byte("test-secret-key")
	a := &AdminAuth{secret: secret, sessionStore: store}
	cookieValue := buildSignedSIDCookie(t, a, sid)

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	handler := SessionGuardWithStore(secret, store)(next)

	req := httptest.NewRequest("GET", "/admin/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: "admin_session", Value: cookieValue})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusFound {
		t.Errorf("AC3 FAIL: expected 302 for expired SID, got %d — "+
			"SessionGuard must check expires_at < NOW()", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/admin/login" {
		t.Errorf("AC3 FAIL: expected redirect to /admin/login, got %q", loc)
	}
	if nextCalled {
		t.Error("AC3 FAIL: next handler must NOT be called for an expired session")
	}
}

// ---------------------------------------------------------------------------
// AC6 / AC7 — TestCallback_ExpiryCappedByIDTokenExp
//
// Given: an OIDC server that returns an ID token expiring in 30 minutes
//        (shorter than the default 8h session window)
// When:  the OIDC callback completes successfully
// Then:  the session inserted into the AdminSessionStore has
//        expires_at <= idToken.Exp   (i.e. it was capped to the token lifetime)
//        AND expires_at > now        (i.e. the session is not immediately expired)
//
// The test uses the existing fake OIDC server infrastructure (setupAdminOIDCServer)
// and an OIDC server variant that returns a short-lived token.
// ---------------------------------------------------------------------------

func TestCallback_ExpiryCappedByIDTokenExp(t *testing.T) {
	// Build a fake OIDC server whose token endpoint returns a JWT expiring in 30 minutes.
	// That is shorter than the 8h cap → expires_at must equal idToken.Exp.
	tokenExp := time.Now().Add(30 * time.Minute)
	srv := setupAdminOIDCServerWithExpiry(t, tokenExp)

	store := newFakeAdminSessionStore()
	a := newTestAdminAuthWithSessionStore(t, store)
	a.configReader = &fakeServerConfigReader{
		issuer:       srv.URL,
		clientID:     "test-client-id",
		clientSecret: "test-client-secret",
	}

	cookieValue := buildValidStateCookie(t, a, "mystate")
	req := httptest.NewRequest("GET", "/admin/callback?code=abc&state=mystate", nil)
	req.Host = "localhost"
	req.AddCookie(&http.Cookie{Name: "admin_oidc_state", Value: cookieValue})
	rr := httptest.NewRecorder()
	a.CallbackHandler(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 SeeOther on success, got %d (body: %q)",
			rr.Code, rr.Body.String())
	}

	// Find the session that was created in the store.
	store.mu.Lock()
	var created *AdminSession
	for _, s := range store.sessions {
		created = s
		break
	}
	store.mu.Unlock()

	if created == nil {
		t.Fatal("AC6 FAIL: CallbackHandler did not insert a session into AdminSessionStore")
	}

	// AC6: expires_at must be <= idToken.Exp (capped to token lifetime).
	// Allow a 5-second slack for processing time.
	capDeadline := tokenExp.Add(5 * time.Second)
	if created.ExpiresAt.After(capDeadline) {
		t.Errorf("AC6 FAIL: session expires_at %v is after idToken.Exp %v — "+
			"CallbackHandler must cap expires_at = min(idToken.Exp, now+8h)",
			created.ExpiresAt, tokenExp)
	}

	// The session must not be immediately expired.
	if !created.ExpiresAt.After(time.Now()) {
		t.Errorf("AC6 FAIL: session expires_at %v is in the past — session was created already expired",
			created.ExpiresAt)
	}
}

// ---------------------------------------------------------------------------
// setupAdminOIDCServerWithExpiry is a variant of setupAdminOIDCServer that
// returns a JWT expiring at the given time (instead of the default 1h).
// This is required by TestCallback_ExpiryCappedByIDTokenExp.
//
// The server reuses the RSA key and sign helper from auth_test.go (same package).
// The /token endpoint calls signAdminJWT with the caller-supplied expiry.
// ---------------------------------------------------------------------------

func setupAdminOIDCServerWithExpiry(t *testing.T, expiry time.Time) *httptest.Server {
	t.Helper()

	// Generate a fresh RSA key pair for this server.
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}

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

	// JWKS endpoint — expose the same public key used by signAdminJWT.
	// jose types are imported in auth_test.go (same package).
	mux.HandleFunc("/keys", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// signAdminJWT uses kid="test-key-1" and RS256 — replicate here.
		_ = json.NewEncoder(w).Encode(buildJWKSForTestKey(privateKey))
	})

	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		// Use the caller-supplied expiry — the key difference from setupAdminOIDCServer.
		idToken := signAdminJWT(t, serverURL, privateKey, expiry, "nebu_role", "instance_admin")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "mock-access-token",
			"token_type":   "Bearer",
			"id_token":     idToken,
		})
	})

	srv := httptest.NewServer(mux)
	serverURL = srv.URL
	t.Cleanup(srv.Close)
	return srv
}

// buildJWKSForTestKey returns a jose.JSONWebKeySet containing the public half of
// the given RSA key, using the same kid="test-key-1" / RS256 / use="sig" attributes
// that signAdminJWT embeds in tokens produced by the test OIDC servers.
func buildJWKSForTestKey(privateKey *rsa.PrivateKey) jose.JSONWebKeySet {
	jwk := jose.JSONWebKey{
		Key:       &privateKey.PublicKey,
		KeyID:     "test-key-1",
		Algorithm: string(jose.RS256),
		Use:       "sig",
	}
	return jose.JSONWebKeySet{Keys: []jose.JSONWebKey{jwk}}
}
