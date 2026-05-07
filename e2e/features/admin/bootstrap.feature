Feature: Admin UI — Bootstrap Wizard
  As an operator
  I want to complete the Bootstrap Wizard via real OIDC login
  So that the Nebu instance is ready to serve users

  # Story 9-26 — Phase 3, AC12.
  # Extracted from e2e/features/admin_ui.feature Scenario 1 + Scenario 4.
  #
  # RED PHASE: Steps not yet wired to playwright-bdd step definitions.
  # The existing admin_ui.feature is a specification document only — not executable.

  Background:
    Given the Nebu stack is running

  @ac12-bootstrap-wizard
  Scenario: Operator completes Bootstrap Wizard via real OIDC login
    Given the Nebu admin UI is accessible at the admin URL
    And no bootstrap has been completed yet
    When the operator navigates to "/admin/"
    Then the browser is redirected to "/admin/bootstrap"
    And the page shows "Bootstrap Setup"
    And the progress indicator shows step 1 as active
    When the operator fills in "Instance Name" with "test-nebu"
    And the operator clicks "Next"
    Then the progress indicator shows step 2 as active
    When the operator fills in "OIDC Issuer URL" with the Dex issuer URL
    And the operator fills in "OIDC Client ID" with "nebu-admin"
    And the operator fills in "OIDC Client Secret" with the configured client secret
    And the operator clicks "Connect with OIDC"
    Then the browser is redirected to the Dex login page
    When the operator fills in the Dex email field with "kai@example.com"
    And the operator fills in the Dex password field with the admin password
    And the operator clicks the Dex login submit button
    Then the browser is redirected back to "/admin/bootstrap/select-claim"
    And the page shows the discovered OIDC claims
    And the page shows at least one claim value as a selectable option
    When the operator selects the claim value "instance_admin" as the admin group claim
    And the operator clicks "Confirm"
    Then the browser is redirected to "/admin/dashboard"
    And the page shows "Dashboard"
    And the operator is authenticated (no redirect to login)

  @ac12-bootstrap-admin-login
  Scenario: Admin logs in via OIDC and reaches dashboard
    Given the Nebu admin UI is accessible at the admin URL
    And bootstrap has been completed
    When the operator navigates to "/admin/login"
    And the operator clicks "Sign in with SSO"
    Then the browser is redirected to the Dex login page
    When the operator fills in the Dex email field with "kai@example.com"
    And the operator fills in the Dex password field with the admin password
    And the operator clicks the Dex login submit button
    Then the browser is redirected to "/admin/dashboard"
    And the page shows "Dashboard"
