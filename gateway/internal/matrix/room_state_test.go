package matrix

// ─── Story 7-19: GET /_matrix/client/v3/rooms/{roomId}/state ─────────────────
//
// Tests written FIRST (ATDD gate), before implementation exists.
// ALL tests in this file MUST FAIL until rooms.go contains GetRoomStateHandler.
//
// Acceptance Criteria covered:
//   AC1  — GET /state → 200 JSON array of all state events (type, state_key, content, sender)
//   AC2  — GET /state/{eventType}/{stateKey} → 200 content object only
//   AC3  — GET /state/{eventType} (no stateKey) → same as stateKey ""
//   AC4  — Non-member → 403 M_FORBIDDEN (gRPC PermissionDenied)
//   AC5  — Unknown room → 404 M_NOT_FOUND (gRPC NotFound)
//   AC6  — Known room, unknown eventType/stateKey → 404 M_NOT_FOUND (gRPC NotFound)
//   AC7  — No valid JWT → 401 M_MISSING_TOKEN (jwtMiddleware, no handler call)
//   AC8  — Proto backward compat: existing /members caller unaffected (tested implicitly
//           via GetMembersHandler tests — those must still pass; not re-tested here)
//
// Design decisions:
//   - GetRoomStateCoreClient is a new consumer-defined interface (Go convention, ADR-009)
//     declared here alongside the tests; it will live in rooms.go when implemented.
//   - mockGetRoomStateCoreClient records capturedReq so tests assert correct gRPC payload.
//   - buildAuthedGetRoomStateHandler registers all three route variants on a ServeMux so
//     PathValue("roomId"), PathValue("eventType"), PathValue("stateKey") resolve correctly.
//   - SyncRoomStateEvent is used as the element type in GetRoomStateResponse.StateEvents
//     (proto field 4, added in this story).

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

// ─── Consumer-defined interface ──────────────────────────────────────────────
//
// GetRoomStateCoreClient is intentionally separate from GetMembersCoreClient
// so the two handlers evolve independently (CLAUDE.md: interfaces small,
// defined by consumer).
//
// NOTE: This type will be declared in gateway/internal/matrix/rooms.go once
// Story 7-19 is implemented. Until then, this file will not compile.

// ─── Mock ────────────────────────────────────────────────────────────────────

type mockGetRoomStateCoreClient struct {
	resp        *pb.GetRoomStateResponse
	err         error
	capturedReq *pb.GetRoomStateRequest
}

func (m *mockGetRoomStateCoreClient) GetRoomState(_ context.Context, req *pb.GetRoomStateRequest) (*pb.GetRoomStateResponse, error) {
	m.capturedReq = req
	return m.resp, m.err
}

// ─── Helper ──────────────────────────────────────────────────────────────────
//
// buildAuthedGetRoomStateHandler wires JWTMiddleware → GetRoomStateHandler on
// all three route patterns so PathValue resolution works in tests.
//
// The handler types (GetRoomStateHandler, GetRoomStateConfig,
// NewGetRoomStateHandler) are expected to be declared in rooms.go.
// This file will not compile until they exist.

func buildAuthedGetRoomStateHandler(t *testing.T, mock *mockGetRoomStateCoreClient) (http.Handler, func() string) {
	t.Helper()

	oidcSrv, privateKey := setupOIDCServer(t)
	t.Cleanup(oidcSrv.Close)

	provider := auth.NewProvider(context.Background(), oidcSrv.URL)

	handler := NewGetRoomStateHandler(GetRoomStateConfig{
		CoreClient: mock,
		ServerName: "test.local",
	})

	authedAll := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, "test.local")(
		http.HandlerFunc(handler.GetRoomState),
	)
	authedSingle := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, "test.local")(
		http.HandlerFunc(handler.GetRoomStateSingleEvent),
	)

	mux := http.NewServeMux()
	mux.Handle("GET /_matrix/client/v3/rooms/{roomId}/state", authedAll)
	mux.Handle("GET /_matrix/client/v3/rooms/{roomId}/state/{eventType}/{stateKey}", authedSingle)
	// Trailing-slash variant: "GET /state/{eventType}/" treats stateKey as "" (AC3 / AC6).
	// In Go 1.22 ServeMux a pattern ending in "/" is a subtree pattern; the more specific
	// "/{eventType}/{stateKey}" pattern takes precedence when stateKey is non-empty.
	mux.Handle("GET /_matrix/client/v3/rooms/{roomId}/state/{eventType}/", authedSingle)
	mux.Handle("GET /_matrix/client/v3/rooms/{roomId}/state/{eventType}", authedSingle)

	makeToken := func() string {
		return signJWT(t, oidcSrv.URL, privateKey, time.Now().Add(time.Hour), nil)
	}

	return mux, makeToken
}

