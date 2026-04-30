package matrix

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	coregrpc "github.com/nebu/nebu/internal/grpc"
	pb "github.com/nebu/nebu/internal/grpc/pb"
	"github.com/nebu/nebu/internal/middleware"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GetMembersCoreClient is a consumer-defined interface for the GetRoomState gRPC call.
// Keep it minimal — only what this handler needs (Go interface convention, ADR-009).
type GetMembersCoreClient interface {
	GetRoomState(ctx context.Context, req *pb.GetRoomStateRequest) (*pb.GetRoomStateResponse, error)
}

// GetRoomMembersHandler handles GET /_matrix/client/v3/rooms/{roomId}/members.
type GetRoomMembersHandler struct {
	coreClient GetMembersCoreClient
	serverName string
}

// GetRoomMembersConfig holds dependencies for NewGetRoomMembersHandler.
type GetRoomMembersConfig struct {
	CoreClient GetMembersCoreClient
	ServerName string
}

// NewGetRoomMembersHandler constructs a GetRoomMembersHandler from the provided config.
func NewGetRoomMembersHandler(cfg GetRoomMembersConfig) *GetRoomMembersHandler {
	return &GetRoomMembersHandler{
		coreClient: cfg.CoreClient,
		serverName: cfg.ServerName,
	}
}

// roomMemberEvent is a Matrix m.room.member state event as returned in the chunk array.
type roomMemberEvent struct {
	Type     string         `json:"type"`
	StateKey string         `json:"state_key"`
	Content  map[string]any `json:"content"`
}

