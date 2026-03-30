package matrix

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetLogin_ReturnsSSO(t *testing.T) {
	h := NewLoginHandler("Test SSO")
	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/login", nil)
	w := httptest.NewRecorder()

	h.GetLogin(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected application/json, got %s", ct)
	}

	var resp LoginResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(resp.Flows) != 1 {
		t.Fatalf("expected 1 flow, got %d", len(resp.Flows))
	}
	if resp.Flows[0].Type != "m.login.sso" {
		t.Errorf("expected m.login.sso, got %s", resp.Flows[0].Type)
	}

	idps := resp.Flows[0].IdentityProviders
	if len(idps) != 1 {
		t.Fatalf("expected 1 identity provider, got %d", len(idps))
	}
	if idps[0].ID != "oidc" {
		t.Errorf("expected id oidc, got %s", idps[0].ID)
	}
	if idps[0].Name != "Test SSO" {
		t.Errorf("expected name Test SSO, got %s", idps[0].Name)
	}
	if idps[0].Icon != nil {
		t.Errorf("expected icon nil, got %v", idps[0].Icon)
	}
}
