package admin

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// contextKey is an unexported typed key for values stored in request contexts.
// Using a typed key prevents collisions with keys from other packages.
type contextKey int

const (
	contextKeyAdminSub contextKey = iota
	contextKeyAdminEmail
)

// AdminSubFromContext returns the admin sub claim stored by SessionGuard, or "".
func AdminSubFromContext(ctx context.Context) string {
	v, _ := ctx.Value(contextKeyAdminSub).(string)
	return v
}

// AdminEmailFromContext returns the admin email stored by SessionGuard, or "".
func AdminEmailFromContext(ctx context.Context) string {
	v, _ := ctx.Value(contextKeyAdminEmail).(string)
	return v
}

// verifySessionCookie verifies the HMAC-SHA256 signature and decodes the cookie.
// This mirrors the AdminAuth.verifyCookie logic for use in middleware without an AdminAuth instance.
func verifySessionCookie(secret []byte, value string) ([]byte, error) {
	idx := strings.LastIndex(value, ".")
	if idx < 0 {
		return nil, errors.New("invalid cookie format")
	}
	encoded, sigPart := value[:idx], value[idx+1:]
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(encoded))
	expectedSig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expectedSig), []byte(sigPart)) {
		return nil, errors.New("invalid cookie signature")
	}
	return base64.RawURLEncoding.DecodeString(encoded)
}

// SessionGuard returns a middleware that validates the admin_session cookie.
// On missing, invalid, or expired cookie: redirects 302 to /admin/login.
// On valid cookie: stores sub and email in the request context for downstream handlers.
func SessionGuard(secret []byte) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie("admin_session")
			if err != nil {
				http.Redirect(w, r, "/admin/login", http.StatusFound)
				return
			}
			payload, err := verifySessionCookie(secret, cookie.Value)
			if err != nil {
				http.Redirect(w, r, "/admin/login", http.StatusFound)
				return
			}
			var sess adminSessionCookie
			if err := json.Unmarshal(payload, &sess); err != nil {
				slog.Warn("session guard: malformed session cookie JSON", "err", err)
				http.Redirect(w, r, "/admin/login", http.StatusFound)
				return
			}
			if sess.Exp <= time.Now().Unix() {
				http.Redirect(w, r, "/admin/login", http.StatusFound)
				return
			}
			ctx := context.WithValue(r.Context(), contextKeyAdminSub, sess.Sub)
			ctx = context.WithValue(ctx, contextKeyAdminEmail, sess.Email)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// BootstrapGuard returns a middleware that guards admin routes based on bootstrap state.
// - If bootstrap is active and the request path is NOT /admin/bootstrap*, redirect 302 to /admin/bootstrap.
// - If bootstrap is complete and the request path IS /admin/bootstrap*, redirect 302 to /admin/dashboard.
// - All other cases pass through to the next handler.
// - On DB error, return 500 Internal Server Error.
func BootstrapGuard(checker BootstrapStatusChecker) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			active, err := checker.IsBootstrapActive(r.Context())
			if err != nil {
				slog.Error("bootstrap guard: status check failed", "err", err)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}

			isBootstrapPath := strings.HasPrefix(r.URL.Path, "/admin/bootstrap")

			if active && !isBootstrapPath {
				// Not yet bootstrapped — send to wizard
				http.Redirect(w, r, "/admin/bootstrap", http.StatusFound)
				return
			}
			if !active && isBootstrapPath {
				// Already bootstrapped — redirect away from wizard
				http.Redirect(w, r, "/admin/dashboard", http.StatusFound)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
