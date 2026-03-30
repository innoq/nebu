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

func mapSystemRole(rawClaim string) string {
	switch rawClaim {
	case "instance_admin", "compliance_officer":
		return rawClaim
	default:
		return "user"
	}
}

func generateDeviceID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // UUID version 4
	b[8] = (b[8] & 0x3f) | 0x80 // UUID variant 2
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

type LoginHandler struct {
	displayName   string
	provider      *auth.Provider
	coreClient    CoreClient
	serverName    string
	clientID      string
	roleClaimName string
}

type LoginConfig struct {
	DisplayName   string
	Provider      *auth.Provider
	CoreClient    CoreClient
	ServerName    string
	ClientID      string
	RoleClaimName string
}

func NewLoginHandler(cfg LoginConfig) *LoginHandler {
	return &LoginHandler{
		displayName:   cfg.DisplayName,
		provider:      cfg.Provider,
		coreClient:    cfg.CoreClient,
		serverName:    cfg.ServerName,
		clientID:      cfg.ClientID,
		roleClaimName: cfg.RoleClaimName,
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

	verifier := inner.Verifier(&oidc.Config{ClientID: h.clientID})
	idToken, err := verifier.Verify(r.Context(), req.Token)
	if err != nil {
		writeMatrixError(w, http.StatusForbidden, "M_FORBIDDEN", "Invalid or expired token")
		return
	}

	var allClaims map[string]interface{}
	if err := idToken.Claims(&allClaims); err != nil {
		writeMatrixError(w, http.StatusForbidden, "M_FORBIDDEN", "Invalid token claims")
		return
	}
	sub, _ := allClaims["sub"].(string)
	preferredUsername, _ := allClaims["preferred_username"].(string)
	email, _ := allClaims["email"].(string)
	rawRole, _ := allClaims[h.roleClaimName].(string)
	systemRole := mapSystemRole(rawRole)

	userID := coregrpc.FormatUserID(sub, h.serverName)
	grpcCtx := coregrpc.WithUserMetadata(r.Context(), userID, systemRole)
	_, err = h.coreClient.ValidateToken(grpcCtx, &pb.ValidateTokenRequest{
		DisplayName: preferredUsername,
		Email:       email,
	})
	if err != nil {
		slog.Error("ValidateToken gRPC failed", "err", err, "user_id", userID)
		writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		return
	}

	resp := LoginTokenResponse{
		AccessToken: req.Token,
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
