package matrix

// ─── Story 4-12: GET /_matrix/client/v3/rooms/{roomId}/messages ───────────────
//
// These tests are written FIRST (ATDD gate), before implementation exists.
// ALL tests in this file are expected to FAIL until Story 4-12 is implemented.
//
// Test strategy:
//   - mockGetMessagesCoreClient implements GetMessagesCoreClient (consumer-defined
//     interface, Go convention) — defined here alongside the tests.
//   - buildAuthedMessagesHandler wires JWTMiddleware around GetMessagesHandler so
//     the full auth → handler pipeline is exercised at httptest level.
//   - A capturedReq field on the mock lets individual tests inspect the gRPC
//     request forwarded by the handler (direction, limit, room_id, from_token).
//   - gRPC error cases use status.Error(codes.NotFound, …) and
//     status.Error(codes.PermissionDenied, …) to trigger 404 and 403 paths.
//   - The unauthenticated test deliberately omits the Authorization header;
//     JWTMiddleware must return 401 before the handler is reached.
//   - The invalid-dir test uses ?dir=x (not "f" or "b"); handler must return 400.
//   - The empty-room test expects chunk: [] (not null) and empty start/end strings.
//   - The default-limit test verifies that an omitted ?limit param is forwarded as
//     10 in the gRPC request.

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

// mockGetMessagesCoreClient implements GetMessagesCoreClient (defined in messages.go).

type mockGetMessagesCoreClient struct {
	resp        *pb.GetMessagesResponse
	err         error
	capturedReq *pb.GetMessagesRequest
}

func (m *mockGetMessagesCoreClient) GetMessages(_ context.Context, req *pb.GetMessagesRequest) (*pb.GetMessagesResponse, error) {
	m.capturedReq = req
	return m.resp, m.err
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// buildAuthedMessagesHandler wires JWTMiddleware → GetMessagesHandler.
// The mux is registered with GET /rooms/{roomId}/messages so r.PathValue("roomId")
// works correctly (Go 1.22+ standard library routing).
//
// Returns the http.Handler ready for httptest, the OIDC test server, and a
// makeToken closure that mints a valid signed JWT each time it is called.
func buildAuthedMessagesHandler(t *testing.T, mock *mockGetMessagesCoreClient) (http.Handler, *httptest.Server, func() string) {
	t.Helper()

	oidcSrv, privateKey := setupOIDCServer(t)
	t.Cleanup(oidcSrv.Close)

	provider := auth.NewProvider(context.Background(), oidcSrv.URL)

	handler := NewGetMessagesHandler(GetMessagesConfig{
		CoreClient: mock,
		ServerName: "test.local",
	})

	authed := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, nil, "test.local")(
		http.HandlerFunc(handler.GetMessages),
	)

	// Wrap in a mux with named path parameter so PathValue("roomId") resolves.
	mux := http.NewServeMux()
	mux.Handle("GET /rooms/{roomId}/messages", authed)

	makeToken := func() string {
		return signJWT(t, oidcSrv.URL, privateKey, time.Now().Add(time.Hour), nil)
	}

	return mux, oidcSrv, makeToken
}

// ─── Test 1: Happy path — authenticated GET with pagination params → 200 ─────
//
// AC #1, #3, #4, #5, #6 — valid JWT + mock returns 2 events with pagination
// tokens → 200 with chunk (2 events), start, end, state fields present.

