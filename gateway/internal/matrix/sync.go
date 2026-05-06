package matrix

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/nebu/nebu/internal/buffer"
	coregrpc "github.com/nebu/nebu/internal/grpc"
	pb "github.com/nebu/nebu/internal/grpc/pb"
	"github.com/nebu/nebu/internal/middleware"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GetSyncCoreClient is the consumer-defined interface for the sync gRPC calls.
// Extended in Story 4-15 to include GetSyncDelta.
// Keep it minimal — only what this handler needs (Go interface convention, ADR-009).
type GetSyncCoreClient interface {
	GetInitialSync(ctx context.Context, req *pb.GetInitialSyncRequest) (*pb.GetInitialSyncResponse, error)
	GetSyncDelta(ctx context.Context, req *pb.GetSyncDeltaRequest) (*pb.GetSyncDeltaResponse, error)
}

// GetSyncHandler handles GET /_matrix/client/v3/sync.
type GetSyncHandler struct {
	coreClient          GetSyncCoreClient
	serverName          string
	timeout             time.Duration
	buffer              *buffer.MessageBuffer  // Story 4-16: local event buffer (nil = disabled)
	db                  *sql.DB                // for pending invite queries
	accountDataDB       AccountDataDB          // Story 7-24: per-room account data (nil = disabled)
	globalAccountDataDB GlobalAccountDataDB    // Story 9-24: top-level global account data (nil = disabled)
}

// GetSyncConfig holds dependencies for NewGetSyncHandler.
type GetSyncConfig struct {
	CoreClient          GetSyncCoreClient
	ServerName          string
	Timeout             time.Duration         // gRPC call timeout; defaults to 5s if zero
	Buffer              *buffer.MessageBuffer // Story 4-16: optional local event buffer
	DB                  *sql.DB               // optional: enables rooms.invite in sync response
	AccountDataDB       AccountDataDB         // Story 7-24: optional, enables account_data in sync response
	GlobalAccountDataDB GlobalAccountDataDB   // Story 9-24: optional, enables top-level account_data in sync response
}

// NewGetSyncHandler constructs a GetSyncHandler from the provided config.
func NewGetSyncHandler(cfg GetSyncConfig) *GetSyncHandler {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	return &GetSyncHandler{
		coreClient:          cfg.CoreClient,
		serverName:          cfg.ServerName,
		timeout:             timeout,
		buffer:              cfg.Buffer,
		db:                  cfg.DB,
		accountDataDB:       cfg.AccountDataDB,
		globalAccountDataDB: cfg.GlobalAccountDataDB,
	}
}

// queryForgottenRoomIDs returns the set of room IDs the user has forgotten (GAP-FORGET).
// Used to exclude forgotten rooms from rooms.join so they don't resurface after POST /forget.
func (h *GetSyncHandler) queryForgottenRoomIDs(ctx context.Context, userID string) map[string]struct{} {
	forgotten := map[string]struct{}{}
	if h.db == nil {
		return forgotten
	}
	rows, err := h.db.QueryContext(ctx,
		`SELECT room_id FROM forgotten_rooms WHERE user_id = $1`, userID)
	if err != nil {
		return forgotten
	}
	defer rows.Close()
	for rows.Next() {
		var roomID string
		if err := rows.Scan(&roomID); err == nil {
			forgotten[roomID] = struct{}{}
		}
	}
	return forgotten
}

// querySinceTsMs looks up the wall-clock timestamp (Unix ms) of the user's
// last persisted sync token. Used to filter buildLeaveRooms so left rooms
// only appear in the first sync after the leave event, not repeatedly (GAP-LEAVE-ONCE).
func (h *GetSyncHandler) querySinceTsMs(ctx context.Context, userID string) int64 {
	if h.db == nil {
		return 0
	}
	var updatedAt int64
	if err := h.db.QueryRowContext(ctx,
		`SELECT updated_at FROM sync_tokens WHERE user_id = $1`, userID,
	).Scan(&updatedAt); err != nil {
		return 0
	}
	return updatedAt
}

