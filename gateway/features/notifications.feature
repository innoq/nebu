Feature: Notifications API — GET /_matrix/client/v3/notifications
  As an end-user
  I want to retrieve my notification history via GET /notifications
  So that my Matrix client can display a badge count, a notification inbox,
  and highlight events that mention me — even after reconnecting

  # Story 7-29 — ATDD: tests written FIRST (red phase), before implementation.
  # All scenarios below MUST FAIL until the Notifications handler is implemented
  # and the route is registered in main.go.

  Background:
    Given the docker compose stack is started
    And kai is authenticated via OIDC

  # AC1 — GET /notifications returns 200 with {"next_token":"...","notifications":[...]}
  # AC5 — empty result: notifications is an empty array, next_token absent or empty
  Scenario: GetNotifications_EmptyResult — no notifications returns empty array
    When kai calls GET /_matrix/client/v3/notifications
    Then the response status is 200
    And the response body contains "notifications"
    And the notifications array is empty
    And the next_token is absent or empty

  # AC1 + AC2 — limit parameter returns first page, next_token present
  Scenario: GetNotifications_ReturnsPagedList — limit=2 returns 2 items and a next_token
    Given kai has 3 notifications in the database
    When kai calls GET /_matrix/client/v3/notifications?limit=2
    Then the response status is 200
    And the notifications array has 2 items
    And each notification item has keys actions, event, read, room_id, ts
    And the next_token is present and non-empty

  # AC2 — from cursor returns second page
  Scenario: GetNotifications_FromCursor_SecondPage — from=TOKEN returns remaining item
    Given kai has 3 notifications in the database
    And kai fetched the first page with limit=2 and received a next_token
    When kai calls GET /_matrix/client/v3/notifications with that next_token and limit=2
    Then the response status is 200
    And the notifications array has 1 item
    And the next_token is absent or empty

  # AC3 — only=highlight filters correctly
  Scenario: GetNotifications_OnlyHighlight_FiltersCorrectly — only=highlight returns just highlights
    Given kai has 1 notification with actions ["notify"] and 1 with actions ["notify","highlight"]
    When kai calls GET /_matrix/client/v3/notifications?only=highlight
    Then the response status is 200
    And the notifications array has 1 item

  # AC4 — limit > 200 returns 400 M_INVALID_PARAM
  Scenario: GetNotifications_LimitExceedsMax_Returns400 — limit=999 returns 400
    When kai calls GET /_matrix/client/v3/notifications?limit=999
    Then the response status is 400
    And the response body contains "M_INVALID_PARAM"

  # AC7 — JWT required
  Scenario: GetNotifications_Unauthenticated_Rejected — request without JWT is rejected
    When an unauthenticated client calls GET /_matrix/client/v3/notifications
    Then the response status is 401
    And the response body contains "M_MISSING_TOKEN"
