Feature: Room Aliases API — GET /rooms/{roomId}/aliases
  As an end-user
  I want to retrieve the list of aliases associated with a room
  So that my Matrix client can display canonical room addresses and allow sharing via human-readable names

  # Story 7-23 — ATDD: tests written FIRST (red phase), before implementation.
  # All scenarios below MUST FAIL until GetRoomAliasesHandler is implemented
  # (route not yet registered in main.go → 404 for all requests).

  Background:
    Given the docker compose stack is started
    And kai is authenticated via OIDC
    And alex is authenticated via OIDC
    And kai creates a room named "aliases-test-room"
    And kai invites alex to the room
    And alex joins the room

  # AC1 — GET /aliases returns 200 with {"aliases":[]} for a room member
  Scenario: GetRoomAliases_EmptyArray_ForMember — member retrieves aliases for a room
    When kai calls GET /rooms/{roomId}/aliases
    Then the response status is 200
    And the response body is a JSON object
    And the response body contains "aliases"
    And the aliases array is empty

  # AC2 — Non-member gets 403 M_FORBIDDEN
  Scenario: GetRoomAliases_Forbidden_NonMember — non-member is denied access
    Given marie is authenticated via OIDC
    When marie calls GET /rooms/{roomId}/aliases
    Then the response status is 403
    And the response body contains "M_FORBIDDEN"

  # AC3 — Unknown room returns 404 M_NOT_FOUND
  Scenario: GetRoomAliases_NotFound_UnknownRoom — unknown room returns 404
    When kai calls GET /rooms/!doesnotexist:server/aliases
    Then the response status is 404
    And the response body contains "M_NOT_FOUND"

  # AC4 — JWT required; request without token is rejected
  Scenario: GetRoomAliases_Unauthenticated — request without JWT is rejected
    When an unauthenticated client calls GET /rooms/{roomId}/aliases
    Then the response status is 401
