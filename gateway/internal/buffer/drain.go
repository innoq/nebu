package buffer

import (
	"context"
	"log/slog"

	pb "github.com/nebu/nebu/internal/grpc/pb"
)

// DrainStrategy determines the maximum event drain rate.
// MVP: only LinearStrategy (ADR-006); AIMD is Phase 2.
type DrainStrategy interface {
	// Rate returns the drain rate in messages/second.
	// loadFactor is 0.0–1.0 from Core /health endpoint (MVP: always pass 1.0).
	// bufferSize is the current total events across all users.
	Rate(loadFactor float64, bufferSize int64) float64
}

// RoomStateLookup is a consumer-defined interface for resolving room membership.
// It is implemented by *coregrpc.Client (GetRoomState) and by stubs in tests.
type RoomStateLookup interface {
	// GetRoomState returns the member list for roomID.
	// Returns an error when the room is not found or Core is unavailable.
	GetRoomState(ctx context.Context, roomID string) ([]string, error)
}

// RouteEventToUsers resolves room membership via lookup and calls buf.Put for
// each member of event.RoomId.
//
// If GetRoomState fails (Core unavailable, room not found), a warning is logged
// and the event is skipped — the function never panics or returns an error.
func RouteEventToUsers(ctx context.Context, event *pb.Event, buf *MessageBuffer, lookup RoomStateLookup) {
	members, err := lookup.GetRoomState(ctx, event.RoomId)
	if err != nil {
		slog.Warn("RouteEventToUsers: GetRoomState failed, skipping event routing",
			"room_id", event.RoomId,
			"event_id", event.EventId,
			"err", err)
		return
	}
	for _, memberID := range members {
		buf.Put(memberID, event)
	}
}
