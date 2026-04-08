package buffer

// Story 4-16: message_buffer Drain Strategy — Linear MVP
//
// Integration test for the EventBus drain routing helper (fanout.go).
//
// These tests are written FIRST (ATDD gate), before implementation exists.
// ALL tests in this file are expected to FAIL until fanout.go is implemented.
//
// Tests cover:
//   - RouteEventToUsers calls buf.Put for every member of the event's room
//   - RouteEventToUsers skips routing when RoomStateLookup returns an error

import (
	"context"
	"errors"
	"testing"

	pb "github.com/nebu/nebu/internal/grpc/pb"
	"github.com/prometheus/client_golang/prometheus"
)

// ─── Stub RoomStateLookup ─────────────────────────────────────────────────────

// stubRoomState implements the RoomStateLookup interface expected by
// RouteEventToUsers (defined in fanout.go). It maps room IDs to member lists.
type stubRoomState struct {
	members map[string][]string
	err     error
}

func (s *stubRoomState) GetRoomState(_ context.Context, roomID string) ([]string, error) {
	if s.err != nil {
		return nil, s.err
	}
	members, ok := s.members[roomID]
	if !ok {
		return nil, errors.New("room not found")
	}
	return members, nil
}

// ─── AC #3 (story): EventBus routing calls Put for each member ───────────────

// TestDrainRouter_RoutesEventToUser verifies that RouteEventToUsers calls
// buf.Put for every member of the event's room, so each member's ring buffer
// receives the event.
//
// Fails until RouteEventToUsers and RoomStateLookup are implemented in fanout.go.
func TestDrainRouter_RoutesEventToUser(t *testing.T) {
	reg := prometheus.NewRegistry()
	buf := NewMessageBuffer(10, reg)

	event := &pb.Event{
		EventId:   "$route1",
		RoomId:    "!room:test.local",
		SenderId:  "@sender:test.local",
		EventType: "m.room.message",
	}

	members := []string{"@alice:test.local", "@bob:test.local"}
	lookup := &stubRoomState{
		members: map[string][]string{
			"!room:test.local": members,
		},
	}

	ctx := context.Background()
	RouteEventToUsers(ctx, event, buf, lookup)

	// Alice must have received the event.
	aliceEvents := buf.DrainFor("@alice:test.local", 10)
	if len(aliceEvents) != 1 {
		t.Errorf("alice: got %d events, want 1", len(aliceEvents))
	} else if aliceEvents[0].EventId != "$route1" {
		t.Errorf("alice event: got %q, want $route1", aliceEvents[0].EventId)
	}

	// Bob must have received the event.
	bobEvents := buf.DrainFor("@bob:test.local", 10)
	if len(bobEvents) != 1 {
		t.Errorf("bob: got %d events, want 1", len(bobEvents))
	} else if bobEvents[0].EventId != "$route1" {
		t.Errorf("bob event: got %q, want $route1", bobEvents[0].EventId)
	}
}

// TestDrainRouter_SkipsOnGetRoomStateError verifies that RouteEventToUsers
// does NOT panic and does NOT put any events when GetRoomState returns an error.
// It must log a warning and skip the event silently.
//
// Fails until RouteEventToUsers is implemented in fanout.go.
func TestDrainRouter_SkipsOnGetRoomStateError(t *testing.T) {
	reg := prometheus.NewRegistry()
	buf := NewMessageBuffer(10, reg)

	event := &pb.Event{
		EventId: "$unreachable",
		RoomId:  "!ghost:test.local",
	}

	lookup := &stubRoomState{
		err: errors.New("core unavailable"),
	}

	ctx := context.Background()

	// Must not panic.
	RouteEventToUsers(ctx, event, buf, lookup)

	// No events should have been routed to any user.
	// We verify by checking a couple of candidate user IDs (there are none
	// defined, but we can still call DrainFor safely to confirm it returns empty).
	got := buf.DrainFor("@anyuser:test.local", 10)
	if len(got) != 0 {
		t.Errorf("expected 0 events after GetRoomState error, got %d", len(got))
	}
}
