Feature: Thread Relations API — GET /rooms/{roomId}/relations/{eventId}/m.thread
  As an Element Web client
  I want to load thread reply events for a given parent event
  So that the thread panel is populated when the first thread reply arrives

  # Story 9-28 — ATDD: tests written FIRST (red phase), before implementation.
  # Bug: first thread reply triggers a notification on the counterpart's client
  # but the message itself does not appear in the thread panel.
  # Root cause: missing GET /_matrix/client/v1/rooms/{roomId}/relations/{eventId}/m.thread
  # and missing unsigned.m.relations.m.thread bundled aggregations in /sync.
  #
  # All scenarios MUST FAIL until:
  #   1. The /relations endpoint is implemented (handler + gRPC RPC)
  #   2. unsigned.m.relations.m.thread is added to thread-root events in /sync

  Background:
    Given the docker compose stack is started
    And kai is authenticated via OIDC
    And alex is authenticated via OIDC
    And kai creates a room named "thread-test-room"
    And kai invites alex to the room
    And alex joins the room

  # AC1 — Primary bug: thread reply visible via /relations after first thread message
  Scenario: ThreadRelations_HappyPath — first thread reply is retrievable via /relations
    Given kai has sent a message in the room
    And alex sends a thread reply to kai's message
    When alex calls GET /rooms/{roomId}/relations/{eventId}/m.thread
    Then the response status is 200
    And the relations response contains the thread reply event

  # AC2 — Pagination shape: response has chunk array (even when empty)
  Scenario: ThreadRelations_EmptyThread — /relations returns empty chunk for non-thread event
    Given kai has sent a message in the room
    When alex calls GET /rooms/{roomId}/relations/{eventId}/m.thread
    Then the response status is 200
    And the relations response chunk is empty

  # AC3 — Bundled aggregations: parent event in /sync carries unsigned.m.relations.m.thread
  Scenario: ThreadRelations_BundledAggregations — sync includes m.thread aggregation on parent
    Given kai has sent a message in the room
    And alex sends a thread reply to kai's message
    When alex calls GET /sync
    Then the response status is 200
    And the sync response includes m.thread bundled aggregation on the parent event

  # AC4 — Non-member is denied access
  Scenario: ThreadRelations_Forbidden — non-member cannot read thread relations
    Given kai has sent a message in the room
    And alex sends a thread reply to kai's message
    And marie is authenticated via OIDC
    When marie calls GET /rooms/{roomId}/relations/{eventId}/m.thread
    Then the response status is 403
    And the response body contains "M_FORBIDDEN"

  # AC5 — Unauthenticated request is rejected
  Scenario: ThreadRelations_Unauthenticated — request without JWT is rejected
    Given kai has sent a message in the room
    When an unauthenticated client calls GET /rooms/{roomId}/relations/{eventId}/m.thread
    Then the response status is 401
    And the response body contains "M_MISSING_TOKEN"

  # AC6 — Unknown eventId returns 404
  Scenario: ThreadRelations_NotFound — unknown parentEventId returns 404
    When alex calls GET /rooms/{roomId}/relations/$unknown_thread_root_9_28/m.thread
    Then the response status is 404
    And the response body contains "M_NOT_FOUND"
