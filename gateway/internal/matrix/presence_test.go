package matrix

// ─── Story 4-18: GET /_matrix/client/v3/presence/{userId}/status ─────────────
//
// These tests are written FIRST (ATDD gate), before implementation exists.
// ALL tests in this file are expected to FAIL until Story 4-18 is implemented.
//
// Test strategy:
//   - mockPresenceCoreClient implements PresenceCoreClient (consumer-defined
//     interface, Go convention) — defined here alongside the tests.
//   - buildAuthedPresenceHandler wires JWTMiddleware → PresenceHandler on a mux
//     registered with the correct GET pattern so r.PathValue("userId") resolves
//     correctly (Go 1.22+ standard library routing).
//   - GET /presence/{userId}/status requires JWT auth (AC #4 — authenticated).
//   - Unknown user returns 200 with presence "offline" (NOT 404): Elixir Core never
//     raises not_found for presence — offline is the default (see story technical
//     requirements, IMPORTANT MVP DECISION note).
//   - No-JWT test: omit Authorization header → 401 from JWTMiddleware.
//   - CoreUnavailable test: mock returns a generic error → 503 M_UNAVAILABLE.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nebu/nebu/internal/auth"
	pb "github.com/nebu/nebu/internal/grpc/pb"
	"github.com/nebu/nebu/internal/middleware"
)

// ─── Mock gRPC core client ────────────────────────────────────────────────────

// mockPresenceCoreClient implements PresenceCoreClient (defined in presence.go).

type mockPresenceCoreClient struct {
	resp        *pb.GetPresenceResponse
	err         error
	capturedReq *pb.GetPresenceRequest
}

func (m *mockPresenceCoreClient) GetPresence(_ context.Context, req *pb.GetPresenceRequest) (*pb.GetPresenceResponse, error) {
	m.capturedReq = req
	return m.resp, m.err
}

func (m *mockPresenceCoreClient) SetPresence(_ context.Context, _ *pb.SetPresenceRequest) (*pb.SetPresenceResponse, error) {
	return &pb.SetPresenceResponse{}, nil
}

// ─── Helper ───────────────────────────────────────────────────────────────────

// buildAuthedPresenceHandler wires JWTMiddleware → PresenceHandler and registers
// it on a mux with the correct GET pattern so r.PathValue works correctly.
//
// Returns the http.Handler, the OIDC test server, and a makeToken closure.
// JWT sub is always "test-sub-123" → authenticated user_id "@test-sub-123:test.local".
func buildAuthedPresenceHandler(t *testing.T, mock *mockPresenceCoreClient) (http.Handler, *httptest.Server, func() string) {
	t.Helper()

	oidcSrv, privateKey := setupOIDCServer(t)
	t.Cleanup(oidcSrv.Close)

	provider := auth.NewProvider(context.Background(), oidcSrv.URL)

	handler := NewPresenceHandler(PresenceConfig{
		CoreClient: mock,
		ServerName: "test.local",
	})

	authed := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, "test.local")(
		http.HandlerFunc(handler.GetPresenceStatus),
	)

	mux := http.NewServeMux()
	mux.Handle("GET /presence/{userId}/status", authed)

	makeToken := func() string {
		return signJWT(t, oidcSrv.URL, privateKey, time.Now().Add(time.Hour), nil)
	}

	return mux, oidcSrv, makeToken
}

// ─── Test 1: GET /presence/{userId}/status — online user → 200 ───────────────
//
// AC #4 — Authenticated request; mock returns presence "online", last_active_ago 5000.
// Handler must return 200 with correct JSON fields.

func TestGetPresence_HappyPath(t *testing.T) {
	mock := &mockPresenceCoreClient{
		resp: &pb.GetPresenceResponse{
			Presence:      "online",
			LastActiveAgo: 5000,
		},
	}

	mux, _, makeToken := buildAuthedPresenceHandler(t, mock)

	req := httptest.NewRequest(http.MethodGet, "/presence/@alice:test.local/status", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if resp["presence"] != "online" {
		t.Errorf("expected presence=online, got %v", resp["presence"])
	}
	// last_active_ago arrives as float64 in JSON decode.
	if resp["last_active_ago"] != float64(5000) {
		t.Errorf("expected last_active_ago=5000, got %v", resp["last_active_ago"])
	}
}

// ─── Test 2: GET /presence/{userId}/status — unknown user → 200 offline ──────
//
// AC #4 / IMPORTANT MVP DECISION — Elixir Core's get_presence/2 never returns
// not_found: unknown users default to offline. The Go handler must return 200
// {"presence": "offline"} and NOT 404 for users Core has never seen.
//
// This test uses a mock that returns presence "offline" (simulating the Core
// default path for unknown users). The handler must NOT attempt a 404 mapping
// for this case.
//
// AT10 in story was revised: per MVP decision, unknown users return offline (200) not 404.

func TestGetPresence_UnknownUser_ReturnsOffline(t *testing.T) {
	mock := &mockPresenceCoreClient{
		resp: &pb.GetPresenceResponse{
			Presence:      "offline",
			LastActiveAgo: 0,
		},
	}

	mux, _, makeToken := buildAuthedPresenceHandler(t, mock)

	req := httptest.NewRequest(http.MethodGet, "/presence/@unknown:test.local/status", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// Must be 200 with offline — NOT 404 (Core never sends not_found for presence).
	if w.Code == http.StatusNotFound {
		t.Fatalf("GET /presence for unknown user must return 200 offline, not 404 — see story MVP DECISION")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if resp["presence"] != "offline" {
		t.Errorf("expected presence=offline for unknown user, got %v", resp["presence"])
	}
}

// ─── Test 3: GET /presence/{userId}/status — no JWT → 401 ────────────────────
//
// AC #4 — GET /presence requires authentication. JWTMiddleware must reject
// requests missing the Authorization header.

func TestGetPresence_NoJWT_Allowed(t *testing.T) {
	mock := &mockPresenceCoreClient{
		resp: &pb.GetPresenceResponse{
			Presence:      "online",
			LastActiveAgo: 1000,
		},
	}

	mux, _, _ := buildAuthedPresenceHandler(t, mock)

	req := httptest.NewRequest(http.MethodGet, "/presence/@alice:test.local/status", nil)
	// Deliberately omit Authorization header.

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// GET /presence IS authenticated (unlike GET /profile). Must be 401.
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated GET /presence, got %d; body: %s", w.Code, w.Body.String())
	}

	// Core must not have been called.
	if mock.capturedReq != nil {
		t.Error("expected GetPresence NOT to be called on unauthenticated request, but capturedReq is set")
	}
}

// ─── Test 4: GET /presence/{userId}/status — Core unavailable → 503 ──────────
//
// AC #4 — When gRPC Core returns a generic internal error, the handler must
// return 503 M_UNAVAILABLE (service unavailable), not 500 or a Go panic.

func TestGetPresence_CoreUnavailable(t *testing.T) {
	mock := &mockPresenceCoreClient{
		err: errors.New("connection refused"),
	}

	mux, _, makeToken := buildAuthedPresenceHandler(t, mock)

	req := httptest.NewRequest(http.MethodGet, "/presence/@alice:test.local/status", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixError
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.ErrCode != "M_UNAVAILABLE" {
		t.Errorf("expected errcode M_UNAVAILABLE, got %s", errResp.ErrCode)
	}
}
