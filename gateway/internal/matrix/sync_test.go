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
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nebu/nebu/internal/auth"
	"github.com/nebu/nebu/internal/buffer"
	pb "github.com/nebu/nebu/internal/grpc/pb"
	"github.com/nebu/nebu/internal/middleware"
	"github.com/prometheus/client_golang/prometheus"
	_ "github.com/jackc/pgx/v5/stdlib"
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

// GetSyncDelta stub — mockGetSyncCoreClient is only used for initial sync tests;
// incremental sync tests use mockGetSyncDeltaCoreClient instead.
func (m *mockGetSyncCoreClient) GetSyncDelta(_ context.Context, _ *pb.GetSyncDeltaRequest) (*pb.GetSyncDeltaResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not used in Story 4-14 tests")
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

	authed := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, "test.local")(
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

	// Capture raw body before decoding (json.NewDecoder consumes the buffer);
	// later assertions for Story 5-29e Bug 4 device-field check inspect the
	// raw JSON for null-vs-empty correctness.
	rawBody := w.Body.String()

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
	if err := json.NewDecoder(strings.NewReader(rawBody)).Decode(&resp); err != nil {
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

	// Story 5-29e Bug 4: every sync response must carry the three device fields
	// as empty (NOT null) values. Element Web's matrix-js-sdk treats missing
	// device_one_time_keys_count as 0, which triggers a keys/query polling loop.
	body := rawBody
	for _, want := range []string{
		`"device_one_time_keys_count":{}`,
		`"device_unused_fallback_key_types":[]`,
		`"device_lists":{`,
		`"changed":[]`,
		`"left":[]`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("Bug 4 regression: response missing %q\nbody: %s", want, body)
		}
	}
	for _, forbidden := range []string{
		`"device_one_time_keys_count":null`,
		`"device_unused_fallback_key_types":null`,
		`"device_lists":null`,
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("Bug 4 regression: response contains forbidden null %q (must be empty value)", forbidden)
		}
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

// ─── Story 4-15: Incremental sync tests ──────────────────────────────────────
//
// The tests below replace the Story 4-14 501-stub test for ?since.
// They are written FIRST (ATDD gate) — all must FAIL until Story 4-15 is
// implemented and `make proto` has regenerated pb/ with GetSyncDelta types.
//
// mockGetSyncDeltaCoreClient — consumer-defined interface for GetSyncDelta.
// Defined here alongside the tests (Go interface convention, ADR-009).
// The interface method GetSyncDelta references *pb.GetSyncDeltaRequest and
// *pb.GetSyncDeltaResponse — these types DO NOT EXIST YET in the generated pb/
// package. Every test in this file will therefore fail with a compilation error
// until `make proto` runs after the proto changes from Story 4-15 are applied.

type mockGetSyncDeltaCoreClient struct {
	// For GetInitialSync (initial sync path — unchanged from 4-14)
	initialResp        *pb.GetInitialSyncResponse
	initialErr         error
	capturedInitialReq *pb.GetInitialSyncRequest
	delayFor           time.Duration

	// For GetSyncDelta (incremental sync path — Story 4-15)
	deltaResp        *pb.GetSyncDeltaResponse
	deltaErr         error
	capturedDeltaReq *pb.GetSyncDeltaRequest
}

func (m *mockGetSyncDeltaCoreClient) GetInitialSync(ctx context.Context, req *pb.GetInitialSyncRequest) (*pb.GetInitialSyncResponse, error) {
	m.capturedInitialReq = req

	if m.delayFor > 0 {
		select {
		case <-time.After(m.delayFor):
		case <-ctx.Done():
			return nil, status.Error(codes.Unavailable, "context deadline exceeded")
		}
	}

	return m.initialResp, m.initialErr
}

func (m *mockGetSyncDeltaCoreClient) GetSyncDelta(ctx context.Context, req *pb.GetSyncDeltaRequest) (*pb.GetSyncDeltaResponse, error) {
	m.capturedDeltaReq = req
	return m.deltaResp, m.deltaErr
}

// buildAuthedSyncDeltaHandler is buildAuthedSyncHandler's counterpart for Story 4-15.
// It wires JWTMiddleware → GetSyncHandler using a mockGetSyncDeltaCoreClient so that
// both GetInitialSync and GetSyncDelta can be exercised from the same handler.
// An optional timeout overrides the default 5s (pass 0 for default).
func buildAuthedSyncDeltaHandler(t *testing.T, mock *mockGetSyncDeltaCoreClient, opts ...time.Duration) (http.Handler, *httptest.Server, func() string) {
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

	authed := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, "test.local")(
		http.HandlerFunc(handler.GetSync),
	)

	makeToken := func() string {
		return signJWT(t, oidcSrv.URL, privateKey, time.Now().Add(time.Hour), nil)
	}

	return authed, oidcSrv, makeToken
}

// ─── Story 4-15 Test 1: Incremental sync — happy path → 200 with delta room ──
//
// AC #1, #2, #3 (Story 4-15) — GET /sync?since=<token> calls GetSyncDelta on Core
// and maps the response (1 room with events) to a Matrix sync response.
//
// Given: valid JWT; ?since=s_token_abc; mock Core returns GetSyncDeltaResponse
//        with 1 room and a new since_token
// When:  GET /_matrix/client/v3/sync?since=s_token_abc
// Then:  200; next_batch is non-empty and != "s_token_abc";
//        rooms.join contains exactly 1 room with state.events and timeline.events

