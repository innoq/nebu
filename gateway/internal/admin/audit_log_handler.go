package admin

import (
	"net/http"
)

// AuditLogHandler serves the /admin/audit-log page (Story 7.12).
// Read-only: no POST handlers. Uses stub data until a real audit log API is available.
type AuditLogHandler struct {
	tmpl *TemplateHandler
}

// NewAuditLogHandler constructs an AuditLogHandler with the given template handler.
func NewAuditLogHandler(tmpl *TemplateHandler) *AuditLogHandler {
	return &AuditLogHandler{tmpl: tmpl}
}

// ListHandler handles GET /admin/audit-log.
// Reads optional ?from= and ?to= query params (date strings "YYYY-MM-DD").
// When both params are present, filters stubAuditLog to entries whose Timestamp
// falls within [from, to+"T23:59:59Z"] using lexicographic string comparison —
// valid because timestamps are ISO-8601 with the date as a sortable prefix.
// When either param is absent, no filter is applied and all entries are returned.
// Renders audit_log.html with AuditLogPageData.
func (h *AuditLogHandler) ListHandler(w http.ResponseWriter, r *http.Request) {
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")

	entries := filterAuditLog(stubAuditLog, from, to)

	data := AuditLogPageData{
		PageData:   PageData{ActiveNav: "audit-log", CSRFToken: CSRFTokenFromContext(r.Context())},
		Entries:    entries,
		From:       from,
		To:         to,
		EmptyState: EmptyStateData{Heading: "No audit entries", Description: "No entries match the selected date range. Try clearing the filter or selecting a different range."},
	}
	h.tmpl.render(w, "audit_log", data)
}

// filterAuditLog returns a filtered slice of StubAuditEntry.
// When both from and to are non-empty, only entries with Timestamp in [from, to+"T23:59:59Z"]
// are included. When either param is empty, all entries are returned unfiltered.
// Uses lexicographic string comparison — correct for ISO-8601 timestamps.
func filterAuditLog(entries []StubAuditEntry, from, to string) []StubAuditEntry {
	if from == "" || to == "" {
		result := make([]StubAuditEntry, len(entries))
		copy(result, entries)
		return result
	}
	toUpperBound := to + "T23:59:59Z"
	result := make([]StubAuditEntry, 0, len(entries))
	for _, e := range entries {
		if e.Timestamp >= from && e.Timestamp <= toUpperBound {
			result = append(result, e)
		}
	}
	return result
}
