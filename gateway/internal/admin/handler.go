package admin

import (
	"embed"
	"html/template"
	"log/slog"
	"net/http"
)

//go:embed templates
var adminFS embed.FS

// TemplateHandler serves Admin UI HTML pages via embedded Go templates.
// Named TemplateHandler to avoid conflict with the oapi-codegen generated Handler function.
type TemplateHandler struct {
	tmpl *template.Template
}

// NewTemplateHandler parses all templates from the embedded FS. Returns error if parsing fails.
func NewTemplateHandler() (*TemplateHandler, error) {
	tmpl, err := template.ParseFS(adminFS, "templates/*.html")
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
