package config

import "os"

// Config holds all NEBU_ environment variable configuration for the gateway.
type Config struct {
	CoreGRPCAddr       string // NEBU_CORE_GRPC_ADDR (default: "core:9000")
	DBURL              string // NEBU_DB_URL
	OIDCIssuer         string // NEBU_OIDC_ISSUER
	InternalSecretFile string // NEBU_INTERNAL_SECRET_FILE (path to mounted secret file)
	ServerName         string // NEBU_SERVER_NAME
}

// Load reads configuration from environment variables.
// NEBU_INTERNAL_SECRET_FILE stores a file path — callers read the file contents themselves.
func Load() Config {
	return Config{
		CoreGRPCAddr:       getEnvOrDefault("NEBU_CORE_GRPC_ADDR", "core:9000"),
		DBURL:              os.Getenv("NEBU_DB_URL"),
		OIDCIssuer:         os.Getenv("NEBU_OIDC_ISSUER"),
		InternalSecretFile: os.Getenv("NEBU_INTERNAL_SECRET_FILE"),
		ServerName:         os.Getenv("NEBU_SERVER_NAME"),
	}
}

func getEnvOrDefault(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}
