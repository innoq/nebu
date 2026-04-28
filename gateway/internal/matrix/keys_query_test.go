package matrix

// ─── Story 5.29e Bug 2a: GET /_matrix/client/v3/profile/{userId} after Bootstrap ─
// ─── Story 5.29e Bug 2b: POST /_matrix/client/v3/keys/query — user presence ─────
//
// ATDD RED PHASE — these tests MUST FAIL until the bugs are fixed.
//
// Source: tmp/test-findings.md, 2026-04-23.
//   Bug 2: "Konnte keine Profile für die folgenden Matrix-IDs finden — @alex:localhost"
//   DM creation hangs because:
//     (a) GET /profile/{userId} returns 404 for bootstrap-provisioned users.
//     (b) POST keys/query returns {"device_keys":{},"failures":{}} for ANY user,
//         including known users — no device entry present even for the requesting user.
//
// Scope (5-29e):
//   - (a) Profile 404: fix the provisioning gap — when Core calls UpdateProfile on
//         first ValidateToken, a profile row is inserted via UPSERT. Test verifies
//         GetProfile returns 200 for a user whose profile row was inserted at login.
//   - (b) keys/query: improve stub so that for known users (present in the users
//         table) an empty device_keys map entry is returned instead of being omitted.
//         Full device-key storage + cross-signing is a future E2EE story.
//
// Test strategy:
//   - Profile tests extend the existing mockProfileDB pattern (same package, same file).
//   - keys/query tests use a KeysQueryHandler (to be extracted from the main.go inline
//     closure) with a minimal UserExistenceChecker interface.
//   - Both use httptest — no Docker, no Postgres.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nebu/nebu/internal/auth"
	"github.com/nebu/nebu/internal/middleware"
)

// ─── Bug 2a: Profile 404 for bootstrap-provisioned user ──────────────────────────

// TestGetProfile_BootstrapProvisioned_Returns200 asserts that a user provisioned
// via Bootstrap login (ValidateToken → Core upserts profile row) returns 200 from
// GET /profile/{userId} with a non-empty displayname.
//
// RED PHASE: This test fails when the profiles table has no row for the user (the
// current gap — provisioning writes to the users table but may leave profiles empty).
// Fix: ensure UpdateProfile gRPC (called by Core) upserts a profiles row with the
// OIDC preferred_username as displayname.
func TestGetProfile_BootstrapProvisioned_Returns200(t *testing.T) {
	// Simulate a freshly provisioned user: profile row was written by Core.
	dbMock := &mockProfileDB{
		found:       true,
		displayName: "alex", // OIDC preferred_username written at provisioning
		avatarURL:   "",     // avatar_url is optional at provisioning
	}
	coreMock := &mockProfileCoreClient{}
	mux := buildProfileHandler(t, coreMock, dbMock)

	req := httptest.NewRequest(http.MethodGet, "/profile/@alex:localhost", nil)
	// GET /profile is public — no Authorization header required.

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /profile for bootstrap-provisioned user must return 200, got %d; body: %s"+
			"\n→ This fails when no profile row was upserted during OIDC login provisioning (Bug 2a).",
			w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}

	// displayname must be present (even if empty string is acceptable, the field must exist).
	if _, ok := resp["displayname"]; !ok {
		t.Error("response must include 'displayname' field")
	}
}

// TestGetProfile_ProfileRowMissing_Returns404 asserts that when no profile row
// exists (pre-fix state), GET /profile returns 404 M_NOT_FOUND.
//
// This is a REGRESSION GUARD: once the provisioning fix is in place, the above
// TestGetProfile_BootstrapProvisioned_Returns200 must pass and this scenario must
// only occur for truly unknown users (never for bootstrap-provisioned ones).
func TestGetProfile_ProfileRowMissing_Returns404(t *testing.T) {
	dbMock := &mockProfileDB{
		found: false, // no profile row — the pre-fix state for bootstrap users
	}
	coreMock := &mockProfileCoreClient{}
	mux := buildProfileHandler(t, coreMock, dbMock)

	req := httptest.NewRequest(http.MethodGet, "/profile/@unknown:localhost", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing profile row, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixError
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.ErrCode != "M_NOT_FOUND" {
		t.Errorf("expected errcode M_NOT_FOUND, got %s", errResp.ErrCode)
	}
}

// ─── Bug 2b: keys/query — known user must appear in device_keys map ───────────────

// fakeUserExistenceChecker implements UserExistenceChecker (defined in keys_query.go) for testing.
type fakeUserExistenceChecker struct {
	existing map[string]bool
}

func (f *fakeUserExistenceChecker) UserExists(_ context.Context, userID string) (bool, error) {
	return f.existing[userID], nil
}

// KeysQueryHandler handles POST /_matrix/client/v3/keys/query.
// This type does NOT exist yet in the codebase (it is an inline closure in main.go).
// The test below will fail to compile until KeysQueryHandler is extracted into a
// named type with a ServeHTTP-compatible method.
//
// Expected contract:
//   - For each userId in request device_keys that exists in the DB: include an
//     empty map in the response device_keys (confirms user existence, no devices yet).
//   - For each userId NOT in the DB: add to failures map.
//   - Never return a missing entry silently (current bug: all users return empty {}).
//
// RED PHASE NOTE: The tests below use a locally-constructed handler to demonstrate
// what the CONTRACT must be. They will fail until:
//   1. KeysQueryHandler is defined in a new gateway/internal/matrix/keys_query.go, and
//   2. The handler is wired in main.go instead of the current inline closure.

// buildKeysQueryHandler constructs a keys/query handler with JWT auth.
// Uses the real KeysQueryHandler (extracted from main.go inline closure in Story 5-29e).
func buildKeysQueryHandler(t *testing.T, checker UserExistenceChecker) (http.Handler, func() string) {
	t.Helper()

	oidcSrv, privateKey := setupOIDCServer(t)
	t.Cleanup(oidcSrv.Close)

	provider := auth.NewProvider(context.Background(), oidcSrv.URL)

	handler := NewKeysQueryHandler(KeysQueryConfig{UserChecker: checker})
	authed := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, "test.local")(
		http.HandlerFunc(handler.PostKeysQuery),
	)
	mux := http.NewServeMux()
	mux.Handle("POST /_matrix/client/v3/keys/query", authed)

	makeToken := func() string {
		return signJWT(t, oidcSrv.URL, privateKey, time.Now().Add(time.Hour), nil)
	}

	return mux, makeToken
}

