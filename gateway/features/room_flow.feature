Feature: Room Flow — Create, Send, Receive
  As a developer
  I want to verify the full chat room lifecycle
  So that CI catches any regression in the core messaging path

  Scenario: User creates a room, sends a message, and another user receives it
    Given the docker compose stack is started
    And kai is authenticated via OIDC
    And alex is authenticated via OIDC
    When kai creates a room named "test-room"
    And kai invites alex to the room
    And alex joins the room
    And kai sends the message "hello" to the room
    Then the response status is 200
    And the response body contains "event_id"
    When alex retrieves messages from the room
    Then the response status is 200
    And the response body contains "hello"

  Scenario: Sending the same txnId twice returns the same event_id
    Given the docker compose stack is started
    And kai is authenticated via OIDC
    When kai creates a room named "idempotency-test"
    And kai sends the message "idempotent" to the room
    Then the response status is 200
    And kai sends the same message again with the same txnId
    Then both sends returned the same event_id
    When kai retrieves messages from the room
    Then the response body contains "idempotent" exactly once
