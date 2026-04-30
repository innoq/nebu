// Story 7-28: GetEventContext — hand-written extension to the generated pb package.
// These message types mirror the proto definitions in proto/core.proto and will be
// replaced by the auto-generated versions once `make proto` is run after the Elixir
// gRPC handler is implemented.
//
// NOTE: Do not add protobuf reflection machinery here; these are plain Go structs
// used only for the Go Gateway ↔ Elixir Core boundary.

package pb

// GetEventContextRequest is the gRPC request for the GetEventContext RPC.
// Story 7-28: GET /_matrix/client/v3/rooms/{roomId}/context/{eventId}.
type GetEventContextRequest struct {
	// RoomId is the Matrix room ID.
	RoomId string `json:"room_id"`
	// EventId is the target event whose context is requested.
	EventId string `json:"event_id"`
	// Limit is the maximum number of events before AND after the target event.
	// 0 = server default (10); server caps at 100.
	Limit int32 `json:"limit,omitempty"`
}

// GetEventContextResponse is the gRPC response for the GetEventContext RPC.
type GetEventContextResponse struct {
	// Event is the target event itself.
	Event *Event `json:"event,omitempty"`
	// EventsBefore contains up to Limit events older than the target, newest last.
	EventsBefore []*Event `json:"events_before,omitempty"`
	// EventsAfter contains up to Limit events newer than the target, oldest first.
	EventsAfter []*Event `json:"events_after,omitempty"`
	// State is the room state snapshot at the time of the target event.
	// Uses SyncRoomStateEvent which already carries state_key.
	State []*SyncRoomStateEvent `json:"state,omitempty"`
	// StartToken is a pagination token usable as `to` in GET /rooms/{roomId}/messages.
	StartToken string `json:"start_token,omitempty"`
	// EndToken is a pagination token usable as `from` in GET /rooms/{roomId}/messages.
	EndToken string `json:"end_token,omitempty"`
}
