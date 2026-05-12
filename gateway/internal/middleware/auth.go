package middleware

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/nebu/nebu/internal/auth"
	coregrpc "github.com/nebu/nebu/internal/grpc"
	"github.com/prometheus/client_golang/prometheus"
)

// UserStatusChecker checks if a user account is active.
// A nil implementation means the check is disabled (backward compat, tests).
type UserStatusChecker interface {
	IsUserActive(ctx context.Context, userID string) (bool, error)
}

// DBUserStatusChecker is the production implementation of UserStatusChecker.
// It queries the users table for is_active on each cache miss.
type DBUserStatusChecker struct {
	DB *sql.DB
}

// IsUserActive returns whether the user account is active in the DB.
// On sql.ErrNoRows (unknown user): returns true to fail-open (let downstream handle 404).
func (c *DBUserStatusChecker) IsUserActive(ctx context.Context, userID string) (bool, error) {
	var isActive bool
	err := c.DB.QueryRowContext(ctx, "SELECT is_active FROM users WHERE user_id = $1", userID).Scan(&isActive)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil // unknown user: fail open
	}
	return isActive, err
}

// statusCacheEntry pairs the cached result with its expiry time.
type statusCacheEntry struct {
	isActive  bool
	expiresAt time.Time
}

// makeStatusChecker returns a closure that checks user status via cache + checker.
// The cache is a per-middleware-instance sync.Map with 60-second TTL entries.
// This ensures each call to WithUserStatusCheck gets its own isolated cache —
// important for test isolation and for production correctness (one cache per gateway instance).
//
// On DB error: fail-open (return isActive=true, log warning). This prevents
// a DB outage from locking out all authenticated users.
func makeStatusChecker(cache *sync.Map, checker UserStatusChecker) func(ctx context.Context, userID string) (bool, error) {
	return func(ctx context.Context, userID string) (bool, error) {
		// Check cache first (60s TTL).
		if v, ok := cache.Load(userID); ok {
			entry := v.(statusCacheEntry)
			if time.Now().Before(entry.expiresAt) {
				return entry.isActive, nil
			}
			cache.Delete(userID) // expired — remove and re-fetch
		}
		isActive, err := checker.IsUserActive(ctx, userID)
		if err != nil {
			return true, err // fail open on DB error
		}
		cache.Store(userID, statusCacheEntry{
			isActive:  isActive,
			expiresAt: time.Now().Add(60 * time.Second),
		})
		return isActive, nil
	}
}

// WithUserStatusCheck wraps an existing JWT middleware and adds an is_active check.
// The check runs AFTER the inner middleware has verified the token and populated
// ContextKeyUserID in the context. This preserves the existing JWTMiddleware signature.
//
// Each call to WithUserStatusCheck creates a fresh per-middleware cache (sync.Map, 60s TTL).
// If checker is nil, the check is skipped (backward compat for callers that pass nil).
// On DB error, the request is allowed through (fail-open) and a warning is logged.
// If is_active=false, responds with 401 M_UNKNOWN_TOKEN "Account deactivated".
func WithUserStatusCheck(next func(http.Handler) http.Handler, checker UserStatusChecker) func(http.Handler) http.Handler {
	// Allocate a fresh cache per middleware instance (test isolation + single-instance safety).
	var cache sync.Map
	var checkFn func(ctx context.Context, userID string) (bool, error)
	if checker != nil {
		checkFn = makeStatusChecker(&cache, checker)
	}

	return func(h http.Handler) http.Handler {
		return next(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// At this point the inner middleware has run (JWT verified) and
			// ContextKeyUserID is populated in the context.
			userID, _ := r.Context().Value(ContextKeyUserID).(string)
			if checkFn != nil && userID != "" {
				active, err := checkFn(r.Context(), userID)
				if err != nil {
					slog.Warn("user status check failed — failing open", "user_id", userID, "err", err)
				} else if !active {
					writeMatrixError(w, http.StatusUnauthorized, "M_UNKNOWN_TOKEN", "Account deactivated")
					return
				}
			}
			h.ServeHTTP(w, r)
		}))
	}
}

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
	// ContextKeyDeviceID holds the device_id from the JWT "did" claim.
	// Empty string if the claim is not present (most OIDC tokens don't include it).
	// Story 7-26: used by device management handlers for current-device detection.
	ContextKeyDeviceID contextKey = "device_id"
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
// userIDClaimLoader is called per-request to retrieve the configured oidc_user_id_claim
// from server_config. Pass nil to fall back to the legacy "name" claim behavior (AC7).
func JWTMiddleware(provider *auth.Provider, clientID string, claimName string, store TokenStore, userIDClaimLoader func(ctx context.Context) string, serverName ...string) func(http.Handler) http.Handler {
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
			email, _ := allClaims["email"].(string)
			deviceID, _ := allClaims["did"].(string) // Story 7-26: device_id claim ("did")
			rawRole := auth.ExtractRoleClaim(allClaims, claimName)
			systemRole := auth.MapSystemRole(rawRole)

			// Determine Matrix user ID using DB-loaded oidc_user_id_claim (AC5, Story 11-10).
			// Per-request DB read (no restart required). Falls back to "name" claim when
			// oidc_user_id_claim is absent from server_config (AC7 backward compat).
			uidClaim := "name"
			if userIDClaimLoader != nil {
				if loaded := userIDClaimLoader(r.Context()); loaded != "" {
					uidClaim = loaded
				}
			}
			userID := coregrpc.FormatUserIDFromClaims(uidClaim, allClaims, srv)

			ctx := r.Context()
			ctx = context.WithValue(ctx, ContextKeySub, sub)
			ctx = context.WithValue(ctx, ContextKeyPreferredUsername, preferredUsername)
			ctx = context.WithValue(ctx, ContextKeyEmail, email)
			ctx = context.WithValue(ctx, ContextKeyNebuRole, rawRole)
			ctx = context.WithValue(ctx, ContextKeySystemRole, systemRole)
			ctx = context.WithValue(ctx, ContextKeyTokenExpiry, idToken.Expiry)
			ctx = context.WithValue(ctx, ContextKeyUserID, userID)
			ctx = context.WithValue(ctx, ContextKeyDeviceID, deviceID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