// ─── AC1: GET /state → 200 JSON array ────────────────────────────────────────
//
// Mock returns two SyncRoomStateEvents. Handler must return 200 with a JSON
// array where each element has at minimum "type", "state_key", "content",
// and "sender" fields (Matrix spec shape).

func TestGetRoomState_AllEvents_HappyPath(t *testing.T) {
	mock := &mockGetRoomStateCoreClient{
		resp: &pb.GetRoomStateResponse{
			StateEvents: []*pb.SyncRoomStateEvent{
				{
					Type:     "m.room.create",
					StateKey: "",
					Content:  []byte(`{"creator":"@alice:test.local"}`),
					Sender:   "@alice:test.local",
				},
				{
					Type:     "m.room.member",
					StateKey: "@alice:test.local",
					Content:  []byte(`{"membership":"join"}`),
					Sender:   "@alice:test.local",
				},
			},
		},
	}

	mux, makeToken := buildAuthedGetRoomStateHandler(t, mock)

	req := httptest.NewRequest(
		http.MethodGet,
		"/_matrix/client/v3/rooms/!room1:test.local/state",
		nil,
	)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// Response must be a JSON array.
	var events []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &events); err != nil {
		t.Fatalf("response is not a JSON array: %v; body: %s", err, w.Body.String())
	}

	if len(events) != 2 {
		t.Fatalf("expected 2 state events, got %d", len(events))
	}

	// Each event must have the required Matrix spec fields.
	for i, ev := range events {
		for _, field := range []string{"type", "state_key", "content", "sender"} {
			if _, ok := ev[field]; !ok {
				t.Errorf("event[%d]: missing required field %q", i, field)
			}
		}
	}

	// Handler must have forwarded the correct room_id with empty event_type filter.
	if mock.capturedReq == nil {
		t.Fatal("handler did not call Core.GetRoomState")
	}
	if mock.capturedReq.RoomId != "!room1:test.local" {
		t.Errorf("expected room_id !room1:test.local, got %s", mock.capturedReq.RoomId)
	}
	if mock.capturedReq.EventType != "" {
		t.Errorf("expected empty event_type filter for /state, got %q", mock.capturedReq.EventType)
	}
}

// ─── AC1 (edge): GET /state with empty state_events → 200 empty array ────────
//
// Newly created room with no state beyond initial events is still valid.
// Response must be [] (empty JSON array), never null.

func TestGetRoomState_AllEvents_EmptyStateEvents(t *testing.T) {
	mock := &mockGetRoomStateCoreClient{
		resp: &pb.GetRoomStateResponse{
			StateEvents: []*pb.SyncRoomStateEvent{},
		},
	}

	mux, makeToken := buildAuthedGetRoomStateHandler(t, mock)

	req := httptest.NewRequest(
		http.MethodGet,
		"/_matrix/client/v3/rooms/!empty:test.local/state",
		nil,
	)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var events []any
	if err := json.Unmarshal(w.Body.Bytes(), &events); err != nil {
		t.Fatalf("response is not a JSON array: %v; body: %s", err, w.Body.String())
	}
	if events == nil {
		t.Error("expected empty array [], got null")
	}
	if len(events) != 0 {
		t.Errorf("expected 0 state events, got %d", len(events))
	}
}

