package config_test

import (
	"os"
	"testing"

	"github.com/nebu/nebu/internal/config"
)

func TestLoad_Defaults(t *testing.T) {
	os.Unsetenv("NEBU_CORE_GRPC_ADDR")
	os.Unsetenv("NEBU_DB_URL")
	os.Unsetenv("NEBU_OIDC_ISSUER")
	os.Unsetenv("NEBU_OIDC_CLIENT_ID")
	os.Unsetenv("NEBU_OIDC_CLIENT_SECRET")
	os.Unsetenv("NEBU_INTERNAL_SECRET_FILE")
	os.Unsetenv("NEBU_SERVER_NAME")
	os.Unsetenv("NEBU_TLS_CERT_FILE")
	os.Unsetenv("NEBU_TLS_KEY_FILE")
	os.Unsetenv("NEBU_TLS_CLIENT_CA_FILE")
	os.Unsetenv("NEBU_OIDC_CLAIM_ROLE")
	os.Unsetenv("NEBU_OIDC_DISPLAY_NAME")

	cfg := config.Load()

	if cfg.CoreGRPCAddr != "core:9000" {
		t.Errorf("CoreGRPCAddr: got %q, want %q", cfg.CoreGRPCAddr, "core:9000")
	}
	if cfg.DBURL != "" {
		t.Errorf("DBURL: got %q, want empty", cfg.DBURL)
	}
	if cfg.OIDCIssuer != "" {
		t.Errorf("OIDCIssuer: got %q, want empty", cfg.OIDCIssuer)
	}
	if cfg.OIDCClientID != "" {
		t.Errorf("OIDCClientID: got %q, want empty", cfg.OIDCClientID)
	}
	if cfg.OIDCClientSecret != "" {
		t.Errorf("OIDCClientSecret: got %q, want empty", cfg.OIDCClientSecret)
	}
	if cfg.InternalSecretFile != "" {
		t.Errorf("InternalSecretFile: got %q, want empty", cfg.InternalSecretFile)
	}
	if cfg.ServerName != "" {
		t.Errorf("ServerName: got %q, want empty", cfg.ServerName)
	}
	if cfg.TLSCertFile != "" {
		t.Errorf("TLSCertFile: got %q, want empty", cfg.TLSCertFile)
	}
	if cfg.TLSKeyFile != "" {
		t.Errorf("TLSKeyFile: got %q, want empty", cfg.TLSKeyFile)
	}
	if cfg.TLSClientCAFile != "" {
		t.Errorf("TLSClientCAFile: got %q, want empty", cfg.TLSClientCAFile)
	}
	if cfg.OIDCClaimRole != "nebu_role" {
		t.Errorf("OIDCClaimRole: got %q, want %q", cfg.OIDCClaimRole, "nebu_role")
	}
	if cfg.OIDCDisplayName != "SSO" {
		t.Errorf("OIDCDisplayName: got %q, want %q", cfg.OIDCDisplayName, "SSO")
	}
}

func TestLoad_TLSFields(t *testing.T) {
	t.Setenv("NEBU_TLS_CERT_FILE", "/certs/server.crt")
	t.Setenv("NEBU_TLS_KEY_FILE", "/certs/server.key")
	t.Setenv("NEBU_TLS_CLIENT_CA_FILE", "/certs/ca.crt")

	cfg := config.Load()

	if cfg.TLSCertFile != "/certs/server.crt" {
		t.Errorf("TLSCertFile: got %q, want /certs/server.crt", cfg.TLSCertFile)
	}
	if cfg.TLSKeyFile != "/certs/server.key" {
		t.Errorf("TLSKeyFile: got %q, want /certs/server.key", cfg.TLSKeyFile)
	}
	if cfg.TLSClientCAFile != "/certs/ca.crt" {
		t.Errorf("TLSClientCAFile: got %q, want /certs/ca.crt", cfg.TLSClientCAFile)
	}
}

