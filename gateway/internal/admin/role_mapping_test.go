package admin

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// newRoleMappingHandler is a test helper that creates a RoleMappingHandler backed by
// a real TemplateHandler (compiles all templates eagerly — catches parse errors early).
func newRoleMappingHandler(t *testing.T) *RoleMappingHandler {
	t.Helper()
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	return NewRoleMappingHandler(tmpl)
}

// TestRoleMappingPageRenders verifies GET /admin/config/role-mapping returns HTTP 200
// with WCAG landmarks and the oidc_group_claim input pre-filled with "groups".
// AC: 3, 8 (Story 7.15)
func TestRoleMappingPageRenders(t *testing.T) {
	h := newRoleMappingHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/config/role-mapping", nil)
	w := httptest.NewRecorder()
	h.Handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "<h1") {
		t.Error("expected <h1 in body")
	}
	if !strings.Contains(body, "Role Mapping") {
		t.Error("expected 'Role Mapping' in body")
	}
	if !strings.Contains(body, `name="oidc_group_claim"`) {
		t.Error("expected oidc_group_claim input in body")
	}
	if !strings.Contains(body, `value="groups"`) {
		t.Error("expected oidc_group_claim input to have value 'groups' (default)")
	}
}

// TestRoleMappingPageFlash verifies GET /admin/config/role-mapping?flash=Role+mapping+saved
// renders an alert banner containing "Role mapping saved".
// AC: 3, 8 (Story 7.15)
func TestRoleMappingPageFlash(t *testing.T) {
	h := newRoleMappingHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/config/role-mapping?flash=Role+mapping+saved", nil)
	w := httptest.NewRecorder()
	h.Handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Role mapping saved") {
		t.Error("expected 'Role mapping saved' in body (flash alert banner)")
	}
}

