package admin

import (
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
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
// selected room pre-loaded in the detail panel.
// Returns HTTP 404 when no room matches roomId (Story 7.9 behaviour change from Story 7.2).
// Reads the ?flash= query param and populates Flash with an AlertBanner on success.
func (h *RoomsHandler) DetailHandler(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("roomId")
	room := findStubRoom(roomID)
	if room == nil {
		http.NotFound(w, r)
		return
	}

	// Read flash query param; populate AlertBannerData if present (Story 7.9 AC1).
	var flash AlertBannerData
	if msg := r.URL.Query().Get("flash"); msg != "" {
		flash = AlertBannerData{Severity: "success", Message: msg, Dismissible: true}
	}

	// Pre-compute InlineEditData for the inline_edit component (Story 7.9 AC1).
	csrfToken := CSRFTokenFromContext(r.Context())
	inlineEdit := InlineEditData{
		ID:         "room-name",
		FieldName:  "name",
		Value:      room.Name,
		Label:      "Room Name",
		FormAction: "/admin/rooms/" + roomID + "/name",
		CSRFToken:  csrfToken,
	}

	// Pre-compute StatusBadgeData; normalise "archived" → "inactive" (Story 7.9 AC1).
	badgeStatus := room.Status
	if badgeStatus == "archived" {
		badgeStatus = "inactive"
	}
	statusBadge := StatusBadgeData{Status: badgeStatus}

	// Pre-compute ConfirmDialogData for the archive confirm_dialog (Story 7.9 AC1).
	confirmDialog := ConfirmDialogData{
		Title:        "Archive room",
		Message:      "This will archive " + room.Name + ". Are you sure?",
		ConfirmLabel: "Archive",
		ConfirmClass: "btn-error",
		FormAction:   "/admin/rooms/" + roomID + "/archive",
		CSRFToken:    csrfToken,
	}

	// Pre-compute room initial using rune-safe slice (Story 7.9 Dev Notes).
	// TODO: use rune-aware initials helper in production when multi-char initials are needed.
	initial := ""
	if runes := []rune(room.Name); len(runes) > 0 {
		initial = string(runes[0:1])
	}

	data := RoomsPageData{
		PageData:                PageData{ActiveNav: "rooms", CSRFToken: csrfToken},
		Rooms:                   toRoomRowDataSlice(stubRooms),
		ActiveItemID:            roomID,
		ActiveRoom:              room,
		CloseURL:                "/admin/rooms",
		Flash:                   flash,
		ActiveRoomInlineEdit:    inlineEdit,
		ActiveRoomStatusBadge:   statusBadge,
		ActiveRoomConfirmDialog: confirmDialog,
		ActiveRoomInitial:       initial,
	}
	h.tmpl.render(w, "rooms", data)
}

// UpdateRoomNameHandler handles POST /admin/rooms/{roomId}/name.
// Validates and updates the room's name in-memory (stub phase).
// TODO(epic-6): replace stub mutation with Admin API call when Epic 6 is implemented.
func (h *RoomsHandler) UpdateRoomNameHandler(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("roomId")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" || utf8.RuneCountInString(name) > 100 {
		http.Error(w, "name must be 1–100 characters", http.StatusBadRequest)
		return
	}
	// Mutate stub data in-memory (changes lost on restart — acceptable for stub phase).
	found := false
	for i := range stubRooms {
		if stubRooms[i].ID == roomID {
			stubRooms[i].Name = name
			found = true
			break
		}
	}
	if !found {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/admin/rooms/"+roomID+"?flash=Name+updated", http.StatusFound)
}

// ArchiveRoomHandler handles POST /admin/rooms/{roomId}/archive.
// Sets Status = "archived" in-memory (stub phase).
// TODO(epic-6): replace stub mutation with Admin API call (POST /api/v1/admin/rooms/{roomId}/archive).
func (h *RoomsHandler) ArchiveRoomHandler(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("roomId")
	found := false
	for i := range stubRooms {
		if stubRooms[i].ID == roomID {
			stubRooms[i].Status = "archived"
			found = true
			break
		}
	}
	if !found {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/admin/rooms/"+roomID+"?flash=Room+archived", http.StatusFound)
}

// UnarchiveRoomHandler handles POST /admin/rooms/{roomId}/unarchive.
// Restores Status = "active" in-memory (stub phase — inverse of ArchiveRoomHandler).
// Used by Playwright smoke-flow specs (Story 7.14) to restore stub state in afterEach.
// TODO(epic-6): replace stub mutation with Admin API call when Epic 6 is implemented.
func (h *RoomsHandler) UnarchiveRoomHandler(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("roomId")
	found := false
	for i := range stubRooms {
		if stubRooms[i].ID == roomID {
			stubRooms[i].Status = "active"
			found = true
			break
		}
	}
	if !found {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/admin/rooms/"+roomID+"?flash=Room+unarchived", http.StatusFound)
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
