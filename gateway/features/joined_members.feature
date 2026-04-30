Feature: Joined Members API — GET /rooms/{roomId}/joined_members
  As an end-user
  I want to retrieve a compact map of all currently joined members of a room
  So that my Matrix client can efficiently render participant lists and avatar rows

  # Story 7-20 — ATDD: tests written FIRST (red phase), before implementation.
  # All scenarios below MUST FAIL until GetJoinedMembersHandler is implemented
  # (route not yet registered in main.go → 404 for all requests).

  Background:
    Given the docker compose stack is started
    And kai is authenticated via OIDC
    And alex is authenticated via OIDC
    And kai sets his displayname to "Kai"
    And kai creates a room named "joined-members-test-room"
    And kai invites alex to the room
    And alex joins the room

  # AC1 + AC2 — GET /joined_members returns 200 with compact map of joined users
  Scenario: GetJoinedMembers_ReturnsCompactMap — member retrieves compact joined members map
    When kai calls GET /rooms/{roomId}/joined_members
    Then the response status is 200
    And the response body is a JSON object
    And the response body contains "joined"
    And the joined map contains the key for kai
    And the joined map contains the key for alex
    And the joined map entry for kai has a display_name field

  # AC3 — Non-member gets 403 M_FORBIDDEN
  Scenario: GetJoinedMembers_Forbidden_NonMember — non-member is denied access
    Given marie is authenticated via OIDC
    When marie calls GET /rooms/{roomId}/joined_members
    Then the response status is 403
    And the response body contains "M_FORBIDDEN"

  # AC4 — Unknown room returns 404 M_NOT_FOUND
  Scenario: GetJoinedMembers_NotFound_UnknownRoom — unknown room returns 404
    When kai calls GET /rooms/!doesnotexist:server/joined_members
    Then the response status is 404
    And the response body contains "M_NOT_FOUND"

  # AC6 — JWT required; request without token is rejected
  Scenario: GetJoinedMembers_Unauthenticated — request without JWT is rejected
    When an unauthenticated client calls GET /rooms/{roomId}/joined_members
    Then the response status is 401
