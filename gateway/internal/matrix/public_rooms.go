package matrix

// Story 7-27: Public Room Directory — GET/POST /_matrix/client/v3/publicRooms
//
// GET  /_matrix/client/v3/publicRooms  — unauthenticated, looseRL
// POST /_matrix/client/v3/publicRooms  — JWT required, bodyLimit1MiB
//
// Both endpoints delegate to a shared listPublicRooms helper that calls the Elixir
// Core via gRPC (ListPublicRooms). The Elixir handler:
//  1. Queries the DB for rooms WHERE join_rule = 'public' AND room_id > $since ORDER BY room_id LIMIT limit+1
//  2. Resolves live member counts from running Room GenServers (DB fallback for offline rooms)
//  3. Returns the page + next_cursor + total_estimate
//
// Pagination:
//   - `since` query param / body field is the room_id of the last item on the previous page.
//   - `next_batch` in the response is the room_id of the last item returned on this page.
//   - If there are no more rooms, next_batch is omitted.
//
// AC1 — limit defaults to 20, capped at 100.
// AC2 — POST accepts {"limit":N,"since":"...","filter":{"generic_search_term":"foo"}}.
// AC3 — Each chunk entry contains: room_id, name, topic (if set), num_joined_members,
//
//	world_readable (false), guest_can_join (false).
//
// AC4 — num_joined_members reflects the live count from Core, not a stale DB value.
// AC5 — Only rooms with join_rule = public appear (enforced by Elixir gRPC handler).
// AC6 — GET is unauthenticated (looseRL); POST requires JWT (jwtMiddleware + bodyLimit1MiB).
// AC7 — Cursor-based pagination is stable across room creations between requests.

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	coregrpc "github.com/nebu/nebu/internal/grpc"
	pb "github.com/nebu/nebu/internal/grpc/pb"
	"github.com/nebu/nebu/internal/middleware"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// PublicRoomsCoreClient is the consumer-defined interface for the ListPublicRooms gRPC call.
// Kept minimal — only what this handler needs (Go interface convention, ADR-009).
type PublicRoomsCoreClient interface {
	ListPublicRooms(ctx context.Context, req *pb.ListPublicRoomsRequest) (*pb.ListPublicRoomsResponse, error)
}

// PublicRoomsHandler handles GET and POST /_matrix/client/v3/publicRooms.
type PublicRoomsHandler struct {
	coreClient PublicRoomsCoreClient
	serverName string
}

// PublicRoomsConfig holds dependencies for NewPublicRoomsHandler.
type PublicRoomsConfig struct {
	CoreClient PublicRoomsCoreClient
	ServerName string
}

// NewPublicRoomsHandler constructs a PublicRoomsHandler from the provided config.
func NewPublicRoomsHandler(cfg PublicRoomsConfig) *PublicRoomsHandler {
	return &PublicRoomsHandler{
		coreClient: cfg.CoreClient,
		serverName: cfg.ServerName,
	}
}

// publicRoomEntry is a single entry in the Matrix publicRooms chunk array.
// Fields follow the Matrix Client-Server API spec r0.6.1 §10.5.
//
// Note: topic and name are omitted (not null) when empty (omitempty).
// world_readable and guest_can_join are always present (no omitempty) to satisfy
// the Matrix spec which requires these fields in every entry.
type publicRoomEntry struct {
	RoomID           string `json:"room_id"`
	Name             string `json:"name,omitempty"`
	Topic            string `json:"topic,omitempty"`
	NumJoinedMembers int32  `json:"num_joined_members"`
	WorldReadable    bool   `json:"world_readable"`
	GuestCanJoin     bool   `json:"guest_can_join"`
}

// publicRoomsResponse is the Matrix publicRooms HTTP response body.
// next_batch is omitted (omitempty) when there are no more pages (AC1, AC7).
type publicRoomsResponse struct {
	Chunk              []publicRoomEntry `json:"chunk"`
	NextBatch          string            `json:"next_batch,omitempty"`
	TotalRoomCountEst  int32             `json:"total_room_count_estimate"`
}

const (
	defaultPublicRoomsLimit = 20
	maxPublicRoomsLimit     = 100
)

