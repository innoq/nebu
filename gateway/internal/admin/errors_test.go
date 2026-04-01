package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestError401_StatusAndHeading verifies Error401 returns HTTP 401, the correct
// Content-Type, and a body containing the "Authentication Required" heading.
func TestError401_StatusAndHeading(t *testing.T) {
	h, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/admin/dashboard", nil)
	Error401(w, r, h)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type: got %q, want %q", ct, "text/html; charset=utf-8")
	}
	if !strings.Contains(w.Body.String(), "Authentication Required") {
		t.Error("expected body to contain 'Authentication Required'")
	}
}

// TestError403_StatusAndHeading verifies Error403 returns HTTP 403, the correct
// Content-Type, and a body containing the "Access Denied" heading.
func TestError403_StatusAndHeading(t *testing.T) {
	h, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/admin/dashboard", nil)
	Error403(w, r, h)
	if w.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusForbidden)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type: got %q, want %q", ct, "text/html; charset=utf-8")
	}
	if !strings.Contains(w.Body.String(), "Access Denied") {
		t.Error("expected body to contain 'Access Denied'")
	}
}

// TestError404_StatusAndHeading verifies Error404 returns HTTP 404, the correct
// Content-Type, and a body containing the "Page Not Found" heading.
func TestError404_StatusAndHeading(t *testing.T) {
	h, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/admin/nonexistent", nil)
	Error404(w, r, h)
	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusNotFound)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type: got %q, want %q", ct, "text/html; charset=utf-8")
	}
	if !strings.Contains(w.Body.String(), "Page Not Found") {
		t.Error("expected body to contain 'Page Not Found'")
	}
}

// TestError500_StatusAndHeading verifies Error500 returns HTTP 500, the correct
// Content-Type, and a body containing the "Internal Server Error" heading.
func TestError500_StatusAndHeading(t *testing.T) {
	h, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/admin/dashboard", nil)
	Error500(w, r, h)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusInternalServerError)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type: got %q, want %q", ct, "text/html; charset=utf-8")
	}
	if !strings.Contains(w.Body.String(), "Internal Server Error") {
		t.Error("expected body to contain 'Internal Server Error'")
	}
}

// TestError500_NoStackTrace verifies that Error500 does not leak any Go runtime
// debug information (goroutine dumps, panic messages, runtime paths) in its response.
func TestError500_NoStackTrace(t *testing.T) {
	h, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/admin/dashboard", nil)
	Error500(w, r, h)
	body := w.Body.String()
	for _, forbidden := range []string{"goroutine", "panic", "runtime/"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("error page must not contain %q", forbidden)
		}
	}
}
