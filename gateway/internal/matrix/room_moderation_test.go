package matrix

// ─── Story 7-22: POST /kick, /ban, /unban, /forget ────────────────────────────
//
// These tests are written FIRST (ATDD gate), before implementation exists.
// ALL tests in this file are expected to FAIL until ModerationHandler is added
// to gateway/internal/matrix/rooms.go (or a dedicated moderation.go).
//
// Failing reasons:
//   - ModerationCoreClient interface does not exist → compile error
//   - ModerationHandler, ModerationConfig, NewModerationHandler do not exist → compile error
//   - PostKickUser, PostBanUser, PostUnbanUser, PostForgetRoom do not exist → compile error
//
// Acceptance Criteria covered:
//   AC1 — POST /kick: 200 {} on success; 403 M_FORBIDDEN on insufficient power level
//   AC2 — POST /ban:  200 {} on success; 403 M_FORBIDDEN on insufficient power level
//   AC3 — POST /unban: 200 {} on success; 403 M_FORBIDDEN on insufficient power level
//   AC4 — POST /forget: 200 {} on success; 403 M_FORBIDDEN if still joined
//   AC5 — All four endpoints return 400 M_BAD_JSON when user_id is missing (kick/ban/unban)
//   AC6 — All four endpoints return 404 M_NOT_FOUND when room does not exist
//   AC7 — 403 M_FORBIDDEN when requesting user is not a room member
//
// Test strategy:
//   - mockModerationCoreClient implements ModerationCoreClient
//     (consumer-defined interface, Go convention) — will be declared in rooms.go.
//   - buildAuthedModerationHandler wires JWTMiddleware → ModerationHandler
//     via a net/http/httptest mux so PathValue resolves correctly.
//   - gRPC error stubs use google.golang.org/grpc/codes to trigger mapped responses.
//   - Test 7 (Kick_BadJSON_MissingUserId) verifies validation at handler level —
//     gRPC mock is never called.

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

// mockModerationCoreClient implements ModerationCoreClient.
// capturedKickReq, capturedBanReq, capturedUnbanReq, capturedForgetReq record
// the last gRPC request forwarded so tests can assert request shape.

type mockModerationCoreClient struct {
	kickResp        *pb.KickUserResponse
	kickErr         error
	capturedKickReq *pb.KickUserRequest

	banResp        *pb.BanUserResponse
	banErr         error
	capturedBanReq *pb.BanUserRequest

	unbanResp        *pb.UnbanUserResponse
	unbanErr         error
	capturedUnbanReq *pb.UnbanUserRequest

	forgetResp        *pb.ForgetRoomResponse
	forgetErr         error
	capturedForgetReq *pb.ForgetRoomRequest
}

func (m *mockModerationCoreClient) KickUser(_ context.Context, req *pb.KickUserRequest) (*pb.KickUserResponse, error) {
	m.capturedKickReq = req
	return m.kickResp, m.kickErr
}

func (m *mockModerationCoreClient) BanUser(_ context.Context, req *pb.BanUserRequest) (*pb.BanUserResponse, error) {
	m.capturedBanReq = req
	return m.banResp, m.banErr
}

func (m *mockModerationCoreClient) UnbanUser(_ context.Context, req *pb.UnbanUserRequest) (*pb.UnbanUserResponse, error) {
	m.capturedUnbanReq = req
	return m.unbanResp, m.unbanErr
}

func (m *mockModerationCoreClient) ForgetRoom(_ context.Context, req *pb.ForgetRoomRequest) (*pb.ForgetRoomResponse, error) {
	m.capturedForgetReq = req
	return m.forgetResp, m.forgetErr
}

// ─── Helper ──────────────────────────────────────────────────────────────────

