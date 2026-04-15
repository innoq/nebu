package matrix

// ─── Story 4-17: PUT /_matrix/client/v3/rooms/{roomId}/typing/{userId} ────────
//
// These tests are written FIRST (ATDD gate), before implementation exists.
// ALL tests in this file are expected to FAIL until Story 4-17 is implemented.
//
// Test strategy:
//   - mockTypingCoreClient implements TypingCoreClient (consumer-defined interface,
//     Go convention) — defined here alongside the tests.
//   - buildAuthedTypingHandler wires JWTMiddleware → TypingHandler in a mux
//     registered with the correct PUT pattern so r.PathValue("roomId") and
//     r.PathValue("userId") resolve correctly (Go 1.22+ standard library routing).
//   - A capturedReq field lets tests inspect the gRPC SetTypingRequest forwarded by
//     the handler (room_id, user_id, typing, timeout_ms).
//   - The userId-mismatch test sends a request where the path userId differs from
//     the JWT sub; the handler must return 403 BEFORE calling Core.
//   - The unauthenticated test deliberately omits the Authorization header;
//     JWTMiddleware must return 401 before the handler is reached.
//   - The room-not-found test uses gRPC status NotFound; the handler maps to 404.
//   - The non-member test uses gRPC status PermissionDenied; the handler maps to 403.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nebu/nebu/internal/auth"
	pb "github.com/nebu/nebu/internal/grpc/pb"
	"github.com/nebu/nebu/internal/middleware"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ─── Mock gRPC core client ────────────────────────────────────────────────────

// mockTypingCoreClient implements TypingCoreClient (defined in typing.go).
// capturedReq records the last SetTypingRequest forwarded so tests can assert
// the handler built the correct gRPC payload.

type mockTypingCoreClient struct {
	resp        *pb.SetTypingResponse
	err         error
	capturedReq *pb.SetTypingRequest
}

func (m *mockTypingCoreClient) SetTyping(_ context.Context, req *pb.SetTypingRequest) (*pb.SetTypingResponse, error) {
	m.capturedReq = req
	return m.resp, m.err
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// buildAuthedTypingHandler wires JWTMiddleware → TypingHandler and registers
// it on a mux with the correct PUT pattern so r.PathValue works correctly.
//
// Returns the http.Handler ready for httptest, the OIDC test server, and a
// makeToken closure that mints a valid signed JWT each time it is called.
//
// The JWT sub is always "test-sub-123", so the authenticated user_id is
// "@test-sub-123:test.local". Tests that need to exercise the happy-path
// must set userId to "@test-sub-123:test.local" in the path.
func buildAuthedTypingHandler(t *testing.T, mock *mockTypingCoreClient) (http.Handler, *httptest.Server, func() string) {
	t.Helper()

	oidcSrv, privateKey := setupOIDCServer(t)
	t.Cleanup(oidcSrv.Close)

	provider := auth.NewProvider(context.Background(), oidcSrv.URL)

	handler := NewTypingHandler(TypingConfig{
		CoreClient: mock,
		ServerName: "test.local",
	})

	authed := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, "test.local")(
		http.HandlerFunc(handler.PutTyping),
	)

	// Wrap in a mux with named path parameters so PathValue resolves.
	mux := http.NewServeMux()
	mux.Handle("PUT /rooms/{roomId}/typing/{userId}", authed)

	makeToken := func() string {
		return signJWT(t, oidcSrv.URL, privateKey, time.Now().Add(time.Hour), nil)
	}

	return mux, oidcSrv, makeToken
}

// ─── Test 1: Happy path — typing=true, authenticated user ────────────────────
//
// AC #1 — PUT with {"typing": true, "timeout": 30000} and a valid JWT where the
// path userId matches the authenticated user_id → 200 {} response.
// The mock receives SetTypingRequest{room_id, user_id, typing: true, timeout_ms: 30000}.