// buildLeaveRooms queries rooms the user has left or declined and builds the
// rooms.leave section of the Matrix sync response.
// Includes: rooms where left_at IS NOT NULL (user left after joining) and
// rooms where the invitation was rejected (rejected_at IS NOT NULL).
// Element Web uses rooms.leave to remove rooms from its local state — without
// it, declined invitations and left rooms remain visible in the UI indefinitely.
//
// sinceMs filters results to only rooms left/rejected after the given Unix-ms
// timestamp. Pass 0 for initial sync (no filter). This prevents left rooms from
// appearing in every subsequent incremental sync (GAP-LEAVE-ONCE).
//
// Fix-1: For each left/rejected room, the most recent m.room.member leave event
// is fetched from the events table and included in state.events. If no event is
// found (e.g. rooms created before this fix), state.events degrades to empty.
//
// Fix-2 (GAP-FORGET): rooms the user has forgotten are excluded via the
// forgotten_rooms subquery.
func (h *GetSyncHandler) buildLeaveRooms(ctx context.Context, userID string, sinceMs int64) map[string]interface{} {
	leaves := map[string]interface{}{}
	if h.db == nil {
		return leaves
	}
	slog.Debug("buildLeaveRooms called", "userID", userID, "sinceMs", sinceMs)

	// leaveEventQuery fetches the most recent m.room.member leave event for a given
	// room and user. Content may be stored as a JSONB object or a double-encoded
	// JSONB string — the CASE guard mirrors the pattern already used in buildInviteRooms.
	const leaveEventQuery = `
		SELECT
		    event_id,
		    sender,
		    CASE
		        WHEN jsonb_typeof(content) = 'object' THEN content::text
		        ELSE content#>>'{}'
		    END AS content_json,
		    origin_server_ts
		FROM events
		WHERE room_id = $1
		  AND event_type = 'm.room.member'
		  AND sender = $2
		  AND (
		    CASE
		        WHEN jsonb_typeof(content) = 'object' THEN content->>'membership'
		        ELSE ((content#>>'{}')::jsonb)->>'membership'
		    END
		  ) = 'leave'
		ORDER BY origin_server_ts DESC
		LIMIT 1`

	// buildStateEvents queries the events table for a leave event and returns the
	// state.events slice to include in the leave room entry.
	// Degrades gracefully to an empty slice if no event is found or the query fails.
	buildStateEvents := func(roomID string) []map[string]interface{} {
		stateEvents := []map[string]interface{}{}
		row := h.db.QueryRowContext(ctx, leaveEventQuery, roomID, userID)
		var eventID, sender, contentJSON string
		var originTS int64
		if err := row.Scan(&eventID, &sender, &contentJSON, &originTS); err != nil {
			// sql.ErrNoRows is the expected case for graceful degradation — no log needed.
			// Any other error is also silently degraded to empty state.events (no crash).
			return stateEvents
		}
		stateEvents = append(stateEvents, map[string]interface{}{
			"type":      "m.room.member",
			"state_key": userID,
			"sender":    sender,
			"content":   json.RawMessage(contentJSON),
		})
		return stateEvents
	}

	// Rooms the user has left (was a member, now has left_at set).
	// GAP-LEAVE-ONCE: only return rooms left after sinceMs (0 = no filter for initial sync).
	// GAP-FORGET: exclude rooms the user has forgotten.
	leftRows, err := h.db.QueryContext(ctx,
		`SELECT room_id FROM room_members
		 WHERE user_id = $1
		   AND left_at IS NOT NULL
		   AND left_at >= $2
		   AND room_id NOT IN (SELECT room_id FROM forgotten_rooms WHERE user_id = $1)`,
		userID, sinceMs)
	if err == nil {
		defer leftRows.Close()
		for leftRows.Next() {
			var roomID string
			if err := leftRows.Scan(&roomID); err == nil {
				slog.Debug("buildLeaveRooms: found left room", "roomID", roomID, "userID", userID)
				stateEvents := buildStateEvents(roomID)
				leaves[roomID] = map[string]interface{}{
					"timeline":     map[string]interface{}{"events": []interface{}{}, "limited": false},
					"state":        map[string]interface{}{"events": stateEvents},
					"account_data": map[string]interface{}{"events": []interface{}{}},
				}
			}
		}
	} else {
		slog.Warn("buildLeaveRooms: query error", "err", err, "userID", userID, "sinceMs", sinceMs)
	}
	// Invitations the user has declined (rejected_at IS NOT NULL).
	// GAP-LEAVE-ONCE: only return rejections after sinceMs (0 = no filter for initial sync).
	// GAP-FORGET: exclude rooms the user has forgotten.
	rejRows, err := h.db.QueryContext(ctx,
		`SELECT room_id FROM room_invitations
		 WHERE invitee_id = $1
		   AND rejected_at IS NOT NULL
		   AND rejected_at >= $2
		   AND room_id NOT IN (SELECT room_id FROM forgotten_rooms WHERE user_id = $1)`,
		userID, sinceMs)
	if err == nil {
		defer rejRows.Close()
		for rejRows.Next() {
			var roomID string
			if err := rejRows.Scan(&roomID); err == nil {
				if _, already := leaves[roomID]; !already {
					stateEvents := buildStateEvents(roomID)
					leaves[roomID] = map[string]interface{}{
						"timeline":     map[string]interface{}{"events": []interface{}{}, "limited": false},
						"state":        map[string]interface{}{"events": stateEvents},
						"account_data": map[string]interface{}{"events": []interface{}{}},
					}
				}
			}
		}
	}
	return leaves
}