// buildAuthedModerationMux wires JWTMiddleware → ModerationHandler and registers
// all four moderation routes on a mux. Returns the mux and a token factory.
//
// JWT sub is always "test-sub-123", authenticated user_id = "@test-sub-123:test.local".
func buildAuthedModerationMux(t *testing.T, mock *mockModerationCoreClient) (http.Handler, func() string) {
	t.Helper()

	oidcSrv, privateKey := setupOIDCServer(t)
	t.Cleanup(oidcSrv.Close)

	provider := auth.NewProvider(context.Background(), oidcSrv.URL)
	jwtMW := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, nil, "test.local")

	h := NewModerationHandler(ModerationConfig{
		CoreClient: mock,
		ServerName: "test.local",
	})

	mux := http.NewServeMux()
	mux.Handle("POST /_matrix/client/v3/rooms/{roomId}/kick", jwtMW(http.HandlerFunc(h.PostKickUser)))
	mux.Handle("POST /_matrix/client/v3/rooms/{roomId}/ban", jwtMW(http.HandlerFunc(h.PostBanUser)))
	mux.Handle("POST /_matrix/client/v3/rooms/{roomId}/unban", jwtMW(http.HandlerFunc(h.PostUnbanUser)))
	mux.Handle("POST /_matrix/client/v3/rooms/{roomId}/forget", jwtMW(http.HandlerFunc(h.PostForgetRoom)))

	makeToken := func() string {
		return signJWT(t, oidcSrv.URL, privateKey, time.Now().Add(time.Hour), nil)
	}

	return mux, makeToken
}

// ─── Test 1: Kick_Success — caller with sufficient power kicks a member ───────
//
// AC1 — POST /kick with valid JWT and user_id → Core returns success → 200 {}

