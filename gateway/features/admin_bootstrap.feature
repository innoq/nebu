Feature: Admin Bootstrap and Dashboard Flow
  As an operator
  I want to verify the bootstrap wizard and dashboard access flow
  So that CI catches any regression in the admin setup path

  Scenario: Bootstrap Wizard step 1 renders correctly
    Given the server has no bootstrap_completed in server_config
    When I request GET /admin/bootstrap without a session cookie
    Then the response is 200
    And the response body contains "Bootstrap"
    And the response body contains "Instance Name"

  Scenario: Bootstrap Wizard step 2 redirects to OIDC login after valid OIDC config
    Given the server has no bootstrap_completed in server_config
    And I have seeded instance_name "test-nebu" into the bootstrap draft
    When I POST step 2 with valid OIDC config to /admin/bootstrap
    Then the response redirects to "/admin/login/start?mode=bootstrap"

  Scenario: Bootstrap completes and dashboard is accessible
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

  Scenario: Unauthenticated request to root is redirected to login when bootstrap complete
    Given bootstrap is complete and server_config is seeded
    When I request GET /admin/ without a session cookie
    Then the response redirects to "/admin/dashboard"
