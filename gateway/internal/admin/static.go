package admin

import (
	"embed"
	"net/http"
	"path"
	"strings"
)

//go:embed static/admin.css static/fonts static/vendor
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

// ServeFontFile serves embedded WOFF2 font files from static/fonts/.
// Route: GET /admin/static/fonts/{filename}
func ServeFontFile(w http.ResponseWriter, r *http.Request) {
	filename := path.Base(r.PathValue("filename")) // path.Base prevents directory traversal
	if !strings.HasSuffix(filename, ".woff2") {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	data, err := staticFS.ReadFile("static/fonts/" + filename)
	if err != nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "font/woff2")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.WriteHeader(http.StatusOK)
	w.Write(data) //nolint:errcheck
}

// ServeVendorFile serves embedded vendor JS files from static/vendor/.
// Route: GET /admin/static/vendor/{filename}
func ServeVendorFile(w http.ResponseWriter, r *http.Request) {
	filename := path.Base(r.PathValue("filename")) // path.Base prevents directory traversal
	if !strings.HasSuffix(filename, ".js") {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	data, err := staticFS.ReadFile("static/vendor/" + filename)
	if err != nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/javascript")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.WriteHeader(http.StatusOK)
	w.Write(data) //nolint:errcheck
}

// ServeMetricsWidgetJS serves the Vue metrics widget script from the embedded templates FS.
// Route: GET /admin/static/metrics-widget.js
func ServeMetricsWidgetJS(w http.ResponseWriter, r *http.Request) {
	data, err := adminFS.ReadFile("templates/metrics-widget.js")
	if err != nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/javascript")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	w.Write(data) //nolint:errcheck
}
