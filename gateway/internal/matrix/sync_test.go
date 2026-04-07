package matrix

// ─── Story 4-14: GET /_matrix/client/v3/sync — Initial Sync ──────────────────
//
// These tests are written FIRST (ATDD gate), before implementation exists.
// ALL tests in this file are expected to FAIL until Story 4-14 is implemented.
//
// Test strategy:
//   - mockGetSyncCoreClient implements GetSyncCoreClient (consumer-defined
//     interface, Go convention) — defined here alongside the tests.
//   - buildAuthedSyncHandler wires JWTMiddleware → GetSyncHandler so the full
//     auth → handler pipeline is exercised at httptest level.
//   - Initial sync (no ?since param) must return 200 with next_batch + rooms.join.
//   - Incremental sync (?since=<token>) is not yet implemented — handler must
//     return 501 Not Implemented (Story 4-15 placeholder).
//   - gRPC UNAVAILABLE error → 503 M_UNAVAILABLE.
//   - Empty rooms → 200 with rooms.join == {}.
//   - Context timeout: mock delays > 5 s → handler times out → 503 or 504.
//   - Unauthenticated request (no JWT) → JWTMiddleware returns 401 before handler.
//
// NOTE: GetSyncCoreClient, GetSyncHandler, NewGetSyncHandler, GetSyncConfig are
// defined in gateway/internal/matrix/sync.go — which does NOT exist yet.
// Every test in this file MUST fail with a compilation error until sync.go is
// created and the proto stubs for GetInitialSync are generated.

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
//
// mockGetSyncCoreClient implements GetSyncCoreClient (defined in sync.go).
// The delayFor field lets individual tests simulate a slow Core by sleeping
// before returning — used by TestGetSync_ContextTimeout.

type mockGetSyncCoreClient struct {
	resp        *pb.GetInitialSyncResponse
	err         error
	capturedReq *pb.GetInitialSyncRequest
	delayFor    time.Duration
}

func (m *mockGetSyncCoreClient) GetInitialSync(ctx context.Context, req *pb.GetInitialSyncRequest) (*pb.GetInitialSyncResponse, error) {
	m.capturedReq = req

	if m.delayFor > 0 {
		select {
		case <-time.After(m.delayFor):
		case <-ctx.Done():
			return nil, status.Error(codes.Unavailable, "context deadline exceeded")
		}
	}

	return m.resp, m.err
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// buildAuthedSyncHandler wires JWTMiddleware → GetSyncHandler.
// Returns the http.Handler ready for httptest, the OIDC test server, and a
// makeToken closure that mints a valid signed JWT each time it is called.
// An optional timeout overrides the default 5s (pass 0 for default).
func buildAuthedSyncHandler(t *testing.T, mock *mockGetSyncCoreClient, opts ...time.Duration) (http.Handler, *httptest.Server, func() string) {
	t.Helper()

	oidcSrv, privateKey := setupOIDCServer(t)
	t.Cleanup(oidcSrv.Close)

	provider := auth.NewProvider(context.Background(), oidcSrv.URL)

	cfg := GetSyncConfig{
		CoreClient: mock,
		ServerName: "test.local",
	}
	if len(opts) > 0 && opts[0] > 0 {
		cfg.Timeout = opts[0]
	}
	handler := NewGetSyncHandler(cfg)

	authed := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil)(
		http.HandlerFunc(handler.GetSync),
	)

	makeToken := func() string {
		return signJWT(t, oidcSrv.URL, privateKey, time.Now().Add(time.Hour), nil)
	}

	return authed, oidcSrv, makeToken
}

// ─── Test 1: Happy path — initial sync with 2 rooms → 200 ───────────────────
//
// AC #1, #2, #3, #4 — authenticated GET /sync (no since param) calls
// GetInitialSync on Core and maps the response to a Matrix sync response.
//
// Given: valid JWT for @test-sub-123:test.local; mock Core returns
//        GetInitialSyncResponse with 2 room entries and a since_token
// When:  GET /_matrix/client/v3/sync (no ?since param)
// Then:  200; next_batch is non-empty; rooms.join contains both room IDs;
//        each joined room has state.events and timeline.events fields