// buildInviteRooms queries pending room invitations for userID and builds the
// rooms.invite section of the Matrix sync response.
func (h *GetSyncHandler) buildInviteRooms(ctx context.Context, userID string) map[string]interface{} {
	invites := map[string]interface{}{}
	if h.db == nil {
		return invites
	}
	rows, err := h.db.QueryContext(ctx,
		`SELECT room_id, inviter_id FROM room_invitations
		 WHERE invitee_id = $1
		   AND accepted_at IS NULL
		   AND rejected_at IS NULL
		   AND room_id NOT IN (SELECT room_id FROM forgotten_rooms WHERE user_id = $1)`,
		userID)
	if err != nil {
		slog.Warn("buildInviteRooms: query failed", "err", err)
		return invites
	}
	defer rows.Close()
	for rows.Next() {
		var roomID, inviterID string
		if err := rows.Scan(&roomID, &inviterID); err != nil {
			continue
		}
		// Build invite_state with at least the membership event.
		// Also include m.room.name if available so the client can display
		// the room name in the invite tile (Matrix spec: stripped state).
		events := []map[string]interface{}{
			{
				"type":      "m.room.member",
				"sender":    inviterID,
				"state_key": userID,
				"content":   map[string]string{"membership": "invite"},
			},
		}
		var roomName string
		nameRow := h.db.QueryRowContext(ctx,
			`SELECT CASE
				WHEN jsonb_typeof(content) = 'object' THEN content->>'name'
				ELSE ((content#>>'{}')::jsonb)->>'name'
			 END
			 FROM events WHERE room_id = $1 AND event_type = 'm.room.name'
			 ORDER BY origin_server_ts DESC LIMIT 1`,
			roomID)
		if err := nameRow.Scan(&roomName); err == nil && roomName != "" {
			events = append(events, map[string]interface{}{
				"type":      "m.room.name",
				"sender":    inviterID,
				"state_key": "",
				"content":   map[string]string{"name": roomName},
			})
		}
		// m.room.join_rules — included per Matrix spec §4.4.4 stripped state
		var joinRule string
		joinRulesRow := h.db.QueryRowContext(ctx,
			`SELECT CASE
				WHEN jsonb_typeof(content) = 'object' THEN content->>'join_rule'
				ELSE ((content#>>'{}')::jsonb)->>'join_rule'
			 END
			 FROM events WHERE room_id = $1 AND event_type = 'm.room.join_rules'
			 ORDER BY origin_server_ts DESC LIMIT 1`,
			roomID)
		if err := joinRulesRow.Scan(&joinRule); err == nil && joinRule != "" {
			events = append(events, map[string]interface{}{
				"type":      "m.room.join_rules",
				"sender":    inviterID,
				"state_key": "",
				"content":   map[string]string{"join_rule": joinRule},
			})
		}
		// m.room.avatar — included per Matrix spec §4.4.4 stripped state
		// Omitted entirely when url is empty or missing (Element Web handles gracefully).
		var avatarURL string
		avatarRow := h.db.QueryRowContext(ctx,
			`SELECT CASE
				WHEN jsonb_typeof(content) = 'object' THEN content->>'url'
				ELSE ((content#>>'{}')::jsonb)->>'url'
			 END
			 FROM events WHERE room_id = $1 AND event_type = 'm.room.avatar'
			 ORDER BY origin_server_ts DESC LIMIT 1`,
			roomID)
		if err := avatarRow.Scan(&avatarURL); err == nil && avatarURL != "" {
			events = append(events, map[string]interface{}{
				"type":      "m.room.avatar",
				"sender":    inviterID,
				"state_key": "",
				"content":   map[string]string{"url": avatarURL},
			})
		}
		// m.room.create — included per Matrix spec §4.4.4 stripped state
		var roomCreator string
		createRow := h.db.QueryRowContext(ctx,
			`SELECT CASE
				WHEN jsonb_typeof(content) = 'object' THEN content->>'creator'
				ELSE ((content#>>'{}')::jsonb)->>'creator'
			 END
			 FROM events WHERE room_id = $1 AND event_type = 'm.room.create'
			 ORDER BY origin_server_ts DESC LIMIT 1`,
			roomID)
		if err := createRow.Scan(&roomCreator); err == nil && roomCreator != "" {
			events = append(events, map[string]interface{}{
				"type":      "m.room.create",
				"sender":    roomCreator,
				"state_key": "",
				"content":   map[string]string{"creator": roomCreator},
			})
		}
		invites[roomID] = map[string]interface{}{
			"invite_state": map[string]interface{}{
				"events": events,
			},
		}
	}
	return invites
}

