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
		CREATE TABLE IF NOT EXISTS forgotten_rooms (
			user_id         TEXT    NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
			room_id         TEXT    NOT NULL,
			forgotten_at_ms BIGINT  NOT NULL DEFAULT (EXTRACT(EPOCH FROM now()) * 1000)::BIGINT,
			PRIMARY KEY (user_id, room_id)
		);
		CREATE TABLE IF NOT EXISTS sync_tokens (
			user_id    TEXT    PRIMARY KEY REFERENCES users(user_id) ON DELETE CASCADE,
			token      TEXT    NOT NULL,
			updated_at BIGINT  NOT NULL
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

	leaves := h.buildLeaveRooms(context.Background(), userID, 0)

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
	leaves := h.buildLeaveRooms(context.Background(), userID, 0)

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

	leaves := h.buildLeaveRooms(context.Background(), userID, 0)

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

// ─── Story 9-19 Fix 2 (GAP-LEAVE-ONCE): buildLeaveRooms sinceMs filter ────────
//
// AC: buildLeaveRooms with sinceMs set to a time AFTER the leave event should NOT
// return the room (i.e. the room already left before this sync cycle).
//
// Given: a room_members row with left_at = T (Unix-ms)
// When:  buildLeaveRooms(ctx, userID, sinceMs = T+1) is called
// Then:  the room is NOT in the result (left_at ≤ sinceMs → filtered out)

func TestGetSyncHandler_BuildLeaveRooms_SinceFilter(t *testing.T) {
	db := openTestDB(t)

	userID := "@sincefilter-user:test.local"
	roomID := "!sincefilter-room:test.local"
	leftAt := int64(1700000005000)

	now := int64(1700000000000)

	// Insert user
	_, err := db.Exec(`INSERT INTO users (user_id, system_role, is_active, created_at) VALUES ($1, 'user', true, $2) ON CONFLICT DO NOTHING`, userID, now)
	if err != nil {
		t.Fatalf("sinceFilter: insert user: %v", err)
	}

	// Insert room
	_, err = db.Exec(`INSERT INTO rooms (room_id, visibility, created_at) VALUES ($1, 'private', $2) ON CONFLICT DO NOTHING`, roomID, now)
	if err != nil {
		t.Fatalf("sinceFilter: insert room: %v", err)
	}

	// Insert room_members with left_at = leftAt
	_, err = db.Exec(
		`INSERT INTO room_members (room_id, user_id, joined_at, left_at) VALUES ($1, $2, $3, $4) ON CONFLICT (room_id, user_id) DO UPDATE SET left_at = EXCLUDED.left_at`,
		roomID, userID, now, leftAt,
	)
	if err != nil {
		t.Fatalf("sinceFilter: insert room_members: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM room_members WHERE room_id = $1 AND user_id = $2`, roomID, userID)
		_, _ = db.Exec(`DELETE FROM rooms WHERE room_id = $1`, roomID)
		_, _ = db.Exec(`DELETE FROM users WHERE user_id = $1`, userID)
	})

	h := &GetSyncHandler{db: db}

	// sinceMs = leftAt + 1: the leave event happened BEFORE this sync window.
	leaves := h.buildLeaveRooms(context.Background(), userID, leftAt+1)

	if _, found := leaves[roomID]; found {
		t.Errorf("GAP-LEAVE-ONCE: room %q must NOT appear in rooms.leave when left_at (%d) <= sinceMs (%d)", roomID, leftAt, leftAt+1)
	}
}

// ─── Story 9-19 Fix 2 (GAP-LEAVE-ONCE): sinceMs=0 → backward-compat ──────────
//
// AC: buildLeaveRooms with sinceMs=0 must return ALL left rooms (no filter).
// This is the initial sync case.
//
// Given: a room_members row with left_at = T
// When:  buildLeaveRooms(ctx, userID, 0) is called
// Then:  the room IS in the result

func TestGetSyncHandler_BuildLeaveRooms_ZeroSinceMs(t *testing.T) {
	db := openTestDB(t)

	userID := "@zerosince-user:test.local"
	roomID := "!zerosince-room:test.local"

	now := int64(1700000010000)

	// Insert user
	_, err := db.Exec(`INSERT INTO users (user_id, system_role, is_active, created_at) VALUES ($1, 'user', true, $2) ON CONFLICT DO NOTHING`, userID, now)
	if err != nil {
		t.Fatalf("zeroSinceMs: insert user: %v", err)
	}

	// Insert room
	_, err = db.Exec(`INSERT INTO rooms (room_id, visibility, created_at) VALUES ($1, 'private', $2) ON CONFLICT DO NOTHING`, roomID, now)
	if err != nil {
		t.Fatalf("zeroSinceMs: insert room: %v", err)
	}

	// Insert room_members with left_at set
	_, err = db.Exec(
		`INSERT INTO room_members (room_id, user_id, joined_at, left_at) VALUES ($1, $2, $3, $4) ON CONFLICT (room_id, user_id) DO UPDATE SET left_at = EXCLUDED.left_at`,
		roomID, userID, now-1000, now,
	)
	if err != nil {
		t.Fatalf("zeroSinceMs: insert room_members: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM room_members WHERE room_id = $1 AND user_id = $2`, roomID, userID)
		_, _ = db.Exec(`DELETE FROM rooms WHERE room_id = $1`, roomID)
		_, _ = db.Exec(`DELETE FROM users WHERE user_id = $1`, userID)
	})

	h := &GetSyncHandler{db: db}

	// sinceMs = 0 → no filter → room must appear
	leaves := h.buildLeaveRooms(context.Background(), userID, 0)

	if _, found := leaves[roomID]; !found {
		t.Errorf("GAP-LEAVE-ONCE backward-compat: room %q MUST appear in rooms.leave when sinceMs=0", roomID)
	}
}

// ─── Story 9-19 Fix 3 (GAP-FORGET): forgotten rooms excluded from rooms.leave ──
//
// AC: buildLeaveRooms must exclude rooms that the user has forgotten (present in
// forgotten_rooms table), even when sinceMs=0.
//
// Given: a room_members row with left_at set AND a forgotten_rooms row for the same room
// When:  buildLeaveRooms(ctx, userID, 0) is called
// Then:  the room is NOT in the result

// TestBuildInviteRooms_ForgottenExcluded verifies that a pending invitation for a
// forgotten room is excluded from rooms.invite (GAP-FORGET AC3: no join/leave/invite).
func TestBuildInviteRooms_ForgottenExcluded(t *testing.T) {
	db := openTestDB(t)

	userID := "@invite-forgotten-user:test.local"
	inviterID := "@invite-forgotten-inviter:test.local"
	roomID := "!invite-forgotten-room:test.local"

	now := int64(1700000030000)

	// Insert users
	for _, uid := range []string{userID, inviterID} {
		_, err := db.Exec(`INSERT INTO users (user_id, system_role, is_active, created_at) VALUES ($1, 'user', true, $2) ON CONFLICT DO NOTHING`, uid, now)
		if err != nil {
			t.Fatalf("inviteForgotten: insert user %s: %v", uid, err)
		}
	}

	// Insert room
	_, err := db.Exec(`INSERT INTO rooms (room_id, visibility, created_at) VALUES ($1, 'private', $2) ON CONFLICT DO NOTHING`, roomID, now)
	if err != nil {
		t.Fatalf("inviteForgotten: insert room: %v", err)
	}

	// Insert a pending invitation (accepted_at and rejected_at both NULL)
	_, err = db.Exec(
		`INSERT INTO room_invitations (room_id, inviter_id, invitee_id, created_at)
		 VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`,
		roomID, inviterID, userID, now,
	)
	if err != nil {
		t.Fatalf("inviteForgotten: insert invitation: %v", err)
	}

	// Insert forgotten_rooms entry — the invite must be excluded
	_, err = db.Exec(
		`INSERT INTO forgotten_rooms (user_id, room_id, forgotten_at_ms) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
		userID, roomID, now,
	)
	if err != nil {
		t.Fatalf("inviteForgotten: insert forgotten_rooms: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM forgotten_rooms WHERE user_id = $1 AND room_id = $2`, userID, roomID)
		_, _ = db.Exec(`DELETE FROM room_invitations WHERE room_id = $1 AND invitee_id = $2`, roomID, userID)
		_, _ = db.Exec(`DELETE FROM rooms WHERE room_id = $1`, roomID)
		_, _ = db.Exec(`DELETE FROM users WHERE user_id = $1 OR user_id = $2`, userID, inviterID)
	})

	h := &GetSyncHandler{db: db}
	invites := h.buildInviteRooms(context.Background(), userID)

	if _, found := invites[roomID]; found {
		t.Errorf("GAP-FORGET: room %q MUST NOT appear in rooms.invite after user has forgotten it", roomID)
	}
}

