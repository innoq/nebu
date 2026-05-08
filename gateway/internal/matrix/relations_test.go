package matrix

// ─── Story 9-29: GET /_matrix/client/v1/rooms/{roomId}/relations/{eventId}[/{relType}[/{eventType}]] ───
//
// Unit tests for the updated GetRelations handler.
// Story 9-28 introduced the handler for /{relType}.
// Story 9-29 extends it:
//   - Base route /relations/{eventId} (no relType) — fixes Element Web 404.
//   - Three-segment route /relations/{eventId}/{relType}/{eventType}.
//   - dir query param: "b" (newest-first, default) or "f" (oldest-first).
//   - recurse query param: accepted without 400/500.
//   - Invalid dir ("dir=xyz") → 400 M_BAD_PARAM.
//   - Unauthenticated → 401 M_MISSING_TOKEN.
//
// Test strategy:
//   - mockGetRelationsCoreClient implements GetRelationsCoreClient.
//   - buildAuthedRelationsHandler wires JWTMiddleware around GetRelationsHandler.
//   - capturedReq on the mock lets tests inspect the gRPC request forwarded.

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

type mockGetRelationsCoreClient struct {
	resp        *pb.GetRelationsResponse
	err         error
	capturedReq *pb.GetRelationsRequest
}

func (m *mockGetRelationsCoreClient) GetRelations(_ context.Context, req *pb.GetRelationsRequest) (*pb.GetRelationsResponse, error) {
	m.capturedReq = req
	return m.resp, m.err
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// buildAuthedRelationsHandler wires JWTMiddleware → GetRelationsHandler for the
// three-segment route so all path values are available.
func buildAuthedRelationsHandler(t *testing.T, mock *mockGetRelationsCoreClient) (http.Handler, *httptest.Server, func() string) {
	t.Helper()

	oidcSrv, privateKey := setupOIDCServer(t)
	t.Cleanup(oidcSrv.Close)

	provider := auth.NewProvider(context.Background(), oidcSrv.URL)

	handler := NewGetRelationsHandler(GetRelationsConfig{
		CoreClient: mock,
	})

	authed := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, "test.local")(
		http.HandlerFunc(handler.GetRelations),
	)

	// Register all three route variants so PathValue resolves correctly.
	mux := http.NewServeMux()
	mux.Handle("GET /_matrix/client/v1/rooms/{roomId}/relations/{eventId}", authed)
	mux.Handle("GET /_matrix/client/v1/rooms/{roomId}/relations/{eventId}/{relType}", authed)
	mux.Handle("GET /_matrix/client/v1/rooms/{roomId}/relations/{eventId}/{relType}/{eventType}", authed)

	makeToken := func() string {
		return signJWT(t, oidcSrv.URL, privateKey, time.Now().Add(time.Hour), nil)
	}
	return mux, oidcSrv, makeToken
}

// ─── Tests ────────────────────────────────────────────────────────────────────