// clampPublicRoomsLimit normalises the limit: 0 → default, >100 → 100.
func clampPublicRoomsLimit(limit int32) int32 {
	if limit <= 0 {
		return defaultPublicRoomsLimit
	}
	if limit > maxPublicRoomsLimit {
		return maxPublicRoomsLimit
	}
	return limit
}

// listPublicRooms is the shared core logic called by both GET and POST handlers.
// It calls the gRPC Core, maps the response, and writes the HTTP reply.
func (h *PublicRoomsHandler) listPublicRooms(ctx context.Context, w http.ResponseWriter, limit int32, since, filterTerm string) {
	limit = clampPublicRoomsLimit(limit)

	resp, err := h.coreClient.ListPublicRooms(ctx, &pb.ListPublicRoomsRequest{
		Limit:      limit,
		Since:      since,
		FilterTerm: filterTerm,
	})
	if err != nil {
		st, _ := status.FromError(err)
		slog.Error("ListPublicRooms gRPC failed", "code", st.Code(), "msg", st.Message())
		switch st.Code() {
		case codes.Unavailable:
			writeMatrixError(w, http.StatusServiceUnavailable, "M_UNAVAILABLE", "Server unavailable")
		default:
			writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		}
		return
	}

	chunk := make([]publicRoomEntry, 0, len(resp.Rooms))
	for _, r := range resp.Rooms {
		if r == nil {
			continue
		}
		chunk = append(chunk, publicRoomEntry{
			RoomID:           r.RoomId,
			Name:             r.Name,
			Topic:            r.Topic,
			NumJoinedMembers: r.NumJoinedMembers,
			WorldReadable:    r.WorldReadable,
			GuestCanJoin:     r.GuestCanJoin,
		})
	}

	body := publicRoomsResponse{
		Chunk:             chunk,
		NextBatch:         resp.NextCursor,
		TotalRoomCountEst: resp.TotalEstimate,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

// GetPublicRooms handles GET /_matrix/client/v3/publicRooms.
//
// Unauthenticated endpoint (no jwtMiddleware wrapper — looseRL only).
// Query params:
//   - limit (int, optional, default 20, cap 100)
//   - since (string, optional, opaque cursor from previous next_batch)
func (h *PublicRoomsHandler) GetPublicRooms(w http.ResponseWriter, r *http.Request) {
	var limit int32
	if s := r.URL.Query().Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			writeMatrixError(w, http.StatusBadRequest, "M_INVALID_PARAM", "limit must be a non-negative integer")
			return
		}
		limit = int32(n)
	}
	since := r.URL.Query().Get("since")
	h.listPublicRooms(r.Context(), w, limit, since, "")
}

// PostPublicRooms handles POST /_matrix/client/v3/publicRooms.
//
// Authenticated endpoint (jwtMiddleware + bodyLimit1MiB).
// Accepts JSON body: {"limit":N,"since":"...","filter":{"generic_search_term":"foo"}}.
// AC6: JWT required — enforced by jwtMiddleware wrapper in main.go; this handler
//
//	can trust that the user is authenticated.
func (h *PublicRoomsHandler) PostPublicRooms(w http.ResponseWriter, r *http.Request) {
	// Decode optional request body.
	// Both "limit" and "since" at the top level, plus nested "filter.generic_search_term".
	var body struct {
		Limit int32  `json:"limit"`
		Since string `json:"since"`
		Filter *struct {
			GenericSearchTerm string `json:"generic_search_term"`
		} `json:"filter"`
	}

	// Body is optional for POST — an empty body is valid (returns all public rooms).
	if r.Body != nil && r.ContentLength != 0 {
		if !requireJSON(w, r) {
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeMatrixError(w, http.StatusBadRequest, "M_NOT_JSON", "Request body is not valid JSON")
			return
		}
	}

	filterTerm := ""
	if body.Filter != nil {
		filterTerm = body.Filter.GenericSearchTerm
	}

	// Attach caller identity to the gRPC context (x-user-id, x-system-role) so
	// the Elixir Core can audit-log this authenticated POST. JWT presence is
	// already enforced by jwtMiddleware in main.go.
	userID, _ := r.Context().Value(middleware.ContextKeyUserID).(string)
	systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)
	grpcCtx := coregrpc.WithUserMetadata(r.Context(), userID, systemRole)

	h.listPublicRooms(grpcCtx, w, body.Limit, body.Since, filterTerm)
}
