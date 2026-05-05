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
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	coregrpc "github.com/nebu/nebu/internal/grpc"
	pb "github.com/nebu/nebu/internal/grpc/pb"
	"github.com/nebu/nebu/internal/audit"
	"golang.org/x/oauth2"
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

// sessionRefreshWindow is the pre-expiry window within which a silent refresh is attempted.
// Sessions expiring within this window are proactively refreshed to avoid mid-request interruptions.
const sessionRefreshWindow = 5 * time.Minute

// sessionRefreshGrace is the grace window after expiry during which a refresh can still be attempted.
// Requests arriving up to 30 seconds after session expiry may still be silently renewed.
const sessionRefreshGrace = 30 * time.Second

// SessionRefreshConfig holds dependencies for the SessionGuardWithRefresh middleware.
type SessionRefreshConfig struct {
	Secret       []byte
	Store        AdminSessionStore
	ConfigReader ServerConfigReader
	CoreClient   pb.CoreServiceClient // optional; nil disables audit logging
}

// SessionGuardWithRefresh is like SessionGuardWithStore but also attempts a silent
// OIDC token refresh when the session is about to expire (within sessionRefreshWindow)
// or just expired (within sessionRefreshGrace).
//
// On successful refresh: UpdateExpiry is called, the cookie MaxAge is slid, an audit
// event is emitted (if CoreClient is set), and the request continues.
// On failed refresh: the session is revoked, the cookie is cleared, and the user is
// redirected to /admin/login.
// When no EncryptedRefreshToken is stored: falls back to standard SessionGuardWithStore behavior.
func SessionGuardWithRefresh(cfg SessionRefreshConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie("admin_session")
			if err != nil {
				http.Redirect(w, r, "/admin/login", http.StatusFound)
				return
			}
			payload, err := verifySessionCookie(cfg.Secret, cookie.Value)
			if err != nil {
				http.Redirect(w, r, "/admin/login", http.StatusFound)
				return
			}
			var sidCookie adminSessionSIDCookie
			if err := json.Unmarshal(payload, &sidCookie); err != nil || sidCookie.SID == "" {
				slog.Warn("session guard (refresh): malformed SID cookie", "err", err)
				http.Redirect(w, r, "/admin/login", http.StatusFound)
				return
			}

			sess, err := cfg.Store.Get(r.Context(), sidCookie.SID)
			if err != nil {
				slog.Warn("session guard (refresh): DB lookup error", "err", err)
				http.Redirect(w, r, "/admin/login", http.StatusFound)
				return
			}
			if sess == nil {
				http.Redirect(w, r, "/admin/login", http.StatusFound)
				return
			}
			if sess.RevokedAt != nil {
				http.Redirect(w, r, "/admin/login", http.StatusFound)
				return
			}

			now := time.Now()
			withinRefreshWindow := sess.ExpiresAt.Before(now.Add(sessionRefreshWindow))
			withinGrace := !sess.ExpiresAt.After(now) && now.Before(sess.ExpiresAt.Add(sessionRefreshGrace))
			needsRefresh := (withinRefreshWindow || withinGrace) && sess.EncryptedRefreshToken != ""

			if needsRefresh {
				// Attempt silent token refresh.
				newExpiry, newEncRT, refreshErr := attemptTokenRefresh(r.Context(), cfg, sess)
				if refreshErr != nil {
					slog.Warn("session guard (refresh): token refresh failed — revoking session", "err", refreshErr)
					if revokeErr := cfg.Store.Revoke(r.Context(), sidCookie.SID); revokeErr != nil {
						slog.Warn("session guard (refresh): failed to revoke session after refresh failure", "err", revokeErr)
					}
					http.SetCookie(w, &http.Cookie{
						Name:     "admin_session",
						Value:    "",
						Path:     "/admin",
						MaxAge:   -1,
						HttpOnly: true,
						Secure:   isRequestSecure(r),
						SameSite: http.SameSiteLaxMode,
					})
					http.Redirect(w, r, "/admin/login", http.StatusFound)
					return
				}

				if updateErr := cfg.Store.UpdateExpiry(r.Context(), sidCookie.SID, newExpiry, newEncRT); updateErr != nil {
					slog.Warn("session guard (refresh): failed to persist new expiry", "err", updateErr)
					// Non-fatal: continue the request with the old expiry; next request will retry.
				}

				// Slide the cookie MaxAge to match the new expiry.
				newMaxAge := int(time.Until(newExpiry).Seconds())
				if newMaxAge <= 0 {
					newMaxAge = 1
				}
				http.SetCookie(w, &http.Cookie{
					Name:     "admin_session",
					Value:    cookie.Value, // same SID, MaxAge extended
					Path:     "/admin",
					MaxAge:   newMaxAge,
					HttpOnly: true,
					Secure:   isRequestSecure(r),
					SameSite: http.SameSiteLaxMode,
				})

				// Emit audit event for session refresh (AC9). Non-blocking on failure.
				if cfg.CoreClient != nil {
					logCtx := r.Context()
					_ = audit.LogEvent(logCtx, cfg.CoreClient, sess.UserID, "admin_session_refreshed", "session", sidCookie.SID,
						map[string]any{"expires_at": newExpiry.Format(time.RFC3339)}, "success", "")
				}

				ctx := context.WithValue(r.Context(), contextKeyAdminSub, sess.UserID)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// Session expired and no refresh token available (or already past grace window).
			if !sess.ExpiresAt.After(now) {
				http.Redirect(w, r, "/admin/login", http.StatusFound)
				return
			}

			// Valid session: store user_id from DB into context.
			ctx := context.WithValue(r.Context(), contextKeyAdminSub, sess.UserID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// attemptTokenRefresh performs the OIDC token refresh and returns the new expiry and
// encrypted refresh token. Returns an error if the token endpoint rejects the request.
func attemptTokenRefresh(ctx context.Context, cfg SessionRefreshConfig, sess *AdminSession) (newExpiry time.Time, newEncRT string, err error) {
	if cfg.ConfigReader == nil {
		return time.Time{}, "", fmt.Errorf("session refresh: no config reader available")
	}

	issuer, clientID, clientSecret, err := cfg.ConfigReader.LoadOIDCConfig(ctx)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("session refresh: failed to load OIDC config: %w", err)
	}

	// Decrypt the stored refresh token.
	plainRT, err := decryptAES256GCM(cfg.Secret, sess.EncryptedRefreshToken)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("session refresh: failed to decrypt refresh token: %w", err)
	}

	// Discover OIDC provider for the endpoint.
	// oidc.ClientContext(ctx, nil) is treated as "no override" by the OIDC
	// library — its internal doRequest falls back to http.DefaultClient when
	// the wrapped client value is nil. The same context is NOT reused for the
	// oauth2 token-source call below, because golang.org/x/oauth2 returns the
	// nil client as-is instead of falling back to DefaultClient.
	oidcCtx := oidc.ClientContext(ctx, nil)
	rawProvider, err := globalProviderCache.load(oidcCtx, issuer)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("session refresh: OIDC provider discovery failed: %w", err)
	}
	provider, ok := rawProvider.(*oidc.Provider)
	if !ok {
		return time.Time{}, "", fmt.Errorf("session refresh: unexpected OIDC provider type")
	}

	oauth2Cfg := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email", "groups", "offline_access"},
	}

	existingToken := &oauth2.Token{RefreshToken: plainRT}
	ts := oauth2Cfg.TokenSource(ctx, existingToken)
	newToken, err := ts.Token()
	if err != nil {
		return time.Time{}, "", fmt.Errorf("session refresh: token endpoint error: %w", err)
	}

	// Compute new session expiry: min(token.Expiry, now+8h).
	const maxSessionDuration = 8 * time.Hour
	newExpiry = time.Now().Add(maxSessionDuration)
	if !newToken.Expiry.IsZero() && newToken.Expiry.Before(newExpiry) {
		newExpiry = newToken.Expiry
	}

	// Token rotation: always use the latest refresh token returned by Dex.
	// If Dex did not return a new refresh token (single-use rotation disabled), keep the old one.
	rtToStore := newToken.RefreshToken
	if rtToStore == "" {
		rtToStore = plainRT // keep old token if Dex didn't rotate
	}

	newEncRT, err = encryptAES256GCM(cfg.Secret, rtToStore)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("session refresh: failed to encrypt new refresh token: %w", err)
	}

	return newExpiry, newEncRT, nil
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
// request is considered secure (direct TLS or trusted proxy with X-Forwarded-Proto: https).
//
// oidcIssuerOrigin (e.g. "https://auth.example.com" or "http://dex:5556") is
// appended to the form-action directive so the browser permits the bootstrap
// step-2 form submission to redirect cross-origin to the OIDC provider. With
// form-action 'self' alone the browser silently blocks the OIDC redirect — the
// form POST returns 303 → 302 chain whose final hop is the issuer origin, and
// CSP form-action is enforced on the entire redirect chain.
func SecurityHeadersMiddleware(oidcIssuerOrigin string) func(http.Handler) http.Handler {
	formAction := "'self'"
	if oidcIssuerOrigin != "" {
		formAction = "'self' " + oidcIssuerOrigin
	}
	csp := "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action " + formAction + "; object-src 'none'"
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Security-Policy", csp)
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Referrer-Policy", "no-referrer")
			if isRequestSecure(r) {
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
						Secure:   isRequestSecure(r),
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

// contextWithAdminIdentity enriches ctx with the admin user's identity as
// outgoing gRPC metadata so that Core audit handlers can attribute actions to
// the acting admin.
//
// The metadata key "x-user-id" is read by Nebu.Grpc.Metadata.user_id/1 in the
// Elixir core. The system role is always "instance_admin" for admin UI actions
// (all users who reach these handlers have been verified as instance_admin by
// the SessionGuard middleware).
//
// Usage: replace r.Context() with contextWithAdminIdentity(r.Context(), adminUserID)
// in every state-changing admin gRPC call (ArchiveRoom, UnarchiveRoom,
// DeactivateUser, ReactivateUser, UpdateUserRole, UpdateRoomSettings).
func contextWithAdminIdentity(ctx context.Context, adminUserID string) context.Context {
	return coregrpc.WithUserMetadata(ctx, adminUserID, "instance_admin")
}
