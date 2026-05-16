//go:build integration

package integration_test

// claim_lock_steps_test.go — Story 14-1b: Gateway API Validation for matrix_user_id_claim
//
// Covers:
//   AC4 — Godog integration test for PATCH /api/v1/admin/config with matrix_user_id_claim:
//     - POST-bootstrap: returns 400 M_FORBIDDEN
//     - PRE-bootstrap: returns 200
//
// Authentication: real Matrix Bearer token for kai@example.com (instance_admin).
// Reuses step "the instance_admin kai is authenticated for admin API" from admin_api_steps_test.go.
// Reuses "bootstrap is complete and server_config is seeded" from admin_bootstrap_steps_test.go.
// Reuses "the server has no bootstrap_completed in server_config" from admin_bootstrap_steps_test.go.

import (
	"fmt"

	"github.com/cucumber/godog"
)

// theAdminPATCHesAdminConfigWithMatrixUserIDClaim calls PATCH /api/v1/admin/config with a
// JSON body containing the given matrix_user_id_claim value, using the admin Bearer token.
// Captures the response into lastStatusCode / lastBody.
func theAdminPATCHesAdminConfigWithMatrixUserIDClaim(claimValue string) error {
	if adminAPIAdminToken == "" {
		return fmt.Errorf("admin API token not set — ensure 'the instance_admin kai is authenticated for admin API' ran first")
	}
	body := fmt.Sprintf(`{"matrix_user_id_claim":%q}`, claimValue)
	return adminAPIDoRequest("PATCH", gatewayURL+"/api/v1/admin/config", adminAPIAdminToken, body)
}

// initializeClaimLockSteps registers the claim-lock integration test step definitions.
func initializeClaimLockSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the admin PATCHes /api/v1/admin/config with matrix_user_id_claim "([^"]*)"$`,
		theAdminPATCHesAdminConfigWithMatrixUserIDClaim)
}
