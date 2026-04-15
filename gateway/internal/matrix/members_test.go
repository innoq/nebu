package matrix

// ─── Story 5-2: GET /_matrix/client/v3/rooms/{roomId}/members ────────────────
//
// These tests are written FIRST (ATDD gate), before implementation exists.
// ALL tests in this file are expected to FAIL until members.go is created.
//
// Problem: Element Web calls GET /rooms/{roomId}/members after entering a room
// to populate the member list panel. Without this endpoint, every room shows
// "Room members will appear incomplete." and the member sidebar is empty.
//
// Test strategy:
//   - mockGetMembersCoreClient implements GetMembersCoreClient (consumer-defined
//     interface, Go convention) — defined here alongside the tests.
//   - buildAuthedMembersHandler wires JWTMiddleware → GetRoomMembersHandler so
//     the full auth → handler pipeline is exercised at httptest level.
//   - A capturedReq field lets tests inspect the gRPC GetRoomStateRequest.
//   - Happy path: mock returns ["@alice:test.local","@bob:test.local"] →
//     handler returns 200 with Matrix-shaped member event list.
//   - Empty room: mock returns [] → 200 with empty chunk array.
//   - Room not found: gRPC NotFound → 404 M_NOT_FOUND.
//   - Not a member: gRPC PermissionDenied → 403 M_FORBIDDEN.
//   - Unauthenticated: no Bearer → 401 M_MISSING_TOKEN.
//   - Core unavailable: gRPC Unavailable → 503 M_UNAVAILABLE.
//
// NOTE: GetMembersCoreClient, GetRoomMembersHandler, GetRoomMembersConfig,
// NewGetRoomMembersHandler, GetRoomMembers are defined in
// gateway/internal/matrix/members.go — which does NOT exist yet.
// Every test in this file MUST fail with a compilation error until members.go
// is created.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nebu/nebu/internal/auth"
	pb "github.com/nebu/nebu/internal/grpc/pb"
	"github.com/nebu/nebu/internal/middleware"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ─── Mock gRPC core client ────────────────────────────────────────────────────

// mockGetMembersCoreClient implements GetMembersCoreClient (defined in members.go).
// capturedReq records the last GetRoomStateRequest forwarded so tests can assert
// the handler built the correct gRPC payload.

type mockGetMembersCoreClient struct {
	resp        *pb.GetRoomStateResponse
	err         error
	capturedReq *pb.GetRoomStateRequest
}

func (m *mockGetMembersCoreClient) GetRoomState(_ context.Context, req *pb.GetRoomStateRequest) (*pb.GetRoomStateResponse, error) {
	m.capturedReq = req
	return m.resp, m.err
}

// ─── Helper ──────────────────────────────────────────────────────────────────

// buildAuthedMembersHandler wires JWTMiddleware → GetRoomMembersHandler and
// registers it on a mux with the correct GET pattern so PathValue resolves.
//
// JWT sub is always "test-sub-123", authenticated user_id = "@test-sub-123:test.local".
func buildAuthedMembersHandler(t *testing.T, mock *mockGetMembersCoreClient) (http.Handler, *httptest.Server, func() string) {
	t.Helper()

	oidcSrv, privateKey := setupOIDCServer(t)
	t.Cleanup(oidcSrv.Close)

	provider := auth.NewProvider(context.Background(), oidcSrv.URL)

	handler := NewGetRoomMembersHandler(GetRoomMembersConfig{
		CoreClient: mock,
		ServerName: "test.local",
	})

	authed := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, "test.local")(
		http.HandlerFunc(handler.GetRoomMembers),
	)

	mux := http.NewServeMux()
	mux.Handle("GET /rooms/{roomId}/members", authed)

	makeToken := func() string {
		return signJWT(t, oidcSrv.URL, privateKey, time.Now().Add(time.Hour), nil)
	}

	return mux, oidcSrv, makeToken
}

// ─── Test 1: Happy path — room with two members ───────────────────────────────
//
// Mock returns two member IDs. Handler must return 200 with a "chunk" array
// containing one m.room.member state event per member.
// Element Web uses this list to populate the member sidebar.