func TestGetSync_IncrementalSync_HappyPath(t *testing.T) {
	msgContentBytes := []byte(`{"msgtype":"m.text","body":"incremental hello"}`)
	stateContentBytes := []byte(`{"membership":"join","displayname":""}`)

	mock := &mockGetSyncDeltaCoreClient{
		deltaResp: &pb.GetSyncDeltaResponse{
			SinceToken:        "next_batch_delta_xyz",
			FallbackToInitial: false,
			Rooms: []*pb.SyncRoom{
				{
					RoomId: "!delta_room1:test.local",
					StateEvents: []*pb.SyncRoomStateEvent{
						{
							Type:     "m.room.member",
							StateKey: "@alice:test.local",
							Content:  stateContentBytes,
							Sender:   "@alice:test.local",
						},
					},
					TimelineEvents: []*pb.Event{
						{
							EventId:   "$delta_ev1:test.local",
							RoomId:    "!delta_room1:test.local",
							SenderId:  "@alice:test.local",
							EventType: "m.room.message",
							Content:   msgContentBytes,
							OriginTs:  1700000001000,
						},
					},
					Limited:   false,
					PrevBatch: "",
				},
			},
		},
	}

	handler, _, makeToken := buildAuthedSyncDeltaHandler(t, mock)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/sync?since=s_token_abc", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for incremental sync, got %d; body: %s", w.Code, w.Body.String())
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

	// next_batch must come from the delta response, not be a re-echo of the since token
	if resp.NextBatch == "" {
		t.Error("expected non-empty next_batch in incremental sync response")
	}
	if resp.NextBatch == "s_token_abc" {
		t.Errorf("next_batch must NOT reuse the incoming since token; got %q", resp.NextBatch)
	}
	if resp.NextBatch != "next_batch_delta_xyz" {
		t.Errorf("expected next_batch=next_batch_delta_xyz, got %q", resp.NextBatch)
	}

	// rooms.join must contain exactly the 1 room from the delta
	if len(resp.Rooms.Join) != 1 {
		t.Errorf("expected 1 room in rooms.join, got %d", len(resp.Rooms.Join))
	}
	if _, ok := resp.Rooms.Join["!delta_room1:test.local"]; !ok {
		t.Errorf("expected rooms.join to contain !delta_room1:test.local, got: %v", resp.Rooms.Join)
	}

	// rooms.invite and rooms.leave must be present (not null)
	if resp.Rooms.Invite == nil {
		t.Error("expected rooms.invite to be present (not null)")
	}
	if resp.Rooms.Leave == nil {
		t.Error("expected rooms.leave to be present (not null)")
	}

	// GetSyncDelta must have been called with the correct since_token
	if mock.capturedDeltaReq == nil {
		t.Fatal("expected GetSyncDelta to be called, but capturedDeltaReq is nil")
	}
	if mock.capturedDeltaReq.SinceToken != "s_token_abc" {
		t.Errorf("expected GetSyncDelta called with since_token=s_token_abc, got %q", mock.capturedDeltaReq.SinceToken)
	}

	// GetInitialSync must NOT have been called for incremental sync
	if mock.capturedInitialReq != nil {
		t.Error("expected GetInitialSync to NOT be called for incremental sync (?since present)")
	}
}

// ─── Story 4-15 Test 2: Incremental sync — empty delta → 200 with empty rooms ─
//
// AC #4 (Story 4-15) — when Core returns no rooms (long-poll timeout fired),
// the handler returns 200 with empty rooms.join and a valid next_batch.
//
// Given: valid JWT; ?since=s_token_abc&timeout=0; mock returns
//        GetSyncDeltaResponse with empty rooms and a new since_token
// When:  GET /_matrix/client/v3/sync?since=s_token_abc&timeout=0
// Then:  200; rooms.join is {}; next_batch is non-empty and != "s_token_abc"

func TestGetSync_IncrementalSync_EmptyDelta(t *testing.T) {
	mock := &mockGetSyncDeltaCoreClient{
		deltaResp: &pb.GetSyncDeltaResponse{
			SinceToken:        "next_batch_empty_delta",
			FallbackToInitial: false,
			Rooms:             []*pb.SyncRoom{},
		},
	}

	handler, _, makeToken := buildAuthedSyncDeltaHandler(t, mock)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/sync?since=s_token_abc&timeout=0", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for empty incremental sync, got %d; body: %s", w.Code, w.Body.String())
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

	// next_batch must be present and must be a NEW token
	if resp.NextBatch == "" {
		t.Error("expected non-empty next_batch even with empty delta")
	}
	if resp.NextBatch == "s_token_abc" {
		t.Errorf("next_batch must NOT reuse the incoming since token; got %q", resp.NextBatch)
	}

	// rooms.join must be present and empty (not null)
	if resp.Rooms.Join == nil {
		t.Error("expected rooms.join to be {} (not null) for empty delta")
	}
	if len(resp.Rooms.Join) != 0 {
		t.Errorf("expected empty rooms.join for empty delta, got %d entries", len(resp.Rooms.Join))
	}

	// rooms.invite and rooms.leave must be present (not null)
	if resp.Rooms.Invite == nil {
		t.Error("expected rooms.invite to be {} (not null)")
	}
	if resp.Rooms.Leave == nil {
		t.Error("expected rooms.leave to be {} (not null)")
	}

	// timeout=0 must be forwarded to gRPC
	if mock.capturedDeltaReq == nil {
		t.Fatal("expected GetSyncDelta to be called")
	}
	if mock.capturedDeltaReq.TimeoutMs != 0 {
		t.Errorf("expected timeout_ms=0 forwarded to Core, got %d", mock.capturedDeltaReq.TimeoutMs)
	}
}

// ─── Story 4-15 Test 3: Incremental sync — ?timeout forwarded to gRPC ─────────
//
// AC #2 (Story 4-15) — the timeout query parameter (in ms) is forwarded to
// Core via GetSyncDeltaRequest.timeout_ms.
//
// Given: valid JWT; ?since=s_token_abc&timeout=500; mock returns empty delta
// When:  GET /_matrix/client/v3/sync?since=s_token_abc&timeout=500
// Then:  200; capturedDeltaReq.timeout_ms == 500