func TestPutTyping_HappyPath_True(t *testing.T) {
	mock := &mockTypingCoreClient{
		resp: &pb.SetTypingResponse{},
	}

	mux, _, makeToken := buildAuthedTypingHandler(t, mock)

	body := `{"typing": true, "timeout": 30000}`
	// Path userId must match the JWT sub ("test-sub-123") → "@test-sub-123:test.local".
	req := httptest.NewRequest(
		http.MethodPut,
		"/rooms/!room1:test.local/typing/@test-sub-123:test.local",
		strings.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")
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
	if len(resp) != 0 {
		t.Errorf("expected empty JSON object {}, got %v", resp)
	}

	// Assert the gRPC request was built correctly.
	if mock.capturedReq == nil {
		t.Fatal("expected SetTyping to be called, but capturedReq is nil")
	}
	if mock.capturedReq.RoomId != "!room1:test.local" {
		t.Errorf("expected room_id %q, got %q", "!room1:test.local", mock.capturedReq.RoomId)
	}
	if mock.capturedReq.UserId != "@test-sub-123:test.local" {
		t.Errorf("expected user_id %q, got %q", "@test-sub-123:test.local", mock.capturedReq.UserId)
	}
	if !mock.capturedReq.Typing {
		t.Errorf("expected typing=true in gRPC request, got false")
	}
	if mock.capturedReq.TimeoutMs != 30000 {
		t.Errorf("expected timeout_ms=30000, got %d", mock.capturedReq.TimeoutMs)
	}
}

// ─── Test 2: Timeout clamped to 30000ms ──────────────────────────────────────
//
// MAJOR-2 — PUT with {"typing": true, "timeout": 60000} (exceeds the 30000ms
// maximum) → 200 {}; handler must clamp timeout_ms to 30000 before forwarding
// the gRPC request to Core.

func TestPutTyping_TimeoutClamped(t *testing.T) {
	mock := &mockTypingCoreClient{
		resp: &pb.SetTypingResponse{},
	}

	mux, _, makeToken := buildAuthedTypingHandler(t, mock)

	// 60000ms exceeds the 30000ms maximum — must be clamped.
	body := `{"typing": true, "timeout": 60000}`
	req := httptest.NewRequest(
		http.MethodPut,
		"/rooms/!room1:test.local/typing/@test-sub-123:test.local",
		strings.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// Assert the gRPC request has timeout_ms clamped to 30000, not 60000.
	if mock.capturedReq == nil {
		t.Fatal("expected SetTyping to be called, but capturedReq is nil")
	}
	if mock.capturedReq.TimeoutMs != 30000 {
		t.Errorf("expected timeout_ms clamped to 30000, got %d", mock.capturedReq.TimeoutMs)
	}
	if !mock.capturedReq.Typing {
		t.Errorf("expected typing=true in gRPC request, got false")
	}
}

// ─── Test 3 (was 2): Happy path — typing=false ───────────────────────────────
//
// AC #1 — PUT with {"typing": false} → 200 {}; timeout_ms must be 0 when
// typing=false (handler must override any supplied timeout with 0 per spec).

func TestPutTyping_HappyPath_False(t *testing.T) {
	mock := &mockTypingCoreClient{
		resp: &pb.SetTypingResponse{},
	}

	mux, _, makeToken := buildAuthedTypingHandler(t, mock)

	body := `{"typing": false}`
	req := httptest.NewRequest(
		http.MethodPut,
		"/rooms/!room1:test.local/typing/@test-sub-123:test.local",
		strings.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	if mock.capturedReq == nil {
		t.Fatal("expected SetTyping to be called, but capturedReq is nil")
	}
	if mock.capturedReq.Typing {
		t.Errorf("expected typing=false in gRPC request, got true")
	}
	// When typing=false, timeout_ms must be 0 regardless of any body value.
	if mock.capturedReq.TimeoutMs != 0 {
		t.Errorf("expected timeout_ms=0 when typing=false, got %d", mock.capturedReq.TimeoutMs)
	}
}

// ─── Test 3: Unauthenticated request → 401 ───────────────────────────────────
//
// AC #1 — JWTMiddleware must reject requests missing Authorization header.
// Core must NOT be called.

func TestPutTyping_Unauthenticated(t *testing.T) {
	mock := &mockTypingCoreClient{
		resp: &pb.SetTypingResponse{},
	}

	mux, _, _ := buildAuthedTypingHandler(t, mock)

	req := httptest.NewRequest(
		http.MethodPut,
		"/rooms/!room1:test.local/typing/@test-sub-123:test.local",
		strings.NewReader(`{"typing": true, "timeout": 5000}`),
	)
	req.Header.Set("Content-Type", "application/json")
	// Deliberately omit Authorization header.

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d; body: %s", w.Code, w.Body.String())
	}

	// Core must not have been called.
	if mock.capturedReq != nil {
		t.Error("expected SetTyping NOT to be called on unauthenticated request, but capturedReq is set")
	}
}

// ─── Test 4: Room not found → 404 ────────────────────────────────────────────
//
// AC #1 — when gRPC Core returns codes.NotFound, the handler must return 404
// with errcode M_NOT_FOUND (or similar — handler maps NotFound → 404).

func TestPutTyping_RoomNotFound(t *testing.T) {
	notFoundErr := status.Error(codes.NotFound, "room not found")
	mock := &mockTypingCoreClient{
		err: notFoundErr,
	}

	mux, _, makeToken := buildAuthedTypingHandler(t, mock)

	body := `{"typing": true, "timeout": 5000}`
	req := httptest.NewRequest(
		http.MethodPut,
		"/rooms/!nonexistent:test.local/typing/@test-sub-123:test.local",
		strings.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixError
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.ErrCode != "M_NOT_FOUND" {
		t.Errorf("expected errcode M_NOT_FOUND, got %s", errResp.ErrCode)
	}
}

// ─── Test 5: Not a room member → 403 ─────────────────────────────────────────
//
// AC #1 — when gRPC Core returns codes.PermissionDenied (user is not a member),
// the handler must return 403 M_FORBIDDEN.
//
// This covers BOTH the Core-enforced membership check (Core returns PermissionDenied)
// AND (implicitly) the path-userId != authenticated-userId check, which is tested
// separately via userId-mismatch below.

func TestPutTyping_NotMember(t *testing.T) {
	permErr := status.Error(codes.PermissionDenied, "not a room member")
	mock := &mockTypingCoreClient{
		err: permErr,
	}

	mux, _, makeToken := buildAuthedTypingHandler(t, mock)

	body := `{"typing": true, "timeout": 5000}`
	req := httptest.NewRequest(
		http.MethodPut,
		"/rooms/!room1:test.local/typing/@test-sub-123:test.local",
		strings.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixError
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.ErrCode != "M_FORBIDDEN" {
		t.Errorf("expected errcode M_FORBIDDEN, got %s", errResp.ErrCode)
	}
}

// ─── Test 6: userId path mismatch → 403 (Core NOT called) ────────────────────
//
// AC #1 — if the userId path parameter does not match the authenticated user_id
// (derived from JWT sub + serverName), the handler must return 403 M_FORBIDDEN
// BEFORE making any gRPC call to Core.

func TestPutTyping_UserIdMismatch(t *testing.T) {
	mock := &mockTypingCoreClient{
		resp: &pb.SetTypingResponse{},
	}

	mux, _, makeToken := buildAuthedTypingHandler(t, mock)

	body := `{"typing": true, "timeout": 5000}`
	// JWT sub is "test-sub-123" → "@test-sub-123:test.local", but path has @bob:test.local.
	req := httptest.NewRequest(
		http.MethodPut,
		"/rooms/!room1:test.local/typing/@bob:test.local",
		strings.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixError
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.ErrCode != "M_FORBIDDEN" {
		t.Errorf("expected errcode M_FORBIDDEN, got %s", errResp.ErrCode)
	}

	// Core must NOT have been called (mismatch check happens before gRPC call).
	if mock.capturedReq != nil {
		t.Error("expected SetTyping NOT to be called on userId mismatch, but capturedReq is set")
	}
}
