Feature: Admin UI — Bootstrap Wizard Step 4 User Import
  As an instance admin
  I want to import OIDC users during the Bootstrap Wizard Step 4
  So that I can pre-provision all users without leaving the wizard

  # Story 14-3b — AC5 Playwright+Gherkin acceptance tests
  #
  # RED PHASE: Step definitions not yet wired to implementation.
  # Scenarios pass after Step 4 UI is implemented in bootstrap.go + bootstrap.html.

  Background:
    Given the Nebu stack is running
    And bootstrap has been completed
    And the operator is logged in as admin

  @ac5-wizard-step4-displayed
  Scenario: wizard step 4 displayed
    When the operator navigates to "/admin/bootstrap?step=4"
    Then the page shows "User Import"
    And the page contains an "Import from OIDC" button

  @ac5-preview-table-loaded
  Scenario: preview table loaded after clicking Import from OIDC
    When the operator navigates to "/admin/bootstrap?step=4"
    And the "Import from OIDC" button is enabled
    And the operator clicks "Import from OIDC"
    Then a preview table is displayed with at least one user row

  @ac5-import-button-clicked
  Scenario: import button clicked and result counts displayed
    When the operator navigates to "/admin/bootstrap?step=4"
    And the "Import from OIDC" button is enabled
    And the operator clicks "Import from OIDC"
    Then a preview table is displayed with at least one user row
    When the operator clicks "Import all"
    Then the import result section is displayed with imported count