func TestGetSync_IncrementalSync_Timeout(t *testing.T) {
	mock := &mockGetSyncDeltaCoreClient{
		deltaResp: &pb.GetSyncDeltaResponse{
			SinceToken: "next_batch_timeout_test",
			Rooms:      []*pb.SyncRoom{},
		},
	}

	handler, _, makeToken := buildAuthedSyncDeltaHandler(t, mock)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/sync?since=s_token_abc&timeout=500", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	if mock.capturedDeltaReq == nil {
		t.Fatal("expected GetSyncDelta to be called")
	}

	// timeout=500 must be forwarded verbatim (below the 30000 ms cap)
	if mock.capturedDeltaReq.TimeoutMs != 500 {
		t.Errorf("expected timeout_ms=500 forwarded to Core, got %d", mock.capturedDeltaReq.TimeoutMs)
	}
}

// ─── Story 4-15 Test 4: Incremental sync — ?timeout clamped to 30000 ──────────
//
// AC #7 (Story 4-15) — timeout values above 30 000 ms are clamped to 30 000 ms
// before being forwarded to Core.
//
// Given: valid JWT; ?since=s_token_abc&timeout=120000 (> 30 000 ms cap)
// When:  GET /_matrix/client/v3/sync?since=s_token_abc&timeout=120000
// Then:  capturedDeltaReq.timeout_ms == 30000 (clamped)

func TestGetSync_IncrementalSync_TimeoutClamped(t *testing.T) {
	mock := &mockGetSyncDeltaCoreClient{
		deltaResp: &pb.GetSyncDeltaResponse{
			SinceToken: "next_batch_clamped",
			Rooms:      []*pb.SyncRoom{},
		},
	}

	handler, _, makeToken := buildAuthedSyncDeltaHandler(t, mock)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/sync?since=s_token_abc&timeout=120000", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	if mock.capturedDeltaReq == nil {
		t.Fatal("expected GetSyncDelta to be called")
	}

	// timeout=120000 must be clamped to 30000 (AC #7)
	const maxTimeoutMs = int64(30000)
	if mock.capturedDeltaReq.TimeoutMs > maxTimeoutMs {
		t.Errorf("expected timeout_ms clamped to %d, got %d", maxTimeoutMs, mock.capturedDeltaReq.TimeoutMs)
	}
	if mock.capturedDeltaReq.TimeoutMs != maxTimeoutMs {
		t.Errorf("expected timeout_ms == %d after clamping, got %d", maxTimeoutMs, mock.capturedDeltaReq.TimeoutMs)
	}
}

// ─── Story 4-15 Test 5: Incremental sync — Core unavailable → 503 M_UNAVAILABLE ─
//
// AC #6 (Story 4-15) — when Core returns gRPC UNAVAILABLE for GetSyncDelta, the
// handler must return 503 with errcode M_UNAVAILABLE.
//
// Given: valid JWT; ?since=s_token_abc; mock Core returns
//        status.Error(codes.Unavailable, "core is down") for GetSyncDelta
// When:  GET /_matrix/client/v3/sync?since=s_token_abc
// Then:  503 with errcode M_UNAVAILABLE

func TestGetSync_IncrementalSync_CoreUnavailable(t *testing.T) {
	mock := &mockGetSyncDeltaCoreClient{
		deltaErr: status.Error(codes.Unavailable, "core is down"),
	}

	handler, _, makeToken := buildAuthedSyncDeltaHandler(t, mock)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/sync?since=s_token_abc", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 for incremental sync with Core unavailable, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixError
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.ErrCode != "M_UNAVAILABLE" {
		t.Errorf("expected errcode M_UNAVAILABLE, got %s", errResp.ErrCode)
	}

	// GetSyncDelta must have been called (capturedDeltaReq set)
	if mock.capturedDeltaReq == nil {
		t.Error("expected GetSyncDelta to be called, but capturedDeltaReq is nil")
	}

	// GetInitialSync must NOT have been called
	if mock.capturedInitialReq != nil {
		t.Error("expected GetInitialSync NOT to be called when Core returned UNAVAILABLE on delta")
	}
}

// ─── Story 4-15 Test 6: Incremental sync — FallbackToInitial=true → falls back ─
//
// AC #5 (Story 4-15) — when Core sets FallbackToInitial=true in GetSyncDeltaResponse,
// the handler must call GetInitialSync and return that response to the client.
//
// Given: valid JWT; ?since=unknown_token;
//        mock deltaResp has FallbackToInitial=true and empty rooms;
//        mock initialResp has 1 room (next_batch: "initial_token",
//          rooms: {"!room1:server": {...}})
// When:  GET /_matrix/client/v3/sync?since=unknown_token
// Then:  200; rooms.join contains the initial room (!room1:test.local);
//        next_batch == "initial_token" (from initial sync, not delta);
//        GetInitialSync was actually called (capturedInitialReq is set)

