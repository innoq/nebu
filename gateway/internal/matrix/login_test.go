package matrix

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	josejwt "github.com/go-jose/go-jose/v4/jwt"
	"github.com/nebu/nebu/internal/auth"
	pb "github.com/nebu/nebu/internal/grpc/pb"
)

type mockCoreClient struct {
	validateResp *pb.ValidateTokenResponse
	validateErr  error
}

func (m *mockCoreClient) ValidateToken(ctx context.Context, req *pb.ValidateTokenRequest) (*pb.ValidateTokenResponse, error) {
	return m.validateResp, m.validateErr
}

func setupOIDCServer(t *testing.T) (*httptest.Server, *rsa.PrivateKey) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}

	jwk := jose.JSONWebKey{
		Key:       &privateKey.PublicKey,
		KeyID:     "test-key-1",
		Algorithm: string(jose.RS256),
		Use:       "sig",
	}
	jwks := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{jwk}}

	var serverURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":   serverURL,
			"jwks_uri": serverURL + "/keys",
		})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jwks)
	})

	srv := httptest.NewServer(mux)
	serverURL = srv.URL
	t.Cleanup(srv.Close)
	return srv, privateKey
}

func signJWT(t *testing.T, serverURL string, privateKey *rsa.PrivateKey, expiry time.Time, claims map[string]any) string {
	t.Helper()
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: privateKey},
		(&jose.SignerOptions{}).WithHeader("kid", "test-key-1"),
	)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	cl := josejwt.Claims{
		Subject:  "test-sub-123",
		Issuer:   serverURL,
		Audience: josejwt.Audience{"nebu-gateway"},
		Expiry:   josejwt.NewNumericDate(expiry),
		IssuedAt: josejwt.NewNumericDate(time.Now().Add(-time.Minute)),
	}
	extra := map[string]any{
		"preferred_username": "kai.mueller",
		"email":              "kai@example.com",
		"nebu_role":          "instance_admin",
	}
	for k, v := range claims {
		extra[k] = v
	}
	raw, err := josejwt.Signed(signer).Claims(cl).Claims(extra).Serialize()
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	return raw
}

func TestGetLogin_ReturnsSSO(t *testing.T) {
	h := NewLoginHandler(LoginConfig{DisplayName: "Test SSO"})
	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/login", nil)
	w := httptest.NewRecorder()

	h.GetLogin(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected application/json, got %s", ct)
	}

	var resp LoginResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(resp.Flows) != 1 {
		t.Fatalf("expected 1 flow, got %d", len(resp.Flows))
	}
	if resp.Flows[0].Type != "m.login.sso" {
		t.Errorf("expected m.login.sso, got %s", resp.Flows[0].Type)
	}

	idps := resp.Flows[0].IdentityProviders
	if len(idps) != 1 {
		t.Fatalf("expected 1 identity provider, got %d", len(idps))
	}
	if idps[0].ID != "oidc" {
		t.Errorf("expected id oidc, got %s", idps[0].ID)
	}
	if idps[0].Name != "Test SSO" {
		t.Errorf("expected name Test SSO, got %s", idps[0].Name)
	}
	if idps[0].Icon != nil {
		t.Errorf("expected icon nil, got %v", idps[0].Icon)
	}
}

