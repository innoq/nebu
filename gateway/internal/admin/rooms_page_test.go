package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRoomsPageRenders verifies GET /admin/rooms returns HTTP 200 with WCAG landmarks.
// AC: 1 (Story 7.8)
func TestRoomsPageRenders(t *testing.T) {
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewRoomsHandler(tmpl)

	req := httptest.NewRequest(http.MethodGet, "/admin/rooms", nil)
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

// TestRoomsPageSearch verifies ?q=general filters to matching rooms only.
// AC: 2 (Story 7.8)
func TestRoomsPageSearch(t *testing.T) {
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewRoomsHandler(tmpl)

	req := httptest.NewRequest(http.MethodGet, "/admin/rooms?q=general", nil)
	w := httptest.NewRecorder()
	h.ListHandler(w, req)

	if w.Code != 200 {
		t.Fatalf("expected HTTP 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "General") {
		t.Error("expected 'General' in body for q=general")
	}
	if strings.Contains(body, "Engineering") {
		t.Error("expected 'Engineering' to be absent for q=general")
	}
}

// TestRoomsPageVisibilityFilter verifies ?visibility=private filters to private rooms only.
// AC: 3 (Story 7.8)
func TestRoomsPageVisibilityFilter(t *testing.T) {
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewRoomsHandler(tmpl)

	req := httptest.NewRequest(http.MethodGet, "/admin/rooms?visibility=private", nil)
	w := httptest.NewRecorder()
	h.ListHandler(w, req)

	if w.Code != 200 {
		t.Fatalf("expected HTTP 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Engineering") {
		t.Error("expected 'Engineering' in body for visibility=private")
	}
	// General is public — its detail link must not appear when filtering by private.
	// Using the room's URL as the precise anchor (avoids false positives from nav text).
	if strings.Contains(body, "/admin/rooms/room-001") {
		t.Error("expected General (room-001, public) to be absent for visibility=private")
	}
}

// TestRoomsPageEmptyState verifies ?q=zzznomatch shows the empty state.
// AC: 4 (Story 7.8)
func TestRoomsPageEmptyState(t *testing.T) {
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewRoomsHandler(tmpl)

	req := httptest.NewRequest(http.MethodGet, "/admin/rooms?q=zzznomatch", nil)
	w := httptest.NewRecorder()
	h.ListHandler(w, req)

	if w.Code != 200 {
		t.Fatalf("expected HTTP 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "No rooms found") {
		t.Error("expected 'No rooms found' empty state in body")
	}
}

// TestRoomsPageStatusBadge verifies badge-success and badge-error appear in the rendered body.
// "Old Project X" (room-004, Status: "archived") → badge-error; active rooms → badge-success.
// AC: 5 (Story 7.8)
func TestRoomsPageStatusBadge(t *testing.T) {
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewRoomsHandler(tmpl)

	req := httptest.NewRequest(http.MethodGet, "/admin/rooms", nil)
	w := httptest.NewRecorder()
	h.ListHandler(w, req)

	if w.Code != 200 {
		t.Fatalf("expected HTTP 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "badge-success") {
		t.Error("expected badge-success for active rooms")
	}
	if !strings.Contains(body, "badge-error") {
		t.Error("expected badge-error for archived room (Old Project X)")
	}
}

// TestRoomsPagePagination verifies the pagination boundary: with 6 stubs and pageSize=5,
// page 0 renders 5 rooms and HasMore=true, so the pagination nav IS rendered.
// (Story 9.15 added room-006 to stubs to exercise the AC3 fallback name template.)
// AC: 6 (Story 7.8)
func TestRoomsPagePagination(t *testing.T) {
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewRoomsHandler(tmpl)

	req := httptest.NewRequest(http.MethodGet, "/admin/rooms", nil)
	w := httptest.NewRecorder()
	h.ListHandler(w, req)

	if w.Code != 200 {
		t.Fatalf("expected HTTP 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `aria-label="pagination"`) {
		t.Error("expected pagination nav when HasMore=true (6 stubs, pageSize=5)")
	}
}
