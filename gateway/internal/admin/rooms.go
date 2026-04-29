package admin

import "net/http"

// RoomsHandler serves the /admin/rooms master-detail page (Story 7.2).
// Uses stub data until Epic 6 provides the real Room Management API.
type RoomsHandler struct {
	tmpl *TemplateHandler
}

// NewRoomsHandler constructs a RoomsHandler with the given template handler.
func NewRoomsHandler(tmpl *TemplateHandler) *RoomsHandler {
	return &RoomsHandler{tmpl: tmpl}
}

// ListHandler handles GET /admin/rooms — renders the room list with no active selection.
// ActiveItemID is empty; the detail column shows an empty-state placeholder.
func (h *RoomsHandler) ListHandler(w http.ResponseWriter, r *http.Request) {
	data := RoomsPageData{
		PageData:  PageData{ActiveNav: "rooms", CSRFToken: CSRFTokenFromContext(r.Context())},
		StubRooms: stubRooms,
		CloseURL:  "/admin/rooms",
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
		StubRooms:    stubRooms,
		ActiveItemID: roomID,
		ActiveRoom:   findStubRoom(roomID), // nil if not found → template renders "not found"
		CloseURL:     "/admin/rooms",
	}
	h.tmpl.render(w, "rooms", data)
}
