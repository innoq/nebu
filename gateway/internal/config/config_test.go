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
	os.Unsetenv("NEBU_INTERNAL_SECRET_FILE")
	os.Unsetenv("NEBU_SERVER_NAME")

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
	if cfg.InternalSecretFile != "" {
		t.Errorf("InternalSecretFile: got %q, want empty", cfg.InternalSecretFile)
	}
	if cfg.ServerName != "" {
		t.Errorf("ServerName: got %q, want empty", cfg.ServerName)
	}
}

func TestLoad_EnvVarsOverrideDefaults(t *testing.T) {
	t.Setenv("NEBU_CORE_GRPC_ADDR", "custom-core:9999")
	t.Setenv("NEBU_DB_URL", "postgres://user:pass@db/nebu")
	t.Setenv("NEBU_OIDC_ISSUER", "https://auth.example.com")
	t.Setenv("NEBU_INTERNAL_SECRET_FILE", "/run/secrets/internal_secret")
	t.Setenv("NEBU_SERVER_NAME", "nebu.example.com")

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
	if cfg.InternalSecretFile != "/run/secrets/internal_secret" {
		t.Errorf("InternalSecretFile: got %q", cfg.InternalSecretFile)
	}
	if cfg.ServerName != "nebu.example.com" {
		t.Errorf("ServerName: got %q", cfg.ServerName)
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