func TestGetSync_InitialSync_HappyPath(t *testing.T) {
	stateContentBytes := []byte(`{"membership":"join","displayname":""}`)
	plContentBytes := []byte(`{"events_default":0,"users_default":0}`)
	msgContentBytes := []byte(`{"msgtype":"m.text","body":"hello"}`)

	mock := &mockGetSyncCoreClient{
		resp: &pb.GetInitialSyncResponse{
			SinceToken: "next_batch_token_abc123",
			Rooms: []*pb.SyncRoom{
				{
					RoomId: "!room1:test.local",
					StateEvents: []*pb.SyncRoomStateEvent{
						{
							Type:     "m.room.member",
							StateKey: "@alice:test.local",
							Content:  stateContentBytes,
							Sender:   "@alice:test.local",
						},
						{
							Type:     "m.room.power_levels",
							StateKey: "",
							Content:  plContentBytes,
							Sender:   "",
						},
					},
					TimelineEvents: []*pb.Event{
						{
							EventId:   "$ev1:test.local",
							RoomId:    "!room1:test.local",
							SenderId:  "@alice:test.local",
							EventType: "m.room.message",
							Content:   msgContentBytes,
							OriginTs:  1700000000001,
						},
						{
							EventId:   "$ev2:test.local",
							RoomId:    "!room1:test.local",
							SenderId:  "@alice:test.local",
							EventType: "m.room.message",
							Content:   msgContentBytes,
							OriginTs:  1700000000002,
						},
					},
					Limited:   false,
					PrevBatch: "",
				},
				{
					RoomId: "!room2:test.local",
					StateEvents: []*pb.SyncRoomStateEvent{
						{
							Type:     "m.room.member",
							StateKey: "@alice:test.local",
							Content:  stateContentBytes,
							Sender:   "@alice:test.local",
						},
					},
					TimelineEvents: []*pb.Event{},
					Limited:        false,
					PrevBatch:      "",
				},
			},
		},
	}

	handler, _, makeToken := buildAuthedSyncHandler(t, mock)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/sync", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}

	var resp struct {
		NextBatch string `json:"next_batch"`
		Rooms     struct {
			Join   map[string]json.RawMessage `json:"join"`
			Invite map[string]interface{}     `json:"invite"`
			Leave  map[string]interface{}     `json:"leave"`
		} `json:"rooms"`
		Presence struct {
			Events []interface{} `json:"events"`
		} `json:"presence"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}

	// next_batch must be the since_token returned by Core
	if resp.NextBatch == "" {
		t.Error("expected non-empty next_batch in sync response")
	}
	if resp.NextBatch != "next_batch_token_abc123" {
		t.Errorf("expected next_batch=next_batch_token_abc123, got %q", resp.NextBatch)
	}

	// rooms.join must contain both room IDs
	if len(resp.Rooms.Join) != 2 {
		t.Errorf("expected 2 rooms in rooms.join, got %d", len(resp.Rooms.Join))
	}
	for _, roomID := range []string{"!room1:test.local", "!room2:test.local"} {
		if _, ok := resp.Rooms.Join[roomID]; !ok {
			t.Errorf("expected rooms.join to contain %q", roomID)
		}
	}

	// Room 1: verify state.events and timeline.events are present
	var room1 struct {
		State struct {
			Events []map[string]interface{} `json:"events"`
		} `json:"state"`
		Timeline struct {
			Events    []map[string]interface{} `json:"events"`
			Limited   bool                     `json:"limited"`
			PrevBatch string                   `json:"prev_batch,omitempty"`
		} `json:"timeline"`
	}
	if err := json.Unmarshal(resp.Rooms.Join["!room1:test.local"], &room1); err != nil {
		t.Fatalf("failed to decode room1: %v", err)
	}
	if len(room1.State.Events) != 2 {
		t.Errorf("expected 2 state events in room1, got %d", len(room1.State.Events))
	}
	if len(room1.Timeline.Events) != 2 {
		t.Errorf("expected 2 timeline events in room1, got %d", len(room1.Timeline.Events))
	}

	// Assert origin_server_ts increases across timeline events (chronological order)
	if len(room1.Timeline.Events) >= 2 {
		ts0, _ := room1.Timeline.Events[0]["origin_server_ts"].(float64)
		ts1, _ := room1.Timeline.Events[1]["origin_server_ts"].(float64)
		if ts0 >= ts1 {
			t.Errorf("expected origin_server_ts to increase across timeline events; got %v then %v", ts0, ts1)
		}
	}

	// invite and leave must be present (even if empty)
	if resp.Rooms.Invite == nil {
		t.Error("expected rooms.invite to be present (not null)")
	}
	if resp.Rooms.Leave == nil {
		t.Error("expected rooms.leave to be present (not null)")
	}

	// presence.events must be present and empty
	if resp.Presence.Events == nil {
		t.Error("expected presence.events to be present (not null)")
	}

	// gRPC call must have been made with the correct user_id
	if mock.capturedReq == nil {
		t.Fatal("expected GetInitialSync to be called, but capturedReq is nil")
	}
	if mock.capturedReq.UserId == "" {
		t.Error("expected non-empty user_id in GetInitialSyncRequest")
	}
}

// ─── Test 2: Unauthenticated request → 401 M_MISSING_TOKEN ───────────────────
//
// AC #1 — JWTMiddleware must reject requests with no Authorization header
// before the handler is reached.
//
// Given: no Authorization header
// When:  GET /_matrix/client/v3/sync
// Then:  401 with errcode M_MISSING_TOKEN

func TestGetSync_Unauthenticated(t *testing.T) {
	mock := &mockGetSyncCoreClient{}
	handler, _, _ := buildAuthedSyncHandler(t, mock)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/sync", nil)
	// Deliberately omit Authorization header

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

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

	// Core must NOT have been called for an unauthenticated request
	if mock.capturedReq != nil {
		t.Error("expected GetInitialSync to NOT be called for unauthenticated request")
	}
}

// ─── Test 3: Incremental sync (?since=<token>) → 501 Not Implemented ─────────
//
// AC #7 — when ?since is present, Story 4-15 handles it.
// For Story 4-14, the handler must return 501 Not Implemented as a placeholder.
//
// Given: valid JWT; ?since=s123_456789 query parameter present
// When:  GET /_matrix/client/v3/sync?since=s123_456789
// Then:  501 (Story 4-15 placeholder; Core must NOT be called)

func TestGetSync_IncrementalSync_Stub(t *testing.T) {
	mock := &mockGetSyncCoreClient{
		resp: &pb.GetInitialSyncResponse{
			SinceToken: "should_not_be_reached",
			Rooms:      []*pb.SyncRoom{},
		},
	}
	handler, _, makeToken := buildAuthedSyncHandler(t, mock)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/sync?since=s123_456789", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501 for since-param request (Story 4-15 placeholder), got %d; body: %s",
			w.Code, w.Body.String())
	}

	// Core must NOT be called — incremental sync is not implemented in this story
	if mock.capturedReq != nil {
		t.Error("expected GetInitialSync to NOT be called when ?since is present (Story 4-15 deferred)")
	}
}

// ─── Test 4: Core unavailable → 503 M_UNAVAILABLE ────────────────────────────
//
// AC #6 — when Core returns gRPC UNAVAILABLE, the handler must return 503
// with errcode M_UNAVAILABLE.
//
// Given: valid JWT; mock Core returns status.Error(codes.Unavailable, ...)
// When:  GET /_matrix/client/v3/sync
// Then:  503 with errcode M_UNAVAILABLE

func TestGetSync_CoreUnavailable(t *testing.T) {
	mock := &mockGetSyncCoreClient{
		err: status.Error(codes.Unavailable, "core is down"),
	}
	handler, _, makeToken := buildAuthedSyncHandler(t, mock)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/sync", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

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

// ─── Test 5: User in no rooms → 200 with rooms.join == {} ────────────────────
//
// AC #5 — when Core returns an empty rooms list, the handler must return 200
// with rooms.join as an empty JSON object (not null).
//
// Given: valid JWT; mock Core returns GetInitialSyncResponse with empty Rooms slice
// When:  GET /_matrix/client/v3/sync
// Then:  200; next_batch is non-empty; rooms.join is {}

func TestGetSync_EmptyRooms(t *testing.T) {
	mock := &mockGetSyncCoreClient{
		resp: &pb.GetInitialSyncResponse{
			SinceToken: "empty_rooms_token",
			Rooms:      []*pb.SyncRoom{},
		},
	}
	handler, _, makeToken := buildAuthedSyncHandler(t, mock)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/sync", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		NextBatch string `json:"next_batch"`
		Rooms     struct {
			Join   map[string]interface{} `json:"join"`
			Invite map[string]interface{} `json:"invite"`
			Leave  map[string]interface{} `json:"leave"`
		} `json:"rooms"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}

	if resp.NextBatch == "" {
		t.Error("expected non-empty next_batch even when user has no rooms")
	}

	// rooms.join must be present and empty (not null)
	if resp.Rooms.Join == nil {
		t.Error("expected rooms.join to be {} (not null) when user has no rooms")
	}
	if len(resp.Rooms.Join) != 0 {
		t.Errorf("expected rooms.join to be empty, got %d entries", len(resp.Rooms.Join))
	}

	// rooms.invite and rooms.leave must be present (not null) even when empty
	if resp.Rooms.Invite == nil {
		t.Error("expected rooms.invite to be {} (not null) when user has no rooms")
	}
	if resp.Rooms.Leave == nil {
		t.Error("expected rooms.leave to be {} (not null) when user has no rooms")
	}
}

