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
	coregrpc "github.com/nebu/nebu/internal/grpc"
	"github.com/prometheus/client_golang/prometheus"
)

// jwtValidationTotal counts JWT validation outcomes by pipeline stage and result.
//   stage="verify"   — OIDC signature / expiry / audience check
//   stage="denylist" — explicit logout check (only reached after verify passes)
//   result="pass"    — check succeeded
//   result="fail"    — check failed, request rejected 401
var jwtValidationTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "nebu_jwt_validation_total",
		Help: "Total JWT validation outcomes by pipeline stage and result",
	},
	[]string{"stage", "result"},
)

func init() {
	prometheus.MustRegister(jwtValidationTotal)
}

type contextKey string

const (
	ContextKeySub               contextKey = "sub"
	ContextKeyPreferredUsername contextKey = "preferred_username"
	ContextKeyEmail             contextKey = "email"
	ContextKeyNebuRole          contextKey = "nebu_role"
	ContextKeySystemRole        contextKey = "system_role"
	ContextKeyTokenExpiry       contextKey = "token_expiry"
	// ContextKeyUserID holds the pre-computed Matrix user ID (@localpart:server).
	// Handlers should read this instead of calling FormatUserID themselves.
	ContextKeyUserID contextKey = "user_id"
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
// sub, preferred_username, email, the raw role claim value (ContextKeyNebuRole),
// the mapped canonical system role (ContextKeySystemRole), and token expiry
// (ContextKeyTokenExpiry). Pass nil for store to disable invalidation checking.
// JWTMiddleware validates OIDC JWT bearer tokens.
// serverName is the Matrix server name (used to pre-compute ContextKeyUserID).
func JWTMiddleware(provider *auth.Provider, clientID string, claimName string, store TokenStore, serverName ...string) func(http.Handler) http.Handler {
	srv := ""
	if len(serverName) > 0 {
		srv = serverName[0]
	}
	// Parse algorithm whitelist once at middleware construction, not per request.
	algs := ParseSupportedAlgs()
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
				writeMatrixError(w, http.StatusUnauthorized, "M_MISSING_TOKEN", "Missing access token")
				return
			}
			rawToken := strings.TrimPrefix(authHeader, "Bearer ")

			// Step 1: Verify signature, expiry, audience — reject before any DB access.
			inner := provider.Inner()
			if inner == nil {
				writeMatrixError(w, http.StatusUnauthorized, "M_UNKNOWN_TOKEN", "OIDC provider unavailable")
				return
			}

			verifier := inner.Verifier(&oidc.Config{
				ClientID:             clientID,
				SupportedSigningAlgs: algs,
			})
			idToken, err := verifier.Verify(r.Context(), rawToken)
			if err != nil {
				jwtValidationTotal.WithLabelValues("verify", "fail").Inc()
				var expiredErr *oidc.TokenExpiredError
				if errors.As(err, &expiredErr) {
					writeMatrixError(w, http.StatusUnauthorized, "M_UNKNOWN_TOKEN", "Token has expired")
				} else {
					writeMatrixError(w, http.StatusUnauthorized, "M_UNKNOWN_TOKEN", "Invalid token")
				}
				return
			}
			jwtValidationTotal.WithLabelValues("verify", "pass").Inc()

			// Step 2: Denylist check — only for cryptographically verified tokens.
			// This prevents DB flooding with random unsigned strings.
			if store != nil && store.IsInvalidated(rawToken) {
				jwtValidationTotal.WithLabelValues("denylist", "fail").Inc()
				writeMatrixError(w, http.StatusUnauthorized, "M_UNKNOWN_TOKEN", "Token has been logged out")
				return
			}
			if store != nil {
				jwtValidationTotal.WithLabelValues("denylist", "pass").Inc()
			}

			var allClaims map[string]interface{}
			if err := idToken.Claims(&allClaims); err != nil {
				log.Printf("failed to extract JWT claims: %v", err)
				writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
				return
			}

			sub, _ := allClaims["sub"].(string)
			preferredUsername, _ := allClaims["preferred_username"].(string)
			name, _ := allClaims["name"].(string)
			email, _ := allClaims["email"].(string)
			rawRole := auth.ExtractRoleClaim(allClaims, claimName)
			systemRole := auth.MapSystemRole(rawRole)

			// Pre-compute Matrix user ID using name claim if available.
			// This is the canonical user ID for all downstream handlers.
			userID := coregrpc.FormatUserIDFromClaims(sub, name, srv)

			ctx := r.Context()
			ctx = context.WithValue(ctx, ContextKeySub, sub)
			ctx = context.WithValue(ctx, ContextKeyPreferredUsername, preferredUsername)
			ctx = context.WithValue(ctx, ContextKeyEmail, email)
			ctx = context.WithValue(ctx, ContextKeyNebuRole, rawRole)
			ctx = context.WithValue(ctx, ContextKeySystemRole, systemRole)
			ctx = context.WithValue(ctx, ContextKeyTokenExpiry, idToken.Expiry)
			ctx = context.WithValue(ctx, ContextKeyUserID, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
