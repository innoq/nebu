package config

import "os"

// Config holds all NEBU_ environment variable configuration for the gateway.
type Config struct {
	CoreGRPCAddr       string // NEBU_CORE_GRPC_ADDR (default: "core:9000")
	DBURL              string // NEBU_DB_URL
	OIDCIssuer         string // NEBU_OIDC_ISSUER
	OIDCClientID       string // NEBU_OIDC_CLIENT_ID
	OIDCClientSecret   string // NEBU_OIDC_CLIENT_SECRET
	InternalSecretFile string // NEBU_INTERNAL_SECRET_FILE (path to mounted secret file)
	ServerName         string // NEBU_SERVER_NAME
	TLSCertFile        string // NEBU_TLS_CERT_FILE
	TLSKeyFile         string // NEBU_TLS_KEY_FILE
	TLSClientCAFile    string // NEBU_TLS_CLIENT_CA_FILE (mTLS Phase 2 — not wired up in MVP)
}

// Load reads configuration from environment variables.
// NEBU_INTERNAL_SECRET_FILE stores a file path — callers read the file contents themselves.
func Load() Config {
	return Config{
		CoreGRPCAddr:       getEnvOrDefault("NEBU_CORE_GRPC_ADDR", "core:9000"),
		DBURL:              os.Getenv("NEBU_DB_URL"),
		OIDCIssuer:         os.Getenv("NEBU_OIDC_ISSUER"),
		OIDCClientID:       os.Getenv("NEBU_OIDC_CLIENT_ID"),
		OIDCClientSecret:   os.Getenv("NEBU_OIDC_CLIENT_SECRET"),
		InternalSecretFile: os.Getenv("NEBU_INTERNAL_SECRET_FILE"),
		ServerName:         os.Getenv("NEBU_SERVER_NAME"),
		TLSCertFile:        os.Getenv("NEBU_TLS_CERT_FILE"),
		TLSKeyFile:         os.Getenv("NEBU_TLS_KEY_FILE"),
		TLSClientCAFile:    os.Getenv("NEBU_TLS_CLIENT_CA_FILE"),
	}
}

func getEnvOrDefault(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}