// ─── Test 6: Context timeout → 503 or 504 when Core delays > 5 s ─────────────
//
// AC #6 — the handler must apply a 5-second timeout to the gRPC call.
// When the mock delays longer than the timeout, the context is cancelled and
// the handler must return 503 or 504 (not hang indefinitely).
//
// Given: valid JWT; mock Core sleeps 10 s (longer than the 5 s handler timeout)
//        and returns gRPC UNAVAILABLE when the context is cancelled
// When:  GET /_matrix/client/v3/sync
// Then:  response status is 503 or 504 (Core unavailable / gateway timeout);
//        the test completes in < 8 s (does not hang for 10 s)

func TestGetSync_ContextTimeout(t *testing.T) {
	// The mock sleeps for 2 s but respects ctx.Done().
	// Handler timeout is set to 500 ms via GetSyncConfig.Timeout.
	mock := &mockGetSyncCoreClient{
		delayFor: 2 * time.Second,
		// resp/err will not be reached — ctx.Done() fires first
	}
	handler, _, makeToken := buildAuthedSyncHandler(t, mock, 500*time.Millisecond)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/sync", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	start := time.Now()
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	elapsed := time.Since(start)

	// Must not have hung for the full mock delay (2 s); should complete in < 1.5 s
	if elapsed > 1500*time.Millisecond {
		t.Errorf("handler took %v — did not apply 500 ms timeout (expected < 1.5 s)", elapsed)
	}

	// Response must indicate a server-side error (503 or 504)
	if w.Code != http.StatusServiceUnavailable && w.Code != http.StatusGatewayTimeout {
		t.Errorf("expected 503 or 504 on timeout, got %d; body: %s", w.Code, w.Body.String())
	}

	// The gRPC call must have been attempted (capturedReq set before sleep)
	if mock.capturedReq == nil {
		t.Error("expected GetInitialSync to be called (capturedReq should be set before the delay)")
	}
}
