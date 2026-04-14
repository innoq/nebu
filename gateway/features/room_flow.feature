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

  Scenario: Newly joined room appears in incremental sync (GAP-1 fix)
    # ATDD: After a user joins a room, GET /sync?since=<previous_token>
    # must return that room in rooms.join — even if no messages were sent yet.
    # Previously broken: join produced no m.room.member event so delta sync
    # never returned the room → Element Web stuck on "Joining…" spinner.
    Given the docker compose stack is started
    And alex is authenticated via OIDC
    And marie is authenticated via OIDC
    When alex creates a room named "invite-sync-test"
    And alex invites marie to the room
    And marie captures a sync token before joining
    And marie joins the room
    And marie calls incremental sync with the captured token
    Then the incremental sync response contains the room in rooms.join
    And the incremental sync response does not contain the room in rooms.invite

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
