package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAuditLogPageRenders verifies GET /admin/audit-log returns HTTP 200 with WCAG landmarks.
// AC: 9 (Story 7.12)
func TestAuditLogPageRenders(t *testing.T) {
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewAuditLogHandler(tmpl)

	req := httptest.NewRequest(http.MethodGet, "/admin/audit-log", nil)
	w := httptest.NewRecorder()
	h.ListHandler(w, req)

	if w.Code != 200 {
		t.Fatalf("expected HTTP 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "<h1") {
		t.Error("expected <h1 in body")
	}
	if !strings.Contains(body, "<main") {
		t.Error("expected <main in body")
	}
}

// TestAuditLogDateFilter verifies date filter returns only entries on the selected date.
// AC: 4, 9 (Story 7.12)
func TestAuditLogDateFilter(t *testing.T) {
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewAuditLogHandler(tmpl)

	// Filter to 2026-04-29 only — entries al-003 (kai) and al-004 (admin) should appear;
	// al-001 and al-002 (2026-04-28, actor kai) should be absent.
	req := httptest.NewRequest(http.MethodGet, "/admin/audit-log?from=2026-04-29&to=2026-04-29", nil)
	w := httptest.NewRecorder()
	h.ListHandler(w, req)

	if w.Code != 200 {
		t.Fatalf("expected HTTP 200, got %d", w.Code)
	}
	body := w.Body.String()

	// al-003: config.update on 2026-04-29 should be present
	if !strings.Contains(body, "config.update") {
		t.Error("expected 'config.update' (2026-04-29 entry) in filtered body")
	}
	// al-004: user.role_change on 2026-04-29 should be present
	if !strings.Contains(body, "user.role_change") {
		t.Error("expected 'user.role_change' (2026-04-29 entry) in filtered body")
	}
	// al-001 and al-002 are on 2026-04-28 — should be absent
	if strings.Contains(body, "user.deactivate") {
		t.Error("expected 'user.deactivate' (2026-04-28 entry) to be absent in filtered body")
	}
	// room.archive appears on both 2026-04-28 (al-002) and 2026-04-30 (al-006), so checking
	// it by action string alone would be unreliable. Check the al-002 TargetName instead.
	if strings.Contains(body, "Old Project X") {
		t.Error("expected 'Old Project X' (al-002, 2026-04-28) to be absent in filtered body")
	}
}

// TestAuditLogNoFilter verifies that GET without params returns all 6 stub entries.
// AC: 4, 9 (Story 7.12)
func TestAuditLogNoFilter(t *testing.T) {
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewAuditLogHandler(tmpl)

	req := httptest.NewRequest(http.MethodGet, "/admin/audit-log", nil)
	w := httptest.NewRecorder()
	h.ListHandler(w, req)

	if w.Code != 200 {
		t.Fatalf("expected HTTP 200, got %d", w.Code)
	}
	body := w.Body.String()

	// All 6 entries should appear — check a unique field from each
	checks := []struct {
		val  string
		desc string
	}{
		{"Dieter Krause", "al-001 TargetName"},
		{"Old Project X", "al-002 TargetName"},
		{"config.update", "al-003 Action"},
		{"user.role_change", "al-004 Action"},
		{"compliance.approve", "al-005 Action"},
		{"Engineering", "al-006 TargetName"},
	}
	for _, c := range checks {
		if !strings.Contains(body, c.val) {
			t.Errorf("expected %q (%s) in unfiltered body", c.val, c.desc)
		}
	}
}

// TestAuditLogEmptyState verifies that a filter matching no entries renders an empty state.
// AC: 7, 9 (Story 7.12)
func TestAuditLogEmptyState(t *testing.T) {
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewAuditLogHandler(tmpl)

	// 2000-01-01 is well before all stub entries — filter should return nothing.
	req := httptest.NewRequest(http.MethodGet, "/admin/audit-log?from=2000-01-01&to=2000-01-01", nil)
	w := httptest.NewRecorder()
	h.ListHandler(w, req)

	if w.Code != 200 {
		t.Fatalf("expected HTTP 200, got %d", w.Code)
	}
	body := w.Body.String()

	// Empty state text should be present
	if !strings.Contains(body, "No audit") && !strings.Contains(body, "no entries") && !strings.Contains(body, "No entries") {
		t.Error("expected empty state message ('No audit' / 'no entries' / 'No entries') in body")
	}
	// No known actor should appear
	if strings.Contains(body, "kai@example.com") {
		t.Error("expected 'kai@example.com' to be absent in empty-state body")
	}
	if strings.Contains(body, "admin@example.com") {
		t.Error("expected 'admin@example.com' to be absent in empty-state body")
	}
}
