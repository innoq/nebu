package config

import (
	"os"
	"strconv"
	"strings"
)

// Config holds all NEBU_ environment variable configuration for the gateway.
type Config struct {
	CoreGRPCAddr       string // NEBU_CORE_GRPC_ADDR (default: "core:9000")
	DBURL              string // NEBU_DB_URL (runtime app role — nebu_app, non-superuser)
	DBURLMigrate       string // NEBU_DB_URL_MIGRATE (migration role — nebu_migrate, table owner)
	OIDCIssuer         string // NEBU_OIDC_ISSUER
	OIDCClientID       string // NEBU_OIDC_CLIENT_ID
	OIDCClientSecret   string // NEBU_OIDC_CLIENT_SECRET
	InternalSecretFile string // NEBU_INTERNAL_SECRET_FILE (path to mounted secret file)
	ServerName         string // NEBU_SERVER_NAME
	TLSCertFile        string // NEBU_TLS_CERT_FILE
	TLSKeyFile         string // NEBU_TLS_KEY_FILE
	TLSClientCAFile    string // NEBU_TLS_CLIENT_CA_FILE (mTLS Phase 2 — not wired up in MVP)
	OIDCClaimRole         string   // NEBU_OIDC_CLAIM_ROLE (default: "nebu_role")
	OIDCDisplayName       string   // NEBU_OIDC_DISPLAY_NAME (default: "SSO")
	SSORedirectSchemes    []string // NEBU_SSO_REDIRECT_SCHEMES (comma-separated extra deep-link schemes)
	BufferCapacity        int      // NEBU_BUFFER_CAPACITY (default: 500)
	BufferBaseRate        float64  // NEBU_BUFFER_BASE_RATE (default: 100.0)
	Env                   string   // NEBU_ENV ("production", "dev", "staging", etc.)
	AllowInsecureKEK      string   // NEBU_ALLOW_INSECURE_KEK ("true" = opt-in for zero KEK in production)
}

// Load reads configuration from environment variables.
// NEBU_INTERNAL_SECRET_FILE stores a file path — callers read the file contents themselves.
func Load() Config {
	return Config{
		CoreGRPCAddr:       getEnvOrDefault("NEBU_CORE_GRPC_ADDR", "core:9000"),
		DBURL:              os.Getenv("NEBU_DB_URL"),
		DBURLMigrate:       os.Getenv("NEBU_DB_URL_MIGRATE"),
		OIDCIssuer:         os.Getenv("NEBU_OIDC_ISSUER"),
		OIDCClientID:       os.Getenv("NEBU_OIDC_CLIENT_ID"),
		OIDCClientSecret:   os.Getenv("NEBU_OIDC_CLIENT_SECRET"),
		InternalSecretFile: os.Getenv("NEBU_INTERNAL_SECRET_FILE"),
		ServerName:         os.Getenv("NEBU_SERVER_NAME"),
		TLSCertFile:        os.Getenv("NEBU_TLS_CERT_FILE"),
		TLSKeyFile:         os.Getenv("NEBU_TLS_KEY_FILE"),
		TLSClientCAFile:    os.Getenv("NEBU_TLS_CLIENT_CA_FILE"),
		OIDCClaimRole:         getEnvOrDefault("NEBU_OIDC_CLAIM_ROLE", "nebu_role"),
		OIDCDisplayName:       getEnvOrDefault("NEBU_OIDC_DISPLAY_NAME", "SSO"),
		SSORedirectSchemes:    getEnvStringSlice("NEBU_SSO_REDIRECT_SCHEMES"),
		BufferCapacity:        getEnvInt("NEBU_BUFFER_CAPACITY", 500),
		BufferBaseRate:        getEnvFloat("NEBU_BUFFER_BASE_RATE", 100.0),
		Env:                   os.Getenv("NEBU_ENV"),
		AllowInsecureKEK:      os.Getenv("NEBU_ALLOW_INSECURE_KEK"),
	}
}

func getEnvOrDefault(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

// getEnvInt reads an integer environment variable.
// Falls back to defaultValue if the variable is unset or cannot be parsed.
func getEnvInt(key string, defaultValue int) int {
	v := os.Getenv(key)
	if v == "" {
		return defaultValue
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return defaultValue
	}
	return n
}

// getEnvStringSlice reads a comma-separated environment variable and returns a
// slice of trimmed, non-empty strings. Returns nil if the variable is unset or
// empty.
func getEnvStringSlice(key string) []string {
	v := os.Getenv(key)
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			result = append(result, s)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// getEnvFloat reads a float64 environment variable.
// Falls back to defaultValue if the variable is unset or cannot be parsed.
func getEnvFloat(key string, defaultValue float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return defaultValue
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return defaultValue
	}
	return f
}
