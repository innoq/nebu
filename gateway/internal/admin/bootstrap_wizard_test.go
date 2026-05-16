package admin

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestBootstrapWizard_Step1_GET verifies GET /admin/bootstrap renders step 1.
func TestBootstrapWizard_Step1_GET(t *testing.T) {
	handler := newTestBootstrapHandler(t, &fakeBootstrapChecker{active: true})

	req := httptest.NewRequest("GET", "/admin/bootstrap", nil)
	rr := httptest.NewRecorder()
	handler.Handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	body := rr.Body.String()
	if !strings.Contains(body, `name="instance_name"`) {
		t.Error("step 1 should contain input[name=instance_name]")
	}
}

// TestBootstrapWizard_Step2_Fields verifies template with Step:2 contains OIDC fields.
func TestBootstrapWizard_Step2_Fields(t *testing.T) {
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}

	w := httptest.NewRecorder()
	data := BootstrapPageData{
		PageData:     PageData{BootstrapMode: true, ActiveNav: "bootstrap"},
		Step:         2,
		InstanceName: "my-instance",
	}
	tmpl.render(w, "bootstrap", data)

	body := w.Body.String()
	for _, field := range []string{`name="oidc_issuer"`, `name="oidc_client_id"`, `name="oidc_client_secret"`} {
		if !strings.Contains(body, field) {
			t.Errorf("step 2 should contain %s", field)
		}
	}
}

// TestBootstrapWizard_Step2_NextButton verifies template with Step:2 contains a "Next" button.
// "Connect with OIDC" moved to Step 3 (Claim Mapping) in Story 11-10.
func TestBootstrapWizard_Step2_ConnectButton(t *testing.T) {
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}

	w := httptest.NewRecorder()
	data := BootstrapPageData{
		PageData:     PageData{BootstrapMode: true, ActiveNav: "bootstrap"},
		Step:         2,
		InstanceName: "my-instance",
	}
	tmpl.render(w, "bootstrap", data)

	body := w.Body.String()
	// Step 2 now shows "Next" (advances to Step 3 Claim Mapping) — "Connect with OIDC" is in Step 3.
	if !strings.Contains(body, "Next") {
		t.Error("step 2 should contain 'Next' button (advances to Claim Mapping step)")
	}
}

// TestBootstrapWizard_Step3_ConnectButton verifies template with Step:3 contains "Connect with OIDC" button.
func TestBootstrapWizard_Step3_ConnectButton(t *testing.T) {
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}

	w := httptest.NewRecorder()
	data := BootstrapPageData{
		PageData:            PageData{BootstrapMode: true, ActiveNav: "bootstrap"},
		Step:                3,
		InstanceName:        "my-instance",
		OIDCUserIDClaim:     "sub",
		OIDCDisplaynameClaim: "name",
		OIDCEmailClaim:      "email",
	}
	tmpl.render(w, "bootstrap", data)

	body := w.Body.String()
	if !strings.Contains(body, "Connect with OIDC") {
		t.Error("step 3 should contain 'Connect with OIDC' button")
	}
	if !strings.Contains(body, `name="oidc_user_id_claim"`) {
		t.Error("step 3 should contain oidc_user_id_claim field")
	}
}

// TestBootstrapWizard_ActiveNav verifies that ActiveNav=bootstrap renders the nav highlight.
func TestBootstrapWizard_ActiveNav(t *testing.T) {
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}

	w := httptest.NewRecorder()
	data := BootstrapPageData{
		PageData: PageData{BootstrapMode: true, ActiveNav: "bootstrap"},
		Step:     1,
	}
	tmpl.render(w, "bootstrap", data)

	body := w.Body.String()
	bootIdx := strings.Index(body, `data-navkey="bootstrap"`)
	if bootIdx < 0 {
		t.Fatal("bootstrap nav item not found")
	}
	bootBlock := body[max(0, bootIdx-200):min(len(body), bootIdx+200)]
	if !strings.Contains(bootBlock, "active") {
		t.Error("bootstrap nav item should have active class when ActiveNav=bootstrap")
	}
}

// TestBootstrapWizard_StepHandler_Step1_Valid verifies valid step 1 POST advances to step 2.
func TestBootstrapWizard_StepHandler_Step1_Valid(t *testing.T) {
	handler := newTestBootstrapHandler(t, &fakeBootstrapChecker{active: true})

	form := url.Values{}
	form.Set("step", "1")
	form.Set("instance_name", "my-nebu-instance")

	req := httptest.NewRequest("POST", "/admin/bootstrap", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	handler.StepHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `name="oidc_issuer"`) {
		t.Error("after valid step 1, should render step 2 with oidc_issuer field")
	}
}

// TestBootstrapWizard_StepHandler_Step1_Invalid verifies invalid step 1 POST re-renders step 1 with error.
func TestBootstrapWizard_StepHandler_Step1_Invalid(t *testing.T) {
	handler := newTestBootstrapHandler(t, &fakeBootstrapChecker{active: true})

	form := url.Values{}
	form.Set("step", "1")
	form.Set("instance_name", "ab") // too short

	req := httptest.NewRequest("POST", "/admin/bootstrap", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	handler.StepHandler(rr, req)

	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `name="instance_name"`) {
		t.Error("on error, should re-render step 1 with instance_name field")
	}
}

// TestBootstrapWizard_StepHandler_BackNavigation verifies back navigation re-renders target step.
func TestBootstrapWizard_StepHandler_BackNavigation(t *testing.T) {
	handler := newTestBootstrapHandler(t, &fakeBootstrapChecker{active: true})

	form := url.Values{}
	form.Set("step", "2")
	form.Set("instance_name", "my-nebu-instance")
	form.Set("go_back", "1")

	req := httptest.NewRequest("POST", "/admin/bootstrap", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	handler.StepHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `name="instance_name"`) {
		t.Error("back navigation to step 1 should render step 1 with instance_name field")
	}
	// Should NOT show step 2 visible form when going back (hidden carry-fields are OK)
	if strings.Contains(body, `Step 2: OIDC Configuration`) {
		t.Error("back navigation to step 1 should not show step 2 content")
	}
}

// ── Story 14-1b: AT#5 — Bootstrap Wizard Step 3 info text ────────────────────

// TestBootstrapHandler_Step3_ShowsInfoText covers AT#5 (AC3) [P0]:
// Bootstrap Wizard Step 3 must always show the info text
// "The Matrix User ID claim cannot be changed after completing setup."
// This is informational and displayed pre-bootstrap (wizard runs before bootstrap is complete).
//
// RED PHASE — fails until bootstrap.html Step 3 includes the info text.
func TestBootstrapHandler_Step3_ShowsInfoText(t *testing.T) {
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("[AT#5/AC3] NewTemplateHandler: %v", err)
	}

	w := httptest.NewRecorder()
	// Render Step 3 — bootstrap is always pre-bootstrap when the wizard renders.
	data := BootstrapPageData{
		PageData:         PageData{BootstrapMode: true, ActiveNav: "bootstrap"},
		Step:             3,
		InstanceName:     "my-instance",
		OIDCIssuer:       "https://dex.example.com",
		OIDCClientID:     "nebu-client",
		OIDCUserIDClaim:  "email",
		OIDCDisplaynameClaim: "name",
		OIDCEmailClaim:   "email",
	}
	tmpl.render(w, "bootstrap", data)

	body := w.Body.String()
	if !strings.Contains(body, "The Matrix User ID claim cannot be changed after completing setup") {
		t.Error("[AT#5/AC3] expected info text 'The Matrix User ID claim cannot be changed after completing setup' in bootstrap step 3")
	}
}
