package admin

// GREEN PHASE — Story 11-10: OIDC Claim Mapping Configuration
//
// AC coverage:
//   - AC3: Admin UI settings page GET + POST (including DB persistence assertion)
//   - AC8: Validation rules (required, length, regex, dot allowed)
//
// Pattern: follows role_mapping_test.go exactly.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// recordingServerConfigReader is a ServerConfigReader test double that records
// SaveClaimMapping calls so tests can assert DB persistence (MAJOR-1 fix).
type recordingServerConfigReader struct {
	savedUserIDClaim      string
	savedDisplaynameClaim string
	savedEmailClaim       string
	saveCalled            bool
}

func (r *recordingServerConfigReader) LoadOIDCConfig(_ context.Context) (string, string, string, error) {
	return "", "", "", nil
}

func (r *recordingServerConfigReader) CompleteBootstrap(_ context.Context) error { return nil }

func (r *recordingServerConfigReader) LoadAdminGroupClaim(_ context.Context) (string, error) {
	return "instance_admin", nil
}

func (r *recordingServerConfigReader) SaveAdminGroupClaim(_ context.Context, _ string) error {
	return nil
}

func (r *recordingServerConfigReader) LoadClaimMapping(_ context.Context) (string, string, string, error) {
	return "sub", "name", "email", nil
}

func (r *recordingServerConfigReader) SaveClaimMapping(_ context.Context, uid, dn, em string) error {
	r.savedUserIDClaim = uid
	r.savedDisplaynameClaim = dn
	r.savedEmailClaim = em
	r.saveCalled = true
	return nil
}

func (r *recordingServerConfigReader) LoadServerConfigKey(_ context.Context, _ string) (string, error) {
	return "", nil
}

// newClaimMappingHandlerForTest creates a ClaimMappingHandler backed by a
// recordingServerConfigReader so tests can assert SaveClaimMapping was called.
func newClaimMappingHandlerForTest(t *testing.T) (*ClaimMappingHandler, *recordingServerConfigReader) {
	t.Helper()
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	reader := &recordingServerConfigReader{}
	return NewClaimMappingHandler(tmpl, reader), reader
}

// newClaimMappingHandler is a test helper that creates a ClaimMappingHandler backed
// by a real TemplateHandler (compile-time catch for template parse errors).
// Mirrors newRoleMappingHandler in role_mapping_test.go.
func newClaimMappingHandler(t *testing.T) *ClaimMappingHandler {
	t.Helper()
	h, _ := newClaimMappingHandlerForTest(t)
	return h
}

// TestClaimMappingHandler_GetDefaults verifies GET /admin/config/claim-mapping returns
// HTTP 200 and renders the form with pre-filled defaults: sub, name, email.
// AC3 — "the page renders a form showing the three configurable claim fields pre-populated
// from server_config (with the Nebu defaults if the keys are absent)."
func TestClaimMappingHandler_GetDefaults(t *testing.T) {
	h := newClaimMappingHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/config/claim-mapping", nil)
	w := httptest.NewRecorder()
	h.Handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Claim Mapping") {
		t.Error("expected 'Claim Mapping' heading in body")
	}
	if !strings.Contains(body, `name="oidc_user_id_claim"`) {
		t.Error("expected oidc_user_id_claim input in body")
	}
	if !strings.Contains(body, `name="oidc_displayname_claim"`) {
		t.Error("expected oidc_displayname_claim input in body")
	}
	if !strings.Contains(body, `name="oidc_email_claim"`) {
		t.Error("expected oidc_email_claim input in body")
	}
	// Default values
	if !strings.Contains(body, `value="sub"`) {
		t.Error("expected default oidc_user_id_claim value 'sub'")
	}
	if !strings.Contains(body, `value="name"`) {
		t.Error("expected default oidc_displayname_claim value 'name'")
	}
	if !strings.Contains(body, `value="email"`) {
		t.Error("expected default oidc_email_claim value 'email'")
	}
}

