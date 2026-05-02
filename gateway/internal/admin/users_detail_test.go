package admin

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestUserDetailPanelRenders verifies that GET /admin/users/usr-001 returns HTTP 200
// with the user's display name and the inline_edit component rendered.
// AC: 6 (TestUserDetailPanelRenders — Story 7.6)
func TestUserDetailPanelRenders(t *testing.T) {
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewUsersHandler(tmpl)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/users/{userId}", h.DetailHandler)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/users/usr-001", nil)
	mux.ServeHTTP(w, r)

	if w.Code != 200 {
		t.Fatalf("want 200 got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Alice Müller") {
		t.Error("expected body to contain 'Alice Müller'")
	}
	if !strings.Contains(body, "inline-edit-field") {
		t.Error("expected body to contain 'inline-edit-field' (from inline_edit component)")
	}
}

// TestUserDetailPanelNotFound verifies that GET /admin/users/xxx-999 returns HTTP 404.
// AC: 6 (TestUserDetailPanelNotFound — Story 7.6); also AC1 (404 for unknown user).
func TestUserDetailPanelNotFound(t *testing.T) {
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewUsersHandler(tmpl)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/users/{userId}", h.DetailHandler)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/users/xxx-999", nil)
	mux.ServeHTTP(w, r)

	if w.Code != 404 {
		t.Fatalf("want 404 for unknown user got %d", w.Code)
	}
}

// TestUserDetailFlashMessage verifies that ?flash=Display+name+updated renders
// an alert banner containing that text.
// AC: 6 (TestUserDetailFlashMessage — Story 7.6); also AC1 (flash query param).
func TestUserDetailFlashMessage(t *testing.T) {
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewUsersHandler(tmpl)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/users/{userId}", h.DetailHandler)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/users/usr-001?flash=Display+name+updated", nil)
	mux.ServeHTTP(w, r)

	if w.Code != 200 {
		t.Fatalf("want 200 got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Display name updated") {
		t.Errorf("expected body to contain 'Display name updated', got: %s", body[:min(500, len(body))])
	}
}

// TestUpdateDisplayNameTooLong verifies that POST with a display_name > 100 chars returns HTTP 400.
// AC: 3 (Story 7.6)
func TestUpdateDisplayNameTooLong(t *testing.T) {
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewUsersHandler(tmpl)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/users/{userId}/display-name", h.UpdateDisplayNameHandler)

	form := url.Values{}
	form.Set("display_name", strings.Repeat("x", 101))
	body := strings.NewReader(form.Encode())

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/admin/users/usr-001/display-name", body)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for too-long display_name got %d", w.Code)
	}
}

// TestUpdateDisplayNameTooLongMultibyte verifies that POST with 101 multibyte runes (> 100 chars)
// returns HTTP 400. Regression test for rune-count vs byte-count correctness (CR fix).
func TestUpdateDisplayNameTooLongMultibyte(t *testing.T) {
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewUsersHandler(tmpl)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/users/{userId}/display-name", h.UpdateDisplayNameHandler)

	// 101 × "ä" = 101 runes but 202 bytes — must be rejected as > 100 chars.
	form := url.Values{}
	form.Set("display_name", strings.Repeat("ä", 101))
	body := strings.NewReader(form.Encode())

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/admin/users/usr-001/display-name", body)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for 101-rune multibyte display_name, got %d", w.Code)
	}
}

// TestUpdateDisplayName verifies that POST /admin/users/usr-001/display-name
// with a valid display_name redirects to the detail URL with a flash param.
// AC: 6 (TestUpdateDisplayName — Story 7.6); also AC4 (PRG redirect).
func TestUpdateDisplayName(t *testing.T) {
	// Save original value by ID (not index) to be resilient to stub ordering changes.
	var original string
	for _, u := range stubUsers {
		if u.ID == "usr-001" {
			original = u.DisplayName
			break
		}
	}
	t.Cleanup(func() {
		for i := range stubUsers {
			if stubUsers[i].ID == "usr-001" {
				stubUsers[i].DisplayName = original
				break
			}
		}
	})

	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewUsersHandler(tmpl)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/users/{userId}/display-name", h.UpdateDisplayNameHandler)

	form := url.Values{}
	form.Set("display_name", "New Name")
	body := strings.NewReader(form.Encode())

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/admin/users/usr-001/display-name", body)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusFound {
		t.Fatalf("want 302 got %d", w.Code)
	}
	location := w.Header().Get("Location")
	if !strings.Contains(location, "/admin/users/usr-001") {
		t.Errorf("expected Location to contain '/admin/users/usr-001', got: %s", location)
	}
	if !strings.Contains(location, "flash=") {
		t.Errorf("expected Location to contain 'flash=', got: %s", location)
	}
}

// TestUpdateDisplayNameEmpty verifies that POST with an empty display_name returns HTTP 400.
// AC: 6 (TestUpdateDisplayNameEmpty — Story 7.6); also AC4 (validation).
func TestUpdateDisplayNameEmpty(t *testing.T) {
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewUsersHandler(tmpl)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/users/{userId}/display-name", h.UpdateDisplayNameHandler)

	form := url.Values{}
	form.Set("display_name", "")
	body := strings.NewReader(form.Encode())

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/admin/users/usr-001/display-name", body)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d", w.Code)
	}
}

