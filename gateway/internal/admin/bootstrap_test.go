package admin

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeBootstrapChecker struct {
	active bool
	err    error
}

var errFakeDB = sql.ErrConnDone

func (f *fakeBootstrapChecker) IsBootstrapActive(_ context.Context) (bool, error) {
	return f.active, f.err
}

func newTestBootstrapHandler(t *testing.T, checker BootstrapStatusChecker) *BootstrapHandler {
	t.Helper()
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	// Use fake implementations for tests that don't need real DB persistence
	h := &BootstrapHandler{
		checker:    checker,
		tmpl:       tmpl,
		persister:  &fakeBootstrapPersister{},
		draftStore: &fakeBootstrapDraftStore{},
		secret:     []byte("test-secret"),
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
	return h
}

// TestBootstrapHandler_Active verifies that an active bootstrap renders step 1 HTML.
func TestBootstrapHandler_Active(t *testing.T) {
	checker := &fakeBootstrapChecker{active: true}
	handler := newTestBootstrapHandler(t, checker)

	req := httptest.NewRequest("GET", "/admin/bootstrap", nil)
	rr := httptest.NewRecorder()
	handler.Handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	ct := rr.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Errorf("expected Content-Type text/html, got %q", ct)
	}

	body := rr.Body.String()
	if !strings.Contains(body, `name="instance_name"`) {
		t.Error("expected HTML to contain instance_name input field")
	}
}