func TestGetRoomMembers_HappyPath(t *testing.T) {
	mock := &mockGetMembersCoreClient{
		resp: &pb.GetRoomStateResponse{
			Members: []string{"@alice:test.local", "@bob:test.local"},
		},
	}

	mux, _, makeToken := buildAuthedMembersHandler(t, mock)

	req := httptest.NewRequest(
		http.MethodGet,
		"/rooms/!room1%3Atest.local/members",
		nil,
	)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var body struct {
		Chunk []map[string]any `json:"chunk"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not valid JSON: %v; body: %s", err, w.Body.String())
	}

	if len(body.Chunk) != 2 {
		t.Fatalf("expected 2 member events in chunk, got %d", len(body.Chunk))
	}

	// Each chunk entry must be a state event with type m.room.member.
	for i, event := range body.Chunk {
		if event["type"] != "m.room.member" {
			t.Errorf("chunk[%d]: expected type m.room.member, got %v", i, event["type"])
		}
		if _, ok := event["state_key"]; !ok {
			t.Errorf("chunk[%d]: missing state_key field", i)
		}
		content, ok := event["content"].(map[string]any)
		if !ok {
			t.Errorf("chunk[%d]: missing or invalid content field", i)
			continue
		}
		if content["membership"] != "join" {
			t.Errorf("chunk[%d]: expected membership=join, got %v", i, content["membership"])
		}
	}

	// Verify the handler forwarded the correct room_id to the Core.
	if mock.capturedReq == nil {
		t.Fatal("handler did not call Core.GetRoomState")
	}
	if mock.capturedReq.RoomId != "!room1:test.local" {
		t.Errorf("expected room_id !room1:test.local, got %s", mock.capturedReq.RoomId)
	}
}

// ─── Test 2: Empty room → 200 with empty chunk ───────────────────────────────
//
// A room with no members (edge case) must return 200 {"chunk":[]} rather
// than 404 or an error. Element Web handles empty chunk gracefully.

func TestGetRoomMembers_EmptyRoom(t *testing.T) {
	mock := &mockGetMembersCoreClient{
		resp: &pb.GetRoomStateResponse{
			Members: []string{},
		},
	}

	mux, _, makeToken := buildAuthedMembersHandler(t, mock)

	req := httptest.NewRequest(
		http.MethodGet,
		"/rooms/!empty%3Atest.local/members",
		nil,
	)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var body struct {
		Chunk []any `json:"chunk"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if body.Chunk == nil {
		t.Error("expected chunk to be an empty array, got null")
	}
	if len(body.Chunk) != 0 {
		t.Errorf("expected 0 chunk entries, got %d", len(body.Chunk))
	}
}

// ─── Test 3: Room not found → 404 M_NOT_FOUND ────────────────────────────────

func TestGetRoomMembers_RoomNotFound(t *testing.T) {
	mock := &mockGetMembersCoreClient{
		err: status.Error(codes.NotFound, "room not found"),
	}

	mux, _, makeToken := buildAuthedMembersHandler(t, mock)

	req := httptest.NewRequest(
		http.MethodGet,
		"/rooms/!unknown%3Atest.local/members",
		nil,
	)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d; body: %s", w.Code, w.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("error response is not valid JSON: %v", err)
	}
	if body["errcode"] != "M_NOT_FOUND" {
		t.Errorf("expected errcode M_NOT_FOUND, got %v", body["errcode"])
	}
}

// ─── Test 4: Not a member → 403 M_FORBIDDEN ──────────────────────────────────
//
// Core returns PermissionDenied when the requesting user is not a member
// of the room. The handler must map this to 403 M_FORBIDDEN.

func TestGetRoomMembers_NotMember(t *testing.T) {
	mock := &mockGetMembersCoreClient{
		err: status.Error(codes.PermissionDenied, "not a member"),
	}

	mux, _, makeToken := buildAuthedMembersHandler(t, mock)

	req := httptest.NewRequest(
		http.MethodGet,
		"/rooms/!room1%3Atest.local/members",
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
		t.Errorf("expected errcode M_FORBIDDEN, got %v", body["errcode"])
	}
}

// ─── Test 5: Unauthenticated → 401 ───────────────────────────────────────────

func TestGetRoomMembers_Unauthenticated(t *testing.T) {
	mock := &mockGetMembersCoreClient{}
	mux, _, _ := buildAuthedMembersHandler(t, mock)

	req := httptest.NewRequest(
		http.MethodGet,
		"/rooms/!room1%3Atest.local/members",
		nil,
	)
	// No Authorization header.

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d; body: %s", w.Code, w.Body.String())
	}

	// Core must not have been called.
	if mock.capturedReq != nil {
		t.Error("Core.GetRoomState must not be called for unauthenticated requests")
	}
}

// ─── Test 6: Core unavailable → 503 M_UNAVAILABLE ────────────────────────────

func TestGetRoomMembers_CoreUnavailable(t *testing.T) {
	mock := &mockGetMembersCoreClient{
		err: status.Error(codes.Unavailable, "core is down"),
	}

	mux, _, makeToken := buildAuthedMembersHandler(t, mock)

	req := httptest.NewRequest(
		http.MethodGet,
		"/rooms/!room1%3Atest.local/members",
		nil,
	)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d; body: %s", w.Code, w.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("error response is not valid JSON: %v", err)
	}
	if body["errcode"] != "M_UNAVAILABLE" {
		t.Errorf("expected errcode M_UNAVAILABLE, got %v", body["errcode"])
	}
}
