Feature: Thread Message Relations — GET /rooms/{roomId}/relations/{eventId}[/{relType}[/{eventType}]]
  As an Element Web client
  I want to retrieve relation events for a given parent event
  So that thread panels, reply lists, and annotation counts are correctly populated

  # Story 9-29 — ATDD: tests written FIRST (red phase), before implementation.
  #
  # Bug: Element Web sends GET /_matrix/client/v1/rooms/{roomId}/relations/{eventId}
  # (no relType) and receives 404 because only the /{relType} variant is registered.
  #
  # All three URL variants MUST be registered per Matrix CS API v1:
  #   1. /relations/{eventId}                        ← MISSING — causes the 404
  #   2. /relations/{eventId}/{relType}               ← exists but incomplete
  #   3. /relations/{eventId}/{relType}/{eventType}   ← MISSING
  #
  # All scenarios MUST FAIL until:
  #   1. The base /relations/{eventId} route is registered (fixes Element Web 404)
  #   2. dir=b / dir=f ordering is respected
  #   3. recurse=true is accepted without 400/500
  #   4. The /{relType}/{eventType} variant is registered
  #   5. All required error codes (401/403/404/400) are returned with correct errcodes

  Background:
    Given the docker compose stack is started
    And kai is authenticated via OIDC
    And alex is authenticated via OIDC
    And kai creates a room named "thread-test-room-9-29"
    And kai invites alex to the room
    And alex joins the room

  # AC1 — Base route (no relType): fixes the Element Web 404
  Scenario: MessagesThread_BaseRoute — base /relations/{eventId} returns all relation events
    Given kai has sent a message in the room
    And alex sends a thread reply to kai's message
    When alex calls GET /relations/{eventId} without relType
    Then the response status is 200
    And the relations response chunk contains the thread reply event
    And the relations response has a "chunk" array

  # AC2 — dir=b (newest-first, default): explicit dir=b returns most-recent reply first
  Scenario: MessagesThread_DirB — /relations with dir=b returns events newest-first
    Given kai has sent a message in the room
    And alex sends a thread reply to kai's message
    And alex sends a second thread reply to kai's message
    When alex calls GET /relations/{eventId}/m.thread with query "dir=b"
    Then the response status is 200
    And the relations response has a "chunk" array
    And the first chunk event is the most recent reply

  # AC3 — dir=f (oldest-first): explicit dir=f returns oldest reply first
  Scenario: MessagesThread_DirF — /relations with dir=f returns events oldest-first
    Given kai has sent a message in the room
    And alex sends a thread reply to kai's message
    And alex sends a second thread reply to kai's message
    When alex calls GET /relations/{eventId}/m.thread with query "dir=f"
    Then the response status is 200
    And the relations response has a "chunk" array
    And the first chunk event is the oldest reply

  # AC4 — recurse=true: must be accepted without 400/500
  Scenario: MessagesThread_RecurseTrue — recurse=true is accepted
    Given kai has sent a message in the room
    And alex sends a thread reply to kai's message
    When alex calls GET /relations/{eventId}/m.thread with query "recurse=true"
    Then the response status is 200
    And the relations response has a "chunk" array

  # AC5 — Three-segment route: /relations/{eventId}/{relType}/{eventType}
  Scenario: MessagesThread_ThreeSegmentRoute — /relations/{eventId}/{relType}/{eventType} filters by both
    Given kai has sent a message in the room
    And alex sends a thread reply to kai's message
    When alex calls GET /relations/{eventId}/m.thread/m.room.message
    Then the response status is 200
    And the relations response has a "chunk" array
    And the relations response chunk contains the thread reply event

  # AC6 — 403 M_FORBIDDEN for non-member
  Scenario: MessagesThread_Forbidden — non-member receives 403 M_FORBIDDEN
    Given kai has sent a message in the room
    And alex sends a thread reply to kai's message
    And marie is authenticated via OIDC
    When marie calls GET /relations/{eventId} without relType
    Then the response status is 403
    And the response body has errcode "M_FORBIDDEN"

  # AC7 — 401 M_MISSING_TOKEN for unauthenticated request
  Scenario: MessagesThread_Unauthenticated — request without token receives 401 M_MISSING_TOKEN
    Given kai has sent a message in the room
    When an unauthenticated client calls GET /relations/{eventId} without relType
    Then the response status is 401
    And the response body has errcode "M_MISSING_TOKEN"

  # AC8 — 404 M_NOT_FOUND for unknown eventId
  Scenario: MessagesThread_NotFound — unknown eventId returns 404 M_NOT_FOUND
    When alex calls GET /relations/$unknown_event_9_29 without relType
    Then the response status is 404
    And the response body has errcode "M_NOT_FOUND"

  # AC9 — 400 M_BAD_PARAM for invalid dir value
  Scenario: MessagesThread_BadParam — invalid dir value returns 400 M_BAD_PARAM
    Given kai has sent a message in the room
    When alex calls GET /relations/{eventId}/m.thread with query "dir=invalid"
    Then the response status is 400
    And the response body has errcode "M_BAD_PARAM"
