package integration_test

import (
	"os"
	"testing"

	"github.com/cucumber/godog"
)

// gatewayURL is the base URL for all HTTP calls to the gateway.
// Override via NEBU_TEST_GATEWAY_URL env var (default: http://localhost:8080).
var gatewayURL string

func TestMain(m *testing.M) {
	gatewayURL = os.Getenv("NEBU_TEST_GATEWAY_URL")
	if gatewayURL == "" {
		gatewayURL = "http://localhost:8080"
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
			Strict:   false, // pending/undefined steps do not fail the suite
			NoColors: true,  // cleaner CI output
		},
		ScenarioInitializer: InitializeScenario,
	}
	if suite.Run() != 0 {
		t.Fatal("integration test suite failed")
	}
}

// InitializeScenario registers step definitions.
// HTTP step implementations are added in Story 1.19.
func InitializeScenario(sc *godog.ScenarioContext) {
	// step definitions added in story 1.19
}
