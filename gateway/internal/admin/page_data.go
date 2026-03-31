package admin

// PageData holds template data passed to all Admin UI page renders.
// BootstrapMode controls sidebar Bootstrap nav item visibility.
// ActiveNav identifies the current page for nav highlight (keys: "bootstrap", "dashboard", "logout").
// Extended by Story 3.13 with status fields.
type PageData struct {
	BootstrapMode bool
	ActiveNav     string
}
