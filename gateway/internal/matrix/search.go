package matrix

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	coregrpc "github.com/nebu/nebu/internal/grpc"
	pb "github.com/nebu/nebu/internal/grpc/pb"
	"github.com/nebu/nebu/internal/middleware"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// SearchCoreClient is the consumer-defined interface for the SearchMessages gRPC call.
// Defined here (not in grpc/) so the matrix package owns its dependency contract.
type SearchCoreClient interface {
	SearchMessages(ctx context.Context, req *pb.SearchMessagesRequest) (*pb.SearchMessagesResponse, error)
}

// SearchConfig holds dependencies for the search handler.
type SearchConfig struct {
	CoreClient SearchCoreClient
}

// SearchHandler implements POST /_matrix/client/v3/search (Matrix spec §11.14).
type SearchHandler struct {
	cfg SearchConfig
}

// NewSearchHandler constructs a SearchHandler.
func NewSearchHandler(cfg SearchConfig) *SearchHandler {
	return &SearchHandler{cfg: cfg}
}

// searchRequest mirrors the Matrix spec §11.14 request body.
type searchRequest struct {
	SearchCategories *searchCategories `json:"search_categories"`
}

type searchCategories struct {
	RoomEvents *roomEventsFilter `json:"room_events"`
}

type roomEventsFilter struct {
	SearchTerm string          `json:"search_term"`
	OrderBy    string          `json:"order_by"`
	Limit      int32           `json:"limit"`
	Filter     *eventFilter    `json:"filter"`
	Groupings  *searchGrouping `json:"groupings"`
}

type eventFilter struct {
	Rooms   []string `json:"rooms"`
	Senders []string `json:"senders"`
}

type searchGrouping struct {
	GroupBy []groupByEntry `json:"group_by"`
}

type groupByEntry struct {
	Key string `json:"key"`
}

// PostSearch handles POST /_matrix/client/v3/search.
func (h *SearchHandler) PostSearch(w http.ResponseWriter, r *http.Request) {
	userID, _ := r.Context().Value(middleware.ContextKeyUserID).(string)
	if userID == "" {
		writeMatrixError(w, http.StatusUnauthorized, "M_UNKNOWN_TOKEN", "authentication required")
		return
	}
	systemRole, _ := r.Context().Value(middleware.ContextKeySystemRole).(string)

	var req searchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeMatrixError(w, http.StatusBadRequest, "M_NOT_JSON", "request body is not valid JSON")
		return
	}

	if req.SearchCategories == nil {
		writeMatrixError(w, http.StatusBadRequest, "M_MISSING_PARAM", "missing required field: search_categories")
		return
	}
	if req.SearchCategories.RoomEvents == nil {
		writeMatrixError(w, http.StatusBadRequest, "M_INVALID_PARAM", "search_categories.room_events is required")
		return
	}

	term := strings.TrimSpace(req.SearchCategories.RoomEvents.SearchTerm)
	if term == "" {
		writeMatrixError(w, http.StatusBadRequest, "M_INVALID_PARAM", "search_categories.room_events.search_term must not be empty")
		return
	}

	limit := req.SearchCategories.RoomEvents.Limit
	if limit <= 0 {
		limit = 10
	}

	var roomFilter, senderFilter []string
	if f := req.SearchCategories.RoomEvents.Filter; f != nil {
		roomFilter = f.Rooms
		senderFilter = f.Senders
	}

	nextBatch := r.URL.Query().Get("next_batch")

	grpcCtx := coregrpc.WithUserMetadata(r.Context(), userID, systemRole)

	grpcReq := &pb.SearchMessagesRequest{
		// SECURITY: UserId intentionally NOT set — user_id comes from x-user-id metadata only.
		SearchTerm:   term,
		Limit:        limit,
		RoomFilter:   roomFilter,
		SenderFilter: senderFilter,
		NextBatch:    nextBatch,
	}

	resp, err := h.cfg.CoreClient.SearchMessages(grpcCtx, grpcReq)
	if err != nil {
		st, _ := status.FromError(err)
		switch st.Code() {
		case codes.ResourceExhausted:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"errcode":        "M_LIMIT_EXCEEDED",
				"error":          "search rate limit exceeded",
				"retry_after_ms": 60000,
			})
		case codes.PermissionDenied:
			writeMatrixError(w, http.StatusForbidden, "M_FORBIDDEN", "not a member of any searched room")
		default:
			writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "search failed")
		}
		return
	}

	results, groups := h.buildResults(resp.Results)

	roomEvents := map[string]interface{}{
		"count":      resp.TotalCount,
		"results":    results,
		"highlights": extractHighlights(term),
		"groups":     groups,
	}
	if resp.NextBatch != "" {
		roomEvents["next_batch"] = resp.NextBatch
	}

	body := map[string]interface{}{
		"search_categories": map[string]interface{}{
			"room_events": roomEvents,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(body)
}

// buildResults converts gRPC SearchResult list into Matrix response results and groups-by-room.
func (h *SearchHandler) buildResults(raw []*pb.SearchResult) ([]map[string]interface{}, map[string]interface{}) {
	results := make([]map[string]interface{}, 0, len(raw))
	// groups["room_id"] maps room_id → {results: [event_id,...]}
	byRoom := map[string][]string{}

	for _, r := range raw {
		var event map[string]interface{}
		if err := json.Unmarshal(r.Event, &event); err != nil {
			event = map[string]interface{}{}
		}

		roomID, _ := event["room_id"].(string)
		eventID, _ := event["event_id"].(string)
		if roomID != "" && eventID != "" {
			byRoom[roomID] = append(byRoom[roomID], eventID)
		}

		ctx := buildContext(r)

		results = append(results, map[string]interface{}{
			"rank":    r.Rank,
			"result":  event,
			"context": ctx,
		})
	}

	roomGroups := map[string]interface{}{}
	for roomID, eventIDs := range byRoom {
		roomGroups[roomID] = map[string]interface{}{"results": eventIDs}
	}
	groups := map[string]interface{}{
		"room_id": roomGroups,
	}

	return results, groups
}

func buildContext(r *pb.SearchResult) map[string]interface{} {
	before := decodeEventList(r.EventsBefore)
	after := decodeEventList(r.EventsAfter)
	profiles := map[string]interface{}{}
	for uid, p := range r.ProfileInfo {
		profiles[uid] = map[string]string{
			"displayname": p.Displayname,
			"avatar_url":  p.AvatarUrl,
		}
	}
	return map[string]interface{}{
		"events_before": before,
		"events_after":  after,
		"profile_info":  profiles,
	}
}

func decodeEventList(raw [][]byte) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(raw))
	for _, b := range raw {
		var ev map[string]interface{}
		if err := json.Unmarshal(b, &ev); err == nil {
			out = append(out, ev)
		}
	}
	return out
}

// extractHighlights returns the search terms as highlight tokens.
// Element Web uses this to bold matching words in the UI.
func extractHighlights(term string) []string {
	words := strings.Fields(term)
	seen := map[string]bool{}
	out := make([]string, 0, len(words))
	for _, w := range words {
		lw := strings.ToLower(w)
		if !seen[lw] {
			seen[lw] = true
			out = append(out, lw)
		}
	}
	return out
}
