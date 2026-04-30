package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestServeFavicon(t *testing.T) {
	req := httptest.NewRequest("GET", "/favicon.ico", nil)
	w := httptest.NewRecorder()
	ServeFavicon(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusOK)
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/x-icon" {
		t.Errorf("Content-Type: got %q, want %q", ct, "image/x-icon")
	}
	if w.Body.Len() == 0 {
		t.Error("expected non-empty body for favicon.ico")
	}
}

func TestServeIconFile(t *testing.T) {
	tests := []struct {
		name       string
		filename   string
		wantStatus int
		wantCT     string
	}{
		{
			name:       "serves icon.svg",
			filename:   "icon.svg",
			wantStatus: http.StatusOK,
			wantCT:     "image/svg+xml",
		},
		{
			name:       "serves icon-32.png",
			filename:   "icon-32.png",
			wantStatus: http.StatusOK,
			wantCT:     "image/png",
		},
		{
			name:       "serves icon-192.png",
			filename:   "icon-192.png",
			wantStatus: http.StatusOK,
			wantCT:     "image/png",
		},
		{
			name:       "rejects .ico extension",
			filename:   "favicon.ico",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "rejects path traversal",
			filename:   "../admin.css",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "rejects unknown extension",
			filename:   "icon.webp",
			wantStatus: http.StatusNotFound,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/admin/static/icons/"+tc.filename, nil)
			req.SetPathValue("filename", tc.filename)
			w := httptest.NewRecorder()
			ServeIconFile(w, req)
			if w.Code != tc.wantStatus {
				t.Errorf("status: got %d, want %d", w.Code, tc.wantStatus)
			}
			if tc.wantCT != "" && w.Code == http.StatusOK {
				if ct := w.Header().Get("Content-Type"); ct != tc.wantCT {
					t.Errorf("Content-Type: got %q, want %q", ct, tc.wantCT)
				}
			}
			if tc.wantStatus == http.StatusOK && w.Body.Len() == 0 {
				t.Error("expected non-empty body")
			}
		})
	}
}

func TestServeFontFile(t *testing.T) {
	tests := []struct {
		name       string
		filename   string
		wantStatus int
		wantCT     string
		wantCC     string
		wantBody   bool
	}{
		{
			name:       "serves Inter-Regular.woff2",
			filename:   "Inter-Regular.woff2",
			wantStatus: http.StatusOK,
			wantCT:     "font/woff2",
			wantCC:     "public, max-age=31536000, immutable",
			wantBody:   true,
		},
		{
			name:       "serves JetBrainsMono-Regular.woff2",
			filename:   "JetBrainsMono-Regular.woff2",
			wantStatus: http.StatusOK,
			wantCT:     "font/woff2",
			wantCC:     "public, max-age=31536000, immutable",
			wantBody:   true,
		},
		{
			name:       "rejects non-woff2 extension",
			filename:   "evil.js",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "rejects path traversal attempt",
			filename:   "../admin.css",
			wantStatus: http.StatusNotFound,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/admin/static/fonts/"+tc.filename, nil)
			req.SetPathValue("filename", tc.filename)
			w := httptest.NewRecorder()
			ServeFontFile(w, req)
			if w.Code != tc.wantStatus {
				t.Errorf("status: got %d, want %d", w.Code, tc.wantStatus)
			}
			if tc.wantCT != "" {
				if ct := w.Header().Get("Content-Type"); ct != tc.wantCT {
					t.Errorf("Content-Type: got %q, want %q", ct, tc.wantCT)
				}
			}
			if tc.wantCC != "" {
				if cc := w.Header().Get("Cache-Control"); cc != tc.wantCC {
					t.Errorf("Cache-Control: got %q, want %q", cc, tc.wantCC)
				}
			}
			if tc.wantBody && w.Body.Len() == 0 {
				t.Error("expected non-empty body, got empty")
			}
		})
	}
}

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
