package admin

// PageData holds template data passed to all Admin UI page renders.
// BootstrapMode controls sidebar Bootstrap nav item visibility.
// Extended by Story 3.5 with ActiveNav, Story 3.13 with status fields.
type PageData struct {
	BootstrapMode bool
}
