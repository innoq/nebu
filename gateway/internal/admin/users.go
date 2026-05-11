package admin

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"unicode/utf8"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/nebu/nebu/internal/grpc/pb"
)

// AdminUsersClient is a minimal consumer-defined interface for the admin user gRPC RPCs.
// *grpc.Client satisfies this interface. In tests, pass nil (triggers stub fallback)
// or inject a fake implementation.
type AdminUsersClient interface {
	ListAdminUsers(ctx context.Context, req *pb.ListAdminUsersRequest) (*pb.ListAdminUsersResponse, error)
	GetAdminUser(ctx context.Context, req *pb.GetAdminUserRequest) (*pb.GetAdminUserResponse, error)
	DeactivateUser(ctx context.Context, req *pb.DeactivateUserRequest) (*pb.DeactivateUserResponse, error)
	ReactivateUser(ctx context.Context, req *pb.ReactivateUserRequest) (*pb.ReactivateUserResponse, error)
	UpdateUserRole(ctx context.Context, req *pb.UpdateUserRoleRequest) (*pb.UpdateUserRoleResponse, error)
}

// UsersHandler serves the /admin/users master-detail page (Story 7.2, extended Story 7.5).
// Story 9.2: backed by real gRPC calls to the Elixir core when core is non-nil.
// Falls back to stub data when core is nil (unit-test path).
type UsersHandler struct {
	tmpl *TemplateHandler
	core AdminUsersClient
}

// NewUsersHandler constructs a UsersHandler with the given template handler and gRPC client.
// Pass nil for core to use stub data (unit-test path; stub fallback is preserved for
// backward compatibility with existing unit tests).
func NewUsersHandler(tmpl *TemplateHandler, core ...AdminUsersClient) *UsersHandler {
	var c AdminUsersClient
	if len(core) > 0 {
		c = core[0]
	}
	return &UsersHandler{tmpl: tmpl, core: c}
}

// protoToStubUser maps an AdminUserProto to a StubUser for template rendering.
// Template compatibility: retaining StubUser avoids a cross-file refactor (see Dev Notes).
func protoToStubUser(u *pb.AdminUserProto) StubUser {
	status := "active"
	if !u.GetIsActive() {
		status = "deactivated"
	}
	return StubUser{
		ID:          u.GetUserId(),
		DisplayName: u.GetDisplayName(),
		Email:       u.GetEmailMasked(),
		Role:        u.GetSystemRole(),
		Status:      status,
	}
}