// TestClaimMappingHandler_GetFlash verifies GET /admin/config/claim-mapping?flash=Claim+mapping+updated
// renders an alert banner containing "Claim mapping updated".
// AC3 — PRG pattern success flash.
func TestClaimMappingHandler_GetFlash(t *testing.T) {
	h := newClaimMappingHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/config/claim-mapping?flash=Claim+mapping+updated", nil)
	w := httptest.NewRecorder()
	h.Handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Claim mapping updated") {
		t.Error("expected 'Claim mapping updated' flash alert in body")
	}
}

// TestClaimMappingHandler_PostValid verifies POST /admin/config/claim-mapping with valid
// form fields returns HTTP 302 redirect to the same page with flash=Claim+mapping+updated
// AND that SaveClaimMapping was called with the submitted values (DB persistence).
// AC3 — "the updated values are persisted to server_config ... and the page shows a success
// flash message (PRG pattern: redirect to ?flash=...)".
func TestClaimMappingHandler_PostValid(t *testing.T) {
	h, reader := newClaimMappingHandlerForTest(t)

	form := url.Values{}
	form.Set("oidc_user_id_claim", "preferred_username")
	form.Set("oidc_displayname_claim", "name")
	form.Set("oidc_email_claim", "email")
	body := strings.NewReader(form.Encode())

	req := httptest.NewRequest(http.MethodPost, "/admin/config/claim-mapping", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.UpdateHandler(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected HTTP 302, got %d", w.Code)
	}
	location := w.Header().Get("Location")
	if !strings.Contains(location, "/admin/config/claim-mapping") {
		t.Errorf("expected Location to contain '/admin/config/claim-mapping', got: %s", location)
	}
	if !strings.Contains(location, "flash=") {
		t.Errorf("expected Location to contain 'flash=', got: %s", location)
	}
	// Assert DB persistence (MAJOR-1 fix): SaveClaimMapping must have been called.
	if !reader.saveCalled {
		t.Error("expected SaveClaimMapping to be called, but it was not")
	}
	if reader.savedUserIDClaim != "preferred_username" {
		t.Errorf("expected savedUserIDClaim = 'preferred_username', got %q", reader.savedUserIDClaim)
	}
}

// TestClaimMappingHandler_PostInvalid_EmptyUserIDClaim verifies POST with oidc_user_id_claim=""
// returns HTTP 422 and re-renders the form with an error for oidc_user_id_claim.
// AC8 — "Claim name: required, 1–50 chars...POST returns HTTP 422 and re-renders the form
// with per-field errors on validation failure."
func TestClaimMappingHandler_PostInvalid_EmptyUserIDClaim(t *testing.T) {
	h := newClaimMappingHandler(t)

	form := url.Values{}
	form.Set("oidc_user_id_claim", "") // required but empty
	form.Set("oidc_displayname_claim", "name")
	form.Set("oidc_email_claim", "email")
	body := strings.NewReader(form.Encode())

	req := httptest.NewRequest(http.MethodPost, "/admin/config/claim-mapping", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.UpdateHandler(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected HTTP 422, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "oidc_user_id_claim") {
		t.Error("expected body to reference oidc_user_id_claim error")
	}
}

// TestClaimMappingHandler_PostInvalid_EmptyDisplaynameClaim verifies POST with
// oidc_displayname_claim="" returns HTTP 422.
// AC8 — all three fields are required.
func TestClaimMappingHandler_PostInvalid_EmptyDisplaynameClaim(t *testing.T) {
	h := newClaimMappingHandler(t)

	form := url.Values{}
	form.Set("oidc_user_id_claim", "sub")
	form.Set("oidc_displayname_claim", "") // required but empty
	form.Set("oidc_email_claim", "email")
	body := strings.NewReader(form.Encode())

	req := httptest.NewRequest(http.MethodPost, "/admin/config/claim-mapping", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.UpdateHandler(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected HTTP 422, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "oidc_displayname_claim") {
		t.Error("expected body to reference oidc_displayname_claim error")
	}
}

// TestClaimMappingHandler_PostInvalid_EmptyEmailClaim verifies POST with
// oidc_email_claim="" returns HTTP 422.
// AC8 — all three fields are required.
func TestClaimMappingHandler_PostInvalid_EmptyEmailClaim(t *testing.T) {
	h := newClaimMappingHandler(t)

	form := url.Values{}
	form.Set("oidc_user_id_claim", "sub")
	form.Set("oidc_displayname_claim", "name")
	form.Set("oidc_email_claim", "") // required but empty
	body := strings.NewReader(form.Encode())

	req := httptest.NewRequest(http.MethodPost, "/admin/config/claim-mapping", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.UpdateHandler(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected HTTP 422, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "oidc_email_claim") {
		t.Error("expected body to reference oidc_email_claim error")
	}
}

// TestClaimMappingHandler_PostInvalid_TooLong verifies POST with oidc_user_id_claim > 50 chars
// returns HTTP 422.
// AC8 — "1–50 chars" length constraint.
func TestClaimMappingHandler_PostInvalid_TooLong(t *testing.T) {
	h := newClaimMappingHandler(t)

	tooLong := strings.Repeat("a", 51) // 51 runes — exceeds max

	form := url.Values{}
	form.Set("oidc_user_id_claim", tooLong)
	form.Set("oidc_displayname_claim", "name")
	form.Set("oidc_email_claim", "email")
	body := strings.NewReader(form.Encode())

	req := httptest.NewRequest(http.MethodPost, "/admin/config/claim-mapping", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.UpdateHandler(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected HTTP 422 for claim > 50 chars, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "oidc_user_id_claim") {
		t.Error("expected body to reference oidc_user_id_claim error")
	}
}

// TestClaimMappingHandler_PostInvalid_IllegalChars verifies POST with oidc_user_id_claim
// containing a space (invalid character) returns HTTP 422.
// AC8 — "matches ^[a-zA-Z0-9:_\-\.]+$".
func TestClaimMappingHandler_PostInvalid_IllegalChars(t *testing.T) {
	h := newClaimMappingHandler(t)

	form := url.Values{}
	form.Set("oidc_user_id_claim", "my claim") // space is not in allowlist
	form.Set("oidc_displayname_claim", "name")
	form.Set("oidc_email_claim", "email")
	body := strings.NewReader(form.Encode())

	req := httptest.NewRequest(http.MethodPost, "/admin/config/claim-mapping", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.UpdateHandler(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected HTTP 422 for claim with space, got %d", w.Code)
	}
}

// TestClaimMappingHandler_PostValid_DotInClaimName verifies POST with oidc_email_claim="user.email"
// (dot is allowed per AC8) returns HTTP 302 success AND saves to DB.
// AC8 — "dot allowed for nested claims like user.email".
func TestClaimMappingHandler_PostValid_DotInClaimName(t *testing.T) {
	h, reader := newClaimMappingHandlerForTest(t)

	form := url.Values{}
	form.Set("oidc_user_id_claim", "sub")
	form.Set("oidc_displayname_claim", "name")
	form.Set("oidc_email_claim", "user.email") // dot allowed
	body := strings.NewReader(form.Encode())

	req := httptest.NewRequest(http.MethodPost, "/admin/config/claim-mapping", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.UpdateHandler(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected HTTP 302 for valid claim with dot, got %d (body: %s)", w.Code, w.Body.String())
	}
	if !reader.saveCalled {
		t.Error("expected SaveClaimMapping to be called, but it was not")
	}
	if reader.savedEmailClaim != "user.email" {
		t.Errorf("expected savedEmailClaim = 'user.email', got %q", reader.savedEmailClaim)
	}
}
