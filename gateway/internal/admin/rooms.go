package admin

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/nebu/nebu/internal/grpc/pb"
)

// AdminRoomsClient is a minimal consumer-defined interface for the admin room gRPC RPCs.
// *grpc.Client satisfies this interface. In tests, pass nil (triggers stub fallback)
// or inject a fake implementation.
type AdminRoomsClient interface {
	ListAdminRooms(ctx context.Context, req *pb.ListAdminRoomsRequest) (*pb.ListAdminRoomsResponse, error)
	GetAdminRoom(ctx context.Context, req *pb.GetAdminRoomRequest) (*pb.GetAdminRoomResponse, error)
	ArchiveRoom(ctx context.Context, req *pb.ArchiveRoomRequest) (*pb.ArchiveRoomResponse, error)
	UnarchiveRoom(ctx context.Context, req *pb.UnarchiveRoomRequest) (*pb.UnarchiveRoomResponse, error)
	UpdateRoomSettings(ctx context.Context, req *pb.UpdateRoomSettingsRequest) (*pb.UpdateRoomSettingsResponse, error)
}

// RoomsHandler serves the /admin/rooms master-detail page (Story 7.2, extended Story 7.8).
// Story 9.3: backed by real gRPC calls to the Elixir core when core is non-nil.
// Falls back to stub data when core is nil (unit-test path).
type RoomsHandler struct {
	tmpl *TemplateHandler
	core AdminRoomsClient
}

// NewRoomsHandler constructs a RoomsHandler with the given template handler and optional gRPC client.
// Pass nil (or omit) for core to use stub data (unit-test path; stub fallback is preserved for
// backward compatibility with existing unit tests).
func NewRoomsHandler(tmpl *TemplateHandler, core ...AdminRoomsClient) *RoomsHandler {
	var c AdminRoomsClient
	if len(core) > 0 {
		c = core[0]
	}
	return &RoomsHandler{tmpl: tmpl, core: c}
}

// protoToStubRoomSummary maps an AdminRoomProto (list view) to a StubRoom.
// AdminRoomProto has no MaxMembers or Visibility (those are detail-only fields).
func protoToStubRoomSummary(r *pb.AdminRoomProto) StubRoom {
	return StubRoom{
		ID:          r.GetRoomId(),
		Name:        r.GetName(),
		MemberCount: int(r.GetMemberCount()),
		Status:      r.GetStatus(),
	}
}

// protoToStubRoom maps an AdminRoomDetailProto (detail view) to a *StubRoom.
// Includes MaxMembers and Visibility which are only present in the detail proto.
func protoToStubRoom(r *pb.AdminRoomDetailProto) *StubRoom {
	return &StubRoom{
		ID:          r.GetRoomId(),
		Name:        r.GetName(),
		Visibility:  r.GetVisibility(),
		MemberCount: int(r.GetMemberCount()),
		MaxMembers:  int(r.GetMaxMembers()),
		Status:      r.GetStatus(),
	}
}