// ─── AC2: GET /state/{eventType}/{stateKey} → 200 content object ─────────────
//
// Matrix spec: the response is the raw content block — NOT the full event
// envelope. Handler must NOT wrap the content in {"type":..., "content":...}.

func TestGetRoomState_SingleEvent_WithStateKey(t *testing.T) {
	mock := &mockGetRoomStateCoreClient{
		resp: &pb.GetRoomStateResponse{
			StateEvents: []*pb.SyncRoomStateEvent{
				{
					Type:     "m.room.member",
					StateKey: "@alice:test.local",
					Content:  []byte(`{"membership":"join","displayname":"Alice"}`),
					Sender:   "@alice:test.local",
				},
			},
		},
	}

	mux, makeToken := buildAuthedGetRoomStateHandler(t, mock)

	req := httptest.NewRequest(
		http.MethodGet,
		"/_matrix/client/v3/rooms/!room1:test.local/state/m.room.member/@alice:test.local",
		nil,
	)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// Response must be the raw content object, not a full event envelope.
	var content map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &content); err != nil {
		t.Fatalf("response is not a JSON object: %v; body: %s", err, w.Body.String())
	}

	// Must have content fields, not event envelope fields.
	if _, ok := content["membership"]; !ok {
		t.Error("expected 'membership' field in content object")
	}
	// Must NOT be wrapped in a full event envelope.
	if _, ok := content["type"]; ok {
		t.Error("response must be raw content only, not a full event envelope with 'type' field")
	}
	if _, ok := content["state_key"]; ok {
		t.Error("response must be raw content only, not a full event envelope with 'state_key' field")
	}

	// Handler must have forwarded the correct filters.
	if mock.capturedReq == nil {
		t.Fatal("handler did not call Core.GetRoomState")
	}
	if mock.capturedReq.EventType != "m.room.member" {
		t.Errorf("expected event_type m.room.member, got %q", mock.capturedReq.EventType)
	}
	if mock.capturedReq.StateKey != "@alice:test.local" {
		t.Errorf("expected state_key @alice:test.local, got %q", mock.capturedReq.StateKey)
	}
}

// ─── AC3: GET /state/{eventType} (no stateKey) → same as stateKey "" ─────────
//
// When no stateKey path segment is present, the handler treats it as stateKey="".
// Response is still the raw content object.

func TestGetRoomState_SingleEvent_EmptyStateKey(t *testing.T) {
	mock := &mockGetRoomStateCoreClient{
		resp: &pb.GetRoomStateResponse{
			StateEvents: []*pb.SyncRoomStateEvent{
				{
					Type:     "m.room.name",
					StateKey: "",
					Content:  []byte(`{"name":"My Room"}`),
					Sender:   "@alice:test.local",
				},
			},
		},
	}

	mux, makeToken := buildAuthedGetRoomStateHandler(t, mock)

	// No stateKey segment — the {eventType} route handles this.
	req := httptest.NewRequest(
		http.MethodGet,
		"/_matrix/client/v3/rooms/!room1:test.local/state/m.room.name",
		nil,
	)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var content map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &content); err != nil {
		t.Fatalf("response is not a JSON object: %v; body: %s", err, w.Body.String())
	}
	if _, ok := content["name"]; !ok {
		t.Error("expected 'name' field in m.room.name content")
	}

	// Handler must have forwarded state_key="" (empty string default).
	if mock.capturedReq == nil {
		t.Fatal("handler did not call Core.GetRoomState")
	}
	if mock.capturedReq.EventType != "m.room.name" {
		t.Errorf("expected event_type m.room.name, got %q", mock.capturedReq.EventType)
	}
	if mock.capturedReq.StateKey != "" {
		t.Errorf("expected empty state_key for single-segment route, got %q", mock.capturedReq.StateKey)
	}
}

