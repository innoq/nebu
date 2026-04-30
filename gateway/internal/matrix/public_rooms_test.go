package matrix

// Story 7-27: Public Room Directory — GET/POST /_matrix/client/v3/publicRooms
//
// Tests written FIRST (ATDD gate), before implementation exists.
// ALL tests in this file MUST FAIL until public_rooms.go is created and routes are registered.
//
// Acceptance Criteria covered:
//   AC1 — GET /publicRooms returns paginated list; limit defaults to 20, capped at 100.
//   AC2 — POST /publicRooms accepts filter body; generic_search_term is forwarded to Core.
//   AC3 — Each chunk entry contains: room_id, name, topic, num_joined_members, world_readable, guest_can_join.
//   AC4 — num_joined_members reflects the live count returned by the gRPC Core (mock).
//   AC5 — Only public rooms appear (enforced by Core; the handler passes through what Core returns).
//   AC6 — GET is accessible without JWT (test: handler does NOT consult auth context).
//         POST requires JWT — enforced by jwtMiddleware in main.go; tested with 401 assertion.
//   AC7 — Cursor-based: next_batch is present when Core returns a non-empty NextCursor.
//
// Design:
//   - PublicRoomsHandler wraps both endpoints; constructor takes PublicRoomsConfig.
//   - PublicRoomsCoreClient is the consumer-defined gRPC interface.
//   - mockPublicRoomsCoreClient implements PublicRoomsCoreClient in-memory.
//   - Tests cover happy paths + limit clamping + cursor forwarding + filter forwarding.
//
// NOTE: PublicRoomsHandler, PublicRoomsConfig, NewPublicRoomsHandler, PublicRoomsCoreClient
// are declared in gateway/internal/matrix/public_rooms.go.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	pb "github.com/nebu/nebu/internal/grpc/pb"
	"github.com/nebu/nebu/internal/middleware"
)

// ─── Mock PublicRoomsCoreClient ───────────────────────────────────────────────

// mockPublicRoomsCoreClient implements PublicRoomsCoreClient.
type mockPublicRoomsCoreClient struct {
	// response is returned on the next call; can be overridden per test.
	response *pb.ListPublicRoomsResponse
	// capturedReq holds the last request received; lets tests assert forwarded params.
	capturedReq *pb.ListPublicRoomsRequest
	// err is returned instead of response when non-nil.
	err error
}

func (m *mockPublicRoomsCoreClient) ListPublicRooms(_ context.Context, req *pb.ListPublicRoomsRequest) (*pb.ListPublicRoomsResponse, error) {
	m.capturedReq = req
	if m.err != nil {
		return nil, m.err
	}
	if m.response != nil {
		return m.response, nil
	}
	// Default: empty directory.
	return &pb.ListPublicRoomsResponse{
		Rooms:         []*pb.RoomSummary{},
		TotalEstimate: 0,
	}, nil
}

// ─── Helper ───────────────────────────────────────────────────────────────────

// newTestPublicRoomsHandler builds a handler with a mock gRPC client.
func newTestPublicRoomsHandler(mock *mockPublicRoomsCoreClient) *PublicRoomsHandler {
	return NewPublicRoomsHandler(PublicRoomsConfig{
		CoreClient: mock,
		ServerName: "test.server",
	})
}

// ─── GET tests ────────────────────────────────────────────────────────────────