func TestGetSyncHandler_BuildLeaveRooms_ForgottenExcluded(t *testing.T) {
	db := openTestDB(t)

	userID := "@forgotten-user:test.local"
	roomID := "!forgotten-room:test.local"

	now := int64(1700000020000)

	// Insert user
	_, err := db.Exec(`INSERT INTO users (user_id, system_role, is_active, created_at) VALUES ($1, 'user', true, $2) ON CONFLICT DO NOTHING`, userID, now)
	if err != nil {
		t.Fatalf("forgottenExcluded: insert user: %v", err)
	}

	// Insert room
	_, err = db.Exec(`INSERT INTO rooms (room_id, visibility, created_at) VALUES ($1, 'private', $2) ON CONFLICT DO NOTHING`, roomID, now)
	if err != nil {
		t.Fatalf("forgottenExcluded: insert room: %v", err)
	}

	// Insert room_members with left_at set
	_, err = db.Exec(
		`INSERT INTO room_members (room_id, user_id, joined_at, left_at) VALUES ($1, $2, $3, $4) ON CONFLICT (room_id, user_id) DO UPDATE SET left_at = EXCLUDED.left_at`,
		roomID, userID, now-2000, now-1000,
	)
	if err != nil {
		t.Fatalf("forgottenExcluded: insert room_members: %v", err)
	}

	// Insert forgotten_rooms entry — this is what should suppress the leave room from sync
	_, err = db.Exec(
		`INSERT INTO forgotten_rooms (user_id, room_id, forgotten_at_ms) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
		userID, roomID, now,
	)
	if err != nil {
		t.Fatalf("forgottenExcluded: insert forgotten_rooms: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM forgotten_rooms WHERE user_id = $1 AND room_id = $2`, userID, roomID)
		_, _ = db.Exec(`DELETE FROM room_members WHERE room_id = $1 AND user_id = $2`, roomID, userID)
		_, _ = db.Exec(`DELETE FROM rooms WHERE room_id = $1`, roomID)
		_, _ = db.Exec(`DELETE FROM users WHERE user_id = $1`, userID)
	})

	h := &GetSyncHandler{db: db}

	// sinceMs = 0 → no time filter, but forgotten_rooms should still exclude it
	leaves := h.buildLeaveRooms(context.Background(), userID, 0)

	if _, found := leaves[roomID]; found {
		t.Errorf("GAP-FORGET: room %q MUST NOT appear in rooms.leave after user has forgotten it", roomID)
	}
}

// ─── Story 9-22: GAP-SINCE-IGNORED — per-device sync token ───────────────────

// buildAuthedSyncDeltaHandlerWithClaims is a test helper that returns a handler
// plus a makeToken function that accepts extra JWT claims (e.g. "did" for device_id).
// Unlike buildAuthedSyncDeltaHandler, it does not return the oidcSrv separately.
func buildAuthedSyncDeltaHandlerWithClaims(t *testing.T, mock *mockGetSyncDeltaCoreClient) (http.Handler, func(map[string]any) string) {
	t.Helper()

	oidcSrv, privateKey := setupOIDCServer(t)
	t.Cleanup(oidcSrv.Close)

	provider := auth.NewProvider(context.Background(), oidcSrv.URL)

	cfg := GetSyncConfig{
		CoreClient: mock,
		ServerName: "test.local",
	}
	handler := NewGetSyncHandler(cfg)

	authed := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, "test.local")(
		http.HandlerFunc(handler.GetSync),
	)

	makeToken := func(extraClaims map[string]any) string {
		return signJWT(t, oidcSrv.URL, privateKey, time.Now().Add(time.Hour), extraClaims)
	}

	return authed, makeToken
}

// TestGetSync_PerDevice_DeviceIdForwardedToCore verifies that the device_id from
// the JWT "did" claim is forwarded as GetSyncDeltaRequest.DeviceId to the Core.
//
// AC6 (Story 9-22): This test FAILS before the fix because handleIncrementalSync
// constructs GetSyncDeltaRequest without DeviceId (field stays empty "").
//
// Given:  JWT "did" claim = "TEST_DEVICE_D1"
// When:   GET /_matrix/client/v3/sync?since=s_token_abc&timeout=0
// Then:   capturedDeltaReq.DeviceId == "TEST_DEVICE_D1"
func TestGetSync_PerDevice_DeviceIdForwardedToCore(t *testing.T) {
	mock := &mockGetSyncDeltaCoreClient{
		deltaResp: &pb.GetSyncDeltaResponse{
			SinceToken:        "next_batch_device_test",
			FallbackToInitial: false,
			Rooms:             []*pb.SyncRoom{},
		},
	}

	handler, makeToken := buildAuthedSyncDeltaHandlerWithClaims(t, mock)
	token := makeToken(map[string]any{"did": "TEST_DEVICE_D1"})

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/sync?since=s_token_abc&timeout=0", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	if mock.capturedDeltaReq == nil {
		t.Fatal("expected GetSyncDelta to be called, but capturedDeltaReq is nil")
	}

	if mock.capturedDeltaReq.DeviceId != "TEST_DEVICE_D1" {
		t.Errorf("expected DeviceId=%q forwarded to Core, got %q",
			"TEST_DEVICE_D1", mock.capturedDeltaReq.DeviceId)
	}
}

// ─── Story 9-22 AC1: Per-device token independence ───────────────────────────
//
// TestSyncTokens_PerDevice_NoOverwrite verifies that two devices (D1 and D2)
// each forward their own device_id independently to Core. The gateway correctly
// isolates device contexts — the Core is responsible for per-device storage,
// but the gateway must pass the right device_id so each device's checkpoint is
// independently scoped.
//
// Given:  two devices D1 and D2 with distinct since_tokens S1 and S2
// When:   GET /sync?since=S1 is called with device D1, then GET /sync?since=S2
//         is called with device D2 (two separate requests)
// Then:   each request forwards its own device_id and since_token to Core;
//         D1 request has DeviceId="D1" and SinceToken="S1",
//         D2 request has DeviceId="D2" and SinceToken="S2"
func TestSyncTokens_PerDevice_NoOverwrite(t *testing.T) {
	mockD1 := &mockGetSyncDeltaCoreClient{
		deltaResp: &pb.GetSyncDeltaResponse{
			SinceToken:        "next_batch_d1",
			FallbackToInitial: false,
			Rooms:             []*pb.SyncRoom{},
		},
	}
	mockD2 := &mockGetSyncDeltaCoreClient{
		deltaResp: &pb.GetSyncDeltaResponse{
			SinceToken:        "next_batch_d2",
			FallbackToInitial: false,
			Rooms:             []*pb.SyncRoom{},
		},
	}

	handlerD1, makeTokenWithClaims := buildAuthedSyncDeltaHandlerWithClaims(t, mockD1)
	tokenD1 := makeTokenWithClaims(map[string]any{"did": "D1"})

	handlerD2, makeTokenWithClaims2 := buildAuthedSyncDeltaHandlerWithClaims(t, mockD2)
	tokenD2 := makeTokenWithClaims2(map[string]any{"did": "D2"})

	// Device D1 syncs with since=S1
	reqD1 := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/sync?since=S1&timeout=0", nil)
	reqD1.Header.Set("Authorization", "Bearer "+tokenD1)
	wD1 := httptest.NewRecorder()
	handlerD1.ServeHTTP(wD1, reqD1)

	if wD1.Code != http.StatusOK {
		t.Fatalf("D1: expected 200, got %d; body: %s", wD1.Code, wD1.Body.String())
	}
	if mockD1.capturedDeltaReq == nil {
		t.Fatal("D1: expected GetSyncDelta to be called")
	}
	if mockD1.capturedDeltaReq.DeviceId != "D1" {
		t.Errorf("D1: expected DeviceId=%q, got %q", "D1", mockD1.capturedDeltaReq.DeviceId)
	}
	if mockD1.capturedDeltaReq.SinceToken != "S1" {
		t.Errorf("D1: expected SinceToken=%q, got %q", "S1", mockD1.capturedDeltaReq.SinceToken)
	}

	// Device D2 syncs with since=S2 — must use its own device_id, not D1
	reqD2 := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/sync?since=S2&timeout=0", nil)
	reqD2.Header.Set("Authorization", "Bearer "+tokenD2)
	wD2 := httptest.NewRecorder()
	handlerD2.ServeHTTP(wD2, reqD2)

	if wD2.Code != http.StatusOK {
		t.Fatalf("D2: expected 200, got %d; body: %s", wD2.Code, wD2.Body.String())
	}
	if mockD2.capturedDeltaReq == nil {
		t.Fatal("D2: expected GetSyncDelta to be called")
	}
	if mockD2.capturedDeltaReq.DeviceId != "D2" {
		t.Errorf("D2: expected DeviceId=%q, got %q", "D2", mockD2.capturedDeltaReq.DeviceId)
	}
	if mockD2.capturedDeltaReq.SinceToken != "S2" {
		t.Errorf("D2: expected SinceToken=%q, got %q", "S2", mockD2.capturedDeltaReq.SinceToken)
	}

	// AC1: D1 and D2 produced independent requests with separate device_ids and since_tokens.
	// The gateway does not mix up the two device contexts.
	if mockD1.capturedDeltaReq.DeviceId == mockD2.capturedDeltaReq.DeviceId {
		t.Errorf("AC1 violation: D1 and D2 forwarded the same DeviceId %q — device contexts not isolated",
			mockD1.capturedDeltaReq.DeviceId)
	}
}

