package matrix

// ─── Story 5.29e Bug 1: POST /_matrix/client/v3/rooms/{roomId}/upgrade ─────────
//
// ATDD RED PHASE — these tests MUST FAIL until the upgrade handler is implemented.
//
// Scope boundary (5-29e): register a 501 Not Implemented stub per Matrix spec §10.2.7.
// Full room-upgrade implementation (new room, tombstone event, state copy) is a
// follow-up story. The stub must:
//   - Be registered at POST /_matrix/client/v3/rooms/{roomId}/upgrade.
//   - Return HTTP 501 with body {"errcode":"M_UNRECOGNIZED","error":"Room upgrade not yet supported"}.
//   - Return 401 M_MISSING_TOKEN when JWT is absent (JWTMiddleware wraps the handler).
//   - Return 400 M_BAD_JSON when the body is missing "new_version".
//
// Source: tmp/test-findings.md, 2026-04-23 —
//   "Aktualisiere auf die empfohlene Chat-Version" → 404 page not found.
//
// RED PHASE mechanism:
//   These tests reference UpgradeRoomHandler and NewUpgradeRoomHandler which do NOT
//   exist yet in rooms.go. The package will FAIL TO COMPILE until those types are
//   defined, which is the canonical red-phase signal for a new handler story.
//
// Test strategy:
//   - mockUpgradeRoomCoreClient — minimal interface stub (empty; upgrade stub needs no gRPC).
//   - buildAuthedUpgradeRoomHandler — wires JWTMiddleware + ServeMux so r.PathValue("roomId") works.
//   - Tests follow the same pattern as TestPostCreateRoom_* in rooms_test.go.

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

// ─── RED PHASE: UpgradeRoomHandler does not exist yet ────────────────────────────
//
// The following type references will cause a COMPILE ERROR until Story 5-29e is
// implemented. This is the intended red-phase signal.
//
// Once implemented, UpgradeRoomHandler must live in gateway/internal/matrix/rooms.go
// (or rooms_upgrade.go) and be registered in cmd/gateway/main.go.

// mockUpgradeRoomCoreClient is a placeholder. The 501-stub handler needs no gRPC
// calls, but the interface satisfies the NewUpgradeRoomHandler config pattern.
type mockUpgradeRoomCoreClient struct{}

// buildAuthedUpgradeRoomHandler wires UpgradeRoomHandler in JWTMiddleware on a
// ServeMux so r.PathValue("roomId") is populated by the Go 1.22+ router.
//
// RED PHASE: will not compile until UpgradeRoomHandler is defined.
func buildAuthedUpgradeRoomHandler(t *testing.T) (http.Handler, func() string) {
	t.Helper()

	oidcSrv, privateKey := setupOIDCServer(t)
	t.Cleanup(oidcSrv.Close)

	provider := auth.NewProvider(context.Background(), oidcSrv.URL)

	// ── RED PHASE compile-error beacon ────────────────────────────────────────
	// NewUpgradeRoomHandler does not exist yet — this line will not compile.
	// Implement UpgradeRoomHandler in rooms.go / rooms_upgrade.go, then this
	// helper (and all tests below) will move from RED to GREEN once the spec is met.
	handler := NewUpgradeRoomHandler(UpgradeRoomConfig{
		ServerName: "test.local",
	})

	authed := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, "test.local")(
		http.HandlerFunc(handler.PostUpgradeRoom),
	)

	mux := http.NewServeMux()
	mux.Handle("POST /_matrix/client/v3/rooms/{roomId}/upgrade", authed)

	makeToken := func() string {
		return signJWT(t, oidcSrv.URL, privateKey, time.Now().Add(time.Hour), nil)
	}

	return mux, makeToken
}

