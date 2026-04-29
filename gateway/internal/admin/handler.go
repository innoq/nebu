package admin

import (
	"embed"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"
)

//go:embed templates
var adminFS embed.FS

// TemplateHandler serves Admin UI HTML pages via embedded Go templates.
// Named TemplateHandler to avoid conflict with the oapi-codegen generated Handler function.
// Each page template is compiled into its own isolated template set together with the
// shared layout templates, so that page-specific data fields (e.g. BootstrapPageData.Step)
// do not interfere with other pages that use the base PageData struct.
type TemplateHandler struct {
	// pageTmpls maps a page template name (the base filename without extension, e.g. "bootstrap")
	// to its own compiled template.Template set (layout + that single page template).
	pageTmpls map[string]*template.Template
	// baseTmpl is the layout-only template set, used when rendering "base" directly.
	baseTmpl *template.Template
}

// NewTemplateHandler parses all templates from the embedded FS.
// For each page template (non-layout .html file) it creates an isolated template.Template
// set containing the shared layout templates plus that page template.
// Returns error if parsing fails.
func NewTemplateHandler() (*TemplateHandler, error) {
	// Collect layout templates (templates/layouts/*.html)
	var layoutPatterns []string
	err := fs.WalkDir(adminFS, "templates/layouts", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && path.Ext(p) == ".html" {
			layoutPatterns = append(layoutPatterns, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Parse base-only template set
	baseTmpl, err := template.ParseFS(adminFS, layoutPatterns...)
	if err != nil {
		return nil, err
	}

	// Collect page templates and component partials separately.
	// Components (templates/components/*.html) are included in every page's template set
	// so that {{ template "master_detail" . }} and {{ template "detail_panel" . }} resolve
	// correctly in any page template. Page files are all other non-layout .html files.
	var pageFiles []string
	var componentFiles []string
	err = fs.WalkDir(adminFS, "templates", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && path.Ext(p) == ".html" && !strings.HasPrefix(p, "templates/layouts/") {
			if strings.HasPrefix(p, "templates/components/") {
				componentFiles = append(componentFiles, p)
			} else {
				pageFiles = append(pageFiles, p)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// For each page file, create an isolated template set: layouts + components + that single page.
	// Components are shared partials (e.g. master_detail, detail_panel) required by multiple pages.
	pageTmpls := make(map[string]*template.Template, len(pageFiles))
	for _, pageFile := range pageFiles {
		// layouts + all components + this page file
		patterns := make([]string, 0, len(layoutPatterns)+len(componentFiles)+1)
		patterns = append(patterns, layoutPatterns...)
		patterns = append(patterns, componentFiles...)
		patterns = append(patterns, pageFile)
		tmpl, err := template.ParseFS(adminFS, patterns...)
		if err != nil {
			return nil, err
		}
		// Key: base filename without extension (e.g. "bootstrap" from "templates/bootstrap.html")
		base := path.Base(pageFile)
		name := strings.TrimSuffix(base, path.Ext(base))
		pageTmpls[name] = tmpl
	}

	return &TemplateHandler{pageTmpls: pageTmpls, baseTmpl: baseTmpl}, nil
}

// render executes the named template into w.
// If name is "base", the layout-only template set is used.
// Otherwise, the page-specific isolated template set is used (which also contains "base").
// Sets Content-Type: text/html; charset=utf-8.
// On execution error: writes 500, logs the error, does NOT panic.
func (h *TemplateHandler) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	var tmpl *template.Template
	if name == "base" {
		tmpl = h.baseTmpl
	} else if t, ok := h.pageTmpls[name]; ok {
		// Use the page-specific template set; render via "base" which includes the page's content block
		tmpl = t
		name = "base"
	} else {
		slog.Error("template not found", "name", name)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if err := tmpl.ExecuteTemplate(w, name, data); err != nil {
		slog.Error("template execution failed", "name", name, "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}