func TestGetSync_IncrementalSync_FallbackToInitial(t *testing.T) {
	stateContentBytes := []byte(`{"membership":"join","displayname":""}`)

	mock := &mockGetSyncDeltaCoreClient{
		// Delta response signals fallback — no rooms returned by delta
		deltaResp: &pb.GetSyncDeltaResponse{
			SinceToken:        "",
			FallbackToInitial: true,
			Rooms:             []*pb.SyncRoom{},
		},
		// Initial sync response that the handler must use after fallback
		initialResp: &pb.GetInitialSyncResponse{
			SinceToken: "initial_token",
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
					},
					TimelineEvents: []*pb.Event{},
					Limited:        false,
					PrevBatch:      "",
				},
			},
		},
	}

	handler, _, makeToken := buildAuthedSyncDeltaHandler(t, mock)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/sync?since=unknown_token", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 after FallbackToInitial, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		NextBatch string `json:"next_batch"`
		Rooms     struct {
			Join   map[string]json.RawMessage `json:"join"`
			Invite map[string]interface{}     `json:"invite"`
			Leave  map[string]interface{}     `json:"leave"`
		} `json:"rooms"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}

	// next_batch must come from the initial sync response
	if resp.NextBatch != "initial_token" {
		t.Errorf("expected next_batch=initial_token (from initial sync fallback), got %q", resp.NextBatch)
	}

	// rooms.join must contain the initial room
	if len(resp.Rooms.Join) != 1 {
		t.Errorf("expected 1 room in rooms.join after fallback, got %d", len(resp.Rooms.Join))
	}
	if _, ok := resp.Rooms.Join["!room1:test.local"]; !ok {
		t.Errorf("expected rooms.join to contain !room1:test.local after fallback, got: %v", resp.Rooms.Join)
	}

	// GetInitialSync must have been called (the fallback path was taken)
	if mock.capturedInitialReq == nil {
		t.Error("expected GetInitialSync to be called when FallbackToInitial=true, but capturedInitialReq is nil")
	}

	// GetSyncDelta must also have been called (it triggered the fallback)
	if mock.capturedDeltaReq == nil {
		t.Error("expected GetSyncDelta to be called before the fallback, but capturedDeltaReq is nil")
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

// ─── Story 4-16: Buffer tests ─────────────────────────────────────────────────
//
// These tests verify the buffer pre-check logic in handleIncrementalSync:
//   - BufferHit: pre-populated buffer events are returned immediately, Core skipped.
//   - BufferMiss: empty buffer causes fall-through to Core (GetSyncDelta called).
//   - NilBuffer: handler with Buffer=nil does not panic, falls through to Core.

// buildAuthedSyncBufferHandler wires JWTMiddleware → GetSyncHandler with an optional
// *buffer.MessageBuffer injected via GetSyncConfig.Buffer.
// Pass nil for buf to test the nil-buffer code path.
func buildAuthedSyncBufferHandler(t *testing.T, mock *mockGetSyncDeltaCoreClient, buf *buffer.MessageBuffer, opts ...time.Duration) (http.Handler, func() string) {
	t.Helper()

	oidcSrv, privateKey := setupOIDCServer(t)
	t.Cleanup(oidcSrv.Close)

	provider := auth.NewProvider(context.Background(), oidcSrv.URL)

	cfg := GetSyncConfig{
		CoreClient: mock,
		ServerName: "test.local",
		Buffer:     buf,
	}
	if len(opts) > 0 && opts[0] > 0 {
		cfg.Timeout = opts[0]
	}
	handler := NewGetSyncHandler(cfg)

	authed := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, "test.local")(
		http.HandlerFunc(handler.GetSync),
	)

	makeToken := func() string {
		return signJWT(t, oidcSrv.URL, privateKey, time.Now().Add(time.Hour), nil)
	}

	return authed, makeToken
}

// TestGetSync_IncrementalSync_BufferHit verifies that when the buffer already
// contains events for the requesting user, the handler returns them immediately
// without calling Core's GetSyncDelta.
//
// Given: buffer pre-populated with 1 event for @test-sub-123:test.local
// When:  GET /_matrix/client/v3/sync?since=s_token_abc
// Then:  200; rooms.join contains !room1:test.local; GetSyncDelta NOT called
func TestGetSync_IncrementalSync_BufferHit(t *testing.T) {
	// userID matches sub="test-sub-123" + serverName="test.local" (from signJWT / FormatUserID)
	const userID = "@test-sub-123:test.local"
	const roomID = "!room1:test.local"

	buf := buffer.NewMessageBuffer(10, prometheus.NewRegistry())
	buf.Put(userID, &pb.Event{
		EventId:   "$buf_ev1",
		RoomId:    roomID,
		SenderId:  userID,
		EventType: "m.room.message",
		Content:   []byte(`{"msgtype":"m.text","body":"buffered"}`),
		OriginTs:  1700000002000,
	})

	mock := &mockGetSyncDeltaCoreClient{
		// deltaResp is intentionally nil — GetSyncDelta must NOT be called
	}

	handler, makeToken := buildAuthedSyncBufferHandler(t, mock, buf)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/sync?since=s_token_abc", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for buffer hit, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		NextBatch string `json:"next_batch"`
		Rooms     struct {
			Join map[string]json.RawMessage `json:"join"`
		} `json:"rooms"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}

	// rooms.join must contain the room from the buffered event
	if _, ok := resp.Rooms.Join[roomID]; !ok {
		t.Errorf("expected rooms.join to contain %q from buffer, got: %v", roomID, resp.Rooms.Join)
	}

	// Core's GetSyncDelta must NOT have been called — buffer hit short-circuits
	if mock.capturedDeltaReq != nil {
		t.Error("expected GetSyncDelta NOT to be called on buffer hit, but capturedDeltaReq is set")
	}
}

// TestGetSync_IncrementalSync_BufferMiss verifies that when the buffer is empty,
// the handler falls through to Core and calls GetSyncDelta.
//
// Given: empty buffer; mock Core returns empty delta
// When:  GET /_matrix/client/v3/sync?since=s_token_abc
// Then:  200; GetSyncDelta WAS called (capturedDeltaReq != nil)
func TestGetSync_IncrementalSync_BufferMiss(t *testing.T) {
	buf := buffer.NewMessageBuffer(10, prometheus.NewRegistry())
	// no events put — buffer is empty

	mock := &mockGetSyncDeltaCoreClient{
		deltaResp: &pb.GetSyncDeltaResponse{
			SinceToken: "next_batch_miss",
			Rooms:      []*pb.SyncRoom{},
		},
	}

	// Use a very short timeout so WaitFor's bufCtx expires quickly and we don't stall
	handler, makeToken := buildAuthedSyncBufferHandler(t, mock, buf, 5*time.Second)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/sync?since=s_token_abc", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for buffer miss, got %d; body: %s", w.Code, w.Body.String())
	}

	// Core's GetSyncDelta must have been called — buffer miss falls through
	if mock.capturedDeltaReq == nil {
		t.Error("expected GetSyncDelta to be called on buffer miss, but capturedDeltaReq is nil")
	}
}

