package admin

import (
	"log/slog"
	"net/http"
	"strings"
)

// BootstrapGuard returns a middleware that guards admin routes based on bootstrap state.
// - If bootstrap is active and the request path is NOT /admin/bootstrap*, redirect 302 to /admin/bootstrap.
// - If bootstrap is complete and the request path IS /admin/bootstrap*, redirect 302 to /admin/login.
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
				http.Redirect(w, r, "/admin/login", http.StatusFound)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
