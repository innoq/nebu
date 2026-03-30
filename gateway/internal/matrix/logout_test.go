package matrix_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nebu/nebu/internal/matrix"
	"github.com/nebu/nebu/internal/middleware"
)

func TestPostLogout_ValidToken(t *testing.T) {
	denylist := middleware.NewDenylist()
	handler := matrix.NewLogoutHandler(denylist)

	rawToken := "test-token-12345"
	expiry := time.Now().Add(time.Hour)

	req := httptest.NewRequest("POST", "/_matrix/client/v3/logout", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	ctx := context.WithValue(req.Context(), middleware.ContextKeyTokenExpiry, expiry)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	handler.PostLogout(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if !denylist.Contains(rawToken) {
		t.Error("expected token to be in denylist after logout")
	}
}

func TestPostLogout_AddsCorrectExpiry(t *testing.T) {
	denylist := middleware.NewDenylist()
	handler := matrix.NewLogoutHandler(denylist)

	rawToken := "expiry-test-token"
	// Use a past expiry — the handler should still call Add(), but Contains()
	// returns false because the entry is already expired. This proves the handler
	// passes the context expiry through (not a hardcoded future value).
	expiry := time.Now().Add(-time.Second)

	req := httptest.NewRequest("POST", "/_matrix/client/v3/logout", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	ctx := context.WithValue(req.Context(), middleware.ContextKeyTokenExpiry, expiry)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	handler.PostLogout(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if denylist.Contains(rawToken) {
		t.Error("expected expired denylist entry to not be contained")
	}
}

func TestPostLogout_EmptyBody(t *testing.T) {
	denylist := middleware.NewDenylist()
	handler := matrix.NewLogoutHandler(denylist)

	req := httptest.NewRequest("POST", "/_matrix/client/v3/logout", nil)
	req.Header.Set("Authorization", "Bearer sometoken")
	ctx := context.WithValue(req.Context(), middleware.ContextKeyTokenExpiry, time.Now().Add(time.Hour))
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	handler.PostLogout(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	if body != "{}\n" {
		t.Errorf("expected body to be {}\\n, got %q", body)
	}
}