// TestGetSync_IncrementalSync_NilBuffer verifies that a handler configured with
// Buffer=nil does not panic and falls through to Core normally.
//
// Given: GetSyncConfig.Buffer = nil; mock Core returns empty delta
// When:  GET /_matrix/client/v3/sync?since=s_token_abc
// Then:  no panic; 200; GetSyncDelta WAS called
func TestGetSync_IncrementalSync_NilBuffer(t *testing.T) {
	mock := &mockGetSyncDeltaCoreClient{
		deltaResp: &pb.GetSyncDeltaResponse{
			SinceToken: "next_batch_nil_buf",
			Rooms:      []*pb.SyncRoom{},
		},
	}

	// Pass nil explicitly for the buffer
	handler, makeToken := buildAuthedSyncBufferHandler(t, mock, nil)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/sync?since=s_token_abc", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with nil buffer, got %d; body: %s", w.Code, w.Body.String())
	}

	// Core's GetSyncDelta must have been called — nil buffer skips buffer logic
	if mock.capturedDeltaReq == nil {
		t.Error("expected GetSyncDelta to be called with nil buffer, but capturedDeltaReq is nil")
	}
}

// ─── Fix-1: buildLeaveRooms — m.room.member leave event in state.events ──────
//
// These tests are written FIRST (ATDD gate) for Fix Story 1.
// ALL tests in this section are expected to FAIL until the fix is applied to
// buildLeaveRooms() in sync.go.
//
// Test strategy:
//   - Tests require NEBU_TEST_DB_URL env var (Postgres DSN). Skipped if absent
//     so `make test-unit-go` (no DB) does not fail the build pipeline.
//   - To observe failures before the fix: set NEBU_TEST_DB_URL and run:
//     go test -run TestBuildLeaveRooms ./internal/matrix/
//   - A minimal schema (users + rooms + room_members + events) is created inline
//     for each test; all rows are cleaned up via t.Cleanup.
//   - The JSONB content column may store objects directly ('object') or as a
//     double-encoded string. The tests use the object form (most common in prod).
//
// AC coverage:
//   AC #1 + AC #2 → TestBuildLeaveRooms_ReturnsLeaveEventInStateEvents
//   AC #3         → TestBuildLeaveRooms_GracefulDegradation_NoLeaveEvent
//   AC #4         → TestBuildLeaveRooms_RejectedInvite_IncludesLeaveEventIfPresent
//   AC #5         → see e2e/tests/features/room/room-lifecycle.spec.ts (existing, documented below)

