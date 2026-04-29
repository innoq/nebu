package admin

import (
	"net/http"
	"strconv"
	"strings"
)

// RoomsHandler serves the /admin/rooms master-detail page (Story 7.2, extended Story 7.8).
// Uses stub data until Epic 6 provides the real Room Management API.
type RoomsHandler struct {
	tmpl *TemplateHandler
}

// NewRoomsHandler constructs a RoomsHandler with the given template handler.
func NewRoomsHandler(tmpl *TemplateHandler) *RoomsHandler {
	return &RoomsHandler{tmpl: tmpl}
}

// ListHandler handles GET /admin/rooms — renders the filtered, paginated room list.
// Query params: q (search string), visibility (filter: "all"|"public"|"private"), page (0-indexed, default 0).
// Page size is 5. HasMore is true when more results exist beyond the current page.
func (h *RoomsHandler) ListHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	visibility := r.URL.Query().Get("visibility")
	if visibility == "" {
		visibility = "all"
	}
	page := 0
	if n, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && n >= 0 {
		page = n
	}

	filtered := filterStubRooms(stubRooms, q, visibility)

	const pageSize = 5
	start := page * pageSize
	end := start + pageSize
	hasMore := end < len(filtered)
	if start > len(filtered) {
		start = len(filtered)
	}
	if end > len(filtered) {
		end = len(filtered)
	}
	paged := filtered[start:end]

	rows := make([]RoomRowData, len(paged))
	for i, room := range paged {
		rows[i] = toRoomRowData(room)
	}

	data := RoomsPageData{
		PageData: PageData{ActiveNav: "rooms", CSRFToken: CSRFTokenFromContext(r.Context())},
		Rooms:    rows,
		SearchInput: SearchInputData{
			Placeholder: "Search rooms…",
			Value:       q,
			ParamName:   "q",
		},
		FilterBar: []FilterOption{{
			Label:        "Visibility",
			ParamName:    "visibility",
			Options:      []string{"all", "public", "private"},
			CurrentValue: visibility,
		}},
		TotalCount:  len(filtered),
		CurrentPage: page,
		HasMore:     hasMore,
		NextPage:    page + 1,
		EmptyState:  EmptyStateData{Heading: "No rooms found", Description: "Try adjusting your search or filter."},
		CloseURL:    "/admin/rooms",
	}
	h.tmpl.render(w, "rooms", data)
}

// DetailHandler handles GET /admin/rooms/{roomId} — renders the room list with the
// selected room pre-loaded in the detail panel. Always returns HTTP 200; a missing
// room renders a "not found" message inside the panel (AC5: no full-page 404).
func (h *RoomsHandler) DetailHandler(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("roomId")
	data := RoomsPageData{
		PageData:     PageData{ActiveNav: "rooms", CSRFToken: CSRFTokenFromContext(r.Context())},
		Rooms:        toRoomRowDataSlice(stubRooms),
		ActiveItemID: roomID,
		ActiveRoom:   findStubRoom(roomID), // nil if not found → template renders "not found"
		CloseURL:     "/admin/rooms",
	}
	h.tmpl.render(w, "rooms", data)
}

// toRoomRowData converts a StubRoom to a RoomRowData with a pre-computed Badge.
// Normalises StubRoom.Status "archived" → StatusBadgeData{Status: "inactive"}.
// Single source of truth for the status mapping used by ListHandler and DetailHandler.
func toRoomRowData(r StubRoom) RoomRowData {
	badgeStatus := r.Status
	if badgeStatus == "archived" {
		badgeStatus = "inactive"
	}
	return RoomRowData{StubRoom: r, Badge: StatusBadgeData{Status: badgeStatus}}
}

// toRoomRowDataSlice converts a []StubRoom to []RoomRowData using toRoomRowData.
// Convenience helper for DetailHandler and test helpers.
func toRoomRowDataSlice(rooms []StubRoom) []RoomRowData {
	rows := make([]RoomRowData, len(rooms))
	for i, room := range rooms {
		rows[i] = toRoomRowData(room)
	}
	return rows
}

// filterStubRooms returns a filtered slice of StubRoom matching the search query and visibility filter.
// q is a case-insensitive substring match on Name.
// visibility "all" (or empty) returns all rooms unfiltered.
func filterStubRooms(rooms []StubRoom, q, visibility string) []StubRoom {
	result := make([]StubRoom, 0, len(rooms))
	qLower := strings.ToLower(q)
	for _, room := range rooms {
		// Apply search filter
		if qLower != "" {
			if !strings.Contains(strings.ToLower(room.Name), qLower) {
				continue
			}
		}
		// Apply visibility filter
		if visibility != "" && visibility != "all" {
			if room.Visibility != visibility {
				continue
			}
		}
		result = append(result, room)
	}
	return result
}
