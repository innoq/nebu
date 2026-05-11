package matrix

// ─── Story 11-8: GET /_matrix/client/v3/rooms/{roomId}/event/{eventId} ────────
//
// Unit tests for GetEventHandler.
// Tests are written FIRST (red phase) before Core gRPC implementation.
//
// Bug: GET /_matrix/client/v3/rooms/{roomId}/event/{eventId} returns 404 (not registered).
//      GET /_matrix/client/v1/rooms/{roomId}/relations/{eventId}?dir=b&recurse=true returns 500.
//      Element calls these endpoints when loading threads.
//
// Test strategy:
//   - mockGetEventCoreClient implements GetEventCoreClient.
//   - buildAuthedEventHandler wires JWTMiddleware around GetEventHandler.
//   - Tests verify HTTP status codes and JSON response shape.

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

type mockGetEventCoreClient struct {
	resp        *pb.GetEventResponse
	err         error
	capturedReq *pb.GetEventRequest
}

func (m *mockGetEventCoreClient) GetEvent(_ context.Context, req *pb.GetEventRequest) (*pb.GetEventResponse, error) {
	m.capturedReq = req
	return m.resp, m.err
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func buildAuthedEventHandler(t *testing.T, mock *mockGetEventCoreClient) (http.Handler, func() string) {
	t.Helper()

	oidcSrv, privateKey := setupOIDCServer(t)
	t.Cleanup(oidcSrv.Close)

	provider := auth.NewProvider(context.Background(), oidcSrv.URL)

	handler := NewGetEventHandler(GetEventConfig{CoreClient: mock})

	authed := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, "test.local")(
		http.HandlerFunc(handler.GetEvent),
	)

	mux := http.NewServeMux()
	mux.Handle("GET /_matrix/client/v3/rooms/{roomId}/event/{eventId}", authed)

	makeToken := func() string {
		return signJWT(t, oidcSrv.URL, privateKey, time.Now().Add(time.Hour), nil)
	}
	return mux, makeToken
}

// ─── Tests ────────────────────────────────────────────────────────────────────

