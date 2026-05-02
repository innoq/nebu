Feature: Room State API — GET /rooms/{roomId}/state
  As an end-user
  I want to retrieve the current state of a room
  So that my Matrix client can display room metadata, power levels, and membership correctly

  # Story 7-19 — ATDD: tests written FIRST (red phase), before implementation.
  # All scenarios below MUST FAIL until GetRoomStateHandler is implemented
  # (routes not yet registered in main.go → 404 for all requests).

  Background:
    Given the docker compose stack is started
    And kai is authenticated via OIDC
    And alex is authenticated via OIDC
    And kai creates a room named "state-test-room"
    And kai invites alex to the room
    And alex joins the room

  # AC1 — GET /state returns 200 JSON array with all current state events
  Scenario: GetRoomState_AllEvents — member retrieves all room state
    When kai calls GET /rooms/{roomId}/state
    Then the response status is 200
    And the response body is a JSON array
    And each element in the response array has keys "type", "state_key", "content" and "sender"

  # AC2 — GET /state/{eventType}/{stateKey} returns 200 with content object only
  Scenario: GetRoomState_SingleEvent_WithStateKey — member retrieves a single state event by type and state_key
    When kai calls GET /rooms/{roomId}/state/m.room.member/{kaiUserId}
    Then the response status is 200
    And the response body is a JSON object
    And the response body contains "membership"

  # AC3 — GET /state/{eventType} (no stateKey) is equivalent to stateKey ""
  Scenario: GetRoomState_SingleEvent_EmptyStateKey — member retrieves state event with empty state_key
    When kai calls GET /rooms/{roomId}/state/m.room.name
    Then the response status is 200
    And the response body is a JSON object

  # AC4 — Non-member gets 403 M_FORBIDDEN
  Scenario: GetRoomState_Forbidden_NonMember — non-member is denied access
    Given marie is authenticated via OIDC
    When marie calls GET /rooms/{roomId}/state
    Then the response status is 403
    And the response body contains "M_FORBIDDEN"

  # AC5 — Unknown room returns 404 M_NOT_FOUND
  Scenario: GetRoomState_NotFound_UnknownRoom — unknown room returns 404
    When kai calls GET /rooms/!nonexistent:server/state
    Then the response status is 404
    And the response body contains "M_NOT_FOUND"

  # AC6 — Known room, unknown event type returns 404 M_NOT_FOUND
  Scenario: GetRoomState_NotFound_UnknownEventType — missing event type/state_key returns 404
    When kai calls GET /rooms/{roomId}/state/m.room.nonexistent/
    Then the response status is 404
    And the response body contains "M_NOT_FOUND"