// ─── JSON response structs ─────────────────────────────────────────────────────

type syncResponse struct {
	NextBatch   string                 `json:"next_batch"`
	Rooms       syncRooms              `json:"rooms"`
	Presence    syncPresence           `json:"presence"`
	AccountData syncAccountDataSection `json:"account_data"` // Story 9-24: §6.3 top-level global account data — MUST NOT use omitempty
	// Story 5-29e Bug 4: Element Web's matrix-js-sdk treats these three fields as
	// mandatory. Missing device_one_time_keys_count is interpreted as 0, triggering
	// an OTK-upload + keys/query polling loop. Always emit empty values (never nil
	// — JSON-null breaks the SDK; only [] / {} are accepted).
	DeviceOneTimeKeysCount   map[string]int  `json:"device_one_time_keys_count"`
	DeviceUnusedFallbackKeys []string        `json:"device_unused_fallback_key_types"`
	DeviceLists              syncDeviceLists `json:"device_lists"`
}

type syncDeviceLists struct {
	Changed []string `json:"changed"`
	Left    []string `json:"left"`
}

// emptySyncDeviceFields returns the empty default values for the three device
// fields above. Every syncResponse construction site must set these — a missing
// field encodes to JSON-null, which Element Web rejects.
func emptySyncDeviceFields() (map[string]int, []string, syncDeviceLists) {
	return map[string]int{}, []string{}, syncDeviceLists{Changed: []string{}, Left: []string{}}
}

type syncRooms struct {
	Join   map[string]syncJoinedRoom `json:"join"`
	Invite map[string]interface{}    `json:"invite"`
	Leave  map[string]interface{}    `json:"leave"`
}

type syncJoinedRoom struct {
	State       syncStateSection       `json:"state"`
	Timeline    syncTimelineSection    `json:"timeline"`
	AccountData syncAccountDataSection `json:"account_data"`
}

// syncAccountDataSection is the account_data section in a joined room's sync response.
// Spec: {"events": [{"type": "m.tag", "content": {...}}, ...]}
type syncAccountDataSection struct {
	Events []syncAccountDataEvent `json:"events"`
}

// syncAccountDataEvent represents one account_data event in the sync response.
type syncAccountDataEvent struct {
	Type    string          `json:"type"`
	Content json.RawMessage `json:"content"`
}

type syncStateSection struct {
	Events []syncStateEvent `json:"events"`
}

type syncStateEvent struct {
	Type     string          `json:"type"`
	StateKey string          `json:"state_key"`
	Content  json.RawMessage `json:"content"`
	Sender   string          `json:"sender,omitempty"`
}

type syncTimelineSection struct {
	Events    []syncTimelineEvent `json:"events"`
	Limited   bool                `json:"limited"`
	PrevBatch string              `json:"prev_batch,omitempty"`
}

// syncUnsigned holds the unsigned section of a timeline event (spec §8.4.3).
// age is the time in milliseconds since the event was sent, computed as
// time.Now().UnixMilli() - event.OriginTS. matrix-js-sdk uses this field for
// event deduplication and lag detection; missing unsigned.age causes sporadic
// re-polling of already-seen events during DM creation.
type syncUnsigned struct {
	Age int64 `json:"age"`
}

type syncTimelineEvent struct {
	EventID  string          `json:"event_id"`
	Type     string          `json:"type"`
	StateKey string          `json:"state_key,omitempty"`
	Sender   string          `json:"sender"`
	RoomID   string          `json:"room_id"`
	Content  json.RawMessage `json:"content"`
	OriginTS int64           `json:"origin_server_ts"`
	Unsigned syncUnsigned    `json:"unsigned"`
}

type syncPresence struct {
	Events []interface{} `json:"events"`
}

// maxSyncTimeoutMs is the upper bound for the ?timeout query parameter (Story 4-15).
const maxSyncTimeoutMs = int64(30_000)

