Feature: OIDC Directory Config — oidc_directory_enabled + oidc_directory_endpoint round-trip
  As an instance admin
  I want to enable and configure the OIDC user directory integration via the Admin API
  So that Admin UI user searches can include OIDC users who have not yet logged in

  # Story 14-2a — ATDD RED PHASE
  # These scenarios are written BEFORE implementation and MUST FAIL until:
  #   - Migration 000048 seeds 'oidc_directory_enabled' and 'oidc_directory_endpoint' rows
  #   - GET /api/v1/admin/config returns both fields in the JSON response
  #   - PATCH /api/v1/admin/config persists both fields correctly
  #   - RLS policy allows updating both keys
  #
  # Auth: real Matrix Bearer token for kai@example.com (instance_admin)
  # Reuses steps from admin_api_steps_test.go and admin_bootstrap_steps_test.go

  Background:
    Given the server is running with a clean test database

  # ---------------------------------------------------------------------------
  # AC2 + AC3 + AC5 — PATCH + GET round-trip: set enabled=true + endpoint, read back
  # ---------------------------------------------------------------------------
  Scenario: set oidc_directory_enabled + endpoint, read back via GET
    Given bootstrap is complete and server_config is seeded
    And the instance_admin kai is authenticated for admin API
    When the admin PATCHes /api/v1/admin/config with oidc_directory_enabled true and endpoint "https://idp.example.com/admin/users"
    Then the response status is 200
    And the response body contains "oidc_directory_enabled"
    And the response body contains "https://idp.example.com/admin/users"
    When the admin GETs /api/v1/admin/config
    Then the response status is 200
    And the response body contains "oidc_directory_enabled"
    And the response body contains "https://idp.example.com/admin/users"

  # ---------------------------------------------------------------------------
  # AC2 — GET /api/v1/admin/config always includes both fields (even with defaults)
  # ---------------------------------------------------------------------------
  Scenario: GET /api/v1/admin/config includes oidc_directory fields with defaults
    Given bootstrap is complete and server_config is seeded
    And the instance_admin kai is authenticated for admin API
    When the admin GETs /api/v1/admin/config
    Then the response status is 200
    And the response body contains "oidc_directory_enabled"
    And the response body contains "oidc_directory_endpoint"

  # ---------------------------------------------------------------------------
  # AC3 — PATCH with oidc_directory_enabled=false disables the integration
  # ---------------------------------------------------------------------------
  Scenario: PATCH oidc_directory_enabled false disables the integration
    Given bootstrap is complete and server_config is seeded
    And the instance_admin kai is authenticated for admin API
    When the admin PATCHes /api/v1/admin/config with oidc_directory_enabled false
    Then the response status is 200
    And the response body contains "oidc_directory_enabled"
    When the admin GETs /api/v1/admin/config
    Then the response status is 200
    And the response body contains "oidc_directory_enabled"
