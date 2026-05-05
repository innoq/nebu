package admin

// PageData holds template data passed to all Admin UI page renders.
// BootstrapMode controls sidebar Bootstrap nav item visibility.
// LoginMode suppresses authenticated navigation on the login page (Story 9.13 AC2).
// ActiveNav identifies the current page for nav highlight (keys: "bootstrap", "dashboard", "logout").
// TopbarStatus and TopbarLabel are set by the dashboard handler to show real system status;
// zero values (empty string) cause base.html to render nothing (guarded by {{ if .TopbarStatus }}).
type PageData struct {
	BootstrapMode bool
	// LoginMode suppresses the authenticated sidebar nav and topbar status on the login page.
	// Set to true only by LoginPageHandler. All other handlers leave it false (zero value).
	LoginMode bool
	ActiveNav string
	// TopbarStatus is a DaisyUI semantic color name ("success", "warning", "error").
	// Empty string → base.html renders the default "Connecting..." placeholder.
	TopbarStatus string
	// TopbarLabel is the human-readable status label shown in the topbar (e.g. "OK", "Degraded", "Down").
	TopbarLabel string
	// CSRFToken holds the double-submit CSRF token injected by CSRFMiddleware.
	// Templates embed it as a hidden <input name="_csrf"> in every state-changing form.
	CSRFToken string
	// CompliancePendingCount is the number of pending compliance access requests (Story 5.4).
	// Only DashboardHandler populates this; all other handlers leave it at 0 (badge hidden).
	// Placed on PageData (not DashboardPageData) so base.html can access it on all pages.
	CompliancePendingCount int
}

// DashboardPageData holds data for the Dashboard page.
// Embeds PageData for ActiveNav, topbar status, and CompliancePendingCount (Story 5.4).
type DashboardPageData struct {
	PageData // embed for BootstrapMode + ActiveNav + TopbarStatus + TopbarLabel + CompliancePendingCount

	// Status values: "green", "amber", or "red"
	GatewayStatus string
	CoreStatus    string
	DBStatus      string
	// WorstStatus is the worst of the three (used for TopbarStatusIndicator)
	WorstStatus string

	// Human-readable labels shown inside each card ("OK", "Degraded", "Unreachable")
	GatewayStatusLabel string
	CoreStatusLabel    string
	DBStatusLabel      string

	// Server info
	InstanceName string
	Uptime       string // formatted string (e.g. "3d 4h 12m")
	GoVersion    string // value of runtime.Version()

	// Note: CompliancePendingCount is reachable via embedded PageData; no
	// separate field on DashboardPageData (avoids field shadowing — Story 5.4
	// review). Dashboard handler sets it as PageData.CompliancePendingCount.
}

// LoginPageData holds data for the Admin login page.
type LoginPageData struct {
	PageData        // embed for BootstrapMode + ActiveNav
	Error    string // optional error message (e.g. "auth_failed", "config_missing")
}

// BootstrapPageData holds data for the Bootstrap Wizard page.
// Step is 1–2 (OIDC connect replaces steps 3+4). All field values carry accumulated state.
type BootstrapPageData struct {
	PageData     // embed for BootstrapMode + ActiveNav
	Step         int
	InstanceName string
	OIDCIssuer   string
	OIDCClientID string
	// OIDCClientSecret is intentionally NOT stored here (security)
	// MaskedSecret is a display-only masked version of the OIDC client secret (e.g. "abc...xyz")
	MaskedSecret string
	// Errors carries per-field or global error messages for re-render
	Errors map[string]string
	// Warnings carries per-field non-blocking warnings (e.g. HTTP issuer in dev)
	Warnings map[string]string
}

// DiscoveredClaim is a single claim key+values pair extracted from an OIDC token
// for display on the claim selection page.
type DiscoveredClaim struct {
	Key    string
	Values []string
}

// ClaimSelectionPageData holds data for the Bootstrap claim-selection page (post-OIDC callback).
type ClaimSelectionPageData struct {
	PageData // embed for BootstrapMode + ActiveNav
	Claims   []DiscoveredClaim
	Email    string
	Error    string
}

// ErrorPageData holds data for server-error pages that include a request ID.
// The RequestID is generated per-request (crypto/rand) and logged alongside
// the full error so operators can correlate user-reported IDs with log entries.
type ErrorPageData struct {
	PageData
	RequestID string
}

