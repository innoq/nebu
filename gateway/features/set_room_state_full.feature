Feature: Room State Event Full Implementation — PUT /rooms/{roomId}/state/{eventType}
  As a Matrix client user
  I want to set and retrieve room state events for all standard types
  So that room name, topic, join rules, and other state persists correctly

  # Story 9-7 — ATDD: tests written FIRST (red phase), before implementation.
  # All scenarios below MUST FAIL until:
  #   - SetRoomStateCoreClient.SendEvent is added to the interface
  #   - PutSetRoomState replaces the 501 fallback with Core.SendEvent delegation
  #   - proto/core.proto gains state_key field 7 in SendEventRequest
  #   - Elixir server.ex extracts state_key from request
  #   - Elixir Room.Server.send_event/6 includes state_key in event_map

  Background:
    Given the docker compose stack is started
    And kai is authenticated via OIDC
    And kai creates a room named "state-impl-test-room"

  # AC1 — m.room.name state event is stored and retrievable
  # Before Story 9-7: PUT returns 501 Not Implemented.
  # After implementation: PUT returns 200 and GET returns the stored name.
  Scenario: SetRoomName_Persisted — m.room.name state event is stored and retrievable
    When kai sends PUT /rooms/{roomId}/state/m.room.name with body {"name":"Test Room 9-7"}
    Then the response status is 200
    And the response body contains "event_id"
    When kai calls GET /rooms/{roomId}/state/m.room.name
    Then the response status is 200
    And the response body contains "Test Room 9-7"

  # AC1 variant — m.room.topic state event is stored and retrievable
  Scenario: SetRoomTopic_Persisted — m.room.topic state event is stored and retrievable
    When kai sends PUT /rooms/{roomId}/state/m.room.topic with body {"topic":"Welcome to the room"}
    Then the response status is 200
    When kai calls GET /rooms/{roomId}/state/m.room.topic
    Then the response status is 200
    And the response body contains "Welcome to the room"

  # AC2 — m.room.encryption returns 200 not 501 (Matrix spec Section 11.2.1 pass-through)
  Scenario: SetEncryption_NotRejectedWith501 — m.room.encryption returns 200 not 501
    When kai sends PUT /rooms/{roomId}/state/m.room.encryption with body {"algorithm":"m.megolm.v1.aes-sha2"}
    Then the response status is 200
    And the response body contains "event_id"

  # AC3 — join_rules state event is reflected in GET /sync room state
  Scenario: SetJoinRules_ReflectedInSync — join_rules PUT is visible in subsequent GET /sync
    Given kai captures a sync token before join_rules change
    When kai sends PUT /rooms/{roomId}/state/m.room.join_rules with body {"join_rule":"invite"}
    Then the response status is 200
    When kai calls incremental sync with the captured token for state check
    Then the sync response contains join_rules "invite" in the room state

  # AC4 — non-member cannot set state events (403 M_FORBIDDEN)
  Scenario: SetRoomName_NonMemberForbidden — non-member receives 403 when setting state
    Given marie is authenticated via OIDC
    When marie sends PUT /rooms/{roomId}/state/m.room.name with body {"name":"Hijacked Name"}
    Then the response status is 403
    And the response body contains "M_FORBIDDEN"

  # AC5 — GET /rooms/{roomId}/state returns all state events including those set via PUT
  Scenario: GetAllRoomState_ContainsPutStateEvents — GET /state returns array including name and topic
    When kai sends PUT /rooms/{roomId}/state/m.room.name with body {"name":"Bulk Test Room"}
    Then the response status is 200
    When kai sends PUT /rooms/{roomId}/state/m.room.topic with body {"topic":"Bulk topic"}
    Then the response status is 200
    When kai calls GET /rooms/{roomId}/state
    Then the response status is 200
    And the response body is a JSON array
    And the response body contains an event with type "m.room.name" and content key "name" equal to "Bulk Test Room"
    And the response body contains an event with type "m.room.topic" and content key "topic" equal to "Bulk topic"
