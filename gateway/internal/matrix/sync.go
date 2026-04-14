package matrix

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
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
	coreClient GetSyncCoreClient
	serverName string
	timeout    time.Duration
	buffer     *buffer.MessageBuffer // Story 4-16: local event buffer (nil = disabled)
}

// GetSyncConfig holds dependencies for NewGetSyncHandler.
type GetSyncConfig struct {
	CoreClient GetSyncCoreClient
	ServerName string
	Timeout    time.Duration         // gRPC call timeout; defaults to 5s if zero
	Buffer     *buffer.MessageBuffer // Story 4-16: optional local event buffer
}

// NewGetSyncHandler constructs a GetSyncHandler from the provided config.
func NewGetSyncHandler(cfg GetSyncConfig) *GetSyncHandler {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	return &GetSyncHandler{
		coreClient: cfg.CoreClient,
		serverName: cfg.ServerName,
		timeout:    timeout,
		buffer:     cfg.Buffer,
	}
}

// ─── JSON response structs ─────────────────────────────────────────────────────

type syncResponse struct {
	NextBatch string       `json:"next_batch"`
	Rooms     syncRooms    `json:"rooms"`
	Presence  syncPresence `json:"presence"`
}

type syncRooms struct {
	Join   map[string]syncJoinedRoom `json:"join"`
	Invite map[string]interface{}    `json:"invite"`
	Leave  map[string]interface{}    `json:"leave"`
}

type syncJoinedRoom struct {
	State    syncStateSection    `json:"state"`
	Timeline syncTimelineSection `json:"timeline"`
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

type syncTimelineEvent struct {
	EventID  string          `json:"event_id"`
	Type     string          `json:"type"`
	Sender   string          `json:"sender"`
	RoomID   string          `json:"room_id"`
	Content  json.RawMessage `json:"content"`
	OriginTS int64           `json:"origin_server_ts"`
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

	syncResp := syncResponse{
		NextBatch: resp.GetSinceToken(),
		Rooms: syncRooms{
			Join:   joinedRooms,
			Invite: map[string]interface{}{},
			Leave:  map[string]interface{}{},
		},
		Presence: syncPresence{Events: []interface{}{}},
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
			resp := h.buildResponseFromBufferedEvents(events, sinceToken)
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
				resp := h.buildResponseFromBufferedEvents(events, sinceToken)
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(resp)
				return
			}
		case <-bufCtx.Done():
			// timeout expired — fall through to Core
		}
		bufCancel()
	}

	// AC #11: handler timeout = timeout_ms + 5000 ms grace period
	handlerTimeout := h.timeout + time.Duration(timeoutMs)*time.Millisecond
	ctx, cancel := context.WithTimeout(r.Context(), handlerTimeout)
	defer cancel()
	grpcCtx := coregrpc.WithUserMetadata(ctx, userID, systemRole)

	deltaResp, err := h.coreClient.GetSyncDelta(grpcCtx, &pb.GetSyncDeltaRequest{
		UserId:     userID,
		SinceToken: sinceToken,
		TimeoutMs:  timeoutMs,
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
		syncResp := syncResponse{
			NextBatch: initialResp.GetSinceToken(),
			Rooms: syncRooms{
				Join:   joinedRooms,
				Invite: map[string]interface{}{},
				Leave:  map[string]interface{}{},
			},
			Presence: syncPresence{Events: []interface{}{}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(syncResp)
		return
	}

	// Build delta sync response
	joinedRooms := buildJoinedRooms(deltaResp.GetRooms())
	syncResp := syncResponse{
		NextBatch: deltaResp.GetSinceToken(),
		Rooms: syncRooms{
			Join:   joinedRooms,
			Invite: map[string]interface{}{},
			Leave:  map[string]interface{}{},
		},
		Presence: syncPresence{Events: []interface{}{}},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(syncResp)
}

// buildResponseFromBufferedEvents constructs a minimal syncResponse from locally-buffered
// *pb.Event values (Story 4-16). Events are placed in the timeline of their respective rooms.
// sinceToken is used as the next_batch value (events are fresh, not a new server token).
func (h *GetSyncHandler) buildResponseFromBufferedEvents(events []*pb.Event, sinceToken string) syncResponse {
	joinedRooms := make(map[string]syncJoinedRoom)
	for _, ev := range events {
		room := joinedRooms[ev.RoomId]
		room.Timeline.Events = append(room.Timeline.Events, syncTimelineEvent{
			EventID:  ev.EventId,
			Type:     ev.EventType,
			Sender:   ev.SenderId,
			RoomID:   ev.RoomId,
			Content:  json.RawMessage(ev.Content),
			OriginTS: ev.OriginTs,
		})
		joinedRooms[ev.RoomId] = room
	}
	return syncResponse{
		NextBatch: sinceToken,
		Rooms: syncRooms{
			Join:   joinedRooms,
			Invite: map[string]interface{}{},
			Leave:  map[string]interface{}{},
		},
		Presence: syncPresence{Events: []interface{}{}},
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
				Sender:   te.GetSenderId(),
				RoomID:   te.GetRoomId(),
				Content:  json.RawMessage(te.GetContent()),
				OriginTS: te.GetOriginTs(),
			})
		}

		joinedRooms[room.GetRoomId()] = syncJoinedRoom{
			State: syncStateSection{Events: stateEvents},
			Timeline: syncTimelineSection{
				Events:    timelineEvents,
				Limited:   room.GetLimited(),
				PrevBatch: room.GetPrevBatch(),
			},
		}
	}
	return joinedRooms
}