func TestPostKickUser_Success(t *testing.T) {
	mock := &mockModerationCoreClient{
		kickResp: &pb.KickUserResponse{},
	}
	mux, makeToken := buildAuthedModerationMux(t, mock)

	body := `{"user_id":"@alex:test.local","reason":"spamming"}`
	req := httptest.NewRequest(http.MethodPost,
		"/_matrix/client/v3/rooms/!testroom:test.local/kick",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// Response must be empty JSON object.
	trimmed := strings.TrimSpace(w.Body.String())
	if trimmed != "{}" {
		t.Errorf("expected body {}, got %q", trimmed)
	}

	// Assert that gRPC request was built with correct fields.
	if mock.capturedKickReq == nil {
		t.Fatal("expected gRPC KickUser to be called, but capturedKickReq is nil")
	}
	if mock.capturedKickReq.RoomId != "!testroom:test.local" {
		t.Errorf("expected RoomId %q, got %q", "!testroom:test.local", mock.capturedKickReq.RoomId)
	}
	if mock.capturedKickReq.TargetId != "@alex:test.local" {
		t.Errorf("expected TargetId @alex:test.local, got %q", mock.capturedKickReq.TargetId)
	}
	if mock.capturedKickReq.CallerId != "@test-sub-123:test.local" {
		t.Errorf("expected CallerId @test-sub-123:test.local, got %q", mock.capturedKickReq.CallerId)
	}
	if mock.capturedKickReq.Reason != "spamming" {
		t.Errorf("expected Reason %q, got %q", "spamming", mock.capturedKickReq.Reason)
	}
}

// ─── Test 2: Kick_Forbidden — caller has insufficient power level ─────────────
//
// AC1 — Core returns PermissionDenied → 403 M_FORBIDDEN

func TestPostKickUser_InsufficientPowerLevel(t *testing.T) {
	mock := &mockModerationCoreClient{
		kickErr: status.Error(codes.PermissionDenied, "insufficient power level"),
	}
	mux, makeToken := buildAuthedModerationMux(t, mock)

	body := `{"user_id":"@other:test.local"}`
	req := httptest.NewRequest(http.MethodPost,
		"/_matrix/client/v3/rooms/!testroom:test.local/kick",
		strings.NewReader(body))
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

// ─── Test 3: Kick_BadJSON_MissingUserId — user_id absent from body ────────────
//
// AC5 — missing user_id in kick/ban/unban → 400 M_BAD_JSON before any gRPC call

func TestPostKickUser_MissingUserID(t *testing.T) {
	mock := &mockModerationCoreClient{
		kickResp: &pb.KickUserResponse{},
	}
	mux, makeToken := buildAuthedModerationMux(t, mock)

	// Body exists but user_id is absent.
	body := `{"reason":"some reason"}`
	req := httptest.NewRequest(http.MethodPost,
		"/_matrix/client/v3/rooms/!testroom:test.local/kick",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", w.Code, w.Body.String())
	}
	var errResp matrixError
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.ErrCode != "M_BAD_JSON" {
		t.Errorf("expected errcode M_BAD_JSON, got %s", errResp.ErrCode)
	}
	// gRPC must NOT have been called.
	if mock.capturedKickReq != nil {
		t.Error("gRPC KickUser must not be called when user_id is missing")
	}
}

// ─── Test 4: Kick_NotFound — room does not exist ──────────────────────────────
//
// AC6 — Core returns NotFound → 404 M_NOT_FOUND

func TestPostKickUser_RoomNotFound(t *testing.T) {
	mock := &mockModerationCoreClient{
		kickErr: status.Error(codes.NotFound, "room not found"),
	}
	mux, makeToken := buildAuthedModerationMux(t, mock)

	body := `{"user_id":"@alex:test.local"}`
	req := httptest.NewRequest(http.MethodPost,
		"/_matrix/client/v3/rooms/!doesnotexist:test.local/kick",
		strings.NewReader(body))
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

// ─── Test 5: Kick_Unauthenticated — missing JWT → 401 ────────────────────────
//
// AC7 — jwtMiddleware must reject requests with no Authorization header.

func TestPostKickUser_Unauthenticated(t *testing.T) {
	mock := &mockModerationCoreClient{
		kickResp: &pb.KickUserResponse{},
	}
	mux, _ := buildAuthedModerationMux(t, mock)

	body := `{"user_id":"@alex:test.local"}`
	req := httptest.NewRequest(http.MethodPost,
		"/_matrix/client/v3/rooms/!testroom:test.local/kick",
		strings.NewReader(body))
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

// ─── Test 6: Ban_Success — moderator bans a member ───────────────────────────
//
// AC2 — POST /ban with valid JWT and user_id → Core returns success → 200 {}

func TestPostBanUser_Success(t *testing.T) {
	mock := &mockModerationCoreClient{
		banResp: &pb.BanUserResponse{},
	}
	mux, makeToken := buildAuthedModerationMux(t, mock)

	body := `{"user_id":"@alex:test.local","reason":"violating rules"}`
	req := httptest.NewRequest(http.MethodPost,
		"/_matrix/client/v3/rooms/!testroom:test.local/ban",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	trimmed := strings.TrimSpace(w.Body.String())
	if trimmed != "{}" {
		t.Errorf("expected body {}, got %q", trimmed)
	}
	if mock.capturedBanReq == nil {
		t.Fatal("expected gRPC BanUser to be called, but capturedBanReq is nil")
	}
	if mock.capturedBanReq.TargetId != "@alex:test.local" {
		t.Errorf("expected TargetId @alex:test.local, got %q", mock.capturedBanReq.TargetId)
	}
}

// ─── Test 7: Ban_Forbidden — Core returns PermissionDenied ───────────────────
//
// AC2 — gRPC PermissionDenied → 403 M_FORBIDDEN

func TestPostBanUser_Forbidden(t *testing.T) {
	mock := &mockModerationCoreClient{
		banErr: status.Error(codes.PermissionDenied, "insufficient power level"),
	}
	mux, makeToken := buildAuthedModerationMux(t, mock)

	body := `{"user_id":"@alex:test.local"}`
	req := httptest.NewRequest(http.MethodPost,
		"/_matrix/client/v3/rooms/!testroom:test.local/ban",
		strings.NewReader(body))
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

// ─── Test 8: Ban_MissingUserID — missing user_id in ban body ──────────────────
//
// AC5 — ban without user_id → 400 M_BAD_JSON

func TestPostBanUser_MissingUserID(t *testing.T) {
	mock := &mockModerationCoreClient{
		banResp: &pb.BanUserResponse{},
	}
	mux, makeToken := buildAuthedModerationMux(t, mock)

	body := `{}`
	req := httptest.NewRequest(http.MethodPost,
		"/_matrix/client/v3/rooms/!testroom:test.local/ban",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", w.Code, w.Body.String())
	}
	var errResp matrixError
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.ErrCode != "M_BAD_JSON" {
		t.Errorf("expected errcode M_BAD_JSON, got %s", errResp.ErrCode)
	}
	if mock.capturedBanReq != nil {
		t.Error("gRPC BanUser must not be called when user_id is missing")
	}
}

// ─── Test 9: Unban_Success — moderator unbans a banned member ────────────────
//
// AC3 — POST /unban with valid JWT and user_id → Core returns success → 200 {}

func TestPostUnbanUser_Success(t *testing.T) {
	mock := &mockModerationCoreClient{
		unbanResp: &pb.UnbanUserResponse{},
	}
	mux, makeToken := buildAuthedModerationMux(t, mock)

	body := `{"user_id":"@alex:test.local"}`
	req := httptest.NewRequest(http.MethodPost,
		"/_matrix/client/v3/rooms/!testroom:test.local/unban",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	trimmed := strings.TrimSpace(w.Body.String())
	if trimmed != "{}" {
		t.Errorf("expected body {}, got %q", trimmed)
	}
	if mock.capturedUnbanReq == nil {
		t.Fatal("expected gRPC UnbanUser to be called, but capturedUnbanReq is nil")
	}
	if mock.capturedUnbanReq.TargetId != "@alex:test.local" {
		t.Errorf("expected TargetId @alex:test.local, got %q", mock.capturedUnbanReq.TargetId)
	}
}

// ─── Test 10: Unban_Forbidden — Core returns PermissionDenied ────────────────
//
// AC3 — gRPC PermissionDenied → 403 M_FORBIDDEN

func TestPostUnbanUser_Forbidden(t *testing.T) {
	mock := &mockModerationCoreClient{
		unbanErr: status.Error(codes.PermissionDenied, "insufficient power level"),
	}
	mux, makeToken := buildAuthedModerationMux(t, mock)

	body := `{"user_id":"@alex:test.local"}`
	req := httptest.NewRequest(http.MethodPost,
		"/_matrix/client/v3/rooms/!testroom:test.local/unban",
		strings.NewReader(body))
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

// ─── Test 11: Unban_MissingUserID — missing user_id in unban body ─────────────
//
// AC5 — unban without user_id → 400 M_BAD_JSON

func TestPostUnbanUser_MissingUserID(t *testing.T) {
	mock := &mockModerationCoreClient{
		unbanResp: &pb.UnbanUserResponse{},
	}
	mux, makeToken := buildAuthedModerationMux(t, mock)

	body := `{}`
	req := httptest.NewRequest(http.MethodPost,
		"/_matrix/client/v3/rooms/!testroom:test.local/unban",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", w.Code, w.Body.String())
	}
	var errResp matrixError
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.ErrCode != "M_BAD_JSON" {
		t.Errorf("expected errcode M_BAD_JSON, got %s", errResp.ErrCode)
	}
}

// ─── Test 12: ForgetRoom_Success — user forgets a room after leaving ──────────
//
// AC4 — POST /forget with valid JWT → Core returns success → 200 {}

func TestPostForgetRoom_Success(t *testing.T) {
	mock := &mockModerationCoreClient{
		forgetResp: &pb.ForgetRoomResponse{},
	}
	mux, makeToken := buildAuthedModerationMux(t, mock)

	// forget body is {}
	req := httptest.NewRequest(http.MethodPost,
		"/_matrix/client/v3/rooms/!testroom:test.local/forget",
		strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	trimmed := strings.TrimSpace(w.Body.String())
	if trimmed != "{}" {
		t.Errorf("expected body {}, got %q", trimmed)
	}
	if mock.capturedForgetReq == nil {
		t.Fatal("expected gRPC ForgetRoom to be called, but capturedForgetReq is nil")
	}
	if mock.capturedForgetReq.RoomId != "!testroom:test.local" {
		t.Errorf("expected RoomId !testroom:test.local, got %q", mock.capturedForgetReq.RoomId)
	}
	if mock.capturedForgetReq.UserId != "@test-sub-123:test.local" {
		t.Errorf("expected UserId @test-sub-123:test.local, got %q", mock.capturedForgetReq.UserId)
	}
}

// ─── Test 13: ForgetRoom_Forbidden_StillJoined — cannot forget while joined ───
//
// AC4 — Core returns FailedPrecondition → 403 M_FORBIDDEN

func TestPostForgetRoom_ForbiddenStillJoined(t *testing.T) {
	mock := &mockModerationCoreClient{
		forgetErr: status.Error(codes.FailedPrecondition, "user must leave the room before forgetting"),
	}
	mux, makeToken := buildAuthedModerationMux(t, mock)

	req := httptest.NewRequest(http.MethodPost,
		"/_matrix/client/v3/rooms/!testroom:test.local/forget",
		strings.NewReader("{}"))
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

// ─── Test 14: ForgetRoom_NotFound — room does not exist ──────────────────────
//
// AC6 — Core returns NotFound → 404 M_NOT_FOUND

func TestPostForgetRoom_RoomNotFound(t *testing.T) {
	mock := &mockModerationCoreClient{
		forgetErr: status.Error(codes.NotFound, "room not found"),
	}
	mux, makeToken := buildAuthedModerationMux(t, mock)

	req := httptest.NewRequest(http.MethodPost,
		"/_matrix/client/v3/rooms/!doesnotexist:test.local/forget",
		strings.NewReader("{}"))
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

// ─── Test 15: ForgetRoom_Unavailable — Core returns Unavailable ─────────────
//
// AC4 (error mapping) — codes.Unavailable → 503 M_UNAVAILABLE

func TestPostForgetRoom_CoreUnavailable(t *testing.T) {
	mock := &mockModerationCoreClient{
		forgetErr: status.Error(codes.Unavailable, "core down"),
	}
	mux, makeToken := buildAuthedModerationMux(t, mock)

	req := httptest.NewRequest(http.MethodPost,
		"/_matrix/client/v3/rooms/!testroom:test.local/forget",
		strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
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

// ─── Test 16: Kick_InvalidArgument — Core returns InvalidArgument → 400 ──────
//
// AC5 (error mapping) — codes.InvalidArgument → 400 M_BAD_JSON

func TestPostKickUser_InvalidArgument(t *testing.T) {
	mock := &mockModerationCoreClient{
		kickErr: status.Error(codes.InvalidArgument, "invalid user ID"),
	}
	mux, makeToken := buildAuthedModerationMux(t, mock)

	body := `{"user_id":"invalid-user-id"}`
	req := httptest.NewRequest(http.MethodPost,
		"/_matrix/client/v3/rooms/!testroom:test.local/kick",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", w.Code, w.Body.String())
	}
	var errResp matrixError
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.ErrCode != "M_BAD_JSON" {
		t.Errorf("expected errcode M_BAD_JSON, got %s", errResp.ErrCode)
	}
}

// ─── Test 17: Ban_NotFound — room does not exist ──────────────────────────────
//
// AC6 — Core returns NotFound → 404 M_NOT_FOUND

func TestPostBanUser_RoomNotFound(t *testing.T) {
	mock := &mockModerationCoreClient{
		banErr: status.Error(codes.NotFound, "room not found"),
	}
	mux, makeToken := buildAuthedModerationMux(t, mock)

	body := `{"user_id":"@alex:test.local"}`
	req := httptest.NewRequest(http.MethodPost,
		"/_matrix/client/v3/rooms/!doesnotexist:test.local/ban",
		strings.NewReader(body))
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
