package admin

import "net/http"

// UsersHandler serves the /admin/users master-detail page (Story 7.2).
// Uses stub data until Epic 6 provides the real User Management API.
type UsersHandler struct {
	tmpl *TemplateHandler
}

// NewUsersHandler constructs a UsersHandler with the given template handler.
func NewUsersHandler(tmpl *TemplateHandler) *UsersHandler {
	return &UsersHandler{tmpl: tmpl}
}

// ListHandler handles GET /admin/users — renders the user list with no active selection.
// ActiveItemID is empty; the detail column shows an empty-state placeholder.
func (h *UsersHandler) ListHandler(w http.ResponseWriter, r *http.Request) {
	data := UsersPageData{
		PageData:  PageData{ActiveNav: "users", CSRFToken: CSRFTokenFromContext(r.Context())},
		StubUsers: stubUsers,
		CloseURL:  "/admin/users",
	}
	h.tmpl.render(w, "users", data)
}

// DetailHandler handles GET /admin/users/{userId} — renders the user list with the
// selected user pre-loaded in the detail panel. Always returns HTTP 200; a missing
// user renders a "not found" message inside the panel (AC5: no full-page 404).
func (h *UsersHandler) DetailHandler(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userId")
	data := UsersPageData{
		PageData:     PageData{ActiveNav: "users", CSRFToken: CSRFTokenFromContext(r.Context())},
		StubUsers:    stubUsers,
		ActiveItemID: userID,
		ActiveUser:   findStubUser(userID), // nil if not found → template renders "not found"
		CloseURL:     "/admin/users",
	}
	h.tmpl.render(w, "users", data)
}
