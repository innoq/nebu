package admin

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestConfigPageRenders verifies GET /admin/config returns HTTP 200 with WCAG landmarks
// and the current InstanceName ("Nebu Dev").
// AC: 1, 8 (Story 7.10)
func TestConfigPageRenders(t *testing.T) {
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewConfigHandler(tmpl)

	req := httptest.NewRequest(http.MethodGet, "/admin/config", nil)
	w := httptest.NewRecorder()
	h.Handler(w, req)

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
	if !strings.Contains(body, "Nebu Dev") {
		t.Error("expected 'Nebu Dev' (stubConfig.InstanceName) in body")
	}
}

// TestConfigPageFlashMessage verifies GET /admin/config?flash=Configuration+saved
// renders an alert banner containing "Configuration saved".
// AC: 2, 8 (Story 7.10)
func TestConfigPageFlashMessage(t *testing.T) {
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewConfigHandler(tmpl)

	req := httptest.NewRequest(http.MethodGet, "/admin/config?flash=Configuration+saved", nil)
	w := httptest.NewRecorder()
	h.Handler(w, req)

	if w.Code != 200 {
		t.Fatalf("expected HTTP 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Configuration saved") {
		t.Error("expected 'Configuration saved' in body (flash alert banner)")
	}
}

// TestUpdateConfig verifies POST /admin/config with valid form values returns HTTP 302
// with a Location header containing /admin/config and flash=.
// AC: 6, 8 (Story 7.10)
func TestUpdateConfig(t *testing.T) {
	// Save and restore stubConfig to avoid test-order dependencies.
	original := stubConfig
	t.Cleanup(func() { stubConfig = original })

	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewConfigHandler(tmpl)

	form := url.Values{}
	form.Set("instance_name", "My Server")
	form.Set("allow_registration", "on")
	form.Set("max_rooms_per_user", "20")
	form.Set("retention_days", "365")
	body := strings.NewReader(form.Encode())

	req := httptest.NewRequest(http.MethodPost, "/admin/config", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.UpdateConfigHandler(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected HTTP 302, got %d", w.Code)
	}
	location := w.Header().Get("Location")
	if !strings.Contains(location, "/admin/config") {
		t.Errorf("expected Location to contain '/admin/config', got: %s", location)
	}
	if !strings.Contains(location, "flash=") {
		t.Errorf("expected Location to contain 'flash=', got: %s", location)
	}
}

// TestUpdateConfigEmptyName verifies POST /admin/config with instance_name= returns HTTP 400.
// AC: 6, 8 (Story 7.10)
func TestUpdateConfigEmptyName(t *testing.T) {
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewConfigHandler(tmpl)

	form := url.Values{}
	form.Set("instance_name", "")
	form.Set("allow_registration", "on")
	form.Set("max_rooms_per_user", "10")
	form.Set("retention_days", "90")
	body := strings.NewReader(form.Encode())

	req := httptest.NewRequest(http.MethodPost, "/admin/config", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.UpdateConfigHandler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected HTTP 400 for empty instance_name, got %d", w.Code)
	}
}

// TestUpdateConfigInvalidMaxRooms verifies POST /admin/config with max_rooms_per_user=0 returns HTTP 400.
// AC: 6, 8 (Story 7.10)
func TestUpdateConfigInvalidMaxRooms(t *testing.T) {
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewConfigHandler(tmpl)

	form := url.Values{}
	form.Set("instance_name", "Test")
	form.Set("max_rooms_per_user", "0")
	form.Set("retention_days", "90")
	body := strings.NewReader(form.Encode())

	req := httptest.NewRequest(http.MethodPost, "/admin/config", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.UpdateConfigHandler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected HTTP 400 for max_rooms_per_user=0, got %d", w.Code)
	}
}