func TestGetMessages_HappyPath(t *testing.T) {
	contentBytes := []byte(`{"msgtype":"m.text","body":"hello"}`)

	mock := &mockGetMessagesCoreClient{
		resp: &pb.GetMessagesResponse{
			Events: []*pb.Event{
				{
					EventId:   "$event1:test.local",
					RoomId:    "!room1:test.local",
					SenderId:  "@alice:test.local",
					EventType: "m.room.message",
					Content:   contentBytes,
					OriginTs:  1700000000001,
				},
				{
					EventId:   "$event2:test.local",
					RoomId:    "!room1:test.local",
					SenderId:  "@bob:test.local",
					EventType: "m.room.message",
					Content:   contentBytes,
					OriginTs:  1700000000002,
				},
			},
			NextBatch: "v1_abc",
			PrevBatch: "v1_xyz",
		},
	}

	handler, _, makeToken := buildAuthedMessagesHandler(t, mock)

	req := httptest.NewRequest(http.MethodGet,
		"/rooms/!room1:test.local/messages?from=v1_xyz&dir=b&limit=5&to=v1_end_token",
		nil)
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
		Chunk []map[string]any `json:"chunk"`
		Start string           `json:"start"`
		End   string           `json:"end"`
		State []any            `json:"state"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}

	if len(resp.Chunk) != 2 {
		t.Errorf("expected 2 events in chunk, got %d", len(resp.Chunk))
	}

	// start = prev_batch, end = next_batch (Matrix spec convention)
	if resp.Start != "v1_xyz" {
		t.Errorf("expected start=v1_xyz, got %q", resp.Start)
	}
	if resp.End != "v1_abc" {
		t.Errorf("expected end=v1_abc, got %q", resp.End)
	}

	// state must be present and empty
	if resp.State == nil {
		t.Error("expected state field to be present (not null)")
	}
	if len(resp.State) != 0 {
		t.Errorf("expected state to be empty, got %d entries", len(resp.State))
	}

	// Each event in chunk must carry the required Matrix fields
	for i, ev := range resp.Chunk {
		for _, field := range []string{"event_id", "room_id", "sender", "type", "content", "origin_server_ts"} {
			if _, ok := ev[field]; !ok {
				t.Errorf("chunk[%d] missing required field %q", i, field)
			}
		}
	}

	// Verify the gRPC request forwarded the correct parameters
	if mock.capturedReq == nil {
		t.Fatal("expected gRPC GetMessages to be called, but capturedReq is nil")
	}
	if mock.capturedReq.RoomId != "!room1:test.local" {
		t.Errorf("expected room_id !room1:test.local, got %q", mock.capturedReq.RoomId)
	}
	if mock.capturedReq.FromToken != "v1_xyz" {
		t.Errorf("expected from_token v1_xyz, got %q", mock.capturedReq.FromToken)
	}
	if mock.capturedReq.Direction != "b" {
		t.Errorf("expected direction b, got %q", mock.capturedReq.Direction)
	}
	if mock.capturedReq.Limit != 5 {
		t.Errorf("expected limit 5, got %d", mock.capturedReq.Limit)
	}
	if mock.capturedReq.GetToToken() != "v1_end_token" {
		t.Errorf("expected to_token v1_end_token, got %q", mock.capturedReq.GetToToken())
	}
}

// ─── Test 2: Unauthenticated request → 401 M_MISSING_TOKEN ───────────────────
//
// AC #1 — JWTMiddleware must reject requests with no Authorization header.

func TestGetMessages_Unauthenticated(t *testing.T) {
	mock := &mockGetMessagesCoreClient{}

	handler, _, _ := buildAuthedMessagesHandler(t, mock)

	req := httptest.NewRequest(http.MethodGet,
		"/rooms/!room1:test.local/messages",
		nil)
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
}

// ─── Test 3: Room not found → 404 M_NOT_FOUND ────────────────────────────────
//
// AC #8 — gRPC NOT_FOUND maps to HTTP 404 M_NOT_FOUND.

func TestGetMessages_RoomNotFound(t *testing.T) {
	mock := &mockGetMessagesCoreClient{
		err: status.Error(codes.NotFound, "room not found"),
	}

	handler, _, makeToken := buildAuthedMessagesHandler(t, mock)

	req := httptest.NewRequest(http.MethodGet,
		"/rooms/!nonexistent:test.local/messages",
		nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

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

// ─── Test 4: Non-member (forbidden) → 403 M_FORBIDDEN ────────────────────────
//
// AC #7 — gRPC PERMISSION_DENIED maps to HTTP 403 M_FORBIDDEN.

func TestGetMessages_NotMember(t *testing.T) {
	mock := &mockGetMessagesCoreClient{
		err: status.Error(codes.PermissionDenied, "not a member"),
	}

	handler, _, makeToken := buildAuthedMessagesHandler(t, mock)

	req := httptest.NewRequest(http.MethodGet,
		"/rooms/!private:test.local/messages",
		nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

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

// ─── Test 5: Invalid dir parameter → 400 M_INVALID_PARAM ─────────────────────
//
// AC #3, #9 — dir must be "f" or "b"; any other value is rejected with 400.

func TestGetMessages_InvalidDir(t *testing.T) {
	mock := &mockGetMessagesCoreClient{}

	handler, _, makeToken := buildAuthedMessagesHandler(t, mock)

	req := httptest.NewRequest(http.MethodGet,
		"/rooms/!room1:test.local/messages?dir=x",
		nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixError
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.ErrCode != "M_INVALID_PARAM" {
		t.Errorf("expected errcode M_INVALID_PARAM, got %s", errResp.ErrCode)
	}

	// gRPC must NOT have been called for an invalid request
	if mock.capturedReq != nil {
		t.Error("expected gRPC GetMessages to NOT be called on invalid dir, but capturedReq was set")
	}
}

// ─── Test 6: Empty room → 200 with chunk: [] ─────────────────────────────────
//
// AC #5 — when core returns 0 events, chunk must be an empty JSON array (not null).

func TestGetMessages_EmptyRoom(t *testing.T) {
	mock := &mockGetMessagesCoreClient{
		resp: &pb.GetMessagesResponse{
			Events:    []*pb.Event{},
			NextBatch: "",
			PrevBatch: "",
		},
	}

	handler, _, makeToken := buildAuthedMessagesHandler(t, mock)

	req := httptest.NewRequest(http.MethodGet,
		"/rooms/!empty:test.local/messages",
		nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()

	// chunk must be present as an array, not as null
	if !strings.Contains(body, `"chunk":[]`) && !strings.Contains(body, `"chunk": []`) {
		t.Errorf("expected chunk to be empty array [], body: %s", body)
	}

	var resp struct {
		Chunk []any  `json:"chunk"`
		Start string `json:"start"`
		End   string `json:"end"`
	}
	if err := json.NewDecoder(strings.NewReader(body)).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}

	if resp.Chunk == nil {
		t.Error("expected chunk to be [] (not null)")
	}
	if len(resp.Chunk) != 0 {
		t.Errorf("expected empty chunk, got %d events", len(resp.Chunk))
	}
	if resp.Start != "" {
		t.Errorf("expected empty start, got %q", resp.Start)
	}
	if resp.End != "" {
		t.Errorf("expected empty end, got %q", resp.End)
	}
}

// ─── Test 7: No limit param → default limit 10 forwarded to gRPC ─────────────
//
// AC #3 — when ?limit is absent, the handler must default to 10 and forward
// that value in the gRPC request's Limit field.

func TestGetMessages_DefaultLimit(t *testing.T) {
	mock := &mockGetMessagesCoreClient{
		resp: &pb.GetMessagesResponse{
			Events:    []*pb.Event{},
			NextBatch: "",
			PrevBatch: "",
		},
	}

	handler, _, makeToken := buildAuthedMessagesHandler(t, mock)

	// No ?limit param — handler must fill in the default (10)
	req := httptest.NewRequest(http.MethodGet,
		"/rooms/!room1:test.local/messages",
		nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	if mock.capturedReq == nil {
		t.Fatal("expected gRPC GetMessages to be called, but capturedReq is nil")
	}

	const defaultLimit = int32(10)
	if mock.capturedReq.Limit != defaultLimit {
		t.Errorf("expected default limit %d forwarded to gRPC, got %d",
			defaultLimit, mock.capturedReq.Limit)
	}

	// Default direction must be "b" when dir param is absent
	if mock.capturedReq.Direction != "b" {
		t.Errorf("expected default direction b, got %q", mock.capturedReq.Direction)
	}
}

// ─── Test 8: Invalid limit parameter → 400 M_INVALID_PARAM ──────────────────
//
// MAJOR-1 — ?limit=abc (non-numeric) must be rejected with 400 before gRPC call.

func TestGetMessages_InvalidLimit(t *testing.T) {
	mock := &mockGetMessagesCoreClient{}

	handler, _, makeToken := buildAuthedMessagesHandler(t, mock)

	req := httptest.NewRequest(http.MethodGet,
		"/rooms/!room1:test.local/messages?limit=abc",
		nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixError
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.ErrCode != "M_INVALID_PARAM" {
		t.Errorf("expected errcode M_INVALID_PARAM, got %s", errResp.ErrCode)
	}

	// gRPC must NOT have been called for an invalid request
	if mock.capturedReq != nil {
		t.Error("expected gRPC GetMessages to NOT be called on invalid limit, but capturedReq was set")
	}
}

// ─── Test 9 (Story 6-9): Archived room → GET /messages still returns 200 ─────
//
// MAJOR-1 — archived rooms allow message history reads.
//
// GetMessagesHandler has NO StatusChecker (unlike SendEventHandler) — this is
// intentional. Read-only access to message history is preserved even after a
// room is archived. The handler does not inspect room status at all; Core simply
// returns the events it has.
//
// This test documents that invariant explicitly: a request against an archived
// room ID succeeds identically to a request against an active room.

func TestGetMessages_ArchivedRoom_Returns200(t *testing.T) {
	// archived rooms allow message history reads — no status check in GetMessages handler
	contentBytes := []byte(`{"msgtype":"m.text","body":"archived message"}`)

	mock := &mockGetMessagesCoreClient{
		resp: &pb.GetMessagesResponse{
			Events: []*pb.Event{
				{
					EventId:   "$archived-event1:test.local",
					RoomId:    "!archived:test.local",
					SenderId:  "@alice:test.local",
					EventType: "m.room.message",
					Content:   contentBytes,
					OriginTs:  1700000000999,
				},
			},
			NextBatch: "v1_arch_next",
			PrevBatch: "v1_arch_prev",
		},
	}

	handler, _, makeToken := buildAuthedMessagesHandler(t, mock)

	// Use an "archived" room ID — the handler does not check room status,
	// so Core returning events is all that matters.
	req := httptest.NewRequest(http.MethodGet,
		"/rooms/!archived:test.local/messages?dir=b",
		nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Archived rooms must still return 200 — read-only history access is preserved.
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for archived room GET /messages, got %d; body: %s",
			w.Code, w.Body.String())
	}

	var resp struct {
		Chunk []map[string]any `json:"chunk"`
		Start string           `json:"start"`
		End   string           `json:"end"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}

	if len(resp.Chunk) != 1 {
		t.Errorf("expected 1 event in chunk for archived room, got %d", len(resp.Chunk))
	}
	if resp.Start != "v1_arch_prev" {
		t.Errorf("expected start=v1_arch_prev, got %q", resp.Start)
	}
	if resp.End != "v1_arch_next" {
		t.Errorf("expected end=v1_arch_next, got %q", resp.End)
	}

	// Handler must have forwarded the request to Core unchanged — no early exit.
	if mock.capturedReq == nil {
		t.Fatal("expected gRPC GetMessages to be called for archived room, but capturedReq is nil")
	}
	if mock.capturedReq.RoomId != "!archived:test.local" {
		t.Errorf("expected room_id !archived:test.local, got %q", mock.capturedReq.RoomId)
	}
}

