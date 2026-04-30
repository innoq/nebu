Feature: Tags API — GET/PUT/DELETE /user/{userId}/rooms/{roomId}/tags
  As an end-user
  I want to tag rooms (e.g. as favourite or low priority) and have those tags persist across sessions
  So that my Matrix client can organise rooms into custom categories

  # Story 7-25 — ATDD: tests written FIRST (red phase), before implementation.
  # All scenarios below MUST FAIL until the Tags API handlers are implemented
  # (routes not yet registered in main.go → 404 for all requests).

  Background:
    Given the docker compose stack is started
    And kai is authenticated via OIDC
    And alex is authenticated via OIDC
    And kai creates a room named "tags-test-room"

  # AC1 — GET /tags returns 200 with {"tags":{}} when no tags have been set (never 404)
  Scenario: GetTags_EmptyTags_ForNewRoom — no tags set returns empty object
    When kai calls GET /user/{userId}/rooms/{roomId}/tags
    Then the response status is 200
    And the response body contains "tags"
    And the tags object is empty

  # AC2 — PUT sets a tag; subsequent GET reflects the tag
  Scenario: PutTag_SetFavourite_ReflectedInGet — PUT sets tag and GET returns it
    When kai calls PUT /user/{userId}/rooms/{roomId}/tags/m.favourite with body {"order":0.5}
    Then the response status is 200
    And the response body is "{}"
    When kai calls GET /user/{userId}/rooms/{roomId}/tags
    Then the response status is 200
    And the tags object contains "m.favourite"

  # AC3 — DELETE removes a tag idempotently
  Scenario: DeleteTag_Idempotent_RemovesTag — DELETE is idempotent
    Given kai has set tag "m.favourite" on the room
    When kai calls DELETE /user/{userId}/rooms/{roomId}/tags/m.favourite
    Then the response status is 200
    And the response body is "{}"
    When kai calls DELETE /user/{userId}/rooms/{roomId}/tags/m.favourite
    Then the response status is 200
    When kai calls GET /user/{userId}/rooms/{roomId}/tags
    Then the response status is 200
    And the tags object is empty

  # AC6 — userId mismatch returns 403 M_FORBIDDEN
  Scenario: PutTag_UserIdMismatch_Forbidden — cannot set tags for another user
    When alex calls PUT /user/kai-userId/rooms/{roomId}/tags/m.favourite with body {}
    Then the response status is 403
    And the response body contains "M_FORBIDDEN"

  # JWT required — request without token is rejected
  Scenario: GetTags_Unauthenticated — request without JWT is rejected
    When an unauthenticated client calls GET /user/{userId}/rooms/{roomId}/tags
    Then the response status is 401

  # Story 7-36: P1 gap closure — 7-25-AC5 tag sync propagation
  Scenario: TagSync_AfterPut_AppearsinSync — tag PUT appears as m.tag event in incremental sync
    Given kai is authenticated via OIDC
    And kai creates a room named "tag-sync-test-room"
    And kai captures a sync token before tag change
    When kai puts tag "m.favourite" with body {"order":0.5} for the created room
    And kai calls incremental sync with the captured token
    Then the incremental sync contains account_data event of type "m.tag" for the room
