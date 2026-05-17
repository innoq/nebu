//go:build integration

package integration_test

// oidc_directory_config_steps_test.go — Story 14-2a: Server Config Schema for OIDC directory
//
// Covers:
//   AC2 — GET /api/v1/admin/config returns oidc_directory_enabled + oidc_directory_endpoint
//   AC3 — PATCH /api/v1/admin/config persists both fields correctly
//   AC5 — Godog round-trip: set enabled=true + endpoint, read back via GET
//
// Authentication: real Matrix Bearer token for kai@example.com (instance_admin).
// Reuses step "the instance_admin kai is authenticated for admin API" from admin_api_steps_test.go.
// Reuses "bootstrap is complete and server_config is seeded" from admin_bootstrap_steps_test.go.
// Reuses "the admin GETs /api/v1/admin/config" if already defined; otherwise defines it here.

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/cucumber/godog"
)

// theAdminPATCHesAdminConfigWithOidcDirectoryEnabledTrueAndEndpoint calls
// PATCH /api/v1/admin/config with oidc_directory_enabled=true and the given endpoint URL.
// Captures the response into lastStatusCode / lastBody.
func theAdminPATCHesAdminConfigWithOidcDirectoryEnabledTrueAndEndpoint(endpointURL string) error {
	if adminAPIAdminToken == "" {
		return fmt.Errorf("admin API token not set — ensure 'the instance_admin kai is authenticated for admin API' ran first")
	}
	body, err := json.Marshal(map[string]any{
		"oidc_directory_enabled":  true,
		"oidc_directory_endpoint": endpointURL,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %w", err)
	}
	return adminAPIDoRequest("PATCH", matrixURL+"/api/v1/admin/config", adminAPIAdminToken, string(body))
}

// theAdminPATCHesAdminConfigWithOidcDirectoryEnabledFalse calls
// PATCH /api/v1/admin/config with oidc_directory_enabled=false.
// Captures the response into lastStatusCode / lastBody.
func theAdminPATCHesAdminConfigWithOidcDirectoryEnabledFalse() error {
	if adminAPIAdminToken == "" {
		return fmt.Errorf("admin API token not set — ensure 'the instance_admin kai is authenticated for admin API' ran first")
	}
	body, err := json.Marshal(map[string]any{
		"oidc_directory_enabled": false,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %w", err)
	}
	return adminAPIDoRequest("PATCH", matrixURL+"/api/v1/admin/config", adminAPIAdminToken, string(body))
}

// theAdminGETsAdminConfig calls GET /api/v1/admin/config.
// Captures the response into lastStatusCode / lastBody.
// Step: "the admin GETs /api/v1/admin/config"
func theAdminGETsAdminConfig() error {
	if adminAPIAdminToken == "" {
		return fmt.Errorf("admin API token not set — ensure 'the instance_admin kai is authenticated for admin API' ran first")
	}
	req, err := http.NewRequest(http.MethodGet, matrixURL+"/api/v1/admin/config", nil)
	if err != nil {
		return fmt.Errorf("failed to create GET request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+adminAPIAdminToken)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("GET /api/v1/admin/config failed: %w", err)
	}
	defer resp.Body.Close()

	lastStatusCode = resp.StatusCode
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 512)
	for {
		n, readErr := resp.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if readErr != nil {
			break
		}
	}
	lastBody = string(buf)
	return nil
}

// initializeOidcDirectoryConfigSteps registers the oidc-directory-config integration test step definitions.
func initializeOidcDirectoryConfigSteps(sc *godog.ScenarioContext) {
	sc.Step(
		`^the admin PATCHes /api/v1/admin/config with oidc_directory_enabled true and endpoint "([^"]*)"$`,
		theAdminPATCHesAdminConfigWithOidcDirectoryEnabledTrueAndEndpoint,
	)
	sc.Step(
		`^the admin PATCHes /api/v1/admin/config with oidc_directory_enabled false$`,
		theAdminPATCHesAdminConfigWithOidcDirectoryEnabledFalse,
	)
	sc.Step(
		`^the admin GETs /api/v1/admin/config$`,
		theAdminGETsAdminConfig,
	)
}
