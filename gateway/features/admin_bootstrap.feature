Feature: Admin Bootstrap and Dashboard Flow
  As an operator
  I want to verify the bootstrap wizard and dashboard access flow
  So that CI catches any regression in the admin setup path

  Scenario: Bootstrap Wizard completes successfully
    Given the server has no bootstrap_completed in server_config
    When I request GET /admin/dashboard without a session cookie
    Then the response redirects to "/admin/login"
    When I request GET /admin/bootstrap without a session cookie
    Then the response is 200
    And the response body contains "Bootstrap"
    When I seed the bootstrap configuration directly into the database
    Then server_config contains key "bootstrap_completed" with value "true"
    When I request GET /admin/bootstrap without a session cookie
    Then the response redirects to "/admin/login"

  Scenario: Dashboard accessible after authentication
    Given bootstrap is complete and server_config is seeded
    And I have a forged valid admin session cookie
    When I request GET /admin/dashboard with the admin session cookie
    Then the response is 200
    And the response body contains "Dashboard"
    And the response body contains "status-card--green"

  Scenario: Unauthenticated dashboard request is redirected
    Given bootstrap is complete and server_config is seeded
    When I request GET /admin/dashboard without a session cookie
    Then the response redirects to "/admin/login"
