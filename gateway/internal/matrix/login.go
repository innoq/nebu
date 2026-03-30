package matrix

import (
	"encoding/json"
	"net/http"
)

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

type LoginHandler struct {
	displayName string
}

func NewLoginHandler(displayName string) *LoginHandler {
	return &LoginHandler{displayName: displayName}
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
