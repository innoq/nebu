Feature: Profile Sub-field Endpoints — GET /profile/{userId}/displayname + /avatar_url
  As an end-user
  I want to retrieve just the displayname or just the avatar_url of any user's profile
  So that my Matrix client can populate user chips and avatars without fetching the full profile object

  # Story 7-21 — ATDD: tests written FIRST (red phase), before implementation.
  # All scenarios below MUST FAIL until GetDisplayname and GetAvatarURL handlers
  # are implemented and registered in main.go.
  # These endpoints are unauthenticated (no JWT required per Matrix spec).

  Background:
    Given the docker compose stack is started
    And kai is authenticated via OIDC
    And kai sets his displayname to "Kai Wonderland"
    And kai sets his avatar_url to "mxc://server/abc123"

  # AC1 — GET /profile/{userId}/displayname returns 200 with {"displayname": "<value>"}
  Scenario: GetDisplayname_ReturnsValue — displayname sub-field endpoint returns single field
    When an unauthenticated client calls GET /profile/{kaiUserId}/displayname
    Then the response status is 200
    And the response body contains "displayname"
    And the response body contains "Kai Wonderland"
    And the response body does not contain "avatar_url"

  # AC2 — GET /profile/{userId}/avatar_url returns 200 with {"avatar_url": "<value>"}
  Scenario: GetAvatarURL_ReturnsValue — avatar_url sub-field endpoint returns single field
    When an unauthenticated client calls GET /profile/{kaiUserId}/avatar_url
    Then the response status is 200
    And the response body contains "avatar_url"
    And the response body contains "mxc://server/abc123"
    And the response body does not contain "displayname"

  # AC3 — GET /profile/{userId}/displayname returns 404 M_NOT_FOUND for unknown user
  Scenario: GetDisplayname_NotFound — unknown user returns 404 M_NOT_FOUND
    When an unauthenticated client calls GET /profile/@ghost:server/displayname
    Then the response status is 404
    And the response body contains "M_NOT_FOUND"

  # AC4 — Both endpoints are registered without jwtMiddleware (unauthenticated access allowed)
  Scenario: GetDisplayname_NoJWT_Allowed — unauthenticated access is permitted
    When an unauthenticated client calls GET /profile/{kaiUserId}/displayname
    Then the response status is 200

  Scenario: GetAvatarURL_NoJWT_Allowed — unauthenticated access is permitted
    When an unauthenticated client calls GET /profile/{kaiUserId}/avatar_url
    Then the response status is 200
