package admin

import (
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// TestBaseLayoutDataTheme verifies that the base layout template renders with
// data-theme="obsidian" on the <html> element (AC: 2, 5).
func TestBaseLayoutDataTheme(t *testing.T) {
	h, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}

	w := httptest.NewRecorder()
	// Render using BootstrapMode=true so the template renders the bootstrap nav
	// without needing a session cookie. The base layout with data-theme is
	// rendered regardless of bootstrap state.
	h.render(w, "base", PageData{BootstrapMode: true})

	body := w.Body.String()
	if !strings.Contains(body, `data-theme="obsidian"`) {
		t.Errorf("expected data-theme=\"obsidian\" in rendered HTML, got: %s", body[:min(200, len(body))])
	}
}

// TestTailwindConfigFontSizeExtensions verifies that tailwind.config.js contains
// the full fontSize extension block with all five named sizes (AC: 3).
func TestTailwindConfigFontSizeExtensions(t *testing.T) {
	data, err := os.ReadFile("tailwind.config.js")
	if err != nil {
		t.Fatalf("cannot read tailwind.config.js: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "fontSize") {
		t.Error("tailwind.config.js missing 'fontSize' extension block")
	}

	for _, key := range []string{`'display'`, `'heading'`, `'body'`, `'caption'`, `'mono'`} {
		if !strings.Contains(content, key) {
			t.Errorf("tailwind.config.js missing fontSize key %s", key)
		}
	}
}

// TestTailwindConfigColorTokens verifies that tailwind.config.js contains all
// required UX-DR1 color tokens including the four-tier background scale (AC: 1).
func TestTailwindConfigColorTokens(t *testing.T) {
	data, err := os.ReadFile("tailwind.config.js")
	if err != nil {
		t.Fatalf("cannot read tailwind.config.js: %v", err)
	}
	content := string(data)

	// Four-tier background scale from UX-DR1 (base-100..base-300 via DaisyUI obsidian
	// theme block; base-400 is an explicit extension in theme.extend.colors).
	for _, token := range []string{`"base-100"`, `"base-200"`, `"base-300"`, `"base-400"`} {
		if !strings.Contains(content, token) {
			t.Errorf("tailwind.config.js missing UX-DR1 color token %s", token)
		}
	}
}

// TestAdminCSSContainsObsidianTokens verifies that the compiled CSS artifact
// contains the :root block and the DaisyUI primary token (AC: 4).
func TestAdminCSSContainsObsidianTokens(t *testing.T) {
	data, err := os.ReadFile("static/admin.css")
	if err != nil {
		t.Fatalf("cannot read static/admin.css: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, ":root") {
		t.Error("static/admin.css missing ':root' CSS block — run 'make build-admin-css' to regenerate")
	}
	if !strings.Contains(content, "--p:") {
		t.Error("static/admin.css missing DaisyUI primary token '--p:' — Obsidian theme may not have been compiled")
	}
}
