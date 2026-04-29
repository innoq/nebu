package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestUsersPageRenders verifies that GET /admin/users returns HTTP 200 with WCAG landmarks.
// AC: 1, 3 (Story 7.5)
func TestUsersPageRenders(t *testing.T) {
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatal(err)
	}
	h := NewUsersHandler(tmpl)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	h.ListHandler(w, r)

	if w.Code != 200 {
		t.Fatalf("want 200 got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "<h1") {
		t.Error("missing <h1> landmark")
	}
	if !strings.Contains(body, "<main") {
		t.Error("missing <main> landmark")
	}
}

// TestUsersPageSearch verifies that ?q=alice filters the list to matching users.
// AC: 1, 2 (Story 7.5)
func TestUsersPageSearch(t *testing.T) {
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatal(err)
	}
	h := NewUsersHandler(tmpl)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/users?q=alice", nil)
	h.ListHandler(w, r)

	if w.Code != 200 {
		t.Fatalf("want 200 got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Alice Müller") {
		t.Error("expected 'Alice Müller' in filtered results")
	}
	if strings.Contains(body, "Bob Wagner") {
		t.Error("expected 'Bob Wagner' to be filtered out")
	}
}

// TestUsersPageRoleFilter verifies that ?role=admin filters to instance_admin users only.
// AC: 1, 2 (Story 7.5)
func TestUsersPageRoleFilter(t *testing.T) {
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatal(err)
	}
	h := NewUsersHandler(tmpl)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/users?role=admin", nil)
	h.ListHandler(w, r)

	if w.Code != 200 {
		t.Fatalf("want 200 got %d", w.Code)
	}
	body := w.Body.String()
	// Alice Müller has Role="instance_admin" → maps to admin filter
	if !strings.Contains(body, "Alice Müller") {
		t.Error("expected 'Alice Müller' (instance_admin) to appear for role=admin filter")
	}
	// Carla Reiter has Role="user" → must NOT appear
	if strings.Contains(body, "Carla Reiter") {
		t.Error("expected 'Carla Reiter' (user role) to be filtered out for role=admin")
	}
}

// TestUsersPageEmptyState verifies that a no-match query shows the empty state component.
// AC: 1, 3 (Story 7.5)
func TestUsersPageEmptyState(t *testing.T) {
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatal(err)
	}
	h := NewUsersHandler(tmpl)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/users?q=zzznomatch", nil)
	h.ListHandler(w, r)

	if w.Code != 200 {
		t.Fatalf("want 200 got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "No users found") {
		t.Error("expected 'No users found' empty state heading")
	}
}

// TestUsersPagePagination verifies that HasMore renders the pagination nav with a bookmarkable
// load-more link carrying all three URL params, and that page=1 shows the next slice (no nav).
// AC: 1 (pagination), 3 (nav landmark), 4 (URL bookmarkability) — Story 7.5
func TestUsersPagePagination(t *testing.T) {
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatal(err)
	}
	h := NewUsersHandler(tmpl)

	// Page 0: 8 total stubs → first 5 shown, HasMore=true → nav rendered
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/users?q=&role=all&page=0", nil)
	h.ListHandler(w, r)
	if w.Code != 200 {
		t.Fatalf("want 200 got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `aria-label="pagination"`) {
		t.Error("expected <nav aria-label=\"pagination\"> when HasMore=true")
	}
	// Load-more link must carry all three params (URL bookmarkability — AC4)
	if !strings.Contains(body, "page=1") {
		t.Error("load-more link must contain page=1")
	}
	if !strings.Contains(body, "role=") {
		t.Error("load-more link must carry role= param")
	}

	// Page 1: remaining 3 stubs (Franz, Gabi, Hans), HasMore=false → no nav
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest(http.MethodGet, "/admin/users?page=1", nil)
	h.ListHandler(w2, r2)
	if w2.Code != 200 {
		t.Fatalf("want 200 got %d", w2.Code)
	}
	body2 := w2.Body.String()
	if !strings.Contains(body2, "Franz Bauer") {
		t.Error("page=1 must show Franz Bauer")
	}
	if strings.Contains(body2, `aria-label="pagination"`) {
		t.Error("pagination nav must NOT appear when HasMore=false (last page)")
	}
}

// TestUsersPageStatusBadge verifies that badge classes match user status.
// badge-success for active users, badge-error for deactivated users (Dieter Krause, usr-004).
// AC: 1, 3 (Story 7.5)
func TestUsersPageStatusBadge(t *testing.T) {
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatal(err)
	}
	h := NewUsersHandler(tmpl)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	h.ListHandler(w, r)

	if w.Code != 200 {
		t.Fatalf("want 200 got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "badge-success") {
		t.Error("expected badge-success for active users")
	}
	if !strings.Contains(body, "badge-error") {
		t.Error("expected badge-error for deactivated users (Dieter Krause)")
	}
}
