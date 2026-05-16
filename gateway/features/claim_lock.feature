Feature: Claim Lock — matrix_user_id_claim cannot be changed after bootstrap
  As an instance admin
  I want the API to reject changes to matrix_user_id_claim after bootstrap
  So that Matrix User IDs remain stable for all existing users

  # Story 14-1b — AC4: Godog integration test
  #
  # Covered scenarios:
  #   - Scenario 1: POST-bootstrap PATCH with matrix_user_id_claim → 400 M_FORBIDDEN
  #   - Scenario 2: PRE-bootstrap PATCH with matrix_user_id_claim → 200
  #
  # Auth: real Matrix Bearer token obtained via Dex OIDC login (same pattern as admin_api.feature)
  # The "bootstrap is complete" step seeds the bootstrap_completed key via the DB
  # (same pattern as claim_mapping.feature and admin_bootstrap.feature).

  Background:
    Given the server is running with a clean test database

  # ---------------------------------------------------------------------------
  # AC4, AC1 — POST-bootstrap PATCH returns 400 M_FORBIDDEN
  # ---------------------------------------------------------------------------
  Scenario: POST-bootstrap PATCH attempt with matrix_user_id_claim is rejected with 400 M_FORBIDDEN
    Given bootstrap is complete and server_config is seeded
    And the instance_admin kai is authenticated for admin API
    When the admin PATCHes /api/v1/admin/config with matrix_user_id_claim "email"
    Then the response status is 400
    And the response body contains "M_FORBIDDEN"
    And the response body contains "matrix_user_id_claim cannot be changed after bootstrap"

  # ---------------------------------------------------------------------------
  # AC4, AC1 — PRE-bootstrap PATCH succeeds
  # ---------------------------------------------------------------------------
  Scenario: PRE-bootstrap PATCH attempt with matrix_user_id_claim succeeds
    Given the server has no bootstrap_completed in server_config
    And the instance_admin kai is authenticated for admin API
    When the admin PATCHes /api/v1/admin/config with matrix_user_id_claim "preferred_username"
    Then the response status is 200
