package admin

import (
	"embed"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
)

//go:embed templates
var adminFS embed.FS

// TemplateHandler serves Admin UI HTML pages via embedded Go templates.
// Named TemplateHandler to avoid conflict with the oapi-codegen generated Handler function.
type TemplateHandler struct {
	tmpl *template.Template
}

// NewTemplateHandler parses all templates from the embedded FS recursively.
// Uses fs.WalkDir to collect all .html files under templates/ so that
// subdirectories (e.g. templates/layouts/) are included automatically.
// Returns error if parsing fails.
func NewTemplateHandler() (*TemplateHandler, error) {
	var patterns []string
	err := fs.WalkDir(adminFS, "templates", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && path.Ext(p) == ".html" {
			patterns = append(patterns, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	tmpl, err := template.ParseFS(adminFS, patterns...)
	if err != nil {
		return nil, err
	}
	return &TemplateHandler{tmpl: tmpl}, nil
}

// render executes the named template into w.
// Sets Content-Type: text/html; charset=utf-8.
// On execution error: writes 500, logs the error, does NOT panic.
func (h *TemplateHandler) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.tmpl.ExecuteTemplate(w, name, data); err != nil {
		slog.Error("template execution failed", "name", name, "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}