// ─── Story 9-22 AC2: Token mismatch → FallbackToInitial ──────────────────────
//
// TestSyncTokens_TokenMismatch_FallsBackToInitial verifies that when Core returns
// FallbackToInitial=true (e.g. because the client sent a stale ?since token that
// doesn't match the stored per-device token), the gateway falls back to
// GetInitialSync and returns the initial sync response to the client.
//
// Given:  device D2 sends ?since=S1 (the token for D1, not D2)
//         Core returns FallbackToInitial=true for the delta request
//         Core returns a full initial sync response when GetInitialSync is called
// When:   GET /sync?since=S1 with device D2's JWT
// Then:   server returns 200 with rooms from the initial sync (not the delta);
//         GetInitialSync WAS called; next_batch == "initial_token_for_d2"
func TestSyncTokens_TokenMismatch_FallsBackToInitial(t *testing.T) {
	stateContentBytes := []byte(`{"membership":"join"}`)

	mock := &mockGetSyncDeltaCoreClient{
		// Core signals token mismatch: FallbackToInitial=true
		deltaResp: &pb.GetSyncDeltaResponse{
			SinceToken:        "",
			FallbackToInitial: true,
			Rooms:             []*pb.SyncRoom{},
		},
		// Core returns a full initial sync response
		initialResp: &pb.GetInitialSyncResponse{
			SinceToken: "initial_token_for_d2",
			Rooms: []*pb.SyncRoom{
				{
					RoomId: "!room_d2:test.local",
					StateEvents: []*pb.SyncRoomStateEvent{
						{
							Type:     "m.room.member",
							StateKey: "@alice:test.local",
							Content:  stateContentBytes,
							Sender:   "@alice:test.local",
						},
					},
					TimelineEvents: []*pb.Event{},
				},
			},
		},
	}

	// Device D2 sends S1 (wrong token — mismatch at Core)
	handler, makeTokenWithClaims := buildAuthedSyncDeltaHandlerWithClaims(t, mock)
	token := makeTokenWithClaims(map[string]any{"did": "D2"})

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/sync?since=S1&timeout=0", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("AC2: expected 200 after token mismatch fallback, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		NextBatch string `json:"next_batch"`
		Rooms     struct {
			Join map[string]json.RawMessage `json:"join"`
		} `json:"rooms"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("AC2: failed to decode response: %v", err)
	}

	// next_batch must come from the initial sync response, not the empty delta
	if resp.NextBatch != "initial_token_for_d2" {
		t.Errorf("AC2: expected next_batch=%q (initial sync), got %q", "initial_token_for_d2", resp.NextBatch)
	}

	// GetInitialSync must have been called (fallback path taken)
	if mock.capturedInitialReq == nil {
		t.Error("AC2: expected GetInitialSync to be called after FallbackToInitial=true")
	}

	// GetSyncDelta must also have been called (it triggered the fallback)
	if mock.capturedDeltaReq == nil {
		t.Error("AC2: expected GetSyncDelta to be called with the stale token")
	}
	if mock.capturedDeltaReq.DeviceId != "D2" {
		t.Errorf("AC2: expected DeviceId=%q forwarded to Core, got %q", "D2", mock.capturedDeltaReq.DeviceId)
	}

	// The room from the initial sync must be in rooms.join
	if _, ok := resp.Rooms.Join["!room_d2:test.local"]; !ok {
		t.Errorf("AC2: expected rooms.join to contain !room_d2:test.local after fallback, got: %v", resp.Rooms.Join)
	}
}

// ─── Story 9-22 AC3: Unknown device → FallbackToInitial ──────────────────────
//
// TestSyncTokens_UnknownDevice_FallsBackToInitial verifies that when a device
// has no stored sync_tokens row (e.g. first request after re-login), Core returns
// FallbackToInitial=true and the gateway correctly serves a full initial sync.
// The gateway must NOT crash or return a 5xx — it must return 200 with a full sync.
//
// Given:  no sync_tokens row for (user_id, device_id) — Core signals fallback
//         Core returns FallbackToInitial=true for the delta request
//         Core returns a full initial sync response
// When:   GET /sync?since=<any_token>&device=<unknown_device>
// Then:   200 response; GetInitialSync WAS called; no 500 error
func TestSyncTokens_UnknownDevice_FallsBackToInitial(t *testing.T) {
	stateContentBytes := []byte(`{"membership":"join"}`)

	mock := &mockGetSyncDeltaCoreClient{
		// Core returns fallback for unknown device
		deltaResp: &pb.GetSyncDeltaResponse{
			SinceToken:        "",
			FallbackToInitial: true,
			Rooms:             []*pb.SyncRoom{},
		},
		initialResp: &pb.GetInitialSyncResponse{
			SinceToken: "initial_token_unknown_device",
			Rooms: []*pb.SyncRoom{
				{
					RoomId: "!some_room:test.local",
					StateEvents: []*pb.SyncRoomStateEvent{
						{
							Type:     "m.room.member",
							StateKey: "@alice:test.local",
							Content:  stateContentBytes,
							Sender:   "@alice:test.local",
						},
					},
					TimelineEvents: []*pb.Event{},
				},
			},
		},
	}

	// Unknown device — no stored sync_tokens row; Core returns fallback
	handler, makeTokenWithClaims := buildAuthedSyncDeltaHandlerWithClaims(t, mock)
	token := makeTokenWithClaims(map[string]any{"did": "UNKNOWN_DEVICE_XYZ"})

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/sync?since=stale_token&timeout=0", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// AC3: Must NOT crash with 500 — unknown device results in full initial sync (200)
	if w.Code != http.StatusOK {
		t.Fatalf("AC3: expected 200 for unknown device (not 500), got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		NextBatch string `json:"next_batch"`
		Rooms     struct {
			Join map[string]json.RawMessage `json:"join"`
		} `json:"rooms"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("AC3: failed to decode response: %v", err)
	}

	// next_batch must come from the initial sync, not empty
	if resp.NextBatch == "" {
		t.Error("AC3: expected non-empty next_batch after fallback for unknown device")
	}
	if resp.NextBatch != "initial_token_unknown_device" {
		t.Errorf("AC3: expected next_batch=%q, got %q", "initial_token_unknown_device", resp.NextBatch)
	}

	// GetInitialSync must have been called — unknown device triggers full sync
	if mock.capturedInitialReq == nil {
		t.Error("AC3: expected GetInitialSync to be called for unknown device")
	}

	// GetSyncDelta must have been called first (it returned fallback)
	if mock.capturedDeltaReq == nil {
		t.Error("AC3: expected GetSyncDelta to be called before fallback")
	}
	if mock.capturedDeltaReq.DeviceId != "UNKNOWN_DEVICE_XYZ" {
		t.Errorf("AC3: expected DeviceId=%q forwarded to Core, got %q",
			"UNKNOWN_DEVICE_XYZ", mock.capturedDeltaReq.DeviceId)
	}
}

// ─── Story 9-23: GAP-INVITE-STATE — invite_state Missing join_rules, avatar, create ──
//
// These tests are written FIRST (ATDD red phase), before any implementation code exists.
// ALL tests in this block are expected to FAIL until Story 9-23 is implemented.
//
// Spec §4.4.4: Stripped state events — each event MUST contain type, sender, state_key,
// and content ONLY. The following events SHOULD be included in invite_state.events:
//   - m.room.join_rules  (AC1)
//   - m.room.avatar      (AC2 — omitted entirely when url is empty/missing)
//   - m.room.create      (AC3)
//   - m.room.member      (AC4 regression)
//   - m.room.name        (AC4 regression)
//
// DB error / missing event → silently omit (never propagate as API error).
// JSONB double-encoding: content stored as string-escaped JSON must parse correctly
// using the CASE guard pattern already used for m.room.name.
//
// Test helper: insertInviteFixture sets up users + room + room_invitations row.
// Each test inserts its own events rows and cleans up via t.Cleanup.

