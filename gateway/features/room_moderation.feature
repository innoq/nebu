Feature: Room Moderation — kick / ban / unban / forget
  As a room moderator or admin
  I want to kick, ban, unban, and forget rooms via the Matrix API
  So that I can manage room membership and keep my room list clean

  # Story 7-22 — ATDD: tests written FIRST (red phase), before implementation.
  # All scenarios below MUST FAIL until the moderation handlers are implemented
  # (routes not yet registered in main.go → 404 for all requests).

  Background:
    Given the docker compose stack is started
    And kai is authenticated via OIDC
    And alex is authenticated via OIDC
    And marie is authenticated via OIDC
    And kai creates a room named "moderation-test-room"
    And kai invites alex to the room
    And alex joins the room
    And kai invites marie to the room
    And marie joins the room

  # AC1 — POST /kick with power level ≥ 50 succeeds
  Scenario: Kick_Success_ModeratorPowerLevel — moderator kicks a joined member
    When kai kicks alex from the room
    Then the response status is 200
    And the response body is "{}"

  # AC1 — POST /kick with insufficient power level returns 403
  Scenario: Kick_Forbidden_InsufficientPowerLevel — non-moderator cannot kick
    When alex kicks marie from the room
    Then the response status is 403
    And the response body contains "M_FORBIDDEN"

  # AC2 — POST /ban with power level ≥ 50 succeeds
  Scenario: Ban_Success — moderator bans a member
    When kai bans alex from the room
    Then the response status is 200
    And the response body is "{}"

  # AC3 — POST /unban with power level ≥ 50 unbans a banned user
  Scenario: Unban_Success — moderator unbans a banned user
    Given kai has banned alex from the room
    When kai unbans alex from the room
    Then the response status is 200
    And the response body is "{}"

  # AC4 — POST /forget succeeds when user has already left
  Scenario: Forget_Success_AfterLeave — user forgets a room after leaving
    Given alex has left the moderation test room
    When alex forgets the moderation test room
    Then the response status is 200
    And the response body is "{}"

  # AC4 — POST /forget returns 403 when user is still joined
  Scenario: Forget_Forbidden_StillJoined — cannot forget a room while still joined
    When kai forgets the moderation test room
    Then the response status is 403
    And the response body contains "M_FORBIDDEN"

  # AC5 — POST /kick returns 400 when user_id is missing
  Scenario: Kick_BadJSON_MissingUserId — kick without user_id returns 400
    When kai posts kick without user_id
    Then the response status is 400
    And the response body contains "M_BAD_JSON"

  # AC6 — POST /ban returns 404 when room does not exist
  Scenario: Ban_NotFound_UnknownRoom — ban in unknown room returns 404
    When kai bans alex from room "!doesnotexist:server"
    Then the response status is 404
    And the response body contains "M_NOT_FOUND"

  # AC7 — JWT required; request without token is rejected
  Scenario: Kick_Unauthenticated — request without JWT is rejected
    When an unauthenticated client kicks alex from the room
    Then the response status is 401
