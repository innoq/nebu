package matrix

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/nebu/nebu/internal/auth"
	coregrpc "github.com/nebu/nebu/internal/grpc"
	pb "github.com/nebu/nebu/internal/grpc/pb"
	"github.com/nebu/nebu/internal/middleware"
	"github.com/nebu/nebu/internal/validate"
)

// CoreClient is a consumer-defined interface for gRPC calls to the Elixir core.
type CoreClient interface {
	ValidateToken(ctx context.Context, req *pb.ValidateTokenRequest) (*pb.ValidateTokenResponse, error)
}

type IdentityProvider struct {
	ID   string  `json:"id"`
	Name string  `json:"name"`
	Icon *string `json:"icon"`
}

type LoginFlow struct {
	Type              string             `json:"type"`
	IdentityProviders []IdentityProvider `json:"identity_providers,omitempty"`
}

type LoginResponse struct {
	Flows []LoginFlow `json:"flows"`
}

type LoginRequest struct {
	Type  string `json:"type"`
	Token string `json:"token"`
}

type LoginTokenResponse struct {
	AccessToken string `json:"access_token"`
	DeviceID    string `json:"device_id"`
	UserID      string `json:"user_id"`
	TokenType   string `json:"token_type"`
}

type matrixError struct {
	ErrCode string `json:"errcode"`
	Err     string `json:"error"`
}

func writeMatrixError(w http.ResponseWriter, status int, errcode, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(matrixError{ErrCode: errcode, Err: message})
}

func generateDeviceID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // UUID version 4
	b[8] = (b[8] & 0x3f) | 0x80 // UUID variant 2
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

type LoginHandler struct {
	displayName        string
	provider           *auth.Provider
	coreClient         CoreClient
	store              middleware.TokenStore // optional; nil = skip denylist check in PostLogin
	serverName         string
	clientID           string
	clientSecret       string
	roleClaimName      string
	ssoRedirectSchemes []string // extra deep-link schemes from NEBU_SSO_REDIRECT_SCHEMES
}

type LoginConfig struct {
	DisplayName        string
	Provider           *auth.Provider
	CoreClient         CoreClient
	Store              middleware.TokenStore // optional; when set, PostLogin rejects invalidated JWTs
	ServerName         string
	ClientID           string
	ClientSecret       string
	RoleClaimName      string
	SSORedirectSchemes []string // extra deep-link schemes from NEBU_SSO_REDIRECT_SCHEMES
}

func NewLoginHandler(cfg LoginConfig) *LoginHandler {
	if cfg.Store == nil {
		slog.Warn("LoginHandler: no TokenStore configured — denylist check disabled; PostLogin will not reject invalidated JWTs")
	}
	return &LoginHandler{
		displayName:        cfg.DisplayName,
		provider:           cfg.Provider,
		coreClient:         cfg.CoreClient,
		store:              cfg.Store,
		serverName:         cfg.ServerName,
		clientID:           cfg.ClientID,
		clientSecret:       cfg.ClientSecret,
		roleClaimName:      cfg.RoleClaimName,
		ssoRedirectSchemes: cfg.SSORedirectSchemes,
	}
}

func (h *LoginHandler) PostLogin(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeMatrixError(w, http.StatusBadRequest, "M_NOT_JSON", "Request body is not valid JSON")
		return
	}

	if req.Type != "m.login.token" {
		writeMatrixError(w, http.StatusBadRequest, "M_UNKNOWN", "Unsupported login type")
		return
	}

	inner := h.provider.Inner()
	if inner == nil {
		writeMatrixError(w, http.StatusServiceUnavailable, "M_UNKNOWN", "Authentication service unavailable")
		return
	}

	// MAJOR-2 fix: req.Token may be an opaque SSO loginToken (64 hex chars) or a
	// raw JWT. Try the opaque store first (single-use, 30s TTL). Fall back to
	// treating it as a raw JWT for any client that calls POST /login directly.
	rawJWT := req.Token
	if idTokenFromStore, found := globalLoginTokens.pop(req.Token); found {
		rawJWT = idTokenFromStore
	}

	verifier := inner.Verifier(&oidc.Config{
		ClientID:             h.clientID,
		SupportedSigningAlgs: validate.SupportedAlgs(),
	})
	idToken, err := verifier.Verify(r.Context(), rawJWT)
	if err != nil {
		writeMatrixError(w, http.StatusForbidden, "M_FORBIDDEN", "Invalid or expired token")
		return
	}

	// Denylist check: reject JWTs that were previously invalidated on logout.
	// This guards against two scenarios:
	//   1. Dex returned a cached id_token despite prompt=login (nonce mismatch already
	//      caught in GetSSOCallback, but defense-in-depth for direct POST /login calls).
	//   2. A client submitting a raw JWT directly (fallback path) that was already logged out.
	//
	// 403 M_FORBIDDEN (not 401 M_UNKNOWN_TOKEN) is intentional here: POST /login is an
	// authentication attempt, not a session validation. 403 signals the credential is
	// forbidden; JWTMiddleware returns 401 M_UNKNOWN_TOKEN for invalidated session tokens.
	if h.store != nil && h.store.IsInvalidated(rawJWT) {
		slog.Warn("PostLogin: rejected invalidated JWT — token is in the denylist", "event", "denylist_hit_login")
		writeMatrixError(w, http.StatusForbidden, "M_FORBIDDEN", "Token has been logged out — please log in again")
		return
	}

	var allClaims map[string]interface{}
	if err := idToken.Claims(&allClaims); err != nil {
		writeMatrixError(w, http.StatusForbidden, "M_FORBIDDEN", "Invalid token claims")
		return
	}
	sub, _ := allClaims["sub"].(string)
	email, _ := allClaims["email"].(string)
	rawRole := auth.ExtractRoleClaim(allClaims, h.roleClaimName)
	systemRole := auth.MapSystemRole(rawRole)

	// Resolve display name from JWT claims in priority order:
	//   1. preferred_username (OIDC standard)
	//   2. name (Dex maps username → name claim for local passwords)
	//   3. email local part as fallback
	// Later: this should come from a configurable claim-mapping (Story 7-15).
	displayName, _ := allClaims["preferred_username"].(string)
	if displayName == "" {
		displayName, _ = allClaims["name"].(string)
	}
	if displayName == "" && email != "" {
		// Fall back to local part of email (before @)
		for i, c := range email {
			if c == '@' {
				displayName = email[:i]
				break
			}
		}
	}

	// Use name claim as Matrix localpart if available (e.g. "alex" → "@alex:server").
	// Story 7-15 will make this configurable via Bootstrap claim-mapping.
	userID := coregrpc.FormatUserIDFromClaims(sub, displayName, h.serverName)
	grpcCtx := coregrpc.WithUserMetadata(r.Context(), userID, systemRole)
	_, err = h.coreClient.ValidateToken(grpcCtx, &pb.ValidateTokenRequest{
		DisplayName: displayName,
		Email:       email,
	})
	if err != nil {
		slog.Error("ValidateToken gRPC failed", "err", err, "user_id", userID)
		writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		return
	}

	resp := LoginTokenResponse{
		AccessToken: rawJWT, // always the real JWT, never the opaque loginToken
		DeviceID:    generateDeviceID(),
		UserID:      userID,
		TokenType:   "Bearer",
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *LoginHandler) GetLogin(w http.ResponseWriter, r *http.Request) {
	resp := LoginResponse{
		Flows: []LoginFlow{
			{
				Type: "m.login.sso",
				IdentityProviders: []IdentityProvider{
					{
						ID:   "oidc",
						Name: h.displayName,
						Icon: nil,
					},
				},
			},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
