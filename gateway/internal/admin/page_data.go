package admin

// PageData holds template data passed to all Admin UI page renders.
// BootstrapMode controls sidebar Bootstrap nav item visibility.
// ActiveNav identifies the current page for nav highlight (keys: "bootstrap", "dashboard", "logout").
// TopbarStatus and TopbarLabel are set by the dashboard handler to show real system status;
// zero values (empty string) cause base.html to render the default "Connecting..." placeholder.
type PageData struct {
	BootstrapMode bool
	ActiveNav     string
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

// UsersPageData holds data for the Users master-detail page (Story 7.2).
// Embeds PageData for nav, topbar, and CSRF token.
// ActiveItemID is the URL path parameter (e.g. "usr-001"); empty string means list view.
// ActiveUser is nil when no item is selected or the ID is not found.
// CloseURL is the back-link URL used by the detail panel's close button.
type UsersPageData struct {
	PageData
	StubUsers    []StubUser
	ActiveItemID string
	ActiveUser   *StubUser // nil = list view or not found
	CloseURL     string    // e.g. "/admin/users"
}

// StubRoom is a fake room record used for the Rooms master-detail page until
// Epic 6 implements the real Room Management API.
type StubRoom struct {
	ID          string
	Name        string
	Visibility  string // "public" | "private"
	MemberCount int
	Status      string // "active" | "archived"
}

// RoomsPageData holds data for the Rooms master-detail page (Story 7.2).
// Same pattern as UsersPageData but for rooms.
type RoomsPageData struct {
	PageData
	StubRooms    []StubRoom
	ActiveItemID string
	ActiveRoom   *StubRoom // nil = list view or not found
	CloseURL     string    // e.g. "/admin/rooms"
}
