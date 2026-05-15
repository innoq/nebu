//go:build integration

package integration_test

// ─── Story 13-7: MSC2965 OIDC Discovery Endpoints ────────────────────────────
//
// Integration step definitions for oidc_discovery.feature.
// These test the running gateway's OIDC discovery routes at the HTTP level.
//
// Endpoints under test:
//   GET /_matrix/client/unstable/org.matrix.msc2965/auth_issuer
//   GET /_matrix/client/v1/auth_issuer
//   GET /_matrix/client/unstable/org.matrix.msc2965/auth_metadata
//   GET /_matrix/client/v1/auth_metadata
//
// All scenarios are RED PHASE — they will fail until oidc_discovery.go is
// implemented and its routes registered in gateway/cmd/gateway/main.go.

import (
	"fmt"
	"net/http"

	"github.com/cucumber/godog"
)

// ─── Step Definitions ────────────────────────────────────────────────────────

// aClientCallsGETWithoutAuthentication makes an unauthenticated GET request to
// the given Matrix API path on the gateway.
// Step: "a client calls GET {path} without authentication"
func aClientCallsGETWithoutAuthentication(path string) error {
	url := matrixURL + path
	resp, err := http.Get(url) //nolint:gosec,noctx
	if err != nil {
		return fmt.Errorf("GET %s failed: %w", url, err)
	}
	return captureResponse(resp)
}

// ─── Scenario Initializer ────────────────────────────────────────────────────

func initializeOIDCDiscoverySteps(sc *godog.ScenarioContext) {
	sc.Step(
		`^a client calls GET (/_matrix/client/[^\s]+) without authentication$`,
		aClientCallsGETWithoutAuthentication,
	)
}
