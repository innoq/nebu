package matrix

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	pb "github.com/nebu/nebu/internal/grpc/pb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Story 7-28: Event Context — GET /_matrix/client/v3/rooms/{roomId}/context/{eventId}
//
// AC1 — 200 with event, events_before, events_after, start, end, state
// AC2 — limit defaults to 10; clamped to 1–100
// AC3 — 404 M_NOT_FOUND for unknown eventId
// AC4 — 403 M_FORBIDDEN for non-member; 401 without JWT
// AC5 — Invalid roomId or eventId → 400 M_INVALID_PARAM

type mockEventContextCoreClient struct {
	resp *pb.GetEventContextResponse
	err  error
}

func (m *mockEventContextCoreClient) GetEventContext(_ context.Context, _ *pb.GetEventContextRequest) (*pb.GetEventContextResponse, error) {
	return m.resp, m.err
}

func newEventContextTestMux(mock *mockEventContextCoreClient) *http.ServeMux {
	h := NewGetEventContextHandler(GetEventContextConfig{CoreClient: mock})
	mux := http.NewServeMux()
	mux.Handle("GET /rooms/{roomId}/context/{eventId}", http.HandlerFunc(h.GetEventContext))
	return mux
}

func makeEventContextRequest(mux *http.ServeMux, roomID, eventID, query, token string) *httptest.ResponseRecorder {
	path := "/rooms/" + roomID + "/context/" + eventID
	if query != "" {
		path += "?" + query
	}
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req = req.WithContext(contextWithUser(req.Context(), token, "user"))
	}
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	return rr
}

func TestGetEventContext_HappyPath(t *testing.T) {
	mock := &mockEventContextCoreClient{
		resp: &pb.GetEventContextResponse{
			StartToken: "t1",
			EndToken:   "t2",
			Event:      &pb.Event{EventId: "$evt1:nebu.local", EventType: "m.room.message", SenderId: "@kai:nebu.local"},
			EventsBefore: []*pb.Event{
				{EventId: "$evt0:nebu.local", EventType: "m.room.message", SenderId: "@kai:nebu.local"},
			},
			EventsAfter: []*pb.Event{},
			State:       []*pb.SyncRoomStateEvent{},
		},
	}
	mux := newEventContextTestMux(mock)
	rr := makeEventContextRequest(mux, "!room:nebu.local", "$evt1:nebu.local", "limit=3", "@kai:nebu.local")

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, field := range []string{"start", "end", "event", "events_before", "events_after", "state"} {
		if _, ok := body[field]; !ok {
			t.Errorf("missing field %q in response", field)
		}
	}
}

func TestGetEventContext_NotFound(t *testing.T) {
	mock := &mockEventContextCoreClient{err: status.Error(codes.NotFound, "event not found")}
	mux := newEventContextTestMux(mock)
	rr := makeEventContextRequest(mux, "!room:nebu.local", "$missing:nebu.local", "", "@kai:nebu.local")

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
	if body := rr.Body.String(); !strings.Contains(body, "M_NOT_FOUND") {
		t.Errorf("expected M_NOT_FOUND, got: %s", body)
	}
}

func TestGetEventContext_Forbidden(t *testing.T) {
	mock := &mockEventContextCoreClient{err: status.Error(codes.PermissionDenied, "not a member")}
	mux := newEventContextTestMux(mock)
	rr := makeEventContextRequest(mux, "!room:nebu.local", "$evt:nebu.local", "", "@marie:nebu.local")

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestGetEventContext_Unauthenticated(t *testing.T) {
	mock := &mockEventContextCoreClient{}
	mux := newEventContextTestMux(mock)
	rr := makeEventContextRequest(mux, "!room:nebu.local", "$evt:nebu.local", "", "")

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestGetEventContext_LimitClamped(t *testing.T) {
	mock := &mockEventContextCoreClient{
		resp: &pb.GetEventContextResponse{
			StartToken: "t1", EndToken: "t2",
			Event:        &pb.Event{EventId: "$evt:nebu.local", EventType: "m.room.message", SenderId: "@kai:nebu.local"},
			EventsBefore: []*pb.Event{}, EventsAfter: []*pb.Event{}, State: []*pb.SyncRoomStateEvent{},
		},
	}
	mux := newEventContextTestMux(mock)
	rr := makeEventContextRequest(mux, "!room:nebu.local", "$evt:nebu.local", "limit=200", "@kai:nebu.local")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 with clamped limit, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestGetEventContext_InvalidRoomID(t *testing.T) {
	mock := &mockEventContextCoreClient{}
	mux := newEventContextTestMux(mock)
	rr := makeEventContextRequest(mux, "notaroomid", "$evt:nebu.local", "", "@kai:nebu.local")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid roomId, got %d", rr.Code)
	}
}
