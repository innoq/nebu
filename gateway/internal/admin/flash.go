package admin

// allowedFlashMessages is the exhaustive set of flash values that GET handlers
// may display. Any value not in this set is silently dropped (Story 7.18).
var allowedFlashMessages = map[string]struct{}{
	"Role updated":          {},
	"Display name updated":  {},
	"User deactivated":      {},
	"User reactivated":      {},
	"Room name updated":     {},
	"Room archived":         {},
	"Room unarchived":       {},
	"Config updated":        {},
	"Error updating config": {},
	"Role mapping updated":  {},
	"Approved":              {},
	"Rejected":              {},
}

// sanitizeFlash returns msg if it is a known-safe flash value and does not
// exceed 80 characters. Otherwise it returns the empty string.
func sanitizeFlash(msg string) string {
	if len(msg) > 80 {
		return ""
	}
	if _, ok := allowedFlashMessages[msg]; !ok {
		return ""
	}
	return msg
}