// StubUser is a fake user record used for the Users master-detail page until
// Epic 6 implements the real User Management API. Replaced by API data in Epic 7 API integration.
type StubUser struct {
	ID          string
	DisplayName string
	Email       string // masked, e.g. "a***@example.com"
	Role        string // "instance_admin" | "compliance_officer" | "user"
	Status      string // "active" | "deactivated"
}

// UserRowData is used in the Users list template (Story 7.5).
// Embeds StubUser and adds a pre-computed Badge for the status_badge component,
// normalising StubUser.Status "deactivated" → StatusBadgeData{Status: "inactive"}.
// This avoids template FuncMap helpers — the handler populates Badge directly.
type UserRowData struct {
	StubUser
	Badge StatusBadgeData
}

// UsersPageData holds data for the Users master-detail page (Story 7.5).
// Embeds PageData for nav, topbar, and CSRF token.
// Users is the filtered+paginated slice (replaces StubUsers from Story 7.2).
// SearchInput, FilterBar, TotalCount, CurrentPage, HasMore, NextPage support search/filter/pagination.
// EmptyState is populated by the handler and rendered when Users is empty.
// ActiveItemID, ActiveUser, CloseURL remain for the MasterDetail detail panel (Stories 7.2/7.6).
// Flash is populated when ?flash= query param is present (Story 7.6).
// ActiveUserInlineEdit and ActiveUserStatusBadge are pre-computed by DetailHandler (Story 7.6).
type UsersPageData struct {
	PageData
	Users        []UserRowData
	SearchInput  SearchInputData
	FilterBar    []FilterOption
	TotalCount   int
	CurrentPage  int
	HasMore      bool
	NextPage     int
	EmptyState   EmptyStateData // populated when Users is empty
	ActiveItemID string
	ActiveUser   *StubUser // nil = list view or not found
	CloseURL     string    // e.g. "/admin/users"
	// Flash is populated when ?flash= query param is present (Story 7.6).
	// Zero-valued in list mode — no template rendering side-effects.
	Flash AlertBannerData
	// ActiveUserInlineEdit is pre-computed by DetailHandler for the inline_edit component (Story 7.6).
	// Only meaningful when ActiveUser != nil.
	ActiveUserInlineEdit InlineEditData
	// ActiveUserStatusBadge is pre-computed by DetailHandler for the status_badge component (Story 7.6).
	// Only meaningful when ActiveUser != nil.
	ActiveUserStatusBadge StatusBadgeData
	// ActiveUserInitial holds the first rune of DisplayName as a string (rune-safe).
	// Pre-computed by DetailHandler to avoid UTF-8 byte-slice edge cases in templates.
	// TODO: use rune-aware initials helper in production when multi-char initials are needed.
	ActiveUserInitial string
	// ActiveUserConfirmDialog is populated by DetailHandler for the confirm_dialog component (Story 7.7).
	// Only meaningful when ActiveUser != nil and ActiveUser.Status == "active".
	ActiveUserConfirmDialog ConfirmDialogData
	// ActiveUserRoleOptions lists the valid role values for the role <select> (Story 7.7).
	ActiveUserRoleOptions []string
	// ActiveUserRoleValue holds the current role for the pre-selected <option> (Story 7.7).
	ActiveUserRoleValue string
}

// StubRoom is a fake room record used for the Rooms master-detail page until
// Epic 6 implements the real Room Management API.
// MaxMembers (0 = no limit) and Visibility are populated by the detail handler from
// AdminRoomDetailProto (Story 9.3 — gRPC integration).
type StubRoom struct {
	ID          string
	Name        string
	Visibility  string // "public" | "private"
	MemberCount int
	MaxMembers  int    // 0 = no limit; populated in detail view only
	Status      string // "active" | "archived"
}

// RoomRowData is used in the Rooms list template (Story 7.8).
// Embeds StubRoom and adds a pre-computed Badge for the status_badge component,
// normalising StubRoom.Status "archived" → StatusBadgeData{Status: "inactive"}.
// This avoids template FuncMap helpers — the handler populates Badge directly.
type RoomRowData struct {
	StubRoom
	Badge StatusBadgeData
}