// TestGetEvent_HappyPath verifies that a valid event is returned as JSON with status 200.
// AC1: endpoint is registered and returns the event.
func TestGetEvent_HappyPath(t *testing.T) {
	mock := &mockGetEventCoreClient{
		resp: &pb.GetEventResponse{
			Event: &pb.Event{
				EventId:   "$test_event_id",
				RoomId:    "!room:test.local",
				SenderId:  "@alice:test.local",
				EventType: "m.room.message",
				Content:   []byte(`{"msgtype":"m.text","body":"hello"}`),
				OriginTs:  1700000000000,
			},
		},
	}

	mux, makeToken := buildAuthedEventHandler(t, mock)
	token := makeToken()

	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/client/v3/rooms/!room:test.local/event/$test_event_id", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rr.Code, rr.Body.String())
	}

	var body struct {
		EventID   string         `json:"event_id"`
		RoomID    string         `json:"room_id"`
		Sender    string         `json:"sender"`
		Type      string         `json:"type"`
		Content   map[string]any `json:"content"`
		OriginTS  int64          `json:"origin_server_ts"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.EventID != "$test_event_id" {
		t.Errorf("expected event_id=$test_event_id, got %q", body.EventID)
	}
	if body.RoomID != "!room:test.local" {
		t.Errorf("expected room_id=!room:test.local, got %q", body.RoomID)
	}
	if body.Sender != "@alice:test.local" {
		t.Errorf("expected sender=@alice:test.local, got %q", body.Sender)
	}
	if body.Type != "m.room.message" {
		t.Errorf("expected type=m.room.message, got %q", body.Type)
	}
	if body.OriginTS != 1700000000000 {
		t.Errorf("expected origin_server_ts=1700000000000, got %d", body.OriginTS)
	}
	if body.Content["body"] != "hello" {
		t.Errorf("expected content.body=hello, got %v", body.Content["body"])
	}

	// gRPC request should have user_id, room_id, event_id set.
	if mock.capturedReq == nil {
		t.Fatal("no gRPC request captured")
	}
	if mock.capturedReq.RoomId != "!room:test.local" {
		t.Errorf("gRPC RoomId: expected !room:test.local, got %q", mock.capturedReq.RoomId)
	}
	if mock.capturedReq.EventId != "$test_event_id" {
		t.Errorf("gRPC EventId: expected $test_event_id, got %q", mock.capturedReq.EventId)
	}
}

// TestGetEvent_NotFound verifies that NOT_FOUND from Core returns 404 M_NOT_FOUND.
// AC2: unknown event_id returns 404.
func TestGetEvent_NotFound(t *testing.T) {
	mock := &mockGetEventCoreClient{
		err: status.Error(codes.NotFound, "event not found"),
	}

	mux, makeToken := buildAuthedEventHandler(t, mock)
	token := makeToken()

	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/client/v3/rooms/!room:test.local/event/$unknown_event", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (body: %s)", rr.Code, rr.Body.String())
	}
	var body struct{ Errcode string `json:"errcode"` }
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Errcode != "M_NOT_FOUND" {
		t.Errorf("expected M_NOT_FOUND, got %q", body.Errcode)
	}
}

// TestGetEvent_Forbidden verifies that PERMISSION_DENIED from Core returns 403 M_FORBIDDEN.
// AC3: non-member gets 403.
func TestGetEvent_Forbidden(t *testing.T) {
	mock := &mockGetEventCoreClient{
		err: status.Error(codes.PermissionDenied, "not a member"),
	}

	mux, makeToken := buildAuthedEventHandler(t, mock)
	token := makeToken()

	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/client/v3/rooms/!room:test.local/event/$some_event", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d (body: %s)", rr.Code, rr.Body.String())
	}
	var body struct{ Errcode string `json:"errcode"` }
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Errcode != "M_FORBIDDEN" {
		t.Errorf("expected M_FORBIDDEN, got %q", body.Errcode)
	}
}

// TestGetEvent_Unauthenticated verifies that a request without a token returns 401.
// AC4: missing auth token returns 401.
func TestGetEvent_Unauthenticated(t *testing.T) {
	mock := &mockGetEventCoreClient{}
	mux, _ := buildAuthedEventHandler(t, mock)

	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/client/v3/rooms/!room:test.local/event/$some_event", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d (body: %s)", rr.Code, rr.Body.String())
	}
	var body struct{ Errcode string `json:"errcode"` }
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode 401 body: %v", err)
	}
	if body.Errcode != "M_MISSING_TOKEN" {
		t.Errorf("expected M_MISSING_TOKEN, got %q", body.Errcode)
	}
}

// TestGetEvent_ContentFieldsPresent verifies that m.relates_to is preserved in the content JSON.
// AC1 (supplementary): ensures content JSON is forwarded verbatim, including thread relation metadata.
func TestGetEvent_ContentFieldsPresent(t *testing.T) {
	mock := &mockGetEventCoreClient{
		resp: &pb.GetEventResponse{
			Event: &pb.Event{
				EventId:   "$abc123",
				RoomId:    "!room:test.local",
				SenderId:  "@bob:test.local",
				EventType: "m.room.message",
				Content:   []byte(`{"msgtype":"m.text","body":"world","m.relates_to":{"rel_type":"m.thread","event_id":"$root"}}`),
				OriginTs:  1234567890000,
				StateKey:  "",
			},
		},
	}

	mux, makeToken := buildAuthedEventHandler(t, mock)
	token := makeToken()

	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/client/v3/rooms/!room:test.local/event/$abc123", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rr.Code, rr.Body.String())
	}

	// Verify m.relates_to is preserved in the content.
	var body struct {
		Content map[string]any `json:"content"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Content["m.relates_to"] == nil {
		t.Errorf("expected m.relates_to in content, got: %v", body.Content)
	}
}
