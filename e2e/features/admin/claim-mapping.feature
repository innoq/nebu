Feature: Admin UI — OIDC Claim Mapping Configuration
  As an operator or admin
  I want to configure OIDC claim mapping via the Bootstrap Wizard and the Admin UI settings page
  So that Nebu works with any OIDC provider and existing deployments continue working unchanged

  # Story 11-10 — ATDD RED PHASE.
  # These scenarios are written BEFORE implementation exists and MUST FAIL until:
  #   - Bootstrap Wizard gains a Step 3 "Claim Mapping" form
  #   - /admin/config/claim-mapping GET/POST route is registered and implemented
  #   - Sidebar has a "Claim Mapping" entry in the Settings section
  #   - "Claim mapping updated" is added to the flash allowlist
  #
  # Runner: playwright-bdd (Cucumber-based Playwright runner)
  # Step definitions: e2e/step-definitions/admin/claim-mapping.steps.ts
  #
  # AC coverage:
  #   AC1  — Bootstrap Wizard shows Step 3 with pre-filled defaults
  #   AC9  — Bootstrap Wizard Claim Mapping step E2E
  #   AC10 — Admin UI Claim Mapping settings page E2E

  Background:
    Given the Nebu stack is running

  # ---------------------------------------------------------------------------
  # AC1 + AC9 — Bootstrap Wizard: Step 3 Claim Mapping form with pre-filled defaults
  # ---------------------------------------------------------------------------
  @ac9-bootstrap-claim-mapping
  Scenario: Bootstrap Wizard shows Claim Mapping step 3 with sensible defaults
    Given the Nebu admin UI is accessible at the admin URL
    And no bootstrap has been completed yet
    When the operator navigates to "/admin/"
    Then the browser is redirected to "/admin/bootstrap"
    And the page shows "Bootstrap Setup"
    When the operator fills in "Instance Name" with "test-nebu"
    And the operator clicks "Next"
    Then the progress indicator shows step 2 as active
    When the operator fills in "OIDC Issuer URL" with the Dex issuer URL
    And the operator fills in "OIDC Client ID" with "nebu-admin"
    And the operator fills in "OIDC Client Secret" with the configured client secret
    And the operator clicks "Next"
    Then the progress indicator shows step 3 as active
    And the page shows "Claim Mapping"
    And the claim mapping form has "oidc_user_id_claim" pre-filled with "name"
    And the claim mapping form has "oidc_displayname_claim" pre-filled with "name"
    And the claim mapping form has "oidc_email_claim" pre-filled with "email"
    When the operator clicks "Connect with OIDC"
    Then the browser is redirected to the Dex login page
    When the operator fills in the Dex email field with "kai@example.com"
    And the operator fills in the Dex password field with the admin password
    And the operator clicks the Dex login submit button
    Then the browser is redirected back to "/admin/bootstrap/select-claim"
    When the operator selects the claim value "instance_admin" as the admin group claim
    And the operator clicks "Confirm"
    Then the browser is redirected to "/admin/dashboard"
    And the page shows "Dashboard"
    When the operator navigates to "/admin/config/claim-mapping"
    Then the page shows "Claim Mapping"
    And the claim mapping form has "oidc_user_id_claim" pre-filled with "name"

  # ---------------------------------------------------------------------------
  # AC10 — Admin UI: Claim Mapping settings page can be updated
  # ---------------------------------------------------------------------------
  @ac10-claim-mapping-settings
  Scenario: Admin UI Claim Mapping page renders current values and allows updates
    Given the Nebu admin UI is accessible at the admin URL
    And bootstrap has been completed
    And the operator is logged in as admin
    When the operator navigates to "/admin/config/claim-mapping"
    Then the page shows "Claim Mapping"
    And the claim mapping form has "oidc_user_id_claim" pre-filled with "name"
    And the claim mapping sidebar navigation link is visible

  @ac10-claim-mapping-update
  Scenario: Admin updates oidc_user_id_claim and sees success flash
    Given the Nebu admin UI is accessible at the admin URL
    And bootstrap has been completed
    And the operator is logged in as admin
    When the operator navigates to "/admin/config/claim-mapping"
    And the operator clears the field "oidc_user_id_claim"
    And the operator fills in "oidc_user_id_claim" with "preferred_username"
    And the operator clicks "Save"
    Then the page shows "Claim mapping updated"
    And the claim mapping form has "oidc_user_id_claim" pre-filled with "preferred_username"

  # ---------------------------------------------------------------------------
  # AC8 — Validation: submitting empty claim name shows per-field error in browser
  # ---------------------------------------------------------------------------
  @ac8-claim-mapping-validation
  Scenario: Admin submits empty claim name and sees per-field validation error
    Given the Nebu admin UI is accessible at the admin URL
    And bootstrap has been completed
    And the operator is logged in as admin
    When the operator navigates to "/admin/config/claim-mapping"
    And the operator clears the field "oidc_user_id_claim"
    And the operator clicks "Save"
    Then the page shows a validation error for "oidc_user_id_claim"
    And the page does not show "Claim mapping updated"
