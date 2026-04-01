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
}

// DashboardPageData holds data for the Dashboard page.
// Embeds PageData for ActiveNav and topbar status.
type DashboardPageData struct {
	PageData // embed for BootstrapMode + ActiveNav + TopbarStatus + TopbarLabel

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
}

// LoginPageData holds data for the Admin login page.
type LoginPageData struct {
	PageData        // embed for BootstrapMode + ActiveNav
	Error string    // optional error message (e.g. "auth_failed", "config_missing")
}

// BootstrapPageData holds data for the Bootstrap Wizard page.
// Step is 1–4. All field values carry accumulated state across steps.
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
}
