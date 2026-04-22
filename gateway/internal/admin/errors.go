package admin

import (
	"crypto/rand"
	"encoding/base32"
	"log/slog"
	"net/http"
)

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
	h.render(w, "500", ErrorPageData{})
}

// renderErrorWithID renders the admin error page with a per-request ID.
// It logs the full error (including the request ID and underlying err) so that
// operators can correlate a user-reported reference ID with a log entry.
// The client receives only a generic HTML error page — the raw err string is
// never written to the response body.
//
// A non-nil TemplateHandler renders the error/500 page; if h is nil a plain-text
// fallback is used to avoid a panic when templates are unavailable.
func renderErrorWithID(w http.ResponseWriter, r *http.Request, status int, title, detail string, err error, h *TemplateHandler) {
	// Generate a 10-character base32 request ID (80 bits of entropy, URL-safe).
	var raw [10]byte
	if _, randErr := rand.Read(raw[:]); randErr != nil {
		// Extremely unlikely; fall back to a fixed sentinel so the response still
		// includes *some* ID rather than being silently absent.
		copy(raw[:], []byte("XXXXXXXX00"))
	}
	// base32 without padding: 10 bytes → 16 characters.
	reqID := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw[:])

	slog.Error(title, "request_id", reqID, "detail", detail, "err", err)

	w.Header().Set("X-Request-ID", reqID)

	if h != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		h.render(w, "500", ErrorPageData{RequestID: reqID})
		return
	}

	// Fallback: plain-text response when no template handler is available.
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte("Internal Server Error. Reference ID: " + reqID + "\n"))
}
