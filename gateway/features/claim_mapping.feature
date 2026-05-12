Feature: OIDC Claim Mapping Configuration
  As an admin or operator
  I want to configure OIDC claim mapping via the Admin UI and see it applied to Matrix login
  So that Nebu works with any OIDC provider without hardcoding claim names

  # Story 11-10 — ATDD RED PHASE.
  # These scenarios are written BEFORE implementation exists and MUST FAIL until:
  #   - GET /admin/config/claim-mapping is registered and implemented
  #   - POST /admin/config/claim-mapping is registered and implemented
  #   - FormatUserIDFromClaims accepts claimName + claims map
  #   - server_config seeding via migration 000044 exists
  #   - JWTMiddleware and LoginHandler use DB-loaded oidc_user_id_claim
  #
  # Auth for admin UI scenarios: forged admin session cookie (integration test pattern
  # consistent with admin_bootstrap.feature).
  # Auth for Matrix login scenarios: real JWT token issued by the test Dex instance.

  Background:
    Given the server is running with a clean test database

  # ---------------------------------------------------------------------------
  # AC3 — Admin UI: GET /admin/config/claim-mapping shows defaults when keys absent
  # ---------------------------------------------------------------------------
  Scenario: Admin navigates to Claim Mapping page and sees default values
    Given bootstrap is complete and server_config is seeded
    And I have a forged valid admin session cookie
    When I request GET /admin/config/claim-mapping with the admin session cookie
    Then the response is 200
    And the response body contains "Claim Mapping"
    And the response body contains "oidc_user_id_claim"
    And the response body contains "oidc_displayname_claim"
    And the response body contains "oidc_email_claim"
    And the response body contains "sub"
    And the response body contains "name"
    And the response body contains "email"

  # ---------------------------------------------------------------------------
  # AC3 — Admin UI: POST valid values → PRG redirect with flash
  # ---------------------------------------------------------------------------
  Scenario: Admin submits valid claim mapping values and sees success flash
    Given bootstrap is complete and server_config is seeded
    And I have a forged valid admin session cookie
    When I POST to /admin/config/claim-mapping with the admin session cookie and valid form:
      | field                  | value              |
      | oidc_user_id_claim     | preferred_username |
      | oidc_displayname_claim | name               |
      | oidc_email_claim       | email              |
    Then the response redirects to "/admin/config/claim-mapping"
    And the redirect location contains "flash="
    When I follow the redirect with the admin session cookie
    Then the response is 200
    And the response body contains "Claim mapping updated"

  # ---------------------------------------------------------------------------
  # AC8 — Validation: empty claim name returns 422 with field error
  # ---------------------------------------------------------------------------
  Scenario: Admin submits empty oidc_user_id_claim and receives validation error
    Given bootstrap is complete and server_config is seeded
    And I have a forged valid admin session cookie
    When I POST to /admin/config/claim-mapping with the admin session cookie and invalid form:
      | field                  | value |
      | oidc_user_id_claim     |       |
      | oidc_displayname_claim | name  |
      | oidc_email_claim       | email |
    Then the response is 422
    And the response body contains "oidc_user_id_claim"

  # ---------------------------------------------------------------------------
  # AC8 — Validation: claim name with illegal character returns 422
  # ---------------------------------------------------------------------------
  Scenario: Admin submits claim name with space character and receives validation error
    Given bootstrap is complete and server_config is seeded
    And I have a forged valid admin session cookie
    When I POST to /admin/config/claim-mapping with the admin session cookie and invalid form:
      | field                  | value    |
      | oidc_user_id_claim     | my claim |
      | oidc_displayname_claim | name     |
      | oidc_email_claim       | email    |
    Then the response is 422
    And the response body contains "oidc_user_id_claim"

  # ---------------------------------------------------------------------------
  # AC5 — Matrix login uses DB-loaded oidc_user_id_claim=preferred_username
  # Design requirement: gateway reads oidc_user_id_claim from server_config on
  # each login request (no restart required). This is a testability constraint —
  # the implementation MUST NOT cache only at startup.
  # ---------------------------------------------------------------------------
  Scenario: User logs in when oidc_user_id_claim is configured as preferred_username
    Given bootstrap is complete and server_config is seeded
    And server_config contains "oidc_user_id_claim" = "preferred_username"
    When a user authenticates via Matrix login with a Dex token containing preferred_username "alex"
    Then the Matrix login response is 200
    And the returned user_id contains "alex"

  # ---------------------------------------------------------------------------
  # AC7 — Backward compatibility: missing oidc_user_id_claim falls back to name claim
  # Design requirement: fallback resolved per-request from DB (no restart required).
  # ---------------------------------------------------------------------------
  Scenario: Gateway falls back to name-claim behavior when oidc_user_id_claim is absent from server_config
    Given bootstrap is complete and server_config is seeded
    And server_config does not contain "oidc_user_id_claim"
    When a user authenticates via Matrix login with a Dex token containing name "alex"
    Then the Matrix login response is 200
    And the returned user_id contains "alex"
