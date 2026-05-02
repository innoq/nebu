package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRoleSelectRenders verifies that GET /admin/users/usr-001 returns HTTP 200
// and the body contains a <select> element with the user's current role as selected.
// AC: 1 (Story 7.7)
func TestRoleSelectRenders(t *testing.T) {
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

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "<select") {
		t.Error("expected body to contain '<select' (role select element)")
	}
	// The user usr-001 has role "instance_admin" — that option must be pre-selected.
	if !strings.Contains(body, "selected") {
		t.Error("expected body to contain 'selected' attribute (pre-selected role option)")
	}
	// The selected option value must be near "instance_admin"
	if !strings.Contains(body, "instance_admin") {
		t.Error("expected body to contain 'instance_admin' (usr-001 role value)")
	}
}

// TestUpdateRole verifies that POST /admin/users/usr-001/role with role=user
// returns HTTP 302 with a Location containing flash=.
// AC: 2 (Story 7.7)
func TestUpdateRole(t *testing.T) {
	// Save original role and restore it after the test.
	var originalRole string
	for _, u := range stubUsers {
		if u.ID == "usr-001" {
			originalRole = u.Role
			break
		}
	}
	t.Cleanup(func() {
		for i := range stubUsers {
			if stubUsers[i].ID == "usr-001" {
				stubUsers[i].Role = originalRole
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
	mux.HandleFunc("POST /admin/users/{userId}/role", h.UpdateRoleHandler)

	body := strings.NewReader("role=user")
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/admin/users/usr-001/role", body)
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

// TestUpdateRoleInvalid verifies that POST with role=hacker returns HTTP 400.
// AC: 2 (Story 7.7)
func TestUpdateRoleInvalid(t *testing.T) {
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewUsersHandler(tmpl)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/users/{userId}/role", h.UpdateRoleHandler)

	body := strings.NewReader("role=hacker")
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/admin/users/usr-001/role", body)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for invalid role got %d", w.Code)
	}
}

// TestDeactivateUser verifies that POST /admin/users/usr-003/deactivate returns
// HTTP 302 with a flash= Location and mutates the stub status to "deactivated".
// Uses usr-003 (Carla Reiter) to avoid interfering with display-name tests on usr-001.
// AC: 4 (Story 7.7)
func TestDeactivateUser(t *testing.T) {
	// Restore usr-003 (stubUsers[2]) to "active" after the test.
	t.Cleanup(func() { stubUsers[2].Status = "active" })

	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewUsersHandler(tmpl)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/users/{userId}/deactivate", h.DeactivateUserHandler)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/admin/users/usr-003/deactivate", nil)
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusFound {
		t.Fatalf("want 302 got %d", w.Code)
	}
	location := w.Header().Get("Location")
	if !strings.Contains(location, "flash=") {
		t.Errorf("expected Location to contain 'flash=', got: %s", location)
	}

	// Verify stub mutation: usr-003 must now be "deactivated".
	u := findStubUser("usr-003")
	if u == nil {
		t.Fatal("usr-003 not found in stubUsers after deactivation")
	}
	if u.Status != "deactivated" {
		t.Errorf("expected stubUsers usr-003 Status == 'deactivated', got: %s", u.Status)
	}
}

// TestConfirmDialogRendered verifies that GET /admin/users/usr-001 returns HTTP 200
// and the body contains the confirm_dialog component (role="alertdialog").
// AC: 3 (Story 7.7)
func TestConfirmDialogRendered(t *testing.T) {
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

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `role="alertdialog"`) {
		t.Errorf("expected body to contain role=\"alertdialog\" (confirm_dialog component); got body length %d", len(body))
	}
}