// openTestDB opens a *sql.DB connection using NEBU_TEST_DB_URL.
// Skips the calling test if the env var is not set.
// Creates the minimal schema required by buildLeaveRooms tests.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := os.Getenv("NEBU_TEST_DB_URL")
	if dsn == "" {
		t.Skip("NEBU_TEST_DB_URL not set — skipping DB-dependent test (set to a Postgres DSN to run)")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("openTestDB: sql.Open: %v", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		t.Fatalf("openTestDB: ping failed: %v — is Postgres running at %s?", err, dsn)
	}
	t.Cleanup(func() { _ = db.Close() })

	// Create the minimal schema needed for buildLeaveRooms tests.
	// IF NOT EXISTS guards allow multiple tests to run in the same DB session.
	schema := `
		CREATE TABLE IF NOT EXISTS users (
			user_id      TEXT    PRIMARY KEY,
			system_role  TEXT    NOT NULL DEFAULT 'user',
			is_active    BOOLEAN NOT NULL DEFAULT true,
			created_at   BIGINT  NOT NULL
		);
		CREATE TABLE IF NOT EXISTS rooms (
			room_id     TEXT    PRIMARY KEY,
			name        TEXT,
			visibility  TEXT    NOT NULL DEFAULT 'private',
			created_at  BIGINT  NOT NULL
		);
		CREATE TABLE IF NOT EXISTS room_members (
			room_id    TEXT    NOT NULL REFERENCES rooms(room_id),
			user_id    TEXT    NOT NULL REFERENCES users(user_id),
			joined_at  BIGINT  NOT NULL,
			left_at    BIGINT,
			PRIMARY KEY (room_id, user_id)
		);
		CREATE TABLE IF NOT EXISTS events (
			event_id         TEXT    PRIMARY KEY,
			room_id          TEXT    NOT NULL REFERENCES rooms(room_id),
			sender           TEXT    NOT NULL,
			event_type       TEXT    NOT NULL,
			content          JSONB   NOT NULL,
			origin_server_ts BIGINT  NOT NULL,
			signatures       JSONB
		);
		CREATE TABLE IF NOT EXISTS room_invitations (
			room_id      TEXT    NOT NULL REFERENCES rooms(room_id),
			inviter_id   TEXT    NOT NULL,
			invitee_id   TEXT    NOT NULL,
			invited_at   BIGINT  NOT NULL,
			accepted_at  BIGINT,
			rejected_at  BIGINT,
			PRIMARY KEY (room_id, invitee_id)
		);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("openTestDB: schema creation failed: %v", err)
	}

	return db
}

// insertTestLeaveFixture inserts a minimal leave fixture:
//   - a user row
//   - a room row
//   - a room_members row with left_at set (user has left)
//   - an events row for m.room.member with membership=leave (if withEvent=true)
//
// Returns a cleanup function that removes all inserted rows.
func insertTestLeaveFixture(t *testing.T, db *sql.DB, userID, roomID, eventID string, withEvent bool) func() {
	t.Helper()

	now := int64(1700000000000)

	// Insert user
	_, err := db.Exec(`INSERT INTO users (user_id, system_role, is_active, created_at) VALUES ($1, 'user', true, $2) ON CONFLICT DO NOTHING`, userID, now)
	if err != nil {
		t.Fatalf("insertTestLeaveFixture: insert user: %v", err)
	}

	// Insert room
	_, err = db.Exec(`INSERT INTO rooms (room_id, visibility, created_at) VALUES ($1, 'private', $2) ON CONFLICT DO NOTHING`, roomID, now)
	if err != nil {
		t.Fatalf("insertTestLeaveFixture: insert room: %v", err)
	}

	// Insert room_members with left_at set
	_, err = db.Exec(
		`INSERT INTO room_members (room_id, user_id, joined_at, left_at) VALUES ($1, $2, $3, $4) ON CONFLICT (room_id, user_id) DO UPDATE SET left_at = EXCLUDED.left_at`,
		roomID, userID, now-1000, now,
	)
	if err != nil {
		t.Fatalf("insertTestLeaveFixture: insert room_members: %v", err)
	}

	if withEvent {
		// Insert m.room.member leave event.
		// Content is stored as a JSONB object (object form, not double-encoded string).
		leaveContent := fmt.Sprintf(`{"membership":"leave","displayname":"%s"}`, userID)
		_, err = db.Exec(
			`INSERT INTO events (event_id, room_id, sender, event_type, content, origin_server_ts) VALUES ($1, $2, $3, 'm.room.member', $4::jsonb, $5) ON CONFLICT DO NOTHING`,
			eventID, roomID, userID, leaveContent, now,
		)
		if err != nil {
			t.Fatalf("insertTestLeaveFixture: insert event: %v", err)
		}
	}

	return func() {
		if withEvent {
			_, _ = db.Exec(`DELETE FROM events WHERE event_id = $1`, eventID)
		}
		_, _ = db.Exec(`DELETE FROM room_members WHERE room_id = $1 AND user_id = $2`, roomID, userID)
		_, _ = db.Exec(`DELETE FROM rooms WHERE room_id = $1`, roomID)
		_, _ = db.Exec(`DELETE FROM users WHERE user_id = $1`, userID)
	}
}

// ─── Fix-1 Test 1 (AC #1, AC #2): buildLeaveRooms returns leave event in state.events ─────
//
// Given: a room in room_members with left_at IS NOT NULL, and an events row for
//        m.room.member with {"membership":"leave"} and sender == userID
// When:  buildLeaveRooms(ctx, userID) is called
// Then:  the returned map has an entry for the room; state.events contains exactly
//        1 event with type="m.room.member", state_key=userID, content.membership="leave"
//
// This test MUST FAIL until buildLeaveRooms queries the events table and includes
// the leave event in the state.events slice.

func TestBuildLeaveRooms_ReturnsLeaveEventInStateEvents(t *testing.T) {
	db := openTestDB(t)

	userID := "@leavetest-user:test.local"
	roomID := "!leavetest-room:test.local"
	eventID := "$leavetest-ev1:test.local"

	cleanup := insertTestLeaveFixture(t, db, userID, roomID, eventID, true /* withEvent */)
	t.Cleanup(cleanup)

	h := &GetSyncHandler{db: db}

	leaves := h.buildLeaveRooms(context.Background(), userID)

	// AC #1: rooms.leave must contain an entry for the left room
	roomEntry, ok := leaves[roomID]
	if !ok {
		t.Fatalf("buildLeaveRooms: expected entry for room %q, got none; result: %v", roomID, leaves)
	}

	roomMap, ok := roomEntry.(map[string]interface{})
	if !ok {
		t.Fatalf("buildLeaveRooms: room entry is not map[string]interface{}, got %T", roomEntry)
	}

	stateRaw, ok := roomMap["state"]
	if !ok {
		t.Fatal("buildLeaveRooms: room entry missing 'state' key")
	}

	stateMap, ok := stateRaw.(map[string]interface{})
	if !ok {
		t.Fatalf("buildLeaveRooms: 'state' is not map[string]interface{}, got %T", stateRaw)
	}

	eventsRaw, ok := stateMap["events"]
	if !ok {
		t.Fatal("buildLeaveRooms: state missing 'events' key")
	}

	// AC #1: state.events must contain exactly 1 m.room.member leave event.
	// The current implementation (before the fix) always returns []interface{}{} here,
	// so this assertion will FAIL with the unfixed code.
	events, ok := eventsRaw.([]map[string]interface{})
	if !ok {
		// Also handle []interface{} (the current hardcoded empty slice)
		eventsSlice, ok2 := eventsRaw.([]interface{})
		if !ok2 {
			t.Fatalf("buildLeaveRooms: state.events is not a slice, got %T", eventsRaw)
		}
		if len(eventsSlice) == 0 {
			t.Fatalf("buildLeaveRooms: state.events is EMPTY — fix required: buildLeaveRooms must query events table for m.room.member leave event (AC #1)")
		}
		// Convert to []map[string]interface{} for further assertions
		events = make([]map[string]interface{}, 0, len(eventsSlice))
		for _, e := range eventsSlice {
			em, ok := e.(map[string]interface{})
			if !ok {
				t.Fatalf("buildLeaveRooms: state.events[i] is not map[string]interface{}, got %T", e)
			}
			events = append(events, em)
		}
	}

	if len(events) != 1 {
		t.Fatalf("buildLeaveRooms: expected 1 event in state.events, got %d (AC #1)", len(events))
	}

	ev := events[0]

	if ev["type"] != "m.room.member" {
		t.Errorf("buildLeaveRooms: state.events[0].type must be 'm.room.member', got %q", ev["type"])
	}

	if ev["state_key"] != userID {
		t.Errorf("buildLeaveRooms: state.events[0].state_key must be %q (userID), got %q", userID, ev["state_key"])
	}

	if ev["sender"] != userID {
		t.Errorf("buildLeaveRooms: state.events[0].sender must be %q (userID), got %q", userID, ev["sender"])
	}

	// Assert content contains membership=leave.
	// Content may be json.RawMessage or map[string]interface{} depending on implementation.
	switch c := ev["content"].(type) {
	case map[string]interface{}:
		if c["membership"] != "leave" {
			t.Errorf("buildLeaveRooms: state.events[0].content.membership must be 'leave', got %q", c["membership"])
		}
	case json.RawMessage:
		var cm map[string]interface{}
		if err := json.Unmarshal(c, &cm); err != nil {
			t.Fatalf("buildLeaveRooms: cannot unmarshal content: %v", err)
		}
		if cm["membership"] != "leave" {
			t.Errorf("buildLeaveRooms: state.events[0].content.membership must be 'leave', got %q", cm["membership"])
		}
	default:
		t.Fatalf("buildLeaveRooms: state.events[0].content is unexpected type %T", ev["content"])
	}
}

// ─── Fix-1 Test 2 (AC #3): graceful degradation — no leave event in DB ───────
//
// Given: a room in room_members with left_at IS NOT NULL, but NO events row for
//        m.room.member (room created before this fix, or event was not persisted)
// When:  buildLeaveRooms(ctx, userID) is called
// Then:  the room entry is still present in the returned map; state.events is an
//        empty slice (not nil, not a panic); no error is returned to the caller
//
// This test MUST PASS before AND after the fix (it validates graceful degradation).
// It will FAIL if the implementation crashes or panics on missing events.

func TestBuildLeaveRooms_GracefulDegradation_NoLeaveEvent(t *testing.T) {
	db := openTestDB(t)

	userID := "@graceful-user:test.local"
	roomID := "!graceful-room:test.local"

	cleanup := insertTestLeaveFixture(t, db, userID, roomID, "" /* no eventID */, false /* withEvent=false */)
	t.Cleanup(cleanup)

	h := &GetSyncHandler{db: db}

	// Must not panic
	leaves := h.buildLeaveRooms(context.Background(), userID)

	// The room entry must be present
	roomEntry, ok := leaves[roomID]
	if !ok {
		t.Fatalf("buildLeaveRooms: expected entry for room %q even when no leave event in DB, got none", roomID)
	}

	roomMap, ok := roomEntry.(map[string]interface{})
	if !ok {
		t.Fatalf("buildLeaveRooms: room entry is not map[string]interface{}, got %T", roomEntry)
	}

	stateRaw, ok := roomMap["state"]
	if !ok {
		t.Fatal("buildLeaveRooms: room entry missing 'state' key")
	}

	stateMap, ok := stateRaw.(map[string]interface{})
	if !ok {
		t.Fatalf("buildLeaveRooms: 'state' is not map[string]interface{}, got %T", stateRaw)
	}

	eventsRaw, ok := stateMap["events"]
	if !ok {
		t.Fatal("buildLeaveRooms: state missing 'events' key (must degrade gracefully, not remove state key)")
	}

	// state.events must be an empty slice (not nil, not a panic).
	// Accept both []interface{} and []map[string]interface{}.
	switch e := eventsRaw.(type) {
	case []interface{}:
		if len(e) != 0 {
			t.Errorf("buildLeaveRooms: graceful degradation: state.events must be empty when no leave event in DB, got %d entries", len(e))
		}
	case []map[string]interface{}:
		if len(e) != 0 {
			t.Errorf("buildLeaveRooms: graceful degradation: state.events must be empty when no leave event in DB, got %d entries", len(e))
		}
	case nil:
		t.Error("buildLeaveRooms: graceful degradation: state.events must not be nil")
	default:
		t.Errorf("buildLeaveRooms: graceful degradation: state.events is unexpected type %T", eventsRaw)
	}
}

// ─── Fix-1 Test 3 (AC #4): rejected invite — includes leave event if one exists ─
//
// Given: a room_invitations row with rejected_at IS NOT NULL (invite declined),
//        AND an events row for m.room.member with membership=leave for that user
// When:  buildLeaveRooms(ctx, userID) is called
// Then:  the room entry appears in rooms.leave; state.events contains the leave event
//
// This test MUST FAIL until the rejected_at branch of buildLeaveRooms also queries
// the events table (AC #4).
//
// Note: This test inserts into room_invitations but NOT into room_members (the user
// rejected the invite without ever joining). The room must still appear in rooms.leave
// with the leave event if one is present in the events table.

func TestBuildLeaveRooms_RejectedInvite_IncludesLeaveEventIfPresent(t *testing.T) {
	db := openTestDB(t)

	userID := "@rejected-user:test.local"
	inviterID := "@inviter-user:test.local"
	roomID := "!rejected-room:test.local"
	eventID := "$rejected-ev1:test.local"

	now := int64(1700000001000)

	// Insert users
	for _, uid := range []string{userID, inviterID} {
		_, err := db.Exec(`INSERT INTO users (user_id, system_role, is_active, created_at) VALUES ($1, 'user', true, $2) ON CONFLICT DO NOTHING`, uid, now)
		if err != nil {
			t.Fatalf("insertTestRejectedInviteFixture: insert user %s: %v", uid, err)
		}
	}

	// Insert room
	_, err := db.Exec(`INSERT INTO rooms (room_id, visibility, created_at) VALUES ($1, 'private', $2) ON CONFLICT DO NOTHING`, roomID, now)
	if err != nil {
		t.Fatalf("insertTestRejectedInviteFixture: insert room: %v", err)
	}

	// Insert room_invitations with rejected_at set.
	// Composite PK is (room_id, invitee_id) — no invitation_id column.
	// No room_members row: user received and rejected the invite without joining.
	_, err = db.Exec(
		`INSERT INTO room_invitations (room_id, inviter_id, invitee_id, invited_at, rejected_at) VALUES ($1, $2, $3, $4, $5) ON CONFLICT (room_id, invitee_id) DO UPDATE SET rejected_at = EXCLUDED.rejected_at`,
		roomID, inviterID, userID, now-1000, now,
	)
	if err != nil {
		t.Fatalf("insertTestRejectedInviteFixture: insert invitation: %v", err)
	}

	// Insert m.room.member leave event (as if Core emitted it on rejection)
	leaveContent := `{"membership":"leave"}`
	_, err = db.Exec(
		`INSERT INTO events (event_id, room_id, sender, event_type, content, origin_server_ts) VALUES ($1, $2, $3, 'm.room.member', $4::jsonb, $5) ON CONFLICT DO NOTHING`,
		eventID, roomID, userID, leaveContent, now,
	)
	if err != nil {
		t.Fatalf("insertTestRejectedInviteFixture: insert event: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM events WHERE event_id = $1`, eventID)
		_, _ = db.Exec(`DELETE FROM room_invitations WHERE room_id = $1 AND invitee_id = $2`, roomID, userID)
		_, _ = db.Exec(`DELETE FROM rooms WHERE room_id = $1`, roomID)
		for _, uid := range []string{userID, inviterID} {
			_, _ = db.Exec(`DELETE FROM users WHERE user_id = $1`, uid)
		}
	})

	h := &GetSyncHandler{db: db}

	leaves := h.buildLeaveRooms(context.Background(), userID)

	// Room must appear in rooms.leave
	roomEntry, ok := leaves[roomID]
	if !ok {
		t.Fatalf("buildLeaveRooms (rejected invite): expected entry for room %q, got none", roomID)
	}

	roomMap, ok := roomEntry.(map[string]interface{})
	if !ok {
		t.Fatalf("buildLeaveRooms (rejected invite): room entry is not map[string]interface{}, got %T", roomEntry)
	}

	stateRaw := roomMap["state"]
	stateMap, ok := stateRaw.(map[string]interface{})
	if !ok {
		t.Fatalf("buildLeaveRooms (rejected invite): 'state' is not map[string]interface{}, got %T", stateRaw)
	}

	eventsRaw := stateMap["events"]

	// AC #4: state.events must contain the leave event.
	// Current implementation (before the fix) always returns empty [] here — this test FAILS.
	eventsSlice, ok := eventsRaw.([]interface{})
	if ok && len(eventsSlice) == 0 {
		t.Fatalf("buildLeaveRooms (rejected invite): state.events is EMPTY — fix required: rejected_at branch must also query events table for m.room.member leave event (AC #4)")
	}

	eventsMap, ok := eventsRaw.([]map[string]interface{})
	if ok && len(eventsMap) == 0 {
		t.Fatalf("buildLeaveRooms (rejected invite): state.events is EMPTY — fix required: rejected_at branch must also query events table for m.room.member leave event (AC #4)")
	}

	if eventsRaw == nil {
		t.Fatal("buildLeaveRooms (rejected invite): state.events is nil — must be a slice")
	}
}

