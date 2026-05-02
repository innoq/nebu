Feature: Event Context API — GET /rooms/{roomId}/context/{eventId}
  As an end-user
  I want to load the surrounding events around a specific message
  So that my Matrix client can display the message in its conversational context without fetching the entire room history

  # Story 7-28 — ATDD: tests written FIRST (red phase), before implementation.
  # All scenarios below MUST FAIL until GetEventContextHandler is implemented
  # (route not yet registered in main.go → 404 for all requests).

  Background:
    Given the docker compose stack is started
    And kai is authenticated via OIDC
    And alex is authenticated via OIDC
    And kai creates a room named "context-test-room"
    And kai invites alex to the room
    And alex joins the room

  # AC1, AC2 — Returns 200 with event, events_before, events_after, start, end, state
  Scenario: GetEventContext_HappyPath — member retrieves context for a known event
    Given kai has sent a message in the room
    When kai calls GET /rooms/{roomId}/context/{eventId}?limit=3
    Then the response status is 200
    And the response body is a JSON object
    And the context response contains the target event
    And the context response contains events_before array
    And the context response contains events_after array
    And the context response contains start token
    And the context response contains end token
    And the context response contains state array

  # AC3 — Unknown eventId returns 404 M_NOT_FOUND
  Scenario: GetEventContext_NotFound — unknown eventId returns 404
    When kai calls GET /rooms/{roomId}/context/$nonexistent_event_id_7_28
    Then the response status is 404
    And the response body contains "M_NOT_FOUND"

  # AC4 — Non-member gets 403 M_FORBIDDEN
  Scenario: GetEventContext_Forbidden — non-member is denied access
    Given kai has sent a message in the room
    And marie is authenticated via OIDC
    When marie calls GET /rooms/{roomId}/context/{eventId}
    Then the response status is 403
    And the response body contains "M_FORBIDDEN"

  # AC4 — JWT required; request without token is rejected
  Scenario: GetEventContext_Unauthenticated — request without JWT is rejected
    Given kai has sent a message in the room
    When an unauthenticated client calls GET /rooms/{roomId}/context/{eventId}
    Then the response status is 401