// ─── AC4: Non-member → 403 M_FORBIDDEN ───────────────────────────────────────
//
// Core returns PermissionDenied when the requesting user is not a room member.
// Applies to both GET /state and GET /state/{eventType}/{stateKey}.

func TestGetRoomState_Forbidden_NonMember(t *testing.T) {
	mock := &mockGetRoomStateCoreClient{
		err: status.Error(codes.PermissionDenied, "not a member of this room"),
	}

	mux, makeToken := buildAuthedGetRoomStateHandler(t, mock)

	req := httptest.NewRequest(
		http.MethodGet,
		"/_matrix/client/v3/rooms/!private:test.local/state",
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

// AC4 variant: non-member trying to fetch a single event also gets 403.
func TestGetRoomState_Forbidden_NonMember_SingleEvent(t *testing.T) {
	mock := &mockGetRoomStateCoreClient{
		err: status.Error(codes.PermissionDenied, "not a member"),
	}

	mux, makeToken := buildAuthedGetRoomStateHandler(t, mock)

	req := httptest.NewRequest(
		http.MethodGet,
		"/_matrix/client/v3/rooms/!private:test.local/state/m.room.name",
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

// ─── AC5: Unknown room → 404 M_NOT_FOUND ─────────────────────────────────────
//
// Core returns NotFound when the room does not exist.

func TestGetRoomState_NotFound_UnknownRoom(t *testing.T) {
	mock := &mockGetRoomStateCoreClient{
		err: status.Error(codes.NotFound, "room not found"),
	}

	mux, makeToken := buildAuthedGetRoomStateHandler(t, mock)

	req := httptest.NewRequest(
		http.MethodGet,
		"/_matrix/client/v3/rooms/!unknown:test.local/state",
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

// ─── AC6: Known room, unknown eventType/stateKey → 404 M_NOT_FOUND ───────────
//
// When a specific eventType+stateKey combination has no current state event,
// Core returns NotFound. Handler must map it to 404 M_NOT_FOUND.

func TestGetRoomState_NotFound_UnknownEventType(t *testing.T) {
	mock := &mockGetRoomStateCoreClient{
		err: status.Error(codes.NotFound, "no state event for type"),
	}

	mux, makeToken := buildAuthedGetRoomStateHandler(t, mock)

	req := httptest.NewRequest(
		http.MethodGet,
		"/_matrix/client/v3/rooms/!room1:test.local/state/m.room.nonexistent/",
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

// ─── AC7: No valid JWT → 401 ──────────────────────────────────────────────────
//
// jwtMiddleware rejects requests without a valid Bearer token before the handler
// is reached. Core must not be called.

func TestGetRoomState_Unauthenticated(t *testing.T) {
	mock := &mockGetRoomStateCoreClient{}
	mux, _ := buildAuthedGetRoomStateHandler(t, mock)

	req := httptest.NewRequest(
		http.MethodGet,
		"/_matrix/client/v3/rooms/!room1:test.local/state",
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

// ─── Core unavailable → 503 M_UNAVAILABLE ────────────────────────────────────
//
// Consistent with error-mapping pattern from story implementation notes.

func TestGetRoomState_CoreUnavailable(t *testing.T) {
	mock := &mockGetRoomStateCoreClient{
		err: status.Error(codes.Unavailable, "core is down"),
	}

	mux, makeToken := buildAuthedGetRoomStateHandler(t, mock)

	req := httptest.NewRequest(
		http.MethodGet,
		"/_matrix/client/v3/rooms/!room1:test.local/state",
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

// ─── AC8 proto backward-compat: gRPC request for /members still works ────────
//
// The GetRoomStateRequest.EventType and StateKey fields default to "" when not
// set — so the existing GetRoomMembersHandler, which passes only RoomId, must
// still produce a valid request. This is tested implicitly by existing
// TestGetRoomMembers_* tests in members_test.go — no code duplication here.
// Documented for traceability.
