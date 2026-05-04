package matrix

// ─── Story 9-7: Room State Event Types — Full Implementation ─────────────────
//
// Tests written FIRST (ATDD gate, red phase), before implementation exists.
// ALL tests in this file MUST FAIL until:
//   - SetRoomStateCoreClient interface gains a SendEvent method
//   - PutSetRoomState replaces the 501 fallback with a Core.SendEvent gRPC call
//   - proto/core.proto gains `string state_key = 7` in SendEventRequest
//   - make proto regenerates core.pb.go with SendEventRequest.StateKey field
//
// Acceptance Criteria covered:
//   AC1 — PUT /rooms/{roomId}/state/m.room.name with valid body → 200 (NOT 501)
//   AC2 — PUT /rooms/{roomId}/state/m.room.encryption → 200 (NOT 501)
//   AC3 — PUT /rooms/{roomId}/state/m.room.join_rules → 200 (NOT 501)
//   AC4 — The 501 fallback is replaced; all whitelisted types reach Core.SendEvent
//   AC4b — Core.SendEvent is called with correct room_id, event_type, state_key
//   AC4c — gRPC PermissionDenied → 403 M_FORBIDDEN on SendEvent path
//   AC4d — gRPC NotFound → 404 M_NOT_FOUND on SendEvent path
//   AC5  — All 16 whitelisted non-power_levels types return 200 (not 501)

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

// ─── Interface compliance check ───────────────────────────────────────────────
//
// Compile-time assertion: mockSetRoomStateCoreClientV2 MUST satisfy
// SetRoomStateCoreClient. Once SendEvent is added to the interface in rooms.go,
// this assertion ensures the mock also satisfies the new requirement.
//
// If SendEvent is NOT added to SetRoomStateCoreClient AND the mock does not
// implement the full interface, this line will produce a compile error:
//
//   cannot use (*mockSetRoomStateCoreClientV2)(nil) (type *mockSetRoomStateCoreClientV2)
//   as type SetRoomStateCoreClient: wrong method set
//
// Note: currently the assertion compiles because the mock satisfies the minimal
// interface. The key failing signal until proto is updated is the StateKey field
// references below.
var _ SetRoomStateCoreClient = (*mockSetRoomStateCoreClientV2)(nil)

// ─── Mock gRPC core client (V2: implements both SetPowerLevels + SendEvent) ───
//
// This mock will NOT compile until SetRoomStateCoreClient requires SendEvent.
// That is the intentional red-phase compilation failure.

type mockSetRoomStateCoreClientV2 struct {
	setPowerLevelsResp *pb.SetPowerLevelsResponse
	setPowerLevelsErr  error
	capturedPLReq      *pb.SetPowerLevelsRequest

	// SendEvent fields — will cause compile error until SetRoomStateCoreClient
	// requires SendEvent AND pb.SendEventRequest gains StateKey field.
	sendEventResp    *pb.SendEventResponse
	sendEventErr     error
	capturedSEReq    *pb.SendEventRequest
}

func (m *mockSetRoomStateCoreClientV2) SetPowerLevels(_ context.Context, req *pb.SetPowerLevelsRequest) (*pb.SetPowerLevelsResponse, error) {
	m.capturedPLReq = req
	return m.setPowerLevelsResp, m.setPowerLevelsErr
}

// SendEvent will not satisfy the SetRoomStateCoreClient interface until
// the interface is extended with SendEvent in rooms.go (Story 9-7, AC4).
func (m *mockSetRoomStateCoreClientV2) SendEvent(_ context.Context, req *pb.SendEventRequest) (*pb.SendEventResponse, error) {
	m.capturedSEReq = req
	return m.sendEventResp, m.sendEventErr
}

// ─── Helper ──────────────────────────────────────────────────────────────────

// buildAuthedSetRoomStateHandlerV2 wraps SetRoomStateHandler.PutSetRoomState
// with JWTMiddleware, accepting the broader SetRoomStateCoreClient interface
// so tests can inject mockSetRoomStateCoreClientV2.
//
// Deliberately distinct from buildAuthedSetRoomStateHandler (which accepts the
// old mock) so existing tests in rooms_test.go and state_event_whitelist_test.go
// are unaffected.
func buildAuthedSetRoomStateHandlerV2(t *testing.T, mock SetRoomStateCoreClient) (http.Handler, func() string) {
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
	mux.Handle("PUT /_matrix/client/v3/rooms/{roomId}/state/{eventType}/{stateKey}", authedHandler)
	mux.Handle("PUT /_matrix/client/v3/rooms/{roomId}/state/{eventType}", authedHandler)

	makeToken := func() string {
		return signJWT(t, oidcSrv.URL, privateKey, time.Now().Add(time.Hour), nil)
	}

	return mux, makeToken
}