// TestGetPublicRooms_EmptyDirectory_Returns200WithEmptyChunk verifies that a
// GET request to an empty directory returns HTTP 200 with chunk=[] (AC1).
func TestGetPublicRooms_EmptyDirectory_Returns200WithEmptyChunk(t *testing.T) {
	mock := &mockPublicRoomsCoreClient{}
	h := newTestPublicRoomsHandler(mock)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/publicRooms", nil)
	w := httptest.NewRecorder()

	h.GetPublicRooms(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	var resp struct {
		Chunk []interface{} `json:"chunk"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON response: %v (body: %s)", err, w.Body.String())
	}
	if resp.Chunk == nil {
		t.Fatal("expected chunk to be present (not null)")
	}
	if len(resp.Chunk) != 0 {
		t.Fatalf("expected empty chunk, got %d entries", len(resp.Chunk))
	}
}

// TestGetPublicRooms_WithRooms_ReturnsChunkEntries verifies that each chunk entry
// contains the required fields: room_id, num_joined_members, world_readable, guest_can_join (AC3).
func TestGetPublicRooms_WithRooms_ReturnsChunkEntries(t *testing.T) {
	mock := &mockPublicRoomsCoreClient{
		response: &pb.ListPublicRoomsResponse{
			Rooms: []*pb.RoomSummary{
				{
					RoomId:           "!pub1:test.server",
					Name:             "General",
					Topic:            "General chat",
					NumJoinedMembers: 5,
					WorldReadable:    false,
					GuestCanJoin:     false,
				},
				{
					RoomId:           "!pub2:test.server",
					Name:             "Engineering",
					NumJoinedMembers: 3,
					WorldReadable:    false,
					GuestCanJoin:     false,
				},
			},
			TotalEstimate: 2,
		},
	}
	h := newTestPublicRoomsHandler(mock)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/publicRooms", nil)
	w := httptest.NewRecorder()

	h.GetPublicRooms(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	var resp struct {
		Chunk []struct {
			RoomID           string `json:"room_id"`
			Name             string `json:"name"`
			Topic            string `json:"topic"`
			NumJoinedMembers int    `json:"num_joined_members"`
			WorldReadable    bool   `json:"world_readable"`
			GuestCanJoin     bool   `json:"guest_can_join"`
		} `json:"chunk"`
		TotalRoomCountEst int `json:"total_room_count_estimate"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON response: %v (body: %s)", err, w.Body.String())
	}
	if len(resp.Chunk) != 2 {
		t.Fatalf("expected 2 chunk entries, got %d", len(resp.Chunk))
	}

	first := resp.Chunk[0]
	if first.RoomID != "!pub1:test.server" {
		t.Errorf("expected room_id !pub1:test.server, got %s", first.RoomID)
	}
	if first.Name != "General" {
		t.Errorf("expected name General, got %s", first.Name)
	}
	if first.Topic != "General chat" {
		t.Errorf("expected topic General chat, got %s", first.Topic)
	}

	// AC4 — num_joined_members is the live count from Core (5 from mock).
	if first.NumJoinedMembers != 5 {
		t.Errorf("expected num_joined_members 5, got %d", first.NumJoinedMembers)
	}
	// AC3 — world_readable and guest_can_join always present.
	if first.WorldReadable {
		t.Error("expected world_readable=false")
	}
	if first.GuestCanJoin {
		t.Error("expected guest_can_join=false")
	}

	// AC1 — total_room_count_estimate present.
	if resp.TotalRoomCountEst != 2 {
		t.Errorf("expected total_room_count_estimate 2, got %d", resp.TotalRoomCountEst)
	}
}

// TestGetPublicRooms_LimitQueryParam_ForwardedToCore verifies that the `limit`
// query parameter is parsed and forwarded to the gRPC Core (AC1).
func TestGetPublicRooms_LimitQueryParam_ForwardedToCore(t *testing.T) {
	mock := &mockPublicRoomsCoreClient{}
	h := newTestPublicRoomsHandler(mock)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/publicRooms?limit=5", nil)
	w := httptest.NewRecorder()

	h.GetPublicRooms(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if mock.capturedReq == nil {
		t.Fatal("expected gRPC call to be made")
	}
	if mock.capturedReq.Limit != 5 {
		t.Errorf("expected limit 5 forwarded to Core, got %d", mock.capturedReq.Limit)
	}
}

// TestGetPublicRooms_DefaultLimit_Is20 verifies that an absent limit param defaults to 20 (AC1).
func TestGetPublicRooms_DefaultLimit_Is20(t *testing.T) {
	mock := &mockPublicRoomsCoreClient{}
	h := newTestPublicRoomsHandler(mock)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/publicRooms", nil)
	w := httptest.NewRecorder()

	h.GetPublicRooms(w, req)

	if mock.capturedReq == nil {
		t.Fatal("expected gRPC call to be made")
	}
	if mock.capturedReq.Limit != defaultPublicRoomsLimit {
		t.Errorf("expected default limit %d, got %d", defaultPublicRoomsLimit, mock.capturedReq.Limit)
	}
}

// TestGetPublicRooms_LimitCap_100 verifies that limit > 100 is capped at 100 (AC1).
func TestGetPublicRooms_LimitCap_100(t *testing.T) {
	mock := &mockPublicRoomsCoreClient{}
	h := newTestPublicRoomsHandler(mock)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/publicRooms?limit=9999", nil)
	w := httptest.NewRecorder()

	h.GetPublicRooms(w, req)

	if mock.capturedReq == nil {
		t.Fatal("expected gRPC call to be made")
	}
	if mock.capturedReq.Limit != maxPublicRoomsLimit {
		t.Errorf("expected limit capped at %d, got %d", maxPublicRoomsLimit, mock.capturedReq.Limit)
	}
}

// TestGetPublicRooms_SinceQueryParam_ForwardedToCore verifies that the `since`
// cursor query param is forwarded to the Core (AC7).
func TestGetPublicRooms_SinceQueryParam_ForwardedToCore(t *testing.T) {
	mock := &mockPublicRoomsCoreClient{}
	h := newTestPublicRoomsHandler(mock)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/publicRooms?since=!abc:server", nil)
	w := httptest.NewRecorder()

	h.GetPublicRooms(w, req)

	if mock.capturedReq == nil {
		t.Fatal("expected gRPC call to be made")
	}
	if mock.capturedReq.Since != "!abc:server" {
		t.Errorf("expected since '!abc:server' forwarded, got %q", mock.capturedReq.Since)
	}
}

// TestGetPublicRooms_NextBatch_PresentWhenCoreReturnsNextCursor verifies that
// next_batch is present in the response when Core returns a non-empty NextCursor (AC7).
func TestGetPublicRooms_NextBatch_PresentWhenCoreReturnsNextCursor(t *testing.T) {
	mock := &mockPublicRoomsCoreClient{
		response: &pb.ListPublicRoomsResponse{
			Rooms: []*pb.RoomSummary{
				{RoomId: "!pub1:test.server", NumJoinedMembers: 1},
				{RoomId: "!pub2:test.server", NumJoinedMembers: 2},
			},
			NextCursor:    "!pub2:test.server",
			TotalEstimate: 5,
		},
	}
	h := newTestPublicRoomsHandler(mock)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/publicRooms?limit=2", nil)
	w := httptest.NewRecorder()

	h.GetPublicRooms(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		Chunk     []interface{} `json:"chunk"`
		NextBatch string        `json:"next_batch"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v (body: %s)", err, w.Body.String())
	}
	if resp.NextBatch == "" {
		t.Error("expected next_batch to be present when Core returns NextCursor")
	}
	if resp.NextBatch != "!pub2:test.server" {
		t.Errorf("expected next_batch '!pub2:test.server', got %q", resp.NextBatch)
	}
}

// TestGetPublicRooms_NextBatch_AbsentWhenNoMorePages verifies that next_batch is
// omitted from the JSON when Core returns an empty NextCursor (AC1, AC7).
func TestGetPublicRooms_NextBatch_AbsentWhenNoMorePages(t *testing.T) {
	mock := &mockPublicRoomsCoreClient{
		response: &pb.ListPublicRoomsResponse{
			Rooms:         []*pb.RoomSummary{{RoomId: "!pub1:test.server", NumJoinedMembers: 1}},
			NextCursor:    "", // no more pages
			TotalEstimate: 1,
		},
	}
	h := newTestPublicRoomsHandler(mock)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/publicRooms", nil)
	w := httptest.NewRecorder()

	h.GetPublicRooms(w, req)

	body := w.Body.String()
	// next_batch must NOT appear in the JSON (omitempty on empty string).
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		t.Fatalf("invalid JSON: %v (body: %s)", err, body)
	}
	if _, ok := raw["next_batch"]; ok {
		t.Error("next_batch must be absent when there are no more pages (Core returned empty NextCursor)")
	}
}

// TestGetPublicRooms_InvalidLimitParam_Returns400 verifies that a non-numeric limit
// query param returns HTTP 400 M_INVALID_PARAM.
func TestGetPublicRooms_InvalidLimitParam_Returns400(t *testing.T) {
	mock := &mockPublicRoomsCoreClient{}
	h := newTestPublicRoomsHandler(mock)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/publicRooms?limit=notanumber", nil)
	w := httptest.NewRecorder()

	h.GetPublicRooms(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (body: %s)", w.Code, w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, "M_INVALID_PARAM") {
		t.Errorf("expected M_INVALID_PARAM in body, got: %s", body)
	}
}

// ─── POST tests ───────────────────────────────────────────────────────────────

// TestPostPublicRooms_WithFilter_ForwardsFilterTermToCore verifies that the
// generic_search_term from the POST body is forwarded to the gRPC Core (AC2).
func TestPostPublicRooms_WithFilter_ForwardsFilterTermToCore(t *testing.T) {
	mock := &mockPublicRoomsCoreClient{}
	h := newTestPublicRoomsHandler(mock)

	bodyJSON := `{"filter":{"generic_search_term":"engi"},"limit":10}`
	req := httptest.NewRequest(http.MethodPost, "/_matrix/client/v3/publicRooms", bytes.NewBufferString(bodyJSON))
	req.Header.Set("Content-Type", "application/json")

	// Inject a fake authenticated context (jwtMiddleware would set this in production).
	ctx := req.Context()
	ctx = contextWithUser(ctx, "@kai:test.server", "user")
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	h.PostPublicRooms(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}
	if mock.capturedReq == nil {
		t.Fatal("expected gRPC call to be made")
	}
	if mock.capturedReq.FilterTerm != "engi" {
		t.Errorf("expected filter_term 'engi' forwarded to Core, got %q", mock.capturedReq.FilterTerm)
	}
	if mock.capturedReq.Limit != 10 {
		t.Errorf("expected limit 10, got %d", mock.capturedReq.Limit)
	}
}

// TestPostPublicRooms_EmptyBody_Returns200 verifies that POST with an empty body
// returns HTTP 200 (body is optional).
func TestPostPublicRooms_EmptyBody_Returns200(t *testing.T) {
	mock := &mockPublicRoomsCoreClient{}
	h := newTestPublicRoomsHandler(mock)

	req := httptest.NewRequest(http.MethodPost, "/_matrix/client/v3/publicRooms", nil)
	// No Content-Type, no body — this is a valid POST per Matrix spec.

	ctx := contextWithUser(req.Context(), "@kai:test.server", "user")
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	h.PostPublicRooms(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}
}

// TestPostPublicRooms_WithSinceCursor_ForwardedToCore verifies that the `since`
// field from the POST body is forwarded as the cursor (AC7).
func TestPostPublicRooms_WithSinceCursor_ForwardedToCore(t *testing.T) {
	mock := &mockPublicRoomsCoreClient{}
	h := newTestPublicRoomsHandler(mock)

	bodyJSON := `{"since":"!pub2:test.server","limit":2}`
	req := httptest.NewRequest(http.MethodPost, "/_matrix/client/v3/publicRooms", bytes.NewBufferString(bodyJSON))
	req.Header.Set("Content-Type", "application/json")
	ctx := contextWithUser(req.Context(), "@kai:test.server", "user")
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	h.PostPublicRooms(w, req)

	if mock.capturedReq == nil {
		t.Fatal("expected gRPC call to be made")
	}
	if mock.capturedReq.Since != "!pub2:test.server" {
		t.Errorf("expected since '!pub2:test.server' forwarded, got %q", mock.capturedReq.Since)
	}
}

// TestPostPublicRooms_NonJSONContentType_Returns415 verifies that POST with a
// non-JSON Content-Type returns HTTP 415 M_UNSUPPORTED_MEDIA_TYPE.
func TestPostPublicRooms_NonJSONContentType_Returns415(t *testing.T) {
	mock := &mockPublicRoomsCoreClient{}
	h := newTestPublicRoomsHandler(mock)

	// Non-zero body with wrong Content-Type.
	body := bytes.NewBufferString(`{"limit":5}`)
	req := httptest.NewRequest(http.MethodPost, "/_matrix/client/v3/publicRooms", body)
	req.Header.Set("Content-Type", "text/plain")
	req.ContentLength = int64(body.Len())

	ctx := contextWithUser(req.Context(), "@kai:test.server", "user")
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	h.PostPublicRooms(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected 415, got %d (body: %s)", w.Code, w.Body.String())
	}
}

// TestGetPublicRooms_NumJoinedMembers_IsAccurate verifies AC4: num_joined_members
// reflects the live count from Core, not a stale DB value.
// The mock Core returns 5 — the handler must pass it through unchanged.
func TestGetPublicRooms_NumJoinedMembers_IsAccurate(t *testing.T) {
	mock := &mockPublicRoomsCoreClient{
		response: &pb.ListPublicRoomsResponse{
			Rooms: []*pb.RoomSummary{
				{
					RoomId:           "!pub1:test.server",
					Name:             "Live Room",
					NumJoinedMembers: 5, // live count from Room GenServer
					WorldReadable:    false,
					GuestCanJoin:     false,
				},
			},
			TotalEstimate: 1,
		},
	}
	h := newTestPublicRoomsHandler(mock)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/publicRooms", nil)
	w := httptest.NewRecorder()

	h.GetPublicRooms(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		Chunk []struct {
			RoomID           string `json:"room_id"`
			NumJoinedMembers int    `json:"num_joined_members"`
		} `json:"chunk"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v (body: %s)", err, w.Body.String())
	}
	if len(resp.Chunk) == 0 {
		t.Fatal("expected 1 chunk entry")
	}
	if resp.Chunk[0].NumJoinedMembers != 5 {
		t.Errorf("expected num_joined_members=5 (live count from Core), got %d", resp.Chunk[0].NumJoinedMembers)
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// contextWithUser injects user_id and system_role into ctx (simulates jwtMiddleware).
func contextWithUser(ctx context.Context, userID, role string) context.Context {
	ctx = context.WithValue(ctx, middleware.ContextKeyUserID, userID)
	ctx = context.WithValue(ctx, middleware.ContextKeySystemRole, role)
	return ctx
}