func TestPostLogin(t *testing.T) {
	srv, key := setupOIDCServer(t)
	provider := auth.NewProvider(context.Background(), srv.URL)

	tests := []struct {
		name          string
		body          string
		provider      *auth.Provider
		coreClient    CoreClient
		wantStatus    int
		wantErrCode   string
		wantAccessTok bool // if true, check access_token matches input JWT
	}{
		{
			name:     "valid token",
			body:     fmt.Sprintf(`{"type":"m.login.token","token":"%s"}`, signJWT(t, srv.URL, key, time.Now().Add(time.Hour), nil)),
			provider: provider,
			coreClient: &mockCoreClient{
				validateResp: &pb.ValidateTokenResponse{UserId: "@test-sub-123:localhost", SystemRole: "instance_admin", DisplayName: "kai.mueller", IsActive: true},
			},
			wantStatus:    http.StatusOK,
			wantAccessTok: true,
		},
		{
			name:        "invalid token",
			body:        `{"type":"m.login.token","token":"garbage"}`,
			provider:    provider,
			coreClient:  &mockCoreClient{},
			wantStatus:  http.StatusForbidden,
			wantErrCode: "M_FORBIDDEN",
		},
		{
			name:        "expired token",
			body:        fmt.Sprintf(`{"type":"m.login.token","token":"%s"}`, signJWT(t, srv.URL, key, time.Now().Add(-time.Hour), nil)),
			provider:    provider,
			coreClient:  &mockCoreClient{},
			wantStatus:  http.StatusForbidden,
			wantErrCode: "M_FORBIDDEN",
		},
		{
			name:        "unsupported login type",
			body:        `{"type":"m.login.password","token":"foo"}`,
			provider:    provider,
			coreClient:  &mockCoreClient{},
			wantStatus:  http.StatusBadRequest,
			wantErrCode: "M_UNKNOWN",
		},
		{
			name:        "malformed JSON",
			body:        `not-json`,
			provider:    provider,
			coreClient:  &mockCoreClient{},
			wantStatus:  http.StatusBadRequest,
			wantErrCode: "M_NOT_JSON",
		},
		{
			name:        "provider unavailable",
			body:        `{"type":"m.login.token","token":"sometoken"}`,
			provider:    auth.NewProvider(context.Background(), "http://127.0.0.1:0"),
			coreClient:  &mockCoreClient{},
			wantStatus:  http.StatusServiceUnavailable,
			wantErrCode: "M_UNKNOWN",
		},
		{
			name:     "gRPC failure",
			body:     fmt.Sprintf(`{"type":"m.login.token","token":"%s"}`, signJWT(t, srv.URL, key, time.Now().Add(time.Hour), nil)),
			provider: provider,
			coreClient: &mockCoreClient{
				validateErr: fmt.Errorf("core unavailable"),
			},
			wantStatus:  http.StatusInternalServerError,
			wantErrCode: "M_UNKNOWN",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewLoginHandler(LoginConfig{
				DisplayName:   "Test SSO",
				Provider:      tt.provider,
				CoreClient:    tt.coreClient,
				ServerName:    "localhost",
				ClientID:      "nebu-gateway",
				RoleClaimName: "nebu_role",
			})

			req := httptest.NewRequest(http.MethodPost, "/_matrix/client/v3/login", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			h.PostLogin(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d; body: %s", tt.wantStatus, w.Code, w.Body.String())
			}

			ct := w.Header().Get("Content-Type")
			if ct != "application/json" {
				t.Errorf("expected Content-Type application/json, got %s", ct)
			}

			if tt.wantAccessTok {
				var resp LoginTokenResponse
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}

				// Extract token from request body for comparison
				var loginReq LoginRequest
				_ = json.Unmarshal([]byte(tt.body), &loginReq)

				if resp.AccessToken != loginReq.Token {
					t.Errorf("access_token should echo original JWT")
				}
				if resp.TokenType != "Bearer" {
					t.Errorf("expected token_type Bearer, got %s", resp.TokenType)
				}
				if resp.UserID != "@test-sub-123:localhost" {
					t.Errorf("expected user_id @test-sub-123:localhost, got %s", resp.UserID)
				}
				if len(resp.DeviceID) == 0 {
					t.Error("device_id should not be empty")
				}
				if !strings.Contains(resp.DeviceID, "-") {
					t.Errorf("device_id should look like UUID, got %s", resp.DeviceID)
				}
			} else {
				var errResp matrixError
				if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
					t.Fatalf("failed to decode error response: %v", err)
				}
				if errResp.ErrCode != tt.wantErrCode {
					t.Errorf("expected errcode %s, got %s", tt.wantErrCode, errResp.ErrCode)
				}
			}
		})
	}
}