// TestUpdateRoleMapping verifies POST /admin/config/role-mapping with valid form values
// returns HTTP 302 with a Location header containing /admin/config/role-mapping and flash=.
// It also verifies stubRoleMappingConfig is updated with the posted values.
// AC: 5, 8 (Story 7.15)
func TestUpdateRoleMapping(t *testing.T) {
	// Save and restore stubRoleMappingConfig to avoid test-order dependencies.
	original := stubRoleMappingConfig
	t.Cleanup(func() { stubRoleMappingConfig = original })

	h := newRoleMappingHandler(t)

	form := url.Values{}
	form.Set("oidc_group_claim", "cognito:groups")
	form.Set("instance_admin_group", "admins")
	form.Set("compliance_user_group", "")
	body := strings.NewReader(form.Encode())

	req := httptest.NewRequest(http.MethodPost, "/admin/config/role-mapping", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.UpdateHandler(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected HTTP 302, got %d", w.Code)
	}
	location := w.Header().Get("Location")
	if !strings.Contains(location, "/admin/config/role-mapping") {
		t.Errorf("expected Location to contain '/admin/config/role-mapping', got: %s", location)
	}
	if !strings.Contains(location, "flash=") {
		t.Errorf("expected Location to contain 'flash=', got: %s", location)
	}
	if stubRoleMappingConfig.OIDCGroupClaim != "cognito:groups" {
		t.Errorf("expected OIDCGroupClaim to be 'cognito:groups', got: %s", stubRoleMappingConfig.OIDCGroupClaim)
	}
}

// TestUpdateRoleMappingEmptyClaimName verifies POST with oidc_group_claim= returns HTTP 422
// and re-renders the form with an error for oidc_group_claim.
// AC: 5, 8 (Story 7.15)
func TestUpdateRoleMappingEmptyClaimName(t *testing.T) {
	h := newRoleMappingHandler(t)

	form := url.Values{}
	form.Set("oidc_group_claim", "")
	form.Set("instance_admin_group", "admins")
	form.Set("compliance_user_group", "")
	body := strings.NewReader(form.Encode())

	req := httptest.NewRequest(http.MethodPost, "/admin/config/role-mapping", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.UpdateHandler(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected HTTP 422 for empty oidc_group_claim, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "oidc_group_claim") {
		t.Error("expected body to reference oidc_group_claim error")
	}
}

// TestUpdateRoleMappingInvalidClaimName verifies POST with oidc_group_claim containing a space
// returns HTTP 422 and re-renders the form with an error for oidc_group_claim.
// AC: 5, 8 (Story 7.15)
func TestUpdateRoleMappingInvalidClaimName(t *testing.T) {
	h := newRoleMappingHandler(t)

	form := url.Values{}
	form.Set("oidc_group_claim", "my group") // space is invalid
	form.Set("instance_admin_group", "admins")
	form.Set("compliance_user_group", "")
	body := strings.NewReader(form.Encode())

	req := httptest.NewRequest(http.MethodPost, "/admin/config/role-mapping", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.UpdateHandler(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected HTTP 422 for claim with space, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "oidc_group_claim") {
		t.Error("expected body to reference oidc_group_claim error")
	}
}

// TestUpdateRoleMappingEmptyAdminGroup verifies POST with instance_admin_group= returns HTTP 422
// and re-renders the form with an error for instance_admin_group.
// AC: 5, 8 (Story 7.15)
func TestUpdateRoleMappingEmptyAdminGroup(t *testing.T) {
	h := newRoleMappingHandler(t)

	form := url.Values{}
	form.Set("oidc_group_claim", "groups")
	form.Set("instance_admin_group", "") // required but empty
	form.Set("compliance_user_group", "")
	body := strings.NewReader(form.Encode())

	req := httptest.NewRequest(http.MethodPost, "/admin/config/role-mapping", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.UpdateHandler(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected HTTP 422 for empty instance_admin_group, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "instance_admin_group") {
		t.Error("expected body to reference instance_admin_group error")
	}
}

// TestUpdateRoleMappingOptionalComplianceGroup verifies POST with valid claim, valid admin group,
// and empty compliance_user_group returns HTTP 302 (optional field accepted empty).
// AC: 5, 8 (Story 7.15)
func TestUpdateRoleMappingOptionalComplianceGroup(t *testing.T) {
	original := stubRoleMappingConfig
	t.Cleanup(func() { stubRoleMappingConfig = original })

	h := newRoleMappingHandler(t)

	form := url.Values{}
	form.Set("oidc_group_claim", "groups")
	form.Set("instance_admin_group", "admins")
	form.Set("compliance_user_group", "") // optional — empty is OK
	body := strings.NewReader(form.Encode())

	req := httptest.NewRequest(http.MethodPost, "/admin/config/role-mapping", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.UpdateHandler(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected HTTP 302 for empty (optional) compliance_user_group, got %d", w.Code)
	}
}

// TestUpdateRoleMappingComplianceGroupTooLong verifies POST with compliance_user_group > 100 runes
// returns HTTP 422 and re-renders the form with an error for compliance_user_group.
// AC: 5, 8 (Story 7.15)
func TestUpdateRoleMappingComplianceGroupTooLong(t *testing.T) {
	h := newRoleMappingHandler(t)

	// 101 runes — uses 'a' which is 1 byte = 1 rune, so len == rune count here.
	tooLong := strings.Repeat("a", 101)

	form := url.Values{}
	form.Set("oidc_group_claim", "groups")
	form.Set("instance_admin_group", "admins")
	form.Set("compliance_user_group", tooLong)
	body := strings.NewReader(form.Encode())

	req := httptest.NewRequest(http.MethodPost, "/admin/config/role-mapping", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.UpdateHandler(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected HTTP 422 for compliance_user_group > 100 runes, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "compliance_user_group") {
		t.Error("expected body to reference compliance_user_group error")
	}
}

// TestUpdateRoleMappingClaimTooLong verifies POST with oidc_group_claim > 50 runes
// returns HTTP 422 and re-renders the form with an error for oidc_group_claim.
// AC: 5, 8 (Story 7.15)
func TestUpdateRoleMappingClaimTooLong(t *testing.T) {
	h := newRoleMappingHandler(t)

	// 51 runes of valid characters — hits the length check before the regex check.
	tooLong := strings.Repeat("a", 51)

	form := url.Values{}
	form.Set("oidc_group_claim", tooLong)
	form.Set("instance_admin_group", "admins")
	form.Set("compliance_user_group", "")
	body := strings.NewReader(form.Encode())

	req := httptest.NewRequest(http.MethodPost, "/admin/config/role-mapping", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.UpdateHandler(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected HTTP 422 for oidc_group_claim > 50 runes, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "oidc_group_claim") {
		t.Error("expected body to reference oidc_group_claim error")
	}
}