// ─── AC1 — m.room.name returns 200 (NOT 501) ─────────────────────────────────
//
// Before this story: handler returns 501 for m.room.name.
// After implementation: returns 200 {"event_id": "..."}.
// This test FAILS until the 501 fallback is replaced with Core.SendEvent.

func TestPutSetRoomState_mRoomName_Returns200(t *testing.T) {
	mock := &mockSetRoomStateCoreClientV2{
		setPowerLevelsResp: &pb.SetPowerLevelsResponse{},
		sendEventResp:      &pb.SendEventResponse{EventId: "$state-event-id-1"},
	}

	mux, makeToken := buildAuthedSetRoomStateHandlerV2(t, mock)

	body := `{"name":"My Room"}`
	req := httptest.NewRequest(http.MethodPut,
		"/_matrix/client/v3/rooms/!room1:test.local/state/m.room.name",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// MUST be 200 — currently 501 until AC4 (replace 501 fallback) is implemented.
	if w.Code != http.StatusOK {
		t.Fatalf("AC1: expected 200 for m.room.name, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		EventID string `json:"event_id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("AC1: response body is not valid JSON: %v; body: %s", err, w.Body.String())
	}

	// Core.SendEvent must have been called.
	if mock.capturedSEReq == nil {
		t.Fatal("AC1: Core.SendEvent must be called for m.room.name, but was not")
	}
	if mock.capturedSEReq.RoomId != "!room1:test.local" {
		t.Errorf("AC1: expected RoomId '!room1:test.local', got %q", mock.capturedSEReq.RoomId)
	}
	if mock.capturedSEReq.EventType != "m.room.name" {
		t.Errorf("AC1: expected EventType 'm.room.name', got %q", mock.capturedSEReq.EventType)
	}
	// state_key for m.room.name is "" (empty string per Matrix spec).
	// This assertion will not compile until SendEventRequest.StateKey exists in core.pb.go.
	if mock.capturedSEReq.StateKey != "" {
		t.Errorf("AC1: expected empty StateKey for m.room.name, got %q", mock.capturedSEReq.StateKey)
	}
}

// ─── AC2 — m.room.encryption returns 200 (NOT 501) ───────────────────────────
//
// Matrix spec Section 11.2.1 mandates encryption state pass-through.
// Before this story: handler returns 501.
// After implementation: returns 200.

func TestPutSetRoomState_mRoomEncryption_Returns200(t *testing.T) {
	mock := &mockSetRoomStateCoreClientV2{
		setPowerLevelsResp: &pb.SetPowerLevelsResponse{},
		sendEventResp:      &pb.SendEventResponse{EventId: "$state-event-enc-1"},
	}

	mux, makeToken := buildAuthedSetRoomStateHandlerV2(t, mock)

	body := `{"algorithm":"m.megolm.v1.aes-sha2"}`
	req := httptest.NewRequest(http.MethodPut,
		"/_matrix/client/v3/rooms/!room1:test.local/state/m.room.encryption",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// MUST be 200 — currently 501 until the 501 fallback is replaced.
	if w.Code != http.StatusOK {
		t.Fatalf("AC2: expected 200 for m.room.encryption, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		EventID string `json:"event_id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("AC2: response body is not valid JSON: %v; body: %s", err, w.Body.String())
	}

	// Core.SendEvent must have been called for m.room.encryption.
	if mock.capturedSEReq == nil {
		t.Fatal("AC2: Core.SendEvent must be called for m.room.encryption, but was not")
	}
	if mock.capturedSEReq.EventType != "m.room.encryption" {
		t.Errorf("AC2: expected EventType 'm.room.encryption', got %q", mock.capturedSEReq.EventType)
	}
}

// ─── AC3 — m.room.join_rules returns 200 (NOT 501) ───────────────────────────
//
// Before this story: handler returns 501 for m.room.join_rules.
// After implementation: returns 200 and Core.SendEvent is called.

func TestPutSetRoomState_mRoomJoinRules_Returns200(t *testing.T) {
	mock := &mockSetRoomStateCoreClientV2{
		setPowerLevelsResp: &pb.SetPowerLevelsResponse{},
		sendEventResp:      &pb.SendEventResponse{EventId: "$state-event-jr-1"},
	}

	mux, makeToken := buildAuthedSetRoomStateHandlerV2(t, mock)

	body := `{"join_rule":"invite"}`
	req := httptest.NewRequest(http.MethodPut,
		"/_matrix/client/v3/rooms/!room1:test.local/state/m.room.join_rules",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("AC3: expected 200 for m.room.join_rules, got %d; body: %s", w.Code, w.Body.String())
	}

	if mock.capturedSEReq == nil {
		t.Fatal("AC3: Core.SendEvent must be called for m.room.join_rules, but was not")
	}
}

// ─── AC4 — No 501 fallback for m.room.topic ──────────────────────────────────
//
// Verifies the 501 fallback path is gone: the handler must NOT return 501
// for any whitelisted event type. Uses m.room.topic as representative.
// This is the critical "red test" — currently the handler hits the 501 branch.

func TestPutSetRoomState_No501Fallback_mRoomTopic(t *testing.T) {
	mock := &mockSetRoomStateCoreClientV2{
		setPowerLevelsResp: &pb.SetPowerLevelsResponse{},
		sendEventResp:      &pb.SendEventResponse{EventId: "$state-event-topic-1"},
	}

	mux, makeToken := buildAuthedSetRoomStateHandlerV2(t, mock)

	body := `{"topic":"Welcome to the room"}`
	req := httptest.NewRequest(http.MethodPut,
		"/_matrix/client/v3/rooms/!room1:test.local/state/m.room.topic",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// The 501 fallback MUST be gone — any other status code (200, 403, 500) is
	// acceptable in the transition, but 501 means the fallback is still present.
	if w.Code == http.StatusNotImplemented {
		t.Fatalf("AC4: 501 fallback must be replaced; got 501 for m.room.topic; body: %s", w.Body.String())
	}
}

// ─── AC4b — Core.SendEvent called with correct fields ─────────────────────────
//
// Verifies that when the handler delegates to Core.SendEvent for a whitelisted
// state event, it populates RoomId, EventType, and StateKey correctly.
// StateKey for m.room.avatar is "" (empty string).

func TestPutSetRoomState_SendEvent_CorrectFields(t *testing.T) {
	mock := &mockSetRoomStateCoreClientV2{
		setPowerLevelsResp: &pb.SetPowerLevelsResponse{},
		sendEventResp:      &pb.SendEventResponse{EventId: "$state-event-avatar-1"},
	}

	mux, makeToken := buildAuthedSetRoomStateHandlerV2(t, mock)

	body := `{"url":"mxc://test.local/avatar123"}`
	req := httptest.NewRequest(http.MethodPut,
		"/_matrix/client/v3/rooms/!room1:test.local/state/m.room.avatar",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("AC4b: expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	if mock.capturedSEReq == nil {
		t.Fatal("AC4b: Core.SendEvent must be called, but was not")
	}
	if mock.capturedSEReq.RoomId != "!room1:test.local" {
		t.Errorf("AC4b: expected RoomId '!room1:test.local', got %q", mock.capturedSEReq.RoomId)
	}
	if mock.capturedSEReq.EventType != "m.room.avatar" {
		t.Errorf("AC4b: expected EventType 'm.room.avatar', got %q", mock.capturedSEReq.EventType)
	}
	// StateKey must be "" for m.room.avatar.
	// Will not compile until SendEventRequest.StateKey exists in core.pb.go.
	if mock.capturedSEReq.StateKey != "" {
		t.Errorf("AC4b: expected empty StateKey for m.room.avatar, got %q", mock.capturedSEReq.StateKey)
	}
	// Content must be non-empty (the JSON-encoded body).
	if len(mock.capturedSEReq.Content) == 0 {
		t.Error("AC4b: Content must be non-empty JSON-encoded body")
	}
}

// ─── AC4c — Core.SendEvent returns PermissionDenied → 403 M_FORBIDDEN ────────
//
// When Core returns PERMISSION_DENIED on a non-power_levels state event,
// the handler must map it to 403 M_FORBIDDEN.

func TestPutSetRoomState_SendEvent_PermissionDenied_Returns403(t *testing.T) {
	mock := &mockSetRoomStateCoreClientV2{
		setPowerLevelsResp: &pb.SetPowerLevelsResponse{},
		sendEventErr:       status.Error(codes.PermissionDenied, "insufficient power level"),
	}

	mux, makeToken := buildAuthedSetRoomStateHandlerV2(t, mock)

	body := `{"name":"Forbidden Room Name"}`
	req := httptest.NewRequest(http.MethodPut,
		"/_matrix/client/v3/rooms/!room1:test.local/state/m.room.name",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("AC4c: expected 403 for PermissionDenied on SendEvent, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixError
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("AC4c: error response is not valid JSON: %v; body: %s", err, w.Body.String())
	}
	if errResp.ErrCode != "M_FORBIDDEN" {
		t.Errorf("AC4c: expected errcode M_FORBIDDEN, got %q", errResp.ErrCode)
	}
}

// ─── AC4d — Core.SendEvent returns NotFound → 404 M_NOT_FOUND ─────────────────
//
// When Core returns NOT_FOUND on a state event, the handler must map it to
// 404 M_NOT_FOUND (room not found).

func TestPutSetRoomState_SendEvent_NotFound_Returns404(t *testing.T) {
	mock := &mockSetRoomStateCoreClientV2{
		setPowerLevelsResp: &pb.SetPowerLevelsResponse{},
		sendEventErr:       status.Error(codes.NotFound, "room not found"),
	}

	mux, makeToken := buildAuthedSetRoomStateHandlerV2(t, mock)

	body := `{"name":"Ghost Room"}`
	req := httptest.NewRequest(http.MethodPut,
		"/_matrix/client/v3/rooms/!nonexistent:test.local/state/m.room.name",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("AC4d: expected 404 for NotFound on SendEvent, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixError
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("AC4d: error response is not valid JSON: %v; body: %s", err, w.Body.String())
	}
	if errResp.ErrCode != "M_NOT_FOUND" {
		t.Errorf("AC4d: expected errcode M_NOT_FOUND, got %q", errResp.ErrCode)
	}
}

// ─── AC5 — All 16 whitelisted state event types return 200 (NOT 501) ──────────
//
// Iterates over all whitelisted non-power_levels event types and asserts each
// returns 200. Any single 501 response is a failure.
//
// This test will fail until the 501 fallback is replaced for ALL types.

func TestPutSetRoomState_AllWhitelistedTypes_Return200(t *testing.T) {
	// All whitelisted types except m.room.power_levels (which has its own path).
	stateEventTypes := []struct {
		eventType string
		body      string
	}{
		{"m.room.name", `{"name":"Test"}`},
		{"m.room.topic", `{"topic":"Test topic"}`},
		{"m.room.avatar", `{"url":"mxc://test.local/abc"}`},
		{"m.room.canonical_alias", `{"alias":"#test:test.local"}`},
		{"m.room.encryption", `{"algorithm":"m.megolm.v1.aes-sha2"}`},
		{"m.room.history_visibility", `{"history_visibility":"shared"}`},
		{"m.room.guest_access", `{"guest_access":"forbidden"}`},
		{"m.room.server_acl", `{"allow":["*"],"deny":[]}`},
		{"m.room.tombstone", `{"body":"Room tombstoned","replacement_room":"!new:test.local"}`},
		{"m.room.pinned_events", `{"pinned":["$event1"]}`},
		{"m.room.join_rules", `{"join_rule":"invite"}`},
		{"m.room.create", `{"creator":"@kai:test.local"}`},
		{"m.room.member", `{"membership":"join"}`},
		{"m.space.child", `{"via":["test.local"]}`},
		{"m.space.parent", `{"via":["test.local"]}`},
	}

	for _, tc := range stateEventTypes {
		t.Run(tc.eventType, func(t *testing.T) {
			mock := &mockSetRoomStateCoreClientV2{
				setPowerLevelsResp: &pb.SetPowerLevelsResponse{},
				sendEventResp:      &pb.SendEventResponse{EventId: "$state-event-" + tc.eventType},
			}

			mux, makeToken := buildAuthedSetRoomStateHandlerV2(t, mock)

			req := httptest.NewRequest(http.MethodPut,
				"/_matrix/client/v3/rooms/!room1:test.local/state/"+tc.eventType,
				strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+makeToken())

			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			// 501 means the fallback is still present — this is the primary failure signal.
			if w.Code == http.StatusNotImplemented {
				t.Fatalf("AC5: %s returned 501 Not Implemented — 501 fallback must be replaced; body: %s",
					tc.eventType, w.Body.String())
			}

			// After AC4 is implemented, all these types must return exactly 200.
			if w.Code != http.StatusOK {
				t.Fatalf("AC5: %s expected 200, got %d; body: %s",
					tc.eventType, w.Code, w.Body.String())
			}
		})
	}
}

// ─── Regression: power_levels path must still use SetPowerLevels (not SendEvent) ──
//
// The m.room.power_levels branch must NOT be affected by the SendEvent delegation.
// SetPowerLevels must be called; SendEvent must NOT be called.

func TestPutSetRoomState_PowerLevels_StillUseSetPowerLevels(t *testing.T) {
	mock := &mockSetRoomStateCoreClientV2{
		setPowerLevelsResp: &pb.SetPowerLevelsResponse{},
	}

	mux, makeToken := buildAuthedSetRoomStateHandlerV2(t, mock)

	body := `{"ban":50,"invite":0,"kick":50}`
	req := httptest.NewRequest(http.MethodPut,
		"/_matrix/client/v3/rooms/!room1:test.local/state/m.room.power_levels",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("regression: m.room.power_levels must still return 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// SetPowerLevels must be called for power_levels.
	if mock.capturedPLReq == nil {
		t.Error("regression: SetPowerLevels must be called for m.room.power_levels, but was not")
	}

	// SendEvent must NOT be called for power_levels — it has its own dedicated handler.
	if mock.capturedSEReq != nil {
		t.Error("regression: SendEvent must NOT be called for m.room.power_levels")
	}
}
