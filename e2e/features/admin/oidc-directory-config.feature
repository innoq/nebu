Feature: Admin UI — OIDC Directory Integration Config Page
  As an instance admin
  I want the Config page to show an OIDC directory toggle and endpoint field
  So that I can enable and configure the OIDC user directory integration without code changes

  # Story 14-2a — ATDD RED PHASE
  # These scenarios are written BEFORE implementation and MUST FAIL until:
  #   - config.html renders a toggle for oidc_directory_enabled
  #   - config.html renders a text field for oidc_directory_endpoint
  #   - The endpoint field is conditionally visible only when the toggle is enabled
  #   - The form POST saves both fields correctly
  #
  # Runner: playwright-bdd (Cucumber-based Playwright runner)
  # Step definitions: e2e/step-definitions/admin/oidc-directory-config.steps.ts
  #
  # AC coverage:
  #   AC4 — Admin UI Config page shows toggle + conditional endpoint field

  Background:
    Given the Nebu stack is running
    And bootstrap is already completed

  # ---------------------------------------------------------------------------
  # AC4 — Admin UI Config page renders the OIDC directory toggle
  # ---------------------------------------------------------------------------
  @ac4-oidc-directory-toggle
  Scenario: Config page shows OIDC directory toggle
    Given an admin is logged into the Admin UI
    When the admin navigates to "/admin/config"
    Then the page shows an OIDC directory enabled toggle
    And the OIDC directory endpoint field is not visible

  # ---------------------------------------------------------------------------
  # AC4 — Enabling the toggle reveals the endpoint text field
  # ---------------------------------------------------------------------------
  @ac4-oidc-directory-endpoint-visible-when-enabled
  Scenario: Enabling the OIDC directory toggle reveals the endpoint field
    Given an admin is logged into the Admin UI
    When the admin navigates to "/admin/config"
    And the admin enables the OIDC directory toggle
    Then the OIDC directory endpoint field is visible
    And the OIDC directory endpoint field is editable

  # ---------------------------------------------------------------------------
  # AC4 — Saving enabled=true with endpoint persists the values
  # ---------------------------------------------------------------------------
  @ac4-oidc-directory-save
  Scenario: Saving OIDC directory config with enabled=true persists the values
    Given an admin is logged into the Admin UI
    When the admin navigates to "/admin/config"
    And the admin enables the OIDC directory toggle
    And the admin fills in the OIDC directory endpoint with "https://idp.example.com/admin/users"
    And the admin saves the config form
    Then the page shows a success flash message
    When the admin navigates to "/admin/config"
    Then the OIDC directory toggle is enabled
    And the OIDC directory endpoint field shows "https://idp.example.com/admin/users"