// ListHandler handles GET /admin/rooms — renders the filtered, paginated room list.
// When core is set: fetches rooms from gRPC ListAdminRooms with cursor-based pagination.
// When core is nil: falls back to stub data (backward-compatible for unit tests).
// Query params: q (search string), visibility (filter: "all"|"public"|"private"), page (legacy), cursor (opaque).
func (h *RoomsHandler) ListHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	visibility := r.URL.Query().Get("visibility")
	if visibility == "" {
		visibility = "all"
	}
	cursor := r.URL.Query().Get("cursor")

	var rooms []StubRoom
	var hasMore bool
	var nextCursor string
	var totalCount int

	if h.core != nil {
		// --- gRPC path ---
		// NOTE: `Search` is forwarded to the server even though the current backend
		// implementation ignores `_search` — forward-compatible with future server-side
		// search (mirrors the users.go pattern from Story 9.2).
		resp, err := h.core.ListAdminRooms(r.Context(), &pb.ListAdminRoomsRequest{
			Limit:  50,
			Cursor: cursor,
			Search: q,
		})
		if err != nil {
			slog.Warn("admin: ListAdminRooms gRPC error", "err", err)
			// Render empty list gracefully — do not panic.
		} else {
			for _, room := range resp.GetRooms() {
				sr := protoToStubRoomSummary(room)
				// Belt-and-braces: apply search filter client-side too in case
				// the backend has not yet implemented `search` filtering.
				if q != "" && !strings.Contains(strings.ToLower(sr.Name), strings.ToLower(q)) {
					continue
				}
				// NOTE: The `visibility` filter ("public"/"private") cannot be applied
				// server-side because `AdminRoomProto` (list summary) has no visibility
				// field, and `ListAdminRoomsRequest` has no `visibility_filter` field.
				// Applying it client-side against `sr.Visibility` would always fail
				// (the field is empty in the summary view), silently hiding all rooms.
				// Until the proto adds visibility to the summary, treat the filter as
				// a no-op on the gRPC path. TODO: extend proto to expose visibility on
				// the room summary or add a server-side visibility_filter field.
				rooms = append(rooms, sr)
			}
			nextCursor = resp.GetNextCursor()
			hasMore = nextCursor != ""
			// Use the server-reported total when available; falls back to page count
			// for backends that don't track a separate total (mirrors users.go).
			if total := int(resp.GetTotal()); total > 0 {
				totalCount = total
			} else {
				totalCount = len(rooms)
			}
		}
	} else {
		// --- stub fallback (nil client, unit-test path) ---
		page := 0
		if n, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && n >= 0 {
			page = n
		}

		filtered := filterStubRooms(stubRooms, q, visibility)
		totalCount = len(filtered)

		const pageSize = 5
		start := page * pageSize
		end := start + pageSize
		hasMore = end < len(filtered)
		if start > len(filtered) {
			start = len(filtered)
		}
		if end > len(filtered) {
			end = len(filtered)
		}
		rooms = filtered[start:end]
	}

	// Compute NextPage for template (legacy page-offset field; unused when cursor is set)
	nextPage := 0
	if cursor == "" {
		pageStr := r.URL.Query().Get("page")
		if n, err := strconv.Atoi(pageStr); err == nil && n >= 0 {
			nextPage = n + 1
		} else {
			nextPage = 1
		}
	}

	rows := make([]RoomRowData, len(rooms))
	for i, room := range rooms {
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
		TotalCount:  totalCount,
		CurrentPage: 0,
		HasMore:     hasMore,
		NextPage:    nextPage,
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

	var room *StubRoom
	var sidebarRooms []StubRoom

	if h.core != nil {
		// --- gRPC path: fetch the single room ---
		resp, err := h.core.GetAdminRoom(r.Context(), &pb.GetAdminRoomRequest{RoomId: roomID})
		if err != nil {
			if st, ok := status.FromError(err); ok && st.Code() == codes.NotFound {
				http.NotFound(w, r)
				return
			}
			slog.Warn("admin: GetAdminRoom gRPC error", "room_id", roomID, "err", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		room = protoToStubRoom(resp.GetRoom())

		// Fetch sidebar list (limit=100, no search/filter)
		listResp, listErr := h.core.ListAdminRooms(r.Context(), &pb.ListAdminRoomsRequest{Limit: 100})
		if listErr != nil {
			slog.Warn("admin: ListAdminRooms (sidebar) gRPC error", "err", listErr)
			// Continue with empty sidebar; detail panel still renders.
		} else {
			for _, rm := range listResp.GetRooms() {
				sidebarRooms = append(sidebarRooms, protoToStubRoomSummary(rm))
			}
		}
	} else {
		// --- stub fallback (nil client, unit-test path) ---
		room = findStubRoom(roomID)
		if room == nil {
			http.NotFound(w, r)
			return
		}
		sidebarRooms = stubRooms
	}

	if room == nil {
		http.NotFound(w, r)
		return
	}

	// Read flash query param; populate AlertBannerData if present (Story 7.9 AC1).
	var flash AlertBannerData
	if msg := sanitizeFlash(r.URL.Query().Get("flash")); msg != "" {
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
	// Story 9.15 AC3: rooms with empty Name (Direct Chats) use the same fallback as the list/detail title.
	roomDisplayName := room.Name
	if roomDisplayName == "" {
		roomDisplayName = fmt.Sprintf("(Direct Chat · %d members)", room.MemberCount)
	}
	confirmDialog := ConfirmDialogData{
		Title:        "Archive room",
		Message:      "This will archive " + roomDisplayName + ". Are you sure?",
		ConfirmLabel: "Archive",
		ConfirmClass: "btn-error",
		FormAction:   "/admin/rooms/" + roomID + "/archive",
		CSRFToken:    csrfToken,
	}

	// Pre-compute room initial using rune-safe slice (Story 7.9 Dev Notes).
	// Story 9.15: rooms with empty Name (Direct Chats) fall back to "·" (middle dot)
	// so the avatar circle is never visually empty.
	// TODO: use rune-aware initials helper in production when multi-char initials are needed.
	initial := "·"
	if runes := []rune(room.Name); len(runes) > 0 {
		initial = string(runes[0:1])
	}

	data := RoomsPageData{
		PageData:                PageData{ActiveNav: "rooms", CSRFToken: csrfToken},
		Rooms:                   toRoomRowDataSlice(sidebarRooms),
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
// Validates and updates the room's name.
// When core is set: calls UpdateRoomSettings gRPC (max_members only; name update is deferred
// as there is no dedicated UpdateRoomName RPC — falls back to stub mutation for name field).
// When core is nil: mutates stub data in-memory (unit-test path).
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

	if h.core != nil {
		// No dedicated UpdateRoomName gRPC RPC exists yet — deferred to a follow-up story.
		// Return a user-visible explanation rather than silently 404ing on real room IDs.
		http.Redirect(w, r, "/admin/rooms/"+roomID+"?flash=Name+update+not+yet+available", http.StatusFound)
		return
	}
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
	http.Redirect(w, r, "/admin/rooms/"+roomID+"?flash=Room+name+updated", http.StatusFound)
}

// UpdateRoomSettingsHandler handles POST /admin/rooms/{roomId}/settings.
// Reads max_members from the form and calls the gRPC UpdateRoomSettings RPC.
// When core is nil: falls back to no-op / stub path (unit-test path).
// Note: UpdateRoomSettingsRequest only carries max_members; visibility is not in the proto.
func (h *RoomsHandler) UpdateRoomSettingsHandler(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("roomId")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	maxMembersStr := strings.TrimSpace(r.FormValue("max_members"))
	maxMembers := int32(0)
	if maxMembersStr != "" {
		n, err := strconv.Atoi(maxMembersStr)
		if err != nil || n < 0 || n > 1_000_000 {
			http.Error(w, "max_members must be a non-negative integer (0 = no limit)", http.StatusBadRequest)
			return
		}
		maxMembers = int32(n)
	}

	if h.core != nil {
		// gRPC path — enrich context with admin identity so Core audit log records actor_user_id.
		grpcCtx := contextWithAdminIdentity(r.Context(), AdminSubFromContext(r.Context()))
		_, err := h.core.UpdateRoomSettings(grpcCtx, &pb.UpdateRoomSettingsRequest{
			RoomId:     roomID,
			MaxMembers: maxMembers,
		})
		if err != nil {
			if st, ok := status.FromError(err); ok && st.Code() == codes.NotFound {
				http.Redirect(w, r, "/admin/rooms/"+roomID+"?flash=Room+not+found", http.StatusFound)
				return
			}
			slog.Warn("admin: UpdateRoomSettings gRPC error", "room_id", roomID, "err", err)
			http.Redirect(w, r, "/admin/rooms/"+roomID+"?flash=Error+updating+settings", http.StatusFound)
			return
		}
	}
	// stub fallback (nil client, unit-test path): no-op — max_members has no stub field to mutate

	http.Redirect(w, r, "/admin/rooms/"+roomID+"?flash=Settings+updated", http.StatusFound)
}

// ArchiveRoomHandler handles POST /admin/rooms/{roomId}/archive.
// When core is set: calls the gRPC ArchiveRoom RPC.
// When core is nil: falls back to stub mutation (unit-test path).
func (h *RoomsHandler) ArchiveRoomHandler(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("roomId")

	if h.core != nil {
		// gRPC path — enrich context with admin identity so Core audit log records actor_user_id.
		grpcCtx := contextWithAdminIdentity(r.Context(), AdminSubFromContext(r.Context()))
		_, err := h.core.ArchiveRoom(grpcCtx, &pb.ArchiveRoomRequest{RoomId: roomID})
		if err != nil {
			if st, ok := status.FromError(err); ok && st.Code() == codes.NotFound {
				http.Redirect(w, r, "/admin/rooms/"+roomID+"?flash=Room+not+found", http.StatusFound)
				return
			}
			slog.Warn("admin: ArchiveRoom gRPC error", "room_id", roomID, "err", err)
			http.Redirect(w, r, "/admin/rooms/"+roomID+"?flash=Error+archiving+room", http.StatusFound)
			return
		}
	} else {
		// stub fallback (nil client, unit-test path)
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
	}

	http.Redirect(w, r, "/admin/rooms/"+roomID+"?flash=Room+archived", http.StatusFound)
}

// UnarchiveRoomHandler handles POST /admin/rooms/{roomId}/unarchive.
// When core is set: calls the gRPC UnarchiveRoom RPC.
// When core is nil: falls back to stub mutation (unit-test path).
// Used by Playwright smoke-flow specs (Story 7.14) to restore stub state in afterEach.
func (h *RoomsHandler) UnarchiveRoomHandler(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("roomId")

	if h.core != nil {
		// gRPC path — enrich context with admin identity so Core audit log records actor_user_id.
		grpcCtx := contextWithAdminIdentity(r.Context(), AdminSubFromContext(r.Context()))
		_, err := h.core.UnarchiveRoom(grpcCtx, &pb.UnarchiveRoomRequest{RoomId: roomID})
		if err != nil {
			if st, ok := status.FromError(err); ok && st.Code() == codes.NotFound {
				http.Redirect(w, r, "/admin/rooms/"+roomID+"?flash=Room+not+found", http.StatusFound)
				return
			}
			slog.Warn("admin: UnarchiveRoom gRPC error", "room_id", roomID, "err", err)
			http.Redirect(w, r, "/admin/rooms/"+roomID+"?flash=Error+unarchiving+room", http.StatusFound)
			return
		}
	} else {
		// stub fallback (nil client, unit-test path)
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
