package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/nebu/nebu/internal/auth"
)

type contextKey string

const (
	ContextKeySub               contextKey = "sub"
	ContextKeyPreferredUsername contextKey = "preferred_username"
	ContextKeyEmail             contextKey = "email"
	ContextKeyNebuRole          contextKey = "nebu_role"
)

type matrixError struct {
	ErrCode string `json:"errcode"`
	Err     string `json:"error"`
}

func writeMatrixError(w http.ResponseWriter, status int, errcode, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(matrixError{ErrCode: errcode, Err: message})
}

// JWTMiddleware validates OIDC JWT tokens. On success, populates context with
// sub, preferred_username, email, nebu_role claims for downstream handlers.
func JWTMiddleware(provider *auth.Provider, clientID string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
				writeMatrixError(w, http.StatusUnauthorized, "M_MISSING_TOKEN", "Missing access token")
				return
			}
			rawToken := strings.TrimPrefix(authHeader, "Bearer ")

			inner := provider.Inner()
			if inner == nil {
				writeMatrixError(w, http.StatusUnauthorized, "M_UNKNOWN_TOKEN", "OIDC provider unavailable")
				return
			}

			verifier := inner.Verifier(&oidc.Config{ClientID: clientID})
			idToken, err := verifier.Verify(r.Context(), rawToken)
			if err != nil {
				var expiredErr *oidc.TokenExpiredError
				if errors.As(err, &expiredErr) {
					writeMatrixError(w, http.StatusUnauthorized, "M_UNKNOWN_TOKEN", "Token has expired")
				} else {
					writeMatrixError(w, http.StatusUnauthorized, "M_UNKNOWN_TOKEN", "Invalid token")
				}
				return
			}

			var claims struct {
				Sub               string `json:"sub"`
				PreferredUsername string `json:"preferred_username"`
				Email             string `json:"email"`
				NebuRole          string `json:"nebu_role"`
			}
			if err := idToken.Claims(&claims); err != nil {
				log.Printf("failed to extract JWT claims: %v", err)
				writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
				return
			}

			ctx := r.Context()
			ctx = context.WithValue(ctx, ContextKeySub, claims.Sub)
			ctx = context.WithValue(ctx, ContextKeyPreferredUsername, claims.PreferredUsername)
			ctx = context.WithValue(ctx, ContextKeyEmail, claims.Email)
			ctx = context.WithValue(ctx, ContextKeyNebuRole, claims.NebuRole)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
