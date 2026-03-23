package middleware

import (
	"crypto/subtle"
	"net/http"
)

// PSKMiddleware returns an HTTP middleware that validates a pre-shared key
// from the Authorization header (Bearer scheme). Uses constant-time comparison
// to prevent timing attacks. Returns 401 with empty body on mismatch.
func PSKMiddleware(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			expected := "Bearer " + secret
			if subtle.ConstantTimeCompare([]byte(authHeader), []byte(expected)) != 1 {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
