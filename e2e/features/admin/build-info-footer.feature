Feature: Build info footer in Admin UI
  As a system operator
  I want to see build metadata in a footer on every authenticated admin page
  So that I can verify the deployed version without leaving the browser

  # Story 11-9 — AC9 + AC10

  Background:
    Given the Nebu stack is running

  @ac9-build-info-footer
  Scenario: Build info footer is visible on authenticated admin pages
    Given the Nebu admin UI is accessible at the admin URL
    And bootstrap has been completed
    And the operator is logged in as admin
    When the operator navigates to "/admin/dashboard"
    Then the page contains a build info footer with text "nebu gateway v"

  @ac10-build-info-footer-unknown
  Scenario: Build info footer gracefully shows unknown values when built without ldflags
    Given the Nebu admin UI is accessible at the admin URL
    And bootstrap has been completed
    And the operator is logged in as admin
    When the operator navigates to "/admin/dashboard"
    Then the build info footer shows a non-empty version string

  @ac9-build-info-footer-absent-on-login
  Scenario: Build info footer is absent on the admin login page
    Given the Nebu admin UI is accessible at the admin URL
    And bootstrap has been completed
    When the operator navigates to "/admin/login"
    Then the page does not contain a build info footer
