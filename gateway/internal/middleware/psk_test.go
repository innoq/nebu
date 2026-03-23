package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPSKMiddleware_ValidToken(t *testing.T) {
	secret := "test-secret"
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	handler := PSKMiddleware(secret)(next)
	req := httptest.NewRequest("POST", "/internal/test", nil)
	req.Header.Set("Authorization", "Bearer test-secret")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if !called {
		t.Error("expected next handler to be called")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("got %d, want 200", rr.Code)
	}
}

func TestPSKMiddleware_MissingHeader(t *testing.T) {
	secret := "test-secret"
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler should not be called")
	})
	handler := PSKMiddleware(secret)(next)
	req := httptest.NewRequest("POST", "/internal/test", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want 401", rr.Code)
	}
	if rr.Body.Len() != 0 {
		t.Errorf("expected empty body, got: %q", rr.Body.String())
	}
}

func TestPSKMiddleware_WrongPSK(t *testing.T) {
	secret := "correct-secret"
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler should not be called")
	})
	handler := PSKMiddleware(secret)(next)
	req := httptest.NewRequest("POST", "/internal/test", nil)
	req.Header.Set("Authorization", "Bearer wrong-secret")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want 401", rr.Code)
	}
	if rr.Body.Len() != 0 {
		t.Errorf("expected empty body, got: %q", rr.Body.String())
	}
}

func TestPSKMiddleware_BearerPrefixOnly(t *testing.T) {
	secret := "test-secret"
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler should not be called")
	})
	handler := PSKMiddleware(secret)(next)
	req := httptest.NewRequest("POST", "/internal/test", nil)
	req.Header.Set("Authorization", "Bearer ")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want 401", rr.Code)
	}
}
