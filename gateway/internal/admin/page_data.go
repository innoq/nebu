package admin

// PageData holds template data passed to all Admin UI page renders.
// BootstrapMode controls sidebar Bootstrap nav item visibility.
// ActiveNav identifies the current page for nav highlight (keys: "bootstrap", "dashboard", "logout").
// Extended by Story 3.13 with status fields.
type PageData struct {
	BootstrapMode bool
	ActiveNav     string
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
