package matrix

// ─── Story 5-1: GET /_matrix/client/v3/user/{userId}/filter/{filterId} ───────
//
// These tests are written FIRST (ATDD gate), before implementation exists.
// ALL tests in this file are expected to FAIL until filter.go is created.
//
// Problem: POST /user/{userId}/filter returns {"filter_id":"0"} (registered in
// main.go). On reconnect, Element Web calls GET /user/{userId}/filter/0 to
// retrieve the filter definition — this endpoint does not exist, causing a 404
// that puts the entire sync loop into ERROR state and prevents any sync from
// completing.
//
// Test strategy:
//   - FilterHandler is a zero-dependency handler (no gRPC, no DB).
//     It returns a static passthrough filter definition for any filter_id.
//   - buildAuthedFilterHandler wires JWTMiddleware → FilterHandler on a mux
//     with the correct GET pattern so r.PathValue("userId") and
//     r.PathValue("filterId") resolve correctly.
//   - Happy path: GET /user/@alex:test.local/filter/0 → 200 with valid JSON
//     filter definition containing the expected fields.
//   - Wrong filter ID: GET /user/@alex:test.local/filter/999 → 200 (no stored
//     filters in MVP; any ID returns the default passthrough definition).
//   - Unauthenticated: request without Authorization → 401 M_MISSING_TOKEN.
//   - Wrong user: userId in URL that doesn't match authenticated user_id → 403.
//
// NOTE: FilterHandler, FilterConfig, NewFilterHandler, GetFilter are
// defined in gateway/internal/matrix/filter.go — which does NOT exist yet.
// Every test in this file MUST fail with a compilation error until filter.go
// is created and FilterHandler is implemented.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nebu/nebu/internal/auth"
	"github.com/nebu/nebu/internal/middleware"
)

// ─── Helper ──────────────────────────────────────────────────────────────────

// buildAuthedFilterHandler wires JWTMiddleware → FilterHandler and registers
// it on a mux with the correct GET pattern so PathValue resolves.
//
// JWT sub is always "test-sub-123", so the authenticated user_id becomes
// "@test-sub-123:test.local".
func buildAuthedFilterHandler(t *testing.T) (http.Handler, *httptest.Server, func() string) {
	t.Helper()

	oidcSrv, privateKey := setupOIDCServer(t)
	t.Cleanup(oidcSrv.Close)

	provider := auth.NewProvider(context.Background(), oidcSrv.URL)

	handler := NewFilterHandler(FilterConfig{
		ServerName: "test.local",
	})

	authed := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, nil, "test.local")(
		http.HandlerFunc(handler.GetFilter),
	)

	mux := http.NewServeMux()
	mux.Handle("GET /user/{userId}/filter/{filterId}", authed)

	// Include "name" claim so FormatUserIDFromClaims produces a deterministic
	// user_id (@filtertest:test.local) rather than a SHA-256 hash fallback.
	makeToken := func() string {
		return signJWT(t, oidcSrv.URL, privateKey, time.Now().Add(time.Hour), map[string]any{
			"name": "filtertest",
		})
	}

	return mux, oidcSrv, makeToken
}

// ─── Test 1: Happy path — filter_id "0" returns passthrough definition ───────
//
// Element Web POSTs a filter, receives filter_id "0", then GETs it on reconnect.
// The response must be valid JSON with a "room" key (or empty object).
// 200 OK — sync loop can proceed.

func TestGetFilter_HappyPath(t *testing.T) {
	mux, _, makeToken := buildAuthedFilterHandler(t)

	req := httptest.NewRequest(
		http.MethodGet,
		"/user/%40filtertest%3Atest.local/filter/0",
		nil,
	)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not valid JSON: %v; body: %s", err, w.Body.String())
	}

	// Must contain the standard filter fields that Element Web validates.
	if _, ok := body["room"]; !ok {
		t.Errorf("expected 'room' key in filter response, got: %v", body)
	}

	ct := w.Header().Get("Content-Type")
	if ct == "" {
		t.Error("expected Content-Type header to be set")
	}
}

// ─── Test 2: Unknown filter ID also returns 200 (stateless MVP) ──────────────
//
// In MVP there is no filter storage. Any filter_id the client requests must
// return the default passthrough definition rather than 404, so that Element Web
// does not crash the sync loop.

func TestGetFilter_UnknownFilterIdReturns200(t *testing.T) {
	mux, _, makeToken := buildAuthedFilterHandler(t)

	req := httptest.NewRequest(
		http.MethodGet,
		"/user/%40filtertest%3Atest.local/filter/999",
		nil,
	)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
}

// ─── Test 3: Unauthenticated request → 401 ───────────────────────────────────
//
// JWTMiddleware must reject requests without a Bearer token before the handler
// is reached. This protects user filter data from unauthenticated access.

func TestGetFilter_Unauthenticated(t *testing.T) {
	mux, _, _ := buildAuthedFilterHandler(t)

	req := httptest.NewRequest(
		http.MethodGet,
		"/user/%40filtertest%3Atest.local/filter/0",
		nil,
	)
	// No Authorization header.

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d; body: %s", w.Code, w.Body.String())
	}
}

// ─── Test 4: User mismatch → 403 ─────────────────────────────────────────────
//
// A user authenticated as @test-sub-123:test.local must not be able to retrieve
// filters for @other-user:test.local. The handler must enforce ownership.

func TestGetFilter_WrongUser_Forbidden(t *testing.T) {
	mux, _, makeToken := buildAuthedFilterHandler(t)

	req := httptest.NewRequest(
		http.MethodGet,
		"/user/%40other-user%3Atest.local/filter/0",
		nil,
	)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d; body: %s", w.Code, w.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("error response is not valid JSON: %v", err)
	}
	if body["errcode"] != "M_FORBIDDEN" {
		t.Errorf("expected errcode M_FORBIDDEN, got: %v", body["errcode"])
	}
}
