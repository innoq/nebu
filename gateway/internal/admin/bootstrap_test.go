package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeBootstrapChecker struct {
	active bool
	err    error
}

var errFakeDB = sql.ErrConnDone

func (f *fakeBootstrapChecker) IsBootstrapActive(_ context.Context) (bool, error) {
	return f.active, f.err
}

func TestBootstrapHandler_Active(t *testing.T) {
	checker := &fakeBootstrapChecker{active: true}
	handler := NewBootstrapHandler(checker)

	req := httptest.NewRequest("GET", "/admin/bootstrap", nil)
	rr := httptest.NewRecorder()
	handler.Handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	ct := rr.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}

	var resp bootstrapResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if !resp.BootstrapActive {
		t.Error("expected bootstrap_active=true, got false")
	}
}

func TestBootstrapHandler_NotActive(t *testing.T) {
	checker := &fakeBootstrapChecker{active: false}
	handler := NewBootstrapHandler(checker)

	req := httptest.NewRequest("GET", "/admin/bootstrap", nil)
	rr := httptest.NewRecorder()
	handler.Handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	var resp bootstrapResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if resp.BootstrapActive {
		t.Error("expected bootstrap_active=false, got true")
	}
}

func TestBootstrapHandler_Error(t *testing.T) {
	checker := &fakeBootstrapChecker{err: errFakeDB}
	handler := NewBootstrapHandler(checker)

	req := httptest.NewRequest("GET", "/admin/bootstrap", nil)
	rr := httptest.NewRecorder()
	handler.Handler(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr.Code)
	}
}
