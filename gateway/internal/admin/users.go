package admin

import (
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
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
// selected user pre-loaded in the detail panel.
// Returns HTTP 404 when no user matches (Story 7.6 behaviour change from Story 7.2).
// Reads the ?flash= query param and populates Flash with an AlertBanner on success.
func (h *UsersHandler) DetailHandler(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userId")
	user := findStubUser(userID)
	if user == nil {
		http.NotFound(w, r)
		return
	}

	// Read flash query param; populate AlertBannerData if present (Story 7.6 AC1).
	var flash AlertBannerData
	if msg := r.URL.Query().Get("flash"); msg != "" {
		flash = AlertBannerData{Severity: "success", Message: msg, Dismissible: true}
	}

	// Build UserRowData slice for the sidebar list (all users, unfiltered).
	rows := make([]UserRowData, len(stubUsers))
	for i, u := range stubUsers {
		rows[i] = toUserRowData(u)
	}

	// Pre-compute InlineEditData for the inline_edit component (Story 7.6 AC3).
	csrfToken := CSRFTokenFromContext(r.Context())
	inlineEdit := InlineEditData{
		ID:         "display-name",
		FieldName:  "display_name",
		Value:      user.DisplayName,
		Label:      "Display Name",
		FormAction: "/admin/users/" + userID + "/display-name",
		CSRFToken:  csrfToken,
	}

	// Pre-compute StatusBadgeData; normalise "deactivated" → "inactive" (Story 7.6 AC3).
	badgeStatus := user.Status
	if badgeStatus == "deactivated" {
		badgeStatus = "inactive"
	}
	statusBadge := StatusBadgeData{Status: badgeStatus}

	// Pre-compute user initials using rune-safe slice (Story 7.6 Dev Notes).
	// TODO: use rune-aware initials helper in production when multi-char initials are needed.
	initial := ""
	if runes := []rune(user.DisplayName); len(runes) > 0 {
		initial = string(runes[0:1])
	}

	data := UsersPageData{
		PageData:              PageData{ActiveNav: "users", CSRFToken: csrfToken},
		Users:                 rows,
		ActiveItemID:          userID,
		ActiveUser:            user,
		CloseURL:              "/admin/users",
		Flash:                 flash,
		ActiveUserInlineEdit:  inlineEdit,
		ActiveUserStatusBadge: statusBadge,
		ActiveUserInitial:     initial,
	}
	h.tmpl.render(w, "users", data)
}

// UpdateDisplayNameHandler handles POST /admin/users/{userId}/display-name.
// Validates and updates the user's display name in-memory (stub phase).
// TODO(epic-6): replace stub mutation with Admin API call when Epic 6 is implemented.
func (h *UsersHandler) UpdateDisplayNameHandler(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userId")
	// TODO(story-7-csrf): enforce CSRF middleware when wiring in production
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	displayName := strings.TrimSpace(r.FormValue("display_name"))
	if displayName == "" || utf8.RuneCountInString(displayName) > 100 {
		http.Error(w, "display_name must be 1–100 characters", http.StatusBadRequest)
		return
	}
	// Mutate stub data in-memory (changes lost on restart — acceptable for stub phase).
	for i := range stubUsers {
		if stubUsers[i].ID == userID {
			stubUsers[i].DisplayName = displayName
			break
		}
	}
	http.Redirect(w, r, "/admin/users/"+userID+"?flash=Display+name+updated", http.StatusFound)
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
