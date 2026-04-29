package admin

import (
	"net/http"
	"strconv"
	"strings"
)

// UsersHandler serves the /admin/users master-detail page (Story 7.2, extended Story 7.5).
// Uses stub data until Epic 6 provides the real User Management API.
type UsersHandler struct {
	tmpl *TemplateHandler
}

// NewUsersHandler constructs a UsersHandler with the given template handler.
func NewUsersHandler(tmpl *TemplateHandler) *UsersHandler {
	return &UsersHandler{tmpl: tmpl}
}

// ListHandler handles GET /admin/users — renders the filtered, paginated user list.
// Query params: q (search string), role (filter), page (0-indexed, default 0).
// Page size is 5. HasMore is true when more results exist beyond the current page.
func (h *UsersHandler) ListHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	role := r.URL.Query().Get("role")
	if role == "" {
		role = "all"
	}
	pageStr := r.URL.Query().Get("page")
	page := 0
	if n, err := strconv.Atoi(pageStr); err == nil && n >= 0 {
		page = n
	}

	filtered := filterStubUsers(stubUsers, q, role)

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

	// Build UserRowData slice with pre-computed Badge for each user row.
	rows := make([]UserRowData, len(paged))
	for i, u := range paged {
		rows[i] = toUserRowData(u)
	}

	data := UsersPageData{
		PageData: PageData{ActiveNav: "users", CSRFToken: CSRFTokenFromContext(r.Context())},
		Users:    rows,
		SearchInput: SearchInputData{
			Placeholder: "Search users…",
			Value:       q,
			ParamName:   "q",
		},
		FilterBar: []FilterOption{{
			Label:        "Role",
			ParamName:    "role",
			Options:      []string{"all", "admin", "compliance_officer", "user"},
			CurrentValue: role,
		}},
		TotalCount:  len(filtered),
		CurrentPage: page,
		HasMore:     hasMore,
		NextPage:    page + 1,
		EmptyState:  EmptyStateData{Heading: "No users found", Description: "Try adjusting your search or filter."},
		CloseURL:    "/admin/users",
	}
	h.tmpl.render(w, "users", data)
}

// DetailHandler handles GET /admin/users/{userId} — renders the user list with the
// selected user pre-loaded in the detail panel. Always returns HTTP 200; a missing
// user renders a "not found" message inside the panel (AC5: no full-page 404).
func (h *UsersHandler) DetailHandler(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userId")

	// Build UserRowData slice for the sidebar list (all users, unfiltered).
	rows := make([]UserRowData, len(stubUsers))
	for i, u := range stubUsers {
		rows[i] = toUserRowData(u)
	}

	data := UsersPageData{
		PageData:     PageData{ActiveNav: "users", CSRFToken: CSRFTokenFromContext(r.Context())},
		Users:        rows,
		ActiveItemID: userID,
		ActiveUser:   findStubUser(userID), // nil if not found → template renders "not found"
		CloseURL:     "/admin/users",
	}
	h.tmpl.render(w, "users", data)
}

// toUserRowData converts a StubUser to a UserRowData with a pre-computed Badge.
// Normalises StubUser.Status "deactivated" → StatusBadgeData{Status: "inactive"}.
// Single source of truth for the status mapping used by ListHandler and DetailHandler.
func toUserRowData(u StubUser) UserRowData {
	badgeStatus := u.Status
	if badgeStatus == "deactivated" {
		badgeStatus = "inactive"
	}
	return UserRowData{StubUser: u, Badge: StatusBadgeData{Status: badgeStatus}}
}

// filterStubUsers returns a filtered slice of StubUser matching the search query and role filter.
// q is a case-insensitive substring match on DisplayName or Email.
// role maps URL-friendly values to StubUser.Role: "admin"→"instance_admin", others are direct matches.
// role "all" (or empty) returns all users unfiltered.
func filterStubUsers(users []StubUser, q, role string) []StubUser {
	result := make([]StubUser, 0, len(users))
	qLower := strings.ToLower(q)
	for _, u := range users {
		// Apply search filter
		if qLower != "" {
			if !strings.Contains(strings.ToLower(u.DisplayName), qLower) &&
				!strings.Contains(strings.ToLower(u.Email), qLower) {
				continue
			}
		}
		// Apply role filter
		if role != "" && role != "all" {
			var wantRole string
			switch role {
			case "admin":
				wantRole = "instance_admin"
			default:
				wantRole = role // "compliance_officer", "user" map directly
			}
			if u.Role != wantRole {
				continue
			}
		}
		result = append(result, u)
	}
	return result
}
