package matrix

// allowedStateEventTypes is the complete whitelist of Matrix state event types
// the gateway accepts on PUT /rooms/{roomId}/state/{eventType}.
//
// Any event type NOT present here is rejected at the gateway with 400 M_BAD_JSON
// before the request reaches Core. This is a single authoritative source —
// add new Matrix-spec-mandated types here and nowhere else (Story 9-6, AC4).
//
// Source: Matrix Client-Server API v1.18, Section 11 (Room State Events).
// Custom/vendor event types (com.*, io.*, etc.) are intentionally excluded.
var allowedStateEventTypes = map[string]bool{
	"m.room.create":             true,
	"m.room.join_rules":         true,
	"m.room.member":             true,
	"m.room.power_levels":       true,
	"m.room.name":               true,
	"m.room.topic":              true,
	"m.room.avatar":             true,
	"m.room.canonical_alias":    true,
	"m.room.encryption":         true, // pass-through per Matrix spec Section 11.2.1
	"m.room.history_visibility": true,
	"m.room.guest_access":       true,
	"m.room.server_acl":         true,
	"m.room.tombstone":          true,
	"m.room.pinned_events":      true,
	"m.space.child":             true,
	"m.space.parent":            true,
}
