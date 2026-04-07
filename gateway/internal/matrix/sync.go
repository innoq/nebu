package matrix

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	coregrpc "github.com/nebu/nebu/internal/grpc"
	pb "github.com/nebu/nebu/internal/grpc/pb"
	"github.com/nebu/nebu/internal/middleware"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GetSyncCoreClient is the consumer-defined interface for the GetInitialSync gRPC call.
// Keep it minimal — only what this handler needs (Go interface convention, ADR-009).
type GetSyncCoreClient interface {
	GetInitialSync(ctx context.Context, req *pb.GetInitialSyncRequest) (*pb.GetInitialSyncResponse, error)
}

// GetSyncHandler handles GET /_matrix/client/v3/sync.
type GetSyncHandler struct {
	coreClient GetSyncCoreClient
	serverName string
	timeout    time.Duration
}

// GetSyncConfig holds dependencies for NewGetSyncHandler.
type GetSyncConfig struct {
	CoreClient GetSyncCoreClient
	ServerName string
	Timeout    time.Duration // gRPC call timeout; defaults to 5s if zero
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

// GetSync handles GET /_matrix/client/v3/sync.
//
// Flow:
//  1. If ?since query param is present → return 501 (Story 4-15 placeholder).
//  2. Extract user_id from JWT context (set by JWTMiddleware).
//  3. Apply 5-second context timeout.
//  4. Call Core.GetInitialSync.
//  5. Map gRPC errors: UNAVAILABLE → 503 M_UNAVAILABLE, others → 500 M_UNKNOWN.
//  6. Build Matrix sync response JSON and return 200.
func (h *GetSyncHandler) GetSync(w http.ResponseWriter, r *http.Request) {
	// AC #7: ?since present → 501 stub (Story 4-15)
	if r.URL.Query().Get("since") != "" {
		writeMatrixError(w, http.StatusNotImplemented, "M_UNRECOGNIZED", "Incremental sync not yet implemented (Story 4-15)")
		return
	}

	sub, _ := r.Context().Value(middleware.ContextKeySub).(string)
	systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
	userID := coregrpc.FormatUserID(sub, h.serverName)

	// AC #6: configurable timeout (defaults to 5s)
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()
	grpcCtx := coregrpc.WithUserMetadata(ctx, userID, systemRole)

	resp, err := h.coreClient.GetInitialSync(grpcCtx, &pb.GetInitialSyncRequest{UserId: userID})
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

	// Build Matrix sync response
	joinedRooms := make(map[string]syncJoinedRoom)
	for _, room := range resp.GetRooms() {
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
