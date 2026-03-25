Feature: Stack Health Smoke Test
  As an operator
  I want to verify the full stack starts and is healthy
  So that CI has a definitive green signal the deployment works

  @pending
  Scenario: Full stack health check passes
    Given the docker compose stack is started
    When I call GET /health on the gateway
    Then the response status is 200
    And the response body contains "UP"
    When I call GET /ready on the gateway
    Then the response status is 200
    And the response body contains "READY"
    When I call GET :4000/health on the core
    Then the response status is 200
    And the response body contains "UP"
