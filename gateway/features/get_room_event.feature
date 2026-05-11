Feature: GET /rooms/{roomId}/event/{eventId}
  As an Element Web client
  I want to fetch a single event by its event_id
  So that I can load thread roots and reconstruct thread views

  # Story 11-8 — ATDD: tests written FIRST (red phase), before implementation.
  # Bug: GET /_matrix/client/v3/rooms/{roomId}/event/{eventId} returns 404
  # because the endpoint was never registered in the gateway.
  # Element calls this endpoint from thread.ts fetchRootEvent() to load the
  # thread root event when rendering the thread panel.
  #
  # All scenarios MUST FAIL until:
  #   1. The /event/{eventId} route is registered in main.go
  #   2. The GetEvent gRPC RPC is implemented in Core

  Background:
    Given the docker compose stack is started
    And kai is authenticated via OIDC
    And alex is authenticated via OIDC
    And kai creates a room named "event-fetch-test-room"
    And kai invites alex to the room
    And alex joins the room

  # AC1 — Primary fix: fetch the event by its ID, returns 200 with event fields
  Scenario: GetRoomEvent_HappyPath — returns the target event as a Matrix event object
    Given kai has sent a message in the room
    When kai calls GET /rooms/{roomId}/event/{eventId}
    Then the response status is 200
    And the response contains field "event_id"
    And the response contains field "room_id"
    And the response contains field "sender"
    And the response contains field "type"
    And the response contains field "content"
    And the response contains field "origin_server_ts"

  # AC2 — Unknown event returns 404
  Scenario: GetRoomEvent_NotFound — unknown event_id returns 404
    When kai calls GET /rooms/{roomId}/event/$unknown_evt_11_8
    Then the response status is 404
    And the response body contains "M_NOT_FOUND"

  # AC3 — Non-member is denied access
  Scenario: GetRoomEvent_Forbidden — non-member cannot fetch event
    Given kai has sent a message in the room
    And marie is authenticated via OIDC
    When marie calls GET /rooms/{roomId}/event/{eventId}
    Then the response status is 403
    And the response body contains "M_FORBIDDEN"

  # AC4 — Unauthenticated request is rejected
  Scenario: GetRoomEvent_Unauthenticated — request without token returns 401
    Given kai has sent a message in the room
    When an unauthenticated client calls GET /rooms/{roomId}/event/{eventId}
    Then the response status is 401
    And the response body contains "M_MISSING_TOKEN"
