package auth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nebu/nebu/internal/auth"
)

func TestNewProvider_Success(t *testing.T) {
	var serverURL string

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                 serverURL,
			"authorization_endpoint": serverURL + "/auth",
			"jwks_uri":               serverURL + "/keys",
		})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{}})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()
	serverURL = srv.URL

	p := auth.NewProvider(context.Background(), srv.URL)
	if p.Inner() == nil {
		t.Fatal("Inner() must be non-nil after successful discovery")
	}
}

func TestNewProvider_Unreachable(t *testing.T) {
	// Port 0 is always closed — simulates unreachable OIDC provider.
	p := auth.NewProvider(context.Background(), "http://127.0.0.1:0")
	if p == nil {
		t.Fatal("NewProvider must never return nil")
	}
	if p.Inner() != nil {
		t.Fatal("Inner() must be nil when discovery fails")
	}
}