func TestLoad_EnvVarsOverrideDefaults(t *testing.T) {
	t.Setenv("NEBU_CORE_GRPC_ADDR", "custom-core:9999")
	t.Setenv("NEBU_DB_URL", "postgres://user:pass@db/nebu")
	t.Setenv("NEBU_OIDC_ISSUER", "https://auth.example.com")
	t.Setenv("NEBU_OIDC_CLIENT_ID", "nebu-gateway")
	t.Setenv("NEBU_OIDC_CLIENT_SECRET", "nebu-dev-secret")
	t.Setenv("NEBU_INTERNAL_SECRET_FILE", "/run/secrets/internal_secret")
	t.Setenv("NEBU_SERVER_NAME", "nebu.example.com")
	t.Setenv("NEBU_OIDC_DISPLAY_NAME", "My Provider")

	cfg := config.Load()

	if cfg.CoreGRPCAddr != "custom-core:9999" {
		t.Errorf("CoreGRPCAddr: got %q, want %q", cfg.CoreGRPCAddr, "custom-core:9999")
	}
	if cfg.DBURL != "postgres://user:pass@db/nebu" {
		t.Errorf("DBURL: got %q", cfg.DBURL)
	}
	if cfg.OIDCIssuer != "https://auth.example.com" {
		t.Errorf("OIDCIssuer: got %q", cfg.OIDCIssuer)
	}
	if cfg.OIDCClientID != "nebu-gateway" {
		t.Errorf("OIDCClientID: got %q", cfg.OIDCClientID)
	}
	if cfg.OIDCClientSecret != "nebu-dev-secret" {
		t.Errorf("OIDCClientSecret: got %q", cfg.OIDCClientSecret)
	}
	if cfg.InternalSecretFile != "/run/secrets/internal_secret" {
		t.Errorf("InternalSecretFile: got %q", cfg.InternalSecretFile)
	}
	if cfg.ServerName != "nebu.example.com" {
		t.Errorf("ServerName: got %q", cfg.ServerName)
	}
	if cfg.OIDCDisplayName != "My Provider" {
		t.Errorf("OIDCDisplayName: got %q, want %q", cfg.OIDCDisplayName, "My Provider")
	}
}

func TestLoad_OIDCClaimRole_Default(t *testing.T) {
	os.Unsetenv("NEBU_OIDC_CLAIM_ROLE")

	cfg := config.Load()

	if cfg.OIDCClaimRole != "nebu_role" {
		t.Errorf("OIDCClaimRole default: got %q, want %q", cfg.OIDCClaimRole, "nebu_role")
	}
}

func TestLoad_OIDCClaimRole_CustomValue(t *testing.T) {
	t.Setenv("NEBU_OIDC_CLAIM_ROLE", "roles")

	cfg := config.Load()

	if cfg.OIDCClaimRole != "roles" {
		t.Errorf("OIDCClaimRole: got %q, want %q", cfg.OIDCClaimRole, "roles")
	}
}

func TestLoad_OIDCDisplayName_Default(t *testing.T) {
	os.Unsetenv("NEBU_OIDC_DISPLAY_NAME")

	cfg := config.Load()

	if cfg.OIDCDisplayName != "SSO" {
		t.Errorf("OIDCDisplayName default: got %q, want %q", cfg.OIDCDisplayName, "SSO")
	}
}

func TestLoad_OIDCDisplayName_CustomValue(t *testing.T) {
	t.Setenv("NEBU_OIDC_DISPLAY_NAME", "Corporate SSO")

	cfg := config.Load()

	if cfg.OIDCDisplayName != "Corporate SSO" {
		t.Errorf("OIDCDisplayName: got %q, want %q", cfg.OIDCDisplayName, "Corporate SSO")
	}
}

func TestLoad_InternalSecretFile_IsPath_NotSecret(t *testing.T) {
	// NEBU_INTERNAL_SECRET_FILE must store a file path, not the secret value itself.
	// This test documents and enforces the pattern.
	t.Setenv("NEBU_INTERNAL_SECRET_FILE", "/run/secrets/internal_secret")

	cfg := config.Load()

	// The field must contain the path string as-is — callers read the file themselves.
	if cfg.InternalSecretFile != "/run/secrets/internal_secret" {
		t.Errorf("InternalSecretFile must be the raw path, got %q", cfg.InternalSecretFile)
	}
}