// ─── Test 9: Limit clamping → gRPC receives clamped limit value ──────────────
//
// MINOR-3 — ?limit=0 must be clamped to 1; ?limit=500 must be clamped to 100.
// Both cases verify the clamped value is forwarded in the gRPC request.

func TestGetMessages_LimitClamping(t *testing.T) {
	cases := []struct {
		name          string
		queryLimit    string
		expectedLimit int32
	}{
		{"zero clamps to 1", "0", 1},
		{"over-max clamps to 100", "500", 100},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mock := &mockGetMessagesCoreClient{
				resp: &pb.GetMessagesResponse{
					Events:    []*pb.Event{},
					NextBatch: "",
					PrevBatch: "",
				},
			}

			handler, _, makeToken := buildAuthedMessagesHandler(t, mock)

			req := httptest.NewRequest(http.MethodGet,
				"/rooms/!room1:test.local/messages?limit="+tc.queryLimit,
				nil)
			req.Header.Set("Authorization", "Bearer "+makeToken())

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
			}

			if mock.capturedReq == nil {
				t.Fatal("expected gRPC GetMessages to be called, but capturedReq is nil")
			}

			if mock.capturedReq.Limit != tc.expectedLimit {
				t.Errorf("expected clamped limit %d, got %d", tc.expectedLimit, mock.capturedReq.Limit)
			}
		})
	}
}