// RoomsPageData holds data for the Rooms master-detail page (Story 7.8, extended Story 7.9).
// Rooms is the filtered+paginated slice (replaces StubRooms from Story 7.2).
// SearchInput, FilterBar, TotalCount, CurrentPage, HasMore, NextPage support search/filter/pagination.
// EmptyState is populated by the handler and rendered when Rooms is empty.
// ActiveItemID, ActiveRoom, CloseURL remain for the MasterDetail detail panel (Story 7.2/7.9).
type RoomsPageData struct {
	PageData
	Rooms        []RoomRowData
	SearchInput  SearchInputData
	FilterBar    []FilterOption
	TotalCount   int
	CurrentPage  int
	HasMore      bool
	NextPage     int
	EmptyState   EmptyStateData
	ActiveItemID string
	ActiveRoom   *StubRoom // nil = list view or not found
	CloseURL     string    // e.g. "/admin/rooms"
	// Flash is populated when ?flash= query param is present (Story 7.9).
	// Zero-valued in list mode — no template rendering side-effects.
	Flash AlertBannerData
	// ActiveRoomInlineEdit is pre-computed by DetailHandler for the inline_edit component (Story 7.9).
	// Only meaningful when ActiveRoom != nil.
	ActiveRoomInlineEdit InlineEditData
	// ActiveRoomStatusBadge is pre-computed by DetailHandler for the status_badge component (Story 7.9).
	// Only meaningful when ActiveRoom != nil.
	ActiveRoomStatusBadge StatusBadgeData
	// ActiveRoomConfirmDialog is pre-computed by DetailHandler for the confirm_dialog component (Story 7.9).
	// Only meaningful when ActiveRoom != nil and ActiveRoom.Status == "active".
	ActiveRoomConfirmDialog ConfirmDialogData
	// ActiveRoomInitial holds the first rune of Name as a string (rune-safe).
	// Pre-computed by DetailHandler to avoid UTF-8 byte-slice edge cases in templates.
	// TODO: use rune-aware initials helper in production when multi-char initials are needed.
	ActiveRoomInitial string
	// ActiveRoomMembers is the list of current members for the selected room (Story 9.18).
	// Nil/empty in list mode or when the gRPC call fails (detail panel still renders).
	ActiveRoomMembers []RoomMemberData
}

// RoomMemberData holds one member row for the Room Detail member list (Story 9.18).
type RoomMemberData struct {
	UserID      string
	DisplayName string // empty string if unavailable
	JoinedAt    int64  // Unix milliseconds
}

// ConfigPageData holds data for the Server Configuration page (Story 7.10).
// Embeds PageData for ActiveNav, topbar status, and CSRF token.
// Config is populated from stubConfig by ConfigHandler.Handler.
// Flash is populated when ?flash= query param is present (PRG pattern).
type ConfigPageData struct {
	PageData
	Config StubConfig
	Flash  AlertBannerData
}

// RoleMappingPageData holds data for the Role Mapping configuration page (Story 7.15).
// Embeds PageData for ActiveNav, topbar status, and CSRF token.
// Config is populated from stubRoleMappingConfig by RoleMappingHandler.Handler.
// Errors carries per-field validation error messages (only on POST re-render).
// Flash is populated when ?flash= query param is present (PRG pattern).
type RoleMappingPageData struct {
	PageData
	Config StubRoleMappingConfig
	Errors map[string]string
	Flash  AlertBannerData
}

// CompliancePageData holds data for the Compliance Access Requests page (Story 7.11).
// Embeds PageData for ActiveNav, topbar status, and CSRF token.
// Requests is the filtered slice of compliance requests.
// StatusFilter holds the current ?status= filter value (default "pending").
// Flash is populated when ?flash= query param is present (PRG pattern).
// Stepper is pre-computed for the wizard_stepper component (always CurrentStep=1).
type CompliancePageData struct {
	PageData
	Requests     []StubComplianceRequest
	StatusFilter string
	Flash        AlertBannerData
	Stepper      WizardStepperData
	EmptyState   EmptyStateData // populated when Requests is empty
}

// AuditLogPageData holds data for the Audit Log page (Story 7.12).
// Embeds PageData for ActiveNav, topbar status, and CSRF token.
// Entries is the filtered slice of audit log entries (from stubAuditLog for MVP).
// From and To hold the current date-range filter values (YYYY-MM-DD strings).
// Flash is reserved for future flash messages (always zero-valued in read-only MVP).
// EmptyState is rendered when Entries is empty.
type AuditLogPageData struct {
	PageData
	Entries    []StubAuditEntry
	From       string // query param "from", e.g. "2026-04-29"
	To         string // query param "to", e.g. "2026-04-29"
	Flash      AlertBannerData
	EmptyState EmptyStateData
}

