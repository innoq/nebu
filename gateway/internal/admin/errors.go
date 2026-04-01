package admin

import "net/http"

// Error401 writes HTTP 401 Unauthorized and renders the "Authentication Required" error page.
func Error401(w http.ResponseWriter, r *http.Request, h *TemplateHandler) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	h.render(w, "401", PageData{})
}

// Error403 writes HTTP 403 Forbidden and renders the "Access Denied" error page.
func Error403(w http.ResponseWriter, r *http.Request, h *TemplateHandler) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusForbidden)
	h.render(w, "403", PageData{})
}

// Error404 writes HTTP 404 Not Found and renders the "Page Not Found" error page.
func Error404(w http.ResponseWriter, r *http.Request, h *TemplateHandler) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	h.render(w, "404", PageData{})
}

// Error500 writes HTTP 500 Internal Server Error and renders the "Internal Server Error" error page.
// Callers MUST log the underlying error before calling this function — no error details are
// ever exposed to the client.
func Error500(w http.ResponseWriter, r *http.Request, h *TemplateHandler) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusInternalServerError)
	h.render(w, "500", PageData{})
}