// insertInviteFixture inserts a minimal pending-invite fixture:
//   - inviterID user row
//   - inviteeID user row
//   - room row
//   - room_invitations row (pending: accepted_at and rejected_at both NULL)
//
// Returns a cleanup function that removes all inserted rows.
func insertInviteFixture(t *testing.T, db *sql.DB, inviterID, inviteeID, roomID string) func() {
	t.Helper()
	now := int64(1700001000000)

	for _, uid := range []string{inviterID, inviteeID} {
		_, err := db.Exec(
			`INSERT INTO users (user_id, system_role, is_active, created_at) VALUES ($1, 'user', true, $2) ON CONFLICT DO NOTHING`,
			uid, now,
		)
		if err != nil {
			t.Fatalf("insertInviteFixture: insert user %s: %v", uid, err)
		}
	}

	_, err := db.Exec(
		`INSERT INTO rooms (room_id, visibility, created_at) VALUES ($1, 'private', $2) ON CONFLICT DO NOTHING`,
		roomID, now,
	)
	if err != nil {
		t.Fatalf("insertInviteFixture: insert room: %v", err)
	}

	_, err = db.Exec(
		`INSERT INTO room_invitations (room_id, inviter_id, invitee_id, invited_at)
		 VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`,
		roomID, inviterID, inviteeID, now,
	)
	if err != nil {
		t.Fatalf("insertInviteFixture: insert invitation: %v", err)
	}

	return func() {
		_, _ = db.Exec(`DELETE FROM room_invitations WHERE room_id = $1 AND invitee_id = $2`, roomID, inviteeID)
		_, _ = db.Exec(`DELETE FROM rooms WHERE room_id = $1`, roomID)
		for _, uid := range []string{inviterID, inviteeID} {
			_, _ = db.Exec(`DELETE FROM users WHERE user_id = $1`, uid)
		}
	}
}

// findInviteEvent is a helper that searches invite_state.events for an event of the
// given type and returns it plus a found flag.
func findInviteEvent(t *testing.T, invites map[string]interface{}, roomID, eventType string) (map[string]interface{}, bool) {
	t.Helper()
	roomRaw, ok := invites[roomID]
	if !ok {
		return nil, false
	}
	roomMap, ok := roomRaw.(map[string]interface{})
	if !ok {
		t.Fatalf("findInviteEvent: room entry is not map[string]interface{}, got %T", roomRaw)
	}
	inviteStateRaw, ok := roomMap["invite_state"]
	if !ok {
		return nil, false
	}
	inviteStateMap, ok := inviteStateRaw.(map[string]interface{})
	if !ok {
		t.Fatalf("findInviteEvent: invite_state is not map[string]interface{}, got %T", inviteStateRaw)
	}
	eventsRaw, ok := inviteStateMap["events"]
	if !ok {
		return nil, false
	}
	// Handle both []map[string]interface{} (our concrete type) and []interface{}
	// (e.g. when the slice has been round-tripped through JSON or an interface{}
	// container). This is consistent with how other helpers in sync_test.go work.
	switch ev := eventsRaw.(type) {
	case []map[string]interface{}:
		for _, e := range ev {
			if e["type"] == eventType {
				return e, true
			}
		}
	case []interface{}:
		for _, eRaw := range ev {
			e, ok := eRaw.(map[string]interface{})
			if !ok {
				t.Fatalf("findInviteEvent: events[i] is not map[string]interface{}, got %T", eRaw)
			}
			if e["type"] == eventType {
				return e, true
			}
		}
	default:
		t.Fatalf("findInviteEvent: events is not a recognized slice type, got %T", eventsRaw)
	}
	return nil, false
}

// assertStrippedStateFields verifies that a stripped state event (Matrix spec §4.4.4)
// contains ONLY the four allowed fields: type, sender, state_key, content.
// The following fields MUST be absent: event_id, unsigned, origin_server_ts, room_id.
// MINOR-3: added per pre-dev test quality review to enforce Matrix spec §4.4.4 MUST.
func assertStrippedStateFields(t *testing.T, ev map[string]interface{}, eventType string) {
	t.Helper()
	forbidden := []string{"event_id", "unsigned", "origin_server_ts", "room_id"}
	for _, field := range forbidden {
		if _, present := ev[field]; present {
			t.Errorf("spec §4.4.4 MUST: stripped state event %q MUST NOT contain field %q", eventType, field)
		}
	}
}

