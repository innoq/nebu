package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestServeCSS(t *testing.T) {
	tests := []struct {
		name         string
		wantStatus   int
		wantCT       string
		wantCC       string
		wantNonEmpty bool
	}{
		{
			name:         "serves admin.css",
			wantStatus:   http.StatusOK,
			wantCT:       "text/css",
			wantCC:       "public, max-age=31536000, immutable",
			wantNonEmpty: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/admin/static/admin.css", nil)
			w := httptest.NewRecorder()
			ServeCSS(w, req)
			if w.Code != tc.wantStatus {
				t.Errorf("status: got %d, want %d", w.Code, tc.wantStatus)
			}
			if ct := w.Header().Get("Content-Type"); ct != tc.wantCT {
				t.Errorf("Content-Type: got %q, want %q", ct, tc.wantCT)
			}
			if cc := w.Header().Get("Cache-Control"); cc != tc.wantCC {
				t.Errorf("Cache-Control: got %q, want %q", cc, tc.wantCC)
			}
			if tc.wantNonEmpty && w.Body.Len() == 0 {
				t.Error("expected non-empty body, got empty")
			}
		})
	}
}
