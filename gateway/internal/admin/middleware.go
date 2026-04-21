package admin

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
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
	contextKeyCSRFToken
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

// SessionGuardWithStore returns a middleware that validates the admin_session cookie by
// looking up the SID in the provided AdminSessionStore (AC3, Story 5.12).
//
// On missing or invalid cookie → 302 to /admin/login.
// On SID not found in store → 302 to /admin/login.
// On revoked_at IS NOT NULL → 302 to /admin/login.
// On expires_at < NOW() → 302 to /admin/login.
// On valid session → stores user_id in request context (read from DB, not cookie).
func SessionGuardWithStore(secret []byte, store AdminSessionStore) func(http.Handler) http.Handler {
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
			var sidCookie adminSessionSIDCookie
			if err := json.Unmarshal(payload, &sidCookie); err != nil || sidCookie.SID == "" {
				slog.Warn("session guard (store): malformed SID cookie", "err", err)
				http.Redirect(w, r, "/admin/login", http.StatusFound)
				return
			}

			sess, err := store.Get(r.Context(), sidCookie.SID)
			if err != nil {
				slog.Warn("session guard (store): DB lookup error", "err", err)
				http.Redirect(w, r, "/admin/login", http.StatusFound)
				return
			}
			if sess == nil {
				// Row not found.
				http.Redirect(w, r, "/admin/login", http.StatusFound)
				return
			}
			if sess.RevokedAt != nil {
				// Session has been revoked.
				http.Redirect(w, r, "/admin/login", http.StatusFound)
				return
			}
			if !sess.ExpiresAt.After(time.Now()) {
				// Session has expired in the DB.
				http.Redirect(w, r, "/admin/login", http.StatusFound)
				return
			}

			// Valid session: store user_id from DB into context.
			ctx := context.WithValue(r.Context(), contextKeyAdminSub, sess.UserID)
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

// SecurityHeadersMiddleware sets standard security headers on every response to
// mitigate clickjacking, MIME-sniffing, and XSS. HSTS is only added when the
// connection is HTTPS (r.TLS != nil) or when X-Forwarded-Proto: https is set
// by a terminating reverse proxy.
func SecurityHeadersMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'; object-src 'none'")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Referrer-Policy", "no-referrer")
			if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
				w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// generateCSRFToken returns a cryptographically random 32-byte base64url-encoded token.
func generateCSRFToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// CSRFTokenFromContext returns the CSRF token stored in the request context by CSRFMiddleware.
// Returns an empty string if no token is present.
func CSRFTokenFromContext(ctx context.Context) string {
	v, _ := ctx.Value(contextKeyCSRFToken).(string)
	return v
}

// CSRFMiddleware implements double-submit-cookie CSRF protection.
//
// On GET requests:
//   - If the path is /admin/callback (login callback), always rotate: issue a fresh csrf_token cookie.
//   - Otherwise, if no csrf_token cookie exists, issue one.
//   - Inject the token into the request context so templates can embed it via CSRFTokenFromContext.
//
// On POST/PUT/DELETE requests:
//   - Read csrf_token cookie value and _csrf form field.
//   - Reject with 403 Forbidden if either is missing or they do not match
//     (comparison is constant-time via subtle.ConstantTimeCompare to prevent timing oracles).
//   - Pass through to next handler on match.
func CSRFMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				// Safe methods: issue / rotate cookie, inject into context.
				var token string

				forceRotate := r.URL.Path == "/admin/callback"

				if !forceRotate {
					// Re-use existing cookie if present.
					if c, err := r.Cookie("csrf_token"); err == nil && c.Value != "" {
						token = c.Value
					}
				}

				if token == "" {
					var err error
					token, err = generateCSRFToken()
					if err != nil {
						http.Error(w, "Internal Server Error", http.StatusInternalServerError)
						return
					}
					http.SetCookie(w, &http.Cookie{
						Name:     "csrf_token",
						Value:    token,
						Path:     "/admin",
						HttpOnly: false, // must be JS-readable for SPA scenarios
						SameSite: http.SameSiteStrictMode,
						Secure:   false, // Story 5.15 will enable HTTPS/Secure
					})
				}

				ctx := context.WithValue(r.Context(), contextKeyCSRFToken, token)
				next.ServeHTTP(w, r.WithContext(ctx))

			default:
				// State-changing methods: verify double-submit.
				cookieVal := ""
				if c, err := r.Cookie("csrf_token"); err == nil {
					cookieVal = c.Value
				}

				if err := r.ParseForm(); err != nil {
					http.Error(w, "Bad Request", http.StatusBadRequest)
					return
				}
				formVal := r.FormValue("_csrf")

				if cookieVal == "" || formVal == "" {
					http.Error(w, "Forbidden", http.StatusForbidden)
					return
				}

				if subtle.ConstantTimeCompare([]byte(cookieVal), []byte(formVal)) != 1 {
					http.Error(w, "Forbidden", http.StatusForbidden)
					return
				}

				ctx := context.WithValue(r.Context(), contextKeyCSRFToken, cookieVal)
				next.ServeHTTP(w, r.WithContext(ctx))
			}
		})
	}
}