// TestBuildInviteRooms_JoinRulesPresent — AC1
//
// Given: a pending invite and a m.room.join_rules event with join_rule="public" in the
//
//	events table (JSONB object form)
//
// When:  buildInviteRooms is called for the invitee
// Then:  invite_state.events contains an event with type="m.room.join_rules" and
//
//	content.join_rule="public"
//
// RED: this test MUST FAIL until buildInviteRooms queries m.room.join_rules.
func TestBuildInviteRooms_JoinRulesPresent(t *testing.T) {
	db := openTestDB(t)

	inviterID := "@9-23-jr-inviter:test.local"
	inviteeID := "@9-23-jr-invitee:test.local"
	roomID := "!9-23-jr-room:test.local"
	eventID := "$9-23-jr-ev1:test.local"

	cleanup := insertInviteFixture(t, db, inviterID, inviteeID, roomID)
	t.Cleanup(cleanup)

	// Insert m.room.join_rules event (JSONB object form)
	_, err := db.Exec(
		`INSERT INTO events (event_id, room_id, sender, event_type, content, origin_server_ts)
		 VALUES ($1, $2, $3, 'm.room.join_rules', '{"join_rule":"public"}'::jsonb, $4)
		 ON CONFLICT DO NOTHING`,
		eventID, roomID, inviterID, int64(1700001001000),
	)
	if err != nil {
		t.Fatalf("JoinRulesPresent: insert event: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM events WHERE event_id = $1`, eventID) })

	h := &GetSyncHandler{db: db}
	invites := h.buildInviteRooms(context.Background(), inviteeID)

	ev, found := findInviteEvent(t, invites, roomID, "m.room.join_rules")
	if !found {
		t.Fatalf("AC1: invite_state.events MUST contain m.room.join_rules when event is in DB; got events: %v", invites[roomID])
	}

	content, ok := ev["content"].(map[string]string)
	if !ok {
		t.Fatalf("AC1: m.room.join_rules content is not map[string]string, got %T", ev["content"])
	}
	if content["join_rule"] != "public" {
		t.Errorf("AC1: expected content.join_rule=%q, got %q", "public", content["join_rule"])
	}
	// MINOR-3: verify stripped-state field exclusion (spec §4.4.4 MUST)
	assertStrippedStateFields(t, ev, "m.room.join_rules")
}

// TestBuildInviteRooms_JoinRulesMissing — AC1 (absent branch)
//
// Given: a pending invite but NO m.room.join_rules event in the events table for that room
// When:  buildInviteRooms is called
// Then:  no m.room.join_rules entry appears in invite_state.events (graceful omission, no error)
//
// RED: this test MUST FAIL until buildInviteRooms correctly omits the event on sql.ErrNoRows.
func TestBuildInviteRooms_JoinRulesMissing(t *testing.T) {
	db := openTestDB(t)

	inviterID := "@9-23-jrm-inviter:test.local"
	inviteeID := "@9-23-jrm-invitee:test.local"
	roomID := "!9-23-jrm-room:test.local"

	cleanup := insertInviteFixture(t, db, inviterID, inviteeID, roomID)
	t.Cleanup(cleanup)

	// Deliberately do NOT insert a m.room.join_rules event.

	h := &GetSyncHandler{db: db}
	invites := h.buildInviteRooms(context.Background(), inviteeID)

	// Room must still appear in invites (the invite itself is present)
	if _, ok := invites[roomID]; !ok {
		t.Fatalf("JoinRulesMissing: room %q must appear in rooms.invite even without join_rules event", roomID)
	}

	_, found := findInviteEvent(t, invites, roomID, "m.room.join_rules")
	if found {
		t.Errorf("AC1 (absent): m.room.join_rules MUST NOT appear in invite_state.events when no event is in DB")
	}
}

// TestBuildInviteRooms_AvatarPresentWhenUrlSet — AC2
//
// Given: a pending invite and a m.room.avatar event with url="mxc://example.com/abc" in
//
//	the events table (JSONB object form)
//
// When:  buildInviteRooms is called
// Then:  invite_state.events contains an event with type="m.room.avatar" and
//
//	content.url="mxc://example.com/abc"
//
// RED: this test MUST FAIL until buildInviteRooms queries m.room.avatar.
func TestBuildInviteRooms_AvatarPresentWhenUrlSet(t *testing.T) {
	db := openTestDB(t)

	inviterID := "@9-23-av-inviter:test.local"
	inviteeID := "@9-23-av-invitee:test.local"
	roomID := "!9-23-av-room:test.local"
	eventID := "$9-23-av-ev1:test.local"

	cleanup := insertInviteFixture(t, db, inviterID, inviteeID, roomID)
	t.Cleanup(cleanup)

	// Insert m.room.avatar event with a non-empty url (JSONB object form)
	_, err := db.Exec(
		`INSERT INTO events (event_id, room_id, sender, event_type, content, origin_server_ts)
		 VALUES ($1, $2, $3, 'm.room.avatar', '{"url":"mxc://example.com/abc"}'::jsonb, $4)
		 ON CONFLICT DO NOTHING`,
		eventID, roomID, inviterID, int64(1700001002000),
	)
	if err != nil {
		t.Fatalf("AvatarPresentWhenUrlSet: insert event: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM events WHERE event_id = $1`, eventID) })

	h := &GetSyncHandler{db: db}
	invites := h.buildInviteRooms(context.Background(), inviteeID)

	ev, found := findInviteEvent(t, invites, roomID, "m.room.avatar")
	if !found {
		t.Fatalf("AC2: invite_state.events MUST contain m.room.avatar when url is set; got events: %v", invites[roomID])
	}

	content, ok := ev["content"].(map[string]string)
	if !ok {
		t.Fatalf("AC2: m.room.avatar content is not map[string]string, got %T", ev["content"])
	}
	if content["url"] != "mxc://example.com/abc" {
		t.Errorf("AC2: expected content.url=%q, got %q", "mxc://example.com/abc", content["url"])
	}
	// MINOR-3: verify stripped-state field exclusion (spec §4.4.4 MUST)
	assertStrippedStateFields(t, ev, "m.room.avatar")
}

// TestBuildInviteRooms_AvatarAbsentWhenNoUrl — AC2 (absent branch)
//
// Given: a pending invite but no m.room.avatar event (or event with empty url)
// When:  buildInviteRooms is called
// Then:  no m.room.avatar entry appears in invite_state.events (graceful omission)
//
// Spec: "Rooms with no avatar are silently omitted — Element Web handles the missing
// event gracefully."
//
// RED: this test MUST FAIL until buildInviteRooms correctly omits avatar on empty url.
func TestBuildInviteRooms_AvatarAbsentWhenNoUrl(t *testing.T) {
	db := openTestDB(t)

	inviterID := "@9-23-avn-inviter:test.local"
	inviteeID := "@9-23-avn-invitee:test.local"
	roomID := "!9-23-avn-room:test.local"
	eventID := "$9-23-avn-ev1:test.local"

	cleanup := insertInviteFixture(t, db, inviterID, inviteeID, roomID)
	t.Cleanup(cleanup)

	// Insert m.room.avatar event with EMPTY url — must be omitted by buildInviteRooms
	_, err := db.Exec(
		`INSERT INTO events (event_id, room_id, sender, event_type, content, origin_server_ts)
		 VALUES ($1, $2, $3, 'm.room.avatar', '{"url":""}'::jsonb, $4)
		 ON CONFLICT DO NOTHING`,
		eventID, roomID, inviterID, int64(1700001003000),
	)
	if err != nil {
		t.Fatalf("AvatarAbsentWhenNoUrl: insert event: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM events WHERE event_id = $1`, eventID) })

	h := &GetSyncHandler{db: db}
	invites := h.buildInviteRooms(context.Background(), inviteeID)

	if _, ok := invites[roomID]; !ok {
		t.Fatalf("AvatarAbsentWhenNoUrl: room %q must appear in rooms.invite even without avatar url", roomID)
	}

	_, found := findInviteEvent(t, invites, roomID, "m.room.avatar")
	if found {
		t.Errorf("AC2 (absent): m.room.avatar MUST NOT appear in invite_state.events when url is empty")
	}
}

// TestBuildInviteRooms_CreatePresent — AC3
//
// Given: a pending invite and a m.room.create event with creator="@alice:example.com" in
//
//	the events table (JSONB object form)
//
// When:  buildInviteRooms is called
// Then:  invite_state.events contains an event with type="m.room.create" and
//
//	content.creator="@alice:example.com"
//
// RED: this test MUST FAIL until buildInviteRooms queries m.room.create.
func TestBuildInviteRooms_CreatePresent(t *testing.T) {
	db := openTestDB(t)

	inviterID := "@9-23-cr-inviter:test.local"
	inviteeID := "@9-23-cr-invitee:test.local"
	roomCreator := "@alice:example.com"
	roomID := "!9-23-cr-room:test.local"
	eventID := "$9-23-cr-ev1:test.local"

	cleanup := insertInviteFixture(t, db, inviterID, inviteeID, roomID)
	t.Cleanup(cleanup)

	// Insert m.room.create event (JSONB object form)
	_, err := db.Exec(
		`INSERT INTO events (event_id, room_id, sender, event_type, content, origin_server_ts)
		 VALUES ($1, $2, $3, 'm.room.create', $4::jsonb, $5)
		 ON CONFLICT DO NOTHING`,
		eventID, roomID, roomCreator, fmt.Sprintf(`{"creator":"%s"}`, roomCreator), int64(1700001004000),
	)
	if err != nil {
		t.Fatalf("CreatePresent: insert event: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM events WHERE event_id = $1`, eventID) })

	h := &GetSyncHandler{db: db}
	invites := h.buildInviteRooms(context.Background(), inviteeID)

	ev, found := findInviteEvent(t, invites, roomID, "m.room.create")
	if !found {
		t.Fatalf("AC3: invite_state.events MUST contain m.room.create when event is in DB; got events: %v", invites[roomID])
	}

	content, ok := ev["content"].(map[string]string)
	if !ok {
		t.Fatalf("AC3: m.room.create content is not map[string]string, got %T", ev["content"])
	}
	if content["creator"] != roomCreator {
		t.Errorf("AC3: expected content.creator=%q, got %q", roomCreator, content["creator"])
	}
	// MINOR-3: verify stripped-state field exclusion (spec §4.4.4 MUST)
	assertStrippedStateFields(t, ev, "m.room.create")
}

// TestBuildInviteRooms_RegressionMemberStillPresent — AC4 regression
//
// Given: a pending invite (no name/join_rules/avatar/create events in DB)
// When:  buildInviteRooms is called
// Then:  invite_state.events still contains m.room.member with content.membership="invite"
//
// Regression guard: new events (join_rules, avatar, create) MUST NOT displace the
// mandatory m.room.member event that was already present before Story 9-23.
//
// RED: this test MUST FAIL until buildInviteRooms is implemented in 9-23 and still
// preserves m.room.member.
func TestBuildInviteRooms_RegressionMemberStillPresent(t *testing.T) {
	db := openTestDB(t)

	inviterID := "@9-23-reg-inviter:test.local"
	inviteeID := "@9-23-reg-invitee:test.local"
	roomID := "!9-23-reg-room:test.local"

	cleanup := insertInviteFixture(t, db, inviterID, inviteeID, roomID)
	t.Cleanup(cleanup)

	// No additional events — only the mandatory m.room.member must be present.

	h := &GetSyncHandler{db: db}
	invites := h.buildInviteRooms(context.Background(), inviteeID)

	ev, found := findInviteEvent(t, invites, roomID, "m.room.member")
	if !found {
		t.Fatalf("AC4 regression: invite_state.events MUST always contain m.room.member; got: %v", invites[roomID])
	}

	content, ok := ev["content"].(map[string]string)
	if !ok {
		t.Fatalf("AC4 regression: m.room.member content is not map[string]string, got %T", ev["content"])
	}
	if content["membership"] != "invite" {
		t.Errorf("AC4 regression: expected content.membership=%q, got %q", "invite", content["membership"])
	}
	if ev["state_key"] != inviteeID {
		t.Errorf("AC4 regression: expected state_key=%q (invitee), got %q", inviteeID, ev["state_key"])
	}
}

// TestBuildInviteRooms_RegressionNameStillPresent — AC4 regression
//
// Given: a pending invite with a m.room.name event in the events table
// When:  buildInviteRooms is called
// Then:  invite_state.events still contains m.room.name with the correct room name
//
// Regression guard: the addition of join_rules, avatar, create queries MUST NOT break
// the existing m.room.name behaviour introduced before Story 9-23.
//
// RED: this test MUST FAIL until buildInviteRooms is implemented in 9-23 and still
// includes m.room.name.
func TestBuildInviteRooms_RegressionNameStillPresent(t *testing.T) {
	db := openTestDB(t)

	inviterID := "@9-23-regn-inviter:test.local"
	inviteeID := "@9-23-regn-invitee:test.local"
	roomID := "!9-23-regn-room:test.local"
	eventID := "$9-23-regn-ev1:test.local"

	cleanup := insertInviteFixture(t, db, inviterID, inviteeID, roomID)
	t.Cleanup(cleanup)

	// Insert m.room.name event (JSONB object form)
	_, err := db.Exec(
		`INSERT INTO events (event_id, room_id, sender, event_type, content, origin_server_ts)
		 VALUES ($1, $2, $3, 'm.room.name', '{"name":"ATDD Room 9-23"}'::jsonb, $4)
		 ON CONFLICT DO NOTHING`,
		eventID, roomID, inviterID, int64(1700001005000),
	)
	if err != nil {
		t.Fatalf("RegressionNameStillPresent: insert event: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM events WHERE event_id = $1`, eventID) })

	h := &GetSyncHandler{db: db}
	invites := h.buildInviteRooms(context.Background(), inviteeID)

	ev, found := findInviteEvent(t, invites, roomID, "m.room.name")
	if !found {
		t.Fatalf("AC4 regression: invite_state.events MUST still contain m.room.name after 9-23 changes; got: %v", invites[roomID])
	}

	content, ok := ev["content"].(map[string]string)
	if !ok {
		t.Fatalf("AC4 regression: m.room.name content is not map[string]string, got %T", ev["content"])
	}
	if content["name"] != "ATDD Room 9-23" {
		t.Errorf("AC4 regression: expected content.name=%q, got %q", "ATDD Room 9-23", content["name"])
	}
}

// ─── Story 9-24: GAP-GLOBAL-ACCOUNT-DATA — Top-level account_data in sync ────
//
// These tests are written FIRST (ATDD gate), before any Story 9-24 implementation
// exists. ALL tests in this section MUST FAIL to compile until:
//   - GlobalAccountDataDB interface is added to account_data.go
//   - GetSyncConfig.GlobalAccountDataDB field is added to sync.go
//   - syncResponse.AccountData field (type syncAccountDataSection) is added to sync.go
//   - injectGlobalAccountData helper is implemented in sync.go
//
// Test strategy:
//   - mockGlobalAccountDataDB implements the GlobalAccountDataDB interface
//     (consumer-defined, defined here alongside the tests per Go convention).
//   - buildAuthedSyncGlobalAccountDataHandler wires JWTMiddleware → GetSyncHandler
//     with both a mockGetSyncCoreClient and a mockGlobalAccountDataDB injected.
//   - Tests cover: initial sync, incremental sync, empty case, DB error degradation,
//     and per-room regression (global rows must NOT appear in rooms.join).
//
// AC coverage:
//   AC1 → TestGetSync_GlobalAccountData_InitialSync
//   AC2 → TestGetSync_GlobalAccountData_IncrementalSync
//   AC1+AC2 empty branch → TestGetSync_GlobalAccountData_Empty
//   AC6 → TestGetSync_GlobalAccountData_DBError_Degrades
//   AC4 regression → TestGetSync_PerRoomAccountData_NotAffectedByGlobal
//   AC5 (struct shape) → tested implicitly by all tests above (json:"account_data" key)

// mockGlobalAccountDataDB implements GlobalAccountDataDB for Story 9-24 tests.
// Defined here (consumer-defined interface, Go convention per ADR-009).
// This type references GlobalAccountDataDB and GlobalAccountDataRow which do NOT
// exist yet — every test in this section MUST fail with a compilation error until
// Story 9-24 adds those types to account_data.go.
type mockGlobalAccountDataDB struct {
	rows []GlobalAccountDataRow
	err  error
}

func (m *mockGlobalAccountDataDB) ListGlobalAccountData(_ context.Context, _ string) ([]GlobalAccountDataRow, error) {
	if m.err != nil {
		return []GlobalAccountDataRow{}, m.err
	}
	if m.rows == nil {
		return []GlobalAccountDataRow{}, nil
	}
	return m.rows, nil
}

// buildAuthedSyncGlobalAccountDataHandler wires JWTMiddleware → GetSyncHandler with
// a mockGetSyncCoreClient (initial sync) and an optional mockGlobalAccountDataDB.
// Used by all Story 9-24 unit tests.
// This function references GetSyncConfig.GlobalAccountDataDB which does NOT exist
// yet — compilation MUST fail until sync.go is updated in Story 9-24.
func buildAuthedSyncGlobalAccountDataHandler(
	t *testing.T,
	coreMock *mockGetSyncCoreClient,
	globalDB *mockGlobalAccountDataDB,
) (http.Handler, func() string) {
	t.Helper()

	oidcSrv, privateKey := setupOIDCServer(t)
	t.Cleanup(oidcSrv.Close)

	provider := auth.NewProvider(context.Background(), oidcSrv.URL)

	cfg := GetSyncConfig{
		CoreClient:          coreMock,
		ServerName:          "test.local",
		GlobalAccountDataDB: globalDB, // Story 9-24: GlobalAccountDataDB field DOES NOT EXIST YET
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

// buildAuthedSyncGlobalAccountDataDeltaHandler is the incremental-sync counterpart.
// Uses mockGetSyncDeltaCoreClient so both initial (FallbackToInitial) and delta paths
// are exercised together with GlobalAccountDataDB injection.
func buildAuthedSyncGlobalAccountDataDeltaHandler(
	t *testing.T,
	coreMock *mockGetSyncDeltaCoreClient,
	globalDB *mockGlobalAccountDataDB,
) (http.Handler, func() string) {
	t.Helper()

	oidcSrv, privateKey := setupOIDCServer(t)
	t.Cleanup(oidcSrv.Close)

	provider := auth.NewProvider(context.Background(), oidcSrv.URL)

	cfg := GetSyncConfig{
		CoreClient:          coreMock,
		ServerName:          "test.local",
		GlobalAccountDataDB: globalDB, // Story 9-24: GlobalAccountDataDB field DOES NOT EXIST YET
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

// ─── Story 9-24 Test 1 [P0]: Initial sync — top-level account_data.events ────
//
// AC1 — GET /sync (no ?since) must return top-level account_data.events containing
// all global account data rows for the authenticated user.
//
// Given: mockGlobalAccountDataDB returns [{type:"m.direct", content:{...}}]
// When:  GET /_matrix/client/v3/sync (no ?since)
// Then:  200; top-level "account_data".events contains 1 entry with type="m.direct"
//        and the expected content; the "account_data" key is present (never absent)
//
// RED: MUST FAIL until syncResponse.AccountData field exists and injectGlobalAccountData
// is called in GetSync (initial sync path).
func TestGetSync_GlobalAccountData_InitialSync(t *testing.T) {
	directContent := json.RawMessage(`{"@bob:nebu.test":["!room1:nebu.test"]}`)

	coreMock := &mockGetSyncCoreClient{
		resp: &pb.GetInitialSyncResponse{
			SinceToken: "next_batch_global_ac1",
			Rooms:      []*pb.SyncRoom{},
		},
	}

	globalDB := &mockGlobalAccountDataDB{
		rows: []GlobalAccountDataRow{
			{EventType: "m.direct", Content: directContent},
		},
	}

	handler, makeToken := buildAuthedSyncGlobalAccountDataHandler(t, coreMock, globalDB)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/sync", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("AC1: expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// Capture raw body before decoding — json.NewDecoder consumes the buffer.
	rawBody := w.Body.String()

	var resp struct {
		NextBatch   string `json:"next_batch"`
		AccountData struct {
			Events []struct {
				Type    string          `json:"type"`
				Content json.RawMessage `json:"content"`
			} `json:"events"`
		} `json:"account_data"`
	}
	if err := json.NewDecoder(strings.NewReader(rawBody)).Decode(&resp); err != nil {
		t.Fatalf("AC1: failed to decode response: %v", err)
	}

	// AC1: top-level account_data must be present (never absent) and contain 1 entry
	if resp.AccountData.Events == nil {
		t.Fatal("AC1: top-level account_data.events must not be nil (absent) — Story 9-24: syncResponse.AccountData field missing")
	}
	if len(resp.AccountData.Events) != 1 {
		t.Fatalf("AC1: expected 1 entry in account_data.events, got %d (body: %s)", len(resp.AccountData.Events), rawBody)
	}
	if resp.AccountData.Events[0].Type != "m.direct" {
		t.Errorf("AC1: expected account_data.events[0].type=%q, got %q", "m.direct", resp.AccountData.Events[0].Type)
	}
	// Verify content round-trips correctly
	var gotContent map[string]interface{}
	if err := json.Unmarshal(resp.AccountData.Events[0].Content, &gotContent); err != nil {
		t.Fatalf("AC1: failed to unmarshal content: %v", err)
	}
	if _, ok := gotContent["@bob:nebu.test"]; !ok {
		t.Errorf("AC1: expected content to contain @bob:nebu.test key, got: %v", gotContent)
	}

	// AC5: verify the JSON key is "account_data" (not omitted) via raw body check
	if !strings.Contains(rawBody, `"account_data"`) {
		t.Errorf("AC5: response must contain JSON key \"account_data\" (top-level), raw body: %s", rawBody)
	}
}

// ─── Story 9-24 Test 2 [P0]: Incremental sync — account_data.events ──────────
//
// AC2 — GET /sync?since=<token> must return top-level account_data.events
// containing all global account data rows. Same always-present guarantee as AC1.
//
// Given: mockGlobalAccountDataDB returns [{type:"m.push_rules", content:{...}}]
// When:  GET /_matrix/client/v3/sync?since=s_token_9_24
// Then:  200; top-level "account_data".events contains type="m.push_rules"
//
// RED: MUST FAIL until GetSyncConfig.GlobalAccountDataDB is wired into
// handleIncrementalSync and the delta sync path.
func TestGetSync_GlobalAccountData_IncrementalSync(t *testing.T) {
	pushRulesContent := json.RawMessage(`{"global":{"content":[],"override":[]}}`)

	coreMock := &mockGetSyncDeltaCoreClient{
		deltaResp: &pb.GetSyncDeltaResponse{
			SinceToken:        "next_batch_incr_global_ac2",
			FallbackToInitial: false,
			Rooms:             []*pb.SyncRoom{},
		},
	}

	globalDB := &mockGlobalAccountDataDB{
		rows: []GlobalAccountDataRow{
			{EventType: "m.push_rules", Content: pushRulesContent},
		},
	}

	handler, makeToken := buildAuthedSyncGlobalAccountDataDeltaHandler(t, coreMock, globalDB)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/sync?since=s_token_9_24", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("AC2: expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// Capture raw body before decoding — json.NewDecoder consumes the buffer.
	rawBody := w.Body.String()

	var resp struct {
		AccountData struct {
			Events []struct {
				Type string `json:"type"`
			} `json:"events"`
		} `json:"account_data"`
	}
	if err := json.NewDecoder(strings.NewReader(rawBody)).Decode(&resp); err != nil {
		t.Fatalf("AC2: failed to decode response: %v", err)
	}

	// AC2: account_data must be present and contain the m.push_rules entry
	if resp.AccountData.Events == nil {
		t.Fatal("AC2: top-level account_data.events must not be nil — incremental sync path missing GlobalAccountDataDB injection")
	}
	if len(resp.AccountData.Events) != 1 {
		t.Fatalf("AC2: expected 1 entry in account_data.events, got %d", len(resp.AccountData.Events))
	}
	if resp.AccountData.Events[0].Type != "m.push_rules" {
		t.Errorf("AC2: expected account_data.events[0].type=%q, got %q", "m.push_rules", resp.AccountData.Events[0].Type)
	}

	// AC5: verify "account_data" key present in incremental sync response
	if !strings.Contains(rawBody, `"account_data"`) {
		t.Errorf("AC5: incremental sync response must contain JSON key \"account_data\" (top-level), raw body: %s", rawBody)
	}
}

// ─── Story 9-24 Test 2b [P0]: Incremental sync FallbackToInitial path ────────
//
// AC2 FallbackToInitial — GET /sync?since=<token> when Core returns
// FallbackToInitial=true MUST also inject top-level account_data.events from
// GlobalAccountDataDB. This covers the case where the developer wires injection
// in the normal delta path but forgets the FallbackToInitial branch.
//
// Given: mockGetSyncDeltaCoreClient with FallbackToInitial=true and empty rooms;
//        mockGlobalAccountDataDB returns [{type:"m.ignored_user_list", content:{...}}]
// When:  GET /_matrix/client/v3/sync?since=s_stale_token_9_24_fallback
// Then:  200; top-level "account_data".events contains type="m.ignored_user_list"
//
// RED: MUST FAIL until the FallbackToInitial branch in GetSync also calls
// injectGlobalAccountData (not just the normal incremental-sync branch).
func TestGetSync_GlobalAccountData_IncrementalSync_FallbackToInitial(t *testing.T) {
	ignoredUserContent := json.RawMessage(`{"ignored_users":{"@spam:nebu.test":{}}}`)

	coreMock := &mockGetSyncDeltaCoreClient{
		deltaResp: &pb.GetSyncDeltaResponse{
			SinceToken:        "next_batch_fallback_ac2b",
			FallbackToInitial: true,
			Rooms:             []*pb.SyncRoom{},
		},
		// initialResp is returned when FallbackToInitial triggers a GetInitialSync call
		initialResp: &pb.GetInitialSyncResponse{
			SinceToken: "next_batch_fallback_initial_ac2b",
			Rooms:      []*pb.SyncRoom{},
		},
	}

	globalDB := &mockGlobalAccountDataDB{
		rows: []GlobalAccountDataRow{
			{EventType: "m.ignored_user_list", Content: ignoredUserContent},
		},
	}

	handler, makeToken := buildAuthedSyncGlobalAccountDataDeltaHandler(t, coreMock, globalDB)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/sync?since=s_stale_token_9_24_fallback", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("AC2 FallbackToInitial: expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// Capture raw body before decoding — json.NewDecoder consumes the buffer.
	rawBodyFallback := w.Body.String()

	var resp struct {
		AccountData struct {
			Events []struct {
				Type string `json:"type"`
			} `json:"events"`
		} `json:"account_data"`
	}
	if err := json.NewDecoder(strings.NewReader(rawBodyFallback)).Decode(&resp); err != nil {
		t.Fatalf("AC2 FallbackToInitial: failed to decode response: %v", err)
	}

	// AC2 FallbackToInitial: account_data must be present and contain the m.ignored_user_list entry
	if resp.AccountData.Events == nil {
		t.Fatal("AC2 FallbackToInitial: top-level account_data.events must not be nil — FallbackToInitial path missing GlobalAccountDataDB injection")
	}
	if len(resp.AccountData.Events) != 1 {
		t.Fatalf("AC2 FallbackToInitial: expected 1 entry in account_data.events, got %d", len(resp.AccountData.Events))
	}
	if resp.AccountData.Events[0].Type != "m.ignored_user_list" {
		t.Errorf("AC2 FallbackToInitial: expected account_data.events[0].type=%q, got %q", "m.ignored_user_list", resp.AccountData.Events[0].Type)
	}

	// AC5: verify "account_data" key present in FallbackToInitial response
	if !strings.Contains(rawBodyFallback, `"account_data"`) {
		t.Errorf("AC5 FallbackToInitial: response must contain JSON key \"account_data\" (top-level), raw body: %s", rawBodyFallback)
	}
}

// ─── Story 9-24 Test 3 [P0]: Empty case — account_data.events is [] ──────────
//
// Spec: when no global account data exists, account_data.events MUST be [] —
// never absent, never null. This is a MUST per Matrix spec §6.3.
//
// Given: mockGlobalAccountDataDB returns [] (no global account data)
// When:  GET /_matrix/client/v3/sync (initial sync)
// Then:  200; response body contains "account_data":{"events":[]} (not absent, not null)
//
// RED: MUST FAIL until syncResponse.AccountData is always populated (empty slice,
// not omitempty).
func TestGetSync_GlobalAccountData_Empty(t *testing.T) {
	coreMock := &mockGetSyncCoreClient{
		resp: &pb.GetInitialSyncResponse{
			SinceToken: "next_batch_empty_global",
			Rooms:      []*pb.SyncRoom{},
		},
	}

	globalDB := &mockGlobalAccountDataDB{
		rows: []GlobalAccountDataRow{}, // empty — no global account data
	}

	handler, makeToken := buildAuthedSyncGlobalAccountDataHandler(t, coreMock, globalDB)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/sync", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("empty case: expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	rawBody := w.Body.String()

	// Spec MUST: "account_data" key must be present in the JSON response
	if !strings.Contains(rawBody, `"account_data"`) {
		t.Errorf("empty case: response MUST contain top-level \"account_data\" key even when empty; raw body: %s", rawBody)
	}

	// Spec MUST: account_data.events must be [] — not null, not absent
	var resp struct {
		AccountData *struct {
			Events []json.RawMessage `json:"events"`
		} `json:"account_data"`
	}
	if err := json.NewDecoder(strings.NewReader(rawBody)).Decode(&resp); err != nil {
		t.Fatalf("empty case: failed to decode response: %v", err)
	}
	if resp.AccountData == nil {
		t.Fatal("empty case: top-level \"account_data\" key must not be absent (omitempty not allowed for this field)")
	}
	if resp.AccountData.Events == nil {
		t.Fatal("empty case: account_data.events must be [] (not null) when no global account data exists")
	}
	if len(resp.AccountData.Events) != 0 {
		t.Errorf("empty case: expected account_data.events to be empty [], got %d entries", len(resp.AccountData.Events))
	}
}

// ─── Story 9-24 Test 4 [P0]: DB error → HTTP 200 + account_data.events:[] ────
//
// AC6 — when ListGlobalAccountData returns an error, the sync response MUST still
// be returned with HTTP 200. account_data.events MUST be [] (graceful degradation).
// The DB error is logged as WARN but not surfaced to the client.
//
// Given: mockGlobalAccountDataDB.ListGlobalAccountData returns an error
// When:  GET /_matrix/client/v3/sync
// Then:  HTTP 200; "account_data":{"events":[]}; no 500/503 returned
//
// RED: MUST FAIL until injectGlobalAccountData degrades gracefully on error.
func TestGetSync_GlobalAccountData_DBError_Degrades(t *testing.T) {
	coreMock := &mockGetSyncCoreClient{
		resp: &pb.GetInitialSyncResponse{
			SinceToken: "next_batch_dberr_global",
			Rooms:      []*pb.SyncRoom{},
		},
	}

	globalDB := &mockGlobalAccountDataDB{
		err: fmt.Errorf("simulated DB connection error for Story 9-24 AC6"),
	}

	handler, makeToken := buildAuthedSyncGlobalAccountDataHandler(t, coreMock, globalDB)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/sync", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// AC6: MUST return 200 even when DB fails — error must not be surfaced to client
	if w.Code != http.StatusOK {
		t.Fatalf("AC6 DB error degradation: expected HTTP 200, got %d; body: %s", w.Code, w.Body.String())
	}

	rawBody := w.Body.String()

	// AC6: account_data must still be present with empty events []
	var resp struct {
		AccountData *struct {
			Events []json.RawMessage `json:"events"`
		} `json:"account_data"`
	}
	if err := json.NewDecoder(strings.NewReader(rawBody)).Decode(&resp); err != nil {
		t.Fatalf("AC6: failed to decode response: %v", err)
	}
	if resp.AccountData == nil {
		t.Fatal("AC6: \"account_data\" must be present in response even on DB error (graceful degradation)")
	}
	if resp.AccountData.Events == nil {
		t.Fatal("AC6: account_data.events must be [] (not null) on DB error")
	}
	if len(resp.AccountData.Events) != 0 {
		t.Errorf("AC6: expected account_data.events to be [] on DB error, got %d entries", len(resp.AccountData.Events))
	}
}

// ─── Story 9-24 Test 5 [P1]: Per-room account_data unaffected by global data ─
//
// AC4 regression — global account data (room_id = '') MUST NOT appear in per-room
// sections (rooms.join.{roomId}.account_data.events). Per-room account data MUST
// remain unaffected.
//
// Given: globalDB returns [{type:"m.direct"}]; per-room account data for !room1
//        is provided via AccountDataDB (m.fully_read)
// When:  GET /_matrix/client/v3/sync
// Then:
//   - top-level account_data.events contains ONLY m.direct
//   - rooms.join.!room1.account_data.events contains ONLY m.fully_read
//   - m.direct does NOT appear in rooms.join.!room1.account_data.events
//   - m.fully_read does NOT appear in top-level account_data.events
//
// RED: MUST FAIL until both GlobalAccountDataDB and AccountDataDB are wired
// independently and their results are placed in the correct sections.
func TestGetSync_PerRoomAccountData_NotAffectedByGlobal(t *testing.T) {
	roomID := "!room1:test.local"
	stateContentBytes := []byte(`{"membership":"join"}`)
	directContent := json.RawMessage(`{"@bob:nebu.test":["!room1:nebu.test"]}`)
	fullyReadContent := json.RawMessage(`{"event_id":"$some-event-id"}`)

	coreMock := &mockGetSyncCoreClient{
		resp: &pb.GetInitialSyncResponse{
			SinceToken: "next_batch_regression_ac4",
			Rooms: []*pb.SyncRoom{
				{
					RoomId: roomID,
					StateEvents: []*pb.SyncRoomStateEvent{
						{
							Type:     "m.room.member",
							StateKey: "@test-sub-123:test.local",
							Content:  stateContentBytes,
							Sender:   "@test-sub-123:test.local",
						},
					},
					TimelineEvents: []*pb.Event{},
					Limited:        false,
				},
			},
		},
	}

	// Global DB returns m.direct (must go to top-level account_data only)
	globalDB := &mockGlobalAccountDataDB{
		rows: []GlobalAccountDataRow{
			{EventType: "m.direct", Content: directContent},
		},
	}

	// Per-room DB returns m.fully_read for !room1 (must go to rooms.join only)
	perRoomDB := &mockSyncAccountDataDB{
		dataByRoomAndType: map[string]map[string]json.RawMessage{
			roomID: {
				"m.fully_read": fullyReadContent,
			},
		},
	}

	oidcSrv, privateKey := setupOIDCServer(t)
	t.Cleanup(oidcSrv.Close)
	provider := auth.NewProvider(context.Background(), oidcSrv.URL)

	cfg := GetSyncConfig{
		CoreClient:          coreMock,
		ServerName:          "test.local",
		AccountDataDB:       perRoomDB,       // per-room account data
		GlobalAccountDataDB: globalDB,        // Story 9-24: global account data
	}
	handler := NewGetSyncHandler(cfg)

	authed := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, "test.local")(
		http.HandlerFunc(handler.GetSync),
	)
	makeToken := func() string {
		return signJWT(t, oidcSrv.URL, privateKey, time.Now().Add(time.Hour), nil)
	}

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/sync", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	authed.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("AC4 regression: expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		AccountData struct {
			Events []struct {
				Type string `json:"type"`
			} `json:"events"`
		} `json:"account_data"`
		Rooms struct {
			Join map[string]struct {
				AccountData struct {
					Events []struct {
						Type string `json:"type"`
					} `json:"events"`
				} `json:"account_data"`
			} `json:"join"`
		} `json:"rooms"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("AC4 regression: failed to decode response: %v", err)
	}

	// AC4: top-level account_data.events must contain m.direct and NOT m.fully_read
	var topLevelTypes []string
	for _, ev := range resp.AccountData.Events {
		topLevelTypes = append(topLevelTypes, ev.Type)
	}
	foundMDirect := false
	for _, evType := range topLevelTypes {
		if evType == "m.direct" {
			foundMDirect = true
		}
		if evType == "m.fully_read" {
			t.Errorf("AC4 regression: m.fully_read (per-room) must NOT appear in top-level account_data.events, got: %v", topLevelTypes)
		}
	}
	if !foundMDirect {
		t.Errorf("AC4 regression: m.direct (global) must appear in top-level account_data.events, got: %v", topLevelTypes)
	}

	// AC4: per-room account_data must contain m.fully_read and NOT m.direct
	roomData, ok := resp.Rooms.Join[roomID]
	if !ok {
		t.Fatalf("AC4 regression: room %q must appear in rooms.join", roomID)
	}
	var perRoomTypes []string
	for _, ev := range roomData.AccountData.Events {
		perRoomTypes = append(perRoomTypes, ev.Type)
	}
	foundFullyRead := false
	for _, evType := range perRoomTypes {
		if evType == "m.fully_read" {
			foundFullyRead = true
		}
		if evType == "m.direct" {
			t.Errorf("AC4 regression: m.direct (global) must NOT appear in rooms.join.%s.account_data.events, got: %v", roomID, perRoomTypes)
		}
	}
	if !foundFullyRead {
		t.Errorf("AC4 regression: m.fully_read (per-room) must appear in rooms.join.%s.account_data.events, got: %v", roomID, perRoomTypes)
	}
}

// mockSyncAccountDataDB is a minimal AccountDataDB mock for Story 9-24 regression tests.
// It supports per-room lookups by (roomID, eventType).
// Returns ErrAccountDataNotFound when the key does not exist.
type mockSyncAccountDataDB struct {
	dataByRoomAndType map[string]map[string]json.RawMessage
}

func (m *mockSyncAccountDataDB) GetAccountData(_ context.Context, _, roomID, eventType string) (json.RawMessage, error) {
	if m.dataByRoomAndType == nil {
		return nil, ErrAccountDataNotFound
	}
	byType, ok := m.dataByRoomAndType[roomID]
	if !ok {
		return nil, ErrAccountDataNotFound
	}
	content, ok := byType[eventType]
	if !ok {
		return nil, ErrAccountDataNotFound
	}
	return content, nil
}

func (m *mockSyncAccountDataDB) PutAccountData(_ context.Context, _, _, _ string, _ json.RawMessage) error {
	return nil
}