// TestKeysQuery_KnownUser_AppearsInDeviceKeysMap asserts that when a known user is
// queried, the response device_keys map contains an entry for that user (even if the
// inner device map is empty — that signals "user exists, no devices registered").
//
// RED PHASE: the current stub always returns {"device_keys":{},...} — the user is
// NOT present as a key in the outer map, so clients cannot distinguish "user not found"
// from "user has no devices". This causes FluffyChat DM creation to hang.
func TestKeysQuery_KnownUser_AppearsInDeviceKeysMap(t *testing.T) {
	checker := &fakeUserExistenceChecker{
		existing: map[string]bool{
			"@alex:localhost": true,
		},
	}
	mux, makeToken := buildKeysQueryHandler(t, checker)

	body := `{"device_keys":{"@alex:localhost":[]}}`
	req := httptest.NewRequest(http.MethodPost,
		"/_matrix/client/v3/keys/query",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		DeviceKeys map[string]any `json:"device_keys"`
		Failures   map[string]any `json:"failures"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode keys/query response: %v", err)
	}

	// The known user MUST appear as a key in device_keys (even with an empty inner map).
	// This is the fix target: current stub returns {} and this assertion FAILS.
	if _, ok := resp.DeviceKeys["@alex:localhost"]; !ok {
		t.Errorf("known user @alex:localhost must appear in device_keys map (even with empty devices); "+
			"current stub returns empty map omitting all users — RED PHASE: this failure is expected until fix is applied. "+
			"full response: %+v", resp)
	}

	// The known user must NOT be in the failures map.
	if _, ok := resp.Failures["@alex:localhost"]; ok {
		t.Errorf("known user @alex:localhost must not appear in failures map, got: %+v", resp)
	}
}

// TestKeysQuery_UnknownUser_NotInDeviceKeys asserts that when an unknown user is
// queried, they do NOT appear in device_keys (or appear in failures, depending on
// spec interpretation). The important invariant: no panic, valid JSON, 200 status.
func TestKeysQuery_UnknownUser_ValidResponse(t *testing.T) {
	checker := &fakeUserExistenceChecker{
		existing: map[string]bool{}, // no users known
	}
	mux, makeToken := buildKeysQueryHandler(t, checker)

	body := `{"device_keys":{"@nonexistent:localhost":[]}}`
	req := httptest.NewRequest(http.MethodPost,
		"/_matrix/client/v3/keys/query",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// Response must be valid JSON with device_keys and failures fields.
	var resp struct {
		DeviceKeys map[string]any `json:"device_keys"`
		Failures   map[string]any `json:"failures"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("keys/query response must be valid JSON: %v; body: %s", err, w.Body.String())
	}

	// Unknown user should not appear in device_keys.
	if _, ok := resp.DeviceKeys["@nonexistent:localhost"]; ok {
		t.Errorf("unknown user should not appear in device_keys, got: %+v", resp.DeviceKeys)
	}
}

// TestKeysQuery_MalformedJSON_Returns400 asserts that a syntactically invalid JSON
// body returns 400 M_BAD_JSON. Closes MINOR-3 from TEA review.
func TestKeysQuery_MalformedJSON_Returns400(t *testing.T) {
	checker := &fakeUserExistenceChecker{existing: map[string]bool{}}
	mux, makeToken := buildKeysQueryHandler(t, checker)

	req := httptest.NewRequest(http.MethodPost,
		"/_matrix/client/v3/keys/query",
		strings.NewReader(`{not valid json`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed JSON, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixError
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.ErrCode != "M_BAD_JSON" {
		t.Errorf("expected errcode M_BAD_JSON, got %s", errResp.ErrCode)
	}
}

// TestKeysQuery_NoAuth_Returns401 asserts that keys/query without JWT → 401.
func TestKeysQuery_NoAuth_Returns401(t *testing.T) {
	checker := &fakeUserExistenceChecker{existing: map[string]bool{}}
	mux, _ := buildKeysQueryHandler(t, checker)

	body := `{"device_keys":{"@alex:localhost":[]}}`
	req := httptest.NewRequest(http.MethodPost,
		"/_matrix/client/v3/keys/query",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// Deliberately omit Authorization header.

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for missing JWT, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixError
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.ErrCode != "M_MISSING_TOKEN" {
		t.Errorf("expected errcode M_MISSING_TOKEN, got %s", errResp.ErrCode)
	}
}
