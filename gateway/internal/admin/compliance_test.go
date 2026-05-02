package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCompliancePageRenders verifies GET /admin/compliance returns HTTP 200 with WCAG landmarks.
// AC: 12 (Story 7.11)
func TestCompliancePageRenders(t *testing.T) {
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewComplianceHandler(tmpl)

	req := httptest.NewRequest(http.MethodGet, "/admin/compliance", nil)
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

// TestCompliancePagePendingFilter verifies default filter shows only pending requests.
// AC: 12 (Story 7.11)
func TestCompliancePagePendingFilter(t *testing.T) {
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewComplianceHandler(tmpl)

	req := httptest.NewRequest(http.MethodGet, "/admin/compliance", nil)
	w := httptest.NewRecorder()
	h.ListHandler(w, req)

	if w.Code != 200 {
		t.Fatalf("expected HTTP 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Alice Müller") {
		t.Error("expected 'Alice Müller' (pending) in body for default pending filter")
	}
	if !strings.Contains(body, "Carla Reiter") {
		t.Error("expected 'Carla Reiter' (pending) in body for default pending filter")
	}
	if strings.Contains(body, "Bob Wagner") {
		t.Error("expected 'Bob Wagner' (approved) to be absent for default pending filter")
	}
}

// TestComplianceApprove verifies POST /admin/compliance/{id}/approve transitions status and redirects.
// AC: 12 (Story 7.11)
func TestComplianceApprove(t *testing.T) {
	// Save and restore stubComplianceRequests to avoid test-order dependencies.
	original := make([]StubComplianceRequest, len(stubComplianceRequests))
	copy(original, stubComplianceRequests)
	t.Cleanup(func() { stubComplianceRequests = original })

	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewComplianceHandler(tmpl)

	// Use a mux to resolve {id} path value.
	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/compliance/{id}/approve", h.ApproveHandler)

	req := httptest.NewRequest(http.MethodPost, "/admin/compliance/cr-001/approve", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected HTTP 302, got %d", w.Code)
	}
	location := w.Header().Get("Location")
	if !strings.Contains(location, "flash=") {
		t.Errorf("expected Location to contain 'flash=', got: %s", location)
	}

	// Verify stub was mutated.
	cr := findStubComplianceRequest("cr-001")
	if cr == nil {
		t.Fatal("cr-001 not found after approve")
	}
	if cr.Status != "approved" {
		t.Errorf("expected cr-001 Status='approved', got %q", cr.Status)
	}
}

// TestComplianceReject verifies POST /admin/compliance/{id}/reject transitions status and redirects.
// AC: 12 (Story 7.11)
func TestComplianceReject(t *testing.T) {
	// Save and restore stubComplianceRequests to avoid test-order dependencies.
	original := make([]StubComplianceRequest, len(stubComplianceRequests))
	copy(original, stubComplianceRequests)
	t.Cleanup(func() { stubComplianceRequests = original })

	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewComplianceHandler(tmpl)

	// Use a mux to resolve {id} path value.
	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/compliance/{id}/reject", h.RejectHandler)

	req := httptest.NewRequest(http.MethodPost, "/admin/compliance/cr-002/reject", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected HTTP 302, got %d", w.Code)
	}
	location := w.Header().Get("Location")
	if !strings.Contains(location, "flash=") {
		t.Errorf("expected Location to contain 'flash=', got: %s", location)
	}

	// Verify stub was mutated.
	cr := findStubComplianceRequest("cr-002")
	if cr == nil {
		t.Fatal("cr-002 not found after reject")
	}
	if cr.Status != "rejected" {
		t.Errorf("expected cr-002 Status='rejected', got %q", cr.Status)
	}
}

// TestComplianceWizardStepper verifies the wizard_stepper component renders with aria-current="step".
// AC: 12 (Story 7.11)
func TestComplianceWizardStepper(t *testing.T) {
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewComplianceHandler(tmpl)

	req := httptest.NewRequest(http.MethodGet, "/admin/compliance", nil)
	w := httptest.NewRecorder()
	h.ListHandler(w, req)

	if w.Code != 200 {
		t.Fatalf("expected HTTP 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `aria-current="step"`) {
		t.Error("expected aria-current=\"step\" in body (wizard stepper active step)")
	}
}