// GetRoomMembers handles GET /_matrix/client/v3/rooms/{roomId}/members.
//
// Flow:
//  1. Extract roomId from URL path via r.PathValue.
//  2. Extract authenticated user_id + systemRole from JWT context.
//  3. Call Core.GetRoomState — returns the list of joined member user IDs.
//  4. Map gRPC errors: PermissionDenied → 403 M_FORBIDDEN; NotFound → 404 M_NOT_FOUND;
//     Unavailable → 503 M_UNAVAILABLE; default → 500 M_UNKNOWN.
//  5. Shape each member ID into a Matrix m.room.member state event.
//  6. Return 200 {"chunk": [...]} — chunk is always an array, never null.
func (h *GetRoomMembersHandler) GetRoomMembers(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("roomId")

	userID, _ := r.Context().Value(middleware.ContextKeyUserID).(string)
	systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
	grpcCtx := coregrpc.WithUserMetadata(r.Context(), userID, systemRole)

	resp, err := h.coreClient.GetRoomState(grpcCtx, &pb.GetRoomStateRequest{RoomId: roomID})
	if err != nil {
		st, _ := status.FromError(err)
		slog.Error("GetRoomState gRPC failed", "code", st.Code(), "msg", st.Message(), "room", roomID)
		switch st.Code() {
		case codes.PermissionDenied:
			writeMatrixError(w, http.StatusForbidden, "M_FORBIDDEN", "You are not a member of this room")
		case codes.NotFound:
			writeMatrixError(w, http.StatusNotFound, "M_NOT_FOUND", "Room not found")
		case codes.Unavailable:
			writeMatrixError(w, http.StatusServiceUnavailable, "M_UNAVAILABLE", "Server unavailable")
		default:
			writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		}
		return
	}

	// Build chunk: one m.room.member event per member ID.
	chunk := make([]roomMemberEvent, 0, len(resp.Members))
	for _, memberID := range resp.Members {
		chunk = append(chunk, roomMemberEvent{
			Type:     "m.room.member",
			StateKey: memberID,
			Content: map[string]any{
				"membership": "join",
			},
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"chunk": chunk})
}

// ─── Story 7-20: GET /rooms/{roomId}/joined_members ──────────────────────────

// GetJoinedMembersCoreClient is a consumer-defined interface for the GetRoomState gRPC call.
// Kept minimal — only what GetJoinedMembersHandler needs (Go interface convention, ADR-009).
type GetJoinedMembersCoreClient interface {
	GetRoomState(ctx context.Context, req *pb.GetRoomStateRequest) (*pb.GetRoomStateResponse, error)
}

// GetJoinedMembersHandler handles GET /_matrix/client/v3/rooms/{roomId}/joined_members.
type GetJoinedMembersHandler struct {
	coreClient GetJoinedMembersCoreClient
	db         ProfileDB
	serverName string
}

// GetJoinedMembersConfig holds dependencies for NewGetJoinedMembersHandler.
type GetJoinedMembersConfig struct {
	CoreClient GetJoinedMembersCoreClient
	DB         ProfileDB
	ServerName string
}

// NewGetJoinedMembersHandler constructs a GetJoinedMembersHandler from the provided config.
func NewGetJoinedMembersHandler(cfg GetJoinedMembersConfig) *GetJoinedMembersHandler {
	return &GetJoinedMembersHandler{
		coreClient: cfg.CoreClient,
		db:         cfg.DB,
		serverName: cfg.ServerName,
	}
}

// joinedMemberProfile is the per-user object in the compact joined_members response.
// Per AC7: display_name and avatar_url are omitted (not set to null) when absent.
// Both fields use omitempty so missing profiles result in an empty object {} rather
// than {"display_name":null,"avatar_url":null}. This is the "omit" convention.
type joinedMemberProfile struct {
	DisplayName *string `json:"display_name,omitempty"`
	AvatarURL   *string `json:"avatar_url,omitempty"`
}

// GetJoinedMembers handles GET /_matrix/client/v3/rooms/{roomId}/joined_members.
//
// Design choice (AC7): missing profile fields are OMITTED (not set to null) — per the
// joinedMemberProfile struct definition, both fields use *string with omitempty, so a
// missing profile yields {} for that user. Either convention is acceptable per spec; we
// chose "omit" so the JSON byte size scales with what is actually known.
//
// Flow:
//  1. Extract roomId from URL path via r.PathValue.
//  2. Extract authenticated user_id + systemRole from JWT context.
//  3. Call Core.GetRoomState — returns joined member IDs in resp.Members. Core enforces
//     membership (raises PermissionDenied if caller is not a joined member, see
//     event_dispatcher/server.ex#get_room_state); no Go-side membership check needed.
//  4. Map gRPC errors: PermissionDenied→403, NotFound→404, Unavailable→503, default→500.
//  5. For each member ID, call db.GetProfile(ctx, mxid). Tolerate ErrProfileNotFound by
//     leaving both display_name and avatar_url unset (omitted from the JSON object).
//
// TODO (Phase 2): Replace N sequential GetProfile calls with a batch GetProfiles DB method.
//
// Returns 200 {"joined": {"@user:server": {"display_name": "...", "avatar_url": "..."}}}.
// The "joined" map is always an object, never null (even for empty rooms).
func (h *GetJoinedMembersHandler) GetJoinedMembers(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("roomId")

	userID, _ := r.Context().Value(middleware.ContextKeyUserID).(string)
	systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
	grpcCtx := coregrpc.WithUserMetadata(r.Context(), userID, systemRole)

	resp, err := h.coreClient.GetRoomState(grpcCtx, &pb.GetRoomStateRequest{RoomId: roomID})
	if err != nil {
		st, _ := status.FromError(err)
		slog.Error("GetRoomState gRPC failed (joined_members)", "code", st.Code(), "msg", st.Message(), "room", roomID)
		switch st.Code() {
		case codes.PermissionDenied:
			writeMatrixError(w, http.StatusForbidden, "M_FORBIDDEN", "You are not a member of this room")
		case codes.NotFound:
			writeMatrixError(w, http.StatusNotFound, "M_NOT_FOUND", "Room not found")
		case codes.Unavailable:
			writeMatrixError(w, http.StatusServiceUnavailable, "M_UNAVAILABLE", "Server unavailable")
		default:
			writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		}
		return
	}

	// Build compact joined map: keyed by MXID, value is display_name + avatar_url.
	// Profile fields are omitted (omitempty on *string) when absent; ErrProfileNotFound
	// is tolerated (AC2, AC7) — the user still appears in the map with an empty {} object.
	joined := make(map[string]*joinedMemberProfile, len(resp.Members))
	for _, memberID := range resp.Members {
		profile, dbErr := h.db.GetProfile(r.Context(), memberID)
		if dbErr != nil {
			if errors.Is(dbErr, ErrProfileNotFound) {
				// No profile row — both fields stay nil and are omitted from JSON.
				joined[memberID] = &joinedMemberProfile{}
			} else {
				slog.Error("GetProfile DB error (joined_members)", "user", memberID, "err", dbErr)
				writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
				return
			}
			continue
		}

		entry := &joinedMemberProfile{}
		if profile.DisplayName != "" {
			dn := profile.DisplayName
			entry.DisplayName = &dn
		}
		if profile.AvatarURL != "" {
			au := profile.AvatarURL
			entry.AvatarURL = &au
		}
		joined[memberID] = entry
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"joined": joined})
}
