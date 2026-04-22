package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// stubProviderCache replaces globalProviderCache for the duration of a test,
// restoring the original on cleanup.
func stubProviderCache(t *testing.T, fn func(ctx context.Context, issuer string) (oidcProvider, error)) {
	t.Helper()
	orig := globalProviderCache
	globalProviderCache = newOIDCProviderCache(fn)
	t.Cleanup(func() { globalProviderCache = orig })
}

// TestAdminError_DoesNotLeakErrorString verifies that when the OIDC provider
// cache returns a DB-level error, the response body does NOT contain the raw
// error string (e.g. "connection refused") — only a generic user-facing message.
func TestAdminError_DoesNotLeakErrorString(t *testing.T) {
	stubProviderCache(t, func(_ context.Context, _ string) (oidcProvider, error) {
		return nil, errors.New("pq: connection refused")
	})

	reader := &fakeServerConfigReader{
		issuer:       "http://localhost:5556",
		clientID:     "test-client-id",
		clientSecret: "test-secret",
	}

	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	a := newTestAdminAuthWithReader(t, reader, tmpl)

	req := httptest.NewRequest("GET", "/admin/login/start", nil)
	req.Host = "localhost"
	rr := httptest.NewRecorder()
	a.LoginStartHandler(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected HTTP 500, got %d", rr.Code)
	}

	// The error page must be HTML (rendered template), not plain text.
	ct := rr.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Errorf("expected Content-Type text/html, got %q", ct)
	}

	body := rr.Body.String()
	if strings.Contains(body, "connection refused") {
		t.Errorf("response body must NOT contain raw error string 'connection refused'; body: %s", body)
	}
	if strings.Contains(body, "pq:") {
		t.Errorf("response body must NOT contain DB error prefix 'pq:'; body: %s", body)
	}
}

// TestAdminError_IncludesRequestID verifies that error responses include a
// non-empty request ID in the X-Request-ID header, enabling log correlation
// when users contact support.
func TestAdminError_IncludesRequestID(t *testing.T) {
	stubProviderCache(t, func(_ context.Context, _ string) (oidcProvider, error) {
		return nil, errors.New("pq: connection refused")
	})

	reader := &fakeServerConfigReader{
		issuer:       "http://localhost:5556",
		clientID:     "test-client-id",
		clientSecret: "test-secret",
	}

	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	a := newTestAdminAuthWithReader(t, reader, tmpl)

	req := httptest.NewRequest("GET", "/admin/login/start", nil)
	req.Host = "localhost"
	rr := httptest.NewRecorder()
	a.LoginStartHandler(rr, req)

	reqID := rr.Header().Get("X-Request-ID")
	if reqID == "" {
		t.Error("X-Request-ID header must be set on error responses")
	}
	if len(reqID) < 8 {
		t.Errorf("X-Request-ID too short (%d chars), expected at least 8", len(reqID))
	}

	// Request ID should also appear in the response body so users can reference it.
	body := rr.Body.String()
	if !strings.Contains(body, reqID) {
		t.Errorf("response body should contain request ID %q for user reference; body: %s", reqID, body)
	}
}
