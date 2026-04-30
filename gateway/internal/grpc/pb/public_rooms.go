// Story 7-27: ListPublicRooms — hand-written extension to the generated pb package.
// These message types mirror the proto definitions in proto/core.proto and will be
// replaced by the auto-generated versions once `make proto` is run after the Elixir
// gRPC handler is implemented.
//
// NOTE: Do not add protobuf reflection machinery here; these are plain Go structs
// used only for the Go Gateway ↔ Elixir Core boundary.

package pb

// ListPublicRoomsRequest is the gRPC request for the ListPublicRooms RPC.
// Story 7-27: GET/POST /_matrix/client/v3/publicRooms.
type ListPublicRoomsRequest struct {
	// Limit is the maximum number of rooms to return per page.
	// 0 = server default (20); server caps at 100.
	Limit int32 `json:"limit,omitempty"`
	// Since is the opaque cursor from the previous page's NextCursor.
	// Empty string = first page.
	Since string `json:"since,omitempty"`
	// FilterTerm is an optional case-insensitive substring to match against
	// room name and topic (ILIKE '%term%').
	FilterTerm string `json:"filter_term,omitempty"`
}

// RoomSummary is a single public room entry in the ListPublicRooms response.
type RoomSummary struct {
	// RoomId is the Matrix room ID (e.g. "!abc:server").
	RoomId string `json:"room_id"`
	// Name is the display name of the room.
	Name string `json:"name,omitempty"`
	// Topic is the room topic (omitted when empty).
	Topic string `json:"topic,omitempty"`
	// NumJoinedMembers is the live member count (from Room GenServer or DB fallback).
	NumJoinedMembers int32 `json:"num_joined_members"`
	// WorldReadable is always false for Nebu rooms (no anonymous read access).
	WorldReadable bool `json:"world_readable"`
	// GuestCanJoin is always false for Nebu rooms.
	GuestCanJoin bool `json:"guest_can_join"`
}

// ListPublicRoomsResponse is the gRPC response for the ListPublicRooms RPC.
type ListPublicRoomsResponse struct {
	// Rooms is the list of public room summaries for this page.
	Rooms []*RoomSummary `json:"rooms"`
	// NextCursor is the opaque pagination cursor for the next page.
	// Empty when there are no more pages.
	NextCursor string `json:"next_cursor,omitempty"`
	// TotalEstimate is an approximate count of all public rooms
	// (ignores filter; fast COUNT(*) from the DB).
	TotalEstimate int32 `json:"total_estimate"`
}
