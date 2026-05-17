Feature: Admin UI — OIDC Directory User Search Integration
  As an instance admin
  I want the user search to include OIDC-directory users who have never logged into Nebu
  So that I can find and preview any provisioned user before they first log in

  # Story 14-2c — ATDD RED PHASE
  # These scenarios are written BEFORE implementation and MUST FAIL until:
  #   - UsersHandler.ListHandler merges OIDC directory results with Nebu DB
  #   - OIDC-only users render with a "Not yet logged in" badge
  #   - The computed Matrix User ID preview is shown for OIDC-only users
  #   - OIDC unavailability shows a non-blocking warning banner
  #
  # Runner: playwright-bdd (Cucumber-based Playwright runner)
  # Step definitions: e2e/step-definitions/admin/oidc-directory-search.steps.ts
  #
  # AC coverage:
  #   AC1 — OIDC-only user appears with "Not yet logged in" badge + Matrix ID preview
  #   AC3 — OIDC provider unavailable: warning banner shown, DB users still visible
  #   AC4 — This .feature file IS the E2E scenario requirement

  Background:
    Given the Nebu stack is running
    And bootstrap is already completed

  # ---------------------------------------------------------------------------
  # AC1 — OIDC-only user appears with "Not yet logged in" badge
  # ---------------------------------------------------------------------------
  @ac1-oidc-only-user-badge
  Scenario: search finds OIDC-only user with Not yet logged in badge
    Given OIDC directory integration is enabled in the server config
    And the OIDC directory contains a user with display name "Diana OIDC" and sub "diana.oidc"
    And "diana.oidc" has never logged into Nebu
    And an admin is logged into the Admin UI
    When the admin navigates to "/admin/users"
    And the admin searches for "diana"
    Then "Diana OIDC" appears in the search results
    And the user row for "Diana OIDC" shows a "Not yet logged in" badge
    And the user row for "Diana OIDC" shows a Matrix User ID preview containing "@diana.oidc:"

  # ---------------------------------------------------------------------------
  # AC3 — OIDC provider unavailable: warning banner shown, DB users still visible
  # ---------------------------------------------------------------------------
  @ac3-oidc-unavailable-warning
  Scenario: search shows warning banner when OIDC directory is unavailable
    Given OIDC directory integration is enabled in the server config
    And the OIDC directory endpoint is unreachable
    And an admin is logged into the Admin UI
    When the admin navigates to "/admin/users"
    Then a warning banner containing "OIDC directory temporarily unavailable" is visible
    And the existing Nebu DB users are still shown in the list
