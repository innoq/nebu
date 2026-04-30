//go:build integration

package integration_test

import (
	"os"
	"testing"

	"github.com/cucumber/godog"
)

// gatewayURL is the base URL for HTTP calls to the gateway.
// Override via NEBU_TEST_GATEWAY_URL env var (default: http://localhost:8080).
var gatewayURL string

// coreURL is the base URL for HTTP calls to the Elixir core.
// Override via NEBU_TEST_CORE_URL env var (default: http://localhost:4000).
var coreURL string

// dexURL is the base URL for the Dex OIDC provider.
// Override via NEBU_TEST_DEX_URL env var (default: http://dex:5556).
var dexURL string

// matrixURL is the base URL for Matrix Client-Server API calls (port 8008).
// Override via NEBU_TEST_MATRIX_URL env var (default: http://gateway:8008).
var matrixURL string

// dbURL is the PostgreSQL connection string for direct DB access in step definitions.
// Override via NEBU_TEST_DB_URL env var (default: postgresql://nebu:nebu_dev_password@postgres:5432/nebu).
var dbURL string

// migrationDBURL is the PostgreSQL connection string for privileged operations (TRUNCATE, DDL).
// Uses nebu_migrate role which has BYPASSRLS and table ownership.
// Override via NEBU_TEST_MIGRATION_DB_URL env var.
var migrationDBURL string

// internalSecret is the gateway's HMAC internal secret, used to forge admin session cookies.
// Override via NEBU_TEST_INTERNAL_SECRET env var (default: dev-secret-placeholder for local runs).
var internalSecret string

// coreGRPCAddr is the gRPC address for the Elixir core service.
// Override via NEBU_TEST_CORE_GRPC_ADDR env var (default: core:9000).
var coreGRPCAddr string

func TestMain(m *testing.M) {
	gatewayURL = os.Getenv("NEBU_TEST_GATEWAY_URL")
	if gatewayURL == "" {
		gatewayURL = "http://localhost:8080"
	}
	coreURL = os.Getenv("NEBU_TEST_CORE_URL")
	if coreURL == "" {
		coreURL = "http://localhost:4000"
	}
	dexURL = os.Getenv("NEBU_TEST_DEX_URL")
	if dexURL == "" {
		dexURL = "http://dex:5556"
	}
	matrixURL = os.Getenv("NEBU_TEST_MATRIX_URL")
	if matrixURL == "" {
		matrixURL = "http://gateway:8008"
	}
	dbURL = os.Getenv("NEBU_TEST_DB_URL")
	if dbURL == "" {
		dbURL = "postgresql://nebu:nebu_dev_password@postgres:5432/nebu"
	}
	migrationDBURL = os.Getenv("NEBU_TEST_MIGRATION_DB_URL")
	if migrationDBURL == "" {
		migrationDBURL = "postgresql://nebu_migrate:nebu_migrate_dev_pw@postgres:5432/nebu"
	}
	internalSecret = os.Getenv("NEBU_TEST_INTERNAL_SECRET")
	if internalSecret == "" {
		internalSecret = "dev-secret-placeholder"
	}
	coreGRPCAddr = os.Getenv("NEBU_TEST_CORE_GRPC_ADDR")
	if coreGRPCAddr == "" {
		coreGRPCAddr = "core:9000"
	}
	os.Exit(m.Run())
}

// TestIntegrationSuite runs all Gherkin scenarios from gateway/features/.
func TestIntegrationSuite(t *testing.T) {
	suite := godog.TestSuite{
		Name: "integration",
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"../../features"},
			TestingT: t,
			Strict:   true, // all steps must be defined — undefined steps fail the suite
			NoColors: true, // cleaner CI output
		},
		ScenarioInitializer: InitializeScenario,
	}
	if suite.Run() != 0 {
		t.Fatal("integration test suite failed")
	}
}
