package matrix

// ─── Story 5-3: POST /_matrix/client/v3/rooms/{roomId}/read_markers ──────────
//
// These tests are written FIRST (ATDD gate), before implementation exists.
// ALL tests in this file are expected to FAIL until read_markers.go is created.
//
// Problem: Element Web calls POST /rooms/{roomId}/read_markers repeatedly to
// persist the "fully read" marker (m.fully_read) alongside the standard read
// receipt. Without this endpoint the client enters a retry loop that produces
// hundreds of "Error sending fully_read" log entries and causes unnecessary
// network load.
//
// Test strategy:
//   - ReadMarkersHandler is a zero-dependency stub handler (no gRPC call in MVP).
//     It accepts any valid JSON body and returns 200 {}.
//   - buildAuthedReadMarkersHandler wires JWTMiddleware → ReadMarkersHandler so
//     the full auth → handler pipeline is exercised at httptest level.
//   - Happy path: POST with {"m.fully_read": "$eventId"} → 200 {}.
//   - Empty body: POST with {} → 200 {} (client sometimes omits optional fields).
//   - Bad JSON body: POST with malformed JSON → 400 M_BAD_JSON.
//   - Unauthenticated: no Bearer → 401 M_MISSING_TOKEN.
//
// NOTE: ReadMarkersHandler, ReadMarkersConfig, NewReadMarkersHandler,
// PostReadMarkers are defined in gateway/internal/matrix/read_markers.go —
// which does NOT exist yet.
// Every test in this file MUST fail with a compilation error until
// read_markers.go is created.

import (
	"bytes"
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

// buildAuthedReadMarkersHandler wires JWTMiddleware → ReadMarkersHandler and
// registers it on a mux with the correct POST pattern so PathValue resolves.
//
// JWT sub is always "test-sub-123", authenticated user_id = "@test-sub-123:test.local".
func buildAuthedReadMarkersHandler(t *testing.T) (http.Handler, *httptest.Server, func() string) {
	t.Helper()

	oidcSrv, privateKey := setupOIDCServer(t)
	t.Cleanup(oidcSrv.Close)

	provider := auth.NewProvider(context.Background(), oidcSrv.URL)

	handler := NewReadMarkersHandler(ReadMarkersConfig{
		ServerName: "test.local",
	})

	authed := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, nil, "test.local")(
		http.HandlerFunc(handler.PostReadMarkers),
	)

	mux := http.NewServeMux()
	mux.Handle("POST /rooms/{roomId}/read_markers", authed)

	makeToken := func() string {
		return signJWT(t, oidcSrv.URL, privateKey, time.Now().Add(time.Hour), nil)
	}

	return mux, oidcSrv, makeToken
}

// ─── Test 1: Happy path — m.fully_read marker ────────────────────────────────
//
// Element Web posts {"m.fully_read": "$eventId", "m.read": "$eventId"} to
// advance the fully-read marker. Handler must return 200 {} and stop the
// client retry loop.

func TestPostReadMarkers_HappyPath(t *testing.T) {
	mux, _, makeToken := buildAuthedReadMarkersHandler(t)

	body := map[string]string{
		"m.fully_read": "$icAX8A-DHKBPotSMEnuI3KZSElcakegTxeVbgCfp9d4",
		"m.read":       "$icAX8A-DHKBPotSMEnuI3KZSElcakegTxeVbgCfp9d4",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(
		http.MethodPost,
		"/rooms/!room1%3Atest.local/read_markers",
		bytes.NewReader(bodyBytes),
	)
	req.Header.Set("Authorization", "Bearer "+makeToken())
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// Response must be {} (empty object), not null or empty string.
	var respBody map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &respBody); err != nil {
		t.Fatalf("response is not valid JSON: %v; body: %s", err, w.Body.String())
	}
}

// ─── Test 2: Empty body → 200 {} ─────────────────────────────────────────────
//
// Some Matrix clients omit the body or send {}. The handler must accept this
// gracefully and return 200 without error.

func TestPostReadMarkers_EmptyBody(t *testing.T) {
	mux, _, makeToken := buildAuthedReadMarkersHandler(t)

	req := httptest.NewRequest(
		http.MethodPost,
		"/rooms/!room1%3Atest.local/read_markers",
		bytes.NewReader([]byte("{}")),
	)
	req.Header.Set("Authorization", "Bearer "+makeToken())
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
}

// ─── Test 3: Malformed JSON → 400 M_BAD_JSON ─────────────────────────────────

func TestPostReadMarkers_BadJSON(t *testing.T) {
	mux, _, makeToken := buildAuthedReadMarkersHandler(t)

	req := httptest.NewRequest(
		http.MethodPost,
		"/rooms/!room1%3Atest.local/read_markers",
		bytes.NewReader([]byte(`{not valid json`)),
	)
	req.Header.Set("Authorization", "Bearer "+makeToken())
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", w.Code, w.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("error response is not valid JSON: %v", err)
	}
	if body["errcode"] != "M_BAD_JSON" {
		t.Errorf("expected errcode M_BAD_JSON, got %v", body["errcode"])
	}
}

// ─── Test 4: Unauthenticated → 401 ───────────────────────────────────────────

func TestPostReadMarkers_Unauthenticated(t *testing.T) {
	mux, _, _ := buildAuthedReadMarkersHandler(t)

	req := httptest.NewRequest(
		http.MethodPost,
		"/rooms/!room1%3Atest.local/read_markers",
		bytes.NewReader([]byte(`{"m.fully_read":"$event"}`)),
	)
	req.Header.Set("Content-Type", "application/json")
	// No Authorization header.

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d; body: %s", w.Code, w.Body.String())
	}
}
