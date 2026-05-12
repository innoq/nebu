Feature: Admin UI — Authentication Guard
  As an unauthenticated visitor
  I want to be redirected to the login page when accessing protected admin routes
  So that the Admin UI is not accessible without authentication

  # Story 9-26 — Phase 3, AC12.
  # Extracted from e2e/features/admin_ui.feature Scenario 3.

  Background:
    Given the Nebu stack is running

  @ac12-auth-guard
  Scenario: Unauthenticated operator is redirected to login
    Given the Nebu admin UI is accessible at the admin URL
    And bootstrap has been completed
    When the operator navigates to "/admin/dashboard"
    Then the browser is redirected to "/admin/login"
    And the page shows "Sign in"
