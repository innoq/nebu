package admin

import (
	"embed"
	"net/http"
)

//go:embed static/admin.css
var staticFS embed.FS

// ServeCSS serves the embedded admin.css with long-lived caching headers.
func ServeCSS(w http.ResponseWriter, r *http.Request) {
	data, err := staticFS.ReadFile("static/admin.css")
	if err != nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/css")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.WriteHeader(http.StatusOK)
	w.Write(data) //nolint:errcheck
}