// ListHandler handles GET /admin/users — renders the filtered, paginated user list.
// When core is set: fetches users from gRPC ListAdminUsers with cursor-based pagination.
// When core is nil: falls back to stub data (backward-compatible for unit tests).
// Query params: q (search string), role (filter), cursor (opaque pagination token), page (legacy).
func (h *UsersHandler) ListHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	role := r.URL.Query().Get("role")
	if role == "" {
		role = "all"
	}
	cursor := r.URL.Query().Get("cursor")

	var users []StubUser
	var hasMore bool
	var nextCursor string
	var totalCount int

	if h.core != nil {
		// --- gRPC path ---
		resp, err := h.core.ListAdminUsers(r.Context(), &pb.ListAdminUsersRequest{
			Limit:  50,
			Cursor: cursor,
			Search: q,
		})
		if err != nil {
			slog.Warn("admin: ListAdminUsers gRPC error", "err", err)
			// Render empty list with no crash; template handles empty slice gracefully.
		} else {
			for _, u := range resp.GetUsers() {
				su := protoToStubUser(u)
				// Apply role filter client-side (gRPC ListAdminUsers filters by search only)
				if role != "" && role != "all" {
					var wantRole string
					switch role {
					case "admin":
						wantRole = "instance_admin"
					default:
						wantRole = role
					}
					if su.Role != wantRole {
						continue
					}
				}
				users = append(users, su)
			}
			nextCursor = resp.GetNextCursor()
			hasMore = nextCursor != ""
			totalCount = int(resp.GetTotal())
		}
	} else {
		// --- stub fallback (nil client, unit-test path) ---
		// Legacy page-based pagination is kept for backward-compatible unit tests.
		pageStr := r.URL.Query().Get("page")
		page := 0
		if pageStr != "" {
			if n := parsePageParam(pageStr); n >= 0 {
				page = n
			}
		}

		filtered := filterStubUsers(stubUsers, q, role)
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
		users = filtered[start:end]
		if hasMore {
			// Encode next page as legacy page param in nextCursor field.
			// UsersPageData.NextPage is still used by the template for legacy pagination.
			nextCursor = ""
		}
	}

	// Compute NextPage for template (legacy page-offset field; unused when cursor is set)
	nextPage := 0
	if cursor == "" {
		// Legacy page param for stub path
		pageStr := r.URL.Query().Get("page")
		if n := parsePageParam(pageStr); n >= 0 {
			nextPage = n + 1
		} else {
			nextPage = 1
		}
	}

	// Build UserRowData slice with pre-computed Badge for each user row.
	rows := make([]UserRowData, len(users))
	for i, u := range users {
		rows[i] = toUserRowData(u)
	}

	usersListPD := newPageData()
	usersListPD.ActiveNav = "users"
	usersListPD.CSRFToken = CSRFTokenFromContext(r.Context())
	data := UsersPageData{
		PageData: usersListPD,
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
		TotalCount:  totalCount,
		CurrentPage: 0,
		HasMore:     hasMore,
		NextPage:    nextPage,
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

	var user *StubUser
	var sidebarUsers []StubUser

	if h.core != nil {
		// --- gRPC path: fetch the single user ---
		resp, err := h.core.GetAdminUser(r.Context(), &pb.GetAdminUserRequest{UserId: userID})
		if err != nil {
			if st, ok := status.FromError(err); ok && st.Code() == codes.NotFound {
				http.NotFound(w, r)
				return
			}
			slog.Warn("admin: GetAdminUser gRPC error", "user_id", userID, "err", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		u := protoToStubUser(resp.GetUser())
		user = &u

		// Fetch sidebar list (limit=100, no search/filter)
		listResp, listErr := h.core.ListAdminUsers(r.Context(), &pb.ListAdminUsersRequest{Limit: 100})
		if listErr != nil {
			slog.Warn("admin: ListAdminUsers (sidebar) gRPC error", "err", listErr)
			// Continue with empty sidebar; detail panel still renders
		} else {
			for _, u := range listResp.GetUsers() {
				sidebarUsers = append(sidebarUsers, protoToStubUser(u))
			}
		}
	} else {
		// --- stub fallback (nil client, unit-test path) ---
		user = findStubUser(userID)
		if user == nil {
			http.NotFound(w, r)
			return
		}
		sidebarUsers = stubUsers
	}

	if user == nil {
		http.NotFound(w, r)
		return
	}

	// Read flash query param; populate AlertBannerData if present (Story 7.6 AC1).
	var flash AlertBannerData
	if msg := sanitizeFlash(r.URL.Query().Get("flash")); msg != "" {
		flash = AlertBannerData{Severity: "success", Message: msg, Dismissible: true}
	}

	// Build UserRowData slice for the sidebar list (all users, unfiltered).
	rows := make([]UserRowData, len(sidebarUsers))
	for i, u := range sidebarUsers {
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

	// Pre-compute ConfirmDialogData for the deactivation confirm_dialog (Story 7.7).
	confirmDialog := ConfirmDialogData{
		Title:        "Deactivate user",
		Message:      "This will immediately invalidate all active sessions for " + user.DisplayName + ". Are you sure?",
		ConfirmLabel: "Deactivate",
		ConfirmClass: "btn-error",
		FormAction:   "/admin/users/" + userID + "/deactivate",
		HiddenFields: nil,
		CSRFToken:    csrfToken,
	}

	usersDetailPD := newPageData()
	usersDetailPD.ActiveNav = "users"
	usersDetailPD.CSRFToken = csrfToken
	data := UsersPageData{
		PageData:                usersDetailPD,
		Users:                   rows,
		ActiveItemID:            userID,
		ActiveUser:              user,
		CloseURL:                "/admin/users",
		Flash:                   flash,
		ActiveUserInlineEdit:    inlineEdit,
		ActiveUserStatusBadge:   statusBadge,
		ActiveUserInitial:       initial,
		ActiveUserConfirmDialog: confirmDialog,
		ActiveUserRoleOptions:   []string{"instance_admin", "compliance_officer", "user"},
		ActiveUserRoleValue:     user.Role,
	}
	h.tmpl.render(w, "users", data)
}

// UpdateRoleHandler handles POST /admin/users/{userId}/role.
// Validates and updates the user's role via gRPC UpdateUserRole (Story 9.2).
// Falls back to stub mutation when core is nil (unit-test path).
func (h *UsersHandler) UpdateRoleHandler(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userId")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	role := r.FormValue("role")
	validRoles := map[string]bool{"instance_admin": true, "compliance_officer": true, "user": true}
	if !validRoles[role] {
		http.Error(w, "invalid role value", http.StatusBadRequest)
		return
	}

	if h.core != nil {
		// gRPC path — enrich context with admin identity so Core audit log records actor_user_id.
		grpcCtx := contextWithAdminIdentity(r.Context(), AdminSubFromContext(r.Context()))
		_, err := h.core.UpdateUserRole(grpcCtx, &pb.UpdateUserRoleRequest{
			UserId: userID,
			Role:   role,
		})
		if err != nil {
			if st, ok := status.FromError(err); ok && st.Code() == codes.NotFound {
				http.Redirect(w, r, "/admin/users/"+userID+"?flash=User+not+found", http.StatusFound)
				return
			}
			slog.Warn("admin: UpdateUserRole gRPC error", "user_id", userID, "err", err)
			http.Redirect(w, r, "/admin/users/"+userID+"?flash=Error+updating+role", http.StatusFound)
			return
		}
	} else {
		// stub fallback (nil client, unit-test path)
		for i := range stubUsers {
			if stubUsers[i].ID == userID {
				stubUsers[i].Role = role
				break
			}
		}
	}

	http.Redirect(w, r, "/admin/users/"+userID+"?flash=Role+updated", http.StatusFound)
}

// DeactivateUserHandler handles POST /admin/users/{userId}/deactivate.
// Calls gRPC DeactivateUser to set is_active=false and invalidate all sessions (Story 9.2).
// Falls back to stub mutation when core is nil (unit-test path).
func (h *UsersHandler) DeactivateUserHandler(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userId")

	if h.core != nil {
		// gRPC path — enrich context with admin identity so Core audit log records actor_user_id.
		grpcCtx := contextWithAdminIdentity(r.Context(), AdminSubFromContext(r.Context()))
		_, err := h.core.DeactivateUser(grpcCtx, &pb.DeactivateUserRequest{UserId: userID})
		if err != nil {
			if st, ok := status.FromError(err); ok && st.Code() == codes.NotFound {
				http.Redirect(w, r, "/admin/users/"+userID+"?flash=User+not+found", http.StatusFound)
				return
			}
			slog.Warn("admin: DeactivateUser gRPC error", "user_id", userID, "err", err)
			http.Redirect(w, r, "/admin/users/"+userID+"?flash=Error+deactivating+user", http.StatusFound)
			return
		}
	} else {
		// stub fallback (nil client, unit-test path)
		for i := range stubUsers {
			if stubUsers[i].ID == userID {
				stubUsers[i].Status = "deactivated"
				break
			}
		}
	}

	http.Redirect(w, r, "/admin/users/"+userID+"?flash=User+deactivated", http.StatusFound)
}

// ReactivateUserHandler handles POST /admin/users/{userId}/reactivate.
// Calls gRPC ReactivateUser to set is_active=true (Story 9.2).
// Falls back to stub mutation when core is nil (unit-test path).
// Used by Playwright smoke-flow specs (Story 7.14) to restore user state in afterEach.
func (h *UsersHandler) ReactivateUserHandler(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userId")

	if h.core != nil {
		// gRPC path — enrich context with admin identity so Core audit log records actor_user_id.
		grpcCtx := contextWithAdminIdentity(r.Context(), AdminSubFromContext(r.Context()))
		_, err := h.core.ReactivateUser(grpcCtx, &pb.ReactivateUserRequest{UserId: userID})
		if err != nil {
			if st, ok := status.FromError(err); ok && st.Code() == codes.NotFound {
				http.Redirect(w, r, "/admin/users/"+userID+"?flash=User+not+found", http.StatusFound)
				return
			}
			slog.Warn("admin: ReactivateUser gRPC error", "user_id", userID, "err", err)
			http.Redirect(w, r, "/admin/users/"+userID+"?flash=Error+reactivating+user", http.StatusFound)
			return
		}
	} else {
		// stub fallback (nil client, unit-test path)
		for i := range stubUsers {
			if stubUsers[i].ID == userID {
				stubUsers[i].Status = "active"
				break
			}
		}
	}

	http.Redirect(w, r, "/admin/users/"+userID+"?flash=User+reactivated", http.StatusFound)
}

// UpdateDisplayNameHandler handles POST /admin/users/{userId}/display-name.
// Validates and updates the user's display name in-memory (stub phase).
// NOTE: Admin display-name update requires a dedicated gRPC RPC — deferred to a follow-up story.
// Story 9.1 did not add an UpdateUserDisplayName RPC to proto/core.proto, so this handler
// continues to use stub mutation for the unit-test path and is a no-op for production
// (it will be wired to a real RPC in a follow-up story for Epic 10).
func (h *UsersHandler) UpdateDisplayNameHandler(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userId")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	displayName := strings.TrimSpace(r.FormValue("display_name"))
	if displayName == "" || utf8.RuneCountInString(displayName) > 100 {
		http.Error(w, "display_name must be 1–100 characters", http.StatusBadRequest)
		return
	}
	// display name update via gRPC deferred to Epic 10 (no Core RPC yet).
	// Mutate stub data in-memory for unit-test path only (changes lost on restart).
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

// parsePageParam parses a page query param string to an int.
// Returns -1 if parsing fails or the value is negative.
func parsePageParam(s string) int {
	if s == "" {
		return 0
	}
	n := 0
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return -1
		}
		n = n*10 + int(ch-'0')
	}
	return n
}