// ─── Test 1: Happy-path stub — valid JWT + new_version → 501 M_UNRECOGNIZED ─────
//
// AC1 (5-29e scope): endpoint registered → HTTP 501 with spec-conformant JSON body.
// Not 404 (current state = route missing). Not empty body or HTML.
func TestUpgradeRoom_StubReturns501_NotImplemented(t *testing.T) {
	mux, makeToken := buildAuthedUpgradeRoomHandler(t)

	body := `{"new_version":"10"}`
	req := httptest.NewRequest(http.MethodPost,
		"/_matrix/client/v3/rooms/!room1:test.local/upgrade",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// Must NOT be 404 — the route must be registered.
	if w.Code == http.StatusNotFound {
		t.Fatalf("upgrade endpoint must be registered (not return 404); "+
			"currently 404 because route is missing from main.go — RED PHASE CONFIRMED")
	}

	if w.Code != http.StatusNotImplemented {
		t.Errorf("expected 501 for upgrade stub, got %d; body: %s", w.Code, w.Body.String())
	}

	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}

	var errResp matrixError
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("501 body must be valid JSON Matrix error: %v; body: %s", err, w.Body.String())
	}
	if errResp.ErrCode != "M_UNRECOGNIZED" {
		t.Errorf("expected errcode M_UNRECOGNIZED, got %q", errResp.ErrCode)
	}
	if errResp.Err == "" {
		t.Error("501 response must include a non-empty human-readable error message")
	}
}

// ─── Test 2: Missing new_version → 400 M_BAD_JSON ────────────────────────────────
//
// AC1 (spec §10.2.7): empty body or body without "new_version" must return 400.
func TestUpgradeRoom_MissingNewVersion_Returns400(t *testing.T) {
	mux, makeToken := buildAuthedUpgradeRoomHandler(t)

	// Body deliberately omits "new_version".
	req := httptest.NewRequest(http.MethodPost,
		"/_matrix/client/v3/rooms/!room1:test.local/upgrade",
		strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing new_version, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixError
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.ErrCode != "M_BAD_JSON" {
		t.Errorf("expected errcode M_BAD_JSON, got %s", errResp.ErrCode)
	}
}

// ─── Test 3: No JWT → 401 M_MISSING_TOKEN ────────────────────────────────────────
//
// AC1: JWTMiddleware must reject upgrade requests without an Authorization header.
func TestUpgradeRoom_NoAuth_Returns401(t *testing.T) {
	mux, _ := buildAuthedUpgradeRoomHandler(t)

	req := httptest.NewRequest(http.MethodPost,
		"/_matrix/client/v3/rooms/!room1:test.local/upgrade",
		strings.NewReader(`{"new_version":"10"}`))
	req.Header.Set("Content-Type", "application/json")
	// Deliberately omit Authorization header.

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixError
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.ErrCode != "M_MISSING_TOKEN" {
		t.Errorf("expected errcode M_MISSING_TOKEN, got %s", errResp.ErrCode)
	}
}

// ─── Test 4: Invalid roomId → 400 M_BAD_JSON ─────────────────────────────────────
//
// AC1: handler must reject malformed Matrix room IDs (e.g., "notaroomid") with
// 400 M_BAD_JSON before attempting to decode the body. Closes MINOR-1 from TEA review:
// the ValidateMatrixRoomID branch in the handler was code-only, not test-covered.
func TestUpgradeRoom_InvalidRoomID_Returns400(t *testing.T) {
	mux, makeToken := buildAuthedUpgradeRoomHandler(t)

	// "notaroomid" lacks the required '!' prefix and ':server' suffix.
	req := httptest.NewRequest(http.MethodPost,
		"/_matrix/client/v3/rooms/notaroomid/upgrade",
		strings.NewReader(`{"new_version":"10"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid room ID, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixError
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.ErrCode != "M_BAD_JSON" {
		t.Errorf("expected errcode M_BAD_JSON, got %s", errResp.ErrCode)
	}
}

// ─── Test 5: Malformed JSON body → 400 M_BAD_JSON ────────────────────────────────
//
// Spec: handler must reject syntactically invalid JSON with 400 before reading new_version.
func TestUpgradeRoom_MalformedJSON_Returns400(t *testing.T) {
	mux, makeToken := buildAuthedUpgradeRoomHandler(t)

	req := httptest.NewRequest(http.MethodPost,
		"/_matrix/client/v3/rooms/!room1:test.local/upgrade",
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