// WizardStepperData is passed to the wizard_stepper component partial (C6, Story 7.3).
// Steps is a slice of step labels (e.g. []string{"Request", "Approved", "Download"}).
// CurrentStep is 0-indexed; steps before it are "completed", the current one is "active",
// and the rest are "upcoming".
type WizardStepperData struct {
	Steps       []string
	CurrentStep int
}

// ConfirmDialogData is passed to the confirm_dialog component partial (C7, Story 7.3).
// HiddenFields map is rendered as <input type="hidden" name="k" value="v"> inside the form.
// CSRFToken must be populated from the page's PageData.CSRFToken by the caller; the dialog
// form is a POST form and requires the CSRF double-submit token. Stories 7.7 and 7.9 populate
// this when embedding the dialog.
//
// NOTE: The <dialog> element uses id="confirm_dialog". If multiple dialogs are needed on one
// page, each needs a unique ID — a future ID field will be added in Stories 7.7/7.9.
type ConfirmDialogData struct {
	Title        string
	Message      string
	ConfirmLabel string
	ConfirmClass string            // DaisyUI btn modifier, e.g. "btn-error"
	FormAction   string            // POST URL for the confirm form
	HiddenFields map[string]string // extra hidden inputs
	CSRFToken    string            // CSRF double-submit token (from PageData.CSRFToken)
}

// SearchInputData is passed to the search_input component partial (C8, Story 7.3).
// The component renders a plain <input> without a surrounding <form> — the page template
// owns the form. The inline debounce script calls form.requestSubmit() after 300ms.
type SearchInputData struct {
	Placeholder string
	Value       string
	ParamName   string // query param key, e.g. "q"
}

// FilterOption represents one <select> dropdown in the FilterBar component (C9/C10, Story 7.3).
// Each option in Options is rendered as an <option> element; the one matching CurrentValue
// receives the selected attribute. The <select> auto-submits its parent form on change.
//
// NOTE: There is intentionally no FilterBarData wrapper struct. Page data structs for Stories
// 7.5 and 7.8 will embed Filters []FilterOption directly, and pass the slice to the template.
type FilterOption struct {
	Label        string
	ParamName    string
	Options      []string
	CurrentValue string
}

// InlineEditData is passed to the inline_edit component partial (C11, Story 7.4).
// The component renders a text display with an edit icon button that reveals an
// inline form on click. Save submits the form via POST; Cancel restores display mode.
// CSRFToken must be populated from the page's PageData.CSRFToken by the caller.
// ID must be unique on the page (used for CSS toggle and ARIA correlation).
type InlineEditData struct {
	ID         string // unique element identifier on the page (e.g. "display-name")
	FieldName  string // <input name="..."> value (e.g. "display_name")
	Value      string // current field value (pre-fills the input)
	Label      string // human-readable label for ARIA attributes (e.g. "Display Name")
	FormAction string // POST URL for the save form (e.g. "/admin/users/usr-001/display-name")
	CSRFToken  string // CSRF double-submit token (from PageData.CSRFToken)
}

// AlertBannerData is passed to the alert_banner component partial (C12, Story 7.4).
// Severity must be one of: "info", "success", "warning", "error".
// When Dismissible is true, an X button is rendered that removes the alert client-side.
// aria-live is set to "assertive" for warning/error and "polite" for info/success.
type AlertBannerData struct {
	Severity    string // "info" | "success" | "warning" | "error"
	Message     string
	Dismissible bool
}

// StatusBadgeData is passed to the status_badge component partial (C13, Story 7.4).
// Status drives the DaisyUI badge colour class (active→success, inactive→error, pending→warning).
// Label overrides the display text; if empty, Status is used as the display text.
type StatusBadgeData struct {
	Status string // "active" | "inactive" | "pending" (unknown → badge-ghost)
	Label  string // optional display text override; if empty, uses Status
}

// EmptyStateData is passed to the empty_state component partial (C14, Story 7.4).
// Heading is rendered as <h3>; Description is rendered as <p>.
// Both are required — empty values render an empty heading/description.
type EmptyStateData struct {
	Heading     string
	Description string
}
