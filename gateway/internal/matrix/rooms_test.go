package matrix

// ─── Story 4-9: POST /_matrix/client/v3/createRoom ────────────────────────────
//
// These tests are written FIRST (ATDD gate), before implementation exists.
// ALL tests in this file are expected to FAIL until Story 4-9 is implemented.
//
// Test strategy:
//   - mockCreateRoomCoreClient implements CreateRoomCoreClient (consumer-defined
//     interface, Go convention) — defined here alongside the tests.
//   - Happy path wires JWTMiddleware around CreateRoomHandler so the full
//     auth → handler pipeline is exercised at httptest level.
//   - gRPC error cases use status.Error(codes.AlreadyExists, …) / a plain
//     error to trigger the M_ROOM_IN_USE / M_UNKNOWN code paths.
//   - Unauthenticated test sends a request with no Authorization header; the
//     JWTMiddleware must return 401 before the handler is reached.
//   - Room ID format assertion: must match ^![a-zA-Z0-9]+:[a-zA-Z0-9.-]+$
//     (starts with !, non-empty opaque part, colon separator, server-name part).

import (
	"context"
	"encoding/json"
	"fmt"
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

type mockCreateRoomCoreClient struct {
	resp        *pb.CreateRoomResponse
	err         error
	capturedReq *pb.CreateRoomRequest
}

func (m *mockCreateRoomCoreClient) CreateRoom(_ context.Context, req *pb.CreateRoomRequest) (*pb.CreateRoomResponse, error) {
	m.capturedReq = req
	return m.resp, m.err
}

func (m *mockCreateRoomCoreClient) InviteUser(_ context.Context, _ *pb.InviteUserRequest) (*pb.InviteUserResponse, error) {
	return &pb.InviteUserResponse{}, nil
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// buildCreateRoomHandler returns a CreateRoomHandler wired with the given mock.
// The handler is NOT wrapped in JWTMiddleware — use buildAuthedHandler for auth tests.
func buildCreateRoomHandler(mock *mockCreateRoomCoreClient) *CreateRoomHandler {
	return NewCreateRoomHandler(CreateRoomConfig{
		CoreClient: mock,
		ServerName: "test.local",
	})
}

// buildAuthedHandler wraps the CreateRoomHandler in a JWTMiddleware backed by
// the provided OIDC test server. Returns the http.Handler chain ready for use
// with httptest.NewRecorder.
func buildAuthedHandler(t *testing.T, mock *mockCreateRoomCoreClient) (http.Handler, *httptest.Server, func() string) {
	t.Helper()

	oidcSrv, privateKey := setupOIDCServer(t)
	t.Cleanup(oidcSrv.Close)

	provider := auth.NewProvider(context.Background(), oidcSrv.URL)

	handler := buildCreateRoomHandler(mock)
	authed := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, "test.local")(
		http.HandlerFunc(handler.PostCreateRoom),
	)

	makeToken := func() string {
		return signJWT(t, oidcSrv.URL, privateKey, time.Now().Add(time.Hour), nil)
	}

	return authed, oidcSrv, makeToken
}

// ─── Test 1: Happy path — authenticated user creates a room ──────────────────
//
// AC #1, #4 — POST with valid JWT + mock returns room_id → 200 with room_id JSON.
// Also validates room_id format: starts with !, contains colon, non-empty parts.

func TestPostCreateRoom_HappyPath(t *testing.T) {
	mock := &mockCreateRoomCoreClient{
		resp: &pb.CreateRoomResponse{RoomId: "!abc123:test.local"},
	}

	authed, _, makeToken := buildAuthedHandler(t, mock)

	body := `{"name": "Test Room"}`
	req := httptest.NewRequest(http.MethodPost, "/_matrix/client/v3/createRoom", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	authed.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}

	var resp struct {
		RoomID string `json:"room_id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}

	if resp.RoomID == "" {
		t.Fatal("expected room_id in response body, got empty string")
	}

	// Room ID format: must start with !, contain a colon, and have non-empty parts on both sides.
	if !strings.HasPrefix(resp.RoomID, "!") {
		t.Errorf("room_id must start with '!', got %s", resp.RoomID)
	}
	colonIdx := strings.Index(resp.RoomID, ":")
	if colonIdx < 0 {
		t.Errorf("room_id must contain ':', got %s", resp.RoomID)
	} else {
		opaque := resp.RoomID[1:colonIdx]
		serverPart := resp.RoomID[colonIdx+1:]
		if len(opaque) == 0 {
			t.Errorf("room_id opaque part must not be empty, got %s", resp.RoomID)
		}
		if len(serverPart) == 0 {
			t.Errorf("room_id server-name part must not be empty, got %s", resp.RoomID)
		}
	}

	// Assert that the gRPC request carried the correct creator_id derived from
	// the JWT sub claim ("test-sub-123") and the handler's server name ("test.local").
	expectedCreatorID := "@test-sub-123:test.local"
	if mock.capturedReq == nil {
		t.Fatal("expected gRPC CreateRoom to be called, but capturedReq is nil")
	}
	if mock.capturedReq.CreatorId != expectedCreatorID {
		t.Errorf("expected CreatorId %q, got %q", expectedCreatorID, mock.capturedReq.CreatorId)
	}
}

// ─── Test 2: Unauthenticated request → 401 M_MISSING_TOKEN ───────────────────
//
// AC #1 — JWTMiddleware must reject requests missing Authorization header.

func TestPostCreateRoom_Unauthenticated(t *testing.T) {
	mock := &mockCreateRoomCoreClient{
		resp: &pb.CreateRoomResponse{RoomId: "!abc123:test.local"},
	}

	authed, _, _ := buildAuthedHandler(t, mock)

	req := httptest.NewRequest(http.MethodPost, "/_matrix/client/v3/createRoom",
		strings.NewReader(`{"name": "Should Not Reach Handler"}`))
	req.Header.Set("Content-Type", "application/json")
	// Deliberately omit Authorization header

	w := httptest.NewRecorder()
	authed.ServeHTTP(w, req)

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

// ─── Test 3: Invalid JSON body → 400 M_BAD_JSON ──────────────────────────────
//
// AC #5 — handler must return 400 M_BAD_JSON when request body is malformed JSON.

func TestPostCreateRoom_BadJSON(t *testing.T) {
	mock := &mockCreateRoomCoreClient{
		resp: &pb.CreateRoomResponse{RoomId: "!abc123:test.local"},
	}

	authed, _, makeToken := buildAuthedHandler(t, mock)

	req := httptest.NewRequest(http.MethodPost, "/_matrix/client/v3/createRoom",
		strings.NewReader(`{not valid json`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	authed.ServeHTTP(w, req)

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

// ─── Test 4: Core returns ALREADY_EXISTS → 400 M_ROOM_IN_USE ─────────────────
//
// AC #6 — gRPC status AlreadyExists must map to 400 M_ROOM_IN_USE.

func TestPostCreateRoom_DuplicateAlias(t *testing.T) {
	alreadyExistsErr := status.Error(codes.AlreadyExists, "alias taken")
	mock := &mockCreateRoomCoreClient{
		err: alreadyExistsErr,
	}

	authed, _, makeToken := buildAuthedHandler(t, mock)

	body := `{"room_alias_name": "existing-room", "name": "My Room"}`
	req := httptest.NewRequest(http.MethodPost, "/_matrix/client/v3/createRoom", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	authed.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixError
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.ErrCode != "M_ROOM_IN_USE" {
		t.Errorf("expected errcode M_ROOM_IN_USE, got %s", errResp.ErrCode)
	}
}

// ─── Test 5: Core returns generic error → 500 M_UNKNOWN ──────────────────────
//
// AC #7 (partial) — any non-classified gRPC error maps to 500 M_UNKNOWN.

func TestPostCreateRoom_CoreError(t *testing.T) {
	mock := &mockCreateRoomCoreClient{
		err: fmt.Errorf("core unavailable"),
	}

	authed, _, makeToken := buildAuthedHandler(t, mock)

	req := httptest.NewRequest(http.MethodPost, "/_matrix/client/v3/createRoom",
		strings.NewReader(`{"name": "My Room"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	authed.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixError
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.ErrCode != "M_UNKNOWN" {
		t.Errorf("expected errcode M_UNKNOWN, got %s", errResp.ErrCode)
	}
}

// ─── Test 6: Core returns PERMISSION_DENIED → 403 M_FORBIDDEN ────────────────
//
// AC #7 — gRPC PermissionDenied maps to 403 M_FORBIDDEN.

func TestPostCreateRoom_Forbidden(t *testing.T) {
	permDeniedErr := status.Error(codes.PermissionDenied, "not allowed")
	mock := &mockCreateRoomCoreClient{
		err: permDeniedErr,
	}

	authed, _, makeToken := buildAuthedHandler(t, mock)

	req := httptest.NewRequest(http.MethodPost, "/_matrix/client/v3/createRoom",
		strings.NewReader(`{"name": "Restricted Room"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	authed.ServeHTTP(w, req)

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

// ═══════════════════════════════════════════════════════════════════════════════
// Story 4-10: POST /_matrix/client/v3/join/{roomIdOrAlias} + InviteUser
// ═══════════════════════════════════════════════════════════════════════════════
//
// These tests are written FIRST (ATDD gate), before implementation exists.
// ALL tests below this line are expected to FAIL until Story 4-10 is implemented.
//
// Test strategy:
//   - mockJoinRoomCoreClient implements JoinRoomCoreClient (consumer-defined
//     interface, Go convention) — defined here alongside the tests.
//   - mockInviteUserCoreClient implements InviteUserCoreClient.
//   - r.PathValue("roomIdOrAlias") requires the request to pass through a
//     *http.ServeMux. Tests use a local mux to exercise path extraction.
//   - JWTMiddleware wraps the handler chain for auth tests.
//   - gRPC error cases use status.Error(codes.NotFound, …) / codes.PermissionDenied
//     to trigger the M_NOT_FOUND / M_FORBIDDEN code paths.
//   - Already-member (idempotent join): Core returns codes.AlreadyExists, Go
//     returns 200 with room_id per Matrix spec requirement.

// ─── Mock gRPC core clients ───────────────────────────────────────────────────

type mockJoinRoomCoreClient struct {
	resp        *pb.JoinRoomResponse
	err         error
	capturedReq *pb.JoinRoomRequest
}

func (m *mockJoinRoomCoreClient) JoinRoom(_ context.Context, req *pb.JoinRoomRequest) (*pb.JoinRoomResponse, error) {
	m.capturedReq = req
	return m.resp, m.err
}

type mockInviteUserCoreClient struct {
	resp        *pb.InviteUserResponse
	err         error
	capturedReq *pb.InviteUserRequest
}

func (m *mockInviteUserCoreClient) InviteUser(_ context.Context, req *pb.InviteUserRequest) (*pb.InviteUserResponse, error) {
	m.capturedReq = req
	return m.resp, m.err
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// buildJoinRoomHandler returns a JoinRoomHandler wired with the given mock.
func buildJoinRoomHandler(mock *mockJoinRoomCoreClient) *JoinRoomHandler {
	return NewJoinRoomHandler(JoinRoomConfig{
		CoreClient: mock,
		ServerName: "test.local",
	})
}

// buildAuthedJoinRoomHandler wraps JoinRoomHandler.PostJoinRoom in JWTMiddleware
// and serves it through a ServeMux so r.PathValue("roomIdOrAlias") works.
func buildAuthedJoinRoomHandler(t *testing.T, mock *mockJoinRoomCoreClient) (http.Handler, func() string) {
	t.Helper()

	oidcSrv, privateKey := setupOIDCServer(t)
	t.Cleanup(oidcSrv.Close)

	provider := auth.NewProvider(context.Background(), oidcSrv.URL)

	handler := buildJoinRoomHandler(mock)
	authedHandler := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, "test.local")(
		http.HandlerFunc(handler.PostJoinRoom),
	)

	// Wrap in a ServeMux so r.PathValue("roomIdOrAlias") is populated.
	mux := http.NewServeMux()
	mux.Handle("POST /{roomIdOrAlias}", authedHandler)

	makeToken := func() string {
		return signJWT(t, oidcSrv.URL, privateKey, time.Now().Add(time.Hour), nil)
	}

	return mux, makeToken
}

// buildAuthedInviteUserHandler wraps InviteUserHandler.PostInviteUser in JWTMiddleware
// and serves it through a ServeMux so r.PathValue("roomId") works.
func buildAuthedInviteUserHandler(t *testing.T, mock *mockInviteUserCoreClient) (http.Handler, func() string) {
	t.Helper()

	oidcSrv, privateKey := setupOIDCServer(t)
	t.Cleanup(oidcSrv.Close)

	provider := auth.NewProvider(context.Background(), oidcSrv.URL)

	handler := NewInviteUserHandler(InviteUserConfig{
		CoreClient: mock,
		ServerName: "test.local",
	})
	authedHandler := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, "test.local")(
		http.HandlerFunc(handler.PostInviteUser),
	)

	mux := http.NewServeMux()
	mux.Handle("POST /{roomId}/invite", authedHandler)

	makeToken := func() string {
		return signJWT(t, oidcSrv.URL, privateKey, time.Now().Add(time.Hour), nil)
	}

	return mux, makeToken
}

// ─── Test 7: Happy path — authenticated user joins existing room → 200 ────────
//
// AC #2 — POST with valid JWT + mock returns JoinRoomResponse → 200 {"room_id": ...}.
// Also asserts that the gRPC request carries the correct user_id and room_id_or_alias.

func TestPostJoinRoom_HappyPath(t *testing.T) {
	mock := &mockJoinRoomCoreClient{
		resp: &pb.JoinRoomResponse{RoomId: "!abc:test.local"},
	}

	mux, makeToken := buildAuthedJoinRoomHandler(t, mock)

	req := httptest.NewRequest(http.MethodPost, "/!abc:test.local", http.NoBody)
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

	var resp struct {
		RoomID string `json:"room_id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if resp.RoomID != "!abc:test.local" {
		t.Errorf("expected room_id !abc:test.local, got %s", resp.RoomID)
	}

	// Assert gRPC request fields.
	if mock.capturedReq == nil {
		t.Fatal("expected gRPC JoinRoom to be called, but capturedReq is nil")
	}
	if mock.capturedReq.RoomIdOrAlias != "!abc:test.local" {
		t.Errorf("expected RoomIdOrAlias %q, got %q", "!abc:test.local", mock.capturedReq.RoomIdOrAlias)
	}
	expectedUserID := "@test-sub-123:test.local"
	if mock.capturedReq.UserId != expectedUserID {
		t.Errorf("expected UserId %q, got %q", expectedUserID, mock.capturedReq.UserId)
	}
}

// ─── Test 8: Unauthenticated request → 401 M_MISSING_TOKEN ───────────────────
//
// AC #1 — JWTMiddleware must reject requests missing Authorization header.

func TestPostJoinRoom_Unauthenticated(t *testing.T) {
	mock := &mockJoinRoomCoreClient{
		resp: &pb.JoinRoomResponse{RoomId: "!abc:test.local"},
	}

	mux, _ := buildAuthedJoinRoomHandler(t, mock)

	req := httptest.NewRequest(http.MethodPost, "/!abc:test.local", http.NoBody)
	// Deliberately omit Authorization header

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

// ─── Test 9: Room not found (gRPC NotFound) → 404 M_NOT_FOUND ────────────────
//
// AC #3 — gRPC status NotFound must map to 404 M_NOT_FOUND.

func TestPostJoinRoom_RoomNotFound(t *testing.T) {
	mock := &mockJoinRoomCoreClient{
		err: status.Error(codes.NotFound, "room does not exist"),
	}

	mux, makeToken := buildAuthedJoinRoomHandler(t, mock)

	req := httptest.NewRequest(http.MethodPost, "/!nonexistent:test.local", http.NoBody)
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

// ─── Test 10: Already a member (idempotent) → 200 with room_id ───────────────
//
// AC #7 — Matrix spec requires idempotent join. Core returning AlreadyExists
// must be treated as success and return 200 {"room_id": ...}.

func TestPostJoinRoom_AlreadyMember(t *testing.T) {
	mock := &mockJoinRoomCoreClient{
		resp: &pb.JoinRoomResponse{RoomId: "!abc:test.local"},
		err:  status.Error(codes.AlreadyExists, "already a member"),
	}

	mux, makeToken := buildAuthedJoinRoomHandler(t, mock)

	req := httptest.NewRequest(http.MethodPost, "/!abc:test.local", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (idempotent), got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		RoomID string `json:"room_id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if resp.RoomID != "!abc:test.local" {
		t.Errorf("expected room_id !abc:test.local, got %s", resp.RoomID)
	}
}

// ─── Test 11: Generic gRPC error → 500 M_UNKNOWN ─────────────────────────────
//
// AC #2 (error path) — any unclassified gRPC error must map to 500 M_UNKNOWN.

func TestPostJoinRoom_CoreError(t *testing.T) {
	mock := &mockJoinRoomCoreClient{
		err: fmt.Errorf("core unavailable"),
	}

	mux, makeToken := buildAuthedJoinRoomHandler(t, mock)

	req := httptest.NewRequest(http.MethodPost, "/!abc:test.local", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixError
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.ErrCode != "M_UNKNOWN" {
		t.Errorf("expected errcode M_UNKNOWN, got %s", errResp.ErrCode)
	}
}

// ─── Test 12: Invite user → 200 {} ───────────────────────────────────────────
//
// AC #6 — POST /rooms/{roomId}/invite with valid JWT + valid body → 200 {}.
// Asserts the gRPC request carries roomId, inviterId (caller), and inviteeId.

func TestPostInviteUser_HappyPath(t *testing.T) {
	mock := &mockInviteUserCoreClient{
		resp: &pb.InviteUserResponse{},
	}

	mux, makeToken := buildAuthedInviteUserHandler(t, mock)

	body := `{"user_id": "@bob:test.local"}`
	req := httptest.NewRequest(http.MethodPost, "/!abc:test.local/invite", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// Body must be an empty JSON object {}.
	bodyStr := strings.TrimSpace(w.Body.String())
	if bodyStr != "{}" {
		t.Errorf("expected body '{}', got %q", bodyStr)
	}

	// Assert gRPC request fields.
	if mock.capturedReq == nil {
		t.Fatal("expected gRPC InviteUser to be called, but capturedReq is nil")
	}
	if mock.capturedReq.InviteeId != "@bob:test.local" {
		t.Errorf("expected InviteeId @bob:test.local, got %s", mock.capturedReq.InviteeId)
	}
	expectedCaller := "@test-sub-123:test.local"
	if mock.capturedReq.InviterId != expectedCaller {
		t.Errorf("expected InviterId %q, got %q", expectedCaller, mock.capturedReq.InviterId)
	}
}

// ─── Test 13: Invite user — bad JSON body → 400 M_BAD_JSON ───────────────────
//
// AC #6 — malformed JSON body must return 400 M_BAD_JSON before reaching Core.

func TestPostInviteUser_BadJSON(t *testing.T) {
	mock := &mockInviteUserCoreClient{
		resp: &pb.InviteUserResponse{},
	}

	mux, makeToken := buildAuthedInviteUserHandler(t, mock)

	req := httptest.NewRequest(http.MethodPost, "/!abc:test.local/invite",
		strings.NewReader(`{not valid json`))
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

// ─── Test 14 (MAJOR-1): Join room — permission denied → 403 M_FORBIDDEN ──────
//
// AC #4 — gRPC PermissionDenied (private room, no invite) must map to 403 M_FORBIDDEN.

func TestPostJoinRoom_Forbidden(t *testing.T) {
	mock := &mockJoinRoomCoreClient{
		err: status.Error(codes.PermissionDenied, "private room, no invitation"),
	}

	mux, makeToken := buildAuthedJoinRoomHandler(t, mock)

	req := httptest.NewRequest(http.MethodPost, "/!private:test.local", http.NoBody)
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

// ─── MAJOR-2: POST /rooms/{roomId}/join — second join endpoint ────────────────
//
// buildAuthedJoinRoomByIdHandler wraps JoinRoomHandler.PostJoinRoomById in
// JWTMiddleware and serves it through a ServeMux so r.PathValue("roomId") works.

func buildAuthedJoinRoomByIdHandler(t *testing.T, mock *mockJoinRoomCoreClient) (http.Handler, func() string) {
	t.Helper()

	oidcSrv, privateKey := setupOIDCServer(t)
	t.Cleanup(oidcSrv.Close)

	provider := auth.NewProvider(context.Background(), oidcSrv.URL)

	handler := buildJoinRoomHandler(mock)
	authedHandler := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, "test.local")(
		http.HandlerFunc(handler.PostJoinRoomById),
	)

	mux := http.NewServeMux()
	mux.Handle("POST /rooms/{roomId}/join", authedHandler)

	makeToken := func() string {
		return signJWT(t, oidcSrv.URL, privateKey, time.Now().Add(time.Hour), nil)
	}

	return mux, makeToken
}

// ─── Test 15 (MAJOR-2a): POST /rooms/{roomId}/join — happy path → 200 ─────────
//
// AC #5 — POST with valid JWT + mock returns JoinRoomResponse → 200 {"room_id": ...}.

func TestPostJoinRoomById_HappyPath(t *testing.T) {
	mock := &mockJoinRoomCoreClient{
		resp: &pb.JoinRoomResponse{RoomId: "!abc:test.local"},
	}

	mux, makeToken := buildAuthedJoinRoomByIdHandler(t, mock)

	req := httptest.NewRequest(http.MethodPost, "/rooms/!abc:test.local/join", http.NoBody)
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

	var resp struct {
		RoomID string `json:"room_id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if resp.RoomID != "!abc:test.local" {
		t.Errorf("expected room_id !abc:test.local, got %s", resp.RoomID)
	}
}

// ─── Test 16 (MAJOR-2b): POST /rooms/{roomId}/join — forbidden → 403 ─────────
//
// AC #5 — gRPC PermissionDenied (no invitation) must map to 403 M_FORBIDDEN.

func TestPostJoinRoomById_Forbidden(t *testing.T) {
	mock := &mockJoinRoomCoreClient{
		err: status.Error(codes.PermissionDenied, "no invitation"),
	}

	mux, makeToken := buildAuthedJoinRoomByIdHandler(t, mock)

	req := httptest.NewRequest(http.MethodPost, "/rooms/!private:test.local/join", http.NoBody)
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

// ─── Test 17 (MAJOR-3a): Invite user — forbidden → 403 M_FORBIDDEN ───────────
//
// AC #8 — gRPC PermissionDenied must map to 403 M_FORBIDDEN.

func TestPostInviteUser_Forbidden(t *testing.T) {
	mock := &mockInviteUserCoreClient{
		err: status.Error(codes.PermissionDenied, "caller not a room member"),
	}

	mux, makeToken := buildAuthedInviteUserHandler(t, mock)

	body := `{"user_id": "@bob:test.local"}`
	req := httptest.NewRequest(http.MethodPost, "/!abc:test.local/invite", strings.NewReader(body))
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

// ─── Test 18 (MAJOR-3b): Invite user — room not found → 404 M_NOT_FOUND ──────
//
// AC #9 — gRPC NotFound must map to 404 M_NOT_FOUND.

func TestPostInviteUser_NotFound(t *testing.T) {
	mock := &mockInviteUserCoreClient{
		err: status.Error(codes.NotFound, "room does not exist"),
	}

	mux, makeToken := buildAuthedInviteUserHandler(t, mock)

	body := `{"user_id": "@bob:test.local"}`
	req := httptest.NewRequest(http.MethodPost, "/!nonexistent:test.local/invite", strings.NewReader(body))
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

// ─── Test 19 (MINOR-2): Invite user — unauthenticated → 401 M_MISSING_TOKEN ──
//
// AC #6 — JWTMiddleware must reject requests missing Authorization header.

func TestPostInviteUser_Unauthenticated(t *testing.T) {
	mock := &mockInviteUserCoreClient{
		resp: &pb.InviteUserResponse{},
	}

	mux, _ := buildAuthedInviteUserHandler(t, mock)

	body := `{"user_id": "@bob:test.local"}`
	req := httptest.NewRequest(http.MethodPost, "/!abc:test.local/invite", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// Deliberately omit Authorization header

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

// ═══════════════════════════════════════════════════════════════════════════════
// Story 4-11: PUT /_matrix/client/v3/rooms/{roomId}/send/{eventType}/{txnId}
// ═══════════════════════════════════════════════════════════════════════════════
//
// These tests are written FIRST (ATDD gate), before implementation exists.
// ALL tests below this line are expected to FAIL until Story 4-11 is implemented.
//
// Test strategy:
//   - mockSendEventCoreClient implements SendEventCoreClient (consumer-defined
//     interface, Go convention) — defined here alongside the tests.
//   - The handler requires path values {roomId}, {eventType}, {txnId}, so all
//     requests pass through a real http.ServeMux to populate r.PathValue(...).
//   - JWTMiddleware wraps the handler chain for auth tests.
//   - gRPC error cases use status.Error(codes.NotFound, …) / codes.PermissionDenied
//     to trigger the M_NOT_FOUND / M_FORBIDDEN code paths.
//   - Idempotency test: mock always returns the same event_id; calling twice
//     must yield 200 with the same event_id both times (no error on second call).

// ─── Mock gRPC core client ────────────────────────────────────────────────────

type mockSendEventCoreClient struct {
	resp        *pb.SendEventResponse
	err         error
	capturedReq *pb.SendEventRequest
}

func (m *mockSendEventCoreClient) SendEvent(_ context.Context, req *pb.SendEventRequest) (*pb.SendEventResponse, error) {
	m.capturedReq = req
	return m.resp, m.err
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// buildSendEventHandler returns a SendEventHandler wired with the given mock.
func buildSendEventHandler(mock *mockSendEventCoreClient) *SendEventHandler {
	return NewSendEventHandler(SendEventConfig{
		CoreClient: mock,
		ServerName: "test.local",
	})
}

// buildAuthedSendEventHandler wraps SendEventHandler.PutSendEvent in JWTMiddleware
// and serves it through a ServeMux so r.PathValue("roomId"), r.PathValue("eventType"),
// and r.PathValue("txnId") are all populated by the Go 1.22+ mux router.
func buildAuthedSendEventHandler(t *testing.T, mock *mockSendEventCoreClient) (http.Handler, func() string) {
	t.Helper()

	oidcSrv, privateKey := setupOIDCServer(t)
	t.Cleanup(oidcSrv.Close)

	provider := auth.NewProvider(context.Background(), oidcSrv.URL)

	handler := buildSendEventHandler(mock)
	authedHandler := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, "test.local")(
		http.HandlerFunc(handler.PutSendEvent),
	)

	// Wrap in a real ServeMux — required for r.PathValue to work with multi-segment params.
	mux := http.NewServeMux()
	mux.Handle("PUT /_matrix/client/v3/rooms/{roomId}/send/{eventType}/{txnId}", authedHandler)

	makeToken := func() string {
		return signJWT(t, oidcSrv.URL, privateKey, time.Now().Add(time.Hour), nil)
	}

	return mux, makeToken
}

// ─── Test 20: Happy path — authenticated user sends an event → 200 with event_id
//
// AC #4, #5 — PUT with valid JWT + mock returns SendEventResponse → 200 {"event_id": "$abc123"}.
// Also asserts gRPC request fields: room_id, event_type, txn_id, sender_id.

func TestPutSendEvent_HappyPath(t *testing.T) {
	mock := &mockSendEventCoreClient{
		resp: &pb.SendEventResponse{EventId: "$abc123"},
	}

	mux, makeToken := buildAuthedSendEventHandler(t, mock)

	body := `{"msgtype":"m.text","body":"hello"}`
	req := httptest.NewRequest(http.MethodPut,
		"/_matrix/client/v3/rooms/!room1:test.local/send/m.room.message/txn1",
		strings.NewReader(body))
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

	var resp struct {
		EventID string `json:"event_id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if resp.EventID != "$abc123" {
		t.Errorf("expected event_id $abc123, got %s", resp.EventID)
	}

	// Assert gRPC request fields are correctly populated from the URL path + JWT.
	if mock.capturedReq == nil {
		t.Fatal("expected gRPC SendEvent to be called, but capturedReq is nil")
	}
	if mock.capturedReq.RoomId != "!room1:test.local" {
		t.Errorf("expected RoomId !room1:test.local, got %s", mock.capturedReq.RoomId)
	}
	if mock.capturedReq.EventType != "m.room.message" {
		t.Errorf("expected EventType m.room.message, got %s", mock.capturedReq.EventType)
	}
	if mock.capturedReq.TxnId != "txn1" {
		t.Errorf("expected TxnId txn1, got %s", mock.capturedReq.TxnId)
	}
	expectedSenderID := "@test-sub-123:test.local"
	if mock.capturedReq.SenderId != expectedSenderID {
		t.Errorf("expected SenderId %q, got %q", expectedSenderID, mock.capturedReq.SenderId)
	}

	// MINOR-5: OriginTs must be a non-zero Unix millisecond timestamp.
	if mock.capturedReq.OriginTs <= 0 {
		t.Errorf("expected non-zero OriginTs, got %d", mock.capturedReq.OriginTs)
	}

	// MINOR-6: Content must be non-empty (handler must forward the JSON body bytes).
	if len(mock.capturedReq.Content) == 0 {
		t.Error("expected non-empty Content bytes in gRPC request, got empty")
	}
}

// ─── Test 21: Unauthenticated request → 401 M_MISSING_TOKEN ──────────────────
//
// AC #1 — JWTMiddleware must reject requests with no Authorization header.

func TestPutSendEvent_Unauthenticated(t *testing.T) {
	mock := &mockSendEventCoreClient{
		resp: &pb.SendEventResponse{EventId: "$abc123"},
	}

	mux, _ := buildAuthedSendEventHandler(t, mock)

	req := httptest.NewRequest(http.MethodPut,
		"/_matrix/client/v3/rooms/!room1:test.local/send/m.room.message/txn1",
		strings.NewReader(`{"msgtype":"m.text","body":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	// Deliberately omit Authorization header

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

// ─── Test 22: Malformed JSON body → 400 M_BAD_JSON ───────────────────────────
//
// AC #3 — handler must decode the JSON request body; malformed JSON → 400 M_BAD_JSON.

func TestPutSendEvent_BadJSON(t *testing.T) {
	mock := &mockSendEventCoreClient{
		resp: &pb.SendEventResponse{EventId: "$abc123"},
	}

	mux, makeToken := buildAuthedSendEventHandler(t, mock)

	req := httptest.NewRequest(http.MethodPut,
		"/_matrix/client/v3/rooms/!room1:test.local/send/m.room.message/txn1",
		strings.NewReader(`{not valid json`))
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

// ─── Test 23: Room not found (gRPC NOT_FOUND) → 404 M_NOT_FOUND ──────────────
//
// AC #7 — gRPC status NotFound must map to 404 M_NOT_FOUND.

func TestPutSendEvent_RoomNotFound(t *testing.T) {
	mock := &mockSendEventCoreClient{
		err: status.Error(codes.NotFound, "room does not exist"),
	}

	mux, makeToken := buildAuthedSendEventHandler(t, mock)

	req := httptest.NewRequest(http.MethodPut,
		"/_matrix/client/v3/rooms/!nonexistent:test.local/send/m.room.message/txn1",
		strings.NewReader(`{"msgtype":"m.text","body":"hello"}`))
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

// ─── Test 24: Not a room member (gRPC PERMISSION_DENIED) → 403 M_FORBIDDEN ───
//
// AC #6 — gRPC status PermissionDenied must map to 403 M_FORBIDDEN.

func TestPutSendEvent_NotMember(t *testing.T) {
	mock := &mockSendEventCoreClient{
		err: status.Error(codes.PermissionDenied, "user is not a member of this room"),
	}

	mux, makeToken := buildAuthedSendEventHandler(t, mock)

	req := httptest.NewRequest(http.MethodPut,
		"/_matrix/client/v3/rooms/!private:test.local/send/m.room.message/txn1",
		strings.NewReader(`{"msgtype":"m.text","body":"hello"}`))
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

// ─── Test 25 (MAJOR-1): Rate limited (gRPC RESOURCE_EXHAUSTED) → 429 M_LIMIT_EXCEEDED
//
// AC #8 — gRPC status ResourceExhausted must map to 429 M_LIMIT_EXCEEDED.
// Matrix Client-Server API spec §11.6: servers MUST return 429 when rate-limiting.

func TestPutSendEvent_RateLimited(t *testing.T) {
	mock := &mockSendEventCoreClient{
		err: status.Error(codes.ResourceExhausted, "rate limit exceeded"),
	}

	mux, makeToken := buildAuthedSendEventHandler(t, mock)

	req := httptest.NewRequest(http.MethodPut,
		"/_matrix/client/v3/rooms/!room1:test.local/send/m.room.message/txn1",
		strings.NewReader(`{"msgtype":"m.text","body":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixError
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.ErrCode != "M_LIMIT_EXCEEDED" {
		t.Errorf("expected errcode M_LIMIT_EXCEEDED, got %s", errResp.ErrCode)
	}
}

// ─── Test 26: Idempotent txn_id — same event_id returned on duplicate call ───
//
// AC #9 — duplicate txn_id (same user + room) returns 200 with the same event_id.
// The mock always returns the same SendEventResponse; both calls must succeed with
// identical event_id values and no error on the second call.

func TestPutSendEvent_Idempotency(t *testing.T) {
	mock := &mockSendEventCoreClient{
		resp: &pb.SendEventResponse{EventId: "$abc123"},
	}

	mux, makeToken := buildAuthedSendEventHandler(t, mock)

	makeReq := func() *http.Request {
		r := httptest.NewRequest(http.MethodPut,
			"/_matrix/client/v3/rooms/!room1:test.local/send/m.room.message/txn1",
			strings.NewReader(`{"msgtype":"m.text","body":"hello"}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+makeToken())
		return r
	}

	// First call.
	w1 := httptest.NewRecorder()
	mux.ServeHTTP(w1, makeReq())

	if w1.Code != http.StatusOK {
		t.Fatalf("first call: expected 200, got %d; body: %s", w1.Code, w1.Body.String())
	}
	var resp1 struct {
		EventID string `json:"event_id"`
	}
	if err := json.NewDecoder(w1.Body).Decode(&resp1); err != nil {
		t.Fatalf("first call: failed to decode response body: %v", err)
	}

	// Second call with the same txn_id.
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, makeReq())

	if w2.Code != http.StatusOK {
		t.Fatalf("second call: expected 200, got %d; body: %s", w2.Code, w2.Body.String())
	}
	var resp2 struct {
		EventID string `json:"event_id"`
	}
	if err := json.NewDecoder(w2.Body).Decode(&resp2); err != nil {
		t.Fatalf("second call: failed to decode response body: %v", err)
	}

	// Both calls must return the same event_id.
	if resp1.EventID != resp2.EventID {
		t.Errorf("idempotency violation: first call returned %q, second call returned %q",
			resp1.EventID, resp2.EventID)
	}
	if resp1.EventID != "$abc123" {
		t.Errorf("expected event_id $abc123, got %s", resp1.EventID)
	}

	// MAJOR-2: Assert that the txn_id was forwarded correctly in the gRPC request.
	if mock.capturedReq == nil {
		t.Fatal("expected gRPC SendEvent to be called, but capturedReq is nil")
	}
	if mock.capturedReq.TxnId != "txn1" {
		t.Errorf("expected TxnId %q forwarded to Core, got %q", "txn1", mock.capturedReq.TxnId)
	}
}

// ═══════════════════════════════════════════════════════════════════════════════
// Story 4-13: PUT /_matrix/client/v3/rooms/{roomId}/state/{eventType}/{stateKey}
// ═══════════════════════════════════════════════════════════════════════════════
//
// Tests for SetRoomStateHandler.PutSetRoomState.
// Test strategy:
//   - mockSetRoomStateCoreClient implements SetRoomStateCoreClient.
//   - Happy path: valid JWT + mock returns SetPowerLevelsResponse → 200 {"event_id": ""}.
//   - Forbidden: gRPC PermissionDenied → 403 M_FORBIDDEN.
//   - Unauthenticated: no Authorization header → 401 M_MISSING_TOKEN.
//   - Room not found: gRPC NotFound → 404 M_NOT_FOUND.

// ─── Mock gRPC core client ────────────────────────────────────────────────────

type mockSetRoomStateCoreClient struct {
	resp        *pb.SetPowerLevelsResponse
	err         error
	capturedReq *pb.SetPowerLevelsRequest
}

func (m *mockSetRoomStateCoreClient) SetPowerLevels(_ context.Context, req *pb.SetPowerLevelsRequest) (*pb.SetPowerLevelsResponse, error) {
	m.capturedReq = req
	return m.resp, m.err
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// buildAuthedSetRoomStateHandler wraps SetRoomStateHandler.PutSetRoomState in JWTMiddleware
// and serves it through a ServeMux so r.PathValue("roomId"), r.PathValue("eventType"),
// and r.PathValue("stateKey") are populated by the Go 1.22+ mux router.
func buildAuthedSetRoomStateHandler(t *testing.T, mock *mockSetRoomStateCoreClient) (http.Handler, func() string) {
	t.Helper()

	oidcSrv, privateKey := setupOIDCServer(t)
	t.Cleanup(oidcSrv.Close)

	provider := auth.NewProvider(context.Background(), oidcSrv.URL)

	handler := NewSetRoomStateHandler(SetRoomStateConfig{
		CoreClient: mock,
		ServerName: "test.local",
	})
	authedHandler := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, "test.local")(
		http.HandlerFunc(handler.PutSetRoomState),
	)

	mux := http.NewServeMux()
	// Register both: with stateKey (e.g. m.room.member/user_id) and without (e.g. m.room.power_levels).
	mux.Handle("PUT /_matrix/client/v3/rooms/{roomId}/state/{eventType}/{stateKey}", authedHandler)
	mux.Handle("PUT /_matrix/client/v3/rooms/{roomId}/state/{eventType}", authedHandler)

	makeToken := func() string {
		return signJWT(t, oidcSrv.URL, privateKey, time.Now().Add(time.Hour), nil)
	}

	return mux, makeToken
}

// ─── TestPutSetRoomState_HappyPath → 200 ────────────────────────────────────
//
// AT #9 — PUT with valid JWT + mock returns SetPowerLevelsResponse → 200 {"event_id": ""}.

func TestPutSetRoomState_HappyPath(t *testing.T) {
	mock := &mockSetRoomStateCoreClient{
		resp: &pb.SetPowerLevelsResponse{},
	}

	mux, makeToken := buildAuthedSetRoomStateHandler(t, mock)

	body := `{"ban":50,"invite":0,"kick":50,"events_default":0,"state_default":50,"users_default":0}`
	req := httptest.NewRequest(http.MethodPut,
		"/_matrix/client/v3/rooms/!room1:test.local/state/m.room.power_levels",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		EventID string `json:"event_id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}

	// State events return an empty event_id in MVP.
	if resp.EventID != "" {
		t.Errorf("expected empty event_id for state event, got %q", resp.EventID)
	}

	// Assert gRPC request carried the correct room_id.
	if mock.capturedReq == nil {
		t.Fatal("expected gRPC SetPowerLevels to be called, but capturedReq is nil")
	}
	if mock.capturedReq.RoomId != "!room1:test.local" {
		t.Errorf("expected RoomId !room1:test.local, got %s", mock.capturedReq.RoomId)
	}
}

// ─── TestPutSetRoomState_Forbidden → 403 ────────────────────────────────────
//
// AT #10 — gRPC PermissionDenied → 403 M_FORBIDDEN.

func TestPutSetRoomState_Forbidden(t *testing.T) {
	mock := &mockSetRoomStateCoreClient{
		err: status.Error(codes.PermissionDenied, "insufficient power level"),
	}

	mux, makeToken := buildAuthedSetRoomStateHandler(t, mock)

	body := `{"ban":50}`
	req := httptest.NewRequest(http.MethodPut,
		"/_matrix/client/v3/rooms/!room1:test.local/state/m.room.power_levels",
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

// ─── TestPutSetRoomState_Unauthenticated → 401 ───────────────────────────────
//
// AT #11 — no Authorization header → 401 M_MISSING_TOKEN.

func TestPutSetRoomState_Unauthenticated(t *testing.T) {
	mock := &mockSetRoomStateCoreClient{
		resp: &pb.SetPowerLevelsResponse{},
	}

	mux, _ := buildAuthedSetRoomStateHandler(t, mock)

	body := `{"ban":50}`
	req := httptest.NewRequest(http.MethodPut,
		"/_matrix/client/v3/rooms/!room1:test.local/state/m.room.power_levels",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// Deliberately omit Authorization header

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

// ─── TestPutSetRoomState_RoomNotFound → 404 ──────────────────────────────────
//
// gRPC NotFound → 404 M_NOT_FOUND.

func TestPutSetRoomState_RoomNotFound(t *testing.T) {
	mock := &mockSetRoomStateCoreClient{
		err: status.Error(codes.NotFound, "room not found"),
	}

	mux, makeToken := buildAuthedSetRoomStateHandler(t, mock)

	body := `{"ban":50}`
	req := httptest.NewRequest(http.MethodPut,
		"/_matrix/client/v3/rooms/!nonexistent:test.local/state/m.room.power_levels",
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
