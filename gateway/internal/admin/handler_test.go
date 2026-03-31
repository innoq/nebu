package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTemplateHandler_render(t *testing.T) {
	h, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	tests := []struct {
		name     string
		tmplName string
		wantCode int
		wantBody bool // true = expect non-empty body
	}{
		{"valid template", "base", http.StatusOK, true},
		{"unknown template", "nonexistent", http.StatusInternalServerError, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.render(w, tc.tmplName, nil)
			if w.Code != tc.wantCode {
				t.Errorf("status: got %d, want %d", w.Code, tc.wantCode)
			}
			if tc.wantBody && w.Body.Len() == 0 {
				t.Error("expected non-empty body, got empty")
			}
			if tc.wantCode == http.StatusOK {
				ct := w.Header().Get("Content-Type")
				if ct != "text/html; charset=utf-8" {
					t.Errorf("Content-Type: got %q, want %q", ct, "text/html; charset=utf-8")
				}
			}
		})
	}
}

func TestBaseLayout(t *testing.T) {
	h, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}

	t.Run("layout contains landmarks without bootstrap mode", func(t *testing.T) {
		w := httptest.NewRecorder()
		h.render(w, "base", PageData{BootstrapMode: false})
		if w.Code != http.StatusOK {
			t.Fatalf("status: got %d, want %d", w.Code, http.StatusOK)
		}
		body := w.Body.String()
		for _, want := range []string{"<header", "<nav", "<main"} {
			if !strings.Contains(body, want) {
				t.Errorf("expected body to contain %q", want)
			}
		}
	})

	t.Run("bootstrap nav item shown when BootstrapMode=true", func(t *testing.T) {
		w := httptest.NewRecorder()
		h.render(w, "base", PageData{BootstrapMode: true})
		if w.Code != http.StatusOK {
			t.Fatalf("status: got %d, want %d", w.Code, http.StatusOK)
		}
		body := w.Body.String()
		if !strings.Contains(body, `data-navkey="bootstrap"`) {
			t.Error("expected body to contain data-navkey=\"bootstrap\" when BootstrapMode=true")
		}
	})

	t.Run("bootstrap nav item hidden when BootstrapMode=false", func(t *testing.T) {
		w := httptest.NewRecorder()
		h.render(w, "base", PageData{BootstrapMode: false})
		if w.Code != http.StatusOK {
			t.Fatalf("status: got %d, want %d", w.Code, http.StatusOK)
		}
		body := w.Body.String()
		if strings.Contains(body, `data-navkey="bootstrap"`) {
			t.Error("expected body NOT to contain data-navkey=\"bootstrap\" when BootstrapMode=false")
		}
	})
}
