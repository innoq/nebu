Feature: Admin UI — Bootstrap Wizard Step 4 SCIM Import Progress
  As an instance admin
  I want to see a live progress bar during SCIM user import in Bootstrap Wizard Step 4
  So that I know the import is running and how many users have been processed

  # Story 14-3c — AC3 Playwright+Gherkin acceptance tests
  #
  # RED PHASE: Step definitions are not yet wired to implementation.
  # Scenarios pass after SCIM progress bar + polling JS are implemented
  # in bootstrap.go + bootstrap.html (Step 4).

  Background:
    Given the Nebu stack is running
    And bootstrap has been completed
    And the operator is logged in as admin
    And SCIM integration is enabled in server config

  @ac3-progress-bar-visible
  Scenario: progress bar is shown when SCIM import starts
    When the operator navigates to "/admin/bootstrap?step=4"
    Then the page shows "User Import"
    And the "Import from SCIM" button is visible and enabled
    When the operator clicks "Import from SCIM"
    Then a progress bar element is visible on the page
    And the progress bar shows imported and total user counts

  @ac3-progress-polling
  Scenario: progress bar updates via polling during import
    When the operator navigates to "/admin/bootstrap?step=4"
    And the "Import from SCIM" button is visible and enabled
    When the operator clicks "Import from SCIM"
    Then the page polls "/api/v1/admin/bootstrap/import-status" for progress
    And the import count display updates with live numbers

  @ac3-import-status-auth
  Scenario: import-status endpoint requires admin authentication
    Given the operator is not logged in
    When the operator sends GET "/api/v1/admin/bootstrap/import-status"
    Then the response status is 401

  @ac5-scim-bearer-token-not-exposed
  Scenario: SCIM bearer token does not appear in config page response
    When the operator navigates to "/admin/config"
    Then the page does not contain the raw SCIM bearer token value
    And the page shows a masked token indicator if SCIM is configured
