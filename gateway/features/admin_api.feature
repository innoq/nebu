Feature: Admin API — User management, role assignment, room archival
  As a developer
  I want Gherkin acceptance tests that cover the key Admin API operations
  So that CI catches regressions in user management, room management, and config operations

  # Story 6-11 — ATDD: tests written FIRST (red phase), before implementation.
  # All scenarios below MUST FAIL until the Admin API endpoints are fully wired up.
  # Auth: Matrix Bearer tokens obtained via Dex Authorization Code flow.
  # Admin actor: kai@example.com (Dex group: instance_admin)
  # Target user: alex@example.com (Dex group: user)

  Background:
    Given the docker compose stack is started

  # AC1 — User management lifecycle:
  #   list users → deactivate → Matrix 401 → reactivate → Matrix 200
  Scenario: User_Management_Lifecycle — deactivate blocks Matrix, reactivate restores access
    Given the instance_admin kai is authenticated for admin API
    And the target user alex is authenticated for admin API
    When the admin calls GET /api/v1/admin/users
    Then the response status is 200
    And the response body contains "data"
    And the response body contains "total"
    When the admin deactivates alex with reason "integration test deactivation"
    Then the response status is 200
    And the response body contains "deactivated"
    When alex calls GET /_matrix/client/v3/sync with their token
    Then the response status is 401
    When the admin reactivates alex
    Then the response status is 200
    And the response body contains "active"
    When alex calls GET /_matrix/client/v3/sync with their token
    Then the response status is 200

  # AC2 — Role assignment lifecycle:
  #   grant compliance_officer → API confirms granted → revoke → API confirms revoked
  #   Compliance endpoint 200/403 tested via compliance@example.com token (JWT-based auth)
  Scenario: Role_Assignment_Lifecycle — grant and revoke compliance_officer role
    Given the instance_admin kai is authenticated for admin API
    And the target user alex is authenticated for admin API
    When the admin grants alex the role "compliance_officer"
    Then the response status is 200
    And the response body contains "granted"
    When the compliance officer user calls GET /api/v1/compliance/access-requests
    Then the response status is 200
    When the admin revokes alex the role "compliance_officer"
    Then the response status is 200
    And the response body contains "revoked"
    When a user without compliance role calls GET /api/v1/compliance/access-requests
    Then the response status is 403

  # AC3 — Room archival:
  #   create room + message → archive → send-event 403 M_ROOM_ARCHIVED → get-messages 200
  Scenario: Room_Archival — archived room blocks writes but allows reads
    Given the instance_admin kai is authenticated for admin API
    And kai creates a room for archival testing
    And kai sends a message to the archival test room
    When the admin archives the archival test room with reason "integration test archival"
    Then the response status is 200
    And the response body contains "archived"
    When kai sends a Matrix event to the archived room
    Then the response status is 403
    And the response body contains "M_ROOM_ARCHIVED"
    When kai calls GET /_matrix/client/v3/rooms/{archivalRoomId}/messages
    Then the response status is 200
    And the response body contains "chunk"