// ─── Test: unsigned.age in timeline events (spec §8.4.3) ─────────────────────
//
// Story 9-10b AC3: every syncTimelineEvent in the sync response MUST carry
// unsigned.age > 0.  matrix-js-sdk uses this field for event deduplication and
// lag detection; missing or zero unsigned.age causes sporadic re-polling of
// already-seen events during DM creation.
//
// Given: Core returns one room with two timeline events (OriginTs in the past)
// When:  GET /_matrix/client/v3/sync (initial sync, no ?since param)
// Then:  200; every timeline event in rooms.join has unsigned.age > 0

func TestGetSync_TimelineEvents_HavePositiveUnsignedAge(t *testing.T) {
	// Use a timestamp clearly in the past so age is guaranteed > 0.
	pastTs := time.Now().Add(-5 * time.Second).UnixMilli()

	mock := &mockGetSyncCoreClient{
		resp: &pb.GetInitialSyncResponse{
			SinceToken: "age_test_token",
			Rooms: []*pb.SyncRoom{
				{
					RoomId: "!ageroom:test.local",
					StateEvents: []*pb.SyncRoomStateEvent{},
					TimelineEvents: []*pb.Event{
						{
							EventId:   "$age1:test.local",
							RoomId:    "!ageroom:test.local",
							SenderId:  "@alice:test.local",
							EventType: "m.room.message",
							Content:   []byte(`{"msgtype":"m.text","body":"hello"}`),
							OriginTs:  pastTs,
						},
						{
							EventId:   "$age2:test.local",
							RoomId:    "!ageroom:test.local",
							SenderId:  "@alice:test.local",
							EventType: "m.room.message",
							Content:   []byte(`{"msgtype":"m.text","body":"world"}`),
							OriginTs:  pastTs + 1000,
						},
					},
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

	// Parse response to inspect unsigned.age on timeline events.
	var resp struct {
		Rooms struct {
			Join map[string]struct {
				Timeline struct {
					Events []struct {
						EventID  string `json:"event_id"`
						Unsigned struct {
							Age int64 `json:"age"`
						} `json:"unsigned"`
					} `json:"events"`
				} `json:"timeline"`
			} `json:"join"`
		} `json:"rooms"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode sync response: %v", err)
	}

	room, ok := resp.Rooms.Join["!ageroom:test.local"]
	if !ok {
		t.Fatal("expected !ageroom:test.local in rooms.join")
	}
	if len(room.Timeline.Events) != 2 {
		t.Fatalf("expected 2 timeline events, got %d", len(room.Timeline.Events))
	}
	for _, ev := range room.Timeline.Events {
		if ev.Unsigned.Age <= 0 {
			t.Errorf("expected unsigned.age > 0 for event %q, got %d", ev.EventID, ev.Unsigned.Age)
		}
	}
}