// GetSync handles GET /_matrix/client/v3/sync.
//
// Flow:
//  1. If ?since query param is present → delegate to handleIncrementalSync (Story 4-15).
//  2. Extract user_id from JWT context (set by JWTMiddleware).
//  3. Apply 5-second context timeout.
//  4. Call Core.GetInitialSync.
//  5. Map gRPC errors: UNAVAILABLE → 503 M_UNAVAILABLE, others → 500 M_UNKNOWN.
//  6. Build Matrix sync response JSON and return 200.
func (h *GetSyncHandler) GetSync(w http.ResponseWriter, r *http.Request) {
	// AC #9 (Story 4-15): ?since present → incremental sync
	if sinceToken := r.URL.Query().Get("since"); sinceToken != "" {
		h.handleIncrementalSync(w, r, sinceToken)
		return
	}

	userID, _ := r.Context().Value(middleware.ContextKeyUserID).(string)
	systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)

	// AC #6: configurable timeout (defaults to 5s)
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()
	grpcCtx := coregrpc.WithUserMetadata(ctx, userID, systemRole)

	resp, err := h.coreClient.GetInitialSync(grpcCtx, &pb.GetInitialSyncRequest{UserId: userID})
	if err != nil {
		st, _ := status.FromError(err)
		slog.Error("GetInitialSync failed", "code", st.Code(), "msg", st.Message(), "user_id", userID)
		switch st.Code() {
		case codes.Unavailable:
			writeMatrixError(w, http.StatusServiceUnavailable, "M_UNAVAILABLE", "Core is temporarily unavailable")
		default:
			writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		}
		return
	}

	// Build Matrix sync response
	joinedRooms := buildJoinedRooms(resp.GetRooms())
	// Story 7-24 AC4: inject per-room account data into each joined room's account_data.events.
	h.injectAccountData(r.Context(), userID, joinedRooms)

	otkCount, fallbackKeys, deviceLists := emptySyncDeviceFields()
	syncResp := syncResponse{
		NextBatch: resp.GetSinceToken(),
		Rooms: syncRooms{
			Join:   joinedRooms,
			Invite: h.buildInviteRooms(r.Context(), userID),
			// Initial sync: sinceMs=0 → no time filter (return all left/rejected rooms).
			Leave: h.buildLeaveRooms(r.Context(), userID, 0),
		},
		Presence:                 syncPresence{Events: []interface{}{}},
		AccountData:              h.injectGlobalAccountData(r.Context(), userID), // Story 9-24: §6.3 top-level global account data
		DeviceOneTimeKeysCount:   otkCount,
		DeviceUnusedFallbackKeys: fallbackKeys,
		DeviceLists:              deviceLists,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(syncResp)
}

