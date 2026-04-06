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
	authed := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil)(
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