// TestGetRelations_BaseRoute verifies that the base /relations/{eventId} route
// returns 200 and forwards rel_type="" to Core (empty = all relation types).
func TestGetRelations_BaseRoute(t *testing.T) {
	mock := &mockGetRelationsCoreClient{
		resp: &pb.GetRelationsResponse{
			Events: []*pb.Event{
				{EventId: "$reply1", EventType: "m.room.message", SenderId: "@alex:test.local", RoomId: "!room:test.local"},
			},
		},
	}

	mux, _, makeToken := buildAuthedRelationsHandler(t, mock)
	token := makeToken()

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v1/rooms/!room:test.local/relations/$root", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rr.Code, rr.Body.String())
	}

	// Verify rel_type is empty (no filter) and dir defaults to "b".
	if mock.capturedReq == nil {
		t.Fatal("no gRPC request captured")
	}
	if mock.capturedReq.RelType != "" {
		t.Errorf("expected RelType='', got %q", mock.capturedReq.RelType)
	}
	if mock.capturedReq.Dir != "b" {
		t.Errorf("expected Dir='b' (default), got %q", mock.capturedReq.Dir)
	}

	// Verify response has chunk array.
	var body struct {
		Chunk []json.RawMessage `json:"chunk"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Chunk) != 1 {
		t.Errorf("expected 1 chunk event, got %d", len(body.Chunk))
	}
}

// TestGetRelations_RelTypeRoute verifies that /relations/{eventId}/{relType}
// forwards rel_type correctly.
func TestGetRelations_RelTypeRoute(t *testing.T) {
	mock := &mockGetRelationsCoreClient{
		resp: &pb.GetRelationsResponse{Events: []*pb.Event{}},
	}

	mux, _, makeToken := buildAuthedRelationsHandler(t, mock)
	token := makeToken()

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v1/rooms/!room:test.local/relations/$root/m.thread", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rr.Code, rr.Body.String())
	}
	if mock.capturedReq.RelType != "m.thread" {
		t.Errorf("expected RelType='m.thread', got %q", mock.capturedReq.RelType)
	}
}

// TestGetRelations_ThreeSegmentRoute verifies that /relations/{eventId}/{relType}/{eventType}
// forwards both rel_type and event_type correctly.
func TestGetRelations_ThreeSegmentRoute(t *testing.T) {
	mock := &mockGetRelationsCoreClient{
		resp: &pb.GetRelationsResponse{Events: []*pb.Event{}},
	}

	mux, _, makeToken := buildAuthedRelationsHandler(t, mock)
	token := makeToken()

	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/client/v1/rooms/!room:test.local/relations/$root/m.thread/m.room.message", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rr.Code, rr.Body.String())
	}
	if mock.capturedReq.RelType != "m.thread" {
		t.Errorf("expected RelType='m.thread', got %q", mock.capturedReq.RelType)
	}
	if mock.capturedReq.EventType != "m.room.message" {
		t.Errorf("expected EventType='m.room.message', got %q", mock.capturedReq.EventType)
	}
}

// TestGetRelations_DirB verifies that dir=b is forwarded to Core.
func TestGetRelations_DirB(t *testing.T) {
	mock := &mockGetRelationsCoreClient{
		resp: &pb.GetRelationsResponse{Events: []*pb.Event{}},
	}

	mux, _, makeToken := buildAuthedRelationsHandler(t, mock)
	token := makeToken()

	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/client/v1/rooms/!room:test.local/relations/$root/m.thread?dir=b", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rr.Code, rr.Body.String())
	}
	if mock.capturedReq.Dir != "b" {
		t.Errorf("expected Dir='b', got %q", mock.capturedReq.Dir)
	}
}

// TestGetRelations_DirF verifies that dir=f is forwarded to Core.
func TestGetRelations_DirF(t *testing.T) {
	mock := &mockGetRelationsCoreClient{
		resp: &pb.GetRelationsResponse{Events: []*pb.Event{}},
	}

	mux, _, makeToken := buildAuthedRelationsHandler(t, mock)
	token := makeToken()

	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/client/v1/rooms/!room:test.local/relations/$root/m.thread?dir=f", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rr.Code, rr.Body.String())
	}
	if mock.capturedReq.Dir != "f" {
		t.Errorf("expected Dir='f', got %q", mock.capturedReq.Dir)
	}
}

// TestGetRelations_InvalidDir verifies that dir=invalid returns 400 M_BAD_PARAM.
func TestGetRelations_InvalidDir(t *testing.T) {
	mock := &mockGetRelationsCoreClient{
		resp: &pb.GetRelationsResponse{Events: []*pb.Event{}},
	}

	mux, _, makeToken := buildAuthedRelationsHandler(t, mock)
	token := makeToken()

	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/client/v1/rooms/!room:test.local/relations/$root/m.thread?dir=invalid", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (body: %s)", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "M_BAD_PARAM") {
		t.Errorf("expected M_BAD_PARAM in body, got: %s", rr.Body.String())
	}
}

// TestGetRelations_RecurseTrue verifies that recurse=true is accepted without 400/500.
func TestGetRelations_RecurseTrue(t *testing.T) {
	mock := &mockGetRelationsCoreClient{
		resp: &pb.GetRelationsResponse{Events: []*pb.Event{}},
	}

	mux, _, makeToken := buildAuthedRelationsHandler(t, mock)
	token := makeToken()

	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/client/v1/rooms/!room:test.local/relations/$root/m.thread?recurse=true", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 (recurse=true must be accepted), got %d (body: %s)", rr.Code, rr.Body.String())
	}
	if !mock.capturedReq.Recurse {
		t.Errorf("expected Recurse=true to be forwarded, got Recurse=false")
	}
}

// TestGetRelations_Unauthenticated verifies that a request without a token returns 401.
func TestGetRelations_Unauthenticated(t *testing.T) {
	mock := &mockGetRelationsCoreClient{}

	mux, _, _ := buildAuthedRelationsHandler(t, mock)

	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/client/v1/rooms/!room:test.local/relations/$root", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d (body: %s)", rr.Code, rr.Body.String())
	}
}

// TestGetRelations_Forbidden verifies that PERMISSION_DENIED from Core returns 403 M_FORBIDDEN.
func TestGetRelations_Forbidden(t *testing.T) {
	mock := &mockGetRelationsCoreClient{
		err: status.Error(codes.PermissionDenied, "not a member"),
	}

	mux, _, makeToken := buildAuthedRelationsHandler(t, mock)
	token := makeToken()

	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/client/v1/rooms/!room:test.local/relations/$root", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d (body: %s)", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "M_FORBIDDEN") {
		t.Errorf("expected M_FORBIDDEN, got: %s", rr.Body.String())
	}
}

// TestGetRelations_NotFound verifies that NOT_FOUND from Core returns 404 M_NOT_FOUND.
func TestGetRelations_NotFound(t *testing.T) {
	mock := &mockGetRelationsCoreClient{
		err: status.Error(codes.NotFound, "event not found"),
	}

	mux, _, makeToken := buildAuthedRelationsHandler(t, mock)
	token := makeToken()

	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/client/v1/rooms/!room:test.local/relations/$unknown_event", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (body: %s)", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "M_NOT_FOUND") {
		t.Errorf("expected M_NOT_FOUND, got: %s", rr.Body.String())
	}
}

// TestGetRelations_PrevBatch verifies that prev_batch from Core is included in the response.
func TestGetRelations_PrevBatch(t *testing.T) {
	mock := &mockGetRelationsCoreClient{
		resp: &pb.GetRelationsResponse{
			Events:    []*pb.Event{},
			PrevBatch: "token_abc123",
		},
	}

	mux, _, makeToken := buildAuthedRelationsHandler(t, mock)
	token := makeToken()

	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/client/v1/rooms/!room:test.local/relations/$root?dir=b", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "token_abc123") {
		t.Errorf("expected prev_batch='token_abc123' in response, got: %s", rr.Body.String())
	}
}

// TestGetRelations_InvalidRecurse verifies that recurse=garbage returns 400 M_BAD_PARAM.
func TestGetRelations_InvalidRecurse(t *testing.T) {
	mock := &mockGetRelationsCoreClient{
		resp: &pb.GetRelationsResponse{Events: []*pb.Event{}},
	}

	mux, _, makeToken := buildAuthedRelationsHandler(t, mock)
	token := makeToken()

	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/client/v1/rooms/!room:test.local/relations/$root/m.thread?recurse=banana", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (body: %s)", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "M_BAD_PARAM") {
		t.Errorf("expected M_BAD_PARAM in body, got: %s", rr.Body.String())
	}
}

// TestGetRelations_DefaultLimit verifies that an omitted limit param defaults to 20.
func TestGetRelations_DefaultLimit(t *testing.T) {
	mock := &mockGetRelationsCoreClient{
		resp: &pb.GetRelationsResponse{Events: []*pb.Event{}},
	}

	mux, _, makeToken := buildAuthedRelationsHandler(t, mock)
	token := makeToken()

	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/client/v1/rooms/!room:test.local/relations/$root", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if mock.capturedReq.Limit != 20 {
		t.Errorf("expected Limit=20 (default), got %d", mock.capturedReq.Limit)
	}
}