// handleIncrementalSync handles GET /_matrix/client/v3/sync?since=<token>.
//
// Story 4-15 — incremental sync with long-polling.
// Flow:
//  1. Parse and clamp ?timeout query param (default 0, max 30000 ms).
//  2. Extract user_id from JWT context.
//  3. Call Core.GetSyncDelta with user_id, since_token, timeout_ms.
//  4. If response.FallbackToInitial → call GetInitialSync and return full response.
//  5. Map gRPC errors: UNAVAILABLE → 503 M_UNAVAILABLE.
//  6. Build Matrix sync response JSON and return 200.
func (h *GetSyncHandler) handleIncrementalSync(w http.ResponseWriter, r *http.Request, sinceToken string) {
	// 1. Parse and clamp timeout
	timeoutMs := int64(0)
	if raw := r.URL.Query().Get("timeout"); raw != "" {
		if v, err := strconv.ParseInt(raw, 10, 64); err == nil {
			timeoutMs = v
		}
	}
	if timeoutMs < 0 {
		timeoutMs = 0
	}
	if timeoutMs > maxSyncTimeoutMs {
		timeoutMs = maxSyncTimeoutMs
	}

	userID, _ := r.Context().Value(middleware.ContextKeyUserID).(string)
	systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)

	// Story 4-16: buffer pre-check — drain local ring buffer before hitting Core.
	// If events are already available locally, return them immediately (skip Core long-poll).
	if h.buffer != nil {
		if events := h.buffer.DrainFor(userID, 50); len(events) > 0 {
			resp := h.buildResponseFromBufferedEvents(events)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		// No events yet — wait briefly on the local buffer signal.
		// If signalled, try to drain again; if events available, return; else fall through to Core.
		bufCtx, bufCancel := context.WithTimeout(r.Context(), 100*time.Millisecond)
		waitCh := h.buffer.WaitFor(bufCtx, userID)
		select {
		case <-waitCh:
			if events := h.buffer.DrainFor(userID, 50); len(events) > 0 {
				bufCancel()
				resp := h.buildResponseFromBufferedEvents(events)
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(resp)
				return
			}
		case <-bufCtx.Done():
			// timeout expired — fall through to Core
		}
		bufCancel()
	}

	// GAP-LEAVE-ONCE fix: read the previous sync token's timestamp BEFORE calling
	// GetSyncDelta. Elixir calls persist_since_token (which updates sync_tokens.updated_at)
	// at the END of GetSyncDelta. If we called querySinceTsMs after GetSyncDelta, we would
	// read the timestamp set by THIS sync cycle, which is always >= left_at, causing
	// buildLeaveRooms to filter out the newly-left room. By reading before, we capture
	// the timestamp from the PREVIOUS sync cycle (before the leave happened), so newly-left
	// rooms (left_at > prev_updated_at) correctly appear in rooms.leave.
	sinceMs := h.querySinceTsMs(r.Context(), userID)
	slog.Debug("handleIncrementalSync: sinceMs read before GetSyncDelta", "userID", userID, "sinceMs", sinceMs)

	// AC #11: handler timeout = timeout_ms + 5000 ms grace period
	handlerTimeout := h.timeout + time.Duration(timeoutMs)*time.Millisecond
	ctx, cancel := context.WithTimeout(r.Context(), handlerTimeout)
	defer cancel()
	grpcCtx := coregrpc.WithUserMetadata(ctx, userID, systemRole)

	deviceID, _ := r.Context().Value(middleware.ContextKeyDeviceID).(string)
	deltaResp, err := h.coreClient.GetSyncDelta(grpcCtx, &pb.GetSyncDeltaRequest{
		UserId:     userID,
		SinceToken: sinceToken,
		TimeoutMs:  timeoutMs,
		DeviceId:   deviceID,
	})
	if err != nil {
		st, _ := status.FromError(err)
		switch st.Code() {
		case codes.Unavailable:
			writeMatrixError(w, http.StatusServiceUnavailable, "M_UNAVAILABLE", "Core is temporarily unavailable")
		default:
			writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		}
		return
	}

	// AC #6 (Story 4-15): FallbackToInitial → delegate to GetInitialSync
	if deltaResp.GetFallbackToInitial() {
		initialResp, err := h.coreClient.GetInitialSync(grpcCtx, &pb.GetInitialSyncRequest{UserId: userID})
		if err != nil {
			st, _ := status.FromError(err)
			switch st.Code() {
			case codes.Unavailable:
				writeMatrixError(w, http.StatusServiceUnavailable, "M_UNAVAILABLE", "Core is temporarily unavailable")
			default:
				writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
			}
			return
		}
		joinedRooms := buildJoinedRooms(initialResp.GetRooms())
		// Story 7-24 AC4: inject per-room account data into joined rooms.
		h.injectAccountData(r.Context(), userID, joinedRooms)
		otkCount, fallbackKeys, deviceLists := emptySyncDeviceFields()
		syncResp := syncResponse{
			NextBatch: initialResp.GetSinceToken(),
			Rooms: syncRooms{
				Join:   joinedRooms,
				Invite: h.buildInviteRooms(r.Context(), userID),
				// FallbackToInitial: sinceMs=0 → no time filter (full re-sync).
				Leave: h.buildLeaveRooms(r.Context(), userID, 0),
			},
			Presence:                 syncPresence{Events: []interface{}{}},
			AccountData:              h.injectGlobalAccountData(r.Context(), userID), // Story 9-24: §6.3 global account data in FallbackToInitial path
			DeviceOneTimeKeysCount:   otkCount,
			DeviceUnusedFallbackKeys: fallbackKeys,
			DeviceLists:              deviceLists,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(syncResp)
		return
	}

	// Build delta sync response.
	// leaveRooms is built first so we can exclude left rooms from joinedRooms.
	// A room must not appear in both rooms.join and rooms.leave in the same response —
	// Element Web behaviour is undefined for such conflicts and it may fail to navigate
	// away. Rooms.leave takes precedence: a room the user just left must not be returned
	// as joined even if fetch_delta_rooms found its leave event in the timeline.
	// GAP-LEAVE-ONCE: sinceMs was captured above BEFORE GetSyncDelta updated sync_tokens.
	leaveRooms := h.buildLeaveRooms(r.Context(), userID, sinceMs)
	slog.Debug("handleIncrementalSync: leaveRooms count", "userID", userID, "count", len(leaveRooms))
	joinedRooms := buildJoinedRooms(deltaResp.GetRooms())
	for roomID := range leaveRooms {
		delete(joinedRooms, roomID)
	}
	// GAP-FORGET: remove forgotten rooms from rooms.join so they don't resurface after
	// POST /forget. The Elixir delta may still include the room (left_room_ids pattern),
	// but a forgotten room must be absent from ALL sync sections.
	for roomID := range h.queryForgottenRoomIDs(r.Context(), userID) {
		delete(joinedRooms, roomID)
	}
	// Story 7-24 AC4: inject per-room account data into joined rooms.
	h.injectAccountData(r.Context(), userID, joinedRooms)
	otkCount, fallbackKeys, deviceLists := emptySyncDeviceFields()
	syncResp := syncResponse{
		NextBatch: deltaResp.GetSinceToken(),
		Rooms: syncRooms{
			Join:   joinedRooms,
			Invite: h.buildInviteRooms(r.Context(), userID),
			Leave:  leaveRooms,
		},
		Presence:                 syncPresence{Events: []interface{}{}},
		AccountData:              h.injectGlobalAccountData(r.Context(), userID), // Story 9-24: §6.3 global account data in delta sync path
		DeviceOneTimeKeysCount:   otkCount,
		DeviceUnusedFallbackKeys: fallbackKeys,
		DeviceLists:              deviceLists,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(syncResp)
}

// syntheticBatchSeq is an atomic counter used by syntheticNextBatch to guarantee
// token uniqueness even when two goroutines call the function within the same millisecond.
// Package-level var; zero-initialized (Story 9-25, GAP-BUFFER-NEXT-BATCH).
var syntheticBatchSeq atomic.Int64

// syntheticNextBatch generates a monotonically advancing, opaque next_batch token
// for responses served from the local ring buffer (Story 9-25, GAP-BUFFER-NEXT-BATCH).
//
// Format: "buf_<unix_ms>_<seq>" — clearly synthetic, not a real Elixir since-token.
// If the client sends this token on the next request, Elixir's GetSyncDelta will
// not find a matching sync_tokens row → FallbackToInitial → safe full re-sync.
//
// Monotonicity guarantee: time.Now().UnixMilli() is monotonically increasing within
// a process under normal NTP conditions. For sub-millisecond bursts the counter suffix
// ensures uniqueness (see syntheticBatchSeq above).
func syntheticNextBatch() string {
	seq := syntheticBatchSeq.Add(1)
	return fmt.Sprintf("buf_%d_%d", time.Now().UnixMilli(), seq)
}

// buildResponseFromBufferedEvents constructs a minimal syncResponse from locally-buffered
// *pb.Event values (Story 4-16). Events are placed in the timeline of their respective rooms.
// syntheticNextBatch() generates a buf_<ms>_<seq> token. The Elixir delta handler will
// not recognise it, triggering FallbackToInitial on the next request — which is the
// correct behaviour for a client that resumes from a synthetic token.
func (h *GetSyncHandler) buildResponseFromBufferedEvents(events []*pb.Event) syncResponse {
	joinedRooms := make(map[string]syncJoinedRoom)
	for _, ev := range events {
		room := joinedRooms[ev.RoomId]
		room.Timeline.Events = append(room.Timeline.Events, syncTimelineEvent{
			EventID:  ev.EventId,
			Type:     ev.EventType,
			StateKey: ev.StateKey,
			Sender:   ev.SenderId,
			RoomID:   ev.RoomId,
			Content:  json.RawMessage(ev.Content),
			OriginTS: ev.OriginTs,
			Unsigned: syncUnsigned{Age: max(1, time.Now().UnixMilli()-ev.OriginTs)},
		})
		joinedRooms[ev.RoomId] = room
	}
	// Buffer-based fast path: skip invite/leave queries (caller handles full sync).
	// Story 9-24: AccountData is set to empty section here — no DB call in the buffer
	// hot path. Global account data changes are rare; the next full sync cycle will
	// pick them up. This keeps the buffer fast-path O(0) DB queries.
	otkCount, fallbackKeys, deviceLists := emptySyncDeviceFields()
	return syncResponse{
		NextBatch: syntheticNextBatch(),
		Rooms: syncRooms{
			Join:   joinedRooms,
			Invite: map[string]interface{}{},
			Leave:  map[string]interface{}{},
		},
		Presence:                 syncPresence{Events: []interface{}{}},
		AccountData:              syncAccountDataSection{Events: []syncAccountDataEvent{}}, // Story 9-24: empty section — buffer fast-path skips DB call
		DeviceOneTimeKeysCount:   otkCount,
		DeviceUnusedFallbackKeys: fallbackKeys,
		DeviceLists:              deviceLists,
	}
}

// buildJoinedRooms converts a slice of SyncRoom protos into the Matrix sync
// rooms.join map format. Reused by both initial sync and incremental sync.
func buildJoinedRooms(rooms []*pb.SyncRoom) map[string]syncJoinedRoom {
	joinedRooms := make(map[string]syncJoinedRoom)
	for _, room := range rooms {
		stateEvents := make([]syncStateEvent, 0, len(room.GetStateEvents()))
		for _, se := range room.GetStateEvents() {
			stateEvents = append(stateEvents, syncStateEvent{
				Type:     se.GetType(),
				StateKey: se.GetStateKey(),
				Content:  json.RawMessage(se.GetContent()),
				Sender:   se.GetSender(),
			})
		}

		timelineEvents := make([]syncTimelineEvent, 0, len(room.GetTimelineEvents()))
		for _, te := range room.GetTimelineEvents() {
			timelineEvents = append(timelineEvents, syncTimelineEvent{
				EventID:  te.GetEventId(),
				Type:     te.GetEventType(),
				StateKey: te.GetStateKey(),
				Sender:   te.GetSenderId(),
				RoomID:   te.GetRoomId(),
				Content:  json.RawMessage(te.GetContent()),
				OriginTS: te.GetOriginTs(),
				Unsigned: syncUnsigned{Age: max(1, time.Now().UnixMilli()-te.GetOriginTs())},
			})
		}

		joinedRooms[room.GetRoomId()] = syncJoinedRoom{
			State: syncStateSection{Events: stateEvents},
			Timeline: syncTimelineSection{
				Events:    timelineEvents,
				Limited:   room.GetLimited(),
				PrevBatch: room.GetPrevBatch(),
			},
			// AccountData is populated by injectAccountData; initialize to empty (not null).
			AccountData: syncAccountDataSection{Events: []syncAccountDataEvent{}},
		}
	}
	return joinedRooms
}

// injectAccountData queries the room_account_data table for all (userID, roomID) pairs
// present in joinedRooms and injects the account_data.events into each room's entry.
//
// Story 7-24 AC4: after a PUT, the next /sync response must include the account_data
// event under rooms.join.{roomId}.account_data.events.
//
// This is a best-effort operation: if the DB is unavailable or a row is missing, that
// room gets an empty events slice (graceful degradation, no error surfaced to client).
func (h *GetSyncHandler) injectAccountData(ctx context.Context, userID string, joinedRooms map[string]syncJoinedRoom) {
	if h.accountDataDB == nil {
		return // disabled (nil = account data injection not configured)
	}
	for roomID, room := range joinedRooms {
		events, err := h.fetchRoomAccountDataEvents(ctx, userID, roomID)
		if err != nil {
			// Graceful degradation: log a warning but don't fail the sync.
			slog.Warn("injectAccountData: failed to fetch account data", "room", roomID, "err", err)
			continue
		}
		room.AccountData = syncAccountDataSection{Events: events}
		joinedRooms[roomID] = room
	}
}

// injectGlobalAccountData queries global account data (room_id = '') for userID
// and returns a syncAccountDataSection for the top-level account_data field.
// Degrades gracefully to an empty events slice on DB error (AC6).
// The section is always returned with Events initialized to a non-nil slice
// so that "account_data":{"events":[]} is always present in the JSON response
// (never absent, never null — Matrix spec §6.3).
func (h *GetSyncHandler) injectGlobalAccountData(ctx context.Context, userID string) syncAccountDataSection {
	if h.globalAccountDataDB == nil {
		return syncAccountDataSection{Events: []syncAccountDataEvent{}}
	}
	rows, err := h.globalAccountDataDB.ListGlobalAccountData(ctx, userID)
	if err != nil {
		slog.Warn("injectGlobalAccountData: DB error", "user_id", userID, "err", err)
		return syncAccountDataSection{Events: []syncAccountDataEvent{}}
	}
	events := make([]syncAccountDataEvent, 0, len(rows))
	for _, r := range rows {
		events = append(events, syncAccountDataEvent{Type: r.EventType, Content: r.Content})
	}
	return syncAccountDataSection{Events: events}
}

// fetchRoomAccountDataEvents queries all account data rows for (userID, roomID) and
// returns them as syncAccountDataEvent values. Returns an empty slice if no rows exist.
func (h *GetSyncHandler) fetchRoomAccountDataEvents(ctx context.Context, userID, roomID string) ([]syncAccountDataEvent, error) {
	if h.accountDataDB == nil {
		return []syncAccountDataEvent{}, nil
	}
	// The AccountDataDB interface only exposes GetAccountData for a single eventType.
	// To fetch all types for a room we query the underlying table via a type-list DB.
	// For MVP, we support the two most important types that Element Web reads from sync:
	// "m.tag" (room tags) and "m.fully_read" (read position marker).
	// TODO: replace with a scanAllRoomAccountData(userID, roomID) query when added to AccountDataDB.
	importantTypes := []string{"m.tag", "m.fully_read", "m.push_rules"}
	var events []syncAccountDataEvent
	for _, eventType := range importantTypes {
		content, err := h.accountDataDB.GetAccountData(ctx, userID, roomID, eventType)
		if err != nil {
			if errors.Is(err, ErrAccountDataNotFound) {
				continue // no data for this type — skip
			}
			return nil, err
		}
		events = append(events, syncAccountDataEvent{
			Type:    eventType,
			Content: content,
		})
	}
	if events == nil {
		events = []syncAccountDataEvent{}
	}
	return events, nil
}
