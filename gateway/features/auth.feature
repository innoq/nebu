Feature: Authentication — End-to-End OIDC Login
  As an operator
  I want to verify the complete OIDC login and logout flow
  So that CI catches any auth regression before production

  Scenario: OIDC login and logout via Dex
    Given the docker compose stack is started
    When I call GET /_matrix/client/v3/login on the matrix API
    Then the response status is 200
    And the response body contains "m.login.sso"
    When I obtain a Dex token for "kai@example.com" with password "changeme"
    And I POST /_matrix/client/v3/login with the Dex token
    Then the response status is 200
    And the response body contains "access_token"
    And the response body contains "CiQwMDAwMDAwMC0wMDAwLTAwMDAtMDAwMC0wMDAwMDAwMDAwMDE"
    When I POST /_matrix/client/v3/logout with the access token
    Then the response status is 200
    When I POST /_matrix/client/v3/logout with the access token
    Then the response status is 401
